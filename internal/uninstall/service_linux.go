//go:build linux

package uninstall

import "github.com/kinthaiofficial/krouter/internal/config"

func platformStopService() error {
	return config.DisableSystemdService()
}

func platformRemoveServiceFile() error {
	path, err := config.SystemdServicePath()
	if err != nil {
		return err
	}
	return removeServiceFileByPath(path)
}
