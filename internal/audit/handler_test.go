package audit

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHandler_Trace(t *testing.T) {
	store := NewMemoryStore()
	eng := NewEngine(store)
	handler := NewHTTPHandler(eng)

	// Record some actions first
	store.Record("admin", "create", "user-123", true, nil)
	store.Record("admin", "update", "user-123", true, nil)

	body, _ := json.Marshal(TraceRequest{Entity: "user-123"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp TraceResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 2 {
		t.Errorf("expected 2 actions, got %d", resp.Total)
	}
}

func TestHandler_Trace_MissingEntity(t *testing.T) {
	handler := NewHTTPHandler(NewEngine(NewMemoryStore()))

	body, _ := json.Marshal(TraceRequest{})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	// Without entity or actor, defaults to "all" and returns recent entries
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}

func TestHandler_Trace_FilterByAction(t *testing.T) {
	store := NewMemoryStore()
	eng := NewEngine(store)
	handler := NewHTTPHandler(eng)

	store.Record("user1", "create", "doc-1", true, nil)
	store.Record("user1", "delete", "doc-1", true, nil)

	body, _ := json.Marshal(TraceRequest{Actor: "user1"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp TraceResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 2 {
		t.Errorf("expected 2 actions for user1, got %d", resp.Total)
	}
}

func TestHandler_Trace_EmptyResult(t *testing.T) {
	handler := NewHTTPHandler(NewEngine(NewMemoryStore()))

	body, _ := json.Marshal(TraceRequest{Actor: "nonexistent"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp TraceResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(resp.Entries))
	}
}

func TestHandler_InvalidJSON(t *testing.T) {
	handler := NewHTTPHandler(NewEngine(NewMemoryStore()))

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader([]byte("bad")))
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
}
