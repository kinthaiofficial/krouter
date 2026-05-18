package config

import (
	"os"
	"os/exec"
	"path/filepath"
)

// AgentInfo describes a detected local AI agent installation.
type AgentInfo struct {
	Name       string // "openclaw" | "hermes" | "cursor" | "claude-code"
	ConfigPath string // absolute path to the agent's config file
	CLIPath    string // for claude-code: path from exec.LookPath
}

// AgentStatus enriches AgentInfo with live connection and provider data.
type AgentStatus struct {
	AgentInfo
	// Connected is true when the agent is confirmed to be routing through the
	// krouter proxy (baseUrl / env var points to 127.0.0.1:8402).
	Connected bool `json:"connected"`
	// Providers lists LLM provider names found in the agent's own config
	// (non-empty only for agents whose config we can read, e.g. openclaw).
	Providers []string `json:"providers,omitempty"`
}

// DetectInstalledAgents scans well-known paths for installed AI agents.
// See spec/07-auto-config.md §5.
func DetectInstalledAgents() []AgentInfo {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var found []AgentInfo

	openclaw := filepath.Join(home, ".openclaw", "openclaw.json")
	if fileExists(openclaw) {
		found = append(found, AgentInfo{Name: "openclaw", ConfigPath: openclaw})
	}

	hermes := filepath.Join(home, ".hermes", "config.toml")
	if fileExists(hermes) {
		found = append(found, AgentInfo{Name: "hermes", ConfigPath: hermes})
	}

	cursor := filepath.Join(home, ".cursor", "settings.json")
	if fileExists(cursor) {
		found = append(found, AgentInfo{Name: "cursor", ConfigPath: cursor})
	}

	if path, err := exec.LookPath("claude"); err == nil {
		found = append(found, AgentInfo{Name: "claude-code", CLIPath: path})
	}

	return found
}

// DetectAgentStatuses returns all detected agents enriched with connection
// status and (for supported agents) their configured LLM provider names.
// Safe to call at any time — reads config files but never writes.
func DetectAgentStatuses() []AgentStatus {
	agents := DetectInstalledAgents()
	if len(agents) == 0 {
		return []AgentStatus{}
	}

	out := make([]AgentStatus, 0, len(agents))
	rcPath := DetectShellRC()

	for _, a := range agents {
		s := AgentStatus{AgentInfo: a}
		switch a.Name {
		case "openclaw":
			s.Connected = IsOpenClawConnected(a.ConfigPath)
			s.Providers = ReadOpenClawProviderNames(a.ConfigPath)
		case "claude-code":
			s.Connected = IsClaudeCodeConnected(rcPath)
		case "hermes", "cursor":
			// Connection detection not yet implemented for these agents.
		}
		out = append(out, s)
	}
	return out
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
