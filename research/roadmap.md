# Aegis Capability Roadmap

**Last updated:** 2026-07-18 (P35.1-P35.3 shipped; P35.4 scope narrowed)

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 2 — **P35.4** (scope narrowed 2026-07-18, below) plus the one parked item,
**P25.9**. P35.1-P35.4 were all filed 2026-07-18 from the same live dogfooding pass: running the
threat-modeling skill's `/threat-model stride` flow against an external repo (a small ~15-file
Python project, not this one) on the local-model setup `aegis doctor` itself recommends
(Ollama, qwen3.6:35b-a3b-fast). The run never produced a completed threat-model suite — it died
partway through the mandatory workspace-exploration step every time, for four distinct, stacked
reasons. **P35.1-P35.3 shipped 2026-07-18** (the three surface-cleanly-then-fix items); P35.4's
provider-side incremental context reuse — the one genuinely large piece — is the only new work
left, and it was always sequenced to land last. For the shipped-batch history and the lessons
drawn from each (P33/P34 diagnosis accuracy, threat-model closure surfaces, live-verification
findings), see [releases.md](releases.md#latest-changes) — that history has been consolidated
there so this document stays limited to what's actually open.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.  
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.  
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).  
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** 0 open. (P35.1, P35.2 shipped 2026-07-18 — see
[releases.md](releases.md#latest-changes); P33.1 and P33.2 shipped 2026-07-15;
P31.1, P31.2, P30.1-P30.3 shipped 2026-07-14; P32.1-P32.4 shipped 2026-07-15.)

---

## Open Work — Tier 2

**Status:** 0 open. (P35.3 shipped 2026-07-18; P34.12 shipped 2026-07-17 — see
[releases.md](releases.md#latest-changes); P34.9 and P34.10 shipped 2026-07-17; P34.5-P34.8
shipped 2026-07-17; P34.3 shipped 2026-07-16; P34.2 shipped 2026-07-16, both levers; P34.1
shipped 2026-07-16; P34.4 shipped 2026-07-16; P33.13, P33.14, P33.15, P33.17, P33.18 shipped
2026-07-16; P33.3-P33.8 shipped 2026-07-15; P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14;
P32.5-P32.7 shipped 2026-07-15.)

Worth a look for a future item: the same "accurate refusal, error-shaped" question for the other
scanners' documented exit codes. P34.6 checked the *language*-targeted tools; nothing has swept
the SCA/secrets tools for non-zero exits that mean "nothing to do" rather than "I broke".

---

## Open Work — Tier 3

**Status:** 1 open — P35.4 (scope narrowed 2026-07-18; **this is the next work item**). (P33.10,
P33.11, P33.16, P33.19 shipped 2026-07-17 — see [releases.md](releases.md#latest-changes); P32.8
shipped 2026-07-15; P33.9, the keystone that unblocked P33.10 and P33.19, shipped 2026-07-16.)

### P35.4 — No incremental context reuse across turns makes long skill runs cost-prohibitive on local models

Effort: L — the largest of the P35 set; the surface-cleanly-first items (P35.1-P35.3) that were
sequenced ahead of it have now shipped, so failures during this work will at least be legible.

**Scope narrowed 2026-07-18.** The filing proposed two fixes — provider-side incremental context
handling, and/or skill-level guidance to read large files in bounded excerpts. The second, cheaper
half **shipped 2026-07-18**: the threat-modeling skill's §2 workspace-exploration step now tells
the model to page large files with `read_file` `offset`/`limit` or targeted `grep` rather than
whole-file reads, since one ~100KB single-file read ate roughly half a 65536-token budget by
itself. **What remains — and is the actual next work item — is the provider-side half:** genuine
incremental context handling so unchanged conversation history isn't reprocessed from scratch
every turn.

Confirmed in llama-server's own log during the live threat-modeling dogfooding run: every
additional tool round trip reprocesses the *entire* conversation — no incremental
KV-cache/prompt-cache reuse across turns — so per-turn cost keeps growing with total conversation
length instead of paying only for the newly-added tokens. By the run's 15th turn, a single
prompt-processing pass took over three minutes on its own, before any response generation.
Together with the whole-file reads (now mitigated at the skill level), this made completing a full
seven-file threat-model suite unreachable in a single session against even a small (~15-file) real
repo, on a local 35B model, despite `context_window` already raised past P35.3's now-calibrated
recommendation. Trigger already exists (this dogfooding run reproduced it end-to-end). Next step:
scope the provider-adapter change (prompt-cache/KV reuse across turns for the Ollama native path,
where the cost was measured) and confirm whether the skill-level mitigation alone already makes
small-repo suites completable before committing to the larger adapter work.

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
