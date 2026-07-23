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
| `AEGIS_SERVER_MAX_CONCURRENT_RUNS` | `server.max_concurrent_runs` | `10` |
| `AEGIS_SERVER_MAX_RUN_DURATION_SEC` | `server.max_run_duration_sec` | `1800` |
| `AEGIS_SERVER_SSE_BUFFER_SIZE` | `server.sse_buffer_size` | `256` |
| `AEGIS_SERVER_TLS_ENABLED` | `server.tls.enabled` | `true` |

API keys use their native names (not the `AEGIS_` prefix):

| Variable | Provider |
|----------|---------|
| `ANTHROPIC_API_KEY` | Anthropic Claude |
| `OPENAI_API_KEY` | OpenAI or any local LLM |
| `GROQ_API_KEY` | Groq |
| `OPENROUTER_API_KEY` | OpenRouter |

Groq and OpenRouter aren't distinct `provider.default` values — both are reached with
`provider.default: openai` plus a `base_url` pointing at their OpenAI-compatible endpoint (see
docs/providers.md). `ProviderAPIKey` checks `OPENAI_API_KEY` first, then falls back to
`GROQ_API_KEY`, then `OPENROUTER_API_KEY` — so either the shared `OPENAI_API_KEY` or the
provider-specific var works.

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

  # Optional fast model for titles, context compaction, and output-guard
  # verdict calls. Falls back to `model` if empty.
  small_model: ""

  # P9.4: opt-in per-turn model routing for user-facing agent turns. When
  # true AND small_model is set, each turn is classified by a purely local
  # heuristic (message length, code fences, multi-step lists, sentence
  # count, and whether the session already made tool calls this turn) as
  # "simple" or "complex". Simple turns run on small_model; everything else
  # — including any turn where the classifier is unsure — stays on `model`.
  # No effect unless small_model is also set, and never overrides an
  # explicit per-session /model pin (P14.7), which always wins. Off by
  # default; existing setups see no behavior change.
  task_routing: false

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

  # Bounds the corrective-nudge retries fired when a turn that plainly reads
  # as actionable produces zero tool calls (a model dumping its reasoning as
  # prose instead of calling a tool). 0 uses the built-in default (1 retry);
  # negative disables the nudge entirely.
  zero_tool_nudge: 0

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

  # Model context window in tokens. 0 = auto-detect: for Ollama (or an openai
  # provider whose base_url points at an Ollama server) the daemon queries the
  # native Ollama API for the context actually being served — the loaded
  # model's allocation, a modelfile num_ctx, or Ollama's 4096 default — and
  # uses it for compaction thresholds and the TUI usage bar; if Ollama is
  # unreachable at startup, detection retries after each run. A non-zero value
  # overrides detection, except that a *verified smaller* served window wins
  # (Ollama silently truncates prompts beyond it, so trusting a larger
  # configured value breaks long runs). /status shows the value in use.
  context_window: 0

  # How long a streamed request waits for the response headers (not the
  # streamed body that follows), in seconds. 0 = default (5 minutes) — the
  # previously-hardcoded value, unchanged unless you opt in. Ollama withholds
  # the response header until prompt-eval (prefill) finishes, so a large local
  # context can legitimately need longer than the default; raise this rather
  # than lower context_window if a run dies mid-turn with
  # "timeout awaiting response headers" (P35.5). See docs/providers.md#response-header-timeout.
  response_header_timeout: 0

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

  # Selects the system-prompt/tool-exposure shape (P25.6). "auto" (default)
  # infers from base_url: loopback/localhost gets the "local" profile
  # (trimmed prompt, web_search/web_fetch/security_scan/git_pr deferred, repo
  # map capped) tuned for small local models; any other base_url gets the
  # unchanged "default" profile. Set "local" or "default" to force the choice
  # regardless of base_url.
  prompt_profile: auto


