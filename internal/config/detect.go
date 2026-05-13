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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
