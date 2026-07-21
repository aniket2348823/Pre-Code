package llm

import (
	"context"
	"fmt"
	"strings"
	"time"

	openai "github.com/sashabaranov/go-openai"
)

// OpenAIAdapter implements the Provider interface for OpenAI.
type OpenAIAdapter struct {
	client *openai.Client
	apiKey string
}

// NewOpenAI creates a new OpenAI provider.
func NewOpenAI(apiKey string) *OpenAIAdapter {
	client := openai.NewClient(apiKey)
	return &OpenAIAdapter{
		client: client,
		apiKey: apiKey,
	}
}

func (o *OpenAIAdapter) Name() string { return "openai" }

func (o *OpenAIAdapter) HealthCheck(ctx context.Context) error {
	// Use a minimal request to check connectivity
	_, err := o.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model:    openai.GPT4oMini,
		Messages: []openai.ChatCompletionMessage{{Role: openai.ChatMessageRoleUser, Content: "ping"}},
		MaxTokens: 1,
	})
	if err != nil {
		// Ignore content policy errors — they mean the API is reachable
		if strings.Contains(err.Error(), "content_policy") || strings.Contains(err.Error(), "safety") {
			return nil
		}
		return fmt.Errorf("openai health check failed: %w", err)
	}
	return nil
}

func (o *OpenAIAdapter) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	msgs := o.convertMessages(req)
	oaiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
	}

	if len(req.Tools) > 0 {
		oaiTools := make([]openai.Tool, len(req.Tools))
		for i, t := range req.Tools {
			oaiTools[i] = openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.Parameters,
				},
			}
		}
		oaiReq.Tools = oaiTools
	}

	resp, err := o.client.CreateChatCompletion(ctx, oaiReq)
	if err != nil {
		return nil, fmt.Errorf("openai chat failed: %w", err)
	}

	latency := time.Since(start)

	content := ""
	var toolCalls []ToolCall

	if len(resp.Choices) > 0 {
		msg := resp.Choices[0].Message
		content = msg.Content
		for _, tc := range msg.ToolCalls {
			args := make(map[string]interface{})
			if tc.Function.Arguments != "" {
				// Try to parse; on failure store raw string
				args["raw"] = tc.Function.Arguments
			}
			toolCalls = append(toolCalls, ToolCall{
				ID:        tc.ID,
				Name:      tc.Function.Name,
				Arguments: args,
			})
		}
	}

	cost := calculateOpenAICost(req.Model, resp.Usage.PromptTokens, resp.Usage.CompletionTokens)

	return &ChatResponse{
		Content:      content,
		ToolCalls:    toolCalls,
		InputTokens:  resp.Usage.PromptTokens,
		OutputTokens: resp.Usage.CompletionTokens,
		Cost:         cost,
		Latency:      latency,
		Model:        req.Model,
		Provider:     "openai",
	}, nil
}

func (o *OpenAIAdapter) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	msgs := o.convertMessages(req)
	oaiReq := openai.ChatCompletionRequest{
		Model:       req.Model,
		Messages:    msgs,
		MaxTokens:   req.MaxTokens,
		Temperature: float32(req.Temperature),
		Stream:      true,
	}

	stream, err := o.client.CreateChatCompletionStream(ctx, oaiReq)
	if err != nil {
		return nil, fmt.Errorf("openai stream failed: %w", err)
	}

	ch := make(chan *ChatChunk, 32)
	go func() {
		defer close(ch)
		defer stream.Close()

		for {
			resp, err := stream.Recv()
			if err != nil {
				ch <- &ChatChunk{Finish: true}
				return
			}
			if len(resp.Choices) > 0 {
				delta := resp.Choices[0].Delta
				ch <- &ChatChunk{
					Content:    delta.Content,
					StopReason: string(resp.Choices[0].FinishReason),
					Finish:     resp.Choices[0].FinishReason != "",
				}
			}
		}
	}()

	return ch, nil
}

func (o *OpenAIAdapter) convertMessages(req *ChatRequest) []openai.ChatCompletionMessage {
	var msgs []openai.ChatCompletionMessage

	if req.System != "" {
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:    openai.ChatMessageRoleSystem,
			Content: req.System,
		})
	}

	for _, m := range req.Messages {
		msgs = append(msgs, openai.ChatCompletionMessage{
			Role:       m.Role,
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		})
	}

	return msgs
}

func calculateOpenAICost(model string, inputTokens, outputTokens int) float64 {
	info, ok := LookupPrice(model)
	if !ok {
		return 0
	}
	inputCost := float64(inputTokens) / 1000.0 * info.InputCostPer1K
	outputCost := float64(outputTokens) / 1000.0 * info.OutputCostPer1K
	return inputCost + outputCost
}
