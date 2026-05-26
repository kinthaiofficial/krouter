package agentscan

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenClawScanner_AgentMeta(t *testing.T) {
	s := OpenClawScanner{}
	if s.AppID() != "openclaw" {
		t.Errorf("AppID = %q, want openclaw", s.AppID())
	}
	if s.DisplayName() == "" {
		t.Errorf("DisplayName empty")
	}
	if !strings.HasSuffix(s.DefaultConfigPath(), filepath.Join(".openclaw", "openclaw.json")) {
		t.Errorf("DefaultConfigPath = %q, unexpected suffix", s.DefaultConfigPath())
	}
}

func TestOpenClawScanner_Scan_BasicProviders(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "openclaw.json")

	cfg := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"anthropic": map[string]any{
					"baseUrl": "http://127.0.0.1:8402",
					"api":     "anthropic-messages",
				},
				"minimax-portal": map[string]any{
					"baseUrl":    "http://127.0.0.1:8402",
					"api":        "anthropic-messages",
					"apiKey":     "sk-api-foo",
					"authHeader": false,
				},
				"unused-empty": map[string]any{
					// no baseUrl → should be skipped
					"api": "openai-chat",
				},
			},
		},
	}
	writeJSON(t, configPath, cfg)

	endpoints, err := OpenClawScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(endpoints) != 2 {
		t.Fatalf("got %d endpoints, want 2 (the empty-baseUrl one should be skipped); got %#v", len(endpoints), endpoints)
	}

	byProvider := map[string]InheritedEndpoint{}
	for _, ep := range endpoints {
		byProvider[ep.Provider] = ep
	}

	if got := byProvider["anthropic"]; got.EndpointURL != "http://127.0.0.1:8402" {
		t.Errorf("anthropic.EndpointURL = %q", got.EndpointURL)
	}
	if got := byProvider["minimax-portal"]; got.APIKey != "sk-api-foo" {
		t.Errorf("minimax-portal.APIKey = %q", got.APIKey)
	}
	if _, ok := byProvider["unused-empty"]; ok {
		t.Errorf("unused-empty provider should have been skipped")
	}
}

func TestOpenClawScanner_Scan_OAuthInheritance(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "openclaw.json")

	cfg := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"minimax-portal": map[string]any{
					"baseUrl":    "http://127.0.0.1:8402",
					"api":        "anthropic-messages",
					"authHeader": true,
				},
			},
		},
	}
	writeJSON(t, configPath, cfg)

	// Drop a sibling auth-profiles.json with an OAuth token.
	profileDir := filepath.Join(dir, "agents", "main", "agent")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatal(err)
	}
	profiles := map[string]any{
		"profiles": map[string]any{
			"minimax-portal:default": map[string]any{
				"type":     "oauth",
				"provider": "minimax-portal",
				"access":   "sk-cp-FAKE-OAUTH-TOKEN",
			},
		},
	}
	writeJSON(t, filepath.Join(profileDir, "auth-profiles.json"), profiles)

	endpoints, err := OpenClawScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("got %d endpoints, want 1: %#v", len(endpoints), endpoints)
	}
	ep := endpoints[0]
	if ep.Provider != "minimax-portal" {
		t.Fatalf("Provider = %q", ep.Provider)
	}
	if ep.ExtrasJSON == "" {
		t.Fatalf("ExtrasJSON empty; expected OAuth token to be attached")
	}

	var extras map[string]string
	if err := json.Unmarshal([]byte(ep.ExtrasJSON), &extras); err != nil {
		t.Fatalf("ExtrasJSON is not valid JSON: %v (%q)", err, ep.ExtrasJSON)
	}
	if extras["oauth_token"] != "sk-cp-FAKE-OAUTH-TOKEN" {
		t.Errorf("oauth_token = %q, want sk-cp-FAKE-OAUTH-TOKEN", extras["oauth_token"])
	}
	if extras["source"] != "openclaw_auth_profile" {
		t.Errorf("source = %q, want openclaw_auth_profile", extras["source"])
	}
}

func TestOpenClawScanner_Scan_NoOAuthDataLeak(t *testing.T) {
	// Provider has only API key, no auth-profiles.json sibling exists.
	// ExtrasJSON must stay empty (no spurious OAuth fields written).
	dir := t.TempDir()
	configPath := filepath.Join(dir, "openclaw.json")

	cfg := map[string]any{
		"models": map[string]any{
			"providers": map[string]any{
				"anthropic": map[string]any{
					"baseUrl": "https://api.anthropic.com",
					"apiKey":  "sk-ant-xxx",
				},
			},
		},
	}
	writeJSON(t, configPath, cfg)

	endpoints, err := OpenClawScanner{}.Scan(context.Background(), configPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(endpoints) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(endpoints))
	}
	if endpoints[0].ExtrasJSON != "" {
		t.Errorf("ExtrasJSON = %q, want empty", endpoints[0].ExtrasJSON)
	}
}

func TestOpenClawScanner_Scan_FileNotFound(t *testing.T) {
	endpoints, err := OpenClawScanner{}.Scan(context.Background(), "/definitely/does/not/exist")
	if err == nil {
		t.Errorf("expected error for missing file, got nil; endpoints=%v", endpoints)
	}
	if endpoints != nil {
		t.Errorf("endpoints = %v, want nil on error", endpoints)
	}
}

func TestOpenClawScanner_Scan_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "openclaw.json")
	if err := os.WriteFile(configPath, []byte("not json at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	endpoints, err := OpenClawScanner{}.Scan(context.Background(), configPath)
	if err == nil {
		t.Errorf("expected parse error; got endpoints=%v", endpoints)
	}
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}
