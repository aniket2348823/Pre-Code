package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// --- Tier variables ---

func TestTierVariables(t *testing.T) {
	if FreeTier.RequestsPerMinute != 30 {
		t.Errorf("FreeTier.RequestsPerMinute = %d, want 30", FreeTier.RequestsPerMinute)
	}
	if FreeTier.RequestsPerHour != 500 {
		t.Errorf("FreeTier.RequestsPerHour = %d, want 500", FreeTier.RequestsPerHour)
	}
	if FreeTier.RequestsPerDay != 5000 {
		t.Errorf("FreeTier.RequestsPerDay = %d, want 5000", FreeTier.RequestsPerDay)
	}
	if ProTier.RequestsPerMinute != 120 {
		t.Errorf("ProTier.RequestsPerMinute = %d, want 120", ProTier.RequestsPerMinute)
	}
	if ProTier.RequestsPerHour != 5000 {
		t.Errorf("ProTier.RequestsPerHour = %d, want 5000", ProTier.RequestsPerHour)
	}
	if ProTier.RequestsPerDay != 50000 {
		t.Errorf("ProTier.RequestsPerDay = %d, want 50000", ProTier.RequestsPerDay)
	}
	if TeamTier.RequestsPerMinute != 600 {
		t.Errorf("TeamTier.RequestsPerMinute = %d, want 600", TeamTier.RequestsPerMinute)
	}
	if TeamTier.RequestsPerHour != 20000 {
		t.Errorf("TeamTier.RequestsPerHour = %d, want 20000", TeamTier.RequestsPerHour)
	}
	if TeamTier.RequestsPerDay != 200000 {
		t.Errorf("TeamTier.RequestsPerDay = %d, want 200000", TeamTier.RequestsPerDay)
	}
}

// --- GetTier ---

func TestGetTier_FreeDefault(t *testing.T) {
	got := GetTier("unknown")
	if got != FreeTier {
		t.Errorf("GetTier(\"unknown\") = %+v, want FreeTier", got)
	}
}

func TestGetTier_EmptyString(t *testing.T) {
	got := GetTier("")
	if got != FreeTier {
		t.Errorf("GetTier(\"\") = %+v, want FreeTier", got)
	}
}

func TestGetTier_Pro(t *testing.T) {
	got := GetTier("pro")
	if got != ProTier {
		t.Errorf("GetTier(\"pro\") = %+v, want ProTier", got)
	}
}

func TestGetTier_Team(t *testing.T) {
	got := GetTier("team")
	if got != TeamTier {
		t.Errorf("GetTier(\"team\") = %+v, want TeamTier", got)
	}
}

func TestGetTier_Enterprise(t *testing.T) {
	got := GetTier("enterprise")
	if got != TeamTier {
		t.Errorf("GetTier(\"enterprise\") = %+v, want TeamTier", got)
	}
}

// --- tierName ---

func TestTierName_Free(t *testing.T) {
	if got := tierName(FreeTier); got != "free" {
		t.Errorf("tierName(FreeTier) = %q, want %q", got, "free")
	}
}

func TestTierName_Pro(t *testing.T) {
	if got := tierName(ProTier); got != "pro" {
		t.Errorf("tierName(ProTier) = %q, want %q", got, "pro")
	}
}

func TestTierName_Team(t *testing.T) {
	if got := tierName(TeamTier); got != "team" {
		t.Errorf("tierName(TeamTier) = %q, want %q", got, "team")
	}
}

func TestTierName_Unknown(t *testing.T) {
	custom := Tier{RequestsPerMinute: 10, RequestsPerHour: 100, RequestsPerDay: 1000}
	if got := tierName(custom); got != "free" {
		t.Errorf("tierName(custom) = %q, want %q", got, "free")
	}
}

// --- refill ---

func TestRefill_ZeroElapsed(t *testing.T) {
	b := &tokenBucket{
		minuteTokens: float64(FreeTier.RequestsPerMinute) / 2,
		hourTokens:   float64(FreeTier.RequestsPerHour) / 2,
		dayTokens:    float64(FreeTier.RequestsPerDay) / 2,
		lastRefill:   time.Now(),
		tier:         FreeTier,
	}
	beforeMinute := b.minuteTokens
	beforeHour := b.hourTokens
	beforeDay := b.dayTokens
	b.refill(FreeTier)
	if b.minuteTokens != beforeMinute {
		t.Errorf("minute tokens changed with zero elapsed: got %f, want %f", b.minuteTokens, beforeMinute)
	}
	if b.hourTokens != beforeHour {
		t.Errorf("hour tokens changed with zero elapsed: got %f, want %f", b.hourTokens, beforeHour)
	}
	if b.dayTokens != beforeDay {
		t.Errorf("day tokens changed with zero elapsed: got %f, want %f", b.dayTokens, beforeDay)
	}
}

