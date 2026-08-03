package router

import (
	"net/http"
	"net/http/httptest"
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
