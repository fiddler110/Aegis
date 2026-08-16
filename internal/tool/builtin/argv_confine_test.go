package builtin

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/tool"
)

// TestReadOnlyGitArgvAgreesAcrossBothPaths is the P66.3 deliverable: a table of
// {git-tool argv, equivalent shell string} pairs asserting that the dedicated
// `git` tool and the shell tool's read-only classifier reach the same verdict
// for the same argv. Divergence between the two — not any single missing
// allowlist entry — is what produced VULN-01, VULN-02 and VULN-11, so this
// test is the thing that keeps them from drifting apart again rather than the
// patch that closed the three known cases.
//
// The shell string is derived from the argv rather than written out beside it,
// so "equivalent" is guaranteed by construction instead of by proofreading.
func TestReadOnlyGitArgvAgreesAcrossBothPaths(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escape.txt")

	cases := []struct {
		name string
		argv []string // subcommand first, as gitTool.Execute assembles it
		want bool     // true: read-only and confined on both paths
	}{
		// The three known escapes. Each must be refused on both paths.
		//
		// VULN-01: --no-index makes `git diff` a plain two-file differ with no
		// repository involved, and the git tool validated no operand at all.
		{"no-index two-file read", []string{"diff", "--no-index", "--", "/dev/null", outside}, false},
		// VULN-11: --output needs no path operand, so operand confinement
		// alone has nothing to reject. This is the cross-platform one — on
		// Windows the shell tool routes through PowerShell, where the VULN-02
		// `sort` case below does not apply but this one still does.
		{"output attached with =", []string{"diff", "--output=" + outside}, false},
		{"output attached short form", []string{"diff", "-o" + outside}, false},
		{"output separated", []string{"diff", "--output", outside}, false},

		// The rest of the union denylist, on both paths.
		{"config override pager", []string{"log", "-c", "core.pager=sh"}, false},
		{"config override attached", []string{"log", "-ccore.pager=sh"}, false},
		{"git-dir relocation", []string{"log", "--git-dir=" + outside}, false},
		{"work-tree relocation", []string{"status", "--work-tree=" + outside}, false},
		{"external diff driver", []string{"diff", "--ext-diff"}, false},
		{"upload-pack", []string{"log", "--upload-pack=sh"}, false},

		// Operand confinement (the check the git tool had none of).
		{"pathspec escape", []string{"diff", "HEAD", "--", outside}, false},
		{"traversal pathspec", []string{"diff", "--", "../../etc/passwd"}, false},

		// Ordinary read-only inspection must survive all of the above.
		{"status short", []string{"status", "--short"}, true},
		{"log oneline", []string{"log", "--oneline"}, true},
		{"log patch", []string{"log", "-p"}, true},
		{"log count", []string{"log", "-n", "20"}, true},
		{"log pretty format", []string{"log", "--pretty=format:%h %s"}, true},
		{"diff in-root pathspec", []string{"diff", "--", "sub/file.txt"}, true},
		{"show a revision", []string{"show", "HEAD"}, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shellCmd := "git " + strings.Join(c.argv, " ")

			gitPath := validateReadOnlyGitArgv(root, c.argv) == nil
			if gitPath != c.want {
				t.Errorf("git tool: validateReadOnlyGitArgv(%q) = %v, want %v", c.argv, gitPath, c.want)
			}
			shellPath := readOnlyShellCommand(root, shellCmd)
			if shellPath != c.want {
				t.Errorf("shell tool: readOnlyShellCommand(%q) = %v, want %v", shellCmd, shellPath, c.want)
			}
			if gitPath != shellPath {
				t.Errorf("the two read-only paths disagree on %q: git tool %v, shell %v", c.argv, gitPath, shellPath)
			}
		})
	}
}

