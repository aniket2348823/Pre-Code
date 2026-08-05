package database

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vigilagent/vigilagent/internal/config"
)

// Content from indexadviser.go

// IndexRecommendation represents a single index recommendation.
type IndexRecommendation struct {
	Type        string // "unused_index" or "missing_index"
	Schema      string
	Table       string
	Index       string
	Reason      string
	Impact      string // "high", "medium", "low"
	SizeBytes   int64  // for unused indexes
	SeqScans    int64  // for missing indexes
	SeqTuples   int64  // for missing indexes
	QuerySample string // example slow query
	CreatedAt   time.Time
}

// IndexAdvisor provides automatic index recommendations.
type IndexAdvisor struct {
	pool *pgxpool.Pool
}

// NewIndexAdvisor creates a new IndexAdvisor.
func NewIndexAdvisor(pool *pgxpool.Pool) *IndexAdvisor {
	return &IndexAdvisor{pool: pool}
}

// GetRecommendations returns index recommendations.
// It checks for:
// 1. Unused indexes (never scanned, taking up space)
// 2. Tables with high sequential scan ratios (missing indexes)
func (a *IndexAdvisor) GetRecommendations(ctx context.Context) ([]IndexRecommendation, error) {
	if a == nil || a.pool == nil {
		return nil, fmt.Errorf("index advisor: nil pool")
	}
	var recommendations []IndexRecommendation

	unused, err := a.findUnusedIndexes(ctx)
	if err != nil {
		slog.Warn("index adviser: failed to find unused indexes", "error", err)
	} else {
		recommendations = append(recommendations, unused...)
	}

	missing, err := a.findMissingIndexes(ctx)
	if err != nil {
		slog.Warn("index adviser: failed to find missing indexes", "error", err)
	} else {
		recommendations = append(recommendations, missing...)
	}

	return recommendations, nil
}

// findUnusedIndexes queries pg_stat_user_indexes for indexes never scanned.
func (a *IndexAdvisor) findUnusedIndexes(ctx context.Context) ([]IndexRecommendation, error) {
	query := `
		SELECT 
			schemaname,
			relname AS table_name,
			indexrelname AS index_name,
			pg_size_pretty(pg_relation_size(indexrelid)) AS index_size,
			pg_relation_size(indexrelid) AS index_size_bytes,
			idx_scan,
			idx_tup_read,
			idx_tup_fetch
		FROM pg_stat_user_indexes
		WHERE idx_scan = 0
		AND schemaname = 'public'
		AND pg_relation_size(indexrelid) > 1024 * 1024  -- only indexes > 1MB
		ORDER BY pg_relation_size(indexrelid) DESC
	`

	rows, err := a.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query unused indexes: %w", err)
	}
	defer rows.Close()

	var results []IndexRecommendation
	for rows.Next() {
		var schema, table, index, sizePretty string
		var sizeBytes, idxScan, idxTupRead, idxTupFetch int64
		if err := rows.Scan(&schema, &table, &index, &sizePretty, &sizeBytes, &idxScan, &idxTupRead, &idxTupFetch); err != nil {
			slog.Warn("index adviser: scan unused index row failed", "error", err)
			continue
		}
		results = append(results, IndexRecommendation{
			Type:      "unused_index",
			Schema:    schema,
			Table:     table,
			Index:     index,
			Reason:    fmt.Sprintf("Index never scanned (idx_scan=0), size: %s", sizePretty),
			Impact:    impactFromSize(sizeBytes),
			SizeBytes: sizeBytes,
			CreatedAt: time.Now(),
		})
	}
	return results, rows.Err()
}

