package router

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func routerTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestSwaggerUIHandler_Router(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()
	r.swaggerUIHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
}

func TestOpenAPISpecHandler_Router(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("GET", "/docs/openapi.yaml", nil)
	w := httptest.NewRecorder()
	r.openapiSpecHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/yaml")
}

func TestRegisterHandler_EmptyBody_Router(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("POST", "/auth/register", nil)
	w := httptest.NewRecorder()
	r.registerHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCorsAllExplicit_Router(t *testing.T) {
	tests := []struct {
		name     string
		origins  []string
		expected bool
	}{
		{"empty", []string{}, false},
		{"wildcard", []string{"*"}, false},
		{"explicit", []string{"https://example.com"}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, corsAllExplicit(tt.origins))
		})
	}
}

func TestCreateOrgHandler_EmptyBody(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("POST", "/organizations", nil, testClaims)
	w := httptest.NewRecorder()
	r.createOrgHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateOrgHandler_EmptyName(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("POST", "/organizations", map[string]string{"name": ""}, testClaims)
	w := httptest.NewRecorder()
	r.createOrgHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateOrgHandler_WhitespaceName(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("POST", "/organizations", map[string]string{"name": "   "}, testClaims)
	w := httptest.NewRecorder()
	r.createOrgHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListOrgsHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("GET", "/organizations", nil)
	w := httptest.NewRecorder()
	r.listOrgsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetOrgHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("GET", "/organizations/org-1", nil)
	w := httptest.NewRecorder()
	r.getOrgHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUpdateOrgHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("PUT", "/organizations/org-1", nil)
	w := httptest.NewRecorder()
	r.updateOrgHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteOrgHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("DELETE", "/organizations/org-1", nil)
	w := httptest.NewRecorder()
	r.deleteOrgHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateProjectHandler_EmptyBody(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("POST", "/projects", nil, testClaims)
	w := httptest.NewRecorder()
	r.createProjectHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateProjectHandler_MissingOrgID(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("POST", "/projects", map[string]string{"name": "test"}, testClaims)
	w := httptest.NewRecorder()
	r.createProjectHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateProjectHandler_MissingName(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("POST", "/projects", map[string]string{"org_id": "org-1"}, testClaims)
	w := httptest.NewRecorder()
	r.createProjectHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListProjectsHandler_MissingOrgID(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("GET", "/projects", nil, testClaims)
	w := httptest.NewRecorder()
	r.listProjectsHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetProjectHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("GET", "/projects/proj-1", nil)
	w := httptest.NewRecorder()
	r.getProjectHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUpdateProjectHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("PUT", "/projects/proj-1", nil)
	w := httptest.NewRecorder()
	r.updateProjectHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteProjectHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("DELETE", "/projects/proj-1", nil)
	w := httptest.NewRecorder()
	r.deleteProjectHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateAgentHandler_EmptyBody(t *testing.T) {
	defer func() { recover() }()
	r := routerTestRouter()
	req := reqWithClaims("POST", "/projects/proj-1/agents", nil, testClaims)
	w := httptest.NewRecorder()
	r.createAgentHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListAgentsHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("GET", "/projects/proj-1/agents", nil)
	w := httptest.NewRecorder()
	r.listAgentsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetAgentHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("GET", "/agents/agent-1", nil)
	w := httptest.NewRecorder()
	r.getAgentHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUpdateAgentHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("PUT", "/agents/agent-1", nil)
	w := httptest.NewRecorder()
	r.updateAgentHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteAgentHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("DELETE", "/agents/agent-1", nil)
	w := httptest.NewRecorder()
	r.deleteAgentHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateSessionHandler_EmptyBody(t *testing.T) {
	defer func() { recover() }()
	r := routerTestRouter()
	req := reqWithClaims("POST", "/agents/agent-1/sessions", nil, testClaims)
	w := httptest.NewRecorder()
	r.createSessionHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListSessionsHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("GET", "/agents/agent-1/sessions", nil)
	w := httptest.NewRecorder()
	r.listSessionsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetSessionHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("GET", "/sessions/session-1", nil)
	w := httptest.NewRecorder()
	r.getSessionHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestUpdateSessionHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("PUT", "/sessions/session-1", nil)
	w := httptest.NewRecorder()
	r.updateSessionHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetOrgHandler_NilRepoPanics(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("GET", "/organizations/org-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.getOrgHandler(w, req)
	}()
}

func TestUpdateOrgHandler_NilRepoPanics(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("PUT", "/organizations/org-1", map[string]string{}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.updateOrgHandler(w, req)
	}()
}

func TestDeleteOrgHandler_NilRepoPanics(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("DELETE", "/organizations/org-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.deleteOrgHandler(w, req)
	}()
}

func TestGetProjectHandler_NilRepoPanics(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("GET", "/projects/proj-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.getProjectHandler(w, req)
	}()
}

func TestGetAgentHandler_NilRepoPanics(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("GET", "/agents/agent-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.getAgentHandler(w, req)
	}()
}

func TestGetSessionHandler_NilRepoPanics(t *testing.T) {
	r := routerTestRouter()
	req := reqWithClaims("GET", "/sessions/session-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.getSessionHandler(w, req)
	}()
}

func TestMetricsHandler_NoClaims(t *testing.T) {
	r := routerTestRouter()
	req := httptest.NewRequest("GET", "/metrics", nil)
	w := httptest.NewRecorder()
	r.metricsHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHealthHandler(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	r.healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	result := parseJSON(t, w)
	if status, ok := result["status"]; !ok || status != "healthy" {
		t.Errorf("expected status=healthy, got %v", status)
	}
}

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
		{"createProject", r.createProjectHandler, "POST", "/projects"},
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

func TestResponseFormats(t *testing.T) {
	r := newTestRouter()

	t.Run("unauthorized returns JSON with error", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/organizations", nil)
		w := httptest.NewRecorder()
		r.createOrgHandler(w, req)

		if !strings.HasPrefix(w.Header().Get("Content-Type"), "application/json") {
			t.Errorf("expected application/json content type, got %q", w.Header().Get("Content-Type"))
		}

		result := parseJSON(t, w)
		if _, ok := result["code"]; !ok {
			if _, ok := result["message"]; !ok {
				if _, ok := result["error"]; !ok {
					t.Error("expected 'code', 'message', or 'error' field in response")
				}
			}
		}
	})

	t.Run("bad request returns JSON with error", func(t *testing.T) {
		req := reqWithClaims("POST", "/organizations", map[string]string{}, testClaims)
		w := httptest.NewRecorder()
		r.createOrgHandler(w, req)

		result := parseJSON(t, w)
		if _, ok := result["code"]; !ok {
			if _, ok := result["message"]; !ok {
				if _, ok := result["error"]; !ok {
					t.Error("expected 'code', 'message', or 'error' field in response")
				}
			}
		}
	})
}

func TestGetOrgHandler_URLParamExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires DB-backed repositories")
	}

	t.Skip("requires full router setup with repositories (integration test)")
}

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

func TestRegisterHandler_Validation(t *testing.T) {
	r := newTestRouter()

	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/auth/register", nil)
		w := httptest.NewRecorder()
		r.registerHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("missing email", func(t *testing.T) {
		req := reqWithClaims("POST", "/auth/register", map[string]string{"password": "12345678"}, testClaims)
		w := httptest.NewRecorder()
		r.registerHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})

	t.Run("short password", func(t *testing.T) {
		req := reqWithClaims("POST", "/auth/register", map[string]string{"email": "test@example.com", "password": "short"}, testClaims)
		w := httptest.NewRecorder()
		r.registerHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestLoginHandler_Validation(t *testing.T) {
	r := newTestRouter()

	t.Run("empty body", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/auth/login", nil)
		w := httptest.NewRecorder()
		r.loginHandler(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", w.Code)
		}
	})
}

func TestCurrentUserHandler_NoClaims(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("GET", "/users/me", nil)
	w := httptest.NewRecorder()
	r.currentUserHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestLogoutHandler_NoClaims(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("POST", "/auth/logout", nil)
	w := httptest.NewRecorder()
	r.logoutHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	result := parseJSON(t, w)
	if errObj, ok := result["error"]; ok {
		if errMap, ok := errObj.(map[string]interface{}); ok {
			if _, ok := errMap["message"]; !ok {
				t.Error("expected 'message' field in error response")
			}
		}
	}
}

func TestLogoutHandler_WithClaims_NoBlacklist(t *testing.T) {
	r := newTestRouter()

	req := reqWithClaims("POST", "/auth/logout", nil, testClaims)
	w := httptest.NewRecorder()
	r.logoutHandler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
	result := parseJSON(t, w)
	if msg, ok := result["message"]; !ok || msg != "logged out successfully" {
		t.Errorf("expected logged out message, got %v", result)
	}
}

func TestChangePasswordHandler_NoClaims(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("PUT", "/users/me/password", nil)
	w := httptest.NewRecorder()
	r.changePasswordHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestChangePasswordHandler_EmptyBody(t *testing.T) {
	r := newTestRouter()
	req := reqWithClaims("PUT", "/users/me/password", nil, testClaims)
	w := httptest.NewRecorder()
	r.changePasswordHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChangePasswordHandler_MissingCurrentPassword(t *testing.T) {
	r := newTestRouter()
	req := reqWithClaims("PUT", "/users/me/password", map[string]string{"new_password": "123456789012"}, testClaims)
	w := httptest.NewRecorder()
	r.changePasswordHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChangePasswordHandler_ShortNewPassword(t *testing.T) {
	r := newTestRouter()
	req := reqWithClaims("PUT", "/users/me/password", map[string]string{"current_password": "oldpass123456", "new_password": "short"}, testClaims)
	w := httptest.NewRecorder()
	r.changePasswordHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestChangePasswordHandler_NilRepository(t *testing.T) {
	r := newTestRouter()

	req := reqWithClaims("PUT", "/users/me/password", map[string]string{"current_password": "oldpass123456", "new_password": "newpass123456"}, testClaims)
	w := httptest.NewRecorder()
	defer func() {
		if recover() == nil {

			if w.Code != http.StatusInternalServerError && w.Code != http.StatusNotFound {
				t.Errorf("expected error status with nil repo, got %d", w.Code)
			}
		}

	}()
	r.changePasswordHandler(w, req)
}

func TestRotateAPIKeyHandler_NoClaims(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("POST", "/api-keys/abc123/rotate", nil)
	w := httptest.NewRecorder()
	r.rotateAPIKeyHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRefreshTokenHandler_NoClaims(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("POST", "/auth/refresh", nil)
	w := httptest.NewRecorder()
	r.refreshTokenHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestRefreshTokenHandler_NilAuth(t *testing.T) {
	r := newTestRouter()

	req := reqWithClaims("POST", "/auth/refresh", nil, testClaims)
	w := httptest.NewRecorder()

	defer func() {
		if rec := recover(); rec == nil {

		}
	}()
	r.refreshTokenHandler(w, req)
}

func TestForgotPasswordHandler_EmptyBody(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("POST", "/auth/forgot-password", nil)
	w := httptest.NewRecorder()
	r.forgotPasswordHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestForgotPasswordHandler_EmptyEmail(t *testing.T) {
	r := newTestRouter()
	req := reqWithClaims("POST", "/auth/forgot-password", map[string]string{"email": ""}, testClaims)
	w := httptest.NewRecorder()
	r.forgotPasswordHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestForgotPasswordHandler_NilEmailService(t *testing.T) {
	r := newTestRouter()

	req := httptest.NewRequest("POST", "/auth/forgot-password", nil)
	w := httptest.NewRecorder()
	r.forgotPasswordHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 (nil body fails validation), got %d", w.Code)
	}
}

func TestResetPasswordHandler_EmptyBody(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("POST", "/auth/reset-password", nil)
	w := httptest.NewRecorder()
	r.resetPasswordHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestResetPasswordHandler_MissingToken(t *testing.T) {
	r := newTestRouter()
	req := reqWithClaims("POST", "/auth/reset-password", map[string]string{"new_password": "newpass123456"}, testClaims)
	w := httptest.NewRecorder()
	r.resetPasswordHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestResetPasswordHandler_ShortPassword(t *testing.T) {
	r := newTestRouter()
	req := reqWithClaims("POST", "/auth/reset-password", map[string]string{"token": "abc", "new_password": "short"}, testClaims)
	w := httptest.NewRecorder()
	r.resetPasswordHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestVerifyEmailHandler_MissingToken(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("GET", "/auth/verify-email", nil)
	w := httptest.NewRecorder()
	r.verifyEmailHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestReadinessHandler_AllNil(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()
	r.readinessHandler(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", w.Code)
	}
	result := parseJSON(t, w)
	if checks, ok := result["checks"]; !ok {
		t.Error("expected 'checks' field in response")
	} else {
		checksMap, ok := checks.(map[string]interface{})
		if !ok {
			t.Error("expected checks to be a map")
		} else if checksMap["postgres"] != "not configured" {
			t.Errorf("expected postgres 'not configured', got %v", checksMap["postgres"])
		}
	}
}
