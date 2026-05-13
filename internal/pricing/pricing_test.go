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

func TestCostFor_KnownModel(t *testing.T) {
	svc := pricing.New(nil)
	// claude-sonnet-4-5: $3/M input, $15/M output
	// 1000 input, 200 output → (1000*3/1e6 + 200*15/1e6) * 1e6 = 3000 + 3000 = 6000 micro-USD... wait
	// 1000 * 0.000003 = 0.003 USD * 1e6 = 3000 micro-USD input
	// 200 * 0.000015 = 0.003 USD * 1e6 = 3000 micro-USD output
	// total: 6000 micro-USD
	cost := svc.CostFor("anthropic", "claude-sonnet-4-5", 1000, 200, 0)
	assert.Equal(t, int64(6000), cost)
}

func TestCostFor_WithCachedTokens(t *testing.T) {
	svc := pricing.New(nil)
	// 500 regular input + 500 cached, 200 output
	// regular: 500 * 0.000003 * 1e6 = 1500
	// cached: 500 * 0.0000003 * 1e6 = 150
	// output: 200 * 0.000015 * 1e6 = 3000
	// total: 4650
	cost := svc.CostFor("anthropic", "claude-sonnet-4-5", 1000, 200, 500)
	assert.Equal(t, int64(4650), cost)
}

func TestCostFor_UnknownModelReturnsZero(t *testing.T) {
	svc := pricing.New(nil)
	cost := svc.CostFor("anthropic", "gpt-99-ultra", 1000, 200, 0)
	assert.Equal(t, int64(0), cost)
}

func TestCostFor_DeepSeek(t *testing.T) {
	svc := pricing.New(nil)
	// deepseek-chat: $0.14/M input, $0.28/M output
	// 1000 input, 500 output
	// input: 1000 * 0.00000014 * 1e6 = 140
	// output: 500 * 0.00000028 * 1e6 = 140
	// total: 280
	cost := svc.CostFor("deepseek", "deepseek-chat", 1000, 500, 0)
	assert.Equal(t, int64(280), cost)
}

func TestBaselineCostFor_KnownModel(t *testing.T) {
	svc := pricing.New(nil)
	baseline := svc.BaselineCostFor("claude-sonnet-4-5", 1000, 200)
	// Same as non-cached CostFor
	assert.Equal(t, int64(6000), baseline)
}

func TestBaselineCostFor_UnknownModelUsesSonnet(t *testing.T) {
	svc := pricing.New(nil)
	// Unknown model falls back to claude-sonnet-4-5 pricing.
	baseline := svc.BaselineCostFor("gpt-99-ultra", 1000, 200)
	assert.Equal(t, int64(6000), baseline) // claude-sonnet-4-5 fallback
}

func TestSavings_DeepSeekVsSonnet(t *testing.T) {
	svc := pricing.New(nil)
	// Saver routed 1000/200 tokens to deepseek-chat instead of claude-sonnet-4-5.
	actual := svc.CostFor("deepseek", "deepseek-chat", 1000, 200, 0)
	baseline := svc.BaselineCostFor("claude-sonnet-4-5", 1000, 200)
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
	cost := svc.CostFor("anthropic", "model-a-0", 1000, 0, 0)
	// 1000 * 0.000003 * 1e6 = 3000
	assert.Equal(t, int64(3000), cost)
}
