package glm_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/providers/glm"
	openaiAdapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGLM_Interface(t *testing.T) {
	a := glm.New(nil)
	var _ providers.Provider = a
	assert.Equal(t, "glm", a.Name())
	assert.Equal(t, providers.ProtocolOpenAI, a.Protocol())
	assert.Contains(t, a.SupportedModels(), "glm-4")
	assert.Contains(t, a.SupportedModels(), "glm-4-flash")
}

func TestGLM_PathReplace_V4(t *testing.T) {
	t.Setenv("ZHIPU_API_KEY", "test-zhipu-key")

	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	// Build adapter pointing at test server with /v4 path replacement.
	a := openaiAdapter.NewWithPathReplace("glm", srv.URL, "/v4", "ZHIPU_API_KEY", []string{"glm-4"}, nil)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		"http://placeholder/v1/chat/completions",
		io.NopCloser(strings.NewReader(`{"model":"glm-4"}`)))
	require.NoError(t, err)

	resp, err := a.Forward(context.Background(), req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, "/v4/chat/completions", capturedPath, "GLM adapter must rewrite /v1 → /v4")
}
