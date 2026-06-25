# Aegis

An AI agent harness for **security, architecture, research, and development**, built from scratch in Go with a terminal-first interface. Designed to work with **local LLMs** (Ollama, LM Studio, LiteLLM) as the primary workflow, with the ability to connect to cloud providers (Anthropic, OpenAI) when needed.

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
- [Personas](#personas)
- [Configuration](#configuration)
  - [AI Gateway / Proxy Support](#ai-gateway--proxy-support)
- [Extensibility](#extensibility)
  - [MCP Servers](#mcp-servers)
  - [Custom Commands](#custom-commands)
  - [Custom Agent Definitions](#custom-agent-definitions)
  - [Process Plugins](#process-plugins)
- [Project Structure](#project-structure)
- [Testing](#testing)

## Architecture

Aegis uses a **daemon + client** architecture. A background daemon (`aegis serve`) owns sessions, the agent engine, tool registry, and model adapter. Clients (the interactive TUI and the `chat` CLI command) connect to it over a local HTTP API with server-sent events (SSE), so sessions survive client restarts and can be resumed.

```
┌────────────────────────────────────────────────────────────────────┐
│                        Daemon (aegis serve)                        │
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

Clone and build Aegis:

**Windows (PowerShell)**
```powershell
git clone https://github.com/scottymacleod/aegis.git
cd aegis
go build -o aegis.exe ./cmd/aegis
```

**Linux / macOS**
```bash
git clone https://github.com/scottymacleod/aegis.git
cd aegis
go build -o aegis ./cmd/aegis
```

Optionally move the binary onto your PATH:

**Windows (PowerShell)**
```powershell
Copy-Item aegis.exe "$env:USERPROFILE\go\bin\aegis.exe"
```

**Linux / macOS**
```bash
sudo mv aegis /usr/local/bin/aegis
# or: mv aegis ~/go/bin/aegis
```

### Using a Local LLM

Local LLM usage is the primary focus of this project. Aegis auto-discovers models running on your machine from three supported servers:

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

**Step 2 — Configure Aegis to use the local model**

Aegis uses the OpenAI-compatible API that local servers expose. Set the provider to `openai`, point `base_url` at your local server, and set the model name:

**Windows (PowerShell)**
```powershell
# Set environment variables for the session
$env:OPENAI_API_KEY = "not-needed"
$env:AEGIS_PROVIDER_DEFAULT = "openai"
$env:AEGIS_PROVIDER_BASE_URL = "http://localhost:11434/v1"   # Ollama
$env:AEGIS_PROVIDER_MODEL = "llama3.1"
```

**Linux / macOS**
```bash
export OPENAI_API_KEY="not-needed"           # required by the adapter but not checked by local servers
export AEGIS_PROVIDER_DEFAULT="openai"
export AEGIS_PROVIDER_BASE_URL="http://localhost:11434/v1"   # Ollama
export AEGIS_PROVIDER_MODEL="llama3.1"
```

Or configure it permanently in the config file:

**Windows**: `%AppData%\aegis\config.yaml`
**Linux**: `~/.config/aegis/config.yaml`
**macOS**: `~/Library/Application Support/aegis/config.yaml`

```yaml
provider:
  default: openai
  base_url: "http://localhost:11434/v1"   # Ollama endpoint
  model: llama3.1                          # model name as reported by the server
```

> **Note**: Set `OPENAI_API_KEY` to any non-empty string (e.g. `"not-needed"`) — the adapter requires it to be set, but local servers do not validate it.

**Step 3 — Discover available models**

Aegis can probe for all locally running model servers and list available models:
```bash
# In a running session, the agent has a list_models tool, or from the CLI:
aegis chat "list available local models" --mode plan
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
export AEGIS_PROVIDER_DEFAULT="openai"

# Windows (PowerShell)
$env:OPENAI_API_KEY = "sk-..."
$env:AEGIS_PROVIDER_DEFAULT = "openai"
```

The default provider is `anthropic` with model `claude-opus-4-8`. Override via config or environment variables.

## Usage

### Starting the Daemon

The daemon must be running before launching the TUI or sending chat commands. It manages sessions, the agent engine, tool registry, and all integrations.

```bash
# Start in a separate terminal (logs to data dir by default)
aegis serve

# Or with log output to stderr for debugging
aegis serve --foreground
```

The daemon listens on `127.0.0.1:4127` by default and generates an auth token stored in the data directory.

### Launching the TUI

```bash
# Default: plan mode (read-only), general persona
aegis

# Build mode (can create/edit/delete files, run shell commands)
aegis --mode build

# Security architect persona
aegis --persona security --mode build

# Other personas (see Personas section for full list)
aegis --persona developer --mode build
aegis --persona sre --mode plan
aegis --persona cloud-architect --mode plan

# Resume an existing session
aegis --resume <session-id>
```

The TUI is built with [Bubble Tea](https://github.com/charmbracelet/bubbletea) and renders a scrollable conversation with a text input area. It streams model responses in real time and shows tool call status inline.

### Non-Interactive CLI

For scripting or single-shot queries without the TUI:

```bash
aegis chat "summarize main.go" --mode build
aegis chat "what security issues exist in this repo?" --mode plan

# Auto-approve all tool calls (unattended)
aegis chat "refactor the config package" --mode build --yes
```

### Other Commands

```bash
# Preview resolved config, tools, memory, and context without calling the model
aegis dry-run

# Run security scanners (semgrep, trivy, gitleaks)
aegis scan ./path

# Render a diagram
aegis diagram --type mermaid --out architecture.svg < diagram.mmd

# List and manage sessions
aegis sessions list

# Show current configuration
aegis config
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

### Personas
With `--persona <name>`, the system prompt is tailored to a specific role. Each persona shapes the agent's behavior, expertise, and focus areas. See the [Personas](#personas) section for the full list and descriptions.

### Security Scanning
The `security_scan` tool runs semgrep, trivy, and gitleaks and produces a unified findings report. Works with any persona but is especially useful with security-focused ones (`security`, `security-architect`, `security-engineer`, `appsec-engineer`).

### Diagrams
`render_diagram` and `aegis diagram` render Mermaid, PlantUML, C4, Graphviz, and more to SVG/PNG via [Kroki](https://kroki.io) (with local CLI fallback). Draw.io export is also supported.

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

## Personas

Personas control the agent's system prompt, shaping its expertise and behavior for different roles. Select one with `--persona <name>` on the CLI or TUI.

| Persona | `--persona` value | Focus |
|---------|-------------------|-------|
| General | `general` (default) | Research, documentation, and coding assistant |
| Security | `security` | Security platform architect: capability research, STRIDE/LINDDUN threat modeling, C4/Mermaid architecture, issue identification |
| Platform Architect | `platform-architect` | System design, technology evaluation, capacity planning, platform standards |
| Security Architect | `security-architect` | Security architecture, threat modeling, security requirements, design review |
| Security Engineer | `security-engineer` | Security tooling, vulnerability management, automation, incident response |
| AppSec Engineer | `appsec-engineer` | Secure code review, application testing, OWASP, CI/CD security integration |
| Developer | `developer` | Implementation, debugging, code review, testing |
| Security Researcher | `security-researcher` | Vulnerability research, attack analysis, MITRE ATT&CK, defensive research |
| Risk Assessor | `risk-assessor` | Risk identification, analysis, evaluation, treatment (NIST RMF, ISO 27005, FAIR) |
| Business Analyst | `business-analyst` | Requirements analysis, process mapping, stakeholder communication |
| Data Analyst | `data-analyst` | Data exploration, statistical analysis, visualization, reporting |
| Network Security Architect | `network-security-architect` | Network design, segmentation, cloud networking, zero-trust, threat analysis |
| Report Writer | `report-writer` | Structured reports, technical writing, findings documentation |
| SRE | `sre` | Reliability engineering, SLOs/SLIs, observability, incident management, capacity planning |
| Infrastructure Architect | `infrastructure-architect` | Infrastructure design, IaC (Terraform/Pulumi), container orchestration, day-2 operations |
| Cloud Architect | `cloud-architect` | Cloud-native design, migration strategies, multi-cloud/hybrid, cost optimization |
| Cloud Security Engineer | `cloud-security-engineer` | Cloud security posture (CIS Benchmarks), IAM, cloud-native security, threat detection |

Custom agent definitions (see [Custom Agent Definitions](#custom-agent-definitions)) can also be used for project-specific roles beyond the built-in personas.

## Configuration

Configuration is resolved with precedence (lowest to highest):

1. **Built-in defaults**
2. **Global config** — `<user-config-dir>/aegis/config.yaml`
3. **Project config** — `./.aegis/config.yaml`
4. **Environment variables** — `AEGIS_*` (underscores map to dots: `AEGIS_PROVIDER_MODEL` → `provider.model`)

API keys are read from the environment only (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) and never written to config files.

**Config file locations by platform:**

| Platform | Global Config Path |
|----------|-------------------|
| Windows | `%AppData%\aegis\config.yaml` |
| Linux | `~/.config/aegis/config.yaml` |
| macOS | `~/Library/Application Support/aegis/config.yaml` |

### Full Config Reference

```yaml
# Provider settings
provider:
  default: openai              # "anthropic" or "openai" (use "openai" for local LLMs)
  model: llama3.1              # model ID as known by the provider/server
  base_url: "http://localhost:11434/v1"  # API base URL (required for local LLMs)
  max_tokens: 8192             # response token cap
  max_retries: 4               # transient failure retries (0 = disabled)
  headers:                     # extra HTTP headers on every request (e.g. gateway auth)
    X-Gateway-Token: "your-token"
    X-Tenant-ID: "your-tenant"

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

### AI Gateway / Proxy Support

Aegis can route all LLM traffic through an AI gateway or reverse proxy for security controls, audit logging, rate limiting, or policy enforcement. Set `base_url` to your gateway endpoint and use `headers` for any gateway-specific authentication.

```yaml
provider:
  default: anthropic
  model: claude-sonnet-4-6
  base_url: "https://ai-gateway.internal.example.com"
  headers:
    X-Gateway-Token: "your-gateway-auth-token"
    X-Tenant-ID: "your-tenant-id"
```

The gateway must proxy requests to the upstream provider at the same API paths:
- **Anthropic**: `POST /v1/messages` (SSE streaming)
- **OpenAI / local LLMs**: `POST /v1/chat/completions` (SSE streaming)

Custom headers are sent on every request alongside the standard provider headers (`x-api-key` for Anthropic, `Authorization: Bearer` for OpenAI). The base URL can also be set via the `AEGIS_PROVIDER_BASE_URL` environment variable.

## Extensibility

### MCP Servers

Aegis consumes external [Model Context Protocol](https://modelcontextprotocol.io/) servers as additional tools. Servers can connect via stdio (launched as child processes) or HTTP/SSE. Configure them in the `mcp[]` array.

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
- Project: `.aegis/commands/*.md`

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
- Project: `.aegis/agents/*.md`

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

External commands can be registered as tools via the `plugins[]` config. Aegis pipes tool input as JSON to stdin and captures stdout as the result.

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
cmd/aegis/                 CLI entry point
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
  persona/                 System prompts: 17 built-in personas (general, security, developer, SRE, etc.)
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
