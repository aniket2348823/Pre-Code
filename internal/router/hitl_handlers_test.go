package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func hitlTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestApproveHITLHandler_NoAuth(t *testing.T) {
	r := hitlTestRouter()
	req := httptest.NewRequest("POST", "/tasks/task-1/hitl", nil)
	w := httptest.NewRecorder()
	r.approveHITLHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestApproveHITLHandler_EmptyBody(t *testing.T) {
	r := hitlTestRouter()
	req := reqWithClaims("POST", "/tasks/task-1/hitl", nil, testClaims)
	w := httptest.NewRecorder()
	r.approveHITLHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestApproveHITLHandler_InvalidDecision(t *testing.T) {
	r := hitlTestRouter()
	req := reqWithClaims("POST", "/tasks/task-1/hitl", map[string]interface{}{
		"decision": "maybe",
	}, testClaims)
	w := httptest.NewRecorder()
	r.approveHITLHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestApproveHITLHandler_Approve_NilRepo(t *testing.T) {
	r := hitlTestRouter()
	req := reqWithClaims("POST", "/tasks/task-1/hitl", map[string]interface{}{
		"decision": "approve",
	}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.approveHITLHandler(w, req)
	}()
}

func TestApproveHITLHandler_Reject_NilRepo(t *testing.T) {
	r := hitlTestRouter()
	req := reqWithClaims("POST", "/tasks/task-1/hitl", map[string]interface{}{
		"decision": "reject",
	}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.approveHITLHandler(w, req)
	}()
}

func TestHITLHandlers_AuthRequired(t *testing.T) {
	r := hitlTestRouter()
	req := httptest.NewRequest("POST", "/tasks/task-1/hitl", nil)
	w := httptest.NewRecorder()
	r.approveHITLHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
