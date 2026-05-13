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
