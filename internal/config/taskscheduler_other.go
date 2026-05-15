//go:build !windows

package config

import "errors"

var errWindowsOnly = errors.New("task scheduler is only supported on Windows")

// TaskName is a stub on non-Windows platforms.
func TaskName() string { return "" }

// GenerateTaskXML is a stub on non-Windows platforms.
func GenerateTaskXML(binaryPath string) ([]byte, error) {
	return nil, errWindowsOnly
}

// DefaultDaemonPath is a stub on non-Windows platforms.
func DefaultDaemonPath() (string, error) {
	return "", errWindowsOnly
}

// RegisterTask is a stub on non-Windows platforms.
func RegisterTask(binaryPath string) error {
	return errWindowsOnly
}

// StartTask is a stub on non-Windows platforms.
func StartTask() error {
	return errWindowsOnly
}

// UnregisterTask is a stub on non-Windows platforms.
func UnregisterTask() error {
	return errWindowsOnly
}

// SetEnvRegistry is a stub on non-Windows platforms.
func SetEnvRegistry(key, value string) error {
	return errWindowsOnly
}
