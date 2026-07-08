# Configuration Reference

Aegis uses a layered configuration system. Values are resolved in this order (highest wins):

```
environment variables
  > .aegis/.env
    > project config (.aegis/config.yaml)
      > global config (~/.config/aegis/config.yaml)
        > built-in defaults
```

**API keys are always read from environment variables** — never from config files.

---

## Config File Locations

| File | Purpose |
|------|---------|
| `~/.config/aegis/config.yaml` (macOS/Linux) | Global config — applies to all projects |
| `%AppData%\aegis\config.yaml` (Windows) | Global config on Windows |
| `.aegis/config.yaml` | Project-level overrides (safe to commit) |
| `.aegis/.env` | Local secrets — add to `.gitignore` |

Generate files with:

```bash
aegis --first-init   # global config with full template
aegis --init         # project config (.aegis/config.yaml)
```

---

## Environment Variables

Any config key can be overridden with an environment variable by converting the YAML path to uppercase with underscores, prefixed with `AEGIS_`:

| Variable | Config key | Example |
|----------|-----------|---------|
| `AEGIS_PROVIDER_DEFAULT` | `provider.default` | `anthropic` |
| `AEGIS_PROVIDER_MODEL` | `provider.model` | `claude-opus-4-8` |
| `AEGIS_PROVIDER_BASE_URL` | `provider.base_url` | `http://localhost:11434/v1` |
| `AEGIS_PROVIDER_MAX_TOKENS` | `provider.max_tokens` | `16384` |
| `AEGIS_PERMISSION_MODE` | `permission.mode` | `plan` |
| `AEGIS_COST_BUDGET_USD` | `cost.budget_usd` | `5.0` |
| `AEGIS_LOG_LEVEL` | `log_level` | `debug` |
| `AEGIS_SERVER_ADDR` | `server.addr` | `127.0.0.1:4127` |

API keys use their native names (not the `AEGIS_` prefix):

| Variable | Provider |
|----------|---------|
| `ANTHROPIC_API_KEY` | Anthropic Claude |
| `OPENAI_API_KEY` | OpenAI or any local LLM |
| `GROQ_API_KEY` | Groq |
| `OPENROUTER_API_KEY` | OpenRouter |

---

## Full Config Reference

