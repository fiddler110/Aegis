# Aegis Capability Roadmap

**Last updated:** 2026-08-06 (tenth pass — cleanup: shipped write-ups, closed-batch origins and
review-refutation records relocated to [releases.md](releases.md), per this file's own contract;
ninth pass: P61.8 shipped, the day it was filed)

This document tracks only **open** work and what's next. For shipped-feature history, batch origins
and full design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>`
heading with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape,
so keep it when adding items.

---

## Status

**6 open items.** One in Tier 2 — **P38.1**, which is live-run verification tracking rather than
independent build work — and five in Tier 4: **P61.7**, **P60.3**, **P52.14**, **P49.3**, **P25.9**.
Every Tier-4 item is measure-first, blocked, or explicitly parked, and none has had its promotion
trigger fire. **Tier 1 and Tier 3 are empty.**

**Every filed batch is closed or down to its parked members.** P61.x (cross-adapter drift, 8 filed)
→ P61.7 only. P60.x (sandbox and eval, 4) → P60.3 only. P59.x (local execution, 10 + the P59.11
follow-on) → 0. P55.x (container-only scanning, 9 filed / 8 built) → 0. P52.x (full-stack review,
17) → P52.14 only. P53.x, P57.1, P58.x, P54.2 → 0. Dates, per-item rationale and every write-up are
in [releases.md](releases.md).

**Where the history went.** Batch origins (what each review actually read, and what it judged already
correct), the **refutation records** — candidate findings checked and deliberately *not* filed — and
every shipped, closed or dropped write-up live in releases.md under *Migrated from roadmap.md*
(the 2026-08-01 and 2026-08-06 cleanups). **Read the refutation records before filing anything**
against `internal/provider`, `internal/ollamainfo`, `internal/repomap` or scanner method resolution:
several obvious-looking gaps there have already been checked and answered, and the point of writing
them down was to stop the next review re-filing them.

**What to do next, which is not a tier item:** re-run P38.1's live conformance test. It is the only
open work whose outcome produces new information rather than new code, and it doubles as the
validation **P57.1** is still owed — a fix aimed at a failure observed exactly once.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status: 0 open.** The tier's most recent occupants were the Tier-1 half of the P61.x cross-adapter
drift batch (P61.1-P61.3, shipped 2026-08-05/06); before them, P59.1-P59.3 and the P55.x Tier-1 half.
See [releases.md](releases.md) for the write-ups and for the full Tier-1 history (P52.1, P52.2,
P51.1, P50.1 and the P47.x batch head).

---

## Open Work — Tier 2

**Status: 1 open — P38.1**, below. There is no unbuilt Tier-2 item left from any batch; the last
were P61.4, P61.5 and P61.8 (2026-08-06).

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
one context with no orchestration mis-route — the P36.3-era failure that killed every prior run is
gone.

**Conformance: still unmet.** Every re-test has stalled short of an unattended verify-clean suite,
but each stall has moved the blocker further from the harness and closer to raw model throughput.
The dated log for 2026-07-21 → 2026-07-27 is in [releases.md](releases.md) (*P38.1 re-test log*);
every harness fix those runs root-caused — P39.5-P39.15, P47.1-P47.9, P52.12 — has shipped. Two
entries govern what happens next:

- **2026-07-24, qwen3.6:35b-a3b-fast vs FirewallRuleAnalyzer (phased drive):** the closure condition
  below was **met** — 23 threats / 22 findings across 9 components, `verify.py`/`lint_dfd.py`/
  `inventory.py --check` all passing, content grounded in real file:line evidence — but it took
  **three manual re-invocations**. Single-invocation stability was root-caused into the P47.x batch
  (shipped) and is the bar that remains.
- **2026-08-03, qwen3.6-fast-32k vs an external target (Documentation-as-Code, a Python CLI toolset
  with no network listener) — the current blocker.** Mechanism reconfirmed again: recon → scaffold →
  phased fill across all 5 content phases completed with zero orchestration mis-route, correctly
  self-recovered from a mid-findings-phase context overflow (fresh context, resumed from disk), and
  correctly classified the deployment as `local-desktop`. **Single-invocation conformance still not
  met — new failure mode found.** Phase 6's verify pass correctly caught genuine cross-file defects
  (five threat IDs each reused for two different threats, nine threats missing from the coverage
  table, incomplete `Related Threats` cross-references) and correctly told them apart from mechanical
  ID-format issues — confirmed independently by running `normalize_ids.py --check`, which reported the
  suite already canonical throughout, so the P47.9 reopen (findings phase) was the right call. But the
  reopened phase then got stuck: the model repeatedly mis-derived a T0-vs-T01 zero-padding offset that
  didn't actually exist, re-read the same ~30 analysis-file lines five turns running, and the loop
  detector's one corrective nudge did not break the cycle — `engine: aborting suspected loop:
  identical tool calls repeated 5 turns` ended the run with the suite still verify-failing. A second
  manual `aegis chat` invocation against the same target and model, with a fresh context, resolved
  every defect and reached a fully verify-clean suite (`verify.py` 19/19, `inventory.py --check`
  10/10, `lint_dfd.py` 6/6). That confirms the mechanism and the check scripts are sound — the
  residual gap is the reopened phase's resilience to a model stuck on its own incorrect theory of the
  data, not the overall design. Filed as **P57.1** and **shipped the same day**, so the next re-test
  is also that fix's validation: a loop abort should now reset to a fresh context with the verifier's
  report handed over as ground truth, rather than ending the drive — which is exactly what the
  successful second manual invocation did by hand.

**Direction (user, 2026-07-24):** the strongest lever is making local models **piecemeal both their
reads and their writes**, then finishing with a **quality-validation pass**; P39.12-P39.15 implement
exactly that.

**Reproduce:** `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads
outside the workspace root); run
`aegis chat "threat model this repo" --skill threat-modeling --mode build --yes` (the prompt is
required — `aegis chat` errors with "no prompt provided" without one). It prints a `phased mode`
notice and resets context each phase.

