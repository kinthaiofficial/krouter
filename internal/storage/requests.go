package storage

import (
	"context"
	"time"
)

// RequestRecord mirrors the `requests` table schema (spec/05-storage.md).
//
// Token bucket semantics (post-Phase1):
//
//	InputTokens:      fresh input tokens (not cached, not written to cache)
//	CachedTokens:     cache_read_input_tokens (billed at ~10% of input price)
//	CacheWriteTokens: cache_creation_input_tokens (billed at 1.25× input price, 5m TTL)
type RequestRecord struct {
	ID               string
	Timestamp        time.Time
	App              string // "openclaw" | "claude-code" | "cursor" | "unknown"
	Protocol         string // "anthropic" | "openai"
	RequestedModel   string
	Provider         string // actual_provider
	Model            string // actual_model
	InputTokens      int
	OutputTokens     int
	CachedTokens     int
	CacheWriteTokens int
	CostMicroUSD     int64 // 1 000 000 = $1.00
	LatencyMS        int64
	StatusCode       int
	ErrorMessage     string
	RoutingPreset    string // "saver" | "balanced" | "quality" | "passthrough" | ""
	KeyHint          string // last 4 chars of the api_key; "" = no key; not set for pre-migration rows
}

// InsertRequest writes a completed request record to the database.
func (s *Store) InsertRequest(ctx context.Context, r RequestRecord) error {
	const q = `INSERT INTO requests
		(id, ts_utc, app, protocol, requested_model,
		 actual_provider, actual_model,
		 input_tokens, output_tokens, cached_tokens, cache_write_tokens,
		 cost_micro_usd, latency_ms, status_code, error_message, routing_preset, key_hint)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

	_, err := s.db.ExecContext(ctx, q,
		r.ID,
		r.Timestamp.UTC().Format(time.RFC3339),
		r.App,
		r.Protocol,
		r.RequestedModel,
		r.Provider,
		r.Model,
		r.InputTokens,
		r.OutputTokens,
		r.CachedTokens,
		r.CacheWriteTokens,
		r.CostMicroUSD,
		r.LatencyMS,
		r.StatusCode,
		r.ErrorMessage,
		r.RoutingPreset,
		r.KeyHint,
	)
	return err
}

// ListRequestsByApp returns the most recent `limit` requests for a specific app, newest first.
func (s *Store) ListRequestsByApp(ctx context.Context, app string, limit int) ([]RequestRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `SELECT
		id, ts_utc, COALESCE(app,''), protocol,
		COALESCE(requested_model,''), COALESCE(actual_provider,''), COALESCE(actual_model,''),
		COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(cached_tokens,0), COALESCE(cache_write_tokens,0),
		COALESCE(cost_micro_usd,0), COALESCE(latency_ms,0),
		COALESCE(status_code,0), COALESCE(error_message,''), COALESCE(routing_preset,'')
		FROM requests
		WHERE app = ?
		ORDER BY ts_utc DESC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, app, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []RequestRecord
	for rows.Next() {
		var r RequestRecord
		var tsStr string
		if err := rows.Scan(
			&r.ID, &tsStr, &r.App, &r.Protocol,
			&r.RequestedModel, &r.Provider, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.CachedTokens, &r.CacheWriteTokens,
			&r.CostMicroUSD, &r.LatencyMS,
			&r.StatusCode, &r.ErrorMessage, &r.RoutingPreset,
		); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
			r.Timestamp = t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ListRequests returns the most recent `limit` requests, newest first.
func (s *Store) ListRequests(ctx context.Context, limit int) ([]RequestRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `SELECT
		id, ts_utc, COALESCE(app,''), protocol,
		COALESCE(requested_model,''), COALESCE(actual_provider,''), COALESCE(actual_model,''),
		COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(cached_tokens,0), COALESCE(cache_write_tokens,0),
		COALESCE(cost_micro_usd,0), COALESCE(latency_ms,0),
		COALESCE(status_code,0), COALESCE(error_message,''), COALESCE(routing_preset,'')
		FROM requests
		ORDER BY ts_utc DESC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []RequestRecord
	for rows.Next() {
		var r RequestRecord
		var tsStr string
		if err := rows.Scan(
			&r.ID, &tsStr, &r.App, &r.Protocol,
			&r.RequestedModel, &r.Provider, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.CachedTokens, &r.CacheWriteTokens,
			&r.CostMicroUSD, &r.LatencyMS,
			&r.StatusCode, &r.ErrorMessage, &r.RoutingPreset,
		); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
			r.Timestamp = t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ProviderStat aggregates request statistics for a provider.
type ProviderStat struct {
	RequestCount int
	CostMicroUSD int64
	P50MS        int64
	P95MS        int64
}

// ProviderStatSince returns aggregated stats for a provider from sinceUTC onward.
// Latencies are fetched sorted ascending; P50 and P95 are percentile indexes.
func (s *Store) ProviderStatSince(ctx context.Context, provider string, sinceUTC time.Time) (ProviderStat, error) {
	const q = `SELECT COALESCE(latency_ms,0), COALESCE(cost_micro_usd,0)
		FROM requests WHERE actual_provider = ? AND ts_utc >= ? ORDER BY latency_ms ASC`
	rows, err := s.db.QueryContext(ctx, q, provider, sinceUTC.UTC().Format(time.RFC3339))
	if err != nil {
		return ProviderStat{}, err
	}
	defer func() { _ = rows.Close() }()

	var lats []int64
	var stat ProviderStat
	for rows.Next() {
		var lat, cost int64
		if err := rows.Scan(&lat, &cost); err != nil {
			return ProviderStat{}, err
		}
		lats = append(lats, lat)
		stat.CostMicroUSD += cost
		stat.RequestCount++
	}
	if err := rows.Err(); err != nil {
		return ProviderStat{}, err
	}
	n := len(lats)
	if n > 0 {
		stat.P50MS = lats[n*50/100]
		stat.P95MS = lats[n*95/100]
	}
	return stat, nil
}

// ProviderTokenTotals aggregates lifetime token counts for a provider.
// Used by the Providers dashboard page so users can see "this provider
// has done 1.2M in / 0.4M out / 280k cached for $4.27 over its lifetime".
// `since == zero time` returns lifetime totals.
type ProviderTokenTotals struct {
	RequestCount     int
	InputTokens      int64
	OutputTokens     int64
	CachedTokens     int64
	CacheWriteTokens int64
	CostMicroUSD     int64
}

func (s *Store) ProviderTokenTotalsSince(ctx context.Context, provider string, sinceUTC time.Time) (ProviderTokenTotals, error) {
	const q = `
		SELECT COUNT(*),
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(cached_tokens), 0),
		       COALESCE(SUM(cache_write_tokens), 0),
		       COALESCE(SUM(cost_micro_usd), 0)
		  FROM requests
		 WHERE actual_provider = ?
		   AND ts_utc >= ?`
	var tot ProviderTokenTotals
	err := s.db.QueryRowContext(ctx, q, provider, sinceUTC.UTC().Format(time.RFC3339)).
		Scan(&tot.RequestCount, &tot.InputTokens, &tot.OutputTokens, &tot.CachedTokens,
			&tot.CacheWriteTokens, &tot.CostMicroUSD)
	return tot, err
}

// ListRequestsInRange returns requests where from <= ts_utc <= to, newest first.
// Optional app filter. Limit defaults to 10000 if <= 0.
func (s *Store) ListRequestsInRange(ctx context.Context, from, to time.Time, app string, limit int) ([]RequestRecord, error) {
	if limit <= 0 {
		limit = 10000
	}
	fromStr := from.UTC().Format(time.RFC3339)
	toStr := to.UTC().Format(time.RFC3339)

	var q string
	var args []any
	if app != "" {
		q = `SELECT id, ts_utc,
			COALESCE(app,''), protocol,
			COALESCE(requested_model,''), COALESCE(actual_provider,''), COALESCE(actual_model,''),
			COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(cached_tokens,0), COALESCE(cache_write_tokens,0),
			COALESCE(cost_micro_usd,0), COALESCE(latency_ms,0),
			COALESCE(status_code,0), COALESCE(error_message,''), COALESCE(routing_preset,'')
			FROM requests WHERE ts_utc >= ? AND ts_utc <= ? AND app = ? ORDER BY ts_utc DESC LIMIT ?`
		args = []any{fromStr, toStr, app, limit}
	} else {
		q = `SELECT id, ts_utc,
			COALESCE(app,''), protocol,
			COALESCE(requested_model,''), COALESCE(actual_provider,''), COALESCE(actual_model,''),
			COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(cached_tokens,0), COALESCE(cache_write_tokens,0),
			COALESCE(cost_micro_usd,0), COALESCE(latency_ms,0),
			COALESCE(status_code,0), COALESCE(error_message,''), COALESCE(routing_preset,'')
			FROM requests WHERE ts_utc >= ? AND ts_utc <= ? ORDER BY ts_utc DESC LIMIT ?`
		args = []any{fromStr, toStr, limit}
	}

	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []RequestRecord
	for rows.Next() {
		var r RequestRecord
		var tsStr string
		if err := rows.Scan(
			&r.ID, &tsStr, &r.App, &r.Protocol,
			&r.RequestedModel, &r.Provider, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.CachedTokens, &r.CacheWriteTokens,
			&r.CostMicroUSD, &r.LatencyMS,
			&r.StatusCode, &r.ErrorMessage, &r.RoutingPreset,
		); err != nil {
			return nil, err
		}
		if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
			r.Timestamp = t
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeleteAllRequests removes all request records from the database.
func (s *Store) DeleteAllRequests(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM requests`)
	return err
}

// KeyHintStats aggregates request statistics for one api-key channel within an app.
type KeyHintStats struct {
	KeyHint      string
	RequestCount int
	CostMicroUSD int64
}

// ListKeyHintsByApp returns per-key-hint aggregates for an app, ordered by request
// count descending. Rows with NULL key_hint (pre-migration) are excluded.
func (s *Store) ListKeyHintsByApp(ctx context.Context, app string) ([]KeyHintStats, error) {
	const q = `SELECT COALESCE(key_hint,''), COUNT(*), COALESCE(SUM(cost_micro_usd),0)
		FROM requests
		WHERE app = ? AND key_hint IS NOT NULL
		GROUP BY key_hint
		ORDER BY COUNT(*) DESC`
	rows, err := s.db.QueryContext(ctx, q, app)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []KeyHintStats
	for rows.Next() {
		var ks KeyHintStats
		if err := rows.Scan(&ks.KeyHint, &ks.RequestCount, &ks.CostMicroUSD); err != nil {
			return nil, err
		}
		out = append(out, ks)
	}
	return out, rows.Err()
}
