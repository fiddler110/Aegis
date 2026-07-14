# Aegis Capability Roadmap

**Last updated:** 2026-07-14 — **P28.7** (Tier 2, Effort S) shipped: a lightweight, persistent
connection/model-health indicator (reachable, model name, last-probed latency) in both the TUI
status area and the web UI header, refreshed periodically in the background rather than requiring
a wasted conversational turn just to check daemon-to-model connectivity. Reuses the existing
`GET /status` endpoint (`internal/server/server.go`), extended with a cheap `probeProviderReachability`
check, rather than adding a new one. See [releases.md](releases.md#latest-changes) for the full
writeup.

Before that (2026-07-13): **P27.19** (FIND-17, Tier 4, CVSS 5.9, doc-only) shipped: the
"Docker/Podman socket privilege equivalence" section in `docs/security_scan.md` (added by
P24.10/FIND-06) already covered `--cap-drop=ALL`/`--security-opt=no-new-privileges` and recommended
rootless Podman / userns-remapped Docker as mitigations for socket-access privilege equivalence —
but FIND-17's remediation also names a **socket-proxy** as an option, and nothing in the docs
mentioned one. Added a new bullet to that section recommending a socket-proxy (e.g.
`docker-socket-proxy`) in front of a rootful Docker daemon for deployments that can't move to
rootless Podman, restricting it to only the container-create/start/stop endpoints Aegis needs
rather than the full Docker API. No code changes — this closes the one concrete gap between
FIND-17's remediation text and the pre-existing FIND-06 documentation; the `--network none` default
and cap-drop hardening FIND-17 also cites were already shipped and already documented, so this item
was purely additive to close the socket-proxy mention. `docs/security_scan.md` is documentation
only, no tests affected. This closes out Tier 4's P27.19; P27.20 remains parked (no trigger beyond
the threat model's own suggestion).

Before that, same day (2026-07-13): **P27.17** (FIND-16, Tier 3, CVSS 3.4) shipped: investigation found the
finding's core mechanism was already in place — `internal/tool/builtin/agent.go`'s `spawnBackground`
(the sole production entry point for a detached/background sub-agent spawn) already read the shared
cost tracker off the caller's request ctx and re-attached it onto the job's severed context before
handing off to the swarm backend, and `internal/swarm/subprocess.go`'s `SubprocessBackend.Spawn`
already applied P24.15's fair-share floor to compute a reduced `WorkerSpec.RemainingBudgetUSD`/
`RemainingTokens` from that carried-forward tracker — but neither half had ever been exercised
together end to end, through the real production path, with a real (non-stub) subprocess backend. New
`TestAgentToolBackgroundSpawnRespectsSharedBudgetCeiling`
(`internal/tool/builtin/agent_subprocess_test.go`) closes that gap: it spawns a detached background
sub-agent through the real `agentTool`, a real `task.Manager`, and a real `*swarm.SubprocessBackend`,
with a shared tracker already carrying significant prior spend, and asserts the detached child's
`WorkerSpec` actually receives the fair-share-reduced ceiling rather than a fresh full budget —
confirmed to actually catch a regression (verified by temporarily reverting the carry-forward locally
and observing the new test fail, then restoring it). No production code changes were needed. Also
corrected a stale comment at `internal/swarm/subprocess.go:155-157` claiming the tracker is nil for
"some background paths" — no longer true given the above, and now says so with a pointer to the new
test. Full writeup in [releases.md](releases.md#latest-changes). This closes out Tier 3 — this item
was implemented in parallel with **P27.16** (output-guard quarantine-on-FAIL) in a separate
worktree/branch; see that item's own history entry below.

Before that, same day (2026-07-13): **P27.16** (FIND-15, Tier 3, CVSS 3.6) shipped: quarantine-on-FAIL
for the output guard. Previously, a FAIL verdict that exhausted the guard's corrective retries only
ever led to the failing response being surfaced anyway — any `write_file`/`edit_file` call the turn
made already landed on disk and stayed there untouched. The engine's per-turn checkpoint
`Snapshotter` (`internal/checkpoint`) already captures pre-write content for every path
`write_file`/`edit_file` touch before the write happens — the same primitive `/rewind` uses — so the
exhausted-retries branch in `internal/engine/engine.go`'s guard check now calls
`checkpoint.SnapshotterFrom(ctx).RestoreFiles(ctx)` (new `Snapshotter` method delegating to the
existing `Store.RestoreFiles`) to roll every file the turn wrote back to its pre-turn state before
surfacing the failing answer, rather than leaving the bad write in place. A nil Snapshotter (no
checkpoint store wired in) makes the call a no-op, so a caller without one keeps today's
retry-then-surface behavior unchanged. The rollback is surfaced to the caller two ways: a new
`Engine.Event.GuardFilesRestored` count on the terminal `KindGuard` failure event, and the
restored-file count appended to that event's `GuardReason` text, which the TUI's existing
output-guard warning line already renders verbatim — no new UI wiring needed. Full writeup in
[releases.md](releases.md#latest-changes).

Before that, same day (2026-07-13): **P27.18** (FIND-19, Tier 3, CVSS 5.5) shipped: the `os` sandbox
backend (seatbelt/bwrap) now confines file reads to the workspace plus a bounded toolchain allowlist
instead of the entire host filesystem — `seatbeltProfile` adds a `file-read*` deny/allow pair
mirroring the existing write-confinement rules, and `bwrapArgs` replaces `--ro-bind / /` with
per-path read-only binds. New `sandbox.os_extra_read_paths` config lets operators extend the
allowlist for non-standard toolchain locations. This was shipped ahead of the then-still-open
P27.16/P27.17 since it was fully self-contained; both have since shipped too. Full writeup in
[releases.md](releases.md#latest-changes).

Before that, same day (2026-07-13): **P27.15** (FIND-08, Tier 3, CVSS 5.6) shipped: cron fire-time gating
now applies the full permission stack — text allow/deny rules, then the contextual egress/network
policy, then the coarse mode check — instead of just the mode check FIND-03/P24.3 originally added.
`internal/server/helpers.go`'s `newCronRunFunc` no longer calls `permission.Policy.Decide` directly;
it takes a `permCheck` thunk, and the new `Server.cronPermCheck` builds the exact same gate
`buildGate` assembles for every interactive engine run (mode → contextual → rules, empty persona)
and checks it against the real `"shell"` tool with the job's command as input. A job's `auto_approve`
opt-in resolves any Ask-tier decision anywhere in that stack (mirroring pre-P27.15 behavior, now
extended uniformly instead of only covering the top mode-level Ask); an explicit `deny` rule or a
Deny-mode decision still blocks regardless of `auto_approve`, and an explicit `allow` rule lets a job
fire unattended without needing `auto_approve` at all — matching how rules already override the mode
gate for interactive tool calls. The `mode func() permission.Mode` construction previously happened
before the `*Server` existed in `New()`'s constructor order (the scheduler is built early so the
cron_* tools can be registered with it); resolved by predeclaring `var s *Server` and closing over it
in the `permCheck` thunk, which is only invoked at actual fire time — long after `New()` finishes
building `s` — rather than restructuring construction order or adding a scheduler-internal setter.

Also new: a human-facing review view for persisted cron jobs (the roadmap item's "surface persisted
auto-approve jobs in a review view"), since jobs were previously visible only to the model via the
`cron_list` tool. `GET /cron/jobs` (`api.CronJobInfo`, `internal/server/sessions.go`), a
`Client.ListCronJobs` method, and a new `aegis cron list` CLI command (`--auto-approve-only` to
filter) — flags each `auto_approve` job inline as `[AUTO_APPROVE — fires unattended, bypassing
interactive approval]`. Docs updated (`docs/tools-reference.md`). New tests:
`TestNewCronRunFuncBlockedByDenyRuleEvenInAutoMode`, `TestNewCronRunFuncAllowedByRuleEvenInPlanMode`,
`TestServerCronPermCheck`, `TestHandleListCronJobs` (`internal/server/cron_test.go`); the 6
pre-existing `newCronRunFunc` tests were updated to the new `permCheck`-thunk signature via a
`cronPermCheckFor` test helper rather than dropped. `go build ./...`, `go vet ./...`, and the full
`go test ./...` pass clean. Next up: 3 remaining Tier 3 items, P27.16 first — see
[Priority Order](#priority-order).

Before that, same day (2026-07-13): **P27.14** (FIND-04, Tier 3, first item of the P27 threat
model's Tier 3) shipped: the daemon (`internal/server/server.go`) now logs a persistent startup
`WARN` recommending `sandbox.backend: os`/`container` any time the effective backend is the unconfined
`local` one and permission mode isn't `plan` (i.e. shell/execute tool calls are reachable at all) —
previously the default `build`-mode + local-backend combination, the most common install shape, got
no signal at all; only the sharper `auto`-mode and `auto_approve_exec` cases did. `aegis doctor`'s
`sandbox` check now reports the same local-backend case as a `WARN` (with a `Fix` naming `os`/
`container`) instead of a silent `PASS`. `aegis --first-init`'s generated global config template now
defaults new installs to `sandbox.backend: os` (seatbelt on macOS, bwrap on Linux) instead of
`local` — `SelectSandbox` already gracefully falls back to unsandboxed `local` (logging the same
warning) when no OS sandbox mechanism is available, e.g. bubblewrap not installed on Linux, or on
Windows, which has neither mechanism — so this is a zero-risk-of-breakage default for a real
isolation win at no cost on macOS. Existing installs' on-disk configs are untouched; only the
generated template and the base `config.Load()` fallback-when-nothing-is-configured default (used
by callers with no config file at all, e.g. tests/embedders) still default to `local`, deliberately
left alone as the conservative base case. Docs updated (`docs/configuration.md`,
`docs/security_scan.md`). New regression tests: `TestNewWarnsLocalSandboxBuildMode`,
`TestNewSkipsLocalSandboxWarningInPlanMode` (`internal/server/sandbox_startup_test.go`),
`TestDoctorCleanSetupExitsZero` updated to assert the new WARN. `go build ./...`, `go vet ./...`,
and the full `go test ./...` pass clean. Next up: the remaining 4 Tier 3 items, P27.15 first — see
[Priority Order](#priority-order).

Before that, same day (2026-07-13): the P27 threat model's entire Tier 2 shipped: all 11 items,
**P27.3–P27.13**. Implemented in parallel by 7 isolated sub-agents in separate git worktrees,
grouped by file-overlap risk rather than 1:1 with finding IDs — 6 agents each owned a fully
disjoint package, and one agent bundled the 5 items that all needed to edit
`internal/config/config.go`'s shared `defaults()` map/`Load()` path (P27.3, P27.5, P27.9, P27.12,
P27.13) into a single branch to avoid map-literal collisions. All 7 branches merged into `main`
with zero manual conflict resolution (git auto-merged every overlapping file, including both
three-way merges touching `config.go` and `server.go`). Full `go build ./...`, `go vet ./...`, and
`go test -count=1 ./...` pass clean on the fully integrated tree. Full per-item writeup in
[releases.md](releases.md#latest-changes) — notable finds along the way: P27.5 (TLS-on-by-default)
surfaced a real latent bug where `AEGIS_SERVER_TLS_ENABLED` silently did nothing (now fixed), and
P27.11 (swarm mailbox hardening) surfaced a real Windows ACL bug where hardening a populated
directory left descendant files with an effectively empty inherited DACL (now fixed with proper
inheritance flags). Next up: Tier 3 (P27.14–P27.18) — see [Priority Order](#priority-order).

Before that, same day (2026-07-13): the P27 threat model's Tier 1 closed out: **P27.1**
(workspace-trust gate, FIND-01/FIND-02) and **P27.2** (`provider.base_url` allowlist/warn, FIND-03)
both shipped. **P27.1** adds `internal/workspacetrust` (a JSON store of per-directory trust
decisions) and gates `config.Load()` on it — until a directory is explicitly trusted via the new
`aegis trust` command, project-sourced `permission.*`, `sandbox.*`, `mcp.servers`, `notify.webhook`,
and `hooks` are frozen back to their user/global values (computed via a second, project-excluded
koanf load compared key-by-key against the merged one), with the diff surfaced through
`cfg.WorkspaceTrust`, a startup WARN (daemon log + TUI stderr banner), and a new `aegis doctor`
check. The two existing first-party project-config writers of a gated key —
`config.PatchProjectSandbox` and `config.AppendProjectPermissionRule` (the TUI's "allow always"
approval option) — auto-trust the directory they write to as a side effect, since that write is
itself an explicit local operator action, not something silently inherited from a cloned repo.
**P27.2** adds `providerfactory.validateBaseURL`: a non-loopback plaintext-HTTP `base_url` is
refused outright when a real API key would be attached (CWE-522), while a non-default HTTPS host
for a cloud provider is allowed through with a prominent startup WARN rather than a hard block,
since legitimate gateway/proxy setups are common. `go build ./...` and `go test ./...` pass clean.

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

**Open items:** none — Tiers 1-3 of the P27 threat-model batch are fully closed, and Tier 4's
**P27.19** shipped 2026-07-13 (doc-only). Tiers 1 and 2 (**P27.1–P27.13**) and all of Tier 3
(**P27.14–P27.18**) shipped 2026-07-13; P27.18 shipped out of order, ahead of P27.16/P27.17, since
it was fully self-contained, and P27.16/P27.17 shipped together via two parallel worktree agents.
Tier 4 has 4 remaining parked items — the pre-existing P25.9/P13.3.3/P6.1 plus **P27.20** (see
[Parked](#open-work--parked-tier-4)). Separately, **P28.7** (Tier 2, Effort S — TUI/web UI
connection-health indicator) shipped 2026-07-14, filed and closed the same day from live-usage
evidence (recurring "test that the model is connected" sessions) rather than as part of the P27
threat-model batch — see [releases.md](releases.md#latest-changes).

**Next session:** nothing queued — the entire P27 threat-model batch (Tiers 1-3 plus P27.19, 19
items) plus P28.7 are shipped. Next trigger: a new threat-model pass, a reported incident, a new
feature evaluation, or a concrete pain point surfacing one of the remaining Tier 4 parked items.
Re-run `TestLiveWorkflow` (recipe in CLAUDE.md) after any change touching the
engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor` (P26.1) is the standalone
preflight companion for the same misconfiguration classes (now including a workspace trust check,
P27.1, and the local-sandbox recommendation, P27.14).

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** empty — **P27.1** and **P27.2** shipped 2026-07-13 (see
[releases.md](releases.md#latest-changes)).

**Tier 2:** empty — **P27.3–P27.13** (all 11 items) shipped 2026-07-13 (see
[releases.md](releases.md#latest-changes)). P27.7's project-persona control-field gate and P27.9's
DAST-target sourcing both ended up folding into (or reusing the machinery of) the `aegis trust` gate
P27.1 built, as anticipated; P27.6's context/memory-file wrapping used the separate `trust.Wrap`
provenance mechanism instead, matching the P24.4 precedent for persona/skill bodies.

**Tier 3:** empty — all 5 items (**P27.14–P27.18**) shipped 2026-07-13 (see
[releases.md](releases.md#latest-changes)). P27.17's investigation found its core mechanism (shared
cost-tracker propagation into detached swarm spawns) already in place from an earlier phase; it
shipped as a verification/test-coverage item rather than a production-code fix.

**Tier 4:** parked — P25.9, P13.3.3, P6.1, P27.20. **P27.19** shipped 2026-07-13 (doc-only, see
above). See [Parked](#open-work--parked-tier-4).

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

#### Tier 2 — shipped 2026-07-13 (P27.3–P27.13, all 11 items — see
[releases.md](releases.md#latest-changes))

#### Tier 3 — real value, larger or sequence-dependent

#### P27.14 — shipped 2026-07-13 (see [releases.md](releases.md#latest-changes))

#### P27.15 — shipped 2026-07-13 (see [releases.md](releases.md#latest-changes)) — scope note: the
existing per-job `auto_approve` field already was the "separately-confirmed flag" the finding
called for (explicit, boolean, distinct from the daemon's ambient permission mode); rather than add
a second flag, its scope was extended in place to resolve Ask-tier decisions across the whole gate
stack instead of only the mode-level one, and `aegis cron list` was added as the review view.

#### P27.16 — shipped 2026-07-13 (see [releases.md](releases.md#latest-changes)) — scope note:
chose the "quarantine/roll back a written file on FAIL" remediation over the alternative "lighter
pre-write pass," reusing the existing checkpoint/rewind `Snapshotter`/`RestoreFiles` machinery
rather than adding a new pre-write validation mechanism.

#### P27.17 — shipped 2026-07-13 (see [releases.md](releases.md#latest-changes)) — scope note: the
finding's core mechanism (a shared/proportional budget ceiling carried into detached swarm spawns)
turned out to already exist — `agent.go`'s `spawnBackground` and `subprocess.go`'s
`SubprocessBackend.Spawn` already did the carry-forward and fair-share-floor computation
respectively, tracing back to a pre-threat-model fix (D1/P10.3/P24.15); what was missing was
end-to-end regression coverage proving the two halves actually connect through the real production
path, which the new test now provides. No production code changed.

#### P27.18 — shipped 2026-07-13 (see [releases.md](releases.md#latest-changes))

**Tier 4 (1 item remaining open; P27.19 shipped 2026-07-13, heading lives in
[Parked](#open-work--parked-tier-4) since that's the canonical open-item list for that tier):**
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
with the user before starting any of these. **P27.20** (added 2026-07-13, from the new threat
model) is parked for a different reason than the original 3 — not "no trigger" but "the finding's
own remediation is explicitly optional," see its entry below. Its sibling **P27.19** was the
same shape (documentation-only remediation) but shipped 2026-07-13 on explicit user request — see
[above](#priority-order).
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

#### P27.19 — shipped 2026-07-13 (see [releases.md](releases.md#latest-changes)) — scope note:
FIND-17's `--cap-drop=ALL`/`--security-opt=no-new-privileges`/rootless-Podman guidance was already
documented via P24.10/FIND-06; the only gap was FIND-17's remediation also naming a socket-proxy as
an option, which the existing docs didn't mention. Closed with a doc-only addition to the same
section rather than a new one.

### P27.20 — FIND-18 (encryption half): optional at-rest encryption for SQLite stores

Priority: Tier 4 · Effort: M/L — parked, no concrete trigger beyond the finding's own suggestion

SQLCipher (or similar) at-rest encryption for the conversation/checkpoint SQLite stores, on top of
the ACL hardening already covered by P27.10 (Tier 2). Larger scope, opt-in, for higher-assurance
deployments — no reported pain driving it yet.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
