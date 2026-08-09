package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/signing"
)

// priceTableMu protects concurrent read-modify-write cycles on the global PriceTable.
var priceTableMu sync.Mutex

// Config holds proxy server configuration.
type Config struct {
	Port       string
	BackendURL string
	APIKey     string
	// Proxy auth: comma-separated list of allowed API keys for the proxy itself.
	// Empty falls back to APIKey. If both are empty, the proxy rejects all
	// requests (fail closed — no open-proxy mode).
	AllowedAPIKeys string
	// ProvenanceSecret signs the per-scan provenance/audit records (HMAC-SHA256).
	// Falls back to APIKey, then a dev-only constant. Never exposed to clients.
	ProvenanceSecret string
	// AllowedModels is a comma-separated allowlist of model names/globs the
	// gateway will serve (e.g. "gpt-4o*,claude-3*"). Empty = allow all.
	AllowedModels string
	// PerKeyDailyQuota caps requests per authenticated key (0 = unlimited).
	PerKeyDailyQuota int
	// TLS
	TLSCertFile string
	TLSKeyFile  string
	// Per-provider keys (used when no BYOK header is present)
	OpenAIKey     string
	AnthropicKey  string
	GeminiKey     string
	NVIDIAKey     string
	GroqKey       string
	MistralKey    string
	CohereKey     string
	OpenRouterKey string
	DeepSeekKey   string
}

// ProxyServer is the VigilAgent LLM Interceptor Gateway.
type ProxyServer struct {
	cfg       Config
	router    *chi.Mux
	client    *http.Client
	reqCount  uint64
	errCount  uint64
	latencies []int64
	latencyMu sync.RWMutex
	// Per-key usage tracking (survives across requests within a process lifetime)
	usageMu    sync.RWMutex
	usageByKey map[string]*KeyUsage
	// Allowed API keys for proxy auth (nil = auth disabled)
	allowedKeys map[string]struct{}
	// Shared response cache for all requests
	sharedCache *llm.InMemoryCache
	// Signed provenance/audit records for scan decisions (bounded in-memory ring).
	provenance       *provenanceStore
	provenanceSecret string
	// Decision counters for the /metrics dashboard (verdict:policy → count).
	decisionMu    sync.Mutex
	decisionCount map[string]uint64
}

// KeyUsage tracks per-key usage metrics.
type KeyUsage struct {
	RequestCount uint64  `json:"request_count"`
	TotalCost    float64 `json:"total_cost"`
	TotalTokens  int     `json:"total_tokens"`
	LastUsed     int64   `json:"last_used"`
	ErrorCount   uint64  `json:"error_count"`
}

// NewServer creates a new proxy server with all routes.
func NewServer(cfg Config) *ProxyServer {
	s := &ProxyServer{
		cfg:         cfg,
		router:      chi.NewRouter(),
		client:      &http.Client{Timeout: 120 * time.Second},
		usageByKey:  make(map[string]*KeyUsage),
		allowedKeys: resolveAllowedKeys(cfg),
		sharedCache: llm.NewInMemoryCache(5 * time.Minute), provenance: newProvenanceStore(1000),
		provenanceSecret: resolveProvenanceSecret(cfg),
		decisionCount:    make(map[string]uint64),
	}
	s.setupMiddleware()
	s.routes()
	return s
}

// resolveAllowedKeys returns the set of API keys the proxy accepts. Fail closed:
// AllowedAPIKeys takes precedence; otherwise the single proxy APIKey is used.
// If neither is configured, the returned set is empty and the auth middleware
// rejects every request (no open-proxy mode).
func resolveAllowedKeys(cfg Config) map[string]struct{} {
	if cfg.AllowedAPIKeys != "" {
		return parseAllowedKeys(cfg.AllowedAPIKeys)
	}
	if cfg.APIKey != "" {
		return map[string]struct{}{cfg.APIKey: {}}
	}
	return map[string]struct{}{}
}

// parseAllowedKeys splits a comma-separated key list into a set.
func parseAllowedKeys(csv string) map[string]struct{} {
	if csv == "" {
		return nil
	}
	keys := make(map[string]struct{})
	for _, k := range strings.Split(csv, ",") {
		k = strings.TrimSpace(k)
		if k != "" {
			keys[k] = struct{}{}
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

// setupMiddleware adds logging, metrics, rate limiting, and auth middleware.
func (s *ProxyServer) setupMiddleware() {
	s.router.Use(middleware.RequestID)
	s.router.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ip := r.RemoteAddr
			if idx := strings.LastIndex(ip, ":"); idx > 0 {
				ip = ip[:idx]
			}
			r.RemoteAddr = ip
			next.ServeHTTP(w, r)
		})
	})
	s.router.Use(middleware.Recoverer)
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.metricsMiddleware)
	s.router.Use(s.rateLimitMiddleware)
	// Auth is always enforced — fail closed. If no allowed keys are configured,
	// every request is rejected rather than opening an unauthenticated proxy
	// that would burn the operator's provider keys.
	s.router.Use(s.authMiddleware)
}

// authMiddleware validates the proxy's own API key via X-API-Key or Authorization header.
func (s *ProxyServer) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for health and metrics endpoints
		if r.URL.Path == "/health" || r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		key := r.Header.Get("X-API-Key")
		if key == "" {
			// Try Authorization: Bearer <key>
			auth := r.Header.Get("Authorization")
			if strings.HasPrefix(auth, "Bearer ") {
				key = strings.TrimPrefix(auth, "Bearer ")
			}
		}

		if key == "" {
			http.Error(w, `{"error":"missing API key: provide X-API-Key header or Authorization: Bearer <key>"}`, http.StatusUnauthorized)
			return
		}

		if _, ok := s.allowedKeys[key]; !ok {
			http.Error(w, `{"error":"invalid API key"}`, http.StatusUnauthorized)
			return
		}

		// Store key in context for usage tracking
		ctx := context.WithValue(r.Context(), apiKeyContextKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type contextKey string

const apiKeyContextKey contextKey = "api_key"

func (s *ProxyServer) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		latency := time.Since(start)
		// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
		slog.Info("proxy request", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "latency", latency.Round(time.Millisecond))
	})
}

func (s *ProxyServer) metricsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddUint64(&s.reqCount, 1)
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		latency := time.Since(start).Milliseconds()
		s.latencyMu.Lock()
		s.latencies = append(s.latencies, latency)
		if len(s.latencies) > 1000 {
			s.latencies = s.latencies[1:]
		}
		s.latencyMu.Unlock()
		if ww.Status() >= 400 {
			atomic.AddUint64(&s.errCount, 1)
		}
	})
}

func (s *ProxyServer) rateLimitMiddleware(next http.Handler) http.Handler {
	// Simple in-memory rate limiter: 100 req/min per IP
	type clientState struct {
		count     int
		resetTime time.Time
	}
	var mu sync.Mutex
	clients := make(map[string]*clientState)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use RealIP middleware's result if available, otherwise parse from RemoteAddr
		ip := r.RemoteAddr
		if idx := strings.LastIndex(ip, ":"); idx > 0 {
			ip = ip[:idx]
		}
		mu.Lock()
		state, ok := clients[ip]
		now := time.Now()
		if !ok || now.After(state.resetTime) {
			state = &clientState{count: 0, resetTime: now.Add(time.Minute)}
			clients[ip] = state
		}
		state.count++
		if state.count > 100 {
			mu.Unlock()
			http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
			return
		}
		mu.Unlock()
		next.ServeHTTP(w, r)
	})
}

