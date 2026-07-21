// Package cors provides HTTP Cross-Origin Resource Sharing middleware.
package cors

import (
	"net/http"
	"strconv"
	"strings"
)

// Config holds CORS configuration.
type Config struct {
	AllowOrigins     []string
	AllowMethods     []string
	AllowHeaders     []string
	ExposeHeaders    []string
	AllowCredentials bool
	MaxAge           int // seconds
}

// DefaultConfig returns a permissive CORS configuration suitable for development.
func DefaultConfig() Config {
	return Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Request-Id", "X-Signature", "X-Timestamp"},
		ExposeHeaders:    []string{"X-Request-Id", "X-Rate-Limit-Remaining"},
		AllowCredentials: false,
		MaxAge:           3600,
	}
}

// ProductionConfig returns a restrictive CORS configuration.
func ProductionConfig(allowedOrigins []string) Config {
	return Config{
		AllowOrigins:     allowedOrigins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH"},
		AllowHeaders:     []string{"Content-Type", "Authorization", "X-Request-Id"},
		ExposeHeaders:    []string{"X-Request-Id"},
		AllowCredentials: true,
		MaxAge:           86400,
	}
}

// isOriginAllowed checks if the origin is in the allow list.
func (c Config) isOriginAllowed(origin string) bool {
	for _, allowed := range c.AllowOrigins {
		if allowed == "*" || allowed == origin {
			return true
		}
	}
	return false
}

// Middleware returns an HTTP middleware that adds CORS headers.
func (c Config) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			if origin != "" && c.isOriginAllowed(origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else if len(c.AllowOrigins) == 1 && c.AllowOrigins[0] == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			}
			w.Header().Set("Access-Control-Allow-Methods", strings.Join(c.AllowMethods, ", "))
			w.Header().Set("Access-Control-Allow-Headers", strings.Join(c.AllowHeaders, ", "))
			w.Header().Set("Access-Control-Max-Age", strconv.Itoa(c.MaxAge))
			if c.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Handle regular requests
		if origin != "" && c.isOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else if len(c.AllowOrigins) == 1 && c.AllowOrigins[0] == "*" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		if len(c.ExposeHeaders) > 0 {
			w.Header().Set("Access-Control-Expose-Headers", strings.Join(c.ExposeHeaders, ", "))
		}
		if c.AllowCredentials {
			w.Header().Set("Access-Control-Allow-Credentials", "true")
		}

		next.ServeHTTP(w, r)
	})
}
