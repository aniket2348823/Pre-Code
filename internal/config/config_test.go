package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	if cfg.Server.Host != "0.0.0.0" {
		t.Errorf("expected default server host '0.0.0.0', got %q", cfg.Server.Host)
	}
	if cfg.Server.Port != 8080 {
		t.Errorf("expected default server port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Server.Env == "" {
		t.Error("expected env to be set")
	}
	if cfg.Database.Host == "" {
		t.Error("expected database host to be set")
	}
	if cfg.Database.Port != 5432 {
		t.Errorf("expected default database port 5432, got %d", cfg.Database.Port)
	}
	if cfg.Database.User != "vigilagent" {
		t.Errorf("expected default database user 'vigilagent', got %q", cfg.Database.User)
	}
	if cfg.Redis.Host == "" {
		t.Error("expected redis host to be set")
	}
	if cfg.Redis.Port != 6379 {
		t.Errorf("expected default redis port 6379, got %d", cfg.Redis.Port)
	}
	if cfg.NATS.URL == "" {
		t.Error("expected NATS URL to be set")
	}
	if cfg.Auth.APIKeyPrefix != "va_" {
		t.Errorf("expected default API key prefix 'va_', got %q", cfg.Auth.APIKeyPrefix)
	}
	if cfg.Auth.JWTExpiration != 24*time.Hour {
		t.Errorf("expected default JWT expiration 24h, got %v", cfg.Auth.JWTExpiration)
	}
	// New pool/retry defaults
	if cfg.Database.PoolMaxOpen != 25 {
		t.Errorf("expected default pool_max_open 25, got %d", cfg.Database.PoolMaxOpen)
	}
	if cfg.Database.PoolMaxIdle != 5 {
		t.Errorf("expected default pool_max_idle 5, got %d", cfg.Database.PoolMaxIdle)
	}
	if cfg.Database.PoolMaxLifetime != 5*time.Minute {
		t.Errorf("expected default pool_max_lifetime 5m, got %v", cfg.Database.PoolMaxLifetime)
	}
	if cfg.Database.PoolMaxIdleTime != 3*time.Minute {
		t.Errorf("expected default pool_max_idle_time 3m, got %v", cfg.Database.PoolMaxIdleTime)
	}
	if cfg.Database.SlowQueryThreshold != 100*time.Millisecond {
		t.Errorf("expected default slow_query_threshold 100ms, got %v", cfg.Database.SlowQueryThreshold)
	}
	if cfg.Database.RetryMaxAttempts != 3 {
		t.Errorf("expected default retry_max_attempts 3, got %d", cfg.Database.RetryMaxAttempts)
	}
	if cfg.Database.PoolStatsInterval != 30*time.Second {
		t.Errorf("expected default pool_stats_interval 30s, got %v", cfg.Database.PoolStatsInterval)
	}
}

func TestDatabaseConfig_DSN(t *testing.T) {
	cfg := &DatabaseConfig{
		Host:     "myhost",
		Port:     5432,
		User:     "myuser",
		Password: "mypass",
		Name:     "mydb",
		SSLMode:  "disable",
	}

	dsn := cfg.DSN()
	expected := "host=myhost port=5432 user=myuser password=mypass dbname=mydb sslmode=disable"
	if dsn != expected {
		t.Errorf("DSN() = %q, want %q", dsn, expected)
	}
}

