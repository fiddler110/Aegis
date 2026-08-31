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
| `--overwrite` | — | With `--init`/`--first-init`, regenerate an existing config from the latest template (backs up the old file first) instead of aborting |

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
| `--output-format <text\|json\|stream-json>` | `text` streams to stdout (default); `json` emits one final result object; `stream-json` emits one JSON event per line plus a trailing result |
| `--render <auto\|on\|off>` | Markdown rendering of the `text` format (default `auto`: on when stdout is a terminal, off when piped). Headings, tables, lists and fenced code are rendered through glamour instead of arriving as one undifferentiated block of text, and a tool call shows indented, scalar-clipped argument JSON instead of one unbroken line. `on` forces it (e.g. for `\| less -R`); `off` gives the raw byte stream. Honors `NO_COLOR` and `GLAMOUR_STYLE` |
| `--skill <name>` | Preload the named skill's full instructions into the prompt (a small local model never has to discover/fetch it via the `skill` tool) and drive the run to completion — after each turn, if any file under `.aegis/` still carries a `<!-- PENDING -->` marker, chat auto-continues instead of stopping at the model's first yield. This is what lets a long multi-phase skill (`threat-modeling`, `deep-research`) finish non-interactively |
| `--max-turns <n>` | With `--skill`, the maximum number of drive-to-completion turns before stopping with a resumable partial result (default `40`) |

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

