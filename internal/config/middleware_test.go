package config

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultMiddlewareConfig_AllFields(t *testing.T) {
	cfg := DefaultMiddlewareConfig()

	// Rate limit
	require.NotNil(t, cfg.RateLimit)
	assert.True(t, cfg.RateLimit.Enabled)
	assert.Equal(t, 100, cfg.RateLimit.Limit)
	assert.Equal(t, time.Minute, cfg.RateLimit.Window)
	assert.Equal(t, "user", cfg.RateLimit.KeyFunc)

	// Timeout
	require.NotNil(t, cfg.Timeout)
	assert.True(t, cfg.Timeout.Enabled)
	assert.Equal(t, 30*time.Second, cfg.Timeout.Timeout)
	assert.Equal(t, "request timeout", cfg.Timeout.Message)

	// CORS
	require.NotNil(t, cfg.CORS)
	assert.True(t, cfg.CORS.Enabled)
	assert.Equal(t, []string{"*"}, cfg.CORS.AllowedOrigins)
	assert.Contains(t, cfg.CORS.AllowedMethods, "GET")
	assert.Contains(t, cfg.CORS.AllowedMethods, "POST")
	assert.Contains(t, cfg.CORS.AllowedMethods, "PUT")
	assert.Contains(t, cfg.CORS.AllowedMethods, "PATCH")
	assert.Contains(t, cfg.CORS.AllowedMethods, "DELETE")
	assert.Contains(t, cfg.CORS.AllowedMethods, "OPTIONS")
	assert.Contains(t, cfg.CORS.AllowedHeaders, "Accept")
	assert.Contains(t, cfg.CORS.AllowedHeaders, "Authorization")
	assert.Contains(t, cfg.CORS.AllowedHeaders, "Content-Type")
	assert.Contains(t, cfg.CORS.AllowedHeaders, "X-API-Key")
	assert.Equal(t, 86400, cfg.CORS.MaxAge)

	// Recovery
	require.NotNil(t, cfg.Recovery)
	assert.True(t, cfg.Recovery.Enabled)

	// Request body
	require.NotNil(t, cfg.RequestBody)
	assert.True(t, cfg.RequestBody.Enabled)
	assert.Equal(t, int64(10<<20), cfg.RequestBody.MaxBytes)
}

func TestMiddlewareConfig_ZeroValue(t *testing.T) {
	cfg := MiddlewareConfig{}
	assert.Nil(t, cfg.RateLimit)
	assert.Nil(t, cfg.Timeout)
	assert.Nil(t, cfg.CORS)
	assert.Nil(t, cfg.Recovery)
	assert.Nil(t, cfg.RequestBody)
}

func TestRateLimitMWConfig_Fields(t *testing.T) {
	cfg := RateLimitMWConfig{
		Enabled:       true,
		Limit:         50,
		Window:        5 * time.Minute,
		KeyFunc:       "api_key",
		TrustedHeader: "X-Forwarded-For",
	}
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 50, cfg.Limit)
	assert.Equal(t, 5*time.Minute, cfg.Window)
	assert.Equal(t, "api_key", cfg.KeyFunc)
	assert.Equal(t, "X-Forwarded-For", cfg.TrustedHeader)
}

func TestTimeoutMWConfig_Fields(t *testing.T) {
	cfg := TimeoutMWConfig{
		Enabled: true,
		Timeout: 60 * time.Second,
		Message: "timeout exceeded",
	}
	assert.True(t, cfg.Enabled)
	assert.Equal(t, 60*time.Second, cfg.Timeout)
	assert.Equal(t, "timeout exceeded", cfg.Message)
}

func TestCORSMWConfig_Fields(t *testing.T) {
	cfg := CORSMWConfig{
		Enabled:          true,
		AllowedOrigins:   []string{"https://app.com"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowedHeaders:   []string{"Content-Type"},
		ExposedHeaders:   []string{"X-Custom"},
		AllowCredentials: true,
		MaxAge:           3600,
	}
	assert.True(t, cfg.Enabled)
	assert.Equal(t, []string{"https://app.com"}, cfg.AllowedOrigins)
	assert.Equal(t, []string{"GET", "POST"}, cfg.AllowedMethods)
	assert.Equal(t, []string{"Content-Type"}, cfg.AllowedHeaders)
	assert.Equal(t, []string{"X-Custom"}, cfg.ExposedHeaders)
	assert.True(t, cfg.AllowCredentials)
	assert.Equal(t, 3600, cfg.MaxAge)
}

func TestCORSMWConfig_Empty(t *testing.T) {
	cfg := CORSMWConfig{}
	assert.False(t, cfg.Enabled)
	assert.Nil(t, cfg.AllowedOrigins)
	assert.Nil(t, cfg.AllowedMethods)
	assert.Nil(t, cfg.AllowedHeaders)
	assert.Nil(t, cfg.ExposedHeaders)
	assert.False(t, cfg.AllowCredentials)
	assert.Equal(t, 0, cfg.MaxAge)
}

func TestRecoveryConfig_Fields(t *testing.T) {
	cfg := RecoveryConfig{Enabled: true}
	assert.True(t, cfg.Enabled)

	cfg2 := RecoveryConfig{Enabled: false}
	assert.False(t, cfg2.Enabled)
}

func TestRequestBodyConfig_Fields(t *testing.T) {
	cfg := RequestBodyConfig{
		Enabled:  true,
		MaxBytes: 5 << 20, // 5MB
	}
	assert.True(t, cfg.Enabled)
	assert.Equal(t, int64(5<<20), cfg.MaxBytes)
}

func TestRequestBodyConfig_ZeroValue(t *testing.T) {
	cfg := RequestBodyConfig{}
	assert.False(t, cfg.Enabled)
	assert.Equal(t, int64(0), cfg.MaxBytes)
}

func TestDefaultMiddlewareConfig_RateLimitKeyFuncs(t *testing.T) {
	cfg := DefaultMiddlewareConfig()
	// Default key function should be "user"
	assert.Equal(t, "user", cfg.RateLimit.KeyFunc)

	// Test various key function values
	for _, kf := range []string{"ip", "user", "api_key"} {
		cfg.RateLimit.KeyFunc = kf
		assert.Equal(t, kf, cfg.RateLimit.KeyFunc)
	}
}

func TestMiddlewareConfig_JSONTags(t *testing.T) {
	cfg := MiddlewareConfig{
		RateLimit: &RateLimitMWConfig{Enabled: true, Limit: 100, Window: time.Minute},
		Timeout:   &TimeoutMWConfig{Enabled: true, Timeout: 30 * time.Second},
	}

	// Verify JSON marshaling works with the tags
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(data), "rate_limit")
	assert.Contains(t, string(data), "timeout")
}

func TestRateLimitMWConfig_JSONTags(t *testing.T) {
	cfg := RateLimitMWConfig{
		Enabled: true,
		Limit:   50,
		Window:  time.Minute,
		KeyFunc: "ip",
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(data), "enabled")
	assert.Contains(t, string(data), "limit")
	assert.Contains(t, string(data), "window")
	assert.Contains(t, string(data), "key_func")
}

func TestCORSMWConfig_JSONTags(t *testing.T) {
	cfg := CORSMWConfig{
		Enabled:          true,
		AllowedOrigins:   []string{"*"},
		AllowCredentials: true,
		MaxAge:           86400,
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.Contains(t, string(data), "allowed_origins")
	assert.Contains(t, string(data), "allow_credentials")
	assert.Contains(t, string(data), "max_age")
}
