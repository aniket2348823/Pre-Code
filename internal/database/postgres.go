package database

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/vigilagent/vigilagent/internal/config"
)

// ErrDBCircuitOpen is returned when the DB circuit breaker is open.
var ErrDBCircuitOpen = errors.New("database circuit breaker is open")

// dbCircuitBreaker is a package-level circuit breaker for the database pool.
var dbCircuitBreaker = &struct {
	mu           sync.Mutex
	state        string // "closed", "open", "half-open"
	failCount    int
	successCount int
	lastFailTime time.Time
	threshold    int
	resetTimeout time.Duration
}{
	state:       "closed",
	threshold:   5,
	resetTimeout: 30 * time.Second,
}

// CheckDBHealth returns nil if the database circuit breaker is closed (healthy).
// Call this from health check endpoints.
func CheckDBHealth() error {
	dbCircuitBreaker.mu.Lock()
	defer dbCircuitBreaker.mu.Unlock()

	switch dbCircuitBreaker.state {
	case "open":
		if time.Since(dbCircuitBreaker.lastFailTime) > dbCircuitBreaker.resetTimeout {
			dbCircuitBreaker.state = "half-open"
			dbCircuitBreaker.failCount = 0
			return nil
		}
		return ErrDBCircuitOpen
	case "half-open":
		return nil
	default:
		return nil
	}
}

// recordDBFailure increments the circuit breaker failure count and opens it when
// the threshold is exceeded.
func recordDBFailure() {
	dbCircuitBreaker.mu.Lock()
	defer dbCircuitBreaker.mu.Unlock()
	dbCircuitBreaker.failCount++
	dbCircuitBreaker.lastFailTime = time.Now()
	if dbCircuitBreaker.failCount >= dbCircuitBreaker.threshold {
		dbCircuitBreaker.state = "open"
		slog.Warn("DB circuit breaker OPEN", "fail_count", dbCircuitBreaker.failCount)
	}
}

// recordDBSuccess resets the circuit breaker on success (half-open → closed).
func recordDBSuccess() {
	dbCircuitBreaker.mu.Lock()
	defer dbCircuitBreaker.mu.Unlock()
	if dbCircuitBreaker.state == "half-open" {
		dbCircuitBreaker.state = "closed"
		dbCircuitBreaker.failCount = 0
		slog.Info("DB circuit breaker CLOSED (recovered)")
	}
}

// Postgres holds the pgxpool connection pool.
type Postgres struct {
	Pool       *pgxpool.Pool
	cancelStats context.CancelFunc
}

var (
	poolOpenConnections = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "vigilagent",
		Subsystem: "db_pool",
		Name:      "open_connections",
		Help:      "Number of open database connections",
	})
	poolInUse = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "vigilagent",
		Subsystem: "db_pool",
		Name:      "in_use",
		Help:      "Number of connections currently in use",
	})
	poolIdle = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "vigilagent",
		Subsystem: "db_pool",
		Name:      "idle",
		Help:      "Number of idle connections in the pool",
	})
	poolWaitCount = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "vigilagent",
		Subsystem: "db_pool",
		Name:      "wait_count_total",
		Help:      "Total number of connections waited for",
	})
	poolWaitDuration = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "vigilagent",
		Subsystem: "db_pool",
		Name:      "wait_duration_seconds_total",
		Help:      "Total time spent waiting for a connection",
	})
	poolMaxIdleClosed = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "vigilagent",
		Subsystem: "db_pool",
		Name:      "max_idle_closed_total",
		Help:      "Total number of connections closed due to max idle time",
	})
	poolMaxLifetimeClosed = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "vigilagent",
		Subsystem: "db_pool",
		Name:      "max_lifetime_closed_total",
		Help:      "Total number of connections closed due to max lifetime",
	})
	poolUtilization = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "vigilagent",
		Subsystem: "db_pool",
		Name:      "utilization_ratio",
		Help:      "Connection pool utilization ratio (0.0 to 1.0)",
	})
	poolHealthUp = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "vigilagent",
		Subsystem: "db_pool",
		Name:      "health",
		Help:      "Pool health status (1 = healthy, 0 = unhealthy)",
	})
)

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

	// Apply pool config with fallback to legacy fields
	maxOpen := cfg.PoolMaxOpen
	if maxOpen <= 0 {
		maxOpen = cfg.MaxOpenConns
	}
	maxIdle := cfg.PoolMaxIdle
	if maxIdle <= 0 {
		maxIdle = cfg.MaxIdleConns
	}
	maxLifetime := cfg.PoolMaxLifetime
	if maxLifetime <= 0 {
		maxLifetime = cfg.MaxLifetime
	}
	maxIdleTime := cfg.PoolMaxIdleTime
	if maxIdleTime <= 0 {
		if cfg.ConnIdleTime > 0 {
			maxIdleTime = cfg.ConnIdleTime
		} else {
			maxIdleTime = 3 * time.Minute
		}
	}

	poolCfg.MaxConns = int32(maxOpen)
	poolCfg.MinConns = int32(maxIdle)
	poolCfg.MaxConnLifetime = maxLifetime
	poolCfg.MaxConnIdleTime = maxIdleTime

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
		"max_conns", maxOpen,
		"max_idle", maxIdle,
		"max_lifetime", maxLifetime,
		"max_idle_time", maxIdleTime,
		"sslmode", cfg.SSLMode,
	)

	p := &Postgres{Pool: pool}

	// Start periodic pool stats logging and metrics export
	statsCtx, statsCancel := context.WithCancel(ctx)
	p.cancelStats = statsCancel
	go p.collectPoolStats(statsCtx, cfg)

	return p, nil
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
			InsecureSkipVerify: false,
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
	if p.cancelStats != nil {
		p.cancelStats()
	}
	if p.Pool != nil {
		p.Pool.Close()
		slog.Info("postgres connection pool closed")
	}
}

