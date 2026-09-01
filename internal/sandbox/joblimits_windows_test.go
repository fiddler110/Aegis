//go:build windows

package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestParseMemoryLimit(t *testing.T) {
	cases := []struct {
		in     string
		want   int64
		wantOK bool
	}{
		{"", 0, false},
		{"   ", 0, false},
		{"4G", 4 << 30, true},
		{"512M", 512 << 20, true},
		{"1024K", 1024 << 10, true},
		{"1000B", 1000, true},
		{"1000", 1000, true},
		{"0", 0, false},
		{"-5M", 0, false},
		{"not-a-number", 0, false},
	}
	for _, c := range cases {
		got, ok := parseMemoryLimit(c.in)
		if ok != c.wantOK || (ok && got != c.want) {
			t.Errorf("parseMemoryLimit(%q) = (%d, %v), want (%d, %v)", c.in, got, ok, c.want, c.wantOK)
		}
	}
}

// TestNewResourceLimiterEmptyLimitsIsNoop verifies a zero ResourceLimits
// builds no job object at all — the common case (no sandbox.limits
// configured) must not pay for one.
func TestNewResourceLimiterEmptyLimitsIsNoop(t *testing.T) {
	l, err := newResourceLimiter(ResourceLimits{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if l != nil {
		t.Error("expected nil limiter for empty limits")
	}
	// Nil-receiver methods must be safe no-ops (LocalBackend.run relies on this).
	if err := l.assign(nil); err != nil {
		t.Errorf("nil limiter assign must be a no-op, got %v", err)
	}
	if err := l.Close(); err != nil {
		t.Errorf("nil limiter Close must be a no-op, got %v", err)
	}
}

// TestResourceLimiterSupportedOnWindows pins the platform split: Windows
// reports support, so sandbox selection does not warn that a configured cap
// goes unenforced here.
func TestResourceLimiterSupportedOnWindows(t *testing.T) {
	if !ResourceLimiterSupported() {
		t.Error("expected ResourceLimiterSupported() to be true on windows")
	}
}

// TestLocalBackendWithLimitsStillRunsCommands is an integration check that a
// configured memory/PID cap doesn't break ordinary command execution — the
// job-object plumbing (CreateJobObject, SetInformationJobObject,
// AssignProcessToJobObject, Close) runs on every call once limits are set, so
// a wiring mistake there would otherwise only surface as "shell calls
// mysteriously fail once sandbox.limits is set".
func TestLocalBackendWithLimitsStillRunsCommands(t *testing.T) {
	b := NewLocalBackend().WithLimits(ResourceLimits{Memory: "256M", PIDs: 64})
	out, err := b.Exec(context.Background(), "Write-Output hello-from-job-object",
		ExecOpts{Dir: t.TempDir(), Timeout: 10 * time.Second})
	if err != nil {
		t.Fatalf("exec under a resource-limited job object failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "hello-from-job-object") {
		t.Errorf("expected command output, got %q", out)
	}
}