func TestRedisConfig_Address(t *testing.T) {
	cfg := &RedisConfig{
		Host: "redis.example.com",
		Port: 6380,
	}

	addr := cfg.Address()
	expected := "redis.example.com:6380"
	if addr != expected {
		t.Errorf("Address() = %q, want %q", addr, expected)
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid config",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigilagent", Name: "vigilagent", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
		Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "gpt-4o"},
		},
		wantErr: false,
	},
	{
		name: "missing database host",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{User: "vigilagent", Name: "vigilagent", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "test-secret-32-chars-long!!!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
			},
			wantErr: true,
			errMsg:  "database.host is required",
		},
		{
			name: "missing database user",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Name: "vigilagent", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "test-secret-32-chars-long!!!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
			},
			wantErr: true,
			errMsg:  "database.user is required",
		},
		{
			name: "missing database name",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", User: "vigilagent", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "test-secret-32-chars-long!!!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
			},
			wantErr: true,
			errMsg:  "database.name is required",
		},
		{
			name: "production with default jwt secret",
			cfg: Config{
				Server:   ServerConfig{Env: "production", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o", OpenAIKey: "sk-test"},
			},
			wantErr: true,
			errMsg:  "auth.jwt_secret must be changed from default value",
		},
		{
			name: "default jwt secret rejected in development",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
			},
			wantErr: true,
			errMsg:  "auth.jwt_secret must be changed from default value",
		},
		{
			name: "secret jwt rejected in development",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "secret", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
			},
			wantErr: true,
			errMsg:  "auth.jwt_secret must not be a common weak secret",
		},
		{
			name: "default jwt rejected in development",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "default", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
			},
			wantErr: true,
			errMsg:  "auth.jwt_secret must not be a common weak secret",
		},
		{
			name: "short jwt secret rejected in development",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "short", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
			},
			wantErr: true,
			errMsg:  "auth.jwt_secret must be at least 32 characters",
		},
		{
			name: "production with real jwt secret",
			cfg: Config{
				Server:   ServerConfig{Env: "production", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o", OpenAIKey: "sk-test"},
				CORS:     CORSConfig{AllowedOrigins: []string{"https://app.example.com"}},
			},
			wantErr: false,
		},
		{
			name: "CORS invalid origin format - no scheme",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
				CORS:     CORSConfig{AllowedOrigins: []string{"localhost:3000"}},
			},
			wantErr: true,
			errMsg:  `cors.allowed_origins: "localhost:3000" is not a valid origin (must start with http:// or https://)`,
		},
		{
			name: "CORS invalid origin format - bare domain",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
				CORS:     CORSConfig{AllowedOrigins: []string{"example.com"}},
			},
			wantErr: true,
			errMsg:  `cors.allowed_origins: "example.com" is not a valid origin (must start with http:// or https://)`,
		},
		{
			name: "CORS valid https origin",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
				CORS:     CORSConfig{AllowedOrigins: []string{"https://app.example.com"}},
			},
			wantErr: false,
		},
		{
			name: "CORS valid http origin",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
				CORS:     CORSConfig{AllowedOrigins: []string{"http://localhost:3000"}},
			},
			wantErr: false,
		},
		{
			name: "CORS wildcard allowed in development",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
				CORS:     CORSConfig{AllowedOrigins: []string{"*"}},
			},
			wantErr: false,
		},
		{
			name: "CORS wildcard rejected in production",
			cfg: Config{
				Server:   ServerConfig{Env: "production", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o", OpenAIKey: "sk-test"},
				CORS:     CORSConfig{AllowedOrigins: []string{"*"}},
			},
			wantErr: true,
			errMsg:  "cors.allowed_origins must not contain wildcard '*' in production",
		},
		{
			name: "CORS empty origins rejected in production",
			cfg: Config{
				Server:   ServerConfig{Env: "production", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o", OpenAIKey: "sk-test"},
				CORS:     CORSConfig{},
			},
			wantErr: true,
			errMsg:  "cors.allowed_origins is required in production",
		},
		{
			name: "CORS mixed valid and invalid origins",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
				CORS:     CORSConfig{AllowedOrigins: []string{"https://app.com", "bad-origin"}},
			},
			wantErr: true,
			errMsg:  `cors.allowed_origins: "bad-origin" is not a valid origin (must start with http:// or https://)`,
		},
		{
			name: "CORS origin with path rejected",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
				CORS:     CORSConfig{AllowedOrigins: []string{"https://example.com/dashboard"}},
			},
			wantErr: true,
			errMsg:  `cors.allowed_origins: "https://example.com/dashboard" must not contain a path (use https://example.com, not https://example.com/path)`,
		},
		{
			name: "CORS upper-cased scheme accepted",
			cfg: Config{
				Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
				Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "vigil", Name: "vigil", MaxOpenConns: 10},
				Redis:    RedisConfig{Host: "localhost", Port: 6379},
				NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "vigilagent"},
				Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
				CORS:     CORSConfig{AllowedOrigins: []string{"HTTP://EXAMPLE.COM"}},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.errMsg {
				t.Errorf("Validate() error = %q, want %q", err.Error(), tt.errMsg)
			}
		})
	}
}

