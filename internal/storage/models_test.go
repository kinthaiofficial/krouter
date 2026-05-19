package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveAndGetDiscoveredModels(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	models := []storage.DiscoveredModel{
		{Provider: "anthropic", ModelID: "claude-opus-4-7", DisplayName: "Claude Opus 4.7"},
		{Provider: "anthropic", ModelID: "claude-sonnet-4-6", DisplayName: "Claude Sonnet 4.6"},
	}
	require.NoError(t, s.SaveDiscoveredModels(ctx, "anthropic", models))

	got, fetchedAt, err := s.GetDiscoveredModels(ctx, "anthropic")
	require.NoError(t, err)
	require.Len(t, got, 2)
	assert.Equal(t, "claude-opus-4-7", got[0].ModelID)
	assert.Equal(t, "Claude Opus 4.7", got[0].DisplayName)
	assert.Equal(t, "anthropic", got[0].Provider)
	assert.False(t, fetchedAt.IsZero())
	assert.WithinDuration(t, time.Now(), fetchedAt, 5*time.Second)
}

func TestSaveDiscoveredModels_Replace(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	first := []storage.DiscoveredModel{
		{Provider: "deepseek", ModelID: "deepseek-chat", DisplayName: "DeepSeek Chat"},
		{Provider: "deepseek", ModelID: "deepseek-coder", DisplayName: "DeepSeek Coder"},
	}
	require.NoError(t, s.SaveDiscoveredModels(ctx, "deepseek", first))

	second := []storage.DiscoveredModel{
		{Provider: "deepseek", ModelID: "deepseek-v3", DisplayName: "DeepSeek V3"},
	}
	require.NoError(t, s.SaveDiscoveredModels(ctx, "deepseek", second))

	got, _, err := s.GetDiscoveredModels(ctx, "deepseek")
	require.NoError(t, err)
	require.Len(t, got, 1, "second save must fully replace the first")
	assert.Equal(t, "deepseek-v3", got[0].ModelID)
}

func TestSaveDiscoveredModels_EmptyClears(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveDiscoveredModels(ctx, "groq", []storage.DiscoveredModel{
		{Provider: "groq", ModelID: "llama3-8b", DisplayName: "Llama3 8B"},
	}))
	require.NoError(t, s.SaveDiscoveredModels(ctx, "groq", nil))

	got, ts, err := s.GetDiscoveredModels(ctx, "groq")
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.True(t, ts.IsZero())
}

func TestGetDiscoveredModels_NoData(t *testing.T) {
	s := openTestStore(t)
	got, ts, err := s.GetDiscoveredModels(context.Background(), "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, got)
	assert.True(t, ts.IsZero())
}

func TestGetAllDiscoveredModels(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveDiscoveredModels(ctx, "anthropic", []storage.DiscoveredModel{
		{Provider: "anthropic", ModelID: "claude-sonnet-4-6"},
	}))
	require.NoError(t, s.SaveDiscoveredModels(ctx, "deepseek", []storage.DiscoveredModel{
		{Provider: "deepseek", ModelID: "deepseek-chat"},
		{Provider: "deepseek", ModelID: "deepseek-coder"},
	}))

	all, err := s.GetAllDiscoveredModels(ctx)
	require.NoError(t, err)
	assert.Len(t, all["anthropic"], 1)
	assert.Len(t, all["deepseek"], 2)
}

func TestOldestModelDiscoveryAge_Empty(t *testing.T) {
	s := openTestStore(t)
	ts, err := s.OldestModelDiscoveryAge(context.Background())
	require.NoError(t, err)
	assert.True(t, ts.IsZero())
}

func TestOldestModelDiscoveryAge_HasData(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.SaveDiscoveredModels(ctx, "anthropic", []storage.DiscoveredModel{
		{Provider: "anthropic", ModelID: "claude-opus-4-7"},
	}))

	ts, err := s.OldestModelDiscoveryAge(ctx)
	require.NoError(t, err)
	assert.False(t, ts.IsZero())
	assert.WithinDuration(t, time.Now(), ts, 5*time.Second)
}
