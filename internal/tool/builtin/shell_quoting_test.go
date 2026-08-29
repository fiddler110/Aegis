package builtin

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

// TestClassifyShellCommandHonorsQuoting pins VULN-14: the read-only classifier
// used to split with strings.Fields, so a quoted absolute path kept its quotes
// through the confinement check, resolved as a *relative* name under the root,
// and earned the CapRead downgrade plan mode allows silently — while the shell
// stripped the quotes and read the real file.
//
// The table runs both directions on purpose. Refusing every quoted command
// would also "pass" the escape half while making `grep "foo bar" .` unusable in
// plan mode (CapExecute is Deny there), so the legitimate cases below are as
// load-bearing as the escapes.
func TestClassifyShellCommandHonorsQuoting(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "my file.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	escapes := []string{
		`cat '/etc/passwd'`,
		`cat "/etc/passwd"`,
		`cat ''/etc/passwd`,
		`cat "/etc/pass"wd`,
		`ls '/'`,
		`grep -r x '/etc'`,
		`head -n 5 "/etc/hosts"`,
		// deniedGitFlags compares raw tokens, so quoting used to hide the flag
		// name itself. --output is an out-of-workspace *write* (VULN-11) and
		// --git-dir relocates the repository (VULN-01).
		`git diff '--output=/tmp/x'`,
		`git log '--git-dir=/tmp/x'`,
		// A POSIX backslash escape is the third spelling of the same thing.
		`cat \/etc/passwd`,
		// Malformed input fails closed rather than being split arbitrarily.
		`cat '/etc/passwd`,
		`cat "unterminated`,
	}
	for _, cmd := range escapes {
		if cap, ok := classifyShellCommand(root, cmd, false); ok {
			t.Errorf("classifyShellCommand(%q) = %s, true; want unclassified — the shell would reach outside the root", cmd, cap)
		}
	}

	// Quoting a path with a space is the reason quotes appear in a read-only
	// command at all; these must keep the downgrade. Under strings.Fields they
	// were split into bogus tokens, so this half is a fix as well as a guard.
	legitimate := []string{
		`cat 'my file.txt'`,
		`cat "my file.txt"`,
		`cat my\ file.txt`,
		`grep "foo bar" .`,
		`ls -la`,
		`git status`,
	}
	for _, cmd := range legitimate {
		cap, ok := classifyShellCommand(root, cmd, false)
		if !ok || cap != tool.CapRead {
			t.Errorf("classifyShellCommand(%q) = %s, %v; want %s, true", cmd, cap, ok, tool.CapRead)
		}
	}
}

// TestSplitShellWordsBackslashDialect pins the one thing the powershell
// parameter decides, and why it cannot be defaulted: a backslash escapes under
// /bin/sh and separates path segments under PowerShell, so guessing wrong opens
// a hole in whichever direction the guess went.
func TestSplitShellWordsBackslashDialect(t *testing.T) {
	const rooted = `\Windows\System32\config\SAM`

	// POSIX: the shell removes the backslashes, so the classifier must too —
	// otherwise the token never looks like the rooted path it becomes.
	got, ok := splitShellWords(`cat `+rooted, true)
	if !ok || len(got) != 2 || got[1] != "WindowsSystem32configSAM" {
		t.Errorf("posix split = %q, %v; want the escapes consumed", got, ok)
	}

	// PowerShell: a backslash is an ordinary separator. Consuming it here would
	// turn a rooted path into the relative name "WindowsSystem32configSAM",
	// which confines happily — the same escape, mirrored.
	got, ok = splitShellWords(`Get-Content `+rooted, false)
	if !ok || len(got) != 2 || got[1] != rooted {
		t.Errorf("powershell split = %q, %v; want the backslashes preserved", got, ok)
	}
}
