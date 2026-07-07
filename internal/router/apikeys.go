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

	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "apikey.created",
			Payload: map[string]interface{}{
				"key_id": key.ID,
				"name":   key.Name,
				"user_id": claims.UserID,
			},
		})
	}

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

	keys, err :=	r.apiKeys.ListByUser(req.Context(), claims.UserID)
	if err != nil {
		response.InternalError(w, "failed to list API keys")
		return
	}

	response.JSON(w, http.StatusOK, keys)
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
	if err :=	r.apiKeys.Delete(req.Context(), keyID, claims.UserID); err != nil {
		if strings.Contains(err.Error(), "not found") {
			response.NotFound(w, "API key not found")
			return
		}
		response.InternalError(w, "failed to delete API key")
		return
	}

	// Dispatch webhook notification
	if r.webhookEngine != nil {
		r.webhookEngine.Dispatch(req.Context(), webhook.Event{
			Type: "apikey.deleted",
			Payload: map[string]interface{}{
				"key_id": keyID,
				"user_id": claims.UserID,
			},
		})
	}

	response.NoContent(w)
}
