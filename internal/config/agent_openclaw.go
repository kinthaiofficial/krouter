package config

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
)

const proxyBase = "http://127.0.0.1:8402"

// ConnectOpenClaw modifies the OpenClaw JSON config to route through kinthai.
// Creates a timestamped backup before modification.
// Target: models.providers.anthropic.{baseUrl, apiKey, api}
func ConnectOpenClaw(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("openclaw: read config: %w", err)
	}

	if err := backupFile(configPath, data); err != nil {
		return err
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("openclaw: parse config: %w", err)
	}

	setNestedJSON(root, []string{"models", "providers", "anthropic"}, map[string]any{
		"baseUrl": proxyBase,
		"apiKey":  "${ANTHROPIC_API_KEY}",
		"api":     "anthropic-messages",
	})

	return writeJSON(configPath, root)
}

// DisconnectOpenClaw removes kinthai routing fields from the OpenClaw config.
func DisconnectOpenClaw(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("openclaw: read config: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("openclaw: parse config: %w", err)
	}

	// Remove the kinthai-set fields from models.providers.anthropic.
	if provider := deepMap(root, "models", "providers", "anthropic"); provider != nil {
		delete(provider, "baseUrl")
		delete(provider, "apiKey")
		delete(provider, "api")
	}

	return writeJSON(configPath, root)
}

// backupFile writes data to {path}.kinthai-bak-{timestamp}.
func backupFile(path string, data []byte) error {
	ts := time.Now().UTC().Format("2006-01-02-15-04-05")
	backup := path + ".kinthai-bak-" + ts
	if err := os.WriteFile(backup, data, 0600); err != nil {
		return fmt.Errorf("config: backup %s: %w", path, err)
	}
	return nil
}

// setNestedJSON navigates the map tree via keys and sets the leaf to value,
// creating intermediate maps as needed.
func setNestedJSON(root map[string]any, keys []string, value any) {
	if len(keys) == 1 {
		root[keys[0]] = value
		return
	}
	next, ok := root[keys[0]].(map[string]any)
	if !ok {
		next = make(map[string]any)
		root[keys[0]] = next
	}
	setNestedJSON(next, keys[1:], value)
}

// deepMap navigates the map tree and returns the leaf map, or nil if not found.
func deepMap(root map[string]any, keys ...string) map[string]any {
	cur := root
	for _, k := range keys {
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

// writeJSON marshals v and writes it atomically to path (0600 perms).
func writeJSON(path string, v any) error {
	out, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("config: marshal JSON: %w", err)
	}
	tmp, err := os.CreateTemp("", "kinthai-*.json.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if _, err := tmp.Write(out); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
		return err
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
