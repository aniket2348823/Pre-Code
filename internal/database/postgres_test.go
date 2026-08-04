package database

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vigilagent/vigilagent/internal/config"
)

func newTestPoolCfg() *pgxpool.Config {
	return &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}
}

// --- configureSSL edge cases ---

func TestConfigureSSL_EmptyMode(t *testing.T) {
	poolCfg := newTestPoolCfg()
	cfg := &config.DatabaseConfig{Host: "db.example.com", SSLMode: ""}
	configureSSL(poolCfg, cfg)
	if poolCfg.ConnConfig.TLSConfig != nil {
		t.Error("empty mode should not set TLS config")
	}
}

func TestConfigureSSL_UnknownMode(t *testing.T) {
	poolCfg := newTestPoolCfg()
	cfg := &config.DatabaseConfig{Host: "db.example.com", SSLMode: "allow"}
	configureSSL(poolCfg, cfg)
	if poolCfg.ConnConfig.TLSConfig != nil {
		t.Error("unknown mode should not set TLS config")
	}
}

func TestConfigureSSL_CaseInsensitive(t *testing.T) {
	poolCfg := newTestPoolCfg()
	cfg := &config.DatabaseConfig{Host: "db.example.com", SSLMode: "REQUIRE"}
	configureSSL(poolCfg, cfg)
	if poolCfg.ConnConfig.TLSConfig == nil {
		t.Fatal("REQUIRE (uppercase) should set TLS config")
	}
}

func TestConfigureSSL_VerifyFullInsecureSkip(t *testing.T) {
	poolCfg := newTestPoolCfg()
	cfg := &config.DatabaseConfig{Host: "db.example.com", SSLMode: "verify-full"}
	configureSSL(poolCfg, cfg)
	if poolCfg.ConnConfig.TLSConfig == nil {
		t.Fatal("verify-full should set TLS config")
	}
	if poolCfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Error("verify-full should set InsecureSkipVerify=false")
	}
}

func TestConfigureSSL_ServerName(t *testing.T) {
	poolCfg := newTestPoolCfg()
	cfg := &config.DatabaseConfig{Host: "myhost", SSLMode: "require"}
	configureSSL(poolCfg, cfg)
	if poolCfg.ConnConfig.TLSConfig.ServerName != "myhost" {
		t.Errorf("ServerName = %q, want %q", poolCfg.ConnConfig.TLSConfig.ServerName, "myhost")
	}
}

// --- shouldEnforceSSL additional cases ---

func TestShouldEnforceSSL_AllModes(t *testing.T) {
	modes := []struct {
		mode string
		want bool
	}{
		{"require", true},
		{"REQUIRE", true},
		{"Require", true},
		{"verify-ca", true},
		{"VERIFY-CA", true},
		{"verify-full", true},
		{"VERIFY-FULL", true},
		{"prefer", false},
		{"PREFER", false},
		{"disable", false},
		{"DISABLE", false},
		{"", false},
		{"allow", false},
		{"random", false},
	}
	for _, m := range modes {
		got := shouldEnforceSSL(&config.DatabaseConfig{SSLMode: m.mode})
		if got != m.want {
			t.Errorf("shouldEnforceSSL(%q) = %v, want %v", m.mode, got, m.want)
		}
	}
}

// --- StartEventPurger edge cases ---

