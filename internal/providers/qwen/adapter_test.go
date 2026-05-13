package qwen_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/providers/qwen"
	openaiAdapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestQwen_Interface(t *testing.T) {
	a := qwen.New(nil)
	var _ providers.Provider = a
	assert.Equal(t, "qwen", a.Name())
	assert.Equal(t, providers.ProtocolOpenAI, a.Protocol())
	assert.Contains(t, a.SupportedModels(), "qwen-max")
	assert.Contains(t, a.SupportedModels(), "qwen-turbo")
}

func TestQwen_PathReplace_CompatibleMode(t *testing.T) {
	t.Setenv("DASHSCOPE_API_KEY", "test-dashscope-key")

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	a := openaiAdapter.NewWithPathReplace("qwen", srv.URL, "/compatible-mode/v1", "DASHSCOPE_API_KEY", []string{"qwen-max"}, nil)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://placeholder/v1/chat/completions",
		io.NopCloser(strings.NewReader(`{"model":"qwen-max"}`)))
	require.NoError(t, err)

	resp, err := a.Forward(context.Background(), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "/compatible-mode/v1/chat/completions", capturedPath,
		"Qwen adapter must rewrite /v1 → /compatible-mode/v1")
}
