package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newFPTestServer(t *testing.T) (*Server, *storage.Store) {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	srv := New(s, "v0.test", 8402, 8403)
	srv.SetTokenForTest("test-token")
	return srv, s
}

func fpAuthedReq(t *testing.T, method, path string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Authorization", "Bearer test-token")
	return r
}

func TestFreeProviders_EmptyCatalog(t *testing.T) {
	srv, _ := newFPTestServer(t)

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, fpAuthedReq(t, http.MethodGet, "/internal/free-providers"))

	require.Equal(t, http.StatusOK, w.Code)
	var got []freeProviderJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	assert.Empty(t, got)
}

func TestFreeProviders_ListsCatalogWithoutInheritance(t *testing.T) {
	srv, store := newFPTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertFreeProvider(ctx, storage.FreeProvider{
		ID: "deepseek", DisplayName: "DeepSeek", KrouterProviderName: "deepseek",
		Protocol: "openai", Region: "china", FreeType: "trial_credit",
		FreeSummary: "¥10", FreeQuotaUSD: 1.4, SignupURL: "https://example.com/ds",
		Active: true, UpdatedAt: time.Now().UTC(),
	}))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, fpAuthedReq(t, http.MethodGet, "/internal/free-providers"))

	require.Equal(t, http.StatusOK, w.Code)
	var got []freeProviderJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.Equal(t, "DeepSeek", got[0].DisplayName)
	assert.False(t, got[0].UserConfigured,
		"no inherited row yet → user_configured false")
	assert.Empty(t, got[0].SourceAgent)
	assert.False(t, got[0].Exhausted)
}

func TestFreeProviders_JoinsInheritedState(t *testing.T) {
	srv, store := newFPTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertFreeProvider(ctx, storage.FreeProvider{
		ID: "deepseek", DisplayName: "DeepSeek", KrouterProviderName: "deepseek",
		Protocol: "openai", FreeType: "trial_credit",
		SignupURL: "https://example.com/", Active: true, UpdatedAt: time.Now().UTC(),
	}))
	require.NoError(t, store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, store.ReplaceInheritedEndpoints(ctx, "openclaw", []storage.InheritedEndpoint{
		{Provider: "deepseek", EndpointURL: "u", APIKey: "sk-x", CapturedAt: 1},
	}))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, fpAuthedReq(t, http.MethodGet, "/internal/free-providers"))

	var got []freeProviderJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.True(t, got[0].UserConfigured)
	assert.Equal(t, "openclaw", got[0].SourceAgent)
}

func TestFreeProviders_SurfacesExhaustionFlag(t *testing.T) {
	srv, store := newFPTestServer(t)
	ctx := context.Background()

	require.NoError(t, store.UpsertFreeProvider(ctx, storage.FreeProvider{
		ID: "deepseek", DisplayName: "DeepSeek", KrouterProviderName: "deepseek",
		Protocol: "openai", FreeType: "trial_credit",
		SignupURL: "https://example.com/", Active: true, UpdatedAt: time.Now().UTC(),
	}))
	require.NoError(t, store.MarkProviderExhausted(ctx, "deepseek",
		time.Now().UTC().Add(time.Hour), 402, "test"))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, fpAuthedReq(t, http.MethodGet, "/internal/free-providers"))

	var got []freeProviderJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1)
	assert.True(t, got[0].Exhausted)
}

func TestFreeProviders_OnlyActiveRowsShown(t *testing.T) {
	srv, store := newFPTestServer(t)
	ctx := context.Background()

	// Both seed an active and inactive row.
	require.NoError(t, store.UpsertFreeProvider(ctx, storage.FreeProvider{
		ID: "live", DisplayName: "Live", KrouterProviderName: "live",
		Protocol: "openai", FreeType: "trial_credit",
		SignupURL: "https://example.com/", Active: true, UpdatedAt: time.Now().UTC(),
	}))
	require.NoError(t, store.UpsertFreeProvider(ctx, storage.FreeProvider{
		ID: "dead", DisplayName: "Dead", KrouterProviderName: "dead",
		Protocol: "openai", FreeType: "trial_credit",
		SignupURL: "https://example.com/", Active: false, UpdatedAt: time.Now().UTC(),
	}))

	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, fpAuthedReq(t, http.MethodGet, "/internal/free-providers"))

	var got []freeProviderJSON
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &got))
	require.Len(t, got, 1, "active=false rows must not appear in /internal/free-providers")
	assert.Equal(t, "Live", got[0].DisplayName)
}

func TestFreeProviders_RejectsNonGet(t *testing.T) {
	srv, _ := newFPTestServer(t)
	w := httptest.NewRecorder()
	srv.Handler().ServeHTTP(w, fpAuthedReq(t, http.MethodPost, "/internal/free-providers"))
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