# ── Permission ────────────────────────────────────────────────────────────────
permission:
  # "plan"  — read-only; no file writes, no shell execution
  # "build" — file edits allowed; shell/execute prompts for approval (default)
  # "auto"  — all capabilities without prompting (trusted sandboxes only)
  mode: build

  # Skip approval prompts for shell/execute calls even in build mode.
  auto_approve_exec: false

  # auto_approve_exec: true combined with an unsandboxed local backend (the
  # default sandbox.backend) means every model-issued shell command runs on
  # the host with no approval and no isolation — the daemon refuses to start
  # with that combination unless this is explicitly set to true. Configure a
  # real sandbox (sandbox.backend: container or os) instead of setting this
  # unless the daemon itself is already running inside an isolated
  # environment (e.g. a CI container).
  allow_unsandboxed_auto_exec: false

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
  # The daemon's listen address. Defaults to loopback. Separately, browser
  # requests carrying a non-loopback Origin header are always rejected
  # regardless of this setting (DNS-rebinding protection).
  addr: "127.0.0.1:4127"

  # Must be set to bind `addr` to a non-loopback address (e.g. "0.0.0.0:4127"
  # to reach the daemon from another machine). The API is protected only by a
  # bearer token with no rate limiting, so the daemon refuses to start on a
  # non-loopback address until this is explicitly acknowledged (FIND-08).
  allow_remote: false

  # Bounds which directories a client may request as a session's working
  # directory (P25.1) once allow_remote is set: the resolved path must be
  # the daemon's own default workspace (or nested under it) or nested under
  # one of these entries, else session creation is rejected. Ignored on the
  # default loopback-only bind, where a client is already as trusted as a
  # local shell user. Empty by default (no extra directories allowed beyond
  # the daemon's own workspace).
  session_workdir_allowlist: []

  # Transport encryption for client<->daemon traffic (FIND-32/P24.18). On by
  # default since P27.5/FIND-13: without it, client<->daemon HTTP is
  # plaintext, including the bearer token and full conversation content —
  # fine against off-host attackers given the loopback-only default above,
  # but observable by another local account on a shared host with
  # packet-capture privilege. Set enabled: false (or AEGIS_SERVER_TLS_ENABLED
  # =false) to opt back out, e.g. in a container/CI environment where the
  # extra cert/handshake overhead isn't worth it and the host isn't shared.
  #
  # With no cert_file/key_file set, the daemon generates a self-signed
  # ECDSA P-256 certificate on first start and persists it as
  # <data_dir>/daemon.crt and daemon.key (reused across restarts, same
  # convention as daemon.token). Every CLI client (`aegis`, `aegis ui`, `aegis
  # sessions`, `aegis acp`, `aegis mcp-serve`, ...) reads daemon.crt and pins
  # it explicitly — this is certificate pinning to a file that never leaves
  # the machine, not verification against a public CA or hostname, so no
  # system trust store is involved. A browser opening the web UI (`aegis ui`)
  # has no such pinning and will show a self-signed-certificate warning; this
  # is expected, and the CLI prints a one-line notice to that effect when TLS
  # is enabled.
  #
  # This protects against another local account observing loopback traffic.
  # It does not protect against Host/OS-level compromise of the same
  # account, which can already read daemon.token — and, with TLS enabled,
  # daemon.key — directly off disk.
  tls:
    enabled: true
    cert_file: ""  # optional operator-supplied cert; auto-generated if empty
    key_file: ""   # optional operator-supplied key; auto-generated if empty

  # Defaults to 10 (P27.12/FIND-14). Caps how many message-turn runs may be
  # actively executing across ALL sessions at once. A request past the cap is
  # rejected immediately (429) rather than queued. The per-session
  # serialization the daemon already does (at most one active run per
  # session) doesn't bound total concurrency across sessions — this exists to
  # bound a lower-trust caller that can create many sessions, e.g. `aegis
  # mcp-serve`. Only top-level HTTP-driven runs count against it; in-process
  # sub-agents spawned by the `agent`/swarm tool don't. 0 = unlimited.
  max_concurrent_runs: 10

  # Defaults to 1800/30 minutes (P27.12/FIND-14). Aborts a single run once it
  # has been active this many seconds, the same clean way an interrupted
  # request is handled. cost.max_tokens_per_run/budget_usd are the primary
  # spend guardrails; this is a coarser wall-clock backstop for a run that
  # never trips those (e.g. a local model stuck in a near-zero-cost tool-call
  # loop). 0 = unlimited.
  max_run_duration_sec: 1800

  # Per-connection cap on queued-but-not-yet-flushed SSE events. If a
  # consumer (TUI, web UI, or an mcp-serve client) reads slower than the
  # engine produces events and the queue fills, the oldest queued event is
  # dropped to make room for the newest rather than growing memory without
  # bound. The run itself keeps executing and persisting to the session store
  # regardless of how far behind the SSE stream falls.
  sse_buffer_size: 256


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
  humor_mode: true

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
  # (send, steer, newline, thinking, complete, help, palette, cancel,
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

  # Opts web_fetch/web_search output into the heuristic prompt-injection
  # scan, mirroring the per-server mcp[].scan_output toggle (FIND-04). On by
  # default since P27.13/FIND-12 — a best-effort heuristic (invisible
  # characters, base64-encoded payloads) that only adds a visible warning on
  # a hit, never blocks or mutates content. The untrusted-content provenance
  # marker is always applied to fetched/searched content regardless of this
  # setting — see docs/mcp-trust-boundary.md.
  scan_output: true


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
  # "local"     — run directly on the host; no isolation at all (P27.14/FIND-04:
  #               approval prompts and env-var stripping are the only
  #               compensating controls — a shell tool call can still read/
  #               write/exfiltrate anything the daemon's own user account can).
  # "os"        — OS-level isolation: seatbelt on macOS, bwrap/Landlock on Linux; no
  #               container needed. `aegis --first-init`'s generated config defaults
  #               new installs to this (falls back to "local" with a startup WARN
  #               if unavailable, e.g. bubblewrap not installed on Linux, or on
  #               Windows where neither mechanism exists).
  #               Confines WRITES, network (if configured below), and READS (P27.18/FIND-19) to
  #               the workspace plus a built-in toolchain allowlist (system dirs, ~/go, ~/.cargo,
  #               ~/.npm, etc. — see os_extra_read_paths below); anything not on that allowlist,
  #               including credential dirs like ~/.ssh or ~/.aws, is simply unreadable from
  #               inside the sandbox. Still weaker than "container", which never mounts the host
  #               filesystem at all — a toolchain dir that happens to also hold a stray credential
  #               file would still be readable. See docs/security_scan.md before relying on this
  #               for genuinely untrusted code.
  # "container" — run inside a container (requires runtime)
  # "auto"      — detect available runtimes and pick the best one
  #
  # A container runtime name (docker, podman, wsl/wslc, apple) is also
  # accepted here directly and is rewritten to backend: container + the
  # matching runtime below (P25.2) — convenient, but the pair below is the
  # canonical spelling. Any other value fails the daemon at startup with an
  # error naming the offending value, rather than silently running
  # unsandboxed (which is what happened before P25.2).
  #
  # NOTE: shown here as "os" because that's what `aegis --first-init` writes
  # into a fresh global config. If this key is absent entirely (no config
  # file, or a config file that doesn't set it), the built-in default is
  # "local", not "os".
  backend: os

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

  # Additional host paths the "os" backend may read from, on top of the
  # workspace and the built-in toolchain allowlist (P27.18/FIND-19). Use this
  # when a project's toolchain lives somewhere non-standard. Entries that
  # don't exist on the host are silently skipped. Only applies to backend: os.
  os_extra_read_paths: []


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

  # Runs a read-capability tool's output through gitleaks-backed secret
  # detection before it's added to the conversation sent to the model
  # provider; any match is masked as "[REDACTED:<rule>]". Defaults to true —
  # sending file/conversation content carrying an unredacted secret to a
  # cloud provider is a real exposure with no other default control. Needs
  # gitleaks on PATH (fails open, i.e. no redaction, if missing) and adds
  # per-call latency; local Ollama usage never sends content off the machine
  # at all and remains the stronger mitigation. See docs/providers.md "Data
  # Exposure & Redaction".
  redact_secrets: true

  # Security-scanner availability (P11.11): controls `aegis scan`/security_scan.
  # "auto" (host binary if present, else a configured container image) |
  # "host" (never fall back to a container) | "container" (always prefer it).
  default_method: auto

  # Per-tool overrides, keyed by scanner name (opengrep, trivy, gitleaks).
  # image must be digest-pinned (image@sha256:...) — Aegis ships no built-in
  # image pin; see docs/security_scan.md for how to obtain and verify one.
  tools: {}
    # trivy:
    #   method: auto
    #   image: "aquasec/trivy@sha256:<digest-you-verified>"
    # opengrep:
    #   enabled: true
    #   method: host
    # gitleaks:
    #   install: prompt   # prompt (default) | always | never

  # One locally-built image carrying every bundled scanner, so container-method
  # scanning needs one image instead of a digest-pinned image per tool. Written
  # by `aegis security build-image` — you don't normally hand-edit this block.
  # Run `aegis security update-db` afterwards to populate the vulnerability
  # databases, which live in a container volume rather than in the image.
  #
  # image_id is a real image ID, not a registry digest: a locally-built image
  # has no digest (RepoDigests is empty until a push/pull), so instead of the
  # pin rule above, Aegis reads the image's actual ID back via `image inspect`
  # and compares it before every container run. Rebuilt or retagged behind
  # Aegis's back = scans fail closed with a specific reason.
  multiscanner:
    enabled: false
    # image: "localhost/aegis-multiscanner:v1"
    # image_id: "sha256:..."   # recorded at build time; re-verified before use
    #
    # The runtime that built the image. Recorded because a locally-built image
    # exists only in the storage of the engine that built it — auto-detection
    # could pick a different one (on Windows it prefers wslc) and report a
    # perfectly good podman-built image as missing.
    # runtime: podman
    #
    # How many scanners run at once (each container-method scanner is one
    # container). Applies to host-method runs too. Default 3; set 1 for
    # strictly sequential runs. The report is identical at any value.
    # concurrency: 3
    #
    # Which scanners the built image carries; written from the profile that
    # was actually built. Empty assumes the full profile.
    # tools: []

  # Names a specific registered WSL distro (e.g. "kali-linux") to target for
  # every WSL-capable scanner (nmap, nuclei, opengrep, kubescape), instead of
  # whatever `wsl --set-default` currently points at. Empty (default) uses
  # WSL's own default-distro selection. Windows-only; a security-tooling
  # distro like Kali is recommended for red-team/recon work — see
  # docs/security_scan.md.
  wsl_distro: ""

  # Hard authorization gate for the dast_scan tool (P11.7), enforced inside
  # the tool itself regardless of permission mode — an active scanner
  # pointed at an arbitrary host is an abuse primitive. Loopback and
  # RFC-1918 private addresses are always allowed with no config needed.
  # Sourced from user/global config only: a project .aegis/config.yaml can
  # never widen this, even once trusted.
  dast:
    # Exact hostnames, ".suffix" subdomain wildcards, or CIDR ranges
    # authorized for scanning, in addition to the loopback/RFC-1918
    # default-allow. Hostnames are matched literally, never DNS-resolved.
    allowed_targets: []
      # - "staging.example.com"
      # - ".internal.example.com"
      # - "10.0.0.0/8"

    # Gates active/api scan modes (real attack payloads, not just passive
    # observation) behind an explicit opt-in, on top of the per-call
    # approval prompt every dast_scan call already gets from its execute
    # capability. Default false.
    allow_active: false

  # Opt-in P12 debate integration (P12.5): both default false. See
  # multi-agent.md#debate-p12.
  debate:
    threat_model: false   # security-architect: debate each threat/mitigation before writing it down
    triage: false           # security-audit skill: debate a borderline/disputed finding before suppressing it


