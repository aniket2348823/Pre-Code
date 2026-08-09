package middleware

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/internal/auth"
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
			// Fail open when Redis is unavailable (e.g. dev mode without the
			// container): go-redis panics with a nil-pointer deref when a nil
			// client is used, and rate limiting must never crash a request.
			if rl.client == nil {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()
			key := "ratelimit:" + keyFunc(r)
			now := time.Now().Unix()

			result, err := rateLimitScript.Run(ctx, rl.client, []string{key},
				int64(rl.window.Seconds()),
				rl.limit,
				now,
			).Int64Slice()
			if err != nil {
				slog.Warn("rate limit check failed", "error", err, "key", key)
				next.ServeHTTP(w, r)
				return
			}

			count := result[0]
			retryAfter := result[1]

			w.Header().Set("X-RateLimit-Limit", strconv.FormatInt(rl.limit, 10))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(max(0, rl.limit-count), 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(now+int64(rl.window.Seconds()), 10))

			// The Lua script returns retryAfter > 0 exactly when the request was
			// denied. count alone cannot distinguish: the last allowed request and
			// the first denied request both report count == limit.
			if retryAfter > 0 {
				// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
				slog.Warn("rate limit exceeded", "key", key, "limit", rl.limit, "remote", r.RemoteAddr)
				w.Header().Set("Retry-After", strconv.FormatInt(retryAfter, 10))
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
// Uses only r.RemoteAddr to prevent header spoofing attacks.
func RateLimitByIP(client *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(client, limit, window)
	return limiter.Middleware(RateLimitByIPKey)
}

// RateLimitByUser rate limits by authenticated user ID.
func RateLimitByUser(client *redis.Client, limit int, window time.Duration) func(http.Handler) http.Handler {
	limiter := NewRateLimiter(client, limit, window)
	return limiter.Middleware(func(r *http.Request) string {
		if claims, ok := auth.ClaimsFromContext(r.Context()); ok && claims != nil {
			return "user:" + claims.UserID
		}
		return RateLimitByIPKey(r)
	})
}

// RateLimitByIPKey extracts the client IP address for use as a rate-limit key.
// Uses only the actual connection IP (r.RemoteAddr) to prevent header spoofing.
func RateLimitByIPKey(r *http.Request) string {
	ip := r.RemoteAddr
	if strings.HasPrefix(ip, "[") {
		if idx := strings.LastIndex(ip, "]:"); idx != -1 {
			ip = ip[1:idx]
		} else {
			ip = strings.Trim(ip, "[]")
		}
	} else if strings.Count(ip, ":") == 1 {
		// IPv4 with port: strip the port. Unbracketed IPv6 (no port in
		// practice; net/http brackets IPv6) passes through whole.
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			ip = ip[:idx]
		}
	}
	return "ip:" + ip
}

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

				sw, exists = rl.counters[key]
				if !exists || now.Sub(sw.windowStart) > rl.window {
					sw = &slidingWindow{windowStart: now}
					rl.counters[key] = sw
				}
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
		return true
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
					"code":   "INFRA_004",
					"error":  "API key creation rate limit exceeded",
					"limit":  l.maxKeys,
					"window": l.window.String(),
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// isAPIKeyCreatePath checks if the path matches the API key creation endpoint.
func isAPIKeyCreatePath(path string) bool {

	return path == "/api/v1/api-keys" || path == "/api-keys"
}

// LoginRateLimiter provides per-IP + per-email rate limiting with progressive lockout.
type LoginRateLimiter struct {
	mu       sync.Mutex
	attempts map[string]*loginAttemptState

	redis *redis.Client

	maxAttempts      int
	window           time.Duration
	lockoutDurations []time.Duration
}

type loginAttemptState struct {
	Count        int
	WindowStart  time.Time
	LockedUntil  time.Time
	LockoutLevel int
}

// LoginRateLimiterConfig configures the login rate limiter.
type LoginRateLimiterConfig struct {
	MaxAttempts      int
	Window           time.Duration
	LockoutDurations []time.Duration
}

// DefaultLoginRateLimiterConfig returns production-ready defaults.
func DefaultLoginRateLimiterConfig() LoginRateLimiterConfig {
	return LoginRateLimiterConfig{
		MaxAttempts: 5,
		Window:      1 * time.Minute,
		LockoutDurations: []time.Duration{
			1 * time.Minute,
			5 * time.Minute,
			15 * time.Minute,
			30 * time.Minute,
		},
	}
}

// NewLoginRateLimiter creates a new login rate limiter.
func NewLoginRateLimiter(redisClient *redis.Client, cfg LoginRateLimiterConfig) *LoginRateLimiter {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultLoginRateLimiterConfig()
	}
	if len(cfg.LockoutDurations) == 0 {
		cfg.LockoutDurations = DefaultLoginRateLimiterConfig().LockoutDurations
	}
	l := &LoginRateLimiter{
		attempts:         make(map[string]*loginAttemptState),
		redis:            redisClient,
		maxAttempts:      cfg.MaxAttempts,
		window:           cfg.Window,
		lockoutDurations: cfg.LockoutDurations,
	}
	go l.cleanupLoop()
	return l
}

