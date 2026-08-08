package router

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/compression"
	"github.com/vigilagent/vigilagent/internal/cors"
	"github.com/vigilagent/vigilagent/internal/idempotency"
	mw "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/internal/rateguard"
	"github.com/vigilagent/vigilagent/internal/requestid"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/signing"
	"github.com/vigilagent/vigilagent/internal/skillengine"
	"github.com/vigilagent/vigilagent/internal/sse"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// Content from middleware_wiring.go
// MiddlewareConfig holds configuration for the full middleware stack.
type MiddlewareConfig struct {
	Signing  *signing.Signer
	IPFilter interface {
		Middleware(http.Handler) http.Handler
	}
	CORS        *cors.Config
	RequestID   bool
	Timeout     time.Duration
	Idempotency *idempotency.Store
	RateGuard   *rateguard.EndpointLimiter
	// SlowPathTimeout is the bounded deadline applied to LLM-backed endpoints
	// (deep-analyze, review, middleware/process, tasks). Zero means the default
	// of 180s. Must stay below the server's http.Server.WriteTimeout so the
	// graceful error response can still be written when the deadline fires.
	SlowPathTimeout time.Duration
}

// setupSecurityMiddleware applies security-focused middleware: request signing,
// IP filtering, and CORS. These run before business logic.
func (r *Router) setupSecurityMiddleware(cfg *MiddlewareConfig) {
	// 1. Request signing verification (if configured)
	if cfg != nil && cfg.Signing != nil {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				// Skip signing for health, metrics, and OPTIONS
				if req.URL.Path == "/api/v1/health" ||
					req.URL.Path == "/api/v1/metrics" ||
					req.Method == http.MethodOptions {
					next.ServeHTTP(w, req)
					return
				}
				if err := cfg.Signing.VerifyRequest(req, nil); err != nil {
					http.Error(w, "invalid signature", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, req)
			})
		})
	}

	// 2. IP filtering (if configured)
	if cfg != nil && cfg.IPFilter != nil {
		r.Use(cfg.IPFilter.Middleware)
	}

	// 3. CORS (use dedicated package if configured, else fallback to inline)
	if cfg != nil && cfg.CORS != nil {
		r.Use(cfg.CORS.Middleware)
	}
}

// setupResilienceMiddleware applies resilience-focused middleware: per-endpoint
// rate limiting, idempotency protection, and request timeout.
func (r *Router) setupResilienceMiddleware(cfg *MiddlewareConfig) {
	// 1. Per-endpoint rate limiting
	if cfg != nil && cfg.RateGuard != nil {
		r.Use(cfg.RateGuard.Middleware)
	}

	// 2. Idempotency protection
	if cfg != nil && cfg.Idempotency != nil {
		r.Use(cfg.Idempotency.Middleware)
	}

	// 3. Request timeout (override chi's default if configured)
	if cfg != nil && cfg.Timeout > 0 {
		r.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				// Long-lived streams (SSE task streams, WebSocket) are never
				// deadline-wrapped: their lifetime is client-driven, and net/http
				// already cancels the request context when the client disconnects.
				if isUnboundedPath(req) {
					next.ServeHTTP(w, req)
					return
				}
				// LLM-backed endpoints (dual-engine analysis, review pipeline, code
				// generation, middleware/process, agent tasks) legitimately run
				// 20-90s — far past the generic deadline. They get a generous
				// bounded deadline via context cancellation instead of the generic
				// http.TimeoutHandler: the stdlib wrapper panics with a nil-pointer
				// deref when the still-running handler writes to the already-
				// torn-down response, whereas the LLM adapters respect ctx
				// cancellation (their http.Client carries its own timeout), so a
				// stuck provider returns a clean error instead of hanging forever.
				// NOTE: the unbounded check ABOVE must stay first — several slow-
				// prefix paths (e.g. /tasks/.../stream) are intentionally exempt.
				if isSlowLLMPath(req.URL.Path) {
					slowTimeout := cfg.SlowPathTimeout
					if slowTimeout <= 0 {
						slowTimeout = 180 * time.Second
					}
					ctx, cancel := context.WithTimeout(req.Context(), slowTimeout)
					defer cancel()
					next.ServeHTTP(w, req.WithContext(ctx))
					return
				}
				http.TimeoutHandler(next, cfg.Timeout, `{"error":"request timeout"}`).ServeHTTP(w, req)
			})
		})
	}
}

// slowLLMPathPrefixes are route prefixes whose handlers perform LLM calls and
// therefore exceed the generic request deadline.
var slowLLMPathPrefixes = []string{
	"/api/v1/deep-analyze",       // dual-engine analysis (deterministic + LLM in parallel)
	"/api/v1/review",             // 5-role LLM review pipeline (+ code generation from prompt)
	"/api/v1/middleware/process", // middleware pipeline with LLM critique
	"/api/v1/tasks",              // agent execution
}

