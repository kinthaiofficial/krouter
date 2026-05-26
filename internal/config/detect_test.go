package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetectInstalledApps_NoAgents(t *testing.T) {
	withHome(t) // clean home with no agent config files
	agents := config.DetectInstalledApps()
	// claude-code may appear if "claude" is on PATH; only check file-based agents.
	for _, a := range agents {
		assert.NotEqual(t, "openclaw", a.Name)
		assert.NotEqual(t, "hermes", a.Name)
		assert.NotEqual(t, "cursor", a.Name)
	}
}

func TestDetectInstalledApps_OpenClaw(t *testing.T) {
	home := withHome(t)
	dir := filepath.Join(home, ".openclaw")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "openclaw.json"), []byte("{}"), 0644))

	agents := config.DetectInstalledApps()
	found := false
	for _, a := range agents {
		if a.Name == "openclaw" {
			found = true
			assert.Equal(t, filepath.Join(home, ".openclaw", "openclaw.json"), a.ConfigPath)
		}
	}
	assert.True(t, found, "expected openclaw to be detected")
}

func TestDetectInstalledApps_Cursor(t *testing.T) {
	home := withHome(t)
	dir := filepath.Join(home, ".cursor")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "settings.json"), []byte("{}"), 0644))

	agents := config.DetectInstalledApps()
	found := false
	for _, a := range agents {
		if a.Name == "cursor" {
			found = true
		}
	}
	assert.True(t, found, "expected cursor to be detected")
}

func TestDetectInstalledApps_Hermes(t *testing.T) {
	home := withHome(t)
	dir := filepath.Join(home, ".hermes")
	require.NoError(t, os.MkdirAll(dir, 0755))
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.toml"), []byte(""), 0644))

	agents := config.DetectInstalledApps()
	found := false
	for _, a := range agents {
		if a.Name == "hermes" {
			found = true
		}
	}
	assert.True(t, found, "expected hermes to be detected")
}

func TestDetectInstalledApps_ClaudeCode_KnownPath(t *testing.T) {
	// Simulate the claude binary in ~/.claude/local/claude (npm install -g path)
	// with a PATH that contains no directories (LaunchAgent scenario: no claude in PATH).
	home := withHome(t)
	t.Setenv("PATH", "") // empty PATH — exec.LookPath will always fail

	claudeDir := filepath.Join(home, ".claude", "local")
	require.NoError(t, os.MkdirAll(claudeDir, 0755))
	claudePath := filepath.Join(claudeDir, "claude")
	require.NoError(t, os.WriteFile(claudePath, []byte("#!/bin/sh\necho claude"), 0755))

	agents := config.DetectInstalledApps()
	found := false
	for _, a := range agents {
		if a.Name == "claude-code" {
			found = true
			assert.Equal(t, claudePath, a.CLIPath)
		}
	}
	assert.True(t, found, "claude-code must be found via known path fallback when not in PATH")
}
