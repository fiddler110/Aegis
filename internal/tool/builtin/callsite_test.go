package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every production call site that registers the built-in tools has to make a
// decision about the local prompt profile (P25.6), and this test is what makes
// "we decided" auditable.
//
// P62.9 cut 590 tokens off the daemon's local base prompt; P62.10 then found the
// profile was passed at exactly one of five Register call sites, so `aegis chat`,
// the subprocess worker, `debate` and `dry-run` each registered the cloud surface
// no matter which model was configured — 1,318 measured schema tokens on the
// CLI's own option set, security_scan alone 818. Nothing failed: the sites were
// simply never revisited when the profile shipped, which is the same shape as
// P10.1 (the gate stack) and P10.2 (the sandbox) not crossing the worker's
// process boundary.
//
// Passing the profile is not required — chat.go deliberately does not, pending
// the live tier. Saying so at the call site is: an omission has to be a sentence
// somebody wrote, not a field somebody forgot.
func TestEveryRegisterCallSiteDecidesTheLocalProfile(t *testing.T) {
	root := repoRoot(t)
	sites := 0
	for _, path := range goSourceFiles(t, root) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		for _, idx := range callIndexes(src, "builtin.Register(") {
			sites++
			call := balancedCall(src[idx:])
			if strings.Contains(call, "LocalProfile:") {
				continue
			}
			if strings.Contains(precedingComment(src[:idx]), "LocalProfile") {
				continue
			}
			// A site that builds its options with enginecfg.BuiltinOptions has
			// decided, and has decided in the one place CLAUDE.md says the
			// decision belongs — that helper sets LocalProfile from
			// cfg.Provider.LocalPromptProfile() for every caller. Recognising it
			// is stricter than the comment escape hatch above, not looser: the
			// field is provably set rather than merely discussed. Without this
			// arm the test failed on chat.go and debate.go after they moved to
			// the helper, which is the "decision made correctly, in the shared
			// place, and the auditor could not see it" shape.
			if optsFromBuiltinOptions(src[:idx], call) {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s: builtin.Register call site neither passes LocalProfile nor explains in a preceding comment why it does not", filepath.ToSlash(rel))
		}
	}
	// A guard on the guard: if a refactor renames the call or moves every site
	// behind a helper, this test would pass by finding nothing at all.
	if sites < 4 {
		t.Fatalf("found only %d builtin.Register call sites; the scan is no longer finding them", sites)
	}
}

// optsFromBuiltinOptions reports whether the options value passed to
// builtin.Register was built by enginecfg.BuiltinOptions earlier in the same
// file. It takes the second argument's identifier out of the call and looks for
// its assignment; overlaying host wiring onto that value afterwards
// (opts.Sandbox = …) does not unset LocalProfile, so an intervening assignment
// to a field is not a disqualifier. A composite literal passed inline has no
// identifier and falls through to the checks above, which is correct — that
// site really does have to say what it decided.
func optsFromBuiltinOptions(before, call string) bool {
	open := strings.Index(call, "(")
	if open < 0 {
		return false
	}
	args := strings.SplitN(call[open+1:], ",", 2)
	if len(args) < 2 {
		return false
	}
	ident := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(args[1]), ")"))
	if ident == "" || strings.ContainsAny(ident, "{}().") {
		return false
	}
	for _, assign := range []string{ident + " := enginecfg.BuiltinOptions(", ident + " = enginecfg.BuiltinOptions("} {
		if strings.Contains(before, assign) {
			return true
		}
	}
	return false
}

// repoRoot walks up from this package to the directory holding go.mod.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not locate the repository root (no go.mod above this package)")
		}
		dir = parent
	}
}

// goSourceFiles lists the repo's production Go files: tests are excluded because
// a test may legitimately register whichever profile it is exercising.
func goSourceFiles(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "vendor", "testdata", "dist":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") && !strings.HasSuffix(path, "_test.go") {
			out = append(out, path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// callIndexes returns every offset in src where call begins.
func callIndexes(src, call string) []int {
	var out []int
	for off := 0; ; {
		i := strings.Index(src[off:], call)
		if i < 0 {
			return out
		}
		out = append(out, off+i)
		off += i + len(call)
	}
}

// balancedCall returns src from the start of a call through its matching close
// paren, so a multi-line composite literal argument is read whole. src must
// begin at the call. An unbalanced remainder returns everything, which fails
// open into the "explain it" branch rather than silently passing.
func balancedCall(src string) string {
	depth := 0
	for i := 0; i < len(src); i++ {
		switch src[i] {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return src[:i+1]
			}
		}
	}
	return src
}

// precedingComment returns the run of `//` comment lines immediately above the
// line the call starts on, which is where a call site's reasoning lives.
func precedingComment(before string) string {
	lines := strings.Split(before, "\n")
	if len(lines) > 0 {
		lines = lines[:len(lines)-1] // drop the partial line the call sits on
	}
	var block []string
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(ln, "//") {
			break
		}
		block = append(block, ln)
	}
	return strings.Join(block, "\n")
}
