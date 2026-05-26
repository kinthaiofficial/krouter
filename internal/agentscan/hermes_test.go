package agentscan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHermesScanner_Meta(t *testing.T) {
	s := HermesScanner{}
	if s.AppID() != "hermes" {
		t.Errorf("AppID = %q, want hermes", s.AppID())
	}
	if s.DisplayName() == "" {
		t.Errorf("DisplayName empty")
	}
	if !strings.HasSuffix(s.DefaultConfigPath(), filepath.Join(".hermes", "config.toml")) {
		t.Errorf("DefaultConfigPath = %q, unexpected suffix", s.DefaultConfigPath())
	}
}

func TestHermesScanner_Scan_Anthropic(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(configPath, []byte(`
[providers.anthropic]
base_url = "http://127.0.0.1:8402"
api_key  = "sk-ant-test"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	eps, err := HermesScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	ep := eps[0]
	if ep.Provider != "anthropic" {
		t.Errorf("Provider = %q", ep.Provider)
	}
	if ep.EndpointURL != "http://127.0.0.1:8402" {
		t.Errorf("EndpointURL = %q", ep.EndpointURL)
	}
	if ep.ProtocolHint != "anthropic-messages" {
		t.Errorf("ProtocolHint = %q", ep.ProtocolHint)
	}
	if ep.APIKey != "sk-ant-test" {
		t.Errorf("APIKey = %q", ep.APIKey)
	}
}

func TestHermesScanner_Scan_MultipleProviders(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(configPath, []byte(`
[providers.anthropic]
base_url = "http://127.0.0.1:8402"

[providers.openai]
base_url = "http://127.0.0.1:8402/v1"
api_key  = "sk-openai-test"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	eps, err := HermesScanner{}.Scan(context.Background(), configPath)
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
	if byProvider["anthropic"].ProtocolHint != "anthropic-messages" {
		t.Errorf("anthropic ProtocolHint = %q", byProvider["anthropic"].ProtocolHint)
	}
	if byProvider["openai"].ProtocolHint != "openai-chat" {
		t.Errorf("openai ProtocolHint = %q", byProvider["openai"].ProtocolHint)
	}
}

func TestHermesScanner_Scan_EmptyBaseURLSkipped(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")

	if err := os.WriteFile(configPath, []byte(`
[providers.anthropic]
api_key = "sk-ant-only-key"
`), 0o644); err != nil {
		t.Fatal(err)
	}

	eps, err := HermesScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0 (empty base_url skipped)", len(eps))
	}
}

func TestHermesScanner_Scan_FileNotFound(t *testing.T) {
	eps, err := HermesScanner{}.Scan(context.Background(), "/no/such/config.toml")
	if err == nil {
		t.Errorf("expected error for missing file")
	}
	if eps != nil {
		t.Errorf("eps = %v, want nil", eps)
	}
}

func TestHermesScanner_Scan_MalformedTOML(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(configPath, []byte("[[not valid toml"), 0o644); err != nil {
		t.Fatal(err)
	}
	eps, err := HermesScanner{}.Scan(context.Background(), configPath)
	if err == nil {
		t.Errorf("expected parse error")
	}
	if eps != nil {
		t.Errorf("eps = %v, want nil", eps)
	}
}
