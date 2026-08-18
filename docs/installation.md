# Installation & First Run

## Prerequisites

- **Go 1.25+** — [go.dev/dl](https://go.dev/dl/)
- **Git**
- A local LLM server **or** a cloud API key (Anthropic or OpenAI)
- *Optional, for security scanning:* **Docker or Podman**. Aegis builds its own scanner images locally rather than installing a dozen host binaries — see [Security scanners](#security-scanners-container-or-host) below. Node.js is **not** needed: the web UI's build output is committed and embedded.

---

## Building from Source

The recommended way to install Aegis is with the platform build scripts. Each script shows a plan, asks which actions to run, embeds the git version into the binary, installs to your Go bin directory, and optionally adds an `aegis-config` shell helper.

Alternatively, tagged releases (`vX.Y.Z`) publish prebuilt binaries for linux/amd64, linux/arm64, darwin/amd64, darwin/arm64, and windows/amd64 on the repository's [Releases page](https://github.com/fiddler110/Aegis/releases), built by [`.github/workflows/release.yml`](../.github/workflows/release.yml) with the same version-stamped `-ldflags` the build scripts use.

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

Each script presents four actions — pass a selection non-interactively (e.g. `./build-linux.sh 1`, `./build-windows.ps1 "all 3"`) or answer the prompt:

1. **[1] Build and install** — compiles with version info embedded, installs binary, detects and removes any stale binary at a different PATH location
2. **[2] Add `aegis-config` helper** — adds a shell function/alias that opens your global config file in `$EDITOR`
3. **[3] Build the container scanner images** *(opt-in, recommended)* — the container path described below
4. **[4] Install host scanner binaries** *(opt-in, fallback)* — the host path described below

`all` always means "actions 1 + 2". Actions 3 and 4 must be selected explicitly (`3` alone, or `"all 3"` together) because both are large, privileged, host-modifying operations.

### Manual build (without the scripts)

```bash
go build -o aegis ./cmd/aegis        # macOS / Linux
go build -o aegis.exe ./cmd/aegis    # Windows
```

Then copy the binary to a directory on your `$PATH`. No container runtime and no Node.js are needed to build — only `aegis security build-image` needs a runtime.

---

## Security Scanners: Container or Host

`aegis scan` and the `security_scan` tool need scanners. There are two ways to supply them, and they are **alternatives, not a sequence**.

### The container path (recommended) — action [3]

Aegis builds **its own** scanner images from a `go:embed`-ed build context. Nothing is pulled from a third-party registry, and the image ID is recorded in config and re-verified via `image inspect` before every run, so an image rebuilt or retagged behind Aegis's back fails closed rather than silently running something else.

```bash
aegis security build-image                 # full profile (~2-3GB), verifies + pins
aegis security build-image --profile core  # static scanners only (~1GB)
aegis security update-db                   # fill the shared vulnerability-DB volume
aegis security build-image --netscanner    # optional second image (~570MB)
aegis security status                      # what resolves where, and DB ages
```

Action [3] runs exactly that sequence, after asking which profile you want and whether to add the netscanner. Four things worth knowing before you run it:

- **Two images, split by mount posture — not tool category.** The **multiscanner** runs with `--network none` and your workspace mounted. The **netscanner** runs with network **on** and the workspace **never** mounted, because nmap, nuclei, and image-reference scanning with trivy/grype each scan a *remote* target and none of them needs your source.
- **The vulnerability databases are not in the image.** They live in a shared container volume filled by `aegis security update-db` — both a size decision and a necessity, since scanner containers run `--rm` and would otherwise re-download trivy's ~1.2GB database on every scan. Re-run `update-db` periodically: a stale database **under-reports rather than failing**, and `aegis security status` shows how old each one is. The netscanner needs no `update-db` — having network, it refreshes its own.
- **The pin is machine-wide.** `build-image` writes it to your **user** config (`~/.config/aegis/config.yaml`, `%AppData%\aegis\config.yaml`), because the images and the DB volume are machine-wide too. Do **not** copy a `multiscanner:`/`netscanner:` block into a repo's `.aegis/config.yaml`: project config wins over user config, so it shadows the machine-wide pin and fails closed on the next rebuild with *"no longer matches the ID recorded in config"*. Use `build-image --project` only when a repo deliberately runs a different image.
- **The build verifies itself.** Each scanner is probed *and* run against a fixture with planted findings, asserting a non-zero finding count. That assertion is the point: a `--version` probe cannot catch a tool that exits clean reporting zero because it never loaded its data. Don't pass `--skip-verify`.

**Runtime notes.** Only `docker` and `podman` can build the images — `wslc` (Windows Containers) is a *run* backend with no build path for this context, which is part of why it is **last** in the Windows auto-detect order (`podman → docker → wslc`). The build scripts still pass `--runtime` explicitly: a locally-built image lives only in the storage of the engine that built it, so on a machine with both engines installed, auto-detection can pick the one that has nothing. On **macOS**, "installed" and "running" are different states — start Docker Desktop, `colima start`, or `podman machine start` first.

If you have no Windows-side runtime, `build-windows.ps1 3` probes your WSL distros for one (pass `-WslDistro <name>` to target a specific one). It will offer to cross-compile a Linux `aegis` and build there — but note what that gets you: a locally-built image exists only in the storage of the engine that built it, so those scanners serve an Aegis run *inside* that distro, not `aegis.exe` on Windows. If you want `aegis.exe` to use them, enable Docker/Podman Desktop's WSL integration instead, which puts the client on the Windows PATH.

### The host path (fallback) — action [4]

Action [4] runs `aegis security install <tool> --yes` for each of: dockle, opengrep, gosec, bandit, brakeman, njsscan, trivy, gitleaks, trufflehog, kubescape, hadolint, grype, osv-scanner, syft, nmap, nuclei. Tools already on `PATH` are skipped. It's best-effort — several need a specific language toolchain (Go/pipx/gem) or package manager (Homebrew/scoop) — so individual failures are summarized rather than aborting the run.

Prefer action [3] where you can. Host binaries are unpinned and unconfined, which means two machines can silently scan with different rule sets, and under `default_method: auto` a tool the multiscanner image carries resolves to the **container** over an available host binary anyway (a refused container falls back to host, and says so).

Two scanners stay host-only regardless:

| Tool | Why no image can carry it |
|------|---------------------------|
| `dockle` | Inspects images through the container engine socket — effectively host root, a privilege level neither scanner image grants. Install it with `aegis security install dockle`. |
| `zap` | A large Java app with its own official image and mount contract (`/zap/wrk`); Aegis invokes that image directly, so there's no guided host install either. |

See [security_scan.md](security_scan.md) for what each scanner does, and [cli-reference.md](cli-reference.md#aegis-security-build-image) for every flag.

#### Windows scoop installs failing

If a `scoop install <tool>` guided install fails with `The term 'Get-FileHash' is not
recognized...`, it's a PATH-ordering conflict on machines that installed PowerShell 7 via
winget/the Microsoft Store: that installer commonly prepends its own Core-edition-only module
directory ahead of the system32 Windows PowerShell one in `PSModulePath`. A spawned
`powershell.exe` (5.1) process inherits that ordering and autoloads the wrong
`Microsoft.PowerShell.Utility` module — one that doesn't export `Get-FileHash`, which scoop's
own install scripts use to verify downloads. Aegis works around this by preferring `pwsh`
(PowerShell 7, which only ever loads its own Core-compatible modules) over `powershell` for
every guided install and shell-tool command on Windows, falling back to `powershell` only when
`pwsh` isn't on `PATH`.

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

**On a local GPU, the wizard asks for a VRAM budget.** It is optional — leave it blank and the config
written is exactly what earlier versions produced. Answering it does two things:

- sizes `context_window` from what your card can actually hold, instead of from the model's training
  maximum. A model with a 262144-token training context otherwise gets 131072, which is 16.5 GiB of
  KV cache before any weights are loaded — more than a 16 GB card holds;
- lets a debate plan its two or three seats against one budget, instead of sizing each as if it were
  alone. See [debate.md](debate.md#fitting-the-seats-in-one-gpu).

State what the model server may use, not what the card reports: subtract the driver reserve and
whatever your desktop already holds (~14.5 of a 16 GB card is the measured figure on the machine this
was calibrated against). Aegis never detects this — no GPU/VRAM introspection is attempted on any
platform.

If the model has not been loaded yet, its resident weights cannot be measured, so the budget is saved
but the window is not fitted. Run one turn, then:

```bash
aegis models --fit --write
```

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
  knowledge.db      Project knowledge base (aegis knowledge index)
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

# How each scanner would run right now, plus each database's age
aegis security status

# Prove every scanner in the built image can still run AND detect
aegis security verify-image
aegis security verify-image --netscanner   # needs network — that's what it verifies
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

**Rebuild the scanner images after a pull that touches them.** The Containerfile and its scripts are `go:embed`-ed, so a new binary can carry a newer build context than the image you have. Aegis records a **source fingerprint** at build time and reports the mismatch as drift rather than silently trusting the old image:

```bash
aegis security status                      # reports source drift, and DB ages
aegis security build-image                 # rebuild + re-verify + re-pin
aegis security build-image --netscanner    # both images share one build context,
                                           # so rebuilding one leaves the other drifted
```

`aegis config update` merges newly documented config fields (such as the `multiscanner`/`netscanner` blocks) into an existing config file without discarding your customizations; a backup is written first, and `--dry-run` shows what it would do.
