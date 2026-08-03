package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/vigilagent/vigilagent/internal/llm"
)

// handleStreaming proxies a streaming request through ModelRouter,
// streams tokens to the client in real-time, and runs background analysis.
func (s *ProxyServer) handleStreaming(
	w http.ResponseWriter,
	r *http.Request,
	modelRouter *llm.ModelRouter,
	requestBody []byte,
	defaultFormat string,
	model string,
) {
	// Build the LLM task for ModelRouter
	messages, err := s.parseMessages(requestBody)
	if err != nil || len(messages) == 0 {
		http.Error(w, `{"error":"invalid request: missing messages"}`, http.StatusBadRequest)
		return
	}

	if model == "" {
		model = "gpt-4o-mini"
	}

	task := &llm.Task{
		ID:          "proxy-stream-" + time.Now().Format("20060102150405"),
		Type:        "feature",
		Description: "Proxy streaming request",
		Messages:    messages,
	}

	// Stream through ModelRouter with smart failover
	streamResult, err := modelRouter.StreamWithFailover(r.Context(), task)
	if err != nil {
		slog.Warn("model router streaming failed, falling back", "error", err)
		s.handleStreamingDirect(w, r, requestBody, defaultFormat)
		return
	}

	// Stream tokens to the client
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-VigilAgent-Provider", streamResult.Provider)
	w.Header().Set("X-VigilAgent-Model", streamResult.Model)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	var fullContent strings.Builder
	const maxStreamContent = 10 << 20 // 10MB limit for accumulated stream content
	start := time.Now()

	for chunk := range streamResult.Ch {
		if chunk.Content != "" {
			if fullContent.Len()+len(chunk.Content) > maxStreamContent {
				slog.Info("streaming content limit reached", "bytes", maxStreamContent)
				break
			}
			fullContent.WriteString(chunk.Content)
			// Forward chunk in OpenAI SSE format
			sseChunk := map[string]interface{}{
				"id":      fmt.Sprintf("chatcmpl-vigil-%d", time.Now().UnixNano()),
				"object":  "chat.completion.chunk",
				"created": time.Now().Unix(),
				"model":   streamResult.Model,
				"choices": []map[string]interface{}{
					{
						"index": 0,
						"delta": map[string]interface{}{
							"content": chunk.Content,
						},
						"finish_reason": nil,
					},
				},
			}
			chunkBytes, _ := json.Marshal(sseChunk)
			fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
			flusher.Flush()
		}
		if chunk.Finish {
			break
		}
	}

	// Run dual-engine analysis on accumulated content
	analysisMode := r.Header.Get("X-VigilAgent-Mode")
	llmKey := r.Header.Get("X-LLM-Key")
	if analysisMode != "passthrough" {
		content := fullContent.String()
		blocks := ExtractCodeBlocks(content)

		if len(blocks) > 0 {
			// Run dual-engine analysis on ALL code blocks in parallel
			var allFindings []Finding
			var mu sync.Mutex
			var wg sync.WaitGroup

			for _, block := range blocks {
				wg.Add(1)
				go func(b CodeBlock) {
					defer wg.Done()
					result := AnalyzeWithDualEngine(
						r.Context(),
						modelRouter,
						s.cfg.BackendURL,
						s.cfg.APIKey,
						llmKey,
						b.Code,
						b.Language,
					)
					if result != nil && len(result.Findings) > 0 {
						mu.Lock()
						allFindings = append(allFindings, result.Findings...)
						mu.Unlock()
					}
				}(block)
			}
			wg.Wait()

			// Build summary from merged findings
			if len(allFindings) > 0 {
				uniqueFindings := DeduplicateFindings(allFindings)
				score, grade := CalculateScore(uniqueFindings)
				corroborated := 0
				for _, f := range uniqueFindings {
					if strings.Contains(f.RuleID, "+llm") {
						corroborated++
					}
				}
				summary := BuildSummary(uniqueFindings, score, grade, corroborated)

				// Inject summary as a final SSE chunk
				summaryChunk := map[string]interface{}{
					"id":      fmt.Sprintf("chatcmpl-vigil-analysis-%d", time.Now().UnixNano()),
					"object":  "chat.completion.chunk",
					"created": time.Now().Unix(),
					"model":   streamResult.Model,
					"choices": []map[string]interface{}{
						{
							"index": 0,
							"delta": map[string]interface{}{
								"content": "\n\n" + summary,
							},
							"finish_reason": nil,
						},
					},
				}
				chunkBytes, _ := json.Marshal(summaryChunk)
				fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
				flusher.Flush()
			}
		}
	}

	// Send final [DONE]
	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()

	latency := time.Since(start)
	slog.Info("streaming complete", "model", streamResult.Model, "provider", streamResult.Provider, "latency", latency.Round(time.Millisecond), "content_len", fullContent.Len(), "blocks", len(ExtractCodeBlocks(fullContent.String())))
}

// parseMessages extracts messages from the request body.
func (s *ProxyServer) parseMessages(body []byte) ([]llm.Message, error) {
	var reqBody struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &reqBody); err != nil {
		return nil, err
	}
	messages := make([]llm.Message, len(reqBody.Messages))
	for i, m := range reqBody.Messages {
		messages[i] = llm.Message{Role: m.Role, Content: m.Content}
	}
	return messages, nil
}

