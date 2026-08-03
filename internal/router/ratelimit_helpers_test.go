package router

import (
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/auth"
)

func ratelimitTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestOrgPlanExtractor_NoClaims(t *testing.T) {
	r := ratelimitTestRouter()
	req := httptest.NewRequest("GET", "/test", nil)
	orgID, plan := r.orgPlanExtractor(req)
	assert.Equal(t, "", orgID)
	assert.Equal(t, "free", string(plan))
}

func TestOrgPlanExtractor_WithClaimsNoOrgID(t *testing.T) {
	r := ratelimitTestRouter()
	req := reqWithClaims("GET", "/test", nil, testClaims)
	orgID, plan := r.orgPlanExtractor(req)
	assert.Equal(t, "", orgID)
	assert.Equal(t, "free", string(plan))
}

func TestOrgPlanExtractor_WithClaimsAndOrgIDQuery(t *testing.T) {
	r := ratelimitTestRouter()
	req := reqWithClaims("GET", "/test?org_id=org-123", nil, testClaims)
	orgID, plan := r.orgPlanExtractor(req)
	assert.Equal(t, "org-123", orgID)
	// db is nil, so plan stays free
	assert.Equal(t, "free", string(plan))
}

func TestOrgPlanExtractor_NilDB(t *testing.T) {
	r := ratelimitTestRouter()
	r.db = nil
	req := reqWithClaims("GET", "/test?org_id=org-456", nil, testClaims)
	orgID, plan := r.orgPlanExtractor(req)
	assert.Equal(t, "org-456", orgID)
	assert.Equal(t, "free", string(plan))
}

func TestOrgPlanExtractor_WithClaimsOrgIDInClaims(t *testing.T) {
	r := ratelimitTestRouter()
	claims := &auth.Claims{
		UserID: "user-1",
		Email:  "test@example.com",
		Role:   "user",
		OrgID:  "org-from-claims",
	}
	req := reqWithClaims("GET", "/test", nil, claims)
	orgID, plan := r.orgPlanExtractor(req)
	assert.Equal(t, "org-from-claims", orgID)
	assert.Equal(t, "free", string(plan))
}

func TestOrgPlanExtractor_QueryParamTakesPriority(t *testing.T) {
	r := ratelimitTestRouter()
	claims := &auth.Claims{
		UserID: "user-1",
		Email:  "test@example.com",
		Role:   "user",
		OrgID:  "org-from-claims",
	}
	req := reqWithClaims("GET", "/test?org_id=org-from-query", nil, claims)
	orgID, _ := r.orgPlanExtractor(req)
	assert.Equal(t, "org-from-query", orgID)
}
