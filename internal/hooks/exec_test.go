package hooks

import (
	"context"
	"encoding/json"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// vetoCommand returns a shell command that writes msg to stderr and exits 2,
// in whatever the platform's default hook shell (see sandbox.ShellCommand,
// called from exec.go) actually understands: PowerShell's `1>&2` redirection
// operator is "reserved for future use" and refuses to parse, so this can't
// be one POSIX-only string shared across platforms the way it was before
// P30.2.
func vetoCommand(msg string) string {
	if runtime.GOOS == "windows" {
		return "[Console]::Error.WriteLine('" + msg + "'); exit 2"
	}
	return `echo "` + msg + `" 1>&2; exit 2`
}

func TestExecPreToolUseVeto(t *testing.T) {
	// exit 2 with stderr → veto surfaced to the model.
	h := NewExec([]ExecSpec{{
		Event:   EventPreToolUse,
		Command: vetoCommand("policy: no shell in prod"),
	}}, nil)
	err := h.PreToolUse(context.Background(), "shell", json.RawMessage(`{"cmd":"ls"}`))
	if err == nil {
		t.Fatal("expected veto error")
	}
	if !strings.Contains(err.Error(), "no shell in prod") {
		t.Errorf("stderr not surfaced: %v", err)
	}
}

func TestExecPreToolUseAllows(t *testing.T) {
	// exit 0 → allow; exit 1 → logged but allowed.
	for _, cmd := range []string{`exit 0`, `exit 1`} {
		h := NewExec([]ExecSpec{{Event: EventPreToolUse, Command: cmd}}, nil)
		if err := h.PreToolUse(context.Background(), "read", nil); err != nil {
			t.Errorf("cmd %q: unexpected veto: %v", cmd, err)
		}
	}
}

func TestExecToolFilter(t *testing.T) {
	// Hook scoped to "shell" should not fire (or veto) for "read".
	h := NewExec([]ExecSpec{{
		Event:   EventPreToolUse,
		Command: `exit 2`,
		Tools:   []string{"shell"},
	}}, nil)
	if err := h.PreToolUse(context.Background(), "read", nil); err != nil {
		t.Errorf("filtered hook fired for wrong tool: %v", err)
	}
	if err := h.PreToolUse(context.Background(), "shell", nil); err == nil {
		t.Error("expected veto for matching tool")
	}
}

// TestCapturedStderrCapsOutput is the VULN-10 regression: a hook's stderr
// must not grow unbounded in memory, or in what gets surfaced as the veto
// reason / log field, no matter how much a runaway or hostile hook writes.
func TestCapturedStderrCapsOutput(t *testing.T) {
	c := &capturedStderr{limit: 16}
	n, err := c.Write([]byte("0123456789"))
	if err != nil || n != 10 {
		t.Fatalf("first write: n=%d err=%v", n, err)
	}
	n, err = c.Write([]byte("abcdefghij")) // 10 more; only 6 bytes of room left
	if err != nil || n != 10 {
		t.Fatalf("second write: n=%d err=%v", n, err)
	}
	got := c.String()
	if !strings.HasPrefix(got, "0123456789abcdef") {
		t.Errorf("captured content = %q, want it to start with the first 16 bytes", got)
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("captured content = %q, want a truncation marker", got)
	}
	if len(c.buf.Bytes()) != 16 {
		t.Errorf("underlying buffer holds %d bytes, want capped at 16", len(c.buf.Bytes()))
	}
}

func TestNewExecNilWhenEmpty(t *testing.T) {
	if h := NewExec(nil, nil); h != nil {
		t.Error("expected nil Exec for empty specs")
	}
}

// TestShellCommandPlatformBranch is the P30.2 regression: on Windows hook
// commands must run through pwsh/powershell (there is no POSIX `sh` on a
// native Windows host), everywhere else through /bin/sh -c, matching the
// shared sandbox.ShellCommand convention (P77.3).
func TestShellCommandPlatformBranch(t *testing.T) {
	const command = `echo hello; exit 0`
	shell, args := sandbox.ShellCommand(command)

	if strings.TrimSpace(shell) == "" {
		t.Fatal("sandbox.ShellCommand returned an empty shell binary")
	}
	if len(args) == 0 {
		t.Fatal("sandbox.ShellCommand returned no args")
	}
	last := args[len(args)-1]
	if last != command {
		t.Errorf("args[last] = %q, want the command unmodified: %q", last, command)
	}

	if runtime.GOOS == "windows" {
		if shell != "pwsh" && shell != "powershell" {
			t.Errorf("shell = %q, want pwsh or powershell on windows", shell)
		}
		wantArgs := []string{"-NoProfile", "-NonInteractive", "-Command", command}
		if !reflect.DeepEqual(args, wantArgs) {
			t.Errorf("args = %v, want %v", args, wantArgs)
		}
		return
	}
	if shell != "/bin/sh" {
		t.Errorf("shell = %q, want /bin/sh on %s", shell, runtime.GOOS)
	}
	wantArgs := []string{"-c", command}
	if !reflect.DeepEqual(args, wantArgs) {
		t.Errorf("args = %v, want %v", args, wantArgs)
	}
}
