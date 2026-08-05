package llm

import (
	"bytes"
	"encoding/json"

	openai "github.com/sashabaranov/go-openai"
)

// openaiClientWithBaseURL returns an OpenAI client pointed at the given base
// URL, for tests using httptest.Server.
func openaiClientWithBaseURL(baseURL string) *openai.Client {
	cfg := openai.DefaultConfig("test-key")
	cfg.BaseURL = baseURL
	return openai.NewClientWithConfig(cfg)
}

// openAIChatResponse generates a mock OpenAI-compatible chat completion
// response body for testing. Used by deepseek, groq, and other OpenAI-compatible
// provider tests.
func openAIChatResponse(text string, inputTokens, outputTokens int) []byte {
	resp := map[string]interface{}{
		"id":     "chatcmpl-test",
		"object": "chat.completion",
		"choices": []map[string]interface{}{
			{
				"index":         0,
				"finish_reason": "stop",
				"message": map[string]interface{}{
					"role":    "assistant",
					"content": text,
				},
			},
		},
		"usage": map[string]interface{}{
			"prompt_tokens":     inputTokens,
			"completion_tokens": outputTokens,
			"total_tokens":      inputTokens + outputTokens,
		},
	}
	data, _ := json.Marshal(resp)
	return data
}

// openAISSEEvents builds a byte stream of OpenAI-style SSE events, one JSON
// object per content chunk, ending with a finish event.
func openAISSEEvents(contents []string) []byte {
	stop := "stop"
	var buf bytes.Buffer
	for _, c := range contents {
		ev := OpenAIStyleSSEEvent{}
		ev.Choices = append(ev.Choices, struct {
			Delta struct {
				Content string `json:"content"`
			} `json:"delta"`
			FinishReason *string `json:"finish_reason"`
		}{
			Delta: struct {
				Content string `json:"content"`
			}{Content: c},
		})
		b, _ := json.Marshal(ev)
		buf.Write(b)
		buf.WriteByte('\n')
	}
	// Final finish event.
	fin := OpenAIStyleSSEEvent{}
	fin.Choices = append(fin.Choices, struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	}{FinishReason: &stop})
	b, _ := json.Marshal(fin)
	buf.Write(b)
	buf.WriteByte('\n')
	return buf.Bytes()
}
