package router

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

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
		response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":   "billing not configured",
			"message": "Stripe integration not configured. Set VIGILAGENT_STRIPE_SECRET_KEY to enable.",
		})
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
		response.JSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"error":   "billing not configured",
			"message": "Stripe integration not configured.",
		})
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"portal_url": "",
	})
}
