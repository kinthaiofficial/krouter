package minimax

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestQuotaPoller_DefaultResolverUsesRequestCache(t *testing.T) {
	// Set the in-memory cache via the existing API, simulating that a real
	// proxied request just went through.
	CacheOAuthToken("Bearer sk-from-request-cache")
	t.Cleanup(func() { CacheOAuthToken("") }) // reset between tests

	poller := NewQuotaPoller(openTestStore(t), nil)
	// We don't call PollOnce against a live network; we just confirm the
	// resolver reads from GetCachedToken by exercising it directly.
	assert.Equal(t, "sk-from-request-cache", poller.resolver(context.Background()))
}

func TestQuotaPoller_WithTokenResolverOverridesDefault(t *testing.T) {
	CacheOAuthToken("Bearer sk-from-request-cache")
	t.Cleanup(func() { CacheOAuthToken("") })

	poller := NewQuotaPoller(openTestStore(t), nil).
		WithTokenResolver(func(_ context.Context) string { return "sk-custom-INJECTED" })

	assert.Equal(t, "sk-custom-INJECTED", poller.resolver(context.Background()),
		"custom resolver should take precedence over the request cache")
}

func TestQuotaPoller_WithTokenResolverNilKeepsDefault(t *testing.T) {
	CacheOAuthToken("Bearer sk-original")
	t.Cleanup(func() { CacheOAuthToken("") })

	poller := NewQuotaPoller(openTestStore(t), nil).WithTokenResolver(nil)
	assert.Equal(t, "sk-original", poller.resolver(context.Background()),
		"WithTokenResolver(nil) should keep the existing resolver")
}

func TestShouldFireExhaust(t *testing.T) {
	window1End := time.Date(2026, 5, 22, 20, 0, 0, 0, time.UTC)
	window2End := time.Date(2026, 5, 23, 1, 0, 0, 0, time.UTC)

	// Helper to construct a tier with given remaining at a window.
	mk := func(used, total int64, windowEnd time.Time) storage.SubscriptionQuota {
		return storage.SubscriptionQuota{
			Provider:     "minimax",
			ModelPattern: "MiniMax-M*",
			TotalCount:   total,
			UsedCount:    used,
			WindowEnd:    windowEnd,
		}
	}

	cases := []struct {
		name string
		old  *storage.SubscriptionQuota
		new  storage.SubscriptionQuota
		fire bool
	}{
		{
			name: "fresh exhaustion in same window — most common case",
			old:  ptrQ(mk(1400, 1500, window1End)), // 100 remaining
			new:  mk(1500, 1500, window1End),       // 0 remaining
			fire: true,
		},
		{
			name: "still has quota — must not fire",
			old:  ptrQ(mk(1400, 1500, window1End)),
			new:  mk(1450, 1500, window1End), // 50 remaining
			fire: false,
		},
		{
			name: "first observation, already at zero — fire (let UI catch up)",
			old:  nil,
			new:  mk(1500, 1500, window1End),
			fire: true,
		},
		{
			name: "same window, both exhausted — dedupe, don't refire",
			old:  ptrQ(mk(1500, 1500, window1End)),
			new:  mk(1500, 1500, window1End),
			fire: false,
		},
		{
			name: "window rolled over, fresh window already exhausted (rare) — fire",
			old:  ptrQ(mk(1500, 1500, window1End)),
			new:  mk(1500, 1500, window2End),
			fire: true,
		},
		{
			name: "window rolled over, fresh window has quota — must not fire",
			old:  ptrQ(mk(1500, 1500, window1End)),
			new:  mk(0, 1500, window2End),
			fire: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.fire, shouldFireExhaust(tc.old, tc.new))
		})
	}
}

func ptrQ(q storage.SubscriptionQuota) *storage.SubscriptionQuota { return &q }