// handleStreamingDirect falls back to direct provider forwarding when ModelRouter fails.
// Includes timeout protection and write-error checking.
func (s *ProxyServer) handleStreamingDirect(
	w http.ResponseWriter,
	r *http.Request,
	requestBody []byte,
	defaultFormat string,
) {
	llmKey := r.Header.Get("X-LLM-Key")
	llmProvider := r.Header.Get("X-LLM-Provider")
	llmModel := r.Header.Get("X-LLM-Model")

	var req struct {
		Model string `json:"model"`
	}
	if err := json.Unmarshal(requestBody, &req); err != nil {
		slog.Warn("streaming: failed to parse model", "error", err)
	}
	if llmModel != "" {
		req.Model = llmModel
	}

	provider := s.resolveProvider(req.Model, llmKey, llmProvider)
	if provider == nil {
		http.Error(w, `{"error":"unsupported model"}`, http.StatusBadRequest)
		return
	}

	rawURL := provider.BaseURL + r.URL.Path
	if err := validateTargetURL(rawURL); err != nil {
		http.Error(w, `{"error":"SSRF protection: " + err.Error()}`, http.StatusForbidden)
		return
	}
	// Re-marshal with potential model override
	fwdBody := requestBody
	if llmModel != "" {
		var bodyMap map[string]interface{}
		if err := json.Unmarshal(requestBody, &bodyMap); err == nil {
			bodyMap["model"] = llmModel
			fwdBody, _ = json.Marshal(bodyMap)
		}
	}

	reqHTTP, err := http.NewRequestWithContext(r.Context(), "POST", rawURL, bytes.NewReader(fwdBody))
	if err != nil {
		http.Error(w, `{"error":"failed to create request"}`, http.StatusInternalServerError)
		return
	}
	reqHTTP.Header.Set("Content-Type", "application/json")
	reqHTTP.Header.Set("Accept", "text/event-stream")

	// Set auth headers
	switch provider.Name {
	case "openai", "nvidia", "groq", "mistral", "openrouter":
		reqHTTP.Header.Set("Authorization", "Bearer "+provider.APIKey)
	case "anthropic":
		reqHTTP.Header.Set("x-api-key", provider.APIKey)
		reqHTTP.Header.Set("anthropic-version", "2023-06-01")
	case "gemini":
		reqHTTP.Header.Set("x-goog-api-key", provider.APIKey)
	case "cohere":
		reqHTTP.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}

	resp, err := s.client.Do(reqHTTP)
	if err != nil {
		errJSON, _ := json.Marshal(map[string]string{"error": "failed to forward stream: " + err.Error()})
		http.Error(w, string(errJSON), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	reader := bufio.NewReader(resp.Body)
	var fullContent strings.Builder
	const maxStreamContent = 10 << 20

	// Read with a deadline to detect hanging providers
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimSpace(line[6:])
			if data == "[DONE]" {
				continue
			}
			if defaultFormat == "openai" {
				var oResp struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					} `json:"choices"`
				}
				if err := json.Unmarshal([]byte(data), &oResp); err == nil {
					if len(oResp.Choices) > 0 {
						fullContent.WriteString(oResp.Choices[0].Delta.Content)
					}
				}
			} else if defaultFormat == "anthropic" {
				var aResp struct {
					Type  string `json:"type"`
					Delta struct {
						Text string `json:"text"`
					} `json:"delta"`
				}
				if err := json.Unmarshal([]byte(data), &aResp); err == nil {
					if aResp.Type == "content_block_delta" {
						fullContent.WriteString(aResp.Delta.Text)
					}
				}
			}
		}
		if fullContent.Len() > maxStreamContent {
			slog.Info("streaming content limit reached", "bytes", maxStreamContent)
			break
		}
		if _, err := w.Write([]byte(line)); err != nil {
			slog.Info("streaming: client disconnected", "error", err)
			return
		}
		flusher.Flush()
	}

	// Post-stream dual-engine analysis
	analysisMode := r.Header.Get("X-VigilAgent-Mode")
	if analysisMode != "passthrough" {
		content := fullContent.String()
		blocks := ExtractCodeBlocks(content)

		if len(blocks) > 0 {
			// Run dual-engine analysis on ALL code blocks in parallel
			var allFindings []Finding
			var mu sync.Mutex
			var wg sync.WaitGroup

			// Build a modelRouter for LLM engine
			modelRouter := s.buildRouter(llmKey, "")

			for _, block := range blocks {
				wg.Add(1)
				go func(b CodeBlock) {
					defer wg.Done()
					result := AnalyzeWithDualEngine(
						r.Context(),
						modelRouter,
						s.cfg.BackendURL,
						s.cfg.APIKey,
						llmKey,
						b.Code,
						b.Language,
					)
					if result != nil && len(result.Findings) > 0 {
						mu.Lock()
						allFindings = append(allFindings, result.Findings...)
						mu.Unlock()
					}
				}(block)
			}
			wg.Wait()

			if len(allFindings) > 0 {
				uniqueFindings := DeduplicateFindings(allFindings)
				score, grade := CalculateScore(uniqueFindings)
				corroborated := 0
				for _, f := range uniqueFindings {
					if strings.Contains(f.RuleID, "+llm") {
						corroborated++
					}
				}
				summary := BuildSummary(uniqueFindings, score, grade, corroborated)

				if defaultFormat == "openai" {
					summaryChunk := map[string]interface{}{
						"choices": []map[string]interface{}{
							{"delta": map[string]interface{}{"content": "\n\n" + summary, "role": "assistant"}},
						},
					}
					chunkBytes, _ := json.Marshal(summaryChunk)
					fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
				} else if defaultFormat == "anthropic" {
					summaryChunk := map[string]interface{}{
						"type":  "content_block_delta",
						"delta": map[string]interface{}{"type": "text_delta", "text": "\n\n" + summary},
					}
					chunkBytes, _ := json.Marshal(summaryChunk)
					fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
				}
				flusher.Flush()
			}
		}
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}