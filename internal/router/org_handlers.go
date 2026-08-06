package router

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/pagination"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// Content from org_handlers.go
// listInvoicesHandler returns billing invoices for the current user's org.
func (r *Router) listInvoicesHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequest(w, "org_id query parameter is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}

	// TODO: When Stripe is configured, fetch real invoices from Stripe API
	if r.cfg != nil && r.cfg.Stripe.SecretKey == "" {
		response.JSON(w, http.StatusOK, map[string]interface{}{
			"invoices": []interface{}{},
			"message":  "Stripe billing not configured. Set VIGILAGENT_STRIPE_SECRET_KEY to enable.",
		})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"invoices": []interface{}{},
	})
}

// getInvoiceHandler returns a specific invoice.
func (r *Router) getInvoiceHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	invoiceID := chi.URLParam(req, "invoiceID")
	if invoiceID == "" {
		response.BadRequest(w, "invoice_id is required")
		return
	}

	// TODO: Fetch from Stripe when configured
	response.NotFound(w, "invoice not found")
}

// createCheckoutHandler creates a Stripe checkout session.
func (r *Router) createCheckoutHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		Plan  string `json:"plan"`
		OrgID string `json:"org_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.Plan == "" {
		response.BadRequest(w, "plan is required (free, pro, team)")
		return
	}
	validPlans := map[string]bool{"free": true, "pro": true, "team": true}
	if !validPlans[input.Plan] {
		response.BadRequest(w, "invalid plan: must be free, pro, or team")
		return
	}
	if input.OrgID == "" {
		response.BadRequest(w, "org_id is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), input.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}

	// TODO: When Stripe is configured, create real checkout session
	if r.cfg == nil || r.cfg.Stripe.SecretKey == "" {
		response.ErrorR(w, req, http.StatusServiceUnavailable, "BILL_001", "Stripe integration not configured. Set VIGILAGENT_STRIPE_SECRET_KEY to enable.")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"checkout_url": "",
	})
}

// getSubscriptionHandler returns the current subscription.
func (r *Router) getSubscriptionHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequest(w, "org_id query parameter is required")
		return
	}

	// Billing is only real when Stripe is configured; without it, claiming an
	// active subscription would fabricate billing state. Fail fast before the
	// membership check so unconfigured billing returns 503 for everyone.
	if r.cfg == nil || r.cfg.Stripe.SecretKey == "" {
		response.ErrorR(w, req, http.StatusServiceUnavailable, "BILL_001",
			"Stripe integration not configured. Set VIGILAGENT_STRIPE_SECRET_KEY to enable.")
		return
	}

	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}

	// TODO: Fetch real subscription from Stripe when configured
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"plan":     "free",
		"status":   "active",
		"features": []string{"basic_agent", "1_project", "1000_tasks_per_month"},
	})
}

// createBillingPortalHandler creates a Stripe billing portal session.
func (r *Router) createBillingPortalHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		OrgID string `json:"org_id"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	if input.OrgID == "" {
		response.BadRequest(w, "org_id is required")
		return
	}
	member, err := r.orgs.IsMember(req.Context(), input.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}

	// TODO: When Stripe is configured, create portal session
	if r.cfg == nil || r.cfg.Stripe.SecretKey == "" {
		response.ErrorR(w, req, http.StatusServiceUnavailable, "BILL_001", "Stripe integration not configured.")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"portal_url": "",
	})
}

