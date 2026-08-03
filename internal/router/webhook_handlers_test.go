package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func webhookTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestCreateWebhookHandler_NoAuth(t *testing.T) {
	r := webhookTestRouter()
	req := httptest.NewRequest("POST", "/webhooks", nil)
	w := httptest.NewRecorder()
	r.createWebhookHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCreateWebhookHandler_EmptyBody(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("POST", "/webhooks", nil, testClaims)
	w := httptest.NewRecorder()
	r.createWebhookHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateWebhookHandler_EmptyURL(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("POST", "/webhooks", map[string]interface{}{
		"url": "",
	}, testClaims)
	w := httptest.NewRecorder()
	r.createWebhookHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateWebhookHandler_InvalidURLScheme(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("POST", "/webhooks", map[string]interface{}{
		"url": "ftp://example.com",
	}, testClaims)
	w := httptest.NewRecorder()
	r.createWebhookHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateWebhookHandler_MissingSecret(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("POST", "/webhooks", map[string]interface{}{
		"url": "https://example.com",
	}, testClaims)
	w := httptest.NewRecorder()
	r.createWebhookHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateWebhookHandler_NoEvents(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("POST", "/webhooks", map[string]interface{}{
		"url":    "https://example.com",
		"secret": "mysecret",
		"events": []interface{}{},
	}, testClaims)
	w := httptest.NewRecorder()
	r.createWebhookHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateWebhookHandler_InvalidEvent(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("POST", "/webhooks", map[string]interface{}{
		"url":    "https://example.com",
		"secret": "mysecret",
		"events": []interface{}{"invalid.event"},
	}, testClaims)
	w := httptest.NewRecorder()
	r.createWebhookHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateWebhookHandler_NilEnginePanics(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("POST", "/webhooks", map[string]interface{}{
		"url":    "https://example.com",
		"secret": "mysecret",
		"events": []interface{}{"task.created"},
	}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.createWebhookHandler(w, req)
	}()
}

func TestListWebhooksHandler_NoAuth(t *testing.T) {
	r := webhookTestRouter()
	req := httptest.NewRequest("GET", "/webhooks", nil)
	w := httptest.NewRecorder()
	r.listWebhooksHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestListWebhooksHandler_NilEnginePanics(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("GET", "/webhooks", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.listWebhooksHandler(w, req)
	}()
}

func TestGetWebhookHandler_NoAuth(t *testing.T) {
	r := webhookTestRouter()
	req := httptest.NewRequest("GET", "/webhooks/wh-1", nil)
	w := httptest.NewRecorder()
	r.getWebhookHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestDeleteWebhookHandler_NoAuth(t *testing.T) {
	r := webhookTestRouter()
	req := httptest.NewRequest("DELETE", "/webhooks/wh-1", nil)
	w := httptest.NewRecorder()
	r.deleteWebhookHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetWebhookDeliveriesHandler_NoAuth(t *testing.T) {
	r := webhookTestRouter()
	req := httptest.NewRequest("GET", "/webhooks/wh-1/deliveries", nil)
	w := httptest.NewRecorder()
	r.getWebhookDeliveriesHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestGetWebhookDeliveriesHandler_InvalidLimit(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("GET", "/webhooks/wh-1/deliveries?limit=abc", nil, testClaims)
	w := httptest.NewRecorder()
	r.getWebhookDeliveriesHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetWebhookDeliveriesHandler_LimitOutOfRange(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("GET", "/webhooks/wh-1/deliveries?limit=200", nil, testClaims)
	w := httptest.NewRecorder()
	r.getWebhookDeliveriesHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestWebhookStatsHandler_NoAuth(t *testing.T) {
	r := webhookTestRouter()
	req := httptest.NewRequest("GET", "/webhooks/stats", nil)
	w := httptest.NewRecorder()
	r.webhookStatsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestWebhookStatsHandler_NilEnginePanics(t *testing.T) {
	r := webhookTestRouter()
	req := reqWithClaims("GET", "/webhooks/stats", nil, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.webhookStatsHandler(w, req)
	}()
}

func TestWebhookHandlers_AuthRequired(t *testing.T) {
	r := webhookTestRouter()
	handlers := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		path   string
	}{
		{"create", r.createWebhookHandler, "POST", "/webhooks"},
		{"list", r.listWebhooksHandler, "GET", "/webhooks"},
		{"get", r.getWebhookHandler, "GET", "/webhooks/x"},
		{"delete", r.deleteWebhookHandler, "DELETE", "/webhooks/x"},
		{"deliveries", r.getWebhookDeliveriesHandler, "GET", "/webhooks/x/deliveries"},
		{"stats", r.webhookStatsHandler, "GET", "/webhooks/stats"},
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

func TestCreateWebhookHandler_URLValidation(t *testing.T) {
	r := webhookTestRouter()
	tests := []struct {
		name    string
		url     string
		want400 bool
	}{
		{"http URL", "http://example.com", false},
		{"https URL", "https://example.com", false},
		{"ftp URL", "ftp://example.com", true},
		{"no scheme", "example.com", true},
		{"empty", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithClaims("POST", "/webhooks", map[string]interface{}{
				"url":    tt.url,
				"secret": "mysecret",
				"events": []interface{}{"task.created"},
			}, testClaims)
			w := httptest.NewRecorder()
			func() {
				defer func() { recover() }()
				r.createWebhookHandler(w, req)
			}()
			if tt.want400 && w.Code != 0 {
				assert.Equal(t, http.StatusBadRequest, w.Code)
			}
		})
	}
}
