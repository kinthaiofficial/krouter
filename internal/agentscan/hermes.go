package agentscan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// HermesScanner extracts vendor endpoint metadata from a Hermes TOML config
// file (typically ~/.hermes/config.toml).
type HermesScanner struct{}

func (HermesScanner) AppID() string     { return "hermes" }
func (HermesScanner) DisplayName() string { return "Hermes" }

func (HermesScanner) DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/.hermes/config.toml"
	}
	return filepath.Join(home, ".hermes", "config.toml")
}

type hermesConfig struct {
	Providers map[string]hermesProvider `toml:"providers"`
}

type hermesProvider struct {
	BaseURL string `toml:"base_url"`
	APIKey  string `toml:"api_key"`
}

func (s HermesScanner) Scan(_ context.Context, configPath string) ([]InheritedEndpoint, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read hermes config: %w", err)
	}

	var cfg hermesConfig
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("parse hermes config: %w", err)
	}

	out := make([]InheritedEndpoint, 0, len(cfg.Providers))
	for name, p := range cfg.Providers {
		if name == "" || p.BaseURL == "" {
			continue
		}
		out = append(out, InheritedEndpoint{
			Provider:     name,
			EndpointURL:  p.BaseURL,
			ProtocolHint: hermesProtocolHint(name),
			APIKey:       p.APIKey,
		})
	}
	return out, nil
}

func hermesProtocolHint(provider string) string {
	if provider == "anthropic" {
		return "anthropic-messages"
	}
	return "openai-chat"
}
