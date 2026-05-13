// Command keygen generates an ECDSA P-256 key pair for update manifest signing.
//
// Usage (one-time setup):
//
//	go run ./cmd/krouter/keygen
//
// Outputs:
//   - internal/upgrade/public_key.pem  (committed to repo)
//   - private_key.pem                  (keep secret, never commit)
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
)

func main() {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate key:", err)
		os.Exit(1)
	}

	privDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal private key:", err)
		os.Exit(1)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		fmt.Fprintln(os.Stderr, "marshal public key:", err)
		os.Exit(1)
	}

	if err := writeFile("private_key.pem", "EC PRIVATE KEY", privDER, 0600); err != nil {
		fmt.Fprintln(os.Stderr, "write private key:", err)
		os.Exit(1)
	}
	if err := writeFile("internal/upgrade/public_key.pem", "PUBLIC KEY", pubDER, 0644); err != nil {
		fmt.Fprintln(os.Stderr, "write public key:", err)
		os.Exit(1)
	}

	fmt.Println("Generated:")
	fmt.Println("  private_key.pem              ← keep secret, do NOT commit")
	fmt.Println("  internal/upgrade/public_key.pem ← commit this file")
}

func writeFile(path, pemType string, der []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	return pem.Encode(f, &pem.Block{Type: pemType, Bytes: der})
}
