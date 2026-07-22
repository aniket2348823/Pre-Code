package main

import (
	"fmt"
	"log/slog"
	"os"

	vigilmcp "github.com/vigilagent/vigilagent/internal/mcp"
)

func main() {
	// Configuration via environment variables
	apiURL := os.Getenv("VIGILAGENT_API_URL")
	if apiURL == "" {
		apiURL = "http://localhost:8080"
	}

	apiKey := os.Getenv("VIGILAGENT_API_KEY")
	if apiKey == "" {
		fmt.Fprintln(os.Stderr, "error: VIGILAGENT_API_KEY environment variable is required")
		os.Exit(1)
	}

	// Optional: user's LLM key for BYOK automation (e.g. CI/CD pipelines)
	llmKey := os.Getenv("VIGILAGENT_LLM_KEY")

	// Configure logging to stderr (stdout is reserved for MCP protocol)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelWarn,
	})))

	slog.Info("starting VigilAgent MCP server", "api_url", apiURL)

	server := vigilmcp.NewServer(apiURL, apiKey, llmKey)
	if err := server.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "MCP server error: %v\n", err)
		os.Exit(1)
	}
}
