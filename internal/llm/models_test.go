package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Content from models_test.go
func TestProviderID_Constants(t *testing.T) {
	assert.Equal(t, ProviderID("openai"), ProviderOpenAI)
	assert.Equal(t, ProviderID("anthropic"), ProviderAnthropic)
	assert.Equal(t, ProviderID("gemini"), ProviderGemini)
	assert.Equal(t, ProviderID("groq"), ProviderGroq)
	assert.Equal(t, ProviderID("mistral"), ProviderMistral)
	assert.Equal(t, ProviderID("cohere"), ProviderCohere)
	assert.Equal(t, ProviderID("nvidia_nim"), ProviderNVIDIANIM)
	assert.Equal(t, ProviderID("openrouter"), ProviderOpenRouter)
	assert.Equal(t, ProviderID("deepseek"), ProviderDeepSeek)
}

func TestProviders_ContainsAll(t *testing.T) {
	providers := Providers()
	require.GreaterOrEqual(t, len(providers), 9)

	ids := make(map[ProviderID]bool)
	for _, p := range providers {
		ids[p.ID] = true
	}

	for _, expected := range []ProviderID{
		ProviderOpenAI, ProviderAnthropic, ProviderGemini, ProviderGroq,
		ProviderMistral, ProviderCohere, ProviderNVIDIANIM, ProviderOpenRouter,
		ProviderDeepSeek,
	} {
		assert.True(t, ids[expected], "missing provider %s", expected)
	}
}

func TestProviders_EachHasName(t *testing.T) {
	for _, p := range Providers() {
		assert.NotEmpty(t, p.Name, "provider %s has empty name", p.ID)
	}
}

func TestProviders_EachHasBaseURL(t *testing.T) {
	for _, p := range Providers() {
		assert.NotEmpty(t, p.BaseURL, "provider %s has empty base URL", p.ID)
	}
}

func TestProviders_EachHasKeyPrefix(t *testing.T) {
	for _, p := range Providers() {
		assert.NotEmpty(t, p.KeyPrefix, "provider %s has empty key prefix", p.ID)
	}
}

func TestProviderModels_OpenAI(t *testing.T) {
	models := ProviderModels(ProviderOpenAI)
	require.NotEmpty(t, models)
	// Should have GPT-5.6, GPT-4o, o3, embeddings, etc.
	ids := make(map[string]bool)
	for _, m := range models {
		ids[m.ID] = true
	}
	assert.True(t, ids["gpt-4o"])
	assert.True(t, ids["gpt-4o-mini"])
}

func TestProviderModels_Anthropic(t *testing.T) {
	models := ProviderModels(ProviderAnthropic)
	require.NotEmpty(t, models)
	ids := make(map[string]bool)
	for _, m := range models {
		ids[m.ID] = true
	}
	assert.True(t, ids["claude-sonnet-4-20250514"] || ids["claude-sonnet-5"])
}

func TestProviderModels_NonExistent(t *testing.T) {
	models := ProviderModels("nonexistent")
	assert.Nil(t, models)
}

func TestFindModel_GPT4o(t *testing.T) {
	m := FindModel("gpt-4o")
	require.NotNil(t, m)
	assert.Equal(t, "GPT-4o", m.Name)
	assert.Equal(t, "openai", m.Provider)
}

func TestFindModel_Claude(t *testing.T) {
	m := FindModel("claude-sonnet-5")
	require.NotNil(t, m)
	assert.Equal(t, "anthropic", m.Provider)
}

func TestFindModel_NonExistent(t *testing.T) {
	m := FindModel("this-model-does-not-exist-12345")
	assert.Nil(t, m)
}

func TestFindModel_EmptyString(t *testing.T) {
	m := FindModel("")
	assert.Nil(t, m)
}

func TestProviderByKeyPrefix_OpenAI(t *testing.T) {
	p := ProviderByKeyPrefix("sk-anything")
	require.NotNil(t, p)
	assert.Contains(t, []ProviderID{ProviderOpenAI, ProviderDeepSeek}, p.ID)
}

