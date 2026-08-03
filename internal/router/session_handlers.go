package router

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/pkg/pagination"
	"github.com/vigilagent/vigilagent/pkg/query"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// listUserSessionsHandler returns all sessions for the authenticated user.
func (r *Router) listUserSessionsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	sessions, err := r.sessions.ListByUser(req.Context(), claims.UserID)
	if err != nil {
		response.InternalError(w, "failed to list sessions")
		return
	}
	if sessions == nil {
		sessions = []repository.Session{}
	}

	filter, sortVal := query.Parse(req)
	pag := pagination.ParseRequest(req)
	processed, meta := query.ProcessList(sessions, filter, sortVal, pag)

	response.SuccessWithMeta(w, req, http.StatusOK, processed, meta)
}

// invalidateSessionHandler cancels an active session.
func (r *Router) invalidateSessionHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	sessionID := chi.URLParam(req, "sessionID")
	if sessionID == "" {
		response.BadRequest(w, "sessionID is required")
		return
	}

	if err := r.sessions.InvalidateSession(req.Context(), sessionID, claims.UserID); err != nil {
		response.NotFound(w, "session not found or already inactive")
		return
	}

	// Broadcast session invalidation via SSE/WebSocket
	if r.wsManager != nil {
		r.wsManager.SSEBroadcast(sessionID, TaskSSEEvent{
			TaskID:  sessionID,
			Event:   "session.cancelled",
			Payload: map[string]interface{}{"reason": "invalidated by user"},
		})
	}

	response.JSON(w, http.StatusOK, map[string]string{"message": "session invalidated"})
}

// listActiveSessionsHandler returns only active sessions for the user.
func (r *Router) listActiveSessionsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	sessions, err := r.sessions.ListByUser(req.Context(), claims.UserID)
	if err != nil {
		response.InternalError(w, "failed to list sessions")
		return
	}

	// Filter to active sessions only
	var activeSessions []repository.Session
	for _, s := range sessions {
		if s.Status == "active" {
			activeSessions = append(activeSessions, s)
		}
	}
	if activeSessions == nil {
		activeSessions = []repository.Session{}
	}

	response.JSON(w, http.StatusOK, activeSessions)
}
