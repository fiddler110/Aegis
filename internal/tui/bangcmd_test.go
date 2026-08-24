package tui

import (
	"runtime"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// TestBangShellCommandPicksPlatformShell locks in that the `!<command>`
// passthrough (execBangCmd, P30.3) never hardcodes a POSIX "sh" — it must
// branch on runtime.GOOS the same way sandbox.ShellCommand does (the shared
// implementation execBangCmd now calls directly, P77.3), since a native
// Windows host has no guarantee of "sh" on PATH.
func TestBangShellCommandPicksPlatformShell(t *testing.T) {
	const command = `echo hello && echo world`
	shell, args := sandbox.ShellCommand(command)

	if strings.TrimSpace(shell) == "" {
		t.Fatal("sandbox.ShellCommand returned an empty shell binary")
	}
	if len(args) == 0 {
		t.Fatal("sandbox.ShellCommand returned no args")
	}

	if runtime.GOOS == "windows" {
		want := sandbox.WindowsShellBinary()
		if shell != want {
			t.Errorf("shell = %q, want sandbox.WindowsShellBinary() = %q", shell, want)
		}
		wantArgs := []string{"-NoProfile", "-NonInteractive", "-Command", command}
		if len(args) != len(wantArgs) {
			t.Fatalf("args = %#v, want %#v", args, wantArgs)
		}
		for i := range wantArgs {
			if args[i] != wantArgs[i] {
				t.Errorf("args[%d] = %q, want %q", i, args[i], wantArgs[i])
			}
		}
	} else {
		if shell != "/bin/sh" {
			t.Errorf("shell = %q, want /bin/sh", shell)
		}
		wantArgs := []string{"-c", command}
		if len(args) != len(wantArgs) {
			t.Fatalf("args = %#v, want %#v", args, wantArgs)
		}
		for i := range wantArgs {
			if args[i] != wantArgs[i] {
				t.Errorf("args[%d] = %q, want %q", i, args[i], wantArgs[i])
			}
		}
	}

	// The command must appear as exactly one, unmodified argv element,
	// matching the property asserted for sandbox.ShellCommand in
	// internal/security/install_test.go.
	last := args[len(args)-1]
	if last != command {
		t.Errorf("args[last] = %q, want the command unmodified: %q", last, command)
	}
}

// TestBangShellCommandNotHardcodedSh guards specifically against the P30.3
// regression: on Windows, the shell binary must not be the bare POSIX "sh"
// that has no guaranteed presence on PATH.
func TestBangShellCommandNotHardcodedSh(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("this regression only manifests on Windows")
	}
	shell, _ := sandbox.ShellCommand("echo hi")
	if shell == "sh" {
		t.Fatalf("sandbox.ShellCommand returned hardcoded %q on Windows, want a Windows shell (pwsh/powershell)", shell)
	}
}