func TestProviderByKeyPrefix_Anthropic(t *testing.T) {
	p := ProviderByKeyPrefix("sk-ant-12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderAnthropic, p.ID)
}

func TestProviderByKeyPrefix_Groq(t *testing.T) {
	p := ProviderByKeyPrefix("gsk_12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderGroq, p.ID)
}

func TestProviderByKeyPrefix_Mistral(t *testing.T) {
	p := ProviderByKeyPrefix("ms-12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderMistral, p.ID)
}

func TestProviderByKeyPrefix_Cohere(t *testing.T) {
	p := ProviderByKeyPrefix("co-12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderCohere, p.ID)
}

func TestProviderByKeyPrefix_NVIDIA(t *testing.T) {
	p := ProviderByKeyPrefix("nvapi-12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderNVIDIANIM, p.ID)
}

func TestProviderByKeyPrefix_Gemini(t *testing.T) {
	p := ProviderByKeyPrefix("AIzaSyXXX")
	require.NotNil(t, p)
	assert.Equal(t, ProviderGemini, p.ID)
}

func TestProviderByKeyPrefix_OpenRouter(t *testing.T) {
	p := ProviderByKeyPrefix("sk-or-12345")
	require.NotNil(t, p)
	assert.Equal(t, ProviderOpenRouter, p.ID)
}

func TestProviderByKeyPrefix_DeepSeek(t *testing.T) {
	p := ProviderByKeyPrefix("sk-12345")
	// sk- matches both OpenAI and DeepSeek; longest prefix wins
	require.NotNil(t, p)
	// Both have sk- prefix, so the longer one wins (both same length, random)
	assert.Contains(t, []ProviderID{ProviderOpenAI, ProviderDeepSeek}, p.ID)
}

func TestProviderByKeyPrefix_NonExistent(t *testing.T) {
	p := ProviderByKeyPrefix("zzz-no-match")
	assert.Nil(t, p)
}

func TestProviderByKeyPrefix_EmptyString(t *testing.T) {
	p := ProviderByKeyPrefix("")
	assert.Nil(t, p)
}

func TestGetFullCatalogEntries(t *testing.T) {
	catalog := GetFullCatalog()
	require.NotEmpty(t, catalog)

	ids := make(map[ProviderID]bool)
	for _, c := range catalog {
		assert.NotEmpty(t, c.Provider.Name)
		assert.NotEmpty(t, c.Models)
		ids[c.Provider.ID] = true
	}

	assert.True(t, ids[ProviderOpenAI])
	assert.True(t, ids[ProviderAnthropic])
	assert.True(t, ids[ProviderGemini])
}

func TestHasPrefixCases(t *testing.T) {
	tests := []struct {
		s, prefix string
		expected  bool
	}{
		{"sk-ant-xxx", "sk-ant-", true},
		{"sk-xxx", "sk-ant-", false},
		{"abc", "abc", true},
		{"", "", true},
		{"abc", "abcd", false},
		{"", "a", false},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, hasPrefix(tt.s, tt.prefix), "hasPrefix(%q, %q)", tt.s, tt.prefix)
	}
}

func TestModelCatalogEntry_Capabilities(t *testing.T) {
	m := FindModel("gpt-4o")
	require.NotNil(t, m)
	assert.Contains(t, m.Capabilities, "tools")
	assert.Contains(t, m.Capabilities, "vision")
}

func TestModelCatalogEntry_Deprecated(t *testing.T) {
	models := ProviderModels(ProviderOpenAI)
	for _, m := range models {
		if m.ID == "gpt-4-turbo" {
			assert.True(t, m.Deprecated)
			return
		}
	}
	t.Skip("gpt-4-turbo not found in catalog")
}

func TestProviderInfo_KeyHint(t *testing.T) {
	providers := Providers()
	for _, p := range providers {
		assert.NotEmpty(t, p.KeyHint, "provider %s has empty key hint", p.ID)
	}
}

func TestProviderInfo_Description(t *testing.T) {
	providers := Providers()
	for _, p := range providers {
		assert.NotEmpty(t, p.Description, "provider %s has empty description", p.ID)
	}
}

func TestFindModel_ModelsFromMultipleProviders(t *testing.T) {
	openaiModels := ProviderModels(ProviderOpenAI)
	anthropicModels := ProviderModels(ProviderAnthropic)
	geminiModels := ProviderModels(ProviderGemini)

	total := len(openaiModels) + len(anthropicModels) + len(geminiModels)
	assert.Greater(t, total, 20, "should have many models across providers")
}

func TestModelCatalogEntry_ContextWindow(t *testing.T) {
	models := ProviderModels(ProviderOpenAI)
	for _, m := range models {
		if m.ID == "gpt-4o" {
			assert.Greater(t, m.ContextWindow, 0)
			assert.Greater(t, m.MaxOutput, 0)
			return
		}
	}
}

func TestModelCatalogEntry_Pricing(t *testing.T) {
	m := FindModel("gpt-4o")
	require.NotNil(t, m)
	assert.Greater(t, m.InputCostPer1M, 0.0)
	assert.Greater(t, m.OutputCostPer1M, 0.0)
}

// Content from llm_common_test.go
func TestSafeReadBody_Normal(t *testing.T) {
	body, err := safeReadBody(bytes.NewReader([]byte("hello world")))
	require.NoError(t, err)
	assert.Equal(t, "hello world", string(body))
}

func TestSafeReadBody_EmptyReader(t *testing.T) {
	body, err := safeReadBody(bytes.NewReader(nil))
	require.NoError(t, err)
	assert.Empty(t, body)
}

func TestSafeReadBody_LargeBody(t *testing.T) {
	data := strings.Repeat("x", 1024*100) // 100KB
	body, err := safeReadBody(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	assert.Equal(t, data, string(body))
}

func TestSafeReadBody_ExactLimit(t *testing.T) {
	data := strings.Repeat("a", maxResponseBodySize)
	body, err := safeReadBody(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	assert.Equal(t, data, string(body))
}

func TestSafeReadBody_OverLimit(t *testing.T) {
	data := strings.Repeat("b", maxResponseBodySize+1)
	body, err := safeReadBody(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	// LimitReader truncates at maxResponseBodySize
	assert.Equal(t, maxResponseBodySize, len(body))
}

func TestSafeReadBody_JSON(t *testing.T) {
	json := `{"key": "value", "number": 42}`
	body, err := safeReadBody(bytes.NewReader([]byte(json)))
	require.NoError(t, err)
	assert.Equal(t, json, string(body))
}

func TestSafeReadBody_BinaryData(t *testing.T) {
	data := []byte{0x00, 0x01, 0x02, 0xFF, 0xFE, 0xFD}
	body, err := safeReadBody(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, data, body)
}

func TestMaxResponseBodySize(t *testing.T) {
	assert.Equal(t, 10*1024*1024, maxResponseBodySize)
}

func TestSafeReadBody_ReadCloser(t *testing.T) {
	data := []byte("test data")
	r := io.NopCloser(bytes.NewReader(data))
	body, err := safeReadBody(r)
	require.NoError(t, err)
	assert.Equal(t, "test data", string(body))
}

func TestSafeReadBody_Unicode(t *testing.T) {
	data := "Hello 世界 🌍 مرحبا"
	body, err := safeReadBody(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	assert.Equal(t, data, string(body))
}

func TestSafeReadBody_Newlines(t *testing.T) {
	data := "line1\nline2\nline3\n"
	body, err := safeReadBody(bytes.NewReader([]byte(data)))
	require.NoError(t, err)
	assert.Equal(t, data, string(body))
}

// Content from llm_extra_test.go
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

func (p *errProvider) Name() string { return p.name }
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
		DefaultModel:        "custom-model",
		BudgetPerTask:       5.0,
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
		fmt.Fprintf(w, "data: %s\n\n", b)
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

// Content from sse_helper_test.go
func TestParseOpenAIStyleSSE_SingleEvent(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{"content": "Hello"}},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.Equal(t, "Hello", chunk.Content)
	assert.False(t, chunk.Finish)
}

func TestParseOpenAIStyleSSE_MultipleEvents(t *testing.T) {
	events := openAISSEEvents([]string{"A", "B", "C"})
	dec := json.NewDecoder(bytes.NewReader(events))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	var content string
	for chunk := range ch {
		if chunk.Finish {
			break
		}
		content += chunk.Content
	}
	assert.Equal(t, "ABC", content)
}

func TestParseOpenAIStyleSSE_EmptyDecoder(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(""))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.True(t, chunk.Finish)
}

func TestParseOpenAIStyleSSE_FinishReason(t *testing.T) {
	finish := "stop"
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"delta":         map[string]interface{}{},
				"finish_reason": finish,
			},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.True(t, chunk.Finish)
}

func TestParseOpenAIStyleSSE_EmptyChoicesEvent(t *testing.T) {
	data := map[string]interface{}{"choices": []map[string]interface{}{}}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.True(t, chunk.Finish)
}

func TestParseOpenAIStyleSSE_ContentAndFinish(t *testing.T) {
	finish := "stop"
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"delta":         map[string]interface{}{"content": "done"},
				"finish_reason": finish,
			},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.Equal(t, "done", chunk.Content)
	assert.True(t, chunk.Finish)
}

func TestParseOpenAIStyleSSE_NilFinishReason(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"delta":         map[string]interface{}{"content": "hi"},
				"finish_reason": nil,
			},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.Equal(t, "hi", chunk.Content)
	assert.False(t, chunk.Finish)
}

func TestBuildOpenAIMessages_WithSystem(t *testing.T) {
	msgs := BuildOpenAIMessages("system prompt", []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})
	require.Len(t, msgs, 3)
	assert.Equal(t, "system", msgs[0].Role)
	assert.Equal(t, "system prompt", msgs[0].Content)
	assert.Equal(t, "user", msgs[1].Role)
	assert.Equal(t, "assistant", msgs[2].Role)
}

func TestBuildOpenAIMessages_NoSystemMsg(t *testing.T) {
	msgs := BuildOpenAIMessages("", []Message{
		{Role: "user", Content: "hi"},
	})
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)
}

func TestBuildOpenAIMessages_Empty(t *testing.T) {
	msgs := BuildOpenAIMessages("sys", nil)
	require.Len(t, msgs, 1)
	assert.Equal(t, "system", msgs[0].Role)
}

func TestBuildOpenAIMessages_NoSystemNoMessages(t *testing.T) {
	msgs := BuildOpenAIMessages("", nil)
	assert.Empty(t, msgs)
}

func TestBuildOpenAIMessages_Multiple(t *testing.T) {
	msgs := BuildOpenAIMessages("", []Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
		{Role: "assistant", Content: "d"},
	})
	require.Len(t, msgs, 4)
}

