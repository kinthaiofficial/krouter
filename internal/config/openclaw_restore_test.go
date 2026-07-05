package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain pins KROUTER_CONFIG_DIR to a throwaway dir for the whole package so
// no test that calls ConnectOpenClaw/DisconnectOpenClaw can touch the real
// ~/.kinthai/openclaw-restore.json. Tests that assert on the restore file's
// content re-pin their own dir via pinRestoreDir.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "krouter-config-test-*")
	if err != nil {
		os.Exit(1)
	}
	_ = os.Setenv("KROUTER_CONFIG_DIR", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// pinRestoreDir points KROUTER_CONFIG_DIR at a fresh temp dir and returns the
// restore file path inside it.
func pinRestoreDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("KROUTER_CONFIG_DIR", dir)
	return filepath.Join(dir, "openclaw-restore.json")
}

func readRestoreFile(t *testing.T, path string) map[string]map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	var f struct {
		Files map[string]map[string]string `json:"files"`
	}
	require.NoError(t, json.Unmarshal(data, &f))
	return f.Files
}

// OpenClaw ≥2026.6.9 strictly validates models.providers.* and rejects unknown
// fields ("Invalid input" → gateway crash-loop), so the sidecar key must never
// appear in any file krouter writes for OpenClaw.
func TestConnectOpenClaw_WritesNoSidecarIntoConfigFiles(t *testing.T) {
	pinRestoreDir(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"apiKey":"sk"},"deepseek":{"baseUrl":"https://api.deepseek.com/v1","apiKey":"ds"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	subDir := filepath.Join(dir, "agents", "claude", "agent")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	subModels := filepath.Join(subDir, "models.json")
	require.NoError(t, os.WriteFile(subModels, []byte(`{"providers":{"minimax":{"baseUrl":"https://api.minimaxi.com/anthropic/v1","apiKey":"mm"}}}`), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	for _, path := range []string{cfg, subModels} {
		data, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.NotContains(t, string(data), "_krouterOriginal",
			"%s must not carry the sidecar key (OpenClaw 2026.6.9 rejects unknown provider fields)", path)
	}
}

func TestConnectOpenClaw_SavesOriginalsToRestoreFile(t *testing.T) {
	restorePath := pinRestoreDir(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com","apiKey":"sk"},"deepseek":{"baseUrl":"https://api.deepseek.com/v1","apiKey":"ds"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	files := readRestoreFile(t, restorePath)
	require.Contains(t, files, cfg)
	assert.Equal(t, "https://api.anthropic.com", files[cfg]["anthropic"])
	assert.Equal(t, "https://api.deepseek.com/v1", files[cfg]["deepseek"])
}

func TestDisconnectOpenClaw_RestoresFromRestoreFileAndRemovesIt(t *testing.T) {
	restorePath := pinRestoreDir(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"apiKey":"sk"},"deepseek":{"baseUrl":"https://api.deepseek.com/v1","apiKey":"ds"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))
	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, err := os.ReadFile(cfg)
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))
	ds := root["models"].(map[string]any)["providers"].(map[string]any)["deepseek"].(map[string]any)
	assert.Equal(t, "https://api.deepseek.com/v1", ds["baseUrl"], "baseUrl restored from the external restore file")

	_, err = os.Stat(restorePath)
	assert.True(t, os.IsNotExist(err), "restore file should be removed once nothing is left to restore")
}

// Configs connected by v2.5.0 carry the in-file sidecar. Reconnecting (e.g.
// after an upgrade) must strip it — self-healing the gateway crash — while
// keeping the recorded original so disconnect still restores it.
func TestConnectOpenClaw_MigratesLegacyInFileSidecar(t *testing.T) {
	restorePath := pinRestoreDir(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"apiKey":"sk"},"deepseek":{"baseUrl":"http://127.0.0.1:8402/a/openclaw/v1","_krouterOriginalBaseUrl":"https://api.deepseek.com/v1","apiKey":"ds"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	data, err := os.ReadFile(cfg)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "_krouterOriginal", "legacy sidecar must be stripped on reconnect")

	files := readRestoreFile(t, restorePath)
	assert.Equal(t, "https://api.deepseek.com/v1", files[cfg]["deepseek"], "legacy sidecar value migrated to the restore file")

	require.NoError(t, config.DisconnectOpenClaw(cfg))
	data, err = os.ReadFile(cfg)
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))
	ds := root["models"].(map[string]any)["providers"].(map[string]any)["deepseek"].(map[string]any)
	assert.Equal(t, "https://api.deepseek.com/v1", ds["baseUrl"])
}

