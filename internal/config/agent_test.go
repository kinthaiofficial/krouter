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

func TestConnectOpenClaw_SetsBaseURLAndApi(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"apiKey":"sk-ant-real"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	provider := root["models"].(map[string]any)["providers"].(map[string]any)["anthropic"].(map[string]any)
	assert.Equal(t, "http://127.0.0.1:8402", provider["baseUrl"])
	assert.Equal(t, "anthropic-messages", provider["api"])
	// Real apiKey must be preserved — never overwritten with placeholder.
	assert.Equal(t, "sk-ant-real", provider["apiKey"])
	// models must be present as an array (OpenClaw schema requires non-nil array;
	// empty is valid — OpenClaw loads its catalog via plugin discovery, not this field).
	models, ok := provider["models"].([]any)
	assert.True(t, ok, "models must be an array")
	assert.NotNil(t, models, "models must not be nil")
}

func TestConnectOpenClaw_PreservesExistingModels(t *testing.T) {
	// If the user already has models configured, krouter must not overwrite them.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"apiKey":"sk-real","models":["custom-model-1","custom-model-2"]}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	provider := root["models"].(map[string]any)["providers"].(map[string]any)["anthropic"].(map[string]any)
	models, _ := provider["models"].([]any)
	require.Len(t, models, 2, "user's custom models must be preserved unchanged")
	assert.Equal(t, "custom-model-1", models[0])
	assert.Equal(t, "custom-model-2", models[1])
}

