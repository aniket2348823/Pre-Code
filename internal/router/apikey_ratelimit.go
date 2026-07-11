package router

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/vigilagent/vigilagent/internal/auth"
	ratelimit "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// apiKeyRateLimitMiddleware applies per-API-key rate limiting.
// When authenticated via API key, rate limits are keyed by the API key prefix
// (not the user ID), so a single user with multiple keys gets independent
// rate limit buckets per key. Falls back to per-user limiting for JWT auth.
func (r *Router) apiKeyRateLimitMiddleware(next http.Handler) http.Handler {
	if r.rl == nil {
		slog.Warn("per-API-key rate limiting disabled: limiter not configured")
		return next
	}
	return r.rl.Middleware(func(req *http.Request) string {
		// Check if authenticated via API key (X-API-Key header or Bearer vga_...)
		if apiKey := extractAPIKeyFromRequest(req); apiKey != "" {
			return "apikey:" + rateLimitKeyFromAPIKey(apiKey)
		}
		// Fall back to user-based limiting
		claims, ok := auth.ClaimsFromContext(req.Context())
		if ok {
			return "user:" + claims.UserID
		}
		return "ip:" + req.RemoteAddr
	})(next)
}

// extractAPIKeyFromRequest pulls the API key from the request headers
// without performing full authentication.
func extractAPIKeyFromRequest(req *http.Request) string {
	if key := req.Header.Get("X-API-Key"); key != "" {
		return key
	}
	authHeader := req.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && parts[0] == "Bearer" {
			token := parts[1]
			// API keys have prefix va_ and no dots (JWTs have dots)
			if !strings.Contains(token, ".") && strings.Contains(token, "_") {
				return token
			}
		}
	}
	return ""
}

// rateLimitKeyFromAPIKey derives a rate limit key from an API key.
// Uses the first 8 characters which are unique enough for rate limiting.
func rateLimitKeyFromAPIKey(apiKey string) string {
	if len(apiKey) > 8 {
		return apiKey[:8]
	}
	return apiKey
}
