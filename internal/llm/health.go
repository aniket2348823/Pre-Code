package llm

import (
	"context"
	"sync"
	"time"
)

// HealthStatus represents the health state of a provider.
type HealthStatus int

const (
	StatusHealthy HealthStatus = iota
	StatusDegraded
	StatusUnhealthy
	StatusDown
)

// ProviderHealth tracks health metrics for a provider.
type ProviderHealth struct {
	Status           HealthStatus
	Provider         Provider
	ErrorRate        float64
	ConsecutiveFails int
	LastChecked      time.Time
	LatencyP50       time.Duration
}

// HealthMonitor tracks provider health and availability.
type HealthMonitor struct {
	providers map[string]*ProviderHealth
	mu        sync.RWMutex
}

// NewHealthMonitor creates a new health monitor.
func NewHealthMonitor() *HealthMonitor {
	return &HealthMonitor{
		providers: make(map[string]*ProviderHealth),
	}
}

// RegisterProvider adds a provider for health tracking.
func (h *HealthMonitor) RegisterProvider(name string, p Provider) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.providers[name] = &ProviderHealth{
		Status:   StatusHealthy,
		Provider: p,
	}
}

// GetHealthyProviders returns names of healthy/degraded providers.
func (h *HealthMonitor) GetHealthyProviders() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var healthy []string
	for name, health := range h.providers {
		if health.Status == StatusHealthy || health.Status == StatusDegraded {
			healthy = append(healthy, name)
		}
	}
	return healthy
}

// Confidence returns a 0..1 score for a provider based on its current health:
// 1.0 when fully healthy, degrading with error rate, and low when unhealthy or
// unknown. Used by the router to rank candidates on reliability rather than an
// arbitrary cost formula.
func (h *HealthMonitor) Confidence(name string) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	health, ok := h.providers[name]
	if !ok {
		return 0.5 // unknown provider: neutral
	}
	switch health.Status {
	case StatusHealthy:
		return maxf(0.5, 1.0-health.ErrorRate)
	case StatusDegraded:
		return maxf(0.3, 0.8-health.ErrorRate)
	case StatusUnhealthy:
		return 0.2
	default: // StatusDown
		return 0.0
	}
}

// RecordFailure records a failure for a provider.
func (h *HealthMonitor) RecordFailure(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	health, ok := h.providers[name]
	if !ok {
		return
	}

	health.ConsecutiveFails++
	if health.ErrorRate+0.1 < 1.0 {
		health.ErrorRate += 0.1
	} else {
		health.ErrorRate = 1.0
	}

	if health.ConsecutiveFails >= 3 {
		health.Status = StatusDown
	} else if health.ConsecutiveFails >= 1 {
		health.Status = StatusUnhealthy
	}
}

// RecordSuccess records a success for a provider.
func (h *HealthMonitor) RecordSuccess(name string, latency time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	health, ok := h.providers[name]
	if !ok {
		return
	}

	health.ConsecutiveFails = 0
	if health.ErrorRate-0.05 > 0 {
		health.ErrorRate -= 0.05
	} else {
		health.ErrorRate = 0
	}
	health.LatencyP50 = latency
	health.LastChecked = time.Now()

	if health.ErrorRate < 0.1 {
		health.Status = StatusHealthy
	} else {
		health.Status = StatusDegraded
	}
}

// CheckHealth actively checks a provider's health.
func (h *HealthMonitor) CheckHealth(ctx context.Context, name string) {
	h.mu.RLock()
	health, ok := h.providers[name]
	h.mu.RUnlock()

	if !ok || health == nil {
		return
	}

	start := time.Now()
	err := health.Provider.HealthCheck(ctx)
	latency := time.Since(start)

	if err != nil {
		h.RecordFailure(name)
	} else {
		h.RecordSuccess(name, latency)
	}
}

// RunPeriodicChecks starts background health checks.
func (h *HealthMonitor) RunPeriodicChecks(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			h.mu.RLock()
			names := make([]string, 0, len(h.providers))
			for name := range h.providers {
				names = append(names, name)
			}
			h.mu.RUnlock()

			for _, name := range names {
				go h.CheckHealth(ctx, name)
			}
		}
	}
}
