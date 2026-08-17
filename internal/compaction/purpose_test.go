package compaction

import (
	"context"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// TestSummaryRequestIsTaggedCompaction pins P67.3's tag on the summarizer's own
// call. It matters twice over: retry treats a summary as attended-but-not-the-
// user's-turn, and P67.6 needs a compaction distinguishable from the
// conversation it is compacting — a summarizer that inherited the run's tag
// would erase exactly that distinction.
func TestSummaryRequestIsTaggedCompaction(t *testing.T) {
	a := &recordingAdapter{summary: "summary of earlier work"}
	s := New(Options{Adapter: a, Model: "m", ContextWindow: 32768, KeepRecent: 2})
	msgs := []provider.Message{
		text(provider.RoleUser, "first question"),
		text(provider.RoleAssistant, "first answer"),
		text(provider.RoleUser, "second question"),
		text(provider.RoleAssistant, "final reply kept"),
	}
	if _, changed, err := s.ForceCompact(context.Background(), "", msgs); err != nil || !changed {
		t.Fatalf("ForceCompact: changed=%v err=%v", changed, err)
	}
	if a.last.Purpose != provider.PurposeCompaction {
		t.Fatalf("summary request purpose = %q, want %q", a.last.Purpose, provider.PurposeCompaction)
	}
	// The run-scoped default must not override it either — the per-call tag is
	// the authoritative one (provider.EffectivePurpose).
	ctx := provider.WithPurpose(context.Background(), provider.PurposeForeground)
	if got := provider.EffectivePurpose(ctx, a.last); got != provider.PurposeCompaction {
		t.Fatalf("effective purpose inside a foreground run = %q, want %q", got, provider.PurposeCompaction)
	}
}
