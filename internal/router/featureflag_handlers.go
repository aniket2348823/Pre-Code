package router

import (
	"encoding/json"
	"net/http"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/featureflags"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// listFeatureFlagsHandler lists all feature flags (admin only).
func (r *Router) listFeatureFlagsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	// Check admin role
	if claims.Role != "admin" {
		response.Forbidden(w, "admin access required")
		return
	}

	if r.featureFlags == nil {
		response.Success(w, http.StatusOK, []interface{}{})
		return
	}

	flags, err := r.featureFlags.GetAll(req.Context())
	if err != nil {
		response.InternalError(w, "failed to list feature flags")
		return
	}

	response.Success(w, http.StatusOK, flags)
}

// updateFeatureFlagHandler updates a feature flag (admin only).
func (r *Router) updateFeatureFlagHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	// Check admin role
	if claims.Role != "admin" {
		response.Forbidden(w, "admin access required")
		return
	}

	key := req.URL.Query().Get("key")
	if key == "" {
		response.BadRequest(w, "key is required")
		return
	}

	var flag featureflags.Flag
	if err := json.NewDecoder(req.Body).Decode(&flag); err != nil {
		response.BadRequest(w, "invalid request body")
		return
	}

	flag.Name = key

	if r.featureFlags == nil {
		response.InternalError(w, "feature flags not configured")
		return
	}

	if err := r.featureFlags.Set(req.Context(), &flag); err != nil {
		response.InternalError(w, "failed to update feature flag")
		return
	}

	response.Success(w, http.StatusOK, flag)
}

// deleteFeatureFlagHandler deletes a feature flag (admin only).
func (r *Router) deleteFeatureFlagHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	// Check admin role
	if claims.Role != "admin" {
		response.Forbidden(w, "admin access required")
		return
	}

	key := req.URL.Query().Get("key")
	if key == "" {
		response.BadRequest(w, "key is required")
		return
	}

	if r.featureFlags == nil {
		response.InternalError(w, "feature flags not configured")
		return
	}

	if err := r.featureFlags.Delete(req.Context(), key); err != nil {
		response.InternalError(w, "failed to delete feature flag")
		return
	}

	response.Success(w, http.StatusOK, map[string]string{"message": "feature flag deleted"})
}

// checkFeatureFlagHandler checks if a feature is enabled for the current user.
func (r *Router) checkFeatureFlagHandler(w http.ResponseWriter, req *http.Request) {
	_, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	key := req.URL.Query().Get("key")
	if key == "" {
		response.BadRequest(w, "key is required")
		return
	}

	if r.featureFlags == nil {
		response.NotFound(w, "feature flag not found")
		return
	}

	flag, err := r.featureFlags.Get(req.Context(), key)
	if err != nil || flag == nil {
		response.NotFound(w, "feature flag not found")
		return
	}

	response.Success(w, http.StatusOK, map[string]interface{}{
		"key":     flag.Name,
		"enabled": flag.Enabled,
	})
}
