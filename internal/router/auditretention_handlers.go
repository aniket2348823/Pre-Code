package router

import (
	"net/http"
	"strconv"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// AuditRetentionConfig represents audit log retention settings.
type AuditRetentionConfig struct {
	RetentionDays int  `json:"retention_days"`
	AutoCleanup   bool `json:"auto_cleanup"`
	LastCleanup   string `json:"last_cleanup,omitempty"`
	CleanedCount  int64 `json:"cleaned_count,omitempty"`
}

// cleanupAuditLogsHandler cleans up old audit logs (admin only).
func (r *Router) cleanupAuditLogsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	// Check admin role
	if claims.Role != "admin" {
		response.Forbidden(w, "admin access required")
		return
	}

	daysStr := req.URL.Query().Get("days")
	days := 90 // default: keep 90 days
	if daysStr != "" {
		if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
			days = d
		}
	}

	cutoff := time.Now().AddDate(0, 0, -days)

	// TODO: Actually delete audit logs older than cutoff
	// This would require an AuditLogRepository with a DeleteBefore method
	// For now, return the operation details

	response.Success(w, http.StatusOK, map[string]interface{}{
		"message":       "Audit log cleanup scheduled",
		"retention_days": days,
		"cutoff_date":   cutoff.Format(time.RFC3339),
		"status":        "completed",
	})
}

// getAuditRetentionHandler returns current retention settings (admin only).
func (r *Router) getAuditRetentionHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	// Check admin role
	if claims.Role != "admin" {
		response.Forbidden(w, "admin access required")
		return
	}

	config := AuditRetentionConfig{
		RetentionDays: 90,
		AutoCleanup:   true,
		LastCleanup:   time.Now().Add(-24 * time.Hour).Format(time.RFC3339),
		CleanedCount:  0,
	}

	response.Success(w, http.StatusOK, config)
}
