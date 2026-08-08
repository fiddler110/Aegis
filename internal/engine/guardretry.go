package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/fiddler110/aegis/internal/checkpoint"
	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/provider"
)

// guardCorrectivePrefix opens every guard corrective message; besides framing
// the retry prompt, it is the marker retractGuardCorrectives keys on to strip
// retry scaffolding from the durable transcript once the run settles. It lives
// with the code that writes it, matching loopNudgePrefix in loopdetect.go —
// retraction reads the marker, the concern owns it.
const guardCorrectivePrefix = "[Your previous response did not pass output validation: "

// guardGate is the fourth and last concern lifted out of Engine.Run under
// P63.9: the output guard's verdict handling, its bounded corrective retry, the
// P59.8 schema the retry is re-asked under, and the P27.16 rollback when the
// retries run out.
//
// Each of the four passes found the state in a different shape, and this one is
// the shape the other three do not cover:
//
//	pass 1 (budgets)     state owned by nothing — a bare local hand-threaded
//	                     into five call sites.
//	pass 2 (loop)        per-turn state living at run scope.
//	pass 3 (compaction)  run-scoped state living with the wrong owner.
//	pass 4 (this)        inter-turn carry state whose set, consume and clear
//	                     were three separate sites.
//
// `constrainNext` was the tell. Its lifetime is neither per-turn nor
// run-scoped: the guard sets it at the *end* of turn N and the turn setup
// consumes it at the *start* of turn N+1 — exactly one iteration boundary, and
// no longer, because leaving the constraint latched would silently re-shape
// every later turn of the run. Nothing enforced that. It was declared among
// Run's run-scoped variables, read at one site to decide `suppressTools`, then
// read again ~200 lines later and manually set back to nil, with a comment at
// each site explaining the discipline the code did not encode.
//
// `takeFormat` makes the clearing structural instead: there is one site, it
// returns the carry and empties it in the same expression, and a caller cannot
// forget the second half because there is no second half. The comments that
// used to explain the discipline are now explaining the P59.8 *decision*, which
// is the only part that was ever load-bearing.
//
// # What deliberately does not move
//
// The retry **count** stays in nudgeState and is passed in, exactly as pass 2
// did with the loop-nudge count and as the tree already does for
// toolFailureTracker/nudgeState. retractAll reads that table to decide whether
// guard scaffolding needs stripping from the durable transcript; a second copy
// here could disagree with it.
//
// # The coupling this pass inherits and does not close
//
// Pass 3 recorded that compaction rewrites the middle of conv.Messages while
// retraction finds correctives by scanning that same list for a marker prefix,
// so a compaction pass can delete a corrective that nudgeState still believes
// is present. Guard correctives are subject to exactly that: retractAll strips
// them via retractGuardCorrectives, keyed on guardCorrectivePrefix, and a
// summarized-away corrective leaves guardRetries counted but nothing to strip.
// It is benign — the count only gates further retries, and a stripped-or-absent
// corrective is the same durable transcript either way — and closing it is a
// behavior change, which these passes are gated against. It is named here
// because this is the second concern to key on the same mechanism, which is
// what makes it worth a fix on its own rather than a note in two files.
//
// A nil *guardGate is a run with no output guard, and every method tolerates
// it, so Run carries no nil checks of its own.
type guardGate struct {
	validate guard.Func
	// maxRetries is resolved once here rather than recomputed inside the turn
	// loop; a configured value of 0 or less means one attempt, not none.
	maxRetries int
	// schema is the P59.8 JSON Schema a corrective retry is re-asked under, from
	// config. It is the *source* of the carry below, never the carry itself.
	schema       json.RawMessage
	collectFiles func(context.Context) []guard.FileContent
	logger       *slog.Logger

	// pending is the carry: the schema the next turn — and only the next turn —
	// decodes under. Set per retry rather than once per run, so a constraint
	// cannot leak onto an unrelated later turn, and emptied by takeFormat rather
	// than by the caller remembering to.
	pending json.RawMessage
}

// newGuardGate builds the gate for a run, or returns nil when no output guard
// is configured.
func (e *Engine) newGuardGate() *guardGate {
	if e.outputGuard == nil {
		return nil
	}
	maxRetries := e.outputGuardMax
	if maxRetries <= 0 {
		maxRetries = 1
	}
	return &guardGate{
		validate:     e.outputGuard,
		maxRetries:   maxRetries,
		schema:       e.outputGuardFmt,
		collectFiles: e.collectWrittenFiles,
		logger:       e.logger,
	}
}

