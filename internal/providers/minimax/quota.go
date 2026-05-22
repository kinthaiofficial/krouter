package minimax

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
)

const quotaAPIURL = "https://api.minimaxi.com/v1/token_plan/remains"

// QuotaPoller fetches MiniMax subscription quota from token_plan/remains and
// writes the result to the subscription_quota_cache table.
//
// The OAuth token comes from a pluggable resolver. The default resolver reads
// from the in-memory cache populated by proxied request headers (see
// CacheOAuthToken). serve.go overrides it via WithTokenResolver so the poller
// prefers tokens inherited from agent configs (inherited_endpoints.extras_json)
// over the request-traffic cache, which fixes the cold-start gap where the
// daemon couldn't poll until the user had sent a first MiniMax request.
//
// Poll schedule:
//   - Normal interval: 30 minutes
//   - When current window ends in < 30 minutes: poll every 5 minutes
//   - Skips poll if the resolver returns "" (no token available)
type QuotaPoller struct {
	store          *storage.Store
	httpClient     *http.Client
	resolver       TokenResolver
	onExhaust      ExhaustCallback
	onUnauthorized UnauthorizedCallback
}

// TokenResolver returns the OAuth token to use for the next poll, or "" to
// skip this cycle. ctx is honoured for any DB / network lookups the resolver
// performs.
type TokenResolver func(ctx context.Context) string

// ExhaustCallback is fired by PollOnce when it detects that a tier just
// transitioned from "had quota" to "zero remaining" in the current window.
// Used by serve.go to broadcast the spec/05 §12.3 `subscription_exhausted`
// SSE event so the dashboard can surface a toast / banner.
//
// Implementations should be fast and non-blocking (the poll loop holds no
// locks but waiting on a slow notifier delays the next iteration).
type ExhaustCallback func(provider, tier string, highspeed bool, windowEnd time.Time)

// UnauthorizedCallback is fired by PollOnce when the MiniMax token-plan API
// rejects our OAuth token — either via HTTP 401/403 or via the body's
// `base_resp.status_code = 1004` "login fail" code. serve.go installs a
// callback that triggers agentscan.RunAll so the daemon picks up any
// fresher token OpenClaw may have written to auth-profiles.json, and
// broadcasts a `subscription_unauthorized` SSE event so the dashboard can
// surface "OpenClaw OAuth expired, please re-login" if the rescan
// doesn't help.
//
// Implementations should be fast: the poll loop runs sequentially and a
// slow callback delays the next iteration.
type UnauthorizedCallback func()

// defaultTokenResolver reads from the in-memory request-header cache.
func defaultTokenResolver(_ context.Context) string { return GetCachedToken() }

// NewQuotaPoller creates a QuotaPoller backed by the given store and HTTP
// client. The default OAuth resolver reads from the in-memory cache populated
// by proxied requests; use WithTokenResolver to override (preferred).
func NewQuotaPoller(store *storage.Store, client *http.Client) *QuotaPoller {
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	return &QuotaPoller{store: store, httpClient: client, resolver: defaultTokenResolver}
}

// WithTokenResolver installs a custom OAuth-token resolver and returns the
// poller for chaining. Passing nil keeps the existing resolver.
func (p *QuotaPoller) WithTokenResolver(r TokenResolver) *QuotaPoller {
	if r != nil {
		p.resolver = r
	}
	return p
}

// WithExhaustCallback installs a callback fired on quota-exhausted
// transitions. Passing nil clears any installed callback.
func (p *QuotaPoller) WithExhaustCallback(cb ExhaustCallback) *QuotaPoller {
	p.onExhaust = cb
	return p
}

// WithUnauthorizedCallback installs a callback fired when MiniMax rejects
// the polled OAuth token. Passing nil clears any installed callback.
func (p *QuotaPoller) WithUnauthorizedCallback(cb UnauthorizedCallback) *QuotaPoller {
	p.onUnauthorized = cb
	return p
}

