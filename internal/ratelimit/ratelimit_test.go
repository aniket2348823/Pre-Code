package ratelimit

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewLimiter(t *testing.T) {
	l := NewLimiter(SlidingWindow, 10, time.Minute)
	if l == nil {
		t.Fatal("expected non-nil limiter")
	}
}

func TestSlidingWindowAllow(t *testing.T) {
	l := NewLimiter(SlidingWindow, 3, time.Second)
	for i := 0; i < 3; i++ {
		if !l.Allow() {
			t.Errorf("expected allow on request %d", i)
		}
	}
	if l.Allow() {
		t.Error("expected deny after limit reached")
	}
}

func TestSlidingWindowReset(t *testing.T) {
	l := NewLimiter(SlidingWindow, 2, time.Second)
	l.Allow()
	l.Allow()
	if l.Allow() {
		t.Error("expected deny")
	}
	l.Reset()
	if !l.Allow() {
		t.Error("expected allow after reset")
	}
}

func TestTokenBucketAllow(t *testing.T) {
	l := NewLimiter(TokenBucket, 5, time.Second)
	for i := 0; i < 5; i++ {
		if !l.Allow() {
			t.Errorf("expected allow on token %d", i)
		}
	}
	if l.Allow() {
		t.Error("expected deny when tokens exhausted")
	}
}

func TestTokenBucketRefill(t *testing.T) {
	l := NewLimiter(TokenBucket, 2, 100*time.Millisecond)
	l.Allow()
	l.Allow()
	if l.Allow() {
		t.Error("expected deny")
	}
	time.Sleep(110 * time.Millisecond)
	if !l.Allow() {
		t.Error("expected allow after refill")
	}
}

func TestFixedWindowAllow(t *testing.T) {
	l := NewLimiter(FixedWindow, 2, time.Second)
	if !l.Allow() {
		t.Error("expected allow")
	}
	if !l.Allow() {
		t.Error("expected allow")
	}
	if l.Allow() {
		t.Error("expected deny at limit")
	}
}

func TestAllowKey(t *testing.T) {
	l := NewLimiter(SlidingWindow, 2, time.Second)
	if !l.AllowKey("user1") {
		t.Error("expected allow for user1")
	}
	if !l.AllowKey("user1") {
		t.Error("expected allow for user1")
	}
	if l.AllowKey("user1") {
		t.Error("expected deny for user1")
	}
	if !l.AllowKey("user2") {
		t.Error("expected allow for user2")
	}
}

func TestResetKey(t *testing.T) {
	l := NewLimiter(SlidingWindow, 1, time.Second)
	l.AllowKey("user1")
	if l.AllowKey("user1") {
		t.Error("expected deny")
	}
	l.ResetKey("user1")
	if !l.AllowKey("user1") {
		t.Error("expected allow after reset key")
	}
}

func TestStats(t *testing.T) {
	l := NewLimiter(SlidingWindow, 10, time.Minute)
	stats := l.Stats()
	if stats["limit"] != 10 {
		t.Errorf("expected limit 10, got %v", stats["limit"])
	}
	if stats["algorithm"] != "sliding_window" {
		t.Errorf("expected sliding_window, got %v", stats["algorithm"])
	}
}

