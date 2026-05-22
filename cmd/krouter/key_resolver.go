package main

import (
	"context"

	"github.com/kinthaiofficial/krouter/internal/storage"
)

// resolveProviderKeyForRouting returns the API key for providerName by looking
// up inherited_endpoints from enabled agents. Returns "" when no credential is
// available. Wired into the keyFn closures handed to openai adapters in
// loadProvidersFromDB so routing (via providerHasKey → HasKey → keyFn)
// automatically reflects keys inherited from agent configs.
//
// This helper does not take a context because the callsite (provider adapter's
// keyFn) is a synchronous callback; context.Background is appropriate for a
// fast in-process SQLite read.
func resolveProviderKeyForRouting(store *storage.Store, providerName string) string {
	if store != nil {
		if eps, err := store.FindInheritedEndpointsByProvider(context.Background(), providerName); err == nil {
			for _, ep := range eps {
				if ep.APIKey != "" {
					return ep.APIKey
				}
			}
		}
	}
	return ""
}
