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

	cur, _ := root["baseUrl"].(string)
	if cur != "" && !isKrouterBase(cur) {
		if _, has := root[krouterOrigBaseURLKey]; !has {
			root[krouterOrigBaseURLKey] = cur
		}
	}
	base := cur
	if base == "" {
		// No configured base: synthesize the protocol's canonical default so the
		// preserved path (and thus the /v1 or bare base) matches OpenCode's
		// wire protocol, which its `provider` field declares.
		if prov, _ := root["provider"].(string); prov == "anthropic" {
			base = "https://api.anthropic.com"
		} else {
			base = "https://api.openai.com/v1"
		}
	}
	root["baseUrl"] = krouterAppBaseURL("opencode", base)

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

	if orig, ok := root[krouterOrigBaseURLKey].(string); ok && orig != "" {
		root["baseUrl"] = orig
		delete(root, krouterOrigBaseURLKey)
	} else {
		delete(root, "baseUrl")
	}

	return writeJSON(configPath, root)
}