func TestConnectOpenClaw_NoApiKeyInjectedWhenMissing(t *testing.T) {
	// User never configured anthropic — krouter must not inject a placeholder.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"minimax":{"apiKey":"mm-real"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	provider := root["models"].(map[string]any)["providers"].(map[string]any)["anthropic"].(map[string]any)
	assert.Equal(t, "http://127.0.0.1:8402", provider["baseUrl"])
	assert.NotContains(t, provider, "apiKey", "krouter must not inject apiKey placeholder")
	// Other providers must be untouched.
	minimax := root["models"].(map[string]any)["providers"].(map[string]any)["minimax"].(map[string]any)
	assert.Equal(t, "mm-real", minimax["apiKey"])
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

func TestDisconnectOpenClaw_RemovesBaseURLAndApi(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	// Simulate a clean new-style install (no apiKey).
	connected := `{"models":{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:8402","api":"anthropic-messages","apiKey":"sk-real"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))

	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	provider := root["models"].(map[string]any)["providers"].(map[string]any)["anthropic"].(map[string]any)
	assert.NotContains(t, provider, "baseUrl")
	assert.NotContains(t, provider, "api")
	// Real apiKey must survive disconnect.
	assert.Equal(t, "sk-real", provider["apiKey"])
}

func TestDisconnectOpenClaw_RemovesOldPlaceholderApiKey(t *testing.T) {
	// Old krouter versions wrote "${ANTHROPIC_API_KEY}" as a literal string.
	// Disconnect removes it and, since no real apiKey remains, also cleans up
	// the entire krouter-created anthropic section.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	old := `{"models":{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:8402","api":"anthropic-messages","apiKey":"${ANTHROPIC_API_KEY}"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(old), 0644))

	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	providers := root["models"].(map[string]any)["providers"].(map[string]any)
	assert.NotContains(t, providers, "anthropic", "placeholder-only anthropic section must be fully removed")
}

func TestDisconnectOpenClaw_RemovesKrouterAddedSectionWhenNoRealKey(t *testing.T) {
	// When the user had no anthropic provider before krouter, ConnectOpenClaw
	// creates the whole section. DisconnectOpenClaw must remove it entirely.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	// Simulate what ConnectOpenClaw writes when user had no prior anthropic config.
	connected := `{"models":{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:8402","api":"anthropic-messages","models":["claude-sonnet-4-5"]}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))

	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	providers := root["models"].(map[string]any)["providers"].(map[string]any)
	assert.NotContains(t, providers, "anthropic", "krouter-created anthropic section must be fully removed")
}

func TestDisconnectOpenClaw_PreservesRealApiKeyAndCustomModels(t *testing.T) {
	// When the user had their own anthropic config (real apiKey + custom models),
	// disconnect must keep both intact and only remove krouter fields.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	connected := `{"models":{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:8402","api":"anthropic-messages","apiKey":"sk-real","models":["custom-model-1","custom-model-2"]}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))

	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	provider := root["models"].(map[string]any)["providers"].(map[string]any)["anthropic"].(map[string]any)
	assert.NotContains(t, provider, "baseUrl")
	assert.NotContains(t, provider, "api")
	assert.Equal(t, "sk-real", provider["apiKey"], "real apiKey must survive disconnect")
	models, _ := provider["models"].([]any)
	require.Len(t, models, 2, "user's custom models must survive disconnect")
	assert.Equal(t, "custom-model-1", models[0])
	assert.Equal(t, "custom-model-2", models[1])
}

func TestConnectOpenClaw_RedirectsMinimaxPortal_WhenPresent(t *testing.T) {
	// If the user has a minimax-portal provider (OpenClaw's OAuth-based MiniMax),
	// ConnectOpenClaw must redirect its baseUrl to krouter. All other fields
	// (authHeader, oauth credentials, models) must be untouched.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"apiKey":"sk-real"},"minimax-portal":{"baseUrl":"https://api.minimaxi.com/anthropic/v1","authHeader":true,"models":["MiniMax-M2.7"]}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	portal := root["models"].(map[string]any)["providers"].(map[string]any)["minimax-portal"].(map[string]any)
	assert.Equal(t, "http://127.0.0.1:8402", portal["baseUrl"], "minimax-portal baseUrl must point to krouter")
	assert.Equal(t, true, portal["authHeader"], "authHeader must be preserved")
	models, _ := portal["models"].([]any)
	assert.NotEmpty(t, models, "models list must be preserved")
}

func TestConnectOpenClaw_SkipsMinimaxPortal_WhenAbsent(t *testing.T) {
	// minimax-portal is optional — must not be created if the user doesn't have it.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"apiKey":"sk-real"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	providers := root["models"].(map[string]any)["providers"].(map[string]any)
	assert.NotContains(t, providers, "minimax-portal", "minimax-portal must not be created if absent")
}

func TestDisconnectOpenClaw_RestoresMinimaxPortalBaseURL(t *testing.T) {
	// Disconnect must restore minimax-portal.baseUrl to the original MiniMax endpoint.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	connected := `{"models":{"providers":{"anthropic":{"apiKey":"sk-real","baseUrl":"http://127.0.0.1:8402","api":"anthropic-messages"},"minimax-portal":{"baseUrl":"http://127.0.0.1:8402","authHeader":true,"models":["MiniMax-M2.7"]}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))

	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	portal := root["models"].(map[string]any)["providers"].(map[string]any)["minimax-portal"].(map[string]any)
	assert.Equal(t, "https://api.minimaxi.com/anthropic/v1", portal["baseUrl"], "minimax-portal baseUrl must be restored")
	assert.Equal(t, true, portal["authHeader"], "authHeader must survive disconnect")
}

func TestDisconnectOpenClaw_IgnoresMinimaxPortal_WhenBaseURLIsNotKrouter(t *testing.T) {
	// If minimax-portal.baseUrl is already something else (not krouter), don't touch it.
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:8402","api":"anthropic-messages"},"minimax-portal":{"baseUrl":"https://some-other-proxy.example.com","authHeader":true}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.DisconnectOpenClaw(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	portal := root["models"].(map[string]any)["providers"].(map[string]any)["minimax-portal"].(map[string]any)
	assert.Equal(t, "https://some-other-proxy.example.com", portal["baseUrl"], "unrelated baseUrl must not be touched")
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

// ── IsOpenClawConnected ───────────────────────────────────────────────────────

func TestIsOpenClawConnected_True(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	connected := `{"models":{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:8402","api":"anthropic-messages"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))
	assert.True(t, config.IsOpenClawConnected(cfg))
}

func TestIsOpenClawConnected_False_WrongURL(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	other := `{"models":{"providers":{"anthropic":{"baseUrl":"https://api.anthropic.com"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(other), 0644))
	assert.False(t, config.IsOpenClawConnected(cfg))
}

func TestIsOpenClawConnected_False_NoBaseURL(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{}`), 0644))
	assert.False(t, config.IsOpenClawConnected(cfg))
}

// ── ReadOpenClawProviderNames ─────────────────────────────────────────────────

func TestReadOpenClawProviderNames_Multi(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	content := `{"models":{"providers":{"anthropic":{},"minimax":{},"openai":{}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(content), 0644))

	names := config.ReadOpenClawProviderNames(cfg)
	assert.Equal(t, []string{"anthropic", "minimax", "openai"}, names)
}

func TestReadOpenClawProviderNames_Empty(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{}`), 0644))
	assert.Nil(t, config.ReadOpenClawProviderNames(cfg))
}

// ── ReadOpenClawAPIKey ────────────────────────────────────────────────────────

func TestReadOpenClawAPIKey_Present(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"models":{"providers":{"anthropic":{"apiKey":"sk-ant-real"}}}}`), 0644))
	assert.Equal(t, "sk-ant-real", config.ReadOpenClawAPIKey(cfg))
}

func TestReadOpenClawAPIKey_Absent(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"models":{"providers":{"anthropic":{}}}}`), 0644))
	assert.Equal(t, "", config.ReadOpenClawAPIKey(cfg))
}

func TestReadOpenClawAPIKey_Placeholder(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"models":{"providers":{"anthropic":{"apiKey":"${ANTHROPIC_API_KEY}"}}}}`), 0644))
	assert.Equal(t, "", config.ReadOpenClawAPIKey(cfg))
}

func TestReadOpenClawAPIKey_MissingFile(t *testing.T) {
	assert.Equal(t, "", config.ReadOpenClawAPIKey("/nonexistent/path.json"))
}

func TestReadOpenClawAPIKey_NoAnthropicSection(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"models":{"providers":{"minimax":{"apiKey":"mm-key"}}}}`), 0644))
	assert.Equal(t, "", config.ReadOpenClawAPIKey(cfg))
}

// ── ReadOpenClawProviderAPIKey ────────────────────────────────────────────────

func TestReadOpenClawProviderAPIKey_Present(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"models":{"providers":{"minimax-portal":{"apiKey":"mm-real-key"}}}}`), 0644))
	assert.Equal(t, "mm-real-key", config.ReadOpenClawProviderAPIKey(cfg, "minimax-portal"))
}

