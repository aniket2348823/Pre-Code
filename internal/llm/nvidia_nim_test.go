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

func TestNewNVIDIANIMAdapter(t *testing.T) {
	n := NewNVIDIANIM("test-key")
	require.NotNil(t, n)
	assert.Equal(t, "test-key", n.apiKey)
	assert.Equal(t, "nvidia_nim", n.Name())
	assert.Equal(t, "https://build.nvidia.com/v1", n.baseURL)
	assert.NotNil(t, n.httpClient)
	assert.Equal(t, 120*time.Second, n.httpClient.Timeout)
}

func TestCalculateNIMCost_Llama405b(t *testing.T) {
	cost := calculateNIMCost("nvidia/llama-3.1-405b-instruct", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.003 + 0.5*0.009
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateNIMCost_Llama70b(t *testing.T) {
	cost := calculateNIMCost("nvidia/llama-3.1-70b-instruct", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.00088 + 0.5*0.00088
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateNIMCost_Llama8b(t *testing.T) {
	cost := calculateNIMCost("nvidia/llama-3.1-8b-instruct", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.00018 + 0.5*0.00018
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateNIMCost_MistralNemo(t *testing.T) {
	cost := calculateNIMCost("nvidia/mistral-nemo-12b-instruct", 1000, 500)
	assert.Greater(t, cost, 0.0)
}

func TestCalculateNIMCost_UnknownFallback(t *testing.T) {
	cost := calculateNIMCost("unknown-model", 1000, 500)
	assert.Greater(t, cost, 0.0)
	// Fallback uses nvidia/llama-3.1-70b-instruct pricing
	expected := 1.0*0.00088 + 0.5*0.00088
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateNIMCost_ZeroTokens(t *testing.T) {
	cost := calculateNIMCost("nvidia/llama-3.1-405b-instruct", 0, 0)
	assert.Equal(t, 0.0, cost)
}

func TestNVIDIANIM_Chat_AuthHeader(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey:  "test-key",
		baseURL: "https://mock.nvidia.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1, 1))
		}),
	}
	_, err := n.Chat(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestNVIDIANIM_Chat_Cost(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1000, 500))
		}),
	}
	resp, err := n.Chat(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Cost, 0.0)
}

func TestNVIDIANIM_Chat_Latency(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1, 1))
		}),
	}
	resp, err := n.Chat(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Latency, 10*time.Millisecond)
}

func TestNVIDIANIM_HealthCheck_AuthHeader(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "my-key", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer my-key", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := n.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestNVIDIANIM_Chat_BadJSON(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("not json"))
		}),
	}
	_, err := n.Chat(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err)
}

func TestNVIDIANIM_Chat_WithSystem(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
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
			// system message should be prepended
			if len(req.Messages) < 2 {
				t.Errorf("expected at least 2 messages, got %d", len(req.Messages))
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 5, 5))
		}),
	}
	_, err := n.Chat(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct", System: "be helpful",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestNVIDIANIM_Stream_AuthHeader(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "test-key", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
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
	ch, err := n.Stream(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	for range ch {
	}
}

func TestNVIDIANIM_Chat_WithTemperature(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			body := make([]byte, 0)
			buf := make([]byte, 1024)
			for {
				nn, err := r.Body.Read(buf)
				body = append(body, buf[:nn]...)
				if err != nil {
					break
				}
			}
			var req OpenAIStyleStreamRequest
			json.Unmarshal(body, &req)
			assert.Equal(t, 0.7, req.Temperature)
			assert.Equal(t, 200, req.MaxTokens)

			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1, 1))
		}),
	}
	_, err := n.Chat(context.Background(), &ChatRequest{
		Model: "nvidia/llama-3.1-405b-instruct",
		Messages:    []Message{{Role: "user", Content: "hi"}},
		MaxTokens:   200,
		Temperature: 0.7,
	})
	assert.NoError(t, err)
}

func TestNVIDIANIM_HealthCheck_Non200(t *testing.T) {
	n := &NVIDIANIMAdapter{
		apiKey: "k", baseURL: "https://mock.test",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusServiceUnavailable)
		}),
	}
	err := n.HealthCheck(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "503")
}