// Router returns the http.Handler for the proxy.
func (s *ProxyServer) Router() http.Handler { return s.router }

// StartHealthChecks begins periodic health checks on all registered providers.
// It uses a background ModelRouter that has all configured providers.
func (s *ProxyServer) StartHealthChecks(ctx context.Context, interval time.Duration) {
	// Build a router with all configured providers for health checking
	router := s.buildRouter("", "")
	router.StartHealthChecks(ctx, interval)
}

func (s *ProxyServer) routes() {
	s.router.Get("/health", s.handleHealth)
	s.router.Get("/metrics", s.handleMetrics)
	s.router.Get("/v1/usage", s.handleUsage)
	s.router.Post("/v1/chat/completions", s.handleChatCompletions)
	s.router.Post("/v1/messages", s.handleMessages)
	// OpenAI Responses API surface (spec: POST /v1/responses)
	s.router.Post("/v1/responses", s.handleResponses)
	// Provenance / audit service
	s.router.Get("/v1/provenance", s.handleProvenanceGet)
	s.router.Post("/v1/provenance/verify", s.handleProvenanceVerify)
	s.router.Post("/v1/provenance/attest", s.handleProvenanceAttest)
	// Provider catalog endpoints
	s.router.Get("/v1/providers", s.handleListProviders)
	s.router.Get("/v1/providers/{providerID}/models", s.handleProviderModels)
	// Model filtering endpoint
	s.router.Get("/v1/models", s.handleListModels)
	// Analysis endpoints
	s.router.Post("/v1/analyze", s.handleAnalyze)
	s.router.Post("/v1/deep-analyze", s.handleDeepAnalyze)
}

func (s *ProxyServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"healthy","service":"vigilagent-proxy"}`))
}

func (s *ProxyServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	s.latencyMu.RLock()
	var avgLatency float64
	var p50Latency float64
	if len(s.latencies) > 0 {
		var sum int64
		for _, l := range s.latencies {
			sum += l
		}
		avgLatency = float64(sum) / float64(len(s.latencies))
		// Compute P50
		sorted := make([]int64, len(s.latencies))
		copy(sorted, s.latencies)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		p50Idx := len(sorted) / 2
		p50Latency = float64(sorted[p50Idx])
	}
	s.latencyMu.RUnlock()

	// Aggregate usage stats
	s.usageMu.RLock()
	var totalRequests, totalErrors uint64
	var totalTokens int
	var totalCost float64
	for _, u := range s.usageByKey {
		totalRequests += u.RequestCount
		totalErrors += u.ErrorCount
		totalTokens += u.TotalTokens
		totalCost += u.TotalCost
	}
	keyCount := len(s.usageByKey)
	s.usageMu.RUnlock()

	s.decisionMu.Lock()
	decisionBreakdown := make(map[string]uint64, len(s.decisionCount))
	var totalDecisions uint64
	for k, v := range s.decisionCount {
		decisionBreakdown[k] = v
		totalDecisions += v
	}
	s.decisionMu.Unlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"requests_total": atomic.LoadUint64(&s.reqCount),
		"errors_total":   atomic.LoadUint64(&s.errCount),
		"avg_latency_ms": avgLatency,
		"p50_latency_ms": p50Latency,
		"healthy":        true,
		"service":        "vigilagent-proxy",
		"timestamp":      time.Now().Unix(),
		"decisions": map[string]interface{}{
			"count":             totalDecisions,
			"by_verdict_policy": decisionBreakdown,
		},
		"usage": map[string]interface{}{
			"tracked_keys":   keyCount,
			"total_requests": totalRequests,
			"total_errors":   totalErrors,
			"total_tokens":   totalTokens,
			"total_cost":     totalCost,
		},
	})
}

// handleUsage returns per-key usage breakdown.
func (s *ProxyServer) handleUsage(w http.ResponseWriter, r *http.Request) {
	s.usageMu.RLock()
	keys := make(map[string]*KeyUsage, len(s.usageByKey))
	for k, v := range s.usageByKey {
		keys[k] = v
	}
	s.usageMu.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"keys":  keys,
		"count": len(keys),
	})
}

// handleListProviders returns all available providers with their models.
func (s *ProxyServer) handleListProviders(w http.ResponseWriter, r *http.Request) {
	catalog := llm.GetFullCatalog()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"providers": catalog})
}

// handleProviderModels returns models for a specific provider.
func (s *ProxyServer) handleProviderModels(w http.ResponseWriter, r *http.Request) {
	providerID := llm.ProviderID(r.PathValue("providerID"))
	models := llm.ProviderModels(providerID)
	if models == nil {
		http.Error(w, `{"error":"unknown provider"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"models": models, "count": len(models)})
}

// handleListModels returns all models across all providers with optional capability filtering.
// Query params: ?capabilities=tools,vision&provider=openai&max_cost=0.10
func (s *ProxyServer) handleListModels(w http.ResponseWriter, r *http.Request) {
	catalog := llm.GetFullCatalog()

	// Parse query params
	capabilities := strings.Split(r.URL.Query().Get("capabilities"), ",")
	providerFilter := r.URL.Query().Get("provider")
	maxCostStr := r.URL.Query().Get("max_cost")

	var maxCost float64
	if maxCostStr != "" {
		fmt.Sscanf(maxCostStr, "%f", &maxCost)
	}

	var filtered []llm.ModelCatalogEntry
	for _, cat := range catalog {
		if providerFilter != "" && cat.Provider.ID != llm.ProviderID(providerFilter) {
			continue
		}
		for _, m := range cat.Models {
			if m.Deprecated {
				continue
			}
			// Filter by capabilities
			if len(capabilities) > 0 && capabilities[0] != "" {
				hasAll := true
				for _, cap := range capabilities {
					cap = strings.TrimSpace(cap)
					if cap == "" {
						continue
					}
					found := false
					for _, mc := range m.Capabilities {
						if mc == cap {
							found = true
							break
						}
					}
					if !found {
						hasAll = false
						break
					}
				}
				if !hasAll {
					continue
				}
			}
			// Filter by max cost (per 1M input tokens)
			if maxCost > 0 && m.InputCostPer1M > maxCost {
				continue
			}
			filtered = append(filtered, m)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"models": filtered, "count": len(filtered)})
}

func (s *ProxyServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleProxyRequest(w, r, "openai")
}

func (s *ProxyServer) handleMessages(w http.ResponseWriter, r *http.Request) {
	s.handleProxyRequest(w, r, "anthropic")
}

type baseRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

