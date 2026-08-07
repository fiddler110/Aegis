# Aegis Capability Roadmap

**Last updated:** 2026-08-07 (thirteenth pass — **the P63.x batch built**: P63.1-P63.7 shipped in one
session, 3 new items filed off the work; twelfth pass: P63.x full-stack review, 7 items filed across
Tiers 1-3; eleventh pass: Tier-4 assessment — P61.7's in-repo half shipped and its remainder
re-scoped, P49.3 dropped on its own measurement, P60.3/P52.14/P25.9 re-verified and left parked)

This document tracks only **open** work and what's next. For shipped-feature history, batch origins
and full design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>`
heading with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape,
so keep it when adding items.

---

## Status

**9 open items.** The P63.x review filed 7 on 2026-08-07 and **all 7 were built the same day**
(P63.1-P63.7; the `Engine.Run` half of P63.7 was split out rather than built — see below). Three new
items were filed off that work: **P63.8** from the sweep P63.3 asked for, **P63.9** from P63.7's
deliberate scope cut, **P63.10** from reading every `Update` case in sequence.

**Tier 1 is empty again.** Tier 2 holds three — **P62.1**, **P38.1** and **P63.8**. Tier 3 holds one,
**P63.9** (`Engine.Run`). Tier 4 is five: **P61.7** (remainder), **P60.3**, **P52.14**, **P25.9**, and
**P63.10**.

**What building the batch taught, beyond the fixes.** Three of the seven turned out to be
larger or differently-shaped than filed, and in each case the item's own write-up is what
made that visible:

- **P63.4 was filed as "two lines, no design question."** It was neither. Measuring
  `modernc.org/sqlite` showed a `PRAGMA busy_timeout` **Exec does not survive connection churn** —
  it is per-connection state, not persisted in the file the way `journal_mode=WAL` is — so the fix is
  a DSN parameter, `knowledge` had the same omission the item did not name, and `cli/worker.go`, the
  one site the item praised for getting it right, was using the unreliable mechanism.
- **P63.5 contained a trap the write-up half-anticipated.** `recordInvalidAuthAttempt` also armed the
  lockout, so following the item's literal instruction — "call it on each failure branch" — would
  have produced exactly the self-DoS the same item warned against. It needed a split, not a call.
- **P63.7 was two items wearing one heading.** The `Update` half was mechanical and shipped; the
  `Engine.Run` half needs a design answer about per-turn versus run-scoped state, which is why it is
  now **P63.9** rather than an unbuilt remainder.

That pattern — the filed item is right about the defect and wrong about the size — matches what the
Tier-4 assessment pass found one day earlier, and is the same argument for measuring before building.

**What the review pass found.** Baseline health is strong and should be recorded as such:
`go build`, `go vet`, `go test ./...` and `go test -race` over the six concurrent packages are all
clean; engine coverage is 92.0%; the documented counts (22 personas, 12 built-in skills) match the
tree exactly; and `webui/dist` is committed in the same commit as its frontend source. The findings
are about **erosion**, not breakage — and the two sharpest ones are erosion against *already-shipped
work*, which is why checking releases.md before filing changed both write-ups:

- **P63.3** is not a fresh inconsistency. `ScopeGate` (P46.1) reintroduced the static-`Capability()`
  pattern that **P32.2, a shipped Tier-1 item, was filed to remove** — in the gate P46.1 itself calls
  the outermost. Reading the refutation records turned "minor inconsistency" into "regression against
  a Tier-1 fix, one release later."
- **P63.7** is not a new complaint about function length. **P40.5 named this exact split** and
  deferred it as "opportunistic follow-up"; the target has since grown 1,249 → 1,324 lines. P40.6
  established the safe method (golden-transcript-gated code motion) on the sibling function.

Both are the tier system working as intended — the value came from the history, not the reading.
The one place the review found the *absence* of a record was P63.4: `busy_timeout` is set in
`cli/worker.go` and in neither long-lived store, with no comment anywhere arguing for the omission.

The 2026-08-06 assessment pass measured all five Tier-4 items rather than re-reading them, and the
measurements moved three of them plus filed a new Tier-2 item. **P61.7's premise was false** — the
backend echoing model text into a classified error message was *Aegis itself*, in the OpenAI adapter
— so its in-repo half shipped the same day, along with **P61.7(b)**, a classifier disagreement the
same measurement exposed that needed no injection to trigger. **P49.3 was dropped**, refuted by its
own measure-first gate. Measuring *why* it was refuted then produced **P62.1**: the repo map's
selection policy turns out to be the alphabet, delivering 10 of 672 files with every
architecture-table package invisible — a live defect nobody had filed, found only by asking what the
dropped item's gate was really blocked on. The other three re-verified as accurately filed and
correctly parked.

All of it repeats the lesson under *How to use this tier* below: every write-up that got measured was
wrong in some way, and twice the measurement was more valuable than the item that prompted it.

**No batch is open beyond its parked or follow-on members.** P63.x (full-stack review, 7 filed
2026-08-07, all 7 built the same day) → **3 open**, and all three were *filed by the build*, not by
the review: P63.8, P63.9, P63.10. P61.x
(cross-adapter drift, 8 filed) → P61.7 only. P60.x (sandbox and eval, 4) → P60.3 only. P59.x (local
execution, 10 + the P59.11 follow-on) → 0. P55.x (container-only scanning, 9 filed / 8 built) → 0.
P52.x (the previous full-stack review, 17) → P52.14 only. P53.x, P57.1, P58.x, P54.2 → 0. Dates,
per-item rationale and every write-up are in [releases.md](releases.md).

**Where the history went.** Batch origins (what each review actually read, and what it judged already
correct), the **refutation records** — candidate findings checked and deliberately *not* filed — and
every shipped, closed or dropped write-up live in releases.md under *Migrated from roadmap.md*
(the 2026-08-01 and 2026-08-06 cleanups). **Read the refutation records before filing anything**
against `internal/provider`, `internal/ollamainfo`, `internal/repomap` or scanner method resolution:
several obvious-looking gaps there have already been checked and answered, and the point of writing
them down was to stop the next review re-filing them.

**What to do next: P63.8, then P62.1.** With Tier 1 empty, P63.8 is the cheapest item that closes
something: one call site and a test, and it retires the last instance of a pattern two shipped items
have now removed elsewhere — leaving it open means the argument P32.2 and P63.3 both made is still
only two-thirds applied. P62.1 is the larger Tier-2 win and the one with a live user-visible effect
(the repo map delivering 10 of 672 files, alphabetically), but it needs a selection policy designed
rather than a line changed, so it should not be started in the same pass.

**Do not start P63.9 casually.** It is the highest-value structural item open and the easiest to do
badly: unlike its sibling it cannot be made safe by technique alone, and a botched pass degrades the
agent loop every other item is measured through. It wants a session of its own.

**What to do next that is not a tier item, unchanged:** re-run P38.1's live conformance test. It is
still the only open work whose outcome produces new information rather than new code, and it doubles
as the validation **P57.1** is owed — a fix aimed at a failure observed exactly once.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status: none open.** P63.1 (a sub-agent panic taking down the daemon) and P63.2 (`govulncheck`
unable to analyze the module) were filed 2026-08-07 from the P63.x full-stack review and shipped the
same day. Before them the tier's most recent occupants were the Tier-1 half of the P61.x
cross-adapter drift batch (P61.1-P61.3, shipped 2026-08-05/06); before those, P59.1-P59.3 and the
P55.x Tier-1 half. See [releases.md](releases.md) for the write-ups and for the full Tier-1 history
(P52.1, P52.2, P51.1, P50.1 and the P47.x batch head).

---

## Open Work — Tier 2

**Status: 3 open — P62.1, P38.1 and P63.8**, below. P62.1 was filed 2026-08-06 off the measurement
that dropped P49.3; the two of them are ranked ahead of P63.8 deliberately, since document order is
priority order. **P63.8 was filed while landing P63.3**, from the tree-wide sweep that item's fix
asked for — it is the last remaining instance of the static-capability pattern and is appended last,
being the least reachable of the set. P63.3-P63.6, filed alongside it by the same review, all shipped
2026-08-07; before those, the last Tier-2 items were P61.4, P61.5 and P61.8 (2026-08-06).

### P62.1 — The repo map's selection policy is the alphabet (measured, not speculative)

`repomap.Build` ends with `sort.Slice(m.Files, ... Path < Path)` (`repomap.go:398`) — plain
alphabetical — and `Render` walks that order and **breaks** at the first file that doesn't fit. So
which files reach the model is decided by filename, and the `break` (not `continue`) makes the cutoff
a hard wall: a 1-symbol file later in the sort can never fit, even with spare bytes.

Measured on this repo:

| | |
|---|---|
| Files surviving into the rendered map | **10 of 672 (1.5%)**, 4 of them test files |
| Full untruncated render | 462,736 bytes = **57.8× the 8000-byte budget** |
| 672 path lines with **zero** symbols | 21,714 bytes = **2.71× the budget** |
| Files that fit at 0 / 1 / 3 / 5 / 10 symbols each | 261 / 106 / 53 / 37 / 21 |
| Test files | 340 of 672 (50.5%), 3,038 of 7,211 symbols |

What the model actually receives is `cmd/aegis/main.go`, the ACP package, `agentdef`, and a wall of
60 `internal/api` struct names — 4 of those 10 entries being test files. **Every package in
CLAUDE.md's architecture table is invisible**: `engine`, `provider`, `tool`, `server`, `config`.
Cross-checked against the one ranking signal already computed (P49.1 import edges), the top in-degree
packages are `internal/tool` (109), `internal/provider` (98), `internal/config` (96),
`internal/sandbox` (55) — not one has a surviving file except `internal/api`, which got in on the
letter "a". This is live for anyone who has run `aegis index`; the map is opt-in (nothing is injected
without a `.aegis/repomap.json` cache), which is the only reason it isn't Tier 1.

**Selection alone is not sufficient, and that is the load-bearing finding.** At 57.8× budget, a
perfect ranking still buys the top ~10-20 files of 672 — and a bare filename listing with no symbols
at all is still 2.71× budget. Ordering changes *which* 1.5% you see; it cannot change that it is
1.5%. Any real fix pairs selection with per-file compression (symbol caps, directory rollups) or a
larger budget. Relatedly, `DefaultMaxBytes = 8000` is **not configurable** — no config key exists and
every call site passes a bare `repomap.Options{}` — so an operator on a 128k-context model cannot
spend 1% of it on a better map even though the budget was calibrated as a ~2k-token slice.

Options measured, cheapest first. **(B) demote test files**: one path predicate, zero new
computation, and it addresses 50.5% of files / 42% of symbols — no theory of "important" needed
beyond production-before-test. **(A) rank by import in-degree**: data already computed in `Build`, one
pass, but only 20.7% of edges resolve in-repo and they resolve to package *directories* (65 distinct),
so it ranks packages and needs a within-package tiebreak. **(C) per-file symbol cap + `continue`
instead of `break`**: no new data, moves coverage from 10 files to 53 (cap 3) or 106 (cap 1), but a
truncated symbol list is a *different* failure — the model may conclude a symbol doesn't exist —
so it needs a per-file "+N more" marker that costs bytes exactly where they are scarce.
**(D) query-relevant selection reusing `memory.LoadRelevant`** is the right shape and the repo's
preferred move (extend, don't parallelize), but is **blocked**: the map is injected once at session
start, before any user query exists, so it needs a per-turn or two-stage map first.

The policy-independent part — a truncation notice that reports the omitted count and points at the
P49.2 `repomap` tool, plus a fix for the notice being appended outside the byte cap — **shipped
2026-08-06**. Everything above is still open.

Priority: Tier 2 — measured, currently degrading every indexed session, and (B) is genuinely cheap.
Recommend B first, then A; treat C as requiring the "+N more" marker, and D as blocked on a
per-turn map. **This supersedes P49.3** (dropped): the constraint is budget and selection, not
extraction fidelity — and these numbers strengthen that drop, since LSP would deepen content that
already cannot fit at the top level.

### P38.1 — Non-orchestrated, single-context threat-model build (primary path for local models)

The threat-modeling skill's primary build is a single-context linear build the driving model runs
itself — no sub-agents, no `agent`-tool orchestration. Context stays bounded by levers that already
exist (SKILL.md §4): `recon.py`'s ~11KB digest, P36.2 pruning of spent write/read payloads,
incremental section-at-a-time writes, and the deterministic P37 scripts. `scaffold.py` (P38.4)
pre-writes all seven files from the skeletons with real structure + a unique
`<!-- PENDING: <section> -->` marker per fillable section, so the model fills sections instead of
authoring structure.

**Mechanism: live-confirmed, repeatedly.** Across re-tests on qwen3:14b, qwen3.6:35b-a3b and
gpt-oss:20b, the drive reliably runs `recon.py` → `scaffold.py` → incremental `edit_file` fills in
one context with no orchestration mis-route — the P36.3-era failure that killed every prior run is
gone.

**Conformance: still unmet.** Every re-test has stalled short of an unattended verify-clean suite,
but each stall has moved the blocker further from the harness and closer to raw model throughput.
The dated log for 2026-07-21 → 2026-07-27 is in [releases.md](releases.md) (*P38.1 re-test log*);
every harness fix those runs root-caused — P39.5-P39.15, P47.1-P47.9, P52.12 — has shipped. Two
entries govern what happens next:

- **2026-07-24, qwen3.6:35b-a3b-fast vs FirewallRuleAnalyzer (phased drive):** the closure condition
  below was **met** — 23 threats / 22 findings across 9 components, `verify.py`/`lint_dfd.py`/
  `inventory.py --check` all passing, content grounded in real file:line evidence — but it took
  **three manual re-invocations**. Single-invocation stability was root-caused into the P47.x batch
  (shipped) and is the bar that remains.
- **2026-08-03, qwen3.6-fast-32k vs an external target (Documentation-as-Code, a Python CLI toolset
  with no network listener) — the current blocker.** Mechanism reconfirmed again: recon → scaffold →
  phased fill across all 5 content phases completed with zero orchestration mis-route, correctly
  self-recovered from a mid-findings-phase context overflow (fresh context, resumed from disk), and
  correctly classified the deployment as `local-desktop`. **Single-invocation conformance still not
  met — new failure mode found.** Phase 6's verify pass correctly caught genuine cross-file defects
  (five threat IDs each reused for two different threats, nine threats missing from the coverage
  table, incomplete `Related Threats` cross-references) and correctly told them apart from mechanical
  ID-format issues — confirmed independently by running `normalize_ids.py --check`, which reported the
  suite already canonical throughout, so the P47.9 reopen (findings phase) was the right call. But the
  reopened phase then got stuck: the model repeatedly mis-derived a T0-vs-T01 zero-padding offset that
  didn't actually exist, re-read the same ~30 analysis-file lines five turns running, and the loop
  detector's one corrective nudge did not break the cycle — `engine: aborting suspected loop:
  identical tool calls repeated 5 turns` ended the run with the suite still verify-failing. A second
  manual `aegis chat` invocation against the same target and model, with a fresh context, resolved
  every defect and reached a fully verify-clean suite (`verify.py` 19/19, `inventory.py --check`
  10/10, `lint_dfd.py` 6/6). That confirms the mechanism and the check scripts are sound — the
  residual gap is the reopened phase's resilience to a model stuck on its own incorrect theory of the
  data, not the overall design. Filed as **P57.1** and **shipped the same day**, so the next re-test
  is also that fix's validation: a loop abort should now reset to a fresh context with the verifier's
  report handed over as ground truth, rather than ending the drive — which is exactly what the
  successful second manual invocation did by hand.

**Direction (user, 2026-07-24):** the strongest lever is making local models **piecemeal both their
reads and their writes**, then finishing with a **quality-validation pass**; P39.12-P39.15 implement
exactly that.

**Reproduce:** `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads
outside the workspace root); run
`aegis chat "threat model this repo" --skill threat-modeling --mode build --yes` (the prompt is
required — `aegis chat` errors with "no prompt provided" without one). It prints a `phased mode`
notice and resets context each phase.

