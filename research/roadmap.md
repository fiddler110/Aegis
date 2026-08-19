# Aegis Capability Roadmap

**Last updated:** 2026-08-19. This document tracks only **open** work and what's next. For
shipped-feature history, batch origins, pass-by-pass narrative, refutation records and full design
rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading with a
`Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so keep it when
adding items.

---

## Status

**33 open items: 28 build + 5 verification-only, plus eight shipped same-day (P71.1, P71.3, P71.4,
P71.5, P71.9, P71.10, P72.2, and the doctor-check half of P71.4).** The **P71 batch — twelve items
filed 2026-08-19 — re-filled three empty tiers**, and a same-day build pass closed six of them plus
one from the follow-up P72 pair, the first time in this document's history a filed batch and its
build have landed in one sitting. Before the P71 batch this line read "26 open items: 21 build (Tier
4 only) + 5 verification-only. Tier 1, Tier 2 and Tier 3 are all empty", and the standing advice was
that the next build item "does not exist yet and has to be *found*". It was found the way that note
predicted: not by promoting a Tier 4 entry, but by a fresh pass producing new work — and then, unlike
every prior batch in this document, mostly built the same day rather than only planned.

**What shipped, in one place — see each item for the full record.** **P71.1**: DuckDuckGo's
rate-limit page is now detected and reported as an error instead of "no results found". **P71.10**:
`deep-research` activation un-defers `web_search`/`web_fetch` so a local-profile session doesn't
reach for `shell` instead. **P71.5**: `web_fetch`'s output cap now scales with the resolved context
window via a new `tool.WithContextWindow` context value threaded from `Engine.toolCtx`. **P71.9**: the
deep-research skill's working-file update is now unconditional and enforced every round (a text-only
`SKILL.md` edit). **P71.3**: `web_fetch`/`web_search` retry transient failures with backoff, never a
4xx. **P71.4**: search results now name their serving backend, and a new `aegis doctor` check
(`doctorSearchCheck`) catches a misconfigured provider before any search is attempted. **P71.2**:
partially addressed — `docs/configuration.md` now recommends Tavily over Brave (whose free tier ended
in February 2026); the actual second-backend fallback ladder is still open, deliberately, since its
candidates remain untested. **P72.2** (filed and shipped in the prior sitting): `/models` shows live
pulled Ollama models instead of a static cross-provider catalog. **Left open on purpose**: **P71.8**
(deep-research phasing) and **P72.1** (dynamic boot/model-switch context sizing) are both real design
work, not wiring, and stayed unbuilt this sitting for that reason — see their entries for why. Every
shipped item above has a live-verified test or a live probe run against this machine recorded in its
entry; none of this is asserted from reading the diff.

**The P71 batch is an evaluation of `/research` and the web-search stack**, prompted by a user report
that a `/research` run on a local 9B "either timed out or didn't produce any real results". Every
claim in P71.1–P71.12 is backed by a measurement or a live-run observation taken on 2026-08-19
against HEAD `898a2c5`; the two runs behind it are described in the batch note below. Four items are
acute and two of them are Tier 1.

**Read this before taking any P71 item: the batch has one root cause and eleven consequences.**
`web_search` reports a DuckDuckGo rate-limit block as `"no results found"` with `IsError: false`
(**P71.1**) — a 200 response carrying a challenge page, after a measured **two queries**. Everything
downstream is a model reacting rationally to being told the web is empty: it invented URLs (7 of 15
fetches 404'd), and in the second run it concluded the tool was broken and **hand-rolled a Bing
scraper in PowerShell through `shell`**, bypassing the SSRF dialer, `trust.Wrap`'s provenance marker,
the injection scan and the output cap in one move (**P71.10**). Fix the misreport first. Several of
the other items are worth much less once the model is told the truth, and at least one of them
(**P71.2**) cannot even be triggered without the detection P71.1 adds.

**The second finding is independent of the first and is arithmetic, not behaviour.**
`CompactionTrigger(16000, 8192)` is **8,000 tokens**; `web_fetch`'s default output cap is 20,000
characters, about **5,000 tokens**. At the shipped local `context_window: 16000`, reading one source
consumes 62% of the compaction budget, so a research run cannot read two pages without compacting
(**P71.5**). The 16k live run took **25 compactions across 42 tool calls** and produced a report with
**zero inline citations**; the 32k control took **4 compactions** — and then stopped after Round 1
with no report at all, because `--skill`'s drive-to-completion keys on `<!-- PENDING -->` markers that
deep-research never writes (**P71.8**). **Raising the window fixes the thrash and exposes a second
bug underneath it**, which is why P71.8 is filed separately and ranked below the two acute fixes.

**One P71 measurement is negative and is filed to stop it being re-derived.** Main-content extraction
for `web_fetch` — dropping nav/header/footer before `htmlToText` — looked like an obvious context win
and is worth only **3–12% (~1.2–1.5 KB) per page** on real documentation, because the existing
converter already takes 66 KB of HTML down to 11 KB of text. It is **P71.12**, Tier 4, explicitly do
not schedule. This is the method note this document keeps re-learning: measure the instrument before
acting on the intuition.

Nine shipped on 2026-08-18, in three sittings: **P66.15**,
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

- **Tier 1:** 0 — **P71.1** and **P71.10** both shipped 2026-08-19, the same day they were filed.
  Before them the tier had been empty since **P69.6** shipped 2026-08-17, and **P66.5** before that
  closed the last exploitable-today finding of the P66 review.
- **Tier 2:** 2 — **P71.2** (partially addressed: the docs fix shipped, the cross-provider fallback
  ladder is still open) and **P68.1** (the instrumentation gap the live tier found), which remains
  deliberately off the ranked list because it travels with the parked live-tier row. **P71.3**,
  **P71.4**, **P71.5** and **P71.9** all shipped 2026-08-19, same day filed. (**P66.25**, **P67.2**,
  **P67.3**, **P67.4** and **P67.5** shipped 2026-08-17; **P66.11**, **P66.12**, **P66.21** and
  **P67.1** shipped 2026-08-16; **P72.2** shipped 2026-08-19.)
- **Tier 3:** 2 — **P71.8** (deep-research declares no phases, so it runs single-context and
  `--skill` cannot drive it to completion) and **P72.1** (`context_window` is sized once by hand; no
  boot-time or model-switch fit — filed 2026-08-19). Both stayed unbuilt this sitting on purpose:
  each needs a design decision (a phase plan; a cold-start policy), not a wire. The tier was emptied
  on 2026-08-18 when **P66.15**, **P67.6**, **P67.7**, **P67.8**, **P67.9** and then **P70.4** all
  shipped. (**P66.13** shipped 2026-08-17; **P66.14** 2026-08-16.)
- **Tier 4:** 24 — four from P71 (**P71.6**, **P71.7**, **P71.11**, **P71.12**), plus **P66.17**,
  **P66.18**, **P66.19**, **P66.20**, **P66.23**, **P66.26** (PERF-02, refiled), **P70.3**, the five
  from P67 (**P67.10**-**P67.14**), and the nine pre-existing: **P65.4**, **P65.5**, **P64.4**,
  **P64.5**, **P61.7** (remainder), **P60.3**, **P52.14**, **P25.9**, **P63.10**.
- **Verification:** 5 — **P66.22** (two conditions left, both blocked on P68.1), **P38.1**,
  **P62.9**, **P65.2** (prompt half, blocked on P68.1), **P62.8**. (**P65.3** closed 2026-08-16:
  both its questions are answered.)

**What to do next.** **P71.1, P71.3, P71.4, P71.5, P71.9 and P71.10 all shipped 2026-08-19**, the day
they were filed — see the Status block above for the one-line summary of each, and each item's own
entry for the full record. What's left: **P71.2**'s cross-provider fallback ladder (the docs half
already shipped), then **P71.8** (deep-research phasing, now that its **P71.9** prerequisite is in)
and **P72.1** (dynamic context sizing) — both real design work, filed with the open questions each
needs answered before either is a wire rather than a decision.

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

## The P71 batch — how it was measured (2026-08-19)

Recorded here rather than in the items, so no P71 entry has to restate it and so the next reader can
tell a measurement from an inference. **Everything below is reproducible; nothing in P71.1–P71.12
rests on reading code alone.** Tree: HEAD `898a2c5`, clean. Host: the machine in
`aegis_machine_specs` — Ryzen 3800XT, RX 7900 GRE 16 GB VRAM, 16 GB system RAM.

#### The two live runs

Both used `aegis chat --skill deep-research --yes --mode build --render off --max-turns 40`, in a
fresh trusted git workspace outside this repo, on one identical prompt: set up a new Azure tenant —
tenant/subscription foundation and identity, public ingress architecture, and which tenant security
capabilities to enable — aligned to CAF, Azure landing zones, the WAF security pillar and MCRA.

| | Run A | Run B |
|---|---|---|
| Model | `aegis-qwen35-9b:16k` | `aegis-qwen35-9b:32k` |
| `context_window` | 16000 (shipped global config) | 32000 (`AEGIS_PROVIDER_CONTEXT_WINDOW`) |
| Elapsed | 646 s | 267 s |
| Tool calls | 42 | 39 |
| Compactions | **25** | 4 |
| `web_search` calls | 19 (8 returned nothing) | 10 (4 returned nothing) |
| `web_fetch` calls | 15 (**7 × 404**) | **0** |
| `shell` calls | 2 | **21** (20 × `Invoke-WebRequest`) |
| Inline `[n]` citations | **0** | 18 |
| Working-file updates | 1 (placeholders only) | 1 |
| Outcome | full report, uncited, 2 of 5 URLs wrong | **no report** — stopped after Round 1, exit 0 |

**Both runs are failures, and they fail differently, which is the finding.** Run A only produced a
report because its compaction thrash kept it talking past the point Run B stopped. Run B is the
cleaner run on every process metric and delivered nothing, because `--skill`'s drive-to-completion
has nothing to continue on (**P71.8**). Do not read the table as "32k is better"; read it as two
independent bugs that mask each other at different window sizes.

#### The bench measurements

Taken through the production types in `internal/tool/builtin` (temporary in-package tests, since
removed — re-create them from this section rather than trusting a stale copy):

- **DuckDuckGo throttling.** Twelve research-shaped queries issued back-to-back through
  `searchTool.Execute`: q01 and q02 returned 8 results each in 976 ms and 734 ms; **q03 through q12
  returned zero, in ~130 ms each**. The zero-result responses are HTTP 200 with a ~14.2 KB
  anomaly/challenge body. Probing `fetchTool.get` directly against both
  `html.duckduckgo.com/html/` and `lite.duckduckgo.com/lite/` over four rounds returned that same
  page from **both** hosts every time — the two endpoints share one bucket (**P71.2**). A query 60 s
  later returned a 37 KB body parsing to 10 results, so the block is roughly a one-minute cooldown.
- **The compaction arithmetic.** `tokenest.CompactionTrigger(window, 8192)` = 8,000 / 22,208 /
  52,608 / 111,411 at windows of 16,000 / 32,000 / 64,000 / 131,072. `web_fetch`'s default cap is
  20,000 chars ≈ 5,000 tokens (**P71.5**).
- **The boilerplate share, which is the batch's one negative result.** Four `learn.microsoft.com`
  pages: raw HTML 64–98 KB → `htmlToText` output 11–38 KB → non-content head/tail **1,218–1,446
  bytes, 3–12%** (**P71.12**).
- **A transient DNS failure, caught by accident.** A `web_fetch` of `learn.microsoft.com` returned
  `lookup learn.microsoft.com: no such host` while `nslookup`, `curl` and a direct
  `net.DefaultResolver.LookupIPAddr` for the same host from the same machine succeeded seconds later.
  Not reproducible on demand — which is the point, and the argument for **P71.3**.

#### Three things this batch checked and cleared

Filed so nobody re-investigates them:

- **`/research` does not require the skill to be enabled.** `deep-research` was `[disabled]` in
  config when the user's failing run happened, and that is **not** a cause: `cmdResearch` →
  `activateSkill` → `handleActivateSkill` "turns on a dormant embedded built-in skill for this
  session only", independent of the config flag. The skill body was preloaded into the prompt in both
  live runs and in the user's.
- **`tool_search`'s exposure survives compaction.** `reg.Load(names...)` mutates the session's
  registry clone, so a tool loaded on turn 3 is still in the exposed schema set on turn 30 even after
  the "now callable" tool result has been summarized away. Run B's zero `web_fetch` calls are a model
  *choice*, not a lost capability — which is why **P71.10** is written as an exposure/incentive
  problem rather than a state-loss one.
- **The HTML-to-text converter is not the problem.** 66 KB of HTML to 11 KB of text is most of the
  available win already, and `htmlToText` correctly drops `script`/`style`/`noscript`. See P71.12.

#### Method note this batch adds

**Two of the four `/research` failure hypotheses that looked strongest from reading the code were
wrong**, and only the live runs separated them: the disabled-skill flag (cleared above) and
compaction-drops-the-loaded-tool (cleared above) both had plausible mechanisms and neither was
happening. The two that survived — a rate limit reported as success, and a per-fetch cap larger than
the compaction trigger — are both *arithmetic or control-flow facts visible in the source*, which
nobody had checked because the interesting-looking hypotheses were elsewhere. This document already
says to check the instrument before acting on the intuition; the P71 corollary is narrower: **when a
harness "just doesn't work", run it once with the tool calls printed before forming a theory.** The
run cost eleven minutes and invalidated half the theory.

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

## Up next — what is left of the web-research stack

**Rewritten 2026-08-19 (second time that day), after a same-day build closed five of the six rows
this table had.** The previous version was a six-row dependency chain — P71 is one root cause with
eleven consequences, so order mattered — and it held: **P71.1, P71.5, P71.10, P71.9 and half of P71.2
(the docs correction) and P71.4 all shipped in the order the chain specified**, the first time in
this document's history a filed batch and its build landed in the same sitting rather than across
several. Full detail of what shipped is in each item's own entry and in the Status block above; this
table is now what's left, not the whole chain.

| # | Item | Tier / size | Why now |
|---|------|-------------|---------|
| 1 | **P71.2** — a real second search backend below DuckDuckGo | Tier 2 / M | The one row not fully closed. The docs half shipped (Tavily recommended over Brave); the fallback-ladder half is still open because its candidates (Mojeek, Marginalia, Startpage, a Bing scrape) are untested, and this item's own text says not to add one without checking it against **P71.1**'s challenge-page detector first. |
| 2 | **P71.8** — deep-research declares no phases, so `--skill` cannot drive it | Tier 3 / M-L | Now genuinely unblocked rather than sequence-dependent: its three prerequisites (**P71.1**, **P71.5**, **P71.9**) all shipped 2026-08-19. What's left is a design pass — the phase decomposition, the cold-start question of what the first phase's context looks like — not a bug fix. |
| 3 | **P72.1** — `context_window` is sized once by hand; no boot-time or model-switch fit | Tier 3 / M-L | The general form of the arithmetic P71.5 fixed per-call: `aegis models --fit` already has the exact math, but nothing calls it at boot or on `/model` switch, the wizard never asks for a budget, and there's a real chicken-and-egg problem (a window is needed to load a model; weights are only measurable once loaded). Design work, not a wire — see the item for the three confirmed gaps. |
| 4 | **The live-tier remainder** (P66.22, P38.1, P62.9, P65.2) — *parked by choice, 2026-08-16* | Verification | Unchanged, and still last for the same reason: **the user parked it**, not a dependency. It is also no longer one sitting — **P38.1** needs permission to launch an unattended auto-approving agent, **P62.9** needs a *better task* rather than more runs of the current one, and **P65.2**, **LLM-03**, **LLM-10** and **ARCH-04** need what the tier cannot show: a surviving data dir and `aegis sessions trace <id>`, which is **P68.1**. Take P68.1 first whenever this row is picked back up, or the sitting produces the same unreadable evidence again. Record in [releases.md](releases.md). |

**Notes on what shipped and what didn't.**

**The forced order held, and is worth recording as a confirmed pattern rather than restating.** Six
items were in dependency order because taking them out of order would waste work — retrying a
misreport (P71.1) before believing the search result, phasing a drive (P71.8) whose evidence would be
unreadable without the acute fixes underneath it. Five landed in that order in one sitting; the sixth
(P71.8) is next precisely because its prerequisites are now satisfied, not because it moved up in
priority.

**The four Tier 4 P71 entries are still deliberately off this table** and none should be promoted
yet. **P71.6** (response caching) and **P71.11** (window-derived budgets) are both blocked on row 2
*by choice* — phasing changes the arithmetic under both, so setting them first fits a constant to a
regime about to change. **P71.7** (publication dates on results) wants a keyed provider to be the
default first, and neither Tavily's nor Brave's response schema was confirmed to carry a usable date
field when checked 2026-08-19 — see the item. **P71.12** (main-content extraction) is a filed
**negative** measurement: 3–12% per page, explicitly do not schedule.

**One item is deliberately off this list: P68.1** (Tier 2, S). It is what the parked row #4 needs
before it is worth re-running — the eval tier deletes the database holding the trace its own closure
conditions are written against. It travels with that row, so it is off the list while the row is
parked.

**This table outranks `scripts/roadmap-status.sh`.** That script reports open items in *document*
order, which is priority order only *within* a track — it cannot see a cross-tier ranking — and it
also cannot see that **P68.1** is deliberately off the list. Use it for repo state and for the parse;
use this table for what to take.

---

## Open Work — Tier 1

**Status: empty. P71.1 and P71.10 both shipped 2026-08-19**, the day they were filed out of the P71
evaluation of `/research` and the web-search stack — see their entries below for the full shipped
record. They were one event seen twice: `web_search` reporting a DuckDuckGo rate-limit block as
`"no results found"` with `IsError: false` (P71.1), and a model that consequently stopped trusting
the tool reaching for `shell` instead, because `LocalProfile` hid `web_fetch` and left the command
runner exposed (P71.10) — bypassing the SSRF dialer, `trust.Wrap`, the injection scan and the output
cap in one move.

**Before them: P69.6 shipped 2026-08-17**, the same day it was filed — see [Nothing planned a
resident set](releases.md#nothing-planned-a-resident-set-2026-08-17-p696). Before it the tier had
been empty since **P66.5** shipped (2026-08-16), closing the last of the findings the review
classified as exploitable on the day it landed: P66.2 (2026-08-15), then P66.1, P66.4, P66.3, P66.6
and P66.5. See [releases.md](releases.md) for what each landed and what was found while landing it —
several of those records correct the item they were built from, which is the part worth reading
before trusting [CodeReview.md](CodeReview.md) directly.

An item enters this tier when it is a real, currently-exploitable security or robustness gap that is
small and has no dependency.

<details>
<summary>P71.1 — the rate-limit misreport (shipped 2026-08-19)</summary>

### P71.1 — `web_search` reports a rate-limit block as "no results found" — SHIPPED 2026-08-19

**Filed 2026-08-19, from running `/research` rather than from reading it.** DuckDuckGo serves its
anomaly/challenge page as **HTTP 200** with a ~14.2 KB body. `fetchTool.get` therefore returns no
error, `parseDDG` finds no `result__a` node, `parseDDGLite` finds no external link, and
`searchTool.Execute` falls through to `internal/tool/builtin/web.go:171`:

```go
msg := "no results found"
if provErr != nil { … }
return tool.Result{Content: msg, IsError: provErr != nil}, nil
```

On the zero-config path `provErr` is always nil, so a throttled query is reported to the model as a
successful search over an empty web, indistinguishable from a genuinely unproductive query.

**Measured 2026-08-19**, twelve research-shaped queries issued back-to-back through
`searchTool.Execute` against the shipped user-agent:

| query | results | elapsed |
|---|---|---|
| q01 | 8 | 976 ms |
| q02 | 8 | 734 ms |
| q03 | 0 | 345 ms |
| q04–q12 | 0 | ~130 ms each |

**Two queries is the empirical ceiling.** The ~130 ms responses are the challenge page being served
from cache, not a search. The block clears after ~60 s (probed directly: a query 60 s later returned
10 results from a 37 KB body).

**The consequence is not a missing result, it is a model that stops trusting the tool.** Both live
runs of the deep-research skill on 2026-08-19 (see the P71 batch note in [Status](#status)) reacted
to the silent empty result by improvising:

- The 16k run began inventing plausible `learn.microsoft.com` URLs to fetch instead of using search
  results — **7 of 15 `web_fetch` calls 404'd**, eventually tripping the P52.3 failure breaker.
- The 32k run concluded `web_search` was broken and **hand-rolled a Bing scraper in PowerShell**
  through the `shell` tool — see **P71.10**, which is the security half of the same event.

**Do:** detect the challenge response (no parseable results **and** the anomaly markers in the body)
and return `IsError: true` with a message naming the condition and the retry window — "search
provider rate-limited this client; retry in ~60s" — rather than "no results found". Keep "no results
found" for the case it actually describes: a parseable results page with zero entries.

**Closure condition:** a test fixture of the captured challenge page drives `searchTool.Execute` to
an `IsError: true` result whose content names the rate limit, and a fixture of a genuine zero-result
page still returns the non-error "no results found". Both fixtures committed, since the live page is
not reproducible on demand.

Priority: **Tier 1 — S.** No dependency. Real, currently-broken, measured, and the smallest fix in
the batch. Take it first: **P71.2**, **P71.3** and **P71.10** all mitigate consequences of this one
misreport, and are worth less until the model is told the truth.

**Shipped 2026-08-19.** `looksLikeDDGChallenge` (`internal/tool/builtin/web.go`) matches the challenge form's action endpoint and copy against a live-captured excerpt; `duckDuckGo` now returns a `blocked` flag alongside results, and `Execute` reports `IsError: true` with the retry message rather than "no results found" when every attempt was the challenge page. Verified live against an actual throttled DuckDuckGo response. New tests: `TestLooksLikeDDGChallenge` pins the detector against both a captured challenge page and a genuine results page.
</details>

<details>
<summary>P71.10 — un-defer the web tools for deep-research (shipped 2026-08-19)</summary>

### P71.10 — Deferring the web tools routes the model around every guardrail on them — SHIPPED 2026-08-19

**Filed 2026-08-19, observed live.** `builtin.Options.LocalProfile` auto-enables whenever
`provider.base_url` resolves to loopback (`config.go:1082`, `LocalPromptProfile`), and moves
`web_fetch`/`web_search` — with `git_pr` and `security_scan` — from the always-exposed set to the
deferred set (`internal/tool/builtin/builtin.go:227-241`). `shell` stays always-exposed. So on every
local-model session the model can see a general-purpose command runner and cannot see the HTTP
client.

**What the 32k live run did with that.** After `web_search` returned "no results found" (P71.1), the
model stopped using the web tools entirely and issued **21 `shell` calls, 20 of them PowerShell
`Invoke-WebRequest`** — first scraping `bing.com/search` for `learn.microsoft.com` hrefs with a
regex, then fetching documentation pages directly. Zero `web_fetch` calls in the whole run.

That path bypasses, at once, every control the fetch tool exists to apply:

- **`netblock.SafeDialer`** — no SSRF blocklist, no resolve-once-dial-the-literal-IP rebinding
  defence, no `CheckRedirect` hook.
- **`trust.Wrap`** — the returned HTML arrived as a plain tool result with no
  `<web_untrusted_output>` marker, so ~5 KB of attacker-controllable page content was presented to
  the model as trusted output. This is the exact provenance property P70.2/P70.4 were built to
  preserve elsewhere.
- **The heuristic prompt-injection scan** (`Search.ScanOutput`, FIND-04/FIND-12), which hangs off
  `trust.Wrap` and therefore never ran.
- **`TruncateHead` and the P64.3 posture**, replaced by whatever `Substring(0, 5000)` the model
  happened to write.

`internal/server/server.go:782` already warns that network policy "does not constrain the shell tool;
commands such as curl/wget/nc bypass it". This is that warning firing in an ordinary research
session, with no adversary — the model reached for the tool it could see.

**It also costs turns.** The 20 shell calls included repeated PowerShell parser errors on regex
escaping; a large fraction of the run's 39 tool calls went to reimplementing `web_fetch` badly.

**Do** (smallest first, and the first is probably enough):

1. **Un-defer `web_search`/`web_fetch` when a network-shaped skill is active.** The registry already
   supports per-session exposure — `tool_search` calls `reg.Load(names...)` on the session's clone
   (`internal/tool/builtin/toolsearch.go:44-62`) — so activating `deep-research` can pre-`Load` them
   the same way, with no profile change.
2. Reconsider the profile default for these two. The P25.6 rationale is per-turn schema tokens, and
   the two web schemas are small; the measured cost of deferring them is a wasted `tool_search` round
   in one run and a full guardrail bypass in the other.
3. Independently of both: a fetch performed through `shell` should not be *cheaper* in guarantees
   than one through `web_fetch`. That is the general form, it is larger than this item, and it is the
   thing `server.go:782` is really saying.

**Note the first `tool_search` call of the 16k run**, which is its own small finding: the model
searched the *tool registry* with a *research query* — `tool_search {"query":"Azure Cloud Adoption
Framework landing zones tenant setup best practices"}` — and got `security_scan` back. The deferral
indirection is not free even when it works.

**Closure condition:** a live local-profile research run reaches the web through `web_fetch`, with
zero `Invoke-WebRequest`/`curl`/`wget` shell calls, and every fetched byte carries its
`<web_untrusted_output>` wrapper.

Priority: **Tier 1 — S** for step 1, M for step 2. Depends on nothing; **P71.1** should land first,
because the misreport is what triggers the improvisation.

**Shipped 2026-08-19 — step 1 only** (step 2, reconsidering the LocalProfile default itself, and step 3, the general shell-vs-web_fetch guarantee gap, stay open — this closes the specific incentive that caused the live failure, not the two larger design questions it named). `preloadNetworkToolsForSkill` (`internal/server/engine_build.go`), the same shape as the existing `preloadPersonaTools`, un-defers `web_search`/`web_fetch` on the session's registry clone the moment `deep-research` activates (`handleActivateSkill`, `internal/server/sessions.go`) — scoped to a `networkShapedSkills` map rather than a general skill-level `tools:` mechanism, since deep-research is the only skill this is true of today. New tests: `TestPreloadNetworkToolsForSkillExposesWebToolsForDeepResearch`, `TestPreloadNetworkToolsForSkillIgnoresOtherSkills`.
</details>

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

**Status: 2 open — P71.2 (partial) and P68.1.** **P71.3, P71.4, P71.5, P71.9 and P72.2 all shipped
2026-08-19**, most the same day they were filed — see each entry below for the full record. **P71.2**
is partially addressed: `docs/configuration.md` now recommends Tavily over Brave (whose no-card free
tier ended February 2026), but the actual cross-provider fallback ladder is still open, deliberately
— its candidates remain untested. **P68.1** stays deliberately off the ranked list because it travels
with the parked live-tier row. (**P66.25, P67.2, P67.3, P67.4 and P67.5 shipped 2026-08-17**; P66.11,
P66.12, P66.21 and P67.1 shipped 2026-08-16; P66.7, P66.9, P66.10 and P66.16 earlier that day. Records
for all of them are in [releases.md](releases.md).)

**The note this tier carried until 2026-08-19 was that a new Tier 2 entry "now comes from a review
pass or a fired trigger, not from what is already filed."** That held: all five P71 entries came from
a live evaluation, and none was promoted from Tier 4. Five of them then shipped the same sitting they
were filed — also new for this document.

<details>
<summary>P72.2 — /models showed a static cross-provider catalog instead of what's actually pulled (shipped 2026-08-19)</summary>

### P72.2 — `/models` showed a static cross-provider catalog instead of what's actually pulled — SHIPPED 2026-08-19

**Filed and shipped 2026-08-19**, reported directly by the user as "/models seems broken" while
asking about local-only model switching. It wasn't crashing — `cmdModels` (`internal/tui/slash.go`)
returned `modelcatalog.Curated()` unconditionally: four Anthropic/OpenAI/Gemini entries plus four
generic Ollama *family* names (`qwen3`, `deepseek-r1`, `qwen2.5-coder`, `llama3.1`). None of those
four is a loadable tag — this machine's actual pulled models are `aegis-qwen35-9b:16k`,
`aegis-phi4-reasoning:16k`, `gemma4:12b`, etc. — so picking a catalog entry would 404 on the next
turn (`cmdModel`'s own doc comment already named this failure mode), and the list was dominated by
cloud providers a local-only user never asked for.

**Do:** query the daemon for what Ollama actually has pulled and show that instead, falling back to
the curated catalog exactly as before when the provider isn't Ollama or isn't reachable.

**Shipped, four pieces:**

- `ollamainfo.ListLocal` (`internal/ollamainfo/ollamainfo.go`) — a `GET /api/tags` client sibling to
  the existing `Digests`, returning name/family/parameter size/quantization/size per model. Excludes
  embedding-only models (`nomic-embed-text` reports `capabilities: ["embedding"]`, no `completion`) —
  listing one would be a guaranteed-broken picker choice, not a degraded one. A model with no
  `capabilities` field at all (an older Ollama server) is kept rather than excluded on missing data.
- `GET /models/local` (`internal/server/models.go`), wired in `server.go`. The daemon does this
  rather than the TUI because it already owns the `provider.base_url` connection; the client has no
  independent route to it. `Reachable: false` covers both "not Ollama" and "unreachable" — the
  client's fallback is identical either way.
- `Client.ListLocalModels` (`internal/client/client.go`) — thin wrapper, same shape as
  `ActivateSkill`.
- `cmdModels` now calls it with a 5 s timeout; on `Reachable && len(Models) > 0` it shows **only**
  the live pulled list via a new `localModelsToCatalog` adapter, matching the user's stated
  preference ("I don't want to focus on or use cloud models at this time") without a new flag. A
  pulled tag's `:16k`/`:32k` suffix (this project's own `aegis-*` convention) is shown as the
  picker's context label when present; otherwise it falls back to the quantization level, since
  `/api/tags` doesn't report context length for an unloaded model (only `/api/ps` does, for one
  that's currently resident).

**Verified live** against this machine's actual daemon (`GET /models/local` over the TLS loopback
listener, bearer-token authed): returns the 6 completion-capable pulled models, correctly excludes
`nomic-embed-text`. New unit tests: `TestListLocalExcludesEmbeddingOnlyModels`,
`TestListLocalKeepsModelsWithNoCapabilitiesField`, `TestListLocalUnreachable`
(`internal/ollamainfo/listlocal_test.go`), `TestLocalModelsToCatalog` (`internal/tui/localmodels_test.go`).
Full `go build ./...`, `go vet ./...` and `go test ./internal/... -count=1` clean.

