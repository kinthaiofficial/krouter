package install

import (
	"fmt"
	"os"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
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
	detectAgentsFn      func() []config.AppInfo
	connectOpenClawFn   func(configPath string) error
	connectClaudeCodeFn func(rcPath string) error
	detectShellRCFn     func() string
	markInstalledFn     func() error
	writePendingFn      func([]agentscan.PendingAgent) error
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
		detectAgentsFn:      config.DetectInstalledApps,
		connectOpenClawFn:   config.ConnectOpenClaw,
		connectClaudeCodeFn: config.ConnectClaudeCode,
		detectShellRCFn:     config.DetectShellRC,
		markInstalledFn:     config.MarkInstalled,
		writePendingFn:      agentscan.WritePending,
	}
}

// Install runs the full install sequence.
func (o *Orchestrator) Install() error {
	// Connect agents runs BEFORE the service is registered/started: it writes
	// pending-agents.json, which the daemon consumes at startup. The reverse
	// order left the daemon's app registration empty until the next restart
	// or rescan tick — with all agent configs already pointing at the proxy,
	// every request misrouted in the meantime.
	steps := []struct {
		name string
		fn   func() error
	}{
		{"Copy binary", o.CopyBinary},
		{"Seed subscription prices", o.SeedSubPrices},
		{"Shell integration", o.ShellIntegration},
		{"Connect agents", o.ConnectAgents},
		{"Register service", o.RegisterService},
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

// RegisterService writes and enables the OS service (systemd on Linux, LaunchAgent on macOS,
// Task Scheduler on Windows).
func (o *Orchestrator) RegisterService() error {
	if o.opt.DryRun {
		return nil
	}
	binPath, err := platformDaemonPath()
	if err != nil {
		o.ui.Warn("  register service: " + err.Error())
		return nil
	}

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

// ConnectAgents patches config files for all detected AI agents and registers
// each successfully connected one with the daemon via pending-agents.json —
// the same handoff the GUI wizard uses (/api/install/apps/select). Without
// that registration the daemon's app_settings stays empty, the inheritance
// scan never runs, and every request through the rewritten configs is
// misrouted. Individual connect failures stay non-fatal, but a registration
// write failure is: it would reproduce exactly that broken state.
func (o *Orchestrator) ConnectAgents() error {
	if o.opt.DryRun || o.opt.SkipAgents {
		return nil
	}
	agents := o.detectAgentsFn()
	var pending []agentscan.PendingAgent
	for _, a := range agents {
		if err := o.connectAgent(a); err != nil {
			o.ui.Warn("  agent " + a.Name + ": " + err.Error())
			continue
		}
		if p, ok := o.pendingFor(a); ok {
			pending = append(pending, p)
		}
	}
	if len(pending) == 0 {
		return nil
	}
	if err := o.writePendingFn(pending); err != nil {
		return fmt.Errorf("register agents with daemon: %w", err)
	}
	return nil
}

func (o *Orchestrator) connectAgent(a config.AppInfo) error {
	switch a.Name {
	case "openclaw":
		return o.connectOpenClawFn(a.ConfigPath)
	case "claude-code":
		return o.connectClaudeCodeFn(o.detectShellRCFn())
	default:
		return nil
	}
}

// pendingFor maps a connected app to its pending-agents.json entry. Only apps
// the CLI actually takes over are registered; detected-but-unconnected ones
// (hermes, cursor, …) are left for the user to enable from the dashboard.
// ConfigPath is what the app's Scanner will read — for claude-code that is
// the shell rc the connect marker went into, not a JSON config.
func (o *Orchestrator) pendingFor(a config.AppInfo) (agentscan.PendingAgent, bool) {
	switch a.Name {
	case "openclaw":
		return agentscan.PendingAgent{AppID: "openclaw", Enabled: true, ConfigPath: a.ConfigPath}, true
	case "claude-code":
		return agentscan.PendingAgent{AppID: "claude-code", Enabled: true, ConfigPath: o.detectShellRCFn()}, true
	default:
		return agentscan.PendingAgent{}, false
	}
}

// MarkInstalled creates the ~/.kinthai/installed marker file.
func (o *Orchestrator) MarkInstalled() error {
	if o.opt.DryRun {
		return nil
	}
	return o.markInstalledFn()
}
