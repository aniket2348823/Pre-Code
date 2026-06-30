package middleware

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// Lua script for atomic rate limiting (sliding window counter)
// KEYS[1] = rate limit key
// ARGV[1] = window size in seconds
// ARGV[2] = max requests
// ARGV[3] = current timestamp
var rateLimitScript = redis.NewScript(`
local key = KEYS[1]
local window = tonumber(ARGV[1])
local limit = tonumber(ARGV[2])
local now = tonumber(ARGV[3])

-- Clean up expired entries
redis.call('ZREMRANGEBYSCORE', key, 0, now - window)

-- Get current count
local count = redis.call('ZCARD', key)

if count < limit then
    -- Add current request
    redis.call('ZADD', key, now, now .. '-' .. math.random())
    redis.call('EXPIRE', key, window)
    return {count + 1, 0}
else
    -- Get oldest entry to calculate retry-after
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

			// Execute atomic Lua script
			result, err := rateLimitScript.Run(ctx, rl.client, []string{key},
				int64(rl.window.Seconds()),
				rl.limit,
				now,
			).Int64Slice()
			if err != nil {
				// On Redis error, allow the request through
				next.ServeHTTP(w, r)
				return
			}

			count := result[0]
			retryAfter := result[1]

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(rl.limit, 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(max(0, rl.limit-count), 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now+int64(rl.window.Seconds()), 10))

			if count > rl.limit {
				if retryAfter > 0 {
					w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
				}
				response.JSON(w, http.StatusTooManyRequests, map[string]string{
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
