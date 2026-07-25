package database

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vigilagent/vigilagent/internal/config"
)

type mockRow struct{ scanErr error }

func (r *mockRow) Scan(dest ...any) error { return r.scanErr }

type mockRows struct{ closed bool }

func (m *mockRows) Close()                                        { m.closed = true }
func (m *mockRows) Err() error                                    { return nil }
func (m *mockRows) CommandTag() pgconn.CommandTag                 { return pgconn.CommandTag{} }
func (m *mockRows) FieldDescriptions() []pgconn.FieldDescription  { return nil }
func (m *mockRows) Next() bool                                    { return false }
func (m *mockRows) Scan(dest ...any) error                        { return nil }
func (m *mockRows) Values() ([]any, error)                        { return nil, nil }
func (m *mockRows) RawValues() [][]byte                           { return nil }
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
func (m *mockTx) Rollback(ctx context.Context) error  { return nil }
func (m *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (m *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (m *mockTx) LargeObjects() pgx.LargeObjects                                { return pgx.LargeObjects{} }
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
	if !poolCfg.ConnConfig.TLSConfig.InsecureSkipVerify {
		t.Error("require should set InsecureSkipVerify=true")
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
