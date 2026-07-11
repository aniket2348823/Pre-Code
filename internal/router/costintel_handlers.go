package router

import (
	"net/http"
	"strconv"

	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/pkg/response"
)

// costIntelDashboardHandler returns cost intelligence dashboard data.
// Uses the real costintel.Engine for forecasting, anomalies, and recommendations,
// and the event repository for actual historical cost/token data.
func (r *Router) costIntelDashboardHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}

	orgID := req.URL.Query().Get("org_id")
	if orgID == "" {
		response.BadRequest(w, "org_id query parameter is required")
		return
	}

	member, err := r.orgs.IsMember(req.Context(), orgID, claims.UserID)
	if err != nil || !member {
		response.Forbidden(w, "access denied")
		return
	}

	from, to := parseTimeRange(req)

	costSummary, err := r.events.GetCostByOrg(req.Context(), orgID, from, to)
	if err != nil {
		response.InternalError(w, "failed to get cost analytics")
		return
	}

	tokenSummary, err := r.events.GetTokensByOrg(req.Context(), orgID, from, to)
	if err != nil {
		response.InternalError(w, "failed to get token analytics")
		return
	}

	topAgents, err := r.events.GetTopAgentsByOrg(req.Context(), orgID, 5)
	if err != nil {
		// Non-critical — log and continue with empty list
		topAgents = nil
	}

	// Use costintel.Engine for forecasting
	var forecast interface{}
	if r.costIntel != nil {
		forecast = r.costIntel.ForecastCost(30)
	}

	// Use costintel.Engine for anomalies
	var anomalies interface{}
	if r.costIntel != nil {
		anomalies = r.costIntel.GetAnomalies()
	}

	// Use costintel.Engine for recommendations
	var recommendations interface{}
	if r.costIntel != nil {
		recommendations = r.costIntel.GetRecommendations()
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"org_id":         orgID,
		"cost":           costSummary,
		"tokens":         tokenSummary,
		"top_agents":     topAgents,
		"forecast":       forecast,
		"anomalies":      anomalies,
		"recommendations": recommendations,
		"period":         map[string]interface{}{"from": from, "to": to},
	})
}

// costIntelForecastHandler returns cost forecast for a given period.
func (r *Router) costIntelForecastHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	if r.costIntel == nil {
		response.InternalError(w, "cost intelligence engine not configured")
		return
	}

	days := 30
	if d := req.URL.Query().Get("days"); d != "" {
		if parsed, err := strconv.Atoi(d); err == nil && parsed > 0 {
			days = parsed
		}
	}

	forecast := r.costIntel.ForecastCost(days)
	response.JSON(w, http.StatusOK, forecast)
}

// costIntelRecommendationsHandler returns cost optimization recommendations.
func (r *Router) costIntelRecommendationsHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	if r.costIntel == nil {
		response.InternalError(w, "cost intelligence engine not configured")
		return
	}

	recs := r.costIntel.GetRecommendations()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"recommendations": recs,
	})
}

// costIntelAnomaliesHandler returns detected cost anomalies.
func (r *Router) costIntelAnomaliesHandler(w http.ResponseWriter, req *http.Request) {
	claims, ok := auth.ClaimsFromContext(req.Context())
	if !ok {
		response.Unauthorized(w, "missing authentication")
		return
	}
	_ = claims

	if r.costIntel == nil {
		response.InternalError(w, "cost intelligence engine not configured")
		return
	}

	anomalies := r.costIntel.GetAnomalies()
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"anomalies": anomalies,
	})
}
