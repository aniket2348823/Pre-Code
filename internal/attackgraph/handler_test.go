package attackgraph

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler_Generate(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	body, _ := json.Marshal(FindingsRequest{
		Description: "Payment processing system",
		Findings: []FindingInput{
			{Title: "SQL Injection in payment", Severity: "critical"},
		},
		Entity: "payment",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp GraphResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Paths) != 1 {
		t.Errorf("expected 1 attack path, got %d", len(resp.Paths))
	}
	if resp.Summary == "" {
		t.Error("summary should not be empty")
	}
}

func TestHandler_Generate_NoFindings(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	body, _ := json.Marshal(FindingsRequest{
		Description: "Test system",
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp GraphResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Paths) != 0 {
		t.Errorf("expected 0 paths for no findings, got %d", len(resp.Paths))
	}
}

func TestHandler_Generate_GenericPath(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	body, _ := json.Marshal(FindingsRequest{
		Description: "Unknown system",
		Findings: []FindingInput{
			{Title: "Something weird", Severity: "medium"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp GraphResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Paths) != 1 {
		t.Errorf("expected 1 generic path, got %d", len(resp.Paths))
	}
	if resp.Paths[0].ID != "generic-exploitation" {
		t.Errorf("expected generic path ID, got %q", resp.Paths[0].ID)
	}
}

func TestHandler_Generate_InferEntity(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	body, _ := json.Marshal(FindingsRequest{
		Description: "Authentication system with broken auth",
		Findings: []FindingInput{
			{Title: "Broken authentication", Severity: "critical"},
		},
	})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp GraphResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Paths) != 1 {
		t.Errorf("expected 1 path, got %d", len(resp.Paths))
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	handler := NewHTTPHandler(NewEngine())

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