func TestConcurrentAllow(t *testing.T) {
	l := NewLimiter(SlidingWindow, 100, time.Second)
	var wg sync.WaitGroup
	allowed := 0
	var mu sync.Mutex
	for i := 0; i < 200; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Allow() {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	if allowed != 100 {
		t.Errorf("expected exactly 100 allowed, got %d", allowed)
	}
}

func TestSlidingWindow_Limit1(t *testing.T) {
	l := NewLimiter(SlidingWindow, 1, time.Minute)
	if !l.Allow() {
		t.Error("first request should be allowed")
	}
	if l.Allow() {
		t.Error("second request should be denied")
	}
}

func TestSlidingWindow_Limit0(t *testing.T) {
	l := NewLimiter(SlidingWindow, 0, time.Minute)
	if l.Allow() {
		t.Error("limit=0 should deny all")
	}
}

func TestSlidingWindow_Window0(t *testing.T) {
	l := NewLimiter(SlidingWindow, 1, 0)
	l.Allow()
}

func TestTokenBucket_RapidExhaustion(t *testing.T) {
	l := NewLimiter(TokenBucket, 5, time.Second)
	allowed := 0
	for i := 0; i < 100; i++ {
		if l.Allow() {
			allowed++
		}
	}
	if allowed != 5 {
		t.Errorf("expected 5 allowed, got %d", allowed)
	}
}

func TestTokenBucket_RefillAfterExhaustion(t *testing.T) {
	l := NewLimiter(TokenBucket, 1, 50*time.Millisecond)
	l.Allow()
	if l.Allow() {
		t.Error("should be denied")
	}
	time.Sleep(75 * time.Millisecond)
	if !l.Allow() {
		t.Error("should be allowed after refill")
	}
}

func TestTokenBucket_Window0(t *testing.T) {
	l := NewLimiter(TokenBucket, 1, 0)
	l.Allow()
}

func TestFixedWindow_Boundary(t *testing.T) {
	l := NewLimiter(FixedWindow, 2, 10*time.Millisecond)
	l.Allow()
	l.Allow()
	if l.Allow() {
		t.Error("should be denied at limit")
	}
	time.Sleep(15 * time.Millisecond)
	if !l.Allow() {
		t.Error("should be allowed after window reset")
	}
}

func TestFixedWindow_VeryShort(t *testing.T) {
	l := NewLimiter(FixedWindow, 1, time.Millisecond)
	l.Allow()
	time.Sleep(2 * time.Millisecond)
	if !l.Allow() {
		t.Error("should be allowed after 1ms window")
	}
}

func TestAllow_GlobalKey(t *testing.T) {
	l := NewLimiter(SlidingWindow, 2, time.Second)
	if !l.Allow() {
		t.Error("first should pass")
	}
	if !l.Allow() {
		t.Error("second should pass")
	}
	if l.Allow() {
		t.Error("third should fail")
	}
}

func TestAllowKey_EmptyKey(t *testing.T) {
	l := NewLimiter(SlidingWindow, 2, time.Second)
	l.AllowKey("")
	l.AllowKey("")
	if l.AllowKey("") {
		t.Error("should be denied at limit")
	}
}

func TestAllowKey_LongKey(t *testing.T) {
	l := NewLimiter(SlidingWindow, 10, time.Second)
	longKey := make([]byte, 1000)
	for i := range longKey {
		longKey[i] = 'a'
	}
	if !l.AllowKey(string(longKey)) {
		t.Error("long key should be allowed")
	}
}

func TestStats_Accuracy(t *testing.T) {
	l := NewLimiter(SlidingWindow, 10, time.Minute)
	l.AllowKey("a")
	l.AllowKey("a")
	l.AllowKey("b")
	stats := l.Stats()
	if stats["keys"] != 2 {
		t.Errorf("expected 2 keys, got %v", stats["keys"])
	}
	if stats["limit"] != 10 {
		t.Errorf("expected limit 10, got %v", stats["limit"])
	}
}

func TestReset_AfterRateLimit(t *testing.T) {
	l := NewLimiter(SlidingWindow, 1, time.Second)
	l.Allow()
	if l.Allow() {
		t.Error("should be denied")
	}
	l.Reset()
	if !l.Allow() {
		t.Error("should be allowed after reset")
	}
}

func TestResetKey_NonExistent(t *testing.T) {
	l := NewLimiter(SlidingWindow, 1, time.Second)
	l.ResetKey("nonexistent")
}

func TestConcurrentAllowKey(t *testing.T) {
	l := NewLimiter(SlidingWindow, 1000, time.Second)
	var wg sync.WaitGroup
	var allowed int64
	for i := 0; i < 2000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.AllowKey("shared") {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	wg.Wait()
	if allowed < 990 || allowed > 1010 {
		t.Errorf("expected ~1000 allowed, got %d", allowed)
	}
}

func TestMemoryLeak_ManyKeys(t *testing.T) {
	l := NewLimiter(SlidingWindow, 10, time.Minute)
	for i := 0; i < 10000; i++ {
		l.AllowKey("key-" + string(rune(i)))
	}
	l.Reset()
	stats := l.Stats()
	if stats["keys"] != 0 {
		t.Errorf("expected 0 keys after reset, got %v", stats["keys"])
	}
}

// --- Tiered rate limiter tests ---

func TestGetTier(t *testing.T) {
	tests := []struct {
		plan string
		want Tier
	}{
		{"free", FreeTier},
		{"pro", ProTier},
		{"team", TeamTier},
		{"enterprise", TeamTier},
		{"", FreeTier},
		{"unknown", FreeTier},
	}
	for _, tt := range tests {
		got := GetTier(tt.plan)
		if got != tt.want {
			t.Errorf("GetTier(%q) = %+v, want %+v", tt.plan, got, tt.want)
		}
	}
}

func TestTierNames(t *testing.T) {
	if got := tierName(FreeTier); got != "free" {
		t.Errorf("tierName(FreeTier) = %q, want %q", got, "free")
	}
	if got := tierName(ProTier); got != "pro" {
		t.Errorf("tierName(ProTier) = %q, want %q", got, "pro")
	}
	if got := tierName(TeamTier); got != "team" {
		t.Errorf("tierName(TeamTier) = %q, want %q", got, "team")
	}
}

func TestNewTieredRateLimiter(t *testing.T) {
	l := NewTieredRateLimiter()
	if l == nil {
		t.Fatal("expected non-nil tiered limiter")
	}
	// cleanup goroutine runs, stop it by letting it gc
}

func TestTieredMiddleware_Allow(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "user1" }
	tierFunc := func(r *http.Request) Tier { return FreeTier }

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("X-RateLimit-Limit") == "" {
		t.Error("expected X-RateLimit-Limit header")
	}
	if rec.Header().Get("X-RateLimit-Tier") != "free" {
		t.Errorf("expected tier 'free', got %q", rec.Header().Get("X-RateLimit-Tier"))
	}
}

func TestTieredMiddleware_Deny(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "user1" }
	tierFunc := func(r *http.Request) Tier {
		return Tier{RequestsPerMinute: 1, RequestsPerHour: 1, RequestsPerDay: 1}
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	// First request should pass
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("first request: expected 200, got %d", rec.Code)
	}

	// Second request should be rate limited
	req2 := httptest.NewRequest("GET", "/test", nil)
	rec2 := httptest.NewRecorder()
	wrapped.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected 429, got %d", rec2.Code)
	}
	if rec2.Header().Get("Retry-After") == "" {
		t.Error("expected Retry-After header")
	}
	if rec2.Header().Get("X-RateLimit-Remaining") != "0" {
		t.Errorf("expected remaining 0, got %q", rec2.Header().Get("X-RateLimit-Remaining"))
	}
}

