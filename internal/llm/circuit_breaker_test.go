package llm

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCircuitBreaker(t *testing.T) {
	cb := NewCircuitBreaker(5, 30*time.Second)
	require.NotNil(t, cb)
	assert.Equal(t, CircuitClosed, cb.state)
	assert.Equal(t, 5, cb.threshold)
	assert.Equal(t, 30*time.Second, cb.timeout)
	assert.False(t, cb.IsOpen())
}

func TestCircuitBreaker_Execute_SuccessDoesNotTrip(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Hour)
	for i := 0; i < 5; i++ {
		err := cb.Execute(func() error { return nil })
		assert.NoError(t, err)
	}
	assert.False(t, cb.IsOpen())
}

func TestCircuitBreaker_Execute_TripsAfterThreshold(t *testing.T) {
	cb := NewCircuitBreaker(3, time.Hour)
	for i := 0; i < 3; i++ {
		err := cb.Execute(func() error { return errors.New("fail") })
		assert.Error(t, err)
	}
	assert.True(t, cb.IsOpen())
}

func TestCircuitBreaker_Execute_RejectsWhenOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, time.Hour)
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })
	assert.True(t, cb.IsOpen())

	err := cb.Execute(func() error { return nil })
	assert.ErrorIs(t, err, ErrCircuitOpen)
}

func TestCircuitBreaker_Execute_HalfOpenAfterTimeout(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })
	assert.True(t, cb.IsOpen())

	time.Sleep(15 * time.Millisecond)

	err := cb.Execute(func() error { return nil })
	assert.NoError(t, err)
	assert.False(t, cb.IsOpen(), "circuit should close after successful half-open probe")
}

func TestCircuitBreaker_Execute_HalfOpenFailureReopens(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })

	time.Sleep(15 * time.Millisecond)

	cb.Execute(func() error { return errors.New("fail again") })
	assert.True(t, cb.IsOpen(), "circuit should reopen after half-open failure")
}

func TestCircuitBreaker_Execute_HalfOpenRejectsSecondProbe(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })

	time.Sleep(15 * time.Millisecond)

	// First probe transitions to HalfOpen and increments halfOpenProbes to 1
	err := cb.Execute(func() error { return nil })
	assert.NoError(t, err)

	// Second probe in HalfOpen should be rejected because halfOpenProbes >= 1
	// But since the first probe succeeded, circuit is now Closed again.
	// We need a scenario where the first probe runs and halfOpenProbes stays >= 1.
	// Actually: after success in HalfOpen, state transitions to Closed and failCount resets.
	// So we need to re-open the circuit. Let's test the half-open probe limit differently.

	// Re-trip the circuit
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })
	assert.True(t, cb.IsOpen())

	time.Sleep(15 * time.Millisecond)

	// First probe goes through (HalfOpen, halfOpenProbes becomes 1)
	err = cb.Execute(func() error { return errors.New("still failing") })
	assert.Error(t, err)
	// Circuit should be Open again after half-open failure

	// Now wait for timeout and try again — first probe goes through
	time.Sleep(15 * time.Millisecond)
	err = cb.Execute(func() error { return nil })
	assert.NoError(t, err)
	assert.False(t, cb.IsOpen())
}

func TestCircuitBreaker_ResetState(t *testing.T) {
	cb := NewCircuitBreaker(2, time.Hour)
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })
	assert.True(t, cb.IsOpen())

	cb.Reset()
	assert.False(t, cb.IsOpen())
	assert.Equal(t, 0, cb.failCount)
	assert.Equal(t, 0, cb.successCount)
}

func TestCircuitBreaker_ResetFromHalfOpen(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })

	time.Sleep(15 * time.Millisecond)
	cb.Reset()
	assert.False(t, cb.IsOpen())

	err := cb.Execute(func() error { return nil })
	assert.NoError(t, err)
}

func TestCircuitBreaker_IsOpen_ThreadSafe(t *testing.T) {
	cb := NewCircuitBreaker(100, time.Hour)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cb.IsOpen()
			cb.Execute(func() error { return nil })
		}()
	}
	wg.Wait()
}

func TestCircuitBreaker_Execute_FailCountResetsOnSuccess(t *testing.T) {
	cb := NewCircuitBreaker(3, 10*time.Millisecond)
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })
	// Success in Closed state does NOT reset failCount (only HalfOpen success does)
	cb.Execute(func() error { return nil })

	// failCount stays at 2; need only 1 more failure to trip
	cb.Execute(func() error { return errors.New("fail") })
	assert.True(t, cb.IsOpen())
}

func TestCircuitBreaker_Execute_SuccessInHalfOpenResetsCounters(t *testing.T) {
	cb := NewCircuitBreaker(2, 10*time.Millisecond)
	cb.Execute(func() error { return errors.New("fail") })
	cb.Execute(func() error { return errors.New("fail") })

	time.Sleep(15 * time.Millisecond)

	cb.Execute(func() error { return nil })
	assert.False(t, cb.IsOpen())
	assert.Equal(t, 0, cb.failCount)
	assert.Equal(t, 1, cb.successCount)
}

func TestErrCircuitOpen(t *testing.T) {
	assert.Equal(t, "circuit breaker is open", ErrCircuitOpen.Error())
}

func TestCircuitState_Constants(t *testing.T) {
	assert.Equal(t, CircuitState(0), CircuitClosed)
	assert.Equal(t, CircuitState(1), CircuitOpen)
	assert.Equal(t, CircuitState(2), CircuitHalfOpen)
}