**Closure condition:** the real suite's PENDING markers reach zero and `verify.py` / `lint_dfd.py` /
`inventory.py --check` all pass, **unattended, in one invocation**. Met once, 2026-07-24 on
FirewallRuleAnalyzer; not repeated since.

Priority: Tier 2 — every load-bearing harness fix the re-tests have root-caused has shipped, and
P47.x/P52.12 addressed the two structural gaps (single-invocation stability, CLI-only reach) found
before 2026-08-03. This item stays open only as the conformance **umbrella**, closeable once a live
built-in drive — reachable from any client since P52.12 — is confirmed to reach a verify-clean suite
unattended, in one invocation, on a local model. Not Tier 1 because it is live-run verification
tracking, not independent build work.

---

## Open Work — Tier 3

**Status: 0 open.** P61.6 (2026-08-06) was the last, and the sequencing finding it produced is
recorded with its write-up: built **second** in its batch rather than last, it turned P61.1 into
option wiring and closed P61.3 with no production code, so the "write each fix twice and delete one
copy" cost the item worried about was never paid. Before it: P59.9, P60.2, P60.4 and P57.1. See
[releases.md](releases.md).

**Three leads sit here unfiled, each with a stated promotion trigger.** None is a `### P<n>.<m>` item
yet, deliberately — filing one before its trigger fires would commit to a design question that has no
answer.

- **Whether Aegis should ever mount a container engine socket.** `dockle` is the only tool that wants
  one — it inspects an image through the local engine rather than pulling it — and socket access is
  effectively host root, a third privilege axis beyond the network/workspace split P55.7 is built on.
  It could live in the netscanner image and run socket-mounted and workspace-free, but that is a
  posture decision on its own merits, not a side effect of building a second image. dockle stays
  host-only and says so in code. **Promote when** someone actually needs container-only dockle.
