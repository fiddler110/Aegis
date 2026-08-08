# Aegis Capability Roadmap

**Last updated:** 2026-08-08 (eighteenth pass — **P63.9 closed: the fourth and last concern, guard
retries, extracted from `Engine.Run`**, which is now 497 lines against the original 725, and
**P63.12 filed and closed** by that pass, emptying Tier 3; seventeenth pass: P63.9's third concern, compaction, extracted;
sixteenth pass: **P63.9's second concern, loop detection, extracted
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

**10 open items**, recounted from the headings, and the shape of the list changed more than the
count. **P63.9 closed 2026-08-08** with its fourth and last concern extracted from `Engine.Run`;
**P63.12** was filed by that pass and **closed the same day**. Then P62.2's fixture was built and run,
and **three items came out of running it** — **P62.4** (a live silent-truncation event), **P62.5**
(the P62.3 ladder, which turns out never to have had a heading of its own), and **P62.6** (the base
prompt costs 7,119 tokens before any work). None of the three came from a review; all three came from
one measurement. P63.11, P63.8 and P62.1 also shipped that day. Before them, the P63.x review
filed 7 on 2026-08-07 and **all 7 were built the same day** (P63.1-P63.7; the `Engine.Run` half of
P63.7 was split out rather than built). Write-ups in [releases.md](releases.md).

**Tier 1 is empty. Tier 2 holds four** — **P62.2** (now answered: the gate loses 2.2x, revert
recommended pending a large-window check), **P38.1** (still the live conformance re-run), and two the
fixture produced: **P62.4** and **P62.5**. Tier 2 has buildable work again for the first time in three
passes. **Tier 3 holds one**, **P62.6**. Tier 4 is unchanged at five: **P61.7** (remainder),
**P60.3**, **P52.14**, **P25.9**, **P63.10**.

**Every open item is now either a live-run measurement or a parked one.** There is no queued build
work anywhere in the tiers, which has not been true at any earlier pass — and it puts the weight on
the adversarial fixture that reliably crosses the compaction trigger. **P62.2** has been owed it
since it was filed, and P63.9's compaction pass needs it too (its live run never fired compaction at
all). Built 2026-08-08 as `TestLiveWorkflowCompactionPrefixCacheGate`.

*Correction, 2026-08-08:* an earlier version of this line claimed the same fixture also closes
**P62.3**. It does not, and building it is what showed why. P62.3's ladder fires on a genuine
**context overflow** inside the phased drive's findings phase; this fixture crosses the *compaction
trigger*, which is a different threshold reached by a different route. Forcing an overflow is a
separate construction and P62.3 stays owed. Two debts, not three.

**What P63.9 taught across all four passes — the catalogue is the durable output, not the line
count.** Each pass asked one question, *what state does this concern actually own*, and each got a
**different shape** of answer. That is the reusable result:

| pass | concern | the state was… |
|---|---|---|
| 1 | budgets | **owned by nothing** — a bare local (`runStart`) hand-threaded into five call sites |
| 2 | loop detection | **per-turn, living at run scope** — fixed by returning a value, not storing a variable |
| 3 | compaction | **run-scoped, living with the wrong owner** — five variables reachable by 600 lines |
| 4 | guard retries | **inter-turn carry**, whose set / consume / clear were three separate sites |

Pass 4's shape is the one the other three do not cover and the one most likely to recur. `constrainNext`
lived for exactly one iteration boundary — set at the end of turn N, consumed at the start of turn
N+1, and required to be cleared in between or it would silently re-shape every later turn. Nothing
enforced that; two comments ~200 lines apart *described* the discipline. `takeFormat` returns the
carry and empties it in one expression, so the caller cannot forget the second half because there is
no second half. **When a comment explains a discipline, look for the API that would make the
discipline unnecessary.**

