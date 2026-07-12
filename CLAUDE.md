# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Build & Run

```bash
# Build and install the binary (macOS)
./build-macos.sh

# Build manually (outputs ./aegis, then installs)
go build -o ./aegis ./cmd/aegis

# Run directly from source
go run ./cmd/aegis

# First-time setup
aegis --first-init
export OPENAI_API_KEY="ollama"   # or ANTHROPIC_API_KEY for cloud
```

The embedded web UI (`aegis ui`, served at `/ui`) is built from
`internal/server/webui/frontend` (Vite + Preact + TypeScript); its output
(`internal/server/webui/dist/`) is committed to git and embedded via
`go:embed`, so `go build`/`go run ./cmd/aegis` need no Node.js. Only rebuild
it when editing frontend source:

```bash
npm --prefix internal/server/webui/frontend ci
npm --prefix internal/server/webui/frontend run build   # regenerates dist/ — commit the result
```

## Testing

```bash
# Run all tests
go test ./...

# Run a specific package
go test ./internal/engine/...

# Run a single test
go test ./internal/engine/... -run TestBudget

# Run with race detector
go test -race ./...

# Regenerate an eval golden transcript after an intentional behavior change
AEGIS_EVAL_UPDATE=1 go test ./internal/eval/... -run TestScenario_ToolRoundTrip

# Regenerate the security-scan regression golden file (same convention, P11.9)
AEGIS_EVAL_UPDATE=1 go test ./internal/security/... -run TestScanRegressionAcrossRecordedOutputs

# Live-model eval tier: rubric-judged prompt/persona quality checks against a
# real local model (not part of `go test ./...` — needs a reachable model
# server). On-demand only — the CI workflow (.github/workflows/nightly-eval.yml)
# is workflow_dispatch-only by decision, never scheduled. To run locally:
ollama pull llama3.2
go test -tags live_eval ./internal/eval/... -run TestLiveModelQuality -v
```

## Architecture

Aegis is a **daemon + client** architecture. The single `aegis` binary can act as either:
- A **daemon** (`aegis serve`) — owns sessions, the model adapter, tool registry, and runs the agent engine over a local HTTP API with SSE streaming
- A **TUI client** (`aegis`) — auto-starts an embedded daemon in-process if none is reachable, then connects a Bubbletea terminal UI to it

### Request flow

```
TUI (internal/tui) → HTTP client (internal/client) → daemon HTTP server (internal/server)
  → engine.Run (internal/engine) → provider.Adapter.Stream (internal/provider/*)
    ↕ tools executed via tool.Registry (internal/tool/builtin/*)
```

### Key packages

| Package | Role |
|---------|------|
| `internal/engine` | Core agent loop: calls the model, dispatches tool calls, handles compaction, output guard, loop detection, budget enforcement |
| `internal/server` | HTTP daemon; wires sessions, tools, permissions, personas, swarm, MCP, cron, checkpoints |
| `internal/provider` | Normalized `Adapter` interface (stream-based) + message types; adapters in `provider/anthropic` and `provider/openai` |
| `internal/session` | SQLite-backed session store (conversations, turn traces, cost) |
| `internal/tool` | `Tool` interface + `Registry` (register/expose separation lets permission modes gate capability without unregistering) |
| `internal/tool/builtin` | All 39+ built-in tools (file ops, git, shell, web, memory, LSP, security scan, diagram, cron, agent spawning, etc.) |
| `internal/permission` | Three modes: `plan` (read-only), `build` (read+write, execute gated), `auto` (all allowed); text-based allow/deny rules; `PersonaToolGate` advisory (never-enforcing) check on a persona's declared `Tools` |
| `internal/persona` | 22 built-in named system prompts (general, security, developer, SRE, red-team, security-critic/security-arbiter and generic critic/arbiter debate roles, etc.); custom personas are `.md` files with YAML frontmatter, hot-reloaded via a signature-cached `Refresh` |
| `internal/skills` | Progressive-disclosure skills: project/user `.md`/bundled-directory skill files, plus skills embedded in the binary (`go:embed`) that stay dormant until named in config/CLI/TUI |
| `internal/swarm` | Multi-agent coordination: spawns sub-agents as goroutines (`in_process`) or subprocesses; file-based mailbox for inter-agent messaging |
| `internal/debate` | Multi-agent-debate (MAD) primitive (P12): propose/critique/rebut/arbitrate over a claim via a caller-supplied `RunFunc`, decoupled from swarm/engine the same way swarm is decoupled from engine; evidence-citation check (P12.3) and shared-tracker budget bound (P12.6) live here; `Config.Domain` (`security`/`generic`) selects the default persona trio and `WithFiles` grounds the claim in specific files, so the same primitive covers security findings and non-security document/plan review (see [docs/debate.md](docs/debate.md)) |
| `internal/compaction` | Context compaction — summarizes old turns when the conversation approaches the model's context window |
| `internal/checkpoint` | Per-turn restore points for `/rewind` |
| `internal/memory` | Project-level and user-level persistent memory; relevance scoring for context injection |
| `internal/tui` | Bubbletea TUI: timeline, streaming, dialog, persona/session pickers, slash commands, cost display |
| `internal/config` | Layered config (defaults → `~/.config/aegis/config.yaml` → `.aegis/config.yaml` → `AEGIS_*` env vars) |
| `internal/mcp` | MCP client (stdio + HTTP/SSE) — Aegis calling *out* to external MCP servers; registered tools appear alongside builtins |
| `internal/mcpserver` | MCP server (`aegis mcp-serve`) — the reverse direction: exposes Aegis sessions as MCP tools (`aegis_prompt`, `aegis_new_session`, `aegis_list_sessions`) to other MCP-speaking harnesses |
| `internal/acp` | ACP JSON-RPC server for editor integrations (Zed, Neovim) |
| `internal/sandbox` | Pluggable execution sandbox: local, Docker, Podman, WSL containers, Apple Containers |
| `internal/cron` | Cron scheduler for background tasks |
| `internal/guard` | Output validation — calls a second model pass against a rubric or JSON schema |
| `internal/eval` | Scenario-based agent-behavior regression harness: scripted multi-turn conversations run against a real engine (deterministic adapter, no live model) with tool-call/text/error assertions and golden transcripts |

