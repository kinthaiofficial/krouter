package minimax

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

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
