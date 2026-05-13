package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetSetting_Miss(t *testing.T) {
	s := openMemoryStore(t)
	val, ok, err := s.GetSetting(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, val)
}

func TestSetSetting_ThenGet(t *testing.T) {
	s := openMemoryStore(t)
	ctx := context.Background()

	require.NoError(t, s.SetSetting(ctx, "preset", "saver"))

	val, ok, err := s.GetSetting(ctx, "preset")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "saver", val)
}

func TestSetSetting_Upsert(t *testing.T) {
	s := openMemoryStore(t)
	ctx := context.Background()

	require.NoError(t, s.SetSetting(ctx, "preset", "saver"))
	require.NoError(t, s.SetSetting(ctx, "preset", "balanced"))

	val, ok, err := s.GetSetting(ctx, "preset")
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "balanced", val)
}

func TestSetSetting_MultipleKeys(t *testing.T) {
	s := openMemoryStore(t)
	ctx := context.Background()

	require.NoError(t, s.SetSetting(ctx, "preset", "quality"))
	require.NoError(t, s.SetSetting(ctx, "theme", "dark"))

	preset, _, _ := s.GetSetting(ctx, "preset")
	theme, _, _ := s.GetSetting(ctx, "theme")
	assert.Equal(t, "quality", preset)
	assert.Equal(t, "dark", theme)
}

func TestCountRequestsToday_Empty(t *testing.T) {
	s := openMemoryStore(t)
	count, err := s.CountRequestsToday(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCountRequestsToday_WithRecords(t *testing.T) {
	s := openMemoryStore(t)
	ctx := context.Background()

	now := time.Now().UTC()
	yesterday := now.Add(-25 * time.Hour)

	// Insert one request from today and one from yesterday.
	require.NoError(t, s.InsertRequest(ctx, storage.RequestRecord{
		ID:        s.NewULID(),
		Timestamp: now,
		Protocol:  "anthropic",
		Provider:  "anthropic",
		Model:     "claude-haiku-4-5",
	}))
	require.NoError(t, s.InsertRequest(ctx, storage.RequestRecord{
		ID:        s.NewULID(),
		Timestamp: yesterday,
		Protocol:  "anthropic",
		Provider:  "anthropic",
		Model:     "claude-haiku-4-5",
	}))

	count, err := s.CountRequestsToday(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
}
