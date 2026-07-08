# Aegis Capability Roadmap

**Last updated:** 2026-07-08 (`/threat-model` framework picker SHIPPED same day — a follow-up
polish to P13.6: a recognized leading framework name, e.g. `/threat-model PASTA the auth service`,
skips the clarifying question; otherwise a picker dialog opens listing all six frameworks with
descriptions instead of spending a model turn asking in chat — see
[releases.md](releases.md#shipped--p13-items-security--capability-enhancements). Earlier the same
day: P23 SHIPPED — local-model context-window truth, from a field
failure where an Ollama-backed threat-model run silently lost its instructions to context truncation:
`internal/ollamainfo` detection of the *served* window via Ollama's native API, effective-window
reconciliation driving compaction/proactive-compaction/TUI bar/`/status`, visible `notice` events for
context fill + compaction + the max_iterations step cap, and incremental section-by-section
threat-model document writing with resume markers and an end-of-document review/debate round — see
[releases.md](releases.md#shipped--p23-items-local-model-context-window-truth--long-run-survivability).
Earlier the same day: P22.1 + P22.4 + P22.2 SHIPPED from the same-day Codex CLI evaluation:
a no-model-turn `/diff [--staged] [path]` command showing the working-tree git diff including
untracked files, chroma-highlighted via a new `highlightUnifiedDiff`; Ctrl+R input-history search (a
filterable, newest-first picker recalling a past sent message onto the input line; Ctrl+R was
already bound to the session switcher, which moved to Ctrl+Y to make room); and `/review
[--staged | <branch|commit>]`, a read-only review flow that inlines the resolved diff into a
prompt loading the already-shipped `content-review` skill and switches the session to plan mode for
the duration — see
[releases.md](releases.md#shipped--p22-items-openai-codex-cli-evaluation-diff-ctrlr-history-search-review).
P22.3 (Esc-Esc backtrack/`/fork`), P22.5 (`/side`), and P22.6 (raw
scrollback) remain open. Earlier the same day: P21.7 intent-based scroll follow SHIPPED — the
still-reported "viewport doesn't follow streamed text" bug post-P18.3/P21.1: every typed key was
also forwarded to the transcript's vi scroll bindings and the geometry-derived `followBottom`
catch-all then killed auto-follow; follow is now explicit user intent. Also same day: P22 added —
OpenAI Codex CLI feature evaluation; six items scoped, the rest of Codex's surface confirmed already
covered or rejected.
Earlier, 2026-07-07: P21 added — fresh-eyes code review of the running app, roadmap/releases
deliberately ignored; found the TUI's "rough feel" root cause — per-token markdown re-render instead
of per-frame — plus daemon-robustness and web-UI-token findings. P21.1 stream-delta coalescing +
P21.4 render-cost regression bench SHIPPED the same day (buffered SSE channel + drain-batched
`waitForEvent` → one markdown render per frame instead of per token); P21.2/P21.3/P21.5/P21.6 scoped.
The `/ui` token-exposure finding folded into P15 as P15.12.
Earlier the same day: P20.1 shipped — `deep-research` builtin skill (structured
plan → search → read → synthesize rounds, source-quality bar, findings log + analyzed-URLs audit
trail, cited report) plus `/research` TUI command — see
[releases.md](releases.md#shipped--p20-items-odysseus-review-research-compare-model-fit).
Earlier the same day: P18 shipped, all three items — see [releases.md](releases.md#shipped
--p18-items-tui-streaming--scroll-polish); P18.1 resolved as a documented decision (no code change),
P18.2 fixed an O(n)-in-scroll-depth scrollbar/offset computation down to O(1), P18.3 fixed the
auto-follow re-arm bug, done in parallel via three isolated git worktrees then merged. Earlier the
same day: P20 added — evaluation of the Odysseus self-hosted AI workspace
(github.com/pewdiepie-archdaemon/odysseus); three capabilities scoped, most of its surface
explicitly rejected as out of scope, not started. Earlier: P19 shipped, both items — see [releases.md](releases.md#shipped--p19
-items-docs--session-command-misc). Earlier the same day: P13.3.1 + P13.3.5
shipped — shell-aware "diagnose this?" error assist for the embedded terminal pane and `!`
bang commands, plus a `tui.keybindings` config remap with startup validation and help-text sync;
P13.3.2/P13.3.3 deliberately left open as the lower-value remainder. Also shipped the same day:
P13.7 — `latex-report` builtin skill + `/report` command, closing out the last P13 item with a real
gap; P16.9 — in-terminal half-block image thumbnails, closing out the P16 TUI polish & interaction
parity track from the crush/opencode/Claude Code gap analysis)

This document tracks only **open** work — what's next. For shipped-feature history and full design
rationale behind completed items, see [releases.md](releases.md).

---

## Status

Open items: **P22** (2026-07-08 Codex CLI evaluation — `/review`, Esc-Esc
backtrack/`/fork`, `/side`, raw scrollback; `/diff` and Ctrl+R history search SHIPPED 2026-07-08),
**P21** (2026-07-07 fresh-eyes
review — P21.1 TUI stream-delta coalescing + P21.4 render-cost bench SHIPPED 2026-07-07, P21.7
intent-based scroll follow SHIPPED 2026-07-08; P21.2 tool-call cards, P21.3 streaming caret, P21.5
daemon resource ceilings, P21.6 MCP output trust remain scoped), **P15.2–P15.12** (web UI
parity with the TUI — P15.1's architecture question is resolved and the frontend scaffold/faithful
-port shipped 2026-07-06, see below; P15.12 added 2026-07-07 for the `/ui` token-exposure finding),
**P13** (P13.3 terminal enhancements, P13.4 nebula-inspired
engagement tooling), **P20** (P20.2 blind model compare, P20.3 hardware-aware model
recommendation — P20.1 deep-research shipped 2026-07-07), **P9.4** (per-task model
routing), **P6.1** (mid-turn state persistence).

Everything else — P2–P5, P7, P8, P9.1/P9.2/P9.5, the 2026-07-03 architecture/security review's
15-item punch list, P10, P11, P12, P13.1/P13.2/P13.5/P13.6/P13.7/P13.8, P14 (all of P14.1–P14.10), the
TQ TUI track, P15.1, P16 (all of P16.1–P16.9), P17 (all of P17.1–P17.5), P18 (all three items), P19
(both items), P20.1, P23 (all three items), and the 2026-07-06 fable-review.md remediation
(CI/CodeQL/Dependabot, Windows token ACL, compiler-enforced daily-cap guard, `aegis harden`,
plan-mode network gating, release workflow, server/TUI file splits, fuzz coverage, live-model eval
tier, script-aware token estimation) — is shipped. See [releases.md](releases.md) for what each
shipped and why.

**P15.1 resolved 2026-07-06:** bundled frontend (Vite + Preact + TypeScript), building to a static
bundle committed at `internal/server/webui/dist/` and embedded via `go:embed` — `aegis ui` stays
one self-contained binary, no separate frontend server, no Node.js needed for `go build`/`go run`.
The scaffold plus a faithful 1:1 port of the prior single-file page's feature set (session
list/create, transcript hydration, streaming turn, tool-call approval, stop/abort, phase indicator)
shipped the same session — no new features yet. **P15.2–P15.11 are unblocked but not started.**
P13's remaining sub-items are also researched and scoped (2026-07-05) but not started. P9.4 and
P6.1 are real but have no concrete trigger — don't build them speculatively; check with the user
first. (P6.5, "desktop/IDE surface beyond ACP," previously covered this exact web-UI question and
concluded "only worth it if user demand materializes... the TUI is the product" — that demand is
now explicit, so P6.5 is superseded by P15 rather than still open on its own.)

---

## Open Work — P15 (Web UI Parity with the TUI)

Requested 2026-07-06: bring `aegis ui` up to the TUI's feature depth (~10,600 lines across
`internal/tui`, 39 slash commands), so it can be a complete, self-sufficient way to run Aegis for a
user who doesn't want a terminal — flagged as an "order of magnitude behind the TUI" gap in
[fable-review.md](fable-review.md). **P15.1 is resolved and its scaffold shipped 2026-07-06** (see
below); `aegis ui` is now built from `internal/server/webui/frontend` (Vite + Preact + TypeScript),
with the built bundle committed at `internal/server/webui/dist/` and embedded via `go:embed`
(`internal/server/webui.go`). That session ported the prior single-file page's exact feature set
1:1 — no new panels yet. **P15.2–P15.11 below are unblocked but not started.**

**The key architectural finding driving this scoping:** a large slice of what the TUI does isn't
actually daemon-API-backed at all — `/skills enable`, `/sandbox use`, `/security-config`, `/security
install`, and the new `aegis harden` all run as **direct, in-process file reads/writes**
(`config.Load()` / `config.Patch*` against local YAML) from inside the TUI process, because the TUI
*is* a local Go program with filesystem access. A browser tab has none of that — it can only ever
reach these features through an HTTP endpoint the daemon exposes, and today none exists for any of
them. So P15 is not purely a frontend exercise: a real slice of it is new daemon API surface,
mirroring functions that already exist in `internal/config`/`internal/security`/`internal/skills`
but have only ever been called from `internal/cli` and `internal/tui`, never `internal/server`.
Everything under "session/turn" (create/list/get/patch/message/approve/steer/checkpoints/rewind/
archive/prune/background/runs/teammates/memory/debate/knowledge/repomap/scan) already has an HTTP
endpoint (`internal/server/*.go`) — those items below are frontend-only.

- **P15.1 — SHIPPED 2026-07-06 — Frontend architecture: bundled Vite + Preact + TypeScript.**
  Resolved in favor of moving off the old dependency-free single-file page (inline CSS/JS, no build
  step) to a small bundled frontend, per user decision. Source lives at
  `internal/server/webui/frontend/`; `npm run build` there produces
  `internal/server/webui/dist/` (index.html shell + hashed `assets/*.js`/`*.css`), which is
  **committed to git** (not gitignored) so `go build ./...`/`go run ./cmd/aegis` need no Node.js —
  only editing frontend source requires rebuilding it. `internal/server/webui.go` embeds `dist/` via
  `embed.FS` (`fs.Sub`-rooted), serving the HTML shell (token-injected, same `__AEGIS_TOKEN__`
  `strings.Replace` mechanism as before) at `GET /ui` and the hashed assets at `GET /ui/assets/`
  (`http.FileServerFS`, long immutable cache — safe since filenames are content-hashed). CSP
  tightened as a side effect: `script-src`/`style-src` dropped `'unsafe-inline'` since bundled JS/CSS
  are external same-origin files, not inline. A CI step (`ci.yml`, ubuntu leg only) rebuilds the
  frontend and diffs `dist/` to catch drift where source changed but the committed build didn't.
  The same session ported the prior page's exact feature set 1:1 onto the new stack (session
  list/create, transcript hydration, streaming turn via SSE, tool-call approval, stop/abort, phase
  indicator) — deliberately no new features, so P15.2–P15.11 below remain exactly as scoped.
- **P15.2 — New daemon config-mutation endpoints.** The concrete backend gap identified above:
  `GET/PATCH /config/sandbox`, `GET/PATCH /config/security` (egress-then-write, network allowlist,
  per-scanner enable/method/image), `GET/PATCH /config/skills` (builtin enable/disable),
  `POST /config/harden` (the new `aegis harden` profile), `GET /security/status`,
  `GET /security/baseline`, `POST /security/install`. Each mirrors an existing `internal/cli`/
  `internal/tui` function almost directly — the resolution logic already exists, this is packaging
  it as HTTP. Needs the same auth/loopback posture the rest of the API already has. (M)
- **P15.3 — Persona, mode, and model management panel.** Persona list/switch (`GET /personas`,
  `PATCH /sessions/{id}`) and per-session model override (same `PATCH`) are already fully
  API-backed — this is a frontend-only picker UI, no new endpoint. (S/M)
- **P15.4 — Cost/token visibility and budget alerts.** Session cost/tokens are already in
  `SessionMeta`; daily cost/tokens are in `GET /status`; the SSE stream already carries
  `KindCostAlert` events (`alertOnCostThreshold`/`alertOnTokenThreshold`) the current page doesn't
  render at all. Frontend-only: a persistent cost/token readout plus a toast/banner on alert events. (S)
- **P15.5 — Checkpoints & rewind UI.** `GET .../checkpoints` and `POST .../rewind` exist; the page
  has no checkpoint list or rewind affordance at all today. Frontend-only. (S/M)
- **P15.6 — Security scanning & baseline surface.** `POST /security/scan` already exists and is
  usable today (no frontend for it); status/baseline/install need P15.2's new endpoints first. Needs
  a findings table (severity/tool/location/remediation — `Finding` is already a stable shape) rather
  than the TUI's plain-text tabwriter output. (M, depends on P15.2 for the status/baseline/install
  half)
