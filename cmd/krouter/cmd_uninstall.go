package main

import (
	"fmt"

	"github.com/kinthaiofficial/krouter/internal/uninstall"
	"github.com/spf13/cobra"
)

func newUninstallCommand() *cobra.Command {
	var yes bool
	var dryRun bool
	var keepData bool

	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Remove krouter daemon and restore agent configs",
		Long: `Stops and removes the krouter user service, removes the krouter binary from
~/.local/bin/, restores AI agent config files to their pre-krouter state, and
optionally deletes ~/.kinthai/ (use --keep-data to preserve logs and config).

Use --yes to skip the confirmation prompt.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if !yes && !dryRun {
				ui := &ttyUI{out: cmd.OutOrStdout()}
				if !ui.Confirm("This will remove krouter and restore agent configs. Continue?") {
					fmt.Fprintln(cmd.OutOrStdout(), "Aborted.")
					return nil
				}
			}

			var ui uninstall.UI
			if yes || dryRun {
				ui = uninstall.NullUI{}
			} else {
				ui = &uninstallTTY{out: cmd.OutOrStdout()}
			}

			u := uninstall.New(ui, uninstall.Options{
				KeepData: keepData,
				DryRun:   dryRun,
			})

			if dryRun {
				fmt.Fprintln(cmd.OutOrStdout(), "Dry run — no changes will be made.")
				fmt.Fprintln(cmd.OutOrStdout())
			}

			if err := u.Uninstall(); err != nil {
				return err
			}

			if dryRun {
				fmt.Fprintln(cmd.OutOrStdout(), "\nDry run complete.")
			} else {
				fmt.Fprintln(cmd.OutOrStdout(), "\nUninstall complete.")
			}
			return nil
		},
	}

	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "Auto-confirm without prompting")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview steps without executing")
	cmd.Flags().BoolVar(&keepData, "keep-data", false, "Preserve ~/.kinthai/ (logs and config)")

	return cmd
}

type uninstallTTY struct {
	out interface{ Write([]byte) (int, error) }
}

func (u *uninstallTTY) Progress(msg string) { fmt.Fprintln(u.out, msg) }
func (u *uninstallTTY) Warn(msg string)     { fmt.Fprintln(u.out, "  ⚠ "+msg) }
