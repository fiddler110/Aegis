package permission_test

// This file lives in the external permission_test package (rather than
// permission's own internal test package) because it needs
// internal/tool/builtin, and internal/tool/builtin imports internal/permission
// itself (shell_readonly.go) — an internal *_test.go file in package
// permission importing builtin would be a genuine import cycle for the test
// binary; permission_test, being a separate package, is not.

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/fiddler110/aegis/internal/permission"
	"github.com/fiddler110/aegis/internal/tool"
	"github.com/fiddler110/aegis/internal/tool/builtin"
)

// recognizedSubjectFields mirrors permission's unexported subjectFieldNames
// (rules.go) — the field names WarnUnmatchableRules treats as proof a rule
// can match a tool. Kept here rather than exported from permission because
// the whole point of this test is to check the *externally observable*
// behavior (does a scoped rule actually fire?) against that list, not to
// reach into the package's internals.
var recognizedSubjectFields = []string{"command", "path", "file_path", "url", "query", "pattern"}

// TestSubjectExtractionAgreesWithSchemaForEveryRegisteredTool is the P74.1
// regression-*class* guard (as opposed to TestRuleGateDenyBlocksPathlessGrep,
// which is the single instance): grep shipped with a schema field
// (WarnUnmatchableRules's "toolHasSubjectField" check) that disagreed with
// what the capability-keyed extraction switch ("subjectFor") actually read,
// so a scoped rule looked matchable but silently never fired. This test
// proves the two stay in agreement for every tool the real built-in registry
// produces, not just grep, by exercising the real RuleGate end to end instead
// of reading subjectFor's source.
//
// For each registered tool whose declared input schema exposes at least one
// recognized subject field, it builds a call carrying a distinctive marker in
// every such field and asserts a deny rule scoped to that marker actually
// blocks the call. A tool that regresses to grep's shape — a recognized field
// the extraction switch never consults — fails loudly here instead of
// shipping a rule that looks like it should work and does not.
func TestSubjectExtractionAgreesWithSchemaForEveryRegisteredTool(t *testing.T) {
	root := t.TempDir()
	reg := tool.NewRegistry()
	if err := builtin.Register(reg, builtin.Options{Root: root}); err != nil {
		t.Fatalf("builtin.Register: %v", err)
	}

	const marker = "zzz-subject-marker"
	base := permission.New(permission.ModeBuild, permission.AutoApprove{})

	for _, tl := range reg.All() {
		var schema struct {
			Properties map[string]json.RawMessage `json:"properties"`
		}
		if err := json.Unmarshal(tl.InputSchema(), &schema); err != nil {
			continue // unparseable schema: WarnUnmatchableRules skips it too (never a false positive)
		}
		present := map[string]bool{}
		for _, f := range recognizedSubjectFields {
			if _, ok := schema.Properties[f]; ok {
				present[f] = true
			}
		}
		if len(present) == 0 {
			continue // no recognized field: a scoped rule against this tool is a known, warned-about no-op, not this test's concern
		}

		// Put the marker in every recognized field the schema declares, so
		// whichever one subjectFor actually reads, the value is there.
		input := map[string]any{}
		for f := range present {
			input[f] = marker
		}
		raw, err := json.Marshal(input)
		if err != nil {
			t.Fatalf("marshal input for %s: %v", tl.Name(), err)
		}

		ruleText := fmt.Sprintf("deny %s(%s)", tl.Name(), marker)
		rules, err := permission.ParseRules([]string{ruleText})
		if err != nil {
			t.Fatalf("ParseRules(%q): %v", ruleText, err)
		}
		gate := permission.NewRuleGate(base, rules)
		if ok, _ := gate.Check(context.Background(), tl, json.RawMessage(raw)); ok {
			t.Errorf("tool %q declares recognized subject field(s) %v (schema says a rule can match it) but %q never fired against input %s — subjectFor is returning an empty/unread subject for it, the P74.1 shape", tl.Name(), present, ruleText, raw)
		}
	}
}
