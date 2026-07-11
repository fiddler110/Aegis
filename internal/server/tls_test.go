package server

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/client"
	"github.com/fiddler110/aegis/internal/config"
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
	pinned := client.New(addr).WithTLS(cfg.TLSCertPath())
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

// TestListenAndServeTLSDisabledUnchanged pins down that leaving
// server.tls.enabled unset (the default) writes no cert/key files and serves
// plain HTTP exactly as before this feature existed.
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
		t.Fatal("expected tlsCert to stay nil when TLS is disabled (the default)")
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