// findMissingIndexes queries pg_stat_user_tables for tables with high sequential scan ratios.
func (a *IndexAdvisor) findMissingIndexes(ctx context.Context) ([]IndexRecommendation, error) {
	query := `
		SELECT 
			schemaname,
			relname AS table_name,
			seq_scan,
			seq_tup_read,
			idx_scan,
			n_tup_ins + n_tup_upd + n_tup_del AS total_writes,
			pg_size_pretty(pg_total_relation_size(relid)) AS table_size,
			pg_total_relation_size(relid) AS table_size_bytes
		FROM pg_stat_user_tables
		WHERE schemaname = 'public'
		AND seq_scan > 100  -- at least 100 seq scans
		AND (seq_scan::float / NULLIF(seq_scan + idx_scan, 0)) > 0.5  -- seq scan ratio > 50%
		ORDER BY seq_tup_read DESC
		LIMIT 20
	`

	rows, err := a.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("query missing indexes: %w", err)
	}
	defer rows.Close()

	var results []IndexRecommendation
	for rows.Next() {
		var schema, table, sizePretty string
		var seqScan, seqTupRead, idxScan, totalWrites, tableSizeBytes int64
		if err := rows.Scan(&schema, &table, &seqScan, &seqTupRead, &idxScan, &totalWrites, &sizePretty, &tableSizeBytes); err != nil {
			slog.Warn("index adviser: scan missing index row failed", "error", err)
			continue
		}
		seqRatio := float64(seqScan) / float64(seqScan+idxScan)
		impact := "medium"
		if seqRatio > 0.9 {
			impact = "high"
		} else if seqRatio < 0.7 {
			impact = "low"
		}
		results = append(results, IndexRecommendation{
			Type:      "missing_index",
			Schema:    schema,
			Table:     table,
			Reason:    fmt.Sprintf("High sequential scan ratio: %.1f%% (seq_scans=%d, idx_scans=%d)", seqRatio*100, seqScan, idxScan),
			Impact:    impact,
			SeqScans:  seqScan,
			SeqTuples: seqTupRead,
			CreatedAt: time.Now(),
		})
	}
	return results, rows.Err()
}

// impactFromSize returns impact level based on index size.
func impactFromSize(bytes int64) string {
	switch {
	case bytes > 100*1024*1024: // > 100MB
		return "high"
	case bytes > 10*1024*1024: // > 10MB
		return "medium"
	default:
		return "low"
	}
}

// RunPeriodicCheck runs index analysis periodically and logs recommendations.
func (a *IndexAdvisor) RunPeriodicCheck(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			recs, err := a.GetRecommendations(ctx)
			if err != nil {
				slog.Warn("index adviser periodic check failed", "error", err)
				continue
			}
			if len(recs) > 0 {
				slog.Info("index adviser recommendations", "count", len(recs))
				for _, r := range recs {
					slog.Info("index recommendation",
						"type", r.Type,
						"table", r.Table,
						"index", r.Index,
						"reason", r.Reason,
						"impact", r.Impact,
					)
				}
			}
		}
	}
}

// GetIndexUsageStats returns detailed index usage statistics for a table.
func (a *IndexAdvisor) GetIndexUsageStats(ctx context.Context, tableName string) ([]map[string]any, error) {
	query := `
		SELECT 
			indexrelname AS index_name,
			idx_scan,
			idx_tup_read,
			idx_tup_fetch,
			pg_size_pretty(pg_relation_size(indexrelid)) AS index_size,
			pg_relation_size(indexrelid) AS index_size_bytes
		FROM pg_stat_user_indexes
		WHERE relname = $1 AND schemaname = 'public'
		ORDER BY idx_scan DESC
	`

	rows, err := a.pool.Query(ctx, query, tableName)
	if err != nil {
		return nil, fmt.Errorf("query index usage: %w", err)
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var indexName string
		var idxScan, idxTupRead, idxTupFetch, sizeBytes int64
		var sizePretty string
		if err := rows.Scan(&indexName, &idxScan, &idxTupRead, &idxTupFetch, &sizePretty, &sizeBytes); err != nil {
			continue
		}
		results = append(results, map[string]any{
			"index_name":       indexName,
			"idx_scan":         idxScan,
			"idx_tup_read":     idxTupRead,
			"idx_tup_fetch":    idxTupFetch,
			"index_size":       sizePretty,
			"index_size_bytes": sizeBytes,
		})
	}
	return results, rows.Err()
}