func (l *LoginRateLimiter) cleanupLoop() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()
	for range ticker.C {
		l.mu.Lock()
		now := time.Now()
		for key, state := range l.attempts {
			expired := !state.LockedUntil.IsZero() && now.After(state.LockedUntil)
			stale := state.LockedUntil.IsZero() && now.Sub(state.WindowStart) > l.window*2
			if expired || stale {
				delete(l.attempts, key)
			}
		}
		l.mu.Unlock()
	}
}

// stateKey returns the map key for a given IP + email combination.
func (l *LoginRateLimiter) stateKey(ip, email string) string {
	return ip + ":" + email
}

// IsLocked checks if the IP+email combination is currently locked out.
func (l *LoginRateLimiter) IsLocked(ctx context.Context, ip, email string) bool {
	if l.redis != nil {
		return l.isLockedRedis(ctx, ip, email)
	}

	key := l.stateKey(ip, email)
	l.mu.Lock()
	defer l.mu.Unlock()

	state, exists := l.attempts[key]
	if !exists {
		return false
	}
	return !state.LockedUntil.IsZero() && time.Now().Before(state.LockedUntil)
}

func (l *LoginRateLimiter) isLockedRedis(ctx context.Context, ip, email string) bool {
	key := "login_lockout:" + ip + ":" + email
	val, err := l.redis.Get(ctx, key).Result()
	if err != nil {
		// Fail closed: a Redis outage must not silently disable login lockout.
		slog.Warn("login rate limiter: lockout read failed, failing closed", "error", err)
		return true
	}
	level, _ := strconv.Atoi(val)
	return level >= 0
}

// RecordFailure records a failed login attempt. Returns the lockout duration if locked.
func (l *LoginRateLimiter) RecordFailure(ctx context.Context, ip, email string) time.Duration {
	if l.redis != nil {
		return l.recordFailureRedis(ctx, ip, email)
	}
	return l.recordFailureMemory(ip, email)
}

func (l *LoginRateLimiter) recordFailureRedis(ctx context.Context, ip, email string) time.Duration {
	attemptK := "login_attempts:" + ip + ":" + email
	lockoutK := "login_lockout:" + ip + ":" + email

	count, err := l.redis.Incr(ctx, attemptK).Result()
	if err != nil {
		slog.Warn("login rate limiter: redis incr failed", "error", err)
		return 0
	}
	if count == 1 {
		l.redis.Expire(ctx, attemptK, l.window)
	}

	if int(count) == l.maxAttempts {
		duration := l.lockoutDurations[0]
		l.redis.Set(ctx, lockoutK, "0", duration)
		return duration
	}

	if int(count) > l.maxAttempts {
		level := 0
		val, err := l.redis.Get(ctx, lockoutK).Result()
		if err == nil {
			level, _ = strconv.Atoi(val)
		}
		if level >= len(l.lockoutDurations) {
			level = len(l.lockoutDurations) - 1
		}
		duration := l.lockoutDurations[level]
		nextLevel := level + 1
		if nextLevel >= len(l.lockoutDurations) {
			nextLevel = len(l.lockoutDurations) - 1
		}
		l.redis.Set(ctx, lockoutK, strconv.Itoa(nextLevel), duration)
		return duration
	}

	return 0
}

