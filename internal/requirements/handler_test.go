package requirements

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
	resolver := NewResolver()
	mux := chi.NewMux()
	mux.Post("/api/v1/requirements", NewHTTPHandler(resolver))
	mux.Post("/api/v1/validate", NewValidateHTTPHandler(resolver))
	return mux
}

// reqWithClaims injects auth claims directly into the request context,
// matching the pattern used by the router package tests.
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

func TestResolveHandler_Success(t *testing.T) {
	mux := setupTestRouter()
	body := map[string]interface{}{
		"description": "Build a payment processing API",
		"declared":    []string{"encryption"},
	}
	req := reqWithClaims("POST", "/api/v1/requirements", body, testClaims)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var rep Report
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if len(rep.Entities) == 0 {
		t.Fatal("expected at least one entity")
	}
	if !contains(rep.Entities, "payment") {
		t.Fatalf("expected payment entity, got %v", rep.Entities)
	}
}

func TestResolveHandler_EmptyDescription(t *testing.T) {
	mux := setupTestRouter()
	body := map[string]interface{}{
		"description": "",
	}
	req := reqWithClaims("POST", "/api/v1/requirements", body, testClaims)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestResolveHandler_InvalidJSON(t *testing.T) {
	mux := setupTestRouter()
	req := reqWithClaims("POST", "/api/v1/requirements", nil, testClaims)
	req.Body = http.NoBody
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestValidateHandler_Success(t *testing.T) {
	mux := setupTestRouter()
	body := map[string]interface{}{
		"description": "payment gateway with user login",
		"declared":    []string{"encryption", "rate_limit"},
	}
	req := reqWithClaims("POST", "/api/v1/validate", body, testClaims)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if _, ok := result["passed"]; !ok {
		t.Fatal("expected 'passed' field in response")
	}
	if _, ok := result["requirements"]; !ok {
		t.Fatal("expected 'requirements' field in response")
	}
}

func TestValidateHandler_MissingCritical(t *testing.T) {
	mux := setupTestRouter()
	body := map[string]interface{}{
		"description": "Build a payment processing API that stores card data",
	}
	req := reqWithClaims("POST", "/api/v1/validate", body, testClaims)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	passed, ok := result["passed"].(bool)
	if !ok {
		t.Fatal("expected 'passed' to be a boolean")
	}
	if passed {
		t.Fatal("expected passed=false for payment system with no declared controls")
	}
}

func TestValidateHandler_NoEntities(t *testing.T) {
	mux := setupTestRouter()
	body := map[string]interface{}{
		"description": "a static marketing homepage",
	}
	req := reqWithClaims("POST", "/api/v1/validate", body, testClaims)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result map[string]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	passed, ok := result["passed"].(bool)
	if !ok || !passed {
		t.Fatal("expected passed=true for static site with no security entities")
	}
}
