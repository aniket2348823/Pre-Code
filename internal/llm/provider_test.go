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
	"sync"
	"testing"
	"time"
)

func TestPriceTable(t *testing.T) {
	if len(PriceTable) == 0 {
		t.Fatal("price table is empty")
	}

	knownModels := []string{"gpt-4o", "gpt-4o-mini", "claude-sonnet-4-20250514", "claude-haiku-3.5"}
	for _, model := range knownModels {
		info, ok := PriceTable[model]
		if !ok {
			t.Errorf("model %s not found in price table", model)
			continue
		}
		if info.InputCostPer1K <= 0 || info.OutputCostPer1K <= 0 {
			t.Errorf("model %s has invalid pricing: input=%f output=%f", model, info.InputCostPer1K, info.OutputCostPer1K)
		}
	}
}

func TestClassifyComplexity(t *testing.T) {
	router := NewModelRouter(nil)

	tests := []struct {
		name     string
		task     *Task
		minScore float64
		maxScore float64
	}{
		{
			name:     "formatting is very low complexity",
			task:     &Task{ID: "t1", Type: "formatting", Tags: []string{}},
			minScore: 0.0,
			maxScore: 0.3,
		},
		{
			name:     "bug_fix is moderate",
			task:     &Task{ID: "t2", Type: "bug_fix", Tags: []string{}},
			minScore: 0.2,
			maxScore: 0.5,
		},
		{
			name:     "architecture with security tag and reasoning is high",
			task:     &Task{ID: "t3", Type: "architecture", Tags: []string{"security"}, RequiresReasoning: true},
			minScore: 0.7,
			maxScore: 1.0,
		},
		{
			name:     "rename is very low complexity",
			task:     &Task{ID: "t4", Type: "rename", Tags: []string{}},
			minScore: 0.0,
			maxScore: 0.3,
		},
		{
			name:     "feature with many files is moderate-high",
			task:     &Task{ID: "t5", Type: "feature", FilesChanged: make([]string, 8), Tags: []string{}},
			minScore: 0.4,
			maxScore: 0.9,
		},
		{
			name:     "unknown type is low",
			task:     &Task{ID: "t6", Type: "unknown", Tags: []string{}},
			minScore: 0.0,
			maxScore: 0.2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := router.classifyComplexity(tt.task)
			score := float64(result)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("classifyComplexity() = %f, want between %f and %f", score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestRankCandidates(t *testing.T) {
	healthMonitor := NewHealthMonitor()
	healthMonitor.RecordSuccess("openai", 100*time.Millisecond)
	healthMonitor.RecordSuccess("anthropic", 100*time.Millisecond)
	healthMonitor.RecordSuccess("google", 100*time.Millisecond)

	router := &ModelRouter{
		providers:     make(map[string]Provider),
		healthMonitor: healthMonitor,
		config:        &RouterConfig{DefaultModel: "gpt-4o", BudgetPerTask: 1.0, DefaultOutputTokens: 500},
		prices:        PriceTable,
	}

	task := &Task{Messages: []Message{{Role: "user", Content: "rename a variable"}}}
	candidates := router.rankCandidates(task, []string{"openai", "anthropic", "google"}, ComplexitySimple)
	if len(candidates) == 0 {
		t.Fatal("rankCandidates returned no candidates for simple complexity")
	}

	// Verify candidates are sorted by cost ascending
	for i := 1; i < len(candidates); i++ {
		if candidates[i].EstCost < candidates[i-1].EstCost {
			t.Errorf("candidates not sorted: [%d]=%f > [%d]=%f",
				i-1, candidates[i-1].EstCost, i, candidates[i].EstCost)
		}
	}

	complexCandidates := router.rankCandidates(task, []string{"openai", "anthropic"}, ComplexityComplex)
	if len(complexCandidates) == 0 {
		t.Fatal("rankCandidates returned no candidates for complex complexity")
	}

	for _, c := range complexCandidates {
		if c.EstCost <= 0 {
			t.Errorf("candidate %s has non-positive cost: %f", c.Model, c.EstCost)
		}
		if c.Provider == "" {
			t.Errorf("candidate %s has empty provider", c.Model)
		}
	}
}

func TestModelRouter_New(t *testing.T) {
	router := NewModelRouter(nil)
	if router == nil {
		t.Fatal("NewModelRouter returned nil")
	}
	if router.config == nil {
		t.Fatal("config is nil")
	}
	if router.config.DefaultModel != "claude-sonnet-4-20250514" {
		t.Errorf("default model = %q", router.config.DefaultModel)
	}
}

func TestModelRouter_RegisterProvider(t *testing.T) {
	router := NewModelRouter(nil)
	mock := &mockProvider{name: "test-provider"}
	router.RegisterProvider("test-provider", mock)

	provider, ok := router.providers["test-provider"]
	if !ok {
		t.Fatal("provider not registered")
	}
	if provider.Name() != "test-provider" {
		t.Errorf("provider.Name() = %q", provider.Name())
	}
}

func TestGetModelsForComplexity(t *testing.T) {
	router := NewModelRouter(nil)

	simpleModels := router.getModelsForComplexity(ComplexitySimple)
	if len(simpleModels) == 0 {
		t.Error("no models for simple complexity")
	}

	complexModels := router.getModelsForComplexity(ComplexityComplex)
	if len(complexModels) == 0 {
		t.Error("no models for complex complexity")
	}

	// Simple and complex should return different models
	for _, sm := range simpleModels {
		for _, cm := range complexModels {
			if sm == cm {
				t.Errorf("model %s in both simple and complex", sm)
			}
		}
	}
}

type mockProvider struct {
	name string
}

func (m *mockProvider) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{Content: "mock", Model: req.Model, Provider: m.name}, nil
}
func (m *mockProvider) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	ch := make(chan *ChatChunk, 1)
	ch <- &ChatChunk{Content: "mock", Finish: true}
	close(ch)
	return ch, nil
}
func (m *mockProvider) HealthCheck(ctx context.Context) error { return nil }
func (m *mockProvider) Name() string                          { return m.name }

var errProviderDown = errors.New("provider is down")

func TestRoute_NoHealthyProviders(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{ID: "t", Type: "bug_fix", Messages: []Message{{Role: "user", Content: "fix"}}}
	_, err := r.Route(context.Background(), task)
	if err == nil {
		t.Error("expected error with no providers")
	}
}

func TestRoute_AllUnhealthy(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordFailure("openai")
	r.healthMonitor.RecordFailure("openai")
	r.healthMonitor.RecordFailure("openai") // StatusDown
	task := &Task{ID: "t", Type: "bug_fix", Messages: []Message{{Role: "user", Content: "fix"}}}
	_, err := r.Route(context.Background(), task)
	if err == nil {
		t.Error("expected error with all unhealthy")
	}
}

func TestClassifyComplexity_EmptyType(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{ID: "t", Type: "", Messages: []Message{{Role: "user", Content: "fix"}}}
	c := r.classifyComplexity(task)
	if float64(c) != 0.0 {
		t.Errorf("empty type should score 0, got %f", c)
	}
}

func TestClassifyComplexity_UnknownType(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{ID: "t", Type: "unknown_type", Messages: []Message{{Role: "user", Content: "fix"}}}
	c := r.classifyComplexity(task)
	if float64(c) > 0.2 {
		t.Errorf("unknown type should score low, got %f", c)
	}
}

func TestClassifyComplexity_SecurityAndProduction(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{ID: "t", Type: "architecture", Tags: []string{"security", "production"}, RequiresReasoning: true}
	c := r.classifyComplexity(task)
	if float64(c) < 0.7 {
		t.Errorf("architecture + security + reasoning should score high, got %f", c)
	}
}

func TestRoute_ExtremeComplexity(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	task := &Task{ID: "t", Type: "security", FilesChanged: make([]string, 100), Tags: []string{"security"}, Messages: []Message{{Role: "user", Content: "audit"}}}
	decision, err := r.Route(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if decision == nil {
		t.Fatal("expected decision")
	}
}

func TestRoute_MinimalComplexity(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	task := &Task{ID: "t", Type: "formatting", Messages: []Message{{Role: "user", Content: "rename"}}}
	decision, err := r.Route(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if decision == nil {
		t.Fatal("expected decision")
	}
}

func TestEstimateInputTokens_ZeroMessages(t *testing.T) {
	task := &Task{Messages: []Message{}}
	tokens := estimateInputTokens(task)
	if tokens < 50 {
		t.Errorf("expected at least 50 floor tokens, got %d", tokens)
	}
}

func TestEstimateInputTokens_Unicode(t *testing.T) {
	task := &Task{Messages: []Message{{Role: "user", Content: "你好世界测试"}}}
	tokens := estimateInputTokens(task)
	if tokens < 50 {
		t.Errorf("expected at least 50, got %d", tokens)
	}
}

func TestSetPrices_EmptyMap(t *testing.T) {
	r := NewModelRouter(nil)
	orig := r.prices
	r.SetPrices(map[string]ModelInfo{})
	if len(r.prices) != len(orig) {
		t.Error("empty SetPrices should not override")
	}
}

func TestSetPrices_NilMap(t *testing.T) {
	r := NewModelRouter(nil)
	orig := r.prices
	r.SetPrices(nil)
	if len(r.prices) != len(orig) {
		t.Error("nil SetPrices should not override")
	}
}

func TestLookupPrice_NonExistent(t *testing.T) {
	_, ok := LookupPrice("nonexistent-model")
	if ok {
		t.Error("non-existent model should return false")
	}
}

func TestAllPrices_IndependentCopy(t *testing.T) {
	prices := AllPrices()
	if len(prices) == 0 {
		t.Fatal("expected non-empty prices")
	}
	// Mutate copy
	for k := range prices {
		delete(prices, k)
		break
	}
	orig := AllPrices()
	if len(orig) == 0 {
		t.Error("original should not be affected")
	}
}

func TestMaxTokensFor_UnknownModel(t *testing.T) {
	r := NewModelRouter(nil)
	tokens := r.maxTokensFor("nonexistent-model")
	if tokens != 4096 {
		t.Errorf("expected 4096 default, got %d", tokens)
	}
}

func TestHealthMonitor_RecordFailure_Down(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	healthy := hm.GetHealthyProviders()
	for _, name := range healthy {
		if name == "p" {
			t.Error("provider should be unhealthy after 3 failures")
		}
	}
}

func TestHealthMonitor_RecordSuccess_Recovery(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	hm.RecordSuccess("p", 10*time.Millisecond)
	healthy := hm.GetHealthyProviders()
	found := false
	for _, name := range healthy {
		if name == "p" {
			found = true
		}
	}
	if !found {
		t.Error("provider should recover after success")
	}
}

func TestHealthMonitor_Confidence_Unknown(t *testing.T) {
	hm := NewHealthMonitor()
	c := hm.Confidence("unknown")
	if c != 0.5 {
		t.Errorf("unknown provider confidence should be 0.5, got %f", c)
	}
}

func TestHealthMonitor_Concurrent(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hm.RecordSuccess("p", time.Millisecond)
			hm.RecordFailure("p")
			hm.GetHealthyProviders()
			hm.Confidence("p")
		}()
	}
	wg.Wait()
}

func TestRunPeriodicChecks_ContextCancel(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		hm.RunPeriodicChecks(ctx, 10*time.Millisecond)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Error("RunPeriodicChecks did not stop after context cancel")
	}
}

func TestPriceTable_ConcurrentReadWrite(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = AllPrices()
		}()
	}
	wg.Wait()
}

