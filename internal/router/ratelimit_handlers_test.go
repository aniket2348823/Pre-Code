package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/auth"
)

// Content from ratelimit_handlers_test.go
func ratelimitTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestOrgPlanExtractor_NoClaims(t *testing.T) {
	r := ratelimitTestRouter()
	req := httptest.NewRequest("GET", "/test", nil)
	orgID, plan := r.orgPlanExtractor(req)
	assert.Equal(t, "", orgID)
	assert.Equal(t, "free", string(plan))
}

func TestOrgPlanExtractor_WithClaimsNoOrgID(t *testing.T) {
	r := ratelimitTestRouter()
	req := reqWithClaims("GET", "/test", nil, testClaims)
	orgID, plan := r.orgPlanExtractor(req)
	assert.Equal(t, "", orgID)
	assert.Equal(t, "free", string(plan))
}

func TestOrgPlanExtractor_WithClaimsAndOrgIDQuery(t *testing.T) {
	r := ratelimitTestRouter()
	req := reqWithClaims("GET", "/test?org_id=org-123", nil, testClaims)
	orgID, plan := r.orgPlanExtractor(req)
	assert.Equal(t, "org-123", orgID)
	// db is nil, so plan stays free
	assert.Equal(t, "free", string(plan))
}

func TestOrgPlanExtractor_NilDB(t *testing.T) {
	r := ratelimitTestRouter()
	r.db = nil
	req := reqWithClaims("GET", "/test?org_id=org-456", nil, testClaims)
	orgID, plan := r.orgPlanExtractor(req)
	assert.Equal(t, "org-456", orgID)
	assert.Equal(t, "free", string(plan))
}

func TestOrgPlanExtractor_WithClaimsOrgIDInClaims(t *testing.T) {
	r := ratelimitTestRouter()
	claims := &auth.Claims{
		UserID: "user-1",
		Email:  "test@example.com",
		Role:   "user",
		OrgID:  "org-from-claims",
	}
	req := reqWithClaims("GET", "/test", nil, claims)
	orgID, plan := r.orgPlanExtractor(req)
	assert.Equal(t, "org-from-claims", orgID)
	assert.Equal(t, "free", string(plan))
}

func TestOrgPlanExtractor_QueryParamTakesPriority(t *testing.T) {
	r := ratelimitTestRouter()
	claims := &auth.Claims{
		UserID: "user-1",
		Email:  "test@example.com",
		Role:   "user",
		OrgID:  "org-from-claims",
	}
	req := reqWithClaims("GET", "/test?org_id=org-from-query", nil, claims)
	orgID, _ := r.orgPlanExtractor(req)
	assert.Equal(t, "org-from-query", orgID)
}

func TestExtractAPIKeyFromRequest(t *testing.T) {
	tests := []struct {
		name     string
		headers  map[string]string
		expected string
	}{
		{
			name:     "X-API-Key header",
			headers:  map[string]string{"X-API-Key": "va_testkey123"},
			expected: "va_testkey123",
		},
		{
			name:     "Bearer token with underscore (API key)",
			headers:  map[string]string{"Authorization": "Bearer va_test_key123"},
			expected: "va_test_key123",
		},
		{
			name:     "Bearer JWT (has dots)",
			headers:  map[string]string{"Authorization": "Bearer eyJhbGci.eyJ1aWQ.abc"},
			expected: "",
		},
		{
			name:     "Bearer with no underscore",
			headers:  map[string]string{"Authorization": "Bearer abcdefgh"},
			expected: "",
		},
		{
			name:     "No auth header",
			headers:  map[string]string{},
			expected: "",
		},
		{
			name:     "Bearer with only prefix",
			headers:  map[string]string{"Authorization": "Bearer va_"},
			expected: "va_",
		},
		{
			name:     "Empty X-API-Key header",
			headers:  map[string]string{"X-API-Key": ""},
			expected: "",
		},
		{
			name:     "Non-Bearer auth",
			headers:  map[string]string{"Authorization": "Basic abc123"},
			expected: "",
		},
		{
			name:     "Bearer with single part",
			headers:  map[string]string{"Authorization": "Bearer"},
			expected: "",
		},
		{
			name:     "X-API-Key takes priority over Bearer",
			headers:  map[string]string{"X-API-Key": "va_first", "Authorization": "Bearer va_second_key"},
			expected: "va_first",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			result := extractAPIKeyFromRequest(req)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestRateLimitKeyFromAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"long key truncated", "va_abcdef123456", "va_abcde"},
		{"exactly 8 chars", "va_12345", "va_12345"},
		{"short key", "va_x", "va_x"},
		{"empty key", "", ""},
		{"7 chars", "abcdefg", "abcdefg"},
		{"9 chars", "123456789", "12345678"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := rateLimitKeyFromAPIKey(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestApiKeyRateLimitMiddleware_NilLimiter(t *testing.T) {
	r := &Router{Mux: chi.NewMux()}
	inner := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// rl is nil — should return next handler without wrapping
	handler := r.apiKeyRateLimitMiddleware(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}
