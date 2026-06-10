// Package pricing maintains LLM pricing data and computes per-request costs.
//
// Three-layer architecture (see spec/04-pricing.md):
//   - Layer 1: Static fallback bundled at compile time
//   - Layer 2: Live sync from krouter CDN (primary) / GitHub raw (fallback) every 24h
//   - Layer 3: Per-request cost accounting
//
// The sync source is data/token_prices.json, a file auto-generated daily by
// scripts/update_prices.go which merges LiteLLM pricing with the local
// supplement (data/token_prices_ext.json). Fetching from our own CDN rather
// than directly from LiteLLM means a LiteLLM format change breaks only the
// daily Action, not every running daemon simultaneously.
package pricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/kinthaiofficial/krouter/data"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

const (
	// defaultPrimaryURL is the krouter-hosted canonical pricing file.
	// Self-hosting gives us access logs (version distribution, 304 ratio,
	// geographic breakdown) that raw.githubusercontent.com doesn't expose.
	defaultPrimaryURL = "https://krouter.kinthai.ai/data/token_prices.json"

	// defaultFallbackURL is the same file served by GitHub raw.
	// Used only when the primary endpoint fails (CDN outage, DNS issue).
	defaultFallbackURL = "https://raw.githubusercontent.com/kinthaiofficial/krouter/main/data/token_prices.json"
)

// LiteLLMToKrouterProvider maps LiteLLM's litellm_provider value to krouter's
// adapter name when the two names differ. Providers not listed here use the
// litellm_provider value directly as the adapter name.
var LiteLLMToKrouterProvider = map[string]string{
	"dashscope":    "qwen",      // Aliyun DashScope → krouter qwen adapter
	"together_ai":  "together",  // Together AI
	"fireworks_ai": "fireworks", // Fireworks AI
	// All other new providers use the same name in both LiteLLM and krouter:
	// gemini, xai, mistral, perplexity, groq, moonshot, zai, openai, deepseek, anthropic
}

// PriceEntry holds per-token costs for a model.
type PriceEntry struct {
	Provider                       string
	InputCostPerToken              float64 // cost per single token in USD
	OutputCostPerToken             float64
	CachedInputCostPerToken        float64 // cache_read: billed at ~10% of input
	CacheWriteInputCostPerToken    float64 // cache_creation (standard 5-min TTL): typically 1.25× input for Anthropic
	CacheWriteInputCostPerToken1hr float64 // cache_creation for TTL > 1 hr (Anthropic extended caching)
	MaxTokens                      int
}

