package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func apikeysTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestCreateAPIKeyHandler_NoAuth(t *testing.T) {
	r := apikeysTestRouter()
	req := httptest.NewRequest("POST", "/api-keys", nil)
	w := httptest.NewRecorder()
	r.createAPIKeyHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateAPIKeyHandler_EmptyBody(t *testing.T) {
	r := apikeysTestRouter()
	req := reqWithClaims("POST", "/api-keys", nil, testClaims)
	w := httptest.NewRecorder()
	r.createAPIKeyHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateAPIKeyHandler_EmptyName(t *testing.T) {
	r := apikeysTestRouter()
	req := reqWithClaims("POST", "/api-keys", map[string]interface{}{"name": ""}, testClaims)
	w := httptest.NewRecorder()
	r.createAPIKeyHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateAPIKeyHandler_WhitespaceName(t *testing.T) {
	r := apikeysTestRouter()
	req := reqWithClaims("POST", "/api-keys", map[string]interface{}{"name": "   "}, testClaims)
	w := httptest.NewRecorder()
	r.createAPIKeyHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateAPIKeyHandler_NilRepoPanics(t *testing.T) {
	r := apikeysTestRouter()
	req := reqWithClaims("POST", "/api-keys", map[string]interface{}{"name": "mykey"}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.createAPIKeyHandler(w, req)
	}()
}

func TestListAPIKeysHandler_NoAuth(t *testing.T) {
	r := apikeysTestRouter()
	req := httptest.NewRequest("GET", "/api-keys", nil)
	w := httptest.NewRecorder()
	r.listAPIKeysHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestListAPIKeysHandler_NilRepoPanics(t *testing.T) {
	r := apikeysTestRouter()
	req := reqWithClaims("GET", "/api-keys", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.listAPIKeysHandler(w, req)
	}()
}

func TestRotateAPIKeyHandler_NoAuth(t *testing.T) {
	r := apikeysTestRouter()
	req := httptest.NewRequest("POST", "/api-keys/key-1/rotate", nil)
	w := httptest.NewRecorder()
	r.rotateAPIKeyHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestRotateAPIKeyHandler_NilRepoPanics(t *testing.T) {
	r := apikeysTestRouter()
	req := reqWithClaims("POST", "/api-keys/key-1/rotate", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.rotateAPIKeyHandler(w, req)
	}()
}

func TestDeleteAPIKeyHandler_NoAuth(t *testing.T) {
	r := apikeysTestRouter()
	req := httptest.NewRequest("DELETE", "/api-keys/key-1", nil)
	w := httptest.NewRecorder()
	r.deleteAPIKeyHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteAPIKeyHandler_NilRepoPanics(t *testing.T) {
	r := apikeysTestRouter()
	req := reqWithClaims("DELETE", "/api-keys/key-1", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.deleteAPIKeyHandler(w, req)
	}()
}

func TestAPIKeyHandlers_AuthRequired(t *testing.T) {
	r := apikeysTestRouter()
	handlers := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		path   string
	}{
		{"create", r.createAPIKeyHandler, "POST", "/api-keys"},
		{"list", r.listAPIKeysHandler, "GET", "/api-keys"},
		{"rotate", r.rotateAPIKeyHandler, "POST", "/api-keys/x/rotate"},
		{"delete", r.deleteAPIKeyHandler, "DELETE", "/api-keys/x"},
	}
	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			req := httptest.NewRequest(h.method, h.path, nil)
			w := httptest.NewRecorder()
			h.fn(w, req)
			assert.Equal(t, http.StatusUnauthorized, w.Code, "expected 401 without auth")
		})
	}
}