### Provider model

`provider.Adapter` is the single seam between the engine and any LLM backend. It exposes one method:

```go
Stream(ctx context.Context, req Request) (<-chan Event, error)
```

All message types (TextBlock, ToolUseBlock, ToolResultBlock, ThinkingBlock) are defined in `internal/provider/provider.go`. The Anthropic adapter maps these natively; the OpenAI adapter translates to/from chat-completions format.

### Tool capability model

Every tool declares a `Capability` (`read`, `write`, `execute`, `network`, `spawn`). The permission gate consults this before execution. In `engine.runTools`, read/network tools run concurrently while write/execute tools are serialized via `sync.RWMutex`.

### Configuration layers

Precedence (lowest → highest): built-in defaults → `~/.config/aegis/config.yaml` → `.aegis/config.yaml` (project-level) → `AEGIS_*` env vars. Secrets (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) come only from the environment. `.aegis/.env` is supported for local secrets without environment pollution.

### Persona system

Personas are `.md` files whose YAML frontmatter (`description`, `model`, `mode`, `tools`, `rules`, `output_guard`) carries overrides; the name is the filename stem. Built-ins live in `internal/persona/builtin/*.md`, embedded via `go:embed` and parsed at package init (`internal/persona/builtin.go`) into the same `Persona` struct as user/project personas — only `Loaded` (false for built-ins) and `Path` differ. Custom personas are `.md` files under project `.aegis/personas/` or user `~/.aegis/personas/`, parsed by `internal/persona/load.go`. A persona can pin a model, declare an advisory tool list, merge permission rules, and override the output guard rubric. The daemon hot-reloads on-disk persona files (`persona.Refresh`, called from persona-touching handlers) — no restart needed; built-ins only change on rebuild. Switching persona mid-session (`PATCH /sessions/{id}` with `persona`) applies the full profile, not just the system prompt. `aegis persona list|show|new|use` manages persona files and the project's `default_persona` (config key; falls back to `general`) from the CLI.

Each built-in persona's `Tools` field is **advisory, never enforced**: `permission.PersonaToolGate` (wrapped outermost in `server.newEngine`) logs a call outside the list and routes it through the same `Approver` used for capability decisions — warn-and-allow under a non-interactive approver, a confirmation prompt under the TUI's — but the real security boundary (mode, rules, contextual gates) is untouched and always still applies. `general` deliberately leaves `Tools` empty (no restriction at all).

### Skills system

Skills (`internal/skills`) are progressive-disclosure playbooks: at session start only a `name — description` line is injected per skill into `<skills_available>`; the model loads the full body on demand via the `skill` tool. A skill is a `.md` file (or a directory bundling a `SKILL.md` manifest with companion assets like templates/scripts, referenced via a generated `<skill_assets>` manifest) under project `.aegis/skills/` or user `~/.aegis/skills/`. Skills without a frontmatter `description:` fall back to eager injection for backward compatibility.

Aegis also ships several skills **embedded in the binary** (`internal/skills/builtin/`, extracted via `go:embed` and materialized to `<data_dir>/builtin-skills/` at daemon startup by `skills.MaterializeBuiltins`) — content-review, html-report, security-audit, architecture-diagram, debug-investigation, redteam-engagement, threat-modeling, latex-report, deep-research. These stay **dormant by default** (zero system-prompt cost) until named in the `skills.builtin_enabled` config list, via `aegis skills enable <name> [--global]`, or the `/skills enable <name> [global]` TUI command; disable the same way. Precedence when a name collides: project skill file > user skill file > embedded built-in.
