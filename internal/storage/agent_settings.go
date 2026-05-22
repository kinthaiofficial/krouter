package storage

import (
	"context"
	"database/sql"
	"errors"
)

// AgentSetting captures the user's choice for one AI agent: whether it is
// enabled for inheritance and which config path to scan. See spec/04.
type AgentSetting struct {
	AgentID       string `json:"agent_id"`
	Enabled       bool   `json:"enabled"`
	ConfigPath    string `json:"config_path"`
	LastScannedAt *int64 `json:"last_scanned_at,omitempty"` // ms UTC, nil when never scanned
	LastError     string `json:"last_error,omitempty"`
}

// GetAgentSetting returns the row for agentID or (nil, nil) when no row exists.
// A missing row is normal: the user has not enabled / customised this agent.
func (s *Store) GetAgentSetting(ctx context.Context, agentID string) (*AgentSetting, error) {
	const q = `SELECT agent_id, enabled, config_path, last_scanned_at, last_error
	           FROM agent_settings WHERE agent_id = ?`
	var (
		a            AgentSetting
		enabled      int
		lastScanned  sql.NullInt64
		lastErrorStr sql.NullString
	)
	err := s.db.QueryRowContext(ctx, q, agentID).Scan(
		&a.AgentID, &enabled, &a.ConfigPath, &lastScanned, &lastErrorStr,
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
	return &a, nil
}

// ListAgentSettings returns every row in agent_settings, ordered by agent_id.
func (s *Store) ListAgentSettings(ctx context.Context) ([]AgentSetting, error) {
	const q = `SELECT agent_id, enabled, config_path, last_scanned_at, last_error
	           FROM agent_settings ORDER BY agent_id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AgentSetting
	for rows.Next() {
		var (
			a            AgentSetting
			enabled      int
			lastScanned  sql.NullInt64
			lastErrorStr sql.NullString
		)
		if err := rows.Scan(&a.AgentID, &enabled, &a.ConfigPath, &lastScanned, &lastErrorStr); err != nil {
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
		out = append(out, a)
	}
	return out, rows.Err()
}

// UpsertAgentSetting inserts or updates a row. The full row is written; pass a
// nil LastScannedAt to leave that field untouched relative to the existing row
// (handled by the SQL via COALESCE so callers can omit timestamps when only
// flipping enabled/config_path).
func (s *Store) UpsertAgentSetting(ctx context.Context, a AgentSetting) error {
	const q = `INSERT INTO agent_settings (agent_id, enabled, config_path, last_scanned_at, last_error)
	           VALUES (?, ?, ?, ?, ?)
	           ON CONFLICT(agent_id) DO UPDATE SET
	             enabled         = excluded.enabled,
	             config_path     = excluded.config_path,
	             last_scanned_at = COALESCE(excluded.last_scanned_at, agent_settings.last_scanned_at),
	             last_error      = excluded.last_error`
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
	_, err := s.db.ExecContext(ctx, q, a.AgentID, enabled, a.ConfigPath, ls, le)
	return err
}

// SetAgentEnabled toggles agent_settings.enabled and leaves other fields
// alone. Returns sql.ErrNoRows if the agent isn't in the table yet (callers
// should UpsertAgentSetting first).
func (s *Store) SetAgentEnabled(ctx context.Context, agentID string, enabled bool) error {
	e := 0
	if enabled {
		e = 1
	}
	res, err := s.db.ExecContext(ctx,
		`UPDATE agent_settings SET enabled = ? WHERE agent_id = ?`, e, agentID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RecordAgentScan updates the last_scanned_at and last_error fields after a
// rescan attempt. Pass empty errorMsg on success.
func (s *Store) RecordAgentScan(ctx context.Context, agentID string, scannedAt int64, errorMsg string) error {
	var le any
	if errorMsg != "" {
		le = errorMsg
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE agent_settings SET last_scanned_at = ?, last_error = ? WHERE agent_id = ?`,
		scannedAt, le, agentID)
	return err
}

// DeleteAgentSetting removes the row for agentID and any of its
// inherited_endpoints rows. We do the second DELETE explicitly because
// PRAGMA foreign_keys is not enabled in storage.Open, so ON DELETE CASCADE
// declared in the schema does not fire. No-op when the agent is unknown.
func (s *Store) DeleteAgentSetting(ctx context.Context, agentID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM inherited_endpoints WHERE agent_id = ?`, agentID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM agent_settings WHERE agent_id = ?`, agentID); err != nil {
		return err
	}
	return tx.Commit()
}
