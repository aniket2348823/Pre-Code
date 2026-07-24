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

// DeepSeekAdapter implements the Provider interface for DeepSeek.
type DeepSeekAdapter struct {
	apiKey     string
	httpClient *http.Client
	baseURL    string
}

// NewDeepSeek creates a new DeepSeek provider.
func NewDeepSeek(apiKey string) *DeepSeekAdapter {
	return &DeepSeekAdapter{
		apiKey:     apiKey,
		httpClient: &http.Client{Timeout: 120 * time.Second},
		baseURL:    "https://api.deepseek.com",
	}
}

func (d *DeepSeekAdapter) Name() string { return "deepseek" }

func (d *DeepSeekAdapter) HealthCheck(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, "GET", d.baseURL+"/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+d.apiKey)
	resp, err := d.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("deepseek health check failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deepseek health check failed: status %d", resp.StatusCode)
	}
	return nil
}

func (d *DeepSeekAdapter) Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error) {
	start := time.Now()

	dReq := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
	}
	if req.MaxTokens > 0 {
		dReq["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != 0 {
		dReq["temperature"] = req.Temperature
	}

	body, _ := json.Marshal(dReq)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", d.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("deepseek API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
		Model string `json:"model"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("deepseek response decode failed: %w", err)
	}

	if len(result.Choices) == 0 {
		return nil, fmt.Errorf("deepseek returned no choices")
	}

	latency := time.Since(start)
	_ = latency // could log latency if needed

	return &ChatResponse{
		Content:      result.Choices[0].Message.Content,
		Model:        result.Model,
		Provider:     "deepseek",
		StopReason:   result.Choices[0].FinishReason,
		InputTokens:  result.Usage.PromptTokens,
		OutputTokens: result.Usage.CompletionTokens,
	}, nil
}

func (d *DeepSeekAdapter) Stream(ctx context.Context, req *ChatRequest) (<-chan *ChatChunk, error) {
	dReq := map[string]interface{}{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   true,
	}
	if req.MaxTokens > 0 {
		dReq["max_tokens"] = req.MaxTokens
	}
	if req.Temperature != 0 {
		dReq["temperature"] = req.Temperature
	}

	body, _ := json.Marshal(dReq)
	httpReq, _ := http.NewRequestWithContext(ctx, "POST", d.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+d.apiKey)

	resp, err := d.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("deepseek stream request failed: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, fmt.Errorf("deepseek stream API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	ch := make(chan *ChatChunk, 64)
	go func() {
		defer resp.Body.Close()
		defer close(ch)

		buf := make([]byte, 0, 4096)
		tmp := make([]byte, 1024)
		for {
			n, err := resp.Body.Read(tmp)
			if n > 0 {
				buf = append(buf, tmp[:n]...)
				// Process complete SSE lines
				for {
					idx := bytes.IndexByte(buf, '\n')
					if idx < 0 {
						break
					}
					line := buf[:idx]
					buf = buf[idx+1:]
					line = bytes.TrimSpace(line)
					if len(line) == 0 {
						continue
					}
					if !bytes.HasPrefix(line, []byte("data: ")) {
						continue
					}
					data := line[6:]
					if string(data) == "[DONE]" {
						ch <- &ChatChunk{Finish: true}
						return
					}
					var chunk struct {
						Choices []struct {
							Delta struct {
								Content string `json:"content"`
							} `json:"delta"`
							FinishReason *string `json:"finish_reason"`
						} `json:"choices"`
						Usage *struct {
							PromptTokens     int `json:"prompt_tokens"`
							CompletionTokens int `json:"completion_tokens"`
						} `json:"usage"`
						Model string `json:"model"`
					}
					if err := json.Unmarshal(data, &chunk); err != nil {
						continue
					}
					if len(chunk.Choices) == 0 {
						continue
					}
					sc := &ChatChunk{
						Content: chunk.Choices[0].Delta.Content,
					}
					if chunk.Choices[0].FinishReason != nil {
						sc.Finish = true
						sc.StopReason = *chunk.Choices[0].FinishReason
					}
					ch <- sc
				}
			}
			if err != nil {
				if err != io.EOF {
					ch <- &ChatChunk{Finish: true, StopReason: fmt.Sprintf("error: %v", err)}
				}
				return
			}
		}
	}()

	return ch, nil
}
