package minimax

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
)

func almostEqual(t *testing.T, want, got float64) {
	t.Helper()
	if math.Abs(want-got) > 1e-9 {
		t.Errorf("expected %v, got %v", want, got)
	}
}

func TestEffectiveCost_StandardTiers(t *testing.T) {
	// $49 / 1500 calls ≈ $0.03267 per call
	almostEqual(t, 49.0/1500.0, EffectiveCostPerCallUSD("MiniMax-M*", 1500, false))
	// $19 / 600
	almostEqual(t, 19.0/600.0, EffectiveCostPerCallUSD("MiniMax-M*", 600, false))
	// $599 / 30000
	almostEqual(t, 599.0/30000.0, EffectiveCostPerCallUSD("MiniMax-M*", 30000, false))
}

func TestEffectiveCost_HighspeedTiers(t *testing.T) {
	// Highspeed plans are pricier; lookup must distinguish them.
	almostEqual(t, 79.0/1500.0, EffectiveCostPerCallUSD("MiniMax-M*", 1500, true))
	almostEqual(t, 999.0/30000.0, EffectiveCostPerCallUSD("MiniMax-M*", 30000, true))
}

func TestEffectiveCost_UnknownTierReturnsZero(t *testing.T) {
	// User on an SKU we don't know about (e.g. custom enterprise plan).
	// "Unknown" = treat as free; routing should still prefer this over
	// per-token endpoints.
	assert.Equal(t, 0.0, EffectiveCostPerCallUSD("MiniMax-M*", 9999, false))
	assert.Equal(t, 0.0, EffectiveCostPerCallUSD("nonsense-tier", 1500, false))
}

func TestMonthlyPriceLookup(t *testing.T) {
	assert.Equal(t, 49.0, MonthlyPriceUSD("MiniMax-M*", 1500, false))
	assert.Equal(t, 79.0, MonthlyPriceUSD("MiniMax-M*", 1500, true))
	assert.Equal(t, 0.0, MonthlyPriceUSD("MiniMax-M*", 9999, false))
}
