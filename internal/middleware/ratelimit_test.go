package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestRateLimitByIPKey(t *testing.T) {
	tests := []struct {
		name        string
		remoteAddr  string
		expectedKey string
	}{
		{"IPv4 with port", "5.6.7.8:1234", "ip:5.6.7.8"},
		{"IPv4 without port", "192.168.1.1", "ip:192.168.1.1"},
		{"IPv6 with port", "[::1]:8080", "ip:::1"},
		{"IPv6 without port", "::1", "ip:::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr

			got := RateLimitByIPKey(req)
			assert.Equal(t, tt.expectedKey, got)
		})
	}
}

func TestRateLimitHeadersMiddleware(t *testing.T) {
	rlm := NewRateLimitHeadersMiddleware(10, time.Minute)

	handler := rlm.Middleware(func(r *http.Request) string {
		return "test-key"
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "10", w.Header().Get("X-RateLimit-Limit"))
	assert.Equal(t, "9", w.Header().Get("X-RateLimit-Remaining"))
	assert.NotEmpty(t, w.Header().Get("X-RateLimit-Reset"))
}

func TestRateLimitHeadersMiddleware_DecrementsRemaining(t *testing.T) {
	rlm := NewRateLimitHeadersMiddleware(5, time.Minute)
	handler := rlm.Middleware(func(r *http.Request) string {
		return "decr-key"
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
	}

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, "1", w.Header().Get("X-RateLimit-Remaining"))
}

func TestRateLimitHeadersMiddleware_EmptyKeyPassesThrough(t *testing.T) {
	rlm := NewRateLimitHeadersMiddleware(5, time.Minute)
	nextCalled := false
	handler := rlm.Middleware(func(r *http.Request) string {
		return ""
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.True(t, nextCalled)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Empty(t, w.Header().Get("X-RateLimit-Limit"))
}

func TestRateLimitHeadersMiddleware_DifferentKeysIndependent(t *testing.T) {
	rlm := NewRateLimitHeadersMiddleware(10, time.Minute)
	handler := rlm.Middleware(func(r *http.Request) string {
		return r.Header.Get("X-Key")
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Key", "key-a")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "9", w.Header().Get("X-RateLimit-Remaining"))

	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-Key", "key-b")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "9", w.Header().Get("X-RateLimit-Remaining"), "different key should have full remaining")
}

func TestNewRateLimitHeadersMiddleware(t *testing.T) {
	rlm := NewRateLimitHeadersMiddleware(100, 30*time.Second)
	assert.NotNil(t, rlm)
	assert.Equal(t, 100, rlm.limit)
	assert.Equal(t, 30*time.Second, rlm.window)
}

func TestRateLimitByIPKey_IgnoresHeaders(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:9999"
	req.Header.Set("X-Forwarded-For", "1.2.3.4")
	req.Header.Set("X-Real-IP", "5.6.7.8")
	req.Header.Set("X-Forwarded-Host", "spoofed.com")

	got := RateLimitByIPKey(req)
	assert.Equal(t, "ip:10.0.0.1", got, "must use RemoteAddr, not proxy headers")
}

func TestRateLimitByUser_UsesXUserID(t *testing.T) {
	keyFunc := func(r *http.Request) string {
		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			userID = r.RemoteAddr
		}
		return "user:" + userID
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "127.0.0.1:8080"
	req.Header.Set("X-User-ID", "user-42")

	got := keyFunc(req)
	assert.Equal(t, "user:user-42", got, "should extract user from X-User-ID header")
}

func TestRateLimitByUser_FallsBackToRemoteAddr(t *testing.T) {
	keyFunc := func(r *http.Request) string {
		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			userID = r.RemoteAddr
		}
		return "user:" + userID
	}

	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.1:5000"

	got := keyFunc(req)
	assert.Equal(t, "user:192.168.1.1:5000", got, "should fall back to RemoteAddr when no X-User-ID")
}
