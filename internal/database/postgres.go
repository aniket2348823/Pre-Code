package database

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/telemetry"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/vigilagent/vigilagent/internal/config"
)

// Content from postgres.go
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
	state:        "closed",
	threshold:    5,
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
	Pool        *pgxpool.Pool
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
			InsecureSkipVerify: false, // InsecureSkipVerify=true would skip ALL cert validation, not just hostname
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
		"status":               "healthy",
		"ping":                 "ok",
		"total_conns":          stat.TotalConns(),
		"acquired_conns":       stat.AcquiredConns(),
		"max_conns":            maxConns,
		"constructing_conns":   stat.ConstructingConns(),
		"empty_acquire_count":  stat.EmptyAcquireCount(),
		"acquire_duration_ms":  stat.AcquireDuration().Milliseconds(),
		"max_idle_destroy":     stat.MaxIdleDestroyCount(),
		"max_lifetime_destroy": stat.MaxLifetimeDestroyCount(),
		"utilization":          fmt.Sprintf("%.2f%%", utilization*100),
	}

	if utilization >= 0.8 {
		result["status"] = "degraded"
		result["warning"] = fmt.Sprintf("pool utilization at %.1f%%, approaching exhaustion", utilization*100)
	}

	return result, nil
}

// Content from conn.go
// Package database provides context-aware database access for RLS support.
// The Conn type wraps pgxpool.Pool and checks request context for a dedicated
// connection or transaction. This ensures that session variables set by the
// auth middleware (app.current_user_id) are visible to all queries in the
// same request, because they execute on the same database connection.

// Conn wraps a pgxpool.Pool and provides context-aware query methods.
// When a dedicated connection or transaction is stored in context (via
// WithConn or WithTx), queries execute on that connection. Otherwise,
// they fall back to the shared pool.
type Conn struct {
	pool               *pgxpool.Pool
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

// Content from retry.go
// RetryConfig configures the retry behavior for database operations.
type RetryConfig struct {
	MaxAttempts int           // maximum number of attempts (including first). Default 3.
	BaseDelay   time.Duration // base delay for exponential backoff. Default 100ms.
	MaxDelay    time.Duration // cap on backoff delay. Default 5s.
	JitterRatio float64       // ± jitter as a fraction [0, 1). Default 0.2 (±20%).
}

// DefaultRetryConfig returns the standard retry configuration.
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxAttempts: 3,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    5 * time.Second,
		JitterRatio: 0.2,
	}
}

// retryableError returns true if the error is a transient failure that
// should be retried. Constraint violations, data errors, and syntax errors
// are NOT retried.
func retryableError(err error) bool {
	if err == nil {
		return false
	}

	// Connection refused / network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		return true
	}
	// dns errors
	if errors.Is(err, net.ErrClosed) {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}

	// pgconn.PgError for PostgreSQL-specific transient errors
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		// Class 08 — Connection Exception
		case "08000", "08001", "08003", "08006", "08007":
			return true
		// Class 40 — Transaction Rollback (serialization, deadlock, statement timeout)
		case "40001", "40P01":
			return true
		// Class 25 — Invalid Transaction State (transient)
		case "25006":
			return true
		// Class 57 — Operator Intervention (query canceled, admin shutdown)
		case "57014", "57P01", "57P02", "57P03":
			return true
		// Class 53 — Insufficient Resources
		case "53100", "53200", "53300":
			return true
		// Class 54 — Program Limit Exceeded (sort/memory — transient under load)
		case "54000", "54001":
			return true
		}
	}

	// Context deadline exceeded (timeout)
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	// String-based fallback for common transient error messages
	msg := err.Error()
	transientKeywords := []string{
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
	lower := strings.ToLower(msg)
	for _, kw := range transientKeywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}

	return false
}

