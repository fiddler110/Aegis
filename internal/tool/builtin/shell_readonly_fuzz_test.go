package builtin

import (
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/sandbox"
	"github.com/fiddler110/aegis/internal/tool"
)

// FuzzClassifyShellCommand is L1. classifyShellCommand is ~1,080 lines of
// hand-written argument parsing across three shell dialects, and it is the
// single largest piece of security-critical parsing in the tree: it is the
// function that decides whether a `shell` call skips the one approval-bearing
// capability in the default posture. Both CRIT-1 (a tilde the classifier never
// expanded) and CRIT-2 (an argv0 it never confined) were table-driven tests
// away from being found and were not found, because a table only contains the
// spellings someone thought of.
//
// The property asserted is the one a downgrade actually promises:
//
//	a CapRead (or CapNetwork) verdict implies nothing outside root is
//	touched, and no binary from inside root is executed.
//
// It is checked against the *tokens the shell will build* — splitShellWords in
// the same dialect the verdict was reached in — rather than against a second
// parser, because the whole class of defect here is the classifier and the
// shell disagreeing about what a token is.
func FuzzClassifyShellCommand(f *testing.F) {
	for _, seed := range []string{
		"ls -la",
		"cat notes.txt",
		"git status --short",
		"git diff --output=/tmp/x",
		"cat ~/.ssh/id_rsa",
		"./scripts/ls",
		"/bin/cat file.txt",
		"cat '/etc/passwd'",
		`cat \/etc/passwd`,
		"grep -f/etc/shadow foo",
		"sort -o out.txt in.txt",
		`Get-Content -Path:C:\Windows\win.ini`,
		"gh pr list",
		"echo hi > out.txt",
		"cat a; rm -rf /",
		"cat -- -weird-name",
		"",
		"   ",
		"--",
		"~",
		// P81.20/FIND-20: every previously-fixed escape gets a named seed
		// here, not just a representative one, so a future regression in any
		// of them is one fuzz corpus entry away from being caught again
		// rather than needing to be refuzzed into existence.
		//
		// CRIT-1 (a tilde the classifier never expanded, so it read as an
		// ordinary relative name and confined "happily" inside root).
		"cat ~root/.bashrc",
		"ls ~",
		"grep --file=~/.ssh/id_rsa foo",
		// CRIT-2 (an unconfined argv0: baseBinaryName reduced a
		// path-qualified binary to a bare name that matched the table, while
		// confinement was only ever checked against fields[1:]).
		"../ls",
		"~/x/ls",
		"./scripts/cat notes.txt",
		// P79.1 (Windows absolute-path escapes through the attached-flag and
		// PowerShell operand-path spellings — reachable through
		// shellTool.CapabilityFor end to end, see
		// TestShellToolCapabilityForRejectsWindowsAbsolutePathEscapes).
		`Get-Content C:\Users\x\.ssh\id_rsa`,
		`Get-Content -Path C:\Users\x\.ssh\id_rsa`,
		`Get-Content -Path:C:\Windows\System32\drivers\etc\hosts`,
		`Get-ChildItem C:\Windows\System32`,
	} {
		for _, ps := range []bool{false, true} {
			f.Add(seed, ps)
		}
	}

	root := f.TempDir()
	f.Fuzz(func(t *testing.T, command string, powershell bool) {
		cap, ok := classifyShellCommand(root, command, powershell)

		// Determinism: the verdict is a permission decision, and a decision
		// that varies between two identical questions cannot be audited.
		if cap2, ok2 := classifyShellCommand(root, command, powershell); cap != cap2 || ok != ok2 {
			t.Fatalf("classification is not deterministic for %q: (%v,%v) then (%v,%v)", command, cap, ok, cap2, ok2)
		}
		if !ok {
			// Not classified: the call keeps its CapExecute approval, which is
			// always a safe answer. The only thing to check is that the
			// capability reported alongside a false verdict stays CapExecute,
			// since callers read the pair.
			if cap != tool.CapExecute {
				t.Fatalf("unclassified command %q reported capability %q, want %q", command, cap, tool.CapExecute)
			}
			return
		}
		if cap == tool.CapExecute {
			t.Fatalf("a classified command must not be CapExecute: %q", command)
		}

		// From here the command was granted the downgrade, so the invariant
		// has to hold.
		// Trim first, exactly as the classifier does: surrounding whitespace is
		// not part of any token — a leading or trailing newline chains nothing
		// — and re-splitting the untrimmed string produces an argv the shell
		// will never build.
		trimmed := strings.TrimSpace(command)
		if strings.ContainsAny(trimmed, permission.ShellChainMetaChars) {
			t.Fatalf("downgraded a command containing a chaining metacharacter: %q", command)
		}
		fields, split := splitShellWords(trimmed, !powershell)
		if !split || len(fields) == 0 {
			t.Fatalf("downgraded %q, which does not split into an argv", command)
		}
		// CRIT-2: token zero must be a bare command name resolved through PATH.
		// A path-qualified argv0 names a file, and a file inside the workspace
		// is attacker-supplied in exactly the threat model this gate exists for.
		if argv0 := fields[0]; strings.ContainsAny(argv0, `/\`) ||
			strings.HasPrefix(argv0, "~") || sandbox.IsRooted(argv0) {
			t.Fatalf("downgraded %q, whose argv0 %q names a file rather than a PATH command", command, argv0)
		}
		// CRIT-1 and P32.1: every path the argv carries must resolve inside
		// root, and must not be one the shell expands out of it first.
		for _, arg := range fields[1:] {
			for _, candidate := range argvPathCandidates(arg) {
				if strings.HasPrefix(candidate, "~") {
					t.Fatalf("downgraded %q, whose argument %q the shell expands to a home directory", command, arg)
				}
				if _, err := sandbox.ValidatePath(root, candidate); err != nil {
					t.Fatalf("downgraded %q, whose argument %q resolves outside the root: %v", command, arg, err)
				}
			}
		}
	})
}
