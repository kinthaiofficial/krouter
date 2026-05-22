package main

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSubStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// upsertTier is a brevity helper for tests that build subscription_quota_cache
// rows directly. All times are relative to "now" so the IsAvailable window is
// open.
func upsertTier(t *testing.T, store *storage.Store, pattern string, used, total int64, highspeed bool) {
	t.Helper()
	now := time.Now().UTC()
	require.NoError(t, store.UpsertSubscriptionQuota(context.Background(), storage.SubscriptionQuota{
		Provider:     "minimax",
		ModelPattern: pattern,
		WindowStart:  now.Add(-30 * time.Minute),
		WindowEnd:    now.Add(4 * time.Hour),
		TotalCount:   total,
		UsedCount:    used,
		Highspeed:    highspeed,
		FetchedAt:    now,
	}))
}

func TestSubscriptionSource_NoQuotaRowsAtAll(t *testing.T) {
	s := newSubscriptionSource(newSubStore(t))
	info := s.GetSubscriptionInfo(context.Background(), "minimax")
	assert.False(t, info.Available)
}

func TestSubscriptionSource_PicksMiniMaxMTierOverSpeechHd(t *testing.T) {
	// Regression test for the pre-fix bug: speech-hd has remaining > 0
	// (it's drained slowly because users don't usually TTS), but the
	// LLM MiniMax-M* tier is exhausted. routing should NOT think minimax
	// is available — sending an LLM request would 4xx because M* is empty.
	store := newSubStore(t)
	upsertTier(t, store, "MiniMax-M*", 1500, 1500, false) // LLM exhausted
	upsertTier(t, store, "speech-hd", 100, 4000, false)   // TTS plenty

	info := newSubscriptionSource(store).
		GetSubscriptionInfo(context.Background(), "minimax")

	assert.False(t, info.Available,
		"speech-hd having quota must not mask MiniMax-M* exhaustion")
}

func TestSubscriptionSource_PrefersStandardOverHighspeed(t *testing.T) {
	// User has bought both standard and highspeed M* plans. Both have
	// remaining quota. Standard is cheaper per call ($0.0000313 vs
	// $0.0000626), so routing should prefer it.
	//
	// The two tiers carry distinct ModelPattern values because that's the
	// upstream `model_name` field that distinguishes them (see
	// internal/providers/minimax/quota.go::parseQuotaResponse, which uses
	// the raw model_name as the pattern and sets Highspeed = strings.Contains
	// of "highspeed"). Using the same pattern for both rows would collide on
	// the (provider, model_pattern) primary key.
	store := newSubStore(t)
	upsertTier(t, store, "MiniMax-M*", 100, 1500, false)            // standard
	upsertTier(t, store, "MiniMax-M*-highspeed", 100, 1500, true)   // highspeed

	info := newSubscriptionSource(store).
		GetSubscriptionInfo(context.Background(), "minimax")

	require.True(t, info.Available)
	assert.Equal(t, "MiniMax-M2.7", info.Model,
		"when both tiers are available, route via standard")
}

func TestSubscriptionSource_FallsBackToHighspeed(t *testing.T) {
	// Standard exhausted, highspeed still has quota → route via highspeed.
	// Distinct patterns reflect the real minimax response (see the
	// PrefersStandardOverHighspeed test for the schema rationale).
	store := newSubStore(t)
	upsertTier(t, store, "MiniMax-M*", 1500, 1500, false)          // standard empty
	upsertTier(t, store, "MiniMax-M*-highspeed", 100, 1500, true)  // highspeed avail

	info := newSubscriptionSource(store).
		GetSubscriptionInfo(context.Background(), "minimax")

	require.True(t, info.Available)
	assert.Equal(t, "MiniMax-M2.7-highspeed", info.Model)
}

func TestSubscriptionSource_NonMinimaxProviderReturnsZero(t *testing.T) {
	// Other vendor names aren't wired up yet; calling for them must not
	// accidentally return a minimax tier's info.
	store := newSubStore(t)
	upsertTier(t, store, "MiniMax-M*", 100, 1500, false)

	info := newSubscriptionSource(store).
		GetSubscriptionInfo(context.Background(), "deepseek")

	assert.False(t, info.Available)
	assert.Empty(t, info.Model)
}

func TestSubscriptionSource_RemainingAndCostRoundTrip(t *testing.T) {
	store := newSubStore(t)
	upsertTier(t, store, "MiniMax-M*", 21, 1500, false) // 1479 remaining

	// Seed token_price_sub so PricingFor can compute EffectiveCostPerCallUSD.
	// (In production the installer seeds this from data/token_price_sub.json.)
	require.NoError(t, store.UpsertSubscriptionPrice(context.Background(), storage.SubscriptionPrice{
		Provider:        "minimax",
		TierPattern:     "MiniMax-M*",
		TotalCount:      1500,
		Highspeed:       false,
		MonthlyPriceCNY: 49,
		WindowHours:     5,
		CNYToUSD:        0.138,
		UpdatedAt:       time.Now(),
	}))

	info := newSubscriptionSource(store).
		GetSubscriptionInfo(context.Background(), "minimax")

	require.True(t, info.Available)
	assert.Equal(t, int64(1479), info.Remaining)
	assert.Equal(t, int64(1500), info.Total)
	// EffectiveCostUSD: 49 × 0.138 / (1500 × 144) ≈ 3.13e-5
	assert.InDelta(t, 49.0*0.138/(1500.0*144.0), info.EffectiveCostUSD, 1e-9)
}