```yaml
# ── Provider ──────────────────────────────────────────────────────────────────
provider:
  # "anthropic" or "openai"
  # Use "openai" for ALL local LLMs and OpenAI-compatible cloud providers.
  default: openai

  # Model ID. "auto" or "" picks the first available Ollama model at startup.
  # Set any explicit ID: "llama3.2", "claude-opus-4-8", "gpt-4o", etc.
  model: "auto"

  # Required for local LLMs and API gateways. Leave empty for direct cloud calls.
  # Ollama:    http://localhost:11434/v1
  # LM Studio: http://localhost:1234/v1
  # llama.cpp: http://localhost:8080/v1
  # vLLM:      http://localhost:8000/v1
  # LiteLLM:   http://localhost:4000/v1
  base_url: "http://localhost:11434/v1"

  # Optional fast model for titles and context compaction.
  # Falls back to `model` if empty.
  small_model: ""

  # Maximum tokens in the model's response. Capped by the model's own limits.
  max_tokens: 8192

  # Retry count for transient failures (connection errors, rate limits).
  # 0 disables retries.
  max_retries: 4

  # Maximum agent-loop iterations per run. 0 uses the built-in default (40).
  max_iterations: 0

  # Abort a run when the same turn is produced N times in a row.
  # 0 uses the built-in default (5).
  loop_threshold: 0

  # Extra HTTP headers on every request to the provider. Useful for gateway auth.
  headers:
    X-Gateway-Token: "your-token"
    X-Tenant-ID: "your-tenant"

  # Extended thinking.
  # null/~ = provider default (Anthropic disables by default; local varies)
  # true   = enable (Anthropic extended thinking; local reasoning models)
  # false  = disable explicitly
  think: ~

  # OpenAI o1/o3 reasoning effort: "low", "medium", or "high".
  # Only applies to o-series models; ignored otherwise.
  reasoning_effort: ""

  # Model context window in tokens. 0 = auto-detect (skips compaction for local
  # models that don't report their limits). Set this if your local model doesn't
  # advertise its context window and you want compaction to work.
  context_window: 0

  # Ordered (provider, model) pairs tried in sequence after the primary
  # adapter exhausts max_retries (P5.9). Empty = no failover.
  fallback: []
  #   - provider: ollama
  #     model: "llama3.2"
  #   - provider: openai
  #     model: "gpt-4o-mini"
  #     base_url: ""      # optional per-fallback API base override

  # Required to fail over FROM a local provider (ollama) TO a cloud provider
  # (anthropic, openai) — guards against a local-only session silently
  # sending data off the machine on an outage. Cloud-to-cloud and any-to-local
  # failover are always allowed and never need this flag.
  allow_cloud_fallback: false


# ── Permission ────────────────────────────────────────────────────────────────
permission:
  # "plan"  — read-only; no file writes, no shell execution
  # "build" — file edits allowed; shell/execute prompts for approval (default)
  # "auto"  — all capabilities without prompting (trusted sandboxes only)
  mode: build

  # Skip approval prompts for shell/execute calls even in build mode.
  auto_approve_exec: false

  # Fine-grained allow/deny rules evaluated before the mode gate.
  # Syntax: "allow <tool>(<pattern>)" or "deny <tool>(<pattern>)"
  # <tool>: tool name, capability alias (bash, write, read, network), or *
  # <pattern>: glob matched against the call's primary input
  # deny takes precedence over allow.
  rules:
    - "allow bash(npm test*)"      # auto-approve npm test without prompting
    - "allow bash(git status)"
    - "deny write(/etc/*)"         # never write under /etc, even in auto mode
    - "deny shell(rm -rf /*)"


# ── Cost guard ────────────────────────────────────────────────────────────────
cost:
  # 0 = unlimited. Set e.g. 5.0 to abort runs that exceed $5 of estimated spend
  # within a single turn. Pricing covers Anthropic, OpenAI, Gemini, Groq,
  # OpenRouter families. Unknown models have tokens counted but no dollar cost.
  #
  # NOTE: budget_usd (and session_cap_usd/daily_cap_usd below) are silent
  # no-ops for local/Ollama models and any model absent from the pricing
  # catalog — their usage is estimated or unpriced, so it contributes $0
  # regardless of how much was actually used. Use max_tokens_per_run /
  # session_token_cap / daily_token_cap (P10.5) as the primary guardrail for a
  # local-first setup; the dollar caps remain a convenience for cloud models.
  budget_usd: 0.0

  # 0 = unlimited. Aborts a run once its cumulative token count (input +
  # output + cache, across every turn) reaches this amount. Always
  # enforceable — token counts are present even when usage was estimated or
  # the model is unpriced, unlike budget_usd.
  max_tokens_per_run: 0

  # 0 = unlimited. Refuses to start a new turn once a session's cumulative
  # (persisted) cost reaches this amount.
  session_cap_usd: 0.0

  # 0 = unlimited. Refuses to start a new turn once total spend across all
  # sessions for the current UTC day reaches this amount.
  daily_cap_usd: 0.0

  # 0 = unlimited. Token-denominated counterparts to session_cap_usd/
  # daily_cap_usd — refuse a new turn once cumulative tokens (session or
  # cross-session daily) reach this amount. Recommended for local models,
  # where the dollar caps above never fire.
  session_token_cap: 0
  daily_token_cap: 0

  # Fraction (0-1) of session_cap_usd/daily_cap_usd/session_token_cap/
  # daily_token_cap at which a "cost_alert" warning event is surfaced to the
  # client instead of a hard stop. Only applies to whichever cap above is
  # non-zero.
  alert_threshold: 0.8


# ── Daemon ────────────────────────────────────────────────────────────────────
server:
  # The daemon's listen address. Loopback only — non-loopback origins are rejected.
  addr: "127.0.0.1:4127"


# ── Logging ───────────────────────────────────────────────────────────────────
# debug | info | warn | error
log_level: info


# ── TUI ───────────────────────────────────────────────────────────────────────
tui:
  # D&D-themed flavor phrases in the status bar while the agent is busy.
  # Phrases are bucketed to match what's actually happening: wizardly/
  # intellectual phrases while the model is reasoning, and separate
  # investigation/crafting/combat/travel/party banks while a tool call is
  # in flight (read/write/execute/network/spawn respectively).
  humor_mode: false

  # Color scheme: "dark" (default), "light", an embedded builtin (catppuccin,
  # dracula, gruvbox, tokyonight), or a custom name loaded from
  # .aegis/themes/<name>.json (project) or ~/.aegis/themes/<name>.json
  # (user). Applied to the sidebar, status bar, glamour markdown rendering,
  # and the ANSI-16 remap used for shell tool output.
  theme: dark

  # Attention system (P16.1): fires on stream-end, approval-pending, and
  # error, but only while the terminal is known to be unfocused (via
  # tea.FocusMsg/BlurMsg — not every terminal/multiplexer reports focus).
  # "off": nothing. "bell": terminal BEL. "desktop": OSC 9/777 desktop
  # notification. "both" (default): bell + desktop. Also settable live with
  # /notify <mode>, for that session only.
  notifications: both

  # Inline image thumbnails (P16.9): render a small half-block ANSI preview
  # in the transcript when an image is attached, instead of only sending it
  # to the model. "auto" (default): rendered when the terminal's detected
  # color profile supports at least 256 colors, skipped otherwise (dumb
  # terminals, NO_COLOR). "off": never render, text-only notice as before.
  image_rendering: auto

  # Keybinding remap (P13.3.5): override the key sequence(s) for a named
  # action. Action names are the lowercased internal/tui keyMap field names
  # (send, queue, newline, thinking, complete, help, palette, cancel,
  # interrupt, clear, editor, cyclemode, histup, histdown, teammates,
  # sessions, terminal, sidebartoggle, pasteimage, diagnose). Values are one
  # or more bubbles/key sequences (e.g. "ctrl+x", "alt+t", "f2"); the first
  # is shown in help text. Unlisted actions keep their default. Run with an
  # unknown action name and Aegis exits with an error naming the typo.
  keybindings:
    terminal: ["ctrl+x"]
    diagnose: ["ctrl+g"]


# ── Diagrams ──────────────────────────────────────────────────────────────────
diagram:
  # Kroki API endpoint for diagram rendering.
  # Use a self-hosted Kroki instance for air-gapped environments.
  kroki_url: "https://kroki.io"


# ── Multi-agent ───────────────────────────────────────────────────────────────
swarm:
  # "in_process"  — sub-agents run as goroutines in the same process (default)
  # "subprocess"  — sub-agents run as isolated processes (more isolation, slower)
  backend: in_process


# ── Lifecycle hooks ───────────────────────────────────────────────────────────
# Shell commands that fire on agent lifecycle events.
# Aegis passes a JSON event object to the command's stdin.
# Exit 0 = allow. Exit 2 = veto (stderr message shown to the model).
# Other non-zero exits are logged as warnings but do not block execution.
hooks:
  # Fires before a tool call. Exit 2 vetoes the call.
  pre_tool_use: ""           # e.g. "aegis-hook-lint"
  # Fires after a tool call completes.
  post_tool_use: ""
  # Fires once when the session starts.
  session_start: ""
  # Fires when the agent finishes (success or error).
  stop: ""
  # Fires when a sub-agent finishes.
  subagent_stop: ""


# ── Web search ────────────────────────────────────────────────────────────────
# Provider for the web_search tool. DuckDuckGo scraping is the zero-config default.
search:
  # brave | tavily | searxng | duckduckgo (default)
  provider: duckduckgo

  # API key for brave or tavily. Supports $VAR expansion from environment / .aegis/.env.
  api_key: ""

  # Required when provider=searxng. Base URL of your self-hosted SearxNG instance.
  base_url: ""


# ── Background session notifications ──────────────────────────────────────────
# Alert when a detached session finishes, errors, or needs input.
notify:
  # Desktop notification via osascript (macOS), notify-send (Linux), or
  # PowerShell toast (Windows). Enabled by default.
  desktop: true

  # POST the event JSON to this URL (optional). Leave empty to disable.
  webhook: ""


# ── Semantic recall ───────────────────────────────────────────────────────────
# Optional embedding layer over the project knowledge base and long-term
# entity memory (P5.8). Disabled by default — both stay FTS5/BM25-only, which
# needs no extra service running. When enabled, search results are the
# reciprocal-rank fusion of the BM25 ranking and a cosine-similarity ranking
# over embeddings computed via a local Ollama model.
embeddings:
  enabled: false

  # Only "ollama" is supported today.
  provider: ollama

  # Any Ollama embedding model, e.g. "nomic-embed-text", "mxbai-embed-large".
  model: "nomic-embed-text"

  # Ollama server base URL.
  base_url: "http://localhost:11434"


# ── Shell sandbox ─────────────────────────────────────────────────────────────
sandbox:
  # "local"     — run directly on the host (default)
  # "os"        — OS-level isolation: seatbelt on macOS, bwrap/Landlock on Linux; no container needed.
  #               Confines WRITES (and network, if configured below) only — the entire host
  #               filesystem is still readable inside the sandbox. See docs/security.md before
  #               relying on this for anything that reads sensitive host files (SSH keys, cloud
  #               credentials); use "container" if you need read confinement too.
  # "container" — run inside a container (requires runtime)
  # "auto"      — detect available runtimes and pick the best one
  backend: local

  # Force a specific runtime when backend=container or backend=auto:
  # docker | podman | wslc | container (Apple Containers, macOS)
  # Leave empty to let auto-detection choose.
  runtime: ""

  # Override the auto-detection priority order.
  # Default (OS-specific): on Windows [wslc, docker, podman]; elsewhere [docker, podman]
  priority: []

  # Container image to use when backend=container or backend=auto selects a container.
  image: "ubuntu:22.04"

  # Allow network access inside containers. false = network-isolated (safer).
  network: false

  # If backend=container/os and the runtime can't be initialized (e.g. Docker
  # daemon down), the default is to log a warning and fall back to running
  # unsandboxed on the host — a silent security downgrade an operator might
  # not notice (P7.4). Set strict=true to make that a hard startup failure
  # instead. The daemon also reports an active fallback via /healthz, and the
  # TUI/CLI print a warning before entering a session when it's set.
  strict: false

  # Commands run by the local/os backends never see ANTHROPIC_API_KEY or
  # OPENAI_API_KEY (P7.2) — a prompt-injected `shell` call can't read the
  # daemon's own provider credentials back out and exfiltrate them via
  # web_fetch. List additional env var names here to also exclude them, e.g.
  # secrets loaded from .aegis/.env for MCP server auth that the shell tool
  # has no legitimate reason to see. Container backend tools never inherit
  # host env at all, so this only applies to backend: local/os/auto(fallback).
  strip_env: []


# ── Contextual security policies ──────────────────────────────────────────────
# Note: these are tool-layer controls, not system-wide egress firewalls.
# The shell tool can still reach the network. For hard enforcement, use
# sandbox.backend=container with network=false.
security:
  # Require approval for write-capability tool calls that happen after any
  # network-capability call in the same session (prevents exfiltrate-then-modify).
  egress_then_write: false

  # Restrict network-capability tool calls to these domains.
  # Empty list = unrestricted.
  network_allowlist: []
    # - "api.github.com"
    # - "registry.npmjs.org"

  # Security-scanner availability (P11.11): controls `aegis scan`/security_scan.
  # "auto" (host binary if present, else a configured container image) |
  # "host" (never fall back to a container) | "container" (always prefer it).
  default_method: auto

  # Per-tool overrides, keyed by scanner name (semgrep, trivy, gitleaks).
  # image must be digest-pinned (image@sha256:...) — Aegis ships no built-in
  # image pin; see docs/security.md for how to obtain and verify one.
  tools: {}
    # trivy:
    #   method: auto
    #   image: "aquasec/trivy@sha256:<digest-you-verified>"
    # semgrep:
    #   enabled: true
    #   method: host
    # gitleaks:
    #   install: prompt   # prompt (default) | always | never

  # Opt-in P12 debate integration (P12.5): both default false. See
  # multi-agent.md#debate-p12.
  debate:
    threat_model: false   # security-architect: debate each threat/mitigation before writing it down
    triage: false           # security-audit skill: debate a borderline/disputed finding before suppressing it


# ── Output validation ─────────────────────────────────────────────────────────
output_guard:
  # enabled: true means every final answer is checked before being shown.
  # Toggle per-session with /guard on|off inside the TUI.
  enabled: true

  # "llm"    — cheap second model call checks the answer against the rubric
  # "schema" — the answer must be valid JSON containing the required keys
  mode: llm

  # Rubric for llm mode. Empty = built-in rubric (fully addresses request, no
  # unfinished work like TODOs/stubs, grounded in tool output). Clearly-marked
  # example/placeholder values in documentation — an illustrative IP address,
  # a <your-api-key>-style token — are acceptable under the built-in rubric,
  # since the real value often depends on the reader's own environment and
  # was never supplied to the model.
  rubric: ""

  # Number of corrective retry attempts when the guard fails.
  # Guards fail open: any validator error yields a pass.
  max_retries: 1


# ── Default persona ────────────────────────────────────────────────────────────
# Persona a new session starts with when --persona isn't passed. Set this in
# .aegis/config.yaml to give a project its own default focus (`aegis persona use
# <name>` writes this for you). An explicit --persona always overrides it.
default_persona: ""   # e.g. "developer"; empty falls back to "general"

