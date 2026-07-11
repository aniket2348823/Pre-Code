package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// NVIDIANIMAdapter implements the Provider interface for NVIDIA NIM.
type NVIDIANIMAdapter struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
}

// NewNVIDIANIM creates a new NVIDIA NIM provider.
func NewNVIDIANIM(apiKey string) *NVIDIANIMAdapter {
	return &NVIDIANIMAdapter{
		apiKey:  apiKey,
		baseURL: "https://integrate.api.nvidia.com/v1",
		httpClient: &http.Client{
			Timeout: 120 * time.Second,
		},
	}
}

func (n *NVIDIANIMAdapter) Name() string { return "nvidia_nim" }

func (n *NVIDIANIMAdapter) HealthCheck(ctx context.Context) error {
	// Standardized: GET /models with auth header
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, n.baseURL+"/models", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+n.apiKey)
	resp, err := n.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("nvidia nim health check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("nvidia nim health check returned status %d", resp.StatusCode)
	}
	return nil
}

// nvidiaResponse reuses OpenAIStyleNonStreamingResponse from sse_helper.go
// since NVIDIA NIM uses the OpenAI-compatible response format.

func (n *NVIDIANIMAdapter) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	msgs := BuildOpenAIMessages(req.System, req.Messages)

	body, _ := json.Marshal(OpenAIStyleStreamRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", n.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+n.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("nvidia nim chat failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := safeReadBody(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("nvidia nim returned status %d: %s", resp.StatusCode, string(respBody))
	}

	nResp, err := ReadFullResponse(bytes.NewReader(respBody))
	if err != nil {
		return nil, err
	}

	latency := time.Since(start)
	content := ""
	if len(nResp.Choices) > 0 {
		content = nResp.Choices[0].Message.Content
	}

	cost := calculateNIMCost(req.Model, nResp.Usage.PromptTokens, nResp.Usage.CompletionTokens)

	return &ChatResponse{
		Content:      content,
		InputTokens:  nResp.Usage.PromptTokens,
		OutputTokens: nResp.Usage.CompletionTokens,
		Cost:         cost,
		Latency:      latency,
		Model:        req.Model,
		Provider:     "nvidia_nim",
	}, nil
}

func (n *NVIDIANIMAdapter) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	msgs := BuildOpenAIMessages(req.System, req.Messages)

	body, _ := json.Marshal(OpenAIStyleStreamRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stream:      true,
	})

	httpReq, err := http.NewRequestWithContext(ctx, "POST", n.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+n.apiKey)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := n.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != 200 {
		respBody, _ := safeReadBody(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("nvidia nim stream returned status %d: %s", resp.StatusCode, string(respBody))
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

func calculateNIMCost(model string, inputTokens, outputTokens int) float64 {
	pricing := map[string]struct{ input, output float64 }{
		"nvidia/llama-3.1-405b-instruct": {0.003, 0.009},
		"nvidia/llama-3.1-70b-instruct":  {0.00088, 0.00088},
		"nvidia/llama-3.1-8b-instruct":   {0.00018, 0.00018},
		"nvidia/mistral-nemo-12b-instruct": {0.0002, 0.0002},
	}

	p, ok := pricing[model]
	if !ok {
		p = pricing["nvidia/llama-3.1-70b-instruct"]
	}

	inputCost := float64(inputTokens) / 1000.0 * p.input
	outputCost := float64(outputTokens) / 1000.0 * p.output
	return inputCost + outputCost
}
