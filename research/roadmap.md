# Aegis Capability Roadmap

**Last updated:** 2026-07-24

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 1 actionable (Tier 2: **P38.1**) + 2 parked (Tier 4: **P38.8**, **P25.9**).
Everything else filed since the last cleanup — the P39.10-P39.15 threat-model harness fixes, the
P40.x TUI/UX batch, P41.1, P44.1, P45.1, P45.2, and the P46.x codex-build track — has **shipped**;
see [releases.md](releases.md) for what each one did.

**Next steps on P38.1:**
1. Re-run the built-in `--skill` drive — now the **in-harness phased drive** (2026-07-24,
   `internal/cli/chat_phased.go`; P38.8's per-phase context reset brought inside the built-in
   path) — on a real target with a local model and confirm it reaches a verify-clean suite. That
   is the closure condition. Compare against `AEGIS_SKILL_DRIVE=linear` (the old single-context
   drive) if a regression is suspected.
2. Housekeeping debt from the 2026-07-23 gpt-oss:20b re-test: **P39.10**/**P39.11** are coded,
   shipped, and verified live, but still need a releases.md entry and regression tests
   (`scanPendingMarkers`/`suiteFileCount` ignoring a materialized-skill PENDING marker; `chat
   --skill` workspace materialization). See the P38.1 body for detail.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** 0 open — all filed Tier 1 items have shipped. See [releases.md](releases.md).

---

## Open Work — Tier 2

**Status:** 1 open — **P38.1**, the threat-model conformance umbrella.

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
  `tier3-batch`, and verified live end-to-end — but still need a releases.md entry and regression
  tests (see Status above). With the scripts reachable, gpt-oss:20b itself then failed to
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

Reproduce: `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads
outside the workspace root); run `aegis chat --skill threat-modeling --mode build --yes` — it now
prints a `phased mode` notice and resets context each phase. Closure condition: the real suite's
PENDING markers reach zero and `verify.py`/`lint_dfd.py`/`inventory.py --check` all pass.

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

**Status:** 0 filed items open — all Tier 3 work has shipped (see [releases.md](releases.md)).
The leads below are mechanical follow-ups worth their own item once a concrete need appears.

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
