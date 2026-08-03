package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
)

func optionsTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestNewRouter(t *testing.T) {
	opts := Options{}
	r := newRouter(opts)
	assert.NotNil(t, r)
	assert.NotNil(t, r.Mux)
}

func TestNewRouter_WithNilConfig(t *testing.T) {
	opts := Options{Config: nil}
	r := newRouter(opts)
	assert.NotNil(t, r)
	assert.Nil(t, r.cfg)
}

func TestDefaultWebSocketManagerConfig(t *testing.T) {
	cfg := DefaultWebSocketManagerConfig()
	assert.Equal(t, 5, cfg.MaxPerUser)
	assert.Equal(t, 1000, cfg.MaxGlobal)
	assert.Equal(t, int64(64*1024), cfg.MaxMessageSize)
	assert.Equal(t, int64(30*60*1e9), cfg.ConnTimeout.Nanoseconds())
}

func TestNewWebSocketManager(t *testing.T) {
	cfg := DefaultWebSocketManagerConfig()
	m := NewWebSocketManager(cfg)
	assert.NotNil(t, m)
	assert.Equal(t, 5, m.maxPerUser)
	assert.Equal(t, 1000, m.maxGlobal)
}

func TestNewWebSocketManager_DefaultsApplied(t *testing.T) {
	cfg := WebSocketManagerConfig{}
	m := NewWebSocketManager(cfg)
	assert.NotNil(t, m)
	assert.Equal(t, 5, m.maxPerUser)
	assert.Equal(t, 1000, m.maxGlobal)
	assert.Equal(t, int64(64*1024), m.maxMessageSize)
}

func TestShutdown_NilCancel(t *testing.T) {
	r := optionsTestRouter()
	// lockoutCancel is nil — should not panic
	r.Shutdown()
}

func TestShutdown_WithCancel(t *testing.T) {
	r := optionsTestRouter()
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.Shutdown()
	}()
	<-done
	assert.True(t, true) // no panic
}

func TestInitHandlers_NilEngine(t *testing.T) {
	r := optionsTestRouter()
	r.engine = nil
	r.initHandlers()
	assert.NotNil(t, r.engine)
}

func TestInitHandlers_NilRequirements(t *testing.T) {
	r := optionsTestRouter()
	r.requirements = nil
	r.initHandlers()
	assert.NotNil(t, r.requirementsHandlerFn)
}

func TestInitHandlers_NilValidator(t *testing.T) {
	r := optionsTestRouter()
	r.validator = nil
	r.initHandlers()
	assert.NotNil(t, r.schemaHandlerFn)
}

func TestInitHandlers_NilCompliance(t *testing.T) {
	r := optionsTestRouter()
	r.complianceChecker = nil
	r.initHandlers()
	assert.NotNil(t, r.complianceHandlerFn)
}

func TestInitHandlers_NilPipeline(t *testing.T) {
	r := optionsTestRouter()
	r.pipeline = nil
	r.initHandlers()
	assert.NotNil(t, r.pipeline)
	assert.NotNil(t, r.pipelineHandlerFn)
}

func TestInitHandlers_NilKnowledge(t *testing.T) {
	r := optionsTestRouter()
	r.knowledge = nil
	r.initHandlers()
	assert.NotNil(t, r.knowledgeHandlerFn)
}

func TestInitHandlers_NilSkillEng(t *testing.T) {
	r := optionsTestRouter()
	r.skillEng = nil
	r.initHandlers()
	assert.NotNil(t, r.skillEngineHandlerFn)
}

func TestInitHandlers_NilConfidence(t *testing.T) {
	r := optionsTestRouter()
	r.confidence = nil
	r.initHandlers()
	assert.NotNil(t, r.confidenceHandlerFn)
}

func TestInitHandlers_NilAttackGraph(t *testing.T) {
	r := optionsTestRouter()
	r.attackGraph = nil
	r.initHandlers()
	assert.NotNil(t, r.attackGraphHandlerFn)
}

func TestInitHandlers_NilAudit(t *testing.T) {
	r := optionsTestRouter()
	r.audit = nil
	r.initHandlers()
	assert.NotNil(t, r.auditHandlerFn)
}

func TestSecurityHeadersMiddleware(t *testing.T) {
	r := optionsTestRouter()
	inner := http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := r.securityHeadersMiddleware(inner)
	req := httptest.NewRequest("GET", "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, "nosniff", w.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", w.Header().Get("X-Frame-Options"))
	assert.Contains(t, w.Header().Get("X-XSS-Protection"), "1; mode=block")
	assert.Contains(t, w.Header().Get("Referrer-Policy"), "strict-origin-when-cross-origin")
	assert.Contains(t, w.Header().Get("Permissions-Policy"), "camera=()")
	assert.Contains(t, w.Header().Get("Content-Security-Policy"), "default-src 'none'")
}

func TestCorsAllExplicit(t *testing.T) {
	tests := []struct {
		name     string
		origins  []string
		expected bool
	}{
		{"empty", []string{}, false},
		{"wildcard", []string{"*"}, false},
		{"explicit", []string{"https://example.com"}, true},
		{"multiple explicit", []string{"https://a.com", "https://b.com"}, true},
		{"mixed", []string{"*", "https://a.com"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := corsAllExplicit(tt.origins)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestOptionsStruct(t *testing.T) {
	opts := Options{}
	assert.Nil(t, opts.Config)
	assert.Nil(t, opts.DB)
	assert.Nil(t, opts.Redis)
	assert.Nil(t, opts.JWT)
	assert.Nil(t, opts.Users)
	assert.Nil(t, opts.Orgs)
}
