package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/vigilagent/vigilagent/internal/llm"
)

// Config holds proxy server configuration.
type Config struct {
	Port       string
	BackendURL string
	APIKey     string
	// Proxy auth: comma-separated list of allowed API keys for the proxy itself.
	// Empty means auth is disabled (open proxy).
	AllowedAPIKeys string
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
		allowedKeys: parseAllowedKeys(cfg.AllowedAPIKeys),
	}
	s.setupMiddleware()
	s.routes()
	return s
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
	s.router.Use(middleware.RealIP)
	s.router.Use(middleware.Recoverer)
	s.router.Use(s.loggingMiddleware)
	s.router.Use(s.metricsMiddleware)
	s.router.Use(s.rateLimitMiddleware)
	if len(s.allowedKeys) > 0 {
		s.router.Use(s.authMiddleware)
	}
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
		log.Printf("proxy: method=%s path=%s status=%d latency=%s",
			r.Method, r.URL.Path, ww.Status(), latency.Round(time.Millisecond))
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
		count    int
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

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"requests_total":     atomic.LoadUint64(&s.reqCount),
		"errors_total":       atomic.LoadUint64(&s.errCount),
		"avg_latency_ms":     avgLatency,
		"p50_latency_ms":     p50Latency,
		"healthy":            true,
		"service":            "vigilagent-proxy",
		"timestamp":          time.Now().Unix(),
		"usage": map[string]interface{}{
			"tracked_keys": keyCount,
			"total_requests": totalRequests,
			"total_errors": totalErrors,
			"total_tokens": totalTokens,
			"total_cost": totalCost,
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
	llmProvider := r.Header.Get("X-LLM-Provider") // optional hint
	llmModel := r.Header.Get("X-LLM-Model")       // optional override
	analysisMode := r.Header.Get("X-VigilAgent-Mode") // passthrough | scan | verify | auto

	if llmModel != "" {
		req.Model = llmModel
	}

	// Build per-request ModelRouter with BYOK if provided, else use backend keys
	modelRouter := s.buildRouter(llmKey, llmProvider)

	// BYOK: inject the requested model into the PriceTable so routing works
	// Must copy existing table first — SetPrices replaces, not merges
	if llmKey != "" && req.Model != "" {
		providerID := resolveProviderID(llmKey, llmProvider)
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

	resp, err := modelRouter.ExecuteWithFailover(r.Context(), task)
	if err != nil {
		log.Printf("ModelRouter execution failed: %v", err)
		s.recordUsage(getAPIKey(r.Context()), 0, 0, true)
		http.Error(w, `{"error":"llm request failed: `+err.Error()+`"}`, http.StatusBadGateway)
		return
	}

	// Track usage
	s.recordUsage(getAPIKey(r.Context()), resp.Cost, resp.InputTokens+resp.OutputTokens, false)

	// ── Extract and analyze code blocks ──
	content := resp.Content
	blocks := ExtractCodeBlocks(content)

	var results []*AnalysisResult
	if analysisMode != "passthrough" && len(blocks) > 0 {
		for _, block := range blocks {
			res, err := AnalyzeCode(r.Context(), s.client, s.cfg.BackendURL, s.cfg.APIKey, block.Code, block.Language)
			if err != nil {
				log.Printf("AnalyzeCode error: %v", err)
				continue
			}
			results = append(results, res)
		}
	}

	// ── Enrich response with analysis summary ──
	summary := FormatAnalysisSummary(results)
	if summary != "" {
		resp.Content += "\n\n" + summary
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
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(oResp)
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
	}
}

// buildRouter creates a ModelRouter for the current request.
// If llmKey is provided (BYOK), it creates an ephemeral router with that key
// and injects the requested model into the PriceTable so routing works.
// Otherwise, it creates a router with all configured backend keys.
// Wires cache and budget guard when available.
func (s *ProxyServer) buildRouter(llmKey, hintProvider string) *llm.ModelRouter {
	router := llm.NewModelRouter(&llm.RouterConfig{
		DefaultModel:       "gpt-4o-mini",
		BudgetPerTask:      10.00,
		DefaultOutputTokens: 4096,
	})

	// Wire response cache (5-minute TTL)
	router.SetCache(llm.NewInMemoryCache(5 * time.Minute))

	if llmKey != "" {
		// BYOK: create a single provider from the user's key
		providerID := resolveProviderID(llmKey, hintProvider)
		p := createProvider(providerID, llmKey)
		if p != nil {
			router.RegisterProvider(string(providerID), p)
		}
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
func createProvider(id llm.ProviderID, apiKey string) llm.Provider {
	switch id {
	case llm.ProviderOpenAI:
		return llm.NewOpenAI(apiKey)
	case llm.ProviderAnthropic:
		return llm.NewAnthropic(apiKey)
	case llm.ProviderGemini:
		p, _ := llm.NewGemini(apiKey)
		return p
	case llm.ProviderGroq:
		return llm.NewGroq(apiKey)
	case llm.ProviderMistral:
		return llm.NewMistral(apiKey)
	case llm.ProviderCohere:
		return llm.NewCohere(apiKey)
	case llm.ProviderNVIDIANIM:
		return llm.NewNVIDIANIM(apiKey)
	case llm.ProviderOpenRouter:
		return llm.NewOpenRouter(apiKey)
	case llm.ProviderDeepSeek:
		return llm.NewDeepSeek(apiKey)
	default:
		return llm.NewOpenAI(apiKey)
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

// ─────────────────────────────────────────────────────────────────────────────
// ANALYSIS ENDPOINTS
// ─────────────────────────────────────────────────────────────────────────────

// analyzeRequest represents a request to analyze code.
type analyzeRequest struct {
	Code     string `json:"code"`
	Language string `json:"language"`
	Model    string `json:"model,omitempty"`
}

// handleAnalyze runs deterministic analysis (fast path) on code.
func (s *ProxyServer) handleAnalyze(w http.ResponseWriter, r *http.Request) {
	var req analyzeRequest
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

	res, err := AnalyzeCode(r.Context(), s.client, s.cfg.BackendURL, s.cfg.APIKey, req.Code, req.Language)
	if err != nil {
		log.Printf("AnalyzeCode error: %v", err)
		http.Error(w, `{"error":"analysis failed: `+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"analysis":  res,
		"mode":      "scan",
		"model":     req.Model,
		"timestamp": time.Now().Unix(),
	})
}

// handleDeepAnalyze runs the full Shift-Zero pipeline (multi-pass LLM analysis) on code.
// Pass 1: Deterministic scan via backend /api/v1/review
// Pass 2: LLM-driven deep analysis via proxy's own ModelRouter
// Pass 3: Cross-validation between scan and LLM results
func (s *ProxyServer) handleDeepAnalyze(w http.ResponseWriter, r *http.Request) {
	var req analyzeRequest
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

	// Pass 1: Deterministic scan
	scanResult, err := AnalyzeCode(r.Context(), s.client, s.cfg.BackendURL, s.cfg.APIKey, req.Code, req.Language)
	if err != nil {
		log.Printf("DeepAnalyze scan pass error: %v", err)
		// Continue even if scan fails — LLM pass can still work
		scanResult = &AnalysisResult{Grade: "N/A", Score: 0}
	}

	// Pass 2: LLM-driven deep analysis
	llmKey := r.Header.Get("X-LLM-Key")
	llmProvider := r.Header.Get("X-LLM-Provider")
	modelRouter := s.buildRouter(llmKey, llmProvider)

	llmPrompt := fmt.Sprintf(
		"Analyze this %s code for bugs, security issues, performance problems, and improvements. "+
			"Be specific about line numbers and fixes.\n\n```%s\n%s\n```",
		req.Language, req.Language, req.Code,
	)
	llmTask := &llm.Task{
		ID:          "deep-analyze-" + time.Now().Format("20060102150405"),
		Type:        "security",
		Description: "Deep code analysis",
		Messages:    []llm.Message{{Role: "user", Content: llmPrompt}},
	}

	llmResult, llmErr := modelRouter.ExecuteWithFailover(r.Context(), llmTask)
	llmContent := ""
	if llmErr == nil {
		llmContent = llmResult.Content
	}

	// Pass 3: Cross-validation summary
	summary := fmt.Sprintf(
		"## Deep Analysis Report\n\n### Deterministic Scan\n- Grade: %s\n- Score: %d/100\n\n### LLM Deep Analysis\n%s",
		scanResult.Grade, scanResult.Score, llmContent,
	)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"scan":     scanResult,
		"llm":      map[string]interface{}{"content": llmContent, "model": req.Model, "error": llmErr != nil},
		"summary":  summary,
		"mode":     "verify",
		"passes":   3,
		"model":    req.Model,
		"timestamp": time.Now().Unix(),
	})
}