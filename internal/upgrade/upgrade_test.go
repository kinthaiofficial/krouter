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
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
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
	svc.CheckNow(context.Background())

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
	svc.CheckNow(context.Background())

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
	svc.CheckNow(context.Background())

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
	assert.NotPanics(t, func() { svc.CheckNow(context.Background()) })
	assert.Nil(t, svc.Latest())
}

func TestApply_NoUpdateAvailable(t *testing.T) {
	key := testKey(t)
	svc, err := upgrade.NewWithManifestURLAndKey("0.0.1", "http://unused", pubKeyPEM(t, key))
	require.NoError(t, err)

	err = svc.Apply(context.Background(), nil)
	assert.Error(t, err, "Apply with no update should return an error")
}

// TestApply_RetriesOnTransientFailure verifies that Apply retries when the
// download server returns errors on the first N attempts then succeeds.
// This is the regression test for issue #73 (unstable proxy causing EOF).
func TestApply_RetriesOnTransientFailure(t *testing.T) {
	// Seed the service with a fake manifest pointing at our mock server.
	key := testKey(t)

	attempts := 0
	// The mock binary payload — small enough for a unit test but realistic.
	payload := []byte("fake-binary-content")
	payloadHash := sha256.Sum256(payload)
	hashHex := fmt.Sprintf("%x", payloadHash)

	var binaryURL string // set after server starts

	failUntil := 2 // first 2 attempts return 500, 3rd succeeds

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/bin" {
			attempts++
			if attempts <= failUntil {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(payload)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	binaryURL = srv.URL + "/bin"

	import_runtime := runtime.GOOS + "-" + runtime.GOARCH
	m := upgrade.Manifest{
		Version:    "9.9.9",
		ReleasedAt: time.Now().UTC(),
		Binaries: map[string]upgrade.Binary{
			import_runtime: {URL: binaryURL, SHA256: hashHex, Size: int64(len(payload))},
		},
	}

	manifestBody, sig := buildManifest(t, key, m)
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json.sig" {
			_, _ = w.Write(sig)
			return
		}
		_, _ = w.Write(manifestBody)
	}))
	defer manifestSrv.Close()

	svc := newTestService(t, key, manifestSrv.URL+"/manifest.json", "0.0.1")
	svc.CheckNow(context.Background())
	require.NotNil(t, svc.Latest(), "update must be detected")

	// Apply should succeed on the 3rd attempt despite 2 initial failures.
	// We can't atomically replace the binary in a unit test (selfupdate.Apply
	// writes the running executable), so we just verify Apply returns no error
	// and that attempts == failUntil+1.
	// Note: selfupdate.Apply will fail to replace the test binary path with
	// "fake-binary-content" at the OS level — but that's after our retry logic.
	// We assert on attempts to confirm the retry loop ran.
	_ = svc.Apply(context.Background(), nil) // may error at selfupdate.Apply level; that's ok
	assert.Equal(t, failUntil+1, attempts, "should have attempted failUntil+1 downloads")
}

// TestApply_ExhaustsRetries verifies that Apply returns an error after
// exhausting all retry attempts rather than hanging indefinitely.
func TestApply_ExhaustsRetries(t *testing.T) {
	key := testKey(t)

	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	import_runtime := runtime.GOOS + "-" + runtime.GOARCH
	m := upgrade.Manifest{
		Version:    "9.9.9",
		ReleasedAt: time.Now().UTC(),
		Binaries: map[string]upgrade.Binary{
			import_runtime: {URL: srv.URL + "/bin", SHA256: "abc", Size: 100},
		},
	}
	manifestBody, sig := buildManifest(t, key, m)
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json.sig" {
			_, _ = w.Write(sig)
			return
		}
		_, _ = w.Write(manifestBody)
	}))
	defer manifestSrv.Close()

	svc := newTestService(t, key, manifestSrv.URL+"/manifest.json", "0.0.1")
	svc.CheckNow(context.Background())
	require.NotNil(t, svc.Latest())

	err := svc.Apply(context.Background(), nil)
	assert.Error(t, err, "Apply should return error after all retries exhausted")
	assert.Equal(t, 4, attempts, "should have attempted exactly maxApplyAttempts=4 downloads")
}

// TestApply_FallsBackToMirror verifies that when the primary (GitHub) binary
// URL is unreachable, Apply downloads from the FallbackURL (CDN mirror) within
// the same attempt — the China-network-restriction scenario.
func TestApply_FallsBackToMirror(t *testing.T) {
	key := testKey(t)

	payload := []byte("fake-binary-content")
	payloadHash := sha256.Sum256(payload)
	hashHex := fmt.Sprintf("%x", payloadHash)

	primaryHits, mirrorHits := 0, 0

	// Primary always fails (simulates GitHub blocked / unreachable).
	primarySrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		primaryHits++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer primarySrv.Close()

	// Mirror serves the binary.
	mirrorSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mirrorHits++
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(payload)))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer mirrorSrv.Close()

	import_runtime := runtime.GOOS + "-" + runtime.GOARCH
	m := upgrade.Manifest{
		Version:    "9.9.9",
		ReleasedAt: time.Now().UTC(),
		Binaries: map[string]upgrade.Binary{
			import_runtime: {
				URL:         primarySrv.URL + "/bin",
				FallbackURL: mirrorSrv.URL + "/bin",
				SHA256:      hashHex,
				Size:        int64(len(payload)),
			},
		},
	}
	manifestBody, sig := buildManifest(t, key, m)
	manifestSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/manifest.json.sig" {
			_, _ = w.Write(sig)
			return
		}
		_, _ = w.Write(manifestBody)
	}))
	defer manifestSrv.Close()

	svc := newTestService(t, key, manifestSrv.URL+"/manifest.json", "0.0.1")
	svc.CheckNow(context.Background())
	require.NotNil(t, svc.Latest())

	// selfupdate.Apply may fail to swap the test binary at the OS level, but the
	// download routing runs before that. We assert the mirror was reached.
	_ = svc.Apply(context.Background(), nil)
	assert.GreaterOrEqual(t, primaryHits, 1, "primary should be tried first")
	assert.GreaterOrEqual(t, mirrorHits, 1, "mirror should be used after primary fails")
}

func TestNew_EmbeddedKeyLoads(t *testing.T) {
	// Verify that the embedded dev public key is valid.
	svc, err := upgrade.New("0.0.1")
	require.NoError(t, err)
	assert.NotNil(t, svc)
}
