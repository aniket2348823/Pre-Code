package proxy

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"

	"github.com/go-chi/chi/v5"
)

type Config struct {
	Port         string
	BackendURL   string
	APIKey       string
	OpenAIKey    string
	AnthropicKey string
	GeminiKey    string
}

type ProxyServer struct {
	cfg    Config
	router *chi.Mux
	client *http.Client
}

func NewServer(cfg Config) *ProxyServer {
	s := &ProxyServer{
		cfg:    cfg,
		router: chi.NewRouter(),
		client: &http.Client{},
	}
	s.routes()
	return s
}

func (s *ProxyServer) Router() http.Handler {
	return s.router
}

func (s *ProxyServer) routes() {
	s.router.Get("/health", s.handleHealth)
	s.router.Post("/v1/chat/completions", s.handleChatCompletions)
	s.router.Post("/v1/messages", s.handleMessages)
}

func (s *ProxyServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

func (s *ProxyServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
	s.handleProxyRequest(w, r, "openai")
}

func (s *ProxyServer) handleMessages(w http.ResponseWriter, r *http.Request) {
	s.handleProxyRequest(w, r, "anthropic")
}

type baseRequest struct {
	Model  string `json:"model"`
	Stream bool   `json:"stream"`
}

func (s *ProxyServer) handleProxyRequest(w http.ResponseWriter, r *http.Request, defaultFormat string) {
	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body", http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

	var req baseRequest
	if err := json.Unmarshal(bodyBytes, &req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	provider := RouteRequest(req.Model, &s.cfg)
	if provider == nil {
		http.Error(w, "Unsupported model", http.StatusBadRequest)
		return
	}

	if req.Stream {
		s.handleStreaming(w, r, provider, bodyBytes, defaultFormat)
		return
	}

	respBody, err := ForwardToProvider(r.Context(), s.client, provider, bodyBytes, r.URL.Path)
	if err != nil {
		http.Error(w, "Failed to forward request: "+err.Error(), http.StatusBadGateway)
		return
	}

	var content string
	if defaultFormat == "openai" {
		var oResp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		json.Unmarshal(respBody, &oResp)
		if len(oResp.Choices) > 0 {
			content = oResp.Choices[0].Message.Content
		}
	} else if defaultFormat == "anthropic" {
		var aResp struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		}
		json.Unmarshal(respBody, &aResp)
		for _, c := range aResp.Content {
			if c.Type == "text" {
				content += c.Text
			}
		}
	}

	blocks := ExtractCodeBlocks(content)
	var results []*AnalysisResult
	for _, block := range blocks {
		res, err := AnalyzeCode(r.Context(), s.cfg.BackendURL, s.cfg.APIKey, block.Code, block.Language)
		if err != nil {
			log.Printf("AnalyzeCode error: %v", err)
			continue
		}
		results = append(results, res)
	}

	summary := FormatAnalysisSummary(results)
	if summary != "" {
		if defaultFormat == "openai" {
			var oResp map[string]interface{}
			json.Unmarshal(respBody, &oResp)
			choices, ok := oResp["choices"].([]interface{})
			if ok && len(choices) > 0 {
				choice, ok2 := choices[0].(map[string]interface{})
				if ok2 {
					message, ok3 := choice["message"].(map[string]interface{})
					if ok3 {
						origContent, _ := message["content"].(string)
						message["content"] = origContent + "\n\n" + summary
						respBody, _ = json.Marshal(oResp)
					}
				}
			}
		} else if defaultFormat == "anthropic" {
			var aResp map[string]interface{}
			json.Unmarshal(respBody, &aResp)
			contentArr, ok := aResp["content"].([]interface{})
			if ok && len(contentArr) > 0 {
				lastIdx := len(contentArr) - 1
				lastItem, ok2 := contentArr[lastIdx].(map[string]interface{})
				if ok2 && lastItem["type"] == "text" {
					origContent, _ := lastItem["text"].(string)
					lastItem["text"] = origContent + "\n\n" + summary
					respBody, _ = json.Marshal(aResp)
				}
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(respBody)
}
