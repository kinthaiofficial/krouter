package subpricing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minimalValidPayload is the JSON the test fixtures serve from the fake
// remote URL. Two-tier shape mirroring the real data/token_price_sub.json
// well enough that schema validation passes and the upsert path runs.
const minimalValidPayload = `{
  "schema_version": 1,
  "tiers": [
    {
      "provider": "minimax",
      "tier_pattern": "MiniMax-M*",
      "total_count": 1500,
      "highspeed": false,
      "monthly_price_cny": 49,
      "window_hours": 5,
      "cny_to_usd": 0.138,
      "data_source_url": "https://platform.minimaxi.com/subscribe/token-plan"
    },
    {
      "provider": "minimax",
      "tier_pattern": "MiniMax-M*",
      "total_count": 4500,
      "highspeed": false,
      "monthly_price_cny": 119,
      "window_hours": 5,
      "cny_to_usd": 0.138,
      "data_source_url": "https://platform.minimaxi.com/subscribe/token-plan"
    }
  ]
}`

// urlRewriter routes every request the daemon makes through the local
// httptest server, regardless of which URL the code asked for. Lets us
// keep PrimaryURL / FallbackURL hardcoded in production while still
// driving the fetch path in tests.
type urlRewriter struct{ target string }

func (r urlRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(r.target)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	req.URL.Path = u.Path
	req.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

// urlRouter dispatches based on the original request URL. We need this
// for the fallback test — both PrimaryURL and FallbackURL must reach
// different handlers within the same test.
type urlRouter struct {
	primaryServer  *httptest.Server
	fallbackServer *httptest.Server
}

func (r urlRouter) RoundTrip(req *http.Request) (*http.Response, error) {
	target := r.primaryServer
	if req.URL.Host == "krouter.kinthai.ai" {
		target = r.fallbackServer
	}
	dst, _ := url.Parse(target.URL)
	req.URL.Scheme = dst.Scheme
	req.URL.Host = dst.Host
	req.Host = dst.Host
	return http.DefaultTransport.RoundTrip(req)
}

func newSyncTestStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// ─── SyncOnce happy path ───────────────────────────────────────────────────

func TestSyncOnce_PrimaryHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// First request — no If-None-Match.
		assert.Empty(t, r.Header.Get("If-None-Match"), "first poll should send no ETag")
		w.Header().Set("ETag", `"abc123"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalValidPayload))
	}))
	defer srv.Close()

	store := newSyncTestStore(t)
	svc := New(store, logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})

	require.NoError(t, svc.SyncOnce(context.Background()))

	// Both tier rows landed in token_price_sub.
	for _, total := range []int64{1500, 4500} {
		got, err := store.FindSubscriptionPrice(context.Background(), "minimax", total, false)
		require.NoError(t, err)
		require.NotNil(t, got, "tier %d missing after sync", total)
	}

	// ETag was cached for the next conditional request.
	etag, _ := store.GetSyncMeta(context.Background(), "sub_price_etag")
	assert.Equal(t, `"abc123"`, etag)
}

// ─── 304 cache-hit short-circuit ───────────────────────────────────────────

func TestSyncOnce_NotModifiedNoOp(t *testing.T) {
	upsertCount := atomic.Int32{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Server replies 304 — body never read, upsert never runs.
		if r.Header.Get("If-None-Match") == "" {
			t.Errorf("expected If-None-Match in conditional request, got none")
		}
		w.WriteHeader(http.StatusNotModified)
		upsertCount.Add(1)
	}))
	defer srv.Close()

	store := newSyncTestStore(t)
	// Pre-seed ETag so the very first SyncOnce sends If-None-Match.
	require.NoError(t, store.SetSyncMeta(context.Background(), "sub_price_etag", `"old-etag"`))

	svc := New(store, logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})

	require.NoError(t, svc.SyncOnce(context.Background()))

	// No tier rows should have been written.
	got, _ := store.FindSubscriptionPrice(context.Background(), "minimax", 1500, false)
	assert.Nil(t, got, "304 path must not write rows")
}

// ─── Primary fails, fallback succeeds ──────────────────────────────────────

func TestSyncOnce_FallsBackToSecondary(t *testing.T) {
	// Primary errors out (we explicitly close the connection).
	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hj, ok := w.(http.Hijacker)
		require.True(t, ok)
		c, _, _ := hj.Hijack()
		_ = c.Close()
	}))
	defer primarySrv.Close()

	fallbackHits := atomic.Int32{}
	fallbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fallbackHits.Add(1)
		w.Header().Set("ETag", `"fb-1"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalValidPayload))
	}))
	defer fallbackSrv.Close()

	store := newSyncTestStore(t)
	svc := New(store, logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRouter{
			primaryServer:  primarySrv,
			fallbackServer: fallbackSrv,
		}})

	require.NoError(t, svc.SyncOnce(context.Background()))

	assert.Equal(t, int32(1), fallbackHits.Load(), "fallback must have been hit exactly once")

	// Rows landed; ETag stored for fallback URL.
	got, _ := store.FindSubscriptionPrice(context.Background(), "minimax", 1500, false)
	assert.NotNil(t, got)
}

// ─── Both endpoints fail ───────────────────────────────────────────────────

func TestSyncOnce_BothEndpointsFailReturnsError(t *testing.T) {
	deadSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer deadSrv.Close()

	store := newSyncTestStore(t)
	svc := New(store, logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRouter{
			primaryServer:  deadSrv,
			fallbackServer: deadSrv,
		}})

	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "primary + fallback both failed")

	// No rows written; daemon keeps last-known-good prices.
	got, _ := store.FindSubscriptionPrice(context.Background(), "minimax", 1500, false)
	assert.Nil(t, got)
}