func TestValidate_PortRange(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
			Redis:    RedisConfig{Host: "localhost", Port: 6379},
			NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m"},
		}
	}

	cfg := base()
	cfg.Server.Port = 0
	if err := cfg.Validate(); err == nil || err.Error() != "server.port must be between 1 and 65535, got 0" {
		t.Errorf("port 0: %v", err)
	}

	cfg = base()
	cfg.Server.Port = 65536
	if err := cfg.Validate(); err == nil || err.Error() != "server.port must be between 1 and 65535, got 65536" {
		t.Errorf("port 65536: %v", err)
	}

	cfg = base()
	cfg.Database.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for db port 0")
	}

	cfg = base()
	cfg.Database.Port = 99999
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for db port 99999")
	}

	cfg = base()
	cfg.Redis.Host = ""
	if err := cfg.Validate(); err == nil || err.Error() != "redis.host is required" {
		t.Errorf("empty redis host: %v", err)
	}

	cfg = base()
	cfg.Redis.Port = 0
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for redis port 0")
	}

	cfg = base()
	cfg.Redis.Port = 99999
	if err := cfg.Validate(); err == nil {
		t.Error("expected error for redis port 99999")
	}
}

func TestValidate_Timeouts(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
			Redis:    RedisConfig{Host: "localhost", Port: 6379},
			NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m"},
		}
	}

	cfg := base()
	cfg.Server.ReadTimeout = 0
	if err := cfg.Validate(); err == nil || err.Error() != "server.read_timeout must be positive" {
		t.Errorf("read timeout 0: %v", err)
	}

	cfg = base()
	cfg.Server.WriteTimeout = 0
	if err := cfg.Validate(); err == nil || err.Error() != "server.write_timeout must be positive" {
		t.Errorf("write timeout 0: %v", err)
	}
}

func TestValidate_MissingNATS(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
			Redis:    RedisConfig{Host: "localhost", Port: 6379},
			NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m"},
		}
	}

	cfg := base()
	cfg.NATS.URL = ""
	if err := cfg.Validate(); err == nil || err.Error() != "nats.url is required" {
		t.Errorf("empty nats url: %v", err)
	}

	cfg = base()
	cfg.NATS.Stream = ""
	if err := cfg.Validate(); err == nil || err.Error() != "nats.stream is required" {
		t.Errorf("empty nats stream: %v", err)
	}
}

func TestValidate_Auth(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
			Redis:    RedisConfig{Host: "localhost", Port: 6379},
			NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m"},
		}
	}

	cfg := base()
	cfg.Auth.JWTSecret = ""
	if err := cfg.Validate(); err == nil || err.Error() != "auth.jwt_secret is required" {
		t.Errorf("empty jwt secret: %v", err)
	}

	cfg = base()
	cfg.Auth.JWTExpiration = 0
	if err := cfg.Validate(); err == nil || err.Error() != "auth.jwt_expiration must be positive" {
		t.Errorf("zero jwt expiration: %v", err)
	}

	cfg = base()
	cfg.Server.Env = "production"
	cfg.Auth.JWTSecret = "short"
	if err := cfg.Validate(); err == nil || err.Error() != "auth.jwt_secret must be at least 32 characters" {
		t.Errorf("short jwt secret in prod: %v", err)
	}
}

func TestValidate_LLM(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
			Redis:    RedisConfig{Host: "localhost", Port: 6379},
			NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m"},
		}
	}

	cfg := base()
	cfg.LLM.DefaultModel = ""
	if err := cfg.Validate(); err == nil || err.Error() != "llm.default_model is required" {
		t.Errorf("empty model: %v", err)
	}

	cfg = base()
	cfg.LLM.BudgetPerTask = -1
	if err := cfg.Validate(); err == nil || err.Error() != "llm.budget_per_task must be non-negative" {
		t.Errorf("negative budget: %v", err)
	}

	cfg = base()
	cfg.LLM.MaxTokens = -1
	if err := cfg.Validate(); err == nil || err.Error() != "llm.max_tokens must be non-negative" {
		t.Errorf("negative max tokens: %v", err)
	}

	cfg = base()
	cfg.Server.Env = "production"
	cfg.Auth.JWTSecret = "super-secret-long-key-for-prod-1234"
	if err := cfg.Validate(); err == nil || err.Error() != "at least one LLM API key is required in production" {
		t.Errorf("no LLM keys in prod: %v", err)
	}
}

