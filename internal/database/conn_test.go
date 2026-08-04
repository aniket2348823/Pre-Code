package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

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
