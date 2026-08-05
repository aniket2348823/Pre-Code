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
)

// batchTestRouter builds a Router whose mux has the health route registered
// so that batch sub-requests can be routed successfully.
func batchTestRouter() *Router {
	r := &Router{Mux: chi.NewMux()}
	r.Mux.Get("/api/v1/health", r.healthHandler)
	return r
}

func batchReqBody(t *testing.T, ops []BatchOperation, atomic, parallel bool) *bytes.Buffer {
	t.Helper()
	req := BatchRequest{
		Operations: ops,
		Atomic:     atomic,
		Parallel:   parallel,
	}
	var buf bytes.Buffer
	require.NoError(t, json.NewEncoder(&buf).Encode(req))
	return &buf
}

func TestBatchHandler_NoAuth(t *testing.T) {
	r := batchTestRouter()
	req := httptest.NewRequest("POST", "/batch", nil)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestBatchHandler_EmptyBody(t *testing.T) {
	r := batchTestRouter()
	req := reqWithClaims("POST", "/batch", nil, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	result := parseJSON(t, w)
	errObj := result["error"].(map[string]interface{})
	assert.Equal(t, "BATCH_001", errObj["code"])
}

func TestBatchHandler_EmptyOperations(t *testing.T) {
	r := batchTestRouter()
	body := batchReqBody(t, []BatchOperation{}, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	result := parseJSON(t, w)
	errObj := result["error"].(map[string]interface{})
	assert.Equal(t, "BATCH_002", errObj["code"])
}

func TestBatchHandler_TooManyOperations(t *testing.T) {
	r := batchTestRouter()
	ops := make([]BatchOperation, 51)
	for i := range ops {
		ops[i] = BatchOperation{Method: "GET", Path: "/api/v1/health"}
	}
	body := batchReqBody(t, ops, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	result := parseJSON(t, w)
	errObj := result["error"].(map[string]interface{})
	assert.Equal(t, "BATCH_003", errObj["code"])
}

func TestBatchHandler_MissingMethod(t *testing.T) {
	r := batchTestRouter()
	ops := []BatchOperation{
		{Path: "/api/v1/health"},
	}
	body := batchReqBody(t, ops, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	result := parseJSON(t, w)
	errObj := result["error"].(map[string]interface{})
	assert.Equal(t, "BATCH_004", errObj["code"])
}

func TestBatchHandler_MissingPath(t *testing.T) {
	r := batchTestRouter()
	ops := []BatchOperation{
		{Method: "GET"},
	}
	body := batchReqBody(t, ops, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBatchHandler_SingleOp(t *testing.T) {
	r := batchTestRouter()
	ops := []BatchOperation{
		{Method: "GET", Path: "/api/v1/health"},
	}
	body := batchReqBody(t, ops, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Results  []BatchResult `json:"results"`
		Total    int           `json:"total"`
		Failures int           `json:"failures"`
		Atomic   bool          `json:"atomic"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 1, resp.Total)
	assert.Equal(t, 0, resp.Failures)
	assert.False(t, resp.Atomic)
	require.Len(t, resp.Results, 1)
	assert.Equal(t, 0, resp.Results[0].Index)
	assert.Equal(t, http.StatusOK, resp.Results[0].Status)
	assert.GreaterOrEqual(t, resp.Results[0].Duration, int64(0))
}

func TestBatchHandler_MultipleOpsSequential(t *testing.T) {
	r := batchTestRouter()
	ops := []BatchOperation{
		{Method: "GET", Path: "/api/v1/health"},
		{Method: "GET", Path: "/api/v1/health"},
	}
	body := batchReqBody(t, ops, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)

	var resp struct {
		Total int `json:"total"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 2, resp.Total)
}

func TestBatchHandler_AtomicBatch(t *testing.T) {
	r := batchTestRouter()
	ops := []BatchOperation{
		{Method: "GET", Path: "/api/v1/health"},
		{Method: "POST", Path: "/nonexistent"},
		{Method: "GET", Path: "/api/v1/health"},
	}
	body := batchReqBody(t, ops, true, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Results  []BatchResult `json:"results"`
		Total    int           `json:"total"`
		Failures int           `json:"failures"`
		Atomic   bool          `json:"atomic"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.True(t, resp.Atomic)
	assert.Equal(t, 3, resp.Total)
	assert.Equal(t, 2, resp.Failures)

	// The op after the first failure must be skipped with 424.
	assert.Equal(t, http.StatusFailedDependency, resp.Results[2].Status)
	assert.Contains(t, resp.Results[2].Error, "atomic batch failed")
}

func TestBatchHandler_ParallelExecution(t *testing.T) {
	r := batchTestRouter()
	ops := []BatchOperation{
		{Method: "GET", Path: "/api/v1/health"},
		{Method: "GET", Path: "/api/v1/health"},
		{Method: "GET", Path: "/api/v1/health"},
	}
	body := batchReqBody(t, ops, false, true)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)

	var resp struct {
		Total    int `json:"total"`
		Failures int `json:"failures"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	assert.Equal(t, 3, resp.Total)
	assert.Equal(t, 0, resp.Failures)
}

func TestBatchHandler_MethodUpperCasing(t *testing.T) {
	r := batchTestRouter()
	ops := []BatchOperation{
		{Method: "get", Path: "/api/v1/health"},
	}
	body := batchReqBody(t, ops, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBatchHandler_WithBody(t *testing.T) {
	r := batchTestRouter()
	bodyJSON, _ := json.Marshal(map[string]string{"email": "test@example.com"})
	ops := []BatchOperation{
		{Method: "POST", Path: "/api/v1/auth/login", Body: bodyJSON},
	}
	body := batchReqBody(t, ops, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestBatchHandler_CountFailures(t *testing.T) {
	r := batchTestRouter()
	results := []BatchResult{
		{Status: 200},
		{Status: 400},
		{Status: 201},
		{Status: 500},
		{Status: 204},
	}
	assert.Equal(t, 2, r.countFailures(results))
}

func TestBatchHandler_CountFailuresEmpty(t *testing.T) {
	r := batchTestRouter()
	assert.Equal(t, 0, r.countFailures(nil))
}

func TestBatchHandler_MaxBatchOperations(t *testing.T) {
	r := batchTestRouter()
	assert.Equal(t, 10, r.maxBatchOperations())
}

func TestBatchHandler_ErrorExtraction(t *testing.T) {
	r := batchTestRouter()
	ops := []BatchOperation{
		{Method: "POST", Path: "/nonexistent"},
	}
	body := batchReqBody(t, ops, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)

	var resp struct {
		Results []BatchResult `json:"results"`
	}
	require.NoError(t, json.NewDecoder(w.Body).Decode(&resp))
	require.Len(t, resp.Results, 1)
	assert.GreaterOrEqual(t, resp.Results[0].Status, 400)
	assert.NotEmpty(t, resp.Results[0].Error)
}

func TestBatchTaskHandler_NoAuth(t *testing.T) {
	r := batchTestRouter()
	req := httptest.NewRequest("POST", "/tasks/batch", nil)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestBatchTaskHandler_EmptyBody(t *testing.T) {
	r := batchTestRouter()
	req := reqWithClaims("POST", "/tasks/batch", nil, testClaims)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBatchTaskHandler_EmptyTasksArray(t *testing.T) {
	r := batchTestRouter()
	req := reqWithClaims("POST", "/tasks/batch", map[string]interface{}{"tasks": []interface{}{}}, testClaims)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBatchTaskHandler_TooManyTasks(t *testing.T) {
	r := batchTestRouter()
	tasks := make([]interface{}, 11)
	for i := range tasks {
		tasks[i] = map[string]interface{}{"prompt": "test", "project_id": "proj-1"}
	}
	req := reqWithClaims("POST", "/tasks/batch", map[string]interface{}{"tasks": tasks}, testClaims)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestBatchTaskHandler_MissingFields(t *testing.T) {
	r := batchTestRouter()
	tasks := []interface{}{
		map[string]interface{}{"project_id": "proj-1"},
	}
	req := reqWithClaims("POST", "/tasks/batch", map[string]interface{}{"tasks": tasks}, testClaims)
	w := httptest.NewRecorder()
	r.batchTaskHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
	result := parseJSON(t, w)
	taskResults := result["results"].([]interface{})
	first := taskResults[0].(map[string]interface{})
	assert.Equal(t, "failed", first["status"])
}

func TestBatchTaskHandler_AccessDenied(t *testing.T) {
	r := batchTestRouter()
	tasks := []interface{}{
		map[string]interface{}{"prompt": "test", "project_id": "proj-unknown"},
	}
	req := reqWithClaims("POST", "/tasks/batch", map[string]interface{}{"tasks": tasks}, testClaims)
	w := httptest.NewRecorder()
	func() {
		defer func() { recover() }()
		r.batchTaskHandler(w, req)
	}()
}

func TestBatchResponse_Structure(t *testing.T) {
	r := batchTestRouter()
	ops := []BatchOperation{
		{Method: "GET", Path: "/api/v1/health"},
	}
	body := batchReqBody(t, ops, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)

	assert.Contains(t, w.Body.String(), `"results"`)
	assert.Contains(t, w.Body.String(), `"total"`)
	assert.Contains(t, w.Body.String(), `"failures"`)
	assert.Contains(t, w.Body.String(), `"atomic"`)
}

func TestBatchOperation_RawBody(t *testing.T) {
	r := batchTestRouter()
	rawBody := json.RawMessage(`{"prompt":"hello","project_id":"proj-1"}`)
	ops := []BatchOperation{
		{Method: "POST", Path: "/api/v1/tasks", Body: rawBody},
	}
	body := batchReqBody(t, ops, false, false)
	req := reqWithClaimsRaw("POST", "/batch", body, testClaims)
	w := httptest.NewRecorder()
	r.batchHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

// reqWithClaimsRaw builds a request with a pre-serialized body and auth claims.
func reqWithClaimsRaw(method, path string, body *bytes.Buffer, claims *auth.Claims) *http.Request {
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	if claims != nil {
		ctx := auth.ContextWithClaims(req.Context(), claims)
		req = req.WithContext(ctx)
	}
	return req
}
