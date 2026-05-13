// Package upgrade handles automatic binary updates via a signed manifest.
//
// Flow (spec/08):
//  1. Every 24 h, fetch manifest.json + manifest.json.sig from GitHub releases.
//  2. Verify ECDSA P-256 signature using the embedded public key.
//  3. If a newer version exists, expose it via Latest().
//  4. Apply() downloads the binary, verifies SHA-256, and atomically replaces
//     the running binary using github.com/minio/selfupdate.
package upgrade

import (
	"context"
	"crypto/ecdsa"
	"crypto/sha256"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime"
	"sync"
	"time"

	"github.com/minio/selfupdate"
)

//go:embed public_key.pem
var embeddedPublicKey []byte

const defaultManifestURL = "https://github.com/kinthaiofficial/krouter/releases/latest/download/manifest.json"

// Manifest describes a release from the CDN.
type Manifest struct {
	Version            string            `json:"version"`
	MinSupportedVersion string           `json:"min_supported_version"`
	ReleaseNotesURL    string            `json:"release_notes_url"`
	ReleasedAt         time.Time         `json:"released_at"`
	IsCritical         bool              `json:"is_critical"`
	Binaries           map[string]Binary `json:"binaries"` // "linux-amd64" → Binary
}

// Binary describes a single platform binary in the manifest.
type Binary struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Service checks for updates and applies them on demand.
type Service struct {
	currentVersion string
	manifestURL    string
	publicKey      *ecdsa.PublicKey
	httpClient     *http.Client
	logger         *slog.Logger

	mu     sync.RWMutex
	latest *Manifest // nil when already up-to-date
}

// New creates a Service using the embedded public key and the default manifest URL.
func New(currentVersion string) (*Service, error) {
	return NewWithManifestURL(currentVersion, defaultManifestURL)
}

// NewWithManifestURL creates a Service with a configurable manifest URL (for testing).
func NewWithManifestURL(currentVersion, manifestURL string) (*Service, error) {
	return NewWithManifestURLAndKey(currentVersion, manifestURL, embeddedPublicKey)
}

// NewWithManifestURLAndKey creates a Service with a custom manifest URL and PEM public key.
// Intended for tests that generate a fresh key pair rather than using the embedded dev key.
func NewWithManifestURLAndKey(currentVersion, manifestURL string, pubKeyPEM []byte) (*Service, error) {
	pub, err := parsePublicKey(pubKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("upgrade: parse public key: %w", err)
	}
	return &Service{
		currentVersion: currentVersion,
		manifestURL:    manifestURL,
		publicKey:      pub,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		logger:         slog.Default(),
	}, nil
}

// Start checks for updates every interval until ctx is cancelled.
func (s *Service) Start(ctx context.Context, interval time.Duration) {
	s.checkOnce(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.checkOnce(ctx)
		}
	}
}

// CheckOnceForTest triggers a single update check synchronously. For tests only.
func (s *Service) CheckOnceForTest(ctx context.Context) {
	s.checkOnce(ctx)
}

// Latest returns the latest available manifest if an update is available, or nil.
func (s *Service) Latest() *Manifest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

// Apply downloads and atomically applies the update binary.
// onProgress is called with percentage 0-100 during the download.
func (s *Service) Apply(ctx context.Context, onProgress func(pct int)) error {
	s.mu.RLock()
	m := s.latest
	s.mu.RUnlock()

	if m == nil {
		return fmt.Errorf("upgrade: no update available")
	}

	platformKey := runtime.GOOS + "-" + runtime.GOARCH
	bin, ok := m.Binaries[platformKey]
	if !ok {
		return fmt.Errorf("upgrade: no binary for platform %s", platformKey)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, bin.URL, nil)
	if err != nil {
		return fmt.Errorf("upgrade: build request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("upgrade: download binary: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("upgrade: unexpected status %d downloading binary", resp.StatusCode)
	}

	// Wrap the body in a progress reader and a SHA-256 hasher.
	total := bin.Size
	hasher := sha256.New()
	pr := &progressReader{r: resp.Body, hasher: hasher, total: total, cb: onProgress}

	if err := selfupdate.Apply(pr, selfupdate.Options{}); err != nil {
		return fmt.Errorf("upgrade: apply binary: %w", err)
	}

	// Verify hash after apply (selfupdate writes via a temp file; hash reflects what was read).
	got := fmt.Sprintf("%x", hasher.Sum(nil))
	if got != bin.SHA256 {
		return fmt.Errorf("upgrade: sha256 mismatch: got %s want %s", got, bin.SHA256)
	}

	s.logger.Info("upgrade: applied", "version", m.Version)
	s.mu.Lock()
	s.latest = nil
	s.mu.Unlock()
	return nil
}

// checkOnce fetches and verifies the manifest, updating s.latest.
func (s *Service) checkOnce(ctx context.Context) {
	s.logger.Info("upgrade: checking for updates", "current", s.currentVersion)

	manifest, err := s.fetchManifest(ctx)
	if err != nil {
		s.logger.Warn("upgrade: manifest fetch failed", "err", err)
		return
	}

	if !semverGreater(manifest.Version, s.currentVersion) {
		s.logger.Debug("upgrade: already up to date", "version", s.currentVersion)
		s.mu.Lock()
		s.latest = nil
		s.mu.Unlock()
		return
	}

	s.logger.Info("upgrade: update available", "latest", manifest.Version)
	s.mu.Lock()
	s.latest = manifest
	s.mu.Unlock()
}

// fetchManifest downloads manifest.json and verifies its ECDSA signature.
func (s *Service) fetchManifest(ctx context.Context) (*Manifest, error) {
	body, err := s.get(ctx, s.manifestURL)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	sig, err := s.get(ctx, s.manifestURL+".sig")
	if err != nil {
		return nil, fmt.Errorf("fetch manifest sig: %w", err)
	}

	digest := sha256.Sum256(body)
	if !ecdsa.VerifyASN1(s.publicKey, digest[:], sig) {
		return nil, fmt.Errorf("manifest signature verification failed")
	}

	var m Manifest
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	return &m, nil
}

func (s *Service) get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 5*1024*1024))
}

// --- semver helpers (no external library) ---

func parseSemver(v string) [3]int {
	// Strip leading 'v'.
	if len(v) > 0 && v[0] == 'v' {
		v = v[1:]
	}
	var major, minor, patch int
	fmt.Sscanf(v, "%d.%d.%d", &major, &minor, &patch) //nolint:errcheck
	return [3]int{major, minor, patch}
}

// SemverGreater returns true when a > b. Exported for testing.
func SemverGreater(a, b string) bool { return semverGreater(a, b) }

// semverGreater returns true when a > b.
func semverGreater(a, b string) bool {
	av, bv := parseSemver(a), parseSemver(b)
	for i := range av {
		if av[i] != bv[i] {
			return av[i] > bv[i]
		}
	}
	return false
}

// --- PEM helpers ---

func parsePublicKey(pemBytes []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	ecPub, ok := pub.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an ECDSA public key")
	}
	return ecPub, nil
}

// --- progress reader ---

type progressReader struct {
	r      io.Reader
	hasher io.Writer
	total  int64
	read   int64
	cb     func(int)
}

func (pr *progressReader) Read(p []byte) (n int, err error) {
	n, err = pr.r.Read(p)
	if n > 0 {
		_, _ = pr.hasher.Write(p[:n])
		pr.read += int64(n)
		if pr.cb != nil && pr.total > 0 {
			pr.cb(int(pr.read * 100 / pr.total))
		}
	}
	return
}
