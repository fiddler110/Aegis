# Aegis

An AI agent harness for **security, architecture, research, and development**, built from scratch in Go with a terminal-first interface. Designed to work with **local LLMs** (Ollama, LM Studio, LiteLLM) as the primary workflow, with the ability to connect to cloud providers (Anthropic, OpenAI) when needed.

It borrows proven patterns from existing agents — provider abstraction and context compression (Hermes), persistent daemon + client sessions and Plan/Build modes (opencode), slash-command skills, subagents, permission modes, and file-based memory (Claude Code) — while staying a clean, single codebase you own.

## Table of Contents

- [Quick Start](#quick-start)
- [Supported Local LLM Backends](#supported-local-llm-backends)
- [Architecture](#architecture)
- [Getting Started](#getting-started)
  - [Prerequisites](#prerequisites)
  - [Installation](#installation)
  - [First-Time Setup](#first-time-setup)
  - [Using a Local LLM](#using-a-local-llm)
  - [Using Cloud Providers](#using-cloud-providers)
- [Usage](#usage)
  - [Launching the TUI](#launching-the-tui)
  - [Non-Interactive CLI](#non-interactive-cli)
  - [Running the Daemon Separately](#running-the-daemon-separately)
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

---

## Quick Start

Already have a local LLM server running? Here's how to go from zero to chatting in under two minutes.

**1. Clone and build**

Run the build script for your platform. Each script shows a plan, asks which actions to run, compiles the binary with version info embedded, installs it to your Go bin directory, and optionally adds an `aegis-config` shell helper to open the config file in your editor.

```powershell
# Windows (PowerShell)
git clone https://github.com/fiddler110/Aegis.git
cd Aegis
.\build-windows.ps1
```

```bash
# Linux
git clone https://github.com/fiddler110/Aegis.git
cd Aegis
chmod +x build-linux.sh && ./build-linux.sh

# macOS
git clone https://github.com/fiddler110/Aegis.git
cd Aegis
chmod +x build-macos.sh && ./build-macos.sh
```

The script prompts you to choose which of two actions to run:
- **[1]** Compile and install `aegis` / `aegis.exe` to your Go bin directory
- **[2]** Add an `aegis-config` shell function to your profile (opens the config file in your editor)

**2. Generate your config and set the environment variable**

```bash
aegis --first-init
```

This writes a full configuration template to your OS config directory with **Ollama active by default** and all other providers (Anthropic, OpenAI, Azure, Groq, OpenRouter, LM Studio, Vertex AI) in commented-out blocks ready to activate.

Local LLM servers don't validate API keys, but the harness requires a non-empty value:

```bash
# Linux / macOS — add to ~/.zshrc or ~/.bashrc for permanence
export OPENAI_API_KEY="ollama"

# Windows PowerShell — add to System Environment Variables for permanence
$env:OPENAI_API_KEY = "ollama"
```

**3. Pull a model and launch**

```bash
ollama pull llama3.2
aegis
```

That's it. The daemon starts automatically in the same process, no second terminal needed. Use `/help` inside the TUI for available commands, or `/config` to open the interactive configuration wizard.

---

## Supported Local LLM Backends

Aegis works with any server that exposes an OpenAI-compatible API (`/v1/chat/completions`). The table below lists the most popular options.

| Server | Default Base URL | Install / Start |
|--------|-----------------|-----------------|
| [Ollama](https://ollama.com) | `http://localhost:11434/v1` | Install from [ollama.com](https://ollama.com). Pull a model: `ollama pull llama3.2`. Runs as a service automatically. |
| [LM Studio](https://lmstudio.ai) | `http://localhost:1234/v1` | Download from [lmstudio.ai](https://lmstudio.ai). Load a model, then start the server from the Local Server tab. |
| [llama.cpp](https://github.com/ggerganov/llama.cpp) | `http://localhost:8080/v1` | Build or download a release. Start: `llama-server -m model.gguf --port 8080`. |
| [vLLM](https://github.com/vllm-project/vllm) | `http://localhost:8000/v1` | `pip install vllm`. Start: `vllm serve meta-llama/Llama-3.1-8B-Instruct`. |
| [LocalAI](https://github.com/mudler/LocalAI) | `http://localhost:8080/v1` | `docker run -p 8080:8080 localai/localai`. |
| [Jan](https://jan.ai) | `http://localhost:1337/v1` | Download from [jan.ai](https://jan.ai). Enable API server in Settings → Advanced. |
| [LiteLLM](https://github.com/BerriAI/litellm) | `http://localhost:4000/v1` | `pip install litellm && litellm --model ollama/llama3.2`. |
| [KoboldCpp](https://github.com/LostRuins/koboldcpp) | `http://localhost:5001/v1` | Download from GitHub. Start: `koboldcpp model.gguf --port 5001`. |
| [text-generation-webui](https://github.com/oobabooga/text-generation-webui) | `http://localhost:5000/v1` | Clone repo and run installer. Enable the OpenAI-compatible API extension. |

> **Tip**: Set `AEGIS_PROVIDER_BASE_URL` to any server's base URL and `AEGIS_PROVIDER_MODEL` to the model name to use a backend not in the `--first-init` template.

---

## Architecture

Aegis uses a **daemon + client** architecture. The daemon owns sessions, the agent engine, tool registry, and the model adapter. The TUI and CLI connect to it over a local HTTP API with server-sent events (SSE).

**By default, `aegis` auto-starts the daemon in the same process** so no second terminal is needed. If you want the daemon to persist across TUI restarts (e.g. for long-running background jobs or shared sessions), run `aegis serve` explicitly in the background.

```
┌────────────────────────────────────────────────────────────────────┐
│                        Daemon (auto-start or aegis serve)          │
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
   │(dashboard│  │  (CLI)  │  │  (debug) │
   │ + spinner│  │         │  │          │
   └─────────┘  └─────────┘  └──────────┘
```

**Core agent loop**: The engine sends the conversation to the model, receives streamed events (text deltas, tool-use requests), dispatches tool calls through the permission gate, appends results, and repeats until the model produces a final answer or the run is interrupted. Context compaction automatically summarizes old turns when the conversation grows large.

---

## Getting Started

### Prerequisites

- **Go 1.25+** — [Install Go](https://go.dev/dl/)
- **Git** — for cloning
- A local LLM server **or** a cloud API key

### Installation

The preferred way to build and install Aegis is with the platform-specific build scripts. They show a plan before acting, embed the git version into the binary, install to your Go bin directory, optionally add an `aegis-config` shell helper, and detect and remove any stale binary at a different PATH location.

**Windows (PowerShell)**
```powershell
git clone https://github.com/fiddler110/Aegis.git
cd Aegis
.\build-windows.ps1
```

The script presents two optional actions:
1. Build and install `aegis.exe` to `%GOPATH%\bin` (default: `%USERPROFILE%\go\bin`)
2. Add an `aegis-config` function to your PowerShell profile that opens the config file in your editor

**Linux**
```bash
git clone https://github.com/fiddler110/Aegis.git
cd Aegis
chmod +x build-linux.sh && ./build-linux.sh
```

The script installs to `/usr/local/bin` (with `sudo` if needed) or falls back to `~/go/bin`. It detects your shell (bash, zsh, fish) and adds the `aegis-config` function to the appropriate aliases file.

**macOS**
```bash
git clone https://github.com/fiddler110/Aegis.git
cd Aegis
chmod +x build-macos.sh && ./build-macos.sh
```

Same as Linux, but defaults to `/usr/local/bin` and targets `~/.zshrc` for zsh (macOS default since Catalina) or `~/.bash_profile` for bash.

#### Manual build (without the scripts)

If you prefer to build manually:

```bash
go build -o aegis ./cmd/aegis          # Linux / macOS
go build -o aegis.exe ./cmd/aegis      # Windows
```

Then copy the binary to a directory on your `PATH`.

### First-Time Setup

Run `--first-init` to generate your global config with a complete commented template:

```bash
aegis --first-init
```

**Config file location by platform:**

| Platform | Path |
|----------|------|
| Windows | `%AppData%\aegis\config.yaml` |
| macOS | `~/.config/aegis/config.yaml` |
| Linux | `~/.config/aegis/config.yaml` |

To create a project-level override in the current directory (safe to commit — no secrets):

```bash
aegis --init
```

This writes `.aegis/config.yaml` with commented examples for overriding the model, permission mode, cost budget, and network allowlist on a per-project basis.

#### aegis-config helper

If you ran action [2] during the build script, you can open your config file at any time with:

```bash
aegis-config      # opens config.yaml in your chosen editor
```

On Windows: reload your profile first with `. $PROFILE`.
On Linux/macOS: reload with `source ~/.zshrc` (or the file the script reports).

### Using a Local LLM

Local LLM usage is the primary focus of this project. The `--first-init` template has Ollama active by default.

**Step 1 — Start your local model server**

```bash
# Ollama — pull once, then it runs as a service
ollama pull llama3.2
# or a larger model for better tool-use:
ollama pull qwen2.5:32b

# Start manually if needed:
ollama serve
```

```
# LM Studio — download the app, load a model, start the local server from the UI
```

**Step 2 — Set the API key environment variable**

Local servers don't validate the API key, but the harness requires it to be non-empty:

```bash
# Linux / macOS — add to ~/.zshrc or ~/.bash_profile for permanence
export OPENAI_API_KEY="ollama"

# Windows PowerShell — add to System Environment Variables for permanence
$env:OPENAI_API_KEY = "ollama"
```

**Step 3 — Edit the config if needed**

Open the file printed by `--first-init` (or use `aegis-config`) and confirm the `model` matches a model you have pulled:

```yaml
provider:
  default: openai
  base_url: "http://localhost:11434/v1"
  model: "llama3.2"    # ← change to any model you have pulled
```

Alternatively, use `/config` inside the TUI to change the provider and model interactively without editing the file manually.

### Using Cloud Providers

Open the global config and uncomment the relevant provider block. Then set the API key in your environment.

**Anthropic (Claude)**
```bash
export ANTHROPIC_API_KEY="sk-ant-..."   # Linux / macOS
$env:ANTHROPIC_API_KEY = "sk-ant-..."   # Windows PowerShell
```

Uncomment in config:
```yaml
provider:
  default: anthropic
  model: "claude-opus-4-8"
  max_tokens: 16384
```

**OpenAI**
```bash
export OPENAI_API_KEY="sk-..."          # Linux / macOS
$env:OPENAI_API_KEY = "sk-..."         # Windows PowerShell
```

Uncomment in config:
```yaml
provider:
  default: openai
  model: "gpt-4o"
  max_tokens: 16384
```

All other providers (Azure OpenAI, Groq, OpenRouter, LM Studio, Vertex AI) have ready-to-uncomment blocks in the file generated by `--first-init`.

---

## Usage

### Launching the TUI

`aegis` starts the daemon automatically in the same process — no second terminal needed.

```bash
aegis                                # build mode (default), general persona
aegis --mode plan                    # read-only / safe exploration
aegis --mode build                   # file edits + shell commands
aegis --persona security             # security architect persona
aegis --persona developer --mode build
aegis --persona sre --mode plan
aegis --resume <session-id>          # resume an existing session
```

**TUI layout:**

```
⬡ AEGIS                                          abc12345  llama3.2
─────────────────────────────────────────────────────────────────────
 SESSION      │  You
 abc12345      │  analyse this repo for security issues
               │
 MODE          │  Assistant
 build         │  I'll start by reading the project structure…
               │
 TOOLS         │  ⚙ glob  {"pattern":"**/*.go"}
 ✓ glob        │  ✓ glob → 42 files matched
 ⚙ read_file   │  ⚙ read_file  {"path":"main.go"}
               │
 COST          │
 $0.0012       │
 in  1234      │
 out 456       │
─────────────────────────────────────────────────────────────────────
 ◐ thinking…                               build   in:1234  out:456
─────────────────────────────────────────────────────────────────────
 │ Send a message…
```

**Keyboard shortcuts:**

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Ctrl+J` | Insert newline in input |
| `Shift+Tab` | Cycle permission mode (plan → build → auto) |
| `Ctrl+T` | Show active sub-agents |
| `Ctrl+C` | Interrupt streaming run / quit |
| Mouse wheel | Scroll conversation |

**Slash commands** (type inside the TUI):

| Command | Action |
|---------|--------|
| `/help` | Show all commands |
| `/config` | Open the interactive configuration wizard |
| `/mode <plan\|build\|auto>` | Switch permission mode |
| `/persona <name>` | Switch persona |
| `/clear` | Clear the transcript |
| `/memory` | Show saved memories |
| `/remember <text>` | Save a memory entry |
| `/skills` | List saved skills |
| `/commands` | List custom commands |
| `/models` | Show current model |
| `/session [list]` | Session info or list all sessions |
| `/quit` | Exit |

#### Configuration Wizard (`/config`)

The `/config` command opens a 5-step interactive wizard inside the TUI for changing provider, model, and settings without leaving the terminal or editing files manually:

1. **Provider** — select from Anthropic, Ollama, OpenAI, LM Studio, Groq, OpenRouter, or Custom
2. **Base URL** — pre-filled for local servers; leave empty for cloud providers that use the default
3. **Model** — shows discovered local models (Ollama/LM Studio) or a curated list for cloud providers; manual entry always available
4. **Max tokens** — response token cap
5. **Thinking mode** — Auto / Enabled / Disabled (for reasoning models like qwen3, deepseek-r1)

Changes are written directly to your global `config.yaml` and take effect the next time you start `aegis`.

### Non-Interactive CLI

For scripting or single-shot queries without the TUI:

```bash
aegis chat "summarise main.go" --mode build
aegis chat "what security issues exist in this repo?" --mode plan

# Auto-approve all tool calls (unattended)
aegis chat "refactor the config package" --mode build --yes
```

### Running the Daemon Separately

If you want the daemon to persist across multiple TUI sessions (e.g. for long-running background jobs that survive a TUI restart), run it explicitly:

```bash
# Background — logs written to the data directory
aegis serve

# Foreground — mirror logs to stderr for debugging
aegis serve --foreground
```

The daemon listens on `127.0.0.1:4127` by default. When a separately started daemon is already running, `aegis` detects it and connects without starting a second one.

### Other Commands

```bash
# Preview resolved config, tools, memory, and context — no model call
aegis dry-run

# Run security scanners (semgrep, trivy, gitleaks)
aegis scan ./path

# Render a diagram from stdin
aegis diagram --type mermaid --out architecture.svg < diagram.mmd

# List and manage sessions
aegis sessions list

# Show resolved configuration
aegis config

# Generate global config template (Ollama default, all providers documented)
aegis --first-init

# Generate project-level config override
aegis --init
```

---

## Capabilities

### File Operations
`read_file`, `write_file`, `edit_file`, `multi_edit`, `glob`, `grep` — all confined to the workspace root. A file staleness tracker rejects edits to files modified externally since they were last read, preventing accidental overwrites.

### Shell Execution
`shell` — runs commands in the workspace directory with a configurable timeout. Supports background jobs (returns a task ID immediately) and optional container sandboxing. Every invocation is gated by the permission mode.

### Permission Modes
- **Plan** — read-only; the agent can search, read, and answer but cannot modify anything.
- **Build** — full access: create, edit, delete files, run shell commands. Shell and execute calls prompt for approval unless `auto_approve_exec` is enabled.
- **Auto** — all capabilities without prompting (use in trusted, unattended contexts).

Every tool call is recorded to an audit trail (`audit.jsonl`) with timestamps and inputs.

### LaTeX Documents
- **`latex_build`** — Compiles a `.tex` file to PDF using xelatex, pdflatex, or lualatex. Runs 1–3 passes to resolve cross-references and table of contents. Returns a structured report: errors with context lines, deduplicated warnings, page count, and the output PDF path. Supports `check_only` mode for fast syntax validation without writing a PDF.
- **`latex_new_document`** — Creates a new `.tex` file with a production-quality preamble ready for enterprise reports, white papers, and technical documents. Includes professional typography, semantic heading colours, `booktabs` tables, `listings` code blocks, `tcolorbox` callout boxes (`notebox` / `warnbox` / `keybox`), figure captions, `hyperref` PDF metadata, and a scaffolded section structure with `%%TODO` markers. Supports styles: `report`, `whitepaper`, `article`, `book`. Works with xelatex (default) and pdflatex.

**Typical notes-to-report workflow:**
1. `glob` + `read_file` — collect your markdown notes
2. `latex_new_document` — create a template with section titles matching the notes
3. `edit_file` — fill each `%%TODO` with synthesised content
4. `latex_build {"path":"report.tex","runs":2}` — compile to PDF

### Web
`web_fetch` — fetches a URL and returns readable text (HTML converted). `web_search` — performs a web search.

### Multi-Agent Orchestration (Swarm)
The `agent` tool lets the model spawn sub-agents ("teammates") that run independently. Sub-agents execute in-process (goroutines) or as separate headless processes. Each sub-agent has its own permission scope (cannot exceed the parent's), and a file-based mailbox enables inter-agent communication. A recursion-depth guard prevents runaway spawning. Active agents are visible in the TUI sidebar (Ctrl+T for a full list).

### Background Tasks and Scheduling
Shell commands and agent tasks can run in the background via the task manager (SQLite-backed). A built-in cron scheduler supports recurring jobs with standard cron expressions. Task tools: `task_create`, `task_get`, `task_list`, `task_stop`, `task_output`.

### Memory and Skills
- `remember` — persists facts into user-scoped or project-scoped memory files, loaded into the system prompt on every turn.
- `save_skill` — saves reusable procedures as markdown files, also injected into context.
- Relevance scoring ranks memory entries by query similarity so the most useful context surfaces first.

### Planning Tools
`todo_add`, `todo_update`, `todo_list` — a lightweight in-session planning surface for tracking multi-step work. `ask_user` collects structured input (free text, single-choice, multi-choice).

### Security Scanning
The `security_scan` tool runs semgrep, trivy, and gitleaks and produces a unified findings report. Works with any persona but is especially useful with security-focused ones (`security`, `security-architect`, `security-engineer`, `appsec-engineer`).

### Diagrams
`render_diagram` and `aegis diagram` render Mermaid, PlantUML, C4, Graphviz, and more to SVG/PNG/PDF via [Kroki](https://kroki.io) (with local CLI fallback). Draw.io export is also supported.

### Code Intelligence (LSP)
When LSP servers are configured, `lsp_diagnostics`, `lsp_references`, and `lsp_definition` give the agent IDE-level code understanding.

### Model Discovery
The `list_models` tool probes `localhost` for Ollama (`:11434`), LM Studio (`:1234`), and LiteLLM (`:4000`) and reports every available model — useful for switching models mid-session or verifying your local setup. The configuration wizard also uses discovery to populate the model list automatically.

### Contextual Security Policies
- `egress_then_write` — requires explicit approval for write operations that follow any network access in the same session (prevents exfiltrate-then-modify patterns).
- `network_allowlist` — restricts outbound calls to listed domains.
- All policy decisions are recorded to the audit trail.

### Sandboxed Execution
Shell commands can run locally (default) or inside containers: Docker, Podman (Linux/macOS/Windows), and Apple Containers (macOS). Network isolation and path validation prevent workspace escapes.

### Cost Tracking
Token usage (including cache hits for Anthropic) is tracked per turn and displayed live in the TUI status bar. A configurable `budget_usd` limit halts a run when estimated spend exceeds the threshold.

---

## Personas

Select a persona with `--persona <name>` or `/persona <name>` inside the TUI.

| Persona | `--persona` value | Focus |
|---------|-------------------|-------|
| General | `general` (default) | Research, documentation, and coding assistant |
| Security | `security` | Security platform architect: capability research, STRIDE/LINDDUN threat modeling, C4/Mermaid architecture |
| Platform Architect | `platform-architect` | System design, technology evaluation, capacity planning |
| Security Architect | `security-architect` | Security architecture, threat modeling, design review |
| Security Engineer | `security-engineer` | Security tooling, vulnerability management, automation, incident response |
| AppSec Engineer | `appsec-engineer` | Secure code review, OWASP, CI/CD security integration |
| Developer | `developer` | Implementation, debugging, code review, testing |
| Security Researcher | `security-researcher` | Vulnerability research, attack analysis, MITRE ATT&CK |
| Risk Assessor | `risk-assessor` | Risk identification and treatment (NIST RMF, ISO 27005, FAIR) |
| Business Analyst | `business-analyst` | Requirements analysis, process mapping, stakeholder communication |
| Data Analyst | `data-analyst` | Data exploration, statistical analysis, visualization, reporting |
| Network Security Architect | `network-security-architect` | Network design, segmentation, zero-trust, threat analysis |
| Report Writer | `report-writer` | Structured reports, technical writing, findings documentation |
| SRE | `sre` | Reliability engineering, SLOs/SLIs, observability, incident management |
| Infrastructure Architect | `infrastructure-architect` | IaC (Terraform/Pulumi), container orchestration, day-2 operations |
| Cloud Architect | `cloud-architect` | Cloud-native design, migration strategies, multi-cloud/hybrid, cost optimization |
| Cloud Security Engineer | `cloud-security-engineer` | Cloud security posture (CIS Benchmarks), IAM, cloud-native security |

Custom agent definitions (see [Custom Agent Definitions](#custom-agent-definitions)) can define project-specific roles beyond the built-ins.

---

## Configuration

Configuration is resolved with the following precedence (highest wins):

```
environment variables  >  project config (.aegis/config.yaml)  >  global config  >  built-in defaults
```

API keys are **always** read from environment variables (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) and are never written to config files.

**Config file locations:**

| Platform | Global config path |
|----------|--------------------|
| Windows | `%AppData%\aegis\config.yaml` |
| macOS | `~/.config/aegis/config.yaml` |
| Linux | `~/.config/aegis/config.yaml` |

**Generate config files:**

```bash
aegis --first-init   # global config — all providers documented, Ollama active
aegis --init         # project config — .aegis/config.yaml in current directory
```

**Edit config files:**

```bash
aegis-config         # open global config in your editor (if set up by build script)
```

Or use `/config` inside the TUI to update provider settings interactively.

### Full Config Reference

```yaml
# ── Provider ──────────────────────────────────────────────────────────────────
provider:
  default: openai              # "anthropic" or "openai" (use "openai" for all
                               # local LLMs and OpenAI-compatible cloud providers)
  model: "llama3.2"            # model ID as known to the provider or server
  base_url: "http://localhost:11434/v1"  # required for local LLMs and proxies
  max_tokens: 8192             # response token cap
  max_retries: 4               # transient-failure retries (0 = disabled)
  think: ~                     # null = provider default; false = disable extended
                               # thinking for reasoning models (qwen3, deepseek-r1)
  headers:                     # extra HTTP headers on every request (gateway auth)
    X-Gateway-Token: "token"

# ── Permission ────────────────────────────────────────────────────────────────
permission:
  mode: build                  # "plan" (read-only) | "build" | "auto"
  auto_approve_exec: false     # skip approval prompts for shell/execute calls

# ── Cost guard ────────────────────────────────────────────────────────────────
cost:
  budget_usd: 0.0              # 0 = unlimited; set e.g. 5.0 to abort past $5

# ── Daemon ────────────────────────────────────────────────────────────────────
server:
  addr: "127.0.0.1:4127"

# ── Logging ───────────────────────────────────────────────────────────────────
log_level: info                # debug | info | warn | error

# ── Diagrams ──────────────────────────────────────────────────────────────────
diagram:
  kroki_url: "https://kroki.io"

# ── Multi-agent ───────────────────────────────────────────────────────────────
swarm:
  backend: in_process          # "in_process" (goroutines) | "subprocess" (isolated)

# ── Shell sandbox ─────────────────────────────────────────────────────────────
sandbox:
  backend: local               # "local" | "container"
  runtime: ""                  # "docker" | "podman" | "container" (Apple); empty = auto
  image: "ubuntu:22.04"        # container image when backend=container
  network: false               # allow network inside containers

# ── Security policies ─────────────────────────────────────────────────────────
security:
  egress_then_write: false     # require approval for writes after network access
  network_allowlist: []        # restrict to these domains (empty = unrestricted)

# ── LSP servers ───────────────────────────────────────────────────────────────
lsp:
  - name: gopls
    command: gopls
    args: []
    extensions: [".go"]

# ── MCP servers ───────────────────────────────────────────────────────────────
mcp:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
    env:
      SOME_TOKEN: "value"

# ── Process plugins ───────────────────────────────────────────────────────────
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

Route all LLM traffic through an AI gateway for audit logging, rate limiting, or policy enforcement:

```yaml
provider:
  default: anthropic
  model: "claude-opus-4-8"
  base_url: "https://ai-gateway.internal.example.com"
  headers:
    X-Gateway-Token: "your-gateway-auth-token"
    X-Tenant-ID: "your-tenant-id"
```

The gateway must proxy to the upstream provider at the same API paths:
- **Anthropic**: `POST /v1/messages` (SSE streaming)
- **OpenAI / local LLMs**: `POST /v1/chat/completions` (SSE streaming)

---

## Extensibility

### MCP Servers

Aegis consumes external [Model Context Protocol](https://modelcontextprotocol.io/) servers as additional tools. Servers connect via stdio (launched as child processes) or HTTP/SSE. Configure them in the `mcp[]` array.

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

Drop markdown files into the agents directory to define reusable agent personas with specific system prompts and tool restrictions.

**Locations** (project overrides global):
- Global: `<data-dir>/agents/*.md`
- Project: `.aegis/agents/*.md`

```markdown
---
name: reviewer
description: Code review specialist
mode: plan
tools: [read_file, glob, grep]
---
You are a code review specialist. Analyse code for correctness, security,
performance, and maintainability. Cite specific line numbers.
```

### Process Plugins

External commands can be registered as tools via the `plugins[]` config array. Aegis pipes tool input as JSON to stdin and captures stdout as the result.

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

---

## Project Structure

```
cmd/aegis/                 CLI entry point
internal/
  engine/                  Agent loop: model → tools → repeat, with gating, hooks, compaction
  provider/                Normalised model interface (Adapter)
    anthropic/             Anthropic Messages API adapter (SSE streaming)
    openai/                OpenAI chat-completions adapter (SSE streaming; also for local LLMs)
  providerfactory/         Build an adapter from config (shared by daemon and CLI)
  tool/                    Tool registry (registration, exposure, capability tagging)
    builtin/               All built-in tools:
                             File:     read_file, write_file, edit_file, multi_edit, glob, grep
                             Shell:    shell (background jobs + sandbox support)
                             Web:      web_fetch, web_search
                             LaTeX:    latex_build, latex_new_document
                             Code:     lsp_diagnostics, lsp_references, lsp_definition
                             Security: security_scan
                             Diagrams: render_diagram
                             Memory:   remember, save_skill
                             Tasks:    task_create/get/list/stop/output
                             Cron:     cron_create/list/delete
                             Planning: todo_add/update/list, ask_user
                             Models:   list_models
                             Agents:   agent (spawn sub-agents)
  permission/              Plan/Build/Auto policies + approval gate + contextual security rules
  compaction/              Token-budget summarisation of old conversation turns
  memory/                  File-based memory + skills, relevance scoring, context discovery
  security/                Semgrep, Trivy, Gitleaks runners + normalised findings
  diagram/                 Kroki + local CLI renderers (Mermaid, PlantUML, C4, Graphviz, draw.io)
  persona/                 System prompts: 17 built-in personas
  hooks/                   Pre/post tool-call hooks + JSONL audit trail
  mcp/                     MCP client (JSON-RPC/stdio + HTTP/SSE transports)
  session/                 SQLite session store
  server/                  Daemon: HTTP + SSE API
  client/                  HTTP client for the daemon API
  tui/                     Bubble Tea terminal dashboard (sidebar, spinner, status bar, mouse scroll)
  api/                     Shared API types (request/response structs, event kinds)
  config/                  Layered config loading (defaults → global → project → env) + config writer
  cost/                    Token-based cost tracking + budget enforcement
  swarm/                   Multi-agent coordination: identities, mailbox, registry, backends
  sandbox/                 Pluggable sandbox: local, Docker, Podman, Apple Containers
  filetracker/             File staleness detection
  lsp/                     LSP client manager (lifecycle, diagnostics, references)
  discover/                Auto-discovery of local model servers (Ollama, LM Studio, LiteLLM)
  task/                    Background task manager (SQLite-backed)
  cron/                    Recurring job scheduler (cron expressions)
  commands/                Custom slash command loader (markdown + YAML frontmatter)
  agentdef/                Custom agent definition loader (markdown + YAML frontmatter)
  plugins/                 External process tool plugin loader
  logging/                 Structured logging setup (slog)
  cli/                     Cobra command tree (root, serve, chat, scan, diagram, sessions, init, …)
```

---

## Testing

```bash
# Run all tests
go test ./...

# Run tests for a specific package
go test ./internal/engine/...
go test ./internal/tool/builtin/...

# Verbose output
go test -v ./internal/provider/anthropic/...

# Live model calls (requires an API key or local server running)
OPENAI_API_KEY="ollama" go test -run TestLive ./internal/provider/openai/...
```

---

## License

This is a personal project. See [LICENSE](LICENSE) for details.
