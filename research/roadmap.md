# Aegis Capability Roadmap

**Last updated:** 2026-07-13 — the P27 threat model's Tier 1 closed out: **P27.1** (workspace-trust
gate, FIND-01/FIND-02) and **P27.2** (`provider.base_url` allowlist/warn, FIND-03) both shipped.
**P27.1** adds `internal/workspacetrust` (a JSON store of per-directory trust decisions) and gates
`config.Load()` on it — until a directory is explicitly trusted via the new `aegis trust` command,
project-sourced `permission.*`, `sandbox.*`, `mcp.servers`, `notify.webhook`, and `hooks` are frozen
back to their user/global values (computed via a second, project-excluded koanf load compared
key-by-key against the merged one), with the diff surfaced through `cfg.WorkspaceTrust`, a startup
WARN (daemon log + TUI stderr banner), and a new `aegis doctor` check. The two existing first-party
project-config writers of a gated key — `config.PatchProjectSandbox` and
`config.AppendProjectPermissionRule` (the TUI's "allow always" approval option) — auto-trust the
directory they write to as a side effect, since that write is itself an explicit local operator
action, not something silently inherited from a cloned repo. **P27.2** adds
`providerfactory.validateBaseURL`: a non-loopback plaintext-HTTP `base_url` is refused outright when
a real API key would be attached (CWE-522), while a non-default HTTPS host for a cloud provider is
allowed through with a prominent startup WARN rather than a hard block, since legitimate
gateway/proxy setups are common. `go build ./...` and `go test ./...` pass clean. Next up: Tier 2
(P27.3, cheapest first) — see [Priority Order](#priority-order).

Before that, same day (2026-07-13): a fresh full-repo STRIDE-A threat model
(`threat-model-20260712-200318/`, commit `7230aaf`) folded in as new track **P27**: 20 findings, 76
threats across 32 components, 0 directly remote-exploitable (Aegis remains `LOCALHOST_SERVICE`).
Unlike the 2026-07-10 pass, every finding here carries a live remediation (no already-mitigated
no-ops) — this report audits a codebase that already absorbed the first pass's fixes. Filed as
**P27.1–P27.2** (Tier 1, Important severity — a workspace-trust gate for project-sourced
`hooks`/config, and a `provider.base_url` allowlist), **P27.3–P27.13** (Tier 2, 11 cheap
self-contained hardening items — mostly flipping existing opt-in controls to default-on or
extending an existing pattern to a new surface), **P27.14–P27.18** (Tier 3, 5 larger items), and
**P27.19–P27.20** (Tier 4, filed into the parked set — one's remediation is documentation-only,
the other is optional at-rest encryption with no concrete trigger). Full breakdown and rationale in
[P27](#open-work--p27-threat-model-findings--2026-07-13) below.

Before that, same day (2026-07-12): a second batch of four Tier 4 parked items promoted and shipped
on explicit user request: **P22.5** (`/side` ephemeral side conversation), **P22.6** (raw
scrollback mode), **P20.2** (`aegis compare` blind model compare), and **P20.3** (hardware-aware
model recommendation, `internal/hwinfo` + `aegis models --recommend`). **P25.9** and **P6.1** were
deliberately excluded from this batch — both Effort L, both high-blast-radius (daemon singleton
rescoping; core engine streaming loop) — better suited to focused solo work than parallel
automation; **P13.3.3** stays excluded as its precondition (ACP-host usage) still hasn't
materialized. Implemented in parallel by four isolated sub-agents in separate git worktrees; one
doc-only merge conflict (both P22.5 and P22.6 appended to the same `docs/tui-guide.md` table) was
resolved by combining both additions, no code conflicts. Full `go build ./...` and `go test ./...`
pass after merge. Full writeups in [releases.md](releases.md#latest-changes). Before this, same
day: four other Tier 4 items — **P24.21** (bearer-token scrubbing in `Client`), **P13.3.2**
(`@shell` terminal-output context token), **P9.4** (opt-in per-task model routing), and **P13.4**
(`security_advise` engagement-notebook/CVE-lookup/guarded-suggestions tool) — shipped as the first
batch (see [releases.md](releases.md#latest-changes) for that writeup too). Before that, same day:
**P26.2** (fixed a
`sessionWorkdirs`/`sessionSkills` map leak on session delete) shipped, closing out the 2026-07-12
routine roadmap review (competitive-landscape scan + internal code audit; no live-eval or
threat-model trigger that round). Landscape scan against Claude Code/Codex CLI/opencode/Gemini CLI
found nothing new since the 2026-07-02 review (convergent themes already closed; A2A and
Dispatch/Channels remain correctly declined). Before that, same day: **P15.13** (web UI session
workdir picker + display), closing out the entire 2026-07-11 roadmap review's promoted set; before
that, same day: **P26.1** (`aegis doctor` preflight self-diagnostic); before that, same day:
**P25.7** (live-eval harness promoted into `internal/eval`) and **P25.8** (session workdir threaded
through swarm/cron/debate), closing the P25 Tier 1 set; before that: P25.4-P25.6 (approval
ergonomics, token observability, local-model profile), P25.3 (output guard vs local/thinking
models), and P25.1-P25.2 (`6b76e5e`, 2026-07-11).

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** P27.3–P27.18 (2026-07-12 threat-model findings, Tiers 2–3 — see
[Priority Order](#priority-order)). Tier 1 (**P27.1**, **P27.2**) shipped 2026-07-13. Tier 4 is 5
items — the pre-existing P25.9/P13.3.3/P6.1 plus **P27.19**/**P27.20** (see
[Parked](#open-work--parked-tier-4)).

**Next session:** start at Tier 2 — 11 cheap, self-contained hardening items, **P27.3** first
(default `security.redact_secrets` on). Re-run `TestLiveWorkflow` (recipe in CLAUDE.md) after any
change touching the engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` (P26.1) is
the standalone preflight companion for the same misconfiguration classes (now including a workspace
trust check, P27.1).

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** empty — **P27.1** and **P27.2** shipped 2026-07-13 (see
[releases.md](releases.md#latest-changes)). P27.6, P27.7, and P27.9 below can still fold their own
project-content trust decision into the `aegis trust` gate P27.1 built.

**Tier 2** (11 items — cheap, self-contained hardening, no dependency):
- **P27.3 (FIND-05)** — Default `security.redact_secrets` on (or prompt on first cloud-provider use).
- **P27.4 (FIND-06)** — Generate/require a shared-secret token by default for `aegis mcp-serve` and
  `aegis acp` stdio interfaces.
- **P27.5 (FIND-13)** — Enable the existing pinned-cert loopback TLS by default.
- **P27.6 (FIND-07)** — Wrap project context/memory files (`AGENTS.md`/`CLAUDE.md`/
  `.aegis/context.md`/`.aegis/memory.md`) in the same untrusted-provenance marker already used for
  persona/skill bodies (mirrors shipped P24.4).
- **P27.7 (FIND-09)** — Treat project-persona control fields (`output_guard`, `mode`, `tools`,
  `rules`) as untrusted; ignore or require confirmation when sourced from a project-level persona.
- **P27.8 (FIND-10)** — Route the HTTP MCP client through the existing `ssrfSafeDialer` (already
  used by `web_fetch`).
- **P27.9 (FIND-11)** — Source `recon_scan`/`dast_scan`'s `allowed_targets` from user/global config
  only, not project config.
- **P27.10 (FIND-18, ACL half)** — Apply `fsguard.RestrictToOwner` to `longmem.db` and
  `knowledge.db` (and WAL/SHM sidecars), matching the session DB.
- **P27.11 (FIND-20)** — Write processed swarm-mailbox files `0o600` (not `0o644`) and
  `fsguard`-harden the `teams/` tree.
- **P27.12 (FIND-14)** — Set conservative default `max_concurrent_runs`/per-run duration caps;
  throttle repeated invalid-auth attempts.
- **P27.13 (FIND-12, default-on half)** — Enable the invisible-char/base64 injection scan by
  default for web/MCP content (currently opt-in per tool).

**Tier 3** (5 items — real value, larger or sequence-dependent):
- **P27.14 (FIND-04)** — Warn/recommend against the unconfined `local` sandbox backend for
  execute-capable tool use; consider defaulting new installs to the OS sandbox where available.
- **P27.15 (FIND-08)** — Apply the full permission stack (mode + text rules + contextual gate) at
  cron fire time, not just the coarse mode check.
- **P27.16 (FIND-15)** — Move (or add) a guard check before irreversible high-risk writes, or
  quarantine/roll back a file written under a subsequent guard FAIL rather than only retrying.
- **P27.17 (FIND-16)** — Propagate a shared/proportional budget ceiling into detached swarm spawns
  so they can't escape the fan-out tree's cost cap.
- **P27.18 (FIND-19)** — Restrict OS-sandbox (seatbelt/bwrap) readable paths to the workspace plus
  required toolchain paths; deny network egress by default under it.

**Tier 4:** parked — P25.9, P13.3.3, P6.1, P27.19, P27.20. See
[Parked](#open-work--parked-tier-4).

---

## Open Work — P27 (Threat Model Findings — 2026-07-13)

Full-repo STRIDE-A threat model at commit `7230aaf`:
[`threat-model-20260712-200318/`](../threat-model-20260712-200318/3-findings.md). 20 findings
total (76 threats across 32 components), all Tier 2 "Conditional Risk" or Tier 3
"Defense-in-Depth" in the threat model's own exploitability scheme — `LOCALHOST_SERVICE` means
zero remote-exploitable (Tier 1 in *that* scheme) findings. Unlike the 2026-07-10 pass (14 of 35
findings were already-mitigated no-ops), every finding here carries a live remediation — this
report audits a codebase that already absorbed the first pass's fixes, so what's left is defaults,
trust boundaries, and defense-in-depth gaps rather than missing controls outright. Grouped below by
the roadmap tier assigned in [Priority Order](#priority-order); FIND-01/FIND-02 are combined into
one roadmap item since their remediations are the same feature, and FIND-18's two remediation
halves (quick ACL fix vs. optional encryption) are split across tiers.

#### Tier 1 — shipped 2026-07-13 (P27.1, P27.2 — see [releases.md](releases.md#latest-changes))

#### Tier 2 — cheap, self-contained hardening, no dependency

### P27.3 — FIND-05: default `security.redact_secrets` on

Priority: Tier 2 · Effort: S, security, Moderate (CVSS 6.1)

Read-tool/conversation content (including secrets) is forwarded to the cloud model provider;
`security.redact_secrets` (gitleaks-backed masking) already exists but is opt-in. Default it on,
or prompt to enable on first cloud-provider use.

### P27.4 — FIND-06: default token for `mcp-serve`/ACP stdio interfaces

Priority: Tier 2 · Effort: S, security, Moderate (CVSS 6.0)

`aegis mcp-serve` and the ACP stdio server support `AEGIS_MCP_TOKEN`/`AEGIS_ACP_TOKEN` but run
unauthenticated when unset. Generate and require a token by default, mirroring the daemon's
auto-generated bearer token, writing it to an owner-only file for the launching integration to
read.

### P27.5 — FIND-13: loopback TLS on by default

Priority: Tier 2 · Effort: S, security, Moderate (CVSS 4.8)

Client↔daemon loopback traffic is plaintext HTTP by default; pinned-cert TLS
(`server.tls.enabled`) already exists but is off. Enable by default, or at minimum document the
shared-host risk prominently.

### P27.6 — FIND-07: trust-wrap project context/memory files

Priority: Tier 2 · Effort: S, security, Moderate (CVSS 5.9)

`AGENTS.md`/`CLAUDE.md`/`.aegis/context.md`/`.aegis/memory.md` are concatenated into the system
prompt with no untrusted-provenance marker, unlike persona/skill bodies. Reuse
`internal/trust.Wrap` the same way P24.4 already did for file-loaded persona/skill bodies.

### P27.7 — FIND-09: treat project-persona control fields as untrusted

Priority: Tier 2 · Effort: S, security, Moderate (CVSS 5.4)

A project persona's frontmatter (`output_guard: none`, `mode`, `tools`, `rules`) is applied as
real settings with no trust check, unlike the (already-wrapped) persona body. Ignore or require
confirmation for control-field changes sourced from project-level personas; user/global personas
keep full control.

### P27.8 — FIND-10: SSRF-safe dialer for the HTTP MCP client

Priority: Tier 2 · Effort: S, security, Moderate (CVSS 5.3)

The HTTP/SSE MCP client uses a plain `http.Client` with no SSRF-safe dialer, unlike `web_fetch`.
Route it through the existing `ssrfSafeDialer` (`internal/tool/builtin/web.go`).

### P27.9 — FIND-11: source scan `allowed_targets` from user/global config only

Priority: Tier 2 · Effort: S, security, Moderate (CVSS 5.1)

`recon_scan`/`dast_scan`'s target-authorization `allowed_targets` is a config value and inherits
the project-config trust weakness, letting a hostile repo widen it to scan arbitrary Internet
hosts. Source the allowlist from user/global config only.

### P27.10 — FIND-18 (ACL half): fsguard-harden `longmem.db`/`knowledge.db`

Priority: Tier 2 · Effort: S, security, Moderate (CVSS 5.8 for the finding as a whole)

`longmem.db` and `.aegis/knowledge.db` rely on directory permissions only, unlike the
`fsguard`-hardened session DB. Apply `fsguard.RestrictToOwner` to both (and WAL/SHM sidecars) —
same pattern as P24.16's session-DB/`.env` hardening.

### P27.11 — FIND-20: harden swarm mailbox file permissions

Priority: Tier 2 · Effort: S, security, Low (CVSS 3.5)

Swarm mailbox messages carry no authentication, and processed files are written world-readable
(`0o644`) with the `teams/` tree not `fsguard`-hardened. Write processed files `0o600`, harden the
tree, and add a per-run shared secret/HMAC to mailbox messages.

### P27.12 — FIND-14: default concurrency/rate caps

Priority: Tier 2 · Effort: S, security, Low (CVSS 3.9)

No rate limiting on the daemon API and `max_concurrent_runs` defaults to unlimited. Set
conservative default caps; throttle repeated invalid-auth attempts (extends P24.5's invalid-token
logging with actual throttling).

### P27.13 — FIND-12 (default-on half): enable injection scan by default

Priority: Tier 2 · Effort: S/M, security, Moderate (CVSS 5.0)

The invisible-char/base64 prompt-injection scan of untrusted web/MCP content is best-effort and
opt-in per tool, on top of the always-on provenance-marker wrapping. Enable it by default for
network-sourced content; expanding detection beyond the current heuristics stays unscoped
(open-ended, no trigger yet).

#### Tier 3 — real value, larger or sequence-dependent

### P27.14 — FIND-04: warn/recommend against the unconfined `local` sandbox backend

Priority: Tier 3 · Effort: M, security, Moderate (CVSS 6.8)

The default `local` sandbox backend runs shell commands on the host with only env-var stripping,
no fs/net/process isolation (the build-mode approval prompt and `ValidatePath` are today's
compensating controls). Recommend/document the OS or container backends for untrusted workloads;
consider a persistent warning banner when `local` is active with execute-capable tools enabled, or
defaulting new installs to the OS sandbox where available.

### P27.15 — FIND-08: apply the full permission stack at cron fire time

Priority: Tier 3 · Effort: M, security, Moderate (CVSS 5.6)

Cron fire-time gating re-checks only the coarse permission mode, not the text allow/deny rules or
contextual egress gate applied to interactive tool calls — extends P24.3's mode-only fire-time
gate to the full stack. Require a separately-confirmed flag for `auto_approve` jobs; surface
persisted auto-approve jobs in a review view.

### P27.16 — FIND-15: pre-write guard check / quarantine on FAIL

Priority: Tier 3 · Effort: M, security, Low (CVSS 3.6)

The output guard runs after files are already written, so a FAIL verdict can only drive a retry,
not undo a bad write. Move a check (or add a lighter pre-write pass) before irreversible writes
for high-risk deliverables, or quarantine/roll back a written file on FAIL.

### P27.17 — FIND-16: propagate budget ceiling into detached swarm spawns

Priority: Tier 3 · Effort: M, security, Low (CVSS 3.4)

Detached/background swarm sub-agent spawns lose the shared cost tracker and fall back to a fresh
full budget, escaping the fan-out tree's ceiling — the in-context-spawn equivalent of this was
already fixed by P24.15's fair-share floor. Propagate a shared/proportional budget ceiling into
detached spawns too.

### P27.18 — FIND-19: OS sandbox read-path confinement

Priority: Tier 3 · Effort: M, security, Moderate (CVSS 5.5, Host/OS-Access defense-in-depth tier)

The OS sandbox (seatbelt/bwrap) restricts writes/network but leaves the entire host filesystem
readable, so a command running under it can read (and, unless network is also denied, exfiltrate)
SSH keys/cloud credentials — matters once a command is already running untrusted code under the
sandbox. Restrict readable paths to the workspace plus required toolchain paths; deny network
egress by default under this backend.

**Tier 4 (2 items, headings live in [Parked](#open-work--parked-tier-4) since that's the canonical
open-item list for that tier):**
- **P27.19 (FIND-17)** — Container backend's Docker/Podman socket access is root-equivalent on the
  host (CWE-269, CVSS 5.9); existing hardening (`--cap-drop=ALL`, `--security-opt=no-new-privileges`,
  `--network none` default) already applied per P24.10, so the residual is inherent to the
  runtime's own socket-trust model. Remediation is documentation (recommend rootless
  Podman/socket-proxy) — no further code action currently scoped.
- **P27.20 (FIND-18, encryption half)** — Optional at-rest encryption (e.g., SQLCipher) for the
  conversation/checkpoint SQLite stores, beyond the ACL fix already covered by P27.10. Larger, no
  concrete trigger beyond the threat model's own suggestion.

**Needs-verification items the threat model itself flagged** (not yet findings — check before or
while implementing the related item above): hook execution timing relative to any trust prompt
(relevant to P27.1); whether the TUI fully neutralizes terminal escape sequences in untrusted tool
output; whether cron fire-time gating truly skips text rules or only the contextual gate (relevant
to P27.15). See `0-assessment.md`'s "Needs Verification" table in the report folder.

---

## Open Work — Parked (Tier 4)

Low urgency, no trigger, or explicitly parked pending demand. Do not build speculatively —
revisit only if a concrete trigger (user demand, reported pain, incident) appears, and check
with the user before starting any of these. **P27.19 and P27.20** (added 2026-07-13, from the new
threat model) are parked for a different reason than the original 3 — not "no trigger" but "the
finding's own remediation is documentation-only or explicitly optional," see their entries below.
Re-verified 2026-07-12 against the full P25/P26 batch — no scope changes to any of the original 3
items below. P25.9 in particular was checked line-by-line:
P25.8 only threaded `Workdir` through `swarm.SpawnConfig`, `cron.Job`, and `api.DebateRequest`
(which directory a *spawned engine's tool calls* resolve against) — it never touched the
daemon-wide singletons P25.9 names (`lsp.Manager`, `knowledge.Store`, `longmem.Store`, the cached
repo-map, persona/command/agent-def directory discovery, the `os` sandbox backend's
write-confinement profile), so P25.9's scope is reconfirmed unchanged, not narrowed.

**2026-07-12 update:** eight of the original 11 parked items — P24.21, P13.3.2, P9.4, P13.4 (first
batch) and P22.5, P22.6, P20.2, P20.3 (second batch) — were explicitly selected by the user across
two rounds and implemented the same day; see [releases.md](releases.md#latest-changes) for what
shipped. The remaining 3 below are still parked, each for its own reason rather than just "not yet
picked": P25.9 and P6.1 were deliberately excluded from the second batch as too large/risky for
parallel automation (both Effort L, both touch daemon-wide or core-engine state), and P13.3.3
remains gated on an ACP-host precondition that hasn't occurred. Being picked from a bundle isn't
the same as an organic demand signal, so none of these three should be started without a fresh
trigger or explicit user direction.

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

### P27.19 — FIND-17: container socket trust (documentation only)

Priority: Tier 4 · Effort: S, doc-only — remediation has no further code action currently scoped

Docker/Podman socket access is root-equivalent on the host. Aegis already applies
`--cap-drop=ALL`, `--security-opt=no-new-privileges`, and a `--network none` default (shipped as
P24.10) — the residual risk is inherent to the container runtime's own socket-trust model, not a
missing Aegis control. Remediation is documenting the socket-privilege caveat and recommending
rootless Podman or a socket-proxy where available.

### P27.20 — FIND-18 (encryption half): optional at-rest encryption for SQLite stores

Priority: Tier 4 · Effort: M/L — parked, no concrete trigger beyond the finding's own suggestion

SQLCipher (or similar) at-rest encryption for the conversation/checkpoint SQLite stores, on top of
the ACL hardening already covered by P27.10 (Tier 2). Larger scope, opt-in, for higher-assurance
deployments — no reported pain driving it yet.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
