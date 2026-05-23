package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFreeProvider(id, krouterName string, active bool) storage.FreeProvider {
	return storage.FreeProvider{
		ID:                  id,
		DisplayName:         id,
		KrouterProviderName: krouterName,
		Protocol:            "openai",
		Region:              "intl",
		FreeType:            "trial_credit",
		FreeSummary:         "test",
		FreeQuotaUSD:        5,
		Validity:            "no_expiry",
		Conditions:          "test",
		SignupURL:           "https://example.com/signup",
		KeySetupHint:        "test",
		Active:              active,
		LastVerified:        "2026-05-23",
		Notes:               "test",
		UpdatedAt:           time.Now().UTC(),
	}
}

func TestFreeProvider_UpsertAndList(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertFreeProvider(ctx, newFreeProvider("deepseek", "deepseek", true)))
	require.NoError(t, s.UpsertFreeProvider(ctx, newFreeProvider("groq", "groq", true)))
	require.NoError(t, s.UpsertFreeProvider(ctx, newFreeProvider("retired", "retired-vendor", false)))

	all, err := s.ListFreeProviders(ctx, false)
	require.NoError(t, err)
	assert.Len(t, all, 3)

	activeOnly, err := s.ListFreeProviders(ctx, true)
	require.NoError(t, err)
	assert.Len(t, activeOnly, 2, "active=false rows excluded when activeOnly=true")
}

func TestFreeProvider_UpsertReplacesOnConflict(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	// First insert: ¥1 quota.
	first := newFreeProvider("deepseek", "deepseek", true)
	first.FreeQuotaUSD = 1
	require.NoError(t, s.UpsertFreeProvider(ctx, first))

	// Second insert with same id: ¥10 quota. Should replace, not duplicate.
	second := newFreeProvider("deepseek", "deepseek", true)
	second.FreeQuotaUSD = 10
	require.NoError(t, s.UpsertFreeProvider(ctx, second))

	rows, _ := s.ListFreeProviders(ctx, false)
	require.Len(t, rows, 1)
	assert.Equal(t, 10.0, rows[0].FreeQuotaUSD,
		"upsert should overwrite quota with the most recent value")
}

func TestFreeProvider_AdditionalProtocolsRoundTrip(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	want := newFreeProvider("openrouter", "openrouter", true)
	want.AdditionalProtocols = []storage.FreeProviderProtocol{
		{
			Protocol:            "anthropic",
			KrouterProviderName: "openrouter-anthropic",
			KeySetupHint:        "same key, baseURL /v1",
		},
	}
	require.NoError(t, s.UpsertFreeProvider(ctx, want))

	rows, err := s.ListFreeProviders(ctx, true)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].AdditionalProtocols, 1)
	assert.Equal(t, "anthropic", rows[0].AdditionalProtocols[0].Protocol)
	assert.Equal(t, "openrouter-anthropic", rows[0].AdditionalProtocols[0].KrouterProviderName)
}

func TestFreeProvider_KrouterNamesIncludesAlternates(t *testing.T) {
	// Both the primary krouter_provider_name and every alternate's name
	// must appear in the union set used by the API handler's "is this
	// inherited provider on the catalog?" join.
	s := openMigratedStore(t)
	ctx := context.Background()

	p := newFreeProvider("openrouter", "openrouter", true)
	p.AdditionalProtocols = []storage.FreeProviderProtocol{
		{Protocol: "anthropic", KrouterProviderName: "openrouter-anthropic"},
	}
	require.NoError(t, s.UpsertFreeProvider(ctx, p))

	names, err := s.FreeProviderKrouterNames(ctx)
	require.NoError(t, err)
	_, hasPrimary := names["openrouter"]
	_, hasAlt := names["openrouter-anthropic"]
	assert.True(t, hasPrimary)
	assert.True(t, hasAlt, "alternate krouter_provider_name should also appear in the union set")
}

