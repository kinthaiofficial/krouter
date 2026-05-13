//go:build !darwin

package config

import (
	"errors"
	"fmt"
	"path"
)

var errDarwinOnly = errors.New("LaunchAgent is only supported on macOS")

// GeneratePlistContent returns the LaunchAgent plist XML.
// Available on all platforms for testing purposes.
func GeneratePlistContent(binaryPath, homeDir string) []byte {
	logDir := path.Join(homeDir, ".kinthai")
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN"
  "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>com.kinthai.router</string>
  <key>ProgramArguments</key>
  <array>
    <string>` + binaryPath + `</string>
    <string>serve</string>
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>StandardOutPath</key>
  <string>` + path.Join(logDir, "daemon.log") + `</string>
  <key>StandardErrorPath</key>
  <string>` + path.Join(logDir, "daemon-error.log") + `</string>
</dict>
</plist>
`)
}

// LaunchAgentPlistPath returns an error on non-macOS platforms.
func LaunchAgentPlistPath() (string, error) {
	return "", errDarwinOnly
}

// WriteLaunchAgentPlist returns an error on non-macOS platforms.
func WriteLaunchAgentPlist(_ string) (string, error) {
	return "", fmt.Errorf("WriteLaunchAgentPlist: %w", errDarwinOnly)
}

// LoadLaunchAgent returns an error on non-macOS platforms.
func LoadLaunchAgent(_ string) error {
	return fmt.Errorf("LoadLaunchAgent: %w", errDarwinOnly)
}

// UnloadLaunchAgent returns an error on non-macOS platforms.
func UnloadLaunchAgent(_ string) error {
	return fmt.Errorf("UnloadLaunchAgent: %w", errDarwinOnly)
}
