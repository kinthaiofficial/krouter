package agentscan

import (
	"sync"

	"github.com/kinthaiofficial/krouter/internal/storage"
)

// Credential is one secret extracted from an AI app's config file: a static
// API key and/or an OAuth token for a provider.
//
// Credentials live ONLY in this in-memory store — they are never written to
// SQLite or any file (D-003). The store is repopulated from the agent config
// files by the scanner at daemon startup, on the 1-minute periodic rescan,
// and on manual rescans, so a key the user rotates in their agent config is
// picked up without krouter ever holding a stale copy on disk.
type Credential struct {
	AppID      string // owning application ("openclaw", "codex", …)
	Provider   string // provider name as scanned (may be a vendor alias)
	APIKey     string // static API key, if any
	OAuthToken string // OAuth access token (e.g. MiniMax portal), if any
}

// CredStore is a thread-safe in-memory credential store, keyed by app.
// Lookups by provider are alias-aware (storage.CanonicalProviderName), so a
// key the agent stored under "dashscope" resolves for krouter's "qwen"
// adapter.
//
// Only enabled apps have entries: the scanner populates per-app on scan, and
// the API layer removes an app's entry when the user disables or deletes it.
type CredStore struct {
	mu    sync.RWMutex
	byApp map[string][]Credential
}

// NewCredStore returns an empty credential store.
func NewCredStore() *CredStore {
	return &CredStore{byApp: make(map[string][]Credential)}
}

// ReplaceApp atomically replaces all credentials for appID with the given
// set. An empty or nil slice removes the app's entry.
func (c *CredStore) ReplaceApp(appID string, creds []Credential) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(creds) == 0 {
		delete(c.byApp, appID)
		return
	}
	cp := make([]Credential, len(creds))
	copy(cp, creds)
	c.byApp[appID] = cp
}

// RemoveApp drops all credentials for appID (app disabled or deleted).
func (c *CredStore) RemoveApp(appID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.byApp, appID)
}

// CredentialsFor returns all credentials whose provider matches the given
// name after alias canonicalization, across all apps. Order is unspecified.
func (c *CredStore) CredentialsFor(provider string) []Credential {
	target := storage.CanonicalProviderName(provider)
	c.mu.RLock()
	defer c.mu.RUnlock()
	var out []Credential
	for _, creds := range c.byApp {
		for _, cr := range creds {
			if storage.CanonicalProviderName(cr.Provider) == target {
				out = append(out, cr)
			}
		}
	}
	return out
}

// KeyFor returns the first non-empty API key for the provider, or "".
func (c *CredStore) KeyFor(provider string) string {
	for _, cr := range c.CredentialsFor(provider) {
		if cr.APIKey != "" {
			return cr.APIKey
		}
	}
	return ""
}

// OAuthTokenFor returns the first non-empty OAuth token for the provider, or "".
func (c *CredStore) OAuthTokenFor(provider string) string {
	for _, cr := range c.CredentialsFor(provider) {
		if cr.OAuthToken != "" {
			return cr.OAuthToken
		}
	}
	return ""
}

// ProvidersWithKey returns the distinct as-scanned provider names that have a
// non-empty API key. Used to decide which providers are worth model discovery.
func (c *CredStore) ProvidersWithKey() []string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	seen := map[string]struct{}{}
	var out []string
	for _, creds := range c.byApp {
		for _, cr := range creds {
			if cr.APIKey == "" {
				continue
			}
			if _, ok := seen[cr.Provider]; ok {
				continue
			}
			seen[cr.Provider] = struct{}{}
			out = append(out, cr.Provider)
		}
	}
	return out
}
