package database

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vigilagent/vigilagent/internal/config"
)

// Content from postgres_test.go
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

// Content from conn_test.go
// --- savepointTx error paths ---

func TestSavepointTx_CommitExecError(t *testing.T) {
	tx := &mockTx{
		execFn: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("RELEASE SAVEPOINT failed")
		},
	}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	err := sptx.Commit(context.Background())
	if err == nil {
		t.Fatal("expected error from commit")
	}
	if err.Error() != "RELEASE SAVEPOINT failed" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSavepointTx_RollbackExecError(t *testing.T) {
	tx := &mockTx{
		execFn: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("ROLLBACK TO SAVEPOINT failed")
		},
	}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	err := sptx.Rollback(context.Background())
	if err == nil {
		t.Fatal("expected error from rollback")
	}
	if err.Error() != "ROLLBACK TO SAVEPOINT failed" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSavepointTx_BeginExecError(t *testing.T) {
	tx := &mockTx{
		execFn: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			return pgconn.CommandTag{}, fmt.Errorf("SAVEPOINT exec failed")
		},
	}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	_, err := sptx.Begin(context.Background())
	if err == nil {
		t.Fatal("expected error from nested begin")
	}
}

// --- savepointTx delegation ---

func TestSavepointTx_DelegatesQueryRow(t *testing.T) {
	called := false
	tx := &mockTx{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			called = true
			return &mockRow{}
		},
	}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	row := sptx.QueryRow(context.Background(), "SELECT 1")
	if row == nil {
		t.Fatal("expected non-nil row")
	}
	if !called {
		t.Fatal("expected embedded Tx.QueryRow to be called")
	}
}

func TestSavepointTx_DelegatesQuery(t *testing.T) {
	called := false
	tx := &mockTx{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			called = true
			return &mockRows{}, nil
		},
	}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	rows, err := sptx.Query(context.Background(), "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows == nil {
		t.Fatal("expected non-nil rows")
	}
	if !called {
		t.Fatal("expected embedded Tx.Query to be called")
	}
}

func TestSavepointTx_DelegatesExec(t *testing.T) {
	called := false
	tx := &mockTx{
		execFn: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			called = true
			return pgconn.CommandTag{}, nil
		},
	}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	_, err := sptx.Exec(context.Background(), "INSERT INTO test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected embedded Tx.Exec to be called")
	}
}

// --- Begin with pre-existing counter ---

func TestConn_Begin_ExistingCounter(t *testing.T) {
	tx := &mockTx{}
	ctx := WithTx(context.Background(), tx)
	ctx = context.WithValue(ctx, ctxSavepointCounter{}, 5)
	c := NewConn(nil)
	stx, err := c.Begin(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stx == nil {
		t.Fatal("expected non-nil savepoint tx")
	}
	tx.mu.Lock()
	found := false
	for _, call := range tx.execCalls {
		if call == "SAVEPOINT sp_6" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SAVEPOINT sp_6 (counter 5+1), got calls: %v", tx.execCalls)
	}
	tx.mu.Unlock()
}

// --- Begin savepoint exec error ---

func TestConn_Begin_SavepointExecError(t *testing.T) {
	tx := &mockTx{
		execFn: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			if len(sql) > 9 && sql[:9] == "SAVEPOINT" {
				return pgconn.CommandTag{}, fmt.Errorf("savepoint creation failed")
			}
			return pgconn.CommandTag{}, nil
		},
	}
	ctx := WithTx(context.Background(), tx)
	c := NewConn(nil)
	_, err := c.Begin(ctx)
	if err == nil {
		t.Fatal("expected error from savepoint exec failure")
	}
}

// --- ConnFromContext path (nil conn panics) ---

func TestConn_QueryRow_ConnPathPanics(t *testing.T) {
	c := NewConn(nil)
	ctx := WithConn(context.Background(), nil)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil conn from context")
			}
		}()
		c.QueryRow(ctx, "SELECT 1")
	}()
}

func TestConn_Query_ConnPathPanics(t *testing.T) {
	c := NewConn(nil)
	ctx := WithConn(context.Background(), nil)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil conn from context")
			}
		}()
		c.Query(ctx, "SELECT 1")
	}()
}

func TestConn_Exec_ConnPathPanics(t *testing.T) {
	c := NewConn(nil)
	ctx := WithConn(context.Background(), nil)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil conn from context")
			}
		}()
		c.Exec(ctx, "INSERT INTO test")
	}()
}

