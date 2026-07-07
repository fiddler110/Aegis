# Aegis Capability Roadmap

**Last updated:** 2026-07-07 (new P16 track added — TUI polish & interaction parity, from the
crush/opencode/Claude Code gap analysis)

This document tracks only **open** work — what's next. For shipped-feature history and full design
rationale behind completed items, see [releases.md](releases.md).

---

## Status

Open items: **P16.2–P16.9** (TUI polish & interaction parity — gap analysis vs crush/opencode/
Claude Code, added 2026-07-07; **P16.1 shipped 2026-07-07**, see below), **P15.2–P15.11** (web UI
parity with the TUI — P15.1's architecture question is resolved and the frontend scaffold/faithful
-port shipped 2026-07-06, see below), **P13** (P13.3 terminal enhancements, P13.4 nebula-inspired
engagement tooling, P13.7 LaTeX report skill), **P9.4** (per-task model routing), **P6.1** (mid-turn
state persistence).

Everything else — P2–P5, P7, P8, P9.1/P9.2/P9.5, the 2026-07-03 architecture/security review's
15-item punch list, P10, P11, P12, P13.1/P13.2/P13.5/P13.6/P13.8, P14 (all of P14.1–P14.10), the
TQ TUI track, P15.1, and the 2026-07-06 fable-review.md remediation (CI/CodeQL/Dependabot, Windows
token ACL, compiler-enforced daily-cap guard, `aegis harden`, plan-mode network gating, release
workflow, server/TUI file splits, fuzz coverage, live-model eval tier, script-aware token
estimation) — is shipped. See [releases.md](releases.md) for what each shipped and why.

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

## Open Work — P16 (TUI Polish & Interaction Parity)

Requested 2026-07-07: "my TUI doesn't feel as polished and streamlined as claude code, opencode or
crush." Researched the same day by direct comparison of `internal/tui` (~10,600 lines) against
crush's `internal/ui` (charmbracelet/crush, ~100 source files across `chat/`, `dialog/`,
`diffview/`, `list/`, `notification/`, `completions/`), opencode's `packages/tui` (sst/opencode —
now a TypeScript rewrite on Bun using their own `@opentui` + SolidJS framework), and Claude Code's
interaction patterns.

**Headline finding:** Aegis's *feature checklist* is near-parity (queued messages, steering,
collapsible thinking, todo strip, sidebar, terminal pane, @file frecency completion, `!` commands,
draft stash, $EDITOR, esc-esc interrupt, mode cycling, palette, pickers all match or beat crush).
The "unpolished" perception comes from six specific presentation/interaction areas, itemized below.

**Language decision (resolved 2026-07-07, don't reopen):** stay on Go/bubbletea v2. Crush is pure
Go on the exact stack Aegis already uses and is the most polished TUI of the three — the ceiling is
architecture and component investment, not language. opencode's TypeScript rewrite required building
an entire terminal UI framework (`@opentui`) and ships ~60–100 MB Bun-compiled per-platform binaries;
adopting it would sacrifice Aegis's single-static-binary property (the same property P15.1
deliberately protected by committing `dist/`). Because the daemon/client split already exists, a
TS/other-language TUI could be added later as just another HTTP/SSE client — staying Go now is not a
one-way door.

**License caution (applies to every item below):** crush is FSL-licensed — study its patterns and
package boundaries (its `internal/ui/AGENTS.md` is effectively an architecture how-to), but **do not
vendor or translate its source**. Everything needed is available under MIT: `alecthomas/chroma`
(syntax highlighting), `charmbracelet/ultraviolet` (screen buffer — already an indirect dep via
bubbletea v2), and the rest of the charm stack.

### The items

