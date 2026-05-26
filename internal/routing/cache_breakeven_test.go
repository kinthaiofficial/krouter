package routing_test

import (
	"math"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/stretchr/testify/assert"
)

// cacheHitBreakeven is package-internal, exposed for tests via the export file.

func TestCacheHitBreakeven_KnownPairs(t *testing.T) {
	// Prices are per-token in USD.
	// Opus:   $15/M  = 0.000015
	// Sonnet: $3/M   = 0.000003
	// Haiku:  $0.8/M = 0.0000008
	// DeepSeek: $0.14/M = 0.00000014
	const (
		opus     = 15.0 / 1e6
		sonnet   = 3.0 / 1e6
		haiku    = 0.8 / 1e6
		deepseek = 0.14 / 1e6
	)

	tests := []struct {
		name      string
		bound     float64
		candidate float64
		wantApprox float64 // expected breakeven; -1 means "unknown"
	}{
		// p* = (1 - 1.25 × 3/15) / 0.9 = (1 - 0.25) / 0.9 = 0.75/0.9 ≈ 0.833
		{"opus→sonnet", opus, sonnet, (1 - 1.25*(sonnet/opus)) / 0.9},
		// p* = (1 - 1.25 × 0.8/3) / 0.9 ≈ 0.741
		{"sonnet→haiku", sonnet, haiku, (1 - 1.25*(haiku/sonnet)) / 0.9},
		// p* = (1 - 1.25 × 0.8/15) / 0.9 ≈ 1.0 → clamped to 1.0
		{"opus→haiku", opus, haiku, 1.0},
		// p* = (1 - 1.25 × 0.14/3) / 0.9 ≈ 1.0 → clamped to 1.0
		{"sonnet→deepseek", sonnet, deepseek, 1.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := routing.CacheHitBreakevenExport(tt.bound, tt.candidate)
			assert.InDelta(t, tt.wantApprox, got, 1e-3)
		})
	}
}

func TestCacheHitBreakeven_ZeroPrice(t *testing.T) {
	// Unknown prices return -1.
	assert.Equal(t, -1.0, routing.CacheHitBreakevenExport(0, 0.000003))
	assert.Equal(t, -1.0, routing.CacheHitBreakevenExport(0.000015, 0))
	assert.Equal(t, -1.0, routing.CacheHitBreakevenExport(0, 0))
}

func TestCacheHitBreakeven_SamePrice(t *testing.T) {
	// No price difference → no incentive to switch → 0.
	p := 0.000003
	assert.Equal(t, 0.0, routing.CacheHitBreakevenExport(p, p))
}

func TestCacheHitBreakeven_CandidateMoreExpensive(t *testing.T) {
	// Candidate costs more → no incentive to switch → 0.
	assert.Equal(t, 0.0, routing.CacheHitBreakevenExport(0.000003, 0.000015))
}

func TestCacheHitBreakeven_ResultInRange(t *testing.T) {
	// For any valid price pair, result must be in [-1, 1].
	pairs := [][2]float64{
		{0.000015, 0.000003},
		{0.000003, 0.0000008},
		{0.000015, 0.0000008},
		{0.000003, 0.00000014},
	}
	for _, p := range pairs {
		v := routing.CacheHitBreakevenExport(p[0], p[1])
		assert.True(t, v >= -1.0 && v <= 1.0,
			"breakeven %.4f out of [-1,1] for bound=%.2e, cand=%.2e", v, p[0], p[1])
	}
}

func TestCacheHitBreakeven_IsFinite(t *testing.T) {
	v := routing.CacheHitBreakevenExport(0.000015, 0.000003)
	assert.False(t, math.IsNaN(v))
	assert.False(t, math.IsInf(v, 0))
}
