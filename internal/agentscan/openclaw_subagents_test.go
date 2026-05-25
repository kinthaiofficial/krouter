package agentscan

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// makeOpenClawTree builds an in-tmp ~/.openclaw layout matching what the
// real installer creates: a global openclaw.json with `agents.list`, plus
// per-sub-agent `agents/<id>/agent/{models.json, auth-profiles.json}`.
func makeOpenClawTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Global openclaw.json — two sub-agents (`main` and `claude`).
	global := map[string]any{
		"agents": map[string]any{
			"defaults": map[string]any{
				"model": map[string]any{"primary": "minimax-portal/MiniMax-M2.7"},
			},
			"list": []any{
				map[string]any{"id": "main"},
				map[string]any{
					"id":        "claude",
					"name":      "Claude profile",
					"model":     "anthropic/claude-haiku-4-5",
					"workspace": "/tmp/claude-ws",
					"agentDir":  filepath.Join(root, "agents", "claude", "agent"),
				},
			},
		},
	}
	require.NoError(t, writeJSONFile(filepath.Join(root, "openclaw.json"), global))

	// main/agent/models.json with two providers — minimax-portal + anthropic.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "agents", "main", "agent"), 0o755))
	require.NoError(t, writeJSONFile(filepath.Join(root, "agents", "main", "agent", "models.json"), map[string]any{
		"providers": map[string]any{
			"minimax-portal": map[string]any{
				"api":     "anthropic-messages",
				"apiKey":  "sk-mm-FAKE",
				"baseUrl": "https://api.minimaxi.com/anthropic/v1",
				"models": []map[string]string{
					{"id": "MiniMax-M2.7", "name": "MiniMax-M2.7"},
					{"id": "MiniMax-M2.5", "name": "MiniMax-M2.5"},
				},
			},
			"anthropic": map[string]any{
				"api":     "anthropic-messages",
				"apiKey":  "sk-ant-FAKE",
				"baseUrl": "https://api.anthropic.com",
				"models": []map[string]string{
					{"id": "claude-haiku-4-5", "name": "Claude Haiku"},
				},
			},
		},
	}))
	// main has an OAuth profile so HasOAuth must be true.
	require.NoError(t, writeJSONFile(filepath.Join(root, "agents", "main", "agent", "auth-profiles.json"), map[string]any{
		"profiles": map[string]any{
			"minimax-portal:default": map[string]any{
				"type":     "oauth",
				"provider": "minimax-portal",
				"access":   "sk-cp-FAKE",
			},
		},
	}))

	// claude/agent/models.json — different provider set, anthropic only.
	require.NoError(t, os.MkdirAll(filepath.Join(root, "agents", "claude", "agent"), 0o755))
	require.NoError(t, writeJSONFile(filepath.Join(root, "agents", "claude", "agent", "models.json"), map[string]any{
		"providers": map[string]any{
			"anthropic": map[string]any{
				"api":     "anthropic-messages",
				"apiKey":  "sk-ant-different",
				"baseUrl": "https://api.anthropic.com",
				"models": []map[string]string{
					{"id": "claude-haiku-4-5", "name": "Claude Haiku"},
					{"id": "claude-sonnet-4-5", "name": "Claude Sonnet"},
				},
			},
		},
	}))
	// claude has NO auth-profiles.json (sk-ant API key, not OAuth).

	return root
}

