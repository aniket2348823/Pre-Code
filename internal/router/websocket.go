package router

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// TaskSSEEvent is a real-time event pushed from the agent to SSE subscribers.
type TaskSSEEvent struct {
	TaskID  string
	Event   string
	Payload map[string]interface{}
}

// handleWebSocket handles WebSocket upgrade requests.
// Since this project uses SSE (Server-Sent Events) for real-time streaming
// (see streamTaskHandler), this endpoint returns guidance directing clients
// to the SSE endpoints instead.
func (r *Router) handleWebSocket(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	// Check connection limits via the unified WebSocket manager
	if r.wsManager != nil && !r.wsManager.CanConnect(claims.UserID) {
		slog.Warn("WebSocket connection rejected: limit exceeded", "user_id", claims.UserID)
		response.JSON(w, http.StatusTooManyRequests, map[string]string{
			"error": "WebSocket connection limit exceeded",
		})
		return
	}

	// Register the connection for tracking
	if r.wsManager != nil {
		r.wsManager.RegisterConnection(claims.UserID)
		defer r.wsManager.UnregisterConnection(claims.UserID)
	}

	// The project uses SSE for real-time streaming, not WebSocket.
	// Return a simple message directing clients to the SSE endpoint.
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"message": "VigilAgent uses SSE for real-time streaming, not WebSocket.",
		"sse_endpoint": fmt.Sprintf("/api/v1/tasks/{taskID}/stream"),
		"usage": "GET /api/v1/tasks/{taskID}/stream with Authorization: Bearer <token>",
		"events": []string{"task_update", "task_planning", "task_executing", "task_completed", "task_failed", "hitl_decision", "heartbeat"},
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}
