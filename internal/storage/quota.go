package storage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// QuotaWindow holds the rolling token usage for a named quota window.
type QuotaWindow struct {
	WindowType  string    // "5h" | "weekly" | "opus"
	TokensUsed  int64
	WindowStart time.Time
	WindowEnd   time.Time
	UpdatedAt   time.Time
}

// GetQuota returns the current quota state for the given window type.
// Returns nil if no record exists.
func (s *Store) GetQuota(ctx context.Context, windowType string) (*QuotaWindow, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT window_type, tokens_used, window_start, window_end, updated_at
		FROM quota_state WHERE window_type = ?
	`, windowType)
	qw, err := scanQuota(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get quota %s: %w", windowType, err)
	}
	return qw, nil
}

// IncrementQuota adds tokens to the given window, creating the record if it does not exist.
// If the window has expired, it resets the window before incrementing.
func (s *Store) IncrementQuota(ctx context.Context, windowType string, tokens int64) error {
	now := time.Now().UTC()

	// Determine window duration.
	duration := windowDuration(windowType)
	windowEnd := now.Add(duration)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO quota_state (window_type, tokens_used, window_start, window_end, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(window_type) DO UPDATE SET
			tokens_used  = CASE
				WHEN window_end < ? THEN ?
				ELSE tokens_used + ?
			END,
			window_start = CASE WHEN window_end < ? THEN ? ELSE window_start END,
			window_end   = CASE WHEN window_end < ? THEN ? ELSE window_end END,
			updated_at   = ?
	`,
		windowType, tokens, now, windowEnd, now,
		// ON CONFLICT SET clause args
		now, tokens,  // CASE WHEN window_end < now THEN tokens ELSE tokens_used + tokens
		tokens,
		now, now,     // window_start reset
		now, windowEnd, // window_end reset
		now,
	)
	if err != nil {
		return fmt.Errorf("increment quota %s: %w", windowType, err)
	}
	return nil
}

// ResetQuota sets the quota window to zero with the given start/end times.
func (s *Store) ResetQuota(ctx context.Context, windowType string, start, end time.Time) error {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO quota_state (window_type, tokens_used, window_start, window_end, updated_at)
		VALUES (?, 0, ?, ?, ?)
		ON CONFLICT(window_type) DO UPDATE SET
			tokens_used  = 0,
			window_start = excluded.window_start,
			window_end   = excluded.window_end,
			updated_at   = excluded.updated_at
	`, windowType, start, end, now)
	if err != nil {
		return fmt.Errorf("reset quota %s: %w", windowType, err)
	}
	return nil
}

// ListQuotas returns all quota windows.
func (s *Store) ListQuotas(ctx context.Context) ([]QuotaWindow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT window_type, tokens_used, window_start, window_end, updated_at
		FROM quota_state ORDER BY window_type
	`)
	if err != nil {
		return nil, fmt.Errorf("list quotas: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var out []QuotaWindow
	for rows.Next() {
		qw, err := scanQuota(rows)
		if err != nil {
			return nil, fmt.Errorf("scan quota: %w", err)
		}
		out = append(out, *qw)
	}
	return out, rows.Err()
}

type quotaScanner interface {
	Scan(dest ...any) error
}

func scanQuota(row quotaScanner) (*QuotaWindow, error) {
	var qw QuotaWindow
	err := row.Scan(
		&qw.WindowType,
		&qw.TokensUsed,
		&qw.WindowStart,
		&qw.WindowEnd,
		&qw.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &qw, nil
}

func windowDuration(windowType string) time.Duration {
	switch windowType {
	case "5h":
		return 5 * time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	case "opus":
		return 24 * time.Hour
	default:
		return 24 * time.Hour
	}
}