func (l *LoginRateLimiter) recordFailureMemory(ip, email string) time.Duration {
	key := l.stateKey(ip, email)

	l.mu.Lock()
	defer l.mu.Unlock()

	state, exists := l.attempts[key]
	if !exists {
		state = &loginAttemptState{WindowStart: time.Now()}
		l.attempts[key] = state
	}

	if time.Since(state.WindowStart) > l.window {
		state.Count = 0
		state.WindowStart = time.Now()
		state.LockedUntil = time.Time{}
	}

	state.Count++

	if state.Count >= l.maxAttempts {
		level := state.LockoutLevel
		if level >= len(l.lockoutDurations) {
			level = len(l.lockoutDurations) - 1
		}
		duration := l.lockoutDurations[level]
		state.LockedUntil = time.Now().Add(duration)
		state.LockoutLevel = level + 1
		return duration
	}

	return 0
}

// RecordSuccess clears the failure state for an IP+email combination.
func (l *LoginRateLimiter) RecordSuccess(ctx context.Context, ip, email string) {
	if l.redis != nil {
		attemptK := "login_attempts:" + ip + ":" + email
		lockoutK := "login_lockout:" + ip + ":" + email
		pipe := l.redis.Pipeline()
		pipe.Del(ctx, attemptK)
		pipe.Del(ctx, lockoutK)
		_, _ = pipe.Exec(ctx)
		return
	}

	key := l.stateKey(ip, email)
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.attempts, key)
}

// GetRemainingLockout returns how long until the lockout expires.
func (l *LoginRateLimiter) GetRemainingLockout(ctx context.Context, ip, email string) time.Duration {
	if l.redis != nil {
		lockoutK := "login_lockout:" + ip + ":" + email
		ttl, err := l.redis.TTL(ctx, lockoutK).Result()
		if err != nil || ttl < 0 {
			return 0
		}
		return ttl
	}

	key := l.stateKey(ip, email)
	l.mu.Lock()
	defer l.mu.Unlock()

	state, exists := l.attempts[key]
	if !exists || state.LockedUntil.IsZero() {
		return 0
	}
	remaining := time.Until(state.LockedUntil)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// Middleware returns HTTP middleware that rate-limits login attempts.
func (l *LoginRateLimiter) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				next.ServeHTTP(w, r)
				return
			}

			var email string
			if r.Body != nil {
				// Cap the read so an oversized login body cannot exhaust memory.
				bodyBytes, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
				r.Body.Close()
				// Always restore the body so downstream handlers can still read it,
				// even if reading or unmarshalling failed.
				r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
				if err == nil {
					var input struct {
						Email string `json:"email"`
					}
					if json.Unmarshal(bodyBytes, &input) == nil {
						email = strings.TrimSpace(input.Email)
					}
				}
			}

			ip := extractIP(r)
			if email == "" {
				next.ServeHTTP(w, r)
				return
			}

			ctx := r.Context()

			if l.IsLocked(ctx, ip, email) {
				remaining := l.GetRemainingLockout(ctx, ip, email)
				w.Header().Set("Retry-After", strconv.FormatInt(int64(remaining.Seconds()), 10))
				response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"code":        "AUTH_006",
					"error":       "too many login attempts, please try again later",
					"retry_after": remaining.Seconds(),
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractIP extracts the client IP from RemoteAddr.
func extractIP(r *http.Request) string {
	ip := r.RemoteAddr
	if strings.HasPrefix(ip, "[") {
		if idx := strings.LastIndex(ip, "]:"); idx != -1 {
			ip = ip[1:idx]
		} else {
			ip = strings.Trim(ip, "[]")
		}
	} else if strings.Count(ip, ":") == 1 {
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			ip = ip[:idx]
		}
	}
	return ip
}

