# Aegis Capability Roadmap

**Last updated:** 2026-07-23

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 1 actionable (1 Tier 2) + 2 parked (Tier 4). The full **Tier 3 batch shipped 2026-07-23**
(P40.3, P40.4, P40.7, P40.9, P45.2 — see [releases.md](releases.md)); only the **P38.1** conformance umbrella
(Tier 2, awaiting a live verify-clean re-test) remains actionable.

**Recommended execution order (cross-tier, 2026-07-23).** A single do-next sequence across all tracks,
ordered by tier-priority then dependency:

1. ~~Independent Tier 2 fill-in — P44.1, P45.1, and the cheap TUI batch P40.1/P40.6/P40.2/P40.5/P40.8.~~
   **Shipped 2026-07-23** (see [releases.md](releases.md)): P44.1 (skill-asset admission scanning), P45.1
   (worktree dirty-file replication), and the five-item TUI/UX batch — P40.8 (LaTeX→Unicode math), P40.5
   (auto dark/light detect), P40.2 (consistent hjkl/g/G), P40.1 (resizable panes), P40.6 (contextual footer).
2. **Remaining Tier 2** — **P38.1** stays open as the threat-model conformance umbrella. The 2026-07-23
   gpt-oss:20b re-test found and fixed two more `chat --skill` harness bugs (**P39.10** asset access,
   **P39.11** oracle poisoning — both verified live); conformance is **still unmet** on a model-side
   run-dir-path-mangling wall. Next: releases.md entries + regression tests for P39.10/P39.11, then a
   verify-clean re-run. See the P38.1 body for the full 2026-07-23 progress note and tomorrow's steps.
3. ~~**Tier 3 TUI/UX** — P40.3 (transcript search), P40.9 (inline mermaid), P40.4 (real inline image
   protocols), P40.7 (unify bespoke dialogs); plus P45.2 (hunk-level attribution).~~ **All shipped
   2026-07-23** (see [releases.md](releases.md)): P40.3 (ctrl+f incremental transcript search), P40.9 (inline
   mermaid→ASCII via `internal/mermaidascii`), P45.2 (agent hunk attribution in `filetracker`), P40.7 (shared
   `fixedPanelFrame` for the two huh-form overlays — a semantic-fit substitute for the literal listDialog
   migration, since those forms aren't pickers), and P40.4 (an **experimental opt-in** kitty-graphics tier:
   detector + encoder shipped and tested, `image_rendering: "kitty"` only, render-loop placement left
   unverified against real terminals by design).

Note: the **codex-build workflow-discipline track is now landed.** P46.1 (per-task file-write scope
enforcement), P46.2 (pre-commit test gate on `git_commit`), and P46.3 (the `structured-build` skill) all
shipped 2026-07-23 — see [releases.md](releases.md).

