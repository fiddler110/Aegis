# Aegis Capability Roadmap

**Last updated:** 2026-07-11 (late night) — **P25.1 (per-session working directory) and P25.2
(sandbox backend name trap) shipped.**
A hands-on session earlier the same day, driving the real TUI and the daemon HTTP API against
local Ollama models (qwen3.6:35b-a3b-deep/fast, qwen3coder:30b-a3b-fast), confirmed five
engine/daemon defects and two quality gaps that make Aegis look far less capable with local models
than it actually is — filed as **P25.1–P25.7**, with root causes verified at specific file:line
locations and a repeatable regression harness preserved at
[research/eval-harness-drive.py](eval-harness-drive.py). P25.1 and P25.2, the two highest-priority
items, shipped this session (see their entries below for implementation detail and what's
deliberately still deferred); **P25.3–P25.7 remain open.** The Tier 3 pass (P24.14 + web-UI batches
A/B/C) completed earlier the same day — writeup in [releases.md](releases.md#latest-changes).

This document tracks only **open** work and what's next. For shipped-feature history and full design
rationale, see [releases.md](releases.md).

---

## Status

**Open items:** **Tier 1 — P25.3–P25.7** (local-model live-eval findings, 2026-07-11; P25.1 and
P25.2 shipped 2026-07-11; see
[Open Work — P25](#open-work--p25-local-model-live-evaluation--2026-07-11)), plus the long-standing
Tier 4 parked set — P24.21 (threat-model residual), P22.5/P22.6, P20.2–P20.3,
P13.3.2–P13.3.3/P13.4, P9.4, P6.1.

**Next session:** work P25 top-to-bottom. P25.3 (output guard vs local/thinking models) is next —
3× turn latency and meta-text leakage into user-visible answers. Re-run the harness (recipe inside
the P25 section) after each fix to confirm the corresponding failure mode is gone — including a
re-run against P25.1's and P25.2's fixes, since the harness itself predates both.

**Priority order:** see [Priority Order](#priority-order) below — it is the authoritative "what's
next" view, ordered by tier and effort.

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no concrete trigger, or explicitly parked pending demand — do not
build speculatively.

### Tier 1 — P25 local-model live-eval findings (2026-07-11)

All Critical/Important findings from the 2026-07-10 STRIDE-A threat model are shipped. The
2026-07-11 live evaluation (real TUI + daemon API runs against local Ollama models) is the new
trigger; full detail in [Open Work — P25](#open-work--p25-local-model-live-evaluation--2026-07-11).

- ~~**P25.1 — Per-session working directory**~~ **SHIPPED 2026-07-11** — highest-leverage item:
  fixed the dominant "model looks dumb in the action phase" failure mode. See the writeup at the
  end of the [P25.1 entry below](#open-work--p25-local-model-live-evaluation--2026-07-11) for what
  shipped vs. what's explicitly deferred.
- ~~**P25.2 — Sandbox backend name trap + untruthful `/config/sandbox`**~~ **SHIPPED 2026-07-11** —
  closed the live safety hole (`backend: podman` silently ran unsandboxed on the host). See the
  writeup at the end of the
  [P25.2 entry below](#open-work--p25-local-model-live-evaluation--2026-07-11) for implementation
  detail.

1. **P25.3 — Output guard vs local/thinking models** (M) — 3× turn latency and meta-text leakage
   into user-visible answers.
2. **P25.4 — Approval ergonomics** (M) — dead `y` hotkey, useless-or-dangerous generated
   Allow-always rules, read-only shell commands gated as execute.
3. **P25.5 — Token-usage observability for local providers** (S).
4. **P25.6 — Local-model profile: prompt weight + scope-creep guardrails** (S/M).
5. **P25.7 — Promote the live-eval harness into `internal/eval`** (M) — regression-locks all of
   the above (now including P25.1 and P25.2).

### Tier 2 — empty

P24.20 (FIND-17) shipped 2026-07-11 (see [releases.md](releases.md#latest-changes)), the last item
in this tier. Next trigger: a new threat-model pass or a reported incident.

### Tier 3 — empty

All items shipped 2026-07-11 (see [releases.md](releases.md#latest-changes)):

- ~~**P15.6–P15.7 — Web UI batch C**~~ **SHIPPED 2026-07-11** (`05ca71f`, built by two parallel
  worktree sub-agents): "Security check" panel (scanner status + two-phase guided install,
  severity-sorted structured findings table — `ScanResponse` gained mirrored `security.Report`
  fields — and the accepted-risk baseline view) and "Skills & memory" panel (memory viewer +
  per-scope note composer, built-in-skills toggle list — `ConfigSkillsResponse` gained the
  `available` catalog).
- ~~**P15.3–P15.5, P15.8–P15.10 — Web UI batches A/B**~~ **SHIPPED 2026-07-11** (`d8fc58e`,
  `eb5a14c`): persona/model panel, cost/token readout + budget-alert toasts, checkpoints/rewind,
  always-allow approvals, debate ("stress-test a claim") + knowledge panels, archived-chats
  tab/prune/background sessions + activity view. P15.11's non-technical framing was applied
  throughout all three batches.
- ~~**P24.14 — FIND-12: MCP outbound tool-call argument content**~~ **SHIPPED 2026-07-11**
  (`73880ae`): outbound data flow documented in docs/mcp-trust-boundary.md; opt-in per-server
  `scan_arguments` outbound secret scan (default off, flag-never-block) in internal/mcp.

### Tier 4 — Parked / low priority / no current trigger

Do not build speculatively — revisit only if a concrete trigger (user demand, reported pain,
incident) appears.

- **P24.21 — FIND-33: memory-lock/zero the bearer token in `Client` process memory** (M, security,
  Low, CVSS 2.8). Explicitly low priority per the finding itself — host/OS access is already a
  significant compromise.
- **P20.2 — Blind model compare** (M) and **P20.3 — Hardware-aware model recommendation** (M):
  competitive-inspired, no direct reported pain.
- **P13.3.2 — `@shell`/`@last` context token** (S) and **P13.3.3 — ACP terminal capability
  passthrough** (M/L): P13.3.3 deferred pending ACP-host usage.
- **P13.4 — Nebula-inspired engagement tooling** (M): "interesting, not urgent" per its own scoping.
- **P9.4 — Per-task model routing** (M) and **P6.1 — Mid-turn state persistence** (L): no concrete
  trigger; check with user before starting.
- **P22.5 — `/side` ephemeral conversation** (S/M) and **P22.6 — Raw scrollback mode** (S/M): polish
  without demand.

---

## Open Work — P25 (Local-Model Live Evaluation — 2026-07-11)

Source: a live evaluation session on 2026-07-11 that drove the real TUI (under GNU screen) and
the daemon HTTP API/SSE (the same seam the TUI uses) against local Ollama models. Headline
result: **the local model is not the bottleneck — the harness is.** The same
`qwen3.6:35b-a3b-deep` that flailed for ~20 minutes in the TUI (web-search detour, `find /`
scan, six approval prompts) completed the identical run-diagnose-fix-verify task in **26 s with
5 tool calls** once the workspace root was correct and the output guard was off. Comparative
runs, same seeded-bug task ("run temps.py, fix the bug, re-run to confirm"):

| Configuration | Wall time | Tool calls | Outcome |
|---|---|---|---|
| TUI, daemon cwd ≠ session dir, guard on | ~20 min, 6 approvals | many | web-search detour, `find /`, eventually correct |
| API, correct root, guard **on** (deep) | 78 s | 7 | correct, but guard added 34 s and leaked "PASS." into the answer |
| API, correct root, guard **off** (deep) | **26 s** | 5 | clean, correct, verified |
| API, correct root, guard off (fast) | 38 s | 4 | clean, correct |
| API, correct root, guard off (qwen3coder:30b) | 87 s | 11 | correct but over-engineered (unrequested files, unprompted `remember`) |

**Regression harness** (used for every run above; re-run after each P25 fix):
[research/eval-harness-drive.py](eval-harness-drive.py) drives `POST /sessions` →
`PATCH /sessions/{id}` (model override) → `POST /sessions/{id}/messages` (SSE, per-turn
`guard_enabled` override) and prints a timestamped event timeline + summary JSON. Start a
dedicated daemon with cwd = the target project and an isolated data dir:

```bash
cd <target-project> && env OPENAI_API_KEY=ollama \
  AEGIS_DATA_DIR=<scratch>/testdata AEGIS_SERVER_ADDR=127.0.0.1:4199 \
  AEGIS_SANDBOX_BACKEND=container AEGIS_SANDBOX_RUNTIME=podman \
  AEGIS_SANDBOX_IMAGE=python:3.12-slim AEGIS_PERMISSION_AUTO_APPROVE_EXEC=true \
  aegis serve
# then:
python3 research/eval-harness-drive.py http://127.0.0.1:4199 \
  <scratch>/testdata/daemon.token <model-or-"default"> <on|off> "<task text>"
```

Bearer token: `<data_dir>/daemon.token`. This machine has podman (no docker CLI); the podman
machine must be running. Seeded-bug fixture: a two-file `temps.py`/`temps.csv` project where
`row["temp"]` (a CSV string) is added to an int — trivially recreatable, or lift it into the
P25.7 eval fixture.

---

**P25.1 — Per-session working directory. [Tier 1]** (M/L) — **SHIPPED 2026-07-11**

- **Symptom:** a TUI session started in directory X, connecting to a daemon that was started in
  directory Y, displays `Dir X` in the welcome screen but executes every tool in Y. In the live
  eval the agent ran `git status` (answered from the Aegis repo, not the session dir), concluded
  "there's no temps.py in the workspace", web-searched, then tried `find / -name temps.py`.
  `read_file` with the session dir's absolute path was refused (outside workspace root), pushing
  the model to shell `cat`/`ls` — each an execute-approval prompt. This is the dominant cause of
  "the model looks dumb in the action phase" with any client that reuses a running daemon.
- **Root cause:** `internal/server/server.go:314` — `cwd, err := os.Getwd()` once at daemon
  startup; that single value becomes `builtin.Register(..., Root: cwd, ...)` (tool workspace
  confinement), `s.workspace`, `memory.NewSources`, `loadRepoMap`, `lsp.NewManager`, the
  knowledge store, persona/command discovery dirs, and sandbox `ExecOpts.Dir`. Sessions have no
  workdir of their own: `api.CreateSessionRequest` (internal/api/api.go:11) has only
  Title/System/Mode/Persona. Meanwhile `aegis chat` (internal/cli/chat.go:95) builds its own
  in-process `engine.New` rooted at the *caller's* cwd — so chat and the TUI behave differently
  against the same daemon, which masked the bug.
- **Fix sketch:** add `Workdir string` to `CreateSessionRequest` + `SessionMeta` (persist in the
  session store). TUI/`chat`/ACP/web clients send their cwd on create. Thread it per-session:
  tool `Root`, sandbox `ExecOpts.Dir`, repo-map/memory/knowledge lookups keyed by session
  workdir (lazy-load + cache per root, the way persona `Refresh` signature-caches). Decide and
  document the trust boundary: daemon should validate the requested workdir (exists, is a
  directory, optionally under an allowlist — a remote-capable daemon must not become an
  arbitrary-filesystem oracle; note `server.allow_remote` exists). Empty `Workdir` keeps today's
  daemon-cwd behavior for backward compatibility. If full threading is too large for one
  session, an acceptable first cut: reject session creation (or warn loudly in the TUI banner)
  when client cwd ≠ daemon workspace root instead of silently mis-executing.
- **Acceptance:** TUI started in dir X against a daemon started in dir Y reads/writes/executes
  in X; `read_file` accepts X-relative and X-absolute paths; welcome-screen `Dir` matches actual
  tool behavior; `aegis chat` and TUI give identical results for the seeded-bug task; harness
  run from a second project against a shared daemon passes without the `find /` failure mode.
- **Tests:** server test — two sessions with different workdirs on one daemon each read their
  own `temps.py`; eval scenario asserting no `web_search`/`find /` tool calls for the fixture
  task; regression for the workspace-confinement error message pointing at the *session* root.
- **Shipped implementation (differs from the fix sketch above):** rather than a per-root
  `tool.Registry` cache — which would mean reconnecting MCP servers, re-registering plugins, and
  rebuilding the swarm/agent tool once per distinct session directory — the daemon keeps one
  shared, MCP/plugin/swarm-wired registry and threads the session's workdir through
  `context.Context` (`tool.WithWorkdir`/`tool.WorkdirFromContext`, mirroring the existing
  `tool.WithRegistry` pattern `tool_search` already relied on). `engine.Options.Workdir` sets it
  once per turn (`executeTool`, right next to `tool.WithRegistry`); every workspace-confined tool
  (file ops, `ls`/`glob`/`grep`, git, `shell`, security/diagram/latex/dast/recon tools,
  `remember`/`save_skill`, background shell jobs) resolves its effective root from that context
  value, falling back to its own construction-time root when unset. `sandbox.ExecOpts.Dir` was
  already per-call for the local and container backends, so this reaches the shell tool with no
  sandbox-package changes. `CreateSessionRequest`/`SessionMeta`/`session.Session`/`session.Meta`
  gained `Workdir`; the session store persists it via the same idempotent
  `ALTER TABLE ... ADD COLUMN` pattern as the P14.7 `Model` field. `handleCreateSession` resolves
  and validates it (must exist, be a directory) and enforces the trust boundary: a new
  `server.session_workdir_allowlist` config key (alongside `server.allow_remote`) restricts a
  remote-accessible daemon to the daemon's own workspace or an explicitly allowlisted root;
  loopback-only daemons (the default) accept any existing directory, matching today's trust model.
  TUI (`internal/cli/root.go`) sends its cwd on create and prefers a resumed session's own
  persisted `Workdir` over the local cwd; ACP (`internal/acp/agent.go`) now forwards the
  `session/new` `cwd` param it was previously parsing and discarding. `aegis chat`, the web UI,
  `mcp-serve`, and `parallel.go` are unchanged (see below).
- **Deliberately deferred (documented gap, not a silent one):** `lsp.Manager`, `knowledge.Store`,
  `longmem.Store`, the cached repo-map (`s.repoMap`), and persona/command/agent-def directory
  discovery all remain scoped to the daemon's own default workspace regardless of a session's
  Workdir — each is a daemon-wide singleton today (one set of language servers, one knowledge DB,
  etc.) and re-scoping them per session is a materially larger change with no test/acceptance
  criterion above actually requiring it. `sandbox.OSBackend` (seatbelt/bwrap) also bakes its
  write-confinement profile to the daemon's workspace at construction — a session on a different
  Workdir under the `os` sandbox backend won't get write access extended to its own directory;
  `resolveSessionWorkdir` logs a one-time warning when this combination is detected. The web UI
  (no filesystem cwd in a browser) and `aegis mcp-serve`/`parallel.go` session creation still omit
  Workdir, falling back to the daemon's default exactly as before this change. Revisit if a
  concrete pain point shows up in a future live-eval pass.

**P25.2 — Sandbox backend name trap + untruthful `/config/sandbox`. [Tier 1]** (S) — **SHIPPED 2026-07-11**

- **Symptom:** `sandbox.backend: podman` (or `docker`) is accepted everywhere — config file,
  `AEGIS_SANDBOX_BACKEND`, `PATCH /config/sandbox` — and `GET /config/sandbox` echoes
  `{"backend":"podman","image":"python:3.12-slim"}` back, but execution silently runs on the
  **local host, unsandboxed**. With `auto_approve_exec: true` (the exact combo the docs suggest
  for containerized auto-runs) every shell command ran on the host unprompted. Only a single
  daemon-startup WARN line ("auto_approve_exec is enabled with the local sandbox") hinted at it.
  Verified live: host-path tracebacks until the backend was respelled, `/workspace` tracebacks
  after.
- **Root cause:** `SelectSandbox` (internal/server/server.go:570) switches only on
  `"container" | "auto" | "os"`; anything else — including the runtime names the docs/CLAUDE.md
  advertise ("local, Docker, Podman, WSL containers, Apple Containers") — hits `default:` →
  `NewLocalBackendWithEnv`, **with no warning** (the warn path only fires when a recognized
  backend fails detection). The correct spelling is `backend: container` + `runtime: podman`,
  which nothing validates or suggests. `/config/sandbox` reports the *configured* value, not the
  *selected* backend, and drops the `sandboxFallback`/`sandboxFallbackReason` the server already
  computes.
- **Fix sketch:** (a) validate at config load: alias `podman`/`docker`/`apple`/`wsl` →
  `backend: container` + `runtime: <name>` (matching `sandbox.ParseRuntimes` vocabulary), and
  hard-error on genuinely unknown values instead of silently running local; (b) make
  `GET /config/sandbox` report the **active** backend + runtime + `fallback_reason`, not the
  config echo; (c) refuse `auto_approve_exec: true` + effective-local-sandbox at startup unless
  an explicit `permission.allow_unsandboxed_auto_exec: true` (or similar) opt-out is set —
  today's WARN is not enough for a silent-privilege-escalation combo; (d) surface the existing
  `warnSandboxFallback` path in the web UI Security panel too (it already has scanner status
  plumbing from P15.6).
- **Acceptance:** `AEGIS_SANDBOX_BACKEND=podman` either works (aliased) or fails fast with a
  message naming the correct keys; `/config/sandbox` can never claim a container backend while
  exec is local; the auto-approve+local combo cannot arise without the explicit opt-out.
- **Tests:** unit tests for the alias/validation table; server test asserting
  `/config/sandbox` reflects `SelectSandbox`'s actual result including the fallback reason;
  startup-refusal test for the auto-approve+local combo.
- **Shipped implementation:** (a) `config.SandboxConfig.Normalize()` (internal/config/config.go),
  called from `config.Load()` and reused by the `PATCH /config/sandbox` handler, aliases
  `docker`/`podman`/`wsl`/`wslc`/`apple` → `backend: container` + the matching `runtime` (an
  explicit `runtime` already set is preserved) and hard-errors on any other unrecognized
  `backend` value naming the offending value and the correct keys; `SelectSandbox`
  (internal/server/server.go) also hardened its own `default:` case as defense-in-depth for any
  `SandboxConfig` built outside `config.Load()`, so an unrecognized backend is rejected there too
  instead of silently becoming local. (b) `api.ConfigSandboxResponse` gained
  `active_backend`/`fallback`/`fallback_reason`; both `/config/sandbox` handlers now report the
  daemon's actual `s.sandbox.Name()` and `s.sandboxFallback(Reason)` alongside the configured
  values, verified live (`AEGIS_SANDBOX_BACKEND=podman` with no podman installed correctly reports
  `active_backend: "local", fallback: true` with the underlying error as the reason). (c) new
  `permission.allow_unsandboxed_auto_exec` config key (default false); daemon startup now refuses
  to start (`unsandboxedAutoExecError` in server.go) when `auto_approve_exec: true` and the
  effective backend is local, unless the opt-out is set — verified live, including the opt-out
  downgrading back to a WARN. (d) web UI: `StatusInfo`/new `ConfigSandboxResponse` TS types gained
  the fallback fields; the sidebar's "Security check" button now shows a warning badge when
  `/status` reports `sandbox_fallback`, and the Security panel gained a read-only "Sandbox" tab
  (`SandboxSection` in SecurityPanel.tsx) showing configured vs. active backend and the fallback
  reason via `GET /config/sandbox`; frontend rebuilt and `dist/` committed.

**P25.3 — Output guard is counterproductive with local/thinking models. [Tier 1]** (M)

- **Symptom:** with the default `output_guard.enabled: true` + `mode: llm`, a correct answer
  from `qwen3.6:35b-a3b-deep` tripled turn time (26 s → 78 s): the guard verdict failed to
  parse ("guard reply did not contain a recognizable PASS/FAIL verdict"), fail-closed forced a
  corrective retry that **re-ran tools**, the retry's verdict failed to parse again, and the
  surfaced answer contained leaked meta-text — literally "**PASS.** The fix is confirmed
  working…" — because the retry turn saw guard feedback in context and answered the guard
  instead of the user.
- **Root cause:** three compounding issues. (1) `internal/guard/guard.go:209` —
  `strings.HasPrefix(upper, "PASS")`: thinking-style local models preface verdicts with
  reasoning, so a passing reply almost never *starts* with PASS (FAIL is matched anywhere,
  PASS only at position 0 — asymmetric). (2) The guard call runs on the **full session model**
  (the deep/thinking one); `Provider.SmallModel` exists and is already preferred for session
  titles (internal/server/sessions.go:810) and compaction (server.go:491–492) but not the
  guard. (3) The corrective retry appends to the same user-visible stream rather than replacing
  the failed answer, and its prompt framing lets verdict language leak into the final text.
- **Fix sketch:** route guard calls to `SmallModel` when set (a non-thinking model makes the
  strict "reply exactly PASS" contract actually satisfiable); strip reasoning/`<think>` blocks
  and/or parse the **last** non-empty line for the verdict before the fail-closed fallback
  (keep fail-closed as the final posture — the asymmetry, not the strictness, is the bug);
  make the retry replace, not append, the visible answer, and keep guard feedback out of any
  text surfaced to the user; consider defaulting `output_guard.enabled: false` (or
  `mode: schema`-only) when the provider base URL is a local endpoint, since a same-model
  rubric self-check adds full-model latency for little signal. Note `--first-init` writes
  `output_guard.enabled: true` into the Ollama-flavored global config — revisit that template
  alongside this.
- **Acceptance:** seeded-bug task with guard on: no unrecognizable-verdict warnings with the
  deep model, ≤ ~15 % latency overhead vs guard-off, and no PASS/FAIL/rubric language in the
  user-visible answer under forced retries.
- **Tests:** unit tests for verdict parsing on replies with reasoning preambles /
  verdict-on-last-line / genuinely ambiguous text (still fail-closed); engine test that a
  guard-retry turn replaces the visible answer; config test for SmallModel routing.

**P25.4 — Approval ergonomics: dead hotkeys, bad generated rules, read-only shell gating.
[Tier 1]** (M)

- **Symptom (a) — dead hotkey:** during the live TUI run the approval dialog's `y` hotkey went
  unresponsive for ~7 minutes of repeated presses (keystrokes were being swallowed, most likely
  by the "Steer the model" composer holding focus) while bare Enter confirmed instantly. No
  visual cue distinguishes dialog-focus from composer-focus.
- **Symptom (b) — generated Allow-always rules:** the dialog's suggested rules were either
  uselessly narrow — `allow shell(cd /private/tmp/…/demo-project && python3 temps.py*)` (never
  matches again once the command varies) — or dangerously broad — `allow shell(cat >*)`, which
  whitelists arbitrary file writes via shell redirection in one keypress.
- **Symptom (c) — read-only commands gated as execute:** because P25.1 pushed the model off the
  `read_file`/`ls` tools, plain `cat`/`ls`/`git status` shell calls each raised a full execute
  approval; six approvals for one bug-fix task is approval fatigue that trains users toward
  `auto_approve_exec: true` (see P25.2 for why that is dangerous today).
- **Fix sketch:** (a) route hotkeys reliably while a dialog is open (dialog takes key priority;
  visible focus indicator; hotkey echo when a key is consumed by the composer instead). (b)
  smarter rule generation: strip leading `cd <dir> &&` and env prefixes, key the suggestion on
  the actual binary + subcommand (`allow shell(python3 *)`, `allow shell(git status*)`), and
  never auto-suggest patterns containing shell-redirection/eval metacharacters (`>`, `|`,
  `$( )`) — require hand-written rules for those. (c) a small read-only shell classifier
  (allowlisted argv[0]+flags: `ls`, `cat`, `head`, `tail`, `wc`, `git status/log/diff` without
  `-c`/hooks, etc., rejecting redirections/pipes/command-substitution) that maps classified
  commands to the `read` capability path — auto-approved in build mode, still subject to
  deny rules and the plan-mode read gate.
- **Acceptance:** hotkeys always work (or visibly indicate why not) while a dialog is shown;
  no generated rule ever contains a redirection wildcard; the seeded-bug task in build mode
  with correct workspace root needs ≤ 2 approvals (the actual `python3` runs, or fewer with
  the classifier + a `python3` rule).
- **Tests:** TUI dialog focus/hotkey unit tests; table-driven tests for rule generation
  (cd-stripping, metacharacter refusal); classifier tests including bypass attempts
  (`cat f > /etc/x`, `git -c core.pager=sh log`, `ls; rm -rf /`).

**P25.5 — Token-usage observability for local providers. [Tier 1]** (S)

- **Symptom:** every API-driven run reported `done in=0 out=0` on the SSE `done` event, while
  the TUI status bar showed live counts (e.g. `in:10038 out:104`) for the same engine. Cost/
  budget features and the eval harness can't see usage over the API for Ollama runs.
- **Root cause (suspected, verify first):** the estimated-token path (`TokensEstimated` /
  character-length inference, api.go:138–140) feeds the TUI's per-turn display but the final
  `done` event's `InputTokens`/`OutputTokens` are only populated from provider-reported usage,
  which the Ollama OpenAI-compat endpoint doesn't always emit mid-stream. Trace where
  `KindDone` is assembled in the engine/server SSE bridge.
- **Fix sketch:** carry the same estimated counts (flagged `tokens_estimated: true`) onto the
  `done` event and into `SessionMeta` totals so API clients, cost tracking, and the web UI cost
  readout (P15.4) agree with the TUI.
- **Acceptance/tests:** harness summary shows non-zero in/out (with the estimated flag) for an
  Ollama run; engine/server test with a usage-less adapter asserting `done` carries estimates.

**P25.6 — Local-model profile: prompt weight + scope-creep guardrails. [Tier 1]** (S/M)

- **Symptom:** first model call carries ~10 k input tokens (system prompt + 19 exposed tool
  schemas + repo map + skills preamble) before the user says a word — heavy for 3B-active MoE
  models both in latency (prompt processing dominates short turns) and instruction-following
  (the web-search detour happened while flailing under P25.1, but nothing in the prompt
  discourages network tools for local file tasks). Separately, `qwen3coder:30b` over-delivered:
  unrequested try/except "robustness", an unrequested `fix-summary.md`, and an **unprompted
  `remember` call** that persisted to project memory — the system prompt never says "don't
  create files or memories the user didn't ask for".
- **Fix sketch:** (a) an opt-in "local" prompt profile (auto-suggested when base_url is
  localhost): trim system-prompt sections, lean harder on the existing `tool_search` deferral
  to cut always-exposed schemas (web_search/web_fetch, security_scan, git_pr, save_skill are
  deferral candidates), and skip the repo map above a size threshold. (b) add two short
  system-prompt rules for all profiles: prefer local file tools over network tools for
  file-scoped tasks; don't write files / call `remember` / add features beyond what was asked.
  (c) measure with the P25.7 harness: first-turn prompt tokens and time-to-first-tool-call,
  before vs after.
- **Acceptance:** first-turn input tokens for the fixture project measurably down (target
  ≥ 30 % under the local profile); qwen3coder run produces no unrequested files or `remember`
  calls; deep-model run still completes the fixture task in ≤ 5 tool calls.

**P25.7 — Promote the live-eval harness into `internal/eval`. [Tier 1]** (M)

- **Why:** everything above was found by *driving the running system*, not by unit tests — the
  existing `internal/eval` scenario tier uses a deterministic adapter (good for engine-loop
  regressions, blind to provider/daemon/sandbox integration), and the `live_eval` tier judges
  prompt/persona quality but not full tool-executing workflows. The gap is exactly where P25.1,
  P25.2, and P25.3 lived.
- **Fix sketch:** port [research/eval-harness-drive.py](eval-harness-drive.py) to Go as a
  `live_eval`-tagged test (or a new `live_workflow` tag): spin up a daemon in a temp fixture
  project (seeded-bug `temps.py`/`temps.csv`), run the fix-the-bug task via the HTTP API with
  auto-approve + container (or `os`) sandbox, and assert workflow-shape invariants rather than
  golden text: task completes; file actually fixed on disk; re-run tool call observed; **no**
  `web_search`/`find /`-style detours; tool-call count ≤ N; wall-time budget; non-zero token
  usage on `done` (P25.5); no guard meta-text ("PASS"/"FAIL:") in the final answer (P25.3).
  Wire into the nightly-eval workflow next to `TestLiveModelQuality` with a small pinned
  Ollama model; keep the Python script in research/ for ad-hoc use.
- **Acceptance:** nightly job runs it green on main; each P25.1/P25.2/P25.3 fix lands with the
  corresponding invariant enabled, so a regression in any of them fails the nightly.

---

## Open Work — P24 (Threat Model Findings — 2026-07-10)

Full-repo STRIDE-A threat model at commit `34aa687`:
[`threat-model-20260710-173718/`](../threat-model-20260710-173718/3-findings.md). 35 findings
total: 14 were "existing control" (already mitigated, verified, no action needed), 30 have shipped
as P24.1–P24.20/P24.22 across 2026-07-10/11 (P24.14 landed 2026-07-11 as commit `73880ae`; see
[releases.md](releases.md#latest-changes) for the earlier writeups), and 1 remains open — P24.21,
Tier 4 above.

---

## Open Work — P15 (Web UI Parity with the TUI)

**Complete as of 2026-07-11.** P15.1 (frontend architecture), P15.12 (token-injection hardening),
P15.2 (config-mutation endpoints), batches A (P15.3–P15.5, P15.10, `d8fc58e`), B (P15.8–P15.9,
`eb5a14c`), and C (P15.6–P15.7, `05ca71f`) have all shipped — see
[releases.md](releases.md#latest-changes). Nothing open under P15.

---

## Open Work — P22 (OpenAI Codex CLI evaluation — 2026-07-08)

Feature evaluation of Codex CLI. P22.1 (`/diff`), P22.2 (`/review`), P22.3 (Esc-Esc backtrack +
`/fork`), and P22.4 (Ctrl+R history search) have shipped.

**P22.5 — `/side` ephemeral side conversation. [Tier 4]** Quick side question in a throwaway
context. (S/M)

**P22.6 — Raw scrollback mode. [Tier 4]** Plain text rendering + release alternate screen for
native selection/scrollback. (S/M)

---

## Open Work — P20 (Odysseus Review: Research, Compare, Model Fit)

Feature evaluation of Odysseus self-hosted AI workspace. P20.1 (deep-research skill + `/research`
command) has shipped.

**P20.2 — Blind model compare. [Tier 4]** Same prompt to two models side-by-side, identities hidden
until vote, then reveal + optional synthesis. (M)

**P20.3 — Hardware-aware model recommendation. [Tier 4]** Detect hardware, curate/recommend local
models, offer `ollama pull`, surface via `/models` command. (M)

---

## Open Work — P13 (Security & Capability Enhancements)

P13.1, P13.2, P13.3.1, P13.3.5, P13.5, P13.6, and P13.8 have shipped. All remaining items are
Tier 4.

**P13.3.2 — `@shell`/`@last` context token. [Tier 4]** Extend `@file`/`@image:` to inject last N
lines of terminal output. (S)

**P13.3.3 — ACP `terminal/*` capability passthrough. [Tier 4]** Let ACP hosts (Zed) supply pty for
agent shell calls. (M/L)

**P13.4 — Nebula-inspired security engagement tooling. [Tier 4]** Engagement notebook +
`security_advise` tool + CVE lookup + status digest + guarded next-step suggestions. (M)

---

## Open Work — P9 (Engineering Quality — lower urgency, no current trigger)

P9.1, P9.2, and P9.5 have shipped. P9.3 and P9.6 were dropped.

**P9.4 — Per-task model routing. [Tier 4]** Pick cheaper model for simple turns, reserve expensive
for hard ones. No evidence of demand. (M)

---

## Open Work — P6 (Long-Horizon / Exploratory)

P6.2 (A2A protocol) and P6.3 (MCP server mode) have shipped/dropped.

**P6.1 — Mid-turn state persistence. [Tier 4]** Persist partial turn state (text, tool calls) to
SQLite during streaming. High complexity, low-probability failure mode. (L)

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
