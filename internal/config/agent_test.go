package config_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── OpenClaw ─────────────────────────────────────────────────────────────────

func TestConnectOpenClaw_SetsBaseURL(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"apiKey":"sk-ant"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	provider := root["models"].(map[string]any)["providers"].(map[string]any)["anthropic"].(map[string]any)
	assert.Equal(t, "http://127.0.0.1:8402", provider["baseUrl"])
	assert.Equal(t, "anthropic-messages", provider["api"])
}

func TestConnectOpenClaw_CreatesBackup(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{}`), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	entries, _ := os.ReadDir(dir)
	backupFound := false
	for _, e := range entries {
		if strings.Contains(e.Name(), ".kinthai-bak-") {
			backupFound = true
		}
	}
	assert.True(t, backupFound, "expected backup file")
}

func TestDisconnectOpenClaw_RemovesBaseURL(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	connected := `{"models":{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:8402","api":"anthropic-messages","apiKey":"${ANTHROPIC_API_KEY}"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))

	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	provider := root["models"].(map[string]any)["providers"].(map[string]any)["anthropic"].(map[string]any)
	assert.NotContains(t, provider, "baseUrl")
	assert.NotContains(t, provider, "api")
}

// ── Cursor ────────────────────────────────────────────────────────────────────

func TestConnectCursor_SetsFields(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"editor.fontSize":14}`), 0644))

	require.NoError(t, config.ConnectCursor(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	assert.Equal(t, "http://127.0.0.1:8402", root["cursor.anthropic.baseUrl"])
	assert.Equal(t, "http://127.0.0.1:8402/v1", root["cursor.openai.baseUrl"])
	assert.Equal(t, float64(14), root["editor.fontSize"]) // pre-existing field preserved
}

func TestDisconnectCursor_RemovesFields(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "settings.json")
	connected := `{"cursor.anthropic.baseUrl":"http://127.0.0.1:8402","cursor.openai.baseUrl":"http://127.0.0.1:8402/v1","editor.fontSize":14}`
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))

	require.NoError(t, config.DisconnectCursor(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	assert.NotContains(t, root, "cursor.anthropic.baseUrl")
	assert.NotContains(t, root, "cursor.openai.baseUrl")
	assert.Equal(t, float64(14), root["editor.fontSize"])
}

// ── Claude Code ───────────────────────────────────────────────────────────────

func TestConnectClaudeCode_AppendsMarker(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	require.NoError(t, os.WriteFile(rc, []byte("# existing config\n"), 0644))

	require.NoError(t, config.ConnectClaudeCode(rc))

	content, _ := os.ReadFile(rc)
	s := string(content)
	assert.Contains(t, s, "# >>> krouter shell integration >>>")
	assert.Contains(t, s, "eval \"$(krouter shell-init)\"")
	assert.Contains(t, s, "# <<< krouter shell integration <<<")
	assert.Contains(t, s, "# existing config")
}

func TestConnectClaudeCode_Idempotent(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")

	require.NoError(t, config.ConnectClaudeCode(rc))
	require.NoError(t, config.ConnectClaudeCode(rc)) // second call must be no-op

	content, _ := os.ReadFile(rc)
	count := strings.Count(string(content), "# >>> krouter shell integration >>>")
	assert.Equal(t, 1, count)
}

func TestDisconnectClaudeCode_RemovesMarker(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	require.NoError(t, os.WriteFile(rc, []byte("# existing\n"), 0644))

	require.NoError(t, config.ConnectClaudeCode(rc))
	require.NoError(t, config.DisconnectClaudeCode(rc))

	content, _ := os.ReadFile(rc)
	s := string(content)
	assert.NotContains(t, s, "krouter shell integration")
	assert.Contains(t, s, "# existing")
}

func TestShellInitOutput_Bash(t *testing.T) {
	out := config.ShellInitOutput("bash")
	assert.Contains(t, out, `export ANTHROPIC_BASE_URL="http://localhost:8402"`)
	assert.Contains(t, out, `export OPENAI_BASE_URL="http://localhost:8402/v1"`)
}

func TestShellInitOutput_Fish(t *testing.T) {
	out := config.ShellInitOutput("fish")
	assert.Contains(t, out, `set -gx ANTHROPIC_BASE_URL "http://localhost:8402"`)
	assert.Contains(t, out, `set -gx OPENAI_BASE_URL "http://localhost:8402/v1"`)
}

// ── Hermes ────────────────────────────────────────────────────────────────────

func TestConnectHermes_SetsBaseURL(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	initial := "[providers]\n[providers.anthropic]\nmodel = \"claude-sonnet\"\n"
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectHermes(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	_, err := toml.Decode(string(data), &root)
	require.NoError(t, err)

	providers := root["providers"].(map[string]any)
	anthropic := providers["anthropic"].(map[string]any)
	assert.Equal(t, "http://127.0.0.1:8402", anthropic["base_url"])
}

func TestDisconnectHermes_RemovesBaseURL(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	connected := "[providers]\n[providers.anthropic]\nbase_url = \"http://127.0.0.1:8402\"\nmodel = \"claude-sonnet\"\n"
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))

	require.NoError(t, config.DisconnectHermes(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	_, err := toml.Decode(string(data), &root)
	require.NoError(t, err)

	providers := root["providers"].(map[string]any)
	anthropic := providers["anthropic"].(map[string]any)
	assert.NotContains(t, anthropic, "base_url")
}
