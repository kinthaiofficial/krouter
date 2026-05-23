package agentscan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCursorScanner_Meta(t *testing.T) {
	s := CursorScanner{}
	if s.AgentID() != "cursor" {
		t.Errorf("AgentID = %q, want cursor", s.AgentID())
	}
	if s.DisplayName() == "" {
		t.Errorf("DisplayName empty")
	}
	if !strings.HasSuffix(s.DefaultConfigPath(), filepath.Join(".cursor", "settings.json")) {
		t.Errorf("DefaultConfigPath = %q, unexpected suffix", s.DefaultConfigPath())
	}
}

func TestCursorScanner_Scan_BothProviders(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "settings.json")

	writeJSON(t, configPath, map[string]any{
		"cursor.anthropic.baseUrl": "http://127.0.0.1:8402",
		"cursor.openai.baseUrl":    "http://127.0.0.1:8402/v1",
		"editor.fontSize":          14,
	})

	eps, err := CursorScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 2 {
		t.Fatalf("got %d endpoints, want 2", len(eps))
	}

	byProvider := map[string]InheritedEndpoint{}
	for _, ep := range eps {
		byProvider[ep.Provider] = ep
	}

	ant := byProvider["anthropic"]
	if ant.EndpointURL != "http://127.0.0.1:8402" {
		t.Errorf("anthropic.EndpointURL = %q", ant.EndpointURL)
	}
	if ant.ProtocolHint != "anthropic-messages" {
		t.Errorf("anthropic.ProtocolHint = %q", ant.ProtocolHint)
	}
	if ant.APIKey != "" {
		t.Errorf("anthropic.APIKey = %q, want empty (keychain)", ant.APIKey)
	}

	oai := byProvider["openai"]
	if oai.EndpointURL != "http://127.0.0.1:8402/v1" {
		t.Errorf("openai.EndpointURL = %q", oai.EndpointURL)
	}
	if oai.ProtocolHint != "openai-chat" {
		t.Errorf("openai.ProtocolHint = %q", oai.ProtocolHint)
	}
}

func TestCursorScanner_Scan_AnthropicOnly(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "settings.json")

	writeJSON(t, configPath, map[string]any{
		"cursor.anthropic.baseUrl": "https://api.anthropic.com",
	})

	eps, err := CursorScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].Provider != "anthropic" {
		t.Errorf("Provider = %q", eps[0].Provider)
	}
}

func TestCursorScanner_Scan_EmptyURLsSkipped(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "settings.json")

	writeJSON(t, configPath, map[string]any{
		"cursor.anthropic.baseUrl": "",
		"cursor.openai.baseUrl":    "",
	})

	eps, err := CursorScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0 (empty URLs skipped)", len(eps))
	}
}

func TestCursorScanner_Scan_FileNotFound(t *testing.T) {
	eps, err := CursorScanner{}.Scan(context.Background(), "/no/such/file.json")
	if err == nil {
		t.Errorf("expected error for missing file")
	}
	if eps != nil {
		t.Errorf("eps = %v, want nil", eps)
	}
}

func TestCursorScanner_Scan_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "settings.json")
	if err := os.WriteFile(configPath, []byte("{bad json"), 0o644); err != nil {
		t.Fatal(err)
	}
	eps, err := CursorScanner{}.Scan(context.Background(), configPath)
	if err == nil {
		t.Errorf("expected parse error")
	}
	if eps != nil {
		t.Errorf("eps = %v, want nil", eps)
	}
}
