# Aegis Capability Roadmap

**Last updated:** 2026-07-11 — Tier 3's remaining findings, **P24.16 and P24.17, shipped in
parallel via isolated git-worktree sub-agents** alongside P24.18 (same pattern as the 2026-07-11
P24.11/P24.12/P24.13 batch and the 2026-07-10 P15.2/P21.2/P24.10 batch). Full-repo STRIDE-A threat
model (`threat-model-20260710-173718/`) findings now stand at 15 of 17 actionable findings shipped
(P24.1–P24.13, P24.16, P24.17); 2 remain open as P24.18–P24.21 (plus the parked P24.22), tiered by
severity/effort/dependency alongside the existing tracks. Remaining Tier 3 work is P24.18 (security
finding), also shipped the same day in its own worktree sub-agent — see below.

This document tracks only **open** work and what's next. For shipped-feature history and full design rationale, see [releases.md](releases.md). Recent shipped items: P24.16/P24.17 (Tier 3 third batch, 2026-07-11), P24.11–P24.13 (Tier 3 second batch, 2026-07-11), P15.2/P21.2/P24.10 (Tier 3 first batch, 2026-07-10), P24.5–P24.9 (threat-model Tier 2 findings, 2026-07-10), P24.1–P24.4 (threat-model Tier 1 findings, 2026-07-10), FIND-04/FIND-08 (threat-model quick fixes, 2026-07-10), P21.3/P22.3 (Tier 2 high-visibility wins, 2026-07-10), P21.5/P21.6/P15.12 (Tier 1 security/robustness, 2026-07-10), P22.1–P22.4 (CLI features, 2026-07-08), P21.1/P21.4/P21.7 (TUI polish, 2026-07-07), P20.1 (deep-research skill, 2026-07-07), P18–P19/P17/P16 (TUI/streaming/polish, 2026-07-07), P13.1/P13.2/P13.5/P13.6/P13.7/P13.8 (security/capability, 2026-07-06), P23 (Ollama context-window detection, 2026-07-08).

---

## Status

