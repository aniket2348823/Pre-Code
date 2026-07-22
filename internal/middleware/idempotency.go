package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const maxCacheEntries = 10000

// IdempotencyMiddleware prevents duplicate POST requests using idempotency keys.
// Clients send an Idempotency-Key header; the server caches the response for the TTL.
type IdempotencyMiddleware struct {
	cache map[string]*idempotencyEntry
	mu    sync.RWMutex
	ttl   time.Duration
}

type idempotencyEntry struct {
	statusCode int
	body       []byte
	header     http.Header
	createdAt  time.Time
}

// NewIdempotencyMiddleware creates a new idempotency middleware with the given TTL.
func NewIdempotencyMiddleware(ttl time.Duration) *IdempotencyMiddleware {
	m := &IdempotencyMiddleware{
		cache: make(map[string]*idempotencyEntry),
		ttl:   ttl,
	}
	go m.cleanup()
	return m
}

// AsMiddleware returns a chi-compatible middleware function (func(http.Handler) http.Handler).
// This is the form required by chi's .With() method.
func (m *IdempotencyMiddleware) AsMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only apply to POST requests
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}

		key := r.Header.Get("Idempotency-Key")
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Hash the key for consistent storage
		hash := sha256.Sum256([]byte(key))
		cacheKey := hex.EncodeToString(hash[:])

		// Check cache
		m.mu.RLock()
		entry, found := m.cache[cacheKey]
		m.mu.RUnlock()

		if found && time.Since(entry.createdAt) < m.ttl {
			// Return cached response
			for k, v := range entry.header {
				for _, vv := range v {
					w.Header().Add(k, vv)
				}
			}
			w.Header().Set("Idempotency-Replayed", "true")
			w.WriteHeader(entry.statusCode)
			w.Write(entry.body)
			return
		}

		// Capture response
		rec := &idempotencyRecorder{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rec, r)

		// Cache the response (respect max size)
		m.mu.Lock()
		if len(m.cache) < maxCacheEntries {
			m.cache[cacheKey] = &idempotencyEntry{
				statusCode: rec.statusCode,
				body:       rec.body,
				header:     rec.Header().Clone(),
				createdAt:  time.Now(),
			}
		}
			m.mu.Unlock()
		})
	}
}

func (m *IdempotencyMiddleware) cleanup() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		m.mu.Lock()
		for k, v := range m.cache {
			if time.Since(v.createdAt) > m.ttl {
				delete(m.cache, k)
			}
		}
		m.mu.Unlock()
	}
}

// idempotencyRecorder captures the response for caching.
type idempotencyRecorder struct {
	http.ResponseWriter
	statusCode int
	body       []byte
}

func (r *idempotencyRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *idempotencyRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return r.ResponseWriter.Write(b)
}
