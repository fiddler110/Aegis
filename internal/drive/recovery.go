package drive

import (
	"errors"
	"fmt"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/provider"
)

// maxPhase6OverflowResets bounds the P47.7 phase-6 overflow-reset loop: a
// context overflow during a verify-fix or quality turn is resumable (the on-disk
// suite is the source of truth, so a fresh context re-reads it), but only this
// many times before stopping, so a model that overflows every attempt still
// terminates rather than looping forever. Sized like MaxVerifyRounds — a few
// resets is generous; more means the phase-6 fill is too large for this
// model/window even after the P47.5b escalation.
const maxPhase6OverflowResets = 3

// phase6OverflowAction is recoverPhase6Overflow's verdict for a failed phase-6
// turn: whether to retry it, stop the drive cleanly, or surface the error.
type phase6OverflowAction int

const (
	// overflowNotHandled: the error is not a context overflow — the caller
	// surfaces it as a terminal engine error.
	overflowNotHandled phase6OverflowAction = iota
	// overflowRetry: a recoverable overflow within budget — the caller resets to
	// a fresh context (implicit in runPhase6Turn) and loops again.
	overflowRetry
	// overflowStop: the reset budget is exhausted — a resumable stop notice was
	// printed and the caller ends the drive cleanly (returns nil).
	overflowStop
	// loopRetry: a recoverable reasoning-loop abort within budget (P57.1). Like
	// overflowRetry the caller resets to a fresh context and loops again, but it
	// additionally escalates the next prompt with StuckLoopDirective — the reset
	// alone drops the wrong theory, the directive keeps the model from
	// re-deriving it. Call sites must handle it alongside overflowRetry; it is
	// deliberately a distinct value so a site that forgets fails loudly (its
	// `default: return err`) instead of silently losing the escalation.
	loopRetry
)

// recoverPhase6Overflow classifies a phase-6 turn error (P47.7). A context
// overflow during a verify-fix or quality turn is resumable — the on-disk suite
// is the source of truth, so a fresh context re-reads it — so on an overflow it
// escalates the window (P47.5b), counts the reset against
// maxPhase6OverflowResets, and returns overflowRetry (the next loop iteration
// re-runs the mechanical checks and re-issues the turn; runPhase6Turn always
// builds a fresh conversation, so the reset is implicit). Once the reset budget
// is exhausted it prints a resumable stop notice and returns overflowStop. A
// non-overflow error returns overflowNotHandled so the caller surfaces it. This
// is the phase-6 parity for the content phases' P47.2 overflow-reset: without it
// a phase-6 overflow died on the raw `ollama: response truncated at the context
// limit` with no reset, no verify rounds 2/3, and no quality stamp (2026-07-27,
// FirewallRiskRater). `where` names the step for the notices.
func (st *State) recoverPhase6Overflow(err error, where string, resets *int) phase6OverflowAction {
	if !provider.IsContextOverflowError(err) {
		return overflowNotHandled
	}
	if *resets++; *resets > maxPhase6OverflowResets {
		st.Logger.Warn("phased drive: phase-6 context overflow persists after max resets", "where", where, "resets", maxPhase6OverflowResets)
		fmt.Fprintf(st.ErrOut, "\n[notice: %s kept overflowing the context after %d reset(s); stopping with an unverified suite — re-run to resume, or reduce the remaining fill]\n", where, maxPhase6OverflowResets)
		return overflowStop
	}
	st.tryEscalateWindow(where)
	st.Logger.Warn("phased drive: phase-6 context overflowed, resetting to a fresh context and retrying", "where", where, "reset", *resets, "err", err)
	fmt.Fprintf(st.ErrOut, "\n[notice: context overflowed during %s; resetting to a fresh context and re-reading the suite from disk (reset %d/%d)]\n", where, *resets, maxPhase6OverflowResets)
	return overflowRetry
}

// maxToolFailureResets bounds how many times one phase (or the phase-6 loop)
// may be reset after the P52.3 circuit breaker trips. Deliberately tighter than
// maxPhase6OverflowResets: an overflow is a mechanical limit a fresh context
// genuinely clears, whereas a run whose every tool call fails may be a real
// impasse (a file that isn't there, a script that won't run), and re-entering it
// indefinitely would burn the same hours the breaker exists to save. Two fresh
// attempts, then a resumable stop.
const maxToolFailureResets = 2

