package config

import (
	"fmt"
	"os"
	"strings"
)

// ValidateProduction checks for insecure defaults that must not be used in production.
func ValidateProduction(cfg *Config) error {
	if cfg.Server.Env != "production" {
		return nil
	}

	var errs []string

	// Reject default JWT secret in production
	if cfg.Auth.JWTSecret == "" {
		errs = append(errs, "VIGILAGENT_JWT_SECRET must be set in production")
	}
	if cfg.Auth.JWTSecret == "change-me-in-production" || cfg.Auth.JWTSecret == "secret" || cfg.Auth.JWTSecret == "default" {
		errs = append(errs, "VIGILAGENT_JWT_SECRET must not be a default/insecure value in production")
	}
	if len(cfg.Auth.JWTSecret) < 32 {
		errs = append(errs, "VIGILAGENT_JWT_SECRET must be at least 32 characters in production")
	}

	// Check for placeholder API keys
	placeholders := []struct{ key, name string }{
		{cfg.LLM.OpenAIKey, "VIGILAGENT_OPENAI_API_KEY"},
		{cfg.LLM.AnthropicKey, "VIGILAGENT_ANTHROPIC_API_KEY"},
	}
	for _, p := range placeholders {
		if p.key != "" && (strings.Contains(strings.ToLower(p.key), "placeholder") || strings.Contains(strings.ToLower(p.key), "sk-xxx")) {
			errs = append(errs, fmt.Sprintf("%s appears to be a placeholder value", p.name))
		}
	}

	// Check database Host
	if cfg.Database.Host == "" || cfg.Database.Host == "localhost" || cfg.Database.Host == "127.0.0.1" {
		errs = append(errs, "VIGILAGENT_DATABASE_HOST must be set to a real database in production")
	}

	// Check Redis Host
	if cfg.Redis.Host == "" || cfg.Redis.Host == "localhost" || cfg.Redis.Host == "127.0.0.1" {
		errs = append(errs, "VIGILAGENT_REDIS_HOST must be set in production")
	}

	if len(errs) > 0 {
		return fmt.Errorf("production configuration errors:\n  - %s", strings.Join(errs, "\n  - "))
	}

	return nil
}

// ValidateProductionEnv reads env vars directly for fast-fail checks before config is loaded.
// Returns an error instead of calling os.Exit so the caller decides how to handle it.
func ValidateProductionEnv() error {
	env := os.Getenv("VIGILAGENT_ENV")
	if env != "production" {
		return nil
	}

	jwtSecret := os.Getenv("VIGILAGENT_JWT_SECRET")
	if jwtSecret == "" {
		return fmt.Errorf("FATAL: VIGILAGENT_JWT_SECRET must be set in production")
	}
	if jwtSecret == "change-me-in-production" || jwtSecret == "secret" || jwtSecret == "default" {
		return fmt.Errorf("FATAL: VIGILAGENT_JWT_SECRET must not be a default value in production")
	}
	if len(jwtSecret) < 32 {
		return fmt.Errorf("FATAL: VIGILAGENT_JWT_SECRET must be at least 32 characters in production")
	}

	return nil
}
