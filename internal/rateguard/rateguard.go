// Package rateguard provides a per-endpoint rate limiter with configurable limits.
package rateguard

import (
	"net/http"
	"sync"
	"time"
)

// EndpointRule defines rate limit for a specific endpoint pattern.
type EndpointRule struct {
	Pattern   string        // e.g. "/api/v1/scan"
	Limit     int           // max requests per window
	Window    time.Duration // sliding window duration
	Burst     int           // optional burst allowance (0 = no burst)
}

// EndpointConfig holds the full endpoint rate limit configuration.
type EndpointConfig struct {
	Rules      []EndpointRule
	DefaultLimit int
	DefaultWindow time.Duration
}

// DefaultEndpointConfig returns reasonable defaults.
func DefaultEndpointConfig() EndpointConfig {
	return EndpointConfig{
		DefaultLimit: 100,
		DefaultWindow: time.Minute,
		Rules: []EndpointRule{
			{Pattern: "/api/v1/scan", Limit: 10, Window: time.Minute, Burst: 2},
			{Pattern: "/api/v1/chat", Limit: 30, Window: time.Minute},
			{Pattern: "/api/v1/tasks", Limit: 50, Window: time.Minute},
		},
	}
}

// windowEntry tracks requests in a sliding window.
type windowEntry struct {
	timestamps []time.Time
}

// EndpointLimiter enforces per-endpoint rate limits.
type EndpointLimiter struct {
	mu       sync.Mutex
	config   EndpointConfig
	windows  map[string]*windowEntry
	keyFunc  func(r *http.Request) string
}

// NewEndpointLimiter creates a new endpoint rate limiter.
func NewEndpointLimiter(cfg EndpointConfig) *EndpointLimiter {
	return &EndpointLimiter{
		config:  cfg,
		windows: make(map[string]*windowEntry),
		keyFunc: defaultKeyFunc,
	}
}

// SetKeyFunc overrides the default key function for generating rate limit keys.
func (el *EndpointLimiter) SetKeyFunc(fn func(r *http.Request) string) {
	el.keyFunc = fn
}

func defaultKeyFunc(r *http.Request) string {
	return r.Method + " " + r.URL.Path
}

// Allow checks if a request is within the rate limit for its endpoint.
func (el *EndpointLimiter) Allow(r *http.Request) bool {
	key := el.keyFunc(r)
	now := time.Now()

	el.mu.Lock()
	defer el.mu.Unlock()

	rule := el.findRule(r.URL.Path)
	entry, exists := el.windows[key]
	if !exists {
		entry = &windowEntry{}
		el.windows[key] = entry
	}

	// Clean up expired timestamps
	cutoff := now.Add(-rule.Window)
	valid := entry.timestamps[:0]
	for _, ts := range entry.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	entry.timestamps = valid

	limit := rule.Limit
	if rule.Burst > 0 {
		limit += rule.Burst
	}

	if len(entry.timestamps) >= limit {
		return false
	}

	entry.timestamps = append(entry.timestamps, now)
	return true
}

func (el *EndpointLimiter) findRule(path string) EndpointRule {
	for _, rule := range el.config.Rules {
		if rule.Pattern == path {
			return rule
		}
	}
	return EndpointRule{
		Limit:  el.config.DefaultLimit,
		Window: el.config.DefaultWindow,
	}
}

// Stats returns current rate limit state for debugging.
func (el *EndpointLimiter) Stats() map[string]int {
	el.mu.Lock()
	defer el.mu.Unlock()

	stats := make(map[string]int)
	for key, entry := range el.windows {
		stats[key] = len(entry.timestamps)
	}
	return stats
}

// Middleware returns HTTP middleware that enforces endpoint rate limits.
func (el *EndpointLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !el.Allow(r) {
			w.Header().Set("Retry-After", "60")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Cleanup removes expired window entries. Call periodically.
func (el *EndpointLimiter) Cleanup() {
	el.mu.Lock()
	defer el.mu.Unlock()

	now := time.Now()
	for key, entry := range el.windows {
		cutoff := now.Add(-el.config.DefaultWindow)
		valid := entry.timestamps[:0]
		for _, ts := range entry.timestamps {
			if ts.After(cutoff) {
				valid = append(valid, ts)
			}
		}
		if len(valid) == 0 {
			delete(el.windows, key)
		} else {
			entry.timestamps = valid
		}
	}
}
