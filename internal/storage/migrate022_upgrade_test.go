package storage_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMigration022_UpgradePathScrubsCredentials simulates upgrading a real
// pre-022 database: rows in inherited_endpoints carry an api_key value and
// an extras_json with oauth_token. After Migrate, the column is gone and the
// token is scrubbed, while non-credential extras fields survive.
func TestMigration022_UpgradePathScrubsCredentials(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "old.db")

	// Build the pre-022 state with raw SQL on a fresh connection: replay the
	// 009/018 schema shape and pretend migrations 001–021 already ran by
	// seeding schema_migrations accordingly is brittle; instead, open a fully
	// migrated store, then recreate the legacy table WITH api_key and data,
	// roll schema_migrations back past 022, and migrate again.
	s, err := storage.Open(dbPath)
	require.NoError(t, err)
	require.NoError(t, s.Migrate())

	_, err = s.DB().Exec(`
		DELETE FROM schema_migrations WHERE version >= '022';
		DROP TABLE inherited_endpoints;
		CREATE TABLE inherited_endpoints (
			app_id        TEXT NOT NULL,
			provider      TEXT NOT NULL,
			endpoint_url  TEXT NOT NULL,
			protocol_hint TEXT,
			api_key       TEXT,
			extras_json   TEXT,
			captured_at   INTEGER NOT NULL,
			PRIMARY KEY (app_id, provider)
		);
		INSERT INTO app_settings (app_id, enabled, config_path) VALUES ('openclaw', 1, '/x')
			ON CONFLICT(app_id) DO NOTHING;
		INSERT INTO inherited_endpoints
			(app_id, provider, endpoint_url, protocol_hint, api_key, extras_json, captured_at)
		VALUES
			('openclaw', 'deepseek', 'u1', 'openai-chat', 'sk-OLD-SECRET', NULL, 1),
			('openclaw', 'minimax-portal', 'u2', NULL, NULL,
			 '{"oauth_token":"sk-cp-OLD","purpose":"subscription_oauth"}', 1);
	`)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	// Reopen: Open+Migrate replays 022 against the legacy-shaped data.
	s2, err := storage.Open(dbPath)
	require.NoError(t, err)
	defer func() { _ = s2.Close() }()
	require.NoError(t, s2.Migrate())

	eps, err := s2.ListInheritedEndpoints(context.Background())
	require.NoError(t, err)
	require.Len(t, eps, 2)
	for _, ep := range eps {
		assert.NotContains(t, ep.ExtrasJSON, "sk-cp-OLD", "oauth_token must be scrubbed on upgrade")
	}

	// The raw file must not contain either secret anywhere (column dropped,
	// extras scrubbed, and VACUUM not required because DROP COLUMN rewrites
	// the row data).
	raw, err := os.ReadFile(dbPath)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "sk-OLD-SECRET")
	assert.NotContains(t, string(raw), "sk-cp-OLD")
}
