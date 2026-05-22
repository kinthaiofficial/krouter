package minimax

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestQuotaPoller_DefaultResolverUsesRequestCache(t *testing.T) {
	// Set the in-memory cache via the existing API, simulating that a real
	// proxied request just went through.
	CacheOAuthToken("Bearer sk-from-request-cache")
	t.Cleanup(func() { CacheOAuthToken("") }) // reset between tests

	poller := NewQuotaPoller(openTestStore(t), nil)
	// We don't call PollOnce against a live network; we just confirm the
	// resolver reads from GetCachedToken by exercising it directly.
	assert.Equal(t, "sk-from-request-cache", poller.resolver(context.Background()))
}

func TestQuotaPoller_WithTokenResolverOverridesDefault(t *testing.T) {
	CacheOAuthToken("Bearer sk-from-request-cache")
	t.Cleanup(func() { CacheOAuthToken("") })

	poller := NewQuotaPoller(openTestStore(t), nil).
		WithTokenResolver(func(_ context.Context) string { return "sk-custom-INJECTED" })

	assert.Equal(t, "sk-custom-INJECTED", poller.resolver(context.Background()),
		"custom resolver should take precedence over the request cache")
}

func TestQuotaPoller_WithTokenResolverNilKeepsDefault(t *testing.T) {
	CacheOAuthToken("Bearer sk-original")
	t.Cleanup(func() { CacheOAuthToken("") })

	poller := NewQuotaPoller(openTestStore(t), nil).WithTokenResolver(nil)
	assert.Equal(t, "sk-original", poller.resolver(context.Background()),
		"WithTokenResolver(nil) should keep the existing resolver")
}

func TestShouldFireExhaust(t *testing.T) {
	window1End := time.Date(2026, 5, 22, 20, 0, 0, 0, time.UTC)
	window2End := time.Date(2026, 5, 23, 1, 0, 0, 0, time.UTC)

	// Helper to construct a tier with given remaining at a window.
	mk := func(used, total int64, windowEnd time.Time) storage.SubscriptionQuota {
		return storage.SubscriptionQuota{
			Provider:     "minimax",
			ModelPattern: "MiniMax-M*",
			TotalCount:   total,
			UsedCount:    used,
			WindowEnd:    windowEnd,
		}
	}

	cases := []struct {
		name string
		old  *storage.SubscriptionQuota
		new  storage.SubscriptionQuota
		fire bool
	}{
		{
			name: "fresh exhaustion in same window — most common case",
			old:  ptrQ(mk(1400, 1500, window1End)), // 100 remaining
			new:  mk(1500, 1500, window1End),       // 0 remaining
			fire: true,
		},
		{
			name: "still has quota — must not fire",
			old:  ptrQ(mk(1400, 1500, window1End)),
			new:  mk(1450, 1500, window1End), // 50 remaining
			fire: false,
		},
		{
			name: "first observation, already at zero — fire (let UI catch up)",
			old:  nil,
			new:  mk(1500, 1500, window1End),
			fire: true,
		},
		{
			name: "same window, both exhausted — dedupe, don't refire",
			old:  ptrQ(mk(1500, 1500, window1End)),
			new:  mk(1500, 1500, window1End),
			fire: false,
		},
		{
			name: "window rolled over, fresh window already exhausted (rare) — fire",
			old:  ptrQ(mk(1500, 1500, window1End)),
			new:  mk(1500, 1500, window2End),
			fire: true,
		},
		{
			name: "window rolled over, fresh window has quota — must not fire",
			old:  ptrQ(mk(1500, 1500, window1End)),
			new:  mk(0, 1500, window2End),
			fire: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.fire, shouldFireExhaust(tc.old, tc.new))
		})
	}
}

func ptrQ(q storage.SubscriptionQuota) *storage.SubscriptionQuota { return &q }

func TestQuotaPoller_WithExhaustCallback_Lifecycle(t *testing.T) {
	poller := NewQuotaPoller(openTestStore(t), nil)
	// Default: no callback installed → onExhaust is nil → PollOnce won't dereference it.
	assert.Nil(t, poller.onExhaust)

	called := 0
	poller.WithExhaustCallback(func(_, _ string, _ bool, _ time.Time) { called++ })
	require.NotNil(t, poller.onExhaust)

	poller.WithExhaustCallback(nil)
	require.Nil(t, poller.onExhaust, "passing nil should clear the callback")

	_ = called // silence linter; assertion above proves the field round-trips
}

func TestQuotaPoller_PollOnceSkipsWhenResolverReturnsEmpty(t *testing.T) {
	CacheOAuthToken("") // ensure no cached token

	poller := NewQuotaPoller(openTestStore(t), nil).
		WithTokenResolver(func(_ context.Context) string { return "" })

	// No token → no HTTP call → no error.
	err := poller.PollOnce(context.Background())
	require.NoError(t, err)
}
