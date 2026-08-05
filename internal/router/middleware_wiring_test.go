package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/cors"
	"github.com/vigilagent/vigilagent/internal/idempotency"
	"github.com/vigilagent/vigilagent/internal/requestid"
)

// Content from middleware_wiring_test.go
// mwDummyHandler is a simple handler that returns 200 OK.
func mwDummyHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("ok"))
	})
}

// newMwRouter creates a bare Router for testing individual middleware setup methods.
// chi requires middleware to be added before routes, so we create a fresh mux.
func newMwRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestSetupSecurityMiddleware_NilConfig(t *testing.T) {
	r := newMwRouter()
	// Should not panic with nil config.
	r.setupSecurityMiddleware(nil)

	r.Handle("/test", mwDummyHandler())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestSetupSecurityMiddleware_CORS(t *testing.T) {
	r := newMwRouter()
	cfg := &MiddlewareConfig{
		CORS: &cors.Config{
			AllowOrigins: []string{"https://example.com"},
			AllowMethods: []string{"GET", "POST"},
			AllowHeaders: []string{"Content-Type"},
			MaxAge:       3600,
		},
	}
	r.setupSecurityMiddleware(cfg)

	r.Handle("/test", mwDummyHandler())

	// Preflight request.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("OPTIONS", "/test", nil)
	req.Header.Set("Origin", "https://example.com")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for preflight, got %d", rec.Code)
	}
	origin := rec.Header().Get("Access-Control-Allow-Origin")
	if origin != "https://example.com" {
		t.Fatalf("expected origin https://example.com, got %s", origin)
	}
}

