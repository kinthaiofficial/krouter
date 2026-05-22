package main

import (
	"context"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

// resolveProviderKeyForRouting returns the API key for providerName, applying
// the same precedence rule as Server.resolveProviderKey:
//
//  1. inherited_endpoints.api_key from any enabled agent
//  2. settings.ProviderKeys[providerName] (dashboard manual override)
//
// Returns "" when neither source has a credential. Wired into the keyFn
// closures handed to openai adapters in loadProvidersFromDB so that routing
// (via providerHasKey → HasKey → resolveKey → keyFn) automatically reflects
// keys inherited from agent configs.
//
// This helper does not take a context because the callsite (provider adapter's
// keyFn) is a synchronous callback with no context to propagate; we use
// context.Background which is appropriate for a fast in-process SQLite read.
func resolveProviderKeyForRouting(store *storage.Store, settings *config.Manager, providerName string) string {
	if store != nil {
		if eps, err := store.FindInheritedEndpointsByProvider(context.Background(), providerName); err == nil {
			for _, ep := range eps {
				if ep.APIKey != "" {
					return ep.APIKey
				}
			}
		}
	}
	if settings != nil {
		if k := settings.Get().ProviderKeys[providerName]; k != "" {
			return k
		}
	}
	return ""
}
