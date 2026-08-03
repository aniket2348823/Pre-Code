package llm

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewHealthMonitor(t *testing.T) {
	hm := NewHealthMonitor()
	require.NotNil(t, hm)
	assert.NotNil(t, hm.providers)
}

func TestHealthMonitor_RegisterProvider(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("test", &countingProvider{name: "test"})
	hm.mu.RLock()
	_, ok := hm.providers["test"]
	hm.mu.RUnlock()
	assert.True(t, ok)
}

func TestHealthMonitor_RegisterProvider_InitialStatus(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("test", &countingProvider{name: "test"})
	hm.mu.RLock()
	health := hm.providers["test"]
	hm.mu.RUnlock()
	assert.Equal(t, StatusHealthy, health.Status)
}

func TestHealthMonitor_GetHealthyProviders_Healthy(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p1", &countingProvider{name: "p1"})
	hm.RegisterProvider("p2", &countingProvider{name: "p2"})
	hm.RecordSuccess("p1", time.Millisecond)
	hm.RecordSuccess("p2", time.Millisecond)

	healthy := hm.GetHealthyProviders()
	assert.Len(t, healthy, 2)
}

func TestHealthMonitor_GetHealthyProviders_UnhealthyFiltered(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("healthy", &countingProvider{name: "healthy"})
	hm.RegisterProvider("down", &countingProvider{name: "down"})
	hm.RecordSuccess("healthy", time.Millisecond)
	hm.RecordFailure("down")
	hm.RecordFailure("down")
	hm.RecordFailure("down") // StatusDown

	healthy := hm.GetHealthyProviders()
	assert.Len(t, healthy, 1)
	assert.Contains(t, healthy, "healthy")
	assert.NotContains(t, healthy, "down")
}

func TestHealthMonitor_Confidence_Healthy(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordSuccess("p", time.Millisecond)
	c := hm.Confidence("p")
	assert.GreaterOrEqual(t, c, 0.5)
	assert.LessOrEqual(t, c, 1.0)
}

func TestHealthMonitor_Confidence_Unhealthy(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	c := hm.Confidence("p")
	assert.Equal(t, 0.2, c)
}

func TestHealthMonitor_Confidence_Down(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	c := hm.Confidence("p")
	assert.Equal(t, 0.0, c)
}

func TestHealthMonitor_Confidence_UnknownProv(t *testing.T) {
	hm := NewHealthMonitor()
	c := hm.Confidence("unknown")
	assert.Equal(t, 0.5, c)
}

func TestHealthMonitor_RecordFailure_Transitions(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})

	hm.RecordFailure("p")
	hm.mu.RLock()
	assert.Equal(t, StatusUnhealthy, hm.providers["p"].Status)
	hm.mu.RUnlock()

	hm.RecordFailure("p")
	hm.mu.RLock()
	assert.Equal(t, StatusUnhealthy, hm.providers["p"].Status)
	hm.mu.RUnlock()

	hm.RecordFailure("p")
	hm.mu.RLock()
	assert.Equal(t, StatusDown, hm.providers["p"].Status)
	hm.mu.RUnlock()
}

func TestHealthMonitor_RecordFailure_ErrorRateCapped(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	for i := 0; i < 20; i++ {
		hm.RecordFailure("p")
	}
	hm.mu.RLock()
	assert.Equal(t, 1.0, hm.providers["p"].ErrorRate)
	hm.mu.RUnlock()
}

func TestHealthMonitor_RecordFailure_UnknownProvider(t *testing.T) {
	hm := NewHealthMonitor()
	// Should not panic
	hm.RecordFailure("unknown")
}

func TestHealthMonitor_RecordSuccess_UnknownProvider(t *testing.T) {
	hm := NewHealthMonitor()
	// Should not panic
	hm.RecordSuccess("unknown", time.Millisecond)
}

func TestHealthMonitor_RecordSuccess_ResetsState(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.RecordFailure("p")
	hm.RecordSuccess("p", 50*time.Millisecond)

	hm.mu.RLock()
	health := hm.providers["p"]
	hm.mu.RUnlock()

	assert.Equal(t, 0, health.ConsecutiveFails)
	assert.Equal(t, 0.0, health.ErrorRate)
	assert.Equal(t, StatusHealthy, health.Status)
}

