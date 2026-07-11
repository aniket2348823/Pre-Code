package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	NATS     NATSConfig
	Auth     AuthConfig
	LLM      LLMConfig
	Stripe   StripeConfig
	CORS     CORSConfig
	Log      LogConfig
}

type ServerConfig struct {
	Host         string
	Port         int
	Env          string
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration
}

type DatabaseConfig struct {
	Host         string
	Port         int
	User         string
	Password     string
	Name         string
	SSLMode      string
	MaxOpenConns int
	MaxIdleConns int
	MaxLifetime  time.Duration
}

type RedisConfig struct {
	Host     string
	Port     int
	Password string
	DB       int
}

type NATSConfig struct {
	URL    string
	Stream string
}

type AuthConfig struct {
	JWTSecret     string
	JWTExpiration time.Duration
	APIKeyPrefix  string
}

// LLMConfig holds LLM provider API keys and routing config.
// Each key is optional; providers are only registered when their key is set.
type LLMConfig struct {
	OpenAIKey      string
	AnthropicKey   string
	GeminiKey      string
	OpenRouterKey  string
	MistralKey     string
	GroqKey        string
	NVIDIANIMKey   string
	CohereKey      string
	DefaultModel   string
	BudgetPerTask  float64
	MaxTokens      int
}

type StripeConfig struct {
	SecretKey     string
	WebhookSecret string
	SuccessURL    string
	CancelURL     string
}

// CORSConfig holds CORS middleware configuration.
type CORSConfig struct {
	AllowedOrigins  []string
	AllowedMethods  []string
	AllowedHeaders  []string
	AllowCredentials bool
	MaxAge          int
}

type LogConfig struct {
	Level  string
	Format string
}

func Load() (*Config, error) {
	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath("./configs")
	viper.AddConfigPath(".")

	// Set defaults
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.env", "development")
	viper.SetDefault("server.read_timeout", 10*time.Second)
	viper.SetDefault("server.write_timeout", 10*time.Second)
	viper.SetDefault("server.idle_timeout", 120*time.Second)

	viper.SetDefault("database.host", "localhost")
	viper.SetDefault("database.port", 5432)
	viper.SetDefault("database.user", "vigilagent")
	viper.SetDefault("database.password", "vigilagent")
	viper.SetDefault("database.name", "vigilagent")
	viper.SetDefault("database.sslmode", "disable")
	viper.SetDefault("database.max_open_conns", 25)
	viper.SetDefault("database.max_idle_conns", 10)
	viper.SetDefault("database.max_lifetime", 5*time.Minute)

	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)

	viper.SetDefault("nats.url", "nats://localhost:4222")
	viper.SetDefault("nats.stream", "vigilagent")

	viper.SetDefault("auth.jwt_secret", "change-me-in-production")
	viper.SetDefault("auth.jwt_expiration", 24*time.Hour)
	viper.SetDefault("auth.api_key_prefix", "va_")

	// LLM defaults
	viper.SetDefault("llm.default_model", "claude-sonnet-4-20250514")
	viper.SetDefault("llm.budget_per_task", 1.0)
	viper.SetDefault("llm.max_tokens", 8192)

	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "json")

	// Enable environment variable override
	viper.AutomaticEnv()
	viper.SetEnvPrefix("VIGILAGENT")

	// Read config file (if exists)
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	cfg := &Config{
		Server: ServerConfig{
			Host:         viper.GetString("server.host"),
			Port:         viper.GetInt("server.port"),
			Env:          viper.GetString("server.env"),
			ReadTimeout:  viper.GetDuration("server.read_timeout"),
			WriteTimeout: viper.GetDuration("server.write_timeout"),
			IdleTimeout:  viper.GetDuration("server.idle_timeout"),
		},
		Database: DatabaseConfig{
			Host:         viper.GetString("database.host"),
			Port:         viper.GetInt("database.port"),
			User:         viper.GetString("database.user"),
			Password:     viper.GetString("database.password"),
			Name:         viper.GetString("database.name"),
			SSLMode:      viper.GetString("database.sslmode"),
			MaxOpenConns: viper.GetInt("database.max_open_conns"),
			MaxIdleConns: viper.GetInt("database.max_idle_conns"),
			MaxLifetime:  viper.GetDuration("database.max_lifetime"),
		},
		Redis: RedisConfig{
			Host:     viper.GetString("redis.host"),
			Port:     viper.GetInt("redis.port"),
			Password: viper.GetString("redis.password"),
			DB:       viper.GetInt("redis.db"),
		},
		NATS: NATSConfig{
			URL:    viper.GetString("nats.url"),
			Stream: viper.GetString("nats.stream"),
		},		Auth: AuthConfig{
			JWTSecret:     viper.GetString("auth.jwt_secret"),
			JWTExpiration: viper.GetDuration("auth.jwt_expiration"),
			APIKeyPrefix:  viper.GetString("auth.api_key_prefix"),
		},
		LLM: LLMConfig{
			OpenAIKey:     viper.GetString("llm.openai_key"),
			AnthropicKey:  viper.GetString("llm.anthropic_key"),
			GeminiKey:     viper.GetString("llm.gemini_key"),
			OpenRouterKey: viper.GetString("llm.openrouter_key"),
			MistralKey:    viper.GetString("llm.mistral_key"),
			GroqKey:       viper.GetString("llm.groq_key"),
			NVIDIANIMKey:  viper.GetString("llm.nvidia_nim_key"),
			CohereKey:     viper.GetString("llm.cohere_key"),
			DefaultModel:  viper.GetString("llm.default_model"),
			BudgetPerTask: viper.GetFloat64("llm.budget_per_task"),
			MaxTokens:     viper.GetInt("llm.max_tokens"),
		},
		Stripe: StripeConfig{
			SecretKey:     viper.GetString("stripe.secret_key"),
			WebhookSecret: viper.GetString("stripe.webhook_secret"),
			SuccessURL:    viper.GetString("stripe.success_url"),
			CancelURL:     viper.GetString("stripe.cancel_url"),
		},
		CORS: CORSConfig{
			AllowedOrigins:  viper.GetStringSlice("cors.allowed_origins"),
			AllowedMethods:  viper.GetStringSlice("cors.allowed_methods"),
			AllowedHeaders:  viper.GetStringSlice("cors.allowed_headers"),
			AllowCredentials: viper.GetBool("cors.allow_credentials"),
			MaxAge:          viper.GetInt("cors.max_age"),
		},
		Log: LogConfig{
			Level:  viper.GetString("log.level"),
			Format: viper.GetString("log.format"),
		},
	}

	// Apply CORS defaults if not configured
	if len(cfg.CORS.AllowedOrigins) == 0 {
		cfg.CORS.AllowedOrigins = []string{"*"}
	}
	if len(cfg.CORS.AllowedMethods) == 0 {
		cfg.CORS.AllowedMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"}
	}
	if len(cfg.CORS.AllowedHeaders) == 0 {
		cfg.CORS.AllowedHeaders = []string{"Accept", "Authorization", "Content-Type", "X-API-Key", "X-Request-ID"}
	}
	if cfg.CORS.MaxAge == 0 {
		cfg.CORS.MaxAge = 86400
	}

	return cfg, nil
}

