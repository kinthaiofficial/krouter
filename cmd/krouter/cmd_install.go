package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/kinthaiofficial/krouter/internal/install"
	"github.com/spf13/cobra"
)

func newInstallCommand() *cobra.Command {
	var yes bool
	var dryRun bool
	var skipAgents bool

	cmd := &cobra.Command{
		Use:   "install",
		Short: "Install krouter daemon and connect your AI agents",
		Long: `Copies the krouter binary to ~/.local/bin/, registers it as a user service
(systemd on Linux, LaunchAgent on macOS), writes shell integration to your RC
file, and patches detected AI agent config files to route through krouter.

Use --yes to skip all confirmation prompts.
Use --dry-run to preview steps without making any changes.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			var ui install.UI
			if yes {
				ui = install.NullUI{}
			} else {
				ui = &ttyUI{out: cmd.OutOrStdout()}
			}

			orch := install.New(ui, install.Options{
				DryRun:     dryRun,
				SkipAgents: skipAgents,
			})

			if dryRun {
				fmt.Fprintln(cmd.OutOrStdout(), "Dry run — no changes will be made.\n")
			}

			if err := orch.Install(); err != nil {
				return err
			}

			if dryRun {
				fmt.Fprintln(cmd.OutOrStdout(), "\nDry run complete. Run without --dry-run to apply.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "\nInstallation complete.")
				fmt.Fprintln(cmd.OutOrStdout(), "Run: krouter serve   (or restart your shell to apply PATH changes)")
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Auto-confirm all prompts")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview steps without executing")
	cmd.Flags().BoolVar(&skipAgents, "skip-agents", false, "Skip agent config connection")

	return cmd
}

// ttyUI is a UI implementation that writes to a terminal and reads confirmations from stdin.
type ttyUI struct {
	out interface{ Write([]byte) (int, error) }
}

func (u *ttyUI) Progress(msg string) {
	fmt.Fprintln(u.out, msg)
}

func (u *ttyUI) Warn(msg string) {
	fmt.Fprintln(u.out, "  ⚠ "+msg)
}

func (u *ttyUI) Confirm(question string) bool {
	fmt.Fprintf(u.out, "%s [y/N] ", question)
	scanner := bufio.NewScanner(os.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}