**Closure condition:** the real suite's PENDING markers reach zero and `verify.py` / `lint_dfd.py` /
`inventory.py --check` all pass, **unattended, in one invocation**. Met once, 2026-07-24 on
FirewallRuleAnalyzer; not repeated since.

Priority: Tier 2 — every load-bearing harness fix the re-tests have root-caused has shipped, and
P47.x/P52.12 addressed the two structural gaps (single-invocation stability, CLI-only reach) found
before 2026-08-03. This item stays open only as the conformance **umbrella**, closeable once a live
built-in drive — reachable from any client since P52.12 — is confirmed to reach a verify-clean suite
unattended, in one invocation, on a local model. Not Tier 1 because it is live-run verification
tracking, not independent build work.

### P63.8 — `recordWrittenPaths` is gated on the static capability, two lines above a branch that isn't

Found while landing P63.3, from the sweep that item asked for. In `engine.runTools`
(`engine/engine.go:2221`):

```go
if !isErr && t.Capability() == tool.CapWrite {
    ...
    e.recordWrittenPaths(paths)
}
if !isErr && e.redactSecrets && tool.EffectiveCapability(t, tu.Input) == tool.CapRead {
```

Two adjacent branches over the same tool and the same input disagree about which capability to ask
for. The second is correct by P25.4c; the first is the pattern P32.2 removed from `ContextualGate`
and P63.3 removed from `ScopeGate`.

