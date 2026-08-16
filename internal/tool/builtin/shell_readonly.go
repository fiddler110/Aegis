package builtin

import (
	"strings"

	"github.com/fiddler110/aegis/internal/permission"
)

// readOnlyShellArgv0 is the allowlist of binaries whose invocation classifies
// as read-only (P25.4c) regardless of arguments, as long as the command
// contains no shell chaining/redirection/substitution metacharacter. Keys
// are lowercase; matching lowercases the observed binary name so a Windows
// "Cat.exe" or a differently-cased alias still matches.
var readOnlyShellArgv0 = map[string]bool{
	"ls": true, "cat": true, "head": true, "tail": true, "wc": true,
	"pwd": true, "stat": true, "file": true,
	"grep": true, "which": true, "whoami": true,
	"date": true, "uname": true, "hostname": true, "id": true,
	"du": true, "df": true,
	"cut": true, "tr": true, "nl": true,
	"type": true,
	// Deliberately NOT here (P66.3/SEC-04): ps, less, more. `ps auxwwe` prints
	// the daemon's own process environment — the provider API keys — which is
	// the exact leak env/printenv are excluded for below, reached by a
	// different binary. `less` and `more` are pagers that shell out (!command,
	// v to open an editor), so they are program launchers wearing a reader's
	// name.
	//
	// Deliberately NOT here (P66.3/VULN-02): sort, tree, uniq. Each has a
	// documented file-*writing* form — `sort -o FILE`, `tree -o FILE`,
	// `uniq INPUT OUTPUT` — so no argument parsing makes them read-only. The
	// attached-value confinement in argv_confine.go stops those forms from
	// escaping the workspace, but a write inside the workspace is still a
	// write, and plan mode allows CapRead silently.
	//
	// Deliberately NOT here (P40.1): env/printenv. They are read-only in the
	// filesystem sense but dump the daemon's process environment, which holds
	// the provider API keys (config.loadDotEnv os.Setenv's .aegis/.env into the
	// process, ProviderAPIKey reads os.Getenv). Downgrading them to CapRead
	// auto-approves them under plan mode, leaking the keys into the transcript
	// and SQLite session store before the CapNetwork egress gate ever fires.
	// Falling back to the normal CapExecute approval is the safe posture, and
	// they are low value as read-only anyway.
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
	// argvStaysInRoot (argv_confine.go) confines operands *and* attached flag
	// values; the old local helper skipped every token starting with "-",
	// which is what VULN-02 rode in on. A path outside root disqualifies the
	// command from the CapRead downgrade — it still runs, just under the
	// normal CapExecute approval flow instead of being silently auto-allowed
	// under plan mode's read gate.
	return argvStaysInRoot(root, fields[1:])
}

// readOnlyGitCommand reports whether a git invocation's arguments (fields
// after "git") are a read-only status/log/diff call. Past the subcommand
// allowlist it defers entirely to validateReadOnlyGitArgv, which is the same
// check the dedicated git tool runs on the same argv — the two used to carry
// divergent denylists and only one of them confined paths (P66.3).
func readOnlyGitCommand(root string, args []string) bool {
	if len(args) == 0 || !readOnlyGitSubcommands[strings.ToLower(args[0])] {
		return false
	}
	return validateReadOnlyGitArgv(root, args) == nil
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
