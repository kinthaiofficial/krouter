package main

import (
	"github.com/kinthaiofficial/krouter/internal/agentscan"
)

// readMinimaxOAuthFromCreds looks for a MiniMax OAuth access token in the
// in-memory credential store (scanned from OpenClaw's auth-profiles.json).
// It returns "" when no token is available; the caller is expected to fall
// back to another source (typically the in-memory request-header cache).
//
// Scanners may record the token under different provider names depending on
// the agent ("minimax-portal" vs "minimax"); we try both well-known names.
func readMinimaxOAuthFromCreds(creds *agentscan.CredStore) string {
	if creds == nil {
		return ""
	}
	for _, providerName := range []string{"minimax-portal", "minimax"} {
		if t := creds.OAuthTokenFor(providerName); t != "" {
			return t
		}
	}
	return ""
}
