package database

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vigilagent/vigilagent/internal/config"
)

// Postgres holds the pgxpool connection pool.
type Postgres struct {
	Pool *pgxpool.Pool
}

// NewPostgres creates a new pgxpool connection pool.
func NewPostgres(ctx context.Context, cfg *config.DatabaseConfig) (*Postgres, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("unable to parse database config: %w", err)
	}

	poolCfg.MaxConns = int32(cfg.MaxOpenConns)
	poolCfg.MinConns = int32(cfg.MaxIdleConns)
	poolCfg.MaxConnLifetime = cfg.MaxLifetime
	poolCfg.MaxConnIdleTime = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("unable to create connection pool: %w", err)
	}

	// Verify connectivity
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("unable to ping database: %w", err)
	}

	slog.Info("connected to postgres",
		"host", cfg.Host,
		"port", cfg.Port,
		"database", cfg.Name,
		"max_conns", cfg.MaxOpenConns,
	)

	return &Postgres{Pool: pool}, nil
}

// HealthCheck pings the database to verify connectivity.
func (p *Postgres) HealthCheck(ctx context.Context) error {
	return p.Pool.Ping(ctx)
}

// Close closes the connection pool.
func (p *Postgres) Close() {
	if p.Pool != nil {
		p.Pool.Close()
		slog.Info("postgres connection pool closed")
	}
}