// GetTableScanStats returns sequential vs index scan statistics for a table.
func (a *IndexAdvisor) GetTableScanStats(ctx context.Context, tableName string) (map[string]any, error) {
	query := `
		SELECT 
			relname AS table_name,
			seq_scan,
			seq_tup_read,
			idx_scan,
			idx_tup_fetch,
			n_tup_ins,
			n_tup_upd,
			n_tup_del,
			n_live_tup,
			n_dead_tup,
			pg_size_pretty(pg_total_relation_size(relid)) AS table_size
		FROM pg_stat_user_tables
		WHERE relname = $1 AND schemaname = 'public'
	`

	var tableNameDB string
	var seqScan, seqTupRead, idxScan, idxTupFetch, nIns, nUpd, nDel, nLive, nDead int64
	var tableSize string

	err := a.pool.QueryRow(ctx, query, tableName).Scan(
		&tableNameDB, &seqScan, &seqTupRead, &idxScan, &idxTupFetch,
		&nIns, &nUpd, &nDel, &nLive, &nDead, &tableSize,
	)
	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("table %s not found", tableName)
	}
	if err != nil {
		return nil, fmt.Errorf("query table stats: %w", err)
	}

	totalScans := seqScan + idxScan
	var seqRatio float64
	if totalScans > 0 {
		seqRatio = float64(seqScan) / float64(totalScans)
	}

	return map[string]any{
		"table_name":     tableNameDB,
		"seq_scan":       seqScan,
		"seq_tup_read":   seqTupRead,
		"idx_scan":       idxScan,
		"idx_tup_fetch":  idxTupFetch,
		"n_tup_ins":      nIns,
		"n_tup_upd":      nUpd,
		"n_tup_del":      nDel,
		"n_live_tup":     nLive,
		"n_dead_tup":     nDead,
		"table_size":     tableSize,
		"seq_scan_ratio": seqRatio,
		"total_scans":    totalScans,
	}, nil
}

// Content from poolmetrics.go

// PoolStatsSnapshot holds a point-in-time copy of connection pool metrics.
type PoolStatsSnapshot struct {
	TotalConns              int32
	AcquiredConns           int32
	MaxConns                int32
	ConstructingConns       int32
	EmptyAcquireCount       int64
	AcquireDurationMs       int64
	MaxIdleDestroyCount     int64
	MaxLifetimeDestroyCount int64
	Utilization             float64
}

// CollectPoolStats reads from pgxpool.Stats() and returns a snapshot.
// It also updates the Prometheus gauges as a side effect (reusing the
// gauges declared in postgres.go).
func CollectPoolStats(pool *pgxpool.Pool) *PoolStatsSnapshot {
	if pool == nil {
		return &PoolStatsSnapshot{}
	}
	stat := pool.Stat()
	snapshot := statsFromStat(stat)
	updatePrometheusGauges(snapshot)
	return snapshot
}

// statsFromStat extracts a snapshot from a pgxpool.Stat.
func statsFromStat(stat *pgxpool.Stat) *PoolStatsSnapshot {
	inUse := stat.AcquiredConns()
	total := stat.TotalConns()
	maxConns := stat.MaxConns()

	var utilization float64
	if maxConns > 0 {
		utilization = float64(inUse) / float64(maxConns)
	}

	return &PoolStatsSnapshot{
		TotalConns:              total,
		AcquiredConns:           inUse,
		MaxConns:                maxConns,
		ConstructingConns:       stat.ConstructingConns(),
		EmptyAcquireCount:       stat.EmptyAcquireCount(),
		AcquireDurationMs:       stat.AcquireDuration().Milliseconds(),
		MaxIdleDestroyCount:     stat.MaxIdleDestroyCount(),
		MaxLifetimeDestroyCount: stat.MaxLifetimeDestroyCount(),
		Utilization:             utilization,
	}
}

// ReadPoolStats returns a point-in-time snapshot of the connection pool metrics.
// Returns an empty snapshot when the pool is nil.
func (p *Postgres) ReadPoolStats() *PoolStatsSnapshot {
	return CollectPoolStats(p.Pool)
}

// updatePrometheusGauges pushes a snapshot into the Prometheus gauges declared
// in postgres.go.
func updatePrometheusGauges(s *PoolStatsSnapshot) {
	idle := s.TotalConns - s.AcquiredConns
	if idle < 0 {
		idle = 0
	}

	poolOpenConnections.Set(float64(s.TotalConns))
	poolInUse.Set(float64(s.AcquiredConns))
	poolIdle.Set(float64(idle))
	poolWaitCount.Set(float64(s.EmptyAcquireCount))
	poolWaitDuration.Set(float64(s.AcquireDurationMs) / 1000.0)
	poolMaxIdleClosed.Set(float64(s.MaxIdleDestroyCount))
	poolMaxLifetimeClosed.Set(float64(s.MaxLifetimeDestroyCount))
	poolUtilization.Set(s.Utilization)

	if s.Utilization < 0.8 {
		poolHealthUp.Set(1)
	} else {
		poolHealthUp.Set(0)
	}
}

// Content from slowquery.go