// handleProxyRequest is the core proxy handler:
// 1. Reads the incoming request
// 2. Detects provider (BYOK header or model prefix)
// 3. Routes through ModelRouter for smart routing + failover
// 4. Optionally runs analysis on code blocks
func (s *ProxyServer) handleProxyRequest(w http.ResponseWriter, r *http.Request, defaultFormat string) {
	// Limit request body to 10MB to prevent OOM on oversized requests.
	const maxRequestBodySize = 10 << 20 // 10MB
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize+1))
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}
	if len(bodyBytes) > maxRequestBodySize {
		http.Error(w, `{"error":"request body too large (max 10MB)"}`, http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req baseRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	// ── BYOK: Check for user's LLM key via header ──
	llmKey := r.Header.Get("X-LLM-Key")
	llmProvider := r.Header.Get("X-LLM-Provider")     // optional hint
	llmModel := r.Header.Get("X-LLM-Model")           // optional override
	analysisMode := r.Header.Get("X-VigilAgent-Mode") // passthrough | scan | verify | auto

	if llmModel != "" {
		req.Model = llmModel
	}

	// ── Tenant policy checks (before any provider call) ──
	// Per-tenant model allowlist and per-key quota are enforced before routing
	// so unauthorized models/quotas never burn provider spend.
	if !s.modelAllowed(req.Model) {
		http.Error(w, `{"error":"model not allowed by tenant policy"}`, http.StatusForbidden)
		return
	}
	if s.cfg.PerKeyDailyQuota > 0 && s.usageCount(getAPIKey(r.Context())) >= uint64(s.cfg.PerKeyDailyQuota) {
		http.Error(w, `{"error":"daily request quota exceeded"}`, http.StatusTooManyRequests)
		return
	}

	// Build per-request ModelRouter with BYOK if provided, else use backend keys
	modelRouter := s.buildRouter(llmKey, llmProvider)

	// BYOK: inject the requested model into the PriceTable so routing works
	// Must copy existing table first — SetPrices replaces, not merges
	// Inject the requested model into the PriceTable so routing works
	// Must copy existing table first — SetPrices replaces, not merges
	if req.Model != "" {
		priceTableMu.Lock()
		providerID := resolveProviderID(llmKey, llmProvider)
		if llmKey == "" {
			providerID = inferProviderFromModel(req.Model, s.cfg)
		}
		existing := llm.AllPrices()
		existing[req.Model] = llm.ModelInfo{
			Name:            req.Model,
			Provider:        string(providerID),
			InputCostPer1K:  0.001,
			OutputCostPer1K: 0.002,
			MaxTokens:       4096,
			Capabilities:    []string{"tools", "vision", "reasoning"},
		}
		modelRouter.SetPrices(existing)
		priceTableMu.Unlock()
	}

	if req.Stream {
		s.handleStreaming(w, r, modelRouter, bodyBytes, defaultFormat, req.Model)
		return
	}

	// ── Non-streaming: Use ModelRouter.ExecuteWithFailover for smart routing ──
	task := s.buildTask(r.Context(), bodyBytes, req.Model)
	if task == nil {
		http.Error(w, `{"error":"invalid request: missing messages"}`, http.StatusBadRequest)
		return
	}

	// ── Design-stage gate: scan the request BEFORE generation ──
	// If the prompt/design itself carries risks (hardcoded secrets, embedded
	// commands, raw SQL built from user input), append policy-mandated secure
	// constraints to the provider request. The LLM does not decide whether a
	// security requirement is mandatory — the policy engine does.
	mode := EnforcementMode(analysisMode)
	if mode == "" {
		mode = ModeObserve
	}
	designFindings, constrained := s.applyDesignGate(task)
	if constrained {
		w.Header().Set("X-VigilAgent-Design-Gate", "constrained")
	} else if mode != ModePassthrough {
		w.Header().Set("X-VigilAgent-Design-Gate", "passed")
	}

	resp, err := modelRouter.ExecuteWithFailover(r.Context(), task)
	if err != nil {
		slog.Error("model router execution failed", "error", err)
		s.recordUsage(getAPIKey(r.Context()), 0, 0, true)
		errJSON, _ := json.Marshal(map[string]string{"error": "llm request failed: " + err.Error()})
		http.Error(w, string(errJSON), http.StatusBadGateway)
		return
	}

	// Track usage
	s.recordUsage(getAPIKey(r.Context()), resp.Cost, resp.InputTokens+resp.OutputTokens, false)

	// ── Extract and analyze code blocks (DUAL-ENGINE PARALLEL ANALYSIS) ──
	content := resp.Content

	// Analysis outcome (advisory verdict) exposed via headers + body metadata.
	// The policy decision is derived from the verdict under the requested mode
	// (observe | balanced | strict) — see ComputePolicy.
	var analysisOutcome *AnalysisOutcome
	var analyzedFindings []Finding
	if mode != ModePassthrough {
		outcome, findings, summary, _ := s.analyzeAndVerdict(r.Context(), modelRouter, llmKey, content)
		if outcome != nil {
			outcome.Policy = ComputePolicy(*outcome, mode)
			analysisOutcome = outcome
			analyzedFindings = findings
			if summary != "" {
				resp.Content += "\n\n" + summary
			}
		}
	}

	// ── Policy enforcement ──
	// strict: block → nothing is released (HTTP 451). balanced: hold_for_review
	// → prose passes, code blocks are withheld for human review. observe:
	// advisory only (the verdict is carried in headers/metadata, never enforced).
	var provenanceRec signing.ProvenanceRecord
	var provenanceSig string
	if analysisOutcome != nil {
		s.recordDecision(analysisOutcome.Verdict, analysisOutcome.Policy)
		provenanceRec, provenanceSig = s.recordProvenance(r, resp.Provider, resp.Model, analysisOutcome.Policy, mode, content, "")
		analysisOutcome.ScanID = provenanceRec.ScanID

		release, status, reason := enforcePolicy(analysisOutcome.Policy, mode)
		if !release {
			// Strict mode: no output is released until the scan policy allows it.
			applyAnalysisHeaders(w, *analysisOutcome)
			w.Header().Set("X-VigilAgent-Policy", string(analysisOutcome.Policy))
			w.Header().Set("X-VigilAgent-Scan-ID", provenanceRec.ScanID)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"error":      reason,
				"decision":   analysisOutcome.Policy,
				"verdict":    analysisOutcome.Verdict,
				"grade":      analysisOutcome.Grade,
				"score":      analysisOutcome.Score,
				"scan_id":    provenanceRec.ScanID,
				"findings":   analyzedFindings,
				"provenance": map[string]interface{}{"record": provenanceRec, "signature": provenanceSig},
			})
			return
		}
		if analysisOutcome.Policy == PolicyHoldForReview && mode == ModeBalanced {
			// Balanced mode: explanatory text flows, code blocks are withheld
			// until a human reviews and approves them.
			resp.Content = redactCodeBlocks(resp.Content)
		}
	}

	// Build OpenAI-compatible response
	oResp := map[string]interface{}{
		"id":      "chatcmpl-vigil-" + time.Now().Format("20060102150405"),
		"object":  "chat.completion",
		"created": time.Now().Unix(),
		"model":   resp.Model,
		"choices": []map[string]interface{}{
			{
				"index": 0,
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": resp.Content,
				},
				"finish_reason": resp.StopReason,
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     resp.InputTokens,
			"completion_tokens": resp.OutputTokens,
			"total_tokens":      resp.InputTokens + resp.OutputTokens,
		},
	}

	// Add VigilAgent metadata headers
	w.Header().Set("X-VigilAgent-Provider", resp.Provider)
	w.Header().Set("X-VigilAgent-Model", resp.Model)
	if resp.Cost > 0 {
		w.Header().Set("X-VigilAgent-Cost", formatFloat(resp.Cost))
	}
	// Advisory verdict from the dual-engine analysis (headers must be set
	// before WriteHeader).
	if analysisOutcome != nil {
		applyAnalysisHeaders(w, *analysisOutcome)
		w.Header().Set("X-VigilAgent-Policy", string(analysisOutcome.Policy))
		oResp["vigilagent"] = *analysisOutcome
	}
	if designFindings != nil {
		oResp["design_gate"] = map[string]interface{}{
			"status":              "constrained",
			"findings":            len(designFindings),
			"constraints_applied": true,
		}
	}
	if provenanceRec.ScanID != "" {
		w.Header().Set("X-VigilAgent-Scan-ID", provenanceRec.ScanID)
		w.Header().Set("X-VigilAgent-Provenance", signing.ProvenanceVerified)
		if provenanceSig != "" {
			w.Header().Set("X-VigilAgent-Provenance-Signature", provenanceSig)
		}
		oResp["provenance"] = map[string]interface{}{"record": provenanceRec, "signature": provenanceSig}
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(oResp)
}

