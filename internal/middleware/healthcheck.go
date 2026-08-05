package middleware

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/pkg/response"
)

// HealthStatus represents the health state of a dependency.
type HealthStatus string

const (
	HealthStatusHealthy  HealthStatus = "healthy"
	HealthStatusDegraded HealthStatus = "degraded"
	HealthStatusDown     HealthStatus = "down"
)

// DepHealth describes the health of a single backend dependency.
type DepHealth struct {
	Status  HealthStatus `json:"status"`
	Latency int64        `json:"latency_ms"`
	Error   string       `json:"error,omitempty"`
}

// HealthResponse is the body returned by liveness/readiness probes.
type HealthResponse struct {
	Status       HealthStatus         `json:"status"`
	Timestamp    time.Time            `json:"timestamp"`
	Version      string               `json:"version"`
	Dependencies map[string]DepHealth `json:"dependencies,omitempty"`
}

// DependencyChecker checks the health of a single dependency.
type DependencyChecker struct {
	Name    string
	CheckFn func(ctx context.Context) error
}

// HealthCache caches health check results for a configurable TTL.
type HealthCache struct {
	mu      sync.RWMutex
	entries map[string]*cacheEntry
	ttl     time.Duration
}

type cacheEntry struct {
	result    HealthResponse
	expiresAt time.Time
}

// NewHealthCache creates a cache with the given TTL.
func NewHealthCache(ttl time.Duration) *HealthCache {
	return &HealthCache{
		entries: make(map[string]*cacheEntry),
		ttl:     ttl,
	}
}

// Get returns the cached response if still valid.
func (c *HealthCache) Get(key string) (HealthResponse, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		return HealthResponse{}, false
	}
	return entry.result, true
}

// Set stores a response in the cache.
func (c *HealthCache) Set(key string, resp HealthResponse) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[key] = &cacheEntry{
		result:    resp,
		expiresAt: time.Now().Add(c.ttl),
	}
}

// checkDependencies runs all dependency checkers and returns per-dep results.
func checkDependencies(ctx context.Context, checkers []DependencyChecker) (map[string]DepHealth, bool) {
	deps := make(map[string]DepHealth, len(checkers))
	allHealthy := true
	for _, checker := range checkers {
		start := time.Now()
		err := checker.CheckFn(ctx)
		latency := time.Since(start).Milliseconds()
		if err != nil {
			deps[checker.Name] = DepHealth{
				Status:  HealthStatusDown,
				Latency: latency,
				Error:   err.Error(),
			}
			allHealthy = false
		} else {
			deps[checker.Name] = DepHealth{
				Status:  HealthStatusHealthy,
				Latency: latency,
			}
		}
	}
	return deps, allHealthy
}

// CachedHealthHandler wraps a readiness handler with TTL-based caching.
// The cacheKey differentiates liveness vs readiness responses.
func CachedHealthHandler(
	version string,
	cache *HealthCache,
	cacheKey string,
	checkers []DependencyChecker,
	liveness bool,
) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if cache != nil {
			if cached, ok := cache.Get(cacheKey); ok {
				cached.Timestamp = time.Now()
				statusCode := http.StatusOK
				if !liveness && cached.Status != HealthStatusHealthy {
					statusCode = http.StatusServiceUnavailable
				}
				response.JSON(w, statusCode, cached)
				return
			}
		}

		resp := HealthResponse{
			Status:    HealthStatusHealthy,
			Timestamp: time.Now(),
			Version:   version,
		}

		if !liveness {
			deps, allHealthy := checkDependencies(r.Context(), checkers)
			resp.Dependencies = deps
			if !allHealthy {
				resp.Status = HealthStatusDown
			}
		}

		if cache != nil {
			cache.Set(cacheKey, resp)
		}

		statusCode := http.StatusOK
		if !liveness && resp.Status != HealthStatusHealthy {
			statusCode = http.StatusServiceUnavailable
		}
		response.JSON(w, statusCode, resp)
	}
}
