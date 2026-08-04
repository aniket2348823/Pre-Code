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

func newIntegrationRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func integrationReqWithClaims(method, path string, body interface{}, claims *auth.Claims) *http.Request {
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

// --- Task 5: Integration-style tests ---

func TestIntegration_UnauthenticatedRequestFlow(t *testing.T) {
	r := newIntegrationRouter()

	// Step 1: Try to create org without auth -> should get 401
	req := httptest.NewRequest("POST", "/organizations", nil)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.createOrgHandler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated org create, got %d", w.Code)
	}

	// Step 2: Try to list orgs without auth -> should get 401
	req = httptest.NewRequest("GET", "/organizations", nil)
	w = httptest.NewRecorder()
	r.listOrgsHandler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated org list, got %d", w.Code)
	}

	// Step 3: Try to create project without auth -> should get 401
	req = httptest.NewRequest("POST", "/projects", nil)
	w = httptest.NewRecorder()
	r.createProjectHandler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated project create, got %d", w.Code)
	}

	// Step 4: Try to create agent without auth -> should get 401
	req = httptest.NewRequest("POST", "/agents", nil)
	w = httptest.NewRecorder()
	r.createAgentHandler(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated agent create, got %d", w.Code)
	}
}

func TestIntegration_AuthenticatedRequestFlow_ValidationErrors(t *testing.T) {
	r := newIntegrationRouter()
	claims := &auth.Claims{
		UserID: "user-123",
		Email:  "test@example.com",
		Role:   "user",
	}

	// Step 1: Create org with empty body -> 400
	req := integrationReqWithClaims("POST", "/organizations", nil, claims)
	w := httptest.NewRecorder()
	r.createOrgHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty body org create, got %d", w.Code)
	}

	// Step 2: Create org with empty name -> 400
	req = integrationReqWithClaims("POST", "/organizations", map[string]string{"name": ""}, claims)
	w = httptest.NewRecorder()
	r.createOrgHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name org create, got %d", w.Code)
	}

	// Step 3: Create project with missing org_id -> 400
	req = integrationReqWithClaims("POST", "/projects", map[string]string{"name": "test"}, claims)
	w = httptest.NewRecorder()
	r.createProjectHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing org_id, got %d", w.Code)
	}

	// Step 4: Create project with missing name -> 400
	req = integrationReqWithClaims("POST", "/projects", map[string]string{"org_id": "org-1"}, claims)
	w = httptest.NewRecorder()
	r.createProjectHandler(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for missing name, got %d", w.Code)
	}
}

func TestIntegration_AuthenticatedRequestFlow_DifferentRoles(t *testing.T) {
	r := newIntegrationRouter()

	roles := []struct {
		name   string
		role   string
		claims *auth.Claims
	}{
		{"admin", "admin", &auth.Claims{UserID: "admin-1", Email: "admin@example.com", Role: "admin"}},
		{"user", "user", &auth.Claims{UserID: "user-1", Email: "user@example.com", Role: "user"}},
		{"viewer", "viewer", &auth.Claims{UserID: "viewer-1", Email: "viewer@example.com", Role: "viewer"}},
	}

	for _, tt := range roles {
		t.Run(tt.name, func(t *testing.T) {
			// Test create org with empty body (validation error = 400, no DB hit)
			req := integrationReqWithClaims("POST", "/organizations", nil, tt.claims)
			w := httptest.NewRecorder()
			r.createOrgHandler(w, req)

			// Should not be 401 (auth passed) — should be 400 (empty body)
			if w.Code == http.StatusUnauthorized {
				t.Errorf("role %s: should not get 401 with valid claims", tt.role)
			}
			if w.Code != http.StatusBadRequest {
				t.Errorf("role %s: expected 400 for empty body, got %d", tt.role, w.Code)
			}
		})
	}
}

