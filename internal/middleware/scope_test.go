package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vigilagent/vigilagent/internal/auth"
)

func TestRequireScope(t *testing.T) {
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	t.Run("passes for JWT (not API key)", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		claims := &auth.Claims{
			UserID:   "user-1",
			IsAPIKey: false,
		}
		req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))

		rr := httptest.NewRecorder()
		middleware := RequireScope("read:projects")
		middleware(nextHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("passes for API key with valid scope", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		claims := &auth.Claims{
			UserID:   "user-1",
			IsAPIKey: true,
			Scopes:   []string{"read:projects"},
		}
		req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))

		rr := httptest.NewRecorder()
		middleware := RequireScope("read:projects")
		middleware(nextHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("passes for API key with admin scope", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		claims := &auth.Claims{
			UserID:   "user-1",
			IsAPIKey: true,
			Scopes:   []string{"*"},
		}
		req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))

		rr := httptest.NewRecorder()
		middleware := RequireScope("read:projects")
		middleware(nextHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("expected status 200, got %d", rr.Code)
		}
	})

	t.Run("fails for API key missing scope", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/", nil)
		claims := &auth.Claims{
			UserID:   "user-1",
			IsAPIKey: true,
			Scopes:   []string{"read:other"},
		}
		req = req.WithContext(auth.ContextWithClaims(req.Context(), claims))

		rr := httptest.NewRecorder()
		middleware := RequireScope("read:projects")
		middleware(nextHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusForbidden {
			t.Errorf("expected status 403, got %d", rr.Code)
		}
	})
}
