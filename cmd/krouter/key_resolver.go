package main

import (
	"github.com/kinthaiofficial/krouter/internal/agentscan"
)

// resolveProviderKeyForRouting returns the API key for providerName from the
// in-memory credential store (populated by the agent scanner; alias-aware).
// Returns "" when no credential is available. Wired into the keyFn closures
// handed to openai adapters in loadProvidersFromDB so routing (via
// providerHasKey → HasKey → keyFn) automatically reflects keys inherited
// from agent configs — without any credential ever touching SQLite (D-003).
func resolveProviderKeyForRouting(creds *agentscan.CredStore, providerName string) string {
	if creds == nil {
		return ""
	}
	return creds.KeyFor(providerName)
}
