package agentscan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// CursorScanner extracts vendor endpoint metadata from a Cursor settings JSON
// file (typically ~/.cursor/settings.json).
//
// Cursor stores API keys in the OS keychain, not in settings.json, so APIKey
// is never populated; krouter relies on the key being forwarded in the request
// header instead.
type CursorScanner struct{}

func (CursorScanner) AgentID() string     { return "cursor" }
func (CursorScanner) DisplayName() string { return "Cursor" }

func (CursorScanner) DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/.cursor/settings.json"
	}
	return filepath.Join(home, ".cursor", "settings.json")
}

func (s CursorScanner) Scan(_ context.Context, configPath string) ([]InheritedEndpoint, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read cursor settings: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse cursor settings: %w", err)
	}

	var out []InheritedEndpoint

	if url := stringField(root, "cursor.anthropic.baseUrl"); url != "" {
		out = append(out, InheritedEndpoint{
			Provider:     "anthropic",
			EndpointURL:  url,
			ProtocolHint: "anthropic-messages",
		})
	}

	if url := stringField(root, "cursor.openai.baseUrl"); url != "" {
		out = append(out, InheritedEndpoint{
			Provider:     "openai",
			EndpointURL:  url,
			ProtocolHint: "openai-chat",
		})
	}

	return out, nil
}

// stringField returns root[key] as a string, or "" if absent or wrong type.
func stringField(root map[string]any, key string) string {
	v, ok := root[key]
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}
