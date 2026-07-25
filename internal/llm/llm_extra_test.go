package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// --- Health Check: CheckHealth covers success + failure paths ---

func TestCheckHealth_Success(t *testing.T) {
	mon := NewHealthMonitor()
	mon.RegisterProvider("good", &countingProvider{name: "good", resp: &ChatResponse{Content: "ok"}})
	mon.CheckHealth(context.Background(), "good")
	h := mon.Confidence("good")
	if h < 0.5 {
		t.Errorf("expected confidence > 0.5 after success, got %f", h)
	}
}

func TestCheckHealth_Failure(t *testing.T) {
	mon := NewHealthMonitor()
	mon.RegisterProvider("bad", &errProvider{name: "bad"})
	mon.CheckHealth(context.Background(), "bad")
	h := mon.Confidence("bad")
	if h > 0.3 {
		t.Errorf("expected low confidence after failure, got %f", h)
	}
}

func TestCheckHealth_UnknownProvider(t *testing.T) {
	mon := NewHealthMonitor()
	// Should not panic on unknown provider
	mon.CheckHealth(context.Background(), "unknown")
}

type errProvider struct {
	name string
}

func (p *errProvider) Name() string                          { return p.name }
func (p *errProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return nil, fmt.Errorf("fail")
}
func (p *errProvider) Stream(_ context.Context, _ *ChatRequest) (<-chan *ChatChunk, error) {
	return nil, fmt.Errorf("fail")
}
func (p *errProvider) HealthCheck(_ context.Context) error { return fmt.Errorf("health check failed") }

// --- Health: error rate exceeding 1.0 boundary ---

func TestRecordFailure_ErrorRateMaxed(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	// Push error rate close to 1.0
	for i := 0; i < 12; i++ {
		hm.RecordFailure("p")
	}
	hm.mu.RLock()
	health := hm.providers["p"]
	rate := health.ErrorRate
	hm.mu.RUnlock()
	if rate != 1.0 {
		t.Errorf("error rate should be capped at 1.0, got %f", rate)
	}
}

func TestRecordSuccess_ErrorRateMin(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	hm.RecordSuccess("p", time.Millisecond)
	hm.mu.RLock()
	health := hm.providers["p"]
	rate := health.ErrorRate
	hm.mu.RUnlock()
	if rate != 0 {
		t.Errorf("error rate should be 0 after success, got %f", rate)
	}
}

func TestRecordFailure_UnknownProvider(t *testing.T) {
	hm := NewHealthMonitor()
	// Should not panic
	hm.RecordFailure("unknown")
}

func TestRecordSuccess_UnknownProvider(t *testing.T) {
	hm := NewHealthMonitor()
	// Should not panic
	hm.RecordSuccess("unknown", time.Millisecond)
}

// --- Health: Confidence states ---

func TestConfidence_Degraded(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.mu.RLock()
	hm.providers["p"].Status = StatusDegraded
	hm.mu.RUnlock()
	c := hm.Confidence("p")
	if c < 0.3 || c > 0.8 {
		t.Errorf("degraded confidence = %f, want [0.3, 0.8]", c)
	}
}

func TestConfidence_Unhealthy(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	c := hm.Confidence("p")
	if c != 0.2 {
		t.Errorf("unhealthy confidence = %f, want 0.2", c)
	}
}

func TestConfidence_Down(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	c := hm.Confidence("p")
	if c != 0.0 {
		t.Errorf("down confidence = %f, want 0.0", c)
	}
}

// --- ExecuteWithFailover: routing failure ---

func TestExecuteWithFailover_RoutingFailure(t *testing.T) {
	r := NewModelRouter(nil)
	// No providers registered → Route fails → ExecuteWithFailover fails
	_, err := r.ExecuteWithFailover(context.Background(), simpleTask())
	if err == nil {
		t.Error("expected routing failure error")
	}
	if !strings.Contains(err.Error(), "routing failed") {
		t.Errorf("error should mention routing: %v", err)
	}
}

// --- StreamWithFailover: routing failure ---