func TestModelInfo_Supports(t *testing.T) {
	info := ModelInfo{Capabilities: []string{"tools", "vision"}}
	if !info.Supports("tools") {
		t.Error("should support tools")
	}
	if !info.Supports("vision") {
		t.Error("should support vision")
	}
	if info.Supports("reasoning") {
		t.Error("should not support reasoning")
	}
}

func TestNewModelRouter_DefaultConfig(t *testing.T) {
	r := NewModelRouter(nil)
	if r.config.DefaultModel != "claude-sonnet-4-20250514" {
		t.Errorf("default model = %q", r.config.DefaultModel)
	}
	if r.config.DefaultOutputTokens != 500 {
		t.Errorf("default output tokens = %d", r.config.DefaultOutputTokens)
	}
}

func TestGetModelsForComplexity_Simple(t *testing.T) {
	r := NewModelRouter(nil)
	models := r.getModelsForComplexity(ComplexitySimple)
	if len(models) == 0 {
		t.Error("simple complexity should have models")
	}
	for _, m := range models {
		if _, ok := PriceTable[m]; !ok {
			t.Errorf("model %s not in price table", m)
		}
	}
}

func TestGetModelsForComplexity_Critical(t *testing.T) {
	r := NewModelRouter(nil)
	models := r.getModelsForComplexity(ComplexityCritical)
	if len(models) == 0 {
		t.Error("critical complexity should have models")
	}
}

