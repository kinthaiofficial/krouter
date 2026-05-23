package agentscan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

// OpenCodeScanner extracts vendor endpoint metadata from an OpenCode config
// file (typically ~/.config/opencode/opencode.json on Linux/macOS).
type OpenCodeScanner struct{}

func (OpenCodeScanner) AgentID() string     { return "opencode" }
func (OpenCodeScanner) DisplayName() string { return "OpenCode" }

func (OpenCodeScanner) DefaultConfigPath() string {
	if runtime.GOOS == "windows" {
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			return filepath.Join(appdata, "opencode", "opencode.json")
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/.config/opencode/opencode.json"
	}
	return filepath.Join(home, ".config", "opencode", "opencode.json")
}

type opencodeConfig struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"baseUrl"`
	APIKey   string `json:"apiKey"`
}

func (s OpenCodeScanner) Scan(_ context.Context, configPath string) ([]InheritedEndpoint, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read opencode config: %w", err)
	}

	var cfg opencodeConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse opencode config: %w", err)
	}

	if cfg.BaseURL == "" {
		return nil, nil
	}

	hint := "openai-chat"
	provider := cfg.Provider
	if provider == "" {
		provider = "openai"
	}
	if provider == "anthropic" {
		hint = "anthropic-messages"
	}

	return []InheritedEndpoint{{
		Provider:     provider,
		EndpointURL:  cfg.BaseURL,
		ProtocolHint: hint,
		APIKey:       cfg.APIKey,
	}}, nil
}
