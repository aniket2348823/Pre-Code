package llm

import (
	"errors"
	"sync"
	"time"
)

// ErrCircuitOpen is returned when the circuit breaker is open.
var ErrCircuitOpen = errors.New("circuit breaker is open")

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed   CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

// CircuitBreaker prevents cascade failures by stopping requests to failing providers.
type CircuitBreaker struct {
	state        CircuitState
	failCount    int
	successCount int
	lastFailure  time.Time
	threshold    int
	timeout      time.Duration
	mu           sync.RWMutex
}

// NewCircuitBreaker creates a new circuit breaker.
func NewCircuitBreaker(threshold int, timeout time.Duration) *CircuitBreaker {
	return &CircuitBreaker{
		state:     CircuitClosed,
		threshold: threshold,
		timeout:   timeout,
	}
}

// Execute runs a function with circuit breaker protection.
func (cb *CircuitBreaker) Execute(fn func() error) error {
	cb.mu.RLock()
	state := cb.state
	cb.mu.RUnlock()

	if state == CircuitOpen {
		if time.Since(cb.lastFailure) > cb.timeout {
			cb.mu.Lock()
			cb.state = CircuitHalfOpen
			cb.mu.Unlock()
		} else {
			return ErrCircuitOpen
		}
	}

	err := fn()

	cb.mu.Lock()
	defer cb.mu.Unlock()

	if err != nil {
		cb.failCount++
		cb.lastFailure = time.Now()
		if cb.failCount >= cb.threshold {
			cb.state = CircuitOpen
		}
		return err
	}

	cb.successCount++
	if cb.state == CircuitHalfOpen {
		cb.state = CircuitClosed
		cb.failCount = 0
	}
	return nil
}

// IsOpen returns whether the circuit is open.
func (cb *CircuitBreaker) IsOpen() bool {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.state == CircuitOpen
}

// Reset resets the circuit breaker to closed state.
func (cb *CircuitBreaker) Reset() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.state = CircuitClosed
	cb.failCount = 0
	cb.successCount = 0
}
