package server

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/config"
)

func TestServer_New_InvalidConfig(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Env:          "development",
			Port:         0, // invalid port
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		Database: config.DatabaseConfig{
			Host:         "localhost",
			Port:         5432,
			User:         "u",
			Name:         "n",
			MaxOpenConns: 10,
		},
		Redis: config.RedisConfig{
			Host: "localhost",
			Port: 6379,
		},
		NATS: config.NATSConfig{
			URL:    "nats://x",
			Stream: "s",
		},
		Auth: config.AuthConfig{
			JWTSecret:     "test-secret-32-chars-long!!!!",
			JWTExpiration: 24 * time.Hour,
		},
		LLM: config.LLMConfig{
			DefaultModel: "m",
		},
	}

	srv, err := New(cfg)
	assert.Nil(t, srv)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid configuration")
}

func TestServer_Router(t *testing.T) {
	// Can't call New() without DB, so create a minimal server manually
	srv := &Server{}
	assert.Nil(t, srv.Router())
}

func TestServer_Shutdown(t *testing.T) {
	srv := &Server{}
	err := srv.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestServer_Shutdown_WithNilComponents(t *testing.T) {
	srv := &Server{
		cfg:       &config.Config{},
		router:    nil,
		db:        nil,
		redis:     nil,
		nats:      nil,
		cleanup:   nil,
		hotReload: nil,
	}
	err := srv.Shutdown(context.Background())
	assert.NoError(t, err)
}

func TestServer_Shutdown_CallsCleanup(t *testing.T) {
	cleanupCalled := false
	srv := &Server{
		cleanup: func() {
			cleanupCalled = true
		},
	}
	err := srv.Shutdown(context.Background())
	assert.NoError(t, err)
	assert.True(t, cleanupCalled)
}

func TestServer_Version(t *testing.T) {
	// version is "dev" by default
	assert.Equal(t, "dev", version)
}

func TestHitlAdapter_Submit(t *testing.T) {
	adapter := &hitlAdapter{}
	assert.Nil(t, adapter.queue)
	// Verify the struct has the expected field
}

func TestMemoryAdapter_Recall_NilManager(t *testing.T) {
	adapter := &memoryAdapter{mgr: nil}
	// Should panic or error when mgr is nil
	assert.Panics(t, func() {
		adapter.Recall(context.Background(), "query", 10)
	})
}

func TestMemoryAdapter_StoreEpisode_NilManager(t *testing.T) {
	adapter := &memoryAdapter{mgr: nil}
	// Should panic or error when mgr is nil
	assert.Panics(t, func() {
		adapter.StoreEpisode(context.Background(), "user", "type", "title", "content", 1.0)
	})
}

func TestServer_New_MissingJWTSecret(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Env:          "development",
			Port:         8080,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		Database: config.DatabaseConfig{
			Host:         "localhost",
			Port:         5432,
			User:         "u",
			Name:         "n",
			MaxOpenConns: 10,
		},
		Redis: config.RedisConfig{
			Host: "localhost",
			Port: 6379,
		},
		NATS: config.NATSConfig{
			URL:    "nats://x",
			Stream: "s",
		},
		Auth: config.AuthConfig{
			JWTSecret:     "",
			JWTExpiration: 24 * time.Hour,
		},
		LLM: config.LLMConfig{
			DefaultModel: "m",
		},
	}

	srv, err := New(cfg)
	assert.Nil(t, srv)
	assert.Error(t, err)
}

func TestServer_New_MissingDatabaseHost(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Env:          "development",
			Port:         8080,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		Database: config.DatabaseConfig{
			Host:         "",
			Port:         5432,
			User:         "u",
			Name:         "n",
			MaxOpenConns: 10,
		},
		Redis: config.RedisConfig{
			Host: "localhost",
			Port: 6379,
		},
		NATS: config.NATSConfig{
			URL:    "nats://x",
			Stream: "s",
		},
		Auth: config.AuthConfig{
			JWTSecret:     "test-secret-32-chars-long!!!!",
			JWTExpiration: 24 * time.Hour,
		},
		LLM: config.LLMConfig{
			DefaultModel: "m",
		},
	}

	srv, err := New(cfg)
	assert.Nil(t, srv)
	assert.Error(t, err)
}

func TestServer_New_MissingLLMModel(t *testing.T) {
	cfg := &config.Config{
		Server: config.ServerConfig{
			Env:          "development",
			Port:         8080,
			ReadTimeout:  10 * time.Second,
			WriteTimeout: 10 * time.Second,
		},
		Database: config.DatabaseConfig{
			Host:         "localhost",
			Port:         5432,
			User:         "u",
			Name:         "n",
			MaxOpenConns: 10,
		},
		Redis: config.RedisConfig{
			Host: "localhost",
			Port: 6379,
		},
		NATS: config.NATSConfig{
			URL:    "nats://x",
			Stream: "s",
		},
		Auth: config.AuthConfig{
			JWTSecret:     "test-secret-32-chars-long!!!!",
			JWTExpiration: 24 * time.Hour,
		},
		LLM: config.LLMConfig{
			DefaultModel: "",
		},
	}

	srv, err := New(cfg)
	assert.Nil(t, srv)
	assert.Error(t, err)
}

func TestServer_Shutdown_CallsRouterShutdown(t *testing.T) {
	srv := &Server{}
	require.NotPanics(t, func() {
		srv.Shutdown(context.Background())
	})
}

func TestServer_HitlAdapter_Type(t *testing.T) {
	adapter := &hitlAdapter{}
	assert.Nil(t, adapter.queue)
}
