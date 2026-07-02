# Installation & First Run

## Prerequisites

- **Go 1.25+** — [go.dev/dl](https://go.dev/dl/)
- **Git**
- A local LLM server **or** a cloud API key (Anthropic or OpenAI)

---

## Building from Source

The recommended way to install Aegis is with the platform build scripts. Each script shows a plan, asks which actions to run, embeds the git version into the binary, installs to your Go bin directory, and optionally adds an `aegis-config` shell helper.

### macOS

```bash
git clone https://github.com/fiddler110/Aegis.git
cd Aegis
chmod +x build-macos.sh && ./build-macos.sh
```

The script installs to `/usr/local/bin` and targets `~/.zshrc` (zsh, default since Catalina) or `~/.bash_profile` (bash).

### Linux

```bash
git clone https://github.com/fiddler110/Aegis.git
cd Aegis
chmod +x build-linux.sh && ./build-linux.sh
```

The script installs to `/usr/local/bin` (with `sudo` if needed) or falls back to `~/go/bin`. It detects your shell (bash, zsh, fish) and adds the `aegis-config` function to the appropriate file.

### Windows (PowerShell)

```powershell
git clone https://github.com/fiddler110/Aegis.git
cd Aegis
.\build-windows.ps1
```

Installs `aegis.exe` to `%GOPATH%\bin` (default `%USERPROFILE%\go\bin`).

### What the build script does

Each script presents two optional actions:

1. **[1] Build and install** — compiles with version info embedded, installs binary, detects and removes any stale binary at a different PATH location
2. **[2] Add `aegis-config` helper** — adds a shell function/alias that opens your global config file in `$EDITOR`

### Manual build (without the scripts)

```bash
go build -o aegis ./cmd/aegis        # macOS / Linux
go build -o aegis.exe ./cmd/aegis    # Windows
```

Then copy the binary to a directory on your `$PATH`.

---

## First-Time Setup

### Step 1: Generate your global config

```bash
aegis --first-init
```

This writes a full configuration template to your OS config directory:

| Platform | Path |
|----------|------|
| macOS | `~/.config/aegis/config.yaml` |
| Linux | `~/.config/aegis/config.yaml` |
| Windows | `%AppData%\aegis\config.yaml` |

The template has **Ollama active by default** and all other providers (Anthropic, OpenAI, Azure, Groq, OpenRouter, LM Studio, Vertex AI) in commented-out blocks ready to activate.

### Step 2: Set your API key environment variable

Local LLM servers don't validate API keys, but Aegis requires a non-empty value:

```bash
# macOS / Linux — add to ~/.zshrc or ~/.bashrc for permanence
export OPENAI_API_KEY="ollama"

# Windows PowerShell — add to System Environment Variables for permanence
$env:OPENAI_API_KEY = "ollama"
```

For cloud providers, use the real key:

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
export OPENAI_API_KEY="sk-..."
```

### Step 3: Pull a model (Ollama)

```bash
# Pull at least one model — Ollama keeps it cached
ollama pull llama3.2

# Better tool-use performance:
ollama pull qwen2.5:32b
```

Aegis **auto-starts Ollama** if it is installed but not running. You do not need to run `ollama serve` manually.

### Step 4: Launch

```bash
aegis
```

The daemon starts automatically in the same process. Type `/help` in the TUI for available commands.

---

## Project-Level Setup

For per-project configuration (safe to commit, no secrets):

```bash
cd /your/project
aegis --init
```

This creates `.aegis/config.yaml` with commented examples for overriding model, permission mode, cost budget, and network allowlist on a per-project basis.

**Project directory structure:**

```
.aegis/
  config.yaml       Project config override
  .env              Local secrets (add to .gitignore)
  memory.md         Project memory (facts for every session)
  skills/           Reusable procedure files
  commands/         Custom slash commands
  personas/         Custom persona definitions
  agents/           Custom agent definitions
  worktrees/        Git worktrees (created by aegis worktree add)
  repomap.json      Repository structure cache (aegis index)
  knowledge.db      Project knowledge base (aegis index)
```

---

## Configuration Quick-Edit

If you ran build action [2], open your config at any time with:

```bash
aegis-config
```

On Windows, reload your profile first: `. $PROFILE`
On macOS/Linux: `source ~/.zshrc` (or the file the script reports)

Or use the `/config` wizard inside the TUI to change provider/model settings interactively.

---

## Verifying Your Installation

```bash
# Show resolved config (no model call)
aegis dry-run

# Probe for local LLM servers
aegis models --local

# Show resolved config
aegis config
```

---

## Using a Local LLM

The generated config defaults to Ollama. To use a different local server:

```yaml
# .aegis/config.yaml or ~/.config/aegis/config.yaml
provider:
  default: openai                          # all local LLMs use the "openai" adapter
  base_url: "http://localhost:1234/v1"     # LM Studio
  model: "lmstudio-community/Meta-Llama-3-8B-Instruct-GGUF"
  max_tokens: 8192
```

Or use an environment variable to override without editing the file:

```bash
export AEGIS_PROVIDER_BASE_URL="http://localhost:1234/v1"
export AEGIS_PROVIDER_MODEL="my-model-name"
```

See [Providers & Models](providers.md) for the complete list of supported local servers and cloud providers.

---

## Using Cloud Providers

**Anthropic:**

```bash
export ANTHROPIC_API_KEY="sk-ant-..."
```

```yaml
provider:
  default: anthropic
  model: "claude-opus-4-8"
  max_tokens: 16384
```

**OpenAI:**

```bash
export OPENAI_API_KEY="sk-..."
```

```yaml
provider:
  default: openai
  model: "gpt-4o"
  max_tokens: 16384
```

All other providers (Azure, Groq, OpenRouter, Vertex AI) have ready-to-uncomment blocks in the `--first-init` template.

---

## Upgrading

Pull the latest changes and rebuild:

```bash
cd Aegis
git pull
./build-macos.sh   # or your platform script
```

The build script detects stale binaries at other PATH locations and removes them.