func TestValidate_LogLevel(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
			Redis:    RedisConfig{Host: "localhost", Port: 6379},
			NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m"},
		}
	}

	for _, level := range []string{"debug", "info", "warn", "error"} {
		cfg := base()
		cfg.Log.Level = level
		if err := cfg.Validate(); err != nil {
			t.Errorf("valid log level %q: %v", level, err)
		}
	}

	cfg := base()
	cfg.Log.Level = "invalid"
	if err := cfg.Validate(); err == nil || err.Error() != "log.level must be one of: debug, info, warn, error" {
		t.Errorf("invalid log level: %v", err)
	}

	cfg = base()
	cfg.Log.Level = ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("empty log level should be valid: %v", err)
	}
}

func TestValidate_MaxOpenConns(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
		Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 0},
		Redis:    RedisConfig{Host: "localhost", Port: 6379},
		NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
		Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
		LLM:      LLMConfig{DefaultModel: "m"},
	}
	if err := cfg.Validate(); err == nil || err.Error() != "database.max_open_conns must be at least 1" {
		t.Errorf("zero max open conns: %v", err)
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	os.WriteFile(envFile, []byte("TEST_ENV_VAR_1=hello\nTEST_ENV_VAR_2=world\n# comment\n\nKEY=VALUE\n"), 0644)

	// Read the env file
	loadEnvFile(envFile)

	if os.Getenv("TEST_ENV_VAR_1") != "hello" {
		t.Errorf("expected TEST_ENV_VAR_1=hello, got %q", os.Getenv("TEST_ENV_VAR_1"))
	}
	if os.Getenv("TEST_ENV_VAR_2") != "world" {
		t.Errorf("expected TEST_ENV_VAR_2=world, got %q", os.Getenv("TEST_ENV_VAR_2"))
	}
	if os.Getenv("KEY") != "VALUE" {
		t.Errorf("expected KEY=VALUE, got %q", os.Getenv("KEY"))
	}

	// Cleanup
	os.Unsetenv("TEST_ENV_VAR_1")
	os.Unsetenv("TEST_ENV_VAR_2")
	os.Unsetenv("KEY")
}

func TestLoadEnvFile_NonExistent(t *testing.T) {
	// Should not panic
	loadEnvFile("/nonexistent/.env")
}

func TestLoad_WithConfigPath(t *testing.T) {
	// When a specific path is set but file doesn't exist, viper may error
	// depending on whether it's treated as ConfigFileNotFoundError or not.
	// Just verify Load doesn't panic.
	os.Setenv("VIGILAGENT_CONFIG_PATH", "nonexistent.yaml")
	defer os.Unsetenv("VIGILAGENT_CONFIG_PATH")
	Load()
}

func TestLoad_CORSDefaults(t *testing.T) {
	os.Unsetenv("VIGILAGENT_CORS_ALLOWED_ORIGINS")
	os.Unsetenv("VIGILAGENT_CORS_ALLOWED_METHODS")
	os.Unsetenv("VIGILAGENT_CORS_ALLOWED_HEADERS")
	os.Unsetenv("VIGILAGENT_CORS_MAX_AGE")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if len(cfg.CORS.AllowedOrigins) == 0 || cfg.CORS.AllowedOrigins[0] != "*" {
		t.Errorf("expected CORS default origins ['*'], got %v", cfg.CORS.AllowedOrigins)
	}
	if len(cfg.CORS.AllowedMethods) == 0 {
		t.Error("expected CORS default methods")
	}
	if len(cfg.CORS.AllowedHeaders) == 0 {
		t.Error("expected CORS default headers")
	}
	if cfg.CORS.MaxAge != 86400 {
		t.Errorf("expected CORS max age 86400, got %d", cfg.CORS.MaxAge)
	}
}

// --- Hot reload tests ---

