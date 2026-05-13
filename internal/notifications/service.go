// Package notifications manages the notification center.
//
// Three channels (see spec/09-notifications.md):
//   - A: Local runtime events (in-process, not persisted) — planned for later
//   - B: Remote feed (Cloudflare Pages CDN, stored in SQLite)
//   - C: Critical (same source, priority=critical)
//
// Channel B/C: polls https://announcements.kinthai.ai/feed.json every 6h.
// Uses ETag (If-None-Match) so 99% of polls return 304 Not Modified.
package notifications

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

const defaultFeedURL = "https://announcements.kinthai.ai/feed.json"

// Service manages remote feed polling and announcement storage.
type Service struct {
	store      *storage.Store
	settings   *config.Manager
	registry   *providers.Registry
	version    string
	feedURL    string
	httpClient *http.Client
	logger     *slog.Logger
}

// New creates a Service with the default CDN feed URL.
func New(store *storage.Store, settings *config.Manager, registry *providers.Registry, version string) *Service {
	return NewWithFeedURL(store, settings, registry, version, defaultFeedURL)
}

// NewWithFeedURL creates a Service with a configurable feed URL (for testing).
func NewWithFeedURL(store *storage.Store, settings *config.Manager, registry *providers.Registry, version, feedURL string) *Service {
	return &Service{
		store:      store,
		settings:   settings,
		registry:   registry,
		version:    version,
		feedURL:    feedURL,
		httpClient: &http.Client{Timeout: 15 * time.Second},
		logger:     slog.Default(),
	}
}

// Start polls the feed immediately, then every 6h until ctx is cancelled.
func (s *Service) Start(ctx context.Context) error {
	// Initial poll on startup.
	s.pollOnce(ctx)

	ticker := time.NewTicker(6 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			s.pollOnce(ctx)
		}
	}
}

// PollOnceForTest triggers a single poll synchronously. For use in tests only.
func (s *Service) PollOnceForTest(ctx context.Context) {
	s.pollOnce(ctx)
}

// pollOnce fetches the feed and processes new announcements.
func (s *Service) pollOnce(ctx context.Context) {
	s.logger.Info("notifications: polling feed")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.feedURL, nil)
	if err != nil {
		s.logger.Warn("notifications: failed to build request", "err", err)
		return
	}

	ua := "krouter/" + s.version + " (" + runtime.GOOS + "-" + runtime.GOARCH + ")"
	req.Header.Set("User-Agent", ua)

	// ETag caching — avoids re-downloading unchanged feeds.
	if s.store != nil {
		if etag, _ := s.store.GetFeedMeta(ctx, "last_etag"); etag != "" {
			req.Header.Set("If-None-Match", etag)
		}
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		s.logger.Warn("notifications: feed request failed", "err", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotModified {
		s.logger.Debug("notifications: feed unchanged (304)")
		if s.store != nil {
			_ = s.store.SetFeedMeta(ctx, "last_polled_at", time.Now().UTC().Format(time.RFC3339))
		}
		return
	}
	if resp.StatusCode != http.StatusOK {
		s.logger.Warn("notifications: unexpected feed status", "code", resp.StatusCode)
		return
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024)) // 5MB max
	if err != nil {
		s.logger.Warn("notifications: failed to read feed body", "err", err)
		return
	}

	var feed feedResponse
	if err := json.Unmarshal(body, &feed); err != nil {
		s.logger.Warn("notifications: failed to parse feed JSON", "err", err)
		return
	}

	s.processAnnouncements(ctx, feed.Announcements)

	if s.store != nil {
		now := time.Now().UTC().Format(time.RFC3339)
		_ = s.store.SetFeedMeta(ctx, "last_polled_at", now)
		if feed.UpdatedAt != "" {
			_ = s.store.SetFeedMeta(ctx, "last_feed_updated_at", feed.UpdatedAt)
		}
		if etag := resp.Header.Get("ETag"); etag != "" {
			_ = s.store.SetFeedMeta(ctx, "last_etag", etag)
		}
	}

	s.logger.Info("notifications: poll complete", "count", len(feed.Announcements))
}

// processAnnouncements filters and stores new announcements.
func (s *Service) processAnnouncements(ctx context.Context, items []feedAnnouncement) {
	for _, item := range items {
		if s.store != nil {
			exists, err := s.store.AnnouncementExists(ctx, item.ID)
			if err != nil || exists {
				continue
			}
		}

		if !s.matchesTargets(item.Targets) {
			continue
		}

		rec := storage.AnnouncementRecord{
			ID:          item.ID,
			Type:        item.Type,
			Priority:    item.Priority,
			PublishedAt: item.PublishedAt,
			ExpiresAt:   item.ExpiresAt,
			TitleJSON:   marshalJSON(item.Title),
			SummaryJSON: marshalJSON(item.Summary),
			URL:         item.URL,
			Icon:        item.Icon,
			ReceivedAt:  time.Now().UTC(),
		}

		if s.store != nil {
			if err := s.store.InsertAnnouncement(ctx, rec); err != nil {
				s.logger.Warn("notifications: failed to store announcement", "id", item.ID, "err", err)
			}
		}
	}
}

// matchesTargets applies local filtering rules (spec/09 §2.5).
func (s *Service) matchesTargets(t feedTargets) bool {
	// Platform filter.
	if len(t.Platform) > 0 && !containsStr(t.Platform, runtime.GOOS) {
		return false
	}

	// Language filter.
	if len(t.Language) > 0 && s.settings != nil {
		lang := s.settings.Get().Language
		if lang == "" {
			lang = "en"
		}
		if !containsStr(t.Language, lang) {
			return false
		}
	}

	// Provider-missing filter.
	for _, providerName := range t.OnlyShowIfProviderMissing {
		envKey := providerEnvKey(providerName)
		if envKey != "" && os.Getenv(envKey) != "" {
			return false // user already has this provider configured
		}
	}

	return true
}

// providerEnvKey maps a provider name to its API key environment variable.
func providerEnvKey(name string) string {
	m := map[string]string{
		"anthropic": "ANTHROPIC_API_KEY",
		"openai":    "OPENAI_API_KEY",
		"deepseek":  "DEEPSEEK_API_KEY",
		"groq":      "GROQ_API_KEY",
	}
	return m[name]
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func marshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// --- feed JSON schema ---

type feedResponse struct {
	UpdatedAt     string             `json:"updated_at"`
	Announcements []feedAnnouncement `json:"announcements"`
}

type feedAnnouncement struct {
	ID          string            `json:"id"`
	Type        string            `json:"type"`
	Priority    string            `json:"priority"`
	PublishedAt time.Time         `json:"published_at"`
	ExpiresAt   *time.Time        `json:"expires_at"`
	Title       map[string]string `json:"title"`
	Summary     map[string]string `json:"summary"`
	URL         string            `json:"url"`
	Icon        string            `json:"icon"`
	Targets     feedTargets       `json:"targets"`
}

type feedTargets struct {
	Platform                  []string `json:"platform"`
	Language                  []string `json:"language"`
	MinVersion                string   `json:"min_version"`
	OnlyShowIfProviderMissing []string `json:"only_show_if_provider_missing"`
}

// Ensure providers.Registry is used to satisfy import (registry used in future health check).
var _ = (*providers.Registry)(nil)
