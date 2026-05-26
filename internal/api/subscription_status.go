package api

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
)

// ─── DTOs ─────────────────────────────────────────────────────────────────

// subscriptionTierJSON is one row in the per-provider tier list.
type subscriptionTierJSON struct {
	TierName                string  `json:"tier_name"`
	Total                   int64   `json:"total"`
	Used                    int64   `json:"used"`
	Remaining               int64   `json:"remaining"`
	Highspeed               bool    `json:"highspeed"`
	WindowStart             string  `json:"window_start"`              // RFC3339
	WindowEnd               string  `json:"window_end"`                // RFC3339
	SecondsToReset          int64   `json:"seconds_to_reset"`          // negative when window is past
	EffectiveCostPerCallUSD float64 `json:"effective_cost_per_call_usd"`
	MonthlyPriceCNY         float64 `json:"monthly_price_cny"`         // original sticker price on minimaxi.com
	MonthlyPriceUSD         float64 `json:"monthly_price_usd"`         // CNY × fixed FX rate; for cross-vendor comparison
}

type subscriptionProviderJSON struct {
	Provider     string                 `json:"provider"`
	SourceApp  string                 `json:"source_app,omitempty"`  // which agent supplied the OAuth/API key
	OAuthPresent bool                   `json:"oauth_present"`
	LastPolledAt string                 `json:"last_polled_at,omitempty"` // RFC3339 of newest tier row
	Tiers        []subscriptionTierJSON `json:"tiers"`
}

// ─── GET /internal/subscription/status ─────────────────────────────────────

// handleSubscriptionStatus returns the current subscription-quota state
// aggregated by provider. Reads from subscription_quota_cache (populated by
// QuotaPoller) and joins with inherited_endpoints to surface which agent
// supplied the credential.
//
// Spec: 05-subscription-quota §12.1.
func (s *Server) handleSubscriptionStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.writeSubscriptionStatus(w, r.Context())
}

// writeSubscriptionStatus emits the JSON payload that GET status returns.
// Shared with the refresh handler so both reads see the freshest poll
// without a second round-trip from the client.
func (s *Server) writeSubscriptionStatus(w http.ResponseWriter, ctx context.Context) {
	if s.store == nil {
		writeJSON(w, []subscriptionProviderJSON{})
		return
	}
	quotas, err := s.store.GetAllSubscriptionQuotas(ctx)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	byProvider := groupQuotasByProvider(quotas)
	out := make([]subscriptionProviderJSON, 0, len(byProvider))
	for provider, tiers := range byProvider {
		sourceAgent, oauthPresent := s.subscriptionAuthSource(ctx, provider)
		out = append(out, subscriptionProviderJSON{
			Provider:     provider,
			SourceApp:  sourceAgent,
			OAuthPresent: oauthPresent,
			LastPolledAt: newestFetchedAt(tiers),
			Tiers:        tiersToJSON(ctx, s.store, tiers),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	writeJSON(w, out)
}

// ─── POST /internal/subscription/refresh ───────────────────────────────────
//
// Body: optional {"provider": "minimax"} — when omitted, refresh all
// supported subscription providers. Returns the same shape as GET status
// after the refresh has run.

func (s *Server) handleSubscriptionRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		Provider string `json:"provider"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
	}

	provider := body.Provider
	if provider == "" || provider == "minimax" || provider == "minimax-portal" {
		// Currently only MiniMax has a polling implementation.
		s.refreshMinimaxQuota(r.Context())
	}
	// Re-emit the latest status so the caller doesn't need two round-trips.
	s.writeSubscriptionStatus(w, r.Context())
}

func (s *Server) refreshMinimaxQuota(ctx context.Context) {
	if s.minimaxPoller == nil || s.store == nil {
		return
	}
	if err := s.minimaxPoller.PollOnce(ctx); err == nil {
		s.Broadcast("subscription_quota_refreshed", map[string]any{"provider": "minimax"})
	}
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func groupQuotasByProvider(quotas []storage.SubscriptionQuota) map[string][]storage.SubscriptionQuota {
	out := map[string][]storage.SubscriptionQuota{}
	for _, q := range quotas {
		out[q.Provider] = append(out[q.Provider], q)
	}
	return out
}

func newestFetchedAt(tiers []storage.SubscriptionQuota) string {
	var newest time.Time
	for _, t := range tiers {
		if t.FetchedAt.After(newest) {
			newest = t.FetchedAt
		}
	}
	if newest.IsZero() {
		return ""
	}
	return newest.UTC().Format(time.RFC3339)
}

func tiersToJSON(ctx context.Context, store *storage.Store, tiers []storage.SubscriptionQuota) []subscriptionTierJSON {
	sort.Slice(tiers, func(i, j int) bool {
		if tiers[i].ModelPattern == tiers[j].ModelPattern {
			return !tiers[i].Highspeed && tiers[j].Highspeed
		}
		return tiers[i].ModelPattern < tiers[j].ModelPattern
	})
	out := make([]subscriptionTierJSON, 0, len(tiers))
	now := time.Now().UTC()
	for i := range tiers {
		t := &tiers[i]
		remaining := t.TotalCount - t.UsedCount
		if remaining < 0 {
			remaining = 0
		}
		// Pricing comes from token_price_sub via SubscriptionQuota.PricingFor,
		// the same lookup the routing engine consumes. This guarantees the
		// dashboard cost matches what routing sees (see spec/05 §11 for the
		// PR #1 dual-table bug history).
		price := t.PricingFor(ctx, store)
		out = append(out, subscriptionTierJSON{
			TierName:                t.ModelPattern,
			Total:                   t.TotalCount,
			Used:                    t.UsedCount,
			Remaining:               remaining,
			Highspeed:               t.Highspeed,
			WindowStart:             t.WindowStart.UTC().Format(time.RFC3339),
			WindowEnd:               t.WindowEnd.UTC().Format(time.RFC3339),
			SecondsToReset:          int64(t.WindowEnd.Sub(now).Seconds()),
			EffectiveCostPerCallUSD: price.EffectiveCostPerCallUSD(),
			MonthlyPriceCNY:         price.MonthlyPriceCNY,
			MonthlyPriceUSD:         price.MonthlyPriceUSD(),
		})
	}
	return out
}

// subscriptionAuthSource finds which enabled agent owns the inherited
// endpoint for this provider, and whether that endpoint carries an OAuth
// token. provider is the krouter-internal name; we try -portal first because
// that's the OpenClaw convention.
func (s *Server) subscriptionAuthSource(ctx context.Context, provider string) (agentID string, oauthPresent bool) {
	if s.store == nil {
		return "", false
	}
	candidates := []string{provider}
	switch provider {
	case "minimax":
		candidates = []string{"minimax-portal", "minimax"}
	}
	for _, p := range candidates {
		eps, err := s.store.FindInheritedEndpointsByProvider(ctx, p)
		if err != nil {
			continue
		}
		for _, ep := range eps {
			if ep.ExtrasJSON != "" {
				var extras map[string]string
				if json.Unmarshal([]byte(ep.ExtrasJSON), &extras) == nil && extras["oauth_token"] != "" {
					return ep.AppID, true
				}
			}
			if ep.APIKey != "" && agentID == "" {
				agentID = ep.AppID
			}
		}
	}
	return agentID, false
}
