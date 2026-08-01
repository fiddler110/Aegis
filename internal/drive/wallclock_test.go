package drive

import (
	"fmt"
	"io"
	"log/slog"
	"testing"

	"github.com/fiddler110/aegis/internal/engine"
)

// TestWallClockAbortIsFatalToTheDrive pins the P52.15 interaction with the two
// resumable-reset paths. The drive deliberately recovers from a context
// overflow (P47.2/P47.7) and from a tool-failure stall (P52.3) by dropping the
// context and resuming from the on-disk PENDING markers — but a wall-clock
// abort must NOT join them.
//
// The distinction is intent, not mechanism: an overflow and an argument-guessing
// stall are conditions a fresh context genuinely clears, whereas a wall-clock
// limit is an operator saying "stop after N minutes". Resetting and continuing
// past it would spend more time than was authorized, which is the one thing the
// bound exists to prevent. Both classifiers must decline it.
func TestWallClockAbortIsFatalToTheDrive(t *testing.T) {
	st := &State{ErrOut: io.Discard, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	err := fmt.Errorf("%w: ran 10m0s of a 10m0s limit", engine.ErrWallClockLimit)

	overflowResets, failureResets := 0, 0
	if got := st.recoverPhase6Overflow(err, "the STRIDE phase", &overflowResets); got != overflowNotHandled {
		t.Errorf("overflow recovery must decline a wall-clock abort, got %v", got)
	}
	if got := st.recoverToolFailureStall(err, "the STRIDE phase", &failureResets); got != overflowNotHandled {
		t.Errorf("tool-failure recovery must decline a wall-clock abort, got %v", got)
	}
	if overflowResets != 0 || failureResets != 0 {
		t.Errorf("a declined error must not consume a reset budget, got overflow=%d failure=%d", overflowResets, failureResets)
	}
}
