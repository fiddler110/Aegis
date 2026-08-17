package engine

import (
	"context"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// TestEnginePurposeReachesEveryRequest is the engine half of P67.3. The tag is
// only useful past the adapter seam, so what has to hold is that it is on the
// request — on *every* turn, not just the first, since a run's later turns are
// exactly the ones that meet a backend already under load.
func TestEnginePurposeReachesEveryRequest(t *testing.T) {
	rec := &reqCapturingAdapter{inner: &scriptAdapter{replies: []string{"one", "two"}}}
	eng, err := New(Options{Adapter: rec, Model: "m", Purpose: provider.PurposeSubAgent})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}
	if len(rec.reqs) == 0 {
		t.Fatal("no requests captured")
	}
	for i, req := range rec.reqs {
		if req.Purpose != provider.PurposeSubAgent {
			t.Errorf("request %d: purpose = %q, want %q", i, req.Purpose, provider.PurposeSubAgent)
		}
	}
}

// TestEnginePurposeDefaultsToUnspecified pins the non-regression half: an
// engine whose caller says nothing sends an untagged request, which resolves to
// the baseline retry policy — the behavior that existed before the tag. A
// caller must never acquire the foreground policy by omission.
func TestEnginePurposeDefaultsToUnspecified(t *testing.T) {
	rec := &reqCapturingAdapter{inner: &scriptAdapter{replies: []string{"one"}}}
	eng, err := New(Options{Adapter: rec, Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	if err := eng.Run(context.Background(), conv, func(Event) {}); err != nil {
		t.Fatalf("run: %v", err)
	}
	for i, req := range rec.reqs {
		if req.Purpose != provider.PurposeUnspecified {
			t.Errorf("request %d: purpose = %q, want the zero value", i, req.Purpose)
		}
	}
}
