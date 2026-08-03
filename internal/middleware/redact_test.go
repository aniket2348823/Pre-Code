package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactValue(t *testing.T) {
	tests := []struct {
		name     string
		key      string
		value    string
		expected string
	}{
		{"password redacted", "password", "secret123", "***REDACTED***"},
		{"password_hash redacted", "password_hash", "abc", "***REDACTED***"},
		{"api_key redacted", "api_key", "key123", "***REDACTED***"},
		{"api-key redacted", "api-key", "key123", "***REDACTED***"},
		{"x-api-key redacted", "x-api-key", "key123", "***REDACTED***"},
		{"authorization redacted", "authorization", "Bearer tok", "***REDACTED***"},
		{"token redacted", "token", "val", "***REDACTED***"},
		{"access_token redacted", "access_token", "val", "***REDACTED***"},
		{"refresh_token redacted", "refresh_token", "val", "***REDACTED***"},
		{"secret redacted", "secret", "val", "***REDACTED***"},
		{"secret_key redacted", "secret_key", "val", "***REDACTED***"},
		{"jwt_secret redacted", "jwt_secret", "val", "***REDACTED***"},
		{"credit_card redacted", "credit_card", "4111", "***REDACTED***"},
		{"ssn redacted", "ssn", "123-45-6789", "***REDACTED***"},
		{"pin redacted", "pin", "1234", "***REDACTED***"},
		{"case insensitive password", "Password", "val", "***REDACTED***"},
		{"case insensitive Authorization", "Authorization", "Bearer tok", "***REDACTED***"},
		{"non-sensitive passthrough", "username", "admin", "admin"},
		{"non-sensitive Content-Type", "Content-Type", "application/json", "application/json"},
		{"empty value passthrough", "password", "", ""},
		{"empty key passthrough", "", "value", "value"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RedactValue(tt.key, tt.value)
			assert.Equal(t, tt.expected, got)
		})
	}
}

func TestRedactHeaders(t *testing.T) {
	headers := map[string][]string{
		"Authorization":  {"Bearer secret-token"},
		"Content-Type":   {"application/json"},
		"X-API-Key":      {"va_abc123"},
		"X-Custom":       {"custom-value"},
		"Password":       {"hunter2"},
		"Empty-Header":   {},
	}

	result := RedactHeaders(headers)

	assert.Equal(t, "***REDACTED***", result["Authorization"])
	assert.Equal(t, "application/json", result["Content-Type"])
	assert.Equal(t, "***REDACTED***", result["X-API-Key"])
	assert.Equal(t, "custom-value", result["X-Custom"])
	assert.Equal(t, "***REDACTED***", result["Password"])
	_, hasEmpty := result["Empty-Header"]
	assert.False(t, hasEmpty, "empty header should not appear in result")
}

func TestRedactHeaders_Empty(t *testing.T) {
	result := RedactHeaders(map[string][]string{})
	assert.Empty(t, result)
}

func TestRedactLogger_CallsNext(t *testing.T) {
	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})

	handler := RedactLogger(next)
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("X-API-Key", "va_secret_key")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.True(t, called, "RedactLogger must call next handler")
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestRedactLogger_DoesNotLeakAuthHeaders(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	handler := RedactLogger(next)
	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	req.Header.Set("Authorization", "Bearer super-secret-token")
	req.Header.Set("X-API-Key", "va_secret_api_key")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}
