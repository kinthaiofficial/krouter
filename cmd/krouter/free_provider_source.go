package main

import (
	"context"

	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

// freeProviderSource implements routing.FreeProviderSource by joining the
// free_provider_state catalog (curated by data/free_tokens.json) with the
// user's inherited_endpoints. A provider qualifies as "available free
// credit" when:
//
//   1. data/free_tokens.json lists it (i.e. an active row in
//      free_provider_state with this krouter_provider_name).
//   2. The user has actually configured an API key for it in some
//      enabled agent (inherited_endpoints row matches by provider name).
//   3. It is NOT currently marked exhausted in provider_exhausted_until.
//
// The routing engine consults this list before falling back to the paid
// cheapest path, so the user gets free-credit savings automatically the
// moment they paste a key into OpenClaw — no dashboard configuration
// required.
type freeProviderSource struct {
	store *storage.Store
}

func newFreeProviderSource(store *storage.Store) *freeProviderSource {
	return &freeProviderSource{store: store}
}

// ListAvailableFreeProviders returns the krouter_provider_name strings
// of catalogued, configured, non-exhausted free-credit providers whose
// protocol matches `protocol`.
//
// Protocol-aware (spec/00 §B2): an anthropic-protocol request only sees
// anthropic-side krouter_provider_names, even if the same vendor has an
// openai entry alongside it. Dual-protocol vendors (OpenRouter, GLM,
// Moonshot per data/free_tokens.json) appear under both protocols' lists
// — each with the dedicated krouter_provider_name the user configured
// in their agent for that protocol's baseURL.
//
// Order is unspecified — the routing engine picks the cheapest model
// across the returned set, so ordering here would be cosmetic.
func (s *freeProviderSource) ListAvailableFreeProviders(ctx context.Context, protocol string) []string {
	if s.store == nil {
		return nil
	}

	// Protocol → set-of-krouter-names from the catalog (active rows only).
	byProto, err := s.store.FreeProviderKrouterNamesByProtocol(ctx)
	if err != nil {
		return nil
	}
	catalog := byProto[protocol]
	if len(catalog) == 0 {
		return nil
	}

	// User-configured (inherited) provider names — across all enabled
	// agents. The actual adapter for each provider is registered with a
	// specific protocol (`reg.Get(name).Protocol()` in the engine); the
	// engine re-checks that as a guard. Here we just filter by name
	// membership in `catalog[protocol]`.
	eps, err := s.store.ListInheritedEndpoints(ctx)
	if err != nil {
		return nil
	}

	seen := map[string]struct{}{}
	for _, ep := range eps {
		name := ep.Provider
		if _, ok := catalog[name]; !ok {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		if s.store.IsProviderExhausted(ctx, name) {
			continue
		}
		seen[name] = struct{}{}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	return out
}

// Compile-time check that the implementation satisfies the interface.
// (Cheap protection against future signature drift.)
var _ routing.FreeProviderSource = (*freeProviderSource)(nil)
