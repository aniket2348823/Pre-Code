package ratelimit

import (
	"sync"
	"testing"
	"time"
)

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
	// Should not panic
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
	// Should not panic
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
	// Should not panic
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
				allowed++
			}
		}()
	}
	wg.Wait()
	if allowed != 1000 {
		t.Errorf("expected exactly 1000 allowed, got %d", allowed)
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
