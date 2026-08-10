package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// newTestRouterForExport builds a router with nil repos — the handler must
// degrade gracefully (empty export) instead of exporting the global catalog.
func newTestRouterForExport() *Router {
	return &Router{Mux: chi.NewMux()}
}

// TestExportSkillsHandler_NoAuth verifies the endpoint requires authentication.
func TestExportSkillsHandler_NoAuth(t *testing.T) {
	r := newTestRouterForExport()
	req := httptest.NewRequest("GET", "/v1/export/skills", nil)
	w := httptest.NewRecorder()
	r.exportSkillsHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// TestExportSkillsHandler_EmptyRepoNoPanic verifies the user-scoped export path
// does not panic when the skill repo is nil (dev/mock mode) and returns an
// empty skills array rather than the global marketplace catalog.
// Regression test for the export leaking every published skill into a
// single user's export (previously called r.skills.List with no user filter).
func TestExportSkillsHandler_EmptyRepoNoPanic(t *testing.T) {
	r := newTestRouterForExport()
	req := reqWithClaims("GET", "/v1/export/skills", nil, testClaims)
	w := httptest.NewRecorder()
	r.exportSkillsHandler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	body := parseJSON(t, w)
	if body["user_id"] != "user-123" {
		t.Errorf("expected user_id user-123 in export, got %v", body["user_id"])
	}
	// The skills key may be absent (omitempty on an empty export) or an empty
	// array — either is correct. What must NEVER happen is the full global
	// marketplace catalog being dumped into the user's export.
	if skills, ok := body["skills"].([]interface{}); ok && len(skills) != 0 {
		t.Errorf("expected empty skills (no installs), got %d entries — global catalog leaked?", len(skills))
	}
}
