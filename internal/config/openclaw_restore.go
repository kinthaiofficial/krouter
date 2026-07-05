package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// openClawRestoreFileName holds the original provider baseUrls that
// ConnectOpenClaw replaced, keyed by config file path then provider name, so
// DisconnectOpenClaw can restore them exactly.
//
// This lives in krouter's own data dir — NOT inside OpenClaw's config files.
// OpenClaw ≥2026.6.9 strictly validates models.providers.* and rejects unknown
// fields ("Invalid input" → gateway refuses to start), so the old in-file
// _krouterOriginalBaseUrl sidecar crashed the gateway on takeover.
const openClawRestoreFileName = "openclaw-restore.json"

type openClawRestore struct {
	// Files maps an absolute config path (openclaw.json or a sub-agent
	// models.json) to provider name → original baseUrl.
	Files map[string]map[string]string `json:"files"`
}

// openClawRestorePath resolves the restore file location the same way
// agentscan.PendingFileDir does — $KROUTER_CONFIG_DIR override first, else
// ~/.kinthai — so the installer and the daemon always agree on it regardless
// of shell-specific env vars. Returns "" when no home dir is available.
func openClawRestorePath() string {
	if dir := os.Getenv("KROUTER_CONFIG_DIR"); dir != "" {
		return filepath.Join(dir, openClawRestoreFileName)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kinthai", openClawRestoreFileName)
}

// loadOpenClawRestore reads the restore file. A missing or unreadable file
// yields an empty (never nil) map — disconnect then simply has nothing to
// restore from the store.
func loadOpenClawRestore() openClawRestore {
	r := openClawRestore{Files: map[string]map[string]string{}}
	path := openClawRestorePath()
	if path == "" {
		return r
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return r
	}
	_ = json.Unmarshal(data, &r)
	if r.Files == nil {
		r.Files = map[string]map[string]string{}
	}
	return r
}

// saveOpenClawRestore writes the restore file atomically, removing it when
// nothing is left to restore.
func saveOpenClawRestore(r openClawRestore) error {
	path := openClawRestorePath()
	if path == "" {
		return fmt.Errorf("openclaw restore: no home dir")
	}
	for k, v := range r.Files {
		if len(v) == 0 {
			delete(r.Files, k)
		}
	}
	if len(r.Files) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	return writeJSON(path, r)
}

// recordOpenClawOriginals merges the given provider → original-baseUrl map for
// configPath into the restore file. New values overwrite old ones: a live
// non-krouter baseUrl seen at connect time is by definition the user's current
// true endpoint.
func recordOpenClawOriginals(configPath string, originals map[string]string) error {
	if len(originals) == 0 {
		return nil
	}
	r := loadOpenClawRestore()
	m := r.Files[configPath]
	if m == nil {
		m = map[string]string{}
		r.Files[configPath] = m
	}
	for k, v := range originals {
		m[k] = v
	}
	return saveOpenClawRestore(r)
}

// openClawOriginalsFor returns the recorded originals for configPath (nil when
// none).
func openClawOriginalsFor(configPath string) map[string]string {
	return loadOpenClawRestore().Files[configPath]
}

// clearOpenClawOriginals drops configPath's entry from the restore file.
func clearOpenClawOriginals(configPath string) error {
	r := loadOpenClawRestore()
	if _, ok := r.Files[configPath]; !ok {
		return nil
	}
	delete(r.Files, configPath)
	return saveOpenClawRestore(r)
}
