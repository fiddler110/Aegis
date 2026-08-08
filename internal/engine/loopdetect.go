package engine

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/fiddler110/aegis/internal/provider"
)

// maxLoopPeriod bounds the cycle lengths loopDetector checks for, beyond the
// plain "all identical" (period 1) case it originally supported. A model
// alternating between two (or a few) distinct tool calls — A, B, A, B, … —
// is just as stuck as one repeating a single call, but exact "last N
// signatures are all equal" matching never fires on that pattern.
const maxLoopPeriod = 4

// loopOutcome records what happened when a recorded turn's tools actually ran.
// The loop gate calls record *before* the round executes, so the turn that
// trips the detector has no outcome yet — hence the third state.
type loopOutcome uint8

const (
	loopOutcomeUnknown loopOutcome = iota // recorded, tools not finished (or never ran)
	loopOutcomeOK                         // every tool result in the round succeeded
	loopOutcomeError                      // at least one tool result in the round errored
)

// loopDetector spots a stuck agent that issues the same tool calls turn after
// turn — either the exact same call every time, or a short cycle of a few
// distinct calls repeating (period 1 through maxLoopPeriod).
//
// P53.2: it also carries the *outcome* of each recorded turn, because the two
// stuck shapes deserve different endings. A model repeating a call that keeps
// failing has exhausted its own recovery and the run is ended (this overlaps
// the P52.3 tool-failure breaker, which catches the same-error/different-args
// variant loopdetect structurally cannot see; the two are complementary, not
// duplicated). A model repeating a call that keeps *succeeding* is merely
// confused about what to do with the answer, which one corrective nudge often
// fixes — so the engine nudges once, resets the window, and only aborts if the
// cycle comes straight back.
type loopDetector struct {
	threshold int
	recent    []string
	// outcomes is index-aligned with recent: outcomes[i] is what the tool round
	// for recent[i] produced, or loopOutcomeUnknown if it has not run yet.
	outcomes []loopOutcome
	// detectedSpan is the length of the window that tripped the most recent
	// detection, so cycleHadError classifies exactly the repeating block rather
	// than the whole retained history.
	detectedSpan int
}

func newLoopDetector(threshold int) *loopDetector {
	return &loopDetector{threshold: threshold}
}

// record adds a turn signature and reports whether a loop is now detected.
// For each candidate period p (1..maxLoopPeriod), it looks for at least
// threshold/p repetitions of a length-p block at the end of the window —
// threshold/1 = threshold repeats of a single call (the original behavior),
// threshold/2 repeats of an alternating pair, and so on. A period needing
// fewer than 2 repeats to reach that confidence is skipped: one or one and a
// half repeats of a longer cycle isn't yet distinguishable from ordinary,
// varied work.
func (d *loopDetector) record(sig string) bool {
	d.recent = append(d.recent, sig)
	d.outcomes = append(d.outcomes, loopOutcomeUnknown)
	maxWindow := d.threshold * maxLoopPeriod
	if len(d.recent) > maxWindow {
		d.recent = d.recent[len(d.recent)-maxWindow:]
		d.outcomes = d.outcomes[len(d.outcomes)-maxWindow:]
	}

	d.detectedSpan = 0
	for period := 1; period <= maxLoopPeriod; period++ {
		repeats := d.threshold / period
		if repeats < 2 {
			continue
		}
		span := period * repeats
		if len(d.recent) < span {
			continue
		}
		if isRepeatingCycle(d.recent[len(d.recent)-span:], period) {
			d.detectedSpan = span
			return true
		}
	}
	return false
}

// noteOutcome attaches the result of a completed tool round to the turn most
// recently passed to record. It is a no-op when nothing is recorded — either
// the turn was skipped (every call poll-exempt) or the window was just reset
// after a nudge — so an outcome is never misattributed to an earlier turn.
func (d *loopDetector) noteOutcome(hadError bool) {
	if len(d.outcomes) == 0 {
		return
	}
	o := loopOutcomeOK
	if hadError {
		o = loopOutcomeError
	}
	d.outcomes[len(d.outcomes)-1] = o
}

// cycleHadError classifies the most recently detected cycle: true if any turn
// in the detected window that has a known outcome errored. The triggering turn
// itself is always unknown (its tools have not run yet) and turns still marked
// unknown simply do not vote; a window in which nothing is known to have failed
// is treated as a succeeding loop, which is the recoverable case.
func (d *loopDetector) cycleHadError() bool {
	span := d.detectedSpan
	if span <= 0 || span > len(d.outcomes) {
		span = len(d.outcomes)
	}
	for _, o := range d.outcomes[len(d.outcomes)-span:] {
		if o == loopOutcomeError {
			return true
		}
	}
	return false
}

// reset clears the detection window after a recoverable trigger has been
// nudged, so the model is judged on what it does *next* rather than re-tripping
// on the same history the instant it repeats itself once more.
func (d *loopDetector) reset() {
	d.recent = nil
	d.outcomes = nil
	d.detectedSpan = 0
}

