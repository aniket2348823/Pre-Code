package config

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server          ServerConfig
	Database        DatabaseConfig
	Redis           RedisConfig
	NATS            NATSConfig
	Auth            AuthConfig
	LLM             LLMConfig
	Stripe          StripeConfig
	CORS            CORSConfig
	Log             LogConfig
	SMTP            SMTPConfig
	SendGrid        SendGridConfig
	BodySize        BodySizeConfig
	SecurityHeaders SecurityHeadersConfig
	Secrets         SecretsConfig
	Audit           AuditConfig
	IPAnomaly       IPAnomalyConfig
}

// AuditConfig configures audit log retention and cleanup.
type AuditConfig struct {
	RetentionDays     int // Delete audit events older than this many days
	MaxStorageMB      int // Maximum audit log storage in MB before alerting
	CleanupInterval   time.Duration
	CompressAfterDays int // Compress events older than this many days
	AlertThresholdMB  int // Alert when storage exceeds this threshold
}

// IPAnomalyConfig configures IP-based anomaly detection.
type IPAnomalyConfig struct {
	Enabled              bool
	BruteForceThreshold  int           // requests per minute to trigger brute force detection
	PortScanThreshold    int           // unique 404 endpoints to trigger port scan detection
	CredentialStufThresh int           // login attempts with different emails to trigger detection
	ScoreThreshold       int           // anomaly score threshold (0-100) for action
	BlockDuration        time.Duration // how long to block a flagged IP
	TrackingWindow       time.Duration // sliding window for request tracking
}

// BodySizeConfig configures request body size limits.
type BodySizeConfig struct {
	MaxBodySize int64 // Maximum request body size in bytes (default 10MB)
}

type ServerConfig struct {
	Host              string
	Port              int
	Env               string
	BaseURL           string
	RateLimitPerMin   int
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
}

type DatabaseConfig struct {
	Host               string
	Port               int
	User               string
	Password           string
	Name               string
	SSLMode            string
	MaxOpenConns       int
	MaxIdleConns       int
	MaxLifetime        time.Duration
	ConnIdleTime       time.Duration // MaxConnIdleTime — how long idle connections stay open (default 5min)
	PoolMaxOpen        int           // explicit pool max open (default 25, overrides MaxOpenConns if > 0)
	PoolMaxIdle        int           // explicit pool max idle (default 5)
	PoolMaxLifetime    time.Duration // explicit pool max connection lifetime (default 5m)
	PoolMaxIdleTime    time.Duration // explicit pool max idle time (default 3m)
	SlowQueryThreshold time.Duration // log queries slower than this (default 100ms)
	RetryMaxAttempts   int           // max retry attempts for transient errors (default 3)
	PoolStatsInterval  time.Duration // interval for periodic pool stats logging (default 30s)
	StatementTimeout   time.Duration // per-statement timeout applied via SET statement_timeout (0 = disabled)
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
	JWTSecret          string
	JWTExpiration      time.Duration
	JWTAudience        string
	JWTBindToIP        bool
	JWTBindToUserAgent bool
	APIKeyPrefix       string
}

// LLMConfig holds LLM provider API keys and routing config.
// Each key is optional; providers are only registered when their key is set.
type LLMConfig struct {
	OpenAIKey     string
	AnthropicKey  string
	GeminiKey     string
	OpenRouterKey string
	MistralKey    string
	GroqKey       string
	NVIDIANIMKey  string
	CohereKey     string
	DefaultModel  string
	BudgetPerTask float64
	MaxTokens     int
}

type StripeConfig struct {
	SecretKey     string
	WebhookSecret string
	SuccessURL    string
	CancelURL     string
}

// CORSConfig holds CORS middleware configuration.
type CORSConfig struct {
	AllowedOrigins       []string
	AllowedMethods       []string
	AllowedHeaders       []string
	AllowCredentials     bool
	MaxAge               int
	AllowInsecureOrigins bool // only for dev mode, default false
}

type LogConfig struct {
	Level  string
	Format string
}

