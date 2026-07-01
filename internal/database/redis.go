package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/internal/config"
)

// Redis wraps the go-redis client.
type Redis struct {
	Client *redis.Client
}

// NewRedis creates a new Redis client connection.
func NewRedis(ctx context.Context, cfg *config.RedisConfig) (*Redis, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Address(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     10,
		MinIdleConns: 5,
	})

	// Verify connectivity
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		return nil, fmt.Errorf("unable to connect to redis: %w", err)
	}

	slog.Info("connected to redis",
		"addr", cfg.Address(),
		"db", cfg.DB,
	)

	return &Redis{Client: client}, nil
}

// HealthCheck pings Redis to verify connectivity.
func (r *Redis) HealthCheck(ctx context.Context) error {
	return r.Client.Ping(ctx).Err()
}

// Close closes the Redis client connection.
func (r *Redis) Close() {
	if r.Client != nil {
		r.Client.Close()
		slog.Info("redis connection closed")
	}
}