// takeFormat returns the schema this turn should decode under and clears the
// carry, so the constraint applies to exactly one turn.
//
// P59.8: a schema-guard corrective retry is sent with decoding constrained to
// the required shape *and* with tools off — the remaining task at that point is
// "emit this object", and a grammar plus a tool schema pull the same turn in two
// directions. A non-nil return is therefore also what tells the caller to
// suppress tools.
func (g *guardGate) takeFormat() json.RawMessage {
	if g == nil {
		return nil
	}
	f := g.pending
	g.pending = nil
	return f
}

// review runs the output guard over a final answer and reports whether the run
// should retry rather than finish.
//
// A true return means a corrective has been appended to conv and the next turn
// is armed with the schema; the caller spends one retry from its own count and
// continues. False means the answer stands — either it passed, or the guard was
// skipped, or the retries are gone and the failing answer is being surfaced
// (with this turn's writes rolled back). Nothing is emitted for an empty final
// answer, which is not something a rubric can judge.
//
// retriesSpent is the run's guard-retry count, held by nudgeState. Passing it
// keeps one owner for "have we already corrected this" rather than two that can
// disagree.
func (g *guardGate) review(ctx context.Context, conv *Conversation, emit EmitFunc, final string, toolRoundsCompleted, retriesSpent int) bool {
	if g == nil || final == "" {
		return false
	}

	ok, reason, status := g.validate(ctx, guard.Input{Text: final, Files: g.collectFiles(ctx)})
	g.logger.Debug("output guard result", "passed", ok, "guard_status", string(status))

	if ok {
		// Genuine pass and fail-open-skip both set ok=true, but the caller can
		// now tell them apart via GuardStatus — without this, "the guard
		// validated and passed" and "the guard silently never ran" were
		// byte-for-byte indistinguishable (FIND-16).
		emit(Event{Kind: KindGuard, GuardPassed: true, GuardStatus: string(status)})
		return false
	}

	if retriesSpent < g.maxRetries {
		g.pending = g.schema
		emit(Event{Kind: KindGuard, GuardPassed: false, GuardReason: reason, GuardStatus: string(status), GuardRetrying: true})
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
			provider.TextBlock{Text: correctiveText(reason, toolRoundsCompleted > 0)},
		}})
		return true
	}

	g.surfaceFailure(ctx, emit, reason, status)
	return false
}

// surfaceFailure emits the terminal guard verdict once the retries are spent,
// after quarantining anything this turn wrote.
//
// P27.16 (FIND-15): the failing response is about to be surfaced anyway, so
// rolling back the file(s) this turn wrote is better than leaving a bad write on
// disk. It reuses the same per-turn checkpoint Snapshotter write_file/edit_file
// already capture pre-write content into (internal/checkpoint), so this is a
// plain restore rather than a new mechanism. A nil Snapshotter — no checkpoint
// store wired in, e.g. tests or an embedded engine without one — makes
// RestoreFiles a no-op, so rollback is skipped rather than erroring, preserving
// the retry-then-surface behavior in that case.
func (g *guardGate) surfaceFailure(ctx context.Context, emit EmitFunc, reason string, status guard.Status) {
	finalReason := "surfacing the response after " + itoa(g.maxRetries) + " failed validation attempt(s): " + reason
	restored, rerr := checkpoint.SnapshotterFrom(ctx).RestoreFiles(ctx)
	if rerr != nil {
		g.logger.Warn("guard fail: rollback of files written this turn failed", "err", rerr)
	} else if restored > 0 {
		g.logger.Warn("guard fail: rolled back files written this turn", "files_restored", restored)
		finalReason += fmt.Sprintf(" — rolled back %d file(s) written this turn", restored)
	}
	emit(Event{Kind: KindGuard, GuardPassed: false, GuardStatus: string(status),
		GuardReason: finalReason, GuardFilesRestored: restored})
}

// correctiveText builds the retry prompt. wroteFiles adds the clause telling the
// model to fix the artifact rather than its description of it, which is only
// meaningful once a tool round has actually run.
func correctiveText(reason string, wroteFiles bool) string {
	s := guardCorrectivePrefix + reason +
		". This means the actual deliverable is incomplete or unpolished, not just its" +
		" description. Do not reply with only an acknowledgment, a plan, or a promise to" +
		" fix it later — that will fail validation again."
	if wroteFiles {
		s += " If you already wrote or edited a file for this task, call your file" +
			" tools now (e.g. edit_file/write_file) to fix the real content directly."
	}
	return s + " Finish the work now, then give a corrected final answer that reflects" +
		" the fixed result. Address the original request only — never mention this" +
		" validation step or include verdict words like PASS or FAIL in your answer.]"
}