func TestRankCandidates_SortedByCost(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.RegisterProvider("anthropic", &countingProvider{name: "anthropic"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	r.healthMonitor.RecordSuccess("anthropic", time.Millisecond)
	task := &Task{Messages: []Message{{Role: "user", Content: "fix"}}}
	candidates := r.rankCandidates(task, []string{"openai", "anthropic"}, ComplexitySimple)
	for i := 1; i < len(candidates); i++ {
		if candidates[i].EstCost < candidates[i-1].EstCost {
			t.Error("candidates should be sorted by cost ascending")
		}
	}
}

func TestRoute_VisionCapability(t *testing.T) {
	r := NewModelRouter(nil)
	// Register both openai and anthropic — gpt-4o (openai) supports vision
	// and is in the moderate complexity tier.
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.RegisterProvider("anthropic", &countingProvider{name: "anthropic"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	r.healthMonitor.RecordSuccess("anthropic", time.Millisecond)
	// Use "refactoring" type (complexity 0.5) to land in the moderate tier
	// where gpt-4o (vision-capable, openai provider) is available.
	task := &Task{ID: "t", Type: "refactoring", RequiredCapabilities: []string{"vision"}, Messages: []Message{{Role: "user", Content: "describe image"}}}
	decision, err := r.Route(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if decision == nil {
		t.Fatal("expected decision")
	}
}

// contextAwareProvider respects context cancellation like real providers.
type contextAwareProvider struct {
	name string
	resp *ChatResponse
	err  error
}

func (p *contextAwareProvider) Name() string { return p.name }
func (p *contextAwareProvider) Chat(ctx context.Context, _ *ChatRequest) (*ChatResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	if p.err != nil {
		return nil, p.err
	}
	return p.resp, nil
}
func (p *contextAwareProvider) Stream(_ context.Context, _ *ChatRequest) (<-chan *ChatChunk, error) {
	return nil, errors.New("not implemented")
}
func (p *contextAwareProvider) HealthCheck(_ context.Context) error { return nil }

func TestRoute_ReasoningCapability(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	task := &Task{ID: "t", Type: "architecture", RequiredCapabilities: []string{"reasoning"}, Messages: []Message{{Role: "user", Content: "design system"}}}
	decision, err := r.Route(context.Background(), task)
	if err != nil {
		t.Fatal(err)
	}
	if decision == nil {
		t.Fatal("expected decision")
	}
}

// --- provider_extra_test.go content below ---

func anthropicResponseBody(content string, promptTokens, completionTokens int) []byte {
	resp := map[string]interface{}{
		"id":   "msg_test",
		"role": "assistant",
		"content": []map[string]string{
			{"type": "text", "text": content},
		},
		"model":       "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"usage": map[string]int{
			"input_tokens":  promptTokens,
			"output_tokens": completionTokens,
		},
	}
	b, _ := json.Marshal(resp)
	return b
}

// --- Models catalog ---

func TestProviders(t *testing.T) {
	providers := Providers()
	if len(providers) < 9 {
		t.Fatalf("expected at least 9 providers, got %d", len(providers))
	}
	ids := make(map[ProviderID]bool)
	for _, p := range providers {
		ids[p.ID] = true
	}
	for _, expected := range []ProviderID{ProviderOpenAI, ProviderAnthropic, ProviderGemini, ProviderGroq, ProviderMistral, ProviderCohere, ProviderNVIDIANIM, ProviderOpenRouter, ProviderDeepSeek} {
		if !ids[expected] {
			t.Errorf("missing provider %s", expected)
		}
	}
}

func TestProviderModels(t *testing.T) {
	models := ProviderModels(ProviderOpenAI)
	if len(models) == 0 {
		t.Fatal("expected OpenAI models")
	}
	models = ProviderModels("nonexistent")
	if models != nil {
		t.Error("non-existent provider should return nil")
	}
}

func TestFindModel(t *testing.T) {
	m := FindModel("gpt-4o")
	if m == nil {
		t.Fatal("expected to find gpt-4o")
	}
	if m.Name != "GPT-4o" {
		t.Errorf("wrong name: %q", m.Name)
	}
	m = FindModel("nonexistent-model-id")
	if m != nil {
		t.Error("non-existent model should return nil")
	}
}

func TestProviderByKeyPrefix(t *testing.T) {
	// Test unique prefixes
	tests := []struct {
		prefix     string
		expectedID ProviderID
	}{
		{"AIzaSyXXX", ProviderGemini},
		{"gsk_12345", ProviderGroq},
		{"co-12345", ProviderCohere},
		{"ms-12345", ProviderMistral},
		{"nvapi-12345", ProviderNVIDIANIM},
	}
	for _, tt := range tests {
		p := ProviderByKeyPrefix(tt.prefix)
		if p == nil {
			t.Fatalf("expected to find provider by prefix %q", tt.prefix)
		}
		if p.ID != tt.expectedID {
			t.Errorf("prefix %q: got %q, want %q", tt.prefix, p.ID, tt.expectedID)
		}
	}

	p := ProviderByKeyPrefix("zzz-noprefix")
	if p != nil {
		t.Error("non-matching prefix should return nil")
	}
}

func TestGetFullCatalog(t *testing.T) {
	catalog := GetFullCatalog()
	if len(catalog) == 0 {
		t.Fatal("expected catalog entries")
	}
	for _, c := range catalog {
		if c.Provider.Name == "" {
			t.Error("provider name should not be empty")
		}
		if len(c.Models) == 0 {
			t.Errorf("provider %s has no models", c.Provider.ID)
		}
	}
}

func TestHasPrefix(t *testing.T) {
	if !hasPrefix("sk-ant-xxx", "sk-ant-") {
		t.Error("should match prefix")
	}
	if hasPrefix("sk-", "sk-ant-") {
		t.Error("shorter string should not match longer prefix")
	}
	if !hasPrefix("abc", "abc") {
		t.Error("equal strings should match")
	}
}

func TestEnsureProviderCatalog(t *testing.T) {
	p := Providers()
	if len(p) < 9 {
		t.Errorf("expected at least 9 providers, got %d", len(p))
	}
}

// --- Anthropic provider (via mock HTTP) ---

func TestAnthropic_Name(t *testing.T) {
	a := NewAnthropic("key")
	if a.Name() != "anthropic" {
		t.Errorf("Name() = %q", a.Name())
	}
}

func TestAnthropic_Chat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(anthropicResponseBody("Hello from Anthropic", 10, 20))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{
		apiKey:   "test-key",
		model:    "claude-sonnet-4-20250514",
		httpAddr: srv.URL,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
	resp, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello from Anthropic" {
		t.Errorf("wrong content: %q", resp.Content)
	}
	if resp.Provider != "anthropic" {
		t.Errorf("wrong provider: %q", resp.Provider)
	}
	if resp.InputTokens != 10 || resp.OutputTokens != 20 {
		t.Errorf("wrong tokens: in=%d out=%d", resp.InputTokens, resp.OutputTokens)
	}
}

func TestAnthropic_Chat_EmptyModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(anthropicResponseBody("ok", 5, 5))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{
		apiKey: "test-key", model: "claude-sonnet-4-20250514", httpAddr: srv.URL,
		client: &http.Client{Timeout: 10 * time.Second},
	}
	resp, err := a.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Errorf("wrong content: %q", resp.Content)
	}
}

func TestAnthropic_Chat_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	_, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestAnthropic_Chat_NoMaxTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if req["max_tokens"] != float64(8192) {
			t.Errorf("expected default max_tokens 8192, got %v", req["max_tokens"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(anthropicResponseBody("ok", 5, 5))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	_, err := a.Chat(context.Background(), &ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnthropic_Chat_SystemPrompt(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if req["system"] != "be helpful" {
			t.Errorf("system prompt not passed, got: %v", req["system"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(anthropicResponseBody("ok", 5, 5))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	_, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", System: "be helpful",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnthropic_Stream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\n"))
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" World\"}}\n\n"))
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	ch, err := a.Stream(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
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
	if content != "Hello World" {
		t.Errorf("wrong stream content: %q", content)
	}
}

func TestAnthropic_Stream_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	_, err := a.Stream(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestAnthropic_Stream_EmptyModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{
		apiKey: "k", model: "claude-sonnet-4-20250514", httpAddr: srv.URL,
		client: &http.Client{Timeout: 10 * time.Second},
	}
	ch, err := a.Stream(context.Background(), &ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
}

func TestAnthropic_Stream_NoMaxTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if req["max_tokens"] != float64(8192) {
			t.Errorf("expected default max_tokens 8192, got %v", req["max_tokens"])
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	ch, err := a.Stream(context.Background(), &ChatRequest{Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
}

func TestCalculateAnthropicCost(t *testing.T) {
	cost := calculateAnthropicCost("claude-sonnet-4-20250514", 1000, 500)
	if cost <= 0 {
		t.Errorf("expected positive cost, got %f", cost)
	}
	cost = calculateAnthropicCost("nonexistent", 1000, 500)
	if cost != 0 {
		t.Errorf("unknown model should return 0, got %f", cost)
	}
}

func TestAnthropic_HealthCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	err := a.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestAnthropic_HealthCheck_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	err := a.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error for 401")
	}
}

// --- DeepSeek provider (via mock HTTP) ---

func TestDeepSeek_Name(t *testing.T) {
	d := NewDeepSeek("key")
	if d.Name() != "deepseek" {
		t.Errorf("Name() = %q", d.Name())
	}
}

func TestDeepSeek_Chat_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("Hello from DeepSeek", 10, 20))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	resp, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello from DeepSeek" {
		t.Errorf("wrong content: %q", resp.Content)
	}
	if resp.Provider != "deepseek" {
		t.Errorf("wrong provider: %q", resp.Provider)
	}
}

func TestDeepSeek_Chat_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestDeepSeek_Chat_NoChoices(t *testing.T) {
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
		t.Fatal("expected error with no choices")
	}
}

func TestDeepSeek_Chat_WithParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if req["max_tokens"] != float64(100) {
			t.Errorf("max_tokens not set")
		}
		if req["temperature"] != 0.7 {
			t.Errorf("temperature not set")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 5, 5))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100, Temperature: 0.7,
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeepSeek_Chat_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestDeepSeek_Chat_FinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]interface{}{"content": "ok"}, "finish_reason": "stop"},
			},
			"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 5},
			"model": "deepseek-chat",
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	resp, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StopReason != "stop" {
		t.Errorf("wrong stop reason: %q", resp.StopReason)
	}
}

