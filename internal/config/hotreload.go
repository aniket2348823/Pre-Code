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
		},
		Auth: AuthConfig{
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
		ctx, hr.cancel = context.WithCancel(ctx)

		// Debounce timer — if multiple fsnotify events arrive within the debounce
		// window, only the last one triggers a reload.
		var debounceTimer *time.Timer

		viper.WatchConfig()
		viper.OnConfigChange(func(e fsnotify.Event) {
			slog.Info("config file changed", "file", e.Name, "op", e.Op)

			hr.mu.RLock()
			debounce := hr.debounce
			hr.mu.RUnlock()

			if debounceTimer != nil {
				debounceTimer.Stop()
			}
			debounceTimer = time.AfterFunc(debounce, func() {
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

				slog.Info("config reloaded successfully")
			})
		})

		slog.Info("config hot reload started", "debounce_ms", hr.debounce.Milliseconds())

		// Wait for context cancellation
		<-ctx.Done()
		close(hr.done)
	})
}

// Stop stops the config watcher and waits for the goroutine to exit.
func (hr *HotReloader) Stop() {
	if hr.cancel != nil {
		hr.cancel()
	}
	if hr.done != nil {
		<-hr.done
	}
}

// Config returns the current (possibly reloaded) config.
func (hr *HotReloader) Config() *Config {
	hr.mu.RLock()
	defer hr.mu.RUnlock()
	return hr.cfg
}
