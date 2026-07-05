package pricing_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/pricing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_NoStore(t *testing.T) {
	svc := pricing.New(nil)
	require.NotNil(t, svc)
}

func TestProviderForModel(t *testing.T) {
	svc := pricing.New(nil)
	assert.Equal(t, "anthropic", svc.ProviderForModel("claude-haiku-4-5"))
	assert.Equal(t, "deepseek", svc.ProviderForModel("deepseek-chat"))
	assert.Equal(t, "openai", svc.ProviderForModel("gpt-4o"))
	assert.Equal(t, "", svc.ProviderForModel("totally-unknown-model"), "unknown model returns empty")
}

func TestCostFor_KnownModel(t *testing.T) {
	svc := pricing.New(nil)
	// claude-sonnet-4-5: $3/M input, $15/M output
	// 1000 fresh input, 200 output, no cache
	// input: 1000 * 3/1e6 * 1e6 = 3000 micro-USD
	// output: 200 * 15/1e6 * 1e6 = 3000 micro-USD
	// total: 6000 micro-USD
	cost := svc.CostFor("anthropic", "claude-sonnet-4-5", 1000, 200, 0, 0)
	assert.Equal(t, int64(6000), cost)
}

func TestCostFor_WithCachedTokens(t *testing.T) {
	svc := pricing.New(nil)
	// 500 fresh input + 500 cache-read, 200 output
	// fresh: 500 * 3/1e6 * 1e6 = 1500
	// cached (read): 500 * 0.3/1e6 * 1e6 = 150
	// output: 200 * 15/1e6 * 1e6 = 3000
	// total: 4650
	cost := svc.CostFor("anthropic", "claude-sonnet-4-5", 500, 200, 500, 0)
	assert.Equal(t, int64(4650), cost)
}

func TestCostFor_WithCacheWriteTokens(t *testing.T) {
	svc := pricing.New(nil)
	// 800 fresh input + 200 cache-write (1.25× surcharge), 100 output
	// fresh: 800 * 3/1e6 * 1e6 = 2400
	// write (1.25×): 200 * 3/1e6 * 1.25 * 1e6 = 750
	// output: 100 * 15/1e6 * 1e6 = 1500
	// total: 4650
	cost := svc.CostFor("anthropic", "claude-sonnet-4-5", 800, 100, 0, 200)
	assert.Equal(t, int64(4650), cost)
}

func TestCostFor_AllBuckets(t *testing.T) {
	svc := pricing.New(nil)
	// 400 fresh + 300 cached-read + 300 cache-write + 100 output
	// fresh: 400 * 3/1e6 * 1e6 = 1200
	// cached read: 300 * 0.3/1e6 * 1e6 = 90
	// cache write (1.25×): 300 * 3/1e6 * 1.25 * 1e6 = 1125
	// output: 100 * 15/1e6 * 1e6 = 1500
	// total: 3915
	cost := svc.CostFor("anthropic", "claude-sonnet-4-5", 400, 100, 300, 300)
	assert.Equal(t, int64(3915), cost)
}

func TestCostFor_UnknownModelReturnsZero(t *testing.T) {
	svc := pricing.New(nil)
	cost := svc.CostFor("anthropic", "gpt-99-ultra", 1000, 200, 0, 0)
	assert.Equal(t, int64(0), cost)
}

func TestCostFor_DeepSeek(t *testing.T) {
	svc := pricing.New(nil)
	// deepseek-chat (LiteLLM 2026-05): $0.28/M input, $0.42/M output
	// 1000 input, 500 output
	// input: 1000 * 0.28/1e6 * 1e6 = 280
	// output: 500 * 0.42/1e6 * 1e6 = 210
	// total: 490
	cost := svc.CostFor("deepseek", "deepseek-chat", 1000, 500, 0, 0)
	assert.Equal(t, int64(490), cost)
}

func TestBaselineCostFor_KnownModel(t *testing.T) {
	svc := pricing.New(nil)
	// Baseline with no cache
	baseline := svc.BaselineCostFor("claude-sonnet-4-5", 1000, 200, 0, 0)
	assert.Equal(t, int64(6000), baseline)
}

func TestBaselineCostFor_WithCacheTokens(t *testing.T) {
	svc := pricing.New(nil)
	// Baseline accounts for cache buckets just like CostFor
	// 500 fresh + 300 cached-read + 200 cache-write, 100 output
	baseline := svc.BaselineCostFor("claude-sonnet-4-5", 500, 100, 300, 200)
	// fresh: 500*3 = 1500, cached: 300*0.3 = 90, write: 200*3.75 = 750, out: 100*15 = 1500 → 3840
	assert.Equal(t, int64(3840), baseline)
}

func TestBaselineCostFor_UnknownModelReturnsZero(t *testing.T) {
	svc := pricing.New(nil)
	// Issue #53: an unknown model has no comparable baseline. We must NOT
	// substitute another model's price (that fabricates savings). Returns 0,
	// consistent with PriceFor("unknown") == (0, 0).
	baseline := svc.BaselineCostFor("gpt-99-ultra", 1000, 200, 0, 0)
	assert.Equal(t, int64(0), baseline)
	in, out := svc.PriceFor("gpt-99-ultra")
	assert.Zero(t, in)
	assert.Zero(t, out)
}

