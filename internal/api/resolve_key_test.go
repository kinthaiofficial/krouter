package api

import (
	"context"
	"path/filepath"
	"sort"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withSettings replaces s.settings with a file-backed manager pre-seeded with
// the given ProviderKeys map for the duration of the test.
func withSettings(t *testing.T, s *Server, keys map[string]string) {
	t.Helper()
	mgr := config.New(filepath.Join(t.TempDir(), "settings.json"))
	require.NoError(t, mgr.Set(config.Settings{ProviderKeys: keys}))
	s.settings = mgr
	t.Cleanup(func() { s.settings = nil })
}

func TestResolveProviderKey_InheritedPreferredOverSettings(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	// Settings has a key.
	withSettings(t, srv, map[string]string{"deepseek": "sk-settings-fallback"})

	// Inherited from an enabled agent with a different key.
	require.NoError(t, store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-inherited-WINS", CapturedAt: 1},
	}))

	got := srv.resolveProviderKey(ctx, "deepseek")
	assert.Equal(t, "sk-inherited-WINS", got)
}

func TestResolveProviderKey_FallsBackToSettings(t *testing.T) {
	srv, _ := newTestServer(t)
	ctx := context.Background()

	withSettings(t, srv, map[string]string{"deepseek": "sk-only-in-settings"})

	// No inherited row at all.
	got := srv.resolveProviderKey(ctx, "deepseek")
	assert.Equal(t, "sk-only-in-settings", got)
}

func TestResolveProviderKey_EmptyWhenNoCredential(t *testing.T) {
	srv, _ := newTestServer(t)
	withSettings(t, srv, map[string]string{})
	assert.Empty(t, srv.resolveProviderKey(context.Background(), "deepseek"))
}

func TestResolveProviderKey_SkipsDisabledAgents(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	withSettings(t, srv, map[string]string{})

	// A disabled agent's inherited key must NOT win.
	require.NoError(t, store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "cursor", Enabled: false, ConfigPath: "/y",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "cursor", []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-disabled-loses", CapturedAt: 1},
	}))

	assert.Empty(t, srv.resolveProviderKey(ctx, "deepseek"),
		"disabled agent should not contribute keys")
}

func TestProvidersWithCredentials_UnionsBothSources(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	withSettings(t, srv, map[string]string{
		"groq":    "sk-groq",
		"empty-1": "", // empty keys must be ignored
	})

	require.NoError(t, store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "u", APIKey: "sk-anthropic", CapturedAt: 1},
		{Provider: "groq", EndpointURL: "u", APIKey: "sk-groq-from-agent", CapturedAt: 1},
		{Provider: "no-key-endpoint", EndpointURL: "u", APIKey: "", CapturedAt: 1}, // no key → exclude
	}))

	got := srv.providersWithCredentials(ctx)
	sort.Strings(got)
	assert.Equal(t, []string{"anthropic", "groq"}, got,
		"union should include settings-only, inherited-only, and dedupe overlaps; empty-keyed rows excluded")
}
