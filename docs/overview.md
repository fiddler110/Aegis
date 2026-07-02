# Overview & Architecture

Aegis is an AI agent harness for **security, architecture, research, and development**, built in Go with a terminal-first interface. It is designed around local LLMs as the primary workflow while supporting cloud providers (Anthropic, OpenAI, Azure, Groq, OpenRouter) when needed.

## What Aegis Is

Aegis sits between you and a language model. You give it a task; it works through it by calling tools (reading files, running commands, searching the web), checking its own work, and asking for approval where needed. All of this happens in your terminal with a live streaming view of what the model is doing and why.

It borrows proven patterns from existing agents:
- **Provider abstraction and context compression** (from Hermes)
- **Persistent daemon + client sessions and Plan/Build modes** (from opencode)
- **Slash-command skills, subagents, permission modes, and file-based memory** (from Claude Code)

The result is a single, clean Go codebase you own and can extend.

## Daemon + Client Architecture

Aegis uses a **daemon + client** model. The daemon is a persistent background process that owns:

- Sessions (conversation history, SQLite-backed)
- The agent engine (model → tools → repeat loop)
- The tool registry
- The model adapter (provider connection)
- Permission gates and audit trail
- Memory and knowledge systems
- Background task manager

Clients connect to the daemon over a local HTTP API with server-sent events (SSE) on `127.0.0.1:4127`.

```
┌────────────────────────────────────────────────────────────────────┐
│                   Daemon (auto-start or aegis serve)               │
│                                                                    │
│  ┌──────────┐  ┌───────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │  Session  │  │   Agent   │  │   Tool   │  │    Provider      │  │
│  │   Store   │  │  Engine   │  │ Registry │  │    Adapter       │  │
│  │ (SQLite)  │  │  (loop)   │  │          │  │ (Anthropic/      │  │
│  └──────────┘  └─────┬─────┘  └────┬─────┘  │  OpenAI/Local)   │  │
│                      │             │         └──────────────────┘  │
│                      ▼             ▼                               │
│              ┌───────────────────────────┐                         │
│              │  Permission Gate + Hooks  │                         │
│              │   (audit.jsonl trail)     │                         │
│              └───────────────────────────┘                         │
│                                                                    │
│  ┌──────────┐  ┌───────────┐  ┌──────────┐  ┌──────────────────┐  │
│  │  Memory  │  │   Swarm   │  │  Sandbox  │  │  MCP / Plugins   │  │
│  │ & Skills │  │(subagents)│  │ (Docker/  │  │ (external tools) │  │
│  └──────────┘  └───────────┘  │  Podman)  │  └──────────────────┘  │
│                               └──────────┘                         │
└────────────────────┬───────────────────────────────────────────────┘
                     │ HTTP + SSE (127.0.0.1:4127)
        ┌────────────┼────────────┐
        ▼            ▼            ▼
   ┌─────────┐  ┌─────────┐  ┌──────────┐
   │   TUI   │  │  chat   │  │ dry-run  │
   │(dashboard│  │  (CLI)  │  │  (debug) │
   └─────────┘  └─────────┘  └──────────┘
```

**By default, `aegis` auto-starts the daemon in the same process** — no second terminal is needed. Run `aegis serve` explicitly if you want the daemon to persist across TUI restarts (for long-running background jobs or shared sessions).

## The Agent Loop

The core of Aegis is the engine loop in `internal/engine/`:

1. **Assemble context** — System prompt + conversation history + memory + repo map
2. **Call model** — Stream request to provider; receive events in real time
3. **Handle events**:
   - Text deltas → append to assistant message, stream to TUI
   - Tool-use request → dispatch to permission gate, then execute
   - Thinking blocks → preserve in history, dim display in TUI
   - Turn done → record trace, check for completion
4. **Tool dispatch**:
   - Check permission gate (mode + text rules + contextual security)
   - Run pre-hook (audit, can veto the call)
   - Execute the tool
   - Run post-hook (audit)
   - Append result to conversation
5. **Repeat** until the model produces a final answer or a stop condition triggers
6. **Output validation** — The final answer is checked by the guard before being shown

**Stop conditions:**
- Model produces an answer with no tool calls
- Maximum iterations reached (default 40)
- Identical-turn repetition detected (default 5 repeated turns)
- Cost budget exhausted
- Context compaction failed

## SSE Event Stream

Every daemon response is delivered as a stream of typed events. Clients (TUI, `aegis chat`, web UI, ACP) subscribe to this stream.

