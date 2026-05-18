package main

import (
	"fmt"
	"runtime"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/spf13/cobra"
)

func newStartCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "start",
		Short: "Start the krouter background daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := startDaemon(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "krouter daemon started.")
			return nil
		},
	}
}

func startDaemon() error {
	switch runtime.GOOS {
	case "darwin":
		return config.StartLaunchAgent()
	case "linux":
		return config.StartSystemdService()
	case "windows":
		return config.StartTask()
	default:
		return fmt.Errorf("start: unsupported platform %s", runtime.GOOS)
	}
}
