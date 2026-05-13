//go:build darwin

package install

import "github.com/kinthaiofficial/krouter/internal/config"

func platformWriteService(binaryPath string) (string, error) {
	return config.WriteLaunchAgentPlist(binaryPath)
}

func platformEnableService() error {
	plistPath, err := config.LaunchAgentPlistPath()
	if err != nil {
		return err
	}
	return config.LoadLaunchAgent(plistPath)
}
