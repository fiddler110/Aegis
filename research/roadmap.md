# Aegis Capability Roadmap

**Last updated:** 2026-08-08 (sixteenth pass — **P63.9's second concern, loop detection, extracted
from `Engine.Run`**, and the full live tier ran green over it; fifteenth pass: **P63.11 shipped and
the live tier now produces a real prompt-profile measurement**, so Tier 2 holds nothing buildable;
fourteenth pass: P63.8 and P62.1
shipped, and P63.9's first of four concerns extracted from `Engine.Run`; thirteenth pass: **the P63.x
batch built**: P63.1-P63.7 shipped in one session, 3 new items filed off the work; twelfth pass:
P63.x full-stack review, 7 items filed across Tiers 1-3; eleventh pass: Tier-4 assessment — P61.7's
in-repo half shipped and its remainder re-scoped, P49.3 dropped on its own measurement,
P60.3/P52.14/P25.9 re-verified and left parked)

This document tracks only **open** work and what's next. For shipped-feature history, batch origins
and full design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>`
heading with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape,
so keep it when adding items.

---

## Status

**8 open items**, recounted from the headings. **P63.11 shipped 2026-08-08**, the same day it was
filed off the live-tier run, alongside P63.8 and P62.1 and P63.9's first of four concerns. Before
them, the P63.x review filed 7 on 2026-08-07 and **all 7 were built the same day** (P63.1-P63.7; the
`Engine.Run` half of P63.7 was split out rather than built). Write-ups in [releases.md](releases.md).

**Tier 1 is empty, and so is Tier 2 of build work.** Tier 2 holds two — **P62.2** and **P38.1** —
both validation owed against already-shipped behavior, closeable only by a live run. Tier 3 holds one,
**P63.9** (`Engine.Run`), now **two** concerns from done rather than four. Tier 4 is five: **P61.7**
(remainder), **P60.3**, **P52.14**, **P25.9**, and **P63.10**.

**What P63.9's loop-detection pass taught: the extraction is worth doing even where the concern is
already its own type.** `loopDetector` had lived in its own file since P53.2, so the pass looked like
it should be a move. It wasn't — the finding was that **two of the concern's variables were at the
wrong scope**, both set by the loop gate and consumed after the same turn's tool round while being
declared where the whole 685-line function could reach them. Naming the per-turn half and returning
it as a *value* (`loopVerdict`) is what removed the interaction, not moving the detector. The lesson
generalizes to the two remaining concerns: **ask where the state's lifetime actually ends, not which
file the type lives in.**

**What building P63.11 taught: a fix aimed at a test can still be defeated by the product behaving
correctly.** The item offered two fixes and recommended the cheaper one — pin a `num_ctx` large
enough that the measurement stops saturating. Pinning it changed nothing, because
`applyDetectedWindowFor` refuses to promise more window than Ollama is *currently serving*, and a
model left resident at 4096 by the previous run is exactly that case. That refusal is right (it is the
silent-truncation guard P52.1 built), so the fix had to move: unload the model first, then wait for
`/api/ps` to stop listing it, because eviction is not complete when the unload call returns. Three
layers, none of them the one the item named, and **only running it after each layer revealed the
next**. The item's other option — a byte comparison in the default suite — turned out to be already
shipped as `TestEffectiveSystem_localProfileTrimsPrompt`, which is the second time in three passes
that checking what exists before building changed the work.

**What building P62.1 taught: an item can be right about the defect and wrong about the sufficiency
of its own recommendation.** P62.1 recommended (B) then (A) — demote tests, rank by in-degree — while
its own body warned in bold that *selection alone is not sufficient*. Both are true, and following the
recommendation alone would have produced a correctly-ordered map still showing 1.5% of the repository,
with the item reading as closed. Ranking and compression answer different questions: (B)+(A) decide
*which* files, (C) decides *how many*. Built together they moved coverage from 10 of 672 files to 37
of 696 with every architecture-table package present; built apart, either would have looked like a
fix and delivered a third of one. **When an item's measurements contradict its own recommendation
list, the measurements are the finding.**

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
2026-08-07, all 7 built the same day) → **2 open**, both *filed by the build* rather than by the
review: P63.9 and P63.10 (P63.8 and P63.11, the third and fourth, shipped 2026-08-08). P61.x
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

**What to do next: P63.9's third concern — and it is the hard pair, so slow down.** Passes 1
(budgets) and 2 (loop detection) are done and the method is established; what remains is **compaction
and guard retries**, which interact with each other *and* with the retry path. Neither has the clean
ownership that made the first two safe, and compaction genuinely mutates `conv` mid-run, which is the
one thing pass 2's "return a value instead of storing a variable" technique cannot encapsulate. Take
them in separate sessions, and run the live tier over each — it is now capable of a clean green
(P63.11 plus the 2026-08-08 run below), so a red would mean something.

One thing is owed rather than optional: **P38.1's live conformance re-run** is still the only open
work whose outcome produces new information rather than new code, and it doubles as the validation
**P57.1** is owed.

**P63.9's budget pass did go through the live tier, and the run is worth reading before the next
pass** (2026-08-08, qwen3:14b on the 16GB-VRAM box — not the recommended qwen3.6:35b-a3b-deep, which
is not pulled here). `GuardNoMetaLeak` passed. `FixSeededBug` failed, and **P60.4's control group
attributed it to the model, not to us**: `claude -p` was pointed at the same task and failed it too,
in 27s against Aegis's 54s. That result also refutes the objection raised before running it — that a
cloud model in the baseline arm would trivially pass and mislabel any Aegis failure as scaffolding.
It did not pass. Both arms produced byte-identical Aegis-side behavior across two runs (the model
issues `del /F /Q`, a cmd.exe builtin the shell tool does not have, and never attempts an edit), which
is a model failure signature and not a budget-gate one.

**Three cautions for whoever runs this tier next, all learned the hard way here:**

- **`gpt-oss:20b` is not a usable instrument on this hardware.** 13.8GB of weights against 16GB VRAM
  and 16GB system RAM thrashes: all three subtests hit their context timeouts with **0 tool calls and
  0 tokens**, and `GuardNoMetaLeak` "passed" *vacuously* — there was no output to leak. A green
  subtest in a timed-out run means nothing.
- **A live-tier fixture rotted silently, and only running the tier found it.** P62.1's per-file
  symbol cap shrank `writeBigRepoMapFixture`'s rendered map from over `bigRepoMapCapBytes` (4000) to
  2154, so `LocalPromptProfileReducesFirstTurnTokens` would have compared two *identical* prompts and
  asserted nothing. Its own guard assertion caught it — but that guard only runs under
  `-tags live_workflow`, which `go test ./...` never builds. **A product change that alters rendered
  prompt content can invalidate a live-tier fixture without any part of the default suite noticing.**
  The fixture is now sized in files rather than functions-per-file, grows until it actually clears the
  threshold, and asserts it is un-truncated as well as large enough.
- **A resident model decides the window, not your config, and it silently changes what a run
  measures.** Ollama serves whatever `num_ctx` the instance was loaded with, and Aegis deliberately
  defers to that over a configured `context_window` — so a model left loaded at 4096 by an earlier run
  caps everything that follows, and any measurement taken through `prompt_eval_count` clamps with it.
  P63.11's subtest now unloads and waits for `/api/ps` to confirm eviction before measuring; **any
  other subtest whose result depends on prompt size should be read with `/api/ps` in view.**

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

**Status: 2 open — P62.2 and P38.1**, below, and **neither is build work**: both are validation owed
against already-shipped behavior, closeable only by a live run. **P63.11, P62.1 and P63.8 shipped
2026-08-08**; P63.11 last, which is what makes the other two's live evidence trustworthy — the tier no
longer reports a saturated measurement as a product regression. P63.3-P63.6 shipped 2026-08-07;
before those, P61.4, P61.5 and P61.8 (2026-08-06). Write-ups for all of them are in
[releases.md](releases.md).

*Correction, 2026-08-08:* the previous status line read "3 open — P62.1, P38.1 and P63.8" and the
header count read 9. Both omitted **P62.2**, which has carried a `Priority: Tier 2` line in this
section since it was filed on 2026-08-07. The counts below are recounted from the headings rather
than carried forward.

### P62.2 — Validate the prefix-cache pruning gate against an adversarial fixture (it shipped unmeasured)

`compaction.Options.PreservePrefixCache` (shipped 2026-08-07) makes the deterministic prune pre-pass
headroom-gated instead of unconditional on a local backend, because rewriting the middle of a
conversation discards the llama.cpp/Ollama prefix KV cache. The **motivating** measurement is solid —
on a 2026-08-07 drive against an external repo, 163 of 238 turns prefilled in under 3s, and the only two turns
whose context *shrank* were also the two slowest prefills in the run:

| turn | context | delta | prefill | implied |
|---|---|---|---|---|
| 119 | 60,471 → 57,518 | −2,953 | 186.4s | ~309 tok/s |
| 171 | 82,577 → 79,751 | −2,826 | 312.2s | ~255 tok/s |

8.3 minutes (~6% of a 142-minute run) to reclaim ~3.5% of a context with room to spare.

**What is missing is the after measurement.** Two subsequent drives against that same repo never
reproduced a prune-induced cache miss — run 2 aborted in the findings phase before reaching the
turn range where run 1's two events occurred, and run 3 resumed mid-suite and finished in 10 turns.
So the gate has unit tests (`internal/compaction/prefixcache_test.go`: off ⇒ byte-identical, on with
headroom ⇒ skipped, on near the window ⇒ prunes, `force` ⇒ always prunes) and **zero live evidence
either that it saves the 8.3 minutes or that it costs headroom**. The headroom risk is the real one:
the gate deliberately lets more content sit in context, and the same run *did* hit a genuine overflow
in its findings phase.

Waiting for the condition to recur by chance is not a plan — three runs produced three different
workloads, because the model's threat-consolidation strategy varies per invocation (31→19, 40→1:1,
40→15). **Build an adversarial fixture instead**: a synthetic `2-*-analysis.md` with 60+ threats and
a pre-scaffolded suite, driven through the findings phase, sized so the conversation reliably crosses
the prune threshold. Measure with the gate on and off, same fixture, same seed: total wall clock,
per-turn `prompt_eval_duration_ms`, the count of turns whose context shrank, and whether the on-run
overflows more. If the gate does not clearly win on wall clock without adding overflows, tighten the
threshold (currently 25% free / 40k on a large window) or revert it — it is an optimization, and an
unmeasured optimization that can cost headroom is not worth keeping.

The same fixture validates the **P62.3 overflow-escalation ladder** (`OverflowEscalationDirective` +
`maxPhaseOverflowResets`, shipped alongside), which is unvalidated for exactly the same reason: it
never fired live. Note the trap that wasted a validation attempt — restoring identical on-disk state
does **not** reproduce a local-model failure. Run 2's *within-run* retries were byte-identical, which
looks like determinism but is not: re-running the same state in a fresh process produced a completely
different (working) strategy. The fixture has to force the condition structurally, not restore a state
that once triggered it.

Priority: Tier 2 — no user-visible breakage today, but two shipped behaviors are riding on one
run's arithmetic, and one of them can plausibly make context overflow *more* likely.

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

---

## Open Work — Tier 3

**Status: 1 open — P63.9**, below, split out of P63.7 on 2026-08-07 when that item's `Update` half
shipped and its `Engine.Run` half proved to be different work. **Two of its four concerns are
extracted** (budgets and loop detection, both 2026-08-08); the remaining two are the hard pair. P61.6 (2026-08-06) was the last before
it, and the sequencing finding it produced is
recorded with its write-up: built **second** in its batch rather than last, it turned P61.1 into
option wiring and closed P61.3 with no production code, so the "write each fix twice and delete one
copy" cost the item worried about was never paid. Before it: P59.9, P60.2, P60.4 and P57.1. See
[releases.md](releases.md).

### P63.9 — `Engine.Run`: the half of P63.7 that needs a design answer, not code motion

Split out of P63.7 when its `Update` half was built, because the two halves are not the same kind of
work and bundling them hid that. P63.7 is now closed by the `Update` split alone; this carries the
part it deliberately deferred.

`engine/engine.go:518` — `Engine.Run`, originally **725 lines, 119 branch points, 10 levels of
nesting** (now 654 after two passes), holding budget enforcement, compaction triggers, nudge
retraction, loop detection and guard retries in one scope where each interacts with the others. The **33 `// Pxx` markers inside it** are
the tell: it has become the place behavior is added *because* it is already where all the state is,
which is self-reinforcing.

