# Harness Enhancement — Handoff / Resume Notes

_Last updated: 2026-06-24_

This document captures the roadmap and current state so work can resume in a new
session. The full approved roadmap lives at:
`C:\Users\scott\.claude\plans\i-want-to-further-sorted-milner.md`

## TL;DR

Implementing a phased enhancement roadmap (Phases 9–15) for the Go agent harness,
porting best-of-breed patterns from **OpenHarness**, **Crush** (charmbracelet),
and **opencode** (anomalyco), plus LangGraph/Omnigent/CrewAI/AG2 ideas.

- **Phase 9 (Resilience & Cost): ✅ COMPLETE**
- **Phase 10 (Multi-agent orchestration): ✅ COMPLETE** (in-process + subprocess + polish)
- **Phase 11 (Background tasks & scheduling): ✅ COMPLETE** (task manager + tools + cron)
- **Phase 12 (Sandboxing & contextual security): ✅ COMPLETE** (sandbox + policies + audit)
- Phases 13–15: ⬜ not started

## Git state (IMPORTANT)

- **No git remote is configured.** Nothing is pushed; no PRs exist. Everything is
  local. To push later: `git remote add origin <url>` then push both branches.
- Branches (each stacks on the previous):
  - `phase-9-resilience-cost` — commits `7c2fca5`, `575a9e6` (off `4b58a7f` Phase 8)
  - `phase-10-multi-agent` — commits `2fbfc84`, `9b0d81d`, `61df5c8` (stacked on Phase 9)
  - `phase-11-tasks` — commits `ba36fef`, `cb7cb2f`, `8158b21` (stacked on Phase 10)
  - `phase-12-sandbox` — commits 12.1, 12.2, 12.3 (stacked on Phase 11)
- **Currently checked out: `phase-12-sandbox`**.
- Working tree is clean except this `HANDOFF.md` (untracked).

To verify everything: `go build ./... && go test ./... -race` (all green).
`go vet ./...` is clean. New files are gofmt-clean; several pre-existing files
were already not gofmt-formatted in the baseline — do NOT mass-reformat them.

## What's done

### Phase 9 — Resilience & Cost (branch `phase-9-resilience-cost`)

- **9.1 Retry/backoff** — `internal/provider/errors.go` + `retry.go`
- **9.2 Prompt caching** — `internal/provider/anthropic/anthropic.go`
- **9.3 Cost tracking** — `internal/cost/cost.go`
- **9.4 Parallel tools** — `internal/engine/engine.go`
- **9.5 Loop detection** — `internal/engine/loopdetect.go`

### Phase 10 — Multi-agent orchestration (branch `phase-10-multi-agent`)

- **Swarm core** — `internal/swarm/` (types, mailbox, registry, inprocess, subprocess)
- **Agent definitions** — `internal/agentdef/agentdef.go`
- **`agent` tool** — `internal/tool/builtin/agent.go` (sync + background delegation)
- **Worker CLI** — `internal/cli/worker.go`
- **Polish** — audit, `/teammates`, TUI swarm panel

### Phase 11 — Background tasks & scheduling (branch `phase-11-tasks`)

- **11.1** — `internal/task/` (Manager, Store, SQLite persistence)
- **11.2** — `internal/tool/builtin/task.go` (6 task tools), background shell + agent
- **11.3** — `internal/cron/` (5-field parser, scheduler, 4 cron tools)

### Phase 12 — Sandboxing & contextual security (branch `phase-12-sandbox`)

#### 12.1 — Sandbox backend + hardened path validator

- **`internal/sandbox/sandbox.go`** — `Backend` interface (`Exec`, `ExecStreaming`,
  `Close`). Shared helpers: `shellCommand`, `execWithTimeout`.
- **`internal/sandbox/local.go`** — `LocalBackend`: runs commands directly on the
  host OS (default, preserves existing behavior).
- **`internal/sandbox/docker.go`** — `ContainerBackend`: auto-detects available
  container runtime (Docker → Podman → Apple Containers on macOS). Falls back to
  local if none found. Each command runs via `docker/podman run --rm` with workspace
  bind-mounted and optional network isolation (`--network none`).
  `ContainerRuntime` enum: `docker`, `podman`, `container` (Apple).
- **`internal/sandbox/pathvalidator.go`** — `ValidatePath`: resolves symlinks
  via `filepath.EvalSymlinks` before checking workspace containment. Handles
  non-existent paths by resolving the nearest existing ancestor. Replaces the
  basic `filepath.Rel` check in builtin tools.
