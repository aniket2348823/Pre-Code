package router

import (
	"net/http"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// RateLimitStats represents rate limit statistics for a user or key.
type RateLimitStats struct {
	Key            string    `json:"key"`
	Type           string    `json:"type"` // "user" or "apikey"
	RequestsPerMin int       `json:"requests_per_min"`
	CurrentUsage   int       `json:"current_usage"`
	Remaining      int       `json:"remaining"`
	ResetAt        time.Time `json:"reset_at"`
	WindowStart    time.Time `json:"window_start"`
	TotalRejected  int64     `json:"total_rejected"`
}

// RateLimitOverview represents the overall rate limit status.
type RateLimitOverview struct {
	TotalKeys    int              `json:"total_keys"`
	ActiveKeys   int              `json:"active_keys"`
	TotalHits    int64            `json:"total_hits"`
	TotalRejects int64            `json:"total_rejects"`
	Stats        []RateLimitStats `json:"stats"`
	TopKeys      []RateLimitStats `json:"top_keys"`
}

// rateLimitDashboardHandler returns rate limit statistics.
func (r *Router) rateLimitDashboardHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing auth")
		return
	}

	// Admin can see all stats, regular users only their own
	overview := RateLimitOverview{
		TotalKeys:    0,
		ActiveKeys:   0,
		TotalHits:    0,
		TotalRejects: 0,
		Stats:        []RateLimitStats{},
		TopKeys:      []RateLimitStats{},
	}

	// Query API keys for the user
	if r.apiKeys != nil {
		ctx := req.Context()
		keys, err := r.apiKeys.ListByUser(ctx, claims.UserID)
		if err == nil {
			overview.TotalKeys = len(keys)
			for _, key := range keys {
				if key.IsActive {
					overview.ActiveKeys++
				}
			}
		}
	}

	// TODO: Query actual rate limit counters from Redis/memory
	// For now, return the structure with placeholder data

	response.Success(w, http.StatusOK, overview)
}
