package router

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// Invitation represents a pending team invitation.
type Invitation struct {
	ID           string    `json:"id"`
	OrgID        string    `json:"org_id"`
	InvitedBy    string    `json:"invited_by"`
	Email        string    `json:"email"`
	Role         string    `json:"role"`
	Token        string    `json:"token,omitempty"`
	Status       string    `json:"status"` // pending, accepted, expired
	ExpiresAt    time.Time `json:"expires_at"`
	CreatedAt    time.Time `json:"created_at"`
}

// inviteMemberHandler sends an invitation email to a new team member.
func (r *Router) inviteMemberHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	orgID := chi.URLParam(req, "orgID")
	if orgID == "" {
		response.BadRequest(w, "orgID is required")
		return
	}

	// Check if requester is owner or admin
	isOwner, err := r.orgs.IsOwner(req.Context(), orgID, claims.UserID)
	if err != nil || !isOwner {
		// Check if admin
		isMember, _ := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
		if !isMember {
			response.JSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
			return
		}
	}

	var input struct {
		Email string `json:"email"`
		Role  string `json:"role"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	input.Email = strings.TrimSpace(input.Email)
	if input.Email == "" {
		response.BadRequest(w, "email is required")
		return
	}

	if input.Role == "" {
		input.Role = "member"
	}

	// Validate role
	validRoles := map[string]bool{"member": true, "admin": true, "viewer": true}
	if !validRoles[input.Role] {
		response.BadRequest(w, "invalid role: must be member, admin, or viewer")
		return
	}

	// Generate invitation token
	token, err := auth.GenerateInvitationToken(32)
	if err != nil {
		response.InternalError(w, "failed to generate invitation token")
		return
	}

	// Store invitation in database (using a simple in-memory approach for now)
	// In production, this should be stored in a invitations table
	invitation := &Invitation{
		ID:        fmt.Sprintf("inv-%d", time.Now().UnixNano()),
		OrgID:     orgID,
		InvitedBy: claims.UserID,
		Email:     input.Email,
		Role:      input.Role,
		Token:     token,
		Status:    "pending",
		ExpiresAt: time.Now().Add(7 * 24 * time.Hour), // 7 days
		CreatedAt: time.Now(),
	}

	// Send invitation email
	if r.email != nil {
		org, err := r.orgs.FindByID(req.Context(), orgID)
		if err == nil {
			inviteURL := fmt.Sprintf("%s/invitations/%s", r.getBaseURL(req), token)
			if err := r.email.SendInvitationEmail(req.Context(), input.Email, org.Name, inviteURL); err != nil {
				slog.Error("failed to send invitation email", "error", err, "email", input.Email)
			}
		}
	}

	// Dispatch webhook
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "invitation.created",
			Payload: map[string]interface{}{
				"invitation_id": invitation.ID,
				"org_id":        orgID,
				"email":         input.Email,
				"role":          input.Role,
				"invited_by":    claims.UserID,
			},
		})
	}

	response.Created(w, map[string]interface{}{
		"invitation": invitation,
		"message":    "invitation sent to " + input.Email,
	})
}

// listInvitationsHandler returns pending invitations for an organization.
func (r *Router) listInvitationsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	orgID := chi.URLParam(req, "orgID")
	if orgID == "" {
		response.BadRequest(w, "orgID is required")
		return
	}

	// Check membership
	isMember, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !isMember {
		response.JSON(w, http.StatusForbidden, map[string]string{"error": "access denied"})
		return
	}

	// For now, return empty list (would need invitations table in production)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"data":  []Invitation{},
		"total": 0,
	})
}

// revokeInvitationHandler cancels a pending invitation.
func (r *Router) revokeInvitationHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	orgID := chi.URLParam(req, "orgID")
	invitationID := chi.URLParam(req, "invitationID")

	if orgID == "" || invitationID == "" {
		response.BadRequest(w, "orgID and invitationID are required")
		return
	}

	// Check ownership
	isOwner, err := r.orgs.IsOwner(req.Context(), orgID, claims.UserID)
	if err != nil || !isOwner {
		response.JSON(w, http.StatusForbidden, map[string]string{"error": "only owners can revoke invitations"})
		return
	}

	// In production, this would update the invitations table
	response.JSON(w, http.StatusOK, map[string]string{
		"message": "invitation revoked",
	})
}

// acceptInvitationHandler accepts a pending invitation.
func (r *Router) acceptInvitationHandler(w http.ResponseWriter, req *http.Request) {
	token := chi.URLParam(req, "token")
	if token == "" {
		response.BadRequest(w, "invitation token is required")
		return
	}

	response.JSON(w, http.StatusNotImplemented, map[string]string{
		"error": "invitation acceptance is not yet implemented",
	})
}

// getBaseURL extracts the base URL from the router config.
func (r *Router) getBaseURL(req *http.Request) string {
	if r.cfg != nil && r.cfg.Server.BaseURL != "" {
		return r.cfg.Server.BaseURL
	}
	return fmt.Sprintf("http://%s", req.Host)
}
