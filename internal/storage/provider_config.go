package storage

import (
	"context"
	"database/sql"
	"errors"
)

// ProviderConfig holds metadata for a single provider as stored in provider_config.
type ProviderConfig struct {
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Protocol    string `json:"protocol"`
	BaseURL     string `json:"base_url"`
	PathPrefix  string `json:"path_prefix"`
	IsBuiltin   bool   `json:"is_builtin"`
	SortOrder   int    `json:"sort_order"`
}

// GetProviderConfigs returns all provider configs ordered by sort_order, name.
func (s *Store) GetProviderConfigs(ctx context.Context) ([]ProviderConfig, error) {
	const q = `SELECT name, display_name, protocol, base_url, path_prefix, is_builtin, sort_order
	           FROM provider_config ORDER BY sort_order, name`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderConfig
	for rows.Next() {
		var c ProviderConfig
		var isBuiltin int
		if err := rows.Scan(&c.Name, &c.DisplayName, &c.Protocol, &c.BaseURL, &c.PathPrefix, &isBuiltin, &c.SortOrder); err != nil {
			return nil, err
		}
		c.IsBuiltin = isBuiltin != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// GetProviderConfig returns the config for a single provider. Returns nil if not found.
func (s *Store) GetProviderConfig(ctx context.Context, name string) (*ProviderConfig, error) {
	const q = `SELECT name, display_name, protocol, base_url, path_prefix, is_builtin, sort_order
	           FROM provider_config WHERE name = ?`
	var c ProviderConfig
	var isBuiltin int
	err := s.db.QueryRowContext(ctx, q, name).Scan(&c.Name, &c.DisplayName, &c.Protocol, &c.BaseURL, &c.PathPrefix, &isBuiltin, &c.SortOrder)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c.IsBuiltin = isBuiltin != 0
	return &c, nil
}

// SaveProviderConfig upserts a custom (non-builtin) provider config.
// Built-in providers are seeded via migration and cannot be replaced by this method.
func (s *Store) SaveProviderConfig(ctx context.Context, cfg ProviderConfig) error {
	const q = `INSERT INTO provider_config (name, display_name, protocol, base_url, path_prefix, is_builtin, sort_order)
	           VALUES (?, ?, ?, ?, ?, ?, ?)
	           ON CONFLICT(name) DO UPDATE SET
	               display_name = excluded.display_name,
	               protocol     = excluded.protocol,
	               base_url     = excluded.base_url,
	               path_prefix  = excluded.path_prefix,
	               sort_order   = excluded.sort_order`
	isBuiltin := 0
	if cfg.IsBuiltin {
		isBuiltin = 1
	}
	_, err := s.db.ExecContext(ctx, q, cfg.Name, cfg.DisplayName, cfg.Protocol, cfg.BaseURL, cfg.PathPrefix, isBuiltin, cfg.SortOrder)
	return err
}

// DeleteProviderConfig removes a custom (non-builtin) provider by name.
// Returns an error if the provider is built-in or does not exist.
func (s *Store) DeleteProviderConfig(ctx context.Context, name string) error {
	const q = `DELETE FROM provider_config WHERE name = ? AND is_builtin = 0`
	res, err := s.db.ExecContext(ctx, q, name)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return errors.New("provider not found or is a built-in provider")
	}
	return nil
}