// staticPrices is the Layer-1 fallback bundled at compile time.
// Values are cost-per-token in USD (divide $N/M by 1_000_000).
// Source: LiteLLM model_prices_and_context_window.json (2026-05).
var staticPrices = map[string]PriceEntry{
	// Anthropic — input/output/cache_read/cache_write all from LiteLLM.
	// cache_write_1hr: extended cache TTL (>1 hr), higher rate.
	"claude-opus-4-7":            {Provider: "anthropic", InputCostPerToken: 5.0 / 1e6, OutputCostPerToken: 25.0 / 1e6, CachedInputCostPerToken: 0.5 / 1e6, CacheWriteInputCostPerToken: 6.25 / 1e6, CacheWriteInputCostPerToken1hr: 10.0 / 1e6},
	"claude-sonnet-4-6":          {Provider: "anthropic", InputCostPerToken: 3.0 / 1e6, OutputCostPerToken: 15.0 / 1e6, CachedInputCostPerToken: 0.3 / 1e6, CacheWriteInputCostPerToken: 3.75 / 1e6},
	"claude-opus-4-5":            {Provider: "anthropic", InputCostPerToken: 15.0 / 1e6, OutputCostPerToken: 75.0 / 1e6, CachedInputCostPerToken: 1.5 / 1e6, CacheWriteInputCostPerToken: 18.75 / 1e6},
	"claude-sonnet-4-5":          {Provider: "anthropic", InputCostPerToken: 3.0 / 1e6, OutputCostPerToken: 15.0 / 1e6, CachedInputCostPerToken: 0.3 / 1e6, CacheWriteInputCostPerToken: 3.75 / 1e6},
	"claude-haiku-4-5":           {Provider: "anthropic", InputCostPerToken: 0.8 / 1e6, OutputCostPerToken: 4.0 / 1e6, CachedInputCostPerToken: 0.08 / 1e6, CacheWriteInputCostPerToken: 1.0 / 1e6},
	"claude-haiku-4-5-20251001":  {Provider: "anthropic", InputCostPerToken: 1.0 / 1e6, OutputCostPerToken: 5.0 / 1e6, CachedInputCostPerToken: 0.1 / 1e6, CacheWriteInputCostPerToken: 1.25 / 1e6, CacheWriteInputCostPerToken1hr: 2.0 / 1e6},
	"claude-3-5-sonnet-20241022": {Provider: "anthropic", InputCostPerToken: 3.0 / 1e6, OutputCostPerToken: 15.0 / 1e6, CachedInputCostPerToken: 0.3 / 1e6, CacheWriteInputCostPerToken: 3.75 / 1e6},
	"claude-3-5-haiku-20241022":  {Provider: "anthropic", InputCostPerToken: 0.8 / 1e6, OutputCostPerToken: 4.0 / 1e6, CachedInputCostPerToken: 0.08 / 1e6, CacheWriteInputCostPerToken: 1.0 / 1e6},
	"claude-3-opus-20240229":     {Provider: "anthropic", InputCostPerToken: 15.0 / 1e6, OutputCostPerToken: 75.0 / 1e6, CachedInputCostPerToken: 1.5 / 1e6, CacheWriteInputCostPerToken: 18.75 / 1e6},
	// DeepSeek — supports cache read; no cache creation charge.
	// LiteLLM 2026-05: deepseek-chat $0.28/$0.42 per MTok input/output.
	"deepseek-chat":  {Provider: "deepseek", InputCostPerToken: 0.28 / 1e6, OutputCostPerToken: 0.42 / 1e6, CachedInputCostPerToken: 0.028 / 1e6},
	"deepseek-coder": {Provider: "deepseek", InputCostPerToken: 0.14 / 1e6, OutputCostPerToken: 0.28 / 1e6},
	// OpenAI — supports cache read; no separate cache creation charge.
	"gpt-4o":        {Provider: "openai", InputCostPerToken: 2.5 / 1e6, OutputCostPerToken: 10.0 / 1e6, CachedInputCostPerToken: 1.25 / 1e6},
	"gpt-4o-mini":   {Provider: "openai", InputCostPerToken: 0.15 / 1e6, OutputCostPerToken: 0.6 / 1e6, CachedInputCostPerToken: 0.075 / 1e6},
	"gpt-4-turbo":   {Provider: "openai", InputCostPerToken: 10.0 / 1e6, OutputCostPerToken: 30.0 / 1e6},
	"gpt-3.5-turbo": {Provider: "openai", InputCostPerToken: 0.5 / 1e6, OutputCostPerToken: 1.5 / 1e6},
	// MiniMax (LiteLLM does not include these; static fallback from MiniMax API platform)
	"MiniMax-M2.7":           {Provider: "minimax", InputCostPerToken: 0.30 / 1e6, OutputCostPerToken: 1.20 / 1e6},
	"MiniMax-M2.7-highspeed": {Provider: "minimax", InputCostPerToken: 0.30 / 1e6, OutputCostPerToken: 1.20 / 1e6},
}

// Service maintains the LLM pricing table and computes costs.
type Service struct {
	mu          sync.RWMutex
	prices      map[string]PriceEntry // model_id → PriceEntry (live, may be updated by sync)
	store       *storage.Store
	httpClient  *http.Client
	logger      *slog.Logger
	primaryURL  string
	fallbackURL string
	userAgent   string
}

// New creates a pricing service. store may be nil (disables SQLite caching).
func New(store *storage.Store) *Service {
	return newService(store, defaultPrimaryURL, defaultFallbackURL)
}

