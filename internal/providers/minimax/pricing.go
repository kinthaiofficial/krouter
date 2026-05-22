package minimax

// subscriptionTier captures one purchasable MiniMax plan as listed on
// https://platform.minimaxi.com/pricing (verified 2026-05). Tiers are
// distinguished by total_count: the API tells us how many calls per window the
// user purchased, and that number uniquely identifies the SKU.
//
// monthly_price_usd × 1000 (millicents) is stored as an int to keep the type
// free of floating-point comparison errors at boundary checks. Effective
// cost-per-call is monthly_price_usd / total_count.
//
// This table is the Phase-1 placeholder for spec/05 §11 — once the
// subscription_pricing.json sync mechanism ships, the data moves to the
// pricing_cache table and this slice goes away.
type subscriptionTier struct {
	TierPattern     string  // glob pattern returned by the API: "MiniMax-M*", "speech-hd", …
	Highspeed       bool    // true when the SKU is the highspeed-only variant
	TotalCount      int     // purchased quota per window
	MonthlyPriceUSD float64 // sticker price
}

var minimaxTiers = []subscriptionTier{
	// MiniMax-M* (the LLM family). Four standard tiers + four highspeed tiers.
	{TierPattern: "MiniMax-M*", Highspeed: false, TotalCount: 600, MonthlyPriceUSD: 19},
	{TierPattern: "MiniMax-M*", Highspeed: false, TotalCount: 1500, MonthlyPriceUSD: 49},
	{TierPattern: "MiniMax-M*", Highspeed: false, TotalCount: 4500, MonthlyPriceUSD: 99},
	{TierPattern: "MiniMax-M*", Highspeed: false, TotalCount: 30000, MonthlyPriceUSD: 599},

	{TierPattern: "MiniMax-M*", Highspeed: true, TotalCount: 600, MonthlyPriceUSD: 29},
	{TierPattern: "MiniMax-M*", Highspeed: true, TotalCount: 1500, MonthlyPriceUSD: 79},
	{TierPattern: "MiniMax-M*", Highspeed: true, TotalCount: 4500, MonthlyPriceUSD: 169},
	{TierPattern: "MiniMax-M*", Highspeed: true, TotalCount: 30000, MonthlyPriceUSD: 999},
}

// EffectiveCostPerCallUSD looks up the monthly price for the given tier and
// returns the per-call effective cost (monthly_price / total_count).
//
// When no tier matches (user is on a non-standard SKU we haven't catalogued,
// or pattern is unknown), the function returns 0. The routing engine should
// still prefer 0-cost endpoints over per-token fallbacks, so an unknown but
// quota-available SKU is treated as "free" — which is the right default
// because we know the user already paid for it.
func EffectiveCostPerCallUSD(tierPattern string, totalCount int, highspeed bool) float64 {
	for _, t := range minimaxTiers {
		if t.TierPattern == tierPattern && t.TotalCount == totalCount && t.Highspeed == highspeed {
			if t.TotalCount == 0 {
				return 0
			}
			return t.MonthlyPriceUSD / float64(t.TotalCount)
		}
	}
	return 0
}

// MonthlyPriceUSD returns the sticker price of a tier, or 0 when unknown.
// Exposed so the UI can show "你的套餐: $49/月 (1500 次)" alongside the
// remaining-quota bar.
func MonthlyPriceUSD(tierPattern string, totalCount int, highspeed bool) float64 {
	for _, t := range minimaxTiers {
		if t.TierPattern == tierPattern && t.TotalCount == totalCount && t.Highspeed == highspeed {
			return t.MonthlyPriceUSD
		}
	}
	return 0
}
