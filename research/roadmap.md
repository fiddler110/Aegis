# Aegis Capability Roadmap

**Last updated:** 2026-07-23

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 6 actionable (0 Tier 1, 1 Tier 2, 5 Tier 3) + 2 parked (Tier 4).

**Recommended execution order (cross-tier, 2026-07-23).** A single do-next sequence across all tracks,
ordered by tier-priority then dependency:

1. ~~Independent Tier 2 fill-in — P44.1, P45.1, and the cheap TUI batch P40.1/P40.6/P40.2/P40.5/P40.8.~~
   **Shipped 2026-07-23** (see [releases.md](releases.md)): P44.1 (skill-asset admission scanning), P45.1
   (worktree dirty-file replication), and the five-item TUI/UX batch — P40.8 (LaTeX→Unicode math), P40.5
   (auto dark/light detect), P40.2 (consistent hjkl/g/G), P40.1 (resizable panes), P40.6 (contextual footer).
2. **Remaining Tier 2** — **P38.1** stays open only as the threat-model conformance umbrella (awaiting a live
   verify-clean re-test; no code blocked on it).
3. **Tier 3 TUI/UX** — **P40.3** (transcript search, highest value), **P40.9** (inline mermaid), **P40.4**
   (real inline image protocols, riskiest), **P40.7** (unify bespoke dialogs); plus **P45.2** (hunk-level
   attribution).

Note: the **codex-build workflow-discipline track is now landed.** P46.1 (per-task file-write scope
enforcement), P46.2 (pre-commit test gate on `git_commit`), and P46.3 (the `structured-build` skill) all
shipped 2026-07-23 — see [releases.md](releases.md).

Note: the **threat-model harness track is now landed.** P39.5, P39.6, P39.7, P39.8 shipped and P39.9's
actionable halves are resolved (see below and [releases.md](releases.md)); the only remaining threat-model
item is the **P38.1** umbrella, which stays open purely to track a live verify-clean re-test — no code is
blocked on it.

Threat-model fix priority order — **all shipped**: **P39.7 → P39.5 → P39.6 → P39.8 → P39.9** are done (see
[releases.md](releases.md)). P39.7 was cheapest and corroborated on two local models; P39.5 was the actual
root cause (the ~9K-token SKILL.md re-injected every turn starved the fill of context — now compacted out of
the first message after the opening turn); P39.6 folded the phase-6 verify-and-fix loop into the drive so its
done-condition is "verifies clean," not "all markers filled"; P39.8 latches a proven-broken LLM summarizer
off for the rest of a run; P39.9 warns before a `/v1` compat drive overflows (and its native-adapter half was
investigated and exonerated). **P38.1** remains the tracking umbrella, now awaiting only a live re-test.

- **P38.1** (Tier 2) — non-orchestrated, single-context threat-model build. **Environment gate lifted** and
  all load-bearing harness fixes (**P39.5–P39.9**) shipped. The build **mechanism** re-confirms; the item
  stays open **only** as the conformance umbrella, closeable once a live `--skill` drive is confirmed to reach
  a verify-clean suite on a local model with the shipped fixes in place. An interim external wrapper is parked
  as **P38.8**.
- **P39.5, P39.6, P39.7, P39.8, P39.9** — **shipped** (harness-side fixes surfaced by the P38.1 re-test):
  bound the drive-loop context (P39.5), fold phase-6 verification into the drive (P39.6), no-progress "act
  now" nudge (P39.7), latch off a broken LLM summarizer on weak local models (P39.8), and warn a `/v1` compat
  drive before it overflows (P39.9, native-adapter half exonerated). See [releases.md](releases.md).
- **P38.8** (Tier 4) — external per-phase threat-model wrapper, parked as a recorded interim workaround.
- **P25.9** (Tier 4) — per-session scoping of the remaining daemon-singleton services (`lsp.Manager`).
  Parked pending demand; do not build speculatively.
- **P44.1, P45.1** — **shipped 2026-07-23**: bundled skill assets now go through admission scanning on
  discovery (a `security.ScanBundleWarnings` seam folds HIGH/CRITICAL findings into the `<skill_assets>`
  block), and `worktree.Manager.Add` now carries uncommitted/untracked (non-ignored) files into a new
  worktree. See [releases.md](releases.md).
