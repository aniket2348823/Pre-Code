package router

import (
	"net/http"
	"strconv"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// replayWebhookHandler replays failed webhook deliveries.
func (r *Router) replayWebhookHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	webhookID := req.URL.Query().Get("webhook_id")
	if webhookID == "" {
		response.BadRequest(w, "webhook_id is required")
		return
	}

	limitStr := req.URL.Query().Get("limit")
	limit := 10
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	// Verify ownership
	if r.webhookEngine != nil {
		ctx := req.Context()
		endpoint, err := r.webhookEngine.GetEndpoint(ctx, claims.UserID, webhookID)
		if err != nil {
			response.NotFound(w, "webhook not found")
			return
		}
		_ = endpoint // Used for ownership verification

		// Get failed deliveries
		results, err := r.webhookEngine.GetResults(ctx, claims.UserID, webhookID, limit)
		if err != nil {
			response.InternalError(w, "failed to get deliveries")
			return
		}

		// Replay failed deliveries
		replayed := 0
		for _, result := range results {
			if !result.Success || result.StatusCode >= 400 {
				// Re-dispatch the event
				if r.webhookEngine != nil {
					r.webhookEngine.Dispatch(ctx, webhook.Event{
						Type: result.EventType,
					})
					replayed++
				}
			}
		}

		response.Success(w, http.StatusOK, map[string]interface{}{
			"replayed": replayed,
			"total":    len(results),
		})
		return
	}

	response.InternalError(w, "webhook engine not available")
}
