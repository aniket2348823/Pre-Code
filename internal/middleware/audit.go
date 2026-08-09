package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/database"
)

// AuditEvent represents a security-relevant event to be logged.
type AuditEvent struct {
	UserID     string
	Action     string
	Resource   string
	ResourceID string
	IPAddress  string
	UserAgent  string
	Status     string
	Details    string
}

// AuditEventLogger is the interface for logging audit events.
type AuditEventLogger interface {
	Log(ctx context.Context, event AuditEvent)
}

// AuditLogger logs security events to the database.
type AuditLogger struct {
	pool *database.Conn
}

// NewAuditLogger creates a new audit logger.
func NewAuditLogger(pool *database.Conn) *AuditLogger {
	return &AuditLogger{pool: pool}
}

// Log records an audit event asynchronously.
func (a *AuditLogger) Log(ctx context.Context, event AuditEvent) {
	go func() {
		// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
		bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		_, err := a.pool.Exec(bgCtx, `
			INSERT INTO audit_logs (user_id, action, resource, resource_id, ip_address, user_agent, status, details, created_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NOW())
		`, event.UserID, event.Action, event.Resource, event.ResourceID,
			event.IPAddress, event.UserAgent, event.Status, event.Details)
		if err != nil {
			slog.Error("audit: failed to log event", "error", err, "action", event.Action, "user_id", event.UserID)
		}
	}()
}

// AuditMiddleware logs all state-changing requests.
func AuditMiddleware(logger AuditEventLogger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Only audit state-changing methods
			if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
				next.ServeHTTP(w, r)
				return
			}

			claims, _ := auth.ClaimsFromContext(r.Context())
			userID := ""
			if claims != nil {
				userID = claims.UserID
			}

			// Wrap response writer to capture status
			rec := &auditStatusRecorder{ResponseWriter: w, statusCode: 200}
			next.ServeHTTP(rec, r)

			status := "success"
			if rec.statusCode >= 400 {
				status = "error"
			}

			logger.Log(r.Context(), AuditEvent{
				UserID:    userID,
				Action:    r.Method + " " + r.URL.Path,
				Resource:  extractResource(r.URL.Path),
				IPAddress: r.RemoteAddr,
				UserAgent: r.UserAgent(),
				Status:    status,
			})
		})
	}
}

// LogAuthEvent logs authentication-specific events.
func (a *AuditLogger) LogAuthEvent(ctx context.Context, userID, action, ipAddr, details string) {
	a.Log(ctx, AuditEvent{
		UserID:    userID,
		Action:    action,
		Resource:  "auth",
		IPAddress: ipAddr,
		Status:    "success",
		Details:   details,
	})
}

// LogAPIKeyEvent logs API key lifecycle events.
func (a *AuditLogger) LogAPIKeyEvent(ctx context.Context, userID, action, keyID, ipAddr string) {
	a.Log(ctx, AuditEvent{
		UserID:     userID,
		Action:     action,
		Resource:   "api_key",
		ResourceID: keyID,
		IPAddress:  ipAddr,
		Status:     "success",
	})
}

// LogPermissionDenied logs access denial events.
func (a *AuditLogger) LogPermissionDenied(ctx context.Context, userID, action, ipAddr string) {
	a.Log(ctx, AuditEvent{
		UserID:    userID,
		Action:    action,
		Resource:  "permission",
		IPAddress: ipAddr,
		Status:    "denied",
	})
}

type auditStatusRecorder struct {
	http.ResponseWriter
	statusCode int
}

func (r *auditStatusRecorder) WriteHeader(code int) {
	r.statusCode = code
	r.ResponseWriter.WriteHeader(code)
}

// extractResource pulls the resource name from a URL path using strings.Split.
func extractResource(path string) string {
	// /api/v1/users → users
	// /api/v1/projects/123/agents → agents
	parts := strings.Split(path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" && parts[i] != "api" && parts[i] != "v1" {
			return parts[i]
		}
	}
	return "unknown"
}

// AuditCleanup handles periodic cleanup and compression of old audit logs.
type AuditCleanup struct {
	pool      *database.Conn
	config    config.AuditConfig
	stop      chan struct{}
	wg        sync.WaitGroup
	startOnce sync.Once
}

// NewAuditCleanup creates a new audit cleanup manager.
func NewAuditCleanup(pool *database.Conn, cfg config.AuditConfig) *AuditCleanup {
	if cfg.CleanupInterval <= 0 {
		cfg.CleanupInterval = time.Hour
	}
	return &AuditCleanup{
		pool:   pool,
		config: cfg,
		stop:   make(chan struct{}),
	}
}

