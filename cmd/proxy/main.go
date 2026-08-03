package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
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
				apiKey = "test-secret-key"
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

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			go server.StartHealthChecks(ctx, 2*time.Minute)

			addr := fmt.Sprintf(":%s", port)
			httpServer := &http.Server{
				Addr:    addr,
				Handler: server.Router(),
			}

			serverErr := make(chan error, 1)
			go func() {
				if cfg.TLSCertFile != "" && cfg.TLSKeyFile != "" {
					log.Printf("Starting proxy server on %s (TLS)", addr)
					serverErr <- httpServer.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
				} else {
					log.Printf("Starting proxy server on %s", addr)
					serverErr <- httpServer.ListenAndServe()
				}
			}()

			quit := make(chan os.Signal, 1)
			signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

			select {
			case err := <-serverErr:
				if err != nil && err != http.ErrServerClosed {
					log.Fatalf("Server failed: %v", err)
				}
			case <-quit:
				log.Println("Shutting down proxy server...")
			}

			cancel()

			shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer shutdownCancel()

			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				log.Fatalf("Proxy server forced to shutdown: %v", err)
			}

			log.Println("Proxy server stopped")
		},
	}

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
