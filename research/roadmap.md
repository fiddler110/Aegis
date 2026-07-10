# Aegis Capability Roadmap

**Last updated:** 2026-07-10 — Tier 2 items shipped (P21.3, P22.3), in parallel via isolated
git-worktree sub-agents, same day as the Tier 1 pass.

This document tracks only **open** work and what's next. For shipped-feature history and full design rationale, see [releases.md](releases.md). Recent shipped items: P21.3/P22.3 (Tier 2 high-visibility wins, 2026-07-10), P21.5/P21.6/P15.12 (Tier 1 security/robustness, 2026-07-10), P22.1–P22.4 (CLI features, 2026-07-08), P21.1/P21.4/P21.7 (TUI polish, 2026-07-07), P20.1 (deep-research skill, 2026-07-07), P18–P19/P17/P16 (TUI/streaming/polish, 2026-07-07), P13.1/P13.2/P13.5/P13.6/P13.7/P13.8 (security/capability, 2026-07-06), P23 (Ollama context-window detection, 2026-07-08).

---

## Status

**Open items:** P21.2, P15.2–P15.11, P22.5/P22.6, P20.2–P20.3, P13.3.2–P13.3.3/P13.4, P9.4, P6.1. See [Priority Order](#priority-order) below for what's next.

**Priority order:** see the tiered breakdown immediately below — it is the authoritative "what's next" view, ordered by tier and effort.

---

## Priority Order

Reorganized 2026-07-10 to answer "what's next" directly, cutting across the P-number tracks below.
Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, high-visibility user-facing wins with no dependency. **Tier 3** =
real value but larger or sequence-dependent (blocks or is blocked by other work). **Tier 4** = low
urgency, no concrete trigger, or explicitly parked pending demand — do not build speculatively.

### Tier 1 — Security & robustness, do next

**All three shipped 2026-07-10** — Tier 1 is now empty. Kept here (struck through) for the record; see [releases.md](releases.md#latest-changes) for implementation detail.

- ~~**P21.5 — Daemon resource ceilings** (S/M). No global cap on concurrent sessions/runs or SSE
  buffer growth; `aegis mcp-serve` now exposes sessions to external MCP clients, so this is a live
  resource-exhaustion path, not theoretical.~~ **SHIPPED 2026-07-10.**
- ~~**P15.12 — Harden the `/ui` token-injection mechanism** (S/M, security). `GET /ui` hands the raw
  long-lived daemon auth token to any local process in cleartext.~~ **SHIPPED 2026-07-10.**
- ~~**P21.6 — MCP tool output trust boundary** (S, security). MCP tool output flows back into model
  context unfiltered — a compromised MCP server is an unguarded prompt-injection vector.~~
  **SHIPPED 2026-07-10.**

### Tier 2 — Cheap, high-visibility wins

**Both shipped 2026-07-10** — Tier 2 is now empty. Kept here (struck through) for the record; see [releases.md](releases.md#latest-changes) for implementation detail.

- ~~**P21.3 — Streaming caret** (S). A blinking caret at the live-tail write head — cheap change,
  large share of the perceived "feels rough vs. crush/Claude Code" gap.~~ **SHIPPED 2026-07-10.**
- ~~**P22.3 — Esc-Esc backtrack + `/fork`** (M). Edit a previous user message and fork the
  conversation from that turn. Real workflow gap (not speculative).~~ **SHIPPED 2026-07-10.**

### Tier 3 — Larger, real, sequence-dependent
- **P15.2 — New daemon config-mutation endpoints** (M). The concrete backend gap blocking
  P15.6/P15.7 and unlocking the rest of the P15 web-UI track. Start here if P15 is still an active priority.
- **P21.2 — Tool-call cards** (M). In-place-updating tool-call blocks. Real polish win, but a larger
  visual restructure than P21.3 — do after the caret.

### Tier 4 — Parked / low priority / no current trigger
Do not build speculatively — revisit only if a concrete trigger (user demand, reported pain, incident) appears.
- **P20.2 — Blind model compare** (M) and **P20.3 — Hardware-aware model recommendation** (M): competitive-inspired, no direct reported pain.
- **P13.3.2 — `@shell`/`@last` context token** (S) and **P13.3.3 — ACP terminal capability passthrough** (M/L): P13.3.3 deferred pending ACP-host usage.
- **P13.4 — Nebula-inspired engagement tooling** (M): "interesting, not urgent" per its own scoping.
- **P9.4 — Per-task model routing** (M) and **P6.1 — Mid-turn state persistence** (L): no concrete trigger; check with user before starting.
- **P22.5 — `/side` ephemeral conversation** (S/M) and **P22.6 — Raw scrollback mode** (S/M): polish without demand.
- **P15.3–P15.11 (minus P15.2, covered in Tier 3)** — real scope, but either dependent on P15.2 or part of larger XL P15 initiative; sequence after Tier 1–3 land.

---

## Open Work — P21 (TUI Polish — Tool-Call Cards)

P21.3 (streaming caret) shipped 2026-07-10.

**P21.2 — Tool-call cards (in-place updating block). [Tier 3]** Restructure tool calls and results as one addressable, updatable transcript item that updates in place (pending → ok/err). (M)

---

## Open Work — P15 (Web UI Parity with the TUI)

Bring `aegis ui` up to feature parity with the TUI. P15.1 (frontend architecture) and P15.12 (token-injection hardening) shipped 2026-07-06 and 2026-07-10.

**P15.2 — New daemon config-mutation endpoints. [Tier 3]** Backend gap: `GET/PATCH /config/sandbox`, `GET/PATCH /config/security`, `GET/PATCH /config/skills`, `POST /config/harden`, `GET /security/status`, `GET /security/baseline`, `POST /security/install`. Unblocks P15.6/P15.7. (M)

**P15.3–P15.11 — Frontend panels. [Tier 4]** Persona/model management, cost/token visibility, checkpoints/rewind, security scanning, skills/memory, debate/knowledge, multi-session lifecycle, approval persistence, non-technical-user framing. Frontend-only except P15.6/P15.7 depend on P15.2.

---

## Open Work — P22 (OpenAI Codex CLI evaluation — 2026-07-08)

Feature evaluation of Codex CLI. P22.1 (`/diff`), P22.2 (`/review`), and P22.4 (Ctrl+R history search) shipped 2026-07-08. P22.3 (Esc-Esc backtrack + `/fork`) shipped 2026-07-10.

**P22.5 — `/side` ephemeral side conversation. [Tier 4]** Quick side question in a throwaway context. (S/M)

**P22.6 — Raw scrollback mode. [Tier 4]** Plain text rendering + release alternate screen for native selection/scrollback. (S/M)

---

## Open Work — P20 (Odysseus Review: Research, Compare, Model Fit)

Feature evaluation of Odysseus self-hosted AI workspace. P20.1 (deep-research skill + `/research` command) shipped 2026-07-07.

**P20.2 — Blind model compare. [Tier 4]** Same prompt to two models side-by-side, identities hidden until vote, then reveal + optional synthesis. (M)

**P20.3 — Hardware-aware model recommendation. [Tier 4]** Detect hardware, curate/recommend local models, offer `ollama pull`, surface via `/models` command. (M)

---

## Open Work — P13 (Security & Capability Enhancements)

P13.1, P13.2, P13.5, P13.6, and P13.8 shipped 2026-07-05–06. P13.3.1 and P13.3.5 (terminal enhancements) shipped 2026-07-07. All remaining items are Tier 4.

**P13.3.2 — `@shell`/`@last` context token. [Tier 4]** Extend `@file`/`@image:` to inject last N lines of terminal output. (S)

**P13.3.3 — ACP `terminal/*` capability passthrough. [Tier 4]** Let ACP hosts (Zed) supply pty for agent shell calls. (M/L)

**P13.4 — Nebula-inspired security engagement tooling. [Tier 4]** Engagement notebook + `security_advise` tool + CVE lookup + status digest + guarded next-step suggestions. (M)

---

## Open Work — P9 (Engineering Quality — lower urgency, no current trigger)

P9.1, P9.2, and P9.5 are shipped. P9.3 and P9.6 dropped 2026-07-05.

**P9.4 — Per-task model routing. [Tier 4]** Pick cheaper model for simple turns, reserve expensive for hard ones. No evidence of demand. (M)

---

## Open Work — P6 (Long-Horizon / Exploratory)

P6.3 (MCP server mode) and P6.2 (A2A protocol) shipped/dropped 2026-07-05–06.

**P6.1 — Mid-turn state persistence. [Tier 4]** Persist partial turn state (text, tool calls) to SQLite during streaming. High complexity, low-probability failure mode. (L)

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