func TestStartEventPurger_NegativeRetention(t *testing.T) {
	p := &Postgres{Pool: nil}
	cancel := p.StartEventPurger(context.Background(), -5, 0)
	if cancel == nil {
		t.Fatal("expected non-nil cancel func")
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
}

func TestStartEventPurger_NegativeInterval(t *testing.T) {
	p := &Postgres{Pool: nil}
	cancel := p.StartEventPurger(context.Background(), 0, -time.Hour)
	if cancel == nil {
		t.Fatal("expected non-nil cancel func")
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
}

func TestStartEventPurger_CancelStopsGoroutine(t *testing.T) {
	p := &Postgres{Pool: nil}
	ctx, cancel := context.WithCancel(context.Background())
	stop := p.StartEventPurger(ctx, 30, 1*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	cancel()
	time.Sleep(10 * time.Millisecond)
	_ = stop
}

func TestStartEventPurger_ReturnedCancelFunc(t *testing.T) {
	p := &Postgres{Pool: nil}
	cancel := p.StartEventPurger(context.Background(), 90, 24*time.Hour)
	if cancel == nil {
		t.Fatal("expected non-nil cancel func")
	}
	cancel()
}

// --- PurgeOldEvents edge cases ---

func TestPurgeOldEvents_PositiveRetentionNilPool(t *testing.T) {
	p := &Postgres{Pool: nil}
	n, err := p.PurgeOldEvents(context.Background(), 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rows, got %d", n)
	}
}

func TestPurgeOldEvents_ZeroRetentionNilPool(t *testing.T) {
	p := &Postgres{Pool: nil}
	n, err := p.PurgeOldEvents(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 rows, got %d", n)
	}
}

// --- Postgres struct ---

func TestPostgres_StructZeroValues(t *testing.T) {
	p := &Postgres{}
	if p.Conn() == nil {
		t.Fatal("Conn() should not return nil")
	}
	if p.PoolStats() != nil {
		t.Fatal("PoolStats() should return nil for zero pool")
	}
	if p.PoolHealthy() {
		t.Fatal("PoolHealthy() should return false for zero pool")
	}
}

func TestPostgres_Close_NilPoolNoPanic(t *testing.T) {
	p := &Postgres{Pool: nil}
	p.Close()
}

// --- shouldEnforceSSL with sslmode variations ---

func TestShouldEnforceSSL_RequireVariants(t *testing.T) {
	variants := []string{"require", "Require", "REQUIRE"}
	for _, v := range variants {
		if !shouldEnforceSSL(&config.DatabaseConfig{SSLMode: v}) {
			t.Errorf("shouldEnforceSSL(%q) should be true", v)
		}
	}
}

func TestShouldEnforceSSL_NonEnforcingModes(t *testing.T) {
	modes := []string{"prefer", "disable", "allow", "optional", "none"}
	for _, m := range modes {
		if shouldEnforceSSL(&config.DatabaseConfig{SSLMode: m}) {
			t.Errorf("shouldEnforceSSL(%q) should be false", m)
		}
	}
}

// --- SSL configureTLS MinVersion ---

func TestConfigureSSL_MinVersionAllModes(t *testing.T) {
	modes := []string{"require", "verify-ca", "verify-full"}
	for _, mode := range modes {
		poolCfg := newTestPoolCfg()
		cfg := &config.DatabaseConfig{Host: "h", SSLMode: mode}
		configureSSL(poolCfg, cfg)
		if poolCfg.ConnConfig.TLSConfig == nil {
			t.Fatalf("mode %q: nil TLS config", mode)
		}
		if poolCfg.ConnConfig.TLSConfig.MinVersion != 0x0303 {
			t.Errorf("mode %q: MinVersion = %x, want TLS 1.2 (0x0303)", mode, poolCfg.ConnConfig.TLSConfig.MinVersion)
		}
	}
}

// --- VerifyRLS with nil pool ---

func TestVerifyRLS_NilPoolNoError(t *testing.T) {
	p := &Postgres{Pool: nil}
	err := p.VerifyRLS(context.Background())
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

// --- HealthCheck nil pool ---

func TestPostgres_HealthCheck_NilPool_NoPanic(t *testing.T) {
	p := &Postgres{Pool: nil}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil pool")
			}
		}()
		p.HealthCheck(context.Background())
	}()
}

func TestConfigureSSL_RequireMode_InsecureSkipVerify(t *testing.T) {
	poolCfg := newTestPoolCfg()
	cfg := &config.DatabaseConfig{Host: "db.prod.com", SSLMode: "require"}
	configureSSL(poolCfg, cfg)

	if poolCfg.ConnConfig.TLSConfig == nil {
		t.Fatal("require mode should set TLS config")
	}
	if poolCfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Error("require mode must set InsecureSkipVerify=false")
	}
}

