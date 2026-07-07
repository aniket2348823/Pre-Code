// Package database provides context-aware database access for RLS support.
// The Conn type wraps pgxpool.Pool and checks request context for a dedicated
// connection or transaction. This ensures that session variables set by the
// auth middleware (app.current_user_id) are visible to all queries in the
// same request, because they execute on the same database connection.
package database

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Conn wraps a pgxpool.Pool and provides context-aware query methods.
// When a dedicated connection or transaction is stored in context (via
// WithConn or WithTx), queries execute on that connection. Otherwise,
// they fall back to the shared pool.
type Conn struct {
	pool *pgxpool.Pool
}

// NewConn creates a context-aware connection wrapper around the pool.
func NewConn(pool *pgxpool.Pool) *Conn {
	return &Conn{pool: pool}
}

// Pool returns the underlying pgxpool.Pool for operations that require
// direct pool access (e.g., Acquire for middleware, Begin for transactions).
func (c *Conn) Pool() *pgxpool.Pool {
	return c.pool
}

// QueryRow executes a single-row query using the connection/transaction from
// context, falling back to the pool.
func (c *Conn) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.QueryRow(ctx, sql, args...)
	}
	if conn, ok := ConnFromContext(ctx); ok {
		return conn.QueryRow(ctx, sql, args...)
	}
	return c.pool.QueryRow(ctx, sql, args...)
}

// Query executes a multi-row query using the connection/transaction from
// context, falling back to the pool.
func (c *Conn) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.Query(ctx, sql, args...)
	}
	if conn, ok := ConnFromContext(ctx); ok {
		return conn.Query(ctx, sql, args...)
	}
	return c.pool.Query(ctx, sql, args...)
}

// Exec executes a command using the connection/transaction from context,
// falling back to the pool.
func (c *Conn) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if tx, ok := TxFromContext(ctx); ok {
		return tx.Exec(ctx, sql, args...)
	}
	if conn, ok := ConnFromContext(ctx); ok {
		return conn.Exec(ctx, sql, args...)
	}
	return c.pool.Exec(ctx, sql, args...)
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
		return &savepointTx{Tx: tx, name: name, counter: 1}, nil
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
	return &savepointTx{Tx: s.Tx, name: name, counter: 0}, nil
}
