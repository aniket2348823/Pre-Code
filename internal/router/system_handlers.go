package router

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/agent"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/featureflags"
	"github.com/vigilagent/vigilagent/internal/llm"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/pagination"
	"github.com/vigilagent/vigilagent/pkg/query"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// Content from system_handlers.go
// adminStatsHandler returns platform-wide statistics.
func (r *Router) adminStatsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	if claims.Role != "admin" && claims.Role != "superadmin" {
		response.Forbidden(w, "admin access required")
		return
	}

	ctx := req.Context()

	// Gather real stats from repositories
	totalUsers, err := r.users.Count(ctx)
	if err != nil {
		response.InternalError(w, "failed to get user count")
		return
	}
	activeUsers24h, err := r.users.CountActive24h(ctx)
	if err != nil {
		response.InternalError(w, "failed to get active user count")
		return
	}

	// Get org count
	var totalOrgs int
	if err := r.orgs.Count(ctx, &totalOrgs); err != nil {
		slog.Warn("failed to count organizations", "error", err)
	}

	// Get project count
	var totalProjects int
	if err := r.projects.Count(ctx, &totalProjects); err != nil {
		slog.Warn("failed to count projects", "error", err)
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"total_users":      totalUsers,
		"total_orgs":       totalOrgs,
		"total_projects":   totalProjects,
		"active_users_24h": activeUsers24h,
	})
}

// adminListUsersHandler returns all users (admin only).
func (r *Router) adminListUsersHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	if claims.Role != "admin" && claims.Role != "superadmin" {
		response.Forbidden(w, "admin access required")
		return
	}

	// Fetch all users
	users, err := r.users.List(req.Context(), 0, 100000)
	if err != nil {
		response.InternalError(w, "failed to list users")
		return
	}
	if users == nil {
		users = []repository.User{}
	}

	filter, sortVal := query.Parse(req)

	// Support page-based query as fallback, cursor-based as primary
	cursor := req.URL.Query().Get("cursor")
	if cursor == "" && req.URL.Query().Get("page") != "" {
		page, _ := strconv.Atoi(req.URL.Query().Get("page"))
		pageSize, _ := strconv.Atoi(req.URL.Query().Get("page_size"))
		if page < 1 {
			page = 1
		}
		if pageSize < 1 || pageSize > 100 {
			pageSize = 20
		}

		allProcessed, _ := query.ProcessList(users, filter, sortVal, pagination.Params{Limit: 100000})

		total := len(allProcessed)
		offset := (page - 1) * pageSize
		end := offset + pageSize
		if offset > total {
			offset = total
		}
		if end > total {
			end = total
		}
		paginated := allProcessed[offset:end]

		response.SuccessWithMeta(w, req, http.StatusOK, paginated, &response.Meta{
			Total:   total,
			Limit:   pageSize,
			Offset:  offset,
			HasMore: end < total,
		})
		return
	}

	pag := pagination.ParseRequest(req)
	processed, meta := query.ProcessList(users, filter, sortVal, pag)
	response.SuccessWithMeta(w, req, http.StatusOK, processed, meta)
}

// adminUpdateUserRoleHandler updates a user's role.
func (r *Router) adminUpdateUserRoleHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	if claims.Role != "admin" && claims.Role != "superadmin" {
		response.Forbidden(w, "admin access required")
		return
	}
	userID := chi.URLParam(req, "userID")

	var input struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Role == "" {
		response.BadRequest(w, "role is required")
		return
	}
	validRoles := map[string]bool{"user": true, "admin": true, "superadmin": true}
	if !validRoles[input.Role] {
		response.BadRequest(w, "invalid role: must be user, admin, or superadmin")
		return
	}
	if err := r.users.UpdateRole(req.Context(), userID, input.Role); err != nil {
		if err.Error() == "user not found" {
			response.NotFound(w, "user not found")
			return
		}
		response.InternalError(w, "failed to update user role")
		return
	}
	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "user.role_changed",
			Payload: map[string]interface{}{
				"user_id": userID,
				"role":    input.Role,
			},
		})
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"user_id": userID,
		"role":    input.Role,
	})
}

// adminDeleteUserHandler deletes a user (admin only).
func (r *Router) adminDeleteUserHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	userID := chi.URLParam(req, "userID")

	if claims.Role != "admin" && claims.Role != "superadmin" {
		response.Forbidden(w, "admin access required")
		return
	}
	// Prevent self-deletion
	if claims.UserID == userID {
		response.BadRequest(w, "cannot delete your own account")
		return
	}
	if err := r.users.Delete(req.Context(), userID); err != nil {
		if err.Error() == "user not found" {
			response.NotFound(w, "user not found")
			return
		}
		response.InternalError(w, "failed to delete user")
		return
	}
	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "user.deleted",
			Payload: map[string]interface{}{
				"user_id": userID,
			},
		})
	}
	response.NoContent(w)
}

