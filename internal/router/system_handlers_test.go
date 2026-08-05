package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/auth"
)

// Content from system_handlers_test.go
func extendedTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestMiddlewareMetricsHandler_NoAuth(t *testing.T) {
	r := extendedTestRouter()
	req := httptest.NewRequest("GET", "/middleware/metrics", nil)
	w := httptest.NewRecorder()
	r.middlewareMetricsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddlewareMetricsHandler_NilDeps(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("GET", "/middleware/metrics", nil, testClaims)
	w := httptest.NewRecorder()
	r.middlewareMetricsHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	result := parseJSON(t, w)
	assert.Contains(t, result, "total_records")
}

func TestMiddlewarePatternsHandler_NoAuth(t *testing.T) {
	r := extendedTestRouter()
	req := httptest.NewRequest("GET", "/middleware/patterns", nil)
	w := httptest.NewRecorder()
	r.middlewarePatternsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddlewarePatternsHandler_NilDeps(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("GET", "/middleware/patterns", nil, testClaims)
	w := httptest.NewRecorder()
	r.middlewarePatternsHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestHealthStatsHandler_NoAuth(t *testing.T) {
	r := extendedTestRouter()
	req := httptest.NewRequest("GET", "/providers/health", nil)
	w := httptest.NewRecorder()
	r.healthStatsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHealthStatsHandler_NilDeps(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("GET", "/providers/health", nil, testClaims)
	w := httptest.NewRecorder()
	r.healthStatsHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCostOverrideHandler_NoAuth(t *testing.T) {
	r := extendedTestRouter()
	req := httptest.NewRequest("POST", "/providers/cost-override", nil)
	w := httptest.NewRecorder()
	r.costOverrideHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

// adminClaims is used for admin-gated handlers (cost overrides, feature flags).
var adminClaims = &auth.Claims{UserID: "admin-1", Email: "admin@example.com", Role: "admin"}

func TestCostOverrideHandler_RejectsNonAdmin(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("POST", "/providers/cost-override", map[string]interface{}{
		"model":             "custom-model",
		"input_cost_per_1k": 0.01,
	}, testClaims)
	w := httptest.NewRecorder()
	r.costOverrideHandler(w, req)
	assert.Equal(t, http.StatusForbidden, w.Code)
}

func TestCostOverrideHandler_EmptyBody(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("POST", "/providers/cost-override", nil, adminClaims)
	w := httptest.NewRecorder()
	r.costOverrideHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCostOverrideHandler_MissingModel(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("POST", "/providers/cost-override", map[string]interface{}{}, adminClaims)
	w := httptest.NewRecorder()
	r.costOverrideHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCostOverrideHandler_WithModel(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("POST", "/providers/cost-override", map[string]interface{}{
		"model":              "custom-model",
		"input_cost_per_1k":  0.01,
		"output_cost_per_1k": 0.02,
	}, adminClaims)
	w := httptest.NewRecorder()
	r.costOverrideHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	result := parseJSON(t, w)
	assert.Equal(t, "updated", result["status"])
}

func TestExtendedHandlers_AuthRequired(t *testing.T) {
	r := extendedTestRouter()
	handlers := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		path   string
	}{
		{"metrics", r.middlewareMetricsHandler, "GET", "/middleware/metrics"},
		{"patterns", r.middlewarePatternsHandler, "GET", "/middleware/patterns"},
		{"batch", r.batchTaskHandler, "POST", "/tasks/batch"},
		{"health", r.healthStatsHandler, "GET", "/providers/health"},
		{"costOverride", r.costOverrideHandler, "POST", "/providers/cost-override"},
	}
	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			req := httptest.NewRequest(h.method, h.path, nil)
			w := httptest.NewRecorder()
			h.fn(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}

func newTestRouterForAdmin() *Router {
	return &Router{
		Mux: chi.NewMux(),
	}
}

func TestAdminMiddleware_RejectsNonAdmin(t *testing.T) {
	r := newTestRouterForAdmin()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := reqWithClaims("GET", "/admin/stats", nil, testClaims)
	w := httptest.NewRecorder()
	r.adminMiddleware(dummy).ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin, got %d", w.Code)
	}
}

func TestAdminMiddleware_RejectsMissingClaims(t *testing.T) {
	r := newTestRouterForAdmin()
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest("GET", "/admin/stats", nil)
	w := httptest.NewRecorder()
	r.adminMiddleware(dummy).ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for missing claims, got %d", w.Code)
	}
}

func TestAdminMiddleware_AllowsAdmin(t *testing.T) {
	r := newTestRouterForAdmin()
	admin := &auth.Claims{UserID: "a", Email: "a@a.com", Role: "admin"}
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := reqWithClaims("GET", "/admin/stats", nil, admin)
	w := httptest.NewRecorder()
	r.adminMiddleware(dummy).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("admin should pass, got %d", w.Code)
	}
}

func TestAdminMiddleware_AllowsSuperAdmin(t *testing.T) {
	r := newTestRouterForAdmin()
	super := &auth.Claims{UserID: "s", Email: "s@s.com", Role: "superadmin"}
	dummy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := reqWithClaims("GET", "/admin/stats", nil, super)
	w := httptest.NewRecorder()
	r.adminMiddleware(dummy).ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("superadmin should pass, got %d", w.Code)
	}
}

func TestAdminRoleValidation_InvalidRolesRejected(t *testing.T) {
	validRoles := map[string]bool{"user": true, "admin": true, "superadmin": true}
	invalid := []string{"root", "superuser", "moderator", ""}
	for _, role := range invalid {
		if validRoles[role] {
			t.Errorf("role %q should be invalid", role)
		}
	}
}

func TestAdminRoleValidation_ValidRolesAccepted(t *testing.T) {
	validRoles := map[string]bool{"user": true, "admin": true, "superadmin": true}
	for _, role := range []string{"user", "admin", "superadmin"} {
		if !validRoles[role] {
			t.Errorf("role %q should be valid", role)
		}
	}
}

func TestRegisterHandler_MissingBody(t *testing.T) {
	r := newTestRouterForAdmin()
	req := httptest.NewRequest("POST", "/auth/register", nil)
	w := httptest.NewRecorder()
	r.registerHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestRegisterHandler_MissingEmail(t *testing.T) {
	r := newTestRouterForAdmin()
	b, _ := json.Marshal(map[string]string{"password": "123456789012"})
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.registerHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing email, got %d", w.Code)
	}
}

func TestRegisterHandler_InvalidEmailNoAt(t *testing.T) {
	r := newTestRouterForAdmin()
	b, _ := json.Marshal(map[string]string{"email": "invalid-email", "password": "123456789012"})
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.registerHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing @, got %d", w.Code)
	}
}

func TestRegisterHandler_InvalidEmailNoDot(t *testing.T) {
	r := newTestRouterForAdmin()
	b, _ := json.Marshal(map[string]string{"email": "user@invalid", "password": "123456789012"})
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.registerHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing ., got %d", w.Code)
	}
}

func TestRegisterHandler_ShortPassword(t *testing.T) {
	r := newTestRouterForAdmin()
	b, _ := json.Marshal(map[string]string{"email": "u@e.com", "password": "short"})
	req := httptest.NewRequest("POST", "/auth/register", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.registerHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for short password, got %d", w.Code)
	}
}

func TestLoginHandler_EmptyBody(t *testing.T) {
	r := newTestRouterForAdmin()
	req := httptest.NewRequest("POST", "/auth/login", nil)
	w := httptest.NewRecorder()
	r.loginHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}