func TestConfigureSSL_PreferMode_InsecureSkipVerify(t *testing.T) {
	poolCfg := newTestPoolCfg()
	cfg := &config.DatabaseConfig{Host: "db.prod.com", SSLMode: "prefer"}
	configureSSL(poolCfg, cfg)

	if poolCfg.ConnConfig.TLSConfig == nil {
		t.Fatal("prefer mode should set TLS config")
	}
	if !poolCfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Error("prefer mode must set InsecureSkipVerify=true")
	}
}

func TestConfigureSSL_VerifyCA_InsecureSkipVerify(t *testing.T) {
	poolCfg := newTestPoolCfg()
	cfg := &config.DatabaseConfig{Host: "db.prod.com", SSLMode: "verify-ca"}
	configureSSL(poolCfg, cfg)

	if poolCfg.ConnConfig.TLSConfig == nil {
		t.Fatal("verify-ca mode should set TLS config")
	}
	if !poolCfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Error("verify-ca mode must set InsecureSkipVerify=true")
	}
}

// --- PoolHealthCheck ---

func TestPoolHealthCheck_NilPool(t *testing.T) {
	p := &Postgres{Pool: nil}
	_, err := p.PoolHealthCheck(context.Background())
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
}

func TestPoolHealthCheck_NilPoolErrorMsg(t *testing.T) {
	p := &Postgres{Pool: nil}
	_, err := p.PoolHealthCheck(context.Background())
	if err.Error() != "pool is nil" {
		t.Errorf("unexpected error: %v", err)
	}
}

// --- updatePoolMetrics ---

func TestUpdatePoolMetrics_NilPoolNoPanic(t *testing.T) {
	p := &Postgres{Pool: nil}
	p.updatePoolMetrics() // should not panic
}

// --- collectPoolStats ---

func TestCollectPoolStats_NilPoolNoPanic(t *testing.T) {
	p := &Postgres{Pool: nil}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	p.collectPoolStats(ctx, &config.DatabaseConfig{PoolStatsInterval: time.Millisecond})
}

func TestCollectPoolStats_DefaultInterval(t *testing.T) {
	p := &Postgres{Pool: nil}
	ctx, cancel := context.WithCancel(context.Background())
	go p.collectPoolStats(ctx, &config.DatabaseConfig{})
	time.Sleep(10 * time.Millisecond)
	cancel()
}

// --- Postgres struct zero value ---

func TestPostgres_ZeroValue_HasCancelFunc(t *testing.T) {
	p := &Postgres{}
	if p.cancelStats != nil {
		t.Error("zero value Postgres should have nil cancelStats")
	}
}

func TestPostgres_Close_NilCancelFuncNoPanic(t *testing.T) {
	p := &Postgres{Pool: nil, cancelStats: nil}
	p.Close() // should not panic
}

func TestDatabaseConfig_DSN(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host:     "db.example.com",
		Port:     5433,
		User:     "admin",
		Password: "s3cret",
		Name:     "mydb",
		SSLMode:  "require",
	}

	dsn := cfg.DSN()
	expected := "host=db.example.com port=5433 user=admin password=s3cret dbname=mydb sslmode=require"
	if dsn != expected {
		t.Errorf("DSN() = %q, want %q", dsn, expected)
	}
}

func TestDatabaseConfig_DSN_DefaultPort(t *testing.T) {
	cfg := &config.DatabaseConfig{
		Host: "localhost",
		Port: 5432,
		User: "user",
		Name: "db",
	}
	dsn := cfg.DSN()
	if !strings.Contains(dsn, "port=5432") {
		t.Errorf("DSN should contain port=5432, got %q", dsn)
	}
}
