package main

import "github.com/spf13/cobra"

// newRootCommand wires up all subcommands.
//
// CLAUDE NOTE: Each subcommand should be in its own file (cmd/krouter/{serve,status,...}.go).
// Keep this file thin — just composition.
func newRootCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "krouter",
		Short: "krouter — local LLM proxy that saves you tokens",
		Long: `krouter runs as a background daemon that proxies LLM requests
from your local AI agents (OpenClaw, Claude Code, Cursor, etc.) and routes
them to the cheapest suitable provider, saving you tokens transparently.

The same binary is used as daemon, CLI, and GUI helper. See subcommands.`,
		Version: Version,
	}

	cmd.AddCommand(
		newServeCommand(),
		newStatusCommand(),
		newShellInitCommand(),
		newConfigCommand(),
		newBudgetCommand(),
		newLogsCommand(),
		newTestCommand(),
		newVersionCommand(),
		newRemoteCommand(),
		newPairCommand(),
		newTaskInstallCommand(),
		newInstallCommand(),
		newUninstallCommand(),
	)

	return cmd
}
