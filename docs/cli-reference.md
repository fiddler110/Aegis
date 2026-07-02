# CLI Reference

All Aegis commands. Run `aegis <command> --help` for the most up-to-date flags.

---

## `aegis` (root)

Launches the terminal UI (TUI) with the daemon auto-starting in the same process.

```bash
aegis [flags]
```

**Flags:**

| Flag | Default | Description |
|------|---------|-------------|
| `--mode <plan\|build\|auto>` | `build` | Permission mode for the session |
| `--persona <name>` | `general` | Persona to use (see [Personas](personas.md)) |
| `--resume <session-id>` | — | Resume an existing session by ID |
| `--first-init` | — | Create global config with full provider template and exit |
| `--init` | — | Create `.aegis/config.yaml` project override and exit |

**Examples:**

```bash
aegis                                    # build mode, general persona
aegis --mode plan                        # read-only exploration
aegis --persona security                 # security architect persona
aegis --persona developer --mode build   # developer, with write/shell access
aegis --resume abc12345                  # continue a previous session
```

---

## `aegis serve`

Run the daemon as a standalone process. The daemon persists across TUI restarts, which is useful for long-running background jobs or keeping sessions alive while you reconnect.

```bash
aegis serve [flags]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--foreground` | Mirror daemon logs to stderr for live debugging |

```bash
aegis serve             # background-style: logs go to data directory only
aegis serve --foreground  # see logs in the terminal
```

When a separate daemon is already running, `aegis` (the TUI) detects it and connects without starting a second one.

---

## `aegis chat`

One-shot chat: send a single prompt, stream the response to stdout, and exit. No TUI.

```bash
aegis chat [prompt] [flags]
aegis chat                     # reads prompt from stdin
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--system <text>` | Override the system prompt |
| `--mode <plan\|build\|auto>` | Permission mode |
| `--persona <name>` | Persona name |
| `--yes` | Auto-approve all tool calls (unattended use) |

**Examples:**

```bash
# Simple query
aegis chat "summarise main.go" --mode plan

# Scripted refactor — auto-approve tool calls
aegis chat "refactor the config package" --mode build --yes

# Pipe from stdin
echo "what security issues exist in this repo?" | aegis chat --mode plan

# Use with a specific persona
aegis chat "review this PR" --persona security-architect --mode plan
```

---

## `aegis acp`

