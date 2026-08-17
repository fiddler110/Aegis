package enginecfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/permission"
)

// Every production call site that builds an engine has to make a decision about
// the permission gate, and this test is what makes "we decided" auditable.
//
// P66.13 found the dominant defect shape in this codebase — a mechanism built
// for one path that a second path silently bypasses — with the CLI as the second
// path. `aegis chat` built a bare permission.New(mode, approver): the daemon's
// five layers reduced to one, so `permission.rules` deny rules and
// `security.egress_then_write` were inert on the scripted path. Extracting the
// constructor turned up a fourth instance the review had not found (`aegis
// debate`) and a fifth partial one (the subprocess worker, stuck two layers
// behind). Every one of them was correct when written; none was revisited when a
// layer was added. That is the same shape as P10.1 (the gate stack not crossing
// the worker's process boundary) and P62.10 (the local prompt profile passed at
// one of five builtin.Register sites), and it responds to the same instrument.
//
// Stacking the gate is not required — a probe with synthetic in-memory tools has
// nothing to gate, and the eval harness is handed its Options whole. Saying so
// is: an omission has to be a sentence somebody wrote, not a field somebody
// forgot.
func TestEveryEngineCallSiteDecidesItsGate(t *testing.T) {
	root := repoRoot(t)
	sites := 0
	for _, path := range goSourceFiles(t, root) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		for _, idx := range callIndexes(src, "engine.New(") {
			sites++
			call := balancedCall(src[idx:])
			// The shared constructor, or a gate value that reached this call
			// from one. A literal permission.New in the call is deliberately
			// *not* accepted: that is the bypass itself.
			if strings.Contains(call, "Gate:") && !strings.Contains(call, "permission.New(") {
				continue
			}
			if strings.Contains(precedingComment(src[:idx]), "Gate") {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s: engine.New call site neither passes a Gate built by enginecfg.BuildGate "+
				"nor explains in a preceding comment why it has none. A bare permission.New here is "+
				"the P66.13 bypass: it is the mode gate alone, with no rules, contextual policy, "+
				"persona-tool or scope layer.", filepath.ToSlash(rel))
		}
	}
	// A guard on the guard: if a refactor renames the call or moves every site
	// behind a helper, this test would pass by finding nothing at all.
	if sites < 6 {
		t.Fatalf("found only %d engine.New call sites; the scan is no longer finding them", sites)
	}
}

// TestBareModeGateIsNotTheStack pins what the extraction is worth, so the test
// above is not the only thing standing between a future edit and a one-layer
// gate: a config with a deny rule and egress-then-write must produce something
// other than the bare permission.New a caller would otherwise write.
func TestBareModeGateIsNotTheStack(t *testing.T) {
	// Deliberately compared by identity of the *outermost* type rather than by
	// behavior: the behavioral tests for each layer live with that layer, and
	// what this asserts is only that the layers were applied at all.
	gate, _ := BuildGate(GateOptions{Mode: "build", Approver: permission.AutoDeny{}})
	if gate == nil {
		t.Fatal("BuildGate returned no gate")
	}
	if _, bare := gate.(interface{ Mode() string }); bare {
		t.Error("BuildGate returned what looks like the bare mode gate; the scope layer (P46.1) must always wrap it")
	}
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
// a test may legitimately build an engine with whatever gate it is exercising.
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
