package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fiddler110/aegis/internal/permission"
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
		// CRIT-2: a path-qualified argv0 is refused the downgrade outright.
		// baseBinaryName reduces "./scripts/ls" to "ls", which hits the
		// read-only table, while argvStaysInRoot is only ever handed
		// fields[1:] — so token zero was never validated as a path at all and
		// a workspace-resident executable ran with no approval, no checkpoint
		// and no exec lock, in every mode including plan. The read-only tier
		// is for a bare name resolved through PATH; even "/bin/cat", which is
		// genuinely the system binary, loses its downgrade, because the
		// classifier cannot tell that spelling from "./scripts/cat".
		{"path-qualified binary", "/bin/cat file.txt", false},
		{"relative path-qualified binary", "./scripts/ls", false},
		{"workspace binary in a subdirectory", "./scripts/cat notes.txt", false},
		{"parent-relative binary", "../ls", false},
		{"home-relative binary", "~/x/ls", false},
		{"bare name still classifies", "cat file.txt", true},
		// CRIT-1: the classifier performs no expansion (sandbox.ValidatePath
		// is lexical), so "~/.ssh/id_rsa" read as a relative name, joined
		// under the root as "<root>/~/.ssh/id_rsa", and validated as confined
		// — while the shell expanded the tilde and read the real key, silently,
		// under plan mode's read gate.
		{"tilde home path", "cat ~/.ssh/id_rsa", false},
		{"bare tilde operand", "ls ~", false},
		{"tilde in an attached flag value", "grep --file=~/.ssh/id_rsa foo", false},
		{"named-user tilde", "cat ~root/.bashrc", false},
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
// Like TestReadOnlyShellPowerShellPathConfinement it names the PowerShell
// dialect rather than inheriting one, for the EXEC-4 reason documented there.
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
			if got := readOnlyShellCommandIn(root, c.command, true); got != c.want {
				t.Errorf("readOnlyShellCommandIn(%q, powershell) = %v, want %v", c.command, got, c.want)
			}
		})
	}
}

// TestShellToolCapabilityForRejectsWindowsAbsolutePathEscapes is the P79.1
// real-path-exploitability check the roadmap entry named as still
// outstanding: TestReadOnlyShellCommandWindowsPaths (above) and the other
// three regression tests exercise readOnlyShellCommand/classifyShellCommand
// directly, never shellTool.CapabilityFor — the actual seam
// permission.Gate.Check consults via tool.EffectiveCapability. This drives
// the exact Windows absolute-path escape shapes through that real seam and
// then through a full plan-mode Gate.Check, confirming the classifier fix
// verified at the unit level actually reaches the production capability
// downgrade path, and that a plan-mode session cannot silently read outside
// its workspace root through it.
func TestShellToolCapabilityForRejectsWindowsAbsolutePathEscapes(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("windows-drive-letter absolute paths only apply on windows")
	}
	root := t.TempDir()
	st := newShellTool(root, 5, nil, nil)

	escapes := []string{
		`Get-Content C:\Users\x\.ssh\id_rsa`,
		`Get-Content -Path C:\Users\x\.ssh\id_rsa`,
		`Get-Content -Path:C:\Windows\System32\drivers\etc\hosts`,
		`Get-ChildItem C:\Windows\System32`,
	}
	for _, cmd := range escapes {
		t.Run(cmd, func(t *testing.T) {
			raw, err := json.Marshal(struct {
				Command string `json:"command"`
			}{cmd})
			if err != nil {
				t.Fatal(err)
			}
			input := json.RawMessage(raw)

			// The classifier must not downgrade this to CapRead.
			if got := st.CapabilityFor(context.Background(), input); got != tool.CapExecute {
				t.Fatalf("CapabilityFor(%q) = %s, want %s — a plan-mode session could read this file with no approval", cmd, got, tool.CapExecute)
			}
			if got := tool.EffectiveCapability(context.Background(), st, input); got != tool.CapExecute {
				t.Fatalf("EffectiveCapability(%q) = %s, want %s", cmd, got, tool.CapExecute)
			}

			// And the production plan-mode gate must therefore deny it
			// outright (plan mode denies CapExecute), never silently allow it
			// the way a CapRead classification would.
			gate := permission.New(permission.ModePlan, denyAllApprover{})
			if allowed, reason := gate.Check(context.Background(), st, input); allowed {
				t.Fatalf("plan-mode gate allowed %q, reason=%q — expected a denial", cmd, reason)
			}
		})
	}
}

// denyAllApprover refuses every Ask decision, so a test using it asserts a
// call was denied outright rather than merely requiring approval.
type denyAllApprover struct{}

func (denyAllApprover) Approve(context.Context, string, string, json.RawMessage) bool { return false }

