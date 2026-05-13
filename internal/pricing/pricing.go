// Package pricing maintains LLM pricing data and computes per-request costs.
//
// Three-layer architecture (see spec/04-pricing.md):
//   - Layer 1: Static fallback bundled at compile time
//   - Layer 2: Live sync from LiteLLM JSON every 24h
//   - Layer 3: Per-request cost accounting
package pricing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
)

const defaultLiteLLMURL = "https://raw.githubusercontent.com/BerriAI/litellm/main/model_prices_and_context_window.json"

// PriceEntry holds per-token costs for a model.
type PriceEntry struct {
	Provider                string
	InputCostPerToken       float64 // cost per single token in USD
	OutputCostPerToken      float64
	CachedInputCostPerToken float64
	MaxTokens               int
}

// staticPrices is the Layer-1 fallback bundled at compile time.
// Values are cost-per-token in USD (divide $N/M by 1_000_000).
var staticPrices = map[string]PriceEntry{
	// Anthropic
	"claude-opus-4-7":          {Provider: "anthropic", InputCostPerToken: 15.0 / 1e6, OutputCostPerToken: 75.0 / 1e6, CachedInputCostPerToken: 1.5 / 1e6},
	"claude-sonnet-4-6":        {Provider: "anthropic", InputCostPerToken: 3.0 / 1e6, OutputCostPerToken: 15.0 / 1e6, CachedInputCostPerToken: 0.3 / 1e6},
	"claude-opus-4-5":          {Provider: "anthropic", InputCostPerToken: 15.0 / 1e6, OutputCostPerToken: 75.0 / 1e6, CachedInputCostPerToken: 1.5 / 1e6},
	"claude-sonnet-4-5":        {Provider: "anthropic", InputCostPerToken: 3.0 / 1e6, OutputCostPerToken: 15.0 / 1e6, CachedInputCostPerToken: 0.3 / 1e6},
	"claude-haiku-4-5":         {Provider: "anthropic", InputCostPerToken: 0.8 / 1e6, OutputCostPerToken: 4.0 / 1e6, CachedInputCostPerToken: 0.08 / 1e6},
	"claude-haiku-4-5-20251001": {Provider: "anthropic", InputCostPerToken: 0.8 / 1e6, OutputCostPerToken: 4.0 / 1e6, CachedInputCostPerToken: 0.08 / 1e6},
	"claude-3-5-sonnet-20241022": {Provider: "anthropic", InputCostPerToken: 3.0 / 1e6, OutputCostPerToken: 15.0 / 1e6, CachedInputCostPerToken: 0.3 / 1e6},
	"claude-3-5-haiku-20241022": {Provider: "anthropic", InputCostPerToken: 0.8 / 1e6, OutputCostPerToken: 4.0 / 1e6, CachedInputCostPerToken: 0.08 / 1e6},
	"claude-3-opus-20240229":    {Provider: "anthropic", InputCostPerToken: 15.0 / 1e6, OutputCostPerToken: 75.0 / 1e6, CachedInputCostPerToken: 1.5 / 1e6},
	// DeepSeek
	"deepseek-chat":  {Provider: "deepseek", InputCostPerToken: 0.14 / 1e6, OutputCostPerToken: 0.28 / 1e6},
	"deepseek-coder": {Provider: "deepseek", InputCostPerToken: 0.14 / 1e6, OutputCostPerToken: 0.28 / 1e6},
	// OpenAI
	"gpt-4o":          {Provider: "openai", InputCostPerToken: 2.5 / 1e6, OutputCostPerToken: 10.0 / 1e6},
	"gpt-4o-mini":     {Provider: "openai", InputCostPerToken: 0.15 / 1e6, OutputCostPerToken: 0.6 / 1e6},
	"gpt-4-turbo":     {Provider: "openai", InputCostPerToken: 10.0 / 1e6, OutputCostPerToken: 30.0 / 1e6},
	"gpt-3.5-turbo":   {Provider: "openai", InputCostPerToken: 0.5 / 1e6, OutputCostPerToken: 1.5 / 1e6},
}

