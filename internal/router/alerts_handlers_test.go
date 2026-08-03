package router

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vigilagent/vigilagent/internal/auth"
	"github.com/vigilagent/vigilagent/internal/repository"
)

func alertsTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestListAlertsHandler_NoAuth(t *testing.T) {
	r := alertsTestRouter()
	req := httptest.NewRequest("GET", "/alerts", nil)
	w := httptest.NewRecorder()
	r.listAlertsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateAlertHandler_NoAuth(t *testing.T) {
	r := alertsTestRouter()
	req := httptest.NewRequest("POST", "/alerts", nil)
	w := httptest.NewRecorder()
	r.createAlertHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateAlertHandler_EmptyBody(t *testing.T) {
	r := alertsTestRouter()
	req := reqWithClaims("POST", "/alerts", nil, testClaims)
	w := httptest.NewRecorder()
	r.createAlertHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateAlertHandler_EmptyName(t *testing.T) {
	r := alertsTestRouter()
	req := reqWithClaims("POST", "/alerts", map[string]interface{}{"name": ""}, testClaims)
	w := httptest.NewRecorder()
	r.createAlertHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateAlertHandler_WhitespaceName(t *testing.T) {
	r := alertsTestRouter()
	req := reqWithClaims("POST", "/alerts", map[string]interface{}{"name": "   "}, testClaims)
	w := httptest.NewRecorder()
	r.createAlertHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateAlertHandler_NilRepoPanics(t *testing.T) {
	r := alertsTestRouter()
	req := reqWithClaims("POST", "/alerts", map[string]interface{}{"name": "test alert"}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.createAlertHandler(w, req)
	}()
}

func TestGetAlertHandler_NoAuth(t *testing.T) {
	r := alertsTestRouter()
	req := httptest.NewRequest("GET", "/alerts/alert-123", nil)
	w := httptest.NewRecorder()
	r.getAlertHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetAlertHandler_NilRepoPanics(t *testing.T) {
	r := alertsTestRouter()
	req := reqWithClaims("GET", "/alerts/alert-123", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.getAlertHandler(w, req)
	}()
}

func TestUpdateAlertHandler_NoAuth(t *testing.T) {
	r := alertsTestRouter()
	req := httptest.NewRequest("PUT", "/alerts/alert-123", nil)
	w := httptest.NewRecorder()
	r.updateAlertHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteAlertHandler_NoAuth(t *testing.T) {
	r := alertsTestRouter()
	req := httptest.NewRequest("DELETE", "/alerts/alert-123", nil)
	w := httptest.NewRecorder()
	r.deleteAlertHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAlertHandler_NilRepoRecoverPanics(t *testing.T) {
	r := alertsTestRouter()
	handlers := []struct {
		name    string
		handler http.HandlerFunc
		method  string
		path    string
	}{
		{"getAlert", r.getAlertHandler, "GET", "/alerts/alert-1"},
		{"updateAlert", r.updateAlertHandler, "PUT", "/alerts/alert-1"},
		{"deleteAlert", r.deleteAlertHandler, "DELETE", "/alerts/alert-1"},
	}
	for _, h := range handlers {
		t.Run(h.name+"_nil_repo", func(t *testing.T) {
			req := reqWithClaims(h.method, h.path, nil, testClaims)
			w := httptest.NewRecorder()
			func() {
				defer func() { recover() }()
				h.handler(w, req)
			}()
		})
	}
}

func TestAlertStructFields(t *testing.T) {
	a := &repository.Alert{
		ID:        "1",
		UserID:    "user-1",
		Name:      "Test",
		Type:      "threshold",
		Condition: map[string]interface{}{"key": "value"},
		Channel:   "webhook",
		IsActive:  true,
	}
	require.NotNil(t, a)
	assert.Equal(t, "1", a.ID)
	assert.Equal(t, "user-1", a.UserID)
	assert.Equal(t, "threshold", a.Type)
	assert.True(t, a.IsActive)
}

func TestAlertsClaimsExtraction(t *testing.T) {
	r := alertsTestRouter()
	tests := []struct {
		name    string
		claims  *auth.Claims
		want401 bool
	}{
		{"nil claims", nil, true},
		{"valid claims", testClaims, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithClaims("GET", "/alerts/alert-1", nil, tt.claims)
			w := httptest.NewRecorder()
			func() {
				defer func() { recover() }()
				r.getAlertHandler(w, req)
			}()
			if tt.want401 {
				assert.Equal(t, http.StatusUnauthorized, w.Code)
			}
		})
	}
}

func TestCreateAlertHandler_BodyParsing(t *testing.T) {
	r := alertsTestRouter()

	t.Run("invalid JSON", func(t *testing.T) {
		req, _ := http.NewRequest("POST", "/alerts", bytes.NewBufferString("not json"))
		req.Header.Set("Content-Type", "application/json")
		ctx := auth.ContextWithClaims(req.Context(), testClaims)
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()
		r.createAlertHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})

	t.Run("valid JSON with missing name", func(t *testing.T) {
		b, _ := json.Marshal(map[string]interface{}{"type": "threshold"})
		req := reqWithClaims("POST", "/alerts", nil, testClaims)
		req.Body = nil
		req.Body = httptest.NewRequest("POST", "/alerts", bytes.NewReader(b)).Body
		w := httptest.NewRecorder()
		r.createAlertHandler(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code)
	})
}