- **P40.1, P40.2, P40.5, P40.6, P40.8** — **shipped 2026-07-23** (TUI/UX Tier 2 batch): resizable panes,
  consistent hjkl/g/G navigation, auto dark/light detection, contextual per-pane footer, and LaTeX→Unicode
  math rendering. See [releases.md](releases.md).
- **P45.2** (Tier 3) — no hunk-level agent-vs-external attribution; `filetracker` only does whole-file mtime
  staleness. Filed 2026-07-23 from the same comparison (`xai-hunk-tracker`).
- **P40.9** (Tier 3) — no inline mermaid diagram rendering in chat; `render_diagram` only produces file
  output. Filed 2026-07-23, same comparison, joins the P40.x TUI/UX track.
- **P46.1, P46.2, P46.3** — **shipped 2026-07-23** (codex-build workflow-discipline track): per-task
  file-write scope enforcement (`permission.ScopeGate` + the `scope` tool), the `git_commit` pre-commit test
  gate (`git.pre_commit_test_command`), and the `structured-build` skill packaging both into a
  one-task-one-commit workflow. See [releases.md](releases.md).

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

**Status:** 0 open. **P41.1 shipped 2026-07-23** — the script-aware estimator now lives in a shared
`internal/tokenest` package that both the engine and `compaction.EstimateTokens` call (see
[releases.md](releases.md)).

---

## Open Work — Tier 2

**Status:** 1 open. Threat-model track — **P38.1** (conformance umbrella; gate lifted and all load-bearing
fixes shipped, now awaiting only a live verify-clean re-test). The TUI/UX Tier 2 batch (**P40.1**, **P40.2**,
**P40.5**, **P40.6**, **P40.8**), independent hardening (**P44.1**, **P45.1**), and the codex-build track
(**P46.1**, **P46.2**) all **shipped 2026-07-23** — see [releases.md](releases.md).

> **P39.6 shipped** (2026-07-21) — the `--skill` drive now runs the bundled phase-6 checks (`verify.py`,
> `lint_dfd.py`, `inventory.py --check`) when its PENDING markers hit zero and feeds any failure back for an
> in-place fix, bounded to `maxVerifyRounds`. See [releases.md](releases.md).

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

Priority: Tier 2 — the environment gate is **lifted**, the re-test is done, and every load-bearing
harness fix it root-caused (**P39.5–P39.9**) has now **shipped** (see [releases.md](releases.md)). This item
stays open only as the conformance **umbrella** — closeable once a live built-in `--skill` drive is confirmed
to reach a verify-clean suite on a local model with those fixes in place. Not Tier 1 because it is live-run
verification tracking, not independent build work.

**Lead (not yet filed):** the "accurate refusal, error-shaped" exit-code question for the SCA/secrets
scanners. P34.6 checked the *language*-targeted tools; nothing has swept the SCA/secrets tools for non-zero
exits that mean "nothing to do" rather than "I broke". No `### P<n>.<m>` heading yet.

**P40.1–P40.7** file TUI/UX gaps from a 2026-07-22 competitive review of `internal/tui` against best-in-class
open source TUIs (lazygit, k9s, yazi, zellij, btop/bottom, lnav, glow/soft-serve). Independent track from the
threat-model items above. The four cheap Tier 2 wins — **P40.1** (pane resize), **P40.6** (contextual
footer), **P40.2** (consistent hjkl/g/G), **P40.5** (dark/light auto-detect) — plus **P40.8** (LaTeX→Unicode
math) all **shipped 2026-07-23** (see [releases.md](releases.md)). Still open in Tier 3: **P40.3** (transcript
search, highest-value), **P40.7** (unify bespoke dialogs), **P40.4** (real inline image protocols, riskiest),
**P40.9** (inline mermaid).

> **P40.1, P40.2, P40.5, P40.6, P40.8 shipped** (2026-07-23) — the TUI/UX Tier 2 batch: resizable sidebar/
> terminal panes (`ctrl+←`/`ctrl+→` on the focused pane), consistent `hjkl`/`g`/`G` scroll on the transcript
> and tool-card views, `auto` as the default theme (terminal-background detection via
> `tea.BackgroundColorMsg`), a focus-scoped status-bar hint footer, and a LaTeX-math → Unicode preprocessing
> pass (`renderMathUnicode`) ahead of the glamour renderer. See [releases.md](releases.md).

