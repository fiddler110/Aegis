# Aegis Release History — Part 2 (continued from releases-01.md)

Continuation of the newest-first `## Latest changes` record list. Start at
[releases.md](../releases.md) for the index, or [releases-01.md](releases-01.md) for the newest entries.

---

### Injected memory files stop paying for their own authoring notes, 2026-08-21 (P74.15)

**Ranked first on the Up next table once the menu lane, the motion group and the harness lane's first
three rows had all shipped, leaving it the sole remaining Tier 2 entry.** `Sources.Load`
(`internal/memory/memory.go`) is the one function that injects `memory.md` into the system prompt —
project and user scopes both, unfiltered — and it runs on every turn. Any HTML comment left in either
file, whether a hand-authored note or a future tool-managed delimiter, was billed against
`localBasePromptCeilingTokens` (test-enforced by `TestEffectiveSystem_localProfileBudget`) exactly as if
it were content the model needed to see.

**What changed.** A new `stripHTMLComments`, backed by a single `regexp.MustCompile("(?s)<!--.*?-->")`,
strips comment spans — including ones that cross lines — from each file's text. It runs inside
`loadDirect`, applied to the output of `readMemoryFileChecked` before `wrapMemoryFile` wraps it in the
untrusted-provenance marker, so the integrity check still hashes and verifies the file's real,
unstripped bytes; only the copy handed to the model loses the comments. `Append` and the on-disk file
are untouched — this is purely a projection at injection time, the same shape `deepagents`' memory
middleware uses for AGENTS.md-style files.

**Why `Sources.Load` and not `FormatEntries`.** The roadmap entry flagged this explicitly: P67.5's
`LoadRelevant`/`FormatEntries` path has no production callers at all — memory reaches the prompt through
`Sources.Load()` today, which is the function that actually runs. Changing `FormatEntries` instead would
have been correct-looking and inert.

**What this unblocks.** A tool that maintains a section of `memory.md` can now leave a delimiter comment
(e.g. `<!-- managed:start -->` / `<!-- managed:end -->`) without spending prompt budget on it every
session — the mechanism P74.15 was filed to make viable, not built here.

**Tested:** `TestLoadStripsHTMLComments` (`internal/memory/memory_test.go`) appends a real entry, then
hand-appends a single-line and a multi-line HTML comment plus a trailing visible line directly to the
on-disk file, and asserts `Load()` drops both comments' text while keeping the visible line — and that
the on-disk file itself still contains the comment afterward, proving the strip is injection-time only.
The existing suite (`TestLoadEmpty`, `TestAppendAndLoad`, `TestLoadWrapsUntrustedProvenance`,
`TestAppendPrunesOldestEntriesWhenOverCap`, `TestSaveSkillAndLoad`) and
`TestEffectiveSystem_localProfileBudget` (`internal/server/server_test.go`) both still pass unchanged —
`go test ./internal/memory/... ./internal/server/... -run 'TestLoad|TestAppend|TestSaveSkill|TestEffectiveSystem_localProfileBudget'`.

### A dangling call whose arguments never parsed gets its own message, 2026-08-20 (P74.14)

**Ranked first on the Up next table once the menu lane and the motion group both closed out and left
the harness lane's remaining rows all parallel.** `repairOrphanedToolUses` fills in synthetic
`tool_result` blocks for any `tool_use` a round left unresolved — the routine path any of Aegis's own
cancellation mechanisms take (`MaxTurnStall`, `MaxWallClockPerRun`, a user interrupt, a TUI quit mid-
stream) since every one of them cancels the run context mid-flight by design. P65.1 already split that
wording on whether `Engine.startedTools` recorded the call as having reached `Execute`: "may have
partially completed" for a call that started, "did not run" for one that did not.

The "did not run" half was overloaded. It is the right claim for a call cut off before its round got to
it — resuming the round would very plausibly have run it. It is the wrong claim for a call whose
`Input` never parsed as JSON at all: truncated mid-argument by the same cancel, or malformed to begin
with. Nothing about resuming would have let that call dispatch — no round state was in its way, its
arguments were. Reporting it as "interrupted; did not run" invites the model to retry the exact same
call, which fails the same way again; the model needs to be told to reissue it with valid arguments
instead. On local models, whose streamed tool-call JSON is truncated far more often than a cloud
model's, this is not a rare case.

**The fix is one more branch in the same three-way switch**, keyed on the block's own `Input` rather
than on anything computed at the call site: `!json.Valid(tu.Input)` alongside the existing `started`
lookup. A new `interruptedMalformedText(name)` reads "tool call never dispatched; NAME's arguments were
malformed or truncated JSON. Reissue the call with valid arguments." — deliberately not calling it
"interrupted", since it would be false regardless of the interruption. Order matters: `started` is
checked first, so a call that *did* reach `Execute` before the round was cut off keeps the "may have
partially completed" wording even if its `Input` looks malformed in isolation — a tool that got as far
as `Execute` is the tool's own business to have validated, and the runtime's `started` record is
stronger evidence than a static read of the JSON.

**Existing behaviour is unchanged for both other branches.** The started/not-started split from P65.1
is untouched, and a clean orphan with no started record still gets the pre-existing "did not run"
wording — pinned by `TestRepairOrphanUsesNotStartedWordingWithoutARecord`, which now uses valid-JSON
fixture input specifically so it exercises that branch rather than the new one. A new
`TestRepairOrphanDistinguishesMalformedFromInterrupted` covers the new branch directly, asserting a
malformed and a clean orphan in the same round get different wording and that neither message bleeds
into the other's.

Touches only `internal/engine/engine.go` (`repairOrphanedToolUses`, `interruptedMalformedText`) and
`internal/engine/orphanrepair_test.go`. `go test ./internal/engine/...` green, including the full P65.1
suite this builds on.

### A running swarm gets a stable colour, not three grey lines, 2026-08-20 (P74.13)

**Independent of every other row, and the last one left once the motion group closed out.** The
sidebar's `AGENTS` section (`internal/tui/view.go`) rendered every running teammate through
`m.th.tool` with the id truncated to eight characters, so a three-agent swarm was three lines of the
same colour differing only in a hash prefix. Nothing else in the UI tied a sidebar line, a
`/teammates` transcript row, or a status segment back to which agent produced it.

**A fixed 8-colour palette per `colorScheme`, indexed by hashing the agent id.** `agentPalette
[8]color.Color` is a new field on `colorScheme` (`internal/tui/colorscheme.go`) — eight Charmtone hues
for `darkScheme` (purple, blue, green, yellow, red, pink, cyan, bright blue), and eight hex colours
already used for other light-scheme roles for `lightScheme`, so the light palette stays inside that
scheme's existing contrast work rather than adding new unvetted hexes. `applyScheme` copies it into a
package-level `colAgentPalette`, and a new `agentColor(id string) color.Color` hashes the id with
`hash/fnv`'s FNV-1a and indexes into it:

```go
func agentColor(id string) color.Color {
	h := fnv.New32a()
	_, _ = h.Write([]byte(id))
	return colAgentPalette[h.Sum32()%uint32(len(colAgentPalette))]
}
```

Hashing rather than assignment order means the same id renders the same colour across a restart and
across every render site, with no state to thread through the sidebar, transcript, or status bar.

**Two render sites now use it.** The sidebar `AGENTS` list styles each running teammate's label with
`lipgloss.NewStyle().Foreground(agentColor(tm.AgentID))` instead of the flat `m.th.tool`.
`renderTeammates` (`internal/tui/tui.go`, the `/teammates` transcript listing) renders the agent id
substring in that same colour while keeping the tag's existing status colour (red `✗` for failed, the
default for done/running) — so a teammate's identity colour is consistent with the sidebar without
losing the failure signal that colour already carried.

**Tests:** `internal/tui/colorscheme_test.go`'s `TestAgentColorStableAndDistinct` — repeated calls for
the same id return the same colour, and two different ids land on different palette entries. `go build
./...` and `go test ./internal/tui/...` both clean.

### The token counter jumps instead of climbing, 2026-08-20 (P74.12)

**The row Up next ranked first once the menu lane and the harness lane's first two rows had all
shipped**, and the last of the three prerequisites it had (honouring the P74.10 reduced-motion flag) was
already satisfied the day it was filed. `renderStats` printed `m.inputTokens`/`m.outputTokens` straight
from the last counter update, and those only change on `KindTurnDone` (`internal/tui/stream.go`) — once
per model turn, with the full new total — so a multi-turn tool round trip read as the number stuttering
in chunk-sized jumps rather than climbing.

**`animStep` was already the right clock; the counters just weren't riding it.** Two new `model` fields,
`displayedInputTokens`/`displayedOutputTokens` (`internal/tui/tui.go`), sit alongside the real
`inputTokens`/`outputTokens` and are what `renderStats` now formats. `model.easeStatCounters`
(`internal/tui/update_tick.go`) is called from `updateSpinnerTick` on every streaming tick, independent
of `followBottom` — cheap to update even when the redraw itself is suppressed, so the counter is already
caught up by the time the view scrolls back into frame:

```go
func easeTowards(displayed, target int) int {
	gap := target - displayed
	switch {
	case gap == 0:
		return displayed
	case gap > 0:
		if step := gap / 8; step > 1 {
			return displayed + step
		}
		return displayed + 1
	default:
		if step := gap / 8; step < -1 {
			return displayed + step
		}
		return displayed - 1
	}
}
```

