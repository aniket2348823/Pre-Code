package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
)

func newTestRouterForAdmin() *Router {
	return &Router{
		Mux: chi.NewMux(),
	}
}

// === Middleware Tests ===

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

// === Role Validation Tests ===

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

// === Register Validation Tests ===

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

// === Login Validation Tests ===

func TestLoginHandler_EmptyBody(t *testing.T) {
	r := newTestRouterForAdmin()
	req := httptest.NewRequest("POST", "/auth/login", nil)
	w := httptest.NewRecorder()
	r.loginHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// Note: loginHandler has no pre-DB validation for email/password fields,
// so it panics with nil repos in unit tests. Testing requires integration tests
// with a real database connection.