// TestReadOnlyTierRefusesEscapesInPlanMode states the property in the terms
// that make it a security bug rather than a style one: plan mode is the mode an
// operator picks *because* they do not trust what the model will do, and
// permission.Policy.Decide allows CapRead silently in every mode.
//
// The two paths refuse differently, and that asymmetry is real rather than an
// oversight: the shell tool refuses by *classification* (it declines the
// CapRead downgrade, so plan mode denies the resulting CapExecute), while the
// git tool is statically CapRead and is therefore always reached — it must
// refuse inside Execute, before git runs.
func TestReadOnlyTierRefusesEscapesInPlanMode(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "escape.txt")
	plan := permission.Policy{Mode: permission.ModePlan}

	escapes := []struct {
		name    string
		argv    []string
		command string // non-git escapes have no git-tool equivalent
	}{
		{name: "git diff --no-index", argv: []string{"diff", "--no-index", "--", "/dev/null", outside}},
		{name: "git diff --output=", argv: []string{"diff", "--output=" + outside}},
		// VULN-02. `sort` is off the argv0 allowlist entirely (it has a
		// documented file-writing form, so no argument parsing makes it
		// read-only), and the attached-value confinement would refuse the
		// escaping path even if it were not.
		{name: "sort --output=", command: "sort --output=" + outside + " in.txt"},
		{name: "sort -o attached", command: "sort -o" + outside + " in.txt"},
		// SEC-04: `ps auxwwe` prints the daemon's environment, i.e. the
		// provider API keys, which is exactly what env/printenv are excluded
		// for.
		{name: "ps auxwwe", command: "ps auxwwe"},
		{name: "less", command: "less notes.txt"},
		{name: "more", command: "more notes.txt"},
	}

	st := newShellTool(root, 5, nil, nil)
	for _, c := range escapes {
		t.Run(c.name, func(t *testing.T) {
			command := c.command
			if command == "" {
				command = "git " + strings.Join(c.argv, " ")
			}

			// Shell path: no CapRead downgrade, so plan mode denies it.
			input, err := json.Marshal(map[string]string{"command": command})
			if err != nil {
				t.Fatal(err)
			}
			cap := tool.EffectiveCapability(st, json.RawMessage(input))
			if got := plan.Decide(cap); got != permission.Deny {
				t.Errorf("shell %q classified %s, plan mode decided %v; want Deny", command, cap, got)
			}

			// Git-tool path: statically CapRead, so plan mode allows the call
			// through and the refusal has to come from Execute itself.
			if c.argv == nil {
				return
			}
			if got := plan.Decide((&gitTool{root: root}).Capability()); got != permission.Allow {
				t.Fatalf("precondition: plan mode should allow the CapRead git tool, got %v", got)
			}
			res, err := (&gitTool{root: root}).Execute(context.Background(), gitInput(t, map[string]any{
				"subcommand": c.argv[0],
				"args":       c.argv[1:],
			}))
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			if !res.IsError {
				t.Errorf("git tool ran %v instead of refusing it: %q", c.argv, res.Content)
			}
		})
	}
}

// TestArgvPathCandidates pins the parsing the confinement rests on, including
// the two spellings the old "skip anything starting with -" rule never looked
// inside. The separated spelling ("-o /etc/x") has no case of its own on
// purpose: its value is a bare operand.
func TestArgvPathCandidates(t *testing.T) {
	cases := []struct {
		arg  string
		want []string
	}{
		{"file.txt", []string{"file.txt"}},
		{"--", nil},
		{"-", nil},
		{"", nil},
		{"--oneline", nil},
		{"-p", nil},
		{"--output=/etc/x", []string{"/etc/x"}},
		{"--output=", nil},
		{"-o/etc/x", []string{"/etc/x"}},
		{"-la", []string{"a"}}, // harmless: resolves inside root
		{"--pretty=format:%h", []string{"format:%h"}},
	}
	for _, c := range cases {
		t.Run(c.arg, func(t *testing.T) {
			got := argvPathCandidates(c.arg)
			if len(got) != len(c.want) {
				t.Fatalf("argvPathCandidates(%q) = %q, want %q", c.arg, got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("argvPathCandidates(%q) = %q, want %q", c.arg, got, c.want)
				}
			}
		})
	}
}

// TestFlagMatchesAttachedShortValue guards the one asymmetry in the denylist
// matcher: a one-letter short flag also matches its attached-value spelling
// ("-o/etc/x"), while a long flag does not match by prefix — "--config" must
// not swallow an unrelated "--configure" and "-c" must not swallow
// "--committer=x".
func TestFlagMatchesAttachedShortValue(t *testing.T) {
	cases := []struct {
		arg, flag string
		want      bool
	}{
		{"-o", "-o", true},
		{"-o/etc/x", "-o", true},
		{"-o=/etc/x", "-o", true},
		{"--output=/etc/x", "--output", true},
		{"--output", "--output", true},
		{"--outputx", "--output", false},
		{"--committer=x", "-c", false},
		{"--config=x", "--config", true},
		{"--configure", "--config", false},
	}
	for _, c := range cases {
		if got := flagMatches(c.arg, c.flag); got != c.want {
			t.Errorf("flagMatches(%q, %q) = %v, want %v", c.arg, c.flag, got, c.want)
		}
	}
}