# Drive a full threat model to completion, non-interactively
aegis chat "threat model this repo" --skill threat-modeling --mode build --yes
```

**Headless skill drives (`--skill`):** built for multi-phase skills whose output is a set of files rather than a single chat reply — `threat-modeling` (scaffolds seven report files, fills them phase by phase, then runs the skill's own `verify.py`/`lint_dfd.py`/`inventory.py --check` checks and feeds any failure back for an in-place fix, bounded by `--max-turns`) and `deep-research` are the shipped examples. Requires `--mode build --yes` (or `auto`) since the drive writes files across many turns with nobody there to approve each one. If the run ends with no `<!-- PENDING -->` markers left but also no files under `.aegis/`, chat prints an explicit warning rather than reporting quiet success — a model can narrate a whole build in its reasoning trace without calling a single tool. Re-running the same command resumes an interrupted or partial drive (the PENDING markers show exactly where it left off).

**The same drive runs in the daemon now**, so it is no longer CLI-only: `/drive <skill> <task…>` (or `/threat-model … unattended`) in the TUI, the **Drive** button beside the web UI composer, and `POST /sessions/{id}/drive` over the HTTP API all run the identical phase machinery. The web UI is the right home for a multi-hour build — its runs are resumable, so they survive closing the tab, which `aegis chat` does not. Any skill can opt in by declaring a `phases:` plan in its own frontmatter (see [skills.md](skills.md#phased-skills-long-unattended-builds)).

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

**Authentication (optional, FIND-02/P24.2):** by default any process able to write to this subprocess's stdin can drive sessions through it — the same trust assumption as most stdio ACP/MCP integrations. Set `AEGIS_ACP_TOKEN` in the environment the editor uses to launch `aegis acp` to require a matching credential: `initialize` then advertises a `shared_secret` auth method, and `session/new`/`session/prompt` are denied with an `authentication required` error until the client calls `authenticate` with `{"methodId": "shared_secret", "token": "<the same value>"}`. Leave it unset (the default) for unchanged, no-auth behavior.

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

## `aegis mcp-serve`

Expose this Aegis daemon as a [Model Context Protocol](https://modelcontextprotocol.io) (MCP) server — the reverse direction of the `mcp:` client config (which lets Aegis call *out* to external MCP servers). Runs JSON-RPC over stdio, so any MCP-speaking harness (Claude Code, Codex, an editor) can launch this as a subprocess and drive Aegis as a tool.

```bash
aegis mcp-serve [flags]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--mode <plan\|build\|auto>` | Default permission mode for new sessions (default: `plan`, or `mcp_server.default_mode` from config) |
| `--auto-approve` | Auto-approve tool calls that would otherwise need interactive approval |

Reuses a running daemon if one is up, or starts an embedded one. Exposes three tools:

| Tool | Purpose |
|------|---------|
| `aegis_prompt` | Delegate a task to an Aegis session and wait for it to finish; returns the final text reply with a trailing `[session: <id>]` marker so the caller can continue the conversation by passing that id back as `session_id`. |
| `aegis_new_session` | Create a session without sending it a message yet. |
| `aegis_list_sessions` | List existing sessions (id, title, mode, last updated). |

New sessions default to **plan mode** (read-only) — a materially lower-trust posture than the local TUI/CLI's own default, since the caller is an external harness. A caller may choose a mode per call with the `mode` tool argument, but only one at or **below** `mcp_server.default_mode`: an MCP client is a program holding a token read from a file, not the local user, so it cannot escalate itself past the configured posture. An attempt to is clamped to the default and logged; `mcp_server.allow_caller_mode_escalation: true` restores the old caller-wins behavior.

A tool call needing approval in a higher mode (`build`/`auto`) is **denied** by default, since an MCP `tools/call` is a synchronous request/response with no human available to ask; pass `--auto-approve` (or set `mcp_server.auto_approve: true`) to allow it instead, optionally narrowed to named tools with `mcp_server.auto_approve_tools`. Every auto-granted approval is logged with the tool and its arguments — with no human in the loop, that log is the only record the call happened. See [Configuration Reference](configuration.md#full-config-reference) (`mcp_server:` block) for the config keys.

**v1 scope, deliberately:** individual built-in tools (`security_scan`, `read_file`, etc.) are not exposed 1:1 as MCP tools bypassing the agent loop — every MCP tool call goes through a real Aegis session. `notifications/cancelled` is not propagated to an in-flight `aegis_prompt` call. Both are documented follow-ups, not oversights.

**Authentication (always on, FIND-02/P24.2, hardened by FIND-06/P27.4):** `aegis mcp-serve` never runs unauthenticated. `tools/call` is denied with an `authentication required` error (JSON-RPC code `-32001`) until the client sends the custom `aegis/authenticate` request with `{"token": "<the token>"}` (`initialize`/`ping`/`tools/list` stay reachable unauthenticated, since they expose no session-driving capability). The token is `AEGIS_MCP_TOKEN` when the launching harness sets it; otherwise the command generates one and writes it to `<data_dir>/mcp.token` (mode 0600, owner-only ACL) for the harness to read after spawning the process. Setting `AEGIS_MCP_TOKEN` in the harness's environment is the simpler path when it can. This is an Aegis-specific extension, not part of the base MCP spec — a host unaware of it will fail at its first `tools/call`, which is the intended posture: without it, any local process able to write to this subprocess's stdin could drive full agent turns.

**Claude Code example** (`.mcp.json`):
```json
{
  "mcpServers": {
    "aegis": {
      "command": "aegis",
      "args": ["mcp-serve"]
    }
  }
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

The UI is a single self-contained page embedded in the binary. It lets you list/create sessions, view transcripts with collapsible thinking and tool sections, send messages with live SSE streaming, and approve tool calls inline. A status pill in the top bar shows what the agent is doing and how long it's been running (`Thinking… 12s`, `Running security_scan…`, `Waiting for your approval`) so a slow model turn never looks like a dead page; the Send button becomes a **Stop** button while a turn is in flight, cancelling it instead of just sitting there disabled.

**Scope:** the web UI has full TUI-parity scope (P15) — sessions, streaming, inline approvals, persona/mode switching, cost/token display, checkpoints/rewind, security scanning, skills, and memory management, all as panels on the same page. See [tui-guide.md](tui-guide.md) for the equivalent TUI controls, and the CLI subcommands documented elsewhere on this page for scriptable/headless use.

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

## `aegis compare`

Blind side-by-side comparison of two models on the same prompt (P20.2). Structurally the mirror image of `aegis parallel`: parallel fans the *same* model out over *different* prompts, compare fans *different* models out over the *same* prompt — each in its own persisted daemon session (created, then PATCHed to the requested model via the P14.7 per-session model override).

```bash
aegis compare <model-A> <model-B> [prompt] [flags]
```

**Flags:**

| Flag | Description |
|------|-------------|
| `--mode <plan\|build\|auto>` | Permission mode for both sessions |
| `--yes` | Auto-approve tool calls in both sessions (required for unattended use) |
| `--synthesize` | After voting, ask a model to synthesize the best of both revealed responses (default off) |
| `--synth-model <id>` | Model to use for `--synthesize`; defaults to the configured `provider.model` — a third model, not either compared one |

```bash
aegis compare claude-sonnet-4-6 gpt-5 "explain the CAP theorem"
echo "explain the CAP theorem" | aegis compare claude-sonnet-4-6 gpt-5
aegis compare claude-opus-4-8 qwen3 "review this diff" --synthesize
```

Both responses stream to completion labeled only "Response 1" / "Response 2" — in a randomized order, so which model landed in which slot isn't a positional tell — with which model produced which answer withheld until you vote. You're then prompted `Vote — which response is better? [1/2/tie/skip]:`; after voting, a reveal prints which model was actually Response 1 vs Response 2. With `--synthesize`, one further call (to `--synth-model`, or `provider.model` by default) combines the two revealed answers into a single, clearly labeled synthesis — not a third blind response. Both sessions persist and can be resumed with `aegis --resume <id>`, matching `aegis parallel`'s convention of never auto-deleting.

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

Print the per-turn trace: turn index, model, input/output/cache tokens, per-turn cost, tool calls with durations, wall time, and a `WHY` column carrying the stop reason, compaction event, guard verdict, correctives and calibration sample count for that turn (P66.11). Shows session totals at the end, followed by the full text of any compaction summary and any failing tool call's error body (P68.1). Useful for auditing or profiling a run, and for answering why a run took the number of turns it did.

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

## `aegis persona`

List, inspect, and scaffold personas. See [Personas](personas.md).

### `aegis persona list`

List all personas (built-in and custom); markers show `[custom]` for file-loaded personas and `[default]` for the currently configured default persona.

```bash
aegis persona list
```

### `aegis persona show`

Show a persona's full profile: description, source (built-in or the file path to edit), model, mode, tools, rules, guard, and system prompt.

```bash
aegis persona show security
aegis persona show my-helper --full   # print the entire system prompt
```

### `aegis persona new`

Scaffold a custom persona `.md` file with a commented frontmatter template. Defaults to the project directory (`.aegis/personas/`); `--global` writes to the user personas directory instead. The daemon picks up the new persona without a restart.

```bash
aegis persona new incident-responder
aegis persona new triage --description "Bug triage lead" --global
```

| Flag | Description |
|------|-------------|
| `--global` | Create in the user-global personas directory instead of the project |
| `--description` | One-line description shown in persona listings |

### `aegis persona use`

Set the persona new sessions start with when `--persona` isn't passed. Writes `default_persona` to the project config (`.aegis/config.yaml`) by default so it travels with the repo; `--global` writes to the user config instead. An explicit `--persona` flag on `aegis` always overrides this.

```bash
aegis persona use developer
aegis persona use security --global
```

| Flag | Description |
|------|-------------|
| `--global` | Write to the user-global config instead of the project |

---

## `aegis skills`

List and toggle the skills built into Aegis (progressive-disclosure playbooks embedded in the binary — code review, security audit, diagramming, debugging, threat modeling, etc.). They stay dormant, costing zero system-prompt tokens, until enabled by name. Project (`.aegis/skills`) and user (`~/.aegis/skills`) skill files are separate and always active regardless of this list.

### `aegis skills list`

```bash
aegis skills list
```

Lists every built-in skill and whether it's currently enabled.

### `aegis skills enable` / `aegis skills disable`

```bash
aegis skills enable security-audit
aegis skills enable threat-modeling --global
aegis skills disable html-report
```

| Flag | Description |
|------|-------------|
| `--global` | Write to the user-global config instead of the project |

Restart Aegis (or the daemon) to apply.

---

## `aegis dry-run`

Preview what Aegis would do — resolved config, tools, memory, and context — without making any model call.

```bash
aegis dry-run
```

Useful for verifying config, checking which memory entries are loaded, confirming tool availability, and troubleshooting.

---

## `aegis doctor`

Preflight self-diagnostic: provider reachability and model availability, tool-calling smoke test (local Ollama-style providers only), configured-vs-actually-active sandbox backend, security scanner availability, output-guard/thinking-model pairing, session workdir allowlist posture, workspace-trust freeze state, and (if one is running) whether the daemon is reachable and in sync with the config on disk.

```bash
aegis doctor
```

Every check but the daemon-reachability one works standalone, with no daemon required — safe to run before `aegis serve`. Each row is `PASS`/`WARN`/`FAIL`; a `FAIL` row prints a `-> ` fix hint and makes the command exit non-zero, so it can gate scripts. A `WARN` never fails the command — it flags something worth a look (no sandbox isolation, an unpulled model, scanners enabled but unavailable) without blocking.

```bash
aegis doctor   # run before `aegis serve`, or any time something feels misconfigured
```

---

## `aegis trust`

Review and accept (or revoke) a project's `.aegis/config.yaml` security-relevant settings (P27.1). A cloned repository's project config is auto-merged with no confirmation by default, but security-relevant keys (`permission.*`, `sandbox.*`, `mcp.servers`, `notify.webhook`, `hooks`, `workspace.additional_roots`) stay frozen to their user/global values until the directory is explicitly trusted here — so checking out a repo can't silently widen its own permissions, add an attacker-controlled MCP server, run lifecycle hooks, or hand itself a window into the rest of your filesystem.

```bash
aegis trust [--yes] [--revoke] [--status] [--dir PATH]
```

| Flag | Description |
|------|-------------|
| `--yes` | Skip the confirmation prompt (non-interactive/scripted use) |
| `--revoke` | Remove trust for this directory instead of granting it |
| `--status` | Show what's currently frozen without prompting or changing anything |
| `--dir PATH` | Record the decision for `PATH` instead of the current directory — how a [`workspace.additional_roots`](configuration.md#full-config-reference) entry is authorized (P52.13) |

```bash
aegis trust --status   # see what the project config would change, no changes made
aegis trust            # review the diff and accept it interactively
aegis trust --revoke   # freeze this directory's project config again

aegis trust --dir ../research-repo   # authorize an additional workspace root
```

`--dir` exists because an additional root is usually a plain directory with no `.aegis/config.yaml` of its own to review, and the no-flag path short-circuits when there is nothing to freeze. What it grants is different too: not "apply this project's config" but "let a session that lists this directory under `workspace.additional_roots` reach into it at all". An additional root never inherits the primary workspace's trust.

A grant is recorded against a fingerprint of that directory's security-relevant config, not just against its path (P66.25/SEC-07). If those settings change later — a `git pull` that adds a `hooks:` block, flips `security.*`, or introduces a `commands:` override — the grant goes **stale**: the workspace is frozen again and `aegis trust` says so, showing the current diff so you can re-accept. Editing keys a project may set without trust (`log_level`, `provider.model`, `cost`, …), or editing your own user-global config, does not go stale. Grants recorded before this behavior existed carry no fingerprint and go stale once, on purpose.

`.aegis/.env` is **not** covered by that fingerprint: trust is resolved before any project-controlled file is read, so a later `.env` edit in an already-trusted workspace never re-prompts. See [Project Config and Workspace Trust](configuration.md#project-config-and-workspace-trust) for why that is the smaller hole and what the residual risk is; `aegis trust --revoke` is the mitigation.

`aegis doctor`'s "workspace trust" check surfaces the same frozen-settings (and stale-grant) state. Restart the daemon after trusting a directory to apply the newly-unfrozen settings.

---

## `aegis scan`

Run available security scanners against a path.

```bash
aegis scan [path] [flags]
```

Default path is the current directory. Runs every enabled scanner (**opengrep**, **trivy**, **gitleaks**, **kubescape**, **hadolint**, **osv-scanner**, **grype**, whichever are installed or container-fallback-able) and produces a normalized findings report with severity, location, rule ID, and remediation hint, persisted to `scan.json` in the data directory, outside the scanned repository. The language-targeted engines (**gosec**/**bandit**/**brakeman**/**njsscan**) are opt-in — a plain scan auto-detects the project's language (`go.mod`/`*.go`, `requirements.txt`/`*.py`, `Gemfile`/`*.rb`, `package.json`/`*.js`, and more) and auto-enables the matching one for this run only, without touching config, so a Rust or Java repo never triggers bandit. **hadolint**/**kubescape** are likewise skipped, with a reason, when the path has no Dockerfile/Kubernetes manifest.

At a real terminal, a plain `aegis scan` (no `--scanner`, no `--yes`) previews that auto-detected plan and asks for confirmation before running anything; `--yes` (or a non-interactive stdin, e.g. CI) skips the prompt and runs immediately.

```bash
aegis scan ./src
aegis scan .
aegis scan --yes                         # skip the confirmation prompt, run the auto-detected plan
aegis scan --scanner trufflehog          # run only trufflehog, force-enabled for this run, no prompt
aegis scan --scanner secrets ./src       # category alias: every scanner tagged "secrets"
aegis scan --list                        # every valid --scanner name/category, with live availability
```

| Flag | Description |
|------|-------------|
| `--scanner`, `-s` | Run only this scanner or category (repeatable), force-enabled regardless of config or relevance — see `--list` for valid names |
| `--list` | List every scanner name and category alias usable with `--scanner`, with live availability, then exit |
| `--yes`, `-y` | Skip the interactive scanner-plan confirmation and run the auto-detected set immediately (for scripts/CI) |

Full scanner reference, category aliases, and details on the container/WSL fallback and dedup/ASVS/baseline pipeline: [docs/security_scan.md](security_scan.md).

### `aegis scan image`

```bash
aegis scan image <ref>
```

Runs image-oriented scanners (trivy image, grype, dockle) against a container image reference (e.g. `alpine:3.20`) and prints a unified findings report, persisted to `image.json` in the data directory. Host-binary only — an image scanner that would otherwise run via a container is reported skipped, since scanner containers run network-isolated and can't pull the target image.

### `aegis scan sbom`

```bash
aegis scan sbom [path] [-o output]
```

Generates a CycloneDX JSON SBOM via syft over the given path (default: current directory), written to `.aegis/sbom.cdx.json` by default (or `-o`/`--output`).

### `aegis scan dast`

```bash
aegis scan dast <target-url> [--mode baseline|active|api] [--api-definition <path-or-url>]
```

Crawls (and, in `--mode active`/`api`, actively attacks) a *running* application via OWASP ZAP, persisted to `dast.json` in the data directory. Container-only, digest-pinned. The target must be the local machine (`localhost`, `127.0.0.0/8`, `::1`) or explicitly declared in `security.dast.allowed_targets` — a private-range address is no longer allowed by default (P81.29); `--mode active`/`api` additionally requires `security.dast.allow_active: true`.

### `aegis scan network`

```bash
aegis scan network <target> [target...]
```

Runs nmap + nuclei against a bare host/IP/CIDR list (attack-surface mapping), persisted to `network.json` in the data directory. Same target-allowlist gate as `scan dast`: only the local machine is allowed with no configuration, and a private-range target needs a `security.dast.allowed_targets` entry.

---

## `aegis security`

Manage security scanner availability (opengrep, gosec, bandit, brakeman, njsscan, trivy, gitleaks, trufflehog, kubescape, hadolint, grype, dockle, osv-scanner, syft) — the tools behind `aegis scan`/the `security_scan` tool.

### `aegis security status`

```bash
aegis security status
```

Shows how each scanner would run right now: host binary, container fallback, or unavailable (with the exact reason).

### `aegis security install`

```bash
aegis security install <tool> [--yes]
```

Guided, approval-gated host install for one scanner — prints the exact command and asks for confirmation before running it (`--yes` skips the prompt for scripted use).

### `aegis security build-image`

```bash
aegis security build-image [--profile core|full] [--netscanner] [--runtime docker|podman] [--image TAG] [--no-cache] [--project] [--skip-verify]
```

Builds one local image carrying every bundled scanner, then records its image ID in config so container-method scanning needs a single image instead of a digest-pinned image per tool. `--profile core` builds only the statically-linked scanners; the default `full` adds the Python (bandit/njsscan), Ruby (brakeman), Go (gosec plus the Go toolchain it needs) and network (nmap/nuclei) scanners (~2.1GB).

The pin is written to the **user** config, so every project on the machine uses the image — like the image itself and the shared `aegis-scanner-cache` volume, it is a machine-wide asset. `--project` pins it in this repo's `.aegis/config.yaml` instead, for the narrow case of a repo deliberately on a different image; the command always prints which file it wrote. Since project config overrides user config, a `security.multiscanner` block left in a repo by an older build shadows the machine-wide pin — `build-image` warns when it finds one. (`--global` is accepted as a deprecated no-op: it asked for what is now the default.)

The recorded image ID is re-verified before every container run — an image rebuilt or retagged behind Aegis's back fails closed rather than running silently. A source fingerprint is recorded alongside it, so an image built from an older Containerfile is reported as drifted rather than silently trusted. The build finishes by running `aegis security verify-image` (`--skip-verify` opts out). Run `aegis security update-db` afterwards to populate the vulnerability databases. See [security_scan.md](security_scan.md#the-multiscanner-image-one-image-instead-of-sixteen).

`--netscanner` builds the **second** image instead (~570MB, most of it the pinned nuclei template set): nmap, nuclei, and image-reference scanning with trivy/grype. It is separate from the multiscanner for exactly one reason — mount posture. Every tool in it needs network egress and none needs the workspace, so it runs with network **on** and no workspace mounted, ever, while the multiscanner keeps `--network none` with the workspace mounted. Both images come out of the same embedded build context (`--target` selects the stage), so they share one fetch script, one set of pinned tool versions and one source fingerprint. It needs no `update-db` — it has network, so trivy and grype refresh their own databases into a separate `aegis-netscanner-cache` volume. See [security_scan.md](security_scan.md#the-netscanner-image-network-on-workspace-never).

### `aegis security verify-image`

```bash
aegis security verify-image [--tool a,b] [--netscanner]
```

Proves the built image's scanners actually run: each tool the profile claims gets a version probe **and** a canary scan against a small embedded fixture with planted findings, asserting a non-zero finding count rather than exit 0. A tool that exits clean while reporting zero — because it never loaded its database, or was never in the image at all — is the failure this catches; a `--version` probe alone does not. Exits non-zero if any tool fails, so it works as a provisioning gate. A missing database cache is reported distinctly from a broken tool.

`--netscanner` verifies the network-facing image instead. Its canary is a trivy/grype scan of `debian:11-slim`, requiring at least 20 findings where both tools report ~190 — a result far below that means a missing or partial vulnerability database, not a clean image. (A small EOL Alpine, the obvious canary, makes trivy report *zero* on a working scanner: Alpine security data is per-branch and trivy stops reporting once a branch leaves support.) This one needs working network access, which is inherent: an image whose whole purpose is registry and target egress cannot be verified offline. nmap and nuclei get a version probe only, reported as skipped with the reason.

### `aegis security update-db`

```bash
aegis security update-db [--skip-java-db]
```

Downloads/refreshes the trivy, grype, and osv-scanner vulnerability databases into the `aegis-scanner-cache` volume, plus trivy's misconfiguration checks bundle. Run it once after `build-image`, then whenever you want fresher data — the databases are only as current as the last run, and `aegis security status` reports their age. `--skip-java-db` drops trivy's ~1.4GB Java database.

Each database is fetched independently: one failing does not abandon the rest, and the run ends with a per-step summary saying exactly which landed and which did not, exiting non-zero if any failed. Re-running retries only what failed, since steps that already succeeded are cheap no-ops.

This mounts no workspace, and scans still run with `--network none` and read the databases from the volume. Two other runs are given network access, both by the same rule — *the run with network does no analysis, or sees no workspace*: gosec's `go mod download` warm phase (workspace mounted **read-only**, no analysis) and the netscanner image (no workspace at all).

### `aegis security config`

```bash
aegis security config
```

View-only: prints the resolved `security.tools`/`security.default_method` configuration. Edit `.aegis/config.yaml` (project) or `~/.config/aegis/config.yaml` (user) directly to change it, or use the interactive `/security-config` TUI dialog.

### `aegis security baseline`

```bash
aegis security baseline [path]
```

View-only: prints each entry in `.aegis/security-baseline.yaml` (the accepted-risk suppression allowlist) with its status — active, expired, or invalid.

---

## `aegis debate`

Adversarially debate any claim — a security finding, threat/mitigation pair, design assertion, or a
claim about any document/plan/decision — instead of trusting a single unchallenged pass. Runs
headless, no daemon required (same one-shot construction as `aegis chat`): a critic challenges the
claim, grounded in cited evidence or an explicit concession, the proposer rebuts, this repeats for
`--max-rounds` (default 2), then an arbiter issues a final UPHOLD/REVISE/REJECT verdict with a
confidence label. Full guide with worked examples: [debate.md](debate.md). Mechanism reference:
[multi-agent.md](multi-agent.md#debate-p12).

```bash
aegis debate <claim> [flags]
```

```bash
aegis debate "The rate limiter fully mitigates the credential-stuffing risk on /login"
aegis debate "CVE-2024-XXXX in libfoo is exploitable in our usage" --max-rounds 3
aegis debate "This finding is a false positive" --output-format json
aegis debate "The plan's phased rollout correctly handles rollback" --domain generic --file docs/migration-plan.md
```

| Flag | Description |
|------|-------------|
| `--domain` | Default persona trio: `security` (default) or `generic`, for non-security claims |
| `--file` | File path the debate roles should read for grounding (repeatable) |
| `--proposer` | Persona for the proposer role (default `security-researcher`, or `general` if `--domain generic`) |
| `--critic` | Persona for the critic role (default `security-critic`, or `critic` if `--domain generic`) |
| `--arbiter` | Persona for the arbiter role (default `security-arbiter`, or `arbiter` if `--domain generic`) |
| `--max-rounds` | Maximum critique/rebuttal rounds before arbitration (default 2, hard-capped at 10) |
| `--output-format` | `text` (default) or `json` (final transcript + verdict object) |

---

## `aegis sandbox`

Inspect and configure the shell execution sandbox.

### `aegis sandbox detect`

```bash
aegis sandbox detect
```

Probe for available container runtimes (Docker, Podman, WSL containers, Apple Containers). Shows a table of what is available and which would be chosen by `auto`.

`auto` takes the first available runtime in the OS default order — **Windows:** `podman → docker → wslc`; **macOS:** `docker → podman → container` (Apple Containers); **Linux:** `docker → podman`. Override with `sandbox.priority`.

`wslc` is last on Windows rather than first. Its CLI is Docker-shaped but carries neither the hardening flags (`--cap-drop`/`--security-opt`/`--read-only`) nor the persistent-container detach/exec surface docker and podman do, and it cannot build the scanner images at all — so preferring it on a machine that has a real engine surfaced as broken scanners rather than as a runtime choice. It stays reachable, last, for a machine that has only Windows Containers.

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

## `aegis harden`

```bash
aegis harden [--project] [--yes]
```

Applies a hardened profile in one step, flipping the permissive-by-default knobs a security review is likely to flag:

- `sandbox.backend` → `auto` (containerized execution when a runtime is available, instead of running tool calls on the host)
- `security.egress_then_write` → `true` (a write after any network egress in the same run requires approval)
- any cost cap still at its unlimited default (`0`) is set to a conservative starting value (`session_cap_usd: 5`, `daily_cap_usd: 25`, `session_token_cap: 300000`, `daily_token_cap: 2000000`); caps you've already customized are left alone

Not touched: `security.network_allowlist` (empty means no restriction — which domains belong there can't be guessed) and plan-mode network access, which is gated by permission policy rather than config.

Shows the planned changes and prompts for confirmation unless `--yes` is passed. Writes to the global config by default; pass `--project` to write `.aegis/config.yaml` instead. Run `aegis dry-run` afterward to see the result, and hand-edit `config.yaml` to loosen anything too strict for your workflow.

---

## `aegis cron`

Inspect persisted cron jobs (background scheduled tasks) — the operator-facing review view for jobs otherwise only visible to the model itself via the `cron_list` tool. In particular, surfaces which jobs carry `auto_approve`, since those bypass interactive approval entirely at fire time.

### `aegis cron list`

```bash
aegis cron list [--auto-approve-only]
```

Lists every cron job: ID, enabled/disabled state, schedule, command, title, and an `[AUTO_APPROVE — fires unattended, bypassing interactive approval]` marker where applicable.

| Flag | Description |
|------|-------------|
| `--auto-approve-only` | Show only jobs with `auto_approve` set |

```bash
aegis cron list
aegis cron list --auto-approve-only   # audit what fires unattended
```

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

Show the curated model catalog and optionally probe for running local servers or get a hardware-aware recommendation.

```bash
aegis models [--local] [--recommend]
aegis models --fit [--fit-model M] [--budget-gb N] [--kv-type f16|q8_0|q4_0] [--write]
aegis models --fit-set a,b,c [--budget-gb N]
aegis models --fit-debate [--budget-gb N]
```

| Flag | Description |
|------|-------------|
| `--local` | Probe localhost for Ollama, LM Studio, LiteLLM |
| `--recommend` | Detect CPU/RAM and narrow local-model recommendations to what this machine can run |
| `--fit` | Size `provider.context_window` from the model's measured KV-cache cost instead of its training maximum |
| `--fit-model` | Model to fit (default: `provider.model`) |
| `--budget-gb` | Memory budget in GiB (default: `provider.vram_budget_gb`); omit both to print the window/footprint curve instead |
| `--kv-type` | KV cache element type: `f16`, `q8_0`, `q4_0` (default: `provider.kv_cache_type`) |
| `--write` | Patch the fitted `context_window` into the global config |
| `--fit-set` | Plan a whole **resident set**: comma-separated models that must fit the budget simultaneously |
| `--fit-debate` | Plan the configured debate's three seat models as one resident set |

Without flags: prints a curated list of recommended models by tier (frontier / balanced / local) with context windows and notes.

With `--local`: additionally probes `localhost:11434`, `localhost:1234`, `localhost:4000` and lists every available model found.

With `--recommend` (P20.3): detects CPU core count and total system RAM (best-effort, platform-specific — no GPU/VRAM introspection, by design; see [providers.md](providers.md#aegis-models)), prints the detected hardware, and narrows the `local` tier to the entries a rule of thumb says will run without heavy swapping. RAM detection fails soft to "unknown" (falls back to the full unnarrowed local list) on unsupported platforms or sandboxed environments. For any recommended model not already pulled, prints the exact `ollama pull <model>` command as a suggestion — never runs it.

### `--fit`: sizing a window to the hardware (P69.5)

The default sizing path takes the model's *training* maximum and halves it. On a VRAM-constrained
local GPU that is the wrong question: what binds is how much KV cache fits next to the weights. A
model with a 262144 training context gets recommended 131072 tokens, which is 16.5 GiB of KV cache
on its own — more than a 16 GB card holds before any weights are loaded.

`--fit` computes the cache exactly, from the geometry in Ollama's `/api/show`
(`blocks × kv_heads × (key_length + value_length) × bytes_per_element`), measures the resident
weights from `/api/ps`, and solves for the largest window that fits a budget you state:

```console
$ aegis models --fit --budget-gb 10.5
Model:        aegis-qwen35-9b:16k
Architecture: qwen35 (33 blocks, 4 KV heads, key 256 + value 256)
KV cache:     132 KiB per token at f16
Training max: 262144 tokens

Loaded now:   6.01 GiB resident, 6.01 GiB in VRAM, window 16000
              Ollama placed it entirely on the GPU at that window.
Weights:      4.00 GiB (measured: resident size minus the loaded window's KV cache)

Budget:       10.50 GiB
Fitted window: 51200 tokens (6.45 GiB of KV, 10.44 GiB total, 0.06 GiB spare)
```

Notes on how to read it:

- **The budget is yours to state, not something Aegis detects.** No GPU/VRAM introspection happens
  here either (same P17.5 rule as `--recommend`). What the command *does* report is Ollama's own
  placement verdict — the `size` vs `size_vram` split — which says whether the window currently in
  use actually fit, without guessing at how much memory exists.
- **The model must be loaded** for the weights figure. `/api/tags` reports on-disk size, which for a
  multimodal model includes a vision projector that is never resident unless an image is sent
  (2.57 GiB of phantom weights on qwen35-9b), so an unloaded model gets no estimate rather than a
  wrong one.
- **Omit `--budget-gb`** to print the window→footprint curve instead and pick a row yourself. This
  is the mode to use when planning several co-resident models.
- **Assumptions are printed, never hidden.** A model whose `head_count_kv` is absent or null gets the
  multi-head fallback *with a `NOTE:` line saying so*; sliding-window models are reported but not
  discounted, so their figures are a deliberate upper bound.
- **`--write` patches only the `context_window:` line**, leaving surrounding comments intact — but it
  does not update a comment that names the old number, so re-read the block after writing.

### `--fit-set` / `--fit-debate`: planning a resident set (P69.6)

`--fit` sizes **one** model as if it owned the card. A debate does not work that way: each seat
resolves its own model, so two or three models are resident at once and each of them needs to be
sized knowing the others exist.

Shape of the output (figures depend on your models and budget):

```console
$ aegis models --fit-debate --budget-gb 14.5
Resident set: configured debate seats — proposer=aegis-qwen35-9b, critic=aegis-qwen35-9b, arbiter=aegis-phi4-reasoning
Budget:       14.50 GiB (f16 KV cache)

MODEL                  WINDOW  KV CACHE  WEIGHTS
aegis-qwen35-9b        25600   3.30 GiB  4.00 GiB
aegis-phi4-reasoning   25600   1.68 GiB  2.89 GiB
...
1 seat(s) share a model with another and were planned once: Ollama holds one
runner per model name, so they share its weights and its KV cache.
```

- **Windows are split by equal *token* count**, not by equal bytes, clamped at each model's training
  maximum. Two seats reading the same transcript need comparable room to hold it; an equal-byte split
  gives the model with the cheap cache a window it can never fill.
- **Seats sharing a model are planned once.** Ollama holds one runner per model *name*, so a shared
  model means one copy of the weights and one KV cache. Counting it twice would refuse sets that fit.
- **Every model in the set must currently be loaded**, for the same reason `--fit` needs one: the
  on-disk size is not the resident size.
- **`--budget-gb` defaults to `provider.vram_budget_gb`**, so once that key is set this command and
  the daemon are answering from the same number. Without either, it refuses rather than inventing one.
- **When nothing fits, it says which wall was hit** — weights alone over budget, or windows squeezed
  below the viable floor. The fixes differ.

`--fit-debate` resolves the *actual* configured seat models through the same resolver the daemon and
`aegis debate` use, so it answers "will my debate fit" without spending a debate to find out. See
[research/debate-topology-plan.md](../research/debate-topology-plan.md) for the measured worked
example.

---

## `aegis config`

Show the fully resolved configuration (after applying all layers).

```bash
aegis config
```

### `aegis config update`

Reconcile an existing config file with template content added since it was created (e.g. new security-scanner options), without discarding customizations. Existing keys, comments, and formatting are left untouched — only genuinely new content is spliced into a section you're already using (an active top-level key), and only if it's not already present. A timestamped backup of the original is written alongside it before any change.

```bash
aegis config update [--global] [--project] [--dry-run]
```

| Flag | Description |
|------|-------------|
| `--global` | Only reconcile the global config (`~/.config/aegis/config.yaml`) |
| `--project` | Only reconcile the project config (`.aegis/config.yaml`) |
| `--dry-run` | Show what would change without writing |

With no `--global`/`--project` flag, both are reconciled if they exist.

```bash
aegis config update --dry-run   # preview
aegis config update             # apply, both global and project config
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

## `aegis knowledge`

Manage the project knowledge base — a separate, searchable FTS5 (optionally hybrid BM25+semantic) index of documentation and code comments backing the `project_knowledge` tool. Not to be confused with `aegis index` above (the structural repo map); see [Memory & Knowledge](memory-and-knowledge.md) for how the two differ.

### `aegis knowledge index`

```bash
aegis knowledge index
```

Rebuilds `.aegis/knowledge.db` from `README.md`, `.md`/`.txt`/`.rst` files, and Go doc comments across the project. When `embeddings.enabled: true` in config, also computes and stores semantic embeddings for hybrid recall (see [Configuration Reference](configuration.md#full-config-reference)). This is a full reindex — there's no incremental staleness detection, so rerun it after adding significant documentation.

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
