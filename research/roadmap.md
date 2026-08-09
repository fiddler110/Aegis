# Aegis Capability Roadmap

**Last updated:** 2026-08-09 (nineteenth pass — **P62.4, P62.2 and P62.5 closed, emptying Tier 2 of
everything except the standing conformance umbrella**. The pass's result is not the three closures
but what the first one did to the second: fixing the token estimate (P62.4) **reversed P62.2's
already-acted-on verdict**, and the gate it had reverted is now restored on a re-measurement that runs
1.7x the other way. Two items filed off the same work — **P62.7** (Tier 3) and **P62.8** (Tier 4);
eighteenth pass: P63.9 closed, its fourth and last concern extracted from `Engine.Run`, which is now
497 lines against the original 725, and **P63.12 filed and closed** by that pass, emptying Tier 3;
seventeenth pass: P63.9's third concern, compaction, extracted;
sixteenth pass: **P63.9's second concern, loop detection, extracted
from `Engine.Run`**, and the full live tier ran green over it; fifteenth pass: **P63.11 shipped and
the live tier now produces a real prompt-profile measurement**; fourteenth pass: P63.8 and P62.1
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

**9 open items**, recounted from the headings: three closed and two filed, against the last pass's
ten. **P62.4, P62.2 and P62.5 closed 2026-08-09**; **P62.7** (Tier 3) and **P62.8** (Tier 4) were
filed off that work, and P62.3's ladder finally got the validation it had been owed since it shipped —
its *mechanism* driven end to end for the first time (no test had ever called `drive.Run`), with one
residual named below rather than quietly folded in. Write-ups in [releases.md](releases.md).

*The residual, stated so it is not mistaken for coverage:* P62.5 closes on a deterministic test that
drives the real reset loop and asserts the sequence of rungs. It hands that loop an error it has
*declared* to be a context overflow, so the one thing still uncovered live is the **classification**
link — that a real Ollama truncation is recognised as an overflow rather than as a malformed call.
That link has unit coverage over recorded real server text, and a live fixture
(`TestLiveWorkflowForcedContextOverflow`) exists but **has not yet fired**: qwen3:14b answered the
oversized-write prompt in text and walked the max-tokens continuation path instead of truncating a
tool call. The fixture records what to try next. It is not evidence of anything today.

**Tier 1 is empty. Tier 2 holds one** — **P38.1**, the live conformance re-run, which is the only
open item anywhere whose outcome produces new information rather than new code. **Tier 3 holds two**,
**P62.6** and **P62.7**. Tier 4 is at six: **P62.8**, **P61.7** (remainder), **P60.3**, **P52.14**,
**P25.9**, **P63.10**.

**The finding of this pass is a method one, and it cost a shipped decision to learn.** P62.2 had been
measured, decided and acted on — the prefix-cache gate was reverted, on the item's own stated
criterion, from a clean A/B showing it 2.2x slower. Then P62.4 fixed the token estimate that gate's
trigger runs on, and re-running the *identical* fixture inverted the result: the gate now wins ~1.7x
on wall clock, reproduced across two runs. The first measurement was not misrecorded and was not
noisy. It was taken on a system whose compaction trigger fired 20-33% late, which put **both arms**
inside the regime where Ollama silently context-shifts — and there the prefix cache is already gone,
so the gate had nothing to protect and its deferral was pure cost. **Before measuring an
optimization, check the instrument the rest of the system is running on.** Note that the item's own
caution — "this is n=1 per arm, re-run before ripping code out" — would not have caught this: a second
run reproduces a systematic error perfectly faithfully. The defence against this class of error is
not repetition, it is asking what the measurement depends on.

**A corollary worth keeping, found the same day.** The live tier was silently cacheable. A re-run of
the A/B returned byte-identical wall-clock *and* prefill totals for both arms, which reads as
flawless reproducibility and was Go replaying the first run's cached verdict — Go's test cache keys
on the binary, arguments and environment, none of which change when the thing under test is a model
server's behaviour. Every documented live command now carries `-count=1`. A tier that exists to catch
what a green unit suite cannot is worth very little if its re-runs do not run.

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

**What to do next: P38.1's live conformance re-run.** It is the only open work anywhere whose outcome
produces new information rather than new code, and it doubles as the validation **P57.1** is owed — a
fix aimed at a failure observed exactly once. Every other tier holds either a parked item or a filed
defect with a known mechanism.

*The instrument that unblocked the last three items is now built and reusable.* The adversarial
compaction fixture (`writeCompactionFixture` +
`TestLiveWorkflowCompactionPrefixCacheGate`) was the thing three items were blocked on, and it paid
for itself three times over: it settled P62.2, it *found* P62.4 and P62.7 while measuring something
else, and P62.5's ladder validation was built on the technique it established. What made it work is
worth reusing rather than rediscovering — it forces its condition **structurally** (a chained
workspace makes one-read-per-turn a property of the fixture rather than a request the model may
ignore), it is sized from a measured base prompt rather than from a round number, and it asserts that
it actually crossed its threshold instead of assuming it did. That last assertion is the only reason
two vacuous green runs were caught.

Three cautions carry over for anything built on it. Restoring identical on-disk state does **not**
reproduce a local-model failure, so force the condition rather than replaying a state that once
triggered it. Read every result with `/api/ps` in view, since a resident model decides the window
regardless of config. And pass `-count=1`: Go's test cache will otherwise return a previous run's
verdict for an unchanged live test, which is indistinguishable from a re-run that reproduced.

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

**Status: 1 open — P38.1**, below, and it is not build work: it is the live conformance re-run, the
only open item anywhere whose outcome produces new information rather than new code. **The 2026-08-09
re-test moved it materially without closing it:** a 14B local model built the complete six-file suite
for the first time on this target, and the ten harness defects that run root-caused shipped as P39.16.
It is still unmet — verification did not pass, and the confirming re-run hung (P39.17). **P62.4, P62.2
and P62.5 all closed 2026-08-09** in one pass, and the order matters more than the count — P62.4 was
the instrument the other two were being measured with. **P63.11, P62.1 and P63.8 shipped 2026-08-08**;
P63.3-P63.6 on 2026-08-07; before those, P61.4, P61.5 and P61.8 (2026-08-06). Write-ups for all of
them are in [releases.md](releases.md).

*What closing these three established, and it is a method point rather than a compaction point.*
P62.2 had been measured, decided, and acted on — the gate was reverted on its own stated criterion.
Then P62.4 fixed the token estimate that gate's trigger runs on, and re-running the identical fixture
**inverted the result**, twice. The first measurement was not misrecorded: it was taken on a system
whose compaction trigger fired 20-33% late, which put both arms into the regime where Ollama
context-shifts and the prefix cache is gone regardless. The item's own "n=1, re-run before ripping
code out" caution would not have caught it, because a second run reproduces a systematic error
faithfully. **Before measuring an optimization, check the instrument the rest of the system is running
on.**

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

- **2026-08-09, LFM2.5-2.6B then qwen3:14b vs AiGateway — conformance still unmet, ten harness
  defects root-caused and shipped as P39.16.** The 2.6B produced **zero files in two runs** and is
  below the floor; it is now refused by a pre-flight gate rather than allowed to burn 40 turns, and
  its real value proved to be as an adversarial harness test — it found four defects in an afternoon
  because it does every wrong thing quickly. The qwen3:14b arm then built the **complete suite** —
  six files, ~35KB, all five content phases, every marker cleared — which is further than any prior
  run on this target. Verification did **not** pass: `component-name-consistency` (11 components
  missing from the analysis file), `count-consistency` (a required table deleted), and
  `coverage-ledger-complete` remained after the bounded fix loop. Two of those three were then fixed
  structurally (P39.16's routing and table guard); the third re-run hung before reaching the verdict.
  **The organizing finding is that all ten defects were the same shape:** a tool that held the
  information the model needed and returned an error without it. See P39.16 in
  [releases.md](releases.md). Three items remain open below: **P39.17** (the hang), **P39.18** (typed
  script tools), and the conformance verdict itself, which is this item.

**Direction (user, 2026-07-24):** the strongest lever is making local models **piecemeal both their
reads and their writes**, then finishing with a **quality-validation pass**; P39.12-P39.15 implement
exactly that. **P39.16 (2026-08-09) extends it one step further:** piecemeal writing still failed
while it went through `edit_file`, because an anchored edit asks the model to *reproduce* text rather
than only produce it. Handle-based tools (`fill_marker`, `edit_section`) remove the reproduction, and
that is what finally made the fill loop reliable on a 14B model.

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

**Status: 4 open — P39.17, P39.18, P62.6 and P62.7**, below. P39.17 and P39.18 were filed
2026-08-09 off the P39.16 validation run and are the two things standing between that batch and a
confident P38.1 verdict: one makes unattended runs untrustworthy to *measure* (a turn can hang
forever without tripping any progress guard), the other is the next failure class after tool
selection (arguments, not tools). P62.6 and P62.7 were both filed off P62.2's fixture (2026-08-08 and
2026-08-09 respectively). **P63.12 was filed and
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

### P39.17 — a phased drive can go silent indefinitely with no wall-clock floor

**Filed 2026-08-09 from the P39.16 validation run.** The drive stopped producing output at 18:12:51
and was still "running" 14 minutes later: `aegis.exe` had accumulated **0.5s of total CPU** since
launch (startup and nothing since), the log had not grown by a byte, and the Ollama process was
likewise idle. Nothing in the harness noticed. Every existing guard is *progress*-shaped — the
no-progress nudge counts turns, the loop detector compares tool calls, the failure breaker counts
failed rounds — and all of them require turns to keep completing. A turn that never returns advances
no counter, so an unattended run can sit dead for hours and look exactly like a slow one.

`cost.max_wall_clock_per_run` exists but is off by default and bounds the *whole run*, which is the
wrong instrument: a legitimate threat-model drive runs for hours, so any value large enough to be
safe is far too large to catch a hang. What is missing is a **per-turn** stall detector — no stream
event and no tool call for N minutes — which is unambiguous in a way a whole-run budget can never be.

Not Tier 2 because it costs an operator a wasted wait rather than a wrong artifact, and the on-disk
suite always survives (the drive is resumable by design). But it silently invalidates unattended
runs, which is precisely what P38.1 is trying to measure, so it blocks confident conformance testing.

**Repro:** not yet isolated. Both processes idle with a live HTTP request outstanding suggests a
request that never returns and never times out; the provider's own retry/timeout path is the first
place to look. Capture: whether the adapter had an in-flight request, and whether any read deadline
was set on it.

**Closure condition:** a stalled turn is detected and either retried or reported, and a re-run of the
P38.1 drive completes or fails loudly rather than hanging.

Priority: Tier 3 — no data loss, but it makes long unattended runs untrustworthy to measure.

### P39.18 — tool arguments are the next wall after tool selection

**Filed 2026-08-09 from the P39.16 validation run.** With per-phase narrowing and handle-based
editing in place, tool *selection* stopped failing on qwen3:14b — every remaining stumble in the run
was a malformed **argument**: `scaffold.py --framework` with the value omitted, and
`2-<framework>-analysis.md` with the placeholder never substituted. Both were caught by corrective
errors P39.16 added, which is the recoverable outcome; but the model is still composing a command
line as a *string*, which is the same class of failure `fill_marker` removed from editing.

The fix has the same shape as the two that worked: stop asking the model to produce a format it has
to get exactly right. Wrap the bundled skill scripts (`recon.py`, `scaffold.py`, `inventory.py`,
`verify.py`, `normalize_ids.py`) as first-class tools with typed JSON schemas, so `--framework`
becomes a required enum the harness renders rather than a token the model has to remember to
follow with a value. That also removes the shell tool from the setup phase, which is currently the
only reason it is exposed there.

The weaker alternative is JSON-schema-constrained decoding via `Request.Format`, already present in
the Ollama adapter and currently used only on schema-guard corrective retries. It constrains the
symptom; typed tools remove the failure mode. Prefer the latter.

**Closure condition:** the threat-model drive completes its setup phase without composing a shell
command line, and an argument error for a bundled script becomes structurally impossible.

Priority: Tier 3 — the errors are already corrective rather than silent, so this buys reliability and
turn count, not correctness.

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


### P62.7 — Compaction is invoked every turn past the trigger and prunes for almost nothing

Found 2026-08-09 in the gate-off arm of `TestLiveWorkflowCompactionPrefixCacheGate`, once P62.4's fix
made the trigger fire at the right time and the behaviour past it became visible for the first time.

**The measurement.** qwen3:14b, 24,576-token window, `preserve_prefix_cache=false`. From turn 5
onward the run emits a compaction notice on *every single turn*:

| turn | prompt | prefill | notice |
|---|---|---|---|
| 4 | 14,463 | 2,483ms | — |
| 5 | 14,519 | 8,540ms | context ~62% full — compacted 11→11 messages |
| 6 | 14,575 | 8,603ms | context ~63% full — compacted 13→13 messages |
| … | … | ~9,000ms | … 10 notices in total |
| 14 | 15,143 | 9,396ms | context ~66% full — compacted 29→29 messages |

Read the message counts: **unchanged every time**. The deterministic pre-pass finds *something* to
strip each turn (`prunedChars > 0`, so `changed=true`) but never enough to bring the estimate back
under the trigger — so the next turn crosses it again and prunes again. Each of those rewrites the
middle of the conversation and costs a full ~9s prefill instead of ~2.5s, and the prompt still climbs
(14,519 → 15,143) throughout.

**Why it is separable from P62.2.** The prefix-cache gate rate-limits this thrash, which is part of
why it now measures ~1.7x faster — but only *by accident*, as a side effect of a threshold chosen for
a different reason. On a cloud backend the gate is off by design (there is no prefix cache to
protect), so the same thrash runs there unmitigated; it is merely cheaper, since a cloud API re-reads
the prompt anyway. The wasted work is real on both.

**Candidate directions, in preference order.** (a) A minimum-yield check: if a prune frees less than
some fraction of what stands between the conversation and the trigger, record that and do not re-run
it until the conversation has grown by at least that much. (b) A cooldown in turns. (c) Let the
pre-pass report *how much* it can still free, so the caller can tell "pruned a little" from "pruned
all there is" — today `changed=true` conflates them, which is the root of the conflation.

Prefer (a): it keys on the thing that actually matters and degrades gracefully as a conversation
approaches genuinely-unprunable. Measure before building — the yield per prune is not recorded
anywhere today, and this write-up infers it from message counts rather than from bytes.

Priority: Tier 3 — real, repeatable wasted work with a clear mechanism, but no user-visible breakage
(every run above completed correctly) and it sits behind a decision about what the pre-pass should
report, which makes it larger than the one-line threshold change it first looks like.

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

**Status: 6 open**, all blocked or explicitly parked; none has a build trigger yet. P62.8 joined
2026-08-09 (blocked on hardware, not on a decision); P63.10 joined 2026-08-07, filed off the P63.7
refactor rather than by a review.

**How to use this tier.** Four items have now been measured and closed — P59.10 and P52.16
(2026-08-05), P61.7's in-repo half and P49.3 (2026-08-06) — and every one taught the same lesson:
**the measurement contradicted part of the filed item.** P61.7 named an unmeasured external
dependency that turned out to be our own code; P49.3's gate turned out to be unmeetable by the work
it proposed. Building either from its write-up alone would have produced the wrong fix, or the wrong
non-decision. Take the measurement first, then re-read the item — do not treat a Tier-4 write-up as a
build plan. Details in [releases.md](releases.md).

### P62.8 — The prefix-cache gate's large-window regime has never been measured

Filed 2026-08-09 when P62.2 closed, to keep the one question that closure does not answer from
disappearing with it.

`compaction.shouldPrune` has two regimes. Below `largeContextWindowThreshold` (200,000) the gate
fires at a **ratio** — 25% free — which by construction places the prune near the window. Above it the
gate switches to a **fixed 40k buffer**, which on a large window is a much smaller fraction and so
places the prune far earlier, in relative terms, than anything measured so far.

Everything known about this gate comes from a 24,576-token window, i.e. entirely from the ratio
branch, and P62.2's history is a specific warning against generalising from it: the same fixture gave
opposite verdicts before and after P62.4, because what mattered was *where in the window* the prune
landed relative to the backend's context-shifting point. The buffer branch changes exactly that
relationship and has no measurement at all.

**Why parked rather than queued.** It needs a backend serving a >200,000-token window. The models on
hand top out at 40,960 (qwen3:14b) and 262,144 (gemma4:12b), and the second is unreachable in
practice: a 200k+ KV cache on 16GB of VRAM plus 16GB of system RAM is swap-bound, which would measure
the paging subsystem rather than the gate. This is a hardware block, not an open design question —
nothing needs deciding, only running.

**How to run it when hardware allows.** The harness already exists and needs no changes:
`AEGIS_EVAL_MODEL=<model> go test -tags live_workflow -count=1 ./internal/eval/ -run
TestLiveWorkflowCompactionPrefixCacheGate -v`, with `compactionNumCtx` raised past 200,000 and the
per-file payload in `writeCompactionFixture` scaled up so the chain still crosses the trigger. Note
`-count=1`: without it a re-run returns the previous verdict from Go's test cache.

Priority: Tier 4 — no trigger, no user impact, and blocked on hardware rather than on any decision.
Promote if a large-window local backend becomes available, or if a cloud provider's behaviour ever
makes the buffer branch reachable in a way worth measuring.


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
