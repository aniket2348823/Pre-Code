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

func TestEngine_NewWithNilTrail(t *testing.T) {
	eng := NewEngine(nil)
	if eng.GetTrail() == nil {
		t.Error("NewEngine(nil) should create default trail")
	}
}

func TestEngine_GetTrail(t *testing.T) {
	store := NewMemoryStore()
	eng := NewEngine(store)
	if eng.GetTrail() != store {
		t.Error("GetTrail should return the trail")
	}
}

func TestEngine_TraceByActor(t *testing.T) {
	store := NewMemoryStore()
	eng := NewEngine(store)
	store.Record("alice", "create", "doc-1", true, nil)
	store.Record("bob", "create", "doc-2", true, nil)
	resp := eng.Trace(nil, TraceRequest{Actor: "alice"})
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}
	if len(resp.Entries) != 1 {
		t.Errorf("expected 1 entry for alice, got %d", len(resp.Entries))
	}
}

func TestEngine_TraceByEntity(t *testing.T) {
	store := NewMemoryStore()
	eng := NewEngine(store)
	store.Record("user1", "scan", "doc-1", true, nil)
	store.Record("user1", "critique", "doc-1", true, nil)
	resp := eng.Trace(nil, TraceRequest{Entity: "scan"})
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}
	if len(resp.Entries) != 1 {
		t.Errorf("expected 1 entry for scan, got %d", len(resp.Entries))
	}
}

func TestEngine_TraceDefaultLimit(t *testing.T) {
	store := NewMemoryStore()
	eng := NewEngine(store)
	for i := 0; i < 100; i++ {
		store.Record("user", "action", "res", true, nil)
	}
	resp := eng.Trace(nil, TraceRequest{})
	if len(resp.Entries) > 50 {
		t.Errorf("expected max 50 entries, got %d", len(resp.Entries))
	}
}

func TestHandler_Trace_ByActor(t *testing.T) {
	store := NewMemoryStore()
	eng := NewEngine(store)
	store.Record("admin", "delete", "user-123", true, nil)
	handler := NewHTTPHandler(eng)
	body, _ := json.Marshal(TraceRequest{Actor: "admin"})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp TraceResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 1 {
		t.Errorf("expected 1 action, got %d", resp.Total)
	}
}

func TestHandler_Trace_Limit(t *testing.T) {
	store := NewMemoryStore()
	eng := NewEngine(store)
	for i := 0; i < 10; i++ {
		store.Record("user", "action", "res", true, nil)
	}
	handler := NewHTTPHandler(eng)
	body, _ := json.Marshal(TraceRequest{Limit: 3})
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	var resp TraceResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Entries) > 3 {
		t.Errorf("expected max 3 entries, got %d", len(resp.Entries))
	}
}
