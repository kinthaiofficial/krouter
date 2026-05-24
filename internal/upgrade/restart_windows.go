//go:build windows

package upgrade

import (
	"os"
	"os/exec"
)

// Restart spawns a new process from os.Args[0] and exits the current one.
// Windows has no exec syscall, so we start a child and exit; the new process
// binds the ports a moment after the parent releases them.
func Restart() error {
	cmd := exec.Command(os.Args[0], os.Args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Start(); err != nil {
		return err
	}
	os.Exit(0)
	return nil // unreachable
}
