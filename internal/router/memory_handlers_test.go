package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/memory"
)

func newTestRouterForMemory() *Router {
	return &Router{
		Mux:    chi.NewMux(),
		memory: memory.NewManager(nil), // in-memory stores, no DB needed for validation tests
	}
}

// === searchMemoryHandler Tests ===

func TestSearchMemoryHandler_MissingAuth(t *testing.T) {
	r := newTestRouterForMemory()
	req := httptest.NewRequest("POST", "/v1/memory/search", nil)
	w := httptest.NewRecorder()
	r.searchMemoryHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestSearchMemoryHandler_EmptyBody(t *testing.T) {
	r := newTestRouterForMemory()
	req := reqWithClaims("POST", "/v1/memory/search", nil, testClaims)
	w := httptest.NewRecorder()
	r.searchMemoryHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body, got %d", w.Code)
	}
}

func TestSearchMemoryHandler_MissingQuery(t *testing.T) {
	r := newTestRouterForMemory()
	req := reqWithClaims("POST", "/v1/memory/search", map[string]interface{}{"limit": 10}, testClaims)
	w := httptest.NewRecorder()
	r.searchMemoryHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing query, got %d", w.Code)
	}
}

func TestSearchMemoryHandler_EmptyQuery(t *testing.T) {
	r := newTestRouterForMemory()
	req := reqWithClaims("POST", "/v1/memory/search", map[string]interface{}{"query": "   "}, testClaims)
	w := httptest.NewRecorder()
	r.searchMemoryHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty query, got %d", w.Code)
	}
}

func TestSearchMemoryHandler_InvalidJSON(t *testing.T) {
	r := newTestRouterForMemory()
	req := httptest.NewRequest("POST", "/v1/memory/search", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	// No auth claims — handler should reject with 401 before body parsing
	w := httptest.NewRecorder()
	r.searchMemoryHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing auth, got %d", w.Code)
	}
}

// === createMemoryHandler Tests ===

func TestCreateMemoryHandler_MissingAuth(t *testing.T) {
	r := newTestRouterForMemory()
	req := httptest.NewRequest("POST", "/v1/memory", nil)
	w := httptest.NewRecorder()
	r.createMemoryHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestCreateMemoryHandler_EmptyContent(t *testing.T) {
	r := newTestRouterForMemory()
	req := reqWithClaims("POST", "/v1/memory", map[string]interface{}{"content": ""}, testClaims)
	w := httptest.NewRecorder()
	r.createMemoryHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty content, got %d", w.Code)
	}
}

func TestCreateMemoryHandler_WhitespaceContent(t *testing.T) {
	r := newTestRouterForMemory()
	req := reqWithClaims("POST", "/v1/memory", map[string]interface{}{"content": "   \t  "}, testClaims)
	w := httptest.NewRecorder()
	r.createMemoryHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for whitespace-only content, got %d", w.Code)
	}
}

func TestCreateMemoryHandler_InvalidType(t *testing.T) {
	r := newTestRouterForMemory()
	req := reqWithClaims("POST", "/v1/memory", map[string]interface{}{"type": "invalid", "content": "test"}, testClaims)
	w := httptest.NewRecorder()
	r.createMemoryHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid type, got %d", w.Code)
	}
}

func TestCreateMemoryHandler_SemanticRequiresProjectID(t *testing.T) {
	r := newTestRouterForMemory()
	req := reqWithClaims("POST", "/v1/memory", map[string]interface{}{"type": "semantic", "content": "test pattern"}, testClaims)
	w := httptest.NewRecorder()
	r.createMemoryHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for semantic without project_id, got %d", w.Code)
	}
}

func TestCreateMemoryHandler_InvalidJSON(t *testing.T) {
	r := newTestRouterForMemory()
	req := httptest.NewRequest("POST", "/v1/memory", strings.NewReader("not json"))
	req.Header.Set("Content-Type", "application/json")
	// No auth claims — handler should reject with 401 before body parsing
	w := httptest.NewRecorder()
	r.createMemoryHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing auth, got %d", w.Code)
	}
}

func TestCreateMemoryHandler_ValidInputPassesValidation(t *testing.T) {
	r := newTestRouterForMemory()
	// Use procedural type — stores in working memory (in-memory, no DB)
	req := reqWithClaims("POST", "/v1/memory", map[string]interface{}{"type": "procedural", "content": "test memory"}, testClaims)
	w := httptest.NewRecorder()
	r.createMemoryHandler(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("expected 201 after validation passes, got %d", w.Code)
	}
}
