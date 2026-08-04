// Package database provides context-aware database access for RLS support.
// The Conn type wraps pgxpool.Pool and checks request context for a dedicated
// connection or transaction. This ensures that session variables set by the
// auth middleware (app.current_user_id) are visible to all queries in the
// same request, because they execute on the same database connection.
package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/vigilagent/vigilagent/internal/telemetry"
)

// Conn wraps a pgxpool.Pool and provides context-aware query methods.
// When a dedicated connection or transaction is stored in context (via
// WithConn or WithTx), queries execute on that connection. Otherwise,
// they fall back to the shared pool.
type Conn struct {
	pool              *pgxpool.Pool
	slowQueryThreshold time.Duration
}

// NewConn creates a context-aware connection wrapper around the pool.
func NewConn(pool *pgxpool.Pool) *Conn {
	return &Conn{pool: pool, slowQueryThreshold: 100 * time.Millisecond}
}

// NewConnWithThreshold creates a connection wrapper with a custom slow query threshold.
func NewConnWithThreshold(pool *pgxpool.Pool, threshold time.Duration) *Conn {
	if threshold <= 0 {
		threshold = 100 * time.Millisecond
	}
	return &Conn{pool: pool, slowQueryThreshold: threshold}
}

// Pool returns the underlying pgxpool.Pool for operations that require
// direct pool access (e.g., Acquire for middleware, Begin for transactions).
func (c *Conn) Pool() *pgxpool.Pool {
	return c.pool
}

// QueryRow executes a single-row query using the connection/transaction from
// context, falling back to the pool. Checks the DB circuit breaker before
// pool access — returns an error row if the breaker is open.
func (c *Conn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	start := time.Now()
	var row pgx.Row
	if tx, ok := TxFromContext(ctx); ok {
		row = tx.QueryRow(ctx, sql, args...)
	} else if conn, ok := ConnFromContext(ctx); ok {
		row = conn.QueryRow(ctx, sql, args...)
	} else {
		dbCircuitBreaker.mu.Lock()
		cbState := dbCircuitBreaker.state
		dbCircuitBreaker.mu.Unlock()
		if cbState == "open" {
			recordDBFailure()
			return &errRow{err: ErrDBCircuitOpen}
		}
		row = c.pool.QueryRow(ctx, sql, args...)
	}
	if dur := time.Since(start); dur > c.slowQueryThreshold {
		slog.Warn("slow query",
			"duration_ms", dur.Milliseconds(),
			"query", sql,
			"rows", 1,
		)
		telemetry.SlowQueryDuration.Observe(dur.Seconds())
	}
	return row
}

// errRow implements pgx.Row and always returns an error.
type errRow struct{ err error }

func (r *errRow) Scan(dest ...any) error { return r.err }
func (r *errRow) Conn() *pgx.Conn        { return nil }

// Query executes a multi-row query using the connection/transaction from
// context, falling back to the pool. Checks the DB circuit breaker before
// pool access.
func (c *Conn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	start := time.Now()
	var rows pgx.Rows
	var err error
	if tx, ok := TxFromContext(ctx); ok {
		rows, err = tx.Query(ctx, sql, args...)
	} else if conn, ok := ConnFromContext(ctx); ok {
		rows, err = conn.Query(ctx, sql, args...)
	} else {
		dbCircuitBreaker.mu.Lock()
		cbState := dbCircuitBreaker.state
		dbCircuitBreaker.mu.Unlock()
		if cbState == "open" {
			recordDBFailure()
			return nil, ErrDBCircuitOpen
		}
		rows, err = c.pool.Query(ctx, sql, args...)
		if err != nil {
			recordDBFailure()
		} else {
			recordDBSuccess()
		}
	}
	if dur := time.Since(start); dur > c.slowQueryThreshold {
		var rowCount int
		if rows != nil {
			rowCount = -1 // unknown until consumed
		}
		slog.Warn("slow query",
			"duration_ms", dur.Milliseconds(),
			"query", sql,
			"rows", rowCount,
		)
		telemetry.SlowQueryDuration.Observe(dur.Seconds())
	}
	return rows, err
}