# ── Git tools ─────────────────────────────────────────────────────────────────
git:
  # Pre-commit test gate (P46.2). When set, the git_commit tool runs this
  # command in the workspace before every commit; a non-zero exit aborts the
  # commit and returns the command's output to the model. Makes "tests pass
  # before every commit" a mechanical gate rather than unenforced prose.
  # Empty (default) = git_commit stays a straight passthrough.
  #
  # It executes an arbitrary host command, so — like hooks and plugins — it is
  # frozen from untrusted project config by the workspace-trust gate: a cloned
  # repo's .aegis/config.yaml cannot introduce or change it until you run
  # `aegis trust`.
  pre_commit_test_command: ""    # e.g. "go test ./..." or "npm test"
  pre_commit_test_timeout_sec: 0  # 0 = default (600s)

# ── Output validation ─────────────────────────────────────────────────────────
output_guard:
  # enabled: true means every final answer is checked before being shown.
  # Toggle per-session with /guard on|off inside the TUI.
  enabled: true

  # "llm"    — second model call checks the answer against the rubric. Runs on
  #            provider.small_model when set (recommended, especially for local
  #            setups — a fast non-thinking judge), otherwise the session model.
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
# redteam-engagement, threat-modeling, latex-report, deep-research,
# structured-build — see
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
  - name: my-custom-lsp
    command: /opt/tools/my-custom-lsp
    args: []
    extensions: [".foo"]
    trust: true   # required for commands outside the built-in allowlist

