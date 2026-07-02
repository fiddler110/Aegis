# TUI Guide

The terminal UI is the primary way to use Aegis. It provides a live streaming view of the agent's work alongside controls for permissions, sessions, and configuration.

---

## Launching

```bash
aegis                        # build mode, general persona
aegis --mode plan            # read-only exploration
aegis --persona security     # security architect persona
aegis --resume <id>          # continue a previous session
```

The daemon starts automatically in the same process — no second terminal needed.

---

## Layout

```
⬡ AEGIS                                          abc12345  claude-opus-4-8
─────────────────────────────────────────────────────────────────────────
 SESSION       │  You
 abc12345      │  fix the timeout bug in the client
               │
 MODE          │  ✻ thinking
 build         │  The retry loop reuses the same context…
               │
 TOOLS         │  Assistant
 ✓ glob        │  I'll patch the client timeout handling.
 ✓ read_file   │
 ⚙ edit_file   │  ⚙ edit_file internal/client/client.go
               │  - http:  &http.Client{Timeout: 0},
 CONTEXT       │  + http:  &http.Client{Timeout: 30 * time.Second},
 ▰▰▰▱▱▱▱ 31%   │  ✓ edit_file → edited client.go (1 replacement)
 cache 78% hit │
               │
 COST          │
 $0.0123       │
 in  64210     │
 out 512       │
─────────────────────────────────────────────────────────────────────────
 ◐ thinking…   2s                          build   ctrl+k · f1 · ctrl+e
─────────────────────────────────────────────────────────────────────────
 │ Message Aegis…
```

### Left sidebar

| Section | What it shows |
|---------|--------------|
| **SESSION** | Current session ID |
| **MODE** | Active permission mode (plan / build / auto) |
| **TOOLS** | Tool calls in this run: `✓` done, `⚙` running, `✗` failed |
| **CONTEXT** | Context-window fill meter + prompt-cache hit rate (Anthropic) |
| **COST** | Cumulative USD spend, input tokens, output tokens |

### Transcript area (right)

- **`You`** — Your messages
- **`✻ thinking`** — Extended reasoning (dim; Anthropic Claude or local reasoning models)
- **`Assistant`** — Model responses
- **`⚙ tool_name args`** — Tool calls with inline diffs for file edits
- **`✓ tool_name → result`** — Tool completion line
- **`⚠ output guard: …`** — Output validation warning (dim)

Multi-line tool output is displayed in collapsible gutter blocks. File edits render as inline diffs with `+` / `-` lines.

### Status bar

Shows the current run state (`◐ thinking…`, `◐ running…`, elapsed time) and the active permission mode. Keyboard hint shortcuts are shown on the right.

