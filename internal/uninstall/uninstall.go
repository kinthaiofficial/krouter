package uninstall

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/kinthaiofficial/krouter/internal/config"
)

// Options controls what the uninstaller removes.
type Options struct {
	// KeepData skips deletion of ~/.kinthai/ (preserves logs and config).
	KeepData bool
	// DryRun prints steps without executing them.
	DryRun bool
}

// UI reports progress and warnings during uninstallation.
type UI interface {
	Progress(msg string)
	Warn(msg string)
}

// NullUI discards all output.
type NullUI struct{}

func (NullUI) Progress(_ string) {}
func (NullUI) Warn(_ string)     {}

// Uninstaller drives the uninstall sequence.
type Uninstaller struct {
	ui  UI
	opt Options

	// Injectable for testing.
	stopServiceFn          func() error
	removeServiceFileFn    func() error
	detectAgentsFn         func() []config.AgentInfo
	disconnectOpenClawFn   func(configPath string) error
	disconnectClaudeCodeFn func(rcPath string) error
	detectShellRCFn        func() string
	removeBinaryFn         func() error
	removeDataDirFn        func() error
}

// New returns a production Uninstaller.
func New(ui UI, opt Options) *Uninstaller {
	return &Uninstaller{
		ui:                     ui,
		opt:                    opt,
		stopServiceFn:          platformStopService,
		removeServiceFileFn:    platformRemoveServiceFile,
		detectAgentsFn:         config.DetectInstalledAgents,
		disconnectOpenClawFn:   config.DisconnectOpenClaw,
		disconnectClaudeCodeFn: config.DisconnectClaudeCode,
		detectShellRCFn:        config.DetectShellRC,
		removeBinaryFn:         removeBinary,
		removeDataDirFn:        removeDataDir,
	}
}

// Uninstall runs the full uninstall sequence.
func (u *Uninstaller) Uninstall() error {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"Stop service", u.StopService},
		{"Remove service file", u.RemoveServiceFile},
		{"Disconnect agents", u.DisconnectAgents},
		{"Remove shell integration", u.RemoveShellIntegration},
		{"Remove binary", u.RemoveBinary},
		{"Remove data dir", u.RemoveDataDir},
	}

	for _, s := range steps {
		u.ui.Progress(s.name + "...")
		if u.opt.DryRun {
			continue
		}
		if err := s.fn(); err != nil {
			u.ui.Warn("  " + err.Error())
		}
	}
	return nil
}

// StopService stops and disables the OS service.
func (u *Uninstaller) StopService() error {
	if u.opt.DryRun {
		return nil
	}
	if err := u.stopServiceFn(); err != nil {
		// Non-fatal: service may already be stopped.
		u.ui.Warn("  stop service: " + err.Error())
	}
	return nil
}

// RemoveServiceFile deletes the service unit / plist file.
func (u *Uninstaller) RemoveServiceFile() error {
	if u.opt.DryRun {
		return nil
	}
	if err := u.removeServiceFileFn(); err != nil {
		u.ui.Warn("  remove service file: " + err.Error())
	}
	return nil
}

// DisconnectAgents removes krouter routing config from all detected agents.
func (u *Uninstaller) DisconnectAgents() error {
	if u.opt.DryRun {
		return nil
	}
	agents := u.detectAgentsFn()
	for _, a := range agents {
		if err := u.disconnectAgent(a); err != nil {
			u.ui.Warn("  agent " + a.Name + ": " + err.Error())
		}
	}
	return nil
}

func (u *Uninstaller) disconnectAgent(a config.AgentInfo) error {
	switch a.Name {
	case "openclaw":
		return u.disconnectOpenClawFn(a.ConfigPath)
	case "claude-code":
		return u.disconnectClaudeCodeFn(u.detectShellRCFn())
	default:
		return nil
	}
}

// RemoveShellIntegration removes the krouter marker block from the shell RC file.
func (u *Uninstaller) RemoveShellIntegration() error {
	if u.opt.DryRun {
		return nil
	}
	rcPath := u.detectShellRCFn()
	if err := u.disconnectClaudeCodeFn(rcPath); err != nil {
		u.ui.Warn("  remove shell integration: " + err.Error())
	}
	return nil
}

// RemoveBinary deletes ~/.local/bin/krouter.
func (u *Uninstaller) RemoveBinary() error {
	if u.opt.DryRun {
		return nil
	}
	return u.removeBinaryFn()
}

// RemoveDataDir deletes ~/.kinthai/ unless KeepData is set.
func (u *Uninstaller) RemoveDataDir() error {
	if u.opt.DryRun || u.opt.KeepData {
		return nil
	}
	return u.removeDataDirFn()
}

func removeBinary() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("remove binary: %w", err)
	}
	dst := filepath.Join(home, ".local", "bin", "krouter")
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove binary: %w", err)
	}
	return nil
}

func removeDataDir() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("remove data dir: %w", err)
	}
	dir := filepath.Join(home, ".kinthai")
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove data dir: %w", err)
	}
	return nil
}

// platformStopService and platformRemoveServiceFile are provided by
// service_linux.go / service_darwin.go / service_other.go.

// nopStop is a no-op stop for platforms where it isn't needed.
func nopStop() error { return nil }

// removeServiceFileByPath removes a file if it exists.
func removeServiceFileByPath(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove service file: %w", err)
	}
	return nil
}

// currentOS is set to runtime.GOOS; overridable in tests.
var currentOS = runtime.GOOS
