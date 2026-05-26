package install

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// installFakeScanner is a no-op Scanner used to exercise the installer's
// agent endpoints without touching the real OpenClaw / Claude Code parsers.
type installFakeScanner struct {
	id      string
	name    string
	path    string
	results []agentscan.InheritedEndpoint
	err     error
}

func (f installFakeScanner) AppID() string                                                   { return f.id }
func (f installFakeScanner) DisplayName() string                                               { return f.name }
func (f installFakeScanner) DefaultConfigPath() string                                         { return f.path }
func (f installFakeScanner) Scan(_ context.Context, _ string) ([]agentscan.InheritedEndpoint, error) {
	return f.results, f.err
}

// withInstallScanners temporarily replaces the Scanner registry; safe for
// parallel test files because each test sets its own slice and restores.
func withInstallScanners(t *testing.T, ss ...agentscan.Scanner) {
	t.Helper()
	saved := agentscan.Scanners
	agentscan.Scanners = ss
	t.Cleanup(func() { agentscan.Scanners = saved })
}

// pinConfigDir makes WritePending land in a temp dir so tests are hermetic.
func pinConfigDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := os.Getenv("KROUTER_CONFIG_DIR")
	require.NoError(t, os.Setenv("KROUTER_CONFIG_DIR", dir))
	t.Cleanup(func() { _ = os.Setenv("KROUTER_CONFIG_DIR", old) })
	return dir
}

func TestInstallServer_AgentsSupported(t *testing.T) {
	withInstallScanners(t,
		installFakeScanner{id: "openclaw", name: "OpenClaw", path: "/tmp/oc"},
	)

	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodGet, "/api/install/apps/supported", nil)))

	require.Equal(t, http.StatusOK, w.Code)
	var out []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	require.Len(t, out, 1)
	assert.Equal(t, "openclaw", out[0]["app_id"])
}

func TestInstallServer_AgentsPreview_RedactsSecrets(t *testing.T) {
	withInstallScanners(t,
		installFakeScanner{
			id: "openclaw", name: "OpenClaw", path: "/d",
			results: []agentscan.InheritedEndpoint{
				{
					Provider:    "anthropic",
					EndpointURL: "https://api.anthropic.com",
					APIKey:      "sk-VERY-SECRET",
				},
				{
					Provider:    "minimax-portal",
					EndpointURL: "u",
					ExtrasJSON:  `{"oauth_token":"sk-cp-OAUTH-SECRET"}`,
				},
			},
		},
	)

	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(
		http.MethodPost, "/api/install/apps/preview",
		strings.NewReader(`{"app_id":"openclaw","path":"/d"}`),
	)))

	require.Equal(t, http.StatusOK, w.Code)

	// The response body must NOT contain the secret values; only boolean
	// presence flags. This protects users with browser devtools open.
	bodyStr := w.Body.String()
	assert.NotContains(t, bodyStr, "sk-VERY-SECRET")
	assert.NotContains(t, bodyStr, "sk-cp-OAUTH-SECRET")

	var resp struct {
		Endpoints []map[string]any `json:"endpoints"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Endpoints, 2)
	assert.Equal(t, true, resp.Endpoints[0]["has_api_key"])
	assert.Equal(t, true, resp.Endpoints[1]["has_oauth_token"])
}

func TestInstallServer_AgentsPreview_ScannerErrorSurfaced(t *testing.T) {
	withInstallScanners(t,
		installFakeScanner{
			id: "openclaw", name: "OpenClaw", path: "/d",
			err: errorString("config not found at /d"),
		},
	)

	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(
		http.MethodPost, "/api/install/apps/preview",
		strings.NewReader(`{"app_id":"openclaw","path":"/d"}`),
	)))

	require.Equal(t, http.StatusOK, w.Code) // scan ran, just unsuccessful
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "config not found")
}

func TestInstallServer_AgentsPreview_UnknownAgent(t *testing.T) {
	withInstallScanners(t /* empty */)
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(
		http.MethodPost, "/api/install/apps/preview",
		strings.NewReader(`{"app_id":"missing","path":"/d"}`),
	)))
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestInstallServer_AgentsSelect_WritesPendingFile(t *testing.T) {
	dir := pinConfigDir(t)
	srv, _ := newTestServer(t)

	body := `{"agents":[
		{"app_id":"openclaw","enabled":true,"config_path":"/x/openclaw.json"},
		{"app_id":"claude-code","enabled":false,"config_path":"/x/.zshrc"}
	]}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(
		http.MethodPost, "/api/install/apps/select", strings.NewReader(body),
	)))

	require.Equal(t, http.StatusOK, w.Code)

	// File appears in the pinned dir with the right shape.
	raw, err := os.ReadFile(filepath.Join(dir, agentscan.PendingFileName))
	require.NoError(t, err)
	var got struct {
		Agents []agentscan.PendingAgent `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(raw, &got))
	require.Len(t, got.Agents, 2)
	assert.Equal(t, "openclaw", got.Agents[0].AppID)
	assert.True(t, got.Agents[0].Enabled)
}

// errorString is a tiny error type. We can't import "errors" inside the test
// file without further imports, so use a local one-liner.
type errorString string

func (e errorString) Error() string { return string(e) }
