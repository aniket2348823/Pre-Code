package database

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vigilagent/vigilagent/internal/config"
)

// Postgres holds the pgxpool connection pool.
type Postgres struct {
	Pool *pgxpool.Pool
}

// NewPostgres creates a new pgxpool connection pool with SSL enforcement.
func NewPostgres(ctx context.Context, cfg *config.DatabaseConfig) (*Postgres, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DSN())
	if err != nil {
		return nil, fmt.Errorf("unable to parse database config: %w", err)
	}

	// Disable prepared statement caching for compatibility with PgBouncer / Supabase Pooler
	poolCfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol

	// Enforce SSL/TLS in production
	if shouldEnforceSSL(cfg) {
		configureSSL(poolCfg, cfg)
		slog.Info("database SSL enforced", "sslmode", cfg.SSLMode)
	}

	poolCfg.MaxConns = int32(cfg.MaxOpenConns)
	poolCfg.MinConns = int32(cfg.MaxIdleConns)
	poolCfg.MaxConnLifetime = cfg.MaxLifetime
	poolCfg.MaxConnIdleTime = 5 * time.Minute

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
		"sslmode", cfg.SSLMode,
	)

	return &Postgres{Pool: pool}, nil
}

// shouldEnforceSSL returns true if SSL should be enforced.
// Only enforce for modes that explicitly require encryption.
// "prefer" means use SSL if available but don't require it.
func shouldEnforceSSL(cfg *config.DatabaseConfig) bool {
	mode := strings.ToLower(cfg.SSLMode)
	switch mode {
	case "require", "verify-ca", "verify-full":
		return true
	default:
		return false
	}
}

// configureSSL sets up TLS configuration for the database connection.
func configureSSL(poolCfg *pgxpool.Config, cfg *config.DatabaseConfig) {
	mode := strings.ToLower(cfg.SSLMode)

	switch mode {
	case "require":
		poolCfg.ConnConfig.TLSConfig = &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
		}
	case "verify-ca", "verify-full":
		poolCfg.ConnConfig.TLSConfig = &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: mode == "verify-ca",
			MinVersion:         tls.VersionTLS12,
		}
	case "prefer":
		poolCfg.ConnConfig.TLSConfig = &tls.Config{
			ServerName:         cfg.Host,
			InsecureSkipVerify: true,
			MinVersion:         tls.VersionTLS12,
		}
	}
}

// Conn returns a context-aware connection wrapper that checks for dedicated
// connections/transactions in context before falling back to the pool.
// This is the recommended way to execute queries when RLS is enabled.
func (p *Postgres) Conn() *Conn {
	return NewConn(p.Pool)
}

// HealthCheck pings the database to verify connectivity.
func (p *Postgres) HealthCheck(ctx context.Context) error {
	return p.Pool.Ping(ctx)
}

// PoolStats returns connection pool utilization metrics.
// Use this for monitoring and circuit breaker decisions.
func (p *Postgres) PoolStats() *pgxpool.Stat {
	if p.Pool == nil {
		return nil
	}
	return p.Pool.Stat()
}

// VerifyRLS checks that Row-Level Security is enabled on critical tables.
// This should be called at startup to prevent silent cross-org data leaks.
func (p *Postgres) VerifyRLS(ctx context.Context) error {
	if p.Pool == nil {
		return nil
	}
	tables := []string{"users", "organizations", "projects", "tasks", "events"}
	for _, table := range tables {
		var rlsEnabled bool
		err := p.Pool.QueryRow(ctx,
			"SELECT c.relrowsecurity FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace WHERE c.relname = $1 AND n.nspname = 'public'",
			table,
		).Scan(&rlsEnabled)
		if err != nil {
			slog.Warn("RLS check: could not verify table (may not exist yet)", "table", table, "error", err)
			continue
		}
		if !rlsEnabled {
			slog.Warn("RLS NOT ENABLED on table", "table", table, "action", "data leak risk across organizations")
		}
	}
	return nil
}

// PoolHealthy returns true if the pool has available connections
// and utilization is below the threshold (circuit breaker).
func (p *Postgres) PoolHealthy() bool {
	if p.Pool == nil {
		return false
	}
	stat := p.Pool.Stat()
	acquiredConns := stat.AcquiredConns()
	maxConns := stat.MaxConns()
	if maxConns == 0 {
		return true
	}
	utilization := float64(acquiredConns) / float64(maxConns)
	return utilization < 0.8 // circuit breaker: unhealthy at 80% utilization
}

// PurgeOldEvents deletes events older than the given retention period.
// Call this periodically (e.g., daily) to prevent unbounded table growth.
func (p *Postgres) PurgeOldEvents(ctx context.Context, retentionDays int) (int64, error) {
	if p.Pool == nil || retentionDays <= 0 {
		return 0, nil
	}
	query := `DELETE FROM events WHERE created_at < NOW() - INTERVAL '1 day' * $1`
	tag, err := p.Pool.Exec(ctx, query, retentionDays)
	if err != nil {
		return 0, fmt.Errorf("failed to purge old events: %w", err)
	}
	rows := tag.RowsAffected()
	if rows > 0 {
		slog.Info("purged old events", "rows_deleted", rows, "retention_days", retentionDays)
	}
	return rows, nil
}

// StartEventPurger starts a background goroutine that periodically purges old events.
// Returns a stop function to shut it down gracefully.
func (p *Postgres) StartEventPurger(ctx context.Context, retentionDays int, interval time.Duration) context.CancelFunc {
	if retentionDays <= 0 {
		retentionDays = 90 // default: keep 90 days
	}
	if interval <= 0 {
		interval = 24 * time.Hour // default: daily
	}
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := p.PurgeOldEvents(ctx, retentionDays); err != nil {
					slog.Warn("event purger failed", "error", err)
				}
			}
		}
	}()
	return cancel
}

// Close closes the connection pool.
func (p *Postgres) Close() {
	if p.Pool != nil {
		p.Pool.Close()
		slog.Info("postgres connection pool closed")
	}
}


