// Package subpricing implements the daemon's background sync of MiniMax
// subscription pricing data from the kinthai.ai canonical endpoint
// (primary) with a fallback to GitHub raw.
//
// Why this exists: the embedded data/token_price_sub.json snapshot ships
// with each krouter binary release. When MiniMax revises a tier price
// (historically ~once or twice a year, but the user-facing impact is
// large), waiting for the next krouter release would leave dashboards
// showing wrong cost figures for weeks. The sync loop closes that gap by
// pulling the same file every 24 hours.
//
// Distribution channels (in fetch order):
//
//  1. Primary — kinthai.ai (self-hosted):
//     https://krouter.kinthai.ai/data/token_price_sub.json
//     Operators control this endpoint, so access logs give us the fleet's
//     deployed-version distribution (the daemon sends a versioned
//     User-Agent), daily unique IP count, 304 vs 200 ratio, and outage
//     visibility. The file content must mirror the canonical
//     data/token_price_sub.json on main; the typical operator workflow
//     is a CI job that re-uploads the file when a commit to main touches
//     it.
//
//  2. Fallback — GitHub raw:
//     https://raw.githubusercontent.com/kinthaiofficial/krouter/main/data/token_price_sub.json
//     The same file served directly by GitHub. Used only when the
//     primary endpoint fails (kinthai.ai outage, DNS issue, etc.).
//     GitHub raw has no operator-visible stats but provides resilience
//     against single-operator failure.
//
// Both URLs must support ETag for the 304 cache-hit path; otherwise every
// poll downloads the file even though it rarely changes.
//
// The daemon sets `User-Agent: krouter-subpricing-sync/<version>` so
// operators reading access logs can see the version distribution of the
// fleet without running any in-product telemetry.
package subpricing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

const (
	// PrimaryURL is the canonical pricing source hosted on kinthai.ai
	// infrastructure. Operating this endpoint ourselves (rather than
	// relying on GitHub raw) gives us access logs — daily unique IPs,
	// User-Agent breakdown (the daemon sets a versioned UA below so the
	// log shows the fleet's version distribution), 304 vs 200 ratio,
	// geographic distribution. raw.githubusercontent.com has no such
	// stats exposed to the repo owner, only GitHub-internal logs.
	//
	// Operators need to mirror data/token_price_sub.json from the main
	// branch to this URL. ETag/Last-Modified headers must be honoured;
	// the daemon sends If-None-Match on every poll and expects 304
	// when the file is unchanged.
	PrimaryURL = "https://krouter.kinthai.ai/data/token_price_sub.json"

	// FallbackURL is GitHub raw serving the same on-disk file. Used
	// only when the primary endpoint fails (kinthai.ai outage, DNS
	// issue, etc.). The file path matches the canonical location so
	// any developer editing data/token_price_sub.json on main updates
	// both the embed (for the next release) and the fallback URL (for
	// running daemons) in a single commit — kinthai.ai is the only
	// channel that requires a separate publish step.
	FallbackURL = "https://raw.githubusercontent.com/kinthaiofficial/krouter/main/data/token_price_sub.json"

	// metaPrefix scopes our keys in token_price_api_meta so they don't
	// collide with the LiteLLM pricing sync that lives in the pricing
	// package (which uses unprefixed "last_etag" / "last_synced_at").
	metaPrefix = "sub_price_"

	// maxBodyBytes caps the size of the response body we'll read so a
	// hostile or accidentally-huge file can't OOM the daemon. The real
	// file is ~2 KB today; 1 MB is plenty of room.
	maxBodyBytes = 1 << 20
)

// Service runs the subscription-pricing sync loop. Construct via New,
// optionally call WithHTTPClient to inject a proxy-aware client, then call
// StartSync to enter the daemon loop.
type Service struct {
	store      *storage.Store
	logger     logging.Logger
	httpClient *http.Client
	userAgent  string

	// onUpdate fires whenever SyncOnce successfully writes new rows. It is
	// expected to broadcast an SSE event to dashboards so they refetch the
	// /internal/subscription/status payload immediately. Optional — leave
	// nil if the caller doesn't care.
	onUpdate func(updatedCount int)
}

