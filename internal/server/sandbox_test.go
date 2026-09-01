package server

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestSelectSandboxFallsBackToLocal verifies the default (non-strict)
// behavior: an unavailable container runtime cascades to the OS backend
// (seatbelt/bwrap) before giving up to unsandboxed local, and reports the
// fallback either way so callers can surface it (P7.4). Which tier it lands
// on is host-dependent — this machine may or may not have an OS sandbox
// mechanism — so the expectation is derived from the same NewOSBackend call
// SelectSandbox itself makes, rather than assumed.
func TestSelectSandboxFallsBackToLocal(t *testing.T) {
	dir := t.TempDir()
	cfg := config.SandboxConfig{Backend: "container", Runtime: "bogus-runtime-does-not-exist"}
	sb, fallback, reason, err := SelectSandbox(cfg, dir, discardLogger())
	if err != nil {
		t.Fatalf("selectSandbox: unexpected error: %v", err)
	}
	if !fallback {
		t.Error("expected fallback = true for an unavailable container runtime")
	}
	if reason == "" {
		t.Error("expected a non-empty fallback reason")
	}

	if _, oerr := sandbox.NewOSBackend(dir, cfg.Network, cfg.StripEnv, cfg.OSExtraReadPaths); oerr == nil {
		// OSBackend.Name() is "os:" + mechanism ("os:seatbelt", "os:bubblewrap"),
		// never a bare "os", so this must match on the prefix.
		if sb == nil || !strings.HasPrefix(sb.Name(), "os:") {
			t.Errorf("expected cascade to the os backend on a host with OS sandboxing available, got %v", sb)
		}
	} else {
		if sb == nil || sb.Name() != "local" {
			t.Errorf("expected local backend when OS sandboxing is also unavailable, got %v", sb)
		}
	}
}

// TestSelectSandboxStrictHardFails verifies that sandbox.strict (the default
// as of P81.22/FIND-22) turns a would-be fallback to *unsandboxed local* into
// a startup error — but, per SelectSandbox's doc comment, only guards the
// last step of the cascade: landing on a still-real OS-level sandbox instead
// of the named container runtime is not what strict refuses. Which of those
// two this test observes is host-dependent (same reasoning as
// TestSelectSandboxFallsBackToLocal), so the expectation is derived from the
// same NewOSBackend call SelectSandbox itself makes.
func TestSelectSandboxStrictHardFails(t *testing.T) {
	dir := t.TempDir()
	cfg := config.SandboxConfig{Backend: "container", Runtime: "bogus-runtime-does-not-exist", Strict: true}
	sb, fallback, _, err := SelectSandbox(cfg, dir, discardLogger())

	if _, oerr := sandbox.NewOSBackend(dir, cfg.Network, cfg.StripEnv, cfg.OSExtraReadPaths); oerr == nil {
		// A real OS-level sandbox is available on this host: strict must not
		// refuse the cascade to it, since that is still real isolation.
		if err != nil {
			t.Fatalf("expected no error when OS-level sandboxing is available under strict, got %v", err)
		}
		if sb == nil || !strings.HasPrefix(sb.Name(), "os:") {
			t.Errorf("expected cascade to the os backend under strict, got %v", sb)
		}
		if !fallback {
			t.Error("expected fallback = true (a different backend than the one named was selected)")
		}
		return
	}

	// Neither the container runtime nor an OS-level sandbox is available: this
	// is the "no real isolation" case strict exists to refuse.
	if err == nil {
		t.Fatal("expected an error when sandbox.strict is set and no isolation backend is available")
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

// TestStrictUnavailableErrIsActionable is the P81.22/FIND-22 regression for
// the error an operator on a host with neither a container runtime nor
// OS-level isolation actually sees now that sandbox.strict defaults to true
// — this is exactly the maintainer's own Windows dev box. The message must
// name a concrete next step, not just wrap the underlying errors.
func TestStrictUnavailableErrIsActionable(t *testing.T) {
	err := strictUnavailableErr("container", errors.New("no container runtime found"), errors.New("no OS sandbox available"))
	if err == nil {
		t.Fatal("expected a non-nil error")
	}
	msg := err.Error()
	for _, want := range []string{"sandbox.strict", "sandbox.backend", "no container runtime found", "no OS sandbox available"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error message missing %q: %s", want, msg)
		}
	}
	// Must give an actionable next step, not just describe the failure.
	if !strings.Contains(msg, "sandbox.strict: false") && !strings.Contains(msg, "install") {
		t.Errorf("error message has no actionable next step: %s", msg)
	}
}

// TestSelectSandboxLocalAppliesEnvAllow verifies SelectSandbox wires
// sandbox.env_allow through to the local backend (P81.26/FIND-26) —
// previously only the container backend ever saw an allowlist at all, and
// the local backend's own default allowlist has no way to widen without this
// wiring.
func TestSelectSandboxLocalAppliesEnvAllow(t *testing.T) {
	t.Setenv("MY_PROJECT_VAR", "present")
	t.Setenv("SOME_OTHER_VAR", "absent")

	cfg := config.SandboxConfig{Backend: "local", EnvAllow: []string{"MY_PROJECT_VAR"}}
	sb, _, _, err := SelectSandbox(cfg, t.TempDir(), discardLogger())
	if err != nil {
		t.Fatalf("selectSandbox: unexpected error: %v", err)
	}

	var command string
	if runtime.GOOS == "windows" {
		command = "Get-ChildItem Env: | ForEach-Object { \"$($_.Name)=$($_.Value)\" }"
	} else {
		command = "env"
	}
	out, err := sb.Exec(context.Background(), command, sandbox.ExecOpts{Dir: t.TempDir(), Timeout: 5 * time.Second})
	if err != nil {
		t.Fatalf("exec error: %v", err)
	}
	if !strings.Contains(out, "MY_PROJECT_VAR") {
		t.Errorf("expected sandbox.env_allow entry to reach the local backend:\n%s", out)
	}
	if strings.Contains(out, "SOME_OTHER_VAR") {
		t.Errorf("expected a non-allowlisted var to stay excluded:\n%s", out)
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
