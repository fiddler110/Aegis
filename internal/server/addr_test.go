package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
)

func TestIsLoopbackAddr(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:4127": true,
		"localhost:4127": true,
		"[::1]:4127":     true,
		"0.0.0.0:4127":   false,
		":4127":          false,
		"10.0.0.5:4127":  false,
		"192.168.1.1:80": false,
	}
	for addr, want := range cases {
		if got := isLoopbackAddr(addr); got != want {
			t.Errorf("isLoopbackAddr(%q) = %v, want %v", addr, got, want)
		}
	}
}

// TestValidateListenAddrRefusesNonLoopbackByDefault is the FIND-08
// regression: a daemon configured to bind a non-loopback address must
// refuse to start unless the operator has explicitly set
// server.allow_remote, since the API is protected only by a bearer token
// with no rate limiting.
func TestValidateListenAddrRefusesNonLoopbackByDefault(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{Addr: "0.0.0.0:4127"}}
	s := &Server{cfg: cfg, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	if err := s.validateListenAddr(); err == nil {
		t.Fatal("expected an error for non-loopback addr without allow_remote, got nil")
	}
}

func TestValidateListenAddrAllowsNonLoopbackWithAllowRemote(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{Addr: "0.0.0.0:4127", AllowRemote: true}}
	s := &Server{cfg: cfg, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	if err := s.validateListenAddr(); err != nil {
		t.Fatalf("expected no error with allow_remote set, got: %v", err)
	}
}

func TestValidateListenAddrAllowsLoopbackByDefault(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{Addr: "127.0.0.1:4127"}}
	s := &Server{cfg: cfg, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}

	if err := s.validateListenAddr(); err != nil {
		t.Fatalf("expected no error for loopback addr, got: %v", err)
	}
}
