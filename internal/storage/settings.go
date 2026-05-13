package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// GetSetting returns the value stored for key in settings_kv.
// Returns ("", false, nil) when the key does not exist.
func (s *Store) GetSetting(ctx context.Context, key string) (string, bool, error) {
	const q = `SELECT value FROM settings_kv WHERE key = ?`
	var value string
	err := s.db.QueryRowContext(ctx, q, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return value, true, nil
}

// SetSetting upserts a key-value pair in settings_kv.
func (s *Store) SetSetting(ctx context.Context, key, value string) error {
	const q = `INSERT INTO settings_kv (key, value, updated_at)
	           VALUES (?, ?, ?)
	           ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`
	_, err := s.db.ExecContext(ctx, q, key, value, time.Now().UTC().Format(time.RFC3339))
	return err
}

// CountRequestsToday returns the number of requests logged since today 00:00:00 UTC.
func (s *Store) CountRequestsToday(ctx context.Context) (int, error) {
	today := time.Now().UTC().Format("2006-01-02") + "T00:00:00Z"
	const q = `SELECT COUNT(*) FROM requests WHERE ts_utc >= ?`
	var count int
	err := s.db.QueryRowContext(ctx, q, today).Scan(&count)
	return count, err
}