Note: the **threat-model harness track** landed P39.5–P39.9, but the 2026-07-23 gpt-oss:20b conformance
re-test surfaced **two more harness bugs now fixed on `tier3-batch`** — **P39.10** (`chat --skill` never
materialized bundled scripts into the workspace, so the sandboxed file tools couldn't reach `recon.py`) and
**P39.11** (the drive's PENDING-marker oracle counted the materialized skeleton templates, so it could never
detect completion). Both are verified live but still need releases.md entries + regression tests. The
**P38.1** umbrella stays open: conformance is still unmet, now on a model-side run-dir-path-mangling wall, not
a harness gap. See the P38.1 body.

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
- **P39.10, P39.11** — **code fixed 2026-07-23 on `tier3-batch`, verified live, releases.md entry + regression
  tests pending** (surfaced by the 2026-07-23 gpt-oss:20b re-test — see P38.1 below): `chat --skill` now
  materializes bundled skill scripts *into the workspace* so the sandboxed file tools can reach them (P39.10),
  and the drive's PENDING-marker oracle skips the materialized `builtin-skills/` subtree so template markers
  don't jam completion (P39.11). These two stood *ahead* of model capability — the built-in drive could not
  reach `recon.py` at all before P39.10.
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
- **P40.3, P40.4, P40.7, P40.9, P45.2** — **shipped 2026-07-23** (Tier 3 batch): full-text transcript
  search (ctrl+f), an experimental opt-in kitty-graphics image tier, shared form-panel chrome
  (`fixedPanelFrame`), inline mermaid→ASCII rendering (`internal/mermaidascii`), and hunk-level
  agent-vs-external change attribution in `filetracker`. See [releases.md](releases.md).
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

**2026-07-23 re-test (gpt-oss:20b vs AiGateway copy) — two new harness bugs found and fixed; conformance
still unmet.** Ran the built-in `aegis chat --skill threat-modeling --mode build --yes` drive against a
scratch copy of AiGateway on gpt-oss:20b (the model that passes `doctor --deep`, re-confirmed here). The
drive died *before model capability was even tested*, exposing two `chat --skill`-CLI bugs that stand ahead
of the shipped P39.5–P39.9 fixes — both now **fixed and verified end-to-end** (build + `go test
./internal/cli/... ./internal/skills/...` green):

- **P39.10 (asset access).** `chat --skill <builtin>` materialized the bundled scripts only to
  `<dataDir>/builtin-skills/` (`internal/cli/chat.go`, `MaterializeBuiltins(cfg.DataDir)`), never into the
  workspace. The `<skill_assets dir>` manifest then pointed at that absolute data-dir path *outside the
  workspace root*, so the sandboxed file tools rejected every read of `recon.py`/`scaffold.py`/skeletons and
  the model bailed to a manual-draft offer before recon. The daemon's session-create and activate-skill paths
  already call `MaterializeBuiltinsToProject`; the CLI was the gap. **Fix:** `chat --skill` now also calls
  `skills.MaterializeBuiltinsToProject(cwd, enabledBuiltins)`. **Verified:** on a fresh copy the six scripts
  auto-appear under `<cwd>/.aegis/builtin-skills/threat-modeling/`, `recon.py` and `scaffold.py` both ran, and
  the full seven-file suite scaffolded.
- **P39.11 (drive-oracle poisoning, revealed by fixing P39.10).** The drive's completion oracle
  `scanPendingMarkers(<cwd>/.aegis)` (and the `suiteFileCount` floor check) walked the whole `.aegis/` tree
  matching `<!-- PENDING`, and the materialized skeleton templates + SKILL.md/README carry that exact marker
  in 8 files — so once the skill lives under `.aegis/builtin-skills/`, the oracle can never reach zero (drive
  runs to `--max-turns`, phase-6 verify never fires). **Fix:** both scans now `SkipDir` the `builtin-skills`
  subtree (const `pendingSkipDir`). **Verified:** the fix run scaffolded the suite and the drive kept filling
  against the *real* suite's markers rather than jamming on the template markers.

**Model wall (gpt-oss:20b) — unchanged and still the blocker after the fixes.** With the scripts reachable,
the run reached scaffold and started `edit_file`, but did not converge, from small-model *path/argument
brittleness*, not orchestration: (a) it mangles the script path (`.\aegis\.builtin-skills\…`, dot misplaced;
nested `powershell -Command` quoting) — one run repeated an identical bad `recon.py` invocation until the
engine loop-detector aborted (working as designed); (b) it scaffolded the suite correctly to `./.aegis/…`
but then `edit_file`d against a *typo* run-dir (`.aegit`, plus a spurious `scratchpad/AIGateway-fix2/`
prefix), so its fills landed in a junk directory and the real suite's ~36 `<!-- PENDING -->` markers stayed
untouched — it cannot hold a consistent run-dir path across turns; (c) recurring calls to a phantom `search`
tool (not registered) and explore/announce stalls; (d) wrong `--framework stride` (should be `stride-a`).

**Next steps for verification (tomorrow).**
1. File **P39.10** and **P39.11** into releases.md (code is committed on `tier3-batch`; add the shipped-item
   entries) and add regression tests: a `scanPendingMarkers`/`suiteFileCount` unit test asserting a
   materialized-skill PENDING marker is ignored, and a `chat --skill` test asserting the workspace
   materialization happens.
2. Re-run the built-in drive on a fresh AiGateway copy with the fixed binary and let it run to `--max-turns`
   (or completion). Success = the real suite's PENDING set reaches zero and `verify.py`/`lint_dfd.py`/
   `inventory.py --check` pass — that is the P38.1 closure condition.
3. If gpt-oss:20b keeps mangling the run-dir path, the lever is harness-side, not a bigger model: give the
   model the canonical run-dir *once* (e.g. have `scaffold.py`/a helper echo the exact absolute run-dir and
   instruct edits to use it verbatim), or have the drive pass the run-dir into the continuation prompt so the
   model never re-types it. Consider filing the phantom-`search`-tool and path-mangling patterns as their own
   sub-findings if they persist.

Reproduce: fixed binary at repo HEAD; `AEGIS_PROVIDER_MODEL=gpt-oss:20b`; `cd <fresh target copy>`; run the
drive prompt from the scratchpad (`prompt2.txt`). Do **not** run with cwd outside the target — the
workspace-root sandbox then rejects all target reads (that clean-target split is the parked P38.8 wrapper,
not the built-in path).

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

**Status:** all filed items **shipped 2026-07-23** — the TUI/UX batch **P40.3** (transcript search), **P40.7**
(shared form-panel chrome), **P40.4** (experimental opt-in kitty-graphics tier), **P40.9** (inline mermaid
rendering), and independent hardening **P45.2** (hunk-level attribution); see [releases.md](releases.md). Only
the leads below remain open. The threat-model track (P39.5/P39.8/P39.9) and the codex-build track's **P46.3**
(structured-build skill) also shipped 2026-07-23.

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

> **P40.3, P40.4, P40.7, P40.9, P45.2 — all shipped 2026-07-23** (the Tier 3 batch; full design notes in
> [releases.md](releases.md)). **P40.3**: ctrl+f incremental transcript search (`internal/tui/search.go`) —
> live query, ⏎/↑↓/ctrl+n·p match stepping, focused-match accent + reverse-highlight. **P40.9**: a
> dependency-free `internal/mermaidascii` renders flowchart/sequence mermaid to box-drawing ASCII, wired into
> `mdRender` so complete ` ```mermaid ` fences render inline (unsupported/mid-stream fences left raw).
> **P45.2**: `internal/filetracker` now records per-write agent-authored hunk ranges (stdlib LCS diff) and
> reconciles them against external edits (contiguity rule), dropping only overlapping hunks; the mtime
> read-before-write guard is untouched. **P40.7**: the two huh-form overlays (`wizardModel`,
> `securityConfigModel`) — which aren't `listDialog` pickers and so can't literally adopt the list widget —
> now share one `fixedPanelFrame` helper for their identical hand-rolled frame, the safe de-duplication the
> item targeted. **P40.4**: an *experimental, opt-in* kitty-graphics tier — `detectKittyGraphics` + a tested
> chunked `kittyGraphicsSequence` encoder, reachable only via `image_rendering: "kitty"` (never auto), with
> render-loop placement left unverified against real terminals by design (the safe half-block default is
> untouched).

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
