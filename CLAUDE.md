# CLAUDE.md

Guidance for Claude Code working in this repository. Keep this file short — deep
rationale belongs in `docs/`, not here.

## Build & Run

```bash
go build -o ./aegis ./cmd/aegis    # or ./build-macos.sh / build-linux.sh / build-windows.ps1
go run ./cmd/aegis
aegis --first-init                 # first-time setup
export OPENAI_API_KEY="ollama"     # or ANTHROPIC_API_KEY for cloud
```

`go build` needs no container runtime or Node.js. The web UI's `dist/` and the
scanner container context are committed and `go:embed`-ed.

Rebuild the web UI only when editing frontend source (commit the result):

```bash
npm --prefix internal/server/webui/frontend ci
npm --prefix internal/server/webui/frontend run build
```

Scanner images (see [docs/security_scan.md](docs/security_scan.md), [docs/installation.md](docs/installation.md)):

```bash
aegis security build-image [--profile core] [--netscanner]
aegis security verify-image [--netscanner]
aegis security update-db
```

## Testing

```bash
go test ./...
go test ./internal/engine/... -run TestBudget
go test -race ./...

AEGIS_EVAL_UPDATE=1 go test ./internal/eval/... -run TestScenario_ToolRoundTrip
AEGIS_EVAL_UPDATE=1 go test ./internal/security/... -run TestScanRegressionAcrossRecordedOutputs
```

Live tiers need a real model server, are on-demand only (no scheduled CI), and
**must** use `-count=1` — Go's test cache can't see that the model server changed,
so a cached pass looks exactly like a reproduced one.

```bash
go test -tags live_eval -count=1 ./internal/eval/... -run TestLiveModelQuality -v
go test -tags live_probe -count=1 ./internal/toolcallprobe/... -run TestLiveProbeReachesAVerdict -v
AEGIS_EVAL_MODEL=qwen3:14b-32k go test -tags live_workflow -count=1 ./internal/eval/... -run TestLiveWorkflow -v
AEGIS_EVAL_BASELINE_HARNESS='claude -p {prompt}' go test -tags live_workflow -count=1 ./internal/eval/... -run TestLiveWorkflowBaseline -v
```

`live_workflow` needs the model's context window pinned in a Modelfile
(`PARAMETER num_ctx 32768`) — `OLLAMA_CONTEXT_LENGTH` isn't visible to Aegis
before a model loads, so the tier otherwise plans against 4096 and skips.

## Architecture

Single binary, daemon + client:

```
TUI (internal/tui) → client (internal/client) → daemon (internal/server)
  → engine.Run (internal/engine) → provider.Adapter.Stream (internal/provider/*)
    ↕ tools via tool.Registry (internal/tool/builtin/*)
```

`aegis serve` is the daemon; bare `aegis` starts an embedded one in-process if
none is reachable.

### Key packages

| Package | Role |
|---------|------|
| `internal/engine` | Agent loop: model turns, tool dispatch, compaction, guard, loop detection, budgets |
| `internal/server` | HTTP daemon; wires sessions, tools, permissions, personas, swarm, MCP, cron, checkpoints |
| `internal/provider` | `Adapter` seam (`Stream`) + message types; `anthropic`/`openai` adapters, plus retry/failover/`num_ctx`/admission-control decorators |
| `internal/session` | SQLite session store (conversations, traces, cost) |
| `internal/tool` | `Tool` interface + `Registry` (register/expose separation) |
| `internal/tool/builtin` | 50+ built-in tools |
| `internal/permission` | Modes `plan`/`build`/`auto`, allow/deny rules, advisory persona tool gate |
| `internal/persona` | 22 built-in system prompts + user/project `.md` personas |
| `internal/skills` | Progressive-disclosure skills (project/user files + embedded built-ins) |
| `internal/drive` | Phased skill drive: multi-phase builds as fresh context-reset runs |
| `internal/swarm` | Sub-agents (goroutine or subprocess) + file mailbox |
| `internal/debate` | Propose/critique/rebut/arbitrate primitive ([docs/debate.md](docs/debate.md)) |
| `internal/compaction` | Summarizes old turns near the context window |
| `internal/checkpoint` | Per-turn restore points for `/rewind` |
| `internal/memory`, `internal/knowledge` | Persistent recall; project knowledge base |
| `internal/repomap` | Ranked `<repo_map>` overview, byte-budgeted and mtime-cached |
| `internal/lsp` | Minimal LSP client for diagnostics/references |
| `internal/cost` | Token/USD tracking backing the `cost.*` budget knobs |
| `internal/tui` | Bubbletea UI |
| `internal/termsafe` | ANSI/OSC stripping (`StripControlSeqs`, `StripDangerousSeqs`) |
| `internal/config` | Layered config |
| `internal/toolpath` | Resolves optional host binaries (rg, git, gh, mmdc, plantuml) via `commands:` config |
| `internal/mcp`, `internal/mcpserver` | MCP client (out) and server (`aegis mcp-serve`, in) |
| `internal/acp` | ACP JSON-RPC for Zed/Neovim |
| `internal/sandbox` | Local/Docker/Podman/WSL/Apple execution sandboxes; persistent per-workspace container |
| `internal/workspacetrust` | Per-directory trust grants (`aegis trust --dir`) |
| `internal/cron` | Background job scheduler |
| `internal/guard` | Second-pass output validation against a rubric/schema |
| `internal/toolcallprobe`, `internal/modelcaps` | Tool-calling smoke probe + persisted per-model capability cache |
| `internal/eval` | Scenario-based behavior regression harness (deterministic adapter) |

### Configuration layers

