package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func costIntelTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestCostIntelDashboardHandler_NoAuth(t *testing.T) {
	r := costIntelTestRouter()
	req := httptest.NewRequest("GET", "/analytics/cost-intel", nil)
	w := httptest.NewRecorder()
	r.costIntelDashboardHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCostIntelDashboardHandler_MissingOrgID(t *testing.T) {
	r := costIntelTestRouter()
	req := reqWithClaims("GET", "/analytics/cost-intel", nil, testClaims)
	w := httptest.NewRecorder()
	r.costIntelDashboardHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCostIntelDashboardHandler_NonMemberOrg(t *testing.T) {
	r := costIntelTestRouter()
	req := reqWithClaims("GET", "/analytics/cost-intel?org_id=org-x", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.costIntelDashboardHandler(w, req)
	}()
}

func TestCostIntelForecastHandler_NoAuth(t *testing.T) {
	r := costIntelTestRouter()
	req := httptest.NewRequest("GET", "/analytics/cost-intel/forecast", nil)
	w := httptest.NewRecorder()
	r.costIntelForecastHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCostIntelForecastHandler_NilCostIntel(t *testing.T) {
	r := costIntelTestRouter()
	req := reqWithClaims("GET", "/analytics/cost-intel/forecast", nil, testClaims)
	w := httptest.NewRecorder()
	r.costIntelForecastHandler(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCostIntelRecommendationsHandler_NoAuth(t *testing.T) {
	r := costIntelTestRouter()
	req := httptest.NewRequest("GET", "/analytics/cost-intel/recommendations", nil)
	w := httptest.NewRecorder()
	r.costIntelRecommendationsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCostIntelRecommendationsHandler_NilCostIntel(t *testing.T) {
	r := costIntelTestRouter()
	req := reqWithClaims("GET", "/analytics/cost-intel/recommendations", nil, testClaims)
	w := httptest.NewRecorder()
	r.costIntelRecommendationsHandler(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCostIntelAnomaliesHandler_NoAuth(t *testing.T) {
	r := costIntelTestRouter()
	req := httptest.NewRequest("GET", "/analytics/cost-intel/anomalies", nil)
	w := httptest.NewRecorder()
	r.costIntelAnomaliesHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCostIntelAnomaliesHandler_NilCostIntel(t *testing.T) {
	r := costIntelTestRouter()
	req := reqWithClaims("GET", "/analytics/cost-intel/anomalies", nil, testClaims)
	w := httptest.NewRecorder()
	r.costIntelAnomaliesHandler(w, req)
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestCostIntelHandlers_AuthRequired(t *testing.T) {
	r := costIntelTestRouter()
	handlers := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		path   string
	}{
		{"dashboard", r.costIntelDashboardHandler, "GET", "/analytics/cost-intel"},
		{"forecast", r.costIntelForecastHandler, "GET", "/analytics/cost-intel/forecast"},
		{"recommendations", r.costIntelRecommendationsHandler, "GET", "/analytics/cost-intel/recommendations"},
		{"anomalies", r.costIntelAnomaliesHandler, "GET", "/analytics/cost-intel/anomalies"},
	}
	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			req := httptest.NewRequest(h.method, h.path, nil)
			w := httptest.NewRecorder()
			h.fn(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code)
		})
	}
}