**Open items:** P24.18–P24.22 (threat-model findings), P15.3–P15.11, P22.5/P22.6, P20.2–P20.3, P13.3.2–P13.3.3/P13.4, P9.4, P6.1. See [Priority Order](#priority-order) below for what's next.

**Priority order:** see the tiered breakdown immediately below — it is the authoritative "what's next" view, ordered by tier and effort.

---

## Priority Order

Reorganized 2026-07-10 to answer "what's next" directly, cutting across the P-number tracks below.
Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, high-visibility user-facing wins with no dependency. **Tier 3** =
real value but larger or sequence-dependent (blocks or is blocked by other work). **Tier 4** = low
urgency, no concrete trigger, or explicitly parked pending demand — do not build speculatively.

### Tier 1 — Security & robustness, do next

Repopulated 2026-07-10 from the STRIDE-A threat model's Critical/Important findings; **all five
items (plus the two Tier-1-caliber quick wins from the same report) are now shipped** — struck
through below for the record. See [releases.md](releases.md#latest-changes) for the full writeup
of each. Tier 1 is empty; next up is [Tier 2](#tier-2--cheap-high-visibility-wins).

- ~~**FIND-04 — `web_fetch`/`web_search` untrusted-content marker** (S).~~ **SHIPPED 2026-07-10.**
- ~~**FIND-08 — `server.addr` non-loopback validation** (S).~~ **SHIPPED 2026-07-10.**
- ~~**P21.5 — Daemon resource ceilings** (S/M).~~ **SHIPPED 2026-07-10.**
- ~~**P15.12 — Harden the `/ui` token-injection mechanism** (S/M, security).~~ **SHIPPED 2026-07-10.**
- ~~**P21.6 — MCP tool output trust boundary** (S, security).~~ **SHIPPED 2026-07-10.**
- ~~**P24.1 — FIND-01: harden the `/ui` page-token flow so only the browser that loaded the page
  can redeem it** (M, security, **Critical**, CVSS 8.2).~~ **SHIPPED 2026-07-10.** Double-submit
  CSRF nonce (`HttpOnly` cookie + `data-csrf-token`/`X-Aegis-CSRF` header) binds `/auth/exchange`
  to the page that actually loaded `/ui`; closes the cross-origin-webpage attack, documented
  residual risk for a raw co-located process with direct HTTP access.
- ~~**P24.2 — FIND-02: authenticate `aegis mcp-serve` and the ACP server** (M, security, Important,
  CVSS 7.8).~~ **SHIPPED 2026-07-10.** `aegis acp` implements ACP's real `authenticate` method;
  `aegis mcp-serve` gets an equivalent `aegis/authenticate` extension gating `tools/call`. Both
  opt-in via `AEGIS_ACP_TOKEN`/`AEGIS_MCP_TOKEN`, no-op (today's behavior) when unset.
- ~~**P24.3 — FIND-03: gate cron firings through the same PermissionGate/contextual-gate stack as
  interactive tool calls** (M, security, Important, CVSS 7.1).~~ **SHIPPED 2026-07-10.** Fire-time
  mode check (plan blocks, build requires a new per-job `auto_approve` opt-in, auto unchanged)
  replaces the previous unconditional exec.
- ~~**P24.4 — FIND-05: wrap persona/skill `.md` body content in an untrusted-provenance marker**
  (M, security, Important, CVSS 6.9).~~ **SHIPPED 2026-07-10.** Reused `internal/trust.Wrap`
  (FIND-04's mechanism) for file-loaded (non-built-in) persona/skill bodies; scan left off (unlike
  MCP/web content) since this re-injects every session and persona/skill prose routinely discusses
  its own instructions, which the heuristic scan false-positives on.

### Tier 2 — Cheap, high-visibility wins

**All of Tier 2 shipped 2026-07-10** — struck through for the record, all five in parallel via
isolated git-worktree sub-agents (same pattern as P21.3/P22.3). See
[releases.md](releases.md#latest-changes) for the full writeup of each. Tier 2 is empty; next up is
[Tier 3](#tier-3--larger-real-sequence-dependent).

- ~~**P21.3 — Streaming caret** (S).~~ **SHIPPED 2026-07-10.**
- ~~**P22.3 — Esc-Esc backtrack + `/fork`** (M).~~ **SHIPPED 2026-07-10.**
- ~~**P24.5 — FIND-11: rate-limit/log repeated invalid bearer-token attempts** (S).~~ **SHIPPED
  2026-07-10.**
- ~~**P24.6 — FIND-13: scan GitHub PR titles/bodies for secret patterns before `gh pr create`**
  (S).~~ **SHIPPED 2026-07-10.**
- ~~**P24.7 — FIND-16: distinguish `OutputGuard` fail-open from a genuine pass in logs/metrics**
  (S).~~ **SHIPPED 2026-07-10.**
- ~~**P24.8 — FIND-31: audit `internal/security/install.go`'s installer-script argument
  construction** (S).~~ **SHIPPED 2026-07-10** (verification-only: audited, no unsanitized
  concatenation found, locked in with regression tests — see P24.22 below for one latent,
  currently-unreachable observation the audit surfaced).
- ~~**P24.9 — FIND-34: add a dedicated cron-execution audit log** (S).~~ **SHIPPED 2026-07-10.**

### Tier 3 — Larger, real, sequence-dependent
- ~~**P15.2 — New daemon config-mutation endpoints** (M).~~ **SHIPPED 2026-07-10.** Added
  `GET/PATCH /config/sandbox`, `/config/security`, `/config/skills`, `POST /config/harden`,
  `GET /security/status`, `GET /security/baseline`, `POST /security/install`; harden's
  cap-computation extracted into `config.ComputeHardenPlan`, shared by the CLI and the new
  endpoint. Unblocks P15.6/P15.7 (still Tier 4 — frontend panels not built yet).
- ~~**P21.2 — Tool-call cards** (M). In-place-updating tool-call blocks.~~ **SHIPPED 2026-07-10.**
  Added `ToolID` to `engine.Event`/`api.Event` (the missing correlation key for concurrent tool
  calls) and restructured TUI rendering from two static appended blocks into one addressable
  transcript item that updates pending -> ok/err in place.
- ~~**P24.10 — FIND-06: document Docker/Podman-socket privilege equivalence + default toward
  rootless/capability-dropping** (M, security, Moderate, CVSS 6.4).~~ **SHIPPED 2026-07-10.**
  Capability-dropping (`--cap-drop=ALL`/`--security-opt=no-new-privileges`) was already shipped;
  added a "Docker/Podman socket privilege equivalence" doc section (`docs/security_scan.md`)
  recommending rootless Podman/userns-remapped Docker, plus a one-time `SocketPrivilegeNotice` log
  line when the daemon selects a docker/podman backend.
- ~~**P24.11 — FIND-07: allowlist or TOFU-confirm the `lsp` tool's config-specified binary** (M,
  security, Moderate, CVSS 6.0).~~ **SHIPPED 2026-07-11.** Added a built-in allowlist of common LSP
  server binary basenames (`internal/lsp/trust.go`) plus an explicit per-server `lsp[].trust: true`
  config opt-in for anything else; `Manager.Start` now refuses to spawn an unrecognized,
  non-trusted command instead of exec'ing it unconditionally. Chose allowlist+opt-in over a
  persisted TOFU store since all configured LSP servers start eagerly at daemon boot, before any
  interactive approver exists — no live human to prompt at the point that matters.
- ~~**P24.12 — FIND-09: add an opt-in redaction/DLP pass for cloud-provider traffic** (M, security,
  Moderate, CVSS 5.2).~~ **SHIPPED 2026-07-11.** New opt-in `security.redact_secrets` config flag;
  when set, every successful read-capability tool result is run through a new
  `security.RedactText` (gitleaks-backed, reusing the FIND-13 secret-detection machinery) before it
  re-enters the model's context, masking any detected secret to `[REDACTED:<RuleID>]`. Fail-open
  (missing gitleaks or a scan error never blocks the tool call), off by default; `docs/providers.md`
  documents local-Ollama as the no-exposure alternative for sensitive codebases.
- ~~**P24.13 — FIND-10: strengthen or clearly caveat the MCP `scan_output` heuristic** (M, security,
  Moderate, CVSS 5.0).~~ **SHIPPED 2026-07-11.** `trust.ScanForInjection` now also matches against a
  zero-width/invisible-character-stripped copy of the content and against any base64-looking
  substring that decodes to valid UTF-8, catching two concrete encoding bypasses the heuristic
  previously missed entirely. `docs/mcp-trust-boundary.md`'s bypass list updated to reflect the new
  boundary (homoglyphs, translation, other encodings, and multi-call-split payloads still aren't
  caught); added a documented evaluate-and-defer writeup for a model-based classifier as the
  longer-term option, since it would add a real network/latency/cost dependency and a new
  attackable trust surface with no evidence the current heuristic is inadequate in practice.
- ~~**P24.16 — FIND-29: extend Windows DACL hardening beyond `daemon.token`** (M, security, Moderate,
  CVSS 4.9). Session DB, checkpoint snapshots, and `.aegis/.env` inherit ambient directory ACLs
  today.~~ **SHIPPED 2026-07-11.** Extracted the SDDL-based owner-only-DACL logic out of
  `internal/server/token_windows.go`/`token_other.go` into a new leaf package, `internal/fsguard`
  (`RestrictToOwner`, same windows/other build-tag split, same `"D:PAI(A;;FA;;;OW)"` SDDL string),
  so `internal/session` and `internal/config` can call it without an import cycle through
  `internal/server`; the old server-local copies were deleted and `auth.go`'s
  `generateAndWriteToken` now calls the shared one. `session.Open` hardens `sessions.db` plus its
  WAL-mode `-wal`/`-shm` sidecars after `migrate()` succeeds (checkpoint snapshots need no separate
  treatment — `internal/checkpoint` shares the same SQLite connection); a hardening failure on the
  main db file propagates as an `Open` error like `daemon.token`'s always has, while a sidecar
  failure (including "doesn't exist yet") only logs a warning. `config.loadDotEnv` applies the same
  hardening to `.aegis/.env` after a successful read, best-effort (warns, doesn't fail
  `config.Load()`) since that file is user-owned, not Aegis-written. `RestrictToOwner` itself treats
  a missing path as a no-op. New `internal/fsguard/fsguard_windows_test.go` reads the on-disk DACL
  back via `golang.org/x/sys/windows` and asserts exactly one ACE naming the owner-rights SID (not
  Everyone); cross-platform smoke/no-op tests added in `internal/fsguard/fsguard_test.go`,
  `internal/session/session_test.go`, and `internal/config/config_test.go`. `docs/configuration.md`
  documents the extended coverage.
- ~~**P24.17 — FIND-30: add integrity verification (hash-at-write, check-at-load) for memory
  files** (M, security, Moderate, CVSS 4.2).~~ **SHIPPED 2026-07-11.** Each memory file
  (`.aegis/memory.md` and the user-global `memory.md`) now has a sha256 sidecar
  (`<path>.integrity`), refreshed by `memory.Append` after every write. `Sources.loadDirect` hashes
  the file's current content at load time and compares it against the sidecar: a match loads
  silently; a mismatch prepends a visible `⚠️ integrity check failed` warning banner to that memory
  section (content is never dropped — a hand-edit may be intentional) and logs via `slog.Warn`; a
  missing sidecar (pre-existing `memory.md`, or first run) silently establishes a new trust baseline
  instead of false-positive-warning every upgrading user. Deliberately a plain hash, not a keyed
  MAC/signature — an adversary with write access to `memory.md` already has write access to any
  sidecar next to it, so a secret key wouldn't raise the bar.
- **P24.18 — FIND-32: offer optional TLS or a Unix-domain-socket/named-pipe transport for
  client↔daemon traffic** (M, security, Low, CVSS 3.3). Currently plaintext HTTP over loopback.
  Being shipped independently in its own worktree sub-agent alongside P24.16/P24.17 — see
  [releases.md](releases.md#latest-changes) once merged.

### Tier 4 — Parked / low priority / no current trigger
Do not build speculatively — revisit only if a concrete trigger (user demand, reported pain, incident) appears.
- **P20.2 — Blind model compare** (M) and **P20.3 — Hardware-aware model recommendation** (M): competitive-inspired, no direct reported pain.
- **P13.3.2 — `@shell`/`@last` context token** (S) and **P13.3.3 — ACP terminal capability passthrough** (M/L): P13.3.3 deferred pending ACP-host usage.
- **P13.4 — Nebula-inspired engagement tooling** (M): "interesting, not urgent" per its own scoping.
- **P9.4 — Per-task model routing** (M) and **P6.1 — Mid-turn state persistence** (L): no concrete trigger; check with user before starting.
- **P22.5 — `/side` ephemeral conversation** (S/M) and **P22.6 — Raw scrollback mode** (S/M): polish without demand.
- **P15.3–P15.11 (minus P15.2, covered in Tier 3)** — real scope, but either dependent on P15.2 or part of larger XL P15 initiative; sequence after Tier 1–3 land.
- **P24.14 — FIND-12: document (and consider an opt-in outbound redaction hook for) MCP tool-call
  argument content** (S/M, security, Moderate, CVSS 4.6). Depends on FIND-04/05 injection vectors
  actually being exploited to matter in practice; documentation alone covers most of the value.
- **P24.15 — FIND-14: give each swarm sub-agent a guaranteed minimum budget floor** (S, security,
  Low, CVSS 3.6). Real but low-severity fairness gap; no reported incident.
- **P24.19 — FIND-15: document that local-Ollama traffic is typically unencrypted** (S, doc-only,
  Low, CVSS 3.3). Root cause is Ollama's own default, not Aegis code.
- **P24.20 — FIND-17: strip/escape ANSI/OSC control sequences in streamed model output before TUI
  render** (S, security, Low, CVSS 3.0). Requires another injection vector to already be exploited
  to matter; still a cheap, self-contained hardening pass.
- **P24.21 — FIND-33: memory-lock/zero the bearer token in `Client` process memory** (M, security,
  Low, CVSS 2.8). Explicitly low priority per the finding itself — Host/OS access is already a
  significant compromise.
- **P24.22 — Quote/escape the `distro` argument in `sandbox.WSLInstallCommand`** (S, security,
  informational). Surfaced during P24.8's audit: `distro` is concatenated unquoted into the
  composed `wsl -d <distro> -- bash -lc ...` string, unlike `linuxCmd` which is quoted. Currently
  unreachable — `install.go`'s only call site hardcodes `""` for `distro`, ignoring
  `Options.WSLDistro`/`security.wsl_distro` entirely — so this is not a live vulnerability. Worth
  fixing defensively if `WSLInstallCommand` ever grows a second, config-driven caller.

---

## Open Work — P24 (Threat Model Findings — 2026-07-10)

Full-repo STRIDE-A threat model at commit `34aa687`:
[`threat-model-20260710-173718/`](../threat-model-20260710-173718/3-findings.md). 35 findings
total; 14 were "existing control" (already mitigated, verified, no action needed — FIND-18/19/20/
21/22/23/24/25/26/27/28/35), FIND-04/FIND-08 shipped same-day, and P24.1–P24.9 (Critical/Important
+ Tier 2 quick wins, below) shipped same-day too. P24.10–P24.13 (Tier 3 first and second batches)
shipped 2026-07-10/11; P24.16 and P24.17 (Tier 3 third batch) shipped 2026-07-11, in parallel with
P24.18 (still open, being merged from its own worktree sub-agent). 2 remain open as P24.18/
P24.19–P24.21 (plus P24.22, a new low-severity item P24.8's audit surfaced), grouped by the tier
they were slotted into above.

**Critical/Important (Tier 1): all shipped 2026-07-10.** See [releases.md](releases.md#latest-changes)
for what each one actually did — P24.1 (FIND-01, `/ui` page-token double-submit CSRF binding),
P24.2 (FIND-02, `aegis acp`/`aegis mcp-serve` shared-secret auth), P24.3 (FIND-03, cron fire-time
permission gate + per-job `auto_approve`), P24.4 (FIND-05, persona/skill untrusted-content wrap).

**Quick wins (Tier 2, Low effort): all shipped 2026-07-10.** See
[releases.md](releases.md#latest-changes) for what each one actually did — P24.5 (FIND-11, counter
+ `Warn` log on repeated invalid-bearer-token attempts), P24.6 (FIND-13, gitleaks-backed secret
scan over `git_pr`'s title/body before `gh pr create`), P24.7 (FIND-16, `guard.Status`
passed/failed/skipped_transport_error threaded from `LLMGuard` through to emitted `KindGuard`
events), P24.8 (FIND-31, audited — no unsanitized concatenation found, locked in with regression
tests; see P24.22 above for the one latent observation it surfaced), P24.9 (FIND-34, durable
`cron_runs` table + `cron_history` tool, independent of turn traces).

**Larger/sequence-dependent (Tier 3, Medium effort):** P24.10 shipped 2026-07-10; P24.11–P24.13
shipped 2026-07-11, all three in parallel via isolated git-worktree sub-agents; P24.16 and P24.17
shipped 2026-07-11, also in parallel via isolated git-worktree sub-agents alongside P24.18 (see
[releases.md](releases.md#latest-changes)).
- **P24.18 (FIND-32)** — Optional TLS (self-signed/pinned) or Unix-domain-socket/named-pipe
  transport for client↔daemon traffic.

**Parked (Tier 4 — low severity or doc-only, no current trigger):**
- **P24.14 (FIND-12)** — Document MCP tool-call argument data flow; consider opt-in outbound
  redaction hook symmetric to `scan_output`.
- **P24.15 (FIND-14)** — Per-agent minimum budget floor in `internal/swarm/subprocess.go`'s shared
  tracker.
- **P24.19 (FIND-15)** — Document that local-Ollama traffic is typically unencrypted plaintext.
- **P24.20 (FIND-17)** — Strip/escape ANSI/OSC control sequences in `internal/tui/streaming.go`'s
  render path.
- **P24.21 (FIND-33)** — Memory-lock/zero the bearer token in `Client`'s process memory where the
  platform supports it.

---

## Open Work — P21 (TUI Polish — Tool-Call Cards)

P21.3 (streaming caret) and P21.2 (tool-call cards) shipped 2026-07-10. Track closed — no open
items remain.

---

## Open Work — P15 (Web UI Parity with the TUI)

Bring `aegis ui` up to feature parity with the TUI. P15.1 (frontend architecture), P15.12 (token-injection hardening), and P15.2 (config-mutation endpoints) shipped 2026-07-06/2026-07-10/2026-07-10.

**P15.3–P15.11 — Frontend panels. [Tier 4]** Persona/model management, cost/token visibility, checkpoints/rewind, security scanning, skills/memory, debate/knowledge, multi-session lifecycle, approval persistence, non-technical-user framing. Frontend-only; the P15.6/P15.7 backend dependency on P15.2 is now unblocked.

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
