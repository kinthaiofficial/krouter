package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSubPriceStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestUpsertAndFindSubscriptionPrice(t *testing.T) {
	store := newSubPriceStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	row := storage.SubscriptionPrice{
		Provider:        "minimax",
		TierPattern:     "MiniMax-M*",
		TotalCount:      1500,
		Highspeed:       false,
		MonthlyPriceCNY: 49,
		WindowHours:     5,
		CNYToUSD:        0.138,
		DataSourceURL:   "https://example",
		UpdatedAt:       now,
	}
	require.NoError(t, store.UpsertSubscriptionPrice(ctx, row))

	got, err := store.FindSubscriptionPrice(ctx, "minimax", 1500, false)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, row.Provider, got.Provider)
	assert.Equal(t, row.TierPattern, got.TierPattern)
	assert.Equal(t, row.TotalCount, got.TotalCount)
	assert.Equal(t, row.Highspeed, got.Highspeed)
	assert.Equal(t, row.MonthlyPriceCNY, got.MonthlyPriceCNY)
	assert.Equal(t, row.WindowHours, got.WindowHours)
	assert.Equal(t, row.CNYToUSD, got.CNYToUSD)
	assert.Equal(t, row.DataSourceURL, got.DataSourceURL)
}

func TestFindSubscriptionPrice_UnknownReturnsNil(t *testing.T) {
	store := newSubPriceStore(t)
	got, err := store.FindSubscriptionPrice(context.Background(), "minimax", 999, false)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestSubscriptionPrice_EffectiveCostFormula(t *testing.T) {
	p := storage.SubscriptionPrice{
		MonthlyPriceCNY: 49,
		CNYToUSD:        0.138,
		TotalCount:      1500,
		WindowHours:     5, // 144 windows/month
	}
	// 49 × 0.138 / (1500 × 144) ≈ $0.0000313/call
	want := 49.0 * 0.138 / (1500.0 * 144.0)
	assert.InDelta(t, want, p.EffectiveCostPerCallUSD(), 1e-12)
	assert.InDelta(t, 49.0*0.138, p.MonthlyPriceUSD(), 1e-12)
}

func TestSubscriptionPrice_ZeroValueIsFree(t *testing.T) {
	var p storage.SubscriptionPrice
	assert.Equal(t, 0.0, p.EffectiveCostPerCallUSD())
	assert.Equal(t, 0.0, p.MonthlyPriceUSD())
}

func TestUpsertSubscriptionPrice_ReplacesOnConflict(t *testing.T) {
	store := newSubPriceStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	base := storage.SubscriptionPrice{
		Provider: "minimax", TierPattern: "MiniMax-M*",
		TotalCount: 1500, Highspeed: false,
		MonthlyPriceCNY: 49, WindowHours: 5, CNYToUSD: 0.138,
		UpdatedAt: now,
	}
	require.NoError(t, store.UpsertSubscriptionPrice(ctx, base))

	// Updated price for same SKU should replace, not duplicate.
	base.MonthlyPriceCNY = 59
	base.UpdatedAt = now.Add(time.Hour)
	require.NoError(t, store.UpsertSubscriptionPrice(ctx, base))

	all, err := store.ListSubscriptionPrices(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	assert.Equal(t, 59.0, all[0].MonthlyPriceCNY)
}