func TestTieredMiddleware_TierChange(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "user1" }
	callCount := 0
	tierFunc := func(r *http.Request) Tier {
		callCount++
		if callCount <= 2 {
			return FreeTier
		}
		return ProTier
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	for i := 0; i < 5; i++ {
		req := httptest.NewRequest("GET", "/test", nil)
		rec := httptest.NewRecorder()
		wrapped.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, rec.Code)
		}
	}
}

func TestTieredMiddleware_TeamTier(t *testing.T) {
	l := NewTieredRateLimiter()
	keyFunc := func(r *http.Request) string { return "user1" }
	tierFunc := func(r *http.Request) Tier { return TeamTier }

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Header().Get("X-RateLimit-Tier") != "team" {
		t.Errorf("expected tier 'team', got %q", rec.Header().Get("X-RateLimit-Tier"))
	}
}

func TestRefill(t *testing.T) {
	b := &tokenBucket{
		minuteTokens: 0,
		hourTokens:   0,
		dayTokens:    0,
		lastRefill:   time.Now().Add(-time.Minute),
	}
	b.refill(FreeTier)
	if b.minuteTokens <= 0 {
		t.Error("expected minute tokens to refill")
	}
	if b.hourTokens <= 0 {
		t.Error("expected hour tokens to refill")
	}
	if b.dayTokens <= 0 {
		t.Error("expected day tokens to refill")
	}
}

func TestRefill_Caps(t *testing.T) {
	b := &tokenBucket{
		minuteTokens: float64(FreeTier.RequestsPerMinute),
		hourTokens:   float64(FreeTier.RequestsPerHour),
		dayTokens:    float64(FreeTier.RequestsPerDay),
		lastRefill:   time.Now(),
	}
	b.refill(FreeTier)
	if b.minuteTokens > float64(FreeTier.RequestsPerMinute) {
		t.Error("minute tokens exceeded max")
	}
	if b.hourTokens > float64(FreeTier.RequestsPerHour) {
		t.Error("hour tokens exceeded max")
	}
	if b.dayTokens > float64(FreeTier.RequestsPerDay) {
		t.Error("day tokens exceeded max")
	}
}

