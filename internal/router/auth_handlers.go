package router

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
	"github.com/vigilagent/vigilagent/internal/webhook"
	"github.com/vigilagent/vigilagent/pkg/pagination"
	"github.com/vigilagent/vigilagent/pkg/query"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// ── API Key Handlers ──────────────────────────────────────

// createAPIKeyHandler creates a new API key for the authenticated user.
// POST /api/v1/api-keys
func (r *Router) createAPIKeyHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	var input struct {
		Name   string   `json:"name"`
		Scopes []string `json:"scopes"`
	}
	// #nosec insecure_json_decode: request body is size-limited by the global limitBodySize middleware (router.go:50) or per-handler http.MaxBytesReader
	if err := json.NewDecoder(req.Body).Decode(&input); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}
	input.Name = strings.TrimSpace(input.Name)
	if input.Name == "" {
		response.BadRequest(w, "name is required")
		return
	}

	// Generate the API key
	apiKeyService := auth.NewAPIKeyService(r.cfg.Auth.APIKeyPrefix)
	plaintext, hash, prefix, err := apiKeyService.GenerateKey()
	if err != nil {
		response.InternalError(w, "failed to generate API key")
		return
	}

	key := &repository.APIKey{
		UserID:   claims.UserID,
		Name:     input.Name,
		KeyHash:  hash,
		Prefix:   prefix,
		Scopes:   input.Scopes,
		IsActive: true,
	}
	if err := r.apiKeys.Create(req.Context(), key); err != nil {
		response.InternalError(w, "failed to save API key")
		return
	}

	// Record API key creation for rate limiting
	if r.apiKeyCreateRateLimiter != nil {
		r.apiKeyCreateRateLimiter.Record(req.Context(), claims.UserID)
	}

	// Dispatch webhook notification (best-effort; engine is nil when no DB is
	// configured, e.g. dev/mock mode)
	r.dispatchWebhook(req.Context(), webhook.Event{
		Type: "apikey.created",
		Payload: map[string]interface{}{
			"key_id":  key.ID,
			"name":    key.Name,
			"user_id": claims.UserID,
		},
	})

	// Return the plaintext key ONCE - it will never be shown again
	response.Created(w, map[string]interface{}{
		"id":         key.ID,
		"name":       key.Name,
		"key":        plaintext,
		"prefix":     prefix,
		"scopes":     key.Scopes,
		"created_at": key.CreatedAt,
	})
}

// listAPIKeysHandler lists all API keys for the authenticated user (without hashes).
// GET /api/v1/api-keys
func (r *Router) listAPIKeysHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	keys, err := r.apiKeys.ListByUser(req.Context(), claims.UserID)
	if err != nil {
		response.InternalError(w, "failed to list API keys")
		return
	}

	filter, sortVal := query.Parse(req)
	pag := pagination.ParseRequest(req)
	processed, meta := query.ProcessList(keys, filter, sortVal, pag)

	response.SuccessWithMeta(w, req, http.StatusOK, processed, meta)
}

// rotateAPIKeyHandler performs key rotation with a 24h grace period for the old key.
// POST /api/v1/api-keys/{keyID}/rotate
func (r *Router) rotateAPIKeyHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	keyID := chi.URLParam(req, "keyID")

	apiKeyService := auth.NewAPIKeyService(r.cfg.Auth.APIKeyPrefix)
	result, err := apiKeyService.RotateKey()
	if err != nil {
		response.InternalError(w, "failed to generate rotated key")
		return
	}

	gracePeriod := 24 * time.Hour

	newKey, err := r.apiKeys.RotateAPIKey(
		req.Context(), keyID, claims.UserID,
		result.NewHash, result.NewPrefix, result.RotationTokenHash,
		gracePeriod,
	)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			response.NotFound(w, "API key not found")
			return
		}
		response.InternalError(w, "failed to rotate API key")
		return
	}

	// Dispatch webhook notification (best-effort; engine is nil when no DB is
	// configured, e.g. dev/mock mode)
	r.dispatchWebhook(req.Context(), webhook.Event{
		Type: "apikey.rotated",
		Payload: map[string]interface{}{
			"old_key_id": keyID,
			"new_key_id": newKey.ID,
			"user_id":    claims.UserID,
		},
	})

	response.Created(w, map[string]interface{}{
		"id":                  newKey.ID,
		"name":                newKey.Name,
		"key":                 result.NewPlaintext,
		"prefix":              result.NewPrefix,
		"old_key_valid_until": time.Now().Add(gracePeriod).Format(time.RFC3339),
	})
}

// deleteAPIKeyHandler revokes an API key.
// DELETE /api/v1/api-keys/{keyID}
func (r *Router) deleteAPIKeyHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	keyID := chi.URLParam(req, "keyID")
	if err := r.apiKeys.Delete(req.Context(), keyID, claims.UserID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			response.NotFound(w, "API key not found")
			return
		}
		response.InternalError(w, "failed to delete API key")
		return
	}

	// Dispatch webhook notification (best-effort; engine is nil when no DB is
	// configured, e.g. dev/mock mode)
	r.dispatchWebhook(req.Context(), webhook.Event{
		Type: "apikey.deleted",
		Payload: map[string]interface{}{
			"key_id":  keyID,
			"user_id": claims.UserID,
		},
	})

	response.NoContent(w)
}

// ── Session Handlers ──────────────────────────────────────

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

	filter, sortVal := query.Parse(req)
	pag := pagination.ParseRequest(req)
	processed, meta := query.ProcessList(activeSessions, filter, sortVal, pag)

	response.SuccessWithMeta(w, req, http.StatusOK, processed, meta)
}
