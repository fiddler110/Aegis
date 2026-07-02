# Aegis Documentation

Welcome to the Aegis documentation. These guides cover every aspect of using, configuring, and extending Aegis.

## Contents

| Document | What it covers |
|----------|----------------|
| [Overview & Architecture](overview.md) | How Aegis works internally: daemon/client model, agent loop, event system |
| [Installation & First Run](installation.md) | Building from source, platform setup, first-time configuration |
| [Configuration Reference](configuration.md) | Every config key explained, precedence rules, environment variables |
| [CLI Reference](cli-reference.md) | Every command and flag (`aegis`, `aegis serve`, `aegis chat`, `aegis sessions`, …) |
| [TUI Guide](tui-guide.md) | Terminal interface layout, keyboard shortcuts, all slash commands |
| [Tools Reference](tools-reference.md) | All 39 built-in tools with inputs, outputs, and examples |
| [Personas](personas.md) | All 17 built-in personas, custom persona files, per-persona model overrides |
| [Permission System](permissions.md) | Plan/Build/Auto modes, text-based rules, contextual security policies |
| [Session Management](sessions.md) | Durable sessions, checkpoints, rewind, export, archiving |
| [Providers & Models](providers.md) | Local LLMs, cloud providers, model selection, extended thinking |
| [Memory & Knowledge](memory-and-knowledge.md) | Project/user memory, skills, project knowledge base, long-term entity store |
| [Extensibility](extensibility.md) | MCP servers, custom commands, custom agents, process plugins, bundles |
| [Multi-Agent & Background Tasks](multi-agent.md) | Swarm, sub-agents, parallel sessions, background tasks, cron scheduling |
| [Security Features](security.md) | Security scanning, sandbox backends, contextual security policies |

## Quick Navigation

**New to Aegis?** Start with [Installation & First Run](installation.md), then [TUI Guide](tui-guide.md).

**Configuring providers?** See [Providers & Models](providers.md) and [Configuration Reference](configuration.md).

**Writing automation?** See [CLI Reference](cli-reference.md) and [Tools Reference](tools-reference.md).

**Extending Aegis?** See [Extensibility](extensibility.md).
