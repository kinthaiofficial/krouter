package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newKeyStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newSettings(t *testing.T, keys map[string]string) *config.Manager {
	t.Helper()
	mgr := config.New(filepath.Join(t.TempDir(), "settings.json"))
	require.NoError(t, mgr.Set(config.Settings{ProviderKeys: keys}))
	return mgr
}

func TestResolveProviderKeyForRouting_InheritedWins(t *testing.T) {
	store := newKeyStore(t)
	settings := newSettings(t, map[string]string{"deepseek": "sk-from-settings"})

	require.NoError(t, store.UpsertAgentSetting(context.Background(), storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(context.Background(), "openclaw", []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-from-inherited", CapturedAt: 1},
	}))

	assert.Equal(t, "sk-from-inherited",
		resolveProviderKeyForRouting(store, settings, "deepseek"))
}

func TestResolveProviderKeyForRouting_FallsBackToSettings(t *testing.T) {
	store := newKeyStore(t)
	settings := newSettings(t, map[string]string{"deepseek": "sk-settings-only"})

	assert.Equal(t, "sk-settings-only",
		resolveProviderKeyForRouting(store, settings, "deepseek"))
}

func TestResolveProviderKeyForRouting_EmptyWhenNothingConfigured(t *testing.T) {
	store := newKeyStore(t)
	settings := newSettings(t, map[string]string{})

	assert.Empty(t, resolveProviderKeyForRouting(store, settings, "deepseek"))
}

func TestResolveProviderKeyForRouting_NilSafe(t *testing.T) {
	assert.Empty(t, resolveProviderKeyForRouting(nil, nil, "deepseek"))
}

func TestResolveProviderKeyForRouting_SkipsDisabledAgents(t *testing.T) {
	store := newKeyStore(t)
	settings := newSettings(t, map[string]string{})

	require.NoError(t, store.UpsertAgentSetting(context.Background(), storage.AgentSetting{
		AgentID: "cursor", Enabled: false, ConfigPath: "/y",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(context.Background(), "cursor", []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-disabled", CapturedAt: 1},
	}))

	assert.Empty(t, resolveProviderKeyForRouting(store, settings, "deepseek"),
		"disabled agent's key must not be used by routing")
}