The consequence is not a gate bypass — scope and the permission stack still bind — it is a **coverage
hole in the write bookkeeping**. A tool that reclassifies into `CapWrite` for a specific call via
`CapabilityFor` has its written paths go unrecorded, so that call gets no output-guard file
validation and no quarantine-on-fail rollback. That is precisely the silent degradation the P32.6
warning three lines above exists to make loud, arrived at by a different route.

**Not reachable today, same reason as P63.3:** `shell` is still the only `tool.CapabilityOverrider`
in the tree and it only narrows (`CapExecute` → `CapRead`). Recording that here so this is not
re-filed as urgent.

**Why it was split out of P63.3 rather than folded in.** P63.3 is a gate fix with a containment
argument; this is post-execution bookkeeping, and the change is not purely additive: today the branch
is skipped for `shell` (static `CapExecute`), and the effective capability makes it a *per-call*
decision for that tool too. That is a behavior change on the write-recording path and wants its own
test, not a ride-along under another item's justification.

**Fix:** `tool.EffectiveCapability(t, tu.Input)` at 2221, plus a test asserting that a tool widening
into `CapWrite` has its paths recorded for output-guard coverage — and an explicit check of what the
branch now does for `shell` calls that narrow to `CapRead`.

Priority: Tier 2 — one call site and a test, closing the last instance of a pattern two shipped items
have now removed elsewhere. Not Tier 1: no tool in the tree can reach it, and it degrades a
verification path rather than opening a gate.

