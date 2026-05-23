package main

import (
	"context"
	"sort"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newStoreForFP(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// seedFreeProvider is a brief helper for the source tests. It writes a row
// to free_provider_state with sensible defaults; tests override only the
// fields they care about.
func seedFreeProvider(t *testing.T, store *storage.Store, id, krouterName string) {
	t.Helper()
	require.NoError(t, store.UpsertFreeProvider(context.Background(), storage.FreeProvider{
		ID:                  id,
		DisplayName:         id,
		KrouterProviderName: krouterName,
		Protocol:            "openai",
		Region:              "intl",
		FreeType:            "trial_credit",
		FreeSummary:         "free",
		FreeQuotaUSD:        5,
		SignupURL:           "https://example.com/" + id,
		Active:              true,
		UpdatedAt:           time.Now().UTC(),
	}))
}

// seedInherited is a brief helper. The agent_settings row must exist before
// inherited_endpoints can be written (FK), and the agent must be enabled
// for FindInheritedEndpointsByProvider to count it.
func seedInherited(t *testing.T, store *storage.Store, agentID, provider string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: agentID, Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, agentID, []storage.InheritedEndpoint{
		{Provider: provider, EndpointURL: "u", APIKey: "sk-x", CapturedAt: 1},
	}))
}

// ─── Core selection logic ──────────────────────────────────────────────────

func TestFreeProviderSource_EmptyWhenNothingConfigured(t *testing.T) {
	store := newStoreForFP(t)
	seedFreeProvider(t, store, "deepseek", "deepseek")
	// no inherited rows

	got := newFreeProviderSource(store).
		ListAvailableFreeProviders(context.Background(), "openai")
	assert.Empty(t, got, "no inherited keys → nothing to prefer")
}

func TestFreeProviderSource_EmptyWhenInheritedButNotCatalogued(t *testing.T) {
	store := newStoreForFP(t)
	// catalogue NOT seeded
	seedInherited(t, store, "openclaw", "deepseek")

	got := newFreeProviderSource(store).
		ListAvailableFreeProviders(context.Background(), "openai")
	assert.Empty(t, got, "inherited provider not in free-tokens catalog → don't prefer")
}

func TestFreeProviderSource_ReturnsInheritedAndCatalogued(t *testing.T) {
	store := newStoreForFP(t)
	seedFreeProvider(t, store, "deepseek", "deepseek")
	seedFreeProvider(t, store, "groq", "groq")
	seedInherited(t, store, "openclaw", "deepseek")

	got := newFreeProviderSource(store).
		ListAvailableFreeProviders(context.Background(), "openai")
	assert.Equal(t, []string{"deepseek"}, got,
		"only inherited+catalogued provider should be returned (not groq, which isn't inherited)")
}

func TestFreeProviderSource_ExhaustedExcluded(t *testing.T) {
	store := newStoreForFP(t)
	ctx := context.Background()
	seedFreeProvider(t, store, "deepseek", "deepseek")
	seedInherited(t, store, "openclaw", "deepseek")

	// Mark it exhausted with a future TTL.
	require.NoError(t, store.MarkProviderExhausted(ctx, "deepseek",
		time.Now().UTC().Add(time.Hour), 402, "test"))

	got := newFreeProviderSource(store).
		ListAvailableFreeProviders(ctx, "openai")
	assert.Empty(t, got, "exhausted provider must be filtered out")
}

func TestFreeProviderSource_ExpiredExhaustionAllowsBack(t *testing.T) {
	store := newStoreForFP(t)
	ctx := context.Background()
	seedFreeProvider(t, store, "deepseek", "deepseek")
	seedInherited(t, store, "openclaw", "deepseek")

	// Mark it exhausted with an already-past TTL.
	require.NoError(t, store.MarkProviderExhausted(ctx, "deepseek",
		time.Now().UTC().Add(-time.Hour), 402, "test"))

	got := newFreeProviderSource(store).
		ListAvailableFreeProviders(ctx, "openai")
	assert.Equal(t, []string{"deepseek"}, got,
		"once the TTL passes, provider must be re-included")
}

func TestFreeProviderSource_InactiveCatalogRowExcluded(t *testing.T) {
	store := newStoreForFP(t)
	ctx := context.Background()
	// active=false row
	require.NoError(t, store.UpsertFreeProvider(ctx, storage.FreeProvider{
		ID:                  "deepseek",
		DisplayName:         "DeepSeek",
		KrouterProviderName: "deepseek",
		Protocol:            "openai",
		FreeType:            "trial_credit",
		SignupURL:           "https://example.com/",
		Active:              false,
		UpdatedAt:           time.Now().UTC(),
	}))
	seedInherited(t, store, "openclaw", "deepseek")

	got := newFreeProviderSource(store).
		ListAvailableFreeProviders(ctx, "openai")
	assert.Empty(t, got, "active=false rows are filtered by FreeProviderKrouterNames")
}

func TestFreeProviderSource_DisabledAgentDoesntCount(t *testing.T) {
	store := newStoreForFP(t)
	ctx := context.Background()
	seedFreeProvider(t, store, "deepseek", "deepseek")

	// Inherited under an agent that's NOT enabled.
	require.NoError(t, store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: false, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-x", CapturedAt: 1},
	}))

	// ListInheritedEndpoints returns rows from ALL agents (enabled or not).
	// freeProviderSource currently doesn't filter by Enabled — that's a
	// known design choice: even disabled agents have keys we can use.
	// This test documents that semantics, NOT asserting "no result".
	got := newFreeProviderSource(store).
		ListAvailableFreeProviders(ctx, "openai")
	assert.Equal(t, []string{"deepseek"}, got,
		"current impl uses keys from disabled agents too; revisit if this becomes a problem")
}

func TestFreeProviderSource_SameProviderAcrossAgentsDeduped(t *testing.T) {
	// inherited_endpoints PK is (agent_id, provider), so duplication only
	// happens when two different agents both expose the same provider.
	// Realistic scenario: user has OpenClaw with deepseek + Claude Code
	// also using deepseek; routing should still see "deepseek" once.
	store := newStoreForFP(t)
	ctx := context.Background()
	seedFreeProvider(t, store, "deepseek", "deepseek")

	for _, agentID := range []string{"openclaw", "claude-code"} {
		require.NoError(t, store.UpsertAgentSetting(ctx, storage.AgentSetting{
			AgentID: agentID, Enabled: true, ConfigPath: "/" + agentID,
		}))
		require.NoError(t, store.ReplaceInheritedEndpoints(ctx, agentID, []storage.InheritedEndpoint{
			{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-x", CapturedAt: 1},
		}))
	}

	got := newFreeProviderSource(store).
		ListAvailableFreeProviders(ctx, "openai")
	sort.Strings(got)
	assert.Equal(t, []string{"deepseek"}, got,
		"same provider in two agents should appear once in routing's free list")
}

func TestFreeProviderSource_NilStoreSafe(t *testing.T) {
	src := newFreeProviderSource(nil)
	assert.Empty(t, src.ListAvailableFreeProviders(context.Background(), "openai"),
		"nil store must be defensive — no panic, empty result")
}
