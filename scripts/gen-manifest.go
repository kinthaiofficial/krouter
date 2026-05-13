//go:build ignore

// gen-manifest generates manifest.json and signs it with an ECDSA P-256 private key.
//
// Usage:
//
//	go run scripts/gen-manifest.go \
//	  -version=v0.1.0 \
//	  -dist=dist \
//	  -key=private_key.pem \
//	  -repo=kinthaiofficial/krouter \
//	  -out=dist/manifest.json
package main

import (
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type Manifest struct {
	Version             string            `json:"version"`
	MinSupportedVersion string            `json:"min_supported_version"`
	ReleaseNotesURL     string            `json:"release_notes_url"`
	ReleasedAt          time.Time         `json:"released_at"`
	IsCritical          bool              `json:"is_critical"`
	Binaries            map[string]Binary `json:"binaries"`
}

type Binary struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// platformBinaries maps platform key → local dist/ filename and GitHub release asset name.
var platformBinaries = []struct {
	key       string // runtime.GOOS + "-" + runtime.GOARCH (used by auto-updater)
	glob      string // filename inside dist/
	assetName string // filename as uploaded to GitHub release
}{
	{"linux-amd64", "krouter-linux-amd64", "krouter-linux-amd64"},
	{"darwin-amd64", "krouter-apple-macos-amd64", "krouter-apple-macos-amd64"},
	{"darwin-arm64", "krouter-apple-macos-arm64", "krouter-apple-macos-arm64"},
	{"windows-amd64", "krouter-windows-amd64.exe", "krouter-windows-amd64.exe"},
}

func main() {
	version := flag.String("version", "", "Release version, e.g. v0.1.0 (required)")
	dist := flag.String("dist", "dist", "goreleaser dist directory")
	keyFile := flag.String("key", "private_key.pem", "ECDSA P-256 private key PEM file")
	repo := flag.String("repo", "kinthaiofficial/krouter", "GitHub repo (owner/name)")
	out := flag.String("out", "dist/manifest.json", "Output manifest.json path")
	flag.Parse()

	if *version == "" {
		fmt.Fprintln(os.Stderr, "gen-manifest: -version is required")
		os.Exit(1)
	}

	ver := strings.TrimPrefix(*version, "v")
	tag := *version
	if !strings.HasPrefix(tag, "v") {
		tag = "v" + tag
	}

	baseURL := fmt.Sprintf("https://github.com/%s/releases/download/%s", *repo, tag)
	releaseNotesURL := fmt.Sprintf("https://github.com/%s/releases/tag/%s", *repo, tag)

	binaries := map[string]Binary{}
	for _, pb := range platformBinaries {
		path := filepath.Join(*dist, pb.glob)
		matches, err := filepath.Glob(path)
		if err != nil || len(matches) == 0 {
			fmt.Fprintf(os.Stderr, "gen-manifest: warning: no binary found for %s at %s\n", pb.key, path)
			continue
		}
		binPath := matches[0]

		sum, size, err := sha256File(binPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gen-manifest: sha256 %s: %v\n", binPath, err)
			os.Exit(1)
		}

		binaries[pb.key] = Binary{
			URL:    baseURL + "/" + pb.assetName,
			SHA256: sum,
			Size:   size,
		}
		fmt.Printf("gen-manifest: %s → %s (sha256=%s)\n", pb.key, pb.assetName, sum[:12]+"...")
	}

	m := Manifest{
		Version:             ver,
		MinSupportedVersion: "1.0.0",
		ReleaseNotesURL:     releaseNotesURL,
		ReleasedAt:          time.Now().UTC(),
		IsCritical:          false,
		Binaries:            binaries,
	}

	manifestJSON, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-manifest: marshal: %v\n", err)
		os.Exit(1)
	}

	if err := os.WriteFile(*out, manifestJSON, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-manifest: write manifest: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("gen-manifest: wrote %s\n", *out)

	// Sign.
	privKey, err := loadPrivateKey(*keyFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-manifest: load private key: %v\n", err)
		os.Exit(1)
	}

	digest := sha256.Sum256(manifestJSON)
	sig, err := ecdsa.SignASN1(rand.Reader, privKey, digest[:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "gen-manifest: sign: %v\n", err)
		os.Exit(1)
	}

	sigPath := *out + ".sig"
	if err := os.WriteFile(sigPath, sig, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "gen-manifest: write sig: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("gen-manifest: wrote %s\n", sigPath)
}

func sha256File(path string) (hexSum string, size int64, err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), n, nil
}

func loadPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	// Key is stored as PKCS8 (openssl pkcs8 -topk8 -nocrypt).
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*ecdsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not an ECDSA private key")
	}
	return key, nil
}
