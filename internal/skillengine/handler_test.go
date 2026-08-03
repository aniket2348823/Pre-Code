package skillengine

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vigilagent/vigilagent/internal/auth"
)

// injectAuth adds valid auth claims to the request context.
func injectAuth(r *http.Request) *http.Request {
	claims := &auth.Claims{
		UserID: "test-user-123",
		Email:  "test@example.com",
		Role:   "user",
	}
	return r.WithContext(auth.ContextWithClaims(r.Context(), claims))
}

func TestHandler_Extract(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	body, _ := json.Marshal(ExtractRequest{
		Finding: Finding{
			Severity:   "high",
			Message:    "SQL injection in query",
			Fix:        "Use parameterized queries",
			Analyzers:  []string{"builtin", "semgrep"},
			Confidence: 0.9,
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = injectAuth(req)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["created"] != true {
		t.Error("expected created=true for first extraction")
	}
}

func TestHandler_Extract_Duplicate(t *testing.T) {
	eng := NewEngine()
	handler := NewHTTPHandler(eng)

	body, _ := json.Marshal(ExtractRequest{
		Finding: Finding{Message: "SQL injection", Fix: "Parameterize"},
	})
	// Extract once
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = injectAuth(req)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	// Extract again with same message
	req = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = injectAuth(req)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	var resp map[string]any
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["created"] != false {
		t.Error("expected created=false for duplicate extraction")
	}
}

func TestHandler_RecordOutcome(t *testing.T) {
	eng := NewEngine()
	// Create a skill first
	skill, _ := eng.ExtractFromFinding(Finding{Message: "test finding", Fix: "fix it"})

	handler := NewHTTPHandler(eng)

	body, _ := json.Marshal(ExtractRequest{
		SkillID: skill.ID,
		Outcome: "accepted",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	req = injectAuth(req)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "recorded" {
		t.Errorf("expected status=recorded, got %q", resp["status"])
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("bad")))
	req = injectAuth(req)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}

func TestHandler_NoAuth(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	body, _ := json.Marshal(ExtractRequest{
		Finding: Finding{Message: "test", Fix: "fix"},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}
