package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/auth"
)

// Content from ratelimit_test.go
// TestRateLimiter_NilClientFailsOpen verifies that a RateLimiter with a nil
// Redis client passes requests through instead of panicking (dev mode without
// Redis). Regression test for the nil-pointer deref in rateLimitScript.Run.
func TestRateLimiter_NilClientFailsOpen(t *testing.T) {
	rl := NewRateLimiter(nil, 100, time.Minute)
	nextCalled := false
	handler := rl.Middleware(func(r *http.Request) string {
		return "test-key"
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/test", nil)
	w := httptest.NewRecorder()
	assert.NotPanics(t, func() { handler.ServeHTTP(w, req) })
	assert.True(t, nextCalled, "next handler must be called when Redis is nil")
	assert.Equal(t, http.StatusOK, w.Code)
}

// TestPlanAwareRateLimiter_NilClientFailsOpen verifies that the plan-aware
// limiter does not panic when Redis is unavailable (nil client) — the daily
// quota Lua path must be skipped, not dereferenced. Regression test for the
// nil-pointer panic found alongside the RateLimiter fix.
func TestPlanAwareRateLimiter_NilClientFailsOpen(t *testing.T) {
	p := NewPlanAwareRateLimiter(nil)
	nextCalled := false
	handler := p.Middleware(func(r *http.Request) (string, PlanTier) {
		return "org-1", PlanFree
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/deep-analyze", nil)
	w := httptest.NewRecorder()
	assert.NotPanics(t, func() { handler.ServeHTTP(w, req) })
	assert.True(t, nextCalled, "next handler must be called when Redis is nil")
	assert.Equal(t, http.StatusOK, w.Code)
}

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

func TestAPIKeyCreateRateLimiter_AllowsWithinLimit(t *testing.T) {
	l := NewAPIKeyCreateRateLimiter(nil, APIKeyCreateRateLimiterConfig{
		MaxKeys: 3,
		Window:  1 * time.Hour,
	})

	for i := 0; i < 2; i++ {
		assert.True(t, l.Allow(nil, "user-1"), "attempt %d should be allowed", i+1)
		l.Record(nil, "user-1")
	}
}

func TestAPIKeyCreateRateLimiter_BlocksAfterLimit(t *testing.T) {
	l := NewAPIKeyCreateRateLimiter(nil, APIKeyCreateRateLimiterConfig{
		MaxKeys: 3,
		Window:  1 * time.Hour,
	})

	l.Record(nil, "user-1")
	l.Record(nil, "user-1")
	l.Record(nil, "user-1")

	assert.False(t, l.Allow(nil, "user-1"), "should be blocked after 3 creations")
}

func TestAPIKeyCreateRateLimiter_DifferentUsersIndependent(t *testing.T) {
	l := NewAPIKeyCreateRateLimiter(nil, APIKeyCreateRateLimiterConfig{
		MaxKeys: 2,
		Window:  1 * time.Hour,
	})

	l.Record(nil, "user-1")
	l.Record(nil, "user-1")

	assert.False(t, l.Allow(nil, "user-1"))

	assert.True(t, l.Allow(nil, "user-2"))
}

func TestAPIKeyCreateRateLimiter_GetRemaining(t *testing.T) {
	l := NewAPIKeyCreateRateLimiter(nil, APIKeyCreateRateLimiterConfig{
		MaxKeys: 3,
		Window:  1 * time.Hour,
	})

	assert.Equal(t, 3, l.GetRemaining(nil, "user-1"))

	l.Record(nil, "user-1")
	assert.Equal(t, 2, l.GetRemaining(nil, "user-1"))

	l.Record(nil, "user-1")
	l.Record(nil, "user-1")
	assert.Equal(t, 0, l.GetRemaining(nil, "user-1"))
}

func TestAPIKeyCreateRateLimiter_WindowExpiry(t *testing.T) {
	l := NewAPIKeyCreateRateLimiter(nil, APIKeyCreateRateLimiterConfig{
		MaxKeys: 2,
		Window:  50 * time.Millisecond,
	})

	l.Record(nil, "user-1")
	l.Record(nil, "user-1")
	assert.False(t, l.Allow(nil, "user-1"))

	time.Sleep(60 * time.Millisecond)
	assert.True(t, l.Allow(nil, "user-1"), "should allow after window expires")
}

func TestAPIKeyCreateRateLimiter_Middleware_AllowsNonPost(t *testing.T) {
	l := NewAPIKeyCreateRateLimiter(nil, DefaultAPIKeyCreateRateLimiterConfig())

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/api/v1/api-keys", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPIKeyCreateRateLimiter_Middleware_AllowsNonAPIKeyPath(t *testing.T) {
	l := NewAPIKeyCreateRateLimiter(nil, DefaultAPIKeyCreateRateLimiterConfig())

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/agents", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAPIKeyCreateRateLimiter_Middleware_BlocksAtLimit(t *testing.T) {
	l := NewAPIKeyCreateRateLimiter(nil, APIKeyCreateRateLimiterConfig{
		MaxKeys: 1,
		Window:  1 * time.Hour,
	})

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))

	ctx := auth.ContextWithClaims(context.Background(), &auth.Claims{UserID: "user-42"})
	req := httptest.NewRequest("POST", "/api/v1/api-keys", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	l.Record(context.Background(), "user-42")

	ctx = auth.ContextWithClaims(context.Background(), &auth.Claims{UserID: "user-42"})
	req = httptest.NewRequest("POST", "/api/v1/api-keys", nil)
	req = req.WithContext(ctx)
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
	assert.Equal(t, "1", w.Header().Get("X-RateLimit-Limit"))
}

func TestAPIKeyCreateRateLimiter_Middleware_NoClaimsPassesThrough(t *testing.T) {
	l := NewAPIKeyCreateRateLimiter(nil, DefaultAPIKeyCreateRateLimiterConfig())

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/api/v1/api-keys", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code, "no claims → pass through")
}

func TestIsAPIKeyCreatePath(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"/api/v1/api-keys", true},
		{"/api-keys", true},
		{"/api/v1/api-keys/abc123", false},
		{"/api/v1/api-keys/rotate", false},
		{"/api/v1/tasks", false},
		{"/api/v1/projects", false},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.expected, isAPIKeyCreatePath(tt.path))
		})
	}
}

