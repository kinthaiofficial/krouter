package minimax

import (
	"context"
	"testing"

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

func TestQuotaPoller_PollOnceSkipsWhenResolverReturnsEmpty(t *testing.T) {
	CacheOAuthToken("") // ensure no cached token

	poller := NewQuotaPoller(openTestStore(t), nil).
		WithTokenResolver(func(_ context.Context) string { return "" })

	// No token → no HTTP call → no error.
	err := poller.PollOnce(context.Background())
	require.NoError(t, err)
}