// PlanTier represents a subscription plan with rate limits.
type PlanTier string

const (
	PlanFree       PlanTier = "free"
	PlanPro        PlanTier = "pro"
	PlanTeam       PlanTier = "team"
	PlanEnterprise PlanTier = "enterprise"
)

// PlanLimits defines rate limits and quotas for a plan tier.
type PlanLimits struct {
	// Rate limiting
	RequestsPerMinute int `json:"requests_per_minute"`
	RequestsPerDay    int `json:"requests_per_day"`
	RequestsPerMonth  int `json:"requests_per_month"`
	BurstSize         int `json:"burst_size"`

	// Usage quotas
	TokensPerMonth   int     `json:"tokens_per_month"`
	TasksPerMonth    int     `json:"tasks_per_month"`
	ScansPerMonth    int     `json:"scans_per_month"`
	MonthlyBudgetUsd float64 `json:"monthly_budget_usd"`

	// Features
	MaxConcurrentTasks  int    `json:"max_concurrent_tasks"`
	MaxProjectMembers   int    `json:"max_project_members"`
	MaxProjects         int    `json:"max_projects"`
	MaxAgentsPerProject int    `json:"max_agents_per_project"`
	PriorityQueue       bool   `json:"priority_queue"`
	SupportSLA          string `json:"support_sla"`
}

// DefaultLimits returns the default limits for each plan tier.
func DefaultLimits() map[PlanTier]PlanLimits {
	return map[PlanTier]PlanLimits{
		PlanFree: {
			RequestsPerMinute:   30,
			RequestsPerDay:      500,
			RequestsPerMonth:    10000,
			BurstSize:           10,
			TokensPerMonth:      500000,
			TasksPerMonth:       50,
			ScansPerMonth:       100,
			MonthlyBudgetUsd:    5.0,
			MaxConcurrentTasks:  1,
			MaxProjectMembers:   3,
			MaxProjects:         1,
			MaxAgentsPerProject: 2,
			PriorityQueue:       false,
			SupportSLA:          "community",
		},
		PlanPro: {
			RequestsPerMinute:   120,
			RequestsPerDay:      5000,
			RequestsPerMonth:    100000,
			BurstSize:           30,
			TokensPerMonth:      5000000,
			TasksPerMonth:       500,
			ScansPerMonth:       1000,
			MonthlyBudgetUsd:    50.0,
			MaxConcurrentTasks:  5,
			MaxProjectMembers:   10,
			MaxProjects:         5,
			MaxAgentsPerProject: 10,
			PriorityQueue:       true,
			SupportSLA:          "email_48h",
		},
		PlanTeam: {
			RequestsPerMinute:   300,
			RequestsPerDay:      20000,
			RequestsPerMonth:    500000,
			BurstSize:           100,
			TokensPerMonth:      20000000,
			TasksPerMonth:       2000,
			ScansPerMonth:       5000,
			MonthlyBudgetUsd:    200.0,
			MaxConcurrentTasks:  15,
			MaxProjectMembers:   50,
			MaxProjects:         20,
			MaxAgentsPerProject: 25,
			PriorityQueue:       true,
			SupportSLA:          "priority_24h",
		},
		PlanEnterprise: {
			RequestsPerMinute:   1000,
			RequestsPerDay:      100000,
			RequestsPerMonth:    0,
			BurstSize:           500,
			TokensPerMonth:      0,
			TasksPerMonth:       0,
			ScansPerMonth:       0,
			MonthlyBudgetUsd:    0,
			MaxConcurrentTasks:  50,
			MaxProjectMembers:   0,
			MaxProjects:         0,
			MaxAgentsPerProject: 0,
			PriorityQueue:       true,
			SupportSLA:          "dedicated",
		},
	}
}

