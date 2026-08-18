package builtin

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/tool"
)

// P67.8 — the test pass is the item.
//
// The read-only classifier decides two things at once: what the engine runs in
// parallel without the exec lock, and what plan mode approves *without asking*.
// Every command this file admits is exercised in both its safe and its unsafe
// form, because the unsafe form is the one that matters.

// TestReadOnlyShellFlagParsing covers the per-command flag tables: what each
// newly-admitted command may do, what it may not, and that an unlisted flag
// fails closed rather than riding in on the binary's name.
func TestReadOnlyShellFlagParsing(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name    string
		command string
		want    bool
	}{
		// --- sort: admitted, minus its writing and exec'ing flags ----------
		{"sort bare", "sort file.txt", true},
		{"sort numeric reverse", "sort -n -r file.txt", true},
		{"sort clustered", "sort -nru file.txt", true},
		{"sort key and separator", "sort -k 2 -t , file.txt", true},
		{"sort parallel", "sort --parallel 4 file.txt", true},
		{"sort check optional value", "sort --check file.txt", true},
		{"sort output separated", "sort -o out.txt file.txt", false},
		{"sort output attached short", "sort -oout.txt file.txt", false},
		{"sort output long", "sort --output=out.txt file.txt", false},
		// Unsafe for a reason the flag name does not suggest: it execs an
		// arbitrary program over sort's temp files.
		{"sort compress-program", "sort --compress-program=gzip file.txt", false},
		{"sort random-source", "sort --random-source=seed.bin file.txt", false},
		{"sort parallel non-numeric", "sort --parallel many file.txt", false},

		// --- tree: admitted, minus -o/--output and --fromfile --------------
		{"tree bare", "tree", true},
		{"tree depth", "tree -L 2 src", true},
		{"tree all dirs", "tree -a -d", true},
		{"tree json to stdout", "tree -J", true},
		{"tree pattern", "tree -P *.go", true},
		{"tree output short", "tree -o out.txt", false},
		{"tree output attached", "tree -oout.txt", false},
		{"tree output long", "tree --output=out.txt", false},
		{"tree fromfile", "tree --fromfile .", false},
		{"tree depth non-numeric", "tree -L deep", false},

		// --- uniq: admitted, but its OUTPUT is a positional ----------------
		{"uniq stdin", "uniq", true},
		{"uniq one input", "uniq -c in.txt", true},
		{"uniq skip fields", "uniq -f 2 -w 10 in.txt", true},
		{"uniq group optional", "uniq --group in.txt", true},
		{"uniq second positional is an output file", "uniq in.txt out.txt", false},
		{"uniq second positional with flags", "uniq -c -i in.txt out.txt", false},

		// --- rg ------------------------------------------------------------
		{"rg bare pattern", "rg foo", true},
		{"rg glob and hidden", "rg -n --hidden -g *.go foo src", true},
		{"rg json context", "rg --json -C 3 foo", true},
		{"rg repeated short cluster", "rg -uu foo", true},
		{"rg replace affects output only", "rg -r bar foo", true},
		{"rg type filter", "rg --type go foo", true},
		// --pre runs an arbitrary preprocessor command per file.
		{"rg pre separated", "rg --pre cat foo", false},
		{"rg pre attached", "rg --pre=cat foo", false},
		{"rg pre-glob", "rg --pre-glob *.gz foo", false},
		// --hostname-bin execs a binary; the name suggests a lookup.
		{"rg hostname-bin", "rg --hostname-bin=hostname foo", false},
		// -z shells out to external decompressors.
		{"rg search-zip short", "rg -z foo", false},
		{"rg search-zip long", "rg --search-zip foo", false},
		{"rg generate", "rg --generate man", false},
		{"rg max-count non-numeric", "rg -m lots foo", false},

		// --- fd --------------------------------------------------------------
		{"fd extension", "fd -e go", true},
		{"fd type and depth", "fd --type f --max-depth 3 foo", true},
		{"fd print0 hidden", "fd -0 -H foo src", true},
		{"fd owner filter is not an output file", "fd -o root foo", true},
		{"fd exec short", "fd -x rm", false},
		{"fd exec long", "fd --exec rm", false},
		{"fd exec-batch", "fd -X echo", false},
		// The item's own instructive example: --list-details internally execs
		// `ls`, so a "details" flag is a PATH-hijack surface.
		{"fd list-details short", "fd -l foo", false},
		{"fd list-details long", "fd --list-details foo", false},
		{"fd gen-completions", "fd --gen-completions bash", false},

		// --- git: subcommands widened, shared argv rules unchanged ---------
		{"git grep", "git grep -n foo", true},
		{"git ls-tree", "git ls-tree -r HEAD", true},
		{"git rev-list", "git rev-list --count HEAD", true},
		{"git for-each-ref", "git for-each-ref", true},
		{"git check-ignore", "git check-ignore -v src", true},
		{"git merge-base", "git merge-base HEAD HEAD", true},
		// reflog expire prunes; symbolic-ref with two operands writes.
		{"git reflog", "git reflog expire --expire=now", false},
		{"git symbolic-ref", "git symbolic-ref HEAD refs/heads/x", false},
		{"git branch creates", "git branch newthing", false},
		{"git grep still refuses the shared denylist", "git grep --open-files-in-pager foo", false},

		// --- date: the clock-setting forms ---------------------------------
		{"date bare", "date", true},
		{"date utc", "date -u", true},
		{"date format positional", "date +%Y-%m-%d", true},
		{"date relative", "date -d yesterday", true},
		{"date set short", "date -s 12:00:00", false},
		{"date set long", "date --set=12:00:00", false},
		// BSD/macOS date sets the clock from a bare positional — no flag
		// table can express that, which is what the predicate is for.
		{"date bsd positional sets the clock", "date 010112002026", false},

		// --- hostname: the operand is the mutation -------------------------
		{"hostname bare", "hostname", true},
		{"hostname fqdn", "hostname -f", true},
		{"hostname sets the hostname", "hostname newname", false},
		{"hostname from file", "hostname -F cfg.txt", false},

		// --- file: -C compiles (writes) a magic file -----------------------
		{"file bare", "file x.txt", true},
		{"file brief mime", "file -b -i x.txt", true},
		{"file magic file", "file -m magic.mgc x.txt", true},
		{"file compile writes", "file -C -m magic", false},
		{"file compile long", "file --compile -m magic", false},

		// --- the same spelling meaning different things --------------------
		// ls -o is "long format, no group"; sort -o writes a file; fd -o
		// filters by owner; grep -o is --only-matching; df --output selects
		// columns. A global flag denylist cannot tell these apart, which is
		// the whole argument for per-command tables.
		{"ls -o is a format flag", "ls -o", true},
		{"grep -o is only-matching", "grep -o foo file.txt", true},
		{"df --output selects columns", "df --output=source,size", true},

		// --- unlisted flags fail closed ------------------------------------
		{"ls unknown long flag", "ls --definitely-not-a-flag", false},
		{"ls unlisted short flag", "ls -Z", false},
		{"cat unknown flag", "cat --nonexistent file.txt", false},
		{"wc unknown flag", "wc --made-up file.txt", false},
		{"grep unknown flag", "grep --invented foo file.txt", false},
		{"rg unknown flag", "rg --not-a-real-flag foo", false},
		{"no-value flag given a value", "cat --number=3 file.txt", false},
		{"unlisted flag after a listed one", "sort -n --nope file.txt", false},

		// --- positional bounds ---------------------------------------------
		{"pwd takes no operand", "pwd extra", false},
		{"whoami takes no operand", "whoami someone", false},
		{"uname takes no operand", "uname -a extra", false},
		{"id takes one operand", "id root", true},

		// --- numeric shorthand ---------------------------------------------
		{"head obsolete count", "head -20 file.txt", true},
		{"tail obsolete count", "tail -5 file.txt", true},
		{"head size suffix", "head -c 10K file.txt", true},
		{"head non-numeric line count", "head -n many file.txt", false},

		// --- POSIX "--", both ways -----------------------------------------
		{"cat honors double dash", "cat -- file.txt", true},
		{"cat double dash protects a dash-leading name", "cat -- -weird.txt", true},
		{"rg double dash before pattern", "rg -- -foo src", true},
		{"double dash does not launder an unlisted flag", "sort -- -o out.txt file.txt", true},
		// pwd and date do not honor "--", so it is just an unlisted flag.
		{"pwd does not honor double dash", "pwd --", false},
		{"date does not honor double dash", "date -- +%F", false},

		// --- metacharacters still reject the whole command ------------------
		{"sort with redirection", "sort file.txt > out.txt", false},
		{"rg piped", "rg foo | head", false},
		{"fd chained", "fd -e go && rm -rf .", false},
		{"tree with backticks", "tree `whoami`", false},
		{"uniq with substitution", "uniq $(echo f)", false},
		{"gh with semicolon", "gh pr list; rm -rf .", false},

		// --- the exclusions that are not about writing ----------------------
		// These do not become admissible under flag parsing, and there is no
		// "safe form" of them to admit. See the table comment in
		// shell_readonly.go for the reasoning behind each.
		{"env", "env", false},
		{"env with a single key", "env HOME", false},
		{"printenv", "printenv", false},
		{"printenv single key", "printenv ANTHROPIC_API_KEY", false},
		{"ps", "ps aux", false},
		{"ps env dump", "ps auxwwe", false},
		{"ps with a flag-shaped argument", "ps -o pid", false},
		{"less", "less file.txt", false},
		{"more", "more file.txt", false},
		{"find", "find . -name *.go", false},
		{"find exec", "find . -exec rm {} +", false},
		{"xargs", "xargs rm", false},
		{"sh", "sh script.sh", false},
		{"curl", "curl https://example.com", false},
		{"sudo", "sudo ls", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := readOnlyShellCommand(root, c.command); got != c.want {
				t.Errorf("readOnlyShellCommand(%q) = %v, want %v", c.command, got, c.want)
			}
		})
	}
}