// loopGuard is the second concern lifted out of Engine.Run under P63.9: the
// loop gate and everything it needs to decide what a detected cycle means.
//
// The question P63.9 asks of each concern is *what state does it actually own*,
// and answering it here found that Run was holding two variables at the wrong
// scope. `loopNudgePending` and `loopRecorded` were both declared in Run — the
// first alongside the run's other run-scoped flags — but each is set by the gate
// and consumed after that same turn's tool round, with no path in between that
// continues the loop. They are per-turn state living in a run-scoped scope,
// which is precisely how a 10-deep function accumulates interactions it does not
// have: anything else in Run could read or write them, and nothing said it
// shouldn't.
//
// So the split is: the guard owns what genuinely survives a turn (the detector's
// window and outcomes, and the threshold the messages quote), and the gate
// returns a loopVerdict the caller holds for the rest of *one* iteration. The
// per-turn state cannot outlive the turn because it is a value, not a variable.
//
// What deliberately does **not** move here is the nudge count. `nudgeState`
// owns "did this run inject a corrective of family X, and has it been
// retracted" for every family, and retractAll reads it; the loop count is one
// row of that table. This mirrors the split already in the tree for the sibling
// concern — toolFailureTracker owns the failure counters while nudgeState owns
// toolFailureNudges/toolFailureOutstanding — so the count is passed in rather
// than duplicated.
//
// A nil *loopGuard is the disabled detector (LoopThreshold < 0), and every
// method tolerates it, so Run carries no nil checks of its own.
type loopGuard struct {
	threshold  int
	detector   *loopDetector
	pollExempt func(provider.ToolUseBlock) bool
}

// newLoopGuard builds the guard for a run, or returns nil when loop detection is
// disabled.
func (e *Engine) newLoopGuard() *loopGuard {
	if e.loopThreshold <= 0 {
		return nil
	}
	return &loopGuard{
		threshold:  e.loopThreshold,
		detector:   newLoopDetector(e.loopThreshold),
		pollExempt: e.pollExempt,
	}
}

// loopVerdict is one turn's worth of loop-gate decision — the per-turn half of
// the concern, returned rather than stored so it cannot leak into the next
// iteration.
//
// The three outcomes are mutually exclusive by construction: an abort, a
// recoverable trigger (notice now, nudge after the round), or neither. recorded
// says whether this turn went into the detector's window, and therefore whether
// its tool results are the outcome that classifies a future cycle.
type loopVerdict struct {
	// abort ends the run: a cycle that errored, or a second cycle after the
	// corrective already failed to break the first.
	abort error
	// notice is emitted immediately, before the tool round.
	notice string
	// nudge is appended after the tool round rather than at the gate, so the
	// assistant's tool_use blocks are never separated from their tool_result
	// blocks.
	nudge string
	// recorded is true only for a turn added to the window that did *not* trip
	// the detector. A triggering turn is deliberately excluded: the window was
	// just reset, so attaching its outcome would misattribute it.
	recorded bool
}

// check runs the loop gate for one turn, before its tools execute.
//
// P53.2(a): calls a tool declares poll-exempt are dropped from the signature,
// and a turn made up entirely of polls is not recorded at all — an agent waiting
// on a background job or a teammate's reply is indistinguishable from a stuck
// one otherwise, and canonicalizeToolInput erases the very fields that would
// tell them apart.
//
// P53.2(b): the outcome decides the ending. A cycle whose rounds errored is
// fatal. A cycle that keeps *succeeding* earns one corrective nudge and a window
// reset; only a second trigger in the same run ends it. nudgesSpent is the run's
// loop-nudge count, held by nudgeState — passing it keeps one owner for the
// "have we already corrected this" fact rather than two that can disagree.
func (g *loopGuard) check(toolUses []provider.ToolUseBlock, nudgesSpent int) loopVerdict {
	if g == nil {
		return loopVerdict{}
	}
	sig, shouldRecord := turnSignatureExcludingPolls(toolUses, g.pollExempt)
	if !shouldRecord {
		return loopVerdict{}
	}
	if !g.detector.record(sig) {
		return loopVerdict{recorded: true}
	}
	if g.detector.cycleHadError() || nudgesSpent > 0 {
		err := fmt.Errorf("%w: identical tool calls repeated %d turns", ErrLoopDetected, g.threshold)
		if nudgesSpent > 0 {
			err = fmt.Errorf("%w — the corrective prompt did not break the cycle", err)
		}
		return loopVerdict{abort: err}
	}
	g.detector.reset()
	return loopVerdict{
		notice: fmt.Sprintf("model repeated the same succeeding tool calls %d turns running — asking it to change approach or answer", g.threshold),
		nudge:  loopNudgeText(g.threshold),
	}
}

// noteOutcome attaches a completed round's result to the turn v recorded, which
// is only knowable after the tools have run. A no-op for a turn that was not
// recorded (all calls poll-exempt, or the window was just reset), so an outcome
// is never misattributed to an earlier turn.
func (g *loopGuard) noteOutcome(v loopVerdict, results []provider.Block) {
	if g == nil || !v.recorded {
		return
	}
	g.detector.noteOutcome(resultsHadError(results))
}