- **P15.7 — Skills & memory management.** `GET/POST /memory` exists (view + append project/user
  memory); skills enable/disable needs P15.2. Frontend: a memory viewer/editor and a skills toggle
  list. (S/M, partially depends on P15.2)
- **P15.8 — Debate & knowledge base UI.** `POST /debate` and `POST /knowledge` (index/query) both
  exist with no frontend today. Frontend-only: a claim-submission form + transcript view for debate,
  a search box + results list for knowledge. (M)
- **P15.9 — Multi-session lifecycle UX.** Archive/unarchive/prune, background (detached) sessions
  and their buffered events, in-flight run list (`/runs`), and teammate/sub-agent status
  (`/teammates`) all have endpoints; the current page only ever shows one active session with no
  archive view, no background-session indicator, and no sub-agent visibility. Frontend-only. (M)
- **P15.10 — Approval UX: persist "always allow."** The TUI's approval dialog can persist a
  pattern-scoped allow rule (`addPermissionRule`, wired through `sseApprover`/`ApproveRequest.
  AllowAlways`+`.Pattern` — already in the API type); the current page's approval box only ever
  sends `approved: true/false` for the one call. Frontend-only: add the "always allow this" checkbox
  and pattern input the API already accepts. (S)
- **P15.12 — Harden the `/ui` token-injection mechanism (2026-07-07 review finding).** `GET /ui`
  (`handleWebUI`, `internal/server/webui.go`) is exempt from `authMiddleware` and injects the raw,
  long-lived daemon auth token straight into the HTML shell (`__AEGIS_TOKEN__` `strings.Replace`).
  The origin guard (`originMiddleware`) only rejects requests that *carry* a non-loopback `Origin`
  header — a direct `GET /ui` from any local process (no Origin header at all) returns the daemon
  token in cleartext. On a single-user host this sits within the current threat model, but on a
  shared/multi-user machine any local account that can reach the loopback port harvests full API
  access, and the token is the *same* secret every other client uses for the lifetime of the daemon.
  Tighten to a short-lived, single-use page token minted per `/ui` load (exchanged once for the real
  session, then invalidated) rather than handing out the daemon's master secret, so a leaked page
  source can't be replayed. Keep the loopback-only posture as the outer boundary. (S/M, security) —
  note: this is a change to P15.1's existing injection mechanism, not new panel work, so it can land
  independently of P15.2–P15.11.
