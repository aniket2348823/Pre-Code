package middleware

import (
	"crypto/sha256"
	"fmt"
	"net/http"
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
	// Move to end (most recently used)
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

// Set stores a response in the cache.
func (c *ResponseCache) Set(key string, entry *CacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if entry.TTL <= 0 {
		entry.TTL = c.defaultTTL
	}

	// Update existing entry
	if _, ok := c.entries[key]; ok {
		c.entries[key] = entry
		// Move to end
		for i, k := range c.order {
			if k == key {
				c.order = append(c.order[:i], c.order[i+1:]...)
				c.order = append(c.order, key)
				break
			}
		}
		return
	}

	// Evict oldest if at capacity
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
			// Only cache GET requests
			if r.Method != http.MethodGet {
				next.ServeHTTP(w, r)
				return
			}

			key := r.URL.Path
			if r.URL.RawQuery != "" {
				key += "?" + r.URL.RawQuery
			}

			// Check for cache bypass header
			if r.Header.Get("Cache-Control") == "no-cache" {
				next.ServeHTTP(w, r)
				return
			}

			// Try cache hit
			if entry, ok := cache.Get(key); ok {
				// Check If-None-Match
				if match := r.Header.Get("If-None-Match"); match != "" {
					if match == entry.ETag || match == "*" {
						w.WriteHeader(http.StatusNotModified)
						w.Write(nil)
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

			// Cache miss — capture response
			rec := &cacheCapture{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rec, r)

			// Cache successful GET responses with a body
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

// statusWriter wraps ResponseWriter to capture the status code.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func (sw *statusWriter) Unwrap() http.ResponseWriter {
	return sw.ResponseWriter
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

			// Invalidate on success
			if sw.status >= 200 && sw.status < 300 {
				invalidateByMethod(cache, r)
			}
		})
	}
}

// invalidateByMethod removes cached entries related to the written resource.
func invalidateByMethod(cache *ResponseCache, r *http.Request) {
	path := r.URL.Path

	// Invalidate the exact path
	cache.Delete(path)
	cache.Delete(path + "?" + r.URL.RawQuery)

	// Invalidate list endpoints that might contain this resource
	// e.g., POST /agents → invalidate /projects/*/agents
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 {
		// Invalidate parent collection
		parent := "/" + strings.Join(parts[:len(parts)-1], "/")
		cache.InvalidatePrefix(parent)
	}

	// Invalidate root collection for this resource type
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
