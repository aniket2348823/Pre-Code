package proxy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestExtractCodeBlocks(t *testing.T) {
	text := "Here is some code:\n```go\nfmt.Println(\"Hello\")\n```\nAnd more:\n```python\nprint(\"Hi\")\n```"
	blocks := ExtractCodeBlocks(text)
	if len(blocks) != 2 {
		t.Fatalf("Expected 2 blocks, got %d", len(blocks))
	}
	if blocks[0].Language != "go" {
		t.Errorf("Expected go, got %s", blocks[0].Language)
	}
	if !strings.Contains(blocks[0].Code, "fmt.Println") {
		t.Errorf("Expected code to contain fmt.Println, got %s", blocks[0].Code)
	}
}

func TestFormatAnalysisSummary(t *testing.T) {
	res := []*AnalysisResult{
		{
			Grade:          "✅ Grade A",
			Score:          95,
			CriticalIssues: 0,
			Suggestions:    2,
			Reviewers: map[string]string{
				"Security":     "✅",
				"Architecture": "✅",
				"Performance":  "⚠️",
			},
		},
	}
	summary := FormatAnalysisSummary(res)
	if !strings.Contains(summary, "✅ Grade A") {
		t.Errorf("Expected summary to contain grade, got %s", summary)
	}
	if !strings.Contains(summary, "Security ✅") {
		t.Errorf("Expected summary to contain reviewers, got %s", summary)
	}
}

func TestProviderRouting(t *testing.T) {
	cfg := &Config{OpenAIKey: "test-openai", AnthropicKey: "test-anthropic"}
	p := RouteRequest("gpt-4o", cfg)
	if p == nil || p.Name != "openai" {
		t.Errorf("Expected openai provider")
	}

	p = RouteRequest("claude-3-5-sonnet", cfg)
	if p == nil || p.Name != "anthropic" {
		t.Errorf("Expected anthropic provider")
	}
}

func TestProxyHandler(t *testing.T) {
	// Stub for proxy testing
}

func TestStreamingProxy(t *testing.T) {
	// Stub for streaming proxy testing
}

func TestAuthMiddleware_AllowedKeys(t *testing.T) {
	cfg := Config{
		Port:           "0",
		BackendURL:     "http://localhost:9999",
		APIKey:         "test-backend",
		AllowedAPIKeys: "key1,key2,key3",
	}
	server := NewServer(cfg)

	// Request without API key should get 401
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 without API key, got %d", w.Code)
	}

	// Request with invalid key should get 401
	req = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "wrong-key")
	w = httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 with wrong key, got %d", w.Code)
	}

	// Request with valid key should pass auth (will fail at LLM but not 401)
	req = httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", "key1")
	w = httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Errorf("Expected non-401 with valid key, got %d", w.Code)
	}
}

func TestAuthMiddleware_BearerToken(t *testing.T) {
	cfg := Config{
		Port:           "0",
		BackendURL:     "http://localhost:9999",
		APIKey:         "test-backend",
		AllowedAPIKeys: "my-secret-key",
	}
	server := NewServer(cfg)

	// Bearer token auth
	req := httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer my-secret-key")
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)

	if w.Code == http.StatusUnauthorized {
		t.Errorf("Expected non-401 with valid Bearer token, got %d", w.Code)
	}
}

func TestAuthMiddleware_HealthSkipsAuth(t *testing.T) {
	cfg := Config{
		Port:           "0",
		BackendURL:     "http://localhost:9999",
		APIKey:         "test-backend",
		AllowedAPIKeys: "key1",
	}
	server := NewServer(cfg)

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("Expected 200 for health (no auth), got %d", w.Code)
	}
}

func TestAuthMiddleware_NoKeysRejectsAll(t *testing.T) {
	cfg := Config{
		Port:           "0",
		BackendURL:     "http://localhost:9999",
		APIKey:         "",
		AllowedAPIKeys: "", // no keys configured
	}
	server := NewServer(cfg)

	req := httptest.NewRequest("GET", "/v1/providers", nil)
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)

	// Fail closed: with no allowed keys configured, every request is rejected
	// rather than opening an unauthenticated proxy.
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 with no keys configured, got %d", w.Code)
	}
}