# ── Per-persona model overrides ───────────────────────────────────────────────
# Pin specific built-in personas to a different model within the same provider.
# Model resolution: config personas[name].model > persona file model > global model.
personas:
  security-architect: { model: claude-opus-4-8 }
  developer: { model: "" }   # blank = use global provider.model


# ── Built-in skills ───────────────────────────────────────────────────────────
# Skills embedded in the Aegis binary (content-review, html-report,
# security-audit, architecture-diagram, debug-investigation,
# redteam-engagement, threat-modeling, latex-report, deep-research — see
# `aegis skills list`). Empty by
# default: they stay dormant (no system-prompt cost) until named here, via
# `aegis skills enable <name>`, or
# the /skills TUI command. Project/user skill files (.aegis/skills/,
# ~/.aegis/skills/) are unaffected — those are always active.
skills:
  builtin_enabled: []   # e.g. ["security-audit", "architecture-diagram"]


# ── LSP servers ───────────────────────────────────────────────────────────────
# Language servers give the agent IDE-level code intelligence (diagnostics,
# references). Multiple servers can be listed; each handles its file extensions.
lsp:
  - name: gopls
    command: gopls
    args: []
    extensions: [".go"]
  - name: typescript-language-server
    command: typescript-language-server
    args: ["--stdio"]
    extensions: [".ts", ".tsx", ".js", ".jsx"]
  - name: pyright
    command: pyright-langserver
    args: ["--stdio"]
    extensions: [".py"]


