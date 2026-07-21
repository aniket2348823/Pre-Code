package config

import "time"

// MiddlewareConfig holds declarative middleware configuration.
type MiddlewareConfig struct {
	RateLimit    *RateLimitMWConfig `yaml:"rate_limit" json:"rate_limit,omitempty"`
	Timeout      *TimeoutMWConfig   `yaml:"timeout" json:"timeout,omitempty"`
	CORS         *CORSMWConfig      `yaml:"cors" json:"cors,omitempty"`
	Recovery     *RecoveryConfig    `yaml:"recovery" json:"recovery,omitempty"`
	RequestBody   *RequestBodyConfig `yaml:"request_body" json:"request_body,omitempty"`
}

// RateLimitMWConfig configures the rate limiting middleware.
type RateLimitMWConfig struct {
	Enabled       bool          `yaml:"enabled" json:"enabled"`
	Limit         int           `yaml:"limit" json:"limit"`
	Window        time.Duration `yaml:"window" json:"window"`
	KeyFunc       string        `yaml:"key_func" json:"key_func"` // "ip", "user", "api_key"
	TrustedHeader string        `yaml:"trusted_header" json:"trusted_header,omitempty"`
}

// TimeoutMWConfig configures request timeout.
type TimeoutMWConfig struct {
	Enabled    bool          `yaml:"enabled" json:"enabled"`
	Timeout    time.Duration `yaml:"timeout" json:"timeout"`
	Message    string        `yaml:"message" json:"message,omitempty"`
}

// CORSMWConfig configures CORS middleware.
type CORSMWConfig struct {
	Enabled         bool     `yaml:"enabled" json:"enabled"`
	AllowedOrigins  []string `yaml:"allowed_origins" json:"allowed_origins"`
	AllowedMethods  []string `yaml:"allowed_methods" json:"allowed_methods"`
	AllowedHeaders  []string `yaml:"allowed_headers" json:"allowed_headers"`
	ExposedHeaders  []string `yaml:"exposed_headers" json:"exposed_headers,omitempty"`
	AllowCredentials bool    `yaml:"allow_credentials" json:"allow_credentials"`
	MaxAge          int      `yaml:"max_age" json:"max_age"`
}

// RecoveryConfig configures the panic recovery middleware.
type RecoveryConfig struct {
	Enabled bool `yaml:"enabled" json:"enabled"`
}

// RequestBodyConfig configures request body limits.
type RequestBodyConfig struct {
	Enabled    bool  `yaml:"enabled" json:"enabled"`
	MaxBytes   int64 `yaml:"max_bytes" json:"max_bytes"`
}

// DefaultMiddlewareConfig returns sensible defaults for development.
func DefaultMiddlewareConfig() MiddlewareConfig {
	return MiddlewareConfig{
		RateLimit: &RateLimitMWConfig{
			Enabled: true,
			Limit:   100,
			Window:  time.Minute,
			KeyFunc: "user",
		},
		Timeout: &TimeoutMWConfig{
			Enabled: true,
			Timeout: 30 * time.Second,
			Message: "request timeout",
		},
		CORS: &CORSMWConfig{
			Enabled:        true,
			AllowedOrigins: []string{"*"},
			AllowedMethods: []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders: []string{"Accept", "Authorization", "Content-Type", "X-API-Key"},
			MaxAge:         86400,
		},
		Recovery: &RecoveryConfig{
			Enabled: true,
		},
		RequestBody: &RequestBodyConfig{
			Enabled:  true,
			MaxBytes: 10 << 20, // 10MB
		},
	}
}