**Left alone on purpose:** the setup wizard (`/config`) doesn't use `modelcatalog` at all — it has
its own model-entry path — so this fix is scoped to `/models` only, which is what was reported
broken.

Priority: Tier 2 — S. Shipped same day, no dependency.

</details>

### P71.2 — The DuckDuckGo "lite" fallback shares the primary's rate-limit bucket

**Filed 2026-08-19.** `searchTool.duckDuckGo` (`internal/tool/builtin/web.go:186-200`) tries
`html.duckduckgo.com` and, on zero results, falls back to `lite.duckduckgo.com`. The comment says the
lite page "has historically been more structurally stable", which is a statement about *parsing* —
and the fallback is written as if the failure mode were a markup change.

**The actual failure mode is throttling, and the two endpoints are throttled together.** Probed
directly on 2026-08-19: once blocked, four consecutive rounds against both hosts returned the same
~14.2 KB challenge page from *both*, in the same request pair. The ladder buys **zero** resilience
against the only failure that occurs in practice, while costing a second round-trip on every
genuinely empty result.

**Do:** make the ladder cross-*provider* rather than cross-*path*. Keep lite as the parse fallback it
was written to be, but add a real second backend below it so a throttled DDG is survivable without a
key. Candidates worth evaluating (none yet tested): Mojeek, Marginalia, Startpage, or a Bing HTML
scrape. Whatever is chosen must be checked for the same 200-with-challenge behaviour **P71.1** covers.

