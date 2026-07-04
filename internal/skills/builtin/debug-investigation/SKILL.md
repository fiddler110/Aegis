---
name: debug-investigation
description: Use when asked to debug a failure, find the root cause of a bug, investigate why something is broken or flaky, or fix a crash/error you can't yet explain. Triggers on "why is this failing", "debug this", "find the root cause", "this test is flaky", "this crashes", "track down this bug".
---

# Debug Investigation Skill

The failure mode this skill exists to prevent is the "shotgun fix": changing
several plausible-looking things at once, hoping one of them helps, without
ever confirming which one actually mattered — or whether the bug is even
fixed. Work in this order instead.

## 1. Reproduce before theorizing

Get the failure to happen in front of you — run the failing test, the exact
command, or the exact input the report describes — before forming a
hypothesis. A bug you can't yet reproduce gets a narrower first step (find
the smallest reproduction), not a guess at the cause.

If it's genuinely non-reproducible (flaky test, intermittent prod issue),
say that explicitly and look for what varies between runs (timing,
ordering, external state, concurrency) rather than picking a cause because
it's the most familiar one.

## 2. Read the actual evidence before hypothesizing

The stack trace, error message, exit code, or failing assertion is ground
truth about *where* things went wrong, even when it's misleading about
*why*. Read it fully — the first frame in your own code, not just the
top of the trace — before forming a theory. Check:

- Recent `git log`/`git blame` on the failing area — did this work before a
  specific commit? `git bisect` (or a manual binary search over commits) is
  faster than reasoning about a diff you can't yet narrow down.
- Whether the failure is in the code under test or in a fixture/mock/setup
  that's silently wrong (a surprisingly common source of "flaky" tests).
- LSP `references`/`definition` on the failing symbol to see every call
  site — a bug that looks local is often triggered by one specific caller
  passing an unexpected value.

## 3. Narrow with bisection, not elimination-by-staring

When the cause isn't obvious from the evidence, cut the search space in
half repeatedly rather than reading the whole implicated area top to
bottom: bisect commits, bisect input size (does it fail on a smaller
input?), bisect which of several changed files matters, or add one
instrumentation point at the midpoint of the suspect code path and see
which half of it the failure falls in.

Minimal, temporary instrumentation (a log line, a print, an assertion) beats
reasoning in your head about what a value "probably" is at a given point —
check it. Remove the instrumentation once you're done; don't leave debug
prints in the final change.

## 4. Confirm the fix against the original reproduction

Before declaring the bug fixed, re-run the exact reproduction from step 1
and confirm it now passes/behaves correctly — not just that the code
"looks right." If you changed more than one thing to get there, revert the
extras one at a time (or re-derive a minimal diff) so you know which change
actually mattered; an unnecessary accompanying change is itself a risk you'd
otherwise be shipping unexamined.

Add or update a test that would have caught this failure, so the fix is
verified by more than a manual re-run.
