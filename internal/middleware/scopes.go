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
				// Fail closed: an endpoint that requires a scope must be
				// authenticated. Anonymous pass-through would grant access.
				response.Unauthorized(w, "missing authentication")
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
		// Wildcard: "admin:*" matches "admin:anything". The scope NAME before the
		// colon must match exactly — a raw string prefix would let a key scoped
		// "a:*" match the entire "ab:*" namespace.
		if strings.HasSuffix(s, ":*") {
			if strings.SplitN(s, ":", 2)[0] == strings.SplitN(required, ":", 2)[0] {
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
