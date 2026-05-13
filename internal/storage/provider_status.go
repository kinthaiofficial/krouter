package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// ProviderStatus holds rolling health metrics for a single provider.
type ProviderStatus struct {
	Provider            string
	LastSuccessAt       *time.Time
	LastFailureAt       *time.Time
	ConsecutiveFailures int
	LastErrorCode       int
	RollingSuccessRate  float64 // approximation over recent requests; 1.0 = 100%
}

// RecordSuccess records a successful upstream call for the given provider.
// Resets consecutive_failures and bumps the rolling success rate toward 1.0.
func (s *Store) RecordSuccess(ctx context.Context, provider string) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_status (provider, last_success_at, consecutive_failures, rolling_success_rate)
		VALUES (?, ?, 0, 1.0)
		ON CONFLICT(provider) DO UPDATE SET
			last_success_at      = excluded.last_success_at,
			consecutive_failures = 0,
			rolling_success_rate = MIN(1.0, (COALESCE(rolling_success_rate, 0.5) * 99 + 1.0) / 100)
	`, provider, now)
	if err != nil {
		return fmt.Errorf("record provider success %s: %w", provider, err)
	}
	return nil
}

// RecordFailure records a failed upstream call for the given provider.
// Increments consecutive_failures and nudges rolling success rate toward 0.
func (s *Store) RecordFailure(ctx context.Context, provider string, httpCode int) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_status (provider, last_failure_at, consecutive_failures, last_error_code, rolling_success_rate)
		VALUES (?, ?, 1, ?, 0.0)
		ON CONFLICT(provider) DO UPDATE SET
			last_failure_at      = excluded.last_failure_at,
			consecutive_failures = consecutive_failures + 1,
			last_error_code      = excluded.last_error_code,
			rolling_success_rate = MAX(0.0, (COALESCE(rolling_success_rate, 0.5) * 99 + 0.0) / 100)
	`, provider, now, httpCode)
	if err != nil {
		return fmt.Errorf("record provider failure %s: %w", provider, err)
	}
	return nil
}

// GetProviderStatus returns the health record for the given provider.
// Returns nil if no record exists yet.
func (s *Store) GetProviderStatus(ctx context.Context, provider string) (*ProviderStatus, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT provider, last_success_at, last_failure_at,
		       consecutive_failures, last_error_code, rolling_success_rate
		FROM provider_status WHERE provider = ?
	`, provider)
	ps, err := scanProviderStatus(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get provider status %s: %w", provider, err)
	}
	return ps, nil
}

// ListProviderStatuses returns health records for all providers.
func (s *Store) ListProviderStatuses(ctx context.Context) ([]ProviderStatus, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, last_success_at, last_failure_at,
		       consecutive_failures, last_error_code, rolling_success_rate
		FROM provider_status ORDER BY provider
	`)
	if err != nil {
		return nil, fmt.Errorf("list provider statuses: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []ProviderStatus
	for rows.Next() {
		ps, err := scanProviderStatus(rows)
		if err != nil {
			return nil, fmt.Errorf("scan provider status: %w", err)
		}
		out = append(out, *ps)
	}
	return out, rows.Err()
}

// ConsecutiveFailures returns the consecutive failure count for a provider.
// Implements routing.HealthChecker. Returns 0 if no record exists.
func (s *Store) ConsecutiveFailures(provider string) int {
	var count int
	err := s.db.QueryRow(
		`SELECT COALESCE(consecutive_failures, 0) FROM provider_status WHERE provider = ?`,
		provider,
	).Scan(&count)
	if err != nil {
		return 0
	}
	return count
}

// scanner is satisfied by both *sql.Row and *sql.Rows.
type providerStatusScanner interface {
	Scan(dest ...any) error
}

func scanProviderStatus(row providerStatusScanner) (*ProviderStatus, error) {
	var ps ProviderStatus
	var lastSuccess, lastFailure sql.NullTime
	var errCode sql.NullInt64
	var rate sql.NullFloat64
	err := row.Scan(
		&ps.Provider,
		&lastSuccess,
		&lastFailure,
		&ps.ConsecutiveFailures,
		&errCode,
		&rate,
	)
	if err != nil {
		return nil, err
	}
	if lastSuccess.Valid {
		t := lastSuccess.Time
		ps.LastSuccessAt = &t
	}
	if lastFailure.Valid {
		t := lastFailure.Time
		ps.LastFailureAt = &t
	}
	if errCode.Valid {
		ps.LastErrorCode = int(errCode.Int64)
	}
	if rate.Valid {
		ps.RollingSuccessRate = rate.Float64
	} else {
		ps.RollingSuccessRate = 1.0
	}
	return &ps, nil
}