---

## Open Work — Tier 3

**Status: 1 open — P63.9**, below, split out of P63.7 on 2026-08-07 when that item's `Update` half
shipped and its `Engine.Run` half proved to be different work. P61.6 (2026-08-06) was the last before
it, and the sequencing finding it produced is
recorded with its write-up: built **second** in its batch rather than last, it turned P61.1 into
option wiring and closed P61.3 with no production code, so the "write each fix twice and delete one
copy" cost the item worried about was never paid. Before it: P59.9, P60.2, P60.4 and P57.1. See
[releases.md](releases.md).

### P63.9 — `Engine.Run`: the half of P63.7 that needs a design answer, not code motion

Split out of P63.7 when its `Update` half was built, because the two halves are not the same kind of
work and bundling them hid that. P63.7 is now closed by the `Update` split alone; this carries the
part it deliberately deferred.

`engine/engine.go:586` — `Engine.Run`, **725 lines, 119 branch points, 10 levels of nesting**, holding
budget enforcement, compaction triggers, nudge retraction, loop detection and guard retries in one
scope where each interacts with the others. The **33 `// Pxx` markers inside it** are the tell: it has
become the place behavior is added *because* it is already where all the state is, which is
self-reinforcing.

**Why this is not a second helping of P63.7's method.** The `Update` split was pure code motion —
per-message-domain files, gated on the eval golden transcripts showing no diff — because each `switch`
case was already an independent unit and moving one could not change what another did. `Run` has no
such seams. Extracting anything from a 10-deep scope means first deciding **what is genuinely
per-turn state and what is run-scoped**, and that is a design question, not a mechanical one. P40.6
answered it for exactly one narrow slice — folding three parallel nudge counters into a `nudgeState`
helper — and left the rest open. That pass is the model for shape and for safety technique, but it is
not a template that can be applied five more times without re-deciding the boundary each time.

