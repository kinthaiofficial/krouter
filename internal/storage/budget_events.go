package storage

import (
	"context"
	"time"
)

// Budget event types — same set monitorBudget broadcasts over SSE.
const (
	BudgetEventWarning80 = "warning_80"
	BudgetEventWarning95 = "warning_95"
	BudgetEventBlocked   = "blocked"
	BudgetEventUnblocked = "unblocked"
)

// BudgetEvent is one threshold transition. monitorBudget inserts at
// most once per (threshold, UTC day) so this table grows at the rate of
// a few rows per day, not per request.
type BudgetEvent struct {
	ID            int64
	Timestamp     time.Time
	EventType     string
	DailyPercent  float64
	DailyCostUSD  float64
	DailyLimitUSD float64
}

// InsertBudgetEvent appends a transition row. The caller is responsible
// for dedup; monitorBudget already tracks lastFiredThreshold to avoid
// double-firing for the same 80/95/100 boundary within a day.
func (s *Store) InsertBudgetEvent(ctx context.Context, e BudgetEvent) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO budget_events
		    (ts_utc, event_type, daily_percent, daily_cost_usd, daily_limit_usd)
		VALUES (?, ?, ?, ?, ?)`,
		e.Timestamp.UTC().UnixMilli(),
		e.EventType,
		e.DailyPercent,
		e.DailyCostUSD,
		e.DailyLimitUSD,
	)
	return err
}

// ListBudgetEvents returns up to `limit` most-recent events, newest first.
// Defaults to 50 when limit <= 0; capped at 500 to keep response sizes bounded.
func (s *Store) ListBudgetEvents(ctx context.Context, limit int) ([]BudgetEvent, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 500 {
		limit = 500
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, ts_utc, event_type, daily_percent, daily_cost_usd, daily_limit_usd
		  FROM budget_events
		 ORDER BY ts_utc DESC
		 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	out := make([]BudgetEvent, 0)
	for rows.Next() {
		var e BudgetEvent
		var tsMS int64
		if err := rows.Scan(&e.ID, &tsMS, &e.EventType, &e.DailyPercent, &e.DailyCostUSD, &e.DailyLimitUSD); err != nil {
			return nil, err
		}
		e.Timestamp = time.UnixMilli(tsMS).UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}
