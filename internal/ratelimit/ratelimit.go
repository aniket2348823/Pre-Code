// Package ratelimit provides advanced rate limiting with sliding window,
// token bucket, and adaptive algorithms for API endpoint protection.
package ratelimit

import (
	"sync"
	"time"
)

// Algorithm type for rate limiting.
type Algorithm string

const (
	SlidingWindow Algorithm = "sliding_window"
	TokenBucket   Algorithm = "token_bucket"
	FixedWindow   Algorithm = "fixed_window"
)

// Limiter enforces rate limits on requests.
type Limiter struct {
	mu         sync.Mutex
	algorithm  Algorithm
	limit      int
	window     time.Duration
	maxTokens  float64
	refillRate float64
	buckets    map[string]*windowBucket
}

// windowBucket tracks requests for a specific key in a window.
type windowBucket struct {
	requests   []time.Time
	tokenCount float64
	lastRefill time.Time
}

// NewLimiter creates a rate limiter.
func NewLimiter(algorithm Algorithm, limit int, window time.Duration) *Limiter {
	return &Limiter{
		algorithm:  algorithm,
		limit:      limit,
		window:     window,
		maxTokens:  float64(limit),
		refillRate: float64(limit) / window.Seconds(),
		buckets:    make(map[string]*windowBucket),
	}
}

// Allow checks if a request is allowed under the rate limit.
func (l *Limiter) Allow() bool {
	return l.AllowKey("global")
}

// AllowKey checks if a request for a specific key is allowed.
func (l *Limiter) AllowKey(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	switch l.algorithm {
	case TokenBucket:
		return l.allowTokenBucket(key)
	case FixedWindow:
		return l.allowFixedWindow(key)
	default:
		return l.allowSlidingWindow(key)
	}
}

func (l *Limiter) allowSlidingWindow(key string) bool {
	bucket := l.getOrCreateBucket(key)
	now := time.Now()
	cutoff := now.Add(-l.window)

	// Remove expired entries
	valid := bucket.requests[:0]
	for _, t := range bucket.requests {
		if t.After(cutoff) {
			valid = append(valid, t)
		}
	}
	bucket.requests = valid

	if len(bucket.requests) >= l.limit {
		return false
	}
	bucket.requests = append(bucket.requests, now)
	return true
}

func (l *Limiter) allowTokenBucket(key string) bool {
	bucket := l.getOrCreateBucket(key)
	now := time.Now()

	// Refill tokens
	elapsed := now.Sub(bucket.lastRefill).Seconds()
	bucket.tokenCount += elapsed * l.refillRate
	if bucket.tokenCount > l.maxTokens {
		bucket.tokenCount = l.maxTokens
	}
	bucket.lastRefill = now

	if bucket.tokenCount < 1 {
		return false
	}
	bucket.tokenCount--
	return true
}

func (l *Limiter) allowFixedWindow(key string) bool {
	bucket := l.getOrCreateBucket(key)
	now := time.Now()
	windowStart := now.Truncate(l.window)

	// Reset if new window
	if bucket.lastRefill.Before(windowStart) {
		bucket.tokenCount = 0
		bucket.lastRefill = windowStart
	}

	if bucket.tokenCount >= float64(l.limit) {
		return false
	}
	bucket.tokenCount++
	return true
}

func (l *Limiter) getOrCreateBucket(key string) *windowBucket {
	bucket, ok := l.buckets[key]
	if !ok {
		var initTokens float64
		if l.algorithm == TokenBucket {
			initTokens = l.maxTokens
		}
		bucket = &windowBucket{
			tokenCount: initTokens,
			lastRefill: time.Now(),
		}
		l.buckets[key] = bucket
	}
	return bucket
}

// Stats returns rate limiter statistics.
func (l *Limiter) Stats() map[string]interface{} {
	l.mu.Lock()
	defer l.mu.Unlock()
	return map[string]interface{}{
		"algorithm": string(l.algorithm),
		"limit":     l.limit,
		"window":    l.window.String(),
		"keys":      len(l.buckets),
	}
}

// Reset clears all rate limit state.
func (l *Limiter) Reset() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buckets = make(map[string]*windowBucket)
}

// ResetKey clears rate limit state for a specific key.
func (l *Limiter) ResetKey(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}
