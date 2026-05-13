package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/api"
	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestServerWithSettings(t *testing.T) (*api.Server, *httptest.Server, *config.Manager) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	mgr := config.New(path)
	srv := api.New(nil, "test-version", 8402, 8403)
	srv.SetTokenForTest("test-token-123")
	srv.SetSettings(mgr)
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts, mgr
}

func TestGetSettings_ReturnsDefaults(t *testing.T) {
	_, ts, _ := newTestServerWithSettings(t)
	resp := doGet(t, ts, "/internal/settings")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var s config.Settings
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&s))
	assert.Equal(t, "balanced", s.Preset)
	assert.Equal(t, "en", s.Language)
}

func TestGetSettings_RequiresAuth(t *testing.T) {
	_, ts, _ := newTestServerWithSettings(t)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		ts.URL+"/internal/settings", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestPatchSettings_Preset(t *testing.T) {
	_, ts, mgr := newTestServerWithSettings(t)

	body := bytes.NewBufferString(`{"preset":"saver"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch,
		ts.URL+"/internal/settings", body)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "saver", mgr.Get().Preset)
}

func TestPatchSettings_Language(t *testing.T) {
	_, ts, mgr := newTestServerWithSettings(t)

	body := bytes.NewBufferString(`{"language":"zh-CN"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch,
		ts.URL+"/internal/settings", body)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "zh-CN", mgr.Get().Language)
}

func TestPatchSettings_BudgetWarnings(t *testing.T) {
	_, ts, mgr := newTestServerWithSettings(t)

	body := bytes.NewBufferString(`{"budget_warnings":{"daily":5.0,"weekly":20.0}}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch,
		ts.URL+"/internal/settings", body)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	s := mgr.Get()
	assert.Equal(t, 5.0, s.BudgetWarnings["daily"])
	assert.Equal(t, 20.0, s.BudgetWarnings["weekly"])
}

func TestPatchSettings_InvalidPreset(t *testing.T) {
	_, ts, _ := newTestServerWithSettings(t)

	body := bytes.NewBufferString(`{"preset":"ultra"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch,
		ts.URL+"/internal/settings", body)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPatchSettings_RequiresAuth(t *testing.T) {
	_, ts, _ := newTestServerWithSettings(t)
	body := bytes.NewBufferString(`{"preset":"saver"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch,
		ts.URL+"/internal/settings", body)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestGetSettings_PatchRoundtrip(t *testing.T) {
	_, ts, _ := newTestServerWithSettings(t)

	// Patch to quality.
	body := bytes.NewBufferString(`{"preset":"quality"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch,
		ts.URL+"/internal/settings", body)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	_ = resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// GET must return quality.
	resp2 := doGet(t, ts, "/internal/settings")
	defer func() { _ = resp2.Body.Close() }()
	var s config.Settings
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&s))
	assert.Equal(t, "quality", s.Preset)
}

// Ensure NilStore server returns something sensible when settings manager is nil.
func TestGetSettings_NilManager(t *testing.T) {
	srv := api.New(nil, "v", 8402, 8403)
	srv.SetTokenForTest("test-token-123")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp := doGet(t, ts, "/internal/settings")
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	var s config.Settings
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&s))
	assert.Equal(t, "balanced", s.Preset)
}

func TestPatchSettings_NilManager(t *testing.T) {
	srv := api.New(nil, "v", 8402, 8403)
	srv.SetTokenForTest("test-token-123")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	body := bytes.NewBufferString(`{"preset":"saver"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPatch,
		ts.URL+"/internal/settings", body)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}

var _ = filepath.Separator // keep import used
