package proxy_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/logging"
	anthropicadapter "github.com/kinthaiofficial/krouter/internal/providers/anthropic"
	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/proxy"
	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFallbackServer creates a routing proxy and a mutable upstream handler.
// The upstream handler can be swapped by writing to the returned *atomic.Value.
func newFallbackServer(t *testing.T, handler http.HandlerFunc) *httptest.Server {
	t.Helper()
	upstream := httptest.NewServer(handler)
	t.Cleanup(upstream.Close)

	store, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })

	adapter := anthropicadapter.New(upstream.URL, upstream.Client())
	reg := providers.New()
	reg.Register(adapter)
	engine := routing.New(reg)

	var logBuf bytes.Buffer
	srv := proxy.New(
		proxy.WithLogger(logging.NewWithWriter("debug", &logBuf)),
		proxy.WithEngine(engine),
		proxy.WithRegistry(reg),
		proxy.WithStore(store),
	)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// TestFallbackDecide_NoFallbackOn401 verifies that a 401 from upstream is
// forwarded directly to the client without triggering a fallback retry.
func TestFallbackDecide_NoFallbackOn401(t *testing.T) {
	var callCount int32
	ts := newFallbackServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
	})

	reqBody := `{"model":"claude-opus-4-5","messages":[],"max_tokens":10}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount), "401 must not trigger retry")
}

// TestFallbackDecide_NoFallbackOn429 verifies that a 429 from upstream is
// forwarded directly to the client without triggering a fallback retry.
func TestFallbackDecide_NoFallbackOn429(t *testing.T) {
	var callCount int32
	ts := newFallbackServer(t, func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&callCount, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate limited"}`))
	})

	reqBody := `{"model":"claude-sonnet-4-5","messages":[],"max_tokens":10}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, int32(1), atomic.LoadInt32(&callCount), "429 must not trigger retry")
}

// TestFallbackDecide_5xxTriggersModelDowngrade verifies that a 503 response
// causes the proxy to retry with the next lower model tier (opus→sonnet).
func TestFallbackDecide_5xxTriggersModelDowngrade(t *testing.T) {
	var callCount int32
	var receivedModels []string

	ts := newFallbackServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&callCount, 1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		model, _ := body["model"].(string)
		receivedModels = append(receivedModels, model)

		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error":"service unavailable"}`))
		} else {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"m1","type":"message","usage":{"input_tokens":5,"output_tokens":3}}`))
		}
	})

	reqBody := `{"model":"claude-opus-4-5","messages":[{"role":"user","content":"hi"}],"max_tokens":10}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// Second attempt should succeed.
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, int32(2), atomic.LoadInt32(&callCount))
	// First request used opus; second should be downgraded.
	require.Len(t, receivedModels, 2)
	assert.Equal(t, "claude-opus-4-5", receivedModels[0])
	assert.Equal(t, "claude-sonnet-4-6", receivedModels[1], "should downgrade to sonnet on 5xx")
}

// TestFallbackDecide_5xxNoFallbackReturnsLastError verifies that when there is
// no fallback option, the last 5xx status is returned to the client.
func TestFallbackDecide_5xxNoFallbackReturnsLastError(t *testing.T) {
	// All requests return 503; no fallback available (only one provider+model).
	ts := newFallbackServer(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"error":"always fails"}`))
	})

	// Use haiku — lowest tier, no further fallback.
	reqBody := `{"model":"claude-haiku-4-5-20251001","messages":[],"max_tokens":10}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}
