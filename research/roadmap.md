# Aegis Capability Roadmap

**Last updated:** 2026-07-14

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** none. All filed work through the P29 batch has shipped — see
[releases.md](releases.md#latest-changes) for what closed and why. Tier 4 has 1 parked item with
no active trigger (see [Parked](#open-work--parked-tier-4)). The P27 threat model's needs-verification
list is now fully closed — see [releases.md](releases.md#latest-changes) for the 2026-07-14
verification pass that confirmed the last two items (hook execution timing, cron fire-time rule
application) were already resolved by shipped mechanisms, no code change needed.

**Next session:** no open item queued — pick up new work from the next audit/eval pass, or from
user-reported gaps. Re-run `TestLiveWorkflow` (recipe in CLAUDE.md) after any change touching the
engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` is the standalone preflight
companion for the same misconfiguration classes.

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** none open.

**Tier 2:** none open.

**Tier 3:** none open.

**Tier 4:** parked — P25.9. See [Parked](#open-work--parked-tier-4).

---

## Open Work

None open. See [Parked](#open-work--parked-tier-4) for the Tier 4 backlog with no active trigger.

---

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

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