# ── MCP servers ───────────────────────────────────────────────────────────────
# External tools via the Model Context Protocol (stdio or HTTP/SSE transport).
# The agent sees MCP tools alongside built-in tools with no distinction.
#
# `capability` sets the tool.Capability ("read", "write", "network", "execute",
# or "spawn") the permission gate uses for every tool this server exposes.
# It defaults to "execute" — the most restrictive class — for any server that
# doesn't declare one, since Aegis has no way to know what an MCP server's
# tools actually do; an unlabeled or untrusted server must not silently get
# the always-allowed "network" capability. Use `tool_capabilities` to override
# per remote tool name when a server exposes a known mix (e.g. a read-only
# `search` tool alongside a `write_file` tool).
mcp:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
    tool_capabilities:
      read_file: read
      list_directory: read
      write_file: write

  - name: github
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "ghp_..."
    capability: network   # trusted, well-known server; all its tools call out to GitHub's API

  # HTTP/SSE transport: set command to empty string and provide a URL.
  - name: my-http-server
    command: ""
    auth: "$MY_MCP_TOKEN"   # $VAR references expanded from environment / .aegis/.env
    # capability omitted → defaults to "execute", so calls hit the Ask gate in build mode


# ── MCP server mode ────────────────────────────────────────────────────────────
# `aegis mcp-serve` (P6.3) — the reverse direction of the `mcp:` block above:
# this daemon acts as an MCP server itself, so other MCP-speaking harnesses
# (Claude Code, Codex, editors) can drive Aegis sessions as tools. See the CLI
# reference for the tools it exposes (aegis_prompt, aegis_new_session,
# aegis_list_sessions).
mcp_server:
  # New sessions default to plan mode (read-only) — a lower-trust posture than
  # the local TUI/CLI's own "build" default, since the caller here is an
  # external harness. Override per-call with the `mode` tool argument, or here
  # to change the server-wide default.
  default_mode: plan
  # An MCP tools/call is a synchronous request/response with no human in the
  # loop to ask, so a tool call needing approval (e.g. a session explicitly
  # requested in build/auto mode) is denied by default. Set true only for a
  # fully trusted caller/sandbox — equivalent to permission.auto_approve_exec
  # but scoped to sessions created through mcp-serve.
  auto_approve: false


