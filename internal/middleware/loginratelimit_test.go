package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLoginRateLimiter_AllowsWithinLimit(t *testing.T) {
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts: 5,
		Window:      1 * time.Minute,
		LockoutDurations: []time.Duration{1 * time.Minute},
	})

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	for i := 0; i < 4; i++ {
		body := strings.NewReader(`{"email":"test@example.com","password":"wrong"}`)
		req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
		req.RemoteAddr = "10.0.0.1:1234"
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		assert.Equal(t, http.StatusOK, w.Code, "attempt %d should pass", i+1)
	}
}

func TestLoginRateLimiter_BlocksAfterMaxAttempts(t *testing.T) {
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts: 3,
		Window:      1 * time.Minute,
		LockoutDurations: []time.Duration{1 * time.Minute},
	})

	ctx := context.Background()

	// Simulate 3 failures via RecordFailure (as the handler would do)
	for i := 0; i < 3; i++ {
		l.RecordFailure(ctx, "10.0.0.1", "victim@example.com")
	}

	// Now the middleware should block
	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(`{"email":"victim@example.com","password":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
}

func TestLoginRateLimiter_ProgressiveLockout(t *testing.T) {
	lockoutDurations := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		500 * time.Millisecond,
	}
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts:      2,
		Window:           1 * time.Minute,
		LockoutDurations: lockoutDurations,
	})

	ctx := context.Background()
	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First lockout (level 0: 100ms)
	for i := 0; i < 2; i++ {
		l.RecordFailure(ctx, "10.0.0.2", "prog@example.com")
	}

	body := strings.NewReader(`{"email":"prog@example.com","password":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.2:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// Wait for first lockout to expire
	time.Sleep(150 * time.Millisecond)

	// Next lockout should be longer (level 1: 200ms)
	for i := 0; i < 2; i++ {
		l.RecordFailure(ctx, "10.0.0.2", "prog@example.com")
	}

	body = strings.NewReader(`{"email":"prog@example.com","password":"wrong"}`)
	req = httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.2:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
}

func TestLoginRateLimiter_DifferentIPsIndependent(t *testing.T) {
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts: 2,
		Window:      1 * time.Minute,
		LockoutDurations: []time.Duration{1 * time.Minute},
	})

	ctx := context.Background()

	// Lock out IP 1
	l.RecordFailure(ctx, "10.0.0.1", "same@example.com")
	l.RecordFailure(ctx, "10.0.0.1", "same@example.com")

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// IP 1 blocked
	body := strings.NewReader(`{"email":"same@example.com","password":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// IP 2 still allowed
	body = strings.NewReader(`{"email":"same@example.com","password":"wrong"}`)
	req = httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.99:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLoginRateLimiter_DifferentEmailsIndependent(t *testing.T) {
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts: 2,
		Window:      1 * time.Minute,
		LockoutDurations: []time.Duration{1 * time.Minute},
	})

	ctx := context.Background()

	// Lock out email A
	l.RecordFailure(ctx, "10.0.0.1", "a@example.com")
	l.RecordFailure(ctx, "10.0.0.1", "a@example.com")

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Email A blocked
	body := strings.NewReader(`{"email":"a@example.com","password":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	// Email B still allowed
	body = strings.NewReader(`{"email":"b@example.com","password":"wrong"}`)
	req = httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.1:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLoginRateLimiter_SkipsNonPOST(t *testing.T) {
	l := NewLoginRateLimiter(nil, DefaultLoginRateLimiterConfig())

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/auth/login", nil)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLoginRateLimiter_SetsRetryAfterHeader(t *testing.T) {
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts: 1,
		Window:      1 * time.Minute,
		LockoutDurations: []time.Duration{5 * time.Minute},
	})

	ctx := context.Background()
	l.RecordFailure(ctx, "10.0.0.1", "retry@example.com")

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(`{"email":"retry@example.com","password":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	retryAfter := w.Header().Get("Retry-After")
	assert.NotEmpty(t, retryAfter)
	assert.True(t, len(retryAfter) > 0)
}

func TestLoginRateLimiter_RecordSuccess(t *testing.T) {
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts: 2,
		Window:      1 * time.Minute,
		LockoutDurations: []time.Duration{1 * time.Minute},
	})

	ctx := context.Background()

	l.RecordFailure(ctx, "10.0.0.1", "user@test.com")
	l.RecordFailure(ctx, "10.0.0.1", "user@test.com")

	assert.True(t, l.IsLocked(ctx, "10.0.0.1", "user@test.com"))

	l.RecordSuccess(ctx, "10.0.0.1", "user@test.com")
	assert.False(t, l.IsLocked(ctx, "10.0.0.1", "user@test.com"))
}

func TestLoginRateLimiter_GetRemainingLockout(t *testing.T) {
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts: 1,
		Window:      1 * time.Minute,
		LockoutDurations: []time.Duration{5 * time.Minute},
	})

	ctx := context.Background()

	remaining := l.GetRemainingLockout(ctx, "10.0.0.1", "test@test.com")
	assert.Equal(t, time.Duration(0), remaining)

	l.RecordFailure(ctx, "10.0.0.1", "test@test.com")
	remaining = l.GetRemainingLockout(ctx, "10.0.0.1", "test@test.com")
	assert.True(t, remaining > 0, "should have positive remaining lockout")
}

func TestLoginRateLimiter_EmptyEmailSkips(t *testing.T) {
	l := NewLoginRateLimiter(nil, DefaultLoginRateLimiterConfig())

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(`{"password":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLoginRateLimiter_WindowReset(t *testing.T) {
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts: 2,
		Window:      50 * time.Millisecond,
		LockoutDurations: []time.Duration{100 * time.Millisecond},
	})

	ctx := context.Background()

	l.RecordFailure(ctx, "10.0.0.1", "window@test.com")
	l.RecordFailure(ctx, "10.0.0.1", "window@test.com")
	assert.True(t, l.IsLocked(ctx, "10.0.0.1", "window@test.com"))

	// Wait for lockout + window to expire
	time.Sleep(200 * time.Millisecond)
	assert.False(t, l.IsLocked(ctx, "10.0.0.1", "window@test.com"))
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		name       string
		remoteAddr string
		expected   string
	}{
		{"IPv4 with port", "10.0.0.1:8080", "10.0.0.1"},
		{"IPv4 without port", "10.0.0.1", "10.0.0.1"},
		{"IPv6 with port", "[::1]:8080", "::1"},
		{"IPv6 without port", "::1", "::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.RemoteAddr = tt.remoteAddr
			got := extractIP(req)
			assert.Equal(t, tt.expected, got)
		})
	}
}
