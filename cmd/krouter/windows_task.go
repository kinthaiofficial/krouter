//go:build windows

package main

import (
	"fmt"
	"os"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/spf13/cobra"
)

func newTaskInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "task-install",
		Short:  "Register Task Scheduler user task (Windows, called by installer)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			exe, err := os.Executable()
			if err != nil {
				return fmt.Errorf("locate executable: %w", err)
			}
			if err := config.RegisterTask(exe); err != nil {
				return fmt.Errorf("register task: %w", err)
			}
			fmt.Println("Task Scheduler task registered.")
			return nil
		},
	}
}