func TestDeepSeek_Stream_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{"Hello", " World"}
		for _, c := range chunks {
			ev := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"delta": map[string]interface{}{"content": c}},
				},
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
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
	for chunk := range ch {
		if chunk.Finish {
			break
		}
		content += chunk.Content
	}
	if content != "Hello World" {
		t.Errorf("wrong content: %q", content)
	}
}

func TestDeepSeek_Stream_ErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte("forbidden"))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Stream(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestDeepSeek_Stream_WithParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if req["max_tokens"] != float64(100) {
			t.Errorf("max_tokens not passed")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	ch, err := d.Stream(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}}, MaxTokens: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	for range ch {
	}
}

func TestDeepSeek_Stream_EmptyLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"x\"}}]}\n\n")
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

func TestDeepSeek_Stream_NonDataLines(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, ": comment line\n")
		fmt.Fprintf(w, "event: test\n")
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

func TestDeepSeek_Stream_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: not-json\n\n")
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

func TestDeepSeek_Stream_NilFinishReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		ev := map[string]interface{}{
			"choices": []map[string]interface{}{
				{"delta": map[string]interface{}{"content": "hi"}, "finish_reason": nil},
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
	for range ch {
	}
}

func TestDeepSeek_Stream_EmptyChoices(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		ev := map[string]interface{}{"choices": []map[string]interface{}{}}
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
	for range ch {
	}
}

func TestDeepSeek_HealthCheck_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	err := d.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestDeepSeek_HealthCheck_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	err := d.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

// --- Gemini ---

func TestGemini_Name(t *testing.T) {
	g := &GeminiAdapter{apiKey: "key"}
	if g.Name() != "gemini" {
		t.Errorf("Name() = %q", g.Name())
	}
}

func TestBuildGeminiContents(t *testing.T) {
	msgs := buildGeminiContents([]Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
		{Role: "user", Content: "bye"},
	})
	if len(msgs) != 3 {
		t.Fatalf("expected 3, got %d", len(msgs))
	}
	if msgs[0].Role != "user" {
		t.Errorf("first role: %q", msgs[0].Role)
	}
	if msgs[1].Role != "model" {
		t.Errorf("second role: %q", msgs[1].Role)
	}
}

func TestCalculateGeminiCost(t *testing.T) {
	models := []string{"gemini-2.5-pro", "gemini-2.0-flash", "gemini-1.5-pro", "gemini-1.5-flash"}
	for _, m := range models {
		cost := calculateGeminiCost(m, 1000, 500)
		if cost <= 0 {
			t.Errorf("cost for %s should be positive, got %f", m, cost)
		}
	}
	cost := calculateGeminiCost("unknown", 1000, 500)
	if cost <= 0 {
		t.Errorf("fallback cost should be positive, got %f", cost)
	}
}

func TestPtrFloat32(t *testing.T) {
	p := ptrFloat32(3.14)
	if *p != 3.14 {
		t.Errorf("ptrFloat32(3.14) = %f", *p)
	}
}

// --- llm_common ---

func TestSafeReadBody(t *testing.T) {
	body, err := safeReadBody(bytes.NewReader([]byte("hello")))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello" {
		t.Errorf("wrong content: %q", string(body))
	}
}

func TestSafeReadBody_Empty(t *testing.T) {
	body, err := safeReadBody(bytes.NewReader(nil))
	if err != nil {
		t.Fatal(err)
	}
	if len(body) != 0 {
		t.Errorf("expected empty, got %d", len(body))
	}
}

// --- Circuit Breaker extras ---

func TestSetPrice_Global(t *testing.T) {
	SetPrice("test-model-set", ModelInfo{Name: "test-model-set", Provider: "test", InputCostPer1K: 0.01, OutputCostPer1K: 0.02})
	info, ok := LookupPrice("test-model-set")
	if !ok {
		t.Fatal("expected to find model")
	}
	if info.InputCostPer1K != 0.01 {
		t.Errorf("wrong cost: %f", info.InputCostPer1K)
	}
	delete(PriceTable, "test-model-set")
}

func TestGetHealthMonitor(t *testing.T) {
	r := NewModelRouter(nil)
	if r.GetHealthMonitor() == nil {
		t.Fatal("expected health monitor")
	}
}

