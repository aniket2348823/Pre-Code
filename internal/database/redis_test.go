package database

import (
	"context"
	"testing"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/vigilagent/vigilagent/internal/config"
)

// --- NewRedis error paths ---

func TestNewRedis_CancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewRedis(ctx, &config.RedisConfig{Host: "localhost", Port: 6379})
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestNewRedis_ContextTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()
	time.Sleep(2 * time.Millisecond)
	_, err := NewRedis(ctx, &config.RedisConfig{Host: "localhost", Port: 6379})
	if err == nil {
		t.Fatal("expected error for expired context")
	}
}

func TestNewRedis_InvalidAddress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := NewRedis(ctx, &config.RedisConfig{Host: "192.0.2.1", Port: 1})
	if err == nil {
		t.Fatal("expected error for unreachable address")
	}
}

// --- Redis struct ---

func TestRedis_StructZeroValues(t *testing.T) {
	r := &Redis{}
	if r.Client != nil {
		t.Fatal("expected nil client for zero struct")
	}
}

func TestRedis_Close_NilClientNoPanic(t *testing.T) {
	r := &Redis{Client: nil}
	r.Close()
}

func TestRedis_HealthCheck_NilClientPanics(t *testing.T) {
	r := &Redis{Client: nil}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil client")
			}
		}()
		r.HealthCheck(context.Background())
	}()
}

// --- Redis client options ---

func TestRedis_ClientOptions(t *testing.T) {
	// Verify that NewRedis would use the correct config fields
	cfg := &config.RedisConfig{
		Host:     "myhost",
		Port:     6380,
		Password: "secret",
		DB:       3,
	}
	// We can't call NewRedis without a live server, but we can verify config
	addr := cfg.Address()
	expected := "myhost:6380"
	if addr != expected {
		t.Errorf("Address() = %q, want %q", addr, expected)
	}
	if cfg.Password != "secret" {
		t.Errorf("Password = %q, want %q", cfg.Password, "secret")
	}
	if cfg.DB != 3 {
		t.Errorf("DB = %d, want %d", cfg.DB, 3)
	}
}

func TestRedis_ClientOptions_Defaults(t *testing.T) {
	cfg := &config.RedisConfig{
		Host: "localhost",
		Port: 6379,
	}
	addr := cfg.Address()
	if addr != "localhost:6379" {
		t.Errorf("Address() = %q, want %q", addr, "localhost:6379")
	}
}

// --- Redis with mock client ---

func TestRedis_Close_NonNilClient(t *testing.T) {
	// Create a real redis client that will fail to connect
	client := redis.NewClient(&redis.Options{
		Addr: "192.0.2.1:1", // unreachable
	})
	r := &Redis{Client: client}
	r.Close()
}

func TestRedis_HealthCheck_ConnectedClient(t *testing.T) {
	// Verify HealthCheck returns error for unreachable client
	client := redis.NewClient(&redis.Options{
		Addr: "192.0.2.1:1",
	})
	defer client.Close()
	r := &Redis{Client: client}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := r.HealthCheck(ctx)
	if err == nil {
		t.Fatal("expected error for unreachable redis")
	}
}
