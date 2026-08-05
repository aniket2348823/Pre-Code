package llm

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// --- Failover, circuit breaker, and attempt tests ---
// (extracted from provider_test.go for parallel-compile speed)

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

func TestCircuitBreaker_Reset(t *testing.T) {
	cb := NewCircuitBreaker(2, time.Hour)
	cb.Execute(func() error { return fmt.Errorf("fail") })
	cb.Execute(func() error { return fmt.Errorf("fail") })
	if !cb.IsOpen() {
		t.Fatal("circuit should be open")
	}
	cb.Reset()
	if cb.IsOpen() {
		t.Fatal("circuit should be closed after reset")
	}
}

func TestCircuitBreaker_HalfOpenRecovery(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)
	cb.Execute(func() error { return fmt.Errorf("fail") })
	cb.Execute(func() error { return fmt.Errorf("fail") })
	time.Sleep(20 * time.Millisecond)
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("half-open should succeed, got: %v", err)
	}
	if cb.IsOpen() {
		t.Fatal("circuit should be closed after recovery")
	}
}

func TestCircuitBreaker_HalfOpenFailure(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)
	cb.Execute(func() error { return fmt.Errorf("fail") })
	cb.Execute(func() error { return fmt.Errorf("fail") })
	time.Sleep(20 * time.Millisecond)
	cb.Execute(func() error { return fmt.Errorf("fail again") })
	if !cb.IsOpen() {
		t.Fatal("circuit should re-open after half-open failure")
	}
}

func TestCircuitBreaker_SuccessInClosed(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Hour)
	for i := 0; i < 2; i++ {
		cb.Execute(func() error { return nil })
	}
	if cb.IsOpen() {
		t.Fatal("circuit should stay closed")
	}
}

// --- provider.go extras ---

