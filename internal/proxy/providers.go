package proxy

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

// ProviderConfig holds routing info for a single LLM provider.
type ProviderConfig struct {
	Name    string
	BaseURL string
	APIKey  string
}

// validateTargetURL parses the URL, resolves its host, and rejects
// loopback / private / unspecified addresses (SSRF guard).
func validateTargetURL(rawURL string) error {
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", parsedURL.Scheme)
	}
	host := parsedURL.Hostname()
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return fmt.Errorf("failed to resolve host: %s", host)
	}
	for _, ip := range ips {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
			return fmt.Errorf("disallowed target IP: %s", ip)
		}
		if ip.IsPrivate() {
			return fmt.Errorf("disallowed target IP: %s", ip)
		}
	}
	return nil
}

// RouteRequest determines the provider from the model name prefix.
// Used as fallback when no BYOK header is provided.
func RouteRequest(model string, cfg *Config) *ProviderConfig {
	// No mock/test provider path: every route targets a real upstream or nil.
	if cfg.OpenRouterKey != "" && (strings.Contains(model, "/") || strings.HasSuffix(model, ":free")) {
		return &ProviderConfig{Name: "openrouter", BaseURL: "https://openrouter.ai/api", APIKey: cfg.OpenRouterKey}
	}
	switch {
	case strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1-") || strings.HasPrefix(model, "o3-") || strings.HasPrefix(model, "o4-") || strings.HasPrefix(model, "gpt-4.5"):
		return &ProviderConfig{Name: "openai", BaseURL: "https://api.openai.com", APIKey: cfg.OpenAIKey}
	case strings.HasPrefix(model, "claude-"):
		return &ProviderConfig{Name: "anthropic", BaseURL: "https://api.anthropic.com", APIKey: cfg.AnthropicKey}
	case strings.HasPrefix(model, "gemini-"):
		return &ProviderConfig{Name: "gemini", BaseURL: "https://generativelanguage.googleapis.com", APIKey: cfg.GeminiKey}
	case strings.HasSuffix(model, ":free") || (cfg.OpenRouterKey != "" && strings.Contains(model, "/")):
		return &ProviderConfig{Name: "openrouter", BaseURL: "https://openrouter.ai/api", APIKey: cfg.OpenRouterKey}
	case strings.HasPrefix(model, "kimi-") || strings.HasPrefix(model, "deepseek-") || strings.HasPrefix(model, "nvidia/") || strings.HasPrefix(model, "meta/") || strings.HasPrefix(model, "mistralai/") || strings.HasPrefix(model, "moonshotai/") || strings.HasPrefix(model, "qwen/") || strings.HasPrefix(model, "deepseek-ai/"):
		return &ProviderConfig{Name: "nvidia", BaseURL: "https://build.nvidia.com", APIKey: cfg.NVIDIAKey}
	case strings.HasPrefix(model, "llama-") || strings.HasPrefix(model, "mixtral-") || strings.HasPrefix(model, "gemma"):
		return &ProviderConfig{Name: "groq", BaseURL: "https://api.groq.com", APIKey: cfg.GroqKey}
	case strings.HasPrefix(model, "mistral") || strings.HasPrefix(model, "open-mixtral") || strings.HasPrefix(model, "codestral") || strings.HasPrefix(model, "pixtral"):
		return &ProviderConfig{Name: "mistral", BaseURL: "https://api.mistral.ai", APIKey: cfg.MistralKey}
	case strings.HasPrefix(model, "command"):
		return &ProviderConfig{Name: "cohere", BaseURL: "https://api.cohere.com", APIKey: cfg.CohereKey}
	case strings.Contains(model, "/"):
		// OpenRouter uses provider/model format (e.g., "anthropic/claude-opus-4")
		return &ProviderConfig{Name: "openrouter", BaseURL: "https://openrouter.ai", APIKey: cfg.OpenRouterKey}
	default:
		return nil
	}
}

// forwardToProvider sends the request to the real LLM provider and returns the raw response.
//
//lint:ignore U1000 used by providers_test.go
func forwardToProvider(ctx context.Context, client *http.Client, provider *ProviderConfig, requestBody []byte, path string) ([]byte, error) {
	rawURL := provider.BaseURL + path

	req, err := http.NewRequestWithContext(ctx, "POST", rawURL, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	// Set auth headers based on provider
	switch provider.Name {
	case "openai", "nvidia", "groq", "mistral", "openrouter", "deepseek":
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	case "anthropic":
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	case "gemini":
		req.Header.Set("x-goog-api-key", provider.APIKey)
	case "cohere":
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(io.LimitReader(resp.Body, 10<<20)) // 10MB limit
}
