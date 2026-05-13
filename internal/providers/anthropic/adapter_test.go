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
