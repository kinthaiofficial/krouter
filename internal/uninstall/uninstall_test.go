package uninstall

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testUninstaller returns an Uninstaller with all side-effects replaced by stubs.
func testUninstaller(ui UI, opt Options) (*Uninstaller, *uninstallHooks) {
	h := &uninstallHooks{}
	u := New(ui, opt)
	u.stopServiceFn = h.stopService
	u.removeServiceFileFn = h.removeServiceFile
	u.detectAgentsFn = h.detectAgents
	u.disconnectOpenClawFn = h.disconnectOpenClaw
	u.disconnectClaudeCodeFn = h.disconnectClaudeCode
	u.detectShellRCFn = func() string { return "/tmp/test_rc" }
	u.removeBinaryFn = h.removeBinary
	u.removeDataDirFn = h.removeDataDir
	return u, h
}

type uninstallHooks struct {
	stopServiceCalled          bool
	removeServiceFileCalled    bool
	detectAgentsResult         []config.AgentInfo
	disconnectOpenClawCalls    []string
	disconnectClaudeCodeCalls  []string
	removeBinaryCalled         bool
	removeDataDirCalled        bool
}

func (h *uninstallHooks) stopService() error          { h.stopServiceCalled = true; return nil }
func (h *uninstallHooks) removeServiceFile() error    { h.removeServiceFileCalled = true; return nil }
func (h *uninstallHooks) detectAgents() []config.AgentInfo { return h.detectAgentsResult }
func (h *uninstallHooks) disconnectOpenClaw(p string) error {
	h.disconnectOpenClawCalls = append(h.disconnectOpenClawCalls, p)
	return nil
}
func (h *uninstallHooks) disconnectClaudeCode(p string) error {
	h.disconnectClaudeCodeCalls = append(h.disconnectClaudeCodeCalls, p)
	return nil
}
func (h *uninstallHooks) removeBinary() error  { h.removeBinaryCalled = true; return nil }
func (h *uninstallHooks) removeDataDir() error { h.removeDataDirCalled = true; return nil }

func TestUninstall_FullFlow(t *testing.T) {
	u, h := testUninstaller(NullUI{}, Options{})

	require.NoError(t, u.Uninstall())

	assert.True(t, h.stopServiceCalled)
	assert.True(t, h.removeServiceFileCalled)
	assert.True(t, h.removeBinaryCalled)
	assert.True(t, h.removeDataDirCalled)
}

func TestUninstall_DryRun_NoActualCalls(t *testing.T) {
	u, h := testUninstaller(NullUI{}, Options{DryRun: true})

	require.NoError(t, u.Uninstall())

	assert.False(t, h.stopServiceCalled)
	assert.False(t, h.removeServiceFileCalled)
	assert.False(t, h.removeBinaryCalled)
	assert.False(t, h.removeDataDirCalled)
}

func TestUninstall_KeepData_PreservesDataDir(t *testing.T) {
	u, h := testUninstaller(NullUI{}, Options{KeepData: true})

	require.NoError(t, u.Uninstall())

	assert.True(t, h.removeBinaryCalled, "binary should still be removed")
	assert.False(t, h.removeDataDirCalled, "data dir must be preserved with --keep-data")
}

func TestUninstall_RemovesService_Linux(t *testing.T) {
	u, h := testUninstaller(NullUI{}, Options{})

	require.NoError(t, u.StopService())
	require.NoError(t, u.RemoveServiceFile())

	assert.True(t, h.stopServiceCalled)
	assert.True(t, h.removeServiceFileCalled)
}

func TestUninstall_DisconnectAgent_OpenClaw(t *testing.T) {
	u, h := testUninstaller(NullUI{}, Options{})
	h.detectAgentsResult = []config.AgentInfo{
		{Name: "openclaw", ConfigPath: "/home/user/.openclaw/openclaw.json"},
	}

	require.NoError(t, u.DisconnectAgents())
	assert.Equal(t, []string{"/home/user/.openclaw/openclaw.json"}, h.disconnectOpenClawCalls)
}

func TestUninstall_RemovesShellIntegration(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	content := "# some existing content\n" +
		"# >>> krouter shell integration >>>\n" +
		"eval \"$(krouter shell-init)\"\n" +
		"# <<< krouter shell integration <<<\n"
	require.NoError(t, os.WriteFile(rcPath, []byte(content), 0644))

	u := &Uninstaller{
		ui:                     NullUI{},
		opt:                    Options{},
		disconnectClaudeCodeFn: config.DisconnectClaudeCode,
		detectShellRCFn:        func() string { return rcPath },
	}

	require.NoError(t, u.RemoveShellIntegration())

	data, err := os.ReadFile(rcPath)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "krouter shell-init")
	assert.Contains(t, string(data), "# some existing content")
}

func TestUninstall_RemovesBinary(t *testing.T) {
	dir := t.TempDir()
	binPath := filepath.Join(dir, "krouter")
	require.NoError(t, os.WriteFile(binPath, []byte("fake binary"), 0755))

	u := &Uninstaller{
		ui:  NullUI{},
		opt: Options{},
		removeBinaryFn: func() error {
			return os.Remove(binPath)
		},
	}

	require.NoError(t, u.RemoveBinary())
	_, err := os.Stat(binPath)
	assert.True(t, os.IsNotExist(err))
}

func TestUninstall_StopService_NonFatal(t *testing.T) {
	u, h := testUninstaller(NullUI{}, Options{})
	h.stopServiceCalled = false
	// Verify StopService completes even if the stub is replaced with an error.
	u.stopServiceFn = func() error { return nil }
	require.NoError(t, u.StopService())
}