// TestReadOnlyShellAttachedValueConfinement covers the half of the decision
// flag parsing does not make: a command whose flags are all listed is still
// only read-only for *this* invocation if every path it carries — operand or
// attached flag value — stays inside the workspace. Attached values are the
// VULN-02 shape: skipping every token that starts with "-" is what let
// `sort -o/etc/passwd` through.
func TestReadOnlyShellAttachedValueConfinement(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")

	cases := []struct {
		name    string
		command string
		want    bool
	}{
		// In-root: flags listed, paths confined, classified.
		{"grep pattern file in root", "grep --file=patterns.txt foo", true},
		{"rg ignore-file in root", "rg --ignore-file=.rgignore foo", true},
		{"fd base directory in root", "fd --base-directory=src foo", true},
		{"sort temp dir in root", "sort -T tmp file.txt", true},

		// Attached long value escaping the root.
		{"grep pattern file escapes", "grep --file=" + outside + " foo", false},
		{"rg ignore-file escapes", "rg --ignore-file=" + outside + " foo", false},
		{"fd base directory escapes", "fd --base-directory=" + outside + " foo", false},
		{"file magic file escapes", "file --magic-file=" + outside + " x.txt", false},

		// Attached short value escaping the root ("-f/etc/passwd").
		{"rg pattern file attached short escapes", "rg -f" + outside + " foo", false},
		{"grep pattern file attached short escapes", "grep -f" + outside + " foo", false},
		{"sort temp dir attached short escapes", "sort -T" + outside + " file.txt", false},

		// Separated value escaping the root (a plain operand next iteration).
		{"rg pattern file separated escapes", "rg -f " + outside + " foo", false},

		// Bare operands, the original P32.1 case.
		{"tree operand escapes", "tree " + outside, false},
		{"uniq operand escapes", "uniq " + outside, false},
		{"traversal operand", "sort ../../../etc/passwd", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := readOnlyShellCommand(root, c.command); got != c.want {
				t.Errorf("readOnlyShellCommand(%q) = %v, want %v", c.command, got, c.want)
			}
		})
	}
}