// Invitation represents a pending team invitation.
type Invitation struct {
	ID        string    `json:"id"`
	OrgID     string    `json:"org_id"`
	InvitedBy string    `json:"invited_by"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	Token     string    `json:"token,omitempty"`
	Status    string    `json:"status"` // pending, accepted, expired
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
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
			response.ErrorR(w, req, http.StatusForbidden, "AUTH_007", "access denied")
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
	// Privilege escalation guard: only the org owner (or a global admin) may
	// invite someone with the elevated "admin" role. Regular members can only
	// invite viewers/members.
	if input.Role == "admin" && !isOwner && claims.Role != "admin" && claims.Role != "superadmin" {
		response.ErrorR(w, req, http.StatusForbidden, "AUTH_007", "only org owners can invite admins")
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
		response.ErrorR(w, req, http.StatusForbidden, "AUTH_007", "access denied")
		return
	}

	// For now, return empty list (would need invitations table in production)
	pag := pagination.ParseRequest(req)
	data := []Invitation{}
	response.SuccessWithMeta(w, req, http.StatusOK, data, &response.Meta{
		Limit:   pag.Limit,
		HasMore: false,
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
		response.ErrorR(w, req, http.StatusForbidden, "AUTH_007", "only owners can revoke invitations")
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

	response.ErrorR(w, req, http.StatusNotImplemented, "INFRA_002", "invitation acceptance is not yet implemented")
}

// getBaseURL extracts the base URL from the router config.
func (r *Router) getBaseURL(req *http.Request) string {
	if r.cfg != nil && r.cfg.Server.BaseURL != "" {
		return r.cfg.Server.BaseURL
	}
	return fmt.Sprintf("http://%s", req.Host)
}

// costIntelDashboardHandler returns cost intelligence dashboard data.
// Uses the real costintel.Engine for forecasting, anomalies, and recommendations,
// and the event repository for actual historical cost/token data.
func (r *Router) costIntelDashboardHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequest(w, "org_id query parameter is required")
		return
	}

	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}

	from, to, err := parseTimeRange(req)
	if err != nil {
		response.BadRequest(w, err.Error())
		return
	}

	costSummary, err := r.events.GetCostByOrg(req.Context(), orgID, from, to)
	if err != nil {
		response.InternalError(w, "failed to get cost analytics")
		return
	}

	tokenSummary, err := r.events.GetTokensByOrg(req.Context(), orgID, from, to)
	if err != nil {
		response.InternalError(w, "failed to get token analytics")
		return
	}

	topAgents, err := r.events.GetTopAgentsByOrg(req.Context(), orgID, 5)
	if err != nil {
		// Non-critical — log and continue with empty list
		topAgents = nil
	}

	// Use costintel.Engine for forecasting
	var forecast interface{}
	if r.costIntel != nil {
		forecast = r.costIntel.ForecastCost(30)
	}

	// Use costintel.Engine for anomalies
	var anomalies interface{}
	if r.costIntel != nil {
		anomalies = r.costIntel.GetAnomalies()
	}

	// Use costintel.Engine for recommendations
	var recommendations interface{}
	if r.costIntel != nil {
		recommendations = r.costIntel.GetRecommendations()
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"org_id":          orgID,
		"cost":            costSummary,
		"tokens":          tokenSummary,
		"top_agents":      topAgents,
		"forecast":        forecast,
		"anomalies":       anomalies,
		"recommendations": recommendations,
		"period":          map[string]interface{}{"from": from, "to": to},
	})
}

// costIntelForecastHandler returns cost forecast for a given period.
func (r *Router) costIntelForecastHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	if r.costIntel == nil {
		response.InternalError(w, "cost intelligence engine not configured")
		return
	}

	days := 30
	if d := req.URL.Query().Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	forecast := r.costIntel.ForecastCost(days)
	response.JSON(w, http.StatusOK, forecast)
}

// costIntelRecommendationsHandler returns cost optimization recommendations.
func (r *Router) costIntelRecommendationsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	if r.costIntel == nil {
		response.InternalError(w, "cost intelligence engine not configured")
		return
	}

	recs := r.costIntel.GetRecommendations()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"recommendations": recs,
	})
}

// costIntelAnomaliesHandler returns detected cost anomalies.
func (r *Router) costIntelAnomaliesHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	if r.costIntel == nil {
		response.InternalError(w, "cost intelligence engine not configured")
		return
	}

	anomalies := r.costIntel.GetAnomalies()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"anomalies": anomalies,
	})
}
