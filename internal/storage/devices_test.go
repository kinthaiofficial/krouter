package storage_test

import (
	"context"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeDevice(id, name string) storage.PairedDevice {
	return storage.PairedDevice{
		DeviceID:   id,
		DeviceName: name,
		TokenHash:  "abc" + id,
		IPAddress:  "192.168.1.100",
		UserAgent:  "TestAgent/1.0",
		PairedAt:   time.Now().UTC().Add(-time.Hour),
	}
}

func TestInsertDevice_And_GetByHash(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	d := makeDevice("dev-001", "Alice's MacBook")
	require.NoError(t, s.InsertDevice(ctx, d))

	got, err := s.GetDeviceByTokenHash(ctx, d.TokenHash)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, d.DeviceID, got.DeviceID)
	assert.Equal(t, d.DeviceName, got.DeviceName)
}

func TestGetDeviceByTokenHash_NotFound(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	got, err := s.GetDeviceByTokenHash(ctx, "nonexistent-hash")
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestListDevices_Empty(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	devices, err := s.ListDevices(ctx)
	require.NoError(t, err)
	assert.Empty(t, devices)
}

func TestListDevices_Multiple(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	require.NoError(t, s.InsertDevice(ctx, makeDevice("dev-a", "Device A")))
	require.NoError(t, s.InsertDevice(ctx, makeDevice("dev-b", "Device B")))

	devices, err := s.ListDevices(ctx)
	require.NoError(t, err)
	assert.Len(t, devices, 2)
}

func TestDeleteDevice(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	d := makeDevice("dev-del", "To Delete")
	require.NoError(t, s.InsertDevice(ctx, d))

	require.NoError(t, s.DeleteDevice(ctx, d.DeviceID))

	devices, err := s.ListDevices(ctx)
	require.NoError(t, err)
	assert.Empty(t, devices)
}

func TestUpdateDeviceLastSeen(t *testing.T) {
	s := openMigratedStore(t)
	ctx := context.Background()

	d := makeDevice("dev-ls", "Last Seen Test")
	require.NoError(t, s.InsertDevice(ctx, d))

	now := time.Now().UTC().Truncate(time.Second)
	require.NoError(t, s.UpdateDeviceLastSeen(ctx, d.DeviceID, now))

	got, err := s.GetDeviceByTokenHash(ctx, d.TokenHash)
	require.NoError(t, err)
	require.NotNil(t, got)
	require.NotNil(t, got.LastSeenAt)
	assert.Equal(t, now.Format(time.RFC3339), got.LastSeenAt.Format(time.RFC3339))
}
