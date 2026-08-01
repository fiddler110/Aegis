package engine

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/fiddler110/aegis/internal/provider"
)

// P52.3 — consecutive-tool-failure circuit breaker.
//
// IsError was computed for every tool result and emitted on the event stream,
// but never aggregated into anything: no counter, no threshold, no nudge, no
// abort. The engine's other stall guards (P28.3 zero-tool, P34.1 empty-answer,
// P34.2 tool-call-as-text, P2.6 step-limit summary, the P39.8 summarizer latch)
// all key on something other than a *failing* tool call, and loopDetector keys
// on a repeating tool-name+canonicalized-input signature — so the single most
// common local-model stall slips through every one of them: call edit_file, get
// "old_string not found", retry with a slightly different old_string, fail
// again, forever. Every signature is genuinely distinct, so no loop is
// detected, and the run burns all the way to maxIterations (default 40)
// producing nothing. The other three budgets don't catch it either: BudgetUSD
// is an explicit no-op for unpriced local usage, MaxTokensPerRun defaults to 0,
// and maxIterations is the thing being burned.
const (
	// toolFailureNudgeThreshold is how many failing tool rounds in a row earn a
	// corrective nudge. Three is deliberately early: the nudge costs one user
	// message and is retracted from the durable transcript afterwards, and by
	// the third identical failure the model has demonstrably stopped learning
	// from the error text on its own.
	toolFailureNudgeThreshold = 3
	// toolFailureAbortThreshold is how many consecutive rounds in which *every*
	// tool result was an error end the run outright. Set to twice the nudge
	// threshold so the model gets a full three further rounds to act on the
	// nudge before the run is killed.
	toolFailureAbortThreshold = 6
	// toolFailureMaxQuoted bounds how much of a tool's error text is quoted back
	// into the nudge/abort message, in runes. Tool errors can carry an entire
	// diff or file listing; the first few hundred characters are what identifies
	// the failure.
	toolFailureMaxQuoted = 300
)

// toolFailureTracker aggregates the per-round IsError signal for a single
// engine.Run. It is deliberately per-Run (not per-Engine): one Run already
// spans every tool round of a long local-model task, which is the failure mode
// being guarded, and carrying counts across runs would let an unrelated earlier
// failure contribute to a later abort.
//
// Two counters, because the failure shape has two variants:
//
//   - allErrorRounds — consecutive rounds in which *every* tool result was an
//     error. Nothing at all is progressing. This is the strict signal, and the
//     only one allowed to end the run.
//   - sameErrorRounds — consecutive rounds in which at least one call failed
//     with the *same* (normalized) error text as the previous round, regardless
//     of the arguments that produced it. This is what catches the
//     same-error-different-args shape directly, since differing arguments are
//     exactly what defeats loopDetector. It only ever earns a nudge: a round
//     with a successful call in it is still making progress somewhere, and
//     killing a run that is writing files is far worse than letting it spend a
//     few more iterations.
type toolFailureTracker struct {
	allErrorRounds  int
	sameErrorRounds int
	// lastErrorKey is the normalized text of the round's first failing result,
	// used only for equality against the next round.
	lastErrorKey string
	// lastErrorText and lastToolName describe that failure for the user-facing
	// nudge/abort message.
	lastErrorText string
	lastToolName  string
}

// record ingests one completed tool round. results is index-aligned with
// toolUses (both runTools paths guarantee this), so the failing call's tool
// name is recoverable for the message. A round with at least one successful
// result resets the strict counter; a round with no failures at all resets both.
func (t *toolFailureTracker) record(toolUses []provider.ToolUseBlock, results []provider.Block) {
	total, errs, firstErr := 0, 0, -1
	for i, b := range results {
		res, ok := b.(provider.ToolResultBlock)
		if !ok {
			continue
		}
		total++
		if res.IsError {
			errs++
			if firstErr < 0 {
				firstErr = i
			}
		}
	}
	if total == 0 || errs == 0 {
		t.reset()
		return
	}

	res := results[firstErr].(provider.ToolResultBlock)
	key := normalizeToolError(res.Content)
	if key != "" && key == t.lastErrorKey {
		t.sameErrorRounds++
	} else {
		t.sameErrorRounds = 1
	}
	t.lastErrorKey = key
	t.lastErrorText = truncateToolError(res.Content)
	t.lastToolName = ""
	if firstErr < len(toolUses) {
		t.lastToolName = toolUses[firstErr].Name
	}

	if errs == total {
		t.allErrorRounds++
	} else {
		t.allErrorRounds = 0
	}
}