func TestNewHotReloader(t *testing.T) {
	cfg := &Config{}
	hr := NewHotReloader(cfg)
	if hr == nil {
		t.Fatal("expected non-nil hot reloader")
	}
	if hr.debounce != 500*time.Millisecond {
		t.Errorf("expected 500ms debounce, got %v", hr.debounce)
	}
}

func TestHotReloader_OnChange(t *testing.T) {
	cfg := &Config{}
	hr := NewHotReloader(cfg)
	called := false
	hr.OnChange(func(newCfg *Config) {
		called = true
	})
	if len(hr.callbacks) != 1 {
		t.Errorf("expected 1 callback, got %d", len(hr.callbacks))
	}
	_ = called
}

func TestHotReloader_Config(t *testing.T) {
	cfg := &Config{Server: ServerConfig{Host: "test"}}
	hr := NewHotReloader(cfg)
	got := hr.Config()
	if got.Server.Host != "test" {
		t.Errorf("expected host 'test', got %q", got.Server.Host)
	}
}

func TestHotReloader_StartStop(t *testing.T) {
	cfg := &Config{}
	hr := NewHotReloader(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		hr.Start(ctx)
		close(done)
	}()
	time.Sleep(200 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func TestReadFromViper(t *testing.T) {
	cfg := readFromViper()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	// Should have CORS defaults
	if len(cfg.CORS.AllowedOrigins) == 0 {
		t.Error("expected CORS defaults")
	}
}

// --- Middleware config tests ---

func TestDefaultMiddlewareConfig(t *testing.T) {
	cfg := DefaultMiddlewareConfig()
	if !cfg.RateLimit.Enabled {
		t.Error("expected rate limit enabled")
	}
	if cfg.RateLimit.Limit != 100 {
		t.Errorf("expected rate limit 100, got %d", cfg.RateLimit.Limit)
	}
	if cfg.RateLimit.Window != time.Minute {
		t.Errorf("expected rate limit window 1m, got %v", cfg.RateLimit.Window)
	}
	if !cfg.Timeout.Enabled {
		t.Error("expected timeout enabled")
	}
	if cfg.Timeout.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", cfg.Timeout.Timeout)
	}
	if !cfg.CORS.Enabled {
		t.Error("expected CORS enabled")
	}
	if !cfg.Recovery.Enabled {
		t.Error("expected recovery enabled")
	}
	if !cfg.RequestBody.Enabled {
		t.Error("expected request body enabled")
	}
	if cfg.RequestBody.MaxBytes != 10<<20 {
		t.Errorf("expected max bytes 10MB, got %d", cfg.RequestBody.MaxBytes)
	}
}

// --- Production validation tests ---

func TestValidateProduction_NotProduction(t *testing.T) {
	cfg := &Config{Server: ServerConfig{Env: "development"}}
	if err := ValidateProduction(cfg); err != nil {
		t.Errorf("should pass for non-production: %v", err)
	}
}

func TestValidateProduction_EmptyJWT(t *testing.T) {
	cfg := &Config{Server: ServerConfig{Env: "production"}, Auth: AuthConfig{JWTSecret: ""}}
	err := ValidateProduction(cfg)
	if err == nil {
		t.Fatal("expected error for empty JWT secret in production")
	}
}

func TestValidateProduction_DefaultJWT(t *testing.T) {
	cfg := &Config{Server: ServerConfig{Env: "production"}, Auth: AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!"}}
	err := ValidateProduction(cfg)
	if err == nil {
		t.Fatal("expected error for default JWT secret in production")
	}
}

func TestValidateProduction_SecretJWT(t *testing.T) {
	cfg := &Config{Server: ServerConfig{Env: "production"}, Auth: AuthConfig{JWTSecret: "secret"}}
	err := ValidateProduction(cfg)
	if err == nil {
		t.Fatal("expected error for 'secret' JWT in production")
	}
}

func TestValidateProduction_DefaultJWT2(t *testing.T) {
	cfg := &Config{Server: ServerConfig{Env: "production"}, Auth: AuthConfig{JWTSecret: "default"}}
	err := ValidateProduction(cfg)
	if err == nil {
		t.Fatal("expected error for 'default' JWT in production")
	}
}

func TestValidateProduction_ShortJWT(t *testing.T) {
	cfg := &Config{Server: ServerConfig{Env: "production"}, Auth: AuthConfig{JWTSecret: "short"}}
	err := ValidateProduction(cfg)
	if err == nil {
		t.Fatal("expected error for short JWT in production")
	}
}

func TestValidateProduction_PlaceholderAPIKey(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		LLM:    LLMConfig{OpenAIKey: "sk-xxx-placeholder"},
	}
	err := ValidateProduction(cfg)
	if err == nil {
		t.Fatal("expected error for placeholder API key")
	}
}

func TestValidateProduction_PlaceholderAnthropic(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		LLM:    LLMConfig{AnthropicKey: "PLACEHOLDER-key"},
	}
	err := ValidateProduction(cfg)
	if err == nil {
		t.Fatal("expected error for placeholder Anthropic key")
	}
}