// Service maintains the LLM pricing table and computes costs.
type Service struct {
	mu         sync.RWMutex
	prices     map[string]PriceEntry // model_id → PriceEntry (live, may be updated by sync)
	store      *storage.Store
	httpClient *http.Client
	logger     *slog.Logger
	syncURL    string
}

// New creates a pricing service. store may be nil (disables SQLite caching).
func New(store *storage.Store) *Service {
	return NewWithSyncURL(store, defaultLiteLLMURL)
}

// NewWithSyncURL creates a pricing service with a custom sync URL (for testing).
func NewWithSyncURL(store *storage.Store, syncURL string) *Service {
	s := &Service{
		prices:     make(map[string]PriceEntry, len(staticPrices)),
		store:      store,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		logger:     slog.Default(),
		syncURL:    syncURL,
	}
	for k, v := range staticPrices {
		s.prices[k] = v
	}
	return s
}

// SyncOnceForTest triggers a single sync immediately (test helper).
func (s *Service) SyncOnceForTest(ctx context.Context) {
	s.syncOnce(ctx)
}

// CostFor returns the cost of a completed request in micro-USD (1e6 = $1.00).
// Returns 0 for unknown models (logged as warning).
func (s *Service) CostFor(provider, model string, inputTokens, outputTokens, cachedTokens int) int64 {
	s.mu.RLock()
	entry, ok := s.prices[model]
	s.mu.RUnlock()

	if !ok {
		s.logger.Warn("unknown model in pricing lookup; returning 0", "provider", provider, "model", model)
		return 0
	}

	regularInput := inputTokens - cachedTokens
	if regularInput < 0 {
		regularInput = 0
	}

	cost := float64(regularInput)*entry.InputCostPerToken +
		float64(cachedTokens)*entry.CachedInputCostPerToken +
		float64(outputTokens)*entry.OutputCostPerToken

	return int64(cost * 1_000_000)
}

// StartSync launches the background 24h LiteLLM sync goroutine.
// It runs an initial sync immediately (on a separate goroutine so Serve is not blocked),
// then repeats every interval. Stops when ctx is cancelled.
func (s *Service) StartSync(ctx context.Context, interval time.Duration) {
	// Load from SQLite cache first (faster than HTTP).
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

// loadFromDB populates in-memory prices from the SQLite pricing_cache.
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
			Provider:                e.Provider,
			InputCostPerToken:       e.InputCostPerToken,
			OutputCostPerToken:      e.OutputCostPerToken,
			CachedInputCostPerToken: e.CachedInputCostPerToken,
			MaxTokens:               e.MaxTokens,
		}
	}
	s.logger.Info("pricing: loaded from SQLite cache", "models", len(entries))
	return nil
}