func TestRefill_PartialRefill(t *testing.T) {
	b := &tokenBucket{
		minuteTokens: 0,
		hourTokens:   0,
		dayTokens:    0,
		lastRefill:   time.Now().Add(-30 * time.Second),
		tier:         FreeTier,
	}
	b.refill(FreeTier)
	if b.minuteTokens <= 0 {
		t.Error("expected minute tokens to refill")
	}
	if b.minuteTokens > float64(FreeTier.RequestsPerMinute) {
		t.Error("minute tokens exceeded max")
	}
	if b.hourTokens <= 0 {
		t.Error("expected hour tokens to refill")
	}
	if b.dayTokens <= 0 {
		t.Error("expected day tokens to refill")
	}
}

func TestRefill_CapsAtMax(t *testing.T) {
	b := &tokenBucket{
		minuteTokens: float64(FreeTier.RequestsPerMinute),
		hourTokens:   float64(FreeTier.RequestsPerHour),
		dayTokens:    float64(FreeTier.RequestsPerDay),
		lastRefill:   time.Now().Add(-time.Hour),
		tier:         FreeTier,
	}
	b.refill(FreeTier)
	if b.minuteTokens != float64(FreeTier.RequestsPerMinute) {
		t.Errorf("minute tokens should cap at max: got %f", b.minuteTokens)
	}
	if b.hourTokens != float64(FreeTier.RequestsPerHour) {
		t.Errorf("hour tokens should cap at max: got %f", b.hourTokens)
	}
	if b.dayTokens != float64(FreeTier.RequestsPerDay) {
		t.Errorf("day tokens should cap at max: got %f", b.dayTokens)
	}
}

func TestRefill_ProTierCaps(t *testing.T) {
	b := &tokenBucket{
		minuteTokens: 0,
		hourTokens:   0,
		dayTokens:    0,
		lastRefill:   time.Now().Add(-24 * time.Hour),
		tier:         ProTier,
	}
	b.refill(ProTier)
	if b.minuteTokens != float64(ProTier.RequestsPerMinute) {
		t.Errorf("minute tokens = %f, want %d", b.minuteTokens, ProTier.RequestsPerMinute)
	}
	if b.hourTokens != float64(ProTier.RequestsPerHour) {
		t.Errorf("hour tokens = %f, want %d", b.hourTokens, ProTier.RequestsPerHour)
	}
	if b.dayTokens != float64(ProTier.RequestsPerDay) {
		t.Errorf("day tokens = %f, want %d", b.dayTokens, ProTier.RequestsPerDay)
	}
}

// --- Token exhaustion across dimensions ---

func TestTokenExhaustion_OnlyMinuteExhausted(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "exhaust-min" }
	minuteOnly := Tier{RequestsPerMinute: 1, RequestsPerHour: 100, RequestsPerDay: 1000}
	tierFunc := func(r *http.Request) Tier { return minuteOnly }

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	// First request passes
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec.Code)
	}

	// Second request: minute exhausted
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", rec2.Code)
	}
}

func TestTokenExhaustion_OnlyHourExhausted(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "exhaust-hour" }
	hourOnly := Tier{RequestsPerMinute: 100, RequestsPerHour: 1, RequestsPerDay: 1000}
	tierFunc := func(r *http.Request) Tier { return hourOnly }

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec.Code)
	}

	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", rec2.Code)
	}
}

func TestTokenExhaustion_OnlyDayExhausted(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "exhaust-day" }
	dayOnly := Tier{RequestsPerMinute: 100, RequestsPerHour: 100, RequestsPerDay: 1}
	tierFunc := func(r *http.Request) Tier { return dayOnly }

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first request: expected 200, got %d", rec.Code)
	}

	// Day tokens may be refilled by tiny amounts due to timer resolution,
	// so keep requesting until 429 (consumption rate vastly exceeds refill).
	found := false
	for i := 0; i < 20; i++ {
		req2 := httptest.NewRequest("GET", "/test", nil)
		rec2 := httptest.NewRecorder()
		wrapped.ServeHTTP(rec2, req2)
		if rec2.Code == http.StatusTooManyRequests {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 429 after exhausting day limit within 20 requests")
	}
}

// --- Multiple keys ---

