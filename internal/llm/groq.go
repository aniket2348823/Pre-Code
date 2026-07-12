package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// GroqAdapter implements the Provider interface for Groq.
type GroqAdapter struct {
	apiKey     string
	httpClient *http.Client
}

// NewGroq creates a new Groq provider.
func NewGroq(apiKey string) *GroqAdapter {
	return &GroqAdapter{
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (g *GroqAdapter) Name() string { return "groq" }

func (g *GroqAdapter) HealthCheck(ctx context.Context) error {
	// Standardized: GET /openai/v1/models with auth header
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.groq.com/openai/v1/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+g.apiKey)
	resp, err := g.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("groq health check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("groq health check returned status %d", resp.StatusCode)
	}
	return nil
}

// groqResponse reuses OpenAIStyleNonStreamingResponse from sse_helper.go
// since Groq uses the OpenAI-compatible response format.

func (g *GroqAdapter) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	msgs := BuildOpenAIMessages(req.System, req.Messages)

	body, _ := json.Marshal(OpenAIStyleStreamRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("groq chat failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := safeReadBody(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("groq returned status %d: %s", resp.StatusCode, string(respBody))
	}

	gResp, err := ReadFullResponse(bytes.NewReader(respBody))
	if err != nil {
		return nil, err
	}

	latency := time.Since(start)
	content := ""
	if len(gResp.Choices) > 0 {
		content = gResp.Choices[0].Message.Content
	}

	cost := calculateGroqCost(req.Model, gResp.Usage.PromptTokens, gResp.Usage.CompletionTokens)

	return &ChatResponse{
		Content:      content,
		InputTokens:  gResp.Usage.PromptTokens,
		OutputTokens: gResp.Usage.CompletionTokens,
		Cost:         cost,
		Latency:      latency,
		Model:        req.Model,
		Provider:     "groq",
	}, nil
}

func (g *GroqAdapter) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	msgs := BuildOpenAIMessages(req.System, req.Messages)

	body, _ := json.Marshal(OpenAIStyleStreamRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", "https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+g.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		respBody, _ := safeReadBody(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("groq stream returned status %d: %s", resp.StatusCode, string(respBody))
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

func calculateGroqCost(model string, inputTokens, outputTokens int) float64 {
	pricing := map[string]struct{ input, output float64 }{
		"llama-3.1-70b-versatile": {0.00059, 0.00079},
		"llama-3.1-8b-instant":    {0.00005, 0.00008},
		"mixtral-8x7b-32768":      {0.00024, 0.00024},
		"gemma2-9b-it":            {0.0002, 0.0002},
	}

	p, ok := pricing[model]
	if !ok {
		p = pricing["llama-3.1-70b-versatile"]
	}

	inputCost := float64(inputTokens) / 1000.0 * p.input
	outputCost := float64(outputTokens) / 1000.0 * p.output
	return inputCost + outputCost
}
