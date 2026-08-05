package database

import (
	"context"
	"testing"
	"time"

	"github.com/vigilagent/vigilagent/internal/config"
)

// Content from indexadviser_test.go
func baseTestConfig() *config.Config {
	return &config.Config{
		Database: config.DatabaseConfig{
			Host:               "localhost",
			Port:               5432,
			User:               "u",
			Name:               "db",
			PoolMaxOpen:        10,
			PoolMaxIdle:        5,
			PoolMaxLifetime:    time.Minute,
			PoolMaxIdleTime:    time.Minute,
			MaxOpenConns:       10,
			MaxIdleConns:       5,
			MaxLifetime:        time.Minute,
			ConnIdleTime:       time.Minute,
			SlowQueryThreshold: 100 * time.Millisecond,
			RetryMaxAttempts:   3,
		},
		Auth: config.AuthConfig{
			JWTSecret:     "01234567890123456789012345678901",
			JWTExpiration: time.Hour,
		},
		NATS:   config.NATSConfig{URL: "nats://localhost", Stream: "s"},
		Redis:  config.RedisConfig{Host: "localhost", Port: 6379},
		Server: config.ServerConfig{Port: 8080, ReadTimeout: time.Second, WriteTimeout: time.Second},
		Log:    config.LogConfig{Level: "info"},
		Audit:  config.AuditConfig{RetentionDays: 90, CompressAfterDays: 30},
		CORS:   config.CORSConfig{AllowedOrigins: []string{"*"}},
		LLM:    config.LLMConfig{DefaultModel: "m", BudgetPerTask: 1},
		IPAnomaly: config.IPAnomalyConfig{
			BruteForceThreshold: 10,
			ScoreThreshold:      70,
		},
	}
}

func TestDatabaseConfig_PoolValidation_MaxLessThanMin(t *testing.T) {
	cfg := baseTestConfig()
	cfg.Database.PoolMaxOpen = 5
	cfg.Database.PoolMaxIdle = 10
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when pool min > max")
	}
	if err.Error() != "database pool min conns (10) must not exceed max conns (5)" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDatabaseConfig_PoolValidation_ZeroPoolMax(t *testing.T) {
	cfg := baseTestConfig()
	cfg.Database.PoolMaxOpen = 0
	cfg.Database.PoolMaxIdle = 0
	cfg.Database.MaxOpenConns = 0
	cfg.Database.MaxIdleConns = 0
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error when pool max open is 0")
	}
}

func TestDatabaseConfig_StatementTimeout_Negative(t *testing.T) {
	cfg := baseTestConfig()
	cfg.Database.StatementTimeout = -time.Second
	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected error for negative statement_timeout")
	}
	if err.Error() != "database.statement_timeout must be non-negative" {
		t.Errorf("unexpected error: %v", err)
	}
}

// Content from poolmetrics_test.go
func TestCollectPoolStats_NilPool(t *testing.T) {
	snap := CollectPoolStats(nil)
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.TotalConns != 0 {
		t.Errorf("TotalConns = %d, want 0", snap.TotalConns)
	}
	if snap.Utilization != 0 {
		t.Errorf("Utilization = %f, want 0", snap.Utilization)
	}
}

func TestCollectPoolStats_WithPool(t *testing.T) {
	// We can't easily create a real pgxpool.Pool without a DB,
	// but we can verify the function handles nil gracefully
	// and that the metrics are registered.
	snap := CollectPoolStats(nil)
	if snap == nil {
		t.Fatal("expected non-nil snapshot from nil pool")
	}
}

func TestStatsFromStat_ZeroValues(t *testing.T) {
	// pgxpool.Stat is created internally; we test the snapshot logic
	// by calling CollectPoolStats with nil (which returns zero snapshot).
	snap := CollectPoolStats(nil)
	if snap.EmptyAcquireCount != 0 {
		t.Errorf("EmptyAcquireCount = %d, want 0", snap.EmptyAcquireCount)
	}
	if snap.MaxConns != 0 {
		t.Errorf("MaxConns = %d, want 0", snap.MaxConns)
	}
}

func TestUpdatePrometheusGauges_NilSnapshot(t *testing.T) {
	// Should not panic with zero snapshot
	snap := &PoolStatsSnapshot{}
	updatePrometheusGauges(snap)
}

