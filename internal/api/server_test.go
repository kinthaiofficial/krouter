package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/api"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openMemStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// newTestServer creates an api.Server with a known token and returns an
// httptest.Server wrapping its handler.
func newTestServer(t *testing.T, store *storage.Store) (*api.Server, *httptest.Server) {
	t.Helper()
	srv := api.New(store, "test-version", 8402, 8403)
	srv.SetTokenForTest("test-token-123")
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func doGet(t *testing.T, ts *httptest.Server, path string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+path, nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func doPost(t *testing.T, ts *httptest.Server, path string, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ts.URL+path,
		bytes.NewBufferString(body))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func doRequest(t *testing.T, ts *httptest.Server, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, ts.URL+path, nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

// ── Auth middleware ──────────────────────────────────────────────────────────

func TestAuth_MissingToken(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doRequest(t, ts, "/internal/status", "")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuth_WrongToken(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doRequest(t, ts, "/internal/status", "wrong-token")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestAuth_CorrectToken(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doRequest(t, ts, "/internal/status", "test-token-123")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ── /internal/status ─────────────────────────────────────────────────────────

func TestStatus_Fields(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doRequest(t, ts, "/internal/status", "test-token-123")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "ok", body["status"])
	assert.Equal(t, "test-version", body["version"])
	assert.NotNil(t, body["uptime_seconds"])
	assert.NotNil(t, body["pid"])
	assert.Equal(t, float64(8402), body["proxy_port"])
	assert.Equal(t, float64(8403), body["mgmt_port"])
}

// ── /internal/logs ───────────────────────────────────────────────────────────

func TestLogs_EmptyStore(t *testing.T) {
	store := openMemStore(t)
	_, ts := newTestServer(t, store)
	resp := doGet(t, ts, "/internal/logs?n=10")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var rows []any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	assert.Empty(t, rows)
}

func TestLogs_WithRecords(t *testing.T) {
	store := openMemStore(t)
	ctx := context.Background()

	err := store.InsertRequest(ctx, storage.RequestRecord{
		ID:           store.NewULID(),
		Timestamp:    time.Now().UTC(),
		Agent:        "openclaw",
		Protocol:     "anthropic",
		Provider:     "anthropic",
		Model:        "claude-haiku-4-5",
		InputTokens:  42,
		OutputTokens: 10,
		LatencyMS:    300,
		StatusCode:   200,
	})
	require.NoError(t, err)

	_, ts := newTestServer(t, store)
	resp := doGet(t, ts, "/internal/logs?n=5")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var rows []map[string]any
	data, _ := io.ReadAll(resp.Body)
	require.NoError(t, json.Unmarshal(data, &rows))
	require.Len(t, rows, 1)
	assert.Equal(t, "anthropic", rows[0]["provider"])
	assert.Equal(t, "claude-haiku-4-5", rows[0]["model"])
	assert.Equal(t, float64(42), rows[0]["input_tokens"])
}

func TestLogs_NilStore(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/logs")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ── /internal/preset ─────────────────────────────────────────────────────────

func TestPreset_Get_Default(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/preset")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "balanced", body["preset"])
}

func TestPreset_Get_Default_EmptyStore(t *testing.T) {
	store := openMemStore(t)
	_, ts := newTestServer(t, store)
	resp := doGet(t, ts, "/internal/preset")
	defer func() { _ = resp.Body.Close() }()

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "balanced", body["preset"])
}

func TestPreset_Set_Valid(t *testing.T) {
	store := openMemStore(t)
	_, ts := newTestServer(t, store)

	resp := doPost(t, ts, "/internal/preset", `{"preset":"saver"}`)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "saver", body["preset"])
}

func TestPreset_Set_Persisted(t *testing.T) {
	store := openMemStore(t)
	_, ts := newTestServer(t, store)

	resp := doPost(t, ts, "/internal/preset", `{"preset":"quality"}`)
	defer func() { _ = resp.Body.Close() }()
	require.Equal(t, http.StatusOK, resp.StatusCode)

	// GET should now return the persisted value.
	resp2 := doGet(t, ts, "/internal/preset")
	defer func() { _ = resp2.Body.Close() }()
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&body))
	assert.Equal(t, "quality", body["preset"])
}

func TestPreset_Set_Invalid(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doPost(t, ts, "/internal/preset", `{"preset":"turbo"}`)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPreset_Set_MalformedJSON(t *testing.T) {
	_, ts := newTestServer(t, nil)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/internal/preset", strings.NewReader("not-json"))
	req.Header.Set("Authorization", "Bearer test-token-123")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestPreset_MethodNotAllowed(t *testing.T) {
	_, ts := newTestServer(t, nil)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodDelete,
		ts.URL+"/internal/preset", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ── /internal/usage ──────────────────────────────────────────────────────────

func TestUsage_NilStore(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/usage")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(0), body["requests_today"])
	assert.Equal(t, float64(0), body["cost_today_usd"])
}

func TestUsage_WithRequests(t *testing.T) {
	store := openMemStore(t)
	ctx := context.Background()

	// Insert a request from today.
	require.NoError(t, store.InsertRequest(ctx, storage.RequestRecord{
		ID:        store.NewULID(),
		Timestamp: time.Now().UTC(),
		Protocol:  "anthropic",
		Provider:  "anthropic",
		Model:     "claude-haiku-4-5",
	}))

	_, ts := newTestServer(t, store)
	resp := doGet(t, ts, "/internal/usage")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(1), body["requests_today"])
}

// ── /internal/announcements ──────────────────────────────────────────────────

func insertTestAnnouncement(t *testing.T, store *storage.Store, id, priority string) {
	t.Helper()
	ctx := context.Background()
	require.NoError(t, store.InsertAnnouncement(ctx, storage.AnnouncementRecord{
		ID:          id,
		Type:        "provider_news",
		Priority:    priority,
		PublishedAt: time.Now().UTC().Add(-time.Hour),
		TitleJSON:   `{"en":"Hello","zh-CN":"你好"}`,
		SummaryJSON: `{"en":"Body","zh-CN":"内容"}`,
		URL:         "https://example.com",
		Icon:        "🔔",
		ReceivedAt:  time.Now().UTC(),
	}))
}

func TestAnnouncements_EmptyStore(t *testing.T) {
	store := openMemStore(t)
	_, ts := newTestServer(t, store)
	resp := doGet(t, ts, "/internal/announcements")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var rows []any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	assert.Empty(t, rows)
}

func TestAnnouncements_NilStore(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/announcements")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestAnnouncements_ReturnsList(t *testing.T) {
	store := openMemStore(t)
	insertTestAnnouncement(t, store, "ann-001", "normal")
	insertTestAnnouncement(t, store, "ann-002", "critical")

	_, ts := newTestServer(t, store)
	resp := doGet(t, ts, "/internal/announcements")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var rows []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	require.Len(t, rows, 2)

	// Verify title is returned as a map (for client-side localisation).
	title, ok := rows[0]["title"].(map[string]any)
	require.True(t, ok, "title must be an object")
	assert.Equal(t, "Hello", title["en"])
	assert.Equal(t, "你好", title["zh-CN"])
}

func TestAnnouncements_UnreadFirst(t *testing.T) {
	store := openMemStore(t)
	insertTestAnnouncement(t, store, "ann-read", "normal")
	insertTestAnnouncement(t, store, "ann-unread", "normal")

	ctx := context.Background()
	require.NoError(t, store.MarkAnnouncementRead(ctx, "ann-read"))

	_, ts := newTestServer(t, store)
	resp := doGet(t, ts, "/internal/announcements")
	defer func() { _ = resp.Body.Close() }()

	var rows []map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	require.Len(t, rows, 2)
	assert.Equal(t, "ann-unread", rows[0]["id"])
}

func TestAnnouncementsRead_MarksAsRead(t *testing.T) {
	store := openMemStore(t)
	insertTestAnnouncement(t, store, "ann-r", "normal")

	_, ts := newTestServer(t, store)
	resp := doPost(t, ts, "/internal/announcements/read", `{"id":"ann-r"}`)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	ctx := context.Background()
	n, err := store.CountUnreadAnnouncements(ctx)
	require.NoError(t, err)
	assert.Equal(t, 0, n)
}

func TestAnnouncementsRead_MissingID(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doPost(t, ts, "/internal/announcements/read", `{}`)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAnnouncementsRead_MethodNotAllowed(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/announcements/read")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestAnnouncementsDismiss_HidesFromList(t *testing.T) {
	store := openMemStore(t)
	insertTestAnnouncement(t, store, "ann-d", "normal")
	insertTestAnnouncement(t, store, "ann-visible", "normal")

	_, ts := newTestServer(t, store)
	resp := doPost(t, ts, "/internal/announcements/dismiss", `{"id":"ann-d"}`)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)

	resp2 := doGet(t, ts, "/internal/announcements")
	defer func() { _ = resp2.Body.Close() }()

	var rows []map[string]any
	require.NoError(t, json.NewDecoder(resp2.Body).Decode(&rows))
	require.Len(t, rows, 1)
	assert.Equal(t, "ann-visible", rows[0]["id"])
}

func TestAnnouncementsDismiss_MissingID(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doPost(t, ts, "/internal/announcements/dismiss", `{}`)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestAnnouncementsCount_Zero(t *testing.T) {
	store := openMemStore(t)
	_, ts := newTestServer(t, store)
	resp := doGet(t, ts, "/internal/announcements/count")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(0), body["unread"])
}

func TestAnnouncementsCount_WithUnread(t *testing.T) {
	store := openMemStore(t)
	insertTestAnnouncement(t, store, "ann-u1", "normal")
	insertTestAnnouncement(t, store, "ann-u2", "normal")

	ctx := context.Background()
	require.NoError(t, store.MarkAnnouncementRead(ctx, "ann-u1"))

	_, ts := newTestServer(t, store)
	resp := doGet(t, ts, "/internal/announcements/count")
	defer func() { _ = resp.Body.Close() }()

	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(1), body["unread"])
}

func TestAnnouncementsCount_NilStore(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/announcements/count")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, float64(0), body["unread"])
}

// ── /internal/update-status ──────────────────────────────────────────────────

func TestUpdateStatus_NoUpgradeService(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/update-status")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, "test-version", body["current"])
	assert.Nil(t, body["latest"])
}

func TestUpdateStatus_MethodNotAllowed(t *testing.T) {
	_, ts := newTestServer(t, nil)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/internal/update-status", nil)
	req.Header.Set("Authorization", "Bearer test-token-123")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ── /internal/remote/* ───────────────────────────────────────────────────────

func TestRemoteStatus_NoService(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/remote/status")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var body map[string]any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	assert.Equal(t, false, body["enabled"])
}

func TestRemoteDisable_NilService(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doPost(t, ts, "/internal/remote/disable", "")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRemoteDisable_MethodNotAllowed(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/remote/disable")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestDevices_Empty_NilService(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doGet(t, ts, "/internal/devices")
	defer func() { _ = resp.Body.Close() }()

	require.Equal(t, http.StatusOK, resp.StatusCode)
	var rows []any
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&rows))
	assert.Empty(t, rows)
}

func TestDeviceDelete_MethodNotAllowed(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doPost(t, ts, "/internal/devices/dev-001", "")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

func TestPairingExchange_NoService(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doPost(t, ts, "/internal/pairing/exchange", `{"code":"123456","device_name":"test"}`)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
}
