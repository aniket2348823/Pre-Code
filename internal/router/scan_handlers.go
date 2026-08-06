package router

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/scanner"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// scanHandler runs the deterministic static-analysis engine over submitted code
// and returns a merged, confidence-scored report (Layer 4: static analysis).
// Body size is enforced by the global limitBodySize middleware (2 MiB)
// and by this handler directly as defense-in-depth.
func (r *Router) scanHandler(w http.ResponseWriter, req *http.Request) {
	if _, ok := auth.ClaimsFromContext(req.Context()); !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	// Defense-in-depth: enforce body size directly in the handler
	if req.Body != nil {
		req.Body = http.MaxBytesReader(w, req.Body, maxRequestBodySize)
	}

	var input struct {
		Language string `json:"language"`
		Code     string `json:"code"`
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) || (err != nil && strings.Contains(err.Error(), "too large")) {
			response.ErrorR(w, req, http.StatusRequestEntityTooLarge, "VAL_005", "request body too large")
			return
		}
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Code == "" {
		response.BadRequest(w, "code is required")
		return
	}

	engine := r.engine
	if engine == nil {
		engine = scanner.DefaultEngine()
	}
	report := engine.Run(req.Context(), scanner.Input{
		Language: input.Language,
		Code:     input.Code,
		Filename: input.Filename,
	})

	// Add a 0-100 score and A-F grade to the report. The browser extension's
	// badge renders result.grade; without it every scan shows "Grade: A" even
	// when critical findings exist. The scoring mirrors the dual-engine
	// endpoint so both surfaces report the same grade for the same findings.
	score, grade := gradeFromFindings(report.Findings)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"findings":          report.Findings,
		"analyzers_run":     report.AnalyzersRun,
		"analyzers_skipped": report.AnalyzersSkipped,
		"analyzer_errors":   report.AnalyzerErrors,
		"grade":             grade,
		"score":             score,
	})

	// Dispatch webhook notification for scan completion
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "scan.completed",
			Payload: map[string]interface{}{
				"language": input.Language,
				"filename": input.Filename,
				"findings": len(report.Findings),
			},
		})
	}
}

// gradeFromFindings computes a 0-100 score and A-F letter grade from a set of
// findings by deducting points per severity, matching the scoring used by the
// dual-engine endpoint (deepAnalyzeHandler).
func gradeFromFindings(findings []scanner.Finding) (int, string) {
	score := 100
	for _, f := range findings {
		switch f.Severity {
		case scanner.SeverityCritical:
			score -= 25
		case scanner.SeverityHigh:
			score -= 15
		case scanner.SeverityMedium:
			score -= 10
		case scanner.SeverityLow:
			score -= 5
		}
	}
	if score < 0 {
		score = 0
	}
	grade := "A"
	switch {
	case score >= 90:
		grade = "A"
	case score >= 75:
		grade = "B"
	case score >= 60:
		grade = "C"
	case score >= 45:
		grade = "D"
	default:
		grade = "F"
	}
	return score, grade
}

// dualFinding represents a single finding from either engine.
type dualFinding struct {
	RuleID     string  `json:"rule_id"`
	Engine     string  `json:"engine"`
	Severity   string  `json:"severity"`
	Category   string  `json:"category"`
	Message    string  `json:"message"`
	Fix        string  `json:"fix,omitempty"`
	Line       int     `json:"line,omitempty"`
	Confidence float64 `json:"confidence"`
	Snippet    string  `json:"snippet,omitempty"`
}

