# Aegis Capability Roadmap

**Last updated:** 2026-08-26 — **P78.1**–**P78.9** filed and shipped the same day. Filed from a
five-track code-quality audit (sprawl/duplication/gaps, not security — that axis is `CodeReview.md`'s
and **P76.1**'s) that read the whole tree in parallel by package group. All nine were opportunistic
Tier 4 items with no fired trigger, picked up together rather than left parked, run as seven parallel
subagents by disjoint package: the four god-file splits (**P78.1** `chat.go`, **P78.2** `engine.go`'s
`Run()`, **P78.3** `slash.go`, **P78.4** `drive.go`), the provider-layer cleanup (**P78.5** dedup'd
adapter helpers, **P78.6** `buildOne`'s struct bundle, **P78.7** Anthropic `Healthy()`), the
`config.go` PATCH-endpoint generic plus the `/config/cost` gap it surfaced (**P78.8**), and six small
residue findings (**P78.9**). Full record: [releases.md](releases.md#p781-p789-shipped-2026-08-26).

**Last updated (previous):** 2026-08-25 — **P77.1** shipped. It was parked Tier 4 pending "a user reports
specifically wanting the reasoning content itself" — the user did, directly. Investigating found the
roadmap entry's own premise stale: `provider.ThinkingBlock`/`EventThinkingDelta` and the TUI's live
dim-text-then-collapsible-block rendering (`ctrl+o` to expand) already existed end to end for both
Anthropic and Ollama/OpenAI-compat adapters — the entry's "nothing shows reasoning" was true only in
the sense that every path was opt-in and undiscoverable. The user chose the narrowest fix: native
Ollama's `provider.think` now defaults to `true` instead of `false` (`internal/providerfactory/factory.go`),
since local reasoning is unbilled (unlike Anthropic's thinking budget, left opt-in) and a model that
rejects the parameter already has a graceful one-shot-400-then-latch fallback (P38.5). Live-verified
against this machine's own Ollama server with the config default unset: `aegis-qwen35-9b:16k` streamed
real `EventThinkingDelta` content, while `aegis-phi4-reasoning:16k`/`phi4-mini-reasoning:3.8b` 400'd
("does not support thinking") and were absorbed by the existing retry/latch path. Full record:
[releases.md](releases.md#p771-shipped-2026-08-25).

**Last updated (previous):** 2026-08-24 — **P77.4** shipped (a `fetchCmd[T]` generic now backs the four
`tui.go` command constructors that were a genuine single-call round trip — `fetchTeammates`,
`fetchTeammatesQuiet`, `fetchSessions`, `switchSessionCmd` — closing out the last open item from the
`internal/tui/tui.go` cleanup pass; **P77.2**, **P77.3**, and **P77.5** shipped earlier the same day).
`fetchBacktrackTargets`/`forkAndSwitchCmd` (multi-step, branching) and `startStream`/`startDrive`
(`context.WithCancel`, not a timeout) stayed literal — forcing those through the generic would have
cost more in accommodating parameters than it saved. Full record:
[releases.md](releases.md#p774-shipped-2026-08-24).

**Last updated (previous):** 2026-08-23 — **P76.1**'s two sessions (the scoped, read-only audit of `internal/tui`
and `internal/security` filed 2026-08-21) both ran this sitting and closed, each with one survivor:
**P76.2** (Tier 2 — a TUI quit path that doesn't cancel a running interactive-terminal command) and
**P76.3** (Tier 3 — a hostile repo can plant its own security-scan baseline to hide its own findings,
needs a design decision). Earlier the same day: the un-numbered PXX.1 request closed out (its concrete
asks were already shipped; its one unaddressed thread — visibility into the model's reasoning —
refiled as **P77.1**, Tier 4), and **P68.3** compressed to a pointer now that its full record lives in
[releases.md](releases.md), matching how **P68.2** was already handled. Before it: 2026-08-22, after
**P68.1** shipped (the live tier can now run a measurement it can read back — closing Tier 2 out).
This document tracks only **open** work and what's next. For
shipped-feature history, batch origins, pass-by-pass narrative, refutation records and full design
rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading with a
`Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so keep it when
adding items.

---

## Status

**36 open items: 28 build + 8 verification-only.** Tier 1: 0. Tier 2: 1 (**P76.2**, filed 2026-08-23,
survivor of P76.1 Session B). Tier 3: 2 (**P76.1**, filed 2026-08-21, both sessions now done — see its
entry; **P76.3**, filed 2026-08-23, survivor of P76.1 Session A). Tier 4: 25. Verification: 8.

**Shipped history lives in [releases.md](releases.md), not here.** This document tracks only open
work and current counts; a completed item's full record — what it was, what building it found, and
what was measured to close it — moves there the day it ships. Most recent sittings, newest first,
each a pointer only: **2026-08-21** — **P63.10**, **P75.1**, and the last three of the P74 batch
(**P74.15**–**P74.17**), closing it out entirely (twenty items, P74.1–P74.20). **2026-08-20** —
seventeen more of the P74 batch, filed and shipped the same day. **2026-08-19** — fourteen items
across three sittings (**P71.1–P71.5/P71.9/P71.10**, **P72.1–P72.3**, **P73.1–P73.2**), several
filed by the live verification of the item before them. **2026-08-18 and earlier** — **P70.1–P70.4**,
**P66.13–P66.15**, **P67.6–P67.9**, **P69.1/P69.5/P69.6**, and the rest of the P66 review batch. Full
records, mechanisms and measurements for every one of these are in
[releases.md](releases.md#latest-changes).

**Every shipped item was closed against a live-verified test or a live probe run on this machine,
recorded in its release entry — never asserted from reading a diff.** That standard constrains future
work here.

Tiers 1-4 are **build work** — every item there requires writing code that doesn't exist yet.
**Verification work** is its own track, not a tier: every item in it is code that is _already
written_, sitting behind one gate — a live-model run producing evidence the item's closure
condition names. Mixing the two under one tiering scheme was misleading a reader into treating
"go run a test" and "go design and build a feature" as the same kind of next action. See
[Verification Work](#verification-work) for that track's own status.

**Everything left in the verification track is blocked on something other than a model server**,
which is why parking it costs little. P38.1 needs permission to launch an unattended auto-approving
agent; P62.9 needs a _better task_ rather than more runs of the current one; LLM-03, LLM-10, ARCH-04
and P65.2 all need a session trace from a run whose data dir survives — **P68.1** shipped that
2026-08-22 (live-verified against `TestLiveWorkflow`/`aegis-qwen35-9b:16k`: the kept data dir's
`sessions.db` outlived the test process and `aegis sessions trace <id>` printed the compaction
summary text, the calibration sample count and each turn's stop reason), so those four now have
readable evidence to judge whenever the parked live-tier row is next picked up — nobody has yet.
Only P62.8 is still purely waiting on hardware.

**No Tier 4 build item currently has a fired trigger** (re-verified 2026-08-15: `sandbox.backend`
still defaults to `"local"`, `lsp.Manager` is still one shared daemon singleton, both TUI
asymmetries in P63.10 are still present as described — that one item has since shipped) — see each
entry's **Promote when** for what would change that. Two of them, **P71.6** (response caching) and
**P71.11** (window-derived budgets), were held pending phasing — "setting them first fits a constant
to a regime about to change" — and that regime changed when P71.8 landed; the reason they were
parked no longer applies, so re-check them rather than assuming Tier 4 still fits. **P71.12** is the
opposite case: a filed _negative_ measurement (main-content extraction is worth 3–12% per page,
because the existing converter already takes 66 KB of HTML down to 11 KB of text), recorded so
nobody re-derives it. Explicitly do not schedule.

**P66.26** (PERF-02) is the one refiled sub-item still open, and it stays Tier 4: a Low-severity
durability trade on the one database holding checkpoints, the cost ledger and traces, with P66.9
having already removed most of the pressure behind it.

### Standing constraints on the open batches

**The three P67 constraints, which apply to every P67.10–P67.14 entry and are not repeated in them.**
That batch is a comparative reading of an external agent implementation, not a review of this
codebase: on 2026-08-16 the leaked Claude Code CLI source was read against Aegis for mechanisms worth
having here.

- **That source is leaked proprietary code. Nothing may be transcribed from it.** Each item is a
  design reading — a mechanism and the reasoning behind it — and needs an independent Go
  implementation written from this document, not from that repository.
- **The leak is partial.** `src/utils/**` is absent, so permission internals, `forkedAgent` and
  `toolResultStorage` were legible only through call sites. Where an entry's claim about _their_
  implementation rests on a call site rather than the code, it says so.
- **Every claim about Aegis in these entries was checked against this tree**, at the file and line
  cited, not against the docs. The claims about their side were not, and cannot be — treat them as
  motivation, never as a specification.

**The two P74 constraints, which apply to every P74.\* entry and are not repeated in them.** That
batch is a comparative reading of two external agent implementations, filed 2026-08-20.

- **`tanbiralam/claude-code` contains two rendering modes and the batch was first read against the
  wrong one.** `src/utils/fullscreen.ts:112` gates them on `process.env.USER_TYPE === 'ant'`:
  alt-screen fullscreen internally, inline document flow for external users. The fullscreen path has
  its own virtual scroll, transcript search and mouse selection. **Every TUI entry here has been
  re-read against the alt-screen mode**; P74.2 was rewritten and P74.18 was filed as a result. Anyone
  adding to this lane should check which mode a mechanism belongs to before filing it.
- **`langchain-ai/deepagents` is Apache-2.0 and Python; `tanbiralam/claude-code` is neither.** The
  second repository is a reconstruction of a shipped Claude Code bundle — its own README states the
  source is Anthropic's property, several modules carry a literal `not included in leaked source`
  stub, and the TypeScript has been through the React compiler. It is the same class of source as the
  P67 batch and carries the same rule: **nothing may be transcribed from it.** Each TUI entry is a
  reading of _observed interface behaviour_ — glyphs, layout decisions, gating thresholds — and needs
  an independent Bubbletea implementation written from this document. The practical point reinforces
  the legal one: it is React and Ink, and none of it is portable anyway.
- **Every claim about Aegis in these entries was checked against this tree**, at the file and line
  cited. **P74.1 goes further and was proved by running the real gate** — a throwaway test against
  `NewRuleGate` with the actual `grep` schema, not a reading of `subjectFor`. Claims about either
  external side were not checked and cannot be: treat them as motivation, never as a specification.

The batch was filed from a review artifact whose finding ids differ from the roadmap's, because the
roadmap renumbers into implementation order. The mapping, for anyone reading the two side by side:

| Roadmap | Artifact | Roadmap | Artifact | Roadmap | Artifact            |
| ------- | -------- | ------- | -------- | ------- | ------------------- |
| P74.1   | SEC-1    | P74.7   | TUI-5    | P74.13  | TUI-9               |
| P74.2   | TUI-1    | P74.8   | DA-2     | P74.14  | DA-5                |
| P74.3   | TUI-2    | P74.9   | DA-3     | P74.15  | DA-6                |
| P74.4   | TUI-3    | P74.10  | TUI-8    | P74.16  | DA-4                |
| P74.5   | TUI-4    | P74.11  | TUI-7    | P74.17  | DA-1                |
| P74.6   | TUI-6    | P74.12  | TUI-10   | P74.18  | _(new, 2026-08-20)_ |
|         |          |         |          | P74.19  | _(new, 2026-08-20)_ |
|         |          |         |          | P74.20  | _(new, 2026-08-20)_ |

**The P66 entries here are deliberately grouped grab-bags**, each collecting the Low-severity residue
of one review domain, filed so no finding is lost rather than because any of them should be
scheduled. Take one only when already working in that file. The review itself — six specialist
reviewers, an adversarial debate and a static-analysis pass, 70 findings against HEAD `3c2b57b` — is
in [CodeReview.md](CodeReview.md) with per-finding evidence. **Read the corrections in releases.md
before acting on that document directly:** several shipped items contradict the finding they were
built from (VULN-03's suggested `::ffff:0:0/96` addition would have blocked the entire public
internet; LLM-04 drops _every_ tool call on a 1-based backend, not only trailing ones).

### Decisions that outlive the items that made them

**Three trust-posture questions were answered on 2026-08-18 and they do not all point the same way,
which is the point.** The swarm mailbox **is** wrapped as untrusted (P70.2) and so is a sub-agent's
result (P70.4), because in both cases content crossed a boundary before being relayed onward;
`security_scan`'s workspace-derived output is **deliberately not** wrapped (P70.3) because a file the
model can already read directly is not a boundary crossing. Zero trust is the stated posture for
_ingestion_ and for _relayed_ content, not a rule that every byte gets a marker. Settle the next such
question against those three, not afresh.

**The TUI keeps alt-screen and the app-owned frame. Decided 2026-08-20, after two wrong answers.** The
question was how to get native-feeling scroll and selection. The first answer was "move to document
flow and delegate scroll, selection and search to the terminal" — a 4–6 day commit/live rewrite that
would have retired `/search`, deleted `selection.go`, and **silently given up re-wrap on resize**, since
content hard-wrapped and printed into scrollback can never reflow. The user caught it by asking whether
resize would still re-wrap.

**What the check found is the reusable part.** The comparison client ships _two_ rendering modes, and
`src/utils/fullscreen.ts:112` decides between them with `return process.env.USER_TYPE === 'ant'` —
**alt-screen fullscreen is the internal default; inline document flow is what external users get.** The
fullscreen path carries its own virtual scroll, its own transcript search and its own mouse selection
(the theme's `selectionBg` token is commented "alt-screen mouse selection"). The mode that re-wraps on
resize is the alt-screen one, which is the architecture Aegis already has.

So the settled position: **the gap was never the rendering model, it was the chrome and the quality of
the in-app implementations.** P74.2 is a one-sitting chrome removal, P74.18 fixes the selection
highlight, and `rawScrollback` stays as an opt-in for anyone who wants true terminal scrollback and
will trade re-wrap for it. Anyone reopening this should start from the two-mode fact, not from the
public build's behaviour.

**The follow-on question was settled the same day: selection stays app-owned, and the clipboard gets
fixed instead.** Releasing mouse capture (**P74.19**) would hand selection to the terminal, which is the
only thing that works over SSH today — but in alt-screen a released wheel event goes to the emulator,
so it buys terminal-side copy at the cost of wheel scroll, and both halves were named as important. The
actual defect is narrower: `copyToClipboard` shells out to `pbcopy`/`xclip`/`wl-copy` with **no OSC 52
path**, so a remote session copies to the wrong machine and says it succeeded. **P74.20** fixes that
directly and keeps wheel scroll, click-to-focus and the P74.18 highlight. P74.19 survives as an
off-by-default escape hatch for `tmux`/`kitty` copy-mode workflows. **Generalize: when a preference and
a defect point at the same symptom, fix the defect before trading away a capability for the
preference.**

**Two method notes, both earned the hard way here.**

- **A mode whose tests only assert on the frame the model produces has not been shown to work.**
  `rawScrollback`'s P22.6 tests check `plainView(m)` — the string Aegis emits — and never what a
  terminal does with a 3,000-line frame in a 40-row window. That is what let the mode read as finished
  and drove the first wrong sizing. Applies to any future rendering-mode work in `internal/tui`.
- **When reading an external implementation for mechanisms, establish which build you are reading
  first.** The whole P74 TUI lane was filed against the public Claude Code behaviour while the
  interesting mode was behind an env check in a file nobody had opened. Two of the batch's items had
  their direction inverted by that one fact.

**Read the P67.7 record before touching `internal/engine`.** That item asked for tool calls to be
dispatched as their blocks complete in the stream, and named four constraints. Building it found two
more: the P53.2 loop guard can _abort_ a run on the complete round's signature, and the pre-tool-round
budget gate exists specifically so a turn whose own usage crosses the cap stops before its tool calls
run — and neither can rule on a prefix of a round. The resolution is a restriction on _when_ early
dispatch is active, not a weakening of either gate. Anyone widening it is reopening that decision.

**Read P66.13's record before adding a permission layer or a run bound anywhere**: both now live in
`internal/enginecfg` and are built once rather than per entry point. Its own correction outlives it —
the item named four instances of one root cause and there were six, so **counting the instances of a
bypass by reading the file where it was found undercounts it.** `TestEveryEngineCallSiteDecidesItsGate`
enumerates them instead. P73.2, three days later, was the same failure mode in the same package:
`BuiltinOptions` never wired `cfg.Search`, so every non-daemon entry point ignored a configured search
provider.

**Two unwired-seam corrections that are still true and still unfiled as work:**

- **P67.5's recall path has no production callers at all.** `LoadRelevant`/`FormatEntries` are
  unwired — memory reaches the prompt through `Sources.Load()`, which injects both files whole and
  unfiltered. The dedupe, freshness and gotcha bias are built and tested; **wiring a caller is
  separate work nobody has filed**, and should be, before the next item that assumes scored recall is
  live.
- **P67.2's memoization is safe on only four of ten prompt sections.** Five read state Aegis mutates
  mid-conversation (skills, memory, context files, repo map, deferred tools). The volatile set is now
  the exhaustive, justified list of what breaks prefill reuse each turn.

**Method notes worth re-reading before filing or building anything new** (full detail in releases.md's
pass history): before measuring an optimization, check the instrument the rest of the system is
running on — this document has three times recorded a fixed instrument _inverting_ an already-acted-on
verdict. When a harness "just doesn't work", run it once with the tool calls printed before forming a
theory: the P71 sitting cost eleven minutes and invalidated half its own theory, and the two
hypotheses that survived were both arithmetic facts visible in the source that nobody had checked
because the interesting-looking ones were elsewhere. Every documented live-tier command needs
`-count=1`, or a re-run silently replays Go's cached verdict instead of reproducing. Mutation-test any
new numeric threshold. And **read the refutation records in releases.md before filing anything**
against `internal/provider`, `internal/ollamainfo`, `internal/repomap`, or scanner method resolution —
several obvious-looking gaps there have already been checked and answered.

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

## Up next

**Updated 2026-08-23**: **P76.1**'s both sessions have now run and closed, read-only, no code changes.
Session B (`internal/tui`) found one survivor, **P76.2** (Tier 2 — S). Session A (`internal/security`)
found one survivor, **P76.3** (Tier 3 — needs a trust-gate-or-disclosure design decision before code).
**P76.1** itself is done and demoted off this table — its remaining value is as a pointer to the
closure record, not open work. Document order in the Tier sections below is the same order as this
table, deliberately, so `scripts/roadmap-status.sh` and this ranking agree.

**The whole P74 batch — twenty items, P74.1 through P74.20 — has shipped**, P74.17 last, deliberately.
See [releases.md](releases.md) for every record.

| #   | Item                                                                                       | Tier / size  | Why now                                                                                                                                                                                                                                                                                                                                                                                                                                                                                           |
| --- | ------------------------------------------------------------------------------------------ | ------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | **P76.2** — quit doesn't cancel a running interactive-terminal command | Tier 2 — S | Filed 2026-08-23, survivor of P76.1 Session B. Mechanical fix, no dependency — three quit paths need one line each. |
| 2   | **P76.3** — a hostile repo can plant its own security-scan baseline to hide its own findings | Tier 3 | Filed 2026-08-23, survivor of P76.1 Session A. Real and currently exploitable against the exact adversarial case the `--network none` scanner subsystem defends against, but needs a trust-gate-or-disclosure design decision, not a one-line fix. |
| 3   | **The live-tier remainder** (P66.22, P38.1, P62.9, P65.2) — _parked by choice, 2026-08-16_ | Verification | Unchanged and still last for the same reason: **the user parked it**, not a dependency. **P38.1** needs permission to launch an unattended auto-approving agent, **P62.9** needs a _better task_ rather than more runs of the current one, and **P65.2**, **LLM-03**, **LLM-10** and **ARCH-04** now have what they needed — a surviving data dir and `aegis sessions trace <id>`, shipped as **P68.1** (2026-08-22) — so whenever this row is next picked up, the next sitting can actually judge them instead of reproducing the same unreadable evidence. |

**One item is deliberately off this list, Tier 4 with no fired trigger.** **P74.21** (filed
2026-08-21) is the half of P74.17's own roadmap entry that did not ship with it — see
[P74.17's Tier 3 record](#p7417--the-entire-local-model-story-is-one-boolean) for what shipped and what
didn't. It sits in Tier 4 rather than here because, exactly like P74.17 before it shipped, it has no
concrete cargo yet: nothing in the tree today needs a per-model prompt suffix or tool-description
override, only the flag-shaped repair behaviors P74.17 already covers. Promote once something concrete
asks for it. **P77.1** was this item's neighbor here until 2026-08-25, when the user gave it exactly the
concrete cargo it was waiting on — see [its shipped record](releases.md#p771-shipped-2026-08-25).

**Sizes are estimates from reading, not from building, and the batch had a known bias.** The P71
record is the caution: several of its rows were smaller than filed and one was larger. P74.17 itself
confirmed it again: the reading estimated it at Tier 3/L, and what shipped was a leaner, correctly-scoped
provider-decorator mechanism rather than the full tool-registration generalization the reading sketched
— the build found the real shape, same as the note here warned it would.

**This table outranks `scripts/roadmap-status.sh`.** That script reports open items in _document_
order. It still cannot see the cross-tier ranking. Use it for repo state and for the parse; use this
table for what to take.

---

## Open Work — Tier 1

**Status: 0 open.** **P74.1** (a path-scoped deny rule can never match `grep`) shipped 2026-08-20, the
same day it was filed — record in [releases.md](releases.md). Before it, the tier was empty for one
day; before that it was last occupied by **P71.1** and **P71.10**, both shipped 2026-08-19 the day they
were filed, and before them **P69.6** (2026-08-17) and **P66.5** (2026-08-16), which closed the last of
the P66 review's exploitable-on-the-day findings. Records for all of them are in
[releases.md](releases.md), and several correct the item they were built from — which is the part worth
reading before trusting [CodeReview.md](CodeReview.md) directly.

An item enters this tier when it is a real, currently-exploitable security or robustness gap that is
small and has no dependency. Nothing is currently open here.

---

## Open Work — Tier 2

**Status: 0 open.** **P68.1** (the live tier can now run a measurement it can read back) shipped
2026-08-22 — record in [releases.md](releases.md). Before it, this tier's most recent shipment was
**P74.15** (HTML
comments stripped from injected memory files) on 2026-08-21, right after **P74.14** (a malformed
dangling call gets its own message instead of the interrupted wording), right after **P74.13** (the
stable per-agent colour), right after **P74.12** (the eased token counter),
right after **P74.11** (the stall shimmer ramp), which shipped right after **P74.10** (the reduced-motion
setting), which shipped right after **P74.9**'s first
half (empty-result normalization, `builtin.NormalizeEmptyResult`), which shipped right after **P74.8**
(prose-tool-call salvage, head of the harness lane) on 2026-08-20, all right after **P74.7** (the real
terminal cursor, closing out the menu lane), which shipped right after **P74.5** (the pickers' selection
chrome, down to one cue) and **P74.6** (the filter affordance and match count), all in the same sitting
as directed, **P74.19** (the mouse-capture escape hatch), **P74.20** (the OSC 52 clipboard fix) and
**P74.18** (the selection-highlight bug) also 2026-08-20, **P71.2**, **P71.3**, **P71.4**, **P71.5**,
**P71.9**, **P72.2**, **P72.3** and **P73.2** on 2026-08-19, and **P66.25**/**P67.2**–**P67.5** before
them. Records in [releases.md](releases.md).

**Two sub-lanes from the 2026-08-19/20 batch are the rest of this tier's recent history, and they did
not block each other.** The selection/clipboard group and the
whole menu lane are both fully shipped (**P74.19**, **P74.20**, **P74.18** — see
[releases.md](releases.md#mouse-capture-becomes-a-config-choice-not-a-package-deal-2026-08-20-p7419);
**P74.5**, **P74.6**, **P74.7** — see
[releases.md](releases.md#pickers-drop-to-one-selection-cue-and-a-filter-hint-2026-08-20-p745-p746)
and [releases.md](releases.md#the-real-terminal-cursor-lands-on-the-focused-row-2026-08-20-p747)).
Local-model tool-call repair is now most of the way shipped: **P74.8** (see
[releases.md](releases.md#a-tool-call-written-as-text-becomes-a-call-2026-08-20-p748)), **P74.9**'s
first half (see
[releases.md](releases.md#an-empty-tool-result-becomes-a-named-placeholder-2026-08-20-p749)), and
**P74.14** (see
[releases.md](releases.md#a-dangling-call-whose-arguments-never-parsed-gets-its-own-message-2026-08-20-p7414))
have all shipped; the argument-shape repair P74.9 deferred is now P74.17's to carry, not a row of its
own. Motion and status is now fully shipped in `internal/tui`, now that **P74.10** (the reduced-motion
flag), **P74.11** (the stall shimmer ramp), **P74.12** (the eased token counter) and **P74.13** (the
stable per-agent colour) have all shipped (see
[releases.md](releases.md#there-is-no-reduced-motion-setting-fixed-2026-08-20-p7410),
[releases.md](releases.md#stall-becomes-a-visible-ramp-not-just-an-abort-2026-08-20-p7411),
[releases.md](releases.md#the-token-counter-jumps-instead-of-climbing-2026-08-20-p7412) and
[releases.md](releases.md#a-running-swarm-gets-a-stable-colour-not-three-grey-lines-2026-08-20-p7413)).
**P74.15** (stripping HTML comments from injected memory files) shipped 2026-08-21 — see
[releases.md](releases.md#injected-memory-files-stop-paying-for-their-own-authoring-notes-2026-08-21-p7415).
**P68.1** (the live tier can now run a measurement it can read back) shipped 2026-08-22, closing this
tier out — record in [releases.md](releases.md).

**P76.2** (filed 2026-08-23, first survivor of **P76.1** Session B) is now open in this tier — see
below.

### P76.2 — Quit doesn't cancel a running interactive-terminal command

**Filed 2026-08-23, out of P76.1 Session B** (the read-only `internal/tui` audit). `Run()`'s doc
comment at `internal/tui/tui.go:90-98` claims every quit path cancels the in-flight request's context.
That's true for `m.cancel` (the model-turn context) but not for `m.termRun.cancel` — the context behind
a command running in the interactive terminal pane (`ctrl+t`, `internal/tui/terminal.go:38-82`). None
of the three quit paths (`update_key.go:158-170`, `update_overlay.go:152-157`,
`update_slash.go:25-28`) touch it.

**Effect:** if a shell command is running in the terminal pane when the user quits — ctrl+c-to-quit,
confirming quit with "y", or `/quit`/`/exit` — the `execTermCmd` goroutine and the child process behind
it (via `sandbox.NewLocalBackend().ExecStreaming`) are never cancelled. They're orphaned past `p.Run()`
returning: a resource leak on exit, not a data-loss or security issue, and only reachable while
`termOpen` with a command actually running.

**Fix is mechanical, not a design question** — add `if m.termRun != nil { m.termRun.cancel() }` (or
equivalent) to each of the three quit paths, matching what `m.cancel` already does there, and update
the `Run()` doc comment's claim to match once it's actually true again.

Priority: Tier 2 — S, no dependency, self-contained.

---

## Open Work — Tier 3

**Status: 2 open — P76.1 (both sessions done, entry kept as a pointer), P76.3 (its Session A
survivor).** **P75.1** (per-block tool-result expand/collapse, filed 2026-08-21 the same day
the styling follow-up below shipped) shipped in full the same day, both slices — record in
[releases.md](releases.md#p751-shipped-in-full-2026-08-21). **P74.17** (per-model harness profiles)
shipped 2026-08-21 too, closing the tier out before this —
record in [releases.md](releases.md#local-model-repair-behaviors-resolve-per-model-instead-of-per-boolean-2026-08-21-p7417).
**P74.2** (the chrome removal — sidebar to an overlay,
auto-hidden scrollbar, title bar folded into the status line) shipped the same day, unblocking P74.3,
which itself shipped the same day and unblocked P74.4, which shipped the same day in turn.
The tier was emptied on 2026-08-18 (**P66.15**, **P67.6**, **P67.7**, **P67.8**, **P67.9**, then
**P70.4**) and the 2026-08-19 sitting kept it that way: **P71.8**, **P73.1** and **P72.1** all shipped
the day each was filed. Records in [releases.md](releases.md).

Two of those records constrain future work here and are summarized under [Decisions that outlive the
items that made them](#decisions-that-outlive-the-items-that-made-them): read **P67.7**'s before
touching `internal/engine`, and **P66.13**'s before adding a permission layer or a run bound anywhere.
Read **P66.14**'s (2026-08-16) before touching the compaction path, because the shared trigger it
introduced changed which numbers two already-shipped heuristics see.

An item enters this tier when it has real value but is larger or sequence-dependent — it blocks, or
is blocked by, other work. **P72.1 is the worked example of the "sequence-dependent" half**: it sat
here rather than being built the day it was filed because it needed a cold-start policy decided, not
a wire, and the resolution was to put four design questions to the user before writing anything.

### P76.1 — Audit the codebase's unread 26%: `internal/tui` and `internal/security`

**Filed 2026-08-21**, from reconciling a generic sprawl/hot-path/security refactor-audit prompt
(`research/CodeRefreactorPrompt.md`) against what already exists. Most of what that prompt asks for
is not open work — it's already-answered work. `CodeReview.md` (2026-08-15) ran a six-specialist
audit against exactly its three axes — sprawl (QUAL-01…15), hot paths (PERF-01…09), security
(SEC-01…14, VULN-01…12, ARCH-01…13) — through adversarial debate and arbitration, a rigor level a
fresh single-pass audit won't match. Most of the high-value findings are already shipped (QUAL-01/02/
03/06, ARCH-05/06, the P66 Tier-1 exploitable set, SEC-01's `.env` gate, VULN-11's flag-parity class),
and the rest is already triaged into Tier 4 with a stated reason (**P66.17**, **P66.23**, **P66.26**,
the QUAL-04/05/07/08/09 grab-bag) or explicitly WONTFIX'd (QUAL-05 — the TUI "god struct" is just the
Bubbletea Elm-architecture model, downgraded to Info absent a concrete bug). Re-running that prompt's
Phase 1 across the whole tree would mostly reproduce `CodeReview.md`.

**What it doesn't reproduce: `CodeReview.md`'s own Section 10.4 names the one gap it left standing.**
`internal/tui` and `internal/security` together are **26% of production Go** and were "still
substantially unread" by the six-specialist pass — the review's stated remaining exposure, not this
item's own guess. `internal/tui` is also the single largest package (16,080 LOC / 56 files) and has
had 20+ items shipped into it in the past week (the whole P74 batch plus P75.1) with no structural
pass behind any of them.

**Plan — deliberately multi-session, one phase per sitting, mirroring why the original review used
six separate specialist tracks instead of one:**

- **Session A — `internal/security` only.** Read-only, no code changes (the original prompt's own
  "AUDIT and STRUCTURAL BLUEPRINT, do not change files" constraint carries over — it's the right
  discipline regardless of which prompt asked for it). Before writing up any candidate finding, check
  it against `CodeReview.md`'s SEC-*/VULN-*/ARCH-* sections and against `releases.md` — if it's already
  there and shipped, or already parked in Tier 4 with a reason, it is not new work. Output: a short
  findings addendum, each item cited `file:line`, each marked CONFIRMED (traced and, where security-
  relevant, executed — see how VULN-01/02/11 were verified) or SUSPECTED, matching the existing
  review's own discipline rather than inventing a new one.
- **Session B — `internal/tui` only.** Same read-only rule, same cross-check requirement — in
  particular against QUAL-05 (already WONTFIX'd; don't re-litigate it) and against [Decisions that
  outlive the items that made them](#decisions-that-outlive-the-items-that-made-them), which records
  two rendering-mode decisions this package's owner already made deliberately.
- **Session C+ — file survivors as real roadmap entries.** Whatever passes the Session A/B cross-check
  becomes its own `P76.2`, `P76.3`, ... with its own Priority line and tier, sized by the existing
  [Tiering Criteria](#tiering-criteria) — not a blanket "Phase A wave" imposed ahead of knowing what
  the findings are.
- **Validation, every session:** `go build ./...`, `go vet ./...`, `go test -race ./...`, plus
  `staticcheck ./...` and `govulncheck ./...` — both already ran clean as of `CodeReview.md` Section
  10; re-run to catch drift since 2026-08-15, not to re-derive that baseline from scratch.

**Do not** re-audit packages `CodeReview.md` already covered in depth (`internal/engine`,
`internal/provider`, `internal/permission`, `internal/session`, `internal/tool/builtin`) without a
reason narrower than "general audit" — those already carry a debated, arbitrated finding set and a
live shipped-fix record; re-reading them from a generic prompt is exactly the duplicate work this
item exists to avoid.

**Both sessions ran 2026-08-23; the audit is closed.** Session B (`internal/tui`) found one survivor:
a resource leak where none of the three quit paths cancel `m.termRun`, so a command running in the
interactive terminal pane (`ctrl+t`) outlives `p.Run()` returning. Filed as **P76.2**, Tier 2.
Everything else in `internal/tui` checked clean — no other goroutine/channel issues, no contradiction
of the alt-screen/app-owned-frame or app-owned-selection decisions, QUAL-05 correctly left alone.

Session A (`internal/security`) found one survivor: `applyBaseline` (`security.go:397-424`,
`baseline.go`) reads `.aegis/security-baseline.yaml` straight from the *scan target* directory with no
workspace-trust gate, and the report surfaces suppressions only as a bare count — never which rule/CVE/
location was hidden. A hostile repository — exactly the threat model the `--network none` scanner
design defends against — can ship a baseline that pre-suppresses the finding for its own planted
vulnerability, and nothing distinguishes an operator-authored baseline from a repo-planted one. Needs a
trust-gate-or-disclosure design decision, not a one-line fix. Filed as **P76.3**, Tier 3. Everything
else in `internal/security` checked clean — network isolation is consistently correct and test-pinned
(including the gosec warm/analyze split's fatal-on-warm-failure invariant, actually enforced in code,
not just documented), no injection surface in `install.go`/`recon.go`'s argv construction, `RedactText`'s
fail-open posture confirmed as the deliberate P24.12/FIND-09 design. VULN-06 (already Tier 4/P66.23),
the XML recursion sweep, and nmap/nuclei flag-injection defenses were all correctly excluded as
already-covered.

**P76.1 itself is done** — both sessions complete, survivors filed as their own entries above/below.
This item's remaining value is as a pointer for anyone asking "has `internal/tui`/`internal/security`
had a structural pass" — yes, 2026-08-23, see `releases.md` for the closure record and **P76.2**/
**P76.3** for what it found.

Priority: Tier 3 — L, multi-session by design. No dependency, but Session A and B should not be
collapsed into one sitting — the prompt budget discipline `CLAUDE.md` documents elsewhere is the same
reason the original six-specialist review didn't run as one pass either.

### P76.3 — A hostile repo can plant its own security-scan baseline to hide its own findings

**Filed 2026-08-23, out of P76.1 Session A** (the read-only `internal/security` audit). `applyBaseline`
(`internal/security/security.go:397-424`, `internal/security/baseline.go`) reads
`.aegis/security-baseline.yaml` straight from the scan **target** directory, with no workspace-trust
gate. `Report.Format()` (`security.go:541-542`) then surfaces every suppression as a bare count —
`"Suppressed by baseline: N"` — never the rule ID, CVE, severity, or location of what was hidden.

**Why this matters here specifically:** the whole scanner subsystem is built around one threat model —
`gosec.go`'s own comments call out "an exfiltration path out of a hostile repo," which is why
multiscanner runs `--network none` with the workspace mounted. A baseline file is a gap in that same
model: a hostile or untrusted repository can ship a `.aegis/security-baseline.yaml` that pre-suppresses
the CVE/SAST finding for a vulnerability it planted itself. The operator, or a model reviewing the scan
output, sees what looks like a clean report plus an easy-to-miss count line — with no way to tell what
was suppressed without manually opening the baseline YAML. Nothing today distinguishes an
operator-authored baseline (legitimate: "we've triaged this and accept the risk") from a
repo-planted one (the exact thing the scanner exists to catch).

**Needs a design decision before code, not a one-line fix** — the two candidate directions don't have
to be exclusive:
- Gate baseline application on `config.WorkspaceTrusted`/`TrustWorkspace` (see `internal/workspacetrust`
  and the `CLAUDE.md` note on how that question must be asked), the same way `.aegis/.env` is already a
  documented, deliberate hole gated on trust rather than an oversight.
- Always list suppressed findings' identity (rule/CVE/severity/location) in the report regardless of
  trust — a baseline that can silently hide *what* it hid is the sharper problem even before trust
  enters into it.

Priority: Tier 3 — real, currently exploitable against the exact adversarial case the subsystem is
designed to defend, but not small-effort/no-dependency (disqualifying Tier 1) and not a one-line fix
(disqualifying Tier 2) — the report-disclosure half and the trust-gate half both need scoping first.

**The un-numbered inline-truncation/blackbox request closed out 2026-08-23.** Everything it asked for
was already shipped by the time it was reviewed — P74.2/P74.3/P74.4 (chrome removal, collapse-with-
expand, read/search grouping), P74.11/P74.12 (the stall ramp and eased token counter), P74.16 (overflow
clip-and-retry), and P75.1 (per-block expand). One thread it named — visibility into the model's actual
reasoning before it acts — is not covered by any of those and is real; it's filed on its own as
**P77.1**, since it's a design question rather than a continuation of that UI-polish work — shipped
2026-08-25, see [its record](releases.md#p771-shipped-2026-08-25). Full closure record for this
request: [releases.md](releases.md#the-inline-truncation-request-closes-out-2026-08-23-pxx1).

## Open Work — Tier 4

**Status: 25 open** — 8 pre-existing (all blocked or explicitly parked, none with a fired trigger),
6 from the P66 review batch, 5 from the P67 external-source reading, 5 from the P71 batch filed
2026-08-19 (**P71.6**, **P71.7**, **P71.11**, **P71.12**, **P71.13**), **P74.21** (filed 2026-08-21
the same day P74.17 shipped without it), and **P77.6** (filed 2026-08-25, spun out of P66.19). **P77.2**,
**P77.3**, **P77.4**, and **P77.5** (filed the same
day, same batch) all shipped 2026-08-24 — see [releases.md](releases.md#p774-shipped-2026-08-24).
**P77.1** shipped 2026-08-25 — see [releases.md](releases.md#p771-shipped-2026-08-25). **P78.1**–**P78.9**
filed and shipped 2026-08-26 — see [releases.md](releases.md#p781-p789-shipped-2026-08-26). **P70.3**
shipped 2026-08-18 and has left this tier. **P63.10** shipped 2026-08-21, taken opportunistically while
`internal/tui` was open for **P75.1** — record in
[releases.md](releases.md#p6310-shipped-2026-08-21).

The P66 entries here are **deliberately grouped grab-bags**: each collects the Low-severity residue of
one review domain. They are filed so no finding is lost, not because any of them should be scheduled.
Take one only when already working in that file. The P67 entries are a different kind of parked: each
is a capability Aegis does not have and nobody has asked for, filed with the specific trigger that
would make it worth building.

**The four P71 entries are a third kind, and two of them are parked by _choice_ rather than by
absence of demand.** **P71.6** (in-session response caching) and **P71.11** (window-derived research
budgets) are both blocked on **P71.8**: phasing changes the arithmetic under each, so fixing them
first would fit a constant to a regime about to change. **P71.7** (publication dates on search
results) waits on a keyed provider being the default, because that is the only backend where the date
is actually available. **P71.12** is different again — it is a filed **negative measurement**, kept
so the next reader does not re-derive an intuition this batch already tested and found small.

### P74.21 — The local-model harness still can't touch a prompt or a tool description

**Filed 2026-08-21, the day P74.17 shipped without it.** P74.17 built `internal/profile.Harness`,
resolved per `Request.Model`, and used it to move two response-repair behaviors (prose-tool-call
salvage, argument-shape repair) off the blanket `LocalPromptProfile()` boolean they were gated on
before. What it deliberately left alone is `builtin.Options.LocalProfile` itself:
`internal/tool/builtin/builtin.go:104` is still one bool, still deciding tool registration —
which families are deferred, which prompt caps apply, that `edit_file` moves behind `tool_search` while
the handle-based editors don't — for every local model identically, exactly as it did before P74.17.

The roadmap entry P74.17 shipped from sketched a fuller `Harness`: one that also carries
`PromptSuffix`, `ToolDescriptionOverrides` and a `DeferredTools` list, so a model-specific quirk could
add a line to the system prompt or rename a tool's description without every local model paying for it.
`deepagents` is still the reference for the shape — a provider-level profile layered under a
model-level one, additive, the same way `profile.NewResolver`'s `Override` already layers for the two
flag fields that shipped.

**Two of the four constraints the original entry named are still real and still apply here
unchanged:**

- **The prompt budget is test-enforced.** `TestEffectiveSystem_localProfileBudget` fails the suite when
  the local base prompt crosses `localBasePromptCeilingTokens`. A per-model `PromptSuffix` must be
  measured against that ceiling per model, not once.
- **Required scaffolding must not be excludable.** Aegis enforces the equivalent invariant at *test*
  time today (`TestEveryEngineCallSiteDecidesItsGate`, `TestEveryRegisterCallSiteDecidesTheLocalProfile`).
  A user-authorable per-model profile needs the same rejection enforced at *runtime*, because a config
  file is not a call site a build-time scan can audit.

**Promote when a concrete per-model prompt or tool-description need shows up** — a model whose own
quirk needs a system-prompt line, or whose tool descriptions need renaming to match vocabulary it was
trained on. P74.17 waited for exactly this kind of cargo (P74.8/P74.9) before it was worth building;
this is the same wait, one layer up.

Priority: Tier 4 — M. No fired trigger yet.

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
snippets _before_ fetching".

`searchResult` carries `title`, `urlStr`, `snippet` and nothing else (`web.go:203`), and DDG snippets
rarely contain a date. So the skill instructs the model to filter on a signal the tool does not
provide, at the one point where filtering is cheap. The only way to obey it is to fetch everything
first — which inverts the budget the skill is trying to hold, and on a small window is exactly the
behaviour **P71.5** makes unaffordable.

A fetched page usually carries `og:article:published_time` or a `<time>` element, which is a real
signal but only available _after_ the fetch this section is trying to avoid.

**Checked 2026-08-19, and weaker than assumed when this item was first filed: neither recommended
provider is a clean win.** Tavily's `/search` response schema is `title`, `url`, `content`, `score`,
`raw_content`, `favicon`, `images`, `id` — **no date field**. Brave's Web Search API supports
`freshness` as a _query_ filter (`pd`/`pw`/`pm`/`py`), which narrows _before_ searching rather than
labeling results _after_, and it was not possible to confirm from the public docs whether individual
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

**Promote when** **P71.8** lands — **it has, 2026-08-19.** Phasing changed the arithmetic: each
round is now a fresh, disk-grounded turn (P47.4) rather than a slice of one accumulating
conversation, so the per-_run_ budget this item measured is no longer the binding constraint the same
way. Re-derive the numbers against the phased shape (a round's own turn budget, not the whole run's)
before building this, rather than assuming the original math still applies unchanged.

Priority: Tier 4 — S. Was blocked on **P71.8** by choice; unblocked 2026-08-19, not yet promoted.

### P71.12 — Main-content extraction for `web_fetch` — measured, and smaller than it looks

**Filed 2026-08-19 as a negative measurement**, so the next reader does not re-derive it. `htmlToText`
(`web.go:257`) keeps every text node outside `script`/`style`/`noscript`, so a fetched page carries
its navigation, cookie prose, "This browser is no longer supported", breadcrumb and footer. The
obvious improvement is to prefer `<main>`/`<article>` and drop `nav`/`header`/`footer`/`aside`.

**It is worth less than it appears.** Measured across four `learn.microsoft.com` pages on 2026-08-19:

| page                                                 | raw HTML | after `htmlToText` | boilerplate | share |
| ---------------------------------------------------- | -------- | ------------------ | ----------- | ----- |
| `cloud-adoption-framework/ready/landing-zone/`       | 66,399   | 11,374             | 1,395       | 12%   |
| `architecture/networking/architecture/hub-spoke`     | 97,699   | 37,774             | 1,250       | 3%    |
| `defender-for-cloud/defender-for-cloud-introduction` | 64,194   | 17,305             | 1,218       | 7%    |
| `networking/design-guide/internet-ingress`           | 84,672   | 29,850             | 1,446       | 5%    |

Roughly **1.2–1.5 KB per page, 3–12%** — a few hundred tokens. The existing converter is already
doing the heavy lifting (66 KB of HTML down to 11 KB of text). Structural extraction is a real but
marginal win, and it carries a real risk: `<main>` heuristics fail differently per site, and a
mis-detected container silently returns _less_ than the naive walk.

**Promote when** already editing `htmlToText` for another reason, or if a site is found where the
boilerplate share is large enough to change a fetch's usable content. Do **not** schedule it as a
context-budget measure — **P71.5** is that measure, and it is worth roughly twenty times as much.

Priority: Tier 4 — S. Confirmed small. Do not schedule.

### P71.13 — Aegis could manage its own SearXNG container instead of only pointing at one

**Filed 2026-08-19**, out of the P71.2 discussion. `provider: searxng` (`websearch_providers.go:111`)
already exists and works — verified live against a user-hosted instance at `10.0.0.2:8787`, which is
now this repo's own `search.base_url` in `.aegis/config.yaml`. What doesn't exist is Aegis standing
one up itself, the way `internal/sandbox`/`aegis security build-image` already manage the scanner
containers' lifecycle (pull, run, health-check, teardown).

**Weighed against just recommending Tavily, and it is not a clean win — record why before building
it.** A self-hosted SearXNG proxies out to the same upstream engines (Google, Bing, Brave, DDG) a
zero-config scrape already hits, so it does not remove the challenge-page failure mode **P71.1**
detects — it moves that risk one layer down, into a container Aegis would now be responsible for,
and a datacenter/CI host's IP is _more_ likely to get blocked by those upstreams than a residential
one. It also introduces a hard container-runtime dependency for a feature that is currently
zero-infra (`go build` needs none — see CLAUDE.md), which is a bigger ask than the scanner containers
make, since those are opt-in security tooling rather than a chat-loop dependency.

**Do, if picked up:** scope it as strictly opt-in (`search.provider: searxng` with no `base_url` set
could trigger a "manage one for me" prompt, never a silent default), reuse the sandbox package's
container lifecycle rather than inventing a second one, and pin an engine list known not to earn an
instant block (Mojeek/Startpage/Brave over Google/Bing) — cross-check against **P71.2**'s untested
candidates, since a self-managed SearXNG and a direct scrape of the same engines are solving the same
problem two ways. Document the honest tradeoff (still dependent on the same upstreams, now with
container-ops cost) rather than pitching it as escaping "external parties" — it doesn't.

Priority: Tier 4 — M. No fired trigger; a real second search backend (**P71.2**) and confirming this
repo's own bring-your-own-instance config work were both dependencies-in-spirit, not blockers, and
both are now satisfied.

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

**Promote when** a measurement on the _current_ tree (post-P66.9) shows fsync cost still material on
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
record parses `<read-files>` tags out of _assistant_ text (LLM-15). The SSE idle watchdog counts
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

**Promote when:** VULN-04's _general_ form — schema validation for tool input — is worth its own item
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
planned. The user chose to act on a prioritized subset (2026-08-25) rather than wait for a trigger;
**GAP-02, GAP-03, GAP-08, and GAP-09 have shipped** (see below). GAP-05 was spun out to its own
future item, **P77.6**, rather than attempted in this pass. **GAP-04** (git support stops short of
branching, `internal/worktree` exposes no tool at all) and **GAP-07** (the MCP server side lags the
mature client — no `resources/*`, `prompts/*`, `sampling/*`, or `notifications/*`) remain open,
unpromoted.

- ~~GAP-02: no log rotation and no size cap~~ — `internal/logging` now rotates `aegis.log` at a
  configurable size (`log.max_size_mb`/`log.max_backups`, default 20MB/5 backups).
- ~~GAP-03: diagnostics have exactly one caller, nothing feeds back after an edit~~ — `write_file`,
  `edit_file`, `multi_edit`, `edit_section`, and `fill_marker` now fold LSP diagnostics for the
  changed file into their own result when a server is configured for it (`appendLSPFeedback`,
  `internal/tool/builtin/lsp.go`).
- ~~GAP-08: no test-runner feedback loop as a first-class concept~~ — a new deferred `run_tests`
  tool (`internal/tool/builtin/tests.go`) auto-detects the project's test command and parses
  go/pytest/jest/cargo summary output into structured pass/fail counts and failing test names.
- ~~GAP-09: structured outputs wired but used at exactly one call site~~ — `guard.LLMGuard`
  (`internal/guard/guard.go`) is now a second use of `provider.Request.Format`: it asks for (and,
  on a backend that honors Format, is constrained to) a `{"verdict":...}` JSON reply, tried first
  and falling through unchanged to the pre-existing text-heuristic `parseVerdict` on anything else —
  additive, not a rewrite of the tuned local-model parsing.

Priority: Tier 4 for the two remaining items (GAP-04, GAP-07) — no triggers. Do not build
speculatively.

### P77.6 — No OS-level process sandbox on Windows (GAP-05, spun out of P66.19)

`internal/sandbox`'s OS-level (no container runtime) backend covers darwin (`sandbox-exec`/seatbelt)
and linux (`bwrap`) in `detectOSSandbox()` (`internal/sandbox/os_sandbox.go`) — there is no
`case "windows"`. A Windows host without podman/docker/WSL installed falls all the way through to
`LocalBackend`: commands run directly on the host with no filesystem or network confinement at all,
the one platform where `sandbox.go`'s backend-selection order has nothing OS-level to fall back to.
Conspicuous because the rest of the Windows story (PowerShell shell tool, path handling, CI) is
otherwise handled well.

**Recommended direction, from the user (2026-08-25):** a Job Object (resource/kill-on-close
containment) plus a restricted access token (drop admin/write privileges outside the workspace) —
native Windows primitives, no new external dependency. AppContainer (the same primitive UWP apps
use) is stronger but was explicitly set aside as higher-complexity for a general-purpose CLI
subprocess model; revisit only if the Job Object + restricted token approach proves insufficient in
practice.

Priority: Tier 3/4 — down the road, not speculative-build-now. No code changes yet.

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

### P61.7 — Retry/terminal classification over _backend-echoed_ text (remainder)

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

**Re-verified 2026-08-06:** the note here previously said `sandbox.backend` "still defaults to
`local`" — that was stale even then; the actual default at the time was `"os"` (P4.7), not `local`.
Either way, `"os"` isn't `"container"`, so the promote condition was unmet.

**Update 2026-08-25:** `sandbox.backend`'s default is now `"container"`, cascading to `"os"` and then
`"local"` when no container runtime is available (`SelectSandbox`, internal/server/server.go). A host
with Docker/Podman running now gets the container backend by default. This satisfies the first half of
the promote condition below — the container backend is now the default a session lands on wherever
Docker/Podman is actually present — but the container-commit checkpoint mechanism itself (the actual
fix this item describes) has not been built yet.

**Promote when:** someone builds the container-commit checkpoint mechanism now that the default flip
has landed, or a user reports a rewind that restored files into an environment that no longer matched
them.

Priority: Tier 4 — no longer blocked, but speculative until someone is actually rewinding inside a
container.

### P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)

`newLoopDetector` is constructed inside `Run`, so its window resets every call. In the TUI and web UI
each user turn is a separate `Run`, so a model that loops _across_ user turns (re-reading the same file
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

### P65.4 — Resume is phase-granular, artifact-inferred, and only the drive has it

**What Aegis has, stated carefully — the gap is narrower than it first looks.** The phased drive
already resumes well: a phase whose files carry no `PENDING` marker costs zero model turns on re-entry,
and the whole reset ladder is built on re-entering from disk. Two limits:

- **It is phase-granular and the granularity is the artifact** — the oracle is the `PENDING` marker in
  the skill's own scaffolded files, so a crash 40 turns into phase 6 re-runs phase 6 from its start.
  Probably the right trade at the drive's scale.
- **It exists only because the _skill_ supplies the oracle.** A plain TUI/web-UI session, a cron job, a
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
web UI, ACP and MCP. Nothing here changes that. Four _optional_ seams are missing, each small on its
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

The comparison source uses this for _speculation_ — predicting the user's next prompt during idle time
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

**P78.1**–**P78.9** (filed and shipped 2026-08-26, from a five-track code-quality/sprawl/duplication
audit of the whole tree) — full record: [releases.md](releases.md#p781-p789-shipped-2026-08-26).

---

## Verification Work

**Status: 8 open** (**P68.4**, **P68.5** and **P68.6** filed 2026-08-17; **P68.2** and **P68.3** both
filed _and closed_ 2026-08-17, their records moved to
[releases.md](releases.md#the-template-that-ate-the-tool-calls-2026-08-17) and
[releases.md](releases.md#the-tiers-task-was-a-boolean-2026-08-17-p683) respectively;
**P65.3** closed 2026-08-16, its record is likewise in [releases.md](releases.md)). Every
item here has its code already written and merged — nothing below is a design or implementation task.
Each is closed by running a live-model harness and recording the result the item's closure condition
names, not by writing more code. They are **not tiered**: tiering answers "how urgent is this build,"
and there is no build left to prioritize.

**The 2026-08-16 sitting changed how these should be scheduled.** They were listed as four items
sharing one harness plus P62.8 waiting on hardware. After running it: the shared-harness premise
holds only for what the tier can _observe_, and four closure conditions (LLM-03, LLM-10, ARCH-04 and
P65.2) turned out not to be observable there at all — they needed **P68.1** first, which shipped
2026-08-22 (a session id that survives the test, plus `aegis sessions trace <id>` now printing the
compaction summary text, the calibration sample count and each turn's stop reason). Those four are
now observable whenever the next live sitting runs; nobody has judged them against real evidence yet.
P38.1 needs a permission rather than a schedule slot, and P62.9 needs a better task rather than more
runs. **This whole track is parked at the one remaining row of [Up next](#up-next) by choice**;
what is written below each item is what the run established, so a future sitting starts from evidence
rather than from the pre-run plan.

**P68.2 — The stock Qwen3 chat template deletes tool calls from history — filed and closed
2026-08-17.** Full record, measurements and the shipped detector (`ollamainfo.TemplateDropsToolCalls`)
are in [releases.md](releases.md#the-template-that-ate-the-tool-calls-2026-08-17). Kept here only
because it still hands something to open work: it's why **P62.9** needs a task replacement rather than
another run of the same one (a 6-run control arm and a concrete reason the 2026-08-16 failures weren't
purely competence), and why **P52.16**'s `toolResultEcho` measurement — taken on the affected
`qwen2.5-coder:1.5b` — is worth a cheap re-run nobody has done yet.

### P66.22 — The LLM-tier findings are all estimates; one live run converts them to measurements

The P66 review never ran a live model. **LLM-01, LLM-02, LLM-03, LLM-10 and ARCH-04 are all claims
about runtime behaviour against a local model, argued entirely from source.** The arbitration upheld
all five and they are well-argued — but CLAUDE.md is emphatic that this class of claim is settled by
measurement, and this document has twice recorded a fixed instrument _inverting_ an already-acted-on
verdict.

One `TestLiveWorkflow` run against `qwen3:14b-32k` answers all of them, and it is the same harness
P38.1, P62.9, P65.2's prompt half and P65.3's local half already need — so this costs no additional
setup if scheduled with them. That bundle is the one remaining row of the [Up next](#up-next)
ten, and it was one row precisely because running the harness without recording all five wastes the
setup. _(It ran on 2026-08-16 — see below for what that premise turned out to be worth. The remainder
is now row #6, parked.)_

**It was scheduled on 2026-08-16 and did not run: no model server was reachable** (nothing listening
on `:11434`). Nothing about the item changed — it is a measurement, so there is no partial credit and
nothing to substitute for it. Both of its gates shipped that day instead.

**It ran later the same day against `qwen3:14b-32k`. Three of the five closure conditions are met;
two are not observable from this tier.** Full record in [releases.md](releases.md) (_The live-tier
sitting, 2026-08-16_):

- **LLM-01 — met.** Local profile 4,871 provider-reported first-turn tokens against 8,393 default,
  neither clamped at the 16,384 window. With a realistic over-cap `CLAUDE.md`, the deterministic
  budget measures 6,383 estimated tokens against a 6,650 ceiling — the 11,611-token figure this item
  was filed on is three fixes stale. The same prompt costs **5,775 / 9,591** on
  `aegis-qwen35-9b:32k`: the ceiling is in `tokenest` units, not in any tokenizer's, and ~19% spread
  between two local models is normal rather than a regression.
- **LLM-02 — met, and it found the _next_ question.** Compaction fires exactly where the shared
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
  differ, so what a live run now measures is _whether the shared number is the right one_, not whether
  the two agree. The 2,048-vs-3,277 disagreement it describes no longer exists.
- **LLM-03** is fixed: the calibration gate is now a positive backend identification, so it fires on
  the `openai` + `:11434/v1` path. The run should confirm a non-zero sample count rather than
  discovering there is none.
- A **third expectation is retired by a side effect**: the prune-thrash the P62.7 minimum-yield rule
  rate-limits was a _consequence_ of the LLM-02 disagreement, and on the P62.7 fixture it disappears
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
dated log (2026-07-21 through 2026-08-09) is in [releases.md](releases.md) (_P38.1 re-test log_).
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
`edit_file`, because an anchored edit asks the model to _reproduce_ text rather than only produce it.
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

**P68.3 — The tier's task was a boolean, so it could not rank anything — filed and shipped
2026-08-17.** Full record, the grading rubric and the measured separation
(`aegis-qwen35-9b:32k` 10.7/12 vs `qwen3:14b-32k` 2.7/12, complete at n=3) are in
[releases.md](releases.md#the-tiers-task-was-a-boolean-2026-08-17-p683). Kept here only because later
items build on it: it shipped `TriageTask` (`internal/eval/triagetask.go`) as the tier's ranking
instrument, kept `SeededBugTask` as the control, and is the closure condition **P62.9** and **P68.4**
both cite.

### P68.4 — The triage rubric's measuring band sits below the strongest local model

**Filed 2026-08-17, from a temperature A/B that measured nothing — twice.** P68.3 shipped a task that
ranks _models_ well (9b 10.7 vs 14b 2.7, complete separation at n=3). The attempt to use it for the
next question — do the sampling parameters `docs/local-model-tuning.md` recommends actually help? —
found it cannot rank _configurations_, because both available substrates sit against a rail:

| substrate             | temp 0.2      | temp 0.6   | reading                                        |
| --------------------- | ------------- | ---------- | ---------------------------------------------- |
| `aegis-qwen35-9b:32k` | 12, 12, 12    | 12, 12, 12 | **ceiling** — rubric exhausted                 |
| `qwen3:14b-32k-fix`   | 3, 3, 3, 3, 3 | 3, 3, 3    | **pinned low** — one repeated minimal strategy |

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
- points for _not_ touching the three files the task never mentions, which the 14b family edits.

**Until this lands, `docs/local-model-tuning.md`'s sampling section stays labelled reasoned-not-
measured**, and it says so in the document. That is the honest state: two experiments were run and
both were void, which is different from "tested and found not to matter", and the page must not drift
into implying the latter.

### P68.5 — P52.16's `toolResultEcho` measurement was taken through a defective template

**Filed 2026-08-17.** P52.16's echo experiment — 32/40 bare → 38/40 echoed, the measurement the whole
`toolResultEcho` mechanism rests on — was run on **`qwen2.5-coder:1.5b`**, which P68.2's detector
flags as shipping the `else if … .ToolCalls` template. That experiment measured tool-result
_correlation_ through a renderer that was deleting the calls being correlated, which is close to the
worst possible confound for it: the echo's stated purpose is carrying an association "in content
where the protocol cannot carry it in metadata", and the protocol was losing even more than assumed.

Nothing is retracted here. The +15pp may well survive — the echo could be _more_ valuable when the
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

This is a model-behaviour observation, but it is not obviously _only_ that, which is why it is filed
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

**What would close it:** read one such run's trace — **P68.1** (shipped 2026-08-22) means a future run's
data dir can now survive and be read with `aegis sessions trace <id>` — and establish whether the
model ever attempted a write tool and failed, or never selected one. Those are an Aegis problem and a
model problem respectively, and the run as recorded cannot tell them apart.

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
completely at **n=3** (10.7 vs 2.7), where this task returned p ≈ 0.45 at n=6. Re-running _this_
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
the compressed prose holds; what is unmeasured is now only turn _cost_ against an exposed-`edit_file`
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
_generation_ and structured fill is _completion_, and every measurement in the P38.x line says local
models degrade on the first and hold up on the second — this is the last unstructured-prose ask left in
the engine, at the moment the model's context is fullest.

**Promote when:** P38.1's re-run is done and the live tier is free — the prompt change wants the same
harness, so running them together costs one setup instead of two.

**2026-08-16: the harness cannot see what this item needs to judge.** The live tier ran twenty-two
compactions across the two P62.2 arms — the skeleton prompt was exercised repeatedly — but a
compaction's _summary text_ never reaches the SSE stream, so the run reports that compaction happened
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
because what mattered was _where in the window_ the prune landed relative to the backend's
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
