package builtin

import (
	"encoding/json"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

// TestSignatureTransparentSetStaysNarrow pins which builtins declare P64.2
// transparency, and — more importantly — which of their near neighbours do not.
//
// The failure this guards against is drift in one direction only. Transparency
// is cheap to add and each addition removes a tool's arguments from the one
// guard that bounds a runaway unattended drive, so the set grows silently unless
// something enumerates it. The rule is in tool.SignatureTransparent's doc: a
// tool qualifies when its arguments differ every turn *as a matter of course*
// (bookkeeping about the work), and never when its arguments are the model's
// choice of work. The opaque half of this table is where that rule is actually
// tested — each entry is a tool someone could plausibly argue into the set.
func TestSignatureTransparentSetStaysNarrow(t *testing.T) {
	transparent := []tool.Tool{
		&todoAddTool{},
		&todoUpdateTool{},
		&rememberTool{},
		&entityRememberTool{},
		&taskUpdateTool{},
	}
	opaque := []tool.Tool{
		// A search query is the model choosing what to look for — the evidence
		// the detector runs on, not bookkeeping about it.
		&projectKnowledgeTool{},
		&entityRecallTool{},
		// A skill body is a deliverable the model authored. Re-authoring the
		// same one every turn is a loop worth catching.
		&saveSkillTool{},
		// Reading the plan back is not a plan write, and repeating it is
		// evidence of exactly the confusion the detector exists to notice.
		&todoListTool{},
	}

	for _, tl := range transparent {
		if !tool.IsSignatureTransparent(tl, json.RawMessage(`{}`)) {
			t.Errorf("%s should declare SignatureTransparent", tl.Name())
		}
	}
	for _, tl := range opaque {
		if tool.IsSignatureTransparent(tl, json.RawMessage(`{}`)) {
			t.Errorf("%s must NOT declare SignatureTransparent — its arguments are the model's choice of work", tl.Name())
		}
	}
}

// TestTransparencyAndPollExemptionAreDisjoint asserts no tool claims both. They
// are different concessions of different sizes (see tool.SignatureTransparent),
// and a tool claiming both is a sign someone reached for the second while
// meaning the first — in which case the stronger exemption silently wins and the
// narrower declaration is decoration.
func TestTransparencyAndPollExemptionAreDisjoint(t *testing.T) {
	all := []tool.Tool{
		&todoAddTool{}, &todoUpdateTool{}, &todoListTool{},
		&rememberTool{}, &saveSkillTool{},
		&entityRememberTool{}, &entityRecallTool{},
		&taskGetTool{}, &taskOutputTool{}, &taskUpdateTool{}, &taskCreateTool{},
		&teamInboxTool{},
	}
	for _, tl := range all {
		in := json.RawMessage(`{}`)
		if tool.IsPollExempt(tl, in) && tool.IsSignatureTransparent(tl, in) {
			t.Errorf("%s claims both poll-exemption and signature-transparency; pick the narrower one that is true", tl.Name())
		}
	}
}