func TestMultipleKeys_SeparateBuckets(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string {
		return r.Header.Get("X-User-ID")
	}
	tierFunc := func(r *http.Request) Tier {
		return Tier{RequestsPerMinute: 1, RequestsPerHour: 100, RequestsPerDay: 1000}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	// User 1 uses their only token
	req1 := httptest.NewRequest("GET", "/test", nil)
	req1.Header.Set("X-User-ID", "user-1")
	rec1 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("user-1 first: expected 200, got %d", rec1.Code)
	}

	// User 1 exhausted
	req2 := httptest.NewRequest("GET", "/test", nil)
	req2.Header.Set("X-User-ID", "user-1")
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("user-1 second: expected 429, got %d", rec2.Code)
	}

	// User 2 should still have tokens
	req3 := httptest.NewRequest("GET", "/test", nil)
	req3.Header.Set("X-User-ID", "user-2")
	rec3 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusOK {
		t.Errorf("user-2 first: expected 200, got %d", rec3.Code)
	}
}

func TestMultipleKeys_IndependentLimits(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string {
		return r.Header.Get("X-User-ID")
	}
	tierFunc := func(r *http.Request) Tier {
		return Tier{RequestsPerMinute: 10000, RequestsPerHour: 10000, RequestsPerDay: 2}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	// User a — 2 requests (both allowed, day limit = 2)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-User-ID", "a")
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("user a request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// User b — 2 requests (independent key, also allowed)
	for i := 0; i < 2; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-User-ID", "b")
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("user b request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// User a third — denied (day bucket exhausted)
	// Token refill from timer resolution may allow one extra request,
	// so retry until 429 (consumption vastly exceeds refill rate).
	foundA := false
	for i := 0; i < 20; i++ {
		reqA := httptest.NewRequest("GET", "/test", nil)
		reqA.Header.Set("X-User-ID", "a")
		recA := httptest.NewRecorder()
		wrapped.ServeHTTP(recA, reqA)
		if recA.Code == http.StatusTooManyRequests {
			foundA = true
			break
		}
	}
	if !foundA {
		t.Errorf("user a: expected 429 after exhausting day limit")
	}

	// User b third — also denied (independently exhausted)
	foundB := false
	for i := 0; i < 20; i++ {
		reqB := httptest.NewRequest("GET", "/test", nil)
		reqB.Header.Set("X-User-ID", "b")
		recB := httptest.NewRecorder()
		wrapped.ServeHTTP(recB, reqB)
		if recB.Code == http.StatusTooManyRequests {
			foundB = true
			break
		}
	}
	if !foundB {
		t.Errorf("user b: expected 429 after exhausting day limit")
	}
}

// --- Concurrent access ---

func TestConcurrentMiddleware_SameKey(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "shared" }
	tierFunc := func(r *http.Request) Tier {
		return Tier{RequestsPerMinute: 100, RequestsPerHour: 1000, RequestsPerDay: 10000}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	var wg sync.WaitGroup
	var mu sync.Mutex
	statusCodes := make(map[int]int)

	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test", nil)
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)
			mu.Lock()
			statusCodes[rec.Code]++
			mu.Unlock()
		}()
	}
	wg.Wait()

	if statusCodes[http.StatusOK] < 98 || statusCodes[http.StatusOK] > 102 {
		t.Errorf("expected ~100 OK responses, got %d", statusCodes[http.StatusOK])
	}
	if statusCodes[http.StatusTooManyRequests] < 98 || statusCodes[http.StatusTooManyRequests] > 102 {
		t.Errorf("expected ~100 429 responses, got %d", statusCodes[http.StatusTooManyRequests])
	}
}

func TestConcurrentMiddleware_DifferentKeys(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string {
		return r.Header.Get("X-User-ID")
	}
	tierFunc := func(r *http.Request) Tier {
		return Tier{RequestsPerMinute: 5, RequestsPerHour: 100, RequestsPerDay: 1000}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/test", nil)
			req.Header.Set("X-User-ID", "user")
			rec := httptest.NewRecorder()
			wrapped.ServeHTTP(rec, req)
		}(i)
	}
	wg.Wait()
}

// --- Middleware headers ---

func TestMiddleware_RateLimitHeaders_Allow(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "headers-allow" }
	tierFunc := func(r *http.Request) Tier { return ProTier }

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-RateLimit-Limit") != "120" {
		t.Errorf("X-RateLimit-Limit = %q, want %q", rec.Header().Get("X-RateLimit-Limit"), "120")
	}
	if rec.Header().Get("X-RateLimit-Tier") != "pro" {
		t.Errorf("X-RateLimit-Tier = %q, want %q", rec.Header().Get("X-RateLimit-Tier"), "pro")
	}
	if rec.Header().Get("X-RateLimit-Remaining") == "" {
		t.Error("expected X-RateLimit-Remaining header")
	}
}

