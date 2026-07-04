package retry

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

func TestExecuteSuccess(t *testing.T) {
	calls := int32(0)
	err := Execute(context.Background(), DefaultPolicy(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected 1 call, got %d", atomic.LoadInt32(&calls))
	}
}

func TestExecuteRetryThenSuccess(t *testing.T) {
	calls := int32(0)
	err := Execute(context.Background(), DefaultPolicy(), func(ctx context.Context) error {
		n := atomic.AddInt32(&calls, 1)
		if n < 3 {
			return fmt.Errorf("transient error")
		}
		return nil
	})
	if err != nil {
		t.Errorf("expected nil error after retries, got %v", err)
	}
	if atomic.LoadInt32(&calls) != 3 {
		t.Errorf("expected 3 calls, got %d", atomic.LoadInt32(&calls))
	}
}

func TestExecuteMaxRetries(t *testing.T) {
	calls := int32(0)
	err := Execute(context.Background(), DefaultPolicy(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return fmt.Errorf("persistent error")
	})
	if err == nil {
		t.Error("expected error after max retries")
	}
	// MaxRetries=3 means 4 total attempts (1 initial + 3 retries)
	if atomic.LoadInt32(&calls) != 4 {
		t.Errorf("expected 4 calls (1+3 retries), got %d", atomic.LoadInt32(&calls))
	}
}

func TestExecuteNotRetryable(t *testing.T) {
	calls := int32(0)
	policy := DefaultPolicy()
	policy.RetryableFn = func(err error) bool {
		return false // never retry
	}
	err := Execute(context.Background(), policy, func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return fmt.Errorf("fatal error")
	})
	if err == nil {
		t.Error("expected error")
	}
	if atomic.LoadInt32(&calls) != 1 {
		t.Errorf("expected 1 call (no retries), got %d", atomic.LoadInt32(&calls))
	}
}

func TestExecuteContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := int32(0)
	cancel() // cancel immediately
	err := Execute(ctx, DefaultPolicy(), func(ctx context.Context) error {
		atomic.AddInt32(&calls, 1)
		return fmt.Errorf("error")
	})
	if err == nil {
		t.Error("expected context error")
	}
}

func TestComputeDelay(t *testing.T) {
	p := DefaultPolicy()
	d0 := p.ComputeDelay(0)
	d1 := p.ComputeDelay(1)
	d2 := p.ComputeDelay(2)
	// Each delay should roughly double
	if d1 <= d0 {
		t.Errorf("expected d1 > d0, got %v <= %v", d1, d0)
	}
	if d2 <= d1 {
		t.Errorf("expected d2 > d1, got %v <= %v", d2, d1)
	}
	// Should not exceed max
	if d2 > p.MaxDelay*2 { // allow for jitter
		t.Errorf("delay %v exceeds reasonable max", d2)
	}
}

func TestStats(t *testing.T) {
	s := NewStats()
	s.RecordSuccess(0)
	s.RecordSuccess(1)
	s.RecordFailure(3)
	summary := s.Summary()
	if summary["total_successes"] != int64(2) {
		t.Errorf("expected 2 successes, got %v", summary["total_successes"])
	}
	if summary["total_failures"] != int64(1) {
		t.Errorf("expected 1 failure, got %v", summary["total_failures"])
	}
}

func TestDefaultPolicy(t *testing.T) {
	p := DefaultPolicy()
	if p.MaxRetries != 3 {
		t.Errorf("expected 3 max retries, got %d", p.MaxRetries)
	}
	if p.Multiplier != 2.0 {
		t.Errorf("expected 2.0 multiplier, got %f", p.Multiplier)
	}
}
