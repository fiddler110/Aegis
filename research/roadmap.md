# Aegis Capability Roadmap

**Last updated:** 2026-07-14 — **P28.6** (Tier 2, harness-quality fix) shipped:
`TestLiveWorkflow/LocalPromptProfileReducesFirstTurnTokens` now uses a dedicated fixture large
enough to actually trip the local prompt profile's repo-map cap, so the local-vs-default token
comparison is a real signal instead of live-model token-accounting noise; see
[releases.md](releases.md#latest-changes) for the full writeup. **P28.1** (Tier 1, TUI
escape-sequence sanitization for untrusted tool output) also shipped 2026-07-14. All other
completed history (the full P27 threat-model batch, P27.1–P27.19, plus everything before it) also
lives there.

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 5 remaining items (P28.2, P28.3, P28.4, P28.5, P28.7) of the 7 filed 2026-07-14 from
a full interactive evaluation of the TUI, web UI, and daemon against three live Ollama models
(`qwythos:latest`, `deepseek-r1:8b`, `gpt-oss:20b`) over the real HTTP+SSE seam via
`TestLiveWorkflow` — see [Open Work](#open-work) below. **P28.1** (Tier 1, TUI escape-sequence
sanitization) shipped 2026-07-14, closing out Tier 1 entirely — see
[releases.md](releases.md#latest-changes). **P28.6** (Tier 2, `TestLiveWorkflow` harness-quality
fix) also shipped 2026-07-14. Tier 4 has 4 parked items with no active trigger (see
[Parked](#open-work--parked-tier-4)), plus 2 remaining unresolved needs-verification notes carried
over from the P27 threat model (see below); the third (TUI escape-sequence neutralization) is now
resolved by P28.1.

**Next session:** start with Tier 2 — **P28.2**, **P28.4**, or **P28.7** (all cheap, no-dependency
wins; no particular order between them). Re-run `TestLiveWorkflow` (recipe in CLAUDE.md) after any
change touching the engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` (P26.1) is
the standalone preflight companion for the same misconfiguration classes (including the workspace
trust check, P27.1, and the local-sandbox recommendation, P27.14).

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** none open — P28.1 shipped 2026-07-14, see [releases.md](releases.md#latest-changes).

**Tier 2:** P28.2, P28.4, P28.7. P28.6 shipped 2026-07-14, see
[releases.md](releases.md#latest-changes).

**Tier 3:** P28.3, P28.5.

**Tier 4:** parked — P25.9, P13.3.3, P6.1, P27.20. See [Parked](#open-work--parked-tier-4).

---

## Needs-verification items (from the P27 threat model, still unresolved)

The P27 threat model (`threat-model-20260712-200318/`, commit `7230aaf`) flagged these as things
to check before or while implementing the related shipped item — not yet filed as their own
findings, and not confirmed resolved by any shipped writeup:

- Hook execution timing relative to any trust prompt (relevant to **P27.1**, the workspace-trust
  gate).
- Whether cron fire-time gating truly exercises text rules end-to-end or only the contextual gate
  (relevant to **P27.15**).

The third item from this list — whether the TUI fully neutralizes terminal escape sequences in
untrusted tool output — was checked 2026-07-14 by reading the actual render paths
(`internal/tui/tui.go`, `toolview.go`, `sanitize.go`, `ansi16.go`): it did not, and this was fixed
the same day as **P28.1** — see [releases.md](releases.md#latest-changes).

See `0-assessment.md`'s "Needs Verification" table in the report folder for the original context.

---

## Open Work

Filed 2026-07-14 from a full interactive evaluation of the TUI, web UI, and daemon — driving real
sessions over the HTTP+SSE seam (the same one the TUI/web UI use) against three live Ollama
models via `TestLiveWorkflow`, plus a read of the TUI render paths and web UI streaming client.
Ordered by tier, most urgent first. **P28.1** (Tier 1, TUI escape-sequence sanitization) shipped
2026-07-14 — see [releases.md](releases.md#latest-changes) — leaving Tier 2 as the current front.
**P28.6** (Tier 2, `TestLiveWorkflow` harness-quality fix) also shipped 2026-07-14, see the same
writeup.

### P28.2 — Guidance/config for tool-calling-capable local models

Priority: Tier 2 · Effort: S — cheap, no-dependency win

Live evaluation (`TestLiveWorkflow` against `qwythos:latest`, `deepseek-r1:8b`, `gpt-oss:20b`,
2026-07-14) found wide variance in local-model tool-calling reliability: `qwythos:latest` (this
repo's own configured `provider.model` default) diagnosed a seeded bug but never called
`edit_file`/`write_file` to fix it; `deepseek-r1:8b` made **zero tool calls** on an explicit
run/fix/verify task, answering in prose instead (a known R1-distill failure mode — reasoning
dumped as the final answer instead of a structured `tool_call`); only `gpt-oss:20b` completed the
task end-to-end (13 tool calls, 2m28s). `aegis doctor`'s provider check only verifies reachability
and model availability, not tool-calling competence. Add: (a) doc guidance on which
locally-runnable model families reliably drive Aegis's tool-calling loop, (b) consider a doctor
check that does a cheap live round-trip tool-call smoke test against the configured model and
warns if it returns zero tool calls for an obviously-actionable prompt.

### P28.3 — Engine nudge/retry when an actionable turn produces zero tool calls

Priority: Tier 3 · Effort: M — real value, larger, needs design/investigation first

Building on P28.2: when a model responds text-only to a task that plainly requires tool use, the
engine currently just accepts the text-only turn as done — the `deepseek-r1:8b` failure mode
observed live. Consider a corrective nudge (similar in spirit to the existing output-guard retry)
that detects a suspicious zero-tool-call completion on a task-shaped prompt and asks the model to
reconsider/act, or investigate whether the OpenAI-compatible adapter should send
`tool_choice: "required"` rather than `"auto"` when the persona/tools make tool use expected.
Needs investigation into what Ollama's OpenAI-compat endpoint actually supports for `tool_choice`
before committing to an approach — do not build speculatively until that's confirmed.

### P28.4 — Compaction has no fallback when the local summarizer returns empty output

Priority: Tier 2 · Effort: S/M

Live evaluation observed `proactive compaction failed: summarizer returned empty output`
(`internal/compaction/compaction.go:212`) against both `qwythos:latest` and `gpt-oss:20b`. The
engine (`internal/engine/engine.go:456-465`) logs a warning and skips compaction for that turn
entirely — no retry, no fallback. Long local-model sessions run far more turns/tokens per task
than cloud sessions (observed: 87k input / 2.4k output tokens over 13 tool calls for one bug fix
with `gpt-oss:20b`), so a summarizer that unreliably returns empty output can repeatedly fail to
shrink context, drifting toward the hard context-window ceiling with no safety valve. Add a
fallback (e.g. deterministic truncation/drop-oldest-tool-output) when the LLM summarizer fails
twice in a row for the same session.

### P28.5 — Web UI SSE stream has no reconnect/resume on drop

Priority: Tier 3 · Effort: M/L — real value, larger, needs design

`consumeSSE` (`internal/server/webui/frontend/src/api.ts`) has no retry/reconnect logic: a dropped
connection mid-turn (network blip, backgrounded-tab throttling, daemon restart) surfaces as a
terminal "Error: ..." with no automatic resume, even though the underlying engine run may still be
executing server-side (the daemon already tracks in-flight/background runs — see `app.tsx`'s
`loadRuns()`). Local-model turns routinely ran 30s-150s+ in live evaluation and can run much
longer for harder tasks, making a mid-turn drop meaningfully more likely than with fast cloud
round-trips. Needs a resumable-stream design (reconnect and replay/attach to the same run ID) —
investigate how much of the existing detached-run infrastructure already supports resumption
before committing to effort size.

### P28.7 — No persistent connection/model-health indicator in TUI or web UI

Priority: Tier 2 · Effort: S

Real usage evidence (not hypothetical): this instance's own `GET /sessions` history contains at
least 6 near-duplicate sessions from 2026-06-26/27 titled "test that the model is connected,"
"validate model is connected," "confirm that the model is connected," "Check that the model is
connected" — a recorded pattern of spending a full conversational turn just to sanity-check
daemon-to-model connectivity. `aegis doctor` and `GET /status` already answer this server-side.
Add a lightweight, persistent connection/model-health indicator to the TUI status area and web UI
header (reachable/model-name/last-latency at a glance) so this doesn't require a wasted prompt or
a separate CLI invocation.

---

## Open Work — Parked (Tier 4)

Low urgency, no trigger, or explicitly parked pending demand. Do not build speculatively —
revisit only if a concrete trigger (user demand, reported pain, incident) appears, and check
with the user before starting any of these. **P27.20** is parked for a different reason than the
other three — not "no trigger" but "the finding's own remediation is explicitly optional."

### P25.9 — Per-session scoping of daemon-singleton services

Priority: Tier 4 · Effort: L — parked, no concrete trigger

The P25.1 deliberately-deferred gaps, tracked here so they aren't lost in releases.md prose:
`lsp.Manager`, `knowledge.Store`, `longmem.Store`, the cached repo-map (`s.repoMap`),
persona/command/agent-def directory discovery, and the `os` sandbox backend's write-confinement
profile (baked at construction; `resolveSessionWorkdir` warns once on the mismatch) all remain
scoped to the daemon's default workspace regardless of a session's Workdir. Each is a
daemon-wide singleton; re-scoping is a materially larger change. Trigger: a concrete pain point
in a future live-eval pass.

### P13.3.3 — ACP `terminal/*` capability passthrough

Priority: Tier 4 · Effort: M/L — parked, no concrete trigger (deferred pending ACP-host usage)

Let ACP hosts (Zed) supply a pty for agent shell calls.

### P6.1 — Mid-turn state persistence

Priority: Tier 4 · Effort: L — parked, no concrete trigger

Persist partial turn state (text, tool calls) to SQLite during streaming. High complexity,
low-probability failure mode. (Checked 2026-07-12: P25.5 added mid-turn token-usage accumulation
in memory, not persistence to SQLite — no overlap, no trigger.)

### P27.20 — FIND-18 (encryption half): optional at-rest encryption for SQLite stores

Priority: Tier 4 · Effort: M/L — parked, no concrete trigger beyond the finding's own suggestion

SQLCipher (or similar) at-rest encryption for the conversation/checkpoint SQLite stores, on top of
the ACL hardening already covered by P27.10 (Tier 2). Larger scope, opt-in, for higher-assurance
deployments — no reported pain driving it yet.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