// AnalysisOutcome is the advisory verdict computed from merged dual-engine
// findings and exposed to clients via X-VigilAgent-* headers and the
// "vigilagent" response metadata. It is purely advisory: the proxy never
// blocks or alters traffic — the client (extension, MCP, curl) decides
// whether and how to enforce it.
type AnalysisOutcome struct {
	Verdict             string         `json:"verdict"`        // pass | warn | block
	Grade               string         `json:"grade"`          // A-F
	Score               int            `json:"score"`          // 0-100
	FindingsCount       int            `json:"findings_count"` // merged findings
	Corroborated        int            `json:"corroborated"`   // found by both engines
	Policy              PolicyDecision `json:"policy,omitempty"`
	ScanID              string         `json:"scan_id,omitempty"`
	ScannersUnavailable bool           `json:"scanners_unavailable,omitempty"`
}

// ComputeVerdict maps merged findings to an advisory verdict:
//   - block: any critical or high severity issue
//   - warn:  any medium issue, or the score dropped below 70
//   - pass:  otherwise
func ComputeVerdict(findings []Finding) AnalysisOutcome {
	score, grade := CalculateScore(findings)
	corroborated := 0
	hasCriticalOrHigh := false
	hasMedium := false
	for _, f := range findings {
		switch f.Severity {
		case "critical", "high":
			hasCriticalOrHigh = true
		case "medium":
			hasMedium = true
		}
		if strings.Contains(f.RuleID, "+llm") {
			corroborated++
		}
	}
	verdict := "pass"
	switch {
	case hasCriticalOrHigh:
		verdict = "block"
	case hasMedium || score < 70:
		verdict = "warn"
	}
	return AnalysisOutcome{
		Verdict:       verdict,
		Grade:         grade,
		Score:         score,
		FindingsCount: len(findings),
		Corroborated:  corroborated,
	}
}

// applyAnalysisHeaders writes the advisory verdict headers for a response.
func applyAnalysisHeaders(w http.ResponseWriter, o AnalysisOutcome) {
	w.Header().Set("X-VigilAgent-Verdict", o.Verdict)
	w.Header().Set("X-VigilAgent-Grade", o.Grade)
	w.Header().Set("X-VigilAgent-Score", strconv.Itoa(o.Score))
	w.Header().Set("X-VigilAgent-Findings", strconv.Itoa(o.FindingsCount))
	w.Header().Set("X-VigilAgent-Corroborated", strconv.Itoa(o.Corroborated))
}

// buildTask creates an llm.Task from the request body.
func (s *ProxyServer) buildTask(ctx context.Context, bodyBytes []byte, model string) *llm.Task {
	var reqBody struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(bodyBytes, &reqBody); err != nil {
		return nil
	}
	if len(reqBody.Messages) == 0 {
		return nil
	}

	messages := make([]llm.Message, len(reqBody.Messages))
	for i, m := range reqBody.Messages {
		messages[i] = llm.Message{Role: m.Role, Content: m.Content}
	}

	return &llm.Task{
		ID:          "proxy-" + time.Now().Format("20060102150405"),
		Type:        "feature",
		Description: "Proxy request",
		Messages:    messages,
		TargetModel: model,
	}
}

// buildRouter creates a ModelRouter for the current request.
// If llmKey is provided (BYOK), it creates an ephemeral router with that key
// and injects the requested model into the PriceTable so routing works.
// Otherwise, it creates a router with all configured backend keys.
// Wires cache and budget guard when available.
func (s *ProxyServer) buildRouter(llmKey, hintProvider string) *llm.ModelRouter {
	router := llm.NewModelRouter(&llm.RouterConfig{
		DefaultModel:        "gpt-4o-mini",
		BudgetPerTask:       10.00,
		DefaultOutputTokens: 4096,
	})

	// Wire shared response cache
	router.SetCache(s.sharedCache)

	if llmKey != "" {
		// BYOK: create a single provider from the user's key
		providerID := resolveProviderID(llmKey, hintProvider)
		p, err := createProvider(providerID, llmKey)
		if err != nil {
			slog.Error("failed to create provider", "provider", providerID, "error", err)
			return router
		}
		router.RegisterProvider(string(providerID), p)
		return router
	}

	// No BYOK: register all configured backend providers
	if s.cfg.OpenAIKey != "" {
		router.RegisterProvider("openai", llm.NewOpenAI(s.cfg.OpenAIKey))
	}
	if s.cfg.AnthropicKey != "" {
		router.RegisterProvider("anthropic", llm.NewAnthropic(s.cfg.AnthropicKey))
	}
	if s.cfg.GeminiKey != "" {
		if p, err := llm.NewGemini(s.cfg.GeminiKey); err == nil {
			router.RegisterProvider("gemini", p)
		}
	}
	if s.cfg.GroqKey != "" {
		router.RegisterProvider("groq", llm.NewGroq(s.cfg.GroqKey))
	}
	if s.cfg.MistralKey != "" {
		router.RegisterProvider("mistral", llm.NewMistral(s.cfg.MistralKey))
	}
	if s.cfg.CohereKey != "" {
		router.RegisterProvider("cohere", llm.NewCohere(s.cfg.CohereKey))
	}
	if s.cfg.NVIDIAKey != "" {
		router.RegisterProvider("nvidia_nim", llm.NewNVIDIANIM(s.cfg.NVIDIAKey))
	}
	if s.cfg.OpenRouterKey != "" {
		router.RegisterProvider("openrouter", llm.NewOpenRouter(s.cfg.OpenRouterKey))
	}
	if s.cfg.DeepSeekKey != "" {
		router.RegisterProvider("deepseek", llm.NewDeepSeek(s.cfg.DeepSeekKey))
	}

	return router
}

