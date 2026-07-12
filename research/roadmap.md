# Aegis Capability Roadmap

**Last updated:** 2026-07-12 — **P15.13** (web UI session workdir picker + display) shipped,
closing out the entire 2026-07-11 roadmap review's promoted set. Before that, same day: **P26.1**
(`aegis doctor` preflight self-diagnostic). Before that, same day: **P25.7** (promoted the
live-eval harness into `internal/eval` as the `live_workflow`-tagged `TestLiveWorkflow`) and
**P25.8** (threaded session workdir through the swarm/cron/debate seams), closing out the entire
P25 Tier 1 set. Previously (same day): P25.4 (approval ergonomics), P25.5 (token-usage
observability for local providers), and P25.6 (local-model prompt profile) shipped. Before that:
P25.3 (output guard vs local/thinking models), and before that (2026-07-11) P25.1 and P25.2
(`6b76e5e`). Full writeups in [releases.md](releases.md#latest-changes). Open: **nothing** — see
Status below for the next trigger.

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** none. The 2026-07-11 roadmap review's promoted set (P26.1, P15.13) is fully
shipped — see [releases.md](releases.md#latest-changes). Tier 4 is the parked set — see
[Parked](#open-work--parked-tier-4).

**Next session:** nothing queued. Next trigger: a new threat-model pass, a reported incident, a
new feature evaluation, or a concrete pain point surfacing one of the Tier 4 parked items. Re-run
`TestLiveWorkflow` (recipe in CLAUDE.md) after any change touching the
engine/server/sandbox/guard/swarm/cron/debate seams — it's the regression lock for the whole P25
batch; `aegis doctor` (P26.1) is the standalone preflight companion for the same misconfiguration
classes.

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** empty. P15.13 (web UI workdir picker) and P26.1 (`aegis doctor`), the 2026-07-11
review's promoted set, both shipped 2026-07-12 — see [releases.md](releases.md#latest-changes).

**Tier 2:** empty.

**Tier 3:** empty. The last items shipped 2026-07-11 (P24.20; P15 web-UI batches A/B/C; P24.14) —
see [releases.md](releases.md#latest-changes). Next trigger: a new threat-model pass, a reported
incident, or a new feature evaluation.

**Tier 4:** parked — P25.9, P24.21, P22.5, P22.6, P20.2, P20.3, P13.3.2, P13.3.3, P13.4, P9.4,
P6.1. See [Parked](#open-work--parked-tier-4).

---

## Shipped — P25 (Local-Model Live Evaluation — 2026-07-11)

Full writeups for P25.1–P25.8 are in [releases.md](releases.md#latest-changes) (this document
tracks only open work). Kept here only as a pointer for context: the whole tier was found by a
live evaluation session that drove the real TUI and the daemon HTTP API/SSE against local Ollama
models — headline result: **the local model is not the bottleneck — the harness is.** Its
regression lock, [research/eval-harness-drive.py](eval-harness-drive.py)'s Go port
`internal/eval/live_workflow_test.go` (`TestLiveWorkflow`, P25.7), documented in CLAUDE.md, is
the way to re-verify any of the P25.1–P25.6 fixes or catch a regression in the
engine/server/sandbox/guard/swarm/cron/debate seams they touched. The original Python script
stays in research/ for ad-hoc one-off drives.

## Open Work — Parked (Tier 4)

Low urgency, no trigger, or explicitly parked pending demand. Do not build speculatively —
revisit only if a concrete trigger (user demand, reported pain, incident) appears, and check
with the user before starting any of these.

### P25.9 — Per-session scoping of daemon-singleton services

Priority: Tier 4 · Effort: L — parked, no concrete trigger

The P25.1 deliberately-deferred gaps, tracked here so they aren't lost in releases.md prose:
`lsp.Manager`, `knowledge.Store`, `longmem.Store`, the cached repo-map (`s.repoMap`),
persona/command/agent-def directory discovery, and the `os` sandbox backend's write-confinement
profile (baked at construction; `resolveSessionWorkdir` warns once on the mismatch) all remain
scoped to the daemon's default workspace regardless of a session's Workdir. Each is a
daemon-wide singleton; re-scoping is a materially larger change. Trigger: a concrete pain point
in a future live-eval pass.

### P24.21 — Memory-lock/zero the bearer token in `Client` process memory

Priority: Tier 4 · Effort: M — parked, no concrete trigger

FIND-33 (security, Low, CVSS 2.8) from the 2026-07-10 STRIDE-A threat model
([threat-model-20260710-173718/](../threat-model-20260710-173718/3-findings.md)) — the only one
of its 35 findings still open (14 were verified existing controls; the other 30 shipped as
P24.1–P24.20/P24.22). Explicitly low priority per the finding itself — host/OS access is
already a significant compromise.

### P22.5 — `/side` ephemeral side conversation

Priority: Tier 4 · Effort: S/M — parked, no concrete trigger

Quick side question in a throwaway context. From the Codex CLI evaluation (2026-07-08; P22.1–P22.4
shipped). Polish without demand.

### P22.6 — Raw scrollback mode

Priority: Tier 4 · Effort: S/M — parked, no concrete trigger

Plain-text rendering + release the alternate screen for native selection/scrollback. From the
Codex CLI evaluation. Polish without demand.

### P20.2 — Blind model compare

Priority: Tier 4 · Effort: M — parked, no concrete trigger

Same prompt to two models side-by-side, identities hidden until vote, then reveal + optional
synthesis. From the Odysseus review (P20.1 shipped). Competitive-inspired, no direct reported
pain.

### P20.3 — Hardware-aware model recommendation

Priority: Tier 4 · Effort: M — parked, no concrete trigger

Detect hardware, curate/recommend local models, offer `ollama pull`, surface via `/models`.
From the Odysseus review. Competitive-inspired, no direct reported pain.

### P13.3.2 — `@shell`/`@last` context token

Priority: Tier 4 · Effort: S — parked, no concrete trigger

Extend `@file`/`@image:` to inject the last N lines of terminal output.

### P13.3.3 — ACP `terminal/*` capability passthrough

Priority: Tier 4 · Effort: M/L — parked, no concrete trigger (deferred pending ACP-host usage)

Let ACP hosts (Zed) supply a pty for agent shell calls.

### P13.4 — Nebula-inspired security engagement tooling

Priority: Tier 4 · Effort: M — parked, no concrete trigger

Engagement notebook + `security_advise` tool + CVE lookup + status digest + guarded next-step
suggestions. "Interesting, not urgent" per its own scoping.

### P9.4 — Per-task model routing

Priority: Tier 4 · Effort: M — parked, no concrete trigger

Pick a cheaper model for simple turns, reserve the expensive one for hard ones. No evidence of
demand.

### P6.1 — Mid-turn state persistence

Priority: Tier 4 · Effort: L — parked, no concrete trigger

Persist partial turn state (text, tool calls) to SQLite during streaming. High complexity,
low-probability failure mode.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
