//go:build darwin

package uninstall

import "github.com/kinthaiofficial/krouter/internal/config"

func platformStopService() error {
	path, err := config.LaunchAgentPlistPath()
	if err != nil {
		return err
	}
	return config.UnloadLaunchAgent(path)
}

func platformRemoveServiceFile() error {
	path, err := config.LaunchAgentPlistPath()
	if err != nil {
		return err
	}
	return removeServiceFileByPath(path)
}