// UsageTracker tracks API usage per org per billing cycle.
type UsageTracker struct {
	client *redis.Client
}

// NewUsageTracker creates a new usage tracker backed by Redis.
func NewUsageTracker(client *redis.Client) *UsageTracker {
	return &UsageTracker{client: client}
}

// UsageKey generates a Redis key for tracking usage.
func (ut *UsageTracker) UsageKey(orgID string, metric string, period string) string {
	return fmt.Sprintf("usage:%s:%s:%s", orgID, metric, period)
}

// CurrentPeriod returns the current billing period string (YYYY-MM).
func CurrentPeriod() string {
	return time.Now().Format("2006-01")
}

// IncrementUsage atomically increments a usage counter and returns the new value.
func (ut *UsageTracker) IncrementUsage(ctx context.Context, orgID string, metric string, amount int64) (int64, error) {
	key := ut.UsageKey(orgID, metric, CurrentPeriod())
	val, err := ut.client.IncrBy(ctx, key, amount).Result()
	if err != nil {
		return 0, err
	}

	ut.client.Expire(ctx, key, 45*24*time.Hour)
	return val, nil
}

// GetUsage returns the current usage for a metric.
func (ut *UsageTracker) GetUsage(ctx context.Context, orgID string, metric string) (int64, error) {
	key := ut.UsageKey(orgID, metric, CurrentPeriod())
	val, err := ut.client.Get(ctx, key).Int64()
	if err == redis.Nil {
		return 0, nil
	}
	return val, err
}

// CheckQuota checks if an org has exceeded its quota for a given metric.
// Returns (allowed, remaining, error). If limit <= 0, it's unlimited.
func (ut *UsageTracker) CheckQuota(ctx context.Context, orgID string, metric string, limit int) (bool, int, error) {
	if limit <= 0 {
		return true, -1, nil
	}
	usage, err := ut.GetUsage(ctx, orgID, metric)
	if err != nil {
		return true, 0, err
	}
	rem := limit - int(usage)
	return rem > 0, rem, nil
}

// OrgPlanFunc extracts the org ID and plan from a request.
type OrgPlanFunc func(r *http.Request) (orgID string, plan PlanTier)

// PlanAwareRateLimiter provides plan-based rate limiting with usage metering.
type PlanAwareRateLimiter struct {
	client  *redis.Client
	limits  map[PlanTier]PlanLimits
	tracker *UsageTracker
	mu      sync.RWMutex
}

// NewPlanAwareRateLimiter creates a new plan-aware rate limiter.
func NewPlanAwareRateLimiter(client *redis.Client) *PlanAwareRateLimiter {
	return &PlanAwareRateLimiter{
		client:  client,
		limits:  DefaultLimits(),
		tracker: NewUsageTracker(client),
	}
}

// SetLimits overrides the default limits for a specific plan.
func (p *PlanAwareRateLimiter) SetLimits(tier PlanTier, limits PlanLimits) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.limits[tier] = limits
}

// GetLimits returns the limits for a plan tier.
func (p *PlanAwareRateLimiter) GetLimits(tier PlanTier) PlanLimits {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if l, ok := p.limits[tier]; ok {
		return l
	}
	return p.limits[PlanFree]
}