- **Auto-engage the tool-calling shim off a low conformance rate.** Explicitly sequenced as a P53.6
  follow-up rather than dropped: the persisted P53.4 rate is already readable per model
  (`modelcaps.Store.ToolCalling`) and `provider.tool_call_shim` rejects `"auto"` rather than silently
  accepting it as a no-op, precisely so the word stays available for this. **Promote when** live runs
  show the rate predicting drive outcomes — engaging a prose-parsing fallback off a signal that isn't
  trustworthy is worse than requiring the operator to ask for it.
- **Grammar-constrained decoding for *tool calls*** (Ollama structured outputs, llama.cpp GBNF). P59.8
  took this lead at the one caller where it had no open design question — the schema guard's
  corrective retry — and deliberately did **not** widen it. The remaining half attacks the opposite end
  of the problem from the shim: making malformed tool-call JSON mechanically impossible rather than
  parsed-and-declined, targeting models that *do* speak the protocol but truncate or malform arguments
  (the P35.2 failure class). None of the six harnesses reviewed in P53.x does it. Needs its own
  `### P<n>.<m>` heading if pursued.

---

## Open Work — Tier 4

**Status: 5 open**, all measure-first, blocked, or explicitly parked; none has a build trigger yet.

**How to use this tier.** P59.10 and P52.16 were measured and closed 2026-08-05, and both taught the
same lesson: **the measurement contradicted part of the filed item**, so building either one from its
write-up alone would have produced the wrong fix. Take the measurement first, then re-read the item —
do not treat a Tier-4 write-up as a build plan. Details in [releases.md](releases.md).

### P61.7 — Retry/terminal classification is substring matching over model-influenceable text

`classifyStreamError` (`errors.go`) decides whether a mid-stream failure is retried or is fatal by
case-insensitive substring match against a free-form server error string. `terminalStreamSignals`
includes tokens as broad as `"does not support"`, `"unsupported"`, `"malformed"` and
`"invalid request"`; `retryableStreamSignals` includes `"crash"`, `"timed out"` and `"out of memory"`.
`IsResponseHeaderTimeoutError` likewise matches Go's internal transport string, which no compile-time
check protects.

The concern is not that the heuristics are wrong — they are well-chosen and terminal-wins-over-retryable
is the right default. It is that **a control-flow decision is made on text the model can influence**.
Some backends and proxies echo request or generation fragments into the error envelope; a generation
containing "unsupported" that reaches an error message flips a retryable infrastructure failure to
terminal, and the reverse direction (a terminal failure re-classified as retryable) burns a full
prompt-eval per attempt on a slow local model. This is the AI-specific injection surface the P61.x
review was asked to look for, and it is the only one it found — prompt/system isolation, tool-schema
handling and PII logging all came back clean.

It is Tier 4 because the likelihood is genuinely unknown: it depends on whether any backend in real use
echoes generated text into an error envelope, which nobody has measured, and the harness has no
reported incident of a misclassification. Filing a fix now would mean guessing at a structural signal
(status code, an error `type` field) that most local backends do not supply.

**Promote when:** a misclassification is actually observed, or a backend is found that demonstrably
echoes generation content into `{"error":...}`. The cheap first step is a table-driven test that runs a
model-authored string containing each signal through the classifier — not to fix it, but to make the
blast radius explicit and reviewable.
Priority: Tier 4 — real surface, unquantified likelihood, no incident; do not build speculatively.

### P60.3 — Checkpoints capture files only, so `/rewind` is silent about everything else

`internal/checkpoint` snapshots each file a write tool touched, lazily, once, capped at 16MiB
(`checkpoint.go:29`), and rewinding writes those contents back. Within its stated scope that is
correct and the scope is documented. What it means in practice is that rewinding a turn that ran
`pip install`, applied a DB migration, started a background process, or wrote a >16MiB artifact
restores the *source* to its pre-turn state and leaves the environment in its post-turn state — the
one combination that was never actually true, and the user is told the turn was undone.

Orchard's roadmap item is stateful sandboxes: pause, resume and **branch** the whole sandbox, so a
checkpoint is the environment rather than a diff over it. Applied here: if a session owns a
persistent container (P60.2), a checkpoint can be a container snapshot/commit, and rewind becomes
honest about installed packages and process state without a size cap.