func TestStreamWithFailover_RoutingFailure(t *testing.T) {
	r := NewModelRouter(nil)
	_, err := r.StreamWithFailover(context.Background(), simpleTask())
	if err == nil {
		t.Error("expected routing failure error")
	}
	if !strings.Contains(err.Error(), "routing failed") {
		t.Errorf("error should mention routing: %v", err)
	}
}

// --- StreamWithFailover: provider circuit breaker open ---

func TestStreamWithFailover_CircuitOpen(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	cb := NewCircuitBreaker(2, time.Hour)
	r.circuitBreakers["openai"] = cb
	cb.Execute(func() error { return fmt.Errorf("fail") })
	cb.Execute(func() error { return fmt.Errorf("fail") })

	_, err := r.StreamWithFailover(context.Background(), simpleTask())
	if err == nil {
		t.Fatal("expected error with circuit breaker open")
	}
}

// --- ExecuteWithFailover: circuit breaker open ---

func TestExecuteWithFailover_CircuitOpen(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai", resp: &ChatResponse{Content: "ok", Cost: 0.01}})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	cb := NewCircuitBreaker(2, time.Hour)
	r.circuitBreakers["openai"] = cb
	cb.Execute(func() error { return fmt.Errorf("fail") })
	cb.Execute(func() error { return fmt.Errorf("fail") })

	_, err := r.ExecuteWithFailover(context.Background(), simpleTask())
	if err == nil {
		t.Fatal("expected error with circuit breaker open")
	}
}

// --- attempt: budget blocks, records cost ---

func TestAttempt_BudgetBlocks(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai", resp: &ChatResponse{Content: "ok", Cost: 0.01}})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	r.SetBudgetGuard(&fakeBudget{reject: fmt.Errorf("over budget")})

	_, err := r.attempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err == nil {
		t.Fatal("expected budget error")
	}
	if !strings.Contains(err.Error(), "over budget") {
		t.Errorf("error should mention budget: %v", err)
	}
}

func TestAttempt_RecordsCostAndCache(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai", resp: &ChatResponse{Content: "ok", Cost: 0.05}})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	b := &fakeBudget{}
	r.SetBudgetGuard(b)
	r.SetCache(NewInMemoryCache(time.Minute))

	_, err := r.attempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.recorded != 0.05 {
		t.Errorf("expected recorded cost 0.05, got %v", b.recorded)
	}
	// Second attempt should hit cache
	prov := r.providers["openai"].(*countingProvider)
	_, err = r.attempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prov.calls != 1 {
		t.Errorf("expected 1 provider call (cache hit), got %d", prov.calls)
	}
}

// --- attempt: provider Chat error records failure ---

func TestAttempt_ProviderErrorRecordsFailure(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai", err: fmt.Errorf("chat error")})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	_, err := r.attempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	h := r.healthMonitor.Confidence("openai")
	if h > 0.5 {
		t.Errorf("confidence should drop after error, got %f", h)
	}
}

// --- streamAttempt: provider error records failure ---

func TestStreamAttempt_ProviderErrorRecordsFailure(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &errStreamProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	_, err := r.streamAttempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	h := r.healthMonitor.Confidence("openai")
	if h > 0.5 {
		t.Errorf("confidence should drop after error, got %f", h)
	}
}

// --- systemPrompt: all task types ---

func TestSystemPrompt_AllTypes(t *testing.T) {
	types := []string{"bug_fix", "feature", "refactoring", "security", "architecture", "other", ""}
	for _, tt := range types {
		p := systemPrompt(&Task{Type: tt})
		if p == "" {
			t.Errorf("systemPrompt(%q) returned empty", tt)
		}
	}
}

// --- GetHealthMonitor returns non-nil ---

func TestGetHealthMonitor_NonNil(t *testing.T) {
	r := NewModelRouter(nil)
	hm := r.GetHealthMonitor()
	if hm == nil {
		t.Fatal("expected non-nil health monitor")
	}
}

// --- CalculateOpenRouterCost with known model ---