- **P15.11 — Non-technical-user framing, not just feature parity.** The TUI is intentionally
  information-dense (terse slash commands, raw persona names, tabwriter tables) for a power-user
  audience; "bring the web UI to TUI depth" and "make it usable for less-technical users" pull in
  different directions if P15.3–P15.10 are implemented as a literal port of TUI surface. This item
  is the explicit reminder to design each panel above for a non-technical audience (plain-language
  labels over persona ids, guided/confirmed flows over raw config editors, sensible defaults
  surfaced instead of every knob) rather than just recreating the TUI's own UI in HTML. No fixed
  scope — apply as a design lens across P15.3–P15.10, and revisit once P15.1 is decided. (cross-
  cutting, not independently sized)

**Suggested sequencing:** ~~P15.1 (decide)~~ done → P15.2 (backend gap, unblocks the config-heavy
items) → P15.3/P15.4/P15.5/P15.8/P15.9/P15.10 (frontend-only, can proceed in parallel now that
P15.1 has landed) → P15.6/P15.7 (need P15.2). Priority: **High** (explicit user request,
2026-07-06). Effort: **XL**
overall — this is a larger initiative than any single prior track (P11's security-scanning buildout
is the closest comparison in scope) and should be its own multi-session effort, not attempted in one
sitting.