// recoverToolFailureStall classifies a P52.3 consecutive-tool-failure abort the
// way the drive classifies a context overflow: terminal to the engine, but
// resumable at the phase level. Without it the breaker would be a regression for
// the phased drive — a stall that used to burn to maxIterations and continue now
// returns a hard error, and Run's default is to treat any
// non-overflow engine error as fatal, so an unattended run would die where it
// previously limped on. That is precisely the manual-re-invocation failure the
// P47.x/P50.x batches exist to remove.
//
// A fresh context is also the *right* remedy here, not just a compatible one:
// the breaker fires when a model keeps re-guessing arguments, which it does by
// reasoning from a context now dense with its own failed attempts. Dropping that
// context and re-reading the on-disk `<!-- PENDING -->` files is a strictly
// better starting point than the one that produced the failures.
//
// Unlike the overflow path it does not escalate the serving window — the window
// is not the problem — and it keeps its own reset budget so a run that both
// overflows and stalls cannot spend one budget on the other. Reuses the
// phase6OverflowAction verdict enum so the call sites' switches need no new case.
func (st *State) recoverToolFailureStall(err error, where string, resets *int) phase6OverflowAction {
	if !errors.Is(err, engine.ErrToolFailureLimit) {
		return overflowNotHandled
	}
	if *resets++; *resets > maxToolFailureResets {
		st.Logger.Warn("phased drive: tool failures persist after max resets", "where", where, "resets", maxToolFailureResets, "err", err)
		fmt.Fprintf(st.ErrOut, "\n[notice: %s kept failing every tool call after %d reset(s); stopping — re-run to resume, or check that the run directory and skill scripts are reachable]\n", where, maxToolFailureResets)
		return overflowStop
	}
	st.Logger.Warn("phased drive: every tool call failed, resetting to a fresh context and retrying", "where", where, "reset", *resets, "err", err)
	fmt.Fprintf(st.ErrOut, "\n[notice: every tool call failed during %s; resetting to a fresh context and re-reading from disk (reset %d/%d)]\n", where, *resets, maxToolFailureResets)
	return overflowRetry
}

// maxReasoningLoopResets bounds how many times one phase (or the phase-6 loop)
// may be reset after the engine's loop guard aborts a turn (P57.1). Sized like
// maxToolFailureResets rather than maxPhase6OverflowResets, and for the same
// reason: an overflow is a mechanical limit a fresh context genuinely clears,
// whereas a model looping on its own reasoning may be looping on something real
// (a check it cannot satisfy), and re-entering indefinitely would burn the hours
// the abort exists to save. Two fresh attempts, then a resumable stop.
const maxReasoningLoopResets = 2

// recoverReasoningLoop classifies an engine loop-guard abort (P57.1) the way the
// drive already classifies a context overflow and a tool-failure stall: terminal
// to the engine, resumable at the phase level.
//
// Before this, a loop abort was the one remaining engine error that killed an
// otherwise-working unattended drive outright. That is what ended the 2026-08-03
// P38.1 re-confirmation run: phase 6 correctly caught real cross-file defects and
// correctly re-opened the owning content phase (P47.9), but the re-opened phase
// convinced itself of a T0-vs-T01 zero-padding offset that did not exist,
// re-derived it identically five turns running, and the engine's single
// corrective nudge did not break the cycle — so the whole drive aborted with the
// suite still verify-failing. A second manual invocation with a fresh context
// resolved every defect immediately, which is the evidence that the *context* is
// the defect, not the model and not the checks.
//
// A fresh context is therefore the right remedy, not merely a compatible one:
// the loop is the model reasoning from a context that now contains four
// restatements of its own wrong theory, and nothing in that context contradicts
// it. Dropping it and re-reading the verifier's report from disk starts from
// evidence instead. The caller pairs the reset with StuckLoopDirective so the
// fresh turn is told the report is authoritative rather than invited to
// re-derive the mismatch — the same "here is what is wrong" shift scaffold.py
// (P38.4) made for structure.
//
// It keeps its own reset budget, separate from the overflow and tool-failure
// ones, so a run that hits two failure modes cannot spend one budget on the
// other. Unlike the overflow path it does not escalate the serving window — the
// window is not the problem.
func (st *State) recoverReasoningLoop(err error, where string, resets *int) phase6OverflowAction {
	if !errors.Is(err, engine.ErrLoopDetected) {
		return overflowNotHandled
	}
	if *resets++; *resets > maxReasoningLoopResets {
		st.Logger.Warn("phased drive: model kept looping after max resets", "where", where, "resets", maxReasoningLoopResets, "err", err)
		fmt.Fprintf(st.ErrOut, "\n[notice: the model kept repeating the same tool calls during %s after %d fresh-context reset(s); stopping — the suite on disk is intact, so re-run to resume]\n", where, maxReasoningLoopResets)
		return overflowStop
	}
	st.Logger.Warn("phased drive: model looped on its own reasoning, resetting to a fresh context and retrying", "where", where, "reset", *resets, "err", err)
	fmt.Fprintf(st.ErrOut, "\n[notice: the model repeated the same tool calls during %s without making progress; resetting to a fresh context and re-reading from disk with the verifier's report as ground truth (reset %d/%d)]\n", where, *resets, maxReasoningLoopResets)
	return loopRetry
}