**What P63.9's compaction pass taught: mutating shared data is not the same as sharing state.** The
item filed compaction as half of "the hard pair" precisely because it rewrites `conv` mid-run, which
pass 2's return-a-value technique cannot encapsulate. True, and not the obstacle it reads as — every
write compaction makes to `conv.Messages` is its own *output*, not a variable another concern also
writes, so there was nothing escaping to encapsulate. The two passes now bracket the question P63.9
exists to ask: pass 2 found **per-turn state at run scope**, pass 3 found **run-scoped state at the
wrong owner** — five variables declared where 600 lines could reach them and touched by one 70-line
block. Ask both questions of the last concern, not just pass 2's.

**Its second lesson is about the tests, and it generalizes past this item.** Six mutations were run
against the extracted code and **three survived** the suite as it stood — including changing the
P28.4 threshold the test is *named* for from two to three. A short fixture cannot tell adjacent
thresholds apart, and a count assertion cannot tell *when* something fired. Asserting the **sequence**
of calls rather than the count killed all three. Where a pass moves code that a green test already
covers, mutate before believing the green.

**What P63.9's loop-detection pass taught: the extraction is worth doing even where the concern is
already its own type.** `loopDetector` had lived in its own file since P53.2, so the pass looked like
it should be a move. It wasn't — the finding was that **two of the concern's variables were at the
wrong scope**, both set by the loop gate and consumed after the same turn's tool round while being
declared where the whole 685-line function could reach them. Naming the per-turn half and returning
it as a *value* (`loopVerdict`) is what removed the interaction, not moving the detector. The lesson
generalizes: **ask where the state's lifetime actually ends, not which file the type lives in.**

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
2026-08-07, all 7 built the same day) → **1 open**, filed by the build rather than by the
review: **P63.10 only** (P63.8 and P63.11 shipped 2026-08-08; P63.9 closed the same day after four
passes, and the P63.12 it filed on its way out was built the same day). P61.x
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

**What to do next: build the adversarial compaction fixture.** It is the single highest-leverage
thing open, because it stopped being validation owed and became **an instrument three items are
blocked on**: **P62.2** (the prefix-cache pruning gate, unmeasured since it shipped), **P62.3** (the
overflow-escalation ladder, unvalidated because it never fired live), and now P63.9's compaction pass
— whose live run turned out never to exercise compaction at all. Build it once, use it three times.

The pattern already exists in the tree: `writeBigRepoMapFixture`, reworked during P63.11, sizes
itself in a dimension that scales, **grows until it actually clears its threshold, and asserts that it
did**. That last assertion is the only reason the previous fixture's rot was caught. Two cautions
carry over from P62.2's own write-up: restoring identical on-disk state does **not** reproduce a
local-model failure, so the fixture must force the condition structurally; and read every result with
`/api/ps` in view, since a resident model decides the window regardless of config.

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

**Four cautions for whoever runs this tier next, all learned the hard way here:**

- **The tier was blind to every engine notice until 2026-08-08, and silence looked like data.**
  `drainWorkflowEvents` did not handle `api.KindNotice`, and the daemon logger it builds is pinned at
  `LevelWarn`, so compaction, the context-full warning, loop-detector nudges and tool-failure
  correctives appeared on neither channel. Fixed during P63.9 pass 3. The general form is the same as
  the fixture-rot entry below: **a tier that cannot observe the subsystem under test reports every
  result as being about something else.** Before grading a pass on this tier, check that the tier can
  see the thing the pass changed.
- **`FixSeededBug` is flaky on qwen3:14b — it has now failed and passed on unchanged code.** Do not
  read a single red as a regression; re-run the subtest alone, and use `TestLiveWorkflowBaseline`
  (P60.4) when attribution actually matters.

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

**What P63.9 cost, for the next item that proposes decomposing a hot function.** Four passes, four
separate landings, ~1,035 lines of new files against 228 removed from `Run`. The warning it carried
throughout — that it could not be made safe by technique alone and a botched pass degrades the agent
loop every other item is measured through — held: no pass was mechanical, and each had to re-decide
the boundary. What made it safe was not care but **gating**: every pass ran the golden transcripts,
and every pass mutation-checked the tests over the code it moved. That second habit is what earned
its keep — passes 3 and 4 each found surviving mutations against *pre-existing* tests, five in total,
including two thresholds the tests were named for.

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

