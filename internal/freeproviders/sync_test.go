package freeproviders

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validPayload = `{
  "schema_version": 1,
  "last_curated": "2026-05-23",
  "providers": [
    {
      "id": "deepseek",
      "display_name": "DeepSeek",
      "krouter_provider_name": "deepseek",
      "protocol": "openai",
      "region": "china",
      "free_type": "trial_credit",
      "free_summary": "¥10",
      "free_quota_usd": 1.4,
      "validity": "30 days",
      "conditions": "phone",
      "signup_url": "https://platform.deepseek.com/sign_up",
      "key_setup_hint": "OpenClaw",
      "active": true,
      "last_verified": "2026-05-23",
      "notes": ""
    },
    {
      "id": "groq",
      "display_name": "Groq",
      "krouter_provider_name": "groq",
      "protocol": "openai",
      "region": "intl",
      "free_type": "daily_quota",
      "free_summary": "free",
      "free_quota_usd": 999,
      "validity": "no_expiry",
      "conditions": "email",
      "signup_url": "https://console.groq.com/keys",
      "key_setup_hint": "OpenClaw",
      "active": true,
      "last_verified": "2026-05-23",
      "notes": ""
    }
  ]
}`

// urlRewriter routes every request through the test server.
type urlRewriter struct{ target string }

func (r urlRewriter) RoundTrip(req *http.Request) (*http.Response, error) {
	u, _ := url.Parse(r.target)
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	req.URL.Path = u.Path
	req.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func newTestStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestSyncOnce_PrimaryHappyPath(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("ETag", `"abc"`)
		_, _ = w.Write([]byte(validPayload))
	}))
	defer srv.Close()

	store := newTestStore(t)
	svc := New(store, logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})

	require.NoError(t, svc.SyncOnce(context.Background()))

	rows, err := store.ListFreeProviders(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	etag, _ := store.GetSyncMeta(context.Background(), "free_providers_etag")
	assert.Equal(t, `"abc"`, etag)
}

func TestSyncOnce_NotModifiedSkips(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("If-None-Match") == "" {
			t.Errorf("expected If-None-Match in conditional request")
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer srv.Close()

	store := newTestStore(t)
	require.NoError(t, store.SetSyncMeta(context.Background(), "free_providers_etag", `"x"`))
	svc := New(store, logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})

	require.NoError(t, svc.SyncOnce(context.Background()))

	rows, _ := store.ListFreeProviders(context.Background(), true)
	assert.Empty(t, rows, "304 must not write rows")
	assert.Equal(t, 1, calls)
}

func TestSyncOnce_RejectsSchemaVersionMismatch(t *testing.T) {
	body := `{"schema_version": 99, "providers": []}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	svc := New(newTestStore(t), logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "schema_version")
}

func TestSyncOnce_RejectsEmptyProviders(t *testing.T) {
	body := `{"schema_version": 1, "providers": []}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	svc := New(newTestStore(t), logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no providers")
}

func TestSyncOnce_RejectsBadFreeType(t *testing.T) {
	body := `{
      "schema_version": 1,
      "providers": [{
        "id": "bad",
        "display_name": "Bad",
        "krouter_provider_name": "bad",
        "free_type": "INVALID_KIND",
        "signup_url": "https://example.com/x"
      }]
    }`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	svc := New(newTestStore(t), logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "free_type")
}

func TestSyncOnce_RejectsMissingSignupURL(t *testing.T) {
	body := `{
      "schema_version": 1,
      "providers": [{
        "id": "x",
        "display_name": "X",
        "krouter_provider_name": "x",
        "free_type": "trial_credit"
      }]
    }`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	svc := New(newTestStore(t), logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "signup_url required")
}

func TestSyncOnce_AcceptsAdditionalProtocols(t *testing.T) {
	body := `{
      "schema_version": 1,
      "providers": [{
        "id": "openrouter",
        "display_name": "OpenRouter",
        "krouter_provider_name": "openrouter",
        "protocol": "openai",
        "free_type": "free_tier",
        "signup_url": "https://openrouter.ai/keys",
        "active": true,
        "additional_protocols": [
          {
            "protocol": "anthropic",
            "krouter_provider_name": "openrouter-anthropic",
            "key_setup_hint": "same key, baseURL /v1"
          }
        ]
      }]
    }`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	store := newTestStore(t)
	svc := New(store, logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})
	require.NoError(t, svc.SyncOnce(context.Background()))

	rows, _ := store.ListFreeProviders(context.Background(), true)
	require.Len(t, rows, 1)
	require.Len(t, rows[0].AdditionalProtocols, 1)
	assert.Equal(t, "anthropic", rows[0].AdditionalProtocols[0].Protocol)
	assert.Equal(t, "openrouter-anthropic", rows[0].AdditionalProtocols[0].KrouterProviderName)
}