**The honest framing, and it should be in the config comments too:** the DuckDuckGo scrape is fine
for a one-off lookup and is structurally unfit for a workload issuing 8–20 queries in a few minutes.
A research run is the second kind. The lasting fix is a keyed provider — `search.provider: tavily`
or a self-hosted SearXNG, both already supported by `websearch_providers.go`. This item is what
makes the *zero-config* path degrade honestly; it is not a substitute for recommending a provider.

**Verified 2026-08-19, correcting what this item said when first filed: Brave is no longer the
free recommendation.** Brave Search API dropped its no-card free tier in February 2026. It is now
$5/1,000 queries with a $5/month credit (~1,000 queries) that requires a card on file at signup and
public attribution on the calling project to keep the credit — drop the attribution and the credit
is gone, and usage past it bills automatically. **Tavily is the one still offering a genuine
no-card free tier**: 1,000 credits/month (a basic search = 1 credit), no card required. Recommend
Tavily as the default keyed suggestion; keep Brave documented as an option for anyone already paying
for it, not as the zero-friction path. `docs/configuration.md`'s "Configure pluggable web search"
example currently shows `provider: brave` and should be updated to `tavily` alongside this.

**Closure condition:** with DDG throttled (reproducible by issuing three queries in five seconds), a
`web_search` call still returns results from another backend, and the result names which backend
served it (**P71.4**).

