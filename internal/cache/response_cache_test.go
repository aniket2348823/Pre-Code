package cache

import (
	"testing"
	"time"
)

func TestNewResponseCache(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100, DefaultTTL: time.Hour})
	if c == nil {
		t.Fatal("expected non-nil cache")
	}
	if c.Size() != 0 {
		t.Errorf("expected empty cache, got size %d", c.Size())
	}
}

func TestPutAndGet(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	key := HashPrompt("gpt-4o", "hello")
	c.Put(key, "gpt-4o", "hello", "world", 10, 0.001, nil)
	got := c.Get(key)
	if got == nil {
		t.Fatal("expected to find cached entry")
	}
	if got.Response != "world" {
		t.Errorf("expected response 'world', got %s", got.Response)
	}
}

func TestCacheMiss(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	got := c.Get("nonexistent")
	if got != nil {
		t.Error("expected nil for cache miss")
	}
	stats := c.Stats()
	if stats.MissCount != 1 {
		t.Errorf("expected 1 miss, got %d", stats.MissCount)
	}
}

func TestCacheHitRate(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	key := HashPrompt("gpt-4o", "test")
	c.Put(key, "gpt-4o", "test", "response", 10, 0.01, nil)
	c.Get(key)
	c.Get(key)
	c.Get(key)
	c.Get("miss1")
	stats := c.Stats()
	if stats.HitCount != 3 {
		t.Errorf("expected 3 hits, got %d", stats.HitCount)
	}
	if stats.HitRate != 75.0 {
		t.Errorf("expected 75%% hit rate, got %f", stats.HitRate)
	}
}

func TestGetByPrompt(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100, KeyPrefix: "test:"})
	key := "test:" + HashPrompt("gpt-4o", "prompt1")
	c.Put(key, "gpt-4o", "prompt1", "answer", 5, 0.005, nil)
	got := c.GetByPrompt("gpt-4o", "prompt1")
	if got == nil {
		t.Fatal("expected to find by prompt")
	}
	if got.Response != "answer" {
		t.Errorf("expected answer, got %s", got.Response)
	}
}

func TestLRUEviction(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 3})
	for i := 0; i < 5; i++ {
		key := HashPrompt("model", string(rune('a'+i)))
		c.Put(key, "model", string(rune('a'+i)), "resp", 1, 0.001, nil)
	}
	if c.Size() != 3 {
		t.Errorf("expected size 3 after eviction, got %d", c.Size())
	}
	// First two entries should be evicted
	stats := c.Stats()
	if stats.EvictionCount != 2 {
		t.Errorf("expected 2 evictions, got %d", stats.EvictionCount)
	}
}

func TestDelete(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	key := HashPrompt("model", "delete-me")
	c.Put(key, "model", "delete-me", "resp", 1, 0.001, nil)
	if !c.Delete(key) {
		t.Error("expected delete to return true")
	}
	if c.Get(key) != nil {
		t.Error("expected nil after delete")
	}
	if c.Delete(key) {
		t.Error("expected delete to return false for nonexistent key")
	}
}

func TestContains(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	key := HashPrompt("model", "exists")
	c.Put(key, "model", "exists", "resp", 1, 0.001, nil)
	if !c.Contains(key) {
		t.Error("expected Contains to return true")
	}
	if c.Contains("nope") {
		t.Error("expected Contains to return false for nonexistent key")
	}
}

func TestClear(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	c.Put("k1", "m", "p", "r", 1, 0.001, nil)
	c.Put("k2", "m", "p2", "r2", 1, 0.001, nil)
	c.Clear()
	if c.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", c.Size())
	}
}

func TestPurgeExpired(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100, DefaultTTL: time.Millisecond})
	key := HashPrompt("model", "expire-me")
	c.Put(key, "model", "expire-me", "resp", 1, 0.001, nil)
	time.Sleep(5 * time.Millisecond)
	purged := c.PurgeExpired()
	if purged != 1 {
		t.Errorf("expected 1 purged, got %d", purged)
	}
	if c.Size() != 0 {
		t.Errorf("expected size 0 after purge, got %d", c.Size())
	}
}

func TestPutWithTTL(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	key := "custom-ttl"
	c.PutWithTTL(key, "model", "p", "r", 1, 0.001, time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	if c.Contains(key) {
		t.Error("expected expired entry to be gone")
	}
}

func TestKeys(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	c.Put("k1", "m", "p", "r", 1, 0.001, nil)
	c.Put("k2", "m", "p2", "r2", 1, 0.001, nil)
	keys := c.Keys()
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestConcurrentAccess(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	done := make(chan bool, 100)
	for i := 0; i < 100; i++ {
		go func(n int) {
			key := HashPrompt("model", string(rune('a'+n%26)))
			c.Put(key, "model", string(rune('a'+n%26)), "resp", 1, 0.001, nil)
			c.Get(key)
			done <- true
		}(i)
	}
	for i := 0; i < 100; i++ {
		<-done
	}
}
