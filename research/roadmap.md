# Aegis Capability Roadmap

**Last updated:** 2026-07-15

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 7 (P32.2-P32.8), plus 3 parked (P32.9-P32.11, Tier 4). A full objective
application review on 2026-07-15 — four parallel passes covering (1) engine/tool/permission/sandbox,
(2) persona/skills/swarm/debate/mcp/mcpserver/acp, (3) server/session/provider/guard/config/cron,
and (4) tui/client/memory/cli — surfaced these (originally 8 open, P32.1-P32.8); see
[Open Work](#open-work) and [Parked](#open-work--parked-tier-4) for the full list. **P32.1** shipped
2026-07-15 — see [releases.md](releases.md#latest-changes). Two cross-cutting patterns came out of
that review, noted here since they span multiple items: **(a)** tools that dynamically reclassify
their own capability create seams where a gate written against the static/declared capability
misses the reclassification — root cause of both P32.1 and P32.2; **(b)** several persistence
layers (checkpoints, `bg_events`, memory) were each built without a shared "how does this get
cleaned up" convention — root cause of P32.3 and P32.8, worth scoping as one retention pass rather
than point fixes. The P30 batch (code-gap scan) and P31 batch (CodeQL alerts) both closed out
2026-07-14 — see [releases.md](releases.md#latest-changes). The P27 threat model's
needs-verification list remains fully closed.

**Next session:** start with **P32.3** (Tier 1, checkpoint/bg_events cleanup — unbounded disk
growth in a shipping feature), then batch **P32.2** and **P32.4** as one "dynamic capability /
resource-bound consistency" pass since they share root cause (a) above. Tier 2 items (P32.5-P32.7)
are cheap, no-dependency wins to fit in alongside. Re-run `TestLiveWorkflow` (recipe in CLAUDE.md)
after any change touching the engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor`
is the standalone preflight companion for the same misconfiguration classes.

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** P32.2, P32.3, P32.4. (P31.1, P31.2, P30.1, P30.2, and P30.3 shipped 2026-07-14; P32.1
shipped 2026-07-15.)

**Tier 2:** P32.5, P32.6, P32.7. (P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14.)

**Tier 3:** P32.8.

**Tier 4:** parked — P25.9, P32.9, P32.10, P32.11. See [Parked](#open-work--parked-tier-4).

---

## Open Work

### P32.2 — `ContextualGate.Check` reads static capability instead of effective capability

Priority: Tier 1 · Effort: S — no dependency, same root cause as P32.1

`internal/permission/contextual.go:105` calls `t.Capability()` where the base `permission.Gate`
(`permission.go:130`) and `engine.serializeTool` (`engine.go:1104`) both correctly call
`tool.EffectiveCapability`. Currently harmless only because the sole `CapabilityOverrider`
(shell) never downgrades into `CapWrite`/`CapNetwork` — the two capabilities `ContextualGate`
gates (egress-then-write, network allowlist). Latent trap: the next tool that narrows into/out of
write or network via `CapabilityFor` would silently bypass those rules. Fix: call
`tool.EffectiveCapability(t, input)` for consistency with the two call sites that already got
this right.

### P32.3 — Session cleanup never removes checkpoint snapshots or `bg_events` rows

Priority: Tier 1 · Effort: S — no dependency

`internal/session/session.go`'s `Store.Delete` (:721-738) and TTL-based `Store.Prune` (:566-592)
never call `checkpoints.DeleteForSession` — only the HTTP `handleDeleteSession` handler
(`internal/server/sessions.go:286`) does, a separate code path. The daemon's TTL auto-pruner
(`server.go:1109-1132`) and the `/sessions/prune` endpoint (`sessions.go:833-850`) both call
`Store.Prune` directly, bypassing checkpoint cleanup entirely. Checkpoint snapshots (full pre-edit
file contents) can be up to 16MiB each (`maxSnapshotBytes`, `checkpoint.go:29`) with no count cap,
and `bg_events` rows (buffered SSE events for background sessions, `session.go:142,602-627`) are
never deleted by any code path. The feature specifically built to bound DB growth
(`cleanup.session_ttl_days`) silently leaves its largest data behind forever. Fix: wire
`checkpoints.DeleteForSession` into `Store.Delete`/`Store.Prune` directly (not just the HTTP
handler), and add `bg_events` cleanup to the same path; consider a `checkpoint.Store`
prune/TTL backstop of its own.

### P32.4 — Debate `max_rounds` and swarm spawn breadth are unclamped

Priority: Tier 1 · Effort: S — no dependency

The `agent` tool's debate mode has no maximum on `max_rounds` at any entry point — the JSON
schema (`internal/tool/builtin/agent.go:169`), the HTTP `DebateRequest.MaxRounds`
(`internal/server/debate.go:61`), and `executeDebate`'s own context timeout (`agent.go:493`,
`maxAgentDuration*(2*maxRounds+2)`) all scale with the value rather than bounding it. Each round
spawns 2 sub-agents via `swarm.Backend.Spawn` (real OS processes in subprocess mode). The only
brake, `budgetExhausted` (`internal/debate/debate.go:220`), silently no-ops if `cost.Tracker`
isn't in context (`agent.go:478`: `tracker, _ := ...`), and even when present is checked only once
per round-start, so overshoot scales with round count. A model turn steered by prompt-injected
file content (debate claims can load via `WithFiles`) could request an arbitrarily large round
count. The same unclamped-breadth shape applies separately to parallel subprocess `agent` tool
calls in one turn — no cap on concurrent spawn count at a given recursion depth (depth itself is
capped at 3 via `maxSpawnDepth`, `agent.go:24`). Fix: clamp `max_rounds` to a small hard ceiling
(e.g. 10) at the schema/handler boundary regardless of tracker presence, and consider a
concurrent-spawn-count cap alongside the existing depth cap.

### P32.5 — Two Windows shell-out sites missed the P30.2/P30.3 hardening sweep

Priority: Tier 2 · Effort: S — no dependency

`internal/notify/notify.go:135` and `internal/tui/clipboard_image.go:45` both still hardcode
`exec.Command("powershell", ...)` instead of `sandbox.WindowsShellBinary()` (which prefers `pwsh`,
fixing the Desktop/Core module-autoload gap documented at `internal/sandbox/sandbox.go:43-54`).
`hooks/exec.go:190-195`, `tui/tui.go:604-609`, and `security/install.go:98-103` were all correctly
updated in the P30 batch. Not currently exploitable — no attacker-controlled input reaches either
site (toast notification text, clipboard paste script) — but it's the same class of gap that
batch was explicitly sweeping for. Fix: swap both call sites to `sandbox.WindowsShellBinary()`.

### P32.6 — Output guard silently drops file coverage for non-standard write-tool input shapes

Priority: Tier 2 · Effort: S — no dependency

`writtenPathsFromInput` (`internal/engine/engine.go:1261-1285`) only recognizes `path`,
`file_path`, and `edits[].path`. Any MCP tool or future builtin write tool using a different field
name gets no output-guard file validation and no quarantine-on-fail checkpoint rollback, with no
log line marking the miss — the guard's core value (validating actual written content, not just
the chat summary) silently degrades to chat-text-only. Fix: at minimum log a warning when a
write-capability tool call has zero paths extracted, so the gap is visible instead of silent;
consider a more general extraction (e.g. tool-declared path-field names) longer term.

### P32.7 — `skills.Discover` re-walks and re-parses every skill file with no cache

Priority: Tier 2 · Effort: S — no dependency

`persona.Refresh` short-circuits via a `dirSignature` mtime/size fingerprint before doing a full
rescan (`internal/persona/load.go:80-89`). `skills.Discover` has no equivalent: `BuildIndex`/
`InjectIntoSystem` (`internal/skills/skills.go:327,361`) call `Discover` fresh on every
session-start / system-prompt build, doing 1-3 `os.ReadDir` calls plus a full read+parse of every
skill file, including a `filepath.WalkDir` of each bundled skill directory for its asset manifest
(`withAssetManifest`, `skills.go:198`). For a project with several bundled skills this is a
repeated full-tree walk per turn/session. Fix: apply the same mtime-signature short-circuit
pattern already proven on the persona side.

### P32.8 — `memory.md` has no total-size cap or pruning path

Priority: Tier 3 · Effort: M — larger, needs a retention design

`internal/memory/memory.go`'s `Append` (:126-156) caps a single entry (`maxMemoryEntry` = 4096B)
but nothing bounds total file size or entry count. A long-running project/user memory accumulates
entries forever, growing system-prompt injection cost every session and slowing `LoadRelevant`'s
per-entry TF-IDF scan linearly. No rotation, LRU-by-relevance trim, or periodic summarization
exists. Larger than the Tier 1/2 items because a real fix needs a retention policy decision (hard
cap + eviction order, or periodic summarization), not just a wiring fix — worth scoping together
with P32.3's checkpoint/`bg_events` cleanup gap as one "persistence retention" pass, since both
are instances of the same missing-lifecycle-policy pattern.

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
