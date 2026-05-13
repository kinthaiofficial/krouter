package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openMigratedStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestUpsertAndGetAllPrices(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	err := s.UpsertPrice(ctx, storage.PriceCacheEntry{
		ModelID:            "claude-sonnet-4-5",
		Provider:           "anthropic",
		InputCostPerToken:  0.000003,
		OutputCostPerToken: 0.000015,
		MaxTokens:          200000,
		UpdatedAt:          now,
	})
	require.NoError(t, err)

	entries, err := s.GetAllPrices(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.Equal(t, "claude-sonnet-4-5", entries[0].ModelID)
	assert.InDelta(t, 0.000003, entries[0].InputCostPerToken, 1e-10)
}

func TestUpsertPrice_UpdatesExisting(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	e := storage.PriceCacheEntry{
		ModelID:            "gpt-4o",
		Provider:           "openai",
		InputCostPerToken:  0.0025,
		OutputCostPerToken: 0.01,
		UpdatedAt:          now,
	}
	require.NoError(t, s.UpsertPrice(ctx, e))

	// Update the price.
	e.InputCostPerToken = 0.002
	require.NoError(t, s.UpsertPrice(ctx, e))

	entries, err := s.GetAllPrices(ctx)
	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.InDelta(t, 0.002, entries[0].InputCostPerToken, 1e-10)
}

func TestSyncMeta_GetSet(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	// Get absent key returns "".
	v, err := s.GetSyncMeta(ctx, "last_etag")
	require.NoError(t, err)
	assert.Empty(t, v)

	require.NoError(t, s.SetSyncMeta(ctx, "last_etag", `"abc123"`))

	v, err = s.GetSyncMeta(ctx, "last_etag")
	require.NoError(t, err)
	assert.Equal(t, `"abc123"`, v)
}

func TestSumCostMicroUSD(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	now := time.Now().UTC()

	for _, cost := range []int64{1000, 2000, 3000} {
		rec := storage.RequestRecord{
			ID:           s.NewULID(),
			Timestamp:    now,
			Protocol:     "anthropic",
			Provider:     "anthropic",
			Model:        "claude-sonnet-4-5",
			CostMicroUSD: cost,
			StatusCode:   200,
		}
		require.NoError(t, s.InsertRequest(ctx, rec))
	}

	total, err := s.SumCostMicroUSD(ctx, now.Add(-time.Minute))
	require.NoError(t, err)
	assert.Equal(t, int64(6000), total)

	// Future cutoff — no records.
	total2, err := s.SumCostMicroUSD(ctx, now.Add(time.Hour))
	require.NoError(t, err)
	assert.Equal(t, int64(0), total2)
}
