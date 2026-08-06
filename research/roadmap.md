# Aegis Capability Roadmap

**Last updated:** 2026-08-05 (sixth pass — the P61.x cross-adapter drift batch filed, 7 items;
fifth pass: P59.11 shipped; fourth pass: P59.10 and P52.16 measured, promoted and shipped, and the
P59.9 loose end measured and closed)

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Seven filed 2026-08-05: the P61.x cross-adapter drift batch**, from a review of the LLM-interaction
surface (`internal/provider` and its three adapters, the decorator chain, and `engine.turn`'s stream
consumption) commissioned as a general Go/architecture/AppSec review. As with P59.x it changed **no
structural judgement** — the `Adapter` seam, the decorator chain and its `Unwrap()` traversal,
`sse.Emitter`'s cancellation-aware sends, and the absence of prompt/PII logging are all right, and
`go vet ./...` plus the provider and engine suites are clean. Four premises of the review brief were
checked and found already satisfied (a single LLM abstraction, `context` propagation, request-body
caps via `http.MaxBytesReader`, loopback validation of `server.addr` and `provider.base_url` — FIND-08
and FIND-03), and are recorded here so they are not re-filed.

What it found instead is a single theme, and it is the mirror image of P59.x's: **every piece of
stream hardening was fixed in the native Ollama adapter and never ported to the other two.** P59.2's
idle watchdog, P59.3's missing-completion-chunk check, P50.1's transport-error classification and
P59.1's `num_predict` clamp all exist exactly once, in `internal/provider/ollama`. The openai adapter
is not a cloud-only path — it is what serves Ollama's OpenAI-compat `/v1` endpoint, which
`docs/installation.md` still configures via `OPENAI_API_KEY="ollama"` — so each gap is live on a local
backend. The root cause is structural rather than three oversights: `internal/provider/sse` abstracts
the *pieces* of an SSE consumer (client, scanner, emitter, error-response) but not the *stream
lifecycle*, so there is no shared place for a lifecycle fix to land and each adapter's `consume` has
drifted independently. **P61.1**-**P61.3** are the three live gaps (Tier 1), **P61.6** is the
structural fix that stops the next one (Tier 3), and the batch should be sequenced with that
dependency in mind — see P61.6 for which order is cheaper. Full batch origin under Tier 1.

**P59.11 shipped 2026-08-05**, the fifth batch that day and a direct follow-on: P59.10 had fixed the
zero-tool nudge's 51x prefill regression and left the **tool-failure nudge at a measured 25.9x**,
because that one was only *bounded* to one per run rather than genuinely spent, so retracting it early
would have removed a correction whose failures could recur. P59.11 supplies the missing observation
instead of assuming it — the failure streak actually clearing — and pairs early retraction with
**re-injectability**, so a relapse earns a fresh nudge by append. The one-per-run behavior that
mattered (never nag on consecutive failing rounds) is preserved by an outstanding-nudge gate rather
than by the count. Write-up in [releases.md](releases.md); no tier held an item for it, since P59.10
had recorded it as a deliberate non-fix rather than filing it.

**Open items (12)** — 5 carried, plus the 7 filed by P61.x above. **P59.10 and P52.16 shipped
2026-08-05** — the fourth batch that day, and the
first assembled by *taking the measurements* Tier 4 items had been parked behind rather than by
picking work off a tier. Both promotion triggers fired, and in both cases the measurement also
corrected the filed hypothesis. **P59.10**: retraction does break Ollama's prefix cache (51x — 3604ms
of prefill against 71ms unretracted, indistinguishable from a cold reprocess), but *not* in the
"bounded to the tail" way the item assumed — the zero-tool nudge is injected as early in a run as it
is possible to be, so retracting it at run end invalidates everything the run produced. Fixed by
changing **when** it is retracted (the moment the first tool round makes it permanently ineligible to
re-fire), not whether — which keeps all of P25.3, unlike the item's own proposed fix. **P52.16**: the
conflation is real and confined to small models (qwen2.5-coder:1.5b 32/40 → 38/40 with the echo;
qwen3:14b and gemma4:12b at ceiling either way), and the echo never hurt, which was the stated risk —
shipped narrowed to same-tool rounds only, so unambiguous rounds keep their exact bytes. **The P59.9
loose end is closed too**, and it is the one that did *not* become a behavior change: the local
default of 1 is right, but for latency (~40% more throughput at ~70% worse turn latency), not for the
correctness hazard it was justified by — four concurrent 12k-token requests were not truncated at
all. Write-ups in [releases.md](releases.md). **That closes the P59.x batch entirely (0 open of 10)**
and leaves **P60.x at 1 of 4** (P60.3, Tier 4).

