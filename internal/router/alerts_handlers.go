package router

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// listAlertsHandler returns alerts for the current user.
func (r *Router) listAlertsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	alerts, err := r.alerts.ListByUser(req.Context(), claims.UserID)
	if err != nil {
		response.InternalError(w, "failed to list alerts")
		return
	}
	if alerts == nil {
		alerts = []repository.Alert{}
	}
	response.JSON(w, http.StatusOK, alerts)
}

// createAlertHandler creates a new alert.
func (r *Router) createAlertHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	var input struct {
		Name      string                 `json:"name"`
		Type      string                 `json:"type"`
		Condition map[string]interface{} `json:"condition"`
		Channel   string                 `json:"channel"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		response.BadRequest(w, "name is required")
		return
	}
	if input.Type == "" {
		input.Type = "threshold"
	}
	if input.Channel == "" {
		input.Channel = "webhook"
	}
	// Ensure Condition is never nil
	if input.Condition == nil {
		input.Condition = map[string]interface{}{}
	}

	alert := &repository.Alert{
		UserID:    claims.UserID,
		Name:      input.Name,
		Type:      input.Type,
		Condition: input.Condition,
		Channel:   input.Channel,
		IsActive:  true,
	}
	if err := r.alerts.Create(req.Context(), alert); err != nil {
		response.InternalError(w, "failed to create alert")
		return
	}

	// Dispatch webhook notification for alert creation
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "alert.created",
			Payload: map[string]interface{}{
				"alert_id": alert.ID,
				"name":     alert.Name,
				"type":     alert.Type,
				"channel":  alert.Channel,
			},
		})
	}

	response.Created(w, alert)
}

// getAlertHandler returns an alert by ID.
func (r *Router) getAlertHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	alertID := chi.URLParam(req, "alertID")
	alert, err := r.alerts.FindByID(req.Context(), alertID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	if alert.UserID != claims.UserID {
		response.Forbidden(w, "access denied")
		return
	}
	response.JSON(w, http.StatusOK, alert)
}

// updateAlertHandler updates an alert.
func (r *Router) updateAlertHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	alertID := chi.URLParam(req, "alertID")
	alert, err := r.alerts.FindByID(req.Context(), alertID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	if alert.UserID != claims.UserID {
		response.Forbidden(w, "access denied")
		return
	}

	var input struct {
		Name     string `json:"name"`
		Channel  string `json:"channel"`
		IsActive *bool  `json:"is_active"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	name := alert.Name
	if input.Name != "" {
		name = input.Name
	}
	channel := alert.Channel
	if input.Channel != "" {
		channel = input.Channel
	}
	isActive := alert.IsActive
	if input.IsActive != nil {
		isActive = *input.IsActive
	}

	if err := r.alerts.Update(req.Context(), alertID, name, channel, isActive); err != nil {
		response.InternalError(w, "failed to update alert")
		return
	}
	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "alert.updated",
			Payload: map[string]interface{}{"alert_id": alertID, "name": name},
		})
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "alert updated"})
}

// deleteAlertHandler deletes an alert.
func (r *Router) deleteAlertHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	alertID := chi.URLParam(req, "alertID")
	alert, err := r.alerts.FindByID(req.Context(), alertID)
	if err != nil {
		response.NotFound(w, err.Error())
		return
	}
	if alert.UserID != claims.UserID {
		response.Forbidden(w, "access denied")
		return
	}
	if err := r.alerts.Delete(req.Context(), alertID); err != nil {
		response.InternalError(w, "failed to delete alert")
		return
	}
	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "alert.deleted",
			Payload: map[string]interface{}{"alert_id": alertID},
		})
	}
	response.NoContent(w)
}