**Approach: one concern at a time, not one sweep.** Each pass takes a single interacting concern
(budget enforcement, compaction, loop detection, guard retries), names the state it actually owns,
lifts it into a helper with that state as a field, and lands separately — gated on the golden
transcripts the same way. A sweep would produce a diff no reviewer can check against a function whose
whole problem is that its parts interact.

The **live tiers matter more here than the unit suite.** `internal/engine` is at 92.0% and green
throughout the regressions P25.7 was built to catch, which is precisely the coverage profile that
hides an integration-shaped break — so `TestLiveWorkflow` (and, where attribution is in doubt,
`TestLiveWorkflowBaseline`) should run over any pass that touches retry, nudge or compaction control
flow, not just `go test ./...`.

Priority: Tier 3 — no urgency and no trigger, and unlike P63.7's half it cannot be made safe by
technique alone. Worth doing incrementally and worth *not* doing in a hurry: a botched pass here
degrades the agent loop itself, which is the one component every other item's behavior is measured
through.

**Three leads sit here unfiled, each with a stated promotion trigger.** None is a `### P<n>.<m>` item
yet, deliberately — filing one before its trigger fires would commit to a design question that has no
answer.

- **Whether Aegis should ever mount a container engine socket.** `dockle` is the only tool that wants
  one — it inspects an image through the local engine rather than pulling it — and socket access is
  effectively host root, a third privilege axis beyond the network/workspace split P55.7 is built on.
  It could live in the netscanner image and run socket-mounted and workspace-free, but that is a
  posture decision on its own merits, not a side effect of building a second image. dockle stays
  host-only and says so in code. **Promote when** someone actually needs container-only dockle.
