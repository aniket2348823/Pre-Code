package llm

import (
	"context"
	"errors"
	"testing"
	"time"
)

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