func TestStreamWithFailover_Success(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	ch := make(chan *ChatChunk, 2)
	ch <- &ChatChunk{Content: "hello", Finish: false}
	ch <- &ChatChunk{Finish: true}
	close(ch)

	sp := &streamProvider{name: "openai", ch: ch}
	r.providers["openai"] = sp

	result, err := r.StreamWithFailover(context.Background(), &Task{
		ID: "t1", Type: "formatting", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Model == "" {
		t.Error("expected model")
	}
	for range result.Ch {
	}
}

func TestStreamWithFailover_AllFail(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &errStreamProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	_, err := r.StreamWithFailover(context.Background(), &Task{
		ID: "t1", Type: "formatting", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

type streamProvider struct {
	name string
	ch   <-chan *ChatChunk
}

func TestAttempt_CircuitBreakerOpen(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai", resp: &ChatResponse{Content: "ok"}})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	cb := NewCircuitBreaker(2, time.Hour)
	r.circuitBreakers["openai"] = cb
	cb.Execute(func() error { return fmt.Errorf("fail") })
	cb.Execute(func() error { return fmt.Errorf("fail") })

	_, err := r.attempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err == nil {
		t.Fatal("expected circuit breaker error")
	}
}

func TestAttempt_UnregisteredProvider(t *testing.T) {
	r := NewModelRouter(nil)
	_, err := r.attempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "nonexistent", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStreamAttempt_CircuitBreakerOpen(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	cb := NewCircuitBreaker(2, time.Hour)
	r.circuitBreakers["openai"] = cb
	cb.Execute(func() error { return fmt.Errorf("fail") })
	cb.Execute(func() error { return fmt.Errorf("fail") })

	_, err := r.streamAttempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err == nil {
		t.Fatal("expected circuit breaker error")
	}
}

func TestStreamAttempt_UnregisteredProvider(t *testing.T) {
	r := NewModelRouter(nil)
	_, err := r.streamAttempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "nonexistent", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestStreamAttempt_BudgetBlocked(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	r.SetBudgetGuard(&fakeBudget{reject: fmt.Errorf("over budget")})

	_, err := r.streamAttempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err == nil {
		t.Fatal("expected budget error")
	}
}

func TestStreamAttempt_ProviderError(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &errStreamProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	_, err := r.streamAttempt(context.Background(), simpleTask(), FallbackOption{
		Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestExecuteWithFailover_FallbackToSecond(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai", err: fmt.Errorf("fail")})
	r.RegisterProvider("anthropic", &countingProvider{name: "anthropic", resp: &ChatResponse{Content: "ok", Cost: 0.01}})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	r.healthMonitor.RecordSuccess("anthropic", time.Millisecond)

	resp, err := r.ExecuteWithFailover(context.Background(), &Task{
		ID: "t1", Type: "formatting", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Errorf("wrong content: %q", resp.Content)
	}
}

func TestExecuteWithFailover_NilCacheNilBudget(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai", resp: &ChatResponse{Content: "ok", Cost: 0.01}})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	r.cache = nil
	r.budget = nil

	resp, err := r.ExecuteWithFailover(context.Background(), simpleTask())
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" {
		t.Errorf("wrong content: %q", resp.Content)
	}
}

func TestStreamAttempt_WithBudget(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	b := &fakeBudget{}
	r.SetBudgetGuard(b)

	ch := make(chan *ChatChunk, 2)
	ch <- &ChatChunk{Content: "hello"}
	ch <- &ChatChunk{Finish: true}
	close(ch)

	sp := &streamProvider{name: "openai", ch: ch}
	r.providers["openai"] = sp

	_, err := r.streamAttempt(context.Background(), &Task{
		ID: "t1", OrgID: "o1", Type: "formatting", Messages: []Message{{Role: "user", Content: "hi"}},
	}, FallbackOption{Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStreamAttempt_ContextCancellation(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	bp := &blockingStreamProvider{name: "openai"}
	r.providers["openai"] = bp

	_, err := r.streamAttempt(ctx, &Task{
		ID: "t1", OrgID: "o1", Type: "formatting", Messages: []Message{{Role: "user", Content: "hi"}},
	}, FallbackOption{Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
}

func TestStreamAttempt_WithCache(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)
	r.SetCache(NewInMemoryCache(time.Minute))

	ch := make(chan *ChatChunk, 1)
	ch <- &ChatChunk{Content: "cached", Finish: true}
	close(ch)

	sp := &streamProvider{name: "openai", ch: ch}
	r.providers["openai"] = sp

	task := &Task{ID: "t1", OrgID: "o1", Type: "formatting", Messages: []Message{{Role: "user", Content: "hi"}}}
	_, err := r.streamAttempt(context.Background(), task, FallbackOption{Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStreamAttempt_FinishFalseRecordsFailure(t *testing.T) {
	r := NewModelRouter(nil)
	r.RegisterProvider("openai", &countingProvider{name: "openai"})
	r.healthMonitor.RecordSuccess("openai", time.Millisecond)

	ch := make(chan *ChatChunk, 1)
	ch <- &ChatChunk{Content: "partial"}
	close(ch)

	sp := &streamProvider{name: "openai", ch: ch}
	r.providers["openai"] = sp

	_, err := r.streamAttempt(context.Background(), &Task{
		ID: "t1", OrgID: "o1", Type: "formatting", Messages: []Message{{Role: "user", Content: "hi"}},
	}, FallbackOption{Provider: "openai", Model: "gpt-4o-mini", EstCost: 0.001})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
}

type blockingStreamProvider struct {
	name string
}

func TestCircuitBreaker_HalfOpenProbeLimit(t *testing.T) {
	cb := NewCircuitBreaker(2, time.Hour)
	// Trip the breaker
	cb.Execute(func() error { return fmt.Errorf("fail") })
	cb.Execute(func() error { return fmt.Errorf("fail") })
	if !cb.IsOpen() {
		t.Fatal("circuit should be open")
	}
	// Wait for timeout to enter half-open
	cb.mu.Lock()
	cb.lastFailure = time.Now().Add(-2 * time.Hour)
	cb.mu.Unlock()

	// First probe in half-open should succeed
	err := cb.Execute(func() error { return nil })
	if err != nil {
		t.Fatalf("first half-open probe should succeed: %v", err)
	}

	// Now trip it again for probe-limit test
	cb.Execute(func() error { return fmt.Errorf("fail") })
	cb.Execute(func() error { return fmt.Errorf("fail") })
	cb.mu.Lock()
	cb.lastFailure = time.Now().Add(-2 * time.Hour)
	cb.mu.Unlock()

	// First probe enters half-open, increments halfOpenProbes
	cb.Execute(func() error { return nil })
	// Second call while still half-open should be blocked
	cb.mu.Lock()
	cb.state = CircuitHalfOpen
	cb.halfOpenProbes.Store(1)
	cb.mu.Unlock()
	err = cb.Execute(func() error { return nil })
	if err != ErrCircuitOpen {
		t.Fatalf("expected ErrCircuitOpen for second half-open probe, got %v", err)
	}
}

// --- Gemini buildGeminiContents edge cases ---

// --- HealthCheck error paths ---