// TestShellCommandCapabilityClassification pins the capability the classifier
// hands back, not just whether it classified. `gh` is the reason the API
// answers with a capability: its subcommands are read-only on the filesystem
// but every one of them is network egress, and permission.Policy.Decide Asks
// for CapNetwork in plan mode while it silently Allows CapRead. Classifying gh
// as CapRead would make the downgrade *more* permissive than the network gate.
func TestShellCommandCapabilityClassification(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name    string
		command string
		wantCap tool.Capability
	}{
		{"local read", "sort file.txt", tool.CapRead},
		{"git log", "git log --oneline", tool.CapRead},
		{"gh pr list", "gh pr list", tool.CapNetwork},
		{"gh pr view with json", "gh pr view 42 --json title", tool.CapNetwork},
		{"gh issue list limit", "gh issue list --limit 10", tool.CapNetwork},
		{"gh run view", "gh run view 12345", tool.CapNetwork},
		{"gh search code", "gh search code needle", tool.CapNetwork},

		// gh's unsafe forms fall all the way back to CapExecute.
		{"gh web launches a browser", "gh pr view --web", tool.CapExecute},
		{"gh web short", "gh pr view -w", tool.CapExecute},
		{"gh hostname retargets the token", "gh pr list --hostname=evil.example", tool.CapExecute},
		{"gh api is a general http client", "gh api /repos/o/r", tool.CapExecute},
		{"gh api post", "gh api -X POST /repos/o/r/issues", tool.CapExecute},
		{"gh auth can print the token", "gh auth status", tool.CapExecute},
		{"gh pr create mutates", "gh pr create", tool.CapExecute},
		{"gh pr merge mutates", "gh pr merge 42", tool.CapExecute},
		{"gh issue close mutates", "gh issue close 42", tool.CapExecute},
		{"gh noun without a verb", "gh pr", tool.CapExecute},
		{"gh unknown noun", "gh secret list", tool.CapExecute},
		{"gh bare", "gh", tool.CapExecute},
	}

	st := newShellTool(root, 5, nil, nil)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			cap, ok := classifyShellCommand(root, c.command)
			if c.wantCap == tool.CapExecute {
				if ok {
					t.Fatalf("classifyShellCommand(%q) classified as %s, want unclassified", c.command, cap)
				}
			} else if !ok || cap != c.wantCap {
				t.Fatalf("classifyShellCommand(%q) = %s, %v; want %s, true", c.command, cap, ok, c.wantCap)
			}

			// The tool seam must report the same thing.
			input, err := json.Marshal(map[string]string{"command": c.command})
			if err != nil {
				t.Fatal(err)
			}
			if got := tool.EffectiveCapability(st, json.RawMessage(input)); got != c.wantCap {
				t.Errorf("EffectiveCapability(%q) = %s, want %s", c.command, got, c.wantCap)
			}

			// readOnlyShellCommand is the narrower question: network egress is
			// not a read, and must not inherit plan mode's silent allow.
			if got := readOnlyShellCommand(root, c.command); got != (c.wantCap == tool.CapRead) {
				t.Errorf("readOnlyShellCommand(%q) = %v, want %v", c.command, got, c.wantCap == tool.CapRead)
			}
		})
	}
}