func TestFreeProvider_KrouterNamesByProtocol(t *testing.T) {
	// Dual-protocol catalog row → two entries in the per-protocol map.
	s := openMigratedStore(t)
	ctx := context.Background()

	p := newFreeProvider("openrouter", "openrouter", true)
	p.Protocol = "openai"
	p.AdditionalProtocols = []storage.FreeProviderProtocol{
		{Protocol: "anthropic", KrouterProviderName: "openrouter-anthropic"},
	}
	require.NoError(t, s.UpsertFreeProvider(ctx, p))

	// Single-protocol provider as a control.
	q := newFreeProvider("deepseek", "deepseek", true)
	q.Protocol = "openai"
	require.NoError(t, s.UpsertFreeProvider(ctx, q))

	byProto, err := s.FreeProviderKrouterNamesByProtocol(ctx)
	require.NoError(t, err)

	require.Contains(t, byProto, "openai")
	_, hasDeepseek := byProto["openai"]["deepseek"]
	_, hasOR := byProto["openai"]["openrouter"]
	assert.True(t, hasDeepseek)
	assert.True(t, hasOR)

	require.Contains(t, byProto, "anthropic")
	_, hasORA := byProto["anthropic"]["openrouter-anthropic"]
	assert.True(t, hasORA)
	_, accidentalCrossover := byProto["anthropic"]["openrouter"]
	assert.False(t, accidentalCrossover,
		"primary openai-only entry must NOT leak into the anthropic protocol bucket")
}

func TestFreeProvider_KrouterNamesActiveOnly(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertFreeProvider(ctx, newFreeProvider("deepseek", "deepseek", true)))
	require.NoError(t, s.UpsertFreeProvider(ctx, newFreeProvider("groq", "groq", true)))
	require.NoError(t, s.UpsertFreeProvider(ctx, newFreeProvider("retired", "retired-vendor", false)))

	names, err := s.FreeProviderKrouterNames(ctx)
	require.NoError(t, err)
	_, hasDS := names["deepseek"]
	_, hasGroq := names["groq"]
	_, hasRetired := names["retired-vendor"]
	assert.True(t, hasDS && hasGroq)
	assert.False(t, hasRetired, "inactive providers excluded from routing lookup")
}

// ── provider_exhausted_until ───────────────────────────────────────────────

func TestProviderExhausted_MarkAndCheck(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	until := time.Now().UTC().Add(time.Hour)
	require.NoError(t, s.MarkProviderExhausted(ctx, "deepseek", until, 402, "test"))

	assert.True(t, s.IsProviderExhausted(ctx, "deepseek"),
		"recently marked provider should report as exhausted")
	assert.False(t, s.IsProviderExhausted(ctx, "untouched"),
		"unmarked provider should not report as exhausted")
}

func TestProviderExhausted_ExpiredMarkIsIgnored(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	pastTime := time.Now().UTC().Add(-time.Hour)
	require.NoError(t, s.MarkProviderExhausted(ctx, "deepseek", pastTime, 429, "test"))

	assert.False(t, s.IsProviderExhausted(ctx, "deepseek"),
		"a mark whose exhausted_until is in the past should be ignored")
}

func TestProviderExhausted_ClearRemovesRow(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	require.NoError(t, s.MarkProviderExhausted(ctx, "deepseek",
		time.Now().Add(time.Hour), 402, "test"))
	require.True(t, s.IsProviderExhausted(ctx, "deepseek"))

	require.NoError(t, s.ClearProviderExhausted(ctx, "deepseek"))
	assert.False(t, s.IsProviderExhausted(ctx, "deepseek"))
}

func TestProviderExhausted_MarkUpsertsOnRepeatedHit(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	// 429 (short TTL) followed by 402 (long TTL). The second mark should
	// extend the exhaustion, not be ignored because the first row exists.
	short := time.Now().UTC().Add(5 * time.Minute)
	require.NoError(t, s.MarkProviderExhausted(ctx, "deepseek", short, 429, "burst"))

	long := time.Now().UTC().Add(24 * time.Hour)
	require.NoError(t, s.MarkProviderExhausted(ctx, "deepseek", long, 402, "quota gone"))

	assert.True(t, s.IsProviderExhausted(ctx, "deepseek"),
		"longer TTL should still report as exhausted after the upsert")
}
