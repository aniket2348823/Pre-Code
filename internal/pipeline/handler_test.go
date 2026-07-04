package pipeline

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
)

var testClaims = &auth.Claims{
	UserID: "user-123",
	Email:  "test@example.com",
	Role:   "user",
}

func setupTestRouter() *chi.Mux {
	p := NewPipeline(nil, nil, nil, nil)
	mux := chi.NewMux()
	mux.Post("/api/v1/validate-full", NewHTTPHandler(p))
	return mux
}

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

func TestValidateFullHandler_Success(t *testing.T) {
	mux := setupTestRouter()
	body := map[string]interface{}{
		"description": "a static marketing homepage",
	}
	req := reqWithClaims("POST", "/api/v1/validate-full", body, testClaims)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rep Report
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if !rep.Passed {
		t.Fatalf("static site should pass, got reasons: %v", rep.Reasons)
	}
}

func TestValidateFullHandler_EmptyDescription(t *testing.T) {
	mux := setupTestRouter()
	body := map[string]interface{}{
		"description": "",
	}
	req := reqWithClaims("POST", "/api/v1/validate-full", body, testClaims)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