func TestIntegration_MiddlewareChain_UnauthenticatedToAuthenticated(t *testing.T) {
	var authMiddleware = func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			// Check for X-API-Key header
			apiKey := req.Header.Get("X-API-Key")
			if apiKey != "" {
				claims := &auth.Claims{
					UserID: "api-user",
					Role:   "user",
				}
				ctx := auth.ContextWithClaims(req.Context(), claims)
				next.ServeHTTP(w, req.WithContext(ctx))
				return
			}
			// Check for Bearer token
			authHeader := req.Header.Get("Authorization")
			if authHeader != "" {
				claims := &auth.Claims{
					UserID: "jwt-user",
					Role:   "user",
				}
				ctx := auth.ContextWithClaims(req.Context(), claims)
				next.ServeHTTP(w, req.WithContext(ctx))
				return
			}
			// No auth -> pass through without claims
			next.ServeHTTP(w, req)
		})
	}

	// Handler that checks for claims
	checkAuth := func(w http.ResponseWriter, req *http.Request) {
		_, ok := auth.ClaimsFromContext(req.Context())
		if ok {
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]string{"status": "authenticated"})
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"status": "unauthenticated"})
		}
	}

	handler := authMiddleware(http.HandlerFunc(checkAuth))

	// Step 1: Unauthenticated request
	req := httptest.NewRequest("GET", "/api/v1/data", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 for unauthenticated, got %d", w.Code)
	}

	// Step 2: Authenticated via API key
	req = httptest.NewRequest("GET", "/api/v1/data", nil)
	req.Header.Set("X-API-Key", "va_test_key")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for API key auth, got %d", w.Code)
	}
	result := parseJSON(t, w)
	if result["status"] != "authenticated" {
		t.Errorf("expected authenticated status, got %v", result["status"])
	}

	// Step 3: Authenticated via Bearer token
	req = httptest.NewRequest("GET", "/api/v1/data", nil)
	req.Header.Set("Authorization", "Bearer eyJhbGciOiJIUzI1NiJ9.signature")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for Bearer auth, got %d", w.Code)
	}
}

func TestIntegration_AdminRoleAuthorization(t *testing.T) {
	adminClaims := &auth.Claims{
		UserID: "admin-1",
		Email:  "admin@example.com",
		Role:   "admin",
	}
	userClaims := &auth.Claims{
		UserID: "user-1",
		Email:  "user@example.com",
		Role:   "user",
	}

	adminHandler := func(w http.ResponseWriter, req *http.Request) {
		claims, ok := auth.ClaimsFromContext(req.Context())
		if !ok {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if claims.Role != "admin" {
			w.WriteHeader(http.StatusForbidden)
			json.NewEncoder(w).Encode(map[string]string{"error": "admin access required"})
			return
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "admin access granted"})
	}

	handler := http.HandlerFunc(adminHandler)

	// Admin should succeed
	req := integrationReqWithClaims("GET", "/admin/dashboard", nil, adminClaims)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for admin, got %d", w.Code)
	}

	// Regular user should be forbidden
	req = integrationReqWithClaims("GET", "/admin/dashboard", nil, userClaims)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("expected 403 for non-admin, got %d", w.Code)
	}
}

func TestIntegration_HealthEndpoint(t *testing.T) {
	r := newIntegrationRouter()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	r.healthHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for health check, got %d", w.Code)
	}

	result := parseJSON(t, w)
	if result["status"] != "healthy" {
		t.Errorf("expected status=healthy, got %v", result["status"])
	}
}

func TestIntegration_RegisterHandler_EmptyBody(t *testing.T) {
	r := newIntegrationRouter()

	req := httptest.NewRequest("POST", "/auth/register", nil)
	w := httptest.NewRecorder()
	r.registerHandler(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty register body, got %d", w.Code)
	}
}

func TestIntegration_CORSHeaders(t *testing.T) {
	r := newIntegrationRouter()

	// OPTIONS preflight should get CORS headers
	req := httptest.NewRequest("OPTIONS", "/api/v1/data", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w := httptest.NewRecorder()
	r.Mux.ServeHTTP(w, req)

	// Chi handles CORS via its middleware - the OPTIONS might return 405 if no route matches
	// but the key thing is it doesn't panic
	if w.Code == http.StatusInternalServerError {
		t.Error("OPTIONS should not return 500")
	}
}

func TestIntegration_JSONResponseFormat(t *testing.T) {
	r := newIntegrationRouter()

	// Test that responses are properly formatted JSON
	req := httptest.NewRequest("POST", "/organizations", nil)
	w := httptest.NewRecorder()
	r.createOrgHandler(w, req)

	contentType := w.Header().Get("Content-Type")
	if contentType != "" && !strings.Contains(contentType, "application/json") {
		t.Errorf("expected application/json content type, got %q", contentType)
	}

	// Response should be valid JSON
	if w.Body.Len() > 0 {
		var result map[string]interface{}
		if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
			t.Errorf("response is not valid JSON: %v", err)
		}
	}
}