- **Auto-engage the tool-calling shim off a low conformance rate.** Explicitly sequenced as a P53.6
  follow-up rather than dropped: the persisted P53.4 rate is already readable per model
  (`modelcaps.Store.ToolCalling`) and `provider.tool_call_shim` rejects `"auto"` rather than silently
  accepting it as a no-op, precisely so the word stays available for this. **Promote when** live runs
  show the rate predicting drive outcomes — engaging a prose-parsing fallback off a signal that isn't
  trustworthy is worse than requiring the operator to ask for it.
- **Grammar-constrained decoding for *tool calls*** (Ollama structured outputs, llama.cpp GBNF). P59.8
  took this lead at the one caller where it had no open design question — the schema guard's
  corrective retry — and deliberately did **not** widen it. The remaining half attacks the opposite end
  of the problem from the shim: making malformed tool-call JSON mechanically impossible rather than
  parsed-and-declined, targeting models that *do* speak the protocol but truncate or malform arguments
  (the P35.2 failure class). None of the six harnesses reviewed in P53.x does it. Needs its own
  `### P<n>.<m>` heading if pursued.

---

## Open Work — Tier 4

**Status: 5 open**, all blocked or explicitly parked; none has a build trigger yet. P63.10 joined
2026-08-07, filed off the P63.7 refactor rather than by a review.

**How to use this tier.** Four items have now been measured and closed — P59.10 and P52.16
(2026-08-05), P61.7's in-repo half and P49.3 (2026-08-06) — and every one taught the same lesson:
**the measurement contradicted part of the filed item.** P61.7 named an unmeasured external
dependency that turned out to be our own code; P49.3's gate turned out to be unmeetable by the work
it proposed. Building either from its write-up alone would have produced the wrong fix, or the wrong
non-decision. Take the measurement first, then re-read the item — do not treat a Tier-4 write-up as a
build plan. Details in [releases.md](releases.md).

### P61.7 — Retry/terminal classification over *backend-echoed* text (remainder)

`classifyStreamError` (`errors.go`) decides whether a mid-stream failure is retried or is fatal by
case-insensitive substring match against a free-form server error string. `terminalStreamSignals`
includes tokens as broad as `"does not support"`, `"unsupported"`, `"malformed"` and
`"invalid request"`; `retryableStreamSignals` includes `"crash"`, `"timed out"` and `"out of memory"`.
The concern is not that the heuristics are wrong — they are well-chosen and
terminal-wins-over-retryable is the right default. It is that **a control-flow decision is made on
text the model can influence.**

