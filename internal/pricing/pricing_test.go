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

func TestParseLiteLLM_ValidJSON(t *testing.T) {
	// Minimal LiteLLM JSON with 60 entries.
	entries := make(map[string]any)
	for i := 0; i < 60; i++ {
		modelID := "model-" + string(rune('a'+i%26)) + "-" + string(rune('0'+i/26))
		entries[modelID] = map[string]any{
			"input_cost_per_token":  0.000003,
			"output_cost_per_token": 0.000015,
			"litellm_provider":      "anthropic",
		}
	}

	body, err := json.Marshal(entries)
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
