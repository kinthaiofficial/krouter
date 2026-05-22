package storage

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// SubscriptionPrice mirrors one row of the token_price_sub table.
//
// Tiers are uniquely identified by (provider, tier_pattern, total_count,
// highspeed); the same vendor can sell multiple sizes of the same tier
// pattern (1500 / 4500 / 30000 calls per window).
type SubscriptionPrice struct {
	Provider        string
	TierPattern     string
	TotalCount      int64
	Highspeed       bool
	MonthlyPriceCNY float64
	WindowHours     int
	CNYToUSD        float64
	DataSourceURL   string
	UpdatedAt       time.Time
}

// WindowsPerMonth returns how many quota windows fit in a 30-day month,
// using the per-row window_hours. Used by EffectiveCostPerCallUSD to
// amortise the monthly price over the per-call cost.
func (p SubscriptionPrice) WindowsPerMonth() float64 {
	if p.WindowHours == 0 {
		return 0
	}
	return (30.0 * 24.0) / float64(p.WindowHours)
}

// EffectiveCostPerCallUSD returns the per-call USD cost for this tier:
//
//	monthly_price_cny × cny_to_usd / (total_count × windows_per_month)
//
// Returns 0 if any factor is missing (treats unknown tiers as "free" so
// routing still prefers paid-subscription endpoints over per-token vendors).
func (p SubscriptionPrice) EffectiveCostPerCallUSD() float64 {
	wpm := p.WindowsPerMonth()
	if p.TotalCount == 0 || wpm == 0 || p.MonthlyPriceCNY == 0 {
		return 0
	}
	return p.MonthlyPriceCNY * p.CNYToUSD / (float64(p.TotalCount) * wpm)
}

// MonthlyPriceUSD returns the sticker price normalised to USD.
func (p SubscriptionPrice) MonthlyPriceUSD() float64 {
	return p.MonthlyPriceCNY * p.CNYToUSD
}

// UpsertSubscriptionPrice inserts or replaces one row in token_price_sub.
// Used by the installer when seeding from token_price_sub.json.
func (s *Store) UpsertSubscriptionPrice(ctx context.Context, p SubscriptionPrice) error {
	const q = `INSERT INTO token_price_sub
		(provider, tier_pattern, total_count, highspeed,
		 monthly_price_cny, window_hours, cny_to_usd,
		 data_source_url, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?)
		ON CONFLICT(provider, tier_pattern, total_count, highspeed) DO UPDATE SET
		  monthly_price_cny = excluded.monthly_price_cny,
		  window_hours      = excluded.window_hours,
		  cny_to_usd        = excluded.cny_to_usd,
		  data_source_url   = excluded.data_source_url,
		  updated_at        = excluded.updated_at`
	_, err := s.db.ExecContext(ctx, q,
		p.Provider, p.TierPattern, p.TotalCount, boolToInt(p.Highspeed),
		p.MonthlyPriceCNY, p.WindowHours, p.CNYToUSD,
		nullableString(p.DataSourceURL), p.UpdatedAt.UTC().UnixMilli(),
	)
	return err
}

// FindSubscriptionPrice looks up one tier. Returns (nil, nil) when no row
// matches — the caller should treat that as "unknown SKU, effective cost 0".
func (s *Store) FindSubscriptionPrice(ctx context.Context, provider string, totalCount int64, highspeed bool) (*SubscriptionPrice, error) {
	const q = `SELECT provider, tier_pattern, total_count, highspeed,
	         monthly_price_cny, window_hours, cny_to_usd,
	         COALESCE(data_source_url,''), updated_at
	         FROM token_price_sub
	         WHERE provider = ? AND total_count = ? AND highspeed = ?
	         LIMIT 1`
	row := s.db.QueryRowContext(ctx, q, provider, totalCount, boolToInt(highspeed))
	return scanSubscriptionPrice(row)
}

// ListSubscriptionPrices returns every row for inspection / dashboard use.
func (s *Store) ListSubscriptionPrices(ctx context.Context) ([]SubscriptionPrice, error) {
	const q = `SELECT provider, tier_pattern, total_count, highspeed,
	         monthly_price_cny, window_hours, cny_to_usd,
	         COALESCE(data_source_url,''), updated_at
	         FROM token_price_sub
	         ORDER BY provider, total_count, highspeed`
	rows, err := s.db.QueryContext(ctx, q)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []SubscriptionPrice
	for rows.Next() {
		p, err := scanSubscriptionPrice(rows)
		if err != nil {
			return nil, err
		}
		if p != nil {
			out = append(out, *p)
		}
	}
	return out, rows.Err()
}

type subPriceScanner interface {
	Scan(dest ...any) error
}

func scanSubscriptionPrice(row subPriceScanner) (*SubscriptionPrice, error) {
	var p SubscriptionPrice
	var hs int
	var updMS int64
	err := row.Scan(
		&p.Provider, &p.TierPattern, &p.TotalCount, &hs,
		&p.MonthlyPriceCNY, &p.WindowHours, &p.CNYToUSD,
		&p.DataSourceURL, &updMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	p.Highspeed = hs != 0
	p.UpdatedAt = time.UnixMilli(updMS).UTC()
	return &p, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
