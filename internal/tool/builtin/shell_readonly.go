package builtin

import (
	"strings"

	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/sandbox"
)

// readOnlyShellArgv0 is the allowlist of binaries whose invocation classifies
// as read-only (P25.4c) regardless of arguments, as long as the command
// contains no shell chaining/redirection/substitution metacharacter. Keys
// are lowercase; matching lowercases the observed binary name so a Windows
// "Cat.exe" or a differently-cased alias still matches.
var readOnlyShellArgv0 = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "wc": true,
	"pwd": true, "stat": true, "file": true,
	"grep": true, "which": true, "whoami": true, "env": true, "printenv": true,
	"date": true, "uname": true, "hostname": true, "id": true,
	"du": true, "df": true, "ps": true, "sort": true, "uniq": true,
	"cut": true, "tr": true, "nl": true, "less": true, "more": true,
	"type": true, "tree": true,
	// PowerShell read-only equivalents (the shell tool runs via PowerShell
	// on Windows; see shell.go's Description).
	"get-childitem": true, "get-content": true, "get-item": true, "test-path": true,
	"get-location": true, "get-process": true, "get-date": true, "select-string": true,
	"where.exe": true,
}

// readOnlyGitSubcommands is the allowlist of git subcommands treated as
// read-only. Only subcommands that are read-only for every possible
// argument combination belong here — e.g. "branch"/"tag"/"remote" are
// excluded because a positional argument turns them into a mutation
// ("git branch foo" creates a branch), unlike "status"/"log"/"diff"/"show"
// which stay read-only regardless of extra flags or pathspecs.
var readOnlyGitSubcommands = map[string]bool{
	"status": true, "log": true, "diff": true, "show": true, "blame": true,
	"rev-parse": true, "describe": true, "ls-files": true, "cat-file": true,
	"shortlog": true,
}

// gitConfigOverrideFlags can redirect git's behavior through an arbitrary
// external program (a pager, a diff/merge driver, a credential helper,
// upload-pack), turning a nominally read-only subcommand into code
// execution — so their presence anywhere in a git invocation disqualifies it
// regardless of subcommand ("git -c core.pager=sh log").
var gitConfigOverrideFlags = []string{
	"-c", "--config", "-p", "--paginate", "--exec", "--exec-path",
	"--upload-pack", "--receive-pack",
}

// readOnlyShellCommand reports whether command is safe to gate as
// tool.CapRead instead of tool.CapExecute (P25.4c): a narrow allowlist of
// inspection commands, rejected outright if any shell chaining/redirection/
// substitution metacharacter is present anywhere in the string — including
// one nested inside quotes, which this scan does not parse — or if any
// argument resolves (via sandbox.ValidatePath, the same root-confinement
// check read_file/grep/glob already use) outside root (P32.1). Being
// conservative here is deliberate: a false negative just means the call
// keeps requiring an execute approval like today; a false positive would
// auto-approve something that mutates state or exfiltrates data.
func readOnlyShellCommand(root, command string) bool {
	command = strings.TrimSpace(command)
	if command == "" || strings.ContainsAny(command, permission.ShellChainMetaChars) {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}
	bin := strings.ToLower(baseBinaryName(fields[0]))
	if bin == "git" {
		return readOnlyGitCommand(root, fields[1:])
	}
	if !readOnlyShellArgv0[bin] {
		return false
	}
	return shellArgsStayInRoot(root, fields[1:])
}

// readOnlyGitCommand reports whether a git invocation's arguments (fields
// after "git") are a read-only status/log/diff call with no config-override
// flag anywhere in the argument list and no path argument (e.g. a "--"
// pathspec) that resolves outside root.
func readOnlyGitCommand(root string, args []string) bool {
	if len(args) == 0 || !readOnlyGitSubcommands[strings.ToLower(args[0])] {
		return false
	}
	for _, a := range args {
		for _, f := range gitConfigOverrideFlags {
			if a == f || strings.HasPrefix(a, f+"=") {
				return false
			}
		}
	}
	return shellArgsStayInRoot(root, args[1:])
}

// shellArgsStayInRoot reports whether every non-flag argument in args
// resolves (via sandbox.ValidatePath) within root. Flags (tokens starting
// with "-") and the bare "--" separator are skipped since they are never
// paths; every other token is validated the same way read_file/grep/glob
// confine their path arguments, so an absolute path or a "../" traversal
// disqualifies the whole command from the CapRead downgrade (it still runs,
// just under the normal CapExecute approval flow instead of being silently
// auto-allowed under plan mode's read gate).
func shellArgsStayInRoot(root string, args []string) bool {
	for _, a := range args {
		if a == "--" || strings.HasPrefix(a, "-") {
			continue
		}
		if _, err := sandbox.ValidatePath(root, a); err != nil {
			return false
		}
	}
	return true
}

// baseBinaryName strips a path prefix and, on Windows, a .exe/.cmd/.bat
// suffix, so "/bin/cat" and "cat.exe" both classify the same as "cat".
func baseBinaryName(s string) string {
	if i := strings.LastIndexAny(s, `/\`); i >= 0 {
		s = s[i+1:]
	}
	lower := strings.ToLower(s)
	for _, ext := range []string{".exe", ".cmd", ".bat"} {
		if strings.HasSuffix(lower, ext) {
			return s[:len(s)-len(ext)]
		}
	}
	return s
}
