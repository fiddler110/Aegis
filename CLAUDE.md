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

The multiscanner container image (`internal/security/multiscanner/Containerfile`
+ `fetch.sh`) bundles every filesystem scanner into one locally-built image, so
container-method scanning needs one image instead of a digest-pinned image per
tool. It's embedded via `go:embed` and built by `aegis security build-image`,
which records the resulting image ID into config; that ID is re-verified via
`image inspect` before every container run (a locally-built image has no
registry digest to pin — see `internal/security/multiscanner.go`). `go build`
needs no container runtime; only `build-image` does.

```bash
aegis security build-image --profile core    # static scanners only
aegis security build-image                   # full: + Python/Ruby/network, ~1.8GB
aegis security update-db                     # fill the DB cache volume (needs network)
```

Vulnerability databases are **not** in the image — they live in a container
volume (`aegis-scanner-cache`), populated by `aegis security update-db`. That's
both a size decision (baked in they were ~3.7GB of a 5.8GB image) and a
necessity: scanner containers run `--rm`, so without a persistent cache every
scan would re-download trivy's ~1.2GB DB. `update-db` is the **only** container
run given network access, and it mounts no workspace; scans keep
`--network none` and read the volume.

Two traps when changing this:
- The Containerfile and its scripts are `go:embed`-ed — **rebuild the binary**
  or `build-image` silently uses the old copy. Every file the Containerfile
  COPYs must be in the embed pattern (a test enforces this).
- Anything a scanner fetches on first use must be baked in **and** the tool
  told to use the local copy. Opengrep's "pinned" rule packs are still an HTTP
  fetch; osv-scanner calls api.osv.dev per run. `gosec` is excluded outright —
  it needs a Go toolchain and module resolution, and reports zero findings
  rather than failing without them (host 244 vs container 0 on this repo). See
  `multiscannerExcludedTools`.

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

# Live-probe tier: checks the tool-calling smoke probe (internal/toolcallprobe,
# shared by `aegis doctor` and the daemon's P34.2 warning) against a real
# model. It exists because the probe's correctness is a claim about real model
# behavior that no unit test can hold — the P34.2 false positive (a reasoning
# model thinking past the token cap, reported as "can't call tools") lived
# through a fully green suite. Run it when changing SmokeMaxTokens, the smoke
# prompt, or the verdict rules. Same on-demand, no-scheduled-CI policy as the
# tiers below. Override the default model/endpoint with
# AEGIS_LIVE_PROBE_MODEL / AEGIS_LIVE_PROBE_URL.
ollama pull qwen3:14b
go test -tags live_probe ./internal/toolcallprobe/... -run TestLiveProbeReachesAVerdict -v

