# Aegis Capability Roadmap

**Last updated:** 2026-07-13 — **P27.16** (FIND-15, Tier 3, CVSS 3.6, from a 2026-07-13 STRIDE-A
threat-model pass) shipped: quarantine-on-FAIL for the output guard. Previously, a FAIL verdict
that exhausted the guard's corrective retries only ever led to the failing response being surfaced
anyway — any `write_file`/`edit_file` call the turn made already landed on disk and stayed there
untouched. The engine's per-turn checkpoint `Snapshotter` (`internal/checkpoint`) already captures
pre-write content for every path `write_file`/`edit_file` touch before the write happens — the same
primitive `/rewind` uses — so the exhausted-retries branch in `internal/engine/engine.go`'s guard
check now calls `checkpoint.SnapshotterFrom(ctx).RestoreFiles(ctx)` (new `Snapshotter` method
delegating to the existing `Store.RestoreFiles`) to roll every file the turn wrote back to its
pre-turn state before surfacing the failing answer, rather than leaving the bad write in place. A
nil Snapshotter (no checkpoint store wired in) makes the call a no-op, so a caller without one keeps
today's retry-then-surface behavior unchanged. The rollback is surfaced to the caller two ways: a
new `Engine.Event.GuardFilesRestored` count on the terminal `KindGuard` failure event, and the
restored-file count appended to that event's `GuardReason` text, which the TUI's existing output-
guard warning line already renders verbatim — no new UI wiring needed. Full writeup in
[releases.md](releases.md#latest-changes). A companion 2026-07-13 threat-model finding, **P27.17**
(FIND-16, swarm sub-agent budget propagation), is being implemented in parallel in a separate line
of work and is not covered by this update — see that item's own history for its status.

Before that, 2026-07-12: a second batch of four Tier 4 parked items promoted and shipped on
explicit user request: **P22.5** (`/side` ephemeral side conversation), **P22.6** (raw scrollback
mode), **P20.2** (`aegis compare` blind model compare), and **P20.3** (hardware-aware model
recommendation, `internal/hwinfo` + `aegis models --recommend`). **P25.9** and **P6.1** were
deliberately excluded from this batch — both Effort L, both high-blast-radius (daemon singleton
rescoping; core engine streaming loop) — better suited to focused solo work than parallel
automation; **P13.3.3** stays excluded as its precondition (ACP-host usage) still hasn't
materialized. Implemented in parallel by four isolated sub-agents in separate git worktrees; one
doc-only merge conflict (both P22.5 and P22.6 appended to the same `docs/tui-guide.md` table) was
resolved by combining both additions, no code conflicts. Full `go build ./...` and `go test ./...`
pass after merge. Full writeups in [releases.md](releases.md#latest-changes). Before this, same
day: four other Tier 4 items — **P24.21** (bearer-token scrubbing in `Client`), **P13.3.2**
(`@shell` terminal-output context token), **P9.4** (opt-in per-task model routing), and **P13.4**
(`security_advise` engagement-notebook/CVE-lookup/guarded-suggestions tool) — shipped as the first
batch (see [releases.md](releases.md#latest-changes) for that writeup too). Before that, same day:
**P26.2** (fixed a
`sessionWorkdirs`/`sessionSkills` map leak on session delete) shipped, closing out the 2026-07-12
routine roadmap review (competitive-landscape scan + internal code audit; no live-eval or
threat-model trigger that round). Landscape scan against Claude Code/Codex CLI/opencode/Gemini CLI
found nothing new since the 2026-07-02 review (convergent themes already closed; A2A and
Dispatch/Channels remain correctly declined). Before that, same day: **P15.13** (web UI session
workdir picker + display), closing out the entire 2026-07-11 roadmap review's promoted set; before
that, same day: **P26.1** (`aegis doctor` preflight self-diagnostic); before that, same day:
**P25.7** (live-eval harness promoted into `internal/eval`) and **P25.8** (session workdir threaded
through swarm/cron/debate), closing the P25 Tier 1 set; before that: P25.4-P25.6 (approval
ergonomics, token observability, local-model profile), P25.3 (output guard vs local/thinking
models), and P25.1-P25.2 (`6b76e5e`, 2026-07-11). Open: **nothing** — see Status below.

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** none on this line of work. **P27.16** (FIND-15, quarantine-on-FAIL for the output
guard) shipped 2026-07-13 — see [releases.md](releases.md#latest-changes). Note: this roadmap file
does not yet reflect the rest of the 2026-07-13 STRIDE-A threat-model pass (P27.1-P27.15, P27.17,
P27.18), which is landing via a separate parallel line of work — reconcile against that history at
merge time. P24.21/P13.3.2/P9.4/P13.4 and P22.5/P22.6/P20.2/P20.3 shipped 2026-07-12 — see
[releases.md](releases.md#latest-changes). Tier 4 is the remaining parked set (3 items) — see
[Parked](#open-work--parked-tier-4).

**Next session:** nothing queued on this line of work beyond reconciling with the parallel P27
batch above. Next trigger otherwise: a new threat-model pass, a reported incident, a new feature
evaluation, or a concrete pain point surfacing one of the Tier 4 parked items. Re-run
`TestLiveWorkflow` (recipe in CLAUDE.md) after any change touching the
engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` (P26.1) is the standalone
preflight companion for the same misconfiguration classes.

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** empty.

**Tier 2:** empty. P26.2 (`sessionWorkdirs`/`sessionSkills` map leak on session delete) shipped
2026-07-12 — see [releases.md](releases.md#latest-changes).

**Tier 3:** empty on this line of work — **P27.16** shipped 2026-07-13 (see
[releases.md](releases.md#latest-changes)). Before that, the last items shipped 2026-07-11 (P24.20;
P15 web-UI batches A/B/C; P24.14) — see [releases.md](releases.md#latest-changes).

**Tier 4:** parked — P25.9, P13.3.3, P6.1. See [Parked](#open-work--parked-tier-4).

---

## Open Work — P27.16 (2026-07-13 threat-model finding FIND-15)

### P27.16 — shipped 2026-07-13 (see [releases.md](releases.md#latest-changes))

FIND-15 from the 2026-07-13 STRIDE-A threat-model pass: "the output guard runs after files are
already written, so a FAIL verdict can only drive a retry, not undo a bad write." Remediation
chosen: quarantine/roll back a written file on an exhausted-retries FAIL, reusing the existing
checkpoint/rewind machinery rather than adding a new pre-write guard pass. See
[releases.md](releases.md#latest-changes) for the full writeup.

---

## Open Work — Parked (Tier 4)

Low urgency, no trigger, or explicitly parked pending demand. Do not build speculatively —
revisit only if a concrete trigger (user demand, reported pain, incident) appears, and check
with the user before starting any of these. Re-verified 2026-07-12 against the full P25/P26 batch
— no scope changes to any of the 3 items below. P25.9 in particular was checked line-by-line:
P25.8 only threaded `Workdir` through `swarm.SpawnConfig`, `cron.Job`, and `api.DebateRequest`
(which directory a *spawned engine's tool calls* resolve against) — it never touched the
daemon-wide singletons P25.9 names (`lsp.Manager`, `knowledge.Store`, `longmem.Store`, the cached
repo-map, persona/command/agent-def directory discovery, the `os` sandbox backend's
write-confinement profile), so P25.9's scope is reconfirmed unchanged, not narrowed.

**2026-07-12 update:** eight of the original 11 parked items — P24.21, P13.3.2, P9.4, P13.4 (first
batch) and P22.5, P22.6, P20.2, P20.3 (second batch) — were explicitly selected by the user across
two rounds and implemented the same day; see [releases.md](releases.md#latest-changes) for what
shipped. The remaining 3 below are still parked, each for its own reason rather than just "not yet
picked": P25.9 and P6.1 were deliberately excluded from the second batch as too large/risky for
parallel automation (both Effort L, both touch daemon-wide or core-engine state), and P13.3.3
remains gated on an ACP-host precondition that hasn't occurred. Being picked from a bundle isn't
the same as an organic demand signal, so none of these three should be started without a fresh
trigger or explicit user direction.

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

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