**Partially addressed 2026-08-19: the documentation half only.** `docs/configuration.md`'s "Configure pluggable web search" example now recommends `provider: tavily` (a genuine no-card free tier, confirmed live) over `provider: brave` (no longer free as of February 2026 — requires a card and public attribution to keep its monthly credit), with both trade-offs stated inline. **The actual cross-provider fallback ladder — a second scrape backend below DDG — remains open**: the candidates named above are still untested, and this item's own text says not to add one without checking it against the same challenge-page behavior **P71.1** detects. Do not read the docs fix as closing this item.

Priority: **Tier 2 — M.** Depends on **P71.1** for the throttle *detection* that has to trigger the
fallback.

<details>
<summary>P71.3 — retry/backoff for web_fetch and web_search (shipped 2026-08-19)</summary>

### P71.3 — Nothing in the web path retries, anywhere — SHIPPED 2026-08-19

**Filed 2026-08-19.** `internal/provider/retry.go` has a tested equal-jitter backoff that eight
caller classes share (P67.3). The web tools use none of it:

- **`fetchTool.get`** (`web.go:99-117`) makes exactly one attempt. A transient resolver failure
  returns `fetch failed: lookup <host>: no such host` as a terminal tool error. **Observed live on
  2026-08-19**: `learn.microsoft.com` failed to resolve inside a `web_fetch` while `nslookup`, `curl`
  and a direct `net.DefaultResolver.LookupIPAddr` on the same host from the same machine seconds
  later all succeeded. A single retry absorbs this class entirely.