**P59.9, P60.2 and P60.4 shipped 2026-08-05**, the third batch that day, and they
**close Tier 3 entirely (0 open)**. Their shared shape is the inverse of the batch below: not a fact
that failed to reach the component needing it, but a **policy nobody owned** for something the system
was doing anyway. Nothing bounded how many requests reached one local model server, so every
concurrent request was built believing it owned the whole GPU (**P59.9** — a semaphore at the adapter
layer, auto-bounding local backends to one in-flight request; the default is a stated policy, not the
measured value the item asked for, and that measurement is the one loose end left). Nothing owned a
sandboxed container's lifetime, so it had none, and no state survived the tool call that created it
(**P60.2** — one container per workspace directory, labelled, TTL-bounded and reaped; this
**unblocks P60.3**, which stays Tier 4 on its own merits). And nothing separated the harness from the
model when a live run failed (**P60.4** — the task definition now lives outside the test that asserts
on our SSE stream, so a second CLI agent measured on the same task and model is a control group).
Write-ups in [releases.md](releases.md). **That leaves P59.x at 1 open of 10** (P59.10, Tier 4,
measure-first) and **P60.x at 1 of 4** (P60.3, Tier 4).

**P59.7, P60.1 and P59.8 shipped 2026-08-05**, earlier the same day. They share a shape rather than a subsystem: in each, something the system already knew was not
reaching the component whose behavior depended on it — the adapter's escalated context window never
reached the engine's compaction trigger (**P59.7**, closing the P59.x Tier-2 half and the batch's
last non-Tier-3 item), the operator's intent to bound a sandboxed command had nowhere to be
expressed at all (**P60.1**), and the schema guard's requirement was expressible to the backend
ahead of generation and wasn't being expressed (**P59.8**, which takes the parked
grammar-constrained-decoding lead at its one unambiguous caller and explicitly does **not** widen it
to tool calls). P59.8's one implicit design question — *which* turn gets constrained — was answered
"only the schema-guard corrective retry, with tools suppressed": a first turn is where the model does
the work, and a grammar forcing a JSON object out of it forbids exactly that. Write-ups in
[releases.md](releases.md). At that point P59.x stood at 2 open of 10 (P59.9, P59.10) and P60.x at 3
of 4 — all but P59.10 and P60.3 closed by the batch above, later the same day.

