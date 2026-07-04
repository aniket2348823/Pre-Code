// Package retry provides configurable retry logic with exponential backoff,
// jitter, and circuit-breaker-aware retries for LLM API calls and other
// external service interactions.
package retry

import (
	"context"
	"math"
	"math/rand"
	"sync"
	"time"
)

// Policy defines retry behavior.
type Policy struct {
	MaxRetries  int           `json:"max_retries"`
	BaseDelay   time.Duration `json:"base_delay"`
	MaxDelay    time.Duration `json:"max_delay"`
	Multiplier  float64       `json:"multiplier"`
	JitterPct   float64       `json:"jitter_pct"` // 0-1, percentage of jitter
	RetryableFn func(err error) bool `json:"-"`     // custom retryable check
}

// DefaultPolicy returns a sensible default retry policy.
func DefaultPolicy() Policy {
	return Policy{
		MaxRetries: 3,
		BaseDelay:  100 * time.Millisecond,
		MaxDelay:   5 * time.Second,
		Multiplier: 2.0,
		JitterPct:  0.1,
	}
}

// RetryablePolicy retries on any error.
func RetryablePolicy() Policy {
	p := DefaultPolicy()
	p.MaxRetries = 5
	return p
}

// IsRetryable checks if an error should trigger a retry.
func (p Policy) IsRetryable(err error) bool {
	if p.RetryableFn != nil {
		return p.RetryableFn(err)
	}
	return true // retry all errors by default
}

// Attempt tracks retry state for a single operation.
type Attempt struct {
	Count     int           `json:"count"`
	LastError error         `json:"-"`
	TotalTime time.Duration `json:"total_time"`
}

// Execute runs fn with retry logic. Returns the result or final error.
func Execute(ctx context.Context, policy Policy, fn func(ctx context.Context) error) error {
	attempt := &Attempt{}
	start := time.Now()

	for i := 0; i <= policy.MaxRetries; i++ {
		attempt.Count = i + 1

		// Check context cancellation
		select {
		case <-ctx.Done():
			attempt.TotalTime = time.Since(start)
			return ctx.Err()
		default:
		}

		err := fn(ctx)
		attempt.LastError = err
		attempt.TotalTime = time.Since(start)

		if err == nil {
			return nil
		}

		// Don't retry if not retryable
		if !policy.IsRetryable(err) {
			return err
		}

		// Don't sleep after last attempt
		if i < policy.MaxRetries {
			delay := policy.ComputeDelay(i)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	return attempt.LastError
}

// ComputeDelay calculates delay with exponential backoff and jitter for a given attempt.
func (p Policy) ComputeDelay(attempt int) time.Duration {
	delay := float64(p.BaseDelay) * math.Pow(p.Multiplier, float64(attempt))
	if delay > float64(p.MaxDelay) {
		delay = float64(p.MaxDelay)
	}

	// Apply jitter
	if p.JitterPct > 0 {
		jitter := delay * p.JitterPct
		delay = delay - jitter + rand.Float64()*2*jitter
	}

	return time.Duration(delay)
}

// Stats tracks retry statistics across all operations.
type Stats struct {
	mu            sync.RWMutex
	totalAttempts int64
	totalRetries  int64
	totalSuccess  int64
	totalFailures int64
}

// NewStats creates a new stats tracker.
func NewStats() *Stats {
	return &Stats{}
}

// RecordSuccess records a successful attempt.
func (s *Stats) RecordSuccess(retries int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalAttempts += int64(retries + 1)
	s.totalRetries += int64(retries)
	s.totalSuccess++
}

// RecordFailure records a failed operation after all retries.
func (s *Stats) RecordFailure(retries int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.totalAttempts += int64(retries + 1)
	s.totalRetries += int64(retries)
	s.totalFailures++
}

// Summary returns retry statistics.
func (s *Stats) Summary() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var avgRetries float64
	total := s.totalSuccess + s.totalFailures
	if total > 0 {
		avgRetries = float64(s.totalRetries) / float64(total)
	}
	return map[string]interface{}{
		"total_successes": s.totalSuccess,
		"total_failures":  s.totalFailures,
		"total_attempts":  s.totalAttempts,
		"avg_retries":     avgRetries,
	}
}
