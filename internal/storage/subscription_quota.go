package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SubscriptionQuota holds the cached state of one subscription provider's
// call-count quota window, as returned by the provider's quota API.
type SubscriptionQuota struct {
	Provider     string
	ModelPattern string
	WindowStart  time.Time
	WindowEnd    time.Time
	TotalCount   int64
	UsedCount    int64
	Highspeed    bool
	FetchedAt    time.Time
}

// IsAvailable reports whether the quota window is still open and has calls remaining.
func (q *SubscriptionQuota) IsAvailable() bool {
	if q == nil {
		return false
	}
	if time.Now().UTC().After(q.WindowEnd) {
		return false // window has expired; wait for next poll
	}
	return q.UsedCount < q.TotalCount
}

// EffectiveCostUSD returns the amortised cost per call in USD.
// Formula: monthly_price_cny / (total_count_per_window * windows_per_month) / cny_to_usd
// This is used for the savings report, not for routing decisions
// (routing simply checks IsAvailable()).
func (q *SubscriptionQuota) EffectiveCostUSD() float64 {
	if q == nil || q.TotalCount == 0 {
		return 0
	}
	const cnyToUSD = 0.138
	const windowsPerMonth = 144.0 // 30 days × 24h / 5h
	priceCNY := minimaxPlanPriceCNY(q.TotalCount, q.Highspeed)
	if priceCNY == 0 {
		return 0
	}
	return priceCNY * cnyToUSD / (float64(q.TotalCount) * windowsPerMonth)
}

// minimaxPlanPriceCNY returns the monthly subscription price in CNY for the
// given window call limit and speed tier. Returns 0 for unknown combinations.
func minimaxPlanPriceCNY(totalCount int64, highspeed bool) float64 {
	type key struct {
		count     int64
		highspeed bool
	}
	prices := map[key]float64{
		{600, false}:   29,
		{1500, false}:  49,
		{4500, false}:  119,
		{1500, true}:   98,
		{4500, true}:   199,
		{30000, true}:  899,
	}
	return prices[key{totalCount, highspeed}]
}

// UpsertSubscriptionQuota writes (or replaces) a quota cache row.
func (s *Store) UpsertSubscriptionQuota(ctx context.Context, q SubscriptionQuota) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO subscription_quota_cache
			(provider, model_pattern, window_start, window_end,
			 total_count, used_count, highspeed, fetched_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(provider, model_pattern) DO UPDATE SET
			window_start = excluded.window_start,
			window_end   = excluded.window_end,
			total_count  = excluded.total_count,
			used_count   = excluded.used_count,
			highspeed    = excluded.highspeed,
			fetched_at   = excluded.fetched_at`,
		q.Provider,
		q.ModelPattern,
		q.WindowStart.UnixMilli(),
		q.WindowEnd.UnixMilli(),
		q.TotalCount,
		q.UsedCount,
		boolToInt(q.Highspeed),
		q.FetchedAt.UnixMilli(),
	)
	return err
}

// GetSubscriptionQuota returns the cached quota for a provider's model pattern.
// Returns nil (no error) when no row exists.
func (s *Store) GetSubscriptionQuota(ctx context.Context, provider, modelPattern string) (*SubscriptionQuota, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT provider, model_pattern, window_start, window_end,
		       total_count, used_count, highspeed, fetched_at
		FROM subscription_quota_cache
		WHERE provider = ? AND model_pattern = ?`,
		provider, modelPattern,
	)
	return scanSubscriptionQuota(row)
}

// GetAllSubscriptionQuotas returns all cached quota rows.
func (s *Store) GetAllSubscriptionQuotas(ctx context.Context) ([]SubscriptionQuota, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider, model_pattern, window_start, window_end,
		       total_count, used_count, highspeed, fetched_at
		FROM subscription_quota_cache
		ORDER BY provider, model_pattern`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var out []SubscriptionQuota
	for rows.Next() {
		q, err := scanSubscriptionQuota(rows)
		if err != nil {
			return nil, err
		}
		if q != nil {
			out = append(out, *q)
		}
	}
	return out, rows.Err()
}

// IsSubscriptionAvailable is a convenience query: true when quota exists and
// has remaining calls in the current window.
func (s *Store) IsSubscriptionAvailable(ctx context.Context, provider string) bool {
	rows, err := s.GetAllSubscriptionQuotas(ctx)
	if err != nil {
		return false
	}
	for _, q := range rows {
		if q.Provider == provider && q.IsAvailable() {
			return true
		}
	}
	return false
}

type scannable interface {
	Scan(dest ...any) error
}

func scanSubscriptionQuota(row scannable) (*SubscriptionQuota, error) {
	var q SubscriptionQuota
	var windowStartMS, windowEndMS, fetchedAtMS int64
	var highspeedInt int
	err := row.Scan(
		&q.Provider,
		&q.ModelPattern,
		&windowStartMS,
		&windowEndMS,
		&q.TotalCount,
		&q.UsedCount,
		&highspeedInt,
		&fetchedAtMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	q.WindowStart = time.UnixMilli(windowStartMS).UTC()
	q.WindowEnd = time.UnixMilli(windowEndMS).UTC()
	q.FetchedAt = time.UnixMilli(fetchedAtMS).UTC()
	q.Highspeed = highspeedInt != 0
	return &q, nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
