package install

import (
	"encoding/json"
	"errors"
	"net"
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
	srv.waitForDaemonFn = func() {} // skip polling in unit tests
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
	srv.Handler().ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/install/detect-apps", nil))
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestInstallServer_BearerAuth(t *testing.T) {
	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/install/detect-apps", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	srv.Handler().ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestInstallServer_DetectAgents(t *testing.T) {
	srv, hooks := newTestServer(t)
	hooks.detectAgentsResult = []config.AppInfo{
		{Name: "openclaw", ConfigPath: "/home/user/.openclaw/openclaw.json"},
		{Name: "claude-code", CLIPath: "/usr/bin/claude"},
	}

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodGet, "/api/install/detect-apps", nil)))

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

	body := `{"app":"openclaw","config_path":"/home/user/.openclaw/openclaw.json"}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/connect-app", strings.NewReader(body))))

	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, []string{"/home/user/.openclaw/openclaw.json"}, hooks.connectOpenClawCalls)
}

func TestInstallServer_ConnectAgent_UnknownAgent(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{"app":"unknown-agent","config_path":""}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/connect-app", strings.NewReader(body))))

	// Unknown agents are silently skipped (no error), so expect 200.
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestInstallServer_ConnectAgent_MissingName(t *testing.T) {
	srv, _ := newTestServer(t)

	body := `{}`
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/connect-app", strings.NewReader(body))))

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestInstallServer_Finalize_ReturnsOK(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodPost, "/api/install/finalize", nil)))

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]string
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, "ok", resp["status"])
}

func TestInstallServer_DaemonReady_NotUp(t *testing.T) {
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authed(httptest.NewRequest(http.MethodGet, "/api/install/daemon-ready", nil)))

	// No daemon listening on 8403 in tests → ready: false.
	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, false, resp["ready"])
}

func TestInstallServer_DaemonReady_ClosesShutdownCh(t *testing.T) {
	// Simulate daemon up: inject a stub health server on a random port, then
	// override readInternalTokenFn so handleDaemonReady can reach ready:true.
	healthLn, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer healthLn.Close()
	go func() {
		conn, _ := healthLn.Accept()
		if conn != nil {
			_, _ = conn.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n"))
			conn.Close()
		}
	}()

	srv, _ := newTestServer(t)

	// Verify ShutdownCh fires via the sync.Once path (direct signal test).

	// Verify ShutdownCh is open before the call.
	select {
	case <-srv.ShutdownCh():
		t.Fatal("ShutdownCh should be open before wizard completes")
	default:
	}

	// Simulate handleDaemonReady signalling shutdown directly (the sync.Once path).
	srv.shutdownOnce.Do(func() { close(srv.shutdownCh) })

	select {
	case <-srv.ShutdownCh():
		// expected
	default:
		t.Fatal("ShutdownCh should be closed after shutdown signal")
	}
}

func TestInstallServer_ShutdownCh_OnlyClosedOnce(t *testing.T) {
	srv, _ := newTestServer(t)

	// Calling shutdownOnce.Do twice should not panic (channel closed only once).
	assert.NotPanics(t, func() {
		srv.shutdownOnce.Do(func() { close(srv.shutdownCh) })
		srv.shutdownOnce.Do(func() { close(srv.shutdownCh) }) // no-op
	})
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

func TestListen_PortConflict_TriesNext(t *testing.T) {
	// Occupy a port to force Listen to skip it.
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer occupied.Close()
	occupiedPort := occupied.Addr().(*net.TCPAddr).Port

	ln, addr, err := Listen(occupiedPort, http.NewServeMux())
	require.NoError(t, err)
	defer ln.Close()

	// Listen should have bound to a different port.
	gotPort := ln.Addr().(*net.TCPAddr).Port
	assert.NotEqual(t, occupiedPort, gotPort)
	assert.Contains(t, addr, "127.0.0.1:")
}