// TestPlanModeStillRefusesTheWideningEdges walks the newly-admitted commands'
// unsafe forms through the gate that actually matters: plan mode, where
// CapRead is allowed silently. A false positive in the classifier is a
// silently auto-approved mutation, so the assertion is on the decision, not on
// the classification.
func TestPlanModeStillRefusesTheWideningEdges(t *testing.T) {
	root := t.TempDir()
	plan := permission.Policy{Mode: permission.ModePlan}
	st := newShellTool(root, 5, nil, nil)

	mustDeny := []string{
		"sort -o out.txt in.txt",
		"sort --output=out.txt in.txt",
		"sort --compress-program=gzip in.txt",
		"tree -o out.txt",
		"uniq in.txt out.txt",
		"rg --pre=cat foo",
		"rg -z foo",
		"fd -x rm",
		"fd --list-details",
		"date -s 12:00:00",
		"hostname newname",
		"file -C -m magic",
		"env",
		"printenv",
		"ps auxwwe",
		"less notes.txt",
		"more notes.txt",
	}
	for _, command := range mustDeny {
		t.Run(command, func(t *testing.T) {
			input, err := json.Marshal(map[string]string{"command": command})
			if err != nil {
				t.Fatal(err)
			}
			cap := tool.EffectiveCapability(st, json.RawMessage(input))
			if got := plan.Decide(cap); got != permission.Deny {
				t.Errorf("shell %q classified %s, plan mode decided %v; want Deny", command, cap, got)
			}
		})
	}

	// gh is the one classified command plan mode must *Ask* about rather than
	// allow silently: read-only on disk, network egress on the wire.
	input, err := json.Marshal(map[string]string{"command": "gh pr list"})
	if err != nil {
		t.Fatal(err)
	}
	if got := plan.Decide(tool.EffectiveCapability(st, json.RawMessage(input))); got != permission.Ask {
		t.Errorf("plan mode decided %v for `gh pr list`; want Ask", got)
	}
}

