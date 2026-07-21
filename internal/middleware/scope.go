package middleware

import (
	"net/http"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// RequireScope checks if the API key used for request has the required scope.
func RequireScope(requiredScope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok {
				response.UnauthorizedR(w, r, "unauthorized")
				return
			}

			if claims.IsAPIKey {
				hasScope := false
				for _, s := range claims.Scopes {
					if s == requiredScope || s == "admin" {
						hasScope = true
						break
					}
				}
				if !hasScope {
					response.ForbiddenR(w, r, "insufficient API key scope: requires "+requiredScope)
					return
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}
