package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/auth"
)

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

	// user-1 blocked
	assert.False(t, l.Allow(nil, "user-1"))

	// user-2 still allowed
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

	// First request — should pass
	ctx := auth.ContextWithClaims(context.Background(), &auth.Claims{UserID: "user-42"})
	req := httptest.NewRequest("POST", "/api/v1/api-keys", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Record this creation
	l.Record(context.Background(), "user-42")

	// Second request — should be blocked
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