**P59.4, P59.5 and P59.6 shipped 2026-08-05** — the rest of the Tier-2 half of the P59.x
batch. All three take a mechanism built for a cloud provider
and ask what it means on one local GPU: a token budget that answered a billing question when the
local user was asking a work question (**P59.4** — resolved with a second, separately-named
`cost.max_generated_tokens_per_run` rather than by splitting one key's meaning across provider
classes), a documentation recommendation that re-introduced the model-eviction churn `keep_alive`
exists to prevent (**P59.5** — the guard now runs on the resident model locally, with a new explicit
`output_guard.model` for operators who want the split anyway), and a pair of prose-tool-call checks
gated on zero-call turns, which made the commonest partial-protocol shape invisible (**P59.6** — a
mixed round is now declined and corrected, the safer of the two readings the item named).
**P59.1, P59.2 and P59.3 shipped 2026-08-04**, the day after the batch was
filed — the Tier-1 half. Write-ups in
[releases.md](releases.md). **Four filed 2026-08-04: the P60.x sandbox-and-eval batch**, from a read of
Microsoft's [Orchard](https://github.com/microsoft/Orchard) assessed for what an RL-training
substrate built on Kubernetes has to offer a single-user local harness. Most of it has nothing —
trainer, k8s, Redis orchestration, 1000-sandbox scale — and the batch records that non-adoption
explicitly. What survived clusters on one seam the prior batches never touched: **`internal/sandbox`'s
`Backend` is stateless per command.** Container runs get privilege hardening but no resource limits
(**P60.1**), every tool call is a fresh `docker run --rm` so nothing an agent does to its environment
survives the call that did it (**P60.2**), and checkpoints therefore snapshot files only, making
`/rewind` silent about installed packages and process state (**P60.3**, which P60.2 has since
unblocked).
**P60.4** is separate: the live-workflow eval measures Aegis and the model fused together, so a
failure cannot distinguish a weak model from a scaffolding regression — Orchard's one genuinely
portable idea is holding the environment fixed and swapping the harness for a baseline. Full batch
origin under Tier 2. **P60.1 shipped 2026-08-05** (`sandbox.limits.*`, applied per-runtime to
whichever flags that runtime's CLI verifiably accepts — see [releases.md](releases.md)); the other
three are unbuilt.

**Ten filed 2026-08-04: the P59.x local-execution review batch** — an
architecture and design review of the harness *as a local-execution system*, covering prompt/context
management, the Ollama request path, error handling and output parsing. It changed no structural
judgement (the adapter seam, capability-tagged tools, per-model context resolution and the phased
drive are all the right answers and none is touched), and it refuted two candidate findings by code
reading rather than filing them. What it found clusters on one theme none of the prior batches
covers: **the context subsystem reasons about the prompt, and nothing reasons about the
generation.** `num_ctx` is prompt+completion on Ollama, but `num_predict` rides through unclamped
from a 32768 default (**P59.1**), the compaction trigger is a flat 85% that predates any generation
reserve (**P59.1**), no budget or timeout can fire *during* a turn so a wedged runner has no valve
(**P59.2**), and a stream that ends without a `done` chunk is surfaced as a finished answer
(**P59.3**). Tier 2 covers the accounting and detection edges around that theme — token-budget
semantics on unpriced backends (**P59.4**), the `small_model` guard recommendation causing model
thrash on VRAM-constrained hosts (**P59.5**), prose tool-call detection gated on zero-call turns
(**P59.6**), and the engine's immutable window versus the adapter's escalation floor (**P59.7**).
Tier 3 takes up the grammar-constrained-decoding lead P53.6 left unfiled, narrowed to the schema
guard where it has no open design question (**P59.8**), and files local-backend admission control
(**P59.9**). **P59.10** (retraction versus the KV prefix cache) is Tier 4, measure-first, and the
diagnostic that settles it already ships. Full batch origin under Tier 1. **P59.1-P59.3 shipped
2026-08-04, P59.4-P59.9 on 2026-08-05, and **P59.10 later the same day**, once its measurement was
taken (see [releases.md](releases.md)). The batch is closed at 10 of 10.

**The 5 pre-existing items.** The **P55.x container-only-scanning batch is closed** — filed 2026-08-02 off a
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

**Two items filed and shipped 2026-08-04: P58.1 and P58.2** — the **daily-copilot batch**, and the
first time this project has explicitly evaluated Aegis as an everyday assistant for research,
documentation and code analysis rather than as a security/threat-modeling task runner. A search of
`research/` confirmed the framing was genuinely new, not a revisited decision. Most of the review's
answer was that the capability is already there and already good: `deep-research` is a real
structured-research workflow, the report/diagram skills produce real deliverables, sessions are
daemon-backed with a picker and no git-repo requirement, and `workspacetrust` gates *project
config* rather than plain chat, so a general question asked outside any repo has no friction. Two
gaps were real and both were narrow. **P58.1:** `internal/cron` is a generic scheduler, so a daily
digest was already expressible — but a fire's outcome only reached `cron_runs`, readable via a
`cron_history` tool call, so nothing was ever pushed and a scheduled digest was self-defeating.
`internal/notify` (desktop + webhook, P5.4) already existed with exactly one producer, so cron
became its second rather than growing a parallel mechanism: a per-job `notify` opt-in, hung off the
same call site that writes the audit record so the two can never disagree. **P58.2:**
`documentation-as-code` reads like a generic docs skill and is not (it requires an external
`docforge.py` toolchain and defers to `latex-report` otherwise), and the report skills are
report-shaped; nothing covered maintained in-repo documentation. `document-codebase` fills that,
organised around the four ways generated docs fail, and is surfaced as `/document`. Write-ups in
[releases.md](releases.md). Neither opens follow-on work.

**One item filed and shipped 2026-08-03: P57.1.** A dedicated P38.1 live re-confirmation run (see
the Tier 2 section below) reconfirmed the threat-modeling phased drive's mechanism, but the first
invocation aborted rather than reaching a verify-clean suite: the phase-6 content-substance reopen
(P47.9) got stuck in an unbreakable model reasoning loop, and only a second manual invocation
succeeded. **Built the same day:** an engine loop abort is now a resumable fresh-context reset with
its own budget (the same treatment a context overflow and a tool-failure trip already get), and the
recovered retry carries a directive telling it the verifier's report is the finding rather than
inviting it to re-derive the mismatch. Write-up in [releases.md](releases.md). P38.1's live re-test
is what closes the loop on it — the fix targets a failure shape seen once.

The batch's strategic goal was that a user installs **one image instead of 17 tools**, and it is
met. P55.1-P55.6 made the *existing* container trustworthy and preferred; P55.7 and P55.8 extended
containerization to the six tools that had no container path at all, by recognizing that the split
those tools needed was **mount posture** rather than tool category (a second image with network on
and no workspace, ever) and that the one genuine exception — gosec, which needs both — is solved by
the two-phase split `update-db` already uses rather than by relaxing the hardening. Two tools stay
host-only by explicit decision, each with its reason stated in code: zap (already runs from its own
official image) and dockle (needs the container engine socket, a privilege axis that deserves its
own decision).

**Those 5 items** (the open set once P57.1 shipped, before the P59.x filing above), unchanged by the P55.x filing and described below in their own
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
re-test history. **2026-08-03 re-test filed a new Tier-3 item, P57.1:** mechanism reconfirmed
again, but the first invocation aborted on an unbreakable model reasoning loop during the phase-6
content-substance reopen, and only a second manual invocation reached a verify-clean suite — see
below and the Tier 3 write-up. Tier 4, all measure-first or parked with no build trigger yet: **P52.14**
(session-scoped loop detector — reviewed 2026-08-01, see below), **P49.3** (LSP-backed repo-map symbol
precision), **P25.9** (per-session scoping of the last daemon-singleton service, `lsp.Manager` —
parked pending a concrete multi-tenant need).

**Everything else has shipped or closed.** The **P52.x full-stack review batch** (filed
2026-07-30) closed 15 of its 17 items between 2026-07-30 and 2026-08-01 — **P52.15** (wall-clock
run budget) shipped and **P52.17** (auto tool-calling probe on model switch) closed as
already-implemented were the last two, leaving **P52.14** and **P52.16** as the batch's only open
items; **P52.16 shipped 2026-08-05** once its A/B was run, so **P52.14** is the last one standing. Also shipped/closed since the last cleanup: **P51.1** (macOS seatbelt profile), the
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

**Status:** 3 open — **P61.2**, **P61.1** and **P61.3**, the Tier-1 half of the P61.x cross-adapter
drift batch filed 2026-08-05 (origin below). All three are the same defect wearing three faces: a
hardening measure that exists only in `internal/provider/ollama` and was never ported to the openai
and anthropic adapters. Each is small and independent; **P61.6** (Tier 3) is the structural fix that
makes the fourth one impossible, and the sequencing question between them is recorded there. They are
listed **P61.2 first**, out of numeric order and deliberately: it is the only one that produces a
silently wrong answer rather than a degraded recovery, and it is the one that should not wait behind
P61.6's refactor.

**P59.1**, **P59.2** and **P59.3** — the Tier-1 half of the P59.x
local-execution review batch (origin below) — shipped 2026-08-04, the day after the batch was filed;
see [releases.md](releases.md) for the write-ups. The P55.x container-only-scanning batch's Tier-1 half — **P55.1**
(image/source drift), **P55.2** (all-or-nothing `update-db`) and **P55.3** (`verify-image` smoke
test) — shipped 2026-08-02, the day the batch was filed. See [releases.md](releases.md) for those
write-ups and for the full Tier-1 history (P52.1, P52.2, P51.1, P50.1, and the P47.x batch head).

**P59.x batch origin.** An architecture and design review (2026-08-04) of the harness as a
*local-execution* system specifically: prompt/context management, the Ollama request path, error
handling and output parsing, read end-to-end across `internal/engine`, `internal/provider/ollama`,
`internal/provider/sse`, `internal/compaction`, `internal/tokenest`, `internal/server`
(`engine_build.go`, `contextwindow.go`, `messages.go`), `internal/guard` and `internal/drive`,
cross-checked against `docs/`. Most of the obvious local-model traps are already closed and the
seams are right — the review changed no structural judgement. What it found instead clusters on one
theme the existing work has not covered: **the context subsystem reasons about the prompt and
nothing reasons about the generation.** `num_ctx` is prompt+completion on Ollama, but `num_predict`
is passed through unclamped, the compaction trigger is a flat percentage that predates any
generation reserve, and no budget or timeout can fire *during* a turn — only between turns.
P59.1-P59.3 are that theme and all three shipped 2026-08-04. P59.4-P59.7 are the accounting and
detection edges around it, and **all four shipped 2026-08-05** along with **P59.8** (write-ups in
[releases.md](releases.md)) — P59.7 last, closing the batch's Tier-2 half. **P59.9** (Tier 3, local-
backend admission control) shipped later the same day, closing that tier too. **P59.10** followed on
2026-08-05 once measured, closing the batch at 10 of 10.

