package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubPoller is a no-network subscriptionPoller used by refresh tests.
type stubPoller struct {
	calls atomic.Int32
	err   error
}

func (p *stubPoller) PollOnce(_ context.Context) error {
	p.calls.Add(1)
	return p.err
}

func TestSubscriptionStatus_EmptyWhenNothingPolledYet(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodGet, "/internal/subscription/status", ""))
	require.Equal(t, http.StatusOK, w.Code)

	var got []subscriptionProviderJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Empty(t, got)
}

func TestSubscriptionStatus_AggregatesTiersAndEffectiveCost(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	now := time.Now().UTC()
	windowStart := now.Add(-30 * time.Minute)
	windowEnd := now.Add(4*time.Hour + 30*time.Minute)

	// Seed token_price_sub with the ¥49/1500 standard tier so the lookup
	// inside tiersToJSON finds it. In production the installer seeds this
	// table from data/token_price_sub.json; tests do it manually.
	require.NoError(t, store.UpsertSubscriptionPrice(ctx, storage.SubscriptionPrice{
		Provider:        "minimax",
		TierPattern:     "MiniMax-M*",
		TotalCount:      1500,
		Highspeed:       false,
		MonthlyPriceCNY: 49,
		WindowHours:     5,
		CNYToUSD:        0.138,
		UpdatedAt:       now,
	}))

	require.NoError(t, store.UpsertSubscriptionQuota(ctx, storage.SubscriptionQuota{
		Provider:     "minimax",
		ModelPattern: "MiniMax-M*",
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		TotalCount:   1500,
		UsedCount:    21,
		Highspeed:    false,
		FetchedAt:    now,
	}))
	require.NoError(t, store.UpsertSubscriptionQuota(ctx, storage.SubscriptionQuota{
		Provider:     "minimax",
		ModelPattern: "speech-hd",
		WindowStart:  windowStart,
		WindowEnd:    windowEnd,
		TotalCount:   4000,
		UsedCount:    100,
		Highspeed:    false,
		FetchedAt:    now,
	}))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodGet, "/internal/subscription/status", ""))
	require.Equal(t, http.StatusOK, w.Code)

	var got []subscriptionProviderJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "minimax", got[0].Provider)
	require.Len(t, got[0].Tiers, 2)

	// Tiers sorted by ModelPattern; MiniMax-M* < speech-hd alphabetically.
	mTier := got[0].Tiers[0]
	assert.Equal(t, "MiniMax-M*", mTier.TierName)
	assert.Equal(t, int64(1500), mTier.Total)
	assert.Equal(t, int64(1479), mTier.Remaining)

	// effective_cost is derived from the SubscriptionPrice row we seeded above:
	//   ¥49 × 0.138 / (1500 × 144) ≈ $0.0000313/call
	// where 144 = windows_per_month = (30 days × 24h) / 5h.
	wantEffective := 49.0 * 0.138 / (1500.0 * 144.0)
	assert.InDelta(t, wantEffective, mTier.EffectiveCostPerCallUSD, 1e-9)

	// MonthlyPriceCNY is the original sticker price; MonthlyPriceUSD is
	// the same number normalised at the fixed CNY→USD rate (see
	// storage.subCNYToUSD): ¥49 × 0.138 ≈ $6.762.
	assert.InDelta(t, 49.0, mTier.MonthlyPriceCNY, 1e-9)
	// MonthlyPriceUSD is the CNY sticker price normalised at cny_to_usd: ¥49 × 0.138 ≈ $6.762.
	assert.InDelta(t, 49.0*0.138, mTier.MonthlyPriceUSD, 1e-9)

	// speech-hd has no token_price_sub row → effective cost 0, monthly price 0.
	speechTier := got[0].Tiers[1]
	assert.Equal(t, "speech-hd", speechTier.TierName)
	assert.Equal(t, 0.0, speechTier.EffectiveCostPerCallUSD)
}

func TestSubscriptionStatus_ReportsAuthSourceFromInheritedEndpoints(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{
			Provider:    "minimax-portal",
			EndpointURL: "u",
			ExtrasJSON:  `{"oauth_token":"sk-cp-XXX"}`,
			CapturedAt:  1,
		},
	}))

	now := time.Now().UTC()
	require.NoError(t, store.UpsertSubscriptionQuota(ctx, storage.SubscriptionQuota{
		Provider: "minimax", ModelPattern: "MiniMax-M*",
		WindowStart: now, WindowEnd: now.Add(time.Hour),
		TotalCount: 1500, UsedCount: 0, FetchedAt: now,
	}))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodGet, "/internal/subscription/status", ""))
	require.Equal(t, http.StatusOK, w.Code)

	var got []subscriptionProviderJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "openclaw", got[0].SourceAgent)
	assert.True(t, got[0].OAuthPresent)
}

func TestSubscriptionRefresh_InvokesPoller(t *testing.T) {
	srv, _ := newTestServer(t)
	stub := &stubPoller{}
	srv.SetMinimaxPoller(stub)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodPost, "/internal/subscription/refresh", `{}`))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, int32(1), stub.calls.Load(), "POST refresh should invoke poller exactly once")
}

func TestSubscriptionRefresh_PollerErrorStillReturnsStatus(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.SetMinimaxPoller(&stubPoller{err: errors.New("network down")})

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodPost, "/internal/subscription/refresh", `{}`))

	// Refresh path always returns the current state — a transient poll error
	// should not 500 the request.
	require.Equal(t, http.StatusOK, w.Code)
}

func TestSubscriptionStatus_RejectsNonGet(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodPut, "/internal/subscription/status", ""))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