### P62.5 — Validate the P62.3 overflow-escalation ladder (it has never had a heading of its own)

Filed 2026-08-08 because this debt was about to be lost, not because it is new. `P62.3`
(`OverflowEscalationDirective` + `maxPhaseOverflowResets`, shipped alongside the prefix-cache gate)
has never had a `### P<n>.<m>` heading — its only record is a paragraph inside **P62.2** saying "the
same fixture validates it". That paragraph is now known to be wrong, and P62.2 is answered, so when
P62.2 closes the ladder's validation would disappear with it.

**Why the P62.2 fixture does not cover it.** Measured while building that fixture: it crosses the
*compaction trigger*, which the ladder does not key on. The ladder fires on a genuine **context
overflow** inside the phased drive's findings phase — a different threshold reached by a different
route. Forcing an overflow is a separate construction.

**What makes it buildable now.** Two things exist that did not when P62.3 shipped. The compaction
fixture establishes the technique (force the condition structurally; a chained workspace makes turn
count a property of the fixture rather than a request — see `writeCompactionFixture`). And **P62.4**
means an overflow is currently *easy* to provoke: the estimate runs at 67-80% of the real prompt, so
a run can be driven past the real window while the engine still believes it has headroom. Note the
order dependency — if P62.4 is fixed first, overflows get harder to reach and this fixture has to
force one deliberately rather than riding the estimate's error.

**Trap, recorded by P62.2 and still applying:** restoring identical on-disk state does not reproduce
a local-model failure. Run 2 of the original investigation replayed byte-identical within-run
retries, which looks like determinism and is not — a fresh process ran the same state to a completely
different, working strategy.

Priority: Tier 2 — same standing P62.2 had: a shipped behavior riding on one run's arithmetic, with
no live evidence it ever fires correctly. Cheap now that the technique and the harness exist.

### P62.4 — Proactive compaction never fired while Ollama silently dropped tokens (measured)

Found 2026-08-08 by P62.2's new fixture, which was built to measure something else. This is the
failure P2.7's proactive compaction exists to prevent, observed with the guard armed and silent.

**The measurement.** `TestLiveWorkflowCompactionPrefixCacheGate`, qwen3:14b, context window pinned to
24,576 and confirmed resolved as such by the daemon (`context window 24576 (from config)` — so this
is *not* a config-vs-served mismatch). The conversation grew one file-read per turn:

| turn | prompt (provider-reported) | prefill |
|---|---|---|
| 0 | 7,119 | 5,980ms (cold) |
| 1-9 | 8,925 → 23,733 | 1,859 → 3,493ms (prefix-cache hits) |
| 10 | 23,637 — **shrinks** | **23,353ms** |
| 11-14 | ~23,757 | ~23,700ms every turn |

**Zero notices fired in the entire run** — no compaction, no fallback, and not the 95%-full warning
either, at 96.7% of the window. Turn 10's shrink-with-10x-prefill is Ollama context-shifting: dropping
the oldest tokens and reprocessing from scratch, every turn thereafter.

**The mechanism.** The engine triggers on `conv.estimatedTokens()` (tokenest), not on what the
provider reports, and the two are far apart.

A second run with a lower trigger *did* fire compaction, and its notices quote the estimate as a
percentage — which pins the gap directly. Gate-off compacted announcing **"context ~64% full"**
(est ≈ 15,729) on a turn the provider reported at **23,637** tokens; gate-on announced
**"~77% full"** (est ≈ 18,923) against ~23,760. **The estimate is running at 67-80% of the provider's
count** — a 20-33% undercount, not the ≥12% lower bound the first run could establish.

That is enough to matter at every window size, and it explains both symptoms: with the trigger set at
85% of the window, a 33% undercount means the real prompt is at ~128% of the window before the
estimate says 85%. The 95%-full notice is gated on the same estimate, which is why the one thing
designed to speak up when compaction *cannot* help was also silent. Even in the run where compaction
did fire, Ollama had already begun context-shifting a turn earlier.

The content here is structured numeric records; P41.1 unified this estimator precisely because it
undercounted CJK/non-ASCII, so code or other scripts may be worse.

