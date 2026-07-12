package builtin

import (
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

func TestReadOnlyShellCommand(t *testing.T) {
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
		{"windows exe suffix", "cat.exe file.txt", true},
		{"leading/trailing whitespace", "  ls -la  ", true},

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

		// Negative cases: not on the allowlist at all.
		{"empty command", "", false},
		{"whitespace only", "   ", false},
		{"rm", "rm -rf /tmp/x", false},
		{"write via echo redirect blocked by metachar anyway", "echo hi > out.txt", false},
		{"unknown binary", "python3 script.py", false},
		{"git unknown subcommand", "git push", false},
		{"git no subcommand", "git", false},
		{"curl", "curl https://example.com", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := readOnlyShellCommand(c.command); got != c.want {
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
