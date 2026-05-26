package anthropic_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/providers"
	anthropicadapter "github.com/kinthaiofficial/krouter/internal/providers/anthropic"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdapter_Interface(t *testing.T) {
	a := anthropicadapter.New("https://api.anthropic.com", nil)
	assert.Equal(t, "anthropic", a.Name())
	assert.Equal(t, providers.ProtocolAnthropic, a.Protocol())
	assert.NotEmpty(t, a.SupportedModels())
}

func TestAdapter_SupportedModels(t *testing.T) {
	a := anthropicadapter.New("https://api.anthropic.com", nil)
	models := a.SupportedModels()
	assert.Contains(t, models, "claude-sonnet-4-5")
	assert.Contains(t, models, "claude-haiku-4-5")
}

func TestAdapter_Forward_URLRewrite(t *testing.T) {
	var gotPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"type":"message"}`))
	}))
	defer upstream.Close()

	a := anthropicadapter.New(upstream.URL, upstream.Client())

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://127.0.0.1:8402/v1/messages",
		strings.NewReader(`{"model":"claude-haiku-4-5","messages":[],"max_tokens":1}`))
	req.Header.Set("x-api-key", "sk-test")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Forward(context.Background(), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "/v1/messages", gotPath)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAdapter_Forward_HeadersForwarded(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "sk-test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	a := anthropicadapter.New(upstream.URL, upstream.Client())

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://127.0.0.1:8402/v1/messages",
		strings.NewReader(`{}`))
	req.Header.Set("x-api-key", "sk-test-key")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Forward(context.Background(), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// When an auth resolver yields a token, Forward injects it as Bearer and drops
// the stale inbound x-api-key — the MiniMax re-route fix for #63.
func TestAdapter_Forward_AuthResolverInjectsBearer(t *testing.T) {
	var gotAuth, gotXAPIKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotXAPIKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	a := anthropicadapter.New(upstream.URL, upstream.Client())
	a.SetAuthResolver(func() string { return "mm-oauth-token" })

	// Inbound carries an Anthropic x-api-key, as a re-routed claude request would.
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://127.0.0.1:8402/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("x-api-key", "sk-anthropic-wrong")

	resp, err := a.Forward(context.Background(), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "Bearer mm-oauth-token", gotAuth, "resolver token must be injected as Bearer")
	assert.Empty(t, gotXAPIKey, "stale x-api-key must be dropped")
}

// With no resolved token, Forward leaves the inbound auth untouched (passthrough).
func TestAdapter_Forward_AuthResolverEmptyPassesThrough(t *testing.T) {
	var gotAuth, gotXAPIKey string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotXAPIKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	a := anthropicadapter.New(upstream.URL, upstream.Client())
	a.SetAuthResolver(func() string { return "" }) // no token → passthrough

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://127.0.0.1:8402/v1/messages", strings.NewReader(`{}`))
	req.Header.Set("x-api-key", "sk-direct")
	req.Header.Set("Authorization", "Bearer inbound-bearer")

	resp, err := a.Forward(context.Background(), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "sk-direct", gotXAPIKey, "no resolved token → forward inbound untouched")
	assert.Equal(t, "Bearer inbound-bearer", gotAuth)
}

func TestAdapter_Forward_UpstreamErrorPassthrough(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer upstream.Close()

	a := anthropicadapter.New(upstream.URL, upstream.Client())

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://127.0.0.1:8402/v1/messages",
		strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Forward(context.Background(), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
	data, _ := io.ReadAll(resp.Body)
	assert.Contains(t, string(data), "invalid key")
}

func TestAdapter_Forward_ContextCancellation(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer upstream.Close()

	a := anthropicadapter.New(upstream.URL, upstream.Client())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://127.0.0.1:8402/v1/messages",
		strings.NewReader(`{}`))

	_, err := a.Forward(ctx, req)
	assert.Error(t, err)
}

func TestDiscoverModels_Success(t *testing.T) {
	body := `{"data":[{"id":"claude-opus-4-7","display_name":"Claude Opus 4.7"},{"id":"claude-sonnet-4-6","display_name":"Claude Sonnet 4.6"}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodGet, r.Method)
		assert.Equal(t, "/v1/models", r.URL.Path)
		assert.Equal(t, "sk-test-key", r.Header.Get("x-api-key"))
		assert.Equal(t, "2023-06-01", r.Header.Get("anthropic-version"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	a := anthropicadapter.New(srv.URL, srv.Client())
	models, err := a.DiscoverModels(context.Background(), func() string { return "sk-test-key" })
	require.NoError(t, err)
	require.Len(t, models, 2)
	assert.Equal(t, "claude-opus-4-7", models[0].ID)
	assert.Equal(t, "Claude Opus 4.7", models[0].DisplayName)
	assert.Equal(t, "claude-sonnet-4-6", models[1].ID)
	assert.Equal(t, "Claude Sonnet 4.6", models[1].DisplayName)
}

func TestDiscoverModels_Unauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid key"}`))
	}))
	defer srv.Close()

	a := anthropicadapter.New(srv.URL, srv.Client())
	_, err := a.DiscoverModels(context.Background(), func() string { return "bad-key" })
	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}

func TestDiscoverModels_EmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[]}`))
	}))
	defer srv.Close()

	a := anthropicadapter.New(srv.URL, srv.Client())
	models, err := a.DiscoverModels(context.Background(), func() string { return "sk-key" })
	require.NoError(t, err)
	assert.Empty(t, models)
}

func TestDiscoverModels_DisplayNameFallsBackToID(t *testing.T) {
	body := `{"data":[{"id":"claude-haiku-4-5","display_name":""}]}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	a := anthropicadapter.New(srv.URL, srv.Client())
	models, err := a.DiscoverModels(context.Background(), func() string { return "sk-key" })
	require.NoError(t, err)
	require.Len(t, models, 1)
	assert.Equal(t, "claude-haiku-4-5", models[0].DisplayName)
}
