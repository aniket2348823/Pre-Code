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

func TestNewGroqAdapter(t *testing.T) {
	g := NewGroq("test-key")
	require.NotNil(t, g)
	assert.Equal(t, "test-key", g.apiKey)
	assert.Equal(t, "groq", g.Name())
	assert.NotNil(t, g.httpClient)
	assert.Equal(t, 120*time.Second, g.httpClient.Timeout)
}

func TestCalculateGroqCost_Llama70b(t *testing.T) {
	cost := calculateGroqCost("llama-3.1-70b-versatile", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.00059 + 0.5*0.00079
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateGroqCost_Llama8b(t *testing.T) {
	cost := calculateGroqCost("llama-3.1-8b-instant", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.00005 + 0.5*0.00008
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateGroqCost_Mixtral(t *testing.T) {
	cost := calculateGroqCost("mixtral-8x7b-32768", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.00024 + 0.5*0.00024
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateGroqCost_Gemma2(t *testing.T) {
	cost := calculateGroqCost("gemma2-9b-it", 1000, 500)
	assert.Greater(t, cost, 0.0)
	expected := 1.0*0.0002 + 0.5*0.0002
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateGroqCost_UnknownFallback(t *testing.T) {
	cost := calculateGroqCost("unknown-model", 1000, 500)
	assert.Greater(t, cost, 0.0)
	// Fallback uses llama-3.1-70b-versatile pricing
	expected := 1.0*0.00059 + 0.5*0.00079
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateGroqCost_ZeroTokens(t *testing.T) {
	cost := calculateGroqCost("llama-3.1-70b-versatile", 0, 0)
	assert.Equal(t, 0.0, cost)
}

func TestGroq_Chat_AuthHeader(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "test-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1, 1))
		}),
	}
	_, err := g.Chat(context.Background(), &ChatRequest{
		Model: "llama-3.1-70b-versatile", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestGroq_Chat_Cost(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1000, 500))
		}),
	}
	resp, err := g.Chat(context.Background(), &ChatRequest{
		Model: "llama-3.1-70b-versatile", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Cost, 0.0)
}

func TestGroq_Chat_Latency(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1, 1))
		}),
	}
	resp, err := g.Chat(context.Background(), &ChatRequest{
		Model: "llama-3.1-70b-versatile", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Latency, 10*time.Millisecond)
}

func TestGroq_HealthCheck_AuthHeader(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "my-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer my-key", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := g.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestGroq_Chat_EmptyMessages(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write(openAIChatResponse("ok", 1, 1))
		}),
	}
	_, err := g.Chat(context.Background(), &ChatRequest{
		Model: "llama-3.1-70b-versatile", Messages: []Message{},
	})
	assert.NoError(t, err)
}

func TestGroq_Chat_BadJSON(t *testing.T) {
	g := &GroqAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("not json"))
		}),
	}
	_, err := g.Chat(context.Background(), &ChatRequest{
		Model: "llama-3.1-70b-versatile", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err)
}

func TestGroq_Stream_AuthHeader(t *testing.T) {
	g := &GroqAdapter{
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
	ch, err := g.Stream(context.Background(), &ChatRequest{
		Model: "llama-3.1-70b-versatile", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	for range ch {
	}
}