// TestShellToolCapabilityFor exercises the tool.CapabilityOverrider seam
// end-to-end through the shell tool, including that tool.EffectiveCapability
// picks it up.
func TestShellToolCapabilityFor(t *testing.T) {
	st := newShellTool(t.TempDir(), 5, nil, nil)

	readInput := json.RawMessage(`{"command":"git status"}`)
	if got := st.CapabilityFor(context.Background(), readInput); got != tool.CapRead {
		t.Errorf("expected CapRead for read-only command, got %s", got)
	}
	if got := tool.EffectiveCapability(context.Background(), st, readInput); got != tool.CapRead {
		t.Errorf("EffectiveCapability: expected CapRead, got %s", got)
	}

	execInput := json.RawMessage(`{"command":"rm -rf /tmp/x"}`)
	if got := st.CapabilityFor(context.Background(), execInput); got != tool.CapExecute {
		t.Errorf("expected CapExecute for non-read-only command, got %s", got)
	}
	if got := tool.EffectiveCapability(context.Background(), st, execInput); got != tool.CapExecute {
		t.Errorf("EffectiveCapability: expected CapExecute, got %s", got)
	}

	// A tool with no CapabilityOverrider falls back to its static Capability.
	other := &readTool{root: t.TempDir()}
	if got := tool.EffectiveCapability(context.Background(), other, nil); got != other.Capability() {
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

// TestShellToolCapabilityForUsesTheSessionWorkdir is CRIT-3 at the tool. The
// classification has to be made against the root the call will run in, which is
// tool.WorkdirFromContext when a session set one — the same value Execute
// resolves through effectiveRoot. Before CapabilityOverrider took a context it
// could only see the daemon-wide construction-time root, so a session rooted
// elsewhere was mis-scoped in both directions at once: absolute reads under the
// *daemon's* workspace were silently downgraded for it, and ordinary reads
// inside its own workspace were refused the downgrade.
func TestShellToolCapabilityForUsesTheSessionWorkdir(t *testing.T) {
	daemonRoot := t.TempDir()
	sessionRoot := t.TempDir()
	st := newShellTool(daemonRoot, 5, nil, nil)

	// A path inside the session's own workspace: a read, but only if the
	// classifier is looking at the session's root.
	inSession := shellInput("cat " + filepath.Join(sessionRoot, "notes.txt"))
	sessionCtx := tool.WithWorkdir(context.Background(), sessionRoot)
	if got := st.CapabilityFor(sessionCtx, inSession); got != tool.CapRead {
		t.Errorf("read inside the session workspace = %s, want %s", got, tool.CapRead)
	}
	if got := st.CapabilityFor(context.Background(), inSession); got != tool.CapExecute {
		t.Errorf("the same read is outside the daemon root and must not downgrade there: got %s", got)
	}

	// And the mirror: a path inside the *daemon's* workspace is outside the
	// session's, so the session must not get the silent downgrade for it.
	inDaemon := shellInput("cat " + filepath.Join(daemonRoot, "secret.txt"))
	if got := st.CapabilityFor(sessionCtx, inDaemon); got != tool.CapExecute {
		t.Errorf("read outside the session workspace = %s, want %s", got, tool.CapExecute)
	}
}

// shellInput encodes a shell tool call. Built with json.Marshal rather than
// string concatenation because a Windows path's backslashes are not valid JSON
// escapes, and a hand-spliced literal fails to parse — which the tool reports
// as CapExecute, i.e. as a passing-looking result for the wrong reason.
func shellInput(command string) json.RawMessage {
	b, err := json.Marshal(struct {
		Command string `json:"command"`
	}{Command: command})
	if err != nil {
		panic(err)
	}
	return b
}

// TestEffectiveCapabilityMemoAsksTheToolOnce is M5. The shell classifier does
// filesystem I/O per argv token and the same call's capability is asked for by
// the gate, the round scheduler, the checkpoint decision and the written-path
// bookkeeping — with the whole approval round-trip sitting between the first
// two. Under the per-call memo the tool is consulted once, so those are one
// decision rather than four that a symlink swapped mid-prompt could split.
func TestEffectiveCapabilityMemoAsksTheToolOnce(t *testing.T) {
	counted := &countingCapabilityTool{cap: tool.CapRead}
	ctx := tool.WithCapabilityMemo(context.Background())
	input := json.RawMessage(`{"command":"git status"}`)
	for range 4 {
		if got := tool.EffectiveCapability(ctx, counted, input); got != tool.CapRead {
			t.Fatalf("capability = %s, want %s", got, tool.CapRead)
		}
	}
	if counted.calls != 1 {
		t.Errorf("CapabilityFor called %d times under one call's memo, want 1", counted.calls)
	}

	// A different input is a different call and is classified on its own.
	tool.EffectiveCapability(ctx, counted, json.RawMessage(`{"command":"rm -rf /"}`))
	if counted.calls != 2 {
		t.Errorf("a second distinct input must be classified: calls = %d, want 2", counted.calls)
	}

	// Without a memo nothing is cached — a context that never passed through
	// the engine's toolCtx behaves exactly as it did before M5.
	bare := &countingCapabilityTool{cap: tool.CapRead}
	tool.EffectiveCapability(context.Background(), bare, input)
	tool.EffectiveCapability(context.Background(), bare, input)
	if bare.calls != 2 {
		t.Errorf("without a memo, calls = %d, want 2", bare.calls)
	}
}

// countingCapabilityTool counts how often its per-call classification runs.
type countingCapabilityTool struct {
	cap   tool.Capability
	calls int
}

func (c *countingCapabilityTool) Name() string                { return "counting" }
func (c *countingCapabilityTool) Capability() tool.Capability { return tool.CapExecute }
func (c *countingCapabilityTool) Description() string         { return "counting" }
func (c *countingCapabilityTool) InputSchema() json.RawMessage {
	return schema(`{"type":"object"}`)
}
func (c *countingCapabilityTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}
func (c *countingCapabilityTool) CapabilityFor(context.Context, json.RawMessage) tool.Capability {
	c.calls++
	return c.cap
}