---

## Open Work — P13 (Security & Capability Enhancements)

Researched 2026-07-05 (five items via background review of a named external project/methodology,
two via direct codebase audit). P13.1, P13.2, P13.5, P13.6, and P13.8 shipped — see
[releases.md](releases.md#shipped--p13-items-security--capability-enhancements). The three below are
scoped proposals, not started.

### P13.3 — Terminal enhancements (Microsoft Intelligent Terminal review)

Researched: Intelligent Terminal is Microsoft's experimental ACP-native fork of Windows Terminal
(a terminal *emulator*, not a standalone tool) — docked Agent Pane, agent status bar,
command-failure diagnosis with fix suggestions, a context-injecting command palette, and session
management. Most of its UX ideas are already shipped in Aegis's TQ track (docked terminal pane,
sidebar status, session picker/resume, theming, streaming). Aegis's embedded terminal pane already
shells out via plain `os/exec` (no pty library), so it's already fully cross-platform — that
constrains what's easy to add without introducing OS-specific pty code.

Genuinely new, worth adding:

- **P13.3.1** — Shell-aware error assist: on a non-zero exit from the `shell` tool or the embedded
  terminal pane, offer an inline "diagnose this?" affordance that pipes stderr+exit code to the
  model on request. Cross-platform (exit-code capture only). (S/M) — **shipped 2026-07-07**: scoped
  to the two surfaces where the model has zero automatic visibility — the embedded terminal pane
  and `!` bang commands (a `shell`-tool failure already flows back to the model on its next turn,
  so it needed no bridge). `ctrl+g` (remappable via P13.3.5) sends the failed command + output as a
  new turn asking the model to diagnose and fix it; the terminal pane's status line and the bang-
  command transcript entry both surface the hint when a command fails.
- **P13.3.2** — `@shell`/`@last` context token: extend the existing `@file`/`@image:`
  attachment-token parser to inject the last N lines of embedded-terminal output into the next
  prompt. (S) — TUI-surface work; build under the same command-surface conventions P14 established.
  Narrower now that P13.3.1 covers the dominant failure-diagnosis case; mainly useful for
  referencing successful manual-terminal output.
- **P13.3.3** — ACP `terminal/*` capability passthrough: `internal/acp` implements
  session/prompt/permission methods but not the optional ACP terminal capability
  (`terminal/create`, `/output`, `/wait_for_exit`, `/kill`, `/release`). Implementing it lets an
  ACP host (Zed, a future Intelligent-Terminal-as-client) supply its own pty for agent shell calls
  — live visibility/Ctrl+C control on the host side — falling back to Aegis's native exec path
  when the host doesn't advertise the capability. The one item requiring real ACP protocol work;
  everything else here is TUI-only. (M/L) — lowest-leverage remaining item: real ACP protocol work
  for an audience narrower than the primary TUI users; defer until there's evidence of ACP-host
  usage.
- **P13.3.4** — Background-task attention indicator: extend the existing sidebar agent-count
  display to flag a failed background sub-agent/cron job. (S) — **subsumed by P16.1** (2026-07-07):
  route these events through the P16.1 notification seam instead of building a sidebar-only
  affordance; don't implement separately.
- **P13.3.5** — Configurable keybinding remap: `internal/tui/keymap.go` is fully hardcoded; add a
  `tui.keybindings` config section. Trivial cross-platform (bubbles/key is already OS-agnostic). (S)
  — **shipped 2026-07-07**: `tui.keybindings` config map (action name -> one or more bubbles/key
  sequences), validated at TUI startup (unknown action name is a hard error, not a silent no-op);
  overriding a binding regenerates its help label too, so the F1 overlay and `/help` both show the
  real bound key, not the hardcoded default.

Remaining: P13.3.2 (S), P13.3.3 (M/L). No blocker between them.

### P13.4 — Nebula (berylliumsec/nebula) AI-pentesting review

