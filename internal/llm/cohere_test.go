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

func TestNewCohereAdapter(t *testing.T) {
	c := NewCohere("test-key")
	require.NotNil(t, c)
	assert.Equal(t, "test-key", c.apiKey)
	assert.Equal(t, "cohere", c.Name())
	assert.NotNil(t, c.httpClient)
	assert.Equal(t, 120*time.Second, c.httpClient.Timeout)
}

func TestCalculateCohereCost_CommandRPlus(t *testing.T) {
	cost := calculateCohereCost("command-r-plus", 1000, 500)
	assert.Greater(t, cost, 0.0)
	// command-r-plus: input=0.0015/1K, output=0.00225/1K
	expected := 1.0*0.0015 + 0.5*0.00225
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateCohereCost_CommandR(t *testing.T) {
	cost := calculateCohereCost("command-r", 1000, 500)
	assert.Greater(t, cost, 0.0)
	// command-r: input=0.00015/1K, output=0.00015/1K
	expected := 1.0*0.00015 + 0.5*0.00015
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateCohereCost_Command(t *testing.T) {
	cost := calculateCohereCost("command", 1000, 500)
	assert.Greater(t, cost, 0.0)
}

func TestCalculateCohereCost_UnknownFallback(t *testing.T) {
	cost := calculateCohereCost("unknown-model", 1000, 500)
	assert.Greater(t, cost, 0.0)
	// Fallback uses command-r pricing
	expected := 1.0*0.00015 + 0.5*0.00015
	assert.InDelta(t, expected, cost, 0.00001)
}

func TestCalculateCohereCost_ZeroTokens(t *testing.T) {
	cost := calculateCohereCost("command-r-plus", 0, 0)
	assert.Equal(t, 0.0, cost)
}

func TestBuildCohereMessages_EmptySystem(t *testing.T) {
	msgs := buildCohereMessages("", []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})
	require.Len(t, msgs, 2)
	assert.Equal(t, "user", msgs[0].Role)
	assert.Equal(t, "hi", msgs[0].Message)
	assert.Equal(t, "assistant", msgs[1].Role)
}

func TestBuildCohereMessages_WithSystemMsg(t *testing.T) {
	msgs := buildCohereMessages("be helpful", []Message{
		{Role: "user", Content: "hi"},
	})
	require.Len(t, msgs, 2)
	assert.Equal(t, "system", msgs[0].Role)
	assert.Equal(t, "be helpful", msgs[0].Message)
	assert.Equal(t, "user", msgs[1].Role)
}

func TestBuildCohereMessages_EmptyMessages(t *testing.T) {
	msgs := buildCohereMessages("sys", nil)
	require.Len(t, msgs, 1)
	assert.Equal(t, "system", msgs[0].Role)
}

func TestBuildCohereMessages_NoSystemNoMessages(t *testing.T) {
	msgs := buildCohereMessages("", nil)
	assert.Empty(t, msgs)
}

func TestCohere_Chat_UsesV2Endpoint(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "test-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Contains(t, r.URL.Path, "/v2/chat")
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"message": map[string]interface{}{
					"content": []map[string]interface{}{{"text": "ok"}},
				},
				"meta": map[string]interface{}{
					"tokens": map[string]interface{}{"input_tokens": 5, "output_tokens": 5},
				},
			}
			b, _ := json.Marshal(resp)
			w.Write(b)
		}),
	}
	_, err := c.Chat(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestCohere_Chat_AuthHeader(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "test-key-123",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-key-123", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"message": map[string]interface{}{
					"content": []map[string]interface{}{{"text": "ok"}},
				},
				"meta": map[string]interface{}{
					"tokens": map[string]interface{}{"input_tokens": 1, "output_tokens": 1},
				},
			}
			b, _ := json.Marshal(resp)
			w.Write(b)
		}),
	}
	_, err := c.Chat(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestCohere_Chat_WithSystemAndTemperature(t *testing.T) {
	c := &CohereAdapter{
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
			var req cohereRequest
			json.Unmarshal(body, &req)
			// system message should be first
			assert.Equal(t, "system", req.Messages[0].Role)
			assert.Equal(t, "be helpful", req.Messages[0].Message)
			assert.Equal(t, 0.7, req.Temperature)

			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"message": map[string]interface{}{
					"content": []map[string]interface{}{{"text": "ok"}},
				},
				"meta": map[string]interface{}{
					"tokens": map[string]interface{}{"input_tokens": 1, "output_tokens": 1},
				},
			}
			b, _ := json.Marshal(resp)
			w.Write(b)
		}),
	}
	_, err := c.Chat(context.Background(), &ChatRequest{
		Model:       "command-r-plus",
		System:      "be helpful",
		Temperature: 0.7,
		Messages:    []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestCohere_HealthCheck_AuthHeader(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "my-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer my-key", r.Header.Get("Authorization"))
			w.WriteHeader(http.StatusOK)
		}),
	}
	err := c.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestCohere_Chat_Latency(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(10 * time.Millisecond)
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"message": map[string]interface{}{
					"content": []map[string]interface{}{{"text": "ok"}},
				},
				"meta": map[string]interface{}{
					"tokens": map[string]interface{}{"input_tokens": 1, "output_tokens": 1},
				},
			}
			b, _ := json.Marshal(resp)
			w.Write(b)
		}),
	}
	resp, err := c.Chat(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Latency, 10*time.Millisecond)
}

func TestCohere_Chat_Cost(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "k",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := map[string]interface{}{
				"message": map[string]interface{}{
					"content": []map[string]interface{}{{"text": "ok"}},
				},
				"meta": map[string]interface{}{
					"tokens": map[string]interface{}{"input_tokens": 1000, "output_tokens": 500},
				},
			}
			b, _ := json.Marshal(resp)
			w.Write(b)
		}),
	}
	resp, err := c.Chat(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Greater(t, resp.Cost, 0.0)
}

func TestCohere_Stream_AuthHeader(t *testing.T) {
	c := &CohereAdapter{
		apiKey: "test-key",
		httpClient: newMockHTTPClient(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
			w.Header().Set("Content-Type", "text/event-stream")
			e := map[string]interface{}{"type": "message-end"}
			b, _ := json.Marshal(e)
			w.Write(b)
			w.Write([]byte("\n"))
		}),
	}
	ch, err := c.Stream(context.Background(), &ChatRequest{
		Model: "command-r-plus", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	for range ch {
	}
}
