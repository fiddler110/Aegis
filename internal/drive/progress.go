package drive

// MaxNoProgressTurns bounds the P39.7 nudge loop: after this many consecutive
// drive turns that mutate no file and leave the PENDING set unchanged, stop
// rather than keep paying for a model that will only narrate. Two nudges then a
// stop mirrors the previous "three consecutive yields" bound.
//
// Shared with the generic single-context drive in internal/cli, which applies
// the same guard — the bound is a property of how a local model stalls, not of
// which drive is running it.
const MaxNoProgressTurns = 3

// ActNowNudge is the P39.7 stall-breaker prefix, prepended to the continuation
// turn when the previous turn mutated no file while PENDING markers remain. It
// is deliberately forceful and concrete — the corroborated lever (P38.1) was an
// explicit "one section per turn, act now via edit_file" preamble, which
// unstuck a gpt-oss:20b fill that had yielded three times with markers present.
func ActNowNudge() string {
	return "STOP NARRATING — ACT NOW. The previous turn changed no file. Do not explain what you will do; do it this turn. Call `edit_file` now to fill the next single `<!-- PENDING: <section> -->` marker with real content — one section, one edit. No preamble, no plan, no questions before the tool call.\n\n"
}

// StuckLoopDirective is the P57.1 escalation, prepended to the next turn's
// prompt after the engine's loop guard aborted the previous one and the drive
// reset to a fresh context (see recoverReasoningLoop).
//
// The reset alone already discards the wrong theory the model was looping on;
// this keeps it from rebuilding the same one. The 2026-08-03 stall was not a
// model that could not do the work — a fresh invocation fixed every defect
// immediately — it was a model that had decided a `T0`-vs-`T01` zero-padding
// offset existed, and kept re-reading the same ~30 lines to confirm it. So the
// directive is specifically anti-re-derivation: the verifier's report already
// below in the prompt is the finding, each evidence line is ground truth, and
// the job is to edit the named line, not to work out what is wrong. It is the
// same shift from "figure out what's wrong" to "here is what's wrong" that
// scaffold.py (P38.4) made for structure.
//
// It deliberately does not repeat ActNowNudge's anti-narration text: the stuck
// model was calling tools every turn, not narrating, so the failure it addresses
// is the opposite one.
//
// withReport selects the ground-truth clause. The phase-6 and re-entry prompts
// carry the verifier's `file:line` report, which is the strongest possible
// "here is what's wrong"; a content phase carries a PENDING file list instead,
// and telling it to trust a report that isn't in its prompt would be the same
// invitation to invent one that the directive exists to remove.
func StuckLoopDirective(withReport bool) string {
	ground := "- The list of remaining files below is ground truth — it was scanned off disk, not inferred. Do not audit it, re-count it, or work out whether it is right.\n"
	if withReport {
		ground = "- The verification report below is the FINDING, not a hint. Every `file:line` in it is ground truth, already checked mechanically. Do not re-read files to confirm it, and do not re-check whether it is really a problem.\n"
	}
	return "STOP RE-DERIVING — YOU ARE REPEATING YOURSELF. The previous attempt was aborted: it made the same tool calls over and over without resolving anything. That context has been discarded, and this is a fresh one. Read this before doing anything else:\n\n" +
		ground +
		"- Do not build a theory about identifier numbering, padding, offsets, or ordering. The checks compare exact strings; if two identifiers are reported as differing, they differ literally, and there is no scheme to work out.\n" +
		"- Do not re-read a file you have already read this turn. If you find yourself opening the same file twice, stop reading and make the edit.\n" +
		"- Act on the FIRST outstanding item: open the named file, make one targeted `edit_file`, then move to the next. If one item defeats you, leave it and fix the others — a partly fixed suite is worth more than another loop.\n\n"
}

// OverflowEscalationDirective is the nudge prepended to a content phase's
// conversation after a context overflow forced a reset. Without it a reset is
// purely mechanical — discard the context, re-read the PENDING files from disk —
// which fixes an overflow caused by *accumulated context* but not one caused by
// the model's own *plan* being too large for a single generation. The plan is
// re-derived from the same inputs after the reset, so it comes out identical and
// fails identically.
//
// That is not hypothetical. On a 2026-08-07 live run against an external repo the analysis phase
// enumerated 40 threats, the findings phase decided to author one finding per
// threat (FIND-01…FIND-40) and announced that plan, and its write truncated
// mid-tool-call into malformed JSON (`unexpected end of JSON input`). Each reset
// replayed it: the prose preceding the 2nd and 3rd truncations was byte-identical.
// Five truncations, zero findings written, no possibility of convergence — the
// drive burned 50 minutes before being killed. The earlier run of the same repo
// survived the same overflow only because it had consolidated 31 threats into 19
// findings, which happened to fit.
//
// So the reset has to change the *strategy*, not just the context: name the real
// failure (one generation too long, not too many files), forbid re-announcing a
// whole-file plan, and shrink the unit of work to a single smallest item. It
// escalates with the reset count because a model that has already ignored the
// gentler form needs the harder bound, and by the last reset the only instruction
// left that can still make progress is "one edit, then stop".
//
// It deliberately does not repeat StuckLoopDirective's anti-re-derivation text:
// the overflowing model is not stuck in a read loop, it is trying to do too much
// at once, which is a different failure with a different remedy.
func OverflowEscalationDirective(reset, maxResets int) string {
	b := "YOUR PREVIOUS ATTEMPT WAS TOO LARGE FOR ONE TURN. It was cut off mid-tool-call, so nothing it tried to write was saved. That context has been discarded; this is a fresh one. The problem was the SIZE OF A SINGLE RESPONSE, not the number of files left — retrying the same approach will fail the same way.\n\n" +
		"- Do NOT restate a plan for the whole file, do NOT enumerate every item you intend to create, and do NOT announce how many there will be. Planning aloud is what ran the response out of room last time.\n" +
		"- Write the SINGLE next outstanding item — one section, one finding, or one table row — in one `edit_file` call, using the smallest edit that completes it.\n"
	if reset >= 2 {
		b += "- Make exactly ONE `edit_file` call this turn and then end the turn. Do not batch two items, and do not continue to the next one — you will be called again immediately.\n"
	}
	if reset >= maxResets {
		b += "- This is the LAST reset available for this phase. If this turn is also too large the phase stops unfinished, so keep this edit as small as it can possibly be.\n"
	}
	return b + "\n"
}

// SameStrings reports whether two already-sorted slices hold the same elements
// in the same order. Pending-marker scans return sorted results, so this is a
// cheap way for the P39.7 guard to tell whether a turn changed the PENDING set.
func SameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// NextWindow returns the next serving-window (num_ctx) size when a phased
// drive escalates from cur toward the model max on a context overflow (P47.5b):
// a doubling step, clamped to max, with a jump straight to max once doubling
// would overshoot. It reports grew=false — cur unchanged — once cur is already
// at or above the ceiling (or max is unknown), which is what bounds the
// escalation to a finite number of steps ending at max. A doubling step (rather
// than a single jump to max) is gentler on GPU memory: it only claims as much
// KV-cache headroom as each successive overflow proves is needed.
//
// Exported alongside the drive rather than left with its CLI caller because
// every host of the drive has to build the same EscalateWindow closure.
func NextWindow(cur, max int) (next int, grew bool) {
	if max <= 0 || cur >= max {
		return cur, false
	}
	next = cur * 2
	if next > max || next <= 0 {
		next = max
	}
	return next, true
}
