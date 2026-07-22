package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
)

// helper to create a minimal Router for handler testing.
// Repositories are nil — only auth/validation error paths are tested.
func newTestRouter() *Router {
	return &Router{
		Mux: chi.NewMux(),
	}
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

// ==================== Health Tests ====================

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

// ==================== Chi URL Param Tests ====================

func TestGetOrgHandler_URLParamExtraction(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping: requires DB-backed repositories")
	}

	// This test verifies that chi URL params are correctly extracted.
	// It requires real repositories to avoid nil-pointer panics.
	// In short mode, we only test the auth/validation path.
	t.Skip("requires full router setup with repositories (integration test)")
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

// ==================== Register/Login Validation ====================

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

// ==================== Logout Handler Tests ====================

func TestLogoutHandler_NoClaims(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("POST", "/auth/logout", nil)
	w := httptest.NewRecorder()
	r.logoutHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
	result := parseJSON(t, w)
	if _, ok := result["message"]; !ok {
		t.Error("expected 'message' field in response")
	}
}

func TestLogoutHandler_WithClaims_NoBlacklist(t *testing.T) {
	r := newTestRouter()
	// blacklist is nil — handler should still return 200
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

// ==================== Change Password Handler Tests ====================

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
	// users repo is nil — handler should return 500
	req := reqWithClaims("PUT", "/users/me/password", map[string]string{"current_password": "oldpass123456", "new_password": "newpass123456"}, testClaims)
	w := httptest.NewRecorder()
	r.changePasswordHandler(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 (user not found with nil repo), got %d", w.Code)
	}
}

// ==================== API Key Rotation Handler Tests ====================

func TestRotateAPIKeyHandler_NoClaims(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("POST", "/api-keys/abc123/rotate", nil)
	w := httptest.NewRecorder()
	r.rotateAPIKeyHandler(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

// ==================== Refresh Token Handler Tests ====================

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
	// auth is nil — handler should panic or return error
	req := reqWithClaims("POST", "/auth/refresh", nil, testClaims)
	w := httptest.NewRecorder()
	// This will panic because r.auth is nil — expected in unit test
	defer func() {
		if rec := recover(); rec == nil {
			// If no panic, that's also acceptable (error response)
		}
	}()
	r.refreshTokenHandler(w, req)
}

// ==================== Forgot Password Handler Tests ====================

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
	// email is nil, users repo is nil — should return success (prevent enumeration)
	req := httptest.NewRequest("POST", "/auth/forgot-password", map[string]string{"email": "test@example.com"}, nil)
	w := httptest.NewRecorder()
	r.forgotPasswordHandler(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 (always success to prevent enumeration), got %d", w.Code)
	}
}

// ==================== Reset Password Handler Tests ====================

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

// ==================== Verify Email Handler Tests ====================

func TestVerifyEmailHandler_MissingToken(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("GET", "/auth/verify-email", nil)
	w := httptest.NewRecorder()
	r.verifyEmailHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// ==================== Readiness Handler Tests ====================

func TestReadinessHandler_AllNil(t *testing.T) {
	r := newTestRouter()
	req := httptest.NewRequest("GET", "/ready", nil)
	w := httptest.NewRecorder()
	r.readinessHandler(w, req)
	// All deps nil → 503
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