func TestReadFullResponse_ValidJSON(t *testing.T) {
	data := openAIChatResponse("hello world", 10, 20)
	resp, err := ReadFullResponse(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, "hello world", resp.Choices[0].Message.Content)
	assert.Equal(t, 10, resp.Usage.PromptTokens)
	assert.Equal(t, 20, resp.Usage.CompletionTokens)
}

func TestReadFullResponse_InvalidJSONBody(t *testing.T) {
	_, err := ReadFullResponse(strings.NewReader("not json at all"))
	assert.Error(t, err)
}

func TestReadFullResponse_EmptyJSON(t *testing.T) {
	_, err := ReadFullResponse(strings.NewReader("{}"))
	assert.NoError(t, err)
}

func TestReadFullResponse_EmptyBody(t *testing.T) {
	_, err := ReadFullResponse(strings.NewReader(""))
	assert.Error(t, err)
}

func TestReadFullResponse_FinishReason(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message":       map[string]interface{}{"content": "ok"},
				"finish_reason": "tool_calls",
			},
		},
		"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 5},
	}
	b, _ := json.Marshal(data)
	resp, err := ReadFullResponse(bytes.NewReader(b))
	require.NoError(t, err)
	assert.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
}

func TestOpenAIStyleSSEEvent_Fields(t *testing.T) {
	event := OpenAIStyleSSEEvent{
		Choices: []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		}{
			{
				Delta: struct {
					Content string `json:"content"`
				}{Content: "test"},
			},
		},
	}
	assert.Equal(t, "test", event.Choices[0].Delta.Content)
}

