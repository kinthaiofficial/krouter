package storage

import (
	"context"
	"database/sql"
	"encoding/json"
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

	// I18n holds per-language overrides of the human-readable fields,
	// shaped lang → field → value, e.g.
	//   {"zh": {"free_summary": "...", "conditions": "...", "notes": "..."}}.
	// The struct's own string fields above are the DEFAULT (English) copy;
	// the dashboard overlays the user's language and falls back to English
	// when a field/lang is missing. Persisted as the i18n_json column.
	I18n map[string]map[string]string

	// AdditionalProtocols carries dual-/multi-protocol vendors. When this
	// catalog entry's primary `Protocol` is e.g. "openai" but the vendor
	// also exposes an Anthropic-compatible endpoint (OpenRouter, GLM,
	// Moonshot, …), the entry lists the alternate protocol(s) here so the
	// dashboard's Free page can show the vendor for both protocols. (The
	// routing free-first path that once consumed this was removed — D-037;
	// this catalog is discovery/signup UI only.) spec/00 §B2 (same-protocol
	// routing) is preserved: each entry below has its own `Protocol` and is
	// matched independently against the request's protocol.
	//
	// Empty slice means the provider speaks only the primary protocol.
	AdditionalProtocols []FreeProviderProtocol

	UpdatedAt time.Time
}

// FreeProviderProtocol describes one alternate-protocol endpoint a free
// provider exposes. Each entry has its own krouter_provider_name because
// the user must configure that as a separate provider entry inside their
// AI agent (e.g. OpenClaw needs both `openrouter` and `openrouter-anthropic`
// rows, same API key, different baseURL) so inheritance can pick both up.
type FreeProviderProtocol struct {
	Protocol            string `json:"protocol"`
	KrouterProviderName string `json:"krouter_provider_name"`
	KeySetupHint        string `json:"key_setup_hint"`
	// I18n overrides KeySetupHint (the only human-readable field here) per
	// language: {"zh": {"key_setup_hint": "..."}}. Rides inside the parent's
	// additional_protocols_json blob, so it needs no dedicated column.
	I18n map[string]map[string]string `json:"i18n,omitempty"`
}

// UpsertFreeProvider inserts or replaces a row in free_provider_state.
// Called by the sync loop and by the installer's seed pass.
func (s *Store) UpsertFreeProvider(ctx context.Context, p FreeProvider) error {
	const q = `INSERT INTO free_provider_state
		(id, display_name, krouter_provider_name, protocol, region, free_type,
		 free_summary, free_quota_usd, validity, conditions, signup_url,
		 key_setup_hint, active, last_verified, notes,
		 additional_protocols_json, i18n_json, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			display_name              = excluded.display_name,
			krouter_provider_name     = excluded.krouter_provider_name,
			protocol                  = excluded.protocol,
			region                    = excluded.region,
			free_type                 = excluded.free_type,
			free_summary              = excluded.free_summary,
			free_quota_usd            = excluded.free_quota_usd,
			validity                  = excluded.validity,
			conditions                = excluded.conditions,
			signup_url                = excluded.signup_url,
			key_setup_hint            = excluded.key_setup_hint,
			active                    = excluded.active,
			last_verified             = excluded.last_verified,
			notes                     = excluded.notes,
			additional_protocols_json = excluded.additional_protocols_json,
			i18n_json                 = excluded.i18n_json,
			updated_at                = excluded.updated_at`
	active := 0
	if p.Active {
		active = 1
	}
	// Serialise i18n overrides; nil/empty round-trips to "{}" so the column
	// is never NULL and the loaded map is always usable.
	i18nBytes := []byte("{}")
	if len(p.I18n) > 0 {
		var err error
		i18nBytes, err = json.Marshal(p.I18n)
		if err != nil {
			return err
		}
	}
	// Serialise the additional protocols once; an empty slice round-trips
	// to "[]" so the DB column is never NULL and the loaded slice is
	// always non-nil (json.Marshal of nil-slice still emits "null", so
	// we coerce to empty slice).
	addBytes := []byte("[]")
	if len(p.AdditionalProtocols) > 0 {
		var err error
		addBytes, err = json.Marshal(p.AdditionalProtocols)
		if err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, q,
		p.ID, p.DisplayName, p.KrouterProviderName, p.Protocol, p.Region, p.FreeType,
		p.FreeSummary, p.FreeQuotaUSD, p.Validity, p.Conditions, p.SignupURL,
		p.KeySetupHint, active, p.LastVerified, p.Notes,
		string(addBytes), string(i18nBytes), p.UpdatedAt.UnixMilli(),
	)
	return err
}