func TestValidateProduction_LocalhostDB(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "localhost"},
		Redis:    RedisConfig{Host: "localhost"},
	}
	err := ValidateProduction(cfg)
	if err == nil {
		t.Fatal("expected error for localhost DB in production")
	}
}

func TestValidateProduction_LocalhostRedis(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "real-db.example.com"},
		Redis:    RedisConfig{Host: "localhost"},
	}
	err := ValidateProduction(cfg)
	if err == nil {
		t.Fatal("expected error for localhost Redis in production")
	}
}

func TestValidateProduction_127001(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "127.0.0.1"},
		Redis:    RedisConfig{Host: "127.0.0.1"},
	}
	err := ValidateProduction(cfg)
	if err == nil {
		t.Fatal("expected error for 127.0.0.1 in production")
	}
}

func TestValidateProduction_Valid(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "db.prod.example.com"},
		Redis:    RedisConfig{Host: "redis.prod.example.com"},
		LLM:      LLMConfig{OpenAIKey: "sk-real"},
	}
	if err := ValidateProduction(cfg); err != nil {
		t.Errorf("valid production config: %v", err)
	}
}

func TestValidateProductionEnv_NotProduction(t *testing.T) {
	os.Unsetenv("VIGILAGENT_ENV")
	if err := ValidateProductionEnv(); err != nil {
		t.Errorf("should pass for non-production: %v", err)
	}
}

func TestValidateProductionEnv_EmptyJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Unsetenv("VIGILAGENT_JWT_SECRET")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	if err == nil {
		t.Fatal("expected error for empty JWT in production env")
	}
}

func TestValidateProductionEnv_DefaultJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "change-me-in-production")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	if err == nil {
		t.Fatal("expected error for default JWT in production env")
	}
}

func TestValidateProductionEnv_SecretJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "secret")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	if err == nil {
		t.Fatal("expected error for 'secret' JWT in production env")
	}
}

func TestValidateProductionEnv_DefaultJWT2(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "default")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	if err == nil {
		t.Fatal("expected error for 'default' JWT in production env")
	}
}

func TestValidateProductionEnv_ShortJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "short")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	if err == nil {
		t.Fatal("expected error for short JWT in production env")
	}
}

func TestValidateProductionEnv_Valid(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "super-secret-long-key-for-prod-1234")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	if err := ValidateProductionEnv(); err != nil {
		t.Errorf("valid production env: %v", err)
	}
}

