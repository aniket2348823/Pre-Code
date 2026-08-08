package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/vigilagent/vigilagent/internal/llm"
)

func reviewTestRouter() *Router {
	return &Router{Mux: chi.NewMux()}
}

func TestReviewHandler_NoAuth(t *testing.T) {
	r := reviewTestRouter()
	req := httptest.NewRequest("POST", "/review", nil)
	w := httptest.NewRecorder()
	r.reviewHandler(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestReviewHandler_EmptyBody(t *testing.T) {
	r := reviewTestRouter()
	req := reqWithClaims("POST", "/review", nil, testClaims)
	w := httptest.NewRecorder()
	r.reviewHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReviewHandler_MissingPromptAndCode(t *testing.T) {
	r := reviewTestRouter()
	req := reqWithClaims("POST", "/review", map[string]interface{}{}, testClaims)
	w := httptest.NewRecorder()
	r.reviewHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReviewHandler_UnsupportedLanguage(t *testing.T) {
	r := reviewTestRouter()
	req := reqWithClaims("POST", "/review", map[string]interface{}{
		"prompt":   "test",
		"language": "cobol",
	}, testClaims)
	w := httptest.NewRecorder()
	r.reviewHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReviewHandler_PathTraversalInFilename(t *testing.T) {
	r := reviewTestRouter()
	req := reqWithClaims("POST", "/review", map[string]interface{}{
		"prompt":   "test",
		"filename": "../etc/passwd",
	}, testClaims)
	w := httptest.NewRecorder()
	r.reviewHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReviewHandler_BackslashInFilename(t *testing.T) {
	r := reviewTestRouter()
	req := reqWithClaims("POST", "/review", map[string]interface{}{
		"prompt":   "test",
		"filename": "C:\\windows\\system32",
	}, testClaims)
	w := httptest.NewRecorder()
	r.reviewHandler(w, req)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestReviewHandler_NilLLMRouter(t *testing.T) {
	r := reviewTestRouter()
	req := reqWithClaims("POST", "/review", map[string]interface{}{
		"prompt": "test prompt",
	}, testClaims)
	w := httptest.NewRecorder()
	r.reviewHandler(w, req)
	// llmRouter is nil → 503
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestDefaultModelForProvider(t *testing.T) {
	tests := []struct {
		provider string
		expected string
	}{
		{"openai", "gpt-4o-mini"},
		{"anthropic", "claude-haiku-3.5"},
		{"nvidia_nim", "meta/llama-3.1-8b-instruct"},
		{"gemini", "gemini-2.0-flash"},
		{"mistral", "mistral-small-latest"},
		{"groq", "llama-3.1-8b-instant"},
		{"cohere", "command-r"},
		{"unknown", "gpt-4o-mini"},
	}
	for _, tt := range tests {
		t.Run(tt.provider, func(t *testing.T) {
			got := defaultModelForProvider(tt.provider)
			assert.Equal(t, tt.expected, got)
			// The model must exist in the price table AND map to the same provider,
			// otherwise rankCandidates would still find zero candidates. The
			// "unknown" fallback is intentionally an OpenAI model (documented
			// behavior — the key prefix was unrecognized), so skip the provider
			// match there.
			info, ok := llm.LookupPrice(got)
			if !ok {
				t.Fatalf("default model %q not in price table", got)
			}
			if tt.provider != "unknown" {
				assert.Equal(t, tt.provider, info.Provider, "default model must belong to its provider")
			}
		})
	}
}

func TestBuildBYOKRouter_UsesBackendDefaultForNVIDIA(t *testing.T) {
	// Backend router is configured with the NVIDIA small model (what the live
	// deployment uses). A BYOK NVIDIA key must inherit that default so routing
	// finds a candidate — this is the regression that broke prompt-only review
	// with "no healthy provider supports the task's requirements".
	backend := llm.NewModelRouter(&llm.RouterConfig{DefaultModel: "meta/llama-3.1-8b-instruct"})
	backend.RegisterProvider("nvidia_nim", llm.NewNVIDIANIM("nvapi-test-key"))
	r := &Router{llmRouter: backend}

	byok := r.buildBYOKRouter("nvapi-test-key")
	assert.Equal(t, "meta/llama-3.1-8b-instruct", byok.DefaultModel())

	// Routing a pipeline-style task (Type=architecture + security tag, no
	// TargetModel) must succeed — it needs a candidate whose provider is the
	// registered nvidia_nim.
	decision, err := byok.Route(t.Context(), &llm.Task{
		ID:          "shift-zero-main",
		Type:        "architecture",
		Description: "main llm",
		Tags:        []string{"architecture", "security"},
	})
	if err != nil {
		t.Fatalf("BYOK NVIDIA router should route pipeline task: %v", err)
	}
	assert.Equal(t, "nvidia_nim", decision.Provider)
	assert.Equal(t, "meta/llama-3.1-8b-instruct", decision.Model)
}

func TestBuildBYOKRouter_OpenAIKeyGetsOpenAIModel(t *testing.T) {
	r := reviewTestRouter()
	byok := r.buildBYOKRouter("sk-test-key")
	assert.Equal(t, "gpt-4o-mini", byok.DefaultModel())

	decision, err := byok.Route(t.Context(), &llm.Task{
		ID:   "test",
		Type: "architecture",
		Tags: []string{"security"},
	})
	if err != nil {
		t.Fatalf("BYOK OpenAI router should route pipeline task: %v", err)
	}
	assert.Equal(t, "openai", decision.Provider)
}

func TestDetectProviderFromKey(t *testing.T) {
	tests := []struct {
		key      string
		expected string
	}{
		{"sk-ant-abc123", "anthropic"},
		{"sk-or-abc123", "openrouter"},
		{"nvapi-abc123", "nvidia_nim"},
		{"AIzaSyA123", "gemini"},
		{"ms-abc123", "mistral"},
		{"gsk_abc123", "groq"},
		{"co-abc123", "cohere"},
		{"sk-abc123", "openai"},
		{"unknown-prefix", "unknown"},
		{"", "unknown"},
	}
	for _, tt := range tests {
		t.Run(tt.key[:min(len(tt.key), 20)], func(t *testing.T) {
			result := detectProviderFromKey(tt.key)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestReviewHandler_LanguageNormalization(t *testing.T) {
	r := reviewTestRouter()

	tests := []struct {
		name     string
		language string
		want400  bool
	}{
		{"empty defaults to go", "", false},
		{"python", "python", false},
		{"py alias", "py", false},
		{"javascript", "javascript", false},
		{"js alias", "js", false},
		{"typescript", "typescript", false},
		{"ts alias", "ts", false},
		{"rust", "rust", false},
		{"rs alias", "rs", false},
		{"java", "java", false},
		{"go", "go", false},
		{"unsupported", "c++", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := reqWithClaims("POST", "/review", map[string]interface{}{
				"prompt":   "test",
				"language": tt.language,
			}, testClaims)
			w := httptest.NewRecorder()
			r.reviewHandler(w, req)
			if tt.want400 {
				assert.Equal(t, http.StatusBadRequest, w.Code)
			} else {
				// Will fail at nil llmRouter, not language validation
				assert.NotEqual(t, http.StatusBadRequest, w.Code)
			}
		})
	}
}