| Event | When it fires |
|-------|--------------|
| `text` | Incremental assistant text delta |
| `thinking` | Extended reasoning token (Anthropic Claude) |
| `tool_call` | Tool about to be executed |
| `tool_result` | Tool execution completed |
| `turn_done` | Model turn finished (carries token/cost data) |
| `steer` | Mid-run steering message injected |
| `trace` | Per-turn observability record |
| `guard` | Output validation result |
| `approval_request` | Waiting for user approval |
| `done` | Entire run finished |
| `error` | Run failed with error |

## Context Compaction

When the conversation grows large (by default, past 85% of the model's context window), Aegis automatically summarizes old turns:

1. Identifies "complete" turns (user question + assistant answer pairs) from the oldest part of the conversation
2. Summarizes them using the configured `small_model` (falls back to the main model)
3. Replaces those turns with the summary
4. Keeps full content for recent turns

This lets sessions continue indefinitely without hitting context limits while preserving the reasoning chain and decision history.

## Checkpoints and Rewind

Every user turn creates a checkpoint before the agent runs. A checkpoint captures:

- The conversation length at that point (so the transcript can be truncated back)
- A copy-on-write snapshot of any file the agent modifies (captured the first time each file is touched, before the modification)

Files larger than 16 MiB are not snapshotted. The snapshot reflects the **pre-turn** state, so restoring it undoes everything the agent did during that turn.

See [Session Management](sessions.md) for how to use `/rewind`.

## Memory Layers

Aegis has several memory systems that persist facts across sessions:

| System | Scope | Storage | Purpose |
|--------|-------|---------|---------|
| Project memory | Project | `.aegis/memory.md` | Facts the agent should always know about this project |
| User memory | Global | `~/.local/share/aegis/memory.md` | Facts that apply across all projects |
| Skills | Project or global | `*.md` files | Reusable procedures the agent can follow |
| Project knowledge base | Project | `.aegis/knowledge.db` | FTS5-indexed README, docs, and code comments |
| Long-term entity store | Global | `~/.local/share/aegis/longmem.db` | Cross-session structured facts about named entities |

See [Memory & Knowledge](memory-and-knowledge.md) for full detail.

## Permission Model

Every tool call goes through the permission gate before executing. The gate evaluates in this order:

1. **Text rules** — explicit `allow`/`deny` patterns win unconditionally
2. **Mode** — `plan` (read-only), `build` (write allowed, shell prompts), or `auto` (everything)
3. **Contextual security** — `egress_then_write`, `network_allowlist`

Every decision (allow, deny, or ask-and-approved) is written to an audit trail (`audit.jsonl`) alongside the tool inputs.

See [Permission System](permissions.md) for the full rule syntax.

## Package Map

```
cmd/aegis/                    Entry point

internal/
  engine/                     Agent loop (model → tools → repeat)
  provider/                   Normalized model interface
    anthropic/                Anthropic Messages API (SSE)
    openai/                   OpenAI-compatible API (SSE; also local LLMs)
  providerfactory/            Build adapters from config
  tool/builtin/               39 built-in tools
  permission/                 Plan/Build/Auto gates + approval
  compaction/                 Context summarization
  memory/                     Project/user memory + skills loader
  knowledge/                  SQLite FTS5 project knowledge base
  longmem/                    Long-term entity store
  checkpoint/                 Per-turn snapshots for rewind
  session/                    SQLite session store
  server/                     Daemon: HTTP + SSE API
  client/                     HTTP client for daemon API
  tui/                        Bubble Tea terminal dashboard
  api/                        Shared wire types
  config/                     Layered config loading
  cost/                       Token tracking + budget enforcement
  trace/                      Per-turn observability
  hooks/                      Pre/post tool hooks + JSONL audit
  persona/                    17 built-in personas
  guard/                      Output validation
  sandbox/                    Local/container shell execution backends
  filetracker/                File staleness detection (prevents overwrites)
  lsp/                        LSP client manager
  mcp/                        MCP client (stdio + HTTP/SSE)
  plugins/                    External process tool plugins
  commands/                   Custom slash command loader
  agentdef/                   Custom agent definition loader
  task/                       Background task manager (SQLite)
  cron/                       Recurring job scheduler
  swarm/                      Multi-agent coordination
  security/                   Semgrep, Trivy, Gitleaks runners
  diagram/                    Kroki + local CLI diagram renderers
  repomap/                    Repository structure index
  discover/                   Local model server auto-discovery
  share/                      Session export (HTML/MD/JSON)
  worktree/                   Git worktree management
  bundle/                     Plugin bundle installer
  modelcatalog/               Curated model information
  logging/                    Structured logging (slog)
  cli/                        Cobra command tree
```