// ListFreeProviders returns every row in free_provider_state, optionally
// filtered to active rows. Sorted by FreeQuotaUSD descending so the UI
// can show the "most credit" providers first by default.
func (s *Store) ListFreeProviders(ctx context.Context, activeOnly bool) ([]FreeProvider, error) {
	q := `SELECT id, display_name, krouter_provider_name, protocol, region, free_type,
		         free_summary, free_quota_usd, validity, conditions, signup_url,
		         key_setup_hint, active, last_verified, notes,
		         additional_protocols_json, i18n_json, updated_at
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
		var addJSON string
		var i18nJSON string
		if err := rows.Scan(
			&p.ID, &p.DisplayName, &p.KrouterProviderName, &p.Protocol, &p.Region, &p.FreeType,
			&p.FreeSummary, &p.FreeQuotaUSD, &p.Validity, &p.Conditions, &p.SignupURL,
			&p.KeySetupHint, &active, &p.LastVerified, &p.Notes,
			&addJSON, &i18nJSON, &updatedAtMS,
		); err != nil {
			return nil, err
		}
		p.Active = active != 0
		p.UpdatedAt = time.UnixMilli(updatedAtMS).UTC()
		// Decode additional_protocols. A malformed JSON column should not
		// kill the whole list; we leave the slice empty and continue.
		if addJSON != "" && addJSON != "[]" {
			_ = json.Unmarshal([]byte(addJSON), &p.AdditionalProtocols)
		}
		// Decode i18n overrides (same lenient policy — bad JSON just means
		// the row renders in its default English).
		if i18nJSON != "" && i18nJSON != "{}" {
			_ = json.Unmarshal([]byte(i18nJSON), &p.I18n)
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// FreeProviderKrouterNames returns the union set of every
// `krouter_provider_name` declared as free-credit across active rows,
// flattening both the primary protocol column and the
// additional_protocols JSON. Used by the API handler's "is this inherited
// provider on the free catalog?" join. For protocol-aware routing
// lookups use FreeProviderKrouterNamesByProtocol.
func (s *Store) FreeProviderKrouterNames(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.ListFreeProviders(ctx, true)
	if err != nil {
		return nil, err
	}
	out := map[string]struct{}{}
	for _, p := range rows {
		if p.KrouterProviderName != "" {
			out[p.KrouterProviderName] = struct{}{}
		}
		for _, ap := range p.AdditionalProtocols {
			if ap.KrouterProviderName != "" {
				out[ap.KrouterProviderName] = struct{}{}
			}
		}
	}
	return out, nil
}

// FreeProviderKrouterNamesByProtocol returns a map from protocol →
// set-of-krouter-names. The routing engine's per-protocol decision path
// uses this to keep spec/00 §B2 (same-protocol routing) intact: an
// anthropic-protocol request only considers the anthropic-side
// krouter_provider_name even if the same vendor has an openai entry
// alongside it.
//
// Example return for the current catalog:
//
//	{
//	  "openai":    {"deepseek": {}, "groq": {}, "openrouter": {}, "zai": {}, ...},
//	  "anthropic": {"openrouter-anthropic": {}, "zai-anthropic": {}, "moonshot-anthropic": {}},
//	}
func (s *Store) FreeProviderKrouterNamesByProtocol(ctx context.Context) (map[string]map[string]struct{}, error) {
	rows, err := s.ListFreeProviders(ctx, true)
	if err != nil {
		return nil, err
	}
	out := map[string]map[string]struct{}{}
	add := func(proto, name string) {
		if proto == "" || name == "" {
			return
		}
		if out[proto] == nil {
			out[proto] = map[string]struct{}{}
		}
		out[proto][name] = struct{}{}
	}
	for _, p := range rows {
		add(p.Protocol, p.KrouterProviderName)
		for _, ap := range p.AdditionalProtocols {
			add(ap.Protocol, ap.KrouterProviderName)
		}
	}
	return out, nil
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
