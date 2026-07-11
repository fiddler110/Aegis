package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"

	"github.com/fiddler110/aegis/internal/fsguard"
)

// ensureTLSCert loads an existing certificate/key pair from certPath/keyPath
// if both are present, or generates a new self-signed ECDSA P-256
// certificate and persists it there for reuse across restarts (FIND-32/
// P24.18) — the same "generate once, reuse unless missing" convention
// generateAndWriteToken uses for daemon.token.
//
// This is a locally pinned trust anchor, not a certificate meant to be
// verified against a public CA chain or a real hostname: the client (see
// internal/client.WithTLS) trusts exactly this file's contents, read
// directly off disk, rather than validating against system roots. That is
// why the certificate carries only loopback SANs and a long (10-year)
// validity window — there is no renewal workflow because there is no
// external party whose trust decay this needs to model, only "does this
// still match the file the client pinned."
func ensureTLSCert(certPath, keyPath string) (tls.Certificate, error) {
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return cert, nil
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate tls key: %w", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("generate tls serial: %w", err)
	}

	tmpl := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "aegis-daemon"},
		NotBefore:             time.Now().Add(-time.Hour), // small backdate tolerates clock skew
		NotAfter:              time.Now().AddDate(10, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:                  true, // self-signed leaf acting as its own trust anchor
		BasicConstraintsValid: true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
	}

	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("create tls certificate: %w", err)
	}
	keyDER, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("marshal tls key: %w", err)
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})

	// The certificate is not secret (it's the thing the client pins, and
	// nothing sensitive is derivable from it); 0o644 is fine. The private key
	// is, so it gets the same treatment as daemon.token: 0o600 plus a real,
	// non-inherited ACL on Windows via fsguard.RestrictToOwner (P24.16/
	// FIND-29's shared helper).
	if err := os.WriteFile(certPath, certPEM, 0o644); err != nil {
		return tls.Certificate{}, fmt.Errorf("write tls cert: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("write tls key: %w", err)
	}
	if err := fsguard.RestrictToOwner(keyPath); err != nil {
		return tls.Certificate{}, fmt.Errorf("restrict tls key permissions: %w", err)
	}

	return tls.X509KeyPair(certPEM, keyPEM)
}
