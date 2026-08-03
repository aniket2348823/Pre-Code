package proxy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRouteRequest_OpenAI(t *testing.T) {
	cfg := &Config{OpenAIKey: "sk-test"}
	tests := []struct {
		model  string
		expect string
	}{
		{"gpt-4o", "openai"},
		{"gpt-4o-mini", "openai"},
		{"gpt-4", "openai"},
		{"gpt-3.5-turbo", "openai"},
		{"o1-preview", "openai"},
		{"o3-mini", "openai"},
		{"o4-mini", "openai"},
		{"gpt-4.5-preview", "openai"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := RouteRequest(tt.model, cfg)
			require.NotNil(t, p)
			assert.Equal(t, tt.expect, p.Name)
			assert.Equal(t, "https://api.openai.com", p.BaseURL)
			assert.Equal(t, "sk-test", p.APIKey)
		})
	}
}

func TestRouteRequest_Anthropic(t *testing.T) {
	cfg := &Config{AnthropicKey: "sk-ant-test"}
	tests := []struct {
		model string
	}{
		{"claude-3-5-sonnet"},
		{"claude-opus-4"},
		{"claude-haiku-3.5"},
		{"claude-sonnet-4-20250514"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := RouteRequest(tt.model, cfg)
			require.NotNil(t, p)
			assert.Equal(t, "anthropic", p.Name)
			assert.Equal(t, "https://api.anthropic.com", p.BaseURL)
			assert.Equal(t, "sk-ant-test", p.APIKey)
		})
	}
}

func TestRouteRequest_Gemini(t *testing.T) {
	cfg := &Config{GeminiKey: "gemini-key"}
	p := RouteRequest("gemini-2.5-pro", cfg)
	require.NotNil(t, p)
	assert.Equal(t, "gemini", p.Name)
	assert.Equal(t, "https://generativelanguage.googleapis.com", p.BaseURL)
}

func TestRouteRequest_Groq(t *testing.T) {
	cfg := &Config{GroqKey: "gsk_test"}
	tests := []struct {
		model string
	}{
		{"llama-3.1-70b-versatile"},
		{"mixtral-8x7b"},
		{"gemma-7b"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := RouteRequest(tt.model, cfg)
			require.NotNil(t, p)
			assert.Equal(t, "groq", p.Name)
			assert.Equal(t, "https://api.groq.com", p.BaseURL)
		})
	}
}

func TestRouteRequest_Mistral(t *testing.T) {
	cfg := &Config{MistralKey: "mistral-key"}
	tests := []struct {
		model string
	}{
		{"mistral-large-latest"},
		{"open-mixtral-8x22b"},
		{"codestral-latest"},
		{"pixtral-large-latest"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := RouteRequest(tt.model, cfg)
			require.NotNil(t, p)
			assert.Equal(t, "mistral", p.Name)
			assert.Equal(t, "https://api.mistral.ai", p.BaseURL)
		})
	}
}

func TestRouteRequest_Cohere(t *testing.T) {
	cfg := &Config{CohereKey: "cohere-key"}
	tests := []struct {
		model string
	}{
		{"command-r-plus"},
		{"command-r"},
		{"command-a"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := RouteRequest(tt.model, cfg)
			require.NotNil(t, p)
			assert.Equal(t, "cohere", p.Name)
			assert.Equal(t, "https://api.cohere.com", p.BaseURL)
		})
	}
}

func TestRouteRequest_NVIDIA(t *testing.T) {
	cfg := &Config{NVIDIAKey: "nv-key"}
	tests := []struct {
		model string
	}{
		{"kimi-k2"},
		{"deepseek-r1"},
		{"nvidia/llama-3.1-405b-instruct"},
		{"meta/llama-3.1-405b-instruct"},
		{"mistralai/mistral-large"},
		{"moonshotai/kimi"},
		{"qwen/qwen-2.5-72b"},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			p := RouteRequest(tt.model, cfg)
			require.NotNil(t, p)
			assert.Equal(t, "nvidia", p.Name)
			assert.Equal(t, "https://build.nvidia.com", p.BaseURL)
		})
	}
}

func TestRouteRequest_OpenRouter(t *testing.T) {
	cfg := &Config{OpenRouterKey: "or-key"}

	// Slash-containing models go to OpenRouter when key is set
	p := RouteRequest("anthropic/claude-opus-4", cfg)
	require.NotNil(t, p)
	assert.Equal(t, "openrouter", p.Name)

	// Free suffix models go to OpenRouter
	p = RouteRequest("model-name:free", cfg)
	require.NotNil(t, p)
	assert.Equal(t, "openrouter", p.Name)
}

func TestRouteRequest_OpenRouterNoKey(t *testing.T) {
	cfg := &Config{}

	// Slash model with no OpenRouter key goes to nvidia if it matches nvidia prefix
	p := RouteRequest("nvidia/llama-3.1-70b-instruct", cfg)
	require.NotNil(t, p)
	assert.Equal(t, "nvidia", p.Name)
}

func TestRouteRequest_Unknown(t *testing.T) {
	cfg := &Config{}
	p := RouteRequest("unknown-model", cfg)
	assert.Nil(t, p)
}

func TestRouteRequest_EmptyModel(t *testing.T) {
	cfg := &Config{OpenAIKey: "key"}
	p := RouteRequest("", cfg)
	assert.Nil(t, p)
}

func TestRouteRequest_FreeSuffix(t *testing.T) {
	cfg := &Config{OpenRouterKey: "or-key"}
	p := RouteRequest("meta-llama/llama-3.1-8b:free", cfg)
	require.NotNil(t, p)
	assert.Equal(t, "openrouter", p.Name)
}