func TestCalculateOpenRouterCost_KnownModel(t *testing.T) {
	cost := calculateOpenRouterCost("gpt-4o", 1000, 500)
	if cost <= 0 {
		t.Errorf("expected positive cost for known model, got %f", cost)
	}
}

// --- BuildCohereMessages edge cases ---

func TestBuildCohereMessages_WithSystem(t *testing.T) {
	msgs := buildCohereMessages("be helpful", []Message{{Role: "user", Content: "hi"}})
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("expected system role, got %q", msgs[0].Role)
	}
}

func TestBuildCohereMessages_NoSystem(t *testing.T) {
	msgs := buildCohereMessages("", []Message{{Role: "user", Content: "hi"}})
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
}

// --- Gemini: buildGeminiContents with non-assistant role ---

func TestBuildGeminiContents_AllRoles(t *testing.T) {
	contents := buildGeminiContents([]Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", Content: "bye"},
		{Role: "system", Content: "be helpful"},
	})
	if len(contents) != 4 {
		t.Fatalf("expected 4, got %d", len(contents))
	}
	// "system" role should be mapped to "user" (default)
	if contents[3].Role != "user" {
		t.Errorf("expected role 'user' for system, got %q", contents[3].Role)
	}
}

// --- Models: ProviderByKeyPrefix edge cases ---

func TestProviderByKeyPrefix_SkAnt(t *testing.T) {
	p := ProviderByKeyPrefix("sk-ant-xxx")
	if p == nil {
		t.Fatal("expected to find anthropic by prefix")
	}
	if p.ID != ProviderAnthropic {
		t.Errorf("expected anthropic, got %v", p.ID)
	}
}

func TestProviderByKeyPrefix_SkOr(t *testing.T) {
	p := ProviderByKeyPrefix("sk-or-xxx")
	if p == nil {
		t.Fatal("expected to find openrouter by prefix")
	}
	if p.ID != ProviderOpenRouter {
		t.Errorf("expected openrouter, got %v", p.ID)
	}
}

// --- EstimateInputTokens with various sizes ---

func TestEstimateInputTokens_LargeMessages(t *testing.T) {
	longContent := strings.Repeat("a", 400)
	task := &Task{Messages: []Message{{Role: "user", Content: longContent}}}
	tokens := estimateInputTokens(task)
	if tokens != 100 {
		t.Errorf("expected 100 tokens for 400 chars, got %d", tokens)
	}
}

func TestEstimateInputTokens_MultipleMessages(t *testing.T) {
	task := &Task{Messages: []Message{
		{Role: "user", Content: strings.Repeat("a", 200)},
		{Role: "assistant", Content: strings.Repeat("b", 200)},
	}}
	tokens := estimateInputTokens(task)
	if tokens != 100 {
		t.Errorf("expected 100 tokens for 400 chars, got %d", tokens)
	}
}

// --- ClassifyComplexity: IsNovel only ---

func TestClassifyComplexity_IsNovel(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{Type: "bug_fix", IsNovel: true}
	c := r.classifyComplexity(task)
	// bug_fix = 0.3, isNovel = 0.15 → 0.45
	if float64(c) < 0.35 || float64(c) > 0.55 {
		t.Errorf("novel complexity = %f, want ~0.45", c)
	}
}

// --- ClassifyComplexity: RequiresReasoning only ---

func TestClassifyComplexity_RequiresReasoning(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{Type: "bug_fix", RequiresReasoning: true}
	c := r.classifyComplexity(task)
	// bug_fix = 0.3, reasoning = 0.2 → 0.5
	if float64(c) < 0.4 || float64(c) > 0.6 {
		t.Errorf("reasoning complexity = %f, want ~0.5", c)
	}
}

// --- rankCandidates with capability requirement ---

