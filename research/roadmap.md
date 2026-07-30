# Aegis Capability Roadmap

**Last updated:** 2026-07-29

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** **P38.1** (Tier 2 umbrella) + the
remaining **P49.x repo-map / index enrichment batch** (**P49.3**/**P49.4** Tier 4, both
measure-first — the structural head **P49.1** and the on-demand query tool **P49.2** have **shipped**
2026-07-29; see [releases.md](releases.md)) +
2 parked (Tier 4: **P38.8**, **P25.9**). Everything else filed since the last cleanup — the P39.10-P39.15
threat-model harness fixes, the P40.x TUI/UX batch, P41.1, P44.1, P45.1, P45.2, the P46.x
codex-build track, **P48.1** (config-test hermeticity), **P47.6** (drive model-selection guidance,
doc-only), **P47.10** (CLI/TUI parity, resolved as documentation), **P49.1**/**P49.2** (repo-map
import edges + on-demand `repomap` query tool), and the entire **P47.x phased-drive stability batch**
(**P47.1**, **P47.2**, **P47.3**, **P47.4**, **P47.5**, **P47.7**, **P47.8**, **P47.9**) — has
**shipped**; see [releases.md](releases.md) for what each one did.

**Next batch — P47.x phased-drive stability (filed 2026-07-24):** the 2026-07-24
FirewallRuleAnalyzer run reached a **verify-clean suite** on `qwen3.6:35b-a3b-fast` (all
`verify.py`/`lint_dfd.py`/`inventory.py --check` passing) — P38.1's mechanism/conformance closure
condition — but only after **three manual re-invocations**: the CLI `chat --skill` drive engine has
no proactive compaction wired in, so each phase's context grew until Ollama hard-rejected the
request (observed 173,816 vs a 131,072 window) and the drive aborted. P47.1-P47.6 make that same
run succeed in **one unattended invocation**; tackle in number order. The whole batch has now
**shipped**: **P47.1** (wire proactive compaction into the CLI drive engine), **P47.2** (treat a
mid-phase context overflow as a resumable phase reset, not a fatal abort), **P47.3** (stop content
phases burning context on manual self-verification), **P47.4** (make in-phase continuations
near-stateless to cap peak context), and **P47.5** (auto-size + auto-escalate the per-phase context
window) — see [releases.md](releases.md).

**Batch extension — phase-6 remediation resilience (filed 2026-07-27):** the first live run of the
ec0127c hollow-report checks + afd6764 self-heal (FirewallRiskRater, `qwen3.6:35b-a3b-fast`)
confirmed both shipped fixes work — self-heal auto-deployed the new `verify.py` and the checks
turned a false-passing hollow suite into `12 passed, 2 failed` with file:line. But with the checks
now correctly failing, the phase-6 verify/quality remediation loop exposed the same class of gaps
P47.1-P47.3 fixed for content phases, one tier down: it had none of them. **P47.7** (extend the
P47.2 overflow-reset to the phase-6 loop), **P47.8** (carry the P39.14 anti-monolithic-write
guardrail into the phase-6 prompts), and **P47.9** (route hollow-body failures back through the
owning content phase) have all **shipped** (see [releases.md](releases.md)); **P47.10** (the
CLI-only drive-to-completion / TUI `/threat-model` parity question) was **resolved 2026-07-27 as
documentation** — see [releases.md](releases.md).

**New batch — P49.x repo-map / index enrichment (filed 2026-07-29):** the `index` functionality
(`internal/repomap`) today gives the model a flat, regex-extracted file→symbol list injected as
`<repo_map>`. Prompted by a review of nanonets/graft (tree-sitter structural pass + LLM concept
nodes + on-demand query tools), this batch evolves the same capability *inside the single-Go-binary,
no-external-runtime constraint* rather than vendoring graft's Node package. Sequence head **P49.1**
(import/dependency edges in the map, Tier 2, cheap + self-contained) and **P49.2** (an on-demand
`repomap` query tool — skeleton/importers/map — Tier 3) **shipped 2026-07-29** (see
[releases.md](releases.md)); the structural tier now exists. The remaining two follow *only if it
doesn't close the discovery gap*: **P49.3** (LSP-backed symbol precision, Tier 4, measure-first) and
**P49.4** (LLM-summarized concept nodes, Tier 4, speculative). Both are measure-first — neither has a
live-run trigger yet, so do not build them speculatively; build only once P49.1/P49.2 have
demonstrably fallen short on a live run.

**Remaining P38.1 debt:** the in-harness phased-drive convergence tracking (see the P38.1 body). The
2026-07-23 gpt-oss:20b housekeeping is now **closed** — **P39.10**/**P39.11** were already coded,
shipped, and verified live; as of 2026-07-27 they also have their releases.md entry and regression
tests (`TestDriveOraclesSkipBuiltinSkillsSubtree` + `TestDriveOraclesSkipRealMaterializedBuiltins`
cover the oracle skip of a materialized-skill PENDING marker; `chat --skill` workspace materialization
is covered by `internal/skills/embedded_test.go`).

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** none open — batch head **P47.1** (wire proactive compaction into the CLI `chat --skill`
drive engine) **shipped** 2026-07-24; see [releases.md](releases.md).

---

## Open Work — Tier 2

**Status:** 1 open — **P38.1** (threat-model conformance umbrella), which is live-run verification
tracking rather than independent build work. The self-contained batch items **P47.1**, **P47.2**,
**P47.3**, **P47.4**, **P47.5**, **P47.7**, **P47.8**, **P47.9** (the full P47.x phased-drive
stability batch), **P48.1** (config-test hermeticity), and **P49.1** (repo-map import edges, the
P49.x batch head) have all shipped — see [releases.md](releases.md).

### P38.1 — Non-orchestrated, single-context threat-model build (primary path for local models)

The threat-modeling skill's primary build is a single-context linear build the driving model runs
itself — no sub-agents, no `agent`-tool orchestration. Context stays bounded by levers that already
exist (SKILL.md §4): `recon.py`'s ~11KB digest, P36.2 pruning of spent write/read payloads,
incremental section-at-a-time writes, and the deterministic P37 scripts. `scaffold.py` (P38.4)
pre-writes all seven files from the skeletons with real structure + a unique
`<!-- PENDING: <section> -->` marker per fillable section, so the model fills sections instead of
authoring structure.

**Mechanism: live-confirmed, repeatedly.** Across re-tests on qwen3:14b, qwen3.6:35b-a3b, and
gpt-oss:20b, the drive reliably runs `recon.py` → `scaffold.py` → incremental `edit_file` fills in
one context with no orchestration mis-route — the P36.3-era failure that killed every prior run is
gone. **Conformance — still unmet.** Every re-test so far has stalled short of a verify-clean
suite, but each stall has moved the blocker further from the harness and closer to raw model
throughput:

- **2026-07-21, qwen3:14b / qwen3.6:35b / gpt-oss:20b:** the ~9K-token SKILL.md preload re-sent
  every turn starved the fill of context before the model could `edit_file` (root cause → shipped
  **P39.5**); the autonomous verify pass missed structural defects a mechanical check should catch
  (→ **P39.6**); models stalled announcing work instead of doing it (→ **P39.7**); a broken LLM
  summarizer looped silently (→ **P39.8**); the `/v1` compat path could overflow un-warned (→
  **P39.9**, native-adapter half exonerated). All shipped — see [releases.md](releases.md).
- **2026-07-23, gpt-oss:20b vs AiGateway:** with P39.5-P39.9 in place, the drive died *before*
  model capability was even tested, on two `chat --skill`-CLI bugs: skill scripts materialized
  only under the data dir, outside the sandboxed workspace root, so the model couldn't reach
  `recon.py` (**P39.10**); and the drive's PENDING-marker oracle walked the materialized skeleton
  templates themselves, so it could never reach zero (**P39.11**). Both are coded, shipped on
  `tier3-batch`, verified live end-to-end, and — as of 2026-07-27 — documented in releases.md with
  regression tests (see Status above); this housekeeping is closed. With the scripts reachable,
  gpt-oss:20b itself then failed to
  converge from small-model path/argument brittleness: mangled script paths, drifting to a typo'd
  run-dir (`.aegit`) mid-build so its fills landed outside the real suite, calls to a
  non-existent `search` tool, and the wrong `--framework` flag.
- **2026-07-24, qwen3.6:35b-a3b-fast vs FirewallRuleAnalyzer:** harness and model-competence
  questions cleared — the drive ran recon → scaffold → fill, held the run-dir path across every
  `edit_file` (the gpt-oss:20b mangling above did not recur), produced grounded file:line-cited
  content, and its DFD passed `lint_dfd.py` 5/5. What blocked closure was throughput/write
  robustness, not orchestration: a 5-minute response-header timeout that a 2845-line file read
  could blow past at ~7 tok/s (**P39.12**), unbounded whole-file reads ballooning cumulative
  session input to 3.47M tokens (**P39.13**), a monolithic ~5,700-token single-file write that
  truncated into a malformed tool call (**P39.14**), and mechanical verify catching structural
  errors but not substance like a Tier-2 threat with a Tier-1 prerequisite (**P39.15**). All four
  shipped 2026-07-24 with regression tests — see [releases.md](releases.md) for the fixes.

**Direction (user, 2026-07-24):** the strongest lever is making local models **piecemeal both
their reads and their writes**, then finishing with a **quality-validation pass**; P39.12-P39.15
implement exactly that.

- **2026-07-24, in-harness phased drive (P38.8's mechanism, brought inside `chat --skill`):** the
  root cause the P39.x fixes kept circling is structural — the drive ran the *whole* six-phase
  build in **one ever-growing conversation** (`internal/cli/chat.go`), so even with pruning the
  peak context climbs until a local window stalls. The parked P38.8 wrapper never hit that because
  it runs a **fresh, skill-free context per phase**. That per-phase reset is now implemented
  *inside* the built-in path: for the threat-modeling skill, `chat --skill` drives
  architecture → DFD → framework-analysis → findings → assessment each in its **own fresh
  conversation** seeded with a compact phase prompt (prior phases grounded from disk, not from
  history), then runs the phase-6 verify+quality round in its own context too. All the existing
  guards are reused (the PENDING oracle, the P39.7 no-progress "act now" nudge, `--max-turns`, the
  P39.6 verify loop, the P38.1 quality pass) — only the context lifetime changed. Lives in
  `internal/cli/chat_phased.go`; `phasePlanFor` gates it to threat-modeling (every other
  PENDING-driven skill keeps the generic single-context drive), and `AEGIS_SKILL_DRIVE=linear`
  forces the old path for comparison. Unit-tested for phase sequencing/completion/prompt wiring;
  **live convergence against a local model is the remaining validation** (see next steps).

- **2026-07-24, qwen3.6:35b-a3b-fast vs FirewallRuleAnalyzer (phased drive, stability):** the
  phased drive **reached a verify-clean suite** — 23 threats / 22 findings across 9 components, all
  `verify.py`/`lint_dfd.py`/`inventory.py --check` passing, content grounded in real file:line
  evidence and its own quality pass catching genuine inaccuracies — i.e. the mechanism/conformance
  closure condition below was **met**. But it took **three manual re-invocations**: the CLI
  `chat --skill` drive engine wires no proactive compaction (`internal/cli/chat.go:199` sets neither
  `ContextWindowTokens` nor `Compactor`, unlike the daemon at `internal/server/engine_build.go:279,288`),
  so each phase's context grew — the model re-reading files and recomputing STRIDE counts by hand —
  until Ollama hard-rejected the request and the drive aborted on a terminal
  `NewContextTruncationError` rather than a resumable stop. Root-caused into the **P47.x phased-drive
  stability batch** (P47.1-P47.6): single-invocation stability is now the bar, distinct from the
  mechanism closure already demonstrated here.

- **2026-07-27, qwen3.6:35b-a3b-fast vs FirewallRiskRater (hollow-report checks + self-heal,
  validated; phase-6 gap found):** first live run of the ec0127c hollow-report checks + afd6764
  self-heal, against a resumed suite whose `<!-- PENDING -->` markers were already deleted but whose
  finding bodies were empty. **Confirmed working:** self-heal auto-refreshed the stale project
  `verify.py` on launch (two `refreshed 1 stale built-in skill file(s)` notices — data dir +
  project), the three new checks turned the previously false-passing hollow suite (`11 passed, 0
  failed` on the old verifier) into `12 passed, 2 failed` with exact file:line, and the drive fixed
  the `no-duplicate-header-rows` failure live. **New gap:** with the checks now correctly failing,
  the phase-6 verify/quality remediation loop had to fix them — and it lacks the P47.2 overflow-reset
  and the P39.14 anti-monolithic-write guardrail the content phases carry, so the first big fill
  attempt (a whole-file `write_file` of the ~400-line `3-findings.md` to fill 15 empty bodies)
  truncated into a malformed tool call → context overflow → drive aborted **uncaught** (raw `ollama:
  response truncated at the context limit` with no `[notice: … resetting]`, no `.quality-stamp.json`,
  verify rounds 2/3 + the quality pass never ran). Fixed by **P47.7** (overflow-reset in phase 6,
  **shipped**) and **P47.8** (guardrail in the phase-6 prompts, **shipped**); **P47.9** (route
  hollow-body failures to the owning content phase) is the Tier-3 follow-up, **shipped 2026-07-30**,
  and the CLI-only drive vs TUI `/threat-model` parity is noted as **P47.10**.

Reproduce: `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads
outside the workspace root); run `aegis chat "threat model this repo" --skill threat-modeling --mode build --yes`
(the prompt is required — `aegis chat` errors with "no prompt provided" without one) — it now
prints a `phased mode` notice and resets context each phase. Closure condition: the real suite's
PENDING markers reach zero and `verify.py`/`lint_dfd.py`/`inventory.py --check` all pass (met
2026-07-24 on FirewallRuleAnalyzer; **unattended single-invocation** stability tracked by P47.x).

Priority: Tier 2 — every load-bearing harness fix the re-tests have root-caused (P39.5-P39.15) has
shipped. This item stays open only as the conformance **umbrella**, closeable once a live
built-in `--skill` drive is confirmed to reach a verify-clean suite on a local model. Not Tier 1
because it is live-run verification tracking, not independent build work.

**Lead (not yet filed):** the "accurate refusal, error-shaped" exit-code question for the
SCA/secrets scanners. P34.6 checked the *language*-targeted tools; nothing has swept the
SCA/secrets tools for non-zero exits that mean "nothing to do" rather than "I broke". No
`### P<n>.<m>` heading yet.

---

## Open Work — Tier 3

**Status:** none of the filed items open — **P47.4** (phased-drive near-stateless continuations) and
**P47.9** (route hollow-body failures back through the owning content phase), the last two P47.x
batch items, **shipped 2026-07-30**; **P49.2** (on-demand repo-map query tool) **shipped 2026-07-29**
— see [releases.md](releases.md). Both P47.4 and P47.9 were built ahead of their measure-first
triggers (the roadmap called them build-only-if-the-cheaper-items-don't-hold) and each ships with an
escape hatch — `AEGIS_PHASE_CONV=growing` restores the pre-P47.4 growing conversation, and P47.9's
re-entry falls back to the generic verify-fix loop — so a live run can still measure whether they
earn their keep. The leads below are mechanical follow-ups worth their own item once a concrete need
appears.

**Lead — P39.9 residual (repro-gated):** a prefill-latency observability gap remains on the
native path — the only unresolved sliver of P39.9, tracked as a lead rather than a blocker
because it needs a focused repro before it is actionable.

**Lead — doc-inconsistency (surfaced building the P37 scripts):**
(a) **threat-ID form** — `references/skeletons/skeleton-stride.md` writes threat IDs as bare
sequential `T1`/`T2`, but `output-formats.md`'s coverage / Related-Threats examples use composite
`T04.S` form; the P37 scripts match both, but the docs should settle on one canonical form.
(b) **inventory YAML style** — `skeleton-inventory.md`'s example is block-style while directive
#13 says list entries are one-line, and `inventory.py` emits one-line flow mappings; the skeleton
example should match what the generator produces. Both cosmetic doc drift, not code bugs.

**Lead — `recon.py` (P37.1) depth follow-ups**, left out of v1 deliberately:
(a) **data-flow edge inference** — seed the DFD's `DF##` flows from import graphs / client
instantiations so phase 2 starts from real edges;
(b) **config-default resolution** — parse the actual bind-address default from the config struct /
`config.yaml` to settle the deployment class deterministically (and downgrade `EXPOSE`/`0.0.0.0`
to `internal-network` when the k8s `Service` is `NodePort`/`ClusterIP` with no TLS terminator,
rather than over-flagging `internet-facing`);
(c) **richer symbol extraction** — functions/methods and route→handler maps, optionally via
`ctags`/tree-sitter when on PATH;
(d) **target-commit in the sidecar** — let `inventory.py` take an optional `--target-dir`/`--repo`
(or read the commit from `0-assessment.md`) so a run directory kept outside the target repo still
records the analyzed code's commit.

**Lead — task-failure halt (surfaced filing P46.3, not yet its own item):** `codex-build` also
halts entirely and presents the current diff if a task fails 3 times, rather than retrying or
silently rewriting. Aegis's `loopDetector` (`internal/engine/loopdetect.go`) only catches literal
repeated tool-call signatures (`engine.go:734-739`), and `BudgetUSD`/`MaxTokensPerRun` only catch
session-wide cost/token exhaustion — neither tracks "this specific task has failed N times" nor
produces a diff/summary artifact on stopping (both just emit a `KindError` event). The
`structured-build` skill now encodes a stop-when-stuck rule in prose; turning that into a
mechanical per-task failure counter would need a persisted "task" boundary to count against, and
is worth its own item once that boundary exists.

---

## Open Work — Tier 4

### P49.3 — LSP-backed symbol extraction for the repo map (precision without tree-sitter)

`repomap`'s regex extraction is deliberately "breadth and robustness over perfect parsing"
(`repomap.go:5`) — it catches top-level declarations only, misses nested/inner symbols, and can't
produce true call/reference edges (P49.1 gives *import* edges, not call edges). graft's foundation
is tree-sitter, but bundling tree-sitter grammars into Aegis means CGo + per-language grammar blobs
— the exact single-static-binary / no-toolchain property CLAUDE.md protects (the same reason `gosec`
is excluded from the multiscanner). Aegis already ships an alternative: `internal/lsp`. When a
language server is available for a file's language, use `textDocument/documentSymbol` (real nested
symbols) and `textDocument/references` (true call/reference edges) to build the map, falling back to
the regex extractor when no server is present — so precision is opportunistic and the no-runtime
default is untouched.

Priority: Tier 4 — larger, and **measure-first**: only worth building once P49.1/P49.2 have shown
the structural tier matters *and* that regex extraction (not edge coverage) is the limiting factor.
LSP adds per-language server availability as a dependency and startup cost; don't pay it
speculatively. The regex path stays the floor regardless.

### P49.4 — LLM-summarized concept nodes (graft pass-2 analog)

graft's second pass has an LLM summarize files into ~20–50 plain-English "concept nodes" with typed
links — the part that gives an agent *what a subsystem does*, not just its symbols. The analog here
would be an opt-in `aegis index --semantic` pass that groups files into concept summaries cached by
content hash. Two reasons this is last and speculative: (1) it costs an LLM pass per file (real
latency/token cost, cache-invalidation surface), unlike every other P49 item which is deterministic
and free; (2) it overlaps `internal/knowledge` and `internal/memory`, which already carry
project-level prose context — a semantic index might belong *there* rather than as a third store.

Priority: Tier 4 — speculative, **do not build until measured**: only if the deterministic
structural tiers (P49.1–P49.3) demonstrably fail to close the re-discovery gap, and only after
deciding whether the summaries live in a new store or extend `knowledge`/`memory`. No trigger yet.

### P47.6 — Drive model-selection guidance (mitigation, not a code fix) — SHIPPED 2026-07-27 (doc note)

The proximate cause of the self-verification looping on the 2026-07-24 run is the `a3b` 3B-active
"fast" MoE model, which loops more than a steadier/larger model; the `-deep` variant or a larger
model converges with less token burn. **Doc note shipped 2026-07-27:** a "Driving the build on a
local model" section in `internal/skills/builtin/threat-modeling/README.md` documents the
throughput/looping tradeoff (prefer a `-deep`/larger drive model over a small "fast" MoE for
fastest unattended convergence; the fast MoE still finishes — the P47.1-P47.8 code fixes make it
resumable — it just costs more turns). **Optional residual (not built):** a startup hint when a small
MoE is the configured drive model — deferred as speculative until a user actually hits the tradeoff,
since the doc note is the primary deliverable and the code fixes address the mechanism regardless.

Priority: Tier 4 — low urgency, doc/guidance only; the code fixes above address the mechanism
regardless of model. The doc note is done; the optional startup hint stays a lead. Did **not** gate
the P47.x batch.

### P47.10 — CLI/TUI drive-to-completion parity for `/threat-model` — RESOLVED 2026-07-27 (documented, option b)

The phased drive-to-completion lives only in the CLI: `runPhasedSkillDrive` (`internal/cli`) auto-
continues while `<!-- PENDING -->` markers remain, resets context per phase, and runs the phase-6
verify/quality pass. The TUI `/threat-model` (`cmdThreatModel`, `internal/tui/slash.go:990`) instead
injects a single `skillTaskMessage` (skill body + task) into the normal interactive loop and stops
at the model's first yield — no PENDING oracle, no phased reset, no auto verify/quality. So the two
surfaces diverge: `aegis chat --skill threat-modeling` finishes unattended, while `/threat-model`
needs the user to keep nudging.

**Decision (user, 2026-07-27): option (b) — document the difference; the divergence is intentional**
(an interactive TUI user is present to steer, and reviewing between phases is the point). Shipped: the
`/threat-model` `detailedHelp` (`internal/tui/commands.go`) now states it is interactive-by-design and
points to `aegis chat --skill threat-modeling --mode build --yes` for the unattended build; the
threat-modeling README's "Driving the build on a local model" section documents the same CLI-unattended
vs TUI-interactive split. No behavior change — option (a) (`/threat-model --auto`) was **not** built.

Priority: Tier 4 — parity/UX question, resolved as documentation. Did not gate the P47.x code batch.

### P38.8 — External per-phase threat-model wrapper as interim autonomous-build workaround (parked)

Until the built-in drive reliably converges, a completed, verify-clean suite is reachable **today**
by driving Aegis outside the `--skill` loop, one phase at a time with bounded context. A reference
implementation is recorded at `tools/aegis-threatmodel.sh` (+ `tools/THREAT-MODEL-AUTOMATION.md`)
in the FirewallRiskRater repo: it runs `scaffold.py`, then a small **skill-free** `aegis chat` per
phase (architecture → DFD → STRIDE → findings → assessment), re-invoking while a phase's file
still has `PENDING` markers with an "act now" preamble, then runs the P37 checks and loops their
failures back to the model until clean. Because each turn's context is just the prompt + that
phase's files, the compaction wedge and preload bloat that hit the built-in path never trigger.
Validated per-phase on `qwen3.6-fast-32k`, 2026-07-21 — all five content phases completed and the
suite verified clean after the fix loop.

Priority: Tier 4 — a workaround that lives *outside* the harness and duplicates what the drive
loop should do natively. Recorded so the working recipe isn't lost. **Its mechanism is now
implemented in-harness** (2026-07-24, `internal/cli/chat_phased.go`): the built-in
`chat --skill threat-modeling` drive resets context per phase exactly as this wrapper does, so the
external script is fully superseded for threat-modeling and needs no further investment. See the
P38.1 "in-harness phased drive" bullet above; this section stays only as the historical reference.

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of the six daemon-singleton services were per-session-scoped when P25.9 first shipped;
`lsp.Manager` was deliberately left as a shared singleton — its per-session resource-growth
tradeoff was judged worse than the isolation gap. Parked pending a concrete multi-tenant need.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