- **`doSearchRequest`** (`websearch_providers.go:145-158`) makes one attempt and does not read
  `Retry-After`. A 429 from Brave or Tavily is reported as a terminal `status 429`, which then
  silently falls through to the DDG scrape (**P71.4**).
- **`searchTool.duckDuckGo`** has no client-side pacing at all, which is why two queries in a second
  is enough to earn a 60-second block (**P71.1**).

**Do**, in three separable pieces:

1. Wrap `fetchTool.get` in the existing backoff for connection-class failures only — DNS, connection
   reset, TLS handshake, and 5xx. Never retry a 4xx: **7 of 15 fetches in the 16k live run were 404s
   on invented URLs**, and retrying those would have burned the budget faster, not slower.
2. Honour `Retry-After` on 429/503 in `doSearchRequest`, with the backoff as the fallback delay.
3. Add a token bucket in front of the DDG scrape — roughly one query per 3–5 s, derived from the
   measured 2-query ceiling. This is pacing, not retry, and it belongs to the scrape path only; a
   keyed provider must not be slowed by it.

**Bound the total.** Every retry added here sits under `MaxTurnStall` (900 s) and must be entered in
`TestToolTimeoutsStayUnderTheStallBound`'s table — the invariant in CLAUDE.md is that the table is
exhaustive, so a new per-call wait that is not in it is the regression.

Priority: **Tier 2 — S** for (1) and (2), **S** for (3). No dependency; (3) pairs naturally with
**P71.1**.

**Shipped 2026-08-19 — pieces (1) and (2); (3), a DDG-scrape token bucket, stays out of scope of this item** (P71.2's cross-provider ladder is the more durable fix for the same symptom, and P71.1's rate-limit detection already gives the model a truthful signal instead of a silent-empty one — a client-side pacer over an unkeyed scrape was judged not worth building against a not-really-supported backend). `fetchTool.get` and `doSearchRequest` (`internal/tool/builtin/web.go`, `websearch_providers.go`) now retry up to `webRetries` (2) times with equal-jitter backoff — restated locally rather than importing `internal/provider`'s decorator, keeping `internal/tool/builtin` a leaf package — retrying only a transport-level failure or a 429/5xx (`webRetryable`), honoring a provider's `Retry-After` header, and **never retrying a 4xx**: the live run this responds to had a model inventing URLs, and retrying a wrong URL just spends the round's budget faster. The worst-case retry sequence (`maxFetchWait`/`maxSearchWait`) is ~100s/~70s, verified under the 900s `MaxTurnStall` bound by a dedicated `TestWebRetryWaitsStayUnderTheStallBound` — kept separate from `TestEveryToolTimeoutIsAccountedFor`'s stricter table, which counts explicit `context.WithTimeout` call sites and doesn't cover the pre-existing `http.Client.Timeout` fields these retries wrap. New tests: `TestFetchToolRetriesTransientFailureThenSucceeds`, `TestFetchToolNeverRetries404`, `TestDoSearchRequestHonorsRetryAfter`.
</details>

<details>
<summary>P71.4 — name the serving backend on a search result (shipped 2026-08-19)</summary>

### P71.4 — A configured search provider's failure is invisible to everyone — SHIPPED 2026-08-19

**Filed 2026-08-19.** `searchTool.Execute` (`web.go:152-165`) calls `providerSearch`, and on error
sets `results = nil` and falls through to the DuckDuckGo scrape. `provErr` is then consulted **only
if the scrape also returned nothing**:

```go
if len(results) == 0 {
    msg := "no results found"
    if provErr != nil { msg = fmt.Sprintf("search failed (provider %q: %v; …)", …) }
```

So whenever the fallback succeeds, a broken configured provider — expired key, wrong `base_url` on a
self-hosted SearXNG, a 429 — is indistinguishable from a working one. The user believes they are on
their keyed provider's monthly allowance; they are silently back on the scrape, and therefore back
inside **P71.1**'s 2-query ceiling. This is the failure mode most likely to make search feel
*inconsistent* rather than broken, because it is intermittent by construction.

**Do:** stamp the serving backend into the wrapped result header — the `trust.Wrap` attribute list
already carries `query`, so add `backend`. When a configured provider failed and the scrape covered
for it, say so on the same line. Add a one-shot warning in the daemon log the first time a configured
provider errors in a session.

**There is no `aegis doctor` check for `search` at all — confirmed 2026-08-19 by reading
`runDoctorChecks`** (`internal/cli/doctor.go:272-287`): it covers workspace trust, provider, provider
adapter, generation budget, tool-call probe, sandbox, scanner, guard, workdir, terminal caps,
per-command host binaries, and daemon — ten checks plus the two loops, and `search` is in none of
them. `doctorCommandChecks`'s pattern (PASS if reachable, WARN if a fallback exists and nothing was
asked for, FAIL if a specific config was given and it did not work) fits `search` directly: WARN on
the zero-config DuckDuckGo default with the same "structurally unfit for a research workload" framing
as this item's config-comment fix, FAIL on a configured provider whose key or `base_url` doesn't
resolve. Add it as an eleventh check.

**Closure condition:** with `search.provider: tavily` and a deliberately invalid key, a `web_search`
call returns results *and* states that Tavily failed and DuckDuckGo served them; `aegis doctor` FAILs
on the same misconfiguration without a `web_search` call being made first.

