package llm

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// AnthropicAdapter implements the Provider interface for Anthropic.
type AnthropicAdapter struct {
	apiKey   string
	model    string
	httpAddr string
	client   *http.Client
}

// NewAnthropic creates a new Anthropic provider.
func NewAnthropic(apiKey string) *AnthropicAdapter {
	return &AnthropicAdapter{
		apiKey:   apiKey,
		model:    "claude-sonnet-4-20250514",
		httpAddr: "https://api.anthropic.com",
		client: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (a *AnthropicAdapter) Name() string { return "anthropic" }

func (a *AnthropicAdapter) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", a.httpAddr+"/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("x-api-key", a.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("anthropic health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 401 {
		return fmt.Errorf("anthropic: invalid API key")
	}
	return nil
}

type anthropicRequest struct {
	Model     string              `json:"model"`
	MaxTokens int                 `json:"max_tokens"`
	System    string              `json:"system,omitempty"`
	Messages  []anthropicMessage  `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID         string `json:"id"`
	Role       string `json:"role"`
	Content    []anthropicContentBlock `json:"content"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicContentBlock struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
}

type anthropicStreamEvent struct {
	Type  string `json:"type"`
	Delta anthropicStreamDelta `json:"delta"`
}

type anthropicStreamDelta struct {
	Type  string `json:"type"`
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Input map[string]interface{} `json:"input,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
}

func (a *AnthropicAdapter) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	model := req.Model
	if model == "" {
		model = a.model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	anthReq := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    req.System,
	}
	for _, m := range req.Messages {
		anthReq.Messages = append(anthReq.Messages, anthropicMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	body, err := json.Marshal(anthReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.httpAddr+"/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := safeReadBody(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("anthropic API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var anthropResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	latency := time.Since(start)

	content := ""
	var toolCalls []ToolCall
	for _, c := range anthropResp.Content {
		if c.Type == "text" {
			content += c.Text
		}
		if c.Type == "tool_use" {
			args := c.Input
			if args == nil {
				args = make(map[string]interface{})
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:        c.ID,
				Name:      c.Name,
				Arguments: args,
			})
		}
	}

	cost := calculateAnthropicCost(model, anthropResp.Usage.InputTokens, anthropResp.Usage.OutputTokens)

	return &ChatResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		InputTokens:  anthropResp.Usage.InputTokens,
		OutputTokens: anthropResp.Usage.OutputTokens,
		Cost:         cost,
		Latency:      latency,
		Model:        model,
		Provider:     "anthropic",
		StopReason:   anthropResp.StopReason,
	}, nil
}

func (a *AnthropicAdapter) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	model := req.Model
	if model == "" {
		model = a.model
	}
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = 8192
	}

	anthReq := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    req.System,
	}
	for _, m := range req.Messages {
		anthReq.Messages = append(anthReq.Messages, anthropicMessage{
			Role:    m.Role,
			Content: m.Content,
		})
	}

	body, _ := json.Marshal(anthReq)
	httpReq, err := http.NewRequestWithContext(ctx, "POST", a.httpAddr+"/v1/messages", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", a.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := a.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != 200 {
		respBody, _ := safeReadBody(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("anthropic stream error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan *ChatChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()

		limitedBody := io.LimitReader(resp.Body, 10<<20) // 10MB limit
		scanner := bufio.NewScanner(limitedBody)
		var eventType string
		var dataBuffer strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				if dataBuffer.Len() > 0 && eventType != "" {
					processAnthropicEvent(eventType, dataBuffer.String(), ch)
					dataBuffer.Reset()
					eventType = ""
				}
				continue
			}
			if strings.HasPrefix(line, "event: ") {
				eventType = strings.TrimPrefix(line, "event: ")
			} else if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				dataBuffer.WriteString(data)
			}
		}
		if dataBuffer.Len() > 0 && eventType != "" {
			processAnthropicEvent(eventType, dataBuffer.String(), ch)
		}
		ch <- &ChatChunk{Finish: true}
	}()

	return ch, nil
}

func processAnthropicEvent(eventType, data string, ch chan<- *ChatChunk) {
	var event anthropicStreamEvent
	if err := json.Unmarshal([]byte(data), &event); err != nil {
		return
	}
	switch eventType {
	case "content_block_delta":
		if event.Delta.Text != "" {
			ch <- &ChatChunk{Content: event.Delta.Text}
		}
		if event.Delta.PartialJSON != "" {
			// Tool call streaming - accumulate partial JSON
			ch <- &ChatChunk{Content: event.Delta.PartialJSON}
		}
	case "message_stop":
		ch <- &ChatChunk{Finish: true}
	case "content_block_start":
		if event.Delta.Type == "tool_use" {
			// Tool call started - could emit a chunk with tool call info
			// For now, we'll handle the delta events
		}
	case "content_block_stop":
		// Tool call completed
	}
}

func calculateAnthropicCost(model string, inputTokens, outputTokens int) float64 {
	info, ok := LookupPrice(model)
	if !ok {
		return 0
	}
	inputCost := float64(inputTokens) / 1000.0 * info.InputCostPer1K
	outputCost := float64(outputTokens) / 1000.0 * info.OutputCostPer1K
	return inputCost + outputCost
}