func TestQuotaPoller_WithExhaustCallback_Lifecycle(t *testing.T) {
	poller := NewQuotaPoller(openTestStore(t), nil)
	// Default: no callback installed → onExhaust is nil → PollOnce won't dereference it.
	assert.Nil(t, poller.onExhaust)

	called := 0
	poller.WithExhaustCallback(func(_, _ string, _ bool, _ time.Time) { called++ })
	require.NotNil(t, poller.onExhaust)

	poller.WithExhaustCallback(nil)
	require.Nil(t, poller.onExhaust, "passing nil should clear the callback")

	_ = called // silence linter; assertion above proves the field round-trips
}

func TestQuotaPoller_PollOnceSkipsWhenResolverReturnsEmpty(t *testing.T) {
	CacheOAuthToken("") // ensure no cached token

	poller := NewQuotaPoller(openTestStore(t), nil).
		WithTokenResolver(func(_ context.Context) string { return "" })

	// No token → no HTTP call → no error.
	err := poller.PollOnce(context.Background())
	require.NoError(t, err)
}

// pollerWithFakeUpstream wires the poller's http.Client to an httptest.Server
// that returns the given response, and installs a token resolver returning
// "stub-token" so PollOnce reaches the HTTP step.
func pollerWithFakeUpstream(t *testing.T, status int, body string) (*QuotaPoller, *int) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	// Route requests to the fake server by rewriting the URL in a custom
	// RoundTripper — the real poller hardcodes the minimax endpoint URL.
	client := &http.Client{Transport: rewriteToURL(srv.URL)}
	calls := 0
	poller := NewQuotaPoller(openTestStore(t), client).
		WithTokenResolver(func(_ context.Context) string { return "stub-token" }).
		WithUnauthorizedCallback(func() { calls++ })
	return poller, &calls
}

// rewriteToURL is a tiny RoundTripper that sends every request to the
// httptest server's URL while preserving the method.
type urlRewriter struct{ target string }

func (r urlRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(r.target)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	req.URL.Path = u.Path
	req.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func rewriteToURL(target string) http.RoundTripper { return urlRewriter{target: target} }

func TestQuotaPoller_PollOnce_HTTPUnauthorizedFiresCallback(t *testing.T) {
	poller, calls := pollerWithFakeUpstream(t, http.StatusUnauthorized, `{}`)

	err := poller.PollOnce(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMinimaxAuth, "401 must wrap ErrMinimaxAuth")
	assert.Equal(t, 1, *calls, "callback should fire exactly once on HTTP 401")
}

func TestQuotaPoller_PollOnce_HTTPForbiddenFiresCallback(t *testing.T) {
	poller, calls := pollerWithFakeUpstream(t, http.StatusForbidden, `{}`)

	err := poller.PollOnce(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMinimaxAuth)
	assert.Equal(t, 1, *calls)
}

func TestQuotaPoller_PollOnce_BodyStatus1004FiresCallback(t *testing.T) {
	// MiniMax's real-world auth failure: HTTP 200 + base_resp.status_code = 1004.
	// See spec/05 §13. Body is the exact shape parseQuotaResponse expects.
	body := `{
		"base_resp": {"status_code": 1004, "status_msg": "login fail"},
		"model_remains": []
	}`
	poller, calls := pollerWithFakeUpstream(t, http.StatusOK, body)

	err := poller.PollOnce(context.Background())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMinimaxAuth,
		"status_code 1004 in body must propagate ErrMinimaxAuth so callers can errors.Is it")
	assert.Equal(t, 1, *calls, "body-level 1004 must fire the callback too")
}

func TestQuotaPoller_PollOnce_HappyPathDoesNotFireCallback(t *testing.T) {
	// Real-shape happy response: status_code 0, one MiniMax-M tier.
	body := `{
		"base_resp": {"status_code": 0, "status_msg": "ok"},
		"model_remains": [{
			"model_name": "MiniMax-M*",
			"start_time": 1779400000000,
			"end_time":   1779418000000,
			"current_interval_total_count": 1500,
			"current_interval_usage_count": 21
		}]
	}`
	poller, calls := pollerWithFakeUpstream(t, http.StatusOK, body)

	err := poller.PollOnce(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, *calls, "happy path must not fire the unauthorized callback")
}

