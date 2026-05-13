package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQuota_GetNonExistent(t *testing.T) {
	s := openTestStore(t)
	qw, err := s.GetQuota(context.Background(), "5h")
	require.NoError(t, err)
	assert.Nil(t, qw)
}

func TestQuota_IncrementCreates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.IncrementQuota(ctx, "5h", 1000))

	qw, err := s.GetQuota(ctx, "5h")
	require.NoError(t, err)
	require.NotNil(t, qw)
	assert.Equal(t, int64(1000), qw.TokensUsed)
	assert.Equal(t, "5h", qw.WindowType)
}

func TestQuota_IncrementAccumulates(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.IncrementQuota(ctx, "weekly", 500))
	require.NoError(t, s.IncrementQuota(ctx, "weekly", 300))

	qw, err := s.GetQuota(ctx, "weekly")
	require.NoError(t, err)
	assert.Equal(t, int64(800), qw.TokensUsed)
}

func TestQuota_Reset(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.IncrementQuota(ctx, "opus", 9999))
	now := time.Now().UTC()
	require.NoError(t, s.ResetQuota(ctx, "opus", now, now.Add(24*time.Hour)))

	qw, err := s.GetQuota(ctx, "opus")
	require.NoError(t, err)
	assert.Equal(t, int64(0), qw.TokensUsed)
}

func TestQuota_List(t *testing.T) {
	s := openTestStore(t)
	ctx := context.Background()

	require.NoError(t, s.IncrementQuota(ctx, "5h", 100))
	require.NoError(t, s.IncrementQuota(ctx, "weekly", 200))
	require.NoError(t, s.IncrementQuota(ctx, "opus", 300))

	quotas, err := s.ListQuotas(ctx)
	require.NoError(t, err)
	assert.Len(t, quotas, 3)
}