// inferProviderFromModel determines the provider from the model name and available keys.
func inferProviderFromModel(model string, cfg Config) llm.ProviderID {
	if cfg.OpenRouterKey != "" && (strings.Contains(model, "/") || strings.HasSuffix(model, ":free")) {
		return llm.ProviderOpenRouter
	}
	switch {
	case strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1-") || strings.HasPrefix(model, "o3-") || strings.HasPrefix(model, "o4-") || strings.HasPrefix(model, "gpt-4.5"):
		return llm.ProviderOpenAI
	case strings.HasPrefix(model, "claude-"):
		return llm.ProviderAnthropic
	case strings.HasPrefix(model, "gemini-"):
		return llm.ProviderGemini
	case strings.HasPrefix(model, "llama-") || strings.HasPrefix(model, "mixtral-") || strings.HasPrefix(model, "gemma"):
		if cfg.GroqKey != "" {
			return llm.ProviderGroq
		}
		if cfg.NVIDIAKey != "" {
			return llm.ProviderNVIDIANIM
		}
		return llm.ProviderGroq
	case strings.HasPrefix(model, "mistral") || strings.HasPrefix(model, "open-mixtral") || strings.HasPrefix(model, "codestral") || strings.HasPrefix(model, "pixtral"):
		return llm.ProviderMistral
	case strings.HasPrefix(model, "command"):
		return llm.ProviderCohere
	case strings.HasPrefix(model, "nvidia/") || strings.HasPrefix(model, "meta/") || strings.HasPrefix(model, "mistralai/") || strings.HasPrefix(model, "moonshotai/") || strings.HasPrefix(model, "qwen/") || strings.HasPrefix(model, "deepseek-ai/"):
		return llm.ProviderNVIDIANIM
	case strings.HasPrefix(model, "deepseek-") || strings.HasPrefix(model, "kimi-"):
		if cfg.NVIDIAKey != "" {
			return llm.ProviderNVIDIANIM
		}
		return llm.ProviderDeepSeek
	default:
		if cfg.NVIDIAKey != "" {
			return llm.ProviderNVIDIANIM
		}
		if cfg.OpenAIKey != "" {
			return llm.ProviderOpenAI
		}
		if cfg.AnthropicKey != "" {
			return llm.ProviderAnthropic
		}
		return llm.ProviderOpenAI
	}
}

// resolveProviderID determines the provider from the API key prefix or hint.
func resolveProviderID(apiKey, hint string) llm.ProviderID {
	if hint != "" {
		hint = strings.ToLower(strings.ReplaceAll(hint, " ", "_"))
		if hint == "nvidia" || hint == "nvidia_nim" || hint == "nvidia nim" {
			return llm.ProviderNVIDIANIM
		}
		return llm.ProviderID(hint)
	}
	// Auto-detect from key prefix
	if info := llm.ProviderByKeyPrefix(apiKey); info != nil {
		return info.ID
	}
	return llm.ProviderOpenAI // default fallback
}

// createProvider instantiates a Provider from its ID and API key.
func createProvider(id llm.ProviderID, apiKey string) (llm.Provider, error) {
	switch id {
	case llm.ProviderOpenAI:
		return llm.NewOpenAI(apiKey), nil
	case llm.ProviderAnthropic:
		return llm.NewAnthropic(apiKey), nil
	case llm.ProviderGemini:
		p, err := llm.NewGemini(apiKey)
		if err != nil {
			return nil, err
		}
		return p, nil
	case llm.ProviderGroq:
		return llm.NewGroq(apiKey), nil
	case llm.ProviderMistral:
		return llm.NewMistral(apiKey), nil
	case llm.ProviderCohere:
		return llm.NewCohere(apiKey), nil
	case llm.ProviderNVIDIANIM:
		return llm.NewNVIDIANIM(apiKey), nil
	case llm.ProviderOpenRouter:
		return llm.NewOpenRouter(apiKey), nil
	case llm.ProviderDeepSeek:
		return llm.NewDeepSeek(apiKey), nil
	default:
		return llm.NewOpenAI(apiKey), nil
	}
}

// resolveProvider finds the right ProviderConfig for direct forwarding.
// Used when we don't want to go through ModelRouter (e.g., for streaming passthrough).
func (s *ProxyServer) resolveProvider(model, llmKey, hint string) *ProviderConfig {
	if llmKey != "" {
		providerID := resolveProviderID(llmKey, hint)
		return buildProviderConfig(providerID, model, llmKey)
	}
	return RouteRequest(model, &s.cfg)
}

func buildProviderConfig(id llm.ProviderID, model, apiKey string) *ProviderConfig {
	switch id {
	case llm.ProviderOpenAI:
		return &ProviderConfig{Name: "openai", BaseURL: "https://api.openai.com", APIKey: apiKey}
	case llm.ProviderAnthropic:
		return &ProviderConfig{Name: "anthropic", BaseURL: "https://api.anthropic.com", APIKey: apiKey}
	case llm.ProviderGemini:
		return &ProviderConfig{Name: "gemini", BaseURL: "https://generativelanguage.googleapis.com", APIKey: apiKey}
	case llm.ProviderGroq:
		return &ProviderConfig{Name: "groq", BaseURL: "https://api.groq.com", APIKey: apiKey}
	case llm.ProviderMistral:
		return &ProviderConfig{Name: "mistral", BaseURL: "https://api.mistral.ai", APIKey: apiKey}
	case llm.ProviderCohere:
		return &ProviderConfig{Name: "cohere", BaseURL: "https://api.cohere.com", APIKey: apiKey}
	case llm.ProviderNVIDIANIM:
		return &ProviderConfig{Name: "nvidia", BaseURL: "https://build.nvidia.com", APIKey: apiKey}
	case llm.ProviderOpenRouter:
		return &ProviderConfig{Name: "openrouter", BaseURL: "https://openrouter.ai", APIKey: apiKey}
	case llm.ProviderDeepSeek:
		return &ProviderConfig{Name: "deepseek", BaseURL: "https://api.deepseek.com", APIKey: apiKey}
	default:
		return &ProviderConfig{Name: "openai", BaseURL: "https://api.openai.com", APIKey: apiKey}
	}
}

func formatFloat(f float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", f), "0"), ".")
}

// recordUsage tracks per-key usage metrics.
func (s *ProxyServer) recordUsage(apiKey string, cost float64, tokens int, err bool) {
	if apiKey == "" {
		return
	}
	s.usageMu.Lock()
	defer s.usageMu.Unlock()
	usage, ok := s.usageByKey[apiKey]
	if !ok {
		usage = &KeyUsage{}
		s.usageByKey[apiKey] = usage
	}
	usage.RequestCount++
	usage.TotalCost += cost
	usage.TotalTokens += tokens
	usage.LastUsed = time.Now().Unix()
	if err {
		usage.ErrorCount++
	}
}

// getAPIKey extracts the API key from request context.
func getAPIKey(ctx context.Context) string {
	if v, ok := ctx.Value(apiKeyContextKey).(string); ok {
		return v
	}
	return ""
}

// usageCount returns the recorded request count for a key.
func (s *ProxyServer) usageCount(apiKey string) uint64 {
	s.usageMu.RLock()
	defer s.usageMu.RUnlock()
	if u, ok := s.usageByKey[apiKey]; ok {
		return u.RequestCount
	}
	return 0
}

