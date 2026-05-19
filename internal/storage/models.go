package storage

import (
	"context"
	"database/sql"
	"time"
)

// DiscoveredModel is a single model entry from the model discovery cache.
type DiscoveredModel struct {
	Provider    string
	ModelID     string
	DisplayName string
	FetchedAt   time.Time
}

// SaveDiscoveredModels atomically replaces the discovery cache for a provider.
// All previous entries for that provider are deleted, then the new list is inserted.
// An empty models slice clears the cache for that provider.
func (s *Store) SaveDiscoveredModels(ctx context.Context, provider string, models []DiscoveredModel) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM model_discovery WHERE provider = ?`, provider); err != nil {
		return err
	}

	now := time.Now().Unix()
	for _, m := range models {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO model_discovery (provider, model_id, display_name, fetched_at) VALUES (?, ?, ?, ?)`,
			provider, m.ModelID, m.DisplayName, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetDiscoveredModels returns cached models for a provider and the fetch timestamp.
// Returns an empty slice and zero Time if no data exists for the provider.
func (s *Store) GetDiscoveredModels(ctx context.Context, provider string) ([]DiscoveredModel, time.Time, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT model_id, display_name, fetched_at FROM model_discovery WHERE provider = ? ORDER BY model_id`,
		provider,
	)
	if err != nil {
		return nil, time.Time{}, err
	}
	defer func() { _ = rows.Close() }()

	var out []DiscoveredModel
	var latestFetch int64
	for rows.Next() {
		var m DiscoveredModel
		var fetchedAt int64
		if err := rows.Scan(&m.ModelID, &m.DisplayName, &fetchedAt); err != nil {
			return nil, time.Time{}, err
		}
		m.Provider = provider
		m.FetchedAt = time.Unix(fetchedAt, 0)
		if fetchedAt > latestFetch {
			latestFetch = fetchedAt
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, time.Time{}, err
	}
	if latestFetch == 0 {
		return out, time.Time{}, nil
	}
	return out, time.Unix(latestFetch, 0), nil
}

// GetAllDiscoveredModels returns all cached models grouped by provider.
func (s *Store) GetAllDiscoveredModels(ctx context.Context) (map[string][]DiscoveredModel, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT provider, model_id, display_name, fetched_at FROM model_discovery ORDER BY provider, model_id`,
	)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make(map[string][]DiscoveredModel)
	for rows.Next() {
		var m DiscoveredModel
		var fetchedAt int64
		if err := rows.Scan(&m.Provider, &m.ModelID, &m.DisplayName, &fetchedAt); err != nil {
			return nil, err
		}
		m.FetchedAt = time.Unix(fetchedAt, 0)
		out[m.Provider] = append(out[m.Provider], m)
	}
	return out, rows.Err()
}

// OldestModelDiscoveryAge returns the oldest fetched_at across all providers,
// or (0, nil) if the table is empty. Used to decide whether a background refresh is needed.
func (s *Store) OldestModelDiscoveryAge(ctx context.Context) (time.Time, error) {
	var ts sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MIN(fetched_at) FROM model_discovery`).Scan(&ts)
	if err != nil || !ts.Valid {
		return time.Time{}, err
	}
	return time.Unix(ts.Int64, 0), nil
}
