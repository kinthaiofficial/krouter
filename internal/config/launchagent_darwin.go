//go:build darwin

package config

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// GeneratePlistContent returns the LaunchAgent plist XML for the given binary path.
// Exported so tests can call it cross-platform.
func GeneratePlistContent(binaryPath, homeDir string) []byte {
	logDir := filepath.Join(homeDir, ".kinthai")
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
  <string>` + filepath.Join(logDir, "daemon.log") + `</string>
  <key>StandardErrorPath</key>
  <string>` + filepath.Join(logDir, "daemon-error.log") + `</string>
</dict>
</plist>
`)
}

// LaunchAgentPlistPath returns the canonical plist file path.
func LaunchAgentPlistPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", "com.kinthai.router.plist"), nil
}

// WriteLaunchAgentPlist writes the LaunchAgent plist to ~/Library/LaunchAgents/.
// Returns the plist file path.
func WriteLaunchAgentPlist(binaryPath string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("launchagent: home dir: %w", err)
	}

	dir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("launchagent: mkdir: %w", err)
	}

	plistPath := filepath.Join(dir, "com.kinthai.router.plist")
	content := GeneratePlistContent(binaryPath, home)

	if err := os.WriteFile(plistPath, content, 0644); err != nil {
		return "", fmt.Errorf("launchagent: write plist: %w", err)
	}
	return plistPath, nil
}

// LoadLaunchAgent registers and starts the daemon via launchctl.
func LoadLaunchAgent(plistPath string) error {
	out, err := exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl load: %w — %s", err, out)
	}
	return nil
}

// UnloadLaunchAgent stops and unregisters the daemon via launchctl.
func UnloadLaunchAgent(plistPath string) error {
	out, err := exec.Command("launchctl", "unload", plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl unload: %w — %s", err, out)
	}
	return nil
}
