package install

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recordingUI captures progress/warn calls for assertions.
type recordingUI struct {
	progress []string
	warns    []string
	confirm  bool
}

func (u *recordingUI) Progress(msg string) { u.progress = append(u.progress, msg) }
func (u *recordingUI) Warn(msg string)     { u.warns = append(u.warns, msg) }
func (u *recordingUI) Confirm(_ string) bool { return u.confirm }

// testOrchestrator returns an Orchestrator with all side-effects replaced by no-ops / stubs.
func testOrchestrator(ui UI, opt Options) (*Orchestrator, *testHooks) {
	h := &testHooks{}
	o := New(ui, opt)
	o.installDaemonFn = h.installDaemon
	o.writeServiceFn = h.writeService
	o.enableServiceFn = h.enableService
	o.writeShellRCFn = h.writeShellRC
	o.detectAgentsFn = h.detectAgents
	o.connectOpenClawFn = h.connectOpenClaw
	o.connectClaudeCodeFn = h.connectClaudeCode
	o.detectShellRCFn = func() string { return "/tmp/test_rc" }
	o.markInstalledFn = h.markInstalled
	o.writePendingFn = h.writePending
	return o, h
}

type testHooks struct {
	installDaemonCalls     []string
	writeServiceCalls      []string
	enableServiceCalled    bool
	writeShellRCCalls      []string
	detectAgentsResult     []config.AppInfo
	connectOpenClawCalls   []string
	connectClaudeCodeCalls []string
	markInstalledCalled    bool
	writePendingCalls      [][]agentscan.PendingAgent

	// order records the sequence of side-effects that matter for the
	// "registration must land before the daemon starts" invariant.
	order []string

	installDaemonErr  error
	writeServiceErr   error
	enableServiceErr  error
	writeShellRCErr   error
	connectOpenClawErr  error
	connectClaudeCodeErr error
	writePendingErr   error
}

func (h *testHooks) installDaemon(src string) (string, error) {
	h.installDaemonCalls = append(h.installDaemonCalls, src)
	if h.installDaemonErr != nil {
		return "", h.installDaemonErr
	}
	return "/home/user/.local/bin/krouter", nil
}

func (h *testHooks) writeService(binPath string) (string, error) {
	h.writeServiceCalls = append(h.writeServiceCalls, binPath)
	if h.writeServiceErr != nil {
		return "", h.writeServiceErr
	}
	return "/etc/systemd/user/krouter.service", nil
}

func (h *testHooks) enableService() error {
	h.order = append(h.order, "enableService")
	h.enableServiceCalled = true
	return h.enableServiceErr
}

func (h *testHooks) writeShellRC(rcPath string) error {
	h.writeShellRCCalls = append(h.writeShellRCCalls, rcPath)
	return h.writeShellRCErr
}

func (h *testHooks) detectAgents() []config.AppInfo {
	return h.detectAgentsResult
}

func (h *testHooks) connectOpenClaw(configPath string) error {
	h.connectOpenClawCalls = append(h.connectOpenClawCalls, configPath)
	return h.connectOpenClawErr
}

func (h *testHooks) connectClaudeCode(rcPath string) error {
	h.connectClaudeCodeCalls = append(h.connectClaudeCodeCalls, rcPath)
	return h.connectClaudeCodeErr
}

func (h *testHooks) markInstalled() error {
	h.markInstalledCalled = true
	return nil
}

func (h *testHooks) writePending(agents []agentscan.PendingAgent) error {
	h.order = append(h.order, "writePending")
	h.writePendingCalls = append(h.writePendingCalls, agents)
	return h.writePendingErr
}

func TestOrchestrator_FullFlow_NullUI(t *testing.T) {
	o, h := testOrchestrator(NullUI{}, Options{SrcBinary: "/tmp/krouter-src"})

	err := o.Install()
	require.NoError(t, err)

	assert.Equal(t, []string{"/tmp/krouter-src"}, h.installDaemonCalls)
	assert.True(t, h.enableServiceCalled)
	assert.Equal(t, []string{"/tmp/test_rc"}, h.writeShellRCCalls)
	assert.True(t, h.markInstalledCalled)
}

func TestOrchestrator_DryRun_NoActualCalls(t *testing.T) {
	ui := &recordingUI{confirm: true}
	o, h := testOrchestrator(ui, Options{DryRun: true})

	err := o.Install()
	require.NoError(t, err)

	assert.Empty(t, h.installDaemonCalls, "dry-run must not copy binary")
	assert.False(t, h.enableServiceCalled, "dry-run must not enable service")
	assert.Empty(t, h.writeShellRCCalls, "dry-run must not write shell rc")
	assert.False(t, h.markInstalledCalled, "dry-run must not mark installed")
	assert.Greater(t, len(ui.progress), 0, "dry-run should still emit progress messages")
}

