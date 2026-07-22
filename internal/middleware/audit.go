package middleware

import (
	"context"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
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
