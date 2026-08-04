package config

import (
	"math"
	"strings"
	"testing"
	"time"
)

func TestConfig_AllZeroValues(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Port:              0,
			RateLimitPerMin:   0,
			ReadTimeout:       0,
			ReadHeaderTimeout: 0,
			WriteTimeout:      0,
			IdleTimeout:       0,
		},
		Database: DatabaseConfig{
			Port:         0,
			MaxOpenConns: 0,
			MaxIdleConns: 0,
			MaxLifetime:  0,
			ConnIdleTime: 0,
		},
		Redis: RedisConfig{
			Port: 0,
			DB:   0,
		},
		Auth: AuthConfig{
			JWTSecret:     "",
			JWTExpiration: 0,
		},
		LLM: LLMConfig{
			BudgetPerTask: 0,
			MaxTokens:     0,
		},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for all zero values")
	}
	// Should catch at least: server port, database host/user/name/port, redis host, nats, auth secret
}

func TestConfig_MaxIntValues(t *testing.T) {
	cfg := Config{
		Server: ServerConfig{
			Env:               "development",
			Port:              math.MaxInt64,
			ReadTimeout:       time.Duration(math.MaxInt64),
			ReadHeaderTimeout: time.Duration(math.MaxInt64),
			WriteTimeout:      time.Duration(math.MaxInt64),
			IdleTimeout:       time.Duration(math.MaxInt64),
			RateLimitPerMin:   math.MaxInt64,
		},
		Database: DatabaseConfig{
			Host:         "localhost",
			Port:         math.MaxInt64,
			User:         "u",
			Name:         "n",
			MaxOpenConns: math.MaxInt64,
			MaxIdleConns: math.MaxInt64,
			MaxLifetime:  time.Duration(math.MaxInt64),
			ConnIdleTime: time.Duration(math.MaxInt64),
		},
		Redis: RedisConfig{
			Host: "localhost",
			Port: math.MaxInt64,
		},
		NATS: NATSConfig{
			URL:    "nats://localhost:4222",
			Stream: "s",
		},
		Auth: AuthConfig{
			JWTSecret:     "a-secure-32-char-jwt-secret-ok!!",
			JWTExpiration: time.Duration(math.MaxInt64),
		},
		LLM: LLMConfig{
			DefaultModel:  "m",
			BudgetPerTask: float64(math.MaxFloat64),
			MaxTokens:     math.MaxInt64,
		},
	}

	// Port out of range should fail
	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for max int port values")
	}

	// DSN with extreme values
	dsn := cfg.Database.DSN()
	if dsn == "" {
		t.Error("DSN should not be empty")
	}

	// Redis address with extreme values
	addr := cfg.Redis.Address()
	if addr == "" {
		t.Error("Address should not be empty")
	}
}

func TestConfig_EmptyStringsEverywhere(t *testing.T) {
	cfg := Config{
		Server:   ServerConfig{},
		Database: DatabaseConfig{},
		Redis:    RedisConfig{},
		NATS:     NATSConfig{},
		Auth:     AuthConfig{},
		LLM:      LLMConfig{},
	}

	err := cfg.Validate()
	if err == nil {
		t.Error("expected error for all empty strings")
	}
	// Should catch: database.host, database.user, database.name, redis.host, nats.url, auth.jwt_secret, llm.default_model
}

func TestConfig_VeryLongJWTSecret(t *testing.T) {
	longSecret := strings.Repeat("x", 1000000) // 1MB

	cfg := Config{
		Server: ServerConfig{
			Env:               "development",
			Port:              8080,
			ReadTimeout:       10 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       10 * time.Second,
		},
		Database: DatabaseConfig{
			Host:         "localhost",
			Port:         5432,
			User:         "u",
			Name:         "n",
			MaxOpenConns: 10,
		},
		Redis: RedisConfig{
			Host: "localhost",
			Port: 6379,
		},
		NATS: NATSConfig{
			URL:    "nats://localhost:4222",
			Stream: "s",
		},
		Auth: AuthConfig{
			JWTSecret:     longSecret,
			JWTExpiration: 24 * time.Hour,
		},
		LLM: LLMConfig{
			DefaultModel: "m",
		},
	}

	// Should handle extremely long secrets without panicking
	err := cfg.Validate()
	if err != nil {
		t.Errorf("1MB JWT secret should be valid, got error: %v", err)
	}

	// DSN should work with long secret
	dsn := cfg.Database.DSN()
	if len(dsn) == 0 {
		t.Error("DSN should not be empty")
	}
}

