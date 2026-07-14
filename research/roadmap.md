# Aegis Capability Roadmap

**Last updated:** 2026-07-14 — **P28.3** (Tier 3, engine nudge/retry on a zero-tool-call actionable
turn) shipped. **P28.7** (Tier 2, persistent connection/model-health indicator in the TUI and web UI)
also shipped, closing out Tier 2 entirely. **P28.2** (local-model tool-calling guidance +
`aegis doctor` smoke test), **P28.4** (compaction fallback when the LLM summarizer fails twice in a
row), and **P28.6** (harness-quality fix for `TestLiveWorkflow`'s local-prompt-profile subtest) also
shipped earlier the same day; see [releases.md](releases.md#latest-changes) for the full writeups.
**P28.1** (Tier 1, TUI escape-sequence sanitization for untrusted tool output) also shipped
2026-07-14. All other completed history (the full P27 threat-model batch, P27.1–P27.19, plus
everything before it) also lives there.

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 1 remaining item (P28.5) of the 7 filed 2026-07-14 from a full interactive
evaluation of the TUI, web UI, and daemon against three live Ollama models (`qwythos:latest`,
`deepseek-r1:8b`, `gpt-oss:20b`) over the real HTTP+SSE seam via `TestLiveWorkflow` — see
[Open Work](#open-work) below. **P28.1** (Tier 1, TUI escape-sequence sanitization), **P28.2**,
**P28.3**, **P28.4**, **P28.6**, and **P28.7** all shipped 2026-07-14, closing out Tiers 1 and 2
entirely and leaving only one Tier 3 item — see [releases.md](releases.md#latest-changes). Tier 4
has 4 parked items with no active trigger (see [Parked](#open-work--parked-tier-4)), plus 2 remaining
unresolved needs-verification notes carried over from the P27 threat model (see below); the third
(TUI escape-sequence neutralization) is now resolved by P28.1.

**Next session:** the one remaining item, **P28.5**, still needs a resumable-stream design (checked
2026-07-14: the existing `runRegistry`, `internal/server/runs.go`, is purely informational — no event
buffering/replay to piggyback on). Re-run `TestLiveWorkflow` (recipe in CLAUDE.md) after any change
touching the engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` (P26.1) is the
standalone preflight companion for the same misconfiguration classes (including the workspace trust
check, P27.1, the local-sandbox recommendation, P27.14, and the tool-calling smoke test, P28.2).

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** none open — P28.1 shipped 2026-07-14, see [releases.md](releases.md#latest-changes).

**Tier 2:** none open — P28.2, P28.4, P28.6, P28.7 all shipped 2026-07-14, see
[releases.md](releases.md#latest-changes).

**Tier 3:** P28.5. P28.3 shipped 2026-07-14, see [releases.md](releases.md#latest-changes).

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
Ordered by tier, most urgent first. **P28.1** (Tier 1, TUI escape-sequence sanitization), all four
Tier 2 items — **P28.2** (local-model tool-calling guidance + `aegis doctor` smoke test), **P28.4**
(compaction fallback on repeated summarizer failure), **P28.6** (`TestLiveWorkflow` harness-quality
fix), and **P28.7** (persistent connection/model-health indicator) — and **P28.3** (Tier 3, engine
nudge/retry on a zero-tool-call actionable turn) all shipped 2026-07-14 — see
[releases.md](releases.md#latest-changes) — leaving only **P28.5** open.

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
