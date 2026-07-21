package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// CohereAdapter implements the Provider interface for Cohere.
type CohereAdapter struct {
	apiKey     string
	httpClient *http.Client
}

// NewCohere creates a new Cohere provider.
func NewCohere(apiKey string) *CohereAdapter {
	return &CohereAdapter{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (c *CohereAdapter) Name() string { return "cohere" }

func (c *CohereAdapter) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.cohere.com/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("cohere health check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("cohere health check returned status %d", resp.StatusCode)
	}
	return nil
}

type cohereRequest struct {
	Model       string      `json:"model"`
	Messages    []cohereMsg `json:"messages"`
	MaxTokens   int         `json:"max_tokens,omitempty"`
	Temperature float64     `json:"temperature,omitempty"`
	Stream      bool        `json:"stream"`
}

type cohereMsg struct {
	Role    string `json:"role"`
	Message string `json:"message"`
}

type cohereResponse struct {
	Message struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	} `json:"message"`
	Usage struct {
		Tokens struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"tokens"`
	} `json:"meta"`
}

// buildCohereMessages converts internal messages to Cohere v2 format.
// NOTE: Cohere v2 chat API supports "system", "user", and "assistant" roles.
func buildCohereMessages(system string, messages []Message) []cohereMsg {
	msgs := make([]cohereMsg, 0, len(messages)+1)
	if system != "" {
		msgs = append(msgs, cohereMsg{Role: "system", Message: system})
	}
	for _, m := range messages {
		msgs = append(msgs, cohereMsg{Role: m.Role, Message: m.Content})
	}
	return msgs
}

func (c *CohereAdapter) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	msgs := buildCohereMessages(req.System, req.Messages)

	body, _ := json.Marshal(cohereRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.cohere.com/v2/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("cohere chat failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := safeReadBody(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("cohere returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var cResp cohereResponse
	if err := json.Unmarshal(respBody, &cResp); err != nil {
		return nil, err
	}

	latency := time.Since(start)
	content := ""
	for _, part := range cResp.Message.Content {
		content += part.Text
	}

	cost := calculateCohereCost(req.Model, cResp.Usage.Tokens.InputTokens, cResp.Usage.Tokens.OutputTokens)

	return &ChatResponse{
		Content:      content,
		InputTokens:  cResp.Usage.Tokens.InputTokens,
		OutputTokens: cResp.Usage.Tokens.OutputTokens,
		Cost:         cost,
		Latency:      latency,
		Model:        req.Model,
		Provider:     "cohere",
	}, nil
}

func (c *CohereAdapter) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	msgs := buildCohereMessages(req.System, req.Messages)

	body, _ := json.Marshal(cohereRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.cohere.com/v2/chat", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		respBody, _ := safeReadBody(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("cohere stream returned status %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan *ChatChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		decoder := json.NewDecoder(resp.Body)
		for {
			var event struct {
				Type  string `json:"type"`
				Delta struct {
					Message struct {
						Content struct {
							Text string `json:"text"`
						} `json:"content"`
					} `json:"delta"`
				} `json:"delta"`
			}
			if err := decoder.Decode(&event); err != nil {
				ch <- &ChatChunk{Finish: true}
				return
			}
			if event.Type == "content-delta" {
				ch <- &ChatChunk{
					Content: event.Delta.Message.Content.Text,
				}
			} else if event.Type == "message-end" {
				ch <- &ChatChunk{Finish: true}
				return
			}
		}
	}()

	return ch, nil
}

func calculateCohereCost(model string, inputTokens, outputTokens int) float64 {
	pricing := map[string]struct{ input, output float64 }{
		"command-r-plus": {0.0015, 0.00225},
		"command-r":      {0.00015, 0.00015},
		"command":        {0.00015, 0.00015},
	}

	p, ok := pricing[model]
	if !ok {
		p = pricing["command-r"]
	}

	inputCost := float64(inputTokens) / 1000.0 * p.input
	outputCost := float64(outputTokens) / 1000.0 * p.output
	return inputCost + outputCost
}
