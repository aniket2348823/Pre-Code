package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// MistralAdapter implements the Provider interface for Mistral AI.
type MistralAdapter struct {
	apiKey     string
	httpClient *http.Client
}

// NewMistral creates a new Mistral AI provider.
func NewMistral(apiKey string) *MistralAdapter {
	return &MistralAdapter{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (m *MistralAdapter) Name() string { return "mistral" }

func (m *MistralAdapter) HealthCheck(ctx context.Context) error {
	// Standardized: GET /v1/models with auth header
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.mistral.ai/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+m.apiKey)
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("mistral health check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("mistral health check returned status %d", resp.StatusCode)
	}
	return nil
}

// mistralResponse reuses OpenAIStyleNonStreamingResponse from sse_helper.go
// since Mistral uses the OpenAI-compatible response format.

func (m *MistralAdapter) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	msgs := BuildOpenAIMessages(req.System, req.Messages)

	body, _ := json.Marshal(OpenAIStyleStreamRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.mistral.ai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("mistral chat failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := safeReadBody(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("mistral returned status %d: %s", resp.StatusCode, string(respBody))
	}

	mResp, err := ReadFullResponse(bytes.NewReader(respBody))
	if err != nil {
		return nil, err
	}

	latency := time.Since(start)
	content := ""
	if len(mResp.Choices) > 0 {
		content = mResp.Choices[0].Message.Content
	}

	cost := calculateMistralCost(req.Model, mResp.Usage.PromptTokens, mResp.Usage.CompletionTokens)

	return &ChatResponse{
		Content:      content,
		InputTokens:  mResp.Usage.PromptTokens,
		OutputTokens: mResp.Usage.CompletionTokens,
		Cost:         cost,
		Latency:      latency,
		Model:        req.Model,
		Provider:     "mistral",
	}, nil
}

func (m *MistralAdapter) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	msgs := BuildOpenAIMessages(req.System, req.Messages)

	body, _ := json.Marshal(OpenAIStyleStreamRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.mistral.ai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+m.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		respBody, _ := safeReadBody(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("mistral stream returned status %d: %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan *ChatChunk, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		decoder := json.NewDecoder(resp.Body)
		ParseOpenAIStyleSSE(decoder, ch)
	}()

	return ch, nil
}

func calculateMistralCost(model string, inputTokens, outputTokens int) float64 {
	pricing := map[string]struct{ input, output float64 }{
		"mistral-large-latest": {0.002, 0.006},
		"mistral-medium-latest": {0.0027, 0.0081},
		"mistral-small-latest":  {0.001, 0.003},
		"open-mixtral-8x22b":    {0.002, 0.006},
		"open-mixtral-8x7b":     {0.0005, 0.0005},
	}

	p, ok := pricing[model]
	if !ok {
		p = pricing["mistral-small-latest"]
	}

	inputCost := float64(inputTokens) / 1000.0 * p.input
	outputCost := float64(outputTokens) / 1000.0 * p.output
	return inputCost + outputCost
}