// retryDelay computes the delay for the given attempt number using exponential
// backoff with jitter.
func retryDelay(attempt int, cfg RetryConfig) time.Duration {
	if attempt <= 0 {
		return 0
	}
	// exponential: base * 2^(attempt-1)
	delay := float64(cfg.BaseDelay) * math.Pow(2, float64(attempt-1))
	if delay > float64(cfg.MaxDelay) {
		delay = float64(cfg.MaxDelay)
	}
	// apply jitter: delay * (1 + random(-jitter, +jitter))
	jitter := cfg.JitterRatio
	if jitter <= 0 {
		return time.Duration(delay)
	}
	jitterRange := delay * jitter
	// rand.Float64 returns [0, 1), so shift to [-jitterRange, +jitterRange]
	offset := (rand.Float64()*2 - 1) * jitterRange
	delay += offset
	if delay < 0 {
		delay = 0
	}
	return time.Duration(delay)
}

// RetryQueryRow wraps a function that returns a pgx.Row with retry logic.
// The function fn is re-invoked on each retry attempt.
func RetryQueryRow(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) pgx.Row) pgx.Row {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultRetryConfig()
	}
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		row := fn(ctx)
		// We can't know if the row has an error without scanning, so we wrap
		// the row with a retry-aware wrapper that only retries on Scan.
		return &retryRow{row: row, fn: fn, cfg: cfg, attempt: attempt}
	}
	return &errRow{err: errors.New("retry: no attempts configured")}
}

// retryRow wraps a pgx.Row and retries on Scan if the error is retryable.
type retryRow struct {
	row     pgx.Row
	fn      func(ctx context.Context) pgx.Row
	cfg     RetryConfig
	attempt int
}

func (r *retryRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	if err != nil && retryableError(err) && r.attempt < r.cfg.MaxAttempts {
		delay := retryDelay(r.attempt, r.cfg)
		slog.Warn("retryable query error, retrying",
			"attempt", r.attempt,
			"max_attempts", r.cfg.MaxAttempts,
			"delay", delay,
			"error", err,
		)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-r.ctx().Done():
			timer.Stop()
			return r.ctx().Err()
		}
		next := r.attempt + 1
		newRow := r.fn(r.ctx())
		return (&retryRow{row: newRow, fn: r.fn, cfg: r.cfg, attempt: next}).Scan(dest...)
	}
	return err
}

func (r *retryRow) ctx() context.Context {
	return context.Background()
}

// RetryQuery wraps a function that returns (pgx.Rows, error) with retry logic.
func RetryQuery(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) (pgx.Rows, error)) (pgx.Rows, error) {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultRetryConfig()
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		rows, err := fn(ctx)
		if err == nil {
			return rows, nil
		}
		lastErr = err
		if !retryableError(err) || attempt >= cfg.MaxAttempts {
			return nil, err
		}
		delay := retryDelay(attempt, cfg)
		slog.Warn("retryable query error, retrying",
			"attempt", attempt,
			"max_attempts", cfg.MaxAttempts,
			"delay", delay,
			"error", err,
		)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
	return nil, lastErr
}

// RetryExec wraps a function that returns (pgconn.CommandTag, error) with retry logic.
func RetryExec(ctx context.Context, cfg RetryConfig, fn func(ctx context.Context) (pgconn.CommandTag, error)) (pgconn.CommandTag, error) {
	if cfg.MaxAttempts <= 0 {
		cfg = DefaultRetryConfig()
	}

	var lastErr error
	for attempt := 1; attempt <= cfg.MaxAttempts; attempt++ {
		tag, err := fn(ctx)
		if err == nil {
			return tag, nil
		}
		lastErr = err
		if !retryableError(err) || attempt >= cfg.MaxAttempts {
			return pgconn.CommandTag{}, err
		}
		delay := retryDelay(attempt, cfg)
		slog.Warn("retryable exec error, retrying",
			"attempt", attempt,
			"max_attempts", cfg.MaxAttempts,
			"delay", delay,
			"error", err,
		)
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return pgconn.CommandTag{}, ctx.Err()
		}
	}
	return pgconn.CommandTag{}, lastErr
}
