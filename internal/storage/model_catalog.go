package storage

import (
	"context"
	"time"
)

// ModelCatalogEntry is one row in the model_catalog table.
type ModelCatalogEntry struct {
	LiteLLMProvider     string
	ModelID             string
	RawKey              string
	InputCostPerToken   float64
	OutputCostPerToken  float64
	MaxTokens           int
	UpdatedAt           time.Time
}

// UpsertModelCatalogBatch replaces all catalog entries for a given provider
// atomically (DELETE + batch INSERT inside one transaction).
func (s *Store) UpsertModelCatalogBatch(ctx context.Context, entries []ModelCatalogEntry) error {
	if len(entries) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	// Group by provider and delete existing rows for each.
	seen := make(map[string]bool)
	for _, e := range entries {
		if !seen[e.LiteLLMProvider] {
			seen[e.LiteLLMProvider] = true
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM model_catalog WHERE litellm_provider = ?`, e.LiteLLMProvider,
			); err != nil {
				return err
			}
		}
	}

	const q = `INSERT INTO model_catalog
		(litellm_provider, model_id, raw_key, input_cost_per_token, output_cost_per_token, max_tokens, updated_at)
		VALUES (?,?,?,?,?,?,?)`
	now := time.Now().UTC().Format(time.RFC3339)
	for _, e := range entries {
		if _, err := tx.ExecContext(ctx, q,
			e.LiteLLMProvider, e.ModelID, e.RawKey,
			e.InputCostPerToken, e.OutputCostPerToken, e.MaxTokens,
			now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetModelsByLiteLLMProvider returns all model IDs for a given litellm_provider.
func (s *Store) GetModelsByLiteLLMProvider(ctx context.Context, provider string) ([]string, error) {
	const q = `SELECT model_id FROM model_catalog WHERE litellm_provider = ? ORDER BY model_id`
	rows, err := s.db.QueryContext(ctx, q, provider)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// GetAllModelCatalog returns all catalog entries grouped by litellm_provider.
func (s *Store) GetAllModelCatalog(ctx context.Context) (map[string][]ModelCatalogEntry, error) {
	const q = `SELECT litellm_provider, model_id, raw_key,
		input_cost_per_token, output_cost_per_token, max_tokens, updated_at
		FROM model_catalog ORDER BY litellm_provider, model_id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make(map[string][]ModelCatalogEntry)
	for rows.Next() {
		var e ModelCatalogEntry
		var updStr string
		if err := rows.Scan(
			&e.LiteLLMProvider, &e.ModelID, &e.RawKey,
			&e.InputCostPerToken, &e.OutputCostPerToken, &e.MaxTokens,
			&updStr,
		); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, updStr); err == nil {
			e.UpdatedAt = t
		}
		out[e.LiteLLMProvider] = append(out[e.LiteLLMProvider], e)
	}
	return out, rows.Err()
}
