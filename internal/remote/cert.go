package remote

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

// GenerateSelfSignedCert creates an ECDSA P-256 self-signed certificate valid
// for 10 years, covering the given hostname and IP addresses.
// Returns PEM-encoded certificate and private key.
func GenerateSelfSignedCert(hostname string, lanIPs []net.IP) (certPEM, keyPEM []byte, err error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("remote: generate key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("remote: serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   "krouter LAN",
			Organization: []string{"kinthai"},
		},
		NotBefore:             time.Now().Add(-time.Minute), // small back-date for clock skew
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	if hostname != "" {
		template.DNSNames = []string{hostname}
	}
	for _, ip := range lanIPs {
		if ip4 := ip.To4(); ip4 != nil {
			template.IPAddresses = append(template.IPAddresses, ip4)
		} else {
			template.IPAddresses = append(template.IPAddresses, ip)
		}
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("remote: create certificate: %w", err)
	}

	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("remote: marshal key: %w", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	return certPEM, keyPEM, nil
}

// SaveCert writes cert and key PEM files to ~/.kinthai/ with secure permissions.
func SaveCert(certPEM, keyPEM []byte) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("remote: home dir: %w", err)
	}
	dir := filepath.Join(home, ".kinthai")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("remote: mkdir: %w", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "cert.pem"), certPEM, 0644); err != nil {
		return fmt.Errorf("remote: write cert: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "key.pem"), keyPEM, 0600); err != nil {
		return fmt.Errorf("remote: write key: %w", err)
	}
	return nil
}

// LoadOrGenerateCert loads the cert/key pair from ~/.kinthai/, generating a
// new one if either file is missing or unreadable.
func LoadOrGenerateCert(hostname string, lanIPs []net.IP) (certPEM, keyPEM []byte, err error) {
	home, _ := os.UserHomeDir()
	certPath := filepath.Join(home, ".kinthai", "cert.pem")
	keyPath := filepath.Join(home, ".kinthai", "key.pem")

	certPEM, certErr := os.ReadFile(certPath)
	keyPEM, keyErr := os.ReadFile(keyPath)
	if certErr == nil && keyErr == nil {
		return certPEM, keyPEM, nil
	}

	certPEM, keyPEM, err = GenerateSelfSignedCert(hostname, lanIPs)
	if err != nil {
		return nil, nil, err
	}
	_ = SaveCert(certPEM, keyPEM) // best-effort; errors ignored
	return certPEM, keyPEM, nil
}
