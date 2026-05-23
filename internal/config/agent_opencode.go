package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// ConnectOpenCode sets the baseUrl field in the OpenCode config JSON to route
// through krouter. Backs up the file before modification.
func ConnectOpenCode(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("opencode: read config: %w", err)
	}

	if err := backupFile(configPath, data); err != nil {
		return err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("opencode: parse config: %w", err)
	}

	root["baseUrl"] = proxyBase

	return writeJSON(configPath, root)
}

// DisconnectOpenCode removes the krouter baseUrl override from the OpenCode
// config JSON.
func DisconnectOpenCode(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("opencode: read config: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("opencode: parse config: %w", err)
	}

	delete(root, "baseUrl")

	return writeJSON(configPath, root)
}
