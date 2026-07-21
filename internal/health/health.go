// Package health provides comprehensive health checking for all VigilAgent
// components with dependency tracking, degraded state detection, and
// auto-recovery capabilities.
package health

import (
	"sync"
	"time"
)

// Status represents the health status of a component.
type Status string

const (
	StatusHealthy   Status = "healthy"
	StatusDegraded  Status = "degraded"
	StatusUnhealthy Status = "unhealthy"
	StatusUnknown   Status = "unknown"
)

// Component represents a health-checkable component.
type Component struct {
	Name         string            `json:"name"`
	Status       Status            `json:"status"`
	Message      string            `json:"message,omitempty"`
	LastCheck    time.Time         `json:"last_check"`
	LatencyMs    int64             `json:"latency_ms"`
	Dependencies []string          `json:"dependencies,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
}

// Check defines a health check function.
type Check func() Component

// Health aggregates health of all components.
type Health struct {
	mu         sync.RWMutex
	components map[string]*Component
	checks     map[string]Check
	interval   time.Duration
	stopCh     chan struct{}
	stopOnce   sync.Once
}

// New creates a health checker.
func New(interval time.Duration) *Health {
	return &Health{
		components: make(map[string]*Component),
		checks:     make(map[string]Check),
		interval:   interval,
		stopCh:     make(chan struct{}),
	}
}

// Register adds a health check for a component.
func (h *Health) Register(name string, check Check) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = check
	h.components[name] = &Component{
		Name:   name,
		Status: StatusUnknown,
	}
}

// RunChecks executes all registered health checks.
// Checks run outside the write lock to avoid blocking during slow checks.
func (h *Health) RunChecks() {
	h.mu.RLock()
	checksCopy := make(map[string]Check, len(h.checks))
	for k, v := range h.checks {
		checksCopy[k] = v
	}
	h.mu.RUnlock()

	results := make(map[string]*Component, len(checksCopy))
	for name, check := range checksCopy {
		start := time.Now()
		result := check()
		result.LastCheck = time.Now()
		result.LatencyMs = time.Since(start).Milliseconds()
		if result.Name == "" {
			result.Name = name
		}
		cp := result
		results[name] = &cp
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	for name, comp := range results {
		h.components[name] = comp
	}
}

// Start begins periodic health checking.
func (h *Health) Start() {
	go func() {
		ticker := time.NewTicker(h.interval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				h.RunChecks()
			case <-h.stopCh:
				return
			}
		}
	}()
}

// Stop halts periodic health checking. Safe to call multiple times.
func (h *Health) Stop() {
	h.stopOnce.Do(func() {
		close(h.stopCh)
	})
}

// Overall returns the aggregated health status.
func (h *Health) Overall() Status {
	h.mu.RLock()
	defer h.mu.RUnlock()
	hasUnhealthy := false
	for _, c := range h.components {
		switch c.Status {
		case StatusUnhealthy:
			return StatusUnhealthy
		case StatusDegraded:
			hasUnhealthy = true
		}
	}
	if hasUnhealthy {
		return StatusDegraded
	}
	if len(h.components) == 0 {
		return StatusUnknown
	}
	return StatusHealthy
}

// GetComponent returns a specific component's health.
func (h *Health) GetComponent(name string) *Component {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.components[name]
	if !ok {
		return nil
	}
	cp := *c
	return &cp
}

// AllComponents returns all component health statuses.
func (h *Health) AllComponents() []Component {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]Component, 0, len(h.components))
	for _, c := range h.components {
		out = append(out, *c)
	}
	return out
}

// Summary returns a health summary.
func (h *Health) Summary() map[string]interface{} {
	h.mu.RLock()
	defer h.mu.RUnlock()
	statuses := make(map[string]int)
	hasUnhealthy := false
	for _, c := range h.components {
		statuses[string(c.Status)]++
		if c.Status == StatusUnhealthy {
			return map[string]interface{}{
				"overall":  string(StatusUnhealthy),
				"total":    len(h.components),
				"statuses": statuses,
			}
		}
		if c.Status == StatusDegraded {
			hasUnhealthy = true
		}
	}
	overall := StatusHealthy
	if hasUnhealthy {
		overall = StatusDegraded
	} else if len(h.components) == 0 {
		overall = StatusUnknown
	}
	return map[string]interface{}{
		"overall":  string(overall),
		"total":    len(h.components),
		"statuses": statuses,
	}
}
