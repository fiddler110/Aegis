# Aegis Capability Roadmap

**Last updated:** 2026-07-15

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 1 (P32.8), plus 3 parked (P32.9-P32.11, Tier 4). A full objective application
review on 2026-07-15 — four parallel passes covering (1) engine/tool/permission/sandbox, (2)
persona/skills/swarm/debate/mcp/mcpserver/acp, (3) server/session/provider/guard/config/cron, and
(4) tui/client/memory/cli — surfaced these (originally 8 open, P32.1-P32.8); see
[Open Work](#open-work) and [Parked](#open-work--parked-tier-4) for the full list. **P32.1** shipped
2026-07-15; **P32.2-P32.7** (all of Tier 1 and Tier 2) shipped the same day — see
[releases.md](releases.md#latest-changes). Two cross-cutting patterns came out of the review, noted
here since they span multiple items: **(a)** tools that dynamically reclassify their own capability
create seams where a gate written against the static/declared capability misses the
reclassification — root cause of both P32.1 and P32.2, both now fixed; **(b)** several persistence
layers (checkpoints, `bg_events`, memory) were each built without a shared "how does this get
cleaned up" convention — root cause of P32.3 (fixed) and P32.8 (still open, needs a retention
design rather than a wiring fix). The P30 batch (code-gap scan) and P31 batch (CodeQL alerts) both
closed out 2026-07-14 — see [releases.md](releases.md#latest-changes). The P27 threat model's
needs-verification list remains fully closed.

**Next session:** only **P32.8** remains open, and it's Tier 3 (needs a retention-policy decision,
not just a wiring fix) — check with the user before starting given the Tier 4 parking convention's
"check with the user" norm extends naturally here once the cheap/no-dependency backlog is empty.
Re-run `TestLiveWorkflow` (recipe in CLAUDE.md) after any change touching the
engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` is the standalone preflight
companion for the same misconfiguration classes.

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

**Tier 3:** P32.8.

**Tier 4:** parked — P25.9, P32.9, P32.10, P32.11. See [Parked](#open-work--parked-tier-4).

---

## Open Work

### P32.8 — `memory.md` has no total-size cap or pruning path

Priority: Tier 3 · Effort: M — larger, needs a retention design

`internal/memory/memory.go`'s `Append` (:126-156) caps a single entry (`maxMemoryEntry` = 4096B)
but nothing bounds total file size or entry count. A long-running project/user memory accumulates
entries forever, growing system-prompt injection cost every session and slowing `LoadRelevant`'s
per-entry TF-IDF scan linearly. No rotation, LRU-by-relevance trim, or periodic summarization
exists. Larger than the Tier 1/2 items because a real fix needs a retention policy decision (hard
cap + eviction order, or periodic summarization), not just a wiring fix — the same
missing-lifecycle-policy pattern P32.3 (checkpoint/`bg_events` cleanup, shipped 2026-07-15) was an
instance of, but unlike P32.3 this one can't be closed with a wiring fix alone.

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

### P32.9 — Persona and skills frontmatter parsers have diverged

Priority: Tier 4 · Effort: M — parked, no concrete trigger

`internal/persona/load.go:314`'s `splitFrontmatter` uses real YAML; `internal/skills/skills.go:270`'s
`splitFrontmatter` is a hand-rolled key:value line parser that can't handle multi-line or nested
values. Not a bug today — skills frontmatter is only `name`/`description` scalars — but it's two
parsers maintaining one "leading `---` block" convention, and a latent trap if skills frontmatter
ever grows a structured field (e.g. a list-valued field the way personas declare `tools: [...]`).
Trigger: skills frontmatter gains a non-scalar field.

### P32.10 — CSRF cookie `Secure` flag doesn't account for reverse-proxy TLS termination

Priority: Tier 4 · Effort: S — parked, no concrete trigger

The CSRF cookie's `Secure` flag is derived from `r.TLS != nil` on the daemon's own connection
(`internal/server/webui.go:84`) — correct for the built-in TLS listener (`s.tlsCert`), but if the
daemon were ever run behind a reverse proxy that terminates TLS and forwards plaintext to the
loopback daemon, `r.TLS` is nil on the backend hop even though the browser used HTTPS, so `Secure`
is (safely, conservatively) left off rather than protecting that deployment shape. Fails safe
(cookie still sent, not dropped) and not exploitable as-is since the cookie only backs a
same-origin double-submit check, not the bearer token. Trigger: reverse-proxy deployment becomes a
documented/supported pattern — until then, not worth a `X-Forwarded-Proto`-trusting change that
would need its own trust-boundary reasoning.

### P32.11 — Anthropic/OpenAI provider adapters share no helper package

Priority: Tier 4 · Effort: L — parked, no concrete trigger

`internal/provider/anthropic` (~500 lines) and `internal/provider/openai` (~407 lines) share
nothing beyond the common `provider.Adapter` interface and message types — no confirmed
duplicate-logic audit was done, but SSE parsing, retry/backoff, and usage accounting are likely
reimplemented per-adapter. The engine-side seam is clean (no Anthropic-specific concepts like
thinking blocks or cache_control leak into `internal/engine` — it only depends on normalized
`provider.Message`/`Block` types), so this is a maintenance-cost question, not a correctness one.
Trigger: adapter maintenance burden becomes a concrete pain point (e.g. a bug fixed in one adapter
but not mirrored in the other).

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