// New creates a Service that uses the supplied store and logger. The HTTP
// client defaults to a 15-second timeout; callers that need a proxy-aware
// transport (most production callers) should follow up with WithHTTPClient.
// The default User-Agent is "krouter-subpricing-sync/dev"; serve.go passes
// the actual daemon version through WithVersion so kinthai.ai access logs
// can show fleet-version distribution.
func New(store *storage.Store, logger logging.Logger) *Service {
	return &Service{
		store:      store,
		logger:     logger,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		userAgent:  "krouter-subpricing-sync/dev",
	}
}

// WithVersion sets the daemon version tag in the User-Agent header so the
// operator's access logs can break down fleet version distribution
// without needing in-product telemetry. Passing "" keeps the default.
func (s *Service) WithVersion(v string) *Service {
	if v != "" {
		s.userAgent = "krouter-subpricing-sync/" + v
	}
	return s
}

// WithHTTPClient replaces the default HTTP client (typically with one that
// uses the daemon's proxy-aware transport).
func (s *Service) WithHTTPClient(c *http.Client) *Service {
	if c != nil {
		s.httpClient = c
	}
	return s
}

// WithUpdateCallback installs a function that fires on each successful
// sync that wrote one or more rows. Pass nil to clear.
func (s *Service) WithUpdateCallback(cb func(updatedCount int)) *Service {
	s.onUpdate = cb
	return s
}

// StartSync runs SyncOnce on a fixed interval until ctx is cancelled. A
// short delay (30 s) precedes the first sync so daemon startup is not
// blocked on a network call. Failures are logged but never propagated —
// the daemon keeps running with whatever pricing data the embed seeded.
func (s *Service) StartSync(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := s.SyncOnce(ctx); err != nil {
				s.logger.Warn("subpricing: sync failed", "err", err)
			}
			timer.Reset(interval)
		}
	}
}

// SyncOnce performs one fetch + parse + upsert cycle. Exported for tests
// and for the dashboard's manual-refresh button.
//
// The primary URL is tried first with the cached ETag attached as
// If-None-Match. A 304 short-circuits to "nothing to do". A 200 means the
// file changed; we parse it, replace the matching rows in token_price_sub,
// and store the new ETag. Any error (network, non-2xx, parse failure)
// falls through to the fallback URL.
//
// If both URLs fail, SyncOnce returns an error wrapping both — the daemon
// keeps the previously-stored rows in place.
func (s *Service) SyncOnce(ctx context.Context) error {
	body, etag, err := s.tryFetch(ctx, PrimaryURL, "primary")
	if err != nil {
		// Primary failed (or 304 with no cached body — see tryFetch).
		// Distinguish "no change" from "actual error" before falling back.
		if errors.Is(err, errNotModified) {
			return nil
		}
		s.logger.Warn("subpricing: primary failed, trying fallback",
			"primary_err", err)
		body, etag, err = s.tryFetch(ctx, FallbackURL, "fallback")
		if err != nil {
			if errors.Is(err, errNotModified) {
				return nil
			}
			return fmt.Errorf("primary + fallback both failed: %w", err)
		}
	}

	count, err := s.applyBody(ctx, body)
	if err != nil {
		return fmt.Errorf("apply: %w", err)
	}

	// Store the ETag for the URL we actually fetched from. We don't share
	// ETags between primary and fallback — they may be different files'
	// metadata even if their content matches.
	if etag != "" {
		_ = s.store.SetSyncMeta(ctx, metaPrefix+"etag", etag)
	}
	_ = s.store.SetSyncMeta(ctx, metaPrefix+"last_synced_at",
		time.Now().UTC().Format(time.RFC3339))

	s.logger.Info("subpricing: synced", "rows", count)
	if s.onUpdate != nil && count > 0 {
		s.onUpdate(count)
	}
	return nil
}

// errNotModified signals a 304 response. tryFetch returns it (rather than
// nil + zero body) so SyncOnce can distinguish "cache hit, skip fallback"
// from "primary failed, try fallback".
var errNotModified = errors.New("not modified")

