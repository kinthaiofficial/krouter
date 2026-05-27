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

// piBuiltinAPI maps Pi's built-in provider names to their wire protocol.
// When models.json overrides only baseUrl/apiKey for a built-in provider, the
// "api" field is omitted; Pi resolves it from this same internal registry.
// Source: github.com/earendil-works/pi packages/coding-agent provider list.
var piBuiltinAPI = map[string]string{
	"anthropic": "anthropic-messages",
	// All other built-in providers use OpenAI-compatible completions.
	"openai":     "openai-completions",
	"deepseek":   "openai-completions",
	"groq":       "openai-completions",
	"mistral":    "openai-completions",
	"xai":        "openai-completions",
	"minimax":    "openai-completions",
	"openrouter": "openai-completions",
	"azure":      "openai-completions",
	"together":   "openai-completions",
	"deepinfra":  "openai-completions",
	"fireworks":  "openai-completions",
	"cerebras":   "openai-completions",
	// google-generative-ai is not currently supported by krouter.
}

// piProtocolHint maps Pi's "api" field (or built-in default) to krouter's
// protocol hint. Returns "" for protocols krouter does not support.
func piProtocolHint(apiType, providerName string) string {
	if apiType == "" {
		// No explicit api field: entry is overriding a built-in provider.
		// Look up the built-in default; custom providers always require api.
		apiType = piBuiltinAPI[providerName]
	}
	switch apiType {
	case "anthropic-messages":
		return "anthropic-messages"
	case "openai-completions", "openai-responses":
		return "openai-chat"
	default:
		// google-generative-ai and unknown types: not supported.
		return ""
	}
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
		hint := piProtocolHint(entry.API, name)
		if hint == "" {
			continue // unsupported protocol (e.g. google-generative-ai)
		}
		out = append(out, InheritedEndpoint{
			Provider:     name,
			EndpointURL:  entry.BaseURL,
			ProtocolHint: hint,
			APIKey:       entry.APIKey,
		})
	}
	return out, nil
}
