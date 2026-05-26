package main

import (
	"context"
	"testing"

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

func TestResolveProviderKeyForRouting_InheritedFromEnabledAgent(t *testing.T) {
	store := newKeyStore(t)

	require.NoError(t, store.UpsertAppSetting(context.Background(), storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(context.Background(), "openclaw", []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-from-inherited", CapturedAt: 1},
	}))

	assert.Equal(t, "sk-from-inherited",
		resolveProviderKeyForRouting(store, "deepseek"))
}

func TestResolveProviderKeyForRouting_EmptyWhenNothingConfigured(t *testing.T) {
	store := newKeyStore(t)
	assert.Empty(t, resolveProviderKeyForRouting(store, "deepseek"))
}

func TestResolveProviderKeyForRouting_NilSafe(t *testing.T) {
	assert.Empty(t, resolveProviderKeyForRouting(nil, "deepseek"))
}

func TestResolveProviderKeyForRouting_SkipsDisabledAgents(t *testing.T) {
	store := newKeyStore(t)

	require.NoError(t, store.UpsertAppSetting(context.Background(), storage.AppSetting{
		AppID: "cursor", Enabled: false, ConfigPath: "/y",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(context.Background(), "cursor", []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-disabled", CapturedAt: 1},
	}))

	assert.Empty(t, resolveProviderKeyForRouting(store, "deepseek"),
		"disabled agent's key must not be used by routing")
}
