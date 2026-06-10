package agentscan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexScanner_Meta(t *testing.T) {
	s := CodexScanner{}
	if s.AppID() != "codex" {
		t.Errorf("AppID = %q, want codex", s.AppID())
	}
	if s.DisplayName() == "" {
		t.Errorf("DisplayName empty")
	}
	if !strings.HasSuffix(s.DefaultConfigPath(), filepath.Join(".codex", "config.toml")) {
		t.Errorf("DefaultConfigPath = %q, unexpected suffix", s.DefaultConfigPath())
	}
}

func TestCodexScanner_Scan_ActiveProvider(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(configPath, []byte(`
model_provider = "my-openai"

[model_providers.my-openai]
name     = "My OpenAI"
base_url = "https://api.openai.com/v1"
env_key  = "OPENAI_API_KEY"
wire_api = "chat"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	// Even when the referenced env var is set in the daemon's environment,
	// the scanner must not resolve it — krouter never reads env vars for
	// credentials.
	t.Setenv("OPENAI_API_KEY", "sk-openai-test-key")

	eps, err := CodexScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	ep := eps[0]
	if ep.Provider != "openai" {
		t.Errorf("Provider = %q, want openai", ep.Provider)
	}
	if ep.EndpointURL != "https://api.openai.com/v1" {
		t.Errorf("EndpointURL = %q", ep.EndpointURL)
	}
	if ep.ProtocolHint != "openai-chat" {
		t.Errorf("ProtocolHint = %q", ep.ProtocolHint)
	}
	if ep.APIKey != "" {
		t.Errorf("APIKey = %q, want empty (env_key must not be resolved)", ep.APIKey)
	}
}

func TestCodexScanner_Scan_KrouterEntrySkipped(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(configPath, []byte(`
model_provider = "krouter"

[model_providers.krouter]
name     = "krouter"
base_url = "http://127.0.0.1:8402/v1"
env_key  = "OPENAI_API_KEY"
wire_api = "chat"

[model_providers.my-openai]
name     = "My OpenAI"
base_url = "https://api.openai.com/v1"
env_key  = "OPENAI_API_KEY"
wire_api = "chat"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	eps, err := CodexScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	// Only my-openai should be returned; krouter entry is skipped.
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1 (krouter entry skipped)", len(eps))
	}
	if eps[0].EndpointURL == "http://127.0.0.1:8402/v1" {
		t.Errorf("krouter's own URL should be filtered out")
	}
}

func TestCodexScanner_Scan_NoProviders(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(configPath, []byte(`model_provider = ""`), 0o644); err != nil {
		t.Fatal(err)
	}

	eps, err := CodexScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0", len(eps))
	}
}

func TestCodexScanner_Scan_FileNotFound(t *testing.T) {
	eps, err := CodexScanner{}.Scan(context.Background(), "/no/such/config.toml")
	if err == nil {
		t.Errorf("expected error for missing file")
	}
	if eps != nil {
		t.Errorf("eps = %v, want nil", eps)
	}
}

func TestCodexScanner_Scan_MalformedTOML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[[broken toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	eps, err := CodexScanner{}.Scan(context.Background(), configPath)
	if err == nil {
		t.Errorf("expected parse error")
	}
	if eps != nil {
		t.Errorf("eps = %v, want nil", eps)
	}
}
