package enginecfg

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Every production call site that builds an engine has to make a decision about
// the run bounds and the backend parameters, and this test is what makes "we
// decided" auditable — the same instrument
// TestEveryEngineCallSiteDecidesItsGate applies to the permission stack.
//
// It exists because the gate test alone did not close the defect shape it was
// built for. The gate stopped drifting; the bounds kept drifting, in the same
// direction and for the same reason. At the time this was written, three of the
// eight engine.New sites hand-copied a subset of enginecfg.Limits rather than
// calling Apply, and every one of them had lost fields since:
//
//   - the in-process sub-agent, the subprocess worker and both debate paths
//     silently ignored security.redact_secrets, so a teammate's read-tool output
//     reached the model provider unscrubbed on a daemon whose operator had
//     turned redaction on — the one item here that is a security gap rather than
//     an ignored preference;
//   - the same four ignored provider.max_iterations and provider.loop_threshold,
//     running on the engine's built-in 40/5 regardless of config;
//   - both debate paths additionally dropped cost.max_turn_stall, which is the
//     only bound covering tool execution at all, and provider.tool_call_shim,
//     without which a debate seat on a shimmed deployment cannot call a tool.
//
// Each was correct when written and none was revisited when a field was added.
// That is precisely the shape the enginecfg package doc describes, and it
// responds to precisely the same instrument: an omission has to be a sentence
// somebody wrote, not a field somebody forgot.
//
// The scan looks at the whole enclosing function rather than only the call's own
// argument list, because Apply writes into an engine.Options built above the
// call — unlike Gate, which is a field inside the literal.
func TestEveryEngineCallSiteDecidesItsLimits(t *testing.T) {
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
			region := enclosingFunc(src, idx) + balancedCall(src[idx:])
			// Bounds that reached this call from the shared reading of config,
			// however the caller spelled it — CostLimits(...).Apply(&opts), a
			// Limits value carried in a local, or a named opt-out such as
			// WithoutContextTokenCap.
			if strings.Contains(region, "CostLimits(") || strings.Contains(region, "Limits.Apply(") {
				continue
			}
			if c := precedingComment(src[:idx]); strings.Contains(c, "Limits") || strings.Contains(c, "bounds") {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s: engine.New call site neither takes its run bounds from enginecfg.CostLimits "+
				"nor explains in a preceding comment why it has none. Hand-copying a subset of the "+
				"fields is the drift this package exists to close: the four sites that did it had each "+
				"lost security.redact_secrets, provider.max_iterations and provider.loop_threshold, and "+
				"two had lost cost.max_turn_stall — the only bound covering tool execution.",
				filepath.ToSlash(rel))
		}
	}
	// A guard on the guard, for the same reason the gate test carries one: a
	// refactor that renames the call or hides every site behind a helper would
	// otherwise pass by finding nothing at all.
	if sites < 6 {
		t.Fatalf("found only %d engine.New call sites; the scan is no longer finding them", sites)
	}
}

// TestEveryEngineCallSiteDecidesItsBackend is the same audit for
// enginecfg.Backend: the sampling knobs, the tool-call shim and the P66.14
// calibration admission. Split from the bounds above rather than folded in
// because the two are genuinely separable decisions — a probe with a synthetic
// adapter has bounds to decide and no backend to inherit — and a single test
// covering both would be satisfied by a site that had thought about only one.
func TestEveryEngineCallSiteDecidesItsBackend(t *testing.T) {
	root := repoRoot(t)
	for _, path := range goSourceFiles(t, root) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		src := string(data)
		for _, idx := range callIndexes(src, "engine.New(") {
			region := enclosingFunc(src, idx) + balancedCall(src[idx:])
			if strings.Contains(region, "ModelBackend(") {
				continue
			}
			if c := precedingComment(src[:idx]); strings.Contains(c, "Backend") || strings.Contains(c, "backend") {
				continue
			}
			rel, _ := filepath.Rel(root, path)
			t.Errorf("%s: engine.New call site neither takes its backend parameters from "+
				"enginecfg.ModelBackend nor explains in a preceding comment why it has none. "+
				"provider.tool_call_shim is the field that makes this more than tidiness: a run "+
				"that omits it on a shimmed deployment cannot call a tool at all.",
				filepath.ToSlash(rel))
		}
	}
}

// enclosingFunc returns the source from the start of the function containing
// the call at idx up to idx. It is a lexical approximation — the last
// line-initial `func ` before the call — which is exactly right for a top-level
// function and conservative for a closure, since it then returns the *outer*
// function and so sees strictly more than the closure alone. Seeing more can
// only make this test more permissive, never less, which is the safe direction
// for a heuristic: a false pass is a missing audit, a false failure is a broken
// build.
func enclosingFunc(src string, idx int) string {
	before := src[:idx]
	start := strings.LastIndex(before, "\nfunc ")
	if start < 0 {
		return before
	}
	return before[start:]
}

// stripLineComments removes `//` comments so a scan that looks for code can't
// match prose. It matters for the gate audit's exclusion rule: cli/debate.go
// explains in a comment that it no longer builds `permission.New(...)`
// directly, and once the scan widened from the call to the enclosing function,
// that sentence read as the bypass it was written to record the removal of.
//
// Deliberately naive — no block comments, no awareness of `//` inside a string
// literal. Both would make it drop *more* text, and the audits it feeds fail
// open (less text means falling through to the "explain it" branch), so the
// error is toward asking a human for a sentence rather than toward silently
// passing a site.
func stripLineComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, ln := range lines {
		if c := strings.Index(ln, "//"); c >= 0 {
			lines[i] = ln[:c]
		}
	}
	return strings.Join(lines, "\n")
}
