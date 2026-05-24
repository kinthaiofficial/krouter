package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/kinthaiofficial/krouter/internal/upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUpdateCheck_TriggersFreshFetchAndReturnsStatus exercises the
// new POST /internal/update-check endpoint that the About page hits
// when the user opens the page. The endpoint must:
//   1) call upgrade.Service.CheckNow synchronously
//   2) return the same JSON shape as /internal/update-status
func TestUpdateCheck_TriggersFreshFetchAndReturnsStatus(t *testing.T) {
	// Spin up a stub manifest server signed by a freshly generated key.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	pubKey, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	pubPEM := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubKey})

	manifestJSON := `{"version":"9.9.9","min_supported_version":"1.0.0","binaries":{}}`
	digest := sha256.Sum256([]byte(manifestJSON))
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	require.NoError(t, err)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json.sig" {
			_, _ = w.Write(sig)
			return
		}
		_, _ = w.Write([]byte(manifestJSON))
	}))
	defer upstream.Close()

	svc, err := upgrade.NewWithManifestURLAndKey("v0.0.1", upstream.URL+"/manifest.json", pubPEM)
	require.NoError(t, err)

	// Build a server, wire the upgrade service, no prior check has run yet
	// so s.upgrade.Latest() returns nil.
	store, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })
	srv := New(store, "v0.0.1", 8402, 8403)
	srv.SetTokenForTest("test-token")
	srv.SetUpgrade(svc)

	// Sanity: GET /internal/update-status before the check runs returns latest=null.
	gw := httptest.NewRecorder()
	gr := httptest.NewRequest(http.MethodGet, "/internal/update-status", nil)
	gr.Header.Set("Authorization", "Bearer test-token")
	srv.Handler().ServeHTTP(gw, gr)
	require.Equal(t, http.StatusOK, gw.Code)
	var before map[string]any
	require.NoError(t, json.Unmarshal(gw.Body.Bytes(), &before))
	assert.Nil(t, before["latest"], "before the fresh check, latest is null")

	// Trigger the fresh check.
	pw := httptest.NewRecorder()
	pr := httptest.NewRequest(http.MethodPost, "/internal/update-check", nil)
	pr.Header.Set("Authorization", "Bearer test-token")
	srv.Handler().ServeHTTP(pw, pr)
	require.Equal(t, http.StatusOK, pw.Code)

	var after map[string]any
	require.NoError(t, json.Unmarshal(pw.Body.Bytes(), &after))
	assert.Equal(t, "9.9.9", after["latest"], "after the synchronous check, latest carries the upstream version")
	assert.Equal(t, "v0.0.1", after["current"])
}

func TestUpdateCheck_RejectsNonPost(t *testing.T) {
	store, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, store.Migrate())
	t.Cleanup(func() { _ = store.Close() })
	srv := New(store, "v0.0.1", 8402, 8403)
	srv.SetTokenForTest("test-token")

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/internal/update-check", nil)
	r.Header.Set("Authorization", "Bearer test-token")
	srv.Handler().ServeHTTP(w, r)
	assert.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
