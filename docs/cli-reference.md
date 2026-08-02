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
| `--output-format <text\|json\|stream-json>` | `text` streams to stdout as-is (default); `json` emits one final result object; `stream-json` emits one JSON event per line plus a trailing result |
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

New sessions default to **plan mode** (read-only) — a materially lower-trust posture than the local TUI/CLI's own default, since the caller is an external harness. A tool call needing approval in a higher mode (`build`/`auto`) is **denied** by default, since an MCP `tools/call` is a synchronous request/response with no human available to ask; pass `--auto-approve` (or set `mcp_server.auto_approve: true`) to allow it instead. See [Configuration Reference](configuration.md#full-config-reference) (`mcp_server:` block) for the config keys.

**v1 scope, deliberately:** individual built-in tools (`security_scan`, `read_file`, etc.) are not exposed 1:1 as MCP tools bypassing the agent loop — every MCP tool call goes through a real Aegis session. `notifications/cancelled` is not propagated to an in-flight `aegis_prompt` call. Both are documented follow-ups, not oversights.

**Authentication (optional, FIND-02/P24.2):** by default any process able to write to this subprocess's stdin can drive sessions through it — the same trust assumption as most stdio MCP servers (an API key passed via `env` in the host's server config, validated by the server itself, is the standard pattern). Set `AEGIS_MCP_TOKEN` in the environment the calling harness uses to launch `aegis mcp-serve` to require it: `tools/call` is denied with an `authentication required` error (JSON-RPC code `-32001`) until the client sends the custom `aegis/authenticate` request with `{"token": "<the same value>"}` (`initialize`/`ping`/`tools/list` stay reachable unauthenticated, since they expose no session-driving capability). This is an Aegis-specific extension, not part of the base MCP spec — a host unaware of it simply never calls it, which is fine as long as `AEGIS_MCP_TOKEN` is left unset. Leave it unset (the default) for unchanged, no-auth behavior.

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

`aegis doctor`'s "workspace trust" check surfaces the same frozen-settings state. Restart the daemon after trusting a directory to apply the newly-unfrozen settings.

---

## `aegis scan`

Run available security scanners against a path.

```bash
aegis scan [path] [flags]
```