func TestRouteRequest_OpenRouterSlashWithoutKey(t *testing.T) {
	cfg := &Config{}
	// Slash model that doesn't match any known prefix and no OpenRouter key
	p := RouteRequest("custom/my-model", cfg)
	// Falls through to default slash case → openrouter with empty key
	require.NotNil(t, p)
	assert.Equal(t, "openrouter", p.Name)
	assert.Equal(t, "", p.APIKey)
}

func TestRouteRequest_FallbackToOpenRouter(t *testing.T) {
	cfg := &Config{OpenRouterKey: "or-key"}
	// Model with :free suffix
	p := RouteRequest("some-model:free", cfg)
	require.NotNil(t, p)
	assert.Equal(t, "openrouter", p.Name)
}

func TestForwardToProvider_Success(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/v1/chat/completions", r.URL.Path)
		assert.Equal(t, "Bearer sk-test", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		body, _ := io.ReadAll(r.Body)
		assert.Contains(t, string(body), "test message")

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"choices":[{"message":{"content":"response"}}]}`))
	}))
	defer backend.Close()

	provider := &ProviderConfig{
		Name:    "openai",
		BaseURL: backend.URL,
		APIKey:  "sk-test",
	}

	resp, err := forwardToProvider(context.Background(), backend.Client(), provider, []byte(`{"messages":[{"role":"user","content":"test message"}]}`), "/v1/chat/completions")
	require.NoError(t, err)
	assert.Contains(t, string(resp), "response")
}

func TestForwardToProvider_AnthropicHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "sk-ant-test", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Empty(t, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	provider := &ProviderConfig{Name: "anthropic", BaseURL: backend.URL, APIKey: "sk-ant-test"}
	resp, err := forwardToProvider(context.Background(), backend.Client(), provider, []byte(`{}`), "/v1/messages")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestForwardToProvider_GeminiHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "gemini-key", r.Header.Get("x-goog-api-key"))
		assert.Empty(t, r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	provider := &ProviderConfig{Name: "gemini", BaseURL: backend.URL, APIKey: "gemini-key"}
	resp, err := forwardToProvider(context.Background(), backend.Client(), provider, []byte(`{}`), "/v1/models")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestForwardToProvider_CohereHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer cohere-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	provider := &ProviderConfig{Name: "cohere", BaseURL: backend.URL, APIKey: "cohere-key"}
	resp, err := forwardToProvider(context.Background(), backend.Client(), provider, []byte(`{}`), "/v1/chat")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestForwardToProvider_GroqHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer groq-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	provider := &ProviderConfig{Name: "groq", BaseURL: backend.URL, APIKey: "groq-key"}
	resp, err := forwardToProvider(context.Background(), backend.Client(), provider, []byte(`{}`), "/v1/chat/completions")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestForwardToProvider_MistralHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer mistral-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	provider := &ProviderConfig{Name: "mistral", BaseURL: backend.URL, APIKey: "mistral-key"}
	resp, err := forwardToProvider(context.Background(), backend.Client(), provider, []byte(`{}`), "/v1/chat/completions")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestForwardToProvider_OpenRouterHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer or-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	provider := &ProviderConfig{Name: "openrouter", BaseURL: backend.URL, APIKey: "or-key"}
	resp, err := forwardToProvider(context.Background(), backend.Client(), provider, []byte(`{}`), "/v1/chat/completions")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestForwardToProvider_NvidiaHeaders(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer nv-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{}`))
	}))
	defer backend.Close()

	provider := &ProviderConfig{Name: "nvidia", BaseURL: backend.URL, APIKey: "nv-key"}
	resp, err := forwardToProvider(context.Background(), backend.Client(), provider, []byte(`{}`), "/v1/chat/completions")
	require.NoError(t, err)
	assert.NotNil(t, resp)
}

func TestForwardToProvider_ContextCanceled(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer backend.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	provider := &ProviderConfig{Name: "openai", BaseURL: backend.URL, APIKey: "key"}
	_, err := forwardToProvider(ctx, backend.Client(), provider, []byte(`{}`), "/v1/chat/completions")
	assert.Error(t, err)
}

func TestForwardToProvider_BadURL(t *testing.T) {
	provider := &ProviderConfig{Name: "openai", BaseURL: "http://127.0.0.1:1", APIKey: "key"}
	_, err := forwardToProvider(context.Background(), &http.Client{}, provider, []byte(`{}`), "/v1/chat/completions")
	assert.Error(t, err)
}

func TestProviderConfig_Fields(t *testing.T) {
	p := ProviderConfig{
		Name:    "openai",
		BaseURL: "https://api.openai.com",
		APIKey:  "sk-test",
	}
	assert.Equal(t, "openai", p.Name)
	assert.Equal(t, "https://api.openai.com", p.BaseURL)
	assert.Equal(t, "sk-test", p.APIKey)
}

func TestForwardToProvider_JSONBody(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)

		var msg map[string]interface{}
		err = json.Unmarshal(body, &msg)
		require.NoError(t, err)
		assert.Equal(t, "gpt-4o", msg["model"])

		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	}))
	defer backend.Close()

	body := `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`
	provider := &ProviderConfig{Name: "openai", BaseURL: backend.URL, APIKey: "key"}
	resp, err := forwardToProvider(context.Background(), backend.Client(), provider, []byte(body), "/v1/chat/completions")
	require.NoError(t, err)
	assert.Contains(t, string(resp), "ok")
}
