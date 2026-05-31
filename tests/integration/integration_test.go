//go:build integration

// Package integration contains end-to-end tests that require a real management
// API server backed by an in-memory SQLite store.
//
// Run with: go test -race -tags=integration ./tests/integration/...
package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/api"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

// startServer opens an in-memory store, creates an api.Server backed by a real
// TCP listener, and returns the base URL. It uses Handler() directly so the
// pre-set test token is never overwritten by Serve()'s token generation.
func startServer(t *testing.T) (baseURL string, token string) {
	t.Helper()

	store, err := storage.Open(":memory:")
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if err := store.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	const testToken = "integration-test-token"
	srv := api.New(store, "integration-test", 8402, port)
	srv.SetTokenForTest(testToken)

	httpSrv := &http.Server{Handler: srv.Handler()}
	t.Cleanup(func() {
		_ = httpSrv.Close()
		_ = store.Close()
	})
	go func() { _ = httpSrv.Serve(ln) }()

	return fmt.Sprintf("http://127.0.0.1:%d", port), testToken
}

func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("free port: %v", err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

func get(t *testing.T, baseURL, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func post(t *testing.T, baseURL, path, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost,
		baseURL+path, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

// getWithOrigin issues a GET carrying an explicit Origin header (and optional
// Bearer token), mimicking a browser request. The management API's CSRF guard
// keys off Origin, so this is how we exercise the cross-origin code paths.
func getWithOrigin(t *testing.T, baseURL, path, token, origin string) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, baseURL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if origin != "" {
		req.Header.Set("Origin", origin)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func readJSON(t *testing.T, r io.Reader) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.NewDecoder(r).Decode(&m); err != nil {
		t.Fatalf("decode JSON: %v", err)
	}
	return m
}

// ── Tests ─────────────────────────────────────────────────────────────────────

func TestIntegration_Status(t *testing.T) {
	baseURL, token := startServer(t)

	resp := get(t, baseURL, "/internal/status", token)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readJSON(t, resp.Body)
	if body["version"] != "integration-test" {
		t.Errorf("version = %v, want integration-test", body["version"])
	}
}

// dashboardOrigin mirrors the allowedOrigin const in internal/api/auth.go: the
// single browser origin the management API's CSRF guard permits. It is a fixed
// value, independent of the actual management port the daemon listens on.
const dashboardOrigin = "http://127.0.0.1:8403"

// TestIntegration_CrossOriginRejected exercises the CSRF guard. The management
// API replaced its old token-required scheme with an Origin check (see
// internal/api/auth.go): a browser request from a foreign Origin is rejected
// with 403, while the dashboard's own Origin is allowed without any token.
func TestIntegration_CrossOriginRejected(t *testing.T) {
	baseURL, _ := startServer(t)

	// Foreign Origin → blocked, even on a plain readable GET.
	resp := getWithOrigin(t, baseURL, "/internal/status", "", "https://evil.example")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("foreign-origin status = %d, want 403", resp.StatusCode)
	}

	// The dashboard's own Origin → allowed without a token.
	resp2 := getWithOrigin(t, baseURL, "/internal/status", "", dashboardOrigin)
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("same-origin status = %d, want 200", resp2.StatusCode)
	}
}

// TestIntegration_BearerTokenBypassesOrigin verifies auth.go's decision order: a
// valid Bearer token authorizes a request unconditionally (even from a foreign
// Origin), and a no-Origin request (curl / CLI) is allowed without any token.
func TestIntegration_BearerTokenBypassesOrigin(t *testing.T) {
	baseURL, token := startServer(t)

	// Valid token + foreign Origin → allowed (token overrides the CSRF guard).
	resp := getWithOrigin(t, baseURL, "/internal/status", token, "https://evil.example")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("token+foreign-origin status = %d, want 200", resp.StatusCode)
	}

	// No Origin, no token (curl / CLI) → allowed.
	resp2 := get(t, baseURL, "/internal/status", "")
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Errorf("no-origin no-token status = %d, want 200", resp2.StatusCode)
	}
}

func TestIntegration_PresetGetSet(t *testing.T) {
	baseURL, token := startServer(t)

	// Default preset.
	resp := get(t, baseURL, "/internal/preset", token)
	body := readJSON(t, resp.Body)
	resp.Body.Close()
	if body["preset"] != "balanced" {
		t.Errorf("default preset = %v, want balanced", body["preset"])
	}

	// Change preset.
	resp2 := post(t, baseURL, "/internal/preset", token, `{"preset":"saver"}`)
	resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("set preset status = %d, want 200", resp2.StatusCode)
	}

	// Confirm change persisted.
	resp3 := get(t, baseURL, "/internal/preset", token)
	body3 := readJSON(t, resp3.Body)
	resp3.Body.Close()
	if body3["preset"] != "saver" {
		t.Errorf("updated preset = %v, want saver", body3["preset"])
	}
}

func TestIntegration_InvalidPreset(t *testing.T) {
	baseURL, token := startServer(t)

	resp := post(t, baseURL, "/internal/preset", token, `{"preset":"invalid"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", resp.StatusCode)
	}
}

func TestIntegration_UsageEmpty(t *testing.T) {
	baseURL, token := startServer(t)

	resp := get(t, baseURL, "/internal/usage", token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := readJSON(t, resp.Body)
	if body["requests_today"] != float64(0) {
		t.Errorf("requests_today = %v, want 0", body["requests_today"])
	}
}

func TestIntegration_LogsEmpty(t *testing.T) {
	baseURL, token := startServer(t)

	resp := get(t, baseURL, "/internal/logs?n=10", token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var items []any
	if err := json.NewDecoder(resp.Body).Decode(&items); err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Errorf("logs = %d items, want 0", len(items))
	}
}

func TestIntegration_AnnouncementsEmpty(t *testing.T) {
	baseURL, token := startServer(t)

	resp := get(t, baseURL, "/internal/announcements", token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}