// NewWithSyncURL creates a pricing service with a custom primary sync URL (for testing).
// The fallback URL is set to the GitHub raw mirror.
func NewWithSyncURL(store *storage.Store, syncURL string) *Service {
	return newService(store, syncURL, defaultFallbackURL)
}

func newService(store *storage.Store, primaryURL, fallbackURL string) *Service {
	s := &Service{
		prices:      make(map[string]PriceEntry, len(staticPrices)),
		store:       store,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
		logger:      slog.Default(),
		primaryURL:  primaryURL,
		fallbackURL: fallbackURL,
		userAgent:   "krouter-pricing-sync/dev",
	}
	for k, v := range staticPrices {
		s.prices[k] = v
	}
	return s
}

// WithVersion sets the daemon version in the User-Agent header so CDN
// access logs can show fleet version distribution.
func (s *Service) WithVersion(v string) *Service {
	if v != "" {
		s.userAgent = "krouter-pricing-sync/" + v
	}
	return s
}

// WithHTTPClient replaces the default HTTP client. Useful for injecting a
// proxy-aware client at daemon startup.
func (s *Service) WithHTTPClient(c *http.Client) *Service {
	s.httpClient = c
	return s
}

// SyncOnceForTest triggers a single sync immediately (test helper).
func (s *Service) SyncOnceForTest(ctx context.Context) {
	s.syncOnce(ctx)
}

// CostFor returns the cost of a completed request in micro-USD (1e6 = $1.00).
//
// inputTokens: fresh tokens (neither cached nor written to cache)
// cachedTokens: cache_read_input_tokens (billed at ~10% of input price)
// cacheWriteTokens: cache_creation_input_tokens (billed at 1.25× input price, 5m TTL)
//
// Returns 0 for unknown models (logged as warning).
func (s *Service) CostFor(provider, model string, inputTokens, outputTokens, cachedTokens, cacheWriteTokens int) int64 {
	s.mu.RLock()
	entry, ok := s.prices[model]
	s.mu.RUnlock()

	if !ok {
		s.logger.Warn("unknown model in pricing lookup; returning 0", "provider", provider, "model", model)
		return 0
	}

	inputTokens, outputTokens, cachedTokens, cacheWriteTokens =
		clampTokens(inputTokens, outputTokens, cachedTokens, cacheWriteTokens)

	// Use the model's actual cache-write rate from the pricing table.
	// Falls back to 0 for providers that don't support prompt caching
	// (e.g. DeepSeek, OpenAI — no cache_creation_input_token_cost in LiteLLM).
	cacheWriteRate := entry.CacheWriteInputCostPerToken

	cost := float64(inputTokens)*entry.InputCostPerToken +
		float64(cachedTokens)*entry.CachedInputCostPerToken +
		float64(cacheWriteTokens)*cacheWriteRate +
		float64(outputTokens)*entry.OutputCostPerToken

	return int64(cost * 1_000_000)
}

// StartSync launches the background 24h sync goroutine.
// Load order on startup:
//  1. Embedded seed (data/token_prices.json via go:embed) — always available offline
//  2. SQLite cache — faster than HTTP, reflects last successful remote sync
//  3. Remote sync (primary CDN → GitHub raw fallback) — picks up updates
//
// Stops when ctx is cancelled.
func (s *Service) StartSync(ctx context.Context, interval time.Duration) {
	// Load embedded seed so the service has complete pricing from the first
	// request, even before the DB or remote sync completes.
	if err := s.loadFromSeed(data.TokenPricesSeedJSON); err != nil {
		s.logger.Warn("pricing: failed to load embedded seed", "err", err)
	}

	// SQLite cache overlays any updates since the last binary release.
	if s.store != nil {
		if err := s.loadFromDB(ctx); err != nil {
			s.logger.Warn("pricing: failed to load from DB cache", "err", err)
		}
	}

	go func() {
		// Initial sync after a short delay so daemon startup isn't blocked.
		t := time.NewTimer(5 * time.Second)
		defer t.Stop()
		tick := time.NewTicker(interval)
		defer tick.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.syncOnce(ctx)
			case <-tick.C:
				s.syncOnce(ctx)
			}
		}
	}()
}