// reset clears every counter, called when a tool round produced no errors at
// all. Without this a run that fails twice, recovers, and later fails twice
// again would accumulate toward an abort it never earned.
func (t *toolFailureTracker) reset() {
	t.allErrorRounds = 0
	t.sameErrorRounds = 0
	t.lastErrorKey = ""
	t.lastErrorText = ""
	t.lastToolName = ""
}

// rounds reports the larger of the two counters — the number of consecutive
// failing rounds the nudge should cite.
func (t *toolFailureTracker) rounds() int {
	if t.sameErrorRounds > t.allErrorRounds {
		return t.sameErrorRounds
	}
	return t.allErrorRounds
}

// shouldNudge reports whether the run has failed enough consecutive rounds to
// warrant a corrective nudge, by either signal.
func (t *toolFailureTracker) shouldNudge() bool {
	return t.allErrorRounds >= toolFailureNudgeThreshold || t.sameErrorRounds >= toolFailureNudgeThreshold
}

// shouldAbort reports whether the run should be terminated. Only the strict
// counter qualifies — see the type comment.
func (t *toolFailureTracker) shouldAbort() bool {
	return t.allErrorRounds >= toolFailureAbortThreshold
}

// toolLabel names the tool that produced the tracked failure, for messages.
func (t *toolFailureTracker) toolLabel() string {
	if t.lastToolName == "" {
		return "the tool"
	}
	return t.lastToolName
}

// toolFailureNudgePrefix opens the P52.3 corrective-nudge prompt and, like
// zeroToolNudgePrefix and emptyAnswerNudgePrefix, doubles as the marker its
// retraction keys on — so everything after it can quote the run's actual error
// text without breaking retractNudges.
const toolFailureNudgePrefix = "[Your recent tool calls have all failed"

// nudgeText builds the corrective prompt, quoting the error the model keeps
// hitting. Quoting the real text matters: the whole point is to break the
// "retry with a slightly different guess" cycle by making the model re-read
// actual state instead of re-guessing arguments.
func (t *toolFailureTracker) nudgeText() string {
	return fmt.Sprintf(toolFailureNudgePrefix+
		" — %d tool round(s) in a row produced errors and no usable result."+
		" The most recent failure was from %s: %q."+
		" Do not retry with another guessed variation of the same arguments — that is exactly what has"+
		" been failing. First re-read the file (or re-inspect the state) you are trying to change with a"+
		" read/search tool, so you are working from its exact current contents rather than from memory,"+
		" then make one corrected call based on what you actually observed. If the target does not exist"+
		" or the approach is wrong, say so and change plan instead of retrying.]",
		t.rounds(), t.toolLabel(), t.lastErrorText)
}

// ErrToolFailureLimit is wrapped by every P52.3 abort so a caller driving the
// engine can classify the stall rather than pattern-matching a message. It
// exists because the abort is terminal to the *engine* but not necessarily to
// the caller: the phased drive (internal/cli/chat_phased.go) treats it the same
// way it treats a context overflow — a resumable phase reset — since a model
// stuck re-guessing arguments is usually reasoning from a context that has
// accumulated its own failed attempts, and the on-disk `<!-- PENDING -->` files
// mean a fresh context can resume from truth. The daemon, by contrast, wants it
// surfaced to the user, which is what returning an error already does.
var ErrToolFailureLimit = errors.New("engine: aborting after consecutive failing tool rounds")

// abortError is the terminal condition surfaced to the client/TUI when the
// breaker trips, in the same shape as the loop-detector and budget aborts: a
// KindError event carrying this error, and the same error returned from Run.
func (t *toolFailureTracker) abortError() error {
	return fmt.Errorf("%w (%d in a row): %s keeps failing with: %q",
		ErrToolFailureLimit, t.allErrorRounds, t.toolLabel(), t.lastErrorText)
}

// toolErrorWhitespaceRe collapses whitespace runs so a tool error that wraps
// differently (or gains a trailing newline) between two otherwise-identical
// failures still compares equal.
var toolErrorWhitespaceRe = regexp.MustCompile(`\s+`)

// normalizeToolError produces the comparison key for "is this the same error as
// last round". Deliberately shallow — trim and collapse whitespace, nothing
// more. Normalizing harder (stripping numbers, paths, quoted fragments) would
// start merging genuinely different failures, and the strict all-error counter
// already covers the case where the text varies every time.
func normalizeToolError(s string) string {
	return strings.TrimSpace(toolErrorWhitespaceRe.ReplaceAllString(s, " "))
}

// truncateToolError renders an error for a user/model-facing message: collapsed
// to one line and capped at toolFailureMaxQuoted runes (rune-wise, so a
// multi-byte character is never split).
func truncateToolError(s string) string {
	s = normalizeToolError(s)
	r := []rune(s)
	if len(r) > toolFailureMaxQuoted {
		return string(r[:toolFailureMaxQuoted]) + "…"
	}
	return s
}