// Middleware returns a chi-compatible middleware that enforces plan-based rate limits.
func (p *PlanAwareRateLimiter) Middleware(orgPlanFn OrgPlanFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			orgID, plan := orgPlanFn(r)
			if orgID == "" {
				orgID = "anonymous"
				plan = PlanFree
			}

			limits := p.GetLimits(plan)

			minuteKey := fmt.Sprintf("ratelimit:org:%s:%d", orgID, time.Now().Unix()/60)
			allowed, count := p.checkMinuteLimit(r.Context(), minuteKey, int64(limits.RequestsPerMinute))
			if !allowed {
				retryAfter := time.Until(time.Now().Truncate(time.Minute).Add(time.Minute))
				w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limits.RequestsPerMinute))
				w.Header().Set("X-RateLimit-Remaining", "0")
				w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(retryAfter).Unix(), 10))
				w.Header().Set("Retry-After", strconv.Itoa(int(retryAfter.Seconds())+1))
				w.Header().Set("X-RateLimit-Plan", string(plan))
				response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"code":        "RATE_001",
					"error":       "rate limit exceeded",
					"plan":        plan,
					"retry_after": int(retryAfter.Seconds()) + 1,
				})
				return
			}

			if limits.RequestsPerDay > 0 && p.client != nil {
				dayKey := fmt.Sprintf("ratelimit:org:%s:day:%s", orgID, time.Now().Format("2006-01-02"))
				// The pre-read is only a fast path; the Lua script below is the
				// atomic decision point (it re-checks under the same lock Redis
				// serializes, closing the check-then-increment race).
				current, _ := p.client.Get(r.Context(), dayKey).Int64()
				if int(current) >= limits.RequestsPerDay {
					p.quotaExceeded(w, plan)
					return
				}

				// Returns {newval, incremented}: incremented=0 means the counter
				// was already at/over the limit and was NOT incremented (deny).
				// incremented=1 means this request was counted, so even newval ==
				// limit is allowed (the exact limit-th request). Comparing counts
				// alone cannot distinguish these two cases, which is why the
				// original `count > limit` check let limit+1 requests through and
				// a plain `>=` check would wrongly deny the limit-th request.
				luaScript := redis.NewScript(`
					local current = redis.call('GET', KEYS[1])
					if current == false then
						current = 0
					else
						current = tonumber(current)
					end
					if current >= tonumber(ARGV[1]) then
						return {current, 0}
					end
					local newval = redis.call('INCR', KEYS[1])
					if newval == 1 then
						redis.call('EXPIRE', KEYS[1], ARGV[2])
					end
					return {newval, 1}
				`)
				vals, err := luaScript.Run(r.Context(), p.client, []string{dayKey}, limits.RequestsPerDay, int(25*time.Hour/time.Second)).Int64Slice()
				if err != nil {
					slog.Warn("daily quota check failed, allowing request", "error", err)
				} else if len(vals) < 2 || vals[1] == 0 {
					p.quotaExceeded(w, plan)
					return
				}
			}

			resetTime := time.Now().Truncate(time.Minute).Add(time.Minute).Unix()
			w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limits.RequestsPerMinute))
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(max(0, int64(limits.RequestsPerMinute)-count), 10))
			w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(resetTime, 10))
			w.Header().Set("X-RateLimit-Plan", string(plan))

			next.ServeHTTP(w, r)
		})
	}
}

// quotaExceeded writes the RATE_002 daily-quota response.
func (p *PlanAwareRateLimiter) quotaExceeded(w http.ResponseWriter, plan PlanTier) {
	response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
		"code":    "RATE_002",
		"error":   "daily quota exceeded",
		"plan":    plan,
		"message": "Upgrade your plan for more daily requests",
	})
}

// checkMinuteLimit uses simple INCR + EXPIRE for minute-based rate limiting.
func (p *PlanAwareRateLimiter) checkMinuteLimit(ctx context.Context, key string, limit int64) (bool, int64) {
	if p.client == nil {
		return true, 0
	}
	count, err := p.client.Incr(ctx, key).Result()
	if err != nil {
		slog.Warn("rate limit check failed, allowing request", "error", err)
		return true, 0
	}
	if count == 1 {
		p.client.Expire(ctx, key, 65*time.Second)
	}
	return count <= limit, count
}

// UsageMeteringMiddleware tracks API usage per org for billing.
type UsageMeteringMiddleware struct {
	tracker *UsageTracker
}

// NewUsageMeteringMiddleware creates a new usage metering middleware.
func NewUsageMeteringMiddleware(client *redis.Client) *UsageMeteringMiddleware {
	return &UsageMeteringMiddleware{
		tracker: NewUsageTracker(client),
	}
}