---

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Ctrl+J` | Insert newline in the input field |
| `/` | Open slash-command completion popup |
| `@` | Open workspace file/reference completion |
| `Ctrl+K` | Open command palette |
| `Ctrl+E` | Edit the current input in `$EDITOR` |
| `Shift+Tab` | Cycle permission mode: plan → build → auto → plan |
| `Ctrl+R` | Open interactive session picker (switch / resume) |
| `Ctrl+T` | Show active sub-agents panel |
| `Ctrl+L` | Clear the transcript (history preserved in session) |
| `F1` | Toggle keyboard-shortcut help overlay |
| `↑` / `↓` | Navigate input history |
| `Ctrl+C` | Interrupt a streaming run; second press quits |
| Mouse wheel | Scroll conversation (auto-follow pauses while scrolled up) |

---

## Input Features

### `@` References

Type `@` to open the reference picker:

| Syntax | What it does |
|--------|-------------|
| `@path/to/file.go` | Attach file path — agent reads it with `read_file` |
| `@image:<path>` | Attach image (PNG/JPEG/GIF/WebP, max 5 MiB) to send to vision model |
| `@diagnostics` | Reference to LSP diagnostics for the current project |
| `@url:<address>` | Reference to a URL — agent fetches it with `web_fetch` |
| `@symbol:<name>` | Reference to a code symbol — agent locates it with search tools |

Paths can be absolute, `~`-relative, or relative to the workspace. File path completion is fuzzy-matched against a workspace index.

### Multi-line input

Press `Ctrl+J` to insert newlines. Press `Ctrl+E` to open the full input in `$EDITOR` (uses `$VISUAL` or `$EDITOR` environment variable, defaulting to `vi`).

### Input history

`↑` / `↓` navigates through previously sent messages in the current session.

---

## Slash Commands

Type `/` to open the command completion popup and browse available commands.

### Navigation & Sessions

| Command | Description |
|---------|-------------|
| `/help [cmd]` | Show all commands, or help for a specific command |
| `/session` | Show current session info (ID, mode, persona, token count) |
| `/session list` | List all sessions |
| `/persona [name]` | Switch persona interactively (without name) or directly |
| `/rewind` | List checkpoints (newest first, with file counts) |
| `/rewind <n> [code\|conversation\|both]` | Restore checkpoint n |
| `/rollback [n]` | Restore checkpoint n and run `git reset --hard` |
| `/timeline` | Jump to a past turn in the conversation |
| `/detach [on\|off]` | Run session in background (survives TUI close) |
| `/archive [off]` | Archive session (hidden from listings, data kept); `/archive off` to restore |

### Permission & Mode

| Command | Description |
|---------|-------------|
| `/mode <plan\|build\|auto>` | Switch permission mode for this session |
| `/guard [on\|off\|status]` | Toggle output validation; `status` shows current state |

### Configuration & Setup

| Command | Description |
|---------|-------------|
| `/config` | Open the interactive configuration wizard (5-step) |
| `/sandbox [use <target>]` | Show active sandbox backend and detected runtimes, or switch backend |

### Memory & Knowledge

| Command | Description |
|---------|-------------|
| `/memory` | Show all saved project and user memories |
| `/remember <text>` | Save a memory entry (prompts for scope: project or user) |
| `/skills` | List all saved skills (project + user) |
| `/commands` | List custom commands loaded from `.aegis/commands/` |

### Display & Session

| Command | Description |
|---------|-------------|
| `/clear` | Clear the transcript display (session history preserved) |
| `/models` | Show current model and provider |
| `/tools compact` | Set tool-output display to 10 lines max |
| `/tools full` | Show complete tool output (no line cap) |
| `/humor [on\|off]` | Toggle D&D-themed thinking phrases in the status bar |
| `/share [html\|md\|json]` | Export session to a shareable file in the current directory |

### Exit

| Command | Description |
|---------|-------------|
| `/quit` | Exit Aegis |
| `/exit` | Exit Aegis |

### Custom Commands

Any markdown files in `.aegis/commands/` or the user data `commands/` directory appear as additional slash commands. See [Extensibility](extensibility.md#custom-commands).

---

## The `/config` Wizard

The `/config` command opens a 5-step interactive wizard for changing provider and model settings without editing files or leaving the terminal:

**Step 1 — Provider**
Select from: Anthropic, Ollama, OpenAI, LM Studio, Groq, OpenRouter, Custom.

**Step 2 — Base URL**
Pre-filled for local servers. Leave empty for cloud providers using the default endpoint.

**Step 3 — Model**
For Ollama and LM Studio: shows discovered models on the running server. For cloud providers: curated list. Manual entry always available.

**Step 4 — Max tokens**
Response token cap.

**Step 5 — Thinking mode**
Auto / Enabled / Disabled. Controls Anthropic extended thinking and the local-model `think` flag (qwen3, deepseek-r1).

Changes are written to the global `config.yaml` and take effect on the next Aegis restart.

---

## The `/rewind` Command

Every user turn captures a checkpoint before the agent runs. `/rewind` lets you undo bad runs.

```
/rewind              list checkpoints
/rewind 3            restore checkpoint 3 (files + conversation)
/rewind 3 code       restore only files (leave transcript intact)
/rewind 3 conversation  restore only transcript (leave files as-is)
/rewind 3 both       restore both (default)
```

Checkpoints are listed newest-first with a label (your message text, truncated) and file count.

The `code` scope deletes files the turn created and reverts files it modified. After a code restore, file-staleness tracking is cleared so the agent re-reads reverted files before touching them again.

`/rollback [n]` is a more aggressive variant: it restores checkpoint n **and** runs `git reset --hard` to the git HEAD from before that turn. Use it when you want to undo both agent file changes and any git commits the agent made.

---

## Session Picker (`Ctrl+R`)

Press `Ctrl+R` to open the interactive session picker. It lists all sessions (newest first) with their title, mode, persona, and last-updated time. Select one to switch to it — the transcript is replayed including past diffs, tool output, and thinking blocks.

---

## Approval Dialogs

In `build` mode (and `plan` mode for network tools), the agent pauses before running shell commands and prompts for approval. The dialog shows:

- Tool name and capability
- The full input (command to run, file to write, URL to fetch)
- `[y]` approve / `[n]` deny / `[a]` approve all remaining calls in this run

Denied calls return an error to the agent, which can then plan an alternative.

---

## Extended Thinking Display

When extended thinking is enabled (Anthropic Claude or local reasoning models), thinking blocks appear as dim `✻ thinking` sections in the transcript. They are streamed live so you can watch the model reason in real time. Thinking blocks are preserved in session history for multi-step correctness.

---

## Output Guard

When the output guard fires on a final answer, the TUI shows a dim `⚠ output guard: <reason>` line below the answer. The agent is asked to revise and re-try (up to `max_retries` attempts). If all retries fail, the raw answer is shown with the warning visible.

Use `/guard status` to check the current guard state; `/guard off` disables it for the rest of the session.

---

## Context Window Meter

The sidebar shows a fill bar (`▰▰▰▱▱▱▱ 31%`) representing how full the model's context window is. At 85% fill, automatic context compaction kicks in and summarizes old turns. The `cache N% hit` line (Anthropic only) shows the prompt-cache hit rate — higher is better for both speed and cost.

---

## Sub-Agent Panel (`Ctrl+T`)

When the `agent` tool spawns sub-agents, press `Ctrl+T` to open a panel listing all active agents with their status (running, done, failed) and task description.