# ── Process plugins ───────────────────────────────────────────────────────────
# Register external commands as tools. Aegis pipes tool input as JSON to stdin
# and captures stdout as the result.
plugins:
  - name: check_types
    description: "Run TypeScript type checking"
    command: npx
    args: ["tsc", "--noEmit", "--pretty"]
    input_schema: '{"type":"object","properties":{"path":{"type":"string"}}}'
    capability: read     # read | write | execute | network
    timeout_sec: 60

  - name: my-linter
    description: "Run custom linting on a file"
    command: my-lint
    args: ["--json"]
    input_schema: '{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}'
    capability: read
    timeout_sec: 30


# ── Session cleanup ───────────────────────────────────────────────────────────
cleanup:
  # Auto-delete non-archived sessions older than N days (since last update).
  # 0 disables automatic pruning.
  session_ttl_days: 0

  # How often the pruner runs, in hours.
  interval_hours: 24


# ── Data directory ────────────────────────────────────────────────────────────
# Where Aegis stores its databases, logs, and user-scoped files.
# Leave empty for the OS default:
#   macOS/Linux: ~/.local/share/aegis/
#   Windows:     %LocalAppData%\aegis\
data_dir: ""
```

---

## The `.aegis/.env` File

Place secrets that must not appear in version-controlled YAML into `.aegis/.env`:

```ini
# .aegis/.env — add this file to .gitignore
MY_MCP_TOKEN=secret-bearer-token-here
SOME_INTERNAL_API_KEY=another-secret
```

- Values are loaded before config parsing, so they can be referenced as `$VAR` in supported YAML fields (currently `mcp[].auth`)
- Real environment variables always override `.env` values
- The file is never written by Aegis — manage it manually

---

## Common Recipes

### Pin to a specific Ollama model

```yaml
provider:
  default: openai
  base_url: "http://localhost:11434/v1"
  model: "qwen2.5:32b"
