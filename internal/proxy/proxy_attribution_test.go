package proxy_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// newAttribServer builds a routing proxy (anthropic upstream) that recognises
// the /a/<appid> prefix and reports every logged request on the returned channel.
func newAttribServer(t *testing.T, upstream *httptest.Server) (*httptest.Server, chan storage.RequestRecord) {
	t.Helper()
	store, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	reg := providers.New()
	reg.Register(anthropicadapter.New(upstream.URL, upstream.Client()))
	engine := routing.New(reg)

	srv := proxy.New(
		proxy.WithLogger(logging.New("error")),
		proxy.WithEngine(engine),
		proxy.WithRegistry(reg),
		proxy.WithStore(store),
		proxy.WithKnownApps([]string{"openclaw", "claude-code", "cursor", "codex", "opencode", "hermes"}),
	)
	recs := make(chan storage.RequestRecord, 4)
	srv.SetOnComplete(func(rec storage.RequestRecord) { recs <- rec })

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, recs
}

func anthropicUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":10,"output_tokens":5}}`))
	}))
	t.Cleanup(up.Close)
	return up
}

func postMessages(t *testing.T, url, userAgent string) *http.Response {
	t.Helper()
	body := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"hi"}],"max_tokens":5}`
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	if userAgent != "" {
		req.Header.Set("User-Agent", userAgent)
	}
	resp, err := http.DefaultClient.Do(req) //nolint:bodyclose
	require.NoError(t, err)
	return resp
}

// The /a/<appid> path prefix is the authoritative source of the application id
// and must win over header sniffing.
func TestAttribution_PathPrefixWinsOverSniff(t *testing.T) {
	ts, recs := newAttribServer(t, anthropicUpstream(t))

	// Cursor UA, but the path says openclaw → openclaw must be recorded.
	resp := postMessages(t, ts.URL+"/a/openclaw/v1/messages", "Cursor/1.0")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case rec := <-recs:
		assert.Equal(t, "openclaw", rec.Agent, "path prefix must win over the Cursor UA")
	case <-time.After(2 * time.Second):
		t.Fatal("no request logged")
	}
}

// Requests with no /a/<appid> prefix fall back to the legacy header sniff.
func TestAttribution_NoPrefixFallsBackToSniff(t *testing.T) {
	ts, recs := newAttribServer(t, anthropicUpstream(t))

	resp := postMessages(t, ts.URL+"/v1/messages", "Cursor/1.0")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case rec := <-recs:
		assert.Equal(t, "cursor", rec.Agent, "no prefix → fall back to UA sniff")
	case <-time.After(2 * time.Second):
		t.Fatal("no request logged")
	}
}

func TestAttribution_UnknownAppIDIs404(t *testing.T) {
	ts, _ := newAttribServer(t, anthropicUpstream(t))
	resp := postMessages(t, ts.URL+"/a/bogus-app/v1/messages", "")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestAttribution_UnknownSuffixIs404(t *testing.T) {
	ts, _ := newAttribServer(t, anthropicUpstream(t))
	// /responses is a real OpenAI endpoint krouter does not handle.
	resp := postMessages(t, ts.URL+"/a/openclaw/v1/responses", "")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// A provider-specific base path is preserved through the prefix, and the proxy
// still attributes + dispatches by the canonical suffix.
func TestAttribution_PreservedProviderPathDispatches(t *testing.T) {
	ts, recs := newAttribServer(t, anthropicUpstream(t))

	// minimax-portal style base (.../anthropic/v1) becomes /a/openclaw/anthropic/v1,
	// then the client appends /messages.
	resp := postMessages(t, ts.URL+"/a/openclaw/anthropic/v1/messages", "")
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	select {
	case rec := <-recs:
		assert.Equal(t, "openclaw", rec.Agent)
		assert.Equal(t, "anthropic", rec.Protocol)
	case <-time.After(2 * time.Second):
		t.Fatal("no request logged")
	}
}