Two candidate findings were **refuted by code reading** and are recorded here so they are not
re-filed. **SSE backpressure**: `emit` is called synchronously per token from the stream goroutine
and looked like it could stall a turn behind a slow consumer — P21.5 already solved it
(`messages.go:132-147`, `newSSEWriter` drops oldest on a full buffer rather than blocking the run).
**Native tool-result correlation for parallel same-tool calls**: real, and already filed as
**P52.16** — this review independently reached the same mitigation (prefixing each result with a
compact echo of the originating call). **The dedicated A/B was run 2026-08-05 and P52.16 shipped**:
the conflation is real on a small model and absent on capable ones, so the echo is applied only to
rounds that call the same tool more than once. Note for future reviews that the first A/B scored a
false 10/10 in *both* arms because the fixture's file bodies named their own paths — a
content-attribution shortcut that never exercises the missing wire metadata.

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

### P61.2 — A truncated stream is reported as a completed answer on openai/anthropic

`ollama.go:1021` (P59.3) treats a clean EOF with no `done:true` chunk as a transport error, for a
reason its comment states plainly: emitting `EventDone` there leaves usage zeroed and `stop` at
`StopEndTurn`, so "a truncated response was surfaced as a complete short answer whose stop reason
claims the model chose to end its turn." Neither other adapter has the check — `openai.go:515` and
`anthropic.go:365` emit `EventDone` unconditionally once the scan loop ends.

The downstream consequence is at `engine.go:1812`: zeroed usage is silently replaced with
`tokenest` estimates and flagged `IsEstimated`, which is indistinguishable from a legitimately
usage-free provider. A cut-off generation therefore produces a plausible short answer, correct-looking
usage, and no error on any surface. This is the batch's only *silent wrong answer* — the other two
Tier-1 items degrade recovery, this one degrades output.

Fix mirrors P59.3: require a terminal chunk (`[DONE]` / `message_stop`) before `EventDone`, and
classify its absence as `provider.NewTransportError` so it takes the same recovery path.

Priority: Tier 1 — silent incorrect output, on a live local path, with a shipped precedent to copy.

### P61.1 — The stream idle watchdog exists only on the native Ollama path