func writeJSONFile(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func TestListOpenClawSubAgents_EnumeratesEverySubAgent(t *testing.T) {
	root := makeOpenClawTree(t)
	subs, err := ListOpenClawSubAgents(root)
	require.NoError(t, err)
	require.Len(t, subs, 2, "global agents.list has 2 entries — both must be returned")

	// Sorted by ID alphabetically.
	assert.Equal(t, "claude", subs[0].ID)
	assert.Equal(t, "main", subs[1].ID)
}

func TestListOpenClawSubAgents_PrimaryModelFallsBackToDefault(t *testing.T) {
	root := makeOpenClawTree(t)
	subs, _ := ListOpenClawSubAgents(root)
	byID := indexByID(subs)

	// `main` has no explicit `model` field → falls back to defaults.model.primary.
	assert.Equal(t, "minimax-portal/MiniMax-M2.7", byID["main"].PrimaryModel)
	// `claude` overrides defaults with its own primary.
	assert.Equal(t, "anthropic/claude-haiku-4-5", byID["claude"].PrimaryModel)
}

func TestListOpenClawSubAgents_ProvidersFromPerSubModelsJson(t *testing.T) {
	root := makeOpenClawTree(t)
	subs, _ := ListOpenClawSubAgents(root)
	byID := indexByID(subs)

	// main has TWO providers (minimax-portal + anthropic).
	require.Len(t, byID["main"].Providers, 2)
	provByName := map[string]OpenClawSubAgentProvider{}
	for _, p := range byID["main"].Providers {
		provByName[p.Provider] = p
	}
	assert.Equal(t, "anthropic-messages", provByName["minimax-portal"].Protocol)
	assert.True(t, provByName["minimax-portal"].HasAPIKey, "real key present in fixture")
	// Raw key never leaks into the struct — the DTO has no APIKey field,
	// only `HasAPIKey` bool. Compile-time check via the struct literal
	// itself rather than a runtime assertion.
	assert.Contains(t, provByName["minimax-portal"].Models, "MiniMax-M2.7")
	assert.Contains(t, provByName["minimax-portal"].Models, "MiniMax-M2.5")

	// claude has only ONE provider (anthropic) — different from main.
	require.Len(t, byID["claude"].Providers, 1)
	assert.Equal(t, "anthropic", byID["claude"].Providers[0].Provider)
	assert.Equal(t, []string{"claude-haiku-4-5", "claude-sonnet-4-5"}, byID["claude"].Providers[0].Models)
}

func TestListOpenClawSubAgents_PrimaryModelEchoedOnMatchingProvider(t *testing.T) {
	root := makeOpenClawTree(t)
	subs, _ := ListOpenClawSubAgents(root)
	byID := indexByID(subs)

	// claude's primary is anthropic/claude-haiku-4-5 → primary_model field on
	// the anthropic provider row should be `claude-haiku-4-5`.
	require.Len(t, byID["claude"].Providers, 1)
	assert.Equal(t, "claude-haiku-4-5", byID["claude"].Providers[0].PrimaryModel)

	// main's primary is minimax-portal/MiniMax-M2.7 → that provider gets the
	// echo, the anthropic one does not.
	for _, p := range byID["main"].Providers {
		switch p.Provider {
		case "minimax-portal":
			assert.Equal(t, "MiniMax-M2.7", p.PrimaryModel)
		case "anthropic":
			assert.Empty(t, p.PrimaryModel)
		}
	}
}

func TestListOpenClawSubAgents_HasOAuthReflectsAuthProfilesPresence(t *testing.T) {
	root := makeOpenClawTree(t)
	subs, _ := ListOpenClawSubAgents(root)
	byID := indexByID(subs)

	assert.True(t, byID["main"].HasOAuth, "main has an OAuth profile in the fixture")
	assert.False(t, byID["claude"].HasOAuth, "claude has no auth-profiles.json")
}

func TestListOpenClawSubAgents_MissingOpenClaw(t *testing.T) {
	// Path that doesn't exist → empty result, no error (OpenClaw simply
	// isn't installed, not an error condition).
	subs, err := ListOpenClawSubAgents(filepath.Join(t.TempDir(), "definitely-not-installed"))
	require.NoError(t, err)
	assert.Empty(t, subs)
}

func TestListOpenClawSubAgents_MissingModelsJson(t *testing.T) {
	// Sub-agent exists in agents.list but has no models.json — sub-agent
	// still surfaces with metadata; Providers is nil.
	root := t.TempDir()
	require.NoError(t, writeJSONFile(filepath.Join(root, "openclaw.json"), map[string]any{
		"agents": map[string]any{
			"list": []any{
				map[string]any{"id": "bare"},
			},
		},
	}))
	subs, err := ListOpenClawSubAgents(root)
	require.NoError(t, err)
	require.Len(t, subs, 1)
	assert.Equal(t, "bare", subs[0].ID)
	assert.Empty(t, subs[0].Providers)
}

func indexByID(subs []OpenClawSubAgent) map[string]OpenClawSubAgent {
	out := map[string]OpenClawSubAgent{}
	for _, s := range subs {
		out[s.ID] = s
	}
	return out
}
