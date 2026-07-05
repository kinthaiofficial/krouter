package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const proxyBase = "http://127.0.0.1:8402"

// krouterHost is the proxy authority (host:port); used to tell whether a base
// URL already points at krouter.
const krouterHost = "127.0.0.1:8402"

// krouterOrigBaseURLKey is the LEGACY in-file sidecar written next to a
// provider's baseUrl by krouter ≤v2.5.0 so DisconnectOpenClaw could restore the
// original endpoint. The assumption "OpenClaw ignores unknown config fields"
// broke in OpenClaw 2026.6.9: models.providers.* is strictly validated and an
// unknown field fails the whole config ("Invalid input"), crash-looping the
// gateway. Originals now live in openclaw-restore.json under ~/.kinthai (see
// openclaw_restore.go); this key is only ever read — and stripped — for
// migration and back-compat restore.
const krouterOrigBaseURLKey = "_krouterOriginalBaseUrl"

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

// ConnectOpenClaw points every OpenClaw LLM provider at the krouter proxy so
// krouter can route (and save tokens on) all of the user's traffic, not just
// Anthropic. For each provider in models.providers the baseUrl is rewritten to
// the krouter proxy appropriate for its wire protocol (anthropic-family → bare
// base, openai-family → /v1); apiKey and all other fields are preserved. The
// original baseUrl is saved in krouter's own restore file (~/.kinthai) so
// disconnect can restore it — never inside OpenClaw's config, which rejects
// unknown provider fields since 2026.6.9. Per-agent models.json files are
// rewritten the same way.
//
// Rationale: OpenClaw runs as a LaunchAgent and does not inherit shell env, so
// a literal "${ANTHROPIC_API_KEY}" placeholder would never be expanded — the
// user's real key must come from OpenClaw's own config, not from krouter. We
// therefore never touch apiKey; the key flows through in the request header.
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

	originals := applyOpenClawConnectToRoot(root)

	// Persist the originals BEFORE rewriting the config: if this fails the
	// config is left untouched and nothing is lost.
	if err := recordOpenClawOriginals(configPath, originals); err != nil {
		return fmt.Errorf("openclaw: save restore info: %w", err)
	}

	if err := writeJSON(configPath, root); err != nil {
		return err
	}

	// Best-effort: redirect each sub-agent's own models.json. A sub-agent may
	// define providers the global config does not, and OpenClaw's per-agent
	// config can override the global one. Errors are non-fatal — the global
	// rewrite above is what matters most.
	redirectOpenClawSubAgents(filepath.Dir(configPath))

	return nil
}

// applyOpenClawConnectToRoot mutates a parsed openclaw.json root in place:
// ensures models.providers.anthropic exists and points at krouter (OpenClaw's
// default use is Claude), then redirects every other provider that has a
// baseUrl to the krouter proxy for its protocol. Returns the provider →
// original-baseUrl map the caller must persist to the restore file.
func applyOpenClawConnectToRoot(root map[string]any) map[string]string {
	originals := map[string]string{}
	models := ensureMap(root, "models")
	providers := ensureMap(models, "providers")

	// Anthropic is always present and routed: krouter is fundamentally an
	// anthropic-protocol proxy and OpenClaw without an explicit provider still
	// talks Claude through it. An absent baseUrl redirects the same as the
	// canonical https://api.anthropic.com (empty path either way), so no
	// original is recorded when krouter introduces the route.
	anthropic := ensureMap(providers, "anthropic")
	if orig := redirectProviderBaseURL(anthropic); orig != "" {
		originals["anthropic"] = orig
	}
	anthropic["api"] = "anthropic-messages"
	// Ensure models is a non-nil array (OpenClaw schema requires it); preserve
	// an existing user-configured list.
	if _, hasModels := anthropic["models"]; !hasModels {
		anthropic["models"] = defaultOpenClawModels
	}

	for name, raw := range providers {
		if name == "anthropic" {
			continue
		}
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		// Only redirect providers that already have a reachable endpoint; we
		// never invent a baseUrl for a provider the user didn't configure.
		if cur, _ := p["baseUrl"].(string); cur == "" {
			continue
		}
		if orig := redirectProviderBaseURL(p); orig != "" {
			originals[name] = orig
		}
	}
	return originals
}

