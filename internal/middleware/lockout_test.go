package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewAccountLockout(t *testing.T) {
	al := NewAccountLockout(3, 5*time.Minute)
	assert.NotNil(t, al)
	assert.Equal(t, 3, al.maxAttempts)
	assert.Equal(t, 5*time.Minute, al.lockoutDuration)
}

func TestAccountLockout_IsLocked_NotLocked(t *testing.T) {
	al := NewAccountLockout(3, 5*time.Minute)
	assert.False(t, al.IsLocked(context.Background(), "user1"))
}

func TestAccountLockout_LocksAfterMaxAttempts(t *testing.T) {
	al := NewAccountLockout(3, 5*time.Minute)
	ctx := context.Background()

	al.RecordFailure(ctx, "user1")
	al.RecordFailure(ctx, "user1")
	assert.False(t, al.IsLocked(ctx, "user1"), "should not be locked after 2 failures")

	al.RecordFailure(ctx, "user1")
	assert.True(t, al.IsLocked(ctx, "user1"), "should be locked after 3 failures")
}

func TestAccountLockout_UnlocksAfterDuration(t *testing.T) {
	al := NewAccountLockout(2, 1*time.Millisecond)
	ctx := context.Background()

	al.RecordFailure(ctx, "user1")
	al.RecordFailure(ctx, "user1")
	assert.True(t, al.IsLocked(ctx, "user1"))

	time.Sleep(5 * time.Millisecond)
	assert.False(t, al.IsLocked(ctx, "user1"), "should be unlocked after duration")
}

func TestAccountLockout_RecordSuccess_ClearsFailures(t *testing.T) {
	al := NewAccountLockout(3, 5*time.Minute)
	ctx := context.Background()

	al.RecordFailure(ctx, "user1")
	al.RecordFailure(ctx, "user1")
	al.RecordSuccess(ctx, "user1")
	assert.False(t, al.IsLocked(ctx, "user1"))

	al.RecordFailure(ctx, "user1")
	assert.False(t, al.IsLocked(ctx, "user1"), "should start fresh after success")
}

func TestAccountLockout_GetRemainingLockout(t *testing.T) {
	al := NewAccountLockout(1, 10*time.Minute)
	ctx := context.Background()

	assert.Equal(t, time.Duration(0), al.GetRemainingLockout(ctx, "user1"))

	al.RecordFailure(ctx, "user1")
	remaining := al.GetRemainingLockout(ctx, "user1")
	assert.InDelta(t, 10*time.Minute, remaining, float64(time.Second))
}

func TestAccountLockout_GetRemainingLockout_NoLock(t *testing.T) {
	al := NewAccountLockout(3, 5*time.Minute)
	remaining := al.GetRemainingLockout(context.Background(), "nonexistent")
	assert.Equal(t, time.Duration(0), remaining)
}

func TestAccountLockout_IndependentUsers(t *testing.T) {
	al := NewAccountLockout(2, 5*time.Minute)
	ctx := context.Background()

	al.RecordFailure(ctx, "user1")
	al.RecordFailure(ctx, "user1")
	al.RecordFailure(ctx, "user2")

	assert.True(t, al.IsLocked(ctx, "user1"))
	assert.False(t, al.IsLocked(ctx, "user2"))
}

func TestAccountLockout_Cleanup(t *testing.T) {
	al := NewAccountLockout(1, 1*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	al.RecordFailure(ctx, "user1")
	assert.True(t, al.IsLocked(ctx, "user1"))

	go al.Cleanup(ctx, 1*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	cancel()
	time.Sleep(5 * time.Millisecond)

	al.mu.Lock()
	_, exists := al.attempts["user1"]
	al.mu.Unlock()
	assert.False(t, exists, "cleanup should remove expired entries")
}

// --- LockoutMiddleware Tests ---

func TestLockoutMiddleware_AllowsNonLocked(t *testing.T) {
	al := NewAccountLockout(3, 5*time.Minute)
	keyFunc := func(r *http.Request) string { return "user1" }

	handler := LockoutMiddleware(al, keyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestLockoutMiddleware_BlocksLockedAccount(t *testing.T) {
	al := NewAccountLockout(2, 10*time.Minute)
	keyFunc := func(r *http.Request) string { return "user1" }
	ctx := context.Background()

	al.RecordFailure(ctx, "user1")
	al.RecordFailure(ctx, "user1")

	handler := LockoutMiddleware(al, keyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	assert.NotEmpty(t, w.Header().Get("Retry-After"))
}

func TestLockoutMiddleware_SetsRetryAfterHeader(t *testing.T) {
	al := NewAccountLockout(1, 5*time.Minute)
	keyFunc := func(r *http.Request) string { return "user1" }
	al.RecordFailure(context.Background(), "user1")

	handler := LockoutMiddleware(al, keyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusTooManyRequests, w.Code)
	retryAfter := w.Header().Get("Retry-After")
	assert.NotEmpty(t, retryAfter)

	var body map[string]interface{}
	json.NewDecoder(w.Body).Decode(&body)
	assert.Equal(t, "AUTH_005", body["code"])
}

func TestLockoutMiddleware_NextHandlerNotCalledWhenLocked(t *testing.T) {
	al := NewAccountLockout(1, 5*time.Minute)
	keyFunc := func(r *http.Request) string { return "user1" }
	al.RecordFailure(context.Background(), "user1")

	nextCalled := false
	handler := LockoutMiddleware(al, keyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	req := httptest.NewRequest("DELETE", "/", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	assert.False(t, nextCalled, "next handler should not be called when locked")
}

func TestLockoutMiddleware_DifferentKeys(t *testing.T) {
	al := NewAccountLockout(1, 5*time.Minute)
	keyFunc := func(r *http.Request) string {
		return r.Header.Get("X-User-ID")
	}
	al.RecordFailure(context.Background(), "locked-user")

	handler := LockoutMiddleware(al, keyFunc)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", "locked-user")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusTooManyRequests, w.Code)

	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("X-User-ID", "good-user")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// --- NewLockout factory ---

func TestNewLockout_ReturnsInMemoryWhenNoRedis(t *testing.T) {
	l := NewLockout(nil, 5, 10*time.Minute)
	_, ok := l.(*AccountLockout)
	assert.True(t, ok, "should return in-memory lockout when redis is nil")
}

func TestLockoutInterface(t *testing.T) {
	var _ Lockout = &AccountLockout{}
	var _ Lockout = (*AccountLockout)(nil)
}
