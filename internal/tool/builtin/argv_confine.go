package builtin

import (
	"fmt"
	"strings"

	"github.com/fiddler110/aegis/internal/sandbox"
)

// P66.3 — one argv path-confinement path for the read-only tier.
//
// Two entry points hand a model-chosen argv to the host under tool.CapRead,
// which permission.Policy.Decide allows *silently in every mode*, plan mode
// included: the dedicated `git` tool (git.go) and the shell tool's read-only
// classifier (shell_readonly.go), which downgrades a recognised command from
// CapExecute to CapRead. They must reach the same verdict for the same argv,
// and they did not — each carried its own git flag denylist, so `--no-index`
// was missing from one and `--output` from the other, and only one of them
// confined paths at all. All three escapes the review found were that
// divergence rather than any single missing entry:
//
//   - VULN-01: `git diff --no-index -- /dev/null <abs path>` through the git
//     tool, which path-validated no operand whatsoever.
//   - VULN-11: `shell("git diff --output=<abs path>")`, where the classifier
//     skipped every token starting with "-" unexamined.
//   - VULN-02: `sort -o<abs path>` / `sort --output=<abs path>`, the same
//     skipped-flag rule on a non-git binary.
//
// Everything the two paths share lives here, and argv_confine_test.go pins the
// agreement with a {git-tool argv, equivalent shell string} table.

// deniedGitFlags is the single union of the two former denylists
// (git.go's deniedGitArgPrefixes and shell_readonly.go's
// gitConfigOverrideFlags), plus --no-index. Rejected wherever they appear.
var deniedGitFlags = []string{
	// Redirect git through an external program — pagers, diff/merge drivers,
	// transport helpers — turning a read-only subcommand into code execution.
	"-c", "--config", "--exec", "--exec-path", "--paginate",
	"--upload-pack", "--receive-pack", "--ext-diff", "--open-files-in-pager",
	// Relocate git's idea of the repository or the working tree, which moves
	// the whole invocation outside the workspace before any operand is read.
	"-C", "--git-dir", "--work-tree",
	// Write a file. --output is the VULN-11 escape: it needs no path operand
	// at all, so operand confinement alone has nothing to reject.
	"-o", "--output",
	// VULN-01. --no-index turns `git diff` into a plain two-file differ with
	// no repository involved, so it reads any two paths the daemon can open.
	"--no-index",
}

// -p is deliberately absent from that union even though the shell classifier
// used to deny it. -p is the pager alias (an external program) only in the
// pre-subcommand position, and neither call path can reach that position: the
// git tool takes the subcommand as its own field and prepends it, and the
// shell classifier requires the first token after "git" to be an allowlisted
// subcommand. Post-subcommand, -p is --patch and is read-only. --paginate
// stays on the list because it has no post-subcommand meaning to lose.

// flagMatches reports whether arg spells flag in any of its forms: the bare
// flag, the "--flag=value" form, and — for a one-letter short flag — the
// attached "-ovalue" form that the old exact/"=" comparison missed entirely.
func flagMatches(arg, flag string) bool {
	if arg == flag || strings.HasPrefix(arg, flag+"=") {
		return true
	}
	if len(flag) == 2 && flag[0] == '-' && flag[1] != '-' {
		return len(arg) > 2 && strings.HasPrefix(arg, flag)
	}
	return false
}

// deniedGitFlag returns the entry of deniedGitFlags that arg spells, or "".
func deniedGitFlag(arg string) string {
	for _, f := range deniedGitFlags {
		if flagMatches(arg, f) {
			return f
		}
	}
	return ""
}

// argvPathCandidates returns the substrings of arg that have to be confined to
// the workspace root. A bare operand is one. A flag is not — except that an
// *attached value* is a path in a flag's clothing: "--output=/etc/x" and
// "-o/etc/x" are both a path, and skipping every token starting with "-" is
// exactly what let VULN-02 and VULN-11 through. The separated spelling
// ("-o /etc/x") needs no case of its own: its value is a bare operand on the
// next iteration.
//
// Values that are not paths at all (a revision, a --grep= pattern, a
// --pretty= format) resolve as relative names inside the root and pass, which
// is the conservative direction — a false positive here only costs the call
// its CapRead downgrade, while a false negative auto-approves a host read.
func argvPathCandidates(arg string) []string {
	switch {
	case arg == "" || arg == "-" || arg == "--":
		return nil
	case !strings.HasPrefix(arg, "-"):
		return []string{arg}
	}
	if _, val, ok := strings.Cut(arg, "="); ok {
		if val == "" {
			return nil
		}
		return []string{val}
	}
	// Short-option cluster with an attached value: "-o/etc/x" is "-o /etc/x".
	if !strings.HasPrefix(arg, "--") && len(arg) > 2 {
		return []string{arg[2:]}
	}
	return nil
}

// firstArgvEscape returns the first argument in args carrying a path that
// resolves outside root (via sandbox.ValidatePath, the same root confinement
// read_file/grep/glob use), or "" when every one is confined.
func firstArgvEscape(root string, args []string) string {
	for _, a := range args {
		for _, c := range argvPathCandidates(a) {
			if _, err := sandbox.ValidatePath(root, c); err != nil {
				return a
			}
		}
	}
	return ""
}

// argvStaysInRoot reports whether every path an argv carries — operand or
// attached flag value — resolves within root.
func argvStaysInRoot(root string, args []string) bool {
	return firstArgvEscape(root, args) == ""
}

// validateReadOnlyGitArgv applies everything the two read-only git paths must
// agree on to one argument vector (subcommand first): the union flag denylist
// and path confinement of every operand and attached flag value.
//
// It deliberately does not check the subcommand allowlist. The two paths admit
// different subcommand sets on purpose — the git tool permits branch, tag,
// remote and stash and guards their mutating shapes in rejectMutatingReadArgs,
// while the shell classifier has no such guard and so admits only subcommands
// that are read-only for every argument combination.
func validateReadOnlyGitArgv(root string, argv []string) error {
	if len(argv) == 0 {
		return fmt.Errorf("a git subcommand is required")
	}
	if len(argv) > maxGitArgs {
		return fmt.Errorf("too many arguments (%d, max %d)", len(argv), maxGitArgs)
	}
	for _, a := range argv {
		if bad := deniedGitFlag(a); bad != "" {
			return fmt.Errorf("argument %q is not allowed (%s can redirect git through an external program, relocate the repository, or write a file)", a, bad)
		}
	}
	if bad := firstArgvEscape(root, argv[1:]); bad != "" {
		return fmt.Errorf("argument %q resolves outside the workspace", bad)
	}
	return nil
}
