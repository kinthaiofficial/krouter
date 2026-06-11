package routing

// cacheHitBreakeven returns the cache-hit-rate threshold above which sticking
// with the bound model is cheaper than switching to a cheaper candidate.
//
// Both prices must be *known* (ok=true from the PricingSource) — callers
// handle unknown prices before calling. A price of 0 means genuinely free.
//
// Math (input-heavy approximation, N tokens per turn):
//
//	Stick:  cost = N × [p×0.1  + (1−p)×1.0] × P_bound
//	Switch: cost = N × 1.25 × P_candidate   (cache cold + 5 min write surcharge)
//
//	Set equal, solve for p:
//	  p* = (1 − 1.25 × P_candidate / P_bound) / 0.9
//
// Return values:
//
//	value in (0,1): stick when actual hit rate ≥ this value
//	0:              candidate is not cheaper (including a free bound model) —
//	                no incentive to switch
//	1:              candidate is so cheap (e.g. free) that even a 100% hit
//	                rate can't save the bound model — switching always wins
func cacheHitBreakeven(boundPrice, candidatePrice float64) float64 {
	if candidatePrice >= boundPrice {
		return 0 // no cheaper alternative; no reason to switch
	}
	// boundPrice > candidatePrice ≥ 0 from here on, so boundPrice > 0.
	ratio := candidatePrice / boundPrice
	p := (1.0 - 1.25*ratio) / 0.9
	if p > 1.0 {
		return 1.0
	}
	if p < 0 {
		return 0.0
	}
	return p
}
