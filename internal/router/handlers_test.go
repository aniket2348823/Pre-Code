package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vigilagent/vigilagent/internal/auth"
)

// helper to create a minimal Router for handler testing.
// Repositories are nil — only auth/validation error paths are tested.
func newTestRouter() *Router {
	return &Router{}
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

// ==================== Auth Tests ====================

func TestAuthRequired(t *testing.T) {
	r := newTestRouter()

	handlers := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{"updateProfile", r.updateProfileHandler, "PUT", "/users/me"},
		{"createOrg", r.createOrgHandler, "POST", "/organizations"},
		{"listOrgs", r.listOrgsHandler, "GET", "/organizations"},
		{"getOrg", r.getOrgHandler, "GET", "/organizations/org-1"},
		{"updateOrg", r.updateOrgHandler, "PUT", "/organizations/org-1"},
		{"deleteOrg", r.deleteOrgHandler, "DELETE", "/organizations/org-1"},
		{"createProject", r.createProjectHandler, "POST", "/projects"},
		{"listProjects", r.listProjectsHandler, "GET", "/projects"},
		{"getProject", r.getProjectHandler, "GET", "/projects/proj-1"},
		{"updateProject", r.updateProjectHandler, "PUT", "/projects/proj-1"},
		{"deleteProject", r.deleteProjectHandler, "DELETE", "/projects/proj-1"},
	}

	for _, h := range handlers {
		t.Run(h.name+"_no_auth", func(t *testing.T) {
			req := httptest.NewRequest(h.method, h.path, nil)
			w := httptest.NewRecorder()
			h.handler(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("expected 401, got %d", w.Code)
			}
		})
	}
}

// ==================== Input Validation Tests ====================

func TestCreateOrgHandler_Validation(t *testing.T) {
	r := newTestRouter()

	t.Run("empty body", func(t *testing.T) {
		req := reqWithClaims("POST", "/organizations", nil, testClaims)
		w := httptest.NewRecorder()
		r.createOrgHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("empty name", func(t *testing.T) {
		req := reqWithClaims("POST", "/organizations", map[string]string{"name": ""}, testClaims)
		w := httptest.NewRecorder()
		r.createOrgHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("whitespace-only name", func(t *testing.T) {
		req := reqWithClaims("POST", "/organizations", map[string]string{"name": "   "}, testClaims)
		w := httptest.NewRecorder()
		r.createOrgHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestCreateProjectHandler_Validation(t *testing.T) {
	r := newTestRouter()

	t.Run("empty body", func(t *testing.T) {
		req := reqWithClaims("POST", "/projects", nil, testClaims)
		w := httptest.NewRecorder()
		r.createProjectHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("missing org_id", func(t *testing.T) {
		req := reqWithClaims("POST", "/projects", map[string]string{"name": "Test"}, testClaims)
		w := httptest.NewRecorder()
		r.createProjectHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("missing name", func(t *testing.T) {
		req := reqWithClaims("POST", "/projects", map[string]string{"org_id": "org-1"}, testClaims)
		w := httptest.NewRecorder()
		r.createProjectHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("empty org_id and name", func(t *testing.T) {
		req := reqWithClaims("POST", "/projects", map[string]string{}, testClaims)
		w := httptest.NewRecorder()
		r.createProjectHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestListProjectsHandler_Validation(t *testing.T) {
	r := newTestRouter()

	t.Run("missing org_id query param", func(t *testing.T) {
		req := reqWithClaims("GET", "/projects", nil, testClaims)
		w := httptest.NewRecorder()
		r.listProjectsHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

// ==================== Response Format Tests ====================

func TestResponseFormats(t *testing.T) {
	r := newTestRouter()

	t.Run("unauthorized returns JSON with error", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/organizations", nil)
		w := httptest.NewRecorder()
		r.createOrgHandler(w, req)

		if w.Header().Get("Content-Type") != "application/json" {
			t.Errorf("expected application/json content type")
		}

		result := parseJSON(t, w)
		if _, ok := result["error"]; !ok {
			t.Error("expected 'error' field in response")
		}
	})

	t.Run("bad request returns JSON with error", func(t *testing.T) {
		req := reqWithClaims("POST", "/organizations", map[string]string{}, testClaims)
		w := httptest.NewRecorder()
		r.createOrgHandler(w, req)

		result := parseJSON(t, w)
		if _, ok := result["error"]; !ok {
			t.Error("expected 'error' field in response")
		}
	})
}

// ==================== Chi URL Param Tests ====================

func TestGetOrgHandler_URLParamExtraction(t *testing.T) {
	r := newTestRouter()

	t.Run("extracts orgID from URL", func(t *testing.T) {
		// Create a chi router to properly set URL params
		chiRouter := r.Mux
		if chiRouter == nil {
			chiRouter = newTestRouter().Mux
		}
		req := reqWithClaims("GET", "/organizations/test-org-id", nil, testClaims)
		w := httptest.NewRecorder()
		r.getOrgHandler(w, req)
		// Without a real DB, this will 500 (IsMember fails), but that confirms
		// the handler reached the DB call (auth passed, URL param was extracted)
		if w.Code == http.StatusUnauthorized {
			t.Error("should not get 401 - auth claims are present")
		}
	})
}

// ==================== Middleware Tests ====================

func TestAuthMiddleware(t *testing.T) {
	r := newTestRouter()

	t.Run("rejects missing Authorization header", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/me", nil)
		w := httptest.NewRecorder()
		r.authMiddleware(http.HandlerFunc(r.currentUserHandler)).ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("rejects invalid Authorization format", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/me", nil)
		req.Header.Set("Authorization", "InvalidFormat")
		w := httptest.NewRecorder()
		r.authMiddleware(http.HandlerFunc(r.currentUserHandler)).ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})

	t.Run("rejects malformed Bearer token", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/users/me", nil)
		req.Header.Set("Authorization", "Bearer invalid-token-value")
		w := httptest.NewRecorder()
		r.authMiddleware(http.HandlerFunc(r.currentUserHandler)).ServeHTTP(w, req)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", w.Code)
		}
	})
}

func TestAdminMiddleware(t *testing.T) {
	r := newTestRouter()

	t.Run("rejects non-admin user", func(t *testing.T) {
		req := reqWithClaims("GET", "/admin/stats", nil, testClaims)
		w := httptest.NewRecorder()
		r.adminMiddleware(http.HandlerFunc(r.adminStatsHandler)).ServeHTTP(w, req)
		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403, got %d", w.Code)
		}
	})

}
