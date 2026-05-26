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
	FreshInputTokens int // cumulative fresh (non-cached) input tokens
	CachedTokens     int // cumulative cache-read tokens
	OutputTokens     int // cumulative output tokens
	CacheWriteTokens int // cumulative cache-write tokens

	// Last-request token buckets — used for hit-rate prediction.
	// Updated on every response; reflect the most recent request only.
	LastInputTokens int // cache_input_tokens (fresh, non-cached)
	LastCacheRead   int // cache_read_input_tokens
	LastCacheWrite  int // cache_creation_input_tokens
}

// ObservedHitRate returns the cache hit rate observed on the last request:
//
//	cache_read / (fresh + cache_read + cache_write)
//
// Use this for dashboard display and cost accounting — it describes what
// actually happened on the most recent turn.
// Returns 0 when no last-request data is available.
func (s SessionState) ObservedHitRate() float64 {
	total := s.LastInputTokens + s.LastCacheRead + s.LastCacheWrite
	if total == 0 {
		return 0
	}
	return float64(s.LastCacheRead) / float64(total)
}

// PredictedHitRate estimates the cache hit rate for the *next* request:
//
//	(cache_read + cache_write) / (fresh + cache_read + cache_write)
//
// The cache_write tokens from the last request become readable cache on the
// next request, so they belong in the numerator when predicting future savings.
// Use this for sticky-routing decisions — it avoids underestimating cache
// benefit and triggering unnecessary provider switches at the margin.
//
// For protocols that don't expose cache_write (OpenAI), cache_write is 0 and
// PredictedHitRate degrades to ObservedHitRate.
func (s SessionState) PredictedHitRate() float64 {
	total := s.LastInputTokens + s.LastCacheRead + s.LastCacheWrite
	if total == 0 {
		return 0
	}
	return float64(s.LastCacheRead+s.LastCacheWrite) / float64(total)
}

// OutputShare returns the fraction of cumulative tokens that were output tokens.
// Returns 0 when no tokens have been seen yet.
func (s SessionState) OutputShare() float64 {
	total := s.FreshInputTokens + s.CachedTokens + s.CacheWriteTokens + s.OutputTokens
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