func TestStartHealthChecks_ContextCancel(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("p", &countingProvider{name: "p"})
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

func TestSystemPrompt(t *testing.T) {
	tests := []struct{ taskType, contains string }{
		{"bug_fix", "fixing the bug"},
		{"feature", "Implement the requested feature"},
		{"refactoring", "Refactor the code"},
		{"security", "security vulnerabilities"},
		{"architecture", "architectural changes"},
		{"unknown", "Complete the requested task"},
		{"", "Complete the requested task"},
	}
	for _, tt := range tests {
		p := systemPrompt(&Task{Type: tt.taskType, FilesChanged: []string{"a.go"}, Tags: []string{"test"}})
		if !strings.Contains(p, tt.contains) {
			t.Errorf("systemPrompt(%q) missing %q, got: %q", tt.taskType, tt.contains, p)
		}
	}
}

func TestSystemPrompt_NilTask(t *testing.T) {
	if systemPrompt(nil) != "" {
		t.Error("nil task should return empty")
	}
}

func TestSystemPrompt_NoFilesOrTags(t *testing.T) {
	p := systemPrompt(&Task{Type: "bug_fix"})
	if !strings.Contains(p, "VigilAgent") {
		t.Error("should contain VigilAgent")
	}
}

// --- SSE Helper extras ---

func TestBuildOpenAIMessages(t *testing.T) {
	msgs := BuildOpenAIMessages("sys", []Message{{Role: "user", Content: "hi"}})
	if len(msgs) != 2 {
		t.Fatalf("expected 2, got %d", len(msgs))
	}
	if msgs[0].Role != "system" {
		t.Errorf("first should be system, got %q", msgs[0].Role)
	}
}

func TestBuildOpenAIMessages_NoSystem(t *testing.T) {
	msgs := BuildOpenAIMessages("", []Message{{Role: "user", Content: "hi"}})
	if len(msgs) != 1 {
		t.Fatalf("expected 1, got %d", len(msgs))
	}
}

func TestReadFullResponse(t *testing.T) {
	data := openAIChatResponse("hello", 10, 20)
	resp, err := ReadFullResponse(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Message.Content != "hello" {
		t.Errorf("wrong content: %q", resp.Choices[0].Message.Content)
	}
}

func TestReadFullResponse_InvalidJSON(t *testing.T) {
	_, err := ReadFullResponse(strings.NewReader("not json"))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestParseOpenAIStyleSSE(t *testing.T) {
	events := openAISSEEvents([]string{"Hello", " World"})
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
	if content != "Hello World" {
		t.Errorf("wrong content: %q", content)
	}
}

func TestParseOpenAIStyleSSE_Empty(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(""))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)
	chunk := <-ch
	if !chunk.Finish {
		t.Error("expected finish")
	}
}

func TestParseOpenAIStyleSSE_ContentOnlyNoFinish(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{"content": "hi"}, "finish_reason": nil},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)
	select {
	case chunk := <-ch:
		if chunk.Content != "hi" {
			t.Errorf("wrong content: %q", chunk.Content)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestParseOpenAIStyleSSE_EmptyChoices(t *testing.T) {
	data := map[string]interface{}{"choices": []map[string]interface{}{}}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)
	select {
	case chunk := <-ch:
		if !chunk.Finish {
			t.Error("expected finish")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

// --- StreamWithFailover + streamAttempt ---

func (p *streamProvider) Name() string { return p.name }
func (p *streamProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{Content: "mock"}, nil
}
func (p *streamProvider) Stream(_ context.Context, _ *ChatRequest) (<-chan *ChatChunk, error) {
	return p.ch, nil
}
func (p *streamProvider) HealthCheck(_ context.Context) error { return nil }

type errStreamProvider struct {
	name string
}

func (p *errStreamProvider) Name() string { return p.name }
func (p *errStreamProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return nil, fmt.Errorf("fail")
}
func (p *errStreamProvider) Stream(_ context.Context, _ *ChatRequest) (<-chan *ChatChunk, error) {
	return nil, fmt.Errorf("stream fail")
}
func (p *errStreamProvider) HealthCheck(_ context.Context) error { return nil }

// --- Attempt with circuit breaker ---

func (p *blockingStreamProvider) Name() string { return p.name }
func (p *blockingStreamProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return &ChatResponse{}, nil
}
func (p *blockingStreamProvider) Stream(ctx context.Context, _ *ChatRequest) (<-chan *ChatChunk, error) {
	ch := make(chan *ChatChunk, 1)
	go func() {
		defer close(ch)
		<-ctx.Done()
	}()
	return ch, nil
}
func (p *blockingStreamProvider) HealthCheck(_ context.Context) error { return nil }

// --- Constructors and Name() ---

func TestNewGroq(t *testing.T) {
	g := NewGroq("key")
	if g.Name() != "groq" {
		t.Errorf("Name() = %q", g.Name())
	}
	if g.apiKey != "key" {
		t.Error("apiKey not set")
	}
}

func TestNewMistral(t *testing.T) {
	m := NewMistral("key")
	if m.Name() != "mistral" {
		t.Errorf("Name() = %q", m.Name())
	}
}

func TestNewNVIDIANIM(t *testing.T) {
	n := NewNVIDIANIM("key")
	if n.Name() != "nvidia_nim" {
		t.Errorf("Name() = %q", n.Name())
	}
}

func TestNewOpenRouter(t *testing.T) {
	o := NewOpenRouter("key")
	if o.Name() != "openrouter" {
		t.Errorf("Name() = %q", o.Name())
	}
}

func TestNewCohere(t *testing.T) {
	c := NewCohere("key")
	if c.Name() != "cohere" {
		t.Errorf("Name() = %q", c.Name())
	}
}

// --- Constructor coverage (Name already tested above) ---

func TestNewOpenAI(t *testing.T) {
	o := NewOpenAI("test-key")
	if o.Name() != "openai" {
		t.Errorf("Name() = %q", o.Name())
	}
	if o.apiKey != "test-key" {
		t.Error("apiKey not set")
	}
}

func TestCalculateOpenAICost_UnknownModel(t *testing.T) {
	cost := calculateOpenAICost("nonexistent-model", 1000, 500)
	if cost != 0 {
		t.Errorf("expected 0 for unknown model, got %f", cost)
	}
}

// --- Cost function branches ---

func TestCalculateOpenRouterCost_UnknownModel(t *testing.T) {
	cost := calculateOpenRouterCost("unknown-model", 1000, 500)
	if cost <= 0 {
		t.Errorf("expected positive fallback cost, got %f", cost)
	}
}

func TestCalculateCohereCost_KnownModels(t *testing.T) {
	costs := []struct {
		model string
	}{
		{"command-r-plus"},
		{"command-r"},
		{"command"},
		{"unknown-model"}, // fallback
	}
	for _, tc := range costs {
		cost := calculateCohereCost(tc.model, 1000, 500)
		if cost <= 0 {
			t.Errorf("calculateCohereCost(%q) = %f, want > 0", tc.model, cost)
		}
	}
}

func TestCalculateGroqCost_KnownModels(t *testing.T) {
	models := []string{"llama-3.1-70b-versatile", "llama-3.1-8b-instant", "mixtral-8x7b-32768", "gemma2-9b-it", "unknown"}
	for _, m := range models {
		cost := calculateGroqCost(m, 1000, 500)
		if cost <= 0 {
			t.Errorf("calculateGroqCost(%q) = %f, want > 0", m, cost)
		}
	}
}

func TestCalculateMistralCost_AllBranches(t *testing.T) {
	models := []string{"mistral-large-latest", "mistral-small-latest", "open-mistral-n7b", "unknown"}
	for _, m := range models {
		cost := calculateMistralCost(m, 1000, 500)
		if cost <= 0 {
			t.Errorf("calculateMistralCost(%q) = %f, want > 0", m, cost)
		}
	}
}

func TestCalculateNIMCost_AllBranches(t *testing.T) {
	models := []string{"meta/llama-3.1-405b-instruct", "unknown"}
	for _, m := range models {
		cost := calculateNIMCost(m, 1000, 500)
		if cost <= 0 {
			t.Errorf("calculateNIMCost(%q) = %f, want > 0", m, cost)
		}
	}
}

// --- Circuit breaker half-open probe limit ---

func TestGroqHealthCheck_NetworkError(t *testing.T) {
	g := &GroqAdapter{
		apiKey:     "key",
		httpClient: &http.Client{Timeout: 1 * time.Nanosecond},
	}
	err := g.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMistralHealthCheck_NetworkError(t *testing.T) {
	m := &MistralAdapter{
		apiKey:     "key",
		httpClient: &http.Client{Timeout: 1 * time.Nanosecond},
	}
	err := m.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestNVIDIANIMHealthCheck_NetworkError(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey:     "key",
		httpClient: &http.Client{Timeout: 1 * time.Nanosecond},
	}
	err := n.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenRouterHealthCheck_NetworkError(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey:     "key",
		httpClient: &http.Client{Timeout: 1 * time.Nanosecond},
	}
	err := o.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestCohereHealthCheck_NetworkError(t *testing.T) {
	c := &CohereAdapter{
		apiKey:     "key",
		httpClient: &http.Client{Timeout: 1 * time.Nanosecond},
	}
	err := c.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
}

// --- Cache Set nil response ---

func TestCacheSet_NilResponse(t *testing.T) {
	c := NewInMemoryCache(time.Minute)
	c.Set("key", nil)
	_, ok := c.Get("key")
	if ok {
		t.Error("nil response should not be cached")
	}
}

// --- Gemini buildGeminiContents edge cases ---

func TestBuildGeminiContents_Empty(t *testing.T) {
	contents := buildGeminiContents(nil)
	if len(contents) != 0 {
		t.Errorf("expected empty, got %d", len(contents))
	}
}

func TestBuildGeminiContents_AssistantRole(t *testing.T) {
	contents := buildGeminiContents([]Message{
		{Role: "assistant", Content: "hi"},
	})
	if len(contents) != 1 {
		t.Fatalf("expected 1, got %d", len(contents))
	}
	if contents[0].Role != "model" {
		t.Errorf("expected role 'model', got %q", contents[0].Role)
	}
}

// --- SetPrices with actual values ---

func TestSetPrices_WithValues(t *testing.T) {
	r := NewModelRouter(nil)
	prices := map[string]ModelInfo{
		"test-model": {Provider: "test", InputCostPer1K: 0.01, OutputCostPer1K: 0.02},
	}
	r.SetPrices(prices)
	pt := r.priceTable()
	if _, ok := pt["test-model"]; !ok {
		t.Error("prices not set")
	}
}

// --- CheckHealth with failing provider ---

func TestCheckHealth_WithFailingProvider(t *testing.T) {
	mon := NewHealthMonitor()
	mon.RegisterProvider("fail", &errStreamProvider{name: "fail"})
	mon.RecordFailure("fail")
	mon.RecordFailure("fail")
	mon.RecordFailure("fail")
	// After 3 failures, status should be Down
	conf := mon.Confidence("fail")
	if conf > 0.1 {
		t.Errorf("failing provider confidence = %f, want low", conf)
	}
}

// --- classifyComplexity more branches ---

func TestClassifyComplexity_RenameType(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{Type: "rename"}
	c := r.classifyComplexity(task)
	if c < 0.05 || c > 0.2 {
		t.Errorf("rename complexity = %f, want ~0.1", c)
	}
}

func TestClassifyComplexity_DocType(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{Type: "documentation"}
	c := r.classifyComplexity(task)
	if c < 0.05 || c > 0.2 {
		t.Errorf("documentation complexity = %f", c)
	}
}

func TestClassifyComplexity_SmallFeature(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{Type: "small_feature"}
	c := r.classifyComplexity(task)
	if c < 0.2 || c > 0.5 {
		t.Errorf("small_feature complexity = %f", c)
	}
}

func TestClassifyComplexity_Feature(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{Type: "feature"}
	c := r.classifyComplexity(task)
	if c < 0.4 || c > 0.7 {
		t.Errorf("feature complexity = %f", c)
	}
}

func TestClassifyComplexity_Architecture(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{Type: "architecture"}
	c := r.classifyComplexity(task)
	if c < 0.6 || c > 0.85 {
		t.Errorf("architecture complexity = %f", c)
	}
}

func TestClassifyComplexity_FileCounts(t *testing.T) {
	tests := []struct {
		name  string
		files int
		min   float64
		max   float64
	}{
		{"1 file", 1, 0.25, 0.35},
		{"3 files", 3, 0.35, 0.45},
		{"7 files", 7, 0.45, 0.55},
		{"15 files", 15, 0.55, 0.65},
	}
	r := NewModelRouter(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := make([]string, tt.files)
			for i := range files {
				files[i] = "f.go"
			}
			task := &Task{Type: "bug_fix", FilesChanged: files}
			c := r.classifyComplexity(task)
			if float64(c) < tt.min || float64(c) > tt.max {
				t.Errorf("complexity = %f, want [%f, %f]", c, tt.min, tt.max)
			}
		})
	}
}

func TestClassifyComplexity_ReasoningAndNovel(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{Type: "bug_fix", RequiresReasoning: true, IsNovel: true}
	c := r.classifyComplexity(task)
	if float64(c) < 0.5 {
		t.Errorf("reasoning+novel complexity = %f, want >= 0.5", c)
	}
}

func TestClassifyComplexity_SecurityTag(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{Type: "bug_fix", Tags: []string{"security"}}
	c := r.classifyComplexity(task)
	if float64(c) < 0.5 {
		t.Errorf("security tag complexity = %f, want >= 0.5", c)
	}
}

func TestClassifyComplexity_ProductionTag(t *testing.T) {
	r := NewModelRouter(nil)
	task := &Task{Type: "bug_fix", Tags: []string{"production"}}
	c := r.classifyComplexity(task)
	if float64(c) < 0.5 {
		t.Errorf("production tag complexity = %f, want >= 0.5", c)
	}
}

// --- rankCandidates filtering ---

func TestRankCandidates_NoHealthyProviders(t *testing.T) {
	r := NewModelRouter(nil)
	// Register no providers, but list one as healthy
	task := &Task{Type: "formatting", Messages: []Message{{Content: "hi"}}}
	result := r.rankCandidates(task, []string{"unknown"}, ComplexitySimple)
	if len(result) != 0 {
		t.Errorf("expected 0 candidates, got %d", len(result))
	}
}

// --- calculateOpenAICost with known model ---

func TestCalculateOpenAICost_KnownModel(t *testing.T) {
	cost := calculateOpenAICost("gpt-4o-mini", 1000, 500)
	if cost <= 0 {
		t.Errorf("expected positive cost for known model, got %f", cost)
	}
}

// --- provider_http_test.go content below ---

// mockTransport is a custom RoundTripper that redirects all requests to a test server.
type mockTransport struct {
	handler http.HandlerFunc
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	w := httptest.NewRecorder()
	t.handler(w, req)
	return w.Result(), nil
}

func newMockHTTPClient(handler http.HandlerFunc) *http.Client {
	return &http.Client{
		Transport: &mockTransport{handler: handler},
		Timeout:   10 * time.Second,
	}
}

// --- Groq ---

func TestGroq_Chat_Success(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "test-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/openai/v1/chat/completions" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if !strings.Contains(r.Header.Get("Authorization"), "Bearer test-key") {
				t.Error("missing auth header")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("Hello from Groq", 10, 20))
		}),
	}

	resp, err := g.Chat(context.Background(), &ChatRequest{
		Model:    "llama-3.1-70b-versatile",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello from Groq" {
		t.Errorf("wrong content: %q", resp.Content)
	}
	if resp.Provider != "groq" {
		t.Errorf("wrong provider: %q", resp.Provider)
	}
	if resp.InputTokens != 10 || resp.OutputTokens != 20 {
		t.Errorf("wrong tokens: in=%d out=%d", resp.InputTokens, resp.OutputTokens)
	}
	if resp.Cost <= 0 {
		t.Error("expected positive cost")
	}
}

func TestGroq_Chat_EmptyChoices(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`))
		}),
	}
	resp, err := g.Chat(context.Background(), &ChatRequest{
		Model: "llama-3.1-70b-versatile", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
}

func TestGroq_Chat_ErrorStatus(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}),
	}
	_, err := g.Chat(context.Background(), &ChatRequest{
		Model: "llama-3.1-70b-versatile", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestGroq_HealthCheck_Success(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/openai/v1/models" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := g.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestGroq_HealthCheck_Error(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}),
	}
	err := g.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestGroq_Stream_Success(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			chunks := []string{"Hello", " World"}
			for _, c := range chunks {
				ev := map[string]interface{}{
					"choices": []map[string]interface{}{
						{"delta": map[string]interface{}{"content": c}},
					},
				}
				b, _ := json.Marshal(ev)
				w.Write(b)
				w.Write([]byte("\n"))
			}
		}),
	}

	ch, err := g.Stream(context.Background(), &ChatRequest{
		Model: "llama-3.1-70b-versatile", Messages: []Message{{Role: "user", Content: "hi"}},
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
	if content != "Hello World" {
		t.Errorf("wrong content: %q", content)
	}
}

func TestGroq_Stream_ErrorStatus(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}),
	}
	_, err := g.Stream(context.Background(), &ChatRequest{
		Model: "llama-3.1-70b-versatile", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestGroq_Chat_WithSystemAndTools(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var req map[string]interface{}
			json.Unmarshal(body, &req)
			msgs := req["messages"].([]interface{})
			if len(msgs) < 2 {
				t.Errorf("expected system + user messages, got %d", len(msgs))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 5, 5))
		}),
	}
	_, err := g.Chat(context.Background(), &ChatRequest{
		Model:    "llama-3.1-70b-versatile",
		System:   "be helpful",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// --- Mistral ---

func TestMistral_Chat_Success(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "test-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/chat/completions" {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("Hello from Mistral", 10, 20))
		}),
	}

	resp, err := m.Chat(context.Background(), &ChatRequest{
		Model:    "mistral-large-latest",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello from Mistral" {
		t.Errorf("wrong content: %q", resp.Content)
	}
	if resp.Provider != "mistral" {
		t.Errorf("wrong provider: %q", resp.Provider)
	}
}

func TestMistral_Chat_EmptyChoices(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`))
		}),
	}
	resp, err := m.Chat(context.Background(), &ChatRequest{
		Model: "mistral-large-latest", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
}

func TestMistral_Chat_ErrorStatus(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}),
	}
	_, err := m.Chat(context.Background(), &ChatRequest{
		Model: "mistral-large-latest", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestMistral_HealthCheck_Success(t *testing.T) {
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

func TestMistral_HealthCheck_Error(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}),
	}
	err := m.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestMistral_Stream_Success(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "k",
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
	ch, err := m.Stream(context.Background(), &ChatRequest{
		Model: "mistral-large-latest", Messages: []Message{{Role: "user", Content: "hi"}},
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

func TestMistral_Stream_ErrorStatus(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}),
	}
	_, err := m.Stream(context.Background(), &ChatRequest{
		Model: "mistral-large-latest", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

// --- NVIDIA NIM ---

func TestNVIDIANIM_Chat_Success(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey:  "test-key",
		baseURL: "https://mock.nvidia.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/chat/completions") {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("Hello from NIM", 10, 20))
		}),
	}

	resp, err := n.Chat(context.Background(), &ChatRequest{
		Model:    "nvidia/llama-3.1-405b-instruct",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello from NIM" {
		t.Errorf("wrong content: %q", resp.Content)
	}
	if resp.Provider != "nvidia_nim" {
		t.Errorf("wrong provider: %q", resp.Provider)
	}
}

func TestNVIDIANIM_Chat_EmptyChoices(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`))
		}),
	}
	resp, err := n.Chat(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
}

func TestNVIDIANIM_Chat_ErrorStatus(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}),
	}
	_, err := n.Chat(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestNVIDIANIM_HealthCheck_Success(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := n.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestNVIDIANIM_HealthCheck_Error(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}),
	}
	err := n.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestNVIDIANIM_Stream_Success(t *testing.T) {
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

func TestNVIDIANIM_Stream_ErrorStatus(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}),
	}
	_, err := n.Stream(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

// --- OpenRouter ---

func TestOpenRouter_Chat_Success(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "test-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/chat/completions") {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			if r.Header.Get("HTTP-Referer") != "https://vigilagent.com" {
				t.Error("missing HTTP-Referer header")
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("Hello from OpenRouter", 10, 20))
		}),
	}

	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello from OpenRouter" {
		t.Errorf("wrong content: %q", resp.Content)
	}
	if resp.Provider != "openrouter" {
		t.Errorf("wrong provider: %q", resp.Provider)
	}
}

func TestOpenRouter_Chat_EmptyChoices(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`))
		}),
	}
	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
}