func TestOpenAIStyleStreamRequest_Fields(t *testing.T) {
	req := OpenAIStyleStreamRequest{
		Model:       "gpt-4o",
		Messages:    []OpenAIStyleMsg{{Role: "user", Content: "hi"}},
		MaxTokens:   100,
		Temperature: 0.7,
		Stream:      true,
	}
	b, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(b), "gpt-4o")
	assert.Contains(t, string(b), "stream")
}

func TestOpenAIStyleMsg_Fields(t *testing.T) {
	msg := OpenAIStyleMsg{Role: "user", Content: "hello"}
	b, err := json.Marshal(msg)
	require.NoError(t, err)
	assert.Contains(t, string(b), "user")
	assert.Contains(t, string(b), "hello")
}

func TestParseOpenAIStyleSSE_ChannelFull(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{"content": "x"}},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 1) // buffer of 1
	ParseOpenAIStyleSSE(dec, ch)

	// Should not block even with full channel
	select {
	case chunk := <-ch:
		assert.Equal(t, "x", chunk.Content)
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestParseOpenAIStyleSSE_OnlyEmptyContent(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{"content": ""}},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	// Empty content with no finish should still be received
	assert.True(t, chunk.Finish) // finish from deferred
}

// Content from router_exec_test.go
// countingProvider records how many times Chat was called so tests can prove
// the cache prevented a paid call.
type countingProvider struct {
	name  string
	calls int
	resp  *ChatResponse
	err   error
}

