package server

import (
	"context"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/config"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// fakeNonLocalBackend stands in for a real container/OS backend in tests —
// only its type (not *sandbox.LocalBackend) matters to cronAutoApproveGuard.
type fakeNonLocalBackend struct{}

func (fakeNonLocalBackend) Name() string { return "container:fake" }
func (fakeNonLocalBackend) Exec(context.Context, string, sandbox.ExecOpts) (string, error) {
	return "", nil
}
func (fakeNonLocalBackend) ExecStreaming(context.Context, string, sandbox.ExecOpts, func(string)) error {
	return nil
}
func (fakeNonLocalBackend) Close() error { return nil }

// TestCronAutoApproveGuardRefusesOnUnsandboxedLocal is the P81.23/FIND-23
// regression: cron_create's auto_approve must be refused when the effective
// sandbox backend is unsandboxed local execution, mirroring
// unsandboxedAutoExecError's own refusal and escape hatch.
func TestCronAutoApproveGuardRefusesOnUnsandboxedLocal(t *testing.T) {
	s := &Server{cfg: &config.Config{}, sandbox: sandbox.NewLocalBackend()}
	err := s.cronAutoApproveGuard()
	if err == nil {
		t.Fatal("expected a refusal for auto_approve on the unsandboxed local backend")
	}
	if !strings.Contains(err.Error(), "unsandboxed") {
		t.Errorf("error %q should name the unsandboxed backend", err.Error())
	}
}

// TestCronAutoApproveGuardAllowsRealBackend confirms a real (non-local)
// backend is never refused.
func TestCronAutoApproveGuardAllowsRealBackend(t *testing.T) {
	s := &Server{cfg: &config.Config{}, sandbox: fakeNonLocalBackend{}}
	if err := s.cronAutoApproveGuard(); err != nil {
		t.Errorf("expected no error for a real sandbox backend, got %v", err)
	}
}

// TestCronAutoApproveGuardHonorsEscapeHatch confirms the same
// permission.allow_unsandboxed_auto_exec opt-out unsandboxedAutoExecError
// honors also silences this guard, rather than requiring the operator to
// acknowledge the risk twice under two different settings.
func TestCronAutoApproveGuardHonorsEscapeHatch(t *testing.T) {
	s := &Server{
		cfg:     &config.Config{Permission: config.PermissionConfig{AllowUnsandboxedAutoExec: true}},
		sandbox: sandbox.NewLocalBackend(),
	}
	if err := s.cronAutoApproveGuard(); err != nil {
		t.Errorf("expected no error when allow_unsandboxed_auto_exec is set, got %v", err)
	}
}