// Start begins the periodic cleanup job.
func (ac *AuditCleanup) Start() {
	ac.startOnce.Do(func() {
		ac.wg.Add(1)
		go func() {
			defer ac.wg.Done()
			ticker := time.NewTicker(ac.config.CleanupInterval)
			defer ticker.Stop()

			for {
				select {
				case <-ticker.C:
					ac.runCleanup()
				case <-ac.stop:
					return
				}
			}
		}()
		slog.Info("audit cleanup started",
			"interval", ac.config.CleanupInterval,
			"retention_days", ac.config.RetentionDays,
		)
	})
}

// Stop signals the cleanup goroutine to stop and waits for it.
// Safe to call multiple times.
func (ac *AuditCleanup) Stop() {
	if ac.stop != nil {
		select {
		case <-ac.stop:
			// already closed
		default:
			close(ac.stop)
		}
	}
	ac.wg.Wait()
}

func (ac *AuditCleanup) runCleanup() {
	// #nosec context_leak: background context for long-running startup/worker/lifecycle code - no request context exists here
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	ac.compressOldEvents(ctx)
	ac.deleteExpiredEvents(ctx)
	ac.monitorStorage(ctx)
}

// compressOldEvents compresses audit events older than CompressAfterDays
// by truncating the details field and marking them as compressed.
func (ac *AuditCleanup) compressOldEvents(ctx context.Context) {
	cutoff := time.Now().AddDate(0, 0, -ac.config.CompressAfterDays)
	result, err := ac.pool.Exec(ctx, `
		UPDATE audit_logs
		SET details = '[compressed] ' || LEFT(details, 100),
		    status = status || ':compressed'
		WHERE created_at < $1
		  AND details NOT LIKE '[compressed]%'
		  AND details != ''
	`, cutoff)
	if err != nil {
		slog.Error("audit: failed to compress old events", "error", err)
		return
	}
	rows := result.RowsAffected()
	if rows > 0 {
		slog.Info("audit: compressed old events", "count", rows, "before", cutoff)
	}
}

// deleteExpiredEvents deletes audit events older than RetentionDays.
func (ac *AuditCleanup) deleteExpiredEvents(ctx context.Context) {
	cutoff := time.Now().AddDate(0, 0, -ac.config.RetentionDays)
	result, err := ac.pool.Exec(ctx, `
		DELETE FROM audit_logs
		WHERE created_at < $1
	`, cutoff)
	if err != nil {
		slog.Error("audit: failed to delete expired events", "error", err)
		return
	}
	rows := result.RowsAffected()
	if rows > 0 {
		slog.Info("audit: deleted expired events", "count", rows, "before", cutoff)
	}
}

// AuditStorageMetrics holds storage monitoring data.
type AuditStorageMetrics struct {
	TotalSizeMB   float64
	EventCount    int64
	OldestEventAt time.Time
}

// monitorStorage checks total storage size and logs warnings if thresholds are exceeded.
func (ac *AuditCleanup) monitorStorage(ctx context.Context) AuditStorageMetrics {
	var metrics AuditStorageMetrics
	err := ac.pool.QueryRow(ctx, `
		SELECT
			COALESCE(pg_column_size(data)::bigint, 0) as total_bytes,
			COUNT(*) as event_count,
			COALESCE(MIN(created_at), NOW()) as oldest_event
		FROM audit_logs data
	`).Scan(&metrics.TotalSizeMB, &metrics.EventCount, &metrics.OldestEventAt)
	if err != nil {
		slog.Error("audit: failed to get storage metrics", "error", err)
		return metrics
	}
	metrics.TotalSizeMB = metrics.TotalSizeMB / (1024 * 1024)

	if ac.config.AlertThresholdMB > 0 && metrics.TotalSizeMB > float64(ac.config.AlertThresholdMB) {
		slog.Warn("audit: storage threshold exceeded",
			"size_mb", metrics.TotalSizeMB,
			"threshold_mb", ac.config.AlertThresholdMB,
		)
	}
	if ac.config.MaxStorageMB > 0 && metrics.TotalSizeMB > float64(ac.config.MaxStorageMB) {
		slog.Error("audit: storage max capacity exceeded",
			"size_mb", metrics.TotalSizeMB,
			"max_mb", ac.config.MaxStorageMB,
		)
	}
	return metrics
}
