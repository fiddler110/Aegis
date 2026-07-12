package server

import (
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
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
// t.TempDir()'s cleanup doesn't race an open SQLite/bolt handle on Windows
// (New() itself only closes these on its own error paths or during
// ListenAndServe's shutdown, neither of which a plain New()-then-return test
// exercises).
func closeTestServerStores(srv *Server) {
	if srv == nil {
		return
	}
	if srv.knowledge != nil {
		_ = srv.knowledge.Close()
	}
	if srv.longMem != nil {
		_ = srv.longMem.Close()
	}
	srv.store.Close()
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
