package builtin

import (
	"fmt"
	"regexp"
	"strings"
)

// posixOnWindowsHint catches the POSIX habits that cannot work against the
// PowerShell shell tool, and answers with the PowerShell equivalent instead of
// letting the command fail on its own.
//
// The tool's own description already says, verbatim, that Unix commands are not
// available and lists the replacements. A 2.6B model read that and ran
// `ls -la`, redirected to `/tmp/recon_output.txt`, and tried to write under
// `/home/user/` — all in one phase (P38.1 re-test, 2026-08-09). That is the
// general lesson from that run: at small sizes, instructions in a schema are
// close to inert, and only structure changes behaviour.
//
// So this returns a *tool error* naming the specific fix, which lands in the
// conversation where the model cannot skip it, rather than a PowerShell parse
// error like "A parameter cannot be found that matches parameter name 'la'"
// that requires knowing PowerShell to decode.
//
// Windows-only: on a POSIX host the shell tool really is /bin/sh and these
// commands are correct.

// unixOnlyCommands maps a Unix command that PowerShell does not provide to its
// PowerShell equivalent. Commands PowerShell aliases natively (ls, cat, rm, cp,
// mv, echo, pwd) are deliberately absent — they work, and flagging them would
// be a false positive. What breaks is their *flags*, handled separately.
var unixOnlyCommands = map[string]string{
	"grep":  "Select-String (or the grep tool)",
	"find":  "Get-ChildItem -Recurse (or the glob tool)",
	"touch": "New-Item -ItemType File",
	"chmod": "not applicable on Windows",
	"chown": "not applicable on Windows",
	"sed":   "the edit_file tool, or -replace",
	"awk":   "ForEach-Object",
	"head":  "Get-Content -TotalCount N",
	"tail":  "Get-Content -Tail N",
	"which": "Get-Command",
	"wc":    "Measure-Object",
	"ln":    "New-Item -ItemType SymbolicLink",
	"du":    "Get-ChildItem | Measure-Object -Sum Length",
	"df":    "Get-PSDrive",
	"man":   "Get-Help",
	"ps":    "Get-Process",
	"kill":  "Stop-Process",
}

// unixFlagRe catches a PowerShell-aliased command carrying Unix short flags
// that its PowerShell implementation does not accept — `ls -la` being the one
// observed live. Deliberately narrow: only the aliases whose PowerShell target
// rejects short flags outright.
var unixFlagRe = regexp.MustCompile(`(?i)^\s*(ls|dir|cp|mv|rm|mkdir)\s+-[a-z]`)

// posixAbsPathRe catches an absolute POSIX path used as a target on Windows.
// Anchored to the conventional roots rather than any leading slash, so a
// PowerShell switch or a regex literal is not mistaken for a path.
var posixAbsPathRe = regexp.MustCompile(`(^|\s|["'>=])(/tmp|/home|/usr|/var|/etc|/opt|/root)(/|\s|["']|$)`)

// checkPosixOnWindows returns a corrective message when cmd cannot work against
// PowerShell, or "" when it looks fine. It never rewrites the command: guessing
// at intent and running something the model did not ask for is a worse failure
// than refusing with an explanation.
func checkPosixOnWindows(cmd string, powershell bool) string {
	if !powershell {
		return ""
	}
	if m := posixAbsPathRe.FindStringSubmatch(cmd); m != nil {
		return fmt.Sprintf("this is a Windows host and %s is not a real path here: the shell tool runs PowerShell, and there is no %s. Use a workspace-relative path (the working directory is already the workspace root), or an absolute Windows path.", m[2], m[2])
	}
	if unixFlagRe.MatchString(cmd) {
		return "this is a Windows host: the shell tool runs PowerShell, where that command is an alias that does not accept Unix short flags. Use Get-ChildItem (with -Force to include hidden entries) instead of `ls -la`, or use the ls/glob tools."
	}
	// Check the leading word of each segment, so a Unix command after a pipe or
	// a separator is caught too.
	for _, seg := range splitShellSegments(cmd) {
		fields := strings.Fields(seg)
		if len(fields) == 0 {
			continue
		}
		name := strings.ToLower(strings.TrimPrefix(fields[0], "\\"))
		if repl, bad := unixOnlyCommands[name]; bad {
			return fmt.Sprintf("this is a Windows host and %q is not available: the shell tool runs PowerShell. Use %s instead.", name, repl)
		}
	}
	return ""
}

// splitShellSegments splits on the separators that start a new command word,
// so `foo | grep bar` and `foo; find .` both surface their second command.
func splitShellSegments(cmd string) []string {
	return strings.FieldsFunc(cmd, func(r rune) bool {
		return r == '|' || r == ';' || r == '\n'
	})
}
