package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// combinedAgentJSON mirrors the JSON shape handleAgents emits — we declare
// only the fields this test inspects so the test doesn't break when the
// payload gains new fields.
type combinedAgentJSON struct {
	Name           string `json:"name"`
	Connected      bool   `json:"connected"`
	Supported      bool   `json:"supported"`
	Enabled        bool   `json:"enabled"`
	InheritedCount int    `json:"inherited_count"`
	LastError      string `json:"last_error"`
}

func TestHandleAgents_IncludesScannerAgentsEvenWhenNotDetectedOnDisk(t *testing.T) {
	// Two scanners registered; nothing actually on disk. The detection
	// loop returns nothing for these names but the Scanner registry alone
	// should put them in the output with Supported=true.
	saved := agentscan.Scanners
	agentscan.Scanners = []agentscan.Scanner{
		stubInheritScanner{id: "openclaw", path: "/tmp/oc"},
		stubInheritScanner{id: "claude-code", path: "/tmp/cc"},
	}
	t.Cleanup(func() { agentscan.Scanners = saved })

	srv, _ := newCombinedTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, combinedAuthedReq(t, http.MethodGet, "/internal/apps"))

	require.Equal(t, http.StatusOK, w.Code)

	var got []combinedAgentJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	byName := map[string]combinedAgentJSON{}
	for _, a := range got {
		byName[a.Name] = a
	}
	if a, ok := byName["openclaw"]; ok {
		assert.True(t, a.Supported, "registry-only scanner must report supported=true")
	} else {
		t.Errorf("openclaw missing from combined view; got %v", got)
	}
}

func TestHandleAgents_OverlaysInheritanceState(t *testing.T) {
	saved := agentscan.Scanners
	agentscan.Scanners = []agentscan.Scanner{
		stubInheritScanner{id: "openclaw", path: "/x"},
	}
	t.Cleanup(func() { agentscan.Scanners = saved })

	srv, store := newCombinedTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "u", CapturedAt: 1},
		{Provider: "minimax-portal", EndpointURL: "u", CapturedAt: 1},
	}))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, combinedAuthedReq(t, http.MethodGet, "/internal/apps"))

	require.Equal(t, http.StatusOK, w.Code)
	var got []combinedAgentJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	// Filter by name — host-installed agents (claude-code, etc.) may add
	// extra rows that we don't care about for this test.
	row := findAgentByName(got, "openclaw")
	require.NotNil(t, row, "openclaw must appear in combined view")
	assert.True(t, row.Supported)
	assert.True(t, row.Enabled)
	assert.Equal(t, 2, row.InheritedCount)
}

func TestHandleAgents_SurfaceLastError(t *testing.T) {
	saved := agentscan.Scanners
	agentscan.Scanners = []agentscan.Scanner{
		stubInheritScanner{id: "openclaw", path: "/wrong"},
	}
	t.Cleanup(func() { agentscan.Scanners = saved })

	srv, store := newCombinedTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertAppSetting(ctx, storage.AppSetting{
		AppID:    "openclaw",
		Enabled:    true,
		ConfigPath: "/wrong",
		LastError:  "read openclaw config: file not found",
	}))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, combinedAuthedReq(t, http.MethodGet, "/internal/apps"))

	var got []combinedAgentJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	row := findAgentByName(got, "openclaw")
	require.NotNil(t, row)
	assert.Contains(t, row.LastError, "file not found")
}

func TestHandleAgents_DetectedButNotInScannerRegistry_StaysVisible(t *testing.T) {
	// Agents that v2.0.47 filesystem detection finds on disk must remain
	// visible even when the Scanner registry is empty (e.g. a downgrade or
	// a fresh checkout before agentscan.Scanners is populated). They
	// should appear with Supported=false; users can still use the v2.0.47
	// connect/disconnect flow on them.
	saved := agentscan.Scanners
	agentscan.Scanners = nil
	t.Cleanup(func() { agentscan.Scanners = saved })

	srv, _ := newCombinedTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, combinedAuthedReq(t, http.MethodGet, "/internal/apps"))

	require.Equal(t, http.StatusOK, w.Code)
	var got []combinedAgentJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))

	// Every row must have Supported=false (empty registry); detection may
	// or may not add rows depending on the host, both are fine.
	for _, row := range got {
		assert.False(t, row.Supported,
			"row %q reported Supported=true with empty Scanner registry", row.Name)
	}
}

func findAgentByName(list []combinedAgentJSON, name string) *combinedAgentJSON {
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	return nil
}

// ── test helpers ───────────────────────────────────────────────────────────

type stubInheritScanner struct {
	id   string
	name string
	path string
}

func (s stubInheritScanner) AppID() string           { return s.id }
func (s stubInheritScanner) DisplayName() string       { return s.name }
func (s stubInheritScanner) DefaultConfigPath() string { return s.path }
func (s stubInheritScanner) Scan(_ context.Context, _ string) ([]agentscan.InheritedEndpoint, error) {
	return nil, nil
}

func newCombinedTestServer(t *testing.T) (*Server, *storage.Store) {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	srv := New(s, "v0.test", 8402, 8403)
	srv.SetTokenForTest("test-token-combined")
	return srv, s
}

func combinedAuthedReq(t *testing.T, method, path string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Authorization", "Bearer test-token-combined")
	return r
}
