# Aegis Capability Roadmap

**Last updated:** 2026-07-15

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 0, and the Tier 4 parked list is now empty too — **P32.9-P32.11** shipped
2026-07-15 at the user's explicit request (parked items are skipped only absent a trigger; a direct
user ask to fix them is one). A full objective application review on 2026-07-15 — four parallel
passes covering (1) engine/tool/permission/sandbox, (2) persona/skills/swarm/debate/mcp/mcpserver/acp,
(3) server/session/provider/guard/config/cron, and (4) tui/client/memory/cli — surfaced 8 open items
(P32.1-P32.8), all now shipped; see [releases.md](releases.md#latest-changes) for the full list.
**P32.1** shipped 2026-07-15; **P32.2-P32.7** (all of Tier 1 and Tier 2) shipped the same day;
**P32.8** (the sole Tier 3 item) shipped the same day after a retention-policy decision (FIFO
eviction, picked by the user over LRU-by-relevance or periodic summarization). Two cross-cutting
patterns came out of the review,
noted here since they span multiple items: **(a)** tools that dynamically reclassify their own
capability create seams where a gate written against the static/declared capability misses the
reclassification — root cause of both P32.1 and P32.2, both now fixed; **(b)** several persistence
layers (checkpoints, `bg_events`, memory) were each built without a shared "how does this get
cleaned up" convention — root cause of P32.3 and P32.8, both now fixed (checkpoints/`bg_events` via
a wiring fix, memory via a size-cap-plus-eviction retention policy). The P30 batch (code-gap scan)
and P31 batch (CodeQL alerts) both closed out 2026-07-14 — see
[releases.md](releases.md#latest-changes). The P27 threat model's needs-verification list remains
fully closed.

**Next session:** the open backlog is empty — only one Tier 4 parked item remains (P25.9), and
it's explicitly not to be built speculatively; check with the user before starting it. Re-run
`TestLiveWorkflow` (recipe in CLAUDE.md) after any change
touching the engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` is the standalone
preflight companion for the same misconfiguration classes.

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** none open. (P31.1, P31.2, P30.1, P30.2, and P30.3 shipped 2026-07-14; P32.1-P32.4
shipped 2026-07-15.)

**Tier 2:** none open. (P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14; P32.5-P32.7 shipped
2026-07-15.)

**Tier 3:** none open. (P32.8 shipped 2026-07-15.)

**Tier 4:** parked — P25.9. See [Parked](#open-work--parked-tier-4). (P32.9-P32.11 shipped
2026-07-15.)

---

## Open Work

None. See [releases.md](releases.md#latest-changes) for what shipped most recently.

---

## Open Work — Parked (Tier 4)

Low urgency, no trigger, or explicitly parked pending demand. Do not build speculatively —
revisit only if a concrete trigger (user demand, reported pain, incident) appears, and check
with the user before starting any of these.

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