func TestSavings_DeepSeekVsSonnet(t *testing.T) {
	svc := pricing.New(nil)
	// Saver routed 1000/200 tokens to deepseek-chat instead of claude-sonnet-4-5.
	actual := svc.CostFor("deepseek", "deepseek-chat", 1000, 200, 0, 0)
	baseline := svc.BaselineCostFor("claude-sonnet-4-5", 1000, 200, 0, 0)
	saved := baseline - actual
	assert.Greater(t, saved, int64(0), "deepseek should be cheaper than claude-sonnet")
}

func TestParseKrouterPrices_ValidJSON(t *testing.T) {
	// Minimal token_prices.json with 60 model entries in krouter wrapped format.
	models := make(map[string]any)
	for i := 0; i < 60; i++ {
		modelID := "model-" + string(rune('a'+i%26)) + "-" + string(rune('0'+i/26))
		models[modelID] = map[string]any{
			"input_cost_per_token":  0.000003,
			"output_cost_per_token": 0.000015,
			"litellm_provider":      "anthropic",
		}
	}
	file := map[string]any{
		"schema_version": 1,
		"generated_at":   "2026-01-01T00:00:00Z",
		"source_sha256":  "abc123",
		"models":         models,
	}

	body, err := json.Marshal(file)
	require.NoError(t, err)

	// Serve from mock HTTP server.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	svc := pricing.NewWithSyncURL(nil, srv.URL)
	svc.SyncOnceForTest(context.Background())

	// After sync, the models should be in the price table.
	cost := svc.CostFor("anthropic", "model-a-0", 1000, 0, 0, 0)
	// 1000 * 0.000003 * 1e6 = 3000
	assert.Equal(t, int64(3000), cost)
}

func TestCostFor_ProviderQualifiedFallback(t *testing.T) {
	// LiteLLM catalogs non-flagship vendors under "<provider>/<model>"
	// (e.g. "minimax/MiniMax-M3") while agents send the bare model id.
	// The 2026-07-05 field report: MiniMax-M3 requests logged cost_usd=0
	// because the bare id missed the exact-match lookup.
	models := map[string]any{
		"minimax/MiniMax-M3": map[string]any{
			"input_cost_per_token":  0.0000003,
			"output_cost_per_token": 0.0000012,
			"litellm_provider":      "minimax",
		},
	}
	for i := 0; i < 60; i++ { // padding: sync sanity threshold
		models["pad-model-"+string(rune('a'+i%26))+"-"+string(rune('0'+i/26))] = map[string]any{
			"input_cost_per_token":  0.000003,
			"output_cost_per_token": 0.000015,
			"litellm_provider":      "anthropic",
		}
	}
	body, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"generated_at":   "2026-01-01T00:00:00Z",
		"source_sha256":  "abc123",
		"models":         models,
	})
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	svc := pricing.NewWithSyncURL(nil, srv.URL)
	svc.SyncOnceForTest(context.Background())

	// Exact provider-qualified id still works.
	qualified := svc.CostFor("minimax", "minimax/MiniMax-M3", 1000, 1000, 0, 0)
	// 1000*0.3/1e6*1e6 + 1000*1.2/1e6*1e6 = 300 + 1200 (float truncation ±1)
	require.InDelta(t, 1500, qualified, 1)

	// Bare id + provider falls back to the qualified entry.
	bare := svc.CostFor("minimax", "MiniMax-M3", 1000, 1000, 0, 0)
	assert.Equal(t, qualified, bare, "bare model id must fall back to <provider>/<model> lookup")

	// Wrong provider must not accidentally match.
	assert.Equal(t, int64(0), svc.CostFor("deepseek", "MiniMax-M3", 1000, 1000, 0, 0))
}

func TestCostFor_ProviderQualifiedFallback_MappedProviderName(t *testing.T) {
	// krouter adapter names differ from LiteLLM's for a few providers
	// (fireworks → fireworks_ai); the fallback must translate.
	models := map[string]any{
		"fireworks_ai/minimax-m3": map[string]any{
			"input_cost_per_token":  0.0000004,
			"output_cost_per_token": 0.0000016,
			"litellm_provider":      "fireworks_ai",
		},
	}
	for i := 0; i < 60; i++ {
		models["pad-model-"+string(rune('a'+i%26))+"-"+string(rune('0'+i/26))] = map[string]any{
			"input_cost_per_token":  0.000003,
			"output_cost_per_token": 0.000015,
			"litellm_provider":      "anthropic",
		}
	}
	body, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"generated_at":   "2026-01-01T00:00:00Z",
		"source_sha256":  "abc123",
		"models":         models,
	})
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	svc := pricing.NewWithSyncURL(nil, srv.URL)
	svc.SyncOnceForTest(context.Background())

	cost := svc.CostFor("fireworks", "minimax-m3", 1000, 0, 0, 0)
	assert.InDelta(t, 400, cost, 1, "krouter provider name must map to the LiteLLM prefix")
}