func TestValidate_DefaultJWTSecretRejectedAllEnvironments(t *testing.T) {
	base := func(env string) Config {
		return Config{
			Server:   ServerConfig{Env: env, Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
			Redis:    RedisConfig{Host: "localhost", Port: 6379},
			NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m"},
		}
	}

	for _, env := range []string{"development", "staging", "production", "test"} {
		cfg := base(env)
		err := cfg.Validate()
		if err == nil || err.Error() != "auth.jwt_secret must be changed from default value" {
			t.Errorf("env=%s: expected default JWT rejection, got %v", env, err)
		}
	}
}

func TestValidate_DefaultDBPasswordRejectedInProduction(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{Env: "production", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
		Database: DatabaseConfig{Host: "db.prod.com", Port: 5432, User: "u", Password: "vigilagent", Name: "n", MaxOpenConns: 10, SSLMode: "require"},
		Redis:    RedisConfig{Host: "redis.prod.com", Port: 6379},
		NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234", JWTExpiration: 24 * time.Hour},
		LLM:      LLMConfig{DefaultModel: "m", OpenAIKey: "sk-test"},
		CORS:     CORSConfig{AllowedOrigins: []string{"https://app.example.com"}},
	}
	err := cfg.Validate()
	if err == nil || err.Error() != "database.password must be changed from default in production" {
		t.Errorf("expected default DB password rejection, got %v", err)
	}
}

func TestValidate_CORS_AllowInsecureOriginsDefaultFalse(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
		Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
		Redis:    RedisConfig{Host: "localhost", Port: 6379},
		NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
		Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
		LLM:      LLMConfig{DefaultModel: "m"},
		CORS:     CORSConfig{AllowedOrigins: []string{"*"}},
	}
	if cfg.CORS.AllowInsecureOrigins {
		t.Error("AllowInsecureOrigins should default to false")
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("wildcard allowed in dev: %v", err)
	}
}

func TestValidate_CORS_SubdomainPatternAccepted(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
		Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
		Redis:    RedisConfig{Host: "localhost", Port: 6379},
		NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
		Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
		LLM:      LLMConfig{DefaultModel: "m"},
		CORS:     CORSConfig{AllowedOrigins: []string{"*.example.com"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("subdomain pattern should be valid: %v", err)
	}
}

func TestValidate_CORS_SubdomainPatternInProduction(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{Env: "production", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
		Database: DatabaseConfig{Host: "db.prod.com", Port: 5432, User: "u", Password: "strong-password-123", Name: "n", MaxOpenConns: 10, SSLMode: "require"},
		Redis:    RedisConfig{Host: "redis.prod.com", Port: 6379},
		NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234", JWTExpiration: 24 * time.Hour},
		LLM:      LLMConfig{DefaultModel: "m", OpenAIKey: "sk-test"},
		CORS:     CORSConfig{AllowedOrigins: []string{"*.app.example.com"}},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("subdomain pattern in production should be valid: %v", err)
	}
}

func TestValidate_SSLModeDisableRejectedInProduction(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{Env: "production", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
		Database: DatabaseConfig{Host: "db.prod.com", Port: 5432, User: "u", Password: "strong-password-123", Name: "n", MaxOpenConns: 10, SSLMode: "disable"},
		Redis:    RedisConfig{Host: "redis.prod.com", Port: 6379},
		NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234", JWTExpiration: 24 * time.Hour},
		LLM:      LLMConfig{DefaultModel: "m", OpenAIKey: "sk-test"},
		CORS:     CORSConfig{AllowedOrigins: []string{"https://app.example.com"}},
	}
	err := cfg.Validate()
	if err == nil || err.Error() != "database.sslmode must not be 'disable' in production" {
		t.Errorf("expected sslmode=disable rejection, got %v", err)
	}
}

func TestValidate_RetryMaxAttemptsNegative(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
		Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10, RetryMaxAttempts: -1},
		Redis:    RedisConfig{Host: "localhost", Port: 6379},
		NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
		Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
		LLM:      LLMConfig{DefaultModel: "m"},
	}
	err := cfg.Validate()
	if err == nil || err.Error() != "database.retry_max_attempts must be non-negative" {
		t.Errorf("expected retry_max_attempts negative error, got %v", err)
	}
}

func TestValidate_SlowQueryThresholdNegative(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
		Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10, SlowQueryThreshold: -time.Second},
		Redis:    RedisConfig{Host: "localhost", Port: 6379},
		NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
		Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
		LLM:      LLMConfig{DefaultModel: "m"},
	}
	err := cfg.Validate()
	if err == nil || err.Error() != "database.slow_query_threshold must be non-negative" {
		t.Errorf("expected slow_query_threshold negative error, got %v", err)
	}
}

func TestValidate_RetryMaxAttemptsZeroValid(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
		Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10, RetryMaxAttempts: 0},
		Redis:    RedisConfig{Host: "localhost", Port: 6379},
		NATS:     NATSConfig{URL: "nats://x", Stream: "s"},
		Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
		LLM:      LLMConfig{DefaultModel: "m"},
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("zero retry_max_attempts should be valid: %v", err)
	}
}