// unboundedPathPatterns are long-lived streaming endpoints that must never be
// deadline-wrapped. WebSocket is matched exactly (http.TimeoutHandler's wrapper
// ResponseWriter does not implement http.Hijacker, so upgrades would fail under
// it); SSE task streams are matched by their /stream suffix.
func isUnboundedPath(req *http.Request) bool {
	path := req.URL.Path
	if path == "/api/v1/ws" || strings.HasPrefix(path, "/api/v1/ws/") {
		return true
	}
	if strings.HasSuffix(path, "/stream") {
		return true
	}
	// middleware/process streams via SSE when the client asks for it (Accept:
	// text/event-stream) — same long-lived treatment as /stream routes.
	return path == "/api/v1/middleware/process" &&
		(req.Header.Get("Accept") == "text/event-stream" ||
			req.Header.Get("Content-Type") == "text/event-stream")
}

// isSlowLLMPath reports whether the request path is exempt from the aggressive
// http.TimeoutHandler because its handler legitimately takes longer than the
// generic deadline (LLM round-trips, agent execution). Matching requires a path
// boundary after the prefix so that e.g. /api/v1/review does not accidentally
// match /api/v1/review-preview.
func isSlowLLMPath(path string) bool {
	for _, p := range slowLLMPathPrefixes {
		if strings.HasPrefix(path, p) && (len(path) == len(p) || path[len(p)] == '/') {
			return true
		}
	}
	return false
}

// setupObservabilityMiddleware applies observability-focused middleware:
// request ID propagation, structured logging, and sensitive field redaction.
func (r *Router) setupObservabilityMiddleware(cfg *MiddlewareConfig) {
	// 1. Request ID (use dedicated package if configured)
	if cfg != nil && cfg.RequestID {
		r.Use(requestid.Middleware)
	}

	// 2. Sensitive field redaction — logs requests without passwords/API keys.
	r.Use(mw.RedactLogger)
}

// NewWithMiddleware creates a Router with the full middleware stack wired.
// Unlike New(), it replaces the default middleware with the provided config.
// All middleware is wired BEFORE routes are set up, as required by chi.
func NewWithMiddleware(opts Options, mcfg *MiddlewareConfig) *Router {
	r := newRouter(opts)

	// Security-critical dependencies must be wired before routes are registered:
	// setupRoutes() dereferences r.apiKeyCreateRateLimiter and r.loginRateLimiter
	// when registering their routes, so leaving these nil panics at startup/request
	// time (previously only New() initialized them).
	r.initSecurityDependencies()

	// Build handlers using shared logic.
	r.initHandlers()

	// Wire compression (outermost, before security)
	r.Use(compression.Middleware)

	// Panic recovery: without this, a handler panic bubbles up past the
	// TimeoutHandler and crashes the request with a raw 500 + stack trace.
	r.Use(middleware.Recoverer)

	// Wire security middleware first (outermost)
	r.setupSecurityMiddleware(mcfg)

	// Wire resilience middleware
	r.setupResilienceMiddleware(mcfg)

	// Wire observability middleware
	r.setupObservabilityMiddleware(mcfg)

	// Routes must come LAST (after all middleware).
	r.setupRoutes()

	return r
}