// syncOnce fetches the LiteLLM JSON and updates the in-memory table.
func (s *Service) syncOnce(ctx context.Context) {
	s.logger.Info("pricing: syncing from LiteLLM")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.syncURL, nil)
	if err != nil {
		s.logger.Warn("pricing: failed to build sync request", "err", err)
		return
	}

	// Set If-None-Match if we have a cached ETag.
	if s.store != nil {
		if etag, _ := s.store.GetSyncMeta(ctx, "last_etag"); etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Warn("pricing: sync request failed", "err", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		s.logger.Info("pricing: no changes (304)")
		return
	}
	if resp.StatusCode != http.StatusOK {
		s.logger.Warn("pricing: unexpected status", "code", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 50*1024*1024)) // 50MB max
	if err != nil {
		s.logger.Warn("pricing: failed to read sync response", "err", err)
		return
	}

	// Sanity check: must be large enough to be a real LiteLLM JSON.
	hash := fmt.Sprintf("%x", sha256.Sum256(body))

	updated, err := s.parseLiteLLM(body)
	if err != nil {
		s.logger.Warn("pricing: failed to parse LiteLLM JSON", "err", err)
		return
	}
	if len(updated) < 50 {
		s.logger.Warn("pricing: suspiciously few models in LiteLLM JSON", "count", len(updated))
		return
	}

	// Merge into in-memory table (don't delete existing entries not in new data).
	s.mu.Lock()
	for k, v := range updated {
		s.prices[k] = v
	}
	s.mu.Unlock()

	s.logger.Info("pricing: sync complete", "models", len(updated))

	// Persist to SQLite and update sync meta.
	if s.store != nil {
		now := time.Now().UTC()
		for modelID, entry := range updated {
			_ = s.store.UpsertPrice(ctx, storage.PriceCacheEntry{
				ModelID:                 modelID,
				Provider:                entry.Provider,
				InputCostPerToken:       entry.InputCostPerToken,
				OutputCostPerToken:      entry.OutputCostPerToken,
				CachedInputCostPerToken: entry.CachedInputCostPerToken,
				MaxTokens:               entry.MaxTokens,
				UpdatedAt:               now,
			})
		}
		_ = s.store.SetSyncMeta(ctx, "last_sync_at", now.Format(time.RFC3339))
		_ = s.store.SetSyncMeta(ctx, "last_sha256", hash)
		_ = s.store.SetSyncMeta(ctx, "source_url", s.syncURL)
		if etag := resp.Header.Get("ETag"); etag != "" {
			_ = s.store.SetSyncMeta(ctx, "last_etag", etag)
		}
	}
}

// liteLLMEntry is the parsed shape of a single entry from the LiteLLM JSON.
type liteLLMEntry struct {
	InputCostPerToken       float64 `json:"input_cost_per_token"`
	OutputCostPerToken      float64 `json:"output_cost_per_token"`
	CacheReadInputTokenCost float64 `json:"cache_read_input_token_cost"`
	MaxTokens               int     `json:"max_tokens"`
	Provider                string  `json:"litellm_provider"`
}

// parseLiteLLM parses the top-level map from LiteLLM JSON.
// Returns only entries that have non-zero input cost.
func (s *Service) parseLiteLLM(data []byte) (map[string]PriceEntry, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}

	out := make(map[string]PriceEntry, len(raw))
	for modelID, entryRaw := range raw {
		var e liteLLMEntry
		if err := json.Unmarshal(entryRaw, &e); err != nil {
			continue // skip malformed entries
		}
		if e.InputCostPerToken == 0 {
			continue // skip free/unknown models without pricing
		}
		out[modelID] = PriceEntry{
			Provider:                e.Provider,
			InputCostPerToken:       e.InputCostPerToken,
			OutputCostPerToken:      e.OutputCostPerToken,
			CachedInputCostPerToken: e.CacheReadInputTokenCost,
			MaxTokens:               e.MaxTokens,
		}
	}
	return out, nil
}

// BaselineCostFor computes the cost of a request using the "balanced" baseline
// (i.e., as if the user's requested model had been used at Anthropic pricing).
// Used for savings computation in the usage API.
func (s *Service) BaselineCostFor(requestedModel string, inputTokens, outputTokens int) int64 {
	// Try the requested model directly; fall back to a known sonnet price.
	s.mu.RLock()
	entry, ok := s.prices[requestedModel]
	s.mu.RUnlock()
	if !ok {
		// Unknown requested model: use claude-sonnet-4-5 as baseline.
		s.mu.RLock()
		entry, ok = s.prices["claude-sonnet-4-5"]
		s.mu.RUnlock()
		if !ok {
			return 0
		}
	}
	cost := float64(inputTokens)*entry.InputCostPerToken +
		float64(outputTokens)*entry.OutputCostPerToken
	return int64(cost * 1_000_000)
}
