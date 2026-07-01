package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

func chatCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "chat",
		Short: "Start an interactive chat session with an agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				return fmt.Errorf("API token required: set --token or VIGIL_API_TOKEN")
			}

			fmt.Println("🤖 VigilAgent Chat")
			fmt.Println("Type your message and press Enter. Type 'quit' or 'exit' to stop.")
			fmt.Println(strings.Repeat("-", 50))

			scanner := bufio.NewScanner(os.Stdin)
			for {
				fmt.Print("\n> ")
				if !scanner.Scan() {
					return nil
				}
				input := strings.TrimSpace(scanner.Text())
				if input == "" {
					continue
				}
				if input == "quit" || input == "exit" {
					fmt.Println("Goodbye!")
					return nil
				}

				fmt.Printf("🧑 You: %s\n", input)
				fmt.Println("🤖 Agent: [Chat integration requires API connection — coming soon]")
			}
		},
	}
}
