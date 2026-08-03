package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// --- Context overwrite ---

func TestWithConn_Overwrite(t *testing.T) {
	ctx := context.Background()
	ctx = WithConn(ctx, nil)
	ctx = WithConn(ctx, nil)
	conn, ok := ConnFromContext(ctx)
	if !ok {
		t.Fatal("expected conn in context")
	}
	if conn != nil {
		t.Fatal("expected nil conn")
	}
}

func TestWithTx_Overwrite(t *testing.T) {
	tx1 := &mockTx{}
	tx2 := &mockTx{}
	ctx := context.Background()
	ctx = WithTx(ctx, tx1)
	ctx = WithTx(ctx, tx2)
	got, ok := TxFromContext(ctx)
	if !ok {
		t.Fatal("expected tx in context")
	}
	if got != tx2 {
		t.Fatal("expected second tx to overwrite first")
	}
}

// --- Routing priority: Tx > Conn ---

func TestConn_QueryRow_TxOverConnPriority(t *testing.T) {
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
	c.QueryRow(ctx, "SELECT 1")
	if !txCalled {
		t.Fatal("expected Tx.QueryRow to be called when both Tx and Conn are in context")
	}
}

func TestConn_Query_TxOverConnPriority(t *testing.T) {
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
	_, err := c.Query(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !txCalled {
		t.Fatal("expected Tx.Query to be called when both Tx and Conn are in context")
	}
}

func TestConn_Exec_TxOverConnPriority(t *testing.T) {
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

// --- ConnFromContext edge cases ---

func TestConnFromContext_EmptyContext(t *testing.T) {
	_, ok := ConnFromContext(context.Background())
	if ok {
		t.Fatal("expected false for empty context")
	}
}

func TestConnFromContext_WrongValueType(t *testing.T) {
	type customKey string
	ctx := context.WithValue(context.Background(), customKey("db_session_conn"), 42)
	_, ok := ConnFromContext(ctx)
	if ok {
		t.Fatal("expected false for wrong value type")
	}
}

func TestConnFromContext_NilConnStoredViaWithConn(t *testing.T) {
	ctx := WithConn(context.Background(), nil)
	conn, ok := ConnFromContext(ctx)
	if !ok {
		t.Fatal("expected true for nil *pgxpool.Conn stored via WithConn")
	}
	if conn != nil {
		t.Fatal("expected nil conn")
	}
}

// --- TxFromContext edge cases ---

func TestTxFromContext_EmptyContext(t *testing.T) {
	_, ok := TxFromContext(context.Background())
	if ok {
		t.Fatal("expected false for empty context")
	}
}

func TestTxFromContext_WrongValueType(t *testing.T) {
	type customKey string
	ctx := context.WithValue(context.Background(), customKey("db_session_tx"), "not-a-tx")
	_, ok := TxFromContext(ctx)
	if ok {
		t.Fatal("expected false for wrong value type")
	}
}

func TestTxFromContext_NilInterfaceValue(t *testing.T) {
	ctx := context.WithValue(context.Background(), sessionTxKey, nil)
	_, ok := TxFromContext(ctx)
	if ok {
		t.Fatal("expected false for nil interface stored as pgx.Tx")
	}
}

// --- Multiple context layers ---

type testContextKey string

func TestWithConn_NestedContexts(t *testing.T) {
	ctx := context.Background()
	ctx = WithConn(ctx, nil)
	childCtx := context.WithValue(ctx, testContextKey("other"), "value")
	conn, ok := ConnFromContext(childCtx)
	if !ok {
		t.Fatal("expected conn in child context")
	}
	if conn != nil {
		t.Fatal("expected nil conn")
	}
}

func TestWithTx_NestedContexts(t *testing.T) {
	tx := &mockTx{}
	ctx := context.Background()
	ctx = WithTx(ctx, tx)
	childCtx := context.WithValue(ctx, testContextKey("other"), "value")
	got, ok := TxFromContext(childCtx)
	if !ok {
		t.Fatal("expected tx in child context")
	}
	if got != tx {
		t.Fatal("expected same tx in child context")
	}
}

// --- Conn and Tx independence ---

func TestConnAndTx_IndependentStorage(t *testing.T) {
	tx := &mockTx{}
	ctx := context.Background()
	ctx = WithTx(ctx, tx)
	ctx = WithConn(ctx, nil)
	gotTx, okTx := TxFromContext(ctx)
	if !okTx || gotTx != tx {
		t.Fatal("expected tx to be retrievable")
	}
	gotConn, okConn := ConnFromContext(ctx)
	if !okConn || gotConn != nil {
		t.Fatal("expected conn to be retrievable")
	}
}

func TestConnOnly_NoTx(t *testing.T) {
	ctx := context.Background()
	ctx = WithConn(ctx, nil)
	_, ok := TxFromContext(ctx)
	if ok {
		t.Fatal("expected no tx when only conn is set")
	}
}

func TestTxOnly_NoConn(t *testing.T) {
	tx := &mockTx{}
	ctx := context.Background()
	ctx = WithTx(ctx, tx)
	_, ok := ConnFromContext(ctx)
	if ok {
		t.Fatal("expected no conn when only tx is set")
	}
}