defaults → `~/.config/aegis/config.yaml` → `.aegis/config.yaml` → `AEGIS_*` env.
Secrets come only from the environment (or `.aegis/.env`, which is read only in
a **trusted** workspace and may not set `AEGIS_*` — it is a secrets file, not a
config layer).
`workspace.additional_roots` is frozen from project config and still needs a
trust grant per root. See [docs/configuration.md](docs/configuration.md).

## Invariants worth knowing before you edit

- **Prompt budget.** `TestEffectiveSystem_localProfileBudget` fails the suite when
  the local base prompt crosses `localBasePromptCeilingTokens`. Raising it is
  allowed; raising it silently is not. `<deferred_tools>` prints `tool.Summarize`,
  not `Description()`; `OutputSchema` is never sent to a model. Under the local
  profile `edit_file` is deferred and the handle-based editors are not — a test
  pins that direction. A tool's `Description()` or error must never name a tool
  the active profile defers.
- **Tool registry clones.** `Registry.Clone()` shares one `toolTable` (with its
  own mutex) so a later parent registration — MCP's `tools/list_changed` above
  all — reaches existing clones. A clone's own `Register`/`Upsert` goes to a
  clone-local overlay instead, so session-scoped tools stay session-scoped.
  Both directions are pinned by tests. Never give a sub-agent, debate role or
  session `s.tools` itself; hand it a clone.
- **Parallel tool rounds.** In `engine.runTools`, write/execute calls take one
  plain exclusive `sync.Mutex` (`execLock`) so they never run concurrently with
  each other. Reads/network calls take no lock — they are *not* held off by a
  concurrent write (P8.6). The only read-vs-write ordering is the same-`path`
  dependency graph, keyed on the literal `"path"` input field, so a `shell` call
  and a `read_file` are never ordered.
- **Tool result size.** Caps live in `internal/tool/builtin/truncate.go`, which
  carries the posture table (which end survives, what happens to the remainder).
  Notice bytes are reserved *out of* the cap; remainders spill to
  `<workspace>/.aegis/spill/` (reachable by `read_file`, **not** by grep).
  Those caps are per *call*; `roundcap.go` bounds a whole parallel round on top
  (`engine.Options.RoundResultCap`, wired to `builtin.CapRound` — a new engine
  construction site must wire it or the round is unbounded again).
- **Search backends.** ripgrep and the pure-Go walker must return identical
  results; a regression test asserts it. Watch `--no-ignore-vcs` + generated
  `-g !dir/` excludes, `cmd.Dir` at the workspace root, and NUL-separated parsing.
  See [docs/host-tools.md](docs/host-tools.md).
- **Run budgets.** `BudgetUSD`, `MaxTokensPerRun`, `MaxIterations` (40),
  `MaxWallClockPerRun` (off by default), plus `MaxTurnStall` (900s, on) which is
  the only bound covering tool execution. Stall and wall-clock aborts are fatal
  to a drive; loop/tool-failure aborts are resettable.
- **The stall bound sits above every *per-call* timeout, not every timeout.**
  `TestToolTimeoutsStayUnderTheStallBound` enumerates them; a new one goes in that
  table. An *aggregate* bound above 900s (the agent tool's fan-out and debate) is
  admissible only if it decomposes into sub-900s waits that beat in between —
  otherwise a healthy long call dies as a fatal `ErrTurnStalled`. Beats ride
  `internal/heartbeat`, which **chains**: a sub-agent's watch must never shadow
  its parent's.
- **Loop detection.** `PollExempter` hides a call entirely (polls only);
  `SignatureTransparent` hides only its arguments (bookkeeping only, never a
  model-chosen search query). Tests keep both sets narrow and disjoint.
- **Compaction.** The summary uses a fixed section skeleton, and the
  `<read-files>`/`<modified-files>` tags are a wire format between successive
  summaries — renaming them breaks accumulation with single-compaction tests
  still green. `FallbackCompact` must carry them too.
- **One compaction trigger.** `tokenest.CompactionTrigger(window, maxTokens)` is
  the only threshold; the engine *and* `compaction.Summarizer` read it, and the
  engine also passes the number it used down per call
  (`engine.BudgetedCompactor` → `compaction.WithTokenBudget`). Never give either
  side a rule of its own — P66.14 closed a 1,229-token disagreement that made the
  summarizer refuse compactions the engine had asked for. Anything per-*run*
  reaching the Summarizer travels on the context, never a setter: it is built once
  per server and shared by every session.
- **Interrupted tool calls.** `repairOrphanedToolUses` reports a started call as
  *possibly* completed (tracked in `Engine.startedTools`), never as not run.
- **Embedded assets.** Containerfile, scanner scripts, built-in skills, personas
  and web UI `dist/` are `go:embed`-ed — rebuild the binary or you ship the old copy.
- **Scanner containers.** Multiscanner runs `--network none` with the workspace
  mounted; `aegis-netscanner` runs with network and **never** a workspace mount;
  `update-db` is the only networked run of the former. gosec is the one two-phase
  tool, and a failed warm phase aborts it rather than reporting a confident wrong count.

## Personas and skills

Personas are `.md` files with YAML frontmatter (`description`, `model`, `mode`,
`tools`, `rules`, `output_guard`); built-ins are embedded, user/project ones live
in `~/.aegis/personas/` or `.aegis/personas/` and hot-reload. A persona's `tools`
list is **advisory** — it prompts/warns, never enforces. See [docs/personas.md](docs/personas.md).

Skills inject only `name — description` until loaded via the `skill` tool.
Embedded built-ins stay dormant until enabled (`aegis skills enable <name>`).
Precedence: project > user > embedded. Picking between the document-shaped ones:
`document-codebase` for docs that live in this repo, `html-report`/`latex-report`
for a standalone deliverable, `documentation-as-code` only with an external
DaC template repo. See [docs/skills.md](docs/skills.md).
