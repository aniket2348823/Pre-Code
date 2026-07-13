package llm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

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

func TestExecuteWithFailover_AllFail(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai", err: errProviderDown})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	_, err := r.ExecuteWithFailover(context.Background(), simpleTask())
	if err == nil {
		t.Error("expected error when all providers fail")
	}
}

func TestExecuteWithFailover_ContextCancel(t *testing.T) {
	r := NewModelRouter(nil)
	// Use a provider that respects context cancellation
	r.RegisterProvider("openai", &contextAwareProvider{name: "openai", resp: &ChatResponse{Content: "ok", Cost: 0.01}})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, err := r.ExecuteWithFailover(ctx, simpleTask())
	if err == nil {
		t.Error("expected error with cancelled context")
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
