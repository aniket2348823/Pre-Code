package ratelimit

import (
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Tier represents a rate limit tier for different subscription plans.
type Tier struct {
	RequestsPerMinute int
	RequestsPerHour   int
	RequestsPerDay    int
}

var (
	// FreeTier is the default tier for free users.
	FreeTier = Tier{
		RequestsPerMinute: 30,
		RequestsPerHour:   500,
		RequestsPerDay:    5000,
	}

	// ProTier is for paid pro users.
	ProTier = Tier{
		RequestsPerMinute: 120,
		RequestsPerHour:   5000,
		RequestsPerDay:    50000,
	}

	// TeamTier is for team/enterprise users.
	TeamTier = Tier{
		RequestsPerMinute: 600,
		RequestsPerHour:   20000,
		RequestsPerDay:    200000,
	}
)

// TieredRateLimiter provides per-user rate limiting with tier-based limits.
type TieredRateLimiter struct {
	buckets map[string]*tokenBucket
	mu      sync.Mutex
}

type tokenBucket struct {
	mu           sync.Mutex
	minuteTokens float64
	hourTokens   float64
	dayTokens    float64
	lastRefill   time.Time
	tier         Tier
}

// NewTieredRateLimiter creates a new tiered rate limiter.
func NewTieredRateLimiter() *TieredRateLimiter {
	l := &TieredRateLimiter{
		buckets: make(map[string]*tokenBucket),
	}
	go l.cleanup()
	return l
}

// GetTier returns the rate limit tier for a given plan.
func GetTier(plan string) Tier {
	switch plan {
	case "pro":
		return ProTier
	case "team", "enterprise":
		return TeamTier
	default:
		return FreeTier
	}
}

// Middleware returns a chi-compatible rate limiting middleware that uses tier-based limits.
func (l *TieredRateLimiter) Middleware(keyFunc func(*http.Request) string, tierFunc func(*http.Request) Tier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			tier := tierFunc(r)

			l.mu.Lock()
			bucket, exists := l.buckets[key]
			if !exists || bucket.tier != tier {
				bucket = &tokenBucket{
					minuteTokens: float64(tier.RequestsPerMinute),
					hourTokens:   float64(tier.RequestsPerHour),
					dayTokens:    float64(tier.RequestsPerDay),
					lastRefill:   time.Now(),
					tier:         tier,
				}
				l.buckets[key] = bucket
			}
			l.mu.Unlock()

			bucket.mu.Lock()
			bucket.refill(tier)
			if bucket.minuteTokens <= 0 || bucket.hourTokens <= 0 || bucket.dayTokens <= 0 {
				bucket.mu.Unlock()
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(tier.RequestsPerMinute))
			w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("Retry-After", "60")
				w.Header().Set("X-RateLimit-Tier", tierName(tier))
				http.Error(w, `{"error":"rate limit exceeded"}`, http.StatusTooManyRequests)
				return
			}
			bucket.minuteTokens--
			bucket.hourTokens--
			bucket.dayTokens--
			remaining := int(bucket.minuteTokens)
			bucket.mu.Unlock()

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(tier.RequestsPerMinute))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Tier", tierName(tier))

			next.ServeHTTP(w, r)
		})
	}
}

func (b *tokenBucket) refill(tier Tier) {
	now := time.Now()
	elapsed := now.Sub(b.lastRefill)

	// Refill minute bucket
	minuteRefills := elapsed.Seconds() / 60.0 * float64(tier.RequestsPerMinute)
	b.minuteTokens += minuteRefills
	if b.minuteTokens > float64(tier.RequestsPerMinute) {
		b.minuteTokens = float64(tier.RequestsPerMinute)
	}

	// Refill hour bucket
	hourRefills := elapsed.Seconds() / 3600.0 * float64(tier.RequestsPerHour)
	b.hourTokens += hourRefills
	if b.hourTokens > float64(tier.RequestsPerHour) {
		b.hourTokens = float64(tier.RequestsPerHour)
	}

	// Refill day bucket
	dayRefills := elapsed.Seconds() / 86400.0 * float64(tier.RequestsPerDay)
	b.dayTokens += dayRefills
	if b.dayTokens > float64(tier.RequestsPerDay) {
		b.dayTokens = float64(tier.RequestsPerDay)
	}

	b.lastRefill = now
}

func (l *TieredRateLimiter) cleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		for k, b := range l.buckets {
			if time.Since(b.lastRefill) > 1*time.Hour {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}

func tierName(t Tier) string {
	if t == TeamTier {
		return "team"
	}
	if t == ProTier {
		return "pro"
	}
	return "free"
}