// Start runs the polling loop until ctx is cancelled.
func (p *QuotaPoller) Start(ctx context.Context) {
	// Initial poll after a short delay so daemon startup is not blocked.
	timer := time.NewTimer(20 * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			_ = p.PollOnce(ctx)
			timer.Reset(p.nextInterval(ctx))
		}
	}
}

// PollOnce performs a single quota fetch and persists results. Exported for testing.
func (p *QuotaPoller) PollOnce(ctx context.Context) error {
	token := p.resolver(ctx)
	if token == "" {
		return nil // no OAuth token available yet; skip silently
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, quotaAPIURL, nil)
	if err != nil {
		return fmt.Errorf("minimax quota: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("minimax quota: http: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 512*1024))
	if err != nil {
		return fmt.Errorf("minimax quota: read body: %w", err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// Theoretical for MiniMax — its token-plan endpoint reports auth
		// failures as HTTP 200 + base_resp.status_code = 1004 (see
		// parseQuotaResponse below) — but we handle the conventional
		// status-code path too in case the API changes or a CDN in front
		// of it rewrites the status.
		p.fireUnauthorized()
		return fmt.Errorf("%w: HTTP %d", ErrMinimaxAuth, resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("minimax quota: upstream %d: %s", resp.StatusCode, body)
	}

	quotas, err := parseQuotaResponse(body)
	if err != nil {
		if errors.Is(err, ErrMinimaxAuth) {
			p.fireUnauthorized()
		}
		return fmt.Errorf("minimax quota: parse: %w", err)
	}

	now := time.Now().UTC()
	for _, q := range quotas {
		q.FetchedAt = now

		// Detect quota-exhausted transitions BEFORE the upsert so we can
		// compare new vs old state. Two cases fire the callback:
		//
		//   (a) old row exists for the same window_end, had quota left,
		//       and the new row reports zero remaining → user just hit
		//       the wall in this window
		//   (b) old row doesn't exist or covers a different window
		//       (window rolled over) AND the new row is already at zero
		//       → unusual, but treat it the same way so the dashboard
		//       reflects the state
		//
		// Window-end equality is the dedupe key: if the window already
		// rolled over to a new one (same provider/pattern but a later
		// window_end) we treat that as a fresh observation, not a repeat.
		if p.onExhaust != nil && newRemaining(q) == 0 {
			old, _ := p.store.GetSubscriptionQuota(ctx, q.Provider, q.ModelPattern)
			if shouldFireExhaust(old, q) {
				p.onExhaust(q.Provider, q.ModelPattern, q.Highspeed, q.WindowEnd)
			}
		}

		if err := p.store.UpsertSubscriptionQuota(ctx, q); err != nil {
			return fmt.Errorf("minimax quota: save %s: %w", q.ModelPattern, err)
		}
	}
	return nil
}

// newRemaining is a tiny helper so PollOnce stays readable.
func newRemaining(q storage.SubscriptionQuota) int64 {
	r := q.TotalCount - q.UsedCount
	if r < 0 {
		return 0
	}
	return r
}

// shouldFireExhaust returns true when the (old, new) tuple represents a
// fresh exhaustion event that hasn't been observed for this window yet.
//
// Cases that fire:
//   - old == nil, new.remaining == 0: first ever observation, already at zero
//   - old.window_end != new.window_end, new.remaining == 0: new window, but
//     somehow already exhausted (rare; we still notify so the dashboard
//     reflects the state)
//   - old.window_end == new.window_end AND old.remaining > 0 AND
//     new.remaining == 0: the most common case — user just used up the
//     window's quota
//
// Cases that DON'T fire:
//   - same window, both exhausted: we already notified for this window
//   - new.remaining > 0: nothing to notify about
func shouldFireExhaust(old *storage.SubscriptionQuota, fresh storage.SubscriptionQuota) bool {
	if newRemaining(fresh) != 0 {
		return false
	}
	if old == nil {
		return true
	}
	if !old.WindowEnd.Equal(fresh.WindowEnd) {
		return true
	}
	return (old.TotalCount - old.UsedCount) > 0
}

// nextInterval returns the polling interval based on how soon the current window ends.
func (p *QuotaPoller) nextInterval(ctx context.Context) time.Duration {
	quotas, err := p.store.GetAllSubscriptionQuotas(ctx)
	if err != nil || len(quotas) == 0 {
		return 30 * time.Minute
	}
	now := time.Now().UTC()
	var earliest time.Time
	for _, q := range quotas {
		if q.Provider != "minimax" {
			continue
		}
		if earliest.IsZero() || q.WindowEnd.Before(earliest) {
			earliest = q.WindowEnd
		}
	}
	if !earliest.IsZero() && earliest.Sub(now) < 30*time.Minute {
		return 5 * time.Minute
	}
	return 30 * time.Minute
}

// quotaAPIResponse mirrors the JSON returned by token_plan/remains.
type quotaAPIResponse struct {
	BaseResp struct {
		StatusCode int    `json:"status_code"`
		StatusMsg  string `json:"status_msg"`
	} `json:"base_resp"`
	ModelRemains []struct {
		ModelName                   string `json:"model_name"`
		StartTime                   int64  `json:"start_time"`
		EndTime                     int64  `json:"end_time"`
		CurrentIntervalTotalCount   int64  `json:"current_interval_total_count"`
		CurrentIntervalUsageCount   int64  `json:"current_interval_usage_count"`
	} `json:"model_remains"`
}

// parseQuotaResponse converts the raw API response into storage rows.
// ErrMinimaxAuth is the sentinel error wrapped when the MiniMax token-plan API
// reports an authentication failure — either via HTTP 401/403 from PollOnce
// or via the body's base_resp.status_code = 1004 ("login fail") that the
// quota endpoint returns as HTTP 200 (see spec/05 §13). Callers can use
// errors.Is(err, ErrMinimaxAuth) to decide whether to trigger an agent
// rescan / surface a re-auth nudge to the user.
var ErrMinimaxAuth = errors.New("minimax: authentication failed")

// Only text model plans (MiniMax-M*) are stored; speech/video plans are skipped.
func parseQuotaResponse(data []byte) ([]storage.SubscriptionQuota, error) {
	var resp quotaAPIResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, err
	}
	if resp.BaseResp.StatusCode != 0 {
		// status_code 1004 is MiniMax's "login fail" / unauthenticated code
		// (documented in spec/05 §13). Wrap the sentinel so the poller can
		// special-case it without parsing the message string.
		if resp.BaseResp.StatusCode == 1004 {
			return nil, fmt.Errorf("%w: status_code=%d msg=%s",
				ErrMinimaxAuth, resp.BaseResp.StatusCode, resp.BaseResp.StatusMsg)
		}
		return nil, fmt.Errorf("API error %d: %s", resp.BaseResp.StatusCode, resp.BaseResp.StatusMsg)
	}

	var out []storage.SubscriptionQuota
	for _, m := range resp.ModelRemains {
		// Only track text generation models (MiniMax-M*).
		if !strings.HasPrefix(m.ModelName, "MiniMax-M") {
			continue
		}
		if m.CurrentIntervalTotalCount == 0 {
			continue // weekly-only quota or inactive plan; skip
		}
		highspeed := strings.Contains(strings.ToLower(m.ModelName), "highspeed")
		out = append(out, storage.SubscriptionQuota{
			Provider:     "minimax",
			ModelPattern: m.ModelName,
			WindowStart:  time.UnixMilli(m.StartTime).UTC(),
			WindowEnd:    time.UnixMilli(m.EndTime).UTC(),
			TotalCount:   m.CurrentIntervalTotalCount,
			UsedCount:    m.CurrentIntervalUsageCount,
			Highspeed:    highspeed,
		})
	}
	return out, nil
}

// fireUnauthorized invokes the registered callback (if any). Kept as a
// method so the call sites in PollOnce stay short and the nil-guard lives
// in one place.
func (p *QuotaPoller) fireUnauthorized() {
	if p.onUnauthorized != nil {
		p.onUnauthorized()
	}
}
