package routing

// CacheHitBreakevenExport wraps the unexported cacheHitBreakeven for
// black-box tests in package routing_test.
func CacheHitBreakevenExport(bound, candidate float64) float64 {
	return cacheHitBreakeven(bound, candidate)
}
