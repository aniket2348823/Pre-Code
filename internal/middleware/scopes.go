package middleware

import (
	"net/http"
	"strings"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// RequireScope checks that the authenticated user's API key has the required scope.
// JWT-authenticated requests (non-API-key) bypass scope checks.
// Supports wildcard matching: "admin:*" matches "admin:read", "admin:write", etc.
func RequireScope(scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			// JWT-based auth (non-API-key) bypasses scope checks
			if !claims.IsAPIKey {
				next.ServeHTTP(w, r)
				return
			}

			// Check if the API key has the required scope
			if !hasScope(claims.Scopes, scope) {
				response.Forbidden(w, "API key missing required scope: "+scope)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// hasScope checks if a scope list contains the required scope.
// Supports wildcard matching: "admin:*" matches "admin:read", "admin:write", etc.
func hasScope(scopes []string, required string) bool {
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == required {
			return true
		}
		// Wildcard: "admin:*" matches "admin:anything"
		if strings.HasSuffix(s, ":*") {
			prefix := strings.TrimSuffix(s, "*")
			if strings.HasPrefix(required, prefix) {
				return true
			}
		}
		// Global wildcard
		if s == "*" {
			return true
		}
	}
	return false
}

