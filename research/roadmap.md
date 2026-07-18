# Aegis Capability Roadmap

**Last updated:** 2026-07-17 (P33.21, P33.22)

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 1 — parked (**P25.9**). Tiers 1-3 are fully clear; Tier 4 has just the one parked
item below. For the shipped-batch history and the lessons drawn from each (P33/P34 diagnosis
accuracy, threat-model closure surfaces, live-verification findings), see
[releases.md](releases.md#latest-changes) — that history has been consolidated there so this
document stays limited to what's actually open.

There is currently no in-progress next batch. Since P25.9 is parked with no active trigger,
starting new work here means either picking a fresh assessment pass or waiting on a concrete
trigger to surface (see the item below).

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

**Status:** None open. (P34.12 shipped 2026-07-17 — see [releases.md](releases.md#latest-changes);
P34.9 and P34.10 shipped 2026-07-17; P34.5-P34.8 shipped 2026-07-17; P34.3 shipped
2026-07-16; P34.2 shipped 2026-07-16, both levers; P34.1 shipped 2026-07-16; P34.4 shipped
2026-07-16; P33.13, P33.14, P33.15, P33.17, P33.18 shipped 2026-07-16; P33.3-P33.8 shipped
2026-07-15; P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14; P32.5-P32.7 shipped 2026-07-15.)

Worth a look for a future item: the same "accurate refusal, error-shaped" question for the other
scanners' documented exit codes. P34.6 checked the *language*-targeted tools; nothing has swept
the SCA/secrets tools for non-zero exits that mean "nothing to do" rather than "I broke".

---

## Open Work — Tier 3

**Status:** None open. (P33.10, P33.11, P33.16, P33.19 shipped 2026-07-17 — see
[releases.md](releases.md#latest-changes); P32.8 shipped 2026-07-15; P33.9, the keystone that
unblocked P33.10 and P33.19, shipped 2026-07-16.)

---

## Open Work — Tier 4

One item parked — P25.9. Low urgency, no trigger, or explicitly parked pending demand. Do not
build speculatively — revisit only if a concrete trigger appears, and check with the user before
starting.
(P33.20 shipped 2026-07-17 alongside P33.11 — its message-allowlist fix was implemented as part of
that work; P32.9-P32.11 shipped 2026-07-15; P33.12, P33.21, and P33.22 shipped 2026-07-17, see
releases.md.)

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
