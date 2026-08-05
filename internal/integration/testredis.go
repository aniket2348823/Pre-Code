package integration

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
)

// TestRedis wraps a real Redis client with cleanup for testing.
type TestRedis struct {
	Client  *redis.Client
	cleanup func()
}

// SetupTestRedis connects to Redis and returns a TestRedis with cleanup.
// Skips if INTEGRATION_TEST != "1" or Redis is unavailable.
func SetupTestRedis(t *testing.T) *TestRedis {
	t.Helper()

	if os.Getenv("INTEGRATION_TEST") != "1" {
		t.Skip("INTEGRATION_TEST not set to \"1\", skipping integration test")
	}

	addr := os.Getenv("TEST_REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}

	password := os.Getenv("TEST_REDIS_PASSWORD")
	db := 0
	if dbStr := os.Getenv("TEST_REDIS_DB"); dbStr != "" {
		fmt.Sscanf(dbStr, "%d", &db)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client := redis.NewClient(&redis.Options{
		Addr:         addr,
		Password:     password,
		DB:           db,
		DialTimeout:  3 * time.Second,
		ReadTimeout:  2 * time.Second,
		WriteTimeout: 2 * time.Second,
		PoolSize:     5,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		t.Skipf("Redis not available at %s: %v", addr, err)
	}

	cleanup := func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		client.FlushDB(ctx)
		client.Close()
	}

	return &TestRedis{Client: client, cleanup: cleanup}
}

// Close calls the cleanup function.
func (r *TestRedis) Close() {
	if r.cleanup != nil {
		r.cleanup()
	}
}