// modelAllowed enforces the per-tenant model allowlist: exact match or prefix
// glob (e.g. "gpt-4o*"), comma-separated. Empty allowlist = allow all.
func (s *ProxyServer) modelAllowed(model string) bool {
	if s.cfg.AllowedModels == "" || model == "" {
		return true
	}
	for _, pat := range strings.Split(s.cfg.AllowedModels, ",") {
		pat = strings.TrimSpace(pat)
		if pat == "" {
			continue
		}
		if pat == "*" || pat == model {
			return true
		}
		if strings.HasSuffix(pat, "*") && strings.HasPrefix(model, strings.TrimSuffix(pat, "*")) {
			return true
		}
	}
	return false
}

// recordDecision counts verdict+policy outcomes for the /metrics dashboard.
func (s *ProxyServer) recordDecision(verdict string, policy PolicyDecision) {
	s.decisionMu.Lock()
	defer s.decisionMu.Unlock()
	s.decisionCount[verdict+":"+string(policy)]++
}

// ─────────────────────────────────────────────────────────────────────────────
// ANALYSIS ENDPOINTS
// ─────────────────────────────────────────────────────────────────────────────

// analyzeRequest represents a request to analyze code.
type analyzeRequest struct {
	Code     string `json:"code"`
	Language string `json:"language"`
	Model    string `json:"model,omitempty"`
}

// handleAnalyze runs dual-engine analysis (deterministic + LLM) on code.
func (s *ProxyServer) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
	var req analyzeRequest
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Code == "" {
		http.Error(w, `{"error":"code is required"}`, http.StatusBadRequest)
		return
	}
	if req.Language == "" {
		req.Language = "go"
	}

	llmKey := r.Header.Get("X-LLM-Key")
	modelRouter := s.buildRouter(llmKey, "")
	result := AnalyzeWithDualEngine(r.Context(), modelRouter, s.cfg.BackendURL, s.cfg.APIKey, llmKey, req.Code, req.Language)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"analysis":  result,
		"mode":      "dual-engine",
		"model":     req.Model,
		"timestamp": time.Now().Unix(),
	})
}

// handleDeepAnalyze runs the full dual-engine pipeline on code.
// This is the main endpoint for the middleware: deterministic + LLM engines in parallel.
func (s *ProxyServer) handleDeepAnalyze(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
	var req analyzeRequest
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Code == "" {
		http.Error(w, `{"error":"code is required"}`, http.StatusBadRequest)
		return
	}
	if req.Language == "" {
		req.Language = "go"
	}

	llmKey := r.Header.Get("X-LLM-Key")
	modelRouter := s.buildRouter(llmKey, "")
	result := AnalyzeWithDualEngine(r.Context(), modelRouter, s.cfg.BackendURL, s.cfg.APIKey, llmKey, req.Code, req.Language)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"result":    result,
		"mode":      "dual-engine",
		"passes":    2,
		"model":     req.Model,
		"timestamp": time.Now().Unix(),
	})
}

// ─────────────────────────────────────────────────────────────────────────────
// POLICY ENGINE
//
// The gateway's policy decision engine. The advisory verdict (pass/warn/block)
// is derived from severity + confidence; the policy DECISION is derived from
// the verdict under the requested enforcement mode. Decisions follow the
// spec's contract: allow | allow_with_notice | require_acknowledgement |
// hold_for_review | block | scanner_unavailable.
// ─────────────────────────────────────────────────────────────────────────────

// PolicyDecision is the release decision for a scanned generation.
type PolicyDecision string

const (
	PolicyAllow           PolicyDecision = "allow"
	PolicyAllowWithNotice PolicyDecision = "allow_with_notice"
	PolicyRequireAck      PolicyDecision = "require_acknowledgement"
	PolicyHoldForReview   PolicyDecision = "hold_for_review"
	PolicyBlock           PolicyDecision = "block"
	// PolicyScannerUnavailable is reserved for the fail-closed/fail-open
	// decision on scanner outages, which the spec defers to per-tenant policy
	// (Phase 3). Phase 1 always emits one of the decisions above.
	PolicyScannerUnavailable PolicyDecision = "scanner_unavailable"
)

// EnforcementMode selects how strictly the policy is applied.
type EnforcementMode string

const (
	// ModeObserve reports the verdict/policy but never restricts output. Use
	// for pilots and evaluation only (spec: "not enforcement").
	ModeObserve EnforcementMode = "observe"
	// ModeBalanced lets explanatory text flow, but code blocks/patches from a
	// held review are withheld until a human approves them.
	ModeBalanced EnforcementMode = "balanced"
	// ModeStrict releases nothing until the scan policy produces an allow
	// decision. Blocked generations return HTTP 451 with the evidence.
	ModeStrict EnforcementMode = "strict"
	// ModePassthrough skips analysis entirely (opt-out; audit trail omitted).
	ModePassthrough EnforcementMode = "passthrough"
)

// ComputePolicy maps the advisory verdict to a policy decision under a mode.
//
//	observe:  pass→allow, warn→allow_with_notice, block→hold_for_review (advisory)
//	balanced: pass→allow, warn→allow_with_notice, block→hold_for_review (code withheld)
//	strict:   pass→allow, warn→require_acknowledgement, block→block (nothing
//	          released; a warning is only released with an explicit
//	          acknowledgement in the client), scanners down→scanner_unavailable
func ComputePolicy(o AnalysisOutcome, mode EnforcementMode) PolicyDecision {
	if mode == ModePassthrough {
		return PolicyAllow
	}
	if o.ScannersUnavailable && mode == ModeStrict {
		// Fail closed: no output is released when the scanners could not run.
		return PolicyScannerUnavailable
	}
	switch o.Verdict {
	case "block":
		if mode == ModeStrict {
			return PolicyBlock
		}
		return PolicyHoldForReview
	case "warn":
		if mode == ModeStrict {
			return PolicyRequireAck
		}
		return PolicyAllowWithNotice
	default:
		return PolicyAllow
	}
}

// enforcePolicy decides whether content may be released and, when it may not,
// the HTTP status and reason for the block. Strict-mode blocks return HTTP 451
// (unavailable for legal reasons — the spec's "not released") and scanner
// outages fail closed with HTTP 503.
func enforcePolicy(policy PolicyDecision, mode EnforcementMode) (release bool, status int, reason string) {
	switch {
	case policy == PolicyBlock && mode == ModeStrict:
		return false, http.StatusUnavailableForLegalReasons, "blocked by VigilAgent policy: output withheld until the scan findings are resolved"
	case policy == PolicyScannerUnavailable && mode == ModeStrict:
		return false, http.StatusServiceUnavailable, "scanner unavailable: failing closed per policy — no output released"
	default:
		return true, 0, ""
	}
}

// codeFenceRe matches fenced code blocks (```lang\n...\n```).
var codeFenceRe = regexp.MustCompile("(?s)```[^`\n]*\n.*?```")

// redactCodeBlocks replaces fenced code blocks with a withheld notice. Used by
// balanced mode so prose flows but generated code is held for human review.
func redactCodeBlocks(content string) string {
	return codeFenceRe.ReplaceAllString(content, "[🛡️ code withheld by VigilAgent policy — review the scan findings before applying]")
}

// ─────────────────────────────────────────────────────────────────────────────
// DUAL-ENGINE ANALYSIS HELPER
// ─────────────────────────────────────────────────────────────────────────────

