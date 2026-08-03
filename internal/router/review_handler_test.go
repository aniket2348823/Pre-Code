package router

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
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