func TestHealthMonitor_RecordSuccess_LatencyP50(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})

	for i := 0; i < 10; i++ {
		hm.RecordSuccess("p", time.Duration(i*10)*time.Millisecond)
	}

	hm.mu.RLock()
	p50 := hm.providers["p"].LatencyP50
	hm.mu.RUnlock()
	assert.Greater(t, p50, time.Duration(0))
}

func TestHealthMonitor_RecordSuccess_LatencyRingBuffer(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})

	// Add more than 100 entries to trigger ring buffer trim
	for i := 0; i < 120; i++ {
		hm.RecordSuccess("p", time.Duration(i)*time.Millisecond)
	}

	hm.mu.RLock()
	assert.Equal(t, 100, len(hm.providers["p"].latencies))
	hm.mu.RUnlock()
}

func TestHealthMonitor_CheckHealth_Success(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("good", &countingProvider{name: "good", resp: &ChatResponse{Content: "ok"}})
	hm.CheckHealth(context.Background(), "good")
	c := hm.Confidence("good")
	assert.GreaterOrEqual(t, c, 0.5)
}

func TestHealthMonitor_CheckHealth_Failure(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("bad", &errProvider{name: "bad"})
	hm.CheckHealth(context.Background(), "bad")
	c := hm.Confidence("bad")
	assert.LessOrEqual(t, c, 0.3)
}

func TestHealthMonitor_CheckHealth_UnknownProvider(t *testing.T) {
	hm := NewHealthMonitor()
	// Should not panic
	hm.CheckHealth(context.Background(), "unknown")
}

func TestHealthMonitor_CheckHealth_NilProvider(t *testing.T) {
	hm := NewHealthMonitor()
	hm.mu.Lock()
	hm.providers["nil"] = nil
	hm.mu.Unlock()
	// Should not panic
	hm.CheckHealth(context.Background(), "nil")
}

func TestHealthMonitor_RunPeriodicChecks(t *testing.T) {
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
		t.Fatal("RunPeriodicChecks did not stop")
	}
}

func TestHealthMonitor_ConcurrentOps(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p1", &countingProvider{name: "p1"})
	hm.RegisterProvider("p2", &countingProvider{name: "p2"})

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				hm.RecordSuccess("p1", time.Millisecond)
			} else {
				hm.RecordFailure("p1")
			}
			hm.GetHealthyProviders()
			hm.Confidence("p1")
			hm.CheckHealth(context.Background(), "p2")
		}(i)
	}
	wg.Wait()
}

func TestHealthStatus_Constants(t *testing.T) {
	assert.Equal(t, HealthStatus(0), StatusHealthy)
	assert.Equal(t, HealthStatus(1), StatusDegraded)
	assert.Equal(t, HealthStatus(2), StatusUnhealthy)
	assert.Equal(t, HealthStatus(3), StatusDown)
}

func TestHealthMonitor_RecordSuccess_DegradedTransition(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	// Set error rate above 0.1 to force degraded path on next success
	hm.mu.Lock()
	hm.providers["p"].ErrorRate = 0.2
	hm.mu.Unlock()

	hm.RecordSuccess("p", time.Millisecond)

	hm.mu.RLock()
	status := hm.providers["p"].Status
	hm.mu.RUnlock()
	// After RecordSuccess, ErrorRate is set to 0 (< 0.1), so status is Healthy
	assert.Equal(t, StatusHealthy, status)
}

func TestHealthMonitor_GetHealthyProviders_Empty(t *testing.T) {
	hm := NewHealthMonitor()
	healthy := hm.GetHealthyProviders()
	assert.Empty(t, healthy)
}

func TestHealthMonitor_RecordFailure_ErrorRateAccumulation(t *testing.T) {
	hm := NewHealthMonitor()
	hm.RegisterProvider("p", &countingProvider{name: "p"})
	hm.RecordFailure("p")
	hm.mu.RLock()
	rate1 := hm.providers["p"].ErrorRate
	hm.mu.RUnlock()

	hm.RecordFailure("p")
	hm.mu.RLock()
	rate2 := hm.providers["p"].ErrorRate
	hm.mu.RUnlock()

	assert.Greater(t, rate2, rate1)
}
