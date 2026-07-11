package router

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/api/contract"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// createWebhookHandler registers a new webhook endpoint.
func (r *Router) createWebhookHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		URL    string   `json:"url"`
		Secret string   `json:"secret"`
		Events []string `json:"events"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	input.URL = strings.TrimSpace(input.URL)
	if input.URL == "" {
		response.BadRequest(w, "url is required")
		return
	}
	if !strings.HasPrefix(input.URL, "http://") && !strings.HasPrefix(input.URL, "https://") {
		response.BadRequest(w, "url must start with http:// or https://")
		return
	}
	if input.Secret == "" {
		response.BadRequest(w, "secret is required for webhook signature verification")
		return
	}
	if len(input.Events) == 0 {
		response.BadRequest(w, "at least one event is required")
		return
	}
	// Validate each event against known types
	for _, e := range input.Events {
		if !contract.WebhookEvent(e).Valid() {
			response.BadRequest(w, "invalid event type: "+e)
			return
		}
	}

	ep := &webhook.Endpoint{
		UserID: claims.UserID,
		URL:    input.URL,
		Secret: input.Secret,
		Events: input.Events,
		Active: true,
	}
	if err := r.webhookEngine.Register(req.Context(), ep); err != nil {
		response.InternalError(w, "failed to register webhook")
		return
	}

	response.Created(w, map[string]interface{}{
		"id":         ep.ID,
		"user_id":    ep.UserID,
		"url":        ep.URL,
		"events":     ep.Events,
		"active":     ep.Active,
		"created_at": ep.CreatedAt,
	})
}

// listWebhooksHandler lists all webhook endpoints for the authenticated user.
func (r *Router) listWebhooksHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	endpoints, err := r.webhookEngine.ListEndpoints(req.Context(), claims.UserID)
	if err != nil {
		response.InternalError(w, "failed to list webhooks")
		return
	}
	if endpoints == nil {
		endpoints = []webhook.Endpoint{}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"webhooks": endpoints,
	})
}

// getWebhookHandler returns a webhook endpoint by ID for the authenticated user.
func (r *Router) getWebhookHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	webhookID := chi.URLParam(req, "webhookID")

	ep, err := r.webhookEngine.GetEndpoint(req.Context(), claims.UserID, webhookID)
	if err != nil {
		response.NotFound(w, "webhook not found")
		return
	}
	// Don't leak the secret in API responses
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"id":         ep.ID,
		"user_id":    ep.UserID,
		"url":        ep.URL,
		"events":     ep.Events,
		"active":     ep.Active,
		"created_at": ep.CreatedAt,
	})
}

// deleteWebhookHandler removes a webhook endpoint by ID for the authenticated user.
func (r *Router) deleteWebhookHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	webhookID := chi.URLParam(req, "webhookID")

	if err := r.webhookEngine.Unregister(req.Context(), claims.UserID, webhookID); err != nil {
		response.InternalError(w, "failed to delete webhook")
		return
	}
	response.NoContent(w)
}

// getWebhookDeliveriesHandler returns recent delivery results for a webhook endpoint.
func (r *Router) getWebhookDeliveriesHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	webhookID := chi.URLParam(req, "webhookID")

	limit := 50 // default
	if limitStr := req.URL.Query().Get("limit"); limitStr != "" {
		parsed, err := strconv.Atoi(limitStr)
		if err != nil || parsed < 1 || parsed > 100 {
			response.BadRequest(w, "limit must be an integer between 1 and 100")
			return
		}
		limit = parsed
	}

	results, err := r.webhookEngine.GetResults(req.Context(), claims.UserID, webhookID, limit)
	if err != nil {
		response.InternalError(w, "failed to get deliveries")
		return
	}
	if results == nil {
		results = []webhook.DeliveryResult{}
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"deliveries": results,
	})
}

// webhookStatsHandler returns delivery statistics for the authenticated user.
func (r *Router) webhookStatsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	stats, err := r.webhookEngine.Stats(req.Context(), claims.UserID)
	if err != nil {
		response.InternalError(w, "failed to get stats")
		return
	}
	response.JSON(w, http.StatusOK, stats)
}
