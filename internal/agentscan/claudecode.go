package agentscan

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/kinthaiofficial/krouter/internal/config"
)

// ClaudeCodeScanner detects whether the user's shell rc file contains the
// krouter shell-integration marker block and, if so, reports that Claude Code
// is wired up to route through krouter.
//
// Unlike OpenClawScanner, this scanner does NOT extract API keys: the user's
// ANTHROPIC_API_KEY is set in the shell environment by the user's own
// scripts, and the inherit story for Claude Code is "krouter rewrites
// ANTHROPIC_BASE_URL via shell-init; the API key in the request header is
// forwarded transparently". We therefore emit a single anthropic endpoint
// pointing at the proxyBase, with no APIKey or ExtrasJSON.
//
// When the marker is absent the scanner returns nil endpoints with nil error.
// The caller can then decide whether to expose Claude Code in the dashboard
// as "configured but not connected".
type ClaudeCodeScanner struct{}

func (ClaudeCodeScanner) AgentID() string     { return "claude-code" }
func (ClaudeCodeScanner) DisplayName() string { return "Claude Code" }

func (ClaudeCodeScanner) DefaultConfigPath() string {
	// Delegate to the existing detector so we stay consistent with whatever
	// shell rc krouter writes its marker into.
	if rc := config.DetectShellRC(); rc != "" {
		return rc
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".zshrc")
	}
	return "/.zshrc"
}

// claudeCodeMarker is the begin-marker comment krouter writes into the user's
// shell rc when ConnectClaudeCode is invoked. Keep this in sync with
// internal/config/agent_claudecode.go.
const claudeCodeMarker = "# >>> krouter shell integration >>>"

// claudeCodeProxyBase mirrors internal/config.proxyBase. We re-declare it here
// to keep agentscan free of imports cycles into the routing/config packages.
const claudeCodeProxyBase = "http://127.0.0.1:8402"

func (ClaudeCodeScanner) Scan(ctx context.Context, configPath string) ([]InheritedEndpoint, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing rc file is treated as "no marker present"; we surface no
			// inherited endpoint instead of a hard error so the dashboard can
			// show Claude Code as "not detected" without an error badge.
			return nil, nil
		}
		return nil, err
	}
	if !strings.Contains(string(data), claudeCodeMarker) {
		return nil, nil
	}
	return []InheritedEndpoint{
		{
			Provider:     "anthropic",
			EndpointURL:  claudeCodeProxyBase,
			ProtocolHint: "anthropic-messages",
		},
	}, nil
}