func (p *countingProvider) Name() string { return p.name }
func (p *countingProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return p.resp, nil
}
func (p *countingProvider) Stream(_ context.Context, _ *ChatRequest) (<-chan *ChatChunk, error) {
	return nil, errors.New("not implemented")
}
func (p *countingProvider) HealthCheck(_ context.Context) error { return nil }

// fakeBudget lets tests reject spend and observe recorded cost.
type fakeBudget struct {
	reject    error
	recorded  float64
	checkCall int
}

func (b *fakeBudget) CheckBudget(_ context.Context, _, _ string, _ float64) error {
	b.checkCall++
	return b.reject
}
func (b *fakeBudget) RecordCost(_, _ string, cost float64) { b.recorded += cost }

func newTestRouter(p Provider, name string) *ModelRouter {
	r := NewModelRouter(&RouterConfig{DefaultModel: "gpt-4o-mini", DefaultOutputTokens: 500})
	r.RegisterProvider(name, p)
	r.healthMonitor.RecordSuccess(name, 10*time.Millisecond) // mark healthy
	return r
}

func simpleTask() *Task {
	return &Task{ID: "t1", OrgID: "o1", Type: "formatting",
		Messages: []Message{{Role: "user", Content: "format this file"}}}
}

func TestExecuteWithFailover_CacheAvoidsSecondCall(t *testing.T) {
	prov := &countingProvider{name: "openai", resp: &ChatResponse{Content: "done", Cost: 0.01}}
	r := newTestRouter(prov, "openai")
	r.SetCache(NewInMemoryCache(time.Minute))

	if _, err := r.ExecuteWithFailover(context.Background(), simpleTask()); err != nil {
		t.Fatalf("first call failed: %v", err)
	}
	if _, err := r.ExecuteWithFailover(context.Background(), simpleTask()); err != nil {
		t.Fatalf("second call failed: %v", err)
	}

	if prov.calls != 1 {
		t.Fatalf("expected provider called once (second served from cache), got %d", prov.calls)
	}
}

func TestExecuteWithFailover_BudgetBlocksCall(t *testing.T) {
	prov := &countingProvider{name: "openai", resp: &ChatResponse{Content: "done", Cost: 0.01}}
	r := newTestRouter(prov, "openai")
	r.SetBudgetGuard(&fakeBudget{reject: errors.New("over budget")})

	_, err := r.ExecuteWithFailover(context.Background(), simpleTask())
	if err == nil {
		t.Fatal("expected budget rejection to surface as an error")
	}
	if prov.calls != 0 {
		t.Fatalf("provider must not be called when budget is exceeded, got %d calls", prov.calls)
	}
}

func TestExecuteWithFailover_RecordsActualCost(t *testing.T) {
	prov := &countingProvider{name: "openai", resp: &ChatResponse{Content: "done", Cost: 0.042}}
	r := newTestRouter(prov, "openai")
	b := &fakeBudget{}
	r.SetBudgetGuard(b)

	if _, err := r.ExecuteWithFailover(context.Background(), simpleTask()); err != nil {
		t.Fatalf("call failed: %v", err)
	}
	if b.recorded != 0.042 {
		t.Fatalf("expected recorded cost 0.042, got %v", b.recorded)
	}
}

func TestRoute_CapabilityFilterExcludesModels(t *testing.T) {
	r := NewModelRouter(&RouterConfig{DefaultOutputTokens: 500})
	// Only deepseek is healthy; it lacks the "tools" capability.
	r.RegisterProvider("deepseek", &countingProvider{name: "deepseek", resp: &ChatResponse{}})
	r.healthMonitor.RecordSuccess("deepseek", time.Millisecond)

	task := &Task{ID: "t", Type: "architecture", RequiredCapabilities: []string{"tools"},
		Messages: []Message{{Role: "user", Content: "redesign the module"}}}

	_, err := r.Route(context.Background(), task)
	if err == nil {
		t.Fatal("expected routing to fail when no healthy model has the required capability")
	}
}
