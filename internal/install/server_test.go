package install

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testToken = "test-install-token-abc123"

// newTestServer creates a Server with stub hooks ready for testing.
func newTestServer(t *testing.T) (*Server, *testHooks) {
	t.Helper()
	ui := NullUI{}
	orch, hooks := testOrchestrator(ui, Options{SrcBinary: "/tmp/krouter-src"})
	srv := NewServer(testToken, orch)
	srv.readInternalTokenFn = func() (string, error) { return "daemon-tok", nil }
	srv.mintDaemonTicketFn = func(_ string) (string, error) { return "test-ticket-xyz", nil }
	return srv, hooks
}

func authed(req *http.Request) *http.Request {
	q := req.URL.Query()
	q.Set("token", testToken)
	req.URL.RawQuery = q.Encode()
	return req
}

func TestInstallServer_Health_NoAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/health", nil))
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestInstallServer_TokenRequired(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/install/detect-agents", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInstallServer_BearerAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/install/detect-agents", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestInstallServer_DetectAgents(t *testing.T) {
	srv, hooks := newTestServer(t)
	hooks.detectAgentsResult = []config.AgentInfo{
		{Name: "openclaw", ConfigPath: "/home/user/.openclaw/openclaw.json"},
		{Name: "claude-code", CLIPath: "/usr/bin/claude"},
	}

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodGet, "/api/install/detect-agents", nil)))

	require.Equal(t, http.StatusOK, w.Code)
	var agents []map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &agents))
	require.Len(t, agents, 2)
	assert.Equal(t, "openclaw", agents[0]["name"])
	assert.Equal(t, "claude-code", agents[1]["name"])
}

func TestInstallServer_CopyBinary(t *testing.T) {
	srv, hooks := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/copy-binary", nil)))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"/tmp/krouter-src"}, hooks.installDaemonCalls)
}

func TestInstallServer_CopyBinary_Error(t *testing.T) {
	srv, hooks := newTestServer(t)
	hooks.installDaemonErr = errors.New("no space left on device")

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/copy-binary", nil)))

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

func TestInstallServer_ShellIntegration(t *testing.T) {
	srv, hooks := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/shell-integration", nil)))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"/tmp/test_rc"}, hooks.writeShellRCCalls)
}

func TestInstallServer_ConnectAgent_OpenClaw(t *testing.T) {
	srv, hooks := newTestServer(t)

	body := `{"agent":"openclaw","config_path":"/home/user/.openclaw/openclaw.json"}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/connect-agent", strings.NewReader(body))))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"/home/user/.openclaw/openclaw.json"}, hooks.connectOpenClawCalls)
}

func TestInstallServer_ConnectAgent_UnknownAgent(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"agent":"unknown-agent","config_path":""}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/connect-agent", strings.NewReader(body))))

	// Unknown agents are silently skipped (no error), so expect 200.
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestInstallServer_ConnectAgent_MissingName(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/connect-agent", strings.NewReader(body))))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestInstallServer_Finalize_ReturnsRedirectURL(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/finalize", nil)))

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Contains(t, resp["redirect_url"], "test-ticket-xyz")
	assert.Contains(t, resp["redirect_url"], "exchange")
}

func TestInstallServer_Finalize_FallsBackWithoutDaemon(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.readInternalTokenFn = func() (string, error) { return "", errors.New("daemon not running") }

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/finalize", nil)))

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	// Falls back to /ui/ without ticket.
	assert.Equal(t, "http://127.0.0.1:8403/ui/", resp["redirect_url"])
}

func TestInstallServer_TokenReplay_FinalizeOnlyOnce(t *testing.T) {
	srv, _ := newTestServer(t)

	// First finalize succeeds.
	w1 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w1, authed(httptest.NewRequest(http.MethodPost, "/api/install/finalize", nil)))
	assert.Equal(t, http.StatusOK, w1.Code)

	// Second finalize is rejected.
	w2 := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w2, authed(httptest.NewRequest(http.MethodPost, "/api/install/finalize", nil)))
	assert.Equal(t, http.StatusGone, w2.Code)
}