// Content from middleware_handlers.go
// middlewareProcessHandler is the core middleware endpoint that runs the
// full pipeline: context → skill match → scan → critique → extract patterns.
// Returns the result as JSON, or streams via SSE if Accept: text/event-stream.
func (r *Router) middlewareProcessHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		TaskType    string        `json:"task_type"`
		Description string        `json:"description"`
		Code        string        `json:"code,omitempty"`
		Language    string        `json:"language,omitempty"`
		Filename    string        `json:"filename,omitempty"`
		Budget      float64       `json:"budget,omitempty"`
		Stream      bool          `json:"stream,omitempty"`
		Context     *contextInput `json:"context,omitempty"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.ErrorR(w, req, http.StatusRequestEntityTooLarge, "VAL_005", "request exceeds 1MB limit")
			return
		}
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Description == "" {
		response.BadRequest(w, "description is required")
		return
	}

	// Convert to middlewareInput
	mwInput := middlewareInput{
		TaskType:    input.TaskType,
		Description: input.Description,
		Code:        input.Code,
		Language:    input.Language,
		Filename:    input.Filename,
		Budget:      input.Budget,
		Context:     input.Context,
	}

	// SSE streaming mode
	if input.Stream || req.Header.Get("Accept") == "text/event-stream" {
		r.handleStreamingProcess(w, req, mwInput)
		return
	}

	// Standard JSON mode — run scanner + extraction
	result := r.runMiddlewarePipeline(req, mwInput)
	response.JSON(w, http.StatusOK, result)
}

// handleStreamingProcess runs the middleware pipeline and streams results via SSE.
func (r *Router) handleStreamingProcess(w http.ResponseWriter, req *http.Request, input middlewareInput) {
	stream := sse.NewStreamer(w)
	if stream == nil {
		response.InternalError(w, "streaming not supported")
		return
	}
	defer stream.Close()

	stream.SendStatus("processing", map[string]string{
		"task_type": input.TaskType,
		"message":   "Starting middleware pipeline...",
	})

	result := r.runMiddlewarePipeline(req, input)

	if result.ScanResult != nil && len(result.ScanResult.Findings) > 0 {
		stream.Send(sse.Event{Event: "findings", Data: result.ScanResult.Findings})
	}
	if len(result.SkillsExtracted) > 0 {
		stream.Send(sse.Event{Event: "patterns", Data: result.SkillsExtracted})
	}

	stream.SendDone(result)
}

// middlewareResult is the aggregated output of the middleware pipeline.
type middlewareResult struct {
	Description     string                 `json:"description"`
	TaskType        string                 `json:"task_type"`
	ScanResult      *scanner.Report        `json:"scan_result,omitempty"`
	SkillsExtracted []*skillengine.Skill   `json:"skills_extracted,omitempty"`
	PipelineResult  *pipelineReport        `json:"pipeline_result,omitempty"`
	Metrics         map[string]interface{} `json:"metrics,omitempty"`
}

type pipelineReport struct {
	Passed     bool    `json:"passed"`
	Confidence float64 `json:"confidence"`
	Layers     []layer `json:"layers"`
}

type layer struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

type contextInput struct {
	Files    []fileInput `json:"files,omitempty"`
	Language string      `json:"language,omitempty"`
}

type fileInput struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

type middlewareInput struct {
	TaskType    string
	Description string
	Code        string
	Language    string
	Filename    string
	Budget      float64
	Context     *contextInput
}

// runMiddlewarePipeline executes the deterministic pipeline (scan → extract patterns).
func (r *Router) runMiddlewarePipeline(req *http.Request, input middlewareInput) *middlewareResult {
	result := &middlewareResult{
		Description: input.Description,
		TaskType:    input.TaskType,
	}

	// Step 1: Run scanner if code is provided
	if input.Code != "" && r.engine != nil {
		report := r.engine.Run(req.Context(), scanner.Input{
			Language: input.Language,
			Code:     input.Code,
			Filename: input.Filename,
		})
		result.ScanResult = report
	}

	// Step 2: Run the unified validation pipeline
	if r.pipeline != nil {
		pipelineReq := &pipelineRequest{
			Description: input.Description,
			Code:        input.Code,
			Language:    input.Language,
			Filename:    input.Filename,
		}
		pipelineReport := r.runPipeline(req, pipelineReq)
		result.PipelineResult = pipelineReport
	}

	// Step 3: Extract vulnerability patterns from scanner findings
	if r.skillEng != nil && result.ScanResult != nil && len(result.ScanResult.Findings) > 0 {
		for _, f := range result.ScanResult.Findings {
			skill, _ := r.skillEng.ExtractFromFinding(skillengine.Finding{
				Severity:   string(f.Severity),
				Message:    f.Message,
				Filename:   f.Filename,
				Line:       f.Line,
				Fix:        f.Fix,
				Analyzers:  f.Analyzers,
				Confidence: f.Confidence,
			})
			// skill is a per-iteration copy; &skill is safe (registry holds its
			// own lock-guarded copy).
			result.SkillsExtracted = append(result.SkillsExtracted, &skill)
		}
	}

	// Step 4: Compute metrics
	result.Metrics = map[string]interface{}{
		"findings_count":   result.findingCount(),
		"skills_extracted": len(result.SkillsExtracted),
		"pipeline_passed":  result.PipelineResult != nil && result.PipelineResult.Passed,
	}

	return result
}

// runPipeline executes the deterministic validation pipeline (requirements + compliance).
// Scanner results are already computed by runMiddlewarePipeline and should not be duplicated.
func (r *Router) runPipeline(_ *http.Request, input *pipelineRequest) *pipelineReport {
	if r.pipeline == nil {
		return nil
	}

	rep := &pipelineReport{
		Passed: true,
		Layers: []layer{},
	}

	// Run requirements resolver
	if r.requirements != nil {
		reqReport := r.requirements.Resolve(input.Description, nil)
		reqPassed := len(reqReport.Missing) == 0
		rep.Layers = append(rep.Layers, layer{Name: "requirements", Passed: reqPassed})
		if !reqPassed {
			rep.Passed = false
		}
	}

	// Run compliance checker
	if r.complianceChecker != nil {
		compReport := r.complianceChecker.Check(input.Description, nil)
		compPassed := len(compReport.Missing) == 0
		rep.Layers = append(rep.Layers, layer{Name: "compliance", Passed: compPassed})
		if !compPassed {
			rep.Passed = false
		}
	}

	// Compute confidence
	passed := 0
	for _, l := range rep.Layers {
		if l.Passed {
			passed++
		}
	}
	if len(rep.Layers) > 0 {
		rep.Confidence = float64(passed) / float64(len(rep.Layers))
	}

	return rep
}

// NOTE: middlewareMetricsHandler and middlewarePatternsHandler are defined in
// extended_handlers.go with real implementations using LLM router health
// and cost intelligence engine. The placeholder stubs were removed here.

// pipelineRequest is the input to the pipeline.
type pipelineRequest struct {
	Description string
	Code        string
	Language    string
	Filename    string
}

// findingCount returns the number of findings in the scan result.
func (m *middlewareResult) findingCount() int {
	if m.ScanResult == nil {
		return 0
	}
	return len(m.ScanResult.Findings)
}