func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode)
}

// Validate checks the configuration for required fields and security constraints.
func (c *Config) Validate() error {
	// Server
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be between 1 and 65535, got %d", c.Server.Port)
	}
	if c.Server.ReadTimeout <= 0 {
		return fmt.Errorf("server.read_timeout must be positive")
	}
	if c.Server.WriteTimeout <= 0 {
		return fmt.Errorf("server.write_timeout must be positive")
	}

	// Database
	if c.Database.Host == "" {
		return fmt.Errorf("database.host is required")
	}
	if c.Database.User == "" {
		return fmt.Errorf("database.user is required")
	}
	if c.Database.Name == "" {
		return fmt.Errorf("database.name is required")
	}
	if c.Database.Port < 1 || c.Database.Port > 65535 {
		return fmt.Errorf("database.port must be between 1 and 65535")
	}
	if c.Database.MaxOpenConns < 1 {
		return fmt.Errorf("database.max_open_conns must be at least 1")
	}

	// Redis
	if c.Redis.Host == "" {
		return fmt.Errorf("redis.host is required")
	}
	if c.Redis.Port < 1 || c.Redis.Port > 65535 {
		return fmt.Errorf("redis.port must be between 1 and 65535")
	}

	// NATS
	if c.NATS.URL == "" {
		return fmt.Errorf("nats.url is required")
	}
	if c.NATS.Stream == "" {
		return fmt.Errorf("nats.stream is required")
	}

	// Auth
	if c.Auth.JWTSecret == "" {
		return fmt.Errorf("auth.jwt_secret is required")
	}
	if c.Auth.JWTSecret == "change-me-in-production" && c.Server.Env == "production" {
		return fmt.Errorf("auth.jwt_secret must be changed in production")
	}
	if len(c.Auth.JWTSecret) < 32 && c.Server.Env == "production" {
		return fmt.Errorf("auth.jwt_secret should be at least 32 characters in production")
	}
	if c.Auth.JWTExpiration <= 0 {
		return fmt.Errorf("auth.jwt_expiration must be positive")
	}

	// LLM
	if c.LLM.DefaultModel == "" {
		return fmt.Errorf("llm.default_model is required")
	}
	if c.LLM.BudgetPerTask < 0 {
		return fmt.Errorf("llm.budget_per_task must be non-negative")
	}
	if c.LLM.MaxTokens < 0 {
		return fmt.Errorf("llm.max_tokens must be non-negative")
	}
	if c.Server.Env == "production" && c.LLM.OpenAIKey == "" && c.LLM.AnthropicKey == "" && c.LLM.GeminiKey == "" && c.LLM.OpenRouterKey == "" && c.LLM.MistralKey == "" && c.LLM.GroqKey == "" && c.LLM.NVIDIANIMKey == "" && c.LLM.CohereKey == "" {
		return fmt.Errorf("at least one LLM API key is required in production")
	}

	// CORS: validate origin format for all configured origins (all environments)
	for _, o := range c.CORS.AllowedOrigins {
		if o == "*" {
			continue // wildcard handled by production check below
		}
		lower := strings.ToLower(o)
		if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
			return fmt.Errorf("cors.allowed_origins: %q is not a valid origin (must start with http:// or https://)", o)
		}
		// Reject origins with paths — CORS origins must be scheme + host + optional port
		rest := lower[7:]
		if strings.HasPrefix(lower, "https://") {
			rest = lower[8:]
		}
		if idx := strings.Index(rest, "/"); idx != -1 {
			return fmt.Errorf("cors.allowed_origins: %q must not contain a path (use https://example.com, not https://example.com/path)", o)
		}
	}
	if c.Server.Env == "production" {
		if len(c.CORS.AllowedOrigins) == 0 {
			return fmt.Errorf("cors.allowed_origins is required in production")
		}
		for _, o := range c.CORS.AllowedOrigins {
			if o == "*" {
				return fmt.Errorf("cors.allowed_origins must not contain wildcard '*' in production")
			}
		}
	}

	// Log
	if c.Log.Level != "" {
		validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
		if !validLevels[c.Log.Level] {
			return fmt.Errorf("log.level must be one of: debug, info, warn, error")
		}
	}

	return nil
}

func (c *RedisConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
