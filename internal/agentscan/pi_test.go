package agentscan

import (
	"context"
	"os"
	"strings"
	"testing"
)

func TestPiScanner_Meta(t *testing.T) {
	s := PiScanner{}
	if s.AppID() != "pi" {
		t.Errorf("AppID = %q, want pi", s.AppID())
	}
	if s.DisplayName() == "" {
		t.Errorf("DisplayName empty")
	}
	if !strings.HasSuffix(s.DefaultConfigPath(), "models.json") {
		t.Errorf("DefaultConfigPath = %q, want suffix models.json", s.DefaultConfigPath())
	}
}

func TestPiScanner_Scan_AnthropicProvider(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/models.json"

	writeJSON(t, path, map[string]any{
		"providers": map[string]any{
			"anthropic": map[string]any{
				"baseUrl": "http://127.0.0.1:8402",
				"api":     "anthropic-messages",
				"apiKey":  "sk-ant-test",
			},
		},
	})

	eps, err := PiScanner{}.Scan(context.Background(), path)
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

func TestPiScanner_Scan_OpenAICompletions(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/models.json"

	writeJSON(t, path, map[string]any{
		"providers": map[string]any{
			"openai": map[string]any{
				"baseUrl": "https://api.openai.com/v1",
				"api":     "openai-completions",
				"apiKey":  "sk-openai-test",
			},
		},
	})

	eps, err := PiScanner{}.Scan(context.Background(), path)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 1 {
		t.Fatalf("got %d endpoints, want 1", len(eps))
	}
	if eps[0].ProtocolHint != "openai-chat" {
		t.Errorf("ProtocolHint = %q, want openai-chat", eps[0].ProtocolHint)
	}
}

func TestPiScanner_Scan_MultipleProviders(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/models.json"

	writeJSON(t, path, map[string]any{
		"providers": map[string]any{
			"anthropic": map[string]any{
				"apiKey": "sk-ant-abc",
				"api":    "anthropic-messages",
			},
			"openai": map[string]any{
				"baseUrl": "https://api.openai.com/v1",
				"api":     "openai-completions",
			},
			"ollama": map[string]any{
				"baseUrl": "http://localhost:11434/v1",
				"api":     "openai-completions",
			},
		},
	})

	eps, err := PiScanner{}.Scan(context.Background(), path)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 3 {
		t.Errorf("got %d endpoints, want 3", len(eps))
	}
}

func TestPiScanner_Scan_SkipsEmptyEntries(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/models.json"

	// Entry with no baseUrl and no apiKey should be skipped.
	writeJSON(t, path, map[string]any{
		"providers": map[string]any{
			"anthropic": map[string]any{
				"api": "anthropic-messages",
			},
		},
	})

	eps, err := PiScanner{}.Scan(context.Background(), path)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(eps) != 0 {
		t.Errorf("got %d endpoints, want 0 (no baseUrl or apiKey)", len(eps))
	}
}

func TestPiScanner_Scan_InferProtocolFromName(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/models.json"

	// No explicit "api" field — infer from provider name.
	writeJSON(t, path, map[string]any{
		"providers": map[string]any{
			"anthropic": map[string]any{
				"apiKey": "sk-ant-xyz",
			},
			"groq": map[string]any{
				"apiKey": "gsk-test",
			},
		},
	})

	eps, err := PiScanner{}.Scan(context.Background(), path)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	byProvider := make(map[string]InheritedEndpoint)
	for _, ep := range eps {
		byProvider[ep.Provider] = ep
	}
	if byProvider["anthropic"].ProtocolHint != "anthropic-messages" {
		t.Errorf("anthropic hint = %q, want anthropic-messages", byProvider["anthropic"].ProtocolHint)
	}
	if byProvider["groq"].ProtocolHint != "openai-chat" {
		t.Errorf("groq hint = %q, want openai-chat", byProvider["groq"].ProtocolHint)
	}
}

func TestPiScanner_Scan_FileNotFound(t *testing.T) {
	eps, err := PiScanner{}.Scan(context.Background(), "/no/such/pi/models.json")
	if err == nil {
		t.Errorf("expected error for missing file")
	}
	if eps != nil {
		t.Errorf("eps = %v, want nil", eps)
	}
}

func TestPiScanner_Scan_MalformedJSON(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/models.json"
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	eps, err := PiScanner{}.Scan(context.Background(), path)
	if err == nil {
		t.Errorf("expected parse error")
	}
	if eps != nil {
		t.Errorf("eps = %v, want nil", eps)
	}
}
