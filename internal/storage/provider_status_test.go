package storage_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProviderStatus_RecordSuccess(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.RecordSuccess(ctx, "anthropic"))

	ps, err := s.GetProviderStatus(ctx, "anthropic")
	require.NoError(t, err)
	require.NotNil(t, ps)
	assert.Equal(t, "anthropic", ps.Provider)
	assert.Equal(t, 0, ps.ConsecutiveFailures)
	assert.NotNil(t, ps.LastSuccessAt)
	assert.Nil(t, ps.LastFailureAt)
}

func TestProviderStatus_RecordFailure(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.RecordFailure(ctx, "deepseek", 503))

	ps, err := s.GetProviderStatus(ctx, "deepseek")
	require.NoError(t, err)
	require.NotNil(t, ps)
	assert.Equal(t, 1, ps.ConsecutiveFailures)
	assert.Equal(t, 503, ps.LastErrorCode)
	assert.NotNil(t, ps.LastFailureAt)
	assert.Nil(t, ps.LastSuccessAt)
}

func TestProviderStatus_ConsecutiveFailures_Reset(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.RecordFailure(ctx, "groq", 500))
	require.NoError(t, s.RecordFailure(ctx, "groq", 500))
	require.NoError(t, s.RecordFailure(ctx, "groq", 500))

	ps, err := s.GetProviderStatus(ctx, "groq")
	require.NoError(t, err)
	assert.Equal(t, 3, ps.ConsecutiveFailures)

	require.NoError(t, s.RecordSuccess(ctx, "groq"))

	ps, err = s.GetProviderStatus(ctx, "groq")
	require.NoError(t, err)
	assert.Equal(t, 0, ps.ConsecutiveFailures, "success must reset consecutive failures")
}

func TestProviderStatus_GetNonExistent(t *testing.T) {
	s := openTestStore(t)
	ps, err := s.GetProviderStatus(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Nil(t, ps)
}

func TestProviderStatus_ListStatuses(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.RecordSuccess(ctx, "anthropic"))
	require.NoError(t, s.RecordFailure(ctx, "openai", 429))

	statuses, err := s.ListProviderStatuses(ctx)
	require.NoError(t, err)
	assert.Len(t, statuses, 2)

	names := make(map[string]bool)
	for _, ps := range statuses {
		names[ps.Provider] = true
	}
	assert.True(t, names["anthropic"])
	assert.True(t, names["openai"])
}

func TestProviderStatus_RollingRate_DegradesOnFailure(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	// Start with 10 successes.
	for i := 0; i < 10; i++ {
		require.NoError(t, s.RecordSuccess(ctx, "anthropic"))
	}
	ps, err := s.GetProviderStatus(ctx, "anthropic")
	require.NoError(t, err)
	assert.InDelta(t, 1.0, ps.RollingSuccessRate, 0.01)

	// One failure should nudge the rate down.
	require.NoError(t, s.RecordFailure(ctx, "anthropic", 500))
	ps, err = s.GetProviderStatus(ctx, "anthropic")
	require.NoError(t, err)
	assert.Less(t, ps.RollingSuccessRate, 1.0)
}
