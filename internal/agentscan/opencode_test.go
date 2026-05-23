package agentscan

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenCodeScanner_Meta(t *testing.T) {
	s := OpenCodeScanner{}
	if s.AgentID() != "opencode" {
		t.Errorf("AgentID = %q, want opencode", s.AgentID())
	}
	if s.DisplayName() == "" {
		t.Errorf("DisplayName empty")
	}
	if !strings.HasSuffix(s.DefaultConfigPath(), "opencode.json") {
		t.Errorf("DefaultConfigPath = %q, want suffix opencode.json", s.DefaultConfigPath())
	}
}

func TestOpenCodeScanner_Scan_Anthropic(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "opencode.json")

	writeJSON(t, configPath, map[string]any{
		"provider": "anthropic",
		"baseUrl":  "http://127.0.0.1:8402",
		"apiKey":   "sk-ant-test",
	})

	eps, err := OpenCodeScanner{}.Scan(context.Background(), configPath)
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
	if ep.ProtocolHint != "anthropic-messages" {
		t.Errorf("ProtocolHint = %q", ep.ProtocolHint)
	}
	if ep.EndpointURL != "http://127.0.0.1:8402" {
		t.Errorf("EndpointURL = %q", ep.EndpointURL)
	}
	if ep.APIKey != "sk-ant-test" {
		t.Errorf("APIKey = %q", ep.APIKey)
	}
}

func TestOpenCodeScanner_Scan_OpenAI(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "opencode.json")

	writeJSON(t, configPath, map[string]any{
		"provider": "openai",
		"baseUrl":  "https://api.openai.com/v1",
	})

	eps, err := OpenCodeScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].ProtocolHint != "openai-chat" {
		t.Errorf("ProtocolHint = %q", eps[0].ProtocolHint)
	}
}

func TestOpenCodeScanner_Scan_NoBaseURL(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "opencode.json")

	writeJSON(t, configPath, map[string]any{
		"provider": "anthropic",
	})

	eps, err := OpenCodeScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0 (no baseUrl)", len(eps))
	}
}

func TestOpenCodeScanner_Scan_DefaultProviderIsOpenAI(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "opencode.json")

	writeJSON(t, configPath, map[string]any{
		"baseUrl": "https://api.openai.com/v1",
	})

	eps, err := OpenCodeScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].Provider != "openai" {
		t.Errorf("Provider = %q, want openai (default)", eps[0].Provider)
	}
}

func TestOpenCodeScanner_Scan_FileNotFound(t *testing.T) {
	eps, err := OpenCodeScanner{}.Scan(context.Background(), "/no/such/opencode.json")
	if err == nil {
		t.Errorf("expected error for missing file")
	}
	if eps != nil {
		t.Errorf("eps = %v, want nil", eps)
	}
}

func TestOpenCodeScanner_Scan_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "opencode.json")
	if err := os.WriteFile(configPath, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	eps, err := OpenCodeScanner{}.Scan(context.Background(), configPath)
	if err == nil {
		t.Errorf("expected parse error")
	}
	if eps != nil {
		t.Errorf("eps = %v, want nil", eps)
	}
}