func TestParseQuotaResponse_RetainsAllNonZeroScenarios(t *testing.T) {
	// MiniMax's token-plan endpoint returns multiple scenarios per account
	// (text gen, speech synthesis, lyrics, image, music, MCP image-understanding,
	// MCP web-search, …). We used to filter out everything but MiniMax-M* —
	// that hid 6 of the 7 scenarios from the dashboard. The fix retains every
	// scenario with a non-zero total_count.
	body := []byte(`{
		"base_resp": {"status_code": 0, "status_msg": "ok"},
		"model_remains": [
			{"model_name": "MiniMax-M*",        "start_time": 1779400000000, "end_time": 1779418000000, "current_interval_total_count": 1500, "current_interval_usage_count": 67},
			{"model_name": "speech_synthesis",  "start_time": 1779379200000, "end_time": 1779465600000, "current_interval_total_count": 4000, "current_interval_usage_count": 0},
			{"model_name": "lyric_generation",  "start_time": 1779379200000, "end_time": 1779465600000, "current_interval_total_count": 100,  "current_interval_usage_count": 0},
			{"model_name": "image_generation",  "start_time": 1779379200000, "end_time": 1779465600000, "current_interval_total_count": 50,   "current_interval_usage_count": 0},
			{"model_name": "music_generation",  "start_time": 1779379200000, "end_time": 1779465600000, "current_interval_total_count": 100,  "current_interval_usage_count": 0},
			{"model_name": "mcp_image_understanding", "start_time": 1779400000000, "end_time": 1779418000000, "current_interval_total_count": 150, "current_interval_usage_count": 0},
			{"model_name": "mcp_web_search",          "start_time": 1779400000000, "end_time": 1779418000000, "current_interval_total_count": 150, "current_interval_usage_count": 0},
			{"model_name": "inactive_plan",     "start_time": 0,             "end_time": 0,             "current_interval_total_count": 0,    "current_interval_usage_count": 0}
		]
	}`)
	got, err := parseQuotaResponse(body)
	require.NoError(t, err)
	require.Len(t, got, 7, "all 7 active scenarios kept; inactive (total_count=0) dropped")

	names := map[string]bool{}
	for _, q := range got {
		names[q.ModelPattern] = true
		assert.Equal(t, "minimax", q.Provider)
	}
	for _, scenario := range []string{
		"MiniMax-M*", "speech_synthesis", "lyric_generation", "image_generation",
		"music_generation", "mcp_image_understanding", "mcp_web_search",
	} {
		assert.True(t, names[scenario], "scenario %q must be retained", scenario)
	}
	assert.False(t, names["inactive_plan"], "zero-total scenarios still skipped")
}

func TestParseQuotaResponse_Status1004ReturnsErrMinimaxAuth(t *testing.T) {
	body := []byte(`{"base_resp":{"status_code":1004,"status_msg":"login fail"}}`)
	_, err := parseQuotaResponse(body)
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrMinimaxAuth)
}

func TestParseQuotaResponse_OtherStatusCodeDoesNotWrapAuth(t *testing.T) {
	// status_code 2013 is some other (non-auth) error per MiniMax docs.
	// Must NOT be classified as an auth failure or we'd spuriously rescan.
	body := []byte(`{"base_resp":{"status_code":2013,"status_msg":"server busy"}}`)
	_, err := parseQuotaResponse(body)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrMinimaxAuth,
		"non-1004 errors must not get the auth sentinel")
}

func TestQuotaPoller_WithUnauthorizedCallback_NilClears(t *testing.T) {
	poller := NewQuotaPoller(openTestStore(t), nil)
	poller.WithUnauthorizedCallback(func() {})
	require.NotNil(t, poller.onUnauthorized)
	poller.WithUnauthorizedCallback(nil)
	require.Nil(t, poller.onUnauthorized, "passing nil should clear the callback")
}