// analyzeAndVerdict runs dual-engine analysis on all code blocks in content and
// returns the merged advisory outcome, the findings, a summary (empty when
// nothing was found), and whether the LLM scanner degraded. outcome is nil when
// there is no code to analyze. When the LLM engine fails on every block AND no
// deterministic evidence exists, the outcome is marked scanners-unavailable so
// strict mode can fail closed.
func (s *ProxyServer) analyzeAndVerdict(ctx context.Context, modelRouter *llm.ModelRouter, llmKey, content string) (*AnalysisOutcome, []Finding, string, bool) {
	blocks := ExtractCodeBlocks(content)
	if len(blocks) == 0 {
		return nil, nil, "", false
	}

	var allFindings []Finding
	var llmErrCount int
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, block := range blocks {
		wg.Add(1)
		go func(b CodeBlock) {
			defer wg.Done()
			result := AnalyzeWithDualEngine(
				ctx,
				modelRouter,
				s.cfg.BackendURL,
				s.cfg.APIKey,
				llmKey,
				b.Code,
				b.Language,
			)
			if result == nil {
				return
			}
			mu.Lock()
			if len(result.Findings) > 0 {
				allFindings = append(allFindings, result.Findings...)
			}
			if result.EngineStats.LLM.Error != "" {
				llmErrCount++
			}
			mu.Unlock()
		}(block)
	}
	wg.Wait()

	unique := DeduplicateFindings(allFindings)
	outcome := ComputeVerdict(unique)
	if llmErrCount >= len(blocks) && len(unique) == 0 {
		outcome.ScannersUnavailable = true
	}
	summary := ""
	if len(unique) > 0 {
		summary = BuildSummary(unique, outcome.Score, outcome.Grade, outcome.Corroborated)
	}
	return &outcome, unique, summary, llmErrCount > 0
}

// ─────────────────────────────────────────────────────────────────────────────
// PROVENANCE & AUDIT SERVICE
// ─────────────────────────────────────────────────────────────────────────────

// resolveProvenanceSecret picks the signing secret: explicit config → proxy API
// key → dev-only constant. Records are always signed, never silently unsigned.
func resolveProvenanceSecret(cfg Config) string {
	if cfg.ProvenanceSecret != "" {
		return cfg.ProvenanceSecret
	}
	if cfg.APIKey != "" {
		return cfg.APIKey
	}
	return "vigilagent-dev-provenance-secret"
}

// provenanceStore is a bounded in-memory ring of signed provenance records.
// Production deployments should back this with durable storage; the ring keeps
// the gateway self-contained for local and manual testing.
type provenanceStore struct {
	mu      sync.RWMutex
	records map[string]signing.ProvenanceRecord
	sigs    map[string]string
	order   []string
	cap     int
}

func newProvenanceStore(capacity int) *provenanceStore {
	return &provenanceStore{
		records: make(map[string]signing.ProvenanceRecord),
		sigs:    make(map[string]string),
		cap:     capacity,
	}
}

func (p *provenanceStore) put(rec signing.ProvenanceRecord, sig string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, exists := p.records[rec.ScanID]; !exists {
		p.order = append(p.order, rec.ScanID)
	}
	p.records[rec.ScanID] = rec
	p.sigs[rec.ScanID] = sig
	for len(p.order) > p.cap {
		oldest := p.order[0]
		p.order = p.order[1:]
		delete(p.records, oldest)
		delete(p.sigs, oldest)
	}
}

func (p *provenanceStore) get(scanID string) (signing.ProvenanceRecord, string, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	rec, ok := p.records[scanID]
	if !ok {
		return signing.ProvenanceRecord{}, "", false
	}
	return rec, p.sigs[scanID], true
}

// newScanID generates a unique scan identifier for provenance records.
func newScanID(s *ProxyServer) string {
	return fmt.Sprintf("scan_%d_%d", time.Now().UnixNano(), atomic.AddUint64(&s.reqCount, 1))
}

// recordProvenance creates, signs, and stores the signed provenance record for
// an analyzed response. The record anchors the exact scanned output (content
// hash) and the policy decision, so any later tamper invalidates it. When
// scanID is empty a new one is generated (streaming callers generate it up
// front so the X-VigilAgent-Scan-ID header is available before the first byte).
func (s *ProxyServer) recordProvenance(r *http.Request, provider, model string, decision PolicyDecision, mode EnforcementMode, content, scanID string) (signing.ProvenanceRecord, string) {
	if scanID == "" {
		scanID = newScanID(s)
	}
	rec := signing.ProvenanceRecord{
		ScanID:           scanID,
		RequestID:        middleware.GetReqID(r.Context()),
		Provider:         provider,
		Model:            model,
		ClientType:       r.Header.Get("X-Client-Type"),
		ClientVersion:    r.Header.Get("X-Client-Version"),
		ProvenanceStatus: signing.ProvenanceVerified,
		ResponseHash:     signing.HashContent(content),
		Decision:         string(decision),
		Mode:             string(mode),
		Timestamp:        time.Now().UTC(),
	}
	sig, err := signing.SignProvenance(s.provenanceSecret, rec)
	if err != nil {
		slog.Warn("provenance signing failed", "error", err)
		sig = ""
	}
	if s.provenance != nil {
		s.provenance.put(rec, sig)
	}
	return rec, sig
}

// handleProvenanceGet returns a stored provenance record by scan ID.
// GET /v1/provenance?scan_id=...
func (s *ProxyServer) handleProvenanceGet(w http.ResponseWriter, r *http.Request) {
	scanID := r.URL.Query().Get("scan_id")
	if scanID == "" {
		http.Error(w, `{"error":"scan_id query parameter is required"}`, http.StatusBadRequest)
		return
	}
	rec, sig, ok := s.provenance.get(scanID)
	if !ok {
		http.Error(w, `{"error":"unknown scan_id"}`, http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"record": rec, "signature": sig})
}

// handleProvenanceVerify validates a provenance record's signature, either by
// submitting the full record+signature or a stored scan_id+signature.
// POST /v1/provenance/verify
func (s *ProxyServer) handleProvenanceVerify(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
	var body struct {
		ScanID    string                    `json:"scan_id"`
		Signature string                    `json:"signature"`
		Record    *signing.ProvenanceRecord `json:"record,omitempty"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}

	valid := false
	reason := ""
	switch {
	case body.Record != nil:
		if err := signing.VerifyProvenance(s.provenanceSecret, *body.Record, body.Signature); err == nil {
			valid = true
		} else {
			reason = err.Error()
		}
	case body.ScanID != "":
		rec, sig, ok := s.provenance.get(body.ScanID)
		if !ok {
			reason = "unknown scan_id"
		} else if sig == "" {
			reason = "record was not signed"
		} else if err := signing.VerifyProvenance(s.provenanceSecret, rec, body.Signature); err == nil {
			valid = true
		} else {
			reason = err.Error()
		}
	default:
		http.Error(w, `{"error":"provide scan_id+signature or a full record"}`, http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"valid": valid, "reason": reason})
}

// handleProvenanceAttest creates and signs a provenance record for content that
// was scanned outside the streaming gateway flow (e.g. MCP attestations).
// POST /v1/provenance/attest
func (s *ProxyServer) handleProvenanceAttest(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20) // 1MB
	var body struct {
		Provider      string `json:"provider"`
		Model         string `json:"model"`
		Decision      string `json:"decision"`
		ResponseHash  string `json:"response_hash"`
		ClientType    string `json:"client_type,omitempty"`
		ClientVersion string `json:"client_version,omitempty"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if body.Decision == "" || body.ResponseHash == "" {
		http.Error(w, `{"error":"decision and response_hash are required"}`, http.StatusBadRequest)
		return
	}
	rec := signing.ProvenanceRecord{
		ScanID:           newScanID(s),
		RequestID:        middleware.GetReqID(r.Context()),
		Provider:         body.Provider,
		Model:            body.Model,
		ClientType:       body.ClientType,
		ClientVersion:    body.ClientVersion,
		ProvenanceStatus: signing.ProvenanceVerified,
		ResponseHash:     body.ResponseHash,
		Decision:         body.Decision,
		Timestamp:        time.Now().UTC(),
	}
	sig, err := signing.SignProvenance(s.provenanceSecret, rec)
	if err != nil {
		slog.Warn("provenance signing failed", "error", err)
		sig = ""
	}
	s.provenance.put(rec, sig)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"record": rec, "signature": sig})
}