// --- Tx priority over Conn ---

func TestConn_QueryRow_TxOverConn(t *testing.T) {
	txCalled := false
	tx := &mockTx{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			txCalled = true
			return &mockRow{}
		},
	}
	ctx := WithTx(context.Background(), tx)
	ctx = WithConn(ctx, nil)
	c := NewConn(nil)
	row := c.QueryRow(ctx, "SELECT 1")
	if row == nil {
		t.Fatal("expected non-nil row")
	}
	if !txCalled {
		t.Fatal("expected Tx.QueryRow to be called when both Tx and Conn are in context")
	}
}

func TestConn_Query_TxOverConn(t *testing.T) {
	txCalled := false
	tx := &mockTx{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			txCalled = true
			return &mockRows{}, nil
		},
	}
	ctx := WithTx(context.Background(), tx)
	ctx = WithConn(ctx, nil)
	c := NewConn(nil)
	rows, err := c.Query(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows == nil {
		t.Fatal("expected non-nil rows")
	}
	if !txCalled {
		t.Fatal("expected Tx.Query to be called when both Tx and Conn are in context")
	}
}

func TestConn_Exec_TxOverConn(t *testing.T) {
	txCalled := false
	tx := &mockTx{
		execFn: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			txCalled = true
			return pgconn.CommandTag{}, nil
		},
	}
	ctx := WithTx(context.Background(), tx)
	ctx = WithConn(ctx, nil)
	c := NewConn(nil)
	_, err := c.Exec(ctx, "INSERT INTO test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !txCalled {
		t.Fatal("expected Tx.Exec to be called when both Tx and Conn are in context")
	}
}

// --- Nested savepoint naming ---