// Exec executes a command using the connection/transaction from context,
// falling back to the pool. Checks the DB circuit breaker before pool access.
func (c *Conn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	start := time.Now()
	var tag pgconn.CommandTag
	var err error
	if tx, ok := TxFromContext(ctx); ok {
		tag, err = tx.Exec(ctx, sql, args...)
	} else if conn, ok := ConnFromContext(ctx); ok {
		tag, err = conn.Exec(ctx, sql, args...)
	} else {
		dbCircuitBreaker.mu.Lock()
		cbState := dbCircuitBreaker.state
		dbCircuitBreaker.mu.Unlock()
		if cbState == "open" {
			recordDBFailure()
			return pgconn.CommandTag{}, ErrDBCircuitOpen
		}
		tag, err = c.pool.Exec(ctx, sql, args...)
		if err != nil {
			recordDBFailure()
		} else {
			recordDBSuccess()
		}
	}
	if dur := time.Since(start); dur > c.slowQueryThreshold {
		rowsAffected := int64(-1)
		if err == nil {
			rowsAffected = tag.RowsAffected()
		}
		slog.Warn("slow query",
			"duration_ms", dur.Milliseconds(),
			"query", sql,
			"rows_affected", rowsAffected,
		)
		telemetry.SlowQueryDuration.Observe(dur.Seconds())
	}
	return tag, err
}

type ctxSavepointCounter struct{}

// Begin starts a new transaction. When a transaction already exists in
// context, it creates a SAVEPOINT on it for nested transaction support.
func (c *Conn) Begin(ctx context.Context) (pgx.Tx, error) {
	if tx, ok := TxFromContext(ctx); ok {
		counter := 0
		if v := ctx.Value(ctxSavepointCounter{}); v != nil {
			counter = v.(int)
		}
		counter++
		name := fmt.Sprintf("sp_%d", counter)
		ctx = context.WithValue(ctx, ctxSavepointCounter{}, counter)
		if _, err := tx.Exec(ctx, "SAVEPOINT "+name); err != nil {
			return nil, err
		}
		return &savepointTx{Tx: tx, name: name, counter: counter}, nil
	}
	return c.pool.Begin(ctx)
}

// HealthCheck pings the underlying pool.
func (c *Conn) HealthCheck(ctx context.Context) error {
	return c.pool.Ping(ctx)
}

// Close closes the underlying pool.
func (c *Conn) Close() {
	c.pool.Close()
}

// savepointTx wraps a pgx.Tx and manages a SAVEPOINT for nested transactions.
// Commit releases the savepoint (not the underlying transaction).
// Rollback rolls back to the savepoint (not the entire transaction).
// All other pgx.Tx methods are delegated to the underlying transaction.
type savepointTx struct {
	pgx.Tx
	name    string
	counter int
}

// Commit releases the savepoint, allowing the outer transaction to continue.
func (s *savepointTx) Commit(ctx context.Context) error {
	_, err := s.Tx.Exec(ctx, "RELEASE SAVEPOINT "+s.name)
	return err
}

// Rollback rolls back to the savepoint, discarding changes since Begin.
func (s *savepointTx) Rollback(ctx context.Context) error {
	_, err := s.Tx.Exec(ctx, "ROLLBACK TO SAVEPOINT "+s.name)
	return err
}

// Begin starts a nested savepoint within the same transaction.
func (s *savepointTx) Begin(ctx context.Context) (pgx.Tx, error) {
	s.counter++
	name := fmt.Sprintf("%s_%d", s.name, s.counter)
	if _, err := s.Tx.Exec(ctx, "SAVEPOINT "+name); err != nil {
		return nil, err
	}
	return &savepointTx{Tx: s.Tx, name: name, counter: s.counter}, nil
}
