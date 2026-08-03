# Aegis Capability Roadmap

**Last updated:** 2026-08-03

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items (5).** The **P55.x container-only-scanning batch is closed** — filed 2026-08-02 off a
full functional test of the multiscanner container and a review of method resolution across all 17
registered scanners, **9 items, 8 built**. Shipped 2026-08-02: **P55.1** (image/source drift),
**P55.2** (all-or-nothing `update-db`), **P55.3** (`verify-image` smoke test), **P55.4**
(container-first resolution), **P55.5** (global pin by default), **P55.6** (DB-age surfacing).
Shipped 2026-08-03: **P55.7** (`aegis-netscanner`) and **P55.8** (gosec two-phase) — the two
structural items that actually close the goal. **P55.9** (relevance gating for the always-on
scanners) was dropped 2026-08-03: P54.2 already showed no correctness gap exists (the dependency
scanners exit 0 with valid empty output on a manifest-free tree), so this was a pure latency
optimization with no measured complaint behind it, and its own write-up noted a naive manifest check
risks trading a slow-correct scan for a fast-wrong one. Not worth tracking speculatively. All
write-ups in [releases.md](releases.md).

The batch's strategic goal was that a user installs **one image instead of 17 tools**, and it is
met. P55.1-P55.6 made the *existing* container trustworthy and preferred; P55.7 and P55.8 extended
containerization to the six tools that had no container path at all, by recognizing that the split
those tools needed was **mount posture** rather than tool category (a second image with network on
and no workspace, ever) and that the one genuine exception — gosec, which needs both — is solved by
the two-phase split `update-db` already uses rather than by relaxing the hardening. Two tools stay
host-only by explicit decision, each with its reason stated in code: zap (already runs from its own
official image) and dockle (needs the container engine socket, a privilege axis that deserves its
own decision).

**The 6 pre-existing items**, unchanged by the P55.x filing and described below in their own
tiers. Before P55.x landed, Tier 2 held **P38.1** only and Tier 3 was empty — **P53.6** (non-native tool-calling
shim) shipped 2026-08-02, closing the **P53.x local-LLM comparative-review batch** at **0 open of
6**. The batch's confirmed capability gap and highest-value item is now built: Aegis had been
detecting a model that writes tool calls into its prose and discarding the signal, and
`provider.tool_call_shim: on` now serves the tool schemas in the system prompt and parses tagged
JSON back into real tool calls — opt-in only, through the same permission gate as a native call,
with a parser that declines a malformed attempt rather than repairing it. The rest of the batch —
**P53.1** (stale `WithKeepAlive` comment), **P53.2** (loop detector: polling exemption +
differentiated outcomes), **P53.3** (compaction summarization-call headroom), **P53.4** (probe
conformance rate) and **P53.5** (persisted per-model capability records) — shipped 2026-08-01/02.
All six write-ups are in [releases.md](releases.md).

**One follow-up P53.6 deliberately left unfiled-as-built:** auto-engaging the shim off a low P53.4
conformance rate. The plumbing exists (the rate is persisted per model via
`modelcaps.Store.ToolCalling`), and the shim's config key rejects `"auto"` rather than accepting it
as a no-op precisely so the word stays available. It is worth taking only once the rate is
trustworthy — i.e. once live runs show it predicting drive outcomes — and not before.

Tier 2 also: **P38.1** — the threat-model conformance umbrella. Mechanism
(recon → scaffold → incremental fill, no orchestration mis-route) is live-confirmed repeatedly;
conformance (a live unattended run reaching a verify-clean suite) is still unmet. Stays open as
live-run verification tracking, not independent build work — see its body below for the full
re-test history. Tier 4, all measure-first or parked with no build trigger yet: **P52.14**
(session-scoped loop detector — reviewed 2026-08-01, see below), **P52.16** (native Ollama
tool-result disambiguation for parallel same-tool calls), **P49.3** (LSP-backed repo-map symbol
precision), **P25.9** (per-session scoping of the last daemon-singleton service, `lsp.Manager` —
parked pending a concrete multi-tenant need).

