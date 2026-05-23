// Package freeproviders implements the daemon's background sync of the
// free-credit provider catalog from data/free_tokens.json on krouter
// infrastructure.
//
// Mirrors the subpricing package's design (spec/05 §11.4): kinthai.ai
// hosts the canonical file so operators have access logs (daemon fleet
// version distribution, daily unique IPs, 304 cache-hit ratio, etc.).
// GitHub raw serves the same file as a resilience fallback.
//
// Why this matters: when a vendor revises free-credit policy (DeepSeek
// stops the ¥10 trial, Groq raises the daily req limit, …) we want
// existing daemons to learn within a day rather than wait for the next
// binary release. The same data/free_tokens.json that ships embedded in
// the installer is the file served by these URLs, so edits propagate
// through both channels from a single commit.
//
// Distribution channels (in fetch order):
//
//  1. Primary  — https://krouter.kinthai.ai/data/free_tokens.json
//                Operated by us; access logs feed GoAccess.
//
//  2. Fallback — https://raw.githubusercontent.com/kinthaiofficial/krouter/main/data/free_tokens.json
//                Used only on primary failure.
//
// Daemon sends `User-Agent: krouter-freeproviders-sync/<version>` so
// access logs can break down by deployed daemon version without any
// in-product telemetry.
package freeproviders

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
	PrimaryURL  = "https://krouter.kinthai.ai/data/free_tokens.json"
	FallbackURL = "https://raw.githubusercontent.com/kinthaiofficial/krouter/main/data/free_tokens.json"

	// metaPrefix scopes our keys in token_price_api_meta (the generic
	// sync-meta KV table) so we don't collide with subpricing's keys.
	metaPrefix = "free_providers_"

	maxBodyBytes = 1 << 20 // 1 MB — file is ~5 KB today
)

// Service runs the free-provider catalog sync loop. Construct via New,
// optionally call WithHTTPClient / WithVersion, then StartSync.
type Service struct {
	store      *storage.Store
	logger     logging.Logger
	httpClient *http.Client
	userAgent  string

	// onUpdate fires whenever SyncOnce successfully writes new rows.
	onUpdate func(updatedCount int)
}

// New creates a Service. Default HTTP client has a 15-second timeout;
// production callers should follow up with WithHTTPClient to inject a
// proxy-aware client. Default User-Agent is "krouter-freeproviders-sync/dev";
// serve.go passes the daemon Version through WithVersion so the kinthai.ai
// access log can break down fleet version distribution.
func New(store *storage.Store, logger logging.Logger) *Service {
	return &Service{
		store:      store,
		logger:     logger,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		userAgent:  "krouter-freeproviders-sync/dev",
	}
}

func (s *Service) WithHTTPClient(c *http.Client) *Service {
	if c != nil {
		s.httpClient = c
	}
	return s
}

func (s *Service) WithVersion(v string) *Service {
	if v != "" {
		s.userAgent = "krouter-freeproviders-sync/" + v
	}
	return s
}

func (s *Service) WithUpdateCallback(cb func(updatedCount int)) *Service {
	s.onUpdate = cb
	return s
}

// StartSync runs SyncOnce on a fixed interval until ctx is cancelled.
// A 45-second delay precedes the first sync so daemon startup is not
// blocked (offset from subpricing's 30s so the two sync loops don't fire
// at exactly the same time and double the burst of network activity).
func (s *Service) StartSync(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	timer := time.NewTimer(45 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if err := s.SyncOnce(ctx); err != nil {
				s.logger.Warn("freeproviders: sync failed", "err", err)
			}
			timer.Reset(interval)
		}
	}
}

// SyncOnce performs one fetch + parse + upsert cycle. Same contract as
// subpricing's: primary → fallback on error, 304 short-circuits.
func (s *Service) SyncOnce(ctx context.Context) error {
	body, etag, err := s.tryFetch(ctx, PrimaryURL, "primary")
	if err != nil {
		if errors.Is(err, errNotModified) {
			return nil
		}
		s.logger.Warn("freeproviders: primary failed, trying fallback",
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

	if etag != "" {
		_ = s.store.SetSyncMeta(ctx, metaPrefix+"etag", etag)
	}
	_ = s.store.SetSyncMeta(ctx, metaPrefix+"last_synced_at",
		time.Now().UTC().Format(time.RFC3339))

	s.logger.Info("freeproviders: synced", "rows", count)
	if s.onUpdate != nil && count > 0 {
		s.onUpdate(count)
	}
	return nil
}

var errNotModified = errors.New("not modified")

func (s *Service) tryFetch(ctx context.Context, url, urlLabel string) (body []byte, etag string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
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
		s.logger.Info("freeproviders: not modified", "url", urlLabel)
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

// providerJSON matches data/free_tokens.json schema. Loose fields tolerate
// missing values; required fields are validated in applyBody.
type providerJSON struct {
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
	Active              bool    `json:"active"`
	LastVerified        string  `json:"last_verified"`
	Notes               string  `json:"notes"`
}

type catalogFile struct {
	SchemaVersion int            `json:"schema_version"`
	LastCurated   string         `json:"last_curated"`
	Disclaimer    string         `json:"disclaimer"`
	Providers     []providerJSON `json:"providers"`
}

// ApplyEmbedded is called once at daemon startup to seed free_provider_state
// from the binary's embedded JSON. Idempotent — re-running just upserts
// the same rows. Returns the count of rows applied so serve.go can log it.
func (s *Service) ApplyEmbedded(ctx context.Context, embedded []byte) (int, error) {
	return s.applyBody(ctx, embedded)
}

// applyBody parses a free_tokens.json payload and upserts every provider
// row. Schema validation guards against accidental commits that would
// wipe the catalog (empty list, missing required fields).
func (s *Service) applyBody(ctx context.Context, body []byte) (int, error) {
	var file catalogFile
	if err := json.Unmarshal(body, &file); err != nil {
		return 0, fmt.Errorf("parse json: %w", err)
	}
	if file.SchemaVersion != 1 {
		return 0, fmt.Errorf("unsupported schema_version=%d (this build expects 1)", file.SchemaVersion)
	}
	if len(file.Providers) == 0 {
		return 0, errors.New("no providers in payload — refusing to wipe existing rows")
	}

	// Validate every row before any DB write so a single bad entry doesn't
	// produce a half-applied catalog.
	for i, p := range file.Providers {
		if p.ID == "" {
			return 0, fmt.Errorf("provider %d: id required", i)
		}
		if p.DisplayName == "" {
			return 0, fmt.Errorf("provider %s: display_name required", p.ID)
		}
		if p.KrouterProviderName == "" {
			return 0, fmt.Errorf("provider %s: krouter_provider_name required", p.ID)
		}
		if p.SignupURL == "" {
			return 0, fmt.Errorf("provider %s: signup_url required (UI's primary CTA)", p.ID)
		}
		if p.FreeType != "trial_credit" && p.FreeType != "daily_quota" && p.FreeType != "free_tier" {
			return 0, fmt.Errorf("provider %s: free_type %q must be trial_credit | daily_quota | free_tier",
				p.ID, p.FreeType)
		}
	}

	now := time.Now().UTC()
	for _, p := range file.Providers {
		row := storage.FreeProvider{
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
			Active:              p.Active,
			LastVerified:        p.LastVerified,
			Notes:               p.Notes,
			UpdatedAt:           now,
		}
		if err := s.store.UpsertFreeProvider(ctx, row); err != nil {
			return 0, fmt.Errorf("upsert %s: %w", p.ID, err)
		}
	}
	return len(file.Providers), nil
}
