//go:build !windows

package upgrade

import (
	"os"
	"syscall"
)

// Restart replaces the current process image with the (just-updated) binary
// at os.Args[0]. On Unix this is a true exec — same PID, same open sockets —
// so port :8402/:8403 are not freed and re-bound.
func Restart() error {
	return syscall.Exec(os.Args[0], os.Args, os.Environ())
}
