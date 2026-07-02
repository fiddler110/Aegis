# Aegis

An AI agent harness for **security, architecture, research, and development**, built from scratch in Go with a terminal-first interface. Designed to work with **local LLMs** (Ollama, LM Studio, LiteLLM) as the primary workflow, with the ability to connect to cloud providers (Anthropic, OpenAI) when needed.

It borrows proven patterns from existing agents — provider abstraction and context compression (Hermes), persistent daemon + client sessions and Plan/Build modes (opencode), slash-command skills, subagents, permission modes, and file-based memory (Claude Code) — while staying a clean, single codebase you own.

## Quick Start

Already have a local LLM server running? Zero to chatting in under two minutes.

**1. Clone and build**

```bash
# macOS
git clone https://github.com/fiddler110/Aegis.git && cd Aegis
chmod +x build-macos.sh && ./build-macos.sh

# Linux
git clone https://github.com/fiddler110/Aegis.git && cd Aegis
chmod +x build-linux.sh && ./build-linux.sh
```

```powershell
# Windows (PowerShell)
git clone https://github.com/fiddler110/Aegis.git; cd Aegis
.\build-windows.ps1
```

The script prompts you to: (1) compile and install `aegis` to your Go bin directory, and (2) optionally add an `aegis-config` shell helper that opens the config file in your editor.

**2. Generate config and set the API key**

```bash
aegis --first-init
# Local servers don't validate the key, but the harness requires it to be non-empty
export OPENAI_API_KEY="ollama"   # Linux/macOS — add to ~/.zshrc for permanence
```

```powershell
$env:OPENAI_API_KEY = "ollama"   # Windows — add to System Environment Variables
```

**3. Pull a model and launch**

```bash
ollama pull llama3.2
aegis
```

The daemon starts automatically — no second terminal needed. Use `/help` inside the TUI for available commands, or `/config` to change provider and model interactively.

---

## Local LLM Backends

Aegis works with any server exposing an OpenAI-compatible API (`/v1/chat/completions`).

| Server | Default Base URL |
|--------|-----------------|
| [Ollama](https://ollama.com) | `http://localhost:11434/v1` |
| [LM Studio](https://lmstudio.ai) | `http://localhost:1234/v1` |
| [llama.cpp](https://github.com/ggerganov/llama.cpp) | `http://localhost:8080/v1` |
| [vLLM](https://github.com/vllm-project/vllm) | `http://localhost:8000/v1` |
| [LiteLLM](https://github.com/BerriAI/litellm) | `http://localhost:4000/v1` |
| [LocalAI](https://github.com/mudler/LocalAI) | `http://localhost:8080/v1` |
| [Jan](https://jan.ai) | `http://localhost:1337/v1` |
| [KoboldCpp](https://github.com/LostRuins/koboldcpp) | `http://localhost:5001/v1` |
| [text-generation-webui](https://github.com/oobabooga/text-generation-webui) | `http://localhost:5000/v1` |

Aegis starts Ollama automatically if it's installed but not running. Set `model: "auto"` to pick the first available model without hardcoding a name.

> Set `AEGIS_PROVIDER_BASE_URL` and `AEGIS_PROVIDER_MODEL` to use any backend not listed here.

---

## Features

- **Daemon + client architecture** — durable SQLite-backed sessions survive TUI restarts; resume any session with `aegis --resume <id>`
- **Plan / Build / Auto modes** — read-only exploration, guided file editing, or fully automatic; fine-grained text-based allow/deny rules per tool and path
- **39 built-in tools** — file ops, git (read + commit), shell with background jobs, web fetch/search, LaTeX compilation, LSP diagnostics, security scanning, diagram rendering, memory, planning, and more
- **17 built-in personas** + custom persona files — security, developer, SRE, cloud architect, risk assessor, and others; each can pin a model and carry its own permission rules
- **Output validation** — a lightweight second-model pass checks every answer against a configurable rubric before it's returned
- **Checkpoints & rewind** — every user turn captures a restore point; `/rewind` reverts files, conversation, or both without `git reset` gymnastics
- **Multi-agent orchestration** — spawn sub-agents (goroutines or subprocesses) with their own permission scopes and an inter-agent file mailbox
- **Parallel sessions** — run independent prompts concurrently with `aegis parallel`
- **MCP support** — consume any Model Context Protocol server as additional tools (stdio or HTTP/SSE)
- **Extended thinking** — Anthropic extended thinking streamed live to the TUI; `think` flag for local reasoning models (qwen3, deepseek-r1)
- **Container sandbox** — Docker, Podman, WSL containers, Apple Containers; `aegis sandbox detect` to probe what's available
- **Web UI + ACP editor integration** — browser dashboard at `aegis ui`; ACP protocol for Zed, Neovim (codecompanion/avante), and other editors
- **Cost tracking** — live token meter, prompt-cache hit rate, configurable spend budget that halts a run when exceeded
- **Session export** — share transcripts as self-contained HTML, Markdown, or JSON with `aegis sessions export`

---

## Documentation

| Document | What it covers |
|----------|----------------|
| [Overview & Architecture](docs/overview.md) | Daemon/client model, agent loop, event system |
| [Installation & First Run](docs/installation.md) | Build scripts, platform setup, first-time configuration |
| [Configuration Reference](docs/configuration.md) | Every config key, environment variables, common recipes |
| [CLI Reference](docs/cli-reference.md) | Every command and flag |
| [TUI Guide](docs/tui-guide.md) | Layout, keyboard shortcuts, slash commands, `@` references |
| [Tools Reference](docs/tools-reference.md) | All 39 built-in tools with inputs, outputs, and examples |
| [Personas](docs/personas.md) | All 17 built-in personas, custom persona files, per-persona model overrides |
| [Permission System](docs/permissions.md) | Plan/Build/Auto modes, text-based rules, contextual security policies |
| [Session Management](docs/sessions.md) | Checkpoints, rewind, export, archiving |
| [Providers & Models](docs/providers.md) | Local LLMs, cloud providers, model selection, extended thinking |
| [Memory & Knowledge](docs/memory-and-knowledge.md) | Project/user memory, skills, knowledge base |
| [Extensibility](docs/extensibility.md) | MCP servers, custom commands, agents, process plugins, bundles |
| [Multi-Agent & Background Tasks](docs/multi-agent.md) | Swarm, parallel sessions, background tasks, cron scheduling |
| [Security Features](docs/security.md) | Security scanning, sandbox backends, contextual policies, audit trail |

---

## License

This is a personal project. See [LICENSE](LICENSE) for details.
