# Aegis Capability Roadmap

**Last updated:** 2026-07-16

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 10 — five Tier 3 items (**P33.9, P33.10, P33.11, P33.16, P33.19**) and five parked
(**P25.9, P33.12, P33.20-P33.22**). Tier 1 and Tier 2 are both fully clear.

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

**Next session:** with Tier 2 clear, **P33.9 (native Ollama adapter) is next** — it's the keystone
of the remaining batch: unblocks P33.10, upgrades P33.4's phase display from inference to fact,
replaces the estimated token heuristic behind the single `streamStats()` seam P33.4 left for
exactly that purpose, and is a prerequisite for P33.19. It's also the largest remaining item (Effort:
L), so budget accordingly rather than pairing it with something else in the same session. P33.16
needs a decision before implementation and is best sequenced after P33.9 gives it a real error
taxonomy to decide against. P33.11 should read P33.20 first — starting it is P33.20's stated
activation trigger. Re-run `TestLiveWorkflow` (recipe in CLAUDE.md) after any change touching the
engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` is the standalone preflight
companion for the same misconfiguration classes.

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

**Status:** None open. (P33.13, P33.14, P33.15, P33.17, P33.18 shipped 2026-07-16; P33.3-P33.8
shipped 2026-07-15; P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14; P32.5-P32.7 shipped 2026-07-15.)

---

## Open Work — Tier 3

Five items: P33.9, P33.10, P33.11, P33.16, P33.19. Medium-effort with real value; some are
sequence-dependent. P33.9 is the keystone unblocking P33.10 and P33.19.
(P32.8 shipped 2026-07-15.)

### P33.9 — Native Ollama provider adapter

Effort: L

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

Effort: M — partially blocked by P33.9

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
blanket choice — which means it needs the error taxonomy that P33.9's native adapter would give
better access to. Decide the policy before writing code; sequence after P33.9 if per-class
classification wins.

### P33.19 — Name the post-tool-round wait

Effort: S — blocked by P33.9

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

## Open Work — Tier 4

Five items parked — P25.9, P33.12, P33.20, P33.21, P33.22. Low urgency, no trigger, or
explicitly parked pending demand. Do not build speculatively — revisit only if a concrete trigger
appears, and check with the user before starting any of these.
(P32.9-P32.11 shipped 2026-07-15.)

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
