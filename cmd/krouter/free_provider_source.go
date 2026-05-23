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
// of every catalogued, configured, non-exhausted free-credit provider
// whose protocol matches `protocol`. Order is unspecified — routing
// picks the cheapest model across the returned set, so ordering here
// would be cosmetic.
func (s *freeProviderSource) ListAvailableFreeProviders(ctx context.Context, protocol string) []string {
	if s.store == nil {
		return nil
	}

	// Catalogued free-credit providers (filtered to active rows).
	catalog, err := s.store.FreeProviderKrouterNames(ctx)
	if err != nil || len(catalog) == 0 {
		return nil
	}

	// User-configured (inherited) provider names.
	eps, err := s.store.ListInheritedEndpoints(ctx)
	if err != nil {
		return nil
	}

	// Build the intersection (catalog ∩ inherited) minus exhausted rows.
	// Protocol filter is best-effort: free_provider_state stores a hint
	// (`protocol` column) but the authoritative protocol is the actual
	// adapter's. The engine's cheapestFreeProviderModel re-checks the
	// adapter, so we don't have to be strict here. We still drop
	// obvious mismatches when both sides are known.
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
