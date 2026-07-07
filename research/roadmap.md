# Aegis Capability Roadmap

**Last updated:** 2026-07-07 (P18 and P19 added — TUI streaming/scroll polish and a docs/command
misc bucket, researched and scoped from a user report, not started. Earlier the same day: P13.3.1 +
P13.3.5 shipped — shell-aware "diagnose this?" error assist for the embedded terminal pane and `!`
bang commands, plus a `tui.keybindings` config remap with startup validation and help-text sync;
P13.3.2/P13.3.3 deliberately left open as the lower-value remainder. Also shipped the same day:
P13.7 — `latex-report` builtin skill + `/report` command, closing out the last P13 item with a real
gap; P16.9 — in-terminal half-block image thumbnails, closing out the P16 TUI polish & interaction
parity track from the crush/opencode/Claude Code gap analysis)

This document tracks only **open** work — what's next. For shipped-feature history and full design
rationale behind completed items, see [releases.md](releases.md).

---

## Status

Open items: **P15.2–P15.11** (web UI
parity with the TUI — P15.1's architecture question is resolved and the frontend scaffold/faithful
-port shipped 2026-07-06, see below), **P13** (P13.3 terminal enhancements, P13.4 nebula-inspired
engagement tooling), **P18** (streaming-thinking display, scroll smoothness, auto-follow
reliability), **P19** (skill/script authoring guide, manual `/compact`), **P9.4** (per-task model
routing), **P6.1** (mid-turn state persistence).

Everything else — P2–P5, P7, P8, P9.1/P9.2/P9.5, the 2026-07-03 architecture/security review's
15-item punch list, P10, P11, P12, P13.1/P13.2/P13.5/P13.6/P13.7/P13.8, P14 (all of P14.1–P14.10), the
TQ TUI track, P15.1, P16 (all of P16.1–P16.9), P17 (all of P17.1–P17.5), and the 2026-07-06 fable-review.md remediation
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

## Open Work — P18 (TUI Streaming & Scroll Polish)

Requested 2026-07-07: three related complaints about the transcript pane during a streaming turn —
extended-thinking collapses instead of staying visible while it's being generated, scrolling feels
non-smooth, and the viewport doesn't reliably auto-follow a streaming response or resume following
when the user scrolls back to the bottom mid-stream. Researched 2026-07-07 (code-read only — no
real terminal available in this environment to reproduce interactively, per prior sessions); all
three below are scoped but **not started**, and should ship as one incremental pass since P18.1 and
P18.3 share the same root cause.

- **P18.1 — Full live extended-thinking display.** Today (`internal/tui/tui.go`), streaming
  `KindThinking` events accumulate into `m.thinkText` and `refresh()` re-renders the *entire*
  accumulated buffer dim above the answer on every frame (tui.go:1677-1679) — nothing is
  code-truncated while a turn is actively streaming. What actually shortens the reasoning is
  `flushThinking`/`appendThinkingBlock` (tui.go:1727-1762): once the turn moves on to the answer or
  a tool call, the block collapses to a one-line `✻ thought for Ns  (ctrl+o to expand)` summary by
  default (`m.thinkExpanded` starts `false`) — the full text is retained, just hidden until ctrl+o.
  Combined with P18.3 below, a user watching a long reasoning pass in a short terminal window sees
  only whatever fraction of the growing dim block currently fits above the fold and never catches
  up, which reads as truncation even though no text is discarded. Needs a product decision before
  implementing: (a) leave collapse-on-flush as the resting state (matches the "fold once done"
  convention TQ9 was built around) and rely on the P18.3 auto-follow fix to make the *live* portion
  actually trackable while it's being generated — plausibly sufficient on its own; or (b) add a
  config/session default so `m.thinkExpanded` starts `true`, keeping reasoning visible through the
  whole turn and only collapsing retroactively once scrolled past. (S once the default is chosen —
  this is a display-policy question, not a missing-data bug.)
- **P18.2 — Smooth scrolling.** Keyboard scroll (`transcriptPane.HandleKey`) moves one line at a
  time; mouse wheel (`HandleMouseWheel`, transcript.go:627+) reproduces bubbles/viewport's default
  3-line-per-tick delta. Neither is obviously wrong, so "not smooth" needs a profiling pass before
  a fix is chosen: candidates are (a) per-tick render cost — `refresh()`/`View()` cost scales with
  visible segment count and re-wraps on every call rather than reusing a wrapped-line cache across
  scroll-only updates (no content change), which could show up as visible jank under rapid wheel
  events or held-down j/k; (b) coalescing bursts of `tea.MouseWheelMsg` that arrive faster than a
  frame can render, so each tick doesn't force a full synchronous redraw; (c) the fixed 3-line delta
  itself feeling coarse compared to other TUIs' finer step. Start by instrumenting scroll-to-render
  latency before picking between these — don't guess. (M — needs measurement first, fix itself is
  likely S.)
