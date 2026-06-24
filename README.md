# agentharness

A personal agent harness for **research, documentation, coding, and security-platform
architecture**, built from scratch in Go with a terminal-first interface.

It borrows proven patterns from existing agents — provider abstraction and context
compression (Hermes), a persistent daemon + client sessions and Plan/Build modes
(opencode), slash-command skills, subagents, permission modes and file-based memory
(Claude Code), and sandboxed local tooling (OpenClaw) — while staying a clean, single
codebase you own.

## Status

Early development. Built in phases:

1. **Scaffold** — module, CLI, layered config, logging ✅
2. Provider adapter + engine loop
3. Tool system + builtins + permissions
4. Sessions + daemon + TUI
5. Context management + memory/skills
6. Security persona (scanners + LLM threat modeling)
7. Diagram subsystem (Mermaid / PlantUML / C4 / draw.io)
8. Extensibility (MCP, plugins, more providers)

## Build & run

```sh
go build ./...
go run ./cmd/harness            # status (TUI lands in Phase 4)
go run ./cmd/harness config     # print resolved config
go run ./cmd/harness serve      # run the daemon
```

## Configuration

Resolved with precedence (low → high): built-in defaults → global config
(`<user-config-dir>/agentharness/config.yaml`) → project config
(`./.agentharness/config.yaml`) → environment (`AGENTHARNESS_*`).

API keys come from the environment only (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`).