// SMTPConfig holds email/SMTP server configuration.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	FromName string
}

// SendGridConfig holds SendGrid API configuration.
type SendGridConfig struct {
	APIKey    string
	FromEmail string
	FromName  string
}

// SecretsConfig holds secrets management configuration.
type SecretsConfig struct {
	Backend                 string // vault, env, file
	Path                    string // secrets path/prefix
	RotationDays            int    // default rotation interval (days)
	CredentialLeakDetection bool   // enable outbound credential scanning
	VaultAddress            string // HashiCorp Vault address
	VaultToken              string // Vault auth token
	VaultMountPath          string // Vault KV mount path
}

// SecurityHeadersConfig holds security header middleware configuration.
type SecurityHeadersConfig struct {
	Enabled               bool
	HSTSMaxAge            int
	HSTSIncludeSubDomains bool
	HSTSPreload           bool
	CSP                   string
	XContentTypeOptions   bool
	XFrameOptions         string
	ReferrerPolicy        string
	PermissionsPolicy     string
	XSSProtection         string
	CacheControlAPI       string
	CacheControlStatic    string
	CustomHeaders         map[string]string
}

func Load() (*Config, error) {
	// Auto-load .env file if present
	loadEnvFile(".env")

	// Support custom config path via env var (used in Docker)
	if configPath := os.Getenv("VIGILAGENT_CONFIG_PATH"); configPath != "" {
		viper.SetConfigFile(configPath)
	} else {
		viper.SetConfigName("config")
		viper.SetConfigType("yaml")
		viper.AddConfigPath("./configs")
		viper.AddConfigPath(".")
	}

	// Set defaults
	viper.SetDefault("server.host", "0.0.0.0")
	viper.SetDefault("server.port", 8080)
	viper.SetDefault("server.env", "development")
	viper.SetDefault("server.base_url", "http://localhost:8080")
	viper.SetDefault("server.rate_limit_per_min", 10000)
	viper.SetDefault("server.read_timeout", 30*time.Second)
	viper.SetDefault("server.read_header_timeout", 10*time.Second)
	viper.SetDefault("server.write_timeout", 60*time.Second)
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
	viper.SetDefault("database.conn_idle_time", 5*time.Minute)
	viper.SetDefault("database.pool_max_open", 25)
	viper.SetDefault("database.pool_max_idle", 5)
	viper.SetDefault("database.pool_max_lifetime", 5*time.Minute)
	viper.SetDefault("database.pool_max_idle_time", 3*time.Minute)
	viper.SetDefault("database.slow_query_threshold", 100*time.Millisecond)
	viper.SetDefault("database.retry_max_attempts", 3)
	viper.SetDefault("database.pool_stats_interval", 30*time.Second)

	viper.SetDefault("redis.host", "localhost")
	viper.SetDefault("redis.port", 6379)
	viper.SetDefault("redis.password", "")
	viper.SetDefault("redis.db", 0)

	viper.SetDefault("nats.url", "nats://localhost:4222")
	viper.SetDefault("nats.stream", "vigilagent")

	viper.SetDefault("auth.jwt_secret", "")
	viper.SetDefault("auth.jwt_expiration", 24*time.Hour)
	viper.SetDefault("auth.jwt_audience", "vigilagent-api")
	viper.SetDefault("auth.jwt_bind_to_ip", false)
	viper.SetDefault("auth.jwt_bind_to_user_agent", false)
	viper.SetDefault("auth.api_key_prefix", "va_")

	// LLM defaults
	viper.SetDefault("llm.default_model", "claude-sonnet-4-20250514")
	viper.SetDefault("llm.budget_per_task", 1.0)
	viper.SetDefault("llm.max_tokens", 8192)

	// SMTP defaults
	viper.SetDefault("smtp.port", 587)

	// Security headers defaults
	viper.SetDefault("security_headers.enabled", true)
	viper.SetDefault("security_headers.hsts_max_age", 63072000)
	viper.SetDefault("security_headers.hsts_include_sub_domains", true)
	viper.SetDefault("security_headers.hsts_preload", true)
	viper.SetDefault("security_headers.csp", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: https:; font-src 'self'; connect-src 'self' https:; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
	viper.SetDefault("security_headers.x_content_type_options", true)
	viper.SetDefault("security_headers.x_frame_options", "DENY")
	viper.SetDefault("security_headers.referrer_policy", "strict-origin-when-cross-origin")
	viper.SetDefault("security_headers.permissions_policy", "camera=(), microphone=(), geolocation=(), payment=()")
	viper.SetDefault("security_headers.xss_protection", "1; mode=block")
	viper.SetDefault("security_headers.cache_control_api", "no-store, no-cache, must-revalidate")
	viper.SetDefault("security_headers.cache_control_static", "public, max-age=31536000")

	// Audit log defaults
	viper.SetDefault("audit.retention_days", 90)
	viper.SetDefault("audit.max_storage_mb", 1024)
	viper.SetDefault("audit.cleanup_interval", 1*time.Hour)
	viper.SetDefault("audit.compress_after_days", 30)
	viper.SetDefault("audit.alert_threshold_mb", 800)

	// IP anomaly detection defaults
	viper.SetDefault("ip_anomaly.enabled", true)
	viper.SetDefault("ip_anomaly.brute_force_threshold", 100)
	viper.SetDefault("ip_anomaly.port_scan_threshold", 50)
	viper.SetDefault("ip_anomaly.credential_stuf_thresh", 20)
	viper.SetDefault("ip_anomaly.score_threshold", 70)
	viper.SetDefault("ip_anomaly.block_duration", 30*time.Minute)
	viper.SetDefault("ip_anomaly.tracking_window", 5*time.Minute)

	viper.SetDefault("body_size.max_body_size", 10<<20) // 10 MB

	// Secrets defaults
	viper.SetDefault("secrets.backend", "env")
	viper.SetDefault("secrets.path", "")
	viper.SetDefault("secrets.rotation_days", 90)
	viper.SetDefault("secrets.credential_leak_detection", false)
	viper.SetDefault("secrets.vault_address", "")
	viper.SetDefault("secrets.vault_token", "")
	viper.SetDefault("secrets.vault_mount_path", "secret")

	viper.SetDefault("log.level", "info")
	viper.SetDefault("log.format", "json")

	// Enable environment variable override
	viper.AutomaticEnv()
	viper.SetEnvPrefix("VIGILAGENT")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))

	// Read config file (if exists)
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
	}

	cfg := &Config{
		Server: ServerConfig{
			Host:              viper.GetString("server.host"),
			Port:              viper.GetInt("server.port"),
			Env:               viper.GetString("server.env"),
			BaseURL:           viper.GetString("server.base_url"),
			RateLimitPerMin:   viper.GetInt("server.rate_limit_per_min"),
			ReadTimeout:       viper.GetDuration("server.read_timeout"),
			ReadHeaderTimeout: viper.GetDuration("server.read_header_timeout"),
			WriteTimeout:      viper.GetDuration("server.write_timeout"),
			IdleTimeout:       viper.GetDuration("server.idle_timeout"),
		},
		Database: DatabaseConfig{
			Host:               viper.GetString("database.host"),
			Port:               viper.GetInt("database.port"),
			User:               viper.GetString("database.user"),
			Password:           viper.GetString("database.password"),
			Name:               viper.GetString("database.name"),
			SSLMode:            viper.GetString("database.sslmode"),
			MaxOpenConns:       viper.GetInt("database.max_open_conns"),
			MaxIdleConns:       viper.GetInt("database.max_idle_conns"),
			MaxLifetime:        viper.GetDuration("database.max_lifetime"),
			ConnIdleTime:       viper.GetDuration("database.conn_idle_time"),
			PoolMaxOpen:        viper.GetInt("database.pool_max_open"),
			PoolMaxIdle:        viper.GetInt("database.pool_max_idle"),
			PoolMaxLifetime:    viper.GetDuration("database.pool_max_lifetime"),
			PoolMaxIdleTime:    viper.GetDuration("database.pool_max_idle_time"),
			SlowQueryThreshold: viper.GetDuration("database.slow_query_threshold"),
			RetryMaxAttempts:   viper.GetInt("database.retry_max_attempts"),
			PoolStatsInterval:  viper.GetDuration("database.pool_stats_interval"),
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
		},
		Auth: AuthConfig{
			JWTSecret:          viper.GetString("auth.jwt_secret"),
			JWTExpiration:      viper.GetDuration("auth.jwt_expiration"),
			JWTAudience:        viper.GetString("auth.jwt_audience"),
			JWTBindToIP:        viper.GetBool("auth.jwt_bind_to_ip"),
			JWTBindToUserAgent: viper.GetBool("auth.jwt_bind_to_user_agent"),
			APIKeyPrefix:       viper.GetString("auth.api_key_prefix"),
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
		SMTP: SMTPConfig{
			Host:     viper.GetString("smtp.host"),
			Port:     viper.GetInt("smtp.port"),
			Username: viper.GetString("smtp.username"),
			Password: viper.GetString("smtp.password"),
			From:     viper.GetString("smtp.from"),
			FromName: viper.GetString("smtp.from_name"),
		},
		SendGrid: SendGridConfig{
			APIKey:    viper.GetString("sendgrid.api_key"),
			FromEmail: viper.GetString("sendgrid.from_email"),
			FromName:  viper.GetString("sendgrid.from_name"),
		},
		CORS: CORSConfig{
			AllowedOrigins:   viper.GetStringSlice("cors.allowed_origins"),
			AllowedMethods:   viper.GetStringSlice("cors.allowed_methods"),
			AllowedHeaders:   viper.GetStringSlice("cors.allowed_headers"),
			AllowCredentials: viper.GetBool("cors.allow_credentials"),
			MaxAge:           viper.GetInt("cors.max_age"),
		},
		Log: LogConfig{
			Level:  viper.GetString("log.level"),
			Format: viper.GetString("log.format"),
		},
		SecurityHeaders: SecurityHeadersConfig{
			Enabled:               viper.GetBool("security_headers.enabled"),
			HSTSMaxAge:            viper.GetInt("security_headers.hsts_max_age"),
			HSTSIncludeSubDomains: viper.GetBool("security_headers.hsts_include_sub_domains"),
			HSTSPreload:           viper.GetBool("security_headers.hsts_preload"),
			CSP:                   viper.GetString("security_headers.csp"),
			XContentTypeOptions:   viper.GetBool("security_headers.x_content_type_options"),
			XFrameOptions:         viper.GetString("security_headers.x_frame_options"),
			ReferrerPolicy:        viper.GetString("security_headers.referrer_policy"),
			PermissionsPolicy:     viper.GetString("security_headers.permissions_policy"),
			XSSProtection:         viper.GetString("security_headers.xss_protection"),
			CacheControlAPI:       viper.GetString("security_headers.cache_control_api"),
			CacheControlStatic:    viper.GetString("security_headers.cache_control_static"),
			CustomHeaders:         viper.GetStringMapString("security_headers.custom_headers"),
		},
		BodySize: BodySizeConfig{
			MaxBodySize: viper.GetInt64("body_size.max_body_size"),
		},
		Audit: AuditConfig{
			RetentionDays:     viper.GetInt("audit.retention_days"),
			MaxStorageMB:      viper.GetInt("audit.max_storage_mb"),
			CleanupInterval:   viper.GetDuration("audit.cleanup_interval"),
			CompressAfterDays: viper.GetInt("audit.compress_after_days"),
			AlertThresholdMB:  viper.GetInt("audit.alert_threshold_mb"),
		},
		IPAnomaly: IPAnomalyConfig{
			Enabled:              viper.GetBool("ip_anomaly.enabled"),
			BruteForceThreshold:  viper.GetInt("ip_anomaly.brute_force_threshold"),
			PortScanThreshold:    viper.GetInt("ip_anomaly.port_scan_threshold"),
			CredentialStufThresh: viper.GetInt("ip_anomaly.credential_stuf_thresh"),
			ScoreThreshold:       viper.GetInt("ip_anomaly.score_threshold"),
			BlockDuration:        viper.GetDuration("ip_anomaly.block_duration"),
			TrackingWindow:       viper.GetDuration("ip_anomaly.tracking_window"),
		},
		Secrets: SecretsConfig{
			Backend:                 viper.GetString("secrets.backend"),
			Path:                    viper.GetString("secrets.path"),
			RotationDays:            viper.GetInt("secrets.rotation_days"),
			CredentialLeakDetection: viper.GetBool("secrets.credential_leak_detection"),
			VaultAddress:            viper.GetString("secrets.vault_address"),
			VaultToken:              viper.GetString("secrets.vault_token"),
			VaultMountPath:          viper.GetString("secrets.vault_mount_path"),
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
	if c.Database.RetryMaxAttempts < 0 {
		return fmt.Errorf("database.retry_max_attempts must be non-negative")
	}
	if c.Database.SlowQueryThreshold < 0 {
		return fmt.Errorf("database.slow_query_threshold must be non-negative")
	}
	// PoolMaxOpen == 0 means "unset" (default 25 applied in Load()), so only
	// enforce the invariant when both fields are explicitly configured.
	if c.Database.PoolMaxOpen > 0 && c.Database.PoolMaxIdle > c.Database.PoolMaxOpen {
		return fmt.Errorf("database pool min conns (%d) must not exceed max conns (%d)", c.Database.PoolMaxIdle, c.Database.PoolMaxOpen)
	}
	if c.Database.StatementTimeout < 0 {
		return fmt.Errorf("database.statement_timeout must be non-negative")
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
	if c.Auth.JWTSecret == "change-me-in-production" {
		return fmt.Errorf("auth.jwt_secret must be changed from default value")
	}
	if c.Auth.JWTSecret == "secret" || c.Auth.JWTSecret == "default" {
		return fmt.Errorf("auth.jwt_secret must not be a common weak secret")
	}
	if len(c.Auth.JWTSecret) < 32 {
		return fmt.Errorf("auth.jwt_secret must be at least 32 characters")
	}
	if c.Auth.JWTExpiration <= 0 {
		return fmt.Errorf("auth.jwt_expiration must be positive")
	}

	// Production DB safety
	if c.Server.Env == "production" {
		if c.Database.Password == "vigilagent" {
			return fmt.Errorf("database.password must be changed from default in production")
		}
		if c.Database.SSLMode == "disable" {
			return fmt.Errorf("database.sslmode must not be 'disable' in production")
		}
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
		// Accept subdomain patterns like *.example.com
		if strings.HasPrefix(o, "*.") {
			// validate the suffix part is a valid domain
			suffix := o[1:] // ".example.com"
			if !strings.Contains(suffix, ".") {
				return fmt.Errorf("cors.allowed_origins: %q is not a valid subdomain pattern", o)
			}
			continue
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

	// Audit — only validated when explicitly configured (defaults are applied in Load()).
	if c.Audit.RetentionDays > 0 {
		if c.Audit.CompressAfterDays <= 0 {
			return fmt.Errorf("audit.compress_after_days must be positive")
		}
		if c.Audit.CompressAfterDays >= c.Audit.RetentionDays {
			return fmt.Errorf("audit.compress_after_days must be less than retention_days")
		}
	}

	// IP Anomaly — only validated when explicitly configured (defaults applied in Load()).
	if c.IPAnomaly.BruteForceThreshold > 0 {
		if c.IPAnomaly.ScoreThreshold < 0 || c.IPAnomaly.ScoreThreshold > 100 {
			return fmt.Errorf("ip_anomaly.score_threshold must be between 0 and 100")
		}
	}

	return nil
}

func (c *RedisConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func loadEnvFile(path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
}
