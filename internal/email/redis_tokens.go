package email

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisTokenStore persists verification tokens in Redis for durability across restarts.
type RedisTokenStore struct {
	client     *redis.Client
	prefix     string
	tokenTTL   time.Duration
}

// NewRedisTokenStore creates a Redis-backed token store.
func NewRedisTokenStore(client *redis.Client, tokenTTL time.Duration) *RedisTokenStore {
	return &RedisTokenStore{
		client:   client,
		prefix:   "email:token:",
		tokenTTL: tokenTTL,
	}
}

func (r *RedisTokenStore) key(token string) string {
	return r.prefix + token
}

// Store saves a token to Redis with automatic expiry.
func (r *RedisTokenStore) Store(ctx context.Context, vt *VerificationToken) error {
	data, err := json.Marshal(vt)
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}
	return r.client.Set(ctx, r.key(vt.Token), data, r.tokenTTL).Err()
}

// Get retrieves a token from Redis. Returns nil if not found or expired.
func (r *RedisTokenStore) Get(ctx context.Context, token string) (*VerificationToken, bool) {
	data, err := r.client.Get(ctx, r.key(token)).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, false
		}
		slog.Warn("redis token store: failed to get token", "error", err)
		return nil, false
	}

	var vt VerificationToken
	if err := json.Unmarshal(data, &vt); err != nil {
		slog.Warn("redis token store: failed to unmarshal token", "error", err)
		return nil, false
	}

	return &vt, true
}

// Delete removes a token from Redis.
func (r *RedisTokenStore) Delete(ctx context.Context, token string) error {
	return r.client.Del(ctx, r.key(token)).Err()
}

// Cleanup removes expired tokens (Redis handles this automatically via TTL, but this
// provides manual cleanup for bulk operations).
func (r *RedisTokenStore) Cleanup(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			// Redis TTL handles expiry automatically — no-op here
			// Could add SCAN-based cleanup for orphaned keys if needed
		}
	}
}
