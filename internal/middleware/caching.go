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
		MaxAge:    30 * time.Second,
		IsPrivate: true,
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

			// Capture response to compute ETag
			rec := &cacheRecorder{ResponseWriter: w, statusCode: 200}
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
					w.Write(nil)
					return
				}
			}
		})
	}
}

// cacheRecorder captures the response body for ETag computation.
type cacheRecorder struct {
	http.ResponseWriter
	statusCode int
	body       []byte
}

func (r *cacheRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

func (r *cacheRecorder) Write(b []byte) (int, error) {
	r.body = append(r.body, b...)
	return r.ResponseWriter.Write(b)
}
