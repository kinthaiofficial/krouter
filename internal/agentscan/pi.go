package agentscan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// PiScanner extracts vendor endpoint metadata from Pi's models.json
// (typically ~/.pi/agent/models.json).
//
// Pi stores per-provider configuration under a top-level "providers" map.
// Each entry may carry a baseUrl, api type, and apiKey. The scanner emits
// one InheritedEndpoint per provider that has at least a baseUrl or apiKey.
type PiScanner struct{}

func (PiScanner) AppID() string      { return "pi" }
func (PiScanner) DisplayName() string { return "Pi" }

func (PiScanner) DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/.pi/agent/models.json"
	}
	return filepath.Join(home, ".pi", "agent", "models.json")
}

type piModelsFile struct {
	Providers map[string]piProviderEntry `json:"providers"`
}

type piProviderEntry struct {
	BaseURL string `json:"baseUrl"`
	API     string `json:"api"`
	APIKey  string `json:"apiKey"`
}

// piProtocolHint maps Pi's "api" field to krouter's protocol hint.
// Pi uses "anthropic-messages" directly; all OpenAI variants map to "openai-chat".
func piProtocolHint(apiType, providerName string) string {
	switch apiType {
	case "anthropic-messages":
		return "anthropic-messages"
	case "openai-completions", "openai-responses":
		return "openai-chat"
	}
	// No explicit api type: infer from well-known provider names.
	if providerName == "anthropic" {
		return "anthropic-messages"
	}
	return "openai-chat"
}

func (s PiScanner) Scan(_ context.Context, configPath string) ([]InheritedEndpoint, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read pi models.json: %w", err)
	}

	var cfg piModelsFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse pi models.json: %w", err)
	}

	var out []InheritedEndpoint
	for name, entry := range cfg.Providers {
		if entry.BaseURL == "" && entry.APIKey == "" {
			continue
		}
		out = append(out, InheritedEndpoint{
			Provider:     name,
			EndpointURL:  entry.BaseURL,
			ProtocolHint: piProtocolHint(entry.API, name),
			APIKey:       entry.APIKey,
		})
	}
	return out, nil
}