# Live-workflow eval tier (P25.7): drives a real daemon over the same HTTP
# API + SSE seam the TUI/web UI use — not a scripted adapter — against a
# real local model, running a seeded-bug fix/verify task and asserting
# workflow-shape invariants (tool-call count, no web-search/`find /`
# detours, non-zero token usage, no guard meta-text leakage, no unrequested
# files). This is what actually caught the P25.1-P25.6 regressions; the
# live_eval tier above never touches the daemon/sandbox/guard integration.
# Needs a reachable Ollama server and python3/python on PATH. On-demand
# only, same no-scheduled-CI-job policy as live_eval. To run locally:
ollama pull qwen3.6:35b-a3b-deep   # or any tool-calling-capable local model
go test -tags live_workflow ./internal/eval/... -run TestLiveWorkflow -v
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
| `internal/tool/builtin` | All 50+ built-in tools (file ops, git, shell, web, memory, LSP, security scan, security engagement/CVE lookup, diagram, cron, agent spawning, etc.) |
| `internal/permission` | Three modes: `plan` (read-only), `build` (read+write, execute gated), `auto` (all allowed); text-based allow/deny rules; `PersonaToolGate` advisory (never-enforcing) check on a persona's declared `Tools` |
| `internal/persona` | 22 built-in named system prompts (general, security, developer, SRE, red-team, security-critic/security-arbiter and generic critic/arbiter debate roles, etc.); custom personas are `.md` files with YAML frontmatter, hot-reloaded via a signature-cached `Refresh` |
| `internal/skills` | Progressive-disclosure skills: project/user `.md`/bundled-directory skill files, plus skills embedded in the binary (`go:embed`) that stay dormant until named in config/CLI/TUI |
| `internal/drive` | The phased skill drive: runs a multi-phase skill build (e.g. threat-modeling) as a sequence of fresh, context-reset `engine.Run`s instead of one ever-growing conversation, so local-model context limits don't stall a long unattended build. Lifted out of the CLI (`internal/cli/chat_phased.go`) into its own package so the daemon, TUI, and web UI all reach it, not just `aegis chat --skill` |
| `internal/swarm` | Multi-agent coordination: spawns sub-agents as goroutines (`in_process`) or subprocesses; file-based mailbox for inter-agent messaging |
| `internal/debate` | Multi-agent-debate (MAD) primitive (P12): propose/critique/rebut/arbitrate over a claim via a caller-supplied `RunFunc`, decoupled from swarm/engine the same way swarm is decoupled from engine; evidence-citation check (P12.3) and shared-tracker budget bound (P12.6) live here; `Config.Domain` (`security`/`generic`) selects the default persona trio and `WithFiles` grounds the claim in specific files, so the same primitive covers security findings and non-security document/plan review (see [docs/debate.md](docs/debate.md)) |
| `internal/compaction` | Context compaction — summarizes old turns when the conversation approaches the model's context window |
| `internal/checkpoint` | Per-turn restore points for `/rewind` |
| `internal/memory` | Project-level and user-level persistent memory; relevance scoring for context injection |
| `internal/knowledge` | Project-level knowledge base (distinct from `internal/memory`'s relevance-scored recall) surfaced to personas/skills as grounding context |
| `internal/repomap` | Builds a compact structural overview of the repo — files, top-level symbols, import edges — injected as `<repo_map>`; regex-based extraction (no tree-sitter/CGo), capped to a byte budget, mtime-cached |
| `internal/lsp` | Minimal LSP client managing language-server subprocesses over stdio JSON-RPC, for diagnostics and reference resolution (`LSP` tool, `aegis doctor`) |
| `internal/cost` | Tracks token spend per run/session and converts it to an estimated USD cost; backs the budget knobs (`cost.*` config: USD, token, wall-clock, iteration limits) enforced in `internal/engine` |
| `internal/tui` | Bubbletea TUI: timeline, streaming, dialog, persona/session pickers, slash commands, cost display |
| `internal/config` | Layered config (defaults → `~/.config/aegis/config.yaml` → `.aegis/config.yaml` → `AEGIS_*` env vars) |
| `internal/mcp` | MCP client (stdio + HTTP/SSE) — Aegis calling *out* to external MCP servers; registered tools appear alongside builtins |
| `internal/mcpserver` | MCP server (`aegis mcp-serve`) — the reverse direction: exposes Aegis sessions as MCP tools (`aegis_prompt`, `aegis_new_session`, `aegis_list_sessions`) to other MCP-speaking harnesses |
| `internal/acp` | ACP JSON-RPC server for editor integrations (Zed, Neovim) |
| `internal/sandbox` | Pluggable execution sandbox: local, Docker, Podman, WSL containers, Apple Containers |
| `internal/workspacetrust` | Per-directory trust decisions (`aegis trust --dir`) gating which roots a session may touch — including `workspace.additional_roots` entries, which need their own trust grant even when frozen from project config |
| `internal/cron` | Cron scheduler for background tasks; shelled commands run under a fixed `cronJobTimeout` (10 min, `internal/server/helpers.go`) |
| `internal/guard` | Output validation — calls a second model pass against a rubric or JSON schema |
| `internal/toolcallprobe` | Tool-calling smoke probe shared by `aegis doctor` and the daemon's model-switch warning — checks a model can actually emit tool calls before a session relies on it |
| `internal/eval` | Scenario-based agent-behavior regression harness: scripted multi-turn conversations run against a real engine (deterministic adapter, no live model) with tool-call/text/error assertions and golden transcripts |

### Provider model

`provider.Adapter` is the single seam between the engine and any LLM backend. It exposes one method:

```go
Stream(ctx context.Context, req Request) (<-chan Event, error)
```

All message types (TextBlock, ToolUseBlock, ToolResultBlock, ThinkingBlock) are defined in `internal/provider/provider.go`. The Anthropic adapter maps these natively; the OpenAI adapter translates to/from chat-completions format.

### Tool capability model

Every tool declares a `Capability` (`read`, `write`, `execute`, `network`, `spawn`). The permission gate consults this before execution. In `engine.runTools`, read/network tools run concurrently while write/execute tools are serialized via `sync.RWMutex`.

### Run budgets

`engine.Options` carries four independent stop conditions, all checked at the same two gates (before each model turn and before each tool round): `BudgetUSD` (a no-op for unpriced local usage), `MaxTokensPerRun` (0 = unbounded), `MaxIterations` (defaults to 40 steps), and `MaxWallClockPerRun` (`cost.max_wall_clock_per_run`, seconds — **off by default**, since a wall-clock cap can't tell a stalled run from a slow one making real progress). A wall-clock abort is fatal to the drive rather than resumable, unlike a context-overflow or tool-failure-breaker reset; sub-agents inherit the parent's bound whole rather than a divided share, since elapsed time isn't additive across concurrent teammates the way spend is.

### Configuration layers

Precedence (lowest → highest): built-in defaults → `~/.config/aegis/config.yaml` → `.aegis/config.yaml` (project-level) → `AEGIS_*` env vars. Secrets (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`) come only from the environment. `.aegis/.env` is supported for local secrets without environment pollution. `workspace.additional_roots` widens sandbox confinement beyond the primary workspace root for cross-repo workflows; it's frozen from untrusted project config (only `~/.config`-level or env can set it) and each root still needs its own `aegis trust --dir` grant.

### Persona system

Personas are `.md` files whose YAML frontmatter (`description`, `model`, `mode`, `tools`, `rules`, `output_guard`) carries overrides; the name is the filename stem. Built-ins live in `internal/persona/builtin/*.md`, embedded via `go:embed` and parsed at package init (`internal/persona/builtin.go`) into the same `Persona` struct as user/project personas — only `Loaded` (false for built-ins) and `Path` differ. Custom personas are `.md` files under project `.aegis/personas/` or user `~/.aegis/personas/`, parsed by `internal/persona/load.go`. A persona can pin a model, declare an advisory tool list, merge permission rules, and override the output guard rubric. The daemon hot-reloads on-disk persona files (`persona.Refresh`, called from persona-touching handlers) — no restart needed; built-ins only change on rebuild. Switching persona mid-session (`PATCH /sessions/{id}` with `persona`) applies the full profile, not just the system prompt. `aegis persona list|show|new|use` manages persona files and the project's `default_persona` (config key; falls back to `general`) from the CLI.

Each built-in persona's `Tools` field is **advisory, never enforced**: `permission.PersonaToolGate` (wrapped outermost in `server.newEngine`) logs a call outside the list and routes it through the same `Approver` used for capability decisions — warn-and-allow under a non-interactive approver, a confirmation prompt under the TUI's — but the real security boundary (mode, rules, contextual gates) is untouched and always still applies. `general` deliberately leaves `Tools` empty (no restriction at all).

### Skills system

Skills (`internal/skills`) are progressive-disclosure playbooks: at session start only a `name — description` line is injected per skill into `<skills_available>`; the model loads the full body on demand via the `skill` tool. A skill is a `.md` file (or a directory bundling a `SKILL.md` manifest with companion assets like templates/scripts, referenced via a generated `<skill_assets>` manifest) under project `.aegis/skills/` or user `~/.aegis/skills/`. Skills without a frontmatter `description:` fall back to eager injection for backward compatibility.

Aegis also ships several skills **embedded in the binary** (`internal/skills/builtin/`, extracted via `go:embed` and materialized to `<data_dir>/builtin-skills/` at daemon startup by `skills.MaterializeBuiltins`) — content-review, html-report, security-audit, architecture-diagram, debug-investigation, redteam-engagement, threat-modeling, latex-report, deep-research, structured-build. These stay **dormant by default** (zero system-prompt cost) until named in the `skills.builtin_enabled` config list, via `aegis skills enable <name> [--global]`, or the `/skills enable <name> [global]` TUI command; disable the same way. Precedence when a name collides: project skill file > user skill file > embedded built-in.