`provider.stream_idle_timeout` (P59.2) is a documented config key that applies to **one of three
adapters**. `providerfactory/factory.go:248` passes `ollama.WithStreamIdleTimeout`; lines 219 and 279
(anthropic, openai) pass only `WithResponseHeaderTimeout`, and no `WithStreamIdleTimeout` option
exists on either adapter to pass. `openai.consume` (`openai.go:353`) and `anthropic.consume`
(`anthropic.go:321`) take no timeout parameter at all.

P59.2's own reasoning is what makes this a Tier-1 gap rather than a missing feature: the streaming
client deliberately leaves `Client.Timeout` at zero, `ResponseHeaderTimeout` stops applying the moment
headers arrive, and `cost.max_wall_clock_per_run` is polled between turns and is off by default. So on
those two adapters **nothing bounds a wedged runner** — the consumer blocks on a read forever, which is
precisely the state P59.2 was filed to end. And the openai adapter is a local path: it is what talks to
Ollama's OpenAI-compat `/v1` endpoint.

Fix is to give both adapters the option and wire it in `buildOne` — but see **P61.6**, which would
supply the watchdog once instead of three times.

Priority: Tier 1 — a config key users will reasonably believe is global silently protects one backend.

### P61.3 — Mid-stream read failures on openai/anthropic are unclassifiable, so backend recovery never fires

`openai.go:480` and `anthropic.go:361` wrap `scanner.Err()` with a bare `fmt.Errorf`. `ollama.go:1001`
wraps the identical condition in `provider.NewTransportError`, deliberately (P50.1): "wrap it as a
transport APIError (not a bare error) so `IsBackendUnavailableError` classifies it and the phased drive
waits for the backend to return and resumes from disk."

Because both `IsBackendUnavailableError` and `retryable()` begin with `errors.As(err, &APIError)` and
return false otherwise, a killed `ollama serve` mid-stream on the compat path is classified by
**nothing**: no retry, no `waitForBackend`, no resume-from-disk. It aborts the drive on an
unclassifiable error — the exact failure P50.1 exists to prevent, on a sibling code path. The openai
adapter also lacks P35.12's `bufio.ErrTooLong` naming, so an oversized tool-call payload there still
surfaces as the opaque "token too long".

Priority: Tier 1 — a shipped recovery mechanism is inert on two of three adapters; the fix is a
constructor swap.

## Open Work — Tier 2

**Status:** 3 open — **P38.1** (threat-model conformance umbrella), which is live-run verification
tracking rather than independent build work, plus **P61.4** and **P61.5** from the P61.x batch (origin
under Tier 1). **There is no unbuilt Tier-2 item left from any earlier batch.** **P59.7** (the
last Tier-2 item of the P59.x local-execution review batch, origin under Tier 1) and **P60.1** (the
Tier-2 head of the P60.x sandbox-and-eval batch, origin below) both shipped 2026-08-05; see
[releases.md](releases.md), and note that P59.7 connected the escalated window to the engine's
compaction *trigger* only — the summarizer's own budget stays pinned at the sized window, which was
already a deliberate choice and is now labelled as one rather than reading like the same oversight.
**P59.4**, **P59.5** and **P59.6** shipped 2026-08-05; see
[releases.md](releases.md) for the write-ups, and note that P59.4 took neither of the two fixes its
item proposed — a second, separately-named key (`cost.max_generated_tokens_per_run`) delivers what
the more useful option wanted without paying its stated cost of splitting one key's meaning across
provider classes. The Tier-2 half of the P55.x batch — **P55.4**
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