func TestReadOpenClawProviderAPIKey_Absent(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"models":{"providers":{"minimax-portal":{}}}}`), 0644))
	assert.Equal(t, "", config.ReadOpenClawProviderAPIKey(cfg, "minimax-portal"))
}

func TestReadOpenClawProviderAPIKey_SectionMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"models":{"providers":{"anthropic":{"apiKey":"sk-real"}}}}`), 0644))
	assert.Equal(t, "", config.ReadOpenClawProviderAPIKey(cfg, "minimax-portal"))
}

// ReadOpenClawAPIKey is a thin wrapper — verify it delegates correctly.
func TestReadOpenClawAPIKey_DelegatesGenericFn(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"models":{"providers":{"anthropic":{"apiKey":"sk-ant-real"}}}}`), 0644))
	assert.Equal(t, config.ReadOpenClawProviderAPIKey(cfg, "anthropic"), config.ReadOpenClawAPIKey(cfg))
}

// ── UpdateOpenClawModels ──────────────────────────────────────────────────────

func TestUpdateOpenClawModels(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	initial := `{"models":{"providers":{"anthropic":{"baseUrl":"http://127.0.0.1:8402","api":"anthropic-messages","apiKey":"sk-real"}}}}`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	models := []map[string]any{
		{"id": "claude-opus-4-7", "name": "Claude Opus 4.7"},
		{"id": "claude-sonnet-4-6", "name": "Claude Sonnet 4.6"},
	}
	require.NoError(t, config.UpdateOpenClawModels(cfg, "anthropic", models))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	provider := root["models"].(map[string]any)["providers"].(map[string]any)["anthropic"].(map[string]any)
	// Other fields preserved.
	assert.Equal(t, "http://127.0.0.1:8402", provider["baseUrl"])
	assert.Equal(t, "anthropic-messages", provider["api"])
	assert.Equal(t, "sk-real", provider["apiKey"])
	// Models updated.
	mlist, ok := provider["models"].([]any)
	require.True(t, ok)
	require.Len(t, mlist, 2)
	m0 := mlist[0].(map[string]any)
	assert.Equal(t, "claude-opus-4-7", m0["id"])
	assert.Equal(t, "Claude Opus 4.7", m0["name"])
}

func TestUpdateOpenClawModels_Idempotent(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"models":{"providers":{"anthropic":{"apiKey":"sk-real"}}}}`), 0644))

	models := []map[string]any{{"id": "claude-opus-4-7", "name": "Claude Opus 4.7"}}
	require.NoError(t, config.UpdateOpenClawModels(cfg, "anthropic", models))
	require.NoError(t, config.UpdateOpenClawModels(cfg, "anthropic", models)) // second call overwrites cleanly

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))
	provider := root["models"].(map[string]any)["providers"].(map[string]any)["anthropic"].(map[string]any)
	mlist, _ := provider["models"].([]any)
	require.Len(t, mlist, 1)
}

