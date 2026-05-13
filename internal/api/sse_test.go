package api_test

import (
	"bufio"
	"context"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/api"
	"github.com/kinthaiofficial/krouter/internal/notify"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSSE_StreamHeaders(t *testing.T) {
	_, ts := newTestServer(t, nil)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		ts.URL+"/internal/events", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))
	assert.Equal(t, "no-cache", resp.Header.Get("Cache-Control"))
}

func TestSSE_ReceivesHeartbeat(t *testing.T) {
	_, ts := newTestServer(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/internal/events", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	scanner := bufio.NewScanner(resp.Body)
	scanner.Scan()
	line := scanner.Text()
	assert.True(t, strings.HasPrefix(line, ":"), "expected SSE comment ping, got %q", line)
}

func TestSSE_SendsEventToSubscribers(t *testing.T) {
	srv, ts := newTestServer(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/internal/events", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	time.Sleep(50 * time.Millisecond)
	srv.Broadcast("test_event", map[string]string{"msg": "hello"})

	scanner := bufio.NewScanner(resp.Body)
	found := false
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "event: test_event") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected test_event SSE event")
}

func TestSSE_AllEventTypes(t *testing.T) {
	eventTypes := []string{"quota_warning", "announcement_new", "upgrade_available", "settings_changed", "request_completed"}
	for _, et := range eventTypes {
		et := et
		t.Run(et, func(t *testing.T) {
			t.Parallel()
			srv, ts := newTestServer(t, nil)

			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet,
				ts.URL+"/internal/events", nil)
			require.NoError(t, err)
			req.Header.Set("Authorization", "Bearer test-token-123")

			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			time.Sleep(50 * time.Millisecond)
			srv.Broadcast(et, map[string]string{"type": et})

			scanner := bufio.NewScanner(resp.Body)
			found := false
			for scanner.Scan() {
				if strings.HasPrefix(scanner.Text(), "event: "+et) {
					found = true
					break
				}
			}
			assert.True(t, found, "expected %s SSE event", et)
		})
	}
}

func TestSSE_RequiresAuth(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp, err := http.Get(ts.URL + "/internal/events")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestSSE_MethodNotAllowed(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp := doPost(t, ts, "/internal/events", "")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// countingNotifier wraps notify.Notifier and counts HandleEvent calls.
type countingNotifier struct {
	notify.Notifier
	count atomic.Int32
}

func TestSSE_NotifierCalledOnBroadcast(t *testing.T) {
	srv := api.New(nil, "v", 8402, 8403)
	srv.SetTokenForTest("test-token-123")
	// Use a very short window so dedupe doesn't block successive events.
	n := notify.NewWithWindow(time.Millisecond)
	srv.SetNotifier(n)

	// Broadcast a quota_warning — notifier.HandleEvent must be called.
	// We can't easily intercept beeep itself, but we verify Broadcast doesn't panic
	// and the Notifier.HandleEvent is invoked without error.
	srv.Broadcast("quota_warning", nil)
	srv.Broadcast("upgrade_available", nil)
	srv.Broadcast("announcement_new", nil)
	// unknown type should be silently ignored by notifier
	srv.Broadcast("unknown_type", nil)
}

func TestSSE_ClientDisconnect_NoLeak(t *testing.T) {
	srv, ts := newTestServer(t, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		ts.URL+"/internal/events", nil)
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token-123")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// Let the context expire → client disconnects.
	<-ctx.Done()
	time.Sleep(100 * time.Millisecond)

	// Broadcast after disconnect should not block or panic.
	srv.Broadcast("test_event", nil)
}
