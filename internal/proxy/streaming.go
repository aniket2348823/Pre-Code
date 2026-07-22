package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (s *ProxyServer) handleStreaming(w http.ResponseWriter, r *http.Request, provider *ProviderConfig, requestBody []byte, defaultFormat string) {
	url := provider.BaseURL + r.URL.Path
	req, err := http.NewRequestWithContext(r.Context(), "POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		http.Error(w, "Failed to create request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	
	if provider.Name == "openai" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	} else if provider.Name == "anthropic" {
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if provider.Name == "gemini" {
		req.Header.Set("x-goog-api-key", provider.APIKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		http.Error(w, "Failed to forward stream: "+err.Error(), http.StatusBadGateway)
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
	var fullContent string

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
						fullContent += oResp.Choices[0].Delta.Content
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
						fullContent += aResp.Delta.Text
					}
				}
			}
		}
		w.Write([]byte(line))
		flusher.Flush()
	}

	blocks := ExtractCodeBlocks(fullContent)
	var results []*AnalysisResult
	for _, block := range blocks {
		res, err := AnalyzeCode(r.Context(), s.cfg.BackendURL, s.cfg.APIKey, block.Code, block.Language)
		if err == nil {
			results = append(results, res)
		}
	}

	summary := FormatAnalysisSummary(results)
	if summary != "" {
		if defaultFormat == "openai" {
			summaryChunk := map[string]interface{}{
				"choices": []map[string]interface{}{
					{
						"delta": map[string]interface{}{
							"content": "\n\n" + summary,
							"role":    "assistant",
						},
					},
				},
			}
			chunkBytes, _ := json.Marshal(summaryChunk)
			fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
		} else if defaultFormat == "anthropic" {
			summaryChunk := map[string]interface{}{
				"type": "content_block_delta",
				"delta": map[string]interface{}{
					"type": "text_delta",
					"text": "\n\n" + summary,
				},
			}
			chunkBytes, _ := json.Marshal(summaryChunk)
			fmt.Fprintf(w, "data: %s\n\n", chunkBytes)
		}
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