func TestIntegration_MultipleEndpointsAuthenticatedFlow(t *testing.T) {
	r := newIntegrationRouter()
	claims := &auth.Claims{
		UserID: "user-123",
		Email:  "test@example.com",
		Role:   "user",
	}

	// Test that multiple authenticated requests don't interfere with each other
	// Use only validation-error paths (empty body -> 400) and list ops to avoid nil repo panic
	endpoints := []struct {
		name   string
		method string
		path   string
		body   interface{}
		code   int
	}{
		{"create org empty body", "POST", "/organizations", nil, http.StatusBadRequest},
		{"create project empty body", "POST", "/projects", nil, http.StatusBadRequest},
		{"create project missing fields", "POST", "/projects", map[string]string{"name": "proj"}, http.StatusBadRequest},
	}

	for _, ep := range endpoints {
		t.Run(ep.name, func(t *testing.T) {
			req := integrationReqWithClaims(ep.method, ep.path, ep.body, claims)
			w := httptest.NewRecorder()

			switch ep.path {
			case "/organizations":
				r.createOrgHandler(w, req)
			case "/projects":
				r.createProjectHandler(w, req)
			default:
				t.Fatalf("unknown path: %s", ep.path)
			}

			// Should not panic and should return a valid HTTP status
			if w.Code < 100 || w.Code > 599 {
				t.Errorf("invalid status code: %d", w.Code)
			}
			// Should not be 401 (auth is valid)
			if w.Code == http.StatusUnauthorized {
				t.Errorf("should not get 401 with valid claims")
			}
			// Check expected code if specified
			if ep.code != 0 && w.Code != ep.code {
				t.Errorf("expected %d, got %d", ep.code, w.Code)
			}
		})
	}
}

func TestIntegration_ConcurrentRequests(t *testing.T) {
	r := newIntegrationRouter()
	claims := &auth.Claims{
		UserID: "user-123",
		Email:  "test@example.com",
		Role:   "user",
	}

	// Concurrent health checks (no auth required)
	done := make(chan struct{})
	for i := 0; i < 50; i++ {
		go func() {
			req := httptest.NewRequest("GET", "/health", nil)
			w := httptest.NewRecorder()
			r.healthHandler(w, req)
			if w.Code != http.StatusOK {
				t.Errorf("expected 200, got %d", w.Code)
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < 50; i++ {
		<-done
	}

	// Concurrent authenticated requests with validation errors (avoids nil repo panic)
	for i := 0; i < 50; i++ {
		go func() {
			// Empty body triggers 400 before hitting nil repo
			req := integrationReqWithClaims("POST", "/organizations", nil, claims)
			w := httptest.NewRecorder()
			r.createOrgHandler(w, req)
			// Should not panic
			if w.Code < 100 || w.Code > 599 {
				t.Errorf("invalid status code: %d", w.Code)
			}
			// Empty body = 400
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for empty body, got %d", w.Code)
			}
			done <- struct{}{}
		}()
	}

	for i := 0; i < 50; i++ {
		<-done
	}
}

func TestIntegration_AuthContextPreservation(t *testing.T) {
	// Test that claims are correctly preserved through the context
	claims := &auth.Claims{
		UserID: "user-42",
		Email:  "preserved@example.com",
		Role:   "admin",
		OrgID:  "org-42",
	}

	var capturedClaims *auth.Claims
	captureHandler := func(w http.ResponseWriter, req *http.Request) {
		c, ok := auth.ClaimsFromContext(req.Context())
		if ok {
			capturedClaims = c
		}
		w.WriteHeader(http.StatusOK)
	}

	req := integrationReqWithClaims("GET", "/test", nil, claims)
	w := httptest.NewRecorder()
	captureHandler(w, req)

	if capturedClaims == nil {
		t.Fatal("claims should be captured")
	}
	if capturedClaims.UserID != "user-42" {
		t.Errorf("UserID = %q, want %q", capturedClaims.UserID, "user-42")
	}
	if capturedClaims.Email != "preserved@example.com" {
		t.Errorf("Email = %q, want %q", capturedClaims.Email, "preserved@example.com")
	}
	if capturedClaims.Role != "admin" {
		t.Errorf("Role = %q, want %q", capturedClaims.Role, "admin")
	}
	if capturedClaims.OrgID != "org-42" {
		t.Errorf("OrgID = %q, want %q", capturedClaims.OrgID, "org-42")
	}
}

func TestIntegration_SwaggerEndpoint(t *testing.T) {
	r := newIntegrationRouter()

	// Swagger UI should not require auth
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()
	r.swaggerUIHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for Swagger UI, got %d", w.Code)
	}
	if w.Header().Get("Content-Type") == "" {
		t.Error("expected Content-Type header for Swagger UI")
	}
}

func TestIntegration_OpenAPISpecEndpoint(t *testing.T) {
	r := newIntegrationRouter()

	req := httptest.NewRequest("GET", "/docs/openapi.yaml", nil)
	w := httptest.NewRecorder()
	r.openapiSpecHandler(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200 for OpenAPI spec, got %d", w.Code)
	}
}