func TestRankCandidates_CapabilityFilter(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	task := &Task{
		Type:                 "refactoring",
		RequiredCapabilities: []string{"reasoning"},
		Messages:             []Message{{Role: "user", Content: "complex refactoring"}},
	}
	candidates := r.rankCandidates(task, []string{"openai"}, ComplexityModerate)
	// gpt-4o doesn't have reasoning, so should filter it out for moderate tier
	// But deepseek-r1 is in complex tier not moderate
	// claude-sonnet-4-20250514 also lacks reasoning → should get filtered
	for _, c := range candidates {
		info, _ := LookupPrice(c.Model)
		if !info.Supports("reasoning") {
			t.Errorf("candidate %s doesn't have reasoning capability", c.Model)
		}
	}
}

// --- DeepSeek: stream with finish_reason in chunk ---

func TestDeepSeek_Stream_FinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		finish := "stop"
		ev := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"delta": map[string]interface{}{"content": "done"}, "finish_reason": finish},
			},
		}
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "data: %s\n\n", b)
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	ch, err := d.Stream(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var content string
	var gotFinish bool
	for chunk := range ch {
		content += chunk.Content
		if chunk.Finish {
			gotFinish = true
			break
		}
	}
	if content != "done" {
		t.Errorf("wrong content: %q", content)
	}
	if !gotFinish {
		t.Error("expected finish")
	}
}

// --- DeepSeek: stream with non-EOF error ---

func TestDeepSeek_Stream_NonEOFError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Send partial data then close abruptly
		w.Write([]byte("data: {"))
		w.(http.Flusher).Flush()
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	ch, err := d.Stream(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
}

// --- DeepSeek: health check network error ---