Default path is the current directory. Runs every enabled scanner (**opengrep**, **trivy**, **gitleaks**, **kubescape**, **hadolint**, **osv-scanner**, **grype**, whichever are installed or container-fallback-able) and produces a normalized findings report with severity, location, rule ID, and remediation hint, persisted to `.aegis/security/scan.json`. The language-targeted engines (**gosec**/**bandit**/**brakeman**/**njsscan**) are opt-in — a plain scan auto-detects the project's language (`go.mod`/`*.go`, `requirements.txt`/`*.py`, `Gemfile`/`*.rb`, `package.json`/`*.js`, and more) and auto-enables the matching one for this run only, without touching config, so a Rust or Java repo never triggers bandit. **hadolint**/**kubescape** are likewise skipped, with a reason, when the path has no Dockerfile/Kubernetes manifest.

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

Runs image-oriented scanners (trivy image, grype, dockle) against a container image reference (e.g. `alpine:3.20`) and prints a unified findings report, persisted to `.aegis/security/image.json`. Host-binary only — an image scanner that would otherwise run via a container is reported skipped, since scanner containers run network-isolated and can't pull the target image.

### `aegis scan sbom`

```bash
aegis scan sbom [path] [-o output]
```

Generates a CycloneDX JSON SBOM via syft over the given path (default: current directory), written to `.aegis/sbom.cdx.json` by default (or `-o`/`--output`).

### `aegis scan dast`

```bash
aegis scan dast <target-url> [--mode baseline|active|api] [--api-definition <path-or-url>]
```

Crawls (and, in `--mode active`/`api`, actively attacks) a *running* application via OWASP ZAP, persisted to `.aegis/security/dast.json`. Container-only, digest-pinned. The target must be loopback/private or explicitly declared in `security.dast.allowed_targets`; `--mode active`/`api` additionally requires `security.dast.allow_active: true`.

### `aegis scan network`

```bash
aegis scan network <target> [target...]
```

Runs nmap + nuclei against a bare host/IP/CIDR list (attack-surface mapping), persisted to `.aegis/security/network.json`. Same target-allowlist gate as `scan dast`.

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
aegis security build-image [--profile core|full] [--runtime docker|podman] [--image TAG] [--no-cache] [--project] [--skip-verify]
```

Builds one local image carrying every bundled scanner, then records its image ID in config so container-method scanning needs a single image instead of a digest-pinned image per tool. `--profile core` builds only the statically-linked scanners; the default `full` adds the Python (bandit/njsscan), Ruby (brakeman) and network (nmap/nuclei) scanners (~1.8GB).

The pin is written to the **user** config, so every project on the machine uses the image — like the image itself and the shared `aegis-scanner-cache` volume, it is a machine-wide asset. `--project` pins it in this repo's `.aegis/config.yaml` instead, for the narrow case of a repo deliberately on a different image; the command always prints which file it wrote. Since project config overrides user config, a `security.multiscanner` block left in a repo by an older build shadows the machine-wide pin — `build-image` warns when it finds one. (`--global` is accepted as a deprecated no-op: it asked for what is now the default.)

The recorded image ID is re-verified before every container run — an image rebuilt or retagged behind Aegis's back fails closed rather than running silently. A source fingerprint is recorded alongside it, so an image built from an older Containerfile is reported as drifted rather than silently trusted. The build finishes by running `aegis security verify-image` (`--skip-verify` opts out). Run `aegis security update-db` afterwards to populate the vulnerability databases. See [security_scan.md](security_scan.md#the-multiscanner-image-one-image-instead-of-sixteen).

### `aegis security verify-image`

```bash
aegis security verify-image [--tool a,b]
```

Proves the built image's scanners actually run: each tool the profile claims gets a version probe **and** a canary scan against a small embedded fixture with planted findings, asserting a non-zero finding count rather than exit 0. A tool that exits clean while reporting zero — because it never loaded its database, or was never in the image at all — is the failure this catches; a `--version` probe alone does not. Exits non-zero if any tool fails, so it works as a provisioning gate. A missing database cache is reported distinctly from a broken tool.

### `aegis security update-db`

```bash
aegis security update-db [--skip-java-db]
```

Downloads/refreshes the trivy, grype, and osv-scanner vulnerability databases into the `aegis-scanner-cache` volume, plus trivy's misconfiguration checks bundle. Run it once after `build-image`, then whenever you want fresher data — the databases are only as current as the last run, and `aegis security status` reports their age. `--skip-java-db` drops trivy's ~1.4GB Java database.

Each database is fetched independently: one failing does not abandon the rest, and the run ends with a per-step summary saying exactly which landed and which did not, exiting non-zero if any failed. Re-running retries only what failed, since steps that already succeeded are cheap no-ops.

This is the only Aegis container run given network access, and it mounts no workspace; scans still run with `--network none` and read the databases from the volume.

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
```

| Flag | Description |
|------|-------------|
| `--local` | Probe localhost for Ollama, LM Studio, LiteLLM |
| `--recommend` | Detect CPU/RAM and narrow local-model recommendations to what this machine can run |

Without flags: prints a curated list of recommended models by tier (frontier / balanced / local) with context windows and notes.

With `--local`: additionally probes `localhost:11434`, `localhost:1234`, `localhost:4000` and lists every available model found.

With `--recommend` (P20.3): detects CPU core count and total system RAM (best-effort, platform-specific — no GPU/VRAM introspection, by design; see [providers.md](providers.md#aegis-models)), prints the detected hardware, and narrows the `local` tier to the entries a rule of thumb says will run without heavy swapping. RAM detection fails soft to "unknown" (falls back to the full unnarrowed local list) on unsupported platforms or sandboxed environments. For any recommended model not already pulled, prints the exact `ollama pull <model>` command as a suggestion — never runs it.

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