// deepAnalyzeHandler is the dual-engine analysis endpoint.
// It REQUIRES authentication (JWT or API key) — the route is registered under
// the protected group so unauthenticated callers cannot burn LLM quota. The
// VSCode extension always sends its VigilAgent API key via the Authorization
// header (see client.ts dualEngine).
// Runs BOTH deterministic scanner + LLM engine IN PARALLEL and merges results.
//
// POST /api/v1/deep-analyze
func (r *Router) deepAnalyzeHandler(w http.ResponseWriter, req *http.Request) {
	start := time.Now()

	if req.Body != nil {
		req.Body = http.MaxBytesReader(w, req.Body, maxRequestBodySize)
	}

	var input struct {
		Language string `json:"language"`
		Code     string `json:"code"`
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Code == "" {
		response.BadRequest(w, "code is required")
		return
	}
	if input.Language == "" {
		input.Language = "auto"
	}
	if input.Filename == "" {
		input.Filename = "code.snippet"
	}

	// Run both engines in parallel
	var (
		detFindings []dualFinding
		llmFindings []dualFinding
		detLatency  time.Duration
		llmLatency  time.Duration
		llmModel    string
		llmError    string
		wg          sync.WaitGroup
	)

	// Engine 1: Deterministic (fast, free)
	wg.Add(1)
	go func() {
		defer wg.Done()
		detStart := time.Now()
		engine := r.engine
		if engine == nil {
			engine = scanner.DefaultEngine()
		}
		report := engine.Run(req.Context(), scanner.Input{
			Language: input.Language,
			Code:     input.Code,
			Filename: input.Filename,
		})
		for _, f := range report.Findings {
			detFindings = append(detFindings, dualFinding{
				RuleID:     f.RuleID,
				Engine:     "deterministic",
				Severity:   string(f.Severity),
				Category:   "security",
				Message:    f.Message,
				Fix:        f.Fix,
				Line:       f.Line,
				Confidence: f.Confidence,
				Snippet:    f.Snippet,
			})
		}
		detLatency = time.Since(detStart)
	}()

	// Engine 2: LLM (slower, semantic understanding)
	wg.Add(1)
	go func() {
		defer wg.Done()
		llmStart := time.Now()

		if r.llmRouter == nil {
			llmError = "no LLM router configured"
			llmLatency = time.Since(llmStart)
			return
		}

		prompt := fmt.Sprintf(`You are a security code reviewer. Analyze this %s code for:
1. Security vulnerabilities (SQL injection, XSS, CSRF, command injection, etc.)
2. Hardcoded secrets and credentials
3. Weak cryptography
4. Logic bugs and edge cases

Return a JSON array of findings. Each finding must have:
- "rule_id": a unique identifier (e.g., "llm-sql-injection-1")
- "severity": "critical", "high", "medium", "low", or "info"
- "category": "injection", "secrets", "xss", "logic", "crypto", "best-practice"
- "message": clear description of the issue
- "fix": how to fix it
- "line": line number if applicable (0 if unknown)
- "confidence": 0.0-1.0 how confident you are

Return ONLY the JSON array, no other text.

Code:
`+"```"+input.Language+"\n"+input.Code+"\n```", input.Language)

		task := &llm.Task{
			ID:          "dual-engine-llm-" + time.Now().Format("20060102150405"),
			Type:        "security",
			Description: "Dual-engine LLM code analysis",
			Messages:    []llm.Message{{Role: "user", Content: prompt}},
		}

		result, err := r.llmRouter.ExecuteWithFailover(req.Context(), task)
		if err != nil {
			llmError = err.Error()
			slog.Warn("dual-engine LLM analysis failed", "error", err)
			llmLatency = time.Since(llmStart)
			return
		}

		llmModel = result.Model

		// Parse JSON array of findings from LLM response
		content := result.Content
		// Extract JSON array from response (may have markdown fences)
		if idx := strings.Index(content, "["); idx >= 0 {
			if end := strings.LastIndex(content, "]"); end > idx {
				content = content[idx : end+1]
			}
		}

		var parsed []dualFinding
		if jsonErr := json.Unmarshal([]byte(content), &parsed); jsonErr != nil {
			slog.Warn("failed to parse LLM findings JSON", "error", jsonErr)
			truncated := result.Content
			if len(truncated) > 200 {
				truncated = truncated[:200]
			}
			llmFindings = []dualFinding{{
				RuleID:     "llm-analysis-complete",
				Engine:     "llm",
				Severity:   "info",
				Category:   "analysis",
				Message:    "LLM analysis completed but response could not be parsed as structured JSON. Raw: " + truncated,
				Confidence: 0.3,
			}}
			llmLatency = time.Since(llmStart)
			return
		}

		// Tag all findings as from LLM engine
		for i := range parsed {
			parsed[i].Engine = "llm"
		}
		llmFindings = parsed
		llmLatency = time.Since(llmStart)
	}()

	wg.Wait()

	// Merge findings from both engines
	allFindings := append(detFindings, llmFindings...)

	// Calculate score and grade
	score := 100
	for _, f := range allFindings {
		switch f.Severity {
		case "critical":
			score -= 25
		case "high":
			score -= 15
		case "medium":
			score -= 10
		case "low":
			score -= 5
		}
	}
	if score < 0 {
		score = 0
	}
	grade := "A"
	switch {
	case score >= 90:
		grade = "A"
	case score >= 75:
		grade = "B"
	case score >= 60:
		grade = "C"
	case score >= 45:
		grade = "D"
	default:
		grade = "F"
	}

	totalLatency := time.Since(start)

	llmStats := map[string]interface{}{
		"findings_count": len(llmFindings),
		"latency_ms":     llmLatency.Milliseconds(),
		"model":          llmModel,
	}
	if llmError != "" {
		llmStats["error"] = llmError
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"findings": allFindings,
		"score":    score,
		"grade":    grade,
		"engine_stats": map[string]interface{}{
			"deterministic": map[string]interface{}{
				"findings_count": len(detFindings),
				"latency_ms":     detLatency.Milliseconds(),
			},
			"llm":              llmStats,
			"total_latency_ms": totalLatency.Milliseconds(),
		},
		"summary": fmt.Sprintf("Dual-engine analysis: %d deterministic + %d LLM findings. Grade: %s (%d/100)",
			len(detFindings), len(llmFindings), grade, score),
		"metadata": map[string]interface{}{
			"code_length":           len(input.Code),
			"language":              input.Language,
			"analyzed_at":           time.Now().UTC().Format(time.RFC3339),
			"corroborated_findings": 0,
		},
	})
}

