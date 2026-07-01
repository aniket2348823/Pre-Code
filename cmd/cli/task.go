package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func taskCmd() *cobra.Command {
	taskCmd := &cobra.Command{
		Use:   "task",
		Short: "Manage tasks",
	}

	taskCmd.AddCommand(
		&cobra.Command{
			Use:   "create [prompt]",
			Short: "Create a new task",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if token == "" {
					return fmt.Errorf("API token required")
				}
				prompt := args[0]
				projectID, _ := cmd.Flags().GetString("project")
				fmt.Printf("Creating task: %s\n", prompt)
				fmt.Printf("Project: %s\n", projectID)
				fmt.Println("✅ Task created (API integration pending)")
				return nil
			},
		},
		&cobra.Command{
			Use:   "list",
			Short: "List tasks for a project",
			RunE: func(cmd *cobra.Command, args []string) error {
				if token == "" {
					return fmt.Errorf("API token required")
				}
				projectID, _ := cmd.Flags().GetString("project")
				fmt.Printf("Listing tasks for project: %s\n", projectID)
				fmt.Println("📋 No tasks found (API integration pending)")
				return nil
			},
		},
		&cobra.Command{
			Use:   "get [task-id]",
			Short: "Get task details",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if token == "" {
					return fmt.Errorf("API token required")
				}
				fmt.Printf("Getting task: %s\n", args[0])
				fmt.Println("📋 Task details (API integration pending)")
				return nil
			},
		},
		&cobra.Command{
			Use:   "cancel [task-id]",
			Short: "Cancel a running task",
			Args:  cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				if token == "" {
					return fmt.Errorf("API token required")
				}
				fmt.Printf("Cancelling task: %s\n", args[0])
				fmt.Println("✅ Task cancelled (API integration pending)")
				return nil
			},
		},
	)

	for _, c := range taskCmd.Commands() {
		c.Flags().StringP("project", "p", "", "Project ID")
	}

	return taskCmd
}
