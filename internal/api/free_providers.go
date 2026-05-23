package api

import (
	"net/http"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
)

// ─── DTOs ──────────────────────────────────────────────────────────────────

type freeProviderJSON struct {
	ID                  string  `json:"id"`
	DisplayName         string  `json:"display_name"`
	KrouterProviderName string  `json:"krouter_provider_name"`
	Protocol            string  `json:"protocol"`
	Region              string  `json:"region"`
	FreeType            string  `json:"free_type"`
	FreeSummary         string  `json:"free_summary"`
	FreeQuotaUSD        float64 `json:"free_quota_usd"`
	Validity            string  `json:"validity"`
	Conditions          string  `json:"conditions"`
	SignupURL           string  `json:"signup_url"`
	KeySetupHint        string  `json:"key_setup_hint"`
	LastVerified        string  `json:"last_verified"`
	Notes               string  `json:"notes,omitempty"`

	// Runtime-joined fields:
	UserConfigured  bool   `json:"user_configured"`            // matches an inherited_endpoints row
	SourceAgent     string `json:"source_agent,omitempty"`     // which agent supplied the key
	Exhausted       bool   `json:"exhausted,omitempty"`        // 4xx mark currently active
	ExhaustedUntil  string `json:"exhausted_until,omitempty"`  // RFC3339 of expiry
	ExhaustedReason string `json:"exhausted_reason,omitempty"` // e.g. "HTTP 402 quota_exceeded"
}

// handleFreeProviders returns the free-provider catalog joined with the
// user's inherited_endpoints, so the dashboard can show "you've already
// applied for and configured this one" vs "this one's still waiting for
// you to claim it".
//
// The joined fields are the value-add over a plain JSON dump of
// data/free_tokens.json: they let the UI sort/filter by claim status and
// make the routing-engine's reasoning visible ("DeepSeek key inherited
// from OpenClaw; routing prefers this for openai-protocol requests").
func (s *Server) handleFreeProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		writeJSON(w, []freeProviderJSON{})
		return
	}

	ctx := r.Context()
	providers, err := s.store.ListFreeProviders(ctx, true /* activeOnly */)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Build a one-shot map of provider-name → first-inheriting-agent for
	// the join. We hit ListInheritedEndpoints once instead of per-row.
	inheritedByName := map[string]string{} // provider → agent_id
	if eps, err := s.store.ListInheritedEndpoints(ctx); err == nil {
		for _, ep := range eps {
			if _, seen := inheritedByName[ep.Provider]; !seen {
				inheritedByName[ep.Provider] = ep.AgentID
			}
		}
	}

	out := make([]freeProviderJSON, 0, len(providers))
	for _, p := range providers {
		row := freeProviderJSON{
			ID:                  p.ID,
			DisplayName:         p.DisplayName,
			KrouterProviderName: p.KrouterProviderName,
			Protocol:            p.Protocol,
			Region:              p.Region,
			FreeType:            p.FreeType,
			FreeSummary:         p.FreeSummary,
			FreeQuotaUSD:        p.FreeQuotaUSD,
			Validity:            p.Validity,
			Conditions:          p.Conditions,
			SignupURL:           p.SignupURL,
			KeySetupHint:        p.KeySetupHint,
			LastVerified:        p.LastVerified,
			Notes:               p.Notes,
		}
		if agentID, ok := inheritedByName[p.KrouterProviderName]; ok {
			row.UserConfigured = true
			row.SourceAgent = agentID
		}
		if s.store.IsProviderExhausted(ctx, p.KrouterProviderName) {
			row.Exhausted = true
			// We could surface the exact expiry but the DB column is just an
			// "until" timestamp; clients can render the badge without it.
			// If a future UI needs the timestamp we add a JOIN; for now
			// keep the response shape compact.
			_ = time.Now // placeholder for the explicit-timestamp future
		}
		out = append(out, row)
	}
	writeJSON(w, out)
}

// freeProvidersHandler returns the http.Handler form for mux registration.
// (Defined here rather than inline in server.go so the routing for this
// feature stays in one file.)
func (s *Server) freeProvidersHandler() http.Handler {
	return http.HandlerFunc(s.handleFreeProviders)
}

// ── helpers below kept un-exported (consumed only by the handler) ─────────

// _ avoids "imported and not used" if a future change drops the
// storage import; explicitly reference it so go vet stays quiet without
// a blank import.
var _ = storage.FreeProvider{}.ID
