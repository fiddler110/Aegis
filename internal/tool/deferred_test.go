package tool

import (
	"context"
	"encoding/json"
	"testing"
)

type fakeTool struct {
	name string
	desc string
}

func (f *fakeTool) Name() string                 { return f.name }
func (f *fakeTool) Description() string          { return f.desc }
func (f *fakeTool) InputSchema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (f *fakeTool) Capability() Capability       { return CapRead }
func (f *fakeTool) Execute(context.Context, json.RawMessage) (Result, error) {
	return Result{}, nil
}

func TestDeferredNotExposedUntilLoaded(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&fakeTool{name: "read", desc: "read files"}); err != nil {
		t.Fatal(err)
	}
	if err := r.RegisterDeferred(&fakeTool{name: "latex_build", desc: "build a LaTeX document"}); err != nil {
		t.Fatal(err)
	}

	// Only the core tool is exposed.
	if got := len(r.Schemas()); got != 1 {
		t.Fatalf("exposed schemas = %d, want 1", got)
	}
	// The deferred tool is advertised.
	def := r.Deferred()
	if len(def) != 1 || def[0].Name != "latex_build" {
		t.Fatalf("Deferred() = %+v", def)
	}

	// Search finds it by keyword.
	matches := r.SearchDeferred("latex")
	if len(matches) != 1 {
		t.Fatalf("SearchDeferred = %d, want 1", len(matches))
	}

	// Loading exposes it and removes it from the advertisement.
	r.Load("latex_build")
	if got := len(r.Schemas()); got != 2 {
		t.Fatalf("after load exposed schemas = %d, want 2", got)
	}
	if got := len(r.Deferred()); got != 0 {
		t.Fatalf("after load Deferred() = %d, want 0", got)
	}
}