func TestDeepSeek_HealthCheck_NetworkError(t *testing.T) {
	d := &DeepSeekAdapter{
		apiKey:     "key",
		httpClient: &http.Client{Timeout: 1 * time.Nanosecond},
		baseURL:    "http://127.0.0.1:1",
	}
	err := d.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- DeepSeek: chat request creation error ---

func TestDeepSeek_Chat_NetworkError(t *testing.T) {
	d := &DeepSeekAdapter{
		apiKey:     "key",
		httpClient: &http.Client{Timeout: 1 * time.Nanosecond},
		baseURL:    "http://127.0.0.1:1",
	}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- DeepSeek: stream request creation error ---

func TestDeepSeek_Stream_NetworkError(t *testing.T) {
	d := &DeepSeekAdapter{
		apiKey:     "key",
		httpClient: &http.Client{Timeout: 1 * time.Nanosecond},
		baseURL:    "http://127.0.0.1:1",
	}
	_, err := d.Stream(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- OpenAI Stream with empty error (EOF) ---

func TestOpenAI_Stream_EOF(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// No data, just close
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	ch, err := o.Stream(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should get a finish chunk
	select {
	case chunk := <-ch:
		if !chunk.Finish {
			t.Error("expected finish on EOF")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for finish")
	}
}

// --- Anthropic: health check network error ---

func TestAnthropic_HealthCheck_NetworkError(t *testing.T) {
	a := &AnthropicAdapter{
		apiKey:   "key",
		httpAddr: "http://127.0.0.1:1",
		client:   &http.Client{Timeout: 1 * time.Nanosecond},
	}
	err := a.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Groq: health check success via server ---

func TestGroq_HealthCheck_Success_HTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	g := &GroqAdapter{
		apiKey:     "k",
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	// Override the URL by using the mock HTTP client approach
	g.httpClient = newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	err := g.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

// --- Cohere: health check success via mock ---

func TestCohere_HealthCheck_Success_HTTP(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := c.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

// --- Mistral: health check success via mock ---

func TestMistral_HealthCheck_Success_HTTP(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := m.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

// --- NVIDIA NIM: health check success via mock ---

func TestNVIDIANIM_HealthCheck_Success_HTTP(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := n.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

// --- OpenRouter: health check success via mock ---

func TestOpenRouter_HealthCheck_Success_HTTP(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := o.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

// --- CalculateOpenAI_Cost with gpt-4o ---

func TestCalculateOpenAICost_GPT4o(t *testing.T) {
	cost := calculateOpenAICost("gpt-4o", 1000, 500)
	if cost <= 0 {
		t.Errorf("expected positive cost, got %f", cost)
	}
}

// --- CacheKey with tools ---

func TestCacheKey_WithTools(t *testing.T) {
	req := &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools:    []ToolDef{{Name: "search"}},
	}
	key := CacheKey(req)
	if key == "" {
		t.Error("expected non-empty key")
	}
}

// --- InMemoryCache: get expired after TTL ---

func TestInMemoryCache_Get_Expired(t *testing.T) {
	c := NewInMemoryCache(time.Second)
	fake := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return fake }
	c.Set("k", &ChatResponse{Content: "v"})
	fake = fake.Add(2 * time.Second)
	_, ok := c.Get("k")
	if ok {
		t.Error("expected miss after expiry")
	}
	st := c.Stats()
	if st.Misses != 1 {
		t.Errorf("expected 1 miss, got %d", st.Misses)
	}
}

// --- SetPrice and AllPrices round trip ---

func TestSetPrice_AndLookup(t *testing.T) {
	SetPrice("round-trip-model", ModelInfo{
		Name: "round-trip-model", Provider: "test",
		InputCostPer1K: 0.1, OutputCostPer1K: 0.2,
	})
	info, ok := LookupPrice("round-trip-model")
	if !ok {
		t.Fatal("expected to find model")
	}
	if info.InputCostPer1K != 0.1 {
		t.Errorf("wrong input cost: %f", info.InputCostPer1K)
	}
	all := AllPrices()
	if _, ok := all["round-trip-model"]; !ok {
		t.Error("model should be in AllPrices")
	}
	delete(PriceTable, "round-trip-model")
}

// --- MaxTokensFor with known model ---

func TestMaxTokensFor_KnownModel(t *testing.T) {
	r := NewModelRouter(nil)
	tokens := r.maxTokensFor("gpt-4o")
	if tokens != 16384 {
		t.Errorf("expected 16384 for gpt-4o, got %d", tokens)
	}
}

// --- Router config with DefaultOutputTokens ---

func TestNewModelRouter_CustomConfig(t *testing.T) {
	r := NewModelRouter(&RouterConfig{
		DefaultModel:       "custom-model",
		BudgetPerTask:      5.0,
		DefaultOutputTokens: 1000,
	})
	if r.config.DefaultModel != "custom-model" {
		t.Errorf("default model = %q", r.config.DefaultModel)
	}
	if r.config.DefaultOutputTokens != 1000 {
		t.Errorf("default output tokens = %d", r.config.DefaultOutputTokens)
	}
}

// --- StartHealthChecks with no providers ---

func TestStartHealthChecks_NoProviders(t *testing.T) {
	r := NewModelRouter(nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.StartHealthChecks(ctx, 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("StartHealthChecks did not stop")
	}
}

// --- ExecuteWithFailover with nil lastErr (impossible but covers dead code) ---

func TestExecuteWithFailover_AllSucceed(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai", resp: &ChatResponse{Content: "ok", Cost: 0.01}})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	resp, err := r.ExecuteWithFailover(context.Background(), simpleTask())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Errorf("wrong content: %q", resp.Content)
	}
}

// --- DeepSeek: Chat with zero choices but valid response ---

func TestDeepSeek_Chat_ZeroChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0},"model":"m"}`))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error with zero choices")
	}
}

// --- Cohere: stream message-end ---

func TestCohere_Stream_MessageEnd(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			events := []map[string]interface{}{
				{"type": "content-delta", "delta": map[string]interface{}{
					"delta": map[string]interface{}{
						"content": map[string]interface{}{"text": "Hello"},
					},
				}},
				{"type": "message-end"},
			}
			for _, e := range events {
				b, _ := json.Marshal(e)
				w.Write(b)
				w.Write([]byte("\n"))
			}
		}),
	}
	ch, err := c.Stream(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var content string
	for chunk := range ch {
		if chunk.Finish {
			break
		}
		content += chunk.Content
	}
	if content != "Hello" {
		t.Errorf("wrong content: %q", content)
	}
}

// --- Cohere: stream unknown event type ---

func TestCohere_Stream_UnknownEventType(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			events := []map[string]interface{}{
				{"type": "unknown-type"},
				{"type": "message-end"},
			}
			for _, e := range events {
				b, _ := json.Marshal(e)
				w.Write(b)
				w.Write([]byte("\n"))
			}
		}),
	}
	ch, err := c.Stream(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
}

// --- ExecuteWithFailover: multiple fallbacks ---

func TestExecuteWithFailover_MultipleFallbacks(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai", err: fmt.Errorf("fail1")})
	r.RegisterProvider("anthropic", &countingProvider{name: "anthropic", err: fmt.Errorf("fail2")})
	r.RegisterProvider("groq", &countingProvider{name: "groq", resp: &ChatResponse{Content: "groq-ok", Cost: 0.01}})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	r.healthMonitor.RecordSuccess("anthropic", time.Millisecond)
	r.healthMonitor.RecordSuccess("groq", time.Millisecond)

	resp, err := r.ExecuteWithFailover(context.Background(), &Task{
		ID: "t1", Type: "formatting", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "groq-ok" {
		t.Errorf("wrong content: %q", resp.Content)
	}
}

// --- OpenAI stream: error mid-stream ---

func TestOpenAI_Stream_ErrorMidStream(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected Flusher")
		}
		// Send one valid event then close
		ev := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"delta": map[string]interface{}{"content": "hi"}},
			},
		}
		b, _ := json.Marshal(ev)
		w.Write(b)
		flusher.Flush()
		// Close connection to trigger error
		hj, ok := w.(http.Hijacker)
		if ok {
			conn, _, _ := hj.Hijack()
			conn.Close()
		}
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	ch, err := o.Stream(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var content string
	for chunk := range ch {
		content += chunk.Content
		if chunk.Finish {
			break
		}
	}
	if content != "hi" {
		t.Errorf("wrong content: %q", content)
	}
}

// --- NVIDIANIM: stream success with content ---

func TestNVIDIANIM_Stream_ContentOnly(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			ev := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"delta": map[string]interface{}{"content": "Hi"}},
				},
			}
			b, _ := json.Marshal(ev)
			w.Write(b)
			w.Write([]byte("\n"))
		}),
	}
	ch, err := n.Stream(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var content string
	for chunk := range ch {
		if chunk.Finish {
			break
		}
		content += chunk.Content
	}
	if content != "Hi" {
		t.Errorf("wrong content: %q", content)
	}
}

// --- DeepSeek: chat with temperature 0 ---

func TestDeepSeek_Chat_ZeroTemperature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		// temperature 0 should not be set
		if _, ok := req["temperature"]; ok {
			t.Error("temperature should not be set when 0")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 5, 5))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// --- DeepSeek: stream with temperature 0 ---

func TestDeepSeek_Stream_ZeroTemperature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	ch, err := d.Stream(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
}

// --- RecordSuccess: latency ring buffer overflow ---

func TestRecordSuccess_LatencyRingBufferOverflow(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	// Add more than 100 latencies to trigger ring buffer trim
	for i := 0; i < 110; i++ {
		hm.RecordSuccess("p", time.Duration(i)*time.Millisecond)
	}
	hm.mu.RLock()
	p := hm.providers["p"]
	if len(p.latencies) != 100 {
		t.Errorf("expected 100 latencies, got %d", len(p.latencies))
	}
	p50 := p.LatencyP50
	hm.mu.RUnlock()
	if p50 <= 0 {
		t.Error("expected positive P50 latency")
	}
}

// --- RecordSuccess: error rate edge ---

func TestRecordSuccess_ErrorRateEdge(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	hm.RecordSuccess("p", time.Millisecond)
	hm.mu.RLock()
	p := hm.providers["p"]
	rate := p.ErrorRate
	status := p.Status
	hm.mu.RUnlock()
	if rate > 0.1 {
		t.Errorf("error rate = %f, want < 0.1", rate)
	}
	if status != StatusHealthy {
		t.Errorf("status = %d, want StatusHealthy", status)
	}
}
