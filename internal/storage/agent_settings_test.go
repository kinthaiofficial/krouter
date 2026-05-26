package storage_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAgentSettings_GetMissingReturnsNilNil(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	got, err := s.GetAppSetting(ctx, "no-such-agent")
	require.NoError(t, err)
	require.Nil(t, got)
}

func TestAgentSettings_UpsertAndGet(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	now := time.Now().UnixMilli()
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID:       "openclaw",
		Enabled:       true,
		ConfigPath:    "/tmp/openclaw.json",
		LastScannedAt: &now,
	}))

	got, err := s.GetAppSetting(ctx, "openclaw")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "openclaw", got.AppID)
	assert.True(t, got.Enabled)
	assert.Equal(t, "/tmp/openclaw.json", got.ConfigPath)
	require.NotNil(t, got.LastScannedAt)
	assert.Equal(t, now, *got.LastScannedAt)
	assert.Empty(t, got.LastError)
}

func TestAgentSettings_UpsertPreservesScannedAtOnPartialUpdate(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	scanTS := time.Now().UnixMilli()
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID:       "openclaw",
		Enabled:       true,
		ConfigPath:    "/tmp/openclaw.json",
		LastScannedAt: &scanTS,
	}))

	// Re-upsert WITHOUT LastScannedAt: should keep the original timestamp via COALESCE.
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID:    "openclaw",
		Enabled:    false,
		ConfigPath: "/new/path.json",
	}))

	got, err := s.GetAppSetting(ctx, "openclaw")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.False(t, got.Enabled)
	assert.Equal(t, "/new/path.json", got.ConfigPath)
	require.NotNil(t, got.LastScannedAt)
	assert.Equal(t, scanTS, *got.LastScannedAt, "scan timestamp should be preserved by COALESCE")
}

func TestAgentSettings_List(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "claude-code", Enabled: true, ConfigPath: "/a",
	}))
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: false, ConfigPath: "/b",
	}))

	all, err := s.ListAppSettings(ctx)
	require.NoError(t, err)
	require.Len(t, all, 2)
	// Ordered by agent_id.
	assert.Equal(t, "claude-code", all[0].AppID)
	assert.Equal(t, "openclaw", all[1].AppID)
}

func TestAgentSettings_SetEnabled(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	// Setting enabled on a non-existent agent → ErrNoRows
	err := s.SetAppEnabled(ctx, "ghost", true)
	require.True(t, errors.Is(err, sql.ErrNoRows))

	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: false, ConfigPath: "/x",
	}))
	require.NoError(t, s.SetAppEnabled(ctx, "openclaw", true))

	got, _ := s.GetAppSetting(ctx, "openclaw")
	assert.True(t, got.Enabled)
}

func TestAgentSettings_RecordScan(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))

	// Successful scan: errorMsg empty, last_error stays null.
	now := time.Now().UnixMilli()
	require.NoError(t, s.RecordAppScan(ctx, "openclaw", now, ""))

	got, _ := s.GetAppSetting(ctx, "openclaw")
	require.NotNil(t, got.LastScannedAt)
	assert.Equal(t, now, *got.LastScannedAt)
	assert.Empty(t, got.LastError)

	// Failed scan: errorMsg recorded.
	require.NoError(t, s.RecordAppScan(ctx, "openclaw", now+1000, "parse error"))
	got, _ = s.GetAppSetting(ctx, "openclaw")
	assert.Equal(t, "parse error", got.LastError)
}

func TestAgentSettings_Delete(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))

	require.NoError(t, s.DeleteAppSetting(ctx, "openclaw"))
	got, err := s.GetAppSetting(ctx, "openclaw")
	require.NoError(t, err)
	assert.Nil(t, got)

	// Deleting a non-existent row is a no-op.
	require.NoError(t, s.DeleteAppSetting(ctx, "ghost"))
}