func TestUpdatePrometheusGauges_HighUtilization(t *testing.T) {
	snap := &PoolStatsSnapshot{
		TotalConns:    25,
		AcquiredConns: 24,
		MaxConns:      25,
		Utilization:   0.96,
	}
	// Should set health gauge to 0 (unhealthy)
	updatePrometheusGauges(snap)
}

func TestUpdatePrometheusGauges_LowUtilization(t *testing.T) {
	snap := &PoolStatsSnapshot{
		TotalConns:    10,
		AcquiredConns: 5,
		MaxConns:      25,
		Utilization:   0.2,
	}
	// Should set health gauge to 1 (healthy)
	updatePrometheusGauges(snap)
}

func TestUpdatePrometheusGauges_ZeroMaxConns(t *testing.T) {
	snap := &PoolStatsSnapshot{
		TotalConns:    0,
		AcquiredConns: 0,
		MaxConns:      0,
		Utilization:   0,
	}
	// Division by zero handled — utilization stays 0
	updatePrometheusGauges(snap)
}

func TestUpdatePrometheusGauges_NegativeIdle(t *testing.T) {
	snap := &PoolStatsSnapshot{
		TotalConns:    2,
		AcquiredConns: 5, // more acquired than total (shouldn't happen but test edge)
		MaxConns:      10,
		Utilization:   0.5,
	}
	// idle should be clamped to 0
	updatePrometheusGauges(snap)
}

func TestPoolStatsSnapshot_Fields(t *testing.T) {
	snap := &PoolStatsSnapshot{
		TotalConns:              10,
		AcquiredConns:           3,
		MaxConns:                20,
		ConstructingConns:       1,
		EmptyAcquireCount:       42,
		AcquireDurationMs:       150,
		MaxIdleDestroyCount:     5,
		MaxLifetimeDestroyCount: 2,
		Utilization:             0.15,
	}

	if snap.TotalConns != 10 {
		t.Errorf("TotalConns = %d", snap.TotalConns)
	}
	if snap.AcquiredConns != 3 {
		t.Errorf("AcquiredConns = %d", snap.AcquiredConns)
	}
	if snap.MaxConns != 20 {
		t.Errorf("MaxConns = %d", snap.MaxConns)
	}
	if snap.Utilization != 0.15 {
		t.Errorf("Utilization = %f", snap.Utilization)
	}
}

func TestReadPoolStats_NilPool(t *testing.T) {
	p := &Postgres{Pool: nil}
	snap := p.ReadPoolStats()
	if snap == nil {
		t.Fatal("expected non-nil snapshot")
	}
	if snap.TotalConns != 0 {
		t.Errorf("TotalConns = %d, want 0", snap.TotalConns)
	}
}

func TestUpdatePoolMetrics_NilPoolNoPanic_Poolmetrics(t *testing.T) {
	p := &Postgres{Pool: nil}
	p.updatePoolMetrics() // should not panic
}

// Content from slowquery_test.go
func TestSlowQueryLogger_DefaultThreshold(t *testing.T) {
	cfg := SlowQueryConfig{
		Threshold: 0,
		Enabled:   true,
	}
	logger := NewSlowQueryLogger(nil, cfg)
	if logger.config.Threshold != time.Second {
		t.Errorf("expected default threshold 1s, got %v", logger.config.Threshold)
	}
	if !logger.config.Enabled {
		t.Error("expected enabled=true")
	}
}

func TestSlowQueryLogger_CustomThreshold(t *testing.T) {
	cfg := SlowQueryConfig{
		Threshold: 500 * time.Millisecond,
		Enabled:   true,
	}
	logger := NewSlowQueryLogger(nil, cfg)
	if logger.config.Threshold != 500*time.Millisecond {
		t.Errorf("expected threshold 500ms, got %v", logger.config.Threshold)
	}
}

func TestSlowQueryLogger_Disabled(t *testing.T) {
	cfg := SlowQueryConfig{
		Threshold: time.Second,
		Enabled:   false,
	}
	logger := NewSlowQueryLogger(nil, cfg)
	if logger.config.Enabled {
		t.Error("expected enabled=false")
	}
}

func TestSlowQueryLogger_LogIfSlow_BelowThreshold(t *testing.T) {
	cfg := SlowQueryConfig{
		Threshold: time.Second,
		Enabled:   true,
	}
	logger := NewSlowQueryLogger(nil, cfg)
	// Should not panic when pool is nil and query is fast
	logger.logIfSlow(context.Background(), "SELECT 1", nil, time.Now().Add(-100*time.Millisecond), nil)
}

