package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestProviderModels_ReadsFromTokenPriceAPI is the regression for the v2.3.0
// bug: the handler previously read from model_catalog (never populated by
// the daemon's normal sync flow), so every call returned []. The fix
// reads from token_price_api which the LiteLLM sync writes to.
func TestProviderModels_ReadsFromTokenPriceAPI(t *testing.T) {
	srv, store := newFPTestServer(t)
	ctx := t.Context()
	now := time.Now().UTC()

	seed := []storage.PriceCacheEntry{
		{ModelID: "claude-sonnet-4-5", Provider: "anthropic", InputCostPerToken: 0.000003, OutputCostPerToken: 0.000015, MaxTokens: 200000, UpdatedAt: now},
		{ModelID: "claude-haiku-4-5", Provider: "anthropic", InputCostPerToken: 0.0000008, OutputCostPerToken: 0.000004, MaxTokens: 200000, UpdatedAt: now},
		{ModelID: "gpt-4o", Provider: "openai", InputCostPerToken: 0.0000025, OutputCostPerToken: 0.00001, MaxTokens: 128000, UpdatedAt: now},
	}
	for _, e := range seed {
		require.NoError(t, store.UpsertPrice(ctx, e))
	}

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, fpAuthedReq(t, http.MethodGet, "/internal/providers/anthropic/models"))
	require.Equal(t, http.StatusOK, w.Code)

	var got []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 2, "anthropic should expose two models from the price cache")

	// Stable sort: claude-haiku-4-5 < claude-sonnet-4-5 alphabetically.
	assert.Equal(t, "claude-haiku-4-5", got[0]["model_id"])
	assert.Equal(t, "claude-sonnet-4-5", got[1]["model_id"])

	// Per-million-token pricing computed from the per-token prices.
	assert.InDelta(t, 0.80, got[0]["input_per_mtok"], 0.0001, "0.0000008 × 1M = $0.80/M")
	assert.InDelta(t, 3.0, got[1]["input_per_mtok"], 0.0001, "0.000003 × 1M = $3.00/M")
	assert.InDelta(t, 15.0, got[1]["output_per_mtok"], 0.0001, "0.000015 × 1M = $15.00/M")
	assert.Equal(t, float64(200000), got[1]["max_tokens"])
}

func TestProviderModels_UnknownProviderReturnsEmpty(t *testing.T) {
	srv, store := newFPTestServer(t)
	ctx := t.Context()
	require.NoError(t, store.UpsertPrice(ctx, storage.PriceCacheEntry{
		ModelID: "gpt-4o", Provider: "openai", UpdatedAt: time.Now().UTC(),
	}))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, fpAuthedReq(t, http.MethodGet, "/internal/providers/nonexistent-xyz/models"))
	require.Equal(t, http.StatusOK, w.Code)

	var got []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Equal(t, []map[string]any{}, got, "unknown provider returns empty list, not 404")
}
