package middleware

import (
	"net/http"
	"strings"

	"github.com/vigilagent/vigilagent/pkg/response"
)

// BodySizeConfig configures the body size limiter middleware.
type BodySizeConfig struct {
	MaxBodySize int64 // Maximum request body size in bytes (default 10MB)
}

// DefaultBodySizeConfig returns a BodySizeConfig with a 10MB default.
func DefaultBodySizeConfig() BodySizeConfig {
	return BodySizeConfig{
		MaxBodySize: 10 << 20, // 10 MB
	}
}

// BodySizeLimiter returns middleware that limits request body size for POST/PUT/PATCH.
// Returns 413 Payload Too Large if the body exceeds the configured max.
func BodySizeLimiter(cfg BodySizeConfig) func(http.Handler) http.Handler {
	if cfg.MaxBodySize <= 0 {
		cfg = DefaultBodySizeConfig()
	}
	maxSize := cfg.MaxBodySize

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodPost, http.MethodPut, http.MethodPatch:
				if r.Body != nil {
					r.Body = http.MaxBytesReader(w, r.Body, maxSize)
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// HandleMaxBytesError checks if an error is from http.MaxBytesReader and writes a 413 response.
// Returns true if the error was handled (caller should return).
func HandleMaxBytesError(w http.ResponseWriter, r *http.Request, err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), "http: request body too large") {
		response.JSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"code":  "INFRA_003",
			"error": "request body too large",
		})
		return true
	}
	return false
}
