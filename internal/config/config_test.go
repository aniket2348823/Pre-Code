package config

import (
	"testing"
	"time"
)

func TestLoad_Defaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() returned error: %v", err)
	}

	// Verify defaults
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
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
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
			errMsg:  "auth.jwt_secret must be changed in production",
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
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
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
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
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
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
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
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
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
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
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
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
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
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
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
				Auth:     AuthConfig{JWTSecret: "change-me-in-production", JWTExpiration: 24 * time.Hour},
				LLM:      LLMConfig{DefaultModel: "gpt-4o"},
				CORS:     CORSConfig{AllowedOrigins: []string{"HTTP://EXAMPLE.COM"}},
			},
			wantErr: false, // case-insensitive scheme check passes
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
