package llm

import (
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
