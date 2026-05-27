package storage

import (
	"context"
	"database/sql"
	"errors"
)

// AppSetting captures the user's choice for one AI app: whether it is
// enabled for inheritance and which config path to scan. See spec/04.
type AppSetting struct {
	AppID         string `json:"app_id"`
	Enabled       bool   `json:"enabled"`
	ConfigPath    string `json:"config_path"`
	LastScannedAt *int64 `json:"last_scanned_at,omitempty"` // ms UTC, nil when never scanned
	LastError     string `json:"last_error,omitempty"`
	Preset        string `json:"preset,omitempty"` // "" = use type-based default
}

// GetAppSetting returns the row for appID or (nil, nil) when no row exists.
// A missing row is normal: the user has not enabled / customised this app.
func (s *Store) GetAppSetting(ctx context.Context, appID string) (*AppSetting, error) {
	const q = `SELECT app_id, enabled, config_path, last_scanned_at, last_error, preset
	           FROM app_settings WHERE app_id = ?`
	var (
		a            AppSetting
		enabled      int
		lastScanned  sql.NullInt64
		lastErrorStr sql.NullString
		preset       sql.NullString
	)
	err := s.db.QueryRowContext(ctx, q, appID).Scan(
		&a.AppID, &enabled, &a.ConfigPath, &lastScanned, &lastErrorStr, &preset,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	a.Enabled = enabled != 0
	if lastScanned.Valid {
		v := lastScanned.Int64
		a.LastScannedAt = &v
	}
	if lastErrorStr.Valid {
		a.LastError = lastErrorStr.String
	}
	if preset.Valid {
		a.Preset = preset.String
	}
	return &a, nil
}

// ListAppSettings returns every row in app_settings, ordered by app_id.
func (s *Store) ListAppSettings(ctx context.Context) ([]AppSetting, error) {
	const q = `SELECT app_id, enabled, config_path, last_scanned_at, last_error, preset
	           FROM app_settings ORDER BY app_id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AppSetting
	for rows.Next() {
		var (
			a            AppSetting
			enabled      int
			lastScanned  sql.NullInt64
			lastErrorStr sql.NullString
			preset       sql.NullString
		)
		if err := rows.Scan(&a.AppID, &enabled, &a.ConfigPath, &lastScanned, &lastErrorStr, &preset); err != nil {
			return nil, err
		}
		a.Enabled = enabled != 0
		if lastScanned.Valid {
			v := lastScanned.Int64
			a.LastScannedAt = &v
		}
		if lastErrorStr.Valid {
			a.LastError = lastErrorStr.String
		}
		if preset.Valid {
			a.Preset = preset.String
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertAppSetting inserts or updates a row. The full row is written; pass a
// nil LastScannedAt to leave that field untouched relative to the existing row
// (handled by the SQL via COALESCE so callers can omit timestamps when only
// flipping enabled/config_path).
func (s *Store) UpsertAppSetting(ctx context.Context, a AppSetting) error {
	const q = `INSERT INTO app_settings (app_id, enabled, config_path, last_scanned_at, last_error, preset)
	           VALUES (?, ?, ?, ?, ?, ?)
	           ON CONFLICT(app_id) DO UPDATE SET
	             enabled         = excluded.enabled,
	             config_path     = excluded.config_path,
	             last_scanned_at = COALESCE(excluded.last_scanned_at, app_settings.last_scanned_at),
	             last_error      = excluded.last_error,
	             preset          = excluded.preset`
	enabled := 0
	if a.Enabled {
		enabled = 1
	}
	var ls any
	if a.LastScannedAt != nil {
		ls = *a.LastScannedAt
	}
	var le any
	if a.LastError != "" {
		le = a.LastError
	}
	_, err := s.db.ExecContext(ctx, q, a.AppID, enabled, a.ConfigPath, ls, le, a.Preset)
	return err
}

// SetAppEnabled toggles app_settings.enabled and leaves other fields
// alone. Returns sql.ErrNoRows if the app isn't in the table yet (callers
// should UpsertAppSetting first).
func (s *Store) SetAppEnabled(ctx context.Context, appID string, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE app_settings SET enabled = ? WHERE app_id = ?`, e, appID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RecordAppScan updates the last_scanned_at and last_error fields after a
// rescan attempt. Pass empty errorMsg on success.
func (s *Store) RecordAppScan(ctx context.Context, appID string, scannedAt int64, errorMsg string) error {
	var le any
	if errorMsg != "" {
		le = errorMsg
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE app_settings SET last_scanned_at = ?, last_error = ? WHERE app_id = ?`,
		scannedAt, le, appID)
	return err
}

// DeleteAppSetting removes the row for appID and any of its
// inherited_endpoints rows. We do the second DELETE explicitly because
// PRAGMA foreign_keys is not enabled in storage.Open, so ON DELETE CASCADE
// declared in the schema does not fire. No-op when the app is unknown.
func (s *Store) DeleteAppSetting(ctx context.Context, appID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM inherited_endpoints WHERE app_id = ?`, appID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM app_settings WHERE app_id = ?`, appID); err != nil {
		return err
	}
	return tx.Commit()
}