// redirectProviderBaseURL points one provider object at the krouter proxy and
// returns the original baseUrl to record in the restore file ("" when there is
// nothing new to record, e.g. already redirected). A legacy in-file sidecar is
// migrated: its value wins and the key is stripped, since OpenClaw ≥2026.6.9
// rejects unknown provider fields. Only baseUrl (and the stripped sidecar) is
// written; apiKey / authHeader / models / api are left untouched.
func redirectProviderBaseURL(p map[string]any) string {
	cur, _ := p["baseUrl"].(string)
	orig := ""
	if legacy, ok := p[krouterOrigBaseURLKey].(string); ok && legacy != "" {
		orig = legacy
	} else if cur != "" && !isKrouterBase(cur) {
		orig = cur
	}
	delete(p, krouterOrigBaseURLKey)
	p["baseUrl"] = krouterAppBaseURL("openclaw", cur)
	return orig
}

// restoreProviderBaseURL restores baseUrl from the LEGACY in-file sidecar and
// removes the sidecar (configs connected by krouter ≤v2.5.0). Returns true if
// a restore happened.
func restoreProviderBaseURL(p map[string]any) bool {
	if orig, ok := p[krouterOrigBaseURLKey].(string); ok && orig != "" {
		p["baseUrl"] = orig
		delete(p, krouterOrigBaseURLKey)
		return true
	}
	return false
}

// restoreProviderFromStore restores p's baseUrl from the original recorded in
// the restore file, but only while the current value still points at krouter —
// an endpoint the user has meanwhile pointed elsewhere by hand is not
// clobbered. Returns true if a restore happened.
func restoreProviderFromStore(p map[string]any, orig string) bool {
	if orig == "" {
		return false
	}
	if cur, _ := p["baseUrl"].(string); cur != "" && !isKrouterBase(cur) {
		return false
	}
	p["baseUrl"] = orig
	return true
}

// krouterAppBaseURL rewrites a provider base URL to route through the krouter
// proxy, tagging it with the application id: the origin (scheme://host[:port])
// is replaced with http://127.0.0.1:8402/a/<appid> and the original path is
// preserved verbatim (so per-provider conventions like /v4 or /anthropic/v1
// survive, and no protocol guessing / no /v1 insertion is needed). `localhost`
// is normalised to `127.0.0.1` because the origin is replaced wholesale.
// Idempotent: a base already pointing at krouter with this app's prefix is
// returned unchanged. See spec/12 §6.3.
func krouterAppBaseURL(appid, base string) string {
	prefix := "/a/" + appid
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return proxyBase + prefix
	}
	if u.Host == krouterHost && strings.HasPrefix(u.Path, prefix) {
		return base
	}
	return proxyBase + prefix + strings.TrimRight(u.Path, "/")
}

// isKrouterBase reports whether a base URL already points at the krouter proxy
// (either 127.0.0.1 or the localhost alias older configs may carry).
func isKrouterBase(s string) bool {
	return strings.HasPrefix(s, "http://127.0.0.1:8402") || strings.HasPrefix(s, "http://localhost:8402")
}

// redirectOpenClawSubAgents rewrites the providers in every
// agents/<id>/agent/models.json under openclawDir to point at krouter, the same
// way the global config is rewritten. Missing files / parse errors are skipped.
func redirectOpenClawSubAgents(openclawDir string) {
	forEachSubAgentModelsFile(openclawDir, func(path string, root map[string]any) bool {
		providers, ok := root["providers"].(map[string]any)
		if !ok {
			return false
		}
		originals := map[string]string{}
		changed := false
		for name, raw := range providers {
			p, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if cur, _ := p["baseUrl"].(string); cur == "" {
				continue
			}
			if orig := redirectProviderBaseURL(p); orig != "" {
				originals[name] = orig
			}
			changed = true
		}
		if !changed {
			return false
		}
		// Same ordering as the global config: don't rewrite this file if its
		// restore info can't be saved.
		if err := recordOpenClawOriginals(path, originals); err != nil {
			return false
		}
		return true
	})
}

// restoreOpenClawSubAgents reverses redirectOpenClawSubAgents on disconnect,
// restoring each provider from the legacy in-file sidecar or the restore file.
func restoreOpenClawSubAgents(openclawDir string) {
	forEachSubAgentModelsFile(openclawDir, func(path string, root map[string]any) bool {
		providers, ok := root["providers"].(map[string]any)
		if !ok {
			return false
		}
		stored := openClawOriginalsFor(path)
		changed := false
		for name, raw := range providers {
			p, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if restoreProviderBaseURL(p) || restoreProviderFromStore(p, stored[name]) {
				changed = true
			}
		}
		if len(stored) > 0 {
			_ = clearOpenClawOriginals(path)
		}
		return changed
	})
}

