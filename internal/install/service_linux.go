//go:build linux

package install

import (
	"os"

	"github.com/kinthaiofficial/krouter/internal/config"
)

func platformWriteService(binaryPath string) (string, error) {
	return config.WriteSystemdService(binaryPath)
}

func platformEnableService() error {
	return config.EnableSystemdService()
}

func platformDaemonPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return home + "/.local/bin/krouter", nil
}