func TestOpenRouter_Chat_ErrorStatus(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}),
	}
	_, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestOpenRouter_HealthCheck_Success(t *testing.T) {
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

func TestOpenRouter_HealthCheck_Error(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}),
	}
	err := o.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestOpenRouter_Stream_Success(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
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
	ch, err := o.Stream(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
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

func TestOpenRouter_Stream_ErrorStatus(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}),
	}
	_, err := o.Stream(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

// --- Cohere ---

func TestCohere_Chat_Success(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "test-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			if !strings.Contains(r.URL.Path, "/v2/chat") {
				t.Errorf("unexpected path: %s", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"message": map[string]interface{}{
					"content": []map[string]interface{}{
						{"text": "Hello from Cohere"},
					},
				},
				"meta": map[string]interface{}{
					"tokens": map[string]interface{}{
						"input_tokens":  10,
						"output_tokens": 20,
					},
				},
			}
			b, _ := json.Marshal(resp)
			w.Write(b)
		}),
	}

	resp, err := c.Chat(context.Background(), &ChatRequest{
		Model:    "command-r-plus",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello from Cohere" {
		t.Errorf("wrong content: %q", resp.Content)
	}
	if resp.Provider != "cohere" {
		t.Errorf("wrong provider: %q", resp.Provider)
	}
}

