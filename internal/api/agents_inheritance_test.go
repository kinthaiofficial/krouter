package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeScanner struct {
	id      string
	name    string
	path    string
	results []agentscan.InheritedEndpoint
	err     error
}

func (f fakeScanner) AgentID() string           { return f.id }
func (f fakeScanner) DisplayName() string       { return f.name }
func (f fakeScanner) DefaultConfigPath() string { return f.path }
func (f fakeScanner) Scan(_ context.Context, _ string) ([]agentscan.InheritedEndpoint, error) {
	return f.results, f.err
}

func setScannersFor(t *testing.T, ss ...agentscan.Scanner) {
	t.Helper()
	saved := agentscan.Scanners
	agentscan.Scanners = ss
	t.Cleanup(func() { agentscan.Scanners = saved })
}

func newTestServer(t *testing.T) (*Server, *storage.Store) {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	srv := New(s, "v0.test", 8402, 8403)
	srv.SetTokenForTest("test-token")
	return srv, s
}

func authedReq(t *testing.T, method, path, body string) *http.Request {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	r.Header.Set("Authorization", "Bearer test-token")
	return r
}

// ─── GET supported ─────────────────────────────────────────────────────────

func TestAgentsSupported_ReturnsScannerRegistry(t *testing.T) {
	setScannersFor(t,
		fakeScanner{id: "openclaw", name: "OpenClaw", path: "/tmp/oc"},
		fakeScanner{id: "claude-code", name: "Claude Code", path: "/tmp/cc"},
	)

	srv, _ := newTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodGet, "/internal/agents/supported", ""))

	require.Equal(t, http.StatusOK, w.Code)
	var got []supportedAgentJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 2)
	assert.Equal(t, "openclaw", got[0].AgentID)
	assert.Equal(t, "OpenClaw", got[0].DisplayName)
	assert.Equal(t, "/tmp/oc", got[0].DefaultPath)
}

// ─── GET configured ────────────────────────────────────────────────────────

func TestAgentsConfigured_EmptyWhenNoRows(t *testing.T) {
	setScannersFor(t /* registry intentionally unused for this call */)
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodGet, "/internal/agents/configured", ""))
	require.Equal(t, http.StatusOK, w.Code)

	var got []configuredAgentJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Empty(t, got)
}

func TestAgentsConfigured_ReturnsRowsWithInheritedCount(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "u1", CapturedAt: 1},
		{Provider: "minimax-portal", EndpointURL: "u2", CapturedAt: 1},
	}))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodGet, "/internal/agents/configured", ""))
	require.Equal(t, http.StatusOK, w.Code)

	var got []configuredAgentJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "openclaw", got[0].AgentID)
	assert.True(t, got[0].Enabled)
	assert.Equal(t, 2, got[0].InheritedCount)
}

// ─── POST {id}/rescan ──────────────────────────────────────────────────────

func TestAgentRescan_WritesInheritedRows(t *testing.T) {
	setScannersFor(t, fakeScanner{
		id: "openclaw", name: "OpenClaw", path: "/default",
		results: []agentscan.InheritedEndpoint{
			{Provider: "anthropic", EndpointURL: "https://api.anthropic.com"},
		},
	})
	srv, store := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodPost,
		"/internal/agents/openclaw/rescan", `{"path":"/custom/path"}`))

	require.Equal(t, http.StatusOK, w.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, true, body["ok"])
	assert.Equal(t, "/custom/path", body["config_path"])
	assert.Equal(t, float64(1), body["inherited_count"])

	// agent_settings row was created with the custom path
	row, _ := store.GetAgentSetting(context.Background(), "openclaw")
	require.NotNil(t, row)
	assert.True(t, row.Enabled, "first-time rescan should default enabled=true")
	assert.Equal(t, "/custom/path", row.ConfigPath)
}

func TestAgentRescan_UnknownAgent(t *testing.T) {
	setScannersFor(t /* empty */)
	srv, _ := newTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodPost,
		"/internal/agents/unknown-vendor/rescan", `{}`))

	assert.Equal(t, http.StatusNotFound, w.Code)
}

// ─── POST {id}/enable | /disable ───────────────────────────────────────────

func TestAgentEnableDisable(t *testing.T) {
	setScannersFor(t, fakeScanner{id: "openclaw", name: "OpenClaw", path: "/d"})
	srv, store := newTestServer(t)

	// enable from scratch (no prior row)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodPost,
		"/internal/agents/openclaw/enable", ""))
	require.Equal(t, http.StatusOK, w.Code)

	row, _ := store.GetAgentSetting(context.Background(), "openclaw")
	require.NotNil(t, row)
	assert.True(t, row.Enabled)
	assert.Equal(t, "/d", row.ConfigPath, "should use scanner default")

	// pre-seed some endpoints to verify disable wipes them
	require.NoError(t, store.ReplaceInheritedEndpoints(context.Background(), "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "u", CapturedAt: 1},
	}))

	w = httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodPost,
		"/internal/agents/openclaw/disable", ""))
	require.Equal(t, http.StatusOK, w.Code)

	row, _ = store.GetAgentSetting(context.Background(), "openclaw")
	assert.False(t, row.Enabled)

	eps, _ := store.ListInheritedEndpointsByAgent(context.Background(), "openclaw")
	assert.Empty(t, eps, "disable should clear inherited endpoints")
}

// ─── DELETE {id} ───────────────────────────────────────────────────────────

func TestAgentDelete_RemovesRow(t *testing.T) {
	srv, store := newTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "u", CapturedAt: 1},
	}))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodDelete, "/internal/agents/openclaw", ""))
	require.Equal(t, http.StatusOK, w.Code)

	row, _ := store.GetAgentSetting(ctx, "openclaw")
	assert.Nil(t, row, "agent_settings row should be gone")

	eps, _ := store.ListInheritedEndpointsByAgent(ctx, "openclaw")
	assert.Empty(t, eps, "inherited endpoints should be gone (manual cascade)")
}

// ─── Existing v2.0.47 verbs still work ─────────────────────────────────────

func TestAgentAction_LegacyVerbsStillReachableUnderNewDispatch(t *testing.T) {
	srv, _ := newTestServer(t)

	// POST /internal/agents/openclaw/connect — still wired. No real OpenClaw is
	// installed in the test environment, so the handler returns a JSON error body.
	// We verify dispatch reached doAgentConnect (body contains "agent not found")
	// rather than a routing-level 404 (which would have no JSON body).
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, authedReq(t, http.MethodPost,
		"/internal/agents/openclaw/connect", ""))
	assert.Contains(t, w.Body.String(), "agent not found",
		"connect verb must reach its handler, not a routing 404; got %d body=%s", w.Code, w.Body.String())
}
