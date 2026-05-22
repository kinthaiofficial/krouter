package agentscan

import (
	"context"
	"testing"
)

// stubScanner is a no-op Scanner used to exercise the registry helpers.
type stubScanner struct{ id, name, path string }

func (s stubScanner) AgentID() string           { return s.id }
func (s stubScanner) DisplayName() string       { return s.name }
func (s stubScanner) DefaultConfigPath() string { return s.path }
func (s stubScanner) Scan(_ context.Context, _ string) ([]InheritedEndpoint, error) {
	return nil, nil
}

func TestRegistryGetAndIDs(t *testing.T) {
	saved := Scanners
	defer func() { Scanners = saved }()

	Scanners = []Scanner{
		stubScanner{id: "openclaw", name: "OpenClaw", path: "/tmp/oc"},
		stubScanner{id: "claude-code", name: "Claude Code", path: "/tmp/cc"},
	}

	if got := Get("openclaw"); got == nil || got.DisplayName() != "OpenClaw" {
		t.Errorf("Get(openclaw) = %v, want OpenClaw", got)
	}
	if got := Get("does-not-exist"); got != nil {
		t.Errorf("Get(does-not-exist) = %v, want nil", got)
	}

	ids := IDs()
	if len(ids) != 2 || ids[0] != "openclaw" || ids[1] != "claude-code" {
		t.Errorf("IDs() = %v, want [openclaw claude-code]", ids)
	}
}

func TestRegistryEmpty(t *testing.T) {
	saved := Scanners
	defer func() { Scanners = saved }()

	Scanners = nil
	if got := Get("anything"); got != nil {
		t.Errorf("Get on empty registry = %v, want nil", got)
	}
	if ids := IDs(); len(ids) != 0 {
		t.Errorf("IDs() on empty registry = %v, want empty", ids)
	}
}