// tryFetch performs a single GET with conditional-request headers. urlLabel
// is used in log messages to disambiguate the primary vs fallback path.
func (s *Service) tryFetch(ctx context.Context, url, urlLabel string) (body []byte, etag string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}

	// Send our last-seen ETag (if any) so the server can answer 304 when
	// the file is unchanged. We store one ETag per (channel) so primary
	// and fallback can have independent cache state.
	if cached, _ := s.store.GetSyncMeta(ctx, metaPrefix+"etag"); cached != "" {
		req.Header.Set("If-None-Match", cached)
	}
	req.Header.Set("User-Agent", s.userAgent)

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		s.logger.Info("subpricing: not modified", "url", urlLabel)
		return nil, "", errNotModified
	}
	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("%s: HTTP %d", urlLabel, resp.StatusCode)
	}

	body, err = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, "", fmt.Errorf("%s: read body: %w", urlLabel, err)
	}
	return body, resp.Header.Get("ETag"), nil
}

// applyBody parses one token_price_sub.json payload and upserts every
// tier into the DB. Returns the number of rows successfully written.
//
// A schema-validation failure on any row aborts the entire apply — we'd
// rather keep last-known-good prices than half-update the table. The
// guards are deliberately loose ("looks like CNY price between 0 and
// 100,000") because the file is human-edited and we don't want to be
// paranoid; the goal is to catch obvious accidents (negative prices, a
// missing field producing a 0).
func (s *Service) applyBody(ctx context.Context, body []byte) (int, error) {
	type tierJSON struct {
		Provider        string  `json:"provider"`
		TierPattern     string  `json:"tier_pattern"`
		TotalCount      int64   `json:"total_count"`
		Highspeed       bool    `json:"highspeed"`
		MonthlyPriceCNY float64 `json:"monthly_price_cny"`
		WindowHours     int     `json:"window_hours"`
		CNYToUSD        float64 `json:"cny_to_usd"`
		DataSourceURL   string  `json:"data_source_url"`
	}
	var file struct {
		SchemaVersion int        `json:"schema_version"`
		Tiers         []tierJSON `json:"tiers"`
	}
	if err := json.Unmarshal(body, &file); err != nil {
		return 0, fmt.Errorf("parse json: %w", err)
	}
	if file.SchemaVersion != 1 {
		return 0, fmt.Errorf("unsupported schema_version=%d (this build expects 1)", file.SchemaVersion)
	}
	if len(file.Tiers) == 0 {
		return 0, errors.New("no tiers in payload — refusing to wipe existing rows")
	}

	for i, t := range file.Tiers {
		if t.Provider == "" || t.TierPattern == "" {
			return 0, fmt.Errorf("tier %d: provider / tier_pattern required", i)
		}
		if t.TotalCount <= 0 {
			return 0, fmt.Errorf("tier %d (%s/%d): total_count must be positive", i, t.Provider, t.TotalCount)
		}
		if t.MonthlyPriceCNY < 0 || t.MonthlyPriceCNY > 100000 {
			return 0, fmt.Errorf("tier %d (%s/%d): monthly_price_cny %.2f outside sane range",
				i, t.Provider, t.TotalCount, t.MonthlyPriceCNY)
		}
		if t.WindowHours <= 0 || t.WindowHours > 24*30 {
			return 0, fmt.Errorf("tier %d (%s/%d): window_hours %d outside sane range",
				i, t.Provider, t.TotalCount, t.WindowHours)
		}
	}

	now := time.Now().UTC()
	for _, t := range file.Tiers {
		row := storage.SubscriptionPrice{
			Provider:        t.Provider,
			TierPattern:     t.TierPattern,
			TotalCount:      t.TotalCount,
			Highspeed:       t.Highspeed,
			MonthlyPriceCNY: t.MonthlyPriceCNY,
			WindowHours:     t.WindowHours,
			CNYToUSD:        t.CNYToUSD,
			DataSourceURL:   t.DataSourceURL,
			UpdatedAt:       now,
		}
		if err := s.store.UpsertSubscriptionPrice(ctx, row); err != nil {
			return 0, fmt.Errorf("upsert %s/%d: %w", t.Provider, t.TotalCount, err)
		}
	}
	return len(file.Tiers), nil
}
