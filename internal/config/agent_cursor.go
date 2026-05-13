package config

import (
	"encoding/json"
	"fmt"
	"os"
)

// ConnectCursor adds kinthai routing fields to the Cursor settings JSON.
// Backs up the file before modification.
// Sets cursor.anthropic.baseUrl and cursor.openai.baseUrl at the top level.
func ConnectCursor(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("cursor: read config: %w", err)
	}

	if err := backupFile(configPath, data); err != nil {
		return err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("cursor: parse config: %w", err)
	}

	root["cursor.anthropic.baseUrl"] = proxyBase
	root["cursor.openai.baseUrl"] = proxyBase + "/v1"

	return writeJSON(configPath, root)
}

// DisconnectCursor removes kinthai routing fields from the Cursor settings JSON.
func DisconnectCursor(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("cursor: read config: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("cursor: parse config: %w", err)
	}

	delete(root, "cursor.anthropic.baseUrl")
	delete(root, "cursor.openai.baseUrl")

	return writeJSON(configPath, root)
}
