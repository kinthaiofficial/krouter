package storage

import (
	"context"
	"time"
)

// PriceCacheEntry mirrors the pricing_cache table row.
type PriceCacheEntry struct {
	ModelID                  string
	Provider                 string
	InputCostPerToken        float64
	OutputCostPerToken       float64
	CachedInputCostPerToken  float64
	MaxTokens                int
	RawJSON                  string
	UpdatedAt                time.Time
}

// UpsertPrice inserts or replaces a single entry in pricing_cache.
func (s *Store) UpsertPrice(ctx context.Context, e PriceCacheEntry) error {
	const q = `INSERT INTO pricing_cache
		(model_id, provider, input_cost_per_token, output_cost_per_token,
		 cached_input_cost_per_token, max_tokens, raw_json, updated_at)
		VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(model_id) DO UPDATE SET
		  provider=excluded.provider,
		  input_cost_per_token=excluded.input_cost_per_token,
		  output_cost_per_token=excluded.output_cost_per_token,
		  cached_input_cost_per_token=excluded.cached_input_cost_per_token,
		  max_tokens=excluded.max_tokens,
		  raw_json=excluded.raw_json,
		  updated_at=excluded.updated_at`
	_, err := s.db.ExecContext(ctx, q,
		e.ModelID, e.Provider,
		e.InputCostPerToken, e.OutputCostPerToken, e.CachedInputCostPerToken,
		e.MaxTokens, e.RawJSON,
		e.UpdatedAt.UTC().Format(time.RFC3339),
	)
	return err
}

// GetAllPrices returns all entries from pricing_cache.
func (s *Store) GetAllPrices(ctx context.Context) ([]PriceCacheEntry, error) {
	const q = `SELECT model_id, provider,
		COALESCE(input_cost_per_token,0), COALESCE(output_cost_per_token,0),
		COALESCE(cached_input_cost_per_token,0), COALESCE(max_tokens,0),
		COALESCE(raw_json,''), updated_at
		FROM pricing_cache`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []PriceCacheEntry
	for rows.Next() {
		var e PriceCacheEntry
		var updStr string
		if err := rows.Scan(
			&e.ModelID, &e.Provider,
			&e.InputCostPerToken, &e.OutputCostPerToken, &e.CachedInputCostPerToken,
			&e.MaxTokens, &e.RawJSON, &updStr,
		); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, updStr); err == nil {
			e.UpdatedAt = t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetSyncMeta returns a value from pricing_sync_meta. Returns "" when absent.
func (s *Store) GetSyncMeta(ctx context.Context, key string) (string, error) {
	const q = `SELECT COALESCE(value,'') FROM pricing_sync_meta WHERE key = ?`
	var v string
	err := s.db.QueryRowContext(ctx, q, key).Scan(&v)
	if err != nil {
		return "", nil // absent key is not an error
	}
	return v, nil
}

// SetSyncMeta upserts a key in pricing_sync_meta.
func (s *Store) SetSyncMeta(ctx context.Context, key, value string) error {
	const q = `INSERT INTO pricing_sync_meta (key, value) VALUES (?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`
	_, err := s.db.ExecContext(ctx, q, key, value)
	return err
}

// SumCostMicroUSD returns the total cost in micro-USD for requests since sinceUTC.
func (s *Store) SumCostMicroUSD(ctx context.Context, sinceUTC time.Time) (int64, error) {
	const q = `SELECT COALESCE(SUM(cost_micro_usd),0) FROM requests WHERE ts_utc >= ?`
	var total int64
	err := s.db.QueryRowContext(ctx, q, sinceUTC.UTC().Format(time.RFC3339)).Scan(&total)
	return total, err
}