```

### Use Claude for one project

In `.aegis/config.yaml`:
```yaml
provider:
  default: anthropic
  model: "claude-opus-4-8"
  max_tokens: 16384
```

And set `ANTHROPIC_API_KEY` in your environment.

### Restrict shell commands in a project

```yaml
permission:
  mode: build
  rules:
    - "allow bash(npm *)"
    - "allow bash(go *)"
    - "deny bash(*)"    # deny everything else
```

### Enable output validation with a custom rubric

```yaml
output_guard:
  enabled: true
  mode: llm
  rubric: "Every security finding must cite a CVE number or CWE ID."
  max_retries: 2
```

### Run all shell commands in Docker

```yaml
sandbox:
  backend: container
  runtime: docker
  image: "node:20-alpine"
  network: false
```

### Restrict outbound network to specific domains

```yaml
security:
  network_allowlist:
    - "api.github.com"
    - "registry.npmjs.org"
    - "pypi.org"
```

### Set a cost budget for a session

```yaml
cost:
  budget_usd: 2.0   # abort the run if estimated spend exceeds $2
```

### Cap total session/daily spend

```yaml
cost:
  session_cap_usd: 10.0   # refuse new turns once a session has spent $10
  daily_cap_usd: 25.0     # refuse new turns once all sessions spend $25 in a UTC day
  alert_threshold: 0.8    # warn at 80% of whichever cap above is set
