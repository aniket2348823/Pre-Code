package proxy

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
)

type ProviderConfig struct {
	Name          string
	BaseURL       string
	APIKey        string
	ModelPrefixes []string
}

func RouteRequest(model string, cfg *Config) *ProviderConfig {
	if strings.HasPrefix(model, "gpt-") || strings.HasPrefix(model, "o1-") || strings.HasPrefix(model, "o3-") {
		return &ProviderConfig{
			Name:          "openai",
			BaseURL:       "https://api.openai.com",
			APIKey:        cfg.OpenAIKey,
			ModelPrefixes: []string{"gpt-", "o1-", "o3-"},
		}
	}
	if strings.HasPrefix(model, "claude-") {
		return &ProviderConfig{
			Name:          "anthropic",
			BaseURL:       "https://api.anthropic.com",
			APIKey:        cfg.AnthropicKey,
			ModelPrefixes: []string{"claude-"},
		}
	}
	if strings.HasPrefix(model, "gemini-") {
		return &ProviderConfig{
			Name:          "gemini",
			BaseURL:       "https://generativelanguage.googleapis.com",
			APIKey:        cfg.GeminiKey,
			ModelPrefixes: []string{"gemini-"},
		}
	}
	// NVIDIA NIM: supports kimi-k2.6, deepseek, llama, mistral, etc.
	if strings.HasPrefix(model, "kimi-") ||
		strings.HasPrefix(model, "deepseek-") ||
		strings.HasPrefix(model, "nvidia/") ||
		strings.HasPrefix(model, "meta/") ||
		strings.HasPrefix(model, "mistralai/") {
		return &ProviderConfig{
			Name:          "nvidia",
			BaseURL:       "https://integrate.api.nvidia.com",
			APIKey:        cfg.NVIDIAKey,
			ModelPrefixes: []string{"kimi-", "deepseek-", "nvidia/", "meta/", "mistralai/"},
		}
	}
	return nil
}

func ForwardToProvider(ctx context.Context, client *http.Client, provider *ProviderConfig, requestBody []byte, path string) ([]byte, error) {
	url := provider.BaseURL + path
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	
	if provider.Name == "openai" || provider.Name == "nvidia" {
		req.Header.Set("Authorization", "Bearer "+provider.APIKey)
	} else if provider.Name == "anthropic" {
		req.Header.Set("x-api-key", provider.APIKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	} else if provider.Name == "gemini" {
		req.Header.Set("x-goog-api-key", provider.APIKey)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	return io.ReadAll(resp.Body)
}