**P60.x batch origin.** A read (2026-08-04) of Microsoft's
[Orchard](https://github.com/microsoft/Orchard) — an open agentic-modeling framework whose three
layers are domain recipes (Orchard-SWE/GUI/Claw), **Orchard Env** (a Kubernetes-native sandbox
service plus a Modal-style Python SDK spinning up ~1000 isolated containers on demand), and an RL
trainer on a vendored `slime` fork — assessed for what transfers to a single-user local harness.
Most of it does not, and the non-adoption is the more useful half of the record: the trainer, slime,
Redis-backed multi-replica orchestration, Calico NetworkPolicies, k8s itself and the in-pod HTTP
agent are all answers to a scale question Aegis does not have. Four things survived that filter, and
each was checked against the code rather than taken from the README: per-sandbox resource limits
(**P60.1**), a session-lifetime sandbox instead of a container per command (**P60.2**), sandbox
snapshot/branch as the basis for a truthful rewind (**P60.3**, Tier 4), and harness-portable
evaluation as a control group for the live-workflow tier (**P60.4**). P60.1 and P60.2 are the two
that stand on their own; the batch's centre of gravity is that `internal/sandbox`'s `Backend` was
stateless per-command, which P60.2 and P60.3 are both about. **P60.1, P60.2 and P60.4 all shipped
2026-08-05**, which leaves **P60.3** — no longer blocked, since P60.2 gave a session a container to
snapshot, but still Tier 4 while the container backend is not a realistic default.

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

- **2026-08-03, qwen3.6-fast-32k (local Ollama) vs an external target (Documentation-as-Code, a
  Python CLI toolset with no network listener) — dedicated live re-confirmation run:** mechanism
  reconfirmed again — recon → scaffold → phased fill across all 5 content phases (architecture, DFD,
  STRIDE analysis, findings, assessment) completed with zero orchestration mis-route, correctly
  self-recovered from a mid-findings-phase context overflow (fresh context, resumed from disk), and
  correctly classified the deployment as `local-desktop`. **Single-invocation conformance still not
  met — new failure mode found.** Phase 6's verify pass correctly caught genuine cross-file defects
  (five threat IDs each reused for two different threats, nine threats missing from the coverage
  table, incomplete `Related Threats` cross-references) and correctly told them apart from
  mechanical ID-format issues — confirmed independently by running `normalize_ids.py --check`, which
  reported the suite already canonical throughout, so the P47.9 reopen (findings phase) was the
  right call. But the reopened phase then got stuck: the model repeatedly mis-derived a T0-vs-T01
  zero-padding offset that didn't actually exist, re-read the same ~30 analysis-file lines five turns
  running, and the loop detector's one corrective nudge did not break the cycle — `engine: aborting
  suspected loop: identical tool calls repeated 5 turns` ended the run with the suite still
  verify-failing. A second manual `aegis chat` invocation against the same target and model, with a
  fresh context, resolved every defect and reached a fully verify-clean suite (`verify.py` 19/19,
  `inventory.py --check` 10/10, `lint_dfd.py` 6/6). Confirms the mechanism and the check scripts are
  sound — the residual gap is the reopened phase's resilience to a model stuck on its own incorrect
  theory of the data, not the overall design. Filed as **P57.1** (Tier 3).

Reproduce: `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads
outside the workspace root); run `aegis chat "threat model this repo" --skill threat-modeling --mode build --yes`
(the prompt is required — `aegis chat` errors with "no prompt provided" without one) — it now
prints a `phased mode` notice and resets context each phase. Closure condition: the real suite's
PENDING markers reach zero and `verify.py`/`lint_dfd.py`/`inventory.py --check` all pass,
**unattended, in one invocation**. Met once, 2026-07-24 on FirewallRuleAnalyzer. The dedicated live
re-confirmation run this item was waiting on happened 2026-08-03 (above) — it did not repeat that
result: the first invocation aborted on the new P57.1 loop rather than reaching a clean suite, so
single-invocation conformance is still unmet. The umbrella stays open. **P57.1 shipped the same
day** (Tier 3), so the next re-test is also the validation of that fix: a loop abort should now
reset to a fresh context with the verifier's report handed over as ground truth, rather than ending
the drive — which is exactly what the successful second manual invocation did by hand.

Priority: Tier 2 — every load-bearing harness fix the re-tests have root-caused (P39.5-P39.15) has
shipped, and P47.x/P52.12 address the two structural gaps (single-invocation stability,
CLI-only reach) found before 2026-08-03. This item stays open only as the conformance **umbrella**,
closeable once a live built-in drive — now reachable from any client via P52.12 — is confirmed to
reach a verify-clean suite unattended, in one invocation, on a local model; the 2026-08-03 re-test
found one further blocker (P57.1) rather than closing it. Not Tier 1 because it is live-run
verification tracking, not independent build work.

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

### P61.4 — The `num_predict` clamp has no counterpart on the OpenAI-compat path

`ollama.clampNumPredict` (`ollama.go:686`, P59.1) reconciles `provider.max_tokens` — default **32768**
(`config.go:1302`) — against the room actually left in the served window. `openai.go:259-263` assigns
`req.MaxTokens` straight to `MaxTokens`/`MaxCompletionTokens` with nothing reconciling the pair. Against
Ollama's own 4096 default window over `/v1`, that is the same request-8x-the-window shape P59.1
describes, with the same consequence: a ceiling hit mid-generation returns `finish_reason "length"` →
`StopMaxTokens` → the engine's continue-from-where-you-left-off retry → context growth until the run
burns to its iteration cap.

This is Tier 2 rather than Tier 1 because the fix is genuinely harder here and partly out of reach: the
compat endpoint cannot carry `num_ctx` and does not report the served window, so the adapter has no
honest number to clamp against — which is the same structural limitation P53.x's comparative review
already identified as the root cause of crush's "local models won't call tools" discussion #1828. The
options worth weighing are (a) plumb the server-resolved window (`internal/ollamainfo` already detects
it per model) onto the request so the openai adapter can clamp when one is present, or (b) leave the
clamp Ollama-native and instead have `aegis doctor` refuse the unreconciled `max_tokens`/window pair,
which is where P59.1's own comment says the user should be told once.

Priority: Tier 2 — real and live, but the cheap fix is a diagnostic and the correct fix needs a design
decision about whether the compat path gets window awareness at all.

### P61.5 — Five small correctness and fidelity papercuts in the adapter and decorator chain

Grouped because each is a few lines and none warrants its own item, but all are in the same layer and
all are real:

- **The `think`-rejection retry discards the original 400.** `ollama.go:587-590` captures `rejection`,
  then overwrites `err` with the retry's error. If the retry also fails, the caller sees only the
  second failure and the "does not support thinking" signal — the thing that explains the whole
  sequence — is gone. `errors.Join(rejection, err)`.