**The in-repo half shipped 2026-08-06** and this item is now only its remainder. The measurement that
closed it also refuted the original filing: the item said likelihood "depends on whether any backend
in real use echoes generated text into an error envelope," and the answer was that *Aegis* did, in
the OpenAI adapter, which spliced the model-authored tool name into a message the classifiers then
matched. A tool named `crash_report` flipped a terminal error to retryable **and** made
`IsBackendUnavailableError` report a dead backend. Fixed via `APIError.Detail` — rendered, never
classified — plus `NewMalformedToolCallError`. Write-up in [releases.md](releases.md).

**What is left is the case originally described:** a server or proxy echoing generation fragments
into its own `{"error":…}` envelope, where the text is genuinely external and classification is the
whole point of reading it. Still unmeasured, and a fix still means guessing at a structural signal
(status code, an error `type` field) most local backends do not supply.

**P61.7(b) — the classifier disagreement the same measurement exposed — also shipped 2026-08-06.**
`Retryable()` and `IsBackendUnavailableError` read one string through two tables in two orders and
could return terminal-and-final AND backend-is-dead at once. Replaced with a single ordered ladder
(context-overflow → backend-dead → terminal → retryable → unrecognized), which reverses
"terminal always wins" for the backend-dead subset and documents why. Needed no injection to
reproduce — an existing test string already triggered it. Write-up in [releases.md](releases.md).

**Promote when:** a misclassification is actually observed, or a backend is found that demonstrably
echoes generation content into `{"error":...}`. The regression test the old entry proposed as a first
step now exists (`TestModelAuthoredTextDoesNotSteerClassification`), asserting invariance across tool
names; extending it to envelope text is the natural probe.
Priority: Tier 4 — narrowed to the external case; real surface, unquantified likelihood, no incident.
The classifier-disagreement sub-defect is separable and closer to Tier 2.

### P60.3 — Checkpoints capture files only, so `/rewind` is silent about everything else

`internal/checkpoint` snapshots each file a write tool touched, lazily, once, capped at 16MiB
(`checkpoint.go:29`), and rewinding writes those contents back. Within its stated scope that is
correct and the scope is documented. What it means in practice is that rewinding a turn that ran
`pip install`, applied a DB migration, started a background process, or wrote a >16MiB artifact
restores the *source* to its pre-turn state and leaves the environment in its post-turn state — the
one combination that was never actually true, and the user is told the turn was undone.

Orchard's roadmap item is stateful sandboxes: pause, resume and **branch** the whole sandbox, so a
checkpoint is the environment rather than a diff over it. Applied here: if a session owns a
persistent container (P60.2), a checkpoint can be a container snapshot/commit, and rewind becomes
honest about installed packages and process state without a size cap.

Two reasons this was Tier 4 and one still holds. It was strictly downstream of P60.2 — there was no
container to snapshot while every command was `--rm` — and that dependency **cleared on 2026-08-05**
when P60.2 shipped. What remains is that it only helps sessions using the container backend, which is
not the default. And Orchard's version is *roadmap, not shipped code*, so there is
no implementation to read; only the idea transfers.

**Re-verified 2026-08-06:** `sandbox.backend` still defaults to `"local"` (`config.go:1348`), so the
one remaining condition is unchanged and the entry above needs no correction. P60.2 clearing the
dependency did not move it.

**Promote when:** the container backend is a realistic default for real sessions, or a user reports a
rewind that restored files into an environment that no longer matched them.
Priority: Tier 4 — no longer blocked, but speculative until someone is actually rewinding inside a
container.

### P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)

`newLoopDetector` is constructed **inside** `Run` (`engine.go:422-424`), so its window resets on
every call. In the TUI and web UI, each user turn is a separate `Run` — so a model that loops
*across* user turns (re-reading the same file every time the user nudges it, re-running the same
failing command after each correction) is never detected, no matter how many turns it repeats.

