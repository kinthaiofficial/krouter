//go:build !linux

package config

import (
	"errors"
	"fmt"
)

var errLinuxOnly = errors.New("systemd --user is only supported on Linux")

// GenerateServiceContent returns the systemd user service unit for the given binary path.
// Available on all platforms for testing purposes.
func GenerateServiceContent(binaryPath string) []byte {
	return []byte(`[Unit]
Description=krouter – local LLM proxy
After=network.target

[Service]
Type=simple
ExecStart=` + binaryPath + ` serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`)
}

// SystemdServicePath returns an error on non-Linux platforms.
func SystemdServicePath() (string, error) {
	return "", errLinuxOnly
}

// WriteSystemdService returns an error on non-Linux platforms.
func WriteSystemdService(_ string) (string, error) {
	return "", fmt.Errorf("WriteSystemdService: %w", errLinuxOnly)
}

// EnableSystemdService returns an error on non-Linux platforms.
func EnableSystemdService() error {
	return fmt.Errorf("EnableSystemdService: %w", errLinuxOnly)
}

// DisableSystemdService returns an error on non-Linux platforms.
func DisableSystemdService() error {
	return fmt.Errorf("DisableSystemdService: %w", errLinuxOnly)
}