// ─── Schema validation guards ──────────────────────────────────────────────

func TestSyncOnce_RejectsBadSchemaVersion(t *testing.T) {
	body := `{"schema_version": 99, "tiers": []}`
	svc := serviceWithBody(t, body, http.StatusOK)
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported schema_version")
}

func TestSyncOnce_RejectsEmptyTiers(t *testing.T) {
	body := `{"schema_version": 1, "tiers": []}`
	svc := serviceWithBody(t, body, http.StatusOK)
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no tiers in payload")
}

func TestSyncOnce_RejectsNegativePrice(t *testing.T) {
	body := `{
		"schema_version": 1,
		"tiers": [{
			"provider": "minimax", "tier_pattern": "MiniMax-M*",
			"total_count": 1500, "highspeed": false,
			"monthly_price_cny": -10,
			"window_hours": 5, "cny_to_usd": 0.138
		}]
	}`
	svc := serviceWithBody(t, body, http.StatusOK)
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "outside sane range")
}

func TestSyncOnce_RejectsZeroTotalCount(t *testing.T) {
	body := `{
		"schema_version": 1,
		"tiers": [{
			"provider": "minimax", "tier_pattern": "MiniMax-M*",
			"total_count": 0, "highspeed": false,
			"monthly_price_cny": 49,
			"window_hours": 5, "cny_to_usd": 0.138
		}]
	}`
	svc := serviceWithBody(t, body, http.StatusOK)
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "total_count must be positive")
}

func TestSyncOnce_RejectsMalformedJSON(t *testing.T) {
	svc := serviceWithBody(t, `not json at all`, http.StatusOK)
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse json")
}

// ─── onUpdate callback ─────────────────────────────────────────────────────

func TestSyncOnce_FiresOnUpdateCallbackWithCount(t *testing.T) {
	called := atomic.Int32{}
	gotCount := atomic.Int32{}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalValidPayload))
	}))
	defer srv.Close()

	store := newSyncTestStore(t)
	svc := New(store, logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}}).
		WithUpdateCallback(func(c int) {
			called.Add(1)
			gotCount.Store(int32(c))
		})

	require.NoError(t, svc.SyncOnce(context.Background()))
	assert.Equal(t, int32(1), called.Load(), "callback should fire exactly once")
	assert.Equal(t, int32(2), gotCount.Load(), "callback should receive the row count")
}

func TestSyncOnce_OnUpdateDoesNotFireOn304(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	store := newSyncTestStore(t)
	require.NoError(t, store.SetSyncMeta(context.Background(), "sub_price_etag", `"x"`))

	called := atomic.Int32{}
	svc := New(store, logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}}).
		WithUpdateCallback(func(int) { called.Add(1) })

	require.NoError(t, svc.SyncOnce(context.Background()))
	assert.Equal(t, int32(0), called.Load(), "304 cache hit must not fire onUpdate")
}

// ─── User-Agent / WithVersion ──────────────────────────────────────────────

func TestSyncOnce_SendsVersionedUserAgent(t *testing.T) {
	gotUA := atomic.Value{}
	gotUA.Store("")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA.Store(r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalValidPayload))
	}))
	defer srv.Close()

	store := newSyncTestStore(t)
	svc := New(store, logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}}).
		WithVersion("v2.2.0")

	require.NoError(t, svc.SyncOnce(context.Background()))
	assert.Equal(t, "krouter-subpricing-sync/v2.2.0", gotUA.Load(),
		"UA must include the version so access logs can break down by fleet version")
}

func TestSyncOnce_DefaultUserAgentIsDev(t *testing.T) {
	gotUA := atomic.Value{}
	gotUA.Store("")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA.Store(r.Header.Get("User-Agent"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalValidPayload))
	}))
	defer srv.Close()

	svc := New(newSyncTestStore(t), logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})
	// No WithVersion call → expect the dev default.

	require.NoError(t, svc.SyncOnce(context.Background()))
	assert.Equal(t, "krouter-subpricing-sync/dev", gotUA.Load())
}

func TestWithVersion_EmptyStringPreservesDefault(t *testing.T) {
	svc := New(newSyncTestStore(t), logging.New("error")).
		WithVersion("")
	assert.Equal(t, "krouter-subpricing-sync/dev", svc.userAgent)
}

// ─── StartSync lifecycle ───────────────────────────────────────────────────

func TestStartSync_ZeroIntervalReturnsImmediately(t *testing.T) {
	// Build the service (opens a store) OUTSIDE the timed goroutine — only the
	// interval<=0 early-return should be measured. Store setup in the timed
	// path flaked the 200ms budget on slow/cold CI runners.
	svc := New(newSyncTestStore(t), logging.New("error"))
	done := make(chan struct{})
	go func() {
		svc.StartSync(context.Background(), 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("StartSync with interval=0 did not return immediately")
	}
}

func TestStartSync_StopsOnCtxCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(minimalValidPayload))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		New(newSyncTestStore(t), logging.New("error")).
			WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}}).
			StartSync(ctx, 50*time.Millisecond)
		close(done)
	}()

	// The first sync is delayed 30s in StartSync. Cancelling well before
	// that should still let the loop exit promptly via the select on
	// ctx.Done.
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("StartSync did not return after ctx cancel")
	}
}

// ─── Helper used by validation tests ───────────────────────────────────────

func serviceWithBody(t *testing.T, body string, status int) *Service {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return New(newSyncTestStore(t), logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})
}
