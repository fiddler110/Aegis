package builtin

import (
	"context"
	"sort"
)

// P67.1: the round-level bound over the per-call caps in truncate.go.
//
// Every cap in that file is per *call*, and the posture table was written when a
// round was one result at a time. It no longer is: Engine.runTools dispatches up
// to maxParallelTools (8) calls concurrently, so a round of N read tools can each
// land at its own cap and produce N times the intended context bite inside a
// single user message. With the largest inline cap at 32 KiB that is 256 KiB —
// ~65,000 estimated tokens — appended in one go, and nothing anywhere bounded the
// aggregate.
//
// This is a budget layered *above* the existing caps, not a change to them. Each
// tool still decides which end of its own output carries the information and
// still spills its own remainder; this only decides how much of a round's worth
// of already-capped results the conversation can afford at once.
//
// # Three decisions worth stating
//
//   - **Each round is evaluated independently.** A large result in this round and
//     another in the next are both fine and neither is touched. The failure this
//     bounds is N results arriving *together*; a run that reads one big file per
//     turn is doing exactly what it should.
//
//   - **The head of the result is what survives**, even though the tools
//     themselves disagree about which end matters. That is not a default chosen
//     over the alternative — it is the only correct choice at this layer, because
//     each result *string* already begins with whatever end its own posture kept.
//     A shell result is tail-posture, so the string it hands over already starts
//     at the tail of the command's output; keeping the head of that string keeps
//     the part shell chose. Applying a posture here would undo theirs.
//
//   - **A round of one result is not subject to this.** A single call is already
//     bounded by its own cap, and the one case that can exceed 32 KiB alone is an
//     explicit `read_file` window, which the posture table deliberately honors
//     verbatim as the caller's own. Trimming that would override a stated
//     posture to solve a problem it is not part of.
const (
	// roundResultBudget is the combined inline size a single tool round may
	// contribute to the conversation.
	//
	// Sized from the cap table rather than picked: it has to admit the largest
	// single inline cap (read_file's 32 KiB default byte bound) plus a second
	// substantial result without spilling anything, or an ordinary two-file round
	// would pay a spill it does not need. 48 KiB does that, prices at ~12,300
	// estimated tokens at tokenest's ASCII rate, and bounds the 8-way worst case
	// at a fifth of what it was.
	//
	// The number is deliberately generous against the *median* round (two or
	// three results of a few KiB each, nowhere near it) and tight against the
	// pathological one. A budget that bit on ordinary rounds would spend a file
	// write and a locator on turns that had no problem.
	roundResultBudget = 48 << 10

	// minInlineResult is how much of a spilled result stays in the conversation.
	// A result trimmed to nothing is a locator the model has no reason to follow:
	// it needs enough to see what the call returned and decide whether the rest
	// matters. 2 KiB is what task_get's output preview settled on for the same
	// question, and 8 of them still leave a third of the budget free.
	minInlineResult = 2 << 10
)

// CapRound trims one tool round's results to roundResultBudget, spilling what it
// removes to <workspace>/.aegis/spill so the model can read it back, and returns
// the results to append — same order, same length, same strings when the round
// already fits.
//
// It selects by *size*: a round of one huge result and four small ones spills the
// one, not all five. Selection repeats while the round is over budget, so a round
// of eight equally large results converges on all eight trimmed rather than
// hammering the first.
//
// Spilling is best-effort, exactly as it is for a tool's own remainder: if the
// file cannot be written the result is still trimmed to fit the budget and the
// notice simply carries no locator. The budget is a context bound, and a
// read-only checkout must not be able to lift it.
//
// root is the fallback workspace root; the per-call workdir on ctx wins (see
// effectiveRoot), which is what keeps a daemon-wide registry writing each
// session's spills into that session's own workspace.
func CapRound(ctx context.Context, root string, results []string) []string {
	if len(results) <= 1 {
		return results
	}
	total := 0
	for _, r := range results {
		total += len(r)
	}
	if total <= roundResultBudget {
		return results
	}

	// Largest first, ties broken by index so the trimming order is deterministic
	// — a round of identical results must not spill a different one per run.
	order := make([]int, len(results))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return len(results[order[a]]) > len(results[order[b]]) })

	out := make([]string, len(results))
	copy(out, results)
	for _, i := range order {
		if total <= roundResultBudget {
			break
		}
		if len(out[i]) <= minInlineResult {
			// Every remaining result is at or below the floor: the round is as
			// small as this bound is willing to make it. Stopping here rather
			// than trimming below the floor is deliberate — see minInlineResult.
			break
		}
		keep := len(out[i]) - (total - roundResultBudget)
		if keep < minInlineResult {
			keep = minInlineResult
		}
		// SpillHead reserves the notice's own bytes out of keep, so what comes
		// back is never longer than keep — which is what lets the arithmetic
		// below be exact rather than optimistic. That property is truncate.go's
		// notice-budget rule, and it is why the locator can be added to a result
		// this function is trying to make smaller.
		trimmed := SpillHead(ctx, root, "round", out[i], keep, "")
		if len(trimmed) >= len(out[i]) {
			break // no progress available; do not loop forever
		}
		total -= len(out[i]) - len(trimmed)
		out[i] = trimmed
	}
	return out
}
