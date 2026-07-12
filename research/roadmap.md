# Aegis Capability Roadmap

**Last updated:** 2026-07-11 (late night) — P25.1 (per-session working directory) and P25.2
(sandbox backend name trap) shipped (`6b76e5e`); writeups moved to
[releases.md](releases.md#latest-changes). A same-day roadmap review added **P25.8** (workdir
threading through spawn/cron/debate), Tier 2 **P26.1** (`aegis doctor`) and **P15.13** (web UI
workdir picker), and parked **P25.9**; P25.7's acceptance was reworked to an on-demand suite —
**no scheduled/nightly CI eval job, by decision.** Open: **Tier 1 P25.3–P25.8, Tier 2 P26.1 +
P15.13.**

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** Tier 1 — P25.3–P25.8 (P25.3–P25.7 from the local-model live-eval findings,
2026-07-11; P25.8 from the same-day roadmap review; P25.1 and P25.2 shipped the same day).
Tier 2 — P26.1 (`aegis doctor`) and P15.13 (web UI workdir picker), also from the roadmap
review. Tier 3 is empty. Tier 4 is the parked set — see
[Parked](#open-work--parked-tier-4).

**Next session:** work P25 top-to-bottom. P25.3 (output guard vs local/thinking models) is next —
3× turn latency and meta-text leakage into user-visible answers. P25.5 (S) is a standalone quick
win that can slot in anywhere; P25.7 lands late so each fix's invariant is regression-locked as it
ships (the eval suite is on-demand only — no scheduled CI job, per 2026-07-11 decision); P25.8
closes the remaining workdir seams after it. Re-run the harness (recipe in the P25 section) after
each fix to confirm the corresponding failure mode is gone — including one re-run against P25.1's
and P25.2's fixes, since the harness itself predates both. Tier 2 (P26.1, P15.13) follows once
Tier 1 is clear, or as breaks between larger items.

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1** (work in order; full detail in the [P25 section](#open-work--p25-local-model-live-evaluation--2026-07-11)):

1. **P25.3 — Output guard vs local/thinking models** (M) — 3× turn latency and meta-text leakage
   into user-visible answers.
2. **P25.4 — Approval ergonomics** (M) — dead `y` hotkey, useless-or-dangerous generated
   Allow-always rules, read-only shell commands gated as execute.
3. **P25.5 — Token-usage observability for local providers** (S).
4. **P25.6 — Local-model profile: prompt weight + scope-creep guardrails** (S/M).
5. **P25.7 — Promote the live-eval harness into `internal/eval`** (M) — deliberately late: it
   regression-locks all of the above (including P25.1 and P25.2). On-demand only; no scheduled
   CI job.
6. **P25.8 — Thread session workdir through the spawn/cron/debate seams** (S/M) — P25.1 residue:
   detached/subprocess sub-agents, cron jobs, and debate still run in the daemon root.

**Tier 2** (from the 2026-07-11 roadmap review):

- **P26.1 — `aegis doctor` preflight self-diagnostic** (M) — generalizes P25.2's
  configured-vs-active truth-telling to every silent-misconfiguration class the live eval hit.
- **P15.13 — Web UI: session workdir picker + display** (S/M) — without it, web sessions always
  fall back to the daemon root.

**Tier 3:** empty. The last items shipped 2026-07-11 (P24.20; P15 web-UI batches A/B/C; P24.14) —
see [releases.md](releases.md#latest-changes). Next trigger: a new threat-model pass, a reported
incident, or a new feature evaluation.

**Tier 4:** parked — P25.9, P24.21, P22.5, P22.6, P20.2, P20.3, P13.3.2, P13.3.3, P13.4, P9.4,
P6.1. See [Parked](#open-work--parked-tier-4).

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

**P25.1 (per-session working directory)** and **P25.2 (sandbox backend name trap + untruthful
`/config/sandbox`)** shipped 2026-07-11 (`6b76e5e`) — implementation writeups, including what
P25.1 deliberately deferred (per-session LSP/knowledge/repo-map scoping, `os`-backend write
confinement), are in [releases.md](releases.md#latest-changes).

### P25.3 — Output guard is counterproductive with local/thinking models

Priority: Tier 1 · Effort: M

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

### P25.4 — Approval ergonomics: dead hotkeys, bad generated rules, read-only shell gating

Priority: Tier 1 · Effort: M

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

### P25.5 — Token-usage observability for local providers

Priority: Tier 1 · Effort: S

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

### P25.6 — Local-model profile: prompt weight + scope-creep guardrails

Priority: Tier 1 · Effort: S/M

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

### P25.7 — Promote the live-eval harness into `internal/eval`

Priority: Tier 1 · Effort: M

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
  On-demand only, no scheduled CI job (deliberate — the nightly-eval workflow stays
  `workflow_dispatch`-only): document the single local command next to the existing `live_eval`
  tier in CLAUDE.md; keep the Python script in research/ for ad-hoc use.
- **Acceptance:** the suite runs green on demand against a local Ollama model via one documented
  `go test -tags live_eval ./internal/eval/... -run TestLiveWorkflow` command; each
  P25.1/P25.2/P25.3 fix lands with its corresponding invariant enabled; re-running the suite is
  the documented definition-of-done for changes touching the engine/server/sandbox/guard seams,
  so a regression in any locked behavior fails the suite on the next run.

### P25.8 — Thread session workdir through the spawn/cron/debate seams

Priority: Tier 1 · Effort: S/M

P25.1 residue at three seams that never received the per-session workdir (roadmap review,
2026-07-11):

- **Gap (a) — swarm sub-agents:** `subAgentRunner` (internal/server/server.go:812) builds
  sub-engines without `engine.Options.Workdir`. Foreground in-process spawns inherit the parent
  session's workdir only *accidentally* — the engine stamps `tool.WithWorkdir` onto the context
  only when its own workdir is non-empty (internal/engine/engine.go:909), so the parent's
  context value leaks through the spawn's context chain. Detached/background spawns (context
  deliberately severed — see the tracker-fallback comment at server.go:789) and the entire
  subprocess backend lose it: a teammate spawned from a session rooted in X silently operates in
  daemon root Y — the exact failure mode P25.1 fixed for top-level sessions.
- **Gap (b) — cron:** jobs always execute in the daemon's cwd (`cronShellRunner(sb, cwd)`,
  server.go:399); no per-job workdir.
- **Gap (c) — debate:** session-less; `debate.WithFiles` paths resolve against the
  daemon-rooted shared tools, so the web UI "stress-test a claim" panel grounds files in the
  wrong root for any non-default-workdir session.
- **Fix sketch:** add `Workdir` to `swarm.SpawnConfig`; the agent tool captures the spawning
  turn's workdir (`tool.WorkdirFromContext`) at spawn time and `subAgentRunner` sets
  `engine.Options.Workdir` explicitly — no reliance on accidental context inheritance — with the
  subprocess backend passing it to the worker (arg or env). Cron: optional per-job `workdir`
  field, default daemon cwd. Debate: accept a workdir on `DebateRequest` (web UI sends the
  session's), applied the same way messages.go threads it into `newEngine`.
- **Acceptance:** a teammate spawned from a session with Workdir X reads/writes in X under all
  three spawn shapes (in-process foreground, detached/background, subprocess backend); a cron
  job with `workdir` set runs there; debate `WithFiles` resolves against the request workdir.
- **Tests:** swarm spawn tests asserting workdir propagation across the three spawn shapes
  (the detached case is the regression that matters — it passes today only by accident);
  cron-runner workdir test; debate-handler test with a fixture file outside the daemon root.

---

## Open Work — P26 / P15 follow-ons (Tier 2)

From the 2026-07-11 roadmap review: cheap, self-contained items that generalize what the P25
batch exposed. Tackle after the Tier 1 set (or as breaks between larger P25 items).

### P26.1 — `aegis doctor` preflight self-diagnostic

Priority: Tier 2 · Effort: M

- **Why:** the whole P25 batch shares one root pattern — *the system was misconfigured and
  nothing said so* (sandbox silently local, workdir silently wrong, guard silently mangling
  answers, token counts silently zero). P25.2 fixed the sandbox instance of this class;
  `doctor` generalizes it into one place a user checks first.
- **Sketch:** `aegis doctor` prints pass/warn/fail per check: provider endpoint reachable and
  the configured model actually available (Ollama `/api/tags`); configured vs. **active**
  sandbox backend + runtime present (podman machine running?) with the fallback reason;
  scanner binaries installed; config validity (reuse `config.SandboxConfig.Normalize()` and
  the existing load-time validation); guard/SmallModel sanity (warn when `output_guard.mode:
  llm` targets a thinking model with no SmallModel — pairs with P25.3); workdir allowlist
  status; daemon reachable + version. Reuses existing plumbing throughout: `/status`,
  `/security/status`, `/config/sandbox`, `sandboxFallbackReason`. Nonzero exit on any fail so
  it can gate scripts.
- **Acceptance:** run against the two live-eval misconfigs — P25.2's (`backend: podman`, no
  podman running) and P25.1's (client cwd ≠ daemon root, no session workdir) — `doctor` names
  each problem and the correcting config key; clean setup exits 0.

### P15.13 — Web UI: session workdir picker + display

Priority: Tier 2 · Effort: S/M

- **Gap:** a browser has no filesystem cwd, so web sessions always fall back to the daemon
  root — P25.1's failure mode persists for exactly the audience the P15 track targeted.
- **Sketch:** the new-chat flow offers a directory choice fed by
  `server.session_workdir_allowlist` plus recent workdirs from the session store, sent as
  `Workdir` on create (the API already supports it, P25.1); show the active workdir in the
  chat header; surface the 400 validation message on a bad choice. Depends on nothing; pairs
  naturally with P25.8's debate-workdir request field.
- **Acceptance:** a web session created with workdir X executes tools in X; the header shows
  it; an invalid directory shows the server's error rather than silently falling back.

---

## Open Work — Parked (Tier 4)

Low urgency, no trigger, or explicitly parked pending demand. Do not build speculatively —
revisit only if a concrete trigger (user demand, reported pain, incident) appears, and check
with the user before starting any of these.

### P25.9 — Per-session scoping of daemon-singleton services

Priority: Tier 4 · Effort: L — parked, no concrete trigger

The P25.1 deliberately-deferred gaps, tracked here so they aren't lost in releases.md prose:
`lsp.Manager`, `knowledge.Store`, `longmem.Store`, the cached repo-map (`s.repoMap`),
persona/command/agent-def directory discovery, and the `os` sandbox backend's write-confinement
profile (baked at construction; `resolveSessionWorkdir` warns once on the mismatch) all remain
scoped to the daemon's default workspace regardless of a session's Workdir. Each is a
daemon-wide singleton; re-scoping is a materially larger change. Trigger: a concrete pain point
in a future live-eval pass.

### P24.21 — Memory-lock/zero the bearer token in `Client` process memory

Priority: Tier 4 · Effort: M — parked, no concrete trigger

FIND-33 (security, Low, CVSS 2.8) from the 2026-07-10 STRIDE-A threat model
([threat-model-20260710-173718/](../threat-model-20260710-173718/3-findings.md)) — the only one
of its 35 findings still open (14 were verified existing controls; the other 30 shipped as
P24.1–P24.20/P24.22). Explicitly low priority per the finding itself — host/OS access is
already a significant compromise.

### P22.5 — `/side` ephemeral side conversation

Priority: Tier 4 · Effort: S/M — parked, no concrete trigger

Quick side question in a throwaway context. From the Codex CLI evaluation (2026-07-08; P22.1–P22.4
shipped). Polish without demand.

### P22.6 — Raw scrollback mode

Priority: Tier 4 · Effort: S/M — parked, no concrete trigger

Plain-text rendering + release the alternate screen for native selection/scrollback. From the
Codex CLI evaluation. Polish without demand.

### P20.2 — Blind model compare

Priority: Tier 4 · Effort: M — parked, no concrete trigger

Same prompt to two models side-by-side, identities hidden until vote, then reveal + optional
synthesis. From the Odysseus review (P20.1 shipped). Competitive-inspired, no direct reported
pain.

### P20.3 — Hardware-aware model recommendation

Priority: Tier 4 · Effort: M — parked, no concrete trigger

Detect hardware, curate/recommend local models, offer `ollama pull`, surface via `/models`.
From the Odysseus review. Competitive-inspired, no direct reported pain.

### P13.3.2 — `@shell`/`@last` context token

Priority: Tier 4 · Effort: S — parked, no concrete trigger

Extend `@file`/`@image:` to inject the last N lines of terminal output.

### P13.3.3 — ACP `terminal/*` capability passthrough

Priority: Tier 4 · Effort: M/L — parked, no concrete trigger (deferred pending ACP-host usage)

Let ACP hosts (Zed) supply a pty for agent shell calls.

### P13.4 — Nebula-inspired security engagement tooling

Priority: Tier 4 · Effort: M — parked, no concrete trigger

Engagement notebook + `security_advise` tool + CVE lookup + status digest + guarded next-step
suggestions. "Interesting, not urgent" per its own scoping.

### P9.4 — Per-task model routing

Priority: Tier 4 · Effort: M — parked, no concrete trigger

Pick a cheaper model for simple turns, reserve the expensive one for hard ones. No evidence of
demand.

### P6.1 — Mid-turn state persistence

Priority: Tier 4 · Effort: L — parked, no concrete trigger

Persist partial turn state (text, tool calls) to SQLite during streaming. High complexity,
low-probability failure mode.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
