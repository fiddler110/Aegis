package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// call is a one-line tool-use block for the gate tests below.
func call(name, input string) []provider.ToolUseBlock {
	return []provider.ToolUseBlock{{Name: name, Input: json.RawMessage(input)}}
}

// newTestLoopGuard builds a guard directly rather than through
// Engine.newLoopGuard, so a test can supply its own poll-exemption rule —
// Engine.pollExempt is a method that consults the tool registry, which is a
// different seam (covered by TestTurnSignatureExcludesPollExemptCalls).
func newTestLoopGuard(threshold int, pollExempt func(provider.ToolUseBlock) bool) *loopGuard {
	return &loopGuard{
		threshold:  threshold,
		detector:   newLoopDetector(threshold),
		pollExempt: pollExempt,
	}
}

// TestLoopGuardNilIsInert covers the disabled detector (LoopThreshold < 0),
// which P63.9 made a nil *loopGuard so Run carries no nil checks. Every method
// must therefore tolerate a nil receiver — the alternative is the nil-map panic
// class P63.1 and P63.8 both landed on.
func TestLoopGuardNilIsInert(t *testing.T) {
	e := &Engine{loopThreshold: -1}
	g := e.newLoopGuard()
	if g != nil {
		t.Fatal("a disabled threshold should produce a nil guard")
	}
	v := g.check(call("read_file", `{"path":"a.go"}`), 0)
	if v.abort != nil || v.notice != "" || v.nudge != "" || v.recorded {
		t.Errorf("nil guard should return an empty verdict, got %+v", v)
	}
	g.noteOutcome(v, nil) // must not panic
}

// TestLoopGuardRecordsUntilItTrips pins the verdict's `recorded` flag, which is
// what decides whether a round's outcome is attributed to this turn. Ordinary
// turns are recorded; the turn that *trips* the detector is not, because the
// window is reset underneath it and the outcome would land on nothing.
func TestLoopGuardRecordsUntilItTrips(t *testing.T) {
	g := newTestLoopGuard(3, nil)
	sig := call("read_file", `{"path":"a.go"}`)

	for i := 0; i < 2; i++ {
		v := g.check(sig, 0)
		if v.abort != nil || v.notice != "" {
			t.Fatalf("turn %d should not have tripped: %+v", i, v)
		}
		if !v.recorded {
			t.Fatalf("turn %d should be recorded", i)
		}
		g.noteOutcome(v, okResults())
	}

	v := g.check(sig, 0)
	if v.notice == "" || v.nudge == "" {
		t.Fatalf("a succeeding cycle should earn a notice and a nudge, got %+v", v)
	}
	if v.abort != nil {
		t.Errorf("a succeeding cycle should be recoverable, got abort %v", v.abort)
	}
	if v.recorded {
		t.Error("the triggering turn must not be recorded — its window was just reset")
	}
}

// TestLoopGuardErrorCycleAborts: a cycle whose rounds errored is fatal on the
// first trigger, with no nudge — the model has already exhausted its own
// recovery (P53.2b).
func TestLoopGuardErrorCycleAborts(t *testing.T) {
	g := newTestLoopGuard(3, nil)
	sig := call("edit_file", `{"path":"a.go"}`)

	for i := 0; i < 2; i++ {
		v := g.check(sig, 0)
		g.noteOutcome(v, errResults())
	}
	v := g.check(sig, 0)
	if v.abort == nil {
		t.Fatalf("an erroring cycle should abort, got %+v", v)
	}
	if !errors.Is(v.abort, ErrLoopDetected) {
		t.Errorf("abort should wrap ErrLoopDetected, got %v", v.abort)
	}
	if strings.Contains(v.abort.Error(), "corrective prompt") {
		t.Errorf("a first-trigger error cycle was never nudged, so the message must not blame a corrective: %v", v.abort)
	}
	if v.notice != "" || v.nudge != "" {
		t.Errorf("an aborting verdict must carry no nudge: %+v", v)
	}
}

// TestLoopGuardSecondTriggerAborts: once the run has spent its one corrective,
// a fresh cycle ends the run and says the corrective is what failed. nudgesSpent
// is passed in rather than owned here — nudgeState is the single owner of "has
// this family been injected" — so this asserts the guard honors it.
func TestLoopGuardSecondTriggerAborts(t *testing.T) {
	g := newTestLoopGuard(3, nil)
	sig := call("read_file", `{"path":"a.go"}`)

	for i := 0; i < 2; i++ {
		v := g.check(sig, 1)
		g.noteOutcome(v, okResults())
	}
	v := g.check(sig, 1)
	if v.abort == nil {
		t.Fatalf("a second cycle after a spent corrective should abort, got %+v", v)
	}
	if !strings.Contains(v.abort.Error(), "the corrective prompt did not break the cycle") {
		t.Errorf("abort should name the failed corrective, got %v", v.abort)
	}
}

// TestLoopGuardIgnoresPollOnlyTurns is the P53.2(a) exemption at the gate seam:
// a turn made up entirely of polls yields no verdict at all — not an empty
// signature, which would form a perfect period-1 cycle out of waiting.
func TestLoopGuardIgnoresPollOnlyTurns(t *testing.T) {
	pollExempt := func(tu provider.ToolUseBlock) bool { return tu.Name == "task_status" }
	g := newTestLoopGuard(3, pollExempt)

	for i := 0; i < 6; i++ {
		v := g.check(call("task_status", `{"id":"1"}`), 0)
		if v.abort != nil || v.notice != "" {
			t.Fatalf("poll-only turn %d must never trip the detector: %+v", i, v)
		}
		if v.recorded {
			t.Fatalf("poll-only turn %d must not be recorded", i)
		}
		g.noteOutcome(v, okResults())
	}
}

// TestLoopGuardNoteOutcomeSkipsUnrecordedTurns guards the misattribution the
// `recorded` flag exists to prevent: feeding an unrecorded turn's results to the
// detector would stamp them onto whatever turn happens to sit at the end of the
// window, turning a succeeding (recoverable) cycle into a fatal one.
func TestLoopGuardNoteOutcomeSkipsUnrecordedTurns(t *testing.T) {
	g := newTestLoopGuard(3, nil)
	sig := call("read_file", `{"path":"a.go"}`)

	v := g.check(sig, 0)
	g.noteOutcome(v, okResults())

	// An unrecorded verdict carrying errors must not reclassify the turn above.
	g.noteOutcome(loopVerdict{recorded: false}, errResults())
	if g.detector.cycleHadError() {
		t.Error("an unrecorded turn's errors were attributed to a recorded turn")
	}
}

func okResults() []provider.Block {
	return []provider.Block{provider.ToolResultBlock{Content: "fine"}}
}

func errResults() []provider.Block {
	return []provider.Block{provider.ToolResultBlock{Content: "boom", IsError: true}}
}
