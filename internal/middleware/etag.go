package middleware

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ETagConfig controls ETag middleware behavior.
type ETagConfig struct {
	Enabled              bool
	Weak                 bool
	MaxAge               time.Duration
	StaleWhileRevalidate time.Duration
	SkipPaths            []string
	Authenticated        bool
	pathPatterns         map[string]bool
	mu                   sync.RWMutex
}

// DefaultETagConfig returns production-ready ETag configuration.
func DefaultETagConfig() *ETagConfig {
	return &ETagConfig{
		Enabled:              true,
		Weak:                 false,
		MaxAge:               30 * time.Second,
		StaleWhileRevalidate: 60 * time.Second,
		SkipPaths:            []string{"/api/v1/health", "/api/v1/ready"},
		Authenticated:        false,
		pathPatterns:         make(map[string]bool),
	}
}

// WithPathPatterns sets path patterns to enable caching on (e.g., "/agents", "/tasks").
func (c *ETagConfig) WithPathPatterns(patterns []string) *ETagConfig {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, p := range patterns {
		c.pathPatterns[p] = true
	}
	return c
}

// matchesPath checks if request path should be cached.
func (c *ETagConfig) matchesPath(path string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	// If no patterns configured, cache all GET requests
	if len(c.pathPatterns) == 0 {
		return true
	}
	for pattern := range c.pathPatterns {
		if strings.HasPrefix(path, pattern) {
			return true
		}
	}
	return false
}

// shouldSkip returns true if this request should bypass ETag caching.
func (c *ETagConfig) shouldSkip(r *http.Request) bool {
	if !c.Enabled {
		return true
	}
	// Only cache GET/HEAD
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return true
	}
	path := r.URL.Path
	for _, skip := range c.SkipPaths {
		if path == skip {
			return true
		}
	}
	return false
}

// ETagMiddleware returns middleware that handles ETag-based HTTP caching.
func ETagMiddleware(cfg *ETagConfig) func(http.Handler) http.Handler {
	if cfg == nil {
		cfg = DefaultETagConfig()
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.shouldSkip(r) {
				next.ServeHTTP(w, r)
				return
			}

			if !cfg.matchesPath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			// Capture response
			rec := &etagRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rec, r)

			// Only set ETag for successful responses
			if rec.statusCode < 200 || rec.statusCode >= 300 {
				return
			}
			if len(rec.body) == 0 {
				return
			}

			// Generate ETag
			hash := sha256.Sum256(rec.body)
			etag := fmt.Sprintf(`"%x"`, hash[:16])
			if cfg.Weak {
				etag = "W/" + etag
			}

			// Set Cache-Control
			var directives []string
			if cfg.MaxAge > 0 {
				directives = append(directives, fmt.Sprintf("max-age=%d", int(cfg.MaxAge.Seconds())))
			}
			directives = append(directives, "must-revalidate")
			if cfg.StaleWhileRevalidate > 0 {
				directives = append(directives, fmt.Sprintf("stale-while-revalidate=%d", int(cfg.StaleWhileRevalidate.Seconds())))
			}
			directives = append(directives, "private")
			w.Header().Set("Cache-Control", strings.Join(directives, ", "))
			w.Header().Set("ETag", etag)

			// Check If-None-Match → 304
			if match := r.Header.Get("If-None-Match"); match != "" {
				if match == etag || match == "*" {
					w.WriteHeader(http.StatusNotModified)
					w.Write(nil)
					return
				}
			}

			// Check If-Modified-Since → 304
			if ims := r.Header.Get("If-Modified-Since"); ims != "" {
				if t, err := time.Parse(http.TimeFormat, ims); err == nil {
					// Treat any successful response as "modified" for safety;
					// real modification times would come from the upstream handler.
					_ = t
				}
			}
		})
	}
}

// etagRecorder captures the response body for ETag computation.
type etagRecorder struct {
	http.ResponseWriter
	statusCode int
	body       []byte
	wrote304   bool
}

func (r *etagRecorder) WriteHeader(code int) {
	r.statusCode = code
	if code != http.StatusNotModified {
		r.ResponseWriter.WriteHeader(code)
	}
}

func (r *etagRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	if r.statusCode == http.StatusNotModified {
		return len(b), nil
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter for http.ResponseController.
func (r *etagRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// StaleWhileRevalidateMiddleware serves stale cached content while refreshing
// in the background. Requires a ResponseCache (see cache.go) to store bodies.
type StaleWhileRevalidateMiddleware struct {
	cache      *ResponseCache
	duration   time.Duration
	mu         sync.RWMutex
	refreshing map[string]bool
}

// NewStaleWhileRevalidate creates the middleware with a shared ResponseCache.
func NewStaleWhileRevalidate(cache *ResponseCache, duration time.Duration) *StaleWhileRevalidateMiddleware {
	return &StaleWhileRevalidateMiddleware{
		cache:      cache,
		duration:   duration,
		refreshing: make(map[string]bool),
	}
}

// Middleware returns the HTTP middleware that serves stale content.
func (swr *StaleWhileRevalidateMiddleware) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || swr.cache == nil {
			next.ServeHTTP(w, r)
			return
		}

		key := r.URL.Path
		if r.URL.RawQuery != "" {
			key += "?" + r.URL.RawQuery
		}

		entry, ok := swr.cache.Get(key)
		if ok && time.Since(entry.StoredAt) > swr.duration {
			// Content is stale — serve it and revalidate in background
			w.Header().Set("Content-Type", entry.ContentType)
			w.Header().Set("ETag", entry.ETag)
			w.Header().Set("Cache-Control", fmt.Sprintf(
				"max-age=0, must-revalidate, stale-while-revalidate=%d", int(swr.duration.Seconds())))
			w.Header().Set("X-Cache", "STALE")
			w.WriteHeader(http.StatusOK)
			w.Write(entry.Body)

			// Background refresh
			swr.maybeRefresh(key, r, next)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (swr *StaleWhileRevalidateMiddleware) maybeRefresh(key string, r *http.Request, next http.Handler) {
	swr.mu.Lock()
	if swr.refreshing[key] {
		swr.mu.Unlock()
		return
	}
	swr.refreshing[key] = true
	swr.mu.Unlock()

	go func() {
		defer func() {
			swr.mu.Lock()
			delete(swr.refreshing, key)
			swr.mu.Unlock()
		}()

		rec := &cacheOnlyRecorder{}
		r2 := r.Clone(r.Context())
		next.ServeHTTP(rec, r2)
		if rec.statusCode >= 200 && rec.statusCode < 300 && len(rec.body) > 0 {
			hash := sha256.Sum256(rec.body)
			swr.cache.Set(key, &CacheEntry{
				Body:        rec.body,
				ContentType: rec.Header().Get("Content-Type"),
				ETag:        fmt.Sprintf(`"%x"`, hash[:16]),
				StoredAt:    time.Now(),
				TTL:         swr.duration,
			})
		}
	}()
}

// cacheOnlyRecorder captures response without writing to client.
type cacheOnlyRecorder struct {
	http.ResponseWriter
	statusCode int
	body       []byte
}

func (r *cacheOnlyRecorder) WriteHeader(code int) {
	r.statusCode = code
}

func (r *cacheOnlyRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}

// Unwrap returns the underlying ResponseWriter.
func (r *cacheOnlyRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// ParseMaxAge parses a max-age string from a Cache-Control header.
func ParseMaxAge(header string) time.Duration {
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "max-age=") {
			val := strings.TrimPrefix(part, "max-age=")
			if secs, err := strconv.Atoi(val); err == nil {
				return time.Duration(secs) * time.Second
			}
		}
	}
	return 0
}
