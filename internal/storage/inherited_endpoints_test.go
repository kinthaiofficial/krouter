package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupAgent(t *testing.T, s *storage.Store, agentID string, enabled bool) {
	t.Helper()
	require.NoError(t, s.UpsertAppSetting(context.Background(), storage.AppSetting{
		AppID: agentID, Enabled: enabled, ConfigPath: "/tmp/" + agentID,
	}))
}

func TestInheritedEndpoints_ReplaceAtomic(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()
	setupAgent(t, s, "openclaw", true)

	now := time.Now().UnixMilli()

	// Initial batch of 3 endpoints.
	first := []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "https://api.anthropic.com", CapturedAt: now},
		{Provider: "minimax-portal", EndpointURL: "http://127.0.0.1:8402", APIKey: "sk-api-x", CapturedAt: now},
		{Provider: "openrouter", EndpointURL: "https://openrouter.ai/api", ExtrasJSON: `{"k":"v"}`, CapturedAt: now},
	}
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", first))

	got, err := s.ListInheritedEndpointsByApp(ctx, "openclaw")
	require.NoError(t, err)
	require.Len(t, got, 3)

	// Replace with smaller batch — old rows must be gone.
	second := []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "https://api.deepseek.com", APIKey: "sk-foo", CapturedAt: now + 1},
	}
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", second))

	got, err = s.ListInheritedEndpointsByApp(ctx, "openclaw")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "deepseek", got[0].Provider)
}

// TestFindInheritedEndpointsByProvider_CanonicalAlias guards the provider-name
// aliasing fix: an agent (e.g. OpenClaw) may name a vendor by its natural name
// ("dashscope") while krouter's adapter is registered under a different
// canonical name ("qwen"). A lookup by the krouter name must still find the
// inherited key, otherwise the provider shows configured:false and routing
// can't resolve its key — even though the user has the credential.
func TestFindInheritedEndpointsByProvider_CanonicalAlias(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()
	setupAgent(t, s, "openclaw", true)

	now := time.Now().UnixMilli()
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "dashscope", EndpointURL: "https://dashscope.aliyuncs.com", APIKey: "sk-dash", CapturedAt: now},
	}))

	// Lookup by krouter's canonical adapter name must resolve the aliased row.
	got, err := s.FindInheritedEndpointsByProvider(ctx, "qwen")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "sk-dash", got[0].APIKey)

	// Lookup by the literal stored name still works too.
	got, err = s.FindInheritedEndpointsByProvider(ctx, "dashscope")
	require.NoError(t, err)
	require.Len(t, got, 1)

	// A non-aliased, unrelated name must not match.
	got, err = s.FindInheritedEndpointsByProvider(ctx, "openai")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestInheritedEndpoints_ReplaceWithEmptyDeletesAll(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()
	setupAgent(t, s, "openclaw", true)

	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "u", CapturedAt: 1},
	}))
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", nil))

	got, err := s.ListInheritedEndpointsByApp(ctx, "openclaw")
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestInheritedEndpoints_ScopedQuery(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	setupAgent(t, s, "openclaw", true)
	setupAgent(t, s, "claude-code", true)

	now := time.Now().UnixMilli()
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "url-oc", CapturedAt: now},
		{Provider: "minimax-portal", EndpointURL: "url-mm", CapturedAt: now},
	}))
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "claude-code", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "url-cc", CapturedAt: now},
	}))

	// per-agent
	got, _ := s.ListInheritedEndpointsByApp(ctx, "openclaw")
	assert.Len(t, got, 2)

	// global
	all, _ := s.ListInheritedEndpoints(ctx)
	assert.Len(t, all, 3)

	// by provider — anthropic appears in both agents
	anth, _ := s.FindInheritedEndpointsByProvider(ctx, "anthropic")
	assert.Len(t, anth, 2)
}

func TestInheritedEndpoints_FindByProvider_RespectsEnabledFlag(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	setupAgent(t, s, "openclaw", true)
	setupAgent(t, s, "cursor", false) // disabled

	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "url-1", CapturedAt: 1},
	}))
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "cursor", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "url-2", CapturedAt: 1},
	}))

	// FindInheritedEndpointsByProvider should skip cursor's row because
	// agent_settings.enabled = 0 for it.
	got, err := s.FindInheritedEndpointsByProvider(ctx, "anthropic")
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "openclaw", got[0].AppID)
}

func TestInheritedEndpoints_DeleteAgentClearsInheritedRows(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	setupAgent(t, s, "openclaw", true)
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "u", CapturedAt: 1},
	}))

	require.NoError(t, s.DeleteAppSetting(ctx, "openclaw"))

	// DeleteAppSetting runs its own transactional cleanup of
	// inherited_endpoints because SQLite FOREIGN KEYS are not enforced under
	// the default PRAGMA (see storage.Open). The user-observable behaviour is
	// identical to ON DELETE CASCADE.
	got, err := s.ListInheritedEndpointsByApp(ctx, "openclaw")
	require.NoError(t, err)
	assert.Empty(t, got)
}
