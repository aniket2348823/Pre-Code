package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig
	Database DatabaseConfig
	Redis    RedisConfig
	NATS     NATSConfig
	Auth     AuthConfig
	Stripe   StripeConfig
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

type StripeConfig struct {
	SecretKey     string
	WebhookSecret string
	SuccessURL    string
	CancelURL     string
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
		},
		Auth: AuthConfig{
			JWTSecret:     viper.GetString("auth.jwt_secret"),
			JWTExpiration: viper.GetDuration("auth.jwt_expiration"),
			APIKeyPrefix:  viper.GetString("auth.api_key_prefix"),
		},
		Stripe: StripeConfig{
			SecretKey:     viper.GetString("stripe.secret_key"),
			WebhookSecret: viper.GetString("stripe.webhook_secret"),
			SuccessURL:    viper.GetString("stripe.success_url"),
			CancelURL:     viper.GetString("stripe.cancel_url"),
		},
		Log: LogConfig{
			Level:  viper.GetString("log.level"),
			Format: viper.GetString("log.format"),
		},
	}

	return cfg, nil
}

func (c *DatabaseConfig) DSN() string {
	return fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode)
}

func (c *RedisConfig) Address() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}
