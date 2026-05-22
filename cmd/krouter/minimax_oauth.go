package main

import (
	"context"
	"encoding/json"

	"github.com/kinthaiofficial/krouter/internal/storage"
)

// readMinimaxOAuthFromInheritedEndpoints looks for a MiniMax OAuth access
// token in inherited_endpoints.extras_json across all enabled agents. It
// returns "" when no token is available; the caller is expected to fall back
// to another source (typically the in-memory request-header cache).
//
// The expected ExtrasJSON shape is the one produced by OpenClawScanner:
//
//	{"oauth_token":"sk-cp-...","purpose":"subscription_oauth", ...}
//
// Other Scanners may write the token under a different provider name (e.g.
// "minimax" or "minimax-portal" depending on the agent); we try both
// well-known names in order.
func readMinimaxOAuthFromInheritedEndpoints(ctx context.Context, store *storage.Store) string {
	if store == nil {
		return ""
	}
	for _, providerName := range []string{"minimax-portal", "minimax"} {
		eps, err := store.FindInheritedEndpointsByProvider(ctx, providerName)
		if err != nil {
			continue
		}
		for _, ep := range eps {
			if ep.ExtrasJSON == "" {
				continue
			}
			var extras map[string]string
			if err := json.Unmarshal([]byte(ep.ExtrasJSON), &extras); err != nil {
				continue
			}
			if t := extras["oauth_token"]; t != "" {
				return t
			}
		}
	}
	return ""
}