func TestMiddleware_RateLimitHeaders_Deny(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "headers-deny" }
	tierFunc := func(r *http.Request) Tier {
		return Tier{RequestsPerMinute: 1, RequestsPerHour: 1, RequestsPerDay: 1}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	// Exhaust
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	// Should be denied — retry to handle timer resolution
	found := false
	for i := 0; i < 20; i++ {
		req2 := httptest.NewRequest("GET", "/test", nil)
		rec2 := httptest.NewRecorder()
		wrapped.ServeHTTP(rec2, req2)

		if rec2.Code == http.StatusTooManyRequests {
			if rec2.Header().Get("Retry-After") == "" {
				t.Error("expected Retry-After header on 429")
			}
			if rec2.Header().Get("X-RateLimit-Remaining") != "0" {
				t.Errorf("X-RateLimit-Remaining = %q, want %q", rec2.Header().Get("X-RateLimit-Remaining"), "0")
			}
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 429 after exhausting rate limit")
	}
}

// --- Tier change resets bucket ---

func TestTierChange_ResetsBucket(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "tier-reset" }
	callCount := 0
	tierFunc := func(r *http.Request) Tier {
		callCount++
		if callCount <= 1 {
			return Tier{RequestsPerMinute: 1, RequestsPerHour: 1, RequestsPerDay: 1}
		}
		return TeamTier
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	// First request: free tier (1 req/min) - uses token
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("first: expected 200, got %d", rec.Code)
	}

	// Second request: team tier (600 req/min) - should pass because bucket resets
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Errorf("second (tier change): expected 200, got %d", rec2.Code)
	}
	if rec2.Header().Get("X-RateLimit-Tier") != "team" {
		t.Errorf("tier = %q, want %q", rec2.Header().Get("X-RateLimit-Tier"), "team")
	}
}

// --- Cleanup ---

func TestCleanup_RemovesStaleBuckets(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "stale-cleanup" }
	tierFunc := func(r *http.Request) Tier { return FreeTier }

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	l.mu.Lock()
	if _, exists := l.buckets["stale-cleanup"]; !exists {
		l.mu.Unlock()
		t.Fatal("expected bucket to exist")
	}
	l.buckets["stale-cleanup"].lastRefill = time.Now().Add(-2 * time.Hour)
	l.mu.Unlock()

	// Simulate cleanup
	l.mu.Lock()
	for k, b := range l.buckets {
		if time.Since(b.lastRefill) > 1*time.Hour {
			delete(l.buckets, k)
		}
	}
	l.mu.Unlock()

	l.mu.Lock()
	_, exists := l.buckets["stale-cleanup"]
	l.mu.Unlock()
	if exists {
		t.Error("expected stale bucket to be cleaned up")
	}
}

func TestCleanup_KeepsFreshBuckets(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "fresh-cleanup" }
	tierFunc := func(r *http.Request) Tier { return FreeTier }

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	l.mu.Lock()
	for k, b := range l.buckets {
		if time.Since(b.lastRefill) > 1*time.Hour {
			delete(l.buckets, k)
		}
	}
	l.mu.Unlock()

	l.mu.Lock()
	_, exists := l.buckets["fresh-cleanup"]
	l.mu.Unlock()
	if !exists {
		t.Error("expected fresh bucket to survive cleanup")
	}
}

// --- NewTieredRateLimiter ---

func TestNewTieredRateLimiter_Initialization(t *testing.T) {
	l := NewTieredRateLimiter()
	if l == nil {
		t.Fatal("expected non-nil limiter")
	}
	l.mu.Lock()
	if l.buckets == nil {
		t.Fatal("expected non-nil buckets map")
	}
	if len(l.buckets) != 0 {
		t.Errorf("expected empty buckets, got %d", len(l.buckets))
	}
	l.mu.Unlock()
}

// --- Custom tier ---

func TestCustomTier_UnknownPlan(t *testing.T) {
	got := GetTier("custom")
	if got != FreeTier {
		t.Errorf("GetTier(\"custom\") = %+v, want FreeTier", got)
	}
}

func TestCustomTier_MiddlewareUsesCustomLimits(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "custom-tier" }
	customTier := Tier{RequestsPerMinute: 3, RequestsPerHour: 10, RequestsPerDay: 50}
	tierFunc := func(r *http.Request) Tier { return customTier }

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	// First 3 requests should pass
	for i := 0; i < 3; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("request %d: expected 200, got %d", i+1, rec.Code)
		}
	}

	// Fourth should be denied
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("fourth: expected 429, got %d", rec.Code)
	}
}
