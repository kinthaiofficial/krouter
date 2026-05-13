package upgrade_test

import (
	"context"
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
	"time"

	"github.com/kinthaiofficial/krouter/internal/upgrade"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testKey generates a fresh ECDSA P-256 key pair for use in tests.
func testKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	return key
}

// buildManifest serialises a Manifest and signs it with key.
// Returns (manifestJSON, signature).
func buildManifest(t *testing.T, key *ecdsa.PrivateKey, m upgrade.Manifest) ([]byte, []byte) {
	t.Helper()
	body, err := json.Marshal(m)
	require.NoError(t, err)
	digest := sha256.Sum256(body)
	sig, err := ecdsa.SignASN1(rand.Reader, key, digest[:])
	require.NoError(t, err)
	return body, sig
}

// pubKeyPEM encodes the public portion of key as a PKIX PEM block.
func pubKeyPEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	require.NoError(t, err)
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
}

// newTestService creates an upgrade.Service pointing at a mock manifest server.
// It injects the test public key via the exported NewWithManifestURLAndKey constructor.
func newTestService(t *testing.T, key *ecdsa.PrivateKey, serverURL, currentVersion string) *upgrade.Service {
	t.Helper()
	svc, err := upgrade.NewWithManifestURLAndKey(currentVersion, serverURL, pubKeyPEM(t, key))
	require.NoError(t, err)
	return svc
}

// --- semver tests ---

func TestSemverGreater(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"0.2.0", "0.1.0", true},
		{"1.0.0", "0.9.9", true},
		{"0.1.1", "0.1.0", true},
		{"0.1.0", "0.1.0", false},
		{"0.0.9", "0.1.0", false},
		{"v1.0.0", "0.9.0", true},
		{"v0.0.1", "v0.0.1", false},
	}
	for _, c := range cases {
		t.Run(c.a+"_vs_"+c.b, func(t *testing.T) {
			assert.Equal(t, c.want, upgrade.SemverGreater(c.a, c.b))
		})
	}
}

// --- manifest fetch + signature verification ---

func TestCheckOnce_NewVersionAvailable(t *testing.T) {
	key := testKey(t)
	m := upgrade.Manifest{
		Version:     "1.0.0",
		ReleasedAt:  time.Now().UTC(),
		IsCritical:  false,
		Binaries:    map[string]upgrade.Binary{},
	}
	body, sig := buildManifest(t, key, m)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json.sig" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(sig)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	svc := newTestService(t, key, srv.URL+"/manifest.json", "0.0.1")
	svc.CheckOnceForTest(context.Background())

	latest := svc.Latest()
	require.NotNil(t, latest)
	assert.Equal(t, "1.0.0", latest.Version)
}

func TestCheckOnce_AlreadyLatest(t *testing.T) {
	key := testKey(t)
	m := upgrade.Manifest{Version: "0.0.1", Binaries: map[string]upgrade.Binary{}}
	body, sig := buildManifest(t, key, m)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json.sig" {
			_, _ = w.Write(sig)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	svc := newTestService(t, key, srv.URL+"/manifest.json", "0.0.1")
	svc.CheckOnceForTest(context.Background())

	assert.Nil(t, svc.Latest(), "Latest() should be nil when already up to date")
}

func TestCheckOnce_BadSignature(t *testing.T) {
	key := testKey(t)
	m := upgrade.Manifest{Version: "9.9.9", Binaries: map[string]upgrade.Binary{}}
	body, _ := buildManifest(t, key, m)

	// Use a *different* key to sign — signature will be invalid against the service's public key.
	wrongKey := testKey(t)
	digest := sha256.Sum256(body)
	badSig, _ := ecdsa.SignASN1(rand.Reader, wrongKey, digest[:])

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json.sig" {
			_, _ = w.Write(badSig)
			return
		}
		_, _ = w.Write(body)
	}))
	defer srv.Close()

	svc := newTestService(t, key, srv.URL+"/manifest.json", "0.0.1")
	svc.CheckOnceForTest(context.Background())

	// Bad signature → Latest() must remain nil.
	assert.Nil(t, svc.Latest())
}

func TestCheckOnce_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	key := testKey(t)
	svc := newTestService(t, key, srv.URL+"/manifest.json", "0.0.1")
	// Must not panic.
	assert.NotPanics(t, func() { svc.CheckOnceForTest(context.Background()) })
	assert.Nil(t, svc.Latest())
}

func TestApply_NoUpdateAvailable(t *testing.T) {
	key := testKey(t)
	svc, err := upgrade.NewWithManifestURLAndKey("0.0.1", "http://unused", pubKeyPEM(t, key))
	require.NoError(t, err)

	err = svc.Apply(context.Background(), nil)
	assert.Error(t, err, "Apply with no update should return an error")
}

func TestNew_EmbeddedKeyLoads(t *testing.T) {
	// Verify that the embedded dev public key is valid.
	svc, err := upgrade.New("0.0.1")
	require.NoError(t, err)
	assert.NotNil(t, svc)
}
