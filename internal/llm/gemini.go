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
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
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
	_, err := g.client.Models.GenerateContent(ctx, "gemini-2.0-flash",
		genai.Text("ping"), nil)
	if err != nil {
		return fmt.Errorf("gemini health check failed: %w", err)
	}
	return nil
}

// ptrFloat32 returns a pointer to a float32.
func ptrFloat32(v float32) *float32 { return &v }

func (g *GeminiAdapter) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	model := req.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}

	contents := buildGeminiContents(req.Messages)

	config := &genai.GenerateContentConfig{
		MaxOutputTokens: int32(req.MaxTokens),
		Temperature:     ptrFloat32(float32(req.Temperature)),
	}

	if req.System != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: req.System}},
		}
	}

	resp, err := g.client.Models.GenerateContent(ctx, model, contents, config)
	if err != nil {
		return nil, fmt.Errorf("gemini chat failed: %w", err)
	}

	latency := time.Since(start)

	content := ""
	var toolCalls []ToolCall

	if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
		for _, part := range resp.Candidates[0].Content.Parts {
			if part.Text != "" {
				content += part.Text
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

	contents := buildGeminiContents(req.Messages)

	config := &genai.GenerateContentConfig{
		MaxOutputTokens: int32(req.MaxTokens),
		Temperature:     ptrFloat32(float32(req.Temperature)),
	}

	if req.System != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{{Text: req.System}},
		}
	}

	streamIter := g.client.Models.GenerateContentStream(ctx, model, contents, config)

	ch := make(chan *ChatChunk, 32)
	go func() {
		defer close(ch)
		for resp, err := range streamIter {
			if err != nil {
				break
			}
			if len(resp.Candidates) > 0 && resp.Candidates[0].Content != nil {
				for _, part := range resp.Candidates[0].Content.Parts {
					if part.Text != "" {
						ch <- &ChatChunk{
							Content: part.Text,
						}
					}
				}
			}
		}
		ch <- &ChatChunk{Finish: true}
	}()

	return ch, nil
}

// buildGeminiContents converts request messages into Gemini Content objects.
func buildGeminiContents(messages []Message) []*genai.Content {
	var contents []*genai.Content
	for _, m := range messages {
		role := "user"
		if m.Role == "assistant" {
			role = "model"
		}
		contents = append(contents, &genai.Content{
			Role:  role,
			Parts: []*genai.Part{{Text: m.Content}},
		})
	}
	return contents
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
