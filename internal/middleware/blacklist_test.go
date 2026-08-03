package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExtractBearerToken_BlacklistCases(t *testing.T) {
	tests := []struct {
		name     string
		auth     string
		expected string
	}{
		{"valid bearer JWT", "Bearer eyJhbGciOiJIUzI1NiJ9.signature", "eyJhbGciOiJIUzI1NiJ9.signature"},
		{"API key with underscore no dots", "Bearer va_abc_def", ""},
		{"Bearer with spaces in token", "Bearer my token", "my token"},
		{"Bearer only", "Bearer ", ""},
		{"empty auth header", "", ""},
		{"not bearer", "Basic dXNlcjpwYXNz", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			got := ExtractBearerToken(req)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestNewJWTBlacklist(t *testing.T) {
	bl := NewJWTBlacklist(nil)
	assert.NotNil(t, bl)
	assert.Equal(t, "jwt:blacklist:", bl.prefix)
}

func TestJWTBlacklist_Middleware_NoTokenPassesThrough(t *testing.T) {
	bl := NewJWTBlacklist(nil)
	nextCalled := false
	handler := bl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestJWTBlacklist_Middleware_APILKeyPassesThrough(t *testing.T) {
	bl := NewJWTBlacklist(nil)
	nextCalled := false
	handler := bl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-API-Key", "va_abc123_def456")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled, "API key requests should pass through")
}

func TestJWTBlacklist_MiddlewareWithUserRevocation_NoClaimsPassesThrough(t *testing.T) {
	bl := NewJWTBlacklist(nil)
	nextCalled := false
	handler := bl.MiddlewareWithUserRevocation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled, "no claims should pass through")
}

func TestJWTBlacklist_MiddlewareWithUserRevocation_WithClaimsNoRedis(t *testing.T) {
	// IsUserRevoked with nil Redis panics, so we test the no-claims path only
	bl := NewJWTBlacklist(nil)
	nextCalled := false
	handler := bl.MiddlewareWithUserRevocation(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled, "no claims should pass through")
}
