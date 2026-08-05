package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
)

func newTestRouter() *Router {
	return &Router{
		Mux:  chi.NewMux(),
		auth: auth.NewJWT(&config.AuthConfig{JWTSecret: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}),
	}
}

// helper to build a request with auth claims in context
func reqWithClaims(method, path string, body interface{}, claims *auth.Claims) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if claims != nil {
		ctx := auth.ContextWithClaims(req.Context(), claims)
		req = req.WithContext(ctx)
	}
	return req
}

// helper to parse JSON response body
func parseJSON(t *testing.T, w *httptest.ResponseRecorder) map[string]interface{} {
	t.Helper()
	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("failed to parse response JSON: %v", err)
	}
	return result
}

var testClaims = &auth.Claims{
	UserID: "user-123",
	Email:  "test@example.com",
	Role:   "user",
}
