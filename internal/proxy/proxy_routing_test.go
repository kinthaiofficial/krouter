package proxy_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/logging"
	anthropicadapter "github.com/kinthaiofficial/krouter/internal/providers/anthropic"
	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/proxy"
	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newRoutingServer creates a full routing-enabled proxy backed by the given upstream.
func newRoutingServer(t *testing.T, upstream *httptest.Server) (*httptest.Server, *storage.Store) {
	t.Helper()

	store, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	adapter := anthropicadapter.New(upstream.URL, upstream.Client())
	reg := providers.New()
	reg.Register(adapter)
	engine := routing.New(reg)

	var buf bytes.Buffer
	srv := proxy.New(
		proxy.WithLogger(logging.NewWithWriter("debug", &buf)),
		proxy.WithEngine(engine),
		proxy.WithRegistry(reg),
		proxy.WithStore(store),
	)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts, store
}

// ── Routing path: non-streaming ───────────────────────────────────────────────

func TestRouting_NonStreaming_KnownModel(t *testing.T) {
	const responseBody = `{"id":"msg_1","type":"message","usage":{"input_tokens":10,"output_tokens":5}}`

	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseBody))
	}))
	defer upstream.Close()

	ts, store := newRoutingServer(t, upstream)

	reqBody := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"Hi"}],"max_tokens":10}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "claude-sonnet-4-5", gotModel)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, responseBody, string(body))

	// Allow goroutine to write SQLite log.
	time.Sleep(50 * time.Millisecond)
	rows, err := store.ListRequests(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "anthropic", rows[0].Provider)
	assert.Equal(t, "claude-sonnet-4-5", rows[0].Model)
	assert.Equal(t, 10, rows[0].InputTokens)
	assert.Equal(t, 5, rows[0].OutputTokens)
	assert.Equal(t, 200, rows[0].StatusCode)
}

func TestRouting_NonStreaming_UnknownModelFallback(t *testing.T) {
	var gotModel string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotModel, _ = body["model"].(string)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","usage":{"input_tokens":1,"output_tokens":1}}`))
	}))
	defer upstream.Close()

	ts, _ := newRoutingServer(t, upstream)

	reqBody := `{"model":"claude-future-9000","messages":[{"role":"user","content":"Hi"}],"max_tokens":1}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	// Engine should have replaced unknown model with fallback.
	assert.Equal(t, "claude-haiku-4-5-20251001", gotModel)
}

// ── Routing path: streaming ───────────────────────────────────────────────────

func TestRouting_Streaming(t *testing.T) {
	sseChunks := []string{
		"data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":8,\"output_tokens\":0}}}\n\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hi\"}}\n\n",
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":3}}\n\n",
		"data: [DONE]\n\n",
	}

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		for _, chunk := range sseChunks {
			_, _ = w.Write([]byte(chunk))
			flusher.Flush()
		}
	}))
	defer upstream.Close()

	ts, store := newRoutingServer(t, upstream)

	reqBody := `{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"Hi"}],"max_tokens":10,"stream":true}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	body, _ := io.ReadAll(resp.Body)
	full := string(body)
	assert.Contains(t, full, "Hi")
	assert.Contains(t, full, "[DONE]")

	// Wait for async SQLite log.
	time.Sleep(50 * time.Millisecond)
	rows, err := store.ListRequests(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "claude-haiku-4-5", rows[0].Model)
	assert.Equal(t, 8, rows[0].InputTokens)
	assert.Equal(t, 3, rows[0].OutputTokens)
}

// ── Routing path: upstream error forwarded ────────────────────────────────────

func TestRouting_Upstream401_Forwarded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"bad key"}`))
	}))
	defer upstream.Close()

	ts, _ := newRoutingServer(t, upstream)

	reqBody := `{"model":"claude-haiku-4-5","messages":[],"max_tokens":1}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// ── Utility: parseAnthropicSSEUsage (via streaming test) ─────────────────────

