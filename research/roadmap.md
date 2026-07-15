# Aegis Capability Roadmap

**Last updated:** 2026-07-15

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 15 — three surviving P33 items (**P33.9-P33.11**, all Tier 3), seven new items
(**P33.13-P33.19**) opened 2026-07-15 from findings surfaced while implementing the P33 batch, and
five parked (**P25.9, P33.12, P33.20-P33.22**).

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

**Next session:** the Tier 2 items are all small and independent — P33.14 (repo-wide `gofmt` + a CI
gate) is the cheapest and removes a recurring papercut; P33.15 (steer error surfacing) is the most
user-visible. P33.13 (persona picker) finishes the job P33.7 started. Of Tier 3, **P33.9 (native
Ollama adapter) is the keystone**: it unblocks P33.10, upgrades P33.4's phase display from
inference to fact, replaces the estimated token heuristic behind the single `streamStats()` seam
P33.4 left for exactly that purpose, and is a prerequisite for P33.19. Re-run `TestLiveWorkflow`
(recipe in CLAUDE.md) after any change touching the engine/server/sandbox/guard/swarm/cron/debate
seams; `aegis doctor` is the standalone preflight companion for the same misconfiguration classes.

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** none open. (P33.1 and P33.2 shipped 2026-07-15; P31.1, P31.2, P30.1-P30.3 shipped
2026-07-14; P32.1-P32.4 shipped 2026-07-15.)

**Tier 2:** P33.13 (persona picker instant-open), P33.14 (repo-wide gofmt + CI gate), P33.15 (steer
error surfacing), P33.17 (stale input-token count), P33.18 (completion popup steals viewport
height). (P33.3-P33.8 shipped 2026-07-15; P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14;
P32.5-P32.7 shipped 2026-07-15.)

**Tier 3:** P33.9 (native Ollama adapter), P33.10 (keep-alive/pre-warm — partially blocked by
P33.9), P33.11 (transient slash panels), P33.16 (mid-stream error retryability — needs a decision),
P33.19 (post-tool-round wait phase — blocked by P33.9). (P32.8 shipped 2026-07-15.)

