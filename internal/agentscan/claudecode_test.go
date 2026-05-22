package agentscan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeCodeScanner_AgentMeta(t *testing.T) {
	s := ClaudeCodeScanner{}
	if s.AgentID() != "claude-code" {
		t.Errorf("AgentID = %q", s.AgentID())
	}
	if s.DisplayName() == "" {
		t.Errorf("DisplayName empty")
	}
	// DefaultConfigPath should at least produce a non-empty string ending in a
	// shell rc filename; we don't pin the exact filename because it depends on
	// the user's $SHELL environment.
	if p := s.DefaultConfigPath(); p == "" || !(strings.HasSuffix(p, "rc") || strings.HasSuffix(p, "fish") || strings.HasSuffix(p, "profile")) {
		t.Errorf("DefaultConfigPath = %q, want shell rc file path", p)
	}
}

func TestClaudeCodeScanner_Scan_MarkerPresent(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	body := `# user's rc
export PATH=/foo
# >>> krouter shell integration >>>
eval "$(krouter shell-init)"
# <<< krouter shell integration <<<
`
	if err := os.WriteFile(rcPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	endpoints, err := ClaudeCodeScanner{}.Scan(context.Background(), rcPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(endpoints))
	}
	if endpoints[0].Provider != "anthropic" {
		t.Errorf("Provider = %q", endpoints[0].Provider)
	}
	if endpoints[0].EndpointURL != "http://127.0.0.1:8402" {
		t.Errorf("EndpointURL = %q", endpoints[0].EndpointURL)
	}
	if endpoints[0].APIKey != "" {
		t.Errorf("APIKey = %q, want empty (Claude Code key is in shell env, not rc file)", endpoints[0].APIKey)
	}
}

func TestClaudeCodeScanner_Scan_NoMarker(t *testing.T) {
	dir := t.TempDir()
	rcPath := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(rcPath, []byte("# just a user rc, no krouter\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	endpoints, err := ClaudeCodeScanner{}.Scan(context.Background(), rcPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if endpoints != nil {
		t.Errorf("got endpoints %v, want nil when marker is absent", endpoints)
	}
}

func TestClaudeCodeScanner_Scan_MissingFile(t *testing.T) {
	endpoints, err := ClaudeCodeScanner{}.Scan(context.Background(), "/no/such/.zshrc")
	if err != nil {
		t.Errorf("missing file should be silent (nil, nil); got err = %v", err)
	}
	if endpoints != nil {
		t.Errorf("missing file: endpoints = %v, want nil", endpoints)
	}
}