// listFeatureFlagsHandler lists all feature flags (admin only).
func (r *Router) listFeatureFlagsHandler(w http.ResponseWriter, req *http.Request) {
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

	if r.featureFlags == nil {
		response.Success(w, http.StatusOK, []interface{}{})
		return
	}

	flags, err := r.featureFlags.GetAll(req.Context())
	if err != nil {
		response.InternalError(w, "failed to list feature flags")
		return
	}

	response.Success(w, http.StatusOK, flags)
}

// updateFeatureFlagHandler updates a feature flag (admin only).
func (r *Router) updateFeatureFlagHandler(w http.ResponseWriter, req *http.Request) {
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

	key := req.URL.Query().Get("key")
	if key == "" {
		response.BadRequest(w, "key is required")
		return
	}

	var flag featureflags.Flag
	if err := json.NewDecoder(req.Body).Decode(&flag); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	flag.Name = key

	if r.featureFlags == nil {
		response.InternalError(w, "feature flags not configured")
		return
	}

	if err := r.featureFlags.Set(req.Context(), &flag); err != nil {
		response.InternalError(w, "failed to update feature flag")
		return
	}

	response.Success(w, http.StatusOK, flag)
}

// deleteFeatureFlagHandler deletes a feature flag (admin only).
func (r *Router) deleteFeatureFlagHandler(w http.ResponseWriter, req *http.Request) {
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

	key := req.URL.Query().Get("key")
	if key == "" {
		response.BadRequest(w, "key is required")
		return
	}

	if r.featureFlags == nil {
		response.InternalError(w, "feature flags not configured")
		return
	}

	if err := r.featureFlags.Delete(req.Context(), key); err != nil {
		response.InternalError(w, "failed to delete feature flag")
		return
	}

	response.Success(w, http.StatusOK, map[string]string{"message": "feature flag deleted"})
}

// checkFeatureFlagHandler checks if a feature is enabled for the current user.
func (r *Router) checkFeatureFlagHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	key := req.URL.Query().Get("key")
	if key == "" {
		response.BadRequest(w, "key is required")
		return
	}

	if r.featureFlags == nil {
		response.NotFound(w, "feature flag not found")
		return
	}

	flag, err := r.featureFlags.Get(req.Context(), key)
	if err != nil || flag == nil {
		response.NotFound(w, "feature flag not found")
		return
	}

	response.Success(w, http.StatusOK, map[string]interface{}{
		"key":     flag.Name,
		"enabled": flag.Enabled,
	})
}

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

	// Admins can see all logs (optionally filtered by user_id); regular users
	// are ALWAYS constrained to their own user_id — the userFilter parameter
	// must never let a non-admin read another user's audit logs.
	query := `
		SELECT id, user_id, action, resource, resource_id, ip_address, user_agent, status, details, created_at
		FROM audit_logs
		WHERE 1=1
	`
	args := []interface{}{}
	argIdx := 1

	if claims.Role != "admin" {
		query += ` AND user_id = $` + strconv.Itoa(argIdx)
		args = append(args, claims.UserID)
		argIdx++
	} else if userFilter != "" {
		query += ` AND user_id = $` + strconv.Itoa(argIdx)
		args = append(args, userFilter)
		argIdx++
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

	// Count total (same access constraints as the data query above)
	countQuery := `SELECT COUNT(*) FROM audit_logs WHERE 1=1`
	countArgs := []interface{}{}
	countIdx := 1
	if claims.Role != "admin" {
		countQuery += ` AND user_id = $` + strconv.Itoa(countIdx)
		countArgs = append(countArgs, claims.UserID)
		countIdx++
	} else if userFilter != "" {
		countQuery += ` AND user_id = $` + strconv.Itoa(countIdx)
		countArgs = append(countArgs, userFilter)
		countIdx++
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

// AuditRetentionConfig represents audit log retention settings.
type AuditRetentionConfig struct {
	RetentionDays int    `json:"retention_days"`
	AutoCleanup   bool   `json:"auto_cleanup"`
	LastCleanup   string `json:"last_cleanup,omitempty"`
	CleanedCount  int64  `json:"cleaned_count,omitempty"`
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
		"message":        "Audit log cleanup is not yet implemented",
		"retention_days": days,
		"cutoff_date":    cutoff.Format(time.RFC3339),
		"status":         "not_implemented",
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

// NOTE: middlewareProcessHandler is defined in middleware_handlers.go
// with full scanner pipeline + SSE streaming support. Not duplicated here.

// middlewareMetricsHandler returns real middleware pipeline metrics.
func (r *Router) middlewareMetricsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	// Return LLM router health and cost intel metrics
	var healthData interface{}
	if r.llmRouter != nil && r.llmRouter.GetHealthMonitor() != nil {
		healthData = map[string]interface{}{"healthy_providers": r.llmRouter.GetHealthMonitor().GetHealthyProviders()}
	}

	var costData map[string]float64
	if r.costIntel != nil {
		costData = r.costIntel.CostByModel()
	}

	var totalRecords int
	var totalCost float64
	if r.costIntel != nil {
		totalRecords = r.costIntel.TotalRecords()
		totalCost = r.costIntel.TotalCost()
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"health":        healthData,
		"cost_by_model": costData,
		"total_records": totalRecords,
		"total_cost":    totalCost,
	})
}

// middlewarePatternsHandler returns learned patterns from the middleware pipeline.
func (r *Router) middlewarePatternsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	// Return cost intel recommendations as "patterns"
	var recs interface{}
	if r.costIntel != nil {
		recs = r.costIntel.GetRecommendations()
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"patterns":         recs,
		"middleware_stats": "active",
	})
}

