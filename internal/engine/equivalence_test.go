package engine

import (
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// TestTurnSignatureUsesEquivalenceKey is the P67.10 unit for the third member
// of the per-call loop-signature family: two calls with structurally
// different raw JSON collapse to the same signature when the tool declares
// them equivalent, and a call the classifier declines (ok=false) falls back
// to the default canonicalization exactly as if equivKey were nil.
func TestTurnSignatureUsesEquivalenceKey(t *testing.T) {
	equiv := func(tu provider.ToolUseBlock) (string, bool) {
		if tu.Name != "search" {
			return "", false
		}
		var args struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(tu.Input, &args); err != nil {
			return "", false
		}
		// Pretend query text is equivalent regardless of casing/whitespace —
		// a comparison canonicalizeToolInput's structural equality can't see.
		return "search:" + args.Query, true
	}

	a, recA := turnSignatureExcludingPolls([]provider.ToolUseBlock{
		{Name: "search", Input: json.RawMessage(`{"query":"foo bar"}`)},
	}, nil, nil, equiv)
	b, recB := turnSignatureExcludingPolls([]provider.ToolUseBlock{
		{Name: "search", Input: json.RawMessage(`{"query":"foo bar","nonce":"abc123"}`)},
	}, nil, nil, equiv)
	if !recA || !recB {
		t.Fatal("a turn with a classified call must still be recorded")
	}
	if a != b {
		t.Errorf("equivalent calls must produce the same signature:\n %q\n %q", a, b)
	}

	// A call the classifier declines falls back to the default
	// canonicalization, so two structurally different "other" calls still
	// compare unequal.
	c, _ := turnSignatureExcludingPolls([]provider.ToolUseBlock{
		{Name: "other", Input: json.RawMessage(`{"x":1}`)},
	}, nil, nil, equiv)
	d, _ := turnSignatureExcludingPolls([]provider.ToolUseBlock{
		{Name: "other", Input: json.RawMessage(`{"x":2}`)},
	}, nil, nil, equiv)
	if c == d {
		t.Error("a call the classifier declines must still be distinguished by its raw input")
	}
}

// TestLoopDetectorFiresOnDeclaredEquivalence exercises the same mechanism
// through loopGuard.check, confirming the equivalence key actually reaches
// loop detection rather than only the signature-building unit above.
func TestLoopDetectorFiresOnDeclaredEquivalence(t *testing.T) {
	equiv := func(tu provider.ToolUseBlock) (string, bool) {
		return "same-call", true
	}
	g := &loopGuard{threshold: 3, detector: newLoopDetector(3), equivKey: equiv}

	calls := [][]provider.ToolUseBlock{
		{{Name: "search", Input: json.RawMessage(`{"query":"a"}`)}},
		{{Name: "search", Input: json.RawMessage(`{"query":"b"}`)}},
		{{Name: "search", Input: json.RawMessage(`{"query":"c"}`)}},
	}
	var v loopVerdict
	for _, c := range calls {
		v = g.check(c, 0)
		if v.abort != nil {
			break
		}
	}
	if v.abort == nil && v.notice == "" {
		t.Fatal("three structurally different calls declared equivalent must trip the loop detector")
	}
}
