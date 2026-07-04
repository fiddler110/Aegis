# Severity Rubric

Shared by code and documentation reviews so findings from both can be
merged into one ranked list. These names match the `html-report` skill's
chip/border classes exactly (`critical`/`high`/`medium`/`low`) — using them
consistently means a review's findings drop into that template with no
remapping.

- **Critical** — will cause incorrect behavior, data loss, a security
  hole, or a crash in normal (non-adversarial) use; or, for docs, a claim
  that will actively mislead someone into doing something broken or unsafe
  (wrong command, wrong flag, security-relevant misstatement).
- **High** — will cause a real problem in a reachable but less common
  path (an edge case, a specific input shape, a concurrency window); or a
  doc gap/inaccuracy that will cost a reader significant time or produce a
  wrong mental model of how the system behaves.
- **Medium** — a real defect or gap, but narrow, low-frequency, or
  self-correcting (e.g. a stale comment, a missing test for an already-safe
  path, an inefficiency that doesn't matter at current scale).
- **Low** — worth noting but not worth blocking on: style inconsistency,
  a nice-to-have test, a doc section that could be clearer but isn't wrong.

Don't force a finding into a higher bucket to make the review look more
substantial, and don't bucket everything as Medium to avoid judgment calls
— the ranking is the point of the review.

Two things are not findings on this scale and shouldn't be reported at all:

- Style/formatting a linter or formatter already enforces.
- Hypothetical concerns with no plausible trigger ("this *could* be a
  problem if the API changed to do X" when it doesn't).