# LSP servers start eagerly at daemon boot, with no interactive prompt
# available — a malicious project config could otherwise point `command` at
# an arbitrary binary and get it executed on first launch. `command` is
# checked by basename (path/extension stripped) against a small built-in
# allowlist of common LSP servers (gopls, typescript-language-server,
# pyright*, rust-analyzer, clangd, etc.); anything not on that list is
# refused unless the entry sets `trust: true`.


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
#
# `scan_output` (default true, P27.13/FIND-12) opts a server's
# tool/resource/prompt output into a heuristic prompt-injection scan before
# it reaches the model — a best-effort check (invisible characters,
# base64-encoded payloads) that only adds a visible warning on a hit, never
# blocks or mutates content, so it's safe to leave on even for well-vetted
# servers. Every server's output is always wrapped with a provenance marker
# regardless of this flag. Set `scan_output: false` per server to opt out
# (e.g. a high-volume trusted server where the extra scan pass isn't worth
# it). See docs/mcp-trust-boundary.md.
#
# `scan_arguments` (default false) is the outbound mirror (FIND-12):
# tool-call arguments are model-constructed and may carry anything the model
# has read into context, and they're forwarded verbatim to the target
# server — an exfiltration channel if the server is untrusted. When enabled,
# arguments bound for this server are checked against a small set of
# credential-shaped patterns (API keys, PEM private keys, AWS keys, bearer
# tokens, ...) and a hit logs a Warn naming the server, tool, and pattern
# class. Flag-only: the call is never blocked or altered. See
# docs/mcp-trust-boundary.md.
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
    # scan_output omitted → defaults to true; not fully vetted, so leave it on
    scan_arguments: true     # not fully vetted — warn if credential-shaped data heads its way


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
#   macOS/Linux: ~/.config/aegis/
#   Windows:     %AppData%\aegis\
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
- On Windows, after Aegis successfully reads this file it best-effort applies
  the same owner-only ACL hardening described below for `sessions.db` and
  the daemon auth token (a no-op on POSIX, where the file's own mode bits
  already restrict it). A failure to tighten the ACL of this pre-existing
  file only logs a warning — it never blocks startup.

