package middleware

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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
