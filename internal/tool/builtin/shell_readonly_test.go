package builtin

import (
	"encoding/json"
	"runtime"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

func TestReadOnlyShellCommand(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name    string
		command string
		want    bool
	}{
		// Positive cases (P25.4c acceptance list).
		{"ls with flag", "ls -la", true},
		{"cat file", "cat file.txt", true},
		{"git status", "git status", true},
		{"git log oneline", "git log --oneline", true},
		{"git diff", "git diff", true},
		{"head", "head -n 20 file.txt", true},
		{"tail", "tail -f log.txt", true},
		{"wc", "wc -l file.txt", true},
		{"pwd", "pwd", true},
		{"path-qualified binary", "/bin/cat file.txt", true},
		{"grep", "grep -n foo file.txt", true},
		{"which", "which python3", true},
		{"whoami", "whoami", true},
		{"du", "du -sh .", true},
		{"df", "df -h", true},
		{"git show", "git show HEAD", true},
		{"git blame", "git blame file.txt", true},
		{"git rev-parse", "git rev-parse HEAD", true},
		{"git ls-files", "git ls-files", true},
		{"windows exe suffix", "cat.exe file.txt", true},
		{"leading/trailing whitespace", "  ls -la  ", true},
		{"relative subdir path", "cat sub/dir/file.txt", true},

		// Bypass attempts (must NOT classify as read-only).
		{"redirection", "cat f > /etc/x", false},
		{"redirection input", "cat < /etc/passwd", false},
		{"git config override", "git -c core.pager=sh log", false},
		{"chaining semicolon", "ls; rm -rf /", false},
		{"chaining and", "ls && rm -rf /", false},
		{"pipe", "cat file.txt | sh", false},
		{"backtick substitution", "cat `whoami`", false},
		{"dollar substitution", "cat $(whoami)", false},
		{"background", "ls &", false},
		{"git paginate override", "git --paginate log", false},
		{"git exec override", "git --exec=/bin/sh status", false},

		// P32.1: absolute/traversal path arguments must not classify as
		// read-only, even though the binary itself is on the allowlist —
		// this is the plan-mode host-filesystem-read bypass.
		{"absolute path outside root (unix)", "cat /etc/shadow", false},
		{"parent traversal", "cat ../../../etc/passwd", false},
		{"traversal with flag", "head -n 5 ../../etc/passwd", false},
		{"git diff pathspec escape", "git diff HEAD -- ../../../etc/passwd", false},

		// Negative cases: not on the allowlist at all.
		{"empty command", "", false},
		{"whitespace only", "   ", false},
		{"rm", "rm -rf /tmp/x", false},
		{"write via echo redirect blocked by metachar anyway", "echo hi > out.txt", false},
		{"unknown binary", "python3 script.py", false},
		{"git unknown subcommand", "git push", false},
		{"git no subcommand", "git", false},
		{"curl", "curl https://example.com", false},
		// P40.1: env/printenv dump the daemon's process environment (provider
		// API keys) and must NOT downgrade to read-only — they fall back to the
		// normal CapExecute approval instead of auto-approving under plan mode.
		{"env", "env", false},
		{"printenv", "printenv", false},
		{"printenv single key", "printenv ANTHROPIC_API_KEY", false},
		// P66.3/SEC-04: ps prints the daemon environment (the provider API
		// keys) — the env/printenv leak above reached by another binary — and
		// less/more are pagers that shell out.
		{"ps", "ps aux", false},
		{"ps env dump", "ps auxwwe", false},
		{"less", "less file.txt", false},
		{"more", "more file.txt", false},
		// P66.3/VULN-02, re-decided by P67.8: the binaries with a documented
		// file-writing form are admitted, and it is the *writing form* that is
		// refused now — see TestReadOnlyShellFlagParsing for the full pass.
		{"sort", "sort file.txt", true},
		{"sort output flag", "sort -o out.txt file.txt", false},
		{"tree", "tree", true},
		{"uniq", "uniq file.txt", true},
		{"uniq output positional", "uniq in.txt out.txt", false},
		// ...but grep's -o is --only-matching, and dropping the write-capable
		// binaries must not cost it.
		{"grep only-matching", "grep -o foo file.txt", true},
		{"grep only-matching attached", "grep -ofoo file.txt", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := readOnlyShellCommand(root, c.command); got != c.want {
				t.Errorf("readOnlyShellCommand(%q) = %v, want %v", c.command, got, c.want)
			}
		})
	}
}

// TestReadOnlyShellCommandWindowsPaths covers P32.1's windows-drive-letter
// case, which filepath.IsAbs only recognizes as absolute on windows itself
// (on unix/darwin a backslash string is just an opaque relative filename).
func TestReadOnlyShellCommandWindowsPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-drive-letter absolute paths only apply on windows")
	}
	root := t.TempDir()
	cases := []struct {
		name    string
		command string
		want    bool
	}{
		{"absolute path outside root", `Get-Content C:\Users\x\.ssh\id_rsa`, false},
		{"powershell absolute path", `Get-ChildItem C:\Windows\System32`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := readOnlyShellCommand(root, c.command); got != c.want {
				t.Errorf("readOnlyShellCommand(%q) = %v, want %v", c.command, got, c.want)
			}
		})
	}
}

// TestShellToolCapabilityFor exercises the tool.CapabilityOverrider seam
// end-to-end through the shell tool, including that tool.EffectiveCapability
// picks it up.
func TestShellToolCapabilityFor(t *testing.T) {
	st := newShellTool(t.TempDir(), 5, nil, nil)

	readInput := json.RawMessage(`{"command":"git status"}`)
	if got := st.CapabilityFor(readInput); got != tool.CapRead {
		t.Errorf("expected CapRead for read-only command, got %s", got)
	}
	if got := tool.EffectiveCapability(st, readInput); got != tool.CapRead {
		t.Errorf("EffectiveCapability: expected CapRead, got %s", got)
	}

	execInput := json.RawMessage(`{"command":"rm -rf /tmp/x"}`)
	if got := st.CapabilityFor(execInput); got != tool.CapExecute {
		t.Errorf("expected CapExecute for non-read-only command, got %s", got)
	}
	if got := tool.EffectiveCapability(st, execInput); got != tool.CapExecute {
		t.Errorf("EffectiveCapability: expected CapExecute, got %s", got)
	}

	// A tool with no CapabilityOverrider falls back to its static Capability.
	other := &readTool{root: t.TempDir()}
	if got := tool.EffectiveCapability(other, nil); got != other.Capability() {
		t.Errorf("EffectiveCapability: expected fallback to static Capability(), got %s want %s", got, other.Capability())
	}

	// The static Capability() itself is unchanged — still CapExecute
	// regardless of input — so anything relying on the old behavior (e.g.
	// WarnUnmatchableRules, which has no per-call input) still sees "shell"
	// as an execute-capability tool.
	if st.Capability() != tool.CapExecute {
		t.Errorf("expected static Capability() to remain CapExecute, got %s", st.Capability())
	}
}
