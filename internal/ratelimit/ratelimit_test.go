package ratelimit

import (
	"sync"
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
	// Different key should still work
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