func TestSyncOnce_RejectsAdditionalProtocolWithSameProtocolAsPrimary(t *testing.T) {
	// additional_protocols listing the same protocol as the primary is
	// an editing accident — would either be a duplicate or hide the
	// primary entry. Reject.
	body := `{
      "schema_version": 1,
      "providers": [{
        "id": "dup",
        "display_name": "Dup",
        "krouter_provider_name": "dup",
        "protocol": "openai",
        "free_type": "free_tier",
        "signup_url": "https://example.com/",
        "additional_protocols": [
          {"protocol": "openai", "krouter_provider_name": "dup-2"}
        ]
      }]
    }`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	svc := New(newTestStore(t), logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicates primary")
}

func TestSyncOnce_RejectsAdditionalProtocolWithDuplicateName(t *testing.T) {
	body := `{
      "schema_version": 1,
      "providers": [{
        "id": "openrouter",
        "display_name": "OpenRouter",
        "krouter_provider_name": "openrouter",
        "protocol": "openai",
        "free_type": "free_tier",
        "signup_url": "https://example.com/",
        "additional_protocols": [
          {"protocol": "anthropic", "krouter_provider_name": "openrouter"}
        ]
      }]
    }`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	svc := New(newTestStore(t), logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}})
	err := svc.SyncOnce(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicates another")
}

func TestApplyEmbedded_SeedsFromBytes(t *testing.T) {
	// Test the install-time path: apply the embedded JSON without any
	// network involvement.
	store := newTestStore(t)
	svc := New(store, logging.New("error"))

	n, err := svc.ApplyEmbedded(context.Background(), []byte(validPayload))
	require.NoError(t, err)
	assert.Equal(t, 2, n)

	rows, _ := store.ListFreeProviders(context.Background(), true)
	assert.Len(t, rows, 2)
}

func TestSyncOnce_FiresOnUpdateCallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(validPayload))
	}))
	defer srv.Close()

	called := 0
	gotCount := 0
	svc := New(newTestStore(t), logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}}).
		WithUpdateCallback(func(c int) {
			called++
			gotCount = c
		})

	require.NoError(t, svc.SyncOnce(context.Background()))
	assert.Equal(t, 1, called)
	assert.Equal(t, 2, gotCount)
}

func TestSyncOnce_VersionedUserAgent(t *testing.T) {
	var seenUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenUA = r.Header.Get("User-Agent")
		_, _ = w.Write([]byte(validPayload))
	}))
	defer srv.Close()

	svc := New(newTestStore(t), logging.New("error")).
		WithHTTPClient(&http.Client{Transport: urlRewriter{srv.URL}}).
		WithVersion("v2.3.0")

	require.NoError(t, svc.SyncOnce(context.Background()))
	assert.Equal(t, "krouter-freeproviders-sync/v2.3.0", seenUA)
}

func TestApplyEmbedded_ParsesI18n(t *testing.T) {
	const payload = `{
	  "schema_version": 1,
	  "providers": [
	    {
	      "id": "zhipu-glm",
	      "display_name": "Zhipu GLM",
	      "krouter_provider_name": "zai",
	      "protocol": "openai",
	      "region": "china",
	      "free_type": "trial_credit",
	      "free_summary": "25M tokens",
	      "free_quota_usd": 3.5,
	      "validity": "30 days",
	      "conditions": "phone",
	      "signup_url": "https://open.bigmodel.cn",
	      "key_setup_hint": "zai key",
	      "active": true,
	      "last_verified": "2026-05-23",
	      "notes": "GLM-4-Flash free",
	      "i18n": { "zh": { "free_summary": "2500万 tokens", "notes": "GLM-4-Flash 完全免费" } },
	      "additional_protocols": [
	        {
	          "protocol": "anthropic",
	          "krouter_provider_name": "zai-anthropic",
	          "key_setup_hint": "same key",
	          "i18n": { "zh": { "key_setup_hint": "同一个 key" } }
	        }
	      ]
	    }
	  ]
	}`
	store := newTestStore(t)
	svc := New(store, logging.New("error"))
	_, err := svc.ApplyEmbedded(context.Background(), []byte(payload))
	require.NoError(t, err)

	rows, err := store.ListFreeProviders(context.Background(), true)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "25M tokens", rows[0].FreeSummary, "base field stays English")
	assert.Equal(t, "2500万 tokens", rows[0].I18n["zh"]["free_summary"], "zh override parsed")
	assert.Equal(t, "GLM-4-Flash 完全免费", rows[0].I18n["zh"]["notes"])
	require.Len(t, rows[0].AdditionalProtocols, 1)
	assert.Equal(t, "同一个 key", rows[0].AdditionalProtocols[0].I18n["zh"]["key_setup_hint"])
}

func TestStartSync_ZeroIntervalReturns(t *testing.T) {
	done := make(chan struct{})
	go func() {
		New(newTestStore(t), logging.New("error")).
			StartSync(context.Background(), 0)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("zero interval should return immediately")
	}
}

func TestStartSync_StopsOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		New(newTestStore(t), logging.New("error")).
			StartSync(ctx, 10*time.Millisecond)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("StartSync did not return after ctx cancel")
	}
}
