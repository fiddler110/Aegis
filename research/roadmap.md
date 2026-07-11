# Aegis Capability Roadmap

**Last updated:** 2026-07-11 — Tiers 1–3 of the previous pass are now fully shipped (most recently
P24.16–P24.18, the STRIDE-A threat model's Tier 3 third batch). This pass re-tiers everything that
remains open: promoted P15.3–P15.11 out of parked status now that their P15.2 backend dependency has
shipped, and pulled four cheap, no-dependency P24 residuals up into a new Tier 2. Tier 1 is empty.

This document tracks only **open** work and what's next. For shipped-feature history and full design
rationale, see [releases.md](releases.md).

---

## Status

**Open items:** P24.14/P24.15/P24.20–P24.22 (threat-model residuals), P15.3–P15.11 (web UI frontend
panels), P22.5/P22.6, P20.2–P20.3, P13.3.2–P13.3.3/P13.4, P9.4, P6.1.

**Priority order:** see [Priority Order](#priority-order) below — it is the authoritative "what's
next" view, ordered by tier and effort.

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no concrete trigger, or explicitly parked pending demand — do not
build speculatively.

### Tier 1 — empty

All Critical/Important findings from the 2026-07-10 STRIDE-A threat model are shipped (see
[releases.md](releases.md#latest-changes)). Next trigger: a new threat-model pass or a reported
incident.

### Tier 2 — Cheap, no-dependency wins

- **P24.15 — FIND-14: give each swarm sub-agent a guaranteed minimum budget floor** (S, security,
  Low, CVSS 3.6). Fairness gap in `internal/swarm/subprocess.go`'s shared tracker; no reported
  incident but small and self-contained.
- **P24.20 — FIND-17: strip/escape ANSI/OSC control sequences in streamed model output before TUI
  render** (S, security, Low, CVSS 3.0). Defense-in-depth for an already-caught injection vector;
  cheap, self-contained hardening pass.
- **P24.22 — Quote/escape the `distro` argument in `sandbox.WSLInstallCommand`** (S, security,
  informational). Currently dead code — `install.go`'s only call site hardcodes `""` for `distro` —
  but worth closing defensively before a second, config-driven caller appears.

### Tier 3 — Larger, real, sequence-dependent

- **P15.3–P15.11 — Web UI frontend panels** (M–L). Persona/model management, cost/token visibility,
  checkpoints/rewind, security scanning, skills/memory, debate/knowledge, multi-session lifecycle,
  approval persistence, non-technical-user framing. Frontend-only now — the P15.6/P15.7 backend
  dependency on P15.2's config-mutation endpoints shipped 2026-07-10, so this is no longer blocked.
- **P24.14 — FIND-12: document (and consider an opt-in outbound redaction hook for) MCP tool-call
  argument content** (S/M, security, Moderate, CVSS 4.6). Value is contingent on FIND-04/FIND-05
  injection vectors actually being exploited in practice; documentation alone covers most of the
  value today, a redaction hook would be the sequence-dependent follow-up if that changes.

### Tier 4 — Parked / low priority / no current trigger

Do not build speculatively — revisit only if a concrete trigger (user demand, reported pain,
incident) appears.

- **P24.21 — FIND-33: memory-lock/zero the bearer token in `Client` process memory** (M, security,
  Low, CVSS 2.8). Explicitly low priority per the finding itself — host/OS access is already a
  significant compromise.
- **P20.2 — Blind model compare** (M) and **P20.3 — Hardware-aware model recommendation** (M):
  competitive-inspired, no direct reported pain.
- **P13.3.2 — `@shell`/`@last` context token** (S) and **P13.3.3 — ACP terminal capability
  passthrough** (M/L): P13.3.3 deferred pending ACP-host usage.
- **P13.4 — Nebula-inspired engagement tooling** (M): "interesting, not urgent" per its own scoping.
- **P9.4 — Per-task model routing** (M) and **P6.1 — Mid-turn state persistence** (L): no concrete
  trigger; check with user before starting.
- **P22.5 — `/side` ephemeral conversation** (S/M) and **P22.6 — Raw scrollback mode** (S/M): polish
  without demand.

---

## Open Work — P24 (Threat Model Findings — 2026-07-10)

Full-repo STRIDE-A threat model at commit `34aa687`:
[`threat-model-20260710-173718/`](../threat-model-20260710-173718/3-findings.md). 35 findings
total: 14 were "existing control" (already mitigated, verified, no action needed), 26 were shipped
as P24.1–P24.13/P24.16–P24.19 across 2026-07-10/11 (see [releases.md](releases.md#latest-changes)
for the full writeup of each), and 5 remain open — P24.14, P24.15, P24.20–P24.22 — tiered above.

---

## Open Work — P15 (Web UI Parity with the TUI)

Bring `aegis ui` up to feature parity with the TUI. P15.1 (frontend architecture), P15.12
(token-injection hardening), and P15.2 (config-mutation endpoints) have shipped.

**P15.3–P15.11 — Frontend panels. [Tier 3]** Persona/model management, cost/token visibility,
checkpoints/rewind, security scanning, skills/memory, debate/knowledge, multi-session lifecycle,
approval persistence, non-technical-user framing. Frontend-only; the P15.6/P15.7 backend dependency
on P15.2 is unblocked.

---

## Open Work — P22 (OpenAI Codex CLI evaluation — 2026-07-08)

Feature evaluation of Codex CLI. P22.1 (`/diff`), P22.2 (`/review`), P22.3 (Esc-Esc backtrack +
`/fork`), and P22.4 (Ctrl+R history search) have shipped.

**P22.5 — `/side` ephemeral side conversation. [Tier 4]** Quick side question in a throwaway
context. (S/M)

**P22.6 — Raw scrollback mode. [Tier 4]** Plain text rendering + release alternate screen for
native selection/scrollback. (S/M)

---

## Open Work — P20 (Odysseus Review: Research, Compare, Model Fit)

Feature evaluation of Odysseus self-hosted AI workspace. P20.1 (deep-research skill + `/research`
command) has shipped.

**P20.2 — Blind model compare. [Tier 4]** Same prompt to two models side-by-side, identities hidden
until vote, then reveal + optional synthesis. (M)

**P20.3 — Hardware-aware model recommendation. [Tier 4]** Detect hardware, curate/recommend local
models, offer `ollama pull`, surface via `/models` command. (M)

---

## Open Work — P13 (Security & Capability Enhancements)

P13.1, P13.2, P13.3.1, P13.3.5, P13.5, P13.6, and P13.8 have shipped. All remaining items are
Tier 4.

**P13.3.2 — `@shell`/`@last` context token. [Tier 4]** Extend `@file`/`@image:` to inject last N
lines of terminal output. (S)

**P13.3.3 — ACP `terminal/*` capability passthrough. [Tier 4]** Let ACP hosts (Zed) supply pty for
agent shell calls. (M/L)

**P13.4 — Nebula-inspired security engagement tooling. [Tier 4]** Engagement notebook +
`security_advise` tool + CVE lookup + status digest + guarded next-step suggestions. (M)

---

## Open Work — P9 (Engineering Quality — lower urgency, no current trigger)

P9.1, P9.2, and P9.5 have shipped. P9.3 and P9.6 were dropped.

**P9.4 — Per-task model routing. [Tier 4]** Pick cheaper model for simple turns, reserve expensive
for hard ones. No evidence of demand. (M)

---

## Open Work — P6 (Long-Horizon / Exploratory)

P6.2 (A2A protocol) and P6.3 (MCP server mode) have shipped/dropped.

**P6.1 — Mid-turn state persistence. [Tier 4]** Persist partial turn state (text, tool calls) to
SQLite during streaming. High complexity, low-probability failure mode. (L)

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
