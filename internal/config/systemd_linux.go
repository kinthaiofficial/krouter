//go:build linux

package config

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
)

// GenerateServiceContent returns the systemd user service unit for the given binary path.
// Exported so tests can call it cross-platform.
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

// SystemdServicePath returns the canonical service file path (~/.config/systemd/user/).
func SystemdServicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "systemd", "user", "krouter.service"), nil
}

// WriteSystemdService writes the service unit file to ~/.config/systemd/user/.
// Returns the service file path.
func WriteSystemdService(binaryPath string) (string, error) {
	servicePath, err := SystemdServicePath()
	if err != nil {
		return "", fmt.Errorf("systemd: service path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(servicePath), 0755); err != nil {
		return "", fmt.Errorf("systemd: mkdir: %w", err)
	}

	content := GenerateServiceContent(binaryPath)
	if err := os.WriteFile(servicePath, content, 0644); err != nil {
		return "", fmt.Errorf("systemd: write service: %w", err)
	}
	return servicePath, nil
}

// prepareUserSession ensures the systemd user session is reachable.
// On SSH servers without an active login session:
//   - linger may be disabled (user bus dies when SSH session ends)
//   - XDG_RUNTIME_DIR may be empty (dbus socket path not set)
//
// Returns the env slice to pass to systemctl exec.Command calls.
func prepareUserSession() ([]string, error) {
	// Enable linger so the user session persists after logout.
	// loginctl enable-linger with no argument targets the current user.
	// Ignore errors — the user may lack polkit rights but linger may already be on.
	u, _ := user.Current()
	if u != nil {
		_ = exec.Command("loginctl", "enable-linger", u.Username).Run()
	}

	env := os.Environ()
	if os.Getenv("XDG_RUNTIME_DIR") == "" {
		env = append(env, fmt.Sprintf("XDG_RUNTIME_DIR=/run/user/%d", os.Getuid()))
	}
	return env, nil
}

// EnableSystemdService runs "systemctl --user enable --now krouter".
func EnableSystemdService() error {
	env, err := prepareUserSession()
	if err != nil {
		return err
	}
	cmd := exec.Command("systemctl", "--user", "enable", "--now", "krouter")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl enable: %w — %s", err, out)
	}
	return nil
}

// DisableSystemdService runs "systemctl --user disable --now krouter".
func DisableSystemdService() error {
	env, err := prepareUserSession()
	if err != nil {
		return err
	}
	cmd := exec.Command("systemctl", "--user", "disable", "--now", "krouter")
	cmd.Env = env
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl disable: %w — %s", err, out)
	}
	return nil
}
