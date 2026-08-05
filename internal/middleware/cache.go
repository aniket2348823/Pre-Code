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

// CacheEntry holds a cached response body.
type CacheEntry struct {
	Body        []byte
	ContentType string
	ETag        string
	StatusCode  int
	StoredAt    time.Time
	TTL         time.Duration
}

// IsExpired returns true if the entry has exceeded its TTL.
func (e *CacheEntry) IsExpired() bool {
	return time.Since(e.StoredAt) > e.TTL
}

// ResponseCache is an in-memory LRU cache for HTTP response bodies.
type ResponseCache struct {
	mu         sync.RWMutex
	entries    map[string]*CacheEntry
	order      []string
	maxSize    int
	defaultTTL time.Duration
	hits       int64
	misses     int64
}

// ResponseCacheConfig configures the in-memory response cache.
type ResponseCacheConfig struct {
	MaxSize    int
	DefaultTTL time.Duration
}

// DefaultResponseCacheConfig returns production-ready cache configuration.
func DefaultResponseCacheConfig() ResponseCacheConfig {
	return ResponseCacheConfig{
		MaxSize:    1024,
		DefaultTTL: 5 * time.Minute,
	}
}

// NewResponseCache creates a new in-memory LRU response cache.
func NewResponseCache(cfg ResponseCacheConfig) *ResponseCache {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 1024
	}
	if cfg.DefaultTTL <= 0 {
		cfg.DefaultTTL = 5 * time.Minute
	}
	return &ResponseCache{
		entries:    make(map[string]*CacheEntry),
		order:      make([]string, 0, cfg.MaxSize),
		maxSize:    cfg.MaxSize,
		defaultTTL: cfg.DefaultTTL,
	}
}

// Get retrieves a cached entry by key. Returns nil if not found or expired.
func (c *ResponseCache) Get(key string) (*CacheEntry, bool) {
	c.mu.RLock()
	entry, ok := c.entries[key]
	c.mu.RUnlock()

	if !ok || entry.IsExpired() {
		c.mu.Lock()
		c.misses++
		c.mu.Unlock()
		return nil, false
	}

	c.mu.Lock()
	c.hits++

	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			break
		}
	}
	c.mu.Unlock()

	return entry, true
}

// GetRaw retrieves a cached entry without checking expiration.
func (c *ResponseCache) GetRaw(key string) (*CacheEntry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	return entry, ok
}

// Set stores a response in the cache.
func (c *ResponseCache) Set(key string, entry *CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry.TTL <= 0 {
		entry.TTL = c.defaultTTL
	}

	if _, ok := c.entries[key]; ok {
		c.entries[key] = entry

		for i, k := range c.order {
			if k == key {
				c.order = append(c.order[:i], c.order[i+1:]...)
				c.order = append(c.order, key)
				break
			}
		}
		return
	}

	for len(c.entries) >= c.maxSize && len(c.order) > 0 {
		oldest := c.order[0]
		c.order = c.order[1:]
		delete(c.entries, oldest)
	}

	c.entries[key] = entry
	c.order = append(c.order, key)
}

// Delete removes an entry from the cache.
func (c *ResponseCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			break
		}
	}
}

// InvalidatePrefix removes all entries whose key starts with prefix.
func (c *ResponseCache) InvalidatePrefix(prefix string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	count := 0
	var remaining []string
	for _, k := range c.order {
		if strings.HasPrefix(k, prefix) {
			delete(c.entries, k)
			count++
		} else {
			remaining = append(remaining, k)
		}
	}
	c.order = remaining
	return count
}

// Stats returns cache statistics.
func (c *ResponseCache) Stats() map[string]int64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return map[string]int64{
		"hits":   c.hits,
		"misses": c.misses,
		"size":   int64(len(c.entries)),
	}
}

// Len returns the number of entries in the cache.
func (c *ResponseCache) Len() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// ResponseCacheMiddleware returns middleware that caches GET responses in memory.
func ResponseCacheMiddleware(cache *ResponseCache) func(http.Handler) http.Handler {
	if cache == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}

			key := r.URL.Path
			if r.URL.RawQuery != "" {
				key += "?" + r.URL.RawQuery
			}

			if r.Header.Get("Cache-Control") == "no-cache" {
				next.ServeHTTP(w, r)
				return
			}

			if entry, ok := cache.Get(key); ok {					if match := r.Header.Get("If-None-Match"); match != "" {
						if match == entry.ETag || match == "*" {
							// 304 must not carry a body.
							w.WriteHeader(http.StatusNotModified)
							return
						}
					}

				w.Header().Set("Content-Type", entry.ContentType)
				w.Header().Set("ETag", entry.ETag)
				w.Header().Set("X-Cache", "HIT")
				w.Header().Set("Cache-Control",
					fmt.Sprintf("max-age=%d, must-revalidate", int(entry.TTL.Seconds())))
				w.WriteHeader(entry.StatusCode)
				w.Write(entry.Body)
				return
			}

			rec := &cacheCapture{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rec, r)

			if rec.statusCode >= 200 && rec.statusCode < 300 && len(rec.body) > 0 {
				ct := rec.Header().Get("Content-Type")
				if ct == "" {
					ct = "application/json"
				}
				hash := sha256.Sum256(rec.body)
				etag := fmt.Sprintf(`"%x"`, hash[:16])

				cache.Set(key, &CacheEntry{
					Body:        rec.body,
					ContentType: ct,
					ETag:        etag,
					StatusCode:  rec.statusCode,
					StoredAt:    time.Now(),
				})

				w.Header().Set("ETag", etag)
				w.Header().Set("X-Cache", "MISS")
			} else {
				w.Header().Set("X-Cache", "MISS")
			}
		})
	}
}

