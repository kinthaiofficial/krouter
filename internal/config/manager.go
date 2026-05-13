// Package config handles user settings (~/.kinthai/settings.json).
//
// Settings are watched via fsnotify. GUI / CLI / daemon all read and write the same file.
// Atomic writes (tempfile + rename) prevent half-written state.
//
// Fields:
//   - Preset:                 "saver" | "balanced" | "quality"
//   - Language:               "en" | "zh-CN"
//   - DaemonURL:              hidden field (default http://127.0.0.1:8403),
//                             for M4+ remote daemon support (spec 10)
//   - NotificationCategories: per-type enable flags
//   - BudgetWarnings:         per-quota threshold settings
//
// See spec/05-storage.md §4 for full schema.
package config

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// Settings is the JSON-serializable user settings.
type Settings struct {
	Preset                 string             `json:"preset"`
	Language               string             `json:"language"`
	DaemonURL              string             `json:"daemon_url,omitempty"`
	NotificationCategories map[string]bool    `json:"notification_categories"`
	BudgetWarnings         map[string]float64 `json:"budget_warnings"`
}

func applyDefaults(s Settings) Settings {
	if s.Preset == "" {
		s.Preset = "balanced"
	}
	if s.Language == "" {
		s.Language = "en"
	}
	return s
}

// Manager watches and persists settings.
type Manager struct {
	path string
	mu   sync.RWMutex
}

// New creates a settings manager. Path defaults to ~/.kinthai/settings.json.
func New(path string) *Manager {
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".kinthai", "settings.json")
	}
	return &Manager{path: path}
}

// Start is a no-op placeholder (fsnotify hot-reload deferred to M1.6).
func (m *Manager) Start(_ context.Context) error { return nil }

// Get reads settings from disk and returns them with defaults applied.
// If the file is missing or corrupt, defaults are returned without error.
func (m *Manager) Get() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()

	data, err := os.ReadFile(m.path)
	if err != nil {
		return applyDefaults(Settings{})
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return applyDefaults(Settings{})
	}
	return applyDefaults(s)
}

// Set writes settings atomically via temp file + rename (0600 perms).
func (m *Manager) Set(s Settings) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(m.path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, "settings-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, m.path)
}
