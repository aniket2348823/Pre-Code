package router

import (
	"net/http"
	"strings"

	"github.com/vigilagent/vigilagent/internal/auth"
	mw "github.com/vigilagent/vigilagent/internal/middleware"
)

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
