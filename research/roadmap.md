# Aegis Capability Roadmap

**Last updated:** 2026-08-18. This document tracks only **open** work and what's next. For
shipped-feature history, batch origins, pass-by-pass narrative, refutation records and full design
rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading with a
`Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so keep it when
adding items.

---

## Status

**26 open items: 21 build (Tier 4 only) + 5 verification-only. Tier 1, Tier 2 and Tier 3 are all
empty** — every remaining build item is Tier 4 with no fired trigger, which this document's own rule
says not to build speculatively. Nine shipped on 2026-08-18, in three sittings: **P66.15**,
**P67.6**, **P67.7**, **P67.8** and **P67.9** (the whole of the then-current "Up next" table except
its parked last row — record in [releases.md](releases.md), *Five rows of Up next, 2026-08-18*), then
**P70.1**, **P70.2** and **P70.3**, the three build rows P66.15's sweep had filed that same morning
(record: [Three rows and a
posture](releases.md#three-rows-and-a-posture-2026-08-18-p701-p702-p703)), then **P70.4**, which
P70.2's build had filed hours earlier (record: [Both halves of the sub-agent
boundary](releases.md#both-halves-of-the-sub-agent-boundary-2026-08-18-p704)).

**Three trust-posture questions were answered on 2026-08-18 and they do not all point the same way,
which is the point.** The swarm mailbox **is** wrapped as untrusted (P70.2) and so is a sub-agent's
result (P70.4), because in both cases content crossed a boundary before being relayed onward;
`security_scan`'s workspace-derived output is **deliberately not** wrapped (P70.3) because a file the
model can already read directly is not a boundary crossing. Zero trust is the stated posture for
*ingestion* and for *relayed* content, not a rule that every byte gets a marker. Settle the next such
question against those three, not afresh.

**Read the P67.7 record before touching `internal/engine`.** That item asked for tool calls to be
dispatched as their blocks complete in the stream, and named four constraints. Building it found two
more the item did not: the P53.2 loop guard can *abort* a run on the complete round's signature, and
the pre-tool-round budget gate exists specifically so a turn whose own usage crosses the cap stops
before its tool calls run — and neither can rule on a prefix of a round. The resolution is a
restriction on *when* early dispatch is active (no spend bound configured; stop at the first
write/execute call), not a weakening of either gate. Anyone widening it is reopening that decision.

**P66.15 was an audit, and audits produce items.** Seven verified findings were fixed in place with
regression tests — including a Medium in `internal/tui/toolview.go` proving P66.6 was *not* the only
unsanitized ingestion point, and a Medium in `captureShellWrites` where `git status --porcelain`
paths (repo-relative) were joined onto the workspace root, so `/rewind` silently restored nothing
whenever the workspace sat inside a larger repo. Four more were verified and deliberately left
unfixed because each is a design decision rather than a line; they are filed as **P70.1**, **P70.2**
and **P70.3** rather than carried silently inside a closed item. The sweep also records what it
checked and cleared — `parseNmapXML` is not XXE-exposed, and the 60s auth-lockout cap is defeatable
but irrelevant next to a 32-byte `crypto/rand` token — so the coverage gap is closed as *swept*
rather than as skipped.

**The P67 batch is a comparative reading of an external agent implementation**, not a review of this
codebase. On 2026-08-16 the leaked Claude Code CLI source (`paperwave/claude-code-cli-leaked` @
`main`, 1,001 files) was read against Aegis for mechanisms worth having here; the write-up, with the
per-item evidence and the four ideas rejected, is at
<https://claude.ai/code/artifact/ebdb26ce-2bfb-4939-871d-8e4407ca6e3d>. **P67.1**-**P67.14** are the
fourteen that survived as filed items; a fifteenth — the render-fidelity constraint on transcript
search — is folded into **P66.15**'s full-text-search bullet, where the feature it constrains was
already parked. Three constraints apply to every P67 entry and are not repeated in them:

- **That source is leaked proprietary code. Nothing may be transcribed from it.** Each item is a
  design reading — a mechanism and the reasoning behind it — and needs an independent Go
  implementation written from this document, not from that repository.
- **The leak is partial.** `src/utils/**` is absent, so permission internals, `forkedAgent` and
  `toolResultStorage` were legible only through call sites. Where an entry's claim about *their*
  implementation rests on a call site rather than the code, it says so.
- **Every claim about Aegis in these entries was checked against this tree**, at the file and line
  cited, not against the docs. The claims about their side were not, and cannot be — treat them as
  motivation, never as a specification.

**The P66 batch is a full-stack code review**, not a feature line. Six specialist reviewers, an
adversarial debate (advocate / refuter / arbitrator) and a static-analysis pass produced 70 findings
against HEAD `3c2b57b`, recorded in [CodeReview.md](CodeReview.md) with per-finding evidence. The
items filed below (**P66.11**-**P66.26**) carry every finding worth acting on; each names the finding
IDs it closes, so the review document is the rationale and this document is the work. **Seventeen of
the batch shipped on 2026-08-15/16** — P66.2, P66.1, P66.4, P66.3, P66.6, P66.7 (both halves), P66.8,
the P66.24 flake found while building P66.4, then P66.5, P66.16, P66.10 and P66.9, and finally
P66.14, P66.11, P66.21 and P66.12 — including both Criticals and **all six** of the findings that
were exploitable the day the review landed. Their
build records, and the corrections several of them make to the item they were written from, are in
[releases.md](releases.md).

**Read those corrections before acting on CodeReview.md directly.** Three of the five items shipped
in the second sitting contradict the finding they were built from: VULN-03's suggested
`::ffff:0:0/96` addition would have blocked the entire public internet, LLM-04 drops *every* tool
call on a 1-based backend rather than only trailing ones, and P66.7's 11,611-token figure was already
stale when it was filed. Two sub-items were deliberately left undone and are refiled as **P66.25**
(SEC-07) and **P66.26** (PERF-02) rather than being carried silently inside a closed item.

Tiers 1-4 are **build work** — every item there requires writing code that doesn't exist yet.
**Verification work** is its own track, not a tier: every item in it is code that is *already
written*, sitting behind one gate — a live-model run producing evidence the item's closure
condition names. Mixing the two under one tiering scheme was misleading a reader into treating
"go run a test" and "go design and build a feature" as the same kind of next action. See
[Verification Work](#verification-work) below.

- **Tier 1:** 0 — **P69.6** (nothing plans a resident set, so every model is sized as if it were
  alone) was filed and shipped on 2026-08-17. Before it the tier had been empty since **P66.5**
  shipped 2026-08-16, closing the last exploitable-today finding.
- **Tier 2:** 1 — **P68.1** (the instrumentation gap the live tier found), and it is deliberately off
  the ranked list because it travels with the parked row #6. (**P66.25**, **P67.2**, **P67.3**,
  **P67.4** and **P67.5** shipped 2026-08-17; **P66.11**, **P66.12**, **P66.21** and **P67.1**
  shipped 2026-08-16.)
- **Tier 3:** 5 — **P66.15**, plus four from P67: **P67.6**, **P67.7**, **P67.8**, **P67.9**.
  (**P66.13** shipped 2026-08-17; **P66.14** 2026-08-16.) **Two of them are unblocked by their
  prerequisite rather than waiting on it:** P67.3 built P67.6's purpose-tag seam, and P67.4 settled
  the cancellation policy P67.7 would otherwise have had to settle mid-refactor.
- **Tier 4:** 20 — **P66.17**, **P66.18**, **P66.19**, **P66.20**, **P66.23**, **P66.26** (PERF-02,
  refiled), the five from P67 (**P67.10**-**P67.14**), plus the nine pre-existing: **P65.4**,
  **P65.5**, **P64.4**, **P64.5**, **P61.7** (remainder), **P60.3**, **P52.14**, **P25.9**, **P63.10**.
- **Verification:** 5 — **P66.22** (two conditions left, both blocked on P68.1), **P38.1**,
  **P62.9**, **P65.2** (prompt half, blocked on P68.1), **P62.8**. (**P65.3** closed 2026-08-16:
  both its questions are answered.)

**What to do next.** **P66.15** — the sweep of `internal/tui` and `internal/security`, 26% of
production Go that nobody has read. It was displaced from the top by P69.6, which is now shipped, so
it returns to row #1. Tier 1 is empty and Tier 2 still holds only the parked **P68.1**.

**P66.13's own correction, which outlives it:** the item named four instances of one root cause and
there were six. `aegis debate` was a fourth bare gate nobody had looked at, `cli/worker.go` was a
fifth that had been fixed once and then drifted two layers behind, and the daemon's own `buildGate`
was dropping the user's configured exec hooks whenever a contextual policy was on. The lesson is the
one the instrument encodes: **counting the instances of a bypass by reading the file where it was
found undercounts it.** `TestEveryEngineCallSiteDecidesItsGate` enumerates them instead. One finding
was also wrong in the safe direction — `cli/dryrun.go` "has no gate at all" is true and harmless,
because it builds no engine and executes nothing.

**Three of the earlier five corrected the item they were built from**, and those corrections outlive
the items too:

- **P67.5's recall path has no production callers at all.** `LoadRelevant`/`FormatEntries` are
  unwired — memory reaches the prompt through `Sources.Load()`, which injects both files whole and
  unfiltered — so the symptom the item described (a top entry re-injected every turn) could not have
  been observed. The dedupe, freshness and gotcha bias are built and tested; **wiring a caller is
  separate work nobody has filed**, and should be, before the next item that assumes scored recall is
  live.
- **P67.2's memoization is safe on only four of ten prompt sections.** Five read state Aegis mutates
  mid-conversation (skills, memory, context files, repo map, deferred tools), so the item's
  memoize-by-default framing would have served stale prompts. The invariant and its test are the
  deliverable; the cache is incidental. The volatile set is now the exhaustive, justified list of what
  breaks prefill reuse each turn — which is an input **P67.6** can use.
- **P67.3's item lists cron among the callers sharing one retry policy.** Cron fires shell commands,
  never a provider request, so there is no cron purpose to tag. The other eight caller classes were
  real and are tagged.

**Everything that is left of the sitting is blocked on something other than a model server**, which
is why parking it costs little. P38.1 needs permission to launch an unattended auto-approving agent;
P62.9 needs a *better task* rather than more runs of the current one; LLM-03, LLM-10, ARCH-04 and
P65.2 all need a session trace from a run whose data dir survives, which is the newly filed
**P68.1**. Only P62.8 is still purely waiting on hardware.

**One claim in the previous plan was half wrong, and the run is how that surfaced.** P66.14's record
says a live run "should no longer show prune thrash", because fixing LLM-02 deleted the band between
two disagreeing triggers. The band is indeed gone — but **a live run shows thrash anyway, from a
different cause**: at a 24,576-token window the base prompt plus `keepRecent`'s tail already fill the
window, so each compaction has only a two-message head to summarize, frees less than one turn adds,
and fires again on the very next turn. Eleven of fifteen turns, on both models, with prefill stepping
up 4x at the first compaction and never recovering. `TestSharedTriggerLeavesNoPruneThrashBand` is
still correct about what it tests; it just does not cover the low-yield regime. **P62.7's
minimum-yield rule suppressed none of the eleven**, which is the specific thing **P67.6** has to fix
and the reason that item's value estimate should go up rather than down.

**Tier 3 has no forced order left either.** Both of its sequencing constraints were discharged by the
2026-08-17 sitting: **P67.3** shipped the purpose-tag seam **P67.6** needs, and **P67.4** settled the
cancellation policy **P67.7** would otherwise have had to settle mid-refactor. What remains is ranked
by size-to-value alone, so any of the six can be taken against whatever file is already open.

**P66.26** (PERF-02) is the one refiled sub-item still open, and it stays Tier 4: a Low-severity
durability trade on the one database that holds checkpoints, the cost ledger and traces, with P66.9
having already removed most of the pressure behind it. (Its sibling **P66.25** shipped 2026-08-17.)

No Tier 4 build item currently has a fired trigger (re-verified 2026-08-15: `sandbox.backend` still
defaults to `"local"`, `lsp.Manager` is still one shared daemon singleton, both TUI asymmetries in
P63.10 are still present as described) — see each entry's **Promote when** for what would change that.

**Method notes worth re-reading before filing or building anything new** (full detail in
releases.md's pass history): before measuring an optimization, check the instrument the rest of the
system is running on — this document has twice recorded a fixed instrument *inverting* an
already-acted-on verdict, and P66.14 is now a third case, where fixing the threshold deleted the
phenomenon a shipped heuristic was built to rate-limit. Every documented live-tier command needs
`-count=1`, or a re-run silently replays Go's cached verdict instead of reproducing. Mutation-test any
new numeric threshold — a short fixture cannot tell adjacent thresholds apart, and a count assertion
cannot tell *when* something fired. And **read the refutation records in releases.md before filing
anything** against `internal/provider`, `internal/ollamainfo`, `internal/repomap`, or scanner method
resolution — several obvious-looking gaps there have already been checked and answered.

---

## Tiering Criteria

Applies to **build work** (Tier 1-4) only — items requiring new code. Items whose code is already
written and are only waiting on a live-run result belong in [Verification Work](#verification-work)
instead, regardless of how large or urgent the underlying question is.

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Up next — what is left, and it is short

**Rewritten 2026-08-18 (third time that day), after its only build row shipped** — **P70.4**, the
last item standing, filed that morning out of building P70.2 and closed that evening with *both*
halves rather than the cap alone. Build record in
[releases.md](releases.md#both-halves-of-the-sub-agent-boundary-2026-08-18-p704).

**All three build tiers are now empty.** Tier 1, Tier 2's only entry (**P68.1**, parked with row #1
below) and Tier 3 hold nothing that is both open and unparked. Four items shipped on 2026-08-18 —
P70.1, P70.2, P70.3, P70.4 — and every one of them was filed the same day it closed.

**The pattern the last three tables observed is now the whole story: the constraint was never
effort, it was decisions.** Four posture questions were put to the user on 2026-08-18 and all four
came back the same day. Three of the answers are the tree's stated reading and point in two
directions on purpose — the mailbox (P70.2) and a sub-agent's result (P70.4) **are** wrapped because
their content crossed a boundary before being relayed; `security_scan`'s workspace-derived output
(P70.3) is **not**, because a file the model can already read is not a crossing. Settle the next such
question against those three, not afresh.

| # | Item | Tier / size | Why now |
|---|------|-------------|---------|
| 1 | **The live-tier remainder** (P66.22, P38.1, P62.9, P65.2) — *parked by choice, 2026-08-16* | Verification | Unchanged, and still last for the same reason: **the user parked it**, not a dependency. It is also no longer one sitting — **P38.1** needs permission to launch an unattended auto-approving agent, **P62.9** needs a *better task* rather than more runs of the current one, and **P65.2**, **LLM-03**, **LLM-10** and **ARCH-04** need what the tier cannot show: a surviving data dir and `aegis sessions trace <id>`, which is **P68.1**. Take P68.1 first whenever this row is picked back up, or the sitting produces the same unreadable evidence again. Record in [releases.md](releases.md). |

**Notes on the ordering, and on what did not make it.**

**This table is one row because the build backlog is empty, not because it was trimmed.** Tier 1,
Tier 2 and Tier 3 hold nothing open and unparked, and everything else is Tier 4 with no fired
trigger — which this document's own rule says not to build speculatively. The previous two tables
each promoted one Tier 4 entry on a stated reason (P70.3's bound half, then nothing); nothing in
Tier 4 currently has one.

**There is no sequenced work left to rank.** The one row depends on nothing in the codebase except
P68.1, and on a reachable model server.

**One item is deliberately off this list: P68.1** (Tier 2, S). It is what the parked row needs before
it is worth re-running — the eval tier deletes the database holding the trace its own closure
conditions are written against. It travels with row #1, so it is off the list while that row is
parked.

**What to do when this table is picked up next.** The honest reading is that the next build item does
not exist yet and has to be *found* rather than selected: either the user unparks the live tier
(taking P68.1 first), or a fresh audit files new work the way P66.15's sweep filed P70.1–P70.3.
Promoting a Tier 4 entry without a fired trigger is the thing this document tells you not to do.

**This table outranks `scripts/roadmap-status.sh`.** That script reports open items in *document*
order, which is priority order only *within* a track — it cannot see a cross-tier ranking — and it
also cannot see that row #2 is parked by choice or that **P68.1** is deliberately off the list. Use
it for repo state and for the parse; use this table for what to take.

---

## Open Work — Tier 1

**Status: empty. P69.6 shipped 2026-08-17**, the same day it was filed — see [Nothing planned a
resident set](releases.md#nothing-planned-a-resident-set-2026-08-17-p696). Before it the tier had
been empty since **P66.5** shipped (2026-08-16), closing the last of the findings the review
classified as exploitable on the day it landed: P66.2 (2026-08-15), then P66.1, P66.4, P66.3, P66.6
and P66.5. See [releases.md](releases.md) for what each landed and what was found while landing it —
several of those records correct the item they were built from, which is the part worth reading
before trusting [CodeReview.md](CodeReview.md) directly.

An item enters this tier when it is a real, currently-exploitable security or robustness gap that is
small and has no dependency.

<details>
<summary>P69.6 — Nothing plans a resident set, so every seat is sized as if it were alone (shipped 2026-08-17)</summary>

### P69.6 — Nothing plans a resident set, so every seat is sized as if it were alone — SHIPPED 2026-08-17

**Filed 2026-08-17, from building P69.5** (`aegis models --fit`, see [releases.md](releases.md)) and
from measuring the debate topology in
[debate-topology-plan.md](debate-topology-plan.md). P69.5 fixed the arithmetic for **one model in
isolation**; this is the half it deliberately left open, and the half the debate feature actually
needs.

**The gap.** `ollamainfo.RecommendContextWindow(modelMax int) int` is the only sizing decision in the
tree, and its signature is the bug: one model in, one number out. It cannot express "these three
models must be resident at once", because it never learns that a second model exists. Since **P69.1**
each debate seat resolves its own model, so a single debate can hold two or three models in VRAM
simultaneously — and every one of them was sized as though it owned the whole card.

**It is currently wrong, not theoretically wrong.** Measured on a 16 GB card with
`aegis-qwen35-9b` (training context 262144):

| Path | Window | KV cache | Total with weights |
|------|--------|----------|--------------------|
| `RecommendContextWindow` (what `--first-init` writes) | 131072 | 16.50 GiB | 20.50 GiB |
| `aegis models --fit --budget-gb 10.5` (P69.5) | 51200 | 6.45 GiB | 10.44 GiB |
| Hand-fitted for two resident seats | 16000 | 2.01 GiB | 6.01 GiB |

The top row does not fit the card at all. **`BaselineContextWindow = 32768` is the sharper problem**:
it is a *floor*, so the function cannot return a window *below* it no matter what model it is asked
about, or how many others must sit beside it. At the geometry measured above (135,168 bytes/token at
f16) 32768 tokens is 4.13 GiB of KV, so the floored model's whole footprint is 8.13 GiB — which does
leave room for the 5.08 GiB arbiter on a 14.5 GiB usable budget, with ~1.3 GiB spare. So the floor is
*marginal* for exactly two seats rather than impossible, and it binds outright for three seats, for a
larger arbiter, or once the KV cache is filled to the window rather than measured at low occupancy.
Either way the mechanism is the same and the user-visible result is the same: `--first-init` writes a
number chosen without knowing a second model exists.

<sub>Corrected 2026-08-17 while implementing. The original text read the 8.12 GiB as KV alone and
concluded the floor "cannot express a co-resident configuration on 16 GB at all", which was too
strong — 8.12 GiB is weights plus KV. The correction narrows the claim; it does not weaken the item,
since a 1.3 GiB margin is not something to arrive at by accident, and it changes what the regression
test should assert.</sub>

**What the design has to settle**, none of which P69.5 answers:

- **Where the budget comes from.** P69.5 makes it an operator-supplied `--budget-gb` precisely to
  avoid this question. `internal/hwinfo` forbids VRAM detection outright ("on any platform, ever",
  P17.5) and that reasoning still holds for driver queries — but `/api/ps`'s `size`/`size_vram`
  split is Ollama's own accounting of its own placement, which is the signal P17.5 said did not
  exist. Whether that is enough to derive a budget, or whether a `provider.vram_budget_gb` config
  key is the honest answer, is the first decision.
- **Who owns the split.** A resident set is a property of a *workload* (a debate's three seats, a
  swarm's fan-out), not of a model or of the daemon. Nothing in the tree currently represents one.
- **Whether seats are co-resident or swapped.** Topology 2 in the plan trades residency for
  sequential loading via `keep_alive: 0`, which needs `provider.Request.KeepAlive` — it does not
  exist (`WithNumCtx` is the pattern to mirror). That is the alternative to shrinking every window,
  and the choice between them is a design decision, not a tuning one.
- **What happens on a machine that cannot fit the set at any window.** Refusing is right; refusing
  *at daemon start* rather than mid-debate is the part that needs designing.

**Do not fix this by lowering `BaselineContextWindow`.** The floor is doing real work for the
single-model case it was written for (P35.3: a skill-driven run builds a >40k-token prompt before
writing output, and a smaller floor makes the first real task fail with no compaction attempted).
The fix is a sizing path that knows how many models must coexist, with the existing per-model
recommendation kept for the case where the answer is one.

**Reuse, do not rebuild:** `ollamainfo.KVGeometry`/`BytesPerToken`/`Fit`/`WeightsBytes` (P69.5)
already give exact per-model KV arithmetic, validated against measurement to 0.2%, plus the
`Footprint.FullyOnGPU` empirical check. The measurement harness is
`research/scripts/vram_topology_probe.py`. What is missing is only the set-level planner on top.

**Closure condition:** `aegis --first-init` on a 16 GB card, followed by a debate with a distinct
arbiter model, produces a config where every seat is 100% on GPU per `/api/ps` — with no hand
editing, and with a stated refusal when no such assignment exists.

**Status 2026-08-17: SHIPPED.** All seven steps applied; the closure condition is met. The record,
including two corrections this made to its own source documents, is in
[releases.md](releases.md#nothing-planned-a-resident-set-2026-08-17-p696). The two open design
questions were decided —
the budget is an explicit `provider.vram_budget_gb` key (no detection, per P17.5), and the debate
builds a plan and installs it as a scoped override on the daemon's per-model context-window cache for
its duration, rather than introducing a "workload" abstraction. All of
[p69.6-resident-set-plan.md](p69.6-resident-set-plan.md) is applied, and everything before step 5 is
behavior-neutral without a configured budget:

- `ollamainfo.PlanResidentSet`/`PlanFor` — the set planner, equal-token split with a training-maximum
  clamp, deduplicating by model name because Ollama holds one runner per name.
- `provider.vram_budget_gb` and `provider.kv_cache_type`, both inert at their defaults.
- `aegis models --fit-set a,b,c` and `--fit-debate`, so the plan is observable before it is enforced.
- `Server.claimResidentSet` — the scoped override, plus a daemon-start warning.
- Step 5: all four debate entry points wired — `POST /debate`, the TUI, the `agent` tool
  (`builtin.WithResidentSetClaim`), and headless `aegis debate` via `provider.WithNumCtx`.
- Step 6: `--first-init` asks for the budget and sizes `context_window` from `Fit`; blank is a
  first-class answer that writes a byte-identical config to before.
- Step 7: docs in [configuration.md](../docs/configuration.md), [cli-reference.md](../docs/cli-reference.md),
  [debate.md](../docs/debate.md) and [installation.md](../docs/installation.md).

Priority: Tier 1 — M. No dependency; P69.5 shipped the arithmetic it builds on.

</details>

---

## Open Work — Tier 2

**Status: 1 open** — **P68.1**, and it is deliberately off the ranked list because it travels with
the parked live-tier row. **P66.25, P67.2, P67.3, P67.4 and P67.5 shipped 2026-08-17**, emptying the
rest of the tier; P66.11, P66.12, P66.21 and P67.1 shipped 2026-08-16, and P66.7, P66.9, P66.10 and
P66.16 earlier that day. Records for all of them are in [releases.md](releases.md).

**P66.25 was the last P66 item and the last security item in the open set.** What that leaves in this
tier is one instrumentation gap whose whole value is making the *next* live sitting readable — so a
new Tier 2 entry now comes from a review pass or a fired trigger, not from what is already filed.

### P68.1 — The live tier can run a measurement it cannot read back

**Filed 2026-08-16, from running the live tier rather than from reading it.** Four open verification
conditions — **LLM-03**, **LLM-10**, **ARCH-04** and **P65.2**'s prompt half — were all scheduled
against `TestLiveWorkflow`, and none of them is observable there. The tier reports what came over the
SSE stream; every one of those four is a fact about what the *engine* decided, which lives in
`TurnTrace` and the session store.

Two concrete gaps, both small:

- **The evidence is deleted at the end of the run.** `newLiveWorkflowDaemonTweaked` builds each
  daemon over a throwaway `os.MkdirTemp` data dir and removes it on cleanup, so `sessions.db` — and
  with it the P66.11 turn trace that *is* LLM-02's and ARCH-04's closure condition restated as a
  struct — goes with it. Every run this sitting made is unreadable after the fact. The fix is an
  env-gated keep (`AEGIS_EVAL_KEEP_DATA_DIR`) plus the session id in the log line, so a run can be
  followed with `aegis sessions trace <id>`.
- **Some of it never leaves the engine at all.** A compaction emits a notice saying it happened and
  how many messages it folded; the *summary text* is never an event. So P65.2 — does a local model
  fill the fixed skeleton without losing what terse bullets kept — cannot be judged from a live run
  no matter how many times compaction fires. Same for a tool error's text, which the tier logs only
  as a character count, and which is exactly what P62.9 wanted to read when `edit_section` failed.

**Do not fix the second one by widening the SSE stream.** A summary and a tool-error body are
per-turn engine state, and the stream is what a UI renders; the trace is already the right home and
already has a reader. What is missing is that the trace records the compaction *event* but not the
text it produced, and that the eval tier throws away the database holding it.

**Closure condition:** a `TestLiveWorkflow` run whose log names a session id that survives the test,
and `aegis sessions trace <id>` on it showing the compaction summary text, the calibration sample
count and each turn's stop reason. That single change is what unblocks four verification items;
until it exists, re-running the tier produces the same evidence it produced today.

Priority: Tier 2 — S. No dependency. **Deliberately off the "Up next" list**, because it travels with
row #6 and that row is parked: its whole value is making the next live sitting readable. If that
sitting is ever scheduled, this comes first — a tier that cannot be read back costs the same and
yields less.

---

## Open Work — Tier 3

**Status: empty. P70.4 shipped 2026-08-18**, the day it was filed — see [Both halves of the
sub-agent boundary](releases.md#both-halves-of-the-sub-agent-boundary-2026-08-18-p704). The tier held
two for a few hours that morning: **P70.1** and **P70.2** were filed out of P66.15's sweep and both
shipped the same afternoon; P70.4 was the item their build produced, and it closed the same day. The tier held five
until 2026-08-18, when **P66.15**, **P67.6**, **P67.7**, **P67.8** and
**P67.9** all shipped: the entire "Up next" table below row #5, in one sitting. Their build records —
including the three places the items were wrong about their own preconditions — are in
[releases.md](releases.md) (*Five rows of Up next, 2026-08-18*). Read the P67.7 record before
touching `internal/engine`: it found two pre-round gates the item never named, and the resolution it
chose is a *restriction on when the feature is active*, not a widening of those gates.

Also read **P66.13**'s record (shipped 2026-08-17) before adding a permission layer or a run bound
anywhere: both now live in `internal/enginecfg` and are built once rather than per entry point. And
**P66.14**'s (2026-08-16) before touching the compaction path, because the shared trigger it
introduced changed which numbers two already-shipped heuristics see. P62.9 and P65.2's prompt half
both moved to [Verification Work](#verification-work) — in each case the code is already shipped and
what remains is a live-run result, not a design or implementation task.

<details>
<summary>P70.4 — the sub-agent result boundary (shipped 2026-08-18)</summary>

### P70.4 — A sub-agent's result reaches its parent bare and uncapped — SHIPPED 2026-08-18

**Shipped 2026-08-18**, the day it was filed, and **both halves were taken together**. The item
predicted a split — cap now, wrap when there was appetite for the posture question — and the user
answered the posture question immediately: **wrap it, zero trust**. Commissioning a sub-agent's work
does not vouch for what that work read, so a parent consuming its child's report is in the same
position as one reading a teammate's relayed prose after all. The counter-argument the item was filed
with is recorded as considered and rejected in
[docs/mcp-trust-boundary.md](../docs/mcp-trust-boundary.md). Record:
[releases.md](releases.md#both-halves-of-the-sub-agent-boundary-2026-08-18-p704).

**Filed 2026-08-18 from building [P70.2](#p702--the-swarm-mailbox-is-an-unwrapped-cross-agent-injection-channel)**,
which wrapped the mailbox and, in sweeping for other model-facing reads of it, found the channel next
to it. Verified at `internal/swarm/subprocess.go:223-229`.

`SubprocessBackend.runWorker` scans the worker's mailbox back for the last `MsgResult` and assigns
`msgs[i].Text` into `Result.Output`. That value reaches the parent model bare — through `agent.go`
(`res.Output`) and through `task_output`. The in-process backend arrives at the same place *without*
the mailbox at all (`inprocess.go:82`, `Result{Output: output}` straight from `runGuarded`), which is
the point: **this is the sub-agent result path, not the mailbox channel.** The mailbox is only its
durability substrate under one of the two backends, so P70.2's wrap does not and should not cover it.

It is the same laundering shape P70.2 closed — a sub-agent that read poisoned web or MCP content
relays it upward as trusted-looking text — and it is *also* uncapped, with no `truncate.go` posture
entry.

**The reason it is a separate item is scope, not doubt about the finding.** P70.2's zero-trust answer
points at wrapping this too, but the blast radius is different in kind: it changes the shape of every
`agent` and `task_output` result and every workflow mode's joined output, and a parent that is
*designed* to consume its child's work is not obviously in the same position as one reading a
teammate's relayed prose. The size cap carries no such question and can be taken alone, exactly as
P70.3's bound half was.

Priority: Tier 3 — S-M. No dependency. The cap is small and unblocked; the wrap needs the same kind
of deliberate answer P70.2 got.

</details>

<details>
<summary>P70.1 — the restore boundary (shipped 2026-08-18)</summary>

### P70.1 — `checkpoint.RestoreFiles` writes anywhere the database says to — SHIPPED 2026-08-18

**Shipped 2026-08-18**, the day it was filed. Both decisions the item left open were answered by the
user: the workspace root is **recorded per checkpoint** (`checkpoints.workspace_root`, an idempotent
`ALTER` following the `git_sha` precedent) rather than threaded into the server-wide `Store`, and a
path that fails validation **refuses the whole restore** before anything is written. Legacy rows with
no recorded root fail closed. Record: [releases.md](releases.md#three-rows-and-a-posture-2026-08-18-p701-p702-p703).

**Filed 2026-08-18 from P66.15's sweep**, which verified it at
`internal/checkpoint/checkpoint.go:201` and deliberately left it unfixed because the fix is a
signature change rather than a line.

`/rewind` restores a turn by replaying BLOB rows: `os.WriteFile(fs.Path, …)` and `os.Remove(fs.Path)`
for every file the checkpoint captured. There is **no path validation of any kind**, because the
`Store` has no notion of a workspace root — it is a database that happens to hold absolute paths.

Session ownership *is* checked, at the handler (`internal/server/sessions.go:565`), and every current
capture site resolves inside the workspace, so this is defence in depth rather than a live hole. What
makes it worth filing is that the sweep found the *one* capture site that did not: P66.15 fixed
`captureShellWrites`, which was joining `git status --porcelain` paths (repo-relative) onto the
workspace root and could record a pre-image against a path outside it. Restore is the layer that
should not depend on every present and future capture site being correct, and it currently does.

Two things to settle, which is why this is not a one-liner:

- **Where the root comes from.** Threading it into `checkpoint.Store` makes every checkpoint
  workspace-bound, which is probably right and is a schema-adjacent decision; passing it to
  `RestoreFiles` keeps the store dumb but puts the invariant back at the call site, which is what
  failed here.
- **What a rejected path does.** Skipping it silently makes `/rewind` quietly partial, which is the
  failure mode the whole feature exists to avoid; failing the restore wholesale is honest but turns a
  stale row into an unusable checkpoint.

Secondary, and worth fixing in the same pass: a file that existed before a turn, was deleted during
it, and is recreated by restore comes back at `0o644` — its original mode is not captured.

Priority: Tier 3 — M. No dependency. Security-adjacent: the fix is a boundary, so the test pass is
the deliverable as much as the code.

</details>

<details>
<summary>P70.2 — the mailbox trust posture (shipped 2026-08-18)</summary>

### P70.2 — The swarm mailbox is an unwrapped cross-agent injection channel — SHIPPED 2026-08-18

**Shipped 2026-08-18**, the day it was filed. The posture question *was* the item, and the user
answered it: **Aegis is built on zero-trust principles**, so the mailbox is a laundering channel and
its content is wrapped. That answer is now the tree's stated reading — see
[docs/mcp-trust-boundary.md](../docs/mcp-trust-boundary.md) — and it is what closes the wrap question
for [P70.3](#p703--scanner-output-reaches-the-model-unbounded-and-half-wrapped) too. Building it
found the *other* half of the same channel, filed as
[P70.4](#p704--a-sub-agents-result-reaches-its-parent-bare-and-uncapped). Record:
[releases.md](releases.md#three-rows-and-a-posture-2026-08-18-p701-p702-p703).

**Filed 2026-08-18 from P66.15's sweep**, verified at `internal/tool/builtin/team.go:250`
(`team_inbox`) against the file-backed queue in `internal/swarm/mailbox.go`.

`trust.Wrap` marks untrusted content for MCP results and web fetches. It does not cover the mailbox,
and nobody had checked: `team_inbox` formats `m.Text` into the tool result bare, with no wrap and no
size cap. The queue is a file under the shared data dir, writable by any peer agent and by any local
process with file access — so a teammate that ingested poisoned web or MCP content can relay it to a
peer as plain, trusted-looking text, laundering the provenance marking on the way.

**The reason it is filed rather than fixed is a posture question, not an effort one.** Wrapping
intra-harness agent traffic as untrusted changes the shape of every swarm result and every prompt
that reads one, and it asserts something about the trust model — that a sub-agent Aegis itself
spawned is a hostile source — that has never been written down. The two defensible readings are:

- **The mailbox is a laundering channel** and must be wrapped, because the content in it did not
  originate with the agent that sent it.
- **The mailbox is internal** and the wrap belongs at the *ingestion* points a sub-agent has (web,
  MCP), which are already covered — in which case the real gap is that a wrapped result can be
  unwrapped by being quoted into a message, and that is a different item.

The size cap is not a posture question and can be taken either way: `truncate.go`'s per-call caps are
not applied here.

Priority: Tier 3 — S-M once the posture is decided; the decision is the work.

</details>

<details>
<summary>P66.15 — the unread-package sweep (shipped 2026-08-18)</summary>

### P66.15 — Sweep the two packages this review did not read — SHIPPED 2026-08-18

`internal/tui` (16,163 non-test lines) and `internal/security` (8,435) are **26% of production Go**
and produced three findings between them, two of which were a struct-field count and a stale comment.
That is not evidence they are clean; it is evidence nobody read them. The one hour eventually spent
in `internal/tui/approval.go` during arbitration produced P66.6 — a Medium security finding at the
last line of the threat model.

Two specific sweeps, each with a stated reason to expect findings:

- **`internal/security`'s scanner-output parsers.** 8.4k lines that shell out to fifteen external
  tools and parse their SARIF/JSON/XML output back into model context. The review noticed the
  prompt-injection shape *for DAST* (SEC-06's 0777 work directory) and did not generalize it; nothing
  swept the parsers. `security.parseNmapXML` is already implicated in P66.2's CVE list.
- **`internal/tui`'s rendering and approval paths.** P66.6 fixes the ingestion point; this is the
  audit that establishes whether it is the only one.

Also unexamined as categories, and worth folding in: DoS against the daemon (session/checkpoint
growth, the 60s auth-lockout cap, unbounded `sessionSems`/`sessionPermCache` growth reachable by a
loopback caller creating sessions in a loop); the `internal/checkpoint` **restore** path, where
`/rewind` writes files back into the workspace from a BLOB and nobody asked whether it path-validates;
and the `internal/swarm` mailbox as a cross-agent injection channel, since `trust.Wrap` covers MCP and
web but nobody checked the mailbox.

Priority: Tier 3 — M. Not a defect; a known gap in coverage with a demonstrated hit rate.

**Four leads sit here unfiled, each with a stated promotion trigger.** None is a `### P<n>.<m>` item
yet, deliberately — filing one before its trigger fires would commit to a design question that has no
answer.

- **Whether Aegis should ever mount a container engine socket.** `dockle` is the only tool that wants
  one — it inspects an image through the local engine rather than pulling it — and socket access is
  effectively host root, a third privilege axis beyond the network/workspace split P55.7 is built on.
  dockle stays host-only and says so in code. **Promote when** someone actually needs container-only
  dockle.
- **Auto-engage the tool-calling shim off a low conformance rate.** The persisted P53.4 rate is already
  readable per model (`modelcaps.Store.ToolCalling`) and `provider.tool_call_shim` rejects `"auto"`
  rather than silently accepting it as a no-op, precisely so the word stays available for this.
  **Promote when** live runs show the rate predicting drive outcomes.
- **Grammar-constrained decoding for *tool calls*** (Ollama structured outputs, llama.cpp GBNF). P59.8
  took this lead at the one caller with no open design question (the schema guard's corrective retry)
  and deliberately did not widen it. Targets models that speak the tool-call protocol but truncate or
  malform arguments (the P35.2 failure class). Needs its own heading if pursued.
- **Code Mode — the model writes a program against a generated SDK instead of emitting tool calls.**
  The token argument is real and attacks the same 84.3%-of-base-prompt cost the P62.x line has been
  chipping at, but our own evidence points the other way for the primary target: P39.16 shipped
  handle-based editors because a 14B model failed `edit_file`'s byte-exact match 12 times running, and
  writing a correct *program* with control flow over those same tools is a harder generation task, not
  an easier one. **Promote when** a measured local run shows a model composing multi-step tool work
  reliably enough that round-trips are the binding cost, or a cloud/mechanical-fan-out target emerges.

**Four more leads, same reasoning — a real mechanism with no observed problem behind it here.**

- **Per-file mutation serialization keyed on `realpath`.** `engine.runTools` serializes *all*
  write/execute tools behind one `execLock`. A per-path queue would let unrelated-file edits run
  concurrently. **Promote when** a measured drive shows serialized writes as a real fraction of wall
  clock — the current coarse lock is correct, and trading it for a fine one on no measurement is how
  concurrency bugs are bought.
- **A wider hook surface.** `internal/hooks` exposes only `PreToolUse`/`PostToolUse`. Four gaps worth
  naming for vocabulary alone: `context` (mutate the message list before every provider call),
  `before_provider_request`/`after_provider_response`, and an `agent_end` vs `agent_settled` split that
  names a distinction the drive's reset ladder already implements without a word for it. Do **not**
  adopt an in-process JS extension runtime for this — it would hand arbitrary code the capabilities
  `internal/permission`/`internal/sandbox` exist to gate. **Promote when** something concrete needs one
  of these four points.
- **Full-text search over session history.** Aegis is already SQLite-backed, so FTS5 over
  `session_messages` is close to free and would back a `/search` the TUI does not have. Co-located FTS
  triggers are the trap — an index failure can roll back canonical writes. **Promote when** someone
  asks for cross-session search. **Second trap, added 2026-08-16 from the P67 reading:** the moment a
  transcript is indexed there are *two* texts for every tool result — what the model saw and what the
  user saw — and `truncate.go`'s spill notices and truncation banners are exactly where they diverge.
  Index the **rendered** text, not the model-facing serialization, and pin the two together with a
  test that flags both directions: text indexed but never rendered (a phantom hit, which is a bug) and
  text rendered but not indexed (an undercount, which is tolerable). Deciding this after the index
  exists means rebuilding it.
- **Pinned distribution for skills and personas** (`aegis skills install git:host/user/repo@ref`),
  gated on the existing `internal/workspacetrust` grant. **Promote when** there is a second party
  publishing Aegis skills — this is distribution, not capability, and worth nothing until someone is on
  the other end of it.

</details>

<details>
<summary>P67.6 — compaction on cache temperature (shipped 2026-08-18)</summary>

### P67.6 — Compaction fires on context pressure only, never on cache temperature — SHIPPED 2026-08-18

Aegis compacts when the conversation approaches the context window. That is the right trigger for the
problem it solves and the wrong trigger for a second problem it does not: a session resumed after a
long gap re-processes a prefix the backend has already evicted, paying full prefill on stale tool
results it is going to summarize away later anyway. The observation this item rests on is a
scheduling one — **when the cache is already cold, clearing old tool results is free**, because the
usual reason not to (you would invalidate the cache) has already happened.

Add a second, orthogonal trigger: if the elapsed time since the last assistant message exceeds a
threshold, replace all but the most recent N compactable tool results with a fixed sentinel string
*before* the request goes out. Only tool results, only the compactable kinds (reads, searches,
shell), never the model's own text.

Three constraints, each of which is a bug if missed:

- **Floor the keep-count at 1.** Clearing every result leaves the model with no working context at
  all, and an off-by-one that keeps zero is easy to write and hard to see.
- **Gate on call purpose, not on "is this the main loop".** Analysis-only callers must be able to
  inspect a conversation without mutating it as a side effect. This is what **P67.3** builds, and why
  this item is sequenced behind it.
- **The sentinel is a wire format.** Once written into the conversation it is read back by the same
  code on later turns; renaming it silently breaks accumulation, the same way the
  `<read-files>`/`<modified-files>` tags do in the compaction summary.

**Sequence after P66.14 and read together with it.** P66.14 reconciles the two existing compaction
thresholds that disagree; adding a third path into that machinery before those two agree would design
the new trigger against a broken baseline. This is also the item where the Ollama prefill behavior
already recorded in this project matters: because `prompt_eval_count` reports the *full* prompt on a
cache hit, the cost this trigger avoids is measurable rather than assumed.

**The live tier has now shown the state this item is really about (2026-08-16), on two different
local models.** Under a forced read-chain at a 24,576-token window, compaction fired on **eleven of
fifteen turns**, each time
summarizing two messages and leaving the conversation at ~90% full so the next turn re-crossed the
trigger immediately; prefill went from ~4.5s to ~18s at the first compaction and never came back.
P62.7's minimum-yield rule suppressed none of them. Whatever this item gates compaction on, it has to
be able to say "not again this turn" in that state — the measurement is in
[releases.md](releases.md) (*The live-tier sitting, 2026-08-16*).

Priority: Tier 3 — M. **Both prerequisites have shipped**: P67.3 (2026-08-17) built the purpose tag
this item gates on, and P66.14 (2026-08-16) made the compaction trigger one shared function. No
remaining dependency.

</details>

<details>
<summary>P67.7 — streaming tool dispatch (shipped 2026-08-18)</summary>

### P67.7 — Tool calls are dispatched only after the whole model turn has streamed — SHIPPED 2026-08-18

`Engine.turn` returns its `toolUses` slice after the stream drains
(`internal/engine/engine.go:773`), and `runTools` is called on the finished slice
(`internal/engine/engine.go:1024`). So the first tool call in a five-call round waits for the fifth to
finish generating before it starts, even though it was fully specified long before.

The scheduler itself is not the problem and should not change — per-call `tool.EffectiveCapability`,
the read/write gating, the `waitFor` dependency graph and the same-path ordering are all correct, and
are a better design than the contiguous-run partitioning the comparison source uses. What changes is
where the scheduler's input comes from: feed it each tool call as that call's block completes in the
stream, rather than handing it a complete slice.

This is the largest engine change in either open batch and the one with the most invariants pointed at
it, so the constraints are the substance of the item:

- **Result order must stay deterministic.** Execution order already is not; the wire order of results
  must remain receipt order regardless of completion order, or every replay and eval fixture moves.
- **`Engine.startedTools` must be recorded at dispatch**, not at collection, or `repairOrphanedToolUses`
  loses the distinction P65.1 exists to preserve — a call that started and may have landed its effects
  versus one that never ran.
- **A cancelled or failed stream must abandon in-flight calls**, and their results must not be
  appended to a turn whose assistant message never completed.
- **The stall bound covers the whole turn.** Starting tools earlier lengthens the window in which a
  tool is running concurrently with generation, so the heartbeat chaining
  (`internal/heartbeat`, a sub-agent's watch must never shadow its parent's) needs re-checking against
  the new overlap, not assumed to carry over.

Take **P67.4** first if both are wanted — sibling cancellation is much simpler to reason about on the
current batch-dispatch model, and the policy it settles is one fewer thing to get right here.

Priority: Tier 3 — L. **P67.4 shipped 2026-08-17**, so the cancellation policy this refactor would
otherwise have settled mid-flight is already settled; inherit it (round context derived from the
turn's, write/execute failures only, every result slot filled) rather than re-deciding it. The largest
payoff of the batch on local models, where generation latency dominates.

</details>

<details>
<summary>P67.8 — per-command shell classification (shipped 2026-08-18)</summary>

### P67.8 — Read-only shell classification is per-binary, so useful commands stay execute-gated — SHIPPED 2026-08-18

`internal/tool/builtin/shell_readonly.go` allowlists whole binaries and rejects the command outright
if any shell metacharacter appears. The conservatism is deliberate and correct as a default — a false
positive here auto-approves a mutation under plan mode — but the file's own comments show where it
runs out: `sort`, `tree` and `uniq` are excluded because each has a file-*writing* form, with the
reasoning "no argument parsing makes them read-only." Argument parsing is precisely what is missing.

Replace the binary allowlist with a **per-command configuration**: a table of permitted flags, each
with an argument type (none / string / number), plus an optional regex and an optional predicate for
cases flags cannot express, and a per-command switch for whether the tool honors POSIX `--`.
Unlisted flags fail closed. That admits `sort` without `-o`, `tree` without `-o`, `uniq` with at most
one positional, and opens up `rg`, `fd`, and read-only `git`/`gh` subcommands with their flags rather
than only their bare forms.

Three things this item must not lose:

- **`argvStaysInRoot` still applies.** Flag-level parsing decides whether a command *can* be
  read-only; path confinement decides whether this invocation is. Both, not either. Attached flag
  values are path operands too — that is what VULN-02 rode in on.
- **The exclusions that are not about writing stay excluded.** `env`/`printenv` dump the daemon's
  process environment and therefore the provider keys; `ps` reaches the same data by another route;
  `less`/`more` shell out. None of these become admissible under flag parsing, and the reasoning
  comments explaining why must survive the rewrite — they are the part most likely to be lost and
  most expensive to rediscover.
- **Some flags are unsafe for reasons the flag name does not suggest.** The instructive example from
  the comparison source: a directory-lister's `--list-details` flag internally execs `ls`, making it a
  PATH-hijack surface. Every added flag needs the question asked, not just "does this write."

Widening this classification widens two things simultaneously — what runs in parallel, and what plan
mode approves without asking. That is why this is Tier 3 with its own test pass rather than a Tier 2
afternoon.

Priority: Tier 3 — M-L. No dependency. Security-sensitive: the test pass is the item, not an
afterthought.

</details>

<details>
<summary>P67.9 — terminal capability is asked, not guessed (shipped 2026-08-18)</summary>

### P67.9 — Terminal capability is inferred from `TERM`, not asked — SHIPPED 2026-08-18

`internal/tui/imagerender.go:89` decides whether the terminal speaks the kitty graphics protocol by
checking whether `TERM` contains `"kitty"`. The file is honest about the consequence: the kitty tier
is opt-in only, and `aegis doctor` can report that it is *plausible*, not that it works.

Terminals will answer the question directly. The obstacle is that a terminal which does not support a
query simply stays silent, so the naive implementation needs a timeout — either too short to be
reliable or too long to sit in startup. The trick that removes it: terminate each batch of queries
with a **DA1 request (`CSI c`)**, which every terminal since the VT100 answers, and rely on terminals
replying in order. If the answer to your query arrives before the DA1 reply, the feature exists; if
DA1 comes first, it does not. No timeout anywhere, and one round-trip for the whole batch.

Worth asking about in the same batch: kitty graphics, synchronized output (DECSET 2026), and true
color. The payoff is an actual answer instead of a guess, `aegis doctor` reporting supported rather
than plausible, and the kitty tier becoming defensible as an auto-selected tier rather than
permanently opt-in.

The awkward part is Bubbletea, not the protocol: responses arrive on the same input channel as
keystrokes and must be recognized and routed before they reach the key handler, or a capability reply
is delivered to the UI as garbage keys. `internal/termsafe` already has the sequence-parsing
vocabulary this needs. Do the probe once at startup and cache it; do not re-query per frame.

Priority: Tier 3 — M. No dependency.

---

</details>

## Open Work — Tier 4

**Status: 21 open** — 9 pre-existing (all blocked or explicitly parked, none with a fired trigger),
6 from the P66 review batch, 5 from the P67 external-source reading and **P70.3**, filed 2026-08-18
out of P66.15's sweep. (This line read "13 open …
plus 4 from P66" until 2026-08-16; it had not been updated when P66.23 was filed, and the Status
block above was the correct count. It moved to 20 later that day when **P66.26** was refiled out of
P66.9.)

The P66 entries here are **deliberately grouped grab-bags**: each collects the Low-severity residue of
one review domain. They are filed so no finding is lost, not because any of them should be scheduled.
Take one only when already working in that file. The P67 entries are a different kind of parked: each
is a capability Aegis does not have and nobody has asked for, filed with the specific trigger that
would make it worth building.

<details>
<summary>P70.3 — the scanner-output bound, and the wrap declined (shipped 2026-08-18)</summary>

### P70.3 — Scanner output reaches the model unbounded and half-wrapped — SHIPPED 2026-08-18

**Shipped 2026-08-18**, the day it was filed, and it closes on *both* halves rather than one:

- **The bound half was built.** `runJSON` and `runContainerCLI` no longer use `cmd.Output()`; both
  read through a bounded writer capped at 64 MiB, and an overflow **refuses to parse** rather than
  handing a parser a truncated SARIF/JSON document.
- **The wrap half was declined, deliberately, by the user.** `security_scan`'s content is
  workspace-derived — files the model can already read directly — so wrapping it would mark as
  untrusted the one class of input the agent is reading on purpose. This is the answer the item asked
  for "once for the whole tree rather than tool by tool", and it is *not* in tension with
  [P70.2](#p702--the-swarm-mailbox-is-an-unwrapped-cross-agent-injection-channel)'s zero-trust
  answer: the mailbox launders content that crossed a boundary, a workspace file did not.

Record: [releases.md](releases.md#three-rows-and-a-posture-2026-08-18-p701-p702-p703).

**Filed 2026-08-18 from P66.15's sweep.** The sweep fixed the two clear cases —
`recon_scan` and `dast_scan` now wrap their reports, because nmap's `product`/`version` fields are a
banner a *remote host* chose to print and ZAP's findings quote the target's responses. Two smaller
ones were left as deliberate decisions rather than incidental ones:

- **`security_scan` (`internal/tool/builtin/security.go:63`) returns the same unwrapped shape**, but
  its content is workspace-derived — files the model can already read — plus third-party rule-pack
  text. Wrapping it is cheap; whether a workspace's own files count as untrusted input to the agent
  that is reading them anyway is the question, and it should be answered once for the whole tree
  rather than tool by tool.
- **No per-call bound.** `security.runJSON` (`internal/security/scanners.go:56`) and
  `runContainerCLI` (`method.go:1090`) both use `cmd.Output()`, so a rogue or compromised scanner's
  stdout is read entirely into memory before parsing, and none of the scan tools apply a per-call cap
  from `truncate.go`'s posture table. They rely solely on `CapRound`, which is the *aggregate* bound
  and was never meant to be the only one.

The bound half is a straightforward fix and could be taken alone. **Promote when** someone takes
either, or when a scanner is observed producing an output large enough to matter.

Priority: Tier 4 — S. Low severity, and the wrap half is a posture decision shared with
[P70.2](#p702--the-swarm-mailbox-is-an-unwrapped-cross-agent-injection-channel).

</details>

### P66.26 — `synchronous=NORMAL` on the three SQLite databases (PERF-02, refiled from P66.9)

**Filed 2026-08-16**, carved out of P66.9 so a deliberately-skipped sub-item is visible rather than
buried in a closed entry. Every SQLite database runs at the default `synchronous=FULL`, paying an
fsync per transaction.

The item splits cleanly, and only one half is contentious. **`knowledge.db` and `longmem.db` are
unconditionally safe**: both are derived stores, rebuildable from their sources, and losing the tail
of a write on power loss costs a re-index. Neither was in P66.9's reach — they live in
`internal/knowledge` and `internal/memory`, not `internal/session` — which is the only reason they
did not ship with it. **`sessions.db` is the contentious half**, and is why the debate downgraded
PERF-02 to Low: it holds checkpoints (`/rewind`), the cost ledger and traces, so `NORMAL` trades a
durability guarantee on the one database whose loss is not recoverable from elsewhere. Do that half
only with the trade written down at the DSN, or not at all.

Note that P66.9 already removed most of the pressure that motivated this: delta coalescing cut the
`bg_events` insert rate by roughly the coalescing factor, and that table was the source of the
fsync-per-token pattern the finding was reacting to. Re-measure before building — this document has
twice recorded a fixed instrument inverting an already-acted-on verdict.

**Promote when** a measurement on the *current* tree (post-P66.9) shows fsync cost still material on
the local path, or when `knowledge.db` re-indexing becomes a noticed cost on its own.

Closes PERF-02. Priority: Tier 4 — S. No dependency.

### P66.17 — Local-model path: the Low-severity residue

Eleven findings from the local-runner review, none individually worth a trip. `tokenest.Message`
ignores `ImageBlock` and `ThinkingBlock`, so images and thinking history are free in every estimate
(LLM-07). The Anthropic adapter's mid-stream errors are unclassifiable and therefore never retryable,
and its tool-call JSON is emitted unvalidated (LLM-08). The P59.5 local-backend carve-out reached the
output guard but not compaction or titles, though `routing.go:13` names all three sites (LLM-06). The
tool-call probe loads the model at the wrong `num_ctx`, forcing a reload on the first real turn
(LLM-10). Failover switches models without re-resolving the context window (LLM-11).
`ollamainfo.Detect` makes an unconditional, always-wasted `/api/show` round-trip (LLM-12).
`fitTranscript` re-renders and re-tokenizes the whole prefix up to O(n) times (LLM-13). A
misconfigured `summary_tokens` silently disables the summarizer's fit check (LLM-14). The carried file
record parses `<read-files>` tags out of *assistant* text (LLM-15). The SSE idle watchdog counts
consumer backpressure as a stalled runner (LLM-17). `reapSpills` scans the whole spill directory on
every spill (LLM-18).

**Promote when:** one of them is implicated in a live-run failure, or you are already in the file.
LLM-06 and LLM-10 are the two most likely to matter on a 16GB-VRAM machine, since both cause an
avoidable model reload.

Priority: Tier 4 — real, individually cheap, no trigger. Do not schedule.

### P66.23 — Go-code security residue

Six line-level findings the debate left standing but small. Grouped so none is lost; none is
scheduled.

`latex_build` runs an arbitrary binary because its `compiler` enum is never enforced — and the
general fact behind it is worth more than the instance: **nothing in this module validates tool input
against `InputSchema()`, so every enum in every builtin is advisory** (VULN-04, downgraded to Low by
arbitration because `latexBuildTool.Capability()` is already `CapExecute`, so no boundary is crossed —
but a future `CapRead` tool with an enum would be a different story). The DAST work directory is
chmod'ed 0777 in a shared temp dir, letting a local user plant the SARIF that becomes both the
operator's report and model context (VULN-06, POSIX-only, needs a hostile local user racing a scan
window). `expandFileMentions` confines lexically only, so a workspace symlink reads outside the root —
bypassing the symlink check every other read path gets (VULN-07, reachability caveat: only the
planted-symlink variant is confirmed). Windows reserved device names and ADS (`file.txt:stream`) are
not rejected by path validation (VULN-08, read but never executed). Five walk callbacks read whole
files unbounded (VULN-09). Hook stderr is captured unbounded and returned to the model (VULN-10).

**Promote when:** VULN-04's *general* form — schema validation for tool input — is worth its own item
if a read- or network-capability tool ever grows an enum that gates a path or a binary. The rest are
opportunistic.

Priority: Tier 4 — all Low, all confirmed, none with a fired trigger.

### P66.18 — Architecture, quality and maintainability residue

A mid-stream `EventError` discards the whole assistant turn **including text already streamed to the
user**, so the transcript loses content the user watched arrive (ARCH-09) — the most user-visible item
in this grab-bag and the one most likely to be reported as a bug. Session-scoped in-memory state leaks
on prune, and two maps leak on delete (ARCH-10).

`hardenDBPermissions` is triplicated verbatim across `internal/knowledge`, `internal/longmem` and
`internal/session` — a **file-permission boundary** copied three times, which is the one kind of
duplication worth de-duplicating on principle rather than on measurement (QUAL-04). `internal/tui` is
a god package with a 97-field god struct (QUAL-05). Ten ad-hoc `truncate` helpers sit alongside the one
canonical truncation policy in `truncate.go` (QUAL-07). `context.Background()` appears inside
request-scoped handlers (QUAL-08). `internal/drive` has no package doc and ~10.5% of exported symbols
are undocumented (QUAL-09).

**Promote when:** QUAL-04 should go with any change to DB file permissions; QUAL-05 with any
substantial TUI work (it would also make P66.15's sweep cheaper). The rest are opportunistic.

Priority: Tier 4 — no trigger. QUAL-04 is the only one with a security-adjacent argument.

### P66.19 — Capability gaps with no fired trigger

Assessed against what a mature coding agent needs, and honestly reported as absent rather than
planned: no log rotation and no size cap, with a *text* handler despite the "structured logging"
claim (GAP-02); LSP is seven read-only tools with no rename and no code action, and diagnostics have
exactly one caller so nothing feeds back after an edit (GAP-03); git support stops short of branching
and `internal/worktree` exposes no tool at all (GAP-04); no OS-level sandbox on Windows, conspicuous
because the rest of the Windows story is handled well (GAP-05); the MCP server side lags the mature
client (GAP-07); no test-runner feedback loop as a first-class concept (GAP-08); structured outputs
are wired but used at exactly one call site (GAP-09).

**GAP-03 and GAP-08 are the same missing idea** — nothing closes the loop after an edit — and are the
pair most worth taking together if this tier is ever opened. GAP-02 is the cheapest and the only one
with an operational failure mode (an unbounded log file).

**Promote when:** a user hits one. GAP-06 (resume across daemon death) is **not** here — it is the
pre-existing P65.4 and stays as filed.

Priority: Tier 4 — no triggers. Do not build speculatively.

### P66.20 — Efficiency residue

The obvious performance work is genuinely done — the review verified per-turn session writes, WAL with
`busy_timeout` on all four stores, incremental token estimates, package-level regexes at all 68 call
sites, real read-tool concurrency, persistent sandbox containers. This is the residue after P66.9
takes the one item that mattered.

The `<repo_map>` is built once at daemon startup and never invalidated; the staleness check was
benchmarked at **11.5 ms** against a 185 ms full rebuild, so the cautious fix is affordable — but note
the finding's title was wrong and `POST /repomap/index` already exists (`server.go:115`), so this is
narrower than reported (PERF-04). `toolshim.Prompt` rebuilds a multi-KB prompt string per turn
(PERF-06). Checkpoint file snapshots are uncompressed, undeduplicated and uncapped (PERF-07) — related
to the pre-existing P60.3. Two `flushMessages` calls per turn where one would do (PERF-09).
`MaterializeBuiltins` re-reads 800 KB of embedded skills on every daemon start at a measured 46.7 ms
(PERF-05, **withdrawn** by arbitration as real-but-nil-impact; recorded here so it is not re-filed).

`sseWriter.send` drops the **oldest** queued event under backpressure, which is right for tool calls
and would silently corrupt text (PERF-08) — marked SUSPECTED and never confirmed. If any item here is
promoted, promote that one, and confirm it first.

Priority: Tier 4 — no triggers. PERF-08 is the only one with a correctness edge.

**How to use this tier.** Every Tier-4 item that has actually been measured so far turned out to be
wrong in some way — an unmeasured dependency that was actually our own code, a gate unmeetable by the
work it proposed, a cap that wasn't the largest one in the tree. Take the measurement first, then
re-read the item; do not treat a Tier-4 write-up as a build plan. Details of past measurements are in
[releases.md](releases.md).

### P64.4 — Edit results carry no diff, and a tool cannot attach anything a replay can render

`edit_file`, `edit_section`, `multi_edit` and `fill_marker` return prose ("updated successfully", a
replacement count); the TUI and web transcript have nothing else to render for an edit. The presenter
runs on both live streaming and transcript replay, so it must be deterministic and cannot do I/O at
replay time. A **tool-private, persisted presentation channel** — `execute` attaches an opaque JSON
payload the core stores with the result and hands back to the presenter, computed once at result time
and read back on every replay — is the reusable mechanism; a diff (applied hunk ± context lines) would
be the first consumer. Cost: an overwrite would need to hold both prior and new text in memory to
compute a UI-only hunk.

**Promote when:** the TUI or web transcript is being worked on for another reason, or a user reports
not being able to tell what an edit actually changed. This is presentation with no correctness or
security edge.

Priority: Tier 4 — real, cheap-ish, no trigger. Do not build speculatively.

### P64.5 — `ask_user` is one free-form question; unattended answers cannot be routed

`internal/tool/builtin/ask.go` is one question string, an optional `[]string` of choices, a string
back. A batch answered by anything other than a human at a terminal — the non-interactive
`Questioner`, a future policy answerer, a parent agent answering for a sub-agent — has to match answers
to questions by question text today, since there's no caller-supplied `id` echoed in the answer; the
text is model-authored and may repeat. A structured error taxonomy (user-cancelled vs
no-provider-registered) would also let an unattended drive respond differently to those two outcomes,
which deserve opposite responses.

Against building it: `ask_user` is close to unusable in the unattended drive that is Aegis's main
proving ground today (`AutoAnswer` returns a fixed string), so the routing problem is real but has no
current caller.

**Promote when:** something other than the TUI is answering questions — a policy answerer, a parent
agent answering for a spawned one, or a drive phase that legitimately needs to stop and ask.

Priority: Tier 4 — no trigger, no current caller. Do not build speculatively.

### P61.7 — Retry/terminal classification over *backend-echoed* text (remainder)

`classifyStreamError` decides whether a mid-stream failure is retried or fatal by substring match
against a free-form server error string. The in-repo half shipped 2026-08-06 (Aegis's own OpenAI
adapter was splicing model-authored tool names into a message the classifier then matched — fixed via
`APIError.Detail`, rendered but never classified). **What's left is the case originally described:** a
server or proxy echoing generation fragments into its own `{"error":…}` envelope, where the text is
genuinely external. Still unmeasured, and a fix means guessing at a structural signal (status code, an
error `type` field) most local backends don't supply.

**Promote when:** a misclassification is actually observed, or a backend is found that demonstrably
echoes generation content into its error envelope.
`TestModelAuthoredTextDoesNotSteerClassification` exists as a regression test for the shipped half;
extending it to envelope text is the natural next probe.

Priority: Tier 4 — narrowed to the external case; real surface, unquantified likelihood, no incident.

### P60.3 — Checkpoints capture files only, so `/rewind` is silent about everything else

`internal/checkpoint` snapshots each file a write tool touched (capped at 16MiB) and rewind writes
those contents back — correct within its documented scope. Rewinding a turn that ran `pip install`,
applied a DB migration, started a background process, or wrote a >16MiB artifact restores the source to
pre-turn state while leaving the environment in post-turn state, and the user is told the turn was
undone. If a session owns a persistent container (P60.2, shipped 2026-08-05), a checkpoint could be a
container snapshot/commit instead, making rewind honest about installed packages and process state.

**Re-verified 2026-08-06:** `sandbox.backend` still defaults to `"local"`, so this only helps sessions
using the container backend, which is not the default.

**Promote when:** the container backend is a realistic default for real sessions, or a user reports a
rewind that restored files into an environment that no longer matched them.

Priority: Tier 4 — no longer blocked, but speculative until someone is actually rewinding inside a
container.

### P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)

`newLoopDetector` is constructed inside `Run`, so its window resets every call. In the TUI and web UI
each user turn is a separate `Run`, so a model that loops *across* user turns (re-reading the same file
every time the user nudges it, re-running the same failing command after each correction) is never
detected. Fix: hoist the detector to session scope via an optional caller-owned detector in
`engine.Options`, so the daemon can hold one per session while the CLI keeps today's per-`Run`
behavior. Open design question: a user legitimately asking for the same call twice across two turns
isn't a loop, so a session-scoped detector likely needs a higher threshold or a reset rule keyed on
whether a user message is a bare retry — fuzzier than the current mechanism.

**Re-verified 2026-08-06:** still constructed inside `Run`; design question still the blocker, not the
port.

**Promote when:** a live run shows a cross-turn loop that per-`Run` detection missed.

Priority: Tier 4 — real but unproven, and the false-positive risk is higher than the current detector's.

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of six daemon-singleton services are per-session-scoped; `lsp.Manager` was deliberately left
shared — its per-session resource-growth tradeoff was judged worse than the isolation gap. Parked
pending a concrete multi-tenant need.

**Re-verified 2026-08-06:** still one shared `lsp.NewManager` at daemon construction. No trigger fired.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

### P63.10 — Two small TUI message-handling asymmetries, seen while splitting `Update`

Both pre-existing, found while reading every `Update` case during an unrelated refactor and
deliberately left in place (fixing a bug inside a no-behavior-change refactor destroys the property
that made the refactor safe).

1. **The spinner tick chain dies while idle.** `updateSpinnerTick` drops the `tea.Cmd` returned by
   `m.sp.Update(msg)` when `!m.streaming`; only the streaming branch re-queues. Looks intentional, but
   the chain is *terminated* rather than paused, so it depends on something else re-starting it at the
   next stream — worth confirming that always happens.
2. **A stale toast expiry can retire a newer toast.** `updateToastExpired` clears `m.activeToast`
   unconditionally, without checking the expiry identifies the toast currently shown. Two toasts in
   quick succession cut the second one short by the first one's timer. Fix needs a toast identity to
   compare before clearing.

Priority: Tier 4 — both cosmetic, neither reachable as a correctness/security problem. No trigger; fix
opportunistically if either file is open for another reason.

### P65.4 — Resume is phase-granular, artifact-inferred, and only the drive has it

**What Aegis has, stated carefully — the gap is narrower than it first looks.** The phased drive
already resumes well: a phase whose files carry no `PENDING` marker costs zero model turns on re-entry,
and the whole reset ladder is built on re-entering from disk. Two limits:

- **It is phase-granular and the granularity is the artifact** — the oracle is the `PENDING` marker in
  the skill's own scaffolded files, so a crash 40 turns into phase 6 re-runs phase 6 from its start.
  Probably the right trade at the drive's scale.
- **It exists only because the *skill* supplies the oracle.** A plain TUI/web-UI session, a cron job, a
  swarm sub-agent, an `aegis chat` run with no skill — none of these has artifacts with markers, so none
  resumes at all. Kill the daemon mid-turn and the turn is gone; the in-memory `repairOrphanedToolUses`
  patch (P65.1, shipped) is the entire recovery story outside the drive.

A durable version would need: write-once entries / mutable namespaced registers / an append-only usage
ledger, one register overwritten with the operation's complete current state after every step (recovery
reads that one row and switches on it rather than replaying a journal), and each tool declaring
`replay: "safe" | "never"` so a mid-effect interruption writes a synthetic result under an id reserved
before the effect started rather than re-running it.

**Why Tier 4 and must not be built speculatively.** The session store is SQLite and already has the
storage substrate; `Capability` already partitions the tool set close to what `replay` would need. But
**nobody has reported losing work this way** — the drive, the only workload long enough for a crash to
be expensive, is also the only one that already resumes.

**If it is ever built:** don't design the durable version first. P65.1's in-process `startedTools`
record is the part with a user-visible payoff already shipped, and the durable version is that same
record written through the session store before the effect and cleared after.

**Promote when:** a real run loses work to a daemon restart or crash outside the drive, or a non-drive
workload grows long enough that it would (unattended cron chains, long-lived swarm sub-agents are the
two candidates).

Priority: Tier 4 — no trigger, large, and its cheapest and most valuable slice already shipped as
P65.1.

### P65.5 — Rewinding away from a branch discards its work instead of summarizing it forward

`internal/checkpoint` gives per-turn restore points and the TUI has `/rewind` and fork; rewinding
restores, it does not carry anything forward. Correct default for the common case (the user wants the
last turns gone) but wrong for the case of exploring an approach, learning something real, abandoning
it, and then watching the model rediscover the same dead end because the transcript no longer contains
it. A branch-navigation summary — find the common ancestor, summarize the abandoned span, append it as
an entry on the target branch, same structured format as P65.2's compaction skeleton and file
tracking — would fix it, offered rather than automatic (or `/rewind` stops meaning "undo").

**Why Tier 4 and not higher.** Downstream of P65.2 (shares the summary format and file tracking —
designing it twice would be wasted work), and there's no complaint behind it: `/rewind` has not been
reported as losing anything a user wanted.

A wider version — **lanes**, named cursors into one shared session tree, each owning its own leaf,
model config, queue, and at most one in-flight operation — would be a cleaner model for
`internal/swarm` sub-agents than spawning goroutines with separate histories, but is a session-storage
rewrite with no complaint behind it either; not filed.

**Promote when:** P65.2 has shipped its summary format, **and** a real session loses reasoning worth
keeping to a `/rewind` or a fork. Both conditions, not either.

Priority: Tier 4 — no trigger, sequenced behind P65.2, changes what a well-understood command means.

### P67.10 — Four seams the tool interface does not have

`tool.Tool` is deliberately rendering-agnostic, and that is what lets one registry serve the TUI, the
web UI, ACP and MCP. Nothing here changes that. Four *optional* seams are missing, each small on its
own and none currently blocking anything:

- **A per-tool equivalence predicate.** Loop detection currently normalizes call signatures centrally,
  with two per-tool opt-outs beside it — `PollExempter` (hide the call entirely) and
  `SignatureTransparent` (hide only its arguments). A tool-supplied "are these two inputs the same
  call" predicate is the third member of that family and is a better factoring for tools whose inputs
  are equivalent in ways a generic normalizer cannot see. If built, it goes in the same tests that
  keep the existing two sets narrow and disjoint.
- **Destructive as an axis distinct from write.** `tool.Capability` distinguishes read from write from
  execute, but not reversible writes from irreversible ones (delete, overwrite, send). The permission
  layer currently has no way to prompt harder for the second kind.
- **Interrupt behavior.** When the user submits a new message while a tool is running, the choice is
  cancel-and-discard or keep-running-and-queue. Aegis applies one answer to every tool; a long
  `shell` build and a two-second `read_file` do not want the same one.
- **A search hint for deferred tools.** `<deferred_tools>` prints `tool.Summarize`, which serves both
  the prompt budget and discoverability with one string and is therefore optimized for neither. A
  short keyword line, separate from the summary, would let a model find a deferred tool by capability
  rather than by name.

**Promote when** one of them is actually needed: a loop the current detector misses (predicate), a
destructive tool auto-approved where it should not be (destructive axis), a user complaint about
interruption (interrupt behavior), or a measured failure to find a deferred tool (search hint). Do not
build all four together; they are related only by living on the same interface.

Priority: Tier 4 — no fired trigger. Do not build speculatively.

### P67.11 — Every budget is a ceiling; none expresses how much effort is wanted

`internal/engine/budget.go` is entirely ceilings — `BudgetUSD`, `MaxTokensPerRun`, `MaxIterations`,
`MaxWallClockPerRun`, `MaxTurnStall` — and all of them abort. There is no knob meaning "this task is
worth 200K tokens of thoroughness, keep going until you have spent it or stopped making progress."

The inverted form is coherent: nudge the run to continue while spend is below a target, and stop early
when returns diminish — the workable test being several consecutive continuations whose token deltas
are each below a small threshold, so a run that is still finding things keeps going and one that is
circling stops.

If ever built, it must be a **separate opt-in target with its own resettable stop**, not a second
meaning layered onto an existing knob. A value that is simultaneously a floor and a ceiling is a
footgun, and the existing abort semantics are load-bearing and asymmetric — stall and wall-clock
aborts are fatal to a drive, loop and tool-failure aborts are resettable. The natural home is
`internal/drive`, where "keep working until this phase is genuinely done" is already the model.

**Promote when** a real drive run ends early with budget unspent and work unfinished. The
diminishing-returns stop test is worth remembering separately from the rest of the idea; it is the
part most likely to be useful in another context.

Priority: Tier 4 — no fired trigger, speculative. Do not build speculatively.

### P67.12 — Personas cannot accumulate anything across runs

Personas are stateless prompts, and memory is session- and project-scoped. A persona that learns
something durable about this codebase — a build quirk, a convention, a place where the obvious
approach fails — has nowhere to put it that survives the run.

The shape worth copying is a per-persona memory directory in one of three scopes: **user** (shared
across projects), **project** (committed, shared with the team), and **local** (project-specific,
never committed). The third is the one that carries most of the practical value, and it maps onto the
`~/.aegis/personas/` vs `.aegis/personas/` split Aegis already has.

Two implementation constraints, both cheap and both easy to miss: normalize paths before the
containment check so `..` cannot escape the memory root, and sanitize the persona name for the
filesystem — namespaced names carry characters Windows rejects outright.

**Promote when** a persona is in repeated use on one project and its operator is re-explaining the
same context each run. Until then this is storage without a demonstrated reader.

Priority: Tier 4 — no fired trigger. Do not build speculatively.

### P67.13 — There is no way to execute a plan without committing to it

Plan mode describes intent; it cannot show the diff that intent would produce, because producing the
diff means performing the writes. The mechanism that resolves this is a **copy-on-write overlay**:
writes are redirected into an overlay directory after copying the original in, reads of
already-written paths are served from the overlay, everything else reads through to the real tree, and
execution stops at the first effect the overlay cannot contain — a non-read-only shell command, a
network call, anything outside the workspace — recording a typed boundary describing where and why it
stopped. Accepting promotes the overlay into the workspace; discarding costs a directory delete.

Aegis has most of the substrate: `internal/sandbox` for isolation, `internal/swarm` for forked runs,
`internal/checkpoint` for per-turn restore, and a permission layer that already classifies calls by
capability. **P67.8**'s flag-level classifier is what would decide the shell boundary precisely rather
than conservatively.

The comparison source uses this for *speculation* — predicting the user's next prompt during idle time
and pre-executing it. **That half is not recommended.** The prediction is the expensive, risky,
low-confidence part; the overlay-and-boundary machinery is the durable part, and its first consumer
should be an honest dry-run mode, where the value does not depend on guessing right.

**Promote when** the overlay has a named first consumer — a `--dry-run` that shows a real diff is the
plausible one. Building the overlay with no consumer produces an untested second write path, which is
strictly worse than not having it.

Priority: Tier 4 — no fired trigger, L. Do not build speculatively.

### P67.14 — Hand-composed ANSI has no rule about transitions versus state

Small, and filed as a discipline note rather than a feature. Where Aegis emits escape sequences
directly — kitty graphics chunking in `internal/tui/imagerender.go`, the stripping and rewriting in
`internal/termsafe` — there is no stated rule distinguishing sequences that express **state** from
sequences that express a **transition**.

The distinction has teeth. Style sequences computed as a diff from the previous style are transitions:
two adjacent ones may be concatenated but the earlier one may never be dropped as redundant, because
its reset codes are not guaranteed to be a subset of the later one's — and a dropped background reset
leaks into the next erase via background-colour erase. State-setting sequences (an absolute cursor
position, an explicit colour) can be collapsed to the last one freely. Optimizations that are correct
for one class silently corrupt the other, on one terminal, months later.

Write the rule down where those sequences are composed. **Promote to real work when** Aegis gains a
frame-diff or output-batching layer that would be tempted to dedupe them — until then the comment is
the whole deliverable.

Priority: Tier 4 — no concrete trigger, XS. A comment, not a feature.

---

## Verification Work

**Status: 8 open** (**P68.4**, **P68.5** and **P68.6** filed 2026-08-17; **P68.2** filed *and closed* 2026-08-17 — it ran the same day and its record is
below; **P65.3** closed 2026-08-16, its record is in [releases.md](releases.md)). Every
item here has its code already written and merged — nothing below is a design or implementation task.
Each is closed by running a live-model harness and recording the result the item's closure condition
names, not by writing more code. They are **not tiered**: tiering answers "how urgent is this build,"
and there is no build left to prioritize.

**The 2026-08-16 sitting changed how these should be scheduled.** They were listed as four items
sharing one harness plus P62.8 waiting on hardware. After running it: the shared-harness premise
holds only for what the tier can *observe*, and four closure conditions (LLM-03, LLM-10, ARCH-04 and
P65.2) turned out not to be observable there at all — they need **P68.1** first. P38.1 needs a
permission rather than a schedule slot, and P62.9 needs a better task rather than more runs. **This
whole track is parked at row #6 of [Up next](#up-next--the-six-items-to-take-in-order) by choice**;
what is written below each item is what the run established, so a future sitting starts from evidence
rather than from the pre-run plan.

### P68.2 — The stock Qwen3 chat template deletes tool calls from history, and it confounded two sittings

**Filed 2026-08-17. The mitigation shipped the same day; what is open is what it changes on the
tier.** Ollama renders history server-side from the model's own chat template, and Qwen3's stock
**Go text/template** writes the assistant turn as:

```
{{ if .Content }}{{ .Content }}{{ else if .ToolCalls }}<tool_call>…{{ end }}
```

Content and tool calls are **mutually exclusive branches**. `translate` in
`internal/provider/ollama` sets both fields on any turn where the model narrated before calling —
which a thinking model does most turns — so **the call is silently deleted from the rendered
history** and the model then sees a `<tool_response>` for a call it has no record of making.

**Measured on `qwen3:14b-32k`**, temperature 0, history = prose + `read_file{path:"srv/etc/config.txt"}`
+ result, then asked which path it read:

| arm | correct |
|---|---|
| as captured (prose + call) | **0/3** — answered `/etc/config.txt` |
| prose withheld | **3/3** |
| prose kept, template's `else if` split into two `if`s | **3/3** |
| `aegis-qwen35-9b:32k` (ships a **Jinja** template) | **3/3** |

Two things follow. First, **this is most of the "the 9b is just better" impression**: the 9b's Jinja
template renders prose *and* the call, so it was never losing arguments the 14b was losing. Second,
it **confounds every multi-turn measurement taken on an affected model** — `qwen2.5-coder:1.5b`, the
model behind P52.16's `toolResultEcho` numbers, is affected too, and P62.9's two 14b failures on
2026-08-16 (one rewrote `temps.py` and reported a confidently wrong average) are exactly the shape a
model shows when it cannot see what it just did.

**Shipped 2026-08-17:** `ollamainfo.TemplateDropsToolCalls` reads the template from `/api/show` and
detects the `else if … .ToolCalls` shape; the adapter asks once per model, persists the verdict in
`internal/modelcaps`, warns, and withholds the prose so the call survives. Detector verified live:
`qwen3:14b-32k` and `qwen2.5-coder:1.5b` flagged, `aegis-qwen35-9b:32k` and `gemma4:12b` clear.
**Splitting the turn into two messages was tried first and does not work** — Ollama coalesces
adjacent same-role messages before templating, so the pair arrives as the same message and is
dropped identically (0/3, unchanged).

**Both closure conditions ran the same day, n=6 per arm, and the answer is "the task still cannot
resolve this".** `TestLiveWorkflow/FixSeededBug`, same fixture, three arms:

| arm | passed | tool calls per run (median) |
|---|---|---|
| unmitigated (pre-fix `317c388`, stock template) | **0/6** | 1, 1, 4, 1, 1, 1 (**1**) |
| mitigation active (prose withheld) | **1/6** | 2, 1, 3, 1, 2, 2 (**2**) |
| template-corrected model (`qwen3:14b-32k-fix`) | **2/6** | 9, 3, 39, 1, 1, 4 (**3.5**) |

**0/6 against 2/6 is not a significant difference** (Fisher's exact, p ≈ 0.45), and no claim of one is
made. Three things are nonetheless worth carrying:

- The control arm is now **properly characterised** rather than anecdotal: **0/6 today, 0/8 including
  2026-08-16**, and in five of six runs it ran the script, read the traceback, and **stopped after a
  single `shell` call**. That is a much stronger version of P62.9's own finding.
- The **failure shape moves** with the fix even where the pass rate does not: median tool calls per
  run 1 → 2 → 3.5 across the three arms. The corrected arms engage; the unmitigated one gives up.
- Both corrected arms produced passes; the unmitigated arm produced none in 8 runs. Directionally
  consistent with the probe, **not independent confirmation of it**.

**The template defect itself does not depend on any of this.** It is settled by the history-fidelity
probe (0/3 → 3/3 → 3/3, deterministic, one variable flipped), which is a mechanism-level measurement
and the evidence the shipped fix rests on. This item's end-to-end half is the weaker instrument, and
it behaved like one.

**What this leaves open** is no longer P68.2's original question but P62.9's: the task needs
replacing before any arm-versus-arm comparison on it means anything. **P68.2 is closed as a
measurement** — recorded here and in [releases.md](releases.md), with the tuning procedure written up
in [docs/local-model-tuning.md](../docs/local-model-tuning.md). **What it hands to P62.9** is a
6-run control arm and a concrete reason the 2026-08-16 failures were not purely competence.

**Still unretracted, and now the more interesting thread:** P52.16's `toolResultEcho` measurement was
taken on `qwen2.5-coder:1.5b`, which the detector flags as affected. That experiment measured
tool-result correlation through a template that was deleting the calls being correlated. Re-running
it is cheap (it is a 40-trial probe, not a workflow tier) and is the one re-run this sitting did not
do.

Priority: Verification — the remaining work is P62.9's task replacement, not another run of this.

### P66.22 — The LLM-tier findings are all estimates; one live run converts them to measurements

The P66 review never ran a live model. **LLM-01, LLM-02, LLM-03, LLM-10 and ARCH-04 are all claims
about runtime behaviour against a local model, argued entirely from source.** The arbitration upheld
all five and they are well-argued — but CLAUDE.md is emphatic that this class of claim is settled by
measurement, and this document has twice recorded a fixed instrument *inverting* an already-acted-on
verdict.

One `TestLiveWorkflow` run against `qwen3:14b-32k` answers all of them, and it is the same harness
P38.1, P62.9, P65.2's prompt half and P65.3's local half already need — so this costs no additional
setup if scheduled with them. That bundle was row **#1** of the [Up next](#up-next--the-six-items-to-take-in-order)
ten, and it was one row precisely because running the harness without recording all five wastes the
setup. *(It ran on 2026-08-16 — see below for what that premise turned out to be worth. The remainder
is now row #6, parked.)*

**It was scheduled on 2026-08-16 and did not run: no model server was reachable** (nothing listening
on `:11434`). Nothing about the item changed — it is a measurement, so there is no partial credit and
nothing to substitute for it. Both of its gates shipped that day instead.

**It ran later the same day against `qwen3:14b-32k`. Three of the five closure conditions are met;
two are not observable from this tier.** Full record in [releases.md](releases.md) (*The live-tier
sitting, 2026-08-16*):

- **LLM-01 — met.** Local profile 4,871 provider-reported first-turn tokens against 8,393 default,
  neither clamped at the 16,384 window. With a realistic over-cap `CLAUDE.md`, the deterministic
  budget measures 6,383 estimated tokens against a 6,650 ceiling — the 11,611-token figure this item
  was filed on is three fixes stale. The same prompt costs **5,775 / 9,591** on
  `aegis-qwen35-9b:32k`: the ceiling is in `tokenest` units, not in any tokenizer's, and ~19% spread
  between two local models is normal rather than a regression.
- **LLM-02 — met, and it found the *next* question.** Compaction fires exactly where the shared
  trigger says (85% of 24,576 ≈ 20,889). What it does after that is the finding: **eleven
  compactions in fifteen turns, each summarizing two messages and leaving the context at ~90% full**,
  so every subsequent turn re-crosses the trigger. Prefill quadruples at the first compaction and
  stays there. P62.7's minimum-yield rule suppressed none of the eleven — read that before **P67.6**.
  **Reproduced identically on a second model** (`aegis-qwen35-9b:32k`: same 11 compactions, same
  11→9, prefill 2.3s → 9.2s), where it settles at ~96% full rather than ~90%. It is a property of
  compaction's yield, not of one model.
- **LLM-03 — not read directly.** The fix is in and the path is right; `estimated=false` on every
  `done` event and estimates tracking served counts to ~11% are consistent with a calibrating
  session, but the sample count itself lives in a session trace, and the live-tier daemons delete
  their data dirs on cleanup.
- **LLM-10 and ARCH-04 — not observable from this tier at all.** Both want `aegis sessions trace
  <id>` against a surviving data dir. Closing them needs a harness change (keep the data dir, read
  the trace) or a hand-run session, not another workflow run.

**Closure conditions**, each a number this review could only estimate:

- **LLM-01** — the measured base-prompt token count with a realistic `CLAUDE.md` present, against the
  4,550 ceiling and against the served window. The estimate is 11,611 tokens for the context files
  alone.
- **LLM-02** — the turn at which compaction actually fires against the turn the engine's trigger
  wanted, at a pinned 4,096 window. The claim is that the summarizer refuses until 3,277 when the
  engine asked at 2,048.
- **LLM-03** — whether the P62.4 correction ever fires on the `openai` + `:11434/v1` path. Expected:
  it does not, and the session runs on the uncorrected 20-33% undercount.
- **LLM-10** — whether a model reload occurs between the tool-call probe and the first real turn.
- **ARCH-04** — whether a fan-out or debate call trips `MaxTurnStall` before its own timeout.

**Both gates are now closed.** P66.7 and P66.14 both shipped 2026-08-16 — the reason to sequence
behind them was that they change three of the five numbers, and measuring the pre-fix state answers a
question nobody will have afterwards. Use `-count=1` — a re-run without it replays Go's cached verdict,
which this document has been caught by before.

**P66.11 shipped too, so the instrument is in place.** `TurnTrace` now carries the stop reason, the
compaction event (applied / summarized / suppressed, tokens freed, and the estimate and trigger the
decision was made on), the guard verdict, the correctives the engine injected, and a run id — LLM-02's
and ARCH-04's closure conditions restated as a struct. Read it with `aegis sessions trace <id>`, whose
`WHY` column renders exactly these, or from the JSON export for the full record.

**Two of the five expectations above have already moved, and the item's own text is now the pre-fix
statement rather than the prediction:**

- **LLM-02** is fixed rather than merely measurable: one shared trigger means the two gates cannot
  differ, so what a live run now measures is *whether the shared number is the right one*, not whether
  the two agree. The 2,048-vs-3,277 disagreement it describes no longer exists.
- **LLM-03** is fixed: the calibration gate is now a positive backend identification, so it fires on
  the `openai` + `:11434/v1` path. The run should confirm a non-zero sample count rather than
  discovering there is none.
- A **third expectation is retired by a side effect**: the prune-thrash the P62.7 minimum-yield rule
  rate-limits was a *consequence* of the LLM-02 disagreement, and on the P62.7 fixture it disappears
  entirely once the trigger is shared. A run that was expected to observe it should not.

Priority: Verification — one run, five answers. Both gates shipped 2026-08-16; needs only a reachable
model server.

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
one context with no orchestration mis-route.

**Conformance: still unmet.** Every re-test has stalled short of an unattended verify-clean suite, but
each stall has moved the blocker further from the harness and closer to raw model throughput. Full
dated log (2026-07-21 through 2026-08-09) is in [releases.md](releases.md) (*P38.1 re-test log*).
Most recent result:

- **2026-08-09, LFM2.5-2.6B then qwen3:14b vs AiGateway — conformance still unmet, ten harness
  defects root-caused and shipped as P39.16.** The 2.6B produced zero files in two runs and is now
  refused by a pre-flight gate. The qwen3:14b arm built the **complete suite** — six files, ~35KB, all
  five content phases, every marker cleared — further than any prior run on this target, but
  verification did not pass unattended: `component-name-consistency`, `count-consistency` and
  `coverage-ledger-complete` remained after the bounded fix loop. Two of those three were then fixed
  structurally (P39.16); the third re-run hung before reaching a verdict. All ten defects were the same
  shape — a tool that held the information the model needed and returned an error without it.

**Direction (user, 2026-07-24):** the strongest lever is making local models **piecemeal both their
reads and their writes**, then finishing with a **quality-validation pass** — P39.12-P39.15 implement
this, and P39.16 (2026-08-09) extended it: piecemeal writing still failed while it went through
`edit_file`, because an anchored edit asks the model to *reproduce* text rather than only produce it.
Handle-based tools (`fill_marker`, `edit_section`) remove the reproduction step and are what finally
made the fill loop reliable on a 14B model.

**Reproduce:** `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads outside
the workspace root); run
`aegis chat "threat model this repo" --skill threat-modeling --mode build --yes` (the prompt is
required). It prints a `phased mode` notice and resets context each phase.

**Closure condition:** the real suite's PENDING markers reach zero and `verify.py` / `lint_dfd.py` /
`inventory.py --check` all pass, **unattended, in one invocation**. Met once, 2026-07-24 on
FirewallRuleAnalyzer; not repeated since.

**2026-08-16: scheduled with the live-tier sitting and did not run.** The model server was reachable
and the target copy staged; the run itself was refused, because the recipe is an unattended agent
with auto-approved host shell (`--yes` plus `auto_approve_exec`) and the session driving it was not
permitted to launch that. Nothing about the item changed. This is a standing property of the recipe,
not a one-off: whoever runs it next either runs it by hand or grants the permission deliberately.

Priority: Verification — every load-bearing harness fix the re-tests have root-caused has shipped
(P39.5-P39.18, P47.1-P47.9, P52.12, P57.1). This item stays open only as the conformance **umbrella**,
closeable once a live built-in drive is confirmed to reach a verify-clean suite unattended, in one
invocation, on a local model. No code work remains; it is live-run tracking.

### P68.3 — The tier's task was a boolean, so it could not rank anything

**Filed and shipped 2026-08-17, out of P68.2's underpowered re-run.** P68.2 ran three arms at n=6 and
returned p ≈ 0.45. The diagnosis is not that six runs is too few — it is that
`TestLiveWorkflow/FixSeededBug` is **pass/fail, so a run yields one bit**, and no n a live tier can
afford rescues an instrument that coarse. Three structural defects, all measured rather than argued:

1. **One bit per run.** Six runs, six bits.
2. **It bottoms out.** Five of six control runs scored zero by *giving up after a single tool call*.
   A task most runs score zero on cannot rank two models, let alone two configurations.
3. **No cross-turn dependency.** Its ideal path is three tool calls, so a model never carries a fact
   from an early turn to a late one — which is exactly the failure P68.2 found in the wild. **The
   tier's discriminating task was structurally blind to the defect the tier had just caught.**

**Shipped: `TriageTask`** (`internal/eval/triagetask.go`), a graded security-triage task scored out
of 12 — five discovery points, 2 precision, 2 integrity, 2 remediation, 1 no-regression. Nine
pure-stdlib Python files (no pytest, no pip: a missing dependency and a weak model must not produce
the same red), five planted issues spanning trivial-to-cross-file, and one file clean on purpose.
Grading is entirely mechanical — parse a JSON report, run a suite, hash the protected files — because
an LLM judge would put a second model's variance *inside* the instrument.

`SeededBugTask` is **kept and relabelled as the tier's control**: small, unambiguous, and a harness
that fails it is failing at driving a model rather than at a hard problem. The two answer different
questions and the tier now runs both.

**Measured the same day, n=3 per model:**

| model | scores | mean |
|---|---|---|
| `aegis-qwen35-9b:32k` | 9, 11, 12 | **10.7 / 12** |
| `qwen3:14b-32k` (mitigated) | 3, 2, 3 | **2.7 / 12** |

Complete separation at n=3 (exact Mann-Whitney, p = 0.10 two-sided — the floor for this n), against
the old task's p ≈ 0.45 at n=6. **This is the closure condition P62.9 has been asking for**: a task
whose result means something at a single-digit n.

The 14b's failure is also now *specific* rather than a bare red: it **never wrote `findings.json` in
any of three runs** despite greping extensively. That is a reporting-step failure, not a
tool-reachability one, and it is the kind of thing the old task could not have said.

**The first live run found a defect in the grader**, which is recorded because it is the argument for
running a new instrument before trusting it: a run that left the project unable to import collapsed
all three run-dependent criteria onto one truncated `Traceback (most recent call last):` — a rubric
printing the same non-diagnosis three times. Breaking the code is now charged **once**, to
`no_regression`, with the actual exception named; discovery is explicitly unaffected, so a broken
build cannot erase a correct audit. Pinned by
`TestTriageBrokenProjectIsChargedOnceAndDiagnosed`.

**What is open:** nothing in this item. What it hands onward is a usable instrument — the arm-versus-arm
comparisons P68.2 could not make (mitigation on/off, template corrected/stock, and the sampling
parameters `docs/local-model-tuning.md` currently recommends on judgement alone) are now runnable at
an n a sitting can afford.

### P68.4 — The triage rubric's measuring band sits below the strongest local model

**Filed 2026-08-17, from a temperature A/B that measured nothing — twice.** P68.3 shipped a task that
ranks *models* well (9b 10.7 vs 14b 2.7, complete separation at n=3). The attempt to use it for the
next question — do the sampling parameters `docs/local-model-tuning.md` recommends actually help? —
found it cannot rank *configurations*, because both available substrates sit against a rail:

| substrate | temp 0.2 | temp 0.6 | reading |
|---|---|---|---|
| `aegis-qwen35-9b:32k` | 12, 12, 12 | 12, 12, 12 | **ceiling** — rubric exhausted |
| `qwen3:14b-32k-fix` | 3, 3, 3, 3, 3 | 3, 3, 3 | **pinned low** — one repeated minimal strategy |

Both arms of both A/Bs are flat, and **neither is evidence that temperature does not matter** — a
saturated instrument returns exactly this pattern whether the variable matters or not. Reading these
as a null would be the same error as reading P68.2's 0/6-against-2/6 as a win, in the other direction.

Two instrument checks were run before concluding, and both came back clean, which is what makes the
"no headroom" reading the surviving one rather than a guess: the derived Modelfiles differ **only** in
`temperature` (`ollama show` confirms `num_ctx` and everything else carried), and all four derived
models still carry the **corrected** chat template (the `FROM <derived model>` inheritance was the
obvious way for this to be a silent P68.2 regression rather than a real result).

**The substrate was removed from the machine on 2026-08-17, after this was filed**, which makes the
item harder rather than staler: `qwen3:14b-32k` and its corrected build are both gone, so the only
mid-range scorer either A/B had is no longer available. What remains locally is
`aegis-qwen35-9b:32k` (saturates at 12/12), `qwen2.5-coder:1.5b` (historically zero tool calls on the
older tier — see [providers.md](../docs/providers.md)) and **`gemma4:12b`, which is untested here and
is the obvious first thing to score**: its manifest advertises tools and its template is clean by the
P68.2 detector, so it is a candidate mid-range substrate rather than a known one. Re-pulling
`qwen3:14b` and rebuilding the corrected variant per
[docs/local-model-tuning.md](../docs/local-model-tuning.md) is the fallback, and is cheap.

**What it needs:** a harder tier of criteria so a strong model has somewhere left to go, and a floor
that a weak model clears by more than one repeated strategy. Candidates, none costed:

- a sixth planted issue that only a cross-module data-flow trace finds (the current hardest, the
  `wire.py` → `jobs.py` pickle, is the one criterion the 9b sometimes misses — so the difficulty
  gradient is right, there is just not enough of it above);
- severity grading, currently parsed and discarded — a finding reported at the wrong severity is
  presently worth the same as one reported correctly;
- points for *not* touching the three files the task never mentions, which the 14b family edits.

**Until this lands, `docs/local-model-tuning.md`'s sampling section stays labelled reasoned-not-
measured**, and it says so in the document. That is the honest state: two experiments were run and
both were void, which is different from "tested and found not to matter", and the page must not drift
into implying the latter.

### P68.5 — P52.16's `toolResultEcho` measurement was taken through a defective template

**Filed 2026-08-17.** P52.16's echo experiment — 32/40 bare → 38/40 echoed, the measurement the whole
`toolResultEcho` mechanism rests on — was run on **`qwen2.5-coder:1.5b`**, which P68.2's detector
flags as shipping the `else if … .ToolCalls` template. That experiment measured tool-result
*correlation* through a renderer that was deleting the calls being correlated, which is close to the
worst possible confound for it: the echo's stated purpose is carrying an association "in content
where the protocol cannot carry it in metadata", and the protocol was losing even more than assumed.

Nothing is retracted here. The +15pp may well survive — the echo could be *more* valuable when the
call is missing entirely, not less — but the number as recorded describes a setup nobody would choose
today.

**What would close it:** re-run the 3-parallel-`read_file` attribution task, 40 trials per arm, on
`qwen2.5-coder:1.5b` with the P68.2 mitigation active, and again on a template-corrected build. This
is a probe rather than a workflow tier — cheap, and it does not need the live-tier sitting's setup.

Priority: Verification. It is the one re-run the 2026-08-17 sitting identified and did not do.

### P68.6 — The 14b family never produces the report, and nothing in the run says why

**Filed 2026-08-17, from P68.3's first live sittings.** Across six graded runs on `qwen3:14b-32k` and
its template-corrected build, the dominant failure is not finding and not fixing — it is that
`findings.json` **is never written, or is written naming 2 of 5 issues**, after the model has greped
the codebase extensively. One run made sixteen tool calls of which ten were consecutive `grep`s and
produced no artifact at all.

This is a model-behaviour observation, but it is not obviously *only* that, which is why it is filed
rather than noted in a doc:

- the task names the output file explicitly and gives its schema in the prompt, so this is not an
  ambiguous instruction;
- the local prompt profile defers `edit_file` and exposes the handle-based editors, and the runs that
  do write use `write_file`/`multi_edit` — so it is worth checking whether a model that has decided
  to "write a JSON report" finds a tool that obviously does that, or bounces off the deferred surface
  and falls back to searching;
- P62.9 has an unresolved watch item about exactly this class of detour, and its `tool_search` signal
  has now been unobserved at n=5 across two sittings.

**Both models were removed from the machine on 2026-08-17**, so this is not reproducible locally
without re-pulling `qwen3:14b` — worth knowing before someone plans a sitting around it. The
behaviour is recorded in enough detail above to be recognised if it recurs on another model, and
whether it is Qwen3-specific or general is itself now an open question.

**What would close it:** read one such run's trace (which needs **P68.1** — the tier deletes its data
dir) and establish whether the model ever attempted a write tool and failed, or never selected one.
Those are an Aegis problem and a model problem respectively, and the run as recorded cannot tell them
apart.

### P62.9 — The exposed-schema half of the base prompt: five editing tools and three prose blocks

**Built 2026-08-14** (local-profile base prompt 4,907 → 4,317 estimated tokens): `edit_file` deferred
under the local profile with the four P39.16 handle-based tools left exposed, and local variants of the
three shared prose blocks that compress rather than drop rules. `ScopeExposed` was also fixed to load a
named deferred tool for a drive phase's declared surface instead of leaving it silently hidden — two
phases (`dfdPhaseTools`, `assessmentPhaseTools`) had been running prompts naming tools not in their
arrays since the day they were written. Full write-up in [releases.md](releases.md).

**Closure condition (not met):** a live-tier measurement (`TestLiveWorkflow`) showing the agent's
behaviour is not worse, watching two things: whether a small model with `edit_file` deferred actually
reaches the handle-based tools instead of burning a turn on `tool_search`, and whether the compressed
`completing-tasks` block still holds the write-the-file rules a small model was measured dropping
first.

**First live evidence, 2026-08-14 (qwen3:14b, seeded-bug task via `aegis chat`, three runs per arm).**
The `tool_search` detour did not happen — across three runs the model went straight to `edit_section`
or `multi_edit`. A pointer defect was found and fixed instead: `edit_section`'s description and
no-headings error both pointed at the deferred `edit_file`, costing one run three failed calls and a
tool-failure-breaker trip before it reached `multi_edit`; both now name `multi_edit`, exposed under
both profiles. What's unanswered is turn cost, not correctness: deferred-surface runs solved the task
in 4-6 tool calls against a steady 3 with `edit_file` exposed, but a control arm with `edit_file`
exposed also failed the task outright twice (by explaining the fix in prose instead of applying it),
so single-run differences on this task are inside the noise, and **no default-prose control has been
run** for the second watch item.

**Superseded in part by P68.3, 2026-08-17.** The second half of the sentence below is the half that
was right, and it has now been built: `TriageTask` is graded out of 12 and separated two models
completely at **n=3** (10.7 vs 2.7), where this task returned p ≈ 0.45 at n=6. Re-running *this*
task at n≥10 would buy a tighter estimate of the wrong quantity, exactly as recorded below.

**What would close it:** the same task at n≥10 per arm, or a task whose edit is unambiguous enough
that a single run means something, plus a default-prose control for the prose-attributable failures
above. Both are runs, not code.

**A second model closes the first watch item, 2026-08-16.** On `aegis-qwen35-9b:32k` the whole
`TestLiveWorkflow` tier passes, including the seeded-bug task — the first time it has been solved on
this tier. With `edit_file` deferred, the model went straight to `multi_edit` (5 tool calls, 13.8s,
no `tool_search`, no detour). The guard arm solved the same task the long way and is the better
record: `edit_section` errored, `multi_edit` errored, and the model re-read the file and got the next
`multi_edit` right — recovery in two calls, no breaker trip. **The deferred surface is reachable and
the compressed prose holds; what is unmeasured is now only turn *cost* against an exposed-`edit_file`
control.**

**Two runs on `qwen3:14b-32k` the same day argue for replacing the task rather than repeating it.** Neither touched `tool_search` — the detour this item watches for is now unobserved at n=5
across two sittings. But both failed the task: one rewrote `temps.py`, re-ran it, and reported a
confidently wrong average; the other ran the script once, read the `TypeError`, and stopped without
editing anything. With the 2026-08-14 control arm failing outright twice as well, **the seeded-bug
task is measuring model competence, not tool reachability** — n≥10 on it would buy a tighter estimate
of the wrong quantity. Replacing the task is the cheaper close. Record in
[releases.md](releases.md).

Priority: Verification — the code is in; what remains is verification competing with P38.1 for the
same scarce live tier, and they can be run in one sitting.

### P65.2 — Compaction summaries are free prose, and nothing carries the file set forward (prompt half)

**Deterministic half shipped 2026-08-14**: `<read-files>`/`<modified-files>` tags now accumulate
across compactions and survive the fallback path, carried via a context decorator
(`engine.FileContextCompactor`) since `Summarizer` is built once per server and shared across sessions.
Cost measured at delta 66 tokens for the skeleton, 33 tokens for a 10-path file list (17 at the 40-path
cap) — comfortably inside budget. Full write-up in [releases.md](releases.md).

**What remains, and it is a run rather than code:** the prompt half — a fixed summary skeleton (`##
Goal` / `## Constraints` / `## Progress` / `## Key Decisions` / `## Next Steps`) instead of free-form
"use terse bullet points" — is built but held open on its own stated gate: a live run showing a local
model fills the skeleton without losing content the terse-bullet prompt kept. Free-form compression is
*generation* and structured fill is *completion*, and every measurement in the P38.x line says local
models degrade on the first and hold up on the second — this is the last unstructured-prose ask left in
the engine, at the moment the model's context is fullest.

**Promote when:** P38.1's re-run is done and the live tier is free — the prompt change wants the same
harness, so running them together costs one setup instead of two.

**2026-08-16: the harness cannot see what this item needs to judge.** The live tier ran twenty-two
compactions across the two P62.2 arms — the skeleton prompt was exercised repeatedly — but a
compaction's *summary text* never reaches the SSE stream, so the run reports that compaction happened
and nothing about what it kept. Judging skeleton-fill against terse-bullet output needs the summary
itself: either a session trace from a run whose data dir survives, or a notice/event carrying the
summary. That is a small harness change, and it is now this item's real blocker rather than tier
availability.

Priority: Verification — real value, unblocked, code already built, gated on live evidence rather
than on design.

### P62.8 — The prefix-cache gate's large-window regime has never been measured

`compaction.shouldPrune` has two regimes: below `largeContextWindowThreshold` (200,000) it fires at a
25%-free ratio; above it, a fixed 40k buffer, which on a large window places the prune much earlier in
relative terms than anything measured so far. Everything known about this gate comes from a
24,576-token window (the ratio branch only), and P62.2's history is a specific warning against
generalising from it — the same fixture gave opposite verdicts before and after an instrument fix,
because what mattered was *where in the window* the prune landed relative to the backend's
context-shifting point. The buffer branch changes exactly that relationship and is unmeasured. The
gate itself needs no new code — this is purely a measurement gap.

**Why parked rather than queued.** Needs a backend serving a >200,000-token window. Models on hand top
out at 40,960 (qwen3:14b) and 262,144 (gemma4:12b, but a 200k+ KV cache on 16GB VRAM / 16GB system RAM
is swap-bound, so it would measure paging rather than the gate). Hardware block, not a design question.

**How to run it when hardware allows:**
`AEGIS_EVAL_MODEL=<model> go test -tags live_workflow -count=1 ./internal/eval/ -run
TestLiveWorkflowCompactionPrefixCacheGate -v`, with `compactionNumCtx` raised past 200,000 and
`writeCompactionFixture`'s per-file payload scaled up so the chain still crosses the trigger.

Priority: Verification — no trigger, no user impact, blocked on hardware rather than on any decision
or any remaining code.

---

For shipped feature history, batch origins, refutation records, competitive-landscape review and the
full gap analysis, see [releases.md](releases.md).
