package router

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

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
	_ = r.orgs.Count(ctx, &totalOrgs)

	// Get project count
	var totalProjects int
	_ = r.projects.Count(ctx, &totalProjects)

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

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	pageSize, _ := strconv.Atoi(req.URL.Query().Get("page_size"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize

	users, err := r.users.List(req.Context(), offset, pageSize)
	if err != nil {
		response.InternalError(w, "failed to list users")
		return
	}
	if users == nil {
		users = []repository.User{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"users": users,
		"page": map[string]interface{}{
			"page":      page,
			"page_size": pageSize,
		},
	})
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