func TestOrchestrator_CopyBinary_UsesProvidedSrc(t *testing.T) {
	o, h := testOrchestrator(NullUI{}, Options{SrcBinary: "/custom/path/krouter"})

	err := o.CopyBinary()
	require.NoError(t, err)
	assert.Equal(t, []string{"/custom/path/krouter"}, h.installDaemonCalls)
}

func TestOrchestrator_CopyBinary_DefaultsToExecutable(t *testing.T) {
	o, h := testOrchestrator(NullUI{}, Options{})

	err := o.CopyBinary()
	require.NoError(t, err)
	// Should use os.Executable() — just verify it was called with some non-empty path.
	require.Len(t, h.installDaemonCalls, 1)
	assert.NotEmpty(t, h.installDaemonCalls[0])
}

func TestOrchestrator_CopyBinary_PropagatesError(t *testing.T) {
	o, h := testOrchestrator(NullUI{}, Options{SrcBinary: "/tmp/src"})
	h.installDaemonErr = errors.New("disk full")

	err := o.CopyBinary()
	assert.ErrorContains(t, err, "disk full")
}

func TestOrchestrator_RegisterService_Linux(t *testing.T) {
	o, h := testOrchestrator(NullUI{}, Options{})

	err := o.RegisterService()
	require.NoError(t, err)
	assert.Len(t, h.writeServiceCalls, 1)
	assert.True(t, h.enableServiceCalled)
}

func TestOrchestrator_RegisterService_WriteError_Warns(t *testing.T) {
	ui := &recordingUI{}
	o, h := testOrchestrator(ui, Options{})
	h.writeServiceErr = errors.New("unsupported")

	err := o.RegisterService()
	require.NoError(t, err, "service write error must be non-fatal")
	assert.NotEmpty(t, ui.warns)
	assert.False(t, h.enableServiceCalled, "enable must be skipped when write fails")
}

func TestOrchestrator_RegisterService_EnableError_Warns(t *testing.T) {
	ui := &recordingUI{}
	o, h := testOrchestrator(ui, Options{})
	h.enableServiceErr = errors.New("systemctl not found")

	err := o.RegisterService()
	require.NoError(t, err)
	assert.NotEmpty(t, ui.warns)
}

func TestOrchestrator_ShellIntegration_Zsh(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")

	o := &Orchestrator{
		ui:  NullUI{},
		opt: Options{},
		writeShellRCFn:  config.ConnectClaudeCode,
		detectShellRCFn: func() string { return rcPath },
	}

	err := o.ShellIntegration()
	require.NoError(t, err)

	data, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "krouter shell-init")
}

func TestOrchestrator_ShellIntegration_Idempotent(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")

	o := &Orchestrator{
		ui:  NullUI{},
		opt: Options{},
		writeShellRCFn:  config.ConnectClaudeCode,
		detectShellRCFn: func() string { return rcPath },
	}

	require.NoError(t, o.ShellIntegration())
	require.NoError(t, o.ShellIntegration())

	data, _ := os.ReadFile(rcPath)
	count := 0
	for i := 0; i < len(data)-len("krouter shell-init")+1; i++ {
		if string(data[i:i+len("krouter shell-init")]) == "krouter shell-init" {
			count++
		}
	}
	assert.Equal(t, 1, count, "shell block must appear exactly once")
}

func TestOrchestrator_ShellIntegration_Fish(t *testing.T) {
	dir := t.TempDir()
	fishPath := filepath.Join(dir, ".config", "fish", "config.fish")

	o := &Orchestrator{
		ui:              NullUI{},
		opt:             Options{},
		writeShellRCFn:  config.ConnectClaudeCode,
		detectShellRCFn: func() string { return fishPath },
	}

	err := o.ShellIntegration()
	require.NoError(t, err)

	data, err := os.ReadFile(fishPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "krouter shell-init")
}

func TestOrchestrator_ConnectAgent_OpenClaw(t *testing.T) {
	o, h := testOrchestrator(NullUI{}, Options{})
	h.detectAgentsResult = []config.AppInfo{
		{Name: "openclaw", ConfigPath: "/home/user/.openclaw/openclaw.json"},
	}

	err := o.ConnectAgents()
	require.NoError(t, err)
	assert.Equal(t, []string{"/home/user/.openclaw/openclaw.json"}, h.connectOpenClawCalls)
}

func TestOrchestrator_ConnectAgent_ClaudeCode(t *testing.T) {
	o, h := testOrchestrator(NullUI{}, Options{})
	h.detectAgentsResult = []config.AppInfo{
		{Name: "claude-code", CLIPath: "/usr/local/bin/claude"},
	}

	err := o.ConnectAgents()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/test_rc"}, h.connectClaudeCodeCalls)
}

