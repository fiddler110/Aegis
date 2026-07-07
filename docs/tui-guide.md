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

The sidebar is **off by default**. Toggle it with `Ctrl+B` or `/sidebar`. When hidden, glanceable stats (context %, cost, agent count) fold into the status bar.

**With sidebar open:**

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
 ◐ thinking…   2s              31% $0.01   build   ctrl+k · f1 · ctrl+e
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

Toggle with `Ctrl+B` or `/sidebar`. When hidden, context % and cost move to the status bar.

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
| `Shift+Enter` | Insert newline in the input field (`Ctrl+J` fallback for terminals that can't send Shift+Enter) |
| `Alt+Enter` | While streaming: queue the current draft as the next message instead of sending immediately |
| `Esc` (×2) | Interrupt the streaming run — first press arms it, second press (quickly after) confirms and cancels; also discards any queued message |
| `Ctrl+C` | Cancel the current run, or quit if nothing is running |
| `Ctrl+O` | Expand/collapse a collapsed thinking block |
| `/` | Open slash-command completion popup |
| `@` | Open workspace file/reference completion |
| `Ctrl+K` | Open command palette |
| `Ctrl+B` | Toggle sidebar on/off |
| `Ctrl+X` | Toggle the embedded terminal pane |
| `Ctrl+E` | Edit the current input in `$EDITOR` |
| `Shift+Tab` | Cycle permission mode: plan → build → auto → plan |
| `Ctrl+R` | Open interactive session picker (switch / resume) |
| `Ctrl+T` | Show active sub-agents panel |
| `Ctrl+L` | Clear the transcript (history preserved in session) |
| `F1` | Toggle keyboard-shortcut help overlay |
| `↑` / `↓` | Navigate input history (moves the cursor within a multiline draft first; history nav only at the first/last line) |
| Mouse wheel | Scroll conversation (auto-follow pauses while scrolled up) |

---

## Input Features

### `@` References

Type `@` to open the reference picker:

| Syntax | What it does |
|--------|-------------|
| `@path/to/file.go` | Attach file path — agent reads it with `read_file` |
| `@path/to/file.go#L10-40` | Attach a specific line range — only lines 10–40 are injected |
| `@image:<path>` | Attach image (PNG/JPEG/GIF/WebP, max 5 MiB) to send to vision model |
| `@diagnostics` | Reference to LSP diagnostics for the current project |
| `@url:<address>` | Reference to a URL — agent fetches it with `web_fetch` |
| `@symbol:<name>` | Reference to a code symbol — agent locates it with search tools |

Paths can be absolute, `~`-relative, or relative to the workspace. File path completion is fuzzy-matched against a workspace index. Line-range references (`#L10-40` or `#10-40`) are expanded by the daemon before the engine call — the model sees only the specified lines.

### Multi-line input

Press `Shift+Enter` (or `Ctrl+J` as a fallback on terminals that don't disambiguate Shift+Enter from Enter) to insert newlines. Press `Ctrl+E` to open the full input in `$EDITOR` (uses `$VISUAL` or `$EDITOR` environment variable, defaulting to `vi`).

### Input history

`↑` / `↓` navigates through previously sent messages in the current session. Inside a multiline draft, `↑`/`↓` first move the cursor between lines; history navigation only kicks in when the cursor is already on the first (↑) or last (↓) line.

### Pasted image paths

Pasting or typing a bare image file path (PNG/JPEG/GIF/WebP, quoted or unquoted) into the input is detected automatically and converted into an `@image:` attachment token — you don't have to type the `@image:` prefix yourself.

### Message queueing

Pressing `Alt+Enter` while a run is streaming queues the current draft instead of sending it immediately. Queued messages show as a dimmed `⏳ queued ▸` block and are sent automatically, one per completed run, once the stream closes. An explicit interrupt (`Esc` ×2 or `Ctrl+C`) or a stream error discards the queue instead of auto-sending it — the assumption is that if you stopped the run, you don't want the next queued message firing on its own.

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
| `/archive [off\|list]` | Archive session (hidden from listings, data kept); `/archive off` to restore; `/archive list` to list archived sessions |
| `/prune [days]` | Delete non-archived sessions older than N days (server TTL if omitted) |
| `/runs` | List message runs currently in flight across all sessions |
| `/bg [list\|events [session-id]]` | List background (detached) sessions, or print one's buffered events |

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
| `/knowledge index` | Rebuild the project knowledge base FTS5 index |
| `/knowledge query <text>` | Search the project knowledge base |
| `/index` | Rebuild the repository map (top-level symbols per file) in the system prompt |
| `/skills` | List all saved skills (project + user) |
| `/commands` | List custom commands loaded from `.aegis/commands/` |
| `/bundle info <path-or-url>` | Show a bundle's manifest, artifacts, and content hash |
| `/bundle install <path-or-url> [global] [sha256:<hash>] [confirm]` | Preview or install a bundle's commands/agents/skills; `global` targets the user data dir, `sha256:<hash>` pins provenance, `confirm` actually writes |

### Security & Analysis

| Command | Description |
|---------|-------------|
| `/scan [path]` | Scan the workspace (or a subpath) with every enabled scanner, auto-detecting the project language; no model turn spent |
| `/scan <scanner-or-category>[,<...>] [path]` | Run only the named scanner(s)/category (e.g. `/scan trufflehog`, `/scan secrets`), force-enabled regardless of config |
| `/scan list` | List every valid scanner name and category alias, with live availability and default-enabled status |
| `/scan image <ref>` | Scan a container image reference for vulnerabilities and best-practice violations |
| `/scan sbom [path]` | Generate a CycloneDX SBOM instead of a findings report |
| `/scan network <target> [target...]` | Run nmap + nuclei against a bare host/IP/CIDR list (attack-surface mapping) |
| `/security [status]` | Show how each scanner would run right now (host binary, container fallback, or unavailable and why) |
| `/security install <tool> [confirm]` | Show (or, with `confirm`, run) the guided host-install command for a scanner |
| `/security baseline [path]` | Show the accepted-risk suppression baseline and each entry's active/expired/invalid status |
| `/security config [global]` | Open the interactive scanner configuration dialog (same as `/security-config`) |
| `/debate <claim>` | Adversarially debate any claim (security finding, design assertion, plan) — propose/critique/rebut/arbitrate, ending in an UPHOLD/REVISE/REJECT verdict |
| `/threat-model [system or feature]` | Threat-model the whole project, or a named system/feature, using STRIDE/LINDDUN/PASTA/Trike/VAST/NIST 800-154 |

### Display & Session

| Command | Description |
|---------|-------------|
| `/clear` | Clear the transcript display (session history preserved) |
| `/models` | Show current model and provider |
| `/model <model-id>` | Switch this session's model mid-session (persisted as a per-session override; must belong to the currently configured provider) |
| `/model default` | Clear the session model override, reverting to the persona/global default |
| `/status` | Show daemon health, sandbox backend, and cost caps/spend (session + cross-session daily) |
| `/tools compact` | Set tool-output display to 10 lines max |
| `/tools full` | Show complete tool output (no line cap) |
| `/sidebar` | Toggle the left sidebar on/off (also `Ctrl+B`) |
| `/theme [name]` | Switch the color scheme live (no restart); built in: dark, light, catppuccin, dracula, gruvbox, tokyonight — or a custom `<name>.json` in `.aegis/themes/`/`~/.aegis/themes/`; no args shows the current theme |
| `/copy` | Copy the last assistant message to clipboard |
| `/copy <n>` | Copy the Nth fenced code block from the last response |
| `/tasks` | Show the live task/todo progress strip (also visible above input) |
| `/humor [on\|off]` | Toggle D&D-themed flavor phrases in the status bar |
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

In `build` mode (and `plan` mode for network tools), the agent pauses before running shell/execute or write calls and shows an option-list approval dialog with a preview of the pending change (unified diff for file edits, the literal command for shell calls):

- **Allow once** (`y`) — approve just this call
- **Allow always for `<pattern>`** (`a`) — approve this call and derive a scoped permission rule (e.g. `allow bash(npm test*)` from a shell command, or a directory glob from a file path) that's persisted to `.aegis/config.yaml → permission.rules`, so the same class of call is auto-approved for the rest of this project going forward
- **Deny** (`n`) — deny this call; the agent gets an error and can plan an alternative
- **Deny with feedback…** (`f`) — deny and type a reason, which is passed back to the model as the tool error instead of a generic denial, steering its next attempt

Navigate options with `↑`/`↓` or `Tab`/`Shift+Tab`, confirm with `Enter`. The transcript behind the dialog is still scrollable so you can review context before deciding.

---

## Live Task Progress Strip

When the model uses the `todo_add`, `todo_update`, or `todo_list` tools, a compact progress strip appears above the input area:

```
▣▣▶▢▢  2/5  refactor session store
```

- `▣` done  `▶` in progress  `▢` pending
- Shows the current in-progress task text
- Expand the full list with `/tasks`

The strip disappears when all tasks are done and reappears as new tasks are added.

---

## Draft Stash

Unsent input is automatically saved to `.aegis/stash.json` when you quit Aegis. The next time you start the same session, the draft is restored into the input field. This works per-session — drafts in different sessions don't interfere.

---

## Extended Thinking Display

When extended thinking is enabled (Anthropic Claude or local reasoning models), thinking blocks stream live as dim `✻ thinking` sections so you can watch the model reason in real time. Once the block settles, it collapses to a one-line `✻ thought for Ns` summary; press `Ctrl+O` to expand it back to the full text. Thinking blocks are preserved in session history for multi-step correctness.

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

---

## Terminal Pane (`Ctrl+X`)

Press `Ctrl+X` to open a scrollable shell pane docked to the right of the transcript. It runs commands directly (via the same local sandbox backend used for shell execution) independent of the agent — useful for poking around the workspace without spending a turn. `Enter` runs the typed command, `↑`/`↓` navigates its own command history, and `Ctrl+C` interrupts a running command in the pane. `Esc` (or `Ctrl+X` again) closes it and returns focus to the main input.