Researched and identified: github.com/berylliumsec/nebula (~500 stars, PyPI `nebula-ai`) —
confirmed as the correct LLM-driven pentesting project (not the unrelated Slack/Defined-Networking
VPN mesh tool of the same name). Its OSS core is an *advisory copilot*, not an autonomous attack
engine: `!`-prefixed LLM queries against on-screen terminal output, AI-assisted categorized
note-taking, real-time next-step suggestions, and ingestion of external recon-tool output — no
exploit chaining, no autonomous target execution, no report generation in the free tier. A paid
"Nebula Pro" tier claims an undocumented "autonomous mode"; public docs give no technical detail,
and this should **not** be a model for anything Aegis adopts.

Genuinely new pattern worth taking: a persistent, session-spanning "engagement notebook" (distinct
from a single scan `Report`) and an advisory flow that ingests arbitrary external tool output
(nmap, nikto, gobuster — none of which Aegis wraps) and reasons about next steps.

- **P13.4.1** — Security engagement notebook: persistent structured notes/findings ledger spanning
  a multi-day review (extends `internal/memory`). (M)
- **P13.4.2** — `security_advise` tool: ingest pasted external recon-tool output, return
  AI-suggested next steps, map into the `Finding` model where possible. (M)
- **P13.4.3** — CVE/exploit-context lookup tool: scoped lookup feeding real citations to the P12
  debate proposer/critic roles instead of relying on model recall. (S)
- **P13.4.4** — Engagement status digest: summary of recent scan/session activity (finding counts,
  deltas, open items). (S) — fold into the existing `/status` command's output rather than adding a
  separate one.
- **P13.4.5** — Guarded "suggest next action" layer on top of `RunDAST`: proposes manual next-test
  steps post-scan, never auto-executed, reusing the exact `allow_active`/`allowed_targets` gate —
  explicitly excludes autonomous exploit chaining. (M)

TUI surface requirement: P13.4.1 (notebook) and P13.4.4 (status digest) each need a slash surface
(`/notebook`, folded into `/status`).

Priority: Low (interesting, not urgent), Effort: M overall. P13.4.5 must not adopt Nebula Pro's
undocumented autonomous-mode pattern.

### P13 cross-cutting requirement — every new capability must ship its in-session TUI surface

A recurring failure mode found during the 2026-07-05 review and worth keeping in mind for every
item above: a capability ships as a *tool* (model-callable) and a *CLI subcommand*, but never gets
an in-session `/slash` command — so a user driving the TUI can't reach it, and it feels absent.
Each open P13 item above already carries its own TUI-surface requirement inline; the general rule
going forward is that no P13 capability is "done" until it's reachable from the TUI and covered by
the P14.1/P14.10 command-surface sync test (`TestBuiltinCommandsCoverDispatchTable`,
`TestCommandDefsWellFormed`). This is a requirement addition, not new scope.

---

## Open Work — P20 (Odysseus Review: Research, Compare, Model Fit)

Researched 2026-07-07 from github.com/pewdiepie-archdaemon/odysseus (~81k stars, Python/Flask-style
backend + vanilla JS frontend) — a self-hosted AI *workspace*: chat + agents (tools, MCP, files,
shell, skills, memory), deep research, blind model comparison, a hardware-aware model "cookbook",
plus a documents editor, IMAP/SMTP email triage, notes/tasks/calendar with CalDAV, gallery/image
editing, TTS/STT. Its chat-agent core (tools/MCP/skills/memory/sessions/themes) is a Python
re-tread of what Aegis already has, and its workspace surface (email, calendar, documents) is out
of scope — but three capabilities fill real Aegis gaps, and two of them are natural web UI panels
that align with the P15 direction. **License constraint: Odysseus is AGPL-3.0 — everything below
is concept-level reimplementation in Go; no code, prompt, or asset reuse.**

