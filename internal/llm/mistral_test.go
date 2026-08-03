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

func TestNewMistralAdapter(t *testing.T) {
	m := NewMistral("test-key")
	require.NotNil(t, m)
	assert.Equal(t, "test-key", m.apiKey)
	assert.Equal(t, "mistral", m.Name())
	assert.NotNil(t, m.httpClient)
	assert.Equal(t, 120*time.Second, m.httpClient.Timeout)
}

func TestCalculateMistralCost_Large(t *testing.T) {
	cost := calculateMistralCost("mistral-large-latest", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.002 + 0.5*0.006
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateMistralCost_Medium(t *testing.T) {
	cost := calculateMistralCost("mistral-medium-latest", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.0027 + 0.5*0.0081
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateMistralCost_Small(t *testing.T) {
	cost := calculateMistralCost("mistral-small-latest", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.001 + 0.5*0.003
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateMistralCost_OpenMixtral8x22b(t *testing.T) {
	cost := calculateMistralCost("open-mixtral-8x22b", 1000, 500)
	assert.Greater(t, cost, 0.0)
}

func TestCalculateMistralCost_OpenMixtral8x7b(t *testing.T) {
	cost := calculateMistralCost("open-mixtral-8x7b", 1000, 500)
	assert.Greater(t, cost, 0.0)
}

func TestCalculateMistralCost_UnknownFallback(t *testing.T) {
	cost := calculateMistralCost("unknown-model", 1000, 500)
	assert.Greater(t, cost, 0.0)
	// Fallback uses mistral-small-latest pricing
	expected := 1.0*0.001 + 0.5*0.003
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateMistralCost_ZeroTokens(t *testing.T) {
	cost := calculateMistralCost("mistral-large-latest", 0, 0)
	assert.Equal(t, 0.0, cost)
}

func TestMistral_Chat_AuthHeader(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "test-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1, 1))
		}),
	}
	_, err := m.Chat(context.Background(), &ChatRequest{
		Model: "mistral-large-latest", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestMistral_Chat_Cost(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1000, 500))
		}),
	}
	resp, err := m.Chat(context.Background(), &ChatRequest{
		Model: "mistral-large-latest", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Cost, 0.0)
}

func TestMistral_Chat_Latency(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1, 1))
		}),
	}
	resp, err := m.Chat(context.Background(), &ChatRequest{
		Model: "mistral-large-latest", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Latency, 10*time.Millisecond)
}

func TestMistral_HealthCheck_AuthHeader(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "my-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer my-key", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := m.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestMistral_Chat_BadJSON(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("not json"))
		}),
	}
	_, err := m.Chat(context.Background(), &ChatRequest{
		Model: "mistral-large-latest", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err)
}

func TestMistral_Chat_WithSystem(t *testing.T) {
	m := &MistralAdapter{
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
				t.Errorf("expected system + user messages, got %d", len(req.Messages))
			}
			if req.Messages[0].Role != "system" {
				t.Errorf("expected system first, got %v", req.Messages[0].Role)
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 5, 5))
		}),
	}
	_, err := m.Chat(context.Background(), &ChatRequest{
		Model: "mistral-large-latest", System: "be helpful",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestMistral_Stream_AuthHeader(t *testing.T) {
	m := &MistralAdapter{
		apiKey: "test-key",
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
	ch, err := m.Stream(context.Background(), &ChatRequest{
		Model: "mistral-large-latest", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	for range ch {
	}
}
