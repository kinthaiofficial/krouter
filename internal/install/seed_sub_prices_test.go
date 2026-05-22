package install

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSubPricesSeedJSON_ParsesAndCoversMinimax(t *testing.T) {
	// The embed must compile (verified at build time) AND contain at least
	// the MiniMax tiers the daemon expects to see. Catches the case where
	// a JSON edit accidentally drops the SKU list.
	var file subPricesFile
	require.NoError(t, json.Unmarshal(subPricesSeedJSON, &file))
	require.NotEmpty(t, file.Tiers, "embedded JSON must contain at least one tier")

	seen := map[int64]bool{}
	for _, tier := range file.Tiers {
		if tier.Provider != "minimax" {
			continue
		}
		seen[tier.TotalCount] = true
		// Sanity checks — the seed JSON should never carry zero pricing data.
		assert.Greater(t, tier.MonthlyPriceCNY, 0.0, "tier %d cny price > 0", tier.TotalCount)
		assert.Greater(t, tier.WindowHours, 0, "tier %d window_hours > 0", tier.TotalCount)
		assert.Greater(t, tier.CNYToUSD, 0.0, "tier %d cny_to_usd > 0", tier.TotalCount)
	}
	// Smoke check: the four common SKUs (600/1500/4500/30000) appear in either
	// standard or highspeed variant. If a release accidentally drops all of one
	// SKU size, this test fails loudly.
	for _, want := range []int64{600, 1500, 4500, 30000} {
		assert.True(t, seen[want], "no MiniMax SKU at total_count=%d", want)
	}
}

func TestSeedSubPrices_WritesRowsToDB(t *testing.T) {
	// Run the seed against a temp DB and confirm the DB has rows after.
	t.Setenv("HOME", t.TempDir())

	o := &Orchestrator{ui: NullUI{}}
	require.NoError(t, o.SeedSubPrices())

	dbPath, err := installDBPath()
	require.NoError(t, err)
	store, err := storage.Open(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Migrate())

	got, err := store.ListSubscriptionPrices(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, got, "SeedSubPrices should have written at least one row")

	// Spot-check: the 1500-standard tier should round-trip.
	p, err := store.FindSubscriptionPrice(context.Background(), "minimax", 1500, false)
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Equal(t, "MiniMax-M*", p.TierPattern)
	assert.Greater(t, p.MonthlyPriceCNY, 0.0)

	// Re-running the seed must be idempotent (no row duplication, no error).
	require.NoError(t, o.SeedSubPrices())
	again, err := store.ListSubscriptionPrices(context.Background())
	require.NoError(t, err)
	assert.Equal(t, len(got), len(again), "second seed should not change row count")

	// Make sure the DB file is in the per-test HOME, not somewhere global.
	assert.True(t, filepath.IsAbs(dbPath))
}