- **`failoverAdapter.Stream` never checks `ctx` between targets** (`failover.go`). A cancelled context
  walks the entire chain, logging a `WARN` per hop.
- **`admissionAdapter.Stream`'s first `select`** (`admission.go`) offers `ctx.Done()` alongside a
  `default:`; when a slot is free Go picks uniformly among ready cases, so an already-cancelled request
  can still acquire a slot and proceed. It reads as a cancellation check and is not one.
- **`healthClient` bypasses the adapter's transport entirely** (`ollama.go:258`). It is a package-level
  `http.Client`, so a user's proxy or TLS configuration does not apply to the liveness probe that gates
  P50.1 recovery. Harmless only because the probe targets loopback today.
- **`WithResponseHeaderTimeout` replaces `a.client` wholesale** in all three adapters, making option
  order significant the moment a second option touches the client — which **P61.1** would be.

Also recorded, deliberately not filed: the idle watchdog's `timer.Reset` races the `AfterFunc` firing
(`ollama.go:851-856`). `idleFired` latches and the consequence is only a sharper message on a stream
that delivered a line at the exact deadline, so it is a comment, not a bug.

Priority: Tier 2 — each is a small self-contained fix; the last one is a prerequisite hazard for P61.1.

## Open Work — Tier 3

**Status:** 1 open — **P61.6** (origin under Tier 1), which is sequence-dependent with the batch's
Tier-1 half by construction. **P59.9**, **P60.2** and **P60.4** all shipped 2026-08-05, closing the tier and
with it the P59.x batch's last Tier-3 item and the P60.x batch's two standalone ones — see
[releases.md](releases.md) for the write-ups. **P59.9** put a semaphore in front of local backends
(`provider.max_concurrent_requests`, auto-bounding a local server to one in-flight request since it
is one GPU, not a fleet); its default is a stated policy rather than the measured value the item
asked for, and remains the one loose end. **P60.2** gave the container sandbox a session lifetime
(`run -d` + `exec` + labelled, TTL-bounded, reaped containers), which **unblocks P60.3** in Tier 4 —
though P60.3 stays parked on its own merits. **P60.4** split the live-workflow task out of the test
that asserts on our SSE stream, so a second CLI agent can be run against the same task and model as
a control group. **P59.8 shipped 2026-08-05** — it superseded the "grammar-constrained
decoding" lead left unfiled by P53.6 below, which explicitly asked for its own `### P<n>.<m>` heading
if pursued; that paragraph is kept as its origin record, and the lead stays parked for *tool calls*,
which P59.8 deliberately did not touch. **P57.1** was filed 2026-08-03 off a dedicated P38.1 re-confirmation run (see the
Tier 2 section) and **shipped the same day** — a threat-modeling phased-drive robustness gap
unrelated to the P55.x batch; write-up in [releases.md](releases.md). Its live re-test is tracked
under P38.1, whose closure condition it now blocks nothing else in.
**P55.7** (`aegis-netscanner`, a second image split by mount posture) and **P55.8** (gosec's
two-phase warm/analyze split) shipped 2026-08-03, closing the P55.x container-only-scanning batch —
see the Tier 1 header for the batch's origin and [releases.md](releases.md) for the write-ups.

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
  two share no implementation, so this needs its own `### P<n>.<m>` heading if pursued. **Taken up
  2026-08-04 as P59.8**, narrowed to the one place it is unambiguously correct.


**P57.1 shipped 2026-08-03 (filed and built the same day).** Found during a dedicated P38.1 live
re-confirmation run (qwen3.6-fast-32k vs an external target; full account in the P38.1 write-up,
Tier 2 above). Everything mechanical behaved correctly — verify caught real cross-file defects,
correctly told mechanical ID-format issues (already canonical) apart from genuine content-authorship
gaps, and reopened only the owning content phase (P47.9). What had no guard was the reopened phase
getting stuck on a **theory of the data that was simply wrong**: the model convinced itself a
T0-vs-T01 zero-padding offset existed, re-derived it identically five turns running, the loop
detector's single corrective nudge did not break the cycle, and the engine aborted the whole drive.
A second invocation with a fresh context fixed every defect immediately — the evidence that the
context, not the model or the checks, was the defect.

Built: an engine loop abort now wraps `engine.ErrLoopDetected`, and the drive classifies it exactly
as it already classifies a context overflow and a tool-failure trip — terminal to the engine,
resumable at the phase level — with its own reset budget at all three engine-error sites (content
phases, the phase-6 loop, the P47.9 re-entry). The recovered retry additionally carries
`StuckLoopDirective`, which is the filed candidate direction: hand the model the verifier's
`file:line` report as the finding rather than inviting it to re-derive the mismatch, the same shift
`scaffold.py` (P38.4) made for structure. Write-up in [releases.md](releases.md). **The live re-test
against the same failure shape is still owed** and is tracked as part of P38.1's closure condition —
this is a fix aimed at a failure observed once.