// cacheCapture captures the response body for caching.
type cacheCapture struct {
	http.ResponseWriter
	statusCode int
	body       []byte
}

func (c *cacheCapture) WriteHeader(code int) {
	c.statusCode = code
	c.ResponseWriter.WriteHeader(code)
}

func (c *cacheCapture) Write(b []byte) (int, error) {
	c.body = append(c.body, b...)
	return c.ResponseWriter.Write(b)
}

// Unwrap returns the underlying ResponseWriter.
func (c *cacheCapture) Unwrap() http.ResponseWriter {
	return c.ResponseWriter
}

// CacheInvalidationMiddleware invalidates cache entries on write operations.
func CacheInvalidationMiddleware(cache *ResponseCache) func(http.Handler) http.Handler {
	if cache == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			isWrite := r.Method == http.MethodPost || r.Method == http.MethodPut ||
				r.Method == http.MethodDelete || r.Method == http.MethodPatch

			if !isWrite {
				next.ServeHTTP(w, r)
				return
			}

			sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(sw, r)

			if sw.status >= 200 && sw.status < 300 {
				invalidateByMethod(cache, r)
			}
		})
	}
}

// invalidateByMethod removes cached entries related to the written resource.
func invalidateByMethod(cache *ResponseCache, r *http.Request) {
	path := r.URL.Path

	cache.Delete(path)
	cache.Delete(path + "?" + r.URL.RawQuery)

	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 {

		parent := "/" + strings.Join(parts[:len(parts)-1], "/")
		cache.InvalidatePrefix(parent)
	}

	if len(parts) > 0 {
		cache.InvalidatePrefix("/" + parts[0])
	}
}

// WriteCacheHeaders sets standard cache headers on a response.
func WriteCacheHeaders(w http.ResponseWriter, maxAge time.Duration, etag string) {
	var directives []string
	if maxAge > 0 {
		directives = append(directives, fmt.Sprintf("max-age=%d", int(maxAge.Seconds())))
	} else {
		directives = append(directives, "no-cache")
	}
	directives = append(directives, "must-revalidate")
	w.Header().Set("Cache-Control", strings.Join(directives, ", "))
	if etag != "" {
		w.Header().Set("ETag", etag)
	}
}

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

			rec := &etagRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rec, r)

			if rec.statusCode < 200 || rec.statusCode >= 300 {
				if rec.headerWritten {
					w.WriteHeader(rec.statusCode)
				}
				w.Write(rec.body)
				return
			}
			if len(rec.body) == 0 {
				if rec.headerWritten {
					w.WriteHeader(rec.statusCode)
				}
				return
			}

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

			if match := r.Header.Get("If-None-Match"); match != "" {
				if match == etag || match == "*" {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}

			if rec.headerWritten {
				w.WriteHeader(rec.statusCode)
			}
			w.Write(rec.body)
		})
	}
}

// etagRecorder captures the response body for ETag computation.
type etagRecorder struct {
	http.ResponseWriter
	statusCode    int
	headerWritten bool
	body          []byte
}

func (r *etagRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.headerWritten = true
}

func (r *etagRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
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

		entry, ok := swr.cache.GetRaw(key)
		if ok && entry.IsExpired() {

			w.Header().Set("Content-Type", entry.ContentType)
			w.Header().Set("ETag", entry.ETag)
			w.Header().Set("Cache-Control", fmt.Sprintf(
				"max-age=0, must-revalidate, stale-while-revalidate=%d", int(swr.duration.Seconds())))
			w.Header().Set("X-Cache", "STALE")
			w.WriteHeader(http.StatusOK)
			w.Write(entry.Body)

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
	header     http.Header
	statusCode int
	body       []byte
}

func (r *cacheOnlyRecorder) Header() http.Header {
	if r.header == nil {
		r.header = make(http.Header)
	}
	return r.header
}

func (r *cacheOnlyRecorder) WriteHeader(code int) {
	r.statusCode = code
}

func (r *cacheOnlyRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}

// Unwrap returns nil — this recorder does not wrap a real writer.
func (r *cacheOnlyRecorder) Unwrap() http.ResponseWriter { return nil }

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
