package drive

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/fiddler110/aegis/internal/engine"
)

// TestTurnStallIsFatalToTheDrive pins P39.17's interaction with the three
// resumable-reset ladders. The drive deliberately recovers from a context
// overflow (P47.2/P47.7), a tool-failure stall (P52.3) and a reasoning loop
// (P57.1) by dropping the context and resuming from the on-disk PENDING
// markers. A per-turn stall must not join them.
//
// The reason is what each ladder is actually claiming. All three recoveries rest
// on the same premise: the *context* is the defect, so a fresh one clears it — a
// model reasoning from its own failed edits, or from four restatements of its
// own wrong theory. A stall makes no claim about the context at all. Something
// is wedged (the backend, the transport, a sandbox exec), and a fresh
// conversation would be handed straight back to it. Auto-retrying would also
// re-create exactly the condition P39.17 was filed against: an unattended run
// burning hours while looking healthy. Fatal and loud is the point; the suite on
// disk survives, so a re-run still resumes.
func TestTurnStallIsFatalToTheDrive(t *testing.T) {
	st := &State{ErrOut: io.Discard, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	err := fmt.Errorf("%w: no model output and no tool activity for 15m2s (limit 15m0s)", engine.ErrTurnStalled)

	overflowResets, failureResets, loopResets := 0, 0, 0
	if got := st.recoverPhase6Overflow(err, "the STRIDE phase", &overflowResets); got != overflowNotHandled {
		t.Errorf("overflow recovery must decline a stall abort, got %v", got)
	}
	if got := st.recoverToolFailureStall(err, "the STRIDE phase", &failureResets); got != overflowNotHandled {
		t.Errorf("tool-failure recovery must decline a stall abort, got %v", got)
	}
	if got := st.recoverReasoningLoop(err, "the STRIDE phase", &loopResets); got != overflowNotHandled {
		t.Errorf("loop recovery must decline a stall abort, got %v", got)
	}
	if overflowResets != 0 || failureResets != 0 || loopResets != 0 {
		t.Errorf("a declined error must not consume a reset budget, got overflow=%d failure=%d loop=%d",
			overflowResets, failureResets, loopResets)
	}
}