// isRepeatingCycle reports whether window consists entirely of a repeating
// block of length period, i.e. window[i] == window[i+period] for every valid
// i. period == 1 reduces to "every element is identical".
func isRepeatingCycle(window []string, period int) bool {
	for i := 0; i+period < len(window); i++ {
		if window[i] != window[i+period] {
			return false
		}
	}
	return true
}

// turnSignature builds a stable signature for a turn's tool calls (names +
// canonicalized inputs, in request order). Two turns with the same signature
// requested the exact same work — the hallmark of a loop.
func turnSignature(toolUses []provider.ToolUseBlock) string {
	sig, _ := turnSignatureExcludingPolls(toolUses, nil)
	return sig
}

// turnSignatureExcludingPolls builds the loop signature for a turn, dropping
// every call that pollExempt reports as a legitimate poll (P53.2), and reports
// whether the turn should be recorded at all.
//
// record is false when nothing survives the filter — a turn made up entirely of
// polls. Recording it as an empty signature would be actively harmful: empty
// equals empty, so a handful of poll-only turns would form a perfect period-1
// cycle and trip the very detector the exemption exists to keep quiet.
// pollExempt may be nil, which reproduces the pre-P53.2 signature exactly.
func turnSignatureExcludingPolls(toolUses []provider.ToolUseBlock, pollExempt func(provider.ToolUseBlock) bool) (string, bool) {
	var b strings.Builder
	kept := 0
	for _, tu := range toolUses {
		if pollExempt != nil && pollExempt(tu) {
			continue
		}
		kept++
		b.WriteString(tu.Name)
		b.WriteByte('\x00')
		b.Write(canonicalizeToolInput(tu.Input))
		b.WriteByte('\n')
	}
	return b.String(), kept > 0
}

// loopNudgePrefix opens the P53.2 recoverable-loop prompt and, like
// zeroToolNudgePrefix / emptyAnswerNudgePrefix / toolFailureNudgePrefix,
// doubles as the marker its retraction keys on — so the text after it can name
// the actual turn count without breaking retractNudges.
const loopNudgePrefix = "[You have issued the same tool call(s)"

// loopNudgeText is the one-shot corrective for a *succeeding* loop. It is
// deliberately not routed through the Approver seam: under the non-interactive
// approver an unattended drive uses, an approval-style check warns and allows,
// which would make the detector toothless in precisely the scenario it exists
// for. Nudge-once-then-abort behaves identically attended or unattended.
func loopNudgeText(turns int) string {
	return fmt.Sprintf(loopNudgePrefix+
		" %d turns running, and they keep succeeding with the same result — you are repeating"+
		" work you have already done instead of making progress. Do not issue those same calls"+
		" again. Either take a different next step that builds on the result you already have,"+
		" or, if you have enough to answer, stop calling tools and give your final answer now.]",
		turns)
}

// looksVolatile reports whether a JSON scalar's string form looks like a
// per-call nonce rather than meaningful content the model chose: an RFC3339-ish
// timestamp, a UUID, or a long run of digits/hex (epoch millis/nanos, random
// nonce). Exact-string matching on raw tool input otherwise means a single
// varying byte from a field like this defeats loop detection entirely, even
// though the call is otherwise identical turn after turn.
var (
	timestampRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}`)
	uuidRe      = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)
	longDigitRe = regexp.MustCompile(`^\d{9,}$`)
	longHexRe   = regexp.MustCompile(`^[0-9a-fA-F]{16,}$`)
)

func looksVolatile(s string) bool {
	return timestampRe.MatchString(s) || uuidRe.MatchString(s) || longDigitRe.MatchString(s) || longHexRe.MatchString(s)
}

// canonicalizeToolInput parses a tool call's raw JSON input and rewrites any
// scalar leaf that looksVolatile to a fixed placeholder, so loop detection
// compares on the call's meaningful content rather than being defeated by an
// incidental nonce/timestamp field. Object keys are also normalized to a
// stable order via json.Marshal's map handling. Input that isn't valid JSON
// (or isn't an object/array) is returned unchanged.
func canonicalizeToolInput(input json.RawMessage) []byte {
	var v any
	if err := json.Unmarshal(input, &v); err != nil {
		return input
	}
	out, err := json.Marshal(canonicalizeValue(v))
	if err != nil {
		return input
	}
	return out
}

const volatilePlaceholder = "‹volatile›"

func canonicalizeValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = canonicalizeValue(vv)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, vv := range t {
			out[i] = canonicalizeValue(vv)
		}
		return out
	case string:
		if looksVolatile(t) {
			return volatilePlaceholder
		}
		return t
	case float64:
		if looksVolatile(strconv.FormatFloat(t, 'f', -1, 64)) {
			return volatilePlaceholder
		}
		return t
	default:
		return v
	}
}
