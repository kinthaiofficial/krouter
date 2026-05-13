package config

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// DataDir returns ~/.kinthai, creating it if it does not exist.
func DataDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: home dir: %w", err)
	}
	dir := filepath.Join(home, ".kinthai")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("config: create data dir: %w", err)
	}
	return dir, nil
}

// IsInstalled reports whether the daemon has been installed (marker file exists).
func IsInstalled() bool {
	home, err := os.UserHomeDir()
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(home, ".kinthai", "installed"))
	return err == nil
}

// MarkInstalled creates the ~/.kinthai/installed marker file.
func MarkInstalled() error {
	dir, err := DataDir()
	if err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(dir, "installed"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("config: mark installed: %w", err)
	}
	return f.Close()
}

// InstallDaemon copies srcBinary to ~/.local/bin/krouter with 0755 perms.
// Returns the destination path.
func InstallDaemon(srcBinary string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("config: home dir: %w", err)
	}

	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		return "", fmt.Errorf("config: create bin dir: %w", err)
	}

	dst := filepath.Join(binDir, "krouter")

	src, err := os.Open(srcBinary)
	if err != nil {
		return "", fmt.Errorf("config: open src binary: %w", err)
	}
	defer func() { _ = src.Close() }()

	tmp, err := os.CreateTemp(binDir, "krouter-*.tmp")
	if err != nil {
		return "", fmt.Errorf("config: create temp: %w", err)
	}
	tmpName := tmp.Name()

	if _, err := io.Copy(tmp, src); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("config: copy binary: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	if err := os.Chmod(tmpName, 0755); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("config: chmod: %w", err)
	}
	if err := os.Rename(tmpName, dst); err != nil {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("config: rename: %w", err)
	}
	return dst, nil
}
