# Aegis Capability Roadmap

**Last updated:** 2026-07-12 — four Tier 4 parked items promoted and shipped on explicit user
request (not a triggered review — see [Parked](#open-work--parked-tier-4) for why these had stayed
parked): **P24.21** (bearer-token scrubbing in `Client`), **P13.3.2** (`@shell` terminal-output
context token), **P9.4** (opt-in per-task model routing), and **P13.4** (`security_advise`
engagement-notebook/CVE-lookup/guarded-suggestions tool). Implemented in parallel by four isolated
sub-agents (disjoint files except a clean auto-merge on `internal/tui/tui.go` between P24.21 and
P13.3.2); full `go build ./...` and `go test ./...` pass after merge. Full writeups in
[releases.md](releases.md#latest-changes). Before this, same day: **P26.2** (fixed a
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

**Open items:** none. P24.21/P13.3.2/P9.4/P13.4 shipped 2026-07-12 — see
[releases.md](releases.md#latest-changes). Tier 4 is the remaining parked set (7 items) — see
[Parked](#open-work--parked-tier-4).

**Next session:** nothing queued. Next trigger: a new threat-model pass, a reported incident, a
new feature evaluation, or a concrete pain point surfacing one of the Tier 4 parked items. Re-run
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

**Tier 3:** empty. The last items shipped 2026-07-11 (P24.20; P15 web-UI batches A/B/C; P24.14) —
see [releases.md](releases.md#latest-changes). Next trigger: a new threat-model pass, a reported
incident, or a new feature evaluation.

**Tier 4:** parked — P25.9, P22.5, P22.6, P20.2, P20.3, P13.3.3, P6.1. See
[Parked](#open-work--parked-tier-4).

---

## Open Work — Parked (Tier 4)

Low urgency, no trigger, or explicitly parked pending demand. Do not build speculatively —
revisit only if a concrete trigger (user demand, reported pain, incident) appears, and check
with the user before starting any of these. Re-verified 2026-07-12 against the full P25/P26 batch
— no scope changes to any of the 7 items below. P25.9 in particular was checked line-by-line:
P25.8 only threaded `Workdir` through `swarm.SpawnConfig`, `cron.Job`, and `api.DebateRequest`
(which directory a *spawned engine's tool calls* resolve against) — it never touched the
daemon-wide singletons P25.9 names (`lsp.Manager`, `knowledge.Store`, `longmem.Store`, the cached
repo-map, persona/command/agent-def directory discovery, the `os` sandbox backend's
write-confinement profile), so P25.9's scope is reconfirmed unchanged, not narrowed.

**2026-07-12 update:** four of the original 11 parked items — P24.21, P13.3.2, P9.4, and P13.4 —
were explicitly selected by the user (from a curated "security/robustness bundle" option, offered
alongside two alternative bundles) and implemented the same day; see
[releases.md](releases.md#latest-changes) for what shipped. The remaining 7 below are still
parked exactly as previously scoped — being picked from a bundle isn't the same as an organic
demand signal for the rest, so they stay parked pending their own trigger.

### P25.9 — Per-session scoping of daemon-singleton services

Priority: Tier 4 · Effort: L — parked, no concrete trigger

The P25.1 deliberately-deferred gaps, tracked here so they aren't lost in releases.md prose:
`lsp.Manager`, `knowledge.Store`, `longmem.Store`, the cached repo-map (`s.repoMap`),
persona/command/agent-def directory discovery, and the `os` sandbox backend's write-confinement
profile (baked at construction; `resolveSessionWorkdir` warns once on the mismatch) all remain
scoped to the daemon's default workspace regardless of a session's Workdir. Each is a
daemon-wide singleton; re-scoping is a materially larger change. Trigger: a concrete pain point
in a future live-eval pass.

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
From the Odysseus review. Competitive-inspired, no direct reported pain. (Checked 2026-07-12:
P25.6's local-model prompt profile routes by deployment shape — loopback vs. remote base_url —
not by hardware/model choice, so it gives no overlap or trigger.)

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
