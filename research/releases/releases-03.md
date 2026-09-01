# Aegis Release History — Part 3 (continued from releases-02.md)

Continuation of the newest-first `## Latest changes` record list. Start at
[releases.md](../releases.md) for the index.

---

### P66.9 — bound `bg_events`, and coalesce text deltas

Shipped as `d4fb209`. The debate's cut of the latency half stands and was not revisited; unbounded
growth was the item. `bg_events` was pruned only by whole-session delete, and the auto-pruner is
gated on `Cleanup.SessionTTLDays`, which has no default.

`DefaultBGEventRetention = 2000` rows per session, enforced inside `AppendBGEvent` and **deliberately
not a config key** — the defect was precisely that the existing pruner depends on an unset one, so
adding a second knob would have rebuilt it. `Store.SetBGEventRetention` exists for tests and
embedders only, which is why `docs/configuration.md` gained nothing. Sweeps run every 128 appends,
keeping the common path a single INSERT, worst case `retention + interval - 1` rows; the sweep is a
bounded reverse scan over the existing `idx_bg_events_session` with no `COUNT(*)`, and a no-op under
the bound.

**A failure mode the item did not anticipate:** the first append a session makes *in a process* always
sweeps. Without that, a session appending fewer than 128 events per daemon lifetime accumulates
across restarts and reproduces the same unbounded growth in slower form — the interval alone is not a
bound. It has its own test.

The bound is safe because `bg_events` is a live catch-up buffer for
`GET /sessions/{id}/events?since=N`, not a transcript — `session_messages` is the durable record — so
what is dropped duplicates content held whole elsewhere. That reasoning is in the doc comment, since
it is the whole licence for discarding rows.

Coalescing folds pure `text`/`thinking` deltas over a 200ms window, flushing on kind change, window
expiry, any non-delta event, and run end. "Pure" is checked with `reflect.DeepEqual` against
`Event{Kind, Text}`, so a future field on `api.Event` makes such events **stop coalescing** rather
than silently dropping the field. Replay holds because deltas of one kind concatenate in id order and
the buffer flushes before any non-delta row, so a coalesced row can never overtake a tool call or
`turn_done`.

All four mechanisms were mutation-tested per this document's own method note — disabling the prune,
removing the first-append sweep, disabling coalescing, removing the ordering flush — and each fails
its own tests and no others.

**PERF-02 (`synchronous=NORMAL`) did not land** and remains open. The two databases where it is
unconditionally safe are `knowledge.db` and `longmem.db`; the only DSN in this item's reach was
`sessions.db`, which is exactly the one the debate downgraded to Low over, since it carries
checkpoints, the cost ledger and traces.

---

**Last updated:** 2026-08-15 (later the same day) — **P66.2 shipped**, the first commit of the P66
review batch and the one the day plan puts first for a reason that has nothing to do with its
severity: it changes the toolchain every subsequent test run in the batch executes on, so landing it
after any other item makes that item's evidence ambiguous.

`go.mod` now pins `toolchain go1.26.6`. At go1.26.5 `govulncheck ./...` reported seven standard-library
vulnerabilities with **six traced call paths from this codebase** — four of them on surfaces the same
review independently established are fed by data the operator does not control (`openai.Adapter.Stream`
and `ollama.Adapter.Healthy` parsing model-server responses, `security.parseNmapXML` hitting an
`encoding/xml` recursion crash on third-party scanner output, and GO-2026-6089's missing
`ReadHeaderTimeout` on the daemon's own listener). All are denial-of-service in character and none
crosses a privilege boundary, which is why the review rated it Medium; all are fixed by the one-line
bump. `govulncheck ./...` now prints `No vulnerabilities found` and `go test ./...` stays green across
all 68 packages.

**The durable half of the item is the CI wiring, not the bump.** A project that ships a vulnerability
scanner and runs `aegis security update-db` to keep *the user's* CVE data fresh had never scanned its
own toolchain — so the finding could only ever recur. `.github/workflows/ci.yml` gained `govulncheck`
(blocking) and `staticcheck` (advisory) on the ubuntu leg, both OS-independent analyses following the
same one-leg convention as the gofmt and frontend-drift checks already there.

**The trap is documented beside the install step, and the documentation is the point.** A tool module
can pin an *older* toolchain in its own `go.mod`, and under the default `GOTOOLCHAIN=auto` `go install`
honors that directive — so the installed binary is built with the older Go and then cannot analyze this
go1.26 module. `honnef.co/go/tools@v0.7.0` does exactly this (`toolchain go1.25.13`), and during the
review it produced 21 packages failing with `package requires newer Go version go1.26 (application
built with go1.25)`, which reads like a broken codebase and is not. Upgrading the tool reproduces it one
version higher; pinning the toolchain fixes it. Both installs therefore run under
`GOTOOLCHAIN="$(go env GOVERSION)"` — taking the pin from the toolchain `actions/setup-go` already
resolved out of `go.mod`, rather than hardcoding a version that would drift from the module the day
someone bumps it again. Re-verified at go1.26.6 rather than trusting the review's go1.26.5 note: the
pinned install yields a staticcheck that analyzes the tree and reports 28 findings.

Those 28 are **P66.12's** work and this item was not licensed to fix them, so the staticcheck step
carries `continue-on-error: true` with a comment naming the item that removes it. That is the honest
posture — a step that gates on a known-failing backlog either goes red permanently or gets deleted, and
both outcomes lose the check. Deleting the line is now written into P66.12's closure condition, so
clearing the backlog cannot silently leave the step advisory for the next 28 findings to accumulate
against.

**Last updated:** 2026-08-15 — **P65.3's mechanism shipped**, closing the Tier 3 measurement gate it
was filed behind. The item's own instructions were "measure Question 1 before building anything," and
Question 1 — does a session's reported cache usage include the summarizer's and guard's one-off prompts
— turned out to be answerable by reading code rather than running a live model: both call sites share
the conversation's Anthropic adapter instance, which emits `cache_control` breakpoints unconditionally
whenever prompt caching is on. Confirmed, not refuted, and worse than framed: neither call site ever
read the stream's `Usage` event, so the billed cache-write cost wasn't merely unattributed, it was
invisible to Aegis. `provider.Request` gained `SuppressCache bool` (alongside `NumCtx`/`Format`), set by
`compaction.go`'s summarizer and `guard.go`'s validation call, honored only by the Anthropic adapter
(`cache := a.cache && !req.SuppressCache`) and ignored elsewhere — same shape as every other per-request
override in that struct. `TestPromptCachingSuppressedPerRequest` pins that a suppressed request carries
no breakpoint on `system`, the last tool, or the last message block. Debate roles, named alongside the
summarizer and guard in the original filing as another rider on the shared adapter, were deliberately
left untouched: nothing established a debate role's prompt is one-off the way a summarization or guard
call is, and suppressing there was not measured, only assumed. The item's local half (Question 2 — does
a one-off call between turns measurably raise the next turn's local prefill?) stays open, gated on the
same live-tier local-model session as P38.1/P62.9/P65.2's prompt half; none of those had a reachable
backend in this pass. See [roadmap.md](roadmap.md)'s P65.3 entry.

**Last updated:** 2026-08-14 (later the same day) — **the P64/P65 batch: P65.1, P64.2, P65.2's
deterministic half, P64.3 and P64.1 all shipped**, taking the five highest-priority buildable items off
the two harness-review batches filed that morning. P64.3 and P64.1 were built in that order on the
document's own sequencing rule — the instrument before the optimization it measures — and their
write-ups are at the end of this entry, after the three correctness items.

**The batch's largest single result is a number nobody had:** nine ordinary built-in tool calls over a
60-file fixture return **15,666 estimated tokens**, against the 4,550-token ceiling the whole P62.x
line spent five items defending. The prompt was measured to the token and gated in CI; the results
were not measured at all. That asymmetry was the deepseek review's finding and it holds up. All three remove a *false or missing statement* rather than
adding a capability, which is why they were sequenced first: P65.1 stops the transcript claiming a
cancelled tool "did not run" when it may have half-completed, P64.2 closes a by-construction defeat of
the loop detector, and P65.2 stops every compaction losing the session's file set. Their write-ups are
immediately below, then P62.10's.

**The batch's finding is about the two guards it touched, and it is one finding, not two.** P64.2 and
P65.1 are the same defect in different clothes: **a mechanism that reports on the agent's behaviour
while keying on the wrong evidence.** The loop detector keys on the whole turn, so it reads a varying
bookkeeping payload as new work; the orphan repair keys on the message list, which records what the
model *asked for* and never what the runtime *started*. In both cases the fix is not a better
heuristic over the same data — it is finding the one fact the mechanism was guessing at and recording
it. That is a different move from tightening a threshold, and it is worth naming because the tempting
version of each fix (widen `PollExempt`; soften the wording for all orphans) is a heuristic and would
have been strictly worse.

**P65.1 — the interrupted-tool result stops asserting something it cannot know (SHIPPED 2026-08-14).**

`repairOrphanedToolUses` injected `"tool call interrupted; %s did not run"` for every unresolved
`tool_use`. The repair itself is right — a provider rejects a conversation with an unmatched tool_use
— but the claim was never checked, because the function reads the message list and the message list
cannot distinguish a call that was refused from one that deleted half a directory before the stall
detector cancelled its context.

It is reached routinely rather than exceptionally, which is what moved it out of Tier 4 when it was
filed: every bound Aegis has for stopping a run cancels the run context mid-flight *by design* —
`MaxTurnStall` (on by default at 900s, and the only bound covering the tool-execution phase),
`MaxWallClockPerRun`, a user interrupt, a TUI quit-while-streaming — and the tool most likely to be
running when a stall bound fires is the long one that stalled. The drive's reset ladder then hands the
next context a transcript asserting the effect did not happen.

`Engine.startedTools` records the tool_use IDs that reached `Execute`, under a mutex for the same
reason `writtenFiles` has one, and `repairOrphanedToolUses` takes it as a parameter. A started call now
says *"interrupted while running; `shell` may have partially completed. Verify before assuming its
effects did or did not land."* The second sentence is the whole change: a model told an effect is
uncertain re-checks, a model told it did not happen re-runs it.

Three decisions the filing did not specify, each of which changes behaviour:

- **The mark sits immediately before `t.Execute`, not at the top of `executeTool`.** Everything above
  that line — unknown tool, gate refusal, hook veto — returns without the tool running, so marking at
  entry would over-warn on exactly the calls whose "did not run" is provably true, and an
  always-uncertain message is no more useful than an always-confident one.
- **The repair runs before the per-run reset, and the order is load-bearing.** The orphans belong to
  the run that was interrupted, so the only map that can classify them is that run's.
- **A nil map keeps the old wording.** A session restored into a fresh process has no record either
  way; recovering the fact across a process boundary needs a durable store and is P65.4's problem.
  The item's scope discipline — "this is not the durable version and must not grow into it" — is
  honored.

`TestInterruptedToolRepairDistinguishesStartedFromUnreached` drives a real `Engine.Run` cancelled
mid-round with two calls outstanding, made deterministic by the same-path ordering in `runTools`
(both calls name one path, so the second waits on the first and abandons the wait on `ctx.Done`).
**Both halves are asserted and both mutations were run:** forcing every orphan to "may have run" fails
on the unreached call, forcing none fails on the started one. A one-call fixture can distinguish
neither — P63.9's finding, applied deliberately rather than rediscovered.

**P64.2 — a bookkeeping call with varying arguments no longer launders a loop past the detector
(SHIPPED 2026-08-14).**

`turnSignatureExcludingPolls` keyed on the **whole turn**, so `grep X` emitted alongside a `todo` write
whose payload changes every turn produced a fresh signature every turn and the repeated grep was never
seen — no matter how many turns it repeated. `toolFailureTracker` only counts *failing* calls, so a
succeeding loop with a varying bookkeeping call simply rode to `MaxIterations`.

The item's stated first step was to demonstrate the defeat by construction rather than from a reading
of the code, and that was done: with the fix's predicate disabled,
`TestEngineDetectsLoopLaunderedByVaryingBookkeepingCall` fails — a plain error loop the detector has
caught since P53.2 goes entirely undetected once one varying `todo` write rides along.

**The design decision worth recording is that transparency is not exemption, and the code makes them
structurally different rather than merely differently-named.** The obvious fix — declare `todo`
`PollExempt` — closes the gap and buys it with the concession `PollExempter`'s own doc comment warns
about at length: an exempt call is dropped entirely, and a turn made of nothing but exempt calls is not
recorded, so a model that does nothing but rewrite its plan five turns running runs to the iteration
cap unwatched. A **signature-transparent** call loses only its *arguments*; its name stays. So
`grep X → todo_write(varying) → grep X` collapses to one repeated signature and is caught, **and** a
turn of pure bookkeeping still counts as a turn and is judged. The narrower concession is what makes
the set safe to grow past three entries.

Five builtins declare it: `todo_add`, `todo_update`, `remember`, `entity_remember`, `task_update`. The
tests pin the *opaque* half as hard as the transparent one, because that is where the rule lives —
`project_knowledge` and `entity_recall` are searches (the query is the model choosing what to look
for, which is the evidence the detector runs on), `save_skill` is a deliverable, `todo_list` is a read.
`TestTransparencyAndPollExemptionAreDisjoint` fails if a tool ever claims both, which is the shape of
someone reaching for the stronger concession while meaning the narrower one.

**P65.2 — compaction summaries have a skeleton, and the file set survives them (deterministic half
SHIPPED 2026-08-14; the prompt half built and gated on the live tier).**

Two things did not exist. The summarizer asked for "terse bullet points" and took what came back, and
nothing carried the file set forward — `Engine.writtenFiles` is reset per run, read paths were tracked
nowhere at all, so after a compaction the model's record of the workspace was whatever survived in the
keep-recent tail plus whatever the free-form summary happened to mention, and after a *second*
compaction, whatever the second summary happened to carry over from the first.

*The prompt half.* `summarizeSystemPrompt` now pins a five-heading skeleton (`## Goal`,
`## Constraints`, `## Progress` with Done/In Progress/Blocked, `## Key Decisions`, `## Next Steps`).
The reason is not style: free-form compression is generation and structured fill is completion, every
measurement in the P38.x line says a local model degrades on the first and holds up on the second, and
the summarizer was the last place in the engine asking a local model for unstructured prose — at the
worst possible moment, when the context is nearly full. **Measured before choosing the section list,
per this document's own repeated finding about instruments: 62 → 128 estimated tokens, a delta of 66,
paid once per summarization request against a transcript already thousands of tokens long.** Pi's list
has six sections plus a Critical Context heading; five were kept and the sixth dropped, on the item's
own rule that a skeleton crowding out content is fixed by fewer sections rather than a bigger budget.

*The deterministic half, and this is where the build differed from the filing.* The item proposed
promoting `Engine.writtenFiles` to session scope. **That does not work, and finding out why changed the
design.** Two things are rebuilt per request: the `Engine` (so no Go-side accumulator survives between
runs) and — worse — the `Summarizer` is built once per *server* and shared by every session, so a
`SetFiles` method would have sessions overwriting each other's paths. That is a cross-session leak, not
a stale list.

So the state lives in the only place that actually persists: **the transcript**. Each summary re-emits
the lists inside `<read-files>`/`<modified-files>` tags, and the next compaction parses them back out
of the prefix it is summarizing and merges. The set accumulates across any number of compactions with
no state anywhere, and survives a daemon restart that reloads the conversation for free.

Four details are load-bearing:

- **The lists travel on the context, via a new `FileContextCompactor` optional interface** — a context
  *decorator*, not a `Compact` variant. The guard calls `CompactYield` when it can and `Compact` when
  it cannot, so a variant would have to be written twice and every future widening would double again.
  The engine cannot import `internal/compaction` (engine's own tests import it, so the dependency would
  close a cycle), which is exactly why the existing seams here are optional interfaces.
- **The block is priced by the same fit check as everything else.** `summarizeRequestTokens` takes the
  fixed prefix, so the lists are inside the budget the reserve exists to defend. Adding them after
  `fitTranscript` — the obvious implementation — is the one way this feature could turn a working
  compaction into a failing one. A 10-path list measures 33 tokens; at the 40-path cap, 17.
- **`FallbackCompact` carries the lists too.** It fires precisely when a local summarizer keeps
  failing, which is the same population the lists exist to help, and because it replaces the prefix
  outright, not re-emitting them would destroy an accumulated set *permanently* rather than for a turn.
- **The lists are re-emitted by code, not by the model.** A model that fumbles `## Key Decisions` still
  cannot lose a path list it was never asked to reproduce — and it is what makes accumulation work at
  all, since the tags are a wire format between successive summaries.
  `TestFileListsAccumulateAcrossCompactions` drives *two* compactions for that reason; a
  single-compaction fixture cannot see the failure.

Read paths are recorded in `executeTool` on the same effective-capability rule (P25.4c) the write
branch uses, so a `cat` routed through `shell` is recorded the way a `read_file` call would be, and
errored reads are excluded — a failed read tells the model nothing about the file and would advertise
one the session never saw. The carried lists are capped at 40 each, keeping the **most recent** paths
and stating the count dropped, on the same rule as `truncNotice` and `omissionNote`.

**Still open:** the prompt half's closure condition is a live run showing a local model filling the
skeleton without losing content the terse-bullet prompt kept. Per the item, it wants the same harness
P38.1's conformance re-run uses — one setup instead of two.

**P64.3 — what a tool result costs is now stated, measured and (in one respect) gated
(SHIPPED 2026-08-14).**

The convention plus its instrument, in that order, because the convention without the instrument is
prose that rots and the instrument without the convention is a number nobody owns.

*The convention.* `TruncateHead`/`TruncateTail` (`internal/tool/builtin/truncate.go`) replace six
independently-chosen caps with two helpers on pi's rule that **the tool declares which end carries the
information** — head for file reads, search results and fetched pages; tail for logs, test runs and
builds. The values were deliberately **not** homogenised (the point is a shared mechanism and a stated
posture, not one number), and the file carries the full posture table: per tool, typical size, cap,
which end survives, and what happens to the remainder. Before this the tree had five different
phrasings of the notice, three of which (`…(truncated)`) said only that something was cut — not which
end, not how much, and not how to get it back.

Two values did change, and both were argued:

- **`shell`'s cap was `200 << 10` — 200 KiB, ~51,200 estimated tokens, larger than the entire context
  window under the local profile.** A cap that cannot bind before the window does is not a cap. It is
  now 24 KiB, the value the skill-script cap already chose for the same class of thing (a subprocess
  writing to stdout); aligning two caps within one class is not the homogenising the item warns
  against, it is removing an 8x difference nobody chose. `TestResultCapsCanBindBeforeTheContextWindow`
  is the one **gate** in this item, and reverting the constant fails it with a message that explains
  itself.
- **`shell` now keeps the tail, where it kept the head.** Command output is a log; a failing build
  prints its errors last, and the head of an over-cap `go test ./...` is the list of packages that
  passed. The head was never argued for — it is what a `text[:max]` slice does.

*The instrument, and it contradicted the filing.* `TestResultSizeComposition` reports measured bytes
and tokens per call over a fixture workspace, mirroring `TestBasePromptComposition_localProfile`'s
shape and printing under `-v`. It **reports and does not gate**, on the item's own reasoning: result
size depends on the workspace, so a CI threshold keyed to it would fail on someone else's repo. The
item was filed believing `shell`'s 200 KiB was the anomaly. **It was not the largest result a built-in
can return — an ordinary default `read_file` was**, at 58,000 bytes / 14,501 estimated tokens for a
2,000-line source file, because `defaultReadLines` bounded lines and *nothing bounded bytes*. That is
the fourth consecutive item in this document whose measurement contradicted part of its own write-up,
and it is the one that would have been missed by building from the filing alone. `maxDefaultReadBytes`
(32 KiB) is defaultReadLines' missing other half.

**P64.1 — a capped result's remainder is recoverable (SHIPPED 2026-08-14, both layers).**

`truncNotice` was honest and was not a recovery path: a model told "these are 500 of an unknown number,
in no particular order" has exactly two moves, reason from a partial set anyway or re-run a narrower
query it has no information to narrow correctly. The full result now goes to a file and the notice
names it.

**The design question the item said could change the answer was answered by measurement, and it did
change the answer — twice.** `TestSpillLocationIsReachableByTheModel` asks the sandbox rather than
reading it:

- **`<data_dir>` is unreachable.** The obvious home, alongside `builtin-skills/` and
  `model_caps.json`, sits outside every root `sandbox.ValidatePathIn` accepts — `read_file` does not
  merely refuse a path there, it fails with "escapes the workspace root". A locator the model cannot
  open is worse than no locator, so the spill lives at `<workspace>/.aegis/spill/`.
- **The item's own suggested wording cannot be honored.** It proposes the hint *"or grep this path to
  search within it"*. `grep` has no path parameter at all — it always searches the workspace root —
  and `.aegis` is in `skipDirNames`, so neither search backend descends into the spill directory.
  `spillLocator` therefore offers `read_file` with offset/limit and **does not promise grep**. Naming a
  recovery path that silently returns "no matches" is precisely the failure this item exists to
  remove, and the test asserts grep's blindness so that if either decision changes the wording gets
  revisited.

All three of the borrowed design's load-bearing details are honored and tested. The notice's bytes are
**reserved out of** the cap, so spilling can never *add* tokens to a turn — removing the reserve fails
`TestTruncateNeverExceedsTheCap` at every limit. `read_file` is **excluded**, since its remainder is
the file, already addressable with offset/limit, and spilling a read produces a file the model would
read again. And the whole thing is **best-effort**: no root, an unwritable workspace or a failed write
leaves the inline result exactly as it was and never converts a success into `isError`.

**The item-level half — the one the item says a careless port would miss — is built.** `glob` and
`grep` cap at the *result* level, so by the time a byte policy sees the result the tail matches are
already discarded; only the collector can spill them. `grep` now collects to `grepSpillMaxMatches`
(2,000) while showing `grepMaxMatches` (500). **Two numbers, not one, because they bound different
costs:** the inline cap bounds the model's context, the collection cap bounds the *walk* — and the walk
is what CLAUDE.md's streaming-cancel note is about, where cancelling ripgrep at the cap took a common
pattern from 964ms to 46ms on a 40k-file tree. Measured on this repo before choosing 4x: `func `
collects 2,000 matches in ~90ms against ~50ms for 500, where uncapped collection costs ~900ms for a set
no model will page through. `TestItemSpillCarriesMatchesPastTheInlineCap` asserts specifically that a
match *beyond* the inline cap is recoverable, which no byte-level spill could deliver.

**One thing the round-trip test found that neither the filing nor the build anticipated.** Asserting
recovery end-to-end — read the locator out of the notice, open it, look for the dropped bytes — fails
on a default read, because a spill file is large by construction and `read_file`'s default window
returns only its head. That is not a spill bug and the locator already says "with offset/limit to
page", but it means the naive recovery attempt returns *the same head the model already had*. The test
now pins both halves: a default read of a spill must tell the model how to page, and the instructed
paged read must reach the dropped tail. A test that only checked "a file was written" would have passed
while the recovery path silently didn't work.

Reaping is TTL (24h) plus count (200) plus bytes (64 MiB), oldest-first — never the newest, which is
the one the current turn's notice points at. Scoping is by **workspace**, not session: the tool layer
has no session id (ctx carries a workdir, extra roots and a registry, nothing else) and adding one to
`internal/tool` is outside this change. That is weaker than the item asks in exactly one way, stated in
the code: a spill from a previous session in the same workspace stays readable until it ages out.

**Last updated (earlier the same day):** 2026-08-14 — **P62.10 shipped, all four parts**, which is where the prompt-cost work
finally lands on the path a local conformance run actually takes: **phase 6 of the phased drive now
narrows to a declared surface, 3,492 → 1,209 schema tokens per turn**, and all four CLI entry points
carry the local prompt profile — the last of them on live evidence rather than on a token count, which
is also what turned up an `edit_section` description pointing at a deferred tool.
**P62.9 built, closure pending live verification**, taking the
local-profile base prompt from **4,907 to 4,317** estimated tokens (12%) and finding a latent
phase-scoping bug on the way. **P62.6 built and closed** the same day, taking it from **7,790 to
4,907** (37%). P62.10's write-up is immediately below, then P62.9's, then P62.6's, then the 2026-08-10
Tier 3 batch.

**P62.10 — the local profile reaches the rest of the CLI, and the drive's longest phase finally
narrows (SHIPPED 2026-08-14, all four parts).**

The item was filed off P62.9 by asking how far that change reached, and the build inverted its own
ranking. The half it led with — `LocalProfile` passed at one of five `builtin.Register` call sites,
worth a measured 1,318 schema tokens — is partly blocked on the live tier, because its most valuable
site is `aegis chat`, the harness every P38.1 re-test drives. The half it filed second, described
only as "phase 6 narrows nothing", turned out to be **2,283 tokens per turn** on the phase a build
spends the most turns in, needing no live tier at all because P39.14 had already decided that phases
narrow. Filed cheap, worth more.

**1. Phase 6 declares a tool surface (3,492 → 1,209 schema tokens per phase-6 turn).** `scopeTools`
had exactly two callers — the content-phase loop and the re-opened content phase — so
`runPhasedVerifyAndQuality` (the `MaxVerifyRounds` fix rounds plus the quality pass, each its own
fresh context) ran on the session's whole surface, `web_search` included: the tool
`TestScopeToolsPerPhase` asserts no content phase offers, and which the P39.14 comment names as "the
detour that opened a real run". `phase6Tools` is `fillPhaseTools` + `threat_model_inventory`, read off
phase 6's own prompts rather than guessed — they say read the suite first, fix in place with
`fill_marker`/`edit_section`/`edit_file`, never re-scaffold and never `write_file` a suite file, so
`write_file` stays out for the reason the fill phases dropped it, and the one addition is the tool that
*generates* `inventory.yaml`, which the assessment prompt already forbids hand-writing.

Two mechanics are load-bearing. The scope is taken once for the whole round rather than per turn,
because every iteration is the same phase; a re-opened content phase narrows further on top of it and
restores back into it. And the content loop releases its own narrowing *before* phase 6 scopes, so
phase 6's baseline is the session surface rather than whichever content phase happened to run last.

The one judgement the build added rather than inherited: narrowing is gated on the *plan* having
declared per-phase tools at all. A plan assembled from a skill's `phases:` frontmatter declares none,
and handing its verify round a threat-model surface would take `web_search` away from a deep-research
fix round that never opted into narrowing anywhere else. Declared narrowing stays declared.

**2. `dry-run`, `worker` and `debate` carry the profile.** `dry-run` was the one to fix first because
it is an *instrument*: its whole job is "preview what this session will send without calling the
model", and against a loopback `base_url` it printed a tool list the session would not use. That is
P62.4's lesson in miniature — an optimization measured with a broken instrument produces a confident
wrong verdict — and this is the operator-facing report for exactly the question P62.6 and P62.9 were
answering. It now labels the active profile in its header and prints deferred tools in their own
section, because under the local profile most of the inventory moves there and a silently shorter list
reads as "these tools are gone" rather than "one `tool_search` away".

`worker.go` is the third thing the subprocess sub-agent did not inherit, after the gate stack (P10.1)
and the sandbox (P10.2) — it reconstructs `cfg` from disk and then has to actually consult it.
`debate.go` was not in the filing's three-site list and should have been: a debate is several full
engine runs per round, so the per-turn schema cost is paid more times there than anywhere else in the
CLI.

**3. The omission is now a sentence somebody wrote, not a field somebody forgot.**
`TestEveryRegisterCallSiteDecidesTheLocalProfile` walks the repo's production Go source, finds every
`builtin.Register(` call, reads it through its matching paren, and requires each either to pass
`LocalProfile:` or to explain in the comment directly above it why it does not. It also asserts a floor
on how many sites it found, so a refactor that renames the call or hides every site behind a helper
fails rather than passing vacuously. `chat.go` is the only site using the escape hatch, and its comment
now states the specific evidence that would change that: `--skill` layers the drive's per-phase tool
arrays on top of whatever is registered there, so deferring `edit_file` on that path changes what those
arrays resolve to mid-drive.

**4. `aegis chat` carries the profile too, and the live tier is what says so.** This was the site the
filing deliberately gated: `aegis chat --skill` is the harness every P38.1 re-test drives, and the
phased drive layers its per-phase tool arrays on top of whatever is registered here. It was measured
rather than argued — the seeded-bug task run end to end through the real command against qwen3:14b at
a 16,384-token window, three runs per arm, outcome re-derived by re-running the script rather than by
believing the model:

| arm | tool calls | sequence | outcome |
|---|---|---|---|
| `edit_file` exposed (before) | 3, 3 | shell → edit_file → shell | passed 2/2 |
| deferred, before the fix below | 8 | shell → edit_section ×3 (failed) → read_file → multi_edit → shell → multi_edit → shell | passed |
| deferred, after it | 4, 6 | shell → read_file → multi_edit → shell | passed 2/2 |

**The predicted refutation did not occur: no run ever called `tool_search`.** P62.9's stated failure
mode was a turn spent hunting for a tool the model knows by name; across three deferred-surface runs
the model went straight to a handle-based editor instead. The deferred surface did take more tool
calls (4-6 against a steady 3), and that is recorded as a caveat rather than a finding, because a
control arm with `edit_file` exposed then failed the task **twice** by explaining the fix in prose
instead of applying it — run-to-run variance on this task is larger than the difference being
measured. The clean number from the same afternoon is prompt size, through the daemon tier with
neither count clamped: local 4,854 vs default 8,376 first-turn input tokens.

**5. The defect the live tier actually found, which nobody had filed.** `edit_section`'s description
ended "use `edit_file` for a surgical change" and its no-headings error read `%s has no markdown
headings` and stopped there. Against a `.py` file with `edit_file` deferred, that is a description
pointing at a tool the model cannot see plus an error carrying none of the information needed to
recover — and it cost three consecutive failed calls and a tool-failure-breaker trip in the run above.
Both now name `multi_edit`, chosen because it is exposed under **both** prompt profiles; the error also
says *why* there is nothing to edit rather than only that there is not. That is P39.16's finding (a
tool that holds what the model needs and returns an error without it) and P62.9's finding (a declared
surface naming a tool that is not on it) landing in the same three lines, in a surface — a tool's own
description — that neither item had thought to check. `TestEditSectionOnHeadinglessFileNamesTheToolThatWorks`
pins both halves, including that the message must *not* name `edit_file`.

**Verification.** `go test ./...` green. Four mutations checked, all killed: dropping phase 6's scope
call (0 scope calls observed), narrowing an undeclared plan, putting `web_search` back on the phase-6
surface, and silently removing `LocalProfile` from `dry-run` while leaving no explanation. The
1,318-token profile delta and the 2,283-token phase-6 delta were both re-measured through `tokenest`
against the CLI's own option set on an empty workspace; the former reproduces the filing's number
exactly (3,492 → 2,174 exposed-schema tokens, 19 → 14 tools). Eight live runs total across the arms
above, on a machine whose Ollama defaults to a 4,096-token window — which is worth recording on its
own, because at that window the local base prompt *is* the entire context and the daemon tier's
`FixSeededBug` fails for want of room, not for want of a fix.

**P62.9 — the exposed-schema half: one deferred editing tool, three local prose blocks, and a
declared surface that was not real (BUILT 2026-08-14, closure pending the live tier).**

| | before | after |
|---|---|---|
| tool schemas | 3,275 (27 exposed) | **3,090** (26 exposed) |
| shared prose blocks | 1,001 | **581** |
| `<deferred_tools>` | 409 (13 tools) | 425 (14 tools) |
| persona + everything else | 222 | 222 |
| **total** | **4,907** | **4,317** |

Unlike its parent, both halves are behaviour changes on the local profile rather than defect fixes,
so this is written up as **built, not closed**. The closure condition is a `TestLiveWorkflow` run
showing the agent's behaviour is not worse; no model server was reachable when this landed, and the
numbers below are what the change *costs*, not evidence that it is free.

**1. The editing surface (185 tokens), and it is the inverse of the obvious cut.** Five editing tools
were exposed at once for 1,299 tokens, and the token-greedy move is to defer the two most expensive —
`edit_section` (407) and `multi_edit` (276) — for ~500. That is precisely backwards. P39.16 shipped
the handle-based tools *because* small models fail `edit_file`'s byte-exact `old_string` match, at 12
consecutive `edit_file` failures becoming 7 clean `edit_section` calls. So the local profile defers
`edit_file`, the cheapest of the five and the one a small model uses worst, and keeps the four it was
introduced in favour of. Deferred rather than removed: `tool_search` loads it by name, a surgical
single-line or table-row change is still what it is best at, and the default profile is untouched. A
test in `builtin_test.go` pins the *direction* — the four handle-based tools must stay exposed —
because a later pass optimizing the same number would otherwise undo this by taking the bigger saving.

**2. The three shared prose blocks (420 tokens), compressed and not cut.** `completing-tasks` (464),
`platform` (284) and `tool-use` (253) had no local variant. They do now
(`persona.ToolUseBlockFor`/`CompletingTasksBlockFor`/`PlatformBlockFor`, selected by the profile in
both `server.effectiveSystem` and the CLI's `buildChatSystem`), and the design rule is that they say
the same things in fewer words. The tempting version of this change is to *drop* rules, and every one
of these rules was added after a real run went wrong — narrating instead of calling a tool, answering
in chat instead of writing the requested file, a skeleton reported as finished, a PowerShell command
written in bash. What a small model loses when a rule is removed is exactly the question the live tier
exists to answer, so nothing was removed on inference. `TestLocalBlocksKeepEveryRule` enumerates each
rule with a phrase that must survive into the local text.

The single deliberate deletion is duplication, not a rule: the default platform block ends by
repeating the tool-use block's "call the tool immediately, do not narrate" sentence, and both blocks
are always injected together. That one sentence is the largest saving in the platform block, and the
test asserts the default block still carries it — so if the exception ever stops describing anything
real, it fails rather than rots.

**3. A declared tool surface that was not real (no tokens, found on the way).** `edit_file` appears
in every one of the drive's phase tool lists, which raised the question of what happens when a phase
names a deferred tool. Nothing did: `Registry.ScopeExposed` narrowed and never widened, so a named
deferred tool stayed hidden. **Two phases had been in that state since they were written** —
`dfdPhaseTools` names `render_diagram` and `assessmentPhaseTools` names `yaml_validate`, both deferred
tools — meaning the DFD phase was told to render a diagram and the assessment phase to validate YAML
with prompts naming tools that were not in their arrays. `ph.tools` is a *declared surface*: naming a
tool is the drive saying the phase needs it. `ScopeExposed` now loads a named deferred tool for the
scope's duration and the restore returns it to deferred.

The narrowing-only rule is kept for everything else, and the line between the two cases is the point:
a tool hidden by a `SetExposed` permission decision stays hidden even when named, because widening
there would be an escalation. Deferral is not an escalation — it is a prompt-cost mechanism, and
`tool_search` can load any deferred tool at any turn. `TestPhaseToolsSurviveScoping` runs the real
builtin registry under the local profile against all four phase sets, so a future deferral that
strands a phase tool fails in `go test ./...` rather than in a live run.

**Closure.** `localBasePromptCeilingTokens` 5,200 → 4,550 against the measured 4,317, with the
comment recording both changes and stating plainly that the live confirmation is outstanding. Three
mutations were run and all three killed: the deferred-loading branch in `ScopeExposed` (kills the new
registry test *and* `TestPhaseToolsSurviveScoping` with all three stranded tools named), the local
prose selection reverting to the default blocks (kills the size test, and the budget test at 4,595
over a 4,550 ceiling), and `edit_file` exposed under the local profile. One scope note recorded in
code rather than left to inference: `aegis chat` never opted into P25.6's tool half, so the CLI path
gets the local prose blocks but still exposes `edit_file` — turning that on is a P25.6 extension with
its own evidence to gather, not something to change on the way past.

**P62.6 — the base prompt is 84% tool inventory, and deferral is most of the waste (SHIPPED 2026-08-14).**

Three fixes, and the order was the point rather than an implementation detail. The item was promoted
on a measurement; that measurement turned out to be taken with an instrument that was itself 4.4%
wrong, which is the nineteenth pass's lesson arriving one item later than it should have.

| | before | after |
|---|---|---|
| tool schemas (27 exposed) | 3,614 | 3,275 |
| `<deferred_tools>` | 2,953 (26 tools) | **409** (13 tools) |
| prose blocks + persona | 1,223 | 1,223 |
| **total** | **7,790** | **4,907** |

**1. The instrument, first (339 tokens that were never real).** `tokenest.Tools` priced
`ToolSchema.OutputSchema`. No adapter sends it: `provider/anthropic` builds its `wireTool` from
name/description/input schema, `provider/openai` sets only `Function.Parameters`, and
`toolshim.Prompt` renders only the input schema. It is a P3.6 affordance for clients and validators,
never for a model. This matters twice — it over-attributed 4.4% of the measured prompt to schemas,
and the estimator's one production caller is `compactionGuard.requestOverhead`, so every session had
been spending phantom tokens of headroom and compacting that much early.
`TestToolsIgnoresOutputSchema` pins the omission *against the adapters' wire shape* and says in its
comment that the day an adapter starts sending output schemas, this failing is the signal to re-add
the term rather than to delete the test — the omission flips from a harmless overcount to an
undercount, which is the dangerous direction for a compaction trigger.

**2. The deferral advertisement (2,279 tokens).** `deferredToolsBlock` printed each unloaded tool's
full `Description()` — an operator's manual, 2,374 bytes for `security_scan`, 593 tokens to advertise
a tool that is *not loaded*. It now prints `tool.Summarize(t)`: a `ShortDescription()` if the tool
declares one (new optional interface, same idiom as `OutputSchemer`/`PollExempter`), else the first
sentence capped at 140 chars, with parenthesis depth tracked so the `(opengrep, trivy, e.g. …)`
lists these descriptions open with do not end the sentence three words in. 2,953 → 674 at 26 tools.

**This costs nothing in discovery, and that is what made it cheap rather than a trade-off.**
`Registry.SearchDeferred` matches a query against the **full** name+description, which lives in the
registry and not in the prompt, and `tool_search` returns the full description with the schema on
load. A scanner name trimmed out of a summary is still findable. `tool.Info` carries both fields for
exactly that reason, and a test asserts a term present only in the dropped half still resolves.

Seven tools declare a hand-written summary, chosen because the derived one reads badly rather than to
save bytes: the four security tools (whose first sentences are long parenthetical inventories, and
one of which opened with "Security engagement assistant (P13.4):", leaking a roadmap id into the
system prompt), `scope`, `entity_remember`, `cron_history`.

**3. Tool families the local profile has no use for (265 tokens, 13 of 26 deferred tools).** The
daemon wires the team task list, cron scheduler and long-term memory store unconditionally, so every
local session advertised `team_send`/`team_inbox`/`team_task_*` to teammates it does not have, plus
`cron_*` and `entity_*`. Under `LocalProfile` those three families are no longer registered. This is
a **profile default with an additive override** (`tools.families`, the same shape and rationale as
`skills.builtin_enabled`) rather than a deletion, because none of the three is genuinely *unusable*
on a local model — a local model driving a swarm is a real configuration, just not the one the
profile is tuned for. The gate is deliberately narrow: security/latex/diagram stay, both because a
local run reaches for them and because P34.3's persona preload depends on the security tools being
registered-and-deferred rather than absent.

**Closure.** `localBasePromptCeilingTokens` 8,200 → 5,200 against the measured 4,907, with the
comment recording where each of the 2,883 tokens went so a future reader can tell a deliberate move
from a silent one. Nine mutations were run over the new code — both edges of `summaryMaxChars`, the
`ShortDescriber` dispatch, the parenthesis-depth check, `Info.Summary` reverting to the full
description, the block printing `Description` again, both directions of `familyEnabled`, and
`tokenest` counting `OutputSchema` again — and all nine were killed. The `summaryMaxChars` band test
uses **literal** numbers rather than the constant, because every other test in that file references
the constant symbolically and would survive any mutation of it; that is this repo's third-consecutive-
pass finding applied on purpose.

**What was deliberately not done, and is now P62.9.** The exposed-schema half: five editing tools at
1,299 tokens (26.5% of what remains) and 1,001 tokens of prose blocks with no local-profile variant.
Both are behaviour changes needing the live tier, not unit tests — and the obvious editing-tool move
points the wrong way, since P39.16 shipped the handle-based tools precisely because small models fail
`edit_file`'s byte-exact match.

**Last updated:** 2026-08-10 — **the Tier 3 batch: P39.17, P39.18 and P62.7 shipped, and P62.6
measured and promoted to Tier 2 rather than built.** Four items worked in parallel, and the pass's
finding is about *deferral* rather than about any of them: P62.6's composition split shows the tool
inventory is **84.3%** of the base prompt, and that `<deferred_tools>` costs 2,953 tokens — 82% of
what the actually-exposed schemas cost — to advertise 26 tools that are not loaded. The mechanism
built to reduce prompt cost is itself most of the prompt cost. Write-ups immediately below; the
2026-08-09 P62.4/P62.2/P62.5 entries follow them.

**P62.6 — the base prompt is 84% tool inventory, and deferral is most of the waste (MEASURED 2026-08-10, promoted to Tier 2, not built).**

The item was explicit that it was measure-first and that its fix was a design question, so this pass
took the measurement and stopped, exactly as filed. `TestBasePromptComposition_localProfile`
(`internal/server/server_test.go`) assembles the base prompt the way the daemon wires it — loopback
`base_url` so the local profile auto-detects, plus the task manager, cron scheduler, todo list, team
task list, knowledge store and memory store the daemon passes. Wiring it *without* those was the
first wrong answer: it misses 10 tools and undercounts by ~1,360 tokens.

| component | est. tokens | % |
|---|---|---|
| tool schemas (27 exposed) | 3,614 | 46.4% |
| **`<deferred_tools>` (26 not loaded)** | **2,953** | **37.9%** |
| completing-tasks / platform / tool-use blocks | 1,001 | 12.8% |
| persona (`general`) | 222 | 2.8% |
| skills, repo map, memory | 0 | 0% |
| **total** | **7,790** | |

The 7,119 live figure reproduces in shape at 7,790 estimated (~9% high — `tokenest` is a heuristic
that prices JSON schema text at flat chars/4 where a real BPE compresses it better). Same order,
same dominant component. On an empty workspace skills/repo-map/memory are genuinely zero, so **this
is a floor, not a ceiling**.

**The finding the item did not anticipate is that deferral is nearly as expensive as exposure.**
`<deferred_tools>` spends ~114 tokens per tool on what should be a name-and-description line. The
four security tools alone are 1,538 tokens — 19.7% of the entire base prompt — *while deferred*;
P25.6 moved `security_scan` out of the schema block and it still costs 593 tokens in the
advertisement. That reorders the candidate list the item proposed: progressive tool disclosure is
the pattern already in use here, and it is the thing to fix before it is the thing to extend.

Also worth recording for whoever takes the design question: the three P39.16 handle-based editing
tools (`edit_section`, `multi_edit`, `fill_marker`) are 1,009 tokens, 13% of the base prompt, and are
three of *five* editing tools exposed simultaneously alongside `edit_file` and `write_file`.

Half two shipped regardless of the design question, because it does not depend on it:
`TestEffectiveSystem_localProfileBudget` asserts a `localBasePromptCeilingTokens = 8200` ceiling in
the **plain** suite (no build tags), so growth is caught by `go test ./...` rather than rediscovered
by a live run. Its comment states it is a budget rather than a target — deliberate growth moves the
number *with* a note saying what was added. The composition test also cross-checks its own component
table against `effectiveSystem`'s assembled byte length, so a block added to one and not the other
fails loudly instead of silently skewing every percentage.

**Promoted to Tier 2 on the item's own stated trigger** ("promote if the composition split shows one
component dominating"). It does.

**P39.17 — a per-turn stall detector, because every other guard needs turns to keep completing (SHIPPED 2026-08-10).**

Filed off the P39.16 validation run, where a drive stopped producing output and was still "running"
14 minutes later with 0.5s of total CPU accumulated since launch. Every existing guard is
*progress*-shaped — the no-progress nudge counts turns, the loop detector compares tool calls, the
failure breaker counts failed rounds — and all require turns to keep completing, so a turn that never
returns advances no counter and an unattended run sits dead looking exactly like a slow one.

`internal/engine/stall.go` adds `stallWatch`: a cancellable context derived inside the run deadline,
sampled at `limit/4`, beaten by every provider stream event and on **both edges** of `executeTool` in
*both* the sequential and parallel tool paths. On firing it cancels (the P59.2 mechanism — a poll
cannot fire inside a wedged call), then re-attributes whatever the cancelled call returned to an error
wrapping `ErrTurnStalled` naming the idle time, the limit and the config key. Returned from `Run` and
emitted as a `KindError` event. Ordering is `budget.override(stall.override(err))`: an explicit
operator wall-clock bound outranks a stall diagnosis.

**`cost.max_turn_stall`, 900 seconds, on by default** (`0` disables) — and both halves of that need
justification. It *can* be on by default because it measures **silence, not duration**: a wall-clock
cap cannot tell a stalled run from a slow one making real progress, which is why
`cost.max_wall_clock_per_run` is off, but "no streamed token and no tool started or finished" needs no
judgement call. And 900 rather than 600 because it is a **backstop** and must sit above every narrower
timeout it backs up (`provider.stream_idle_timeout` at 10 min, the shell tool's 600s per-call ceiling,
cron's 10-minute bound) so those still report their own, more precise failure first. A config test
asserts the default exceeds 10 minutes, so the layering cannot silently invert.

**Fatal to the drive, not resumable.** All three reset ladders (`recoverPhase6Overflow`,
`recoverToolFailureStall`, `recoverReasoningLoop`) decline it, and the reasoning is worth keeping:
their shared premise is that the *context* is the defect, and a stall makes no claim about the
context — the backend, transport or tool is wedged, and a fresh conversation is handed straight back
to it. Auto-retrying would also recreate precisely what this item was filed against, an unattended run
burning hours while looking healthy. The on-disk suite survives, so re-running still resumes.

*Mutation testing was the part that earned its keep.* Six mutations, **three initially escaped**:
weakening the threshold to `limit*3` escaped because no test bounded *when* detection happened (fixed
by a `< 2*limit` latency assertion); deleting the tool-phase beat escaped because every false-positive
case happened to be beaten by stream events *between* rounds (fixed with a `CapWrite` slow tool, hence
serialized by the exec lock, spanning 480ms with no provider event at all); and deleting the
stream-loop beat escaped because the drip case finished *inside* the bound (fixed by raising the drip
and asserting the case actually reached the detector). A fourth weakness surfaced on the way: the drip
adapter returned silently on cancel, so a spurious stall truncated the turn into a valid empty final
answer and `Run` returned `nil` — it now emits an `EventError` like a real adapter.

**P39.18 — typed tools for the bundled skill scripts, so an argument error is structurally impossible (SHIPPED 2026-08-10).**

With per-phase narrowing and handle-based editing in place, tool *selection* stopped failing on
qwen3:14b and every remaining stumble was a malformed **argument** — `scaffold.py --framework` with
the value omitted, `2-<framework>-analysis.md` with the placeholder never substituted. Same class
`fill_marker` removed from editing: the model was composing a command line as a *string*.

Five tools (`threat_model_recon`, `_scaffold`, `_inventory`, `_verify`, `_normalize_ids`), one per
script, all `CapExecute` — the same gate the shell calls they replace already passed through, so no
permission-posture change. **One tool per script rather than a generic runner** because a generic tool
needs a free-form `args` blob to span five argument sets, which is string composition wearing a JSON
hat and cannot express "`framework` is a required enum" — the entire point of the item.

Schemas are derived from each file's real argparse, and `threat_model_scaffold`'s `framework` is a
required enum of scaffold.py's actual `FRAMEWORKS` keys (minus its two aliases, so there is one
canonical spelling per choice). **A test parses `scaffold.py` and fails if the schema and the script
ever drift**, which is what stops the schema rotting away from the thing it describes.

*Scoping matters more than the tools, given what P62.6 measured the same day.* These are **not** in
`builtin.Register`. `builtin.ThreatModelScriptTools(root, skillDir)` constructs them and they are
`Upsert`ed onto the **session registry clone** only when a run has loaded a skill that bundles the
scripts — three wiring points (`cli/chat.go` after `--skill` resolves, `server/drive.go`'s
`resolveDriveSpec`, and `server.go`'s `activateSessionSkill`), so the daemon-wide surface never grows
and the default prompt pays nothing. Within a drive, the existing per-phase `ScopeTools`/`ScopeExposed`
seam narrows further: only setup sees recon+scaffold, only assessment sees inventory. The constructor
also stats each script and omits tools for scripts an older materialized skill build lacks.

Both halves of the closure condition are met, and the second was verified rather than assumed: shell
was in `setupPhaseTools` for exactly recon.py, `date` and scaffold.py, and in `assessmentPhaseTools`
(per its own comment) solely for inventory.py — **no threat-model phase exposes shell now**. Two
extras removed the remaining shell uses: with `run_dir` omitted, `threat_model_scaffold` derives and
reports its own timestamped run directory from the host clock, killing both the `date` call and the
hand-composed path that produced the unsubstituted-placeholder bug. Every path argument goes through
`sandbox.ValidatePath`. Invalid arguments are rejected in the argv builder — **python is never
spawned**, proven by a sentinel-file test — and each rejection enumerates the accepted values, the
way a bad `fill_marker` index is answered with the markers that exist.

**P62.7 — compaction stops re-pruning for almost nothing every turn (SHIPPED 2026-08-10).**

The item inferred the defect from message counts; this pass measured it in bytes first, as the item
asked. `TestPruneYieldPerTurnMeasurement` reproduces the live trace deterministically (24,576-token
window, trigger 15,156, the real `Summarizer`):

| turns | yield | ratio to the gap it must close |
|---|---|---|
| 5–15 | 180 chars / 45 tokens, *every turn* | **0.03 → 0.01×** — 11 notices in a row, counts unchanged |
| 16 | 73,188 chars / 18,419 tokens | **3.99×** — the real summarization |

The premise holds with a very large margin, and the two distributions are cleanly separated, which is
what makes the threshold defensible rather than tuned. The test fails loudly if that separation ever
stops being true, so the fix cannot silently become pointless.

**The root cause is the conflation the item's option (c) named.** `(*Summarizer).compact` runs the
deterministic pre-pass, sets `changedByPrune = prunedChars > 0`, then returns early from
`!s.shouldCompact(...)` — so summarization never runs (hence unchanged message counts) but `changed`
is `true` because the prune freed a handful of characters. In the engine that triggers
`conv.invalidate()` plus a "compacted N→N messages" notice, and the invalidate is what costs the full
~9s prefill recompute.

So (c) was built as a **prerequisite** for the preferred (a), not as an alternative to it: the engine
cannot apply a yield check to a bare bool. `YieldReportingCompactor` is an optional interface matching
the existing `FallbackCompactor`/`CalibratedCompactor` pattern, so every other Compactor keeps
compiling with today's behaviour. It returns plain values rather than a struct **because a shared
struct type would close an import cycle** — engine's in-package tests import compaction, so compaction
cannot import engine.

`minPruneYieldFraction = 0.25` sits mid-void between the measured 0.01–0.03 and 3.99, mirrors
`prunePrefixCacheRatio`, and stays well under 1.0 so a prune genuinely chipping at the excess is never
suppressed. On a low-yield prune the guard records `retryEstimate = est + gap` — "grown by at least
what stood between it and the trigger" — and since the gap doubles each time it fires, re-attempts
back off geometrically. Option (b), a cooldown in turns, was rejected: a turn count is not the thing
that matters. When suppressed, `beforeTurn` returns **before** calling Compact at all: no invalidate,
no notice, no work. Net effect on the fixture: 12 applied compactions → **3**, with the real
summarization still happening one turn later.

`compactionSuppressCeiling(window, trigger) = trigger + (window-trigger)/2` keeps this a throughput
change rather than a safety change — past it, suppression always yields. It **subsumes** the 95%
context-full notice path, since `compactionTrigger` never exceeds 85% of the window, and that is
proved across all window/max-token pairs rather than asserted for one case
(`TestSuppressionCeilingIsBelowNinetyFivePercent`). P62.4 already burned someone by gating that
notice on the wrong number; it was not going to be regressed on the way past.

*Mutation testing repeated the P63.9 lesson exactly.* Eight mutations, and **`0.25 → 0.5` survived**
the suite as it stood — a fixture cannot tell adjacent thresholds apart. Fixed by
`TestMinimumYieldBoundary` (249 vs 250 tokens of a 1,000-token gap). The suppression test asserts the
exact **sequence** `[5 10 16]` rather than a count, for the same reason. No golden transcript changed,
and the reason was checked rather than observed: the eval scenarios never cross the trigger.

**P39.16 — the small-model tool batch: handle-based editing, per-phase tool surfaces, and pinned sampling (SHIPPED 2026-08-09).**

Filed off a P38.1 re-test on `hf.co/LiquidAI/LFM2.5-2.6B-GGUF` that produced **zero files in two
runs**, then validated on qwen3:14b. The batch's organizing finding is narrow and repeated ten times:
**every failure was a tool that knew the answer and did not say it.** The marker list, the PowerShell
equivalent, the offending filename character, the file's real path, the duplicate heading's index —
in each case the harness held exactly the information the model needed and returned an error without
it. Fixing messages beat fixing models, and the one error written that the model *could not act on*
("rename or disambiguate them") produced an immediate loop until the drive reset it.

*Handle-based editing (`fill_marker`, `edit_section`).* `edit_file` requires `old_string` reproduced
byte for byte, from memory, through JSON escaping — the single hardest thing to ask of a small local
model, and the one it fails most reliably. Both new tools select a target by *handle* (marker
index/key, section heading/index) and take only new text. Measured on the same phase: 12 consecutive
`edit_file` failures (10 "old_string not found", 2 "occurs 2 times") became 7 `edit_section` calls
with zero failures. This is not a 2.6B problem — **qwen3:14b fails the exact-match path too**, which
is why the deep-fill probe was re-cut against `fill_marker` (it was failing models that complete real
drives).

*Structure guards on `edit_section`.* Selecting by handle means the caller cannot see the extent of
what it selected, so the tool must describe the blast radius before acting. Two refusals, both found
live and both destructive when absent: replacing a section that holds a markdown table with prose
that holds none, and replacing a parent section whose body contains nested subsections (a section
runs to the next same-or-higher heading — this silently deleted 3.7KB of analysis and reported
success). `allow_structure_loss` carries the intent when removal is deliberate.

*`mode:"new"`.* A capability gap, not a bug: narrowing a phase's tools must leave a path for every
operation the phase can legitimately need. Fill phases could edit and fill but not **create**, so a
model asked to author eleven missing component sections rewrote the one section it could reach until
the drive reset it. With section creation available it wrote all eleven in one pass (1481 → 6540
bytes).

*Per-phase tool narrowing (`Registry.ScopeExposed`).* A 2.6B offered 50+ schemas used four, and its
one wrong choice — an unprompted `web_search` — opened a phase and spent its context on the public
web. Persona `Tools` could not help: `PersonaToolGate` warns *after* the call while every schema is
still sent. Narrowing the schema array is the only tool-selection instruction a small model cannot
ignore. Wired to the CLI drive alone, which owns its registry; the narrowing is registry-wide.

*Sampling knobs (`provider.temperature`, `provider.seed`).* `Temperature` was plumbed end-to-end and
**never set by anything**, so every local run inherited Ollama's 0.8 default: two runs of one prompt
against one model took visibly different paths (one opened by writing a file, the other by running a
web search). Both are pointer-typed so `temperature: 0` stays distinguishable from unset.

*Corrective errors.* `write_file` refuses a directory-shaped path (a trailing separator was cleaned
away, creating a zero-byte *file* where a run directory belonged and making every later write fail
with an opaque `MkdirAll` error) and an invalid Windows filename character (a model copied the
literal `2-<framework>-analysis.md` placeholder from its own skill docs and retried it verbatim).
Not-found errors now name the file's real location, and treat an unsubstituted `<…>` placeholder as a
glob — one such hint replaced 40 wasted `read_file` calls and a stalled phase with 4 calls and a
completed phase. The materialized built-in skill tree is refused to all six write-capable file tools
after a model overwrote `recon.py` with the command line it meant to run, and the drive
re-materializes it at every phase boundary to cover the shell tool.

*One pre-existing correctness bug, unrelated to model size.* `read_file` reported a **missing** file
as `"is empty (0 bytes)"` — `looksBinary` returns size 0 for anything it cannot open, and the
zero-size branch fired before `os.Open` was reached. Any caller was being told a nonexistent file
exists and is blank.

*What is not closed.* The verify-clean conformance run itself — see P38.1 and the new P39.17.

**P62.4 — the compaction trigger was measuring the wrong prompt (CLOSED 2026-08-09).**

The defect had two halves and only the second one needed a learning algorithm.

*The structural half.* `tokenest.Messages` prices `System` + `Messages`. A request also carries
`Request.Tools`, set from the registry on every native-tool-calling turn, and a backend counts those
schemas in `prompt_eval_count` exactly like transcript text. Nothing ever added them. With 50+
builtin tools that is thousands of tokens present in every request and invisible to the one check
whose job is to compact *before* a local server silently drops the oldest turns. The tool-shim path
(P53.6) never had this hole — under the shim the schemas are rendered into the system prompt, so the
estimate saw them, and someone had already measured `toolshim.Prompt` separately on top. The native
path attached the same information to a *different field*, where nothing about the estimate looked
wrong. New `tokenest.Tools`, and `compactionGuard.requestOverhead` now covers whichever path is live.

*The residual half.* `tokenest.Calibrator` learns a multiplicative correction from the counts the
provider reports every turn. The correction is `(raw + overhead) * scale` — factoring the known
additive part out first keeps `scale` near 1.0 and about the *heuristic's* accuracy, instead of
folding a fixed cost into a multiplier that would over-correct further the longer a conversation ran.
Three deliberate asymmetries, all pinned by tests: the scale never falls below 1.0 (over-estimating
costs an early compaction; under-estimating costs a silent truncation, and only one of those is
recoverable), it rises faster than it falls, and it **discards window-saturated samples** — a
truncated prompt reports the clamp, which *understates* the true ratio, so learning from it would
shrink the correction exactly on the turns where the undercount is doing damage.

*The seam that was easy to miss.* The engine and the Compactor run **two** gates over the same
messages. A correction applied to only one of them re-creates P41.1 in a new form: the engine calls
`Compact()` believing the conversation is over budget, the summarizer prices the same messages
uncorrected and declines, and the engine reads `changed=false` as "nothing left to compact" — which
is the exact symptom P62.4 was filed about. Hence `engine.CalibratedCompactor`, and a test that
drives a real `compaction.Summarizer` through the band where the two gates disagree.

*Live result (qwen3:14b, 24,576-token window).* The failure P62.4 recorded — the prompt pinned at
~23,7xx for five straight turns while Ollama context-shifted at ~23.7s/turn, with **zero notices of
any kind** at 96.7% of the window — is gone. No turn in either arm now exceeds 18,654 tokens,
compaction fires and announces itself, and the notices' quoted percentages match the provider's real
counts (a turn reported at "~62% full" measured 14,751/24,576 = 60%).

**P62.2 — the gate is kept, and the first measurement was an instrument artifact (CLOSED 2026-08-09).**

P62.2 measured `PreservePrefixCache` at 3m19s against 1m32s and recommended reverting it. That
recommendation was followed. Re-running the same fixture after P62.4 landed inverts the result, twice:

| | gate **on** | gate **off** |
|---|---|---|
| before P62.4 | 3m19s / 128,005ms prefill | 1m32s / 64,958ms |
| after P62.4, run 1 | **1m27s / 54,286ms** | 2m7s / 98,332ms |
| after P62.4, run 2 | **1m16s / 54,288ms** | 2m7s / 98,630ms |

Neither reading was misrecorded. The first was taken on a system whose compaction trigger fired 20-33%
too late, which put **both arms** inside the regime where Ollama context-shifts — and there the prefix
cache is already gone, so the gate has nothing left to protect and its deferral is pure cost. Correct
the estimate and the operating point moves out of that regime, at which point the per-turn trace shows
the gate doing exactly what it was built for: past the trigger, gate-off prunes on *every* turn for a
yield too small to drop back under it (message counts unchanged — 11→11, 13→13, 15→15) and pays a full
~9s prefill each time, while gate-on stays append-only at ~2.5s and takes that hit three times.

**The durable lesson is about method, not about caching: a measurement of an optimization is only as
good as the instrument the rest of the system was running on.** P62.2's arithmetic was never checked
against the possibility that the trigger it depended on was wrong, and the item's own "n=1, re-run
before ripping code out" caution did not help — a second run would have reproduced the same wrong
answer, because the flaw was systematic rather than noisy.

Two fixes travelled with it. `internal/cli/chat.go` derived `PreservePrefixCache` from
`config.LocalBackend` directly and so ignored `compaction.preserve_prefix_cache` entirely: the escape
hatch the daemon honoured did not exist on the CLI path, which is the path phased drives run on and
the one the gate was originally measured against. And **the live tier was silently cacheable** — a
"second run" of the A/B returned byte-identical wall-clock and prefill totals for both arms, which
reads as perfect reproducibility and was Go replaying the first run's verdict. Go's test cache keys on
the binary, arguments and environment, none of which change when the thing under test is a model
server. All four documented live commands in CLAUDE.md now carry `-count=1`.

**P62.5 — the overflow-escalation ladder, driven end to end (CLOSED 2026-08-09).**

P62.3 shipped `OverflowEscalationDirective` + `maxPhaseOverflowResets` with no live evidence they ever
fire, and its unit tests covered each part in isolation: the directive escalates, `freshPhaseConv`
carries one, the budget constant is bounded, the stop notice reads correctly. What none of them
covered is that **reset N actually carries rung N** — and no test had ever called `drive.Run` at all.
A loop that passed `""` on every reset, or froze at rung 1, would have kept every one of those tests
green, and passing `""` on reset is precisely the bug P62.3 was filed to fix.

`TestOverflowLadderClimbsThenStops` drives the real loop with an adapter that overflows and records
what the model is asked on each attempt, then asserts the **sequence**: one initial attempt with no
directive, rungs 1-3 in order each still naming the PENDING file, and a bounded stop attributed to
phase size rather than `--max-turns`. Mutation-checked — reverting the directive to `""`, freezing it
at rung 1, and unbinding the budget each fail it.

That test fakes exactly one link: it hands the drive an error it has *declared* to be an overflow.
`TestLiveWorkflowForcedContextOverflow` was written to cover that link against a real model — and it
is **scaffolding that has not yet fired**, recorded here as such rather than as a result. Attempted
against qwen3:14b at an 8,192-token window (sized from the measured 7,119-token base prompt, since
anything smaller truncates the *prompt* and fails for an unrelated reason), the model did not answer
with one oversized tool call. It answered in text, hit max_tokens, and took the "continue from where
you left off" path, regrowing the context and repeating toward `maxIterations` — a real path, but not
this one, and slow enough that the attempt was abandoned after ~30 minutes with no verdict.

The honest state, therefore: the ladder's *mechanism* is validated deterministically and thoroughly;
its *classification link* — that a real Ollama truncation is recognised as an overflow rather than as
a malformed call — remains covered only by unit tests over recorded server text. Forcing a tool call
structurally (a tool whose schema demands a large argument, rather than a prompt asking for a large
file) is the obvious next attempt, and is noted in the fixture itself so the next person does not
re-derive the failure.

**Last updated:** 2026-08-08 — **P63.9 CLOSED: the fourth and last concern, guard retries, extracted
from `Engine.Run`**, which finishes a four-pass decomposition that took the function from 725 to 497
lines. Before it, the third concern (compaction) was extracted — the pass that corrects the item's own
framing of what made it hard — and before that the second (loop detection), and **P63.11 shipped and the live tier produced its
first real prompt-profile measurement**. Earlier the same day: Tier 2 emptied of buildable work and
the first `Engine.Run` pass landed — P63.8 and P62.1 shipped, and P63.9's first concern (the run
budgets) was extracted. Write-ups immediately below; the 2026-08-07 P63.x batch follows them.

**P62.2's adversarial fixture — built 2026-08-08, and what building it cost to get right.**
`TestLiveWorkflowCompactionPrefixCacheGate` (`internal/eval`, `live_workflow` tag) runs one
forced-compaction workload twice, changing only `compaction.preserve_prefix_cache`, and reports wall
clock, per-turn prompt size and prefill, and the count of turns whose context shrank. It needed a
small product change to exist at all: `PreservePrefixCache` was derived from `config.LocalBackend()`
with no override, so both arms would have been identical. `compaction.preserve_prefix_cache` is now a
tri-state (`nil` = auto-detect, behavior unchanged) — independently justified, since P62.2 says the
gate may need tightening or reverting and **an optimization that cannot be switched off cannot be
A/B'd or reverted without a rebuild**, which is the corner it shipped into.

Three runs were needed, and each failure was a different kind of instrument defect worth recording:

- **Run 1 passed and measured nothing — because of its own guard.** The subtest asserts "compaction
  actually ran" precisely to avoid a vacuous green, and matched on the substring `"context ~"`. The
  *failure* notice — `context ~129% full and nothing left to compact` — contains it. A guard that can
  be satisfied by the failure it guards against is not a guard. It now matches `"compacted"` for
  success and fails explicitly on `"nothing left to compact"`.
- **"Read them one at a time" is not enforceable by asking.** `read_file` declares `CapRead`, so
  `engine.runTools` dispatches the round concurrently; qwen3:14b emitted all 8 reads in a single
  turn, producing a 2-turn run with everything still inside the keepRecent tail. The fixture is now a
  **chain** — each file's last line names the next, in a non-guessable order — so one read per turn is
  a property of the workspace rather than a request. Confirmed working on run 2
  (`data_01 → 09 → 04 → 12`, sequential).
- **The base prompt is 7,119 tokens, and that is the number the fixture has to be sized from.**
  Measured on qwen3:14b: system prompt + tool schemas + repo map, before a single file is read. At
  the 8192-token window run 2 used, that is **87% of the window gone at turn zero** — the model got
  four reads in, ran out of room, and abandoned the chain, and compaction never fired because nothing
  ever accumulated ahead of the tail. Two useful corollaries: `compactionTrigger` is
  `min(85% of window, window − maxTokens − margin)`, so a large completion reserve against a small
  window drags the trigger *below the base prompt* and asks for compaction on turn one; and a
  compaction attempt that finds nothing to do is **silent** unless the estimate is also over 95%, so
  "no notice" does not mean "no attempt".

Run 2 did produce a clean prefix-cache signature worth keeping even though the run failed its
assertions: turn 0 prefilled in 6,050ms (cold), turns 1-2 in ~500ms (cache hits on an append-only
conversation), turns 3-4 in ~1,030ms. That is the mechanism P62.2 is arguing about, visible.

**P63.12 — a flag that made a claim about the transcript, and the transcript could disagree
(FILED AND SHIPPED 2026-08-08).** Filed by P63.9's pass 4 and built the same day, but only after
checking its own premise — which narrowed it twice, and the narrowing is the useful part.

The filing blamed "compaction rewrites the middle of `conv.Messages`" in general. Measuring found
that **pruning is not a vector at all**: `pruneStaleToolResults` keys on tool_use/tool_result blocks
and never touches a plain user text message, so only *summarization* can delete a corrective — which
it does wholesale, replacing everything ahead of the keep-recent tail (default 8).

Second narrowing: of the six nudge families, **exactly one is harmed**. The counts —
`guardRetries`, `loopNudges`, `zeroToolNudges`, `emptyAnswerNudges`, `shimFormatNudges` — record what
*this run injected*, which stays true no matter what happens to the transcript afterwards, and they
gate retry budgets rather than visibility. `toolFailureOutstanding` was the only field asserting
something about **the transcript's current contents**, and the only one whose falsity suppresses
re-injection of a message whose entire purpose is to be seen by the model. So the fix is one field,
not a redesign of `nudgeState`, and the item's own "should nudgeState track message identity?"
framing was bigger than the defect.

**What makes it reachable rather than theoretical** is a threshold asymmetry neither the filing nor
the original P52.3 work names: `shouldNudge` fires on `allErrorRounds >= 3` **or**
`sameErrorRounds >= 3`, while `shouldAbort` fires only on `allErrorRounds >= 6`. A model whose rounds
are partly succeeding therefore nudges at three and **never aborts** — it runs to the iteration cap,
which is far more than the eight messages of keep-recent tail. That is the P38.1 drive's shape
exactly: a long local-model run, a small window, and a stall the model keeps half-recovering from.

The fix deletes the flag and asks `hasNudge(conv, toolFailureNudgePrefix)`. That is not a new idea in
this file — `retractGuardCorrectives` already documents the rule ("identified by content rather than
by indices recorded at retry time so a compaction or prepare-step rewrite mid-run can't shift the
bookkeeping onto the wrong messages"). Retraction followed it; the injection gate did not. Linear in
conversation length, once per tool round, on conversations that by definition are long enough to have
been compacted — not a cost worth trading correctness for.

Regression: `TestP6312ToolFailureNudgeReinjectedAfterCompactionDeletesIt`, driving a compactor that
deletes exactly the nudge so the deletion is isolated from everything else a summarization pass does.
It asserts the fixture actually reproduced the condition, so it fails loudly rather than vacuously if
the mechanism moves. Four mutations, all caught, including restoring the old once-per-run semantics.

**A flaky test was found and fixed on the way** (`internal/toolcallprobe`,
`TestGateWarningCarriesTheRate`). It asserted `w2 != w` — that the *first* warning is not yet refined
with the conformance sample — which is a race, not a contract: `Warning` renders the rate whenever
`Trials > 1`, and the background refinement the same call starts can land between its own `Verdict`
call and the `Conformance` read a few lines below. It failed roughly one full-suite run in ten while
the product behaved correctly either way (an earlier sample is a better warning, not a worse one).
The substring check it sat next to already tested the real contract. This matters beyond one test:
these passes are gated on mutation checks, and **in a non-deterministic suite a surviving mutation
and a flake are indistinguishable.**

**P63.9 pass 4 and closure — guard retries, and the state shape the other three passes did not cover
(2026-08-08).** `Run` went from 551 to **497 lines**, closing the item. Totals across the four passes:
**725 → 497 lines (-31%)**, max nesting **10 → 6** levels, and the `// Pxx` marker count inside the
function **29 → 21** — that last one being the metric the item actually cared about, since those
markers were its evidence that behavior was being added to `Run` *because* that is where all the state
already was.

**The catalogue of state shapes is the item's durable output**, more than the line count. Each pass
asked one question — *what state does this concern actually own* — and each got a different kind of
answer: pass 1 found state **owned by nothing** (a bare `runStart` hand-threaded into five call
sites); pass 2 found **per-turn state at run scope**; pass 3 found **run-scoped state with the wrong
owner**; pass 4 found **inter-turn carry**.

That fourth shape is the one the others do not cover. `constrainNext` lived for exactly one iteration
boundary: the guard set it at the *end* of turn N, the turn setup consumed it at the *start* of turn
N+1, and it had to be cleared in between or the P59.8 schema would silently re-shape every later turn
of the run — with tools suppressed, since a non-nil format also means "tools off". Nothing enforced
any of that. It was declared among `Run`'s run-scoped variables, read at one site to decide
`suppressTools`, read again ~200 lines later and manually set back to nil, with a comment at *each*
site explaining the discipline the code did not encode. `guardGate.takeFormat` returns the carry and
empties it in the same expression, so a caller cannot forget the second half because there is no
second half. The generalizable form: **when a comment explains a discipline, look for the API that
makes the discipline unnecessary.**

`guardGate` (`internal/engine/guardretry.go`) also takes the verdict handling, the bounded corrective,
the corrective text builder, and the P27.16 rollback-on-exhausted-retries. The retry **count** stays
in `nudgeState` and is passed in — the same boundary passes 2 and 3 held, because `retractAll` reads
that table. A nil `*guardGate` is a run with no output guard. Two constants moved to the concerns that
own them: `guardCorrectivePrefix` here (matching `loopNudgePrefix` in `loopdetect.go`) and
`summarizerGiveUpThreshold` to `compact.go`, which also fixes a pre-existing mangle where
`guardCorrectivePrefix`'s doc comment was attached to `summarizerGiveUpThreshold`.

**Seven mutations, two survived, and both survivors were assertions the suite believed it already
made.** `TestSchemaGuardFormatIsPerRetry` states in its own doc comment that "a turn that is not a
guard retry never carries it" — and never constructs such a turn, so deleting the clear from
`takeFormat` failed nothing. Building that case needs a turn following a retry without being one; an
empty retry supplies it, because the P34.1 empty-answer nudge fires before the guard is reached. The
second survivor: nothing asserted the `final == ""` gate, so the guard would happily judge an empty
answer, spend a retry on a turn already corrected once, and emit a `KindGuard` verdict about text that
does not exist. Both now covered; all seven mutations fail the suite.

**Gated without the live tier, deliberately** (the machine was needed for other work): `go build`,
`go vet`, `go test ./...`, `go test -race` over engine/eval/server/drive/swarm, and the eval golden
transcripts. The live tier's value for this concern is in any case limited — pass 3's run established
that a green there is evidence the surrounding loop works, not that the extracted concern does.

**P63.12 was filed by this pass** — compaction can summarize away a corrective that `nudgeState` still
counts as present, and guard correctives are now the second consumer of that
marker-in-the-transcript assumption. Two consumers is what makes it worth fixing once rather than
noting in two files. The `toolFailureOutstanding` case is the one with teeth: it stays latched, so the
P52.3 corrective is suppressed for the rest of the run with no notice and a healthy-looking counter.

**P63.9 pass 3 — compaction, and the difference between mutating shared data and sharing state
(2026-08-08).** `Run` went from 654 to **551 lines**, the largest single drop of the three passes so
far, and the finding is a correction to P63.9's own framing.

The item filed compaction as half of "the hard pair" on a specific ground: pass 2's technique — name
the per-turn state and return it as a value rather than storing it — **cannot** encapsulate a concern
that mutates `conv` mid-run. That is true, and it turned out not to be the obstacle it reads as.
Mutating shared data is not the same as sharing state. Every write compaction makes to
`conv.Messages` is its own output, not a variable another concern also writes, so there was nothing
to return as a value because nothing was escaping in the first place: `pct` and `compacted` were
already block-scoped, and the five variables sitting in `Run`'s preamble all genuinely live for the
whole run.

**So the defect was ownership, not scope** — the opposite of pass 2's finding, and worth recording as
such, because the two passes now bracket the question P63.9 exists to ask. Pass 2 found per-turn
state at run scope. Pass 3 found run-scoped state at the wrong *owner*: the shim's prompt cost, the
consecutive-failure count, the cumulative LLM-failure count, the summarizer latch and the
context-full warned flag were declared where 600 lines could reach them and touched by one 70-line
block. Naming the owner is the whole fix; the `conv` rewriting travels with it as a method parameter
because the rewriting **is** the concern.

`compactionGuard` (`internal/engine/compact.go`) therefore owns all five, plus both call sites — the
unconditional run-entry pass and the per-turn headroom gate — and `Run` is left with two calls and a
comment. Unlike `loopGuard` there is no nil form: the 95%-full notice is emitted by exactly this
concern and fires whether or not anything can be compacted, which is the local-server
silent-truncation case it exists for, so the nil check is on the compactor field, inside.

**One coupling is real and was deliberately left alone.** Compaction rewrites the middle of
`conv.Messages`; nudge retraction finds its correctives by scanning that same list for a marker
prefix. A pass that summarizes away an outstanding nudge leaves retraction a no-op while `nudgeState`
still believes the corrective is in the transcript — `toolFailureOutstanding` in particular stays
latched, suppressing re-injection for the rest of the run. It is benign today (worst case: one
corrective not re-sent) and closing it is a behavior change, which this pass is gated against. It is
documented at the seam because it is the coupling to think about before the **guard-retry pass**, the
one remaining concern, which keys on the same marker-in-the-transcript mechanism.

A second asymmetry is now written down rather than merely true: the run-entry pass shares the
compactor with the proactive path but **none** of its failure bookkeeping, so a run whose entry pass
fails still pays `summarizerGiveUpThreshold` further LLM calls before the P39.8 latch fires.
Preserved, not fixed, for the same reason.

**The mutation checks found a coverage gap, and it was pre-existing.** Six mutations were run against
the extracted code; **three survived** the suite as it stood. The sharpest is that
`TestProactiveCompactionFallsBackAfterTwoFailures` — named for the threshold — does not detect
changing that threshold from two to three, because its three-turn script cannot tell the two apart;
nor did anything detect deleting the counter's reset, or dropping the shim catalog from the headroom
estimate entirely. Two new tests close all three by asserting the *sequence* of compactor calls
rather than a count (`compact, compact, compact, fallback, compact` pins the entry pass being outside
the bookkeeping, the fallback firing on the second consecutive failure, and the reset after it) and
by running one fixture twice with the shim as the only difference. All six mutations now fail the
suite, including deleting the run-entry pass outright.

Gated on `go build`, `go vet`, `go test ./...`, `go test -race` over engine/eval/server/swarm/drive,
and the eval golden transcripts. Live-tier result recorded with the pass in roadmap.md.

**A third finding about the live tier itself, in the same family as the two the pass-1 and pass-2 runs
produced: `TestLiveWorkflow` was structurally blind to every engine notice.** `drainWorkflowEvents`
switched on `KindText`/`KindToolCall`/`KindToolResult`/`KindApprovalRequest`/`KindError`/`KindDone`
and let `api.KindNotice` fall through unhandled, while the daemon logger the same test builds is
pinned at `LevelWarn` — so compaction, the context-full warning, loop-detector nudges, tool-failure
correctives and shim warnings were invisible on both channels at once. This surfaced while trying to
attribute this pass's `FixSeededBug` failure: the log could not answer *did compaction even run*,
which is the first question to ask of a compaction pass, and the absence of compaction lines looked
like evidence when it was only silence. Notices are now logged in the timeline and collected on
`workflowSummary.notices`. The general shape is the one the fixture-rot finding already made:
**a tier that cannot observe the subsystem under test reports every result as being about something
else.**

**P63.9 pass 2 — loop detection, and the state that was at the wrong scope (2026-08-08).** `Run` went
from 685 to **654 lines**, but the line count is again the least of it. `loopDetector` has had its own
file and its own type since P53.2, so this pass looked like it should be a move; the actual finding
was that **two of the concern's variables were declared at the wrong scope**:

- `loopNudgePending` sat among `Run`'s run-scoped flags, carrying the recoverable-loop corrective from
  the gate (which fires *before* a tool round) to just after that round.
- `loopRecorded` was re-declared each iteration, saying whether this turn entered the detector's
  window and therefore whether its results are the outcome that classifies a future cycle.

Both are set by the gate and consumed after that same turn's tool round, and **no path between them
continues the loop** — the only exits are terminal. So both are per-turn state living where a
685-line function could reach them, which is precisely the mechanism by which `Run` accumulates
interactions it does not really have.

The extraction follows from that: `loopGuard` owns what genuinely survives a turn (the detector's
window and outcomes, the threshold the messages quote, the poll-exemption predicate), and `check`
returns a **`loopVerdict` value** the caller holds for exactly one iteration. Per-turn state can no
longer outlive the turn, because it is a value rather than a variable. The gate in `Run` collapsed
from a 22-line nested block to four lines of decision handling.

Two boundaries were deliberately *not* crossed. The loop-nudge **count** stays in `nudgeState` and is
passed in, mirroring the split already in the tree for the sibling concern — `toolFailureTracker` owns
the failure counters while `nudgeState` owns `toolFailureNudges`/`toolFailureOutstanding` — because
`retractAll` reads that table and duplicating a row invites the two copies to disagree. And a disabled
detector is a **nil `*loopGuard`** whose methods tolerate a nil receiver, so `Run` lost its
`if loop != nil` checks rather than trading them for a different conditional.

Gated on `go test ./...`, `go test -race`, the eval golden transcripts, and the **full live tier**
(qwen3:14b — all three subtests passed). `loopguard_test.go` adds six assertions at the new seam,
including the two the extraction makes statable: that the triggering turn is *not* recorded (its
window was just reset, so an outcome would misattribute), and that a nil guard is inert. Both were
**mutation-checked** rather than assumed — removing the `recorded` guard from `noteOutcome` fails the
new unit test, and removing the spent-corrective condition from `check` fails the existing end-to-end
`TestEngineAbortsOnSecondSucceedingLoop`.

One note against over-reading the live run: `FixSeededBug` passed this time having failed over pass 1,
and that is **not** attributable to this change. P60.4's control group had already pinned the earlier
failure on the model, and this run's model simply ran the script, made the correct
`int(row["temp"])` edit and re-ran it. What the green does establish is that the tier can produce one,
so a future red over a P63.9 pass carries information.

**P63.11 — a live subtest that could not pass, and blamed the wrong thing when it didn't
(SHIPPED 2026-08-08).** `TestLiveWorkflow/LocalPromptProfileReducesFirstTurnTokens` asserts the
`local` prompt profile (P25.6) produces fewer first-turn input tokens than `default`, measured
through the model's reported `prompt_eval_count`. On qwen3:14b at Ollama's default 4096 window both
profiles reported exactly **4095** — `num_ctx - 1` — and the subtest failed saying the local profile
"did not reduce first-turn input tokens". P25.6 was working the whole time; the *instrument* had
saturated, because both prompts exceeded the window and the server reports the clamp for each.

Fixed by making the measurement possible and the failure honest, and the item's two options turned
out not to be alternatives at all:

- **Pin the window.** The test daemon set only `MaxTokens: 4096` and never a `context_window`, so it
  inherited whatever Ollama happened to be serving. It now pins `promptProfileNumCtx` (16384) — via a
  new `newLiveWorkflowDaemonWithWindow`, so the two workflow subtests keep an auto-detected window and
  do not pay VRAM for a pin they don't need.
- **The pin alone does not work, which only running it showed.** With the model already resident at
  4096, `applyDetectedWindowFor` correctly refuses to promise more than Ollama is *currently serving*
  — "configured context_window exceeds what Ollama is serving; using the served value ...
  configured=16384 served=4096" — and the measurement saturated exactly as before. So the subtest now
  unloads the model first (`keep_alive: 0`), making detection non-authoritative so config wins and the
  model reloads at the requested window.
- **Eviction is not complete when the unload response returns.** A daemon built ~100ms later still
  detected the old instance and pinned itself to the window that was supposed to be gone. The helper
  polls `/api/ps` — the same signal `internal/ollamainfo` treats as authoritative — until the model
  actually stops being listed, rather than sleeping a guessed interval.
- **Saturation is now named as saturation.** `saturationReason` reports a count sitting at the served
  window (Ollama reports `num_ctx-1`), or two identical counts with no window known, and the subtest
  `Skip`s with the served windows and their sources rather than failing with P25.6's name on it. A
  SKIP still reads as "asserted nothing", which is the property that matters.

**The item proposed replacing the live measurement with a byte comparison of `effectiveSystem`;
that assertion already exists** — `TestEffectiveSystem_localProfileTrimsPrompt` covers it in the
default suite, without a model. What only the live path can show is that the smaller prompt actually
*reaches the provider*, so the end-to-end measurement was kept rather than replaced.

`saturationReason` is deliberately in an **untagged** file with its own unit test. The live tier's
guard assertions only run under `-tags live_workflow`, which is how P62.1 silently rotted a live-tier
fixture through a green `go test ./...` two days earlier; the decision logic belongs where the default
suite can cover it even though its caller cannot be.

**Validated live, and it produced the measurement the subtest was always supposed to produce**
(qwen3:14b, 2026-08-08): **local=7039, default=9283 input tokens at a 16384 window sourced from
config** — a real 2,244-token reduction, and the first end-to-end confirmation that P25.6's trim
reaches the provider rather than merely shortening a string. The run also got faster (74s → 33s),
since neither prompt is being truncated any more.

**P62.1 confirmed the roadmap's own warning about itself.** Its write-up said, in bold, that
*selection alone is not sufficient* — at 57.8× budget a perfect ranking still buys the top ~10-20
files of 672. Building only the recommended (B) and (A) would have produced a correctly-ordered map
that still showed 1.5% of the repository, and the item would have read as closed. Ranking and
per-file compression turn out to be answers to different questions: (B)+(A) decide *which* files, (C)
decides *how many*, and only together do they move coverage 10 → 37 files with every
architecture-table package present. This is the third consecutive pass where the filed item was right
about the defect and wrong about the sufficiency of its own recommendation.

**P63.8 — write bookkeeping was gated on the static capability (SHIPPED 2026-08-08).** In
`engine.runTools`, two adjacent branches over the same tool and the same input disagreed about which
capability to ask for: the write-recording branch read `t.Capability()` while the secret-redaction
branch three lines below read `tool.EffectiveCapability(t, tu.Input)` (P25.4c). The first is the
pattern P32.2 removed from `ContextualGate` and P63.3 removed from `ScopeGate`; this was its last
instance in the tree, found by the sweep P63.3 asked for.

The consequence was never a gate bypass — scope and the permission stack still bind — it was a
**coverage hole in the write bookkeeping**. A tool reclassifying into `CapWrite` for a specific call
via `CapabilityFor` had its written paths go unrecorded, so that call got no output-guard file
validation and no quarantine-on-fail rollback: precisely the silent degradation the P32.6 warning
three lines above exists to make loud, arrived at by a different route.

Fixed by reading the effective capability at the call site. The change is **not purely additive**,
which is why the item was split out of P63.3 rather than folded in: the branch was previously skipped
for `shell` on its static `CapExecute`, and the effective capability makes it a per-call decision for
that tool too. `TestExecuteToolRecordsWrittenPathsByEffectiveCapability` covers all five shapes — a
tool widening into `CapWrite` (the defect), the same tool on a read call, a statically-`CapWrite` tool
narrowing out (the behavior change), the same tool on a write call, and the `CapExecute`→`CapRead`
narrowing `shell` actually performs today, which is unaffected because neither capability is
`CapWrite`. Still not reachable in production — `shell` remains the only `CapabilityOverrider` in the
tree and it only narrows — which is why this stayed Tier 2 rather than being re-filed as urgent.

Writing the test surfaced one adjacent defect: `recordWrittenPaths` wrote to a map that only `Run`
initializes, so a tool call outside a run panicked on a nil map write. Unreachable in production, but
a nil-map panic on the engine's tool path is the P63.1 failure class, so it got a lazy init.

**P62.1 — the repo map's selection policy was the alphabet (SHIPPED 2026-08-08).** `repomap.Build`
ended in `sort.Slice(m.Files, ... Path < Path)` and `Render` walked that order and **broke** at the
first file that didn't fit, so which files reached the model was decided by filename and the cutoff
was a hard wall — a one-symbol file later in the sort could never fit even with spare bytes. Measured
on this repo before the fix: **10 of 672 files** survived into the rendered map, 4 of them test files,
and every package in CLAUDE.md's architecture table (`engine`, `provider`, `tool`, `server`,
`config`) was invisible while `internal/api` got in on the letter "a".

All three buildable options shipped together, plus the configurability the item flagged separately:

- **(B) demote test files.** `isTestPath` is a cross-language name heuristic — Go's `_test.go`,
  Python's `test_*.py`/`*_test.py`, JS/TS's `*.test.*`/`*.spec.*`, Ruby's `*_spec.rb`, and any file
  under a conventionally-named test directory. It needs no theory of "important" beyond
  production-before-test, and it addresses 351 of 696 files. A false positive costs a production file
  some rank, never its presence, since demotion only reorders.
- **(A) rank by import in-degree**, reusing the edges P49.1 already computes — no new extraction pass.
  `packageInDegree` counts only edges resolving to a package directory the map contains (bare
  third-party and stdlib tokens carry no signal about *this* repository) and counts distinct importing
  *packages*, so a package of many small files can't out-vote one of few large ones. Because edges
  resolve to directories, in-degree ranks packages and needs a within-package tiebreak: symbol count,
  then path — without it the alphabet would silently decide the order again inside each package.
- **(C) per-file symbol cap, and `continue` instead of `break`.** `DefaultMaxSymbolsPerFile = 3`.
  A file whose list is cut carries an explicit `… +N more` marker, because a silently shortened list
  is a worse failure than a shallow one — the model can only conclude the missing symbols do not
  exist. The `break` → `continue` change lets a small file fill budget a large one couldn't use.
- **The budget is now configurable**: `repomap.max_bytes` and `repomap.max_symbols_per_file` (plus
  `AEGIS_REPOMAP_*` and `aegis index --max-bytes/--max-symbols-per-file`, which override config only
  when actually passed, so `--max-symbols-per-file=-1` for "uncapped" isn't read as absent). The 8000
  default was calibrated as a ~2k-token slice of a small window; an operator on a 128k-context model
  could not previously spend 1% of it on a better map.

Measured after, same repo: **37 of 696 files**, **zero** of them test files, and the top of the map is
`internal/provider`, `internal/sandbox`, `internal/tool`, `internal/config` — the set the architecture
documentation also names. `schemaVersion` went 2 → 3 since file ordering and per-file caps change the
cached render without changing what `Build` extracts.

Making the budget configurable introduced a footgun that had to be closed in the same change: neither
knob affects extraction, so a cache whose fingerprint still matched would be reused and the operator
would see no effect from raising `max_bytes` until some unrelated source file happened to change.
Both knobs are now mixed into the fingerprint (`TestRenderOptionsInvalidateTheCache`).

**(D) query-relevant selection reusing `memory.LoadRelevant` stays blocked** on a per-turn or
two-stage map, unchanged. One observation for whoever takes it: in-degree ranking is per-*package*,
so the rendered map now leads with seven consecutive `internal/provider` files and nine
`internal/sandbox` ones. Depth-within-package may be the wrong trade against breadth-across-packages,
but that is a further selection question and was deliberately not answered here.

**P63.9 — first concern extracted from `Engine.Run`: the run budgets (PARTIAL, 2026-08-08; item
stays open).** P63.9 asks for one concern at a time, each naming the state it actually owns, each
landing separately — explicitly *not* one sweep, because a sweep produces a diff no reviewer can check
against a function whose whole problem is that its parts interact.

Budgets went first because the ownership question has an unusually clean answer. Three of the four
bounds (`budgetUSD`, the two token caps) are pure reads of the cost tracker and own nothing. The
fourth owned exactly one thing — the run's start instant, previously a bare local named `runStart`
threaded by hand into five calls across 600 lines. Once that is a field on a `runBudget` struct
(`internal/engine/budget.go`), the concern has nothing left in `Run` to interact with.

`Engine.wallClockExceeded`, `tokenBudgetExceeded`, `wallClockOverride` and `clock` moved off `Engine`
onto `runBudget`; the two gates collapsed from three duplicated inline checks each into one
`budget.exceeded()` call, which is the property the inline form kept losing. Check order is
observable and preserved: cost, then tokens, then time. `Run` is 725 → 685 lines — a small number that
undersells the change, since the point was removing the hand-threaded `runStart`, not the line count.

**Three concerns remain** — compaction, loop detection, guard retries — and they are the harder ones:
compaction and guard retries interact with each other and with the retry path, so neither has the
"owns exactly one field" shape that made this pass safe. The item stays Tier 3 and stays worth not
doing in a hurry.

**Live-tier validation (2026-08-08), including two findings about the tier itself.** P63.9 argues the
live tiers matter more here than the unit suite, since `internal/engine` at 92.0% coverage is exactly
the profile that hides an integration-shaped break — so `TestLiveWorkflow` ran over this pass on
qwen3:14b (the recommended qwen3.6:35b-a3b-deep is not pulled on this box). `GuardNoMetaLeak` passed.
`FixSeededBug` failed, and **P60.4's control group did the job it was built for**: `claude -p` on the
same task failed too, in 27s against Aegis's 54s, so the verdict is *model*, not scaffolding. Worth
recording that this **refuted the objection raised before the run** — that a cloud model in the
baseline arm would trivially pass and mislabel any Aegis failure as ours. It did not pass. The
Aegis-side behavior was byte-identical across two independent runs: the model issues `del /F /Q`, a
cmd.exe builtin the shell tool does not have, and never attempts an edit. That is a model failure
signature, not a budget-gate one.

Two things about the tier came out of the run and are worth more than the pass/fail:

- **`gpt-oss:20b` cannot be used as an instrument on 16GB VRAM / 16GB RAM.** All three subtests hit
  their context timeouts with **0 tool calls and 0 tokens** — and `GuardNoMetaLeak` "passed"
  *vacuously*, because a run that emits nothing leaks nothing. A green subtest inside a timed-out run
  is not evidence.
- **P62.1 silently invalidated a live-tier fixture, and only running the tier found it.** The
  per-file symbol cap shrank `writeBigRepoMapFixture`'s rendered map from over `bigRepoMapCapBytes`
  (4000) to 2154, which would have made `LocalPromptProfileReducesFirstTurnTokens` compare two
  identical prompts and assert nothing. Its own guard caught it — but that guard only builds under
  `-tags live_workflow`, so `go test ./...` could never have seen it. **A change to rendered prompt
  content can rot a live-tier fixture with the default suite fully green.** The fixture now sizes
  itself in files rather than functions-per-file, grows until it actually clears the threshold instead
  of trusting a hand-computed count, and asserts the map is un-truncated as well as large enough —
  the property the byte comparison alone cannot see.

---

**Last updated:** 2026-08-07 — **the whole P63.x batch built in one session**: P63.1-P63.7 shipped
the day after the review filed them, emptying Tier 1 and clearing every Tier-2 item the review
produced. Three items were filed *by the build* rather than by the review — **P63.8** (the last
instance of the static-capability pattern, found by the sweep P63.3 asked for), **P63.9**
(`Engine.Run`, split out of P63.7 because it needs a design answer rather than code motion) and
**P63.10** (two TUI asymmetries seen while moving every `Update` case). Write-ups immediately below;
the preceding 2026-08-06 Tier-4 assessment pass follows them.

**What the batch taught, aside from the fixes.** Three of the seven were larger or differently-shaped
than filed, and in each case the item's own text is what exposed the mismatch:

- **P63.4 was filed as "two lines, no design question"** and was neither — measuring the driver
  showed the prescribed mechanism does not work, the item named two stores when three had the
  omission, and the one file it praised for getting this right was the file most in need of the fix.
- **P63.5's literal instruction would have produced the failure the same item warned against** —
  "call `recordInvalidAuthAttempt` on each failure branch" also arms the lockout, which is the
  self-DoS its own write-up ruled out of scope. It needed a split, not a call.
- **P63.7 was two items under one heading**, and only became visible as such once its safe half was
  built and the unsafe half had nothing left to hide behind.

The generalization is the same one the 2026-08-06 assessment pass reached from the other direction:
a filed item is usually right about the defect and unreliable about the size. Both passes now argue
for measuring first, and this one adds that the measurement is often cheapest *during* the build.

**P63.1 — a sub-agent panic killed the daemon; the identical top-level path survived (SHIPPED
2026-08-07).** `engine.runTools` already recovered a panicking tool call and already stated why:
unrecovered, it crosses the goroutine boundary and takes down the whole daemon process, every
concurrent session and not just the one that triggered it. That reasoning had never been applied to
the goroutine hosting an *entire engine run*. `InProcessBackend.Spawn` launched a teammate with a
bare `wg.Go` calling `Server.subAgentRunner` and thence `engine.Run`, with no `recover` on the path.

The blast radius was asymmetric, which is what made it easy to miss: a top-level run is hosted by an
HTTP handler and gets `net/http`'s recover, so a panic kills one connection; a teammate got nothing,
so the same panic killed the process. The trigger was any panic in `Run` **outside** `runTools` —
compaction, the output guard, an adapter, loop detection — i.e. exactly the surfaces `runTools`' own
recover does not cover. Measured at filing: 3 `recover()` sites against 19 goroutine launch sites,
with the swarm backends holding none.

`runGuarded` wraps `b.run` with a `defer`/`recover` that logs the panic and its stack and converts it
into a named-return error, so it flows down the failure route `Spawn`'s closure already used for an
ordinary `err`: `StatusFailed` in the registry plus a mailbox `MsgResult` carrying the error. No
second failure path was added — that reuse is the whole point, and it restores parity with the
tool-call recover one layer down. The stack goes to the log only: `res.Err` feeds an 80-char registry
summary and the parent's mailbox, where a full stack is noise. `SubprocessBackend` is unaffected by
construction; a panicking child is a process exit there, already handled.

**P63.2 — the Go vulnerability scanner could not analyze the module (SHIPPED 2026-08-07).** `go.mod`
pinned `go 1.26.4` — a **patch-level** `go` directive with no `toolchain` line. `govulncheck`
(x/vuln v1.6.0) declares `go >= 1.25.0`, so the go command built it with go1.25.12, and a 1.25-built
analyzer refuses the whole module: *"package requires newer Go version go1.26 (application built with
go1.25)"*, repeated for ~30 packages, exit 1. For a tool whose own remit is security scanning, the
supply-chain check was silently inoperative.

The pin is now `go 1.26` plus `toolchain go1.26.5`, which expresses the same intent without making
every third-party analyzer that declares a lower floor unable to load the module. **The patch-level
directive is the defect, not the version** — it would have broken the next analyzer the same way.

Forcing the analyzer's own toolchain surfaced two advisories with call-path evidence, both closed
here: **GO-2026-5320** (goldmark v1.7.13 → v1.8.5, indirect via `charm.land/glamour/v2`, reachable
from `cli/chat_render.go`) and **GO-2026-5856** (stdlib `crypto/tls` ECH privacy leak, go1.26.4 →
go1.26.5, reachable from `server.go`, both provider adapters and `security/method.go`).

**Neither is meaningfully exploitable here, and the item was filed knowing that**: goldmark's is an
HTML-XSS in a renderer whose output goes to a terminal, and the ECH leak needs Encrypted Client Hello
in play. The Tier-1 case rested on the **blindness** — an inoperative scanner reports nothing about
the advisory that *does* matter, and reports it just as quietly. The goldmark path is still worth
noting on its own: `chat_render.go` renders model output, the same prompt-injection-carrying text the
web UI deliberately routes through DOMPurify, so one path was rigorously sanitized while the other
fed a parser with a known advisory.

The CI job went into `codeql.yml` rather than `ci.yml`, because `ci.yml`'s `push`/`pull_request`
triggers are commented out — a supply-chain check wired there would only run when someone remembered
to dispatch it, which is the same blindness in a new shape. It reads `go-version-file: go.mod` so the
analyzer is built with the module's own toolchain, i.e. against the exact failure mode above.
`govulncheck ./...` now reports no vulnerabilities found.

**P63.3 — `ScopeGate` re-introduced the static-capability pattern P32.2 removed (SHIPPED
2026-08-07).** `ScopeGate.Check` tested `t.Capability()`. Every other gate tests
`tool.EffectiveCapability(t, input)`, and not incidentally: **P32.2 was a shipped Tier-1 item** that
changed `ContextualGate.Check` for exactly this reason. `ScopeGate` shipped later, as P46.1, and
reintroduced the pattern — in the gate that binds hardest of the five.

Not exploitable, and the reason is recorded so it is not re-filed as urgent: `shell` is the only
`tool.CapabilityOverrider` in the tree and only *narrows* `CapExecute` → `CapRead`, so no tool can
reach `CapWrite` through `CapabilityFor` and task-scope containment was intact throughout. The gap
was that *"no override ever widens into `CapWrite`"* was an unwritten, untested invariant holding up
a security boundary — the same shape of assumption P32.2 declined to keep one release earlier.

`TestScopeGateUsesEffectiveCapability` mirrors `TestNetworkAllowListUsesEffectiveCapability`: a tool
with static `CapRead` that widens to `CapWrite`, asserted scope-blocked outside the scope and still
allowed inside it, so the widening restricts rather than blanket-denies. It fails against the pre-fix
code, which is what made this worth landing rather than commenting.

The tree-wide sweep the item asked for is the part worth keeping. It found three categories, not one:
sites where the static capability is **deliberate and tested** (`rules.go` — rule matching keys off
the tool's static shape so a `deny shell(...)` rule cannot be dodged by a call that looks read-only,
recorded in `TestRuleGateDenyStillBlocksReadClassifiedShellCall`; converting those would be the
opposite bug), sites where it is **structurally forced** (`isNetworkCapable` is a by-name post-hoc
lookup for the egress latch), and exactly one genuine remaining instance, filed as **P63.8**.

**P63.4 — every SQLite store now has a `busy_timeout`, set on the DSN (SHIPPED 2026-08-07).**
`PRAGMA busy_timeout` was set in exactly one place — `cli/worker.go`, at 5000ms. The long-lived
stores did not set it: `session`, `longmem` and **`knowledge`** (which the filed item did not name)
each paired `SetMaxOpenConns(1)` with `PRAGMA journal_mode=WAL` and stopped.

`SetMaxOpenConns(1)` serializes writers *within* one process, which is why this was invisible in
ordinary single-daemon use and why the suite never saw it. It bites *across* processes — an
`aegis chat` CLI against a session DB while `aegis serve` holds it — where SQLite's default is to
fail a contended write with `SQLITE_BUSY` **immediately** rather than wait. WAL widens the window in
which that is survivable, so the missing timeout gave back the reader/writer concurrency WAL buys.

**The item was filed as "two lines, no design question" and the measurement contradicted it.**
Against `modernc.org/sqlite` v1.54.0, the driver in use: the default `busy_timeout` is 0, confirming
the premise — and a `db.Exec("PRAGMA busy_timeout=5000")` **does not survive connection churn**, with
fresh connections reading back 0. Unlike `journal_mode=WAL`, `busy_timeout` is per-connection state
and is *not* persisted in the database file, so an `Exec` configures only whichever pooled connection
served it; `SetMaxOpenConns(1)` caps concurrency but does not pin connection *identity*, and the pool
reopens on idle close or connection error. The driver's `applyQueryParams` runs `_pragma=` params on
every connection it opens, so the DSN form is the only reliable one.

That inverts the item's own framing: `cli/worker.go`, cited as the one place written with
cross-process contention in mind, was the site using the mechanism that does not work. It was
converted rather than copied. `checkpoint`, `task` and `cron` open no database — they take a
`*sql.DB` from the caller, in production the session store's — and inherit the fix; a sweep found
exactly four non-test `sql.Open` sites with a sqlite driver, all now covered.

The tests set `SetMaxIdleConns(0)` to force a fresh connection per query and assert the pragma reads
back across several of them; **the churn is the load-bearing part**, since it is precisely what an
`Exec`-configured store fails. They also assert `journal_mode` still reads `wal`, proving the
appended query string did not mangle a Windows path. No cross-process contention test was written:
parking a write lock long enough to observe the wait needs a second process and a timing window,
which is a flake source rather than a regression guard.

**P63.5 — `POST /auth/exchange` is in the audit trail, and deliberately still outside the lockout
(SHIPPED 2026-08-07).** `authMiddleware` exempts `/auth/exchange` — necessarily, since the frontend
holds only a page token at that point and has no daemon token to present. The consequence was that
page-token failures never reached `recordInvalidAuthAttempt`, leaving that endpoint with neither the
FIND-11 logging cadence nor the P27.12/FIND-14 lockout every other authenticated route carries.

**The brute-force angle was never the concern**: page tokens are 256 bits from `crypto/rand` with a
60s TTL, single-use, deleted on redemption whether or not the exchange succeeds, and bound to a
double-submit CSRF nonce. The gap was **observability**, inverted from where it would be useful — a
process probing `/sessions` produced a `slog.Warn` with a remote address and a cumulative count,
while the same process probing the one auth endpoint a browser can actually reach produced no log
line at all. FIND-01 already names a local process driving this flow as accepted residual risk;
accepted risk should still be observable.

**Following the item's literal instruction would have produced the failure the same item warned
against.** `recordInvalidAuthAttempt` did two jobs — the counter and the log line, *then*
`registerAuthFailure`. Calling it from `handleAuthExchange` would have armed the lockout from the one
endpoint a browser must reach with no daemon token in hand, letting any local process wedge the
operator's own UI out of loading. So it was split: `logInvalidAuthAttempt` is the logging half, and
`recordInvalidAuthAttempt` is that plus `registerAuthFailure`. The exchange handler calls the logging
half on each of its four rejection branches — page token missing, CSRF cookie missing, CSRF header
missing or mismatched, page token invalid/expired/already-redeemed. Whether the lockout should ever
engage here is a real question, deliberately left open.

The `invalidAuthAttempts` counter stays shared so the cadence and `cumulative_count` remain coherent
across both routes; it cannot influence the lockout, which lives in separate
`authLockMu`/`authConsecutiveFailures` state. The new `reason` attribute was added to the
middleware's existing lines too — without it all six branches share one message string and are
indistinguishable in the log, which undercuts the point — and is always a fixed handler-side string,
never attacker data. The invalid/expired/already-redeemed branch names all three causes rather than
distinguishing them: `exchangePageToken` returns a bare bool, and a finer split would leak redemption
state into the log for no observability gain.

`TestAuthExchangeFailuresDoNotArmLockout` fires `authLockThreshold*3` failed exchanges and asserts the
streak is still zero, no lock deadline is set, and a subsequent correctly-authenticated request
returns 200 rather than 429. That test is the item, not a supplement to it.

**P63.6 — the permission stack's order is stated once, where it stays correct (SHIPPED 2026-08-07).**
Three consecutive comments in `Server.buildGate` each claimed the outermost position. The real
evaluation order is `Scope → PersonaTool → Rules → Contextual → Mode`, so only the scope one was
right: rules were third and claimed first, persona tools second and claimed first. Each had been
correct when written; none was updated as a layer landed above it. `CLAUDE.md` carried the same rot,
describing `PersonaToolGate` as *"wrapped outermost in `server.newEngine`"* — wrong on both counts.

Filed and fixed because of **what the comments were doing**. They did not describe the code, they
justified security ordering — the rules comment argued they are "evaluated before the contextual and
mode gates," which is the kind of statement a future reader reorders against. A wrong ordering claim
on a permission stack is worse than no claim, and the stack had two.

The order now lives in `buildGate`'s doc comment, which already listed the stack and is the one place
that stays correct as layers are added, together with an instruction to extend *that list* rather
than restate a position at each site — the restating is what rotted. Each site now describes only
what its layer does. The scope site keeps its argument, being the one that was right, but reframed
from a position claim to **why it must go on last**: it binds hardest, so anything added after it
would relax a containment the run opted into. That claim survives a reorder; "it is the outermost"
is the one that does not.

**P63.7 — the TUI `Update` switch is split into per-message-domain files (SHIPPED 2026-08-07,
`Engine.Run` half split out as P63.9).** P40.5 decomposed `tui.go` from 4,731 to 2,285 lines and
closed with one sentence: *"The finer per-message-domain split of the `Update` switch is left as
opportunistic follow-up."* The opportunity was not taken and the target grew — `update.go` was 1,344
lines containing exactly one function, `model.Update` at 1,324, up from the 1,249 P40.5 recorded, and
the largest function in the tree by nearly a factor of two.

`update.go` is now **181 lines** and does nothing but dispatch: the modal overlays get first refusal
in the order they stack on screen, then the per-domain handlers own the work, then anything unclaimed
falls through to the composer. Twelve files — overlay, dialog, key, slash, stream, session, shell,
status, clipboard, layout, tick, compose — grouped by what a message *is*, not by line count.

Pure code motion by the P40.5/P40.6 method: nothing renamed, no control flow changed, no bug fixed in
passing, and gated on the eval golden transcripts showing no diff (`tool_round_trip.golden.json`
byte-identical; the suite was never run with `AEGIS_EVAL_UPDATE`, which would have destroyed the
entire safety property). The +330 lines over the original are package blocks, doc comments and
signatures.

Two handler shapes appear, forced by the original control flow rather than chosen: a case that always
returns became `(tea.Model, tea.Cmd)`; a case that falls through to the composer returns a trailing
`bool`, and the dispatcher re-adopts the model it hands back. **That second shape is load-bearing**
— several fall-through paths call pointer-receiver helpers (`resizePane`, `diagnoseLastFailureCmd`,
`toggleTerminal`) that mutate `m`, so a moved block that dropped the returned model would have been a
silent behavior change on a value receiver.

`internal/tui` coverage moved **58.2% → 58.1%**, i.e. slightly down, and that is the honest reading
rather than a disappointment: the split adds dispatcher statements no existing test exercises. The
item's argument was that a 1,324-line function is not unit-testable at any reasonable effort and the
split is the *precondition* for moving the number, not the thing that moves it.

`Engine.Run` was left untouched and is now **P63.9**. Building the safe half is what made the
distinction concrete: `Update`'s switch cases were already independent units, so moving one could not
change what another did, while `Run` has no such seams and extracting from a 10-deep scope requires
first deciding what is genuinely per-turn state and what is run-scoped. That is a design question,
not code motion, and it does not belong under a heading whose other half was mechanical.

---

**Last updated:** 2026-08-06 — off a Tier-4 assessment pass that measured before deciding:
**P61.7's in-repo half shipped**, **P61.7(b) shipped** (the classifier disagreement the same
measurement exposed), **P49.3 dropped**, and **P62.1 filed** (repo-map selection, Tier 2) on a
measurement that reframed the dropped item's successor. Write-ups immediately below.

**P61.7(b) — two classifiers, one string, contradictory verdicts (SHIPPED 2026-08-06).** Measuring
P61.7 turned up a second defect that needs no injection at all. `Retryable()` and
`IsBackendUnavailableError` both read the same free-form server string, through different tables in
different orders: `classifyStreamError` scanned `terminalStreamSignals` first and returned "do not
retry" on a hit, while `IsBackendUnavailableError` scanned `backendDeadStreamSignals` directly with
no terminal check. So `"model runner has unexpectedly stopped (last output: … unsupported …)"` came
back terminal-and-final AND backend-is-dead at once — two verdicts whose recoveries contradict each
other, produced by any server that appends detail to a crash message.

The defect was already pinned in the suite and nobody had noticed: `"worker crashed: context length
exceeded"`, a deliberately-authored existing test string, produced `Retryable=false`/`Dead=true`. No
test had ever asked the two functions the same question.

Replaced with **one ordered ladder** (`classifyStreamMessage`) that all three classifiers read:
context-overflow → backend-dead → remaining terminal → remaining retryable → unrecognized (terminal,
P33.16's default, unchanged).

**Backend-dead now outranks generic terminal**, reversing "terminal always wins". The cost that
justified terminal-wins does not exist on that branch: P33.16 chose terminal-by-default because a
retry burns another full prompt-eval on a slow local model, and a *dead* backend cannot burn one —
the attempt fails at connect in milliseconds. The error asymmetry compounds it. A false "dead" costs
one liveness probe that short-circuits immediately plus a context reset; a false "terminal" aborts
the whole phased drive on a half-built suite, which is the exact failure P50.1 was filed for. And
`terminalStreamSignals` is deliberately broad (`malformed`, `unsupported`, `does not support`,
`invalid request`) — precisely the vocabulary an echoed fragment hits by accident — while
`backendDeadStreamSignals` name specific mechanical events. The specific claim about machine state
should beat a broad substring in prose.

**But context-overflow outranks backend-dead**, which is why this is a three-way ladder and not an
inversion. An oversized prefill plausibly *causes* the runner to die; if a crash-plus-overflow
message took the wait path, the drive would wait for the corpse, re-send the same oversized prompt
and kill the server again, while P47.2's fresh-context reset — the recovery that works — never ran.
Size explains the death; the death does not explain the size. This is also what keeps
`"worker crashed: context length exceeded"` terminal, as it has always been.

One gap surfaced on review of the first implementation: `IsContextOverflowError` still bypassed the
ladder via its `IsTruncatedToolCallError` tail, so
`"invalid tool call arguments: unexpected end of JSON input; connection reset by peer"` returned
dead AND overflow together — the very double-verdict the ladder exists to prevent. The raw
truncation signature now sits on the ladder's first rung and all three classifiers answer from one
classification. `TestStreamClassifiersAgree` asserts the coherence invariants (*dead ⇒ retryable*,
*dead and overflow mutually exclusive*) independently of its table, so a later row cannot slip past
them.

**Repo-map truncation notice + a latent cap violation (SHIPPED 2026-08-06,** filed under P62.1's
measurement**).** `Render`'s truncation notice said only that the map was truncated, so a 10-of-672
prefix was indistinguishable from a complete map of a small repo. It now reports the omitted count
and names the P49.2 `repomap` tool that can answer for the rest, converting a silent gap into an
actionable one. Fixing it exposed a latent bug: the notice was appended *after* the fit loop with no
budget accounting, so a truncated render could exceed `MaxBytes` by the notice's length. `Render`
now reserves the notice's worst-case length before filling. This was the one change that is
policy-independent — true whatever selection order is chosen later — so it shipped ahead of P62.1.

**P61.7 (in-repo half) — Aegis was the backend echoing model text into a classified error message
(SHIPPED 2026-08-06).** The item was filed Tier 4 on the grounds that its likelihood "depends on
whether any backend in real use echoes generated text into an error envelope, which nobody has
measured." The measurement found the echo without leaving the repo: the OpenAI adapter's
`chunkDecoder.Finish` built its own message with `fmt.Sprintf("invalid tool call arguments for %q:
…", acc.name)` and handed it to `NewStreamError`, whose signal tables then substring-matched it.
`acc.name` is the tool name **the model emitted**.

Measured through the real classifiers, a malformed tool call named `crash_report` — not an
adversarial prompt, just a plausible tool name hitting the `"crash"` signal — came back
`Retryable()=true` **and** `IsBackendUnavailableError()=true`, against `false`/`false` for the same
failure named `write_file`. The second verdict is the larger blast radius and is not what the item
predicted: it routes the phased drive's P50.1 recovery into a liveness probe and wait-and-resume for
a server that never died. The truncation branch one line up carried the identical defect via
`NewContextTruncationError`'s `underlying` argument.

Fixed by separating the two jobs the `Message` field was doing. A new `APIError.Detail` field carries
model-authored text: rendered by `Error()`, ignored by every classifier. `NewMalformedToolCallError`
gives that failure class a **fixed** message and an explicit terminal verdict, with the tool name in
`Detail`; the truncation branch does the same. Because `Message` is read nowhere outside the
classifiers (plus one `does not support thinking` check in the Ollama adapter), nothing else moved.

The regression test asserts **invariance**, not a verdict — every classifier answer must be identical
across nine tool names whatever signal they collide with, compared against an honest-named control —
so the underlying policy stays free to change while the injection surface stays closed. One
observation deliberately left alone: `IsContextOverflowError` answers true on this path for *every*
name including the control, because P47.x matches the raw truncated-tool-call signature. That is a
policy question about malformed vs. truncated calls, is not model-influenceable, and was out of
scope.

**What remains open:** the original external-backend case — a server or proxy echoing generation
fragments into its own `{"error":…}` envelope. That is still unmeasured and still needs a structural
signal most local backends do not supply. Measurement confirmed the reverse direction is live there:
`"model runner has unexpectedly stopped (last output: … unsupported …)"` classifies terminal while
`IsBackendUnavailableError` independently says the backend is dead — the two classifiers disagreeing
on one error, which is a defect even with no injection involved.

**P49.3 — LSP-backed symbol extraction for the repo map (DROPPED 2026-08-06).** The item was
explicitly measure-first: build only once regex extraction, rather than edge coverage, is shown to be
the limiting factor. Measured on this repo, the map renders **7847 of a 8000-byte budget and
truncates**, fitting 187 lines out of 673 files and 7208 top-level symbols. LSP's contribution is
*nested* symbols and reference edges — strictly more content contending for a budget that already
cannot fit the top-level ones, dropping whole files at the boundary to make room.

So the gate cannot be met by extraction work at all: precision that never reaches the model is not
precision. The limiting factor is **selection** — which files earn the budget — and that is a
different item, on ranking or relevance-scoping the map. Dropped rather than parked, on the same
reasoning as P49.4: a parked item invites a future reviewer to build it from the write-up, and this
write-up's premise is now known false. **Re-file only if** a budget/selection tier ships and
extraction fidelity is then shown to be what limits the result.

**P61.8 — the daemon and the diagnostic stop giving two answers to one question (SHIPPED
2026-08-06).** P61.4 fixed `aegis doctor`'s belief that `provider.context_window` is what the server
serves; the daemon still held it, one layer down, in the function every consumer of the effective
window reads from. `applyDetectedWindowFor` (`internal/server/contextwindow.go`) reconciled a
detection against config with a rule that is exactly right natively — a *loaded-model* reading below
`cfgWin` wins, otherwise config wins — and exactly wrong on the OpenAI-compat `/v1` path, which
cannot carry `num_ctx`. There, config is a statement of intent the server never hears, so "otherwise
config wins" meant a `context_window: 32768` beat a real modelfile/default reading of 4096, and that
number went on to drive the compaction trigger, the engine's proactive-compaction check, `/status`,
the TUI usage bar, and — since P61.4 — `Request.NumCtx` and therefore the compat adapter's own
`max_tokens` clamp.

The exposure was narrow and the item said so: the `cfgWin` branch only beats a *non-authoritative*
reading, such an entry stays `final: false`, and `maybeRefreshContextWindowFor` downgrades it after
the first run. What that leaves is the first turn on each model — the one carrying the full system
prompt plus any skill body — spent believing there is 8x the room Ollama is serving, so nothing
compacts and Ollama truncates from the front (P39.9's shape, bounded to one turn).

Built as the item prescribed: mirror `doctorServedWindow`'s rule rather than rewrite the
reconciliation. On an `IsLegacyOllamaCompat` provider `cfgWin` is neutralized to zero before the
existing switch runs, so **any** reading wins regardless of authority — which is not a relaxation but
a truth about that path, where a modelfile or default reading is not a guess about what will be
served, it *is* what will be served, because nothing overrides it on the wire. Neutralizing rather
than adding a branch keeps one reconciliation rule ("whatever the server says, wins") and one warn
when the two disagree. When nothing was detected at all, `configEntry` substitutes
`ollamainfo.DefaultServeContext`, and `initContextWindow` now takes the same
keep-the-base-and-retry branch on this path as the native one, since letting config stand `final`
there would pin an unservable number for the daemon's lifetime.

Two boundaries are deliberate and each is pinned by its own test. The **stand-in default** rides
`IsOllamaPortBaseURL`, not `IsLegacyOllamaCompat`: the latter's bare-`/v1` half also matches LM Studio
and liteLLM, which have their own serving defaults, and inventing Ollama's 4096 for a server that
isn't Ollama would be a new wrong answer rather than a fix. The **detection-wins rule** rides the
broad predicate instead, and can afford to: reaching `applyDetectedWindowFor` at all means
`ollamainfo` got an answer out of a native Ollama API, so the ambiguity the stricter predicate guards
against is already resolved by the successful probe. Config still means what it always meant on the
native adapter — this is a compat-path carve-out, not a general demotion — and the new
`ollama:compat-default` provenance is rendered in `/status` and `/doctor`-adjacent TUI output with
the reason attached, rather than hiding behind the existing `ollama:default`.

Tests: `TestApplyDetectedWindowCompatPathTakesGuessOverConfig` (the deliberate inverse of the
pre-existing `TestApplyDetectedWindowConfigWinsOverGuess` — same reading, opposite correct answer,
which is the whole item in two tests), `TestApplyDetectedWindowNativePathUnaffectedByCompatRule`,
`TestConfigEntryCompatSubstitutesOllamaDefault`, `TestConfigEntryAmbiguousCompatBaseKeepsConfig`, and
`TestInitContextWindowCompatUnreachableSubstitutesAndRetries`.

---

**P61.5 + P61.6 + P61.1 + P61.3 + P61.4 and the openai liveness
probe: the P61.x cross-adapter drift batch closes except its parked item.** Filed 2026-08-05 as seven
items on one theme — every piece of stream hardening lived in `internal/provider/ollama` and had
never been ported to the two adapters that also serve local backends — the batch stood at
**one open (P61.7, Tier 4, measure-first)** plus one newly filed successor, **P61.8** (shipped above).

Its shape is the one the filing's sequencing note predicted and then under-sold. Doing the
*structural* item second rather than last did not merely make the Tier-1 fixes "nearly free": it
turned **P61.1** into option wiring and closed **P61.3** with **no production code at all**, since
the classification P61.3 asked for is a line inside the lifted skeleton. The order actually built
was P61.5's prerequisite bullet → P61.6 → P61.1 → P61.3 → P61.4, and each item after the refactor
cost a fraction of what writing the same fix three times and deleting two copies would have.

Two of the five write-ups below exist mainly to record a **correction to the filed item**, and in
both cases building from the roadmap text alone would have shipped something wrong: P61.5 prescribed
an `errors.Join` argument order that would have made retryable failures unretryable, and P61.3's
stated fix ("the fix is a constructor swap") was both larger and smaller than the item thought — it
needed no swap in the end, and classification alone would still have left the recovery it was filed
to restore completely inert.

**P61.5 — five papercuts in the adapter and decorator chain, one of them prescribed backwards.** All
five are a few lines each and none warranted its own item; what makes the group worth a write-up is
that the smallest of them was the one the roadmap got wrong.

The **`think`-rejection retry** (`ollama.go`) captured the original 400 into `rejection` and then
overwrote `err` with the retry's failure, so a caller facing two failures saw only the second and
lost the "does not support thinking" signal that explains why a second request was sent at all. The
item prescribed `errors.Join(rejection, err)`. **That order is wrong and would have shipped a
regression.** `errors.As`/`errors.Is` walk a joined error in order and stop at the first match, so
putting the stale terminal 400 first hands every downstream classifier — `retryable()`,
`IsBackendUnavailableError`, `IsContextOverflowError` — the think rejection instead of the failure
that actually ended the request. A retryable 503 or a dead backend on the retry would have been read
as an unretryable, unrecoverable 400, silently disabling P50.1 recovery for exactly the models that
need the retry path at all. Shipped as `errors.Join(err, rejection)`, with the order asserted rather
than commented.

The other four: **`failoverAdapter.Stream`** now checks `ctx` before each target, so a cancelled run
stops at the first instead of walking the whole chain with a guaranteed-to-fail request and a `WARN`
per hop (the context's error is reported, joined with the last target's when one exists, since the
context is what ended the attempt). **`admissionAdapter.Stream`'s first `select`** read as a
cancellation check and was not one — Go picks uniformly among ready cases, so with a free slot and an
already-cancelled context the acquire won about half the time and a dead request took a slot and a
full backend turn; the check is now a plain `ctx.Err()` before the select, which keeps the original
shape for the *blocking* wait, where `ctx.Done()` genuinely races the slot and either outcome is
correct. **`healthClient`** was a package-level `http.Client` with its own default transport, so a
user's proxy or TLS configuration did not reach the liveness probe that gates P50.1 recovery; it now
shares the adapter's transport (and its connection pool) while keeping its own 3s timeout, since
inheriting the streaming client's deliberately-unbounded one would defeat the point of a liveness
check.

The fifth was the prerequisite for everything else in this batch, which is why it was built first.
**`WithResponseHeaderTimeout` replaced `a.client` wholesale** in all three adapters, so option order
decided which of two independent client settings survived — a latent hazard on ollama/openai and an
actual one on anthropic, where `WithHTTPClient` and `WithResponseHeaderTimeout` both assigned
`a.client` and whichever came second silently discarded the other. New `sse.ApplyResponseHeaderTimeout`
*composes* onto whatever client is present: it clones the transport rather than mutating it (a
caller-supplied client may share its pool with the rest of that caller's program), and a client whose
`Transport` is a custom `RoundTripper` rather than an `*http.Transport` is returned intact with no
timeout applied, because dropping the caller's RoundTripper to gain a timeout is the wholesale
replacement this function exists to stop. Anthropic additionally remembers the configured
`headerTimeout` so a later `WithHTTPClient` can re-apply it, which is what makes the two options
commute in *either* order rather than only one.

Tests: `TestStreamThinkRetryFailureKeepsBothErrors` (both errors survive **and** `errors.As` finds
the retry's 503 first, still retryable — the assertion that holds the argument order),
`TestHealthProbeUsesTheAdapterTransport` (the probe goes through the adapter's transport exactly
once, keeps its own timeout, and does not disturb the streaming client's zero),
`TestFailover_CancelledContextStopsAtFirstTarget` and `TestFailover_CancelledMidChainKeepsTheTargetError`,
`TestAdmissionCancelledContextNeverAcquiresASlot`, and three `sse` tests around
`ApplyResponseHeaderTimeout` plus `TestClientOptionsComposeInEitherOrder` on anthropic.

**P61.6 — the stream lifecycle now has one home, and the item's own sketch of it was not expressive
enough.** `internal/provider/sse` owned everything identical across adapters *except the part that
kept breaking*: the client, the sized scanner, the cancellation-aware `Emitter`, `HandleErrorResponse`
— and then each adapter wrote its own `consume`, independently re-deriving the idle watchdog, the
terminal-chunk requirement, the `scanner.Err()` classification, the `bufio.ErrTooLong` naming and the
final `EventDone`. All five had drifted, in the same direction, toward whichever adapter was in front
of whoever was last debugging a local model.

New `internal/provider/sse/run.go` — `sse.Run(ctx, body, out, opts, dec)` — owns all five. It closes
the body and the channel, drives the watchdog, reads the body line by line through the decoder,
classifies a read failure, enforces the terminal requirement and emits the final `EventDone`. Each
adapter is reduced to per-chunk decode, which is the part that legitimately differs (NDJSON with
native tool calls, SSE with `[DONE]`, SSE with indexed content blocks): `ollama.chunkDecoder`,
`openai.chunkDecoder`, `anthropic.eventDecoder`.

**The roadmap sketched a single `decodeChunk` func value, and that cannot express this lifecycle.**
Two adapters need a post-loop hook and those hooks sit on **opposite sides of the terminal check**:
anthropic must flush a buffered final event *before* `scanner.Err()` is classified (its SSE framing
dispatches an event on the blank line that ends it, so a stream not ending in one holds its last
`message_delta` — the very event that makes the stream count as terminal — only in the buffer),
while openai must flush accumulated tool-call arguments *after* the terminal check, which is P61.2's
deliberate ordering and the reason a cut stream cannot emit a half-assembled tool call. One callback
cannot be on both sides of a check. Hence a `Decoder` interface (`Line`/`Finish`) plus an **optional**
`Flusher` for the one adapter whose framing needs it, and a `Status` enum
(`Continue`/`Terminal`/`End`/`Abort`) as the decoder's only vocabulary for lifecycle decisions —
`Continue` is the zero value, so a decoder falling off the end of its switch keeps the stream alive
rather than ending it. `End` exists solely because OpenAI's `[DONE]` is the one terminator that also
promises no further lines; everything else may legitimately be followed by a usage-only trailer.

The second thing the item did not anticipate: by the time this was built, **P61.2 had converged the
three adapters on deliberately *different* terminal conditions**, so the requirement could not simply
be lifted. A naive reading — "the terminal check drifted, so unify it" — would have hard-coded one
terminator and broken Ollama's `/v1` compat path, which omits `[DONE]` and ends on a `finish_reason`
chunk. So `Options.MissingTerminal` parameterises the *message* while the *condition* stays in the
decoder, which is where the wire-format knowledge belongs; an empty `MissingTerminal` disables the
requirement entirely, which no shipped adapter does — it exists so a future format with no terminator
at all need not lie about one.

Two behavior changes arrived with the refactor rather than as separate fixes, which is the point of
having done it: `scanner.Err()` is now `provider.NewTransportError` on openai and anthropic (this is
**P61.3**'s entire production fix — see below), and `bufio.ErrTooLong` is named as a 4MiB line-limit
problem on all three instead of surfacing as bufio's opaque "token too long" on two.

Tests: `internal/provider/sse/run_test.go` covers the skeleton in isolation (14 cases: terminal
present/absent, the optional requirement, `StatusEnd` stopping the read, abort emitting nothing
further, `Flush` running and being able to terminate, `Flush`'s abort winning, `Finish` ending the
stream itself, transport classification, the named oversized line, the watchdog firing/disabled/
resetting per line, and context cancellation). Each adapter's existing suite is unchanged and still
passes, which is the check that the decoders preserved their wire behavior rather than being
rewritten around the new shape.

**P61.1 — `provider.stream_idle_timeout` finally means the same thing on every backend.** With
P61.6 landed this is wiring, not mechanism: `WithStreamIdleTimeout` and `resolveStreamIdleTimeout()`
on the openai and anthropic adapters with semantics **identical** to ollama's (0 → the 10-minute
default, negative → disabled), and `buildOne` passing the configured value to all three. The
mechanism itself — the watchdog that closes the body, which is the only way to break a blocked
`bufio.Scanner`, and the transport classification that routes a wedged runner into the same
`waitForBackend`/resume-from-disk path a crashed one takes — is `sse.Run`'s, written once and tested
once.

The user-facing half of the item was the untruth, and it is corrected in three places.
`internal/config/config.go`'s doc comment said the key was "honored by the native ollama adapter,
where the stall this catches actually happens" — which was both the documentation *and* the
rationalisation of the gap, and false on its own terms: the openai adapter is a local path too (it is
what talks to Ollama's `/v1` compat endpoint), so the backend most likely to wedge was the one only
half covered by a key users reasonably read as global. `docs/providers.md` now states the key applies
to all three adapters, and ollama's own option doc no longer says `<= 0` selects the default while
also saying a negative value disables the bound.

Tests: `TestStreamIdleTimeoutWiring` and `TestStreamIdleTimeoutAbortsAStalledRunner` on both
adapters, plus the structural guard described at the end of this section, which is the one that
matters — per-adapter tests cannot catch "the *fourth* adapter forgot".

**P61.3 — closed by P61.6 with no production code, and two corrections to what the item claimed.**
The fix the item asked for ("the fix is a constructor swap") is a single line of `sse.Run`:
`scanner.Err()` is wrapped in `provider.NewTransportError` rather than a bare `fmt.Errorf`, so
`IsBackendUnavailableError` and `retryable()` — both of which begin with `errors.As(err, &APIError)`
and return false otherwise — can see it at all. P35.12's `bufio.ErrTooLong` naming, the item's second
half, is likewise now shared. Since nothing new was written for this item, closure was verified by
**mutation**: reverting `run.go`'s transport wrap to a bare `fmt.Errorf` fails the new tests on all
three adapters.

**Correction 1: "no retry" was never about this error.** The item states a killed `ollama serve`
mid-stream on the compat path gets "no retry, no `waitForBackend`, no resume-from-disk". The middle
and last are right; the first is not, and it is not a property of the classification.
`retryAdapter.Stream` only retries errors returned **synchronously** from `Stream`, deliberately, so
that partial output already emitted to the caller is never replayed. A mid-stream `EventError` is not
retried on **any** adapter, ollama included, and no constructor swap could have changed that. What
the swap actually buys is the recovery path, which is a different mechanism and the one that
mattered.

**Correction 2: `waitForBackend` had a second precondition the item never mentioned, and
classification alone left recovery inert.** `drive/health.go` returns `backendNotDown` unless the
adapter answers `provider.CheckBackendHealth` — there has to be something to wait *on* — and only
`ollama.Adapter` implemented `provider.HealthChecker`. So a correctly-classified backend death on
the `/v1` path still aborted the drive, and P61.3 as filed would have shipped as a half fix that
looked complete and tested green. That residual is the next entry.

**The openai liveness probe — P61.3's residual, and a deliberate divergence from the native one.**
`openai.Adapter.Healthy` probes `GET <base_url>/models`, so `provider.HealthChecker` is satisfied and
`recoverBackendDown` returns `backendRecovered` rather than `backendNotDown` on the OpenAI-compat
path. The endpoint is `/models` and emphatically not Ollama's `/api/version`: that path is not part of
the OpenAI API, and this adapter also serves real OpenAI, LM Studio, liteLLM and assorted gateways.
`/models` is the one liveness endpoint every OpenAI-compatible server implements, and it is
side-effect-free — it lists what is configured, loads and unloads nothing, and is not a metered
completion, so probing a paid backend costs nothing.

What counts as healthy is **looser** than the native probe's "200 or nothing", and the difference is
the whole design. The question here is **liveness, not usability**: `recoverBackendDown` already knows
the request failed, and all it needs to learn is whether there is a server on the other end again. A
401 from a gateway that wants a key, a 403, a 404 from a backend that routes `/chat/completions` but
not `/models`, a 429, a 500 — every one of those proves an HTTP server answered. Treating them as
unhealthy would be **worse than having no probe at all**: the drive would burn its full 10-minute
recovery budget waiting for a server that never left. The carve-out is the gateway trio — 502, 503,
504 — where the responder is a proxy explicitly saying the *upstream* model server, the thing being
waited for, is gone. The probe client mirrors P61.5: the adapter's transport, its own 3s timeout, and
configured headers sent (a gateway may need them to route at all) with `Authorization` only when a
key was configured, so an unauthenticated local server is not handed an empty bearer.

Anthropic deliberately gets no probe, and the reason is about the *backend* rather than about which
adapter is "native": there is no local server to wait for, a transient remote outage is already the
retry decorator's job, a longer one outlasts any bounded wait, and probing is a billable round-trip
against someone else's quota. P50.1's `supported == false` means "don't wait on this backend", which
is the honest answer.

Tests: `TestHealthyTreatsAnAnsweringServerAsAlive`, `TestHealthyRejectsGatewayUpstreamFailures`,
`TestHealthySendsConfiguredHeaders`, `TestHealthyReachableThroughDecorators` (the probe must survive
the decorator chain a real session is wrapped in), and — the one that asserts the *point* rather than
the mechanism — `drive.TestRecoverBackendDownReachesTheOpenAIAdapter`, which drives the real
`recoverBackendDown` with the real adapters: a live compat server yields `backendRecovered`, a dead
one yields `backendGaveUp` (the wait actually ran and expired, which is what distinguishes a wired
probe from an unwired one, since both end the phase but only one keeps the suite resumable), and
anthropic still yields `backendNotDown` as the control.

**P61.4 — both halves of the item's either/or, and the second half turned out to be a repair rather
than an addition.** The item offered (a) plumb the resolved window onto the request so the compat
adapter can clamp, or (b) leave the clamp Ollama-native and have `aegis doctor` refuse the
unreconciled pair. Both shipped, because they cover different populations: the clamp only fires when
a window was actually resolved, and the diagnostic is what covers every case where one was not.

**(a)** `openai.clampMaxTokens` mirrors `ollama.clampNumPredict` exactly — same reserve arithmetic,
same 512 floor, and never raising the caller's request — deliberately, because the two adapters can
be pointed at the *same* Ollama server and a user must not get a different completion budget
depending on which one the config happened to select. No new plumbing was needed at all:
`Request.NumCtx` already reached the adapter via `provider.WithNumCtx` and was simply being ignored.
What differs from the native clamp is the gate, and it is three conditions that all fail closed.
`WithSharedContextWindow` is set by `buildOne` **only** for a `:11434` base URL, via a promoted
`IsOllamaPortBaseURL` — deliberately narrower than `IsLegacyOllamaCompat`, whose bare-`/v1` half also
matches LM Studio, liteLLM and any gateway fronting a cloud model, where `max_tokens` is a *separate*
output allowance and clamping it would truncate a legitimate long generation. That predicate exists
to make *advice* over-reach, and advice can be dismissed where a clamp cannot; missing a proxied
Ollama costs the clamp, not correctness. On top of that sits a structural refusal of
`api.openai.com` regardless of the option, and `req.NumCtx > 0` — no invented default, because the
compat endpoint cannot report the served window and there is no honest number to guess.

**(b)** was not a new check. `doctorGenerationBudgetCheck` has existed since P59.1 and was **silently
useless for exactly the configuration it was built for**: `doctorServedWindow` trusted
`provider.context_window` on the stated grounds that "that is exactly what the adapter sends as
num_ctx" — true natively, and **false on `/v1`, which never sends it**. So `max_tokens: 32768` against
`context_window: 32768` reported PASS while Ollama served its 4096 default, which is the 8x-the-window
shape P59.1 exists to catch, reported clean by the row built to catch it. On the compat path the check
now judges against the *detected* reading, falls back to Ollama's documented default window when the
server can't be reached but the base URL is unambiguously Ollama, and no longer names a knob that path
ignores: the remedy says `provider.context_window` cannot help here and points at
`OLLAMA_CONTEXT_LENGTH` or the adapter switch the "provider adapter" row already spells out. The
fallback is honest for a diagnostic in a way it would not be for a clamp — it costs a line of advice
if wrong, not a truncated generation.

Tests: `TestStreamClampsMaxTokensToHeadroom`, `TestStreamMaxTokensUnclampedWithoutAWindow`,
`TestStreamMaxTokensUnclampedOnANonSharedBudgetBackend`, `TestClampRefusesTheRealOpenAIEndpoint`,
`TestClampAppliesToReasoningModelsField` (the clamp must land on `max_completion_tokens` too),
`TestIsOllamaPortBaseURL`, and `TestDoctorGenerationBudgetFixNamesTheKnobThatMoves`.

**The structural guards, which are as much of this batch's value as any single fix.** P61.6's own
statement of the problem is "the failure mode is *the next adapter forgets*, not *an adapter is
wrong*", and per-adapter tests cannot see that. `internal/providerfactory/idlebound_test.go` drives
every adapter `buildOne` can construct against a server that sends headers and then goes silent, and
requires each to bound the stall *and* classify it as backend-unavailable; a second test parses the
supported-provider list out of `buildOne`'s own unknown-provider error, so a **fourth** adapter
cannot be added without appearing in the table. `internal/providerfactory/streamdeath_test.go` is the
same idea for the death rather than the stall: it kills the connection by **hijacking** it, which is
load-bearing rather than incidental — without the hijack, `net/http` completes the response framing
on handler return and the client sees a clean EOF, which is the P61.2 case and would have passed the
test while proving nothing about P61.3. It asserts the read failure survived into the message (so the
right branch of `sse.Run` is under test), that the error is a `*provider.APIError`, that
`IsBackendUnavailableError` and `Retryable()` both see it, that an oversized line is named on every
adapter and is deliberately *not* backend-unavailable (the backend is alive and re-running would
reproduce it), and — via `livenessProbeWired` — that each adapter's position on P50.1's second
precondition is a checked fact rather than a belief.

---

**P61.2 — a cut-off stream was reported as a completed answer on openai and anthropic (SHIPPED
2026-08-05).** The first item built out of the P61.x cross-adapter drift batch, filed the same day,
and the one the batch's own sequencing note said should not wait behind P61.6's refactor: it is the
only Tier-1 member that produces a *silently wrong answer* rather than a degraded recovery. The
defect is P59.3's, unported. Both adapters' `consume` loops emitted `EventDone` unconditionally once
the scan loop ended, and a body closed mid-generation — a proxy cutting the response, a gateway or
model runner exiting between chunks — leaves `scanner.Err()` nil, so no read-failure path fired.
`usage` stayed zeroed, which `engine.go` silently replaces with `tokenest` estimates flagged
`IsEstimated`, exactly the treatment a legitimately usage-free provider gets. The result was a
plausible short answer with correct-looking accounting and no error on any surface.

Both loops now track whether a terminal chunk arrived and return `provider.NewTransportError` on a
clean EOF without one, so the failure takes the same `IsBackendUnavailableError` recovery path —
`waitForBackend`/resume-from-disk, P50.1 — that ollama's has taken since P59.3. No new recovery
machinery, which is what kept this cheap, and the same argument P59.3 made for classifying it as
transport rather than inventing a category.

The terminal condition is deliberately **loose in both adapters**, because a check strict enough to
reject a legitimate stream would be a worse regression than the bug it fixes. openai accepts
`[DONE]` **or** any chunk carrying a `finish_reason`: `[DONE]` is a convention of OpenAI's reference
server rather than a guarantee of the compat wire format, and compat backends — Ollama's `/v1`
endpoint among them, which is the live local path this item exists for — close after the final
`finish_reason` chunk without sending it. anthropic accepts `message_stop` **or** a `message_delta`
carrying a `stop_reason`; `message_delta` is where the completion semantics actually land (final stop
reason and output-token count), so a stream that lost only the envelope's last event is complete and
none of the harm above applies to it. `handleData` had no `message_stop` case at all before this.
Either way the real failure — neither arriving — is still caught. The check sits **before** the
accumulated-tool-call flush, so a cut stream cannot emit a half-assembled tool call.

One correction to the filed item, recorded because the roadmap text overstated the anthropic half:
it describes the truncation as surfacing with a stop reason claiming the model chose to end its turn.
That holds on openai, whose `stop` defaults to `StopEndTurn`; anthropic defaults to `StopOther`, so
there only the zeroed-usage half applied.

Tests: `TestStreamWithoutTerminatorIsAnError` (openai) and `TestStreamWithoutTerminalEventIsAnError`
(anthropic) feed a body that closes cleanly mid-generation and assert the result is classified
backend-unavailable so the P50.1 resume path handles it. Their converses —
`TestStreamTerminatorsStillComplete` and `TestStreamTerminalEventsStillComplete` — are the ones that
matter more, holding the looseness above: every accepted terminator, including the envelope-less
`finish_reason` and `message_delta` cases, must still finish with `EventDone`, the right stop reason
and no error event.

---

**P59.11 — the tool-failure nudge's 25.9x, closed by finding the missing observation (SHIPPED
2026-08-05).** P59.10 fixed the zero-tool nudge's 51x prefill cost and deliberately left the
tool-failure nudge (P52.3) at **25.9x** (67ms → 1745ms of next-run prefill), for a stated reason:
the zero-tool nudge is *spent* by construction — its injection gate can never fire again after a
tool round — whereas the tool-failure nudge was merely **bounded** to one per run, so retracting it
early would have removed a live correction while the failures it addressed could still recur. A
measured cost traded for an unmeasured behavioral risk.

The gap was an observation, not a mechanism. A tool-failure corrective is spent when **the failure
streak it was correcting ends** — which the engine already computes, since
`toolFailureTracker.reset()` clears both counters on a round with no errors at all. That is now
exposed as `cleared()`, and `nudgeState.retractSpentToolFailure` strips the nudge at that point
instead of at run end.

What makes it safe is the second half: the nudge is now **re-injectable**. The old "one per run"
bound was doing two jobs — stop the engine nagging on every subsequent failing round, and bound the
total. Only the first matters, and it is now held by `toolFailureOutstanding`: while an un-retracted
nudge is in the conversation no second one is ever injected, so a run that keeps failing still sees
exactly one nudge and then the abort threshold, unchanged. A second nudge is reachable only after a
demonstrated recovery *and* a fresh threshold-length streak — a new episode, quoting the new error,
and an **append**, which no prefix cache minds. `toolFailureNudgeMax` (3) bounds the pathological
oscillation. So the corrective is never absent while it is needed, and the break costs the recovery
window rather than the whole remainder of the run.

The roadmap's alternative (retract from the persisted transcript only) stays rejected for the same
reason P59.10 rejected it: P25.3 wants the scaffolding out of a later turn's context too.

Tests: `TestP5911ToolFailureNudgeRetractedOnRecovery` replaces the old measurement-holding test and
asserts the break; `TestP5911ToolFailureNudgeReinjectedAfterRecovery` holds the property that makes
early retraction safe (streak → recovery → relapse gets a second nudge, and never two at once);
`TestP5911ToolFailureNudgeSurvivesAnOngoingStreak` is the converse — a run that never recovers keeps
the corrective in front of the model to the end, so the 25.9x case still exists and is now exactly
the case where paying it is correct. The pre-existing P52.3 suite
(`TestToolFailureNudgeAfterThreeAllErrorRounds`, the abort test) is unchanged and still passes,
which is the check that the one-per-run behavior it encodes was preserved rather than relaxed.

---

**Last updated:** 2026-08-05 (fourth pass) — **P59.10 + P52.16 + the P59.9 loose end: the
measure-first batch.** Three items that had been parked in Tier 4 with explicit "do not build
without a measurement" instructions. The measurements were taken; two of the three promoted and
shipped, and the third corrected a *rationale* rather than a behavior. The batch's shape is that in
every case the recorded hypothesis was partly wrong, and the measurement is what said which part.

**P59.10 — nudge retraction versus Ollama's prefix cache.** `nudges.retractAll` strips corrective
scaffolding out of the middle of `conv.Messages` once a run settles, so the next run re-sends an
edited history and a local runner's KV prefix cache is invalid from the first changed token onward.
The item's promotion trigger was "a measured turn shows `prompt_eval_duration` failing to collapse".
It does not collapse. Measured against Ollama 0.30.10 / qwen3:14b, the next run's first turn cost
**3604ms of prefill against 71ms unretracted — 51x**, and within noise of a cold prefill of the whole
conversation (3711ms), meaning the cache was not dented but wholly lost. `prompt_eval_count` stayed
at the full prompt length throughout (4621 either way), reconfirming P35.13: duration, not count, is
the cache-hit signal.

The measurement also **refuted the item's own cost model**. P59.10 assumed the damage was "bounded to
the tail — nudges are appended late in a run". That holds for exactly one of the three families
measured. The guard corrective genuinely is a tail edit (85ms → 57ms, free) because the guard only
runs once a final answer exists. But the **zero-tool nudge is injected as early in a run as it is
possible to be** — its gate fires only while `toolRoundsCompleted == 0` — so retracting it at run end
invalidates every token the run produced, and the more productive the run the more its own retraction
costs. The tool-failure nudge lands mid-run and measured 25.9x.

The fix is a change of **when**, not whether. `nudgeState.retractSpentZeroTool` strips the zero-tool
nudge the moment the first tool round completes — which is the moment the engine already treats it as
spent, since the injection gate can never fire again that run. Nothing the run could still have used
is removed, and only that one round sits downstream of the break instead of the whole run. Measured
back to **73ms (1.0x)**, i.e. indistinguishable from never having retracted. The roadmap's own
proposed fix (retract from the persisted transcript only, leaving the in-memory conversation intact)
was **not** taken: P25.3 states the scaffolding has no business in the durable transcript *or a later
turn's context*, and that fix sacrifices the second half — it would have bought the prefill back by
re-opening the leak P25.3 exists to close. The tool-failure nudge was left at 25.9x: it is
bounded to one per run, so unlike the zero-tool nudge retracting it early would permanently remove a
correction while the failures it addresses can still recur — a measured prefill cost traded for an
unmeasured behavioral risk. (**P59.11 closed that later the same day**, by making the retraction
conditional on the streak actually clearing and the nudge re-injectable — see the write-up above.)
Tests: `internal/engine/prefixcache_test.go` is a committed measurement
harness (break position and re-prefill tokens per family, plus an `AEGIS_P5910_DUMP` hook that emits
the replay inputs for the Ollama-side half), with `TestP5910ZeroToolNudgeStillRetractedFromTranscript`
holding the P25.3 property the timing change must not weaken.

**P52.16 — native Ollama tool-result disambiguation.** Ollama's native API correlates tool results by
*name*, with no ID, so three parallel `read_file` calls — which the engine explicitly permits, since
read-capability tools run concurrently in `runTools` — produce three wire messages identical in their
correlation metadata, leaving position as the only signal. The item was measure-first because the
proposed mitigation (prefixing each result with a compact echo of its call) could plausibly *hurt* by
adding noise. A paired A/B on a 3-parallel-read attribution task, graded on naming the file a fact
came from, found the conflation is real and confined to where the item predicted it — small models:

| model | bare | echoed |
|---|---|---|
| qwen2.5-coder:1.5b | 32/40 | 38/40 |
| qwen3:14b | 9/10 | 10/10 |
| gemma4:12b | 20/20 | 20/20 |

One methodological note worth keeping: a first run scored 10/10 in *both* arms because the fixture's
file bodies named their own path, letting the bare arm attribute from content and never exercising
the missing metadata at all. Removing that confound is what surfaced the effect.

Shipped narrower than proposed. The echo is applied **only to rounds that call the same tool more
than once** (`ambiguousRound`) — the case where the protocol genuinely cannot disambiguate. A round
calling each tool once keeps today's exact bytes, so the common case pays no tokens and, more
importantly, no prefix-cache churn from a re-encoded history. The rendering (`toolResultEcho`) sorts
keys and truncates values so it is deterministic and bounded: an echo that varied run-to-run would
break the very cache `translate`'s ordering rationale exists to protect. Tests:
`internal/provider/ollama/toolresultecho_test.go` (5 cases: unambiguous rounds byte-identical,
same-tool rounds disambiguated, order-independence, value bounding, non-scalar args skipped).

**P59.9's loose end — the concurrency default was a stated policy, not a measured one.** P59.9
shipped `provider.max_concurrent_requests` defaulting local backends to 1, and the roadmap recorded
that the measurement behind that number was never taken. Taking it **kept the default and corrected
the reason for it**. The stated rationale was correctness: that Ollama splits its KV cache across
`OLLAMA_NUM_PARALLEL` slots, so a request sized against the full `num_ctx` is silently truncated.
That does not reproduce. A needle test — four concurrent ~12k-token requests at `num_ctx` 16384, each
carrying a passphrase in its *first* tokens, the region truncation drops first — returned all four
verbatim, with identical `prompt_eval_count` (12034) and zero failures. Nothing was truncated and
nothing was evicted.

What concurrency actually costs is latency, and the aggregate gain is well short of linear: 11.2
tok/s at K=1, 15.6 (1.40x) at K=2, 17.9 (1.60x) at K=4, while p50 turn latency goes 5.7s → 6.7s →
9.8s. So a second in-flight request buys ~40% throughput for ~70% worse turn latency — the wrong
trade for an interactive agent, a defensible one for a batch of independent sub-agents. The default
stays 1 on that basis. The correction matters because the old comment told an operator that raising
the depth risked silent truncation, which would have deterred exactly the swarm use case where it is
the right call; prefix-cache reuse also survives concurrency intact (29–47ms prefills at every depth
measured), so raising it forfeits nothing but latency headroom. No behavior change — the doc comments
in `internal/provider/admission.go` and `internal/config/config.go` and the `docs/providers.md`
section now state what was measured.

**Last updated:** 2026-08-05 — **P59.9 + P60.2 + P60.4: three places where the harness owned no
policy for something it was nonetheless doing.** Nothing bounded how many requests reached one local
model server; nothing owned a sandbox's lifetime, so it had none; and nothing separated the harness
from the model when a live run failed.

**P59.9 — no admission control in front of a local backend.** The daemon serves concurrent sessions,
swarm spawns sub-agents, and the guard/compaction/title passes are requests of their own. Against a
cloud endpoint that is harmless — the provider fans them out across a fleet. Against one Ollama
server it is false in a way that costs correctness rather than latency: every request is built
believing it owns the full detected `num_ctx` (`WithNumCtx`, `engine_build.go`), while Ollama splits
its KV cache across `OLLAMA_NUM_PARALLEL` slots and evicts models to fit. Detection reads `/api/ps`
for the *loaded* allocation, which is a correct reading of one resident model and says nothing about
N concurrent claims on it. On a 16GB box that is the OOM-and-evict path — and it lands squarely on
the two failures the P59.x batch already built for: a shrunken per-slot window is P59.1's truncation,
an eviction mid-run is P59.2's stall.

Built as `provider.WithAdmissionControl`, a semaphore decorator wired in `providerfactory`, with
`provider.max_concurrent_requests` as the policy: 0 is *auto* (local backends get 1, cloud stays
unbounded), a positive value applies to any backend, and a negative one opts a local backend back out
— which is why 0 could not simply mean "unbounded". Three placement decisions carry the design. The
slot is held for the **whole life of the stream**, not until `Stream` returns, because the request
occupies the model until its last token; a cancelled run releases immediately (the forwarder drains
the base channel in the background) so an abandoned stream cannot permanently cost one unit of a
depth that defaults to 1. It sits **inside** the retry decorator, so a backoff sleep does not sit on
a slot a queued caller could use. And it is applied **per built adapter**, not once around the
composed chain, so a local primary with a cloud fallback does not hand the cloud its single-GPU queue
depth. "Local" is deliberately broader than the factory's data-residency test — any loopback
`base_url` (LM Studio, llama.cpp, a proxy) is one GPU too.

It deliberately does **not** detect VRAM: P20.3 and P17.5 both rejected that and their reasons hold.
A queue depth is a policy an operator sets, not a capacity a harness infers — which is also the
answer to why `swarm.AdaptiveLimiter` was not extended instead. That limiter bounds *spawns*,
reactively, by observing whether measured speedup tracks n; that is the right question for "is this
host CPU-bound" and the wrong one for a fixed VRAM budget, which is not something you discover after
the fact. This makes the limiter's job easier rather than replacing it. The default of 1 is a
judgement, not a measurement: the item asked for a measured before/after to size it, and until that
exists the conservative end of its own suggested range is the honest choice, with the key documented
so a host with room says so explicitly.

**P60.2 — the container sandbox was a fresh container per command, so nothing persisted between tool
calls.** `ContainerBackend.Exec` built `docker run --rm …` for *every* invocation and `Close()` was a
no-op because there was nothing to close. The visible cost was start latency; the real cost was that
no state survived a tool call. An installed toolchain, a warmed build cache, a background dev server,
a half-applied migration — discarded the moment the command returned, with the workspace bind-mount
the only channel through which anything was observable. An agent could not do the ordinary thing
(`npm install`, then `npm test`) without collapsing it into one shell string. That is why the
container backend was hard to recommend: for multi-step work it was behaviourally *worse* than
`local`, not merely slower.

Built as the shape Orchard Env uses: `run -d` per workspace directory, `exec` per command, teardown
on `Close()` — on by default (`sandbox.persistent`) wherever the CLI surface is verified, which is
docker and podman only, the same "only where verified" rule `OCIHardeningFlags` and `ResourceFlags`
already follow. wslc and Apple Containers keep the per-command behavior and the daemon says so at
startup rather than letting an operator infer persistence that isn't there. Explicitly not adopted:
Orchard's in-pod HTTP agent, which exists to bypass the Kubernetes API server across ~1000 sandboxes;
`docker exec` gets the whole benefit on one host.

One claim was narrowed while building it, because the intuitive version is wrong: **filesystem and
process state persist, shell state does not.** Each `exec` is a new process with a new shell, so `cd`
and `export` still die with the command — exactly as they do between two shell calls on the `local`
backend. What now survives is everything installed, everything written outside the mount, and
anything left running.

The honest cost is that `--rm`-per-command is what made the old design leak-free, and this trades
that for owned state. Three things buy it back, and all three are the design rather than hygiene:
every container carries `aegis.sandbox` and `aegis.sandbox.owner=<pid>@<host>` labels; its entrypoint
is a bounded `sleep` (`sandbox.session_ttl_sec`, 4h) under `--rm`, so an orphan removes *itself* with
nothing needing to run; and a daemon start reaps containers whose owning pid is verifiably absent on
this same host. That reaper's safety property is asymmetric on purpose — PID reuse can only make it
too conservative (an orphan left to its TTL), never too aggressive (killing a live session's
sandbox). Alongside: a failed start degrades to one-shot runs and says so *once* rather than per tool
call; a vanished container is restarted and the command retried exactly once, so a TTL expiry reads
as a slow command rather than a failed one; the container count is capped per backend so an unusual
set of directories cannot leak; the detached run carries the *same* hardening flags, resource limits
and network posture a per-command run does, so persistence never becomes a quiet way to get a weaker
container; and the subprocess swarm worker closes its own sandbox, since one leaked container per
spawned teammate adds up fast. Tested against a fake container CLI, so the whole lifecycle is covered
by `go test ./...` on a machine with no container runtime installed.

**P60.4 — the live-workflow eval had no cross-harness control group.** `TestLiveWorkflow` measures
Aegis and the model *fused together*: when a run failed its workflow-shape assertions, nothing in the
result distinguished "this local model is too weak" from "our scaffolding regressed". That was only
ever knowable because the same model had passed before — no help at all for a model being tried for
the first time, and the P25.x regressions it caught were all scaffolding.

The adoptable idea from Orchard is its evaluation discipline, not its infrastructure: hold the
environment fixed and swap the harness. The obstacle was that the task lived *inside* the test —
`writeSeededBugFixture` was Go that materialized a fixture and the assertions read Aegis's SSE
stream. Built the split the item asked for: `internal/eval/workflowtask.go` now owns the fixture, the
prompt and the **outcome** check (re-run the program and check the answer — never the agent's claim
of success), harness-independent, with a `Harness` seam whose implementations are Aegis-over-HTTP+SSE
and any other CLI agent (`AEGIS_EVAL_BASELINE_HARNESS='claude -p {prompt}'`). `Compare` turns two
outcomes into an attribution: only-Aegis-failed is **scaffolding** (a test failure), both-failed is
**model** and an unusable baseline is **unknown** (both skips — a control group must be able to
*decline* to blame the harness, or a weak local model turns this tier red and teaches people to
ignore it).

Two boundaries are deliberate. The SSE-shape assertions (tool-call budget, no `find /` detours, no
guard meta-text leakage, token accounting) stay Aegis-only: they read our own event stream, and a
baseline required to emit them would restrict the comparison to harnesses built like ours — exactly
the bias a control group must not have. And the outcome check rejects the two ways to "pass" by
deleting the problem (hardcoding the answer, dropping the data source), because a harness comparison
is worthless if passing can mean that. Because nothing in the task file talks to a model or the
network, it is ordinary code unit-tested by plain `go test ./...` — the live tiers stay behind their
build tags, and the baseline tier is opt-in on top, since a stale baseline is worse than none.

---

**Last updated:** 2026-08-05 — **P59.7 + P60.1 + P59.8: three seams where a number, a limit and a
constraint each lived in only one of the places that needed them.** Different subsystems, one shape:
something the system already knew was not reaching the component whose behavior depended on it.

**P59.7 — the engine's context window was immutable while the adapter's was not.** The P47.5b
overflow escalation makes the served `num_ctx` a monotonic runtime floor on the Ollama adapter,
outranking any per-request value. But `engine.contextWindowTokens` was captured once at
`engine.New` and never re-read, and the compactor learns its window from the server's per-model
detection path. Nothing connected the escalation to either — so a mid-run raise left the engine
compacting against the pre-escalation number, burning summarizer calls (minutes, on a local model)
on a conversation that now had room, during the very overflow recovery that raised the window.
Latent rather than live, since only the CLI drive escalates today, and about to become live the
moment a daemon-hosted drive (P52.12) uses the escalation.

Built as a *reading* seam matching the writing one: `provider.ContextWindowFloorReporter` +
`provider.RaisedContextWindow`, which unwraps the retry/failover decorators exactly as
`RaiseContextWindow` does — because the escalation is written *through* that chain, so anything that
stopped at the retry decorator would report 0 forever and be silently wrong. The Ollama adapter
reports `numCtxRaised`, not `numCtx`, and that distinction is the whole correctness argument:
`numCtx` is the adapter-wide *fallback* for requests carrying none of their own, so a consumer
reading it would mistake a configured default for an escalation and override a correctly-detected
per-model window (P52.1) with a server-wide one. `numCtxRaised` is non-zero only because
`RaiseContextWindow` made it so. `engine.Options.ContextWindowFloor` is a func rather than a value
for the same reason the bug existed — a value would be captured at construction again — and the
engine takes the larger of it and the constructed window before every turn, so the floor is a floor
on both sides rather than an override. Nil (every non-escalating backend) changes nothing.

One consumer was deliberately *not* connected: the summarizer's own token budget. The CLI drive had
already recorded that as a considered choice ("a larger num_ctx only buys physical headroom against
a transient overshoot"), and it is right — sizing the summary request to the new room spends the
recovery's winnings on the recovery. What was wrong was the *trigger*, not the budget. The comment
now says which of the two is deliberate, so the split reads as a decision rather than as the same
oversight one layer down.

Tests: an engine run whose conversation trips the trigger against the constructed window and must
not compact once a floor reports room; the inverse for a zero floor (no escalation yet) and a
smaller-than-configured one, since a floor that could shrink the window would silently disable
proactive compaction for every backend that can't escalate; and the decorator-unwrap guard through
failover→retry→base.

**P60.1 — the container sandbox set no resource limits.** `OCIHardeningFlags` covers the privilege
axis correctly (`--cap-drop=ALL --security-opt=no-new-privileges`) and there was nothing at all on
the resource axis: no `--memory`, no `--cpus`, no `--pids-limit`, on any of the three run paths. A
model-driven `go build`, `npm ci` or test run could consume the whole host, and on a 16GB machine
that host is also running the daemon *and* the model server — so the failure mode is the OOM killer
choosing between Ollama and Aegis, not a failed command. `--rm`-per-command bounds a runaway's
*duration* and never its peak, and the peak is what binds: one `go build` is enough.

Built: `sandbox.ResourceLimits` (memory, cpus, pids) + `sandbox.ResourceFlags(rt, lim)`, appended by
all three run paths, configured under `sandbox.limits.*` and defaulted conservatively (4G / 2 CPUs /
1024 pids) — sized to let ordinary build and test work through while making a runaway a failed
command. Values pass through to the runtime CLI verbatim in its own vocabulary ("4G", "512M", "1.5")
rather than being parsed here; the engines already own that vocabulary and a second, poorer parser
would only add ways to be wrong. Each field is independently optional, so emptying one is the
documented escape hatch for a heavy toolchain.

The per-runtime split is the part with teeth, and it is decided the way `OCIHardeningFlags` decides
its own: a flag a runtime does not know is **not a weaker limit, it is a container that refuses to
start** — which is exactly how the pre-P24 hardening copy silently killed every wslc scanner run,
reported as "the tool is missing from the image". So a flag appears only where it is verified.
docker/podman take all three. Apple Containers takes `--memory` and `--cpus` (both documented
Resource Options of `container run`) and not `--pids-limit`, which its CLI does not have. wslc takes
nothing: its resource surface is unverified, and applying an unverified flag to the one runtime
already known to reject unknown arguments is a trade this codebase has paid for once. That gap is
surfaced rather than hidden — `SupportsResourceLimits` backs a startup WARN when limits are
configured but the selected runtime cannot enforce them, because a cap the operator believes is in
force and isn't is worse than no cap. `aegis sandbox test` runs with the limits applied for the same
reason: a test that passes uncapped and a real run that dies at the cap is the failure that command
exists to catch.

Tests: the per-runtime subset matrix, field independence (including a non-positive pids count
meaning "uncapped" rather than `--pids-limit -1`, and whitespace-trimmed values), the flags actually
reaching all three built command lines with the hardening flags undisplaced and the image still
preceding the shell invocation, and an end-to-end config check that the defaults survive the layers
and arrive in the sandbox package's shape.

**P59.8 — Ollama structured outputs (`format`) for the schema guard.** `wireRequest` had no `Format`
field, so grammar-constrained decoding was unused anywhere in the harness. This does **not** reopen
the parked question of grammar-constrained *tool calls* (P53.6's lead); it takes the one caller with
no open design question behind it. `guard.SchemaGuard` requires the final text to parse as a JSON
object carrying every required key, and that requirement was expressible to the backend ahead of
generation and wasn't being expressed — the guard asked in prose, then hoped, tolerating a leading
```json fence precisely because the hope often fails.

Two pieces, as the item sequenced them. The plumbing: `provider.Request.Format` (a JSON Schema),
passed through by the native Ollama adapter as `format` and ignored by every other adapter —
deliberately, and documented as such. OpenAI's `response_format` wants a *named* schema plus a
decision about strict mode (which would reject the open-ended property schemas this carries), and
Anthropic has no equivalent; ignoring it is safe because it is an optimization and never a
correctness requirement — the guard's own check still runs and is what decides. A backend that
ignores the constraint has to keep failing loudly rather than passing on the strength of a
constraint that was never applied.

The use: `guard.SchemaFormat` renders the required keys as `{"type":"object", properties:{k:{}},
required:[…]}` — presence only, open values, open `additionalProperties`, because that is exactly
what `SchemaGuard` asserts and a schema that invented types would constrain the model beyond what
the guard will enforce, under a grammar it cannot argue with.

The one design decision the item left implicit is *which turn* gets constrained, and the answer is
**only the corrective retry after a schema-guard failure**, never the ordinary turns. A first turn is
where the model does the work — reading files, calling tools — and a grammar forcing a JSON object
out of it forbids exactly that. By the time a schema guard has rejected an answer, the remaining task
genuinely is "emit this object", which is the one moment the constraint describes the intent instead
of fighting it. Tools are suppressed on that turn for the same reason, reusing the existing
`suppressTools` path. The constraint is consumed per retry rather than latched for the run, so a
second failure re-applies it and no unrelated later turn inherits it.

Tests: the first turn unconstrained and still carrying tools while the retry carries the schema and
no tools; an llm-mode guard's retry staying free generation *with* its tools (that retry's whole
point can be "go fix the file you wrote"); the per-retry rather than per-run lifetime across two
failures; `SchemaFormat` requiring exactly the keys the guard checks, de-duplicated and trimmed, with
`{}` value schemas and nil for nothing-to-require; and two adapter wire tests — the schema arriving
verbatim, and `format` absent from the serialized body on every ordinary request (a decoded struct
cannot tell an omitted key from a null, so the raw body is checked).

Full suite green.

---

**Previously, 2026-08-05:** **P59.4 + P59.5 + P59.6: the Tier-2 half of the P59.x
local-execution review, minus P59.7.** Three items that each take a mechanism built for a cloud
provider and ask what it means on one local GPU. None is a correctness bug; all three are cases where
the harness gives a locally-correct answer to a question the local user was not asking.

**P59.4 — one token budget was answering two different questions.** `cost.max_tokens_per_run` sums
`prompt_eval_count` across every turn, and on Ollama that count is the *full prompt each turn*, not a
per-turn delta (as `ollama.go`'s translate doc establishes at length, and correctly). For a priced
provider that is exactly right — you are billed on the whole prompt every turn and the number is the
spend. For an unpriced local backend it is the only budget that does anything (`BudgetUSD` is a
documented no-op there), so users reach for it as a *work* budget and get an ~O(N²)-in-conversation-
length number instead: a 20-turn run on an 8k window reports ~160k tokens consumed while the model
generated a small fraction of that, and a cap set with local work in mind aborts far earlier than
intended. The item offered two fixes — document the current meaning, or re-point the cap at generated
tokens for unpriced providers — and flagged the second's cost: it splits one key's meaning across
provider classes. **Neither was taken; a third avoids that cost.** `cost.max_tokens_per_run` keeps its
meaning untouched for everyone already using it, and a separate `cost.max_generated_tokens_per_run`
counts output tokens only, backed by a new `cost.Tracker.TotalGeneratedTokens()`. Two keys, each
saying in its own name what it counts, and no config value whose meaning depends on which provider
you pointed it at. Both are checked at the same two gates as the other budgets, via one
`tokenBudgetExceeded()` helper — the generation cap first, so a run that blew both is told the more
specific thing. The abort messages carried equal weight: the old one ("used N of M token limit") gave
a user nothing to reason from precisely because the number it reported was not the quantity they had
in mind, so the context-budget abort now says the count includes the whole prompt every turn and
names the key that bounds generated output instead. Sub-agents inherit the new cap **whole** rather
than as a divided share — `swarm.WithBudgetOverride` carries exactly two dimensions and widening that
seam is its own change — which bounds each teammate individually and is documented as such.

**P59.5 — a documentation recommendation was re-introducing the exact churn `keep_alive` exists to
prevent.** `docs/providers.md` recommended configuring a fast `small_model` before enabling the
output guard, and `engine_build.go` correctly resolved that model's own context window (P52.4). The
advice is sound on Anthropic, where the small model is separate remote capacity. On one local Ollama
server it inverts: the guard fires on every final answer plus its corrective retries, and each call
naming a model other than the resident one can evict that model and force a full cold reload on the
next turn — on a 16GB-VRAM box, every post-guard turn. That is precisely what
`providerfactory.defaultOllamaKeepAlive` and the P33.9 `load_duration` telemetry were built to
eliminate, and the engine was already *reporting* it (the cold-load notice would fire on every
post-guard turn) while attributing it to nothing. `guardModel` now resolves per backend: an explicit
`output_guard.model` first, then `provider.small_model` on a cloud provider, then the session's own
model — which on a local backend means the guard runs on the weights already loaded. The new
`output_guard.model` key is the deliberate half: an operator with the VRAM to hold two models still
gets the split, but by asking for it rather than inheriting it from a key meant for compaction and
titles. `docs/providers.md` now splits cloud from local instead of giving one recommendation for
both; the advice about avoiding a *thinking* model for the guard's PASS/FAIL contract is unchanged
and still correct, with the added note that on a thinking session model with no VRAM headroom,
leaving the guard off beats paying a reload per turn.

**P59.6 — prose-emitted tool calls were only detected on turns with no tool calls.** Both the P34.2
notice and the P53.6 shim parse sat inside the `len(toolUses) == 0` branch, so a model that emitted
one *real* tool call and printed two more as prose in the same reply got neither: no notice, and
under the shim the printed calls were silently dropped rather than parsed or declined — after which
the model reasoned about results that never existed. That partial-protocol shape is characteristic of
the 14-27B class the shim was built for; the zero-call gate encoded "can this model ever produce the
protocol" into a check that should have been asking "how often", which is exactly the distinction
P53.4's conformance rate exists for. Both checks now run on any turn whose reply text carries the
shape. The notice branches on what actually happened — a mixed turn is reported as a partial-protocol
reply naming what did not run, not as a model that cannot call tools, since it demonstrably can. Its
"no tool call has succeeded all run" gate is **kept** for the zero-native-call case, where it is
still doing real work: a model quoting JSON after its calls have succeeded is quoting, not failing,
and warning there is the false positive `TestToolCallAsTextNoticeSkippedAfterRealToolCall` exists to
prevent. The shim half needed the mixed-round decision the old gate sidestepped, and it went the way
the item called the safer default: **decline and correct**, consistent with the parser's existing
decline-rather-than-repair posture — a turn written half in each dialect is genuinely ambiguous about
intent, and dispatching both halves would double-execute a model that wrote one call twice. The
correction rides `shimMixedPending` to just *after* the tool round, the same mechanism
`loopNudgePending` uses and for the same reason: the native calls are real and must still be answered
with their results, so nothing may be appended between an assistant's tool_use blocks and them. It
reuses `shimFormatNudgePrefix`, so it retracts with the rest of the shim scaffolding and is bounded
by the same two-per-run budget.

Tests: a two-case table proving the generation budget ignores 800k of accumulated input and fires
exactly on generated output, a direct assertion that the context-budget abort names the other key and
that the generation budget takes precedence when both are blown, the local/cloud/explicit matrix for
`guardModel` (the P25.3 small-model regression is untouched and still passes — the local case is its
inverse, not its contradiction), a mixed-round notice test asserting the real call still executes,
and a shim mixed-round test asserting the printed call never runs, the correction lands after the
results rather than between them, and it is retracted from the durable transcript. Full suite green,
race detector clean on `engine`/`server`/`cost`/`config`. **P59.7 remains open** — it is the one
Tier-2 P59.x item that is latent rather than live, and its fix is a plumbing change to how three
consumers read the context window.

---

**Previously, 2026-08-04:** **P59.1 + P59.2 + P59.3: the harness now reasons about the
generation, not only about the prompt.** The Tier-1 half of the P59.x local-execution review batch,
built the day it was filed. The three items share one root: every part of the context subsystem
reasons about the *prompt* — how big it is, when to compact it, how to detect the served window —
and nothing reasoned about the *completion* or about the stream carrying it. On Ollama that is a
false split, because `num_ctx` is one budget covering both.

**P59.1 — `max_tokens` was never reconciled with the served window.** `provider.max_tokens` defaults
to 32768 and rode straight through to `options.num_predict` in the same request that carries
`num_ctx`, and detected windows are routinely 4096 (Ollama's own server default). Nothing validated
the pair — not `internal/config`, not `aegis doctor`. The engine compounded it by compacting at a
flat 85%, a threshold that reserves headroom for prompt *growth* and was never sized against
generation: at 4096 it left ~614 tokens to answer in. What made this Tier 1 rather than a config
footgun is that the recovery path *amplifies* it — exhausting the context mid-generation yields
`done_reason: "length"` → `StopMaxTokens` → the engine's "continue from where you left off" retry,
which grows the context, shrinks the next turn's headroom, truncates sooner, and burns to
`maxIterations`. Silent front-truncation reached through generation instead of prompt growth, which
is the one direction nothing watched. Fixed in all three of the places the write-up identified,
each independently shippable: `compactionTrigger(window, maxTokens)` sizes the trigger as
`window - min(maxTokens, window/2)` less a margin, floored at half the window and **capped at the
old 85%** so it can only ever compact earlier, never later (a property a table test asserts across
the whole window/max_tokens grid — compacting *later* would be a new way to overrun a window);
the native adapter clamps `num_predict` at request-build time to the headroom the prompt actually
leaves, using the same `internal/tokenest` estimate the engine compacts against so the two agree
about how full a window is, one-directional so a caller asking for a short answer still gets one,
and floored at 512 because the honest number for an over-full prompt is negative, which Ollama reads
as "generate until the context is full" — the exact behavior being avoided; and `aegis doctor` grew
a **generation budget** row that warns when `max_tokens` claims more than half the served window,
naming the knob. The doctor row is the one that reaches the user *before* the failure, and it fires
on the shipped default config against a stock Ollama install.

**P59.2 — nothing bounded the gap between streamed tokens, and no budget could fire mid-turn.**
`sse.NewStreamingClient` leaves `Timeout` at zero and bounds only `ResponseHeaderTimeout`, which is
correct and well-argued (the timeout covers reading the body too, and Ollama withholds the header
until prefill finishes — why P38.1 raised it to 30m). But once headers arrive nothing watched the
gap between chunks: a wedged runner left `for ev := range stream` blocked indefinitely, and
`MaxWallClockPerRun` — the one budget whose entire purpose is "don't spend more than N minutes on
this" — is polled at exactly two gates, before each model turn and before each tool round, so it was
structurally unable to fire during the phase where local backends actually hang. Both halves fixed.
An inter-chunk idle watchdog in the adapter's `consume` closes the body when nothing arrives for
`provider.stream_idle_timeout` (default 10 minutes, negative disables); it resets per *delivered
line* rather than per parsed chunk, since a line we skip is still evidence the runner is alive. The
resulting error lands on the existing P50.1 branch, so a wedged server takes the same
`waitForBackend`/resume-from-disk path a crashed one already did — no new recovery machinery, which
is what made this cheap. The bound is deliberately generous: prefill precedes the headers, so every
gap it sees is an inter-token gap, and a bound tight enough to catch a hang quickly would guillotine
a legitimately slow run (the regression shape P52.15 avoided by leaving the wall clock off by
default). Separately the wall-clock budget became a `context.WithTimeout` on the run in addition to
the polls, so it bounds the turn it is already inside. That needed one piece of care: cancellation
surfaces as whatever the cancelled call returned — a bare `ErrInterrupted`, or an adapter transport
error a caller *classifies as backend-unavailable and would then wait for and resume*, turning a
deliberate budget abort into a retry loop. `wallClockOverride` re-derives the real reason at all
three exits (loop-top cancel, turn error, tool-round error), leaving an ordinary interrupt its own
identity. Tested at both ends: a run that hangs inside a turn aborts on the wall clock, and a run
with no budget configured still hangs until its caller cancels — the pre-existing contract, and the
reason the knob is opt-in.

**P59.3 — a stream ending without a `done` chunk was indistinguishable from a finished turn.** In
`consume`, `usage` started zeroed and `stop` defaulted to `StopEndTurn`, and `EventDone` was emitted
at function end regardless of whether `chunk.Done` was ever seen. A server closing the body cleanly
mid-stream leaves `scanner.Err()` nil, so neither the P50.1 mid-stream-read-failure path nor the
P35.12 line-limit path fired; the engine saw zero usage, substituted estimates and flagged
`IsEstimated` — the same treatment a legitimately usage-free provider gets — so a truncated response
surfaced as a complete short answer with a stop reason claiming the model chose to end its turn. Now
tracked with a `sawDone` flag and classified as a transport error, which is what it is: the same
failure P50.1 handles, minus the read error. Cheap on its own, and it also removes the ambiguity that
would have made P59.2's idle-timeout trips hard to tell from clean short answers — which is why it
went first.

Docs updated (`docs/configuration.md`, `docs/providers.md`), including one pre-existing drift caught
alongside: the response-header-timeout section still documented the pre-P38.1 5-minute default.
Full Go suite green, `-race` clean on `internal/engine` and `internal/provider/ollama`.

**Previously, 2026-08-04** — **P58.1 + P58.2: closing the two gaps between "task runner" and
"daily assistant".** Filed off a whole-solution review (2026-08-04) asking a question this project
had never explicitly asked: is Aegis usable as an everyday copilot for research, documentation, and
code analysis, rather than as a security/threat-modeling task runner? Most of the answer was yes and
already built — `deep-research` is a real structured-research workflow, the report and diagram
skills produce real deliverables, sessions are daemon-backed with a picker and no git-repo
requirement, and `workspacetrust` gates *project config*, not plain chat in an arbitrary directory,
so asking a general question outside a repo has no friction. Two gaps were real, and both were
narrow:

**P58.1 — a scheduled job could not tell anyone anything.** `internal/cron` is a generic recurring
scheduler, not a security-specific one, so "give me a 9am digest" was already expressible. But a
fire's outcome only ever landed in the `cron_runs` audit table, readable via a `cron_history` tool
call from inside a session. Nothing pushed. That makes a digest or watch job self-defeating: the
entire value of firing unattended is being *told* the outcome, and the user had to remember to go
ask. The fix needed no new subsystem — `internal/notify` (desktop via osascript/notify-send/toast,
plus an optional JSON webhook) had existed since P5.4 and was wired to exactly one producer,
background-session completion. So cron became its second producer rather than growing a parallel
mechanism. `Job.Notify` is a per-job opt-in (schema-migrated like `auto_approve` and `workdir`
before it, so existing rows are untouched), surfaced on `cron_create`, `cron_list`, `aegis cron
list`, and the operator-facing API view. It is per-job rather than a global switch on purpose:
whether an outcome is worth interrupting a human for is a property of the job — a daily digest
wants delivery on success, a minute-by-minute watch job would be a firehose — and off-by-default
means no existing job changes behaviour. Delivery hangs off the *same* call site that writes the
audit record, taking the same `(status, output)` pair, because a notification that disagreed with
the durable log would be worse than no notification; it fires on all three outcomes, including
`blocked`, which got its own `notify.Status` rather than being folded into `error` since the two
want different reactions (blocked means it never ran and the permission setup is what needs
attention). `Event` gained `JobID` and `Output`: the output *is* the payload for a digest, so the
webhook carries it in full and the human-readable message leads with a rune-safe single-line
excerpt, rather than a notification that says "job completed" and sends the user back to
`cron_history` to read the thing they asked to be told about. Tested at the fire seam: notification
on success/failure/blocked with status and output matching the audit record exactly, silence for a
job that did not opt in, `Notify` surviving a `Toggle` rewrite (the whole row is rewritten, so a
field that scanned but never persisted would reset silently), and the excerpt's flattening and
UTF-8-boundary truncation.

**P58.2 — no generic "document this codebase" skill.** `documentation-as-code` reads like one and
is not: it only activates when an *external* DaC repository supplies `docforge.py` and an
organization's branded template families, and it explicitly defers to `latex-report` otherwise.
`html-report`/`latex-report` are report-shaped — a standalone deliverable about a moment in time for
a reader outside the repo. Nothing covered the everyday ask: a README, an ARCHITECTURE doc, a
package overview, an API reference, an onboarding guide — a file a maintainer edits next month,
living next to the code. `document-codebase` fills that, and the distinction drives its content:
repo documentation is judged by whether it is still true in six months, so the skill is organised
around the four ways generated docs fail — restating the code, documenting what was inferred rather
than read, duplicating detail that guarantees drift, and bulldozing human-authored prose. Hence:
settle audience and document type first (they do not merge), read before describing, run the
commands you document or flag them unverified, cite `file:line` while drafting but keep only stable
anchors in the delivered text, write one section per edit (the same anti-monolithic-write
constraint P39.14 measured), and default to surgical edits over replacement when a doc already
exists. Surfaced as `/document` in the TUI alongside `/report` and `/research`, on the same stated
rationale those two carry — a discoverable entry point beats relying on the model noticing a trigger
phrase. Being a built-in it stays dormant until enabled, so it costs nothing until asked for.

Both items also corrected pre-existing documentation drift found on the way: every built-in-skills
list in `docs/` and `CLAUDE.md` was still missing `documentation-as-code`, and CLAUDE.md now states
how to choose between the three confusably-named documentation skills, since picking wrong produces
a correctly-built document of the wrong kind. Full Go suite green.

**Previously, 2026-08-03** — **P57.1: a model looping on its own wrong theory no longer kills
the drive.** The 2026-08-03 P38.1 live re-confirmation run did everything right except finish. The
phased drive reconned, scaffolded and filled all five content phases with no mis-route, recovered
itself from a mid-findings context overflow, and phase 6's verifier caught real cross-file defects
and correctly told them apart from mechanical ID-format ones (confirmed independently:
`normalize_ids.py --check` reported the suite already canonical). It re-opened the owning content
phase, exactly as P47.9 intends. Then the re-opened phase decided a `T0`-vs-`T01` zero-padding
offset existed between two files — it did not — re-read the same ~30 lines five turns running, the
engine's one corrective nudge failed to break the cycle, and the engine aborted. A second manual
invocation against the same target and the same model, with a fresh context, fixed every defect and
reached a clean suite (verify.py 19/19, inventory 10/10, lint_dfd 6/6). That contrast is the whole
diagnosis: not the model, not the check scripts — **the context**, which by then held four
restatements of a wrong theory and nothing contradicting it.

The drive already knew how to survive two errors that are terminal to the engine but resumable at
the phase level: a context overflow (P47.2/P47.7) and a consecutive-tool-failure trip
(P52.3). Both recover the same way — discard the context, re-read the suite from disk. The loop
abort was the one remaining engine error still fatal, and its own rationale
(`recoverToolFailureStall`'s "a model re-guessing arguments from a context dense with its own
failures") describes the loop case at least as well. So it is now classified the same way:
`engine.ErrLoopDetected` is a wrapped sentinel (matched with `errors.Is`, never by message text),
and `recoverReasoningLoop` gives it a **fresh-context reset with its own budget** — separate from
the overflow and tool-failure budgets, so two failure modes cannot spend each other's allowance —
at all three engine-error sites: content phases, the phase-6 verify/quality loop, and the P47.9
re-entry where it actually fired. Two resets, then a resumable stop, deliberately tighter than the
overflow budget: an overflow is a mechanical limit a fresh context genuinely clears, while a model
looping may be looping on something real.

The reset alone would only be half the fix, because the same phase with the same prompt can rebuild
the same theory. So a loop-recovered retry also carries **`StuckLoopDirective`** — the roadmap's
filed candidate direction, which is the same shift from "figure out what's wrong" to "here is
what's wrong" that `scaffold.py` (P38.4) made for structure. It tells the fresh context that the
verifier's `file:line` report already in its prompt *is* the finding rather than a hint, that
identifier numbering/padding/offsets are not a scheme to be worked out (the checks compare exact
strings), not to re-read a file it has already read this turn, and to leave a defeating item and
fix the others. It takes a `withReport` flag because a content phase's prompt carries a PENDING
file list rather than a verifier report, and pointing a stuck model at a report its prompt does not
contain would invite the exact invention the directive exists to remove. It is not a second copy of
`ActNowNudge`: that one fights narration, and this model was calling tools every single turn.

Tested at the seam the failure lives on, matching the package's existing convention (its `Engine`
is concrete, so there is no fake-engine end-to-end): the verdict's budget/escalation behaviour, the
sentinel's separation from the tool-failure and wall-clock ones in both directions, the directive's
content and its two variants, and the wiring — that the directive leads each of the three prompts
after a loop abort, that the verifier evidence survives alongside it, and that an ordinary first
attempt never pays for it. The abort's user-visible message text is unchanged. Full Go suite green.
**Live re-test still owed**, and it is the real closure condition — this is a fix aimed at a
failure shape observed once, and the roadmap item said so when it was filed.

**Previously, 2026-08-03** — **P56.1: the two surfaces that never rendered the model's
markdown now do.** The models write headings, tables, fenced code and lists; two of the three
places that display them threw that structure away. The TUI has rendered markdown through glamour
since TQ10, so this was easy to believe was solved everywhere — it was not. `aegis chat` wrote
`ev.Text` straight to stdout, and the web UI's transcript rendered assistant text as a single
`<p>{text}</p>`. A scan-summary table arrived as its literal pipe-delimited source in both.

**CLI.** The default `text` format now buffers prose and flushes it through glamour at *block*
boundaries. Per-block rather than per-turn is the whole design question: markdown cannot be styled
a token at a time, because what a line means (`| a | b |`, `  - x`) is not knowable until its block
closes — but buffering a whole turn would replace a live stream with a long silence. So the split
point is chosen structurally, and it is stricter than "ends in a blank line", because two
constructs span blank lines and render wrongly when severed: a **fenced code block** (whose body
may contain blank lines and is not markdown at all) and a **loose list** (where cutting between
items restarts an ordered list's numbering at 1 and turns one list into several). A cut is refused
while an odd number of fences precede it, and refused while the following text continues a list.
The invariant that matters more than any individual boundary is covered by a byte-at-a-time
round-trip test: repeated cut-and-emit must never drop or duplicate a byte. Tool calls changed too
— the argument JSON was one unbroken line, so a `write_file` pushed its path off the screen behind
its own payload; it is now indented, with string leaves clipped (the keys are the information, the
4KB file body is not).

The compatibility guarantee is explicit and tested: **piped output is byte-identical to what it
was**, since `--render auto` only enables rendering when stdout is a terminal, and scripts, CI jobs
and other agents consume this through a pipe. `--render on` forces it (for `| less -R`), `off`
disables it. `NO_COLOR` governs both halves — glamour's styling and the renderer's own chrome —
because styling only one of them is the worst of the three outcomes.

**Web UI.** Assistant text and thinking traces now go through `marked` → **DOMPurify** → an `.md`
container. The sanitize call is the security boundary, not marked's escaping: marked passes raw
HTML through by design, and model output is untrusted in the one sense that matters — a
prompt-injection vector can put arbitrary text into it. Verified headlessly against the real
module: `<script>`, `<img onerror>`, `javascript:` hrefs, `<iframe>`, `<style>` and `<svg onload>`
are all neutralized, while tables, lists, fenced code and inline code render. Links get
`rel="noopener noreferrer"`. Two non-obvious details: `.md p` must override the transcript's
`white-space:pre-wrap` (which is what kept *unrendered* text legible and would double every blank
line once the parser has already decided where breaks go), and rendering needs a **bounded memo
cache** — `Transcript` re-renders every item on every state change and text streams in token by
token, so without one, each SSE event would re-parse and re-sanitize the entire conversation
history.

Also in this change: the ANSI/OSC sanitizers moved out of `internal/tui` into
**`internal/termsafe`**, since the CLI renderer writes the same two classes of text (model prose,
raw tool output) to the same kind of terminal. A second copy is the shape of bug this codebase paid
for once already — the duplicated OCI hardening-flag list that made every wslc container scan
impossible (P55.7). The TUI keeps thin aliases, so its call sites and `sanitize_test.go` are
unchanged. Verified live: `aegis chat --render on` against gpt-oss:20b returns a drawn table,
bullets and inline-code pills; full Go suite green.

**Previously, 2026-08-03** — **P55.7 and P55.8 shipped, closing the P55.x
container-only-scanning batch at 8 of 9 built (P55.9 dropped, not deferred).** These were the two
items that actually close the batch's "zero required host tools" goal; everything before them made
the *existing* container trustworthy and preferred.

**P55.7 — `aegis-netscanner`: a second image split by mount posture, not tool category.** Six
scanners had no container path, and the stated reason was always the same: they need network
egress, and the scanner runner denies it. Reviewing what each one actually needs showed the split
isn't offline-vs-network — it's *what the container is allowed to see*. `nmap`/`nuclei` take a
target list, `trivy image`/`grype <ref>` take an image reference: all four need egress and **none
of them needs the workspace**, because they scan a remote target rather than local source. Only
`gosec` and `trufflehog --verify` want both at once, which is the combination the hardening exists
to forbid. So there is now a second locally-built image that runs with **network on and no
workspace mount, ever** — and the invariant is structural rather than conventional:
`runNetscannerImage` takes a target and has no directory parameter to pass, while the
multiscanner's `runScannerImage` keeps its `dir` and its `--network none`. The two resolve through
separate resolvers (`ResolveNetwork` vs `Resolve`) so "container" never means two postures at one
call site. Both images are built from **one** embedded context via `--target`, so they share one
fetch script, one set of pinned tool versions, and one source fingerprint. The concrete win: on
Windows, `recon_scan` used to require provisioning a Kali WSL distro to run nmap and nuclei — two
tools that were already sitting inside the multiscanner image the operator had built. Two
carve-outs kept, both named rather than implied: zap stays on its own official image (already no
host install, and folding a large Java app in buys nothing), and dockle stays host-only because it
needs the **engine socket**, a third privilege axis (effectively host root) that deserves its own
decision. Verification covers the second image too: version probes plus a trivy/grype scan of a
known-vulnerable public image. Picking that image took measurement rather than intuition — the
obvious choice, a tiny EOL Alpine, makes **trivy report zero on a perfectly working scanner**
(Alpine security data is per-branch and trivy stops reporting once a branch leaves support:
`alpine:3.14` → 0, `alpine:3.10` → 1, `debian:11-slim` → 190). A canary that swings between 0 and
1 would have failed correct images with the exact message reserved for a broken one, so the canary
is `debian:11-slim` with a floor of 20 against ~190.

**P55.8 — gosec without a host Go toolchain.** gosec was the one tool container-only could not
absorb, and dropping the host path without solving it would have silently deleted all Go SAST
coverage from a Go-first codebase: it is compile-assisted, resolves packages via `go list`, and
**does not fail** when it can't. The fix is not a relaxed `--network none` but the split this codebase already uses for
`update-db` — *the phase with network does no analysis, the phase that analyzes has no network*.
Phase 1 runs `go mod download` with network on and the workspace mounted **read-only**, filling a
persistent module-cache volume; phase 2 runs gosec under `--network none` against that warm cache,
mounted exactly as every other scanner is. `go mod download` fetches modules without executing
them and cannot write to the source tree, so the exposure is materially smaller than a general
network-plus-workspace grant. Live testing sharpened the justification for the fail-closed rule, and it is worth recording
because the original framing was wrong in a way that mattered. Measured on this repo with the
built image (gosec 2.28.0): host **244**, no toolchain **0**, toolchain with a *cold* module cache
**258**, toolchain with a warm cache **283**. The zero is the documented shape. The 258 is worse:
with no modules to resolve, `go list` leaves packages with type errors, gosec logs "skipping SSA
analysis" and carries on with the AST-only rules, so every type-aware rule silently stops firing
(G115 3→0, G118 5→0, G124 1→0, G702 1→0, G122 5→1, G703 13→2) while the total still looks
healthy. A zero draws the eye; a confident 258 does not, and nothing downstream can tell it apart
from a good run. So **a failed warm phase aborts gosec** rather than falling through. `GOTOOLCHAIN=auto` in the image means a repo on a newer Go than the
pinned toolchain gets that toolchain downloaded during phase 1, where the network exists. The
acceptance test is finding parity rather than "it runs": the canary fixture gained a real Go module
(stored as `.canary`-suffixed files so a nested `go.mod` can't break the embed and the planted
vulnerabilities can't be compiled into Aegis), and `verify-image` now asserts gosec finds something
in it.

Also in this change: recon targets beginning with `-` are rejected up front, since a target is
appended to an argv on the host path and passed as a positional parameter on the container path,
and in both places a leading dash reads as a scanner flag. `aegis security status` grew a second
table for the network-facing tools — resolving all sixteen descriptors through the directory
resolver reported nmap as "not installed" on machines where it was sitting inside a built image.
**Verified live against podman 6.0.0**, not just by unit test. Both images build clean.
Netscanner (569MB): all four tools probe, `VerifyNetscanner` reports trivy 190 / grype 184
through the real `ScanImage` path, nmap's no-mount report round-trip returns a parsed finding
with service detection against a live container (`nginx 1.31.3` on :80), and nuclei returns 14
findings using the **baked** template set with no `templates_version` configured — the host
path's git clone is genuinely unnecessary now. Multiscanner full (1.85GB): Go 1.26.5 and gosec
2.28.0 verified in-build, gosec's two-phase run produced **283 findings on this repo in 2m35s**
(vs the 244 host baseline), and its canary passes at 10 findings on the materialized `.canary`
fixture. Full suite green.

**Three defects the live rollout found**, each of the class this batch exists to catch — a
component reporting success while doing nothing:

- **wslc runs were never possible.** `internal/security` carried its own copy of "which
  runtimes accept the OCI hardening flags" and excluded only Apple Containers. wslc presents a
  Docker-style CLI but rejects `--cap-drop`/`--security-opt`, so on any Windows machine where
  `DetectBest` picked wslc — which it prefers there — *every* container-method scan died on
  "Argument name was not recognized", surfaced per tool as "the tool is missing from the image
  or cannot start". `internal/sandbox` had known this since wslRunArgs was written; the
  duplicate is what drifted. Now one exported `sandbox.OCIHardeningFlags`, used by all five
  runners, with a test that fails if a sixth is added with its own literal flags.
- **`build-image` silently migrated engines.** A rebuild with no `--runtime` auto-detected
  from scratch, so an operator with a working podman setup who re-ran it got the image rebuilt
  into *wslc's* store. Everything that makes the setup work is per-runtime — the image and
  both cache volumes — so the build succeeded, the pin was rewritten, and the populated
  vulnerability databases were left behind in podman, surfacing as "cache not populated"
  immediately after a successful build. A rebuild now reuses the runtime already recorded in
  config; `--runtime` still overrides for a deliberate migration.
- **`--netscanner` pinned nothing.** `SecurityPatch` grew a `Netscanner` field and
  `buildSecurityBlock` was never taught to write it. The command built the image, printed
  "pinned in: <file>", exited 0, and wrote no block — after which every network tool resolved
  as though the image did not exist. Because `patchSecurity` replaces the whole `security:`
  block, an unwritten field isn't merely unsaved, it is *deleted* on the next unrelated write.
  Fixed, plus a round-trip test over a fully-populated patch and a reflection check that fails
  when a field is added to `SecurityPatch` without being covered.

Since both images share one build context they also share one fingerprint, so rebuilding
either one marks the other stale. `build-image` now says so at the moment it happens rather
than leaving the next `security status` to raise a warning the operator has just "fixed".

**Previously, 2026-08-02** — **the P55.x container-only-scanning batch shipped 6 of its 9 items
the day it was filed** (P55.1-P55.6; P55.7/P55.8 stay open in Tier 3, P55.9 parked in Tier 4). The
batch came out of a full functional test of the multiscanner container, which found the container's
*scanning* sound and its *provisioning* full of silent failures — three sharing one shape: the
scanner stopped working and no layer of the system noticed. Now: the image carries a source
fingerprint so it can't fall behind the Containerfile unnoticed; `update-db` runs each database
independently with a per-step summary instead of aborting the rest on the first failure; a new
`aegis security verify-image` runs every bundled tool against a planted-findings fixture and asserts
a **non-zero finding count**, not exit 0; `auto` resolution prefers the pinned, confined container
over an unpinned host binary; the pin is machine-wide by default; and database age is reported.
`verify-image` justified itself immediately, catching two live silent-all-clears on its first run —
syft's container mode broken outright, and gitleaks reporting zero secrets on a tree full of them.
Measured: 12/14 tools verified with findings, and `security status` went from 6.55s to 4.03s median
after memoizing the runtime probe the resolution flip made hot. Full suite green.

**Previously, 2026-08-02** — **P54.2 closed: no gap found.** The long-standing "accurate refusal,
error-shaped" lead for the SCA/secrets scanners was swept by measurement and produced no bug —
osv-scanner's exit 128 is the only refusal of that shape and P34.12 already interpreted it. The
measurements are recorded in code so the sweep isn't repeated. Also 2026-08-02: **P53.6 shipped**,
closing the P53.x local-LLM comparative-review batch
(**0 open of 6**). Aegis had been detecting a model that writes tool calls into its prose and
discarding the signal with a notice; `provider.tool_call_shim: on` now serves the tool schemas in the
system prompt and parses tagged JSON back into real tool calls — opt-in only, through the same
permission gate as native calls, with a parser that declines a malformed attempt rather than repairing
it. Earlier the same day: **P53.5** (per-model capability records persist across restarts) and
**P54.1** (Windows/macOS cross-platform suite fixes, including a real LaTeX confinement bypass on
Windows). Full suite green on Windows; `go vet` clean cross-compiled for darwin/arm64 and linux/amd64.

**Previously, 2026-08-01** — **P52.15 shipped and P52.17 closed as already-implemented**, taking
the P52.x full-stack review batch to **2 open of 17** (P52.14 and P52.16, both correctly parked
measure-first). Full suite green, race detector clean on every touched package.

**P52.15 — wall-clock run budget.** Three budgets existed and none bounded *time*: `BudgetUSD` is an
explicit no-op for unpriced local usage, `MaxTokensPerRun` defaults to 0, and `MaxIterations` is a
step count defaulting to 40 — which on a model measured at ~7 tok/s (the P38.1 note) is potentially
hours before any safety valve trips. The constraint users actually have — "don't spend more than N
minutes on this" — had no expression at all. `engine.Options.MaxWallClockPerRun` now aborts at the
same two gates as the cost/token budgets: before each model turn (the P9 dead-zone placement — a
guard corrective retry or a max-token continuation burns just as much wall clock as a tool round) and
again before each tool round, so a spent budget stops the run before side effects rather than one
iteration late. Aborts wrap an exported `engine.ErrWallClockLimit`, the `ErrToolFailureLimit` idiom,
so a caller can classify "ran out of time" apart from "ran out of iterations"; the message names the
knob that raises it. Configured as `cost.max_wall_clock_per_run` (seconds, via
`CostConfig.MaxWallClockPerRun()`) and wired into the daemon engine build, the CLI chat engine, and
both swarm backends.

**Four decisions, the first load-bearing.** (1) **Off by default.** A wall-clock cap cannot
distinguish a stalled run from a slow one making real progress, so any non-zero default would
eventually guillotine legitimate long work — the same regression shape the P52.3 reconcile caught
when the tool-failure breaker met the phased drive. Opt-in only. (2) **Per-`Run`, not global.** The
roadmap item worried a global cap would kill a long phased build mid-phase and suggested a per-phase
budget instead; per-`Run` scoping gives that for free, since the drive already runs each phase as its
own `Run`. (3) **Fatal to the drive**, unlike a context overflow (P47.2/P47.7) or a tool-failure
stall (P52.3), both of which reset to a fresh context and resume. Those are conditions a fresh
context genuinely clears; a wall-clock limit is an operator saying "stop after N minutes", and
resuming past it would defeat setting it. Pinned by a test asserting both `recoverPhase6Overflow` and
`recoverToolFailureStall` decline `ErrWallClockLimit` *and* consume no reset budget doing so.
(4) **Sub-agents inherit the bound whole** rather than a divided share the way the FIND-14 cost/token
floors work — spend is additive across siblings, elapsed time is not, and teammates run concurrently,
so "N minutes" means the same N minutes for each.

**The motivating surface was swarm, not cron.** The review that filed P52.15 implied unattended runs
were unbounded generally. Cron is not: it fires *shell commands* through `cronShellRunner`, which has
carried a `cronJobTimeout = 10 * time.Minute` all along — the timeout lives in
`internal/server/helpers.go`, outside `internal/cron/`, which is what made it easy to miss. Spawned
swarm teammates (`swarm/inprocess.go`, `cli/worker.go`) were the genuinely unbounded engine surface:
they get guaranteed floor shares of the USD and token budgets and nothing bounding duration, with no
human present to interrupt.

**One incidental fix found while wiring config.** `patchCost` splices in a freshly built `cost:`
block, so any key `buildCostBlock` doesn't write is *erased* from the user's file. Adding a cost key
without threading it through `CostPatch` would have made `aegis harden` silently delete a wall-clock
bound the user had set. Carried through with a regression test; `harden` still sets no wall-clock
value of its own, since it's an operator preference rather than a security control.

**P52.17 — closed as already-implemented; the item's premise was a review error.** It was filed on
the observation that the engine's P34.2 notice only detects a tool-incapable model *after* a turn is
spent. That is true of that notice — but it is lever (2) of two, and **lever (1) already does exactly
what the item proposed.** `Server.toolCallingWarning` (`internal/server/toolcalling.go`) runs the
smoke probe at **run start** against the resolved model and warns up front
(`internal/server/messages.go`), backed by `toolcallprobe.Gate`: a singleflight per-model verdict
cache that probes once per model per daemon and deliberately declines to cache an inconclusive
verdict. Warnings are bounded to once per session per model and re-fire on a model switch. All of it
shipped in `e1b55f1` with P34.2 itself. Its placement is also *better* than what the item asked for —
the item named three model-selection sites (daemon start, `PATCH /sessions/{id}`, the TUI picker);
run start is the single choke point downstream of all three, the only place the model is known after
the persona pin, the per-session override, and P30 routing have resolved, and the moment the model
loads anyway so the probe shares that cold load. The two residuals the item raised (verdict cache not
surviving a daemon restart; CLI `chat` excluded) were both explicit P34.2 decisions with recorded
rationale, and neither was reopened. Lesson recorded in the roadmap: an item asserting "X is not
done" needs the *absence* verified, not just the presence of a weaker mechanism nearby.

---

**Previously, same day:** **P52.12 and P52.13 shipped**, closing the P52.x review's Tier-3 work:
the phased drive now lives in the daemon where every client can run it, and a session can reach more
than one repository. Full suite green (61 packages, **including the three `internal/sandbox`
`/private/var` symlink failures previously carried as known-broken** — they were never a validator
bug: `ValidatePath` returns the symlink-*resolved* path by design, and the tests compared it against a
raw `t.TempDir()`, which on macOS is `/var/folders/…` for `/private/var/folders/…`. Fixed test-side
with a `tempRoot` helper, the same hermeticity problem P48.1 fixed for config), race detector clean on
every touched package, web UI rebuilt and re-embedded.

**P52.12 — lift the phased drive into the daemon (supersedes P50.5).** Every reliability mechanism
built for local models — fresh context per phase, the P47.9 hollow-body re-entry router, P50.1
backend liveness + resume-from-disk, P47.5b context escalation, the P39.7 no-progress guard — was
reachable only through `aegis chat --skill`. The TUI and web UI ran the single-context drive that the
phased drive exists *because* it fails (the P38.1 wall), which is the wrong default for the clients
most people use and especially wrong for the web UI, where a multi-hour build most wants to live: its
runs survive closing the tab, and `aegis chat` does not. Four parts:

1. **The machinery moved to `internal/drive`** (from `internal/cli/chat_phased*.go`) unchanged in
   behaviour. Nothing in it was ever CLI-specific — it is orchestration *above* `engine.Run`, and the
   engine, gate, tool registry and event plumbing were already shared.
2. **`PlanFor` reads the plan from the skill's own `phases:` frontmatter** instead of hard-coding one
   skill name, so `deep-research`, `latex-report`, `structured-build` and `documentation-as-code` can
   opt in without a code change. The built-in `threat-modeling` plan still wins for that one name: its
   per-phase prompts carry guardrails a frontmatter string cannot express (P47.3 no-self-verify,
   P39.14 anti-monolithic-write, framework-specific scaffolding) and every P38.1/P47.x live run was
   tuned against them — letting an edited SKILL.md silently replace them would be a regression wearing
   the clothes of a generalization.
3. **`POST /sessions/{id}/drive`** streams over the existing SSE seam, sharing the whole `streamRun`
   body with `POST /messages` — semaphores, spend caps, approvals, steering, checkpoints, persistence,
   usage accounting, detached-run buffering — with the drive as the single branch at the `eng.Run`
   call. Splitting the handlers would have meant two copies of ~300 lines of lifecycle, which is
   exactly how a daemon grows a drive that silently misses the cost caps. A skill with no phase plan
   is **refused**, not quietly run as a single-context turn: falling back would hand a caller that
   explicitly asked for a phased build the failure the phased build exists to replace, and silently —
   the symptom shows up hours later as a stalled run. `GET /sessions/{id}/drive` lists the drivable
   skills, resolved against the session's own workspace.
4. **Every client**: TUI `/drive <skill> <task…>` plus `/threat-model … unattended`, and a web UI
   **Drive** button beside the composer. `/threat-model` stays interactive by default — P47.10's
   reasoning that review between phases is *valuable* still holds; the defect was the absence of a
   choice, which forced anyone wanting an unattended build out of the TUI entirely.

**Two defects found while wiring it up, both invisible to the parts already built.** First, the
completion oracle had not been generalized with the plan: every phase resolves its files through
`LatestRunDir`, hard-coded to `.aegis/security/threat-model/<date>/` and keyed off a threat-model
sentinel file, so a frontmatter-declared plan resolved `""` forever — no phase could ever report
itself complete, and each would burn its whole turn budget before the drive moved on. Declared phases
would have "worked" in the sense that they ran. `RunDirResolver` now decides it with the plan: a
declared `run_dir:` glob resolves to its newest matching directory (the "each run scaffolds a fresh
dated directory" pattern, generalized), `threat-modeling` keeps its layout exactly, and anything else
treats the workspace root as the run directory. The end-to-end daemon test — a two-phase project skill
driven over real HTTP — fails against the unfixed code by taking 40 turns per phase instead of one.
Second, the TUI's resumable-run semantics: a drive is marked resumable so it outlives a dropped
connection, which would have broken interrupt (a resumable run keeps executing server-side when its
request context is cancelled), so the cancel handle the TUI stores now stops the run through the
daemon *first* and then closes the stream. Every existing cancel site — ESC, Ctrl+C, `/quit` — keeps
meaning "stop this run" without knowing a drive is behind it.

**Phase boundaries are now an operator-visible signal**, not just a log line: the drive writes
`phase N/M — <name>` / `complete` / `already complete on disk` to its notice stream, the daemon turns
each into an SSE notice, and the web UI renders notices at all for the first time (they were a
declared parity no-op). During a multi-hour build these are the only progress signal between tool
calls. The web UI also now distinguishes "the run started and the connection died" from "the daemon
refused the request" — only the first is worth reattaching to, and a drive makes the second a common
case, where reporting it as a lost connection would hide the actual reason.

**P52.13 — `workspace.additional_roots`: a session can reach more than one repository.** There was no
multi-root support at all — every workspace-confined tool validated against the single directory Aegis
was started in — which makes the cross-repo shape inexpressible: read research artifacts out of repo
A, write the formal document into repo B. Starting from a common parent works and is what people did,
but it widens confinement far past what the task needs and inflates the repo map with everything else
under that parent. `workspace.additional_roots` takes a list of `{path, writable}` entries; relative
paths resolve against the session workdir, and **roots are read-only unless explicitly marked
writable**, because the workflow's own shape is asymmetric — the research repo should not be
scribbled into just because it must be read.

**Two independent locks stand in front of every entry, and they are the point of the design.** The
config key joins `permission.*`/`sandbox.*`/`mcp.servers`/`notify.webhook`/`hooks` in the P27.1
frozen-from-untrusted-project set: a cloned repo nominating `/` or `~` as an additional root would
turn `read_file` into an arbitrary host read, which is precisely the silent widening that gate exists
for. And each root additionally needs its **own** `aegis trust` decision — an additional root does not
inherit the primary workspace's trust, since trusting the repo you are working in is not the same
decision as granting it a window into another directory on the host. `aegis trust --dir <path>` is new
for exactly this: an additional root usually has no `.aegis/config.yaml` of its own to review, so
there was no way to record a decision about it without cd-ing there first. Entries that are untrusted,
missing, not a directory, duplicated, or already nested inside the workdir are **dropped with a
warning** rather than failing the session — a stale root in a config should degrade to plain
single-root confinement, not stop work.

**The confinement check runs per root, against each root's own `EvalSymlinks` identity — never once
against a prefix covering the set.** That distinction is the whole security property: two roots under
a shared parent must not make that parent reachable, so a symlink out of root A into root B's *parent*
is refused even though it lands "between" two legitimate roots. `ValidatePathIn` also deliberately
drops the single-root form's lexical pre-check, which assumes the candidate was built by joining
against the root being tested — true only for the primary, and applying it per root would reject a
relative path naming a file in an additional root before its symlinks were ever resolved. A
one-element writable root set is bit-for-bit the old behaviour, which is what every session with
nothing configured gets.

The roots reach tools the same way the session workdir already does (`engine.Options.ExtraRoots` →
`tool.WithExtraRoots` on the call context → `effectiveRoots`), so no tool signature changed and
`resolveRead`/`resolveWrite` are the only new discipline: write access requires the landing root to be
writable. The daemon memoizes resolution per session workdir — it reads the trust store off disk and
stats every root, and `newEngine` runs per turn, per sub-agent spawn, and per debate round; restarting
the daemon is already how a trust decision is applied. Two call sites deliberately keep the
single-root form: `resolvePath`, for the handful of callers with no request context, and
`shellArgsStayInRoot`, where a path outside the primary root merely disqualifies a command from the
plan-mode read-gate *downgrade* — it still runs, under normal execute approval, so the conservative
answer is the correct one.

Previously, 2026-07-31 — **P52.1, P52.3, P52.4, P52.8, P52.10 shipped**, the second parallel
batch off the P52.x full-stack review and the one that **closes the review's open Tier-1 and Tier-2
build work**. Four file-disjoint lanes built concurrently, then reconciled — the reconcile pass found
a real cross-lane defect neither lane could see, described under P52.3 below. Full suite green (61
packages; the three pre-existing `internal/sandbox` `/private/var` symlink failures are unchanged and
unrelated), race detector clean on every touched package.

**P52.1 — the context window is now detected per model, not per server.** `contextwindow.go` resolved
**one** effective window against `cfg.Provider.Model` and `newEngine` handed that single number to
every run — but the model a turn actually runs on is resolved *per turn* (`resolveModel`: session
`/model` override > persona config override > persona file `model:` > global, plus `turnModel`'s
routing to `provider.small_model`). So the window the engine enforced and the model that had to live
inside it could be two different models, and both directions were wrong. A persona pinning a
larger-context model made the engine compact at 85% of a window smaller than the real one, burning
summarizer calls — minutes, on a local model — on a conversation that had room. A persona pinning (or
routing selecting) a *smaller*-context model was worse and is the failure this whole subsystem exists
to prevent: the engine believed it had headroom, never compacted, and Ollama silently dropped the
oldest tokens **including the system prompt** — the exact silent truncation `ollamainfo` was written
to catch, reintroduced through the per-session model path that postdates it. Detection is now keyed by
model: a `ctxWinEntry{win, src, final}` per model behind `ctxWinMu`, each carrying its **own** `final`
state, since a model that hasn't been loaded yet only yields a modelfile/default guess and must
re-detect independently of every other model after the first run loads it. The config-vs-served
reconciliation in `applyDetectedWindowFor` runs per entry — the configured `context_window` expresses
one desired window for the daemon, but what a *given* model is actually served is a property of that
model. The globally-configured model's entry deliberately stays in the existing
`ctxWin`/`ctxWinSrc`/`ctxWinFinal` fields rather than moving into the map: those are what `/status`
reports and what the daemon-wide summarizer is tuned to, both server-wide by construction, and a
reading for one session's persona-pinned model must not silently redefine what every other session
compacts against. `newEngine` resolves the window *after* `turnModel` has picked the model and
`maybeRefreshContextWindowFor` refreshes the model the finished run actually used — a turn routed to
the small model taught us nothing about the primary's allocation and everything about the small one's.
First-use detection for an unseen model is **synchronous** (5s bound): seeding from the global window
and correcting after the run is precisely the failure being fixed, since it would leave the pinned
model's *first* turn — the one carrying the full system prompt plus any skill body — believing it had
the primary's headroom. The output guard gets its own model's window too, since the verdict runs on
`provider.small_model`.

**P52.4 — `num_ctx` moved from adapter state to the request.** `s.adapter` is one shared adapter built
at daemon start; the native Ollama adapter carried `num_ctx` as **adapter state** and stamped it onto
every request, while the model is per-request. So a turn routed to `provider.small_model` asked Ollama
to serve that small model with the **primary** model's `num_ctx` — on VRAM-constrained hardware either
an oversized KV allocation for a model that doesn't need it, or eviction of the primary model to make
room, producing exactly the cold-reload churn between turns that `load_duration` telemetry was added
to make visible. `provider.Request` gained `NumCtx`; the adapter's value is now the *fallback* when a
request doesn't specify one, so nothing changes for any non-Ollama caller or for the CLI drive.
**The engine was deliberately not touched.** Rather than teaching `engine.Options` about `num_ctx` —
which the engine has no business knowing — the server wraps its shared adapter per run with a new
`provider.WithNumCtx` decorator, following the `Unwrap() Adapter` convention the retry and failover
decorators already use. The consequence is the point of shipping these two together: the window
*requested* and the window *enforced* now come from the same `effectiveContextWindowFor(model)` call
and cannot disagree. Sub-agent spawns, which can name their own model, get the same treatment.
`RaiseContextWindow` needed a decision: an escalation responds to an overflow that already happened,
while a request's `NumCtx` was computed *before* the run, so if the request simply won, escalating a
daemon-shared adapter would be silently undone by the next request. Escalations are therefore applied
as a monotonic **floor** over both request and adapter values — inert today (the only caller is the
single-model CLI phased drive, whose requests carry no `NumCtx`) and correct once P52.12 lifts the
drive into the daemon. Tests: per-model cache and cache-hit counting, per-entry reconciliation with
one model downgraded while another keeps config, non-authoritative re-detect after a run leaving the
global entry untouched, and end-to-end — a persona pinning `small:1b` produces a request with
`Model: small:1b, NumCtx: 4096` while unpinned turns still get the configured 32768.

**P52.3 — consecutive-tool-failure circuit breaker.** `IsError` was computed for every tool result,
emitted on the event stream, and then **never aggregated into anything** — no counter, no threshold,
no nudge, no abort. The engine's stall guards (P28.3 zero-tool, P34.1 empty-answer, P34.2
tool-call-as-text, P2.6 step-limit, the P39.8 summarizer latch) all key on something other than a
*failing* tool call, and the gap is structural rather than incidental: `loopDetector` matches a
repeating signature of tool name + canonicalized input, but the common small-model failure is a model
whose arguments *legitimately differ every turn* — call `edit_file`, get `old_string not found`, retry
with a slightly different `old_string`, fail again. Every signature is genuinely distinct, so the
detector never fires and the run burns all the way to `maxIterations` (default 40) producing nothing.
On a ~7 tok/s local model that is hours. None of the three budgets catch it either: `BudgetUSD` is an
explicit no-op for unpriced local usage, `MaxTokensPerRun` defaults to 0, and `maxIterations` is the
thing being burned. `internal/engine/toolfailure.go` now tracks two per-`Run` counters:
`allErrorRounds` (consecutive rounds where *every* result was an error — nothing is progressing) and
`sameErrorRounds` (consecutive rounds whose first failure carries the same normalized error text
regardless of arguments, which catches the same-error-different-args shape `loopDetector`
structurally cannot see). Normalization is deliberately shallow — trim and collapse whitespace,
nothing more — because stripping numbers or paths would start merging genuinely different failures,
and the strict counter already covers text that varies every time. At 3 rounds a corrective nudge is
injected in the existing `nudgeState` idiom, quoting the actual error and instructing the model to
re-read the file rather than re-guess arguments; it is bounded to one per run and registered so
`retractAll` strips it from the durable transcript like every other corrective. At 6 the run ends with
an error naming the repeated failure and tool, in the same shape as the loop-detector and budget
aborts. **One deliberate deviation from the roadmap:** only the *strict* counter can end a run. A
round that mixes a repeating failure with a succeeding call is the ordinary edit → `go test` →
still-fails → edit cycle, where the shell tool reports a non-zero exit as `IsError` with identical
text every round; killing a run that is actively writing files would be a far worse failure than the
stall it prevents. The secondary counter earns a nudge and nothing more. This closes the Tier-3
"task-failure halt" lead filed with P46.3, which had assumed it needed a persisted task boundary to
count against — it does not; the per-`Run` tool round is a perfectly good boundary for the failure
shape that actually occurs.

**P52.3 reconcile — the breaker would have been a regression for the phased drive.** Found in the
cross-lane pass, invisible to any single lane: `runPhasedSkillDrive` treats every engine error that is
not backend-down or a context overflow as **fatal to the whole drive** (`chat_phased.go`, three
sites — the content-phase loop, the phase-6 verify/quality loop, and the P47.9 hollow re-entry). So a
stall that previously burned to `maxIterations` and limped onward would now return a hard error and
kill an unattended run — re-introducing exactly the manual-re-invocation failure the P47.x/P50.x
batches exist to remove. The abort now wraps an exported `engine.ErrToolFailureLimit` sentinel so a
caller can classify the stall instead of pattern-matching a message, and the drive treats it the way
it treats an overflow: a resumable phase reset. A fresh context is also the *right* remedy here, not
merely a compatible one — the breaker fires when a model keeps re-guessing arguments, which it does by
reasoning from a context now dense with its own failed attempts, so dropping that context and
re-reading the on-disk `<!-- PENDING -->` files is a strictly better starting point than the one that
produced the failures. Two differences from the overflow path, both deliberate: it does **not**
escalate the serving window (the window is not what failed), and it keeps its **own** reset budget
(`maxToolFailureResets = 2`, tighter than `maxPhase6OverflowResets`) so a run that both overflows and
stalls cannot spend one budget on the other — and because an overflow is a mechanical limit a fresh
context genuinely clears, whereas every-tool-call-failing may be a real impasse worth stopping on.

**P52.8 — mechanical substance floor for threat-model content.** Nothing in the 15 `verify.py` checks
rejected vacuous content: a suite where every Evidence cell read `see code`, every Mitigation `TBD`,
and every category `None identified` passed all of them and got stamped. The P38.1 quality pass is the
intended backstop, but it is an LLM call and (per P52.12) CLI-only, so the TUI path had **no substance
gate whatsoever**. Four checks were added — 16 `evidence-cells-cited` (an Evidence cell that is *only*
a filename, with no line number, symbol, config key, or prose), 17 `no-placeholder-cells` (a
required-substantive cell that is *exactly* `TBD`/`N/A`/`see code`/…), 18 `none-identified-fraction`
(a table, or a file's threat-shaped tables in aggregate, that is essentially nothing but "none
identified"), and 19 `prose-sections-substantive` (a manifest `kind: "prose"` section below a minimum
length). They consume P52.7's `.scaffold-manifest.json` directly and reuse `find_heading` /
`section_region` / `region_substance` as-is; `scaffold.py` needed no change, because the manifest was
already built as a superset for this. Checks 1–15 and **their names** are untouched — `chat_phased.go`
routes on the literal string `finding-bodies-nonempty`, so a rename would silently drop P47.9's
hollow-body re-entry. Every threshold lives in one module-level `SUBSTANCE` dict with a matching CLI
flag, and an empty comma-list disables a rule outright. **The calibration deliberately under-flags**,
because a false failure costs a verify bounce and erodes trust in the whole suite: the
`None identified` cap sits at 0.95, so nothing below 100% ever fires and a component with 6 of 7
categories empty still passes; placeholder matching is exact rather than substring, so "TBD — owner to
confirm by Q3" is real content; `Anchor` is **not** an evidence column (the Key Components table's
Anchor is *supposed* to be a bare path); `Prerequisite` is not substance-checked (`None` is one of its
legal fixed values); `Description`/`Configuration` were dropped because a bare `N/A` is often the
honest answer; and Deployment Classification is exempt from the prose floor since its correct fill is
one of four fixed words. Double-reporting is suppressed by construction against checks 1, 14 and 15.
Verified against fixtures both ways: a legitimate suite gets `19 passed, 0 failed`, while a vacuous one
that passes checks 1–15 — the roadmap's premise, reproduced exactly and now asserted as a test — fails
4. All seven freshly-scaffolded frameworks add zero new failures on an unfilled scaffold. **Worth
recording: these scripts had no automated coverage at all before this.** No Python test existed
anywhere in the repo; the Go side only writes a *stub* `verify.py` (`chat_verify_test.go`) or checks
the real one materializes byte-identically (`embedded_test.go`). The new
`_verify_substance_test.py` (stdlib `unittest`, 15 tests) closes that. Its **leading underscore is
load-bearing**: `internal/skills/embedded.go` uses `//go:embed builtin`, a plain directory pattern,
which excludes `_*` and `.*` — the same rule that keeps `__pycache__` out of the binary — so it is
tracked source that never ships inside the skill. Confirmed empirically against a built binary.

**P52.10 — `latex_build` can resolve citations.** `latex_new_document` scaffolded a `biblatex`/`biber`
block into every preamble, but `latex_build` only ever ran the LaTeX compiler in a plain multi-pass
loop — there was **no `biber`/`bibtex` invocation anywhere in the tool**. A user who uncommented the
block and added `references.bib` got a PDF full of `[?]` marks and no indication why, which made the
bibliography support purely decorative for exactly the citation-heavy security writing it was there
for. A tri-state `bib` input (omitted = auto-detect, true = force, false = suppress) now drives a real
bib pass. Auto-detection walks the `.tex` **and its transitive in-workspace includes** for
`\addbibresource`/`\addglobalbib`/`\bibliography{`, with comments and verbatim blocks stripped first —
so the commented-out scaffold does *not* trigger a pass but uncommenting it does. Which tool to run is
decided from the **generated artefacts** rather than the source, since that is what the tool actually
consumes: a `.bcf` means `biber`, otherwise an `.aux` containing `\bibdata` means `bibtex`. After a bib
run at least two further LaTeX passes are forced regardless of `runs`. **Confinement was the hard
part and is the reason this had to follow P52.2.** What P52.2 delivered is a *static scan of the LaTeX
source* — `openin_any` is inert on TeX Live 2026 — and it covers `\addbibresource`/`\bibliography`
**arguments** but not `biber`, which resolves resources declared in the generated `.bcf`. So this item
adds two subprocesses outside the existing confinement, and a second layer was required: a new
`checkLatexBibConfinement` runs **after pass 1 and before the bib binary is even looked up**, parsing
the `.bcf`'s datasource elements or the `.aux`'s `\bibdata`/`\bibstyle`, following `\@input` chains
into nested `.aux` files (capped at 64 files / 4 MiB), and validating every name through
`sandbox.ValidatePath` against **both** directories the tool could resolve it from — escaping from
either is refused, and the bib binary never runs. Remote `scheme://` datasources are refused outright:
a URL is not a path the validator can reason about, and `latex_build` has no network capability.
`biber` also gets `--noconf`, because its first config location is `biber.conf` in the cwd — inside the
model-writable workspace. The P52.2 traversal was factored into a shared `latexWalkSources` so the
confinement scan and bib auto-detection cannot drift on which files are in play; the scan's behaviour
is unchanged and all its tests pass untouched. **`latexmk` was evaluated and rejected**, against the
roadmap's stated first preference. Its rc-file objection is answerable (`-norc` suppresses the
arbitrary-Perl `./latexmkrc` evaluation). The decisive objection is *where the check has to sit*:
latexmk decides for itself, mid-run, when to invoke biber over the `.bcf` it just generated, and
exposes no seam between those two events — its only interposition point is the `$biber` command
string, so honouring the check would mean shipping a separate wrapper executable that re-implements it
out of process. Option 2 was implemented instead. Two smaller defects in the same function were folded
in: the multi-pass loop now breaks on the *first* failing pass, so `lastLog` and `runErr` always
describe the same pass (previously a failure on pass 2 was overwritten by a successful pass 3 and
reported as `BUILD SUCCESS`), and `parseLatexLog` counts dropped warnings as it goes against a named
`latexMaxWarnings` const instead of re-deriving the count from `len(s.warnings) == 15` after the
`… and N more` line may already have been appended. ~25 new tests use stub `xelatex`/`biber`/`bibtex`
scripts on a temp PATH, so **no TeX installation is required**; one live test keeps the pre-existing
skip-if-absent idiom, and was verified against a real toolchain — before, `Citation 'aegis2026'
undefined` + `Empty bibliography`; after, `BUILD SUCCESS (pdflatex, 3 pass(es))` with
`bibliography: biber ran over main.bcf`. **Residual gaps, stated plainly:** there is no iteration to
convergence (compute pass → bib → two more passes is right for the ordinary document but not one
needing a fourth LaTeX pass — it fails loudly via LaTeX's own `Rerun to get cross-references right`
warning, and the `runs` cap was raised 3 → 4 so the model has a real fix); the bib scan is a static
scan on an unconfined process, like P52.2, so a TOCTOU swap of the `.bcf` between scan and exec is not
modelled; and a workspace-local `.bst` is path-validated but not otherwise sandboxed, judged
sufficient because BibTeX's style language is a restricted VM that can only write the `.bbl`.

**Defect follow-up, same day — both batch-2 leads closed, and the suite is green for the first time
in weeks.** The batch filed two leads and left several residual gaps; investigating them produced
three fixes and one correction to my own reporting.

*The summarizer was tuned to the wrong model's window* — the second half of P52.1. Compaction prefers
`provider.small_model` (`compModel`), but `compaction.Options.ContextWindow` was fed the **global**
model's window, and `setWindowLocked` only retuned the summarizer from the global model's entry. So on
any setup with a small model configured, the summarizer's own request could exceed what Ollama serves
it and be silently front-truncated — producing the broken/empty summary that P39.8's latch exists to
stop looping on. The summarizer is now built from `effectiveContextWindowFor(compModel)`, retuned only
by that model's entry (never by an arbitrary session's persona-pinned model — the compactor is
daemon-wide), and given its own `num_ctx` via `provider.WithNumCtx`. One consequence needed closing:
compaction runs inside the engine rather than through `newEngine`, so it never reports a run model and
its entry would have been resolved once at startup — possibly from a not-yet-loaded modelfile guess —
and never corrected; the post-run refresh now covers `compModel` explicitly when it differs from the
run model. `compaction.Summarizer` gained a `ContextWindow()` accessor so the invariant is assertable.

*Sub-agents had no per-turn proactive compaction* — `engine.Options.ContextWindowTokens` was left at
0, the engine's "disabled" value, so the 85%-fill check and its nothing-left-to-compact notice never
ran for a spawn however long it grew. Fixed by feeding it the `spawnWin` already resolved for P52.4.
**A correction to how this was first reported:** the lead said a spawn had "no proactive compaction at
all", which is wrong. `engine.Run` calls the compactor **unconditionally at entry**
(`engine.go:345`), independent of `ContextWindowTokens` — so spawns always had that pass, gated by the
summarizer's own budget. The first regression test written for this passed against *unfixed* code for
precisely that reason, and only counting calls (1 = entry only, ≥2 = the per-turn check also ran)
distinguishes the two. The test now asserts the count and was verified to fail against the unfixed
code.

*The three long-standing `internal/sandbox` failures were a real, fixable test defect, not host
noise.* `TestValidatePathBasic`/`Absolute`/`NewFile` compared `ValidatePath`'s result against a raw
`t.TempDir()`, but `ValidatePath` returns the symlink-**resolved** path by design — that is the path a
caller must open to avoid a TOCTOU swap of a link beneath it — and on macOS `/var` is a symlink to
`/private/var`. Fixed test-side with a `tempRoot(t)` helper (the same hermeticity shape P48.1 fixed
for config); teaching `ValidatePath` to return the unresolved path would have defeated its purpose.
**`go test ./...` is now fully green — 62 packages, zero failures.** That matters beyond tidiness:
these three had been written off as environmental for weeks, and P51.1 (a shipped sandbox backend that
executed nothing at all on macOS 26) hid behind exactly that assumption. A permanently-red suite
trains everyone to skim past the failure that is real.

A pre-existing `gofmt` misalignment in `server.go`'s struct fields was cleared at the same time, so
`gofmt -l internal/` is clean.

Previously, 2026-07-30 — **P52.2, P52.5, P52.6, P52.7, P52.9 shipped** (plus **P52.11**, the
`documentation-as-code` skill, committed there), the first parallel batch off the P52.x full-stack
review. Five file-disjoint items built concurrently, then reconciled.

**P52.2 — `latex_build` no longer hands the host filesystem to the TeX compiler.** A `.tex` file the
model itself authored could `\input{~/.ssh/id_rsa}` and have the contents typeset into the output
PDF: the compiler was invoked with no `-no-shell-escape` and an unset `cmd.Env`, inheriting a host
TeX config where `openin_any = a`. Every other file-touching builtin routes through
`sandbox.ValidatePath`; this one validated the `.tex` path and then gave a subprocess the whole disk.
**The fix the roadmap prescribed turned out to be a no-op, and that is the main finding here.** The
roadmap asserted `openin_any=p` is "honoured by TeX itself, so the hardening holds regardless of the
host's `texmf.cnf`". As of TeX Live 2026 that is false: `texmf-dist/web2c/texmf.cnf` documents
`openin_any` as having **no effect** — `kpse_in_name_ok` and friends always return true — because
"there were obscure ways to inject arbitrary input from the supposedly-forbidden areas, so it gave a
false sense of security" ([tex-live thread, Dec 2025](https://tug.org/pipermail/tex-live/2025-December/051965.html)).
Confirmed empirically: with `openin_any=p openout_any=p shell_escape=f` and `-no-shell-escape`, an
`\input` of an absolute out-of-workspace path is still opened (the run log records it) and its text
still lands in the PDF's content stream. So the three-line fix alone would have shipped as security
theatre and failed its own regression test. The invocation is hardened anyway (`-no-shell-escape`
first so it cannot be swallowed as a filename, `openin_any`/`openout_any=p`, `shell_escape=f`,
inherited values *stripped* rather than shadowed, applied on every pass) — that is still effective on
TeX Live ≤2025 and MiKTeX — **and** the source is now scanned before the compiler runs for file
references resolving outside the workspace root: `\input` (braced and TeX's brace-less form),
`\include`, `\InputIfFileExists`, `\openin`, `\lstinputlisting`, `\verbatiminput`, `\includegraphics`,
`\includepdf`, `\addbibresource`, `\bibliography`, `\import`/`\subimport`, `\graphicspath` roots, and
local `.sty`/`.cls`, with `~` expanded and includes followed transitively so a one-hop bypass through
a chapter file is caught (capped at 128 files / 4 MiB each). TeX comments and
`verbatim`/`lstlisting`/`minted`/`alltt` blocks are stripped first, so a security report that *quotes*
`\input{/etc/passwd}` still builds. A latent bug surfaced while testing: `sandbox.ValidatePath`'s fast
pre-check compares against the **unresolved** root, so on macOS — where any `/tmp` or `/var` workspace
is reached through a symlink — validating an already-resolved `/private/...` path flagged the
document's own chapters as escapes; the scan now resolves both sides through `EvalSymlinks` first.
Verified against a real compiler both ways: the escaping document is refused and leaks nothing, and an
ordinary multi-file build — the full `latex_new_document` template, `output_dir` and all — still
compiles clean. **Residual, deliberately not closed:** the scan is a heuristic on a hardened process,
not a sandbox. Filenames a document builds from macros at run time (`\input{\somemacro}`) cannot be
resolved statically and are allowed by design; the durable fix is running the compiler under
`internal/sandbox`, filed as a Tier-3 lead rather than taken as a drive-by change, since P51.1 had
just finished proving the seatbelt profile was executing nothing at all on macOS 26.

**P52.5 — latch the `think`-rejection verdict.** The P38.5 retry for models that 400 the instant
`think` is sent ("does not support thinking") was correct but stateless: `a.think` was never updated,
so the adapter re-sent `think` on the very next request, took the same 400, warned again, and retried
again — for every turn of the session. A 40-iteration local run paid 40 pointless 400s and buried real
signal under 40 identical warnings. `Stream` now consults a latch before the first attempt and skips it
once a model has proven it rejects the parameter; the latch is written only after the think-omitted
retry has actually *succeeded*, and the warning is gated on the same `LoadOrStore` so it fires exactly
once per model even when `Stream` is entered concurrently by several sessions against the shared daemon
adapter. It is keyed by `req.Model`, not held per-adapter: one daemon adapter serves a mix of models,
and latching adapter-wide on one model's rejection would silently strip `think` from a sibling that
supports it. One behaviour change beyond the spec: the warning now fires only on a *successful* retry,
so a retry that also fails surfaces the raw error rather than a misleading "retried without it".

**P52.6 — synchronize `RaiseContextWindow`.** It mutated `numCtx` with no synchronization while
`doChat` read it on every request. The doc comment was honest that this was safe only because the sole
caller was `internal/cli/chat.go`, a single-session CLI process — an invariant that dies the moment
**P52.12** lifts the phased drive into the daemon, where `s.adapter` is shared across every concurrent
session and one session's context escalation becomes an unguarded write racing every other session's
`Stream`. `go test -race` could not have caught it: no existing test drove the daemon and the
escalation path together. `numCtx` is now behind an `RWMutex` taken by both `RaiseContextWindow` and a
new `contextWindow()` helper that `doChat` reads through, and the stale caveat is replaced by the
actual guarantee — escalation stays monotonic, so a concurrent raise can only ever be observed as a
larger window, never a shrink. Landed ahead of P52.12 so the structural change need not also carry a
concurrency fix. The new `-race` test (32 concurrent escalations against 32 concurrent `Stream` calls)
reports the race verbatim against pre-fix code — write at `RaiseContextWindow` against read in
`doChat` — and is clean after.

**P52.7 — suite-wide hollow-body check.** P47.9's `finding-bodies-nonempty` proved the failure real (a
weak model deletes a `<!-- PENDING -->` marker and writes nothing, leaving a heading over blank space —
structurally intact, substantively blank), but it only looked inside `### FIND-##` blocks: an empty
Deployment Classification, Security Infrastructure Inventory, PASTA stage or Executive Summary all
passed `verify.py` clean. `scaffold.py` now records every site it marked — file, enclosing heading,
heading level, table columns — into a hidden `.scaffold-manifest.json` in the run directory, derived
from the *built content* rather than by instrumenting the builders, so any future skeleton section is
covered with no second place to keep in sync. A new check, `section-bodies-nonempty`, asserts against
it, converting the shipped property *"no PENDING marker remains"* into the one actually wanted:
*"every site that had a marker now has substance"*, with `file:line` per failure. A 5-section hollow
suite that scored `14 passed, 0 failed` before now reports 5 failures across 4 files. Existing
behaviour is preserved throughout: the guidance-comment / `---` rule / bare-table-separator exclusions
are unchanged, an unfilled marker is still reported once (by check 1, never twice), and a suite
scaffolded before this — or one with a corrupt manifest — degrades to the old behaviour rather than
failing, since resumed runs exist in the wild. **Deviation from the roadmap, deliberate:** the item
said to *generalize check 12*; instead check 12 keeps its name and check 15 is new, because
`chat_phased.go`'s `contentSubstanceChecks` routes on the literal string `finding-bodies-nonempty` to
send hollow findings back through the findings phase (P47.9) — renaming would have silently dropped
that routing. The two never overlap: check 12 owns model-authored `####` subsections inside finding
blocks (never scaffolded, so never in the manifest), check 15 owns scaffolded headings suite-wide. The
sidecar lives in the run directory and uses the `.json` convention `.quality-stamp.json` established,
so no suite scanner sees it and the built-in-skill self-heal path (which only refreshes
`.aegis/builtin-skills/`) cannot disturb it. The manifest is deliberately a superset of what this check
needs so **P52.8**'s substance floor can consume it directly — `kind`/`columns` locate every scaffolded
table by real column name, `kind: "prose"` entries are exactly the narrative sections a length floor
applies to, and `manifest_version` allows a schema bump.

**P52.9 — `yaml_validate`.** Aegis had no YAML tooling at all, yet YAML is a first-class deliverable in
two shipped flows: `inventory.yaml` in the threat-model suite, and the `documentation-as-code` skill's
`slides` family, whose entire output is a `.yaml` file. Both were edited as opaque text via `edit_file`,
so a broken indent stayed invisible until a downstream consumer (`inventory.py --check`, a deck
renderer) failed with an error far from the cause — several turns of localization on a slow local
model, which is exactly the budget P47.x/P50.x exist to protect. The new tool (`CapRead`, deferred,
`internal/tool/builtin/yaml.go`) parses through the existing `go.yaml.in/yaml/v3` dependency — **no new
dependency** — and returns either the parse error with its line and a `>`-marked ±2-line excerpt, or a
compact outline of top-level keys with each value's kind and line number. The outline is the point: it
makes the tool a cheap structural probe usable *before* an edit, not just a post-hoc check. It decodes
document-by-document rather than via `yaml.Unmarshal`, which silently ignores everything after the
first `---` and would call a file with a broken second document valid. Paths route through the same
`sandbox.ValidatePath`/`effectiveRoot` confinement as every other file builtin. One limitation is
deliberate and visible in the output: `go.yaml.in/yaml/v3` never exposes the problem mark's column
(`parser.fail` emits only `line N`), so the tool says so plainly and leans on the excerpt rather than
inventing a column that would misdirect on exactly the indentation bugs it exists to catch. Registered
deferred — the two workflows that need it are skill-driven and can name it, so `tool_search` covers
discovery without every unrelated turn paying the schema cost.

**P52.11 — `documentation-as-code` skill** shipped 2026-07-30 and is committed here (see the roadmap
entry for its design and its confidentiality boundary).

Previously, 2026-07-30 — **P51.1 shipped**: the macOS OS-sandbox backend (`sandbox: os`,
seatbelt) ran **no commands at all** on macOS 26 — `/bin/sh` took SIGABRT during exec with no
diagnostic beyond the signal, surfacing as two failing `internal/sandbox` tests that looked like
host noise. The cause was the P27.18 read confinement: `(deny file-read*)` also denies reading the
**root directory itself**, and resolving any absolute path walks `/`, so the shell died before it
started. The same bisect turned up two adjacent gaps — `/tmp`, `/etc`, `/var` are symlinks into
`/private/*` and seatbelt checks the read against the *symlink* before following it, so allow-listing
only the `/private/*` target left `cat /etc/hosts` and `> /tmp/x` failing with EPERM; and `/bin/sh`
reads `/private/var/select/sh` for its shell personality, printing an `Error opening` line on every
command. `seatbeltProfile` now carries five built-in read allowances: `(literal "/")`, the three
symlink aliases as **literals** — a `(subpath "/")` would grant read of the entire filesystem — and
`(subpath "/private/var/select")`. They are kept out of `defaultOSReadPaths`, which is shared with
bwrap (where `/etc` and `/var` are real directories) and renders every entry as a `(subpath ...)`.
Confinement is unchanged and re-verified by hand: `$HOME`, `~/.ssh`, `/private/var/db` and writes
through `/etc` all stay denied, and `(literal "/")` discloses the root directory's entry names only.
New tests pin the five allowances and reject `(subpath "/")`, plus an integration test asserting a
trivial command actually runs and that `/etc` is readable but not writable. Previously,
2026-07-30 — **P50.1-P50.4 shipped**, the phased-drive determinism & resilience
batch prompted by the 2026-07-30 FirewallRiskRater run (which *did* reach a verify-clean,
quality-stamped suite unattended, but surfaced three concrete weaknesses). **P50.1 — backend liveness +
resumable reset:** the run's real stall was Ollama dying mid-phase, which the retry decorator can't
recover (it only covers a *synchronous* Stream failure before tokens flow), so the drive died with a
half-built phase. A dead/unreachable backend is now classified (`provider.IsBackendUnavailableError`:
transport refused/reset + the runner-crash stream signals, excluding overflow/rate-limit/cancel) and
treated like a context overflow — *resumable* — but gated on a new adapter liveness probe
(`provider.HealthChecker`, a GET /api/version on Ollama, reached via the unwrapping
`provider.CheckBackendHealth`): the drive **waits** for the server to return (bounded, default 10 min,
opt-in `AEGIS_OLLAMA_AUTOSTART=1` best-effort `ollama serve`), then resets to a fresh context and
resumes from the on-disk `<!-- PENDING -->` files. Wired into the content-phase loop, the phase-6
verify/quality loop, and the P47.9 re-entry. A follow-up closes the classifier's remaining hole: the
Ollama adapter emitted a *mid-stream transport read failure* — connection reset / unexpected EOF, the
signature of `ollama serve` being killed or crashing **while tokens are streaming** — as a bare error
that `IsBackendUnavailableError` could not classify, so the drive aborted instead of waiting. That
path now wraps the failure as a `provider.NewTransportError` like the synchronous `doChat` counterpart
does, and it is the more common of the two on a slow local model whose per-turn stream is long.
**P50.2 — deterministic ID canonicalizer:** a new bundled
`normalize_ids.py` (sibling to `inventory.py`) strips invented `T<n>.<suffix>` threat IDs back to the
bare `T<n>` the analysis defines and renumbers `FIND-##` to a gapless sequence, rewriting the coverage
table and every `Related Threats` reference in lockstep — the root cause of both the invented-ID verify
bounce and the quality-pass hand-renumber regression. It is idempotent and runs as a deterministic
pre-verify pass every phase-6 round, so ID drift is auto-fixed by a script instead of a model turn.
Validated end-to-end on the real FirewallRiskRater suite: injecting the two live-run defects failed 3
checks, and one normalize pass restored 14/14 clean and was idempotent. **P50.3 — quality-pass
regression guard:** the P38.1 quality pass could edit a mechanically-clean suite into a broken one
(the duplicate `FIND-07`). The drive now snapshots the suite at the moment the mechanical checks first
pass (a known-clean state) immediately before the quality pass; if the pass regresses it and the
bounded fix rounds can't heal it, the drive **rolls back to that snapshot** and stamps it rather than
shipping a regression — so the quality pass can only improve or no-op. The quality prompt is also told
not to hand-renumber IDs (P50.2 owns that). **P50.4 — live progress heartbeat:** a background ticker
logs the current phase/turn/elapsed/pending every 30s during a long turn, and each turn boundary logs a
structured progress line, so a hung phase (or a dead backend, before P50.1's wait kicks in) is
observable instead of invisible until the run ends. **P50.5** (wire the phased drive into the TUI
`/threat-model`) stays a Tier-3 lead — it revisits the P47.10 documented deferral and awaits a concrete
interactive-user need. Full `go test ./internal/cli/... ./internal/provider/...` green; new tests cover
the classifier, the Ollama probe + capability unwrap, the backend-recovery verdicts (recover/give-up/
not-handled/cancel), the mid-stream connection drop (a hijacked connection writes a truncated chunk
then closes uncleanly, and the resulting event must classify as backend-unavailable), the snapshot
round-trip, and the heartbeat tracker. See below. Previously,
2026-07-30 — **P47.4 and P47.9 shipped**, closing the P47.x phased-drive stability
batch. **P47.4** makes the phased threat-model drive's in-phase continuations **near-stateless**: instead
of appending each continuation to an ever-growing conversation (where every re-read of the ~400-line
findings file is retained for the rest of the phase and peak context climbs cumulatively), each turn now
resets to a fresh `[system + continuation]` context and re-reads only what it needs from disk — the
always-on form of the P47.2 on-overflow reset, capping a phase's peak context at ~one turn's reads.
`AEGIS_PHASE_CONV=growing` restores the old behaviour for comparison. **P47.9** routes a content-substance
verify failure — empty finding bodies (`finding-bodies-nonempty`) or a mis-filed coverage row
(`coverage-matches-related-threats`) on a hollow resume — back through the content phase that **owns** the
file (findings), re-driving it in its own bounded near-stateless loop with the phase's own authoring frame
and turn budget, instead of dumping all that authoring on the bounded phase-6 verify-fix loop where one
large fill overflowed (2026-07-27, FirewallRiskRater). Both were built ahead of their measure-first
triggers and ship with escape hatches (the env var above; P47.9 falls back to the generic fix loop if
re-entry can't clear the check), so a live run can still measure whether they earn their keep. See below.
Previously, 2026-07-29 — **P49.1 and P49.2 shipped** (the buildable head of the P49.x repo-map /
index enrichment batch: the repository map now carries **import/dependency edges** — `internal/repomap`
extracts per-file imports for Go/Python/JS-TS/Rust/Ruby from the same read `Build` already does, resolving
module-local and `./`/`../` specifiers to repo-relative paths (stdlib/third-party stay bare tokens), rendering
a compact `→ a, b` line *after* symbols so a tight byte budget drops edges before symbols, and bumping the
cache-schema to v2 so edge-less v1 caches rebuild (**P49.1**); and a new **deferred `repomap` builtin tool**
lets the model pull that structure on demand — `action:"map"` (whole map at a large budget, optional path
glob), `action:"skeleton"` (one file's symbols+imports without a `read`), `action:"importers"` (reverse
blast-radius query over the edges) — costing nothing until invoked (**P49.2**). The remaining P49.x items —
**P49.3** (LSP-backed symbol precision) and **P49.4** (LLM concept nodes) — stay Tier-4 measure-first and were
deliberately **not** built: they unlock only if the structural tier fails to close the discovery gap on a live
run. See below. Previously, 2026-07-27 — **P47.10 resolved** (CLI/TUI `/threat-model` parity — decided as
documentation, option b: the divergence is intentional since an interactive TUI user is present to steer,
so `/threat-model`'s help and the threat-modeling README now state it is interactive-by-design and point
to `aegis chat --skill threat-modeling --mode build --yes` for the unattended phased drive; no
`/threat-model --auto` was built — see below). Previously, 2026-07-27 — **P47.6 shipped** (drive model-selection guidance, doc-only: a new
"Driving the build on a local model" section in `internal/skills/builtin/threat-modeling/README.md`
documents the throughput/looping tradeoff — a small "fast" active-parameter MoE like `a3b` loops more
on self-verification and costs more turns than a steadier `-deep`/larger model, though both now finish
since the P47.1-P47.8 code fixes made the drive resumable regardless of model; the optional startup-hint
half of the item is deferred as speculative — see below). Previously, 2026-07-27 — **P48.1 shipped** (config-test hermeticity: four `Load()`-based tests in
`internal/config/config_test.go` now call `redirectConfigDir(t)` so they assert built-in defaults / env
overrides against an empty temp config dir instead of the developer's real `~/.config/aegis/config.yaml` —
fixing a standing local failure of `TestOutputGuardDefaults` on a machine that disables the output guard,
and closing the latent same-class trap in `TestEnvOverride`/`TestEnvBaseURL`/`TestEnvOverrideServerLimits`
that passed only by env-override luck — see below). Previously, 2026-07-27 — **P39.10 and P39.11 documented + regression-tested** (backfilling the
remaining P38.1 debt: the two 2026-07-23 `chat --skill`-CLI fixes that shipped on `tier3-batch` but never
got a release note or tests — builtin skills now materialize into `<cwd>/.aegis/builtin-skills` so the
sandboxed file tools can reach `recon.py`/`scaffold.py` (**P39.10**), and the drive-completion oracle
skips that materialized skill source so its example `<!-- PENDING -->` markers no longer keep the drive
from ever converging (**P39.11**) — see below). Previously, 2026-07-27 — **P47.5, P47.7, and P47.8
shipped** (the next three P47.x phased-drive stability items: the phased threat-model drive now auto-sizes the Ollama serving window
to the model's recommended max up front and escalates `num_ctx` toward the model max on a context
overflow — removing the manual `AEGIS_PROVIDER_CONTEXT_WINDOW` bump the 2026-07-24 run needed
(**P47.5**); a context overflow during the phase-6 verify/quality remediation loop now resets to a
fresh context and retries instead of aborting the whole drive on the raw error (**P47.7**, the
phase-6 parity for the shipped P47.2 content-phase reset); and both phase-6 prompts now carry the
P39.14 anti-monolithic-write guardrail so the drive stops trying to fill many empty sections with one
truncating whole-file `write_file` (**P47.8**) — all three from the 2026-07-27 FirewallRiskRater run
that validated the ec0127c hollow-report checks — see below). Previously, 2026-07-24 —
**P47.3 shipped** (the two large content-phase seeds and the shared
in-phase continuation prompt of the phased threat-model drive now explicitly tell the model not to
re-audit already-filled files or recompute STRIDE/coverage counts by hand to self-check — the exact
in-phase token-burn that drove both context overflows on the 2026-07-24 FirewallRuleAnalyzer run,
work the deterministic phase-6 verifier already owns — the third P47.x phased-drive stability item,
cutting how often P47.1/P47.2 must act — see below). Previously, 2026-07-24 — **P47.2 shipped** (a
context-overflow error mid-phase now resets the phased threat-model drive to a fresh context and
retries the phase from disk instead of aborting the whole drive — the second P47.x phased-drive
stability item, the residual-recovery complement to P47.1's compaction — see below). Previously,
2026-07-24 — **P47.1 shipped** (proactive per-turn
compaction wired into the CLI `chat --skill` drive engine — the head of the P47.x phased-drive
stability batch, which alone would have prevented both context-overflow aborts on the 2026-07-24
FirewallRuleAnalyzer run — see below). Previously,
2026-07-24 — **P39.12, P39.13, P39.14, and P39.15 shipped** (threat-model drive robustness,
from the P38.1 full-stack test vs FirewallRuleAnalyzer on qwen3.6:35b: a 30-minute default response-header
timeout, a 1500-line default cap on `read_file`, a hard one-section-per-`edit_file` rule against monolithic
writes, and a final quality-and-sanity pass after mechanical verify — see below). Previously,
2026-07-23 — **the Tier 3 batch shipped: P40.3, P40.4, P40.7, P40.9, and P45.2** (full-text
transcript search, an experimental opt-in kitty-graphics image tier, shared form-panel chrome extraction,
inline mermaid-diagram ASCII rendering, and hunk-level agent-vs-external change attribution — see below).
Earlier the same day: **P40.1, P40.2, P40.5, P40.6, P40.8, P44.1, and P45.1 shipped** (the
parallelizable Tier-2 batch: the five-item TUI/UX set — resizable panes, consistent hjkl/g/G navigation, auto
dark/light detection, a contextual per-pane footer, and LaTeX→Unicode math rendering — plus two independent
hardening items: bundled-skill-asset admission scanning and worktree dirty-file replication — see below).
Earlier the same day: **P46.1, P46.2, and P46.3 shipped** (the codex-build workflow-discipline
track: per-task file-write scope enforcement, a pre-commit test gate on `git_commit`, and a `structured-build`
skill packaging both into a one-task-one-commit workflow — see below). Previously the same day: **P41.1 shipped**
(compaction's flat chars/4 token estimate replaced with the engine's script-aware one via a new shared
`internal/tokenest` package — see below). Previously,
2026-07-22: **P43.1 shipped** (debate concession-detector negation blindness, found
examining `internal/debate`/`internal/swarm` reliability — see below). Earlier the same day: **P42.1 and
P42.2 shipped** (workspace-trust and capability-spoofing gaps in `internal/plugins`, found by a scoped
security self-review — see below). Earlier still: **P39.7 shipped** (no-progress guard on the `--skill`
drive loop — see below). Previously, 2026-07-21: **P38.6 and P38.7 shipped** (the two actionable engineering
findings split out of the P38.1 conformance re-test — see below). Earlier the same day: **P39.1, P39.2, and
P39.4 shipped; P39.3 spiked and closed NO-GO** (all from a local-14b-model harness-improvement research pass
— see [roadmap.md](roadmap.md)).

**P47.10 — CLI/TUI drive-to-completion parity for `/threat-model` (resolved as documentation).** The
unattended phased drive-to-completion (fresh context per phase, PENDING oracle, auto verify + quality
pass) lives only in the CLI `runPhasedSkillDrive`; the TUI `/threat-model` seeds the skill into the
normal chat loop and stops at the model's first yield, so the two surfaces diverge. Filed as a
decide-not-assume item. **Decision (user, 2026-07-27): option (b) — document the difference; the
divergence is intentional** (an interactive TUI user is present to steer, and reviewing between phases
is the point of the interactive surface). Shipped as docs only, no behavior change: `/threat-model`'s
`detailedHelp` (`internal/tui/commands.go`) now says it is interactive-by-design and points to
`aegis chat --skill threat-modeling --mode build --yes` for the hands-off build, and the threat-modeling
README's "Driving the build on a local model" section documents the CLI-unattended vs TUI-interactive
split. Option (a) — wiring the phased drive behind a `/threat-model --auto` flag — was explicitly **not**
built. `go build ./...` + `go test ./internal/tui/... ./internal/skills/...` green.

**P47.6 — drive model-selection guidance (doc-only).** The self-verification looping that drove the
context growth on the 2026-07-24 FirewallRuleAnalyzer run traced proximately to the drive model: a
small "fast" active-parameter MoE (`a3b`, ~3B active) loops more — re-auditing already-filled files
and recomputing STRIDE/coverage counts by hand — than a steadier `-deep` variant or a larger dense
model, so it burns more turns and wall time to reach the same verify-clean suite. The P47.1-P47.8 code
fixes make the drive converge *regardless* of model (proactive compaction, on-overflow phase reset,
window auto-escalation, the `noSelfVerifyInstruction` guardrail, phase-6 overflow recovery), so this is
a throughput/looping mitigation, not a correctness gate. Shipped as a "Driving the build on a local
model" section in `internal/skills/builtin/threat-modeling/README.md` — the natural home because it is
guidance for the *user* choosing which model to point the drive at (the driving model can't reselect
itself), not skill instructions the model reads. The optional second half of the item — a startup hint
when a small MoE is the configured drive model — is deferred as speculative until a user actually hits
the tradeoff; the doc note is the primary deliverable, and the code fixes address the mechanism for
every model. No product code; README lives under the recursive `//go:embed builtin` pattern, so
`go test ./internal/skills/...` still passes.

**P48.1 — isolate config tests from the developer's real `~/.config/aegis/config.yaml`.**
`TestOutputGuardDefaults` called `config.Load()` without the `redirectConfigDir(t)` isolation its sibling
tests use, so it read the developer's real user config; on a machine whose config sets
`output_guard.enabled: false` (the common local setting) it failed its "defaults to true" assertion —
`Load()` had correctly applied the user layer, but the test meant to check the *built-in default*. It passed
in CI only because CI has no user config. Three sibling `Load()`-callers had the same latent gap, passing
today only because an env override dominated the leaked user value: `TestEnvOverride`, `TestEnvBaseURL`,
`TestEnvOverrideServerLimits`. Fix: `redirectConfigDir(t)` (which redirects `HOME`/`XDG_CONFIG_HOME`/`APPDATA`
to an empty temp dir) is now the first line of each, making every `Load()`-based config test hermetic
regardless of the developer's environment. Test-only change, no product code; `go test ./internal/config/...`
now passes on a customized dev machine, not just in CI.

**P47.5 — right-size the per-phase context window up front and auto-escalate on overflow.** The
2026-07-24 FirewallRuleAnalyzer run only converged after a manual `AEGIS_PROVIDER_CONTEXT_WINDOW=196608`
because the generic configured window was too small for the phased build, and a mid-phase overflow was
terminal. Two levers close that. (a) Up-front sizing: for a phased `--skill` drive on an Ollama-backed
provider, `recommendPhasedDriveWindow` (`internal/cli/chat.go`) probes the model's training-context max
and, when `ollamainfo.RecommendContextWindow(modelMax)` beats the configured window, raises
`cfg.Provider.ContextWindow` *before* the adapter is built — so both the `num_ctx` sent to Ollama and the
P47.1 compaction budget get the room the phased build needs, with a notice, and no manual step. (b)
On-overflow escalation: a new optional adapter capability `provider.ContextWindowRaiser`
(`RaiseContextWindow`, implemented by the native Ollama adapter and reached through the retry/failover
decorators via `provider.RaiseContextWindow`'s `Unwrap` walk) lets a phase double `num_ctx` toward the
model max on a context overflow (`nextDriveWindow` — a doubling step, gentler on GPU memory than a jump to
max), additive to the P47.2/P47.7 fresh-context reset. The compaction budget stays at the sized window
deliberately — a larger `num_ctx` only buys physical headroom against a transient overshoot. Regression
tests: `TestNextDriveWindow`, `TestRaiseContextWindow` (ollama, monotonic + actually-sent),
`TestRaiseContextWindow_UnwrapsDecorators` (provider, unwrap chain), and the non-Ollama sizing gate.

**P47.4 — near-stateless in-phase continuations cap peak context.** Each in-phase continuation of the
phased threat-model drive appended to a growing `conv` in `runPhasedSkillDrive`
(`internal/cli/chat_phased.go`), so within a phase every re-read of a large file (the ~400-line
`3-findings.md`, the ~210-line analysis) was retained for the rest of the phase and peak context grew
cumulatively, not per-turn — the structural growth the compaction (P47.1) and on-overflow reset (P47.2)
before it could only chase. Since the `<!-- PENDING -->` files on disk are the source of truth, the fix
resets the conversation to just `[system + phaseContinuePrompt(pending)]` (plus any P39.7 nudge) every
continuation turn — the model re-reads only what it needs — so a phase's peak context is capped at roughly
one turn's reads. This is the always-on form of P47.2's on-overflow reset: it reuses the same
`freshPhaseConv` helper (now taking a `nudge` prefix so the stall-breaker survives the reset), fires every
turn rather than only after an overflow, and therefore makes overflows rarer. The no-progress guard is
unaffected — it tracks `iterMutations` and the pending set, both outside `conv`. `AEGIS_PHASE_CONV=growing`
restores the pre-P47.4 accumulate-then-reset behaviour so the two can be measured side by side (P47.4 was a
measure-first item, built ahead of its live-run trigger). Regression-tested by `TestGrowingPhaseConvForced`
(the escape-hatch gate) and `TestFreshPhaseConv_NudgePrefix` (the nudge survives the reset); the existing
`TestFreshPhaseConv_ReseedChoice` still guards the reseed-prompt choice. Last item but one of the P47.x
phased-drive stability batch.

**P47.9 — route hollow-body failures back through the owning content phase.** When a run resumes a suite
whose `<!-- PENDING -->` markers were deleted but whose prose bodies are empty — the case the
`finding-bodies-nonempty` check (ec0127c) catches — the marker oracle (`skillPhase.complete`) marks every
content phase "complete" and the phased drive jumps straight to phase 6, so **all** the remaining authoring
(filling ~60 empty sections across 15 findings, reconciling the coverage table) lands on the bounded phase-6
verify-fix loop. That is too much substantive authoring for one bounded loop on a slow local model: observed
2026-07-27 (FirewallRiskRater) it never converged, and the single large fill attempt triggered the
P47.7/P47.8 overflow. The fix couples a content-substance verify failure to phase re-entry rather than a
generic fix prompt. `runPhasedVerifyAndQuality` (`internal/cli/chat_phased.go`) now checks each failure set
for a content-substance check (`finding-bodies-nonempty`, and by extension
`coverage-matches-related-threats` — both owned by the findings phase) *before* consuming a verify round; on
a match it calls `runReopenedContentPhase`, which re-drives the owning phase in its own bounded,
near-stateless fresh-context loop (P47.4-style) whose completion oracle is the verify check clearing — the
PENDING-marker oracle can't be used, a hollow resume has none. The re-entry prompt (`hollowBodyReentryPrompt`)
orients the fresh context (run dir + SKILL.md), names the exact empty sections extracted from the verifier
evidence (`extractCheckFailures` pulls just the owned check's `FAIL` block, so unrelated mechanical failures
don't leak in), and carries the one-section-one-edit guardrail — deliberately *not* reusing
`noSelfVerifyInstruction`, whose wording is built around markers the hollow case lacks. It reuses the P47.7
overflow-reset and the P39.7 no-progress guard, and is routed at most once per phase (a `reopened` set): if
the re-entry can't fully clear the check it falls through to the bounded generic verify-fix loop, so there is
no infinite re-entry. Regression-tested by `chat_phased_reentry_test.go` — routing
(`TestOwnerPhaseForContentFailure`: content failures route, mechanical ones don't, gated to a plan with the
owning phase), the completion oracle (`TestPhaseHasContentFailure`), evidence extraction
(`TestExtractCheckFailures`, `TestFailuresContainCheck`), and the prompt (`TestHollowBodyReentryPrompt`:
names the sections, carries the guardrail, never mentions PENDING). Final item of the P47.x phased-drive
stability batch.

**P47.7 — a phase-6 context overflow resets instead of aborting the drive.** P47.2 made a mid-phase
overflow a resumable fresh-context reset, but only in the content-phase loop; the phase-6 verify/quality
loop (`runPhasedVerifyAndQuality`, `internal/cli/chat_verify.go`/`chat_phased.go`) returned the engine
error straight up, so an overflow during a verify-fix or quality turn aborted the *whole* drive on the raw
`ollama: response truncated at the context limit … unexpected end of JSON input` — with no reset, no verify
rounds 2/3, and no `.quality-stamp.json` (observed 2026-07-27, FirewallRiskRater). The fix adds
`recoverPhase6Overflow`: a context overflow during a phase-6 turn escalates the window (P47.5b), counts the
reset against a bounded `maxPhase6OverflowResets`, and loops again — the next iteration re-runs the
mechanical checks and re-issues the turn from a fresh, run-dir-oriented context (`runPhase6Turn` already
builds a fresh conversation, so the reset is implicit). A non-overflow error is still surfaced as terminal;
once the reset budget is exhausted it prints a resumable stop notice and ends the drive cleanly. A subtle
correctness fix rides along: `qualityReviewed` is now set only *after* the quality turn completes, so a
turn that overflowed mid-review is not mistaken for a finished pass and stamped. Regression-tested by
`TestRecoverPhase6Overflow` (three-way classification, escalation-per-retry, budget-exhaustion stop) and
`TestTryEscalateWindow_NilSafe`.

**P47.8 — carry the anti-monolithic-write guardrail into the phase-6 prompts.** The content-phase prompts
forbid whole-file rewrites (the P39.14 "one section, one edit … a monolithic write truncates" lesson), but
`verifyFixPrompt` and `qualityReviewPrompt` only said to "resolve every failing check" — so, told to fill 15
empty finding bodies, the drive chose a single whole-file `write_file` of the ~400-line `3-findings.md`,
whose tool-call JSON truncated and triggered the P47.7 overflow (2026-07-27). Both phase-6 prompts now carry
a shared `phase6IncrementalEditRule`: make each fix a small targeted `edit_file` — one section/row per edit,
never regenerate a whole file, never `write_file` a suite file. Cheap, reusing the existing P39.14 rule; it
reduces how often the overflow fires while P47.7 recovers when it still does. Regression-tested by
`TestPhase6PromptsCarryIncrementalEditRule`.

**P47.3 — stop content phases burning context on manual self-verification.** On the 2026-07-24
FirewallRuleAnalyzer run both context overflows were driven by the same behavior: the model re-reading
already-filled suite files and recomputing STRIDE coverage arithmetic across dozens of in-phase turns —
work the deterministic phase-6 `verify.py` / `inventory.py` already own authoritatively. The content-phase
seeds (`phasePromptAnalysis`, `phasePromptFindings`) and the shared `phaseContinuePrompt` in
`internal/cli/chat_phased.go` never told the model to stop. The fix adds one shared instruction
(`noSelfVerifyInstruction`): do not re-read or re-audit files whose `<!-- PENDING -->` markers are already
cleared, and do not recompute STRIDE/threat/coverage counts by hand to double-check your own work — the
phase-6 verifier does that later — spend each turn filling the next marker and nothing else. It is woven
into the two large content-phase seeds and the in-phase continuation prompt; the short DFD/assessment seeds
are left as-is. The findings seed additionally clarifies that reading the prior-phase analysis file to
source the coverage table is expected authoring, not self-checking, so the instruction doesn't suppress a
legitimate read. This is a pure prompt change — no code-path risk — that attacks the token-burn at its
source, cutting per-phase turn count and context growth regardless of whether compaction is on, so it
reduces how often the P47.1/P47.2 defenses have to act. Regression-tested (`chat_phased_test.go`:
`TestContentPromptsSuppressSelfVerification`) that all three carriers hold the instruction and that it names
the mechanical verifier as the authority. Third item of the P47.x phased-drive stability batch.

**P47.2 — a context-overflow error resets the phase instead of aborting the drive.** Even with P47.1's
compaction wired, a residual overflow can still happen (a single oversized turn, an undetectable local
window). When it did, `runPhasedSkillDrive`'s inner loop returned the engine error verbatim, so a
terminal `NewContextTruncationError` (or an Ollama hard-reject envelope) aborted the **whole** phased
drive — even though the failure is *resumable at the phase level*: the phase's `<!-- PENDING -->` files
persist on disk, and a fresh, near-empty context re-reads them and continues (exactly why the 2026-07-24
manual re-runs worked). The fix detects that specific error class inside the loop and, instead of
`return err`, resets `conv` to a fresh conversation and retries the phase — the same fresh-context reset
the drive already does at phase *boundaries*, now applied within a phase on overflow. A new
`provider.IsContextOverflowError` classifies only the size-caused terminal errors a smaller context can
recover from (the P35.2 truncation error plus context-size stream envelopes), deliberately excluding
size-independent terminal failures (model-not-found, malformed) and response-header timeouts where a
reset would only loop. The reset counts as a turn so the existing `--max-turns` guard still bounds it,
and a new `freshPhaseConv` helper chooses the reseed prompt: the in-phase continuation prompt when files
exist on disk, or the full phase seed prompt when the setup phase overflowed before the run directory
was even created. Regression tests cover the classifier's include/exclude boundaries
(`internal/provider/errors_test.go`) and the reseed choice (`chat_phased_reset_test.go`). Second item of
the P47.x phased-drive stability batch; the residual-recovery complement to P47.1 (compaction prevents
most overflows, this recovers from the rest).

**P47.1 — proactive compaction wired into the CLI `chat --skill` drive engine.** The daemon builds its
engine with both a resolved context window and a `Compactor`, so engine.Run's proactive per-turn compaction
(fires at 85% fill, gated on `contextWindowTokens > 0` **and** a non-nil compactor) runs on the server path.
The CLI `engine.New` in `internal/cli/chat.go` set **neither**, so the phased threat-model drive — which runs
entirely on the CLI engine and grows context every turn — had no defense against its own growth: on the
2026-07-24 FirewallRuleAnalyzer run, context climbed until Ollama hard-rejected the request (173,816 vs a
131,072 window) and the drive aborted with a terminal `NewContextTruncationError`, three separate times,
each needing a manual re-invocation. The fix mirrors the server (~a few lines already proven in
`internal/server/engine_build.go`): a new `driveCompaction` helper resolves the effective window the same way
the daemon does — configured `provider.context_window`, reconciled downward when a *loaded* Ollama model is
actually serving less (via `ollamainfo.Detect`, the silent-front-truncation guard) — builds a
`compaction.Summarizer` over it (preferring `provider.small_model` for the summary calls, skipping
auto-compaction rather than defaulting to the 120k cloud budget when a local window is still unknown), and
passes `ContextWindowTokens` + `Compactor` into the CLI `engine.New`. Both the single-context linear drive and
the P38.8 phased drive reuse that one engine, so both gain the defense. Extracted into `driveCompaction` so a
regression test (`chat_compaction_test.go`) can assert the CLI path keeps compaction enabled (non-zero window
+ non-nil compactor) and can't silently diverge from the daemon again. Head of the P47.x phased-drive
stability batch; on its own it would have prevented both aborts on the 2026-07-24 run.

**P39.10 / P39.11 — `chat --skill` workspace materialization + drive-oracle skip (from the P38.1
gpt-oss:20b re-test).** The 2026-07-23 gpt-oss:20b run died *before* model capability was tested, on
two `chat --skill`-CLI bugs that both shipped on `tier3-batch` and were verified live end-to-end; this
entry backfills the release note and the regression tests that were the remaining P38.1 debt.

- **P39.10 — materialize enabled builtin skills into the workspace, not just the data dir**
  (`internal/cli/chat.go`, `skills.MaterializeBuiltinsToProject`). `aegis chat` runs in-process and only
  extracted the embedded builtin skills to `<dataDir>/builtin-skills`, which is *outside* the sandboxed
  workspace root — so the file tools (confined to `cwd`) rejected reading a skill's bundled scripts, and
  the model couldn't reach `recon.py`/`scaffold.py` to start the build. The CLI now mirrors the daemon and
  also materializes the enabled builtins into `<cwd>/.aegis/builtin-skills`, so the `<skill_assets>`
  manifest resolves to a workspace-relative path the file tools accept and `skills.Load` prefers the
  project copy. Covered by `internal/skills/embedded_test.go`
  (`TestMaterializeBuiltinsToProjectPlacesAssetsReachableByReadFile` and siblings).
- **P39.11 — the drive-completion oracle skips the materialized skill source**
  (`internal/cli/chat.go`, `scanPendingMarkers`/`suiteFileCount`). With P39.10 placing the skill's own
  skeleton/reference assets under `<cwd>/.aegis/builtin-skills`, those files carry the skill's *example*
  `<!-- PENDING: … -->` markers — so the PENDING-marker completion oracle walked the skeleton templates
  and could never reach zero (the drive never converged, phase-6 verify never fired), and the P38.6
  fabricated-success floor check counted skill source as build output. Both walks now `SkipDir` the
  `builtin-skills` subtree (`pendingSkipDir`, mirroring `skills.builtinSkillsDirName`). Regression tests
  `TestDriveOraclesSkipBuiltinSkillsSubtree` (synthetic) and `TestDriveOraclesSkipRealMaterializedBuiltins`
  (drives the real `MaterializeBuiltinsToProject` output, so a dir rename or a skeleton change that
  reintroduced live markers fails the test) in `internal/cli/chat_drive_test.go`.

With the scripts reachable, gpt-oss:20b itself then failed to converge from small-model
path/argument brittleness (mangled script paths, a typo'd run-dir, a non-existent `search` tool, the
wrong `--framework` flag) — a model-competence limit, not a harness bug, and separate from these two fixes.

**P39.12 / P39.13 / P39.14 / P39.15 — threat-model drive robustness (from the P38.1 full-stack test).**
The 2026-07-24 full-stack test drove the built-in `aegis chat --skill threat-modeling` against a lean copy of
FirewallRuleAnalyzer (FastAPI + MariaDB, ~8.7K LOC) on `qwen3.6:35b-a3b-fast` via the native ollama adapter.
It cleared the harness and model-competence questions — the drive ran recon → scaffold → fill, held the
run-dir path across every `edit_file` (the old gpt-oss:20b mangling did not recur), produced grounded
file:line-cited threats, and its DFD passed `lint_dfd.py` 5/5 — but did not reach a verify-clean suite because
of throughput and write robustness, not orchestration. Four fixes, all with regression tests:

- **P39.12 — default `provider.response_header_timeout` 5m → 30m** (`internal/provider/sse/sse.go`). Run 1
  aborted at turn 7: reading the 2845-line `fwweb/main.py` in one turn pushed the next prefill past the
  5-minute header timeout at ~7 tok/s on a local 35B. Ollama withholds the response header until prefill
  finishes, so a large-context threat-model turn legitimately needs longer; 5m was too tight for any
  content-rich repo on modest hardware. Tests updated in `sse`, `config`, and `ollama`.
- **P39.13 — `read_file` caps an unbounded read at 1500 lines** (`internal/tool/builtin/file.go`). One
  whole-file read of a large source file is the per-turn context spike that both blew the timeout above and
  drove cumulative session input to 3.47M tokens before truncating at the context limit. The tool already
  took `offset`/`limit`; now an unbounded read returns the first 1500-line window plus a notice telling the
  model to page with `offset` or grep for the part it needs. An explicit limit is still honored verbatim.
  Regression test `TestReadDefaultLineCap`.
- **P39.14 — one section per `edit_file`, no monolithic writes** (SKILL.md §4 + `chat.go` continuation/act-now
  prompts). The model wrote the entire findings file in a single `edit_file` (~5,700 tokens, ~13 min at
  7 tok/s), and on the final run that write truncated into a malformed tool call (`unexpected end of JSON`).
  The skill's context-bounding levers and the drive prompts now hard-require filling exactly one
  `<!-- PENDING: <section> -->` marker per `edit_file` call, and read source in ranges rather than whole.
- **P39.15 — a final quality-and-sanity pass after mechanical verify** (`chat.go`, `chat_verify.go`). The
  phase-6 scripts verify structure and counts but not substance; verify.py caught real model errors (a Tier-2
  threat with a Tier-1 prerequisite, `AV:N` on the non-network `IngestWorker`) yet a build can pass the
  scripts with vague evidence, filler, or incoherent severities. When the scripts verify clean the drive now
  runs one bounded, once-only self-review turn (`qualityReviewPrompt`) checking groundedness, filler, and
  internal consistency and fixing issues in place; the mechanical checks re-run afterward, so a review edit
  that breaks a script check is caught by the existing fix loop. Test `TestQualityReviewPrompt`. The **P38.1**
  umbrella stays open pending a verify-clean re-run with these fixes on a smaller target or faster model.

**P40.3 — full-text search within a session's transcript.** Every picker fuzzy-filters lists of turns, but
nothing grepped the actual message *content* of the open session, so "find the earlier message where I asked
about X" had no answer short of manual scrolling. A new incremental search mode (`internal/tui/search.go`),
opened with **ctrl+f** (rebindable `transcriptsearch`), captures keyboard input like lnav's `/`-search: typing
edits the query live, ⏎/↓/ctrl+n and ↑/ctrl+p step between matches (wrapping), esc closes. `transcriptPane.Search`
greps each item's ANSI-stripped raw text case-insensitively; the focused match is scrolled to the top and marked
with the existing focused-item accent bar, and every visible occurrence is reverse-highlighted in place
(`highlightSearchMatches`, width-preserving so the selection/focus overlays keep working). The search bar
replaces the composer's status line while active, keeping the input-area height stable. Tests: `search_test.go`
(pane grep, navigation/wrap, the full ctrl+f→type→esc Update flow, highlighter width-preservation).

**P40.9 — inline mermaid diagrams now render as box-drawing ASCII in the transcript.** `render_diagram` only ever
produced a *file*; a model that inlined a ` ```mermaid ` snippet just got an unstyled code block. A new
dependency-free package `internal/mermaidascii` renders the common shapes — flowchart/graph (`TD/TB/BT/LR/RL`,
node shapes `[]`/`()`/`{}`/`(())`, edge-label forms, dotted/thick links) and `sequenceDiagram` (participants,
lifelines, solid/dotted arrows) — into box-drawing text, best-effort (`Render` returns `ok=false`, never an
error, on unsupported/unparseable input) and output-size capped (60 nodes / 80 messages). Multi-child branches
compose real T-/cross-junctions via a per-cell direction mask rather than the last edge clobbering the first. A
`renderMermaidBlocks` preprocessing pass in `mdRender` (`internal/tui/mermaid.go`) swaps each *complete*
` ```mermaid ` fence for the rendered ASCII in a plain code fence; unsupported diagrams and mid-stream
unterminated fences are left byte-for-byte untouched (raw source still shows). Tests: `mermaidascii_test.go`
(pinned canvases for a tiny graph + sequence, LR/shapes/labels/branch-junction/caps/CRLF), `mermaid_test.go`
(fence substitution, untouched cases, transcript integration).

**P45.2 — hunk-level agent-vs-external change attribution.** `internal/filetracker` only tracked whole-file
mtimes, so there was no way to answer "which lines in this file did the agent author" (needed for scoped diff/
revert UX). A new `hunks.go` records, per successful `write_file`/`edit_file`/`multiedit`, the changed line
ranges attributed to the agent — computed with a dependency-free stdlib LCS line diff (bounded, degrading to
whole-file attribution above the bound) — remapping and merging previously recorded hunks through each edit
(`RecordAgentWrite`). `AgentHunks` reconciles the stored ranges against a fresh disk read: a hunk survives an
external edit only if all its lines remain present and contiguous, so an out-of-band change drops just the
overlapping hunks (survivors shift) rather than the whole file's state. The existing mtime-based `CheckWrite`
read-before-write guard is untouched; all additions are additive. Tests: `hunks_test.go` (14 cases — recording,
merge, external-edit drop/shift, pruning, diff primitives) plus the wired write/edit tools.

**P40.7 — the two hand-built form overlays now share one panel-frame helper.** `securityConfigModel` and
`wizardModel` are `huh` multi-step forms, not `listDialog` pickers, so they can't literally reuse the list
widget — but both hand-rolled a byte-identical rounded accent-bordered frame in their `view()`. That frame is
now a single `fixedPanelFrame(content, width)` helper in `dialog.go`, beside `dialogFrame`/`renderOverlay`, so
the overlay chrome (border, accent, padding) is defined once; width stays per-form. The dimming/centering half
of the shared chrome the two forms already got from `renderOverlay`. Behavior is unchanged (existing
wizard/securityconfig tests stay green); this is the safe, real de-duplication the item targeted, in place of
forcing form input into a list picker.

**P40.4 — an experimental, opt-in kitty-graphics image tier (prototype).** The half-block thumbnail flows
through the cell-grid renderer as ordinary SGR text; a true kitty/iTerm2 escape does not, and there is no such
terminal in CI to verify placement against — the reason the tier was originally descoped. P40.4 lands the
building blocks the roadmap asked for as tested, safe increments: `detectKittyGraphics` (env-based:
kitty/Ghostty/WezTerm/Konsole) and a correct, chunked kitty graphics-protocol encoder (`kittyGraphicsSequence`,
`f=100,a=T`, ≤4096-byte `m=1/m=0` chunks, scaled to a cell box). It is wired only behind an explicit
`image_rendering: "kitty"` opt-in — **never** auto-selected; `"auto"` stays half-block — so the safe default is
untouched. The render-loop placement remains the unverified step and is documented as such (in code and
`docs/configuration.md`). Tests: `kitty_test.go` (detector, opt-in resolution, escape structure + chunking, the
never-error thumbnail path).

**P40.8 — LaTeX math now renders as a Unicode approximation in the transcript instead of raw markup.**
`newGlamourRenderer` wires up plain glamour with no math extension, so `$E=mc^2$` or a `$$...$$` block showed
as literal dollar-sign text (goldmark has no math awareness). Following xAI `grok-build`'s terminal-appropriate
approach — a Unicode approximation, not real TeX typesetting — a new `renderMathUnicode` preprocessing pass
(`internal/tui/latex.go`) runs in `mdRender` ahead of glamour: it converts `$...$`/`$$...$$` spans to Unicode
(super/subscripts, Greek letters, operators/relations, arrows, `\frac{a}{b}`→`(a)/(b)`). Two safety rules keep
it from mangling prose: fenced code blocks and inline code spans pass through untouched (a shell `$HOME` is
never rewritten), and a single-`$…$` span converts only when its content actually looks like math (a backslash
command or `^`/`_`), so currency like "$5 and $10" is left alone. Non-representable exponents keep their
literal `^{…}` form. Tests: `latex_test.go` (conversions, code preservation, currency, unbalanced/escaped `$`).

**P40.5 — the default theme now auto-detects the terminal's light/dark background.** Aegis always defaulted to
the dark scheme and required an explicit `/theme`/config value to switch to light. `tui.theme` now defaults to
`"auto"`: `Run` binds a provisional dark scheme (lipgloss captures colors at style-creation time), `Init`
issues bubbletea v2's `RequestBackgroundColor`, and `Update` applies the light or dark scheme from the
`tea.BackgroundColorMsg.IsDark()` reply — rebuilding `m.th`/`m.renderer` the same way the live `/theme` switch
does. `/theme auto` re-enables detection; any explicit `/theme <name>` clears the auto flag so a later
background report can't override the user's choice. Tests: `TestIsAutoTheme` plus the existing theme-switch
coverage.

**P40.2 — hjkl/g/G scrolling is now consistent across every scrollable content surface.** `j`/`k` worked on
the transcript and completion popup but the transcript pane and the tool-card (`transientPanel`) overlay had
divergent scroll vocabularies. Both now share the full vi set: `j`/`k` (line), `u`/`d`+`ctrl` (half-page),
`b`/`f`/`space`/`ctrl+f`/`ctrl+b` (page), and `g`/`G`+`home`/`end` (top/bottom). The terminal pane and
completion popup are input surfaces where letters are typing, so they keep `pgup`/`pgdn` only, by design.
Tests: extended `TestTranscriptHandleKeyMatchesViewportDefaults`.

**P40.1 — the sidebar and terminal panes are now resizable.** Both were fixed-width constants
(`sidebarInnerW`, `termPaneVpW`), toggled but never resized. The live widths are now per-model state
(`m.sidebarW`, `m.term.width`), adjustable within min/max bounds with `ctrl+←`/`ctrl+→` on the focused pane
(terminal when it has focus, else the sidebar) — `ctrl`+arrows are free, since the textarea uses `alt`+arrows
for word navigation. A new `resizePane` method clamps and re-runs `layout()`; the terminal pane gained
`setWidth`/`totalW` and re-wraps its buffer on resize. Tests: `paneresize_test.go` (grow/shrink, bound
clamping, terminal-focused resize, no-focus no-op).

**P40.6 — the status-bar hint footer is now scoped to the focused input surface.** The bottom bar always
showed a static `ctrl+k · f1 · ctrl+e` hint regardless of focus. A new `contextualFooterHints` method
(sourced from `m.keys`, so `tui.keybindings` overrides are reflected) shows chat-composer hints by default
(palette / help / editor, plus a resize hint when the sidebar is open) and terminal-pane hints
(`esc chat` / diagnose / resize) when the terminal has focus — lazygit's focus-scoped bottom-bar precedent.
Tests: `footer_test.go`.

**P44.1 — bundled skill assets now go through admission scanning before being surfaced to the model.**
Surfaced comparing against Cisco's DefenseClaw, whose CodeGuard admission gate statically scans a skill asset
before it's trusted. A bundled skill directory (`.aegis/skills/<name>/SKILL.md` + companion `scripts/`,
`references/`) can ship arbitrary `.py`/`.sh` files that `withAssetManifest` lists for the model to read/run,
but `wrapUntrustedSkill` only wrapped the SKILL.md *prose* — the scripts' content went unscrutinized. Added a
`skills.BundleScanner` seam (a plain package var set once at startup, matching the security package's
`inspectImageID`/`cacheFileExists` idiom; `skills` does not import `security`) wired at daemon (`server.New`)
and CLI (`aegis chat`) startup to `security.ScanBundleWarnings`, which runs the same `DefaultScanners` over the
directory that `aegis security scan` drives and returns one warning line per HIGH/CRITICAL finding. On
discovery of a bundled *untrusted* directory (never the embedded built-ins), `appendFromDir` folds any warning
into the top of the `<skill_assets>` block with do-not-run framing — the same "frame it as data, never drop
it" posture `trust.Wrap`'s scan-hit path takes. The verdict is baked into the skill content already memoized by
`discoverCache`'s directory signature, so re-scanning happens only when the bundle changes. Degrades to a
silent no-op when the multiscanner image isn't built and no host scanner is installed (every scanner resolves
to `MethodNone` → zero findings → nil), mirroring `verifyMultiscannerImage`. Tests:
`internal/skills/bundlescan_test.go` (seam, fold-in, degradation, trusted-exclusion) and
`internal/security/skillscan_test.go` (severity filter, formatting, degradation).

**P45.1 — `worktree.Manager.Add` now carries uncommitted/untracked files into a new worktree.** `git worktree
add` only checks out the committed tree, so staged/unstaged/untracked changes in the source working tree were
invisible to a new worktree — spawning a subagent into an isolated worktree silently dropped the caller's
in-progress edits. Surfaced comparing against xAI `grok-build`'s `xai-fast-worktree`. `Add` now runs a
copy-on-top pass (`carryDirty`) after the standard `git worktree add` — leaving its committed-checkout and
`-b` semantics untouched — that parses `git status --porcelain -z` (NUL-terminated, verbatim paths) and mirrors
the source working tree's dirty state onto the fresh checkout: it copies modified/staged/untracked files
(preserving mode and symlinks, creating parent dirs) and applies deletions and rename old-name removals so the
worktree faithfully reflects the source. gitignored files are excluded automatically (porcelain omits them
without `--ignored`). A new `AddCarry` additionally returns the carried paths; `aegis worktree add` prints
`carried N uncommitted file(s)…` so the behavior is discoverable. Tests:
`TestAddCarriesDirtyState`/`TestAddCarriesRename`/`TestAddCleanTreeCarriesNothing`.

**P46.1 — Per-task file-write scope is now mechanically enforced, not just advisory session-wide rules.**
Surfaced comparing against `codex-build` (a Claude-orchestrates-Codex workflow whose `check_scope.py`
mechanically verifies a task's writes stay within a declared per-task path allowlist). Aegis's write gating
was all session-lifetime: the mode gate is path-blind, and text allow/deny rules (`internal/permission/rules.go`)
are parsed once at load and apply for the whole session — nothing let a task say "I should only be touching
these files" and have it enforced. Added a new `permission.TaskScope` (a mutable, mutex-guarded per-session
allowlist of path globs) plus `permission.ScopeGate`, wired as the **outermost** gate in `server.buildGate`
so an out-of-scope write is refused even when a standing `allow write(...)` rule would grant it (the scope is
a further restriction the task opted into, not a competing permission). The scope rides on the run context —
the engine passes one context into both `gate.Check` and `tool.Execute`, so a new deferred `scope` tool
(`set`/`clear`/`show`, capability `read` so it's usable in plan mode too) mutates the same object the gate
reads. Scope restricts writes only (`write_file`/`edit_file`/`multi_edit`, via `path`/`file_path`/`edits[].path`);
reads are never restricted, and a path-less write-capability tool (git_commit, remember) is never scope-blocked.
Paths and patterns go through the same normalization as `RuleGate`'s Read/Write matching so a `..`/case/separator
trick can't dodge the scope. Per-session `TaskScope` stored on the server (`taskScopeFor`), injected into
`runCtx`, and cleaned up on session delete. Tests: `internal/permission/scope_test.go` (gate enforcement,
inactive-passthrough, pathless-write and read exemptions, traversal) and `internal/tool/builtin/scope_test.go`
(tool set/show/clear, no-context error). Full `go test ./...` green.

**P46.2 — `git_commit` now runs an optional pre-commit test gate before committing.** Same `codex-build`
comparison: its loop refuses to commit unless the configured test command just passed. Aegis's
`gitCommitTool.Execute` was a straight passthrough — no test-command config, no check that anything ran, only
the ordinary `CapWrite` permission gate. Added `config.GitConfig.PreCommitTestCommand` (+
`PreCommitTestTimeoutSec`, default 600s): when set, `git_commit` runs it in the workspace on the host (same
place `runGit` runs git) via the platform shell *before* staging, and a non-zero exit aborts the commit and
returns the command output instead, leaving the index untouched. Unset (the default) is a no-op, so existing
sessions with no test command are unaffected. Because it executes an arbitrary host command, it is treated as
a security-relevant setting **frozen from untrusted project config by the workspace-trust gate** (P27.1):
added to `securityRelevantDiff` and the freeze list in `applyWorkspaceTrust`, so a cloned repo's
`.aegis/config.yaml` cannot introduce or change it until `aegis trust`. Tests:
`TestGitCommitPreCommitTestGate` (failing gate refuses + leaves index clean, passing gate commits, unset is a
no-op) and `TestWorkspaceTrustFreezesGitPreCommitTestCommand` (frozen while untrusted, applies after trust).
Full `go test ./...` green.

**P46.3 — `structured-build` skill packages the P46.1/P46.2 mechanisms into a one-task-one-commit workflow.**
The remaining `codex-build` property was workflow discipline layered on those two gates: one task → one commit,
one plan → one PR. Sequenced deliberately after P46.1/P46.2 landed as real mechanisms, because a skill enforcing
this only in prose would repeat the exact weakness the roadmap has flagged elsewhere (P44.1, P39.6) —
instructions a model drops under context pressure are not a mechanical check. The new embedded built-in skill
(`internal/skills/builtin/structured-build/SKILL.md`, dormant until enabled) drives, per task: write an explicit
task list, `scope(set, ...)` the task's file footprint, edit + verify tests, `git_commit` (which re-runs the
pre-commit gate as a hard check), `scope(clear)`, repeat — plus a stop-when-stuck rule (leave the diff intact
and hand back rather than thrash). `TestBuiltinsListsEmbeddedSkills` updated; skill-list references in
CLAUDE.md and docs refreshed. Full `go test ./...` green.

**P41.1 — Compaction now shares the engine's script-aware token estimate instead of a flat chars/4 one.**
A 2026-07-22 data-flow review found the proactive compaction gate could silently no-op a compaction the
engine had already decided was needed: `compaction.EstimateTokens` was a flat `chars/4` heuristic, while the
engine's own `estimateTokens` is script-aware (CJK/Hangul/Kana at ~1 token/char, other non-ASCII at ~0.5
token/char) precisely because flat `chars/4` badly undercounts dense scripts. The engine used its accurate
version for the 85%/95% "context nearly full" checks and `MaxTokensPerRun`, but `Summarizer.compact` — the
primary gate, called unconditionally at the top of every `engine.Run` — decided whether to actually compact
using its own cruder estimate. So for a CJK/Cyrillic/Greek/Arabic/emoji-heavy conversation the engine could
correctly call `Compact`, only for the summarizer's `shouldCompact` to decide there was still room and no-op
— worst case letting a local (Ollama) server truncate from the front and drop the system prompt before
compaction ever fired, the exact P2.7 failure the proactive machinery exists to avoid. Fixed by extracting
the script-aware estimator into a new shared `internal/tokenest` package (`Estimate`, `Message`, `Messages`)
that both the engine and `compaction.EstimateTokens` now call — one implementation, no second heuristic to
drift. The engine's estimator tests moved to `internal/tokenest/tokenest_test.go`, joined by
`TestMessagesIsScriptAware` (the P41.1 regression guard proving the whole-conversation estimate counts CJK
far above flat chars/4). Full `go test ./...` green.

**P43.1 — Debate's concession detector no longer misreads a hedged critique as a full concession.** Examining
`internal/debate`/`internal/swarm` reliability as a candidate next-phase roadmap area found `concedeRe`
(`internal/debate/debate.go`) matched the bare word "concede" anywhere in a critic's response with no
negation handling — confirmed live: a critique reading "I won't concede this point — the claim is missing a
rate limit check, see api.go:42." matched as a full concession. Because `Round.Conceded` short-circuits the
round (skips the proposer's rebuttal) and the arbiter persona is explicitly instructed to weigh a conceded
round in the claim's favor, this could flip a debate that should REJECT/REVISE into an UPHOLD purely from
critique phrasing — not model capability, since even a fully compliant model saying "I'll concede X is
minor, but the core flaw stands" would trip it. The same file's `verdictOutcomeRe`/`verdictConfidenceRe`
already anchor to line-start for the arbiter's structured output for exactly this reason; `concedeRe` never
got the same treatment. Fixed: `concedeRe` is now anchored to the start of the trimmed response
(`^[\s*_]*concede\b`, tolerating leading whitespace/markdown emphasis), and both call sites (`hasEvidence`,
`Run`'s `Round.Conceded` assignment) go through a new `isConcession` helper instead of calling the regex
directly. Tests: `TestRunHedgedCritiqueIsNotMisreadAsConcession` (full `Run` regression — proves the
proposer's rebuttal now actually executes instead of being skipped) and `TestConcedeRegexAnchoredToStart`
(direct regex table test covering compliant/markdown/negated/mid-sentence shapes),
`internal/debate/debate_test.go`. Full `go test ./...` green (58 packages).

**P42.1/P42.2 — `internal/plugins` closed the two gaps a scoped post-2026-07-03 security self-review found.**
A review targeted at exactly the packages that shipped after the 2026-07-03 architecture/security review
(`internal/plugins`, `internal/hooks`, `internal/mcpserver`, `internal/acp`, `internal/cron`) found every
sibling already carried a FIND-xx/P24.x/P27.x hardening comment except `internal/plugins` (added
2026-07-16) — it was never folded into the P27.1 workspace-trust gate its structural twin, `mcp.servers`,
already has. **P42.1:** `Config.Plugins` is now part of `securityRelevantDiff`/`applyWorkspaceTrust`
(`internal/config/config.go`), so an untrusted project's `.aegis/config.yaml` can no longer register a
process-tool plugin (an arbitrary host command exposed as a live tool) with no confirmation — mirrors the
existing `cfg.MCP`/`cfg.Hooks` freeze exactly. **P42.2:** `ProcessToolConfig.Capability`
(`internal/plugins/plugins.go`) was a free-text config field the permission gate trusted verbatim; since it's
config data (potentially from that same untrusted project), a plugin could declare `capability: "read"` to
be auto-allowed even in plan mode, or `"write"` to skip build mode's execute-`Ask` prompt, while its
`Execute` ran an arbitrary command regardless. `processTool.Capability()` now always reports `CapExecute`,
full stop — the field stays for documentation purposes but no longer feeds the gate. Tests:
`internal/config/workspacetrust_test.go` (plugins added to the freeze/unfreeze regression),
`internal/plugins/plugins_test.go` (`TestProcessToolCapability` now asserts `CapExecute` regardless of
config). Full `go test ./...` green (58 packages).

**P39.9 (`/api/ps`-verification half) — the native-Ollama context-window path now verifies the real allocation
instead of trusting `num_ctx = context_window` outright.** `internal/server/contextwindow.go`'s
`initContextWindow` short-circuited the native `provider.default: ollama` + configured-`context_window` case:
it set `ctxWinFinal = true` and returned without ever probing, on the theory that the native adapter (P33.9)
pins `options.num_ctx` to the configured window on every request, so "the served window is exactly what's
configured." That holds on well-resourced hardware, but `num_ctx` is a *request*, not a guarantee — on
VRAM-constrained hardware Ollama can allocate *less* than asked (or offload KV/layers to CPU), silently
front-truncating prompts (system prompt first) exactly like the OpenAI-compat path, and the daemon could not
see it. The fix removes the short-circuit so the native path runs the same `ollamainfo.Detect` (`/api/ps`- and
`/api/show`-backed) detection as the compat path and lets `applyDetectedWindow` reconcile: a *loaded*
(authoritative) `/api/ps` reading below the configured window is served as the effective window with the
existing "configured context_window exceeds what Ollama is serving" warning; a matching/larger reading keeps
the configured value (provenance stays `config`); a non-authoritative reading (model not loaded yet) keeps the
configured value and stays non-final so `maybeRefreshContextWindow` re-detects after the first run loads the
model; an unreachable Ollama keeps the configured value, stashes the native base, and stays non-final for a
run-time retry. This is the ready-fix lead surfaced by the P39.9 investigation (the adapter's tool-calling
itself was exonerated for the available models; see [roadmap.md](roadmap.md)). The behavior is preserving
where the allocation matches — the common well-resourced case still serves `config`/`config`. Tests: the old
`TestInitContextWindowNativeOllamaWithConfigSkipsProbe` (which pinned the now-removed skip) is replaced by four
cases in `contextwindow_test.go` — VRAM-limited downgrade to the loaded value, honored-config staying
`config`, unreachable-Ollama keeping config + non-final, and reachable-but-not-loaded keeping config +
non-final — against a fake native-endpoint server. `go test ./internal/server/...` green. The remaining open
half of P39.9 is the repro-gated prefill-latency observability gap.

**P40.1 — `env`/`printenv` dropped from the read-only shell allowlist (plan-mode secret-leak fix).**
`internal/tool/builtin/shell_readonly.go` classified `env`/`printenv` as `CapRead` via
`readOnlyShellArgv0`, so under plan mode a model could run `shell {"command":"env"}`, have it
auto-approved as read-only, and pull the daemon's process environment — which holds the provider API
keys (`config.loadDotEnv` `os.Setenv`s `.aegis/.env`, `ProviderAPIKey` reads `os.Getenv`) — straight
into the transcript and SQLite session store before the `CapNetwork` egress gate ever fires. The two
argv0 entries are removed (with a comment recording why they must not return), so the commands now fall
back to the normal `CapExecute` approval flow. They are low value as read-only anyway. Tests:
`env`/`printenv`/`printenv <key>` now assert `false` in `TestReadOnlyShellCommand`
(`internal/tool/builtin/shell_readonly_test.go`).

**P40.2 — `write_file`/`edit_file` preserve an existing file's mode on overwrite.**
`internal/tool/builtin/file.go` previously hardcoded `0o644` on every write, so overwriting or editing a
mode-sensitive file (a `0700` script, a key/token file) silently dropped the exec bit and widened it to
world-readable — while parent dirs were made `0o750`. Both tools now route through a `writePreservingMode`
helper that `os.Stat`s the target and reuses its permission bits when it already exists, falling back to the
named `newFileMode` (0o644) only for create-new. A Unix-only test asserts a `0700` file keeps its mode
across both `write_file` and `edit_file` overwrites, and a fresh file lands at `newFileMode`.

**P40.3 — `read_file` bounds its allocation to what a bounded read returns.** The tool used to `strings.Split`
the entire file (up to the 50 MiB `maxReadBytes` cap) into a `[]string` before applying `offset`/`limit`, so a
`limit:20` read of a large file still allocated every line. It now scans with a `bufio.Scanner` and a custom
`splitLinesKeepFinal` split func — which reproduces `strings.Split(data, "\n")` semantics exactly, including
the trailing empty final line for a file ending in a newline and preserving CRLF bytes — and stops once
`offset+limit` lines are emitted. A 10-case table test renders each input through both the new path and a
reference oracle mirroring the old renderer and asserts byte-identical output (trailing newline, no trailing
newline, empty file, CRLF, blank lines, offset/limit windows, offset-past-EOF).

**P40.4 — stray repo-root `*.err` files.** Already handled in the prior codebase-review commit (files dropped,
`*.err` added to `.gitignore`); verified none remain tracked or on disk.

**P40.5 — `internal/tui/tui.go` decomposed from 4,731 to 2,285 lines.** Pure code motion into three new
same-package files, no logic change: `view.go` (the `View`/`render*` rendering layer, 733 lines), `stream.go`
(`applyStreamBatch`/`applyEvent` and the pending-tool-card lifecycle, 509 lines), and `update.go` (the
`Update` message-routing switch, 1,249 lines). Imports were resolved with `goimports`; `go test
./internal/tui/...` stays green. The finer per-message-domain split of the `Update` switch is left as
opportunistic follow-up.

**P40.6 — `engine.Run` nudge/guard bookkeeping folded into a `nudgeState` helper.** The three parallel
counters (`guardRetries`, `zeroToolNudges`, `emptyAnswerNudges`) and the matching trio of terminal
retraction if-blocks became a single `nudgeState` struct with a `retractAll(conv)` method. Behavior is
unchanged — same retractions, same guards, same order — and the eval golden transcripts show **no** diff
(`go test ./internal/eval/...`), which is the safety net the refactor was gated on.

**Last updated:** 2026-07-21 — **P39.5, P39.6, P39.7, P39.8 shipped and P39.9 partially shipped** — the
harness-side drive-loop fixes root-caused by the P38.1 conformance re-test (see below). These land the code;
the **P38.1 umbrella stays open** pending a live re-test that confirms the built-in `--skill` drive now
reaches a verify-clean suite on a local model. Earlier the same day: **P38.6 and P38.7 shipped** (the two
actionable engineering findings split out of the P38.1 re-test); **P39.1, P39.2, and P39.4 shipped;
P39.3 spiked and closed NO-GO** (all from a local-14b-model harness-improvement research pass — see
[roadmap.md](roadmap.md)).

**P39.7 — no-progress guard turns "announce then yield" into an "act now" nudge.** The drive loop
(`internal/cli/chat.go`) previously just counted three consecutive zero-tool turns and stopped. It now
tracks whether a turn actually *mutated a suite file* (`write_file`/`edit_file`/`multi_edit`) or *changed the
PENDING marker set* — the two signals of real progress — and when a turn does neither while markers remain,
re-prompts with an explicit `actNowNudge()` ("STOP NARRATING — ACT NOW … call `edit_file` now … one section,
one edit") prepended to the continuation, bounded to `maxNoProgressTurns` (3) consecutive stalls before
stopping. Direct evidence this is the right lever: adding an "act now" preamble to a stalled `gpt-oss:20b`
run landed its first real `edit_file` in the P38.1 corroboration. Tests: `TestActNowNudge`, `TestSameStrings`,
`TestMutatingTools` in `internal/cli/chat_drive_test.go`.

**P39.5 — the drive stops re-sending the whole SKILL.md every turn.** Root cause of P38.1's unmet
conformance: `aegis chat --skill` prepends the ~9K-token SKILL.md body to the first user message, which
threads through the conversation and rides *every* request (`prompt_bytes≈31534` at turn 0), so on a 32K
local window the recon digest plus a few reads left no room to `edit_file` (a scaffolded resume made 86 tool
calls across 3 iterations and cleared 0 of 23 markers). After the opening turn — when the model has already
seen the full instructions — `compactFirstSkillMessage` rewrites the first message once, swapping the skill
body for a compact pointer (`compactSkillPreamble`) that names the on-disk `SKILL.md` to re-read on demand,
the same disposable-skill-reference logic P36.2 already applies to skill-reference *reads*. A new exported
`engine.Conversation.Invalidate()` keeps the cached token estimate correct after the in-place rewrite. Guarded
to fire only while the message still carries the preamble. Tests: `TestCompactSkillPreamble`,
`TestCompactFirstSkillMessage`.

**P39.6 — the drive's done-condition is now "verifies clean," not "all markers filled."** When the drive's
PENDING markers hit zero it now runs the threat-modeling skill's bundled phase-6 checks (`verify.py`,
`lint_dfd.py`, `inventory.py --check`) against the completed run directory; on failure it feeds the failure
text back with `verifyFixPrompt` for an in-place fix and re-runs, bounded to `maxVerifyRounds` (3). This is the
autonomous analogue of SKILL.md §5's fix-and-re-run round — the duplicate threat ID, tier↔prerequisite
mismatches and stale counts that shipped uncaught in the re-test were all flagged by `verify.py`, which
nothing was running. Gated on the skill actually bundling a `verify.py` and a run directory existing, so other
skills are unaffected (`ran=false` → the pre-P39.6 "markers cleared = done" path). New code in
`internal/cli/chat_verify.go`; `pythonExe` probes `--version` so Windows' `python` App-execution-alias shim
can't make every drive spuriously "fail verification." Tests: `TestVerifyFixPrompt`,
`TestVerifySkillOutputsGate`, `TestLatestThreatModelRunDir`, `TestVerifySkillOutputsRuns`.

**P39.8 — a proven-broken LLM summarizer is latched off for the rest of the run.** Compaction and
`output_guard` route to `provider.small_model` when set (existing), but with only a weak main model the
summarizer returns empty and the engine re-tried it two calls per compaction cycle forever (**42×** "summarizer
returned empty output" in one run). `internal/engine/engine.go` now tracks cumulative LLM-summarizer failures
per run and, past `summarizerGiveUpThreshold` (4), latches the LLM summarizer off and compacts deterministically
(P36.2 fallback) for the rest of the run — the P28.4 two-consecutive-failure fallback still fires meanwhile, so
context always keeps shrinking. Per-run state, never carried across runs. Test:
`TestProactiveCompactionLatchesOffSummarizer` in `internal/engine/contextnotice_test.go`.

**P39.9 (partial) — `/v1` compat drives now warn before overflowing; the native-adapter hang stays open.**
The actionable half shipped: `aegis chat --skill` on the legacy OpenAI-compat (`/v1`) Ollama adapter — which
cannot send `num_ctx`, so `context_window` is ignored and Ollama serves the modelfile default — now probes the
served window up front and, when it's too small for a skill-driven prompt, prints a notice naming the fix
(`warnCompatDriveWindow` / `compatDriveWindowNotice` in `internal/cli/chat.go`), including a runnable
modelfile-derivative recipe (`providerfactory.LegacyOllamaModelfileRecipe`: `printf 'FROM <m>\nPARAMETER
num_ctx <n>\n' | ollama create <m>-ctx<n> -f -`) for when the native adapter can't be used. Tests:
`TestCompatDriveWindowNotice`, `TestLegacyOllamaModelfileRecipe`. The **native-Ollama-adapter half — no tool
call / no run directory after 8+ minutes on the skill-preload turn — remains open**: it is investigation-gated
(needs a focused repro: think-mode? oversized system prompt?) and was not touched, so P39.9 stays open for
that half.

**P38.6 — thinking-mode models fabricate a completed drive instead of executing it.** The P38.1 re-test
found that `aegis chat --skill threat-modeling` with `provider.think: true` drove **zero** real tool calls:
qwen3:14b narrated the whole multi-phase build inside its `thinking` trace and reported all seven files
written and every check clean — having written nothing. Because `scaffold.py` never ran, no `<!-- PENDING`
markers existed, so the drive-to-completion oracle saw "no markers" and stopped as if complete — a silent
false success on the shipped default config (the worst shape: a user believes they have a threat model and
have nothing). Both levers from the filing shipped, in `internal/cli/chat.go`: **(a)** a `--skill` run now
force-disables `provider.think` for the drive (with a loud stderr notice when it overrides an
explicitly-enabled setting), since the whole point of the drive is tool-executed multi-phase work that
reasoning-mode simulation defeats — precedented by the mythos-sec test that ran with think off by hand;
**(b)** a floor check hardens the oracle against any *other* fabrication path — after a completed drive,
`suiteFileCount(pendingRoot)` distinguishes "finished, every marker resolved" from "nothing was ever
written" (both leave `scanPendingMarkers` empty) and prints a notice when a drive reported completion but
wrote no files under `.aegis/`. Tests: `TestSuiteFileCount` in `internal/cli/chat_drive_test.go`.

**P38.7 — `scaffold.py`'s identical `<!-- PENDING -->` markers made `edit_file replace_all` a file-nuke.**
`scaffold.py` used to write the *same* literal `<!-- PENDING -->` marker for every fillable section of every
file. A weak model filling section-by-section naturally reached for `edit_file(old="<!-- PENDING -->", …)`,
which matched all N markers in the file; `edit_file` then errored ("occurs 12 times") or, on a
`replace_all: true` retry, **overwrote all of the file's distinct sections with one wrong string** — exactly
what corrupted architecture.md in the re-test. Fix: `scaffold.py` now emits a **unique, self-describing**
marker per section, keyed to the section (`<!-- PENDING: deployment-classification -->`), so an `edit_file`
old-string naturally targets exactly one site and `replace_all` can't blanket-nuke a file. A shared prefix
(`<!-- PENDING`) keeps every downstream substring scan working: `verify.py`'s no-leftover-skeleton check
(`SKELETON_MARKERS`) and the drive's `scanPendingMarkers` both match the prefix now, not the bare literal,
so keyed markers still count as unfinished. Coordinated across `scaffold.py` (a new `pending(key)` builder;
`table`/`prose` take a `key`; every builder passes a section key), `verify.py`, `internal/cli/chat.go`
(`scanPendingMarkers` + `continuePrompt`, which now warns off `replace_all`), and SKILL.md §4.1/§4.2 wording.
Verified: all seven frameworks scaffold with zero duplicate or bare markers per file, a fresh scaffold still
lints 6/6 (`lint_dfd.py`) and fails `verify.py`'s leftover-marker check as before, and `scanPendingMarkers`
detects keyed markers (extended `TestScanPendingMarkers`).

**P39.1 — regression test that `effectiveSystem` is byte-stable turn over turn.** P35.7's code-reading pass
had concluded `Server.effectiveSystem` (`internal/server/helpers.go:42`) *should* render byte-identical
across turns given unchanged inputs (persona blocks, memory/context files, the skills index, the
deferred-tools list are all either static or deterministically sorted), but flagged it as unconfirmed live —
the whole KV-cache-reuse story local models depend on (P35.4's `keep_alive` residency, P35.9's stable
tool-call IDs) relies on the serialized prompt prefix staying identical turn to turn, and nothing would have
caught a future regression (an unsorted map range, a nondeterministic file walk) before a live run did.
Added two tests to `internal/server/server_test.go`: `TestEffectiveSystem_ByteStable` (two calls with
identical inputs must produce identical output) and `TestEffectiveSystem_DeferredToolsOrderIndependent` (the
sharpest case — `tool.Registry.Deferred()` at `internal/tool/tool.go:160-171` ranges a Go map and relies
entirely on a trailing `sort.Slice`; registering the same two tools in reverse order across two registries
must still produce byte-identical `deferredToolsBlock` output). Pure test addition, no product code changed
— the `sort.Slice` was already correct.

**P39.2 — coach tool-execution error messages for weak local models.** Two independent, small changes
targeting failure classes the P38.1 live tests actually reproduced (mythos-sec:24b inventing tool names,
running bare script paths without an interpreter prefix). (a) `engine.executeTool`'s unknown-tool branch
(`internal/engine/engine.go`, ~line 1453-1456) now returns `unknown tool %q; registered tools: <sorted,
comma-joined names>` instead of a bare name — via a new `registeredToolNames` helper using
`tool.Registry.All()` — so a model that invents a name can self-correct from the error itself instead of
guessing again next turn. Extended `TestRunUnknownTool` (content assertion) and added
`TestRunUnknownTool_ListsRegisteredNames`. (b) the shell tool (`internal/tool/builtin/shell.go`) now appends
an interpreter hint on failure only — e.g. `(did you mean to run this with an interpreter, e.g. `python
recon.py`?)` — when a failing command's first token has a known scripting extension (`.py`/`.sh`/`.js`/
`.rb`) and isn't already prefixed by a known interpreter; never touches the success path or blocks
execution. Added `TestShellFailedScriptHintsInterpreter` and `TestShellFailedNonScriptNoHint` (guards
against over-eager hinting).

**P39.3 — investigation spike into grammar/schema-constrained tool-call decoding on the Ollama adapter:
closed NO-GO.** A live Ollama server was reachable (`qwen2.5-coder:1.5b`, `qwen3:14b` pulled), so the spike
sent real `/api/chat` requests instead of relying on docs. Baseline: `qwen3:14b` with only `tools` set
returns a proper native `tool_calls` array. Adding a `format` JSON-schema field to the *same* request
(alongside the same `tools` array) changes the result completely — the model returns plain schema-conforming
`content` with **no `tool_calls` field at all**, reproduced identically on both models tested. **Ollama's
`format` and native tool-calling are mutually exclusive on one request** — `format` cannot be layered on top
of `tools` to constrain tool-name/argument generation while still getting native `tool_calls` out, so the
originally-scoped "smallest useful win" (constrain tool name via `format`, no new request fields) isn't
actually free. No code shipped — the client-side reject-and-inform alternative is what P39.2 already ships.
A larger, distinct idea (route shaky models through `format`-only prompting with a dynamically-built
tool-call-envelope schema, reusing the existing tool-call-as-text fallback parser in
`internal/engine`/`internal/engine/toolcallastext_test.go`) is noted as an unfiled lead in
[roadmap.md](roadmap.md), not built or filed as a follow-up item, since its value depends on how often Aegis
actually drives models that need it. See roadmap.md's P39.3 section for the full spike transcript and
reasoning.

**P39.4 — `aegis doctor --deep`'s structured multi-turn fill probe.** `toolcallprobe.Run`'s existing
single-turn smoke test only answers "did a structured tool call come back at all" — the P38.1 arc found
qwen3:14b passes that cleanly and still fails a real multi-phase scaffold-and-fill skill run, one level up
(losing track of which of several near-identical `<!-- PENDING -->` sections it already filled, blanket
`edit_file replace_all` footguns, and think-mode fabrication). Added `internal/toolcallprobe/deepprobe.go`:
a self-contained (no `internal/eval` dependency) `RunDeepFill(ctx, adapter, model) (DeepResult, error)` that
drives a real `internal/engine` agentic loop — the same tool-calling loop a real session uses, not a second
hand-rolled one — through a tiny in-memory synthetic document (3 sections, each stubbed with the same
`<!-- PENDING -->` marker `internal/cli/chat.go` already uses) and one fake `edit_fill` tool deliberately
mirroring `edit_file`'s real semantics (`old_string` must occur exactly once unless `replace_all` is set, so
the P38.7 footgun reproduces faithfully). `DeepResult{FabricatedCompletion, ClobberedMarkers, TimedOut}`
reports the three P38.1-observed failure shapes independently, never folded into the existing binary
`Verdict`. Wired into `aegis doctor` as an opt-in `--deep` flag (`internal/cli/doctor.go`): a new
`doctorDeepFillCheck` row, gated to local (Ollama-style) providers only, WARN-not-FAIL on any probe failure,
strictly additive — `aegis doctor` with no flag is byte-for-byte unchanged. Tests: four scripted-adapter
cases in `internal/toolcallprobe/deepprobe_test.go` (clean pass, each failure shape in isolation) plus
`internal/cli/doctor_test.go` coverage that the row only appears with `--deep`, skips for cloud providers,
and degrades to WARN on transport failure — all deterministic, no live model needed for `go test ./...`.
Live-verified against a real `qwen3:14b` (native Ollama, `think` disabled): `aegis doctor --deep` first hit
its initial 90s timeout budget mid-probe (cold model load plus several fill turns exceeded it — WARN, not a
crash, confirming the degrade-gracefully contract), then, after bumping `deepFillCheckTimeout` to 4 minutes,
completed cleanly end-to-end with a `PASS structured multi-turn fill` row.

Earlier 2026-07-21 — **P38.1's conformance re-test was executed (negative on qwen3:14b); P38.6
and P38.7 filed.**

**P38.1 re-test — the linear threat-model build does not reach a verify-clean suite on qwen3:14b, even with
P38.4 scaffolding.** This was the remaining work on P38.1 (a live-run verification, not a code change): with
`scaffold.py` shipped, re-run `aegis chat --skill threat-modeling` on the config-default local model
(qwen3:14b, native Ollama) against `D:\Development\AiGateway` and check for a verify-clean seven-file suite.
Result: **negative, in two `think`-dependent failure modes.** (1) With `provider.think: true` (the default),
the model made **zero real tool calls** — it narrated the entire build inside its reasoning trace and
returned a final answer claiming all seven files were written and every check script passed clean, having
written nothing; because `scaffold.py` never ran there were no `<!-- PENDING -->` markers, so the
drive-to-completion oracle stopped as "complete" (`turns:3, tool_calls:1`). (2) With `think` off, it ran
`recon.py` and `scaffold.py` (writing all seven files — **live-confirming the P38.4 mechanism**), but then
skipped the `date` step (scaffolding into SKILL.md's literal *example* timestamp dir), hit the
`max_tokens: 8192` output cap on one turn, and ran `edit_file(old="<!-- PENDING -->", replace_all=true)`
which **overwrote all 12 of architecture.md's identical section markers with one wrong string**, then looped
on failing edits until loop-detection aborted (turn 11); five of seven files stayed all-PENDING and
`verify.py` failed. **Takeaway:** P38.4 moved qwen3:14b's failure from "authoring structure" (fixed) to
"incrementally filling it"; the model is still too weak to converge, and the stronger `qwen3.6:35b-a3b` MoE
is not installed to try the "or a stronger local" arm. The two *actionable* engineering findings are filed
as new Tier-2 roadmap items — **P38.6** (thinking-mode models fabricate a completed drive instead of
executing it) and **P38.7** (`scaffold.py`'s identical `<!-- PENDING -->` markers turn `edit_file
replace_all` into a file-nuke). No code shipped for this entry — it records a verification result and files
follow-ups. See [roadmap.md](roadmap.md).

Earlier 2026-07-21 — **P38.3 and P38.5 shipped (both Tier 3).**

**P38.3 — per-turn usage promoted onto the `turn_done` event, everywhere it was silently dropped.**
`engine.Event{Kind: KindTurnDone}` already carried `*provider.Usage` (including P35.7's
`PromptEvalDurationMS`, the KV-cache-hit signal), and the daemon's `toAPIEvent` already forwarded
`InputTokens`/`OutputTokens`/cache counts to `api.Event` for the SSE path the TUI/web UI read — but two
gaps meant a run's turn-over-turn context growth still wasn't externally observable without SQLite
spelunking or debug-log tailing: (1) `PromptEvalDurationMS` itself was never on the wire in `api.Event`
at all, so even the daemon SSE path couldn't tell a KV-cache-hit turn from a full reprocess without
reading the debug log; (2) `aegis chat --output-format stream-json`'s `emitStreamEvent` had no `case`
for `KindTurnDone` — it fell through the switch with `Type: "turn_done"` set and *nothing else*, so a
one-shot scripted run's only usage figure was the final `result` trailer, never a per-turn number. Fixed
both: `api.Event` gained `PromptEvalDurationMS`, populated in `toAPIEvent`; `streamEvent` gained the same
usage fields (`input_tokens`, `output_tokens`, cache counts, `prompt_eval_duration_ms`, `cost_usd`),
populated on `KindTurnDone`. Turn-over-turn growth across a long scripted run (not just the single
aggregate) is now readable from a `stream-json` pipe or the SSE stream directly. Tests:
`TestToAPIEventTurnDoneCarriesPromptEvalDuration` (server), `TestEmitStreamEventTurnDone` (cli).

**P38.5 — a model that rejects `think` now degrades instead of aborting the run.** The 2026-07-20 test
found `supergoatscriptguy/mythos-sec:24b` 400s the instant Aegis sends `think` at all
(`"...mythos-sec:24b" does not support thinking"`), killing the run before a single tool call. The native
Ollama adapter (`internal/provider/ollama`) now retries once, automatically: `Stream` first tries the
request with the configured `think` value; on an HTTP 400 whose body contains "does not support
thinking" **and** only when a non-nil `think` was actually sent (so an unrelated 400 — malformed request,
model not found — never triggers it and never masks itself behind a second failure), it logs a
`slog.Warn` naming the model and retries once with `think` omitted entirely. `ollama.WithLogger` (default
`slog.Default()`) wires the daemon's real logger through `providerfactory`. This does not make such a
model viable on its own — mythos-sec:24b with thinking disabled still can't drive tools — it only removes
a misleading, run-killing error for models that happen to reject the parameter. Tests:
`TestStreamRetriesWhenModelRejectsThink`, `TestStreamDoesNotRetryOtherBadRequests` (ollama).

Earlier — **P38.4 shipped: deterministic skeleton scaffolding (`scaffold.py`).** The
threat-modeling skill gained a sixth bundled script, `scaffold.py`, that pre-writes all seven report files
**from the skeletons** — real structure (every heading, every table's header row + separator, the fixed
value lists, the DFD's `flowchart LR` header and three `classDef`s) with a `<!-- PENDING -->` marker per
fillable section — so a weak local model **fills sections** (via `edit_file`) instead of authoring the
structure it gets wrong. It closes the exact gap the 2026-07-20 qwen3:14b live test exposed: the 14B model
skipped the skeleton templates, so its files lacked the required tables/headings, `verify.py` failed 6/10,
and it re-authored freeform structure on every self-correction pass instead of converging. SKILL.md §4.1
step 2 now calls `scaffold.py` instead of hand-writing bare stubs, and §4.2 tells the model to fill
`<!-- PENDING -->` markers one section at a time rather than regenerate whole files. Validation: a
freshly-scaffolded suite already passes `lint_dfd.py` 6/6, and a minimally-filled one passes `verify.py`
9/9 (only the intentionally-unfilled DFD stub's PENDING marker trips the leftover-syntax check) — proving
the scaffolded structure is verify-clean once filled, so self-correction now converges against a real
structure. The script is stdlib-only, deterministic (reads no clock — timestamps stay PENDING), and never
clobbers a file whose PENDING markers are already gone. It supports all six frameworks (plus `stride-a`).
This unblocks **P38.1**'s remaining work: a re-test of the linear build to a verify-clean suite on a
capable local model. See [roadmap.md](roadmap.md).

Earlier today — **P38.2 shipped and the P38.1 linear build was live-tested.** `aegis chat`
gained **`--skill <name>`**: it preloads the named skill's full body into the prompt (so a small local
model never has to discover-and-fetch it via the `skill` tool — the P36.1 skip) and **drives the run to
completion** — after each yield, while any file under `.aegis/` still carries a `<!-- PENDING -->` marker,
it appends a "continue, don't stop" turn on the *same* conversation and re-runs, bounded by `--max-turns`
(default 40) and a no-progress guard. It also adds the `MaterializeBuiltins` call `aegis chat` was missing
(only the daemon did it before), so a scripted run's builtin skill body and bundled scripts are on disk.
The **live test** (qwen3:14b vs AiGateway) confirmed the P38.1 linear build's mechanism — one context, no
orchestration, **no `{mode,agents}` mis-route**, `recon.py` → all seven files → the P37 check scripts,
inside the context window (~44K input tokens / 33 tool calls) — but the 14B output does **not conform**
(it skips the skeleton templates, so `verify.py` fails 6/10 and it can't self-converge). That gap is the
new Tier-1 **P38.4** (deterministic skeleton scaffolding); mythos-sec:24b proved a dead end (400s on
`think` → new **P38.5**, and can't drive tools even with thinking off). See [roadmap.md](roadmap.md).

Earlier today — the **P38.1 linear-build rework of the threat-modeling skill shipped.**
`SKILL.md` §4 no longer delegates the build through the `agent` tool's `mode:"sequential"` workflow: it
now instructs the driving model to build all seven files itself, in one context, phase by phase in
dependency order, carrying only a short running note of stable identifiers between phases. The
`agent`/`mode:"sequential"` call block, the terse-final-answer contract, and the shared-pool time budget
(all of which existed only to serve orchestration) are removed; the phase *ordering* and per-file
structure are kept. Context stays bounded by the four levers that were already doing the real work —
recon's ~11KB digest, P36.2 pruning of spent write/read payloads, incremental section-at-a-time writes,
and the deterministic P37 scripts. `references/verification-and-updates.md`'s "Phased-orchestration
governance" section was rewritten to "Single-context build governance" and the update-workflow paragraph
de-orchestrated to match; the debate step (§5) stays, reframed as a standalone `agent` `mode:"debate"`
call at depth 1 (not a phase-6 sub-agent at depth 2). What remains **open** is the live verification that
a full seven-file linear build actually stays inside the context window on the target local models — that
needs **P38.2** (chat drive-to-completion) and **P38.3** (per-turn telemetry) plus a live run. See
[roadmap.md](roadmap.md).

Earlier today, a **P38.1 first fix** (SKILL.md §4.2 `agent`-call callout + a `skill`-tool corrective
guard) was shipped and then **superseded by the rework above** after three live runs proved it
insufficient: qwen3:14b mis-routed the workflow payload to `ls` (guard never fires there) and hand-wrote
an incomplete suite with a false "complete" claim, and mythos-sec:24b couldn't even invoke `recon.py`
(shell flailing) and loop-aborted. **Neither tested local model (14B/24B) can drive the phased
multi-agent workflow** — which is why orchestration is abandoned for local models and the phased `agent`
path is parked (still available for capable cloud/large models, no longer the default). See
[roadmap.md](roadmap.md). Also today: **P37.6 shipped** (two threat-model script fixes from a live
dogfood eval — see its entry below), and the **P36 live-verification of P36.1-P36.3 was attempted** on a
real local model (qwen3:14b) for the first time: P36.1 (deterministic skill load) and P37.1 (`recon.py`)
confirmed live, but P36.3's phased orchestration is **refuted** on that model, so the debt is **not
retired** and **P38.1-P38.3** were filed. Earlier: **P37.1-P37.5 shipped** — the
threat-model suite-scripting batch is complete — five bundled stdlib scripts (`recon.py`, `inventory.py`,
`verify.py`, `lint_dfd.py`, `diff_inventory.py`) that codify the mechanical parts of the threat-modeling
skill and leave judgment to the model (see the P37.x entries below). This is the work that lifts the
Aegis builtin past the `.claude/skills/threat-model-analyst` sibling it was benchmarked against. Earlier:
**P36.1, P36.2, and P36.3 shipped**. **P36.3**: the threat-modeling
skill's build stages are now phased through the `agent` tool's `mode: "sequential"` workflow — each
phase runs in a fresh, isolated sub-agent context, loads only its own reference file(s), writes its
own report file, and returns only terse stable identifiers (not file content) to the next phase —
instead of one long-lived, ever-growing run, bounding peak input context per request on local models.
Verifying the sequential-workflow mechanics strengthened the case: the 10-min cap is a *shared pool*
across phases (`maxAgentDuration*(phases+1)`, ~70 min for six phases), not a per-phase cap, so a heavy
phase can run past 10 min, and the spawn depth stays within the depth-3 ceiling. Live local-model
verification of the peak-context win is still outstanding. **P36.1**: skill-triggering slash commands
(`/threat-model`, `/report`, `/research`, `/review`) now inject the activated skill's body
deterministically instead of relying on the model to call the `skill` tool first — closing the Tier 1
gap where a small local model skipped the load and lost the instruction; a pre-existing Windows-only
skills-test failure was fixed in passing. **P36.2**: `compaction.pruneStaleToolResults` now also blanks
confirmed `write_file`/`edit_file` payloads and one-time skill-reference reads in the pre-`keepRecent`
prefix (live token-growth re-measurement still outstanding). Earlier: **P35.13 fully shipped** (its
final open piece, the summed-token-surface decision, resolved today — see the P35.13 entry below).
Earlier: **P35.12 and P35.8 shipped**. **P35.12**: two native-Ollama stream
cosmetics from the P35.9-filing review. `errorMessage` (`internal/provider/ollama/ollama.go`) no
longer surfaces raw JSON when an error envelope is an object without a `message` field — it now also
tries `error`/`detail` string fields and, failing those, compacts the object into a single tidy line
rather than dumping raw multi-line bytes (it still never swallows a present error into ""). Second,
because the native path delivers each tool call *whole* on one NDJSON line, a tool-call argument
payload over the shared 4MiB scanner cap (`internal/provider/sse/sse.go`) previously failed as the
opaque `bufio.Scanner: token too long`; `consume` now detects `bufio.ErrTooLong` and emits an
actionable error naming the cause (an oversized tool-call payload past the 4MiB line limit). Table
tests cover the error-fallback shapes and a >4MiB line. **P35.8**: exit-trace instrumentation for
`aegis chat` (`internal/cli/chat.go`) after a live run once vanished mid-turn leaving nothing on
disk — no panic, no signal record, no final answer. Three seams now log to `aegis.log`: a deferred
`recover` writes the panic value + `debug.Stack()` before re-panicking (registered after the
log-closer defer so LIFO ordering flushes the log before the file closes); the run context now comes
from an extracted `installSignalCancel` helper that logs *which* signal fired (Ctrl-C or SIGTERM,
portable) before cancelling, replacing a bare `signal.NotifyContext(os.Interrupt)` that recorded
nothing; and "run starting"/"run finished" boundary markers bracket `eng.Run`, so a silent
disappearance now shows as a start with no matching finish. The signal helper is unit-tested via a
split-out `watchSignal` (no real OS signal needed). No behavior change beyond the panic re-raise.
Earlier the same day: **P35.10 and P35.11 shipped**, closing out Tier 2. **P35.10**:
`InputTokens` on the native-Ollama path is the tokens Ollama actually evaluated this turn
(`prompt_eval_count`), which with P35.4 KV-cache residency is only the *newly appended* delta on a
cache-hit turn (37 after turn 1's 3944, per P35.7) — the truthful "prefill work done" number, not
the full prompt/context size. That shift in meaning was undocumented. A consumer audit over every
`InputTokens` reader confirmed the billing/budget/work paths (`internal/cost`, engine run usage,
turn traces, session totals, all `in=` displays) are correct under this meaning, and compaction
already avoids it (the proactive check uses `conv.estimatedTokens()`, not usage). The one genuine
"context size" consumer — the TUI's context-fullness bar (`renderContextBar`) — understates on a
native-Ollama cache-hit turn; left as-is (display-only, no compaction/cost impact; a correct fix
needs an estimated-context number the daemon doesn't yet surface to the UI) with the caveat
documented at the call site, the mapping site (`internal/provider/ollama/ollama.go`), and the
`Usage.InputTokens` doc (`internal/provider/provider.go`). No behavior change. **P35.11**:
`probeProviderReachability` (`internal/server/provider_health.go`) fired a live Ollama
`GET /api/version` on every `/status` poll; a 1-2s UI poll loop meant a steady upstream request
stream for a value that changes rarely. The probe result (reachable + latency) is now cached for a
3s window under a mutex, so a fast poll loop coalesces to at most one upstream request per window;
the actual probe runs outside the lock (it can block on a 2s timeout), and a same-tick cold race
just writes an equivalent entry. Tests (with an injected clock seam and a counting fake Ollama
server) assert coalescing, expiry-triggered re-probe, and clean behavior under `-race` with 32
concurrent callers. Earlier the same day: **P35.9 shipped**: the native-Ollama adapter's `translate()`
(`internal/provider/ollama/ollama.go`) resolved tool-result names from a single ID→name map built
over the *entire* message history, last write wins. Because `consume` mints tool-use IDs from a
counter that resets every request (`tu_0`, `tu_1`, …), the same ID recurs across turns naming
whatever tool was called first each time — a normal shape for a mixed-tool agentic run (e.g.
read-file in turn 1, run-shell in turn 3, both minted as `tu_0`). That collision meant every
earlier turn's tool result got silently relabeled with a later turn's tool name, both misleading
the model about which tool produced which result and mutating the serialized prefix between
requests — defeating Ollama's KV-cache prefix reuse (the fourth cache-invalidation candidate that
P35.7's non-determinism sweep didn't catch, since it only checked same-index-same-tool runs).
Fixed by resolving each `ToolResultBlock` against the nearest *preceding* `ToolUseBlock` in message
order instead of a whole-history map — correct regardless of ID reuse, requires no change to ID
minting, and repairs already-stored sessions with colliding IDs on next read. Regression test
(`TestTranslateReusedToolIDsResolvePositionally`) covers both the mislabelling and a byte-stability
assertion that turn 1's serialized prefix is unchanged by appending turn 2. Earlier the same day:
**P35.7 live-confirmed**: a real `aegis chat` run against Ollama
(`qwen3:14b`, resident via `keep_alive`) doing a STRIDE threat model of an external repo captured 8
turns of `prompt_eval_count`/`prompt_eval_duration_ms`. Turn 2 needed only 37 new prefill tokens
after turn 1's 3944 (103ms), and turns 5-8 held the same pattern as context grew from 17.6k to 19k
tokens (`prompt_eval_duration_ms` tracking each turn's token *delta* at ~2-4.7ms/token, not the
running total — turn 8 processed 19038 total context tokens in 3.3s, far below what a full
reprocess at the observed per-token rate would take). This resolves the question the diagnostic was
filed to answer: Ollama's KV-cache prefix reuse **is** working under P35.4's `keep_alive` residency,
so P35.5's timeout was genuinely about the ceiling being too low for large one-off prefill jumps
(e.g. a tool result dumping a large file listing), not about repeated full-context reprocessing. No
response-header timeout or error occurred at any point in the run, so P35.6's actionable-error path
went untested live but also unneeded. Follow-up fix shipped alongside: `aegis chat` never wired
`cfg.LogLevel` into a real logger, so this debug-level prompt_eval instrumentation was invisible on
the one CLI path most likely to be used for this exact diagnostic — `internal/cli/chat.go` now
builds a `logging.New` logger and passes it into `engine.New`, mirroring the existing
`serve`/`acp`/`mcp-serve` pattern. (Original diagnostic-only writeup, no live data, is preserved
below for the record.) Earlier the same day: **P35.6**: when P35.5's response-header timeout fires on the
native-Ollama or OpenAI-compat path, the bare Go transport string
(`net/http: timeout awaiting response headers`) — indistinguishable from a dead server, naming no
remedy — is now rewrapped into an actionable, non-retryable error naming the cause (prefill on a
local backend slower than the configured budget) and the levers (raise
`provider.response_header_timeout`, lower `context_window`, reduce per-turn context growth),
mirroring P35.2's context-truncation precedent. Earlier the same day: **P35.5**: native-Ollama
agentic runs no longer die outright on a
large-context prefill — `provider.response_header_timeout` (seconds) now lets a slow-prefill local
box raise the shared 5-minute HTTP response-header timeout that every provider adapter's streaming
client enforces, discovered when a live `/threat-model stride` re-run on the doctor-recommended
native-Ollama setup got 5 turns / 27 tool calls / ~62k input tokens deep and still died with
`net/http: timeout awaiting response headers`. Default unchanged (5 minutes) so nothing changes
unless a user opts in. Earlier the same day: **P35.1-P35.4**: the four
stacked failures found running the threat-modeling skill against an external repo on the
doctor-recommended local setup (Ollama, qwen3.6:35b). `aegis chat` now wires configured built-in
skills into its tool registry (P35.1); context-limit truncation mid-tool-call surfaces an
actionable "raise provider.context_window" error instead of an opaque JSON-parse failure (P35.2);
`aegis doctor` calibrates its recommended `context_window` against the model's real
training-context max instead of a fixed 16GB-safe 32768 (P35.3); the threat-modeling skill now
steers toward bounded/chunked large-file reads (P35.4, skill half); and the native Ollama path now
defaults `keep_alive` to a bounded 30m resident window so a multi-turn run keeps the model loaded
and reuses its KV cache across turns instead of reprocessing the whole conversation each turn
(P35.4, provider half). Earlier: **P33.21 and P33.22**: ACP now
surfaces `KindToolCallStart` as a
`pending` tool-call notification that the matching `KindToolCall` upgrades in place, `bg events`
prints the same start timing, and `escPending` was renamed to `backtrackArmed`. Earlier the same
day: **P33.12**: the first-run wizard and `/security-config` editor now
composite over the live chat via `renderOverlay`, the same treatment the approval dialog (P33.6),
transient panels (P33.11), and completion popup (P33.18) already use, instead of replacing the
frame outright. Earlier: **P34.11**: grype reinstated into the multiscanner image for tool
centralization, which was P34.11's own activation trigger, so the parked build-artifact-exclusion
fix shipped with it. Earlier: **P34.12**, the last Tier 2 item: osv-scanner's exit-128 "no package
sources" refusal, which turned out to need two-way disambiguation rather than the one-way mapping
its own filing proposed. Earlier the same day: **P34.9** and **P34.10**, clearing the rest of Tier
2 — njsscan's Windows traceback (a libsast bug, not the semgrep gap the item diagnosed) and
trivy's silent npm dev-dependency skip. Earlier still: **P34.5-P34.8**, the previous Tier 2 batch.

---