// Middleware returns middleware that tracks request count per org.
func (u *UsageMeteringMiddleware) Middleware(orgPlanFn OrgPlanFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			orgID, _ := orgPlanFn(r)

			ww := &statusWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(ww, r)

			if ww.status < 400 && orgID != "" {
				if _, err := u.tracker.IncrementUsage(r.Context(), orgID, "api_requests", 1); err != nil {
					slog.Warn("usage metering failed", "org_id", orgID, "error", err)
				}
			}
		})
	}
}

// statusWriter wraps ResponseWriter to capture the status code.
// Implements http.Flusher and http.Hijacker to support SSE streaming and WebSocket.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

// Unwrap returns the underlying ResponseWriter for io.WriterTo support.
func (w *statusWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush implements http.Flusher for SSE streaming support.
func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Hijack implements http.Hijacker for WebSocket upgrade support.
func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, fmt.Errorf("underlying ResponseWriter does not implement http.Hijacker")
}

// QuotaEnforcer checks org-level usage quotas before processing requests.
type QuotaEnforcer struct {
	tracker *UsageTracker
	limits  map[PlanTier]PlanLimits
	mu      sync.RWMutex
}

// NewQuotaEnforcer creates a new quota enforcer.
func NewQuotaEnforcer(client *redis.Client) *QuotaEnforcer {
	return &QuotaEnforcer{
		tracker: NewUsageTracker(client),
		limits:  DefaultLimits(),
	}
}

// SetLimits overrides the default limits for a specific plan.
func (q *QuotaEnforcer) SetLimits(tier PlanTier, limits PlanLimits) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.limits[tier] = limits
}

// CheckTasksQuota checks if an org can create more tasks.
func (q *QuotaEnforcer) CheckTasksQuota(ctx context.Context, orgID string, plan PlanTier) (bool, int, error) {
	q.mu.RLock()
	limits := q.limits[plan]
	q.mu.RUnlock()
	return q.tracker.CheckQuota(ctx, orgID, "tasks", limits.TasksPerMonth)
}

// CheckTokensQuota checks if an org has token budget remaining.
func (q *QuotaEnforcer) CheckTokensQuota(ctx context.Context, orgID string, plan PlanTier) (bool, int, error) {
	q.mu.RLock()
	limits := q.limits[plan]
	q.mu.RUnlock()
	return q.tracker.CheckQuota(ctx, orgID, "tokens", limits.TokensPerMonth)
}

// CheckScansQuota checks if an org can run more scans.
func (q *QuotaEnforcer) CheckScansQuota(ctx context.Context, orgID string, plan PlanTier) (bool, int, error) {
	q.mu.RLock()
	limits := q.limits[plan]
	q.mu.RUnlock()
	return q.tracker.CheckQuota(ctx, orgID, "scans", limits.ScansPerMonth)
}

// Middleware returns middleware that checks task quotas only on task creation.
// Only enforces quotas on POST /tasks — reads (GET) are allowed through.
func (q *QuotaEnforcer) Middleware(orgPlanFn OrgPlanFunc) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

			if r.Method != http.MethodPost || !strings.Contains(r.URL.Path, "/tasks") {
				next.ServeHTTP(w, r)
				return
			}

			orgID, plan := orgPlanFn(r)
			if orgID == "" {
				next.ServeHTTP(w, r)
				return
			}

			allowed, remaining, err := q.CheckTasksQuota(r.Context(), orgID, plan)
			if err != nil {
				slog.Warn("quota check failed, allowing request", "org_id", orgID, "error", err)
				next.ServeHTTP(w, r)
				return
			}

			w.Header().Set("X-Quota-Tasks-Remaining", strconv.Itoa(remaining))
			w.Header().Set("X-Quota-Plan", string(plan))

			if !allowed {
				response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"code":    "QUOTA_001",
					"error":   "monthly task quota exceeded",
					"plan":    plan,
					"message": "Upgrade your plan for more tasks per month",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