Two reasons this was Tier 4 and one still holds. It was strictly downstream of P60.2 — there was no
container to snapshot while every command was `--rm` — and that dependency **cleared on 2026-08-05**
when P60.2 shipped. What remains is that it only helps sessions using the container backend, which is
not the default. And Orchard's version is *roadmap, not shipped code*, so there is
no implementation to read; only the idea transfers.

**Promote when:** the container backend is a realistic default for real sessions, or a user reports a
rewind that restored files into an environment that no longer matched them.
Priority: Tier 4 — no longer blocked, but speculative until someone is actually rewinding inside a
container.

### P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)

`newLoopDetector` is constructed **inside** `Run` (`engine.go:422-424`), so its window resets on
every call. In the TUI and web UI, each user turn is a separate `Run` — so a model that loops
*across* user turns (re-reading the same file every time the user nudges it, re-running the same
failing command after each correction) is never detected, no matter how many turns it repeats.

Fix would be to hoist the detector to session scope, plumbed through `engine.Options` as an optional
caller-owned detector so the daemon can hold one per session while the CLI keeps today's per-`Run`
behavior. The complication worth thinking through before building: a user *legitimately* asking for
the same tool call twice in two turns is not a loop, so a session-scoped detector likely needs a
higher threshold than the per-`Run` one, or needs to reset on any user message that isn't a bare
retry — which is a fuzzier judgment than the current mechanism makes.

**Precondition met 2026-08-01:** P53.2 deliberately landed first, since widening the scope of a
detector that mis-fired on polling and always aborted fatally would have multiplied both defects. A
session-scoped detector would now inherit a sounder mechanism. **Reviewed 2026-08-01, still correctly
parked** — `newLoopDetector` is unchanged, and the design question above is real work rather than a
mechanical port to a wider scope. Not worth building speculatively: without a concrete false-negative
in hand it would ship a detector tuned against a guess rather than an observed failure mode.

**Promote when:** a live run shows a cross-turn loop that per-`Run` detection missed.
Priority: Tier 4 — real but unproven, and the false-positive risk is higher than the current
detector's.

### P49.3 — LSP-backed symbol extraction for the repo map (precision without tree-sitter)

`repomap`'s regex extraction is deliberately "breadth and robustness over perfect parsing"
(`repomap.go:5`) — it catches top-level declarations only, misses nested/inner symbols, and can't
produce true call/reference edges (P49.1 gives *import* edges, not call edges). graft's foundation
is tree-sitter, but bundling tree-sitter grammars into Aegis means CGo + per-language grammar blobs
— the exact single-static-binary / no-toolchain property CLAUDE.md protects. Aegis already ships an
alternative: `internal/lsp`. When a language server is available for a file's language, use
`textDocument/documentSymbol` (real nested symbols) and `textDocument/references` (true
call/reference edges) to build the map, falling back to the regex extractor when no server is present
— so precision is opportunistic and the no-runtime default is untouched.

Priority: Tier 4 — larger, and **measure-first**: only worth building once P49.1/P49.2 have shown
the structural tier matters *and* that regex extraction (not edge coverage) is the limiting factor.
LSP adds per-language server availability as a dependency and startup cost; don't pay it
speculatively. The regex path stays the floor regardless.

*(P49.4, the LLM-summarized concept-node sibling, was dropped 2026-08-03 rather than parked — it
carried two unresolved problems at once. Re-file only if P49.1-P49.3 demonstrably fail to close the
re-discovery gap **and** the "new store vs. extend `knowledge`/`memory`" question has an answer;
rationale in [releases.md](releases.md).)*

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of the six daemon-singleton services were per-session-scoped when P25.9 first shipped;
`lsp.Manager` was deliberately left as a shared singleton — its per-session resource-growth
tradeoff was judged worse than the isolation gap. Parked pending a concrete multi-tenant need.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

---

For shipped feature history, batch origins, refutation records, competitive-landscape review and the
full gap analysis, see [releases.md](releases.md).
