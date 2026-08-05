package middleware

import (
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/pkg/response"
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
	processing bool
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

			// Check cache under write lock to prevent TOCTOU race
			m.mu.Lock()
			entry, found := m.cache[cacheKey]
			if found && time.Since(entry.createdAt) < m.ttl {
				if entry.processing {
					// Another request is processing - wait for it
					m.mu.Unlock()
					m.waitForCompletion(cacheKey, w)
					return
				}
				// Return cached response
				for k, v := range entry.header {
					for _, vv := range v {
						w.Header().Add(k, vv)
					}
				}
				w.Header().Set("Idempotency-Replayed", "true")
				w.WriteHeader(entry.statusCode)
				w.Write(entry.body)
				m.mu.Unlock()
				return
			}

			// Reserve this key for processing
			if len(m.cache) >= maxCacheEntries {
				// Evict oldest (simple strategy)
				var oldestKey string
				var oldestTime time.Time
				for k, v := range m.cache {
					if oldestKey == "" || v.createdAt.Before(oldestTime) {
						oldestKey = k
						oldestTime = v.createdAt
					}
				}
				if oldestKey != "" {
					delete(m.cache, oldestKey)
				}
			}
			m.cache[cacheKey] = &idempotencyEntry{
				processing: true,
				createdAt:  time.Now(),
			}
			m.mu.Unlock()

			// Capture response
			rec := &idempotencyRecorder{ResponseWriter: w, statusCode: http.StatusOK}
			next.ServeHTTP(rec, r)

			// Cache the response (respect max size)
			m.mu.Lock()
			m.cache[cacheKey] = &idempotencyEntry{
				statusCode: rec.statusCode,
				body:       rec.body,
				header:     rec.Header().Clone(),
				createdAt:  time.Now(),
				processing: false,
			}
			m.mu.Unlock()
		})
	}
}

func (m *IdempotencyMiddleware) waitForCompletion(cacheKey string, w http.ResponseWriter) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-ticker.C:
			m.mu.RLock()
			entry, ok := m.cache[cacheKey]
			if ok && !entry.processing && time.Since(entry.createdAt) < m.ttl {
				for k, v := range entry.header {
					for _, vv := range v {
						w.Header().Add(k, vv)
					}
				}
				w.Header().Set("Idempotency-Replayed", "true")
				w.WriteHeader(entry.statusCode)
				w.Write(entry.body)
				m.mu.RUnlock()
				return
			}
			m.mu.RUnlock()
		case <-timeout:
			// The in-flight request is taking too long. Respond with a 409 so the
			// client can retry rather than hanging with no response at all.
			response.JSON(w, http.StatusConflict, map[string]interface{}{
				"code":  "IDEM_001",
				"error": "request with this idempotency key is still processing",
			})
			return
		}
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