// SlowQueryConfig holds slow query logging configuration.
type SlowQueryConfig struct {
	Enabled        bool          // enable slow query logging
	Threshold      time.Duration // log queries slower than this
	SampleRate     float64       // sample rate for logging (0.0-1.0)
	MaxQueryLength int           // truncate queries longer than this
}

// DefaultSlowQueryConfig returns default slow query configuration.
func DefaultSlowQueryConfig() SlowQueryConfig {
	return SlowQueryConfig{
		Enabled:        true,
		Threshold:      1 * time.Second,
		SampleRate:     1.0,
		MaxQueryLength: 2000,
	}
}

// SlowQueryLogger wraps a pgxpool.Pool to log slow queries.
type SlowQueryLogger struct {
	pool   *pgxpool.Pool
	config SlowQueryConfig
}

// NewSlowQueryLogger creates a new SlowQueryLogger.
// If config.Threshold is 0 (and the logger is enabled), it defaults to 1s.
func NewSlowQueryLogger(pool *pgxpool.Pool, config SlowQueryConfig) *SlowQueryLogger {
	if config.Threshold <= 0 {
		config.Threshold = 1 * time.Second
	}
	return &SlowQueryLogger{
		pool:   pool,
		config: config,
	}
}

// Query executes a query and logs if it exceeds the threshold.
func (s *SlowQueryLogger) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if !s.config.Enabled {
		return s.pool.Query(ctx, sql, args...)
	}
	start := time.Now()
	rows, err := s.pool.Query(ctx, sql, args...)
	s.logIfSlow(ctx, sql, args, start, err)
	return rows, err
}

// QueryRow executes a query row and logs if it exceeds the threshold.
func (s *SlowQueryLogger) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if !s.config.Enabled {
		return s.pool.QueryRow(ctx, sql, args...)
	}
	start := time.Now()
	row := s.pool.QueryRow(ctx, sql, args...)
	// Note: QueryRow doesn't return error immediately, so we can't log here
	// The actual execution happens on Scan()
	return &loggedRow{row: row, logger: s, sql: sql, args: args, start: start}
}

// Exec executes a command and logs if it exceeds the threshold.
func (s *SlowQueryLogger) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if !s.config.Enabled {
		return s.pool.Exec(ctx, sql, args...)
	}
	start := time.Now()
	tag, err := s.pool.Exec(ctx, sql, args...)
	s.logIfSlow(ctx, sql, args, start, err)
	return tag, err
}

// logIfSlow logs the query if it exceeds the threshold.
func (s *SlowQueryLogger) logIfSlow(ctx context.Context, sql string, args []any, start time.Time, err error) {
	duration := time.Since(start)
	if duration < s.config.Threshold {
		return
	}

	// Apply sample rate
	if s.config.SampleRate < 1.0 {
		// Simple deterministic sampling based on query hash
		hash := hashString(sql)
		if float64(hash%10000)/10000.0 > s.config.SampleRate {
			return
		}
	}

	query := truncateQuery(sql, s.config.MaxQueryLength)
	argsStr := formatArgs(args)

	level := slog.LevelWarn
	if duration > 5*time.Second {
		level = slog.LevelError
	}

	slog.Log(ctx, level, "slow query detected",
		"duration_ms", duration.Milliseconds(),
		"threshold_ms", s.config.Threshold.Milliseconds(),
		"query", query,
		"args", argsStr,
		"error", err,
	)
}

// loggedRow wraps pgx.Row to log on Scan.
type loggedRow struct {
	row    pgx.Row
	logger *SlowQueryLogger
	sql    string
	args   []any
	start  time.Time
}

func (r *loggedRow) Scan(dest ...any) error {
	err := r.row.Scan(dest...)
	r.logger.logIfSlow(context.Background(), r.sql, r.args, r.start, err)
	return err
}

// SetStatementTimeout sets the statement_timeout for the current session.
func (s *SlowQueryLogger) SetStatementTimeout(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}
	timeoutMs := timeout.Milliseconds()
	_, err := s.pool.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs))
	return err
}

// SetGlobalStatementTimeout sets the statement_timeout globally (requires superuser).
func (s *SlowQueryLogger) SetGlobalStatementTimeout(ctx context.Context, timeout time.Duration) error {
	if timeout <= 0 {
		return nil
	}
	timeoutMs := timeout.Milliseconds()
	_, err := s.pool.Exec(ctx, fmt.Sprintf("ALTER DATABASE %s SET statement_timeout = %d", currentDatabase(ctx, s.pool), timeoutMs))
	return err
}

