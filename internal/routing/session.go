package routing

// SessionState tracks aggregated token usage for a single agent conversation
// (identified by a stable session key derived from api_key + system_prompt +
// tools + first user message). Used in Phase 2 (shadow mode) to observe cache
// hit rates before Phase 3 enables cache-aware sticky routing.
type SessionState struct {
	Requests         int
	FreshInputTokens int
	CachedTokens     int
	OutputTokens     int
	CacheWriteTokens int
	LastProvider     string
	LastModel        string
}

// CacheHitRate returns the fraction of input tokens served from cache.
// Returns 0 when no input tokens have been seen yet.
func (s SessionState) CacheHitRate() float64 {
	total := s.FreshInputTokens + s.CachedTokens
	if total == 0 {
		return 0
	}
	return float64(s.CachedTokens) / float64(total)
}

// OutputShare returns the fraction of total tokens that were output tokens.
// Returns 0 when no tokens have been seen yet.
func (s SessionState) OutputShare() float64 {
	total := s.FreshInputTokens + s.CachedTokens + s.OutputTokens
	if total == 0 {
		return 0
	}
	return float64(s.OutputTokens) / float64(total)
}

// SessionSource is the read/update interface for session state.
// Implementations must be safe for concurrent use.
type SessionSource interface {
	// Get returns the current state for the given key.
	// ok is false when no state exists yet for the key.
	Get(key string) (SessionState, bool)

	// Update atomically applies fn to the state for key.
	// If no state exists yet, fn receives a zero-value SessionState.
	Update(key string, fn func(*SessionState))
}
