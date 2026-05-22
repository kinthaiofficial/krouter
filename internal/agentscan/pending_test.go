package agentscan_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPendingStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// pinPendingDir points KROUTER_CONFIG_DIR at a temp dir and returns it.
// PendingFileDir reads the env var before falling back to home/xdg paths so
// tests stay hermetic.
func pinPendingDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	old := os.Getenv("KROUTER_CONFIG_DIR")
	require.NoError(t, os.Setenv("KROUTER_CONFIG_DIR", dir))
	t.Cleanup(func() { _ = os.Setenv("KROUTER_CONFIG_DIR", old) })
	return dir
}

func TestWritePending_RoundTrips(t *testing.T) {
	dir := pinPendingDir(t)

	want := []agentscan.PendingAgent{
		{AgentID: "openclaw", Enabled: true, ConfigPath: "/u/.openclaw/openclaw.json"},
		{AgentID: "claude-code", Enabled: false, ConfigPath: "/u/.zshrc"},
	}
	require.NoError(t, agentscan.WritePending(want))

	body, err := os.ReadFile(filepath.Join(dir, agentscan.PendingFileName))
	require.NoError(t, err)

	var got struct {
		Agents []agentscan.PendingAgent `json:"agents"`
	}
	require.NoError(t, json.Unmarshal(body, &got))
	assert.Equal(t, want, got.Agents)
}

func TestImportPending_NoFileIsNoOp(t *testing.T) {
	_ = pinPendingDir(t)
	s := newPendingStore(t)

	// Must not panic, must not error visibly.
	agentscan.ImportPending(context.Background(), s, logging.New("error"))

	all, _ := s.ListAgentSettings(context.Background())
	assert.Empty(t, all)
}

func TestImportPending_AppliesSelectionsAndRemovesFile(t *testing.T) {
	dir := pinPendingDir(t)
	s := newPendingStore(t)

	// Wire a real scanner that returns a deterministic endpoint so we can
	// assert the post-import state of inherited_endpoints.
	savedScanners := agentscan.Scanners
	agentscan.Scanners = []agentscan.Scanner{
		fakeScanner{id: "openclaw", path: "/d", results: []agentscan.InheritedEndpoint{
			{Provider: "anthropic", EndpointURL: "u"},
		}},
	}
	defer func() { agentscan.Scanners = savedScanners }()

	require.NoError(t, agentscan.WritePending([]agentscan.PendingAgent{
		{AgentID: "openclaw", Enabled: true, ConfigPath: "/custom/path"},
	}))

	agentscan.ImportPending(context.Background(), s, logging.New("error"))

	// agent_settings row is present, enabled, with the path from the file.
	row, err := s.GetAgentSetting(context.Background(), "openclaw")
	require.NoError(t, err)
	require.NotNil(t, row)
	assert.True(t, row.Enabled)
	assert.Equal(t, "/custom/path", row.ConfigPath)

	// inherited_endpoints has been written by the ScanOne call.
	eps, _ := s.ListInheritedEndpointsByAgent(context.Background(), "openclaw")
	require.Len(t, eps, 1)
	assert.Equal(t, "anthropic", eps[0].Provider)

	// Pending file is removed on full success.
	_, err = os.Stat(filepath.Join(dir, agentscan.PendingFileName))
	assert.True(t, os.IsNotExist(err), "pending file should be removed after successful import")
}

func TestImportPending_DisabledClearsInheritedRows(t *testing.T) {
	_ = pinPendingDir(t)
	s := newPendingStore(t)
	ctx := context.Background()

	// Pre-seed an enabled row + endpoints that the wizard then chooses to
	// disable.
	require.NoError(t, s.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, s.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "anthropic", EndpointURL: "u", CapturedAt: 1},
	}))

	require.NoError(t, agentscan.WritePending([]agentscan.PendingAgent{
		{AgentID: "openclaw", Enabled: false, ConfigPath: "/x"},
	}))

	agentscan.ImportPending(ctx, s, logging.New("error"))

	row, _ := s.GetAgentSetting(ctx, "openclaw")
	assert.False(t, row.Enabled)
	eps, _ := s.ListInheritedEndpointsByAgent(ctx, "openclaw")
	assert.Empty(t, eps, "disable should drop inherited endpoints")
}

func TestImportPending_ScanFailureStillRemovesFile(t *testing.T) {
	// Spec/04: when a Scanner errors out (config missing, malformed, etc.) the
	// user's intent has nevertheless been persisted to agent_settings — the
	// pending file must NOT stick around, or it would silently overwrite any
	// later dashboard edits on the next daemon start.
	dir := pinPendingDir(t)
	s := newPendingStore(t)

	savedScanners := agentscan.Scanners
	agentscan.Scanners = []agentscan.Scanner{
		fakeScanner{id: "openclaw", err: assertErr("config not found")},
	}
	defer func() { agentscan.Scanners = savedScanners }()

	require.NoError(t, agentscan.WritePending([]agentscan.PendingAgent{
		{AgentID: "openclaw", Enabled: true, ConfigPath: "/missing.json"},
	}))

	agentscan.ImportPending(context.Background(), s, logging.New("error"))

	// agent_settings row still recorded, last_error captures the scan failure.
	row, _ := s.GetAgentSetting(context.Background(), "openclaw")
	require.NotNil(t, row)
	assert.True(t, row.Enabled)
	assert.NotEmpty(t, row.LastError, "ScanOne should still record the error")

	// Pending file IS removed because UpsertAgentSetting succeeded.
	_, err := os.Stat(filepath.Join(dir, agentscan.PendingFileName))
	assert.True(t, os.IsNotExist(err),
		"pending file must be removed even when ScanOne fails, otherwise it overwrites dashboard edits")
}

type assertErr string

func (e assertErr) Error() string { return string(e) }

func TestImportPending_MalformedFileGetsRenamed(t *testing.T) {
	dir := pinPendingDir(t)
	s := newPendingStore(t)

	require.NoError(t, os.MkdirAll(dir, 0o755))
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, agentscan.PendingFileName),
		[]byte("this is not JSON"), 0o644))

	agentscan.ImportPending(context.Background(), s, logging.New("error"))

	// Original removed, .malformed sibling left for debugging.
	_, err := os.Stat(filepath.Join(dir, agentscan.PendingFileName))
	assert.True(t, os.IsNotExist(err), "malformed file should be moved aside, not left")
	_, err = os.Stat(filepath.Join(dir, agentscan.PendingFileName+".malformed"))
	assert.NoError(t, err, ".malformed sibling should be preserved")
}

func TestImportPending_UnknownAgentDoesNotErrorOut(t *testing.T) {
	_ = pinPendingDir(t)
	s := newPendingStore(t)

	savedScanners := agentscan.Scanners
	agentscan.Scanners = nil // simulate downgrade: scanner not compiled in
	defer func() { agentscan.Scanners = savedScanners }()

	require.NoError(t, agentscan.WritePending([]agentscan.PendingAgent{
		{AgentID: "future-agent", Enabled: true, ConfigPath: "/x"},
	}))

	agentscan.ImportPending(context.Background(), s, logging.New("error"))

	// Setting row is still recorded so a future upgrade picks it up.
	row, _ := s.GetAgentSetting(context.Background(), "future-agent")
	require.NotNil(t, row)
	assert.True(t, row.Enabled)
}