Speak the [Agent Client Protocol](https://agentclientprotocol.com) (ACP) — editor integration for Zed, Neovim (via `codecompanion`/`avante`), and other ACP clients. Runs JSON-RPC over stdio.

```bash
aegis acp [flags]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--mode <plan\|build>` | Permission mode for sessions |

Reuses a running daemon if one is up, or starts an embedded one. Protocol frames use stdout exclusively; logs go to the log file. Image content blocks in a prompt are forwarded to the model.

**Zed example** (`settings.json`):
```json
{
  "agent_servers": [
    {
      "command": "aegis",
      "args": ["acp"]
    }
  ]
}
```

---

## `aegis ui`

Start a browser-based UI over the daemon API.

```bash
aegis ui [flags]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--no-open` | Print the URL instead of opening a browser |

```bash
aegis ui             # opens http://127.0.0.1:4127/ui in your browser
aegis ui --no-open   # just print the URL
```

The UI is a single self-contained page embedded in the binary. It lets you list/create sessions, view transcripts with collapsible thinking and tool sections, send messages with live SSE streaming, and approve tool calls inline.

---

## `aegis parallel`

Run several prompts as concurrent, independent sessions.

```bash
aegis parallel "prompt A" "prompt B" ... [flags]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--mode <plan\|build\|auto>` | Permission mode for all sessions |
| `--yes` | Auto-approve tool calls in all sessions (required for unattended use) |

```bash
aegis parallel "fix the failing tests" "update the README" --yes
aegis parallel "security review" "dependency audit" --mode plan
```

Progress is interleaved in the terminal, with per-session summaries and resume hints (`aegis --resume <id>`) at the end. These are independent user-launched sessions, not sub-agents.

---

## `aegis runs`

List runs currently in flight across all sessions.

```bash
aegis runs
```

---

## `aegis sessions`

Manage stored sessions.

### `aegis sessions list`

```bash
aegis sessions list [--archived]
```

List all sessions. Add `--archived` to include archived (soft-deleted) sessions.

### `aegis sessions export`

```bash
aegis sessions export <id> --format <html|md|json> [--out <file>]
```

Export a session as a shareable transcript. Default format is `html` (self-contained file with collapsible sections). `md` suits GitHub/PR pastes; `json` is raw session data.

```bash
aegis sessions export abc12345 --format html --out review.html
aegis sessions export abc12345 --format md
```

### `aegis sessions trace`

```bash
aegis sessions trace <id>
```

Print the per-turn trace: turn index, model, input/output/cache tokens, per-turn cost, tool calls with durations, and wall time. Shows session totals at the end. Useful for auditing or profiling a run.

### `aegis sessions delete`

```bash
aegis sessions delete <id>
```

Permanently delete a session and all its data (checkpoints, traces).

### `aegis sessions archive`

```bash
aegis sessions archive <id>
```

Soft-delete: hide from listings while keeping data. Reverse with `aegis sessions unarchive <id>`.

### `aegis sessions unarchive`

```bash
aegis sessions unarchive <id>
```

Restore an archived session.

### `aegis sessions prune`

```bash
aegis sessions prune
```

Auto-delete non-archived sessions older than the configured TTL (`cleanup.session_ttl_days` in config). Runs on a schedule when the daemon is running; this command triggers it manually.

---

## `aegis dry-run`

Preview what Aegis would do — resolved config, tools, memory, and context — without making any model call.

```bash
aegis dry-run
```

Useful for verifying config, checking which memory entries are loaded, confirming tool availability, and troubleshooting.

---

## `aegis scan`

Run available security scanners against a path.

```bash
aegis scan [path]
```

Default path is the current directory. Runs **semgrep**, **trivy**, and **gitleaks** (whichever are installed) and produces a normalized findings report with severity, location, rule ID, and remediation hint.

```bash
aegis scan ./src
aegis scan .
```

---

## `aegis sandbox`

Inspect and configure the shell execution sandbox.

### `aegis sandbox detect`

```bash
aegis sandbox detect
```

Probe for available container runtimes (Docker, Podman, WSL containers, Apple Containers). Shows a table of what is available and which would be chosen by `auto`.

### `aegis sandbox use`

```bash
aegis sandbox use <target>
```

Set the active sandbox backend. `<target>` is one of: `local`, `auto`, `docker`, `podman`, `wslc`, `container`. Writes to config.

```bash
aegis sandbox use auto      # auto-detect best runtime
aegis sandbox use docker
aegis sandbox use local     # back to no sandboxing
```

### `aegis sandbox test`

```bash
aegis sandbox test
```

Run `uname -a` in the configured sandbox to verify it works.

---

## `aegis worktree`

Manage git worktrees for isolated parallel work.

```bash
aegis worktree add <name> [--branch <branch>]
aegis worktree list
aegis worktree remove <name>
aegis worktree prune
```

Worktrees are created under `.aegis/worktrees/`. Run `aegis` from inside a worktree to scope the session to that checkout. Multiple agents in separate worktrees can't conflict on file edits.

```bash
aegis worktree add feature-x --branch feature-x
aegis worktree list
aegis worktree remove feature-x
aegis worktree prune    # clean up stale worktrees
```

---

## `aegis bundle`

Install a bundle of commands, agents, and skills.

```bash
aegis bundle info <path>
aegis bundle install <path> [--scope <project|user>] [--overwrite]
```

A bundle is a directory with a `bundle.yaml` manifest plus `commands/`, `agents/`, and/or `skills/` subdirectories. Default scope is `project` (installs to `.aegis/`).

```bash
aegis bundle info ./my-bundle           # preview what would be installed
aegis bundle install ./my-bundle        # install to .aegis/
aegis bundle install ./my-bundle --scope user  # install to user data dir
```

---

## `aegis models`

Show the curated model catalog and optionally probe for running local servers.

```bash
aegis models [--local]
```

| Flag | Description |
|------|-------------|
| `--local` | Probe localhost for Ollama, LM Studio, LiteLLM |

Without `--local`: prints a curated list of recommended models by tier (frontier / balanced / local) with context windows and notes.

With `--local`: additionally probes `localhost:11434`, `localhost:1234`, `localhost:4000` and lists every available model found.

---

## `aegis config`

Show the fully resolved configuration (after applying all layers).

```bash
aegis config
```

---

## `aegis index`

Build a repository map — top-level symbols per source file.

```bash
aegis index [--print] [--max-bytes <n>]
```

| Flag | Default | Description |
|------|---------|-------------|
| `--print` | — | Print the map to stdout |
| `--max-bytes <n>` | ~2000 tokens | Token budget for the injected map |

The map is cached at `.aegis/repomap.json`. When the cache exists, the daemon injects it into the system prompt. The cache stores a content fingerprint (path + size + mtime) and is automatically rebuilt when sources change.

Languages supported: Go, Python, JavaScript/TypeScript, Rust, Ruby, Java.

```bash
aegis index           # build/rebuild the map
aegis index --print   # inspect the map
```

---

## `aegis diagram`

Render a diagram from stdin.

```bash
aegis diagram --type <type> --out <file>
```

```bash
aegis diagram --type mermaid --out architecture.svg < diagram.mmd
aegis diagram --type plantuml --out sequence.png < sequence.puml
```

Supported types: `mermaid`, `plantuml`, `c4`, `graphviz`, `drawio`, and others supported by Kroki. Falls back to local CLI tools if Kroki is unavailable.

---

## `aegis bg`

Background session management.

```bash
aegis bg events <session-id>   # check background session progress
```

---

## `aegis worker`

Internal background worker process. Not intended for direct use.

---

## Global Flags

These flags are available on most commands:

| Flag | Description |
|------|-------------|
| `--help`, `-h` | Show help for the command |

---

## Exit Codes

| Code | Meaning |
|------|---------|
| `0` | Success |
| `1` | General error |
| `2` | Usage error (bad flags) |