Note this is the same estimate that P53.6's shim-catalog addend and P59.1's completion reserve are
corrections to — both of which exist because the estimate was known to be off in a *known direction*.
This is the same class of error with no correction applied.

**What to decide, and measure first.** Three candidate directions, in preference order: (a) calibrate
tokenest against provider-reported `prompt_eval_count` — the daemon already receives that number every
turn and could learn a per-model correction factor rather than guessing; (b) apply a safety margin to
the trigger sized from the observed undercount; (c) treat a provider-reported count that exceeds the
estimate as authoritative and re-trigger on it. (a) is the only one that fixes the 95% notice too.
Do not build from this write-up without re-measuring — the undercount's size is a lower bound from one
content type, and the tiering criteria's own lesson is that a filed item is usually wrong about scale.

Priority: Tier 2 — a live, reproducible silent-truncation event on a local model, which is the exact
class of failure P52.1/P2.7/P59.1 were built to close, and the fixture that reproduces it is now in
the tree. Not Tier 1 only because it needs a small window to reach and the default local window is
larger; the estimate error is proportional, so a big-window run should be measured before assuming it
is safe.

### P62.2 — MEASURED 2026-08-08: the gate loses 2.2x on wall clock; recommend revert

**Result, and it reverses the item's premise.** `TestLiveWorkflowCompactionPrefixCacheGate`,
qwen3:14b, window 24,576 (confirmed resolved), 14-file chained read, same fixture both arms:

| | gate **off** | gate **on** |
|---|---|---|
| wall clock | **1m32s** | 3m19s |
| total prefill | **64,958ms** | 128,005ms |
| turns whose context shrank | 2 | 2 |
| overflows | none | none |

Both arms are byte-identical through turn 10. Then gate-off prunes (23,637 -> 14,977, one 9,286ms
hit) and its next three turns cost 2.6-3.0s each. Gate-on defers, and its next three turns cost
**~23,750ms each** at a prompt pinned to ~23,758 — before pruning at turn 14 anyway.

**The mechanism is structural, not incidental.** The gate defers pruning until the conversation is
near the window, which is exactly where Ollama begins context-shifting — so every turn in the
deferral window is already a full reprocess. **The gate protects the prefix cache in the one regime
where the prefix cache is already gone**, pays ~23.7s/turn to wait, and then pays the prune's cost
regardless. Pruning early is what keeps the conversation small enough for the cache to survive at all.

That argument does not depend on this workload: the gate's threshold (25% free) *by construction*
places the prune next to the window, which is the saturation regime. Tightening the threshold means
pruning earlier, which is what turning the gate off already does — so "tighten" and "revert" converge.

**Recommendation: revert `PreservePrefixCache` to unconditional pruning**, per this item's own stated
criterion ("if the gate does not clearly win on wall clock without adding overflows... revert it").
Keep `compaction.preserve_prefix_cache` as the escape hatch and the A/B harness.

**What would change the recommendation, and should be checked before ripping code out.** This is n=1
per arm, one model, one workload, at a 24,576 window; the motivating measurement was a 40k+ window
over a 142-minute drive with a very different shape. On a **large** window the gate uses a fixed 40k
buffer rather than a ratio, so the deferral may end well short of saturation and the original
arithmetic could still hold. Re-run the same fixture with the window raised past
`largeContextWindowThreshold` before deciding. The harness now makes that one command.

*Original filing follows, for the reasoning the measurement was testing.*

**P62.2 as originally filed**, kept because it is the reasoning the measurement was testing:

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

**Status: 1 open — P62.6**, below, filed 2026-08-08 off P62.2's fixture. **P63.12 was filed and
closed the same day** (2026-08-08) — filed by P63.9's
last pass, then built once its premise was checked rather than assumed. That check is the part worth
keeping: the item as written blamed "compaction rewrites the transcript" in general, and measuring
narrowed it twice. `pruneStaleToolResults` only touches tool_use/tool_result blocks, so **pruning is
not a vector at all** — summarization is, since everything before the keep-recent tail (default 8) is
replaced outright. And of the six nudge families, only **one** is actually harmed: the counts
(`guardRetries`, `loopNudges`, `zeroToolNudges`, …) record what this run injected and remain true
whatever happens to the transcript. `toolFailureOutstanding` was the sole flag making a claim *about
the transcript's current contents*, and the sole one that suppresses re-injection of a message whose
entire purpose is to be visible to the model.