func TestAPIKeyCreateRateLimiter_DefaultConfig(t *testing.T) {
	cfg := DefaultAPIKeyCreateRateLimiterConfig()
	assert.Equal(t, 3, cfg.MaxKeys)
	assert.Equal(t, 1*time.Hour, cfg.Window)
}

func TestLoginRateLimiter_AllowsWithinLimit(t *testing.T) {
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts:      5,
		Window:           1 * time.Minute,
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
		MaxAttempts:      3,
		Window:           1 * time.Minute,
		LockoutDurations: []time.Duration{1 * time.Minute},
	})

	ctx := context.Background()

	for i := 0; i < 3; i++ {
		l.RecordFailure(ctx, "10.0.0.1", "victim@example.com")
	}

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

	for i := 0; i < 2; i++ {
		l.RecordFailure(ctx, "10.0.0.2", "prog@example.com")
	}

	body := strings.NewReader(`{"email":"prog@example.com","password":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.2:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	time.Sleep(150 * time.Millisecond)

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
		MaxAttempts:      2,
		Window:           1 * time.Minute,
		LockoutDurations: []time.Duration{1 * time.Minute},
	})

	ctx := context.Background()

	l.RecordFailure(ctx, "10.0.0.1", "same@example.com")
	l.RecordFailure(ctx, "10.0.0.1", "same@example.com")

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(`{"email":"same@example.com","password":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	body = strings.NewReader(`{"email":"same@example.com","password":"wrong"}`)
	req = httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.99:1234"
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLoginRateLimiter_DifferentEmailsIndependent(t *testing.T) {
	l := NewLoginRateLimiter(nil, LoginRateLimiterConfig{
		MaxAttempts:      2,
		Window:           1 * time.Minute,
		LockoutDurations: []time.Duration{1 * time.Minute},
	})

	ctx := context.Background()

	l.RecordFailure(ctx, "10.0.0.1", "a@example.com")
	l.RecordFailure(ctx, "10.0.0.1", "a@example.com")

	handler := l.Middleware()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	body := strings.NewReader(`{"email":"a@example.com","password":"wrong"}`)
	req := httptest.NewRequest("POST", "/api/v1/auth/login", body)
	req.RemoteAddr = "10.0.0.1:1234"
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

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
		MaxAttempts:      1,
		Window:           1 * time.Minute,
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
		MaxAttempts:      2,
		Window:           1 * time.Minute,
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
		MaxAttempts:      1,
		Window:           1 * time.Minute,
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
		MaxAttempts:      2,
		Window:           50 * time.Millisecond,
		LockoutDurations: []time.Duration{100 * time.Millisecond},
	})

	ctx := context.Background()

	l.RecordFailure(ctx, "10.0.0.1", "window@test.com")
	l.RecordFailure(ctx, "10.0.0.1", "window@test.com")
	assert.True(t, l.IsLocked(ctx, "10.0.0.1", "window@test.com"))

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

func TestDefaultLimits_AllTiersPresent(t *testing.T) {
	limits := DefaultLimits()
	assert.Contains(t, limits, PlanFree)
	assert.Contains(t, limits, PlanPro)
	assert.Contains(t, limits, PlanTeam)
	assert.Contains(t, limits, PlanEnterprise)
}

func TestDefaultLimits_FreePlanValues(t *testing.T) {
	limits := DefaultLimits()
	free := limits[PlanFree]
	assert.Equal(t, 30, free.RequestsPerMinute)
	assert.Equal(t, 500, free.RequestsPerDay)
	assert.Equal(t, 10000, free.RequestsPerMonth)
	assert.Equal(t, 10, free.BurstSize)
	assert.Equal(t, 500000, free.TokensPerMonth)
	assert.Equal(t, 50, free.TasksPerMonth)
	assert.Equal(t, 1, free.MaxConcurrentTasks)
	assert.False(t, free.PriorityQueue)
	assert.Equal(t, "community", free.SupportSLA)
}

func TestDefaultLimits_ProPlanValues(t *testing.T) {
	limits := DefaultLimits()
	pro := limits[PlanPro]
	assert.Equal(t, 120, pro.RequestsPerMinute)
	assert.Equal(t, 5000, pro.RequestsPerDay)
	assert.True(t, pro.PriorityQueue)
	assert.Equal(t, "email_48h", pro.SupportSLA)
}

func TestDefaultLimits_EnterpriseUnlimited(t *testing.T) {
	limits := DefaultLimits()
	ent := limits[PlanEnterprise]
	assert.Equal(t, 0, ent.RequestsPerMonth, "0 means unlimited")
	assert.Equal(t, 0, ent.TokensPerMonth, "0 means unlimited")
	assert.Equal(t, 0, ent.TasksPerMonth, "0 means unlimited")
	assert.True(t, ent.PriorityQueue)
	assert.Equal(t, "dedicated", ent.SupportSLA)
}

func TestDefaultLimits_TeamPlanValues(t *testing.T) {
	limits := DefaultLimits()
	team := limits[PlanTeam]
	assert.Equal(t, 300, team.RequestsPerMinute)
	assert.Equal(t, 20000, team.RequestsPerDay)
	assert.Equal(t, 2000, team.TasksPerMonth)
	assert.Equal(t, "priority_24h", team.SupportSLA)
}

func TestCurrentPeriod_Format(t *testing.T) {
	period := CurrentPeriod()
	now := time.Now()
	expected := now.Format("2006-01")
	assert.Equal(t, expected, period)
}

func TestUsageTracker_UsageKey(t *testing.T) {
	ut := &UsageTracker{}
	key := ut.UsageKey("org-123", "tasks", "2026-01")
	assert.Equal(t, "usage:org-123:tasks:2026-01", key)
}

func TestNewUsageTracker(t *testing.T) {
	ut := NewUsageTracker(nil)
	assert.NotNil(t, ut)
}

func TestPlanAwareRateLimiter_GetLimits(t *testing.T) {
	parl := NewPlanAwareRateLimiter(nil)

	limits := parl.GetLimits(PlanFree)
	assert.Equal(t, 30, limits.RequestsPerMinute)

	limits = parl.GetLimits(PlanPro)
	assert.Equal(t, 120, limits.RequestsPerMinute)
}

func TestPlanAwareRateLimiter_SetLimits(t *testing.T) {
	parl := NewPlanAwareRateLimiter(nil)

	custom := PlanLimits{RequestsPerMinute: 999}
	parl.SetLimits(PlanFree, custom)

	limits := parl.GetLimits(PlanFree)
	assert.Equal(t, 999, limits.RequestsPerMinute)
}

func TestPlanAwareRateLimiter_GetLimits_FallbackToFree(t *testing.T) {
	parl := NewPlanAwareRateLimiter(nil)

	limits := parl.GetLimits("nonexistent_plan")
	free := parl.GetLimits(PlanFree)
	assert.Equal(t, free.RequestsPerMinute, limits.RequestsPerMinute)
}

func TestNewPlanAwareRateLimiter(t *testing.T) {
	parl := NewPlanAwareRateLimiter(nil)
	assert.NotNil(t, parl)
	assert.NotNil(t, parl.limits)
	assert.NotNil(t, parl.tracker)
}

func TestQuotaEnforcer_SetLimits(t *testing.T) {
	qe := NewQuotaEnforcer(nil)
	custom := PlanLimits{TasksPerMonth: 10}
	qe.SetLimits(PlanFree, custom)

	qe.mu.RLock()
	assert.Equal(t, 10, qe.limits[PlanFree].TasksPerMonth)
	qe.mu.RUnlock()
}

func TestNewQuotaEnforcer(t *testing.T) {
	qe := NewQuotaEnforcer(nil)
	assert.NotNil(t, qe)
	assert.NotNil(t, qe.tracker)
}

func TestPlanTier_Constants(t *testing.T) {
	assert.Equal(t, PlanTier("free"), PlanFree)
	assert.Equal(t, PlanTier("pro"), PlanPro)
	assert.Equal(t, PlanTier("team"), PlanTeam)
	assert.Equal(t, PlanTier("enterprise"), PlanEnterprise)
}

func TestPlanLimits_ZeroValuesAreUnlimited(t *testing.T) {
	limits := DefaultLimits()
	ent := limits[PlanEnterprise]
	assert.Equal(t, 0, ent.RequestsPerMonth)
	assert.Equal(t, 0, ent.TokensPerMonth)
	assert.Equal(t, 0, ent.TasksPerMonth)
	assert.Equal(t, 0, ent.ScansPerMonth)
	assert.Equal(t, 0.0, ent.MonthlyBudgetUsd)
}

// Content from ratelimit_race_test.go
func TestRateLimitHeadersMiddleware_ConcurrentAccess(t *testing.T) {
	rlm := NewRateLimitHeadersMiddleware(100, time.Minute)
	handler := rlm.Middleware(func(r *http.Request) string {
		return "concurrent-key"
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	var successCount int64
	var errorCount int64
	goroutines := 200

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code == http.StatusOK {
				atomic.AddInt64(&successCount, 1)
			} else {
				atomic.AddInt64(&errorCount, 1)
			}
		}()
	}
	wg.Wait()

	// With limit=100 and 200 goroutines, some should succeed, headers should not panic
	total := atomic.LoadInt64(&successCount) + atomic.LoadInt64(&errorCount)
	if total != int64(goroutines) {
		t.Errorf("expected %d total requests, got %d", goroutines, total)
	}
	// All should return 200 since RateLimitHeadersMiddleware doesn't reject, only adds headers
	if atomic.LoadInt64(&errorCount) > 0 {
		t.Errorf("expected no errors, got %d", atomic.LoadInt64(&errorCount))
	}
}

func TestRateLimitHeadersMiddleware_ConcurrentDecrementAccuracy(t *testing.T) {
	limit := 10
	rlm := NewRateLimitHeadersMiddleware(limit, time.Minute)
	handler := rlm.Middleware(func(r *http.Request) string {
		return "accuracy-key"
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	goroutines := 50

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
		}()
	}
	wg.Wait()

	// Final request to check remaining count
	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	limitHeader := w.Header().Get("X-RateLimit-Limit")
	remaining := w.Header().Get("X-RateLimit-Remaining")

	if limitHeader != "10" {
		t.Errorf("expected limit=10, got %q", limitHeader)
	}
	// remaining should be >= 0 (capped at 0 by the middleware)
	if remaining == "" {
		t.Error("expected X-RateLimit-Remaining header to be set")
	}
}

func TestRateLimitHeadersMiddleware_ConcurrentDifferentKeys(t *testing.T) {
	rlm := NewRateLimitHeadersMiddleware(5, time.Minute)
	handler := rlm.Middleware(func(r *http.Request) string {
		return r.Header.Get("X-Key")
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	// 10 keys, each hit 5 times with limit=5
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "h", "i", "j"}

	for _, key := range keys {
		for j := 0; j < 5; j++ {
			wg.Add(1)
			go func(k string) {
				defer wg.Done()
				req := httptest.NewRequest("GET", "/", nil)
				req.Header.Set("X-Key", k)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					t.Errorf("key=%s: expected 200, got %d", k, w.Code)
				}
			}(key)
		}
	}
	wg.Wait()

	// Each key should have remaining=0 after 5 hits with limit=5
	for _, key := range keys {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Key", key)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		remaining := w.Header().Get("X-RateLimit-Remaining")
		if remaining != "0" {
			t.Errorf("key=%s: expected remaining=0, got %q", key, remaining)
		}
	}
}

func TestAccountLockout_ConcurrentAccess(t *testing.T) {
	al := NewAccountLockout(5, 1*time.Minute)
	var wg sync.WaitGroup
	goroutines := 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx := t.Context()
			id := "user1"
			al.RecordFailure(ctx, id)
		}(i)
	}
	wg.Wait()

	// After 100 failures (well above limit of 5), user should be locked
	if !al.IsLocked(t.Context(), "user1") {
		t.Error("user should be locked after concurrent failures")
	}
}