- **Shell tool refactored** — `shell.go` now delegates to `sandbox.Backend` instead
  of creating `exec.Command` directly. `task_create` also uses the backend.
- **Config** — `sandbox.backend` (local | container), `sandbox.runtime` (docker |
  podman | container | empty=auto), `sandbox.image`, `sandbox.network`.
- **Tests** — path validator (basic, absolute, dot-dot escape, new file, symlink
  escape, symlink inside), local backend (exec, timeout, streaming, failure).

#### 12.2 — Contextual security policies

- **`internal/permission/contextual.go`** — `ContextualGate` wrapping the base
  `Gate`. Implements both `engine.Gate` and `hooks.Hook` interfaces. Stateful
  tracking of session events with two rules:
  - **EgressThenWrite**: after any successful `CapNetwork` tool call, subsequent
    `CapWrite` calls require `Ask` (approval). Protects against read→exfil→write
    data exfiltration patterns.
  - **NetworkAllowList**: `CapNetwork` calls checked against a configurable domain
    allowlist with subdomain matching (leading dot). Calls to unlisted domains
    denied. Search tools get a synthetic `search.api` passthrough.
- **`OnDecision` callback** for audit observability.
- **`Reset()`** clears state between sessions.
- **Tests** — 10 tests: egress-then-write (block, approve, ignore errors, disabled),
  network allowlist (allowed, subdomain, blocked, disabled, search passthrough),
  reset, base-denial precedence, OnDecision callback.

#### 12.3 — Server wiring + audit integration

- **Config** — `security.egress_then_write` (bool), `security.network_allowlist`
  (string list).
- **Server wiring** — `newEngine()` wraps the base gate with `ContextualGate` when
  any security policy is enabled. Gate's `OnDecision` feeds into audit trail.
  Contextual gate composed into hooks chain so `PostToolUse` updates state.
- **Audit** — `hooks.Audit.PolicyDecision()` records contextual policy decisions
  as `phase: "policy_decision"` JSONL records with rule, decision, reason, cap.

### New config knobs (all in `internal/config/config.go`, with defaults)

- `provider.max_retries` (4)
- `cost.budget_usd` (0 = unlimited)
- `swarm.backend` ("in_process" | "subprocess")
- `sandbox.backend` ("local" | "container")
- `sandbox.runtime` ("" = auto-detect | "docker" | "podman" | "container")
- `sandbox.image` ("ubuntu:22.04")
- `sandbox.network` (false)
- `security.egress_then_write` (false)
- `security.network_allowlist` (empty = no restriction)

## Deferred / explicitly NOT done

- Agent definitions from files/plugins (markdown discovery) — slated for **Phase 15**.
- The `agent` tool is registered only in the **daemon/server** path, not the `chat`
  one-shot CLI path.
- Docker backend uses per-command `docker run --rm`; a persistent container
  optimization could be added later.

## NEXT: Phase 13 — Coding intelligence (Crush-inspired)

Goal: LSP integration, file staleness tracking, MCP HTTP/SSE transports,
local-model discovery. Full detail in the roadmap file.

After Phase 13: Phase 14 (relevance-scored memory, TODO tool, dry-run, config schema),
Phase 15 (custom tools/agents/commands + plugin loader).

## Working conventions used so far

- From-scratch ethos: no heavy deps. Used stdlib (`sync`, `math/rand/v2`,
  `sync.WaitGroup.Go`) over new modules.
- Each phase (or sub-slice) = one commit in the existing `Phase N:` style, ending
  with `Co-Authored-By:` tag.
- Tests accompany each change; run with `-race`. Match existing test style
  (table-driven, `scriptedAdapter`/`fixedAdapter` fakes).
- Don't run `gofmt -w` across the repo — only on newly created/edited files
  (baseline has pre-existing unformatted files).
- The user prefers being asked at phase boundaries before large new phases.
- SQLite driver: `modernc.org/sqlite` (pure Go), driver name `"sqlite"`, not
  `mattn/go-sqlite3`. Tests use `filepath.Join(t.TempDir(), "name.db")`.

## How to resume

1. `git checkout phase-12-sandbox` (or create `phase-13-coding` off it).
2. Re-read the roadmap: `C:\Users\scott\.claude\plans\i-want-to-further-sorted-milner.md`.
3. `go build ./... && go test ./... -race` to confirm a green baseline.
4. Start Phase 13 per the roadmap.