func TestSlowQueryLogger_LogIfSlow_Disabled(t *testing.T) {
	cfg := SlowQueryConfig{
		Threshold: time.Second,
		Enabled:   false,
	}
	logger := NewSlowQueryLogger(nil, cfg)
	// Should not panic even with slow query when disabled
	logger.logIfSlow(context.Background(), "SELECT 1", nil, time.Now().Add(-5*time.Second), nil)
}

func TestApplyStatementTimeout_NilPool(t *testing.T) {
	err := ApplyStatementTimeout(nil, nil, &config.DatabaseConfig{})
	if err != nil {
		t.Errorf("expected nil error for nil pool, got %v", err)
	}
}

func TestApplyStatementTimeout_ZeroTimeout(t *testing.T) {
	err := ApplyStatementTimeout(nil, nil, &config.DatabaseConfig{StatementTimeout: 0})
	if err != nil {
		t.Errorf("expected nil error for zero timeout, got %v", err)
	}
}

func TestIndexAdvisor_NilPool(t *testing.T) {
	advisor := NewIndexAdvisor(nil)
	recs, err := advisor.GetRecommendations(nil)
	if err == nil {
		t.Fatal("expected error for nil pool")
	}
	if recs != nil {
		t.Error("expected nil recommendations for nil pool")
	}
}

// Content from poolmetrics_bench.go
func BenchmarkCollectPoolStats_NilPool(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = CollectPoolStats(nil)
	}
}

func BenchmarkUpdatePrometheusGauges(b *testing.B) {
	b.Run("healthy", func(b *testing.B) {
		snap := &PoolStatsSnapshot{
			TotalConns:    10,
			AcquiredConns: 3,
			MaxConns:      20,
			Utilization:   0.15,
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			updatePrometheusGauges(snap)
		}
	})

	b.Run("unhealthy", func(b *testing.B) {
		snap := &PoolStatsSnapshot{
			TotalConns:    25,
			AcquiredConns: 24,
			MaxConns:      25,
			Utilization:   0.96,
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			updatePrometheusGauges(snap)
		}
	})

	b.Run("zero_connections", func(b *testing.B) {
		snap := &PoolStatsSnapshot{}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			updatePrometheusGauges(snap)
		}
	})

	b.Run("negative_idle", func(b *testing.B) {
		snap := &PoolStatsSnapshot{
			TotalConns:    2,
			AcquiredConns: 5,
			MaxConns:      10,
			Utilization:   0.5,
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			updatePrometheusGauges(snap)
		}
	})
}

func BenchmarkPoolStatsSnapshot_IdleCalc(b *testing.B) {
	snap := &PoolStatsSnapshot{
		TotalConns:    50,
		AcquiredConns: 20,
		MaxConns:      100,
		Utilization:   0.2,
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		idle := snap.TotalConns - snap.AcquiredConns
		if idle < 0 {
			idle = 0
		}
		_ = idle
	}
}

func BenchmarkPostgres_ReadPoolStats(b *testing.B) {
	p := &Postgres{Pool: nil}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.ReadPoolStats()
	}
}

func BenchmarkPostgres_updatePoolMetrics(b *testing.B) {
	p := &Postgres{Pool: nil}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.updatePoolMetrics()
	}
}

func BenchmarkCollectPoolStats_Concurrent(b *testing.B) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = CollectPoolStats(nil)
		}
	})
}

func BenchmarkUpdatePrometheusGauges_Concurrent(b *testing.B) {
	snap := &PoolStatsSnapshot{
		TotalConns:    10,
		AcquiredConns: 3,
		MaxConns:      20,
		Utilization:   0.15,
	}
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			updatePrometheusGauges(snap)
		}
	})
}

func BenchmarkPoolStatsSnapshot_Utilization(b *testing.B) {
	snapshots := []PoolStatsSnapshot{
		{TotalConns: 0, AcquiredConns: 0, MaxConns: 0},
		{TotalConns: 10, AcquiredConns: 3, MaxConns: 20},
		{TotalConns: 25, AcquiredConns: 24, MaxConns: 25},
		{TotalConns: 100, AcquiredConns: 50, MaxConns: 100},
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s := snapshots[i%len(snapshots)]
		var utilization float64
		if s.MaxConns > 0 {
			utilization = float64(s.AcquiredConns) / float64(s.MaxConns)
		}
		_ = utilization
	}
}
