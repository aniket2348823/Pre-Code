package router

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// adminStatsHandler returns platform-wide statistics.
func (r *Router) adminStatsHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"total_users":      0,
		"total_orgs":       0,
		"total_projects":   0,
		"total_agents":     0,
		"total_sessions":   0,
		"total_tasks":      0,
		"total_skills":     0,
		"active_users_24h": 0,
		"message":          "admin stats placeholder",
	})
}

// adminListUsersHandler returns all users (admin only).
func (r *Router) adminListUsersHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"users":   []interface{}{},
		"message": "admin user listing placeholder",
	})
}

// adminUpdateUserRoleHandler updates a user's role.
func (r *Router) adminUpdateUserRoleHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
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

	// Verify user exists
	if _, err := r.users.FindByID(req.Context(), userID); err != nil {
		response.NotFound(w, "user not found")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"user_id": userID,
		"role":    input.Role,
		"message": "role updated (placeholder - DB update not implemented)",
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

	// Prevent self-deletion
	if claims.UserID == userID {
		response.BadRequest(w, "cannot delete your own account")
		return
	}

	if _, err := r.users.FindByID(req.Context(), userID); err != nil {
		response.NotFound(w, "user not found")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"user_id": userID,
		"message": "user deleted (placeholder - DB delete not implemented)",
	})
}
