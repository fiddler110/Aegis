# Aegis Capability Roadmap

**Last updated:** 2026-08-16. This document tracks only **open** work and what's next. For
shipped-feature history, batch origins, pass-by-pass narrative, refutation records and full design
rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading with a
`Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so keep it when
adding items.

---

## Status

**42 open items: 36 build (Tier 1-4) + 6 verification-only. Tier 1 is empty.**

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
against HEAD `3c2b57b`, recorded in [CodeReview.md](../CodeReview.md) with per-finding evidence. The
items filed below (**P66.11**-**P66.26**) carry every finding worth acting on; each names the finding
IDs it closes, so the review document is the rationale and this document is the work. **Thirteen of
the batch shipped on 2026-08-15/16** — P66.2, P66.1, P66.4, P66.3, P66.6, P66.7 (both halves), P66.8,
the P66.24 flake found while building P66.4, and then P66.5, P66.16, P66.10 and P66.9 — including
both Criticals and **all six** of the findings that were exploitable the day the review landed. Their
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

- **Tier 1:** 0 — empty. **P66.5** shipped 2026-08-16, closing the last exploitable-today finding.
- **Tier 2:** 9 — **P66.11**, **P66.12**, **P66.21**, **P66.25** (SEC-07, refiled), plus five from
  P67: **P67.1**, **P67.2**, **P67.3**, **P67.4**, **P67.5**.
- **Tier 3:** 7 — **P66.13**, **P66.14**, **P66.15**, plus four from P67: **P67.6**, **P67.7**,
  **P67.8**, **P67.9**.
- **Tier 4:** 20 — **P66.17**, **P66.18**, **P66.19**, **P66.20**, **P66.23**, **P66.26** (PERF-02,
  refiled), the five from P67 (**P67.10**-**P67.14**), plus the nine pre-existing: **P65.4**,
  **P65.5**, **P64.4**, **P64.5**, **P61.7** (remainder), **P60.3**, **P52.14**, **P25.9**, **P63.10**.
- **Verification:** 6 — **P66.22**, **P38.1**, **P62.9**, **P65.2** (prompt half), **P65.3**
  (local half), **P62.8**.

**What to do next.** Rows 1-5 of the "Up next" ten shipped on 2026-08-16 (see
[Up next](#up-next--the-ten-items-to-take-in-order) for the table as it now stands). What that
leaves:

**Tier 1 is empty and there is no forced order left in the batch.** One sequencing fact survives:
**P66.14 gates P66.22**, the live-tier run. P66.7 was the other gate and has now shipped, so P66.14
is the only thing still standing between the batch and the measurement — it changes numbers P66.22
would otherwise measure wrong, and measuring first answers a question nobody will have afterwards.

**The two refiled sub-items are not equivalent in urgency.** **P66.25** (SEC-07, content-bound trust
grants) is real work with a real gap behind it — a `git pull` that adds a `hooks:` block still
re-prompts nothing — and P66.5 built its prerequisite, the well-defined security-relevant subset.
**P66.26** (PERF-02) is Tier 4 and should stay there: it is a Low-severity durability trade on the
one database that holds checkpoints, the cost ledger and traces.

**Among verification items**, P38.1's live conformance re-run stays the highest-value one: it is the
only open item anywhere whose outcome produces new information rather than new code, and three of the
other verification items (P38.1 itself, P62.9's remaining live-tier evidence, P65.2's prompt half)
all close on the *same* harness — a local model driving `aegis chat --skill threat-modeling`. One
live-tier setup answers all three:

- **P38.1** — the conformance verdict itself (see its entry for the closure condition and reproduce
  steps).
- **P62.9** — needs n≥10 per arm on the deferred-`edit_file` surface, plus a default-prose control
  that has not yet been run (first n=3 evidence is in its entry).
- **P65.2**'s prompt half — does a local model fill the compaction-summary skeleton without losing
  what the old terse-bullet prompt kept? The summarizer fires mid-drive, so P38.1's run is this
  question's fixture too.

No Tier 4 build item currently has a fired trigger (re-verified 2026-08-15: `sandbox.backend` still
defaults to `"local"`, `lsp.Manager` is still one shared daemon singleton, both TUI asymmetries in
P63.10 are still present as described) — see each entry's **Promote when** for what would change that.

**Method notes worth re-reading before filing or building anything new** (full detail in
releases.md's pass history): before measuring an optimization, check the instrument the rest of the
system is running on — this document has twice recorded a fixed instrument *inverting* an
already-acted-on verdict. Every documented live-tier command needs `-count=1`, or a re-run silently
replays Go's cached verdict instead of reproducing. Mutation-test any new numeric threshold — a short
fixture cannot tell adjacent thresholds apart, and a count assertion cannot tell *when* something
fired. And **read the refutation records in releases.md before filing anything** against
`internal/provider`, `internal/ollamainfo`, `internal/repomap`, or scanner method resolution — several
obvious-looking gaps there have already been checked and answered.

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

## Up next — the ten items to take in order

**Written 2026-08-16**, after the P66 day plan closed. This is a *reading* of the tiers, not a
second ranking: every row's tier and size come from the item's own `Priority:` line, and the order
is what those tiers say once the two real sequencing constraints are honored. Each row's "why now"
is the one-sentence case; the item's own entry below carries the evidence and closure condition.

**Rows 1-5 shipped later the same day** — one commit each, each independently green, build records in
[releases.md](releases.md). They are struck through rather than deleted, because three of them
correct the finding they were built from and the row is where a reader looks first. **Rows 6-10 are
the live list**, and nothing about their ordering changed: they had no dependency on rows 1-5 except
#10's, which is now half-satisfied.

| # | Item | Tier / size | Why now |
|---|------|-------------|---------|
| 1 | ~~**P66.5** — invert the config freeze list~~ **SHIPPED** `0352112` | T1 · M | All of Tier 1. Last of the six exploitable-today findings: `commands:` unfrozen means an untrusted repo gets arbitrary binary exec through `grep` — a `CapRead` tool, so **plan mode allows it silently**. Inverted to a `configTrustPolicy` table that defaults to frozen. `security.*` landed as `frozenUntilTrusted`, not baseline-only — that would have broken `PatchProjectSecurity`'s six call sites. SEC-07 refiled as **P66.25**. |
| 2 | ~~**P66.7** — cap context-file injection (LLM-01 remainder)~~ **SHIPPED** `9482c87` | T2 · S | `CLAUDE.md` injected uncapped. **The 11,611-token figure was stale** — 10,257 bytes / 2,560 tokens when measured at build time — so the 8,000-byte cap was derived from the served window instead. The budget test now runs over a realistic `CLAUDE.md` fixture and fails if the cap is lifted. Ungates #10. |
| 3 | ~~**P66.16** — OpenAI adapter drops tool calls~~ **SHIPPED** `444516e` | T2 · S | **Worse than filed:** `Finish` iterating `0..len` over a map keyed by wire index doesn't drop trailing calls on a 1-based backend, it emits **zero** — `len == 1` reads `tools[0]`, finds nil, continues. Fixed by sorting the map's real keys; IDs synthesized as `tu_<index>`. |
| 4 | ~~**P66.10** — bounded security remainder~~ **SHIPPED** `fd4f49b` | T2 · S-M | All three landed; SSRF list deduplicated into `internal/netblock`. **VULN-03's suggested `::ffff:0:0/96` was rejected**: `Contains` reduces it via `To4()` to `0.0.0.0/0`, blocking the whole public internet. Pinned by an over-blocking guard test. |
| 5 | ~~**P66.9** — bound `bg_events`~~ **SHIPPED** `d4fb209` | T2 · S | `DefaultBGEventRetention = 2000`, deliberately not a config key — the defect *was* a pruner gated on an unset one. The interval alone is not a bound: a session appending <128 events per daemon lifetime accumulates across restarts, so a process's first append always sweeps. PERF-02 refiled as **P66.26**. |
| 6 | **P66.21** — doc corrections the review disproved | T2 · S | Three left (P66.8 closed the first). The `view.go` one is actively harmful: it asserts the pre-P35.13 `prompt_eval_count` claim and proposes remediation that must not be done. |
| 7 | **P66.14** — reconcile the two compaction thresholds | T3 · M | The engine triggers at 2,048 on a 4,096 window; the summarizer refuses until 3,277 — so compaction lands with 819 tokens left for the completion. And the P62.4 calibration is inert on the documented `openai` + `:11434/v1` path, so **every user following the documented configuration runs the whole session on a 20-33% undercount**. Also gates #10. |
| 8 | **P66.12** — staticcheck cleanup | T2 · S | 28 findings, no new defects — a good result worth banking. Closure is deleting `continue-on-error` from CI; until then the step is advisory and the next 28 accumulate the same way. |
| 9 | **P66.11** — redaction + turn trace | T2 · M | `internal/share` redacts nothing at all. The `TurnTrace` half is the higher-leverage piece: stop reason, compaction event, guard verdict and retry record are all computed and discarded one line later, and every live-tier item in this document would be easier to close with them. |
| 10 | **P66.22** — the live-tier run | Verification | Converts five estimated LLM-tier findings into measurements in one `TestLiveWorkflow` pass. **#2 has shipped, so only #7 still gates it.** Shares its harness with P38.1, P62.9 and P65.2's prompt half — schedule all four in one sitting. |

**Two notes on the ordering.** **P66.13** (T3, M-L) is deliberately below the cut even though
`aegis chat` bypassing the whole permission stack is the more serious defect: it needs `newChatCmd` —
683 lines wrapping a 615-line closure — split before either bug is testable, and that enabling
refactor does not belong in a batch of small fixes. **P38.1** is the highest-value *verification*
item and is available whenever a local model is up; #10 is listed instead because P38.1's own run is
cheaper to schedule as part of that same sitting.

Rows 2-9 have no dependencies on each other and can be reordered freely — only #1 (Tier 1) and #10
(sequenced behind #2 and #7) are fixed. **As of the rows 1-5 sitting, the live remainder is #6, #7,
#8, #9 in any order, then #10 behind #7.**

**This reading predates the P67 batch** and is left as written rather than re-ranked — it is the
record of what the tiers said on the morning of 2026-08-16. Three P67 items would enter the ten on a
re-read, and one interacts with a row already on it:

- **P67.1** (per-round tool-result cap) would sit around #4. It is the smallest item in either batch,
  it closes a hole that parallelism opened in an otherwise fully-argued file, and it reuses the spill
  path unchanged.
- **P67.3** (call-purpose tag on provider requests) would sit just behind it — small on its own, and
  the enabling seam for **P67.6**, which cannot be gated correctly without it.
- **P67.6** (time-based micro-compaction) **touches the same question as row #7, P66.14**. P66.14
  reconciles *when* compaction fires against context pressure; P67.6 adds a second, orthogonal
  trigger based on cache temperature. Doing P66.14 first is right — reconcile the thresholds that
  disagree before adding a third path into the same machinery — but whoever takes P66.14 should read
  P67.6 first so the two triggers are designed as one decision rather than bolted together.

The remaining P67 Tier-3 items (**P67.7**, **P67.8**, **P67.9**) are each larger than anything on the
current ten and belong to a later sitting.

---

## Open Work — Tier 1

**Status: 0 open — Tier 1 is empty.** Every finding the review classified as exploitable on the day
it landed has now shipped: P66.2 (2026-08-15), then P66.1, P66.4, P66.3, P66.6 and finally **P66.5**
(2026-08-16). See [releases.md](releases.md) for what each landed and what was found while landing
it — several of those records correct the item they were built from, which is the part worth reading
before trusting [CodeReview.md](../CodeReview.md) directly.

An item enters this tier when it is a real, currently-exploitable security or robustness gap that is
small and has no dependency. Nothing currently qualifies; a new one would come from a review pass or
a fired trigger on a Tier 4 entry, not from re-reading the existing findings.

---

## Open Work — Tier 2

**Status: 9 open** — four from the P66 review batch and five from the P67 external-source reading.
**P66.7, P66.9, P66.10 and P66.16 shipped 2026-08-16** (P66.8 earlier the same day), leaving P66.11,
P66.12, P66.21 and the newly refiled **P66.25**. Each is self-contained and independently shippable;
the one ordering constraint inside the tier is that **P67.3** builds the seam **P67.6** (Tier 3)
needs, so taking P67.3 early costs nothing and unblocks later work.

### P66.11 — Nothing redacts, and the turn trace is too thin to debug a bad run

Two halves of one gap: what leaves the process, and what is kept about a run.

`internal/share` performs **no redaction at all** (SEC-08) — a shared session carries whatever the
transcript holds, including anything a `.env` or `ps` call put there. Add a redaction pass reusing
`internal/mcp/outbound.go`'s existing credential patterns, emit a redaction count so a silent miss is
visible, and apply it to the audit trail too (SEC-11's redact-don't-truncate half).

`TurnTrace` carries no stop reason, no compaction event, no guard verdict, no retry record and no run
id (GAP-01) — **all of which are already computed and discarded one line after they are produced**.
For a project whose entire method is measurement-driven, that is the gap most at odds with how the
project works: every live-tier item in this document would be easier to close with it. Widen the
struct; **skip the OTel/Prometheus half**, which is a dependency decision this item does not need to
make.

Closes SEC-08, SEC-11, GAP-01. Priority: Tier 2 — M.

### P66.12 — staticcheck cleanup

`staticcheck` (2026.1) now runs clean across the tree and reports **28 issues in 173k lines** — no new
correctness or security defect, which is a good result worth recording. What is left is housekeeping:
17 U1000 unused symbols (including `doctorToolCallSmokePrompt` at `internal/cli/doctor.go:616`, a
leftover from before the probe moved to `internal/toolcallprobe` and an invitation to edit the wrong
copy); three vestigial `SA4005` fields on `fakeImageScanner` (`internal/security/security_test.go:315`)
that are positioned and commented exactly like a working P55.7 assertion but are dead — the real
assertion runs through `recordingImageScanner`'s pointer; one genuine `SA4006` test gap at
`internal/compaction/compaction_test.go:95`, where the under-budget path never inspects `out`; and
style hits.

Two are **false positives** worth rewording anyway: the deliberate side-effecting
`d.record("a") || d.record("a")` at `internal/engine/loopdetect_test.go:286`, and prose beginning
`go:embed` in a doc comment at `internal/security/multiscanner_test.go:781` — in the one file whose
subject is embed patterns silently omitting files.

`staticcheck` is **already in CI** as of P66.2, with the toolchain pin and its rationale documented
beside the install step (`honnef.co/go/tools` carries `toolchain go1.25.13` in its own `go.mod`, so
`GOTOOLCHAIN=auto go install` produces a binary that cannot analyze this go1.26 module and reports 21
compile errors instead of analysis). It runs `continue-on-error: true` precisely because these 28
findings are still open. **Deleting that line is this item's closure condition** — clearing the
backlog without making the step gate leaves the next 28 to accumulate the same way.

Closes QUAL-15. Priority: Tier 2 — S.

### P66.21 — Documentation corrections the review proved wrong

Four documented claims that the code contradicts. Grouped because doc work should not be counted as
remediation effort, and because a wrong doc in this repo is load-bearing — CLAUDE.md is the primary
knowledge store (QUAL-14) and these sentences are why a maintainer would *not* look.

- ~~CLAUDE.md: the 900s stall bound "sits deliberately above every narrower timeout it backstops" —
  false, see P66.8.~~ **Done 2026-08-16** with P66.8. The claim lived in `internal/config/config.go`'s
  `DefaultMaxTurnStallSec` comment and `docs/configuration.md` as well as CLAUDE.md; all three now
  state the true relation ("above every *per-call* bound") and the condition under which a larger
  aggregate is admissible.
- CLAUDE.md: write/execute tools serialize via `sync.RWMutex` — it is a plain `sync.Mutex`, and the
  guarantee is narrower than the doc implies (ARCH-13).
- `buildChatSystem`'s doc comment claims equivalence with the daemon's `effectiveSystem` — false, see
  P66.13.
- `internal/tui/view.go:305-312` still asserts the pre-P35.13 claim that `prompt_eval_count` is a
  cache-hit delta. P35.13 corrected the code; this comment survived and is now wrong in the *opposite*
  direction, telling a maintainer the context meter "understates how full the context window is" when
  on native Ollama it is accurate — and proposing remediation that should not be done (LLM-09).

Closes ARCH-13, LLM-09, and the doc half of P66.13 (P66.8's doc half is closed). Priority: Tier 2 — S.

### P66.25 — Trust grants are permanent and content-blind (SEC-07, refiled from P66.5)

**Filed 2026-08-16**, carved out of P66.5 rather than left as an unhonored "fold in if cheap" clause.
P66.5 shipped the inverted freeze list and with it a **well-defined security-relevant subset** of the
config — which was SEC-07's missing prerequisite, and is why this is now a coherent item instead of a
vague one. What did not ship is the re-prompt.

A trust grant (`aegis trust`, `internal/workspacetrust`) is recorded once per directory and never
re-examined. It says "this path is trusted", not "this *content* is trusted". So a `git pull` that
adds a `hooks:` block, flips `security.*`, or introduces a `commands:` override re-prompts nothing:
the operator approved a directory weeks ago and silently inherits whatever the repository has become
since. P66.5 closed the untrusted case completely; this is the trusted-then-changed case.

The shape is a content fingerprint over the security-relevant subset, stored with the grant, checked
on load, re-prompting when it moves. **What makes it Tier 2 rather than trivial** — and what stopped
it shipping inside P66.5 — is that the fingerprint has to cover `.aegis/.env` to be honest, and
P66.1 deliberately resolves `.env` *before* any project-controlled file is read. Honouring both means
either inverting P66.1's ordering (reading and parsing project config ahead of the trust decision,
which is the ordering P66.1 exists to prevent) or accepting a documented hole where the re-prompt
covers `config.yaml` but not `.env`. **Pick one and write down why** — an undocumented partial
fingerprint is worse than either. It also changes the `workspacetrust` store format and the
`Trust`/`IsTrusted` signatures used from `internal/cli` and `internal/server`, so it is a migration,
not an edit.

Closes SEC-07. Priority: Tier 2 — M. Sequence after P66.5 (shipped); read P66.1's record in
[releases.md](releases.md) first, because the `.env` ordering is the whole difficulty.

### P67.1 — Tool-result caps are per-call, and a parallel round multiplies them

`internal/tool/builtin/truncate.go` carries the posture table for every tool result — which end
survives, what happens to the remainder, how many notice bytes are reserved out of the cap — and
every cap in it is **per call**. The table was written when a round was one result at a time. It no
longer is: `Engine.runTools` (`internal/engine/engine.go:1769`) dispatches up to `maxParallelTools`
concurrently, so a round of N read tools can each land at its own cap and produce N× the intended
context bite inside a single user message. Nothing anywhere bounds the aggregate.

The fix is a round-level budget layered *above* the existing caps, not a change to them: after the
round's results are collected and before they are appended, if the combined size exceeds the budget,
spill the largest results to `<workspace>/.aegis/spill/` — the mechanism already exists and already
hands back a `read_file`-reachable path — until the total fits. Evaluate each round independently; a
large result in this round and another in the next are both fine and neither should be touched.

Two details worth pinning with the test rather than discovering later: notice bytes are reserved out
of the cap, so a spilled result's replacement notice must be counted against the round budget too;
and the spill must select by size, so a round of one huge result and four small ones spills the one,
not all five.

Priority: Tier 2 — S. No dependency. The smallest item in either open batch.

### P67.2 — The prompt has a size invariant and no stability invariant

`TestEffectiveSystem_localProfileBudget` fails the suite when the local base prompt crosses
`localBasePromptCeilingTokens`, and CLAUDE.md states the rule it enforces: raising the ceiling is
allowed, raising it silently is not. There is no equivalent guard on the axis that costs more per
turn. Nothing stops a *volatile* value — a timestamp, a cost figure, a changing repo-map digest, a
count that moves with session state — from being assembled into the system prefix, where it breaks
the prompt cache on every single turn and shows up only as unexplained prefill cost.

Build the prompt from **named sections** with two constructors: a default that is computed once and
memoized for the life of the conversation, and an explicitly-named volatile one that recomputes per
turn and **takes a written justification as a required argument**. Then a test computes every section
twice and fails on any that differs without having been declared volatile. The naming does most of
the enforcement; the test catches the rest.

This is the same shape as the ceiling test on a different axis, and it composes with P66.7 — a
context-file cap is easier to reason about when the rest of the prefix is known to be stable.

Priority: Tier 2 — S-M. No dependency.

### P67.3 — Provider requests carry no purpose, so one retry policy serves every caller

`internal/provider/retry.go` applies a single policy to everything that passes through the adapter
seam. Aegis makes far more kinds of call than that policy can be right for at once: the user's turn,
compaction summaries, the guard's second pass, debate roles, swarm sub-agents, the tool-call probe,
cron jobs. On a capacity or rate-limit error they all back off identically, including the ones no
human is waiting on and whose failure is invisible.

Thread a **call-purpose tag** through the adapter seam and key policy on it. The immediate payoff is
retry: background work fails fast instead of amplifying load during exactly the window when the
backend is already struggling, and the foreground turn is then free to retry harder than a
one-size policy allows. Default new purposes to the conservative setting and make opting in explicit,
so a call path added later does not silently acquire aggressive retry.

The tag is worth more than the retry change alone. **P67.6 needs it** — a compaction trigger that
fires on the main conversation must not fire for a sub-agent or an analysis-only caller, and the
purpose tag is the correct discriminator for that. It would also give `internal/cost` a spend
breakdown by call class, which today it cannot produce.

Keep the existing `Retry-After` clamp at `MaxDelay` (`internal/provider/retry.go:86`) exactly as it
is. It is what keeps provider backoff inside the 900s `MaxTurnStall` bound without the retry path
needing heartbeats at all, and it should not be "fixed" into honoring the header unbounded.

Priority: Tier 2 — S-M. No dependency. Enables P67.6.

### P67.4 — A failed tool call leaves its siblings running to completion

In a parallel round, `Engine.runTools` runs every call to completion regardless of what its siblings
did. A round of four builds where the first fails still pays wall-clock for the other three, and
their results are appended to a conversation the model is about to redirect anyway.

Derive a per-round context from the turn context and cancel it on the first error, so sibling
subprocesses die promptly while the turn itself continues normally. The parent/child split is the
whole point: cancelling siblings must not cancel the turn.

Two things to decide rather than assume. **Which failures qualify** — a `read_file` on a missing path
is a normal negative result and must not kill the round, while a failing `shell` usually should; the
capability classification the scheduler already computes (`Engine.serializeTool` →
`tool.EffectiveCapability`) is the natural place to hang that policy. And **what the cancelled
siblings report back** — the honest wording already exists in `interruptedMaybeRanText`, and the P65.1
reasoning behind it applies unchanged here: a cancelled call that had already started may have landed
its effects, and telling the model it did not run invites a destructive re-run.

Shortening doomed rounds also shortens the aggregate wait that `MaxTurnStall` backstops, which is a
second, smaller reason to want it.

Priority: Tier 2 — S. No dependency.

### P67.5 — Recall re-injects what it already injected, and says nothing about age

`internal/memory/relevance.go` scores entries by TF-IDF over an mtime/size-cached corpus. The scoring
is the right call for a local-first tool — no model round-trip, no cost — and this item does not
change it. Three behaviors *around* it are missing:

- **Already-surfaced dedupe.** Nothing filters entries injected on earlier turns of the same run, so a
  top-scoring entry is re-injected every turn it keeps winning, spending the entry budget on context
  the model already has.
- **Freshness.** `FormatEntries` renders content with no indication of age. The mtime is already read
  to key the cache (`cachedEntries`), so threading it through to the rendered entry costs one struct
  field and no extra I/O — and a memory's age is often the thing that decides whether to trust it.
- **Reference-vs-gotcha bias.** An entry that documents how to use a tool is near-useless when the
  model is already using that tool successfully; an entry that records a *gotcha* about that same
  tool is most valuable at exactly that moment. Bias scoring toward the latter when the tool in
  question appears in the run's recent tool calls.

The dedupe should filter before scoring, not after, so the entry budget is spent on candidates that
can actually be used.

Priority: Tier 2 — S. No dependency.

---

## Open Work — Tier 3

**Status: 7 filed items** — three from the P66 review batch and four from the P67 external-source
reading, each larger or sequence-dependent rather than urgent. P62.9, P65.2's prompt half and P65.3's
local half all moved to
[Verification Work](#verification-work) — in each case the code is already shipped and what remains
is a live-run result, not a design or implementation task.

### P66.13 — `aegis chat` re-derives what the daemon centralizes, and every copy has drifted

The dominant defect shape in this codebase — *a mechanism built for one path that a second path
silently bypasses* — with the CLI as the second path. Four instances of one root cause:

- **QUAL-01** — `internal/cli/chat.go:274` builds a **bare** `permission.New(...)`. The daemon's
  `buildGate` (`internal/server/engine_build.go:162-224`) stacks five layers. `permission.rules` deny
  rules and `security.egress_then_write` are therefore silently inert under `aegis chat`, and
  `internal/cli/dryrun.go` has no gate at all. `cli/worker.go:174`'s own comment names this exact
  bypass as the one P10.1 closed — `worker.go` was fixed and `chat.go` was not.
- **QUAL-02** — `buildChatSystem` (`chat.go:871`) claims in its doc comment to be "equivalent to the
  daemon's `effectiveSystem`". It omits `<deferred_tools>` **entirely**, so the 26 deferred tools the
  whole P62.6 line is about are undiscoverable via `tool_search` on the CLI path — a pure capability
  loss with the token saving already banked. It also skips the P25.6 local repo-map cap, on the path
  that *is* the local-model path.
- **ARCH-06** — `aegis chat` ignores `max_iterations`, `loop_threshold`, `redact_secrets`, the output
  guard and hooks.
- **QUAL-06** — `builtin.Options` is a 27-field struct filled differently at all five call sites; the
  CLI omits `Commands`, so the entire `toolpath`/ripgrep contract is inert there.

Fix by extraction, not by patching each: pull `buildGate` into a constructor both the daemon and
`chat.go` call, and do the same for the cost limits and `builtin.Options`. Emit `deferredToolsBlock`
from `buildChatSystem` or stop deferring on that path. `newChatCmd` is a 683-line function wrapping a
615-line `RunE` closure holding both bugs (QUAL-03) — splitting it far enough to make both testable is
the **enabling refactor**, not a finding in its own right, which is why this is Tier 3 and M-L rather
than a quick fix.

**Ship the invariant test, not just the fix:** every production site that builds an engine either
stacks the full gate or states in a comment why it does not — the same grep-the-source shape as
`TestEveryRegisterCallSiteDecidesTheLocalProfile`, which is the instrument this class of defect
actually responds to.

Closes QUAL-01, QUAL-02, QUAL-03, QUAL-06, ARCH-05, ARCH-06. Priority: Tier 3 — M-L, sequence-dependent.

### P66.14 — Two compaction thresholds that disagree, and a calibrator that never fires

Four findings in the token-accounting path, grouped because they share one seam and fixing them
separately would mean touching it three times:

- **LLM-02** — P59.1's completion-sized compaction trigger is discarded one layer down.
  `engine.compactionTrigger` (`engine.go:495`) reserves room for the completion;
  `compaction.Summarizer.shouldCompact` (`compaction.go:243`) uses a flat 20%-free rule and never sees
  `maxTokens`. At a 4,096 window the engine triggers at 2,048 and the summarizer refuses until 3,277 —
  so summarization finally happens with 819 tokens left for a completion the request asked 32,768 for.
  `SetEstimateCorrection` exists precisely because these two gates must not disagree; the argument was
  never applied to the trigger itself. Fix with one shared trigger function taking `(window,
  maxTokens)` used by both.
- **LLM-03** — the P62.4 calibration is inert on the OpenAI-compat path. `afterTurn`
  (`engine/compact.go:446`) gates on `PromptEvalDurationMS > 0`, which only the native Ollama adapter
  sets — while `docs/providers.md` recommends `provider.default: openai` + `:11434/v1` for Ollama.
  **Every user following the documented configuration runs the whole session on the uncorrected
  20-33% undercount.** Gate on a positive backend identification instead; `sharedContextWindow` is
  already one.
- **ARCH-07** — `SetEstimateCorrection` pushes a per-run overhead into a Summarizer built once per
  *server* and shared by every session, which that type's own doc comment argues per-session data
  cannot live on.
- **PERF-03** — `compactionGuard.requestOverhead` is snapshotted once in the constructor
  (`compact.go:260`), but `tool_search`'s `reg.Load` mutates the exposed set mid-run, silently
  undercounting the compaction trigger by up to 593 tokens for a single tool against a 4,550 budget.

Closes LLM-02, LLM-03, ARCH-07, PERF-03. Priority: Tier 3 — M.

### P66.15 — Sweep the two packages this review did not read

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

### P67.6 — Compaction fires on context pressure only, never on cache temperature

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

Priority: Tier 3 — M. Sequence after P67.3 (needs the purpose tag) and P66.14 (same machinery).

### P67.7 — Tool calls are dispatched only after the whole model turn has streamed

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

Priority: Tier 3 — L. Sequence after P67.4. The largest payoff of the batch on local models, where
generation latency dominates.

### P67.8 — Read-only shell classification is per-binary, so useful commands stay execute-gated

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

### P67.9 — Terminal capability is inferred from `TERM`, not asked

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

## Open Work — Tier 4

**Status: 20 open** — 9 pre-existing (all blocked or explicitly parked, none with a fired trigger),
6 from the P66 review batch and 5 from the P67 external-source reading. (This line read "13 open …
plus 4 from P66" until 2026-08-16; it had not been updated when P66.23 was filed, and the Status
block above was the correct count. It moved to 20 later that day when **P66.26** was refiled out of
P66.9.)

The P66 entries here are **deliberately grouped grab-bags**: each collects the Low-severity residue of
one review domain. They are filed so no finding is lost, not because any of them should be scheduled.
Take one only when already working in that file. The P67 entries are a different kind of parked: each
is a capability Aegis does not have and nobody has asked for, filed with the specific trigger that
would make it worth building.

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

**Status: 6 open.** Every item here has its code already written and merged — nothing below is a
design or implementation task. Each is closed by running a live-model harness and recording the
result the item's closure condition names, not by writing more code. They are **not tiered**:
tiering answers "how urgent is this build," and there is no build left to prioritize. **Five of the
six share one harness** (a local model driving `aegis chat --skill threat-modeling` /
`TestLiveWorkflow`) and are listed first so one live-tier setup can answer all five in a sitting; the
sixth (P62.8) is blocked on hardware, not on scheduling. P66.22 is the newest and is the only one
with a *sequencing* constraint — it must run after the P66 fixes it measures, not before.

### P66.22 — The LLM-tier findings are all estimates; one live run converts them to measurements

The P66 review never ran a live model. **LLM-01, LLM-02, LLM-03, LLM-10 and ARCH-04 are all claims
about runtime behaviour against a local model, argued entirely from source.** The arbitration upheld
all five and they are well-argued — but CLAUDE.md is emphatic that this class of claim is settled by
measurement, and this document has twice recorded a fixed instrument *inverting* an already-acted-on
verdict.

One `TestLiveWorkflow` run against `qwen3:14b-32k` answers all of them, and it is the same harness
P38.1, P62.9 and P65.2's prompt half already need — so this costs no additional setup if scheduled
with them.

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

Run it **after** P66.7 and P66.14 ship, not before: those change three of the five numbers, and
measuring the pre-fix state answers a question nobody will have afterwards. Use `-count=1` — a
re-run without it replays Go's cached verdict, which this document has been caught by before.

Priority: Verification — one run, five answers. Sequence after the Tier 2/3 items it measures.

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

Priority: Verification — every load-bearing harness fix the re-tests have root-caused has shipped
(P39.5-P39.18, P47.1-P47.9, P52.12, P57.1). This item stays open only as the conformance **umbrella**,
closeable once a live built-in drive is confirmed to reach a verify-clean suite unattended, in one
invocation, on a local model. No code work remains; it is live-run tracking.

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

**What would close it:** the same task at n≥10 per arm, or a task whose edit is unambiguous enough
that a single run means something, plus a default-prose control for the prose-attributable failures
above. Both are runs, not code.

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

Priority: Verification — real value, unblocked, code already built, gated on live evidence rather
than on design.

### P65.3 — The summarizer and the guard ride the conversation's cache posture, and neither is reused

**Mechanism shipped 2026-08-15.** Question 1 (cloud) is answered by code-reading, not inference: the
compaction summarizer (`compaction.go:684`) and the output guard's validation pass (`guard.go:194`)
both call `Stream` on the *same shared* Anthropic adapter instance the conversation uses
(`s.modelAdapter`), which emitted `cache_control` breakpoints unconditionally whenever prompt caching
was on — so both were billed a cache **write** for a one-off prompt with no possible matching read.
Worse than the roadmap's framing: neither call site ever read `ev.Usage` off the stream, so that cost
wasn't just unattributed, it was invisible to Aegis entirely. Confirmed, not refuted — promoted and
built same day.

**Fix:** `provider.Request.SuppressCache` (`provider.go`, alongside `NumCtx`/`Format`) — set `true` by
the summarizer and the guard, honored by the Anthropic adapter (`cache := a.cache && !req.SuppressCache`,
`anthropic.go:313`) to skip breakpoints on `System`, `Tools`, and the last message block; every other
adapter ignores the field, same pattern as `NumCtx`. `TestPromptCachingSuppressedPerRequest` pins it.
Debate roles were *not* touched — the roadmap named them as another rider on the shared adapter, but a
debate role's prompt is not necessarily one-off the way a summarizer/guard call is, and no measurement
established it needs the same suppression; leaving it is the narrower change.

**What remains — Question 2 (local), still gated on live evidence:** does a summarization or guard call
between turns measurably raise the *next* turn's prefill on a local backend? The P62.2 re-measurement
apparatus answers this, `-count=1` discipline included, but needs a reachable local model server this
pass did not have. The dropped-`Usage` gap found while answering Question 1 (compaction/guard discard
`EventDone.Usage` entirely rather than attributing it) is a separate, adjacent item — not required to
close this one, worth its own line if someone wants session cost totals to include it.

**Promote when:** a live local-tier session is available — same harness as P38.1/P62.9/P65.2.

Priority: Verification → mechanism closed; the local half stays open behind the same live-tier gate
as the other three items above.

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
