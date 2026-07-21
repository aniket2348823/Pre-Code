package llm

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

// ResponseCache caches LLM responses so identical requests can be served
// without a paid provider call. Implementations may be exact-match (hash of
// the request) or semantic (embedding similarity); the router only depends on
// this interface.
type ResponseCache interface {
	// Get returns a cached response for the key, if present and unexpired.
	Get(key string) (*ChatResponse, bool)
	// Set stores a response under the key.
	Set(key string, resp *ChatResponse)
	// Stats returns hit/miss counters for cost-savings reporting.
	Stats() CacheStats
}

// CacheStats reports cache effectiveness.
type CacheStats struct {
	Hits   int64
	Misses int64
}

// CacheKey derives a stable key from the parts of a request that determine the
// response. Model, system prompt, messages, and sampling params all matter; a
// change in any of them must miss the cache.
func CacheKey(req *ChatRequest) string {
	h := sha256.New()
	payload := struct {
		Model       string    `json:"model"`
		System      string    `json:"system"`
		Messages    []Message `json:"messages"`
		MaxTokens   int       `json:"max_tokens"`
		Temperature float64   `json:"temperature"`
	}{
		Model:       req.Model,
		System:      req.System,
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	}
	// json.Marshal on a struct is deterministic (field order is fixed).
	b, _ := json.Marshal(payload)
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

// InMemoryCache is a TTL-bounded, exact-match ResponseCache. It is safe for
// concurrent use and is the default cache for single-instance deployments.
// A Redis-backed or semantic cache can replace it behind the ResponseCache
// interface without touching the router.
type InMemoryCache struct {
	mu      sync.RWMutex
	ttl     time.Duration
	entries map[string]cacheEntry
	now     func() time.Time // injectable for tests
	hits    int64
	misses  int64
}

type cacheEntry struct {
	resp      *ChatResponse
	expiresAt time.Time
}

// NewInMemoryCache creates a cache with the given entry TTL.
func NewInMemoryCache(ttl time.Duration) *InMemoryCache {
	return &InMemoryCache{
		ttl:     ttl,
		entries: make(map[string]cacheEntry),
		now:     time.Now,
	}
}

// Get returns a cached response if present and unexpired.
func (c *InMemoryCache) Get(key string) (*ChatResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok || c.now().After(e.expiresAt) {
		if ok {
			delete(c.entries, key) // evict expired
		}
		c.misses++
		return nil, false
	}
	c.hits++
	// Return a copy marked as cache-served (zero cost, zero latency).
	cp := *e.resp
	cp.Cost = 0
	cp.Latency = 0
	return &cp, true
}

// Set stores a response under the key with the configured TTL.
func (c *InMemoryCache) Set(key string, resp *ChatResponse) {
	if resp == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	stored := *resp
	c.entries[key] = cacheEntry{resp: &stored, expiresAt: c.now().Add(c.ttl)}
}

// Stats returns hit/miss counters.
func (c *InMemoryCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return CacheStats{Hits: c.hits, Misses: c.misses}
}
