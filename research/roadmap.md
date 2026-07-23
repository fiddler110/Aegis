# Aegis Capability Roadmap

**Last updated:** 2026-07-23

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 21 actionable (1 Tier 1, 11 Tier 2, 9 Tier 3) + 2 parked (Tier 4).

Threat-model fix priority order (do-first to least-urgent): **P39.7 (shipped) → P39.5 → P39.6 → P39.9 →
P39.8**, with **P38.1** as the tracking umbrella that closes once the remaining three land. Rationale:
P39.7 was cheapest and already corroborated on two independent local models — now shipped (see
[releases.md](releases.md)); P39.5 is the actual root cause (SKILL.md re-injected every turn starves the
fill of context) and everything else rides on it; P39.6 only has something to check once a build reaches
zero markers; P39.9 and P39.8 are adapter/robustness polish, with P39.9 ranked first because a dead
tool-call path blocks everything, whereas P39.8 already degrades gracefully via the P36.2 fallback.

- **P41.1** (Tier 1) — proactive compaction gates on a flat `chars/4` token estimate
  (`compaction.EstimateTokens`) instead of the engine's script-aware one (`engine.estimateTokens`), so a
  CJK/non-ASCII-heavy conversation can silently skip compaction the engine itself has already determined is
  needed. Surfaced by a 2026-07-22 data-flow review, not yet fixed.
- **P38.1** (Tier 2) — non-orchestrated, single-context threat-model build. **Environment gate lifted:**
  the doctor-recommended `qwen3.6:35b-a3b` is now installed and the conformance re-test has been run
  (2026-07-21, against FirewallRiskRater). The build **mechanism** re-confirms, but the autonomous
  `--skill` drive still does **not** reach a verify-clean suite on the stronger model. The reasons are
  now root-caused and split into **P39.5–P39.9** (P39.7 shipped 2026-07-22); an interim external wrapper
  is parked as **P38.8**.
- **P39.5, P39.6, P39.8, P39.9** (Tier 2/3) — the remaining harness-side fixes surfaced by the P38.1
  re-test: bound the drive-loop context (P39.5), fold phase-6 verification into the drive (P39.6),
  native-Ollama adapter reliability (P39.9), and robust compaction/guard on weak local models (P39.8).
  **P39.7** (no-progress guard) shipped 2026-07-22 — see [releases.md](releases.md).
- **P38.8** (Tier 4) — external per-phase threat-model wrapper, parked as a recorded interim workaround.
- **P25.9** (Tier 4) — per-session scoping of the remaining daemon-singleton services (`lsp.Manager`).
  Parked pending demand; do not build speculatively.
- **P44.1** (Tier 2) — bundled skill assets (scripts under a project/user `.aegis/skills/<name>/`) never go
  through admission scanning before being surfaced to the model. Filed 2026-07-22 from a DefenseClaw
  comparison.
- **P45.1** (Tier 2) — `worktree.Manager.Add` never carries uncommitted/untracked files into a new worktree.
  Filed 2026-07-23 from an xAI `grok-build` (`xai-fast-worktree`) comparison.
- **P45.2** (Tier 3) — no hunk-level agent-vs-external attribution; `filetracker` only does whole-file mtime
  staleness. Filed 2026-07-23 from the same comparison (`xai-hunk-tracker`).
- **P40.8** (Tier 2) — no LaTeX math rendering in the transcript; `$...$`/`$$...$$` show as raw text. Filed
  2026-07-23 from a `grok-build` (`xai-grok-markdown`) comparison, joins the P40.x TUI/UX track.
- **P40.9** (Tier 3) — no inline mermaid diagram rendering in chat; `render_diagram` only produces file
  output. Filed 2026-07-23, same comparison, joins the P40.x TUI/UX track.
- **P46.1** (Tier 2) — per-task file-write scope has no mechanical enforcement, only session-wide permission
  rules. Filed 2026-07-23 from a `codex-build` (Claude-orchestrates-Codex) comparison.
- **P46.2** (Tier 2) — `git_commit` has no pre-commit test gate; it's a straight passthrough regardless of
  test state. Filed 2026-07-23, same comparison.
- **P46.3** (Tier 3) — structured-build skill packaging declared scope + test-gated commit + one-task-one-
  commit discipline. Filed 2026-07-23, same comparison; sequenced after P46.1/P46.2.

A handful of unfiled **leads** (condensed under Tier 2/Tier 3 below) capture mechanical follow-ups worth
their own item when a concrete need appears.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.  
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.  
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).  
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** 1 open.

### P41.1 — Compaction's own token estimate disagrees with the engine's script-aware one, silently skipping compaction it shouldn't