// Legacy in-file sidecars are still honoured on disconnect even without a
// reconnect first (v2.5.0 → upgrade → straight uninstall).
func TestDisconnectOpenClaw_LegacyInFileSidecarStillRestores(t *testing.T) {
	pinRestoreDir(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	connected := `{"models":{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:8402/a/openclaw","api":"anthropic-messages","apiKey":"sk"},"deepseek":{"baseUrl":"http://127.0.0.1:8402/a/openclaw/v1","_krouterOriginalBaseUrl":"https://api.deepseek.com/v1","apiKey":"ds"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))

	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, err := os.ReadFile(cfg)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "_krouterOriginal")
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))
	ds := root["models"].(map[string]any)["providers"].(map[string]any)["deepseek"].(map[string]any)
	assert.Equal(t, "https://api.deepseek.com/v1", ds["baseUrl"])
}

// A user who already pointed a provider back at its vendor by hand (the field
// workaround for the 2026.6.9 crash) must not have that choice clobbered by a
// later disconnect replaying a stale recorded original.
func TestDisconnectOpenClaw_DoesNotClobberManuallyRestoredBaseURL(t *testing.T) {
	restorePath := pinRestoreDir(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"apiKey":"sk"},"deepseek":{"baseUrl":"https://api.deepseek.com/v1","apiKey":"ds"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))
	require.NoError(t, config.ConnectOpenClaw(cfg))

	// Simulate the manual fix: user rewrites deepseek's baseUrl themselves.
	data, err := os.ReadFile(cfg)
	require.NoError(t, err)
	patched := strings.Replace(string(data), "http://127.0.0.1:8402/a/openclaw/v1", "https://my-relay.example.com/v1", 1)
	require.NoError(t, os.WriteFile(cfg, []byte(patched), 0644))

	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, err = os.ReadFile(cfg)
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))
	ds := root["models"].(map[string]any)["providers"].(map[string]any)["deepseek"].(map[string]any)
	assert.Equal(t, "https://my-relay.example.com/v1", ds["baseUrl"], "manual endpoint must survive disconnect")

	_, err = os.Stat(restorePath)
	assert.True(t, os.IsNotExist(err), "stale entries are dropped after disconnect")
}

func TestConnectOpenClaw_SubAgentOriginalsRoundTripViaRestoreFile(t *testing.T) {
	restorePath := pinRestoreDir(t)
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"models":{"providers":{"anthropic":{"apiKey":"sk"}}}}`), 0644))

	subDir := filepath.Join(dir, "agents", "claude", "agent")
	require.NoError(t, os.MkdirAll(subDir, 0755))
	subModels := filepath.Join(subDir, "models.json")
	require.NoError(t, os.WriteFile(subModels, []byte(`{"providers":{"deepseek":{"baseUrl":"https://api.deepseek.com/v1","apiKey":"k"}}}`), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	files := readRestoreFile(t, restorePath)
	require.Contains(t, files, subModels, "sub-agent originals keyed by the models.json path")
	assert.Equal(t, "https://api.deepseek.com/v1", files[subModels]["deepseek"])

	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, err := os.ReadFile(subModels)
	require.NoError(t, err)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))
	ds := root["providers"].(map[string]any)["deepseek"].(map[string]any)
	assert.Equal(t, "https://api.deepseek.com/v1", ds["baseUrl"])
}