**Shipped 2026-08-19, both halves.** `searchTool.Execute` (`internal/tool/builtin/web.go`) now stamps `backend` onto the `trust.Wrap` attributes (`"duckduckgo"`, the configured provider's name, or absent on a genuine empty result), and prepends a `[note: configured provider %q failed (%v); DuckDuckGo served this instead]` line when a fallthrough happened — verified live against a real Tavily key, which correctly shows `backend="tavily"`. Separately, `doctorSearchCheck` (`internal/cli/doctor.go`) is a new `aegis doctor` check — config-shape validation, not a live network probe, so doctor doesn't spend a metered provider's quota just to run: FAILs on a keyed provider with no `api_key`, on `searxng` with a missing or unparseable `base_url`, and on an unrecognized `provider` string; WARNs on the zero-config DuckDuckGo default with the same framing as the docs fix; PASSes a correctly-configured keyed provider. Verified live — `aegis doctor` on this machine's zero-config default correctly WARNs. New tests: `TestSearchToolWrapsUntrustedContent` (updated) and `TestDoctorSearchCheck`.
</details>

Priority: **Tier 2 — S.** No dependency. Cheap, and it is the difference between "search is flaky"
and a diagnosable configuration error.

<details>
<summary>P71.5 — scale web_fetch's cap to the resolved context window (shipped 2026-08-19)</summary>

### P71.5 — `web_fetch`'s output cap is a constant, and on a 16k window it exceeds the compaction trigger — SHIPPED 2026-08-19

**Filed 2026-08-19, measured.** `fetchTool.Execute` defaults `limit` to **20,000 characters**
(`web.go:81-84`), documented in `truncate.go`'s posture table as ~5.0k tokens. That figure is not
compared against anything.

`tokenest.CompactionTrigger(window, maxTokens)` is the one compaction threshold (CLAUDE.md, P66.14).
Evaluated at `max_tokens: 8192`:

| window | trigger | one default `web_fetch` |
|---|---|---|
| 16,000 | **8,000** (the `window/2` floor) | ~5,000 tok |
| 32,000 | 22,208 | ~5,000 tok |
| 64,000 | 52,608 | ~5,000 tok |
| 131,072 | 111,411 | ~5,000 tok |

**At the shipped local config — `context_window: 16000` in the generated global config — a single
source read is 62% of the entire compaction budget.** Reading one page can therefore trigger
compaction on its own, and reading two consecutively cannot avoid it.

**This is what the 16k live run looked like: 25 compactions across 42 tool calls**, almost all of the
shape `11→9 messages` — each compaction buying about two turns of headroom before firing again. Every
one is an extra full inference over a ~10k-token prompt on a 9B, which is where the wall-clock went
(646 s). The 32k control run, same model and same task, took **4 compactions and 267 s**.

The downstream damage is worse than the latency. Search results were summarized away before the model
could fetch the URLs in them — which is *why* it began inventing URLs (**P71.1**) — and the final
report carried **zero inline `[n]` citations** despite the skill's section 4 requiring one on every
non-obvious claim.

**Do:** size the default cap from the resolved context window instead of pinning it — something on
the order of `min(20000, window * 0.15 * 4)` chars, so a source read is a bounded fraction of the
compaction budget rather than most of it. Keep the explicit `max_chars` argument as the override it
already is, and keep the P64.1 no-spill exclusion exactly as it stands: the clipped remainder must
not become an unwrapped workspace file.

**Do not** simply raise `context_window`. The 16k pin is load-bearing for the P69 debate topology
(user config comment, and `research/debate-topology-plan.md`): it overrides each model's Modelfile
`num_ctx`, so raising it re-inflates every seat's KV cache at once and breaks residency on a 16 GB
card. The per-tool cap is the knob that does not have that coupling.

**Closure condition:** at `context_window: 16000`, a `web_fetch` with no `max_chars` returns a result
that does not by itself cross `CompactionTrigger(16000, 8192)`; a unit test pins the relationship
rather than the constant.

Priority: **Tier 2 — S.** No dependency. Highest-value item in the batch after **P71.1**, and the one
that makes local-profile research viable at all.

**Shipped 2026-08-19**, and shipped closer to the resolved *per-turn* window than the item asked for: `tool.WithContextWindow`/`ContextWindowFromContext` (`internal/tool/tool.go`) is a new context-value pair, the same shape as the existing `WithRegistry`/`WithWorkdir`, carrying `Engine.effectiveContextWindow()` — the actual escalatable, possibly-detected figure, not a static config read — into `Engine.toolCtx` on every tool call. `defaultFetchLimit` (`internal/tool/builtin/web.go`) sizes the cap at `window*3/5` chars, capped at today's flat 20,000 and floored at 4,000, so cloud-scale contexts see no behavior change and only a small window shrinks. At `context_window: 16000` this is 9,600 chars (~2,400 tokens) against an 8,000-token `CompactionTrigger` — comfortably inside it, closing the item's own arithmetic. New tests: `TestWithContextWindowRoundTrips`, `TestWithContextWindowZeroOrNegativeCarriesNothing`, `TestContextWindowFromContextUnset` (`internal/tool`), `TestDefaultFetchLimitScalesWithContextWindow`, `TestFetchToolUsesScaledCapByDefault` (`internal/tool/builtin`).
</details>

<details>
<summary>P71.9 — make the working-file update unconditional (shipped 2026-08-19)</summary>

### P71.9 — The deep-research findings log is advisory, and the run leaves it as placeholders — SHIPPED 2026-08-19

**Filed 2026-08-19, observed live.** Section 2 of `internal/skills/builtin/deep-research/SKILL.md`
diagnoses the problem correctly — "a long research run can outlive the context window, and a log that
lived only in conversation is destroyed by compaction exactly when it's most needed" — and then makes
the remedy conditional: "**For anything beyond a couple of rounds**, keep the log and trail in a
working file (e.g. `.aegis/research/<topic-slug>.md`), updated each round".

**The 16k live run wrote that file once, at Round 1, and never touched it again** across 42 tool
calls and 25 compactions. Its entire content at the end of the run, after the scope header:

    **Sources Examined:**
    - [url1] — kept/rejected — need to fetch and evaluate

    **Findings:**
    - [To be updated after web_search]

    ---

    ## Audit Trail
    - [To be updated]

(Indented rather than fenced on purpose: `scripts/roadmap-status.sh` treats any column-0 `## ` line
as a section break, so a verbatim excerpt containing a markdown heading silently truncates the item's
body and drops its priority line from the parse. Indent quoted headings.)

976 bytes, all scaffold, no evidence. Everything real lived in the conversation, which was being
compacted every two turns. The audit trail in the final report was therefore **reconstructed from
memory at the end** — the specific thing section 2 exists to prevent — and it shows: two of the five
cited URLs are wrong (`ready/landing-zones/` for `ready/landing-zone/`, and a bare directory listing
cited as a source).

**Do:** make the working file unconditional, and make the write part of the round rather than a
recommendation about it. Concretely — reword section 1's step 5 from "Record — update the findings
log" to an explicit "append to `<file>` **before** the next `web_search`", drop the "beyond a couple
of rounds" qualifier, and have section 0 create the file with the round-1 skeleton as its first
action. The file is the only artifact that survives compaction; the skill should treat it as the
primary record and the conversation as the cache, not the other way round.

**This is the cheap half of P71.8.** Phasing gives per-round context resets and is the structural
fix; this makes the *current* single-context drive stop losing its own evidence, and it is a text
edit to one embedded skill file. Note that `SKILL.md` is `go:embed`-ed — rebuild the binary or the
old copy ships (CLAUDE.md, *Embedded assets*).

**Closure condition:** a live run at `context_window: 16000` ends with a working file containing one
populated log entry per round and no `[To be updated]` placeholders.

Priority: **Tier 2 — S.** No dependency, no Go code. Take it alongside **P71.5**.

**Shipped 2026-08-19** — a text edit to the embedded `internal/skills/builtin/deep-research/SKILL.md`, no Go code, exactly as filed. Section 0 now creates the working file unconditionally, before the first search, with the round-1 skeleton; section 1 step 5 is reworded from "update the findings log" to an explicit `edit_file` instruction that must land **before the next `web_search`/`web_fetch` call**, not "when convenient"; section 2's "for anything beyond a couple of rounds" qualifier is gone. Since `SKILL.md` is `go:embed`-ed (CLAUDE.md, *Embedded assets*), this ships with the next binary rebuild — confirmed via `go build ./...` in this same session. Full live re-verification (a research run whose working file is populated every round, not just at round 1) is still open — the SKILL.md change is textual and testable by inspection, but the behavior change needs a live run to confirm the model actually follows the stronger instruction.
</details>

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

**Status: 2 open — P71.8 and P72.1.** **P71.8**, filed 2026-08-19, is deep-research declaring no
`phases:` frontmatter, so it runs single-context and `aegis chat --skill` cannot drive it to
completion. It is the structural item of the P71 batch, and its three prerequisites are now all in
place: **P71.9** (the working file that becomes its phase artifact), **P71.1** and **P71.5** (the two
acute bugs that would otherwise make a phased run's evidence unreadable) all shipped 2026-08-19. It
also corrects `internal/drive/drive.go:175`, which names four skills as multi-phase file-per-phase
builds when `threat-modeling` is the only one with a plan.

**P72.1**, filed the same day out of a follow-up conversation, is the general form of a question the
P71 batch's `context_window: 16000` finding raised without naming: nothing computes or applies a
context-window fit at daemon boot or `/model` switch. `aegis models --fit` (P69.5) already has the
exact math; this item is the wiring, the wizard prompt, and the cold-start design decision the wiring
needs — three gaps confirmed by reading the actual call graph, not assumed from the release note.

**Before it: P70.4 shipped 2026-08-18**, the day it was filed — see [Both halves of the
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

### P71.8 — deep-research declares no phases, so it runs single-context and `--skill` cannot drive it to completion

**Filed 2026-08-19, from two live runs that failed in opposite directions.** This is the structural
item of the P71 batch; **P71.1** and **P71.5** are the two acute bugs, and this is the reason the
skill has no margin to absorb either.

**Three facts that only make sense together.**

1. `internal/skills/builtin/deep-research/SKILL.md` declares **no `phases:` frontmatter** — only
   `name` and `description`. So `drive.PlanFor("deep-research", nil)` returns nil and the run uses
   the generic single-context drive. `TestPhasePlanFor` (`internal/drive/drive_test.go:112`) actively
   pins this: *"a skill declaring no phases must fall back to the generic drive"*.
2. `internal/drive/drive.go:175` says the opposite in prose: "deep-research, latex-report,
   structured-build and documentation-as-code are all multi-phase, file-per-phase builds with the
   same single-context problem threat-modeling has." That comment describes an intent nobody
   implemented. **`threat-modeling` is the only built-in skill with a phase plan**, and — grep
   confirms — the only one that scaffolds `<!-- PENDING -->` markers.
3. `aegis chat --skill` auto-continues *while `<!-- PENDING -->` markers remain under `.aegis/`*.
   deep-research writes none, so the drive-to-completion the flag exists to provide is **inert** for
   it. The flag's own help text names deep research as a beneficiary ("This is what lets a long,
   multi-phase skill (threat model, deep research) finish non-interactively"). It does not.

**What that produced live, on the same model and task, at two window sizes:**

| | 16k (shipped config) | 32k control |
|---|---|---|
| Elapsed | 646 s | 267 s |
| Tool calls | 42 | 39 |
| Compactions | 25 | 4 |
| Empty searches | 8 / 19 | 4 / 10 |
| Failed fetches | 7 / 15 | — (0 `web_fetch` calls) |
| Inline `[n]` citations | **0** | 18 |
| Outcome | full report, uncited, 2 bad URLs | **no report at all** |

**Raising the window fixes the thrash and exposes the drive-termination bug underneath.** The 32k run
stopped after Round 1 with a status update headed "Work Remaining", listing Rounds 2–5 as future
work, and exited 0. Nothing was wrong with the model's behaviour — it yielded, as a model does, and
nothing continued it, because there were no markers to continue on. The 16k run only produced a
report because its thrash happened to keep it talking past the point the 32k run stopped.

**Traced 2026-08-19 through `internal/cli/chat.go` and `internal/drive/drive.go`, to name exactly
which budget each run actually exhausted, since two different ones are in play with the same default
value.** `provider.max_iterations` (default 40) bounds tool-call *rounds* inside one
`engine.Run()` call; `--max-turns` (also defaulted 40, confusingly) bounds how many times
`runLinear`'s outer loop calls `engine.Run()` at all. Without phases, `runLinear` calls `engine.Run()`
**once**: on return, `scanPendingMarkers` finds nothing (deep-research writes no markers), so
`drive.VerifySkillOutputs` reports `ran=false` (no verifier configured), and the loop `break`s,
declaring the run done (`chat.go:849-851`). So the entire research task — every round, every search,
every fetch — happened inside the tool-call-round budget of **one single `engine.Run()` call**, and
the run's actual stopping condition was never "the research finished"; it was "the model stopped
issuing tool calls," which `runLinear` then took at face value because there was nothing telling it
otherwise. Compare `drive.Run`'s phased path (`drive.go:423-470`): each phase gets its **own** fresh
`engine.Conversation` and its own call to `st.Engine.Run`, so phasing does not just reset context —
it also resets the 40-round budget every phase, which single-context deep-research currently spends
once for the whole task. This is *why* phasing is the fix and not just a workaround: the two 40s are
the same number by coincidence, not by design, and single-context deep-research is silently living
inside the smaller of two independent budgets that were sized for different things.

**Do:** give deep-research a real phase plan, via the `phases:` frontmatter seam P52.12 built for
exactly this ("how any skill opts in without a code change"). The natural decomposition is already
written in the skill: scope → round → round → … → synthesize, with the working file from **P71.9** as
the artifact each phase appends to and the next phase's fresh context reads back. Each round becomes
a context-reset run, which is the actual fix for a small window — the per-fetch cap (**P71.5**)
raises the ceiling, phasing removes it.

**Three constraints on the design:**

- **The phase artifact must be the marker-bearing file.** That is what re-arms `--skill`'s
  auto-continue and what `drive.verify` reads. Without markers this item does not close, however good
  the phase list is.
- **Do not narrow phase tools.** `drive.phase6Phase` (`drive.go:148-166`) already reasons about this
  and gets it right: a plan from frontmatter declares no per-phase tools, and narrowing its verify
  round to a threat-model surface "would take capabilities away from a skill that never opted into
  narrowing — deep-research wants `web_search` in a fix round exactly as much as a threat model does
  not." Leave that behaviour alone.
- **`drive.verify:71`** notes that skills without phases keep the pre-P39.6 "markers cleared = done"
  behaviour. Adding phases moves deep-research onto the other path; check that transition rather than
  assuming it.

**And fix the comment either way.** `drive.go:175` names four skills as multi-phase and three of them
(deep-research, latex-report, structured-build, documentation-as-code) declare no phases. Whichever
of them is not going to get a plan should come out of that sentence, so the next reader does not
trust it the way this investigation initially did.

**Closure condition:** `aegis chat --skill deep-research "<topic>"` at `context_window: 16000` runs
to a cited final report without manual continuation, with per-round compaction counts in single
digits, and `PlanFor("deep-research", spec)` returns a non-nil plan (which means
`TestPhasePlanFor`'s deep-research assertion has to be rewritten against a skill that genuinely
declares nothing — do not simply delete it).

Priority: **Tier 3 — M/L.** Sequence-dependent: **P71.9** should land first (the working file is this
item's phase artifact), and **P71.1** and **P71.5** before that, or the phased run inherits both
acute bugs and the evidence will not be readable.

### P72.1 — `context_window` is sized once by hand; nothing computes or applies a fit at boot or model switch

**Filed 2026-08-19, from a user question**: could the serving context window be set to whatever this
machine can actually hold, automatically, at daemon boot or when `/model` switches models — instead
of being a number someone worked out once and pasted into `config.yaml`.

**The math to answer that question already exists and is exact.** P69.5 shipped `aegis models --fit`:
given a KV budget in GiB, `ollamainfo.Fit` solves for the largest window that fits a model's measured
weights plus its KV-cache cost, validated against real Ollama measurements to 0.2% error. Run live on
2026-08-19 against this machine's `aegis-qwen35-9b:16k` (RX 7900 GRE, 16 GiB VRAM):

| Budget | Window | KV cache | Total | Notes |
|---|---|---|---|---|
| — (curve) | 16,384 | 2.06 GiB | 6.02 GiB | today's shipped `context_window: 16000` |
| — (curve) | 65,536 | 8.25 GiB | 12.21 GiB | 3.8 GiB spare on a 16 GiB card |
| 14.00 GiB | 79,360 | 9.99 GiB | 13.95 GiB | f16 KV, 0.05 GiB spare |
| 14.00 GiB | 135,680 | 8.77 GiB | 13.69 GiB | **q8_0 KV** — roughly halves cache cost per token |
| — (curve) | 131,072 | 16.50 GiB | 20.46 GiB | the model-max recommendation — **does not fit this card at all** |

So a solo session on this model is running at **16,384 of a safely-fittable 65,000+** — the number is
not wrong, it is just far more conservative than the hardware requires, because nothing ever asked
the hardware.

**Three things stand between that math and "dynamic," confirmed by reading the actual call graph
rather than assuming it from the release note:**

1. **`provider.vram_budget_gb` defaults to 0, and the setup wizard never asks.** Grepped every
   consumer (`internal/cli/modelsfit.go`, `internal/config/config.go`, `internal/config/write.go`,
   `internal/tui/wizard.go`) — `wizard.go:545` hardcodes `budgetGB = 0` at the one place first-init
   could collect it. The budget is "operator input" by design (`internal/hwinfo` forbids VRAM
   auto-detection outright, P17.5 — it would mean reimplementing Ollama's own placement heuristic
   unreliably), but today nothing ever prompts for that input, so the fit math never runs unless the
   operator finds `--fit` on their own.
2. **`--fit` cannot run before a model has been loaded once.** `fitWeights` measures resident weight
   bytes from `/api/ps`, which requires the model already loaded — and loading a model *is* what
   commits it to a serving window. This is a real chicken-and-egg constraint, not an oversight: you
   cannot solve for the window before you know the weights, and you cannot cheaply know the weights
   without loading at *some* window first. `--fit`'s own error path names this directly ("Model is not
   currently loaded, so its resident weights cannot be measured").
3. **Nothing calls any of this at boot or on `/model`.** `cmdModel` (`internal/tui/slash.go`) switches
   the session's model override and nothing else — no re-fit, no window recomputation. Daemon startup
   reads `context_window` from config as a plain int; no boot-time code path touches `ollamainfo.Fit`.
   `--write` patches the *global* config file and needs a daemon restart to take effect — there is no
   live "the model just changed, resize now" path even manually.

**A fourth complication is architectural, not a missing wire: `context_window` is one global number,
but the debate topology (P69.1/P69.5) already resolves each seat's window independently per model**
via `effectiveContextWindowFor` (`internal/server/contextwindow.go:269`), detected from what Ollama
reports for that specific tag rather than read from the global config. A boot-time "fit the window to
the hardware" pass has to either leave debate's per-seat resolution alone (it already does something
smarter than the plain config path) or explicitly account for it — sizing the *default* model's
window against the full budget while debate is inactive would starve it, or overshoot it, the moment
a debate seat needs to co-reside. **This is exactly the class of problem `--fit-debate`/`--fit-set`
already solve for a manual run**; a boot-time version needs the same resident-set reasoning P69.6
built, not just the single-model half.

**Do**, roughly in landing order:

1. **Wizard: ask for a VRAM budget instead of defaulting to 0.** `internal/hwinfo` cannot detect it,
   so this stays a question to the operator — but ask it, rather than silently shipping the
   pathological model-max default P69.6's own release note found (131072 tokens, 20.46 GiB, does not
   fit this card).
2. **A boot-time fit pass, gated on `provider.vram_budget_gb` being set** (P69.6's "inert until opted
   in" pattern — keep it). On daemon startup, if the default model is already loaded (warm start) or
   after its first load (cold start), run the existing `ollamainfo.Fit` against the configured budget
   and apply the result as the *effective* window for that run — not necessarily rewriting
   `config.yaml`, since the chicken-and-egg problem means the *first* load of a cold daemon still has
   to pick something before it can measure anything. A safe floor (today's `RecommendContextWindow`
   halving, or the last-written config value) for that first load, then re-fit and apply from the
   second load onward, is one resolution; there may be a better one — this needs a design pass, not
   just a wire.
3. **`/model` switch: re-fit for the new model** using the same budget, once it has been loaded once.
   Until then it necessarily runs at a fallback figure — the same first-load problem as (2), once per
   newly-selected model rather than once per daemon lifetime.
4. **Route the debate/resident-set case through P69.6's existing solver**, not a second, simpler one
   — a boot-time fit that ignores co-residency would re-introduce the exact bug P69.6 closed (sizing
   a seat as if it were alone).

**Do not:** have this silently overwrite a `context_window` the operator hand-tuned for a reason —
the debate topology's current 16k pin is exactly such a value, load-bearing per its own config
comment and `debate-topology-plan.md`. Any automatic write needs to be opt-in and visible (a log line
at minimum, matching what P69.6 already does for its resident-set claims), never a silent overwrite
of a number someone set deliberately.

**Closure condition:** with `provider.vram_budget_gb` set and no `context_window` configured, a fresh
daemon boot against this machine's `aegis-qwen35-9b:16k` serves a window in the 55,000–80,000 range
(matching the measured curve above, not the 16,384 default or the unfit 131,072 model-max
recommendation) without a manual `--fit --write` step, and a debate started immediately afterward
still gets P69.6's resident-set-aware per-seat windows rather than a value sized as if it were alone.

Priority: **Tier 3 — M/L.** Real value, no fired trigger in the sense this document uses it (nobody
was blocked), but genuinely sequence-dependent on a design decision for the cold-start problem in
step 2, and it touches the wizard, the daemon boot path, and the existing resident-set solver rather
than being a single self-contained change.

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

**Status: 24 open** — 9 pre-existing (all blocked or explicitly parked, none with a fired trigger),
6 from the P66 review batch, 5 from the P67 external-source reading, **P70.3** (filed 2026-08-18 out
of P66.15's sweep) and 4 from the P71 batch filed 2026-08-19 (**P71.6**, **P71.7**, **P71.11**,
**P71.12**). (This line read "21 open" against a Status block that said 20, a drift dating to
2026-08-16 when it was not updated as P66.23 and then P66.26 were filed; it is reconciled here
against the Status block, which has been the correct count throughout.)

The P66 entries here are **deliberately grouped grab-bags**: each collects the Low-severity residue of
one review domain. They are filed so no finding is lost, not because any of them should be scheduled.
Take one only when already working in that file. The P67 entries are a different kind of parked: each
is a capability Aegis does not have and nobody has asked for, filed with the specific trigger that
would make it worth building.

**The four P71 entries are a third kind, and two of them are parked by *choice* rather than by
absence of demand.** **P71.6** (in-session response caching) and **P71.11** (window-derived research
budgets) are both blocked on **P71.8**: phasing changes the arithmetic under each, so fixing them
first would fit a constant to a regime about to change. **P71.7** (publication dates on search
results) waits on a keyed provider being the default, because that is the only backend where the date
is actually available. **P71.12** is different again — it is a filed **negative measurement**, kept
so the next reader does not re-derive an intuition this batch already tested and found small.

### P71.6 — Nothing memoizes a fetch or a search within a session

**Filed 2026-08-19.** `web_fetch` and `web_search` re-issue every request. The deep-research skill's
audit trail is explicitly designed around not repeating work — "it prevents re-fetching the same dead
ends when a topic gets revisited" (SKILL.md §2) — but that guarantee lives entirely in the model's
context, which compaction deletes. After a compaction the model has no record of what it fetched, so
a re-fetch is both likely and silently expensive: full network round-trip, full token cost again.

An in-session cache keyed on the normalized URL (and on query+max_results for search) would make the
audit trail's promise real rather than aspirational, and would make a re-fetch after compaction
nearly free — which is the recovery path P64.1 deliberately chose over spilling.

**Promote when** **P71.8** lands: a phased run reads the working file back into each fresh context
and will re-fetch by design at phase boundaries, which is the first time this stops being
speculative. Until then the compaction thrash (**P71.5**) dominates and this would be measuring the
wrong thing.

Priority: Tier 4 — S. No fired trigger yet.

### P71.7 — `web_search` results carry no publication date, so the source-quality bar cannot be applied

**Filed 2026-08-19.** Section 3 of the deep-research skill requires the model to "note publication
dates. For fast-moving topics prefer recent material and flag anything old enough that it may no
longer hold." Section 1 step 3 requires that quality bar to be applied to "result titles/URLs/
snippets *before* fetching".

`searchResult` carries `title`, `urlStr`, `snippet` and nothing else (`web.go:203`), and DDG snippets
rarely contain a date. So the skill instructs the model to filter on a signal the tool does not
provide, at the one point where filtering is cheap. The only way to obey it is to fetch everything
first — which inverts the budget the skill is trying to hold, and on a small window is exactly the
behaviour **P71.5** makes unaffordable.

A fetched page usually carries `og:article:published_time` or a `<time>` element, which is a real
signal but only available *after* the fetch this section is trying to avoid.

**Checked 2026-08-19, and weaker than assumed when this item was first filed: neither recommended
provider is a clean win.** Tavily's `/search` response schema is `title`, `url`, `content`, `score`,
`raw_content`, `favicon`, `images`, `id` — **no date field**. Brave's Web Search API supports
`freshness` as a *query* filter (`pd`/`pw`/`pm`/`py`), which narrows *before* searching rather than
labeling results *after*, and it was not possible to confirm from the public docs whether individual
result objects carry an `age`/`page_age` field — needs a live authenticated call against
`api.search.brave.com/res/v1/web/search` to settle, not another documentation read.

**Promote when** that live call is made (a five-minute check once a Brave key exists for any other
reason) and confirms a per-result date field, or when Brave's `freshness` filter is judged good
enough on its own — it solves a related but different problem: "don't return old pages" rather than
"tell me how old this page is". Until one of those is true this item stays unbuildable as stated.

Priority: Tier 4 — S. Real, and unbuildable well on the zero-config backend.

### P71.11 — The deep-research budgets are cloud-window constants handed to a local model

**Filed 2026-08-19.** The skill fixes its budget in prose: "**Round cap: 8**", "roughly 5–12 quality
sources", and it relies on `web_fetch`'s 20,000-char default per read. None of the three is a
function of the context window.

At `context_window: 16000` that budget is arithmetically impossible: 8 rounds × 5–12 sources ×
~5,000 tokens per source is one to two orders of magnitude past a window whose compaction trigger is
8,000 tokens. The model does not know this, so it plans a run it cannot execute and then discovers
the wall one compaction at a time. The 16k live run's own opening plan states "**Budget:** 8 rounds
max, targeting 5-12 quality sources" — copied faithfully from the skill, and never achievable.

**Do:** derive the round and source targets from the resolved window at skill-activation time, the
way `enginecfg` derives run limits once for every caller, rather than hard-coding a cloud-sized
number in prose. Roughly: four rounds and three or four sources at 16k, the current numbers at 128k.

**Promote when** **P71.8** lands. Phasing changes the arithmetic completely — a per-round context
reset means the per-*run* budget stops being the binding constraint — so setting these numbers before
phasing exists would be fitting a constant to a regime that is about to change. Filed now so the
observation is not lost.

Priority: Tier 4 — S. Blocked on **P71.8** by choice, not by dependency.

### P71.12 — Main-content extraction for `web_fetch` — measured, and smaller than it looks

**Filed 2026-08-19 as a negative measurement**, so the next reader does not re-derive it. `htmlToText`
(`web.go:257`) keeps every text node outside `script`/`style`/`noscript`, so a fetched page carries
its navigation, cookie prose, "This browser is no longer supported", breadcrumb and footer. The
obvious improvement is to prefer `<main>`/`<article>` and drop `nav`/`header`/`footer`/`aside`.

**It is worth less than it appears.** Measured across four `learn.microsoft.com` pages on 2026-08-19:

| page | raw HTML | after `htmlToText` | boilerplate | share |
|---|---|---|---|---|
| `cloud-adoption-framework/ready/landing-zone/` | 66,399 | 11,374 | 1,395 | 12% |
| `architecture/networking/architecture/hub-spoke` | 97,699 | 37,774 | 1,250 | 3% |
| `defender-for-cloud/defender-for-cloud-introduction` | 64,194 | 17,305 | 1,218 | 7% |
| `networking/design-guide/internet-ingress` | 84,672 | 29,850 | 1,446 | 5% |

Roughly **1.2–1.5 KB per page, 3–12%** — a few hundred tokens. The existing converter is already
doing the heavy lifting (66 KB of HTML down to 11 KB of text). Structural extraction is a real but
marginal win, and it carries a real risk: `<main>` heuristics fail differently per site, and a
mis-detected container silently returns *less* than the naive walk.

**Promote when** already editing `htmlToText` for another reason, or if a site is found where the
boilerplate share is large enough to change a fetch's usable content. Do **not** schedule it as a
context-budget measure — **P71.5** is that measure, and it is worth roughly twenty times as much.

Priority: Tier 4 — S. Confirmed small. Do not schedule.

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
