# Agent Harness

A personal AI agent harness for **research, documentation, coding, and security-platform architecture**, built from scratch in Go with a terminal-first interface. Designed to work with **local LLMs** (Ollama, LM Studio, LiteLLM) as the primary workflow, with the ability to connect to cloud providers (Anthropic, OpenAI) when needed.

It borrows proven patterns from existing agents — provider abstraction and context compression (Hermes), a persistent daemon + client sessions and Plan/Build modes (opencode), slash-command skills, subagents, permission modes and file-based memory (Claude Code), and sandboxed local tooling (OpenClaw) — while staying a clean, single codebase you own.

## Table of Contents

- [Architecture](#architecture)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
  - [Using a Local LLM](#using-a-local-llm)
  - [Using Cloud Providers](#using-cloud-providers)
- [Usage](#usage)
  - [Starting the Daemon](#starting-the-daemon)
  - [Launching the TUI](#launching-the-tui)
  - [Non-Interactive CLI](#non-interactive-cli)
  - [Other Commands](#other-commands)
- [Capabilities](#capabilities)
- [Configuration](#configuration)
- [Extensibility](#extensibility)
  - [MCP Servers](#mcp-servers)
  - [Custom Commands](#custom-commands)
  - [Custom Agent Definitions](#custom-agent-definitions)
  - [Process Plugins](#process-plugins)
- [Project Structure](#project-structure)
- [Testing](#testing)

## Architecture

Agent Harness uses a **daemon + client** architecture. A background daemon (`harness serve`) owns sessions, the agent engine, tool registry, and model adapter. Clients (the interactive TUI and the `chat` CLI command) connect to it over a local HTTP API with server-sent events (SSE), so sessions survive client restarts and can be resumed.

```
┌────────────────────────────────────────────────────────────────────┐
│                        Daemon (harness serve)                      │
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
│  │ & Skills │  │ (subagents)│  │ (Docker/  │  │  (external tools)│  │
│  └──────────┘  └───────────┘  │  Podman)  │  └──────────────────┘  │
│                               └──────────┘                         │
└────────────────────┬───────────────────────────────────────────────┘
                     │ HTTP + SSE (127.0.0.1:4127)
        ┌────────────┼────────────┐
        ▼            ▼            ▼
   ┌─────────┐  ┌─────────┐  ┌──────────┐
   │   TUI   │  │  chat   │  │ dry-run  │
   │ (Bubble │  │  (CLI)  │  │  (debug) │
   │   Tea)  │  │         │  │          │
   └─────────┘  └─────────┘  └──────────┘
```

**Core agent loop**: The engine sends the conversation to the model, receives streamed events (text deltas, tool-use requests), dispatches tool calls through the permission gate, appends results, and repeats until the model produces a final answer or the run is interrupted. Context compaction automatically summarizes old turns when the conversation grows large.

## Getting Started

### Prerequisites

- **Go 1.25+** — [Install Go](https://go.dev/dl/)
- **Git** — for cloning the repository
- A local LLM server **or** a cloud API key (see below)

### Installation

Clone and build the harness:

**Windows (PowerShell)**
```powershell
git clone https://github.com/scottymacleod/agentharness.git
cd agentharness
go build -o harness.exe ./cmd/harness
```

**Linux / macOS**
```bash
git clone https://github.com/scottymacleod/agentharness.git
cd agentharness
go build -o harness ./cmd/harness
```

Optionally move the binary onto your PATH:

**Windows (PowerShell)**
```powershell
Copy-Item harness.exe "$env:USERPROFILE\go\bin\harness.exe"
```

**Linux / macOS**
```bash
sudo mv harness /usr/local/bin/harness
# or: mv harness ~/go/bin/harness
```

### Using a Local LLM

Local LLM usage is the primary focus of this project. The harness auto-discovers models running on your machine from three supported servers:

| Server | Default Port | Install |
|--------|-------------|---------|
| [Ollama](https://ollama.com) | `localhost:11434` | `curl -fsSL https://ollama.com/install.sh \| sh` (Linux/macOS) or download from ollama.com (Windows) |
| [LM Studio](https://lmstudio.ai) | `localhost:1234` | Download from lmstudio.ai |
| [LiteLLM](https://github.com/BerriAI/litellm) | `localhost:4000` | `pip install litellm` |

**Step 1 — Start your local model server**

Example with Ollama:
```bash
# Pull a model (do this once)
ollama pull llama3.1
# or a larger model for better tool-use support:
ollama pull qwen2.5:32b

# Ollama runs automatically as a service after install,
# or start it manually:
ollama serve
```

Example with LM Studio:
1. Download and install LM Studio
2. Load a model from the Discover tab
3. Start the local server from the Local Server tab (runs on port 1234)

**Step 2 — Configure the harness to use the local model**

The harness uses the OpenAI-compatible API that local servers expose. Set the provider to `openai`, point `base_url` at your local server, and set the model name:

**Windows (PowerShell)**
```powershell
# Set environment variables for the session
$env:OPENAI_API_KEY = "not-needed"
$env:AGENTHARNESS_PROVIDER_DEFAULT = "openai"
$env:AGENTHARNESS_PROVIDER_BASE_URL = "http://localhost:11434/v1"   # Ollama
$env:AGENTHARNESS_PROVIDER_MODEL = "llama3.1"
```

**Linux / macOS**
```bash
export OPENAI_API_KEY="not-needed"           # required by the adapter but not checked by local servers
export AGENTHARNESS_PROVIDER_DEFAULT="openai"
export AGENTHARNESS_PROVIDER_BASE_URL="http://localhost:11434/v1"   # Ollama
export AGENTHARNESS_PROVIDER_MODEL="llama3.1"
```

Or configure it permanently in the config file:

**Windows**: `%AppData%\agentharness\config.yaml`
**Linux**: `~/.config/agentharness/config.yaml`
**macOS**: `~/Library/Application Support/agentharness/config.yaml`

```yaml
provider:
  default: openai
  base_url: "http://localhost:11434/v1"   # Ollama endpoint
  model: llama3.1                          # model name as reported by the server
```

> **Note**: Set `OPENAI_API_KEY` to any non-empty string (e.g. `"not-needed"`) — the adapter requires it to be set, but local servers do not validate it.

**Step 3 — Discover available models**

The harness can probe for all locally running model servers and list available models:
```bash
# In a running session, the agent has a list_models tool, or from the CLI:
harness chat "list available local models" --mode plan
```

**Base URLs for common local servers:**

| Server | Base URL |
|--------|----------|
| Ollama | `http://localhost:11434/v1` |
| LM Studio | `http://localhost:1234/v1` |
| LiteLLM | `http://localhost:4000/v1` |

### Using Cloud Providers

Cloud providers can be used alongside or instead of local models.

**Anthropic (Claude)**
```bash
# Linux / macOS
export ANTHROPIC_API_KEY="sk-ant-..."

# Windows (PowerShell)
$env:ANTHROPIC_API_KEY = "sk-ant-..."
```

**OpenAI**
```bash
# Linux / macOS
export OPENAI_API_KEY="sk-..."
export AGENTHARNESS_PROVIDER_DEFAULT="openai"

# Windows (PowerShell)
$env:OPENAI_API_KEY = "sk-..."
$env:AGENTHARNESS_PROVIDER_DEFAULT = "openai"
```

The default provider is `anthropic` with model `claude-opus-4-8`. Override via config or environment variables.

## Usage

### Starting the Daemon

The daemon must be running before launching the TUI or sending chat commands. It manages sessions, the agent engine, tool registry, and all integrations.

```bash
# Start in a separate terminal (logs to data dir by default)
harness serve

# Or with log output to stderr for debugging
harness serve --foreground
```

The daemon listens on `127.0.0.1:4127` by default and generates an auth token stored in the data directory.

### Launching the TUI

```bash
# Default: plan mode (read-only), general persona
harness

# Build mode (can create/edit/delete files, run shell commands)
harness --mode build

# Security architect persona
harness --persona security --mode build

# Resume an existing session
harness --resume <session-id>
```

The TUI is built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and renders a scrollable conversation with a text input area. It streams model responses in real time and shows tool call status inline.

### Non-Interactive CLI

For scripting or single-shot queries without the TUI:

```bash
harness chat "summarize main.go" --mode build
harness chat "what security issues exist in this repo?" --mode plan

# Auto-approve all tool calls (unattended)
harness chat "refactor the config package" --mode build --yes
```

### Other Commands

```bash
# Preview resolved config, tools, memory, and context without calling the model
harness dry-run

# Run security scanners (semgrep, trivy, gitleaks)
harness scan ./path

# Render a diagram
harness diagram --type mermaid --out architecture.svg < diagram.mmd

# List and manage sessions
harness sessions list

# Show current configuration
harness config
```

## Capabilities

### Coding and Research
File read/write/edit/multi-edit, glob, grep, sandboxed shell execution, web fetch/search — all confined to the workspace root. File staleness tracking prevents edits to files modified externally since they were last read.

### Permission Modes
- **Plan** (default) — read-only; the agent can search, read files, and answer questions but cannot modify anything.
- **Build** — the agent can create, edit, and delete files, execute shell commands, and perform write operations.

Shell and execute commands are gated by an approval prompt unless `auto_approve_exec` is enabled. Every tool call is recorded to an audit trail (`audit.jsonl`) with timestamps and inputs.

### Multi-Agent Orchestration (Swarm)
The `agent` tool lets the model spawn sub-agents ("teammates") that run independently. Sub-agents can execute in-process (goroutines) or as separate headless processes. Each sub-agent has its own permission scope (cannot exceed the parent's), and a file-based mailbox enables inter-agent communication. A recursion-depth guard prevents runaway spawning.

### Background Tasks and Scheduling
Shell commands and agent tasks can run in the background via the task manager (SQLite-backed). A built-in cron scheduler supports recurring jobs with standard cron expressions. Task tools: `task_create`, `task_status`, `task_list`, `task_cancel`, `task_output`, `task_wait`.

### Memory and Skills
- `remember` — persists facts into user-scoped or project-scoped memory files, loaded into the system prompt on every turn.
- `save_skill` — saves reusable procedures as markdown files, also injected into context.
- Relevance scoring ranks memory entries by query similarity so the most useful context surfaces first.

### Planning Tools
`todo_add`, `todo_update`, `todo_list` — a lightweight in-session planning surface for tracking multi-step work. `ask_user` collects structured input (free text, single-choice, multi-choice).

### Security Architect
With `--persona security`, the system prompt drives capability research, STRIDE/LINDDUN threat modeling, and C4/Mermaid architecture diagramming. The `security_scan` tool runs semgrep, trivy, and gitleaks and produces a unified findings report.

### Diagrams
`render_diagram` and `harness diagram` render Mermaid, PlantUML, C4, Graphviz, and more to SVG/PNG via [Kroki](https://kroki.io) (with local CLI fallback). Draw.io export is also supported.

### Code Intelligence (LSP)
When LSP servers are configured, tools like `lsp_diagnostics`, `lsp_references`, and `lsp_definition` give the agent IDE-level code understanding.

### Model Discovery
The `list_models` tool probes `localhost` for Ollama (`:11434`), LM Studio (`:1234`), and LiteLLM (`:4000`) and reports every model available — useful for switching models mid-session or verifying your local setup.

### Contextual Security Policies
- `egress_then_write` — requires explicit approval for write operations that follow network access (prevents exfiltrate-then-modify patterns).
- `network_allowlist` — restricts outbound network calls to listed domains.
- All policy decisions are recorded to the audit trail.

### Sandboxed Execution
Shell commands can run in a local sandbox (default) or inside containers:
- **Docker** and **Podman** on Linux/macOS/Windows
- **Apple Containers** on macOS
- Network isolation, configurable image, and path validation prevent workspace escapes.

### Cost Tracking
Token usage (including cache hits for Anthropic) is tracked per turn. A configurable `budget_usd` limit halts a run when estimated spend exceeds the threshold.

## Configuration

Configuration is resolved with precedence (lowest to highest):

1. **Built-in defaults**
2. **Global config** — `<user-config-dir>/agentharness/config.yaml`
3. **Project config** — `./.agentharness/config.yaml`
4. **Environment variables** — `AGENTHARNESS_*` (underscores map to dots: `AGENTHARNESS_PROVIDER_MODEL` → `provider.model`)

API keys are read from the environment only (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) and never written to config files.

**Config file locations by platform:**

| Platform | Global Config Path |
|----------|-------------------|
| Windows | `%AppData%\agentharness\config.yaml` |
| Linux | `~/.config/agentharness/config.yaml` |
| macOS | `~/Library/Application Support/agentharness/config.yaml` |

### Full Config Reference

```yaml
# Provider settings
provider:
  default: openai              # "anthropic" or "openai" (use "openai" for local LLMs)
  model: llama3.1              # model ID as known by the provider/server
  base_url: "http://localhost:11434/v1"  # API base URL (required for local LLMs)
  max_tokens: 8192             # response token cap
  max_retries: 4               # transient failure retries (0 = disabled)

# Permission defaults
permission:
  mode: plan                   # "plan" (read-only) or "build" (can mutate)
  auto_approve_exec: false     # skip approval for shell commands in build mode

# Cost tracking
cost:
  budget_usd: 0                # abort when estimated cost exceeds this (0 = unlimited)

# Daemon
server:
  addr: "127.0.0.1:4127"      # listen address

# Multi-agent
swarm:
  backend: in_process          # "in_process" or "subprocess"

# Command sandbox
sandbox:
  backend: local               # "local" or "container"
  runtime: ""                  # "docker", "podman", "container" (Apple); empty = auto-detect
  image: "ubuntu:22.04"        # container image for sandbox
  network: false               # allow network inside containers

# Contextual security policies
security:
  egress_then_write: false     # require approval for writes after network access
  network_allowlist: []        # restrict network to these domains (empty = unrestricted)

# Diagram rendering
diagram:
  kroki_url: "https://kroki.io"

# LSP servers for code intelligence
lsp:
  - name: gopls
    command: gopls
    args: ["-remote=auto"]
    extensions: [".go"]

# MCP servers (stdio or HTTP)
mcp:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]

# External process tool plugins
plugins:
  - name: my-linter
    description: "Run custom linting"
    command: my-lint
    args: ["--json"]
    input_schema: '{"type":"object","properties":{"path":{"type":"string"}}}'
    capability: read
    timeout_sec: 30
```

## Extensibility

### MCP Servers

The harness consumes external [Model Context Protocol](https://modelcontextprotocol.io/) servers as additional tools. Servers can connect via stdio (launched as child processes) or HTTP/SSE. Configure them in the `mcp[]` array.

```yaml
mcp:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
  - name: github
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "ghp_..."
```

### Custom Commands

Drop markdown files into the commands directory to create custom slash commands. Files use YAML frontmatter for metadata and a body template with `{{arg}}` placeholders.

**Locations** (project overrides global):
- Global: `<data-dir>/commands/*.md`
- Project: `.agentharness/commands/*.md`

```markdown
---
name: review
description: Review code changes for a given file
args: [file]
---
Please review the code in {{file}} for bugs, security issues, and style problems.
Provide specific line-number references for each finding.
```

### Custom Agent Definitions

Drop markdown files into the agents directory to define reusable agent personas. The YAML frontmatter specifies the name, mode, and allowed tools; the body becomes the system prompt.

**Locations** (project overrides global):
- Global: `<data-dir>/agents/*.md`
- Project: `.agentharness/agents/*.md`

```markdown
---
name: reviewer
description: Code review specialist
mode: plan
tools: [read, glob, grep]
---
You are a code review specialist. Analyze code for correctness, security,
performance, and maintainability. Cite specific line numbers.
```

### Process Plugins

External commands can be registered as tools via the `plugins[]` config. The harness pipes tool input as JSON to stdin and captures stdout as the result.

```yaml
plugins:
  - name: check_types
    description: "Run TypeScript type checking"
    command: npx
    args: ["tsc", "--noEmit", "--pretty"]
    input_schema: '{"type":"object","properties":{"path":{"type":"string"}}}'
    capability: read
    timeout_sec: 60
```

## Project Structure

```
cmd/harness/               CLI entry point
internal/
  engine/                  Agent loop: model → tools → repeat, with gating, hooks, compaction
  provider/                Normalized model interface (Adapter)
    anthropic/             Anthropic Messages API adapter (SSE streaming)
    openai/                OpenAI chat-completions adapter (SSE streaming, also for local LLMs)
  providerfactory/         Build an adapter from config (single code path for daemon + CLI)
  tool/                    Tool registry (registration, exposure, capability tagging)
    builtin/               All built-in tools:
                             File: read, write, edit, multi_edit, glob, grep
                             Shell: shell (with background + sandbox support)
                             Web: fetch, search
                             Code: lsp_diagnostics, lsp_references, lsp_definition
                             Security: security_scan
                             Diagrams: render_diagram
                             Memory: remember, save_skill
                             Tasks: task_create/status/list/cancel/output/wait
                             Cron: cron_create/list/delete
                             Planning: todo_add/update/list, ask_user
                             Models: list_models
                             Agents: agent (spawn sub-agents)
  permission/              Plan/Build policies + approval gate + contextual security rules
  compaction/              Token-budget summarization of old conversation turns
  memory/                  File-based memory + skills, relevance scoring, context discovery
  security/                Semgrep, Trivy, Gitleaks runners + normalized findings
  diagram/                 Kroki + local CLI renderers (Mermaid, PlantUML, C4, Graphviz, draw.io)
  persona/                 System prompts: general + security-architect
  hooks/                   Pre/post tool-call hooks + JSONL audit trail
  mcp/                     MCP client (JSON-RPC/stdio + HTTP/SSE transports)
  session/                 SQLite session store
  server/                  Daemon: HTTP + SSE API
  client/                  HTTP client for the daemon API
  tui/                     Bubble Tea terminal UI (streaming, scrollable conversation)
  api/                     Shared API types (request/response structs)
  config/                  Layered config loading (defaults → global → project → env)
  cost/                    Token-based cost tracking + budget enforcement
  swarm/                   Multi-agent coordination: identities, mailbox, registry, backends
  sandbox/                 Pluggable sandbox: local, Docker, Podman, Apple Containers
  filetracker/             File staleness detection (reject edits to externally modified files)
  lsp/                     LSP client manager (lifecycle, diagnostics, references)
  discover/                Auto-discovery of local model servers (Ollama, LM Studio, LiteLLM)
  task/                    Background task manager (SQLite-backed)
  cron/                    Recurring job scheduler (cron expressions)
  commands/                Custom slash command loader (markdown with YAML frontmatter)
  agentdef/                Custom agent definition loader (markdown with YAML frontmatter)
  plugins/                 External process tool plugin loader
  logging/                 Structured logging setup (slog)
  cli/                     Cobra command tree (root, serve, chat, scan, diagram, sessions, etc.)
```

## Testing

Every package has unit tests. Provider adapters are tested against the real SSE wire format via `httptest`, the daemon end-to-end with a mock adapter, and the MCP client against an in-memory fake server.

```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/engine/...
go test ./internal/provider/anthropic/...

# With verbose output
go test -v ./...

# Live model calls (requires an API key or local server running)
OPENAI_API_KEY="not-needed" go test -run TestLive ./internal/provider/openai/...
```

## License

This is a personal project. See [LICENSE](LICENSE) for details.