**Pass 1 of 4 landed 2026-08-08: the run budgets** (`internal/engine/budget.go`). Budgets went first
because the ownership question had an unusually clean answer — three of the four bounds are pure
reads of the cost tracker and own nothing, and the fourth owned exactly one thing, the run's start
instant, previously a bare local threaded by hand into five calls across 600 lines. Once that is a
field on a `runBudget`, the concern has nothing left in `Run` to interact with. The two gates
collapsed from three duplicated inline checks each into one `budget.exceeded()` call; check order
(cost → tokens → time) is observable and was preserved. `Run` is now **685 lines** — a number that
undersells the change, since the point was deleting the hand-threaded `runStart`, not the line count.
Gated on `go test ./...` and the eval golden transcripts (`TestScenario_BudgetAbortsSecondTurn`
included). Write-up in [releases.md](releases.md).

**Pass 2 of 4 landed 2026-08-08: loop detection** (`loopGuard` in `internal/engine/loopdetect.go`).
`Run` is now **654 lines**. The ownership question here had a different answer from budgets', and
finding it *was* the pass: **two of the concern's variables were at the wrong scope.**
`loopNudgePending` was declared among `Run`'s run-scoped flags and `loopRecorded` was re-declared each
iteration, but both are set by the loop gate and consumed after that same turn's tool round, with no
path between them that continues the loop. They are **per-turn state in a run-scoped scope** — which
is exactly how this function accumulates interactions it does not really have, since anything else in
`Run` could read or write them and nothing said it shouldn't.

