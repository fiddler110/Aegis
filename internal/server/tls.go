package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math/big"
	"net"
	"os"
	"strings"
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
// The logger is used for the P81.25 regeneration warning below; it is never
// nil at the one call site (wireAuthAndTLS), but a nil one is tolerated so a
// test can call this helper bare.
func ensureTLSCert(certPath, keyPath string, logger *slog.Logger) (tls.Certificate, error) {
	if cert, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		if logger != nil {
			logger.Info("tls certificate loaded",
				"path", certPath,
				"fingerprint_sha256", certFingerprint(cert.Certificate[0]),
			)
		}
		return cert, nil
	}

	// P81.25: distinguish first-ever generation (normal, and silent beyond
	// the Info line at the end) from regeneration over material a client may
	// already have pinned. internal/client.WithTLS trusts exactly the bytes
	// at certPath, so replacing them silently changes the identity of the
	// listener every pinned client trusts — deleting daemon.crt, or a
	// half-written pair that no longer loads, both land here. That is worth
	// an operator-visible line even though the regeneration itself is what
	// keeps the daemon startable.
	certExisted := fileExists(certPath)
	keyExisted := fileExists(keyPath)

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

	// Both halves get the daemon.token treatment: 0o600 plus a real,
	// non-inherited ACL on Windows via fsguard.RestrictToOwner (P24.16/
	// FIND-29's shared helper). The key is secret, so that has always been
	// obvious. The certificate is not secret — but it is the thing
	// internal/client.WithTLS *pins*, so its integrity is exactly as
	// load-bearing as the key's confidentiality: another local account that
	// can rewrite daemon.crt makes the client trust an impersonating
	// listener. The mode bit alone does not say that on Windows, where a new
	// file inherits its parent directory's ACL rather than deriving
	// permissions from the mode argument (see generateAndWriteToken's comment
	// for the same reasoning applied to the token). P81.25/FIND-25.
	if err := os.WriteFile(certPath, certPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("write tls cert: %w", err)
	}
	if err := os.WriteFile(keyPath, keyPEM, 0o600); err != nil {
		return tls.Certificate{}, fmt.Errorf("write tls key: %w", err)
	}
	if err := fsguard.RestrictToOwner(certPath); err != nil {
		return tls.Certificate{}, fmt.Errorf("restrict tls cert permissions: %w", err)
	}
	if err := fsguard.RestrictToOwner(keyPath); err != nil {
		return tls.Certificate{}, fmt.Errorf("restrict tls key permissions: %w", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return tls.Certificate{}, err
	}
	if logger != nil {
		fp := certFingerprint(der)
		if certExisted || keyExisted {
			logger.Warn("tls certificate regenerated over existing material; clients pinning the old certificate will fail until they re-read it",
				"cert_path", certPath,
				"key_path", keyPath,
				"cert_existed", certExisted,
				"key_existed", keyExisted,
				"fingerprint_sha256", fp,
			)
		} else {
			logger.Info("tls certificate generated",
				"path", certPath,
				"fingerprint_sha256", fp,
			)
		}
	}
	return cert, nil
}

// certFingerprint renders the SHA-256 fingerprint of a DER certificate in the
// colon-separated hex form openssl and every browser certificate viewer show,
// so an operator has something they can actually compare against the file on
// disk (`openssl x509 -in daemon.crt -noout -fingerprint -sha256`). Logged on
// every start — load and generation alike — because a pin change is only
// detectable if the pre-change value was visible too. Nothing else in the tree
// surfaces this today (P81.25/FIND-25).
func certFingerprint(der []byte) string {
	sum := sha256.Sum256(der)
	var b strings.Builder
	for i, x := range sum {
		if i > 0 {
			b.WriteByte(':')
		}
		fmt.Fprintf(&b, "%02X", x)
	}
	return b.String()
}

// CertFingerprint reads the PEM certificate at certPath and returns its
// SHA-256 fingerprint in the same colon-separated hex form certFingerprint
// logs at daemon startup. Exported for `aegis ui` (internal/cli/ui.go),
// which prints it alongside its self-signed-certificate warning (P81.18/
// FIND-18) so an operator who does click through the browser's warning has
// something on hand to compare against — the daemon's own startup log line
// is easy to miss, and is the one place this value existed before.
func CertFingerprint(certPath string) (string, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", fmt.Errorf("no PEM block found in %s", certPath)
	}
	return certFingerprint(block.Bytes), nil
}

// fileExists reports whether path is present, treating any stat error other
// than "not found" as present: the P81.25 warning should err toward telling
// the operator that material may have been replaced.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !errors.Is(err, fs.ErrNotExist)
}
