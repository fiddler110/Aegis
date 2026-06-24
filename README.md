# agentharness

A personal agent harness for **research, documentation, coding, and security-platform
architecture**, built from scratch in Go with a terminal-first interface.

It borrows proven patterns from existing agents — provider abstraction and context
compression (Hermes), a persistent daemon + client sessions and Plan/Build modes
(opencode), slash-command skills, subagents, permission modes and file-based memory
(Claude Code), and sandboxed local tooling (OpenClaw) — while staying a clean, single
codebase you own.

## Architecture

A background **daemon** (`harness serve`) owns sessions, the agent engine, and tools.
Clients (the **TUI** and the `chat` command) talk to it over a local HTTP API with
server-sent events, so sessions survive client restarts.

```
cmd/harness            entry point (TUI client + subcommands)
internal/
  engine               agent loop: model -> tools -> repeat, with gating, hooks, compaction
  provider             normalized model interface
    anthropic          Anthropic Messages adapter (SSE)
    openai             OpenAI chat-completions adapter (SSE)
  providerfactory      build an adapter from config
  tool                 tool registry (registration vs. exposure)
    builtin            read/write/edit/glob/grep/shell/web/security/diagram/memory tools
  permission           Plan/Build policies + approval gate
  compaction           token-budget summarization of old turns
  memory               file-based memory + skills loaded into the system prompt
  security             semgrep/trivy/gitleaks runners + normalized findings
  diagram              Kroki + CLI renderers; Mermaid/PlantUML/C4/draw.io
  persona              general + security-architect system prompts
  hooks                pre/post tool-call hooks (audit trail)
  mcp                  minimal MCP (JSON-RPC/stdio) client
  session              SQLite session store
  server / client      daemon HTTP+SSE API and its client
  tui                  Bubble Tea terminal UI
```

## Build & run

```sh
go build ./...
export ANTHROPIC_API_KEY=sk-...     # or OPENAI_API_KEY with provider=openai

go run ./cmd/harness serve          # start the daemon (separate terminal)
go run ./cmd/harness                # launch the TUI
go run ./cmd/harness --persona security --mode build   # security architect, can edit

# without the daemon:
go run ./cmd/harness chat "summarize main.go" --mode build --yes
go run ./cmd/harness scan ./path                       # run security scanners
go run ./cmd/harness diagram --type mermaid --out a.svg < diagram.mmd
go run ./cmd/harness sessions list
```

## Capabilities

- **Coding/research/docs**: file read/write/edit, glob, grep, sandboxed shell, web
  fetch/search — all confined to the workspace.
- **Permission modes**: `plan` (read-only) vs `build` (can mutate); shell/execute is
  gated by approval. Every tool call is recorded to an audit trail (`audit.jsonl`).
- **Memory & skills**: `remember` and `save_skill` tools persist facts/procedures into
  files that are reloaded into the system prompt.
- **Security architect** (`--persona security`): `security_scan` runs semgrep, trivy,
  and gitleaks into a unified findings report; the persona prompt drives capability
  research, STRIDE/LINDDUN threat modeling, and C4/Mermaid architecture.
- **Diagrams**: `render_diagram` / `harness diagram` render Mermaid, PlantUML, C4,
  Graphviz, etc. to SVG/PNG, plus draw.io export, via Kroki with a local CLI fallback.
- **Extensible**: multiple providers (Anthropic, OpenAI), external **MCP** servers
  consumed as tools, and pre/post tool-call hooks.

## Configuration

Resolved with precedence (low → high): built-in defaults → global config
(`<user-config-dir>/agentharness/config.yaml`) → project config
(`./.agentharness/config.yaml`) → environment (`AGENTHARNESS_*`). API keys come from the
environment only (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`).

```yaml
# ~/.config/agentharness/config.yaml (or %AppData%\agentharness on Windows)
provider:
  default: anthropic        # or openai
  model: claude-opus-4-8
permission:
  mode: plan                # plan | build
  auto_approve_exec: false  # auto-approve shell in build mode
diagram:
  kroki_url: https://kroki.io
mcp:
  - name: filesystem
    command: npx
    args: ["-y", "@modelcontextprotocol/server-filesystem", "."]
```

## Tests

`go test ./...` — every package has unit tests; provider adapters are tested against
the real SSE wire format via `httptest`, the daemon end-to-end with a mock adapter, and
the MCP client against an in-memory fake server. Live model calls require an API key.
