package middleware

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// CacheControlConfig holds caching configuration.
type CacheControlConfig struct {
	MaxAge     time.Duration
	IsPrivate  bool
	Immutable  bool
	VaryHeader string
}

// DefaultAPICache returns cache config suitable for API responses (short-lived).
func DefaultAPICache() CacheControlConfig {
	return CacheControlConfig{
		MaxAge:     30 * time.Second,
		IsPrivate:  true,
		VaryHeader: "Authorization",
	}
}

// DefaultStaticCache returns cache config suitable for static content (long-lived).
func DefaultStaticCache() CacheControlConfig {
	return CacheControlConfig{
		MaxAge:    24 * time.Hour,
		IsPrivate: false,
		Immutable: true,
	}
}

// NoCache returns cache config that disables caching.
func NoCache() CacheControlConfig {
	return CacheControlConfig{
		MaxAge:    0,
		IsPrivate: true,
	}
}

// CacheControl returns middleware that sets Cache-Control and ETag headers.
func CacheControl(cfg CacheControlConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only cache GET/HEAD requests
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				next.ServeHTTP(w, r)
				return
			}

			// Capture response fully (headers, status, body) without writing
			// through, so we can decide 304 and set Cache-Control before the
			// response is committed to the client.
			rec := &cacheRecorder{header: make(http.Header), statusCode: 200}
			next.ServeHTTP(rec, r)

			// Build Cache-Control header
			var directives []string
			if cfg.MaxAge > 0 {
				directives = append(directives, fmt.Sprintf("max-age=%d", int(cfg.MaxAge.Seconds())))
			} else {
				directives = append(directives, "no-cache")
				directives = append(directives, "no-store")
			}
			if cfg.IsPrivate {
				directives = append(directives, "private")
			} else {
				directives = append(directives, "public")
			}
			if cfg.Immutable {
				directives = append(directives, "immutable")
			}

			// Copy captured headers to the real response writer
			copyHeader(w.Header(), rec.header)
			w.Header().Set("Cache-Control", strings.Join(directives, ", "))

			if cfg.VaryHeader != "" {
				w.Header().Set("Vary", cfg.VaryHeader)
			}

			// Compute ETag from response body
			if len(rec.body) > 0 {
				hash := sha256.Sum256(rec.body)
				etag := fmt.Sprintf(`"%x"`, hash[:16])
				w.Header().Set("ETag", etag)

				// Check If-None-Match for 304 responses
				if match := r.Header.Get("If-None-Match"); match == etag {
					w.WriteHeader(http.StatusNotModified)
					return
				}
			}

			w.WriteHeader(rec.statusCode)
			if len(rec.body) > 0 {
				w.Write(rec.body)
			}
		})
	}
}

// copyHeader copies all headers from src into dst.
func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// cacheRecorder captures the full response (headers, status, body) without
// writing through to the client, so the middleware can decide on 304 and set
// caching headers before committing the response.
type cacheRecorder struct {
	header     http.Header
	statusCode int
	body       []byte
}

func (r *cacheRecorder) Header() http.Header {
	return r.header
}

func (r *cacheRecorder) WriteHeader(code int) {
	r.statusCode = code
}

func (r *cacheRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return len(b), nil
}

// Unwrap returns nil — this recorder does not wrap a real writer.
func (r *cacheRecorder) Unwrap() http.ResponseWriter { return nil }