- **P18.3 — Auto-follow reliability + resume-on-return-to-bottom.** `m.followBottom` is meant to
  auto-scroll during streaming and re-arm the moment the user scrolls back to the bottom (already
  partly implemented: `sendUserMessage` sets it `true` on send, `refresh()` calls `GotoBottom()`
  when it's `true`, and a catch-all `m.followBottom = m.transcript.AtBottom()` re-derivation runs at
  tui.go:1504). The likely bug, found by code reading, not yet reproduced live: that catch-all sits
  after a *second*, separate `switch` statement reached only when the *first* `switch msg.(type)`
  (starting ~tui.go:837) falls through without an early `return`. `eventMsg` — the case that fires
  for every streamed token, including thinking/text deltas — always `return`s early (tui.go:1153)
  and so never reaches the re-derivation. Meanwhile `spinner.TickMsg` (tui.go:844-861) *does* fall
  through to it, but only calls `refresh()`/`GotoBottom()` itself when `followBottom` is already
  `true` (a P3.7 redraw-suppression guard) — so once `followBottom` flips `false` for any reason,
  nothing streamed by `eventMsg` in between ever nudges the viewport, and only a subsequent spinner
  tick or an explicit user scroll can re-arm it. Needs verification against a real terminal (flagged
  in every prior TUI session as unavailable in this dev environment) before landing a fix; the fix
  itself is likely small — e.g. re-deriving `followBottom` (or unconditionally following when it's
  already `true`) from inside the `eventMsg` branch too, not just the tick/key/mouse paths. (S once
  verified.)

TUI surface requirement: none of these are new capabilities, so no new `/slash` command — existing
scroll keys/mouse wheel and the ctrl+o thinking toggle are the surface.

Priority: Medium (direct usability complaint, not exploratory), Effort: S-M per item once P18.2 is
profiled. Ship as one pass — P18.1's chosen option and P18.3 are coupled.

---

## Open Work — P19 (Docs & Session-Command Misc)

Two unrelated small items requested alongside P18 on 2026-07-07; grouped here as a no-blocker
bucket the same way P13.3's leftovers are, not because they share a theme.

- **P19.1 — Skill + companion-script authoring guide.** `internal/skills` (bundled `SKILL.md` +
  companion assets like `internal/skills/builtin/latex-report/analyze_sources.py` or
  `html-report/validate_report.py`) has no user-facing walkthrough today — `docs/extensibility.md`
  covers lifecycle hooks, MCP servers, custom commands/agents, process plugins, and plugin bundles,
  but never mentions skills at all, even though the mechanism (frontmatter `name:`/`description:`,
  progressive disclosure via the `skill` tool, the generated `<skill_assets>` manifest for bundled
  files, project vs. user vs. embedded-builtin precedence, `aegis skills enable/disable`) is fully
  built and documented in code comments only. Scope: a new `docs/skills.md` (sibling to
  `docs/personas.md`/`docs/debate.md`, not folded into `extensibility.md`, since skills are already
  their own documented subsystem in CLAUDE.md) covering: minimal single-file skill, bundled
  directory skill with a companion script and how `<skill_assets>` exposes it to the model,
  frontmatter fields, project/user/builtin precedence and name collisions, and a worked example
  (e.g. a small skill that ships a Python validation script, mirroring `html-report`). (S)
- **P19.2 — Manual `/compact` command.** `internal/compaction` only ever triggers automatically
  when a turn's estimated token count crosses budget (`Summarizer.shouldCompact`); there is no way
  for a user to force it early (e.g. before a long tool-heavy stretch they know is coming). Note:
  `/tools compact` (`internal/tui/slash.go:325`) is an unrelated existing command — it toggles
  tool-*output* display width, not conversation compaction — so naming the new command needs to
  avoid that collision (`/compact` is likely still fine since `/tools compact` is a subcommand of
  `/tools`, but confirm no ambiguity in the palette/autocomplete before shipping). Needs a daemon
  entry point (`Summarizer.Compact` is already callable directly, sidestepping the budget check) and
  a thin slash command wired to it, following the same dispatch-table pattern as every other command
  in `internal/tui/slash.go`. (S)

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