// TestReadOnlyShellPowerShellFlags covers the single-dash-long parameter style
// the Windows shell path uses: whole-word parameters, case-insensitive, no
// short-flag clustering, no POSIX "--", and abbreviations failing closed.
func TestReadOnlyShellPowerShellFlags(t *testing.T) {
	root := t.TempDir()
	cases := []struct {
		name    string
		command string
		want    bool
	}{
		{"get-childitem recurse", "Get-ChildItem -Recurse -Filter *.go", true},
		{"get-childitem lowercase", "get-childitem -recurse", true},
		{"get-childitem depth", "Get-ChildItem -Depth 2 -File", true},
		{"get-childitem colon-attached value", "Get-ChildItem -Filter:*.go", true},
		{"get-content tail", "Get-Content -Path notes.txt -Tail 20", true},
		{"get-content raw", "Get-Content notes.txt -Raw", true},
		{"select-string", "Select-String -Pattern foo -Path notes.txt", true},
		{"test-path", "Test-Path -Path notes.txt -PathType Leaf", true},
		{"get-date format", "Get-Date -Format o", true},
		{"get-process", "Get-Process -Name aegis", true},

		// Abbreviations are legal PowerShell but are not enumerated; failing
		// closed costs the call its downgrade, which is the safe direction.
		{"abbreviation fails closed", "Get-ChildItem -Rec", false},
		// Not a parameter cluster: "-la" is one unknown parameter name.
		{"no short clustering", "Get-ChildItem -la", false},
		// Reaches a remote machine.
		{"get-process computername", "Get-Process -ComputerName other", false},
		{"unknown parameter", "Get-Content -WriteTo out.txt", false},
		{"no posix double dash", "Get-ChildItem -- src", false},
		{"non-numeric depth", "Get-ChildItem -Depth deep", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := readOnlyShellCommand(root, c.command); got != c.want {
				t.Errorf("readOnlyShellCommand(%q) = %v, want %v", c.command, got, c.want)
			}
		})
	}
}

// TestReadOnlyShellPowerShellPathConfinement is the windows-only half: a
// drive-letter path is only recognized as absolute on windows itself.
func TestReadOnlyShellPowerShellPathConfinement(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("drive-letter absolute paths only apply on windows")
	}
	root := t.TempDir()
	cases := []struct {
		command string
		want    bool
	}{
		{`Get-Content -Path C:\Users\x\.ssh\id_rsa`, false},
		{`Get-Content -Path:C:\Windows\System32\drivers\etc\hosts`, false},
		{`Get-ChildItem -Path src -Recurse`, true},
	}
	for _, c := range cases {
		t.Run(c.command, func(t *testing.T) {
			if got := readOnlyShellCommand(root, c.command); got != c.want {
				t.Errorf("readOnlyShellCommand(%q) = %v, want %v", c.command, got, c.want)
			}
		})
	}
}
