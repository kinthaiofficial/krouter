package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetBudget_EmptyDB(t *testing.T) {
	store := openMemStore(t)
	_, ts := newTestServer(t, store)

	resp := doGet(t, ts, "/internal/budget")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(0), body["cost_today_usd"])
	assert.Equal(t, float64(0), body["savings_today_usd"])
	assert.Equal(t, float64(0), body["requests_today"])
	assert.NotEmpty(t, body["date"])
}

func TestGetBudget_NilStore(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/budget")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(0), body["cost_today_usd"])
}

func TestGetBudget_WithTodayRequests(t *testing.T) {
	store := openMemStore(t)
	ctx := context.Background()

	require.NoError(t, store.InsertRequest(ctx, storage.RequestRecord{
		ID:           store.NewULID(),
		Timestamp:    time.Now().UTC(),
		Protocol:     "anthropic",
		Provider:     "anthropic",
		Model:        "claude-haiku-4-5",
		CostMicroUSD: 500_000, // $0.50
	}))

	_, ts := newTestServer(t, store)
	resp := doGet(t, ts, "/internal/budget")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(1), body["requests_today"])
	assert.InDelta(t, 0.5, body["cost_today_usd"], 0.001)
}

func TestGetBudget_RequiresAuth(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp, err := http.Get(ts.URL + "/internal/budget")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetBudget_MethodNotAllowed(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doPost(t, ts, "/internal/budget", "")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestGetBudget_DateField(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/budget")
	defer func() { _ = resp.Body.Close() }()

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	// date must be in YYYY-MM-DD format
	date, ok := body["date"].(string)
	require.True(t, ok)
	_, err := time.Parse("2006-01-02", date)
	assert.NoError(t, err, "date must be YYYY-MM-DD, got %q", date)
}
