package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewOpenAIAdapter(t *testing.T) {
	o := NewOpenAI("test-key")
	require.NotNil(t, o)
	assert.Equal(t, "test-key", o.apiKey)
	assert.Equal(t, "openai", o.Name())
	assert.NotNil(t, o.client)
}

func TestCalculateOpenAICost_GPT4oMini(t *testing.T) {
	cost := calculateOpenAICost("gpt-4o-mini", 1000, 500)
	assert.Greater(t, cost, 0.0)
}

func TestCalculateOpenAICost_GPT4oCalc(t *testing.T) {
	cost := calculateOpenAICost("gpt-4o", 1000, 500)
	assert.Greater(t, cost, 0.0)
}

func TestCalculateOpenAICost_UnknownModelCalc(t *testing.T) {
	cost := calculateOpenAICost("nonexistent-model", 1000, 500)
	assert.Equal(t, 0.0, cost)
}

func TestCalculateOpenAICost_ZeroTokens(t *testing.T) {
	cost := calculateOpenAICost("gpt-4o", 0, 0)
	assert.Equal(t, 0.0, cost)
}

func TestOpenAI_ConvertMessages_WithSystem(t *testing.T) {
	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL("http://localhost:1/v1")}
	msgs := o.convertMessages(&ChatRequest{
		System: "be helpful",
		Messages: []Message{
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
		},
	})
	require.Len(t, msgs, 3)
	assert.Equal(t, "system", msgs[0].Role)
	assert.Equal(t, "be helpful", msgs[0].Content)
	assert.Equal(t, "user", msgs[1].Role)
	assert.Equal(t, "hi", msgs[1].Content)
	assert.Equal(t, "assistant", msgs[2].Role)
}

func TestOpenAI_ConvertMessages_NoSystem(t *testing.T) {
	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL("http://localhost:1/v1")}
	msgs := o.convertMessages(&ChatRequest{
		Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)
}

func TestOpenAI_ConvertMessages_Empty(t *testing.T) {
	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL("http://localhost:1/v1")}
	msgs := o.convertMessages(&ChatRequest{})
	assert.Empty(t, msgs)
}

func TestOpenAI_ConvertMessages_ToolCallID(t *testing.T) {
	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL("http://localhost:1/v1")}
	msgs := o.convertMessages(&ChatRequest{
		Messages: []Message{
			{Role: "tool", Content: "result", ToolCallID: "call_123"},
		},
	})
	require.Len(t, msgs, 1)
	assert.Equal(t, "call_123", msgs[0].ToolCallID)
}

func TestOpenAI_Chat_NoTools(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		_, hasTools := req["tools"]
		assert.False(t, hasTools, "tools should not be present")
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 1, 1))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	_, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestOpenAI_Chat_WithMultipleTools(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		tools := req["tools"].([]interface{})
		assert.Len(t, tools, 2)
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 5, 5))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	_, err := o.Chat(context.Background(), &ChatRequest{
		Model:    "gpt-4o",
		Messages: []Message{{Role: "user", Content: "search"}},
		Tools: []ToolDef{
			{Name: "search", Description: "search the web"},
			{Name: "calculate", Description: "do math"},
		},
	})
	assert.NoError(t, err)
}

func TestOpenAI_HealthCheck_NetworkError(t *testing.T) {
	o := &OpenAIAdapter{
		apiKey: "k",
		client: openaiClientWithBaseURL("http://127.0.0.1:1/v1"),
	}
	err := o.HealthCheck(context.Background())
	assert.Error(t, err)
}

func TestOpenAI_Stream_ContextCancel(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		for {
			select {
			case <-r.Context().Done():
				return
			default:
				ev := map[string]interface{}{
					"choices": []map[string]interface{}{
						{"delta": map[string]interface{}{"content": "x"}},
					},
				}
				b, _ := json.Marshal(ev)
				fmt.Fprintf(w, "data: %s\n\n", b)
				flusher.Flush()
				time.Sleep(5 * time.Millisecond)
			}
		}
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()

	ch, err := o.Stream(ctx, &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
	for range ch {
	}
}

func TestOpenAI_Chat_Latency(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 1, 1))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Latency, 10*time.Millisecond)
}

func TestOpenAI_Chat_Cost(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 1000, 500))
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Cost, 0.0)
}

func TestOpenAI_Chat_StopReason(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		resp := map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message":        map[string]interface{}{"content": "ok"},
					"finish_reason":  "length",
				},
			},
			"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 5},
			"model": "gpt-4o",
		}
		b, _ := json.Marshal(resp)
		w.Write(b)
	}))
	defer ts.Close()

	o := &OpenAIAdapter{apiKey: "k", client: openaiClientWithBaseURL(ts.URL + "/v1")}
	resp, err := o.Chat(context.Background(), &ChatRequest{
		Model: "gpt-4o", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "ok", resp.Content)
}