func TestSavepointTx_Begin_NestedNaming(t *testing.T) {
	tx := &mockTx{}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	nested, err := sptx.Begin(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nested == nil {
		t.Fatal("expected non-nil nested savepoint tx")
	}
	// The nested savepoint should be sp_1_2
	tx.mu.Lock()
	found := false
	for _, call := range tx.execCalls {
		if call == "SAVEPOINT sp_1_2" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SAVEPOINT sp_1_2, got calls: %v", tx.execCalls)
	}
	tx.mu.Unlock()
}

// --- NewConnWithThreshold ---

func TestNewConnWithThreshold(t *testing.T) {
	c := NewConnWithThreshold(nil, 200*time.Millisecond)
	if c == nil {
		t.Fatal("NewConnWithThreshold returned nil")
	}
	if c.slowQueryThreshold != 200*time.Millisecond {
		t.Errorf("threshold = %v, want 200ms", c.slowQueryThreshold)
	}
}

func TestNewConnWithThreshold_ZeroFallsBack(t *testing.T) {
	c := NewConnWithThreshold(nil, 0)
	if c.slowQueryThreshold != 100*time.Millisecond {
		t.Errorf("zero threshold should fallback to 100ms, got %v", c.slowQueryThreshold)
	}
}

func TestNewConnWithThreshold_NegativeFallsBack(t *testing.T) {
	c := NewConnWithThreshold(nil, -time.Second)
	if c.slowQueryThreshold != 100*time.Millisecond {
		t.Errorf("negative threshold should fallback to 100ms, got %v", c.slowQueryThreshold)
	}
}

func TestNewConn_DefaultThreshold(t *testing.T) {
	c := NewConn(nil)
	if c.slowQueryThreshold != 100*time.Millisecond {
		t.Errorf("default threshold = %v, want 100ms", c.slowQueryThreshold)
	}
}

// Content from retry_test.go
// --- retryableError ---

func TestRetryableError_Nil(t *testing.T) {
	if retryableError(nil) {
		t.Error("nil should not be retryable")
	}
}

func TestRetryableError_NetError(t *testing.T) {
	err := &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
	if !retryableError(err) {
		t.Error("net.OpError should be retryable")
	}
}

func TestRetryableError_DNSError(t *testing.T) {
	err := &net.DNSError{Err: "no such host", Name: "db.example.com"}
	if !retryableError(err) {
		t.Error("net.DNSError should be retryable")
	}
}

func TestRetryableError_NetClosed(t *testing.T) {
	err := net.ErrClosed
	if !retryableError(err) {
		t.Error("net.ErrClosed should be retryable")
	}
}

func TestRetryableError_PgError_Class08(t *testing.T) {
	codes := []string{"08000", "08001", "08003", "08006", "08007"}
	for _, code := range codes {
		err := &pgconn.PgError{Code: code}
		if !retryableError(err) {
			t.Errorf("PgError code %s should be retryable", code)
		}
	}
}

func TestRetryableError_PgError_Class40(t *testing.T) {
	codes := []string{"40001", "40P01"}
	for _, code := range codes {
		err := &pgconn.PgError{Code: code}
		if !retryableError(err) {
			t.Errorf("PgError code %s should be retryable", code)
		}
	}
}

func TestRetryableError_PgError_Class57(t *testing.T) {
	codes := []string{"57014", "57P01", "57P02", "57P03"}
	for _, code := range codes {
		err := &pgconn.PgError{Code: code}
		if !retryableError(err) {
			t.Errorf("PgError code %s should be retryable", code)
		}
	}
}

func TestRetryableError_PgError_Class53(t *testing.T) {
	codes := []string{"53100", "53200", "53300"}
	for _, code := range codes {
		err := &pgconn.PgError{Code: code}
		if !retryableError(err) {
			t.Errorf("PgError code %s should be retryable", code)
		}
	}
}

func TestRetryableError_PgError_Class54(t *testing.T) {
	codes := []string{"54000", "54001"}
	for _, code := range codes {
		err := &pgconn.PgError{Code: code}
		if !retryableError(err) {
			t.Errorf("PgError code %s should be retryable", code)
		}
	}
}

func TestRetryableError_PgError_ConstraintViolation(t *testing.T) {
	err := &pgconn.PgError{Code: "23505"} // unique_violation
	if retryableError(err) {
		t.Error("unique_violation should NOT be retryable")
	}
}

func TestRetryableError_PgError_SyntaxError(t *testing.T) {
	err := &pgconn.PgError{Code: "42601"} // syntax_error
	if retryableError(err) {
		t.Error("syntax_error should NOT be retryable")
	}
}

func TestRetryableError_PgError_DataError(t *testing.T) {
	err := &pgconn.PgError{Code: "22P02"} // invalid_text_representation
	if retryableError(err) {
		t.Error("invalid_text_representation should NOT be retryable")
	}
}

func TestRetryableError_ContextDeadline(t *testing.T) {
	err := context.DeadlineExceeded
	if !retryableError(err) {
		t.Error("context.DeadlineExceeded should be retryable")
	}
}

func TestRetryableError_StringKeywords(t *testing.T) {
	keywords := []string{
		"connection refused",
		"connection reset",
		"connection timed out",
		"timeout",
		"serialization failure",
		"deadlock detected",
		"query canceled",
		"admin shutdown",
		"too many clients",
		"pool timeout",
		"i/o timeout",
		"broken pipe",
		"bad connection",
	}
	for _, kw := range keywords {
		err := errors.New(kw)
		if !retryableError(err) {
			t.Errorf("error %q should be retryable", kw)
		}
	}
}

func TestRetryableError_NonRetryable(t *testing.T) {
	errs := []error{
		errors.New("table does not exist"),
		errors.New("column missing"),
		errors.New("permission denied"),
		errors.New("random unrelated error"),
	}
	for _, err := range errs {
		if retryableError(err) {
			t.Errorf("error %q should NOT be retryable", err.Error())
		}
	}
}

// --- retryDelay ---

func TestRetryDelay_AttemptOne(t *testing.T) {
	cfg := DefaultRetryConfig()
	delay := retryDelay(1, cfg)
	if delay < cfg.BaseDelay/2 || delay > cfg.BaseDelay+time.Duration(float64(cfg.BaseDelay)*cfg.JitterRatio) {
		t.Errorf("attempt 1 delay %v out of expected range", delay)
	}
}

func TestRetryDelay_Exponential(t *testing.T) {
	cfg := RetryConfig{BaseDelay: 100 * time.Millisecond, MaxDelay: 5 * time.Second, JitterRatio: 0}
	d1 := retryDelay(1, cfg)
	d2 := retryDelay(2, cfg)
	d3 := retryDelay(3, cfg)
	if d1 != 100*time.Millisecond {
		t.Errorf("attempt 1: expected 100ms, got %v", d1)
	}
	if d2 != 200*time.Millisecond {
		t.Errorf("attempt 2: expected 200ms, got %v", d2)
	}
	if d3 != 400*time.Millisecond {
		t.Errorf("attempt 3: expected 400ms, got %v", d3)
	}
}

func TestRetryDelay_MaxCap(t *testing.T) {
	cfg := RetryConfig{BaseDelay: 100 * time.Millisecond, MaxDelay: 500 * time.Millisecond, JitterRatio: 0}
	delay := retryDelay(10, cfg)
	if delay > 500*time.Millisecond {
		t.Errorf("delay should be capped at MaxDelay, got %v", delay)
	}
}

func TestRetryDelay_ZeroJitter(t *testing.T) {
	cfg := RetryConfig{BaseDelay: 100 * time.Millisecond, MaxDelay: 5 * time.Second, JitterRatio: 0}
	d1 := retryDelay(1, cfg)
	d2 := retryDelay(1, cfg)
	if d1 != d2 {
		t.Errorf("zero jitter should produce deterministic delay, got %v and %v", d1, d2)
	}
}

func TestRetryDelay_AttemptZero(t *testing.T) {
	delay := retryDelay(0, DefaultRetryConfig())
	if delay != 0 {
		t.Errorf("attempt 0 should return 0 delay, got %v", delay)
	}
}

// --- DefaultRetryConfig ---

func TestDefaultRetryConfig(t *testing.T) {
	cfg := DefaultRetryConfig()
	if cfg.MaxAttempts != 3 {
		t.Errorf("MaxAttempts = %d, want 3", cfg.MaxAttempts)
	}
	if cfg.BaseDelay != 100*time.Millisecond {
		t.Errorf("BaseDelay = %v, want 100ms", cfg.BaseDelay)
	}
	if cfg.MaxDelay != 5*time.Second {
		t.Errorf("MaxDelay = %v, want 5s", cfg.MaxDelay)
	}
	if cfg.JitterRatio != 0.2 {
		t.Errorf("JitterRatio = %f, want 0.2", cfg.JitterRatio)
	}
}

// --- RetryQuery ---

func TestRetryQuery_SuccessFirstAttempt(t *testing.T) {
	cfg := DefaultRetryConfig()
	calls := 0
	result, err := RetryQuery(context.Background(), cfg, func(ctx context.Context) (pgx.Rows, error) {
		calls++
		return &mockRows{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil rows")
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryQuery_TransientErrorRetries(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, JitterRatio: 0}
	calls := 0
	_, err := RetryQuery(context.Background(), cfg, func(ctx context.Context) (pgx.Rows, error) {
		calls++
		if calls < 3 {
			return nil, &net.OpError{Op: "dial", Net: "tcp", Err: errors.New("connection refused")}
		}
		return &mockRows{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryQuery_NonRetryableErrorNoRetry(t *testing.T) {
	cfg := DefaultRetryConfig()
	calls := 0
	_, err := RetryQuery(context.Background(), cfg, func(ctx context.Context) (pgx.Rows, error) {
		calls++
		return nil, errors.New("table does not exist")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("non-retryable error should not retry, got %d calls", calls)
	}
}

func TestRetryQuery_ExhaustsAttempts(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, JitterRatio: 0}
	calls := 0
	_, err := RetryQuery(context.Background(), cfg, func(ctx context.Context) (pgx.Rows, error) {
		calls++
		return nil, errors.New("connection refused")
	})
	if err == nil {
		t.Fatal("expected error after exhausting retries")
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryQuery_ContextCancelledDuringRetry(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Second, MaxDelay: time.Second, JitterRatio: 0}
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	_, err := RetryQuery(ctx, cfg, func(ctx context.Context) (pgx.Rows, error) {
		calls++
		return nil, errors.New("connection refused")
	})
	if err == nil {
		t.Fatal("expected error after context cancel")
	}
	if calls != 1 {
		t.Errorf("expected 1 call before cancel, got %d", calls)
	}
}

// --- RetryExec ---

func TestRetryExec_SuccessFirstAttempt(t *testing.T) {
	cfg := DefaultRetryConfig()
	calls := 0
	tag, err := RetryExec(context.Background(), cfg, func(ctx context.Context) (pgconn.CommandTag, error) {
		calls++
		return pgconn.CommandTag{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
	_ = tag
}

func TestRetryExec_TransientErrorRetries(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: time.Millisecond, JitterRatio: 0}
	calls := 0
	_, err := RetryExec(context.Background(), cfg, func(ctx context.Context) (pgconn.CommandTag, error) {
		calls++
		if calls < 3 {
			return pgconn.CommandTag{}, errors.New("connection reset")
		}
		return pgconn.CommandTag{}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryExec_NonRetryableErrorNoRetry(t *testing.T) {
	cfg := DefaultRetryConfig()
	calls := 0
	_, err := RetryExec(context.Background(), cfg, func(ctx context.Context) (pgconn.CommandTag, error) {
		calls++
		return pgconn.CommandTag{}, &pgconn.PgError{Code: "23505"}
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("non-retryable error should not retry, got %d calls", calls)
	}
}

// Content from database_test.go
type mockRow struct{ scanErr error }

func (r *mockRow) Scan(dest ...any) error { return r.scanErr }

type mockRows struct{ closed bool }

func (m *mockRows) Close()                                       { m.closed = true }
func (m *mockRows) Err() error                                   { return nil }
func (m *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (m *mockRows) Next() bool                                   { return false }
func (m *mockRows) Scan(dest ...any) error                       { return nil }
func (m *mockRows) Values() ([]any, error)                       { return nil, nil }
func (m *mockRows) RawValues() [][]byte                          { return nil }
func (m *mockRows) Conn() *pgx.Conn                              { return nil }

type mockTx struct {
	mu         sync.Mutex
	queryRowFn func(ctx context.Context, sql string, args ...any) pgx.Row
	queryFn    func(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	execFn     func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	execCalls  []string
}

func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, fmt.Errorf("mock: Begin not implemented")
}
func (m *mockTx) Commit(ctx context.Context) error   { return nil }
func (m *mockTx) Rollback(ctx context.Context) error { return nil }
func (m *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (m *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (m *mockTx) LargeObjects() pgx.LargeObjects                               { return pgx.LargeObjects{} }
func (m *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (m *mockTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	m.mu.Lock()
	m.execCalls = append(m.execCalls, sql)
	m.mu.Unlock()
	if m.execFn != nil {
		return m.execFn(ctx, sql, arguments...)
	}
	return pgconn.CommandTag{}, nil
}
func (m *mockTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.queryFn != nil {
		return m.queryFn(ctx, sql, args...)
	}
	return &mockRows{}, nil
}
func (m *mockTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if m.queryRowFn != nil {
		return m.queryRowFn(ctx, sql, args...)
	}
	return &mockRow{}
}
func (m *mockTx) Conn() *pgx.Conn { return nil }

// session.go tests

func TestWithConnAndConnFromContext(t *testing.T) {
	ctx := context.Background()
	_, ok := ConnFromContext(ctx)
	if ok {
		t.Fatal("expected no conn in empty context")
	}
	ctx = WithConn(ctx, nil)
	conn, ok := ConnFromContext(ctx)
	if !ok {
		t.Fatal("expected conn in context")
	}
	if conn != nil {
		t.Fatal("expected nil conn")
	}
}

func TestConnFromContext_WrongType(t *testing.T) {
	type otherKey string
	wrongCtx := context.WithValue(context.Background(), otherKey("db_session_conn"), "not-a-conn")
	_, ok := ConnFromContext(wrongCtx)
	if ok {
		t.Fatal("expected false for wrong type")
	}
}

func TestWithTxAndTxFromContext(t *testing.T) {
	ctx := context.Background()
	_, ok := TxFromContext(ctx)
	if ok {
		t.Fatal("expected no tx in empty context")
	}
	ctx = WithTx(ctx, nil)
	_, ok = TxFromContext(ctx)
	if ok {
		t.Fatal("nil pgx.Tx should not be retrievable")
	}
}

func TestWithTxAndTxFromContext_NonNil(t *testing.T) {
	tx := &mockTx{}
	ctx := WithTx(context.Background(), tx)
	got, ok := TxFromContext(ctx)
	if !ok {
		t.Fatal("expected tx in context")
	}
	if got != tx {
		t.Fatal("expected same tx returned")
	}
}

func TestTxFromContext_WrongType(t *testing.T) {
	type otherKey string
	wrongCtx := context.WithValue(context.Background(), otherKey("db_session_tx"), "not-a-tx")
	_, ok := TxFromContext(wrongCtx)
	if ok {
		t.Fatal("expected false for wrong type")
	}
}

// postgres.go: shouldEnforceSSL

func TestShouldEnforceSSL(t *testing.T) {
	tests := []struct {
		mode string
		want bool
	}{
		{"require", true},
		{"verify-ca", true},
		{"verify-full", true},
		{"prefer", false},
		{"disable", false},
		{"", false},
		{"REQUIRE", true},
		{"Prefer", false},
		{"random", false},
	}
	for _, tt := range tests {
		t.Run(tt.mode, func(t *testing.T) {
			got := shouldEnforceSSL(&config.DatabaseConfig{SSLMode: tt.mode})
			if got != tt.want {
				t.Errorf("shouldEnforceSSL(%q) = %v, want %v", tt.mode, got, tt.want)
			}
		})
	}
}

// postgres.go: configureSSL

func TestConfigureSSL_Require(t *testing.T) {
	poolCfg := &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}
	cfg := &config.DatabaseConfig{Host: "db.example.com", SSLMode: "require"}
	configureSSL(poolCfg, cfg)
	if poolCfg.ConnConfig.TLSConfig == nil {
		t.Fatal("expected TLS config for require")
	}
	if poolCfg.ConnConfig.TLSConfig.ServerName != "db.example.com" {
		t.Errorf("ServerName = %q, want %q", poolCfg.ConnConfig.TLSConfig.ServerName, "db.example.com")
	}
	if poolCfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Error("require should set InsecureSkipVerify=false to verify server certificate")
	}
}

func TestConfigureSSL_VerifyCA(t *testing.T) {
	poolCfg := &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}
	cfg := &config.DatabaseConfig{Host: "db.example.com", SSLMode: "verify-ca"}
	configureSSL(poolCfg, cfg)
	if poolCfg.ConnConfig.TLSConfig == nil {
		t.Fatal("expected TLS config for verify-ca")
	}
	if !poolCfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Error("verify-ca should set InsecureSkipVerify=true")
	}
}

func TestConfigureSSL_VerifyFull(t *testing.T) {
	poolCfg := &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}
	cfg := &config.DatabaseConfig{Host: "db.example.com", SSLMode: "verify-full"}
	configureSSL(poolCfg, cfg)
	if poolCfg.ConnConfig.TLSConfig == nil {
		t.Fatal("expected TLS config for verify-full")
	}
	if poolCfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Error("verify-full should set InsecureSkipVerify=false")
	}
}

func TestConfigureSSL_Prefer(t *testing.T) {
	poolCfg := &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}
	cfg := &config.DatabaseConfig{Host: "db.example.com", SSLMode: "prefer"}
	configureSSL(poolCfg, cfg)
	if poolCfg.ConnConfig.TLSConfig == nil {
		t.Fatal("expected TLS config for prefer")
	}
	if !poolCfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Error("prefer should set InsecureSkipVerify=true")
	}
}

func TestConfigureSSL_Disable(t *testing.T) {
	poolCfg := &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}
	cfg := &config.DatabaseConfig{Host: "db.example.com", SSLMode: "disable"}
	configureSSL(poolCfg, cfg)
	if poolCfg.ConnConfig.TLSConfig != nil {
		t.Error("disable should not set TLS config")
	}
}

func TestConfigureSSL_MinVersion(t *testing.T) {
	for _, mode := range []string{"require", "verify-ca", "verify-full"} {
		poolCfg := &pgxpool.Config{ConnConfig: &pgx.ConnConfig{}}
		cfg := &config.DatabaseConfig{Host: "h", SSLMode: mode}
		configureSSL(poolCfg, cfg)
		if poolCfg.ConnConfig.TLSConfig == nil {
			t.Fatalf("mode %q: nil TLS config", mode)
		}
		if poolCfg.ConnConfig.TLSConfig.MinVersion != 0x0303 {
			t.Errorf("mode %q: MinVersion = %x, want TLS 1.2", mode, poolCfg.ConnConfig.TLSConfig.MinVersion)
		}
	}
}

// conn.go: constructors

func TestNewConn(t *testing.T) {
	c := NewConn(nil)
	if c == nil {
		t.Fatal("NewConn returned nil")
	}
	if c.Pool() != nil {
		t.Fatal("expected nil pool")
	}
}

func TestConn_Pool(t *testing.T) {
	c := &Conn{pool: nil}
	if c.Pool() != nil {
		t.Fatal("expected nil pool")
	}
}

// conn.go: QueryRow routing

func TestConn_QueryRow_TxPath(t *testing.T) {
	called := false
	tx := &mockTx{
		queryRowFn: func(ctx context.Context, sql string, args ...any) pgx.Row {
			called = true
			return &mockRow{}
		},
	}
	ctx := WithTx(context.Background(), tx)
	c := NewConn(nil)
	row := c.QueryRow(ctx, "SELECT 1")
	if row == nil {
		t.Fatal("expected non-nil row")
	}
	if !called {
		t.Fatal("expected tx.QueryRow to be called")
	}
}

func TestConn_QueryRow_PoolPath_Panics(t *testing.T) {
	c := NewConn(nil)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil pool fallback")
			}
		}()
		c.QueryRow(context.Background(), "SELECT 1")
	}()
}

// conn.go: Query routing

func TestConn_Query_TxPath(t *testing.T) {
	called := false
	tx := &mockTx{
		queryFn: func(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
			called = true
			return &mockRows{}, nil
		},
	}
	ctx := WithTx(context.Background(), tx)
	c := NewConn(nil)
	rows, err := c.Query(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rows == nil {
		t.Fatal("expected non-nil rows")
	}
	if !called {
		t.Fatal("expected tx.Query to be called")
	}
}

func TestConn_Query_PoolPath_Panics(t *testing.T) {
	c := NewConn(nil)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil pool fallback")
			}
		}()
		c.Query(context.Background(), "SELECT 1")
	}()
}

// conn.go: Exec routing

func TestConn_Exec_TxPath(t *testing.T) {
	called := false
	tx := &mockTx{
		execFn: func(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
			called = true
			return pgconn.CommandTag{}, nil
		},
	}
	ctx := WithTx(context.Background(), tx)
	c := NewConn(nil)
	_, err := c.Exec(ctx, "INSERT INTO test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("expected tx.Exec to be called")
	}
}

func TestConn_Exec_PoolPath_Panics(t *testing.T) {
	c := NewConn(nil)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil pool fallback")
			}
		}()
		c.Exec(context.Background(), "INSERT INTO test")
	}()
}

// conn.go: Begin tx path

func TestConn_Begin_TxPath(t *testing.T) {
	tx := &mockTx{}
	ctx := WithTx(context.Background(), tx)
	c := NewConn(nil)
	stx, err := c.Begin(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stx == nil {
		t.Fatal("expected non-nil savepoint tx")
	}
	tx.mu.Lock()
	if len(tx.execCalls) == 0 || tx.execCalls[0] != "SAVEPOINT sp_1" {
		t.Errorf("expected SAVEPOINT sp_1, got calls: %v", tx.execCalls)
	}
	tx.mu.Unlock()
}

func TestConn_Begin_TxPath_Nested(t *testing.T) {
	tx := &mockTx{}
	ctx := WithTx(context.Background(), tx)
	c := NewConn(nil)
	stx, err := c.Begin(ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	stx2, err := stx.Begin(ctx)
	if err != nil {
		t.Fatalf("unexpected error on nested begin: %v", err)
	}
	if stx2 == nil {
		t.Fatal("expected non-nil nested savepoint tx")
	}
	tx.mu.Lock()
	found := false
	for _, call := range tx.execCalls {
		if call == "SAVEPOINT sp_1_2" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected SAVEPOINT sp_1_2, got calls: %v", tx.execCalls)
	}
	tx.mu.Unlock()
}

func TestConn_Begin_PoolPath_Panics(t *testing.T) {
	c := NewConn(nil)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil pool fallback")
			}
		}()
		c.Begin(context.Background())
	}()
}

// savepointTx: Commit / Rollback / Begin

func TestSavepointTx_Commit(t *testing.T) {
	tx := &mockTx{}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	err := sptx.Commit(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tx.mu.Lock()
	if len(tx.execCalls) != 1 || tx.execCalls[0] != "RELEASE SAVEPOINT sp_1" {
		t.Errorf("expected RELEASE SAVEPOINT sp_1, got: %v", tx.execCalls)
	}
	tx.mu.Unlock()
}

func TestSavepointTx_Rollback(t *testing.T) {
	tx := &mockTx{}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	err := sptx.Rollback(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	tx.mu.Lock()
	if len(tx.execCalls) != 1 || tx.execCalls[0] != "ROLLBACK TO SAVEPOINT sp_1" {
		t.Errorf("expected ROLLBACK TO SAVEPOINT sp_1, got: %v", tx.execCalls)
	}
	tx.mu.Unlock()
}

func TestSavepointTx_Begin(t *testing.T) {
	tx := &mockTx{}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	nested, err := sptx.Begin(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if nested == nil {
		t.Fatal("expected non-nil nested savepoint tx")
	}
	tx.mu.Lock()
	if len(tx.execCalls) != 1 || tx.execCalls[0] != "SAVEPOINT sp_1_2" {
		t.Errorf("expected SAVEPOINT sp_1_2, got: %v", tx.execCalls)
	}
	tx.mu.Unlock()
}

func TestSavepointTx_Begin_TripleNest(t *testing.T) {
	tx := &mockTx{}
	sptx := &savepointTx{Tx: tx, name: "sp_1", counter: 1}
	sptx2, _ := sptx.Begin(context.Background())
	sptx3, err := sptx2.Begin(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sptx3 == nil {
		t.Fatal("expected non-nil")
	}
	tx.mu.Lock()
	found := false
	for _, c := range tx.execCalls {
		if c == "SAVEPOINT sp_1_2_3" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected SAVEPOINT sp_1_2_3, got: %v", tx.execCalls)
	}
	tx.mu.Unlock()
}

// conn.go: HealthCheck / Close panics

func TestConn_HealthCheck_PoolPath_Panics(t *testing.T) {
	c := NewConn(nil)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil pool")
			}
		}()
		c.HealthCheck(context.Background())
	}()
}

func TestConn_Close_PoolPath_Panics(t *testing.T) {
	c := NewConn(nil)
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil pool")
			}
		}()
		c.Close()
	}()
}