> **P44.1 shipped** (2026-07-23) — bundled, untrusted skill directories are now screened through the same
> filesystem scan `aegis security scan` drives on discovery: a `skills.BundleScanner` seam (wired at
> daemon/CLI startup to `security.ScanBundleWarnings`) folds any HIGH/CRITICAL finding into the
> `<skill_assets>` block as a visible warning, cached by the existing directory signature, and degrades to a
> silent no-op when no scanner is installed. See [releases.md](releases.md).

> **P45.1 shipped** (2026-07-23) — `worktree.Manager.Add` now carries the source working tree's dirty state
> (modified/staged/untracked non-ignored files, plus deletions/renames) into a freshly created worktree via a
> copy-on-top pass over `git status --porcelain -z`, so spawning a subagent into an isolated worktree no
> longer silently drops in-progress edits. A new `AddCarry` returns the carried paths for reporting. See
> [releases.md](releases.md).

> **P46.1 shipped** (2026-07-23) — per-task file-write scope is now mechanically enforced: a new
> `permission.TaskScope` + `permission.ScopeGate` (wired outermost in `server.buildGate`) refuses any
> `write_file`/`edit_file`/`multi_edit` outside the active scope, and a deferred `scope` tool sets/clears it.
> See [releases.md](releases.md).

> **P46.2 shipped** (2026-07-23) — `git_commit` now runs an optional `git.pre_commit_test_command` before
> staging and aborts the commit on a non-zero exit; the setting is frozen from untrusted project config by the
> workspace-trust gate. See [releases.md](releases.md).

---

## Open Work — Tier 3

**Status:** 5 filed. Threat-model track — **all shipped** (P39.5/P39.8/P39.9 landed with the harness fixes
above; see the shipped note below and [releases.md](releases.md)); only open leads remain. TUI/UX track
(independent, see Tier 2/3 note above P40.1) — **P40.3** (transcript search, highest value of the batch),
**P40.7** (unify bespoke dialogs), **P40.4** (real inline image protocols, riskiest), **P40.9** (inline
mermaid rendering). Independent hardening — **P45.2** (hunk-level agent-vs-external attribution). The
codex-build track's **P46.3** (structured-build skill) shipped 2026-07-23 — see [releases.md](releases.md).

> **P39.5 shipped** (2026-07-21) — the drive stops re-sending the whole ~9K-token SKILL.md every turn:
> `compactFirstSkillMessage` rewrites the first user message once after the opening turn, swapping the skill
> body for a compact on-disk pointer the model can re-read on demand. **P39.8 shipped** (2026-07-21) — a
> proven-broken LLM summarizer is latched off for the rest of a run past `summarizerGiveUpThreshold` (4)
> cumulative failures, compacting deterministically (P36.2) thereafter. **P39.9 shipped/resolved** — the `/v1`
> compat drive now warns before overflowing with a runnable num_ctx-modelfile recipe (`warnCompatDriveWindow`
> / `LegacyOllamaModelfileRecipe`), and the native-adapter-hang half was investigated and **exonerated** (the
> adapter's tool-calling is fine for the available models). See [releases.md](releases.md).

**Lead — P39.9 residual (repro-gated):** a prefill-latency observability gap remains on the native path — the
only unresolved sliver of P39.9, tracked as a lead rather than a blocker because it needs a focused repro
before it is actionable.

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

> **P46.3 shipped** (2026-07-23) — the `structured-build` embedded skill packages P46.1's `scope` tool and
> P46.2's pre-commit test gate into a one-task-one-commit workflow (declare scope → edit → verify → commit →
> clear, repeat), landing only after both mechanisms were real so the skill leans on enforced gates rather
> than prose. See [releases.md](releases.md).

**Lead — task-failure halt (surfaced filing P46.3, not yet its own item):** `codex-build` also halts entirely
and presents the current diff if a task fails 3 times, rather than retrying or silently rewriting. Aegis's
`loopDetector` (`internal/engine/loopdetect.go`) only catches literal repeated tool-call signatures
(`engine.go:734-739`), and `BudgetUSD`/`MaxTokensPerRun` only catch session-wide cost/token exhaustion —
neither tracks "this specific task has failed N times" nor produces a diff/summary artifact on stopping (both
just emit a `KindError` event). The `structured-build` skill now encodes a stop-when-stuck rule in prose;
turning that into a mechanical per-task failure counter would need a persisted "task" boundary to count
against, and is worth its own item once that boundary exists.

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
