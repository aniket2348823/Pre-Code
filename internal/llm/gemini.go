package llm

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/genai"
)

// GeminiAdapter implements the Provider interface for Google Gemini.
type GeminiAdapter struct {
	client *genai.Client
	apiKey string
}

// NewGemini creates a new Google Gemini provider.
func NewGemini(apiKey string) (*GeminiAdapter, error) {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, &genai.ClientOptions{
		APIKey: apiKey,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create gemini client: %w", err)
	}
	return &GeminiAdapter{
		client: client,
		apiKey: apiKey,
	}, nil
}

func (g *GeminiAdapter) Name() string { return "gemini" }

func (g *GeminiAdapter) HealthCheck(ctx context.Context) error {
	_, err := g.client.Models.GenerateContent(ctx, "gemini-2.0-flash", genai.Text("ping"), nil)
	if err != nil {
		return fmt.Errorf("gemini health check failed: %w", err)
	}
	return nil
}

func (g *GeminiAdapter) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	model := req.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}

	var parts []genai.Part
	for _, m := range req.Messages {
		parts = append(parts, genai.Text(m.Content))
	}

	config := &genai.GenerateContentConfig{
		MaxOutputTokens: uint32(req.MaxTokens),
		Temperature:     genai.Float32(float32(req.Temperature)),
	}

	if req.System != "" {
		config.SystemInstruction = genai.Text(req.System)
	}

	resp, err := g.client.Models.GenerateContent(ctx, model, parts, config)
	if err != nil {
		return nil, fmt.Errorf("gemini chat failed: %w", err)
	}

	latency := time.Since(start)

	content := ""
	var toolCalls []ToolCall

	if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
		for _, part := range resp.Candidates[0].Content.Parts {
			if text, ok := part.(genai.Text); ok {
				content += string(text)
			}
		}
	}

	inputTokens := 0
	outputTokens := 0
	if resp.UsageMetadata != nil {
		inputTokens = int(resp.UsageMetadata.PromptTokenCount)
		outputTokens = int(resp.UsageMetadata.CandidatesTokenCount)
	}

	cost := calculateGeminiCost(model, inputTokens, outputTokens)

	return &ChatResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
		Cost:         cost,
		Latency:      latency,
		Model:        model,
		Provider:     "gemini",
	}, nil
}

func (g *GeminiAdapter) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	model := req.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}

	var parts []genai.Part
	for _, m := range req.Messages {
		parts = append(parts, genai.Text(m.Content))
	}

	config := &genai.GenerateContentConfig{
		MaxOutputTokens: uint32(req.MaxTokens),
		Temperature:     genai.Float32(float32(req.Temperature)),
	}

	if req.System != "" {
		config.SystemInstruction = genai.Text(req.System)
	}

	iter := g.client.Models.GenerateContentStream(ctx, model, parts, config)

	ch := make(chan *ChatChunk, 32)
	go func() {
		defer close(ch)
		for iter.Next() {
			resp := iter.Current()
			if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
				for _, part := range resp.Candidates[0].Content.Parts {
					if text, ok := part.(genai.Text); ok {
						ch <- &ChatChunk{
							Content: string(text),
						}
					}
				}
			}
		}
		ch <- &ChatChunk{Finish: true}
	}()

	return ch, nil
}

func calculateGeminiCost(model string, inputTokens, outputTokens int) float64 {
	pricing := map[string]struct{ input, output float64 }{
		"gemini-2.5-pro":   {0.00125, 0.01},
		"gemini-2.0-flash": {0.000075, 0.0003},
		"gemini-1.5-pro":   {0.00125, 0.005},
		"gemini-1.5-flash": {0.000075, 0.0003},
	}

	p, ok := pricing[model]
	if !ok {
		p = pricing["gemini-2.0-flash"]
	}

	inputCost := float64(inputTokens) / 1000.0 * p.input
	outputCost := float64(outputTokens) / 1000.0 * p.output
	return inputCost + outputCost
}
