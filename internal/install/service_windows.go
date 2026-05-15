//go:build windows

package install

import "github.com/kinthaiofficial/krouter/internal/config"

func platformWriteService(binaryPath string) (string, error) {
	if err := config.RegisterTask(binaryPath); err != nil {
		return "", err
	}
	return binaryPath + " (Task Scheduler: " + config.TaskName() + ")", nil
}

func platformEnableService() error {
	return config.StartTask()
}

func platformDaemonPath() (string, error) {
	return config.DefaultDaemonPath()
}