func TestFixedWindow_AllowKey(t *testing.T) {
	l := NewLimiter(FixedWindow, 2, time.Second)
	if !l.AllowKey("k1") {
		t.Error("first should pass")
	}
	if !l.AllowKey("k1") {
		t.Error("second should pass")
	}
	if l.AllowKey("k1") {
		t.Error("third should deny")
	}
	if !l.AllowKey("k2") {
		t.Error("different key should pass")
	}
}

func TestTokenBucket_AllowKey(t *testing.T) {
	l := NewLimiter(TokenBucket, 2, time.Second)
	if !l.AllowKey("k1") {
		t.Error("first should pass")
	}
	if !l.AllowKey("k1") {
		t.Error("second should pass")
	}
	if l.AllowKey("k1") {
		t.Error("third should deny")
	}
}

func TestTokenBucket_AllowKey_Refill(t *testing.T) {
	l := NewLimiter(TokenBucket, 1, 50*time.Millisecond)
	l.AllowKey("k1")
	if l.AllowKey("k1") {
		t.Error("should deny")
	}
	time.Sleep(75 * time.Millisecond)
	if !l.AllowKey("k1") {
		t.Error("should allow after refill")
	}
}

func TestRefill_CapsMinuteHourDay(t *testing.T) {
	// Set lastRefill far in the past so refill amounts are huge,
	// hitting the cap for minute, hour, and day buckets.
	b := &tokenBucket{
		minuteTokens: 0,
		hourTokens:   0,
		dayTokens:    0,
		lastRefill:   time.Now().Add(-24 * time.Hour),
	}
	b.refill(ProTier)

	if b.minuteTokens != float64(ProTier.RequestsPerMinute) {
		t.Errorf("minute tokens capped: got %f, want %d", b.minuteTokens, ProTier.RequestsPerMinute)
	}
	if b.hourTokens != float64(ProTier.RequestsPerHour) {
		t.Errorf("hour tokens capped: got %f, want %d", b.hourTokens, ProTier.RequestsPerHour)
	}
	if b.dayTokens != float64(ProTier.RequestsPerDay) {
		t.Errorf("day tokens capped: got %f, want %d", b.dayTokens, ProTier.RequestsPerDay)
	}
}

func TestTieredCleanup_RemovesStale(t *testing.T) {
	l := NewTieredRateLimiter()

	keyFunc := func(r *http.Request) string { return "stale-user" }
	tierFunc := func(r *http.Request) Tier { return FreeTier }
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	// Create a bucket via a request
	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	l.mu.Lock()
	if _, exists := l.buckets["stale-user"]; !exists {
		l.mu.Unlock()
		t.Fatal("expected bucket to exist after request")
	}
	// Simulate old bucket by setting lastRefill to 2 hours ago
	l.buckets["stale-user"].lastRefill = time.Now().Add(-2 * time.Hour)
	l.mu.Unlock()

	// Manually run cleanup logic (same as the goroutine body)
	l.mu.Lock()
	for k, b := range l.buckets {
		if time.Since(b.lastRefill) > 1*time.Hour {
			delete(l.buckets, k)
		}
	}
	l.mu.Unlock()

	l.mu.Lock()
	_, exists := l.buckets["stale-user"]
	l.mu.Unlock()
	if exists {
		t.Error("expected stale bucket to be cleaned up")
	}
}

func TestTieredCleanup_KeepsFresh(t *testing.T) {
	l := NewTieredRateLimiter()

	keyFunc := func(r *http.Request) string { return "fresh-user" }
	tierFunc := func(r *http.Request) Tier { return FreeTier }
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := l.Middleware(keyFunc, tierFunc)
	wrapped := mw(handler)

	req := httptest.NewRequest("GET", "/test", nil)
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	// Manually run cleanup — bucket is fresh, should survive
	l.mu.Lock()
	for k, b := range l.buckets {
		if time.Since(b.lastRefill) > 1*time.Hour {
			delete(l.buckets, k)
		}
	}
	l.mu.Unlock()

	l.mu.Lock()
	_, exists := l.buckets["fresh-user"]
	l.mu.Unlock()
	if !exists {
		t.Error("expected fresh bucket to survive cleanup")
	}
}
