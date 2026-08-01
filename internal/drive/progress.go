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