func TestCohere_Chat_ErrorStatus(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}),
	}
	_, err := c.Chat(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestCohere_Chat_BadJSON(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("not json"))
		}),
	}
	_, err := c.Chat(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error for bad JSON")
	}
}

func TestCohere_HealthCheck_Success(t *testing.T) {
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

func TestCohere_HealthCheck_Error(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		}),
	}
	err := c.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestCohere_Stream_Success(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			// Cohere uses double-nested delta: delta.delta.content.text
			events := []map[string]interface{}{
				{"type": "content-delta", "delta": map[string]interface{}{
					"delta": map[string]interface{}{
						"content": map[string]interface{}{"text": "Hello"},
					},
				}},
				{"type": "content-delta", "delta": map[string]interface{}{
					"delta": map[string]interface{}{
						"content": map[string]interface{}{"text": " World"},
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
	if content != "Hello World" {
		t.Errorf("wrong content: %q", content)
	}
}

func TestCohere_Stream_ErrorStatus(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte("forbidden"))
		}),
	}
	_, err := c.Stream(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 403")
	}
}

func TestCohere_Stream_EOF(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			// No data - just close
		}),
	}
	ch, err := c.Stream(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Should get finish from deferred function
	select {
	case chunk := <-ch:
		if !chunk.Finish {
			t.Error("expected finish on EOF")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestCohere_Chat_WithSystem(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			var req map[string]interface{}
			json.Unmarshal(body, &req)
			msgs := req["messages"].([]interface{})
			if len(msgs) < 2 {
				t.Errorf("expected system + user messages, got %d", len(msgs))
			}
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"message": map[string]interface{}{
					"content": []map[string]interface{}{{"text": "ok"}},
				},
				"meta": map[string]interface{}{
					"tokens": map[string]interface{}{"input_tokens": 5, "output_tokens": 5},
				},
			}
			b, _ := json.Marshal(resp)
			w.Write(b)
		}),
	}
	_, err := c.Chat(context.Background(), &ChatRequest{
		Model:    "command-r-plus",
		System:   "be helpful",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

// --- OpenAI (using mock transport to intercept go-openai client) ---

func TestOpenAI_Chat_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("Hello from OpenAI", 10, 20))
	}))
	defer ts.Close()

	// go-openai respects OPENAI_API_BASE env, but we can't set it per-test easily.
	// Instead, create the adapter with a custom client.
	o := &OpenAIAdapter{
		apiKey: "test-key",
		client: openaiClientWithBaseURL(ts.URL + "/v1"),
	}

	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "Hello from OpenAI" {
		t.Errorf("wrong content: %q", resp.Content)
	}
	if resp.Provider != "openai" {
		t.Errorf("wrong provider: %q", resp.Provider)
	}
}

