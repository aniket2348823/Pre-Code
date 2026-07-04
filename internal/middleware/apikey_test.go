package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractAPIKey_AllSources(t *testing.T) {
	// Test all extraction paths in one table-driven test
	tests := []struct {
		name     string
		xAPIKey  string
		auth     string
		expected string
	}{
		{"X-API-Key header", "va_key_1", "", "va_key_1"},
		{"Bearer API key", "", "Bearer va_key_2", "va_key_2"},
		{"Bearer JWT ignored", "", "Bearer eyJ.payload.sig", ""},
		{"No headers", "", "", ""},
		{"X-API-Key priority over Bearer", "va_header", "Bearer va_bearer", "va_header"},
		{"Bearer without underscore ignored", "", "Bearer sometoken", ""},
		{"Bearer with empty token", "", "Bearer ", ""},
		{"Malformed auth header", "", "Bearer", ""},
		{"Basic auth ignored", "", "Basic dXNlcjpwYXNz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.xAPIKey != "" {
				req.Header.Set("X-API-Key", tt.xAPIKey)
			}
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}

			got := extractAPIKey(req)
			if got != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestHashKey_Consistency(t *testing.T) {
	h1 := hashKey("test-key")
	h2 := hashKey("test-key")
	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-char hex hash, got %d chars", len(h1))
	}

	h3 := hashKey("different-key")
	if h1 == h3 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestAPIKeyAuthError_Messages(t *testing.T) {
	if ErrInvalidAPIKey.Error() != "invalid or unknown API key" {
		t.Errorf("unexpected: %q", ErrInvalidAPIKey.Error())
	}
	if ErrExpiredAPIKey.Error() != "API key has expired" {
		t.Errorf("unexpected: %q", ErrExpiredAPIKey.Error())
	}
}
