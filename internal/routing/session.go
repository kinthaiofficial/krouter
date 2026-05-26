package routing

// SessionState tracks aggregated token usage for a single agent conversation
// (identified by a stable session key derived from api_key + system_prompt +
// tools + first user message).
//
// BoundProvider and BoundModel are set from the first request's routing
// decision and never updated. They are the sticky target for Phase 3
// cache-aware routing: switching away from this (provider, model) pair would
// invalidate the accumulated prompt cache.
type SessionState struct {
	BoundProvider    string // first request's resolved provider — sticky target
	BoundModel       string // first request's resolved model — sticky target
	RequestCount     int
	FreshInputTokens int
	CachedTokens     int
	OutputTokens     int
	CacheWriteTokens int
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
