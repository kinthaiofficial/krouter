package storage

import (
	"context"
	"database/sql"
	"errors"
	"path"
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

// MatchesModel reports whether the given model id falls under this quota's
// ModelPattern. Patterns follow glob semantics via `path.Match`, which matches
// the spec/05 §8 wildcard convention:
//
//	"MiniMax-M*"        → "MiniMax-M2.7", "MiniMax-M2.5-highspeed", "MiniMax-M2"
//	"speech-hd"         → "speech-hd"        (exact match)
//	"MiniMax-Hailuo-2*" → "MiniMax-Hailuo-2.3-Fast-6s-768p"
//
// This is used by routing so that when multiple tiers exist for a provider
// (e.g. MiniMax-M* for LLM + speech-hd for TTS + Hailuo for video), the
// routing engine consults the correct tier for the model it intends to
// rewrite the request to — instead of "first available", which was the
// pre-fix bug where speech-hd's leftover quota could mask MiniMax-M*
// exhaustion and break LLM requests.
func (q *SubscriptionQuota) MatchesModel(modelID string) bool {
	if q == nil || q.ModelPattern == "" || modelID == "" {
		return false
	}
	ok, err := path.Match(q.ModelPattern, modelID)
	return err == nil && ok
}

// PricingFor looks up the subscription tier row in token_price_sub that
// matches this quota's provider / total_count / highspeed combination,
// and returns a populated SubscriptionPrice. When no row matches (unknown
// SKU, table not yet seeded), a zero-value SubscriptionPrice is returned
// — its EffectiveCostPerCallUSD and MonthlyPriceUSD methods both return
// 0, which the routing engine treats as "free" (the user already paid
// for the subscription, we just lack the price tag).
//
// Single source of truth for subscription pricing: both routing
// (cmd/krouter/serve.go::subscriptionSource.GetSubscriptionInfo) and the
// dashboard (internal/api/subscription_status.go::tiersToJSON) read the
// same row through this method. No parallel lookup tables allowed
// anywhere else in the tree — see spec/05 §11 for the bug history that
// led to this rule.
func (q *SubscriptionQuota) PricingFor(ctx context.Context, store *Store) SubscriptionPrice {
	if q == nil || store == nil {
		return SubscriptionPrice{}
	}
	price, err := store.FindSubscriptionPrice(ctx, q.Provider, q.TotalCount, q.Highspeed)
	if err != nil || price == nil {
		return SubscriptionPrice{}
	}
	return *price
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
