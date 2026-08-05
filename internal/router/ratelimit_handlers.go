package router

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/vigilagent/vigilagent/internal/auth"
	mw "github.com/vigilagent/vigilagent/internal/middleware"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// Content from ratelimit_handlers.go
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

// apiKeyRateLimitMiddleware applies per-API-key rate limiting.
// When authenticated via API key, rate limits are keyed by the API key prefix
// (not the user ID), so a single user with multiple keys gets independent
// rate limit buckets per key. Falls back to per-user limiting for JWT auth.
func (r *Router) apiKeyRateLimitMiddleware(next http.Handler) http.Handler {
	if r.rl == nil {
		slog.Warn("per-API-key rate limiting disabled: limiter not configured")
		return next
	}
	return r.rl.Middleware(func(req *http.Request) string {
		// Check if authenticated via API key (X-API-Key header or Bearer vga_...)
		if apiKey := extractAPIKeyFromRequest(req); apiKey != "" {
			return "apikey:" + rateLimitKeyFromAPIKey(apiKey)
		}
		// Fall back to user-based limiting
		claims, ok := auth.ClaimsFromContext(req.Context())
		if ok {
			return "user:" + claims.UserID
		}
		return "ip:" + req.RemoteAddr
	})(next)
}

// extractAPIKeyFromRequest pulls the API key from the request headers
// without performing full authentication.
func extractAPIKeyFromRequest(req *http.Request) string {
	if key := req.Header.Get("X-API-Key"); key != "" {
		return key
	}
	authHeader := req.Header.Get("Authorization")
	if authHeader != "" {
		parts := strings.SplitN(authHeader, " ", 2)
		if len(parts) == 2 && parts[0] == "Bearer" {
			token := parts[1]
			// API keys have prefix va_ and no dots (JWTs have dots)
			if !strings.Contains(token, ".") && strings.Contains(token, "_") {
				return token
			}
		}
	}
	return ""
}

// rateLimitKeyFromAPIKey derives a rate limit key from an API key.
// Uses the first 8 characters which are unique enough for rate limiting.
func rateLimitKeyFromAPIKey(apiKey string) string {
	if len(apiKey) > 8 {
		return apiKey[:8]
	}
	return apiKey
}

// Content from ratelimit_helpers.go
// orgPlanExtractor extracts the org ID and plan from a request.
// Used by PlanAwareRateLimiter, UsageMeteringMiddleware, and QuotaEnforcer.
// It attempts to look up the org's actual plan from the database/cache.
func (r *Router) orgPlanExtractor(req *http.Request) (string, mw.PlanTier) {
	// Try to get org ID from JWT claims
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		return "", mw.PlanFree
	}

	// Try to get org ID from query param first, then from claims
	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		orgID = claims.OrgID
	}

	// Look up org's actual plan from the database if we have a pool.
	// Falls back to free plan if the billing_plan column doesn't exist yet
	// (migration 000008_add_billing_plan.up.sql needs to be run).
	plan := mw.PlanFree
	if r.db != nil && orgID != "" {
		var planStr string
		err := r.db.Pool.QueryRow(req.Context(),
			"SELECT COALESCE(billing_plan, 'free') FROM organizations WHERE id = $1",
			orgID,
		).Scan(&planStr)
		if err == nil && planStr != "" {
			plan = mw.PlanTier(strings.ToLower(planStr))
		}
	}

	return orgID, plan
}
