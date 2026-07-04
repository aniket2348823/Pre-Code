// Package cache provides intelligent response caching for LLM operations.
// It uses content hashing to deduplicate identical requests and supports
// configurable TTL, LRU eviction, and cache hit rate tracking.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

// CachedResponse stores a cached LLM response.
type CachedResponse struct {
	Key           string        `json:"key"`
	Model         string        `json:"model"`
	Prompt        string        `json:"prompt"`
	Response      string        `json:"response"`
	TokensUsed    int           `json:"tokens_used"`
	CostUSD       float64       `json:"cost_usd"`
	HitCount      int           `json:"hit_count"`
	CreatedAt     time.Time     `json:"created_at"`
	ExpiresAt     time.Time     `json:"expires_at"`
	Tags          []string      `json:"tags,omitempty"`
}

// CacheStats tracks cache performance.
type CacheStats struct {
	HitCount      int64   `json:"hit_count"`
	MissCount     int64   `json:"miss_count"`
	HitRate       float64 `json:"hit_rate"`
	TotalSavedUSD float64 `json:"total_saved_usd"`
	EvictionCount int64   `json:"eviction_count"`
	Size          int     `json:"size"`
	MaxSize       int     `json:"max_size"`
}

// Config configures the response cache.
type Config struct {
	MaxSize      int           `json:"max_size"`      // max entries
	DefaultTTL   time.Duration `json:"default_ttl"`   // default TTL
	KeyPrefix    string        `json:"key_prefix"`     // prefix for all keys
	EnableHash   bool          `json:"enable_hash"`    // use content hashing for keys
}

// ResponseCache provides LLM response caching.
type ResponseCache struct {
	mu      sync.RWMutex
	entries map[string]*CachedResponse
	order   []string // LRU order: most recent at end
	config  Config
	stats   CacheStats
}

// NewResponseCache creates a new response cache.
func NewResponseCache(cfg Config) *ResponseCache {
	if cfg.MaxSize <= 0 {
		cfg.MaxSize = 10000
	}
	if cfg.DefaultTTL == 0 {
		cfg.DefaultTTL = 24 * time.Hour
	}
	if cfg.KeyPrefix == "" {
		cfg.KeyPrefix = "llm:"
	}
	return &ResponseCache{
		entries: make(map[string]*CachedResponse),
		config:  cfg,
	}
}

// HashPrompt generates a cache key from model + prompt.
func HashPrompt(model, prompt string) string {
	h := sha256.Sum256([]byte(model + ":" + prompt))
	return hex.EncodeToString(h[:16])
}

// Get retrieves a cached response by key. Returns nil if not found or expired.
func (c *ResponseCache) Get(key string) *CachedResponse {
	c.mu.Lock() // write lock because we update LRU order and hit count
	defer c.mu.Unlock()

	entry, ok := c.entries[key]
	if !ok {
		c.stats.MissCount++
		return nil
	}

	if time.Now().After(entry.ExpiresAt) {
		c.deleteEntry(key)
		c.stats.MissCount++
		return nil
	}

	// Update LRU order: move to end
	c.moveToEnd(key)
	entry.HitCount++
	c.stats.HitCount++
	c.stats.TotalSavedUSD += entry.CostUSD

	cp := *entry
	return &cp
}

// GetByPrompt looks up a cached response by model and prompt content.
func (c *ResponseCache) GetByPrompt(model, prompt string) *CachedResponse {
	key := c.config.KeyPrefix + HashPrompt(model, prompt)
	return c.Get(key)
}

// Put stores a response in the cache.
func (c *ResponseCache) Put(key, model, prompt, response string, tokensUsed int, costUSD float64, tags []string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	entry := &CachedResponse{
		Key:        key,
		Model:      model,
		Prompt:     prompt,
		Response:   response,
		TokensUsed: tokensUsed,
		CostUSD:    costUSD,
		HitCount:   0,
		CreatedAt:  now,
		ExpiresAt:  now.Add(c.config.DefaultTTL),
		Tags:       tags,
	}

	// If key exists, update
	if _, ok := c.entries[key]; ok {
		c.entries[key] = entry
		c.moveToEnd(key)
		return
	}

	// Evict if at capacity
	for len(c.entries) >= c.config.MaxSize {
		c.evictOldest()
	}

	c.entries[key] = entry
	c.order = append(c.order, key)
}

// PutWithTTL stores a response with a custom TTL.
func (c *ResponseCache) PutWithTTL(key, model, prompt, response string, tokensUsed int, costUSD float64, ttl time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	entry := &CachedResponse{
		Key:        key,
		Model:      model,
		Prompt:     prompt,
		Response:   response,
		TokensUsed: tokensUsed,
		CostUSD:    costUSD,
		CreatedAt:  now,
		ExpiresAt:  now.Add(ttl),
	}

	if _, ok := c.entries[key]; ok {
		c.entries[key] = entry
		c.moveToEnd(key)
		return
	}

	for len(c.entries) >= c.config.MaxSize {
		c.evictOldest()
	}

	c.entries[key] = entry
	c.order = append(c.order, key)
}

// Delete removes an entry from the cache.
func (c *ResponseCache) Delete(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.entries[key]; !ok {
		return false
	}
	c.deleteEntry(key)
	return true
}

// Contains checks if a key exists and is not expired.
func (c *ResponseCache) Contains(key string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[key]
	if !ok {
		return false
	}
	return time.Now().Before(entry.ExpiresAt)
}

// Size returns the number of entries.
func (c *ResponseCache) Size() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.entries)
}

// Stats returns cache statistics.
func (c *ResponseCache) Stats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()
	stats := c.stats
	stats.Size = len(c.entries)
	stats.MaxSize = c.config.MaxSize
	total := stats.HitCount + stats.MissCount
	if total > 0 {
		stats.HitRate = float64(stats.HitCount) / float64(total) * 100
	}
	return stats
}

// Clear removes all entries.
func (c *ResponseCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]*CachedResponse)
	c.order = c.order[:0]
}

// PurgeExpired removes all expired entries.
func (c *ResponseCache) PurgeExpired() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	var expired []string
	for key, entry := range c.entries {
		if now.After(entry.ExpiresAt) {
			expired = append(expired, key)
		}
	}
	for _, key := range expired {
		c.deleteEntry(key)
	}
	return len(expired)
}

// Keys returns all non-expired keys.
func (c *ResponseCache) Keys() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	now := time.Now()
	keys := make([]string, 0, len(c.entries))
	for key, entry := range c.entries {
		if now.Before(entry.ExpiresAt) {
			keys = append(keys, key)
		}
	}
	return keys
}

// deleteEntry removes an entry (caller must hold lock).
func (c *ResponseCache) deleteEntry(key string) {
	delete(c.entries, key)
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			return
		}
	}
}

// evictOldest removes the LRU entry (caller must hold lock).
func (c *ResponseCache) evictOldest() {
	if len(c.order) == 0 {
		return
	}
	key := c.order[0]
	c.order = c.order[1:]
	delete(c.entries, key)
	c.stats.EvictionCount++
}

// moveToEnd moves a key to the end of LRU order (caller must hold lock).
func (c *ResponseCache) moveToEnd(key string) {
	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)
			c.order = append(c.order, key)
			return
		}
	}
}