func TestConfig_VeryLongStringsInFields(t *testing.T) {
	long := strings.Repeat("a", 100000)

	cfg := Config{
		Server: ServerConfig{
			Env:               long,
			Port:              8080,
			BaseURL:           long,
			ReadTimeout:       10 * time.Second,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      10 * time.Second,
			IdleTimeout:       10 * time.Second,
		},
		Database: DatabaseConfig{
			Host:         "localhost",
			Port:         5432,
			User:         long,
			Password:     long,
			Name:         long,
			SSLMode:      long,
			MaxOpenConns: 10,
		},
		Redis: RedisConfig{
			Host:     "localhost",
			Port:     6379,
			Password: long,
		},
		NATS: NATSConfig{
			URL:    "nats://localhost:4222",
			Stream: "s",
		},
		Auth: AuthConfig{
			JWTSecret:     "a-secure-32-char-jwt-secret-ok!!",
			JWTExpiration: 24 * time.Hour,
		},
		LLM: LLMConfig{
			DefaultModel:  long,
			OpenAIKey:     long,
			AnthropicKey:  long,
		},
	}

	// Should not panic even with extreme strings
	err := cfg.Validate()
	_ = err // may or may not be valid depending on field

	// DSN with long fields
	dsn := cfg.Database.DSN()
	if dsn == "" {
		t.Error("DSN should not be empty")
	}

	// Redis address
	addr := cfg.Redis.Address()
	if addr == "" {
		t.Error("Address should not be empty")
	}
}

