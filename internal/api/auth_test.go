package api_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// cookieJar implements http.CookieJar for tests.
type cookieJar struct {
	mu      sync.Mutex
	cookies map[string][]*http.Cookie // host → cookies
}

func newCookieJar() *cookieJar {
	return &cookieJar{cookies: make(map[string][]*http.Cookie)}
}

func (j *cookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	j.mu.Lock()
	j.cookies[u.Host] = append(j.cookies[u.Host], cookies...)
	j.mu.Unlock()
}

func (j *cookieJar) Cookies(u *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.cookies[u.Host]
}

// mintTicket calls POST /internal/auth/ticket with Bearer auth.
func mintTicketReq(t *testing.T, ts *httptest.Server, token string) (string, int) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		ts.URL+"/internal/auth/ticket", nil)
	require.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", resp.StatusCode
	}
	var body map[string]string
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	return body["ticket"], resp.StatusCode
}

func mustMintTicket(t *testing.T, ts *httptest.Server) string {
	t.Helper()
	ticket, code := mintTicketReq(t, ts, "test-token-123")
	require.Equal(t, http.StatusOK, code)
	require.NotEmpty(t, ticket)
	return ticket
}

// ── Ticket minting ────────────────────────────────────────────────────────────

func TestMintTicket_RequiresBearer(t *testing.T) {
	_, ts := newTestServer(t, nil)
	_, code := mintTicketReq(t, ts, "")
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestMintTicket_WrongBearer(t *testing.T) {
	_, ts := newTestServer(t, nil)
	_, code := mintTicketReq(t, ts, "wrong-token")
	assert.Equal(t, http.StatusUnauthorized, code)
}

func TestMintTicket_ReturnsHexToken(t *testing.T) {
	_, ts := newTestServer(t, nil)
	ticket := mustMintTicket(t, ts)
	// 32 bytes → 64 hex chars
	assert.Len(t, ticket, 64)
	for _, c := range ticket {
		assert.True(t, (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f'),
			"ticket must be lowercase hex, got char %q", c)
	}
}

func TestMintTicket_MethodNotAllowed(t *testing.T) {
	_, ts := newTestServer(t, nil)
	// GET /internal/auth/ticket — should be 405 (not 401, since no auth needed for 405).
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		ts.URL+"/internal/auth/ticket", nil)
	require.NoError(t, err)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusMethodNotAllowed, resp.StatusCode)
}

// ── Ticket exchange ───────────────────────────────────────────────────────────

func TestExchangeTicket_ValidOnce(t *testing.T) {
	_, ts := newTestServer(t, nil)
	ticket := mustMintTicket(t, ts)

	jar := newCookieJar()
	noRedirect := &http.Client{
		Jar: jar,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := noRedirect.Get(fmt.Sprintf("%s/internal/auth/exchange?ticket=%s&redirect=/ui/", ts.URL, ticket))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusFound, resp.StatusCode)

	// Cookie must be set.
	parsed, _ := url.Parse(ts.URL)
	cookies := jar.Cookies(parsed)
	found := false
	for _, c := range cookies {
		if c.Name == "krouter_session" {
			found = true
			assert.NotEmpty(t, c.Value)
		}
	}
	assert.True(t, found, "krouter_session cookie not set")
}

func TestExchangeTicket_ReplayFails(t *testing.T) {
	_, ts := newTestServer(t, nil)
	ticket := mustMintTicket(t, ts)

	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}

	resp1, err := noRedirect.Get(fmt.Sprintf("%s/internal/auth/exchange?ticket=%s", ts.URL, ticket))
	require.NoError(t, err)
	_ = resp1.Body.Close()
	assert.Equal(t, http.StatusFound, resp1.StatusCode)

	resp2, err := noRedirect.Get(fmt.Sprintf("%s/internal/auth/exchange?ticket=%s", ts.URL, ticket))
	require.NoError(t, err)
	_ = resp2.Body.Close()
	assert.Equal(t, http.StatusUnauthorized, resp2.StatusCode)
}

func TestExchangeTicket_MissingTicket(t *testing.T) {
	_, ts := newTestServer(t, nil)
	resp, err := http.Get(ts.URL + "/internal/auth/exchange")
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// ── Session cookie auth ───────────────────────────────────────────────────────

func TestSessionCookie_ValidRequest(t *testing.T) {
	_, ts := newTestServer(t, nil)
	ticket := mustMintTicket(t, ts)

	jar := newCookieJar()
	client := &http.Client{Jar: jar}
	// Exchange ticket; redirect to /internal/status which requires session cookie.
	resp, err := client.Get(fmt.Sprintf("%s/internal/auth/exchange?ticket=%s&redirect=/internal/status", ts.URL, ticket))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSessionCookie_InvalidCookieFails(t *testing.T) {
	_, ts := newTestServer(t, nil)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet,
		ts.URL+"/internal/status", nil)
	require.NoError(t, err)
	req.AddCookie(&http.Cookie{Name: "krouter_session", Value: "invalid-session-id"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestBearerStillAccepted_WithNewAuth(t *testing.T) {
	_, ts := newTestServer(t, nil)
	// Bearer token path must continue to work.
	resp := doGet(t, ts, "/internal/status")
	defer func() { _ = resp.Body.Close() }()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

// ── Concurrent ticket exchange (replay prevention) ────────────────────────────

func TestConcurrentTicketExchange_OnlyOneSucceeds(t *testing.T) {
	_, ts := newTestServer(t, nil)
	ticket := mustMintTicket(t, ts)

	const n = 50
	results := make([]int, n)
	var wg sync.WaitGroup
	wg.Add(n)
	noRedirect := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			resp, err := noRedirect.Get(
				fmt.Sprintf("%s/internal/auth/exchange?ticket=%s", ts.URL, ticket))
			if err == nil {
				results[i] = resp.StatusCode
				_ = resp.Body.Close()
			}
		}()
	}
	wg.Wait()

	successes := 0
	for _, code := range results {
		if code == http.StatusFound {
			successes++
		}
	}
	assert.Equal(t, 1, successes, "exactly one goroutine should exchange the ticket successfully")
}