// postgres.go: trivial wrappers

func TestPostgresConn(t *testing.T) {
	p := &Postgres{Pool: nil}
	c := p.Conn()
	if c == nil {
		t.Fatal("Postgres.Conn() returned nil")
	}
}

func TestPoolStats_NilPool(t *testing.T) {
	p := &Postgres{Pool: nil}
	if s := p.PoolStats(); s != nil {
		t.Fatal("expected nil stats for nil pool")
	}
}

func TestClose_NilPool(t *testing.T) {
	p := &Postgres{Pool: nil}
	p.Close()
}

func TestVerifyRLS_NilPool(t *testing.T) {
	p := &Postgres{Pool: nil}
	if err := p.VerifyRLS(context.Background()); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestPoolHealthy_NilPool(t *testing.T) {
	p := &Postgres{Pool: nil}
	if p.PoolHealthy() {
		t.Fatal("expected unhealthy for nil pool")
	}
}

// postgres.go: PurgeOldEvents

func TestPurgeOldEvents_NilPool(t *testing.T) {
	p := &Postgres{Pool: nil}
	n, err := p.PurgeOldEvents(context.Background(), 30)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestPurgeOldEvents_ZeroRetention(t *testing.T) {
	p := &Postgres{Pool: nil}
	n, err := p.PurgeOldEvents(context.Background(), 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

func TestPurgeOldEvents_NegativeRetention(t *testing.T) {
	p := &Postgres{Pool: nil}
	n, err := p.PurgeOldEvents(context.Background(), -1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0, got %d", n)
	}
}

// postgres.go: StartEventPurger

func TestStartEventPurger_NilPool_Defaults(t *testing.T) {
	p := &Postgres{Pool: nil}
	cancel := p.StartEventPurger(context.Background(), 0, 0)
	if cancel == nil {
		t.Fatal("expected non-nil cancel func")
	}
	time.Sleep(10 * time.Millisecond)
	cancel()
}

func TestStartEventPurger_TickerPath(t *testing.T) {
	p := &Postgres{Pool: nil}
	ctx, cancel := context.WithCancel(context.Background())
	p.StartEventPurger(ctx, 30, 1*time.Millisecond)
	time.Sleep(25 * time.Millisecond)
	cancel()
}

// postgres.go: HealthCheck panics

func TestPostgres_HealthCheck_NilPool(t *testing.T) {
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

// redis.go: nil client paths

func TestRedis_HealthCheck_NilClient(t *testing.T) {
	r := &Redis{Client: nil}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("expected panic for nil client")
			}
		}()
		r.HealthCheck(context.Background())
	}()
}

func TestRedis_Close_NilClient(t *testing.T) {
	r := &Redis{Client: nil}
	r.Close()
}

// Skipped tests for live connections

func TestMigrate_Skipped(t *testing.T) {
	t.Skip("requires live postgres connection")
}

func TestCurrentVersion_Skipped(t *testing.T) {
	t.Skip("requires live postgres connection")
}

func TestNewPostgres_Skipped(t *testing.T) {
	t.Skip("requires live postgres connection")
}

func TestNewRedis_Skipped(t *testing.T) {
	t.Skip("requires live redis connection")
}