**Tier 4:** parked — P25.9, P33.12, P33.20, P33.21, P33.22. See [Parked](#open-work--parked-tier-4).
(P32.9-P32.11 shipped 2026-07-15.)

---

## Open Work

### P33.9 — Native Ollama provider adapter

Priority: Tier 3 · Effort: L

**The keystone of the remaining batch** — it unblocks P33.10 and P33.19, and upgrades P33.4's
shipped phase display from inference to fact.

The OpenAI-compat endpoint structurally blocks four things the harness currently works around:
**(1)** per-request `num_ctx` — today Aegis can only *detect and warn* about the serving context
(`internal/server/contextwindow.go`, `internal/ollamainfo`) instead of setting it; **(2)**
`keep_alive` control on chat requests (see P33.10); **(3)** real token usage — local runs mark
usage `IsEstimated` via the byte heuristic (`internal/engine/engine.go`), so token caps, the
context bar, and compaction thresholds all run on estimates; **(4)** load/prompt-eval telemetry.
Ollama's native `/api/chat` returns `prompt_eval_count`, `eval_count`, `eval_duration`, and
`load_duration` with every response — real tokens, real tok/s, and the ability to surface "model
cold-loaded (8.2s)" as a dim `KindNotice`.

Plan: new `internal/provider/ollama` implementing the existing `provider.Adapter` seam (one
`Stream` method — the whole point of the abstraction); reuse `internal/provider/sse` helpers
(including P33.1's `NewStreamingClient`) and the retry/failover decorators from
`providerfactory.Build`; config selects it (e.g. provider `ollama` with the existing base-URL
plumbing); the openai adapter remains for genuinely OpenAI-compatible gateways. `ollamainfo`'s
three-source context-window detection collapses to "ask the request we just made" for sessions on
this adapter. Run the `live_workflow` eval tier against it before switching any default (recipe in
CLAUDE.md).

**Seams P33.1-P33.8 deliberately left for this item** (all shipped, all waiting):
- `model.streamStats()` (`internal/tui/tui.go`) is the *only* place the 4-bytes-per-token heuristic
  exists, and it sets `estimated: true`. The formatter and both render sites are already
  estimate-agnostic (there is a test pinning the no-tilde reported-counts render). Assign real
  per-delta counts and clear the flag — no caller changes needed.
- `provider.EventToolUseStart` (P33.3) needs an emission point in the new adapter to keep
  provisional tool cards working; a producer that never emits it degrades gracefully, so this is
  not a blocker, but it is a visible regression against the openai path if skipped.
- P33.1's mid-stream error-envelope handling (`errorMessage()`, both the bare-string and object
  spellings) is OpenAI-adapter-local. The native adapter needs its own equivalent; Ollama's native
  API uses the bare-string spelling.

### P33.10 — Ollama keep-alive management and pre-warm

Priority: Tier 3 · Effort: M — partially blocked by P33.9

Ollama unloads models after its default 5-minute `keep_alive`; the next message then pays a full
cold reload (tens of seconds on 16GB VRAM). P33.4 shipped a phase split that makes this wait
*visible* ("waiting for first token · Ns") but cannot yet *name* it as a cold load — that needs
P33.9's `load_duration`. Two levers: **(1)** a config knob (e.g. `provider.ollama.keep_alive`)
passed on chat requests — requires the native adapter (P33.9); the OpenAI-compat endpoint ignores
it. **(2)** Independent of P33.9: a native-API warm ping (in-repo precedent:
`internal/cli/ollama.go:17-29` already POSTs `keep_alive: 0` to force-unload — the inverse
operation) triggered when the TUI regains focus (`tea.FocusMsg` is already tracked) or on first
keystroke of a new message, gated on `/api/ps` (via `ollamainfo`) reporting the model unloaded.
Never default `keep_alive: -1` (pin-forever) — the machine profile this targets has 16GB system RAM
and other workloads; make persistence an explicit user choice.

### P33.11 — Transient panels for informational slash commands

Priority: Tier 3 · Effort: M

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

### P33.13 — Persona picker opens instantly (finishes P33.7)

Priority: Tier 2 · Effort: M

P33.7 shipped instant-open loading states for the session (Ctrl+Y) and backtrack (Esc-Esc) pickers
and corrected the record on the rest: `/session` never opened a picker (`cmdSession` prints text),
and `/timeline` and the model picker are backed by local data with no RPC to wait on. The **persona
picker** (`/persona` → `client.ListPersonas`) is the one genuinely remote-backed picker still doing
fetch-then-open, and the original item never mentioned it.

It was deferred rather than rushed because it is not the same shape as the other two: it opens
through the generic `slashResultMsg` path, so opening early needs a pre-dispatch hook in
`handleSlashCommand` — currently a value receiver returning `tea.Cmd`, so it cannot mutate the
model. That refactor is the item. Reuse P33.7's machinery verbatim once the hook exists
(`newLoadingDialog`, `fixedW` to prevent the width snap, `awaitingPicker(kind)` for
dismiss-before-data, and the dialog-block fall-through that data messages need in order to be
reachable at all — see `tui.go:1294-1303`). Note the persona list is also hot-reloaded server-side
(`persona.Refresh`), so it is the picker most likely to be genuinely slow.

Done when: `/persona` opens instantly with a loading row, populates in place, and handles
dismiss-before-data and fetch error the same way the session picker does; tests mirror
`picker_loading_test.go`.

### P33.14 — Repo-wide `gofmt` cleanup and a CI gate

Priority: Tier 2 · Effort: S

`gofmt -l ./internal ./cmd` currently reports three files unformatted at HEAD:
`internal/checkpoint/checkpoint.go`, `internal/server/auth.go`, `internal/tool/builtin/
knowledge_test.go`. All three are pre-existing and unrelated to any recent item — they were noticed
independently by two different P33 agents, each of which correctly left them alone to keep its diff
clean, which is exactly how drift like this survives.

The formatting fix is trivial. **The item is really the CI gate**: nothing currently fails when
unformatted code lands, so this will silently recur. Add a `gofmt -l` check (failing on non-empty
output) to the existing CI workflow, and consider `go vet ./...` alongside it if not already gated.
Do the gate in the same change as the cleanup, or the cleanup is pointless.

### P33.15 — Steer error surfacing in the TUI

Priority: Tier 2 · Effort: S

Three related loose ends left by P33.2, grouped because they all live in the same TUI steer/error
path:

1. **429 vs 404 are conflated.** P33.2 made the server distinguish "steer buffer full" (429,
   retryable) from "run already finished" (404, not) — a distinction that did not exist before, and
   which was the whole point of the `steerBox` fence. The TUI treats both as a generic `errMsg`, so
   the user sees the same opaque failure for "try again" and "the run's over".
2. **A failed steer POST visually tears down a live run.** `errMsg` sets `m.streaming = false` even
   when the failure is a *steer POST* on a still-live stream. Pre-existing and not touched by
   P33.2, but it means a transient steer failure makes a run that is in fact still going look
   finished. Scope the teardown to failures that actually end the stream.
3. **The denial-feedback steer renders as a user turn.** `internal/tui/approval.go:236-262` posts
   `"The user denied the %s call. Feedback: …"` on the same steer channel. It is normally consumed
   (a denial happens mid-tool-round) so this is latent, but it now inherits P33.2's safety net —
   and if it ever comes back unconsumed, the TUI will requeue that system-phrased text as the
   user's next message. Either tag steers with an origin so the requeue path can render/skip
   system-originated ones, or exclude them from requeue entirely.

### P33.16 — Should mid-stream provider errors be retryable?

Priority: Tier 3 · Effort: S — needs a decision before implementing

P33.1 made mid-stream Ollama errors (`{"error": ...}` envelopes: model OOM, context overflow,
worker crash) surface as `provider.EventError` instead of vanishing into a truncated answer. It
emits them as a bare `fmt.Errorf`, following the existing `scanner.Err()` idiom in that adapter —
which means they are **not** picked up by `provider`'s retry logic, unlike errors constructed via
`provider.NewTransportError`.

Whether that is right is a genuine behavior decision, deliberately left above P33.1's scope. The
classes differ: a worker crash is plausibly retryable; a context overflow will fail identically on
every retry and retrying wastes a full prompt-eval on a slow local model, which is precisely the
cost this batch has been trying to reduce. Likely answer is per-class classification rather than a
blanket choice — which means it needs the error taxonomy that P33.9's native adapter would give
better access to. Decide the policy before writing code; sequence after P33.9 if per-class
classification wins.

### P33.17 — The `↑` input-token count is last turn's number

Priority: Tier 2 · Effort: S

`m.inputTokens` — the `↑4.2k` segment in the streaming hint — holds the *previous* turn's prompt
size while the current turn streams. Pre-existing and inherited from the old hint, but P33.4 made
it considerably more prominent by keeping the hint visible for the entire streaming duration
instead of only in the no-live-text branch. It is a subtly stale number now shown continuously,
which is worse than one shown briefly.

Fix options, cheapest first: zero/hide the segment until the current turn's real prompt size is
known; or estimate it at send time from the assembled request. Note P33.9 would deliver the real
number (`prompt_eval_count`) — but only for Ollama sessions, so this still wants a
provider-agnostic answer. Keep it truthful: an absent number beats a wrong one.

### P33.18 — Completion popup steals viewport height

Priority: Tier 2 · Effort: S

The last known layout-reflow jump in the normal flow. P33.6 moved the approval dialog to the
`renderOverlay` compositor so transcript geometry stays stable during permission-heavy runs; the
completion popup still inserts into the vertical layout and shrinks the transcript the same way the
approval dialog used to. It was explicitly out of scope for P33.6 and noted there as the follow-up
candidate.

P33.6's diff is the template — remove the branch from `fixedH()` and `renderChat()`'s `parts`, drop
the `applyViewportHeight()` calls that exist only to make room, and composite instead. One real
difference to think through: the completion popup is anchored to the composer and non-modal (the
user is still typing into the box behind it), where the approval dialog is centred and modal, so
`renderOverlay`'s centring and background-dimming are not directly reusable. Placement is the work.

### P33.19 — Name the post-tool-round wait

Priority: Tier 3 · Effort: S — blocked by P33.9

P33.4 shipped a **per-run** phase split: "waiting for first token · Ns" until the first model
output, then generating. That is correct as specified, but it means after a tool result the bar
reads `generating…` through the next 10-60s prompt-eval wait — the same dead air the item existed
to expose, just not on the first round.

A per-model-call phase would catch every round, but "waiting for first token" is the wrong words
for it: it is neither the first token nor, while the tool is still executing, a model wait at all.
Distinguishing "tool is running" from "tool finished, model is re-evaluating a now-larger prompt"
truthfully requires knowing when the model actually started work — which is P33.9's
`prompt_eval_count`/`load_duration` territory. Do not guess at wording for a state the TUI cannot
currently measure; that is why this is blocked rather than merely sequenced.

---

## Open Work — Parked (Tier 4)

Low urgency, no trigger, or explicitly parked pending demand. Do not build speculatively —
revisit only if a concrete trigger (user demand, reported pain, incident) appears, and check
with the user before starting any of these.

### P33.20 — The dialog block swallows every message while a dialog is open

Priority: Tier 4 · Effort: S — parked, latent (but see trigger)

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

Priority: Tier 4 · Effort: S — parked, no concrete trigger

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

Priority: Tier 4 · Effort: S — parked, cosmetic

After P33.5, `escPending` is written by exactly one path (arming the idle backtrack picker) but is
still cleared defensively in several send/stream-start handlers (`tui.go:1491,1530,1543,1581,1596`).
The defensive clears are harmless; the flag is simply no longer about "an Esc is pending" in
general, and `backtrackArmed` would say what it means. Pure naming cleanup — bundle it into the
next substantive edit of that region rather than spending a commit on it.

### P33.12 — Composite the wizard and security-config forms as overlays

Priority: Tier 4 · Effort: M — parked, no concrete trigger

The first-run wizard and the security-config editor are the last two surfaces that replace the
frame outright instead of compositing over the live chat (`render()`). The code comments defend
full-screen for long multi-step forms, which is fair — park this unless the P33.6 approval-overlay
work makes the remaining inconsistency feel worse in practice, or a user reports losing their place
after closing one of these forms.

Status note: P33.6 has now shipped, so the trigger condition is live rather than hypothetical — the
approval dialog composites, and these two do not. No pain reported yet, so it stays parked; revisit
if the inconsistency is noticed in real use.

### P25.9 — Per-session scoping of `lsp.Manager`

Priority: Tier 4 · Effort: L — parked, no concrete trigger

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
