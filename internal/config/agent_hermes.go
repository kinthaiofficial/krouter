package config

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

// ConnectHermes modifies the Hermes TOML config to route through kinthai.
// Sets providers.anthropic.base_url. Backs up before modification.
func ConnectHermes(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("hermes: read config: %w", err)
	}

	if err := backupFile(configPath, data); err != nil {
		return err
	}

	var root map[string]any
	if _, err := toml.Decode(string(data), &root); err != nil {
		return fmt.Errorf("hermes: parse TOML: %w", err)
	}

	setNestedJSON(root, []string{"providers", "anthropic", "base_url"}, proxyBase)

	return writeTOML(configPath, root)
}

// DisconnectHermes removes the kinthai base_url from the Hermes TOML config.
func DisconnectHermes(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("hermes: read config: %w", err)
	}

	var root map[string]any
	if _, err := toml.Decode(string(data), &root); err != nil {
		return fmt.Errorf("hermes: parse TOML: %w", err)
	}

	if provider := deepMap(root, "providers", "anthropic"); provider != nil {
		delete(provider, "base_url")
	}

	return writeTOML(configPath, root)
}

// writeTOML encodes v as TOML and writes it atomically to path.
func writeTOML(path string, v any) error {
	tmp, err := os.CreateTemp("", "kinthai-*.toml.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()

	if err := toml.NewEncoder(tmp).Encode(v); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return fmt.Errorf("hermes: encode TOML: %w", err)
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Chmod(tmpName, 0600); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	return os.Rename(tmpName, path)
}
