package middleware

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// LoginRateLimiter provides per-IP + per-email rate limiting with progressive lockout.
type LoginRateLimiter struct {
	mu      sync.Mutex
	attempts map[string]*loginAttemptState

	redis *redis.Client

	maxAttempts       int
	window            time.Duration
	lockoutDurations  []time.Duration
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
		attempts:          make(map[string]*loginAttemptState),
		redis:             redisClient,
		maxAttempts:       cfg.MaxAttempts,
		window:            cfg.Window,
		lockoutDurations:  cfg.LockoutDurations,
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
		return false
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
				bodyBytes, err := io.ReadAll(r.Body)
				r.Body.Close()
				if err == nil {
					var input struct {
						Email string `json:"email"`
					}
					if json.Unmarshal(bodyBytes, &input) == nil {
						email = strings.TrimSpace(input.Email)
					}
					r.Body = io.NopCloser(bytes.NewReader(bodyBytes))
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
	} else if strings.Count(ip, ":") > 1 {
		// IPv6 without brackets
	} else {
		if idx := strings.LastIndex(ip, ":"); idx != -1 {
			ip = ip[:idx]
		}
	}
	return ip
}
