package storage

import (
	"context"
	"database/sql"
)

// InheritedEndpoint is one vendor endpoint extracted from an AI app's
// config, persisted in the inherited_endpoints table. The app_id column is a
// foreign key into app_settings; cascade-deletes propagate from there.
//
// This struct is the storage-layer counterpart of
// agentscan.InheritedEndpoint. We keep them as separate types to avoid an
// import cycle (storage cannot depend on agentscan and vice versa).
type InheritedEndpoint struct {
	AppID        string `json:"app_id"`
	Provider     string `json:"provider"`
	EndpointURL  string `json:"endpoint_url"`
	ProtocolHint string `json:"protocol_hint,omitempty"`
	APIKey       string `json:"api_key,omitempty"`
	ExtrasJSON   string `json:"extras_json,omitempty"`
	CapturedAt   int64  `json:"captured_at"`
}

// ReplaceInheritedEndpoints atomically swaps all inherited_endpoints rows for
// appID with the supplied set. Empty endpoints slice removes all rows for
// appID (e.g. when a Scan returns no providers). Atomicity matters because
// the routing engine reads this table concurrently; partial writes could send
// requests to a half-replaced state.
func (s *Store) ReplaceInheritedEndpoints(ctx context.Context, appID string, endpoints []InheritedEndpoint) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // safe no-op after Commit

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM inherited_endpoints WHERE app_id = ?`, appID); err != nil {
		return err
	}

	const ins = `INSERT INTO inherited_endpoints
	             (app_id, provider, endpoint_url, protocol_hint, api_key, extras_json, captured_at)
	             VALUES (?, ?, ?, ?, ?, ?, ?)`
	stmt, err := tx.PrepareContext(ctx, ins)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, ep := range endpoints {
		if ep.Provider == "" {
			continue
		}
		var protocol any
		if ep.ProtocolHint != "" {
			protocol = ep.ProtocolHint
		}
		var apiKey any
		if ep.APIKey != "" {
			apiKey = ep.APIKey
		}
		var extras any
		if ep.ExtrasJSON != "" {
			extras = ep.ExtrasJSON
		}
		if _, err := stmt.ExecContext(ctx,
			appID, ep.Provider, ep.EndpointURL, protocol, apiKey, extras, ep.CapturedAt,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// ListInheritedEndpoints returns every row across all apps, ordered by
// (app_id, provider). Useful for the dashboard / SSE event payload.
func (s *Store) ListInheritedEndpoints(ctx context.Context) ([]InheritedEndpoint, error) {
	const q = `SELECT app_id, provider, endpoint_url, protocol_hint, api_key, extras_json, captured_at
	           FROM inherited_endpoints ORDER BY app_id, provider`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInheritedEndpoints(rows)
}

// ListInheritedEndpointsByApp returns inherited_endpoints scoped to one app.
func (s *Store) ListInheritedEndpointsByApp(ctx context.Context, appID string) ([]InheritedEndpoint, error) {
	const q = `SELECT app_id, provider, endpoint_url, protocol_hint, api_key, extras_json, captured_at
	           FROM inherited_endpoints WHERE app_id = ? ORDER BY provider`
	rows, err := s.db.QueryContext(ctx, q, appID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanInheritedEndpoints(rows)
}

// FindInheritedEndpointsByProvider returns endpoints for a given provider name
// across any enabled app. Routing engine uses this to discover candidate
// upstream URLs for routing decisions.
//
// Matching is on the canonical provider name (see canonicalProviderName), so a
// lookup by krouter's adapter name ("qwen") also resolves rows the agent stored
// under a vendor alias ("dashscope"). The DB keeps the original name as scanned;
// only the comparison is alias-aware.
func (s *Store) FindInheritedEndpointsByProvider(ctx context.Context, provider string) ([]InheritedEndpoint, error) {
	const q = `SELECT i.app_id, i.provider, i.endpoint_url, i.protocol_hint,
	                  i.api_key, i.extras_json, i.captured_at
	           FROM inherited_endpoints AS i
	           JOIN app_settings AS a ON a.app_id = i.app_id
	           WHERE a.enabled = 1
	           ORDER BY i.app_id`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	all, err := scanInheritedEndpoints(rows)
	if err != nil {
		return nil, err
	}
	target := canonicalProviderName(provider)
	out := make([]InheritedEndpoint, 0, len(all))
	for _, ep := range all {
		if canonicalProviderName(ep.Provider) == target {
			out = append(out, ep)
		}
	}
	return out, nil
}

func scanInheritedEndpoints(rows *sql.Rows) ([]InheritedEndpoint, error) {
	var out []InheritedEndpoint
	for rows.Next() {
		var (
			ep       InheritedEndpoint
			protocol sql.NullString
			apiKey   sql.NullString
			extras   sql.NullString
		)
		if err := rows.Scan(
			&ep.AppID, &ep.Provider, &ep.EndpointURL,
			&protocol, &apiKey, &extras, &ep.CapturedAt,
		); err != nil {
			return nil, err
		}
		if protocol.Valid {
			ep.ProtocolHint = protocol.String
		}
		if apiKey.Valid {
			ep.APIKey = apiKey.String
		}
		if extras.Valid {
			ep.ExtrasJSON = extras.String
		}
		out = append(out, ep)
	}
	return out, rows.Err()
}
