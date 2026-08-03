package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/llm"
)

func providerTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestListProvidersHandler(t *testing.T) {
	r := providerTestRouter()
	req := httptest.NewRequest("GET", "/providers", nil)
	w := httptest.NewRecorder()
	r.listProvidersHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	result := parseJSON(t, w)
	providers, ok := result["providers"]
	assert.True(t, ok)
	providersList, ok := providers.([]interface{})
	assert.True(t, ok)
	assert.True(t, len(providersList) > 0)
}

func TestListProviderModelsHandler_EmptyID(t *testing.T) {
	r := providerTestRouter()
	req := httptest.NewRequest("GET", "/providers/", nil)
	w := httptest.NewRecorder()
	r.listProviderModelsHandler(w, req)
	// PathValue returns "" without chi router → handler returns 400
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestListProviderModelsHandler_NoRouteParams(t *testing.T) {
	r := providerTestRouter()
	// Without chi route context, PathValue returns empty → 400
	req := httptest.NewRequest("GET", "/providers/openai/models", nil)
	w := httptest.NewRecorder()
	r.listProviderModelsHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetModelHandler_EmptyID(t *testing.T) {
	r := providerTestRouter()
	req := httptest.NewRequest("GET", "/models/", nil)
	w := httptest.NewRecorder()
	r.getModelHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGetModelHandler_NoRouteParams(t *testing.T) {
	r := providerTestRouter()
	// Without chi route context, PathValue returns empty → 400
	req := httptest.NewRequest("GET", "/models/gpt-4o", nil)
	w := httptest.NewRecorder()
	r.getModelHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestProviderHandlers_PublicEndpoints(t *testing.T) {
	r := providerTestRouter()
	handlers := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		path   string
	}{
		{"listProviders", r.listProvidersHandler, "GET", "/providers"},
	}
	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			req := httptest.NewRequest(h.method, h.path, nil)
			w := httptest.NewRecorder()
			h.fn(w, req)
			assert.Equal(t, http.StatusOK, w.Code)
		})
	}
}

func TestProviderModelsCount(t *testing.T) {
	models := llm.ProviderModels(llm.ProviderID("openai"))
	assert.True(t, len(models) > 0, "openai should have models")
	for _, m := range models {
		assert.NotEmpty(t, m.ID)
		assert.NotEmpty(t, m.Name)
	}
}

func TestFindModel(t *testing.T) {
	models := llm.ProviderModels(llm.ProviderID("openai"))
	if len(models) == 0 {
		t.Skip("no openai models")
	}
	m := llm.FindModel(models[0].ID)
	assert.NotNil(t, m)
	assert.Equal(t, models[0].ID, m.ID)
}

func TestFindModel_NotFound(t *testing.T) {
	m := llm.FindModel("totally-fake-model-xyz")
	assert.Nil(t, m)
}
