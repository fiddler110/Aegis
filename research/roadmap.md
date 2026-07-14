# Aegis Capability Roadmap

**Last updated:** 2026-07-14 — **P28.5** (Tier 3, resumable web UI SSE stream) shipped, closing out
the entire P28 batch: all 7 items filed 2026-07-14 (**P28.1**–**P28.7**) are now shipped — see
[releases.md](releases.md#latest-changes) for the full writeups. All other completed history (the
full P27 threat-model batch, P27.1–P27.19, plus everything before it) also lives there.

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 6 new items, **P29.1**–**P29.6**, filed 2026-07-14 from a full docs-vs-implementation
gap evaluation (parallel audit of every docs/*.md file against the actual Go code in
internal/tool/builtin, internal/permission, internal/config, internal/provider). All 6 are doc
drift found in an otherwise tightly-maintained codebase — no missing features, just documentation
that no longer matches (or never matched) the real behavior. See [Open Work](#open-work) below.

The 2026-07-14 P28 batch (all 7 items filed that day from a full interactive evaluation of the TUI,
web UI, and daemon against three live Ollama models) has separately shipped in full — see
[releases.md](releases.md#latest-changes). Tier 4 has 4 parked items with no active trigger (see
[Parked](#open-work--parked-tier-4)), plus 2 remaining unresolved needs-verification notes carried
over from the P27 threat model (see below); the third (TUI escape-sequence neutralization) is
resolved by P28.1.

**Next session:** start with **P29.1** (trivial, one-line doc fix, no dependency) — see
[Open Work](#open-work). Re-run `TestLiveWorkflow` (recipe in CLAUDE.md) after any change touching
the engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` (P26.1) is the standalone
preflight companion for the same misconfiguration classes (including the workspace trust check,
P27.1, the local-sandbox recommendation, P27.14, and the tool-calling smoke test, P28.2).

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** none open — P28.1 shipped 2026-07-14, see [releases.md](releases.md#latest-changes).

**Tier 2:** P29.1, P29.2, P29.3, P29.5, P29.6 open (all cheap, no-dependency doc fixes). P28.2,
P28.4, P28.6, P28.7 all shipped 2026-07-14, see [releases.md](releases.md#latest-changes).

**Tier 3:** P29.4 open (needs a decision: doc-only fix vs. actually wiring the env vars). P28.3 and
P28.5 both shipped 2026-07-14, see [releases.md](releases.md#latest-changes).

**Tier 4:** parked — P25.9, P13.3.3, P6.1, P27.20. See [Parked](#open-work--parked-tier-4).

---

## Needs-verification items (from the P27 threat model, still unresolved)

The P27 threat model (`threat-model-20260712-200318/`, commit `7230aaf`) flagged these as things
to check before or while implementing the related shipped item — not yet filed as their own
findings, and not confirmed resolved by any shipped writeup:

- Hook execution timing relative to any trust prompt (relevant to **P27.1**, the workspace-trust
  gate).
- Whether cron fire-time gating truly exercises text rules end-to-end or only the contextual gate
  (relevant to **P27.15**).

The third item from this list — whether the TUI fully neutralizes terminal escape sequences in
untrusted tool output — was checked 2026-07-14 by reading the actual render paths
(`internal/tui/tui.go`, `toolview.go`, `sanitize.go`, `ansi16.go`): it did not, and this was fixed
the same day as **P28.1** — see [releases.md](releases.md#latest-changes).

See `0-assessment.md`'s "Needs Verification" table in the report folder for the original context.

---

## Open Work

All 6 items below were filed 2026-07-14 from a full parallel audit comparing every docs/*.md file
against the actual implementation (internal/tool/builtin, internal/permission, internal/config,
internal/provider, internal/persona, internal/skills, internal/swarm, internal/debate,
internal/mcp, internal/mcpserver, internal/session, internal/memory). The persona/skills/swarm/
debate/MCP/memory slice of docs matched the code exactly — no items filed there. These 6 are all in
the permission/config/tools slice.

### P29.1 — Tool-reference doc names a tool that doesn't exist: `team_task_create` vs `team_task_add`

Priority: Tier 2 · Effort: S

`docs/tools-reference.md` documents the team-task-creation tool as `team_task_create`, but the
actually-registered tool name is `team_task_add` (`internal/tool/builtin/team.go:40`, confirmed in
`persona_test.go:218` and `tui/flavor.go:38`). A model following the docs literally calls a
nonexistent tool and fails. Fix: correct the doc to `team_task_add` — renaming the tool itself to
match the doc is not worth the churn (call sites, tests, flavor map all already agree on
`team_task_add`).

### P29.2 — Permission audit-trail docs describe a different mechanism than the real one

Priority: Tier 2 · Effort: S

`docs/permissions.md:150-165` documents per-session audit files at
`~/.local/share/aegis/audit/<session-id>.jsonl` with fields including `session_id`, `capability`,
and decision values `ask_approved`/`ask_denied`. The real audit trail is one **global** file
`<data_dir>/audit.jsonl` (`internal/server/server.go:628`) with a different schema
(`internal/hooks/hooks.go:67-82`) — no `session_id` field exists, and `ask_approved`/`ask_denied`
appear nowhere in the codebase. Fix: rewrite the docs section to match the real global-file schema.
(Per-session audit files could be a real feature request, but that's a separate, larger item — not
filed here since there's no expressed demand for it.)

### P29.3 — Default data directory documented incorrectly

Priority: Tier 2 · Effort: S

`docs/sessions.md:26` and `docs/configuration.md:745-746` claim `~/.local/share/aegis`
(macOS/Linux) / `%LocalAppData%\aegis` (Windows) as the default data directory. The actual
`defaultDataDir()` (`internal/config/config.go:874-890`) uses `~/.config/aegis` /
`%AppData%\aegis` — a different XDG category on both platforms. Fix: correct both doc files.

### P29.4 — `GROQ_API_KEY` / `OPENROUTER_API_KEY` documented as native, but never read

Priority: Tier 3 · Effort: S/M — needs a decision on which fix path, hence Tier 3 not Tier 2

`docs/configuration.md:56-61` lists `GROQ_API_KEY`/`OPENROUTER_API_KEY` as native provider env
vars; `config.ProviderAPIKey` (`internal/config/config.go:1157-1171`) only checks
`ANTHROPIC_API_KEY`/`OPENAI_API_KEY`/hardcoded `"ollama"` — Groq/OpenRouter only work today via
`OPENAI_API_KEY` reuse. Two fix paths: (a) cheap — correct the docs to say these providers work
through OpenAI-compatible base-URL + `OPENAI_API_KEY`, not their own named var (Effort S); (b)
larger — actually read the named env vars per-provider in `ProviderAPIKey` so the docs become true
(Effort M). Check with the user which is wanted before starting.

### P29.5 — Several working config keys are undocumented in configuration.md

Priority: Tier 2 · Effort: S

`provider.prompt_profile`, `security.wsl_distro`, `security.dast.allowed_targets`,
`security.dast.allow_active`, and `security.redact_secrets` (documented only in `providers.md`) are
implemented and functional but missing from `docs/configuration.md`'s reference tables. Fix: add
them to the main config reference.

### P29.6 — Config reference sample values don't match real defaults

Priority: Tier 2 · Effort: S

`docs/configuration.md`'s example config shows `tui.humor_mode: false` (actual built-in default is
`true`, `internal/config/config.go:857`) and `sandbox.backend: os` as if it were the default
(actual default is `local`, `internal/config/config.go:844` — `os` is only what `--first-init`'s
generated template writes). Fix: correct both sample values, or add a clarifying inline comment
next to each explaining sample vs. default.

---

For prior shipped work: the full 7-item P28 batch filed 2026-07-14 from a live interactive
evaluation of the TUI, web UI, and daemon — driving real sessions over the HTTP+SSE seam (the same
one the TUI/web UI use) against three live Ollama models via `TestLiveWorkflow`, plus a read of the
TUI render paths and web UI streaming client — has all shipped: **P28.1** (Tier 1, TUI
escape-sequence sanitization), all four Tier 2 items (**P28.2** local-model tool-calling guidance +
`aegis doctor` smoke test, **P28.4** compaction fallback on repeated summarizer failure, **P28.6**
`TestLiveWorkflow` harness-quality fix, **P28.7** persistent connection/model-health indicator), and
both Tier 3 items (**P28.3** engine nudge/retry on a zero-tool-call actionable turn, **P28.5**
resumable web UI SSE stream) — see [releases.md](releases.md#latest-changes) for the full writeups.

---

## Open Work — Parked (Tier 4)

Low urgency, no trigger, or explicitly parked pending demand. Do not build speculatively —
revisit only if a concrete trigger (user demand, reported pain, incident) appears, and check
with the user before starting any of these. **P27.20** is parked for a different reason than the
other three — not "no trigger" but "the finding's own remediation is explicitly optional."

### P25.9 — Per-session scoping of daemon-singleton services

Priority: Tier 4 · Effort: L — parked, no concrete trigger

The P25.1 deliberately-deferred gaps, tracked here so they aren't lost in releases.md prose:
`lsp.Manager`, `knowledge.Store`, `longmem.Store`, the cached repo-map (`s.repoMap`),
persona/command/agent-def directory discovery, and the `os` sandbox backend's write-confinement
profile (baked at construction; `resolveSessionWorkdir` warns once on the mismatch) all remain
scoped to the daemon's default workspace regardless of a session's Workdir. Each is a
daemon-wide singleton; re-scoping is a materially larger change. Trigger: a concrete pain point
in a future live-eval pass.

### P13.3.3 — ACP `terminal/*` capability passthrough

Priority: Tier 4 · Effort: M/L — parked, no concrete trigger (deferred pending ACP-host usage)

Let ACP hosts (Zed) supply a pty for agent shell calls.

### P6.1 — Mid-turn state persistence

Priority: Tier 4 · Effort: L — parked, no concrete trigger

Persist partial turn state (text, tool calls) to SQLite during streaming. High complexity,
low-probability failure mode. (Checked 2026-07-12: P25.5 added mid-turn token-usage accumulation
in memory, not persistence to SQLite — no overlap, no trigger.)

### P27.20 — FIND-18 (encryption half): optional at-rest encryption for SQLite stores

Priority: Tier 4 · Effort: M/L — parked, no concrete trigger beyond the finding's own suggestion

SQLCipher (or similar) at-rest encryption for the conversation/checkpoint SQLite stores, on top of
the ACL hardening already covered by P27.10 (Tier 2). Larger scope, opt-in, for higher-assurance
deployments — no reported pain driving it yet.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
