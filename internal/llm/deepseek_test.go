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

func TestNewDeepSeek(t *testing.T) {
	d := NewDeepSeek("test-key")
	require.NotNil(t, d)
	assert.Equal(t, "test-key", d.apiKey)
	assert.Equal(t, "deepseek", d.Name())
	assert.Equal(t, "https://api.deepseek.com", d.baseURL)
	assert.NotNil(t, d.httpClient)
	assert.Equal(t, 120*time.Second, d.httpClient.Timeout)
}

func TestDeepSeek_Chat_AuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 1, 1))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "test-key", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestDeepSeek_Chat_ContentLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("Hello DeepSeek World", 10, 20))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	resp, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "Hello DeepSeek World", resp.Content)
	assert.Equal(t, "deepseek", resp.Provider)
	assert.Equal(t, 10, resp.InputTokens)
	assert.Equal(t, 20, resp.OutputTokens)
}

func TestDeepSeek_Chat_VerifyRequestBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		assert.Equal(t, "deepseek-chat", req["model"])
		assert.NotNil(t, req["messages"])

		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 1, 1))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.NoError(t, err)
}

func TestDeepSeek_Chat_TemperatureAndMaxTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req map[string]interface{}
		json.Unmarshal(body, &req)
		assert.Equal(t, float64(200), req["max_tokens"])
		assert.Equal(t, 0.5, req["temperature"])

		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 1, 1))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
		MaxTokens: 200, Temperature: 0.5,
	})
	assert.NoError(t, err)
}

func TestDeepSeek_Chat_MultipleMessages(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write(openAIChatResponse("ok", 5, 5))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat",
		Messages: []Message{
			{Role: "system", Content: "be helpful"},
			{Role: "user", Content: "hi"},
			{Role: "assistant", Content: "hello"},
			{Role: "user", Content: "bye"},
		},
	})
	assert.NoError(t, err)
}

func TestDeepSeek_Stream_SuccessChunks(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{"Hello", " ", "DeepSeek"}
		for _, c := range chunks {
			ev := map[string]interface{}{
				"choices": []map[string]interface{}{
					{"delta": map[string]interface{}{"content": c}},
				},
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "data: %s\n\n", b)
		}
		fmt.Fprintf(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	ch, err := d.Stream(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	require.NoError(t, err)
	var content string
	for chunk := range ch {
		if chunk.Finish {
			break
		}
		content += chunk.Content
	}
	assert.Equal(t, "Hello DeepSeek", content)
}

func TestDeepSeek_HealthCheck_AuthHeader(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "test-key", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	err := d.HealthCheck(context.Background())
	assert.NoError(t, err)
}

func TestDeepSeek_HealthCheck_Non200(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	err := d.HealthCheck(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "status 503")
}

func TestDeepSeek_Chat_HttpClientTimeout(t *testing.T) {
	d := NewDeepSeek("key")
	assert.Equal(t, 120*time.Second, d.httpClient.Timeout)
}

func TestDeepSeek_Chat_ChatError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Chat(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}

func TestDeepSeek_Stream_ErrorStatusCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("rate limited"))
	}))
	defer srv.Close()

	d := &DeepSeekAdapter{apiKey: "k", httpClient: &http.Client{Timeout: 10 * time.Second}, baseURL: srv.URL}
	_, err := d.Stream(context.Background(), &ChatRequest{
		Model: "deepseek-chat", Messages: []Message{{Role: "user", Content: "hi"}},
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "429")
}