---

## Local Data Store Permissions (Windows ACL Hardening)

A POSIX file mode bit (e.g. `0o600`) already restricts a file to its owner,
but has no effect on Windows: a new file there inherits its parent
directory's ACL, which commonly grants access to more than just the current
user. On Windows, Aegis applies an explicit, non-inherited, owner-only DACL
(`internal/fsguard`) to every local file that can hold secrets or
conversation history, so another local account on a shared machine can't
read them just because it can read the containing folder:

- The daemon auth token (`<data_dir>/auth`)
- The SQLite session database (`<data_dir>/sessions.db`) and its WAL-mode
  sidecar files (`sessions.db-wal`, `sessions.db-shm`) — this also covers
  checkpoint snapshots, which are stored in the same database
- `.aegis/.env`, best-effort, after Aegis successfully reads it (see above)

This is a defense-in-depth control for Host/OS-level access (FIND-29); it
does not protect against a compromised process running as the same user
account. On POSIX platforms all of the above is a no-op — the existing mode
bits are already sufficient.

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

### Bound daemon resources when exposing sessions to another harness (P21.5 / P27.12)

`aegis mcp-serve` lets another MCP-speaking harness create sessions and drive
runs through this daemon. `server.max_concurrent_runs` (default 10) and
`server.max_run_duration_sec` (default 1800/30 minutes) already give every
daemon a global concurrency ceiling and a wall-clock run timeout out of the
box, so a misbehaving or hostile caller can't fan out unbounded sessions/runs
and exhaust the host. Tighten or loosen them for your own deployment:

```yaml
server:
  max_concurrent_runs: 4     # refuse (429) runs past this many active at once
  max_run_duration_sec: 900  # abort any single run past 15 minutes
```

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
