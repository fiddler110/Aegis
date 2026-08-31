package server

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/permission"
)

// TestNewRefusesAutoApproveExecWithLocalSandbox is the P25.2 regression test
// for the daemon's startup posture check: auto_approve_exec: true combined
// with an effective local (unsandboxed) backend used to be only a WARN log
// line — easy to miss, and exactly the combo a silent sandbox.backend typo
// (e.g. "podman" pre-P25.2 aliasing) or an unavailable container runtime
// fallback would produce. New() must now refuse to start instead.
func TestNewRefusesAutoApproveExecWithLocalSandbox(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:    dir,
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "auto", AutoApproveExec: true},
		// Sandbox left zero-value -> "local".
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv, err := New(cfg, logger)
	if err == nil {
		closeTestServerStores(srv)
		t.Fatal("expected New to refuse auto_approve_exec + local sandbox, got nil error")
	}
	if !strings.Contains(err.Error(), "allow_unsandboxed_auto_exec") {
		t.Errorf("error %q should name the opt-out", err.Error())
	}
}

// TestNewAllowsAutoApproveExecWithLocalSandboxWhenOptedOut verifies the
// explicit escape hatch actually works, and only downgrades to a warning.
func TestNewAllowsAutoApproveExecWithLocalSandboxWhenOptedOut(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:  dir,
		Provider: config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{
			Mode: "auto", AutoApproveExec: true, AllowUnsandboxedAutoExec: true,
		},
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	srv, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New: unexpected error with allow_unsandboxed_auto_exec set: %v", err)
	}
	defer closeTestServerStores(srv)
}

// closeTestServerStores releases every on-disk store New() opens, so
// t.TempDir()'s cleanup doesn't race an open SQLite/bolt handle on Windows.
// It is now a thin alias for Server.Close — kept as a name because the call
// sites read better with it, but deliberately *not* a second teardown list:
// the reason this helper existed (New opens handles that only ListenAndServe
// ever let go of) is exactly what Close fixed, and a private copy would drift
// from it the next time a store is added.
func closeTestServerStores(srv *Server) {
	if srv == nil {
		return
	}
	_ = srv.Close(context.Background())
}

// TestNewWarnsLocalSandboxBuildMode is the P27.14/FIND-04 regression test:
// the default install (mode: build, sandbox.backend unset -> local) used to
// log nothing at all about running unconfined on the host — only the auto
// mode and auto_approve_exec cases below got a startup warning. New() must
// now log a persistent recommendation to use "os" or "container" any time
// the local backend is reachable with execute-capable tools (i.e. mode !=
// plan), not just those two sharper cases.
func TestNewWarnsLocalSandboxBuildMode(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:    dir,
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "build"},
		// Sandbox left zero-value -> "local".
	}
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	srv, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	defer closeTestServerStores(srv)

	out := buf.String()
	if !strings.Contains(out, "sandbox backend is 'local'") {
		t.Errorf("expected a local-sandbox recommendation warning in build mode, got:\n%s", out)
	}
	if !strings.Contains(out, "sandbox.backend: container") {
		t.Errorf("expected the warning to name sandbox.backend: container as a fix, got:\n%s", out)
	}
}

// TestNewSkipsLocalSandboxWarningInPlanMode verifies the new persistent
// warning is gated on execute capability actually being reachable: plan mode
// denies shell/execute tool calls entirely, so warning about the local
// backend there would be noise.
func TestNewSkipsLocalSandboxWarningInPlanMode(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		DataDir:    dir,
		Provider:   config.ProviderConfig{Model: "test", MaxTokens: 100},
		Permission: config.PermissionConfig{Mode: "plan"},
	}
	var buf strings.Builder
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	srv, err := New(cfg, logger)
	if err != nil {
		t.Fatalf("New: unexpected error: %v", err)
	}
	defer closeTestServerStores(srv)

	if out := buf.String(); strings.Contains(out, "sandbox backend is 'local'") {
		t.Errorf("expected no local-sandbox warning in plan mode, got:\n%s", out)
	}
}

// TestUnsandboxedAutoExecError covers the extracted decision function
// directly (New() only calls it once sb is already known to be the local
// backend — a real container/OS backend isn't reliably available in test
// environments, so the "non-local backend never trips this" half of the
// contract is the `if _, isLocal := ...; isLocal` gate in New() itself, not
// this function).
func TestUnsandboxedAutoExecError(t *testing.T) {
	// Not opted out: refuses, and the message surfaces the fallback reason
	// when the local backend was reached via a fallback rather than being
	// explicitly configured.
	err := unsandboxedAutoExecError(
		config.PermissionConfig{AutoApproveExec: true},
		"container", true, "configured sandbox backend \"container\" unavailable — running unsandboxed on the host",
	)
	if err == nil {
		t.Fatal("expected an error when allow_unsandboxed_auto_exec is unset")
	}
	if !strings.Contains(err.Error(), "unavailable") {
		t.Errorf("error %q should surface the fallback reason", err.Error())
	}

	// Opted out: no error.
	err = unsandboxedAutoExecError(
		config.PermissionConfig{AutoApproveExec: true, AllowUnsandboxedAutoExec: true},
		"local", false, "",
	)
	if err != nil {
		t.Errorf("expected no error when allow_unsandboxed_auto_exec is set, got %v", err)
	}
}

// TestUnsandboxedAutoExecErrorCoversAutoMode is the P66.1/SEC-09 regression.
// `permission.mode: auto` makes permission.Policy.Decide return Allow for
// CapExecute with no prompt — the same unattended host execution
// auto_approve_exec buys — but the startup refusal was keyed on
// auto_approve_exec alone, leaving auto mode as a WARN. That was the step that
// let SEC-01's payload be config alone with no second flag needed, so it
// closes with SEC-01 rather than after it.
func TestUnsandboxedAutoExecErrorCoversAutoMode(t *testing.T) {
	err := unsandboxedAutoExecError(
		config.PermissionConfig{Mode: string(permission.ModeAuto)},
		"local", false, "",
	)
	if err == nil {
		t.Fatal("permission.mode auto on the local backend must refuse startup, not warn")
	}
	if !strings.Contains(err.Error(), "auto") {
		t.Errorf("error %q should name the setting that triggered it", err.Error())
	}

	// The same opt-out governs both spellings — an operator who has already
	// acknowledged unsandboxed auto-exec is not asked twice.
	if err := unsandboxedAutoExecError(
		config.PermissionConfig{Mode: string(permission.ModeAuto), AllowUnsandboxedAutoExec: true},
		"local", false, "",
	); err != nil {
		t.Errorf("expected no error when allow_unsandboxed_auto_exec is set, got %v", err)
	}

	// Build and plan mode are untouched: they still prompt, so there is
	// nothing here to refuse.
	for _, mode := range []permission.Mode{permission.ModeBuild, permission.ModePlan} {
		if err := unsandboxedAutoExecError(
			config.PermissionConfig{Mode: string(mode)}, "local", false, "",
		); err != nil {
			t.Errorf("mode %q must not refuse startup: %v", mode, err)
		}
	}
}