- **P20.1 — SHIPPED 2026-07-07** — deep-research workflow, built skill-first as scoped: new
  `deep-research` embedded builtin skill (structured plan → search → select → read → record
  rounds capped at 8, a source-quality bar, a findings log with an analyzed-URLs audit trail,
  numbered-citation discipline, and a structured final report) plus a `/research` TUI command —
  see [releases.md](releases.md#shipped--p20-items-odysseus-review-research-compare-model-fit).
  The escalation path stays open as scoped: promote to an engine-level workflow only if
  skill-driven runs prove insufficient; a web UI research panel folds into P15 later.
- **P20.2 — Blind model compare.** Same prompt sent to two models side-by-side, identities hidden
  until the user votes left/right/tie, then reveal + optional synthesis of both answers. Directly
  useful to Aegis's local-model audience — "is qwen3.5 or llama3.2 better at my codebase", "did
  this persona/prompt change help", "is the cloud model worth it over local" — and Aegis already
  has the entire backend: multi-provider adapters, per-session model override, ephemeral sessions,
  independent SSE streams. Design lesson worth stealing from their implementation: blinding leaks
  easily — comparison sessions must be neutrally named (`[CMP] Model A/B`) because session
  lists/sidebars de-anonymize the pair before the vote; store the blind mapping server-side and
  return real model names only after the vote is recorded. Scope: `POST /compare` daemon endpoint
  (two ephemeral sessions, two streams, vote/reveal/optional-synthesis), web UI side-by-side panel
  as the primary surface (fits P15; the P15.1 scaffold makes this buildable now), TUI `/compare`
  with sequential A-then-B display as the required in-session surface. (M)
- **P20.3 — Hardware-aware model recommendation ("cookbook-lite").** Odysseus's `services/hwfit`
  detects hardware (RAM/GPU/VRAM via profiles), discovers candidate models from Hugging Face, and
  recommends what actually fits, feeding a download-and-serve flow. Aegis is explicitly
  local-model-first (Ollama) yet offers zero fit guidance — first-run setup assumes the user
  already knows which model their machine can run. Scope: cross-platform CPU/RAM/GPU/VRAM
  detection (the existing sandbox runtime-detection pattern is the precedent for probing host
  capability), a small curated table of coding-agent-suitable local models with memory footprints
  and quantization variants (curated beats live HF discovery — Aegis cares about the ~dozen models
  good at tool use, not all of HF), recommend + offer `ollama pull`, surfaced as `aegis models
  recommend`, a `--first-init` step, and a `/models` TUI info command. Explicitly **not** a
  serving stack — Ollama serves; Aegis only advises and pulls. (M)
- **P20.4 — Not adopting (recorded so it isn't re-litigated).** Documents editor, email
  (IMAP/SMTP triage/drafts), notes/tasks/calendar/CalDAV, gallery/image editing, TTS/STT,
  contacts/faces/YouTube ingestion: workspace/personal-assistant features that would dilute
  Aegis's coding + security agent focus and drag in heavy non-goal dependencies. Themes, presets,
  sessions, scheduled tasks, 2FA: already covered by P16.7 themes, personas, the session store,
  `internal/cron`, and token auth + `aegis harden`. If the web UI is ever deliberately exposed
  beyond loopback, revisit auth hardening as its own item on Aegis's threat model, not by
  borrowing Odysseus's multi-user account design.

**Suggested sequencing:** independent items, no ordering constraint between the two remaining
(P20.1 shipped 2026-07-07). P20.2 first if the web UI track is active (clearest immediate utility,
exercises the P15.1 scaffold on a real new panel); P20.3 needs no web UI at all. The P13
cross-cutting rule applies: each item ships its in-session TUI surface (`/compare`, `/models` —
P20.1's `/research` shipped) and is covered by the P14 command-surface sync tests. Priority:
**Low-Medium** (competitive-inspired, no direct user pain behind it). Effort: **M** per item.

---

## Open Work — P21 (Fresh-Eyes Code Review — 2026-07-07)

A from-scratch review of the running application — engine loop, permission gate, daemon auth/HTTP
surface, swarm subprocess, and the full TUI streaming/render path — done deliberately *without*
reference to the roadmap or releases, to catch what a checklist re-verification structurally can't
(cf. the 2026-07-03 fresh-eyes review). Overall finding: the backend is sound (per-turn budget
gates, capability-based tool serialization, panic recovery in tool goroutines, loopback bind +
constant-time token compare + DNS-rebinding origin guard + Windows token ACLs). The issues are
concentrated in the TUI render pipeline and two daemon-robustness gaps.

**Root cause of the "TUI feels rough vs crush/Claude Code" complaint (P21.1):** the render pipeline
does expensive per-*token* work where the polished TUIs do per-*frame* work. `waitForEvent`
(`internal/tui/tui.go`) pulls exactly one SSE event per Bubbletea `Update` cycle; each streamed
token (`eventMsg` → `applyEvent` → `refresh()`) re-runs glamour markdown over the growing live tail
(`liveBlock.render` → `md(b.raw[boundary:])`, `internal/tui/transcript.go`) and recomputes scroll
layout (`SetTail`+`GotoBottom`), on every token. Bubbletea throttles *painting* to ~60fps, but this
work runs inside `Update`, upstream of that throttle, so it executes at token rate. The
boundary-cache only settles the prefix at blank lines, so inside a long paragraph the whole current
paragraph is re-parsed per token. That is the micro-jitter, input latency, and CPU spin.

- **P21.1 — Stream-delta coalescing — SHIPPED 2026-07-07.** Decoupled ingest rate from render rate
  so glamour/layout work is bounded by frame rate, not token rate. Two-part fix: (1) the client's
  SSE channel is now buffered (`make(chan api.Event, 256)`, `internal/client/client.go`) so the
  parser goroutine runs ahead of the render loop — an unbuffered channel meant at most one event was
  ever ready, so draining alone would have done nothing; (2) `waitForEvent` (`internal/tui/tui.go`)
  now blocks for the first event then non-blockingly drains everything else buffered into one
  `batchEventMsg` (capped at `maxEventsPerBatch = 512` so a fast stream still yields to input/paint),
  and the new `applyStreamBatch` applies the whole batch with a single `refresh()`. A close observed
  mid-drain folds into the batch (`closed` flag) and re-uses the existing `streamClosedMsg` teardown.
  The single-event `eventMsg` path is retained (direct test drivers) and funnels through the same
  helper, so follow-bottom/notify bookkeeping is identical. Verified: `go test`/`-race`/`vet`/build
  all clean.
- **P21.1a — Follow-up (open, only if needed):** frame-clock `refresh()` on a ticker so even a
  single unbroken burst larger than one drain is rendered at most once per frame. Not built —
  channel buffering + drain already collapses the common case; revisit only if profiling shows
  residual per-token cost on very fast local models.
- **P21.2 — Tool-call cards (in-place updating block).** Today a tool call and its result are two
  separately-appended transcript items (`renderToolCall` then `renderToolResult`, keyed by
  call/result ordering per tool name). Claude Code renders a tool invocation as one coherent block
  that updates in place (pending → ok/err) — that in-place model is what keeps a tool-heavy turn
  from reading as noise. Restructure the two appends into a single addressable, updatable transcript
  item. Depends on nothing; complements P21.1. (M)
- **P21.3 — Streaming caret.** The live tail shows a spinner phrase but no steady text cursor at the
  write head. A blinking block caret at the end of the streaming text reads as "alive" rather than
  "redrawing" and is a large share of the perceived-polish gap for a cheap change. (S)
- **P21.4 — Render-cost regression bench — SHIPPED 2026-07-07 (with P21.1).**
  `internal/tui/streaming_coalesce_test.go` locks in the fix: `TestWaitForEventCoalescesBufferedTokens`
  asserts 200 buffered tokens collapse into exactly one batch (one refresh = one render — reverting
  to one-event-per-msg makes it fail), `TestWaitForEventRespectsBatchCap` proves the 512 cap yields
  control back, `TestBatchEventMsgEquivalentToSequential` proves the coalesced path renders
  byte-identically to the per-token path, and `TestBatchEventMsgClosedTearsDownStream` covers the
  mid-drain close.
- **P21.5 — Daemon resource ceilings.** `sessionSems` caps runs to one-per-session, but there is no
  global cap on total concurrent sessions/runs and no bound on SSE buffer growth. This matters more
  now that `aegis mcp-serve` (`internal/mcpserver`) exposes sessions to other MCP-speaking harnesses
  — a misbehaving or hostile MCP client could fan out sessions unbounded and exhaust the host. Add a
  configurable max-concurrent-runs and an optional per-run wall-clock ceiling; keep the loopback
  boundary as the outer guard. (S/M, security/robustness)
- **P21.6 — MCP tool output trust boundary.** MCP tools are capability-gated (default `execute`, the
  most restrictive — P7.1) but their *output* flows back into the model context unfiltered, so a
  compromised or malicious MCP server is a prompt-injection vector with no guardrail. Document the
  trust assumption in `docs/` and consider an opt-in output scan / provenance marker for MCP sources
  the user hasn't explicitly trusted. (S, security — lower urgency, no reported incident)
- **P21.7 — Intent-based scroll follow — SHIPPED 2026-07-08.** Root cause of the still-reported
  "viewport doesn't follow streamed content, I have to scroll to find it" complaint (post-P18.3,
  post-P21.1): `followBottom` was re-derived from *geometry* (`AtBottom()`) in Update's catch-all on
  every fall-through message, while every KeyMsg was also forwarded to `transcriptPane.HandleKey`'s
  vi-style scroll bindings (the P16.4 "known existing quirk"). Net effect: typing any `u`/`k`/`b`/
  space/arrow-key while a response streamed both edited the draft AND scrolled the transcript, and
  the catch-all then cleared `followBottom` for the rest of the turn; separately, any mid-stream
  pane-height change (completion popup, approval dialog, textarea wrap) briefly falsified
  `AtBottom()` and killed follow the same silent way. Fix (crush parity): follow is now user
  *intent* — paused only by an explicit scroll-up (wheel, pgup), resumed by scrolling back to the
  bottom or sending a message; only pgup/pgdown scroll the transcript while the textarea owns typed
  input (full vi set still active on the approval dialog's fall-through, where nothing is being
  typed); the batch-apply path re-arms follow one-way (`if AtBottom() → follow=true`), never clears;
  `applyViewportHeight` re-pins the bottom on pane resize while following; the send/queue paths
  resync the pane height after `ta.Reset()`. Tests: `internal/tui/follow_intent_test.go` (5
  regressions: typing letters mid-stream, viewport shrink mid-stream, resize re-pin, pgup-pause/
  pgdown-resume, wheel pause/resume).

**Suggested sequencing:** P21.1 + P21.4 together (the fix and its proof), then P21.3 (cheap visible
win), then P21.2 (larger visual restructure). P21.5/P21.6 are independent security/robustness items
with no ordering constraint. The `/ui` token finding from the same review is tracked as **P15.12**
in the web-UI track above (it's a change to P15.1's existing token-injection mechanism, so it lives
with P15). Per the P13 cross-cutting rule, none of these add model-callable capability, so no new
`/slash` surface is required. Priority: **High** for P21.1 (direct user pain), **Medium** for the
rest. Effort: **M** overall — mostly small, contained changes.

---

## Open Work — P22 (OpenAI Codex CLI evaluation — 2026-07-08)

Feature evaluation of [github.com/openai/codex](https://github.com/openai/codex) (Rust, terminal
coding agent) against Aegis, requested 2026-07-08 — same exercise as the P16 crush/opencode/Claude
Code gap analysis and the P20 Odysseus evaluation. Most of Codex's surface Aegis already has, often
more richly: AGENTS.md/CLAUDE.md context files (`internal/memory/context.go`), skills, MCP
client+server, plan/approval modes, sandboxing, `/compact`, `/copy`, `/theme`, `/status`,
`/archive`, session resume/switch, per-turn checkpoints (`/rewind`), background runs
(`/bg`, `/runs`, `/detach`), queued messages, mid-turn steering, `!` shell commands, `@`-file
fuzzy mention, external-editor compose, image paste, custom commands, configurable keybindings,
cost/usage display, personas (richer than Codex's `/personality`), debate, memory. Genuine gaps
worth adopting, in priority order:

- **P22.1 — SHIPPED 2026-07-08 — `/diff` command.**
  See [releases.md](releases.md#shipped--p22-items-openai-codex-cli-evaluation-diff-ctrlr-history-search).
- **P22.2 — SHIPPED 2026-07-08 — `/review` mode.**
  See [releases.md](releases.md#shipped--p22-items-openai-codex-cli-evaluation-diff-ctrlr-history-search-review).
- **P22.3 — Esc-Esc backtrack + `/fork`.** Edit a previous user message and fork the conversation
  from that turn (Codex: transcript forking). Aegis's per-turn checkpoints (`internal/checkpoint`)
  and the timeline picker already address file state and navigation; the missing piece is forking
  the *conversation* into a new session from turn N with the edited message replayed. Session store
  already snapshots turns, so this is a copy-turns-then-branch operation plus TUI affordance. (M)
- **P22.4 — SHIPPED 2026-07-08 — Ctrl+R prompt-history search.**
  See [releases.md](releases.md#shipped--p22-items-openai-codex-cli-evaluation-diff-ctrlr-history-search).
  Note: this moved the session switcher to Ctrl+Y to free up Ctrl+R.
- **P22.5 — `/side` ephemeral side conversation.** Ask a quick side question in a throwaway context
  that never enters the session transcript or token budget (Codex `/side`/`/btw`). Maps cleanly to
  a hidden one-shot session against the current model. (S/M)
- **P22.6 — Raw scrollback mode.** `/raw` (Codex Alt+R): temporarily render the transcript as plain
  unstyled text and release the alternate screen so the terminal's native selection/copy and
  scrollback work. Complements the P16.5 mouse selection for tmux/SSH cases where screen-space
  selection is awkward. (S/M)

Evaluated and **rejected**: remote TUI over WebSocket (conflicts with the deliberate loopback-bind
security boundary; the P15 web UI with its token story is Aegis's remote surface), best-of-N
attempts (swarm/debate already cover multi-candidate workflows with arbitration, and local models
make N full attempts expensive), `codex cloud` task integration (no cloud backend exists or is
wanted), `/import` Claude-Code migration (niche; `.aegis/` layout is close enough to hand-port),
execpolicy Starlark DSL (Aegis's text permission rules + capability gates + contextual gates cover
the need; a new policy language is surface area without a trigger), Vim composer mode and
`/statusline`/`/title` customization (polish without demand — revisit on request).

Priority: **Medium** overall — P22.1/P22.4/P22.2 shipped 2026-07-08; P22.3 is the highest-value
remaining item. Effort: **S–M** per item. None add model-callable capability beyond what
`/review`'s read-only flow already reused from existing gates.

---

## Open Work — P9 (Engineering Quality — lower urgency, no current trigger)

P9.1, P9.2, and P9.5 are shipped. P9.3 (telemetry export) and P9.6 (bulk session/memory
export-import) were dropped 2026-07-05 — not wanted. Remaining:

### P9.4 — No per-task/complexity model routing

P5.9 only reroutes on failure. Nothing picks a cheaper model for simple turns and reserves an
expensive one for hard turns (cf. Aider). Plausible cheap win given cost tracking already exists,
but no evidence of demand. Priority: **Low**, Effort: **M**.

**Not blocking** — real but no concrete trigger, don't build speculatively.

---

## Open Work — P6 (Long-Horizon / Exploratory)

P6.3 (MCP server mode) shipped 2026-07-05.

### P6.1 — Mid-turn state persistence _(was P4.1)_

Persist partial turn state (accumulated assistant text, received tool calls) to SQLite during
streaming so a crash mid-turn loses nothing. High complexity, low-probability failure mode; revisit
if crash-during-long-turn becomes a reported pain point.

### P6.5 — Desktop / IDE surface beyond ACP _(superseded by P15, 2026-07-06)_

ACP covers Zed and Neovim; the web UI covers browsers. Evaluate: (a) VS Code extension speaking to
the daemon API, (b) wrapping the web UI in a lightweight desktop shell. Was: "only worth it if user
demand materializes — the TUI is the product." That demand materialized 2026-07-06 (see
[P15](#open-work--p15-web-ui-parity-with-the-tui) below) — a VS Code extension or desktop shell
remains unstarted and speculative, but the "is the web UI worth investing in" question this item
posed is answered; don't reopen it as a separate speculative track once P15 lands.

**P6.1 is not blocking** — no reported pain point, don't build without a concrete trigger; check
with the user first. (P6.2, A2A protocol integration, was evaluated and declined 2026-07-05 — no
consumer, not wanted.)

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