// loadFromDB populates in-memory prices from the SQLite token_price_api table.
func (s *Service) loadFromDB(ctx context.Context) error {
	entries, err := s.store.GetAllPrices(ctx)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range entries {
		s.prices[e.ModelID] = PriceEntry{
			Provider:                       e.Provider,
			InputCostPerToken:              e.InputCostPerToken,
			OutputCostPerToken:             e.OutputCostPerToken,
			CachedInputCostPerToken:        e.CachedInputCostPerToken,
			CacheWriteInputCostPerToken:    e.CacheWriteInputCostPerToken,
			CacheWriteInputCostPerToken1hr: e.CacheWriteInputCostPerToken1hr,
			MaxTokens:                      e.MaxTokens,
		}
	}
	s.logger.Info("pricing: loaded from SQLite cache", "models", len(entries))
	return nil
}

// errNotModified signals a 304 response from tryFetch.
var errNotModified = errors.New("not modified")

// syncOnce fetches token_prices.json from the CDN (primary) or GitHub raw
// (fallback) and updates the in-memory table. Both failures are logged; the
// daemon keeps the last-known-good prices in memory.
func (s *Service) syncOnce(ctx context.Context) {
	s.logger.Info("pricing: syncing")

	body, etag, err := s.tryFetch(ctx, s.primaryURL, "primary")
	if err != nil {
		if errors.Is(err, errNotModified) {
			s.logger.Info("pricing: no changes (304)", "url", "primary")
			return
		}
		s.logger.Warn("pricing: primary failed, trying fallback", "err", err)
		body, etag, err = s.tryFetch(ctx, s.fallbackURL, "fallback")
		if err != nil {
			if errors.Is(err, errNotModified) {
				s.logger.Info("pricing: no changes (304)", "url", "fallback")
				return
			}
			s.logger.Warn("pricing: fallback also failed; keeping existing prices", "err", err)
			return
		}
	}

	updated, sha, err := s.parseKrouterPrices(body)
	if err != nil {
		s.logger.Warn("pricing: failed to parse token_prices.json", "err", err)
		return
	}
	if len(updated) < 50 {
		s.logger.Warn("pricing: suspiciously few models in token_prices.json", "count", len(updated))
		return
	}

	s.mu.Lock()
	for k, v := range updated {
		s.prices[k] = v.PriceEntry
	}
	s.mu.Unlock()

	s.logger.Info("pricing: sync complete", "models", len(updated))

	if s.store != nil {
		now := time.Now().UTC()
		for modelID, entry := range updated {
			_ = s.store.UpsertPrice(ctx, storage.PriceCacheEntry{
				ModelID:                        modelID,
				Provider:                       entry.Provider,
				InputCostPerToken:              entry.InputCostPerToken,
				OutputCostPerToken:             entry.OutputCostPerToken,
				CachedInputCostPerToken:        entry.CachedInputCostPerToken,
				CacheWriteInputCostPerToken:    entry.CacheWriteInputCostPerToken,
				CacheWriteInputCostPerToken1hr: entry.CacheWriteInputCostPerToken1hr,
				MaxTokens:                      entry.MaxTokens,
				RawJSON:                        entry.RawJSON,
				UpdatedAt:                      now,
			})
		}
		_ = s.store.SetSyncMeta(ctx, "last_sync_at", now.Format(time.RFC3339))
		_ = s.store.SetSyncMeta(ctx, "last_sha256", sha)
		if etag != "" {
			_ = s.store.SetSyncMeta(ctx, "last_etag", etag)
		}
	}
}

