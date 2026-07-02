# Multi-Agent & Background Tasks

Aegis supports multi-agent orchestration (spawning sub-agents for parallel or sequential work), background task management, recurring scheduled jobs, and user-launched parallel sessions.

---

## Sub-Agents (Swarm)

The `agent` tool lets the model spawn sub-agents — independent agents that run their own engine loops, have their own tool registries, and operate within a scoped permission boundary.

### When to use sub-agents

Sub-agents are useful when:
- A task can be decomposed into independent subtasks (parallel search, parallel review)
- A subtask needs a different persona or permission mode than the parent
- A long-running background task should not block the parent

### Calling the `agent` tool

```json
{
  "prompt": "Review all files in internal/permission/ for correctness and security",
  "subagent_type": "security-architect",   // optional: persona or custom agent name
  "mode": "plan",                          // optional: permission mode (≤ parent mode)
  "background": false                      // false: wait for result; true: return task ID
}
```

**Execution modes:**
- `background: false` (default) — the parent waits for the sub-agent to finish and receives its answer
- `background: true` — returns a task ID immediately; parent can continue and check back with `task_get`

### Permission scoping

Sub-agents inherit the parent's permission context but **cannot exceed it**. If the parent is in `plan` mode, spawning a sub-agent with `mode: build` will be clamped to `plan`.

### Recursion limit

Sub-agents are limited to 3 levels of nesting to prevent runaway spawning.

### Viewing active agents

Press `Ctrl+T` in the TUI to open the sub-agent panel, showing all active agents with their status (running, done, failed) and task description.

---

## Swarm Execution Backends

Configured in `config.yaml`:

```yaml
swarm:
  backend: in_process    # default: goroutines in same process
  # backend: subprocess  # isolated processes (more isolation, slower startup)
```

**`in_process`** — Sub-agents run as goroutines. Fastest, lowest overhead. Sub-agents share the same memory space as the parent (no data isolation).

**`subprocess`** — Each sub-agent runs as a separate headless process. More isolation; useful when sub-agents run untrusted or conflicting operations. Slower to start.

---

## Orchestration Patterns

### Sequential

Parent spawns agents one at a time, passing each agent's output to the next:

```
Parent:
  → Agent A: "Identify all files that handle authentication"
  ← Result: ["internal/auth/auth.go", "internal/middleware/jwt.go"]
  → Agent B: "Review internal/auth/auth.go for security issues"
  ← Result: "Found 2 issues..."
  → Agent C: "Fix the issues identified in the review"
```

### Parallel

Parent spawns multiple agents with `background: true`, then collects results:

```
Parent:
  → Agent A (background): "Review internal/engine/"
  → Agent B (background): "Review internal/permission/"
  → Agent C (background): "Review internal/provider/"
  ← Collect: task_output(A), task_output(B), task_output(C)
  → Synthesize findings
```

### Loop

The same agent is re-run until a condition is met:

```
Parent:
  → Agent: "Run the test suite and fix any failures"
  ← If tests still failing: repeat
  ← If tests pass: done
```

---

## Parallel User Sessions

`aegis parallel` runs independent top-level sessions concurrently (distinct from sub-agents):

```bash
aegis parallel "fix the failing tests" "update the API documentation" --yes
aegis parallel "security review of auth module" "dependency audit" --mode plan
```

Each prompt gets its own session. Progress is interleaved in the terminal. When all finish, a per-session summary is printed with resume hints:

```
Session abc12345: ✓ done — "fix the failing tests"
  Resume: aegis --resume abc12345

Session def67890: ✓ done — "update the API documentation"
  Resume: aegis --resume def67890
```

**Flags:**
- `--yes` — auto-approve all tool calls (required for truly unattended fan-out)
- `--mode` — permission mode for all sessions

These are user-launched sessions, not sub-agents. Each runs independently and persists in the session store.

---

## Background Tasks

The task manager lets commands run in the background, freeing the agent to continue other work.

### The `shell` tool with `background: true`

