package config

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/spf13/viper"
)

// OnConfigChangeFunc is called when config changes. The new config is passed in.
type OnConfigChangeFunc func(newCfg *Config)

// HotReloader watches the config file for changes and triggers callbacks.
// It uses viper's built-in fsnotify watcher for file-based config.
type HotReloader struct {
	mu        sync.RWMutex
	callbacks []OnConfigChangeFunc
	cfg       *Config
	debounce  time.Duration
	cancel    context.CancelFunc
	done      chan struct{}
	startOnce sync.Once
	// reloadWG tracks in-flight debounced reloads so Stop/shutdown can wait
	// for them to finish touching viper before signaling completion — without
	// this, a straggler reload races teardown (e.g. viper.Reset() in tests).
	reloadWG sync.WaitGroup
}

// NewHotReloader creates a new hot reloader attached to the current viper config.
func NewHotReloader(cfg *Config) *HotReloader {
	return &HotReloader{
		cfg:      cfg,
		debounce: 500 * time.Millisecond,
		done:     make(chan struct{}),
	}
}

// OnChange registers a callback that fires when config is reloaded.
func (hr *HotReloader) OnChange(fn OnConfigChangeFunc) {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	hr.callbacks = append(hr.callbacks, fn)
}

// readFromViper reads all config fields from viper into a new Config struct.
// This avoids the redundant ReadInConfig call that Load() would trigger.
func readFromViper() *Config {
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
			StatementTimeout:   viper.GetDuration("database.statement_timeout"),
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
		BodySize: BodySizeConfig{
			MaxBodySize: viper.GetInt64("body_size.max_body_size"),
		},
		SecurityHeaders: SecurityHeadersConfig{
			Enabled:               viper.GetBool("security_headers.enabled"),
			HSTSMaxAge:            viper.GetInt("security_headers.hsts_max_age"),
			HSTSIncludeSubDomains: viper.GetBool("security_headers.hsts_include_subdomains"),
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
		Secrets: SecretsConfig{
			Backend:                 viper.GetString("secrets.backend"),
			Path:                    viper.GetString("secrets.path"),
			RotationDays:            viper.GetInt("secrets.rotation_days"),
			CredentialLeakDetection: viper.GetBool("secrets.credential_leak_detection"),
			VaultAddress:            viper.GetString("secrets.vault_address"),
			VaultToken:              viper.GetString("secrets.vault_token"),
			VaultMountPath:          viper.GetString("secrets.vault_mount_path"),
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
	}

	// Apply CORS defaults
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

	return cfg
}

// Start begins watching the config file for changes. Safe to call only once
// (protected by sync.Once). A debounce timer prevents rapid-fire reloads.
func (hr *HotReloader) Start(ctx context.Context) {
	hr.startOnce.Do(func() {
		ctx, cancel := context.WithCancel(ctx)
		hr.mu.Lock()
		hr.cancel = cancel
		hr.mu.Unlock()

		// Debounce timer — if multiple fsnotify events arrive within the debounce
		// window, only the last one triggers a reload.
		var debounceTimer *time.Timer

		viper.WatchConfig()
		viper.OnConfigChange(func(e fsnotify.Event) {
			// Once shutdown starts, stop touching viper and stop scheduling new
			// reloads: viper.WatchConfig() cannot be stopped, so a late event
			// would otherwise race teardown (e.g. viper.Reset() in tests).
			select {
			case <-ctx.Done():
				return
			default:
			}
			slog.Info("config file changed", "file", e.Name, "op", e.Op)

			hr.mu.RLock()
			debounce := hr.debounce
			hr.mu.RUnlock()

			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounce, func() {
				hr.reloadWG.Add(1)
				defer hr.reloadWG.Done()
				// Cancelled before the debounce elapsed: skip the viper round-trip
				// entirely (the caller may be tearing down viper state).
				select {
				case <-ctx.Done():
					return
				default:
				}
				// Force a fresh read so the reload does not race with viper's
				// internal auto-reload (which can be reordered relative to
				// externally registered OnConfigChange handlers).
				_ = viper.ReadInConfig()
				newCfg := readFromViper()

				if err := newCfg.Validate(); err != nil {
					slog.Error("reloaded config failed validation, keeping old config", "error", err)
					return
				}

				// Notify all registered callbacks
				hr.mu.RLock()
				callbacks := make([]OnConfigChangeFunc, len(hr.callbacks))
				copy(callbacks, hr.callbacks)
				hr.mu.RUnlock()

				for _, fn := range callbacks {
					fn(newCfg)
				}

				// Update the stored config
				hr.mu.Lock()
				hr.cfg = newCfg
				hr.mu.Unlock()

				// #nosec log_injection: structured key-value logging (the rule's own recommended safe pattern) - no format-string interpolation of user input
				slog.Info("config reloaded successfully")
			})
		})

		slog.Info("config hot reload started", "debounce_ms", hr.debounce.Milliseconds())

		// Wait for context cancellation
		<-ctx.Done()
		// Wait for any in-flight debounced reload to finish touching viper
		// before signaling completion, so callers can safely tear down
		// (e.g. viper.Reset()) without racing a straggler reload.
		hr.reloadWG.Wait()
		close(hr.done)
	})
}

// Stop stops the config watcher and waits for the goroutine to exit.
func (hr *HotReloader) Stop() {
	hr.mu.RLock()
	cancel := hr.cancel
	done := hr.done
	hr.mu.RUnlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		<-done
	}
}

// Config returns the current (possibly reloaded) config.
func (hr *HotReloader) Config() *Config {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	return hr.cfg
}