// forEachSubAgentModelsFile reads each agents/<id>/agent/models.json under
// openclawDir, hands the file path and parsed root to mutate, and rewrites the
// file (with a backup) only when mutate reports a change. All I/O errors are
// ignored so a single bad sub-agent never fails the connect/disconnect of the
// others.
func forEachSubAgentModelsFile(openclawDir string, mutate func(path string, root map[string]any) bool) {
	entries, err := os.ReadDir(filepath.Join(openclawDir, "agents"))
	if err != nil {
		return
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		path := filepath.Join(openclawDir, "agents", ent.Name(), "agent", "models.json")
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var root map[string]any
		if err := json.Unmarshal(data, &root); err != nil {
			continue
		}
		if !mutate(path, root) {
			continue
		}
		_ = backupFile(path, data)
		_ = writeJSON(path, root)
	}
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
	return isKrouterBase(baseURL)
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
// config at configPath. Thin wrapper around ReadOpenClawProviderAPIKey.
func ReadOpenClawAPIKey(configPath string) string {
	return ReadOpenClawProviderAPIKey(configPath, "anthropic")
}

// ReadOpenClawProviderAPIKey transiently reads the apiKey for any provider from
// the OpenClaw config. The key is used only for model discovery and is never
// stored by krouter. Returns "" if the config cannot be read, if the key is
// absent, or if the value is the broken placeholder sentinel.
func ReadOpenClawProviderAPIKey(configPath, providerName string) string {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return ""
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return ""
	}
	provider := deepMap(root, "models", "providers", providerName)
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

// PreviewOpenClawConnect returns the config JSON before and after a hypothetical
// ConnectOpenClaw call, without modifying any files or creating backups.
func PreviewOpenClawConnect(configPath string) (before, after []byte, err error) {
	before, err = os.ReadFile(configPath)
	if err != nil {
		return nil, nil, fmt.Errorf("openclaw: read config: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(before, &root); err != nil {
		return nil, nil, fmt.Errorf("openclaw: parse config: %w", err)
	}

	// Preview reflects the global config diff only; per-agent models.json files
	// are rewritten by ConnectOpenClaw but are not part of this preview. The
	// returned originals are discarded — a preview must not touch the restore
	// file.
	_ = applyOpenClawConnectToRoot(root)

	after, err = json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, nil, fmt.Errorf("openclaw: marshal preview: %w", err)
	}
	return before, after, nil
}

// DisconnectOpenClaw reverses ConnectOpenClaw: restores every provider's baseUrl
// from the restore file (or the legacy in-file sidecar, which it also strips),
// and cleans up the krouter-injected anthropic fields. Real user-supplied
// apiKeys are never touched. Per-agent models.json files are restored the same
// way.
func DisconnectOpenClaw(configPath string) error {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return fmt.Errorf("openclaw: read config: %w", err)
	}

	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("openclaw: parse config: %w", err)
	}

	providers := deepMap(root, "models", "providers")
	stored := openClawOriginalsFor(configPath)

	// Anthropic: krouter sets `api` and may have created the whole section.
	if provider := deepMap(providers, "anthropic"); provider != nil {
		delete(provider, "api")
		restored := restoreProviderBaseURL(provider) ||
			restoreProviderFromStore(provider, stored["anthropic"])
		if !restored {
			delete(provider, "baseUrl")
		}
		// Remove only the broken placeholder written by old krouter versions;
		// leave real user-supplied apiKeys intact.
		if provider["apiKey"] == placeholderAPIKey {
			delete(provider, "apiKey")
		}
		// If no real apiKey remains and there was nothing to restore, the
		// section was created entirely by krouter — strip it back out.
		if _, hasRealKey := provider["apiKey"]; !hasRealKey && !restored {
			delete(provider, "models")
			if len(provider) == 0 {
				delete(providers, "anthropic")
			}
		}
	}

	// Every other provider: restore baseUrl from the legacy sidecar or the
	// restore file.
	for name, raw := range providers {
		if name == "anthropic" {
			continue
		}
		p, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if restoreProviderBaseURL(p) || restoreProviderFromStore(p, stored[name]) {
			continue
		}
		// Back-compat: configs connected by pre-sidecar krouter versions have no
		// recorded original. The only non-anthropic provider those versions
		// redirected was minimax-portal, whose original endpoint is known.
		if name == "minimax-portal" && p["baseUrl"] == proxyBase {
			p["baseUrl"] = minimaxPortalOriginalBaseURL
		}
	}

	if err := writeJSON(configPath, root); err != nil {
		return err
	}

	// The config is restored — its restore entry is spent. Best-effort: a
	// leftover entry can't clobber anything because restoreProviderFromStore
	// only ever touches baseUrls still pointing at krouter.
	_ = clearOpenClawOriginals(configPath)

	restoreOpenClawSubAgents(filepath.Dir(configPath))

	return nil
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
