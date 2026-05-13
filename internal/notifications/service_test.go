package notifications_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/notifications"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// openStore opens an in-memory SQLite store with all migrations applied.
func openStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// settingsWithLang writes a settings file with the given language and returns a Manager.
func settingsWithLang(t *testing.T, lang string) *config.Manager {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")
	data, _ := json.Marshal(map[string]string{"language": lang})
	require.NoError(t, os.WriteFile(path, data, 0600))
	return config.New(path)
}

// feedJSON builds a minimal feed JSON payload with the given announcements.
func feedJSON(t *testing.T, items []map[string]any) []byte {
	t.Helper()
	payload := map[string]any{
		"updated_at":    time.Now().UTC().Format(time.RFC3339),
		"announcements": items,
	}
	b, err := json.Marshal(payload)
	require.NoError(t, err)
	return b
}

// announcement builds a feed announcement map with sensible defaults.
func announcement(id string, extra map[string]any) map[string]any {
	a := map[string]any{
		"id":           id,
		"type":         "provider_news",
		"priority":     "normal",
		"published_at": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339),
		"title":        map[string]string{"en": "Test"},
		"summary":      map[string]string{"en": "Body"},
		"url":          "https://example.com",
		"icon":         "🔔",
		"targets":      map[string]any{},
	}
	for k, v := range extra {
		a[k] = v
	}
	return a
}

func TestPollOnce_StoresNewAnnouncement(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body := feedJSON(t, []map[string]any{announcement("ann-001", nil)})
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, nil, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx)

	exists, err := store.AnnouncementExists(ctx, "ann-001")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPollOnce_SetsETagOnFirstPoll(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, nil))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, nil, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx)

	etag, err := store.GetFeedMeta(ctx, "last_etag")
	require.NoError(t, err)
	assert.Equal(t, `"v1"`, etag)
}

func TestPollOnce_SendsIfNoneMatchOnSubsequentPoll(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	receivedHeaders := make([]string, 0, 2)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedHeaders = append(receivedHeaders, r.Header.Get("If-None-Match"))
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, nil))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, nil, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx) // first poll — no ETag stored yet
	svc.PollOnceForTest(ctx) // second poll — should send If-None-Match

	require.Len(t, receivedHeaders, 2)
	assert.Empty(t, receivedHeaders[0], "first poll should not send If-None-Match")
	assert.Equal(t, `"v1"`, receivedHeaders[1], "second poll should send stored ETag")
}

func TestPollOnce_Handles304(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if r.Header.Get("If-None-Match") != "" {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		w.Header().Set("ETag", `"v1"`)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, []map[string]any{announcement("ann-304", nil)}))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, nil, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx) // 200 — stores ann-304
	svc.PollOnceForTest(ctx) // 304 — should not re-process

	assert.Equal(t, 2, callCount)

	// last_polled_at must be set even on 304.
	polled, err := store.GetFeedMeta(ctx, "last_polled_at")
	require.NoError(t, err)
	assert.NotEmpty(t, polled)
}

func TestPollOnce_DeduplicatesExistingAnnouncement(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, []map[string]any{announcement("ann-dup", nil)}))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, nil, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx)
	svc.PollOnceForTest(ctx)

	// ListAnnouncements must return exactly one record.
	recs, err := store.ListAnnouncements(ctx, 10)
	require.NoError(t, err)
	assert.Len(t, recs, 1)
}

func TestPollOnce_PlatformFilter_SkipsMismatch(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	// "plan9" never matches the running OS.
	ann := announcement("ann-platform", map[string]any{
		"targets": map[string]any{"platform": []string{"plan9"}},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, []map[string]any{ann}))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, nil, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx)

	exists, err := store.AnnouncementExists(ctx, "ann-platform")
	require.NoError(t, err)
	assert.False(t, exists, "platform-mismatched announcement should be skipped")
}

func TestPollOnce_PlatformFilter_PassesCurrentOS(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	// Empty platform list means "all platforms".
	ann := announcement("ann-allplatform", map[string]any{
		"targets": map[string]any{"platform": []string{}},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, []map[string]any{ann}))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, nil, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx)

	exists, err := store.AnnouncementExists(ctx, "ann-allplatform")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPollOnce_LanguageFilter_SkipsMismatch(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	settings := settingsWithLang(t, "en")

	ann := announcement("ann-lang", map[string]any{
		"targets": map[string]any{"language": []string{"zh-CN"}},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, []map[string]any{ann}))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, settings, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx)

	exists, err := store.AnnouncementExists(ctx, "ann-lang")
	require.NoError(t, err)
	assert.False(t, exists, "zh-CN-only announcement should be skipped for en user")
}

func TestPollOnce_LanguageFilter_PassesMatch(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	settings := settingsWithLang(t, "zh-CN")

	ann := announcement("ann-zh", map[string]any{
		"targets": map[string]any{"language": []string{"zh-CN"}},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, []map[string]any{ann}))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, settings, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx)

	exists, err := store.AnnouncementExists(ctx, "ann-zh")
	require.NoError(t, err)
	assert.True(t, exists)
}

func TestPollOnce_ProviderMissingFilter_SkipsWhenPresent(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key")

	ann := announcement("ann-provider", map[string]any{
		"targets": map[string]any{"only_show_if_provider_missing": []string{"anthropic"}},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, []map[string]any{ann}))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, nil, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx)

	exists, err := store.AnnouncementExists(ctx, "ann-provider")
	require.NoError(t, err)
	assert.False(t, exists, "announcement should be skipped when the required provider is already configured")
}

func TestPollOnce_ProviderMissingFilter_ShowsWhenAbsent(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	// Ensure env var is not set.
	t.Setenv("ANTHROPIC_API_KEY", "")

	ann := announcement("ann-missing", map[string]any{
		"targets": map[string]any{"only_show_if_provider_missing": []string{"anthropic"}},
	})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, []map[string]any{ann}))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, nil, nil, "0.0.1", srv.URL)
	svc.PollOnceForTest(ctx)

	exists, err := store.AnnouncementExists(ctx, "ann-missing")
	require.NoError(t, err)
	assert.True(t, exists, "announcement should appear when the required provider is not configured")
}

func TestPollOnce_SetsUserAgentHeader(t *testing.T) {
	store := openStore(t)
	ctx := context.Background()

	var gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, nil))
	}))
	defer srv.Close()

	svc := notifications.NewWithFeedURL(store, nil, nil, "1.2.3", srv.URL)
	svc.PollOnceForTest(ctx)

	assert.Contains(t, gotUA, "krouter/1.2.3")
}

func TestNew_NilStoreDoesNotPanic(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(feedJSON(t, []map[string]any{announcement("ann-nil", nil)}))
	}))
	defer srv.Close()

	// nil store must not panic — service just skips persistence.
	svc := notifications.NewWithFeedURL(nil, nil, nil, "0.0.1", srv.URL)
	assert.NotPanics(t, func() { svc.PollOnceForTest(ctx) })
}
