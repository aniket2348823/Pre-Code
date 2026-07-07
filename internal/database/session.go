// Package database provides context-based DB session management for RLS.
// When RLS is enabled, the auth middleware acquires a dedicated connection,
// sets the session variable app.current_user_id, and stores it in context.
// The Conn type extracts the connection from context to ensure the same
// connection is used for all queries in a request.
package database

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ctxKey string

const sessionConnKey ctxKey = "db_session_conn"
const sessionTxKey ctxKey = "db_session_tx"

// WithConn stores a dedicated DB connection in context for the request lifecycle.
// The caller must call conn.Release() when the request is done.
func WithConn(ctx context.Context, conn *pgxpool.Conn) context.Context {
	return context.WithValue(ctx, sessionConnKey, conn)
}

// ConnFromContext extracts a dedicated DB connection from context.
// Returns nil, false if no dedicated connection exists (e.g., public routes).
func ConnFromContext(ctx context.Context) (*pgxpool.Conn, bool) {
	conn, ok := ctx.Value(sessionConnKey).(*pgxpool.Conn)
	return conn, ok
}

// WithTx stores a database transaction in context for the request lifecycle.
// When present, Conn methods use the transaction instead of the pool.
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, sessionTxKey, tx)
}

// TxFromContext extracts a database transaction from context.
// Returns nil, false if no transaction exists.
func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(sessionTxKey).(pgx.Tx)
	return tx, ok
}