`internal/compaction/compaction.go:101` (`EstimateTokens`) is a flat `chars/4` heuristic with no script
awareness. `internal/engine/engine.go:113` (`estimateTokens`) is script-aware — CJK/Hangul/Kana at ~1
token/char, other non-ASCII scripts at ~0.5 token/char — specifically because flat `chars/4` badly
undercounts dense scripts (see that function's own doc comment). The engine uses its accurate version for
the proactive 85%/95% "context nearly full" checks (`engine.go:496-538`) and `MaxTokensPerRun` enforcement,
but `Summarizer.compact` (`compaction.go:136`) — the *primary* compaction gate, called unconditionally at
the top of every `engine.Run`, not just the mid-run safety net — decides whether to actually compact using
its own, separate, less-accurate `EstimateTokens`. The two never share an implementation.

Net effect: for a CJK-heavy (or Cyrillic/Greek/Arabic/emoji-heavy) conversation, the engine can correctly
determine the context is 85%+ full and call `compactor.Compact`, only for the summarizer's cruder internal
`shouldCompact` check to decide there's still room and silently no-op — no error, no log beyond the outer
"not compacted" path. That's exactly the failure mode the script-aware estimator was built to prevent,
except the fix was never propagated into the package that owns the actual compaction trigger. Worst case: a
local model server (Ollama) silently truncates from the front — dropping the system prompt — before
compaction ever fires, the specific failure P2.7's proactive-compaction machinery exists to avoid.

Fix direction: export the engine's script-aware estimator (or move it to a shared package) and have
`compaction.Summarizer` use it instead of maintaining a second, independent heuristic.

Priority: Tier 1 — a real, currently-triggerable robustness gap (any non-ASCII-heavy conversation hits it),
small fix (swap one function for the other / share an implementation), no dependency on other roadmap work.

---

## Open Work — Tier 2

**Status:** 11 open. Threat-model track, in priority order — **P39.6** (fold phase-6 verification into the
drive loop), **P38.1** (conformance umbrella; gate now lifted, closes once the fixes below land). TUI/UX
track (independent, see Tier 2/3 note above P40.1) — **P40.1** (resizable panes), **P40.6** (contextual
footer), **P40.2** (consistent hjkl/g/G), **P40.5** (dark/light auto-detect), **P40.8** (LaTeX math
rendering). Independent hardening — **P44.1** (skill-asset scanning), **P45.1** (worktree dirty-file
replication), **P46.1** (per-task scope enforcement), **P46.2** (pre-commit test gate).

### P39.6 — Fold phase-6 verification into the drive-to-completion loop

The `--skill` drive stops when no `<!-- PENDING -->` marker remains, but it never runs the bundled P37 checks
(`verify.py`, `lint_dfd.py`, `inventory.py --check`). In the 2026-07-21 P38.1 re-test the "complete" suite
carried a duplicate threat ID (`T7` twice), tier↔prerequisite mismatches (Local-Process threats filed under
Tier 2), and stale tier counts in `0-assessment.md` — every one flagged by `verify.py`, all shipped uncaught
because nothing ran it. Fold the checks into the loop: when markers hit zero, run all three; if any fails,
feed the failure text back to the model to fix in place and re-run, bounded to a few rounds. This is the
autonomous analogue of SKILL.md §5's phase-6 round, and it is what the P38.8 wrapper already does as a
proof-of-shape.

Priority: Tier 2 — cheap, self-contained, no dependency. Turns the drive's done-condition from "all markers
filled" into "verifies clean," which is the real done-condition for an autonomous run. Sequence after
P39.5 lands — nothing meaningful to verify until a build actually reaches zero markers.

### P38.1 — Non-orchestrated, single-context threat-model build (primary path for local models)

The threat-modeling skill's primary build is a single-context linear build the driving model runs itself —
no sub-agents, no `agent`-tool orchestration. Context stays bounded by levers that already exist (SKILL.md
§4): `recon.py`'s ~11KB digest, P36.2 pruning of spent write/read payloads, incremental section-at-a-time
writes, and the deterministic P37 scripts. `scaffold.py` (P38.4) pre-writes all seven files from the
skeletons with real structure + a unique `<!-- PENDING: <section> -->` marker per fillable section, so the
model fills sections instead of authoring structure.

**Mechanism: live-confirmed.** In the 2026-07-21 re-test (qwen3:14b vs AiGateway, `aegis chat --skill
threat-modeling` drive-to-completion) the model ran `recon.py` → `scaffold.py` and wrote all seven files in
one context with no orchestration mis-route — the P36.3-era failure that killed every prior run is gone.

**Conformance: still unmet on qwen3:14b.** The 14B model's weakness has moved from "authoring structure"
(P38.4 fixed that) to "incrementally filling it via `edit_file`": it scaffolds but can't drive the fill to a
verify-clean suite. The two *actionable* findings from that re-test — think-mode fabrication and the
identical-marker `replace_all` footgun — were split out and **shipped** as P38.6 and P38.7 (see
[releases.md](releases.md#latest-changes)).

**2026-07-21 re-test on `qwen3.6:35b-a3b` (environment gate lifted).** The MoE is now installed; run against
FirewallRiskRater (a Rust/Axum API + Flask frontend), both the `-deep` and a 32k-`num_ctx` `-fast` derivative.
Findings: (1) the **mechanism re-confirms** — `recon.py` → `scaffold.py` → seven files in one context, no
mis-route, and the architecture doc it produced was genuinely high quality (correct anchors, deployment
class, security-infra inventory); (2) but the autonomous `--skill` drive still does **not** converge to a
verify-clean suite — one scaffolded resume made **86 tool calls across 3 drive iterations and cleared 0 of 23
`PENDING` markers**, because the ~9K-token SKILL.md preload rides *every* turn (`prompt_bytes≈31534` at turn 0)
and fills the 32K local window before the model can `edit_file` (→ **P39.5**); (3) the native Ollama adapter
emitted no tool call at all, forcing the legacy `/v1` adapter + a `num_ctx 32768` modelfile (→ **P39.9**);
(4) compaction/`output_guard` return empty on this model (42× `summarizer returned empty output` in the daemon
log, → **P39.8**); (5) the finished suite carried duplicate threat IDs, tier↔prerequisite mismatches and stale
counts that no autonomous verify pass caught (→ **P39.6**). Driving the *same* model one phase at a time
**without** the preload completed all seven files and verified clean after a fix loop — proof the blocker is
harness-side context bounding, **not** model capability. Full evidence + reference wrapper: FirewallRiskRater
`tools/THREAT-MODEL-AUTOMATION.md` (recorded as **P38.8**).

**2026-07-21 corroboration (`gpt-oss:20b` MoE vs AiGateway).** A second model/target pair reproduces the same
harness-side wall. `gpt-oss:20b` is the **first local model to pass `aegis doctor --deep`** (the synthetic
structured multi-turn fill probe) where qwen3:14b, gemma4:12b and mythos-sec:24b all fail it — yet passing
`--deep` did **not** predict a verify-clean build, because the probe never exercises the P39.5 SKILL.md-preload
pressure (it is a necessary-not-sufficient gate, not a conformance signal). Three `--skill` runs against
AiGateway: (1) pointed at a *pre-existing* complete suite, it ran `scaffold.py` as a no-op and then
**confabulated** a full build+verify report (0 `edit_file`, verify scripts never run) — an output-text analogue
of the P38.6 think fabrication; (2) on a clean fresh scaffold it filled **0 of 35 `PENDING` markers** and
yielded 3× with markers still present — a second instance of the **P39.7** "announce-then-yield" stall; (3)
adding an explicit "one section per turn, act now via `edit_file`" preamble to the prompt **unstuck the fill**
(first real `edit_file` landed) before it snagged on two lower-level faults — `scaffold.py .` dumping skeletons
into the repo root (bad run-dir + malformed `--framework stride-a` args) and an Ollama tool-call JSON parse
error on a rich markdown-table `new_string` (invalid `\'` escape + a non-ASCII hyphen). Takeaways: the preamble
result is direct corroboration that **P39.7's "act now" nudge is the right lever**, on a second model; and
`gpt-oss:20b`'s tool-call serialization is fragile on large `edit_file` payloads (relates to **P39.9**).

P36.2 (pruning that keeps a single-context linear build inside the window) is the load-bearing mechanism
here and is **partially confirmed live** (a 33-call run held inside ~44K input tokens with no overflow); its
definitive confirmation rides on a scaffolded, verify-clean re-test measured through P38.3's per-turn usage
telemetry.

Priority: Tier 2 — the environment gate is **lifted** and the re-test is done; the verify-clean goal is still
unmet autonomously, but the reasons are now root-caused and filed as **P39.5–P39.9**. This item stays open as
the conformance **umbrella** — closeable once the built-in `--skill` drive reaches a verify-clean suite on a
local model (which P39.5 + P39.6 are the load-bearing fixes for). Not Tier 1 because it is live-run
verification tracking, not independent build work.

**Lead (not yet filed):** the "accurate refusal, error-shaped" exit-code question for the SCA/secrets
scanners. P34.6 checked the *language*-targeted tools; nothing has swept the SCA/secrets tools for non-zero
exits that mean "nothing to do" rather than "I broke". No `### P<n>.<m>` heading yet.

**P40.1–P40.7** file TUI/UX gaps from a 2026-07-22 competitive review of `internal/tui` against best-in-class
open source TUIs (lazygit, k9s, yazi, zellij, btop/bottom, lnav, glow/soft-serve). Independent track from the
threat-model items above — no priority ordering implied between the two; within TUI/UX itself, **P40.1**
(pane resize), **P40.6** (contextual footer), **P40.2** (consistent hjkl/g/G), and **P40.5** (dark/light
auto-detect) are the cheap Tier 2 wins, while **P40.3** (transcript search) is the highest-value item overall
but large enough to sit in Tier 3 alongside **P40.7** (unify bespoke dialogs) and **P40.4** (real inline image
protocols, riskiest — needs a terminal-compat prototype).

### P40.1 — Resizable panes (sidebar, terminal pane)

The optional left sidebar and right-docked terminal pane (`Ctrl+B`/`Ctrl+X`) are fixed-width constants
(`sidebarInnerW`, `termPaneTotalW` in `tui.go`), toggled on/off but never resized; `layout()` (`tui.go:2587`)
recomputes viewport width from whichever panes are open but always at the same constant width. Best-in-class
multiplexer/dashboard TUIs (`zellij`, `tmux`, `k9s`) let a user grow/shrink a focused pane with a keybind
without leaving the app. Add a resize keybind (e.g. a modifier + arrow while a pane has focus) that adjusts a
stored width within min/max bounds and re-runs `layout()`.

Priority: Tier 2 — additive, no architecture change; turns an existing constant into a small piece of
persisted state plus a keybinding.

### P40.2 — Consistent hjkl/g/G navigation across every scrollable surface

`j`/`k` currently work only in the completion popup (`tui.go:1426`) and transcript scroll
(`transcript.go:746`); `bubbles/list`-backed dialogs get full hjkl for free but the transcript, tool-card
view, and terminal pane are inconsistent with each other. Tools like `yazi`, `lazygit`, `k9s`, and `lnav`
commit to hjkl plus `g`/`G` (top/bottom) on every scrollable surface, not just list widgets. Extend the same
handling to the remaining panes.

Priority: Tier 2 — small, self-contained; the pattern is already proven at two call sites, just needs
replicating to the rest.

### P40.5 — Auto-detect terminal light/dark background for the default theme

Aegis always defaults to `darkScheme()` (`colorscheme.go:261`) and requires an explicit `/theme` command or
config value to switch to light; tools built on the same lipgloss/termenv stack (`glow`, `soft-serve`) call
`termenv.HasDarkBackground()` at startup to pick a sane default automatically.

Priority: Tier 2 — small, no-dependency; a single startup check feeding into the same scheme-selection path
that `/theme`/config already use.

### P40.6 — Contextual per-pane keybinding footer

`F1` (`renderHelpBox`, `tui.go:4505`) always renders the full static keymap regardless of what has focus
(chat vs. sidebar vs. terminal pane vs. an open dialog). `lazygit`'s bottom bar instead shows only the hints
relevant to whichever panel is currently focused. A one-line contextual footer would reduce how often users
need the full overlay for common actions.

Priority: Tier 2 — additive; `keyMap.helpEntries()` already exists as the single source of truth, this just
needs a focus-scoped subset and a footer render path.

### P40.8 — Render LaTeX math in the transcript instead of showing it raw

`newGlamourRenderer` (`tui.go:4633`) wires up plain `glamour.NewTermRenderer` with no math extension, so a
model response containing `$E=mc^2$` or a `$$...$$` block renders as literal dollar-sign text — goldmark
(glamour's underlying parser) has no math awareness by default. xAI's `grok-build` (`xai-grok-markdown`)
solves this the terminal-appropriate way: it doesn't attempt real TeX typesetting, it converts common LaTeX
math markup to a Unicode approximation (`$E=mc^2$` → `E=mc²`) as a preprocessing pass before the existing
renderer sees it — superscripts/subscripts/Greek letters/common operators via Unicode codepoints. Personas
that discuss math (general math help, algorithm complexity, crypto primitives) currently degrade to raw
markup with no visual distinction from prose.

Priority: Tier 2 — small, self-contained preprocessing step ahead of the existing `newGlamourRenderer` call;
no new rendering pipeline, no new dependency (a regex/parser pass over recognized delimiters).

### P44.1 — Bundled skill assets (scripts, not just prose) never go through admission scanning

Surfaced 2026-07-22 comparing Aegis against Cisco's DefenseClaw, whose CodeGuard admission gate statically
scans a skill/MCP asset for secrets, dangerous exec, unsafe deserialization, and injection patterns *before*
it's trusted. Aegis has no equivalent for the one place it has genuinely untrusted executable content on
disk: a bundled skill directory (`.aegis/skills/<name>/SKILL.md` plus companion `scripts/`, `references/`)
can ship arbitrary `.py`/`.sh` files. `appendFromDir` (`internal/skills/skills.go:214`) loads such a bundle
and `withAssetManifest` (`skills.go:301`) lists every companion file in a `<skill_assets>` block telling the
model to read them with its own file tools — and, if the skill's own instructions say so, run them via the
shell tool. `wrapUntrustedSkill` (`skills.go:269`) only wraps the `SKILL.md` prose in a provenance marker
(deliberately `scan=false` — skill prose about its own instructions makes the heuristic noisy, per that
function's comment); it never touches the bundled scripts' actual content, and nothing else in the discovery
path does either. A compromised contributor's commit (project `.aegis/skills/`) or a bad drop into a user's
`~/.aegis/skills/` gets its scripts surfaced to the model, and potentially executed, with zero static
scrutiny — the same class of supply-chain gap DefenseClaw's admission control targets, but for skills instead
of MCP servers (Aegis doesn't fetch/build MCP server code itself — `cfg.MCP.servers` just points at an
already-installed external command — so that half of DefenseClaw's model doesn't apply here today).

Fix direction: reuse the filesystem-scan path `aegis security scan` already drives
(`security.DefaultScanners`/`security.PlanScanners` against a directory, backed by the multiscanner container
image — `internal/security/multiscanner.go`) rather than building a second static-analysis engine. On first
discovery of a *bundled, untrusted* skill directory (the `!trusted` branch of `appendFromDir`, i.e. never the
embedded built-ins), run it through the same scan and fold any HIGH/CRITICAL finding into the
`<skill_assets>` block as a visible warning — same "frame it as data, never drop it" precedent
`trust.Wrap`'s scan-hit path and `mcp.wrapUntrustedSkill`'s sibling `wrapUntrustedOutput` already set. Cache
the verdict the same way `discoverCache`/`skillsDirSignature` (`skills.go:70-146`) already cache the parsed
skill by directory signature, so re-scanning only happens when the bundle's files actually change. Must
degrade to a silent no-op (not a hard failure) when the multiscanner image hasn't been built — mirror the
existing `verifyMultiscannerImage`/`verifyMultiscannerCache` fallback pattern — since most sessions won't have
run `aegis security build-image`.

Priority: Tier 2 — self-contained hardening that reuses existing scanner and caching infrastructure end to
end, no dependency on other open roadmap work. Not Tier 1: bundled skills are typically project-authored
content at the same trust level as the rest of the repo already, so this is defense-in-depth against a
supply-chain scenario rather than a currently-demonstrated exploit path.

### P45.1 — `worktree.Manager.Add` doesn't carry uncommitted/untracked files into the new worktree

`Add` (`internal/worktree/worktree.go:54`) is a thin wrapper over `git worktree add [-b branch] dest`, which
only checks out the **committed** tree (HEAD or a new branch from it) — any staged, unstaged, or untracked
files in the caller's working tree are invisible to the new worktree. Surfaced 2026-07-23 comparing against
xAI's `grok-build`, whose `xai-fast-worktree` crate exists specifically to replicate dirty and (optionally)
ignored files into a freshly created worktree — on top of CoW/BTRFS-snapshot cloning for speed, which Aegis
doesn't need. The correctness half does apply here: today, spawning a subagent into an isolated worktree
(the package's own doc comment frames this as "the 2026-standard mechanism for safe parallel agent
execution") silently drops any in-progress uncommitted work in the source tree — the subagent operates on a
clean HEAD checkout with no signal that it's missing the user's current edits, which can produce duplicate or
conflicting work rather than an explicit error.

Fix direction: after `git worktree add --no-checkout dest`, diff `git status --porcelain` against the source
tree and copy modified/staged/untracked files (respecting `.gitignore` for untracked, mirroring
`xai-fast-worktree`'s dirty/ignored split) into `dest` before returning, or surface a clear warning in the
tool result when dirty state exists and isn't copied.

Priority: Tier 2 — a real correctness surprise (silent, not exploitable) with a small, self-contained fix
(`git status --porcelain` + file copy); no dependency on other roadmap work.

### P46.1 — Per-task file-write scope is never mechanically enforced, only advisory permission rules

Surfaced 2026-07-23 comparing against `codex-build` (a Claude-orchestrates-Codex build workflow), whose
`check_scope.py` mechanically checks that a task's file modifications stay within a declared per-task
allowlist of exact paths, verified before and after code generation. Aegis's write-path gating has two
mechanisms, both coarser and neither task-scoped: the mode gate (`internal/permission/permission.go:56-88`)
decides per-capability (read/write/execute/network/spawn) with no path awareness at all, and the rule-based
allow/deny gate (`internal/permission/rules.go`) matches glob patterns like `allow write_file(src/**)`
against a file path (`ParseRule`, rules.go:69-92; `RuleGate.Check`, rules.go:321-341) — but these rules are
parsed once at session/config load (`ParseRules`, rules.go:96-110) and apply for the whole session, not a
single task. `ContextualGate` (`internal/permission/contextual.go`) is the closest existing shape — extra
gate state layered on top of the base mode/rule check — but it tracks session-lifetime facts (e.g. "network
was used this session"), not a per-task path allowlist. No built-in persona or skill even encodes "stay
within a declared file scope" as prose (grepped `internal/persona/builtin/*.md` and
`internal/skills/builtin/*/SKILL.md` — no hits). So a skill or subagent that should only be touching a
handful of files for one task has no way to say so, and nothing would catch it if it strayed.

Fix direction: add a gate layer with `ContextualGate`'s shape that a skill/tool call can push a declared path
allowlist onto for the duration of a task (e.g. a dedicated `scope` tool call or skill front-matter field),
checked the same way `RuleGate` matches glob patterns against every `write_file`/`edit_file` call, popped
when the task/skill run ends.

Priority: Tier 2 — additive new gate layer following an existing pattern (`ContextualGate`), self-contained,
no dependency on other roadmap work. Not Tier 1: no currently-demonstrated exploit — this is blast-radius
containment for agent-authored changes, not a response to an active gap.

### P46.2 — No pre-commit test gate; `git_commit` always executes regardless of test state

Same `codex-build` comparison (2026-07-23): its orchestration loop refuses to commit a task's changes unless
the configured test command has just passed — "tests run before every commit, not usually, every one; red
gate → no commit." Aegis's `gitCommitTool.Execute` (`internal/tool/builtin/git.go:192-247`) is a straight
passthrough: validate message/paths → stage (`git add -A` or explicit paths) → `git commit -m` → return the
hash and diffstat. There's no test-command config, no check that a test tool ran (let alone passed) since the
last edit to a staged file, and no engine-level gate inspecting commit calls for a preceding test run — it's
gated only by the ordinary `CapWrite` permission check, identical to any other write. The `developer` persona
already tells the model in prose to "run the relevant build or test command to verify correctness" after
edits (`internal/persona/builtin/developer.md:59-62`), but that's unenforced guidance disconnected from the
commit call itself — a model can skip it, or lose it under context pressure, and commit broken code anyway.

Fix direction: an optional config (e.g. `git.pre_commit_test_command`, or a skill/persona-declared test
command) that `gitCommitTool` checks before running `git commit` — either require a matching test-tool
invocation to have succeeded since the last staged edit, or shell out to the configured command itself and
refuse the commit with the failure output on non-zero exit. Must default to a no-op when unconfigured so it
doesn't retroactively break sessions with no test command declared.

Priority: Tier 2 — self-contained change scoped to one tool, no dependency on other roadmap work.

---

## Open Work — Tier 3

**Status:** 9 filed. Threat-model track, in priority order — **P39.5** (bound the drive-loop context: root
cause, do first), **P39.9** (native-Ollama adapter reliability: a dead tool-call path blocks everything
downstream), **P39.8** (compaction/guard on weak local models: already mitigated by a fallback, least
urgent) — plus open leads below. TUI/UX track (independent, see Tier 2/3 note above P40.1) — **P40.3**
(transcript search, highest value of the batch), **P40.7** (unify bespoke dialogs), **P40.4** (real inline
image protocols, riskiest), **P40.9** (inline mermaid rendering). Independent hardening — **P45.2**
(hunk-level agent-vs-external attribution), **P46.3** (structured-build skill; sequenced after P46.1/P46.2).

### P39.5 — Bound the skill drive-loop's peak context so local-model fills converge (P38.1 root cause)

The 2026-07-21 P38.1 re-test root-caused why the drive-to-completion fill doesn't reach a verify-clean suite
on a stronger local model: `aegis chat --skill <name>` re-injects the full SKILL.md (~9K tokens;
`prompt_bytes≈31534` at turn 0) into **every** turn, so on a 32K local window the architecture recon + a few
file reads leave no room to `edit_file`. On a scaffolded resume, one run made **86 tool calls across 3 drive
iterations and cleared 0 of 23 `PENDING` markers** — the model re-reads the partial suite each iteration and
never converges. The fix is context discipline the harness enforces: after phase 1, load only the *current
phase's* reference (not the whole SKILL.md), leaning on P36.2 pruning to drop spent reads. Proof this is the
right lever: an external wrapper (P38.8) driving the same model one phase at a time **without** the preload
completed all seven files. Sequence-dependent — pairs with P39.6 (verify loop) and rides P38.3 telemetry to
confirm the window stays bounded.

Priority: Tier 3 — the load-bearing blocker behind P38.1's unmet conformance, now root-caused; larger than a
config tweak and interacts with the drive loop, per-phase reference loading, and P36.2.

### P39.9 — Native-Ollama adapter emits no tool call on large skill-preload turns; `/v1` path ignores `context_window`

Two adapter-level snags surfaced in the 2026-07-21 runs. (a) With `provider.default: ollama` (native adapter)
the skill-preload turn produced **no tool call and no run directory after 8+ minutes on two runs** — the same
prompt on the legacy openai-compat (`/v1`) adapter emitted tool calls immediately; needs a focused repro
(think-mode? oversized system prompt?) before it's actionable. (b) The `/v1` compat path never sends `num_ctx`,
so Ollama serves the model's modelfile default (16384 for stock `qwen3.6:35b-a3b-fast`), producing
`request (34774 tokens) exceeds the available context size (16384)`; the user must bake a `num_ctx 32768`
modelfile derivative. Either honor `provider.context_window` on the `/v1` path, or surface the modelfile
requirement in `aegis doctor` / first-init. Relates to P35.2/P35.3 (context-window guidance) and
P35.9/P39.3 (native-adapter work).

Priority: Tier 3 — the `/v1`+`num_ctx` half is documented-workaround-able today; the native-adapter hang is
investigation-gated (needs a repro) rather than a ready fix. Ranked ahead of P39.8: a silently-dead tool-call
path blocks the whole drive, whereas P39.8 already degrades gracefully.

### P39.8 — Compaction / output-guard secondary LLM calls are unreliable on weak local models

Aegis's proactive context-compaction summarizer and the `output_guard` rubric check each make a secondary LLM
call to the *same* local model, which returns empty output — the daemon log shows **42×
`summarizer returned empty output`** across the 2026-07-21 runs. The existing deterministic fallback (P36.2)
fires after two empty summaries so a run degrades rather than hard-fails, but the guard call and the degraded
summaries still cost latency and quality on exactly the long runs that most need compaction. Options: a
`provider.summarizer_model` that routes compaction/guard to a small dedicated model, or auto-skipping the LLM
summarizer for models flagged weak (straight to the deterministic path). `output_guard.enabled: false` is the
current manual workaround (set in the FirewallRiskRater `.aegis/config.yaml`).

Priority: Tier 3 — a real robustness gap on local models, but partly mitigated by the existing fallback and
larger than config since it needs a routing / opt-out mechanism. Least urgent of the three Tier 3 items.

**Lead — doc-inconsistency (surfaced building the P37 scripts):**
(a) **threat-ID form** — `references/skeletons/skeleton-stride.md` writes threat IDs as bare sequential
`T1`/`T2`, but `output-formats.md`'s coverage / Related-Threats examples use composite `T04.S` form; the
P37 scripts match both, but the docs should settle on one canonical form.
(b) **inventory YAML style** — `skeleton-inventory.md`'s example is block-style while directive #13 says
list entries are one-line, and `inventory.py` emits one-line flow mappings; the skeleton example should
match what the generator produces. Both cosmetic doc drift, not code bugs.

**Lead — `recon.py` (P37.1) depth follow-ups**, left out of v1 deliberately:
(a) **data-flow edge inference** — seed the DFD's `DF##` flows from import graphs / client instantiations
so phase 2 starts from real edges;
(b) **config-default resolution** — parse the actual bind-address default from the config struct /
`config.yaml` to settle the deployment class deterministically (and downgrade `EXPOSE`/`0.0.0.0` to
`internal-network` when the k8s `Service` is `NodePort`/`ClusterIP` with no TLS terminator, rather than
over-flagging `internet-facing`);
(c) **richer symbol extraction** — functions/methods and route→handler maps, optionally via
`ctags`/tree-sitter when on PATH;
(d) **target-commit in the sidecar** — let `inventory.py` take an optional `--target-dir`/`--repo` (or read
the commit from `0-assessment.md`) so a run directory kept outside the target repo still records the
analyzed code's commit.

### P40.3 — Full-text search within a session's transcript/history

Every picker (session, persona, model, timeline, command palette) gets fuzzy filtering via the shared
`listDialog` (`dialog.go`), but nothing greps the actual message content of the open session or across
sessions — the timeline picker lists turns, not matches within them, and `/knowledge query` is a model-facing
tool, not a UI widget. `lnav`'s incremental `/`-search-with-`n`/`N`-next-match (and plain `less`) is the
standard here. Aegis sessions run long (agentic, multi-hour), so "find the earlier message where I asked
about X" is a real, recurring need with no current answer short of manual scrolling.

Priority: Tier 3 — highest end-user value of the TUI/UX batch (P40.1–P40.7), but needs a new
incremental-search widget and match-navigation state rather than a keybinding, so it's larger than the Tier 2
items in that batch.

### P40.4 — Real inline image protocol support (kitty graphics / iTerm2 / sixel)

`imagerender.go:17-33` explicitly descopes true inline-image protocols today because Bubbletea's cell-grid
redraw model has no primitive for "opaque out-of-band terminal state" the way it does for OSC 8 hyperlinks —
only a half-block SGR-text fallback ships. `yazi` and `superfile` solve the identical problem by writing image
escapes directly to the terminal *outside* the Bubbletea render loop, tracking the pane's screen position, and
redrawing only on scroll/resize/focus change. Worth a focused prototype against that specific technique before
treating the gap as permanently closed — `imagerender.go`'s own comment already flags it as "a candidate
follow-up once [richer protocols] can be verified against real terminals."

Priority: Tier 3 — real value (currently a documented, deliberate gap) but risky: needs verification against
real terminals before it's safe to ship, per the caveat already recorded in the code.

### P40.9 — Inline mermaid diagram rendering in the transcript

`render_diagram` (`internal/tool/builtin/diagram.go`) is the only path from mermaid/plantuml/graphviz source
to a viewable diagram, and it always produces a **file** (SVG/PNG/draw.io) via Kroki or a local CLI fallback
— there's no ASCII-art rendering of a ` ```mermaid ` fenced block inline in the chat transcript itself, the
way `xai-grok-markdown`'s dedicated `mermaid` module renders diagrams directly into the terminal pager. Today
a model that inlines a small mermaid snippet in its response (rather than calling `render_diagram`) just gets
it shown as an unstyled code block; a user has to explicitly ask for the tool call and open the resulting
file to see the shape of the diagram.

Priority: Tier 3 — real value (diagrams are common in architecture/threat-model personas' prose, not just
tool output) but needs an actual mermaid-to-ASCII/box-drawing layout engine (or shelling out to Kroki for a
preview render), which is a materially bigger lift than P40.8's text substitution.

### P40.7 — Migrate `securityConfigModel` and `wizardModel` onto the unified `listDialog` overlay system

`dialog.go`'s `listDialog` unified four previously-separate picker types (palette/persona/session/timeline/
model/history/threat-model/backtrack pickers), but `securityConfigModel` (`securityconfig.go`) and
`wizardModel` (`wizard.go`) remain bespoke, hand-rolled overlays outside that system. Not user-visible as a
bug today, but every future fix to shared dialog behavior (theming, dimming, resize, centering) has to be
duplicated across three implementations instead of one.

Priority: Tier 3 — real maintenance value but a larger refactor of two working, non-trivial forms (a 5-step
wizard, a scanner-config form) rather than a small addition.

### P45.2 — No hunk-level agent-vs-external change attribution

`internal/filetracker` (`tracker.go`) only tracks whole-file mtimes: `RecordRead` stamps a file's mtime,
`CheckWrite` rejects a write if the file changed since the last read (stale-read discipline), but it has no
concept of *which lines* in a file are agent-authored versus user-authored. Surfaced 2026-07-23 comparing
against xAI's `grok-build`, whose `xai-hunk-tracker` crate runs an actor that attributes each diff hunk to
`Agent` or `External` (fed by both the agent's own edit tool and an fs-notify watch for out-of-band changes),
giving the rest of the system a per-hunk source label rather than a per-file timestamp. Aegis's
`internal/checkpoint` restores whole turns, and there's no way today to answer "which of the changes
currently in this file did the agent make" (e.g. to revert only the agent's hunks after the user has since
hand-edited the same file, or to render a diff view scoped to just the AI's contribution).

Fix direction: extend `filetracker` (or a new package it composes with) to record, per successful `edit_file`/
`write_file` call, the resulting hunk ranges attributed to the agent, and reconcile against external mtime
changes the same way `CheckWrite` already detects staleness — so external edits invalidate only the
overlapping agent-attributed hunks rather than the whole file's tracking state.

Priority: Tier 3 — real value for `/rewind`-adjacent precision and diff UX, but a materially bigger lift than
P45.1: needs actual hunk-diffing (not just mtime comparison) and touches `checkpoint`'s restore model, not a
self-contained change.

### P46.3 — Structured-build skill: declared scope + test-gated commit + one-task-one-commit discipline

`codex-build`'s remaining property (2026-07-23 comparison, alongside P46.1/P46.2) is workflow discipline
layered on top of its scope and test-gate mechanisms: one task produces exactly one commit, one plan produces
exactly one PR — eliminating mega-diffs and giving each commit a reviewable, single-purpose unit of change.
Aegis has no equivalent packaged workflow: nothing steers a multi-task feature build toward one-commit-per-
task, and the `git_commit` tool has no opinion on commit granularity today.

Fix direction: once **P46.1** (per-task scope enforcement) and **P46.2** (test-gated commit) land as actual
mechanisms rather than prose, package them into a skill (e.g. `structured-build`) that, for each task in a
plan: declares the task's file scope, drives edits, requires the test gate to pass, and commits — one task,
one commit. Sequenced after P46.1/P46.2 specifically because a skill enforcing this only in prose would repeat
the same weakness the roadmap has already flagged elsewhere (e.g. **P44.1**, **P39.6**) — instructions a
model can drop under context pressure are not a substitute for a mechanical check.

**Related, not yet filed as its own item:** `codex-build` also halts entirely and presents the current diff
to the user if a task fails 3 times, rather than continuing to retry or silently rewriting code. Aegis's
`loopDetector` (`internal/engine/loopdetect.go`) only catches literal repeated tool-call signatures
(`engine.go:734-739`), and `BudgetUSD`/`MaxTokensPerRun` (`engine.go:470-476`, `741-751`) only catch
session-wide cost/token exhaustion — neither tracks "this specific task has failed N times" as its own
signal, and neither produces a diff/summary artifact on stopping (both just emit a `KindError` event). Worth
its own item once a "task" boundary exists (e.g. via P46.3) to count failures against.

Priority: Tier 3 — real value (reviewable history, bounded blast radius per task) but sequence-dependent on
P46.1 and P46.2 landing as enforced mechanisms first; not self-contained today.

---

## Open Work — Tier 4

### P38.8 — External per-phase threat-model wrapper as interim autonomous-build workaround (parked)

Until P39.5–P39.6 land, a completed, verify-clean suite is reachable **today** by driving Aegis outside the
`--skill` loop, one phase at a time with bounded context. A reference implementation is recorded at
`tools/aegis-threatmodel.sh` (+ `tools/THREAT-MODEL-AUTOMATION.md`) in the FirewallRiskRater repo: it runs
`scaffold.py`, then a small **skill-free** `aegis chat` per phase (architecture → DFD → STRIDE → findings →
assessment), re-invoking while a phase's file still has `PENDING` markers with an "act now" preamble, then
runs the P37 checks and loops their failures back to the model until clean. Because each turn's context is
just the prompt + that phase's files, the compaction wedge (P39.8) and preload bloat (P39.5) never trigger.
Validated per-phase on `qwen3.6-fast-32k`, 2026-07-21 — all five content phases completed and the suite
verified clean after the fix loop.

Priority: Tier 4 — a workaround that lives *outside* the harness and duplicates what the drive loop should do
natively. Recorded so the working recipe isn't lost; **superseded by P39.5 + P39.6** (P39.7 shipped
2026-07-22) once the built-in path converges. Do not invest in it beyond the reference.

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of the six daemon-singleton services were per-session-scoped when P25.9 first shipped; `lsp.Manager`
was deliberately left as a shared singleton — its per-session resource-growth tradeoff was judged worse
than the isolation gap. Parked pending a concrete multi-tenant need.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
