package llm

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseOpenAIStyleSSE_SingleEvent(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{"content": "Hello"}},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.Equal(t, "Hello", chunk.Content)
	assert.False(t, chunk.Finish)
}

func TestParseOpenAIStyleSSE_MultipleEvents(t *testing.T) {
	events := openAISSEEvents([]string{"A", "B", "C"})
	dec := json.NewDecoder(bytes.NewReader(events))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	var content string
	for chunk := range ch {
		if chunk.Finish {
			break
		}
		content += chunk.Content
	}
	assert.Equal(t, "ABC", content)
}

func TestParseOpenAIStyleSSE_EmptyDecoder(t *testing.T) {
	dec := json.NewDecoder(strings.NewReader(""))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.True(t, chunk.Finish)
}

func TestParseOpenAIStyleSSE_FinishReason(t *testing.T) {
	finish := "stop"
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"delta":          map[string]interface{}{},
				"finish_reason":  finish,
			},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.True(t, chunk.Finish)
}

func TestParseOpenAIStyleSSE_EmptyChoicesEvent(t *testing.T) {
	data := map[string]interface{}{"choices": []map[string]interface{}{}}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.True(t, chunk.Finish)
}

func TestParseOpenAIStyleSSE_ContentAndFinish(t *testing.T) {
	finish := "stop"
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"delta":          map[string]interface{}{"content": "done"},
				"finish_reason":  finish,
			},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.Equal(t, "done", chunk.Content)
	assert.True(t, chunk.Finish)
}

func TestParseOpenAIStyleSSE_NilFinishReason(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"delta":          map[string]interface{}{"content": "hi"},
				"finish_reason":  nil,
			},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	assert.Equal(t, "hi", chunk.Content)
	assert.False(t, chunk.Finish)
}

func TestBuildOpenAIMessages_WithSystem(t *testing.T) {
	msgs := BuildOpenAIMessages("system prompt", []Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})
	require.Len(t, msgs, 3)
	assert.Equal(t, "system", msgs[0].Role)
	assert.Equal(t, "system prompt", msgs[0].Content)
	assert.Equal(t, "user", msgs[1].Role)
	assert.Equal(t, "assistant", msgs[2].Role)
}

func TestBuildOpenAIMessages_NoSystemMsg(t *testing.T) {
	msgs := BuildOpenAIMessages("", []Message{
		{Role: "user", Content: "hi"},
	})
	require.Len(t, msgs, 1)
	assert.Equal(t, "user", msgs[0].Role)
}

func TestBuildOpenAIMessages_Empty(t *testing.T) {
	msgs := BuildOpenAIMessages("sys", nil)
	require.Len(t, msgs, 1)
	assert.Equal(t, "system", msgs[0].Role)
}

func TestBuildOpenAIMessages_NoSystemNoMessages(t *testing.T) {
	msgs := BuildOpenAIMessages("", nil)
	assert.Empty(t, msgs)
}

func TestBuildOpenAIMessages_Multiple(t *testing.T) {
	msgs := BuildOpenAIMessages("", []Message{
		{Role: "user", Content: "a"},
		{Role: "assistant", Content: "b"},
		{Role: "user", Content: "c"},
		{Role: "assistant", Content: "d"},
	})
	require.Len(t, msgs, 4)
}

func TestReadFullResponse_ValidJSON(t *testing.T) {
	data := openAIChatResponse("hello world", 10, 20)
	resp, err := ReadFullResponse(bytes.NewReader(data))
	require.NoError(t, err)
	assert.Equal(t, "hello world", resp.Choices[0].Message.Content)
	assert.Equal(t, 10, resp.Usage.PromptTokens)
	assert.Equal(t, 20, resp.Usage.CompletionTokens)
}

func TestReadFullResponse_InvalidJSONBody(t *testing.T) {
	_, err := ReadFullResponse(strings.NewReader("not json at all"))
	assert.Error(t, err)
}

func TestReadFullResponse_EmptyJSON(t *testing.T) {
	_, err := ReadFullResponse(strings.NewReader("{}"))
	assert.NoError(t, err)
}

func TestReadFullResponse_EmptyBody(t *testing.T) {
	_, err := ReadFullResponse(strings.NewReader(""))
	assert.Error(t, err)
}

func TestReadFullResponse_FinishReason(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{
				"message":        map[string]interface{}{"content": "ok"},
				"finish_reason":  "tool_calls",
			},
		},
		"usage": map[string]interface{}{"prompt_tokens": 5, "completion_tokens": 5},
	}
	b, _ := json.Marshal(data)
	resp, err := ReadFullResponse(bytes.NewReader(b))
	require.NoError(t, err)
	assert.Equal(t, "tool_calls", resp.Choices[0].FinishReason)
}

func TestOpenAIStyleSSEEvent_Fields(t *testing.T) {
	event := OpenAIStyleSSEEvent{
		Choices: []struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		}{
			{
				Delta: struct {
					Content string `json:"content"`
				}{Content: "test"},
			},
		},
	}
	assert.Equal(t, "test", event.Choices[0].Delta.Content)
}

func TestOpenAIStyleStreamRequest_Fields(t *testing.T) {
	req := OpenAIStyleStreamRequest{
		Model:       "gpt-4o",
		Messages:    []OpenAIStyleMsg{{Role: "user", Content: "hi"}},
		MaxTokens:   100,
		Temperature: 0.7,
		Stream:      true,
	}
	b, err := json.Marshal(req)
	require.NoError(t, err)
	assert.Contains(t, string(b), "gpt-4o")
	assert.Contains(t, string(b), "stream")
}

func TestOpenAIStyleMsg_Fields(t *testing.T) {
	msg := OpenAIStyleMsg{Role: "user", Content: "hello"}
	b, err := json.Marshal(msg)
	require.NoError(t, err)
	assert.Contains(t, string(b), "user")
	assert.Contains(t, string(b), "hello")
}

func TestParseOpenAIStyleSSE_ChannelFull(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{"content": "x"}},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 1) // buffer of 1
	ParseOpenAIStyleSSE(dec, ch)

	// Should not block even with full channel
	select {
	case chunk := <-ch:
		assert.Equal(t, "x", chunk.Content)
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
}

func TestParseOpenAIStyleSSE_OnlyEmptyContent(t *testing.T) {
	data := map[string]interface{}{
		"choices": []map[string]interface{}{
			{"delta": map[string]interface{}{"content": ""}},
		},
	}
	b, _ := json.Marshal(data)
	dec := json.NewDecoder(bytes.NewReader(b))
	ch := make(chan *ChatChunk, 10)
	ParseOpenAIStyleSSE(dec, ch)

	chunk := <-ch
	// Empty content with no finish should still be received
	assert.True(t, chunk.Finish) // finish from deferred
}
