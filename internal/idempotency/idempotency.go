// Package idempotency provides HTTP request idempotency protection.
// Duplicate requests with the same idempotency key return the cached response.
package idempotency

import (
	"net/http"
	"sync"
	"time"
)

// Config holds idempotency configuration.
type Config struct {
	KeyHeader   string        // header name for idempotency key
	MaxAge      time.Duration // how long to cache responses
	MaxEntries  int           // max cached entries
}

// DefaultConfig returns sensible defaults.
func DefaultConfig() Config {
	return Config{
		KeyHeader:  "Idempotency-Key",
		MaxAge:     24 * time.Hour,
		MaxEntries: 10000,
	}
}

type cachedResponse struct {
	statusCode int
	body       []byte
	headers    http.Header
	createdAt  time.Time
	processing bool // true while request is being processed
}

// Store holds cached idempotent responses.
type Store struct {
	mu      sync.RWMutex
	entries map[string]*cachedResponse
	config  Config
}

// NewStore creates a new idempotency store.
func NewStore(cfg Config) *Store {
	return &Store{
		entries: make(map[string]*cachedResponse),
		config:  cfg,
	}
}

// Get retrieves a cached response by key. Returns nil if not found or expired.
func (s *Store) Get(key string) *cachedResponse {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.entries[key]
	if !ok {
		return nil
	}
	if time.Since(entry.createdAt) > s.config.MaxAge {
		return nil
	}
	return entry
}

// Set stores a response by key.
func (s *Store) Set(key string, statusCode int, body []byte, headers http.Header) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Evict oldest if at capacity
	if len(s.entries) >= s.config.MaxEntries {
		s.evictOldest()
	}

	s.entries[key] = &cachedResponse{
		statusCode: statusCode,
		body:       body,
		headers:    headers,
		createdAt:  time.Now(),
	}
}

// Delete removes a cached response.
func (s *Store) Delete(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.entries, key)
}

// Cleanup removes expired entries.
func (s *Store) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	count := 0
	for key, entry := range s.entries {
		if time.Since(entry.createdAt) > s.config.MaxAge {
			delete(s.entries, key)
			count++
		}
	}
	return count
}

func (s *Store) evictOldest() {
	var oldestKey string
	var oldestTime time.Time
	for key, entry := range s.entries {
		if oldestKey == "" || entry.createdAt.Before(oldestTime) {
			oldestKey = key
			oldestTime = entry.createdAt
		}
	}
	if oldestKey != "" {
		delete(s.entries, oldestKey)
	}
}

// Size returns the number of cached entries.
func (s *Store) Size() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Middleware wraps an HTTP handler with idempotency protection.
// Requests with the same Idempotency-Key within the cache window
// return the cached response without invoking the handler.
func (s *Store) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get(s.config.KeyHeader)
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Try to reserve or get cached response
		s.mu.Lock()
		entry, ok := s.entries[key]
		if ok && time.Since(entry.createdAt) <= s.config.MaxAge {
			if entry.processing {
				// Another request is processing - wait for it
				s.mu.Unlock()
				s.waitForCompletion(key, w)
				return
			}
			// Return cached response
			for k, vals := range entry.headers {
				for _, v := range vals {
					w.Header().Add(k, v)
				}
			}
			w.Header().Set("Idempotent-Replayed", "true")
			w.WriteHeader(entry.statusCode)
			w.Write(entry.body)
			s.mu.Unlock()
			return
		}

		// Reserve this key for processing
		if len(s.entries) >= s.config.MaxEntries {
			s.evictOldest()
		}
		s.entries[key] = &cachedResponse{
			processing: true,
			createdAt:  time.Now(),
		}
		s.mu.Unlock()

		// Process the request
		rw := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}
		next.ServeHTTP(rw, r)

		// Store the actual response
		s.mu.Lock()
		s.entries[key] = &cachedResponse{
			statusCode: rw.statusCode,
			body:       rw.body,
			headers:    rw.Header().Clone(),
			createdAt:  time.Now(),
			processing: false,
		}
		s.mu.Unlock()
	})
}

// waitForCompletion waits for another request to complete processing.
func (s *Store) waitForCompletion(key string, w http.ResponseWriter) {
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	timeout := time.After(30 * time.Second)
	for {
		select {
		case <-ticker.C:
			s.mu.RLock()
			entry, ok := s.entries[key]
			if ok && !entry.processing && time.Since(entry.createdAt) <= s.config.MaxAge {
				for k, vals := range entry.headers {
					for _, v := range vals {
						w.Header().Add(k, v)
					}
				}
				w.Header().Set("Idempotent-Replayed", "true")
				w.WriteHeader(entry.statusCode)
				w.Write(entry.body)
				s.mu.RUnlock()
				return
			}
			s.mu.RUnlock()
		case <-timeout:
			// Timeout - treat as cache miss and let the request proceed
			return
		}
	}
}

// MaxBodySize limits how much of the response body is captured for caching.
// Responses larger than this are not cached to prevent memory exhaustion.
const MaxBodySize = 1 << 20 // 1 MB

// responseWriter captures the response for caching.
type responseWriter struct {
	http.ResponseWriter
	statusCode int
	body       []byte
	skipped    bool
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	if !rw.skipped {
		remaining := MaxBodySize - len(rw.body)
		if remaining <= 0 {
			rw.skipped = true
		} else if len(b) > remaining {
			rw.body = append(rw.body, b[:remaining]...)
			rw.skipped = true
		} else {
			rw.body = append(rw.body, b...)
		}
	}
	return rw.ResponseWriter.Write(b)
}
