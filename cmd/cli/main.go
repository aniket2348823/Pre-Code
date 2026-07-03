package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	baseURL string
	token   string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "vigil",
		Short: "VigilAgent CLI — AI agent management platform",
		Long:  `VigilAgent is an AI agent management platform with real-time monitoring, analytics, and control.`,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if baseURL == "" {
				baseURL = os.Getenv("VIGIL_API_URL")
				if baseURL == "" {
					baseURL = "http://localhost:8080"
				}
			}
			if token == "" {
				token = os.Getenv("VIGIL_API_TOKEN")
			}
			return nil
		},
	}

	rootCmd.PersistentFlags().StringVar(&baseURL, "url", "", "API base URL (default: http://localhost:8080)")
	rootCmd.PersistentFlags().StringVar(&token, "token", "", "API token (or set VIGIL_API_TOKEN)")

	rootCmd.AddCommand(
		configCmd(),
		versionCmd(),
	)

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
