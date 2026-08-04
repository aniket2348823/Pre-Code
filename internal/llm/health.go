package llm

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// telemetryLLMProviderHealth tracks provider health for Grafana dashboard.
// Registered in health.go (not telemetry.go) to avoid duplicate metric registration.
var telemetryLLMProviderHealth = promauto.NewGaugeVec(
	prometheus.GaugeOpts{
		Name: "vigilagent_llm_provider_healthy",
		Help: "LLM provider health status (1 = healthy, 0 = unhealthy)",
	},
	[]string{"provider"},
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
	latencies        []time.Duration // ring buffer for P50 computation
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

// Confidence returns a 0..1 score for a provider based on its current health.
func (h *HealthMonitor) Confidence(name string) float64 {
	h.mu.RLock()
	defer h.mu.RUnlock()

	health, ok := h.providers[name]
	if !ok {
		return 0.5 // unknown provider: neutral
	}
	switch health.Status {
	case StatusHealthy:
		return max(0.5, 1.0-health.ErrorRate)
	case StatusDegraded:
		return max(0.3, 0.8-health.ErrorRate)
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

	// Update Prometheus gauge for Grafana dashboard.
	telemetryLLMProviderHealth.WithLabelValues(name).Set(0)
}

// RecordSuccess records a success for a provider and computes real P50 latency.
func (h *HealthMonitor) RecordSuccess(name string, latency time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Update Prometheus gauge for Grafana dashboard.
	telemetryLLMProviderHealth.WithLabelValues(name).Set(1)

	health, ok := h.providers[name]
	if !ok {
		return
	}

	health.ConsecutiveFails = 0
	health.ErrorRate = 0
	health.LastChecked = time.Now()

	// Maintain a ring buffer of the last 100 latencies for P50 computation
	health.latencies = append(health.latencies, latency)
	if len(health.latencies) > 100 {
		health.latencies = health.latencies[1:]
	}
	// Compute actual P50
	if len(health.latencies) > 0 {
		sorted := make([]time.Duration, len(health.latencies))
		copy(sorted, health.latencies)
		sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
		p50Idx := len(sorted) / 2
		health.LatencyP50 = sorted[p50Idx]
	}

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
	var provider Provider
	if ok && health != nil {
		provider = health.Provider
	}
	h.mu.RUnlock()

	if provider == nil {
		return
	}

	start := time.Now()
	err := provider.HealthCheck(ctx)
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

	var wg sync.WaitGroup

	for {
		select {
		case <-ctx.Done():
			wg.Wait()
			return
		case <-ticker.C:
			h.mu.RLock()
			names := make([]string, 0, len(h.providers))
			for name := range h.providers {
				names = append(names, name)
			}
			h.mu.RUnlock()

			for _, name := range names {
				wg.Add(1)
				go func(n string) {
					defer wg.Done()
					h.CheckHealth(ctx, n)
				}(name)
			}
		}
	}
}
