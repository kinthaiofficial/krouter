// Package main is the entry point for the krouter binary.
//
// This single binary serves multiple roles via subcommands:
//   - serve:       run as daemon (managed by LaunchAgent/systemd/Task Scheduler)
//   - status:      query daemon status
//   - shell-init:  output shell integration code
//   - config:      get/set configuration
//   - budget:      view current quota usage
//   - logs:        view recent logs
//   - test:        send test request to verify routing
//   - version:     show version info
//   - remote:      enable/disable remote daemon access
//   - pair:        generate or validate pairing token
//
// See spec/06-cli.md for full CLI design.
// See spec/01-proxy-layer.md for daemon implementation.
package main

import (
	"fmt"
	"os"
)

// These are set at build time via -ldflags.
var (
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	if err := newRootCommand().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
