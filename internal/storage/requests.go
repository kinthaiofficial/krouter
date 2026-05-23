package storage

import (
	"context"
	"time"
)

// RequestRecord mirrors the `requests` table schema (spec/05-storage.md).
type RequestRecord struct {
	ID             string
	Timestamp      time.Time
	Agent          string // "openclaw" | "claude-code" | "cursor" | "unknown"
	Protocol       string // "anthropic" | "openai"
	RequestedModel string
	Provider       string // actual_provider
	Model          string // actual_model
	InputTokens    int
	OutputTokens   int
	CachedTokens   int
	CostMicroUSD   int64 // 1 000 000 = $1.00
	LatencyMS      int64
	StatusCode     int
	ErrorMessage   string
}

// InsertRequest writes a completed request record to the database.
func (s *Store) InsertRequest(ctx context.Context, r RequestRecord) error {
	const q = `INSERT INTO requests
		(id, ts_utc, agent, protocol, requested_model,
		 actual_provider, actual_model,
		 input_tokens, output_tokens, cached_tokens,
		 cost_micro_usd, latency_ms, status_code, error_message)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`

	_, err := s.db.ExecContext(ctx, q,
		r.ID,
		r.Timestamp.UTC().Format(time.RFC3339),
		r.Agent,
		r.Protocol,
		r.RequestedModel,
		r.Provider,
		r.Model,
		r.InputTokens,
		r.OutputTokens,
		r.CachedTokens,
		r.CostMicroUSD,
		r.LatencyMS,
		r.StatusCode,
		r.ErrorMessage,
	)
	return err
}

// ListRequestsByAgent returns the most recent `limit` requests for a specific agent, newest first.
func (s *Store) ListRequestsByAgent(ctx context.Context, agent string, limit int) ([]RequestRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	const q = `SELECT
		id, ts_utc, COALESCE(agent,''), protocol,
		COALESCE(requested_model,''), COALESCE(actual_provider,''), COALESCE(actual_model,''),
		COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(cached_tokens,0),
		COALESCE(cost_micro_usd,0), COALESCE(latency_ms,0),
		COALESCE(status_code,0), COALESCE(error_message,'')
		FROM requests
		WHERE agent = ?
		ORDER BY ts_utc DESC
		LIMIT ?`

	rows, err := s.db.QueryContext(ctx, q, agent, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []RequestRecord
	for rows.Next() {
		var r RequestRecord
		var tsStr string
		if err := rows.Scan(
			&r.ID, &tsStr, &r.Agent, &r.Protocol,
			&r.RequestedModel, &r.Provider, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.CachedTokens,
			&r.CostMicroUSD, &r.LatencyMS,
			&r.StatusCode, &r.ErrorMessage,
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
		id, ts_utc, COALESCE(agent,''), protocol,
		COALESCE(requested_model,''), COALESCE(actual_provider,''), COALESCE(actual_model,''),
		COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(cached_tokens,0),
		COALESCE(cost_micro_usd,0), COALESCE(latency_ms,0),
		COALESCE(status_code,0), COALESCE(error_message,'')
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
			&r.ID, &tsStr, &r.Agent, &r.Protocol,
			&r.RequestedModel, &r.Provider, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.CachedTokens,
			&r.CostMicroUSD, &r.LatencyMS,
			&r.StatusCode, &r.ErrorMessage,
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
	RequestCount  int
	InputTokens   int64
	OutputTokens  int64
	CachedTokens  int64
	CostMicroUSD  int64
}

func (s *Store) ProviderTokenTotalsSince(ctx context.Context, provider string, sinceUTC time.Time) (ProviderTokenTotals, error) {
	const q = `
		SELECT COUNT(*),
		       COALESCE(SUM(input_tokens), 0),
		       COALESCE(SUM(output_tokens), 0),
		       COALESCE(SUM(cached_tokens), 0),
		       COALESCE(SUM(cost_micro_usd), 0)
		  FROM requests
		 WHERE actual_provider = ?
		   AND ts_utc >= ?`
	var tot ProviderTokenTotals
	err := s.db.QueryRowContext(ctx, q, provider, sinceUTC.UTC().Format(time.RFC3339)).
		Scan(&tot.RequestCount, &tot.InputTokens, &tot.OutputTokens, &tot.CachedTokens, &tot.CostMicroUSD)
	return tot, err
}

// ListRequestsInRange returns requests where from <= ts_utc <= to, newest first.
// Optional agent filter. Limit defaults to 10000 if <= 0.
func (s *Store) ListRequestsInRange(ctx context.Context, from, to time.Time, agent string, limit int) ([]RequestRecord, error) {
	if limit <= 0 {
		limit = 10000
	}
	fromStr := from.UTC().Format(time.RFC3339)
	toStr := to.UTC().Format(time.RFC3339)

	var q string
	var args []any
	if agent != "" {
		q = `SELECT id, ts_utc,
			COALESCE(agent,''), protocol,
			COALESCE(requested_model,''), COALESCE(actual_provider,''), COALESCE(actual_model,''),
			COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(cached_tokens,0),
			COALESCE(cost_micro_usd,0), COALESCE(latency_ms,0),
			COALESCE(status_code,0), COALESCE(error_message,'')
			FROM requests WHERE ts_utc >= ? AND ts_utc <= ? AND agent = ? ORDER BY ts_utc DESC LIMIT ?`
		args = []any{fromStr, toStr, agent, limit}
	} else {
		q = `SELECT id, ts_utc,
			COALESCE(agent,''), protocol,
			COALESCE(requested_model,''), COALESCE(actual_provider,''), COALESCE(actual_model,''),
			COALESCE(input_tokens,0), COALESCE(output_tokens,0), COALESCE(cached_tokens,0),
			COALESCE(cost_micro_usd,0), COALESCE(latency_ms,0),
			COALESCE(status_code,0), COALESCE(error_message,'')
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
			&r.ID, &tsStr, &r.Agent, &r.Protocol,
			&r.RequestedModel, &r.Provider, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.CachedTokens,
			&r.CostMicroUSD, &r.LatencyMS,
			&r.StatusCode, &r.ErrorMessage,
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
