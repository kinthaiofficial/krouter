package proxy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/providers"
	anthropicadapter "github.com/kinthaiofficial/krouter/internal/providers/anthropic"
	"github.com/kinthaiofficial/krouter/internal/proxy"
	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type budgetExceededQuota struct{}

func (budgetExceededQuota) CurrentQuota(context.Context) routing.QuotaState {
	return routing.QuotaState{DailyPercent: 1.0}
}

// A budget-exceeded request must still write a durable log row (#66) — the 429
// path previously returned without logging, so the request vanished from the
// dashboards.
func TestBudgetExceeded_WritesLogRow(t *testing.T) {
	store, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	upstream := anthropicUpstream(t) // never reached: budget blocks before forwarding
	reg := providers.New()
	reg.Register(anthropicadapter.New(upstream.URL, upstream.Client()))
	engine := routing.New(reg)
	engine.WithQuota(budgetExceededQuota{})

	srv := proxy.New(
		proxy.WithLogger(logging.New("error")),
		proxy.WithEngine(engine),
		proxy.WithRegistry(reg),
		proxy.WithStore(store),
	)
	recs := make(chan storage.RequestRecord, 2)
	srv.SetOnComplete(func(rec storage.RequestRecord) { recs <- rec })

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	resp := postMessages(t, ts.URL+"/v1/messages", "")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusTooManyRequests, resp.StatusCode)

	select {
	case rec := <-recs:
		assert.Equal(t, http.StatusTooManyRequests, rec.StatusCode, "budget-exceeded must write a 429 log row")
		assert.Equal(t, "claude-sonnet-4-5", rec.RequestedModel)
	case <-time.After(2 * time.Second):
		t.Fatal("no log row written for budget-exceeded request")
	}
}