func TestSetupResilienceMiddleware_Timeout(t *testing.T) {
	r := newMwRouter()
	cfg := &MiddlewareConfig{
		Timeout: 1 * time.Second,
	}
	r.setupResilienceMiddleware(cfg)

	r.Handle("/test", mwDummyHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestSetupObservabilityMiddleware_RequestID(t *testing.T) {
	r := newMwRouter()
	cfg := &MiddlewareConfig{
		RequestID: true,
	}
	r.setupObservabilityMiddleware(cfg)

	r.Handle("/test", mwDummyHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-Id", "test-id-123")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	rid := rec.Header().Get("X-Request-Id")
	if rid != "test-id-123" {
		t.Fatalf("expected X-Request-Id test-id-123, got %s", rid)
	}
}

func TestSetupObservabilityMiddleware_GeneratesID(t *testing.T) {
	r := newMwRouter()
	cfg := &MiddlewareConfig{
		RequestID: true,
	}
	r.setupObservabilityMiddleware(cfg)

	r.Handle("/test", mwDummyHandler())

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(rec, req)

	rid := rec.Header().Get("X-Request-Id")
	if rid == "" {
		t.Fatal("expected auto-generated X-Request-Id")
	}
	if len(rid) != 32 {
		t.Fatalf("expected 32-char hex request ID, got %q (len=%d)", rid, len(rid))
	}
}

func TestMiddlewareConfig_AllFields(t *testing.T) {
	idempotencyStore := idempotency.NewStore(idempotency.Config{})
	cfg := &MiddlewareConfig{
		RequestID:   true,
		Timeout:     5 * time.Second,
		CORS:        &cors.Config{AllowOrigins: []string{"*"}},
		Idempotency: idempotencyStore,
	}

	if !cfg.RequestID {
		t.Error("expected RequestID true")
	}
	if cfg.Timeout != 5*time.Second {
		t.Error("expected Timeout 5s")
	}
	if cfg.CORS == nil {
		t.Error("expected non-nil CORS")
	}
	if cfg.Idempotency == nil {
		t.Error("expected non-nil Idempotency store")
	}
}

func TestMiddlewareChaining(t *testing.T) {
	// Verify that multiple middleware layers compose correctly.
	r := newMwRouter()

	cfg := &MiddlewareConfig{
		RequestID: true,
		Timeout:   2 * time.Second,
		CORS: &cors.Config{
			AllowOrigins: []string{"*"},
			AllowMethods: []string{"GET"},
			AllowHeaders: []string{"Content-Type"},
		},
	}

	r.setupSecurityMiddleware(cfg)
	r.setupResilienceMiddleware(cfg)
	r.setupObservabilityMiddleware(cfg)

	r.Handle("/test", mwDummyHandler())

	// Regular GET request.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Should have request ID from requestid middleware.
	rid := rec.Header().Get("X-Request-Id")
	if rid == "" {
		t.Fatal("expected X-Request-Id header")
	}

	// Should have CORS header from cors middleware.
	origin := rec.Header().Get("Access-Control-Allow-Origin")
	if origin != "*" {
		t.Fatalf("expected Access-Control-Allow-Origin *, got %s", origin)
	}
}

// Verify that requestid.FromContext works through the middleware chain.
func TestMiddlewareChain_ContextRequestID(t *testing.T) {
	r := newMwRouter()

	cfg := &MiddlewareConfig{
		RequestID: true,
	}
	r.setupObservabilityMiddleware(cfg)

	var capturedID string
	r.Handle("/test", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedID = requestid.FromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-Request-Id", "ctx-test-id")
	r.ServeHTTP(rec, req)

	if capturedID != "ctx-test-id" {
		t.Fatalf("expected FromContext to return ctx-test-id, got %q", capturedID)
	}
}

// TestNewWithMiddleware verifies the full-stack constructor wires all middleware.
func TestNewWithMiddleware(t *testing.T) {
	cfg := &config.Config{}
	cfg.CORS.AllowedOrigins = []string{"https://app.example.com"}
	opts := Options{Config: cfg}
	mcfg := &MiddlewareConfig{
		RequestID: true,
		Timeout:   5 * time.Second,
		CORS: &cors.Config{
			AllowOrigins: []string{"https://app.example.com"},
			AllowMethods: []string{"GET", "POST"},
			AllowHeaders: []string{"Content-Type", "Authorization"},
			MaxAge:       86400,
		},
	}

	r := NewWithMiddleware(opts, mcfg)

	// Use /ping to avoid conflict with /health registered by setupRoutes.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/v1/health", nil)
	req.Header.Set("Origin", "https://app.example.com")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Request ID should be auto-generated.
	rid := rec.Header().Get("X-Request-Id")
	if rid == "" {
		t.Fatal("expected auto-generated X-Request-Id from NewWithMiddleware")
	}

	// CORS should reflect configured origin.
	origin := rec.Header().Get("Access-Control-Allow-Origin")
	if origin != "https://app.example.com" {
		t.Fatalf("expected CORS origin https://app.example.com, got %s", origin)
	}
}

// TestUseCORSFromConfig_NilGuard verifies no panic when r.cfg is nil.
func TestUseCORSFromConfig_NilGuard(t *testing.T) {
	// Validates no nil-pointer dereference when r.cfg is nil.
	r := newMwRouter()
	r.useCORSFromConfig() // should fall back to cors.DefaultConfig()

	r.Handle("/test", mwDummyHandler())

	// Test 1: Request with Origin header — should echo origin back.
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("Origin", "https://any.com")
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with nil cfg, got %d", rec.Code)
	}
	// DefaultConfig has AllowOrigins=["*"], so isOriginAllowed returns true
	// for any origin. The middleware echoes the request origin back.
	origin := rec.Header().Get("Access-Control-Allow-Origin")
	if origin != "https://any.com" {
		t.Fatalf("expected echoed origin https://any.com, got %s", origin)
	}

	// Test 2: Request without Origin header — should not panic or crash.
	rec2 := httptest.NewRecorder()
	req2 := httptest.NewRequest("GET", "/test", nil)
	r.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 without Origin header, got %d", rec2.Code)
	}
}

// Content from middleware_handlers_test.go
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