// tryFetch performs a conditional GET against url. Returns errNotModified on
// 304, an error on non-200, or the body bytes + ETag on success.
func (s *Service) tryFetch(ctx context.Context, url, label string) ([]byte, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	if s.store != nil {
		if etag, _ := s.store.GetSyncMeta(ctx, "last_etag"); etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
	}
	req.Header.Set("User-Agent", s.userAgent)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("%s: %w", label, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		return nil, "", errNotModified
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: HTTP %d", label, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
	if err != nil {
		return nil, "", fmt.Errorf("%s: read body: %w", label, err)
	}
	return body, resp.Header.Get("ETag"), nil
}

// modelEntry is the per-model shape inside token_prices.json.
// Field names match LiteLLM's canonical format exactly so the file
// can be parsed without transformation.
type modelEntry struct {
	InputCostPerToken                   float64 `json:"input_cost_per_token"`
	OutputCostPerToken                  float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost             float64 `json:"cache_read_input_token_cost"`
	CacheCreationInputTokenCost         float64 `json:"cache_creation_input_token_cost"`
	CacheCreationInputTokenCostAbove1hr float64 `json:"cache_creation_input_token_cost_above_1hr"`
	MaxTokens                           int     `json:"max_tokens"`
	Provider                            string  `json:"litellm_provider"`
}

// parsedEntry combines a PriceEntry with the original raw JSON bytes for DB storage.
type parsedEntry struct {
	PriceEntry
	RawJSON string
}

// parseKrouterPrices parses data/token_prices.json (krouter canonical format).
// The file is a thin wrapper around the LiteLLM model map:
//
//	{ "schema_version":1, "source_sha256":"...", "models": { <model_id>: {...} } }
//
// Returns the parsed entries and the source_sha256 field.
// All models are included regardless of whether input_cost_per_token is zero,
// since fields like max_tokens are useful even for free-tier models.
func (s *Service) parseKrouterPrices(body []byte) (map[string]parsedEntry, string, error) {
	var file struct {
		SchemaVersion int                        `json:"schema_version"`
		SourceSHA256  string                     `json:"source_sha256"`
		Models        map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		return nil, "", fmt.Errorf("unmarshal: %w", err)
	}
	if file.SchemaVersion != 1 {
		return nil, "", fmt.Errorf("unsupported schema_version=%d (this build expects 1)", file.SchemaVersion)
	}

	out := make(map[string]parsedEntry, len(file.Models))
	for modelID, entryRaw := range file.Models {
		var e modelEntry
		if err := json.Unmarshal(entryRaw, &e); err != nil {
			continue
		}
		out[modelID] = parsedEntry{
			PriceEntry: PriceEntry{
				Provider:                       e.Provider,
				InputCostPerToken:              e.InputCostPerToken,
				OutputCostPerToken:             e.OutputCostPerToken,
				CachedInputCostPerToken:        e.CacheReadInputTokenCost,
				CacheWriteInputCostPerToken:    e.CacheCreationInputTokenCost,
				CacheWriteInputCostPerToken1hr: e.CacheCreationInputTokenCostAbove1hr,
				MaxTokens:                      e.MaxTokens,
			},
			RawJSON: string(entryRaw),
		}
	}
	return out, file.SourceSHA256, nil
}

// loadFromSeed parses the embedded token_prices.json and populates the
// in-memory price table. Called once at startup before any remote sync.
func (s *Service) loadFromSeed(seedJSON []byte) error {
	entries, sha, err := s.parseKrouterPrices(seedJSON)
	if err != nil {
		return err
	}
	s.mu.Lock()
	for k, v := range entries {
		s.prices[k] = v.PriceEntry
	}
	s.mu.Unlock()
	s.logger.Info("pricing: loaded embedded seed", "models", len(entries), "sha256", sha[:12])
	return nil
}

// InputCostPerToken returns the input cost per token in USD for the given
// model and whether the model is in the pricing table. ok=false means the
// price is unknown; (0, true) means the model is genuinely free — the routing
// engine needs the distinction so free models win cheapest-model scans
// (D-037) while unpriced ones stay unranked.
func (s *Service) InputCostPerToken(model string) (float64, bool) {
	s.mu.RLock()
	e, ok := s.prices[model]
	s.mu.RUnlock()
	if !ok {
		return 0, false
	}
	return e.InputCostPerToken, true
}

// ProviderForModel returns the krouter provider/adapter name associated with a
// model in the pricing table, or "" if the model is unknown. The LiteLLM vendor
// string is mapped to the krouter adapter name where they differ. Used to
// attribute a proxied request to the provider whose key it carries, for lazy
// model discovery.
func (s *Service) ProviderForModel(model string) string {
	s.mu.RLock()
	e, ok := s.prices[model]
	s.mu.RUnlock()
	if !ok {
		return ""
	}
	if mapped, ok := LiteLLMToKrouterProvider[e.Provider]; ok {
		return mapped
	}
	return e.Provider
}

// ModelCount returns the number of models currently in the pricing table.
func (s *Service) ModelCount() int {
	s.mu.RLock()
	n := len(s.prices)
	s.mu.RUnlock()
	return n
}

// PriceFor returns input and output costs per million tokens for the given model.
// Returns (0, 0) for unknown models.
func (s *Service) PriceFor(model string) (inputPerMTok, outputPerMTok float64) {
	s.mu.RLock()
	e, ok := s.prices[model]
	s.mu.RUnlock()
	if !ok {
		return 0, 0
	}
	return e.InputCostPerToken * 1e6, e.OutputCostPerToken * 1e6
}

// CacheReadPerMTok returns the cache-read cost per million tokens for model.
// Returns 0 for models that don't support prompt caching.
func (s *Service) CacheReadPerMTok(model string) float64 {
	s.mu.RLock()
	e, ok := s.prices[model]
	s.mu.RUnlock()
	if !ok {
		return 0
	}
	return e.CachedInputCostPerToken * 1e6
}

// ProviderFor returns the canonical provider name for a given model id —
// i.e. the provider that "owns" the model in the LiteLLM catalogue.
// Returns "" for unknown models. Used by the Router dashboard card to
// show "you asked for claude-sonnet-4-5 (canonical provider: anthropic)
// but krouter routed to glm-4.6 on zai".
func (s *Service) ProviderFor(model string) string {
	s.mu.RLock()
	e, ok := s.prices[model]
	s.mu.RUnlock()
	if !ok {
		return ""
	}
	return e.Provider
}

// BaselineCostFor computes what the request WOULD have cost at the requested
// model's OWN catalog price (micro-USD) — the basis for the savings figure
// (baseline − actual). Returns 0 for a model not in the catalog.
//
// inputTokens: fresh tokens (not cached, not written to cache)
// cachedTokens: cache_read_input_tokens (billed at ~10% of input price)
// cacheWriteTokens: cache_creation_input_tokens (billed at 1.25× input price)
//
// It deliberately does NOT substitute another model's price (it used to fall
// back to claude-sonnet-4-5): comparing the user's actual cost against a model
// they never asked for fabricates a savings number, and is inconsistent with
// PriceFor, which already returns 0 for unknown models (issue #53). Callers
// treat a 0 baseline as "no comparable baseline" — the savings aggregator only
// sums positive (baseline − actual) deltas, so an unknown model contributes no
// fabricated savings.
func (s *Service) BaselineCostFor(requestedModel string, inputTokens, outputTokens, cachedTokens, cacheWriteTokens int) int64 {
	s.mu.RLock()
	entry, ok := s.prices[requestedModel]
	s.mu.RUnlock()
	if !ok {
		return 0
	}
	inputTokens, outputTokens, cachedTokens, cacheWriteTokens =
		clampTokens(inputTokens, outputTokens, cachedTokens, cacheWriteTokens)
	cacheWriteRate := entry.CacheWriteInputCostPerToken
	cost := float64(inputTokens)*entry.InputCostPerToken +
		float64(cachedTokens)*entry.CachedInputCostPerToken +
		float64(cacheWriteTokens)*cacheWriteRate +
		float64(outputTokens)*entry.OutputCostPerToken
	return int64(cost * 1_000_000)
}

// clampTokens floors all token buckets at 0 so malformed upstream usage data
// can never produce a negative cost.
func clampTokens(in, out, cached, cacheWrite int) (int, int, int, int) {
	if in < 0 {
		in = 0
	}
	if out < 0 {
		out = 0
	}
	if cached < 0 {
		cached = 0
	}
	if cacheWrite < 0 {
		cacheWrite = 0
	}
	return in, out, cached, cacheWrite
}
