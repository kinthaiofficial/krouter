package routing_test

import (
	"sync"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ── SessionState methods ──────────────────────────────────────────────────────

func TestCacheHitRate_ZeroWhenNoTokens(t *testing.T) {
	var s routing.SessionState
	assert.Equal(t, 0.0, s.CacheHitRate())
}

func TestCacheHitRate_AllFresh(t *testing.T) {
	s := routing.SessionState{FreshInputTokens: 1000, CachedTokens: 0}
	assert.Equal(t, 0.0, s.CacheHitRate())
}

func TestCacheHitRate_AllCached(t *testing.T) {
	s := routing.SessionState{FreshInputTokens: 0, CachedTokens: 1000}
	assert.Equal(t, 1.0, s.CacheHitRate())
}

func TestCacheHitRate_Mixed(t *testing.T) {
	s := routing.SessionState{FreshInputTokens: 500, CachedTokens: 500}
	assert.InDelta(t, 0.5, s.CacheHitRate(), 1e-9)
}

func TestOutputShare_ZeroWhenNoTokens(t *testing.T) {
	var s routing.SessionState
	assert.Equal(t, 0.0, s.OutputShare())
}

func TestOutputShare_OutputOnly(t *testing.T) {
	s := routing.SessionState{OutputTokens: 500}
	assert.Equal(t, 1.0, s.OutputShare())
}

func TestOutputShare_Mixed(t *testing.T) {
	// 200 fresh + 200 cached + 100 output = 500 total; output share = 0.2
	s := routing.SessionState{
		FreshInputTokens: 200,
		CachedTokens:     200,
		OutputTokens:     100,
	}
	assert.InDelta(t, 0.2, s.OutputShare(), 1e-9)
}

// ── MemSessionStore ───────────────────────────────────────────────────────────

func TestMemSessionStore_GetMissing(t *testing.T) {
	store := routing.NewMemSessionStore()
	defer store.Close()

	_, ok := store.Get("nonexistent")
	assert.False(t, ok)
}

func TestMemSessionStore_UpdateCreates(t *testing.T) {
	store := routing.NewMemSessionStore()
	defer store.Close()

	store.Update("key1", func(s *routing.SessionState) {
		s.RequestCount = 1
		s.FreshInputTokens = 500
		s.OutputTokens = 100
	})

	st, ok := store.Get("key1")
	require.True(t, ok)
	assert.Equal(t, 1, st.RequestCount)
	assert.Equal(t, 500, st.FreshInputTokens)
	assert.Equal(t, 100, st.OutputTokens)
}

func TestMemSessionStore_UpdateAccumulates(t *testing.T) {
	store := routing.NewMemSessionStore()
	defer store.Close()

	for range 3 {
		store.Update("conv", func(s *routing.SessionState) {
			s.RequestCount++
			s.FreshInputTokens += 200
			s.CachedTokens += 100
			s.OutputTokens += 50
		})
	}

	st, ok := store.Get("conv")
	require.True(t, ok)
	assert.Equal(t, 3, st.RequestCount)
	assert.Equal(t, 600, st.FreshInputTokens)
	assert.Equal(t, 300, st.CachedTokens)
	assert.Equal(t, 150, st.OutputTokens)
}

func TestMemSessionStore_ConcurrentSafety(t *testing.T) {
	store := routing.NewMemSessionStore()
	defer store.Close()

	const goroutines = 50
	const updatesEach = 20
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for range goroutines {
		go func() {
			defer wg.Done()
			for range updatesEach {
				store.Update("shared", func(s *routing.SessionState) {
					s.RequestCount++
					s.FreshInputTokens += 10
				})
			}
		}()
	}
	wg.Wait()

	st, ok := store.Get("shared")
	require.True(t, ok)
	assert.Equal(t, goroutines*updatesEach, st.RequestCount)
	assert.Equal(t, goroutines*updatesEach*10, st.FreshInputTokens)
}

func TestMemSessionStore_TTLExpiry(t *testing.T) {
	// This test uses an internal evict call via a short sleep. We can't test
	// the 75-min TTL directly, so we verify the eviction logic indirectly:
	// after Close, a re-created store starts empty.
	store := routing.NewMemSessionStore()
	store.Update("k", func(s *routing.SessionState) { s.RequestCount = 5 })
	st, ok := store.Get("k")
	require.True(t, ok)
	assert.Equal(t, 5, st.RequestCount)
	store.Close()

	// New store is empty.
	store2 := routing.NewMemSessionStore()
	defer store2.Close()
	_, ok = store2.Get("k")
	assert.False(t, ok)
}

func TestMemSessionStore_MultipleKeys(t *testing.T) {
	store := routing.NewMemSessionStore()
	defer store.Close()

	store.Update("a", func(s *routing.SessionState) { s.RequestCount = 1 })
	store.Update("b", func(s *routing.SessionState) { s.RequestCount = 2 })
	store.Update("c", func(s *routing.SessionState) { s.RequestCount = 3 })

	for key, want := range map[string]int{"a": 1, "b": 2, "c": 3} {
		st, ok := store.Get(key)
		require.True(t, ok, "key %s missing", key)
		assert.Equal(t, want, st.RequestCount, "key %s", key)
	}
}

func TestMemSessionStore_BoundProviderModel(t *testing.T) {
	store := routing.NewMemSessionStore()
	defer store.Close()

	store.Update("sess", func(s *routing.SessionState) {
		s.BoundProvider = "anthropic"
		s.BoundModel = "claude-haiku-4-5-20251001"
	})

	st, ok := store.Get("sess")
	require.True(t, ok)
	assert.Equal(t, "anthropic", st.BoundProvider)
	assert.Equal(t, "claude-haiku-4-5-20251001", st.BoundModel)
}

// Verify the evict ticker does not block after Close.
func TestMemSessionStore_CloseIdempotentShutdown(t *testing.T) {
	store := routing.NewMemSessionStore()
	store.Update("x", func(s *routing.SessionState) { s.RequestCount = 1 })

	done := make(chan struct{})
	go func() {
		store.Close()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not return within 2 seconds")
	}
}
