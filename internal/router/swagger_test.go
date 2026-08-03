package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func swaggerTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestSwaggerUIHandler(t *testing.T) {
	r := swaggerTestRouter()
	req := httptest.NewRequest("GET", "/docs", nil)
	w := httptest.NewRecorder()
	r.swaggerUIHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/html")
	body := w.Body.String()
	assert.Contains(t, body, "SwaggerUIBundle")
	assert.Contains(t, body, "VigilAgent API Documentation")
	assert.Contains(t, body, "/api/v1/docs/openapi.yaml")
}

func TestOpenAPISpecHandler(t *testing.T) {
	r := swaggerTestRouter()
	req := httptest.NewRequest("GET", "/docs/openapi.yaml", nil)
	w := httptest.NewRecorder()
	r.openapiSpecHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/yaml")
}
