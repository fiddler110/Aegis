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
- Phases 12–15: ⬜ not started

## Git state (IMPORTANT)

- **No git remote is configured.** Nothing is pushed; no PRs exist. Everything is
  local. To push later: `git remote add origin <url>` then push both branches.
- Branches (each stacks on the previous):
  - `phase-9-resilience-cost` — commits `7c2fca5`, `575a9e6` (off `4b58a7f` Phase 8)
  - `phase-10-multi-agent` — commits `2fbfc84`, `9b0d81d`, `61df5c8` (stacked on Phase 9)
  - `phase-11-tasks` — commits `ba36fef`, `cb7cb2f`, + 11.3 cron (stacked on Phase 10)
- **Currently checked out: `phase-11-tasks`**.
- Working tree is clean except this `HANDOFF.md` (untracked).

To verify everything: `go build ./... && go test ./... -race` (all green).
`go vet ./...` is clean. New files are gofmt-clean; several pre-existing files
were already not gofmt-formatted in the baseline — do NOT mass-reformat them.

## What's done

### Phase 9 — Resilience & Cost (branch `phase-9-resilience-cost`)

- **9.1 Retry/backoff** — `internal/provider/errors.go` (`APIError` w/ `Retryable()`,
  `Retry-After` parsing), `internal/provider/retry.go` (`WithRetry` decorator, exp
  backoff + jitter, retries only pre-stream failures). Adapters return typed errors;
  wired in `providerfactory` via `provider.max_retries` (default 4).
- **9.2 Prompt caching** — `internal/provider/anthropic/anthropic.go` emits
  `cache_control` breakpoints on system prompt, tool list, and message prefix;
  cache tokens surfaced into `provider.Usage` (`CacheCreationTokens`/`CacheReadTokens`).
  Toggle via `anthropic.WithPromptCaching`.
- **9.3 Cost tracking** — `internal/cost/cost.go` (pricing catalog longest-prefix
  match, `Tracker`, `Snapshot`). Engine accumulates per-turn cost, aborts past
  `cost.budget_usd`. TUI shows running `$`; `chat` prints a cost summary.
- **9.4 Parallel tools** — `internal/engine/engine.go` `runTools`: reads/network run
  concurrently (bounded `maxParallelTools=8`), writes/execute serialized via RWMutex,
  emits serialized, results position-indexed to preserve order. Single tool = sequential.
- **9.5 Loop detection** — `internal/engine/loopdetect.go`: aborts when identical
  tool-call turns repeat `LoopThreshold` times (default 3).

### Phase 10 — Multi-agent orchestration (branch `phase-10-multi-agent`)

- **`internal/swarm/`** — pure coordination layer (no engine import):
  - `types.go` — `Identity`, `SpawnConfig`, `Backend` interface (`Spawn`/`Shutdown`/
    `OnStop`), `Handle.Wait`, `RunFunc`, spawn-depth + parent-mode context helpers.
  - `mailbox.go` — durable file mailbox (atomic temp+rename, chronological order,
    unread filter, corrupt-skip). `MailboxRoot(dataDir)` = `<dataDir>/teams`.
  - `registry.go` — thread-safe team registry (`Member` w/ status/summary/timestamps).
  - `inprocess.go` — `InProcessBackend`: teammates as goroutines via `RunFunc`.
  - `subprocess.go` — `SubprocessBackend`: relaunches the harness as `__worker`,
    reads result from mailbox after exit, synthesizes failure (w/ stderr) on crash.
    `WorkerSpec` is the JSON contract.
- **`internal/agentdef/agentdef.go`** — built-in agent definitions: `general`,
  `explore` (read-only search), `plan`, `build`. Resolved by `subagent_type`.
- **`internal/tool/builtin/agent.go`** — the `agent` tool (`CapSpawn`). Resolves
  subagent_type, **clamps child mode to ≤ parent** (`plan` parent forces `plan`
  child), recursion depth guard (`maxSpawnDepth=3`), spawns + waits, returns output.
  `background:true` flag launches the teammate as a detached background task.
- **`internal/cli/worker.go`** — hidden `__worker` command for the subprocess backend
  (loads own config, builtin tools only = leaf node, records result in mailbox).
- **permission** — added `tool.CapSpawn`; allowed in both plan/build modes.
- **server** — `buildSwarmBackend` selects backend from `swarm.backend` config
  (`in_process` default | `subprocess`); injects session mode into run ctx;
  `subAgentRunner()` builds sub-engines for in-process; shutdown waits for teammates.
- **Polish** — `OnStop` lifecycle callback → `SUBAGENT_STOP` audit record
  (`hooks.Audit.SubagentStop`); `GET /teammates` route + `api.Teammate` +
  `client.Teammates`; TUI `Ctrl+T` swarm panel.

### Phase 11 — Background tasks & scheduling (branch `phase-11-tasks`)

#### 11.1 — Background task manager + SQLite persistence

