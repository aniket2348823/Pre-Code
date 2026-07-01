package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

func initCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [project-name]",
		Short: "Initialize a VigilAgent project",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			projectName := ".vigil"
			if len(args) > 0 {
				projectName = args[0]
			}

			configDir := filepath.Join(".", projectName)
			if err := os.MkdirAll(configDir, 0755); err != nil {
				return fmt.Errorf("failed to create project directory: %w", err)
			}

			configFile := filepath.Join(configDir, "config.yaml")
			content := `# VigilAgent Project Configuration
project:
  name: ` + projectName + `
  description: ""

# LLM Provider settings
llm:
  primary: openai
  fallback:
    - anthropic
  models:
    openai:
      model: gpt-4o
      max_tokens: 8192
    anthropic:
      model: claude-sonnet-4-20250514
      max_tokens: 8192

# Agent settings
agent:
  max_iterations: 20
  max_retries: 3
  require_approval: false

# Budget settings
budget:
  daily_limit_usd: 10.00
  per_task_limit_usd: 1.00
`

			if err := os.WriteFile(configFile, []byte(content), 0644); err != nil {
				return fmt.Errorf("failed to write config: %w", err)
			}

			fmt.Printf("✅ Project initialized: %s/\n", projectName)
			fmt.Printf("   Config: %s\n", configFile)
			fmt.Println("\nNext steps:")
			fmt.Println("  1. Set your API key:  export VIGIL_API_TOKEN=your_token")
			fmt.Println("  2. Start a chat:     vigil chat")
			fmt.Println("  3. Create a task:    vigil task create \"Fix the bug\"")
			return nil
		},
	}
}
