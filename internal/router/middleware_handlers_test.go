package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func middlewareTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestMiddlewareProcessHandler_NoAuth(t *testing.T) {
	r := middlewareTestRouter()
	req := httptest.NewRequest("POST", "/middleware/process", nil)
	w := httptest.NewRecorder()
	r.middlewareProcessHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestMiddlewareProcessHandler_EmptyBody(t *testing.T) {
	r := middlewareTestRouter()
	req := reqWithClaims("POST", "/middleware/process", nil, testClaims)
	w := httptest.NewRecorder()
	r.middlewareProcessHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMiddlewareProcessHandler_MissingDescription(t *testing.T) {
	r := middlewareTestRouter()
	req := reqWithClaims("POST", "/middleware/process", map[string]interface{}{
		"task_type": "security_review",
	}, testClaims)
	w := httptest.NewRecorder()
	r.middlewareProcessHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMiddlewareProcessHandler_WithDescription(t *testing.T) {
	r := middlewareTestRouter()
	req := reqWithClaims("POST", "/middleware/process", map[string]interface{}{
		"description": "review this code",
		"task_type":   "security_review",
	}, testClaims)
	w := httptest.NewRecorder()
	r.middlewareProcessHandler(w, req)
	// With nil deps (engine, pipeline, skillEng), should still succeed with empty results
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddlewareProcessHandler_WithCode(t *testing.T) {
	r := middlewareTestRouter()
	req := reqWithClaims("POST", "/middleware/process", map[string]interface{}{
		"description": "review code",
		"code":        `fmt.Println("hello")`,
		"language":    "go",
	}, testClaims)
	w := httptest.NewRecorder()
	r.middlewareProcessHandler(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestMiddlewareProcessHandler_SSEStreamMode(t *testing.T) {
	r := middlewareTestRouter()
	req := reqWithClaims("POST", "/middleware/process", map[string]interface{}{
		"description": "test",
		"stream":      true,
	}, testClaims)
	req.Header.Set("Accept", "text/event-stream")
	w := httptest.NewRecorder()
	r.middlewareProcessHandler(w, req)
	// Streaming mode
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestFindingCount_NilScanResult(t *testing.T) {
	m := &middlewareResult{}
	assert.Equal(t, 0, m.findingCount())
}



func TestPipelineRequest_Fields(t *testing.T) {
	pr := &pipelineRequest{
		Description: "test desc",
		Code:        "test code",
		Language:    "go",
		Filename:    "main.go",
	}
	assert.Equal(t, "test desc", pr.Description)
	assert.Equal(t, "test code", pr.Code)
	assert.Equal(t, "go", pr.Language)
	assert.Equal(t, "main.go", pr.Filename)
}

func TestMiddlewareInput_Fields(t *testing.T) {
	mi := &middlewareInput{
		TaskType:    "security",
		Description: "desc",
		Code:        "code",
		Language:    "go",
		Filename:    "f.go",
		Budget:      5.0,
	}
	assert.Equal(t, "security", mi.TaskType)
	assert.Equal(t, "desc", mi.Description)
	assert.Equal(t, "code", mi.Code)
	assert.Equal(t, "go", mi.Language)
	assert.Equal(t, 5.0, mi.Budget)
}

func TestPipelineReport_Fields(t *testing.T) {
	pr := &pipelineReport{
		Passed:     true,
		Confidence: 0.85,
		Layers: []layer{
			{Name: "requirements", Passed: true},
			{Name: "compliance", Passed: false},
		},
	}
	assert.True(t, pr.Passed)
	assert.Equal(t, 0.85, pr.Confidence)
	assert.Len(t, pr.Layers, 2)
}

func TestMiddlewareResult_Fields(t *testing.T) {
	mr := &middlewareResult{
		Description: "test",
		TaskType:    "security",
		Metrics: map[string]interface{}{
			"findings_count": 0,
		},
	}
	assert.Equal(t, "test", mr.Description)
	assert.Equal(t, "security", mr.TaskType)
	assert.Equal(t, 0, mr.findingCount())
}

func TestContextInput_Fields(t *testing.T) {
	ci := &contextInput{
		Files: []fileInput{
			{Path: "main.go", Content: "package main"},
		},
		Language: "go",
	}
	assert.Len(t, ci.Files, 1)
	assert.Equal(t, "main.go", ci.Files[0].Path)
}

func TestRunPipeline_NilPipeline(t *testing.T) {
	r := middlewareTestRouter()
	// pipeline is nil
	result := r.runPipeline(nil, &pipelineRequest{Description: "test"})
	assert.Nil(t, result)
}