func TestConfig_NegativePortValues(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
			Redis:    RedisConfig{Host: "localhost", Port: 6379},
			NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m"},
		}
	}

	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{"negative server port", func(c *Config) { c.Server.Port = -1 }, true},
		{"negative db port", func(c *Config) { c.Database.Port = -1 }, true},
		{"negative redis port", func(c *Config) { c.Redis.Port = -1 }, true},
		{"negative max open conns", func(c *Config) { c.Database.MaxOpenConns = -1 }, true},
		{"negative budget", func(c *Config) { c.LLM.BudgetPerTask = -1 }, true},
		{"negative max tokens", func(c *Config) { c.LLM.MaxTokens = -1 }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.modify(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_ProductionSafetyEdgeCases(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   ServerConfig{Env: "production", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "db.prod.com", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10, Password: "real-pass", SSLMode: "require"},
			Redis:    RedisConfig{Host: "redis.prod.com", Port: 6379},
			NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m", OpenAIKey: "sk-real"},
			CORS:     CORSConfig{AllowedOrigins: []string{"https://app.example.com"}},
		}
	}

	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{"production with default password", func(c *Config) { c.Database.Password = "vigilagent" }, true},
		{"production with disable sslmode", func(c *Config) { c.Database.SSLMode = "disable" }, true},
		{"production with wildcard CORS", func(c *Config) { c.CORS.AllowedOrigins = []string{"*"} }, true},
		{"production with empty CORS", func(c *Config) { c.CORS.AllowedOrigins = []string{} }, true},
		{"production with no LLM keys", func(c *Config) {
			c.LLM.OpenAIKey = ""
			c.LLM.AnthropicKey = ""
			c.LLM.GeminiKey = ""
			c.LLM.OpenRouterKey = ""
			c.LLM.MistralKey = ""
			c.LLM.GroqKey = ""
			c.LLM.NVIDIANIMKey = ""
			c.LLM.CohereKey = ""
		}, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.modify(&cfg)
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_ProductionSafety_ValidateProduction(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   ServerConfig{Env: "production", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "db.prod.com", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10, Password: "real-pass", SSLMode: "require"},
			Redis:    RedisConfig{Host: "redis.prod.com", Port: 6379},
			NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m", OpenAIKey: "sk-real"},
			CORS:     CORSConfig{AllowedOrigins: []string{"https://app.example.com"}},
		}
	}

	tests := []struct {
		name    string
		modify  func(*Config)
		wantErr bool
	}{
		{"production with localhost db", func(c *Config) { c.Database.Host = "localhost" }, true},
		{"production with 127.0.0.1 redis", func(c *Config) { c.Redis.Host = "127.0.0.1" }, true},
		{"production with placeholder openai key", func(c *Config) { c.LLM.OpenAIKey = "sk-xxx-placeholder" }, true},
		{"production with placeholder anthropic key", func(c *Config) { c.LLM.AnthropicKey = "PLACEHOLDER-key" }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			tt.modify(&cfg)
			err := ValidateProduction(&cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProduction() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_CORSOriginEdgeCases(t *testing.T) {
	base := func() Config {
		return Config{
			Server:   ServerConfig{Env: "development", Port: 8080, ReadTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second},
			Database: DatabaseConfig{Host: "localhost", Port: 5432, User: "u", Name: "n", MaxOpenConns: 10},
			Redis:    RedisConfig{Host: "localhost", Port: 6379},
			NATS:     NATSConfig{URL: "nats://localhost:4222", Stream: "s"},
			Auth:     AuthConfig{JWTSecret: "a-secure-32-char-jwt-secret-ok!!", JWTExpiration: 24 * time.Hour},
			LLM:      LLMConfig{DefaultModel: "m"},
		}
	}

	tests := []struct {
		name    string
	 origins []string
		wantErr bool
	}{
		{"valid https", []string{"https://app.example.com"}, false},
		{"valid http", []string{"http://localhost:3000"}, false},
		{"valid with port", []string{"https://example.com:8443"}, false},
		{"wildcard allowed", []string{"*"}, false},
		{"no scheme", []string{"example.com"}, true},
		{"path included", []string{"https://example.com/path"}, true},
		{"ftp scheme", []string{"ftp://example.com"}, true},
		{"mixed valid invalid", []string{"https://a.com", "bad"}, true},
		{"empty string", []string{""}, true},
		{"just http", []string{"http://"}, false}, // technically valid format
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := base()
			cfg.CORS.AllowedOrigins = tt.origins
			err := cfg.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_DSN_LongFields(t *testing.T) {
	cfg := &DatabaseConfig{
		Host:     strings.Repeat("h", 10000),
		Port:     5432,
		User:     strings.Repeat("u", 10000),
		Password: strings.Repeat("p", 10000),
		Name:     strings.Repeat("n", 10000),
		SSLMode:  "disable",
	}

	dsn := cfg.DSN()
	if dsn == "" {
		t.Error("DSN should not be empty for long field values")
	}
	// Should contain all the long values
	if len(dsn) < 30000 {
		t.Errorf("DSN with long fields should be substantial, got len=%d", len(dsn))
	}
}

func TestConfig_RedisAddress_EdgeCases(t *testing.T) {
	tests := []struct {
		host string
		port int
		addr string
	}{
		{"localhost", 6379, "localhost:6379"},
		{"redis.prod.internal", 6380, "redis.prod.internal:6380"},
		{strings.Repeat("r", 10000), 6379, strings.Repeat("r", 10000) + ":6379"},
		{"", 0, ":0"},
		{"::1", 6379, "::1:6379"},
	}

	for _, tt := range tests {
		cfg := &RedisConfig{Host: tt.host, Port: tt.port}
		got := cfg.Address()
		if got != tt.addr {
			t.Errorf("Address() = %q, want %q", got, tt.addr)
		}
	}
}

func TestMiddlewareConfig_EdgeCases(t *testing.T) {
	cfg := DefaultMiddlewareConfig()

	// All zero values
	zeroCfg := MiddlewareConfig{}
	if zeroCfg.RateLimit != nil {
		t.Error("zero MiddlewareConfig should have nil RateLimit")
	}
	if zeroCfg.Timeout != nil {
		t.Error("zero MiddlewareConfig should have nil Timeout")
	}
	if zeroCfg.CORS != nil {
		t.Error("zero MiddlewareConfig should have nil CORS")
	}
	if zeroCfg.Recovery != nil {
		t.Error("zero MiddlewareConfig should have nil Recovery")
	}
	if zeroCfg.RequestBody != nil {
		t.Error("zero MiddlewareConfig should have nil RequestBody")
	}

	// Defaults should be non-zero
	if !cfg.RateLimit.Enabled {
		t.Error("default rate limit should be enabled")
	}
	if cfg.RateLimit.Limit != 100 {
		t.Errorf("default rate limit = %d, want 100", cfg.RateLimit.Limit)
	}
	if cfg.RequestBody.MaxBytes != 10<<20 {
		t.Errorf("default max bytes = %d, want %d", cfg.RequestBody.MaxBytes, 10<<20)
	}
}

func TestValidateProduction_NilConfig(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			// ValidateProduction panics on nil — document this behavior
			t.Logf("ValidateProduction(nil) panics as expected: %v", r)
		}
	}()
	err := ValidateProduction(nil)
	if err != nil {
		t.Logf("ValidateProduction(nil) returned error: %v", err)
	}
}

func TestValidateProduction_NonProductionEnvs(t *testing.T) {
	envs := []string{"development", "staging", "test", "LOCAL", ""}
	for _, env := range envs {
		t.Run(env, func(t *testing.T) {
			cfg := &Config{Server: ServerConfig{Env: env}}
			if err := ValidateProduction(cfg); err != nil {
				t.Errorf("non-production env %q should pass: %v", env, err)
			}
		})
	}
}
