package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// OpenRouterAdapter implements the Provider interface for OpenRouter.
// OpenRouter proxies requests to multiple LLM providers through a single API.
// NOTE: OpenRouter passes through provider pricing which varies per model.
// Cost calculation falls back to PriceTable estimates when no override exists.
type OpenRouterAdapter struct {
	apiKey     string
	httpClient *http.Client
}

// NewOpenRouter creates a new OpenRouter provider.
func NewOpenRouter(apiKey string) *OpenRouterAdapter {
	return &OpenRouterAdapter{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (o *OpenRouterAdapter) Name() string { return "openrouter" }

func (o *OpenRouterAdapter) HealthCheck(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+o.apiKey)
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("openrouter health check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("openrouter health check returned status %d", resp.StatusCode)
	}
	return nil
}

func (o *OpenRouterAdapter) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	msgs := BuildOpenAIMessages(req.System, req.Messages)

	body, _ := json.Marshal(OpenAIStyleStreamRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      false,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://vigilagent.com")
	httpReq.Header.Set("X-Title", "VigilAgent")

	resp, err := o.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("openrouter chat failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := safeReadBody(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("openrouter returned status %d: %s", resp.StatusCode, string(respBody))
	}

	orResp, err := ReadFullResponse(bytes.NewReader(respBody))
	if err != nil {
		return nil, err
	}

	latency := time.Since(start)
	content := ""
	if len(orResp.Choices) > 0 {
		content = orResp.Choices[0].Message.Content
	}

	cost := calculateOpenRouterCost(req.Model, orResp.Usage.PromptTokens, orResp.Usage.CompletionTokens)

	return &ChatResponse{
		Content:      content,
		InputTokens:  orResp.Usage.PromptTokens,
		OutputTokens: orResp.Usage.CompletionTokens,
		Cost:         cost,
		Latency:      latency,
		Model:        req.Model,
		Provider:     "openrouter",
	}, nil
}

func (o *OpenRouterAdapter) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	msgs := BuildOpenAIMessages(req.System, req.Messages)

	body, _ := json.Marshal(OpenAIStyleStreamRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://openrouter.ai/api/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+o.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("HTTP-Referer", "https://vigilagent.com")
	httpReq.Header.Set("X-Title", "VigilAgent")

	resp, err := o.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		respBody, _ := safeReadBody(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("openrouter stream returned status %d: %s", resp.StatusCode, string(respBody))
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

// calculateOpenRouterCost uses PriceTable estimates when available, falling back
// to rough defaults. For production accuracy, configure per-model overrides via
// ModelRouter.SetPrices or fetch live pricing from OpenRouter's /models endpoint.
func calculateOpenRouterCost(model string, inputTokens, outputTokens int) float64 {
	if info, ok := LookupPrice(model); ok {
		inputCost := float64(inputTokens) / 1000.0 * info.InputCostPer1K
		outputCost := float64(outputTokens) / 1000.0 * info.OutputCostPer1K
		return inputCost + outputCost
	}
	// Fallback: rough estimate for models not in PriceTable
	inputCost := float64(inputTokens) / 1000.0 * 0.001
	outputCost := float64(outputTokens) / 1000.0 * 0.003
	return inputCost + outputCost
}
