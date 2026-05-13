package storage_test

import (
	"testing"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestPackageBuilds(t *testing.T) {
	s := openTestStore(t)
	assert.NotNil(t, s)
}

func TestOpenInMemory(t *testing.T) {
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NotNil(t, s)
	_ = s.Close()
}

func TestMigrationsApply(t *testing.T) {
	s := openTestStore(t)
	var version int
	err := s.DB().QueryRow(
		`SELECT version FROM schema_migrations WHERE version = 1`,
	).Scan(&version)
	require.NoError(t, err)
	assert.Equal(t, 1, version)
}

func TestAllTablesExist(t *testing.T) {
	s := openTestStore(t)

	rows, err := s.DB().Query(
		`SELECT name FROM sqlite_master WHERE type='table' ORDER BY name`,
	)
	require.NoError(t, err)
	defer func() { _ = rows.Close() }()

	var tables []string
	for rows.Next() {
		var name string
		require.NoError(t, rows.Scan(&name))
		tables = append(tables, name)
	}
	require.NoError(t, rows.Err())

	expected := []string{
		"announcements",
		"feed_meta",
		"paired_devices",
		"pricing_cache",
		"pricing_sync_meta",
		"provider_status",
		"quota_state",
		"requests",
		"schema_migrations",
		"settings_kv",
	}
	for _, want := range expected {
		assert.Contains(t, tables, want, "table %q should exist", want)
	}
}

func TestClose(t *testing.T) {
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Close())
}

func TestNewULID(t *testing.T) {
	s := openTestStore(t)
	id1 := s.NewULID()
	id2 := s.NewULID()
	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
	assert.Len(t, id1, 26, "ULID must be 26 characters")
}
