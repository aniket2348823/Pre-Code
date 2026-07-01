package memory

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Embedder defines the interface for text embedding providers.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
	Dimensions() int
	Name() string
}

// OpenAIEmbedder implements Embedder using OpenAI's text-embedding API.
type OpenAIEmbedder struct {
	apiKey     string
	model      string
	dimensions int
	httpClient *http.Client
}

// NewOpenAIEmbedder creates a new OpenAI embedding provider.
func NewOpenAIEmbedder(apiKey string) *OpenAIEmbedder {
	return &OpenAIEmbedder{
		apiKey:     apiKey,
		model:      "text-embedding-3-small",
		dimensions: 1536,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (e *OpenAIEmbedder) Name() string       { return "openai" }
func (e *OpenAIEmbedder) Dimensions() int    { return e.dimensions }

type openAIEmbedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type openAIEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
		Index     int       `json:"index"`
	} `json:"data"`
	Usage struct {
		PromptTokens int `json:"prompt_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage"`
}

func (e *OpenAIEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := openAIEmbedRequest{
		Model: e.model,
		Input: []string{text},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal embedding request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.openai.com/v1/embeddings", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+e.apiKey)

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("embedding API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var embedResp openAIEmbedResponse
	if err := json.Unmarshal(respBody, &embedResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(embedResp.Data) == 0 {
		return nil, fmt.Errorf("no embeddings returned")
	}

	return embedResp.Data[0].Embedding, nil
}

// NoOpEmbedder is a placeholder that returns zero vectors.
type NoOpEmbedder struct {
	dimensions int
}

func NewNoOpEmbedder(dimensions int) *NoOpEmbedder {
	return &NoOpEmbedder{dimensions: dimensions}
}

func (e *NoOpEmbedder) Name() string    { return "noop" }
func (e *NoOpEmbedder) Dimensions() int { return e.dimensions }

func (e *NoOpEmbedder) Embed(_ context.Context, _ string) ([]float32, error) {
	return make([]float32, e.dimensions), nil
}
