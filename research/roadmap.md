# Aegis Capability Roadmap

- [Aegis Capability Roadmap](#aegis-capability-roadmap)
  - [Status](#status)
  - [Tiering Criteria](#tiering-criteria)
  - [Up next](#up-next)
  - [Open Work — Tier 4](#open-work--tier-4)
    - [P81.7 — The local model endpoint is unauthenticated plaintext HTTP on loopback (FIND-07)](#p817--the-local-model-endpoint-is-unauthenticated-plaintext-http-on-loopback-find-07)
    - [P77.6 — No OS-level process sandbox on Windows (GAP-05, spun out of P66.19)](#p776--no-os-level-process-sandbox-on-windows-gap-05-spun-out-of-p6619)
    - [P60.3 — Checkpoints capture files only, so `/rewind` is silent about everything else](#p603--checkpoints-capture-files-only-so-rewind-is-silent-about-everything-else)
    - [P65.4 — Resume is phase-granular, artifact-inferred, and only the drive has it](#p654--resume-is-phase-granular-artifact-inferred-and-only-the-drive-has-it)
    - [P67.13 — There is no way to execute a plan without committing to it](#p6713--there-is-no-way-to-execute-a-plan-without-committing-to-it)
    - [P80.3 remainder — `Server`'s auth/lockout and context-window field groups](#p803-remainder--servers-authlockout-and-context-window-field-groups)
    - [P66.19 — Capability gaps with no fired trigger](#p6619--capability-gaps-with-no-fired-trigger)
    - [P64.5 — `ask_user` is one free-form question; unattended answers cannot be routed](#p645--ask_user-is-one-free-form-question-unattended-answers-cannot-be-routed)
    - [P61.7 — Retry/terminal classification over _backend-echoed_ text (remainder)](#p617--retryterminal-classification-over-backend-echoed-text-remainder)
    - [P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)](#p5214--session-scoped-loop-detector-cross-run-loops-are-invisible)
    - [P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)](#p259--per-session-scoping-of-lspmanager-remaining-daemon-singleton)
    - [P65.5 — Rewinding away from a branch discards its work instead of summarizing it forward](#p655--rewinding-away-from-a-branch-discards-its-work-instead-of-summarizing-it-forward)
    - [P67.11 — Every budget is a ceiling; none expresses how much effort is wanted](#p6711--every-budget-is-a-ceiling-none-expresses-how-much-effort-is-wanted)
    - [P67.12 — Personas cannot accumulate anything across runs](#p6712--personas-cannot-accumulate-anything-across-runs)
    - [P85.5 — Agent-triggered self-refinement of persona, memory and skill definitions is not represented](#p855--agent-triggered-self-refinement-of-persona-memory-and-skill-definitions-is-not-represented)
    - [P85.6 — Non-blocking recursive sub-agent spawn as a language-level call (design reading, not recommended)](#p856--non-blocking-recursive-sub-agent-spawn-as-a-language-level-call-design-reading-not-recommended)
    - [P85.7 — Agent-owned internal heartbeat schedule with steer-vs-follow-up delivery, distinct from cron](#p857--agent-owned-internal-heartbeat-schedule-with-steer-vs-follow-up-delivery-distinct-from-cron)
    - [P86.1 — A wrong-typed tool argument reaches the model as Go's own JSON error](#p861--a-wrong-typed-tool-argument-reaches-the-model-as-gos-own-json-error)
    - [P86.2 — No detector for distinct, successful tool calls that teach the model nothing new](#p862--no-detector-for-distinct-successful-tool-calls-that-teach-the-model-nothing-new)
    - [P86.3 — Non-interactive runs exit 0/1/2 and say why they stopped only in free text](#p863--non-interactive-runs-exit-012-and-say-why-they-stopped-only-in-free-text)
    - [P86.4 — A pending approval cannot be questioned, only answered](#p864--a-pending-approval-cannot-be-questioned-only-answered)
    - [P86.5 — A long agent turn has no fold above the per-call and read/search-group level](#p865--a-long-agent-turn-has-no-fold-above-the-per-call-and-readsearch-group-level)
    - [P86.6 — Narrow-terminal rows are cut with an ellipsis, not laid out by fact priority](#p866--narrow-terminal-rows-are-cut-with-an-ellipsis-not-laid-out-by-fact-priority)
    - [P86.7 — Nothing forecasts the compaction that is about to discard the prefix cache](#p867--nothing-forecasts-the-compaction-that-is-about-to-discard-the-prefix-cache)
    - [P86.8 — The model cannot look up Aegis's own features; it improvises them](#p868--the-model-cannot-look-up-aegiss-own-features-it-improvises-them)
    - [P86.9 — The local prompt profile is chosen from the base URL, not the window it is meant to protect](#p869--the-local-prompt-profile-is-chosen-from-the-base-url-not-the-window-it-is-meant-to-protect)
    - [P86.10 — Skills are reached only if the model chooses to load them](#p8610--skills-are-reached-only-if-the-model-chooses-to-load-them)
    - [P86.11 — Guard changes have no written "is this fail-safe actually closed-loop" review rule](#p8611--guard-changes-have-no-written-is-this-fail-safe-actually-closed-loop-review-rule)
    - [P86.13 — Markdown is drawn by glamour's stock style: off-palette, 7–11× the visible bytes, full-width](#p8613--markdown-is-drawn-by-glamours-stock-style-off-palette-711-the-visible-bytes-full-width)
    - [P86.14 — Live reasoning is streamed whole into the transcript tail](#p8614--live-reasoning-is-streamed-whole-into-the-transcript-tail)
    - [P86.15 — Mid-turn narration is drawn identically to the answer](#p8615--mid-turn-narration-is-drawn-identically-to-the-answer)
    - [P86.16 — Every mark is a hard-coded literal; there is no glyph tier and no ASCII floor](#p8616--every-mark-is-a-hard-coded-literal-there-is-no-glyph-tier-and-no-ascii-floor)
    - [P86.17 — No contrast or theme lint; the palette is ~32 role fields with no measured relationships](#p8617--no-contrast-or-theme-lint-the-palette-is-32-role-fields-with-no-measured-relationships)
    - [P86.18 — The working shimmer emits an escape pair per rune and steps by tick count](#p8618--the-working-shimmer-emits-an-escape-pair-per-rune-and-steps-by-tick-count)
    - [P86.20 — Blank rows are added by hand at each append site, with no spacing authority](#p8620--blank-rows-are-added-by-hand-at-each-append-site-with-no-spacing-authority)
    - [P87.1 — The live workflow tier scores pass/fail and elapsed time; it never records how many steps or tokens the run took](#p871--the-live-workflow-tier-scores-passfail-and-elapsed-time-it-never-records-how-many-steps-or-tokens-the-run-took)
    - [P87.2 — Traces are read one session at a time; nothing aggregates failure modes across sessions](#p872--traces-are-read-one-session-at-a-time-nothing-aggregates-failure-modes-across-sessions)
    - [P87.3 — Eval scenarios always start at turn 0; there is no "resume from a recorded state and vary one thing"](#p873--eval-scenarios-always-start-at-turn-0-there-is-no-resume-from-a-recorded-state-and-vary-one-thing)
    - [P87.4 — Skills cannot be induced from recorded runs (design reading; sequenced behind P87.2, P87.3 and P85.5)](#p874--skills-cannot-be-induced-from-recorded-runs-design-reading-sequenced-behind-p872-p873-and-p855)
    - [P87.5 — No way to inject faults into tool results on a live run (design reading, low priority)](#p875--no-way-to-inject-faults-into-tool-results-on-a-live-run-design-reading-low-priority)
    - [P88.3 — Sampling parameters for a local Ollama model are set only by hand-built Modelfiles](#p883--sampling-parameters-for-a-local-ollama-model-are-set-only-by-hand-built-modelfiles)
    - [P88.4 — A run can end on a failing build or test with no report, or an empty one](#p884--a-run-can-end-on-a-failing-build-or-test-with-no-report-or-an-empty-one)
    - [P88.5 — Repeated failed edits to one file have no "stop patching, rewrite it" exit](#p885--repeated-failed-edits-to-one-file-have-no-stop-patching-rewrite-it-exit)
    - [P88.6 — `edit_section` is exposed for code, and fails on it with a message the model does not act on](#p886--edit_section-is-exposed-for-code-and-fails-on-it-with-a-message-the-model-does-not-act-on)
  - [Verification Work](#verification-work)
    - [P88.7 — `GoFixTask` and its repeated live test are built; no live sitting yet](#p887--gofixtask-and-its-repeated-live-test-are-built-no-live-sitting-yet)
    - [P80.4 — `live_workflow`'s two standalone tests both need a stronger model than this machine has run](#p804--live_workflows-two-standalone-tests-both-need-a-stronger-model-than-this-machine-has-run)
    - [P38.1 — Non-orchestrated, single-context threat-model build (primary path for local models)](#p381--non-orchestrated-single-context-threat-model-build-primary-path-for-local-models)
    - [P68.4 — The triage rubric's measuring band sits below the strongest local model](#p684--the-triage-rubrics-measuring-band-sits-below-the-strongest-local-model)
    - [P62.8 — The prefix-cache gate's large-window regime has never been measured](#p628--the-prefix-cache-gates-large-window-regime-has-never-been-measured)

---

## Status

**2026-10-21 — cloud-and-local harness pass: P88.1, P88.2, P86.12, P86.19 shipped; P88.7 built and
moved to Verification Work; three unfiled defects found and fixed.** Done at the user's request, in their
order: cloud-model correctness first, then the P88 local fixes, then a model capability table with live
discovery, then the setup flow, then dialog polish. Found while doing it and fixed, none previously filed:
**(1)** `provider.think: true` sent `{type:"enabled", budget_tokens}` to every Claude model — a 400 on
everything from Opus 4.7 on — and the adapter ignored the guard's thinking suppression entirely; **(2)**
`internal/cost` priced every Opus at the 4.0 rate ($15/$75), overstating current spend 3–4× and tripping
`budget_usd` early; **(3)** saving `/config` replaced the whole `provider:` block, silently deleting
`fallback`, `headers`, `keep_alive`, `compat` and every other key the menu does not render. New, also
shipped: `provider.cache_ttl`, Anthropic `reasoning_effort`, `provider.small_provider` (hybrid
local/cloud small model behind `allow_cloud_small_model`), `GET /models/remote`, first-run setup. Full
record: [releases](releases/releases-01.md#2026-10-21-cloud-and-local-harness-pass). Tier 4 open count:
**45** (49 − 4); Verification Work: **5** (4 + P88.7). **Not live-verified:** no request reached the real
Anthropic API (every new request shape is checked against recorded wire formats and the published API
rules only), and no local model was run. **Suite note:** `TestWindowsSpecialNameRule` (`internal/sandbox`)
and `TestLatexBuildResolvesCitationsLive` (biber on this Mac) fail on `main` before any of this work — not
fixed here, and not caused by it.
**P88 batch filed, 2026-10-01 — seven Tier 4 items, none built** (**P88.1**–**P88.7**). From a live trial,
at the user's request, of `ornith-1.5:9b` and `aegis-qwen35-9b:32k` driven through `aegis chat` on one
small task (fix a divide-by-zero, add a table-driven test, run `go test`): the untuned ornith left an
uncompilable file with no report, the qwen build left a broken import and an empty reply, and ornith with
the qwen build's **sampling parameters pinned and its template unchanged** passed. What it points at:
`old_string not found` carries no hint (**P88.1**), `write_file` accepts literal-`
` content
(**P88.2**), nothing tells a user their local model runs at Ollama's default sampling (**P88.3**), a run
can end on a failing command with no report (**P88.4**), repeated failed edits to one file have no
rewrite exit (**P88.5**), `edit_section` is exposed for code (**P88.6**), and there is no repeatable
scenario to measure any of it (**P88.7**). **One run per configuration, one prompt** — the sampling effect
in particular is suggestive, not established; **P88.7** is the item that decides it. Full entries:
[Open Work — Tier 4](#open-work--tier-4). Tier 4 open count: **49** (42 + 7). No item has a fired trigger;
see each entry's own **Promote when**. Start with **P88.1**/**P88.2** (small, local, fix failures seen
directly) or **P88.7** if measuring first.

**P87 batch filed, 2026-09-21 — five Tier 4 items, none built** (**P87.1**–**P87.5**). Comparative reading
of google-research/envharness (public, Apache-2.0; a framework that wraps frozen RL benchmarks in
composable `Setup`/`Rules`/`Link` layers and runs an LLM "designer" loop that diagnoses a policy's weaknesses
from its trajectories), done at the user's request. **Most of it does not transfer** — the RL/verl-agent
pipeline, benchmark bridges, task composition and model-written executable rules have no Aegis counterpart
or need. What did transfer is evaluation and diagnosis method, and it is mostly cheap: the live tier records
no step/token counts (**P87.1**), traces cannot be aggregated across sessions (**P87.2**), eval cannot resume
from a recorded state (**P87.3**), and two research items sit behind those — offline skill induction from
paired runs (**P87.4**) and fault injection (**P87.5**). Each Aegis claim was checked against this tree at the
file cited; the EnvHarness claims rest on its README and three source files read through a summarising fetch —
it was **not cloned, built or run**. Full entries: [Open Work — Tier 4](#open-work--tier-4). Tier 4 open
count: **42** (37 + 5). No item has a fired trigger; see each entry's own **Promote when**. **P87.1** is the
one worth doing first if any is.

**P86 batch filed, 2026-09-20 — twenty Tier 4 items, none built** (P86.1–P86.11 from the first
reading of what CodeAF *does*; P86.12–P86.20 from a second, same-day reading of how its TUI is
*implemented* — see the note before P86.12 for what that second reading did and did not verify). Comparative reading of
Agent-Field/CodeAF (public, Apache-2.0, a Go coding harness built for cloud open-weight models;
cloned to `research/CodeAF/`), done at the user's request. It is a much larger product than Aegis
(3,347 Go files; a ~186k-line TUI, a SQLite event graph, OpenRouter lane routing and hedging, standing
orders, remote attach, media and connectors), and **most of it was judged not to transfer**: its
routing, hedging, crew and model-pool machinery presumes many endpoints per model, and Ollama is one
connection option there with none of `internal/ollamainfo`'s `num_ctx`/residency handling — on local
models Aegis is the more developed of the two. What did transfer is harness hygiene and TUI polish,
recorded as **P86.1**–**P86.20**, ordered within each reading by importance/effort. Every claim about Aegis's own code
was checked against this tree at the file cited (**P86.1**'s failure text was reproduced with a
scratch program); the claims about CodeAF rest on reading its docs and the headers of roughly 45 files
and were **not** independently verified by building or running it. Not filed, with the reason: its
lane/hedge/waiting-controller/crew/model-pool stack (one lane per local model), its resident/standing/
tenure layer (a different product scope), its omission of `max_tokens`/temperature/reasoning (the
opposite of what local runs need), and its saved-JS-program runtime (`internal/jsrun`; large, and
`internal/drive` already covers the phased case). A grep of `internal/persona`, `internal/sysprompt`
and `internal/skills` for the work-authorizing phrases its research notes flag ("consider multiple
approaches", "be thorough" and similar) found none, so no prompt-wording item was filed. Full entries:
[Open Work — Tier 4](#open-work--tier-4). Tier 4 open count: **37** (17 + 20). No item in this batch
has a fired trigger; see each entry's own **Promote when**.

**2026-09-13 — P68.4's rubric shipped; P65.4 found almost entirely already shipped and corrected in
place; P68.5 and P68.6 closed as not worth pursuing on this hardware.** Continuation of the same day's
roadmap validation below.

- **P68.4 shipped in full.** The security-triage fixture (`internal/eval/triagefixture.go`,
  `triagetask.go`) gained a sixth planted issue needing a three-file trace (`ssrf` in `dispatch.py`,
  reachable only by following `wire.py` → `jobs.py` → `dispatch.py`, one hop past the old hardest
  issue), a `severity_accuracy` criterion (2 pts, scored only against true positives — a wrong severity
  used to cost nothing) and a `no_scope_creep` criterion (1 pt, penalizing edits outside the two FIXME
  fixes). Rubric is now 16 points, `PassMark` 12 (same 75% ratio as before). New tests:
  `TestTriageSeverityScoring`, `TestTriageScopeCreepPenalizesUnrelatedEdits`; the full suite and
  `go build ./...` are green. **Not yet live-run** — closing the item still needs a sitting against
  `aegis-qwen35-9b:32k` to see whether the harder rubric actually gives it room below the ceiling.
- **P65.4 corrected, not built further.** Investigating what to implement found both of the entry's
  named "cheap slices" already shipped and unrecorded: `internal/opregister` (2026-08-21) and per-tool
  `replay: "safe"|"never"` (`tool.ReplayClass`, also 2026-08-21, extended 2026-09-02), the latter wired
  into `repairOrphanedToolUses` exactly as the entry described and covered by
  `TestRepairOrphanUsesSafeWordingForReplaySafeTool`. Nothing was built; the entry text is now accurate
  and its remaining scope (full-turn resume, a mutable ledger) is the large piece it has always said
  not to build without a real loss report. See the entry below for the corrected write-up.
- **P68.5 and P68.6 closed, not removed silently.** The user would use a re-pull only if the installed
  Phi models could stand in; they can't. Both `aegis-phi4-reasoning:16k` and `phi4-mini-reasoning:3.8b`
  share one Ollama chat template (blob `f4dd2368e6c3`) that renders `{{ .Content }}` per message and
  never references `.ToolCalls` anywhere in the template — not the "renders content *or* tool calls"
  defect P68.2's detector catches (`{{ else if .ToolCalls }}`), but the same failure family
  unconditionally: an assistant turn that made a tool call is invisible in every subsequent turn's
  history regardless of whether it also produced content, unless Ollama's runtime injects that text by
  some means the template itself never touches. That is a worse confound for both items than either was
  filed against, not a substitute for it. Removed rather than deferred, since re-pulling
  `qwen2.5-coder:1.5b`/`qwen3:14b` was declined; see releases.md for the closing record.

**Roadmap validation, 2026-09-13 — no item closed or re-tiered in this half; stale claims corrected in
place.** Every open entry's claims about code were re-checked against this tree, and every claim about
models against what is actually installed (`~/.ollama/models` manifests, Ollama 0.30.10). The installed
set is now `aegis-qwen35-9b` (`:16k`/`:32k`), `aegis-phi4-reasoning:16k`, `phi4-mini-reasoning:3.8b`,
`llama3.2:3b` and `nomic-embed-text`; `gemma4:12b`, `qwen2.5-coder:1.5b` and every `qwen3:14b` build the
Verification entries described as on hand are gone. **P62.8**'s "no model on hand" no longer holds —
the 9b advertises a 262,144-token window, though whether its cache fits is unmeasured. **Deferred by
the user until hardware that runs a model stronger than the 9b:** **P80.4**'s 14-file-chain half and
**P38.1**'s conformance run. Everything else checked out as written — P81.7's upstream blockers (Ollama
PR #8072, issue #739) are both still open.

**P85.4 shipped in full, 2026-09-10, and P85.8 folded into P67.11 the same day.** The read-only
fork-lineage tree view: `/tree` opens a foldable tree over every session's `/fork` ancestry, nesting
each fork under the session it branched from rather than Ctrl+Y's flat, unrelated list. Needed a small
new piece of storage the entry itself flagged as missing — a `parent_id` column
(`internal/session/session.go`'s `SetParent`, wired from `handleFork` in `internal/server/sessions.go`)
recording which session a fork branched from, since nothing tracked that edge before. `internal/tui/sessiontree.go`
builds the forest from a flat `[]api.SessionMeta` via `ParentID`, flattens it depth-first into
tree-connector-prefixed rows, and supports `Space` (fold/unfold the node under the cursor) and `Tab`
(cycle "all sessions" ↔ "this lineage only", narrowing to just the current session's own ancestors and
descendants). This is deliberately the "smaller, non-storage-changing slice" the entry itself named —
read-only, no editable branch labels, no interaction with **P65.5**'s still-unfiled "lanes" idea; the
`parent_id` edge is additive metadata, not the lanes design. **P85.8** (a persistent cross-turn goal
object) was filed as "adjacent to P67.11" from the same batch and is folded into that entry's own
write-up rather than kept as a separate item — see [Open Work — Tier 4](#open-work--tier-4)'s P67.11
entry. Full record: [releases/releases-01.md#p854-shipped-2026-09-10](releases/releases-01.md#p854-shipped-2026-09-10).

**P85.3 shipped in full, 2026-09-10.** Browse and reorder for the TQ8 queued-follow-up-message list
(`internal/tui`), the smallest item from the P85 batch, built at the user's request ahead of any fired
trigger. `Alt+↑`/`Alt+↓` browse `m.composer.queued` — a new `queuedSel` cursor into it — rendering the
selected entry's full text where the rest of the queue still shows the truncated one-line summary;
`Ctrl+Alt+↑`/`Ctrl+Alt+↓` swap the selected entry with its neighbor, and the selection follows the swap
so repeated presses walk an entry to the front or back. No new storage: this surfaces the drain queue
TQ8 already had rather than building a second one, matching the entry's own framing. The selection
resets on every mutation the queue wasn't computed against — auto-send draining the front entry, or an
interrupt/error discarding the rest — so a stale index never survives past the shape it was set for.
Full record: [releases/releases-01.md#p853-shipped-2026-09-10](releases/releases-01.md#p853-shipped-2026-09-10).

**P85.2 shipped in full, 2026-09-10.** A usage-vs-window fallback for silent context truncation,
closing the blind spot in `IsContextOverflowError`/`classifyStreamMessage`
(`internal/provider/errors.go`), which only ever matched an error message's text — nothing caught a
backend that accepted an oversized prompt and answered as if nothing were wrong. Implemented once, in
`internal/provider/sse.Run` (shared by all three adapters), rather than duplicated per adapter: usage
reported at the stream's end is compared against the request's known serving window
(`provider.Request.NumCtx`) for the two documented failure shapes — a clean end of turn after prompt
tokens alone reached the window, or a `max_tokens` stop with zero output — and reported as the same
context-truncation error a message match would have produced. Full record:
[releases/releases-01.md#p852-shipped-2026-09-10](releases/releases-01.md#p852-shipped-2026-09-10).

**P85.1 shipped in full, 2026-09-10.** The standout item from the P85 batch filed the same day — a
declarative `provider.compat` config block for the `openai` adapter (`max_tokens_field`,
`requires_tool_result_name`, `supports_strict_mode`), closing the "needs a new Go option and a
rebuilt binary" gap the entry named for a divergent OpenAI-compatible local or third-party backend.
Two quirks from the comparison source's own schema — `requires_assistant_after_tool_result` and
`thinking_format` — were deliberately left out: the former needed message-sequence surgery with no
concrete reported failure behind it, the latter is already handled by the decoder's existing
union of `thinking`/`reasoning`/`reasoning_content` deltas. Full record:
[releases/releases-01.md#p851-shipped-2026-09-10](releases/releases-01.md#p851-shipped-2026-09-10).
Built at the user's request, ahead of any fired trigger.

**P85 batch filed, 2026-09-10 — eight Tier 4 items; four (P85.1, P85.2, P85.3, P85.4) since shipped,
one (P85.8) folded into P67.11.** Comparative reading of PrimeIntellect-ai/prime-agent (public,
MIT-licensed TypeScript coding agent — not leaked, so this carries none of the P67/P74 batches'
reproduction restriction, but each item is still written as an independent Go mechanism, not a port)
against this tree, done at the user's request. Every claim about Aegis's own code was checked against
this tree at the file cited; the claims about the comparison source were not independently verified
beyond reading its own code and rest on that reading alone. The rest range from a persona
self-refinement design (**P85.5**) to genuine paradigm-level design readings recorded but explicitly
not recommended without a driving need (**P85.6**). Full entries: [Open Work — Tier
4](#open-work--tier-4). Tier 4 open count: **17** (14 pre-existing + 3 remaining). No remaining item in
this batch has a fired trigger; see each entry's own **Promote when**.

**P81.28 closed in full, 2026-09-04.** The remediation bullet left open after 2026-09-03's
provenance-labeling half — never promote a prose-parsed tool call that reproduces a span of untrusted
content already in the turn — turned out to need no new taint mechanism: P81.1's
`taint_after_untrusted_content` rule had already shipped in full on 2026-08-31, and this item's own
"what remains" text simply hadn't caught up. `internal/provider/prosetaint.go` indexes a turn's
`trust.Wrap`-marked tool results by 32-byte shingle hash; `salvageToolCall` now refuses to promote any
candidate call whose body reproduces an indexed span, skipping only that branch rather than the whole
reply. Full record:
[P81.28 closed in full](releases/releases-01.md#p8128-closed-in-full-2026-09-04).

**P66.17/LLM-11 closed 2026-09-04, live-verified.** Failover was riding the primary model's serving
window to every fallback target, at both the runtime request layer and the adapter-construction
layer; live-reproduced with two locally pinned models and a forced failover before fixing, then
re-verified live against the fixed binary. Full record:
[P66.17/LLM-11 closed](releases/releases-01.md#p6617llm-11-closed-2026-09-04). This was the sole
open remainder of the P66/P67 sweep below — P66.17 is now closed in full.

**P66/P67 sweep, 2026-09-03 — P66.18, P66.20 and P66.23 closed in full; P66.17 closed except
LLM-11 (itself closed the next day, above); GAP-04 shipped out of P66.19.** Taken on direct request
to review every open P66 and P67
item and build what still needed building. Fifteen findings shipped, **three refuted against current
code** (QUAL-08's
unbounded probe is already bounded by a 5s `ctxWinDetectTimeout`; LLM-12's `/api/show` is the sole
source of `Result.ModelMax`; PERF-06 is already memoized on `reg.SchemaVersion()`), and **one
deliberately left open at the time** (LLM-11 — three plausible fixes, and choosing needed a live
failover measurement).

**The most important outcome was not on the list.** P66.23/VULN-08 was filed as Low-severity
tidiness — Windows device names and ADS unrejected, "code reading; not executed". Executing it found
a **live plan-mode escape**: `splitShellWords` strips backslashes, so
`Get-Content C:\Users\x\.ssh\id_rsa` becomes the drive-relative candidate `C:Usersx.sshid_rsa`, which
`absCandidate` folded into `<root>\C:Usersx.sshid_rsa` and `escapesRoot` called confined — so
`classifyShellCommand` downgraded the command to `CapRead`, allowed silently in plan mode, while
PowerShell would have read the real key. `FuzzClassifyShellCommand` seeds #52/#58 were passing *by
accident of the folding*. This is the CRIT-2 family, and it was live on Windows. Full record:
[The P66/P67 sweep](releases/releases-01.md#the-p66p67-sweep-2026-09-03).

**Updated 2026-09-03.** Tier 1: **0 open**. Tier 2: **0 open**. Tier 3: **0 open** — the P81
threat-model batch (filed 2026-08-31) is fully resolved except two Tier 4 items (P81.11 closed the
same day it was re-tiered, P81.31 shipped in full, P81.18 shipped except one explicitly-deferred
helper — see below); full closing
record in
[releases.md](releases/releases-01.md#threat-model-2026-08-31--the-p81-batch-closing-record).
Tier 4: **14 open** — parked build items, none with a fired trigger; see
[Open Work — Tier 4](#open-work--tier-4). Verification: **6 open** — code already written,
waiting on a live-model run; see [Verification Work](#verification-work).

**P66.26 closed, P66.18/P66.23 partially shipped, 2026-09-03** — the first pass over the P66 residue
grab-bags, superseded the same day by the sweep above, which closed the rest. Full record:
[releases.md](releases/releases-01.md#p6626-closed-p6618p6623-partially-shipped-2026-09-03).

**P81.11 closed 2026-09-03.** Its residual scheduling question — whether `govulncheck` gets a
schedule of its own independent of the disabled `ci.yml` — turned out to already be answered: a
weekly-cron `govulncheck` job has run in `codeql.yml` since 2026-08-07 (P63.2, commit `899912b`),
**before** the threat model that filed this item even ran. The entry's own text describing the
question as open was stale from the moment it was written. No code changed to close this; full
record in [releases.md](releases/releases-01.md#p8111-closed-p817-and-p8128-partially-shipped-2026-09-03).

**P81.31 shipped in full, P81.18 shipped except its trust-store helper, 2026-09-03.** P81.31/FIND-31:
a per-session checkpoint byte cap with oldest-first eviction
(`cleanup.checkpoint_max_session_mb`), and a rewind-time confirmation gate that refuses (HTTP 428) to
overwrite a file changed by something other than the agent's own turn until the caller confirms.
P81.18/FIND-18: `aegis ui` now prints the daemon's certificate fingerprint alongside its existing
self-signed-certificate notice, and `docs/configuration.md` documents the supported tunnelled/proxied
path; an automated OS-trust-store-import command was deliberately not built (real, platform-specific,
no fired trigger — documented as a manual step instead). Full record in
[releases.md](releases/releases-01.md#p8131-shipped-p8118-shipped-except-its-trust-store-helper-2026-09-03).

**This document tracks only open work and current counts.** A completed item's full record — what
it was, what building it found, and what was measured to close it — belongs in
[releases.md](releases.md), not here; it moves there the day it ships. (2026-09-03 housekeeping:
several items whose write-ups had drifted back into this file — the drift
[roadmap-status.md](../.claude/skills/roadmap-status.md) warns against — were moved out; see
[the migration record](releases/releases-01.md#roadmap-housekeeping-closed-items-migrated-from-roadmapmd-2026-09-03).)

**Every shipped item is closed against a live-verified test or a live probe run on this machine,
recorded in its release entry — never asserted from reading a diff.** That standard constrains
future work here.

Tiers 1-4 are **build work** — every item there requires writing code that doesn't exist yet.
**Verification work** is its own track, not a tier: every item in it is code that is _already
written_, sitting behind one gate — a live-model run producing evidence the item's closure
condition names.

**No Tier 4 item currently has a fired trigger** — see each entry's own **Promote when** for what
would change that.

**Standing constraints on the open batches.** **The three P67 constraints, which apply to every P67.11–P67.13 entry below and are not repeated
in them.** Those three are what remains open from a comparative reading of the leaked Claude Code
CLI source against Aegis, done 2026-08-16 — P67.10 and P67.14 have since shipped; full batch
record in
[releases.md](releases/releases-01.md#standing-constraints--the-p67-and-p74-external-source-reading-batches-p74-half-closed).

- **That source is leaked proprietary code. Nothing may be transcribed from it.** Each item is a
  design reading — a mechanism and the reasoning behind it — and needs an independent Go
  implementation written from this document, not from that repository.
- **The leak is partial.** `src/utils/**` is absent, so permission internals, `forkedAgent` and
  `toolResultStorage` were legible only through call sites. Where an entry's claim about _their_
  implementation rests on a call site rather than the code, it says so.
- **Every claim about Aegis in these entries was checked against this tree**, at the file and line
  cited, not against the docs. The claims about their side were not, and cannot be — treat them as
  motivation, never as a specification.

**The P66 entries here are deliberately grouped grab-bags** (**P66.17**, **P66.18**, **P66.20**,
**P66.23**), each collecting the Low-severity residue of one review domain, filed so no finding is
lost rather than because any of them should be scheduled. Take one only when already working in
that file. The review itself — six specialist reviewers, an adversarial debate and a
static-analysis pass, 70 findings against HEAD `3c2b57b` — is in [CodeReview.md](CodeReview.md)
with per-finding evidence. **Read the corrections in releases.md before acting on that document
directly:** several shipped items contradict the finding they were built from (VULN-03's
suggested `::ffff:0:0/96` addition would have blocked the entire public internet; LLM-04 drops
_every_ tool call on a 1-based backend, not only trailing ones).

---

## Tiering Criteria

Applies to **build work** (Tier 1-4) only — items requiring new code. Items whose code is already
written and are only waiting on a live-run result belong in [Verification Work](#verification-work)
instead, regardless of how large or urgent the underlying question is.

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Up next

**Last updated: 2026-09-03; re-checked 2026-09-13, row unchanged; the week-of-2026-09-21 order was
added 2026-09-20 below the table** — both items' model-bound halves
are deferred until hardware that runs a model stronger than `aegis-qwen35-9b`. The only genuinely
scheduled row is the live-tier remainder — the
rest of the Verification track (**P66.22**, **P62.9**, **P65.2**) closed 2026-09-01/09-02 by live
evidence and moved to [releases.md](releases/releases-01.md#roadmap-housekeeping-closed-items-migrated-from-roadmapmd-2026-09-03).

| #   | Item                                                                                             | Tier / size  | Why now                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| --- | ------------------------------------------------------------------------------------------------ | ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | **The live-tier remainder** (P38.1, P80.4) — _parked by choice, 2026-08-16_                      | Verification | **P38.1** still needs permission to launch an unattended auto-approving agent — the recipe is `--yes` plus `auto_approve_exec`, and a session hasn't been permitted to launch that unattended. **P80.4** is unchanged: its two standalone tests need a model that will complete the fixture's 14-file read chain, and nothing available here has done that yet. Both remain parked by choice rather than blocked on code — everything load-bearing for either has already shipped (see each entry below).                                                                                                                                                                                                                                                                                                                                                                                                       |

**Implementation order — week of 2026-09-21 (set 2026-09-20, at the user's request).** This schedules
what to build or run next week; it does **not** change any tier. Every Tier 4 item below still has no
fired trigger by its own entry, so building one is an explicit, early promotion made here. Ranking rules,
in order: **(1)** closure-by-evidence items whose code is already written outrank any build (this file's
own standard is "closed against a live-verified test"), and a long live run **starts first** so it runs
while code is written; **(2)** defects *measured on this tree* (P86.12–P86.14) outrank ones only
reproduced (P86.1) or inferred; **(3)** XS/S before M; **(4)** group by file so one code area is entered
once; **(5)** dependencies — P86.15 needs P86.13 and P86.5, P86.19 pairs with P86.4. Assumes one person,
five working days, about one S item per half day and an M item taking one to two days; the plan below is
about 60% of that on purpose. Sizes are estimates from reading (see the caveat under this table).

| #   | Day     | Item                                                                                              | Size    | Why here                                                                                                                                                                                                                       |
| --- | ------- | ------------------------------------------------------------------------------------------------- | ------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | Mon am  | **P68.4** live run against `aegis-qwen35-9b:32k` (background)                                     | run     | The rubric is built and green; only the run is missing. The GPU is otherwise idle while TUI code is written, and TUI tests don't use it. Read the result at the end of the day.                                             |
| 2   | Mon     | **P80.4** forced-overflow fixture (small change, then a run on the 9b)                            | S       | The one half of P80.4 not blocked on hardware. Serialize after #1 — both need the GPU.                                                                                                                                         |
| 3   | Mon     | ~~**P86.12** footer hints as `key verb`; drop the permanent `READY` chip~~ shipped 2026-10-21                            | XS      | Ten-line change, verified defect, no dependency. Do it while `view.go` is open.                                                                                                                                                |
| 4   | Mon–Tue | **P86.14** bounded 3-line live reasoning window + collapsed `thought for Ns · N tok` row          | S       | Measured defect on the models actually in use (`aegis-phi4-reasoning`, the 9B's thinking mode). Self-contained.                                                                                                                 |
| 5   | Tue     | **P86.13 (a)** glamour style built from the colour scheme, trim padded cells, cap the wrap        | S       | Measured (7–11× bytes, off-palette H1) and the most-read surface. Re-run the byte measurement afterwards and record before/after in the entry. Stop at (a); (b) only if (a) leaves a defect.                                 |
| 6   | Tue     | **P86.18** shimmer: clock-based phase, merged same-colour runs, grapheme-aware widths             | XS–S    | Same rendering-cost theme as #5; do after it so the two are measured separately.                                                                                                                                               |
| 7   | Wed     | **P86.1** one tool-argument decoder (accept `10.0`, model-vocabulary refusals) + two-strike rule  | S–M     | Reproduced defect, mechanical migration of 62 call sites behind a guard test. Ranked below the measured TUI items only because no real incidence is recorded (see its entry: history scan inconclusive).                     |
| 8   | Wed     | **P86.2** line-novelty "going nowhere" detector, nudge-first                                      | S       | Same `internal/engine` area as #7. Nudge-only, so it cannot end a run that was working. Same evidence caveat as #7.                                                                                                            |
| 9   | Thu     | **P86.3** exit ladder + `stop` word; make the documented exit 2 real, with a one-release legacy switch | S  | Docs and binary already disagree, so part of this is a bug fix. Breaking for scripts testing `$? == 1`.                                                                                                                        |
| 10  | Thu     | **P86.7** compaction forecast in the status line                                                  | S       | Arithmetic is trivial; the work is finding the per-turn token history. Timebox 30 minutes to locate it — if there is none, move to next week.                                                                                  |
| 11  | Fri     | **P86.17** theme contrast lint over the four built-in themes                                      | S       | Its result is informative on day one: it says whether any built-in already fails, which decides whether more palette work is warranted at all.                                                                                 |
| 12  | Fri     | **P86.16** first step only — a one-cell-width test over every glyph literal                       | S       | Test-only; answers the entry's unverified half (does any glyph measure two cells?) before any vocabulary is built.                                                                                                             |
| 13  | Fri     | **P86.11** write the closed-loop fail-safe rule into `CLAUDE.md`/`docs/`                          | XS      | A paragraph. Costs nothing and applies to the guard changes in #7–#9.                                                                                                                                                          |
| 14  | Fri     | **P86.9** derive the local prompt profile from the window (stretch)                               | S       | Touches the prompt-budget invariants (`TestEffectiveSystem_localProfileBudget`), so it is last and only if #1–#13 are done. Settle the profile once at construction.                                                            |

**Queued for the week after, in this order** (each has a stated dependency or an unfinished
measurement): **P86.4 + P86.19** together (the approval block is where both land; P86.4's protocol half
is unscoped); **P86.5**, then **P86.15** (which needs P86.13 and P86.5); **P86.6** (with the model
picker, which was not read); **P86.8** (embedded manual and its two gates); **P86.10** only after a
skill-load-rate measurement exists; the rest of **P86.16** if #12 finds a real width problem;
**P86.13 (b)** only if (a) leaves a defect; **P86.20** only when a doubled gap or uneven left edge is
actually seen.

**Not scheduled, and why.** Each of these is parked by its own entry and nothing changed that:
**P81.7** (upstream-blocked on Ollama PR #8072/issue #739); **P77.6** (Windows sandbox — "down the road",
no code, and the largest item on the list); **P60.3**, **P65.4**, **P67.13**, **P65.5**, **P67.11**,
**P67.12**, **P85.5**, **P85.6**, **P85.7**, **P64.5**, **P66.19** (each says "do not build speculatively"
and its **Promote when** is unmet); **P61.7** and **P52.14** (no observed miss; the latter has a higher
false-positive risk than the detector it extends — and **P86.2** should run first, since it may change
what a session-scoped detector still needs to catch); **P80.3 remainder** and **P25.9** (structural, no
trigger); and the model-bound Verification halves — **P80.4**'s 14-file chain, **P38.1**'s conformance
run and **P62.8** (whose only test needs the same chain-following model) — deferred until hardware that
runs a model stronger than the 9B, as already recorded in [Status](#status).

**Sizes are estimates from reading, not from building, and the batch had a known bias** — several
past estimates were smaller than filed and one was larger. Treat any size estimate below as a
rough signal, not a commitment.

**This table outranks `scripts/roadmap-status.sh`.** That script reports open items in _document_
order and cannot see the cross-tier ranking. Use it for repo state and for the parse; use this
table for what to take.

**Everything in Tier 4 below has no fired trigger** — see each entry's own **Promote when**
condition for what would change that.

---

## Open Work — Tier 4

**Status: 49 open** (17 before the P86 batch below; **P86.1**–**P86.20**, filed 2026-09-20 from a
comparative reading of Agent-Field/CodeAF; and **P87.1**–**P87.5**, filed 2026-09-21 from one of
google-research/envharness; and **P88.1**–**P88.7**, filed 2026-10-01 from a live trial of two local 9B
models — see [Status](#status)). Eight pre-existing (all blocked or explicitly parked, none with a fired
trigger), one from the P81 threat-model batch (**P81.7**, parked for a stated reason — see its
entry; mechanism half shipped 2026-09-03, remainder upstream-blocked),
one from the P66 review batch (**P66.19**'s GAP-07 remainder),
three from the P67 external-source reading (**P67.11**, **P67.12**, **P67.13**), **P77.6**
(spun out of P66.19), and three from the P85 comparative reading against PrimeIntellect-ai/prime-agent
(**P85.5**–**P85.7**, filed 2026-09-10 — see [Status](#status); **P85.1**, **P85.2**, **P85.3** and
**P85.4** shipped the same day, and **P85.8** folded into **P67.11**'s own write-up, so none of the
five are in this count). **P81.11** closed 2026-09-03 (see
[Status](#status)) rather than staying in this count; **P81.31** shipped in full and **P81.18**
shipped except its explicitly-deferred trust-store helper, both 2026-09-03 (see
[releases.md](releases/releases-01.md#p8131-shipped-p8118-shipped-except-its-trust-store-helper-2026-09-03)).
**P66.26** closed in full the same day (see [Status](#status)). **P66.18**, **P66.20** and
**P66.23** closed in full 2026-09-03 in the P66/P67 sweep; their write-ups moved to
[releases.md](releases/releases-01.md#the-p66p67-sweep-2026-09-03). **P66.17** closed in full
2026-09-04 (its LLM-11 remainder resolved live — see
[Status](#status)). **P81.28** closed in full 2026-09-04 — see
[Status](#status). Everything else that was ever
in this tier has shipped — see
[releases.md](releases/releases-01.md#roadmap-housekeeping-closed-items-migrated-from-roadmapmd-2026-09-03)
for the closed-item record.

**How to use this tier.** Every Tier-4 item that has actually been measured so far turned out to
be wrong in some way — an unmeasured dependency that was actually our own code, a gate unmeetable
by the work it proposed, a cap that wasn't the largest one in the tree. Take the measurement
first, then re-read the item; do not treat a Tier-4 write-up as a build plan. Details of past
measurements are in [releases.md](releases.md).

**Below, the still-open items are ordered by importance/impact/benefit — highest first — rather
than by filing order or ID.** A scored, threat-model-sourced finding with real CVSS weight
outranks a residue grab-bag; a correctness bug users would actually notice outranks a speculative
feature idea; an item a prior sitting explicitly judged not worth its cost sits last. This is a
reading order, not a build queue — nothing below has a fired trigger, and each item's own
"Priority: Tier 4" line and "do not build speculatively" caveat still governs whether to act on
it.

**The remaining P85 items are, like the P66 grab-bags, kept together as one block** (at the end of
this list) rather than interleaved into the importance ordering above, for the same reason: they
share one filing date and one comparison source, and reading them together matters more than their
individual rank. **P85.1**, **P85.2**, **P85.3** and **P85.4** all shipped 2026-09-10 (see
[Status](#status)) and **P85.8** folded into **P67.11** the same day — none of the five are in this
block anymore. Within the remaining block, **P85.6** is the least actionable and is recorded chiefly so
the design reading is not lost.

**The P86 items follow the P85 block, also kept together** (one filing date, one comparison source,
Agent-Field/CodeAF), and are ordered within it by importance/effort — **P86.1** first, because it is a
reproduced gap in the place a small local model fails most; **P86.10** and **P86.11** last of the first
reading, because the first is gated on a measurement nobody has taken and the second is a paragraph.
**P86.12**–**P86.20** are the second reading (TUI look and feel), in the same order: **P86.12**
(XS) and **P86.13**/**P86.14** (the measured defects) first, **P86.20** last because it only earns its
cost if a spacing defect is actually visible. The **week-of-2026-09-21 implementation order** across the
whole roadmap is in [Up next](#up-next). Unlike the P67 batch,
CodeAF is public and Apache-2.0, so the reproduction restriction does not apply — but each entry is
still an independent Go mechanism, not a port.

**The P87 items follow the P86 block** (one filing date, one comparison source,
google-research/envharness), ordered by cost and dependency: **P87.1** (XS–S, test-package only) and
**P87.2a** (S) first because they need no model and everything later reports through them; **P87.3** next,
the one with open design questions; **P87.4** and **P87.5** are research readings, sequenced behind the
rest, and **P87.5** is the one most likely never to be built.

**The P88 items follow the P87 block** (one filing date, one source: a live trial of two local 9B models),
ordered by value and cost: **P88.1** and **P88.2** first (S each, `internal/tool/builtin`, fix failures seen
directly); **P88.7** is the measurement that gates **P88.3** and should come first if the order is "measure,
then change"; **P88.4**–**P88.6** last, each small and each possibly made unnecessary by **P88.1**.

### P81.7 — The local model endpoint is unauthenticated plaintext HTTP on loopback (FIND-07)

**Filed 2026-08-31**, from the threat model
([**FIND-07**](../threat-model-20260831-002123/3-findings.md#find-07-the-local-model-endpoint-is-unauthenticated-plaintext-http-on-loopback),
CVSS 7.1, `Important`, CWE-319). The default local deployment sends every prompt — workspace file
contents included — to `http://localhost:11434` over plaintext HTTP with no authentication in either
direction. A local process with packet-capture privilege reads the whole conversation; a local process
that binds the port first, or wins a restart race, answers **as the model** and dictates what tool
calls the agent attempts next.

**The argument for taking it seriously is the project's own.** `server.tls.enabled` defaults to true
specifically because "plaintext HTTP still leaves the bearer token and full conversation content
readable to another local account on a shared host with packet-capture privilege." The content on the
provider hop is identical and there is no authentication at all. `validateBaseURL` exempts loopback
endpoints from the plaintext refusal deliberately, "matching how such setups already work today" — a
compatibility decision, not a security one.

**Shipped 2026-09-03: the bearer-token half, plus hardening documentation.**
`provider.local_auth_token` (`internal/config/config_provider.go`, `ProviderConfig.HeadersFor`) sends
`Authorization: Bearer <token>` to any target `config.LocalBackend` resolves as local, wired into both
call sites in `providerfactory.Build` (primary and fallback), and an explicit `headers.Authorization`
still wins over it. `docs/installation.md` now documents the honest limit: **Ollama itself does not
check the header** — there is no upstream config to require it — so the token alone does not close
either threat against a bare `ollama serve`. What it does is give an authenticating reverse proxy
placed in front of Ollama something to enforce (the documented pattern: Aegis → HTTPS+bearer → proxy
→ plaintext loopback → Ollama, with the proxy also terminating TLS), and it closes the gap directly
against any other local OpenAI-compatible server that does validate bearer tokens (llama.cpp's server,
vLLM, LiteLLM). `TestProviderConfig_HeadersFor` pins the local-only scoping and the explicit-header
precedence.

**Checked 2026-09-03: the Unix-socket/named-pipe path is not viable today.** `OLLAMA_HOST=unix://...`
support is [PR #8072](https://github.com/ollama/ollama/pull/8072) against upstream Ollama, still open
and unmerged as of the most recent check; the original request
([issue #739](https://github.com/ollama/ollama/issues/739)) has been open since 2023 with nothing
shipped. No Windows named-pipe support has ever been proposed. This confirms the original entry's own
caveat — "half the remediation is upstream" — was optimistic rather than merely cautious; there is
currently no upstream socket/pipe surface to build against at all.

**What remains.** TLS-with-pinned-cert direct to a local provider is subsumed by the documented proxy
pattern (the proxy terminates TLS) rather than needing `client.NewFromConfig`'s pinning machinery
extended to a second endpoint type — not filed separately. The socket/pipe path stays parked on
upstream.

**Why Tier 4 rather than higher.** The prerequisite is another local account, or a hostile process
already running as someone on this host, on a single-user development machine. Promote the
remaining socket/pipe question if Ollama ever merges PR #8072 (or ships Windows named-pipe support) —
that's also the condition that promotes **P81.24**'s encryption half.

Priority: Tier 4 — mechanism half shipped; the remainder is upstream-blocked, not a local decision.

### P77.6 — No OS-level process sandbox on Windows (GAP-05, spun out of P66.19)

`internal/sandbox`'s OS-level (no container runtime) backend covers darwin (`sandbox-exec`/seatbelt)
and linux (`bwrap`) in `detectOSSandbox()` (`internal/sandbox/os_sandbox.go`) — there is no
`case "windows"`. A Windows host without podman/docker/WSL installed falls all the way through to
`LocalBackend`: commands run directly on the host with no filesystem or network confinement at all,
the one platform where `sandbox.go`'s backend-selection order has nothing OS-level to fall back to.
Conspicuous because the rest of the Windows story (PowerShell shell tool, path handling, CI) is
otherwise handled well.

**Recommended direction, from the user (2026-08-25):** a Job Object (resource/kill-on-close
containment) plus a restricted access token (drop admin/write privileges outside the workspace) —
native Windows primitives, no new external dependency. AppContainer (the same primitive UWP apps
use) is stronger but was explicitly set aside as higher-complexity for a general-purpose CLI
subprocess model; revisit only if the Job Object + restricted token approach proves insufficient in
practice.

Priority: Tier 3/4 — down the road, not speculative-build-now. No code changes yet.

### P60.3 — Checkpoints capture files only, so `/rewind` is silent about everything else

`internal/checkpoint` snapshots each file a write tool touched (capped at 16MiB) and rewind writes
those contents back — correct within its documented scope. Rewinding a turn that ran `pip install`,
applied a DB migration, started a background process, or wrote a >16MiB artifact restores the source to
pre-turn state while leaving the environment in post-turn state, and the user is told the turn was
undone. If a session owns a persistent container (P60.2, shipped 2026-08-05), a checkpoint could be a
container snapshot/commit instead, making rewind honest about installed packages and process state.

**Update 2026-08-25:** `sandbox.backend`'s default is now `"container"`, cascading to `"os"` and then
`"local"` when no container runtime is available (`SelectSandbox`, internal/server/server.go). A host
with Docker/Podman running now gets the container backend by default. This satisfies the first half of
the promote condition below — the container backend is now the default a session lands on wherever
Docker/Podman is actually present — but the container-commit checkpoint mechanism itself (the actual
fix this item describes) has not been built yet.

**Promote when:** someone builds the container-commit checkpoint mechanism now that the default flip
has landed, or a user reports a rewind that restored files into an environment that no longer matched
them.

Priority: Tier 4 — no longer blocked, but speculative until someone is actually rewinding inside a
container.

### P65.4 — Resume is phase-granular, artifact-inferred, and only the drive has it

**What Aegis has, stated carefully — the gap is narrower than it first looks.** The phased drive
already resumes well: a phase whose files carry no `PENDING` marker costs zero model turns on re-entry,
and the whole reset ladder is built on re-entering from disk. Two limits:

- **It is phase-granular and the granularity is the artifact** — the oracle is the `PENDING` marker in
  the skill's own scaffolded files, so a crash 40 turns into phase 6 re-runs phase 6 from its start.
  Probably the right trade at the drive's scale.
- **It exists only because the _skill_ supplies the oracle.** A plain TUI/web-UI session, a cron job, a
  swarm sub-agent, an `aegis chat` run with no skill — none of these has artifacts with markers, so none
  resumes at all. Kill the daemon mid-turn and the turn is gone. What survives is bookkeeping only:
  P65.1's `repairOrphanedToolUses` patch, made durable across a restart by `internal/opregister` (see
  the correction below).

A fully durable version would still need a mutable namespaced register / append-only usage ledger — one
register overwritten with the operation's complete current state after every step, so recovery reads
that one row and switches on it rather than replaying a journal — and the ability to resume a whole
*turn*, not just account for one interrupted tool call.

**Why Tier 4 and must not be built speculatively.** The session store is SQLite and already has the
storage substrate. But **nobody has reported losing work this way** — the drive, the only workload long
enough for a crash to be expensive, is also the only one that already resumes.

**Corrected 2026-09-13: both of the cheap slices this entry pointed at already shipped, unrecorded —
this entry had drifted further from the code than any other item found in this pass.**

- **The durable started-tool record.** The step this entry named as the place to start — P65.1's
  `startedTools` record "written through the session store before the effect and cleared after" —
  exists as `internal/opregister` (added 2026-08-21, commit `e13bd13`): an `operation_registers` table
  on the session DB, `MarkStarted` just before a tool's `Execute` and `MarkFinished` after, with
  `newEngine` (`internal/server/engine_build.go`) seeding `engine.Options.InitialStartedTools` from
  `Pending`, so a session resumed after a daemon restart reports an orphaned call as "may have run"
  rather than "never started". `opregister_test.go` covers it.
- **Per-tool `replay: "safe" | "never"`.** Also shipped the same day (`internal/tool/tool.go`'s
  `ReplayClass`/`ReplayClassifier`/`EffectiveReplay`, extended 2026-09-02 in P67.10's tool-interface
  sweep), and wired into exactly the synthetic-result path this entry described:
  `repairOrphanedToolUses` (`internal/engine/toolexec.go`) calls `tool.EffectiveReplay` on every
  "may have run" orphan and, when the tool declares `ReplaySafe` — every read-only builtin
  (`read_file`, `grep`, `glob`, `ls`, the LSP tools, `repomap`, `task_list`/`task_get`,
  `web_fetch`/`web_search`, project knowledge) — swaps the universal "verify before assuming" wording
  for one telling the model the call is idempotent and safe to just reissue.
  `TestRepairOrphanUsesSafeWordingForReplaySafeTool` (`internal/engine/orphanrepair_test.go`) pins it,
  citing P65.4 by name.

Neither piece was ever recorded here or in releases.md; only the audit's architecture notes and the
code's own P65.4 references mention them. **What remains** is only the large, explicitly-parked half:
resuming a whole turn outside the drive, and the mutable current-state register. Both are exactly what
"must not be built speculatively" below already governs — nothing here is a small next step anymore.

**Promote when:** a real run loses work to a daemon restart or crash outside the drive, or a non-drive
workload grows long enough that it would (unattended cron chains, long-lived swarm sub-agents are the
two candidates).

Priority: Tier 4 — no trigger. Both cheap, valuable slices (P65.1 in memory, `internal/opregister`
across a restart, per-tool replay safety) have shipped; what's left is the large speculative redesign
this entry has always argued against building without one.

### P67.13 — There is no way to execute a plan without committing to it

Plan mode describes intent; it cannot show the diff that intent would produce, because producing the
diff means performing the writes. The mechanism that resolves this is a **copy-on-write overlay**:
writes are redirected into an overlay directory after copying the original in, reads of
already-written paths are served from the overlay, everything else reads through to the real tree, and
execution stops at the first effect the overlay cannot contain — a non-read-only shell command, a
network call, anything outside the workspace — recording a typed boundary describing where and why it
stopped. Accepting promotes the overlay into the workspace; discarding costs a directory delete.

Aegis has most of the substrate: `internal/sandbox` for isolation, `internal/swarm` for forked runs,
`internal/checkpoint` for per-turn restore, and a permission layer that already classifies calls by
capability. **P67.8**'s flag-level classifier (shipped 2026-08-18) is what would decide the shell boundary
precisely rather than conservatively.

The comparison source uses this for _speculation_ — predicting the user's next prompt during idle time
and pre-executing it. **That half is not recommended.** The prediction is the expensive, risky,
low-confidence part; the overlay-and-boundary machinery is the durable part, and its first consumer
should be an honest dry-run mode, where the value does not depend on guessing right.

**Promote when** the overlay has a named first consumer — a `--dry-run` that shows a real diff is the
plausible one. Building the overlay with no consumer produces an untested second write path, which is
strictly worse than not having it.

**Design note written 2026-09-03** at the user's direction, in place of a speculative build:
[research/design-plan-overlay.md](design-plan-overlay.md). It exists so the build is cheap the day a
consumer appears, and it does **not** change this item's tier. Three things in it are worth knowing
before anyone schedules this:

- **The load-bearing assumption was measured and came back half-wrong.** The plan was to install the
  overlay beneath the existing context-carried `checkpoint.Snapshotter` seam so no write tool would
  change. All nine write sites do reach that seam, but the call is
  `checkpoint.SnapshotterFrom(ctx).Capture(abs)` — it *notifies* the snapshotter, which copies the
  old contents away, and the tool then performs its own `os.WriteFile`. It observes; it does not
  intercept. The overlay is still buildable there, but it needs the seam upgraded from notifying to
  path-resolving across nine sites, and `captureShellWrites` (two of the nine) does not fit the shape
  at all — it discovers written paths by watching the filesystem, so it has no path to redirect and
  stays a boundary in v1.
- **Still unmeasured:** `Snapshotter.claim` is first-touch-wins because checkpointing wants the
  *oldest* content per path; an overlay wants the *newest*. Same set, opposite update rule. Sharing
  the type without noticing that is a quiet correctness bug of exactly the kind this tier keeps
  producing.
- **The obvious consumer name is taken.** `aegis dry-run` already exists
  (`internal/cli/dryrun.go`) and is a *configuration* preview — resolved config, tools, memory and
  context, without calling the model. It executes nothing and is unrelated. A consumer needs a
  different name, or `dry-run` needs a subcommand split.

Priority: Tier 4 — no fired trigger, L. Do not build speculatively.

### P80.3 remainder — `Server`'s auth/lockout and context-window field groups

**P80.2 (the three unread packages) and the first slice of P80.3 (the `Server` struct's per-session
`sync.Map` family) both shipped 2026-09-03 on direct request.** Full record:
[releases.md](releases/releases-01.md#p802-and-p803-2026-09-03). P80.2's investigation closed
`internal/tui` and the sandbox container backends clean (correctness only, no permission-posture
findings — the questions the original entry named all check out) and found one real defect in
`internal/drive`: budgets were accumulating across a phased drive's shared `cost.Tracker` instead of
resetting per phase-turn as `internal/engine/budget.go`'s own doc comment says they should. That was
fixed the same sitting (`runBudget` now baselines the tracker's totals per `Run`), closing P80.2 in
full — nothing left to promote.

**P80.3 is only partly closed.** The struct half of the audit's L4 named three field groups legible
from `Server`'s eight mutexes: the per-session `sync.Map` family (now grouped into `sessionMaps`,
reachable as `Server.sess` — the slice that shipped), the auth/lockout state (`authToken`, `tlsCert`,
`invalidAuthAttempts`, `authLockMu`/`authConsecutiveFailures`/`authLockedUntil`,
`pageTokenMu`/`pageTokens`), and the context-window state (`ctxWinMu` guarding `ctxWin`, `ctxWinSrc`,
`ctxWinFinal`, `ctxWinByModel`, `autofitWin`, `weightsSeen`). The other two groups are still flat fields
on `Server`. Unlike the per-session maps, both remaining groups had non-`sync.Map` state guarded by a
plain `sync.Mutex`, so grouping them is a larger rewrite (real fields with real zero-value defaults some
tests do set directly, not a `sync.Map` every test already skips) — worth its own sitting rather than
folding into this one speculatively.

Promote when: someone is already changing the auth/lockout or context-window state's shape, or a bug
turns up that a grouped struct would have made visible (the M6 archetype — a per-session map
`handleDeleteSession` forgot — doesn't apply to either remaining group, since neither is per-session
state needing session-scoped cleanup; the risk here is closer to "a lock protects fields that turn out
not to all be adjacent in the source").

Priority: Tier 4 — no fired trigger, M. Structural and ongoing by the audit's own framing; the file
half and now the per-session-map slice already bought most of the legibility.

### P66.19 — Capability gaps with no fired trigger

Assessed against what a mature coding agent needs, and honestly reported as absent rather than
planned. The user chose to act on a prioritized subset (2026-08-25), and four of the six findings
here have since shipped — see
[releases.md](releases/releases-01.md#qual-04-and-gap-02030809--resolved-sub-findings-with-no-other-record)
for that record. GAP-05 was spun out to its own item, **P77.6** (still open).

**GAP-04's branching half shipped 2026-09-03** in the P66/P67 sweep, on the argument that it does not
need a trigger: the `git` tool refused branch operations and its own error text told the model to use
`shell`, so the safe, routine operation (branch before editing) was gated harder than the dangerous
one — an inverted risk gradient, not a missing feature. `git_branch`
(`internal/tool/builtin/gitbranch.go`) is `CapWrite` with `list`/`create`/`switch`/`delete`, `-d`
only and never `-D`, and is deferred under `LocalProfile` (its schema is 146 tokens and
`localBasePromptCeilingTokens` had none to spare). Record:
[The P66/P67 sweep](releases/releases-01.md#the-p66p67-sweep-2026-09-03).

**What remains here.** GAP-04's *worktree* half — `internal/worktree` still exposes no tool, which
would let parallel sub-agents work on isolated checkouts (M, and worth more once swarm workloads
grow) — and **GAP-07** (the MCP server side lags the mature client: `internal/mcpserver` exposes
exactly three tools with no `resources/*` or `prompts/*`, so a third-party harness cannot discover
Aegis's personas or skills through MCP even though both are prompt-shaped; `roots/list` on the client
side is the natural companion, since `workspacetrust` already holds the answer). Both re-verified
against code 2026-09-03 and both unpromoted.

Priority: Tier 4 for the two remaining items (GAP-04's worktree tool, GAP-07) — no triggers. Do not
build speculatively.

### P64.5 — `ask_user` is one free-form question; unattended answers cannot be routed

`internal/tool/builtin/ask.go` is one question string, an optional `[]string` of choices, a string
back. A batch answered by anything other than a human at a terminal — the non-interactive
`Questioner`, a future policy answerer, a parent agent answering for a sub-agent — has to match answers
to questions by question text today, since there's no caller-supplied `id` echoed in the answer; the
text is model-authored and may repeat. A structured error taxonomy (user-cancelled vs
no-provider-registered) would also let an unattended drive respond differently to those two outcomes,
which deserve opposite responses.

Against building it: `ask_user` is close to unusable in the unattended drive that is Aegis's main
proving ground today (`AutoAnswer` returns a fixed string), so the routing problem is real but has no
current caller.

**Promote when:** something other than the TUI is answering questions — a policy answerer, a parent
agent answering for a spawned one, or a drive phase that legitimately needs to stop and ask.

Priority: Tier 4 — no trigger, no current caller. Do not build speculatively.

### P61.7 — Retry/terminal classification over _backend-echoed_ text (remainder)

`classifyStreamError` decides whether a mid-stream failure is retried or fatal by substring match
against a free-form server error string. The in-repo half shipped 2026-08-06 (Aegis's own OpenAI
adapter was splicing model-authored tool names into a message the classifier then matched — fixed via
`APIError.Detail`, rendered but never classified). **What's left is the case originally described:** a
server or proxy echoing generation fragments into its own `{"error":…}` envelope, where the text is
genuinely external. Still unmeasured, and a fix means guessing at a structural signal (status code, an
error `type` field) most local backends don't supply.

**Promote when:** a misclassification is actually observed, or a backend is found that demonstrably
echoes generation content into its error envelope.
`TestModelAuthoredTextDoesNotSteerClassification` exists as a regression test for the shipped half;
extending it to envelope text is the natural next probe.

Priority: Tier 4 — narrowed to the external case; real surface, unquantified likelihood, no incident.

### P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)

`newLoopDetector` is constructed inside `Run`, so its window resets every call. In the TUI and web UI
each user turn is a separate `Run`, so a model that loops _across_ user turns (re-reading the same file
every time the user nudges it, re-running the same failing command after each correction) is never
detected. Fix: hoist the detector to session scope via an optional caller-owned detector in
`engine.Options`, so the daemon can hold one per session while the CLI keeps today's per-`Run`
behavior. Open design question: a user legitimately asking for the same call twice across two turns
isn't a loop, so a session-scoped detector likely needs a higher threshold or a reset rule keyed on
whether a user message is a bare retry — fuzzier than the current mechanism.

**Promote when:** a live run shows a cross-turn loop that per-`Run` detection missed.

Priority: Tier 4 — real but unproven, and the false-positive risk is higher than the current detector's.

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of six daemon-singleton services are per-session-scoped; `lsp.Manager` was deliberately left
shared — its per-session resource-growth tradeoff was judged worse than the isolation gap. Parked
pending a concrete multi-tenant need.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

### P65.5 — Rewinding away from a branch discards its work instead of summarizing it forward

`internal/checkpoint` gives per-turn restore points and the TUI has `/rewind` and fork; rewinding
restores, it does not carry anything forward. Correct default for the common case (the user wants the
last turns gone) but wrong for the case of exploring an approach, learning something real, abandoning
it, and then watching the model rediscover the same dead end because the transcript no longer contains
it. A branch-navigation summary — find the common ancestor, summarize the abandoned span, append it as
an entry on the target branch, same structured format as P65.2's compaction skeleton and file
tracking — would fix it, offered rather than automatic (or `/rewind` stops meaning "undo").

**Why Tier 4 and not higher.** Downstream of P65.2 (shares the summary format and file tracking —
designing it twice would be wasted work), and there's no complaint behind it: `/rewind` has not been
reported as losing anything a user wanted.

A wider version — **lanes**, named cursors into one shared session tree, each owning its own leaf,
model config, queue, and at most one in-flight operation — would be a cleaner model for
`internal/swarm` sub-agents than spawning goroutines with separate histories, but is a session-storage
rewrite with no complaint behind it either; not filed.

**Promote when:** a real session loses reasoning worth keeping to a `/rewind` or a fork.

Priority: Tier 4 — no trigger, sequenced behind P65.2 (shipped), changes what a well-understood command
means.

### P67.11 — Every budget is a ceiling; none expresses how much effort is wanted

`internal/engine/budget.go` is entirely ceilings — `BudgetUSD`, `MaxTokensPerRun`, `MaxIterations`,
`MaxWallClockPerRun`, `MaxTurnStall` — and all of them abort. There is no knob meaning "this task is
worth 200K tokens of thoroughness, keep going until you have spent it or stopped making progress."

The inverted form is coherent: nudge the run to continue while spend is below a target, and stop early
when returns diminish — the workable test being several consecutive continuations whose token deltas
are each below a small threshold, so a run that is still finding things keeps going and one that is
circling stops.

If ever built, it must be a **separate opt-in target with its own resettable stop**, not a second
meaning layered onto an existing knob. A value that is simultaneously a floor and a ceiling is a
footgun, and the existing abort semantics are load-bearing and asymmetric — stall and wall-clock
aborts are fatal to a drive, loop and tool-failure aborts are resettable. The natural home is
`internal/drive`, where "keep working until this phase is genuinely done" is already the model.

**Promote when** a real drive run ends early with budget unspent and work unfinished. The
diminishing-returns stop test is worth remembering separately from the rest of the idea; it is the
part most likely to be useful in another context.

**Folded in, 2026-09-10: P85.8** (persistent cross-turn goal object with token-budget accounting, from
the P85 comparative reading of PrimeIntellect-ai/prime-agent) proposed the same gap from the other
side — their `goal` skill holds one persistent objective plus a token budget that re-prompts the agent
across turns until it explicitly calls `goal.complete()`. That is a lighter-weight version of this
same idea for a plain session rather than a phased drive: state the objective once, keep a running
budget, let completion be an explicit model action rather than an external stop condition. Not filed
as its own item — same underlying gap with a smaller surface, and building it as a separate track
would mean designing "keep going until genuinely done" twice.

Priority: Tier 4 — no fired trigger, speculative. Do not build speculatively.

### P67.12 — Personas cannot accumulate anything across runs

Personas are stateless prompts, and memory is session- and project-scoped. A persona that learns
something durable about this codebase — a build quirk, a convention, a place where the obvious
approach fails — has nowhere to put it that survives the run.

The shape worth copying is a per-persona memory directory in one of three scopes: **user** (shared
across projects), **project** (committed, shared with the team), and **local** (project-specific,
never committed). The third is the one that carries most of the practical value, and it maps onto the
`~/.aegis/personas/` vs `.aegis/personas/` split Aegis already has.

Two implementation constraints, both cheap and both easy to miss: normalize paths before the
containment check so `..` cannot escape the memory root, and sanitize the persona name for the
filesystem — namespaced names carry characters Windows rejects outright.

**Promote when** a persona is in repeated use on one project and its operator is re-explaining the
same context each run. Until then this is storage without a demonstrated reader.

Priority: Tier 4 — no fired trigger. Do not build speculatively.

### P85.5 — Agent-triggered self-refinement of persona, memory and skill definitions is not represented

**Filed 2026-09-10**, same comparative reading. This is the closest thing to genuine
"self-improvement" found in the source (`prime-agent-runtime/src/rlm/`, described on GitHub as "a
self-improving RLM agent"). Its refinement harness (`refinement.ts`, `skills/refine/`) lets the agent
itself flag, mid-turn, a repeated failure or reusable tactic worth keeping; a separate LLM-judged
pass — scheduled, never applied mid-turn — then makes evidence-logged CRUD edits to a small store of
`prompt`/`memory`/`skill`/`subagent` entries and rebuilds the system prompt before the agent resumes.

This generalizes **P67.12** (personas cannot accumulate anything across runs, still open) from "give
a persona a memory directory" to "let the agent decide what's worth keeping and log why." P67.12's
per-persona memory-directory design (user/project/local scopes, the path-containment and
filename-sanitization constraints it already names) is the storage substrate this would write
through; the refinement half — an agent-initiated, turn-boundary-gated, evidence-logged write path —
is the new piece. The turn-boundary gating is load-bearing in their design specifically to avoid a
mid-turn self-edit destabilizing the run in progress, the same reason `internal/compaction` and
`internal/checkpoint` already avoid mutating state mid-round.

**Promote when:** P67.12 promotes — a persona in repeated use whose operator is re-explaining the
same context every run. This is the mechanized version of that same trigger.

Priority: Tier 4 — no fired trigger, L, sequenced behind P67.12 — do not build the refinement pass
before the memory substrate it would write to exists.

### P85.6 — Non-blocking recursive sub-agent spawn as a language-level call (design reading, not recommended)

**Filed 2026-09-10**, same comparative reading. `prime-agent-runtime`'s `rlm()` is a callable inside
the agent's persistent Python REPL: `await rlm("sub-task")` spawns a full child agent session and
returns an admission handle immediately; the child reports back only via explicit message-passing
(`agent_message.send`), never a return value the parent blocks on.

`internal/swarm`'s sub-agents are dispatched as ordinary tool calls the engine's parallel-round
machinery already governs — `execLock`, `RoundResultCap`, the per-round path-dependency graph, the
stall heartbeat (all documented in this file's own "Invariants worth knowing" section). Moving to a
non-blocking spawn-and-message-later model means either rebuilding those same guarantees on a second,
out-of-round channel, or losing them for anything spawned that way — the invariants list is long
precisely because each entry closed a real defect (P8.6, P67.4, P65.1, CRIT-4). This is recorded as a
design comparison, not a recommendation: it is architecturally a different paradigm from Aegis's
per-round tool dispatch, not a bolt-on.

Priority: Tier 4 — no fired trigger, XL, **not recommended without a concrete driving need** (the
same explicit caveat P67.13 used for the speculation half of the overlay idea) — `internal/swarm`'s
current blocking model has no reported problem this would solve.

### P85.7 — Agent-owned internal heartbeat schedule with steer-vs-follow-up delivery, distinct from cron

**Filed 2026-09-10**, same comparative reading. Their `rlm-heartbeat` skill lets the agent schedule
its own recurring internal prompt (default 5 min) with two explicit delivery modes: `steer`
(interrupt the current turn) or `follow_up` (queue until idle) — separate from any user-facing
schedule.

`internal/cron` already runs scheduled jobs, and `internal/heartbeat` already exists as the
stall-detection beat chain this file documents ("a sub-agent's watch must never shadow its parent's")
— but that is liveness plumbing, not an agent-authored recurring self-prompt. The distinguishing
feature worth naming is the explicit steer-vs-follow-up choice per schedule, which `internal/cron`'s
job model doesn't have: a cron job runs as a new session/turn, it doesn't choose to interrupt a live
one.

**Promote when:** a drive or long-running session needs to check on itself periodically without
waiting for the next user turn — the shape of need `internal/drive`'s phased builds already partly
cover for a fixed multi-phase build, but not for an open-ended session.

Priority: Tier 4 — no fired trigger, M, narrow scope — the closest existing analog (`internal/drive`
phases) already covers the scheduled-build case.

### P86.1 — A wrong-typed tool argument reaches the model as Go's own JSON error

**Filed 2026-09-20**, from a comparative reading of Agent-Field/CodeAF (see [Status](#status)). The
highest-value item of the batch: a verified gap in the exact place a small local model fails most.
`parseArgs` (`internal/tool/builtin/builtin.go`, ~488) is `fmt.Errorf("invalid arguments: %w", err)`
around `json.Unmarshal`, and it has **62 call sites**. Reproduced with a scratch program: a model that
sends `{"limit":10.0}` — which several providers do, since JSON has no integer type — is told
`invalid arguments: json: cannot unmarshal number 10.0 into Go struct field args.limit of type int`;
`{"limit":"10"}` and `{"path":5}` produce the same shape of message. That sentence names a Go type, a
struct the model has never seen and a value it has every reason to believe was correct, so it cannot
act on it and re-sends the same call. `internal/provider/argshaperepair.go` already repairs the
*shape* (double-encoded object, redundant `arguments`/`parameters` wrapper, bare scalar for a
one-property schema) but deliberately never validates a value, so a wrong *type* falls straight
through to the message above.

CodeAF hit exactly this (a model called one tool with `{"limit":10.0}` eleven times in a turn and the
turn ended on its loop guard) and its answer (`internal/session/toolargs.go`) has three parts worth
lifting: **one decoder** every tool goes through, with a structural test that fails the build when a
tool calls `json.Unmarshal` on its own arguments; **accept the whole-valued float** wherever an
integer is wanted (10.0 is ten; 10.5 is still refused); and **every refusal is one short sentence in
the model's vocabulary that names the argument and the corrected call** — `limit takes a whole number:
send {"limit":10}, not 10.0` — never a Go type. It also refuses a numeric string rather than coercing
it, on the stated ground that a decoder that accepts a form nothing sends is inventing a dialect.

The companion half is a faster loop rule: an identical call refused twice for its arguments is a loop,
not three times, because the refusal already carried its own cure, so the second identical call is
proof the correction went unread. `internal/engine/toolfailure.go`'s nudge is at three consecutive
failing rounds and its abort at six, and `loopdetect.go` keys on repeated signatures — neither has a
faster path for a refusal that names its fix.

**Promote when** a live run on `aegis-qwen35-9b` (or any local model) shows a tool re-sent unchanged
after an argument error — the loop transcript is the trigger. Cheap to check first: grep a recent
session for `invalid arguments: json:`. **Checked 2026-09-20 and inconclusive:** a keyword count over a
copy of the local `sessions.db` (63 sessions, 2026-06-25 to 2026-08-09) found **no** `invalid arguments:
json:`, `cannot unmarshal`, `Go struct field`, loop-abort or tool-failure-limit text (the copy was
deleted afterwards). The scan was **not validated** against the stored message format — a follow-up
that would have confirmed it covered tool results was not run — and the history predates the current
9B setup, so treat "no observed incidence" as weak evidence, not as a finding. The defect itself (the
error text) is reproduced; how often a real model hits it is not known.

Priority: Tier 4 — no fired trigger, S–M (one decoder plus a mechanical migration of the call sites
and a guard test; the two-strike rule is a few lines in `toolfailure.go`). **First in the batch.**

### P86.2 — No detector for distinct, successful tool calls that teach the model nothing new

**Filed 2026-09-20**, same comparative reading. `internal/engine/loopdetect.go` catches exact cycles
of period 1–4 (`maxLoopPeriod`) over canonicalized tool-call signatures; `toolfailure.go` (P52.3)
catches consecutive all-error rounds. Neither sees the third shape: a model issuing *different*
calls that all *succeed* and all return mostly the same text — a search rephrased five times, or a
test script re-run with a timestamp on its first line. No signature repeats and nothing errors, so
both guards stay quiet while the run burns iterations. (Grep for novelty/silent-batch handling under
`internal/engine` found none.)

CodeAF's `internal/session/novelty.go` answers it with a **line-level novelty ratio**: for each
result, what fraction of its non-blank lines (trailing whitespace trimmed, nothing else normalized,
the tool name part of the key) has this run never been told; a step "taught something" only if more
than half were new (`taughtLineThreshold = 0.5`, a constant, not a setting, "because a threshold a
task could raise would be a threshold a stuck task raises"). The memory is two rotating generations
of 4,096 line hashes, so it is bounded by construction. Five consecutive rounds that teach nothing
earn a note, not an abort. Its `looped.go` adds a **silent-batch** rule (six consecutive tool rounds
with no visible assistant text, for a reasoning model re-deriving a plan that vanishes at each step
boundary), **hysteresis** (one successful call not already nudged about resets every streak;
re-escalation needs several repeats; the *nudge count* steps down by one only on material progress —
a file written or a shell command that changed the tree — never on narration), and a rule that
results the harness itself answered (a withdrawn tool, a refused call) are never counted against the
model.

The comparison is with **P52.14** (a session-scoped detector — cross-`Run` loops are invisible, still
open): both are "the existing detectors have a blind spot"; this one is about *what* is compared, not
*how long* it is remembered, and they compose.

**Promote when** a run is observed spending its iteration budget on successful, non-repeating calls.
Until then this is the same "detector without an observed miss" shape P52.14 already is.

Priority: Tier 4 — no fired trigger, S (~100 lines plus tests; nudge-first, so it cannot end a run
that was working).

### P86.3 — Non-interactive runs exit 0/1/2 and say why they stopped only in free text

**Filed 2026-09-20**, same comparative reading. `docs/cli-reference.md` documents three exit codes
(0 success, 1 general error, 2 usage error), but `cmd/aegis/main.go` does `os.Exit(1)` for **every**
error `cli.Execute` returns, so the documented 2 never appears — the docs and the binary already
disagree. The `--output-format json` result (`internal/cli/chat_output.go`'s `chatResult`) carries
`answer`, cost, turns, token counts and an `error` string, but no machine-readable statement of *why*
a run ended. Meanwhile the engine already distinguishes the causes with typed sentinels —
`ErrInterrupted`, `ErrWallClockLimit`, `ErrTurnStalled`, `ErrLoopDetected` (`internal/engine/engine.go`)
and `ErrToolFailureLimit` (`toolfailure.go`), plus the budget errors — so a script driving `aegis chat`,
a cron job or a drive can only pattern-match the message text to learn whether it hit a limit it set
or genuinely failed.

CodeAF's headless contract (`docs/design/polish/envelope-and-exits.md`, `cmd/codeaf/envelope.go`)
puts every headless verb on one ladder and one JSON object: **0** done; **1** could not be run at all
(no key, bad arguments); **2** ran and did not finish (part of the work does not stand); **3** a limit
*you set* stopped it (wall, token budget, turn cap, price); **4** it needs an answer and nobody was
there — plus a `stop` field naming why in one word, "the same word on all three commands, and not
going to move again". It also shipped a `CODEAF_EXIT_CODES=legacy` switch for one release and kept
every old JSON field beside its new spelling, because changing exit codes is a change to what
existing scripts see. The mapping here would be near-mechanical (`errors.Is` against the sentinels
above), with the same care.

**Promote when** aegis is driven from CI, cron or `drive` by something that needs to branch on the
stop cause, or when the doc/binary disagreement above is fixed anyway (the cheapest first slice is
making the documented 2 real).

Priority: Tier 4 — no fired trigger, S. Breaking for scripts that test `$? == 1`; ship the legacy
switch.

### P86.4 — A pending approval cannot be questioned, only answered

**Filed 2026-09-20**, same comparative reading. The approval block (`internal/tui/approval.go`) offers
allow once, allow always (pattern-scoped, `suggestRulePattern`), deny, and deny-with-feedback
(`apprAllowOnce`/`apprAllowAlways`/`apprDeny`/`apprDenyFeedback`). There is no way to ask *about* the
call — "why this path?", "what does that flag do?" — without deciding it. Deny-with-feedback is the
nearest thing and it costs the call.

CodeAF's permission block has two extra keys beyond answering: `c` **change** ("say what you want
different, then enter") and `?` **ask back** ("type your question, then enter"); the question is
answered by the model and the same block is asked again. Two related details are cheap to copy: the
decision leaves a **reopenable row** in the transcript (`needs your ok to run bash → allow once · you ·
20:21 · c change`), and a call whose outcome cannot be taken back drops the widening "always" choice
(Aegis already does the latter — `allowAlwaysLabel`'s "no safe rule; write one by hand" case). A
countdown that never auto-answers is decoration for Aegis and is not proposed.

Not verified: how much the daemon-side approval request/response protocol (`internal/server`) would
need to grow — the approval is answered over the API, so "ask back" needs a new response kind that
holds the call, runs a side turn and re-issues the request. That is the whole cost of the item.

**Promote when** a session shows a user denying a call only to learn something about it.

Priority: Tier 4 — no fired trigger, S–M (TUI half is small; the protocol half is the unknown).

### P86.5 — A long agent turn has no fold above the per-call and read/search-group level

**Filed 2026-09-20**, same comparative reading. Aegis folds consecutive successful read/grep/glob calls
into one group card (P74.4, `internal/tui/toolcard.go`'s `toolGroup`) and lets each result be expanded
independently (P75.1). Nothing folds a *turn*: a 9B model doing 20–30 calls to answer one question
leaves that whole run in the transcript above its answer.

CodeAF's `internal/tui3/workfold.go` collapses settled machinery and its narration into one chip per
**phase** of a turn — `▸ worked · 10 tool calls · 41s · ctrl+e` — while the latest paragraph and the
live frontier stay visible. Three details are worth keeping: it is **render-time structure, derived
from the entry list and never journaled** (replay derives the same fold; a person's expansion dies
with the window); chips default shut and the *reader's* expansion is the reader's own state, kept in
one map; and there are three doors — a key, a click, and scrolling up into it — because "discoverability
before purity". A design note in the same repo records the owner *reversing* an earlier "a task page
folds nothing" rule for the reason that it optimised the rare audit at the cost of the common visit.
Its companion `caption` design (a deterministic one-line composite of a batch's calls when the model
said nothing before them) would need no extra model call, which matters locally; it is **plan-only in
CodeAF, nothing built**, so it is a design reading, not a tested mechanism.

**Promote when** long tool runs are reported as burying answers, or a session's transcript is
observed to be mostly tool cards.

Priority: Tier 4 — no fired trigger, M (render-time derivation in `internal/tui`; no storage).

### P86.6 — Narrow-terminal rows are cut with an ellipsis, not laid out by fact priority

**Filed 2026-09-20**, same comparative reading. `internal/tui/view.go` truncates sidebar rows with a
single `truncate(text, w)` at about ten sites (model, sandbox, cron, tool rows, file rows), and the
status/footer hints are joined into one string. At narrow widths that slices a fact in half — CodeAF's
own audit found a picker drawing `1M · $3/$15 per…` (a price cut in half is a wrong number) beside
`anthropic/claude-son…` (a name that could be either of two models), and footers cut mid-word so a
hint named a key and then ate it.

`internal/tui3/rowfit.go` is a pure, deterministic fitter with four laws: **the identity is whole or
the row is pointless** (it is truncated only when it cannot fit alone); **facts degrade before they
disappear** (each carries up to three spellings — `$0.08/$0.15 per M` → `$0.15/M` → `$0.15`); **the
tail is a prefix of itself** (facts are added in priority order and the first that will not fit *ends*
the tail, never skipping forward into the gap, so a reader cannot mistake "did not fit" for "not
published"); and **unknown is not narrow** (a fact nobody supplied draws nothing and does not end the
tail). A second helper, `hintFit`, cuts footer hints by *clause*. Their audit method is also worth
noting: capture every screen at 60/80/120/160 columns and pin each fix with a test at those widths.

Not read: `internal/tui/modelpicker.go` and `sessionpicker.go`, the two lists with the most facts per
row, were not opened for this filing; they are the likely first consumers.

**Promote when** a narrow-terminal screenshot shows a cut fact, or when the model picker gains a
column (size/quant/fit) that will not fit at 80 columns.

Priority: Tier 4 — no fired trigger, S (pure function; testable by pasting rows).

### P86.7 — Nothing forecasts the compaction that is about to discard the prefix cache

**Filed 2026-09-20**, same comparative reading. Aegis warns when context is nearly full and nothing
is left to compact (`internal/engine/compact.go`'s "context ~%d%% full…" notice) and shows a usage
figure, but no forecast of compaction itself exists anywhere in the tree (grep). For a local model
that is the more useful warning: a compaction discards the prefix cache and costs a summarization
call, so "compaction in ~3 turns" tells the user to finish or `/compact` on their own terms.

CodeAF's `etaSegment` (`internal/tui3/render.go`) is about twenty lines: average per-turn growth over a
small ring of turn-end token counts, against what remains before the compaction threshold. It is
deliberately **silent in three cases** — the conversation is not growing (so "compaction in ~400
turns" is never shown), the answer is more than five turns away (`etaHorizon`), and compaction is
already due ("a forecast of a thing that is happening is not a forecast"). It is a good instance of
the emptiness law: absence means nothing to report.

Where Aegis holds a per-turn token history for this to read is **not yet located** — the entry's real
work is finding or adding that ring, not the arithmetic. The threshold itself comes from
`tokenest.CompactionTrigger`, which the engine and summarizer already share, so no second rule is
introduced.

**Promote when** a compaction is reported as a surprise mid-task.

Priority: Tier 4 — no fired trigger, S (trivial arithmetic; the ring is the work).

### P86.8 — The model cannot look up Aegis's own features; it improvises them

**Filed 2026-09-20**, same comparative reading. There is no built-in tool that lets the model consult
Aegis's own documentation (`internal/tool/builtin` has none, and `CLAUDE.md`'s embedded-assets list
does not include `docs/`). A local 9B model's training data does not contain Aegis, so "how do I
enable X", "what does `/rewind` restore", "why did you ask me that" are answered from invention.

CodeAF treats this as a first-class feature ("THE MANUAL LAW" in its `CLAUDE.md`): Markdown pages under
`internal/manual/chat/` are compiled into the binary and read by a `manual` tool. The parts worth
lifting are the **gates**, not the tool: the build fails if any slash command or alias, or any tool
name, is missing from the corpus; a table of 100+ real questions in a person's own words must each
still retrieve the page that answers them (and when one reaches the wrong page, "fix the page, never
the test" — write the asker's vocabulary into a `## ` heading, because headings are the search index);
each section stays under ~2,000 characters and self-contained because it is retrieved alone; pages
must state **limits and refusals**, since "no, and here is what it says instead" is the most useful
answer; and when a capability lands, grep the corpus for the page that said it was impossible. Its
own history includes a doc that called `generate_image` impossible for weeks after it shipped, which
is the failure the last rule exists for.

Aegis-specific constraint: under the local profile the tool must be **deferred** (`tool.Summarize`
only in `<deferred_tools>`), and `TestEffectiveSystem_localProfileBudget` still applies. The corpus can
be built from existing `docs/*.md` so there is one source, not a second copy.

**Promote when** a user reports the model confidently misdescribing an Aegis feature.

Priority: Tier 4 — no fired trigger, M (embed + retrieval + the two gates).

### P86.9 — The local prompt profile is chosen from the base URL, not the window it is meant to protect

**Filed 2026-09-20**, same comparative reading. `ProviderConfig.LocalPromptProfile`
(`internal/config/config_provider.go`, ~488) returns true for an explicit `local`, false for an
explicit `default`, and otherwise `isLoopbackBaseURL(BaseURL)` — loopback is the proxy for "small
model with a small window". It is wrong in both directions: a large-window model served on
`localhost` is trimmed for no reason, and a small-window Ollama on another LAN machine gets the full
prompt. The explicit override exists, but the default is a guess about size standing in for the size.

CodeAF's `internal/session/promptprofile.go` arrives at the opposite rule and records why: the profile
derives from **the model's context window alone** (`leanWindowThreshold` = 32,000), with the order
*pin, then settings row, then window, and nothing else* — "there is deliberately no fourth rung — no
model name, no vendor, no crew seat", because an earlier draft keyed on open-weight models and was
wrong about models that have 128k windows when served normally ("open weights are a licence, not a
size"). The three words `auto`/`lean`/`full` are shared by the setting and a one-launch pin.
`internal/ollamainfo` already knows the served window (`Detect`, `RecommendContextWindow`; not read in
detail for this filing), so the fact is on hand; loopback would remain the fallback when the window is
unknown.

Watch item: `TestEffectiveSystem_localProfileBudget` and the invariant that a tool's description never
names a tool the active profile defers both key off the profile, so the derivation must settle **once
per session at construction** (CodeAF's rule — the page and the belt cannot disagree about which
profile they are) rather than be re-derived per request.

**Promote when** a model with a large window on loopback, or a small one off it, is observed on the
wrong profile.

Priority: Tier 4 — no fired trigger, S.

### P86.10 — Skills are reached only if the model chooses to load them

**Filed 2026-09-20**, same comparative reading. Aegis skills inject only `name — description` until the
model calls the `skill` tool (`CLAUDE.md`). Whether a 9B model reliably does so is **unmeasured** —
that measurement is the gate for this item, not the mechanism.

CodeAF's subharness detector (`internal/subharness/detect.go`, documented in `docs/SUBHARNESS.md`)
scores a user turn against each entry's authored `Cues` **without any model call**: pure and
deterministic (same turn, same score, on every machine), independent signals combined as
`1 − Π(1 − w)`, a threshold that must be *exceeded*. The result is **a question, never a routing** —
one row asking "run harness X? enter / esc" — asked once per turn before the first request, only for
text a person typed (never a woken or scheduled turn), and silent in headless runs. Its stated reason
for no model call: an unrequested question must be predictable, and a wrong offer costs trust in every
later card while a missed one costs nothing, so recall is deliberately traded for precision.

**Promote when** a measurement shows the local model not loading a skill that plainly applied.

Priority: Tier 4 — no fired trigger, S–M, and **do not build before the measurement above exists**.

### P86.11 — Guard changes have no written "is this fail-safe actually closed-loop" review rule

**Filed 2026-09-20**, same comparative reading. Documentation only. `docs/design/failsafe/FAILSAFE.md`
in CodeAF was written after five headless failures each hit a fail-safe that already existed and each
slipped past it, and reduces the lesson to one rule: *a fail-safe is closed-loop or it is decoration* —
it **detects by structure, not vocabulary** (a phrase allowlist is always one sentence behind); it
**sources evidence from the world, not from the component it is checking** (diff the tree rather than
trust the tool that claims it wrote a file); it **propagates to the verdict the person reads**; it
**leaves a record that can be autopsied**; and it has **a floor that cannot deliver nothing as done**.

Aegis already leans this way (the plan-mode parser fuzz invariant, `TestEveryEngineCallSiteDecidesItsGate`,
the `repairOrphanedToolUses` "possibly completed" wording), and the rule is worth stating once in
`CLAUDE.md` or `docs/` so a new guard is reviewed against it. Related but separate: **P61.7** (retry
classification over backend-echoed text) is a vocabulary-detection instance of the first clause.

**Promote when** a guard is added or a guard is found to have not fired.

Priority: Tier 4 — no fired trigger, XS (a paragraph).

**P86.12–P86.20 are a second reading of the same source, filed the same day, on how CodeAF
*implements* its look and feel rather than what its TUI does.** Their measurements of Aegis were taken
by running Aegis's own renderer from a temporary test file (since deleted), not by reading; their
claims about CodeAF rest on its code and design docs (`docs/DESIGN-LANGUAGE.md`,
`internal/tui3/styles.go`, `internal/tui2/{tokens,prose}`) and **CodeAF's TUI was not run**, so nothing
here is a judgement of how it looks on screen. CodeAF's stance is deliberate restraint (one lit element
per screen, colour on strokes only, no borders except one frame); Aegis's Crush-derived purple look is a
brand choice and none of these entries proposes replacing it — they take the *mechanisms*.

### P86.13 — Markdown is drawn by glamour's stock style: off-palette, 7–11× the visible bytes, full-width

**Filed 2026-09-20**, second reading. Measured by rendering a sample through `newGlamourRenderer`
(`internal/tui/tui.go`, ~1572) — three findings. **(1) It does not use Aegis's colours.** Only
`glamour.WithStandardStyle("dark"|"light")` is passed; `theme_loader.go:151,153` shows a JSON theme can
choose only *which* of the two (`Dark bool`). The H1 rendered as `38;5;228;48;5;63;1` — bold yellow on a
violet chip, from glamour's stock style — and body text as a fixed `38;5;252`, so `dracula`, `gruvbox`,
`tokyonight` and `catppuccin` never reach prose, and the loudest thing in a reply is the one element no
theme controls. **(2) It is byte-heavy, and worse the wider the terminal.** Glamour pads every line to
the wrap width with one escape pair per cell: for a six-line paragraph plus list, width 80 gave 6,295
raw bytes for 875 visible (7.2×, 374 padded trailing-space cells), width 120 gave 8.7×, width 200 gave
10.8× (1,103 padded cells). That is per-frame output cost on slow terminals and ssh, and trailing
padding a selection or copy can pick up (not tested here). **(3) It wraps to the full terminal width**
(`WithWordWrap(width)`), so a 200-column window gets 200-character lines.

CodeAF (`internal/tui2/prose`, ~2,200 non-comment lines) makes the opposite choices and states why:
goldmark as a **parser only**, the AST walked and painted through one styler ("a second colour
authority on this surface is exactly the failure" its palette layer exists to end); every returned
string is **one screen row with no padding**; paragraphs, lists and quotes wrap to a **reading measure**
(`DefaultMeasure = 88`, clamped to the width) while figures (tables, code) and unbreakable tokens (URLs,
paths) get the full width; **headings promote by grey-ramp tier and whitespace, never size**; tables
**truncate cells with an ellipsis rather than wrapping** so a row is always a row; code is highlighted
only at 256 colours and above ("a lying colour is worse than no colour"); and model text is sanitized
and stripped of its own SGR first because "prose owns its own colours".

Two tiers of fix, cheap one first, and the cheap one may be enough: **(a)** build a glamour
`ansi.StyleConfig` from the active `colorScheme` (fixes the off-palette H1/body/code colours for every
theme), post-process the result to trim padded trailing cells and merge adjacent identical SGRs, and
cap the wrap at a measure; **(b)** replace glamour with a goldmark-AST renderer (goldmark is already a
dependency, `go.mod` indirect). (a) is S; (b) is M–L and should wait for a measured reason (a table or
nested-list defect (a) cannot fix). `internal/tui/streaming.go`'s safe-paragraph-boundary cache and
`latex.go`'s pre-pass both sit in front of the renderer and are unaffected either way.

**Promote when** the off-palette headings are noticed in daily use, or a slow terminal/ssh session
shows redraw lag on long replies.

Priority: Tier 4 — no fired trigger, S for (a) / M–L for (b). **The highest-impact visual item in the
batch**: it is the surface people read most.

### P86.14 — Live reasoning is streamed whole into the transcript tail

**Filed 2026-09-20**, second reading. `refresh` (`internal/tui/tui.go`, ~1153) writes the entire
`thinkText` — `wrap(thinking header + thinkingDim.Render(think))` — into the live tail every refresh,
with no cap. A reasoning model (`aegis-phi4-reasoning`, the 9B's thinking mode) can emit thousands of
tokens before the first answer word, which grows a wall of dim italic that pushes the answer off
screen and is re-wrapped in full on every refresh. It is collapsed only when the turn ends
(`ctrl+o`, ~1314).

CodeAF's `internal/tui3/thinking.go` shows a **three-line reading window**: the last three wrapped
lines, faded by depth so the newest is brightest and no one has to work out which end is new; the
header carries `thinking · 148 tok` while streaming; the moment the first non-reasoning token (or tool
call) lands the block **collapses to one row** — `thought for 6s · 148 tok · ctrl+e` — and `ctrl+e`
opens it again. Two details matter: expanding is allowed *while streaming* ("a model reasoning about
the wrong file is worth catching before it edits one" — the reason to show it at all), and reasoning
is **display-only and never journaled**, so a replay shows answers and not the thinking, stated as a
deliberate honesty ("the alternative is a surface that invents a record the session does not have").
Aegis already has the collapsed/expanded toggle and per-item cache; what is missing is the bounded
live view and the duration/token facts on the collapsed row.

**Promote when** a local reasoning model's think phase is observed to scroll the answer away.

Priority: Tier 4 — no fired trigger, S. Self-contained (one function plus the collapsed-row text);
**good first build**.

### P86.15 — Mid-turn narration is drawn identically to the answer

**Filed 2026-09-20**, second reading. Nothing in `internal/tui` distinguishes assistant prose that
precedes more tool work from the reply that actually answers (grep for narration/demotion handling
found none). "Let me check the config first" is drawn in the same ink, at the same margin, with the
same markdown as the final answer, so an hour of tool-heavy work reads as a wall of paragraphs with no
way to tell which one the reader was waiting for.

CodeAF's `internal/tui3/hierarchy.go` is a structural answer that **needs no cooperation from the
model**: prose followed by more work in the same turn was never the answer (the next tool call opening
under it is the proof); streamed prose stays compact until the response boundary confirms its role
(a tool-free response boundary promotes it); when the turn settles, whatever is last is the answer and
everything above it is working material; and **an interrupted turn promotes nothing**, so the absence
of a full-ink block under the work states plainly that no answer was reached. Demoted narration is
**indented by the machinery's own column, in a narration tier one rung under body ink (not the dim
the surface uses for its own voice), and plain — no markdown at all**, because weight is the one
channel it cannot borrow without lying: a bold lead-in inside narration would render heavier than the
answer under it, inverting the hierarchy. One blank row of "breath" opens a promoted answer under a
turn that did work; a turn with no work renders byte-identically to before (a pinned test). It is a
stamp pass over the entry list, computed once per layout and invalidating only blocks whose tier
changed.

It interacts with **P86.13** (demoted blocks would bypass the markdown renderer) and **P86.5**
(the turn fold — chips would hold the demoted narration), so build after those or design with them.
Aegis's streaming live-tail cache (`liveBlock`) must learn that a block's tier can flip when the next
tool call arrives.

**Promote when** long tool-heavy turns are reported as hard to scan.

Priority: Tier 4 — no fired trigger, M. **The biggest structural readability change** in the batch;
sequence it behind P86.13 and P86.5.

### P86.16 — Every mark is a hard-coded literal; there is no glyph tier and no ASCII floor

**Filed 2026-09-20**, second reading. `internal/tui` spells its marks inline across many files —
`● ✓ × ⎿ ⚡ ⏸ ⧗ ✻ ▸ ▎ ⚇ ⚠` — with no vocabulary, no tier and no fallback (grep for a glyph set or ASCII
tier found none; whether any test constrains glyph width was not checked). Two risks, one
of which is **unverified**: a terminal or font missing a rare codepoint (`⧗` U+29D7, `⚇` U+2687, `✻`
U+273B) draws tofu, and a glyph that measures two cells where the layout assumed one shifts every
column to its right. Aegis's own platform is Windows (Windows Terminal, conhost, and ssh sessions with
whatever font the far end has); whether these render correctly there has not been checked.

CodeAF's answer (`internal/tui2/tokens/glyphset.go`, `internal/tui3/styles.go`): **one door** —
`palette.glyph(id)` resolves a named slot into one of three spellings: the *plain* floor (metric-safe,
"a designed floor rather than a degradation"), an opt-in *Nerd Font* tier (BMP private-use,
Font-Awesome-4-era codepoints only, because later ranges moved between Nerd Font versions), and an
*ASCII* tier that is a spelling for a screen reader and a terminal with no Unicode, one nameable
character per slot. Detection may only **veto** (`TERM` empty or `dumb`, or a non-UTF-8 locale) because
"there is no escape sequence that answers *can you draw U+251C*". The two rules that keep it honest:
**the tier changes which glyph is drawn in a cell, never how many cells a line occupies** (asserted by
a test that every glyph on both sides of every binding measures one cell), and **icons for meaning,
geometry for structure** (state/attention marks are upgradable; separators, rails, gauge steps and
diff signs are not). A structural test fails the build on a mark spelled outside the vocabulary.

**Promote when** a glyph is reported as tofu or misaligned, or Aegis is run in a non-UTF-8 context; do
the one-cell width test first — it is cheap and answers the unverified half.

Priority: Tier 4 — no fired trigger, S (test only) → M (vocabulary + door + literal-ban test).

### P86.17 — No contrast or theme lint; the palette is ~32 role fields with no measured relationships

**Filed 2026-09-20**, second reading. `colorScheme` (`internal/tui/colorscheme.go`) carries about 32
scalar colour fields (brand, four foreground and four background tiers, status roles with up to three
shades each, gradient endpoints) plus an eight-hue agent palette and a 16-colour ANSI remap, assigned from Charm's
`charmtone` names, and four JSON themes (`internal/tui/themes/builtin/`) can replace it. A search of
`internal/tui` found no contrast, luminance or role-collision test, so nothing checks that a theme's
body text is legible on its background, that its dim tier hasn't dropped out of sight, or that two
roles stay distinguishable on a 256-colour terminal.

CodeAF holds its palette to numbers, each asserted by a named test: the reading tiers sit in
**contrast bands** against the terminal ground (body 9.5–12:1 — "the body may not be the brightest
thing on the screen", because above ~11:1 light-on-dark halates; second voice 4.5–7.5:1, the WCAG text
floor; murmur 2.6–4.4:1, "allowed to be quiet, but a tier under 2.6:1 is not quiet, it is gone"); the
**signal hues** (accent, add, del, warn…) sit inside a 15-point HSL lightness band so none outranks
another; and `TestNoTwoRolesShareA256Index` walks the table so no two roles collapse onto one xterm-256
cell. A value already in band is never touched. Its derivation (`adaptive.go`) also **re-derives the
selection/cursor grounds from the terminal's measured background** when the terminal answers the
background query — Aegis already requests the background colour (`RequestBackgroundColor` appears in
`tui.go`/`update.go`) but whether it re-derives anything from the answer was not checked.

The cheap, useful slice is a **lint, not a redesign**: a test (and an `aegis theme check` or
`aegis doctor` row) that computes these ratios for every built-in and user JSON theme and names the
failing role. That guards user-authored themes, which nothing does today.

**Promote when** a user theme is reported unreadable, or a new built-in theme is added.

Priority: Tier 4 — no fired trigger, S (lint + test); the rest of the palette discipline is L and not
proposed.

### P86.18 — The working shimmer emits an escape pair per rune and steps by tick count

**Filed 2026-09-20**, second reading. `shimmerText` (`internal/tui/shimmer.go`) splits the string with
`[]rune` and renders each rune through its own `lipgloss.NewStyle().Foreground(...).Bold(true)` — one
escape pair per character, on every animation tick, for the status line and every pending tool card —
and advances by `step`, a frame counter, so the sweep speed depends on the tick rate. `[]rune` also
splits combining sequences and joined emoji. (Aegis already has `reducedMotion`, which CodeAF also
treats as a first-class tier.)

CodeAF's `captionmotion.go` / `tokens/motion.go` differ in four ways worth taking: **one cadence**
(`MotionInterval` = 120ms, bounded 100–150ms with stated reasons: below ~100ms a braille cycle reads as
noise, above ~150ms it visibly steps) so no surface picks its own; **phase from the clock, not a
counter**, so "a busy terminal may skip pictures without slowing the sign of life" and two rows given
the same instant show the same frame (a repaint inside one step is byte-identical, hence zero dirty
rows); **adjacent grapheme clusters with the same colour share one escape pair** ("most of the line is
the quiet prefix or suffix, so per-character escapes would spend terminal bandwidth on pixels that have
not changed"); and widths measured with `uniseg` in cells. It also caps product-wide motion at three
named things (the spinner, a slow breathe, the cursor) and bounds one cycle at 0.8–2s so that a fast
cycle cannot be mistaken for urgency, which is amber's job.

**Promote when** the shimmer shows lag on a slow link, or with any edit to `shimmer.go`.

Priority: Tier 4 — no fired trigger, XS–S.

### P86.20 — Blank rows are added by hand at each append site, with no spacing authority

**Filed 2026-09-20**, second reading, and **conditional**: it is only worth doing if uneven or doubled
gaps are actually visible. The transcript is built from strings that each carry their own newlines —
`barLabel("You", …) + "\n" + text + "\n\n"` (`tui.go`, ~1494; `stream.go`, ~372), tool cards ending in
`"\n"` and relying on the next item's leading `"\n"` for spacing (`toolview.go` says so in its own
comment), a turn separator appended separately — so every new block type has to remember the
convention and two adjacent blocks can each add a gap.

CodeAF's `spacing.go` is 29 lines and makes it one law: a named ladder (**clause** 0 rows, **block** 1
row, **boundary** 1 row or a hairline with one row of clearance, **breath** 2 rows only above the
composer on tall windows, shrinking as height is taken away), and `separated(rows)` which appends a
blank row only when the previous row has content and is not already blank — "**a gap asked for twice
is still one gap**". Blank rows never open or close a block; the transcript compositor owns them, so
no renderer appends one. A reading gutter of two cells on all conversation text is applied in one
pass after layout for the same reason ("thirty renderers that each remembered to add two spaces would
be thirty places for one of them to forget"), and a blank row stays blank so a copy doesn't pick up
trailing whitespace. Assistant prose in Aegis already sits behind glamour's own two-space margin
(seen in the P86.13 measurement) while the `You` text and tool cards appear to start at column 0, so a
mixed left edge is likely — **not checked on screen**.

**Promote when** a doubled blank row or an uneven left edge is seen in a real session.

Priority: Tier 4 — no fired trigger, M and touching every renderer; last in the batch, do not start
without a visible defect.

### P87.1 — The live workflow tier scores pass/fail and elapsed time; it never records how many steps or tokens the run took

**Filed 2026-09-21**, from a comparative reading of google-research/envharness (see [Status](#status)).
EnvHarness's headline result is not only a pass-rate gain but an efficiency one — "~9.8% fewer interaction
steps" for skills learned in a reshaped environment — because two policies with the same pass rate are not
equally good if one needs a third more turns.

`internal/eval.TaskOutcome` carries `Passed`, `Failures`, the per-criterion `Score`, `Elapsed` and `Err`
(`workflowtask.go`) and nothing about the run's shape, so `Compare` (Aegis vs a baseline harness) and any
before/after comparison of a prompt or persona change can only say pass/fail and how long. The data is
already recorded: every live-workflow test logs its session id and keeps the data dir (`live_workflow_test.go`
— "`aegis sessions trace <logged id>`"), and each `trace.TurnTrace` holds turn count, input/output tokens,
`ToolCalls`, `StopReason` and `Correctives`. Reading the trace back after `RunTask` and adding turns, tool
calls, tokens and correctives-fired to the outcome (and to `String()`) is the whole change. It only works for
an Aegis run: a `CLIHarness` baseline such as `claude -p` has no Aegis session, so the new fields would be
empty on the baseline side and `Compare`'s verdict must not depend on them.

Not checked: whether the `-v` log line already prints enough to make this redundant in practice.

**Promote when** two variants (a prompt block, a compaction setting, a model) tie on pass rate and a choice
between them is still needed — or a live-tier result is being reported and "it passed" is the whole account.

Priority: Tier 4 — no fired trigger, XS–S, test-package only. The cheapest item in this batch and the one
the others would report through.

### P87.2 — Traces are read one session at a time; nothing aggregates failure modes across sessions

**Filed 2026-09-21**, same comparative reading. EnvHarness's core loop is a *designer* that reads the
policy's trajectories, diagnoses a specific weakness, and writes a change aimed at it. Two halves, with very
different costs:

**(a) The aggregate — no model, do this first.** `aegis sessions trace <id>` (`internal/cli/sessions.go`)
prints one session's turns, with a WHY column for stop reason, compaction and injected corrective. The record
underneath is already rich (`trace.TurnTrace`: `StopReason`, `Correctives` — `max_tokens_continuation`,
`shim_format`, `zero_tool`, `empty_answer`, `guard`, `loop` — `Compaction`, `Guard`, `ToolCalls`), but no
command reads across sessions, so the question that decides where to spend effort on a local model — *which
corrective fires most, on which model and persona, over the last N sessions* — needs a hand-rolled loop over
`sessions list`. A `sessions trace --summary [--since] [--model] [--persona]` that counts correctives, stop
reasons, aborts by kind (loop, tool-failure breaker, stall) and turns-per-run would answer it. Pure
aggregation; nothing new is persisted.

**(b) The designer — a model reads failing sessions and proposes a change to a persona, skill or tool
description.** EnvHarness feeds its designer text summaries of trajectories and lets it write executable
rules; the Aegis analogue would only *propose* prose edits for an operator to accept. It needs a model
stronger than the local 9B to be worth reading, so it is an operator command, never part of the agent loop —
and it is the same territory as **P85.5** (agent-initiated refinement) and **P67.12** (personas cannot
accumulate anything). Do not build (b) before (a) has shown a pattern worth diagnosing.

**Promote when** a local-model session goes badly and finding out *how often* that happens takes more than
one command — (a). (b) promotes with P85.5.

Priority: Tier 4 — no fired trigger, (a) S, (b) M–L and sequenced behind (a) and P85.5.

### P87.3 — Eval scenarios always start at turn 0; there is no "resume from a recorded state and vary one thing"

**Filed 2026-09-21**, same comparative reading. EnvHarness's `ActionableEnv` contract requires
`save_state() -> dict` and `from_state(dict)` so that many policies can be started from the *identical*
mid-episode state; where the runtime is too heavy to serialise (a container, a browser) the saved state is
just `(reset_seed, reset_options)` and the environment is rebuilt by replaying an action prefix — its `Setup`
layer. The point is isolation of one variable: same state, one thing changed.

Aegis has the parts, not the seam. `handleFork` (`internal/server/sessions.go`, ~819–908) copies
`Messages[:keep]` and the model override into a new session and records the lineage edge (`SetParent`,
P85.4) — the conversation, **not** the workspace. `internal/checkpoint` snapshots each file before a turn
modifies it and `RestoreFiles` puts them back, but that is a rewind within one session, not a way to
materialise "the tree as it stood at turn N" somewhere else. `eval.Scenario` and `WorkflowTask.Materialize`
both build their starting point from scratch. So a question like *does compaction at turn 30 lose something a
different compaction setting keeps*, or *does this prompt block change what the 9B does on the turn after a
real overflow*, can only be asked by re-running the first 30 turns — which is expensive on this hardware and
not reproducible, since the first 30 turns differ each time.

The shape: fork a recorded session at turn N, restore the files as of N into a scratch directory, and run the
next turn under a different model, prompt or compaction setting — or replay a recorded prefix of tool calls
into a fresh tree, in which case only calls whose tool declares `tool.ReplayClass` `"safe"` may be replayed
(P65.4 built that classification for exactly this hazard). Open design questions, not settled: whether
`RestoreFiles` can target a directory other than the live workspace, and whether the fork must carry
`Traces` so a comparison reads both sides the same way.

Overlaps **P65.5** (rewind/fork discards a branch) only in using the same fork machinery; this is an
evaluation seam, not a conversation feature. Long-context paths (compaction, drive re-entry, overflow
escalation) are the ones hardest to reach cold, which is the case for it.

**Promote when** a compaction, overflow or re-entry regression needs an A/B against identical state and the
only available method is re-running the turns before it.

Priority: Tier 4 — no fired trigger, M, and the item in this batch with real design questions. Pairs with
**P87.1** (the comparison needs step/token numbers to be worth making).

### P87.4 — Skills cannot be induced from recorded runs (design reading; sequenced behind P87.2, P87.3 and P85.5)

**Filed 2026-09-21**, same comparative reading. EnvHarness's `reasoning_bank/induce.py` turns trajectories
into a small store of retrievable "memory items" three ways: single-trajectory (separate success and failure
prompts), parallel (several runs of the same task, contrasted without outcome labels), and **paired-diff**
(a failure and a success on the *same* task, asked for only the technique that differs). Its constraints are
what make it interesting for a small prompt budget: an item is a title of at most 10 words, a one-sentence
description and content of at most 250 characters, observations in the input are cut to 350 characters, and
its output is parsed by a fixed `# Memory Item / ## Title / ## Description / ## Content` shape with a loose
fallback for models that drift from it. Fatal errors (auth, budget) abort a batch; transient ones retry four
times with backoff. **It has no explicit dedup and no leakage filter** — it relies on the prompt — so the
"induce a skill, leakage-filter it" step in `research/CodeAF`'s FailForge note is not something EnvHarness
supplies; a task-specific answer leaking into a general skill would have to be guarded separately.

The fit with Aegis is the skill format itself: skills inject only `name — description` until loaded
(`CLAUDE.md`), so an induced item of that size lands where the local prompt budget
(`TestEffectiveSystem_localProfileBudget`) can afford it — and only as a dormant skill, never as base-prompt
text. What is missing is everything upstream: paired runs of one task (**P87.3** produces them), a way to find
which sessions are worth pairing (**P87.2a**), a model stronger than the local 9B to write the items, and a
gate that a loaded skill actually helps — **P86.10** already records that whether the 9B loads a skill at all
is unmeasured. The write path and the turn-boundary discipline belong to **P85.5**/**P67.12**.

EnvHarness's evidence that this works is on its own benchmarks and trained/retrieved policies (up to +9 points
on held-out tasks); nothing in the source read says it transfers to a 9B local model without native
tool-calling training. Treat as a research item.

**Promote when** P87.2 and P87.3 exist and a repeated failure has a success on the same task to contrast with.

Priority: Tier 4 — no fired trigger, L, last in the batch; do not build before the three items it depends on.

### P87.5 — No way to inject faults into tool results on a live run (design reading, low priority)

**Filed 2026-09-21**, same comparative reading. EnvHarness's `Rules` hooks intercept actions, transitions and
observations, so a designer can reshape what the agent sees without touching the environment. The narrow
transferable use is *deliberate* perturbation: run the live tier with a tool's result truncated, a call
failing on its Nth use, or trailing lines dropped, to see how a small model copes with noisy output.

Aegis has two near-misses and neither is this. `eval/adversarial_test.go` scripts attack shapes, but each with
a bespoke stand-in tool (`injectedFileTool`) on a scripted adapter — mechanism, not model behaviour. And
`engine.Hooks.PostToolUse` receives the result but returns nothing (`engine.go`, ~242): Aegis hooks observe
and veto, they cannot reshape an observation, which is the right posture for a security product and means a
perturbation layer would be a `tool.Registry` decorator used only by the eval package, not a hook. Real
truncation and spill (`internal/tool/builtin/truncate.go`) already put the model through capped and
spilled output in ordinary runs, so the marginal value is the *unnatural* faults, which is thin.

**Promote when** a live-tier failure is plausibly caused by the shape of a tool result and a re-run cannot
reproduce it.

Priority: Tier 4 — no fired trigger, S–M, do not build speculatively.

**Not filed, with the reason.** EnvHarness's RL pipeline (`rl/`, verl-agent) and its trained-policy results —
Aegis does not train. `Link` (composing another benchmark's tasks into an episode) and the per-benchmark
`bridges/` — no second task family to compose. Its "model-written Python rules run in an isolated subprocess,
with the state schema injected as the only readable fields" — Aegis lets no model author executable rules, so
there is nothing to confine; if that ever changes, the declared-read-surface idea applies and the existing
sandbox is the place to run it. Its `EnvHook` lifecycle observers (`on_episode_start/end`,
`on_mutation_applied`) have no error handling at all (an exception propagates), which is a reason not to copy
the shape. The provider-decorator composition Aegis already has (`providerfactory.Decorate`) is the same
"stack freely at a standard interface" idea. **Provenance:** claims about EnvHarness rest on its README and on
`core/hooks.py`, `core/actionable_env.py` and `reasoning_bank/induce.py`, read through a summarising fetch, and
on directory listings; it was **not cloned, built or run**. Claims about Aegis were checked against this tree
at the files cited.

### P88.3 — Sampling parameters for a local Ollama model are set only by hand-built Modelfiles

**Filed 2026-10-01**, same trial. `ollama pull ornith-1.5:9b` ships with **no** `PARAMETER` lines, so a run
uses Ollama's defaults; the `aegis-qwen35-9b` builds pin `temperature 0.2`, `top_k 20`, `top_p 0.8`,
`repeat_penalty 1`, `num_ctx 32768`. A local copy of ornith with exactly those parameters and an
**unchanged template** (`aegis-ornith-9b:32k`) completed the test prompt that the untuned model failed.
The template was *not* the cause: ornith's blob carries a full Qwen3.5 Jinja template with tool-call and
`<think>` handling (`ollama show --template`); the bare `TEMPLATE {{ .Prompt }}` in `--modelfile` output
is a placeholder. (The `aegis-qwen35-9b` Go template, by contrast, has no tool section and a mangled
`{{ if .Prompt }} .Prompt }}` line.)

Today nothing in Aegis tells a user their model is running at defaults, and the `num_ctx` decorator
(`internal/provider`) shows the mechanism for sending a per-request option. Candidates: warn once when a
local-profile Ollama model reports no sampling parameters; send conservative `temperature`/`top_p`/`top_k`
per request for the local profile (a user's own Modelfile or config still wins); and a docs section plus
a command that writes the Modelfile. Which of these is right depends on the evidence below.

**This is one run per configuration.** The effect is suggestive, not established. Do **P88.7** first, or at
least repeat the test three times per setting, before changing a default — a default that is wrong for a
model family is worse than none.

**Promote when** a repeated comparison shows the pinned settings win on pass rate. Priority: Tier 4 — no
fired trigger, S–M; plausibly the largest single effect in the batch if it holds.

### P88.4 — A run can end on a failing build or test with no report, or an empty one

**Filed 2026-10-01**, same trial. Three runs, three endings: untuned `ornith` — no final text, the file
still uncompilable; `aegis-qwen35-9b:32k` — `[notice: model produced no text even after being asked for a
plain-text answer — the reply is empty]` after its last `go test` failed with `undefined: testing`; tuned
`ornith` — a report, but about ten lines against an explicit "keep the final report to 3 lines". The
empty-answer nudge (P34.1) fires on an empty reply; nothing keys on **what the last tool result was**. A
run whose final tool call exited non-zero and whose reply is empty (or claims success) is the case worth a
targeted nudge — "your last command failed: fix it or say plainly that it still fails" — before the empty
notice is shown.

The length miss is a separate and minor thing: the output guard (`internal/guard`) validates against a
rubric/schema and could carry a length bound, but a prose-length instruction is not a schema and this may
not be worth it. Record, don't build, unless the guard already has a hook for it.

**Promote when** an empty or false-success ending recurs on a task with a failing last command. Priority:
Tier 4 — no fired trigger, S.

### P88.5 — Repeated failed edits to one file have no "stop patching, rewrite it" exit

**Filed 2026-10-01**, same trial. The tuned `ornith` run took about 25 tool calls for a task a clean run does
in about five; four of them were failed `multi_edit`/`edit_section` calls against `main.go` and
`main_test.go`, plus four failing `go test` runs. It
did finish, by rewriting the whole file with `write_file` — which it could have done after the first
miss. `toolfailure.go` (P52.3) counts consecutive *all-error rounds* and nudges at three; it does not know
that the failures are all against one path, and its message is generic.

Candidate: after N failed edits to the same path with no successful write between, the result says "re-read
the file, then replace it with `write_file`" — the model already takes that exit unprompted eventually, so
this only shortens the road. **Related, not duplicate:** **P86.2** (successful calls that teach nothing —
this is failing ones), **P86.1** (a faster two-strike rule for a refusal that names its fix — P88.1 would
supply the "names its fix"), and **P52.14**. Both loop-detector sets (`PollExempter`,
`SignatureTransparent`) are deliberately narrow and disjoint — a per-path counter should not touch them.

**Promote when** a run is seen spending an iteration budget on one file's edit failures; build **P88.1**
first, since a hint may make this unnecessary. Priority: Tier 4 — no fired trigger, S.

### P88.6 — `edit_section` is exposed for code, and fails on it with a message the model does not act on

**Filed 2026-10-01**, same trial. Two of the three runs called `edit_section` on a `.go` file and got `main.go
has no markdown headings, so it has no sections to edit — use multi_edit (or write_file for a whole small
file)`. The tool's own description already says "for a file with no markdown headings (source code, config,
data) … use multi_edit instead", so a description fix would change nothing; the models call it anyway.
`edit_section` is registered always-exposed (`internal/tool/builtin/builtin.go`, ~217) and, per the P62.9
note, the local profile keeps it *because* it replaced `edit_file` for prose — but the trial task was code.

Options, none obviously right: defer it under the local profile when the workspace is not documentation
(needs a signal for that), return a refusal that includes the exact `multi_edit` call shape for the file,
or leave it and accept one wasted call. Mind the CLAUDE.md invariant that a tool's `Description()` or error
must never name a tool the active profile defers — the current message names `multi_edit` and
`write_file`, both of which the local profile keeps exposed, so it is fine today and a change must keep it so.

**Promote when** P88.1 is built and the wasted call is still seen. Priority: Tier 4 — no fired trigger,
XS–S, and the lowest value in the batch.

---

## Verification Work

**Status: 4 open** (was 6 before 2026-09-13 — **P68.5** and **P68.6** closed rather than pursued; see
[Status](#status)). Every item here has its code already written and merged — nothing below is a
design or implementation task. Each is closed by running a live-model harness and recording the
result the item's closure condition names, not by writing more code. They are **not tiered**:
tiering answers "how urgent is this build," and there is no build left to prioritize.

**These do not share one blocker.** **P38.1** needs permission to launch an unattended
auto-approving agent. **P62.8** was filed as waiting on hardware (a backend serving a
>200,000-token window) — the installed 9b advertises one, so what remains is whether its cache
fits. **P80.4**'s gate half needs a model stronger than `aegis-qwen35-9b` (deferred until that
hardware exists), but its overflow-fixture half runs on the 9b. **P68.4**'s rubric build is done
(2026-09-13); what's left for it is exactly the live run this section is named for. See each entry.
**P66.22**, **P62.9** and **P65.2** closed by live evidence 2026-09-01/09-02 — full record in
[releases.md](releases/releases-01.md#roadmap-housekeeping-closed-items-migrated-from-roadmapmd-2026-09-03).

### P88.7 — `GoFixTask` and its repeated live test are built; no live sitting yet

**Built 2026-10-21** (record: [releases](releases/releases-01.md#2026-10-21-cloud-and-local-harness-pass)).
`internal/eval/gofixtask.go` is the 2026-10-01 trial's scenario as a portable `WorkflowTask` — a Go module
whose `Average` divides by `len(nums)`, an 8-criterion rubric (`fix_correct` via a behavioral oracle that runs
with the agent's own test set aside, `test_added`, `test_table_driven`, `test_covers_empty`, `vet_clean`,
`tests_pass`, `no_cheat`, `no_scope_creep`; `PassMark` 7) — and `TestLiveGoFixRepeated` (`live_workflow` tag)
runs it `AEGIS_EVAL_RUNS` times (default 3) against `AEGIS_EVAL_MODEL` and logs one `SUMMARY` line: pass
rate, score, mean calls, mean tokens, empty replies, mean elapsed. `TaskOutcome` now carries calls, tokens
and an empty-reply flag (a first slice of **P87.1**).

**What closes it:** one sitting per configuration the trial compared — untuned `ornith-1.5:9b`,
`aegis-qwen35-9b:32k`, and ornith with the qwen build's sampling pinned — at the same N, read side by side.
That is what decides **P88.3**, and it is the first measurement of whether the shipped **P88.1** hints and
**P88.2** refusal change outcomes. Ollama was not running on 2026-10-21, so nothing has been run.

**Provenance and what was not done.** All claims above come from three `aegis chat` runs on 2026-10-01
(Ollama 0.30.10, Windows, `aegis version 2f77fee-dirty`): untuned `ornith-1.5:9b` (112 s), `aegis-qwen35-9b:32k`
(66 s) and `aegis-ornith-9b:32k` — a local copy of ornith with the qwen build's sampling parameters, created
with `ollama create` and **not committed anywhere** (140 s, tests passed on independent `go test`). One run
each; no repetition; one prompt; a scratch workspace outside the repo. The Aegis code claims (file and
function names, the P62.9 deferral, the P52.3/P34.1 behaviour) were checked by reading the cited files this
session; no code was changed. Pinning a model's sampling in a Modelfile is not something Aegis does today.

### P80.4 — `live_workflow`'s two standalone tests both need a stronger model than this machine has run

**Filed 2026-08-30**, from the audit's **C2** entry finally being executed. The tier had never been run;
running it three times found three product defects (**P79.2** the daemon released nothing unless it
exited through `ListenAndServe`; **P79.3** compaction's summarizer spent its whole budget on a thinking
preamble and returned empty on every cycle; **P79.4** the empty-answer nudge re-asked over the channel
that had just swallowed the answer), all fixed and re-verified live the same day — record in
[releases.md](releases/releases-01.md#comprehensive-architecture-and-security-audit-remediated-in-full-2026-08-30).
`TestLiveWorkflow`'s four subtests now pass against `aegis-qwen35-9b:32k`, `SecurityTriage` scoring
full marks on a 12-point rubric where it scored 3/12 before P79.4. (The rubric is now worth 16 points
as of P68.4's 2026-09-13 rebuild — see that entry — so a re-run will report against the new scale, not
repeat the old 12/12.)

**What is left is not code.** The two standalone tests in that file decline to report rather than
passing vacuously, and both declines are about the model:

- **`TestLiveWorkflowCompactionPrefixCacheGate`** had two complaints and now has one. "No compaction
  actually ran, so this run measures nothing about the gate" is gone — P79.3 means compaction runs and
  succeeds. What remains is that this model abandons the fixture's 14-file read chain after five files,
  so the conversation never grows as designed and the test refuses to report a gate comparison from it.
  It needs a model that will follow a long mechanical chain. Note this is the _small_-window arm;
  **P62.8**'s large-window regime is a separate, still hardware-blocked question against the same test.
- **`TestLiveWorkflowForcedContextOverflow`** passed on one run and skipped on the next — the model's
  `write_file` call was not long enough to hit the 8,192-token ceiling the test needs, so it declined to
  measure. That is run-to-run variance in how much a live model chooses to emit, detected correctly.
  Making it deterministic means raising the fixture's requested line count or lowering the window until
  overflow is forced rather than hoped for, which _is_ a small code change — but one worth making with
  a run in front of you rather than blind.

Closure condition: one `live_workflow` run on a model that completes the 14-file chain, producing
either a prefix-cache gate comparison or a recorded reason the comparison is still not meaningful; plus
a forced-overflow fixture that overflows on every run rather than on some.

Priority: Verification — the code is in and green. **The two halves have different blockers
(2026-09-13).** The prefix-cache gate comparison needs a model that completes the 14-file chain, which
`aegis-qwen35-9b:32k` does not and nothing else installed here is known to — deferred until hardware
that runs a stronger model. The forced-overflow fixture change is a small code change that can be
made and run against the 9b now. The earlier note that this shared a blocker with **P68.4** and
**P68.6** was wrong for both — P68.4's build half is done (see that entry) and P68.6 was closed
(see [Status](#status)).

### P38.1 — Non-orchestrated, single-context threat-model build (primary path for local models)

The threat-modeling skill's primary build is a single-context linear build the driving model runs
itself — no sub-agents, no `agent`-tool orchestration. Context stays bounded by levers that already
exist (SKILL.md §4): `recon.py`'s ~11KB digest, P36.2 pruning of spent write/read payloads,
incremental section-at-a-time writes, and the deterministic P37 scripts. `scaffold.py` (P38.4)
pre-writes all seven files from the skeletons with real structure + a unique
`<!-- PENDING: <section> -->` marker per fillable section, so the model fills sections instead of
authoring structure.

**Mechanism: live-confirmed, repeatedly.** Across re-tests on qwen3:14b, qwen3.6:35b-a3b and
gpt-oss:20b, the drive reliably runs `recon.py` → `scaffold.py` → incremental `edit_file` fills in
one context with no orchestration mis-route.

**Conformance: still unmet.** Every re-test has stalled short of an unattended verify-clean suite, but
each stall has moved the blocker further from the harness and closer to raw model throughput. Full
dated log (2026-07-21 through 2026-08-09) is in [releases.md](releases.md) (_P38.1 re-test log_).
Most recent result:

- **2026-08-09, LFM2.5-2.6B then qwen3:14b vs AiGateway — conformance still unmet, ten harness
  defects root-caused and shipped as P39.16.** The 2.6B produced zero files in two runs and is now
  refused by a pre-flight gate. The qwen3:14b arm built the **complete suite** — six files, ~35KB, all
  five content phases, every marker cleared — further than any prior run on this target, but
  verification did not pass unattended: `component-name-consistency`, `count-consistency` and
  `coverage-ledger-complete` remained after the bounded fix loop. Two of those three were then fixed
  structurally (P39.16); the third re-run hung before reaching a verdict. All ten defects were the same
  shape — a tool that held the information the model needed and returned an error without it.

**Direction (user, 2026-07-24):** the strongest lever is making local models **piecemeal both their
reads and their writes**, then finishing with a **quality-validation pass** — P39.12-P39.15 implement
this, and P39.16 (2026-08-09) extended it: piecemeal writing still failed while it went through
`edit_file`, because an anchored edit asks the model to _reproduce_ text rather than only produce it.
Handle-based tools (`fill_marker`, `edit_section`) remove the reproduction step and are what finally
made the fill loop reliable on a 14B model.

**Reproduce:** `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads outside
the workspace root); run
`aegis chat "threat model this repo" --skill threat-modeling --mode build --yes` (the prompt is
required). It prints a `phased mode` notice and resets context each phase.

**Closure condition:** the real suite's PENDING markers reach zero and `verify.py` / `lint_dfd.py` /
`inventory.py --check` all pass, **unattended, in one invocation**. Met once, 2026-07-24 on
FirewallRuleAnalyzer; not repeated since.

**2026-08-16: scheduled with the live-tier sitting and did not run.** The model server was reachable
and the target copy staged; the run itself was refused, because the recipe is an unattended agent
with auto-approved host shell (`--yes` plus `auto_approve_exec`) and the session driving it was not
permitted to launch that. Nothing about the item changed. This is a standing property of the recipe,
not a one-off: whoever runs it next either runs it by hand or grants the permission deliberately.

Priority: Verification — every load-bearing harness fix the re-tests have root-caused has shipped
(P39.5-P39.18, P47.1-P47.9, P52.12, P57.1). This item stays open only as the conformance **umbrella**,
closeable once a live built-in drive is confirmed to reach a verify-clean suite unattended, in one
invocation, on a local model. No code work remains; it is live-run tracking. **2026-09-13:** none of
the models it was re-tested on (qwen3:14b, qwen3.6:35b-a3b, gpt-oss:20b) is still installed, and it
has never been run on `aegis-qwen35-9b`. Deferred with the other stronger-model items; note that
whether the 9b falls short here is itself untested.

### P68.4 — The triage rubric's measuring band sits below the strongest local model

**Filed 2026-08-17, from a temperature A/B that measured nothing — twice.** P68.3 shipped a task that
ranks _models_ well (9b 10.7 vs 14b 2.7, complete separation at n=3). The attempt to use it for the
next question — do the sampling parameters `docs/local-model-tuning.md` recommends actually help? —
found it cannot rank _configurations_, because both available substrates sit against a rail:

| substrate             | temp 0.2      | temp 0.6   | reading                                        |
| --------------------- | ------------- | ---------- | ----------------------------------------------- |
| `aegis-qwen35-9b:32k` | 12, 12, 12    | 12, 12, 12 | **ceiling** — rubric exhausted                 |
| `qwen3:14b-32k-fix`   | 3, 3, 3, 3, 3 | 3, 3, 3    | **pinned low** — one repeated minimal strategy |

Both arms of both A/Bs are flat, and **neither is evidence that temperature does not matter** — a
saturated instrument returns exactly this pattern whether the variable matters or not.

Two instrument checks were run before concluding, and both came back clean, which is what makes the
"no headroom" reading the surviving one rather than a guess: the derived Modelfiles differ **only** in
`temperature` (`ollama show` confirms `num_ctx` and everything else carried), and all four derived
models still carry the **corrected** chat template.

**The substrate was removed from the machine on 2026-08-17, after this was filed**: `qwen3:14b-32k`
and its corrected build are both gone, so the only mid-range scorer either A/B had is no longer
available. **Inventory re-checked 2026-09-13:** `qwen2.5-coder:1.5b` and `gemma4:12b`, which this
paragraph previously listed as still installed, are gone too. What is installed is
`aegis-qwen35-9b` (`:16k`/`:32k`, saturates at 12/12), `llama3.2:3b` (the non-thinking control in the
audit's Phase 5 pass, never scored on this task) and two `phi4` reasoning builds (never scored on this
task). Re-pulling `qwen3:14b` and rebuilding the corrected variant per
[docs/local-model-tuning.md](../docs/local-model-tuning.md) is still the cheap route to a mid-range
scorer.

**Shipped in full, 2026-09-13.** Three additions to the fixture and rubric, all in
`internal/eval/triagefixture.go` and `triagetask.go`:

- **A sixth planted issue needing a three-file trace.** `dispatch.py` (new) reads
  `job["callback_url"]` straight out of the dict `jobs.py`'s `run_next` builds from
  `pickle.loads` — itself fed by `wire.py`'s untrusted network frame — and passes it unchecked to
  `urllib.request.urlopen` (category `ssrf`). Finding it means following the same untrusted-source
  trace as `unsafe_deserialization` one file further, so it is the new hardest criterion rather than a
  parallel one.
- **`severity_accuracy` (2 points).** Scored only against true positives — a spurious finding's
  severity is not graded, since precision already charges for it — comparing each reported severity
  to a fixed answer key (`triageSeverity`: two `medium`, four `high`, deliberately mixed so "label
  everything high" is distinguishable from actually grading each issue).
- **`no_scope_creep` (1 point).** Step 2 of the prompt says "fix those two, and only those two" —
  this is what makes that scored: any edit to a non-fix, non-protected `.py` file (config.py, client.py,
  wire.py, jobs.py, dispatch.py today) costs the point. Filed as "the three files the task never
  mentions"; implemented against every audit-only file rather than exactly three.

Rubric is now worth 16 points (was 12), `PassMark` raised to 12 (same 75% ratio). New tests
`TestTriageSeverityScoring` and `TestTriageScopeCreepPenalizesUnrelatedEdits`; the existing suite was
updated for the new totals and the `readFindings` return type (`map[finding]string` carrying severity,
was `map[finding]bool`). `go test ./internal/eval/...` and `go build ./...` both green.

**What remains is the live run this was built for**, not code: whether `aegis-qwen35-9b:32k` still
saturates the rubric, and where a stronger model (deferred — see [Status](#status)) lands on it. Until
that run happens, `docs/local-model-tuning.md`'s sampling section should stay labelled
reasoned-not-measured, as it already does — a harder rubric is not itself a temperature measurement.

Priority: Verification — the rubric build is done and green; what remains is a live run, same as
every other item in this section.

### P62.8 — The prefix-cache gate's large-window regime has never been measured

`compaction.shouldPrune` has two regimes: below `largeContextWindowThreshold` (200,000) it fires at a
25%-free ratio; above it, a fixed 40k buffer, which on a large window places the prune much earlier in
relative terms than anything measured so far. Everything known about this gate comes from a
24,576-token window (the ratio branch only), and P62.2's history is a specific warning against
generalising from it — the same fixture gave opposite verdicts before and after an instrument fix,
because what mattered was _where in the window_ the prune landed relative to the backend's
context-shifting point. The buffer branch changes exactly that relationship and is unmeasured. The
gate itself needs no new code — this is purely a measurement gap.

**Why parked rather than queued.** Needs a backend serving a >200,000-token window. **Re-checked
2026-09-13:** both models this paragraph used to name (`qwen3:14b` at 40,960, `gemma4:12b` at 262,144)
are gone. The installed `aegis-qwen35-9b` advertises `qwen35.context_length = 262144`, so "no model on
hand" is no longer the blocker; whether its cache fits is. **Estimated from its GGUF metadata, not
measured:** it is a hybrid model with full attention on only 8 of its 32 main layers
(`full_attention_interval = 4`; 4 KV heads × 256 dims), so a cache allocated for those layers alone
is ~32 KiB/token at f16 — ~6.3 GiB at 204,800 tokens (~7 GiB if the MTP layer carries one too), about
half that at `q8_0` — against ~5.7 GiB of weights on a 16GB card. That plausibly fits; it would not if
the backend allocated cache for all 33 blocks (~26 GiB). Two confounds to settle before trusting a
result: a quantized cache, if needed, is a different backend configuration from every prior
measurement of this gate; and the recurrent (linear-attention) layers carry state that may not be
reusable across a shared prefix the way a plain KV cache is — the prefix-reuse behaviour this gate is
about may simply differ on this architecture. Still a hardware question, but a narrower one.

**How to run it when hardware allows:**
`AEGIS_EVAL_MODEL=<model> go test -tags live_workflow -count=1 ./internal/eval/ -run
TestLiveWorkflowCompactionPrefixCacheGate -v`, with `compactionNumCtx` raised past 200,000 and
`writeCompactionFixture`'s per-file payload scaled up so the chain still crosses the trigger.

Priority: Verification — no trigger, no user impact, blocked on hardware rather than on any decision
or any remaining code.

---

For shipped feature history, batch origins, refutation records, competitive-landscape review and the
full gap analysis, see [releases.md](releases.md).
