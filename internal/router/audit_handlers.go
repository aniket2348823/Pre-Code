package router

import (
	"net/http"
	"strconv"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// auditLogEntry represents a single audit log record for the API response.
type auditLogEntry struct {
	ID         int64     `json:"id"`
	UserID     string    `json:"user_id"`
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resource_id,omitempty"`
	IPAddress  string    `json:"ip_address"`
	UserAgent  string    `json:"user_agent,omitempty"`
	Status     string    `json:"status"`
	Details    string    `json:"details,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// listAuditLogsHandler returns audit logs for the authenticated user (admin sees all).
func (r *Router) listAuditLogsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	limit := 50
	if l := req.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 200 {
			limit = parsed
		}
	}

	offset := 0
	if o := req.URL.Query().Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	userFilter := req.URL.Query().Get("user_id")
	actionFilter := req.URL.Query().Get("action")

	// Admins can see all logs; regular users see only their own
	query := `
		SELECT id, user_id, action, resource, resource_id, ip_address, user_agent, status, details, created_at
		FROM audit_logs
		WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1

	if claims.Role != "admin" || userFilter != "" {
		if userFilter != "" {
			query += ` AND user_id = $` + strconv.Itoa(argIdx)
			args = append(args, userFilter)
			argIdx++
		} else if claims.Role != "admin" {
			query += ` AND user_id = $` + strconv.Itoa(argIdx)
			args = append(args, claims.UserID)
			argIdx++
		}
	}

	if actionFilter != "" {
		query += ` AND action ILIKE $` + strconv.Itoa(argIdx)
		args = append(args, "%"+actionFilter+"%")
		argIdx++
	}

	query += ` ORDER BY created_at DESC`
	query += ` LIMIT $` + strconv.Itoa(argIdx)
	args = append(args, limit)
	argIdx++
	query += ` OFFSET $` + strconv.Itoa(argIdx)
	args = append(args, offset)

	rows, err := r.db.Conn().Query(req.Context(), query, args...)
	if err != nil {
		response.InternalError(w, "failed to query audit logs")
		return
	}
	defer rows.Close()

	var logs []auditLogEntry
	for rows.Next() {
		var entry auditLogEntry
		if err := rows.Scan(
			&entry.ID, &entry.UserID, &entry.Action, &entry.Resource, &entry.ResourceID,
			&entry.IPAddress, &entry.UserAgent, &entry.Status, &entry.Details, &entry.CreatedAt,
		); err != nil {
			continue
		}
		logs = append(logs, entry)
	}

	if logs == nil {
		logs = []auditLogEntry{}
	}

	// Count total
	countQuery := `SELECT COUNT(*) FROM audit_logs WHERE 1=1`
	countArgs := []interface{}{}
	countIdx := 1
	if claims.Role != "admin" || userFilter != "" {
		if userFilter != "" {
			countQuery += ` AND user_id = $` + strconv.Itoa(countIdx)
			countArgs = append(countArgs, userFilter)
			countIdx++
		} else if claims.Role != "admin" {
			countQuery += ` AND user_id = $` + strconv.Itoa(countIdx)
			countArgs = append(countArgs, claims.UserID)
			countIdx++
		}
	}

	var total int
	_ = r.db.Conn().QueryRow(req.Context(), countQuery, countArgs...).Scan(&total)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"data":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}
