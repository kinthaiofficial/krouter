package openai_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	openaiAdapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdapter_Interface(t *testing.T) {
	a := openaiAdapter.New("deepseek", "https://api.deepseek.com", "DEEPSEEK_API_KEY", []string{"deepseek-chat"}, nil)
	// Verify it satisfies the interface at compile time.
	var _ providers.Provider = a
	assert.Equal(t, "deepseek", a.Name())
	assert.Equal(t, providers.ProtocolOpenAI, a.Protocol())
	assert.Equal(t, []string{"deepseek-chat"}, a.SupportedModels())
}

func TestAdapter_Forward_RewritesURL(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-test-key")

	var capturedURL string
	var capturedAuth string
	var capturedXAPIKey string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.Path
		capturedAuth = r.Header.Get("Authorization")
		capturedXAPIKey = r.Header.Get("x-api-key")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer srv.Close()

	a := openaiAdapter.New("deepseek", srv.URL, "DEEPSEEK_API_KEY", []string{"deepseek-chat"}, nil)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://placeholder/v1/chat/completions",
		io.NopCloser(strings.NewReader(`{"model":"deepseek-chat"}`)))
	require.NoError(t, err)
	req.Header.Set("x-api-key", "sk-ant-old-key")
	req.Header.Set("Content-Type", "application/json")

	resp, err := a.Forward(context.Background(), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "/v1/chat/completions", capturedURL)
	assert.Equal(t, "Bearer sk-test-key", capturedAuth)
	assert.Empty(t, capturedXAPIKey, "x-api-key should be stripped")
}

func TestAdapter_HasKey_FalseWhenNoEnvVar(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "")
	a := openaiAdapter.New("deepseek", "https://api.deepseek.com", "DEEPSEEK_API_KEY", []string{"deepseek-chat"}, nil)
	assert.False(t, a.HasKey())
}

func TestAdapter_HasKey_TrueFromEnvVar(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-ds-test")
	a := openaiAdapter.New("deepseek", "https://api.deepseek.com", "DEEPSEEK_API_KEY", []string{"deepseek-chat"}, nil)
	assert.True(t, a.HasKey())
}

func TestAdapter_HasKey_TrueFromKeyFn(t *testing.T) {
	a := openaiAdapter.NewWithKeyFn("deepseek", "https://api.deepseek.com", func() string { return "sk-fn-key" }, []string{"deepseek-chat"}, nil)
	assert.True(t, a.HasKey())
}

func TestAdapter_HasKey_FalseFromKeyFnReturnsEmpty(t *testing.T) {
	a := openaiAdapter.NewWithKeyFn("deepseek", "https://api.deepseek.com", func() string { return "" }, []string{"deepseek-chat"}, nil)
	assert.False(t, a.HasKey())
}

func TestAdapter_NewWithKeyFn_UsesKeyFnNotEnvVar(t *testing.T) {
	t.Setenv("DEEPSEEK_API_KEY", "sk-env-key") // env var present but should be ignored

	var capturedAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := openaiAdapter.NewWithKeyFn("deepseek", srv.URL, func() string { return "sk-fn-key" }, []string{"deepseek-chat"}, nil)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://placeholder/v1/chat/completions", http.NoBody)
	resp, err := a.Forward(context.Background(), req)
	require.NoError(t, err)
	_ = resp.Body.Close()

	assert.Equal(t, "Bearer sk-fn-key", capturedAuth, "keyFn must override env var")
}

func TestAdapter_Forward_StripsAnthropicHeaders(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-openai-test")

	var capturedHeaders http.Header

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := openaiAdapter.New("openai", srv.URL, "OPENAI_API_KEY", []string{"gpt-4o"}, nil)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://placeholder/v1/chat/completions",
		io.NopCloser(strings.NewReader(`{}`)))
	require.NoError(t, err)
	req.Header.Set("x-api-key", "sk-ant-xxx")
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("anthropic-beta", "tools-2024")

	resp, err := a.Forward(context.Background(), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Empty(t, capturedHeaders.Get("x-api-key"))
	assert.Empty(t, capturedHeaders.Get("anthropic-version"))
	assert.Empty(t, capturedHeaders.Get("anthropic-beta"))
	assert.Equal(t, "Bearer sk-openai-test", capturedHeaders.Get("Authorization"))
}