Fix would be to hoist the detector to session scope, plumbed through `engine.Options` as an optional
caller-owned detector so the daemon can hold one per session while the CLI keeps today's per-`Run`
behavior. The complication worth thinking through before building: a user *legitimately* asking for
the same tool call twice in two turns is not a loop, so a session-scoped detector likely needs a
higher threshold than the per-`Run` one, or needs to reset on any user message that isn't a bare
retry — which is a fuzzier judgment than the current mechanism makes.

**Precondition met 2026-08-01:** P53.2 deliberately landed first, since widening the scope of a
detector that mis-fired on polling and always aborted fatally would have multiplied both defects. A
session-scoped detector would now inherit a sounder mechanism. **Reviewed 2026-08-01, still correctly
parked** — `newLoopDetector` is unchanged, and the design question above is real work rather than a
mechanical port to a wider scope. Not worth building speculatively: without a concrete false-negative
in hand it would ship a detector tuned against a guess rather than an observed failure mode.

**Re-verified 2026-08-06:** `newLoopDetector` is still constructed inside `Run` (now
`engine.go:631`), and the design question above is still the blocker rather than the port.

**Promote when:** a live run shows a cross-turn loop that per-`Run` detection missed.
Priority: Tier 4 — real but unproven, and the false-positive risk is higher than the current
detector's.

*(P49.3, LSP-backed symbol extraction for the repo map, was **dropped 2026-08-06** — refuted by its
own measure-first gate. Measured on this repo, the map renders 7847 of an 8000-byte budget and
**truncates**, fitting 187 lines out of 673 files and 7208 top-level symbols; LSP would add *nested*
symbols and reference edges, i.e. strictly more content contending for a budget that already cannot
fit the top-level ones. Precision that never reaches the model is not precision, so the gate is
unmeetable by the work the item proposed. The limiting factor is **selection** — which files earn the
budget — which is a different item. Dropped rather than parked for the same reason as P49.4: the
write-up's premise is now known false, and a parked item invites building from it. Re-file only if a
budget/selection tier ships and extraction fidelity is then shown to be the limit; rationale in
[releases.md](releases.md).)*

*(P49.4, the LLM-summarized concept-node sibling, was dropped 2026-08-03 rather than parked — it
carried two unresolved problems at once. Re-file only if the deterministic structural tiers
demonstrably fail to close the re-discovery gap **and** the "new store vs. extend
`knowledge`/`memory`" question has an answer; rationale in [releases.md](releases.md).)*

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of the six daemon-singleton services were per-session-scoped when P25.9 first shipped;
`lsp.Manager` was deliberately left as a shared singleton — its per-session resource-growth
tradeoff was judged worse than the isolation gap. Parked pending a concrete multi-tenant need.

**Re-verified 2026-08-06:** still one shared `lsp.NewManager(cwd, logger)` at daemon construction
(`internal/server/server.go:597`). No trigger has fired.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

### P63.10 — Two small TUI message-handling asymmetries, seen while splitting `Update`

Both were found by reading every `Update` case in sequence during P63.7 and both are **pre-existing**
— P63.7 was pure code motion and deliberately preserved them, since fixing a bug inside a
no-behavior-change refactor destroys the property that made the refactor safe. Filed here so the
observation is not lost with the sub-agent that made it.

**1. The spinner tick chain dies while idle.** `updateSpinnerTick` (`tui/update_tick.go`) drops the
`tea.Cmd` returned by `m.sp.Update(msg)` when `!m.streaming`; only the streaming branch re-queues.
This looks intentional — not animating an idle spinner is the obvious reason — but the effect is that
the chain is *terminated* rather than paused, so it depends on something else re-starting it at the
next stream. Worth confirming that re-start actually exists on every path, and saying so in a comment
either way.

**2. A stale toast expiry can retire a newer toast.** `updateToastExpired` clears `m.activeToast`
unconditionally, without checking that the expiry it received identifies the toast currently shown.
Two toasts in quick succession therefore cut the second one short by the first one's timer. The fix
is an identity on the toast and a comparison before clearing.

Priority: Tier 4 — both are cosmetic, neither is reachable as a correctness or security problem, and
the toast one needs a toast-identity concept that does not exist yet. No trigger; do not build
speculatively. Fix opportunistically if either file is open for another reason.

---

For shipped feature history, batch origins, refutation records, competitive-landscape review and the
full gap analysis, see [releases.md](releases.md).
