package llm

import (
	"sync"
	"testing"
	"time"
)

func TestCacheKey_StableAndSensitive(t *testing.T) {
	base := &ChatRequest{
		Model:    "gpt-4o",
		System:   "you are helpful",
		Messages: []Message{{Role: "user", Content: "hello"}},
	}
	// Same input → same key.
	if CacheKey(base) != CacheKey(base) {
		t.Fatal("cache key not stable for identical requests")
	}
	// Different model → different key.
	other := *base
	other.Model = "claude-opus-4"
	if CacheKey(base) == CacheKey(&other) {
		t.Fatal("cache key must change when model changes")
	}
	// Different message content → different key.
	other2 := *base
	other2.Messages = []Message{{Role: "user", Content: "goodbye"}}
	if CacheKey(base) == CacheKey(&other2) {
		t.Fatal("cache key must change when messages change")
	}
}

func TestInMemoryCache_HitMissAndCostZeroing(t *testing.T) {
	c := NewInMemoryCache(time.Minute)
	key := "k1"

	if _, ok := c.Get(key); ok {
		t.Fatal("expected miss on empty cache")
	}

	c.Set(key, &ChatResponse{Content: "cached", Cost: 0.05, Latency: 2 * time.Second})

	got, ok := c.Get(key)
	if !ok {
		t.Fatal("expected hit after Set")
	}
	if got.Content != "cached" {
		t.Fatalf("wrong content: %q", got.Content)
	}
	// A cache hit costs nothing — that is the whole point.
	if got.Cost != 0 {
		t.Fatalf("cache hit should report zero cost, got %v", got.Cost)
	}

	st := c.Stats()
	if st.Hits != 1 || st.Misses != 1 {
		t.Fatalf("unexpected stats: %+v", st)
	}
}

func TestInMemoryCache_Expiry(t *testing.T) {
	c := NewInMemoryCache(time.Minute)
	fake := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return fake }

	c.Set("k", &ChatResponse{Content: "x"})
	if _, ok := c.Get("k"); !ok {
		t.Fatal("expected hit before expiry")
	}

	fake = fake.Add(2 * time.Minute) // advance past TTL
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected miss after TTL expiry")
	}
}

func TestInMemoryCache_Concurrent(t *testing.T) {
	c := NewInMemoryCache(time.Minute)
	var wg sync.WaitGroup

	// Concurrent writes
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := "key"
			c.Set(key, &ChatResponse{Content: "val"})
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Get("key")
			c.Stats()
		}()
	}

	wg.Wait()
}

func TestInMemoryCache_NilResponse(t *testing.T) {
	c := NewInMemoryCache(time.Minute)
	c.Set("k", nil)
	if _, ok := c.Get("k"); ok {
		t.Fatal("nil response should not be stored")
	}
}

func TestInMemoryCache_CacheHitZeroLatency(t *testing.T) {
	c := NewInMemoryCache(time.Minute)
	c.Set("k", &ChatResponse{Content: "x", Cost: 0.10, Latency: 5 * time.Second})
	got, ok := c.Get("k")
	if !ok {
		t.Fatal("expected hit")
	}
	if got.Cost != 0 {
		t.Errorf("expected zero cost on cache hit, got %v", got.Cost)
	}
	if got.Latency != 0 {
		t.Errorf("expected zero latency on cache hit, got %v", got.Latency)
	}
}

func TestInMemoryCache_MultipleKeys(t *testing.T) {
	c := NewInMemoryCache(time.Minute)
	c.Set("a", &ChatResponse{Content: "alpha"})
	c.Set("b", &ChatResponse{Content: "beta"})

	got, ok := c.Get("a")
	if !ok || got.Content != "alpha" {
		t.Errorf("key a: got %v, want alpha", got)
	}
	got, ok = c.Get("b")
	if !ok || got.Content != "beta" {
		t.Errorf("key b: got %v, want beta", got)
	}
}

func TestInMemoryCache_ConcurrentReadWrite(t *testing.T) {
	c := NewInMemoryCache(time.Minute)
	var wg sync.WaitGroup
	const n = 100

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			c.Set("key", &ChatResponse{Content: "val"})
		}(i)
	}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Get("key")
			c.Stats()
		}()
	}
	wg.Wait()

	// Verify cache still works after concurrent access
	c.Set("after", &ChatResponse{Content: "ok"})
	if _, ok := c.Get("after"); !ok {
		t.Error("cache broken after concurrent access")
	}
}

func TestInMemoryCache_ExpiredEntryEvicted(t *testing.T) {
	c := NewInMemoryCache(time.Minute)
	fake := time.Unix(1_700_000_000, 0)
	c.now = func() time.Time { return fake }

	c.Set("k", &ChatResponse{Content: "x"})
	if _, ok := c.Get("k"); !ok {
		t.Fatal("expected hit")
	}

	// Advance past TTL
	fake = fake.Add(2 * time.Minute)
	if _, ok := c.Get("k"); ok {
		t.Fatal("expected miss after expiry")
	}

	// Verify entry was evicted from map
	c.mu.RLock()
	_, exists := c.entries["k"]
	c.mu.RUnlock()
	if exists {
		t.Error("expired entry should be evicted from map")
	}
}

func TestInMemoryCache_NilResponseNotStored(t *testing.T) {
	c := NewInMemoryCache(time.Minute)
	c.Set("k", nil)
	if _, ok := c.Get("k"); ok {
		t.Fatal("nil response should not be stored")
	}

	// Also verify stats: should be a miss, not a hit
	st := c.Stats()
	if st.Misses == 0 {
		t.Error("expected at least one miss")
	}
}
