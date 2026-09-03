package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/fsguard"
)

// freeLoopbackAddr reserves an ephemeral loopback port by binding to it and
// immediately releasing it, so the returned address is (almost certainly)
// free for the daemon to bind moments later.
func freeLoopbackAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("reserve free port: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// TestListenAndServeTLSRoundTrip covers FIND-32/P24.18: with server.tls.enabled
// true, the daemon auto-generates a self-signed cert/key under DataDir and
// serves HTTPS; a client pinned to that exact certificate (client.WithTLS,
// mirroring what client.NewFromConfig wires up automatically) can reach it,
// while a client that does not trust that certificate fails closed rather
// than silently connecting (no InsecureSkipVerify anywhere in this path).
func TestListenAndServeTLSRoundTrip(t *testing.T) {
	dir := t.TempDir()
	addr := freeLoopbackAddr(t)

	cfg := &config.Config{
		DataDir:    dir,
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		Server:     config.ServerConfig{Addr: addr, TLS: config.ServerTLSConfig{Enabled: true}},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.tlsCert == nil {
		t.Fatal("expected tlsCert to be populated when server.tls.enabled is true")
	}
	// The auto-generated cert/key must be persisted so a restart reuses them
	// instead of minting a new pair the client would no longer trust.
	if _, err := os.Stat(cfg.TLSCertPath()); err != nil {
		t.Fatalf("expected cert file to be written at %s: %v", cfg.TLSCertPath(), err)
	}
	if _, err := os.Stat(cfg.TLSKeyPath()); err != nil {
		t.Fatalf("expected key file to be written at %s: %v", cfg.TLSKeyPath(), err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()
	defer func() {
		cancel()
		<-errCh
	}()

	// A client pinned to the daemon's own certificate must succeed.
	pinned, err := client.New(addr).WithTLS(cfg.TLSCertPath())
	if err != nil {
		t.Fatalf("WithTLS: %v", err)
	}
	if err := waitForHealthy(pinned, 3*time.Second); err != nil {
		t.Fatalf("pinned client never became healthy: %v", err)
	}

	// A client that does not pin the certificate (default system root pool)
	// must fail closed against a self-signed certificate — no silent trust.
	unpinned := client.New("https://" + addr)
	if err := unpinned.Health(context.Background()); err == nil {
		t.Fatal("expected unpinned client to fail against the self-signed certificate, got nil error")
	}
}

// TestListenAndServeTLSDisabledUnchanged pins down that explicitly leaving
// server.tls.enabled false (its pre-P27.5 value; config.Load() itself now
// defaults it to true — see TestEnvOverrideServerTLS/TestLoadDefaults in
// internal/config) writes no cert/key files and serves plain HTTP exactly as
// before this feature existed. This test builds *Server directly from a
// struct literal, bypassing config.Load()'s defaults entirely, so it is
// unaffected by that default flip and still covers the explicit-opt-out path
// (AEGIS_SERVER_TLS_ENABLED=false or server.tls.enabled: false in YAML).
func TestListenAndServeTLSDisabledUnchanged(t *testing.T) {
	dir := t.TempDir()
	addr := freeLoopbackAddr(t)

	cfg := &config.Config{
		DataDir:    dir,
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
		Server:     config.ServerConfig{Addr: addr},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if srv.tlsCert != nil {
		t.Fatal("expected tlsCert to stay nil when TLS is explicitly disabled")
	}
	if _, err := os.Stat(cfg.TLSCertPath()); err == nil {
		t.Fatal("expected no cert file to be written when TLS is disabled")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe(ctx) }()
	defer func() {
		cancel()
		<-errCh
	}()

	plain := client.New(addr).WithTokenFile(cfg.AuthTokenPath())
	if err := waitForHealthy(plain, 3*time.Second); err != nil {
		t.Fatalf("plain-HTTP client never became healthy: %v", err)
	}
}

func waitForHealthy(cl *client.Client, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if lastErr = cl.Health(context.Background()); lastErr == nil {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return lastErr
}

// TestEnsureTLSCertRestrictsBothHalves is P81.25/FIND-25. daemon.key was
// already given fsguard.RestrictToOwner; daemon.crt was not, even though it is
// the file internal/client.WithTLS *pins* — another local account able to
// rewrite it makes the client trust an impersonating listener. On Windows the
// mode bit alone does not say that (a new file inherits its parent
// directory's ACL), which is the whole reason generateAndWriteToken calls
// fsguard for daemon.token.
func TestEnsureTLSCertRestrictsBothHalves(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "daemon.crt")
	keyPath := filepath.Join(dir, "daemon.key")

	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, nil))
	if _, err := ensureTLSCert(certPath, keyPath, logger); err != nil {
		t.Fatalf("ensureTLSCert: %v", err)
	}

	for _, p := range []string{certPath, keyPath} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatalf("stat %s: %v", p, err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 0600", p, info.Mode().Perm())
		}
		// fsguard.RestrictToOwner is a no-op on POSIX and the real control on
		// Windows; calling it again must succeed on a file it has already
		// restricted, which is the observable both platforms share here.
		if err := fsguard.RestrictToOwner(p); err != nil {
			t.Errorf("RestrictToOwner(%s): %v", p, err)
		}
	}

	// First-ever generation is normal and must not warn — only the
	// fingerprint is reported, so the operator has a value to compare later.
	if strings.Contains(logBuf.String(), "level=WARN") {
		t.Errorf("first-ever generation warned, got: %s", logBuf.String())
	}
	if !strings.Contains(logBuf.String(), "fingerprint_sha256=") {
		t.Errorf("no certificate fingerprint reported, got: %s", logBuf.String())
	}
}

// TestEnsureTLSCertWarnsOnRegeneration is the second half of P81.25: deleting
// daemon.crt (or leaving a pair that no longer loads) regenerates silently on
// the next start, changing the identity every pinned client trusts. The
// regeneration itself is what keeps the daemon startable and stays; the
// silence is the defect.
func TestEnsureTLSCertWarnsOnRegeneration(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "daemon.crt")
	keyPath := filepath.Join(dir, "daemon.key")

	first, err := ensureTLSCert(certPath, keyPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("first ensureTLSCert: %v", err)
	}

	// A reload of intact material is silent and returns the same certificate.
	var reloadBuf bytes.Buffer
	again, err := ensureTLSCert(certPath, keyPath, slog.New(slog.NewTextHandler(&reloadBuf, nil)))
	if err != nil {
		t.Fatalf("reload ensureTLSCert: %v", err)
	}
	if certFingerprint(again.Certificate[0]) != certFingerprint(first.Certificate[0]) {
		t.Error("reloading intact material produced a different certificate")
	}
	if strings.Contains(reloadBuf.String(), "level=WARN") {
		t.Errorf("reloading intact material warned, got: %s", reloadBuf.String())
	}

	// Now delete the pinned certificate, as an operator (or anything else)
	// could, and start again.
	if err := os.Remove(certPath); err != nil {
		t.Fatal(err)
	}
	var regenBuf bytes.Buffer
	regen, err := ensureTLSCert(certPath, keyPath, slog.New(slog.NewTextHandler(&regenBuf, nil)))
	if err != nil {
		t.Fatalf("regenerate ensureTLSCert: %v", err)
	}
	if certFingerprint(regen.Certificate[0]) == certFingerprint(first.Certificate[0]) {
		t.Fatal("expected a new certificate after deleting daemon.crt")
	}
	logged := regenBuf.String()
	if !strings.Contains(logged, "level=WARN") || !strings.Contains(logged, "regenerated over existing material") {
		t.Errorf("regeneration over an existing pin did not warn, got: %s", logged)
	}
	// The new fingerprint is in the warning, so the operator can compare it
	// against what their clients pinned.
	if !strings.Contains(logged, certFingerprint(regen.Certificate[0])) {
		t.Errorf("regeneration warning omits the new fingerprint, got: %s", logged)
	}
}

// TestCertFingerprintMatchesOpenSSLForm pins the rendering, since the value's
// only purpose is being compared by eye against
// `openssl x509 -in daemon.crt -noout -fingerprint -sha256`.
func TestCertFingerprintMatchesOpenSSLForm(t *testing.T) {
	der := []byte("not a real certificate, only bytes to hash")
	sum := sha256.Sum256(der)
	got := certFingerprint(der)
	if len(got) != len(sum)*3-1 {
		t.Fatalf("fingerprint %q has length %d, want %d", got, len(got), len(sum)*3-1)
	}
	if !regexp.MustCompile(`^([0-9A-F]{2}:){31}[0-9A-F]{2}$`).MatchString(got) {
		t.Errorf("fingerprint %q is not colon-separated uppercase hex", got)
	}
	if !strings.HasPrefix(got, fmt.Sprintf("%02X:%02X", sum[0], sum[1])) {
		t.Errorf("fingerprint %q does not start with the SHA-256 of the input", got)
	}
}

// TestCertFingerprintFromFileMatchesGeneratedCert is P81.18/FIND-18: the
// exported CertFingerprint (read from disk, used by `aegis ui`) must agree
// with the fingerprint ensureTLSCert logs for the same certificate.
func TestCertFingerprintFromFileMatchesGeneratedCert(t *testing.T) {
	dir := t.TempDir()
	certPath := filepath.Join(dir, "daemon.crt")
	keyPath := filepath.Join(dir, "daemon.key")

	cert, err := ensureTLSCert(certPath, keyPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("ensureTLSCert: %v", err)
	}
	want := certFingerprint(cert.Certificate[0])

	got, err := CertFingerprint(certPath)
	if err != nil {
		t.Fatalf("CertFingerprint: %v", err)
	}
	if got != want {
		t.Errorf("CertFingerprint(%q) = %q, want %q", certPath, got, want)
	}
}

// TestCertFingerprintMissingFile checks the error path a `aegis ui` running
// before any daemon has ever started at this data dir would hit.
func TestCertFingerprintMissingFile(t *testing.T) {
	if _, err := CertFingerprint(filepath.Join(t.TempDir(), "nope.crt")); err == nil {
		t.Error("expected an error for a missing certificate file")
	}
}
