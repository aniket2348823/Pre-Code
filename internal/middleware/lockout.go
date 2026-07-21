package middleware

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// Lockout is the interface for account lockout implementations.
type Lockout interface {
	IsLocked(ctx context.Context, identifier string) bool
	RecordFailure(ctx context.Context, identifier string)
	RecordSuccess(ctx context.Context, identifier string)
	GetRemainingLockout(ctx context.Context, identifier string) time.Duration
}

// --- In-Memory Implementation ---

// AccountLockout tracks failed login attempts per account (in-memory).
type AccountLockout struct {
	mu              sync.Mutex
	attempts        map[string]*lockoutState
	maxAttempts     int
	lockoutDuration time.Duration
}

type lockoutState struct {
	FailedAttempts int
	LockedUntil    time.Time
	LastAttempt    time.Time
}

// NewAccountLockout creates a new in-memory account lockout manager.
func NewAccountLockout(maxAttempts int, lockoutDuration time.Duration) *AccountLockout {
	return &AccountLockout{
		attempts:        make(map[string]*lockoutState),
		maxAttempts:     maxAttempts,
		lockoutDuration: lockoutDuration,
	}
}

func (al *AccountLockout) IsLocked(_ context.Context, identifier string) bool {
	al.mu.Lock()
	defer al.mu.Unlock()

	state, exists := al.attempts[identifier]
	if !exists {
		return false
	}

	if !state.LockedUntil.IsZero() && time.Now().Before(state.LockedUntil) {
		return true
	}

	if !state.LockedUntil.IsZero() && time.Now().After(state.LockedUntil) {
		state.FailedAttempts = 0
		state.LockedUntil = time.Time{}
	}

	return false
}

func (al *AccountLockout) RecordFailure(_ context.Context, identifier string) {
	al.mu.Lock()
	defer al.mu.Unlock()

	state, exists := al.attempts[identifier]
	if !exists {
		state = &lockoutState{}
		al.attempts[identifier] = state
	}

	state.FailedAttempts++
	state.LastAttempt = time.Now()

	if state.FailedAttempts >= al.maxAttempts {
		state.LockedUntil = time.Now().Add(al.lockoutDuration)
	}
}

func (al *AccountLockout) RecordSuccess(_ context.Context, identifier string) {
	al.mu.Lock()
	defer al.mu.Unlock()
	delete(al.attempts, identifier)
}

func (al *AccountLockout) GetRemainingLockout(_ context.Context, identifier string) time.Duration {
	al.mu.Lock()
	defer al.mu.Unlock()

	state, exists := al.attempts[identifier]
	if !exists || state.LockedUntil.IsZero() {
		return 0
	}

	remaining := time.Until(state.LockedUntil)
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (al *AccountLockout) Cleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			al.mu.Lock()
			now := time.Now()
			for id, state := range al.attempts {
				if !state.LockedUntil.IsZero() && now.After(state.LockedUntil.Add(al.lockoutDuration)) {
					delete(al.attempts, id)
				}
			}
			al.mu.Unlock()
		}
	}
}

// --- Redis-Backed Implementation ---

// RedisAccountLockout provides distributed account lockout backed by Redis.
type RedisAccountLockout struct {
	client          *redis.Client
	maxAttempts     int
	lockoutDuration time.Duration
	attemptWindow   time.Duration
}

// NewRedisAccountLockout creates a new Redis-backed account lockout manager.
func NewRedisAccountLockout(client *redis.Client, maxAttempts int, lockoutDuration time.Duration) *RedisAccountLockout {
	return &RedisAccountLockout{
		client:          client,
		maxAttempts:     maxAttempts,
		lockoutDuration: lockoutDuration,
		attemptWindow:   lockoutDuration * 2,
	}
}

func (al *RedisAccountLockout) lockoutKey(identifier string) string {
	return "lockout:" + identifier
}

func (al *RedisAccountLockout) attemptKey(identifier string) string {
	return "attempts:" + identifier
}

func (al *RedisAccountLockout) IsLocked(ctx context.Context, identifier string) bool {
	val, err := al.client.Get(ctx, al.lockoutKey(identifier)).Result()
	if err != nil {
		return false
	}
	locked, _ := strconv.ParseBool(val)
	return locked
}

func (al *RedisAccountLockout) RecordFailure(ctx context.Context, identifier string) {
	attemptK := al.attemptKey(identifier)

	count, err := al.client.Incr(ctx, attemptK).Result()
	if err != nil {
		slog.Warn("redis lockout: failed to record failure", "error", err)
		return
	}

	if count == 1 {
		al.client.Expire(ctx, attemptK, al.attemptWindow)
	}

	if int(count) >= al.maxAttempts {
		if err := al.client.Set(ctx, al.lockoutKey(identifier), "true", al.lockoutDuration).Err(); err != nil {
			slog.Warn("redis lockout: failed to lock account", "error", err)
		}
		al.client.Del(ctx, attemptK)
	}
}

func (al *RedisAccountLockout) RecordSuccess(ctx context.Context, identifier string) {
	pipe := al.client.Pipeline()
	pipe.Del(ctx, al.attemptKey(identifier))
	pipe.Del(ctx, al.lockoutKey(identifier))
	_, _ = pipe.Exec(ctx)
}

func (al *RedisAccountLockout) GetRemainingLockout(ctx context.Context, identifier string) time.Duration {
	ttl, err := al.client.TTL(ctx, al.lockoutKey(identifier)).Result()
	if err != nil || ttl < 0 {
		return 0
	}
	return ttl
}

// --- Factory ---

// NewLockout returns the best available lockout implementation.
// If a Redis client is provided, returns a distributed Redis-backed lockout.
// Otherwise returns the in-memory implementation.
func NewLockout(redisClient *redis.Client, maxAttempts int, lockoutDuration time.Duration) Lockout {
	if redisClient != nil {
		return NewRedisAccountLockout(redisClient, maxAttempts, lockoutDuration)
	}
	return NewAccountLockout(maxAttempts, lockoutDuration)
}

// --- HTTP Middleware ---

// LockoutMiddleware returns HTTP middleware that blocks locked accounts.
func LockoutMiddleware(lockout Lockout, keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := keyFunc(r)
			ctx := r.Context()

			if lockout.IsLocked(ctx, key) {
				remaining := lockout.GetRemainingLockout(ctx, key)
				w.Header().Set("Retry-After", fmt.Sprintf("%.0f", remaining.Seconds()))
				response.JSON(w, http.StatusTooManyRequests, map[string]interface{}{
					"code":       "AUTH_005",
					"error":      "account locked due to too many failed attempts",
					"retry_after": remaining.Seconds(),
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
