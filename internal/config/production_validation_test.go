package config

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateProduction_NonProdEnvSkipsChecks(t *testing.T) {
	cfg := &Config{Server: ServerConfig{Env: "development"}}
	err := ValidateProduction(cfg)
	assert.NoError(t, err)
}

func TestValidateProduction_EmptyEnvSkipsChecks(t *testing.T) {
	cfg := &Config{Server: ServerConfig{Env: ""}}
	err := ValidateProduction(cfg)
	assert.NoError(t, err)
}

func TestValidateProduction_MissingJWTSecret(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: ""},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET must be set")
}

func TestValidateProduction_InsecureJWTChangeMe(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "change-me-in-production"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default/insecure value")
}

func TestValidateProduction_InsecureJWTSecret(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "secret"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default/insecure value")
}

func TestValidateProduction_InsecureJWTDefault(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "default"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default/insecure value")
}

func TestValidateProduction_JWTTooShort(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "short"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 32 characters")
}

func TestValidateProduction_JWT31Chars(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "1234567890123456789012345678901"}, // 31 chars
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 32 characters")
}

func TestValidateProduction_JWT32CharsOK(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "12345678901234567890123456789012"}, // 32 chars
	}
	err := ValidateProduction(cfg)
	assert.NotContains(t, err.Error(), "JWT_SECRET")
}

func TestValidateProduction_OpenAIPlaceholder(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		LLM:    LLMConfig{OpenAIKey: "sk-xxx-placeholder"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "OPENAI_API_KEY appears to be a placeholder")
}

func TestValidateProduction_AnthropicPlaceholder(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		LLM:    LLMConfig{AnthropicKey: "PLACEHOLDER-key"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "ANTHROPIC_API_KEY appears to be a placeholder")
}

func TestValidateProduction_PlaceholderUpperCase(t *testing.T) {
	cfg := &Config{
		Server: ServerConfig{Env: "production"},
		Auth:   AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		LLM:    LLMConfig{OpenAIKey: "SK-XXX-PLACEHOLDER"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "placeholder")
}

func TestValidateProduction_LocalhostDBProd(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "localhost"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_HOST must be set to a real database")
}

func TestValidateProduction_127001DBProd(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "127.0.0.1"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_HOST")
}

func TestValidateProduction_EmptyDBProd(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: ""},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_HOST")
}

func TestValidateProduction_LocalhostRedisProd(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "db.prod.com"},
		Redis:    RedisConfig{Host: "localhost"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "REDIS_HOST must be set")
}

func TestValidateProduction_127001RedisProd(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "db.prod.com"},
		Redis:    RedisConfig{Host: "127.0.0.1"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "REDIS_HOST must be set")
}

func TestValidateProduction_EmptyRedisProd(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "db.prod.com"},
		Redis:    RedisConfig{Host: ""},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "REDIS_HOST must be set")
}

func TestValidateProduction_AllValidProd(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "db.prod.example.com"},
		Redis:    RedisConfig{Host: "redis.prod.example.com"},
		LLM:      LLMConfig{OpenAIKey: "sk-real-key"},
	}
	err := ValidateProduction(cfg)
	assert.NoError(t, err)
}

func TestValidateProduction_AggregatesMultipleErrors(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: ""},
		Database: DatabaseConfig{Host: "localhost"},
		Redis:    RedisConfig{Host: "localhost"},
	}
	err := ValidateProduction(cfg)
	require.Error(t, err)
	errStr := err.Error()
	assert.Contains(t, errStr, "JWT_SECRET")
	assert.Contains(t, errStr, "DATABASE_HOST")
	assert.Contains(t, errStr, "REDIS_HOST")
}

func TestValidateProduction_RealKeysPass(t *testing.T) {
	cfg := &Config{
		Server:   ServerConfig{Env: "production"},
		Auth:     AuthConfig{JWTSecret: "super-secret-long-key-for-prod-1234"},
		Database: DatabaseConfig{Host: "db.prod.com"},
		Redis:    RedisConfig{Host: "redis.prod.com"},
		LLM: LLMConfig{
			OpenAIKey:    "sk-real-openai-key-1234567890",
			AnthropicKey: "sk-ant-real-key-1234567890",
		},
	}
	err := ValidateProduction(cfg)
	assert.NoError(t, err)
}

func TestValidateProduction_VariousNonProdEnvs(t *testing.T) {
	for _, env := range []string{"development", "staging", "test", ""} {
		cfg := &Config{
			Server:   ServerConfig{Env: env},
			Auth:     AuthConfig{JWTSecret: ""},
			Database: DatabaseConfig{Host: ""},
			Redis:    RedisConfig{Host: ""},
		}
		err := ValidateProduction(cfg)
		assert.NoError(t, err, "env=%q should pass", env)
	}
}

// --- ValidateProductionEnv tests ---

func TestValidateProdEnv_NonProdSkips(t *testing.T) {
	os.Unsetenv("VIGILAGENT_ENV")
	err := ValidateProductionEnv()
	assert.NoError(t, err)
}

func TestValidateProdEnv_MissingJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Unsetenv("VIGILAGENT_JWT_SECRET")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_SECRET must be set")
}

func TestValidateProdEnv_ChangeMeJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "change-me-in-production")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default value")
}

func TestValidateProdEnv_SecretJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "secret")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default value")
}

func TestValidateProdEnv_DefaultJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "default")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "default value")
}

func TestValidateProdEnv_ShortJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "short")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 32 characters")
}

func TestValidateProdEnv_GoodJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "super-secret-long-key-for-prod-1234")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	assert.NoError(t, err)
}

func TestValidateProdEnv_StagingOK(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "staging")
	os.Unsetenv("VIGILAGENT_JWT_SECRET")
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
	}()
	err := ValidateProductionEnv()
	assert.NoError(t, err)
}

func TestValidateProdEnv_31CharJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "1234567890123456789012345678901") // 31 chars
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least 32 characters")
}

func TestValidateProdEnv_32CharJWT(t *testing.T) {
	os.Setenv("VIGILAGENT_ENV", "production")
	os.Setenv("VIGILAGENT_JWT_SECRET", "12345678901234567890123456789012") // 32 chars
	defer func() {
		os.Unsetenv("VIGILAGENT_ENV")
		os.Unsetenv("VIGILAGENT_JWT_SECRET")
	}()
	err := ValidateProductionEnv()
	assert.NoError(t, err)
}