// ─────────────────────────────────────────────────────────────────────────────
// DESIGN-STAGE GATE
// ─────────────────────────────────────────────────────────────────────────────

// applyDesignGate scans the last user message (prompt / design document) with
// the deterministic engine BEFORE generation. When the design itself carries
// risks (hardcoded secrets, embedded commands, raw user-input SQL), policy-
// mandated secure constraints are appended to the provider request. The model
// does not get to decide whether a security requirement is mandatory — the
// policy engine does.
func (s *ProxyServer) applyDesignGate(task *llm.Task) ([]Finding, bool) {
	if task == nil || len(task.Messages) == 0 {
		return nil, false
	}
	last := task.Messages[len(task.Messages)-1]
	if last.Role != "user" {
		return nil, false
	}
	findings := (&DualEngineAnalyzer{}).localDeterministicScan(last.Content, "design")
	if len(findings) == 0 {
		return nil, false
	}
	var b strings.Builder
	b.WriteString("SECURE DESIGN CONSTRAINTS — policy-mandated by the design-stage scan of your request. ")
	b.WriteString("These requirements are NOT optional suggestions; the policy engine requires them. ")
	b.WriteString("Incorporate ALL of the following into your design and output:\n")
	for _, f := range findings {
		fix := f.Fix
		if fix == "" {
			fix = "address this requirement"
		}
		fmt.Fprintf(&b, "- [%s] %s Required fix: %s\n", strings.ToUpper(f.Severity), f.Message, fix)
	}
	task.Messages = append(task.Messages, llm.Message{Role: "system", Content: b.String()})
	return findings, true
}

// ─────────────────────────────────────────────────────────────────────────────
// OPENAI RESPONSES API (POST /v1/responses)
// ─────────────────────────────────────────────────────────────────────────────

// handleResponses implements the OpenAI Responses API surface on top of the
// same scan-and-release pipeline as /v1/chat/completions. Non-streaming for
// now; streaming callers should use chat completions.
func (s *ProxyServer) handleResponses(w http.ResponseWriter, r *http.Request) {
	const maxRequestBodySize = 10 << 20
	bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, maxRequestBodySize+1))
	if err != nil {
		http.Error(w, `{"error":"failed to read request body"}`, http.StatusBadRequest)
		return
	}
	if len(bodyBytes) > maxRequestBodySize {
		http.Error(w, `{"error":"request body too large (max 10MB)"}`, http.StatusRequestEntityTooLarge)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req struct {
		Model  string          `json:"model"`
		Stream bool            `json:"stream"`
		Input  json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if req.Stream {
		s.handleResponsesStream(w, r, bodyBytes, req.Model, req.Input)
		return
	}
	messages, err := parseResponsesInput(req.Input)
	if err != nil {
		http.Error(w, `{"error":"invalid input: must be a string or an array of {role, content} messages"}`, http.StatusBadRequest)
		return
	}

	// Reuse the exact chat-completions pipeline by translating the request,
	// then re-shape the response into the Responses API contract.
	chatBody, _ := json.Marshal(map[string]interface{}{"model": req.Model, "stream": false, "messages": messages})
	clone := r.Clone(r.Context())
	clone.Body = io.NopCloser(bytes.NewReader(chatBody))
	rec := httptest.NewRecorder()
	s.handleProxyRequest(rec, clone, "openai")

	// Copy the VigilAgent verdict/provenance headers through.
	for k, vals := range rec.Header() {
		if strings.HasPrefix(strings.ToLower(k), "x-vigilagent-") {
			for _, v := range vals {
				w.Header().Add(k, v)
			}
		}
	}
	w.Header().Set("Content-Type", "application/json")

	if rec.Code != http.StatusOK {
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
		return
	}

	var chat map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &chat); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"failed to translate gateway response"}`))
		return
	}

	content := ""
	if choices, ok := chat["choices"].([]interface{}); ok && len(choices) > 0 {
		if c0, ok := choices[0].(map[string]interface{}); ok {
			if msg, ok := c0["message"].(map[string]interface{}); ok {
				content, _ = msg["content"].(string)
			}
		}
	}

	usage := map[string]interface{}{"input_tokens": 0, "output_tokens": 0, "total_tokens": 0}
	if u, ok := chat["usage"].(map[string]interface{}); ok {
		usage = map[string]interface{}{
			"input_tokens":  u["prompt_tokens"],
			"output_tokens": u["completion_tokens"],
			"total_tokens":  u["total_tokens"],
		}
	}

	respObj := map[string]interface{}{
		"id":         chat["id"],
		"object":     "response",
		"created_at": chat["created"],
		"model":      chat["model"],
		"status":     "completed", "output": []map[string]interface{}{
			{
				"id":     "msg_" + strings.TrimPrefix(fmt.Sprintf("%v", chat["id"]), "chatcmpl-vigil-"),
				"type":   "message",
				"role":   "assistant",
				"status": "completed",
				"content": []map[string]interface{}{
					{"type": "output_text", "text": content, "annotations": []interface{}{}},
				},
			}},
		"usage": usage,
	}
	if vg, ok := chat["vigilagent"]; ok {
		respObj["vigilagent"] = vg
	}
	if dg, ok := chat["design_gate"]; ok {
		respObj["design_gate"] = dg
	}
	if p, ok := chat["provenance"].(map[string]interface{}); ok {
		respObj["provenance"] = p
	}
	json.NewEncoder(w).Encode(respObj)
}

// parseResponsesInput converts the OpenAI Responses API `input` field (a string
// or an array of {role, content} messages) into the canonical message list.
func parseResponsesInput(input json.RawMessage) ([]llm.Message, error) {
	if len(input) == 0 {
		return nil, fmt.Errorf("empty input")
	}
	var asString string
	if err := json.Unmarshal(input, &asString); err == nil {
		return []llm.Message{{Role: "user", Content: asString}}, nil
	}
	var msgs []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(input, &msgs); err != nil {
		return nil, err
	}
	if len(msgs) == 0 {
		return nil, fmt.Errorf("empty input")
	}
	out := make([]llm.Message, len(msgs))
	for i, m := range msgs {
		out[i] = llm.Message{Role: m.Role, Content: m.Content}
	}
	return out, nil
}