func TestAccountLockout_ConcurrentMultipleUsers(t *testing.T) {
	al := NewAccountLockout(3, 1*time.Minute)
	var wg sync.WaitGroup
	users := []string{"alice", "bob", "charlie"}

	for _, user := range users {
		for i := 0; i < 3; i++ {
			wg.Add(1)
			go func(u string) {
				defer wg.Done()
				al.RecordFailure(t.Context(), u)
			}(user)
		}
	}
	wg.Wait()

	// All users should be locked
	for _, user := range users {
		if !al.IsLocked(t.Context(), user) {
			t.Errorf("user %s should be locked", user)
		}
	}

	// RecordSuccess for one user should unlock
	al.RecordSuccess(t.Context(), "alice")
	if al.IsLocked(t.Context(), "alice") {
		t.Error("alice should be unlocked after success")
	}
	// Others should still be locked
	if !al.IsLocked(t.Context(), "bob") {
		t.Error("bob should still be locked")
	}
}

func TestLockoutMiddleware_ConcurrentRequests(t *testing.T) {
	al := NewAccountLockout(5, 1*time.Minute)
	keyFunc := func(r *http.Request) string { return "user1" }

	handler := LockoutMiddleware(al, keyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	var wg sync.WaitGroup
	var blocked int64
	var allowed int64

	// First, lock the user
	for i := 0; i < 5; i++ {
		al.RecordFailure(t.Context(), "user1")
	}

	// Now make concurrent requests — all should be blocked
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)
			if w.Code == http.StatusTooManyRequests {
				atomic.AddInt64(&blocked, 1)
			} else {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	wg.Wait()

	if atomic.LoadInt64(&blocked) != 50 {
		t.Errorf("expected 50 blocked, got %d", atomic.LoadInt64(&blocked))
	}
	if atomic.LoadInt64(&allowed) != 0 {
		t.Errorf("expected 0 allowed, got %d", atomic.LoadInt64(&allowed))
	}
}

func TestCSRFMiddleware_ConcurrentTokenGeneration(t *testing.T) {
	secret := []byte("test-secret-key-for-csrf-32bytes!")
	m := NewCSRFMiddleware(secret)

	var wg sync.WaitGroup
	goroutines := 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Each goroutine generates and validates its own token
			rawToken := "test-token"
			validToken := rawToken + "." + m.signToken(rawToken)
			if !m.verifyToken(validToken) {
				t.Errorf("valid token should verify")
			}
		}()
	}
	wg.Wait()
}

