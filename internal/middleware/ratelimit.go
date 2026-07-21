package middleware

import (
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// rateLimitScript is a Lua script for atomic sliding window rate limiting.
var rateLimitScript = redis.NewScript(`
local key = KEYS[1]
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

redis.call('ZREMRANGEBYSCORE', key, 0, now - window)

local count = redis.call('ZCARD', key)

if count < limit then
    redis.call('ZADD', key, now, now .. '-' .. math.random())
    redis.call('EXPIRE', key, window)
    return {count + 1, 0}
else
    local oldest = redis.call('ZRANGE', key, 0, 0, 'WITHSCORES')
    local retryAfter = 0
    if #oldest > 0 then
        retryAfter = math.ceil(window - (now - tonumber(oldest[2])))
    end
    return {count, retryAfter}
end
`)

// RateLimiter provides Redis-backed sliding window rate limiting.
type RateLimiter struct {
	client *redis.Client
	limit  int64
	window time.Duration
}

// NewRateLimiter creates a new rate limiter with the given limit and window.
func NewRateLimiter(client *redis.Client, limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		client: client,
		limit:  int64(limit),
		window: window,
	}
}

// Middleware returns a chi-compatible middleware for rate limiting.
func (rl *RateLimiter) Middleware(keyFunc func(r *http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			key := "ratelimit:" + keyFunc(r)
			now := time.Now().Unix()

			result, err := rateLimitScript.Run(ctx, rl.client, []string{key},
				int64(rl.window.Seconds()),
				rl.limit,
				now,
			).Int64Slice()
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			count := result[0]
			retryAfter := result[1]

			w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(rl.limit, 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(max(0, rl.limit-count), 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now+int64(rl.window.Seconds()), 10))

			if count > rl.limit {
				if retryAfter > 0 {
					w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
				}
				response.JSON(w, http.StatusTooManyRequests, map[string]string{
					"code":  "INFRA_001",
					"error": "rate limit exceeded",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RateLimitByKey is a simple rate limiter that uses a fixed key.
func RateLimitByKey(client *redis.Client, key string, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(client, limit, window)
	return limiter.Middleware(func(r *http.Request) string {
		return key
	})
}

// RateLimitByIP rate limits by client IP address.
func RateLimitByIP(client *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(client, limit, window)
	return limiter.Middleware(func(r *http.Request) string {
		ip := r.Header.Get("X-Forwarded-For")
		if ip == "" {
			ip = r.Header.Get("X-Real-IP")
		}
		if ip == "" {
			ip = r.RemoteAddr
		}
		return "ip:" + ip
	})
}

// RateLimitByUser rate limits by authenticated user ID.
func RateLimitByUser(client *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(client, limit, window)
	return limiter.Middleware(func(r *http.Request) string {
		userID := r.Header.Get("X-User-ID")
		if userID == "" {
			userID = r.RemoteAddr
		}
		return "user:" + userID
	})
}

// RateLimitByIPKey extracts the client IP address for use as a rate-limit key.
func RateLimitByIPKey(r *http.Request) string {
	ip := r.Header.Get("X-Forwarded-For")
	if ip != "" {
		if idx := strings.Index(ip, ","); idx != -1 {
			ip = strings.TrimSpace(ip[:idx])
		}
	}
	if ip == "" {
		ip = r.Header.Get("X-Real-IP")
	}
	if ip == "" {
		ip = r.RemoteAddr
	}
	if strings.HasPrefix(ip, "[") {
		if idx := strings.LastIndex(ip, "]:"); idx != -1 {
			ip = ip[1:idx]
		}
	} else {
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			ip = ip[:idx]
		}
	}
	return "ip:" + ip
}

// --- Rate Limit Headers Middleware (adds X-RateLimit-* to ALL responses) ---

// RateLimitHeadersMiddleware adds X-RateLimit-* headers to every response.
type RateLimitHeadersMiddleware struct {
	limit    int
	window   time.Duration
	counters map[string]*slidingWindow
	mu       sync.RWMutex
}

type slidingWindow struct {
	count       int
	windowStart time.Time
	mu          sync.Mutex
}

// NewRateLimitHeadersMiddleware creates middleware that sets rate limit headers on every response.
func NewRateLimitHeadersMiddleware(limit int, window time.Duration) *RateLimitHeadersMiddleware {
	rl := &RateLimitHeadersMiddleware{
		limit:    limit,
		window:   window,
		counters: make(map[string]*slidingWindow),
	}
	go rl.cleanup()
	return rl
}

func (rl *RateLimitHeadersMiddleware) cleanup() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		now := time.Now()
		rl.mu.Lock()
		for key, sw := range rl.counters {
			if now.Sub(sw.windowStart) > rl.window {
				delete(rl.counters, key)
			}
		}
		rl.mu.Unlock()
	}
}

// Middleware returns an HTTP middleware that sets rate limit headers on every response.
func (rl *RateLimitHeadersMiddleware) Middleware(keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			if key == "" {
				next.ServeHTTP(w, r)
				return
			}

			rl.mu.RLock()
			sw, exists := rl.counters[key]
			rl.mu.RUnlock()

			now := time.Now()
			if !exists || now.Sub(sw.windowStart) > rl.window {
				rl.mu.Lock()
				sw = &slidingWindow{windowStart: now}
				rl.counters[key] = sw
				rl.mu.Unlock()
			}

			sw.mu.Lock()
			sw.count++
			count := sw.count
			sw.mu.Unlock()

			remaining := rl.limit - count
			if remaining < 0 {
				remaining = 0
			}
			resetAt := sw.windowStart.Add(rl.window)

			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(rl.limit))
			w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetAt.Unix(), 10))

			next.ServeHTTP(w, r)
		})
	}
}
