//go:build !windows

package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newTaskInstallCommand is a no-op stub on non-Windows platforms.
func newTaskInstallCommand() *cobra.Command {
	return &cobra.Command{
		Use:    "task-install",
		Short:  "Register Task Scheduler user task (Windows only)",
		Hidden: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return fmt.Errorf("task-install is only supported on Windows")
		},
	}
}
