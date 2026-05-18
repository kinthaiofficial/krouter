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

// launchctlTarget returns the per-user bootstrap target (e.g. "gui/501").
func launchctlTarget() string {
	return fmt.Sprintf("gui/%d", os.Getuid())
}

// LoadLaunchAgent registers and starts the daemon via launchctl bootstrap.
// Uses bootout first (synchronous — waits for the old process to fully exit)
// then bootstraps the new binary, ensuring the ports are always free.
func LoadLaunchAgent(plistPath string) error {
	target := launchctlTarget()
	// Ignore bootout error: it fails if the service was never loaded (first install).
	_ = exec.Command("launchctl", "bootout", target, plistPath).Run()
	out, err := exec.Command("launchctl", "bootstrap", target, plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %w — %s", err, out)
	}
	return nil
}

// UnloadLaunchAgent stops and unregisters the daemon via launchctl bootout.
func UnloadLaunchAgent(plistPath string) error {
	target := launchctlTarget()
	out, err := exec.Command("launchctl", "bootout", target, plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl bootout: %w — %s", err, out)
	}
	return nil
}

// StopLaunchAgent stops the running daemon via launchctl bootout.
// bootout is synchronous — it waits until the process has fully exited.
func StopLaunchAgent() error {
	plistPath, err := LaunchAgentPlistPath()
	if err != nil {
		return err
	}
	return UnloadLaunchAgent(plistPath)
}

// StartLaunchAgent starts the daemon via launchctl bootstrap.
func StartLaunchAgent() error {
	plistPath, err := LaunchAgentPlistPath()
	if err != nil {
		return err
	}
	target := launchctlTarget()
	out, err := exec.Command("launchctl", "bootstrap", target, plistPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("launchctl bootstrap: %w — %s", err, out)
	}
	return nil
}
