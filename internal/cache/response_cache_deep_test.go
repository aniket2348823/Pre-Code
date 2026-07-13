package cache

import (
	"sync"
	"testing"
	"time"
)

func TestPut_MaxSize1(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 1})
	c.Put("k1", "m", "p", "r1", 1, 0.001, nil)
	if c.Size() != 1 {
		t.Errorf("size should be 1, got %d", c.Size())
	}
	c.Put("k2", "m", "p2", "r2", 1, 0.001, nil)
	if c.Size() != 1 {
		t.Errorf("size should still be 1, got %d", c.Size())
	}
	if c.Get("k1") != nil {
		t.Error("k1 should be evicted")
	}
	if c.Get("k2") == nil {
		t.Error("k2 should exist")
	}
}

func TestPut_MaxSize0(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 0})
	if c.Size() != 0 {
		t.Errorf("size should be 0, got %d", c.Size())
	}
}

func TestPut_EmptyKey(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	c.Put("", "m", "p", "r", 1, 0.001, nil)
	if c.Size() != 1 {
		t.Error("empty key should be stored")
	}
}

func TestPut_TTL0(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	c.PutWithTTL("k", "m", "p", "r", 1, 0.001, 0)
	// TTL=0 means ExpiresAt is now, should be expired
	time.Sleep(10 * time.Millisecond)
	if c.Contains("k") {
		t.Error("TTL=0 should be expired immediately")
	}
}

func TestGetAfterPut_TTL1ms(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	c.PutWithTTL("k", "m", "p", "r", 1, 0.001, 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
	if c.Get("k") != nil {
		t.Error("should be expired")
	}
}

func TestConcurrentPutGet(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	var wg sync.WaitGroup
	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := HashPrompt("model", string(rune('a'+n%26)))
			c.Put(key, "model", string(rune('a'+n%26)), "resp", 1, 0.001, nil)
			c.Get(key)
		}(i)
	}
	wg.Wait()
}

func TestConcurrentPutDelete(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			key := HashPrompt("model", string(rune('a'+n%26)))
			c.Put(key, "model", string(rune('a'+n%26)), "resp", 1, 0.001, nil)
			c.Delete(key)
		}(i)
	}
	wg.Wait()
}

func TestLRU_EvictionOrder(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 3})
	c.Put("a", "m", "pa", "ra", 1, 0.001, nil)
	c.Put("b", "m", "pb", "rb", 1, 0.001, nil)
	c.Put("c", "m", "pc", "rc", 1, 0.001, nil)
	// Access a to make it most recent
	c.Get("a")
	c.Put("d", "m", "pd", "rd", 1, 0.001, nil) // should evict b (LRU)
	if c.Get("a") == nil {
		t.Error("a should still exist")
	}
	if c.Get("b") != nil {
		t.Error("b should be evicted")
	}
	if c.Get("d") == nil {
		t.Error("d should exist")
	}
}

func TestHitRate_ZeroTotal(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	stats := c.Stats()
	if stats.HitRate != 0 {
		t.Errorf("hit rate should be 0, got %f", stats.HitRate)
	}
}

func TestHitRate_AllHits(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	key := HashPrompt("m", "p")
	c.Put(key, "m", "p", "r", 1, 0.01, nil)
	c.Get(key)
	c.Get(key)
	stats := c.Stats()
	if stats.HitCount != 2 {
		t.Errorf("expected 2 hits, got %d", stats.HitCount)
	}
}

func TestHitRate_AllMisses(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	c.Get("miss1")
	c.Get("miss2")
	stats := c.Stats()
	if stats.MissCount != 2 {
		t.Errorf("expected 2 misses, got %d", stats.MissCount)
	}
}

func TestPurgeExpired_Mixed(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	c.PutWithTTL("expire", "m", "p", "r", 1, 0.001, 50*time.Millisecond)
	c.Put("keep", "m", "p2", "r2", 1, 0.001, nil)
	time.Sleep(100 * time.Millisecond)
	purged := c.PurgeExpired()
	if purged != 1 {
		t.Errorf("expected 1 purged, got %d", purged)
	}
	if c.Size() != 1 {
		t.Errorf("expected 1 remaining, got %d", c.Size())
	}
}

func TestClear_Concurrent(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Put("k", "m", "p", "r", 1, 0.001, nil)
		}()
	}
	wg.Wait()
	c.Clear()
	if c.Size() != 0 {
		t.Errorf("size should be 0 after clear, got %d", c.Size())
	}
}

func TestKeys_Snapshot(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	c.Put("k1", "m", "p", "r", 1, 0.001, nil)
	c.Put("k2", "m", "p2", "r2", 1, 0.001, nil)
	keys := c.Keys()
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

func TestSize_Accuracy(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	for i := 0; i < 50; i++ {
		c.Put(HashPrompt("m", string(rune('a'+i%26))), "m", "p", "r", 1, 0.001, nil)
	}
	c.Clear()
	if c.Size() != 0 {
		t.Errorf("expected 0 after clear, got %d", c.Size())
	}
}

func TestHashPrompt_Empty(t *testing.T) {
	h := HashPrompt("", "")
	if len(h) == 0 {
		t.Error("empty inputs should produce non-empty hash")
	}
}

func TestHashPrompt_Long(t *testing.T) {
	longPrompt := make([]byte, 10000)
	for i := range longPrompt {
		longPrompt[i] = 'x'
	}
	h := HashPrompt("model", string(longPrompt))
	if len(h) == 0 {
		t.Error("long prompt should produce non-empty hash")
	}
}

func TestHashPrompt_Deterministic(t *testing.T) {
	h1 := HashPrompt("gpt-4o", "hello")
	h2 := HashPrompt("gpt-4o", "hello")
	if h1 != h2 {
		t.Error("same input should produce same hash")
	}
}

func TestConcurrentClear(t *testing.T) {
	c := NewResponseCache(Config{MaxSize: 100})
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.Put("k", "m", "p", "r", 1, 0.001, nil)
			c.Clear()
		}()
	}
	wg.Wait()
}