### P61.6 — `sse` abstracts the pieces of a stream consumer but not its lifecycle

This is the root cause **P61.1**, **P61.2** and **P61.3** are three symptoms of, and the reason to
expect a fourth. `internal/provider/sse` already owns everything that is identical across adapters
*except the part that keeps breaking*: it supplies the HTTP client, the sized scanner, the
cancellation-aware `Emitter` and `HandleErrorResponse`, and then each adapter writes its own `consume`
loop. Those three loops independently re-derive the idle watchdog, the terminal-chunk requirement, the
`scanner.Err()` classification, the `bufio.ErrTooLong` naming and the final `EventDone` — and every one
of those five has now drifted, in the same direction, toward whichever adapter was in front of whoever
was fixing a local-model bug.

The fix is to lift the *skeleton* into `sse` — roughly `sse.Run(ctx, body, out, opts, decodeChunk)`,
owning the watchdog, the loop, the terminal-chunk requirement and the error classification — and leave
each adapter owning only per-chunk decode, which is the part that legitimately differs (NDJSON with
native tool calls, SSE with `[DONE]`, SSE with indexed content blocks). Anthropic's `dispatch`/
`handleData` split already has close to the right shape for the callback.

**Sequencing, which is the whole reason this is Tier 3 rather than Tier 1.** Doing P61.6 first makes
P61.1-P61.3 nearly free but puts a refactor of all three adapters ahead of three live bugs; doing the
three first means writing each fix twice and then deleting one copy. The recommendation is
**P61.2 first** (it is the silent-wrong-answer one and should not wait on a refactor), then P61.6,
then P61.1 and P61.3 as consequences of it — which also means the watchdog gets tested once, in `sse`,
rather than at one adapter's worth of coverage. Note that P61.5's last bullet
(`WithResponseHeaderTimeout` replacing `a.client`) should land before any of it.

A structural regression guard belongs with this: a test that enumerates the constructed adapters and
asserts each carries an idle bound, since the failure mode here is "the next adapter forgets", not
"an adapter is wrong".

Priority: Tier 3 — real value and it is the only fix that stops recurrence, but it is sequence-
dependent with three Tier-1 items and should not block them.

## Open Work — Tier 4

**Status:** 5 open, all measure-first, blocked, or explicitly parked; none has a build trigger yet.
**P61.7** was added 2026-08-05 by the P61.x batch (origin under Tier 1) and is measure-first: the
concern is real but its likelihood is unquantified, and the batch declined to guess.

**Two of this tier's measure-first items were measured and closed 2026-08-05: P59.10 and P52.16.**
Both had promotion triggers rather than build plans, both triggers fired, and both shipped — see the
Status section and [releases.md](releases.md). The tier-level lesson is worth keeping: in each case
the *measurement contradicted part of the filed item*, so building either one from its write-up alone
would have produced the wrong fix. P59.10's write-up assumed the prefill damage was bounded to the
tail and proposed retracting from the persisted transcript only; the damage was not tail-bounded, and
that fix would have traded the prefill cost for the P25.3 context leak. P52.16's write-up worried the
echo might hurt; it did not hurt, but it also did nothing for the two capable models, so shipping it
unconditionally would have taxed every round for a small-model-only benefit. Measure-first earned its
keep on both.

**P60.3** was added 2026-08-04 by the P60.x batch (origin under Tier 2). **P60.3 was unblocked 2026-08-05**
when P60.2 shipped: a session now owns a container there is something to snapshot. It stays Tier 4
on its own merits — the container backend is still not the default, and Orchard's version is roadmap
rather than shipped code, so only the idea transfers. **P55.9**
(relevance gating for the always-on scanners) and **P49.4** (LLM-summarized concept nodes) were
reviewed and dropped 2026-08-03 rather than left parked — see the Status section and the P49.x note
above for why.

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
prompt-eval per attempt on a slow local model. This is the AI-specific injection surface the review was
asked to look for, and it is the only one it found — prompt/system isolation, tool-schema handling and
PII logging all came back clean.

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

**Promote when:** a live run shows a cross-turn loop that per-`Run` detection missed.

**Precondition now met (2026-08-01).** P53.2 deliberately landed first: widening the scope of a
detector that mis-fired on polling and always aborted fatally would have multiplied both defects.
With the poll exemption and the nudge-once-then-abort outcome split shipped, a session-scoped
detector would now inherit a sounder mechanism — but the promotion trigger above is still the
observed cross-turn false negative, which has not happened.

Priority: Tier 4 — real but unproven, and the false-positive risk is higher than the current
detector's.

**Reviewed 2026-08-01: still correctly parked, no code change.** Confirmed against current code —
`newLoopDetector` is still constructed inside `Run` at every call site, unchanged since this item
was filed. No live run has reported the cross-turn loop this would catch, so the promotion trigger
has not fired. Not worth building speculatively: the design question it flags (what counts as a
legitimate repeat vs. a loop across turns) is real work, not a mechanical port of the per-`Run`
detector to a wider scope, and building it without a concrete false-negative in hand risks shipping
a detector tuned against a guess rather than an observed failure mode.

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
