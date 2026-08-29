package permission

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

// multiEditSchema mirrors multi_edit's real input schema in the one respect
// that matters here: the only path lives under edits[], with no top-level
// "path" or "file_path" for subjectFor to fall back on.
const multiEditSchema = `{"type":"object","properties":{"edits":{"type":"array","items":{"type":"object","properties":{"path":{"type":"string"},"old_string":{"type":"string"},"new_string":{"type":"string"}}}}},"required":["edits"]}`

func multiEditInput(paths ...string) json.RawMessage {
	edits := make([]map[string]string, 0, len(paths))
	for _, p := range paths {
		edits = append(edits, map[string]string{"path": p, "old_string": "a", "new_string": "b"})
	}
	b, _ := json.Marshal(map[string]any{"edits": edits})
	return b
}

// TestDenyRuleReachesMultiEditPaths pins the SEC-B hole: subjectFor read
// "path"/"file_path" but not edits[].path, so a path-scoped deny rule blocked
// write_file on a path and silently allowed multi_edit on the identical one.
// Nothing warned — WarnUnmatchableRules stays quiet because the rule does match
// the other write tools.
func TestDenyRuleReachesMultiEditPaths(t *testing.T) {
	rules, err := ParseRules([]string{"deny write(secrets/**)"})
	if err != nil {
		t.Fatal(err)
	}
	g := NewRuleGate(New(ModeBuild, AutoApprove{}), rules)
	me := fakeTool{name: "multi_edit", cap: tool.CapWrite, schema: json.RawMessage(multiEditSchema)}

	// The denied path alone.
	if ok, _ := g.Check(context.Background(), me, multiEditInput("secrets/key.pem")); ok {
		t.Error("multi_edit of a denied path was allowed")
	}
	// Denied alongside permitted: the call is all-or-nothing, so any denied
	// path must block the whole thing rather than being written with the rest.
	if ok, _ := g.Check(context.Background(), me, multiEditInput("docs/a.md", "secrets/key.pem")); ok {
		t.Error("multi_edit touching a denied path among permitted ones was allowed")
	}
	// A call that touches nothing denied is unaffected.
	if ok, reason := g.Check(context.Background(), me, multiEditInput("docs/a.md", "docs/b.md")); !ok {
		t.Errorf("multi_edit of permitted paths was blocked: %s", reason)
	}
}

// TestAllowRuleRequiresEveryMultiEditPath pins the other half of the
// asymmetry matchesAll documents: a scoped allow is clearance for the paths it
// names, so a call reaching past them falls through to the base gate rather
// than being auto-approved wholesale on the strength of its first path.
func TestAllowRuleRequiresEveryMultiEditPath(t *testing.T) {
	rules, err := ParseRules([]string{"allow write(docs/**)"})
	if err != nil {
		t.Fatal(err)
	}
	me := fakeTool{name: "multi_edit", cap: tool.CapWrite, schema: json.RawMessage(multiEditSchema)}

	// AutoDeny as the base makes "did the allow rule short-circuit?" observable:
	// only a rule match can produce true here.
	g := NewRuleGate(New(ModePlan, AutoDeny{}), rules)

	if ok, _ := g.Check(context.Background(), me, multiEditInput("docs/a.md", "docs/b.md")); !ok {
		t.Error("allow rule did not grant a call fully inside its scope")
	}
	if ok, _ := g.Check(context.Background(), me, multiEditInput("docs/a.md", "src/main.go")); ok {
		t.Error("allow rule granted a call that reaches outside its scope")
	}
}

// TestMultiEditIsNotReportedUnmatchable guards the startup warning against
// becoming a false positive now that the gate actually matches edits[].
func TestMultiEditIsNotReportedUnmatchable(t *testing.T) {
	rules, err := ParseRules([]string{"deny write(secrets/**)"})
	if err != nil {
		t.Fatal(err)
	}
	me := fakeTool{name: "multi_edit", cap: tool.CapWrite, schema: json.RawMessage(multiEditSchema)}
	var warned int
	WarnUnmatchableRules(rules, []tool.Tool{me}, func(string, ...any) { warned++ })
	if warned != 0 {
		t.Errorf("multi_edit reported as unmatchable %d time(s); the rule gate now reads edits[].path", warned)
	}
}