func TestOpenAI_Chat_WithTools(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if _, ok := req["tools"]; !ok {
			t.Error("tools should be present in request")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 5, 5))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	_, err := o.Chat(context.Background(), &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "hi"}},
		Tools:    []ToolDef{{Name: "search", Description: "search the web"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenAI_Chat_EmptyChoices(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[],"usage":{"prompt_tokens":0,"completion_tokens":0}}`))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "" {
		t.Errorf("expected empty content, got %q", resp.Content)
	}
}

func TestOpenAI_Chat_ErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	_, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestOpenAI_Chat_WithSystemPrompt(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		msgs := req["messages"].([]interface{})
		if len(msgs) < 2 {
			t.Errorf("expected system + user, got %d messages", len(msgs))
		}
		first := msgs[0].(map[string]interface{})
		if first["role"] != "system" {
			t.Errorf("first message should be system, got %v", first["role"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 5, 5))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	_, err := o.Chat(context.Background(), &ChatRequest{
		Model:    "gpt-4o",
		System:   "be helpful",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenAI_Chat_WithToolCalls(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": "",
						"tool_calls": []map[string]interface{}{
							{
								"id":   "call_123",
								"type": "function",
								"function": map[string]interface{}{
									"name":      "search",
									"arguments": `{"query":"test"}`,
								},
							},
						},
					},
					"finish_reason": "tool_calls",
				},
			},
			"usage": map[string]interface{}{
				"prompt_tokens":     5,
				"completion_tokens": 5,
			},
			"model": "gpt-4o",
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "search for test"}},
		Tools:    []ToolDef{{Name: "search", Description: "search"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.ToolCalls) == 0 {
		t.Error("expected tool calls in response")
	}
}

func TestOpenAI_HealthCheck_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 1, 1))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	err := o.HealthCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
}

func TestOpenAI_HealthCheck_ContentPolicy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("content_policy violation"))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	err := o.HealthCheck(context.Background())
	if err != nil {
		t.Fatal("content_policy error should be ignored in health check")
	}
}

func TestOpenAI_HealthCheck_Safety(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("safety filter triggered"))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	err := o.HealthCheck(context.Background())
	if err != nil {
		t.Fatal("safety error should be ignored in health check")
	}
}

func TestOpenAI_HealthCheck_Error(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("server error"))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	err := o.HealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestOpenAI_Stream_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		events := []map[string]interface{}{
			{"choices": []map[string]interface{}{{"delta": map[string]interface{}{"content": "Hello"}, "finish_reason": nil}}},
			{"choices": []map[string]interface{}{{"delta": map[string]interface{}{"content": " World"}, "finish_reason": "stop"}}},
		}
		for _, e := range events {
			b, _ := json.Marshal(e)
			fmt.Fprintf(w, "data: %s\n\n", b)
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
	if content != "Hello World" {
		t.Errorf("wrong content: %q", content)
	}
}

func TestOpenAI_Stream_ErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("error"))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	_, err := o.Stream(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error on 500")
	}
}

func TestOpenAI_Chat_ConvertMessages(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		msgs := req["messages"].([]interface{})
		// system + user + assistant = 3
		if len(msgs) != 3 {
			t.Errorf("expected 3 messages, got %d", len(msgs))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 5, 5))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	_, err := o.Chat(context.Background(), &ChatRequest{
		Model:  "gpt-4o",
		System: "be helpful",
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello", ToolCallID: "call_123"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
}
