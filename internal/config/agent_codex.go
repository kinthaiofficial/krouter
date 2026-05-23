package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// ConnectCodex adds a krouter-managed provider entry to the Codex CLI config
// and sets model_provider to that entry, routing all Codex traffic through
// krouter. Backs up the file before modification.
//
// The krouter entry preserves env_key from the previously active provider so
// that the user's API key continues to reach krouter.
func ConnectCodex(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("codex: read config: %w", err)
	}

	if err := backupFile(configPath, data); err != nil {
		return err
	}

	var root map[string]any
	if _, err := toml.Decode(string(data), &root); err != nil {
		return fmt.Errorf("codex: parse TOML: %w", err)
	}

	// Inherit the env_key from the currently active provider so the user's
	// API key reaches krouter in the request header.
	envKey := "OPENAI_API_KEY"
	if active, _ := root["model_provider"].(string); active != "" {
		if providers, _ := root["model_providers"].(map[string]any); providers != nil {
			if ep, _ := providers[active].(map[string]any); ep != nil {
				if k, _ := ep["env_key"].(string); k != "" {
					envKey = k
				}
			}
		}
	}

	providers := ensureMap(root, "model_providers")
	providers["krouter"] = map[string]any{
		"name":     "krouter",
		"base_url": proxyBase + "/v1",
		"env_key":  envKey,
		"wire_api": "chat",
	}
	root["model_provider"] = "krouter"

	return writeTOML(configPath, root)
}

// DisconnectCodex removes the krouter provider entry from the Codex CLI config
// and restores model_provider to the previously active provider, if known.
func DisconnectCodex(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("codex: read config: %w", err)
	}

	var root map[string]any
	if _, err := toml.Decode(string(data), &root); err != nil {
		return fmt.Errorf("codex: parse TOML: %w", err)
	}

	providers, _ := root["model_providers"].(map[string]any)
	if providers == nil {
		return nil
	}

	// Find a non-krouter provider to restore to.
	var fallback string
	for id := range providers {
		if id != "krouter" {
			fallback = id
			break
		}
	}

	delete(providers, "krouter")

	if cur, _ := root["model_provider"].(string); cur == "krouter" {
		if fallback != "" {
			root["model_provider"] = fallback
		} else {
			delete(root, "model_provider")
		}
	}

	return writeTOML(configPath, root)
}