- **`internal/task/task.go`** — core task manager: `State` enum (running/done/failed/
  stopped), `Task` struct, `Spec`, `RunFunc` type, `Manager` with `Start`/`Get`/
  `List`/`SetTitle`/`Stop`/`Wait`/`Shutdown`. `outputBuffer` accumulates streamed
  output. `safeRun` recovers panics. Detached contexts (background-derived) so jobs
  survive the HTTP request that spawned them.
- **`internal/task/store.go`** — SQLite persistence: `tasks` table, `Save` (upsert),
  `Get`, `List` (by session or all), `scanTask` helper. Shares the session DB via
  `session.Store.DB()`.
- **`internal/task/task_test.go`** — tests: TaskCompletes, TaskFailure,
  TaskPanicRecovered, TaskStop, StopUnknown, StopFinishedIsNoop, LivePollAndList,
  SetTitle, ShutdownCancelsLive, PersistenceSurvivesNewManager.

#### 11.2 — task_* tools, killable background shell, async sub-agents

- **`internal/tool/builtin/task.go`** — six task tools: `task_create` (launches
  background shell via `runShellStreaming`), `task_list`, `task_get` (tail output),
  `task_output` (full output), `task_update` (rename), `task_stop` (cancel).
- **`internal/tool/builtin/shell.go`** — `background:true` flag added. When set
  and task manager is available, launches command as a detached background job.
  `runShellStreaming` streams combined stdout/stderr to the task buffer via
  `emitWriter`. Process is killable via `task_stop`.
- **`internal/tool/builtin/agent.go`** — `background:true` flag added.
  `spawnBackground` wraps spawn+wait inside a `task.Manager.Start` so the teammate
  runs detached. Returns task id for polling.
- **Server wiring** — `server.go` creates `task.NewStore(store.DB())` and
  `task.NewManager`, passes to `builtin.Options.Tasks` and `NewAgentTool`.
  Shutdown drains `tasks.Shutdown(shutdownCtx)`.
- **`internal/tool/builtin/task_test.go`** — tests: TaskCreateAndPoll,
  TaskUpdateAndStopErrors, BackgroundShellTool, BackgroundShellWithoutManager,
  BackgroundAgentTool, BackgroundAgentWithoutManager.

#### 11.3 — Cron scheduler

- **`internal/cron/schedule.go`** — 5-field cron parser: `Parse()`, `parseField()`,
  `Schedule.Matches()`. Supports macros (`@hourly`, `@daily`, `@weekly`, `@monthly`).
  Standard cron OR-rule for dom/dow.
- **`internal/cron/cron.go`** — `Job` struct, `Store` (SQLite `cron_jobs` table),
  `Scheduler` with `Create`/`List`/`Delete`/`Toggle`/`tick`/`Run`. `tick()` fires
  enabled jobs matching current minute (idempotent within a minute). `Run()` loops
  aligned to minute boundaries. Injectable clock for tests.
- **`internal/tool/builtin/cron.go`** — four cron tools: `cron_create` (validates
  schedule, persists job), `cron_list`, `cron_delete`, `cron_toggle`.
- **Server wiring** — `server.go` creates `cron.NewStore(store.DB())` and
  `cron.NewScheduler` with a `RunFunc` that launches cron job commands as background
  tasks via the task manager. Scheduler goroutine starts in `ListenAndServe`,
  cancelled on shutdown.
- **`internal/cron/cron_test.go`** — tests: ParseAndMatches, ParseMacros,
  ParseErrors, DomDowOrRule, StoreRoundTrip, SchedulerCreateAndToggle,
  SchedulerCreateBadSchedule, TickFiresAndIdempotent, TickSkipsDisabled.
- **`internal/tool/builtin/cron_test.go`** — tests: CronCreateAndList,
  CronCreateBadSchedule, CronToggleAndDelete, CronDeleteNotFound,
  CronCreateMissingFields.

### New config knobs (all in `internal/config/config.go`, with defaults)

- `provider.max_retries` (4)
- `cost.budget_usd` (0 = unlimited)
- `swarm.backend` ("in_process" | "subprocess")

## Deferred / explicitly NOT done

- Agent definitions from files/plugins (markdown discovery) — slated for **Phase 15**.
- The `agent` tool is registered only in the **daemon/server** path, not the `chat`
  one-shot CLI path.

## NEXT: Phase 12 — Docker sandbox & contextual security policies

Goal: container-isolated command execution and richer security policies.
Full detail in the roadmap file at the path above.

After Phase 12: Phase 13 (LSP, file staleness, MCP HTTP/SSE, local-model discovery),
Phase 14 (relevance-scored memory, TODO tool, dry-run, config schema),
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

1. `git checkout phase-11-tasks` (or create `phase-12-docker` off it).
2. Re-read the roadmap: `C:\Users\scott\.claude\plans\i-want-to-further-sorted-milner.md`.
3. `go build ./... && go test ./... -race` to confirm a green baseline.
4. Start Phase 12 per the roadmap.
