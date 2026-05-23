package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// FreeProvider mirrors one row of the free_provider_state table — the
// curated catalog of providers offering free credits / quotas / tiers.
// Lifecycle: seeded by the installer at first launch from
// data/free_tokens.json (via the `data` package), refreshed periodically
// by internal/freeproviders from krouter.kinthai.ai.
type FreeProvider struct {
	ID                  string
	DisplayName         string
	KrouterProviderName string
	Protocol            string
	Region              string  // "china" | "intl"
	FreeType            string  // "trial_credit" | "daily_quota" | "free_tier"
	FreeSummary         string
	FreeQuotaUSD        float64 // best-effort; 999.0 means "effectively unlimited"
	Validity            string  // human-readable: "7 days" | "no_expiry" | "30 days"
	Conditions          string
	SignupURL           string
	KeySetupHint        string
	Active              bool
	LastVerified        string  // ISO date
	Notes               string
	UpdatedAt           time.Time
}

// UpsertFreeProvider inserts or replaces a row in free_provider_state.
// Called by the sync loop and by the installer's seed pass.
func (s *Store) UpsertFreeProvider(ctx context.Context, p FreeProvider) error {
	const q = `INSERT INTO free_provider_state
		(id, display_name, krouter_provider_name, protocol, region, free_type,
		 free_summary, free_quota_usd, validity, conditions, signup_url,
		 key_setup_hint, active, last_verified, notes, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			display_name           = excluded.display_name,
			krouter_provider_name  = excluded.krouter_provider_name,
			protocol               = excluded.protocol,
			region                 = excluded.region,
			free_type              = excluded.free_type,
			free_summary           = excluded.free_summary,
			free_quota_usd         = excluded.free_quota_usd,
			validity               = excluded.validity,
			conditions             = excluded.conditions,
			signup_url             = excluded.signup_url,
			key_setup_hint         = excluded.key_setup_hint,
			active                 = excluded.active,
			last_verified          = excluded.last_verified,
			notes                  = excluded.notes,
			updated_at             = excluded.updated_at`
	active := 0
	if p.Active {
		active = 1
	}
	_, err := s.db.ExecContext(ctx, q,
		p.ID, p.DisplayName, p.KrouterProviderName, p.Protocol, p.Region, p.FreeType,
		p.FreeSummary, p.FreeQuotaUSD, p.Validity, p.Conditions, p.SignupURL,
		p.KeySetupHint, active, p.LastVerified, p.Notes, p.UpdatedAt.UnixMilli(),
	)
	return err
}

// ListFreeProviders returns every row in free_provider_state, optionally
// filtered to active rows. Sorted by FreeQuotaUSD descending so the UI
// can show the "most credit" providers first by default.
func (s *Store) ListFreeProviders(ctx context.Context, activeOnly bool) ([]FreeProvider, error) {
	q := `SELECT id, display_name, krouter_provider_name, protocol, region, free_type,
		         free_summary, free_quota_usd, validity, conditions, signup_url,
		         key_setup_hint, active, last_verified, notes, updated_at
		  FROM free_provider_state`
	if activeOnly {
		q += ` WHERE active = 1`
	}
	q += ` ORDER BY free_quota_usd DESC, display_name ASC`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []FreeProvider
	for rows.Next() {
		var p FreeProvider
		var active int
		var updatedAtMS int64
		if err := rows.Scan(
			&p.ID, &p.DisplayName, &p.KrouterProviderName, &p.Protocol, &p.Region, &p.FreeType,
			&p.FreeSummary, &p.FreeQuotaUSD, &p.Validity, &p.Conditions, &p.SignupURL,
			&p.KeySetupHint, &active, &p.LastVerified, &p.Notes, &updatedAtMS,
		); err != nil {
			return nil, err
		}
		p.Active = active != 0
		p.UpdatedAt = time.UnixMilli(updatedAtMS).UTC()
		out = append(out, p)
	}
	return out, rows.Err()
}

// FreeProviderKrouterNames returns the set of `krouter_provider_name`s
// across all active rows. Used by routing to decide whether an inherited
// provider qualifies as a free-credit candidate (cheap O(N) lookup —
// fewer than 30 rows expected).
func (s *Store) FreeProviderKrouterNames(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT krouter_provider_name FROM free_provider_state WHERE active = 1`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := map[string]struct{}{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		if name != "" {
			out[name] = struct{}{}
		}
	}
	return out, rows.Err()
}

// ── provider_exhausted_until ───────────────────────────────────────────────

// MarkProviderExhausted records that an upstream returned an auth-or-quota
// failure for `provider`. The provider will be treated as "exhausted" by
// the routing engine until `until` passes; at that point a subsequent
// IsProviderExhausted call sees the timestamp lapsed and returns false,
// effectively re-enabling the provider.
//
// statusCode + reason are persisted for debugging / dashboard display
// (e.g. "exhausted at 14:23 — HTTP 402 quota_exhausted").
func (s *Store) MarkProviderExhausted(ctx context.Context, provider string, until time.Time, statusCode int, reason string) error {
	const q = `INSERT INTO provider_exhausted_until
		(provider, exhausted_until, last_reason, last_status_code, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(provider) DO UPDATE SET
			exhausted_until  = excluded.exhausted_until,
			last_reason      = excluded.last_reason,
			last_status_code = excluded.last_status_code,
			updated_at       = excluded.updated_at`
	now := time.Now().UTC().UnixMilli()
	_, err := s.db.ExecContext(ctx, q, provider, until.UTC().UnixMilli(), reason, statusCode, now)
	return err
}

// IsProviderExhausted returns true if the provider has a non-expired
// exhaustion mark in the DB. Returns false (no error) when no row exists
// or when the recorded `exhausted_until` is in the past.
func (s *Store) IsProviderExhausted(ctx context.Context, provider string) bool {
	const q = `SELECT exhausted_until FROM provider_exhausted_until WHERE provider = ?`
	var until int64
	err := s.db.QueryRowContext(ctx, q, provider).Scan(&until)
	if errors.Is(err, sql.ErrNoRows) {
		return false
	}
	if err != nil {
		return false
	}
	return time.Now().UTC().UnixMilli() < until
}

// ClearProviderExhausted removes the exhaustion row for `provider`. Used
// by tests and by /internal/free-providers/{name}/clear-exhaustion (a
// manual override endpoint for support cases).
func (s *Store) ClearProviderExhausted(ctx context.Context, provider string) error {
	_, err := s.db.ExecContext(ctx,
		`DELETE FROM provider_exhausted_until WHERE provider = ?`, provider)
	return err
}
