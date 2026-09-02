package engine

import (
	"context"
	"time"

	"github.com/fiddler110/aegis/internal/tool"
)

// toolPreferFinish and timeAfterFunc are seams over tool.EffectivePreferFinish
// and time.AfterFunc so a test can stub the preference check and avoid an
// actual multi-second sleep for the grace-timer fallback, the same pattern
// gitpr.go's scanPRTextForSecrets uses.
var (
	toolPreferFinish = tool.EffectivePreferFinish
	timeAfterFunc    = time.AfterFunc
)

// P67.10: a stop request has always meant "cancel the run's context right
// now" everywhere in the codebase — the client tearing down its HTTP
// request, POST /sessions/{id}/stop, the TUI's ESC/Ctrl+C. That is correct
// for a long or expensive call, but a call cheap enough to just let finish
// (tool.InterruptPreference) gets no benefit from it: cancelling mid-flight
// costs exactly the same as letting it return in the next instant, and the
// caller loses whatever partial work that call was about to produce cleanly.
//
// RequestStop is the soft alternative. hardCancel is always the caller's real
// context.CancelFunc for this run — RequestStop never owns cancellation
// itself, matching every other stop path in the codebase; it only ever
// decides *when* to call it.
//
//   - hard, or hardCancel == nil: calls hardCancel immediately (or does
//     nothing if it's nil) — today's only behavior, unchanged.
//   - soft: if nothing is in flight right now, or any in-flight call's tool
//     doesn't prefer to finish (tool.EffectivePreferFinish), falls back to
//     hardCancel immediately — a stop request is never silently ignored.
//     Otherwise it arms the graceful-stop flag the run's main loop consumes
//     at the next safe point (the end of the current tool round) and arms
//     gracefulStopGrace as a safety net, so a soft stop can never itself
//     become the reason a run hangs.
//
// Returns true if the stop will happen softly (flag armed, no immediate
// cancel), false if it took effect immediately (hard, no in-flight calls, or
// a non-preferring call in flight).
func (e *Engine) RequestStop(hard bool, hardCancel context.CancelFunc) bool {
	if hardCancel == nil {
		return false
	}
	if hard {
		hardCancel()
		return false
	}
	calls := e.snapshotInFlight()
	if len(calls) == 0 {
		hardCancel()
		return false
	}
	for _, c := range calls {
		t, ok := e.tools.Get(c.name)
		if !ok || !toolPreferFinish(t, c.input) {
			hardCancel()
			return false
		}
	}

	e.gracefulStopMu.Lock()
	e.gracefulStopRequested = true
	if e.gracefulStopTimer != nil {
		e.gracefulStopTimer.Stop()
	}
	e.gracefulStopTimer = timeAfterFunc(gracefulStopGrace, hardCancel)
	e.gracefulStopMu.Unlock()
	return true
}

// consumeGracefulStop reports whether a soft RequestStop is outstanding, and
// clears it (and its safety-net timer) either way. Called once per turn, at
// the one safe point a soft stop is honored: right after a tool round
// finishes and its results are already appended to the conversation, so
// ending the run here never discards a result the model or the client has
// not seen.
func (e *Engine) consumeGracefulStop() bool {
	e.gracefulStopMu.Lock()
	defer e.gracefulStopMu.Unlock()
	requested := e.gracefulStopRequested
	e.gracefulStopRequested = false
	if e.gracefulStopTimer != nil {
		e.gracefulStopTimer.Stop()
		e.gracefulStopTimer = nil
	}
	return requested
}