An eighth of the gap moves fast when far behind and slows as it converges, and the ±1 floor guarantees it
always reaches the target in a bounded number of frames rather than stalling asymptotically short of it.
Reduced motion (P74.10) skips the ease entirely and snaps both counters to the true values every tick, so
a reduced-motion run still prints the real number immediately as the item required. The counters are also
snapped directly to the true values in `updateStreamClosed` and `updateErr`
(`internal/tui/update_stream.go`) and reset to zero alongside `inputTokens`/`outputTokens` at both
existing reset sites (`internal/tui/tui.go`, `internal/tui/update_slash.go`'s `/clear`), since ticks stop
firing once `m.streaming` goes false and nothing would otherwise finish an in-flight ease.

**Tests:** `internal/tui/stat_ease_test.go` — a streaming tick moves the displayed counters partway
toward a large target without reaching it, repeated ticks converge exactly without overshoot, and reduced
motion snaps both counters to the true values on the very first tick. `go build ./...`,
`go vet ./internal/tui/...` and `go test ./internal/tui/...` all clean.

### Stall becomes a visible ramp, not just an abort, 2026-08-20 (P74.11)

**The row Up next ranked first once P74.10 shipped**, since P74.11 was gated on the reduced-motion flag
it added. Between "working" and the 900s `MaxTurnStall` abort the TUI showed exactly one thing — a
shimmer identical at second 2 and second 400 — which is indistinguishable from a hang on a local model,
where a 90-second silence during prompt evaluation or post-tool-round re-evaluation is routine.

**The clock already existed per wait phase; only the color was static.** `model.phaseStatus()` already
distinguishes three states — waiting for the first token (`streamStart`), a post-tool-round re-eval
(`modelWaitAt`), and actively generating — and a new `model.stallElapsed()` (`internal/tui/tui.go`) reads
whichever of those two wait clocks is currently running, returning zero once the model is producing
output: tokens arriving is forward progress, not a stall, so the ramp only ever applies to genuine dead
air.

**`stallRampColor(elapsed, bound time.Duration) color.Color`** (`internal/tui/shimmer.go`) interpolates
`colAccent` toward `colWarning` via the same `lipgloss.Blend1D` `shimmerText` already uses, fed by
`elapsed` from `stallElapsed()` and `bound` from a new `Config.MaxTurnStall` — the same
`cost.max_turn_stall` value the engine aborts a stalled turn against
(`cfg.Cost.MaxTurnStall()`, wired through `tui.Run` from `internal/cli/root.go`), so the ramp tracks the
real bound rather than an invented one. The ramp is deliberately front-loaded (`sqrt(t)`) and saturates
at 70% of the bound rather than 100%, so a run reads as visibly "getting stuck" well before it would
actually abort — mirroring the comparison client's continuous spinner-to-red creep rather than a
threshold flip. A disabled bound (`max_turn_stall: 0`) or zero elapsed leaves the color at `colAccent`,
unchanged.

Wired at both places the "working" shimmer renders: the transcript tail's phrase
(`internal/tui/tui.go`, the `case m.firstTokenAt.IsZero()` / `case !m.modelWaitAt.IsZero()` branches) and
the status line (`internal/tui/view.go:renderInputArea`). Neither call site needed its own clock — both
already had `m.animStep` for the sweep and now also pass `stallRampColor(m.stallElapsed(),
m.maxTurnStall)` as the highlight instead of a fixed `colAccent`.

**Tests:** `internal/tui/stall_ramp_test.go` — `stallRampColor` returns `colAccent` unchanged with a
disabled bound or zero elapsed, moves monotonically as elapsed grows, and holds steady past its 70%
saturation point rather than overshooting; `stallElapsed` reads the waiting-phase clock, drops to zero
once `firstTokenAt` is set (generating, no stall signal), and picks back up from `modelWaitAt` during a
post-tool-round re-eval. `go build ./...`, `go vet ./internal/tui/... ./internal/cli/...` and
`go test ./internal/tui/... ./internal/cli/...` all clean.

### There is no reduced-motion setting → fixed, 2026-08-20 (P74.10)

**Filed to gate P74.11 and P74.12, taken first as the roadmap directed.** Nothing in `internal/tui` read
a motion preference: `shimmerText`, `caretGlyph`, the cycling `thinkingPhrase` and every pending tool
card's shimmer frame all animated unconditionally whenever `m.streaming && m.followBottom`, with no
config key and no way to turn any of it off short of not using the tool — an accessibility gap (the
shimmer is a continuous moving-luminance sweep) and a CPU one (`updateSpinnerTick` re-rendered the whole
tail and every pending card on every ~100ms tick, on a machine that may simultaneously be running local
inference).

**All four animations share one clock, so freezing it in one place freezes all four.** `shimmerText`,
`caretGlyph` and `thinkingPhrase` each take `m.animStep` as their frame argument, and
`updatePendingToolCards` (P21.2) is driven from the same tick. `updateSpinnerTick`
(`internal/tui/update_tick.go`) already gated the whole block on `m.followBottom` (P3.7); reduced motion
adds one more gate around the frame advance itself:

```go
if m.followBottom {
    m.pollTick++
    if !m.reducedMotion {
        m.animStep++
        m.updatePendingToolCards()
        m.refresh()
    }
    if m.pollTick%20 == 0 {
        cmds = append(cmds, m.fetchTeammatesQuiet())
    }
}
```

`m.animStep` stops advancing, so `shimmerText` renders one static gradient instead of a sweep,
`caretGlyph` lands on whichever half of `caretBlinkPeriod` it was last at (frame 0 by construction, since
`animStep` starts and stays at zero — a solid caret, never blinking), `thinkingPhrase` stops cycling, and
`updatePendingToolCards`/`refresh()` — the expensive per-tick work, not `m.sp.Update` itself — never run,
which is where the CPU saving actually comes from. The spinner tick keeps being re-queued regardless (the
existing "always re-queue so animation resumes on scroll-back" comment), so nothing about scroll-back
recovery or the tick's own liveness changes.

**One non-animation consumer rode the same counter and had to be split off.** The P2.5 sub-agent roster
poll fired on `m.animStep%20 == 0`; freezing `animStep` at 0 would have made that condition true on
*every* tick instead of every twentieth once reduced motion no longer advances it past zero. Given its
own counter, `m.pollTick`, incremented unconditionally in the same block, so the poll's cadence is
unaffected by the motion setting.

**Wired the standard way**: `TUIConfig.ReducedMotion` (`internal/config/config.go`, `koanf:"reduced_motion"`,
default `false` — picked up automatically as `AEGIS_TUI_REDUCED_MOTION` via the existing env layer, no
bespoke env check needed) → `tui.Config.ReducedMotion` (`internal/cli/root.go`) → `model.reducedMotion`,
set once in `newModel` the same way `Config.Mouse` sets `mouseOff` (P74.19). Documented in
[docs/configuration.md](../docs/configuration.md) beside `mouse`.

**Tests:** `TestReducedMotionFreezesAnimStep` / `TestMotionDefaultAdvancesAnimStep` (control case) /
`TestReducedMotionStillPollsTeammates` / `TestReducedMotionCaretStaysStaticOnce`
(`internal/tui/reduced_motion_test.go`). `go build ./...` and `go test ./internal/tui/...
./internal/config/... ./internal/cli/...` are green.

### An empty tool result becomes a named placeholder, 2026-08-20 (P74.9)

**First of P74.9's two repairs, taken alone as filed** — the second (per-model argument-shape repair)
was explicitly deferred to P74.17 in the same item and does not ship here. A tool that legitimately
returns nothing — a `grep` with no matches, a `read_file` on an empty file — used to hand the model an
empty string, indistinguishable from a failed call. Many local models cannot tell the two apart and
re-issue the call, which reads as a loop and can trip the P52.3 failure breaker or the loop detector for
a reason that has nothing to do with either.

**`builtin.NormalizeEmptyResult(toolName, content string) string`** (`internal/tool/builtin/truncate.go`,
beside the P64.3 posture table it belongs with) returns `content` unchanged unless it is empty or
all-whitespace, in which case it returns a one-sentence placeholder naming the tool and stating plainly
that an empty result is not necessarily a failure. This is correct for every model — an empty result is
exactly as ambiguous to a capable model as to a small one, it is just less likely to act on the
ambiguity — so unlike the argument-shape half it is unconditional, not gated on `LocalProfile`.

**Wired at the one seam every tool call passes through**, `Engine.executeTool`
(`internal/engine/engine.go`): applied only when `!isErr`, after the existing `err`-to-`isErr` conversion,
so a real error keeps its own message and only a genuinely empty *success* gets the placeholder. Both
call paths — `runToolsSequential` and the parallel `toolRound.run` — dispatch through `executeTool`, so
one change point covers both.

**Does not touch the loop detector**, by construction rather than by a special case: the turn signature
`loopDetector` compares is built from each surviving call's name and canonicalized *input* only, never
from result content (see `SignatureTransparent`'s doc in `internal/tool/tool.go`), and
`toolFailureTracker` counts `IsError`, not an empty string. `NormalizeEmptyResult` is also deterministic —
the same tool name always produces the same placeholder — so two empty results from the same call are
exactly as indistinguishable to the signature as two empty results were before this item, which is the
property the closure condition asked for.

**Tests:** `TestNormalizeEmptyResultReplacesOnlyEmptyContent` and
`TestNormalizeEmptyResultIsDeterministic` (`internal/tool/builtin/truncate_test.go`) at the function level;
`TestExecuteToolNormalizesEmptySuccessResult` and `TestExecuteToolLeavesErrorResultsAlone`
(`internal/engine/engine_test.go`) at the engine seam, the latter confirming an empty *error* result is
left alone rather than rewritten into the success placeholder. `go build ./...`, `go vet ./...` and
`go test ./internal/tool/builtin/... ./internal/engine/...` are green.

### A tool call written as text becomes a call, 2026-08-20 (P74.8)

**Head of the harness lane, taken as soon as the menu lane closed.** `internal/provider/openai/openai.go`
reads `tool_calls` off the wire and nothing else — when a local model answers a tool-enabled turn with a
fenced JSON object, a tagged block, or a bare `{"name": ..., "arguments": ...}` narrated in prose, the
turn produces plain text, the engine sees no call, and the loop either stalls or retries blind against a
model that already answered.

**`provider.WithProseToolCallSalvage`** (`internal/provider/prosetoolcall.go`) is a new decorator beside
`retry.go` and `numctx.go`, but a mirror-image one: every other decorator in the package forwards events
as they arrive, because it only needs to look at the request. This one needs the whole assembled reply
before it can tell a genuine text answer from a mis-emitted call, so it buffers one turn's stream,
decides, then either replays it completely unchanged or replaces the buffered text with the parsed call
plus whatever prose survives it. That is a real trade — no live token-by-token display on the turns it
rewrites — accepted because P74.17's per-model profile mechanism is the intended place to turn this on
per model; until it exists, the existing local-profile boolean is the only lever there is, so it gates
construction in `providerfactory.Build` (`internal/providerfactory/factory.go`) and never touches a cloud
turn.

**Two details carried over from the `deepagents` reading, both load-bearing:**

- **A parsed name must match a tool actually sent on that request** (`req.Tools`, not the tool registry
  at large). A reply that only mentions `read_file` in a sentence never becomes a call — there's no
  call-shaped JSON to find, so the scan simply finds nothing.
- **Surviving prose is never discarded.** The matched call's own JSON span is stripped out of the
  buffered text; everything else — narration before or after it — is re-emitted as the turn's text
  content.

**Parsing, in order of how explicit the model was:** a `<tool_call>`/`<function_call>` tag, then a fenced
` ```json ` block, then a bare JSON object found by walking every `{` in the reply and asking
`encoding/json`'s streaming decoder whether a balanced object starts there. A candidate's `arguments`
(also accepted as `parameters`/`input`) is normalized whether the model nested it as an object or encoded
it as a JSON string — both shapes show up in local-model output — and defaults to `{}` rather than a nil
`Input` when the field is missing.

**A call that already arrived structured is left completely alone** — the decorator only inspects the
buffered text at all once it has confirmed no `EventToolUse`/`EventToolUseStart` occurred, so a turn that
narrates and then makes a real call keeps its narration and its call exactly as the base adapter produced
them.

**Tests** (`internal/provider/prosetoolcall_test.go`), the closure condition's table test over each
malformed shape: fenced JSON, the `<tool_call>` tag, a bare object, string-encoded arguments, a
mention-only reply (the negative case — asserts no call and the original `StopEndTurn`), a call naming a
tool that wasn't offered on the request (also no call), a real structured call replayed byte-for-byte,
and a request with no tools at all (never scanned, whatever the text contains). `go build ./...`,
`go vet ./...` and `go test ./internal/provider/... ./internal/providerfactory/...` are green.

Related but distinct, and not conflated: the qwen3:14b Ollama-template issue drops a call from *history*
after it was correctly parsed — a different bug with a different fix. This item is the general defence
for the family of "the call never parsed as structured at all," not that specific one.

### The grep/bulk-scope permission gap, 2026-08-20 (P74.1)

**Filed and shipped the same day, proved against the real gate rather than read off `subjectFor`.** A
deny rule intended to keep a directory out of the model's context held for `read_file` and was a silent
no-op for `grep`, which returned matching lines from the same files with no denial and no warning.

`subjectFor` (`internal/permission/rules.go:183`) extracts the string a rule's glob matches against by
switching on capability. The `CapRead` branch returned `firstNonEmpty(args.Path, args.FilePath)`.
`grep`'s schema (`internal/tool/builtin/search.go:301`) has neither — it takes `pattern`, `glob` and
`ignore_case`, and always searches the whole workspace root (`effectiveRoot(ctx, t.root)`) with `glob`
as the only narrowing — so the extracted subject was always the empty string, `normalizePathLike("")`
cleaned to `"."`, and no path pattern matched it.

**The safety net missed it for a reason worth recording separately from the fix.**
`WarnUnmatchableRules` asks `toolHasSubjectField`, which introspects the tool's *declared input schema*
for any of the six names in `subjectFieldNames`. `grep` declares `pattern`, which is on that list, so
the check passed — but the `CapRead` branch of `subjectFor` returned before `pattern` was ever
consulted. **The schema-level check and the extraction-level switch disagreed, and the warning was
defeated precisely where it was needed.**

**Evidence before the fix.** A throwaway test against `NewRuleGate` with a stub carrying `grep`'s real
schema and capability: rule `deny grep(secrets/**)`, input `{"pattern":"AWS_SECRET"}` →
`allowed=true`, `reason=""`, and `WarnUnmatchableRules` emitted nothing.

**The fix classifies each filesystem tool by scope rather than patching the `CapRead` extraction.**
Adding `glob`/`pattern` to that branch was the smaller change and was rejected as wrong in the general
case — a `glob` is a filter, not a scope, and a `grep` with no `glob` at all still walks everything.
Instead, `grep`/`glob`/`ls` are now classified as **bulk**-scope (the path argument is a search root
and any descendant may surface) as opposed to **exact**-scope (`read_file`, `write_file`, `edit_file`,
where the call operates on the named path), following the shape `deepagents`' `_fs_interrupt.py` uses.
A bulk call fires whenever the searched subtree *intersects* a rule's pattern, and unconditionally when
the call names no root at all — matching what a blast-radius rule means, and reusing the fact that
Aegis's globs already span path separators (`globToRegexp`). The same shape closed
`security_scan`/`latex_build` (path-only `CapExecute` tools) and `project_knowledge`/`entity_recall`
(query-only `CapRead` tools).

**Closure condition, both halves.** `TestRuleGateDenyBlocksPathlessGrep` asserts `deny
grep(secrets/**)` denies a pathless `grep`. `TestSubjectExtractionAgreesWithSchemaForEveryRegisteredTool`
asserts the schema/extraction agreement directly — for every registered tool, if `toolHasSubjectField`
says a rule can match it, `subjectFor` must return non-empty for some input satisfying its schema. The
second is what stops the regression class rather than the one instance: the next tool with the same
"scope is a search root, not a named field" shape reintroduces the gap silently unless this test would
catch it too.

**Tests.** `internal/permission/rules_test.go` and the new `internal/permission/subject_agreement_test.go`.
`go test ./internal/permission/...` and `go build ./...` are green.

### Six framed regions become one, 2026-08-20 (P74.2)

**Filed as a document-flow rewrite, twice, and corrected the same day to something an order of
magnitude smaller before it was built.** The comparison client ships *two* rendering modes, and the P74
batch had been read against the public one; `src/utils/fullscreen.ts:112`'s `USER_TYPE === 'ant'` check
is what the internal-staff mode runs, and it is alt-screen — the architecture Aegis already has. The
gap was never the rendering model, it was the chrome and the quality of the in-app implementations. See
[Decisions that outlive the items that made them](roadmap.md#decisions-that-outlive-the-items-that-made-them)
for the full correction record, including the two wrong answers that preceded it and why `rawScrollback`
stays exactly as it was.

`renderChat` used to compose six framed regions: a title bar, a bordered sidebar column, a scrollbar
glyph column, the transcript viewport, a todo strip, and a rounded-bordered composer over a status line.
Alt-screen and the bounded viewport stayed — they're what let the app own every cell, which is what
makes resize re-wrap work. Everything else was the actual target.

**The sidebar became an overlay instead of a layout member.** It used to be joined into `renderChat` via
`lipgloss.JoinHorizontal`, reflowing and shrinking the transcript pane by `m.sidebarW + 1` columns every
time it opened. It now composites over the finished frame in `render()` via `renderAnchoredOverlay` —
the same mechanism P33.11/P33.12 established for the transient panel and the wizard — drawn full-height
at the screen's left edge, as the lowest overlay layer so every modal dialog still lands on top of it.
`layout()` no longer reserves any width for it at all; `m.sidebarOpen` already defaulted to Go's zero
value (`false`), so "off by default" was already true — what changed is that opening it stopped
perturbing anything underneath.

**The scrollbar column auto-hides.** `renderScrollbar` used to draw a full track/thumb whenever content
was scrollable, regardless of scroll position. It now also requires `!m.followBottom` — the column
renders blank while pinned to the bottom (the normal state) and only shows a track once the user has
actually scrolled away, the way a GUI overlay scrollbar behaves. The column's width stays reserved
either way, deliberately: making it truly zero-width while hidden would itself perturb transcript
geometry on every scroll-position change, which is the exact reflow this item exists to remove.

**The title bar is gone; its content folded into the status line.** `renderTitleBar` rendered the brand
mark and the P28.7 connection/model badge as their own always-visible row. That function is deleted;
`renderBrandSegment` produces the same content as one more entry in `renderInputArea`'s existing
priority-ordered, tail-dropping segment list (`joinedWidth`), given the *highest* priority so it's the
last thing to drop on a narrow terminal — it's the one signal that used to have a dedicated row.

**The mouse-coordinate math needed the same correction, or selection would silently land on the wrong
cell.** `paneOrigin()` (`internal/tui/selection.go`) used to add one row for the title bar and
`m.sidebarW + 1` columns whenever the sidebar was open. Both assumptions were now wrong: the title-bar
row is gone (origin row 0, not 1), and the sidebar no longer shifts the transcript at all, since it
overlays rather than reflows. But an overlay *occludes* — the transcript pane is still geometrically
present under it, just not visible — so a click in that band must not resolve to the (hidden) content
beneath. `toPaneCoord` and `clampPaneCoord` gained a `sidebarOccludes(x)` check: a click under the
overlay's columns is now treated as outside the pane, and a drag that crosses under it clamps to the
overlay's right edge instead of the transcript's own left edge.

**`layout()` and `fixedH()` lost the sidebar-width and title-bar-row terms** they used to carry, freeing
one full row of viewport height (title bar) that the transcript pane now gets back, in addition to the
sidebar's reclaimed width — the item's own note that "sidebar plus scrollbar plus padding is about 30
columns of an 80-column terminal, permanently" no longer holds once the sidebar isn't part of that
budget.

**Explicitly out of scope, and this was the point of the correction:** `tea.Println`, a commit/live
split, retiring `/search`, and deleting `selection.go`. None of those survive reading the two-mode fact
instead of the public build's behavior. `rawScrollback` is untouched — the sidebar overlay is
additionally gated on `!m.rawScrollback`, matching the existing suppression of the scrollbar and
terminal pane there.

**Closure condition, verified.** A fresh install shows no sidebar, no scrollbar while pinned to the
bottom, and no title bar; the sidebar overlay opens and closes without perturbing transcript geometry
(`TestSidebarOverlayDoesNotChangeTranscriptGeometry` asserts `m.transcript.Width()`/`Height()` are
identical before, during, and after); resize still re-wraps (`TestResizeStillRewrapsAfterChromeRemoval`
drives real `WindowSizeMsg`es and asserts the pane narrows and widens back); `/search`, drag-selection
and `ScrollToItem` all still work (existing coverage, re-run green); the scrollbar hides while following
bottom and shows once scrolled away (`TestScrollbarAutoHidesWhilePinnedToBottom`); the brand/connection
content survives in the status line with no separate title row
(`TestTitleBarFoldedIntoStatusLine`); and a click under the sidebar overlay no longer resolves to
transcript content (`TestSidebarOverlayOccludesPaneMouseCoords`).

**Tests.** New coverage in `internal/tui/chrome_test.go`. `TestPaneOriginAndToPaneCoord` and
`TestScrollbackModeHidesSidebar` (pre-existing) were updated for the new geometry rather than left
broken — the former asserts the sidebar overlay leaves the origin unmoved and now occludes instead of
shifting it; the latter is unaffected by the mechanism change since it already asserted on rendered
content, not layout math. `go build ./...`, `go vet ./internal/tui/...` and `go test ./internal/tui/...`
are green.

### One tool block, not two events, 2026-08-20 (P74.3)

**Unblocked the same day P74.2 shipped**, and taken next per the Up next table. A completed tool call
used to render as two independent transcript lines, both leading with the tool name:
`renderToolCall` produced `● read_file  internal/x.go`, then `renderToolResult`
(`internal/tui/toolview.go`) produced `✓ read_file → …`. On a round with a dozen calls that is a dozen
redundant identifiers, and the pair reads as two events rather than one block with an outcome.

**What changed, concretely.** `renderToolResult`'s header no longer echoes the tool name — the call
block above it already carries that — and instead renders `{tag} ⎿` as a continuation gutter. The
single-line branch (short results) drops the name the same way. The multi-line branch keeps its
existing per-call line cap (`maxBodyLines`, driven by `toolCompact`/`/tools full`) but changes what
happens when a result would need truncating: instead of a chopped body plus a "▶ N more lines" footer,
it now collapses to one line — `N lines  (/tools full to expand)` — and `/tools full` (which raises the
cap past the result's line count) is the unchanged expand path. A result that already fits inside the
active cap, compact or full, still renders through the same body path as before, `renderReadFileResult`'s
chroma-highlighted read view included — the specialized renderers were kept working verbatim rather than
routed through a new summary mechanism, since P16.2/P16.3's diff and highlighting work is what's most
likely to break on a gutter change.

**What didn't need to change.** `renderToolCardDone` already composed the call block and the result
into one transcript item (P21.2), and `toolCompact` already existed as the per-session compact/full
toggle — the harder half of this item was already built; what was missing was the header shape and the
truncation-vs-collapse decision.

**Tests.** `TestRenderToolResult_HeaderDropsRepeatedName` (new) asserts neither the single-line nor the
multi-line branch repeats the tool name and that both carry the `⎿` gutter.
`TestRenderToolResult_CollapsesToSummaryWhenOverCap` (new) asserts a 20-line result under a 10-line cap
renders as a single collapsed summary line naming `/tools full`, and that raising the cap (the `/tools
full` state) renders the complete body again. The pre-existing `renderToolResult` tests
(`TestRenderToolResult_ReadFileUsesPathForHighlighting`, `TestRenderToolResult_SanitizesDangerousSeqs`,
etc.) all use results short enough to stay under their line cap, so they exercise the unchanged
full-body path and pass without modification. `go build ./...`, `go vet ./internal/tui/...` and
`go test ./internal/tui/...` are green. Unblocks P74.4.

### Mouse capture becomes a config choice, not a package deal, 2026-08-20 (P74.19)

**The last row of the selection/clipboard group**, taken once P74.20 closed the SSH case that had been
this item's strongest justification. What's left is the narrower audience the item was always framed
for: people who specifically want the terminal, not Aegis, to own click-drag selection — a `tmux`/
`kitty` copy-mode workflow, mainly.

`View()` (`internal/tui/view.go`) previously derived `tea.MouseMode` from exactly one thing:
`rawScrollback`. `/scrollback` releasing mouse capture is a side effect of releasing alt-screen and the
bounded-viewport transcript rendering together — there was no way to release capture on its own. A new
`tui.mouse: off` config key (`config.TUI.Mouse`, plumbed through `tui.Config.Mouse` to a `model.mouseOff`
field set once at startup) adds that missing combination: `View()` now sets `MouseModeNone` when
*either* `rawScrollback` or `mouseOff` is set, independently of `AltScreen`, which stays on when only
`mouseOff` is set. That's the property `/scrollback` structurally can't offer — resize re-wrap keeps
working, because the bounded-viewport transcript rendering (the thing that actually defeats native
scrollback, per P22.6's investigation) never changes.

**Deliberately no in-session toggle.** `rawScrollback` has `/scrollback` because it's a mode swap
someone might want mid-session; `mouseOff` is read once from config, because the costs (no wheel scroll,
no click-to-focus, `selection.go`'s drag-to-copy goes idle) are the kind of tradeoff worth deciding once
per setup rather than per session.

**Settled 2026-08-20, and stays settled:** off by default. The wheel-scroll trade was put to the user
when P74.20 was scoped and declined in favor of the OSC 52 fix, which solves the SSH clipboard case
without costing anything. This item is the escape hatch for the narrower audience left after that,
not a reopening of the default.

**Closure condition, verified.** `TestMouseOffKeepsAltScreenButReleasesCapture`
(`internal/tui/scrollback_test.go`) confirms `Config{Mouse: "off"}` produces a `View()` with
`AltScreen == true` and `MouseMode == tea.MouseModeNone`. `TestMouseOffDefaultsOn` confirms an unset
`Config.Mouse` leaves capture on, matching pre-existing behavior.

**Not done in this pass, and deliberately:** the roadmap item flagged that `Update` (`update_compose.go`)
forwards only `pgup`/`pgdown` to the transcript pane while the composer holds focus, so `GotoTop`/
`GotoBottom` and the half-page keys are unreachable by keyboard during typing. Checked against
`bubbles/v2/textarea`'s own `KeyMap` (`ctrl+u` → delete-before-cursor, `ctrl+d`/`delete` → delete-char-
forward, `home`/`ctrl+a` → line-start, `end`/`ctrl+e` → line-end) — every other candidate key is already
claimed by line editing, which is why P21.7 restricted forwarding to `pgup`/`pgdown` in the first place.
Widening it would silently break typing. `pgup`/`pgdown` already reach every position by repetition, so
this is left as-is rather than reopening P21.7's key-ownership split.

**Tests.** `internal/tui/scrollback_test.go`. `go build ./...` and `go test ./internal/tui/... ./internal/config/... ./internal/cli/...` are green.

### OSC 52 becomes the primary clipboard path, 2026-08-20 (P74.20)

**Ranked first in Up next once P74.18 shipped** — a silent-wrong-result bug, not a preference, and it
fixes `/copy` in addition to every mouse-drag copy affordance, which a mouse-capture change (P74.19)
does nothing for.

`copyToClipboard` (`internal/tui/view.go`) switched on `runtime.GOOS` and shelled out to `pbcopy` on
darwin, `xclip`/`xsel`/`wl-copy` on linux, and `clip.exe` on windows. Every one of those talks to the
clipboard of the machine the process is running on. Run Aegis over SSH, in a container, or in WSL
reaching a Windows terminal, and that machine is not the one at the user's keyboard — the copy silently
"succeeds" and the text lands somewhere unreachable, or on a headless linux box the tool lookup fails
outright.

**OSC 52 asks the terminal emulator itself to set the clipboard**, so it crosses SSH, tmux and
containers by construction, and bubbletea v2 already carries it: `tea.SetClipboard(s)` returns a `Cmd`
that the framework's own event loop turns into `ansi.SetSystemClipboard` and writes synchronized with
the next frame (`clipboard.go`, `tea.go:813`) — no new terminal-writing code needed in `internal/tui`.
`copyToClipboardCmd` now batches that command with a synthetic success `clipboardResultMsg` for any
payload at or under `maxOSC52Payload` (50,000 raw bytes, chosen so the base64 encoding stays under
tmux's historic 74,994-byte OSC 52 buffer cap with margin). There is no synchronous way to learn whether
a terminal honoured the sequence — unlike a read, a set has no ack — so within the limit the command is
treated as best-effort success, matching how the codebase already treats other one-way OSC writes (the
window-title sequence in `View()`).

**Above the threshold**, `copyToClipboardCmd` skips OSC 52 entirely and falls back to the original
native-tool path unchanged, since a sequence that gets silently truncated is worse than one that was
never sent.

**Kept deliberately separate**: this is a deliberate emission on a trusted, user-initiated path (a drag
selection or `/copy`, both actions the user took), and must not be confused with
`termsafe.StripDangerousSeqs`, which strips the same OSC 52 sequence from *untrusted* model/tool output
(P28.1) so a scanned file or model response can't hijack the clipboard. The two run in opposite
directions on opposite trust boundaries and stay in their own files.

**Closure condition, verified.** `TestCopyToClipboardCmdUsesOSC52` (`internal/tui/clipboard_osc52_test.go`)
confirms a payload under the limit returns a batch carrying both a `tea.SetClipboard`-shaped message
(identified structurally — bubbletea's `setClipboardMsg` is unexported) and a successful
`clipboardResultMsg`. `TestCopyToClipboardCmdFallsBackAboveOSC52Limit` confirms a payload over the limit
returns the native-tool `clipboardResultMsg` path instead of a batch.

**Tests.** `internal/tui/clipboard_osc52_test.go`. `go build ./...` and `go test ./internal/tui/...` are
green.

### Selection stops fragmenting over chroma color, 2026-08-20 (P74.18)

**Filed out of the P74.2 correction, ranked first in Up next despite being Tier 2**, because it lands
directly on the capability the direction decision just named important, and it is the one outright bug
in the selection path.

`selection.go:305`'s drag-selection overlay highlighted the selected range with
`lipgloss.NewStyle().Reverse(true)` — SGR-7, which swaps a cell's foreground and background *as the
terminal currently has them set*. Over uniform text that reads fine. Over chroma-highlighted content
(diffs, P16.3; `read_file` output, P16.2) it does not: each token carries its own foreground color with
no reset in between, so each one inverts to a different background and the selection reads as a ragged
stripe of mismatched blocks instead of one contiguous region.

**The fix matches what a real terminal's own selection does: replace the background, leave the
foreground alone.** A `selectionBg` role was added to `colorScheme` (`internal/tui/colorscheme.go`) —
picked for contrast against each scheme's own `fgBase`, not just eyeballed on dark: the dark scheme
blends toward `charmtone.Charple` at 0.55 for a bright fill, the light scheme uses a pale
`#C7D2FE` against its near-black text. JSON-loaded themes (`theme_loader.go`'s `toScheme`) derive theirs
the same way the existing background tiers are derived, `blend(background, brightMagenta, 0.45)`, so
every built-in and custom theme gets a value with no per-theme authoring. `renderTranscriptContent`'s
overlay now builds `lipgloss.NewStyle().Background(colSelectionBg)` instead of `Reverse(true)` and wraps
the same `ansi.Cut`-extracted selected span — the span's own embedded foreground codes survive untouched
inside the wrapper, since they only ever set foreground, never reset the background the wrapper opened.

**Left alone, deliberately:** `highlightSearchMatches` (`selection.go:284`), which already uses an
explicit `colBrandFg`/`colBrandBg` pair rather than `Reverse` — Aegis's search highlighting was already
the better of the two treatments, and only the selection overlay needed the change.

**Closure condition, verified.** `TestSelectionOverlayUsesBackgroundNotReverse`
(`internal/tui/selection_test.go`) renders a line with two differently-colored, non-reset chroma tokens
under an active selection and asserts: no `\x1b[7m` (SGR-7) escape appears anywhere in the output; the
derived `colSelectionBg` background escape does appear; and both original foreground color codes survive
inside the selected span.

**Tests.** `internal/tui/selection_test.go`. `go build ./...`, `go vet ./internal/tui/...` and
`go test ./internal/tui/...` are green.

### An exploration phase reads as a narrative, not a wall of cards, 2026-08-20 (P74.4)

**Unblocked once P74.3 shipped**, and taken next per the Up next table — the largest remaining density
win in the batch. A read-only exploration phase (`read_file`, `grep`, `glob` back to back) used to emit
one addressable card per call, unconditionally; a fifteen-call run reads as fifteen near-identical
lines with the actual decision buried under a log of syscalls.

**The grouping rule stays deliberately narrow.** A call folds into a group only once its own
`KindToolResult` has actually confirmed success — never at call time, and never for a still-pending
sibling. It extends the group in progress, or starts a new one with whatever ungrouped successful
read/search card sits immediately before it, only when that predecessor is still its literal positional
neighbor in the transcript (`transcriptPane.ItemBefore`, which skips over cards already folded away so a
group can keep growing past its own hidden members). Any error, any write/execute call, or any other
transcript item landing in between breaks the chain — the failure mode this item has to avoid is hiding
something that mattered, not maximizing how much collapses.

**Parallel rounds, decided explicitly rather than assumed.** `engine.runTools` runs read/network calls
concurrently, so a round's results can resolve out of call order. Grouping never counts a call before its
own result arrives, which means a round that resolves out of order under-groups rather than
over-claiming: three concurrent reads whose results land B, C, A merge B+C (C's positional predecessor,
once resolved, is B) but leave A on its own, since A precedes the group's actual start card rather than
following it. Fewer calls collapse than a perfectly-ordered round would allow, but nothing is ever shown
as part of a group before its result is known.

**Mechanics.** No new transcript item is created for a group — the first member's own card (already
appended, pending, at `KindToolCall` time, same as before) is repurposed in place the moment a second
member merges into it (`model.foldIntoReadGroup`, `internal/tui/stream.go`), and every later member's own
card is folded out of view with a new `transcriptPane.HideItem` (`internal/tui/transcript.go`) rather than
removed — items stay addressable (so `ItemBefore`'s adjacency walk still sees where they sat) but
contribute nothing to height, byte accounting, or output. The collapsed card
(`renderToolGroup`, `internal/tui/toolview.go`) shows one line by default — `"Read 2 files"`,
`"Searched 1 pattern, read 1 file"` — and expands to one line per call, reusing each call's own
identifying target (path, or pattern plus location), under the same `/tools full` convention P74.3
already established for an over-cap single result. A call's target descriptor is captured at
`KindToolCall` time and cached on its `toolCard` (`groupLabel`), because the engine's `KindToolResult`
does not repeat a call's input — reading it from the result event would have silently produced empty
labels.

**Left alone, deliberately:** `loadHistory`'s replay of a resumed session's stored messages still renders
one card per call, ungrouped — it doesn't go through `pendingTools`/`toolCard` at all, and grouping it is
a separate, easier problem (no streaming, no concurrency) left for later rather than folded in here.

**Tests.** `internal/tui/toolgroup_test.go` (new): two sequential successful reads collapse; an error
sandwiched between two successes breaks the group and still renders its own error card; a write call in
between breaks the group; grep+read_file summarize together distinguishing "patterns" from "files"; the
parallel-round case above renders exactly what the decision above says it should at each step; `/tools
full` expands a group to one line per call. `go build ./...` and `go test ./internal/tui/...` are green.

### Pickers drop to one selection cue, and a filter hint, 2026-08-20 (P74.5, P74.6)

**Taken together in one sitting, per P74.6's own filed direction** ("same function, same file, and the
footer's styling depends on whether the frame is still there") — the head of the menu lane, and the
most direct answer to the standing complaint that Aegis's menus feel like an application rather than a
terminal list.

**P74.5 — three "selected" signals collapse to one.** `configureDialogList`
(`internal/tui/dialog.go`) set a brand title chip — `Background(colBrandBg)`, bold, padded — on a solid
fill; `dialogFrame` wrapped every picker in a rounded primary border; `aegisListDelegate` marked the
focused row with a left `NormalBorder` bar in `colPrimary` **plus** `colPrimary` foreground **plus**
bold. Three cues for one fact, and the filled chip was the single most application-shaped element
anywhere in the UI.

- `configureDialogList`'s title is now plain bold `colPrimary` text with no background, and
  `TitleBar`'s style draws a `colSeparator` hairline rule along its bottom edge (`Border(...,
  false,false,true,false)`, width pinned to the list's own) instead of a blank padding row.
- `aegisListDelegate`'s `SelectedTitle` swaps the `NormalBorder` for a one-off `lipgloss.Border{Left:
  "❯"}` — the same left-border mechanism that used to draw the `│` bar now draws a pointer glyph
  instead — and drops `Bold(true)`, leaving the `colPrimary` foreground/border colour as the only cue.
  `SelectedDesc` is unchanged.
- `listDialog.View` no longer calls `dialogFrame`: a picker renders as `Padding(0, 1)` over plain text,
  no border and no `Background(colSurface)` fill, so the terminal's own background is the surface —
  exactly what the comparison implementation this batch was read against does. `dialogFrame` itself is
  untouched and still backs its other two callers, `approval.go` and `quitconfirm.go`, which are
  genuinely modal rather than a transient list already composited over `renderOverlay`'s dimmed
  transcript (P16.6).
- The shared-chrome comment at the top of `dialog.go`, which stated the framed/chip styling was
  deliberate and mirrored Crush, is rewritten rather than deleted: it now says why that decision
  reversed for pickers (the frame competed with the dim P16.6 already provides for modality) and names
  the two callers that still keep `dialogFrame`.

**P74.6 — the only genuinely undiscoverable interaction gets one footer line.** `configureDialogList`
already called `SetShowHelp(false)` and `SetShowStatusBar(false)`, and `newPalette`'s own comment named
the intent — "Browse mode by default; typing any character activates filtering naturally" — but nothing
on screen said so: no visible query, no hint that input was accepted, no match count.

`listDialog.View` now appends one dim (`colTextMuted`) line under the list, built by a new `footer`
method:

- Unfiltered: `type to filter · ↑↓ move · enter select · esc close`.
- Filtering or filtered: the `type to filter` clause drops out (it's no longer news once the user is
  already doing it) and a right-aligned `n/m` — `len(VisibleItems())` over `len(Items())` — takes its
  place, gapped to the list's own width; if the terminal is too narrow for both, the count is dropped
  rather than truncating either string mid-word.

**Verified by hand**, not just by test: a throwaway visual harness rendered `newPalette` in browse mode
(hairline rule, `❯ /help`, footer hint, no border) and with `SetFilterState(list.FilterApplied)` after
`SetFilterText("hel")` (fuzzy-matched rows, footer showing `24/50` right-aligned) before being deleted —
the initial version of the footer used the full unfiltered hint even while filtering, which measured out
to a negative gap at the palette's default width and silently dropped the count; the fix was to shorten
the hint once filtering starts rather than widen the box.

**Tests.** No existing test asserted the removed border/chip/frame styling directly, so
`go test ./internal/tui/...` (including `TestListDialogSelectAndCancel` and
`TestRenderOverlayCompositesOverChat`, which check dialog behaviour and title-text presence, not pixel
styling) passes unchanged. `go build ./...` and `go vet ./...` are clean.

### The real terminal cursor lands on the focused row, 2026-08-20 (P74.7)

**Filed and shipped the same day, the last row of the menu lane** once P74.5/P74.6 closed the other
two-thirds of it. `View()` (`internal/tui/view.go`) set `AltScreen`, `MouseMode`, `WindowTitle` and
`ReportFocus` on every `tea.View`, but never `Cursor` — while a picker or the approval dialog was open,
the hardware cursor stayed wherever the composer had last left it, so keyboard selection read as
watching a redraw rather than moving through a list. Three things follow from declaring a position
instead: a screen reader follows it, a terminal emulator that highlights the cursor line agrees with
the app about where "here" is, and IME composition lands in the right place.

**`model.render` now returns `(string, *tea.Cursor)`.** The shape change is the only non-mechanical
part, exactly as the item predicted. Two new call sites compute the cursor's *local* position — where
it sits inside the already-rendered overlay string, before that string is centered on screen:

- `listDialog.cursorPos` (`internal/tui/dialog.go`) and the new `approvalCursorPos`
  (`internal/tui/approval.go`) both call a shared `findGlyphPos(content, glyph)`, which strips ANSI per
  line (`ansi.Strip`) and returns the rune column and line index of the first match — column and row are
  measured in visible cells, not bytes, so styling never throws off the position. `listDialog` searches
  for `❯`, the same pointer glyph `aegisListDelegate` already draws on the selected row (P74.5); a new
  `listPointer` constant keeps the two in sync instead of duplicating the literal. The approval dialog
  searches for `▸`, its own selected-option marker, or — while `feedbackMode` is active — for `▎`, the
  static caret already rendered at the end of the typed deny-feedback text.
- `overlayOrigin(fg, width, height)` (`internal/tui/dialog.go`) factors the `(x, y)` centering math back
  out of `renderOverlay`, which now calls it too instead of duplicating the formula. `render` adds a
  local cursor position to this origin to get full-frame coordinates, then hands `tea.NewCursor(x, y)`
  to `View()`, which sets `v.Cursor`.

**Every other overlay branch returns a nil cursor** — help, quit-confirm, the wizard, the security
config, the transient panel, and the completion popup all have no notion of a focused row, so they were
left alone rather than given a position that would mean nothing.

**Tests.** `internal/tui/dialog_test.go`: `TestListDialogCursorPosMovesWithSelection` (cursor row moves
when the selection does) and `TestRenderReturnsDialogCursor` (nil with nothing open, non-nil and
non-negative once a picker opens). `internal/tui/approval_test.go`:
`TestApprovalCursorPosTracksSelectionAndFeedbackMode` (cursor tracks the selected option, then moves to
the feedback caret once `f` is pressed, and `render()` translates it into frame coordinates). Existing
call sites in the test suite that used to take `m.render()`'s single string return now go through a new
`m.renderContent()` helper (`internal/tui/view.go`), which discards the cursor — `render()` itself keeps
the two-value shape `View()` needs. `go build ./...`, `go vet ./internal/tui/...` and
`go test ./internal/tui/...` are green.

### A claim owns residency, not just windows, 2026-08-19 (P72.3)

**Filed and shipped straight out of P72.1**, from a user question about the debate case: two models
are about to be resident, both need windows, and when the work is done the daemon should be back to
one model at its full size. The question was right, and the state it described was worse than a
missing remeasurement — **with a memory budget set, a debate on this machine refused to start at
all.**

**The refusal, measured cold on 2026-08-19** against the configured seat trio (proposer and critic on
`aegis-qwen35-9b:16k`, arbiter on `aegis-phi4-reasoning:16k`):

    model "aegis-qwen35-9b:16k" is not loaded, so its resident weights cannot be
    measured; run it once (`ollama run ... ''`) and re-plan

`PlanFor` needs every member's resident weights, `/api/ps` only reports them for a loaded model, and
a debate is exactly the workload whose arbiter is *not* the model the daemon has been serving. So on
any machine whose seats do not all share one model, the plan was refused on every cold start — and it
was refused with "cannot fit", which reads as a hardware verdict when the truth was that nobody had
asked the arbiter its size. This was P69.6 behaviour from the day it shipped; what changed is that
P72.1 gives operators a reason to set `vram_budget_gb`, so the latent refusal became a likely one.

**The claim now owns the residency of its set for the set's lifetime**, in three added steps around
the plan it already made:

- **Load the members that are not resident, before planning.** The same chicken-and-egg resolution
  P72.1 uses at boot, applied where the set is actually known. Bounded per model (each load waits at
  most `autofitLoadTimeout`), not in aggregate, so a three-seat set — which collapses to two distinct
  runners — cannot approach the turn-stall bound the caller runs under.
- **Reload the members whose window the plan moved.** Leaving this to each seat's own request works,
  since `num_ctx` is stamped per request, but it pays the reload inside the first model turn and
  until then Ollama is serving a window nothing agrees with.
- **Unload, on release, what the claim brought in.** `restoreWindows` puts the daemon's model back to
  its solo window, and that window was solved assuming nothing else is on the card. An arbiter still
  held by `keep_alive` for another half hour makes the assumption false, and Ollama's answer to a
  load it cannot place is to spill to system RAM.

**Three rules on that unload, each of them a thing not to do.** A model the claim found *already*
resident is left alone — this claim did not make it resident and has no standing to evict it. The
global model and `provider.small_model` are never unloaded even when a seat names them: they are what
every ordinary turn, every compaction and every title generation runs on. And the *reload* at the
restored solo window is deliberately left to the next turn, which stamps `num_ctx` and gets it for
free — doing it eagerly would pay a cold load that a second debate, the common next action, would
immediately undo.

**Verified live, cold, on 2026-08-19.** Same trio, same 14.5 GiB budget, nothing resident:

    before preload: REFUSED — model "aegis-qwen35-9b:16k" is not loaded ...
    preloaded aegis-qwen35-9b:16k         in 12.1s
    preloaded aegis-phi4-reasoning:16k    in 12.4s
    after preload:  ok=true
      aegis-qwen35-9b:16k        window=29696  kv=3.74 GiB  weights=4.00 GiB
      aegis-phi4-reasoning:16k   window=29696  kv=3.62 GiB  weights=3.09 GiB
      total=14.45 GiB of 14.50 GiB (spare 0.05 GiB), 1 seat collapsed

The two numbers together are the whole point of the subsystem: **solo, the 9B is fitted to 82,944;
co-resident with the arbiter, both drop to 29,696.** One seat collapses because the proposer and the
critic share a runner, which is P69.6's dedupe doing its job.

**Touched:** `internal/server/residentset.go` (the claim's three new steps, `releaseResidency`),
`internal/server/autofit.go` (`autofitPreload`/`autofitCommit` generalised into
`preloadForMeasurement`/`commitWindows`, now shared by the boot fit and the claim),
`internal/ollamainfo/kvfit.go` (`Unload`), `internal/cli/ollama.go` (the exit-path unload now
delegates to it, so there is one spelling of "finished with this model"). Six new tests in
`internal/server/autofit_test.go`, against a fake Ollama that honours `keep_alive: 0`.

### The window the machine could always have served, 2026-08-19 (P72.1)

**The arithmetic had been right and unused for two days.** P69.5 shipped `ollamainfo.Fit` — solve for
the largest window whose KV cache fits a stated budget beside the model's measured weights, validated
to 0.2% against real Ollama placements — and P69.6 shipped the resident-set solver above it. Nothing
called either one except `aegis models --fit`, a command an operator has to find. The measured cost
on the machine both were calibrated against: a solo session serving **16,384 tokens of a safely
fittable 65,000+**, because nothing ever asked the hardware.

**One of the item's three named gaps was already closed when the work started.** P72.1 said the setup
wizard "never asks" for a VRAM budget and pointed at `wizard.go:545` hardcoding `budgetGB = 0`. P69.6
had added the question — `wizard.go:203`, "VRAM budget (GiB) … Blank to skip" — and the fit call
beside it. The item was filed against a stale read. Its step 4 (route the debate case through the
existing resident-set solver) was likewise already true of `claimResidentSet`. What was actually
missing was steps 2 and 3, and they turned out to be **one mechanism**: `effectiveContextWindowFor`
is already per-model and already drives `num_ctx` per turn through `modelAdapter`, so a fit pass in
that path covers boot *and* `/model` switch without a second wire.

**Four decisions the user made before anything was built**, all of them shaping the code rather than
decorating it:

- **A configured `context_window` is never replaced silently.** New `provider.autofit_context`. The
  fit runs on the budget alone when no window is configured — the item's own closure condition — but
  overriding a window someone set is a separate, explicit yes. This repo's global config is exactly
  the case the flag protects: `context_window: 16000`, annotated in place as load-bearing for the
  P69 debate topology.
- **Preload rather than wait.** The chicken-and-egg is real — weights come from `/api/ps`, which needs
  the model loaded, and loading commits it to a window — so the daemon loads the model *at the window
  the first turn would have used*, measures, fits, then reloads at the answer. A fit that changes
  nothing therefore costs no reload at all.
- **The primary and `small_model` are planned as one resident set**, through `ollamainfo.PlanFor`,
  never through `Fit` directly. `small_model` is co-resident with the primary with no debate in
  sight: compaction runs on it while `keep_alive` still holds the primary. Sizing each against the
  whole budget is the bug P69.6 closed, one layer out.
- **Nothing is written back to `config.yaml`.** The fitted window is effective for the daemon's
  lifetime, announced in the log, and reported by `/status` as source `fit:vram-budget`.

**The fitted window is installed as what was *asked for*, not as what is served.** `configWindowFor`
returns the fitted number where `provider.context_window` used to be read, which puts it on the config
side of `applyDetectedWindowFor`'s existing reconciliation — so verification is free: if Ollama serves
less than the fit solved for, the authoritative-reading rule downgrades to reality and says so, naming
the budget rather than `OLLAMA_CONTEXT_LENGTH`. Installing it as a plain cache entry instead would
have had the first post-run refresh reconcile it against `context_window` and quietly undo it.

**`/model` switch admits, it does not re-fit.** A newly selected model does not replace the one the
daemon was serving; `keep_alive` is still holding that one, and Ollama keeps a runner per model name.
So `autofitAdmit` adds the new arrival to the set and re-plans the whole set, after the turn that
loaded it — off the critical path, and the only moment its weights are measurable. The set only grows
within a daemon's lifetime, which makes the windows it hands out monotonically non-increasing: an
admission can shrink a member, but no sequence of them can oscillate.

**And a resident-set claim outranks the fit while it is installed.** A plan sized its models against
what is resident *now*; the fit sized them as if alone. Without the guard, the mid-debate refresh —
which P69.6 deliberately leaves enabled, since `/api/ps` is the verdict on its own prediction — would
reconcile a seat's window back up to the solo figure and hand the next seat a window the set cannot
afford.

**The live run found a defect the feature itself created, which is the part worth recording.** Booting
the daemon against `aegis-qwen35-9b:16k` with a 14.5 GiB budget produced exactly the intended result —
preload at 16000, weights measured at 4.00 GiB (P69.5's own figure), fit to **82,944**, reload, and
`/api/ps` reporting `context_length: 82944` with `size_vram == size`, fully on the GPU. Then
`aegis models --fit-debate` refused:

    No assignment fits: could not derive resident weights for "aegis-qwen35-9b:16k"
    from its loaded footprint (8.01 GiB at window 82944)

**The fit had destroyed the evidence it was computed from.** `WeightsBytes` derives weights by
subtracting the KV cache a loaded window accounts for, and `BytesPerToken` is a deliberate *upper
bound* for sliding-window architectures. At 16000 the over-estimate is small enough to leave a sane
remainder (6.01 GiB reported, 4.00 derived). At 82,944 the predicted cache is 10.44 GiB against a
reported footprint of 8.01 — the real cache is roughly 4 GiB, since the model's sliding-window layers
stop growing — so the subtraction leaves nothing and the model stops being measurable *by having been
resized*. A debate started after the boot fit would have been refused for want of a figure the daemon
had measured thirty seconds earlier.

**Fixed by remembering, not by re-deriving.** Weights are window-invariant, so a figure taken at any
window stays true. `PlanFor` gained a caller-held `known map[string]int64` consulted only when the
live derivation fails; the daemon fills it from every plan that succeeds (`recordWeights`). Confirmed
against the live server in exactly the state that produced the refusal: without the cache
`ok=false`, with it `ok=true, window=82944, total=14.44 GiB`. The CLI passes `nil` and keeps its
existing honest refusal — a one-shot diagnostic has no earlier measurement to remember.

**Two things this does not do**, both deliberate. It does not discount sliding-window attention, so
the fit still over-reserves on models like this one — 82,944 was solved as if the cache cost 10.44 GiB
when it costs about 4, which means the card would hold considerably more. That is P69.5's documented
safe direction (over-reserving costs context; under-reserving costs an OOM or a silent spill) and
changing it is a separate question. And it does not run on the OpenAI-compat `/v1` path, which cannot
carry `num_ctx` — a fitted window there would be a number the server never receives.

**Touched:** `internal/server/autofit.go` (new), `internal/server/contextwindow.go`,
`internal/server/residentset.go`, `internal/server/server.go`, `internal/ollamainfo/kvfit.go`
(`WarmAt`), `internal/ollamainfo/residentset.go` (`PlanFor`'s weights cache), `internal/config`
(`autofit_context`, frozen from project config for the same reason `vram_budget_gb` is),
`internal/tui/wizard.go` (a stated budget now also writes the permission to keep acting on it),
`internal/cli/modelsfit.go`, `docs/configuration.md`. Thirteen new tests in
`internal/server/autofit_test.go` against a fake Ollama whose residency actually moves, plus five in
`internal/config`.

### Phasing, a content gate, and the search config nobody wired, 2026-08-19 (P71.8, P73.1, P73.2)

**The structural half of the P71 batch, and the two items its own verification produced.** P71.1–P71.5
fixed what was acutely broken in the web path; this is the item that explains why the skill had no
margin to absorb any of it, plus the two things that fell out of proving it.

#### P71.8 — deep-research ran single-context, and `--skill` could not drive it to completion

**Three facts that only make sense together.** `deep-research/SKILL.md` declared no `phases:`
frontmatter, so `drive.PlanFor` returned nil and the run used the generic single-context drive —
`TestPhasePlanFor` actively *pinned* that. `drive.go:175` said the opposite in prose, naming four
skills as multi-phase file-per-phase builds when `threat-modeling` was the only one with a plan. And
`aegis chat --skill` auto-continues only while `<!-- PENDING -->` markers remain, which deep-research
never wrote — so the drive-to-completion the flag exists to provide was **inert** for the skill its
own help text names as a beneficiary.

**Two live runs, same model and task, failing in opposite directions:**

| | 16k (shipped config) | 32k control |
|---|---|---|
| Elapsed | 646 s | 267 s |
| Tool calls | 42 | 39 |
| Compactions | **25** | 4 |
| Empty searches | 8 / 19 | 4 / 10 |
| Inline `[n]` citations | **0** | 18 |
| Outcome | full report, uncited, 2 bad URLs | **no report at all** |

Raising the window fixed the thrash and exposed the drive-termination bug underneath: the 32k run
stopped after Round 1 with a "Work Remaining" note listing Rounds 2–5 as future work, and exited 0.

**Tracing it named a coincidence worth keeping.** `provider.max_iterations` (default 40) bounds
tool-call *rounds* inside one `engine.Run()`; `--max-turns` (also 40) bounds how many times the outer
loop calls `engine.Run()` at all. Without phases `runLinear` calls it **once** — so the entire
research task lived inside the tool-call-round budget of a single `engine.Run()`, and the run's real
stopping condition was never "the research finished" but "the model stopped issuing tool calls".
`drive.Run`'s phased path gives each phase its own fresh conversation *and* its own 40-round budget.
The two 40s are the same number by coincidence, not design, which is why phasing is the fix rather
than a workaround.

**Shipped as frontmatter, no Go code**, per P52.12's own design: `run_dir: .aegis/research/*` and two
phases — **research** (`setup: true`, owns `findings.md`) and **synthesize** (owns `report.md`,
stub-first behind its own marker so a truncated first write cannot look finished). `drive.go:175`'s
comment now names only the skill that actually has a plan. `TestDeepResearchDeclaresAPlan` loads the
real embedded `SKILL.md` through `MaterializeBuiltinsToProject` + `Load` rather than a hand-built
spec, so a YAML mistake fails there; `TestPhasePlanFor`'s deep-research case was rewritten against
`html-report` rather than deleted, per the item's own instruction.

**Verified live at the shipped `context_window: 16000`** — the exact config the failing runs used.
Both phases ran to completion with no manual continuation, one context-overflow reset recovered
automatically, and 21 compactions total that were **single-digit within each turn** (9→9, 11→9, 13→9)
rather than the 25-in-one-conversation thrash, because each turn is its own `engine.Run()`.

**And the half that was not met, stated rather than quietly claimed:** the report had zero real
citations. The phased drive gave the model a *less* crowded context and it still skipped the citation
discipline, reproducing the original bug report's own headline number under a mechanism that no longer
had context thrash to blame. Filed as P73.1 rather than folded in.

#### P73.1 — a mechanical content gate for phased-drive completion

**The phase's completion oracle was "the marker is gone", and the model decides when to clear it.**
Nothing checked what was *in* the file. The live run called `web_search` 4 times and `web_fetch` 6
times against real pages, then wrote an invented "Phase 1 … Phase 12" narrative naming no source —
content that could have been produced without calling a tool at all — and a Sources section citing
`findings.md`'s own headings back at itself.

**Shipped generic, not deep-research-specific.** `skills.PhaseSpec` gained `require_pattern`,
`require_count` and `require_hint`; `Phase.complete()` now requires every owned file to be both
marker-free *and* matching the pattern at least `require_count` times, with `contentGateReason()` and
a distinct `phaseContinuePrompt` branch so the nudge never falsely claims a PENDING marker remains.
Any frontmatter-declared phase can opt in — P52.12's generalization applied to a new axis. deep-
research's two phases declare gates: a real `url:` in `findings.md`, and a numbered Sources line
carrying a real URL in `report.md`.

**Verified live in three stages, and the middle one is the record worth keeping.** The first re-run
still produced zero citations — and **gamed the new gate**, writing fabricated `url: https://example.com/…`
lines to satisfy the regex. That was a genuine negative result: a pattern gate can be satisfied by a
hallucinated match. It also exposed why the run was starving, which became P73.2. After that fix a
second run got real URLs into `findings.md` but produced a Sources list with no links, which is what
tightened the `synthesize` gate from an inline-`[n]` check to a numbered-line-with-URL check. The
third run closed it clean: 5 real fetched URLs in `findings.md`, 6 inline `[n]` markers, and a Sources
section naming all 5, with **zero compactions** now that the configured search provider was actually
being used.

**The limit is worth recording on its own:** a purely mechanical pattern gate cannot distinguish real
evidence from a plausible-looking fake — the general limit of every check in this family, including
threat-modeling's `inventory.py --check`, which validates structure rather than truth. It raised the
floor from "zero citations, undetected" to "a fabricated URL, which a human or a smarter check could
still catch", which is the whole of what a cheap gate can promise.

#### P73.2 — `enginecfg.BuiltinOptions` never wired `cfg.Search`

**Found by P73.1's live verification, and it is the exact failure mode `internal/enginecfg` exists to
prevent.** A phased run in a project configured with `search.provider: searxng` never once called
SearXNG — every result came back `backend="duckduckgo"`. `BuiltinOptions(cfg, root)` is what `aegis
chat`, `aegis debate`, `aegis dryrun` and `cli/worker.go` all call to build the config-derived half of
`builtin.Options` — its own doc comment says "what must not differ is the half that is a straight
reading of config" — and it never set `Search` at all. Only the daemon did, as a manual overlay
applied *after* the shared call. So every non-daemon entry point silently used the zero-config
DuckDuckGo scrape no matter what `search:` said.

Fixed by moving `Search` into `BuiltinOptions` where its own contract already put it, and deleting the
overlay. `TestBuiltinOptionsWiresSearch` pins the round-trip.

**Verified live:** `aegis doctor` went from the zero-config-scrape warning to `PASS search provider
"searxng"`, and a live turn's `web_search` returned `backend="searxng"`. The first check of the fix
ran in a not-yet-trusted temp workspace and *also* showed `backend="duckduckgo"` — that was the
trust-freeze working as designed (`config.SecurityFingerprint` reverting `search.provider` until
`aegis trust --dir`), a different and correct mechanism, re-checked after granting trust.

### The web-research stack, fixed the day it was filed, 2026-08-19 (P71.1, P71.2, P71.3, P71.4, P71.5, P71.9, P71.10, P72.2)

**Prompted by a user report** that a `/research` run on a local 9B "either timed out or didn't produce
any real results". Every claim below is backed by a measurement or a live-run observation taken that
day, not by reading the code.

#### P71.1 — a rate-limit block reported as "no results found"

DuckDuckGo serves its challenge page as **HTTP 200** with a ~14.2 KB body, so `fetchTool.get`
returned no error, no parser found a result, and `searchTool.Execute` fell through to `msg := "no
results found"` with `IsError` false. A throttled query was reported to the model as a successful
search over an empty web.

**Measured:** twelve research-shaped queries back-to-back returned 8, 8, 0, 0, 0… — **two queries is
the empirical ceiling**, and the ~130 ms responses after it are the challenge page being served from
cache. The block clears after ~60 s.

**The consequence is not a missing result, it is a model that stops trusting the tool.** The 16k run
began inventing plausible `learn.microsoft.com` URLs — 7 of 15 `web_fetch` calls 404'd, eventually
tripping the P52.3 failure breaker. The 32k run concluded search was broken and **hand-rolled a Bing
scraper in PowerShell** through `shell`, which is P71.10.

`looksLikeDDGChallenge` now matches the challenge form's action endpoint and copy against a
live-captured excerpt; `duckDuckGo` returns a `blocked` flag and `Execute` reports `IsError: true`
with the retry window. "No results found" is kept for the case it actually describes.

#### P71.10 — deferring the web tools routed the model around every guardrail on them

`LocalProfile` auto-enables on a loopback `base_url` and defers `web_fetch`/`web_search` — while
`shell` stays always-exposed. So on every local-model session the model can see a general-purpose
command runner and cannot see the HTTP client. After P71.1's silent empty result, the 32k run issued
**21 `shell` calls, 20 of them PowerShell `Invoke-WebRequest`**, and zero `web_fetch` calls.

That path bypasses, at once: `netblock.SafeDialer` (no SSRF blocklist, no DNS-rebinding defence, no
redirect hook), `trust.Wrap` (~5 KB of attacker-controllable page content presented as trusted
output), the heuristic prompt-injection scan that hangs off it, and `TruncateHead`'s posture —
replaced by whatever `Substring(0, 5000)` the model happened to write. `server.go:782` already warned
that network policy "does not constrain the shell tool"; this is that warning firing in an ordinary
research session with no adversary.

**Shipped: step 1 only.** `preloadNetworkToolsForSkill` un-defers the two web tools on the session's
registry clone the moment `deep-research` activates, scoped to a `networkShapedSkills` map. The two
larger questions the item named stay open and are recorded as such: whether the LocalProfile default
for these two tools is right at all, and the general form — a fetch performed through `shell` should
not be *cheaper* in guarantees than one through `web_fetch`.

#### P71.2 — the "lite" fallback shared the primary's rate-limit bucket

The DDG ladder fell back from `html.duckduckgo.com` to `lite.duckduckgo.com`, written as if the
failure mode were a markup change. **The actual failure mode is throttling, and the two endpoints are
throttled together** — probed directly: once blocked, four consecutive rounds returned the same
challenge page from both hosts. The ladder bought zero resilience against the only failure that
occurs in practice.

**All four candidate backends were probed live before one was picked**, per the item's own
instruction not to add a backend without checking it against P71.1's challenge behaviour. **Mojeek**
returns a CAPTCHA on the first request; **Startpage** an Anubis proof-of-work challenge on the first
request; a **Bing scrape** was dropped once two of four turned out to be blocked pre-emptively rather
than after volume. **Marginalia** served a genuine results page on the first request and only
rate-limited after rapid repeats, recovering in single-digit seconds against DDG's ~60. The chain is
now configured provider → DDG (primary + lite) → Marginalia → give up, and the give-up message names
whichever backends actually sent a challenge rather than assuming DuckDuckGo.

**A documentation correction shipped with it:** Brave dropped its no-card free tier in February 2026
(now $5/1,000 queries, with a monthly credit requiring a card on file and public attribution to
keep). **Tavily** is the one still offering a genuine no-card free tier, and `docs/configuration.md`
now recommends it.

**The framing that survives the fix:** an unkeyed scrape ladder, even two deep, remains structurally
unfit for an 8–20-query research run. This makes the zero-config path degrade *honestly*; it is not a
substitute for a keyed provider.

#### P71.3 — nothing in the web path retried, anywhere

`internal/provider/retry.go` has a tested equal-jitter backoff that eight caller classes share. The
web tools used none of it. Observed live: `learn.microsoft.com` failed to resolve inside a
`web_fetch` while `nslookup`, `curl` and a direct resolver call from the same machine seconds later
all succeeded.

`fetchTool.get` and `doSearchRequest` now retry up to twice with equal-jitter backoff — restated
locally rather than importing `internal/provider`, keeping `internal/tool/builtin` a leaf package —
retrying only a transport failure or 429/5xx, honouring `Retry-After`, and **never a 4xx**: the run
this responds to had a model inventing URLs, and retrying a wrong URL only spends the budget faster.
Worst-case sequences (~100 s / ~70 s) are pinned under the 900 s `MaxTurnStall` bound by a dedicated
test. Piece (3), a client-side token bucket over the DDG scrape, was judged not worth building
against a not-really-supported backend now that P71.2 gives a second backend and P71.1 tells the
truth.

#### P71.4 — a configured provider's failure was invisible to everyone

`provErr` was consulted **only if the scrape also returned nothing**, so whenever the fallback
succeeded a broken configured provider — expired key, wrong SearXNG `base_url`, a 429 — was
indistinguishable from a working one. The user believes they are on their keyed allowance; they are
silently back on the scrape, and therefore back inside P71.1's two-query ceiling. This is the failure
mode most likely to make search feel *inconsistent* rather than broken, because it is intermittent by
construction.

Results now carry `backend` in the `trust.Wrap` attributes and prepend a note naming the provider
that failed and the one that covered for it. And `aegis doctor` gained an eleventh check —
`doctorSearchCheck`, config-shape validation rather than a live probe, so doctor does not spend a
metered provider's quota to run: FAIL on a keyed provider with no key, on `searxng` with an
unparseable `base_url`, or an unrecognized provider; WARN on the zero-config default; PASS on a
correctly configured one.

#### P71.5 — the fetch cap was a constant that exceeded the compaction trigger

`fetchTool.Execute` defaulted `limit` to 20,000 characters — ~5k tokens — and that figure was never
compared against anything. At the shipped `context_window: 16000`, `CompactionTrigger` is 8,000, so
**a single source read was 62% of the entire compaction budget**: reading one page could trigger
compaction on its own, and two consecutively could not avoid it. That is the 16k run's 25 compactions
across 42 tool calls, almost all of the shape `11→9 messages`.

The downstream damage was worse than the latency: search results were summarized away before the
model could fetch the URLs in them, which is *why* it began inventing URLs.

**Shipped closer to the resolved per-turn window than the item asked for.**
`tool.WithContextWindow`/`ContextWindowFromContext` is a new context-value pair carrying
`Engine.effectiveContextWindow()` — the actual escalatable, possibly-detected figure, not a static
config read — into `Engine.toolCtx` on every tool call. `defaultFetchLimit` sizes the cap at
`window*3/5` chars, capped at today's 20,000 and floored at 4,000, so cloud-scale contexts see no
change and only a small window shrinks. At 16,000 that is 9,600 chars (~2,400 tokens) against an
8,000-token trigger.

**Explicitly not done:** raise `context_window`. The 16k pin is load-bearing for the P69 debate
topology — it overrides each model's Modelfile `num_ctx`, so raising it re-inflates every seat's KV
cache at once. The per-tool cap is the knob without that coupling. (P72.1, later the same day, is the
principled version of that whole question.)

#### P71.9 — the findings log was advisory, and the run left it as placeholders

Section 2 of the skill diagnosed the problem correctly — "a log that lived only in conversation is
destroyed by compaction exactly when it's most needed" — and then made the remedy conditional: "for
anything beyond a couple of rounds". **The 16k run wrote the file once, at Round 1, and never touched
it again** across 42 tool calls and 25 compactions: 976 bytes, all scaffold, `[To be updated]` where
the evidence should be. The audit trail in the final report was therefore reconstructed from memory
at the end — the specific thing section 2 exists to prevent — and two of its five cited URLs were
wrong.

A text edit to the embedded `SKILL.md`, no Go code: section 0 creates the working file
unconditionally before the first search, section 1 step 5 became an explicit `edit_file` instruction
that must land *before the next search or fetch*, and the "beyond a couple of rounds" qualifier is
gone.

#### P72.2 — `/models` showed a static catalog instead of what is pulled

Reported by the user as "/models seems broken". It was not crashing: `cmdModels` returned
`modelcatalog.Curated()` unconditionally — four cloud entries plus four generic Ollama *family* names
(`qwen3`, `deepseek-r1`, …), none of which is a loadable tag, so picking one would 404 on the next
turn.

Four pieces: `ollamainfo.ListLocal` (a `GET /api/tags` client, excluding embedding-only models —
listing `nomic-embed-text` would be a guaranteed-broken picker choice, while a model with *no*
`capabilities` field at all is kept rather than excluded on missing data); `GET /models/local` on the
daemon, which owns the `base_url` connection the client has no independent route to; a thin
`Client.ListLocalModels`; and `cmdModels` showing only the live pulled list when the provider is
reachable, falling back to the curated catalog exactly as before otherwise. A pulled tag's
`:16k`/`:32k` suffix is shown as the context label when present, since `/api/tags` does not report
context length for an unloaded model.

**Verified live** against this machine's daemon: 6 completion-capable models returned,
`nomic-embed-text` correctly excluded.

### How the P71 batch was measured, 2026-08-19

Recorded once rather than in each item, so no P71 entry has to restate it and so the next reader can
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

### Both halves of the sub-agent boundary, 2026-08-18 (P70.4)

P70.2's build, that same morning, wrapped the swarm mailbox and — sweeping for other model-facing
reads of it — found the channel next to it. That became **P70.4**, and it shipped the same evening.
The item explicitly planned to be taken in two pieces: a size cap that "carries no such question and
can be taken alone", and a wrap that "needs the same kind of deliberate answer P70.2 got". Both were
taken, because the answer arrived immediately.

**The posture question, and the answer.** The item's own counter-argument was that a parent which
*commissioned* a sub-agent's work is not obviously in the same position as one reading a teammate's
relayed prose — the parent asked for this text, so treating it as hostile input reads as paranoia
about your own tooling. The user's answer: **wrap it, zero trust.** Commissioning the work does not
vouch for what the work *read*. A sub-agent that fetched a poisoned page or called a malicious MCP
server writes what it read into its own report, and the ingestion-time marking `web_fetch` and MCP
results carry is lost the moment the child summarizes them in its own words. That is the laundering
shape P70.2 closed one channel of; this is the other.

It is now the third answer in a set of three that deliberately do not all point the same way. The
mailbox (P70.2) and a sub-agent's result (P70.4) are wrapped because their content crossed a boundary
before being relayed onward; `security_scan`'s workspace-derived output (P70.3) is not, because a file
the model can already read directly is not a crossing. All three are recorded in
[docs/mcp-trust-boundary.md](../docs/mcp-trust-boundary.md), including the argument rejected here.

**The finding was wider than one line number.** The item verified it at
`internal/swarm/subprocess.go:223`, where `runWorker` scans the mailbox back for the last `MsgResult`
and assigns its text into `Result.Output`. But that is the durability substrate of *one* backend: the
in-process backend reaches the same place without the mailbox at all (`inprocess.go:82`). What the
build had to enumerate was every path `Result.Output` takes to the parent model, and there are four,
all in `internal/tool/builtin/agent.go`:

| Path | Site | Note |
|------|------|------|
| Foreground `agent` call | `execute` | The plain case. |
| Workflow modes | `executeWorkflow` (sequential / parallel / loop) | Three returns, each joining teammate output. |
| Debate | `executeDebate` | Wrapped at the transcript, **not** at the role — see below. |
| Background spawn | `spawnBackground` → `task_output` | Capped and wrapped *before* the task store, not at the reader. |

`TestSubAgentResultIsCappedAndWrappedOnEveryPath` is a table over those four rather than one
representative case, because the failure this item was filed for is a path that forgot, not a helper
that is wrong.

**Two places the obvious implementation would have broken something.**

- **A head cap inside the debate's `runRole` would have corrupted the protocol, not just shortened
  it.** `parseVerdict` reads the *last* `VERDICT:` line in the arbiter's text — that is P69.1's fix
  for reasoning models that draft a verdict mid-thought and revise it at the end. Cutting a role's
  tail is therefore exactly how a REJECT gets read as the UPHOLD it was drafted from: silent, and it
  inverts the result. Bounding role text is the debate's own concern and it has its own bounds for it
  (the round ceiling, the budget-headroom check). The cap and wrap go at `transcript.Format()`, the
  boundary where the debate becomes a result for the parent model, and that is the only boundary this
  item is about.

- **Capping at `task_output` would have broken the shell tool's stated recovery path.** `task_output`
  is generic — it also serves shell's background jobs, and the shell cap's truncation notice *names*
  it as where the dropped bytes can be recovered. Capping there would break that promise and would
  wrap output that never came from a sub-agent. The background path therefore caps and wraps before
  the text enters the task store. `TestTaskOutputStaysGenericForNonAgentJobs` pins the negative.

**The cap divides rather than clips.** 24,000 bytes (~6.0k estimated tokens, the value shell and the
skill-script runner already chose for a subprocess writing a report; deliberately above `team_inbox`'s
20,000, since a sub-agent's report is the *point* of the call the parent made). Head end — a
sub-agent's report is a digest written top-down. A workflow divides that budget into per-teammate
shares (`agentShare`, floored at 2,000) rather than applying one cap over the joined text, because a
single cap over the join does not truncate a batch evenly — it deletes the last teammates outright,
and a parallel batch's last two agents vanishing without a trace is worse than four visibly shortened
reports. The join is still bounded on the way out, which is what catches a batch too wide for the
shares to divide.

**The remainder is not spilled**, the same exclusion `team_inbox` and `web_fetch` take, and here it is
load-bearing rather than inherited: spilling writes the overflow to a workspace file that `read_file`
returns with **no envelope at all**, so a context-budget feature would hand back unmarked exactly the
bytes the wrap just marked. `TestSubAgentCapKeepsTheHeadAndSpillsNothing` asserts the notice names no
spill locator.

**Aegis's own text stays outside the envelope.** The "unknown subagent_type" note and the loop's
"(loop completed in N iterations)" status line are the harness speaking, not the sub-agent; rendering
them inside the marker would tell the model to distrust the harness. Pinned by
`TestUnknownSubagentTypeNoticeStaysOutsideTheEnvelope`.

**Found while building: a data race in the test suite, not in the product.** `fakeBackend.Spawn`
recorded its `SpawnConfig` into shared struct fields with no lock. A `parallel` workflow calls `Spawn`
once per goroutine, so that is a race under `-race` — and nothing had ever exercised it, because the
one existing concurrency test drives a different double (`gatingBackend`). P70.4's per-path table was
the first test to run `fakeBackend` concurrently, and it failed immediately. Fixed with a mutex. Worth
recording because it is the second time in this batch that adding coverage for one thing surfaced a
defect in the scaffolding for another.

**Tests.** `internal/tool/builtin/agentresult_test.go` is the new pass: the four-path table, the
background-before-the-store assertion, the head-and-no-spill posture, the share arithmetic plus a
four-agent batch where every teammate must survive, the harness-text-outside-the-envelope ordering,
and the `task_output`-stays-generic negative. `maxAgentResult` joins the enumeration in
`TestResultCapsCanBindBeforeTheContextWindow` — a cap no test can name is a cap that drifts. Three
existing assertions compared a whole result string to the sub-agent's exact text and now go through an
`agentBody` helper that fails if the envelope is missing, so a test asserting on *content* cannot be
the thing that quietly deletes the wrap. `go test ./...` and `go test -race ./internal/tool/builtin/`
are green.

**What this leaves.** Nothing. P70.4 was the last open build item in the tree: Tier 1, Tier 2 and
Tier 3 are all empty, and the "Up next" table is down to its one parked-by-choice verification row.
Every remaining item is Tier 4 with no fired trigger. The next build item does not exist yet and has
to be found — by unparking the live tier, or by an audit that files new work the way P66.15's sweep
filed this one.

---

### Three rows and a posture, 2026-08-18 (P70.1, P70.2, P70.3)

P66.15's sweep shipped that morning and, being an audit, ended by filing three items it had verified
but deliberately not fixed. All three shipped the same afternoon. Each had been filed *because* it
carried a question rather than a line of work, so the shape of this sitting is three answers and the
code that follows from them.

---

#### P70.1 — the restore boundary

`checkpoint.Store.RestoreFiles` replayed BLOB rows to absolute paths with **no validation of any
kind**, because the store had no notion of a workspace root. The item named two decisions; the user
answered both.

**Where the root comes from: recorded per checkpoint.** `checkpoints.workspace_root`, added by the
same idempotent-`ALTER` convention already used for `git_sha`, with `Store.Create` taking it and
`internal/server/messages.go` passing the session's workdir. The alternative the item floated —
threading a root into the `Store` — is wrong here for a reason worth writing down: the `Store` is
constructed **once per server** (`server.go:611`) and shared across sessions with different
workspaces, so a store-wide root could only ever be right by coincidence.

**What a rejected path does: refuse the whole restore.** Every path is validated *before* anything is
written; one bad path means nothing is written at all, reported through a new `ErrRestoreRefused`
sentinel that callers can tell apart from the pre-existing best-effort per-file write errors. A
half-rewound tree is the exact failure `/rewind` exists to prevent.

**Legacy rows fail closed.** A checkpoint with captured files but no recorded root is refused rather
than exempted — trusting recorded paths is precisely the behavior this item removes, and the user's
fallback (`/rollback`, a git reset) is offered by the same rewind request. One carve-out keeps it
quiet: a checkpoint that captured *no* files needs no root, which is the common "turn wrote nothing"
case.

**The containment check is not a prefix check.** `withinRoot` rejects non-absolute paths, then runs
both sides through `EvalSymlinks` — walking up to the deepest existing ancestor and re-appending the
remaining components when the leaf does not exist yet, so a symlinked directory pointing out of the
workspace is caught while a legitimate created-then-deleted or deleted-then-recreated path still
validates. Containment is `filepath.Rel` plus a `..` test (so `/work-other` does not pass as inside
`/work`), the root itself is rejected, and on Windows both sides are lowercased because
`EvalSymlinks` there can return different casing or the long form of an 8.3 name.

**Two call-site consequences, decided rather than inherited.** The rewind handler returns **409** and
`return`s *before* truncating the conversation, so a refused `both` rewind never leaves the transcript
describing a state the disk is not in. `guardretry`'s rollback treats a refusal as **non-fatal** —
warned, and appended to the guard reason — because that path exists only to tidy a response already
being surfaced as failed; making it fatal would turn a guard FAIL into a run error.

**Secondary, as the item asked:** file mode is now captured and restored, with an explicit
`os.Chmod` alongside the write, since `WriteFile`'s mode argument only applies on create and a mode
changed during the turn would otherwise survive the rewind.

**One nuance left alone.** For the git `/rollback` variant, `git reset --hard` still runs before the
file restore, so a refusal 409s *after* the reset has happened. The reset is a superset of the
checkpoint restore for tracked files and the 409 body says nothing was restored from the checkpoint,
so the ordering stands.

Tests pin the boundary as the deliverable: an out-of-root path aborts the restore with the good
in-root file also left unrestored, a table over `withinRoot` (sibling prefix, `..`, the root itself,
relative paths, plus the legitimate shapes), symlink escape, recreate-after-delete and
delete-of-created, mode preservation, the legacy row, and a server-level test that poisons a real
checkpoint and asserts 409 + nothing touched + conversation not truncated. The symlink and
mode-preservation tests skip on Windows, where they cannot mean anything.

---

#### P70.2 — the mailbox, and the posture question that was the item

`team_inbox` formatted a teammate's `m.Text` into the tool result bare: no `trust.Wrap`, no size cap.
The item refused to guess, because wrapping intra-harness traffic asserts something about the trust
model that had never been written down.

**The user's answer: zero trust.** The mailbox is a laundering channel — a teammate that ingested
poisoned web or MCP content can relay it to a peer as plain, trusted-looking text, and the wrap at
the original ingestion point does not survive the relay. So mailbox content is wrapped, in the same
shape `internal/mcp/trust.go`, `web.go` and `scanreport.go` use, with a `team_untrusted_output` tag
and framing that says why the bytes may not have originated with the sender. Aegis's own "inbox
empty" sentence stays unwrapped, and a test pins that it does.

**The cap posture deviates from the usual one, deliberately.** 20,000 bytes, head end — `web_fetch`'s
value, for `web_fetch`'s reasons — but the remainder is **not spilled**, which every other capped
tool does. Spilling would write the *unwrapped* overflow to a workspace file that `read_file` returns
with no marker, reopening the exact laundering path the item closes. Two further details: the
per-message header is reserved out of the cap so a single over-cap body still cannot exceed it, and a
message that does not fit the batch budget is **left unread** rather than consumed and dropped, with
a notice naming the count. The budget costs a second `team_inbox` call, never a message.

**The sweep for other laundering paths found one, and it is a different item.** `subprocess.go:223`
lifts the worker's last `MsgResult` text into `Result.Output`, which reaches the parent model bare
through `agent.go` and `task_output` — but the in-process backend reaches the same place *without the
mailbox at all*, so this is the sub-agent **result** path and the mailbox is merely its durability
substrate under one backend. Wrapping it changes the shape of every `agent`/`task_output` result and
every workflow mode's joined output, which is not something to do as a side effect of a mailbox fix.
Filed as **P70.4**, uncapped as well as unwrapped.

Docs carry the decision so the next question can be settled against it rather than afresh:
[docs/mcp-trust-boundary.md](../docs/mcp-trust-boundary.md) states the zero-trust reading,
`docs/multi-agent.md` and `docs/tools-reference.md` note the new shape — and the latter's `team_inbox`
argument block, which documented a `since` parameter that has never existed, was corrected in passing.

---

#### P70.3 — the bound built, the wrap declined

**The bound half.** `security.runJSON` and `runContainerCLI` both used `cmd.Output()`, so a rogue or
compromised scanner's stdout was read entirely into memory before parsing, with no per-call cap at
all — they leaned on `CapRound`, which is the *aggregate* bound and was never meant to be the only
one. Both now read through a bounded writer capped at **64 MiB**, matching `spillMaxBytes` and near
`maxReadBytes`; the cap is deliberately generous against `truncate.go`'s 20-32 KiB model-facing caps
because it is a memory-safety ceiling on a raw SARIF report (a large monorepo scan legitimately runs
to megabytes), not a token budget.

**Two implementation points that are not stylistic.** The bound is a *writer* rather than an
`io.LimitReader` over `StdoutPipe`, because `os/exec` pipes a non-`*os.File` `Stdout` through an
internal `io.Copy` goroutine that `cmd.Wait()` waits on — stopping the read at the cap risks the
child blocking forever on a full, undrained pipe and `Wait()` hanging against it. The writer instead
drains to completion, discarding past the cap. And overflow **refuses to parse**: it returns an error
naming the cap, never a prefix, checked *before* the existing "non-zero exit tolerated if output was
produced" branch so a rogue scanner cannot dodge the bound by also exiting non-zero. That follows the
gosec two-phase precedent — fail honestly rather than hand a parser a truncated JSON document that
parses cleanly into a confidently under-reported finding count. Since `cmd.Output()` is gone, so is
its automatic population of `ExitError.Stderr`, which `interpretOSVError` depends on for P34.12's
"no dependencies" vs "extraction failure" distinction; stderr is now captured and attached by hand,
with a test pinning it.

**The wrap half was declined by the user, and that is the durable output.** `security_scan`'s content
is workspace-derived — files the model can already read directly — so wrapping it would mark as
untrusted the one class of input the agent is reading on purpose. The item asked for this to be
answered "once for the whole tree rather than tool by tool", and it now is. It does **not** contradict
P70.2: the mailbox launders content that crossed a boundary, and a workspace file did not. Zero trust
is the posture for *ingestion*, not a rule that every byte acquires a marker.

---

**Verification.** `go build ./...`, `go vet ./...` and `go test ./...` all clean with the three
changes in the tree together — checked on the combined tree, not only per item, since P70.1's schema
migration and P70.2's `truncate.go` row touch packages the other two also compile against.

### Five rows of Up next, 2026-08-18 (P66.15, P67.6, P67.7, P67.8, P67.9)

The 2026-08-17 "Up next" table had six rows. Five of them shipped on 2026-08-18, in the table's own
order; the sixth is the live-tier remainder, which the user parked and which is untouched. What
follows is per-item, and the parts worth reading are the places the *items were wrong about
themselves* — three of the five were, in ways that changed what got built.

---

#### P66.15 — the sweep of the two packages nobody read

`internal/tui` (16k non-test lines) and `internal/security` (8.4k) were 26% of production Go and had
produced three findings between them, two of which were a struct-field count and a stale comment.
The item's claim was that this is evidence nobody read them rather than evidence they are clean. It
was right.

**Seven findings verified and fixed, each with a regression test confirmed to fail with the fix
stashed out.** The two that matter most:

- **P66.6 was not the only unsanitized ingestion point** (`internal/tui/toolview.go:46`, Medium).
  Every tool call is drawn into the transcript as well as into the approval dialog — before approval
  on an auto-approved tool, on every history reload, and in build/auto mode where an allowed path
  never opens the dialog at all. `renderWriteDiff`/`renderEditDiff`/`renderMultiEditDiff` handed
  model-supplied file content straight to `diffLines`, which strips nothing, and the generic branch
  printed the raw input (`strings.Fields` does not treat ESC as whitespace). Only `renderShellCall`
  had a per-renderer strip, which is itself the evidence that the per-branch form leaks. Fixed at the
  one choke point instead.
- **`captureShellWrites` was addressing the wrong paths** (`shell_checkpoint.go:52`, Medium).
  `git status --porcelain` reports paths relative to the **repository** root — porcelain forces
  `status.relativePaths` off, verified empirically — and the code joined them onto the *workspace*
  root. Whenever a workspace sat inside a larger repo, every capture addressed a path that did not
  exist: the command's real writes were never captured, so `/rewind` silently restored nothing, *and*
  a pre-image could be recorded against a bogus in-workspace path that restore would then write out.

The other five: unsanitized slash-command output (`/scan network` prints nmap banners a *scanned
remote host* chose), unsanitized guard/error events (a guard's text is the judge model's own words),
unsanitized `!`-shell output, unwrapped `recon_scan`/`dast_scan` reports, and the two per-session maps
(`sessionSems`, `sessionPermCache`) that no cleanup path ever freed — a loopback caller creating and
deleting sessions in a loop leaked a channel plus one entry per approved tool for the daemon's
lifetime.

**What the sweep checked and cleared is recorded too**, so the coverage gap closes as *swept* rather
than as skipped: `parseNmapXML` is not XXE- or entity-expansion-exposed (Go's `encoding/xml` resolves
no external entities and expands no custom ones unless `Decoder.Entity` is set, which it never is);
all five named parsers already have fuzz targets and no panic path was found; `notify.sanitize`
correctly strips controls and semicolons before OSC 9/777; and the 60s auth-lockout cap *is*
defeatable — any concurrent successful request resets the streak, so a TUI polling `/status` keeps it
at zero — but the token is 32 bytes of `crypto/rand`, so the throttle is not the control that
matters and it is not filed as a vulnerability.

**Four findings were verified and deliberately not fixed**, because each is a design decision rather
than a line, and are filed as **P70.1** (`checkpoint.RestoreFiles` path-validates nothing),
**P70.2** (the swarm mailbox as an unwrapped cross-agent channel) and **P70.3** (scanner output,
unbounded and half-wrapped). Filing them is the point: a sub-item quietly left undone inside a closed
item is how P66.25 and P66.26 came to need refiling.

---

#### P67.6 — a second compaction trigger, on cache temperature

Aegis compacted on *context pressure* only. That is the right trigger for running out of window and
the wrong one for a different problem: a conversation resumed after a long gap re-sends a prefix the
backend has already evicted, paying full prefill on stale tool results it will summarize away later
anyway. The observation the item rests on is a scheduling one — **when the cache is already cold,
clearing old tool results is free**, because the usual reason not to rewrite the middle of a
conversation has already happened.

`compaction.ClearColdToolResults` replaces every clearable tool result except the most recent N with
`compaction.ColdCacheSentinel`; `engine.ColdCacheCompactor` is the optional seam, alongside the four
this file already had. The engine owns the *when* (the idle gap, the purpose gate, a once-per-run
latch), the compactor owns the *what* (which result kinds are disposable, how many to keep, the
sentinel).

All three of the item's named constraints are pinned by a test rather than by a comment:

- **The keep-count is floored at 1.** `TestColdClearFloorsKeepAtOne` passes 0, -1 and -100.
- **The gate is on call purpose**, which is why the item was sequenced behind P67.3.
  `TestColdCacheGatesOnCallPurpose` walks all nine `provider.Purpose` values;
  `TestColdCacheHonorsAContextDeclaredPurpose` covers the launcher path, mirroring
  `provider.EffectivePurpose`'s precedence. `PurposeUnspecified` is eligible, deliberately: the
  analysis-only purposes are exactly the ones that were *added* with a tag, so the untagged default
  is the conversation-owning case.
- **The sentinel is a wire format.** `TestColdClearSentinelIsStable` compares it to a literal and
  fails with a paragraph explaining what a rename breaks — the same hazard the
  `<read-files>`/`<modified-files>` tags carry, and it fails the same quiet way.

Two decisions the item did not make. The default is **20 minutes** (`compaction.cold_cache_after`,
`"off"` disables), chosen to sit clear of every cache TTL this ships against rather than to split the
difference: Ollama's default keep-alive is 5 minutes, Anthropic's prompt cache is 5 minutes or 1 hour
by tier. It is a default, not a finding — no live measurement has been taken at any value, which is
why it is a knob. And the pass **fires at most once per run**: the condition it detects is true
exactly once, and leaving it unlatched would rewrite the conversation every turn, which is the thrash
P62.7 exists to stop.

`LastActivityAt` is plumbed from the session row (`sess.UpdatedAt`, read before the run appends
anything), because the engine's own clock starts at run entry and would measure a gap of zero no
matter how long a session had been idle — which is the *resume* case, i.e. the one the item is
actually about.

---

#### P67.7 — tool calls dispatched from the stream, and the two gates the item did not name

The largest change of the five, and the one whose item turned out to be incomplete.

**What was asked:** `Engine.turn` returned its `toolUses` slice after the stream drained, so the first
call of a five-call round waited for the fifth to finish generating. Feed the scheduler each call as
its block completes instead. The scheduler itself is correct and should not change.

**What was built:** `internal/engine/toolround.go`, which is the old `runTools` restructured around a
`toolRound` type with `add`/`wait`/`abort`. `turn` takes a round and calls `add` on `EventToolUse`;
`Run` opens the round before the turn and either hands it to `runTools` or aborts it.

The index arithmetic had to go. The old code sized `results`/`traces`/`serialize`/`paths`/`done` from
`len(toolUses)` and had each goroutine write `results[i]`; appending to those slices while goroutines
hold indices into them is a data race with a silent failure mode. Each call now owns a `*toolSlot`
whose address never moves. The semaphore also moved from the dispatch loop into the goroutine —
blocking the dispatch loop was free when the caller had nothing else to do, and is not when the
caller is draining a provider stream and beating the P39.17 heartbeat.

**Two constraints the item did not name, both found by a failing test rather than by reading:**

- **`TestBudgetGateStopsRun` failed.** The pre-tool-round budget gate exists precisely so that a turn
  whose *own* usage crosses the cap stops before its tool calls run — and that usage is not known
  until the turn ends. A call dispatched mid-stream cannot honor a bound that is not yet decidable.
- **The P53.2 loop guard can *abort the run*** on the complete round's signature, and structurally
  cannot rule on a prefix of one.

The temptation was to weaken the budget gate on the argument that a read-capability call costs no
money and has no side effects. That argument is defensible and it was **not** taken: the gate is an
explicit, tested, user-facing guarantee, and quietly narrowing it to buy latency is not a trade to
make on the engine's behalf. The resolution is a restriction on *when* early dispatch is active:

- **No spend bound configured** (`runBudget.spendBounded`: USD or either token cap). Wall-clock and
  the stall watch are exempt — they are attached to the context as real deadlines and already reach a
  running round.
- **Early dispatch stops at the first write/execute call.** This is what keeps side effects behind
  every pre-round gate, and it is also what keeps the dispatched set a *prefix* of the round, which
  the same-path `waitFor` graph depends on.
- **Not under `suppressTools`** (the calls are about to be discarded as hallucinations) and **not
  under the tool-call shim** (the calls are not in the stream at all — they are parsed out of the
  reply text afterwards).

This lands the payoff on the workload the item names — local models, where generation latency
dominates and pricing is zero, so no spend bound is set — and leaves a cloud run with a budget on the
pre-P67.7 path. That is a real narrowing of the item and is recorded as one.

A third thing the item did not name, found by `go test -race`: **a run now has more than one producer
of events.** `Run` emits, and the round's goroutines emit, concurrently — and every consumer of an
`EmitFunc` (the TUI, the daemon's SSE writer, an eval collector) was written for a single producer.
Serialized once at the top of `Run` (`serializedEmit`) rather than at each producer, because the
failure mode of a future caller forgetting to wrap is a race in code this package does not own.

The item's four original constraints all hold, and one needed no code at all: **`startedTools` is
already recorded at dispatch**, inside `executeTool`, so P65.1's distinction between a call that may
have landed effects and one that never ran survives unchanged.

---

#### P67.8 — the shell allowlist is per-command, not per-binary

`shell_readonly.go` allowlisted whole binaries and rejected anything with a shell metacharacter. Its
own comments showed where that ran out: `sort`, `tree` and `uniq` were excluded because each has a
file-*writing* form, with the reasoning "no argument parsing makes them read-only". Argument parsing
is now there — a per-command table of permitted flags with argument types, optional value patterns, a
predicate escape hatch, a per-command POSIX `--` switch, and **unlisted flags failing closed**.

The interesting part is which flags are refused and why, because "does it write" is not the only
question:

- `sort --compress-program` execs a program over temp files; `rg --pre`/`--pre-glob` run a
  preprocessor per file; `rg --hostname-bin` execs a binary and the name suggests a lookup;
  `fd -l/--list-details` internally execs `ls`, which is the item's own PATH-hijack example.
- `date` needed a **predicate**, not a flag rule: BSD `date 010112002026` sets the clock from a
  *positional*. `uniq`'s writing form is likewise a positional, which is why the table has a
  positional bound at all.
- `gh` is classified **CapNetwork, not CapRead**. Every `gh` call is egress, and `permission.Policy`
  *asks* for CapNetwork while silently *allowing* CapRead — a CapRead downgrade would have been more
  permissive than the network gate it must sit behind. `gh api`, `gh auth`, `-w/--web` and
  `--hostname` are refused outright.
- The exclusions that were never about writing survive with their reasoning intact: `env`/`printenv`
  (the *default* output is the provider keys), `ps` (same data by another route), `less`/`more`
  (shell out). `find` was added to that list — its unsafe primaries are operands in an expression
  language, not flags.

**The test pass found a bug, which is the argument for having written it.** A PowerShell
colon-attached path (`Get-Content -Path:<abs>`) escaped confinement: `argvPathCandidates` read the
token as a short cluster and offered a benign relative name to `argvStaysInRoot`. That is VULN-02's
shape in a third spelling, and it is now split into an operand before the path check.

---

#### P67.9 — the terminal is asked, not guessed

`imagerender.go` decided whether a terminal speaks the kitty graphics protocol by checking whether
`TERM` contains `"kitty"`. The obstacle to asking is that an unsupporting terminal simply stays
silent, so the naive implementation needs a timeout that is either too short to be reliable or too
long to sit in startup. The trick the item names removes it: terminate the batch with a **DA1 request
(`CSI c`)**, which every terminal since the VT100 answers, and rely on in-order replies. A feature
answer before the DA1 reply means the feature exists; DA1 first means it does not.

New package `internal/termcaps`. `Decide(io.Reader)` consults no clock at all and is tested against
13 recorded streams × 4 chunk sizes — kitty-supporting, kitty-silent, replies *after* DA1 not
counted, out-of-order-but-pre-DA1, `DECRPM` 0/4, truncated mid-APC, interleaved type-ahead. The one
timeout in the system (`ProbeDeadline`, 2s) is documented as a safety net that a conforming terminal
never waits out, not as a decision input.

**No second parser.** `internal/termsafe` grew `NextSeq`, exposing the completeness bit an
incremental reader needs and a stripper does not, wrapping the existing recognizer.

The Bubbletea problem the item warns about — replies arriving on the same channel as keystrokes — is
solved structurally rather than hopefully: `termcaps.Cached()` runs from `newModel`, strictly before
`tea.NewProgram(m).Run()`, so Bubbletea never owns stdin while replies are in flight. The honest
caveat is documented: a keystroke typed inside the few-microsecond window is in the same read buffer
and cannot be un-read, which is exactly why the probe must run before the program rather than
alongside it.

**One thing the item did not anticipate.** On Windows the probe cannot simply write: if
`ENABLE_VIRTUAL_TERMINAL_PROCESSING` cannot be enabled on the output handle, the query bytes would be
*printed* into the console as garbage. The implementation writes nothing at all in that case, and the
same rule covers every non-TTY path (piped, CI, `aegis serve`) — `TestProbeNonTTY` asserts both no
hang and **zero bytes written**.

The payoff the item asked for is delivered: kitty is now an **auto-selected** tier rather than
permanently opt-in, and `aegis doctor` reports `supported: … — probed: the terminal answered
(DA1-terminated batch)` instead of "plausible", falling back to the old wording only where it is
actually correct (not a terminal). `AEGIS_TERM_CAPS` forces or disables the whole thing;
`tui.image_rendering` gained `"halfblock"` to force the safe tier.

---

#### Testing

`go build ./...`, `go vet ./...`, `staticcheck ./...` and `go test ./...` are all clean.
`go test -race` is clean on every package touched — which is the load-bearing one for P67.7, and is
what caught the concurrent-emit race that the ordinary run did not.

### Nothing planned a resident set, 2026-08-17 (P69.6)

**The signature was the bug.** `ollamainfo.RecommendContextWindow(modelMax int) int` is one model in,
one number out, and it cannot express "these three models must be resident at once" because it never
learns a second model exists. Since P69.1 each debate seat resolves its own model, so a debate holds
two or three models in VRAM simultaneously — every one of them sized as though it owned the card.
P69.5 fixed the arithmetic for one model in isolation; this is the half it left open, and the half
the debate feature actually needs.

**The two open design questions, settled.** The budget is an explicit `provider.vram_budget_gb` key —
no detection, because `internal/hwinfo` rules out GPU/VRAM introspection "on any platform, ever"
(P17.5), and the number wanted is not the card's capacity anyway but capacity minus the driver
reserve and the desktop compositor, which only the operator can see. And the plan is owned by the
*debate*, installed as a scoped override on the daemon's per-model context-window cache for its
duration, rather than by a new "workload" abstraction that nothing else would have used.

**`ollamainfo.PlanResidentSet` — the set planner.** Binary-searches the largest **equal token window**
`T` such that `sum(min(T, ContextMax_i) x BytesPerToken_i) <= budget - sum(weights)`. Equal *tokens*
rather than equal *bytes* because the window is the number the engine budgets a conversation against,
and two seats reading the same transcript need comparable room to hold it; an equal-byte split hands
the cheap-KV model a window it can never fill and starves the expensive one on the identical prompt.
The `min()` clamp gives redistribution for free — a member that reaches its training maximum stops
consuming budget as `T` climbs, and the search keeps going for the rest.

**The planner's unit is a distinct model, not a seat**, and that is a correctness property rather than
a tidiness one. Ollama holds one runner per model *name*, so the proposer and critic sharing a 9B
share one copy of the weights and one KV cache. A planner that budgets three seats when two share
weights refuses configurations that fit — pinned by `TestPlanResidentSetCollapsesSeatsSharingAModel`,
whose second half proves the un-collapsed arithmetic really does overflow the budget the collapsed
one has room to spare in.

**`BaselineContextWindow = 32768` was not lowered — it is not on the path.** The roadmap forbade
fixing this by lowering the floor, which does real work for the single-model case it was written for
(P35.3). The n=1 path still calls `RecommendContextWindow` and still gets 32768; the multi-member path
calls the planner, which floors at `MinFittedContextWindow` (2048) like every other fitted answer. A
co-resident set is a different question from the one the baseline answers.

**Installed by writing real cache entries, not by adding a lookup layer.** The obvious implementation
— a `ctxWinPlanned` map consulted ahead of the cache in `effectiveContextWindowFor` — is wrong, and
the reason is a P66.14 repeat one layer down: `setWindowLocked` retunes the daemon-wide summarizer
when the model being written is `compModel`, so a parallel layer would leave the compactor budgeting
against the solo window while the runner serves the planned one. So `claimResidentSet` saves the
affected entries, writes the planned ones *through* `setWindowLocked`, and restores on release. One
source of truth, the retune correct in both directions for free, and `effectiveContextWindowFor`,
`newEngine` and `modelAdapter` need no changes at all — they pick the plan up because it is simply
what the cache now says.

**Three things that came out of building it, none of them in the plan:**

- **A claim never *raises* a window.** On the common case — every seat on the same model — the planned
  window is *larger* than the detected one, so honoring it would make every debate pay a cold
  unload/reload to gain context nobody asked for. Shrinking is the only direction a claim buys
  anything in.
- **`internal/config` has no `config.Validate`** to put the planned validation in. It went where it
  could live instead: a negative budget reads as zero, `KVCacheTypeValid()` names a typo, and a
  daemon-start check warns — staying silent when the model simply is not loaded yet, because warning
  there would train the operator to ignore the warning that matters.
- **Both new keys are frozen against untrusted project config.** Every other project-settable
  `provider.*` key describes the *work*; these describe the operator's *machine*. A cloned repo
  declaring how much VRAM the model server may hold oversizes every window on hardware it has never
  seen.

**Two corrections to the source documents, both found by doing the arithmetic:**

- **[roadmap.md](roadmap.md) P69.6** claimed 32768 tokens "alone is 8.12 GiB, leaving no room for the
  5.08 GiB arbiter", and that the floor "cannot express a co-resident configuration on 16 GB at all".
  At the geometry both documents record (135,168 bytes/token at f16), 32768 tokens is **4.13 GiB of
  KV**; 8.12 GiB is weights *plus* KV. Read correctly the pair sums to 13.21 GiB against a 14.5 GiB
  budget — **it fits**, with ~1.3 GiB spare. The floor is *marginal* for exactly two seats, not
  impossible; it binds for three seats, a larger arbiter, or a cache filled to the window rather than
  measured at low occupancy. This does not weaken the item, but it changes what the regression test
  should assert, which is why it had to be settled first.
- **[debate-topology-plan.md](debate-topology-plan.md) §1.2** labels its Topology 1 KV column `q8_0`
  while the measured figures match **f16** (4.00 GiB resident weights + 2.06 GiB f16 KV = 6.06 against
  6.02 measured; q8_0 would predict 5.10) — which would mean q8_0 is *unspent* headroom rather than
  headroom already counted. But that reading contradicts §1.1's own note that `/api/ps` grows the
  cache as tokens arrive, and §1.1's 32k reading is consistent with growth and inconsistent with a
  full f16 cache. The two readings cannot both mean what their labels say, so the discrepancy is
  recorded rather than resolved by relabelling, and the regression test asserts against the
  hand-fitted 16000 and the stated 14.5 GiB — both stated, not inferred — so it does not inherit the
  problem. Re-run `research/scripts/vram_topology_probe.py` recording the server's actual
  `OLLAMA_KV_CACHE_TYPE`, at a filled window, before anything else is pinned to that table.

**Wired at all four entry points**, so the CLI and the daemon cannot disagree about the machine they
are both running on: `POST /debate` and the TUI's `/debate` claim through the daemon's cache; the
`agent` tool's debate mode claims through a new `WithResidentSetClaim`, alongside the
`WithDebateSeatModel` P69.1 added for the same reason; and headless `aegis debate`, which has no cache
to install into, stamps each seat's adapter with `provider.WithNumCtx` from the same planner and the
same config key.

**Observable before it is enforced:** `aegis models --fit-set a,b,c` plans an explicit set, and
`--fit-debate` plans the *actual* configured seat trio, resolved through `enginecfg.DebateSeatModel`
so the diagnostic cannot pass while the debate it diagnoses runs on different models. `--budget-gb`
defaults to the config key.

**`--first-init` closes the loop.** The wizard asks for a VRAM budget on a local Ollama backend and
sizes `context_window` from `Fit` rather than `RecommendContextWindow`. Blank is a first-class answer
and produces a byte-identical config to before. When the model has never been loaded its resident
weights cannot be measured — and the tempting substitute, `/api/tags`' on-disk size, overstates
qwen35-9b by 2.57 GiB of never-resident vision projector — so the budget is written, the old
recommendation stands as the window, and the user is told to run `aegis models --fit --write` after
the first turn.

**Inert until opted in.** With `provider.vram_budget_gb` unset, every path keeps the behavior it had
before P69.6: no planning, no claim, no entry touched. That is what made steps 1–4 landable
independently of the wiring, and it is why this ships on by default.

### A context window sized to the machine, not to the model, 2026-08-17 (P69.1, P69.5)

**Two shipped items and one filed.** P69.1 gave each debate role its own model; P69.5 made a model's
serving context window computable from what the hardware actually holds. The half deliberately left
open — planning several models that must be resident *at once* — is filed as **P69.6**, the only open
Tier 1 item.

**P69.1 — a debate seat resolves its own model.** `debate.RunFunc` took `(ctx, systemPrompt,
userPrompt)`, so the three runners (daemon, headless CLI, and the `agent` tool's debate mode) each
hardcoded one `Model:`. A persona's `model:` frontmatter and a `personas.<name>.model` config
override were honored everywhere in the tree *except* the one feature whose entire premise is that
different models disagree. `RunFunc` now carries a `Seat{Role, Persona, Last}`, and
`enginecfg.PersonaModel`/`DebateSeatModel` is the single resolver all three paths share so they
cannot drift. Each seat is served with its **own** detected context window, not the primary model's —
a 3.8B arbiter handed a 9B's `num_ctx` allocates a KV cache it never fills, out of the same VRAM the
debater is holding. `buildGate` still receives `persona.Persona{}` on purpose: the seat's persona
supplies the system prompt and the model, never the tool gate.

**A verdict-inversion bug found while testing it.** `parseVerdict` used `FindStringSubmatch`, which
takes the *leftmost* match. A reasoning model that drafts `VERDICT: UPHOLD` at line start inside its
`<think>` trace and then rules `VERDICT: REJECT` was parsed as `UPHOLD`. This is not hypothetical for
the topology P69.5 measures: `phi4-mini-reasoning` reports `capabilities: [tools, completion]` with
**no `thinking`**, so Ollama cannot separate the trace and the whole deliberation arrives as content.
Confirmed in 5/5 live debates — every one contained a raw `<think>` block, and one of them emitted
its verdict from inside an *unterminated* one. Now takes the last match; single-verdict output is
unaffected.

**P69.5 — `aegis models --fit`.** `ollamainfo.RecommendContextWindow(modelMax)` sizes a window from
the model's training maximum, halved, floored at 32768. For `aegis-qwen35-9b` (training context
262144) that is 131072 tokens = **16.50 GiB of KV cache**, on a card that holds 16. New in
`internal/ollamainfo/kvfit.go`: `KVGeometry`, `BytesPerToken`, `Fit`, `WeightsBytes`, `Loaded`.

Four things that had to be right, each of which was wrong in an earlier Python prototype:

- **Weights come from `/api/ps` minus the loaded window's KV, not from `/api/tags`.** The on-disk
  size includes a vision projector that is not resident unless an image is sent — qwen35-9b reports
  6.57 GiB on disk against **4.00 GiB** actually loaded. A 2.57 GiB overstatement would eat a fitted
  window's entire margin.
- **A null `head_count_kv` is absent, not zero, and the fallback is announced.** gemma4 reports it as
  JSON null; silently substituting `head_count` overstates that model's cache **eightfold**. The
  substitution is still made — it is correct for multi-head attention — but `KVGeometry.Inferred`
  records it and the CLI prints it as a `NOTE:`.
- **Block-quantized KV is not a power of two per element.** A q8_0 block is a 2-byte scale plus 32
  int8 values: 8.5 bits each, not 8. Truncating would understate the cache, the one direction this
  code must never err in.
- **Sliding-window attention is reported but not discounted.** GGUF does not reliably say which
  layers use the window, so the estimate stays a deliberate upper bound rather than guessing the
  interleave.

**Validated against measurement, not derived.** The formula predicts 6.00 GiB for the 9B at 16000
tokens where Ollama reports 6.01 — 0.2% error — and that anchor is pinned by
`TestBytesPerTokenMatchesTheMeasuredQwen35`.

**What it deliberately does not do is detect VRAM.** `internal/hwinfo` forbids it outright ("on any
platform, ever", P17.5) because it would mean reimplementing Ollama's offload heuristic from a
fragile proxy. So the budget is an operator input, and the *verification* is empirical instead:
`Footprint.FullyOnGPU` reads Ollama's own `size`/`size_vram` split, which is its accounting of its
own placement decision rather than a guess about it.

**`config.PatchGlobalContextWindow` patches one line.** `PatchGlobalProvider` rebuilds the whole
provider block, which deletes any comment explaining why a window was chosen — exactly the thing a
calibrated number needs to keep. It refuses rather than creating a provider block that would silently
drop the adapter and base URL.

**Measured topology, on a 16 GB card:** debater `aegis-qwen35-9b:16k` at 6.01 GiB and arbiter
`aegis-phi4-reasoning:16k` at 5.08 GiB, both 100% on GPU, 11.06 GiB total. Before pinning the
arbiter's window it consumed **7.02 GiB — more than the 9B** — because it shipped with no parameters
at all and inherited a 32k window. Worked through in
[debate-topology-plan.md](debate-topology-plan.md), with the measurement harness at
`research/scripts/vram_topology_probe.py`.

---

### The gate stack finally has one home, 2026-08-17 (P66.13)

Row #1 of the rewritten "Up next" list, and the most serious *defect* left in the open set. Closes
**QUAL-01**, **QUAL-02**, **QUAL-03**, **QUAL-06**, **ARCH-05** and **ARCH-06**.

**The item was right about the shape and understated the count.** It named four instances of one root
cause with `aegis chat` as the second path. Extracting the constructor turned up two more, plus a bug
in the daemon that nothing had reported:

- **A fourth bare gate, unreported.** `internal/cli/debate.go` built
  `permission.New(ModeBuild, AutoDeny{})` directly, so an operator's deny rules and
  `egress_then_write` were inert for every debate role. Nobody had looked, because the review's four
  instances were all in `chat.go`.
- **A fifth, partial.** `internal/cli/worker.go` hand-rolled three of the five layers — it had been
  fixed for P10.1 and then drifted two behind. The per-task write scope (P46.1) and the persona-tool
  gate were absent, so a `scope` tool call in a subprocess teammate confined nothing at all.
- **The daemon dropped the user's exec hooks whenever a contextual policy was on.** `buildGate`'s
  contextual branch built a fresh `hooks.NewMulti(s.audit, ctxGate)`, replacing `s.hooks` rather than
  composing with it — so turning on `egress_then_write` or a network allowlist silently disabled
  every configured `PreToolUse`/`PostToolUse` hook for the run. The two features are unrelated;
  nothing connected them but that one line. Composing instead of replacing is the fix.

**What shipped.** Two new packages, and the count of things that must be got right per entry point
drops from "read the daemon and copy it" to one call each:

- **`internal/enginecfg`** — `BuildGate` (the five-layer stack, with the evaluation order documented
  once), `CostLimits`/`Limits.Apply` (the run bounds, plus the three provider-side ones ARCH-06 found
  missing on the CLI), `BuiltinOptions` (the config-derived half of the 27-field struct),
  `OutputGuard`/`GuardModel`, `ExecHooks`, `ConfigRules`, `Approver`.
- **`internal/sysprompt`** — the block renderers and the two local-profile byte caps the daemon and
  the CLI must agree on. Assembly stays with the daemon: `promptSections` carries the P67.2
  stable/volatile split, which is a property of a long-lived session and means nothing to a one-shot
  process.

**What `aegis chat` gained**, all of it previously configured-and-inert: `permission.rules` deny
rules, `security.egress_then_write` and the network allowlist, the persona-tool and write-scope
layers, a persona's own deny rules (`--persona` reached the prompt and not the gate),
`max_iterations`, `loop_threshold`, `redact_secrets`, the output guard, user `hooks:`, the
`commands:`/toolpath resolver (so `grep` had been falling back to the pure-Go walker there
regardless of config), `<deferred_tools>`, the debate-integration block, and both local-profile
prompt caps — on the path that *is* the local-model path.

**One behavior change worth naming rather than burying:** `aegis chat` now honors
`permission.auto_approve_exec`, which the daemon and the subprocess worker already honored and it
alone ignored. `--yes` remains the per-invocation opt-in for everyone else.

**The enabling refactor.** `newChatCmd` was 683 lines wrapping a 615-line `RunE` closure holding both
bugs (QUAL-03). It is now a flag struct plus `runChat`, with `readChatPrompt`, `openChatLog`,
`prepareChatSkills`, `buildChatGate`, `buildChatEngine`, `emitChatSummary` and a `chatDrive` type
carrying `runPhased`/`runLinear`. That is what makes the two bugs assertions instead of code
readings.

**The instrument.** `TestEveryEngineCallSiteDecidesItsGate` scans every production `engine.New` and
fails when one neither takes a gate from `enginecfg.BuildGate` nor says in a preceding comment why it
has none — the same grep-the-source shape as
`TestEveryRegisterCallSiteDecidesTheLocalProfile`, which is the instrument this defect class actually
responds to. Two sites legitimately have no gate and now say so: the tool-call probe (synthetic
in-memory tools, nothing to mediate) and the eval harness (handed its `Options` whole by the scenario
that defines it). Backed by three behavioral tests in `internal/cli` — a config deny rule, a persona
deny rule, and the `<deferred_tools>` block — the first of which was mutation-checked by nulling the
rule pass-through and confirming it fails.

**One finding corrected.** QUAL-01 lists `internal/cli/dryrun.go` as having "no gate at all". True and
harmless: `dry-run` builds no engine and executes nothing. It is a preview of the registered tool
surface, which is why the fix it needed was `BuiltinOptions` (so the preview matches a real run),
not a gate. The invariant test scans `engine.New` sites for exactly this reason.

**Verified:** full `go test ./...`, `go vet ./...`, `staticcheck` clean, `-race` on
`internal/{cli,server,enginecfg,sysprompt}`.

### The top five, 2026-08-17

Rows #1-#5 of the "Up next" ten, taken in one sitting and shipped as five commits. None of them
needed a model server; the parked live-tier row (#10) stayed parked. Each record below says what
shipped and, where the item was wrong about the tree, what corrected it.

#### P67.3 — provider requests carry a call purpose, and retry keys on it

`internal/provider/retry.go` applied one policy to everything crossing the adapter seam. The failure
mode is symmetric and that is what makes it worth fixing: a session title nobody is waiting on backed
off four times against a rate-limited backend — amplifying exactly the load that rate-limited it —
while the user's own turn got no more patience than the title did.

`provider.Purpose` (new `purpose.go`) tags a call by its **caller**, never by its content: two
identical message lists sent by the engine and by the summarizer are different calls, and only the
caller knows which. It travels two ways. `Request.Purpose` is the per-call tag, set by the component
that knows what a particular call is (compaction, the guard, the probe, the title generator, MCP
sampling). `provider.WithPurpose(ctx, …)` is a run-scoped default for a *launcher* — the code that
starts a run knows what kind of run it is, and threading that through every intermediate signature
would touch code with no business knowing about retry policy. **The per-call tag wins**
(`EffectivePurpose`), which is the whole reason both exist: a launcher says "this run is a
sub-agent", and the summarizer inside it still says "this call is a compaction". Reversing the
precedence would erase the distinction **P67.6** is built on, so a test pins it, and a second test in
`internal/compaction` pins that a summary stays a compaction inside a foreground run.

Retry **derives** from the configured baseline rather than replacing it, so `provider.max_retries`
stays the reference point in every direction: foreground adds two attempts, background (probe, title,
MCP sampling) is capped at one retry and a 5s backoff, and everything else — including every untagged
call — is the baseline unchanged. Two consequences are deliberate: nothing regresses by omission, and
`max_retries: 0` still disables retries everywhere including the user's turn, because the foreground
bonus is a delta on a baseline rather than a floor.

**Compaction, the guard, sub-agents and debate roles are attended, not background.** The classifying
question is not "is this the user's turn" but "who is blocked while this retries", and the turn those
four serve fails with them — failing them fast spares nobody. `TestUrgencyOf_ClassifiesEveryPurpose`
is that classification written down, and an unknown purpose resolves to the conservative baseline.

The `Retry-After` clamp at `MaxDelay` is untouched in mechanism, as the item required — it is what
keeps provider backoff inside the 900s `MaxTurnStall` bound without the retry path needing heartbeats
— and now clamps to the *purpose's* `MaxDelay`, which only ever tightens it.

**One correction.** The item lists cron jobs among the callers sharing the policy. Cron fires shell
commands (`newCronRunFunc` → `runCronCmd`), never a provider request, so there is no cron purpose to
tag today. The other eight callers it names are all real and all tagged.

#### P66.25 — trust grants are bound to content, not just to a path (SEC-07)

The last P66 item, and the last security item in the open set. A grant said "this path is trusted"
and never "this content is trusted", so a `git pull` adding a `hooks:` block, flipping `security.*`
or introducing a `commands:` override re-prompted nothing.

The grant now carries a SHA-256 fingerprint over the security-relevant subset of that directory's
*own* `.aegis/config.yaml` — exactly the keys P66.5's inverted freeze list does not mark
`projectSettable`, which is why the item only became coherent after P66.5 shipped. `policyFor`
defaults an unlisted key to frozen, so a dangerous key added in a later release is fingerprinted the
day it exists rather than the day someone remembers it. Only the project file's keys are hashed,
never the merged config: a digest that moved when the operator edited their own global config, or
fired on an ordinary `log_level` edit, would train them to re-accept without reading, which is the
failure mode a re-prompt has to avoid to be worth anything.

**The `.env` question was settled the documented-partial way, and the reasoning is the item's real
content.** Covering `.aegis/.env` would mean parsing project-controlled content ahead of the trust
decision — the precise ordering P66.1/SEC-01 exists to prevent — so the two cannot both be had. It is
also the smaller hole: `.env` is read only in an *already trusted* workspace, and may not set
`AEGIS_*` at all, so an unfingerprinted `.env` edit cannot change an Aegis setting the way an
unfingerprinted `hooks:` or `commands:` block can. What it can still do — set ordinary environment
variables a child process reads (`PATH`, `GIT_SSH_COMMAND`, …) — is written down as a residual risk
in `SecurityFingerprint`'s comment, in `docs/configuration.md` and in `docs/cli-reference.md`, with
`--revoke` as the mitigation, and `TestDotEnvIsNotFingerprinted` makes silently *starting* to cover
it a test failure too.

**Migration: a pre-fingerprint grant is `Stale`, not `Trusted`.** Those grants were made against
content nobody recorded, so "it still matches" is not a fact anything can check, and adopting the
current content would bless a `hooks:` block that arrived between the grant and the upgrade — exactly
the silent inheritance the item exists to end. One re-prompt per already-trusted directory, paid
once. `Stale` gates identically to `Untrusted` (the freeze applies, nothing is unlocked) and exists
as a separate value only so the operator is told "what you approved has changed" rather than "you
never approved this"; it reaches `aegis trust`, `aegis doctor`, the stderr startup warning and the
daemon's `stale_grant` log field.

Two things the item did not anticipate. The `--dir` path (`workspace.additional_roots`) needed the
same treatment rather than an exception, since a `--dir` grant and a cwd grant key the same store
entry and would otherwise mean different things for one path. And the write paths that self-trust
(`PatchProjectSandbox`, `PatchProjectSecurity`, `AppendProjectPermissionRule`) had to record the
fingerprint strictly *after* their own write, or the operator's own edit would have gone stale the
instant the call returned.

#### P67.4 — a failed tool call cancels its siblings

`Engine.runTools` ran every call in a round to completion regardless of what its siblings did. The
round now runs under its own context derived from the turn's, cancelled on the first qualifying
failure, so sibling subprocesses die promptly instead of finishing work the model is about to redirect
past — which also shortens the aggregate wait `MaxTurnStall` backstops.

**The parent/child split is the invariant, and it is tested.** Cancelling siblings must never cancel
the turn: the checks after `wg.Wait()` still read `ctx`, not `roundCtx`, so only the turn's own
cancellation ends the run, and a test asserts the model is called again and reaches its final answer
after a round is cancelled.

**Which failures qualify: write/execute only.** A read that fails is a normal negative result — a
`read_file` on a path the model guessed wrong is how it learns the path is wrong — and cancelling for
that would make speculative parallel reads, most of what the parallel round is for, unusable.
`serialize[i]` is exactly that classification, already computed for scheduling via
`tool.EffectiveCapability`, so the policy hangs off it rather than off a second copy that could
drift, and it inherits the per-call refinement that keeps a shell call reclassified as read-only from
cancelling anything.

**What the cancelled siblings report follows P65.1 unchanged.** A call abandoned before `Execute`
says plainly that nothing was executed — the one cancellation case where that is honestly assertable.
A call already running keeps whatever the tool reported and gets the cancellation appended, never a
claim that it did not run, because a model told that re-runs it. Every result slot is filled either
way: a `tool_use` with no `tool_result` is a protocol error, so filling them is what makes cancelling
safe at all, not bookkeeping. Cancellation is also honored at both *waits* — the same-path dependency
graph and `execLock` — since otherwise it would shorten only a round's concurrent tail and not its
serialized spine, which is usually the expensive part.

**One interaction the item did not anticipate.** Marking cancelled siblings as errors puts them in
front of the P52.3 tool-failure breaker, which keys its nudge and its abort on the *first* error in
the round — and a round is a set of concurrent calls, so nothing says the failing one has the lowest
index. A sibling cancelled at a lower index would have become the error the breaker reports, nudging
the model about "tool call skipped" instead of about the build that failed. Cancellation artifacts
carry two stable markers and are skipped in `toolFailureTracker.record`; removing that guard makes
the test fail with the wrong tool named, which is how it was verified.

#### P67.5 — recall dedupes across turns, renders age, and biases toward gotchas

Three behaviors around `internal/memory`'s TF-IDF scoring, which is unchanged. A per-run
`RecallState`, threaded through a new `RecallOptions` to `LoadRelevantFor`, drops entries the run has
already been shown — **before** scoring, as the item required, so the entry budget goes to candidates
the model can still use rather than being spent on repeats and then emptied. Marking happens on what
is actually returned, so an entry cut by the entry/token budget stays eligible next turn instead of
being burned unseen. The state is caller-owned and per-run by construction; a package-level set would
starve the next run of what the previous one consumed.

`Entry` now carries the `ModTime` from the stat pass that already keys the P8.5 relevance cache
(`entriesSignature` became `statSources` — one walk, two consumers), so `FormatEntries` renders age
in a coarse hour/day/month ladder at no extra I/O. Scoring gained a documented reference-vs-gotcha
bias: an entry warning about a tool the run has recently called is multiplied by 1.5, a pure how-to
entry for a tool being driven without failures by 0.5, and a single failed call withdraws the damp.
The asymmetry is the point — a "successful" call is exactly when a silent gotcha bites. It is a
multiplier around TF-IDF rather than an override, so it reorders near ties and can never manufacture
relevance for an entry the query does not match. Both factors and all three age cutoffs are
mutation-tested against literals.

**The correction is larger than the item.** `LoadRelevant` and `FormatEntries` have **no production
callers**. Memory reaches the prompt through `Sources.Load()`, which injects both memory files whole
and unfiltered. The item's stated symptom — a top-scoring entry re-injected every turn it keeps
winning — therefore could not have been observed: the scored-recall path is unwired, and these three
behaviors take effect when a caller adopts it. A second, smaller correction: per-entry timestamps are
not recoverable from an append-only, hand-editable `memory.md`, so freshness is file-mtime granular —
an upper bound on staleness rather than the entry's own age.

#### P67.2 — the prompt gets a stability invariant, not just a size ceiling

`effectiveSystem` was an anonymous `[]string` of appends, so nothing stopped a volatile value from
being assembled into the system prefix, where it breaks the prompt cache every turn and shows up only
as unexplained prefill cost. The blocks are now named `promptSection` values built through one of two
constructors: `stableSection`, memoized for the conversation under a `(sessionID, local-profile)` key
cleared with the session's other in-memory state, and `volatileSection`, which recomputes per turn and
**takes a written justification as a required argument**, panicking on an empty one — the list is
rebuilt on every call and by the tests, so that fires at first construction rather than in production
only. `TestPromptSections_StabilityInvariant` computes every section twice and fails any that differs
without the declaration, mutation-verified against a deliberately unstable section. Same shape as the
`localBasePromptCeilingTokens` ceiling, on the axis that costs per turn rather than per request.

**The correction: the split came out asymmetric, and only four of ten sections are safe to memoize.**
The three persona blocks and the debate block are config-derived prose; every other block reads state
Aegis itself mutates mid-conversation — `activateSessionSkill` adds skills, the `memory` tool appends
facts, `tool_search` and MCP's `tools/list_changed` move the deferred inventory, context files are
re-read as the user and the agent edit them, and the repo map rebuilds as the agent edits the
workspace. The item's framing (memoized by default, volatile as the exception) would have served
stale prompts on five sections. Correctness took the cache's place and each carries its reason in the
file. The prompt's content and order are byte-identical; the existing composition, ceiling, cap and
P39.1 byte-stability tests pass untouched. The cheap win still lands: the volatile set is now the
exhaustive, justified list of what breaks prefill reuse each turn, which is the input P67.6 and any
later prefix-cache work needs.

### The temperature A/B that measured nothing, twice, 2026-08-17

Recorded because a void experiment that goes unrecorded gets re-run, and because the shape of this
one is the shape of every false null. With P68.3's graded task in hand, the obvious next question was
whether the sampling parameters `docs/local-model-tuning.md` recommends on judgement actually help.
Temperature 0.2 against 0.6, single-variable Modelfiles:

| substrate | 0.2 | 0.6 |
|---|---|---|
| `aegis-qwen35-9b:32k` | 12, 12, 12 | 12, 12, 12 |
| `qwen3:14b-32k-fix` | 3, 3, 3, 3, 3 | 3, 3, 3 |

Flat in all four arms — and **none of it is evidence that temperature does not matter**. The 9b has
exhausted the rubric and the 14b is pinned at one repeated minimal strategy; a saturated instrument
returns exactly this pattern regardless. The substrate call was mine and it was wrong twice: the 9b
was chosen on a 10.7/12 mean that included one run scored under the *old* grader, before the
collapsed-criteria fix, so its true level was already at the ceiling and there was never room for
degradation to show.

**Two instrument checks ran before concluding**, which is the only reason "no headroom" survives as
the reading rather than being a guess: `ollama show` confirmed the derived models differ solely in
`temperature`, and the P68.2 detector confirmed all four still carry the **corrected** chat template —
`FROM <derived model>` silently losing it was the obvious way for this to be a P68.2 regression
wearing a null's clothes.

Filed as **P68.4**: the rubric ranks models well and configurations not at all, because its useful
measuring band sits below the strongest local model available. The tuning page's sampling section
stays labelled reasoned-not-measured, and now says explicitly that two experiments were void.

---

### The tier's task was a boolean, 2026-08-17 (P68.3)

**P68.2's re-run came back at p ≈ 0.45, and the honest diagnosis was not "six runs is too few".**
`TestLiveWorkflow/FixSeededBug` is pass/fail, so a run yields **one bit**; five of six control runs
scored zero by *giving up after a single tool call*; and its ideal path is three tool calls, so a
model never has to carry a fact from an early turn to a late one. That last point is the damning one:
**the tier's discriminating task was structurally blind to the class of defect the tier had just
caught in P68.2.**

**Shipped: `TriageTask`** — a graded security-triage task scored out of 12. Nine pure-stdlib Python
files (no pytest, no pip: a missing dependency and a weak model must not produce the same red), five
planted issues from trivial to cross-file, one file clean on purpose:

| criterion | points | what it stops |
|---|---|---|
| discovery (5 × 1) | 5 | — |
| precision | 2 | naming every file in every category without reading anything |
| integrity | 2 | editing the test suite the remediation points are read from |
| remediation (2 × 1) | 2 | — |
| no-regression | 1 | "fixing" traversal by refusing every path |

Grading is entirely mechanical — parse a JSON report, run a suite, hash the protected files. **No LLM
judge**, deliberately: a judge model puts a second model's variance inside the instrument, and the
instrument is the thing being trusted. `SeededBugTask` is kept and relabelled as the tier's
**control** — small and unambiguous, so a harness that fails it is failing at driving a model.

**Measured the same day, n=3 per model:**

| model | scores | mean |
|---|---|---|
| `aegis-qwen35-9b:32k` | 9, 11, 12 | **10.7 / 12** |
| `qwen3:14b-32k` (mitigated) | 3, 2, 3 | **2.7 / 12** |

Complete separation at **n=3** (exact Mann-Whitney p = 0.10 two-sided, the floor for this n) against
the old task's p ≈ 0.45 at n=6. And the failures are now *specific*: the 14b **never wrote
`findings.json` in any of three runs** despite greping extensively — a reporting-step failure, which
a boolean would have rendered as the same red as "gave up immediately".

**The first live run found a defect in the grader, and that is the part worth keeping.** A run that
left the project unable to import collapsed all three run-dependent criteria onto one truncated
`Traceback (most recent call last):` — a rubric printing the same non-diagnosis three times, which is
precisely the "criteria that fall together" failure the rubric's own doc comment warns about.
Breaking the code is now charged **once**, to `no_regression`, naming the actual exception; discovery
is explicitly unaffected, so a broken build cannot erase a correct audit. This is the third time this
document has recorded a fresh instrument being wrong on its first real use, and the third time the
thing that caught it was checking mechanism rather than reading the number.

Tests: eleven in `internal/eval/triagetask_test.go`, led by `TestTriageFixtureStartsVulnerable` —
the two security tests must **fail** and the two functional tests **pass** before an agent touches
anything, or the task measures nothing while looking green (the P62.2 failure, twice).
`go build ./...` + `go test ./...` + `staticcheck ./...` green.

---

### The template that ate the tool calls, 2026-08-17

**The question was "why does `aegis-qwen35-9b:32k` work better than `qwen3:14b-32k`".** Most of the
answer is not a property of either model. Ollama renders history server-side from the model's own
chat template, and the two models ship *different kinds* of template: the 9b a **Jinja** one
(Ollama's newer renderer), the 14b the stock **Go text/template**, whose assistant branch reads

```
{{ if .Content }}{{ .Content }}{{ else if .ToolCalls }}<tool_call>…{{ end }}
```

Content and tool calls are mutually exclusive. `translate` emits both on every turn where the model
narrated before calling, so on the 14b **the call was being deleted from the rendered history** —
the model saw a tool result for a call it had no record of making, and the arguments (paths, edits,
commands) went with it.

**Measured**, temperature 0, three trials per arm. History: prose + `read_file{path:"srv/etc/config.txt"}`
+ result, then "which path did you read?"

| arm | correct |
|---|---|
| `qwen3:14b-32k`, as sent today | **0/3** — answered `/etc/config.txt` |
| `qwen3:14b-32k`, prose withheld | **3/3** |
| `qwen3:14b-32k`, template's `else if` split into two `if`s | **3/3** |
| `aegis-qwen35-9b:32k` (Jinja) | **3/3** |

A one-shot tool call is fine on both (3/3 each, including multi-line arguments with escaped quotes),
which is why the toolcall probe never caught this: **the defect only exists in multi-turn history**,
which is everything the engine does.

**The obvious fix does not work.** Splitting the turn into two messages — prose, then call — was
tried first and measured 0/3, unchanged: Ollama coalesces adjacent same-role messages before
templating, so the pair arrives at the template as the same content-plus-calls message. Worth
recording because it is the change a reader of the template would reach for.

**What shipped.** `ollamainfo.TemplateDropsToolCalls` reads `/api/show`'s template and detects the
`else if … .ToolCalls` shape (Jinja templates are reported clean). The adapter asks once per model,
persists the verdict in `internal/modelcaps` so the next process reads it from disk, warns once, and
withholds the prose on affected models so the call survives. It is **off unless wired**
(`ollama.WithTemplateProbe`) — `internal/providerfactory` is the only site that talks to a real
Ollama, so a test stub or a non-Ollama endpoint issues no surprise request, and a model with a
correct template keeps today's exact bytes and its prefix cache.

Withholding prose is the lesser loss, not a good outcome: the narration is commentary, the call
carries the arguments the rest of the history refers back to. **The better fix is on the model
side** — rebuild with an assistant branch that renders `.Content` and `.ToolCalls` in sequence, which
keeps both (verified: 3/3) — and the warning says so.

**Detector verified live** against five local models: `qwen3:14b-32k` and `qwen2.5-coder:1.5b`
flagged; `aegis-qwen35-9b:32k`, `gemma4:12b` and a template-corrected 14b clear.

**The end-to-end re-run, same day, n=6 per arm — and it does not confirm anything.**
`TestLiveWorkflow/FixSeededBug`, three arms, same fixture:

| arm | passed | tool calls per run (median) |
|---|---|---|
| unmitigated (pre-fix `317c388`) | **0/6** | 1, 1, 4, 1, 1, 1 (**1**) |
| mitigation active | **1/6** | 2, 1, 3, 1, 2, 2 (**2**) |
| template-corrected model | **2/6** | 9, 3, 39, 1, 1, 4 (**3.5**) |

0/6 against 2/6 is **not significant** (Fisher's exact, p ≈ 0.45). Recorded because the temptation
was to report "the fix took the tier from 0 to 2 passes" and that would have been the same mistake
this document has twice caught elsewhere: an instrument reporting a number it cannot support. The
seeded-bug task is already known to be too weak to settle differences this size — that is **P62.9**'s
standing conclusion, and this run reinforces it.

What the run *is* good for: the control arm went from anecdotal (0/2 on 2026-08-16) to
**characterised** — 0/6 today, 0/8 in total, and in five of six runs it ran the script, read the
traceback, and stopped after a **single** `shell` call. And the **failure shape moves with the fix
even though the pass rate does not**: median tool calls per run 1 → 2 → 3.5 across the arms. The
corrected arms keep working the problem; the unmitigated one quits.

**The template finding never depended on the tier.** It is settled by the history probe above — 0/3 →
3/3, deterministic, one variable flipped — which is why that probe, not this table, is what the fix
rests on. Worth stating plainly because the tier is the more impressive-looking instrument and the
weaker one.

**Written up for reuse:** [docs/local-model-tuning.md](../docs/local-model-tuning.md) is the
procedure — extract the template, detect the `else if .ToolCalls` defect, patch it, pin `num_ctx`,
set sampling for tool fidelity, and verify with the three checks (doctor → history probe → live
tier). It is explicit about which of its recommendations are measured (the template) and which are
reasoned defaults (the sampling parameters).

**Two existing records are now suspect**, which is the part worth carrying forward. `qwen2.5-coder:1.5b`
is affected, and it is the model behind **P52.16**'s `toolResultEcho` measurement (32/40 → 38/40) —
that experiment was run through a template that was deleting the calls being correlated. And
**P62.9**'s verdict that "the seeded-bug task is measuring model competence, not tool reachability"
rests on two 14b failures whose shape (rewriting a file, then reporting a confidently wrong result)
is what a model does when it cannot see what it just did. Neither is retracted here — both need a
re-run, which is **P68.2**.

Tests: `internal/ollamainfo/template_test.go` pins the detector against **templates captured from a
real server** (`testdata/`) rather than synthetic ones, because the defect is a property of
vendor-shipped templates and a fixture would keep passing after they changed shape.
`internal/provider/ollama/toolcallprose_test.go` pins both directions of the switch, the
once-per-model probe, the cross-process persistence, and that an unreadable template is *not*
persisted. `go build ./...` + `go test ./...` green.

---

### The live-tier sitting, 2026-08-16 — the compaction A/B finally measured something

Row #1 of the "Up next" ten: one setup, five items. **Models:** `qwen3:14b-32k` (Q4_K_M) first, then
everything except P38.1 again on `aegis-qwen35-9b:32k` (Qwen3.5-9B-MTP, UD-Q4_K_XL) — both with
`num_ctx 32768` pinned in the Modelfile and fully resident in VRAM. Ollama on `:11434`, `-count=1`
throughout, windows/amd64. Two items closed, two moved, one did not run — and the second model
turned P62.9's open watch item into a positive result, so read that subsection too.

**What the sitting produced, item by item:**

| Item | Outcome |
|---|---|
| **P62.2 / P65.3 local half** | **Measured, first time ever.** The A/B ran with compaction actually firing — after its fixture was found defeated and rebuilt. Numbers below. |
| **P66.22 / LLM-01** | **Measured.** Local profile 4,871 provider-reported first-turn tokens vs 8,393 default, both against a 16,384 window neither saturated. The deterministic budget test reports 4,336 estimated bare / 6,383 with an over-cap context file (ceilings 4,550 / 6,650) — the 11,611-token figure P66.7 was filed on is now three fixes stale. |
| **P66.22 / LLM-02** | **Confirmed fixed, and a new phenomenon found.** Compaction fires — repeatedly. See "compaction thrash" below. |
| **P62.9** | **Two more runs, and the answer is still "the task is too weak to arm this".** Both failed the outcome check, in two different ways, with `edit_file` deferred. |
| **P38.1** | **Did not run.** It needs an unattended agent with auto-approved host shell, which this session was not permitted to launch. Nothing about the item changed. |

#### The fixture was defeated by `{"limit":1}`, and that is the finding

`TestLiveWorkflowCompactionPrefixCacheGate`'s first run of the day came back **PASS-shaped and
empty**: 12 turns per arm, wall 16s vs 15s, prefill 953ms vs 951ms, and the instrument check
correctly failing both arms with *"no compaction actually ran, so this run measures nothing about
the gate"*. The cause is one tool argument. The chain files were 60 numbered lines with the pointer
on the last one; the model called `read_file {"limit":1,"path":"data_04.txt"}` on every file, got a
**29-byte** result, and walked the whole chain at ~55 tokens per turn instead of ~1,950. The prompt
grew 4,951 → 5,560 against a 24,576 window across twelve turns and never came near the trigger.

This is the *same* failure the fixture's own doc comment already records once (the first version was
batched into two turns by `CapRead` concurrency), arriving through a different door: the fixture
forced *sequencing* structurally but left *payload size* to the model's discretion, and
`read_file`'s paging window is exactly the lever for that. The rebuild makes each file **a single
physical line** — payload and `next:` pointer on the same line — so a line-count window cannot
shrink a result whose whole payload is line one. A `strings.Count(content, "\n") != 1` assertion in
the generator holds the property, because a stray `\n` in a format string would silently restore the
defect and the A/B would go back to measuring nothing while looking fine.

**The instrument check is what saved this.** It was added because the fixture's first version came
back green and empty; it did the same job again today against a completely different cause. That is
the argument for asserting *mechanism* alongside a reported measurement.

#### P62.2 — the prefix-cache gate costs nothing measurable at a 24,576 window

Both arms, 14-file chain, `num_ctx 24576`, `max_tokens 8192`, model unloaded before each arm so the
pin took (window reported as 24576 *from config* in both):

| | gate on | gate off |
|---|---|---|
| wall | **4m58s** | **5m03s** |
| total prefill (turns 1..n) | **213,118ms** | **219,038ms** |
| turns | 15 | 15 |
| reads | 14 | 14 |
| shrinking turns | 1 | 1 |
| compactions | 11 | 11 |

A 2.7% prefill difference in favour of the gate, on a sample of one, with the two arms tracking each
other turn for turn (turn 4: 17,877ms vs 17,860ms). **That is noise, and the honest reading is that
this workload cannot tell the two settings apart** — not that the gate is free in general. The
regime it was designed for is the one P62.8 still has never measured (a >200,000-token window, where
`shouldPrune` switches from the 25%-free ratio to a fixed 40k buffer); nothing here speaks to it.

#### The finding the A/B was not looking for: compaction thrash, every turn, freeing nothing

Both arms show the same shape, and it is more interesting than the gate:

```
turn  3: prompt= 15329 prefill=  4570ms
turn  4: prompt= 19146 prefill= 17877ms   <- first compaction
turn  5: prompt= 19152 prefill= 17768ms
...
turn 14: prompt= 19502 prefill= 18572ms
notice: context ~87% full — compacted 11→9 messages   (x11)
```

Eleven compactions in fifteen turns, one per turn from turn 4 onward, each summarizing exactly **two
messages** and leaving the conversation at ~90% of the window — so the next turn crosses the trigger
again immediately. Prefill quadruples the turn compaction starts (4.5s → 17.9s) and never comes
back down, because a rewritten history is a broken prefix every single turn. The run spends **~87%
of its wall clock in prefill**.

This is not the gate's doing (it is identical with the gate off) and it is not the trigger
disagreeing with itself (P66.14 fixed that; the trigger fires exactly where it says it will, at 85%
of 24,576 ≈ 20,889). It is that **compaction's yield per invocation is smaller than one turn's
growth** on this workload: `keepRecent`'s tail plus the base prompt already fill the window, so
there is only ever a two-message head to summarize. P62.7's minimum-yield rule is the mechanism
meant to hold compaction off in exactly this state, and it did not suppress a single one of these
eleven — worth a look before **P67.6** gates compaction on anything further.

**P65.3's Question 2 is answered by the same table**, and answered yes: on a local backend, a
summarizer call between turns raises the next turn's prefill by roughly **4x** and keeps it there.
The mechanism half (cloud) shipped 2026-08-15; this is the local half it was waiting on.

#### P66.22 / LLM-01 — the prompt numbers, measured

`TestLiveWorkflow/LocalPromptProfileReducesFirstTurnTokens` passed with neither count clamped:
**local = 4,871**, **default = 8,393** provider-reported first-turn tokens, window 16,384 *from
config* on both. The daemon's own notice on the default arm — *"system prompt and tool schemas are
~8487 tokens of a 16384-token context window"* — is P66.7's warning working as designed.

Against a realistic project the deterministic half is now the tighter instrument:
`TestEffectiveSystem_localProfileBudget` reports **4,336** estimated tokens bare and **6,383** with
an over-cap `CLAUDE.md` present (ceilings 4,550 and 6,650). LLM-01 was filed on an 11,611-token
estimate for the context files alone; P66.7's cap has since bound it to ~2,000. The estimate and the
provider's count agree to within ~11% on the same shape of prompt, which is as close as a heuristic
and a tokenizer are expected to get.

#### P62.9 — two more runs, same verdict as 2026-08-14, for a new reason

`TestLiveWorkflow/FixSeededBug`, `edit_file` deferred (local profile), two runs:

- **Run 1** — 3 tool calls (`shell`, `write_file`, `shell`), 43.6s, no detour, no approval, non-zero
  usage. It rewrote `temps.py`, re-ran it, and produced `Average temperature: 22.50°C` where the
  task wants `75` — a **wrong fix, verified by the model and still wrong**.
- **Run 2** — 1 tool call. It ran the script, read the `TypeError`, and stopped without editing
  anything. The outcome check re-ran the script and got the original traceback back.

Neither run touched `tool_search`, which is the detour P62.9 was actually watching for — consistent
with 2026-08-14 and now at n=5 across sittings. But **the task cannot arm the comparison it is
being used for**: the failure mode both times was task competence, not tool reachability, and
2026-08-14 recorded the `edit_file`-exposed control failing outright twice as well. P62.9's own
closure condition asks for n≥10 per arm *or a task whose edit is unambiguous enough that a single
run means something*; five runs across two sittings say the second half of that sentence is the one
worth building.

`GuardNoMetaLeak` passed — no `PASS.`/`FAIL:`/`VERDICT:` leakage — and logged the *"model ended its
turn with no text — asking it for a plain-text answer"* corrective, which is the P25.3 path working.

#### Second model, same day: `aegis-qwen35-9b:32k` (Qwen3.5-9B-MTP, UD-Q4_K_XL, `num_ctx 32768`)

Everything above except P38.1, re-run against a second local model. **`TestLiveWorkflow` passes in
full — all three subtests — which is the first time the seeded-bug task has ever been solved on this
tier.** Five tool calls, 13.8s: `shell` (reproduce) → `read_file` → `shell` (inspect the CSV) →
`multi_edit` → `shell` (verify). No `tool_search`, no detour, no approval.

**This is the arm P62.9 was actually asking for.** With `edit_file` deferred under the local profile,
the model went straight to `multi_edit` with a one-line anchored edit — `total += row["temp"]` →
`total += float(row["temp"])` — and the outcome check passed. The guard subtest, working the same
task the long way, is the more interesting record: it tried `edit_section` (error), then `multi_edit`
(error), then re-read the file with `Get-Content` and got `multi_edit` right on the next attempt.
**Two failed edits and a self-directed re-read, not a tool-failure-breaker trip** — the recovery path
P39.16's handle-based tools were built for, working. The error *text* is not in the SSE stream, only
its length, which is the same blind spot P65.2 hits.

The A/B reproduces the compaction thrash exactly, at a different model, tokenizer and speed:

| | gate on | gate off |
|---|---|---|
| wall | **3m24s** | **3m34s** |
| total prefill | **110,812ms** | **111,457ms** |
| turns / reads / compactions | 15 / 14 / 11 | 15 / 14 / 11 |

0.6% apart — noise again, now at n=2 models. Prefill steps from ~2.3s to ~9.2s at the first
compaction and stays flat; eleven compactions, every one of them 11→9 messages. The one difference
worth noting is **where it settles: ~96-97% full**, against ~90% on `qwen3:14b-32k`, with the prompt
still creeping up turn over turn (20,200 → 20,724). The trigger is the same 20,889; this model simply
generates less per turn, so each cycle re-crosses it from closer to the ceiling. The phenomenon is a
property of compaction's yield, not of one model's verbosity.

**One number for LLM-01 that only a second model can show:** the same prompt bytes cost **5,775 /
9,591** first-turn tokens here (local / default) against **4,871 / 8,393** on `qwen3:14b-32k` — ~19%
more for an identical prompt. The budget in `localBasePromptCeilingTokens` is estimated in
`tokenest`'s units, and a ceiling in estimated tokens is not a promise about any particular
tokenizer's count. Worth remembering before a future measurement is read as a regression.

#### P65.3 — closed

Both halves are now answered, so the item leaves the roadmap. **Question 1 (cloud)** shipped
2026-08-15: `provider.Request.SuppressCache`, set by the summarizer and the guard, honored by the
Anthropic adapter, pinned by `TestPromptCachingSuppressedPerRequest`. **Question 2 (local)** is the
measurement above — a summarizer call between turns raises the next turn's prefill by ~4x on
`qwen3:14b-32k` (4.5s → 18s) and ~4x on `aegis-qwen35-9b:32k` (2.3s → 9.2s), and it does not recover
while compaction keeps firing. Whether that 4x is worth acting on is **P67.6**'s decision, not this
item's.

One adjacent gap found while answering Question 1 stays open and is *not* part of this closure: the
compaction and guard call sites never read `ev.Usage` off the stream, so their token cost is invisible
to Aegis rather than merely unattributed. Worth its own item if session cost totals should include it.

#### What this sitting did not close

- **P38.1** — blocked on permission to run an unattended agent with auto-approved host shell, not on
  the model server. The fresh target copy was staged and the recipe unchanged.
- **LLM-10** (model reload between the tool-call probe and the first real turn) and **ARCH-04** (a
  fan-out or debate call tripping `MaxTurnStall` before its own timeout) — neither is observable
  from the workflow tier as it stands; both want a session trace (`aegis sessions trace <id>`) from
  a run whose data dir survives, which these throwaway-daemon tests delete.
- **LLM-03** — the fix is in and the path is right, but *"non-zero calibration sample count"* was
  not read directly: the tests' daemons keep their sessions in temp data dirs that are removed on
  cleanup. What was observed is consistent with it (`estimated=false` usage on every `done` event,
  estimates tracking the served counts to ~11%), which is evidence, not the closure condition.

### P66.14 — one compaction trigger, and a calibration that reaches the documented path

Closes **LLM-02**, **LLM-03**, **ARCH-07** and **PERF-03** — four findings in the token-accounting
path, grouped because they share one seam.

**LLM-02 — the two thresholds.** `engine.compactionTrigger` sized its trigger against the completion
the request may ask for (P59.1); `compaction.Summarizer.shouldCompact` applied a flat 20%-free rule
that never saw `maxTokens`. At a 4,096-token window the engine asked for a compaction at 2,048
estimated tokens and the summarizer refused until 3,277. The formula now lives in
`tokenest.CompactionTrigger(window, maxTokens)` — tokenest, because both packages already import it and
neither may import the other (engine's own tests import compaction, so that dependency would close a
cycle) — and the engine additionally passes **the number it actually used** down per call, so the two
cannot differ even if their configuration does. `shouldPrune` moved with it: the pre-pass gate is
expressed as a *lead* over the shared trigger (5% of the window, or 20k on a large one), which is
exactly the relation the old pair of constants encoded.

Direction, stated because unifying means one side moves: on any window small enough for the completion
reservation to bind, the shared trigger is *earlier* than the summarizer's old 80% — that is the LLM-02
case. Above ~133k tokens it is the 85% ceiling, marginally *later* than 80%. The engine's gate is the
one sized against the completion, so it wins in both directions.

**The finding this produced, which is the most useful thing in this record.** P62.7 shipped a
minimum-yield rule against a measured phenomenon: eleven consecutive prune-only compactions, each
freeing ~45 estimated tokens against a gap that grew from 1,462 to 4,332. **That band was the threshold
disagreement.** Every turn between the engine's trigger (15,156 on the P62.7 fixture) and the
summarizer's refusal (19,660) called `Compact`, got the deterministic pre-pass and nothing else, and
paid a rewrite for almost nothing. With one trigger the fixture's first over-trigger turn summarizes 91
messages into 9 and the following nineteen turns need nothing at all — the applied-on sequence went
from `[5 10 16]` to `[1]`. `TestLowYieldPruneStopsRecompactingEveryTurn` was rewritten as
`TestSharedTriggerLeavesNoPruneThrashBand`, asserting the absence of the band; the minimum-yield rule
itself is untouched and still tested by the stub-driven cases, which construct a low-yield prune
directly instead of relying on a threshold gap to produce one. This is the third time this project has
recorded a fixed instrument changing a verdict that had already been acted on.

**LLM-03 — the calibration was inert on the documented path.** `afterTurn` gated on
`PromptEvalDurationMS > 0`, which is *telemetry only the native adapter populates* — so P62.4's
correction never fired on `provider.default: openai` with an `:11434/v1` base_url, the configuration
`docs/providers.md` itself recommends. Every session on that setup ran the whole way on the uncorrected
20-33% undercount, with no signal that the calibrator had never taken a sample. The gate is now
`Options.SharedContextWindow`, set from a new `providerfactory.CertainlyOllama` (the native adapter, or
the compat adapter on an `:11434` base_url) — a positive identification of the *backend*, which is what
that gate was reaching for. Deliberately narrower than `config.LocalBackend`, which also matches LM
Studio and any loopback proxy: those report prompt tokens on their own terms.

**ARCH-07 — per-run data on a per-server object.** `SetEstimateCorrection` pushed `overhead` — the
estimate of the *calling run's* exposed tool schemas — onto a `Summarizer` built once per server and
shared by every session, which `filecontext.go`'s own comment argues a setter cannot do safely. The old
comment claimed the exemption on the grounds that "a calibration is process-wide"; the overhead
travelling with it is what makes that false. Replaced by `compaction.WithTokenBudget(ctx, overhead,
scale, trigger)` and `engine.BudgetedCompactor`, a context decorator in the same shape as
`FileContextCompactor` — and the same seam carries the trigger, which is what closes LLM-02
structurally rather than by convention. `CalibratedCompactor` is gone.

**PERF-03 — a snapshot that could not see `tool_search`.** `compactionGuard.requestOverhead` was
measured once in the constructor, on the argument that a mid-run exposure change moves it by less than
the estimate's own error. That is wrong by an order of magnitude: a single deferred schema is up to 593
estimated tokens against a 4,550 budget — 13% — where the calibrated estimate's residual error is a few
percent. It is now a function memoized on a new `tool.Registry.SchemaVersion()`, which increments
wherever `schemaCache` is invalidated, so an unchanged exposed set costs a counter read and a changed
one pays the re-render it needs. `afterTurn` now calibrates against the overhead recorded *at request
time* (`lastOverhead`) rather than re-reading it, since a turn that loaded a deferred tool would
otherwise be paired with the next turn's schemas.

Tests: `internal/tokenest/trigger_test.go` (the moved value table, the never-later-than-85% guard, and
the prune gate's order relative to the trigger asserted in both directions);
`internal/compaction/budget_test.go` (the shared trigger at the shipped default pair, a caller-supplied
trigger overriding the Summarizer's own, the zero-budget degradation, and that the scale is applied
*after* the overhead — the other order leaves the schemas uncorrected);
`internal/engine/overhead_test.go` (overhead follows a `reg.Load`, is memoized, and every exposure path
moves the schema version, including a scope's *restore*); and
`internal/engine/tokencalib_test.go` rewritten around admissibility — an identified backend with no
prefill telemetry is now a sample, which is the LLM-03 regression stated as the case that must pass.

### P66.11 — a redaction pass, and a turn trace worth reading

Closes **SEC-08**, **SEC-11**'s redact-don't-truncate half, and **GAP-01**. Two halves of one item:
what leaves the process, and what is kept about a run.

**The pattern set moved before it was reused.** `internal/mcp/outbound.go` held the only in-process
credential pattern set and said so in a comment. With two more consumers it moved to `internal/redact`
— `Classes` for the flag-only form MCP uses, `Text` for the replacing form — because two copies of a
credential list is how the artifact a user hands to someone else comes to be filtered by the older one.
The table test moved with the patterns; what stayed in `internal/mcp` is the *boundary* behaviour
(opt-in per server, warns without blocking, names the class and never the match).

**SEC-08 — `internal/share` redacted nothing at all.** An export is the one artifact in this system
built to leave the machine and it carried the transcript verbatim: every tool result, every shell
command's output, anything a `cat .env` put in context. The pass runs over the *session* ahead of
rendering rather than inside each renderer — one filter instead of three, and the JSON format, which
marshals the session directly and is the one most likely to be fed to another program, was the easiest
of the three to forget. `Render` now returns the count as well as the bytes, both document formats state
it in their header, JSON carries it as an additive `redactions` key (an embedded pointer keeps every
existing consumer working), and both call sites print it — because a redaction pass that silently finds
nothing is indistinguishable from one that was never wired up, which is exactly the state this package
was in. Zero is reported explicitly.

One implementation hazard, pinned by a test: the assignment pattern can match across the quote and
colon of an object like `{"api_key":"..."}`, and `json.RawMessage` is marshalled verbatim — so a
redaction that ate a delimiter would fail the *whole* export with "json: error calling MarshalJSON"
rather than mangling one field. Invalid results are re-encoded as a JSON string: the arguments then
render as one redacted line instead of an object, which is a visible degradation of a call whose
arguments contained a credential, and the honest trade against shipping invalid JSON.

**SEC-11 — redact, then record.** `maxAuditInput` replaced any tool input over 1 KiB with
`"[N bytes, truncated]"`, so a `write_file` with a 2 KiB payload or any long shell pipeline was **not
recorded at all** — the trail lost exactly the calls that matter for reconstructing an incident, and the
stated reason ("avoid logging credentials embedded in long commands") argues for redaction, not for
discarding the record. Now: redact through `internal/redact`, keep the record, and keep a size bound at
16 KiB for genuine bulk data — which, when it bites, keeps the **head** with an explicit
`...[audit: input truncated to N of M bytes]` marker instead of substituting the length for the content.
A truncated command still names the command. The one limitation, recorded in the test rather than
engineered around: a head-keeping bound over JSON means a huge field ordered before the identifying one
crowds it out.

**GAP-01 — the turn trace.** `TurnTrace` carried tokens, cost, tool calls and wall time — enough to
answer "what did this cost" and nothing about *why the turn ended the way it did*. It now also carries
the provider's stop reason, a `Compaction` record (applied / summarized / suppressed, tokens freed,
messages before and after, and the estimate and trigger the decision was made on), a `Guard` verdict
(status as well as passed, so a fail-open skip is distinguishable — the FIND-16 distinction), the
`Correctives` the engine injected this turn, and a `RunID`. Every one of those was already computed and
discarded one line later.

Two structural notes. The final-answer branch now emits its trace at the **end** rather than the top,
because the two things worth recording about such a turn — which corrective it provoked and what the
guard made of it — are both decided below; each of the five exits appends its corrective and emits,
which is why the two are kept adjacent at every site. The tool-round path emits at the end of the round
for the same reason, through an idempotent closure, because that path has two exits and the
tool-failure abort must keep emitting the trace of the turn that ended the run. The OTel/Prometheus half
was skipped as the item directed. `aegis sessions trace` gained a `WHY` column so the record is readable
without a JSON export.

### P67.1 — a round-level bound over the per-call result caps

`internal/tool/builtin/truncate.go` carries the posture table for every tool result and **every cap in
it is per call**, written when a round was one result at a time. `Engine.runTools` dispatches up to
`maxParallelTools` concurrently — and that constant is **8**, larger than the item implied — so a round
of read tools could each land at its own 32 KiB cap and put 256 KiB (~65,000 estimated tokens) into a
single user message with nothing bounding the aggregate.

`builtin.CapRound` is a budget layered above the existing caps: 48 KiB per round, sized from the cap
table rather than picked (it admits the largest single inline cap plus a second substantial result
without spilling anything), selecting the **largest** results and spilling them through the existing
`SpillHead` path until the round fits, with a 2 KiB floor so a spilled result still shows what the call
returned. Both details the item asked to be pinned with a test are: notice bytes are reserved out of
each result's own budget by `truncate.go`'s existing rule, so they are counted; and a round of one huge
result and four small ones spills the one.

Three decisions worth stating. **The head survives**, even though the tools disagree about which end
matters — each result string already *begins* with whatever end its own posture kept, so a shell
result's tail is the head of the string handed over, and applying a posture here would undo theirs.
**Each round is evaluated independently**, so a large result this round and another next round are both
fine. **A round of one is exempt**: it is already bounded by its own cap, and the one thing that can
exceed the budget alone is an explicit `read_file` window the posture table deliberately honors
verbatim.

The engine reaches it through `Options.RoundResultCap`, a function seam wired at every engine
construction site, rather than by importing `internal/tool/builtin` — which would give the engine a
dependency on every builtin tool and a plausible future cycle (`engine` to `builtin` to `swarm` back to
`engine`, the moment sub-agents move in-process). The cap runs **after** every result is emitted: the
human has already seen the full output and the trace records what ran, so what this trims is only the
model's copy — trimming before emission would hide output from the user to save the model's context. A
hook returning the wrong number of results is ignored rather than trusted, since applying it would pair
a `tool_result` with the wrong `tool_use`.

### P66.21 — three doc corrections, and one that had already been deleted

**ARCH-13 contradicted the item.** CLAUDE.md's claim that write/execute tools serialize via
`sync.RWMutex` was not there to fix: `git log -S RWMutex` shows it was removed by the commit that cut
CLAUDE.md by 73%, which left the guarantee *undocumented* rather than wrong. Re-added accurately —
write/exec take one plain exclusive `sync.Mutex`, reads take no lock and are not held off by a
concurrent write (P8.6), and the only read-vs-write ordering is the same-`path` dependency graph, so a
`shell` call and a `read_file` are never ordered.

`buildChatSystem`'s doc comment claimed equivalence with the daemon's `effectiveSystem`; it now states
the four divergences verified by diffing the two — no deferred-tools block (so the model is never told
to reach deferred tools through `tool_search`), no debate-integration block, no local-profile caps on
context files or the repo map, and a repo map that comes only from the on-disk `aegis index` cache while
it is fresh. That list is the map P66.13's refactor needs, which is part of why P66.13 moved up the ten.

`internal/tui/view.go` still asserted the pre-P35.13 claim that Ollama's `prompt_eval_count` is a
cache-hit delta, and — worse than being stale — proposed a remediation that must not be applied. It now
states that the count is the full prompt every turn, that the meter is therefore accurate on a cache-hit
turn, and that the fix P35.10 proposed (feeding the bar an estimate instead) would replace a correct
number with an estimate.

### P66.12 — staticcheck, and the CI step that now gates

The 28 findings are cleared and **`continue-on-error: true` is deleted from the staticcheck step**,
which is this item's actual closure condition — clearing the backlog without making the step gate would
have let the next 28 accumulate the same way. Two of the findings were false positives and were
reworded rather than silenced: the deliberately side-effecting double `d.record` is now two explicit
evaluations (a rewrite that short-circuits away the second call would delete the point of the test), and
the doc comment beginning with an embed directive was rephrased so it is not parsed as one — in the one
file whose subject is embed patterns silently omitting files.

One thing learned is recorded beside the CI step: the untagged run only sees the default build, so a
symbol used solely by a build-tagged test (`live_eval`, `live_workflow`, `live_probe`) reads as U1000
dead. Annotate it with a lint-ignore directive; do not delete it.

**Previously, 2026-08-16 (second sitting) — the first five rows of the "Up next" ten as it then stood
shipped**: P66.5, P66.7's LLM-01 remainder, P66.16, P66.10 and P66.9, one commit each, every commit
independently `go build ./...` + `go test ./...` green rather than only the final tree. That empties
Tier 1. Their records are below, and three of them **correct the finding they were built from** —
VULN-03's suggested `::ffff:0:0/96` would have blocked the entire public internet, LLM-04 was
dropping *all* tool calls on a 1-based backend rather than trailing ones, and P66.7's 11,611-token
measurement was already stale. Two sub-items deliberately did not land and are now open on their own
terms: SEC-07 (content-bound trust grants) and PERF-02 (`synchronous=NORMAL`).

**The ten as it stood that morning** is retired below. [roadmap.md](roadmap.md) now carries a
rewritten ten — the first that ranks P66 and P67 against each other — so this table is kept here as
the record of what the tiers said before those five landed, not as a live list. Rows 6-10 were all
carried forward into the new ten; only their order changed.

| # | Item | Tier / size | Outcome |
|---|------|-------------|---------|
| 1 | **P66.5** — invert the config freeze list | T1 · M | **SHIPPED** `0352112`. All of Tier 1. `commands:` unfrozen meant an untrusted repo got arbitrary binary exec through `grep` — a `CapRead` tool, so plan mode allowed it silently. Inverted to a `configTrustPolicy` table defaulting to frozen; `security.*` landed as `frozenUntilTrusted`, not baseline-only, which would have broken `PatchProjectSecurity`'s six call sites. SEC-07 refiled as P66.25. |
| 2 | **P66.7** — cap context-file injection (LLM-01 remainder) | T2 · S | **SHIPPED** `9482c87`. The 11,611-token figure was stale — 10,257 bytes / 2,560 tokens at build time — so the 8,000-byte cap was derived from the served window instead. |
| 3 | **P66.16** — OpenAI adapter drops tool calls | T2 · S | **SHIPPED** `444516e`. Worse than filed: `Finish` iterating `0..len` over a map keyed by wire index emitted **zero** calls on a 1-based backend, not merely trailing ones. |
| 4 | **P66.10** — bounded security remainder | T2 · S-M | **SHIPPED** `fd4f49b`. SSRF list deduplicated into `internal/netblock`. VULN-03's suggested `::ffff:0:0/96` was rejected — `Contains` reduces it via `To4()` to `0.0.0.0/0`. |
| 5 | **P66.9** — bound `bg_events` | T2 · S | **SHIPPED** `d4fb209`. `DefaultBGEventRetention = 2000`, deliberately not a config key — the defect *was* a pruner gated on an unset one. PERF-02 refiled as P66.26. |
| 6 | **P66.21** — doc corrections | T2 · S | Open; #5 on the new ten. |
| 7 | **P66.14** — reconcile the compaction thresholds | T3 · M | Open; promoted to **#1** on the new ten — it gates the live-tier sitting. |
| 8 | **P66.12** — staticcheck cleanup | T2 · S | Open; #6 on the new ten. |
| 9 | **P66.11** — redaction + turn trace | T2 · M | Open; promoted to **#2** — `TurnTrace` is the instrument the live sitting reads. |
| 10 | **P66.22** — the live-tier run | Verification | Open; **#3**, and now framed as a five-item sitting rather than a single run. |

**Previously, 2026-08-16 — the P66 day plan is finished.** Written 2026-08-15 to be executed
2026-08-16, it was ordered by dependency and blast radius rather than by severity, and all four
blocks are done: P66.2 (toolchain, shipped 2026-08-15 — its own entry is below), then the two
Criticals P66.1 and P66.4, then P66.3's read-only tier, then the cheap high-value block of P66.6,
P66.7's LLM-16 half and P66.8. One item per commit, each with its test, `go test ./...` green before
each — the suite passing was never sufficient evidence for this batch, because four of these items
fixed defects that lived in a fully green tree. What follows is the record of what shipped **and of
what was found while shipping it**: several of these notes correct the roadmap item they were written
from, and those corrections are the durable part.

### P66.1 — gate `.aegis/.env` on workspace trust, and make the baseline honest

Shipped as `92f72be`. All four parts landed: trust is resolved before any project-controlled file is
read (so `.aegis/.env` is skipped entirely for an untrusted directory), `AEGIS_*` keys are dropped and
logged from `.env` even when trusted, the baseline layer is built over an `environSnapshot()` taken
before `loadDotEnv`, and `applyWorkspaceTrust` no longer forges `Trusted = true` from a missing
`config.yaml`. SEC-09 folded in: `unsandboxedAutoExecError` now covers `ModeAuto` as well as
`AutoApproveExec` under the same `allow_unsandboxed_auto_exec` opt-out.

Both named tests exist in `internal/config/dotenv_trust_test.go`, plus the non-`AEGIS_` loader-variable
half and a blast-radius guard that a genuine operator-set `AEGIS_*` still applies and does not read as
a project change. Every one was confirmed failing against the unfixed tree before the fix landed.
`TestWorkspaceTrustNoProjectConfigIsTrusted` asserted the behaviour this item reverses and was
rewritten as `TestWorkspaceTrustNoProjectConfigFreezesNothing`. No loader-variable denylist, per the
arbitration.

### P66.4 — give the shared tool table its own mutex, and clones their own overlay

Shipped as `46dde08`. `tools` moved into a `toolTable` carrying its own mutex (lock order:
`Registry.mu` before `toolTable.mu`); a clone's own `Register`/`Upsert` now writes a clone-local
overlay shadowing the shared table, which is the fix for the deterministic cross-session leak.
`subAgentToolRegistry` hands each spawn a clone of its parent session's registry — `SpawnConfig`
already carried `ParentSessionID`, so no new plumbing — and `debate.go:102` had the identical
one-line defect and was fixed with it. Lazy clone at `sessionToolRegistry` (ARCH-11). ARCH-08's
residual closed as a side effect exactly as predicted.

`TestConcurrentSkillActivationAcrossSessions` reproduced the reported race verbatim under `-race`
before the fix (two clones' `Upsert` on one map, from `activateSessionSkill`), and also fails on the
deterministic leak without `-race`. `TestCloneUpsertStaysLocal` pins the overlay contract in both
directions including clone-of-clone; `TestSubAgentToolSearchDoesNotWidenTheDaemon` guards ARCH-02 on
identity as well as effect.

### P66.24 — stop two MCP tests from hanging the whole suite

Found in passing while building P66.4, and fixed the same day. `internal/mcp`'s `TestSamplingHandler`
and `TestToolsChangedNotification` each started `go io.Copy(io.Discard, serverReader)` **and** a
`json.Decoder` on that same pipe. The two readers competed, and when the drain goroutine won the
initialize request the fake server never replied — `c.initialize(context.Background())` has no
deadline, so the package hung until the 10-minute test timeout killed the whole suite. Hit once during
the block and not reproducible in six isolated re-runs, which is the profile of a flake that fires on
a loaded CI box and gets dismissed as infrastructure.

The drain now starts *after* the initialize read (one reader on the pipe at a time), and an `initCtx`
helper bounds every handshake at 10s so a future regression is a named failure in seconds rather than
a suite-wide timeout. Verified by stress rather than by re-running: `-race -count=120` over the two
tests hangs to the 241s timeout on the old code, with `io.Copy` at `mcp_test.go:238` in the panic
stack, and finishes in 2.6s on the fixed code.

### P66.3 — one argv path-confinement path for the read-only tier

Shipped as `c1f1a8d`. Everything the two read-only argv paths must agree on now lives in
`internal/tool/builtin/argv_confine.go`: one union git-flag denylist (`deniedGitFlags`, including
`--no-index`), one attached-value-aware flag matcher, one path-candidate extractor, and
`validateReadOnlyGitArgv`, which both `gitTool.Execute` and `readOnlyGitCommand` now call on the
same argv. `git.go`'s `deniedGitArgPrefixes`/`validateGitArgs` and `shell_readonly.go`'s
`gitConfigOverrideFlags`/`shellArgsStayInRoot` are gone. The budget note's three spellings all
landed; the item did not overrun.

*Three deviations from the plan worth knowing.*

**`-p` came off the denylist rather than onto it.** The union of the two lists would have denied it,
but `-p` is the pager alias — an external program — only in the *pre-subcommand* position, and
neither call path can reach that position: the git tool takes the subcommand as its own field and
prepends it, and the shell classifier requires the first token after `git` to be an allowlisted
subcommand. Post-subcommand `-p` is `--patch` and is read-only, so denying it (as the shell path did)
cost `git log -p` for nothing. `--paginate` stays denied — it has no post-subcommand meaning to lose.

**Three more argv0 drops than the plan named, and they close VULN-02 at its root.** Beyond `ps`,
`less` and `more` (SEC-04), `sort`, `tree` and `uniq` came off `readOnlyShellArgv0` as well: each has
a documented file-*writing* form (`sort -o FILE`, `tree -o FILE`, `uniq INPUT OUTPUT`), so no
argument parsing makes them read-only. Confinement stops those forms escaping the workspace, but a
write *inside* the workspace is still a write and plan mode allows `CapRead` silently. The review's
own VULN-02 fix section reached the same conclusion for `sort`; `tree` and `uniq` are the same
criterion applied consistently, which is the whole argument of this item. A regression case pins that
this did not cost `grep -o` (`--only-matching`), the one allowlisted `-o` that is a read.

**The separated `-o <path>` spelling needed no case of its own** — its value is a bare operand, and
operand confinement was already being added. The helper handles `--flag=value` and `-ovalue`; the
third spelling falls out.

*Verified against the unfixed tree, not just green afterwards.* A worktree at `184497d` accepted all
eight escapes: the six shell classifications (`git diff --output=`, `sort --output=`, `sort -o`
attached, `ps auxwwe`, `less`, `more`) all returned `CapRead`, and the git tool ran both
`--no-index` and the escaping pathspec without a refusal. VULN-01 reproduced verbatim on Windows —
`git diff --no-index -- NUL <abs path>` through the `CapRead` git tool returned the full contents of
a file outside the workspace.

The deliverable is `TestReadOnlyGitArgvAgreesAcrossBothPaths` (`argv_confine_test.go`), a table of 19
argvs asserting the two paths reach the same verdict, with the shell string *derived* from the argv
so equivalence is guaranteed by construction rather than by proofreading.
`TestReadOnlyTierRefusesEscapesInPlanMode` states the property in plan-mode terms and records the one
real asymmetry between the paths: the shell tool refuses by declining the `CapRead` downgrade, while
the git tool is statically `CapRead` and is always reached, so it must refuse inside `Execute`.

Closed VULN-01 (+SEC-05), VULN-02, VULN-11, SEC-04, SEC-10.

### P66.6 — sanitize the approval dialog at ingestion

Shipped as `f72e116`. Sanitized at ingestion (`stream.go`'s `KindApprovalRequest`), with
`StripControlSeqs` rather than `StripDangerousSeqs` — the dialog applies its own lipgloss styling
*after* ingestion, verified by reading the render path, so model-supplied SGR can only fight the
TUI's own colours.

*Two things the item's own description would have missed.* The suggested **"allow always" rule
pattern** carried the escape too, so even the one covered path (`shell`, patched under P28.1) leaked —
via `suggestRulePattern`, which `renderShellCall`'s stripping never saw. And a single strip over
`string(ev.ToolInput)` is **not sufficient**: a real provider delivers the payload as the
six-character JSON escape for ESC, which is plain ASCII on the wire and only becomes a control byte
when `renderWriteDiff` unmarshals `content`. Raw ESC bytes are the *other* shape, and they make the
JSON unparseable — which drops the preview into `renderApprovalPreview`'s generic excerpt branch that
prints the bytes verbatim. `sanitizeToolInputJSON` does both passes.

Checked before shipping: `approvalState.input` is render-only (the approval response carries just the
id), so sanitizing cannot alter the call that actually runs.

The closure condition needed one honest amendment. A literal `ContainsRune(out, 0x1b) == false` can
never pass, because the dialog's own chrome *is* ESC bytes — lipgloss emits truecolor SGR for the
frame and option list even under `NO_COLOR`. `TestApprovalDialogStripsControlSequencesFromToolInput`
removes SGR only (`\x1b\[[0-9;]*m`, the sole form the TUI emits) and asserts no ESC survives that, so
anything left is by construction an escape the event smuggled in. Eight carriers across both render
paths, all eight confirmed failing against the unfixed tree.

### P66.7, LLM-16 half — say so when the fixed prompt crowds the window

Shipped as `5ed832d`. One `KindNotice` at run construction when `tokenest(system) + requestOverhead`
crosses `oversizedSystemPercent` of the served window, naming both numbers and taking its remedy
clause verbatim from `ollamainfo.Result.Describe()` so `/status` and this read as one voice. Silent
when the window is unknown — that is "not known yet", not "tiny".

**The threshold is 50%, not the review's ~60%,** and the disagreement was resolved rather than split:
`compactionTrigger` is floored at `window/2`, so `window/2` is the lowest estimate at which proactive
compaction can fire at all. A fixed prompt at that point puts every turn over the trigger from its
first message — the state actually worth naming — and 60% leaves a band of runs sitting in it
unwarned. `TestOversizedSystemPromptThresholdMutation` hardcodes 49%/51% so it discriminates: the
constant at 60 fails the "just above" case and at 40 fails the "just below" case (both run).

Nothing about prompt *content* changed. The `localContextFilesMaxBytes` cap and the realistic-`CLAUDE.md`
budget fixture (LLM-01) stay open under P66.7.

### P66.8 — put the stall bound above the timeouts it backstops

Shipped as `35e8f95`, and it was two defects, not one. The timeouts were the reported half; the beat
could not have arrived anyway, because `withStallBeat` was a bare `context.WithValue` and a
sub-agent's engine installed its watch over the same key.

`internal/heartbeat` (new) carries the beat chain. It is a **leaf package** because the three parties
sit on opposite sides of the import graph — `internal/tool` already imports `internal/provider`, so no
home inside any of the three is reachable from the other two. `agent.go` now bounds each *individual*
wait at `maxAgentDuration` and beats on every completion (per teammate, per debate role); the
aggregate batch/debate contexts stay as the outer cap and are admissible **precisely because** they
decompose into sub-900s waits with observable activity between them. The per-wait bound is what fixes
sequential and loop mode, where one teammate could previously spend the whole batch budget on a single
silent wait. `admissionAdapter` beats every 30s while queued — the one wait in the codebase *known* to
be alive while producing nothing, which is what licenses a blind ticker there and nowhere else.

The docs were corrected rather than deleted: the true relation is "above every **per-call** bound",
and an aggregate above 900s is admissible only if it decomposes. That sentence now appears in
`config.go`, `docs/configuration.md` and CLAUDE.md, which also closes P66.21's first bullet.

`TestToolTimeoutsStayUnderTheStallBound` mirrors `TestResultCapsCanBindBeforeTheContextWindow`, and
its **grep-the-source half** counts the `context.WithTimeout` sites in the package and requires the
tables to name all 13 — so a new timeout cannot be added without a decision, which is exactly how the
two agent bounds drifted 40 and 80 minutes above a limit the docs claimed they were under. Mutation
checks run, not asserted: the pre-P66.8 per-teammate wait fails at 40m0s; `stallBound` at 5 minutes
fails all six 10-minute entries and neither latex entry.
`TestChildStallWatchDoesNotHideItsParent` pins the chain in both directions and reproduces the
shadowing verbatim against a reverted `withStallBeat`. A follow-up commit (`c0c2196`) covers
`internal/heartbeat` directly, so the new leaf package is tested on its own terms rather than only
through its two callers.

### P66.5 — invert the config freeze list

Shipped as `0352112`. `securityRelevantDiff` was an enumerated denylist found incomplete four times
(P42.1, P46.2, P52.13, P66); six independent discoveries of one structural problem is the argument
for inverting rather than extending a fifth time. `internal/config/freeze.go` replaces the eight
hand-written `if` blocks with a `configTrustPolicy` table classifying each path as
`projectSettable` / `frozenUntilTrusted` / `baselineOnly`. `policyFor` walks a dotted path to its
nearest classified ancestor and **defaults to frozen**, so fail-closed no longer depends on the
table being complete — which is the property the old shape lacked. One reflection walk drives both
the diff and the freeze, so the two can no longer disagree; that divergence *was* the old code's
defect, repeated three times.

Newly frozen: `commands`, `server`, `security`, `provider`, `lsp`, `mcp_server`, `search`,
`embeddings`, `diagram`, `cleanup`. `provider` is refined by dotted entries so its 18 model/tuning
knobs stay settable — the ordinary "this repo wants qwen3:14b" case must not need a trust prompt, and
narrowing the frozen default is the only thing dotted paths are used for. `data_dir` and
`security.dast.allowed_targets` are `baselineOnly`, applied *after* the freeze so an untrusted
attempt still shows up in `WorkspaceTrust.Changes`.

**One deviation from the item, and it is a correction to it.** The roadmap asked for `security.*` to
be baseline-only on the `Security.DAST.AllowedTargets` precedent. That would have silently broken
`PatchProjectSecurity`, which has six production call sites (`aegis harden --project`,
`/security-config`, `PATCH /config/security`, image-pin writes from `security build-image`). It is
`frozenUntilTrusted` instead, with `allowed_targets` keeping the P27.9 baseline-only treatment.
SEC-03's substance — an untrusted repo disabling `egress_then_write` or the network allowlist — is
closed either way. `PatchProjectSecurity` now records trust for cwd, following `PatchProjectSandbox`.

`TestEveryConfigFieldDeclaresATrustPolicy` reflects over `Config`'s koanf-tagged fields and fails on
any that declares no policy, checking the reverse direction too, with the same "the scan is still
finding things" floor as the callsite test it is modelled on. Reflection over the type rather than a
source grep: same guarantee, resolves dotted paths, cannot be fooled by formatting. Verified by
deleting the `log_level` entry. `TestUnclassifiedConfigKeyIsFrozen` pins the fail-closed default
separately. `rejectRelativeCommandOverrides` drops project-introduced relative-path `commands:`
values even after `aegis trust`.

SEC-02, SEC-03, SEC-06 closed. **SEC-07 did not land** and is now its own Tier 2 item: a
content-bound trust grant has to gate the `.aegis/.env` load, which P66.1 deliberately resolves
*before* any project-controlled file is read, so honouring it means either inverting P66.1's ordering
or accepting a re-prompt that covers `config.yaml` but not `.env`. The well-defined subset this item
builds was its prerequisite; the rest is not cheap.

### P66.7, LLM-01 half — cap context-file injection, and let the budget test see it

Shipped as `9482c87`, closing P66.7. `localContextFilesMaxBytes = 8000`, sited beside
`localRepoMapMaxBytes` and applied only under the local profile.

**The cap is derived, not measured off this repository, and the roadmap's number was stale.**
`LoadContext()` measures 10,257 bytes / 2,560 tokens here today, not the recorded 11,611 tokens —
sizing against that figure would have been sizing against a moving number, which is the failure mode
the item itself warned about. The derivation, written into the comment: a 32,768 served window under
the documented local config, a target of a quarter of it for the always-injected prefix so the LLM-16
notice reports *transcript* growth rather than being tripped by the prefix alone, minus the
4,550-token base ceiling and the ~1,000-byte repo map. 8,000 bytes is 2× the repo-map cap, on the
stated ordering that hand-written project instructions are worth more per byte than a generated one.
The comment also records what the cap does *not* do: nothing rescues Ollama's default 4,096 window,
since the base prompt alone exceeds it.

**Posture is deliberately asymmetric with the repo map.** An over-cap repo map is dropped whole —
generated, ranked, degrades to nothing. Context files are the project's instructions, so they
truncate head-first at a line boundary with `truncate.go`'s exact notice wording, notice bytes
reserved *out of* the limit, and a file that arrives with nothing left gets an `[omitted: …]` notice
rather than vanishing. `builtin.TruncateHead` could not be called — `internal/tool/builtin` already
imports `internal/memory` — so the wording is duplicated with a comment naming P64.3 and the reason.
Truncation runs *before* `wrapContextFile` so the `trust.Wrap` provenance envelope is never cut
through. The 5s sources cache is keyed on the budget, so the daemon (capped) and `aegis chat`
(uncapped) cannot be served each other's size.

`TestEffectiveSystem_localProfileBudget` is no longer structurally blind: two subtests over a fixture
carrying a realistic ~12,000-byte `CLAUDE.md` — bare workspace 4,336/4,550, with context files
6,383/6,650 against a new `localInjectedPromptCeilingTokens`. The blindness check is the real
assertion: setting the cap to 800,000 fails both new cases, so **cap removal is now caught**, which
was the point of the item.

### P66.16 — the OpenAI adapter was dropping tool calls

Shipped as `444516e`, and **LLM-04 is worse than the finding recorded.** `chunkDecoder.tools` is
keyed by the wire's `tc.Index`, but `Finish` walked a synthetic `0..len(tools)` range, correct only
when the indices are exactly `{0..n-1}`. The review described dropped *tails*; in fact a lone call at
index 1 gives `len == 1`, reads `tools[0]`, finds nil and continues — so a 1-based backend emits
**zero** tool calls under a `finish_reason: "tool_calls"` stop, having already fired
`EventToolUseStart`. This is on the adapter `docs/providers.md` recommends for local Ollama.
`Finish` now sorts the map's actual keys, which covers 1-based, gapped and out-of-order indices and
makes emission order deterministic wire-index order rather than map order.

LLM-05: `toolAccum.callID(index)` returns the wire ID when present and `tu_<index>` otherwise,
mirroring the native Ollama adapter. Keyed on the **wire index**, so the start event and `Finish`
compute the same value without coordinating, and a synthesized ID cannot collide with a real one or
with another call in the turn.

`TestStreamToolUseStartAnnouncedOnceWhenIDArrivesLate` (the P33.3 split-ID stream) asserted the start
event carried `ID: ""` and now asserts `tu_0`. That is the correct direction: the start fires before
the ID is known so it gets the synthesized one, while the assembled call keeps the late-arriving real
ID, which is what the wire requires. The TUI already handles it — `rekeyPendingTool` exists precisely
to re-key a card appended under a synthetic start key once the real ID arrives.
`TestStreamToolCallIndexingAndIDSynthesis` covers all six cases and every subtest fails against the
unfixed adapter.

### P66.10 — the bounded security remainder

Shipped as `fd4f49b`. Three fixes.

**ARCH-03** — the output guard read files back with a context lacking `WithWorkdir`/`WithExtraRoots`,
attached only in `executeTool`, so on a custom workdir it silently validated nothing. Extracted
`e.toolCtx(ctx)`, used by both `executeTool` and `collectWrittenFiles`.
`TestGuardReadsBackFilesFromSessionWorkdir` runs the registry at a daemon temp dir and the engine at a
different session one, and fails without the fix with `guard saw 0 file(s), want 1`.

**VULN-03** — the blocklist was duplicated byte-for-byte in `internal/tool/builtin/web.go` and
`internal/mcp/http.go`. Both copies are gone in favour of a new dependency-free leaf package,
`internal/netblock`, holding the CIDR table plus `IsPrivate`, `SafeDialer` and `ValidateNotPrivate` —
a new package rather than either existing file, because a cross-package import of
`internal/tool/builtin` from `internal/mcp` is exactly what the old comment argued against. Added
`IsUnspecified()`, `0.0.0.0/8`, `100.64.0.0/10`, `192.0.0.0/24`, `198.18.0.0/15` and
`IsInterfaceLocalMulticast()`.

**This one corrects the finding.** VULN-03's suggested remediation names `::ffff:0:0/96`, and adding
it would have been a severe outage: `net.IPNet.Contains` reduces that CIDR via `To4()` to
`0.0.0.0/0`, matching **every IPv4 address** — the entire public internet blocked. IPv4-mapped
addresses were already handled correctly (`::ffff:127.0.0.1` is blocked). Recorded in the package
comment and pinned by `TestIsPrivateLeavesPublicAddressesAlone`, which is the over-blocking guard
this list did not previously have. Any future reading of VULN-03 should start here.

**VULN-05** — `LocalBackend.Exec` buffered unbounded, so the 24 KiB shell cap applied only after a
10-minute `cat /dev/urandom` was already in the daemon heap. `cmd.Run` with a `capWriter` on both
streams, `maxCapturedOutput = 4 MiB` — well above the largest entry in `truncate.go`'s posture table
(32 KiB git), so no realistic result is altered and downstream truncation is unchanged.
`capWriter.Write` always returns `len(p)` so a short count cannot SIGPIPE the child. The cap keeps
the **head** — keeping the tail of an unbounded stream needs either the defect or a ring buffer — and
appends its notice last so it survives the shell tool's `TruncateTail`.

