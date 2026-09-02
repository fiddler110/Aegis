package sysprompt

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

type fakeDeferredTool struct {
	name, desc, hint string
}

func (f *fakeDeferredTool) Name() string                 { return f.name }
func (f *fakeDeferredTool) Description() string          { return f.desc }
func (f *fakeDeferredTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *fakeDeferredTool) Capability() tool.Capability  { return tool.CapRead }
func (f *fakeDeferredTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}
func (f *fakeDeferredTool) SearchHint() string { return f.hint }

// TestDeferredToolsBlockAppendsKeywordsWhenPresent is the P67.10 unit: a
// SearchHinter's keyword line is appended in brackets after the summary, and
// a tool without one gets no bracket at all.
func TestDeferredToolsBlockAppendsKeywordsWhenPresent(t *testing.T) {
	r := tool.NewRegistry()
	if err := r.RegisterDeferred(&fakeDeferredTool{name: "hinted", desc: "Do a thing well.", hint: "alpha, beta"}); err != nil {
		t.Fatal(err)
	}
	// A plain tool with no SearchHint implementer at all.
	if err := r.RegisterDeferred(&plainDeferredTool{name: "plain", desc: "Do another thing."}); err != nil {
		t.Fatal(err)
	}

	block := DeferredToolsBlock(r)
	if !strings.Contains(block, "hinted: Do a thing well. [alpha, beta]") {
		t.Errorf("block missing hinted tool's keyword line: %q", block)
	}
	if !strings.Contains(block, "plain: Do another thing.\n") {
		t.Errorf("block should print the plain tool's line with no trailing bracket: %q", block)
	}
	if strings.Contains(block, "plain: Do another thing. [") {
		t.Errorf("block must not append a bracket for a tool with no SearchHinter: %q", block)
	}
}

// plainDeferredTool implements tool.Tool only, no SearchHinter — the
// overwhelming majority case.
type plainDeferredTool struct{ name, desc string }

func (f *plainDeferredTool) Name() string        { return f.name }
func (f *plainDeferredTool) Description() string { return f.desc }
func (f *plainDeferredTool) InputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (f *plainDeferredTool) Capability() tool.Capability { return tool.CapRead }
func (f *plainDeferredTool) Execute(context.Context, json.RawMessage) (tool.Result, error) {
	return tool.Result{}, nil
}
