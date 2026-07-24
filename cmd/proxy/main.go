package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"
	"github.com/vigilagent/vigilagent/internal/proxy"
)

func main() {
	var rootCmd = &cobra.Command{
		Use:   "proxy",
		Short: "VigilAgent LLM Proxy Gateway",
		Run: func(cmd *cobra.Command, args []string) {
			port := os.Getenv("VIGILAGENT_PROXY_PORT")
			if port == "" {
				port = "9090"
			}
			backendURL := os.Getenv("VIGILAGENT_BACKEND_URL")
			if backendURL == "" {
				backendURL = "http://localhost:8080"
			}
			apiKey := os.Getenv("VIGILAGENT_API_KEY")
			if apiKey == "" {
				log.Fatal("VIGILAGENT_API_KEY is required")
			}

			cfg := proxy.Config{
				Port:           port,
				BackendURL:     backendURL,
				APIKey:         apiKey,
				AllowedAPIKeys: os.Getenv("VIGILAGENT_ALLOWED_KEYS"),
				TLSCertFile:    os.Getenv("VIGILAGENT_TLS_CERT"),
				TLSKeyFile:     os.Getenv("VIGILAGENT_TLS_KEY"),
				OpenAIKey:      os.Getenv("OPENAI_API_KEY"),
				AnthropicKey:   os.Getenv("ANTHROPIC_API_KEY"),
				GeminiKey:      os.Getenv("GEMINI_API_KEY"),
				NVIDIAKey:      os.Getenv("NVIDIA_API_KEY"),
				GroqKey:        os.Getenv("GROQ_API_KEY"),
				MistralKey:     os.Getenv("MISTRAL_API_KEY"),
				CohereKey:      os.Getenv("COHERE_API_KEY"),
				OpenRouterKey:  os.Getenv("OPENROUTER_API_KEY"),
				DeepSeekKey:    os.Getenv("DEEPSEEK_API_KEY"),
			}

			server := proxy.NewServer(cfg)

			// Start periodic health checks (every 2 minutes)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go server.StartHealthChecks(ctx, 2*time.Minute)

			addr := fmt.Sprintf(":%s", port)

			// TLS support
			if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
				log.Printf("Starting proxy server on %s (TLS)", addr)
				if err := http.ListenAndServeTLS(addr, cfg.TLSCertFile, cfg.TLSKeyFile, server.Router()); err != nil {
					log.Fatalf("TLS server failed: %v", err)
				}
			} else {
				log.Printf("Starting proxy server on %s", addr)
				if err := http.ListenAndServe(addr, server.Router()); err != nil {
					log.Fatalf("Server failed: %v", err)
				}
			}
		},
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