func TestRouting_SSEUsageParsing(t *testing.T) {
	// Embed usage in a realistic SSE stream.
	sseData := `data: {"type":"message_start","message":{"usage":{"input_tokens":20,"output_tokens":0}}}

data: {"type":"message_delta","usage":{"output_tokens":15}}

data: [DONE]

`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		_, _ = w.Write([]byte(sseData))
		flusher.Flush()
	}))
	defer upstream.Close()

	ts, store := newRoutingServer(t, upstream)

	reqBody := `{"model":"claude-sonnet-4-5","messages":[],"max_tokens":50,"stream":true}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.ReadAll(resp.Body)

	time.Sleep(50 * time.Millisecond)
	rows, err := store.ListRequests(context.Background(), 10)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	// Should capture input_tokens=20 and output_tokens=15 from the last usage events.
	assert.Equal(t, 20, rows[0].InputTokens)
	assert.Equal(t, 15, rows[0].OutputTokens)
}

// ── Budget hard-stop ──────────────────────────────────────────────────────────

// exhaustedQuota is a routing.QuotaSource stub that reports 100% daily usage.
type exhaustedQuota struct{}

func (exhaustedQuota) CurrentQuota(_ context.Context) routing.QuotaState {
	return routing.QuotaState{DailyPercent: 1.0}
}

func newRoutingServerWithQuota(t *testing.T, upstream *httptest.Server, qs routing.QuotaSource) *httptest.Server {
	t.Helper()

	store, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	adapter := anthropicadapter.New(upstream.URL, upstream.Client())
	reg := providers.New()
	reg.Register(adapter)
	engine := routing.New(reg)
	engine.WithQuota(qs)

	var buf bytes.Buffer
	srv := proxy.New(
		proxy.WithLogger(logging.NewWithWriter("debug", &buf)),
		proxy.WithEngine(engine),
		proxy.WithRegistry(reg),
		proxy.WithStore(store),
	)

	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

func TestBudgetExceeded_Anthropic_Returns429(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// Should never be reached — budget block must fire before any forward.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"msg_1"}`))
	}))
	defer upstream.Close()

	ts := newRoutingServerWithQuota(t, upstream, exhaustedQuota{})

	reqBody := `{"model":"claude-sonnet-4-5","messages":[],"max_tokens":50}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "exhausted budget must return 429")

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "budget", "error body must mention budget")
}

func TestBudgetExceeded_OpenAI_Returns429(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()

	store, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	reg := providers.New()
	engine := routing.New(reg)
	engine.WithQuota(exhaustedQuota{})

	var buf bytes.Buffer
	srv := proxy.New(
		proxy.WithLogger(logging.NewWithWriter("debug", &buf)),
		proxy.WithEngine(engine),
		proxy.WithRegistry(reg),
		proxy.WithStore(store),
	)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	reqBody := `{"model":"gpt-4o","messages":[]}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode, "exhausted budget must return 429 for OpenAI protocol")

	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), "budget")
}

// Issue #52: when the fallback chain is exhausted (here: the only provider is
// unreachable), the proxy returns 502 but must still produce a durable log
// record, so the failed request isn't silently missing from the Router/Logs
// dashboards. We assert via the onComplete hook (fired by logRequest) rather
// than reading the :memory: store, whose per-connection isolation makes a
// cross-goroutine read flaky.
func TestRouting_FallbackExhausted_StillLogsRow(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())

	store, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	reg := providers.New()
	reg.Register(anthropicadapter.New(upstream.URL, upstream.Client()))
	engine := routing.New(reg)

	logged := make(chan storage.RequestRecord, 8)
	srv := proxy.New(
		proxy.WithLogger(logging.NewWithWriter("error", &bytes.Buffer{})),
		proxy.WithEngine(engine),
		proxy.WithRegistry(reg),
		proxy.WithStore(store),
	)
	srv.SetOnComplete(func(rec storage.RequestRecord) { logged <- rec })
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	upstream.Close() // unreachable → every fallback attempt errors out → 502

	reqBody := `{"model":"claude-sonnet-4-5","messages":[{"role":"user","content":"Hi"}],"max_tokens":10}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadGateway, resp.StatusCode)

	select {
	case rec := <-logged:
		assert.Equal(t, http.StatusBadGateway, rec.StatusCode, "fallback exhaustion must log a 502 row (#52)")
		assert.Equal(t, "anthropic", rec.Protocol)
		assert.Equal(t, "claude-sonnet-4-5", rec.RequestedModel)
	case <-time.After(2 * time.Second):
		t.Fatal("fallback-exhausted request was not logged (#52)")
	}
}