func TestUpdateOpenClawModels_CreatesProviderIfAbsent(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{}`), 0644))

	require.NoError(t, config.UpdateOpenClawModels(cfg, "anthropic", []map[string]any{{"id": "m1", "name": "M1"}}))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))
	mlist := root["models"].(map[string]any)["providers"].(map[string]any)["anthropic"].(map[string]any)["models"].([]any)
	require.Len(t, mlist, 1)
}

// ── IsClaudeCodeConnected ─────────────────────────────────────────────────────

func TestIsClaudeCodeConnected_True(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	require.NoError(t, config.ConnectClaudeCode(rc))
	assert.True(t, config.IsClaudeCodeConnected(rc))
}

func TestIsClaudeCodeConnected_False(t *testing.T) {
	dir := t.TempDir()
	rc := filepath.Join(dir, ".zshrc")
	require.NoError(t, os.WriteFile(rc, []byte("# plain rc\n"), 0644))
	assert.False(t, config.IsClaudeCodeConnected(rc))
}

func TestIsClaudeCodeConnected_MissingFile(t *testing.T) {
	assert.False(t, config.IsClaudeCodeConnected("/tmp/nonexistent-rc-file"))
}

// ── OpenCode ──────────────────────────────────────────────────────────────────

func TestConnectOpenCode_SetsBaseUrl(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "opencode.json")
	require.NoError(t, os.WriteFile(cfg, []byte(`{"provider":"anthropic","apiKey":"sk-test"}`), 0644))

	require.NoError(t, config.ConnectOpenCode(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	assert.Equal(t, "http://127.0.0.1:8402", root["baseUrl"])
	assert.Equal(t, "anthropic", root["provider"])
	assert.Equal(t, "sk-test", root["apiKey"])
}

func TestDisconnectOpenCode_RemovesBaseUrl(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "opencode.json")
	connected := `{"provider":"anthropic","baseUrl":"http://127.0.0.1:8402","apiKey":"sk-test"}`
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))

	require.NoError(t, config.DisconnectOpenCode(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	require.NoError(t, json.Unmarshal(data, &root))

	assert.NotContains(t, root, "baseUrl")
	assert.Equal(t, "anthropic", root["provider"])
	assert.Equal(t, "sk-test", root["apiKey"])
}

// ── Codex ─────────────────────────────────────────────────────────────────────

func TestConnectCodex_AddsKrouterProvider(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	initial := `model_provider = "my-openai"

[model_providers.my-openai]
name     = "My OpenAI"
base_url = "https://api.openai.com/v1"
env_key  = "OPENAI_API_KEY"
wire_api = "chat"
`
	require.NoError(t, os.WriteFile(cfg, []byte(initial), 0644))

	require.NoError(t, config.ConnectCodex(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	_, err := toml.Decode(string(data), &root)
	require.NoError(t, err)

	assert.Equal(t, "krouter", root["model_provider"])

	providers, _ := root["model_providers"].(map[string]any)
	require.NotNil(t, providers)

	krouter, _ := providers["krouter"].(map[string]any)
	require.NotNil(t, krouter, "krouter provider entry must be created")
	assert.Equal(t, "http://127.0.0.1:8402/v1", krouter["base_url"])
	assert.Equal(t, "OPENAI_API_KEY", krouter["env_key"], "env_key inherited from previous provider")

	// Original provider preserved.
	orig, _ := providers["my-openai"].(map[string]any)
	require.NotNil(t, orig)
	assert.Equal(t, "https://api.openai.com/v1", orig["base_url"])
}

func TestDisconnectCodex_RemovesKrouterProvider(t *testing.T) {
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.toml")
	connected := `model_provider = "krouter"

[model_providers.krouter]
name     = "krouter"
base_url = "http://127.0.0.1:8402/v1"
env_key  = "OPENAI_API_KEY"
wire_api = "chat"

[model_providers.my-openai]
name     = "My OpenAI"
base_url = "https://api.openai.com/v1"
env_key  = "OPENAI_API_KEY"
wire_api = "chat"
`
	require.NoError(t, os.WriteFile(cfg, []byte(connected), 0644))

	require.NoError(t, config.DisconnectCodex(cfg))

	data, _ := os.ReadFile(cfg)
	var root map[string]any
	_, err := toml.Decode(string(data), &root)
	require.NoError(t, err)

	// model_provider should no longer be "krouter"
	assert.NotEqual(t, "krouter", root["model_provider"])

	providers, _ := root["model_providers"].(map[string]any)
	assert.NotContains(t, providers, "krouter", "krouter entry must be removed")
	assert.Contains(t, providers, "my-openai", "original provider must survive")
}
