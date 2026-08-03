package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOpenRouterAdapter(t *testing.T) {
	o := NewOpenRouter("test-key")
	require.NotNil(t, o)
	assert.Equal(t, "test-key", o.apiKey)
	assert.Equal(t, "openrouter", o.Name())
	assert.NotNil(t, o.httpClient)
	assert.Equal(t, 120*time.Second, o.httpClient.Timeout)
}

func TestCalculateOpenRouterCost_KnownModelCalc(t *testing.T) {
	cost := calculateOpenRouterCost("gpt-4o", 1000, 500)
	assert.Greater(t, cost, 0.0)
}

func TestCalculateOpenRouterCost_FallbackUnknownModel(t *testing.T) {
	cost := calculateOpenRouterCost("some-unknown-model", 1000, 500)
	assert.Greater(t, cost, 0.0)
	// Fallback: input=0.001/1K, output=0.003/1K
	expected := 1.0*0.001 + 0.5*0.003
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateOpenRouterCost_ZeroTokens(t *testing.T) {
	cost := calculateOpenRouterCost("gpt-4o", 0, 0)
	assert.Equal(t, 0.0, cost)
}

func TestOpenRouter_Chat_AuthHeader(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "test-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			assert.Equal(t, "https://vigilagent.com", r.Header.Get("HTTP-Referer"))
			assert.Equal(t, "VigilAgent", r.Header.Get("X-Title"))
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1, 1))
		}),
	}
	_, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestOpenRouter_Chat_Cost(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1000, 500))
		}),
	}
	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Cost, 0.0)
}

func TestOpenRouter_Chat_Latency(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1, 1))
		}),
	}
	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Latency, 10*time.Millisecond)
}

func TestOpenRouter_Chat_BadJSON(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("not json"))
		}),
	}
	_, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err)
}

func TestOpenRouter_Chat_WithSystem(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			body := make([]byte, 0)
			buf := make([]byte, 1024)
			for {
				n, err := r.Body.Read(buf)
				body = append(body, buf[:n]...)
				if err != nil {
					break
				}
			}
			var req OpenAIStyleStreamRequest
			json.Unmarshal(body, &req)
			if len(req.Messages) < 2 {
				t.Errorf("expected system + user, got %d messages", len(req.Messages))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 5, 5))
		}),
	}
	_, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", System: "be helpful",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestOpenRouter_HealthCheck_AuthHeader(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "my-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer my-key", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := o.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestOpenRouter_HealthCheck_Non200(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}),
	}
	err := o.HealthCheck(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}

func TestOpenRouter_Stream_AuthHeaders(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "test-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			assert.Equal(t, "https://vigilagent.com", r.Header.Get("HTTP-Referer"))
			assert.Equal(t, "VigilAgent", r.Header.Get("X-Title"))
			w.Header().Set("Content-Type", "text/event-stream")
			ev := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"delta": map[string]interface{}{"content": "hi"}},
				},
			}
			b, _ := json.Marshal(ev)
			w.Write(b)
			w.Write([]byte("\n"))
		}),
	}
	ch, err := o.Stream(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	for range ch {
	}
}

func TestOpenRouter_Chat_UnknownModelCost(t *testing.T) {
	o := &OpenRouterAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			// Use a response with a model not in PriceTable
			resp := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"message": map[string]interface{}{"content": "ok"}, "finish_reason": "stop"},
				},
				"usage": map[string]interface{}{"prompt_tokens": 1000, "completion_tokens": 500},
				"model": "some-random-model",
			}
			b, _ := json.Marshal(resp)
			w.Write(b)
		}),
	}
	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model: "some-random-model", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	// Should use fallback pricing
	assert.Greater(t, resp.Cost, 0.0)
}
