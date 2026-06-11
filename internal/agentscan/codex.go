package agentscan

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// CodexScanner extracts vendor endpoint metadata from an OpenAI Codex CLI
// config file (typically ~/.codex/config.toml).
//
// Codex stores API keys by referencing environment variable names via the
// env_key field rather than embedding them directly. The scanner deliberately
// does NOT resolve env_key: krouter never reads environment variables for
// credentials (privacy principle — and the daemon's service environment lacks
// the user's shell exports anyway, so the lookup could only ever misfire).
// The APIKey field stays empty and the key on the live request is forwarded
// as-is.
type CodexScanner struct{}

func (CodexScanner) AppID() string       { return "codex" }
func (CodexScanner) DisplayName() string { return "Codex" }

func (CodexScanner) DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/.codex/config.toml"
	}
	return filepath.Join(home, ".codex", "config.toml")
}

type codexConfig struct {
	ModelProvider  string                   `toml:"model_provider"`
	ModelProviders map[string]codexProvider `toml:"model_providers"`
}

type codexProvider struct {
	Name    string `toml:"name"`
	BaseURL string `toml:"base_url"`
	EnvKey  string `toml:"env_key"`
	WireAPI string `toml:"wire_api"`
}

func (s CodexScanner) Scan(_ context.Context, configPath string) ([]InheritedEndpoint, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read codex config: %w", err)
	}

	var cfg codexConfig
	if _, err := toml.Decode(string(data), &cfg); err != nil {
		return nil, fmt.Errorf("parse codex config: %w", err)
	}

	if len(cfg.ModelProviders) == 0 {
		return nil, nil
	}

	out := make([]InheritedEndpoint, 0, len(cfg.ModelProviders))
	for id, p := range cfg.ModelProviders {
		if p.BaseURL == "" {
			continue
		}
		// Skip the krouter-managed provider entry (added by ConnectCodex).
		if id == "krouter" {
			continue
		}
		// env_key intentionally unresolved — see the type comment.
		out = append(out, InheritedEndpoint{
			Provider:     "openai", // all Codex providers speak OpenAI-compatible protocol
			EndpointURL:  p.BaseURL,
			ProtocolHint: "openai-chat",
		})
	}

	// If model_provider points at one of the entries we didn't skip, prefer it
	// first in the slice so callers see the active provider at index 0.
	if cfg.ModelProvider != "" && cfg.ModelProvider != "krouter" {
		for i, ep := range out {
			if cfg.ModelProviders[cfg.ModelProvider].BaseURL == ep.EndpointURL {
				out[0], out[i] = out[i], out[0]
				break
			}
		}
	}

	return out, nil
}
