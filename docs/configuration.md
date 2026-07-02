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
  # 0 = unlimited. Set e.g. 5.0 to abort runs that exceed $5 of estimated spend.
  # Pricing covers Anthropic, OpenAI, Gemini, Groq, OpenRouter families.
  # Unknown models have tokens counted but no dollar cost.
  budget_usd: 0.0


# ── Daemon ────────────────────────────────────────────────────────────────────
server:
  # The daemon's listen address. Loopback only — non-loopback origins are rejected.
  addr: "127.0.0.1:4127"


# ── Logging ───────────────────────────────────────────────────────────────────
# debug | info | warn | error
log_level: info


# ── TUI ───────────────────────────────────────────────────────────────────────
tui:
  # D&D-themed thinking phrases in the status bar while the model reasons.
  humor_mode: false


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


# ── Shell sandbox ─────────────────────────────────────────────────────────────
sandbox:
  # "local"     — run directly on the host (default)
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


# ── Output validation ─────────────────────────────────────────────────────────
output_guard:
  # enabled: true means every final answer is checked before being shown.
  # Toggle per-session with /guard on|off inside the TUI.
  enabled: true

  # "llm"    — cheap second model call checks the answer against the rubric
  # "schema" — the answer must be valid JSON containing the required keys
  mode: llm

  # Rubric for llm mode. Empty = built-in rubric (fully addresses request,
  # no placeholders/TODOs, grounded in tool output).
  rubric: ""

  # Number of corrective retry attempts when the guard fails.
  # Guards fail open: any validator error yields a pass.
  max_retries: 1


# ── Per-persona model overrides ───────────────────────────────────────────────
# Pin specific built-in personas to a different model within the same provider.
# Model resolution: config personas[name].model > persona file model > global model.
personas:
  security-architect: { model: claude-opus-4-8 }
  developer: { model: "" }   # blank = use global provider.model


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
mcp:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]

  - name: github
    command: npx
    args: ["-y", "@modelcontextprotocol/server-github"]
    env:
      GITHUB_TOKEN: "ghp_..."

  # HTTP/SSE transport: set command to empty string and provide a URL.
  - name: my-http-server
    command: ""
    auth: "$MY_MCP_TOKEN"   # $VAR references expanded from environment / .aegis/.env


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

### Configure per-persona models

```yaml
personas:
  security-architect: { model: claude-opus-4-8 }
  developer:          { model: gpt-4o }
  report-writer:      { model: claude-opus-4-8 }
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
