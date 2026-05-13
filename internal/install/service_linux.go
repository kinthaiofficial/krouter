//go:build linux

package install

import "github.com/kinthaiofficial/krouter/internal/config"

func platformWriteService(binaryPath string) (string, error) {
	return config.WriteSystemdService(binaryPath)
}

func platformEnableService() error {
	return config.EnableSystemdService()
}
