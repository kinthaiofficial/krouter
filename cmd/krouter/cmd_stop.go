package main

import (
	"fmt"
	"runtime"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/spf13/cobra"
)

func newStopCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the krouter background daemon",
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := stopDaemon(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "krouter daemon stopped.")
			return nil
		},
	}
}

func stopDaemon() error {
	switch runtime.GOOS {
	case "darwin":
		return config.StopLaunchAgent()
	case "linux":
		return config.StopSystemdService()
	case "windows":
		return config.StopTask()
	default:
		return fmt.Errorf("stop: unsupported platform %s", runtime.GOOS)
	}
}
