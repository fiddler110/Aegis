package server

import (
	"io"
	"log/slog"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSelectSandboxFallsBackToLocal verifies the default (non-strict)
// behavior: an unavailable container runtime falls back to the unsandboxed
// local backend and reports the fallback so callers can surface it (P7.4).
func TestSelectSandboxFallsBackToLocal(t *testing.T) {
	cfg := config.SandboxConfig{Backend: "container", Runtime: "bogus-runtime-does-not-exist"}
	sb, fallback, reason, err := SelectSandbox(cfg, t.TempDir(), discardLogger())
	if err != nil {
		t.Fatalf("selectSandbox: unexpected error: %v", err)
	}
	if !fallback {
		t.Error("expected fallback = true for an unavailable container runtime")
	}
	if reason == "" {
		t.Error("expected a non-empty fallback reason")
	}
	if sb == nil || sb.Name() != "local" {
		t.Errorf("expected local backend, got %v", sb)
	}
}

// TestSelectSandboxStrictHardFails verifies that sandbox.strict turns a would-be
// silent fallback into a startup error instead (P7.4).
func TestSelectSandboxStrictHardFails(t *testing.T) {
	cfg := config.SandboxConfig{Backend: "container", Runtime: "bogus-runtime-does-not-exist", Strict: true}
	sb, fallback, _, err := SelectSandbox(cfg, t.TempDir(), discardLogger())
	if err == nil {
		t.Fatal("expected an error when sandbox.strict is set and the backend is unavailable")
	}
	if sb != nil {
		t.Errorf("expected nil backend on strict failure, got %v", sb)
	}
	if fallback {
		t.Error("expected fallback = false on strict failure")
	}
}

// TestSelectSandboxDefaultIsLocal verifies the zero-value/unset backend still
// runs unsandboxed without being reported as a fallback (that's the intended
// baseline, not a downgrade from a stronger posture the operator asked for).
func TestSelectSandboxDefaultIsLocal(t *testing.T) {
	sb, fallback, reason, err := SelectSandbox(config.SandboxConfig{}, t.TempDir(), discardLogger())
	if err != nil {
		t.Fatalf("selectSandbox: unexpected error: %v", err)
	}
	if fallback {
		t.Error("expected fallback = false for the default local backend")
	}
	if reason != "" {
		t.Errorf("expected empty reason, got %q", reason)
	}
	if sb == nil || sb.Name() != "local" {
		t.Errorf("expected local backend, got %v", sb)
	}
}

// TestSelectSandboxUnknownBackendHardFails is the P25.2 regression test for
// the actual root cause: an unrecognized backend value (e.g. a container
// runtime name typed straight into sandbox.backend, like "podman") must not
// silently reach the unsandboxed local backend the way it used to — it
// should be rejected as an error. config.SandboxConfig.Normalize already
// catches the well-known runtime-name aliases before Load() returns, so
// this exercises SelectSandbox's own defense-in-depth for cfg values built
// outside that path (as this test itself does).
func TestSelectSandboxUnknownBackendHardFails(t *testing.T) {
	cfg := config.SandboxConfig{Backend: "podman"}
	sb, fallback, _, err := SelectSandbox(cfg, t.TempDir(), discardLogger())
	if err == nil {
		t.Fatal("expected an error for an unrecognized sandbox.backend, got nil")
	}
	if sb != nil {
		t.Errorf("expected nil backend on error, got %v", sb)
	}
	if fallback {
		t.Error("expected fallback = false on hard error (this is not a fallback, it's a rejection)")
	}
}
