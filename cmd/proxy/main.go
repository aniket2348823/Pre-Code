package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

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
				Port:         port,
				BackendURL:   backendURL,
				APIKey:       apiKey,
				OpenAIKey:    os.Getenv("OPENAI_API_KEY"),
				AnthropicKey: os.Getenv("ANTHROPIC_API_KEY"),
				GeminiKey:    os.Getenv("GEMINI_API_KEY"),
			}

			server := proxy.NewServer(cfg)
			addr := fmt.Sprintf(":%s", port)
			log.Printf("Starting proxy server on %s", addr)
			if err := http.ListenAndServe(addr, server.Router()); err != nil {
				log.Fatalf("Server failed: %v", err)
			}
		},
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
