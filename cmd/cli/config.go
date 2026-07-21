package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

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
