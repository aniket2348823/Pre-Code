package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func sessionTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestListUserSessionsHandler_NoAuth(t *testing.T) {
	r := sessionTestRouter()
	req := httptest.NewRequest("GET", "/users/me/sessions", nil)
	w := httptest.NewRecorder()
	r.listUserSessionsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInvalidateSessionHandler_NoAuth(t *testing.T) {
	r := sessionTestRouter()
	req := httptest.NewRequest("POST", "/sessions/sess-1/invalidate", nil)
	w := httptest.NewRecorder()
	r.invalidateSessionHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestListActiveSessionsHandler_NoAuth(t *testing.T) {
	r := sessionTestRouter()
	req := httptest.NewRequest("GET", "/users/me/sessions/active", nil)
	w := httptest.NewRecorder()
	r.listActiveSessionsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInvalidateSessionHandler_EmptySessionID(t *testing.T) {
	r := sessionTestRouter()
	req := reqWithClaims("POST", "/sessions//invalidate", nil, testClaims)
	w := httptest.NewRecorder()
	r.invalidateSessionHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}
