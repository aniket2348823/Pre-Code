package llm

import (
	"encoding/json"
	"io"
)

// OpenAIStyleSSEEvent represents a standard OpenAI-compatible SSE streaming event.
type OpenAIStyleSSEEvent struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// ParseOpenAIStyleSSE reads OpenAI-compatible SSE events from a decoder and
// sends ChatChunks to the provided channel. It handles the common pattern
// shared by OpenRouter, Mistral, NVIDIA NIM, Groq, and other OpenAI-compatible
// providers. Returns on EOF, decode error, or when a finish_reason is received.
func ParseOpenAIStyleSSE(decoder *json.Decoder, ch chan<- *ChatChunk) {
	defer func() {
		// Ensure channel always gets a finish signal
		select {
		case ch <- &ChatChunk{Finish: true}:
		default:
		}
	}()

	for {
		var event OpenAIStyleSSEEvent
		if err := decoder.Decode(&event); err != nil {
			// EOF or read error — stream ended
			return
		}
		if len(event.Choices) > 0 {
			finish := event.Choices[0].FinishReason != nil
			content := event.Choices[0].Delta.Content

			// Only send if there's content or it's a finish event
			if content != "" || finish {
				ch <- &ChatChunk{
					Content: content,
					Finish:  finish,
				}
			}
			if finish {
				return
			}
		}
	}
}

// OpenAIStyleStreamRequest is the common request body for OpenAI-compatible APIs.
type OpenAIStyleStreamRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIStyleMsg `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature float64         `json:"temperature,omitempty"`
	Stream      bool            `json:"stream"`
}

// OpenAIStyleMsg is a standard chat message for OpenAI-compatible APIs.
type OpenAIStyleMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// BuildOpenAIMessages converts internal Message slice + system prompt to
// OpenAI-style messages, prepending the system message if non-empty.
func BuildOpenAIMessages(system string, messages []Message) []OpenAIStyleMsg {
	msgs := make([]OpenAIStyleMsg, 0, len(messages)+1)
	if system != "" {
		msgs = append(msgs, OpenAIStyleMsg{Role: "system", Content: system})
	}
	for _, m := range messages {
		msgs = append(msgs, OpenAIStyleMsg{Role: m.Role, Content: m.Content})
	}
	return msgs
}

// OpenAIStyleNonStreamingResponse is the common non-streaming response body.
type OpenAIStyleNonStreamingResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// ReadFullResponse reads a non-streaming OpenAI-style response body.
func ReadFullResponse(body io.Reader) (*OpenAIStyleNonStreamingResponse, error) {
	var resp OpenAIStyleNonStreamingResponse
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, err
	}
	return &resp, nil
}