func TestAuthMiddleware_APIFallsBackToAPIKey(t *testing.T) {
	// When only APIKey is configured, it becomes the accepted credential.
	cfg := Config{
		Port:       "0",
		BackendURL: "http://localhost:9999",
		APIKey:     "test-backend",
	}
	server := NewServer(cfg)

	// Without the key → 401
	req := httptest.NewRequest("GET", "/v1/providers", nil)
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 without key, got %d", w.Code)
	}

	// With the fallback key → 200
	req2 := httptest.NewRequest("GET", "/v1/providers", nil)
	req2.Header.Set("X-API-Key", "test-backend")
	w2 := httptest.NewRecorder()
	server.Router().ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("Expected 200 with fallback key, got %d", w2.Code)
	}
}

func TestUsageTracking(t *testing.T) {
	cfg := Config{
		Port:       "0",
		BackendURL: "http://localhost:9999",
		APIKey:     "test-backend",
	}
	server := NewServer(cfg)

	// Record some usage
	server.recordUsage("test-key", 0.05, 100, false)
	server.recordUsage("test-key", 0.03, 80, false)
	server.recordUsage("test-key", 0, 0, true) // error

	server.usageMu.RLock()
	usage := server.usageByKey["test-key"]
	server.usageMu.RUnlock()

	if usage.RequestCount != 3 {
		t.Errorf("Expected 3 requests, got %d", usage.RequestCount)
	}
	if usage.TotalCost != 0.08 {
		t.Errorf("Expected cost 0.08, got %f", usage.TotalCost)
	}
	if usage.TotalTokens != 180 {
		t.Errorf("Expected 180 tokens, got %d", usage.TotalTokens)
	}
	if usage.ErrorCount != 1 {
		t.Errorf("Expected 1 error, got %d", usage.ErrorCount)
	}
}

func TestUsageEndpoint(t *testing.T) {
	cfg := Config{
		Port:       "0",
		BackendURL: "http://localhost:9999",
		APIKey:     "test-backend",
	}
	server := NewServer(cfg)
	server.recordUsage("key-a", 0.10, 200, false)
	server.recordUsage("key-b", 0.05, 100, false)

	req := httptest.NewRequest("GET", "/v1/usage", nil)
	req.Header.Set("X-API-Key", cfg.APIKey) // proxy is fail-closed: authenticated request
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	count := int(resp["count"].(float64))
	if count != 2 {
		t.Errorf("Expected 2 tracked keys, got %d", count)
	}
}

func TestMetricsEndpoint(t *testing.T) {
	cfg := Config{
		Port:       "0",
		BackendURL: "http://localhost:9999",
		APIKey:     "test-backend",
	}
	server := NewServer(cfg)
	server.recordUsage("key1", 0.10, 50, false)

	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	server.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("Expected 200, got %d", w.Code)
	}

	var resp map[string]interface{}
	json.NewDecoder(w.Body).Decode(&resp)

	usage := resp["usage"].(map[string]interface{})
	totalCost := usage["total_cost"].(float64)
	if totalCost != 0.10 {
		t.Errorf("Expected total cost 0.10, got %f", totalCost)
	}
}

func TestParseAllowedKeys(t *testing.T) {
	keys := parseAllowedKeys("key1,key2,key3")
	if len(keys) != 3 {
		t.Errorf("Expected 3 keys, got %d", len(keys))
	}
	if _, ok := keys["key2"]; !ok {
		t.Error("Expected key2 in set")
	}

	keys = parseAllowedKeys("")
	if keys != nil {
		t.Error("Expected nil for empty string")
	}

	keys = parseAllowedKeys(" , , ")
	if keys != nil {
		t.Error("Expected nil for whitespace-only string")
	}
}

func TestBuildRouter_CacheWired(t *testing.T) {
	cfg := Config{
		Port:       "0",
		BackendURL: "http://localhost:9999",
		APIKey:     "test-backend",
		OpenAIKey:  "sk-test",
	}
	server := NewServer(cfg)

	router := server.buildRouter("", "")
	if router == nil {
		t.Fatal("Expected non-nil router")
	}
	// Verify router is functional by attempting a route (cache should not panic)
	_ = router.GetHealthMonitor()
}

func TestBuildRouter_BYOK(t *testing.T) {
	cfg := Config{
		Port:       "0",
		BackendURL: "http://localhost:9999",
		APIKey:     "test-backend",
	}
	server := NewServer(cfg)

	router := server.buildRouter("sk-test-openai", "")
	if router == nil {
		t.Fatal("Expected non-nil router")
	}
	// Verify router is functional with health monitor
	hm := router.GetHealthMonitor()
	if hm == nil {
		t.Error("Expected non-nil health monitor")
	}
}
