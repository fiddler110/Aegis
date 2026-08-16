package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fiddler110/aegis/internal/config"
)

// stallBound is the shipped cost.max_turn_stall as a duration. Read from
// config rather than restated, so raising the default moves this test with it
// instead of leaving a second copy of 900 behind.
var stallBound = time.Duration(config.DefaultMaxTurnStallSec) * time.Second

// perCallToolTimeouts enumerates every bound in this package that governs a
// *single* uninterrupted wait — one subprocess, one sub-agent, one HTTP call.
//
// Each must sit below the stall bound, because the engine's detector is beaten
// on the two edges of a tool execution and on provider stream events, and none
// of those fire while one of these waits is in progress. A per-call bound above
// the stall bound therefore does not produce its own precise error; it produces
// a fatal ErrTurnStalled — "the turn is hung, not slow" — which every drive
// reset ladder declines as unrecoverable. That is the P66.8 / ARCH-04 defect:
// a legitimately long fan-out diagnosed as the one thing a reset cannot fix.
//
// A new capped wait belongs in this map. If it cannot pass, it is not a wait
// the stall detector can backstop.
var perCallToolTimeouts = map[string]time.Duration{
	"agent — single spawn (agent.go)":             maxAgentDuration,
	"agent — one workflow teammate (spawn)":       maxAgentDuration,
	"agent — one debate role (runRole)":           maxAgentDuration,
	"agent — background job (spawnBackground)":    maxAgentDuration,
	"git — one git subprocess (gitTimeout)":       gitTimeout,
	"git — one gh subprocess (gitpr.go)":          gitTimeout,
	"git — pre-commit test command":               time.Duration(config.DefaultPreCommitTestTimeoutSec) * time.Second,
	"latex — one compiler run (latex.go:140)":     3 * time.Minute,
	"latex — one conversion run (latex.go:471)":   2 * time.Minute,
	"shell — checkpoint pre-scan":                 shellCheckpointTimeout,
	"shell — checkpoint post-scan":                shellCheckpointTimeout,
	"shell — caller-supplied timeout_sec ceiling": time.Duration(maxShellTimeoutSec) * time.Second,
}

// The provider admission queue is the fourth wait P66.8 covers and is
// deliberately absent from both maps: it has no timeout at all (a queued
// request waits for the slot or for its caller's cancellation), so there is no
// bound here to check. It is made safe the other way — admissionAdapter.Stream
// beats every admissionBeatInterval while queued — because a queue wait is the
// one wait in this codebase known to be alive while producing nothing.

// aggregateToolTimeouts enumerates the bounds that are deliberately *above* the
// stall bound, each with the reason it is allowed to be.
//
// The only admissible reason is that the wait is decomposed into per-call waits
// from the map above and reports observable activity between them, so the
// aggregate can never be reached without a beat. "It is a long operation" is
// not a reason — that is exactly the case the defect was filed against.
var aggregateToolTimeouts = map[string]string{
	"agent — workflow batch (len(agents)+1 teammates)": "spawn bounds each teammate at maxAgentDuration and beats on every completion",
	"agent — debate (2*rounds+2 roles)":                "runRole bounds each role at maxAgentDuration and beats on every completion",
}

// TestToolTimeoutsStayUnderTheStallBound is the P66.8 invariant, mirroring
// TestResultCapsCanBindBeforeTheContextWindow: that test asks whether a result
// cap can bind before the context window does, this one asks whether a timeout
// can fire before the stall detector does.
//
// Mutation check (run 2026-08-16): restoring agent.go's pre-P66.8 shape — the
// workflow batch context used directly as the per-teammate wait, i.e.
// maxAgentDuration*(len(agents)+1) at three agents — fails this at 40m0s
// against the 15m0s bound, which is the reported defect. Setting stallBound to
// 5 minutes fails on all six 10-minute entries (the four agent waits, the
// pre-commit test and the shell ceiling) and on neither latex entry, confirming
// the bound is not vacuous and discriminates at the current values.
func TestToolTimeoutsStayUnderTheStallBound(t *testing.T) {
	for name, d := range perCallToolTimeouts {
		if d <= 0 {
			t.Errorf("%s has a non-positive bound (%s) — an unbounded wait is invisible to the stall detector", name, d)
			continue
		}
		if d >= stallBound {
			t.Errorf("%s waits up to %s, at or over the %s stall bound — it would surface as a fatal ErrTurnStalled instead of its own error",
				name, d, stallBound)
		}
	}
	for name, reason := range aggregateToolTimeouts {
		if strings.TrimSpace(reason) == "" {
			t.Errorf("%s is exempted from the stall bound with no stated reason", name)
		}
	}
}

// TestEveryToolTimeoutIsAccountedFor is the grep-the-source half, the same
// instrument as TestEveryRegisterCallSiteDecidesTheLocalProfile: it counts the
// context.WithTimeout call sites in this package and requires the two maps
// above to name all of them.
//
// Without it the enumeration is a snapshot that silently stops being an
// enumeration the first time someone adds a timeout, which is precisely how the
// two agent bounds got 40 and 80 minutes above a limit the docs claimed they
// were under.
func TestEveryToolTimeoutIsAccountedFor(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob: %v", err)
	}
	sites := 0
	perFile := map[string]int{}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if n := strings.Count(string(b), "context.WithTimeout("); n > 0 {
			sites += n
			perFile[f] = n
		}
	}

	// perCallToolTimeouts carries one entry with no context.WithTimeout site of
	// its own — the shell tool clamps timeout_sec and hands the duration to
	// exec rather than to a context — so it is subtracted from the accounting.
	named := len(perCallToolTimeouts) - 1 + len(aggregateToolTimeouts)
	if sites != named {
		t.Errorf("internal/tool/builtin has %d context.WithTimeout call sites (%v) but the stall-bound tables name %d.\n"+
			"A new timeout must be added to perCallToolTimeouts (and kept under the %s stall bound) or to aggregateToolTimeouts with a stated reason.",
			sites, perFile, named, stallBound)
	}
}
