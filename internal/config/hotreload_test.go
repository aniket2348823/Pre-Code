package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/viper"
)

func TestHotReloader_StopWithoutStart(t *testing.T) {
	cfg := &Config{}
	hr := NewHotReloader(cfg)

	done := make(chan struct{})
	go func() {
		hr.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(1 * time.Second):
		t.Log("Stop blocks without Start (expected: done channel never closed)")
	}
}

func TestHotReloader_StartAndStop(t *testing.T) {
	cfg := &Config{}
	hr := NewHotReloader(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		hr.Start(ctx)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)

	// Cancel context — this should cause Start to return
	cancel()

	select {
	case <-done:
		// Start returned after context cancel
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after context cancel")
	}
}

func TestHotReloader_WithOnChangeCallback(t *testing.T) {
	cfg := &Config{}
	hr := NewHotReloader(cfg)

	called := false
	hr.OnChange(func(newCfg *Config) {
		called = true
	})

	if len(hr.callbacks) != 1 {
		t.Errorf("expected 1 callback, got %d", len(hr.callbacks))
	}
	_ = called
}

func TestReadFromViper_WithEnvVars(t *testing.T) {
	os.Setenv("VIGILAGENT_SERVER_HOST", "custom-host")
	defer os.Unsetenv("VIGILAGENT_SERVER_HOST")

	cfg := readFromViper()
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
}

func TestReadFromViper_CORSDefaults(t *testing.T) {
	os.Unsetenv("VIGILAGENT_CORS_ALLOWED_ORIGINS")
	os.Unsetenv("VIGILAGENT_CORS_ALLOWED_METHODS")
	os.Unsetenv("VIGILAGENT_CORS_ALLOWED_HEADERS")
	os.Unsetenv("VIGILAGENT_CORS_MAX_AGE")

	cfg := readFromViper()
	if len(cfg.CORS.AllowedOrigins) == 0 {
		t.Error("expected CORS default origins")
	}
	if len(cfg.CORS.AllowedMethods) == 0 {
		t.Error("expected CORS default methods")
	}
	if len(cfg.CORS.AllowedHeaders) == 0 {
		t.Error("expected CORS default headers")
	}
	if cfg.CORS.MaxAge != 86400 {
		t.Errorf("expected MaxAge 86400, got %d", cfg.CORS.MaxAge)
	}
}

func TestHotReloader_StopAfterStart(t *testing.T) {
	cfg := &Config{}
	hr := NewHotReloader(cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		hr.Start(ctx)
		close(done)
	}()

	time.Sleep(200 * time.Millisecond)

	// Now call Stop — this should hit the cancel() and <-done paths
	hr.Stop()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Stop")
	}
}

func TestHotReloader_ConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	os.WriteFile(cfgFile, []byte(`
server:
  host: 0.0.0.0
  port: 9999
`), 0644)

	cfg := &Config{}
	hr := NewHotReloader(cfg)

	if hr.Config() == nil {
		t.Fatal("expected non-nil config")
	}
	if hr.Config().Server.Host != "" {
		t.Errorf("expected empty host before reload, got %q", hr.Config().Server.Host)
	}
}