So the split is not "move the detector": the guard owns what genuinely survives a turn (the window,
the outcomes, the threshold the messages quote), and the gate returns a **`loopVerdict` value** the
caller holds for the rest of one iteration. Per-turn state can no longer outlive the turn because it
is a value, not a variable. The nudge *count* deliberately stayed in `nudgeState`, matching the split
already in the tree for the sibling concern — `toolFailureTracker` owns the failure counters while
`nudgeState` owns `toolFailureNudges`/`toolFailureOutstanding` — so it is passed in rather than
duplicated. A disabled detector is a nil `*loopGuard` whose methods tolerate a nil receiver, so `Run`
lost its nil checks too.

Gated on `go test ./...`, `-race`, the eval golden transcripts, **and the full live tier** (all three
subtests passed, 2026-08-08 — see below). Two mutation checks confirmed the gate's tests are not
vacuous. Write-up in [releases.md](releases.md).

**Two concerns remain: compaction and guard retries** — the hard pair, and they should go last and
separately. They interact with each other and with retry, so neither has the clean-ownership shape
that made passes 1 and 2 safe. Pass 2's method (find the state at the wrong scope, return it as a
value instead of storing it) is worth trying on them, but neither is expected to yield as cleanly:
compaction genuinely mutates `conv` mid-run, which no value can encapsulate.

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

**Pass 2 ran it, and the tier is now worth running** (qwen3:14b, 2026-08-08, after P63.11): all three
subtests passed, including `FixSeededBug`, which had failed on the pass-1 run. That is **not** evidence
this pass fixed anything — P60.4's control group had already attributed the earlier failure to the
model, and the model this time simply ran the script, made the correct `int(row["temp"])` edit and
re-ran it. What it does show is that the tier can now produce a clean green, so a future red over a
P63.9 pass is informative rather than expected.

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