// collectPoolStats periodically logs pool stats and updates Prometheus metrics.
func (p *Postgres) collectPoolStats(ctx context.Context, cfg *config.DatabaseConfig) {
	interval := cfg.PoolStatsInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// Log once immediately on startup
	p.updatePoolMetrics()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.updatePoolMetrics()
		}
	}
}

// updatePoolMetrics snapshots current pool stats into Prometheus gauges and logs.
func (p *Postgres) updatePoolMetrics() {
	if p.Pool == nil {
		return
	}
	stat := p.Pool.Stat()

	openConns := int64(stat.TotalConns())
	inUse := int64(stat.AcquiredConns())
	idle := openConns - inUse
	if idle < 0 {
		idle = 0
	}

	poolOpenConnections.Set(float64(openConns))
	poolInUse.Set(float64(inUse))
	poolIdle.Set(float64(idle))
	poolWaitCount.Set(float64(stat.EmptyAcquireCount()))
	poolWaitDuration.Set(stat.AcquireDuration().Seconds())
	poolMaxIdleClosed.Set(float64(stat.MaxIdleDestroyCount()))
	poolMaxLifetimeClosed.Set(float64(stat.MaxLifetimeDestroyCount()))

	maxConns := stat.MaxConns()
	var utilization float64
	if maxConns > 0 {
		utilization = float64(inUse) / float64(maxConns)
	}
	poolUtilization.Set(utilization)

	healthy := utilization < 0.8
	if healthy {
		poolHealthUp.Set(1)
	} else {
		poolHealthUp.Set(0)
		slog.Warn("DB pool utilization above 80%",
			"in_use", inUse,
			"max_conns", maxConns,
			"utilization", fmt.Sprintf("%.2f%%", utilization*100),
		)
	}

	slog.Debug("db pool stats",
		"open", openConns,
		"in_use", inUse,
		"idle", idle,
		"empty_acquire_count", stat.EmptyAcquireCount(),
		"acquire_duration_ms", stat.AcquireDuration().Milliseconds(),
		"utilization", fmt.Sprintf("%.2f%%", utilization*100),
	)
}

// PoolHealthCheck returns a detailed pool health status including utilization
// and all pool stats. Intended for health check HTTP endpoints.
func (p *Postgres) PoolHealthCheck(ctx context.Context) (map[string]any, error) {
	if p.Pool == nil {
		return nil, fmt.Errorf("pool is nil")
	}
	if err := p.Pool.Ping(ctx); err != nil {
		return nil, fmt.Errorf("ping failed: %w", err)
	}

	stat := p.Pool.Stat()
	maxConns := stat.MaxConns()
	inUse := stat.AcquiredConns()
	var utilization float64
	if maxConns > 0 {
		utilization = float64(inUse) / float64(maxConns)
	}

	result := map[string]any{
		"status":                "healthy",
		"ping":                  "ok",
		"total_conns":           stat.TotalConns(),
		"acquired_conns":        stat.AcquiredConns(),
		"max_conns":             maxConns,
		"constructing_conns":    stat.ConstructingConns(),
		"empty_acquire_count":   stat.EmptyAcquireCount(),
		"acquire_duration_ms":   stat.AcquireDuration().Milliseconds(),
		"max_idle_destroy":      stat.MaxIdleDestroyCount(),
		"max_lifetime_destroy":  stat.MaxLifetimeDestroyCount(),
		"utilization":           fmt.Sprintf("%.2f%%", utilization*100),
	}

	if utilization >= 0.8 {
		result["status"] = "degraded"
		result["warning"] = fmt.Sprintf("pool utilization at %.1f%%, approaching exhaustion", utilization*100)
	}

	return result, nil
}