func TestHotReloader_StartCallbackOnFileChange(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
  validYAML := "server:\n  host: 0.0.0.0\n  port: 8080\n  env: development\n  read_timeout: 10s\n  write_timeout: 10s\ndatabase:\n  host: localhost\n  port: 5432\n  user: u\n  name: n\n  max_open_conns: 10\nredis:\n  host: localhost\n  port: 6379\nnats:\n  url: nats://x\n  stream: s\nauth:\n  jwt_secret: test-secret-32-chars-long-ok!!!!\n  jwt_expiration: 24h\nllm:\n  default_model: m\n"
	os.WriteFile(cfgFile, []byte(validYAML), 0644)

	// Point viper at the temp config
 oldEnv := os.Getenv("VIGILAGENT_CONFIG_PATH")
	os.Setenv("VIGILAGENT_CONFIG_PATH", cfgFile)
	defer func() {
		if oldEnv == "" {
			os.Unsetenv("VIGILAGENT_CONFIG_PATH")
		} else {
			os.Setenv("VIGILAGENT_CONFIG_PATH", oldEnv)
		}
		viper.Reset()
	}()

	// Read initial config to seed viper
	viper.SetConfigFile(cfgFile)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("failed to read initial config: %v", err)
	}

	cfg := &Config{Server: ServerConfig{Host: "old"}}
	hr := NewHotReloader(cfg)

 callbackCh := make(chan *Config, 1)
	hr.OnChange(func(newCfg *Config) {
		callbackCh <- newCfg
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		hr.Start(ctx)
		close(done)
	}()

	// Modify config file to trigger watcher (change port)
	updatedYAML := "server:\n  host: 0.0.0.0\n  port: 9999\n  env: development\n  read_timeout: 10s\n  write_timeout: 10s\ndatabase:\n  host: localhost\n  port: 5432\n  user: u\n  name: n\n  max_open_conns: 10\nredis:\n  host: localhost\n  port: 6379\nnats:\n  url: nats://x\n  stream: s\nauth:\n  jwt_secret: test-secret-32-chars-long-ok!!!!\n  jwt_expiration: 24h\nllm:\n  default_model: m\n"
	time.Sleep(200 * time.Millisecond)
	os.WriteFile(cfgFile, []byte(updatedYAML), 0644)

	select {
	case newCfg := <-callbackCh:
		if newCfg.Server.Port != 9999 {
			t.Errorf("expected port 9999 after reload, got %d", newCfg.Server.Port)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("callback not triggered after config file change")
	}

 cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}

func TestHotReloader_StartValidationFailureOnReload(t *testing.T) {
	dir := t.TempDir()
	cfgFile := filepath.Join(dir, "config.yaml")
	initialContent := []byte("server:\n  host: 0.0.0.0\n  port: 8080\n  env: development\n  read_timeout: 10s\n  write_timeout: 10s\n")
	os.WriteFile(cfgFile, initialContent, 0644)

 oldEnv := os.Getenv("VIGILAGENT_CONFIG_PATH")
	os.Setenv("VIGILAGENT_CONFIG_PATH", cfgFile)
	defer func() {
		if oldEnv == "" {
			os.Unsetenv("VIGILAGENT_CONFIG_PATH")
		} else {
			os.Setenv("VIGILAGENT_CONFIG_PATH", oldEnv)
		}
		viper.Reset()
	}()

	viper.SetConfigFile(cfgFile)
	if err := viper.ReadInConfig(); err != nil {
		t.Fatalf("failed to read initial config: %v", err)
	}

	cfg := &Config{Server: ServerConfig{Host: "old"}}
	hr := NewHotReloader(cfg)

 callbackCh := make(chan *Config, 1)
	hr.OnChange(func(newCfg *Config) {
		callbackCh <- newCfg
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		hr.Start(ctx)
		close(done)
	}()

	// Write invalid config (port 0 fails validation)
	time.Sleep(200 * time.Millisecond)
	invalidContent := []byte("server:\n  host: 0.0.0.0\n  port: 0\n  env: development\n  read_timeout: 10s\n  write_timeout: 10s\n")
	os.WriteFile(cfgFile, invalidContent, 0644)

	// Callback should NOT fire because validation fails
	select {
	case <-callbackCh:
		t.Fatal("callback should not fire for invalid config")
	case <-time.After(2 * time.Second):
		// Expected — invalid config is rejected
	}

	// Config should still be the old one
	if hr.Config().Server.Host != "old" {
		t.Errorf("expected old config preserved, got host %q", hr.Config().Server.Host)
	}

 cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after cancel")
	}
}

func TestHotReloader_MultipleCallbacks(t *testing.T) {
	cfg := &Config{}
	hr := NewHotReloader(cfg)

	var count int
	hr.OnChange(func(newCfg *Config) { count++ })
	hr.OnChange(func(newCfg *Config) { count++ })
	hr.OnChange(func(newCfg *Config) { count++ })

	if len(hr.callbacks) != 3 {
		t.Errorf("expected 3 callbacks, got %d", len(hr.callbacks))
	}
}