- **P16.1 — SHIPPED 2026-07-07 — Notifications & attention system.** New `internal/tui/notify`
  package: OSC 0/2 window-title updates reflecting session state (via bubbletea v2's native
  `tea.View.WindowTitle`, no hand-rolled sequence needed), BEL + OSC 9 (with OSC 777 fallback) on
  stream-end/approval-pending/error, focus tracking via `tea.FocusMsg`/`tea.BlurMsg` to suppress
  notifications while focused (defaulting to "notify" when a terminal never reports focus, rather
  than silently suppressing forever), and a `tui.notifications` config knob (off/bell/desktop/both,
  default both) plus a live `/notify <mode>` command. Raw sequences go through bubbletea v2's
  `tea.Raw`/`RawMsg` (the sanctioned synchronized-output path) rather than writing to stdout
  directly. **Subsumes P13.3.4** (background-task attention indicator) — not implemented
  separately; deferred until a concrete failed-background-agent trigger exists, to route through
  this same seam then. See [releases.md](releases.md#shipped--p16-items-tui-polish--interaction-parity).
- **P16.2 — Syntax highlighting via chroma.** No code highlighting exists outside glamour's
  assistant-markdown fences: tool results, shell output (ANSI-16 remap only), read_file excerpts,
  and diff bodies are all flat single-color text. Add `alecthomas/chroma/v2` (MIT) with a lexer
  keyed off file extension (edit/write/read tools already carry `path`), styled from the existing
  `colorscheme.go` palette so both themes stay coherent. Applies to: diff added/removed/context
  lines (see P16.3), `read_file` result blocks, and fenced code in tool output. Cache highlighted
  output per block — chroma on every re-render would fight the P8 render-cost work. (M)
- **P16.3 — Diff presentation upgrade.** `toolview.go`'s LCS diff is correct but visually minimal:
  no line numbers, no intraline (word-level) change emphasis, flat +/- coloring, hard 24-line cap.
  Keep the existing `diffLines` LCS core and add a presentation layer: line-number gutter (old/new),
  intraline diff within changed line pairs (highlight the changed span, not the whole line), chroma
  syntax coloring under the +/- tint (depends on P16.2), and hunk headers with real line ranges
  instead of the bare `@@ ... @@`. Side-by-side split view (crush `diffview` has one, golden-tested
  per width/height) is explicitly **deferred** — unified-with-line-numbers covers the transcript
  case; split view only pays off in a dedicated review surface Aegis doesn't have. Since edit diffs
  dominate an agent transcript, this is the highest-visibility per-message polish item. Adopt
  crush's golden-file testing *convention* (render at a matrix of widths, snapshot) using the
  existing `AEGIS_EVAL_UPDATE=1` regen pattern. (M)
- **P16.4 — Transcript as a cached per-message item list (architecture investment).** The transcript
  is one big string re-joined into a `bubbles/viewport` on every refresh; streaming cost is tamed by
  the safe-boundary rewrap, but the monolith blocks per-message interaction and degrades on very
  long sessions. Restructure to crush's documented model: the chat view becomes a **virtualized list
  of per-message item renderers that cache their rendered output and invalidate individually** (on
  width change, theme change, or their own data change), with the existing `followBottom` flag
  driving auto-scroll. Sub-items stay imperative structs (render methods, no nested Elm models) per
  crush's `AGENTS.md` guidance — which matches how `internal/tui` already treats its pickers.
  `charmbracelet/ultraviolet` (already in go.mod as an indirect dep) offers the screen-buffer/
  rectangle layer if needed, but the item-list + cache refactor is the required core; ultraviolet
  adoption is optional follow-on, not a prerequisite. This is the enabler for P16.5 (mouse
  hit-testing needs line→message mapping) and for future per-message focus/expand/copy. Biggest
  single effort in the track; do not start it in the same session as anything else. (L)
- **P16.5 — Mouse selection, click interactions, and scrollbar.** `MouseModeCellMotion` is enabled
  but only the viewport's built-in wheel scroll does anything — there is no `tea.MouseMsg` handling
  at all, and because alt-screen + mouse mode disables the terminal's native selection, users
  currently **can't even select/copy text without shift-click**: enabling mouse mode without
  offering selection is worse than not enabling it. Add: drag selection with copy-on-release (reuse
  the existing `copyToClipboard` OSC-52/native path), double-click word / triple-click line
  selection, click-to-focus a message, and a rendered scrollbar glyph column replacing the
  title-bar "62% ·" text. Depends on P16.4 for line→message hit-testing. Interim mitigation if
  P16.4 is far off: a `/mouse` toggle that drops mouse mode so native terminal selection works. (M/L,
  depends on P16.4)
- **P16.6 — Unified dialog overlay + shared filterable list component.** Six ad-hoc modal fields
  (`palette`, `personaPicker`, `sessionPicker`, `timelinePicker`, `securityConfig`, `wizard`) each
  render full-screen via early returns in `render()` — no layering over the dimmed chat, and each
  picker re-implements its own filtering/key-handling/styles. Build (a) a `dialog` overlay stack
  (open/close/esc semantics in one place, dialogs composited over the chat with `lipgloss.Place`
  rather than replacing it) and (b) one reusable filterable-list component (crush's `ui/list`:
  filterable + focus + match-highlight) that palette/persona/session/timeline all migrate onto.
  Then add the dialogs users notice are missing: **model picker** (grouped by provider, current
  model marked — today model choice is config/CLI-only mid-session), and **quit confirmation** when
  a stream is active. File-picker dialog is deferred (@ completion covers the case). (M/L)
- **P16.7 — Runtime-loadable themes.** Two hardcoded schemes (dark/light) vs opencode's ~30 JSON
  theme assets + user themes. `colorscheme.go` already centralizes every color: add a loader for
  named theme files (JSON or YAML to match config conventions) from `~/.aegis/themes/` and project
  `.aegis/themes/`, ship 4–6 popular built-ins (catppuccin, dracula, gruvbox, tokyonight) embedded
  the same way builtin skills are, and extend `/theme` + `tui.theme` to accept any loaded name.
  Constraint from TQ10 still applies: lipgloss styles capture colors at creation time, so theme
  switching keeps the existing apply-before-styles-build path (mid-session switch = rebuild styles,
  which P16.4's cache invalidation must account for). (S/M)
- **P16.8 — Clipboard image paste.** `PasteMsg` handling only recognizes pasted *file paths* with
  image extensions; pasting actual image data from the clipboard (screenshot workflows — Claude
  Code and crush both support this) does nothing. Read image bytes from the OS clipboard on a paste
  keybinding, write to a temp file, and reuse the existing `@image:` attachment-token path.
  Windows/macOS/Linux clipboard-image access differs enough that this needs the same
  per-OS treatment `copyToClipboard` already has. (S/M)
- **P16.9 — In-terminal image rendering.** Attached images are sent to the model but never shown;
  crush renders them inline (kitty graphics / iTerm2 inline-image protocols, half-block fallback).
  Render a thumbnail in the transcript when an image attachment is sent, gated on terminal
  capability detection. Lowest priority in the track — cosmetic, benefits only image workflows. (M,
  nice-to-have)

**Explicitly not in this track:** configurable keybindings — already scoped as **P13.3.5** (add a
`tui.keybindings` config section); don't duplicate it here, but its priority rises now that the
polish track exists. Sound/audio cues (opencode-style) — rejected as out of character for the tool.
A which-key pending-keybind popup — revisit only if P13.3.5 ships chord bindings.

**Suggested sequencing:** ~~P16.1 first~~ **shipped 2026-07-07** → P16.2 + P16.3 together (one
visual unit) → P16.7 (independent, small) → P16.4 (the architecture investment, own session(s)) →
P16.5 (unlocked by P16.4) → P16.6 → P16.8/P16.9 as gap-fillers. P16.2/P16.3/P16.7/P16.8 have no
dependency on each other and can each ship standalone.

Priority: **High** (explicit user request, 2026-07-07 — this is the TUI-side counterpart of P15's
web-UI push; the TUI remains the primary surface). Effort: **L overall** — smaller than P15, larger
than any single P13 item; P16.4 alone is L and should be treated as its own multi-session effort.

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
  model on request. Cross-platform (exit-code capture only). (S/M)
- **P13.3.2** — `@shell`/`@last` context token: extend the existing `@file`/`@image:`
  attachment-token parser to inject the last N lines of embedded-terminal output into the next
  prompt. (S) — TUI-surface work; build under the same command-surface conventions P14 established.
- **P13.3.3** — ACP `terminal/*` capability passthrough: `internal/acp` implements
  session/prompt/permission methods but not the optional ACP terminal capability
  (`terminal/create`, `/output`, `/wait_for_exit`, `/kill`, `/release`). Implementing it lets an
  ACP host (Zed, a future Intelligent-Terminal-as-client) supply its own pty for agent shell calls
  — live visibility/Ctrl+C control on the host side — falling back to Aegis's native exec path
  when the host doesn't advertise the capability. The one item requiring real ACP protocol work;
  everything else here is TUI-only. (M/L)
- **P13.3.4** — Background-task attention indicator: extend the existing sidebar agent-count
  display to flag a failed background sub-agent/cron job. (S) — **subsumed by P16.1** (2026-07-07):
  route these events through the P16.1 notification seam instead of building a sidebar-only
  affordance; don't implement separately.
- **P13.3.5** — Configurable keybinding remap: `internal/tui/keymap.go` is fully hardcoded; add a
  `tui.keybindings` config section. Trivial cross-platform (bubbles/key is already OS-agnostic). (S)
  — TUI-surface work, same note as P13.3.2. Priority raised by the P16 polish track (2026-07-07):
  Claude Code (`keybindings.json`) and opencode both ship this; it's the P16-adjacent item users
  will ask for first.

Priority: Low-Medium, Effort: S-M per item, no single blocker.

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

### P13.7 — LaTeX report writing: consolidation skill

Audited against the current codebase: `latex_build`/`latex_new_document` tools already exist
(`internal/tool/builtin/latex.go`) with a built-in "report" document-class style (fancy headers,
code-listing styling, bibliography support), and the `report-writer` persona already references
them. The original framing of this item ("incorporate LaTeX use") no longer matches the codebase —
the capability exists.

The real gap: no skill walks through the specific ask — consolidating a large number of existing
markdown research/planning docs into one coherent LaTeX report — the way `html-report` bundles a
template + validator + steps for its narrower single-report case.

- **P13.7.1** — New builtin skill `internal/skills/builtin/latex-report/SKILL.md` (mirrors
  `html-report`'s pattern): steps for gathering/reading the source markdown docs, synthesizing a
  section outline, calling `latex_new_document(style="report")`, filling sections from the source
  material, `latex_build`, and reporting the output PDF path. Skill (progressive disclosure), not
  always-loaded — triggered on phrases like "consolidate these into a report", "write this up as a
  LaTeX report". (M)

TUI surface requirement: add a `/report [latex] <sources…>` slash entry point that kicks off the
consolidation skill, rather than depending on trigger-phrase detection.

Priority: Low, Effort: M.

### P13 cross-cutting requirement — every new capability must ship its in-session TUI surface

A recurring failure mode found during the 2026-07-05 review and worth keeping in mind for every
item above: a capability ships as a *tool* (model-callable) and a *CLI subcommand*, but never gets
an in-session `/slash` command — so a user driving the TUI can't reach it, and it feels absent.
Each open P13 item above already carries its own TUI-surface requirement inline; the general rule
going forward is that no P13 capability is "done" until it's reachable from the TUI and covered by
the P14.1/P14.10 command-surface sync test (`TestBuiltinCommandsCoverDispatchTable`,
`TestCommandDefsWellFormed`). This is a requirement addition, not new scope.

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
