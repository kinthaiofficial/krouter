package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/kinthaiofficial/krouter/internal/config"
)

// Version and BuildTime are injected via -ldflags at build time.
var (
	Version   = "dev"
	BuildTime = "unknown"
)

// App is the Wails application struct exposed to the frontend.
type App struct{ ctx context.Context }

// NewApp creates a new App instance.
func NewApp() *App { return &App{} }

// startup is called by Wails when the application starts.
func (a *App) startup(ctx context.Context) { a.ctx = ctx }

// GetToken reads ~/.kinthai/internal-token so the frontend can authenticate
// against the management API without the user needing to copy the token.
func (a *App) GetToken() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(home, ".kinthai", "internal-token"))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// GetVersion returns the embedded build version string.
func (a *App) GetVersion() string { return Version }

// IsFirstLaunch returns true if the daemon has not been installed yet.
func (a *App) IsFirstLaunch() bool { return !config.IsInstalled() }

// GUIAgentInfo is the JSON-serialisable agent info returned to the frontend.
type GUIAgentInfo struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

// GetInstalledAgents returns the list of detected local AI agents.
func (a *App) GetInstalledAgents() []GUIAgentInfo {
	agents := config.DetectInstalledAgents()
	out := make([]GUIAgentInfo, 0, len(agents))
	for _, ag := range agents {
		path := ag.ConfigPath
		if path == "" {
			path = ag.CLIPath
		}
		out = append(out, GUIAgentInfo{Name: ag.Name, Path: path})
	}
	return out
}

// InstallResult is the JSON-serialisable result of RunInstall.
type InstallResult struct {
	BinaryPath  string `json:"binary_path"`
	PlistPath   string `json:"plist_path"`
	ServicePath string `json:"service_path"`
	TaskName    string `json:"task_name"`
	Error       string `json:"error"`
}

// RunInstall copies the running binary to the platform-appropriate location and
// registers the platform service manager:
//   - macOS: LaunchAgent plist
//   - Linux: systemd --user service
//   - Windows: Task Scheduler user task
func (a *App) RunInstall() InstallResult {
	src, err := os.Executable()
	if err != nil {
		return InstallResult{Error: "cannot locate current executable: " + err.Error()}
	}

	binaryPath, err := config.InstallDaemon(src)
	if err != nil {
		return InstallResult{Error: "install daemon: " + err.Error()}
	}

	res := InstallResult{BinaryPath: binaryPath}

	// macOS: LaunchAgent plist.
	if plistPath, err := config.WriteLaunchAgentPlist(binaryPath); err == nil {
		res.PlistPath = plistPath
	}

	// Linux: systemd --user service.
	if servicePath, err := config.WriteSystemdService(binaryPath); err == nil {
		res.ServicePath = servicePath
	}

	// Windows: Task Scheduler user task.
	if err := config.RegisterTask(binaryPath); err == nil {
		res.TaskName = "krouter-daemon"
	}

	return res
}

// ConnectAgent modifies the given agent's config to route through kinthai.
// Returns an empty string on success, or an error message on failure.
func (a *App) ConnectAgent(name string) string {
	agents := config.DetectInstalledAgents()
	for _, ag := range agents {
		if ag.Name != name {
			continue
		}
		var err error
		switch name {
		case "openclaw":
			err = config.ConnectOpenClaw(ag.ConfigPath)
		case "cursor":
			err = config.ConnectCursor(ag.ConfigPath)
		case "hermes":
			err = config.ConnectHermes(ag.ConfigPath)
		case "claude-code":
			err = config.ConnectClaudeCode(config.DetectShellRC())
		}
		if err != nil {
			return err.Error()
		}
		return ""
	}
	return "agent not found: " + name
}

// ApplyUpdate triggers the daemon to download and apply the pending update.
// Returns "" on success or an error message. The daemon process will restart
// after the binary is replaced; the GUI should detect the disconnect and prompt
// the user to relaunch.
func (a *App) ApplyUpdate() string {
	token := a.GetToken()
	if token == "" {
		return "daemon not running"
	}
	req, err := http.NewRequestWithContext(a.ctx, http.MethodPost,
		"http://127.0.0.1:8403/internal/update-apply", nil)
	if err != nil {
		return err.Error()
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err.Error()
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Sprintf("daemon returned %d", resp.StatusCode)
	}
	return ""
}

// MarkSetupComplete marks the daemon as installed so the wizard doesn't re-run.
func (a *App) MarkSetupComplete() string {
	if err := config.MarkInstalled(); err != nil {
		return err.Error()
	}
	return ""
}
