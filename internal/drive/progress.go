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