```

### Set a token budget (recommended for local/Ollama models)

Dollar caps never fire for local models — their usage is estimated and
contributes $0 regardless of actual volume. Token caps are always
enforceable:

```yaml
cost:
  max_tokens_per_run: 200000   # abort a single run past 200k cumulative tokens
  session_token_cap: 1000000   # refuse new turns once a session hits 1M tokens
  daily_token_cap: 5000000     # refuse new turns once all sessions hit 5M tokens in a UTC day
```

### Route threat-model entries or borderline scan findings through a debate round (P12.5)

Both default off — a debate round is 3+ model calls instead of 1, so this is a deliberate opt-in, not a
default-on behavior change:

```yaml
security:
  debate:
    threat_model: true   # security-architect debates each threat/mitigation before writing it down
    triage: true           # security-audit skill debates a borderline/disputed finding before suppressing it
```

See [multi-agent.md](multi-agent.md#debate-p12) for the debate mechanism itself (`/debate`, `aegis
debate`, the `agent` tool's `mode:"debate"`).

### Configure per-persona models

```yaml
personas:
  security-architect: { model: claude-opus-4-8 }
  developer:          { model: gpt-4o }
  report-writer:      { model: claude-opus-4-8 }
```

### Set this project's default persona

```yaml
default_persona: developer
```

Or from the CLI: `aegis persona use developer` (add `--global` to set the user-wide default instead).

### Enable a built-in skill for this project

```yaml
skills:
  builtin_enabled: ["security-audit", "architecture-diagram"]
```

Or from the CLI: `aegis skills enable security-audit` (add `--global` for the user-wide default instead), or `/skills enable security-audit` in the TUI.

### Configure lifecycle hooks

```yaml
hooks:
  pre_tool_use: "/usr/local/bin/aegis-lint-hook"   # lint before file writes
  post_tool_use: "jq . >> /var/log/aegis-audit.jsonl"
```

### Configure pluggable web search

```yaml
search:
  provider: brave
  api_key: "$BRAVE_API_KEY"   # set BRAVE_API_KEY in environment or .aegis/.env
```

### Use an AI gateway

```yaml
provider:
  default: anthropic
  model: "claude-opus-4-8"
  base_url: "https://ai-gateway.internal.example.com"
  headers:
    X-Gateway-Token: "your-token"
    X-Tenant-ID: "tenant-id"
```

The gateway must proxy the provider's native paths:
- Anthropic: `POST /v1/messages`
- OpenAI/local: `POST /v1/chat/completions`
