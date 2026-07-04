package router

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vigilagent/vigilagent/internal/config"
	"github.com/vigilagent/vigilagent/internal/cors"
	"github.com/vigilagent/vigilagent/internal/idempotency"
	"github.com/vigilagent/vigilagent/internal/requestid"
)

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
