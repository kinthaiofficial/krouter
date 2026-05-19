package config

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"time"
)

const proxyBase = "http://127.0.0.1:8402"

// placeholderAPIKey is the broken value written by old krouter versions.
// DisconnectOpenClaw removes it so users are not left with an unusable key.
const placeholderAPIKey = "${ANTHROPIC_API_KEY}"

// minimaxPortalOriginalBaseURL is the upstream endpoint written by OpenClaw for
// the minimax-portal provider. DisconnectOpenClaw restores it after removing
// our proxyBase override so OpenClaw can reach MiniMax directly again.
const minimaxPortalOriginalBaseURL = "https://api.minimaxi.com/anthropic/v1"

// defaultOpenClawModels is injected into models.providers.anthropic.models when
// that field is absent. OpenClaw schema requires a non-nil array (undefined crashes
// the agent on startup); an empty array satisfies the schema.
// OpenClaw loads its model catalog from plugin discovery, not from this field,
// so an empty array leaves model selection fully intact.
// String elements (previous implementation) are schema-invalid — each entry must
// be a ModelDefinition object {id, name, ...}.
var defaultOpenClawModels = []any{}

// ConnectOpenClaw points the OpenClaw anthropic provider at the krouter proxy.
// Only baseUrl and api are written; apiKey and all other existing fields are
// preserved unchanged.
//
// Rationale: OpenClaw runs as a LaunchAgent and does not inherit shell env, so
// a literal "${ANTHROPIC_API_KEY}" placeholder would never be expanded — the
// user's real key must come from OpenClaw's own config, not from krouter.
// setNestedJSON previously replaced the whole anthropic node, destroying
// existing keys (e.g. a real MiniMax apiKey stored there). Merge instead.
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

	// Navigate/create models.providers.anthropic without replacing existing fields.
	models := ensureMap(root, "models")
	providers := ensureMap(models, "providers")
	anthropic := ensureMap(providers, "anthropic")

	anthropic["baseUrl"] = proxyBase
	anthropic["api"] = "anthropic-messages"
	// Never touch apiKey — the user's real key must stay as-is.
	// Ensure models is a non-nil array (OpenClaw schema requires it).
	// Only set when absent; an existing user-configured list is preserved.
	if _, hasModels := anthropic["models"]; !hasModels {
		anthropic["models"] = defaultOpenClawModels
	}

	// If the user has a minimax-portal provider configured, redirect it through
	// krouter as well. OpenClaw's OAuth flow (authHeader:true) generates the
	// Authorization header itself — we only change baseUrl, nothing else.
	if minimaxPortal := deepMap(providers, "minimax-portal"); minimaxPortal != nil {
		minimaxPortal["baseUrl"] = proxyBase
	}

	return writeJSON(configPath, root)
}

// IsOpenClawConnected reports whether the OpenClaw config at configPath has its
// Anthropic provider baseUrl pointing at the krouter proxy.
func IsOpenClawConnected(configPath string) bool {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return false
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return false
	}
	provider := deepMap(root, "models", "providers", "anthropic")
	if provider == nil {
		return false
	}
	baseURL, _ := provider["baseUrl"].(string)
	return baseURL == proxyBase
}

// ReadOpenClawProviderNames returns the names of LLM providers configured in
// the OpenClaw config at configPath (e.g. ["anthropic", "minimax"]).
// Returns nil on read/parse error.
func ReadOpenClawProviderNames(configPath string) []string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return nil
	}
	providers := deepMap(root, "models", "providers")
	if len(providers) == 0 {
		return nil
	}
	names := make([]string, 0, len(providers))
	for k := range providers {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

// ReadOpenClawAPIKey transiently reads the Anthropic API key from the OpenClaw
// config at configPath. The key is used only for model discovery and is never
// stored by krouter. Returns "" if the config cannot be read, if the key is
// absent, or if the value is the broken placeholder sentinel.
func ReadOpenClawAPIKey(configPath string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return ""
	}
	provider := deepMap(root, "models", "providers", "anthropic")
	if provider == nil {
		return ""
	}
	key, _ := provider["apiKey"].(string)
	if key == "" || key == placeholderAPIKey {
		return ""
	}
	return key
}

// UpdateOpenClawModels overwrites the models field for providerName in the OpenClaw
// config at configPath. Only the models array is updated; all other fields
// (baseUrl, api, apiKey, etc.) are preserved. No backup is written — the initial
// connect backup already covers the original state.
func UpdateOpenClawModels(configPath, providerName string, models []map[string]any) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("openclaw: read config: %w", err)
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("openclaw: parse config: %w", err)
	}

	modelsSection := ensureMap(root, "models")
	provs := ensureMap(modelsSection, "providers")
	provider := ensureMap(provs, providerName)

	out := make([]any, len(models))
	for i, m := range models {
		out[i] = m
	}
	provider["models"] = out

	return writeJSON(configPath, root)
}

// DisconnectOpenClaw removes krouter's routing fields from the OpenClaw config.
// Only removes baseUrl, api, and (if it's the broken placeholder) apiKey.
// Real user-supplied apiKeys are never touched.
func DisconnectOpenClaw(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("openclaw: read config: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("openclaw: parse config: %w", err)
	}

	if provider := deepMap(root, "models", "providers", "anthropic"); provider != nil {
		delete(provider, "baseUrl")
		delete(provider, "api")
		// Remove only the broken placeholder written by old krouter versions;
		// leave real user-supplied apiKeys intact.
		if provider["apiKey"] == placeholderAPIKey {
			delete(provider, "apiKey")
		}

		// If no real apiKey remains, the anthropic section was created entirely by
		// krouter (the user never had their own anthropic provider configured).
		// Remove krouter-injected fields and clean up the now-empty section so the
		// config is back to its original state.
		if _, hasRealKey := provider["apiKey"]; !hasRealKey {
			delete(provider, "models")
			if len(provider) == 0 {
				if provs := deepMap(root, "models", "providers"); provs != nil {
					delete(provs, "anthropic")
				}
			}
		}
	}

	// Restore minimax-portal baseUrl to the upstream MiniMax endpoint so OpenClaw
	// can reach MiniMax directly again. Only touch baseUrl — never the OAuth fields.
	if minimaxPortal := deepMap(root, "models", "providers", "minimax-portal"); minimaxPortal != nil {
		if minimaxPortal["baseUrl"] == proxyBase {
			minimaxPortal["baseUrl"] = minimaxPortalOriginalBaseURL
		}
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

// ensureMap returns the map at root[key], creating it if absent or wrong type.
func ensureMap(root map[string]any, key string) map[string]any {
	if m, ok := root[key].(map[string]any); ok {
		return m
	}
	m := make(map[string]any)
	root[key] = m
	return m
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
