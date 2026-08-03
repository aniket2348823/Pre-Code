package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func extendedTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestMiddlewareMetricsHandler_NoAuth(t *testing.T) {
	r := extendedTestRouter()
	req := httptest.NewRequest("GET", "/middleware/metrics", nil)
	w := httptest.NewRecorder()
	r.middlewareMetricsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddlewareMetricsHandler_NilDeps(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("GET", "/middleware/metrics", nil, testClaims)
	w := httptest.NewRecorder()
	r.middlewareMetricsHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	result := parseJSON(t, w)
	assert.Contains(t, result, "total_records")
}

func TestMiddlewarePatternsHandler_NoAuth(t *testing.T) {
	r := extendedTestRouter()
	req := httptest.NewRequest("GET", "/middleware/patterns", nil)
	w := httptest.NewRecorder()
	r.middlewarePatternsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddlewarePatternsHandler_NilDeps(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("GET", "/middleware/patterns", nil, testClaims)
	w := httptest.NewRecorder()
	r.middlewarePatternsHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBatchTaskHandler_NoAuth(t *testing.T) {
	r := extendedTestRouter()
	req := httptest.NewRequest("POST", "/tasks/batch", nil)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestBatchTaskHandler_EmptyBody(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("POST", "/tasks/batch", nil, testClaims)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBatchTaskHandler_EmptyTasksArray(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("POST", "/tasks/batch", map[string]interface{}{"tasks": []interface{}{}}, testClaims)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBatchTaskHandler_TooManyTasks(t *testing.T) {
	r := extendedTestRouter()
	tasks := make([]interface{}, 11)
	for i := range tasks {
		tasks[i] = map[string]interface{}{"prompt": "test", "project_id": "proj-1"}
	}
	req := reqWithClaims("POST", "/tasks/batch", map[string]interface{}{"tasks": tasks}, testClaims)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBatchTaskHandler_MissingPromptInTask(t *testing.T) {
	r := extendedTestRouter()
	tasks := []interface{}{
		map[string]interface{}{"project_id": "proj-1"},
	}
	req := reqWithClaims("POST", "/tasks/batch", map[string]interface{}{"tasks": tasks}, testClaims)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
	result := parseJSON(t, w)
	taskResults := result["tasks"].([]interface{})
	first := taskResults[0].(map[string]interface{})
	assert.Equal(t, "failed", first["status"])
}

func TestBatchTaskHandler_MissingProjectID(t *testing.T) {
	r := extendedTestRouter()
	tasks := []interface{}{
		map[string]interface{}{"prompt": "test"},
	}
	req := reqWithClaims("POST", "/tasks/batch", map[string]interface{}{"tasks": tasks}, testClaims)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusCreated, w.Code)
	result := parseJSON(t, w)
	taskResults := result["tasks"].([]interface{})
	first := taskResults[0].(map[string]interface{})
	assert.Equal(t, "failed", first["status"])
}

func TestBatchTaskHandler_NilRepoPanics(t *testing.T) {
	r := extendedTestRouter()
	tasks := []interface{}{
		map[string]interface{}{"prompt": "test", "project_id": "proj-1"},
	}
	req := reqWithClaims("POST", "/tasks/batch", map[string]interface{}{"tasks": tasks}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.batchTaskHandler(w, req)
	}()
}

func TestHealthStatsHandler_NoAuth(t *testing.T) {
	r := extendedTestRouter()
	req := httptest.NewRequest("GET", "/providers/health", nil)
	w := httptest.NewRecorder()
	r.healthStatsHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestHealthStatsHandler_NilDeps(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("GET", "/providers/health", nil, testClaims)
	w := httptest.NewRecorder()
	r.healthStatsHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestCostOverrideHandler_NoAuth(t *testing.T) {
	r := extendedTestRouter()
	req := httptest.NewRequest("POST", "/providers/cost-override", nil)
	w := httptest.NewRecorder()
	r.costOverrideHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestCostOverrideHandler_EmptyBody(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("POST", "/providers/cost-override", nil, testClaims)
	w := httptest.NewRecorder()
	r.costOverrideHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCostOverrideHandler_MissingModel(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("POST", "/providers/cost-override", map[string]interface{}{}, testClaims)
	w := httptest.NewRecorder()
	r.costOverrideHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCostOverrideHandler_WithModel(t *testing.T) {
	r := extendedTestRouter()
	req := reqWithClaims("POST", "/providers/cost-override", map[string]interface{}{
		"model":              "custom-model",
		"input_cost_per_1k":  0.01,
		"output_cost_per_1k": 0.02,
	}, testClaims)
	w := httptest.NewRecorder()
	r.costOverrideHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	result := parseJSON(t, w)
	assert.Equal(t, "updated", result["status"])
}

func TestExtendedHandlers_AuthRequired(t *testing.T) {
	r := extendedTestRouter()
	handlers := []struct {
		name   string
		fn     http.HandlerFunc
		method string
		path   string
	}{
		{"metrics", r.middlewareMetricsHandler, "GET", "/middleware/metrics"},
		{"patterns", r.middlewarePatternsHandler, "GET", "/middleware/patterns"},
		{"batch", r.batchTaskHandler, "POST", "/tasks/batch"},
		{"health", r.healthStatsHandler, "GET", "/providers/health"},
		{"costOverride", r.costOverrideHandler, "POST", "/providers/cost-override"},
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
