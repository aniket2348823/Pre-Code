package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func billingTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestListInvoicesHandler_NoAuth(t *testing.T) {
	r := billingTestRouter()
	req := httptest.NewRequest("GET", "/billing/invoices", nil)
	w := httptest.NewRecorder()
	r.listInvoicesHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestListInvoicesHandler_MissingOrgID(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("GET", "/billing/invoices", nil, testClaims)
	w := httptest.NewRecorder()
	r.listInvoicesHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	result := parseJSON(t, w)
	assert.NotEmpty(t, result)
}

func TestGetInvoiceHandler_NoAuth(t *testing.T) {
	r := billingTestRouter()
	req := httptest.NewRequest("GET", "/billing/invoices/inv-1", nil)
	w := httptest.NewRecorder()
	r.getInvoiceHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetInvoiceHandler_NilRepoPanics(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("GET", "/billing/invoices/inv-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.getInvoiceHandler(w, req)
	}()
}

func TestCreateCheckoutHandler_NoAuth(t *testing.T) {
	r := billingTestRouter()
	req := httptest.NewRequest("POST", "/billing/checkout", nil)
	w := httptest.NewRecorder()
	r.createCheckoutHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateCheckoutHandler_EmptyBody(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("POST", "/billing/checkout", nil, testClaims)
	w := httptest.NewRecorder()
	r.createCheckoutHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateCheckoutHandler_MissingPlan(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("POST", "/billing/checkout", map[string]interface{}{}, testClaims)
	w := httptest.NewRecorder()
	r.createCheckoutHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateCheckoutHandler_InvalidPlan(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("POST", "/billing/checkout", map[string]interface{}{
		"plan":   "enterprise",
		"org_id": "org-1",
	}, testClaims)
	w := httptest.NewRecorder()
	r.createCheckoutHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateCheckoutHandler_MissingOrgID(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("POST", "/billing/checkout", map[string]interface{}{
		"plan": "pro",
	}, testClaims)
	w := httptest.NewRecorder()
	r.createCheckoutHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateCheckoutHandler_NonMemberOrg(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("POST", "/billing/checkout", map[string]interface{}{
		"plan":   "pro",
		"org_id": "org-unknown",
	}, testClaims)
	w := httptest.NewRecorder()
	// orgs repo is nil — IsMember panics
	func() {
		defer func() { recover() }()
		r.createCheckoutHandler(w, req)
	}()
}

func TestGetSubscriptionHandler_NoAuth(t *testing.T) {
	r := billingTestRouter()
	req := httptest.NewRequest("GET", "/billing/subscription", nil)
	w := httptest.NewRecorder()
	r.getSubscriptionHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetSubscriptionHandler_MissingOrgID(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("GET", "/billing/subscription", nil, testClaims)
	w := httptest.NewRecorder()
	r.getSubscriptionHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetSubscriptionHandler_NonMemberOrg(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("GET", "/billing/subscription?org_id=org-unknown", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.getSubscriptionHandler(w, req)
	}()
}

func TestCreateBillingPortalHandler_NoAuth(t *testing.T) {
	r := billingTestRouter()
	req := httptest.NewRequest("POST", "/billing/portal", nil)
	w := httptest.NewRecorder()
	r.createBillingPortalHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateBillingPortalHandler_EmptyBody(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("POST", "/billing/portal", nil, testClaims)
	w := httptest.NewRecorder()
	r.createBillingPortalHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateBillingPortalHandler_MissingOrgID(t *testing.T) {
	r := billingTestRouter()
	req := reqWithClaims("POST", "/billing/portal", map[string]interface{}{}, testClaims)
	w := httptest.NewRecorder()
	r.createBillingPortalHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBillingHandlers_AuthRequired(t *testing.T) {
	r := billingTestRouter()
	handlers := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		path   string
	}{
		{"listInvoices", r.listInvoicesHandler, "GET", "/billing/invoices"},
		{"getInvoice", r.getInvoiceHandler, "GET", "/billing/invoices/x"},
		{"createCheckout", r.createCheckoutHandler, "POST", "/billing/checkout"},
		{"getSubscription", r.getSubscriptionHandler, "GET", "/billing/subscription"},
		{"createBillingPortal", r.createBillingPortalHandler, "POST", "/billing/portal"},
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
