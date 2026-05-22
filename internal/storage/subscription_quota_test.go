package storage

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestSubscriptionQuota_MatchesModel covers the spec/05 §8 wildcard matrix:
// each row is a (pattern, query, want) tuple that mirrors a real shape
// returned by the minimax token-plan API.
func TestSubscriptionQuota_MatchesModel(t *testing.T) {
	cases := []struct {
		pattern string
		query   string
		want    bool
	}{
		// LLM family wildcard — should cover every MiniMax-M variant.
		{"MiniMax-M*", "MiniMax-M2.7", true},
		{"MiniMax-M*", "MiniMax-M2.7-highspeed", true},
		{"MiniMax-M*", "MiniMax-M2.5", true},
		{"MiniMax-M*", "MiniMax-M2", true},

		// Different families must not match.
		{"MiniMax-M*", "speech-hd", false},
		{"MiniMax-M*", "MiniMax-Hailuo-2.3-Fast-6s-768p", false},

		// Exact pattern (no wildcard) — only the exact id matches.
		{"speech-hd", "speech-hd", true},
		{"speech-hd", "speech-hd-pro", false},
		{"speech-hd", "MiniMax-M2.7", false},

		// Hailuo video family wildcard.
		{"MiniMax-Hailuo-2*", "MiniMax-Hailuo-2.3-Fast-6s-768p", true},
		{"MiniMax-Hailuo-2*", "MiniMax-Hailuo-3", false},

		// Empty / zero-value guards.
		{"", "MiniMax-M2.7", false},
		{"MiniMax-M*", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.pattern+" / "+tc.query, func(t *testing.T) {
			q := &SubscriptionQuota{ModelPattern: tc.pattern}
			assert.Equal(t, tc.want, q.MatchesModel(tc.query))
		})
	}
}

func TestSubscriptionQuota_MatchesModel_NilReceiverSafe(t *testing.T) {
	var q *SubscriptionQuota
	assert.False(t, q.MatchesModel("MiniMax-M2.7"))
}
