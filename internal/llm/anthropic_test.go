package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAnthropic(t *testing.T) {
	a := NewAnthropic("test-key")
	require.NotNil(t, a)
	assert.Equal(t, "test-key", a.apiKey)
	assert.Equal(t, "anthropic", a.Name())
	assert.Equal(t, "claude-sonnet-4-20250514", a.model)
	assert.Equal(t, "https://api.anthropic.com", a.httpAddr)
	assert.NotNil(t, a.client)
	assert.Equal(t, 120*time.Second, a.client.Timeout)
}

func TestAnthropic_Chat_MultipleContentBlocks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"id":   "msg_123",
			"role": "assistant",
			"content": []map[string]string{
				{"type": "text", "text": "Hello "},
				{"type": "text", "text": "World"},
			},
			"model":       "claude-sonnet-4-20250514",
			"stop_reason": "end_turn",
			"usage": map[string]int{
				"input_tokens":  10,
				"output_tokens": 20,
			},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	defer srv.Close()

	a := &AnthropicAdapter{
		apiKey:   "key",
		model:    "claude-sonnet-4-20250514",
		httpAddr: srv.URL,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
	resp, err := a.Chat(context.Background(), &ChatRequest{
		Model:    "claude-sonnet-4-20250514",
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Hello World", resp.Content)
	assert.Equal(t, 10, resp.InputTokens)
	assert.Equal(t, 20, resp.OutputTokens)
}

func TestAnthropic_Chat_CustomMaxTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		if req["max_tokens"] != float64(500) {
			t.Errorf("expected max_tokens 500, got %v", req["max_tokens"])
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(anthropicResponseBody("ok", 1, 1))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	_, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", MaxTokens: 500,
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestAnthropic_Chat_MultipleMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		msgs := req["messages"].([]interface{})
		if len(msgs) != 3 {
			t.Errorf("expected 3 messages, got %d", len(msgs))
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(anthropicResponseBody("ok", 5, 5))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	_, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514",
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
			{Role: "user", Content: "bye"},
		},
	})
	assert.NoError(t, err)
}

func TestAnthropic_HealthCheck_Headers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "test-key", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	err := a.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestAnthropic_Chat_Headers(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.Header().Set("Content-Type", "application/json")
		w.Write(anthropicResponseBody("ok", 1, 1))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "test-key", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	_, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestCalculateAnthropicCost_VariousModels(t *testing.T) {
	tests := []struct {
		model          string
		inputTokens    int
		outputTokens   int
		expectPositive bool
	}{
		{"claude-opus-4", 1000, 500, true},
		{"claude-sonnet-4-20250514", 1000, 500, true},
		{"claude-haiku-3.5", 1000, 500, true},
		{"nonexistent", 1000, 500, false},
		{"claude-sonnet-4-20250514", 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			cost := calculateAnthropicCost(tt.model, tt.inputTokens, tt.outputTokens)
			if tt.expectPositive {
				assert.Greater(t, cost, 0.0)
			} else {
				assert.Equal(t, 0.0, cost)
			}
		})
	}
}

func TestAnthropic_Chat_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not json"))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	_, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse response")
}

func TestAnthropic_Chat_NonTextContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"id":   "msg_123",
			"role": "assistant",
			"content": []map[string]string{
				{"type": "image", "text": "not a text block"},
			},
			"model":       "claude-sonnet-4-20250514",
			"stop_reason": "end_turn",
			"usage":       map[string]int{"input_tokens": 5, "output_tokens": 5},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	resp, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Empty(t, resp.Content)
}

func TestAnthropic_Chat_StopReason(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"id":          "msg_123",
			"role":        "assistant",
			"content":     []map[string]string{{"type": "text", "text": "ok"}},
			"model":       "claude-sonnet-4-20250514",
			"stop_reason": "max_tokens",
			"usage":       map[string]int{"input_tokens": 5, "output_tokens": 5},
		}
		b, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Write(b)
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	resp, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "max_tokens", resp.StopReason)
}

func TestAnthropic_Chat_Cost(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(anthropicResponseBody("ok", 1000, 500))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	resp, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Cost, 0.0)
}

func TestAnthropic_Stream_ChatChunkFinish(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Write([]byte("event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hi\"}}\n\n"))
		w.Write([]byte("event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	ch, err := a.Stream(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	var content string
	var gotFinish bool
	for chunk := range ch {
		if chunk.Finish {
			gotFinish = true
			break
		}
		content += chunk.Content
	}
	assert.Equal(t, "Hi", content)
	assert.True(t, gotFinish)
}

func TestAnthropic_Chat_Latency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write(anthropicResponseBody("ok", 1, 1))
	}))
	defer srv.Close()

	a := &AnthropicAdapter{apiKey: "k", httpAddr: srv.URL, client: &http.Client{Timeout: 10 * time.Second}}
	resp, err := a.Chat(context.Background(), &ChatRequest{
		Model: "claude-sonnet-4-20250514", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Latency, 10*time.Millisecond)
}
