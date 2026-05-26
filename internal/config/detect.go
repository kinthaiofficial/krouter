package config

import (
	"os"
	"os/exec"
	"path/filepath"
)

// AppInfo describes a detected local AI app installation.
type AppInfo struct {
	Name       string `json:"name"`
	ConfigPath string `json:"config_path,omitempty"`
	CLIPath    string `json:"cli_path,omitempty"`
}

// AppStatus enriches AppInfo with live connection and provider data.
type AppStatus struct {
	AppInfo
	// Connected is true when the app is confirmed to be routing through the
	// krouter proxy (baseUrl / env var points to 127.0.0.1:8402).
	Connected bool `json:"connected"`
	// Providers lists LLM provider names found in the app's own config
	// (non-empty only for apps whose config we can read, e.g. openclaw).
	Providers []string `json:"providers,omitempty"`
}

// DetectInstalledApps scans well-known paths for installed AI apps.
// See spec/07-auto-config.md §5.
func DetectInstalledApps() []AppInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var found []AppInfo

	openclaw := filepath.Join(home, ".openclaw", "openclaw.json")
	if fileExists(openclaw) {
		found = append(found, AppInfo{Name: "openclaw", ConfigPath: openclaw})
	}

	hermes := filepath.Join(home, ".hermes", "config.toml")
	if fileExists(hermes) {
		found = append(found, AppInfo{Name: "hermes", ConfigPath: hermes})
	}

	cursor := filepath.Join(home, ".cursor", "settings.json")
	if fileExists(cursor) {
		found = append(found, AppInfo{Name: "cursor", ConfigPath: cursor})
	}

	if path, err := exec.LookPath("claude"); err == nil {
		found = append(found, AppInfo{Name: "claude-code", CLIPath: path})
	} else if path := findClaudeInKnownPaths(home); path != "" {
		found = append(found, AppInfo{Name: "claude-code", CLIPath: path})
	}

	return found
}

// DetectAppStatuses returns all detected apps enriched with connection
// status and (for supported apps) their configured LLM provider names.
// Safe to call at any time — reads config files but never writes.
func DetectAppStatuses() []AppStatus {
	apps := DetectInstalledApps()
	if len(apps) == 0 {
		return []AppStatus{}
	}

	out := make([]AppStatus, 0, len(apps))
	rcPath := DetectShellRC()

	for _, a := range apps {
		s := AppStatus{AppInfo: a}
		switch a.Name {
		case "openclaw":
			s.Connected = IsOpenClawConnected(a.ConfigPath)
			s.Providers = ReadOpenClawProviderNames(a.ConfigPath)
		case "claude-code":
			s.Connected = IsClaudeCodeConnected(rcPath)
		case "hermes", "cursor":
			// Connection detection not yet implemented for these apps.
		}
		out = append(out, s)
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// findClaudeInKnownPaths searches well-known installation paths for the claude
// binary. Used when exec.LookPath fails (e.g. daemon PATH is minimal).
func findClaudeInKnownPaths(home string) string {
	candidates := []string{
		filepath.Join(home, ".claude", "local", "claude"),   // npm install -g @anthropic-ai/claude-code
		filepath.Join(home, ".local", "bin", "claude"),
		"/usr/local/bin/claude",
		"/opt/homebrew/bin/claude",
	}
	for _, p := range candidates {
		if fileExists(p) {
			return p
		}
	}
	return ""
}
