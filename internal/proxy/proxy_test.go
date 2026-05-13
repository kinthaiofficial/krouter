package proxy_test

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/proxy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTestServer creates a proxy backed by the given mock upstream and returns
// an httptest.Server wrapping the proxy handler.
func newTestServer(t *testing.T, upstream *httptest.Server) *httptest.Server {
	t.Helper()
	var buf bytes.Buffer
	srv := proxy.New(
		proxy.WithLogger(logging.NewWithWriter("debug", &buf)),
		proxy.WithAnthropicURL(upstream.URL),
	)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return ts
}

// ── /health ─────────────────────────────────────────────────────────────────

func TestHealth(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	resp, err := http.Get(ts.URL + "/health")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(body), `"status":"ok"`)
}

func TestHealth_MethodNotAllowed(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	resp, err := http.Post(ts.URL+"/health", "application/json", nil) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ── /v1/messages (non-streaming) ────────────────────────────────────────────

func TestAnthropicMessages_NonStreaming(t *testing.T) {
	const responseBody = `{"id":"msg_1","type":"message","content":[{"type":"text","text":"Hi"}]}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/messages", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(responseBody))
	}))
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	reqBody := `{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"Hi"}],"max_tokens":10}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, responseBody, string(body))
}

func TestAnthropicMessages_HeadersForwarded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "sk-test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/messages",
		strings.NewReader(`{"model":"claude-haiku-4-5","messages":[],"max_tokens":1}`))
	req.Header.Set("x-api-key", "sk-test-key")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ── /v1/messages (streaming) ─────────────────────────────────────────────────

func TestAnthropicMessages_Streaming(t *testing.T) {
	sseChunks := []string{
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"Hello\"}}\n\n",
		"data: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\" world\"}}\n\n",
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
	ts := newTestServer(t, upstream)

	reqBody := `{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"Hi"}],"max_tokens":10,"stream":true}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
	assert.Equal(t, "no", resp.Header.Get("X-Accel-Buffering"))

	body, _ := io.ReadAll(resp.Body)
	full := string(body)
	assert.Contains(t, full, "Hello")
	assert.Contains(t, full, "world")
	assert.Contains(t, full, "[DONE]")
}

// ── Upstream error codes ──────────────────────────────────────────────────────

func TestAnthropicMessages_Upstream401(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"type":"authentication_error","message":"invalid x-api-key"}}`))
	}))
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", //nolint:noctx
		strings.NewReader(`{"model":"claude-haiku-4-5","messages":[],"max_tokens":1}`))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAnthropicMessages_Upstream429(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"rate_limit_error"}}`))
	}))
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", //nolint:noctx
		strings.NewReader(`{"model":"claude-haiku-4-5","messages":[],"max_tokens":1}`))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusTooManyRequests, resp.StatusCode)
	assert.Equal(t, "30", resp.Header.Get("Retry-After"))
}

func TestAnthropicMessages_Upstream503(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", //nolint:noctx
		strings.NewReader(`{"model":"claude-haiku-4-5","messages":[],"max_tokens":1}`))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

// ── Client disconnect cancels upstream ───────────────────────────────────────

func TestAnthropicMessages_ClientDisconnect(t *testing.T) {
	upstreamCancelled := make(chan struct{})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		// Send one chunk, then wait for context cancellation.
		_, _ = w.Write([]byte("data: first\n\n"))
		flusher.Flush()
		<-r.Context().Done()
		close(upstreamCancelled)
	}))
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	reqBody := `{"model":"claude-haiku-4-5","messages":[{"role":"user","content":"Hi"}],"max_tokens":100,"stream":true}`
	resp, err := http.Post(ts.URL+"/v1/messages", "application/json", strings.NewReader(reqBody)) //nolint:noctx
	require.NoError(t, err)

	// Read first chunk then close the connection.
	buf := make([]byte, 64)
	_, _ = resp.Body.Read(buf)
	_ = resp.Body.Close()

	select {
	case <-upstreamCancelled:
		// Upstream context was cancelled — correct behaviour.
	case <-time.After(3 * time.Second):
		t.Fatal("upstream context was not cancelled after client disconnect")
	}
}

// ── /v1/models ───────────────────────────────────────────────────────────────

func TestModels_ForwardedToUpstream(t *testing.T) {
	const modelsBody = `{"object":"list","data":[{"id":"claude-sonnet-4-5","object":"model"}]}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, http.MethodGet, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(modelsBody))
	}))
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	req, _ := http.NewRequest(http.MethodGet, ts.URL+"/v1/models", nil)
	req.Header.Set("x-api-key", "sk-test")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	body, _ := io.ReadAll(resp.Body)
	assert.Equal(t, modelsBody, string(body))
}

// ── /v1/chat/completions (stub) ──────────────────────────────────────────────

func TestOpenAICompletions_NotImplemented(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", //nolint:noctx
		strings.NewReader(`{}`))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotImplemented, resp.StatusCode)
}

// ── Unknown path ──────────────────────────────────────────────────────────────

func TestUnknownPath(t *testing.T) {
	upstream := httptest.NewServer(http.NotFoundHandler())
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	resp, err := http.Get(ts.URL + "/unknown/path") //nolint:noctx
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

// ── Security: hop-by-hop headers not forwarded ───────────────────────────────

func TestAnthropicMessages_HopByHopNotForwarded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Empty(t, r.Header.Get("Connection"), "Connection header must not be forwarded")
		assert.Empty(t, r.Header.Get("Transfer-Encoding"), "Transfer-Encoding must not be forwarded")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()
	ts := newTestServer(t, upstream)

	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/v1/messages",
		strings.NewReader(`{"model":"claude-haiku-4-5","messages":[],"max_tokens":1}`))
	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Transfer-Encoding", "chunked")
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
