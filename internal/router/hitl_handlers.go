package router

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	mw "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// Content from hitl_handlers.go
// approveHITLHandler approves or rejects a HITL checkpoint for a task.
// On approval, it broadcasts a real-time SSE event so the agent's polling
// goroutine (in streamTaskHandler) can resume execution. On rejection,
// it marks the task as failed.
func (r *Router) approveHITLHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	taskID := chi.URLParam(req, "taskID")

	var input struct {
		Decision string `json:"decision"` // "approve" or "reject"
		Reason   string `json:"reason"`
	}
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	if input.Decision != "approve" && input.Decision != "reject" {
		response.BadRequest(w, "decision must be 'approve' or 'reject'")
		return
	}

	// Verify task exists and user has access
	task, err := r.tasks.FindByID(req.Context(), taskID)
	if err != nil {
		response.NotFound(w, "task not found")
		return
	}

	// Only tasks actually waiting for a human decision may be decided.
	// This prevents resurrecting completed/failed/cancelled tasks.
	if task.Status != "waiting_hitl" && task.Status != "pending" {
		response.JSON(w, http.StatusConflict, map[string]interface{}{
			"error":  "task is not awaiting a HITL decision",
			"status": task.Status,
		})
		return
	}

	project, err := r.projects.FindByID(req.Context(), task.ProjectID)
	if err != nil {
		response.NotFound(w, "project not found")
		return
	}

	member, err := r.orgs.IsMember(req.Context(), project.OrgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}

	// Broadcast real-time HITL decision via SSE hub so the agent's
	// background goroutine can resume. The agent polls for SSE events
	// during waiting_hitl state and resumes on hitl.approved.
	if r.wsManager != nil {
		r.wsManager.SSEBroadcast(taskID, TaskSSEEvent{
			TaskID: taskID,
			Event:  "hitl_decision",
			Payload: map[string]interface{}{
				"decision": input.Decision,
				"user_id":  claims.UserID,
				"reason":   input.Reason,
			},
		})
	}

	// Update task status based on decision
	if input.Decision == "approve" {
		if err := r.tasks.UpdateStatus(req.Context(), taskID, "executing"); err != nil {
			response.InternalError(w, "failed to resume task")
			return
		}
	} else {
		if err := r.tasks.UpdateStatus(req.Context(), taskID, "failed"); err != nil {
			response.InternalError(w, "failed to reject task")
			return
		}
	}

	// Dispatch HITL decision webhook
	if r.webhookEngine != nil {
		eventType := "hitl.approved"
		if input.Decision == "reject" {
			eventType = "hitl.rejected"
		}

		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: eventType,
			Payload: map[string]interface{}{
				"task_id":  taskID,
				"user_id":  claims.UserID,
				"decision": input.Decision,
				"reason":   input.Reason,
			},
		})
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"task_id":  taskID,
		"decision": input.Decision,
		"message":  "HITL checkpoint " + input.Decision + "d",
	})
}

// Content from hitl_queue_handlers.go
// ensureHITLHandler lazily creates the HITL HTTP handler exactly once.
// The handler only wraps the fixed queue, so a single assignment is enough;
// using sync.Once prevents a data race when multiple requests arrive together.
func (r *Router) ensureHITLHandler() *mw.HITLHandler {
	r.hitlOnce.Do(func() {
		r.hitlHandler = mw.NewHITLHandler(r.hitlQueue)
	})
	return r.hitlHandler
}

// listHITLCheckpointsHandler returns all pending HITL checkpoints for the authenticated user.
func (r *Router) listHITLCheckpointsHandler(w http.ResponseWriter, req *http.Request) {
	r.ensureHITLHandler().ListPendingHandler(w, req)
}

// decideHITLHandler processes a human decision on a checkpoint.
func (r *Router) decideHITLHandler(w http.ResponseWriter, req *http.Request) {
	r.ensureHITLHandler().DecideHandler(w, req)
}

// hitlStatusHandler returns the status of a specific checkpoint.
func (r *Router) hitlStatusHandler(w http.ResponseWriter, req *http.Request) {
	r.ensureHITLHandler().StatusHandler(w, req)
}
