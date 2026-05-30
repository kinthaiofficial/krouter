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

const (
	// defaultManifestURL is the authoritative source: GitHub releases. Tried first,
	// everywhere (D-013 keeps GitHub as the source of truth).
	defaultManifestURL = "https://github.com/kinthaiofficial/krouter/releases/latest/download/manifest.json"
	// defaultFallbackManifestURL is the kinthai.ai mirror, used only when GitHub
	// is unreachable (e.g. network restrictions in mainland China). The mirror
	// serves the same signed manifest, so the ECDSA check passes either way.
	defaultFallbackManifestURL = "https://krouter.kinthai.ai/release/latest/manifest.json"
)

// Manifest describes a release.
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
	URL         string `json:"url"`                    // primary: GitHub release asset
	FallbackURL string `json:"fallback_url,omitempty"` // mirror: CDN (used when URL unreachable)
	SHA256      string `json:"sha256"`
	Size        int64  `json:"size"`
}

// Service checks for updates and applies them on demand.
type Service struct {
	currentVersion      string
	manifestURL         string
	fallbackManifestURL string // CDN mirror; "" disables fallback (tests)
	publicKey           *ecdsa.PublicKey
	httpClient          *http.Client
	logger              *slog.Logger

	mu     sync.RWMutex
	latest *Manifest // nil when already up-to-date
}

// New creates a Service using the embedded public key, the default GitHub
// manifest URL, and the CDN mirror as fallback.
func New(currentVersion string) (*Service, error) {
	s, err := NewWithManifestURL(currentVersion, defaultManifestURL)
	if err != nil {
		return nil, err
	}
	s.fallbackManifestURL = defaultFallbackManifestURL
	return s, nil
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

// WithHTTPClient replaces the default HTTP client. Useful for injecting a
// proxy-aware client at daemon startup.
func (s *Service) WithHTTPClient(c *http.Client) *Service {
	s.httpClient = c
	return s
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

// CheckNow triggers a single update check synchronously, off the normal
// 24 h schedule. Used by the About page's "open the page → just tell me
// if there's an update" flow so users don't have to wait up to a full
// day for the periodic ticker. Tests also call this to drive the
// check loop deterministically.
func (s *Service) CheckNow(ctx context.Context) {
	s.checkOnce(ctx)
}

// Latest returns the latest available manifest if an update is available, or nil.
func (s *Service) Latest() *Manifest {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest
}

const (
	maxApplyAttempts = 4
	applyBaseDelay   = 2 * time.Second
	// downloadTimeout caps a single download attempt. GitHub release assets
	// are typically 25–40 MB; 10 minutes is generous even on a 1 Mbit link.
	// The outer http.Client.Timeout (30 s) covers manifest fetches only —
	// we override it per-attempt via context deadline.
	downloadTimeout = 10 * time.Minute
)

// Apply downloads and atomically applies the update binary.
// onProgress is called with percentage 0-100 during the download;
// it is reset to 0 at the start of each retry attempt.
//
// Up to maxApplyAttempts are made with exponential backoff (2 s, 4 s, 8 s)
// so that transient network failures (unexpected EOF, connection reset)
// through an unstable proxy don't permanently block the upgrade.
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

	// Download sources in priority order: GitHub first, CDN mirror as fallback.
	urls := []string{bin.URL}
	if bin.FallbackURL != "" {
		urls = append(urls, bin.FallbackURL)
	}

	var lastErr error
	for attempt := 0; attempt < maxApplyAttempts; attempt++ {
		if attempt > 0 {
			delay := applyBaseDelay * (1 << (attempt - 1)) // 2s, 4s, 8s
			s.logger.Warn("upgrade: download failed, retrying",
				"attempt", attempt, "of", maxApplyAttempts-1,
				"delay", delay, "err", lastErr)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
			if onProgress != nil {
				onProgress(0) // reset UI progress bar for next attempt
			}
		}

		// Within each attempt, try GitHub then the mirror. The mirror serves the
		// same binary, so the SHA-256 check (bin.SHA256) applies to both.
		for i, url := range urls {
			if i > 0 {
				s.logger.Warn("upgrade: primary download unreachable, trying mirror",
					"mirror", url, "err", lastErr)
				if onProgress != nil {
					onProgress(0)
				}
			}
			lastErr = s.applyOnce(ctx, url, bin, onProgress)
			if lastErr == nil {
				s.logger.Info("upgrade: applied", "version", m.Version,
					"attempt", attempt+1, "source", url)
				s.mu.Lock()
				s.latest = nil
				s.mu.Unlock()
				return nil
			}
		}
	}
	return fmt.Errorf("upgrade: apply failed after %d attempts: %w", maxApplyAttempts, lastErr)
}

// applyOnce performs a single download + apply attempt from url with its own
// deadline. bin supplies the expected SHA-256 and size (same for any source).
func (s *Service) applyOnce(ctx context.Context, url string, bin Binary, onProgress func(pct int)) error {
	dlCtx, cancel := context.WithTimeout(ctx, downloadTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(dlCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("download binary: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	hasher := sha256.New()
	pr := &progressReader{r: resp.Body, hasher: hasher, total: bin.Size, cb: onProgress}

	if err := selfupdate.Apply(pr, selfupdate.Options{}); err != nil {
		return fmt.Errorf("apply binary: %w", err)
	}

	got := fmt.Sprintf("%x", hasher.Sum(nil))
	if got != bin.SHA256 {
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, bin.SHA256)
	}
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
// It tries GitHub first, then the CDN mirror — each source is fetched as a
// (manifest, sig) pair so a partial outage can't mix sources.
func (s *Service) fetchManifest(ctx context.Context) (*Manifest, error) {
	sources := []string{s.manifestURL}
	if s.fallbackManifestURL != "" {
		sources = append(sources, s.fallbackManifestURL)
	}

	var lastErr error
	for i, base := range sources {
		m, err := s.fetchManifestFrom(ctx, base)
		if err == nil {
			return m, nil
		}
		lastErr = err
		if i+1 < len(sources) {
			s.logger.Warn("upgrade: manifest source unreachable, trying mirror",
				"failed", base, "err", err)
		}
	}
	return nil, lastErr
}

// fetchManifestFrom downloads + verifies the manifest from a single base URL.
func (s *Service) fetchManifestFrom(ctx context.Context, manifestURL string) (*Manifest, error) {
	body, err := s.get(ctx, manifestURL)
	if err != nil {
		return nil, fmt.Errorf("fetch manifest: %w", err)
	}
	sig, err := s.get(ctx, manifestURL+".sig")
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