func TestSanitizeMiddleware_ConcurrentPathTraversal(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	wrapped := SanitizeMiddleware(handler)

	var wg sync.WaitGroup
	goroutines := 100

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var req *http.Request
			if idx%2 == 0 {
				req = httptest.NewRequest("GET", "/api/normal", nil)
			} else {
				req = httptest.NewRequest("GET", "/api/%2e%2e%2fetc%2fpasswd", nil)
			}
			w := httptest.NewRecorder()
			wrapped.ServeHTTP(w, req)
		}(i)
	}
	wg.Wait()
}

func TestDetectSQLInjection_Concurrent(t *testing.T) {
	inputs := []struct {
		input    string
		expected bool
	}{
		{"SELECT * FROM users", true},
		{"hello world", false},
		{"'; DROP TABLE users;--", true},
		{"normal text", false},
	}

	var wg sync.WaitGroup
	for _, tt := range inputs {
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(input string, expected bool) {
				defer wg.Done()
				if got := DetectSQLInjection(input); got != expected {
					t.Errorf("DetectSQLInjection(%q) = %v, want %v", input, got, expected)
				}
			}(tt.input, tt.expected)
		}
	}
	wg.Wait()
}

func TestDetectXSS_Concurrent(t *testing.T) {
	inputs := []struct {
		input    string
		expected bool
	}{
		{"<script>alert(1)</script>", true},
		{"hello world", false},
		{"javascript:alert(1)", true},
		{"safe text", false},
	}

	var wg sync.WaitGroup
	for _, tt := range inputs {
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(input string, expected bool) {
				defer wg.Done()
				if got := DetectXSS(input); got != expected {
					t.Errorf("DetectXSS(%q) = %v, want %v", input, got, expected)
				}
			}(tt.input, tt.expected)
		}
	}
	wg.Wait()
}

func TestCompareTokens_Concurrent(t *testing.T) {
	token := "test-token-value"
	var wg sync.WaitGroup
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if !compareTokens(token, token) {
				t.Error("identical tokens should match")
			}
			if compareTokens(token, "wrong") {
				t.Error("different tokens should not match")
			}
		}()
	}
	wg.Wait()
}