What made it reachable rather than theoretical is a detail neither the filing nor the original P52.3
work names: `shouldNudge` fires on `allErrorRounds >= 3` **or** `sameErrorRounds >= 3`, while
`shouldAbort` fires only on `allErrorRounds >= 6`. So a model whose rounds are partly succeeding
nudges at three and **never aborts** — it runs to the iteration cap, far past the keep-recent tail.
The fix deletes the flag and asks `hasNudge(conv, prefix)` instead, which is the rule
`retractGuardCorrectives` already documents ("identified by content rather than by indices... so a
compaction or prepare-step rewrite mid-run can't shift the bookkeeping"). Write-up in
[releases.md](releases.md).

**P63.9 closed 2026-08-08, all four concerns extracted.** `Engine.Run` went **725 → 497 lines**
(-31%), max nesting 10 → 6 levels, and its `// Pxx` marker count 29 → 21 — the metric the item cared
about most, since those markers were the evidence that behavior was being added to `Run` *because*
that is where all the state already was. The four concerns now live in `budget.go` (172),
`loopdetect.go` (386), `compact.go` (266) and `guardretry.go` (211). Each pass landed separately,
gated on the golden transcripts, and each found the state in a **different shape** — that catalogue is
the item's durable output and is recorded with the write-ups in [releases.md](releases.md). P61.6
(2026-08-06) was the last Tier-3 item before
it, and the sequencing finding it produced is
recorded with its write-up: built **second** in its batch rather than last, it turned P61.1 into
option wiring and closed P61.3 with no production code, so the "write each fix twice and delete one
copy" cost the item worried about was never paid. Before it: P59.9, P60.2, P60.4 and P57.1. See
[releases.md](releases.md).


### P62.6 — The base prompt is 7,119 tokens before any work, on the *trimmed* local profile

Measured 2026-08-08 while sizing P62.2's fixture, and the number is larger than anything in the tree
assumes. On qwen3:14b against a temp workspace of 14 `.txt` files — no repo to map, no memory, no
skills enabled — the first turn's provider-reported prompt was **7,119 tokens**.

**That is the trimmed profile, not the default one.** The fixture passes `PromptProfile: ""` against
`http://localhost:11434`, and `LocalPromptProfile()` auto-detects loopback as local — so P25.6's
reduced system prompt and deferred network/security tool schemas were already applied. 7,119 is what
is left after the trimming.

**Why it matters here specifically.** Aegis's stated target is local models on consumer hardware, and
the recorded constraint for this machine is 16GB VRAM — which in practice means an 8k-16k served
window. At 8,192 the base prompt is **87% of the window before the first tool call**, which is not a
tuning problem but a "the agent cannot do multi-turn work" problem. It is also what made P62.2's
fixture fail twice: the model got four reads in and ran out of room.

**Measure before building, and measure the composition first.** The single number does not say where
it goes. Split it — system prompt, tool schemas (50+ builtins, and the schema block is the obvious
suspect), `<skills_available>`, `<repo_map>`, memory — before proposing anything. Candidate
directions only after that: progressive tool disclosure (the pattern `internal/skills` already uses
for skills, applied to tool schemas), a smaller default exposure under the local profile, or a
tool-schema budget analogous to `repomap.max_bytes`. `TestEffectiveSystem_localProfileTrimsPrompt`
already asserts the local profile is *smaller*; nothing asserts it is small enough, and a byte
assertion on the assembled prompt would make this a default-suite regression rather than a thing
rediscovered by a live run.

Priority: Tier 3 — real, measured and squarely on the primary use case, but the fix is a design
question (what does the agent stop being able to do?) rather than a defect with a known repair, and
no user has reported it. Promote to Tier 2 if the composition split shows one component dominating.

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