// GetSlowQueriesFromPgStatStatements returns slow queries from pg_stat_statements (if extension exists).
func (s *SlowQueryLogger) GetSlowQueriesFromPgStatStatements(ctx context.Context, limit int) ([]SlowQueryRecord, error) {
	query := `
		SELECT 
			query,
			calls,
			total_exec_time,
			mean_exec_time,
			max_exec_time,
			rows,
			100.0 * shared_blks_hit / nullif(shared_blks_hit + shared_blks_read, 0) AS hit_percent
		FROM pg_stat_statements
		WHERE mean_exec_time > $1
		ORDER BY mean_exec_time DESC
		LIMIT $2
	`

	thresholdMs := s.config.Threshold.Milliseconds()
	rows, err := s.pool.Query(ctx, query, thresholdMs, limit)
	if err != nil {
		// pg_stat_statements might not be installed
		if strings.Contains(err.Error(), "relation") && strings.Contains(err.Error(), "pg_stat_statements") {
			return nil, ErrPgStatStatementsNotInstalled
		}
		return nil, fmt.Errorf("query pg_stat_statements: %w", err)
	}
	defer rows.Close()

	var results []SlowQueryRecord
	for rows.Next() {
		var r SlowQueryRecord
		if err := rows.Scan(&r.Query, &r.Calls, &r.TotalExecTime, &r.MeanExecTime, &r.MaxExecTime, &r.Rows, &r.HitPercent); err != nil {
			continue
		}
		r.Query = truncateQuery(r.Query, s.config.MaxQueryLength)
		results = append(results, r)
	}
	return results, rows.Err()
}

// SlowQueryRecord represents a slow query from pg_stat_statements.
type SlowQueryRecord struct {
	Query         string
	Calls         int64
	TotalExecTime float64
	MeanExecTime  float64
	MaxExecTime   float64
	Rows          int64
	HitPercent    float64
}

// ErrPgStatStatementsNotInstalled is returned when pg_stat_statements extension is not available.
var ErrPgStatStatementsNotInstalled = fmt.Errorf("pg_stat_statements extension not installed")

// currentDatabase returns the current database name.
func currentDatabase(ctx context.Context, pool *pgxpool.Pool) string {
	var db string
	_ = pool.QueryRow(ctx, "SELECT current_database()").Scan(&db)
	return db
}

// hashString returns a simple hash of a string for sampling.
func hashString(s string) uint32 {
	var h uint32
	for i := 0; i < len(s); i++ {
		h = h*31 + uint32(s[i])
	}
	return h
}

// truncateQuery truncates a query to maxLength.
func truncateQuery(query string, maxLength int) string {
	if len(query) <= maxLength {
		return query
	}
	return query[:maxLength] + "... [truncated]"
}

// formatArgs formats query arguments for logging.
func formatArgs(args []any) string {
	if len(args) == 0 {
		return ""
	}
	var parts []string
	for i, arg := range args {
		if i > 5 {
			parts = append(parts, fmt.Sprintf("... (%d more)", len(args)-i))
			break
		}
		parts = append(parts, fmt.Sprintf("$%d=%v", i+1, arg))
	}
	return strings.Join(parts, ", ")
}

// Pool returns the underlying pool for direct access.
func (s *SlowQueryLogger) Pool() *pgxpool.Pool {
	return s.pool
}

// Threshold returns the configured slow query threshold.
func (s *SlowQueryLogger) Threshold() time.Duration {
	return s.config.Threshold
}

// Enabled returns whether slow query logging is enabled.
func (s *SlowQueryLogger) Enabled() bool {
	return s.config.Enabled
}

// LogIfSlow logs the query if it exceeds the threshold.
func (s *SlowQueryLogger) LogIfSlow(sql string, duration time.Duration) {
	s.logIfSlow(context.Background(), sql, nil, time.Now().Add(-duration), nil)
}

// ApplyStatementTimeout sets the statement_timeout for the current session.
func ApplyStatementTimeout(ctx context.Context, pool *pgxpool.Pool, cfg *config.DatabaseConfig) error {
	if pool == nil || cfg == nil || cfg.StatementTimeout <= 0 {
		return nil
	}
	timeoutMs := cfg.StatementTimeout.Milliseconds()
	_, err := pool.Exec(ctx, fmt.Sprintf("SET LOCAL statement_timeout = %d", timeoutMs))
	return err
}
