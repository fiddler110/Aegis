# Aegis Capability Roadmap

**Last updated:** 2026-08-20. This document tracks only **open** work and what's next. For
shipped-feature history, batch origins, pass-by-pass narrative, refutation records and full design
rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading with a
`Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so keep it when
adding items.

---

## Status

**54 open items: 46 build + 8 verification-only.** The empty-tier state the last revision described
lasted one day. **P74 — twenty items filed 2026-08-20 — is the fresh pass that advice called for**,
and it did what the P71 batch did: a comparative reading of two external agent implementations
against this tree, with every claim about *this* side checked at the file and line cited. Tier 1 has
one entry again (a permission-rule gap proved against the live gate, not read off a diff), Tier 2 has
fourteen, and Tier 3 has five in a stated dependency order.

**The batch splits into two lanes that do not block each other**, so they can be worked in parallel or
in either order: a **TUI lane** (thirteen items, answering a standing user complaint that the interface
reads as an application where comparable agents read as a terminal) and a **harness lane** (six items
from `langchain-ai/deepagents`, headed by the per-model profile mechanism Aegis compresses into one
boolean). The twentieth, **P74.1**, belongs to neither and goes first because it is Tier 1.

Tier 4 remains what it was — 25 entries each filed with a reason not to schedule them, and not a queue
to promote from.

**Shipped history lives in [releases.md](releases.md), not here.** This document tracks only open
work; a completed item's full record — what it was, what building it found, and what was measured to
close it — moves there. The most recent sittings, newest first:

- **2026-08-19** — **P72.3** (a resident-set claim now owns its models' residency, not just their
  windows), **P72.1** (the serving context window is solved from a stated VRAM budget at startup:
  `16000 → 82944` measured on this machine), **P73.2**, **P73.1**, **P71.8**, and the eight-item
  web-research batch **P71.1/P71.2/P71.3/P71.4/P71.5/P71.9/P71.10** plus **P72.2**. Fourteen items in
  three sittings, most of them filed and shipped the same day — the first time in this document's
  history that a filed batch and its build have landed together, and twice over the day an item was
  filed *by* the live verification of the item before it (P73.1 out of P71.8, P73.2 out of P73.1,
  P72.3 out of P72.1).
- **2026-08-18** — **P70.4**, **P70.1**, **P70.2**, **P70.3**, and **P66.15**/**P67.6**/**P67.7**/
  **P67.8**/**P67.9**.
- **2026-08-17 and earlier** — **P69.1**/**P69.5**/**P69.6**, **P66.13**, the P66 review batch, and
  the rest.

**Every shipped item above was closed against a live-verified test or a live probe run on this
machine, recorded in its release entry.** None of it is asserted from reading a diff. That standard
is the one thing from the shipped record that constrains future work here.

- **Tier 1:** 1 — **P74.1** (a path-scoped deny rule can never match `grep`), filed 2026-08-20 and
  proved against the real gate. Empty for one day before that, since **P71.1**/**P71.10** shipped
  2026-08-19. An item enters here only as a real, currently-exploitable gap that is small and
  unblocked.
- **Tier 2:** 15 — **P68.1** (the instrumentation gap the live tier found), deliberately off the
  ranked list because it travels with the parked live-tier row, plus fourteen P74 entries: **P74.18**
  (the selection-highlight bug, ranked third overall), **P74.20** (the OSC 52 clipboard fix),
  **P74.19** (the mouse-capture escape hatch), **P74.5**, **P74.6**, **P74.7** (menus),
  **P74.8**, **P74.9** (local-model tool-call repair), **P74.10**, **P74.11**, **P74.12**, **P74.13**
  (motion and status), **P74.14**, **P74.15**.
- **Tier 3:** 5, all P74 — **P74.2**, **P74.3**, **P74.4** (the TUI layout chain, strictly ordered),
  **P74.16** and **P74.17**. Empty for two days before that.
- **Tier 4:** 25 — five from P71 (**P71.6**, **P71.7**, **P71.11**, **P71.12**, **P71.13**), six from
  the P66 review (**P66.17**, **P66.18**, **P66.19**, **P66.20**, **P66.23**, **P66.26**), five from
  P67 (**P67.10**–**P67.14**), and the nine pre-existing: **P65.4**, **P65.5**, **P64.4**, **P64.5**,
  **P61.7** (remainder), **P60.3**, **P52.14**, **P25.9**, **P63.10**.
- **Verification:** 8. See [Verification Work](#verification-work) for the track's own status line.

Tiers 1-4 are **build work** — every item there requires writing code that doesn't exist yet.
**Verification work** is its own track, not a tier: every item in it is code that is *already
written*, sitting behind one gate — a live-model run producing evidence the item's closure
condition names. Mixing the two under one tiering scheme was misleading a reader into treating
"go run a test" and "go design and build a feature" as the same kind of next action.

**Everything left in the verification track is blocked on something other than a model server**,
which is why parking it costs little. P38.1 needs permission to launch an unattended auto-approving
agent; P62.9 needs a *better task* rather than more runs of the current one; LLM-03, LLM-10, ARCH-04
and P65.2 all need a session trace from a run whose data dir survives, which is **P68.1**. Only
P62.8 is still purely waiting on hardware.

**No Tier 4 build item currently has a fired trigger** (re-verified 2026-08-15: `sandbox.backend`
still defaults to `"local"`, `lsp.Manager` is still one shared daemon singleton, both TUI
asymmetries in P63.10 are still present as described) — see each entry's **Promote when** for what
would change that. Two of them, **P71.6** (response caching) and **P71.11** (window-derived budgets),
were held pending phasing — "setting them first fits a constant to a regime about to change" — and
that regime changed when P71.8 landed; the reason they were parked no longer applies, so re-check
them rather than assuming Tier 4 still fits. **P71.12** is the opposite case: a filed *negative*
measurement (main-content extraction is worth 3–12% per page, because the existing converter already
takes 66 KB of HTML down to 11 KB of text), recorded so nobody re-derives it. Explicitly do not
schedule.

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
  `toolResultStorage` were legible only through call sites. Where an entry's claim about *their*
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
  reading of *observed interface behaviour* — glyphs, layout decisions, gating thresholds — and needs
  an independent Bubbletea implementation written from this document. The practical point reinforces
  the legal one: it is React and Ink, and none of it is portable anyway.
- **Every claim about Aegis in these entries was checked against this tree**, at the file and line
  cited. **P74.1 goes further and was proved by running the real gate** — a throwaway test against
  `NewRuleGate` with the actual `grep` schema, not a reading of `subjectFor`. Claims about either
  external side were not checked and cannot be: treat them as motivation, never as a specification.

The batch was filed from a review artifact whose finding ids differ from the roadmap's, because the
roadmap renumbers into implementation order. The mapping, for anyone reading the two side by side:

| Roadmap | Artifact | Roadmap | Artifact | Roadmap | Artifact |
|---|---|---|---|---|---|
| P74.1 | SEC-1 | P74.7 | TUI-5 | P74.13 | TUI-9 |
| P74.2 | TUI-1 | P74.8 | DA-2 | P74.14 | DA-5 |
| P74.3 | TUI-2 | P74.9 | DA-3 | P74.15 | DA-6 |
| P74.4 | TUI-3 | P74.10 | TUI-8 | P74.16 | DA-4 |
| P74.5 | TUI-4 | P74.11 | TUI-7 | P74.17 | DA-1 |
| P74.6 | TUI-6 | P74.12 | TUI-10 | P74.18 | *(new, 2026-08-20)* |
| | | | | P74.19 | *(new, 2026-08-20)* |
| | | | | P74.20 | *(new, 2026-08-20)* |

**The P66 entries here are deliberately grouped grab-bags**, each collecting the Low-severity residue
of one review domain, filed so no finding is lost rather than because any of them should be
scheduled. Take one only when already working in that file. The review itself — six specialist
reviewers, an adversarial debate and a static-analysis pass, 70 findings against HEAD `3c2b57b` — is
in [CodeReview.md](CodeReview.md) with per-finding evidence. **Read the corrections in releases.md
before acting on that document directly:** several shipped items contradict the finding they were
built from (VULN-03's suggested `::ffff:0:0/96` addition would have blocked the entire public
internet; LLM-04 drops *every* tool call on a 1-based backend, not only trailing ones).

### Decisions that outlive the items that made them

**Three trust-posture questions were answered on 2026-08-18 and they do not all point the same way,
which is the point.** The swarm mailbox **is** wrapped as untrusted (P70.2) and so is a sub-agent's
result (P70.4), because in both cases content crossed a boundary before being relayed onward;
`security_scan`'s workspace-derived output is **deliberately not** wrapped (P70.3) because a file the
model can already read directly is not a boundary crossing. Zero trust is the stated posture for
*ingestion* and for *relayed* content, not a rule that every byte gets a marker. Settle the next such
question against those three, not afresh.

**The TUI keeps alt-screen and the app-owned frame. Decided 2026-08-20, after two wrong answers.** The
question was how to get native-feeling scroll and selection. The first answer was "move to document
flow and delegate scroll, selection and search to the terminal" — a 4–6 day commit/live rewrite that
would have retired `/search`, deleted `selection.go`, and **silently given up re-wrap on resize**, since
content hard-wrapped and printed into scrollback can never reflow. The user caught it by asking whether
resize would still re-wrap.

**What the check found is the reusable part.** The comparison client ships *two* rendering modes, and
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
more: the P53.2 loop guard can *abort* a run on the complete round's signature, and the pre-tool-round
budget gate exists specifically so a turn whose own usage crosses the cap stops before its tool calls
run — and neither can rule on a prefix of a round. The resolution is a restriction on *when* early
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
running on — this document has three times recorded a fixed instrument *inverting* an already-acted-on
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

**Rewritten 2026-08-20, when the P74 batch filled the table the P71–P73 chain had emptied.** Document
order in the Tier sections below is the same order as this table, deliberately, so
`scripts/roadmap-status.sh` and this ranking agree for the first time.

**The ranking has three rules and they are worth reading before picking a row.**

1. **P74.1 goes first because it is Tier 1** — currently exploitable, unblocked, and about an hour of
   work. Nothing else in the batch competes with that.
2. **P74.2 → P74.3 → P74.4 is a chain**, and its direction was settled on 2026-08-20 after two wrong
   answers: **Aegis keeps alt-screen and the app-owned frame**, because that is what makes resize
   re-wrap work and it is the mode the comparison client's own staff run. P74.2 is a one-sitting chrome
   removal, not the 4–6 day document-flow rewrite it was filed as twice. The rationale and the two
   method notes that came out of it are under [Decisions that outlive the items that made
   them](#decisions-that-outlive-the-items-that-made-them) — **read that before reopening this**.
   **P74.18 is deliberately ranked third out of tier**: it is XS, it is the one outright bug in the
   selection path, and selection quality is the capability the direction decision named.
3. **Everything else is parallel.** The menu rows (P74.5–P74.7), the harness rows (P74.8, P74.9,
   P74.14–P74.17) and the motion rows (P74.10–P74.13) touch disjoint files, so a second sitting can
   take a different lane without a merge conflict. The one intra-lane order that matters is
   **P74.10 before P74.11 and P74.12**: both add animation, and both have to respect the flag P74.10
   introduces.

| # | Item | Tier / size | Why now |
|---|------|-------------|---------|
| 1 | **P74.1** — a path-scoped deny rule can never match `grep` | Tier 1, S | The only currently-exploitable row. `deny read_file(secrets/**)` holds and `deny grep(secrets/**)` is a silent no-op returning matching lines from the same files, and `WarnUnmatchableRules` does not warn because the schema check and the extraction switch disagree. Proved against the real gate. |
| 2 | **P74.2** — drop the chrome, keep alt-screen | Tier 3, S | **Rewritten 2026-08-20 after the direction correction**: sidebar to an overlay, scrollbar auto-hidden, title bar folded into the status line. One sitting, and it keeps re-wrap, `/search` and drag-selection, all of which the document-flow version would have traded away. |
| 3 | **P74.18** — selection highlights with SGR-7 inverse | Tier 2, XS | Out of order deliberately: it is Tier 2 but it lands on the exact capability the direction decision named important, and it is the one outright *bug* in the selection path. Fragments visibly over every chroma-highlighted diff and `read_file`. |
| 4 | **P74.20** — no OSC 52, so copy is broken over SSH | Tier 2, XS | A silent wrong result, not a preference: `copyToClipboard` shells to `pbcopy`/`xclip`/`wl-copy`, which over SSH is the remote machine's clipboard. Fixes `/copy` too, which a mouse-capture change does nothing for. |
| 5 | **P74.19** — mouse capture is unconditional | Tier 2, XS | Pairs with the row above. A `tui.mouse: off` key is the only configuration that gives terminal-native selection *and* re-wrap, since releasing capture does not require releasing alt-screen. Escape hatch, not a default. |
| 6 | **P74.3** — one tool block, not two events | Tier 3, S | Blocked by P74.2 on the gutter's indentation constant. Stops printing the tool name on both the call and the result line, and hangs the result off a `⎿` continuation carrying a summary. |
| 7 | **P74.4** — collapse runs of reads and searches | Tier 3, M | Blocked by P74.3's gutter shape. The largest density win in the batch, and *more* valuable in a bounded viewport than in document flow — which is how the comparison client treats it too. |
| 8 | **P74.5** — lighten the pickers | Tier 2, S | Head of the menu lane, and the most direct answer to the specific complaint that the menus feel wrong. Three heavy selection signals become one. |
| 9 | **P74.6** — a filter affordance and a match count | Tier 2, XS | Same lane, same file. Removes the only genuinely undiscoverable interaction in the app for the cost of one row. |
| 10 | **P74.7** — move the real terminal cursor onto the focused row | Tier 2, XS | Same lane. Disproportionate "native" payoff per line changed, and it is the accessibility half of the menu work. |
| 11 | **P74.8** — salvage tool calls that arrive as prose | Tier 2, S | Head of the harness lane and the cheapest large reliability win for local models. A provider decorator beside `retry.go` and `numctx.go`; independent of everything above. |
| 12 | **P74.9** — normalize empty tool results, repair argument-shape drift | Tier 2, S | Pairs with P74.8 and lands in the same sitting. An empty result a model cannot distinguish from a failure is a loop that reads as a model defect. |
| 13 | **P74.10** — a reduced-motion setting | Tier 2, XS | Take before P74.11 and P74.12, which both add animation that has to honour it. Accessibility item *and* a CPU item on a machine already spending everything on inference. |
| 14 | **P74.11** — show stall as a visual state, not just an abort | Tier 2, S | Between "working" and a 900-second abort there is currently nothing, and on a local model a 90-second silence is both normal and indistinguishable from a hang. |
| 15 | **P74.12** — ease the token counter instead of jumping it | Tier 2, XS | Polish, and cheap: `animStep` already provides the clock. |
| 16 | **P74.13** — give each running sub-agent a stable colour | Tier 2, XS | Independent of every other row. A three-agent swarm is currently three near-identical grey lines. |
| 17 | **P74.14** — distinguish a malformed dangling call from a cancelled one | Tier 2, XS | One branch in `repairOrphanedToolUses`. "Possibly completed" is right for a cancellation and wrong for arguments that never parsed. |
| 18 | **P74.15** — strip HTML comments from injected memory files | Tier 2, XS | Free bytes against a test-enforced prompt ceiling, and it makes tool-managed markers in project memory viable. |
| 19 | **P74.16** — a reactive clip path on context overflow | Tier 3, M | Sequence-dependent on nothing, but larger than the rest of the harness lane and it touches the truncation posture table, so it wants a clear run. |
| 20 | **P74.17** — per-model harness profiles | Tier 3, L | Deliberately last. It is the largest item in the batch and the one that pays off over time rather than on the day; take it once P74.8 and P74.9 have given it concrete cargo to carry, or it gets built as an empty abstraction. |
| 21 | **The live-tier remainder** (P66.22, P38.1, P62.9, P65.2) — *parked by choice, 2026-08-16* | Verification | Unchanged and still last for the same reason: **the user parked it**, not a dependency. **P38.1** needs permission to launch an unattended auto-approving agent, **P62.9** needs a *better task* rather than more runs of the current one, and **P65.2**, **LLM-03**, **LLM-10** and **ARCH-04** need a surviving data dir and `aegis sessions trace <id>`, which is **P68.1**. Take P68.1 first whenever this row is picked back up, or the sitting produces the same unreadable evidence again. |

**One item is deliberately off this list: P68.1** (Tier 2, S). It is what row 18 needs before it is
worth re-running — the eval tier deletes the database holding the trace its own closure conditions are
written against. It travels with that row, so it is off the list while the row is parked.

**Sizes are estimates from reading, not from building, and the batch has a known bias.** The P71
record is the caution: several of its rows were smaller than filed and one was larger. Treat XS/S as
"one sitting" and M/L as "expect the build to find something the reading did not" — which for P74.17
in particular is close to certain, because a profile mechanism only reveals its real shape once a
second model needs a different one.

**This table outranks `scripts/roadmap-status.sh`.** That script reports open items in *document*
order. For P74 the two now agree by construction, but the script still cannot see the cross-tier
ranking (P74.2 outranks eleven Tier 2 rows), cannot see the P74.2→P74.3→P74.4 chain, and cannot see
that **P68.1** is deliberately off the list. Use it for repo state and for the parse; use this table
for what to take.

---

## Open Work — Tier 1

**Status: 1 open — P74.1**, filed 2026-08-20. The tier was empty for one day; before that it was last
occupied by **P71.1** and **P71.10**, both shipped 2026-08-19 the day they were filed, and before them
**P69.6** (2026-08-17) and **P66.5** (2026-08-16), which closed the last of the P66 review's
exploitable-on-the-day findings. Records for all of them are in [releases.md](releases.md), and
several correct the item they were built from — which is the part worth reading before trusting
[CodeReview.md](CodeReview.md) directly.

An item enters this tier when it is a real, currently-exploitable security or robustness gap that is
small and has no dependency.

### P74.1 — A path-scoped deny rule can never match `grep`

**Filed 2026-08-20 and proved against the real gate, not read off `subjectFor`.** A deny rule intended
to keep a directory out of the model's context holds for `read_file` and is a silent no-op for `grep`,
which returns matching lines from the same files.

`subjectFor` (`internal/permission/rules.go:183`) extracts the string a rule's glob matches against by
switching on capability. The `CapRead` branch returns `firstNonEmpty(args.Path, args.FilePath)`. The
`grep` tool's schema (`internal/tool/builtin/search.go:301`) has neither: it takes `pattern`, `glob`
and `ignore_case`, and always searches the whole workspace root — `effectiveRoot(ctx, t.root)` — with
`glob` as the only narrowing. So the extracted subject is always the empty string,
`normalizePathLike("")` cleans to `"."`, and no path pattern matches it.

**The safety net misses it for a reason worth recording separately from the fix.**
`WarnUnmatchableRules` asks `toolHasSubjectField`, which introspects the tool's *declared input
schema* for any of the six names in `subjectFieldNames`. `grep` declares `pattern`, which is on that
list, so the check passes — but the `CapRead` branch of `subjectFor` returns before `pattern` is ever
consulted. **The schema-level check and the extraction-level switch disagree, and the warning is
defeated precisely where it is needed.** Any fix has to close both halves or the next tool with the
same shape reintroduces it silently.

**Evidence.** A throwaway test against `NewRuleGate` with a stub carrying `grep`'s real schema and
capability: rule `deny grep(secrets/**)`, input `{"pattern":"AWS_SECRET"}` → `allowed=true`,
`reason=""`, and `WarnUnmatchableRules` emitted nothing. Reproduce before fixing; do not take this
paragraph as the test.

**Two candidate fixes, and the second is the one to prefer.** Adding `glob`/`pattern` to the `CapRead`
extraction is the smaller change and is wrong in the general case — a `glob` is a filter, not a scope,
and a `grep` with no `glob` at all still walks everything. The better shape is the one
`deepagents`' `_fs_interrupt.py` uses: classify each filesystem tool as **exact** scope (the call
operates on the named path — `read_file`, `write_file`, `edit_file`) or **bulk** scope (the path
argument is a search root and any descendant may surface — `grep`, `glob`, `ls`), and for a bulk call
fire whenever the searched subtree *intersects* a rule's pattern, unconditionally when the call names
no root at all. That matches what a blast-radius rule means, and Aegis's globs already span path
separators (`globToRegexp`) for exactly that reason.

**Closure condition:** a test in `internal/permission` asserting `deny grep(secrets/**)` denies a
pathless `grep`, plus a second asserting the schema/extraction agreement directly — for every
registered tool, if `toolHasSubjectField` says a rule can match it, `subjectFor` must return non-empty
for some input satisfying its schema. The second is what stops the regression class rather than the
instance.

Priority: Tier 1 — currently exploitable, small, no dependency. Take first.

---

## Open Work — Tier 2

**Status: 15 open — fourteen P74 entries filed 2026-08-20, plus P68.1**, which stays deliberately off
the ranked list because it travels with the parked live-tier row. Everything else this tier held has
shipped: **P71.2**, **P71.3**, **P71.4**, **P71.5**, **P71.9**, **P72.2**, **P72.3** and **P73.2** on
2026-08-19, and **P66.25**/**P67.2**–**P67.5** before them. Records in [releases.md](releases.md).

**Document order below is priority order**, so the fourteen P74 entries come first and P68.1 sits last
as the parked one. The note this tier has carried since 2026-08-19 held: a new Tier 2 entry comes from
a review pass or a fired trigger, not from what is already filed — and the P74 batch is another review
pass, not a promotion from Tier 4.

**Four sub-lanes, and they do not block each other.** **P74.18** (the selection-highlight bug, ranked
third overall despite its tier) **P74.20** (the OSC 52 clipboard fix) and
**P74.19** (the mouse-capture escape hatch, off by default) are the selection/clipboard group. Menus (**P74.5**–**P74.7**) all live in
`internal/tui/dialog.go` and want one sitting. Local-model tool-call repair (**P74.8**, **P74.9**)
lives in `internal/provider` and `internal/tool/builtin`. Motion and status (**P74.10**–**P74.13**)
live in `internal/tui`, and **P74.10 must precede P74.11 and P74.12**. The last two (**P74.14**,
**P74.15**) are one-branch items that fit anywhere.

### P74.18 — Drag-selection highlights with SGR-7 inverse, which fragments over syntax highlighting

**Filed 2026-08-20 out of the P74.2 correction**, while comparing Aegis's selection implementation
against the alt-screen one it should have been read against in the first place.

`selection.go:305` highlights the selected range with `lipgloss.NewStyle().Reverse(true)` — SGR-7,
which swaps foreground and background **per cell**. Over uniform text that looks fine. Over
chroma-highlighted content it does not: every token that carries its own colour inverts to a different
background, so the selection reads as a ragged stripe of mismatched blocks instead of one contiguous
region. Aegis applies chroma to diffs (P16.3) and to `read_file` output (P16.2), which is exactly the
content people drag-select.

The comparison client hit this and moved off it. Its theme carries a dedicated `selectionBg` token
whose comment is the whole finding:

> *"Solid bg that REPLACES the cell's bg while preserving its fg — matches native terminal selection.
> Previously SGR-7 inverse (swapped fg/bg per cell), which fragmented badly over syntax
> highlighting."*

**The fix is the same shape:** add a `selectionBg` role to `colorScheme` (both schemes — it has to
clear text in each) and rewrite the highlight to set background only, leaving foreground untouched.
That is what a real terminal's selection does, which is why it reads as native.

**Two details worth getting right.** The token must be picked for contrast against *both* schemes'
foreground tiers, not just eyeballed on dark — the light scheme's `fgBase` is near-black and needs a
pale selection, the dark scheme's needs the inverse.

**And do not "fix" the search highlighting while you are in there.** Inverse did not disappear from the
comparison client, it moved: `selection.ts:914` uses the solid background for *selection*, while
`searchHighlight.ts:84` still uses `withInverse` for *search matches*. Aegis is the exact mirror image
— `highlightSearchMatches` (`selection.go:284`) already sets an explicit `colBrandFg`/`colBrandBg`
pair, which is the better of the two treatments, and only the selection overlay inverts. The scope of
this item is `selection.go:305` and nothing else.

**Closure condition:** a test asserting the selection style sets a background and does not set
`Reverse`, plus a rendered check over a chroma-highlighted line confirming every cell in the range
carries the same background.

Priority: Tier 2 — XS, no dependency, and it lands directly on the capability the 2026-08-20 direction
decision named as important.

### P74.20 — The clipboard has no OSC 52 path, so copy is broken over SSH

`copyToClipboard` (`internal/tui/view.go:655`) switches on `runtime.GOOS` and shells out: `pbcopy` on
darwin, then `xclip` / `xsel` / `wl-copy` on linux, erroring with "no clipboard tool found" when none is
present. Every one of those writes to the clipboard of the machine **Aegis is running on**.

Run Aegis over SSH — or in a container, or WSL reaching a Windows terminal — and that is the wrong
machine. `/copy`, drag-selection, and every other copy affordance silently succeed and put the text
somewhere the user cannot reach. On a headless remote box the `xclip`/`xsel`/`wl-copy` lookup fails
outright and the feature just reports an error.

**OSC 52 is the fix and it is the standard one**: the escape sequence asks the *terminal emulator* to
set the local clipboard, so it crosses SSH, tmux and containers by construction. Support is broad
(iTerm2, kitty, WezTerm, Windows Terminal, foot, recent xterm; tmux needs `set-clipboard on`), and the
right shape is to try OSC 52 first and keep the native tools as the fallback, not the reverse.

**Two things to get right.**

- **Aegis already strips OSC 52 from untrusted output** (`termsafe.StripDangerousSeqs`, and the
  transcript/tool-view tests assert it), which is correct and must not be weakened. Writing the sequence
  is a *deliberate emission on a trusted path*, not a relaxation of the filter — keep the two clearly
  separated so nobody later "simplifies" them into one place.
- **Size limits.** Many terminals cap OSC 52 payloads (and tmux historically capped hard). Fall back to
  the native tool above a conservative threshold rather than emitting a sequence that gets silently
  truncated.

**This is the better answer to the remote-clipboard problem than P74.19**, because it keeps wheel
scroll, click-to-focus and the P74.18 highlight, and it fixes `/copy` — which a mouse-capture change
does nothing for.

Priority: Tier 2 — XS, additive, blocks nothing, and it fixes a silent-wrong-result bug rather than a
preference.

### P74.19 — Mouse capture is unconditional, so terminal-native selection is unreachable in the default layout

`View()` sets `MouseMode = tea.MouseModeCellMotion` whenever `rawScrollback` is off, which is the
default. That capture is what makes Aegis's own drag-selection possible — and it is also what stops the
terminal emulator from offering its own click-drag select and copy-on-select.

Today the only way to get terminal-native selection is `/scrollback`, which also releases alt-screen
and therefore gives up resize re-wrap. **Those two things do not actually have to travel together.**
The comparison client separates them explicitly: `CLAUDE_CODE_DISABLE_MOUSE=1` keeps alt-screen and the
virtualized scroll but skips mouse capture, and its own comment names the reason — *"so tmux/kitty/
terminal-native copy-on-select keeps working"* — while `CLAUDE_CODE_NO_FLICKER=0` is the all-or-nothing
switch that also drops alt-screen. Two knobs, two different trades.

A `tui.mouse: off` key doing the same in Aegis is a few lines in `View()`, and it is the **only**
configuration that delivers terminal-native selection *and* re-wrap at once.

**The advantage that actually justifies it is SSH, and it is real.** `copyToClipboard`
(`internal/tui/view.go:655`) shells out to `pbcopy` / `xclip` / `xsel` / `wl-copy`, and there is **no
OSC 52 path** — the only OSC 52 in the tree is in the sanitizers, which correctly strip it from
untrusted output. Over SSH that means app-owned selection copies to the *remote* machine's clipboard,
which is useless to the person at the keyboard. Terminal-native selection is the only thing that works
there today. The secondary case is `tmux`/`kitty` users whose copy-mode and clipboard tooling assume
the terminal owns selection.

**But P74.20 is the better answer for the SSH case specifically**, because it fixes it without giving
up anything. Take this item for the people who genuinely prefer terminal selection, not as the fix for
a remote clipboard.

**Three costs, and the first is the one that matters.**

- **No wheel scroll.** In alt-screen a released wheel event goes to the emulator, which usually sends
  nothing useful. Scrolling becomes keyboard-only — which cuts against "native scroll" being important,
  so this cannot be the default without reopening that. Check the keyboard bindings are all reachable
  while the composer holds focus before calling this done: today `Update` forwards only `pgup`/`pgdown`
  to the pane while typing, so `GotoTop`/`GotoBottom` and the half-page keys are not.
- **Selection is unreliable *during* streaming.** Most emulators drop a selection when the cells under
  it change, and Aegis repaints every animation tick while a turn streams. Idle is fine — the only idle
  redraw is the 20-second `statusRefreshInterval` tick, which mostly changes no cells — so this affects
  selecting mid-turn, not selecting an answer after it lands.
- **No click-to-focus, and `selection.go` goes idle.**

**This is an escape hatch, not a direction.** It does not supersede P74.18: someone running with capture
on still drags across chroma-highlighted diffs, and that is the bug.

Priority: Tier 2 — XS, additive, blocks nothing. **Off by default, and that is settled, not open**: the
wheel-scroll trade was put to the user on 2026-08-20 and declined in favour of **P74.20**, which fixes
the remote clipboard without costing anything. Do not reopen it as a default without new information.

### P74.5 — The pickers stack three heavy selection signals where one would do

Aegis's overlay pickers say "selected" three times over. `configureDialogList`
(`internal/tui/dialog.go:45`) sets a brand title chip — `Background(colBrandBg)`, `Foreground(colBrandFg)`,
bold, padded — on a solid fill. `dialogFrame` (`:67`) wraps the result in a rounded primary border.
`aegisListDelegate` (`:22`) marks the focused row with a left `NormalBorder` bar in `colPrimary`
**plus** `colPrimary` foreground **plus** bold. The chip on a solid fill is the most
application-shaped element anywhere in the UI, and the doubled selection cue is why a picker reads as
a dialog box rather than a list.

The comparison case draws a `❯` pointer in one accent colour, the label in the same colour, a `✓` when
an item is chosen, and a dim description indented by two — with no frame, no fill and no border bar,
letting the terminal's own background be the surface.

**What to change, concretely:** replace the chip with plain bold `colPrimary` text over a hairline
rule; replace the delegate's left border bar with a `❯` pointer and drop the bold, keeping the colour
shift as the single cue; keep the rounded frame for genuinely modal dialogs (approval, quit-confirm)
and drop it for pickers, which are transient and already composite over a dimmed transcript.

**Watch the shared-chrome comment at the top of `dialog.go`** — it states the current styling is
deliberate and mirrors Crush. That decision is being reversed here, so the comment needs rewriting
rather than deleting, with the reason (the frame competes with the dimmed backdrop that P16.6 already
provides for modality).

**Closure condition:** the existing dialog snapshot tests updated, and a note in the rewritten comment
naming which dialogs keep the frame and why.

Priority: Tier 2 — cheap, self-contained, and the most direct answer to the standing menu complaint.

### P74.6 — A picker gives no sign that typing filters it

`configureDialogList` calls `SetShowHelp(false)` and `SetShowStatusBar(false)`, and `newPalette`'s
comment states the intent: "Browse mode by default; typing any character activates filtering
naturally." The behaviour is right and completely invisible — there is no visible query, no hint that
input is accepted, and no match count.

One dim footer line inside the picker fixes all three: `type to filter · ↑↓ move · enter select · esc
close`, with `n/m` right-aligned once a filter is active. It costs one row of the height
`dialogListH` already budgets and removes the only genuinely undiscoverable interaction in the app.

**Do this in the same sitting as P74.5** — same function, same file, and the footer's styling depends
on whether the frame is still there.

Priority: Tier 2 — XS, no dependency beyond sharing a file with P74.5.

### P74.7 — The terminal cursor never moves onto the focused row

`View()` (`internal/tui/view.go:31`) sets `AltScreen`, `MouseMode`, `WindowTitle` and `ReportFocus`,
but never positions a cursor. While a picker is open the hardware cursor is wherever the composer left
it, which is why keyboard selection in an Aegis menu feels like watching a redraw rather than moving
through a list.

The comparison implementation declares a cursor position on every focused list row. Three things
follow from the terminal's own cursor tracking the selection: screen readers follow it, terminal
emulators that highlight the cursor line agree with the app about where "here" is, and IME composition
lands in the right place.

Bubbletea v2 carries cursor state on `tea.View`. The work is to thread a declared position out of
`listDialog.View` (and the approval dialog, which has the same problem) up to `model.render`, which
already returns a plain string — so this needs a small return-shape change, and that is the only part
that is not mechanical.

Priority: Tier 2 — XS, disproportionate payoff per line changed, and the accessibility half of the
menu lane.

### P74.8 — A tool call that arrives as prose is silently lost

`internal/provider/openai/openai.go` reads `tool_calls` off the wire (`:248`, `:643`, `:702`) and
nothing else. When a local model emits its call as text — a fenced JSON object, a tagged block, or a
bare `{"name": ..., "arguments": ...}` — the turn produces prose, the engine sees no tool call, and the
loop either stalls or retries blind against a model that already answered.

`deepagents` handles this as a per-model middleware: intercept any response that came back with **no**
structured tool calls, strip reasoning tags, then try a tagged-text parser followed by a bare-JSON
parser. Two details in that implementation are the ones that make it safe rather than a source of
false positives, and both must be carried across:

- **Parsed names are validated against the tool list actually sent on that request.** A model writing
  the word `read_file` in a sentence does not become a call.
- **Leftover text is preserved as content**, not discarded. A model that narrates and then calls
  loses neither half.

The natural home is a provider decorator beside `retry.go`, `numctx.go` and `failover.go` — the same
seam, applied to the response rather than the request. **Keep it off by default and let P74.17 turn it
on per model**; until that exists, gate it on the existing local-profile boolean rather than making
every cloud turn pay a regex pass.

**Related but distinct, and worth not conflating:** the qwen3:14b Ollama-template issue drops the call
from *history* after it was correctly parsed. That is a different bug with a different fix; this item
is the general defence for the family, not that specific one.

**Closure condition:** a table test over recorded local-model responses in each malformed shape,
asserting the salvaged call, the surviving prose, and — the important negative — that a response
merely *mentioning* a tool name yields no call.

Priority: Tier 2 — the cheapest large reliability win for local models, and independent of every TUI
row.

### P74.9 — An empty tool result is indistinguishable from a failed one

Two small repairs from the same `deepagents` shim, and the second is the one that matters.

**Empty-result normalization.** A tool that legitimately returns nothing — a `grep` with no matches, a
`read_file` on an empty file — hands the model an empty string. Many local models cannot tell that
apart from "the tool failed", and re-call it, which then reads as a loop and can trip the P52.3 failure
breaker or the loop detector for a reason that has nothing to do with either. Replacing an empty
result with a named placeholder ends it. **This is correct for every model, so it belongs in
`internal/tool/builtin` near the truncation posture table in `truncate.go`, not behind a profile** —
and the posture table is where the decision should be recorded, since it is the same class of decision
about what a result looks like when there is less of it than expected.

**Argument-shape repair.** Rewriting `path` → `file_path` and filling a missing `limit` with a default
is a per-model shim, not a universal one — a model that guesses argument names wrongly is a model
problem, and papering over it globally hides a real signal from the tool-calling probe. **Hold this
half until P74.17 exists**, then register it per model. Filed together because they come from the same
source; they do not ship together.

**Closure condition:** for the first half, a test that an empty `tool.Result` reaches the model as the
placeholder, and that the loop detector does not count two such results as a repeat.

Priority: Tier 2 — first half unblocked and small; second half deliberately deferred to P74.17.

### P74.10 — There is no reduced-motion setting

Nothing in `internal/tui` reads a motion preference — no config key, no env check, no equivalent of the
`NO_COLOR` handling `imagerender.go` already does for colour. `shimmerText`, the blinking caret
(`caretBlinkPeriod`), the cycling `thinkingPhrase` and every pending tool card animate unconditionally
whenever `m.streaming && m.followBottom`.

Two reasons this matters more here than for a cloud client. It is an **accessibility** item: the
shimmer is a continuous moving-luminance sweep, which is the class of animation vestibular sensitivity
reacts to, and there is currently no way to turn it off short of not using the tool. And it is a
**CPU** item: `updateSpinnerTick` re-renders the status line and calls `updatePendingToolCards` every
frame, on a machine that is simultaneously running inference against a 16 GB card.

The implementation detail worth copying is that the comparison client does not merely skip the
*drawing* — it passes a null interval so the clock **unsubscribes**. The Aegis equivalent is to stop
re-queueing the spinner tick, not to keep ticking and render a static frame. Note that P3.7 already
established the pattern by gating on `followBottom`; this is the same shape with a different
condition.

**Take this before P74.11 and P74.12.** Both add animation, and both have to honour the flag; adding
them first means retrofitting two more call sites.

Priority: Tier 2 — XS, and it gates two later rows.

### P74.11 — Stall is an abort with nothing before it

`MaxTurnStall` (900s, the only bound covering tool execution) is a hard fatal abort. Between "working"
and that abort the UI shows exactly one thing: a shimmer that looks identical at second 2 and second
400. On a local model a 90-second silence during prompt evaluation is completely normal *and*
completely indistinguishable from a hang, which is the state the user is actually in when they reach
for ctrl-c.

The comparison client ramps its spinner toward red as the gap since the last token grows, continuously
rather than at a threshold, so a run visibly gets stuck before it is stuck.

Aegis has the pieces: `shimmerText` already takes a highlight colour and blends a ramp with
`lipgloss.Blend1D`, and the stream already knows when the last token arrived. Interpolating the
highlight from `colAccent` toward `colWarning` as a function of that gap is a few lines. **Pick the
mapping against the real bound**, not an invented one — the ramp should be visibly underway well
before `MaxTurnStall` fires, and the sidebar's `WAITING`/`GENERATING` section split already encodes
the distinction that matters (no first token yet vs. tokens stopped).

Must honour P74.10.

Priority: Tier 2 — small, and it converts the single most anxious state in local-model use into a
legible one.

### P74.12 — The token counter jumps instead of climbing

`renderStats` prints `in:%d out:%d` straight from the last counter update, so the number stutters in
chunk-sized steps. The comparison client chases the real value with a gap-proportional increment each
frame — small when close, larger when far behind — so it climbs smoothly and reads as continuous
progress rather than as intermittent arrivals.

`animStep` already provides the frame clock and `renderStats` is the only render site. Pure polish, and
cheap; filed because it is the kind of detail that separates "works" from "feels finished".

Must honour P74.10 — a reduced-motion run should print the true number immediately.

Priority: Tier 2 — XS, no dependency beyond P74.10.

### P74.13 — A running swarm is three near-identical grey lines

The sidebar's `AGENTS` section renders every running teammate through `m.th.tool` with an id truncated
to eight characters, so three concurrent sub-agents are three lines of the same colour differing only
in a hash prefix. Nothing else in the UI ties a tool card, a transcript line or a status segment back
to which agent produced it.

The comparison client reserves a fixed eight-colour agent palette and uses one stable colour per
teammate everywhere that teammate appears. Aegis already has the raw material — Charmtone gives eight
distinct hues that sit inside the existing scheme, and `colorScheme` is the right place to name them
as a slice rather than scattering literals.

Hash the agent id to an index so the assignment is stable across restarts and across the sidebar,
transcript and status bar without threading state.

Priority: Tier 2 — XS, fully independent of every other row.

### P74.14 — A dangling call whose arguments never parsed is reported as "possibly completed"

`repairOrphanedToolUses` reports a started call as *possibly* completed, tracked via
`Engine.startedTools`. For an interrupted call that is exactly right and is a deliberate, documented
invariant — the call may genuinely have had an effect, and claiming it did not run is the dangerous
direction.

It is wrong for one case. A tool call whose **arguments were malformed or truncated** never dispatched
at all; there is no ambiguity to preserve. Telling the model "possibly completed" there invites it to
skip a retry it should make, and on local models truncated argument JSON is common enough to matter.
`deepagents` splits exactly these two messages — *arguments were malformed or truncated* versus
*another message came in before it could be completed*.

One extra branch, keyed on whether the call ever reached dispatch rather than on parse success at the
call site, so it stays correct if argument parsing moves.

**Do not weaken the existing behaviour while adding the branch** — the "possibly completed" default is
load-bearing and the invariant is documented in CLAUDE.md.

Priority: Tier 2 — XS, no dependency.

### P74.15 — Injected memory files pay for their own authoring notes

`deepagents`' memory middleware strips `<!-- ... -->` from AGENTS.md-style content before it reaches
the system prompt. Aegis injects project and user memory whole.

Two things follow. Free bytes against `localBasePromptCeilingTokens`, which is a hard test-enforced
ceiling — `TestEffectiveSystem_localProfileBudget` fails the suite when the local base prompt crosses
it, so anything that reclaims budget without a policy argument is worth having. And it makes
**machine-managed markers viable**: a tool that maintains a section of a memory file can leave a
delimiter comment without spending prompt budget on it every turn.

Watch the interaction with P67.5's still-unwired recall path, noted under [Decisions that outlive the
items that made them](#decisions-that-outlive-the-items-that-made-them): memory currently reaches the
prompt through `Sources.Load()`, which injects both files unfiltered, and that is the function to
change — not `FormatEntries`, which has no production callers.

Priority: Tier 2 — XS, no dependency.

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

**Status: 5 open, all P74**, filed 2026-08-20. The tier was emptied on 2026-08-18 (**P66.15**,
**P67.6**, **P67.7**, **P67.8**, **P67.9**, then **P70.4**) and the 2026-08-19 sitting kept it that
way: **P71.8**, **P73.1** and **P72.1** all shipped the day each was filed. Records in
[releases.md](releases.md).

Two of those records constrain future work here and are summarized under [Decisions that outlive the
items that made them](#decisions-that-outlive-the-items-that-made-them): read **P67.7**'s before
touching `internal/engine`, and **P66.13**'s before adding a permission layer or a run bound anywhere.
Read **P66.14**'s (2026-08-16) before touching the compaction path, because the shared trigger it
introduced changed which numbers two already-shipped heuristics see.

An item enters this tier when it has real value but is larger or sequence-dependent — it blocks, or
is blocked by, other work. **P72.1 is the worked example of the "sequence-dependent" half**: it sat
here rather than being built the day it was filed because it needed a cold-start policy decided, not
a wire, and the resolution was to put four design questions to the user before writing anything.

**P74.2 → P74.3 → P74.4 is the other kind of sequence-dependent: a chain.** Its ordering flipped twice
on 2026-08-20 while P74.2's size was wrong; the settled shape is P74.2 first (one sitting of chrome
removal), then the gutter, then the grouping. **P74.16 and P74.17 are here on size, not sequence** —
neither blocks anything, both are larger than a sitting.

### P74.2 — The chrome, not the rendering model: six framed regions where one would do

**Filed as a document-flow rewrite, twice, and corrected on 2026-08-20 to something an order of
magnitude smaller. The correction is the useful part and is recorded under [Decisions that outlive the
items that made them](#decisions-that-outlive-the-items-that-made-them).** In short: the comparison
client ships **two** rendering modes, the batch was originally read against the wrong one, and the mode
Anthropic's own staff run is alt-screen — the architecture Aegis is already in.

`renderChat` composes six framed regions: a title bar, a bordered sidebar column, a scrollbar glyph
column, the transcript viewport, a todo strip, and a rounded-bordered composer over a status line. Only
the frames are the problem. **Alt-screen and the bounded viewport are keepers**, because they are what
let the app own every cell — which is what makes resize re-wrap work, and what keeps `/search`,
`selection.go` and the timeline picker's `ScrollToItem` alive.

Three consequences of the chrome, none of which need a rendering-model change to fix:

- **Every frame is an edge the eye crosses.** A normal screen draws roughly a dozen box-drawing runs
  before any content.
- **Persistent chrome competes with content.** The sidebar is always saying *session, mode, model,
  tools, files, agents, context, cost*, none of which changes more than twice a minute.
- **Fixed columns squeeze prose.** Sidebar plus scrollbar plus padding is about 30 columns of an
  80-column terminal — 37% of the width, permanently.

**The work, and it is one sitting:**

- **Sidebar off by default, reachable as an overlay.** `renderOverlay` already composites over live
  chat and P33.11/P33.12 established the pattern, so this is a default plus a keybinding, not a new
  component. `renderInputArea` already folds the context meter into the status bar when the sidebar is
  hidden, which is most of the fallback already written.
- **Auto-hide the scrollbar column.** It carries no information while pinned to the bottom, which is
  the normal state. Render it only when scrolled away, the way a GUI overlay scrollbar behaves.
- **Fold the title bar into the status line.** It carries a brand mark, the connection dot and the
  model name; the status line already has a priority-ordered segment list with tail-dropping
  (`joinedWidth`) that these fit into. That reclaims a row and removes the topmost frame.

**Explicitly out of scope, and this is the reversal:** `tea.Println`, a commit/live split, retiring
`/search`, and deleting `selection.go`. All of those followed from the document-flow reading and none
of them survives it. **`rawScrollback` stays exactly as it is** — an opt-in for anyone who wants true
terminal scrollback and will trade re-wrap for it.

**Closure condition:** a fresh install shows no sidebar, no scrollbar while pinned to the bottom, and
no title bar; the sidebar overlay opens and closes without perturbing transcript geometry; resize still
re-wraps everything (assert it — this is the property the corrected direction exists to keep);
`/search`, drag-selection and `ScrollToItem` all still work.

Priority: Tier 3 — S, and sequence-dependent only because P74.3's gutter indentation depends on
whether the scrollbar column is still there. Take before P74.3.

### P74.3 — A tool call renders as two events that both announce themselves

A completed call currently emits two lines, each leading with the tool name: `renderToolCall` produces
`● read_file  internal/x.go`, then `renderToolResult` (`internal/tui/toolview.go:139`) produces
`✓ read_file → …`. On a twelve-call round that is twelve redundant identifiers, and the pair reads as
two events rather than one block with an outcome.

The comparison client prints the call once — `⏺ Read(internal/x.go)` — and hangs the result off a
continuation gutter, `  ⎿  Read 120 lines (ctrl+o to expand)`. Two properties do the work: the result
line carries a **summary** rather than the name, and it carries the summary rather than the body by
default. The gutter glyph is what makes call-plus-result read as a single unit.

Aegis already has the harder half. `renderToolCardDone` composes the call block and the result into one
transcript item, and `toolCompact` already exists as a per-session toggle. What changes is
`renderToolResult`'s header — drop the repeated name, emit a `⎿` gutter, and make the default a
one-line summary with the existing expand path.

**The specialized renderers must keep working verbatim.** `renderEditDiff`, `renderShellCall`,
`renderReadFileResult` and the chroma-highlighted read path all feed this; the P16.3 diff presentation
and the P16.2 highlighting are the parts most likely to break on a gutter change.

Blocked by P74.2, but only lightly — the coupling is the gutter's indentation constant, which depends
on whether the scrollbar column is still there. **This briefly ran ahead of P74.2 while that item was a
4–6 day rewrite; the 2026-08-20 correction shrank P74.2 to one sitting, so the natural order is back.**

Priority: Tier 3 — small, sequence-dependent on P74.2.

### P74.4 — An exploration phase renders as a wall of cards

An exploration phase is ten to twenty consecutive `read_file`, `grep` and `glob` calls, and Aegis emits
one card per call unconditionally. The result buries the actual answer under a log of syscalls, and it
is the reason a long turn is hard to read back.

The comparison client folds a run into a single line — *"Searched for 13 patterns, read 6 files"* —
expandable on demand. **After P74.2 this is the largest remaining density win**, and it is the one that
most changes how a turn reads: the transcript becomes a narrative of decisions rather than a
transcript of calls.

**Keep the grouping rule narrow, because the failure mode is hiding something that mattered.**
Consecutive calls only, all read-capability, all succeeded — any error, any write, any execute breaks
the group and renders normally. A grouped run must stay expandable, and the P21.2 combined-card
machinery is where the state for that already lives.

**The interaction with parallel rounds needs deciding, not assuming.** A parallel round is already a
set of simultaneous calls; whether a round is one group, or groups merge across rounds, changes what
the summary counts. Decide it explicitly and write it into the item's record.

Blocked by P74.3 — the collapsed line is a summary in the same gutter shape that item introduces. **The
commit-lifecycle argument that briefly also blocked this on P74.2 is void**: the corrected P74.2 does
not introduce a commit boundary, so a group that stays open until a non-read call breaks it is just an
item being re-rendered, which the pane already does on every tick for tool-card shimmer.

**Aegis's comparison point does this too, and does it harder in the mode that matters**:
`collapseReadSearch.ts` gates *additional* grouping — non-search Bash commands included — on
`isFullscreenEnvEnabled()`, i.e. only in the alt-screen mode. Collapsing is more valuable in a bounded
viewport, not less, which is the opposite of what the original document-flow framing implied.

Priority: Tier 3 — medium, and last in the layout chain.

### P74.16 — Truncation is entirely proactive; nothing clips in response to an actual overflow

Aegis bounds tool results in three proactive layers: per-call caps with the posture table in
`internal/tool/builtin/truncate.go`, a whole-round cap in `roundcap.go`, and spill to
`<workspace>/.aegis/spill/`. All three fire on Aegis's own estimate of size.

`deepagents` adds a second, reactive path: when the **provider itself** returns a context-overflow
error, clip the trailing tool-result batch before retrying, split by kind. A `read_file` result is
head-sliced and annotated with a pointer back to the original `file_path` — **no new write is needed,
because the content already exists at that path**. Everything else is offloaded to a stub.

The read-file shortcut is worth taking on its own merits regardless of the reactive path: Aegis's spill
directory currently duplicates content that, for reads, is already on disk somewhere the model can
reach with `read_file` plus an offset.

**Two constraints from this tree.** The posture table in `truncate.go` is the documented home for
"which end survives and what happens to the remainder", so a new posture goes in that table rather than
beside the retry. And the spill directory is deliberately reachable by `read_file` and **not** by grep;
a pointer-to-original path must not quietly widen that.

**Check first whether the overflow error is even distinguishable.** `internal/provider/errors.go` and
the P61.7 remainder both concern classifying backend-echoed text; if an overflow is not reliably
separable from other 400-class failures on the local path, that classification is the real first step
and this item is larger than it looks.

Priority: Tier 3 — medium, no sequence dependency, but it touches the truncation posture table and
wants a clear run rather than being squeezed between two TUI rows.

### P74.17 — The entire local-model story is one boolean

`builtin.Options.LocalProfile` is a `bool` (`internal/tool/builtin/builtin.go:104`), decided once in
`enginecfg.BuiltinOptions` (`internal/enginecfg/tools.go:57`) from `cfg.Provider.LocalPromptProfile()`.
Every local model — qwen3:14b, gpt-oss, LFM2.5 — lands in the same bucket and gets the same deferred
tool set, the same prompt profile and the same tool descriptions. **Their failure modes are not the
same**, and the boolean has no room to say so.

`deepagents` keys a registry on **model spec** and merges profiles additively, so a provider-level
profile layers under a model-level one. Each profile carries a system-prompt suffix, per-tool
description overrides, an excluded-tools set, an excluded-middleware set, extra middleware and
sub-agent overrides. The asymmetry in their tree is the argument for the shape: their Haiku profile is
52 lines of prompt text, their Nemotron profile is 1,826 lines of quirk shims for one open-weight
model.

**Aegis already has both halves this needs.** `internal/modelcaps` persists per-model capability and is
already keyed the right way; `internal/sysprompt` owns the prompt blocks and their byte caps. A
`profile.Harness` resolved from the model id, carrying `PromptSuffix`, `ToolDescriptionOverrides`,
`DeferredTools` and a response-decorator list, is the generalization.

**Four constraints, and the first two are hard invariants of this tree.**

- **The prompt budget is test-enforced.** `TestEffectiveSystem_localProfileBudget` fails the suite when
  the local base prompt crosses `localBasePromptCeilingTokens`. A per-model `PromptSuffix` must be
  measured against that ceiling per model, not once.
- **A tool's description must never name a tool the active profile defers.** That is already an
  invariant; per-model description overrides multiply the number of ways to break it, so the existing
  test has to become per-profile rather than per-build.
- **Required scaffolding must not be excludable.** `deepagents` rejects a profile that tries to strip
  the middleware backing filesystem tools, subagent dispatch or permission enforcement, and formats the
  rejection identically at construction and assembly time. Aegis enforces the same class of invariant
  at *test* time with `TestEveryEngineCallSiteDecidesItsGate` and
  `TestEveryRegisterCallSiteDecidesTheLocalProfile`. **Keep the tests and add the runtime error**, because
  a profile is user-authorable in a way an `engine.New` call site is not.
- **`LocalProfile` is load-bearing in the deferred-tool direction.** Under it `edit_file` is deferred
  and the handle-based editors are not, and a test pins that direction. The migration has to preserve
  it as a profile default, not lose it in the generalization.

**Take this last, and take it with cargo.** Built before P74.8 and P74.9 exist it is an empty
abstraction with one boolean's worth of content; built after, it has two concrete per-model behaviours
asking to be registered, which is what will reveal the real shape of the interface.

Priority: Tier 3 — largest item in the batch, no blocker, but deliberately sequenced last behind
P74.8 and P74.9.

## Open Work — Tier 4

**Status: 25 open** — 9 pre-existing (all blocked or explicitly parked, none with a fired trigger),
6 from the P66 review batch, 5 from the P67 external-source reading, and 5 from the P71 batch filed
2026-08-19 (**P71.6**, **P71.7**, **P71.11**, **P71.12**, **P71.13**). **P70.3** shipped 2026-08-18
and has left this tier.

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

**Promote when** **P71.8** lands — **it has, 2026-08-19.** Phasing changed the arithmetic: each
round is now a fresh, disk-grounded turn (P47.4) rather than a slice of one accumulating
conversation, so the per-*run* budget this item measured is no longer the binding constraint the same
way. Re-derive the numbers against the phased shape (a round's own turn budget, not the whole run's)
before building this, rather than assuming the original math still applies unchanged.

Priority: Tier 4 — S. Was blocked on **P71.8** by choice; unblocked 2026-08-19, not yet promoted.

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
and a datacenter/CI host's IP is *more* likely to get blocked by those upstreams than a residential
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
whole track is parked at the one remaining row of [Up next](#up-next) by choice**;
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
setup if scheduled with them. That bundle is the one remaining row of the [Up next](#up-next)
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
