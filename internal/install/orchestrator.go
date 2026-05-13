package install

import (
	"fmt"
	"os"

	"github.com/kinthaiofficial/krouter/internal/config"
)

// UI reports progress and requests user confirmation during installation.
type UI interface {
	Progress(msg string)
	Confirm(question string) bool
	Warn(msg string)
}

// NullUI silently auto-confirms everything. Used for --yes / headless mode.
type NullUI struct{}

func (NullUI) Progress(_ string)     {}
func (NullUI) Confirm(_ string) bool { return true }
func (NullUI) Warn(_ string)         {}

// Options controls what the Orchestrator does.
type Options struct {
	// SrcBinary is the binary to install. Defaults to os.Executable() if empty.
	SrcBinary string
	// DryRun prints steps without executing them.
	DryRun bool
	// SkipAgents skips the agent-connection step.
	SkipAgents bool
}

// Orchestrator drives the install sequence.
type Orchestrator struct {
	ui  UI
	opt Options

	// Injectable for testing.
	installDaemonFn     func(src string) (string, error)
	writeServiceFn      func(binaryPath string) (string, error)
	enableServiceFn     func() error
	writeShellRCFn      func(rcPath string) error
	detectAgentsFn      func() []config.AgentInfo
	connectOpenClawFn   func(configPath string) error
	connectClaudeCodeFn func(rcPath string) error
	detectShellRCFn     func() string
	markInstalledFn     func() error
}

// New returns a production Orchestrator backed by real config functions.
func New(ui UI, opt Options) *Orchestrator {
	return &Orchestrator{
		ui:                  ui,
		opt:                 opt,
		installDaemonFn:     config.InstallDaemon,
		writeServiceFn:      platformWriteService,
		enableServiceFn:     platformEnableService,
		writeShellRCFn:      config.ConnectClaudeCode,
		detectAgentsFn:      config.DetectInstalledAgents,
		connectOpenClawFn:   config.ConnectOpenClaw,
		connectClaudeCodeFn: config.ConnectClaudeCode,
		detectShellRCFn:     config.DetectShellRC,
		markInstalledFn:     config.MarkInstalled,
	}
}

// Install runs the full install sequence.
func (o *Orchestrator) Install() error {
	steps := []struct {
		name string
		fn   func() error
	}{
		{"Copy binary", o.CopyBinary},
		{"Register service", o.RegisterService},
		{"Shell integration", o.ShellIntegration},
		{"Connect agents", o.ConnectAgents},
		{"Mark installed", o.MarkInstalled},
	}

	for _, s := range steps {
		o.ui.Progress(s.name + "...")
		if o.opt.DryRun {
			continue
		}
		if err := s.fn(); err != nil {
			return fmt.Errorf("install: %s: %w", s.name, err)
		}
	}
	return nil
}

// CopyBinary copies the krouter binary to ~/.local/bin/krouter.
func (o *Orchestrator) CopyBinary() error {
	if o.opt.DryRun {
		return nil
	}
	src := o.opt.SrcBinary
	if src == "" {
		var err error
		src, err = os.Executable()
		if err != nil {
			return fmt.Errorf("find executable: %w", err)
		}
	}
	dst, err := o.installDaemonFn(src)
	if err != nil {
		return err
	}
	o.ui.Progress("  → " + dst)
	return nil
}

// RegisterService writes and enables the OS service (systemd on Linux, LaunchAgent on macOS).
func (o *Orchestrator) RegisterService() error {
	if o.opt.DryRun {
		return nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	binPath := home + "/.local/bin/krouter"

	svcPath, err := o.writeServiceFn(binPath)
	if err != nil {
		// On Windows or unsupported platforms the service write is a no-op.
		o.ui.Warn("  register service: " + err.Error())
		return nil
	}
	o.ui.Progress("  → " + svcPath)

	if err := o.enableServiceFn(); err != nil {
		o.ui.Warn("  enable service: " + err.Error())
	}
	return nil
}

// ShellIntegration appends the krouter shell block to the user's RC file.
func (o *Orchestrator) ShellIntegration() error {
	if o.opt.DryRun {
		return nil
	}
	rcPath := o.detectShellRCFn()
	if err := o.writeShellRCFn(rcPath); err != nil {
		return err
	}
	o.ui.Progress("  → " + rcPath)
	return nil
}

// ConnectAgents patches config files for all detected AI agents.
func (o *Orchestrator) ConnectAgents() error {
	if o.opt.DryRun || o.opt.SkipAgents {
		return nil
	}
	agents := o.detectAgentsFn()
	for _, a := range agents {
		if err := o.connectAgent(a); err != nil {
			o.ui.Warn("  agent " + a.Name + ": " + err.Error())
		}
	}
	return nil
}

func (o *Orchestrator) connectAgent(a config.AgentInfo) error {
	switch a.Name {
	case "openclaw":
		return o.connectOpenClawFn(a.ConfigPath)
	case "claude-code":
		return o.connectClaudeCodeFn(o.detectShellRCFn())
	default:
		return nil
	}
}

// MarkInstalled creates the ~/.kinthai/installed marker file.
func (o *Orchestrator) MarkInstalled() error {
	if o.opt.DryRun {
		return nil
	}
	return o.markInstalledFn()
}
