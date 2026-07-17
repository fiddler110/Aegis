# Aegis Capability Roadmap

**Last updated:** 2026-07-17

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 11 — one Tier 2 item (**P34.12**), four Tier 3 items (**P33.10, P33.11, P33.16,
P33.19**), and six parked (**P25.9, P33.12, P33.20-P33.22, P34.11**). Tier 1 is fully clear.

**P34.9 and P34.10 shipped 2026-07-17**, clearing Tier 2 apart from **P34.12**, which this batch
filed on its way out — osv-scanner reports `error: exit status 128` on any tree with no dependency
lockfile, which is the P34.6 "accurate refusal that reads as a broken tool" shape a third time.
Filed with a verified mechanism (reproduced with Aegis's exact args, before and after `git init`,
on both an absent and an empty lockfile) rather than a plausible one — the first guess, that 128
meant a git failure, was wrong. See
[releases.md](releases.md#latest-changes). **P34.9 makes it four consecutive items whose diagnosis
was wrong, and it's the sharpest case yet: the fix the item specified was the one that could not
work.** The item blamed njsscan's Windows traceback on semgrep being unsupported there and offered
"gate on semgrep's own availability" as a candidate. Semgrep runs fine on Windows; njsscan's engine
(libsast) just hardcodes `if platform.system() == 'Windows': return None` and then crashes on its
own stub — it never asks whether semgrep is there, so that gate would have passed and reproduced
the identical crash. The mechanism took one `grep` of the installed package to find.

**What's new here is a second cost of a wrong diagnosis, beyond the fix failing: the false
mechanism implicated an innocent component.** "Semgrep isn't supported on Windows" is a claim about
a tool Aegis ships its own scanner for — acting on it would have gated a working semgrep too. A
wrong mechanism doesn't just fail to fix its own item; it invites collateral changes to whatever
it names. The [P34.5-P34.8 lesson](#status) said re-verify a handed-down mechanism before building
on it; P34.9 adds that the re-verification is cheapest at the boundary the mechanism names
(here: run semgrep once), and that this is worth doing *even when the fix seems obvious*.

**P34.10 is the counter-example worth keeping in view: its numbers were exactly right** (1 of 140
packages, and zero cost today because those packages have no CVEs). Not every item has decayed —
which is the point of re-verifying rather than assuming either way. Its *trade*, though, inverted
under measurement: the "dev-dep CVEs are lower severity, so trivy's default spares the user noise"
argument dissolves once you notice osv-scanner already includes dev deps, so the findings arrive
regardless. The default bought no quiet, only scanner disagreement about scope. **When an item
frames something as a judgement call, check whether the rest of the system has already made it.**

**P34.5-P34.8 shipped 2026-07-17** — the whole of the Tier 2 batch, four parallel sub-agents in
isolated worktrees, merged one at a time. See [releases.md](releases.md#latest-changes).
**Three of the four items were wrong about something material, and each sub-agent caught it by
going to the real system rather than implementing the spec as written.** P34.7's item named
`DetectBest` as the host dependency to seam — that function is never reached on the podman path,
so the specified fix would have left the test exactly as broken. P34.8's item asked why trivy
found 3 where grype found 47, and both halves of the premise dissolved: grype's extras are
gitignored `.exe` build artifacts, and the "1 finding across 140 npm packages" conflated two
ecosystems (it was a Go finding; osv scanned all 140 npm packages and correctly found zero).
P34.6's item under-stated its own blast radius — auto-detection, not operator opt-in, was
enabling brakeman on every Ruby repo.

**The lesson is narrower than "specs are sometimes wrong": all three errors were in the item's
*diagnosis*, not its symptom.** The observed symptoms were all real and all correctly reported.
What decayed between filing and implementation was the explanation attached to them — a plausible
mechanism recorded once, at a moment when checking it was expensive, and thereafter read as
established fact. P34.8 is the sharpest case: the item's own hypothesis section was honest that
"they scan different things" was a hypothesis, and it was still wrong. When an item hands the
implementer a mechanism, that mechanism is the first thing to re-verify, not the thing to build on.

**Two threat-model findings closed 2026-07-16 — both had shipped for only half their surface.**
**FIND-14**'s fair-share budget floor existed on the subprocess backend only, so every *in-process*
teammate still checked one shared tracker against the daemon's full cap and one expensive sibling
could starve the rest; the floor now travels on the context (`WithBudgetOverride`) since an
in-process teammate has no `WorkerSpec` to carry it. **FIND-17**'s TUI sanitization covered the
model's answer text but not its *thinking* text, which renders through lipgloss rather than
`mdRender` — so an embedded OSC/ANSI sequence still reached the terminal, on live turns and on
replayed history alike. See [releases.md](releases.md#latest-changes).

**Both are the same shape, and it's a new one for this roadmap: a fix scoped to one code path reads
as done in the changelog.** These weren't wrong diagnoses (the P33/P34 pattern) — they were correct
fixes with an unexamined blast radius, closed without asking which *other* paths the finding covered.
The other backend and the other render channel were never named, so nothing flagged them as open.
Worth asking of any finding marked closed: which surfaces does it actually name, and which does it
merely happen to cover?

**P34.2's live warning was firing on a capable model, found and fixed 2026-07-16** while
live-verifying P34.3 — the daemon warned that `qwen3:14b` "likely can't use tools" in the same run
where it made real tool calls. Two defects stacked: the probe's 256-token cap truncated the model
mid-reasoning (it needs 124-825 completion tokens on that prompt — 3 of 5 runs cut off), and the
**OpenAI adapter never mapped `finish_reason: "length"`**, so truncation arrived as a clean
`StopEndTurn` and was indistinguishable from a model that chose not to call. Fixed at both levels:
truncation is now a non-verdict (`Unknown`, uncached) rather than an accusation, and the cap is
2048. See [releases.md](releases.md#latest-changes).

**That is the third consecutive item whose defect was invisible to a green suite**, and the first
where the *fix itself* shipped the bug — P34.2's own "measure before deferring" lesson was applied
to the probe's cost and not to its token budget. `internal/toolcallprobe` had no tests at all; it
now has them, plus a `live_probe` tier, because the scripted tests can only assert what the code
does with a given stream, never whether the cap fits how a reasoning model actually thinks.

**P34.3 shipped 2026-07-16** — persona activation preloads the deferred tools a persona declares.
Live A/B against `qwen3:14b`, the model that produced the original observation. See
[releases.md](releases.md#latest-changes).

**The live A/B found the item's own diagnosis understated the bug, which is the P34.2 verification
lesson repeating.** The item says the model "tried `security_scan` twice before being told to call
`tool_search`" — an inefficiency. The recorded baseline (fix stashed, same model/prompt/mode) is
worse: qwen3:14b reasons *correctly and by name* ("I should start by calling the recon_scan function
with the target 127.0.0.1"), then emits **zero tool calls and zero text** — the turn dead-ends into
P34.1's empty-answer nudge and an empty reply. A persona promising a tool the schema list doesn't
carry doesn't just misroute the model; it can strand it entirely. Worth remembering as a shape: the
observation a roadmap item is filed from is one sample of the failure, not its bound.

**P34.2 shipped 2026-07-16 — both levers**, live-verified against the `qwen2.5-coder:1.5b` model that
produced the original observation and against a capable 24b model for false positives. See
[releases.md](releases.md#latest-changes).

**The lesson worth carrying forward is about verification, not the item.** Every stage of this item
was green before it was right, and only live runs said otherwise — four separate times:
**(1)** the unit tests passed and the TUI rendered correctly, yet the first live `aegis chat
--output-format stream-json` run exposed two pre-existing bugs the tests could not see (notices
serialized without their text, and a trailer reporting `"tool_calls":0` unconditionally). **(2)** A
third instance of that same defect class — notices dropped entirely in chat's *default* text format —
was then found only by going looking for it. **(3)** Lever (1) was wired, built, and unit-green, and
still did nothing on the surface it was tested on: `aegis chat` builds its own in-process engine and
never touches the daemon's run path, which no test asserted. **(4)** Driving a real daemon showed the
warning was correct but *nagging* — it repeated on every turn, including `what is 2+2?` — which no
test would have called a failure. The rule this suggests for future items: a green suite says the code
does what the test says, never that the feature reached a user. Drive the real binary before shipping.

**The item's own cost framing was wrong, and worth remembering as a shape:** it deferred lever (1) on
"probe cost", which on inspection didn't exist — the probe only ever runs against local Ollama, and
at run start it shares a cold load the turn was about to pay anyway. A cost objection stated in the
abstract survived three drafts of this roadmap; one measurement dissolved it. Measure before deferring.

**P34.1 shipped 2026-07-16** — the empty-answer nudge. See
[releases.md](releases.md#latest-changes). Its stated mechanism was marked unverified; it was
re-derived with a failing test before any fix was written, and **the diagnosis held** (the
`len(toolUses) == 0` branch emits `KindDone` without ever checking that text was produced, and
the output guard's `if final != ""` means an empty answer skips validation entirely rather than
tripping it). That's the first item in two batches whose written mechanism survived verification —
the cheap failing-test proof is what established that, and is worth keeping as the default.

**P34.2 added 2026-07-16** from a live 3-model eval pass (`gpt-oss:20b`, `qwen3:14b`,
`qwen2.5-coder:1.5b` — 3 use cases each via `aegis chat` through the P33.9 native adapter):
nothing warns when a configured model can't actually emit structured tool calls.

**P33.9 shipped 2026-07-16** — the native Ollama provider adapter, Tier 3's keystone. See
[releases.md](releases.md#latest-changes) for the full writeup. P33.10 and P33.19 are now
unblocked; P33.16 has the real error taxonomy it was waiting on to decide its retry-classification
question.

**P33.13, P33.14, P33.15, P33.17, P33.18 shipped 2026-07-16** — the whole of the Tier 2 batch,
implemented by five parallel sub-agents, each in its own isolated git worktree (`isolation:
"worktree"`), then merged into `main` one at a time. All five auto-merged with zero conflicts
despite four of the five touching `internal/tui/tui.go`, because worktree isolation kept the
concurrent edits from ever colliding on disk — conflict avoidance came from git's merge at
integration time rather than from hand-grouping items by file the way the P33.1-P33.8 batch did.
Full `go build ./...` / `go vet ./...` / `go test ./...` green after every merge. See
[releases.md](releases.md#latest-changes) for the full writeup.

**P33.1-P33.8 shipped 2026-07-15** — the whole of the batch's Tier 1 and Tier 2, implemented by
parallel sub-agents grouped so no two concurrently edited the same file. See
[releases.md](releases.md#latest-changes) for the full writeup. The P33 assessment's cross-cutting
insight held up in implementation and still scopes the remaining items: **the renderer is not the
bottleneck** — the transcript virtualization, wrap caching, event batch-draining, and
boundary-cached live glamour styling are sound and benchmarked; the responsiveness gaps live at
seams where the model is doing real work and the UI has nothing truthful to say about it.

**The most important lesson from the P33 batch, recorded because it should change how the next
assessment is written:** four of the eight items had materially inaccurate descriptions, and in
three cases implementing the item exactly as written would have shipped a non-fix or a visibly
wrong UI. P33.1's stated root cause was wrong — the Ollama error envelope decoded *successfully*
and was dropped one step earlier than the `json.Unmarshal` path the item blamed, so fixing only the
documented path would have fixed nothing while looking done. P33.4's phase-end condition missed the
tool-call-first case, and its named tok/s data source is reset every tool round. P33.7's picker
inventory named two pickers that aren't remote-backed and one that doesn't exist, while omitting
the one that does. P33.3's proposed `Index` wire field was unnecessary. All four items cited exact
line numbers and were right about *where* the code was — **the line-number precision was not
evidence the diagnosis of *mechanism* was right.** Future assessments should either verify the
mechanism (a failing test against current code is the cheap proof) or explicitly mark the diagnosis
as unverified so the implementer re-derives it rather than trusting it.

A second, milder pattern from the same batch: **shipping a truthful UI repeatedly required
deviating from the spec.** P33.4 alone needed three deviations (phase ends at *any* first model
output, not just text; `outBytes` not `liveText`; tok/s measured from first token, not from send —
otherwise a 60s cold-load is averaged into a throughput the model never ran at). Where an item says
"show X", the implementer owns the question of whether X is substantiable.

**Next session:** **P33.10** is the best next win — with P34.9 and P34.10 shipped, the only Tier 2
item left is **P34.12**, which is small and self-contained enough to pick up alongside rather than
instead of it. **P33.10** has a head start from P34.2: `internal/toolcallprobe` established that a probe at run start is nearly free
because it shares the load the turn was going to pay, which is the same insight pre-warm turns on, and
`toolcallprobe.Gate` is a working model of the per-model caching P33.10 needs. With P33.9 shipped, **P33.10
(keep-alive management/pre-warm)** and **P33.19
(name the post-tool-round wait)** are both unblocked and can now use real `load_duration`/
`prompt_eval_count` data instead of guessing. **P33.16** (mid-stream error retry classification)
also has what it was waiting on: a real per-class error taxonomy from the native adapter's error
path, distinct from the OpenAI-compat one. Decide P33.16's policy before writing code. P33.11
should read P33.20 first — starting it is P33.20's stated activation trigger. P33.9's native adapter
has since been validated against a live Ollama server (0.30.10, `gpt-oss:20b` and `qwen3:14b`) — see
[releases.md](releases.md#latest-changes): the adapter itself held up across 10 runs (real usage
every time, correct tool-call translation), and the handful of failures were model-side reliability
variance (malformed tool-call output, a bad edit), not adapter defects. Re-run `TestLiveWorkflow`
after any change touching the engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` is
the standalone
preflight companion for the same misconfiguration classes.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.  
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.  
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).  
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** None open. (P33.1 and P33.2 shipped 2026-07-15; P31.1, P31.2, P30.1-P30.3 shipped 2026-07-14; P32.1-P32.4 shipped 2026-07-15.)

---

## Open Work — Tier 2

One item, filed 2026-07-17 by the P34.9/P34.10 batch on its way out — the same way that batch's
own two items were filed, and the same family as both (a scanner whose accurate refusal reaches
the report as a broken tool). Dependency-free. (P34.9 and P34.10 shipped 2026-07-17 — see
[releases.md](releases.md#latest-changes); P34.5-P34.8 shipped 2026-07-17; P34.3 shipped
2026-07-16; P34.2 shipped 2026-07-16, both levers; P34.1 shipped 2026-07-16; P34.4 shipped
2026-07-16; P33.13, P33.14, P33.15, P33.17, P33.18 shipped 2026-07-16; P33.3-P33.8 shipped
2026-07-15; P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14; P32.5-P32.7 shipped 2026-07-15.)

### P34.12 — osv-scanner reports "error: exit status 128" on any tree with no dependency lockfile

Effort: S

Found 2026-07-17 while live-verifying P34.9 — a scratch JS project (package.json + app.js, no
lockfile) produced `osv-scanner: error: exit status 128` in an otherwise clean scan report.

**The mechanism is verified, not inferred** (osv-scanner 2.4.0, the version pinned in the
multiscanner Containerfile and the one on this host — so this isn't version skew):

- With Aegis's exact args (`--format json --call-analysis=all --recursive .`), osv-scanner exits
  **128** and writes **zero bytes to stdout**, with `No package sources found, --help for usage
  information.` on stderr. 128 is osv-scanner's documented "no package sources" code.
- It does this both for a tree with no lockfile at all *and* for a lockfile whose `packages` has
  only the root entry. Add one real package and it's exit 0 with JSON on stdout.
- It is **not** git-related, which is the obvious wrong guess (128 is also git's error code):
  reproduced identically before and after `git init`.
- `runJSON` tolerates a non-zero exit "as long as output was produced". stdout is empty here, so
  the tolerance doesn't apply and the exit code becomes an error row.

So this is the **P34.6 shape exactly**: brakeman's "Please supply the path to a Rails application"
was an accurate refusal that read as a broken tool, and this is the same — "there are no
dependencies here to scan" is correct, useful, and currently rendered as a failure. It hits any
tree with no dependency manifest: a C/C++ repo, a shell/docs repo, a bare scratch directory.

The decision is where to put it, and unlike P34.6 the obvious answer is probably wrong.
A `RelevanceChecker` mirrors brakeman's fix but would have to enumerate every manifest type
osv-scanner supports across every ecosystem it covers — a list that will drift out of date
silently, and the failure mode of drift is a **skipped SCA scan on a repo that had dependencies
all along**, which is worse than the error row it replaces. Prefer teaching the osv-scanner Scan
branch that exit 128 with empty stdout means zero findings, since that's osv-scanner's own
documented contract and needs no list. Worth checking whether the container path agrees (same
binary and version, so it should — but that's the assumption, not a measurement).

Worth a look while there: the same "accurate refusal, error-shaped" question for the other
scanners' documented exit codes. P34.6 checked the *language*-targeted tools; nothing has swept
the SCA/secrets tools for non-zero exits that mean "nothing to do" rather than "I broke".

---

## Open Work — Tier 3

Four items: P33.10, P33.11, P33.16, P33.19. Medium-effort with real value; some are
sequence-dependent. (P32.8 shipped 2026-07-15; P33.9, the keystone unblocking P33.10 and P33.19,
shipped 2026-07-16 — see [releases.md](releases.md#latest-changes).)

### P33.10 — Ollama keep-alive management and pre-warm

Effort: M — unblocked by P33.9

Ollama unloads models after its default 5-minute `keep_alive`; the next message then pays a full
cold reload (tens of seconds on 16GB VRAM). P33.4 shipped a phase split that makes this wait
*visible* ("waiting for first token · Ns"); P33.9 shipped the ability to *name* it as a cold load
(`Usage.LoadDurationMS`, surfaced as a `KindNotice` when ≥1s) — that half is done. What's left is
the pre-warm half. Two levers: **(1)** a config knob (e.g. `provider.ollama.keep_alive`) passed on
chat requests — the native adapter already accepts this (`ollama.WithKeepAlive`, unwired), it just
needs a `ProviderConfig` field and `providerfactory.buildOne` to pass it through. **(2)** Independent
lever: a native-API warm ping (in-repo precedent: `internal/cli/ollama.go:17-29` already POSTs
`keep_alive: 0` to force-unload — the inverse operation) triggered when the TUI regains focus
(`tea.FocusMsg` is already tracked) or on first keystroke of a new message, gated on `/api/ps` (via
`ollamainfo`) reporting the model unloaded. Never default `keep_alive: -1` (pin-forever) — the
machine profile this targets has 16GB system RAM and other workloads; make persistence an explicit
user choice.

### P33.11 — Transient panels for informational slash commands

Effort: M

`/status`, `/models`, `/help`, `/config`, `/memory` and friends print static blocks into the
transcript (`internal/tui/slash.go`), so after a session of housekeeping the conversation is
interleaved with stale panels — a real contributor to the "disjointed" feel, and Claude Code
renders these ephemerally instead. Plan: render informational command output in a dismissable
overlay panel (reuse `dialogFrame`/`renderOverlay` from dialog.go) that never enters transcript
history; action commands' confirmations (e.g. "✓ Configuration saved") stay in-transcript. Add a
`transient` flag per command in `commandDefs` (`internal/tui/commands.go`) so the existing
single-source-of-truth table (P14.10 convention) decides, not scattered call sites. Needs a small
scrolling story for tall output (`/help`, `/models`) — the pane already has the primitives.

Note: P33.6 and P33.7 have since built out the overlay/dialog vocabulary this item reuses
(`renderOverlay` now carries the approval dialog; `listDialog` gained `noticeItem`, `fixedW`,
`setNotice`, and a shared `dialogListH`). Read those before starting — the compositing question is
largely answered, leaving the `commandDefs` flag and the scrolling story as the real work. **Also
read P33.20 first**: the dialog block currently swallows every message while a dialog is open,
which is unreachable today only because pickers require `!streaming`. Transient panels shown
*during* a run would make it reachable, so P33.20 likely stops being parked the moment this starts.

### P33.16 — Should mid-stream provider errors be retryable?

Effort: S — needs a decision before implementing

P33.1 made mid-stream Ollama errors (`{"error": ...}` envelopes: model OOM, context overflow,
worker crash) surface as `provider.EventError` instead of vanishing into a truncated answer. It
emits them as a bare `fmt.Errorf`, following the existing `scanner.Err()` idiom in that adapter —
which means they are **not** picked up by `provider`'s retry logic, unlike errors constructed via
`provider.NewTransportError`.

Whether that is right is a genuine behavior decision, deliberately left above P33.1's scope. The
classes differ: a worker crash is plausibly retryable; a context overflow will fail identically on
every retry and retrying wastes a full prompt-eval on a slow local model, which is precisely the
cost this batch has been trying to reduce. Likely answer is per-class classification rather than a
blanket choice. P33.9 shipped its own `errorMessage()` in `internal/provider/ollama/ollama.go`,
structurally identical to the OpenAI-compat one (same bare-`fmt.Errorf`, not-retryable-by-default
behavior) — it doesn't yet classify by error text, so the taxonomy work below is still open, just
now with a second call site (`internal/provider/openai/errorMessage` and
`internal/provider/ollama/errorMessage`) that any per-class classification needs to reach. Decide
the policy before writing code.

### P33.19 — Name the post-tool-round wait

Effort: S — unblocked by P33.9

P33.4 shipped a **per-run** phase split: "waiting for first token · Ns" until the first model
output, then generating. That is correct as specified, but it means after a tool result the bar
reads `generating…` through the next 10-60s prompt-eval wait — the same dead air the item existed
to expose, just not on the first round.

A per-model-call phase would catch every round, but "waiting for first token" is the wrong words
for it: it is neither the first token nor, while the tool is still executing, a model wait at all.
Distinguishing "tool is running" from "tool finished, model is re-evaluating a now-larger prompt"
truthfully requires knowing when the model actually started work. P33.9 shipped exactly that signal
for the native adapter (`provider.Usage.LoadDurationMS`, populated from Ollama's `load_duration`/
`prompt_eval_count` on every `EventDone`) — it currently reaches the TUI only as a post-turn
`KindNotice`, one-shot per call, not a live per-round phase word. The remaining work is turning that
into the live phase label this item wants; do not guess at wording for a state the TUI can now
measure but doesn't yet render mid-round.

---

## Open Work — Tier 4

Six items parked — P25.9, P33.12, P33.20, P33.21, P33.22, P34.11. Low urgency, no trigger, or
explicitly parked pending demand. Do not build speculatively — revisit only if a concrete trigger
appears, and check with the user before starting any of these.
(P32.9-P32.11 shipped 2026-07-15.)

### P34.11 — If grype is ever reinstated, `dir:` mode must exclude build artifacts

Effort: S — parked; activation trigger is any move to re-add grype to the multiscanner

Parked because it is conditional on a decision that currently points the other way. P34.8
measured grype at 55 findings against this repo where trivy found 0 vulns, and classified them:
**48 of 55 were gitignored compiled `.exe` build artifacts** (`testrun/aegis.exe`,
`aegis-eval.exe`), almost all `stdlib` CVEs from the go1.25.0 toolchain baked into the binary,
plus 2 from a vendored `tsc.exe` inside `node_modules`. `grype dir:` finds these because `syft`
catalogs **574** Go components by reading binaries, against go.mod's 67.

That is a property of one developer's local build output, not of the project's dependencies —
which is **evidence for grype's current exclusion** (`multiscannerExcludedTools`,
`internal/security/multiscanner.go`), not against it. P34.8's item had anticipated the opposite:
that grype's extras might be real unique coverage and therefore an argument to reinstate it. They
aren't.

So this item only activates if someone moves to re-add grype for some *other* reason. If that
happens, the finding to carry forward is that `dir:` mode needs to exclude build outputs
(honoring `.gitignore`, or an explicit exclude list) — otherwise grype reports a scan of the
developer's `go build` output as though it were a scan of the project, and the resulting finding
count is both alarming and almost entirely noise. Note this also makes grype's numbers
machine-dependent: a clean worktree with no compiled binaries gave **1** finding, the same tree
with build artifacts gave 55.

### P33.20 — The dialog block swallows every message while a dialog is open

Effort: S — parked, latent (but see trigger)

`Update`'s dialog branch returns early for *every* message while a dialog is open — including
stream events. This is unreachable today because the pickers that open dialogs all require
`!streaming`, but it is fragile: it already bit P33.7, whose data-message handlers were silently
unreachable until the block was made to fall through for `sessionsLoadedMsg`/`backtrackTargetsMsg`
(`tui.go:1294-1303`). The fix is an allowlist of message types that must always reach the main
update path, rather than per-message fall-through patches accumulating one item at a time.

**Trigger, likely and specific:** P33.11 (transient slash panels) would render dialogs *during* a
run, making this immediately reachable and turning a latent fragility into dropped stream events.
Treat P33.11 as this item's activation, and read this before starting that.

### P33.21 — Editor/background surfaces ignore `KindToolCallStart`

Effort: S — parked, no concrete trigger

P33.3 added `provider.EventToolUseStart` → `KindToolCallStart` and wired it through the engine, the
api wire, and the TUI. `internal/acp/agent.go` and `internal/cli/bg.go` ignore the new kind and
behave exactly as before — deliberate, and correct as a default, since the kind is purely additive.
ACP's `tool_call` update has a natural `pending` status mapping if editor-side early feedback is
ever wanted (Zed/Neovim would get the same "preparing `read_file`…" affordance the TUI now has).
Trigger: a user working through the ACP integration reporting the same dead-air feel P33.3 fixed
for the TUI.

Cosmetic side effect noted here so it isn't rediscovered as a bug: `runRegistry.observe` now records
`"tool_call_start"` as a run's `last_kind` in `GET /runs`. Tool *counts* still key on
`KindToolCall`, so this is observability-only.

### P33.22 — Rename `escPending` to reflect its single purpose

Effort: S — parked, cosmetic

After P33.5, `escPending` is written by exactly one path (arming the idle backtrack picker) but is
still cleared defensively in several send/stream-start handlers (`tui.go:1491,1530,1543,1581,1596`).
The defensive clears are harmless; the flag is simply no longer about "an Esc is pending" in
general, and `backtrackArmed` would say what it means. Pure naming cleanup — bundle it into the
next substantive edit of that region rather than spending a commit on it.

### P33.12 — Composite the wizard and security-config forms as overlays

Effort: M — parked, no concrete trigger

The first-run wizard and the security-config editor are the last two surfaces that replace the
frame outright instead of compositing over the live chat (`render()`). The code comments defend
full-screen for long multi-step forms, which is fair — park this unless the P33.6 approval-overlay
work makes the remaining inconsistency feel worse in practice, or a user reports losing their place
after closing one of these forms.

Status note: P33.6 has now shipped, so the trigger condition is live rather than hypothetical — the
approval dialog composites, and these two do not. No pain reported yet, so it stays parked; revisit
if the inconsistency is noticed in real use.

### P25.9 — Per-session scoping of `lsp.Manager`

Effort: L — parked, no concrete trigger

The P25.1 deliberately-deferred gap list originally named six daemon-wide singletons; five
shipped (see [releases.md](releases.md#latest-changes)): `knowledge.Store`, `longmem.Store`, the
cached repo-map, persona/agent-def directory discovery, and the `os` sandbox backend's
write-confinement profile are all now session-Workdir-aware. `lsp.Manager` alone remains parked —
re-scoping it means starting a second set of real language-server subprocesses per distinct
session root, with no natural bound (P25.8 already threads Workdir through cron/swarm/debate, so
a long-lived daemon could accumulate many distinct roots). That's either an unbounded resource
leak (no cap) or a new eviction/restart failure surface (capped LRU) for a narrower benefit than
the other five. Trigger: a concrete pain point in a future live-eval pass, or a deliberate design
for capped/LRU per-root manager pooling.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
