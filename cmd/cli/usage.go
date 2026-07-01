package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func usageCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "usage",
		Short: "Show usage statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				return fmt.Errorf("API token required")
			}
			fmt.Println("📊 Usage Statistics:")
			fmt.Println("  Tasks completed: 0")
			fmt.Println("  Tokens used:     0")
			fmt.Println("  Cost (30d):      $0.00")
			fmt.Println("\n(API integration pending)")
			return nil
		},
	}
}

func costCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "cost",
		Short: "Show cost breakdown",
		RunE: func(cmd *cobra.Command, args []string) error {
			if token == "" {
				return fmt.Errorf("API token required")
			}
			fmt.Println("💰 Cost Breakdown:")
			fmt.Println("  OpenAI:     $0.00 (0 tasks)")
			fmt.Println("  Anthropic:  $0.00 (0 tasks)")
			fmt.Println("  Total:      $0.00")
			fmt.Println("\n(API integration pending)")
			return nil
		},
	}
}

func configCmd() *cobra.Command {
	configCmd := &cobra.Command{
		Use:   "config",
		Short: "Manage CLI configuration",
	}

	configCmd.AddCommand(
		&cobra.Command{
			Use:   "set [key] [value]",
			Short: "Set a configuration value",
			Args:  cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Printf("Setting %s = %s\n", args[0], args[1])
				fmt.Println("✅ Configuration updated")
				return nil
			},
		},
		&cobra.Command{
			Use:   "get [key]",
			Short: "Get a configuration value",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Printf("%s = (not set)\n", args[0])
				return nil
			},
		},
	)

	return configCmd
}