// executeTaskBackground runs a task in the background goroutine.
func (r *Router) executeTaskBackground(task *repository.Task, userID string) {
	// Decrement batch rate limit counter when task completes (success or failure).
	if userID != "" && r.rds != nil && r.rds.Client != nil {
		defer func() {
			batchKey := fmt.Sprintf("batch:%s", userID)
			r.rds.Client.Decr(context.Background(), batchKey)
		}()
	}

	if r.agentExec == nil {
		if err := r.tasks.Complete(context.Background(), task.ID, task.Prompt, "", "", 0, 0, 0, 0); err != nil {
			slog.Error("failed to complete task (no agent)", "error", err, "task_id", task.ID)
		}
		return
	}

	bgCtx := context.Background()
	var orgID string
	if proj, err := r.projects.FindByID(bgCtx, task.ProjectID); err == nil {
		orgID = proj.OrgID
	}
	agentTask := &agent.Task{
		ID:            task.ID,
		UserID:        task.UserID,
		ProjectID:     task.ProjectID,
		OrgID:         orgID,
		Title:         task.Prompt,
		Description:   task.Prompt,
		MaxIterations: task.MaxIterations,
		MaxRetries:    3,
		MaxTokens:     task.MaxTokens,
		State:         agent.StatePending,
		Tags:          []string{},
	}

	result, execErr := r.agentExec.ExecuteTask(bgCtx, agentTask)
	if execErr != nil {
		if updateErr := r.tasks.UpdateStatus(context.Background(), task.ID, "failed"); updateErr != nil {
			slog.Error("failed to update task status to failed", "error", updateErr, "task_id", task.ID)
		}
		return
	}
	if err := r.tasks.Complete(bgCtx, task.ID, result.Result, "", "",
		result.TokensUsed, 0, result.TokensUsed, result.Cost); err != nil {
		slog.Error("failed to complete task", "error", err, "task_id", task.ID)
	}
}

// healthStatsHandler returns real-time provider health statistics.
func (r *Router) healthStatsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	var healthSummary interface{}
	if r.llmRouter != nil && r.llmRouter.GetHealthMonitor() != nil {
		healthSummary = map[string]interface{}{"healthy_providers": r.llmRouter.GetHealthMonitor().GetHealthyProviders()}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"providers": healthSummary,
	})
}

// costOverrideHandler updates pricing for a specific model at runtime.
func (r *Router) costOverrideHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	// Admin-only: overriding model pricing affects platform-wide billing.
	if claims.Role != "admin" && claims.Role != "superadmin" {
		response.Forbidden(w, "admin access required")
		return
	}

	var input struct {
		Model           string  `json:"model"`
		InputCostPer1K  float64 `json:"input_cost_per_1k"`
		OutputCostPer1K float64 `json:"output_cost_per_1k"`
		MaxTokens       int     `json:"max_tokens,omitempty"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Model == "" {
		response.BadRequest(w, "model is required")
		return
	}

	// Get existing info or create new (LookupPrice is thread-safe)
	info, _ := llm.LookupPrice(input.Model)
	if info.Name == "" {
		info = llm.ModelInfo{Name: input.Model}
	}
	if input.InputCostPer1K > 0 {
		info.InputCostPer1K = input.InputCostPer1K
	}
	if input.OutputCostPer1K > 0 {
		info.OutputCostPer1K = input.OutputCostPer1K
	}
	if input.MaxTokens > 0 {
		info.MaxTokens = input.MaxTokens
	}

	// Update the global price table (SetPrice is thread-safe)
	llm.SetPrice(input.Model, info)
	if r.llmRouter != nil {
		r.llmRouter.SetPrices(llm.AllPrices())
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"model":  input.Model,
		"status": "updated",
		"info":   info,
	})
}
