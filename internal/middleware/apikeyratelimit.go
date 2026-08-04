package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// APIKeyCreateRateLimiter limits API key creation to prevent brute-force abuse.
type APIKeyCreateRateLimiter struct {
	mu      sync.Mutex
	counts  map[string]*apikeyWindowState
	redis   *redis.Client
	maxKeys int
	window  time.Duration
}

type apikeyWindowState struct {
	Count       int
	WindowStart time.Time
}

// APIKeyCreateRateLimiterConfig configures the API key creation rate limiter.
type APIKeyCreateRateLimiterConfig struct {
	MaxKeys int           // max API keys per user per window (default 3)
	Window  time.Duration // sliding window (default 1 hour)
}

// DefaultAPIKeyCreateRateLimiterConfig returns production-ready defaults.
func DefaultAPIKeyCreateRateLimiterConfig() APIKeyCreateRateLimiterConfig {
	return APIKeyCreateRateLimiterConfig{
		MaxKeys: 3,
		Window:  1 * time.Hour,
	}
}

// NewAPIKeyCreateRateLimiter creates a new API key creation rate limiter.
func NewAPIKeyCreateRateLimiter(redisClient *redis.Client, cfg APIKeyCreateRateLimiterConfig) *APIKeyCreateRateLimiter {
	if cfg.MaxKeys <= 0 {
		cfg = DefaultAPIKeyCreateRateLimiterConfig()
	}
	l := &APIKeyCreateRateLimiter{
		counts:  make(map[string]*apikeyWindowState),
		redis:   redisClient,
		maxKeys: cfg.MaxKeys,
		window:  cfg.Window,
	}
	go l.cleanupLoop()
	return l
}

func (l *APIKeyCreateRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for key, state := range l.counts {
			if now.Sub(state.WindowStart) > l.window {
				delete(l.counts, key)
			}
		}
		l.mu.Unlock()
	}
}

func (l *APIKeyCreateRateLimiter) keyPrefix(userID string) string {
	return "apikey_create:" + userID
}

// Allow checks if the user can create another API key.
func (l *APIKeyCreateRateLimiter) Allow(ctx context.Context, userID string) bool {
	key := l.keyPrefix(userID)

	if l.redis != nil {
		return l.allowRedis(ctx, key)
	}
	return l.allowMemory(key)
}

func (l *APIKeyCreateRateLimiter) allowRedis(ctx context.Context, key string) bool {
	count, err := l.redis.Get(ctx, key).Int()
	if err != nil {
		return true // key doesn't exist or error → allow
	}
	return count < l.maxKeys
}

func (l *APIKeyCreateRateLimiter) allowMemory(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	state, exists := l.counts[key]
	if !exists {
		return true
	}
	if time.Since(state.WindowStart) > l.window {
		return true
	}
	return state.Count < l.maxKeys
}

// Record records an API key creation for the user.
func (l *APIKeyCreateRateLimiter) Record(ctx context.Context, userID string) {
	key := l.keyPrefix(userID)

	if l.redis != nil {
		l.recordRedis(ctx, key)
		return
	}
	l.recordMemory(key)
}

func (l *APIKeyCreateRateLimiter) recordRedis(ctx context.Context, key string) {
	count, err := l.redis.Incr(ctx, key).Result()
	if err != nil {
		slog.Warn("api key rate limiter: redis incr failed", "error", err)
		return
	}
	if count == 1 {
		l.redis.Expire(ctx, key, l.window)
	}
}

func (l *APIKeyCreateRateLimiter) recordMemory(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	state, exists := l.counts[key]
	if !exists {
		state = &apikeyWindowState{WindowStart: time.Now()}
		l.counts[key] = state
	}
	if time.Since(state.WindowStart) > l.window {
		state.Count = 0
		state.WindowStart = time.Now()
	}
	state.Count++
}

// GetRemaining returns how many API keys the user can still create.
func (l *APIKeyCreateRateLimiter) GetRemaining(ctx context.Context, userID string) int {
	key := l.keyPrefix(userID)

	if l.redis != nil {
		count, err := l.redis.Get(ctx, key).Int()
		if err != nil {
			return l.maxKeys
		}
		remaining := l.maxKeys - count
		if remaining < 0 {
			return 0
		}
		return remaining
	}

	l.mu.Lock()
	defer l.mu.Unlock()

	state, exists := l.counts[key]
	if !exists || time.Since(state.WindowStart) > l.window {
		return l.maxKeys
	}
	remaining := l.maxKeys - state.Count
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Middleware returns HTTP middleware that limits API key creation per user.
// Must be placed after auth middleware (requires JWT claims in context).
func (l *APIKeyCreateRateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only limit POST requests to /api-keys
			if r.Method != http.MethodPost || !isAPIKeyCreatePath(r.URL.Path) {
				next.ServeHTTP(w, r)
				return
			}

			claims, ok := auth.ClaimsFromContext(r.Context())
			if !ok || claims == nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()

			if !l.Allow(ctx, claims.UserID) {
				remaining := l.GetRemaining(ctx, claims.UserID)
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(l.maxKeys))
				w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(remaining))
				w.Header().Set("Retry-After", strconv.FormatInt(int64(l.window.Seconds()), 10))
				response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"code":    "INFRA_004",
					"error":   "API key creation rate limit exceeded",
					"limit":   l.maxKeys,
					"window":  l.window.String(),
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isAPIKeyCreatePath checks if the path matches the API key creation endpoint.
func isAPIKeyCreatePath(path string) bool {
	// Match /api/v1/api-keys (exact POST for creation)
	return path == "/api/v1/api-keys" || path == "/api-keys"
}