**Everything else has shipped or closed.** The **P52.x full-stack review batch** (filed
2026-07-30) closed 15 of its 17 items between 2026-07-30 and 2026-08-01 — **P52.15** (wall-clock
run budget) shipped and **P52.17** (auto tool-calling probe on model switch) closed as
already-implemented were the last two, leaving **P52.14** and **P52.16** as the batch's only open
items. Also shipped/closed since the last cleanup: **P51.1** (macOS seatbelt profile), the
**P50.x** phased-drive determinism batch, the **P49.x** repo-map batch head (**P49.1**/**P49.2** —
**P49.3** remains open above; **P49.4** was dropped, see the Tier 4 header), the entire **P47.x**
phased-drive stability batch,
**P48.1** (config-test hermeticity), and **P38.8** (external per-phase threat-model wrapper —
superseded once its mechanism shipped in-harness for P38.1, see releases.md). Full write-ups for
all of it are in [releases.md](releases.md).

**2026-08-01 cleanup.** This file had drifted from its own stated contract — full SHIPPED/CLOSED
write-ups for the P52.x/P51.1/P50.x/P47.6/P47.10 items had accumulated here instead of in
releases.md. Moved them out; no content was changed, only relocated. While auditing, one stale note
was caught and corrected: a "content-substance check routing" follow-up mentioned alongside the
P52.7/P52.8 write-ups (never given its own `P<n>.<m>` number) was still being described as
outstanding. It isn't — file-aware routing (`perFile`, `fileOwnerPhase`) shipped as part of
P52.12's move into `internal/drive` (`internal/drive/drive.go:837-878`); there was no separate gap
to track.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** empty. The P55.x container-only-scanning batch's Tier-1 half — **P55.1** (image/source
drift), **P55.2** (all-or-nothing `update-db`) and **P55.3** (`verify-image` smoke test) — shipped
2026-08-02, the day the batch was filed. See [releases.md](releases.md) for those write-ups and for
the full Tier-1 history (P52.1, P52.2, P51.1, P50.1, and the P47.x batch head).

**P55.x batch origin**, kept as the evidence record for a closed batch. A full functional
test of the multiscanner container (2026-08-02) against a purpose-built multi-language vulnerable
fixture, plus a review of `internal/security`'s method resolution across all 17 registered scanners.
The container's *scanning* was sound — 14/14 bundled tools execute offline and detection is good.
What the test found instead was a cluster of **provisioning** failures, three sharing one shape:
*the scanner silently or loudly stopped working and no layer of the system noticed*. That shape was
Tier 1 because `internal/security/multiscanner.go` already names it as the thing this design most
fears — "a silent all-clear from a scanner that never looked at a database." Four defects found
alongside (kubescape fatal in container mode, kubescape's SARIF unparseable, njsscan broken by the
semgrep removal, grype absent from the pinned image) are the evidence base; two of the four survived
a green `go test ./...`, a successful image build, and a scan that reported findings. Full account
in [releases.md](releases.md).

The batch's strategic driver was a decision to make the container the **only** way Aegis scans, so a
user installs one image instead of 17 tools. All eight built items have shipped; see the Status
section above for what P55.7 and P55.8 changed, and [releases.md](releases.md) for the write-ups.

## Open Work — Tier 2

**Status:** 1 open — **P38.1** (threat-model conformance umbrella), which is live-run verification
tracking rather than independent build work. The Tier-2 half of the P55.x batch — **P55.4**
(container-first resolution), **P55.5** (global pin by default) and **P55.6** (DB-age surfacing) —
shipped 2026-08-02 alongside its Tier-1 half; see [releases.md](releases.md). The **P53.x local-LLM
comparative-review batch**'s Tier-2 half (**P53.1**-**P53.4**, filed and shipped 2026-08-01) and the
earlier batch — P52.3, P52.4, P52.5, P52.6, P52.7, P52.8, P52.9, P52.10, P52.11, the full P47.x
self-contained batch, P48.1, P49.1, and P50.2/P50.3/P50.4 — have shipped; see
[releases.md](releases.md).

**P53.x batch origin.** A comparative review (2026-08-01) of how six frontier harnesses — opencode,
crush, pi, aider, OpenHands, goose — drive local models, cross-checked line-by-line against Aegis.
The review's headline is that Aegis is **ahead** on the four things that dominate local-model bug
trackers elsewhere: native `/api/chat` transport (opencode/crush/pi are all on OpenAI-compat `/v1`,
which structurally cannot carry `num_ctx` — the root cause of crush's flagship "local models won't
call tools" discussion #1828), `/api/show`-based proactive context detection (`internal/ollamainfo`,
vs goose's blind 128k default for unknown models), a bounded resident `keep_alive` default
(`providerfactory/factory.go:31`), and P25.6's `LocalProfile` tool-schema deferral (goose #6883
reports Qwen3-coder silently degrading to XML-in-content past ~5 tools). Five candidate gaps from a
first-pass review were **refuted by direct code reading** and are recorded here so they are not
re-filed: proactive `num_ctx` (`ollamainfo.go:135-162`), config-driven `keep_alive`
(`factory.go:44,215`), ID-based tool-result correlation (`ollama.go:605` — the wire-level name-only
limitation is separately tracked as **P52.16**), `<think>`-before-tool-parse ordering (structurally
impossible to break on the native path — reasoning and tool calls arrive in separate NDJSON fields,
`ollama.go:590-600`), and tool-set shrinking for weak models (P25.6, `builtin.go:170-180`). What
follows is what survived verification.

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
  regression tests; this housekeeping is closed. With the scripts reachable,
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
  P39.6 verify loop, the P38.1 quality pass) — only the context lifetime changed. Originally in
  `internal/cli/chat_phased.go`; lifted into `internal/drive` by P52.12 so every client (daemon,
  TUI, web UI), not just the CLI, gets it. Unit-tested for phase sequencing/completion/prompt
  wiring.

- **2026-07-24, qwen3.6:35b-a3b-fast vs FirewallRuleAnalyzer (phased drive, stability):** the
  phased drive **reached a verify-clean suite** — 23 threats / 22 findings across 9 components, all
  `verify.py`/`lint_dfd.py`/`inventory.py --check` passing, content grounded in real file:line
  evidence and its own quality pass catching genuine inaccuracies — i.e. the mechanism/conformance
  closure condition below was **met**. But it took **three manual re-invocations**: the CLI
  `chat --skill` drive engine wired no proactive compaction, so each phase's context grew — the
  model re-reading files and recomputing STRIDE counts by hand — until Ollama hard-rejected the
  request and the drive aborted on a terminal `NewContextTruncationError` rather than a resumable
  stop. Root-caused into the **P47.x phased-drive stability batch** (P47.1-P47.6, all shipped):
  single-invocation stability was the bar, distinct from the mechanism closure already demonstrated
  here.

- **2026-07-27, qwen3.6:35b-a3b-fast vs FirewallRiskRater (hollow-report checks + self-heal,
  validated; phase-6 gap found):** first live run of the ec0127c hollow-report checks + afd6764
  self-heal, against a resumed suite whose `<!-- PENDING -->` markers were already deleted but whose
  finding bodies were empty. **Confirmed working:** self-heal auto-refreshed the stale project
  `verify.py` on launch, the three new checks turned the previously false-passing hollow suite
  (`11 passed, 0 failed` on the old verifier) into `12 passed, 2 failed` with exact file:line, and
  the drive fixed the `no-duplicate-header-rows` failure live. **New gap found and fixed:** the
  phase-6 verify/quality remediation loop lacked the overflow-reset and anti-monolithic-write
  guardrails the content phases carry — fixed by **P47.7**/**P47.8** (shipped), with **P47.9**
  (route hollow-body failures to the owning content phase) as the Tier-3 follow-up, **shipped
  2026-07-30**.

Reproduce: `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads
outside the workspace root); run `aegis chat "threat model this repo" --skill threat-modeling --mode build --yes`
(the prompt is required — `aegis chat` errors with "no prompt provided" without one) — it now
prints a `phased mode` notice and resets context each phase. Closure condition: the real suite's
PENDING markers reach zero and `verify.py`/`lint_dfd.py`/`inventory.py --check` all pass (met
2026-07-24 on FirewallRuleAnalyzer; **unattended single-invocation** stability shipped via P47.x but
has not had its own dedicated live re-confirmation run since — the umbrella stays open until one
happens).

Priority: Tier 2 — every load-bearing harness fix the re-tests have root-caused (P39.5-P39.15) has
shipped, and P47.x/P52.12 address the two structural gaps (single-invocation stability,
CLI-only reach) found since. This item stays open only as the conformance **umbrella**, closeable
once a live built-in drive — now reachable from any client via P52.12 — is confirmed to reach a
verify-clean suite unattended, in one invocation, on a local model. Not Tier 1 because it is
live-run verification tracking, not independent build work.

**Lead closed 2026-08-02 as P54.2 — swept, no gap found.** The "accurate refusal, error-shaped"
exit-code question for the SCA/secrets scanners (P34.6 checked only the *language*-targeted tools)
was answered by running all six at their pinned versions against an empty tree and a docs/C/shell
tree with no dependency manifests. trivy, grype and syft exit 0 with valid output; gitleaks forces
`--exit-code 0` and its report file is read independently of the run's error; trufflehog exits 0
with empty stdout, which is the success branch. **osv-scanner's exit 128 is the only refusal of this
shape, and it was already interpreted by P34.12** — so osv-scanner is to the SCA/secrets half what
brakeman was to the language-targeted half: the only one. No gate added; the measurements are now
recorded in the `runJSON` doc comment (`internal/security/scanners.go`) so the sweep isn't re-run
from scratch. Write-up in [releases.md](releases.md).

## Open Work — Tier 3

**Status:** empty. **P55.7** (`aegis-netscanner`, a second image split by mount posture) and
**P55.8** (gosec's two-phase warm/analyze split) shipped 2026-08-03, closing the P55.x
container-only-scanning batch — see the Tier 1 header for the batch's origin and
[releases.md](releases.md) for the write-ups.

One decision was deliberately *not* made while building them, and is recorded here rather than
filed: **whether Aegis should ever mount a container engine socket.** `dockle` is the only tool
that wants one — it inspects an image through the local engine rather than pulling it — and socket
access is effectively host root, a third privilege axis beyond the network/workspace split P55.7 is
built on. It could live in the netscanner image and run socket-mounted and workspace-free, but that
is a posture decision on its own merits, not a side effect of building a second image. dockle stays
host-only and says so; promote this to a `### P<n>.<m>` item only if someone actually needs
container-only dockle.

**P53.6** (non-native tool-calling shim — `internal/toolshim`,
`provider.tool_call_shim`) shipped 2026-08-02, the last of the P53.x local-LLM comparative-review
batch (see the Tier 2 header for the batch's origin and for the five candidate gaps that were
refuted by code reading). The two former Tier-3 items **P52.12** (lift the phased drive into the
daemon) and **P52.13** (`workspace.additional_roots`) shipped 2026-08-01. All three write-ups are in
[releases.md](releases.md).

**Two leads left by P53.6, neither filed:**

- **Auto-engage the shim off a low conformance rate.** Explicitly sequenced as a follow-up rather
  than dropped: the persisted P53.4 rate is already readable per model (`modelcaps.Store.ToolCalling`)
  and the config key rejects `"auto"` rather than silently accepting it, so the word stays available
  for exactly this. Promote when live runs show the rate predicting drive outcomes — engaging a
  prose-parsing fallback off a signal that isn't trustworthy is worse than requiring the operator to
  ask for it.
- **Grammar-constrained decoding** (Ollama structured outputs, llama.cpp GBNF). Deliberately not
  folded into P53.6 and still unfiled. It attacks the opposite end of the problem — making malformed
  tool-call JSON mechanically impossible rather than parsed-and-declined — and targets models that
  *do* speak the protocol but truncate or malform arguments (the P35.2 failure class), whereas the
  shim targets models that cannot speak it at all. None of the six reviewed harnesses does it. The
  two share no implementation, so this needs its own `### P<n>.<m>` heading if pursued.

## Open Work — Tier 4

**Status:** 4 open, all measure-first or explicitly parked; none has a build trigger yet. **P55.9**
(relevance gating for the always-on scanners) and **P49.4** (LLM-summarized concept nodes) were
reviewed and dropped 2026-08-03 rather than left parked — see the Status section and the P49.x note
above for why.

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

**Promote when:** a live run shows a cross-turn loop that per-`Run` detection missed.

**Precondition now met (2026-08-01).** P53.2 deliberately landed first: widening the scope of a
detector that mis-fired on polling and always aborted fatally would have multiplied both defects.
With the poll exemption and the nudge-once-then-abort outcome split shipped, a session-scoped
detector would now inherit a sounder mechanism — but the promotion trigger above is still the
observed cross-turn false negative, which has not happened.

**Priority:** Tier 4 — real but unproven, and the false-positive risk is higher than the current
detector's.

**Reviewed 2026-08-01: still correctly parked, no code change.** Confirmed against current code —
`newLoopDetector` is still constructed inside `Run` at every call site, unchanged since this item
was filed. No live run has reported the cross-turn loop this would catch, so the promotion trigger
has not fired. Not worth building speculatively: the design question it flags (what counts as a
legitimate repeat vs. a loop across turns) is real work, not a mechanical port of the per-`Run`
detector to a wider scope, and building it without a concrete false-negative in hand risks shipping
a detector tuned against a guess rather than an observed failure mode.

### P52.16 — Native Ollama tool-result disambiguation for same-tool parallel calls (measure-first)

Ollama's native API correlates tool results **by name, with no ID** — native tool calls carry no
identifier at all (`ollama.go:167-186`), so `translate` emits `role:"tool"` messages keyed only on
`ToolName` (`:266`). The ID→name walk is correct and its ordering rationale (`:213-224`) is sound.

But it does not resolve the case where one turn issues **several calls to the same tool** — three
parallel `read_file`s, which the engine explicitly permits since read-capability tools run
concurrently in `runTools`. All three results become `role:"tool"` messages that are identical in
their correlation metadata, leaving position as the only signal. This is a protocol limitation rather
than an Aegis bug, but it is a plausible and untested contributor to the small-model confusion seen on
multi-read turns.

Cheap mitigation to trial: on the native path only, prefix each tool-result content with a compact
echo of the originating call (`[read_file path=internal/engine/engine.go]`), so the association is
carried in content where the protocol can't carry it in metadata. That costs a few tokens per result
and could plausibly *hurt* by adding noise — which is exactly why this is measure-first.

**Promote when:** a live A/B on a multi-read turn shows the model conflating results. Do not ship the
mitigation without that measurement. **Priority:** Tier 4 — speculative; the hypothesis is plausible
but unverified.

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

**P49.4 (LLM-summarized concept nodes) dropped 2026-08-03.** graft's second pass has an LLM
summarize files into ~20–50 plain-English "concept nodes" with typed links; the analog here would
have been an opt-in `aegis index --semantic` pass. Dropped rather than parked because it carries two
unresolved problems at once rather than one: it costs an LLM pass per file (real latency/token cost,
cache-invalidation surface) where every other P49 item is deterministic and free, and it overlaps
`internal/knowledge`/`internal/memory`, which already carry project-level prose context — so before
it could even be measure-first candidate it needs a decision on whether it's a new store or belongs
in one of those. Re-file only if the deterministic structural tiers (P49.1–P49.3) demonstrably fail
to close the re-discovery gap and that store question has an answer.

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of the six daemon-singleton services were per-session-scoped when P25.9 first shipped;
`lsp.Manager` was deliberately left as a shared singleton — its per-session resource-growth
tradeoff was judged worse than the isolation gap. Parked pending a concrete multi-tenant need.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