func TestOrchestrator_ConnectAgents_NonFatalOnError(t *testing.T) {
	ui := &recordingUI{}
	o, h := testOrchestrator(ui, Options{})
	h.detectAgentsResult = []config.AppInfo{
		{Name: "openclaw", ConfigPath: "/path/to/openclaw.json"},
	}
	h.connectOpenClawErr = errors.New("config not writable")

	err := o.ConnectAgents()
	require.NoError(t, err, "agent connect errors must be non-fatal")
	assert.NotEmpty(t, ui.warns)
}

func TestOrchestrator_ConnectAgents_SkipAgents(t *testing.T) {
	o, h := testOrchestrator(NullUI{}, Options{SkipAgents: true})
	h.detectAgentsResult = []config.AppInfo{
		{Name: "openclaw", ConfigPath: "/path/to/openclaw.json"},
	}

	err := o.ConnectAgents()
	require.NoError(t, err)
	assert.Empty(t, h.connectOpenClawCalls, "SkipAgents must prevent connecting agents")
}

func TestOrchestrator_DaemonAlreadyInstalled_MarkInstalledIdempotent(t *testing.T) {
	dir := t.TempDir()
	// Pre-create the marker file.
	_ = os.WriteFile(filepath.Join(dir, "installed"), nil, 0600)

	called := false
	o, _ := testOrchestrator(NullUI{}, Options{})
	o.markInstalledFn = func() error {
		called = true
		return nil
	}

	require.NoError(t, o.MarkInstalled())
	assert.True(t, called)
}

// ─── App registration (pending-agents.json) ─────────────────────────────────
//
// Connecting an agent only rewrites its config file; without a matching
// pending-agents.json entry the daemon's app_settings stays empty, no
// endpoints are inherited, and every proxied request is misrouted. The GUI
// wizard registers via /api/install/apps/select; the CLI must do the same.

func TestOrchestrator_ConnectAgents_RegistersConnectedAgents(t *testing.T) {
	o, h := testOrchestrator(NullUI{}, Options{})
	h.detectAgentsResult = []config.AppInfo{
		{Name: "openclaw", ConfigPath: "/home/u/.openclaw/openclaw.json"},
		{Name: "claude-code"},
	}

	require.NoError(t, o.ConnectAgents())

	require.Len(t, h.writePendingCalls, 1, "pending-agents.json must be written once")
	assert.ElementsMatch(t, []agentscan.PendingAgent{
		{AppID: "openclaw", Enabled: true, ConfigPath: "/home/u/.openclaw/openclaw.json"},
		{AppID: "claude-code", Enabled: true, ConfigPath: "/tmp/test_rc"},
	}, h.writePendingCalls[0])
}

func TestOrchestrator_ConnectAgents_FailedConnectNotRegistered(t *testing.T) {
	ui := &recordingUI{}
	o, h := testOrchestrator(ui, Options{})
	h.detectAgentsResult = []config.AppInfo{
		{Name: "openclaw", ConfigPath: "/path/to/openclaw.json"},
	}
	h.connectOpenClawErr = errors.New("config not writable")

	require.NoError(t, o.ConnectAgents())
	assert.Empty(t, h.writePendingCalls, "an agent that failed to connect must not be registered")
}

func TestOrchestrator_ConnectAgents_UnconnectedAgentsNotRegistered(t *testing.T) {
	// Detected-but-not-connected apps (hermes, cursor, …) are not taken over
	// by the CLI, so they must not appear in pending-agents.json either.
	o, h := testOrchestrator(NullUI{}, Options{})
	h.detectAgentsResult = []config.AppInfo{
		{Name: "hermes", ConfigPath: "/home/u/.hermes/config.toml"},
	}

	require.NoError(t, o.ConnectAgents())
	assert.Empty(t, h.writePendingCalls)
}

func TestOrchestrator_ConnectAgents_PendingWriteFailureIsFatal(t *testing.T) {
	// A silent registration failure reproduces the P0 (connected but never
	// scanned → all traffic misrouted), so it must fail the install step.
	o, h := testOrchestrator(NullUI{}, Options{})
	h.detectAgentsResult = []config.AppInfo{
		{Name: "openclaw", ConfigPath: "/home/u/.openclaw/openclaw.json"},
	}
	h.writePendingErr = errors.New("disk full")

	err := o.ConnectAgents()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disk full")
}

func TestOrchestrator_Install_RegistrationLandsBeforeServiceStart(t *testing.T) {
	// The daemon consumes pending-agents.json at startup; the CLI must write
	// it before the service is enabled so a fresh install is correct from the
	// first request, not after the next restart or rescan tick.
	o, h := testOrchestrator(NullUI{}, Options{SrcBinary: "/tmp/krouter-src"})
	h.detectAgentsResult = []config.AppInfo{
		{Name: "openclaw", ConfigPath: "/home/u/.openclaw/openclaw.json"},
	}

	require.NoError(t, o.Install())
	require.Equal(t, []string{"writePending", "enableService"}, h.order,
		"pending-agents.json must exist before the daemon starts")
}
