package main

import (
	"context"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestReadMinimaxOAuth_FromOpenClawExtras(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{
			Provider:    "minimax-portal",
			EndpointURL: "http://127.0.0.1:8402",
			ExtrasJSON:  `{"oauth_token":"sk-cp-FROM-AGENT","purpose":"subscription_oauth"}`,
			CapturedAt:  1,
		},
	}))

	assert.Equal(t, "sk-cp-FROM-AGENT", readMinimaxOAuthFromInheritedEndpoints(ctx, s))
}

func TestReadMinimaxOAuth_FallbacksToAlternateProviderName(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	// Some agents may name the provider just "minimax" (no -portal).
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{
			Provider:    "minimax",
			EndpointURL: "u",
			ExtrasJSON:  `{"oauth_token":"sk-cp-ALT"}`,
			CapturedAt:  1,
		},
	}))

	assert.Equal(t, "sk-cp-ALT", readMinimaxOAuthFromInheritedEndpoints(ctx, s))
}

func TestReadMinimaxOAuth_EmptyWhenNoExtras(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "minimax-portal", EndpointURL: "u", APIKey: "sk-static", CapturedAt: 1},
	}))

	assert.Empty(t, readMinimaxOAuthFromInheritedEndpoints(ctx, s),
		"static-key-only endpoint should not yield an OAuth token")
}

func TestReadMinimaxOAuth_RespectsEnabledFlag(t *testing.T) {
	s := openStore(t)
	ctx := context.Background()

	// Disabled agent → its OAuth token must not be returned.
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: false, ConfigPath: "/x",
	}))
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{
			Provider:    "minimax-portal",
			EndpointURL: "u",
			ExtrasJSON:  `{"oauth_token":"sk-cp-DISABLED"}`,
			CapturedAt:  1,
		},
	}))

	assert.Empty(t, readMinimaxOAuthFromInheritedEndpoints(ctx, s))
}

func TestReadMinimaxOAuth_NilStoreSafe(t *testing.T) {
	assert.Empty(t, readMinimaxOAuthFromInheritedEndpoints(context.Background(), nil))
}