// ── Requirements, Validation & Pipeline Delegates ─────────
// These endpoints delegate to pre-built sub-handlers wired into the
// Router struct (e.g. schema validation, compliance, knowledge graph).

// requirementsHandler delegates to the pre-built handler stored in the router.
func (r *Router) requirementsHandler(w http.ResponseWriter, req *http.Request) {
	r.requirementsHandlerFn.ServeHTTP(w, req)
}

// validateHandler delegates to the pre-built handler stored in the router.
func (r *Router) validateHandler(w http.ResponseWriter, req *http.Request) {
	r.validateHandlerFn.ServeHTTP(w, req)
}

// schemaHandler delegates to the pre-built schema validation handler.
func (r *Router) schemaHandler(w http.ResponseWriter, req *http.Request) {
	r.schemaHandlerFn.ServeHTTP(w, req)
}

// complianceHandler delegates to the pre-built compliance handler.
func (r *Router) complianceHandler(w http.ResponseWriter, req *http.Request) {
	r.complianceHandlerFn.ServeHTTP(w, req)
}

// pipelineHandler delegates to the pre-built unified validation pipeline handler.
func (r *Router) pipelineHandler(w http.ResponseWriter, req *http.Request) {
	r.pipelineHandlerFn.ServeHTTP(w, req)
}

// knowledgeHandler delegates to the pre-built knowledge graph handler.
func (r *Router) knowledgeHandler(w http.ResponseWriter, req *http.Request) {
	r.knowledgeHandlerFn.ServeHTTP(w, req)
}

// skillEngineHandler delegates to the pre-built skill extraction handler.
func (r *Router) skillEngineHandler(w http.ResponseWriter, req *http.Request) {
	r.skillEngineHandlerFn.ServeHTTP(w, req)
}

// confidenceHandler delegates to the pre-built confidence engine handler.
func (r *Router) confidenceHandler(w http.ResponseWriter, req *http.Request) {
	r.confidenceHandlerFn.ServeHTTP(w, req)
}

// attackGraphHandler delegates to the pre-built attack graph handler.
func (r *Router) attackGraphHandler(w http.ResponseWriter, req *http.Request) {
	r.attackGraphHandlerFn.ServeHTTP(w, req)
}

// auditHandler delegates to the pre-built audit trail handler.
func (r *Router) auditHandler(w http.ResponseWriter, req *http.Request) {
	r.auditHandlerFn.ServeHTTP(w, req)
}
