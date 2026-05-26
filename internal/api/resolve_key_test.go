package api

import (
	"context"
	"sort"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveProviderKey_InheritedFromEnabledAgent(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-inherited", CapturedAt: 1},
	}))

	assert.Equal(t, "sk-inherited", srv.resolveProviderKey(ctx, "deepseek"))
}

func TestResolveProviderKey_EmptyWhenNoCredential(t *testing.T) {
	srv, _ := newTestServer(t)
	assert.Empty(t, srv.resolveProviderKey(context.Background(), "deepseek"))
}

func TestResolveProviderKey_SkipsDisabledAgents(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "cursor", Enabled: false, ConfigPath: "/y",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "cursor", []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-disabled-loses", CapturedAt: 1},
	}))

	assert.Empty(t, srv.resolveProviderKey(ctx, "deepseek"),
		"disabled agent should not contribute keys")
}

func TestProvidersWithCredentials_InheritedOnly(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "u", APIKey: "sk-anthropic", CapturedAt: 1},
		{Provider: "groq", EndpointURL: "u", APIKey: "sk-groq", CapturedAt: 1},
		{Provider: "no-key-endpoint", EndpointURL: "u", APIKey: "", CapturedAt: 1}, // no key → exclude
	}))

	got := srv.providersWithCredentials(ctx)
	sort.Strings(got)
	assert.Equal(t, []string{"anthropic", "groq"}, got)
}