```json
{
  "command": "npm run build && npm test",
  "background": true,
  "title": "Build and test"
}
```

Returns a task ID (e.g., `task-abc123`) immediately. The command runs in the background.

### Task management tools

**`task_create`** — Launch a command as a background job:
```json
{"command": "cargo build --release", "title": "Release build"}
```

**`task_list`** — List all jobs (newest first):
```json
{}
```
Returns: id, state (running/done/failed), title, created_at.

**`task_get`** — Get status and output tail:
```json
{"id": "task-abc123"}
```

**`task_output`** — Get full output:
```json
{"id": "task-abc123"}
```

**`task_stop`** — Cancel a running job:
```json
{"id": "task-abc123"}
```

**`task_update`** — Rename a job:
```json
{"id": "task-abc123", "title": "Release build (v2.1.0)"}
```

### Monitoring background jobs

```bash
aegis runs         # list all in-flight runs across all sessions
```

Or check from inside the TUI with `Ctrl+T` (shows sub-agents and background tasks).

---

## Cron Scheduling

The cron scheduler runs recurring jobs on a standard 5-field cron schedule.

### Cron tools

**`cron_create`** — Create a job:
```json
{
  "schedule": "0 9 * * 1-5",
  "command": "aegis scan .",
  "title": "Daily security scan"
}
```

Cron expression format: `minute hour day-of-month month day-of-week`

| Expression | Meaning |
|-----------|---------|
| `0 9 * * 1-5` | 9am, Monday–Friday |
| `*/15 * * * *` | Every 15 minutes |
| `0 0 * * *` | Midnight daily |
| `0 6 * * 1` | 6am every Monday |

**`cron_list`** — List all jobs:
```json
{}
```

**`cron_delete`** — Delete a job:
```json
{"id": "cron-abc123"}
```

**`cron_toggle`** — Enable or disable without deleting:
```json
{"id": "cron-abc123", "enabled": false}
```

### Cron job output

Cron jobs are executed as background tasks. Use `task_list` and `task_output` to retrieve their output.

---

## Background Sessions

A TUI session can be detached and continue running after you close the terminal:

```
/detach on
```

The agent keeps processing even with the TUI closed. Reconnect with:
```bash
aegis --resume <session-id>
```

Check progress without reconnecting:
```bash
aegis bg events <session-id>
```

---

## Worktree Isolation

For safe parallel work by multiple agents on the same repo:

```bash
aegis worktree add feature-x --branch feature-x
aegis worktree add feature-y --branch feature-y
```

Creates git worktrees under `.aegis/worktrees/`. Run `aegis` from inside a worktree to scope the session to that checkout. Agents in separate worktrees operate on independent file trees and cannot overwrite each other's changes.

```bash
# In one terminal
cd .aegis/worktrees/feature-x
aegis --persona developer --mode build

# In another terminal
cd .aegis/worktrees/feature-y
aegis --persona developer --mode build
```

**Caveats:**
- Worktrees share absolute paths for ports, databases, and caches — these can still conflict
- Clean up when done: `aegis worktree remove feature-x` or `aegis worktree prune`

---

## Inter-Agent Communication

Sub-agents spawned via the `agent` tool can communicate through a mailbox system when using the `subprocess` backend. This is an advanced feature for multi-agent workflows where agents need to coordinate.

In the default `in_process` backend, sub-agent results are returned directly to the parent through Go channels.

---

## Practical Example: Parallel Code Review

The parent agent spawns three sub-agents in parallel to review different packages, then synthesizes their findings:

```
You: Review the entire codebase for security issues. Use parallel sub-agents for efficiency.

Agent:
  I'll split the review across sub-agents by package.

  [task_create] Review internal/permission/
  [task_create] Review internal/server/
  [task_create] Review internal/tool/builtin/

  [checking each task until done...]
  [task_output: permission review]
  [task_output: server review]
  [task_output: builtin review]

  Here's the synthesized security assessment:

  Critical:
  - internal/server/server.go:142 — Missing rate limiting on POST /sessions
  ...
```
