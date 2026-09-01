# Aegis Release History — Part 4 (batch origins, refutation records, P52.x/P51.1/P50.x, P55.x scanning)

Start at [releases.md](../releases.md) for the index.

---

## Migrated from roadmap.md — batch origins, refutation records, and closed-batch status notes (2026-08-06)

Moved here 2026-08-06 during a roadmap cleanup, the second of its kind after 2026-08-01 and for the
same reason: roadmap.md holds only open work, per its own stated contract, and shipped-batch
narrative had accumulated in its Status and per-tier headers instead. No content was changed, only
relocated and grouped. Three kinds of record live here, and the middle one is the reason this
section is worth reading rather than archiving:

1. **Batch origins** — what each review actually read and what it concluded, including the parts it
   judged already correct.
2. **Refutation records** — candidate findings checked and deliberately *not* filed. These exist so
   a later review does not re-file them; check them before opening an item against
   `internal/provider`, `internal/ollamainfo`, `internal/repomap` or scanner method resolution.
3. **Closed-batch status notes** — the roadmap's running commentary on what shipped when, which is
   duplicate detail now that every item has its own write-up above.

---

### Batch origins

**P61.x — cross-adapter drift (filed 2026-08-05, 7 items + P61.8 filed 2026-08-06).** From a review
of the LLM-interaction surface (`internal/provider` and its three adapters, the decorator chain, and
`engine.turn`'s stream consumption) commissioned as a general Go/architecture/AppSec review. As with
P59.x it changed **no structural judgement** — the `Adapter` seam, the decorator chain and its
`Unwrap()` traversal, `sse.Emitter`'s cancellation-aware sends, and the absence of prompt/PII logging
are all right, and `go vet ./...` plus the provider and engine suites were clean. Four premises of
the review brief were checked and found already satisfied (a single LLM abstraction, `context`
propagation, request-body caps via `http.MaxBytesReader`, loopback validation of `server.addr` and
`provider.base_url` — FIND-08 and FIND-03), and are recorded so they are not re-filed.

What it found instead is a single theme, and it is the mirror image of P59.x's: **every piece of
stream hardening was fixed in the native Ollama adapter and never ported to the other two.** P59.2's
idle watchdog, P59.3's missing-completion-chunk check, P50.1's transport-error classification and
P59.1's `num_predict` clamp all existed exactly once, in `internal/provider/ollama`. The openai
adapter is not a cloud-only path — it is what serves Ollama's OpenAI-compat `/v1` endpoint, which
`docs/installation.md` still configures via `OPENAI_API_KEY="ollama"` — so each gap was live on a
local backend. The root cause was structural rather than three oversights: `internal/provider/sse`
abstracted the *pieces* of an SSE consumer (client, scanner, emitter, error-response) but not the
*stream lifecycle*, so there was no shared place for a lifecycle fix to land and each adapter's
`consume` had drifted independently. P61.1-P61.3 were the three live gaps (Tier 1), P61.6 the
structural fix that stops the next one (Tier 3), and the batch was filed with a sequencing note about
that dependency. **P61.2 shipped 2026-08-05**, the day it was filed and ahead of P61.6 as the
sequencing recommended, since it was the batch's only silent wrong answer. **P61.5, P61.6, P61.1,
P61.3 and P61.4 shipped 2026-08-06**, and **P61.8 the same day**, closing the batch except **P61.7**
(Tier 4, measure-first, untouched).

The sequencing bet paid better than the item predicted: building the structural fix *second* rather
than last turned P61.1 into option wiring and closed P61.3 with **no production code at all**, since
the classification it asked for is one line inside the lifted skeleton. Three members also corrected
the filed item in ways that changed what was built — P61.5's prescribed `errors.Join(rejection, err)`
argument order would have shipped a regression (`errors.As` stops at the first match, so the stale
terminal 400 would have made every retryable failure unretryable); P61.3's "no retry" claim and its
unmentioned second precondition both turned out to be wrong, the latter leaving recovery inert on the
`/v1` path until an openai liveness probe was added; and P61.8's single rule turned out to need *two*
predicates, one broad and one strict.

**P59.x — local-execution review (filed 2026-08-04, 10 items + P59.11).** An architecture and design
review of the harness as a *local-execution* system specifically: prompt/context management, the
Ollama request path, error handling and output parsing, read end-to-end across `internal/engine`,
`internal/provider/ollama`, `internal/provider/sse`, `internal/compaction`, `internal/tokenest`,
`internal/server` (`engine_build.go`, `contextwindow.go`, `messages.go`), `internal/guard` and
`internal/drive`, cross-checked against `docs/`. Most of the obvious local-model traps were already
closed and the seams are right — the review changed no structural judgement (the adapter seam,
capability-tagged tools, per-model context resolution and the phased drive are all the right answers
and none was touched).

What it found clusters on one theme none of the prior batches covered: **the context subsystem
reasons about the prompt, and nothing reasons about the generation.** `num_ctx` is prompt+completion
on Ollama, but `num_predict` rode through unclamped from a 32768 default (P59.1), the compaction
trigger was a flat 85% that predated any generation reserve (P59.1), no budget or timeout could fire
*during* a turn so a wedged runner had no valve (P59.2), and a stream that ended without a `done`
chunk was surfaced as a finished answer (P59.3). Tier 2 covered the accounting and detection edges
around that theme — token-budget semantics on unpriced backends (P59.4), the `small_model` guard
recommendation causing model thrash on VRAM-constrained hosts (P59.5), prose tool-call detection
gated on zero-call turns (P59.6), and the engine's immutable window versus the adapter's escalation
floor (P59.7). Tier 3 took up the grammar-constrained-decoding lead P53.6 left unfiled, narrowed to
the schema guard where it has no open design question (P59.8), and filed local-backend admission
control (P59.9). P59.10 (retraction versus the KV prefix cache) was Tier 4, measure-first.
P59.1-P59.3 shipped 2026-08-04, P59.4-P59.9 on 2026-08-05, P59.10 later the same day once its
measurement was taken, and P59.11 followed it. **The batch closed at 10 of 10 plus its follow-on.**

**P60.x — sandbox and eval (filed 2026-08-04, 4 items).** A read of Microsoft's
[Orchard](https://github.com/microsoft/Orchard) — an open agentic-modeling framework whose three
layers are domain recipes (Orchard-SWE/GUI/Claw), **Orchard Env** (a Kubernetes-native sandbox
service plus a Modal-style Python SDK spinning up ~1000 isolated containers on demand), and an RL
trainer on a vendored `slime` fork — assessed for what an RL-training substrate built on Kubernetes
has to offer a single-user local harness. Most of it does not transfer, and the non-adoption is the
more useful half of the record: the trainer, slime, Redis-backed multi-replica orchestration, Calico
NetworkPolicies, k8s itself and the in-pod HTTP agent are all answers to a scale question Aegis does
not have.

Four things survived that filter, each checked against the code rather than taken from the README:
per-sandbox resource limits (P60.1), a session-lifetime sandbox instead of a container per command
(P60.2), sandbox snapshot/branch as the basis for a truthful rewind (P60.3, Tier 4), and
harness-portable evaluation as a control group for the live-workflow tier (P60.4). The batch's centre
of gravity is that **`internal/sandbox`'s `Backend` was stateless per command** — container runs got
privilege hardening but no resource limits, every tool call was a fresh `docker run --rm` so nothing
an agent did to its environment survived the call that did it, and checkpoints therefore snapshotted
files only. P60.4 is separate: the live-workflow eval measures Aegis and the model fused together, so
a failure cannot distinguish a weak model from a scaffolding regression, and Orchard's one genuinely
portable idea is holding the environment fixed and swapping the harness for a baseline. **P60.1,
P60.2 and P60.4 shipped 2026-08-05**, leaving P60.3 — no longer blocked, since P60.2 gave a session a
container to snapshot, but still Tier 4 while the container backend is not a realistic default and
while Orchard's own version is roadmap rather than shipped code.

**P55.x — container-only scanning (filed 2026-08-02, 9 items, 8 built).** A full functional test of
the multiscanner container against a purpose-built multi-language vulnerable fixture, plus a review of
`internal/security`'s method resolution across all 17 registered scanners. The container's *scanning*
was sound — 14/14 bundled tools execute offline and detection is good. What the test found instead was
a cluster of **provisioning** failures, three sharing one shape: *the scanner silently or loudly
stopped working and no layer of the system noticed*. That shape was Tier 1 because
`internal/security/multiscanner.go` already names it as the thing this design most fears — "a silent
all-clear from a scanner that never looked at a database." Four defects found alongside (kubescape
fatal in container mode, kubescape's SARIF unparseable, njsscan broken by the semgrep removal, grype
absent from the pinned image) are the evidence base; two of the four survived a green `go test ./...`,
a successful image build, and a scan that reported findings.

The batch's strategic driver was a decision to make the container the **only** way Aegis scans, so a
user installs **one image instead of 17 tools** — and that goal is met. P55.1-P55.6 made the
*existing* container trustworthy and preferred; P55.7 and P55.8 extended containerization to the six
tools that had no container path at all, by recognizing that the split those tools needed was **mount
posture** rather than tool category (a second image with network on and no workspace, ever) and that
the one genuine exception — gosec, which needs both — is solved by the two-phase split `update-db`
already uses rather than by relaxing the hardening. Two tools stay host-only by explicit decision,
each with its reason stated in code: zap (already runs from its own official image) and dockle (needs
the container engine socket, a privilege axis that deserves its own decision — see the parked lead
still carried in roadmap.md's Tier 3).

**P53.x — local-LLM comparative review (filed 2026-08-01, 6 items).** A comparative review of how six
frontier harnesses — opencode, crush, pi, aider, OpenHands, goose — drive local models, cross-checked
line-by-line against Aegis. The review's headline is that Aegis is **ahead** on the four things that
dominate local-model bug trackers elsewhere: native `/api/chat` transport (opencode/crush/pi are all
on OpenAI-compat `/v1`, which structurally cannot carry `num_ctx` — the root cause of crush's flagship
"local models won't call tools" discussion #1828), `/api/show`-based proactive context detection
(`internal/ollamainfo`, vs goose's blind 128k default for unknown models), a bounded resident
`keep_alive` default (`providerfactory/factory.go:31`), and P25.6's `LocalProfile` tool-schema
deferral (goose #6883 reports Qwen3-coder silently degrading to XML-in-content past ~5 tools). The
batch closed at 6 of 6 on 2026-08-01/02, P53.6 (the non-native tool-calling shim) last and highest
value.

**P58.x — daily-copilot review (filed and shipped 2026-08-04, 2 items).** The first time this project
explicitly evaluated Aegis as an everyday assistant for research, documentation and code analysis
rather than as a security/threat-modeling task runner; a search of `research/` confirmed the framing
was genuinely new, not a revisited decision. Most of the review's answer was that the capability is
already there and already good: `deep-research` is a real structured-research workflow, the
report/diagram skills produce real deliverables, sessions are daemon-backed with a picker and no
git-repo requirement, and `workspacetrust` gates *project config* rather than plain chat, so a general
question asked outside any repo has no friction. Two gaps were real, both narrow, and both shipped the
same day. Neither opened follow-on work.

---

### Refutation records — candidate findings checked and deliberately not filed

**From the P59.x review (2026-08-04), two refuted by code reading.** **SSE backpressure**: `emit` is
called synchronously per token from the stream goroutine and looked like it could stall a turn behind
a slow consumer — P21.5 already solved it (`messages.go:132-147`, `newSSEWriter` drops oldest on a
full buffer rather than blocking the run). **Native tool-result correlation for parallel same-tool
calls**: real, and already filed as **P52.16** — this review independently reached the same mitigation
(prefixing each result with a compact echo of the originating call). The dedicated A/B was run
2026-08-05 and P52.16 shipped: the conflation is real on a small model and absent on capable ones, so
the echo is applied only to rounds that call the same tool more than once. **Note for future
reviews:** the first A/B scored a false 10/10 in *both* arms because the fixture's file bodies named
their own paths — a content-attribution shortcut that never exercises the missing wire metadata.

**From the P53.x review (2026-08-01), five refuted by direct code reading.** Proactive `num_ctx`
(`ollamainfo.go:135-162`), config-driven `keep_alive` (`factory.go:44,215`), ID-based tool-result
correlation (`ollama.go:605` — the wire-level name-only limitation was separately tracked as P52.16),
`<think>`-before-tool-parse ordering (structurally impossible to break on the native path — reasoning
and tool calls arrive in separate NDJSON fields, `ollama.go:590-600`), and tool-set shrinking for weak
models (P25.6, `builtin.go:170-180`).

**From the P61.x review (2026-08-05), four brief premises found already satisfied** — listed in the
batch origin above; they are review-brief premises rather than candidate findings, but they belong to
the same "do not re-file" set.

---

### Closed and dropped leads

**P54.2 — SCA/secrets scanner exit codes, closed 2026-08-02, no gap found.** The "accurate refusal,
error-shaped" exit-code question for the SCA/secrets scanners (P34.6 checked only the *language*-
targeted tools) was answered by running all six at their pinned versions against an empty tree and a
docs/C/shell tree with no dependency manifests. trivy, grype and syft exit 0 with valid output;
gitleaks forces `--exit-code 0` and its report file is read independently of the run's error;
trufflehog exits 0 with empty stdout, which is the success branch. **osv-scanner's exit 128 is the
only refusal of this shape, and it was already interpreted by P34.12** — so osv-scanner is to the
SCA/secrets half what brakeman was to the language-targeted half: the only one. No gate added; the
measurements are recorded in the `runJSON` doc comment (`internal/security/scanners.go`) so the sweep
isn't re-run from scratch. Full write-up above.

**P55.9 — relevance gating for the always-on scanners, dropped 2026-08-03.** P54.2 had already shown
no correctness gap exists (the dependency scanners exit 0 with valid empty output on a manifest-free
tree), so this was a pure latency optimization with no measured complaint behind it, and its own
write-up noted a naive manifest check risks trading a slow-correct scan for a fast-wrong one. Dropped
rather than parked: not worth tracking speculatively.

**P49.4 — LLM-summarized concept nodes, dropped 2026-08-03.** graft's second pass has an LLM summarize
files into ~20–50 plain-English "concept nodes" with typed links; the analog here would have been an
opt-in `aegis index --semantic` pass. Dropped rather than parked because it carries two unresolved
problems at once rather than one: it costs an LLM pass per file (real latency/token cost,
cache-invalidation surface) where every other P49 item is deterministic and free, and it overlaps
`internal/knowledge`/`internal/memory`, which already carry project-level prose context — so before it
could even be a measure-first candidate it needs a decision on whether it's a new store or belongs in
one of those. **Re-file only if** the deterministic structural tiers (P49.1–P49.3) demonstrably fail
to close the re-discovery gap *and* that store question has an answer.

**The Tier-4 lesson from P59.10 and P52.16, measured and closed 2026-08-05.** Both were measure-first
items whose promotion triggers fired, and in each case **the measurement contradicted part of the
filed item**, so building either from its write-up alone would have produced the wrong fix. P59.10's
write-up assumed the prefill damage was bounded to the tail and proposed retracting from the persisted
transcript only; the damage was not tail-bounded (51x — 3604ms of prefill against 71ms unretracted,
indistinguishable from a cold reprocess, because the zero-tool nudge is injected as early in a run as
it is possible to be), and that fix would have traded the prefill cost for the P25.3 context leak.
P52.16's write-up worried the echo might hurt; it did not hurt, but it also did nothing for the two
capable models (qwen2.5-coder:1.5b 32/40 → 38/40 with the echo; qwen3:14b and gemma4:12b at ceiling
either way), so shipping it unconditionally would have taxed every round for a small-model-only
benefit. **The P59.9 loose end closed the same day** and is the one that did *not* become a behavior
change: the local default of 1 is right, but for latency (~40% more throughput at ~70% worse turn
latency), not for the correctness hazard it was justified by — four concurrent 12k-token requests were
not truncated at all.

---

### P38.1 re-test log, 2026-07-21 through 2026-07-27

Relocated from the open P38.1 item, which keeps its mechanism/conformance summary, the current
blocker (the 2026-08-03 run) and its reproduce/closure condition. Every fix these runs root-caused has
shipped; the value here is the record of *how the blocker moved* — each stall landed further from the
harness and closer to raw model throughput.

- **2026-07-21, qwen3:14b / qwen3.6:35b / gpt-oss:20b:** the ~9K-token SKILL.md preload re-sent every
  turn starved the fill of context before the model could `edit_file` (root cause → shipped **P39.5**);
  the autonomous verify pass missed structural defects a mechanical check should catch (→ **P39.6**);
  models stalled announcing work instead of doing it (→ **P39.7**); a broken LLM summarizer looped
  silently (→ **P39.8**); the `/v1` compat path could overflow un-warned (→ **P39.9**, native-adapter
  half exonerated). All shipped.
- **2026-07-23, gpt-oss:20b vs AiGateway:** with P39.5-P39.9 in place, the drive died *before* model
  capability was even tested, on two `chat --skill`-CLI bugs: skill scripts materialized only under the
  data dir, outside the sandboxed workspace root, so the model couldn't reach `recon.py` (**P39.10**);
  and the drive's PENDING-marker oracle walked the materialized skeleton templates themselves, so it
  could never reach zero (**P39.11**). Both shipped and verified live end-to-end, with regression
  tests. With the scripts reachable, gpt-oss:20b itself then failed to converge from small-model
  path/argument brittleness: mangled script paths, drifting to a typo'd run-dir (`.aegit`) mid-build so
  its fills landed outside the real suite, calls to a non-existent `search` tool, and the wrong
  `--framework` flag.
- **2026-07-24, qwen3.6:35b-a3b-fast vs FirewallRuleAnalyzer:** harness and model-competence questions
  cleared — the drive ran recon → scaffold → fill, held the run-dir path across every `edit_file` (the
  gpt-oss:20b mangling did not recur), produced grounded file:line-cited content, and its DFD passed
  `lint_dfd.py` 5/5. What blocked closure was throughput/write robustness, not orchestration: a
  5-minute response-header timeout that a 2845-line file read could blow past at ~7 tok/s (**P39.12**),
  unbounded whole-file reads ballooning cumulative session input to 3.47M tokens (**P39.13**), a
  monolithic ~5,700-token single-file write that truncated into a malformed tool call (**P39.14**), and
  mechanical verify catching structural errors but not substance like a Tier-2 threat with a Tier-1
  prerequisite (**P39.15**). All four shipped 2026-07-24 with regression tests.
- **2026-07-24, in-harness phased drive (P38.8's mechanism, brought inside `chat --skill`):** the root
  cause the P39.x fixes kept circling was structural — the drive ran the *whole* six-phase build in
  **one ever-growing conversation** (`internal/cli/chat.go`), so even with pruning the peak context
  climbed until a local window stalled. The parked P38.8 wrapper never hit that because it runs a
  **fresh, skill-free context per phase**. That per-phase reset was implemented *inside* the built-in
  path: architecture → DFD → framework-analysis → findings → assessment each in its **own fresh
  conversation** seeded with a compact phase prompt (prior phases grounded from disk, not from history),
  then the phase-6 verify+quality round in its own context too. All existing guards reused (the PENDING
  oracle, the P39.7 no-progress "act now" nudge, `--max-turns`, the P39.6 verify loop, the P38.1 quality
  pass) — only the context lifetime changed. Originally `internal/cli/chat_phased.go`; lifted into
  `internal/drive` by P52.12 so every client reaches it.
- **2026-07-24, qwen3.6:35b-a3b-fast vs FirewallRuleAnalyzer (phased drive, stability):** the phased
  drive **reached a verify-clean suite** — 23 threats / 22 findings across 9 components, all
  `verify.py`/`lint_dfd.py`/`inventory.py --check` passing, content grounded in real file:line evidence
  and its own quality pass catching genuine inaccuracies — i.e. the mechanism/conformance closure
  condition was **met**. But it took **three manual re-invocations**: the CLI `chat --skill` drive
  engine wired no proactive compaction, so each phase's context grew — the model re-reading files and
  recomputing STRIDE counts by hand — until Ollama hard-rejected the request and the drive aborted on a
  terminal `NewContextTruncationError` rather than a resumable stop. Root-caused into the **P47.x
  phased-drive stability batch** (P47.1-P47.6, all shipped): single-invocation stability was the bar,
  distinct from the mechanism closure already demonstrated.
- **2026-07-27, qwen3.6:35b-a3b-fast vs FirewallRiskRater (hollow-report checks + self-heal, validated;
  phase-6 gap found):** first live run of the ec0127c hollow-report checks + afd6764 self-heal, against
  a resumed suite whose `<!-- PENDING -->` markers were already deleted but whose finding bodies were
  empty. **Confirmed working:** self-heal auto-refreshed the stale project `verify.py` on launch, the
  three new checks turned the previously false-passing hollow suite (`11 passed, 0 failed` on the old
  verifier) into `12 passed, 2 failed` with exact file:line, and the drive fixed the
  `no-duplicate-header-rows` failure live. **New gap found and fixed:** the phase-6 verify/quality
  remediation loop lacked the overflow-reset and anti-monolithic-write guardrails the content phases
  carry — fixed by **P47.7**/**P47.8**, with **P47.9** (route hollow-body failures to the owning content
  phase) as the Tier-3 follow-up, shipped 2026-07-30.

---

### Closed-batch status notes

The roadmap's running commentary, kept for the dates and for the two or three judgements that are not
restated in the per-item write-ups above.

**P59.11 (2026-08-05)** was a direct follow-on rather than a filed item: P59.10 had fixed the zero-tool
nudge's 51x prefill regression and left the **tool-failure nudge at a measured 25.9x**, because that
one was only *bounded* to one per run rather than genuinely spent, so retracting it early would have
removed a correction whose failures could recur. P59.11 supplies the missing observation instead of
assuming it — the failure streak actually clearing — and pairs early retraction with
**re-injectability**, so a relapse earns a fresh nudge by append. The one-per-run behavior that
mattered (never nag on consecutive failing rounds) is preserved by an outstanding-nudge gate rather
than by the count. No tier held an item for it, since P59.10 had recorded it as a deliberate non-fix.

**P59.9, P60.2 and P60.4 (2026-08-05)** closed Tier 3 entirely, and their shared shape is worth
keeping: not a fact that failed to reach the component needing it, but a **policy nobody owned** for
something the system was doing anyway. Nothing bounded how many requests reached one local model
server, so every concurrent request was built believing it owned the whole GPU (P59.9). Nothing owned a
sandboxed container's lifetime, so it had none, and no state survived the tool call that created it
(P60.2). And nothing separated the harness from the model when a live run failed (P60.4).

**P59.7, P60.1 and P59.8 (2026-08-05, earlier the same day)** share a shape rather than a subsystem: in
each, something the system already knew was not reaching the component whose behavior depended on it —
the adapter's escalated context window never reached the engine's compaction trigger (P59.7), the
operator's intent to bound a sandboxed command had nowhere to be expressed at all (P60.1), and the
schema guard's requirement was expressible to the backend ahead of generation and wasn't being
expressed (P59.8). P59.8's one implicit design question — *which* turn gets constrained — was answered
"only the schema-guard corrective retry, with tools suppressed": a first turn is where the model does
the work, and a grammar forcing a JSON object out of it forbids exactly that. P59.7 connected the
escalated window to the engine's compaction *trigger* only — the summarizer's own budget stays pinned
at the sized window, which was already a deliberate choice and is now labelled as one rather than
reading like the same oversight.

**P59.4, P59.5 and P59.6 (2026-08-05)** all take a mechanism built for a cloud provider and ask what it
means on one local GPU: a token budget that answered a billing question when the local user was asking
a work question (P59.4 — resolved with a second, separately-named `cost.max_generated_tokens_per_run`
rather than by splitting one key's meaning across provider classes, which is **neither** of the two
fixes the item proposed), a documentation recommendation that re-introduced the model-eviction churn
`keep_alive` exists to prevent (P59.5 — the guard now runs on the resident model locally, with a new
explicit `output_guard.model` for operators who want the split anyway), and a pair of prose-tool-call
checks gated on zero-call turns, which made the commonest partial-protocol shape invisible (P59.6 — a
mixed round is now declined and corrected, the safer of the two readings the item named).

**P61.4 and P61.5 (2026-08-06)** were the P61.x batch's Tier-2 half. P61.4 took **both** halves of the
either/or its item posed rather than choosing between them, because they cover different populations —
the compat-path `max_tokens` clamp only fires when a window was actually resolved, and `aegis doctor`'s
generation-budget row is what covers every case where one was not. That second half turned out to be a
**repair rather than an addition**: the check had existed since P59.1 and was silently useless for
exactly the configuration it was built for, because `doctorServedWindow` trusted
`provider.context_window` as "what the adapter sends as num_ctx" — true natively, false on `/v1`, which
never sends it. **P61.8 (2026-08-06)** is the daemon-side counterpart of that same blind spot, filed
off P61.4 and shipped the same day.

**P52.x (filed 2026-07-30)** closed 15 of its 17 items between 2026-07-30 and 2026-08-01 — P52.15
(wall-clock run budget) shipped and P52.17 (auto tool-calling probe on model switch) closed as
already-implemented were the last two, leaving P52.14 and P52.16; **P52.16 shipped 2026-08-05** once its
A/B was run, so P52.14 is the batch's last one standing. Also shipped or closed across the same window:
**P51.1** (macOS seatbelt profile), the **P50.x** phased-drive determinism batch, the **P49.x** repo-map
batch head (P49.1/P49.2 — P49.3 stays Tier 4, P49.4 was dropped, above), the entire **P47.x**
phased-drive stability batch, **P48.1** (config-test hermeticity), and **P38.8** (external per-phase
threat-model wrapper — superseded once its mechanism shipped in-harness for P38.1).

**The 2026-08-01 cleanup note**, kept because it is the precedent this one follows. roadmap.md had
drifted from its own stated contract — full SHIPPED/CLOSED write-ups for the P52.x/P51.1/P50.x/P47.6/
P47.10 items had accumulated there instead of here. Moved out; no content changed, only relocated.
While auditing, one stale note was caught and corrected: a "content-substance check routing" follow-up
mentioned alongside the P52.7/P52.8 write-ups (never given its own `P<n>.<m>` number) was still being
described as outstanding. It isn't — file-aware routing (`perFile`, `fileOwnerPhase`) shipped as part of
P52.12's move into `internal/drive` (`internal/drive/drive.go:837-878`); there was no separate gap to
track.

---

## Migrated from roadmap.md — P52.x full-stack review batch, P51.1, P50.x (shipped/closed items)

Moved here 2026-08-01 during a roadmap cleanup so roadmap.md holds only open work, per its own
stated contract. No content changed from the roadmap write-ups; see the batch summary at the top
of this file (P52.15/P52.17) for the closing status.

### P52.1 — Context window is detected for the global model, not the model the turn actually runs on — SHIPPED 2026-07-31

Shipped with **P52.4**, as the item required. Built as specified: a per-model `ctxWinEntry{win, src,
final}` cache behind `ctxWinMu`, each entry carrying its own re-detect state, with
`applyDetectedWindowFor` reconciling config-vs-served per entry and `maybeRefreshContextWindowFor`
refreshing the model the finished run actually used. Two decisions beyond the spec, both documented in
code: the **globally-configured model's entry stays in the existing `ctxWin`/`ctxWinSrc`/`ctxWinFinal`
fields** rather than moving into the map, because those are what `/status` reports and what the
daemon-wide summarizer is tuned to — a reading for one session's persona-pinned model must not
redefine what every other session compacts against; and **first-use detection for an unseen model is
synchronous** (5s bound), because seeding from the global window and correcting after the run is
precisely the failure being fixed — it would leave the pinned model's *first* turn, the one carrying
the full system prompt, believing it had the primary's headroom. The output guard gets its own model's
window too. See [releases.md](../releases.md). The original analysis follows.

`internal/server/contextwindow.go` resolves **one server-wide** effective context window, detected
against `s.cfg.Provider.Model` (`initContextWindow` at `:52`, `maybeRefreshContextWindow` at `:117`),
and `newEngine` hands that single number to every run: `ctxWin, _ := s.effectiveContextWindow()` →
`ContextWindowTokens: ctxWin` (`engine_build.go:274`, `:288`). But the model a turn actually runs on
is resolved **per turn**: `resolveModel` (`engine_build.go:54`) layers session `/model` override >
persona config override > the persona file's own `model:` > global, and `turnModel` can additionally
route a turn to `provider.small_model`. So the window the engine enforces and the model that has to
live inside it can be two different models.

Both directions are wrong, and one is the failure this whole subsystem was written to prevent:

- **Persona pins a larger-context model** → the engine compacts at 85% of a window smaller than the
  real one, burning summarizer calls (and on a local model, minutes) on a conversation that had room.
- **Persona pins — or task routing selects — a smaller-context model** → the engine believes it has
  headroom, never compacts, and Ollama silently drops the oldest tokens **including the system
  prompt**. That is precisely the silent-truncation failure `ollamainfo` exists to catch
  (`ollamainfo.go:1-8`), reintroduced through the per-session model path that postdates it.

Fix: key detection by model rather than by server. A small `map[string]ollamainfo.Result` cache
behind `ctxWinMu` (each entry carrying its own `Authoritative()`/`ctxWinFinal` state, since a model
not yet loaded still needs the re-detect-after-first-run path), resolved in `newEngine` *after*
`turnModel` has picked the model, with the existing config-vs-served reconciliation in
`applyDetectedWindow` applied per entry. `maybeRefreshContextWindow` then refreshes the entry for the
model the finished run used, not `cfg.Provider.Model`. Pairs with **P52.4**, which fixes the adapter
half (the `num_ctx` actually requested); do them together — fixing only one leaves the request and
the enforcement disagreeing in the other direction.

**Priority:** Tier 1 — a live correctness gap that silently degrades any session using a
persona-pinned model or small-model routing, with no diagnostic (the model just quietly forgets its
instructions). Contained to `contextwindow.go` + one call site; no dependency.


### P52.2 — `latex_build` escapes workspace confinement (arbitrary host file read into a PDF) — SHIPPED 2026-07-30

**Correction, found while building this (2026-07-30).** The fix prescribed below is a **no-op on
TeX Live 2026**. This item asserted that `openin_any=p` is "honoured by TeX itself, so this holds
regardless of the host's `texmf.cnf`". That is no longer true: TL2026's
`texmf-dist/web2c/texmf.cnf` documents `openin_any` as having **no effect** — `kpse_in_name_ok` and
related functions always return true — because "there were obscure ways to inject arbitrary input
from the supposedly-forbidden areas, so it gave a false sense of security"
([tex-live thread, Dec 2025](https://tug.org/pipermail/tex-live/2025-December/051965.html)). So the
host's `openin_any = a` is upstream's new default with the semantics deleted, not a
misconfiguration. Verified empirically: with `openin_any=p openout_any=p shell_escape=f` and
`-no-shell-escape`, an `\input` of an absolute out-of-workspace path is still opened and its text
still reaches the PDF content stream. The three-line fix alone would have shipped as security
theatre and failed its own regression test.

**What shipped instead:** the process hardening below (still effective on TeX Live ≤2025 and
MiKTeX, and `-no-shell-escape`/`openout_any` are real everywhere) **plus** a pre-compile static scan
of the `.tex` and its transitive in-workspace includes for file references resolving outside the
root, validated through `sandbox.ValidatePath`. See [releases.md](../releases.md) for the covered
directives and the exclusion handling. **Residual gap, deliberately left open:** the scan is a
heuristic on a hardened process, not a sandbox — filenames constructed from macros at run time
(`\input{\somemacro}`) cannot be resolved statically and are allowed. The durable fix is running the
compiler under `internal/sandbox`; filed as the Tier-3 lead below rather than taken as a drive-by
change, since P51.1 had just finished proving the seatbelt profile executed nothing at all on macOS
26. The original analysis follows.

`internal/tool/builtin/latex.go:100-108` builds the compiler invocation as:

```go
flags := []string{"-interaction=nonstopmode", "-halt-on-error", "-output-directory=" + outDir}
```

No `-no-shell-escape`, and no environment hardening. Verified against the live TeX config on the dev
host (`kpsewhich --var-value=...`): `shell_escape = p` (restricted — a whitelist of `\write18`
commands is permitted) and, critically, **`openin_any = a` — TeX may read any file on the host.**

So a `.tex` file *the model itself authors* can `\input{~/.ssh/id_rsa}` or `\InputIfFileExists` any
path on the machine and embed the contents in the output PDF. Every other file-touching builtin
routes through `sandbox.ValidatePath` (`builtin.go:224`), which is symlink-aware and correct;
`latex_build` resolves only the **`.tex` path itself** through it and then hands the whole filesystem
to a subprocess. The tool is `CapExecute` so it is permission-gated, but the confinement asymmetry is
real — and it matters more now that document authoring is a first-class workflow (see **P52.10**,
**P52.11**), where the source material (third-party `.sty` files, templates, research artifacts) is
not necessarily the user's own.

Fix, cheap and with no functional downside:

```go
flags = append([]string{"-no-shell-escape"}, flags...)
cmd.Env = append(os.Environ(), "openin_any=p", "openout_any=p")
```

`openin_any=p` (paranoid) restricts reads to the current tree and TEXMF — exactly what a
workspace-confined build wants — and `-no-shell-escape` closes the restricted-`\write18` whitelist.
Add a regression test that a `.tex` containing `\input` of an absolute out-of-workspace path fails to
embed it. ~~Note the env vars are honoured by TeX itself, so this holds regardless of the host's
`texmf.cnf`.~~ **← false as of TeX Live 2026; see the correction at the top of this item.**

**Priority:** Tier 1 — a currently-exploitable confinement escape in shipped code, and the fix is
three lines plus a test. No dependency. *(Shipped 2026-07-30 — the fix was not three lines; see the
correction above.)*


### P51.1 — The macOS seatbelt profile runs no commands at all — SHIPPED 2026-07-30

Found 2026-07-30 while running the full suite: `TestOSBackendConfinesWrites` and
`TestOSBackendConfinesWritesToSessionWorkdir` fail on macOS 26.5.2 with `signal: abort trap` on a
write *inside* the workspace. Reproducing the generated profile by hand showed this is not a test
artifact — **`sandbox: os` runs nothing on macOS 26**: `/bin/sh` takes SIGABRT during exec, with no
diagnostic beyond the signal. The cause is the P27.18 read confinement in `seatbeltProfile`:
`(deny file-read*)` also denies a read of the **root directory itself**, and resolving any absolute
path walks `/`, so exec of `/bin/sh` dies before the shell starts. Two adjacent gaps came out of the
same bisect: `/tmp`, `/etc` and `/var` are symlinks into `/private/*` and seatbelt checks the read
against the **symlink** before following it, so allow-listing only the `/private/*` target leaves
`cat /etc/hosts` and `> /tmp/x` failing with EPERM; and `/bin/sh` reads `/private/var/select/sh` to
pick its shell personality, printing an `Error opening ...` line on every command. Fix: five
built-in read allowances in `seatbeltProfile` — `(literal "/")`, the three symlink aliases as
**literals** (a `(subpath "/")` would hand back the whole filesystem), and
`(subpath "/private/var/select")`. They are deliberately not routed through `defaultOSReadPaths`,
which is shared with bwrap and renders every entry as a `(subpath ...)`. Confinement is unchanged
and re-verified: `$HOME`, `~/.ssh`, `/private/var/db` and writes through `/etc` all stay denied;
`(literal "/")` discloses the root directory's entry names only.

**Priority:** Tier 1 — a shipped sandbox backend that executes nothing, and the failure is silent
(SIGABRT, no message). Contained to one function; no dependency.


### P50.1 — Backend liveness + resumable reset (a dead model server must not silently kill the drive) — SHIPPED 2026-07-30

The 2026-07-30 FirewallRiskRater run's real stall was **Ollama dying mid-phase** — not a logic bug.
The drive had nothing to fall back on: `provider.WithRetry` only retries a **synchronous** `Stream`
failure before any tokens stream (`retry.go`), so a mid-stream `{"error":"model runner has
unexpectedly stopped"}` (classified retryable by `classifyStreamError`, but surfaced as an
`EventError`, past the retry seam) or a connection-refused outage that outlasts the ~4 capped
backoffs ends the engine `Run` with a terminal error, and `runPhasedSkillDrive` returns it as fatal
— the whole `aegis chat` process exits with a half-built phase and no resume. The phased drive
*already* has the recovery primitive for this: the P47.2 / P47.7 fresh-context reset, which resumes
any phase from its on-disk `<!-- PENDING -->` files. This item classifies a **backend-unreachable /
runner-died** error the same way it classifies a context overflow — resumable — and adds a bounded
**wait-for-recovery** step: poll a new adapter liveness probe (`/api/version` on Ollama) with
backoff until the server answers again (or a total budget expires), print a clear "backend
unreachable — waiting to resume from disk" notice, then reset the phase context and continue.
Best-effort auto-restart of `ollama serve` is gated behind an opt-in (`AEGIS_OLLAMA_AUTOSTART=1`);
the default is wait-and-resume, which is safe and reversible. Mechanism: a new optional adapter
capability `provider.HealthChecker` (mirrors `ContextWindowRaiser` — reached via an unwrapping
`provider.CheckBackendHealth` helper), a `provider.IsBackendUnavailableError` classifier (transport
refused/reset + the `retryableStreamSignals` infra class), and a `waitForBackend` loop the content
phases and phase-6 share, alongside the existing overflow handling. Follow-up: the Ollama adapter's
*mid-stream transport read failure* (connection reset / unexpected EOF — the server dying while tokens
stream, the common case on a long per-turn stream) was still emitted as a bare error the classifier
could not see; it is now wrapped as a transport `APIError` like the synchronous `doChat` path.

**Priority:** Tier 1 — a real robustness gap that silently discards hours of work; small, contained
to the drive + the Ollama adapter, no dependency.

---


### P52.3 — Consecutive-tool-failure circuit breaker (the loop the loop detector cannot see) — SHIPPED 2026-07-31

Shipped as specified (`internal/engine/toolfailure.go`), with **one deliberate deviation** and **one
cross-lane fix the item did not anticipate**.

**Deviation:** the item reads as if both counters feed both thresholds. Only the **strict**
`allErrorRounds` counter can end a run; the secondary same-error counter earns a nudge and nothing
more. A round that mixes a repeating failure with a *succeeding* call is the ordinary edit → `go test`
→ still-fails → edit cycle, where the shell tool reports a non-zero exit as `IsError` with identical
text every round — killing a run that is actively writing files would be a far worse failure than the
stall it prevents. Pinned by a test.

**Cross-lane fix, found in the reconcile pass:** the abort would have been a *regression* for the
phased drive, which treats any engine error that is not backend-down or a context overflow as fatal —
so a stall that used to burn to `maxIterations` and limp onward would have killed an unattended run,
re-introducing exactly the manual-re-invocation failure P47.x/P50.x exist to remove. The abort now
wraps an exported `engine.ErrToolFailureLimit` sentinel, and `chat_phased.go` classifies it as a
**resumable phase reset** at all three `eng.Run` sites (content phase, phase-6 loop, P47.9 hollow
re-entry) — a fresh context is the right remedy, not merely a compatible one, since the breaker fires
when a model is reasoning from a context dense with its own failed attempts. Unlike the overflow path
it does **not** escalate the serving window and it keeps its own reset budget
(`maxToolFailureResets = 2`). See [releases.md](../releases.md). The original analysis follows.

`IsError` is computed for every tool result and emitted on the event stream (`engine.go:1276-1278`,
`:1327`, `:1332`) and then **never aggregated into anything** — no counter, no threshold, no nudge,
no abort. The engine has a rich set of stall guards (P28.3 zero-tool, P34.1 empty-answer, P34.2
tool-call-as-text, P2.6 step-limit summary, the P39.8 summarizer latch) and none of them fire on
repeated *failing* tool calls.

The gap is structural, not incidental. `loopDetector` matches a repeating **signature** of tool
name + canonicalized input (`loopdetect.go:39-72`), with period 1..4. `canonicalizeToolInput`
correctly neutralizes nonces and timestamps so an incidental varying byte can't defeat it. But the
common small-model failure is a model whose arguments *legitimately differ every turn*: call
`edit_file`, get `old_string not found`, retry with a slightly different `old_string`, fail again,
repeat. Every signature is genuinely distinct, so the detector never fires, and the run burns all the
way to `maxIterations` (default 40) producing nothing. On a ~7 tok/s local model that is potentially
hours. None of the three existing budgets catch it either: `BudgetUSD` is an explicit no-op for
unpriced local usage (`engine.go:541-550`), `MaxTokensPerRun` defaults to 0, and `maxIterations` is
the thing being burned.

Fix: track, per `Run`, the number of **consecutive tool rounds in which every tool result was an
error** (and, secondarily, the count of consecutive identical *error strings* regardless of input,
which catches the same-error-different-args shape directly). At threshold 3, inject a corrective
nudge in the existing `nudgeState` idiom — quoting the actual error text and instructing the model to
re-read the file/re-inspect state before retrying, rather than re-guessing arguments. At threshold 6,
abort with a message naming the repeated error. The nudge must be registered in `nudgeState` so
`retractAll` strips it from the durable transcript like every other corrective (`engine.go:785-795`).

This **promotes the existing Tier-3 "task-failure halt" lead** (filed with P46.3), which identified
the same gap from the `codex-build` angle — that lead noted it would need "a persisted task boundary
to count against". It does not: the per-`Run` tool round is a perfectly good boundary for the failure
shape that actually occurs, and a persisted task boundary can layer on later if `structured-build`
ever needs it. Treat that lead as closed by this item.

**Priority:** Tier 2 — ~30 lines in an established idiom, no dependency, and it closes the single
most common local-model stall the current guard set misses. Highest-value Tier-2 item in the batch.


### P52.4 — Per-request `num_ctx` (stop a small-model turn allocating the primary model's KV cache) — SHIPPED 2026-07-31

Shipped alongside **P52.1**, as the item required. `provider.Request` gained `NumCtx`; the Ollama
adapter's value is now the fallback, so nothing changes for any non-Ollama caller. **The engine was
deliberately not touched** — instead of teaching `engine.Options` about `num_ctx`, the server wraps
its shared adapter per run with a new `provider.WithNumCtx` decorator, following the `Unwrap()
Adapter` convention the retry and failover decorators already use. **P52.12 should reuse that seam.**
One decision the item did not cover: `RaiseContextWindow` escalations are applied as a monotonic
**floor** over both the request and adapter values, not overridden by the request — an escalation
responds to an overflow that already happened, while a request's `NumCtx` was computed *before* the
run, so letting the request win would silently undo an escalation on a daemon-shared adapter. Inert
today (the only caller is the single-model CLI drive), correct once P52.12 lands. See
[releases.md](../releases.md). The original analysis follows.

`s.adapter` is a **single shared adapter** built once at daemon start and used by every run
(`engine_build.go:276`). The native Ollama adapter carries `num_ctx` as **adapter state**
(`ollama/ollama.go:36`, set via `WithNumCtx`) and stamps it onto every request
(`doChat`, `:342-345`). The model, by contrast, is per-request (`provider.Request.Model`).

So when `turnModel` routes a turn to `provider.small_model`, Ollama is asked to serve that small
model with the **primary** model's `num_ctx`. On VRAM-constrained hardware that either forces an
oversized KV allocation for a model that doesn't need it, or evicts the primary model to make room —
producing exactly the cold-reload churn between turns that `load_duration` telemetry was added to
make visible (`ollama.go:554`). The same applies to a persona-pinned model.

Fix: move `num_ctx` from adapter state to a per-`Request` field. `wireOptions.NumCtx` is already
populated per request, so the wire path needs no change — only the *source* of the value moves from
`a.numCtx` to `req.NumCtx`, with the adapter's value kept as the fallback when the request doesn't
specify one (preserving today's behavior for every non-Ollama caller). `newEngine` then sets it from
the same per-model resolution **P52.1** introduces, so the window requested and the window enforced
come from one place and cannot disagree.

Build immediately after **P52.1** — they are two halves of one correctness story, and shipping either
alone leaves the request and the enforcement inconsistent in the opposite direction. Note this also
removes the mutability that makes **P52.6** necessary on the `Stream` path, though `RaiseContextWindow`
still needs its own treatment for the escalation path.

**Priority:** Tier 2 — contained to the Ollama adapter + `provider.Request` + one call site; no
dependency beyond P52.1, which it should ship alongside.


### P52.5 — Latch the `think`-rejection verdict (a wasted 400 round trip on every single turn) — SHIPPED 2026-07-30

Shipped as specified, with the `sync.Map` keyed on `req.Model` the item called "the honest shape".
One behaviour change beyond the spec: the warning now fires only after a *successful* think-omitted
retry, so a retry that also fails surfaces the raw error instead of a misleading "retried without
it". See [releases.md](../releases.md).

`ollama.go:291-309` handles the P38.5 case where a model 400s the instant `think` is sent at all
("does not support thinking") by retrying once with the field omitted. The retry is correct and the
warning is right. But `a.think` is **never updated**, so the adapter re-sends `think` on the next
request, 400s again, warns again, and retries again — **for every turn of the entire session.**

On a cloud provider that is a wasted round trip. On a local server it is worse: the failed request
still reaches Ollama, and the warning fires on every turn, burying real signal in the log. A
40-iteration run pays 40 pointless 400s.

Fix: after a *successful* retry with `think` omitted, latch the adapter's `think` to nil so
subsequent requests skip the doomed first attempt, and emit the warning only on the first occurrence.
Because `Stream` can be entered concurrently by multiple sessions against the shared daemon adapter,
the latch needs synchronization — an `atomic.Bool` alongside the existing `*bool` is enough (read it
in `doChat`, set it once in the retry path), and it composes with **P52.6** rather than duplicating
it. Keep the latch per-adapter, not per-model: a daemon serving two models where only one rejects
`think` would mis-latch, so gate the latch on `req.Model` — a small `sync.Map[string]bool` keyed by
model is the honest shape.

**Priority:** Tier 2 — small and self-contained, removes a per-turn cost and a per-turn log line on
exactly the models most likely to be used locally.


### P52.6 — Synchronize `RaiseContextWindow` before the daemon can call it — SHIPPED 2026-07-30

Shipped ahead of **P52.12** as the sequencing note below required. `numCtx` is now behind an
`RWMutex`; a new `-race` test (32 concurrent escalations against 32 concurrent `Stream` calls)
reproduces the race verbatim against pre-fix code and is clean after. See
[releases.md](../releases.md).

`ollama.go:82-94` mutates `a.numCtx` with no synchronization. Its doc comment is honest about this —
*"Not safe for concurrent use with Stream — the phased drive only calls it between turns, after a
Stream error has returned and before the next Run"* — and that invariant holds **today**, because the
only caller is `internal/cli/chat.go:435`, a single-session CLI process.

It stops holding the moment **P52.12** lifts the phased drive into the daemon, where `s.adapter` is
shared across every concurrent session (`engine_build.go:276`). At that point one session escalating
its context window is an unsynchronized write racing every other session's `Stream` read of the same
field — a genuine data race, and one that `go test -race` will not catch because no existing test
drives the daemon and the escalation path together.

Fix: guard `numCtx` with a mutex (or make it atomic), in both `RaiseContextWindow` and the `doChat`
read. Land this **before** P52.12 rather than as part of it, so the structural change doesn't have to
carry a concurrency fix as well. If **P52.4** ships first, the field largely stops being read on the
hot path — but the escalation path still writes it, so this item stands either way.

**Priority:** Tier 2 — a few lines, no behavior change today, and it removes a latent race that
P52.12 would otherwise introduce silently. Sequence it before P52.12.


### P52.7 — Extend the hollow-body check to all seven suite files (not just `3-findings.md`) — SHIPPED 2026-07-30

**Deviation from the spec below, deliberate:** this item said to *generalize check 12* into a
suite-wide check. Instead check 12 keeps its name and a new check 15 (`section-bodies-nonempty`) was
added, because `internal/cli/chat_phased.go`'s `contentSubstanceChecks` routes on the **literal
string** `finding-bodies-nonempty` to send hollow findings back through the findings phase (P47.9) —
renaming it would have silently dropped that routing. The two never overlap: check 12 owns
model-authored `####` subsections inside `### FIND-##` blocks (never scaffolded, so never in the
manifest); check 15 owns scaffolded headings suite-wide. The manifest ships as
`.scaffold-manifest.json` in the run directory. **Follow-up worth filing:** extend
`contentSubstanceChecks` so a `section-bodies-nonempty` failure routes to the phase owning the named
file, rather than falling through to the generic verify-fix turn. See [releases.md](../releases.md).

`verify.py`'s `check_finding_bodies_nonempty` (`:695`) states the failure mode precisely in its own
docstring: *"A weak model can delete the `<!-- PENDING -->` marker without writing anything in its
place, leaving a heading over empty space — structurally intact but substantively blank, which no
other check notices."* That check shipped with P47.9 and its live value is proven — it is what turned
a false-passing hollow suite into `12 passed, 2 failed` on the 2026-07-27 FirewallRiskRater run.

**It is scoped to `3-findings.md` alone.** The same failure is equally available in
`0.1-architecture.md`, `1-model.md`, `2-<framework>-analysis.md`, and `0-assessment.md`, and none of
them are checked. An empty Deployment Classification, an empty Security Infrastructure Inventory, an
empty PASTA stage, or an empty Executive Summary all pass `verify.py` clean today. Some are caught
indirectly by the count/bijection checks; the **prose** sections are not caught at all — and the
architecture file's prose is what every later phase's tiering depends on.

Fix: generalize check 12 into a suite-wide `check_section_bodies_nonempty`. The clean way is to have
`scaffold.py` — which already knows every marker key it wrote — emit a small manifest (a sidecar, or
a deterministic re-derivation from the skeletons) that `verify.py` asserts against. That converts the
current property, *"no PENDING marker remains"*, into the property actually wanted: *"every site that
had a PENDING marker now has substance."* Keep the existing exclusions (a lone HTML comment, a `---`
rule, a bare table separator are not content) and keep the division of labour with check 1 so an
unfilled marker is reported once, not twice.

**Priority:** Tier 2 — mechanical Python in an existing idiom, and it closes a proven-real check gap
across five files. Prerequisite for **P52.8**, which reuses the same manifest.


### P52.8 — Mechanical substance floor for threat-model content (anti-`TBD`) — SHIPPED 2026-07-31

Shipped as four new checks — 16 `evidence-cells-cited`, 17 `no-placeholder-cells`, 18
`none-identified-fraction`, 19 `prose-sections-substantive` — consuming P52.7's
`.scaffold-manifest.json` directly and reusing `find_heading`/`section_region`/`region_substance`
as-is. `scaffold.py` needed no change: the manifest was already built as a superset for this. Checks
1-15 and their names are untouched, for the P52.7 reason (`chat_phased.go` routes on the literal
string). Every threshold lives in one module-level `SUBSTANCE` dict with a matching CLI flag.

**Calibration deliberately under-flags**, as the item required: the `None identified` cap is 0.95 so
nothing below 100% fires; placeholder matching is exact, never substring; `Anchor` is not an evidence
column, `Prerequisite`/`Description`/`Configuration` are not substance-checked, and Deployment
Classification is exempt from the prose floor. Verified against fixtures both ways — a legitimate
suite gets `19 passed, 0 failed`, a vacuous one that passes checks 1-15 (this item's premise,
reproduced exactly and asserted as a test) fails 4, and all seven freshly-scaffolded frameworks add
zero new failures on an unfilled scaffold.

**Worth recording for anyone touching these scripts: they had no automated coverage at all before
this.** No Python test existed in the repo; the Go side only stubs `verify.py`
(`chat_verify_test.go`) or checks it materializes byte-identically (`embedded_test.go`). The new
`_verify_substance_test.py` closes that, and its **leading underscore is load-bearing** —
`//go:embed builtin` is a plain directory pattern, which excludes `_*`, so the test is tracked source
that never ships inside the skill. See [releases.md](../releases.md). The original analysis follows.

Nothing in the 14 `verify.py` checks rejects vacuous content. A suite in which every threat's Evidence
cell reads `see code`, every Mitigation reads `TBD`, and every category is `None identified` passes
all 14 checks and gets stamped. The P38.1 quality pass is the intended backstop — but it is an LLM
call and, per **P52.12**, it is CLI-only, so the TUI path has **no substance gate whatsoever**.

A mechanical floor catches the worst of it for near-zero cost and, unlike the quality pass, cannot
itself regress the suite (the problem P50.3 had to solve):

- reject an Evidence cell that is a bare filename with no line number, symbol, or config key — the
  skill's §3 already *requires* the citation, nothing checks it;
- reject placeholder tokens (`TBD`, `TODO`, `N/A`, `See above`, `see code`) in cells the skeleton
  marks as required-substantive;
- cap the fraction of `None identified` cells per framework table — one or two is a legitimate,
  complete entry (the skill explicitly says so); twelve out of twelve means the pass never happened;
- require a minimum prose length for the narrative sections **P52.7**'s manifest identifies.

Every threshold must be tunable and each failure must name file:line like the existing checks. Bias
toward under-flagging: a false failure costs a verify bounce and erodes trust in the whole check
suite, which is worth more than catching every marginal cell.

**Priority:** Tier 2 — Python only, no Go changes, and it is the only substance gate the TUI path
would have. Depends on **P52.7** for the section manifest — **shipped 2026-07-30, so this is now
unblocked.** The manifest (`.scaffold-manifest.json`, run directory) is deliberately a superset of
what check 15 needs: `kind: "table"` + `columns` locate every scaffolded table by its real column
names (enough to require a line number/symbol/config key in an Evidence cell, reject `TBD`/`N/A`/`see
code` per named column, and cap the `None identified` fraction per table), `kind: "prose"` entries
are exactly the narrative sections a minimum-length floor applies to, and `heading`/`level`/`to_eof`
give the exact region. `find_heading` / `section_region` / `region_substance` in `verify.py` are
reusable as-is. `manifest_version` is present for a schema bump and `Suite.manifest()` ignores
unknown keys, so adding fields is backward compatible in both directions.


### P52.9 — A `yaml_validate` tool (YAML is a deliverable and nothing checks it) — SHIPPED 2026-07-30

Shipped as specified, registered **deferred**. One documented limitation: `go.yaml.in/yaml/v3` never
exposes the problem mark's column for a parse failure (`parser.fail` emits only `line N`), so the
tool reports the true line plus a `>`-marked source excerpt and says plainly that no column is
available, rather than inventing one that would misdirect on indentation bugs. See
[releases.md](../releases.md).

Aegis has **no YAML tooling at all** — `internal/tool/builtin/security.go` is the only builtin that
even mentions yaml. Yet YAML is a first-class output in two shipped workflows: `inventory.yaml` is one
of the threat-model suite's seven files, and the documentation-as-code skill (**P52.11**) drives a
`slides` template family whose entire deliverable is a `.yaml` file.

Today the model edits both as opaque text with `edit_file`. A broken indent is invisible until a
downstream consumer fails — `inventory.py --check` for the sidecar, a deck renderer for slides — and
the resulting error usually names a symptom far from the cause. On a slow local model, localizing
that costs several turns, which is exactly the budget the P47.x/P50.x work exists to protect.

Fix: a `yaml_validate` tool, `CapRead`, that parses the file and returns either the parse error with
line/column or a compact key outline on success. **`go.yaml.in/yaml/v3` is already a direct
dependency** (`go.mod:25`), so this adds no new dependency and no new failure mode. Roughly 60 lines
in the shape of the existing small builtins. Worth also emitting the top-level key list on success —
that turns the tool into a cheap structural probe the model can use *before* editing, not only after.

**Priority:** Tier 2 — small, zero new deps, and it pays into both the threat-model flow and the
document-authoring flow. Sequence before or alongside P52.11's first real use.


### P52.10 — `latex_build` can never resolve citations (the biblatex preamble is decorative) — SHIPPED 2026-07-31

Shipped as **option 2 only — `latexmk` was evaluated and rejected**, against this item's stated first
preference. The rc-file objection is answerable (`-norc` suppresses the arbitrary-Perl `./latexmkrc`
evaluation). The decisive objection is *where the confinement check has to sit*: latexmk decides for
itself, mid-run, when to invoke biber over the `.bcf` it just generated, and exposes no seam between
those two events — its only interposition point is the `$biber` command string, so honouring the check
would mean shipping a separate wrapper executable that re-implements it out of process. If the Tier-3
sandbox lead ever lands, revisit: under real process confinement the objection disappears.

The confinement warning in this item was correct and load-bearing. A new `checkLatexBibConfinement`
runs **after pass 1 and before the bib binary is looked up**, parsing the `.bcf`'s datasources or the
`.aux`'s `\bibdata`/`\bibstyle`, following `\@input` chains into nested `.aux` files, and validating
every name through `sandbox.ValidatePath` against both directories the tool could resolve it from;
remote `scheme://` datasources are refused outright. `biber` also gets `--noconf`, since its first
config location is `biber.conf` in the *model-writable* cwd. The P52.2 traversal was factored into a
shared `latexWalkSources` so the scan and bib auto-detection cannot drift. Both smaller defects were
folded in. **Residual gaps, deliberately open:** no iteration to convergence (the `runs` cap went 3 →
4 and LaTeX's own `Rerun to get cross-references right` warning surfaces in the report); the bib scan
is static, on an unconfined process, so a TOCTOU swap of the `.bcf` between scan and exec is not
modelled; and a workspace-local `.bst` is path-validated but not otherwise sandboxed. See
[releases.md](../releases.md). The original analysis follows.

`latex_new_document` scaffolds a `biblatex`/`biber` block into every generated preamble
(`latex.go:476-478`, and again in the body at `:568-569`, both commented out for the user to enable).
But `latex_build` only ever runs the LaTeX compiler in a plain multi-pass loop (`:112-125`) — there is
**no `biber`/`bibtex` invocation anywhere in the tool**. So a user who uncomments the biblatex block,
adds `references.bib`, and builds gets a PDF with unresolved `[?]` citation marks and no indication
why. For security research writing, which is citation-heavy, that makes the bibliography support
purely decorative.

Fix, in order of preference:

1. **Prefer `latexmk` when it is on PATH.** It solves the compile/bib/index fixpoint correctly —
   including the case where a citation added on pass 2 needs a third pass — and would replace the
   hand-rolled `runs` loop entirely. Keep the existing loop as the fallback.
2. **Otherwise, run `biber` (or `bibtex`) between passes** when the source contains
   `\addbibresource`/`\bibliography`, then force at least two subsequent LaTeX passes. Auto-detecting
   from the source is better than a flag the model has to remember, but expose a `bib` boolean too so
   it can be forced or suppressed.

Two smaller defects in the same function worth folding in: the multi-pass loop keeps `runErr` from a
failed pass 1+ while `lastLog` reflects only the final pass, so a mid-sequence failure can be reported
against the wrong log; and `parseLatexLog`'s warning cap (`:176`, `:197-202`) compares
`len(s.warnings) == 15` after the `… and N more` line may already have been appended, which is fragile
if the cap is ever changed. Both are minor but cheap to fix while the function is open.

Must be built **after P52.2** — adding external tool invocations to this path while it still runs
unconfined widens the same hole. **P52.2 shipped 2026-07-30, but read its correction before starting
this:** the confinement it delivered is a *static scan of the LaTeX source*, not process
confinement (`openin_any` is inert on TeX Live 2026). The scan checks `\addbibresource` /
`\bibliography` **arguments** — the source-level half — but `biber` resolves resources declared in
the generated `.bcf`, and neither `biber` nor `bibtex` is covered by it at all. So this item adds
**two subprocesses outside the current confinement**. Either extend the scan to the `.bcf`/`.aux`
resource lists before invoking them, or treat it as a reason to prefer the sandbox lead below.

**Priority:** Tier 2 — contained to one tool, and it converts a shipped-but-nonfunctional feature into
a working one. Depends on P52.2 for ordering (now shipped, with the caveat above).


### P52.11 — `documentation-as-code` built-in skill — SHIPPED 2026-07-30

Aegis had no awareness of a Documentation-as-Code toolchain: nothing in `internal/` or `docs/`
referenced `docforge.py`, the `_templates/` families, or `md2report.py`. The gap mattered because the
generic `latex_new_document` preamble, good as it is, cannot know an organization's house style,
metadata defaults, or build wiring — so a model asked for a formal document either hand-authored a
LaTeX preamble that looked wrong next to every other document the organization publishes, or
approximated the house style from whatever it happened to see.

Shipped as a dormant built-in skill (`internal/skills/builtin/documentation-as-code/SKILL.md`,
enabled via `aegis skills enable documentation-as-code`), covering: locating the toolchain and reading
`.docforge_config.json` for defaults rather than re-specifying them; the four `--type` families
(`report`/`process`/`runbook`/`slides`) and when each applies; the two routes in — `--from-md`
(preferred: Aegis drafts Markdown, the toolchain converts, so no LaTeX is ever hand-authored, and it
is `report`-only) versus scaffold-then-fill-one-section-per-`edit_file`; a mandatory `--dry-run`
first, with the two failure modes it prevents (hard failure on an existing `<dest>/<name>`, and a
wrong `--dest`) called out so a collision doesn't turn into a retry loop; the `Makefile` target set
(`all`/`diagrams`/`pdf`/`quick`/`clean`/`distclean`) preferred over a raw compiler call; the 17 slide
`type:` values with an explicit "do not invent a type" rule and the leading-space bullet-nesting trap;
and diagram authoring via `assets/*.mmd`.

**Confidentiality boundary (§0 of the skill), the design constraint that shaped it:** a DaC repository
is organization-owned and its templates carry branding — logos, image assets, colour palettes,
reference documents, classification banners, team names, and example documents about real internal
systems. The skill therefore describes **mechanism only** and never reproduces template content. It
explicitly forbids copying branding into anything the model authors, hard-coding metadata defaults
(they are read from `.docforge_config.json` at run time), treating the repo's `education/`/`examples/`
/`research/` directories as content sources rather than structural references, and relocating branded
documents out of the repository. When no DaC repo is in play it routes to `latex-report` instead,
because an unbranded document is the correct output there. The shipped file was scanned to confirm
zero employer-identifying content; `TestBuiltinsListsEmbeddedSkills` was extended to cover it.

**Follow-ups, not blockers:** a `/docs` or `/report dac` TUI entry point (mirroring how `/report latex`
reaches `latex-report` at `tui/slash.go:1022`) once the skill has real use; and bundling an
`analyze_sources.py`-style structural pre-pass if source assembly turns out to be the slow step.

**Priority:** Tier 2 — shipped; no Go changes were required beyond the embed and the test.


### P50.2 — Deterministic ID canonicalizer (`normalize_ids.py`) — scripted renumber, not LLM-authored — SHIPPED 2026-07-30

Both the invented-`T#.<cat>`-suffix verify bounce and the quality-pass duplicate-`FIND-07`
regression share one root cause: the **LLM authoring and renumbering identifiers by hand**. The P37
scripts *check* IDs (`verify.py`'s `check_threat_coverage_bijection`, `check_finding_ids_sequential`,
`check_coverage_matches_related_threats`) but nothing *canonicalizes* them, so every fix is a
model turn that can drift or truncate. This item adds a bundled `normalize_ids.py` (sibling to
`inventory.py`) that mechanically rewrites the suite into canonical form: strip any invented
`T<n>.<suffix>` back to the bare `T<n>` the analysis file defines, renumber `FIND-##` to a gapless
`FIND-01..FIND-NN` sequence in document order, and rewrite **every** cross-reference in lockstep —
the coverage table's Threat-ID/Finding-ID columns and each finding's `Related Threats` line — so the
two symmetric locations can never disagree. It is idempotent (a canonical suite is a no-op) and
diff-only unless it finds something to fix. Wired as a deterministic pre-verify pass in the phase-6
loop (run before `verify.py`, so a drift is normalized away instead of bounced back to the model)
and named in the findings-phase + quality prompts as the tool to use for any renumber instead of
hand-editing. Also settles the Tier-3 "threat-ID form" doc lead by making the bare `T<n>` form
canonical in code.

**Priority:** Tier 2 — cheap, self-contained Python + a small Go wiring hook; removes an entire
class of verify bounces and the quality-pass regression's root cause. Depends on nothing; P50.3
builds on it.


### P50.3 — Quality-pass regression guard (snapshot + rollback; never ship worse than clean) — SHIPPED 2026-07-30

The P38.1 quality pass edited a **mechanically-clean** suite into a broken one (duplicate `FIND-07`)
and was saved only by luck of the round-2 recheck ordering. The pass must not be able to regress the
suite it was handed. This item snapshots the suite fingerprint **and file contents** at the moment
the mechanical checks first go clean (immediately before the quality pass), then after the pass
re-runs `normalize_ids.py` (P50.2) + the mechanical checks: if the suite still verifies clean, stamp
and finish as today; if it does not and the bounded fix rounds can't heal it, **roll back to the
pre-pass snapshot** — which is known-clean — and stamp that, rather than shipping a regressed suite
or stopping with a broken one. Pairs with constraining the quality prompt away from bulk renumber
(defer that to P50.2's script) and treating a step-limit-truncated quality turn as a resumable reset
(the same P47.7 machinery), so a large re-tier can't be left half-applied.

**Priority:** Tier 2 — small, contained to `runPhasedVerifyAndQuality` + a snapshot helper; converts
the quality pass from "can regress" to "can only improve or no-op". Depends on P50.2.


### P50.4 — Live per-turn progress heartbeat (make a hung/dead phase observable) — SHIPPED 2026-07-30

`audit.jsonl` is not flushed live and the phased drive logs only at phase boundaries, so a phase
that hangs (or a backend that died — see P50.1) is invisible until the whole run ends: the only
live signal today is watching Ollama's token counter. This item emits a structured per-turn
heartbeat — phase name, turn index within the phase, elapsed, and remaining PENDING count — at each
in-phase iteration and on a periodic timer during a long single turn, and flushes the audit sink so
an external supervisor (or a human tail) can detect a stall. It is the observability precondition
that makes P50.1's wait-for-recovery and any future supervisor actionable.

**Priority:** Tier 2 — small logging/plumbing change, no behavior risk; multiplies the value of
P50.1.


### P52.13 — `workspace.additional_roots` (unblock the cross-repo research→document workflow) — SHIPPED 2026-08-01

**Shipped 2026-08-01** — as specified, read-only by default, with per-root `aegis trust` (new
`--dir` flag) on top of the P27.1 untrusted-project freeze. The confinement check runs per root
against each root's own resolved identity rather than once against a covering prefix, so two roots
under a shared parent never make that parent reachable. Full write-up in
[releases.md](../releases.md). The original item follows.

There is **no multi-root support** — confirmed by search: no `AdditionalRoots`/`allowed_roots`
concept exists anywhere in `internal/`. Every workspace-confined tool resolves through
`effectiveRoot` (`builtin.go:234`) → `sandbox.ValidatePath` (`pathvalidator.go:18`) against exactly
one session workdir.

That makes the natural document-authoring shape inexpressible: *read research artifacts from repo A,
write a formal document into repo B*. Today the only workarounds are to run Aegis from a common
parent directory — which works but inflates the repo map (the Aegis map alone is already 436KB) and
widens confinement far past what the task needs — or to shuttle files by hand.

Fix: a `workspace.additional_roots` config list (project- and user-level, following the existing
config layering). `ValidatePath` gains a variant that accepts a root **set**: a path validates if it
resolves, symlinks and all, inside *any* configured root, and the existing single-root behavior is the
degenerate case. The symlink-escape check must run per candidate root, not once against a merged
prefix, or a symlink from root A into root B's parent would validate incorrectly.

Two design points to settle when building: whether additional roots are read-only by default (likely
yes — the common case is "read research from A, write to B", and making A read-only is a cheap,
meaningful restriction), and how they interact with `workspacetrust` (an additional root should
require its own trust decision, not inherit the primary root's).

**Priority:** Tier 3 — larger than a Tier-2 item because it touches path validation, config, and
trust, but self-contained and unblocking. Build before **P52.12**; the two are independent.


### P52.12 — Lift the phased drive into the daemon (every client gets the local-model machinery) — SHIPPED 2026-08-01, supersedes P50.5

**Shipped 2026-08-01** — all four parts, plus two defects found in the wiring: the completion oracle
had not been generalized with the plan (`LatestRunDir` is threat-model-specific, so a declared plan
resolved `""` forever and every phase burned its full turn budget), and the TUI's resumable-drive
cancel had to stop the run daemon-side or interrupt would have become a no-op. Full write-up in
[releases.md](../releases.md). The original item follows.

**This supersedes P50.5**, which framed the problem as "wire the phased drive into the TUI
`/threat-model`". The full-stack review showed the scope is wider and the framing should change: the
issue is not TUI parity, it is that **every reliability mechanism built for local models is
unreachable from every client except one CLI subcommand.**

Everything in `internal/cli/chat_phased.go` (984 lines) — fresh context per phase, the P47.9
hollow-body re-entry router (`:813`), P50.1 backend liveness + resume-from-disk
(`chat_phased_health.go`), P47.5b context escalation (`:198`), and the P39.7 no-progress guard
(`chat.go:553`) — is reachable only through `aegis chat --skill`. `phasePlanFor` (`chat_phased.go:76`)
hard-codes a single skill name and is called only from `chat.go:108` and `:415`. The TUI's own help
text states the split outright (`tui/commands.go:194`): `/threat-model` is "interactive by design",
and unattended builds require dropping to the CLI. **The web UI has no equivalent at all** — it is a
chat surface over the daemon, with no drive of any kind.

So TUI and web users run the single-context drive that the phased drive exists *because* it fails —
the P38.1 wall. That is the wrong default for the clients most people actually use, and it is
especially wrong for the web UI, which is where a multi-hour build most wants to live (it survives
terminal closure, which `aegis chat` does not).

Shape of the work:

1. Move `skillPhase`/`phaseParams`/`phasePlanFor` out of `internal/cli` into a neutral package
   (`internal/drive`, or `internal/skills` if the phase plan becomes skill metadata). Nothing in the
   phase machinery is CLI-specific — it is orchestration *above* `engine.Run`, and the engine, gate,
   tool registry, and event plumbing are already shared.
2. **Generalize `phasePlanFor` to read the phase plan from the skill's own frontmatter** rather than
   hard-coding `"threat-modeling"`. That lets `deep-research`, `latex-report`, `structured-build`, and
   the new `documentation-as-code` (**P52.11**) opt in without a code change, and it removes the
   awkwardness of a general mechanism keyed to one skill name.
3. Expose it as a daemon endpoint (`POST /sessions/{id}/drive {skill, task}`) streaming over the
   existing SSE seam, so no new transport is needed. The P50.4 heartbeat is already the right progress
   signal for a UI to render.
4. Give the TUI `/threat-model` an explicit unattended mode, and the web UI a drive control. Keep
   interactive-by-default for `/threat-model` — P47.10's reasoning that interactive review between
   phases is *valuable* still stands; it is the *absence of a choice* that is the defect.

**~~Land P52.6 first.~~ Done — P52.6 shipped 2026-07-30.** `s.adapter` is shared across concurrent
sessions (`engine_build.go:276`) and the drive calls `RaiseContextWindow`, which *was*
unsynchronized; this item would have turned that latent race live. `numCtx` is now mutex-guarded, so
this item no longer has to carry a concurrency fix.

**Priority:** Tier 3 — the highest-value item in the batch by impact, and the largest by surface
(session/SSE seam, config, two clients). Not speculative: the trigger already exists in every P38.1
live run that had to be driven from the CLI. Sequence after **P52.6**, and after **P52.13** if both
are in flight.

**Superseded — P50.5 (2026-07-30):** "Wire the phased drive into the TUI `/threat-model`". Folded
into P52.12 above, which covers the same work plus the web UI and the skill-frontmatter
generalization. P47.10's original defer-as-documentation decision is thereby revisited and overturned:
the review supplies the concrete need it was waiting on.

**Lead — run the LaTeX compiler under `internal/sandbox` (surfaced shipping P52.2, 2026-07-30):**
P52.2 closed the arbitrary-host-read escape with a static scan of the LaTeX source, because the
environment hardening the item prescribed turned out to be inert on TeX Live 2026. A scan cannot be
complete: TeX can build a filename from macros at run time (`\input{\somemacro}`), and that is
unresolvable statically and allowed by design. The durable fix is executing the compiler through
`internal/sandbox` like any other subprocess, which also covers **P52.10**'s `biber`/`bibtex` passes
for free. Not filed as an item yet because the sandbox backends need a look first — P51.1 found the
macOS seatbelt profile was executing *nothing at all*, so "just run it in the sandbox" is not the
cheap change it sounds like, and the residual it closes is awkward to exploit (the attacker must
already control the `.tex` the model authors). File it properly when someone touches the sandbox
backends or P52.10 comes up.

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

**Lead — task-failure halt (surfaced filing P46.3) — PROMOTED to P52.3 (2026-07-30):** `codex-build`
also halts entirely and presents the current diff if a task fails 3 times, rather than retrying or
silently rewriting. Aegis's `loopDetector` (`internal/engine/loopdetect.go`) only catches literal
repeated tool-call signatures, and `BudgetUSD`/`MaxTokensPerRun` only catch session-wide cost/token
exhaustion — neither tracks "this specific task has failed N times". This lead deferred the work
pending "a persisted task boundary to count against"; the P52.x review concluded that boundary is not
required — the per-`Run` tool round is a sufficient counting unit for the failure shape that actually
occurs on local models. **Now filed as Tier-2 `P52.3`**; treat this lead as closed. The diff/summary
artifact-on-stop half of the original idea is *not* carried into P52.3 and remains unclaimed — file it
separately if `structured-build` ever needs it.

---


### P53.6 — Non-native tool-calling shim: act on the signal Aegis was already detecting — SHIPPED 2026-08-02

Aegis detected the exact condition a fallback would handle and then threw the signal away. The P34.2
check in `engine.go` spots a model writing `{"name": ..., "arguments": ...}` into its prose instead of
emitting a tool call — the `qwen2.5-coder:1.5b` signature, where a model whose Ollama manifest claims
tool support cannot speak the protocol and then fabricates the results it never fetched — and the
comment was explicit that warning was all it would do: *"Name it once; never block."* The daemon's
model-switch gate emitted a notice and proceeded; `aegis doctor` was diagnostic. A repo-wide search
found no toolshim, no prompt-based tool-call parsing, no JSON repair. Warn-only was the right first
move (a prose-only session with such a model is still legitimate, and blocking would have been worse
than nothing), but it left the 14-27B class — exactly the class the P39.x re-test history keeps
fighting — unable to do anything at all.

**Shipped.** New `internal/toolshim` plus engine wiring, behind `provider.tool_call_shim` (`"off"`
default, `"on"`). With the shim on, `turn()` sends **no** native tool schemas; it appends the rendered
tool catalog and a format contract to the request's system prompt, and the model calls a tool by
writing `<tool_call>{"name": …, "arguments": {…}}</tool_call>`, which `toolshim.Parse` turns back into
ordinary `provider.ToolUseBlock` values before the zero-tool-calls branch is ever reached. Everything
downstream — the loop detector, the budget gates, `runTools`, the tool-failure breaker — sees a normal
tool round and needs no knowledge of how the calls arrived.

**Three design rules, each load-bearing.**

(1) **Opt-in and explicit, never auto-engaged.** A shim that quietly starts turning prose into
executable tool calls is a security surface, not a convenience, so nothing switches it on: not a
failed probe, not a low P53.4 conformance rate. `"auto"` is *rejected* rather than silently accepted,
because accepting the word today would ship a value that does nothing; the daemon warns at startup on
any unrecognized value, since "treated as off" is otherwise indistinguishable from "never set".
Auto-engagement off the persisted rate (`modelcaps.Store.ToolCalling`) stays the follow-up the item
sequenced it as — worth taking only once that rate is trustworthy.

(2) **No shortcut past the gate.** Parsed calls are `ToolUseBlock`s and nothing else, dispatched
through the same `runTools` path as native ones — same permission gate, capability check, hooks,
workspace confinement. By dispatch time the engine cannot tell them apart, which is the point:
there is deliberately no second execution path to audit. Test-pinned with a denying gate that must
see exactly one check and let nothing execute.

(3) **Decline, never repair.** goose documents its own parse failures (markdown where JSON was
requested, malformed JSON, inconsistent shapes) and has an open request for JSON repair
(block/goose#6688). A parser that repairs is one that can fabricate a call the model never made, with
real side effects behind it. So `Parse` is strict: an unterminated tag, two JSON values in one tag,
trailing prose, an unknown tool name, or non-object arguments fails the **whole turn's** calls — not
just the bad one, because a model that lost the shape once is no longer a trustworthy source for the
rest. The single tolerated deviation is a markdown fence *inside* the tags: chat-tuned models fence
JSON by reflex, and unwrapping a fence cannot change what the call says. OpenAI's string-encoded
`arguments` is unwrapped once and must itself contain an object; `parameters` is accepted as an alias.
A failed attempt earns a corrective naming the specific reason, bounded at 2 per run
(`shimFormatNudgeMax`) and retracted from the durable transcript afterwards like every other nudge
family — leaving malformed tool calls in the transcript would teach a later turn that they belong
there.

**The two non-obvious integration points.** A shimmed assistant message holds only *text* — the calls
were parsed out of it, never emitted as `tool_use` blocks — so appending `tool_result` blocks would
leave them orphaned and get the conversation rejected by the provider. `toolshim.RenderResults`
formats the round as tagged text instead, labelled with each call's tool name (there is no
`tool_use_id` to correlate on, so a three-call round would otherwise come back as three anonymous
blobs). And the shim's catalog rides every request but lives *outside* `conv.System` — appended in
`turn()` so compaction, session persistence, and checkpoints see a clean transcript — which means the
proactive-compaction check would undercount the real prompt by exactly what the shim adds, the wrong
direction for a check whose job is to compact before a local server silently truncates. Estimated
once per run and added to the fill check.

**Also:** suppressed on the P2.6 step-limit summary turn (that turn asks for prose and no tools;
serving a catalog and then parsing a call out of the requested summary would defeat it), and wired
into all four engine construction sites — daemon sessions, in-process spawns, `aegis chat --skill`,
and the `aegis worker` subprocess backend, because a shimmed parent whose teammates weren't shimmed
would hand every spawned agent a model that can't act. `aegis doctor`'s zero-tool-calls verdict — the
one verdict the shim is actually for, as opposed to the inconsistent-rate verdict it would not help —
now names it as the fix.

**Deliberately not folded in:** grammar-constrained/schema-constrained decoding (Ollama structured
outputs, llama.cpp GBNF). It attacks the opposite end of the problem — making malformed tool-call JSON
mechanically impossible rather than parsed-and-declined — and targets models that *do* speak the
protocol but malform arguments (the P35.2 failure class). None of the six reviewed harnesses does it.
File separately if pursued; the two share no implementation.

**Prior art, synthesized not copied.** goose's `GOOSE_TOOLSHIM` uses a *second* model
(mistral-nemo 12-14b) to interpret replies back into structured calls — reported to bring phi4-14b and
gemma3-27b to roughly llama3.3-70b-native parity. OpenHands' `NonNativeToolCallingMixin` is the
lighter variant: schemas in the prompt, regex parse, per-model native-vs-non-native from a registry.
Shipped here is the lighter shape — no second model, no extra inference cost, no second thing that can
be wrong — with a stricter parser than either. opencode, crush, and pi ship nothing here, so this is
net-new capability rather than catching up.

Tested: 12 `internal/toolshim` tests (mode parsing incl. rejected `"auto"`, prompt rendering + schema
compaction, six accepted call shapes, three no-attempt cases that must read as final answers — bare
call-shaped JSON without tags among them — nine declined-attempt cases each named by reason,
all-or-nothing on a mixed turn, result rendering) and 7 engine tests (off-by-default changes nothing
about the request or transcript; end-to-end parse→execute→text-result with no orphaned blocks and the
caller's system prompt preserved; gate consulted and denial honored; malformed call corrected and not
executed, corrective retracted; bounded give-up; a plain answer is not a correction; suppressed on the
step-limit turn), plus 2 config tests. Full suite green.

---

## P55.x — container-only scanning: making the multiscanner trustworthy, then preferred (SHIPPED 2026-08-02)

Filed and shipped the same day, off a full functional test of the multiscanner container against a
purpose-built multi-language vulnerable fixture plus a review of `internal/security`'s method
resolution across all 17 registered scanners. **Six of nine items shipped** — P55.1-P55.6, the Tier-1
integrity half and the Tier-2 half. P55.7 (`aegis-netscanner`) and P55.8 (gosec two-phase) remain
open in Tier 3; P55.9 stays parked in Tier 4 by its own measure-first criteria.

The strategic goal is that a user installs **one image instead of 17 tools**. The Tier-1 work had to
land first, because container-only means a broken container is no longer a degraded path — it is the
whole product.

**What the test actually found.** The container's *scanning* was sound: 14/14 bundled tools execute
offline and detection is genuinely good (trivy 59 vulns / 26 misconfigs / 5 secrets, osv-scanner 59
offline, gitleaks 5/5 planted secrets, end-to-end `aegis scan` 173 findings with cross-tool dedup and
ASVS tagging intact). What it found instead was a cluster of **provisioning** failures sharing one
shape: *the scanner silently or loudly stopped working and no layer of the system noticed.*

Four defects were fixed ahead of the batch and are its evidence base: kubescape fatally broken in
container mode (no rego library baked, so `--network none` gave `open $HOME/.kubescape/allcontrols.json:
no such file or directory`); kubescape's SARIF unparseable even once it ran (`--output /dev/stdout`
interleaved with its human summary table); njsscan broken by the semgrep removal (it shells out to a
`semgrep` binary by name, and the bare symlink let that lookup escape to the system PATH); and grype
absent from the pinned image entirely. **Two of the four survived a green `go test ./...`, a
successful image build, and a scan that reported findings.** The njsscan case is the sharpest
argument for P55.3: the Containerfile's build-time `njsscan --version` check passed for the entire
duration of the breakage, because `--version` never reaches the semgrep subprocess.

### P55.1 — Image/source drift detection

The image-ID pin proves the image hasn't changed since it was pinned; it cannot see that the image no
longer matches the *source it was built from*. That gap produced a real, undetected failure: the
pinned image predated three multiscanner commits, so it never contained grype. The consequence
chained and every link failed quietly — `update-db.sh` ran `grype db update` under `set -eu` on an
image without grype, aborting the entire database refresh, which is why `/cache/grype` had never
existed on that machine. Container-method grype would have been correctly refused by
`multiscannerDBTools`; it was masked only because grype happened to be on the host PATH.

A sha256 **source fingerprint** over the embedded Containerfile, `fetch.sh` and `update-db.sh` (the
`go:embed` set is already authoritative) is recorded at build time as
`security.multiscanner.source_fingerprint` and compared wherever image state is surfaced. Filenames
are included in the hash input so a rename is detected, and CRLF is normalized before hashing so a
Windows checkout with `autocrlf` doesn't report drift against byte-identical source.

**Drift reports rather than fails closed** — a deliberate departure in mechanism, not intent, from the
image-ID path whose wording it otherwise follows. An ID mismatch means "not the vetted artifact" and
must fail closed; drift means "the vetted artifact, built from older source," which is still a real
image worth scanning with. Refusing it would trade a silent under-report for a silent no-scan. A
config carrying `image_id` but no fingerprint reads as *unknown*, not as drift, so existing installs
upgrade without a spurious warning.

### P55.2 — `update-db` per-step, plus trivy's checks bundle

`update-db.sh` ran three fetches straight-line under `set -eu`, so the first failure aborted every
later one with no notion of partial success. The observed state was trivy and osv-scanner fully
populated while grype had no database at all, from a single early failure — one error, a non-zero
exit, and nothing saying "2 of 3 are fine."

Each refresh is now its own step. The load-bearing implementation detail: steps **re-invoke the script
as a child process** rather than running as a function or `if`/`||` condition, because those suppress
`errexit` for everything inside them — which would have silently lost the intra-step error detection
the plausibility assertions depend on. A separate process keeps its own `set -eu` fully armed while
the parent reads only the exit status. Outcomes collect into a summary; the exit status is non-zero if
any step failed. Inside the osv step, one ecosystem's failure no longer abandons the other nine.

Every plausibility assertion is preserved, scoped to its own step — the grype `vulnerability.db` and
npm `all.zip` size checks are exactly the right instinct and stay; they just no longer take the other
tools down with them.

Also folded in the **trivy misconfiguration checks bundle**, which no step populated, so every scan
logged `failed to check cache: cache does not exist at "/cache/trivy/policy/content"` and fell back to
trivy's embedded (frozen, older) checks. Degraded rather than broken — but an ERROR on every single
scan trains operators to ignore scanner errors. There is no download-only flag for the bundle at trivy
0.72, so the step runs one misconfig scan against a throwaway Dockerfile through the same
`trivy fs --scanners misconfig` entry point a real scan uses, then asserts the cache landed — a trivy
that can't reach the registry falls back to embedded checks and still exits 0. Verified in the image:
3.1MB written to `/cache/trivy/policy/content`.

### P55.3 — `aegis security verify-image`

`MultiscannerTools(profile)` is a **static list**. Aegis routed a scan to the container on the strength
of that list plus an image-ID match, never checking the named tool was present in the image or could
produce a result. Both failure modes were live simultaneously: grype in the list and absent from the
image; kubescape in the list, present, and fatal on every invocation.

`verify-image` runs a version probe **and** a canary scan against a small embedded fixture for each
tool the profile claims, asserting a **non-zero finding count rather than exit 0**. That assertion is
the entire point: a `--version` probe would have caught grype's absence but not kubescape's fatal, and
neither catches the class this codebase already documents for gosec and osv-scanner — a tool that
exits clean and reports zero because it never loaded its data. It runs as the last step of
`build-image` (`--skip-verify` opts out) and exits non-zero, so it works as a provisioning gate. A
missing DB cache is a distinct `blocked` status, not a tool failure.

**The canary found three real defects on its first run**, which is the item justifying itself:

- **syft's container mode was entirely broken.** `sbom.go` used `runContainerImage` (bare args, for
  entrypointed per-tool images) instead of `runScannerImage`, so the runtime tried to exec
  `/src/dir:/src` and exited 127. Invisible because its only caller silently falls back to a direct
  scan on any error.
- **gitleaks container mode reported 0 on a tree full of secrets** — the exact silent all-clear this
  item exists to catch. gitleaks 8.30 writes a temp report and *renames* it into place, and a rename
  onto `/dev/stdout` replaces the symlink instead of writing to the pipe: exit 0, empty stdout, parsed
  as zero secrets. Fixed with the report-file + `cat` pattern kubescape already uses.
  gosec/bandit/brakeman genuinely do stream to `/dev/stdout` and are unaffected.
- **Detector allowlists defeat "obviously fake" secrets.** Both gitleaks and trufflehog explicitly
  allowlist `AKIAIOSFODNN7EXAMPLE`, so a fixture built from canonically-fake credentials reported
  zero from both. The fixture keeps that pair as a documented trap and adds synthetic tokens that do
  fire; a test asserts every planted value is visibly fake.

Measured against the built image: **12 of 14 tools verified with findings** (trivy 48, kubescape 24,
grype 18, osv-scanner 16, opengrep 14, syft 7 components, bandit 6, hadolint 6, brakeman 5, njsscan 4,
gitleaks 2, trufflehog 1). nmap and nuclei take a network target, so no filesystem fixture can trip
them; they are skipped with a stated reason and get a version probe only. That is the one place the
canary requirement isn't met, and it isn't closable with a fixture.

### P55.4 — Container-first method resolution

`Resolve`'s `auto` branch tried host → container → WSL and returned the host binary the moment
`lookPath` succeeded. Of the scanners that ran in the end-to-end fixture test, seven resolved to
`host` and exactly one to a container path — and that one went to **WSL**, not the container. The
image was built, pinned, cache-populated, and almost entirely unused.

Host-first was right when the container was a *fallback* for tools the operator hadn't installed. It
is wrong once the container is the supported path: host binaries are unpinned, whatever version
happened to be on PATH, with no reproducibility and no `--network none` confinement, so two scans on
two machines can silently use different scanner versions and rule sets.

`auto` is now **container → host → WSL**, with the container leg gated on the multiscanner covering
the tool. A refused container never fails the tool — image-ID or cache verification refusing falls
through to host, since a working unpinned scan beats no scan. `method: host` and `method: container`
are untouched.

Deliberately **not** inverted for an operator-pinned per-tool `security.tools.<name>.image`: that
image is the operator's own artifact, gets only the digest-pin regex rather than the real image-ID
comparison the multiscanner gets, and already has an unambiguous opt-in in `method: container`. The
multiscanner had no such knob, which is why its default had to move.

Falling back to host is recorded rather than silent. `Resolve`'s four-value shape is the
`Scanner.Resolve` interface's, forwarded by ~14 implementations, so widening it was unavailable;
overloading `reason` would have been safe but invisible, since every display site discards it on
success. A `ResolveDetailed`/`Resolution.Note` seam carries it instead, collapsed at the display sites
to one advisory per distinct cause naming each tool once rather than fourteen near-identical
paragraphs. It cannot fire for an operator who never built the image, since `Resolve` only prefers the
container for tools the multiscanner covers — a host binary isn't their fallback, it's their plan.

**Cost regression, fixed in the same batch.** The inversion moved `detectRuntime` from "reached only
when no host binary exists" to "reached once per covered tool" — it shells out to `podman version` /
`docker version` under a 3s per-candidate timeout, so one `security status` render went from roughly
one probe to one per scanner. Image and cache verification were already memoized behind a 15s TTL; the
runtime probe was not. It now is, hanging off `Options` following the existing
`MultiscannerPolicy.check` convention — including the deliberate property that an `Options` built
directly in code gets nil and probes every call, which keeps the seam-swapping tests honest.
Deliberately not in `internal/sandbox`: `DetectBest`'s other callers are diagnostics
(`aegis sandbox detect`, `aegis doctor`) whose job is to report the runtime's state *right now*, and
serving them a 15s-old answer would make a diagnostic lie about the thing it was run to check. The
negative result caches too — it is the slow path, only reached after every candidate has timed out —
and concurrent callers collapse onto one probe via a pending-entry latch, since `engine.runTools` runs
read-capability tools concurrently and without it N goroutines would all miss a cold cache and all
shell out. Measured on `aegis security status` over three runs with podman up: **median 6.55s → 4.03s.**

### P55.5 — Pin the multiscanner globally by default

`build-image` wrote `security.multiscanner` to the *project's* `.aegis/config.yaml` unless `--global`
was passed. But the image lives in machine-wide container storage and the database volume is
explicitly one cache shared by every scan in every project — so the configuration was the only
per-repo part of an otherwise machine-wide asset.

The effect was invisible. Inside the repo it was built from, `security status` reported scanners as
`container`; from any other directory the same binary and the same image reported *"not installed …
run `aegis security build-image`"* — advice to rebuild an image that already existed. An operator
provisions once and concludes it didn't work.

The user config is now the default target, with `--project` for the narrow case of a repo deliberately
on a different image. `--global` is kept as a deprecated, hidden no-op rather than removed: it asked
for exactly what is now the default, so provisioning scripts pass it, and deleting it would fail those
runs *after* a multi-gigabyte build.

Since project config overrides user config, a `security.multiscanner` block left in a repo by an older
build silently shadows the new machine-wide pin — and every operator who ran the old command has that
state, with a symptom (an image ID that was just rewritten still failing) that points nowhere near the
cause. `build-image` now warns when it finds one, including the case where the project block names the
same image, which is harmless today but goes stale at the next rebuild.

The pin is also now built from the file being rewritten rather than from the merged config, via a new
`config.FileSecurity`. `patchSecurity` replaces a file's whole `security:` block, so carrying fields
through from the merge would copy one layer into the other — pinning the user config from inside a
repo would have promoted that repo's `security.tools` and `wsl_distro` machine-wide.

### P55.6 — Surface vulnerability-database age

Nothing reported how old the scanners' data was. Measured during the test: trivy's DB carried
`NextUpdate 2026-07-17` and was read on 2026-08-02 — 16 days past its own refresh horizon — and scans
reported no concern. That silence is partly by construction: the image sets
`GRYPE_DB_VALIDATE_AGE=false` and `TRIVY_SKIP_*_UPDATE=true`, deliberately disabling the tools' own
staleness guards, because `--network none` scans cannot refresh and a cached DB is *expected* to be
old. That is correct — but it shifted the responsibility to Aegis, and Aegis never picked it up. A
stale SCA database doesn't fail; it **under-reports**, the same silent-all-clear shape as P55.1-P55.3.

Age comes from trivy's `metadata.json` `UpdatedAt` (the database's own build time) and, for grype and
osv-scanner, the marker mtime — a *download* time, which is a lower bound on data age: it can
under-state age but never invent it, which is the safe direction. Each row states which source it
used. Marker paths come from the existing `multiscannerDBTools` map, so a new DB-backed scanner gets
an age report for free.

Warns past **7 days**, justified on the constant: every tool measures "current" in hours (trivy
rebuilds every 6h, grype and osv publish daily) and the loosest guard any of them ships is grype's
5-day `max-allowed-built-age`. Seven sits just past that on purpose — a daily *or weekly* refresh
cadence never warns and so never gets tuned out, while the case it catches is the real one: a cache
populated once at provisioning and never again.

Read-only by design: **never auto-refreshes and never fails a scan.** Air-gapped operation is a
supported posture and the no-network-in-scans decision is not reopened. Costs exactly one extra
container run, with a test asserting the count rather than a comment claiming it. Renders nothing at
all when no multiscanner is pinned — host-only scanners manage their own databases, and Aegis has no
standing to advise a container build on every status call.

### Two roadmap measurements that no longer reproduce

Recorded so they aren't re-investigated. The `aegis-scanner-cache` volume was repopulated during the
batch, so **grype's database is present, not absent**, and **trivy's DB is hours old, not 16 days** —
P55.6's stale path is covered by a fixture rather than by the live cache, and P55.1's account of the
grype-absence chain is now history rather than current state. Separately, the image pinned on the
development machine predates P55.1, so it carries no fingerprint and correctly reports *unknown*
rather than drift; drift begins reporting after the next `build-image`, which is also when P55.2's
rewritten `update-db.sh` enters the fingerprint set.


---

### P54.2 — SCA/secrets scanners swept for "accurate refusal, error-shaped" exit codes — CLOSED 2026-08-02, no gap found

Carried as an unfiled lead in roadmap.md since 2026-07-19. `runJSON`/`runContainerCLI` tolerate a
non-zero exit whenever output was produced (scanners exit non-zero on findings) and report the
*empty-stdout* case as a scan error. That rule is only correct if no scanner uses "non-zero exit,
nothing on stdout" to mean "there is nothing here to analyze" — which is exactly the P34.6 bug, where
brakeman's accurate refusal on a non-Rails repo surfaced as `brakeman: error: exit status 4`. P34.6
swept the four language-targeted engines and found brakeman was the only offender. Nobody had swept
the SCA/secrets half.

**Answered by running them, not by reading them** — the same methodology P34.6 used, and the reason
that item's follow-on question was trustworthy. Each tool was invoked at the version pinned in the
multiscanner Containerfile, with the exact arguments Aegis passes, against two workspaces with
nothing for an SCA tool to find: an empty directory, and a docs/C/shell tree (`README.md`, `main.c`,
`run.sh`) with no dependency manifest of any kind.

| tool | version | exit | stdout | verdict |
|---|---|---|---|---|
| trivy | 0.72.0 | 0 | valid SARIF | no gate needed |
| grype | 0.115.0 | 0 | valid SARIF | no gate needed |
| syft | 1.46.0 | 0 | valid CycloneDX | no gate needed |
| gitleaks | 8.30.1 | 0 | report file written | forced via `--exit-code 0`; report read independently of `Run()`'s error |
| trufflehog | 3.95.9 | 0 | *empty* | JSON Lines with zero lines is how it says "no secrets" — empty stdout with a zero exit is the success branch, so it parses to zero findings |
| osv-scanner | 2.4.0 | **128** | *empty* | the only refusal of this shape — already interpreted by P34.12 |

**Result: no code change to scanner behavior, and no new `RelevanceChecker`.** osv-scanner is to the
SCA/secrets half precisely what brakeman was to the language-targeted half — the one tool whose "not
applicable" was indistinguishable from "failed" — and it was fixed two batches ago. `interpretOSVError`
was re-confirmed to cover **both** execution paths (`osvScanner.Scan` routes the host and container
runners through it alike), so there is no half-covered case hiding behind the measurement.

Two notes on rigor, since a sweep that finds nothing is only worth as much as its method:
trufflehog was not installed on the measuring host, so rather than reason from its documentation the
pinned 3.95.9 release binary was downloaded and **SHA256-verified against the upstream checksums
file** the way `fetch.sh` does, then measured — it is the one tool whose result would otherwise have
been an assumption. And these are *host*-path measurements; the container path runs the same binaries
at the same pinned versions through `runContainerCLI`, which applies the identical empty-output rule.

**Shipped as documentation, deliberately.** A negative sweep's whole value is not re-running it, so
the measured table now lives in the `runJSON` doc comment (`internal/security/scanners.go`) — next to
the rule it justifies, in the same style as osv.go's measured exit-code block — with an instruction to
re-measure before adding a tool to `DefaultScanners` rather than assuming the pattern holds. No tests
added: these are claims about external binaries' behavior, which a unit test can only restate, not
verify. Full suite green.

---

### P54.1 — Cross-platform suite: a LaTeX confinement bypass on Windows, plus two POSIX-only test assumptions — SHIPPED 2026-08-02

Aegis is developed across a macOS machine and a Windows one, and the suite had quietly stopped being
green on the second. Three packages failed on Windows and passed on macOS. Only one was a test
problem in the harmless sense; the headline is a genuine security defect that was **invisible on the
platform it was developed on**.

**The bug: `\input{/etc/passwd}` validated as confined on Windows.** `latexResolveRef`
(`internal/tool/builtin/latex.go`) gates on `filepath.IsAbs(arg)` before falling through to
`filepath.Join(baseDir, arg)`. On Windows `filepath.IsAbs` requires a volume, so it answers **false**
for `/etc/passwd` — the argument then went through `Join`, which folds the leading separator away and
yields `<workspace>\etc\passwd`, a path the sandbox validator then confirms as perfectly confined.
Meanwhile MiKTeX resolves that same argument against the current drive root. The P52.2 confinement
scan therefore reported no escape for a read that really does leave the workspace, on Windows only.
All seven failing LaTeX tests (source scan, `.bcf` datasources, `.aux` `\bibdata`/`\bibstyle`) were
the same root cause, since the bibliography checker routes through the same resolver.

Fixed by `latexRefIsRooted`: `filepath.IsAbs(arg) || strings.HasPrefix(arg, "/")`. A no-op on
macOS/Linux (where `IsAbs` already covers it), and on Windows the rooted argument now reaches
`sandbox.ValidatePathIn` still rooted, where `absCandidate`'s existing P32.1 handling resolves it
against the drive root and correctly refuses it. Deliberately **not** symmetric on `\`: in TeX a
leading backslash is a macro escape (`\input{\jobname.tex}`), not a Windows root, and treating it as
a path would invent Windows-only false violations — so the scan's verdict is now identical on both
platforms in both directions. New `TestLatexResolveRefKeepsRootedPathsRooted` pins the property
directly (the candidate must never come back re-parented under `baseDir`) rather than relying on the
table test that only caught it by accident.

**The same trap, swept.** `server.resolveSafeImagePath` gated its "absolute image paths are not
allowed" refusal on `filepath.IsAbs` too, so on Windows the refusal silently stopped applying and the
path was joined onto the working directory instead. Not an escape — the subsequent `filepath.Rel`
check still held — but the same input was refused on macOS and quietly accepted on Windows. The rule
now has **one home**, `sandbox.IsRooted`, covering `IsAbs`, Windows rooted-without-volume (`/x`,
`\x`), and Windows volume-relative (`C:x`); drift between three hand-written spellings of this test
is exactly what produced the LaTeX bug, so new callers are pointed at it in the doc comment.

**Two POSIX-only test assumptions, fixed as tests.**

- `internal/engine`'s `TestWallClockBudgetStopsRun` expressed "already over budget" as a **1ns**
  limit, on the stated reasoning that `time.Since` is always at least a nanosecond by the time the
  first gate runs. True on macOS/Linux; false on Windows, where Go's monotonic clock is ~0.5ms-granular
  and two back-to-back `time.Now` calls return the *same* instant — measured here as 1000/1000
  zero-elapsed samples. The gate saw `elapsed == 0` and never fired. Production was never wrong (the
  knob is configured in whole seconds), so the fix is an unexported `Engine.now` injected only by
  same-package tests, and the test now advances a fake clock by an hour instead of trusting the host
  clock. `TestWallClockBudgetCountsToolTime` deliberately keeps its real 120ms-vs-50ms sleep — its
  whole point is that *actual* tool time counts, and a fake clock would hollow that out.
- `internal/sandbox`'s two `TestValidatePathInSymlink*` cases called `t.Fatal` when `os.Symlink`
  failed, which on an unelevated Windows account without Developer Mode is "a required privilege is
  not held by the client" — an environment limitation reported as a defect, which trains people to
  ignore a red suite. Now a shared `mustSymlink` helper that `t.Skipf`s with the remedy in the
  message. Deliberately a check on the *error* rather than a blanket `GOOS == "windows"` skip, and
  the two pre-existing blanket skips in `sandbox_test.go` were converted to it as well — a Windows box
  with Developer Mode enabled now gets real coverage of the symlink-escape rules, which is the OS
  whose path semantics most deserve them.

Full suite green on Windows; `go vet ./...` clean cross-compiled for darwin/arm64 and linux/amd64
(so the test files, not just the binary, are checked for both); `-race` clean on `internal/engine`,
`internal/sandbox`, `internal/server`, `internal/tool/builtin`.

---

### P53.5 — Per-model capability records persist instead of being re-discovered each restart — SHIPPED 2026-08-02

Aegis learned model quirks the hard way and then forgot them every restart. The `think`-parameter
rejection latch was the clearest case: `thinkRejected sync.Map` discovered that a model 400s on any
`think` value *by sending one and being rejected*, then cached that for the process lifetime only —
so every daemon start re-paid the failed request. The same was true of everything `toolcallprobe`
learned, including the P53.4 conformance sample, which on a model generating at single-digit
tokens/sec is five sequential generations thrown away at exit.

**Shipped.** New `internal/modelcaps`: a JSON store under `<data_dir>/model_caps.json`, keyed by
model name, holding the `think`-rejection flag, the P53.4 conformance sample (verdict + trials +
tool-call trials + no-verdict count), and the model manifest's own native tool-support claim. 0600,
owner-restricted via `fsguard`, human-readable.

**pi's pattern, synthesized rather than copied.** pi (badlogic/pi-mono) records capability
declaratively in `models.json` (`supportsDeveloperRole`, `supportsReasoningEffort`,
`thinkingLevelMap`, …) — the *complement* of what Aegis does, not a replacement. The synthesis
shipped here is better than either: probe once, persist the result, let the user pre-declare a quirk
for a model the probe has never met, and let an explicit declaration outrank a discovered value.
Precedence is **declared > persisted-discovered > default**, enforced inside the store so no caller
can get it wrong.

**Staleness is the reason this was never done before, and is what the design turns on.**
`toolcallprobe.Gate`'s own comment refused persistence on exactly these grounds: an Ollama tag is
mutable, so `ollama pull qwen3:14b` can replace the weights without the name changing, and a verdict
written to disk could outlive the model it describes. Records are therefore keyed to the model's
**content digest**, not its name: new `ollamainfo.Digests` reads `/api/tags`, `Store.Reconcile` drops
every record whose digest moved and stamps the rest, and `modelcaps.ReconcileOllama` runs that before
anything writes — in the daemon at start, and in `aegis doctor` before it probes, so a verdict is
always stamped with the digest it was actually measured against. A model *absent* from the digest map
is deliberately left alone: absence means "couldn't ask" (unreachable server, non-Ollama provider),
and "cannot tell" must never be read as "everything is stale". A record written with no digest
available (server down at the time) is adopted by the next reconcile rather than discarded — losing a
real measurement on missing evidence is the wrong trade.

**Cache, never source of truth.** Nothing here can wedge a model into a capability it doesn't have.
A record is only ever consulted to *skip* work already proven to fail; every miss falls through to
the live discovery path unchanged. Deleting the file costs exactly one re-probe. The asymmetry in the
adapter is the load-bearing part: a store saying "rejected" latches, a store saying "not rejected"
(what a `think: true` declaration produces) latches *nothing* and lets the model answer for itself —
which is what makes a wrong cached value recoverable without touching the filesystem. An **unreached**
verdict is never persisted at all (`SetToolCalling` refuses anything but `ok`/`unsupported`), because
persisting one would carry the P34.2 false positive across restarts rather than merely across
sessions.

**Wiring.** `ollama.WithCapabilityStore` backs the P52.5 latch (in-memory map stays the hot path;
the store is consulted once per model on first miss, and written once on discovery).
`toolcallprobe.WithStore` seeds the gate's verdict *and* sample, so a restarted daemon returns the
persisted verdict with **zero** probes — test-pinned against an adapter that fails on any call. The
gate persists from trial 1 rather than waiting for background refinement, so a daemon exiting
mid-sample still saved the verdict it reached; refinement overwrites with the fuller sample. Both
interfaces are declared in the consuming packages over flattened scalars, so neither
`internal/provider/ollama` nor `internal/toolcallprobe` imports `modelcaps` — `modelcaps.ProbeStore`
adapts across, preserving the manifest claim across probe writes (the measured verdict and the
manifest's claim are allowed to disagree, and that disagreement is the informative part, so neither
may erase the other). `providerfactory.Build` grew a variadic `Option` with `WithModelCaps`, wired
from the daemon and from `aegis chat` / `aegis worker` / `aegis debate` / `aegis doctor`.

**`aegis doctor` seeds the daemon.** doctor takes the best sample Aegis ever takes — fully blocking,
full trial count — and now writes it to the same store, so a daemon started afterwards reuses it and
probes nothing on its first message.

**Config:** `provider.model_capabilities`, a map of model name → `{think, tool_calling}`, every field
optional (unset declares nothing). Documented in [configuration.md](../docs/configuration.md) and
[providers.md](../docs/providers.md). `Config.ModelCapsPath()` returns `""` for an empty `data_dir`,
which `modelcaps.Open` turns into a working in-memory-only store — without that guard
`filepath.Join("", name)` is *relative* and every hand-built test config would drop a cache file into
the working directory.

Tested: 13 store tests (round-trip across reopen, both declaration-overrides-persisted directions,
unknown-verdict-never-written, digest reconcile drop/keep/adopt, manifest-claim preservation, corrupt
file, nil receiver, path-less store, JSON shape), 2 adapter tests (latch survives a fresh adapter and
re-writes nothing; a store saying "not rejected" does not suppress `think`), 4 gate tests (persist +
reuse-with-zero-probes, no-verdict never persisted, trial-1 persisted before refinement completes, no
store = unchanged behavior). `-race` clean on `internal/toolcallprobe` and `internal/provider/ollama`.
Feeds **P53.6**, which consumes the persisted rate as its fallback-engagement signal.

---

### P53.4 — `toolcallprobe` reports a conformance rate, not just a boolean — SHIPPED 2026-08-01

The probe ran one smoke prompt and reported `ToolCalls`/`Truncated` — answering "can this model ever
emit a tool call" but not "how often does it", and the second question is the one deciding whether an
unattended drive survives. A model that complies 60% of the time passed cleanly and then failed a long
run in a way that looked like a harness bug, which is the class of confusion the P39.x re-test history
is full of. aider's polyglot leaderboard publishes percent-using-the-correct-edit-format as a second
column for exactly this reason; goose publishes the gradient informally (discussion #1403) and ships
no probe at all.

**Shipped.** `Run` is untouched — same signature, same contract, same single-trial semantics, since it
is the fast path other code depends on. New `internal/toolcallprobe/conformance.go` adds
`RunTrials(ctx, adapter, model, trials)` returning a `Conformance{Trials, ToolCallTrials, NoVerdict,
Results, Err}` with `Denominator()`, `Rate() (float64, bool)`, `Verdict()`, and `Summary()`. Trials
run **sequentially** on purpose: a local model server serializes generation anyway, so concurrency
would only distort latency and, on a memory-tight box, the results.

**The no-verdict accounting is the point.** `Run`'s contract — `ToolCalls == 0 && Truncated` is *no
verdict*, never failure — is preserved **per trial**, and such trials are excluded from the
denominator rather than counted as misses; aggregating them as failures would silently re-introduce
the P34.2 false positive the contract exists to prevent. Rate is `ToolCallTrials / (Trials −
NoVerdict)`. A trial that made a call and *then* truncated counts as a call. When every trial is
no-verdict there is **no rate at all** — `Rate()` returns `ok=false` and `Verdict()` returns
`Unknown`, so there is no bare float that could read as 0.0 and accuse a model the probe never got an
answer out of. `Verdict()` is deliberately *not* a threshold on the rate: "made a tool call at least
once" is the claim this probe can justify, and turning a partial rate into an `Unsupported` verdict is
a policy decision belonging to whatever engages a fallback (P53.6), not to the measurement.

**Transport errors abort only if nothing was measured.** A first-trial failure returns an error,
identical to `Run`. A failure after ≥1 successful trial stops the loop and returns the partial sample
with `Conformance.Err` set and a nil error — a rate over 3 of 5 trials is worth more than discarding
the sample — and `Summary()` says "sample cut short after N trial(s)".

**Config:** `provider.tool_call_probe_trials`, default 5 (the sample `SmokeMaxTokens`'s own
calibration used). `1` or less reproduces pre-P53.4 behavior exactly — one `Run`, no background
goroutine — and that equivalence is test-pinned at both `RunTrials` and `Gate`.

**`aegis doctor`** runs the full sample inline and reports the rate. Because that is up to N× slower,
it announces the cost *before* any check runs rather than appearing to hang, naming the trial count,
the model, the per-trial bound, and the config key to turn it off; the announcement is suppressed for
cloud/unresolved-model configs and for `trials=1`. The command's overall timeout grows per extra trial
so the extra trials cannot starve the other rows.

**The daemon must not pay for this, and doesn't.** `Gate` is consulted from the message path before
the user's first reply, so five sequential generations inline would be minutes of dead air on a model
running at single-digit tokens/sec. The **first** trial runs exactly as before and produces the
blocking verdict; the remaining trials run in **one background goroutine per model**, publishing each
result as it lands so even a cancelled refinement contributes what it measured. It is parented to the
gate's own lifetime context (`bgCtx` from `context.Background()`), explicitly **not** the per-request
context — that one is cancelled the moment the message completes, which is roughly when refinement
would be starting, and a regression test cancels the request context and asserts no background trial
dies with it. Each background trial is bounded by the same `ProbeTimeout`; `Gate.Close()` cancels
`bgCtx` and waits with a 2s grace (blocking daemon shutdown on a slow model server would be the worse
trade) and is called from the server's shutdown branch. Refinement is scheduled inside the existing
singleflight flight and further guarded by a `refining[model]` map so a re-probe cannot start a
second. All conformance state lives behind the gate's existing mutex; `-race` clean on
`internal/toolcallprobe` and `internal/server`.

The live tier (`probe_live_test.go`, `live_probe` tag) had a hand-rolled 5-run loop; it now calls
`RunTrials` + `Rate()`, so the tier that exists to check the probe against real model behavior
exercises the shipped accounting rather than a parallel copy of it. Feeds **P53.5** (the rate is the
value worth persisting) and **P53.6** (the rate is the natural trigger for engaging a fallback).

---

### P53.3 — Compaction reserves headroom for its own summarization call — SHIPPED 2026-08-01

`summarize` sent the **entire** prefix transcript in one unbounded request — no chunking, no size
cap, and no check that the resulting request fit the window it exists to stay inside. goose has hit
this repeatedly and unrecoverably (block/goose#8642, #4635: compaction fires too late, the
summarization call itself exceeds the limit, session dead). Aegis was better protected — the trigger
leaves real slack, `pruneStaleToolResults` runs first and unconditionally, and a failed compaction is
non-fatal (logged, run continues uncompacted, falling through to reactive `RaiseContextWindow`) — so
this shipped as hardening, not a live-bug fix.

**Shipped.** `fitTranscript` runs before the request is built. `summarizeFitBudget` = context window
(or `maxBudget`) − `summaryTokens` − a reserve; `summarizeRequestTokens` prices the **whole** request
via `tokenest` (the repo's single estimator — no second heuristic), including the system prompt and
preamble, which were extracted into `summarizeSystemPrompt`/`summarizePreamble` constants precisely so
they could be counted. If it does not fit, a deterministic two-stage shrink runs: **stage 1**
truncates oversized individual blocks middle-out down a descending rune-cap ladder
(`{4000, 2000, 1000, 500, 250}`), re-estimating at each step; **stage 2**, only if that is still
insufficient, drops the oldest messages one at a time. If even that cannot fit, it returns an error
rather than issuing a request already known to be too large — non-fatal, on the existing logged path.

`summarizeReserveBuffer = 10_000` / `summarizeReserveRatio = 0.10` mirror the trigger constants
(`largeContextWindowBuffer` 20k / `smallContextWindowRatio` 0.20) at half size, across the same
`largeContextWindowThreshold` split. Half is the point: the trigger buffer absorbs *future* growth,
while this reserve only has to absorb how wrong `tokenest` can be about text already in hand.

**Truncation is marked, and omission is admitted.** Middle-out keeps both what a block is and what it
concluded, with a visible `…[truncated by compaction: N characters elided]…` marker so the
summarizing model cannot mistake a truncated block for a complete one. Stage-2 drops are worse than
lossy — `Compact` replaces the whole prefix with the summary, so anything dropped is gone from
history with no record — so when stage 2 fires, the returned summary carries an explicit note that
the N earliest messages were omitted and nothing above reflects their content. A silently-lossy
summary must never present itself as complete.

**Two things worth knowing.** (1) The fit check is skipped when the budget is `<= summaryTokens`, not
only when it is 0. Several existing tests and the `MaxBudget` semantics use a tiny fixed *trigger*
budget (`MaxBudget: 5`, `400`; the server uses `1`), which is not a context window and says nothing
about what a request may weigh — treating one as a fit budget would turn every such caller into a
hard error. Real windows (2048 up) are still checked. (2) The roadmap filed this against "a single
very large tool result", but that shape was already largely defused: `renderTranscript` has long
capped tool results at 800 runes. The genuinely uncapped paths are **text blocks and tool-use
inputs**, so those are what stage 1 actually protects, and the tests exercise a giant text block for
that reason. The standing 800-rune tool-result cap now carries the same explicit marker instead of a
bare `…`.

`boundary`'s pairing invariant is untouched: `Compact` replaces the entire prefix with one summary
message and re-appends `msgs[boundary:]` verbatim, so truncating and dropping only affect rendered
text that never reaches the wire as blocks — no `tool_use`/`tool_result` pair can be split. Five
regression tests in a new `fit_test.go`, including one proving the common path renders unchanged.

---

### P53.2 — Loop detector: polling exemption + differentiated outcomes — SHIPPED 2026-08-01

Two defects in one mechanism, found by cross-checking against OpenHands' Stuck Detector. **(a)** An
agent legitimately polling a long-running process was indistinguishable from a stuck one, with
`canonicalizeToolInput` as an accelerant — it rewrites timestamp/UUID/long-digit/hex leaves to
`‹volatile›`, which is correct for its own purpose but erases the one field that would *distinguish*
successive polls. OpenHands ships this same false positive (#5355) *without* the canonicalization
accelerant. **(b)** Every trigger was a fatal `KindError` ending the run, though a model repeating a
*succeeding* call is confused and recoverable with one nudge while a model repeating a *failing* call
has exhausted its own recovery.

**Shipped (a).** `tool.PollExempter` is a new optional Tool extension (`PollExempt(input) bool`) with
an `IsPollExempt` helper, following the established `OutputSchemer`/`CapabilityOverrider` idiom.
`turnSignatureExcludingPolls` drops exempt calls from the turn signature, and a turn made up
*entirely* of polls is not recorded at all — recording it as an empty signature would be actively
harmful, since empty equals empty and a few poll-only turns would form a perfect period-1 cycle,
tripping the very detector the exemption exists to quiet. Three builtins qualified: `task_get` and
`task_output` (background-job status/output — the textbook wait loop; the answer changes on the job's
timeline, and there is no non-polling way to call either) and `team_inbox` (the file-based swarm
mailbox has no callback, so re-reading until a teammate replies is the intended coordination
pattern). Deliberately **not** exempted: shell/bash (the most-looped tool — blanket-exempting it
would gut the detector), `task_list`/`team_task_list` (discovery listings, not tied to a specific
awaited thing), `team_task_claim` (wait-shaped, but it mutates and drives work — a claim loop that
completes nothing is a real stall), and `cron_list`/`cron_history` (cron's cadence is minutes, far
longer than a run's turn cadence, so in-run repetition is not waiting).

**Shipped (b).** The detector now carries a per-turn outcome (`loopOutcome`, index-aligned with the
signature window) plus `noteOutcome`/`cycleHadError`/`reset`. An error cycle aborts with today's
byte-identical message. A succeeding cycle earns one corrective nudge, a window reset, and a
`KindNotice`; a second trigger in the same run aborts with `— the corrective prompt did not break the
cycle` appended. The nudge is bounded to one per run via `nudgeState.loopNudges` and retracted through
the existing `retractNudges` path.

**Three decisions worth keeping.** (1) **Argument inference was rejected** for the poll signal: the
obvious tell — successive calls differing only in a timestamp or cursor — is exactly what
`canonicalizeToolInput` normalizes away, so re-deriving it would mean either weakening that
normalization or bolting a contradictory second heuristic onto it. Worse, a false positive silently
disables one of the few guards bounding a runaway unattended drive. An explicit opt-out keeps the
exemption set small, reviewable, and greppable. (2) **The `Approver` seam was not used** for the
recoverable case, deviating from this item's original filing: under the non-interactive approver an
unattended drive uses, an approval-style check warns and allows, making the detector toothless in
precisely the scenario it exists for. Nudge-once-then-abort behaves identically attended or
unattended. (3) **The nudge is injected after the triggering turn's tool round**, not at the gate —
appending a user message at the gate would leave the assistant's `tool_use` blocks without matching
`tool_result` blocks, an invalid transcript. This is the same injection point the P52.3 nudge uses;
the fatal paths still return at the gate, before the round, so they add no side effects.

`cycleHadError` votes only on turns with a known outcome — the triggering turn's tools have not run
yet — and is "any errored round in the window", not "all", since a cycle that fails part of the time
is not one a nudge about repeating *successful* work should be describing. One eval assertion moved
with the behavior: `TestAdversarial_LoopDetectionNotEvadedByNonce` used a succeeding tool, so it now
aborts on call 6 (nudge at 3, reset, abort at 6) rather than 3; its `ExpectErrorContains("loop")`
claim is unchanged. Seven new engine tests cover both halves plus unchanged period-1..4 detection.

---

### P53.1 — Stale P33.10 comment on `WithKeepAlive` (doc-only correctness) — SHIPPED 2026-08-01

`internal/provider/ollama/ollama.go` documented `WithKeepAlive` as "Not yet driven by config — see
roadmap P33.10". That was false: P33.10 was closed by P35.4, and `cfg.Provider.KeepAlive` is threaded
end to end through `providerfactory.Build` → `buildOne` → `ollama.WithKeepAlive`, with a bounded
`defaultOllamaKeepAlive = "30m"` substituted when unset and an explicit `"-1"`/`"0"` allowed to win.
Filed rather than fixed silently because the comment had already produced a measurable error: a
2026-08-01 audit of the Ollama subsystem read it and reported a config-wiring gap that does not
exist, which then propagated into a recommendation.

**Shipped.** The comment now states the providerfactory default and explains why the policy lives
there rather than in the adapter (whose own default genuinely is still "omit", so it stays a faithful
transport). One adjacent misattribution was caught in the same pass and fixed: `ProviderConfig.KeepAlive`'s
doc in `internal/config/config.go` credited the *native adapter* with substituting the bounded
resident default — the same wrong claim that seeded the bad audit — now corrected to providerfactory.

**The sweep found nothing else.** All `roadmap P<n>.<m>` / `see P<n>.<m>` / `TODO(P…)` references,
every `TODO`/`FIXME`/`XXX`/`HACK`, and every `P<n>.<m>` token co-located with future-tense or
incomplete language were checked repo-wide (2312 tokens across 430 files, `research/` excluded).
Nearly all are pure provenance citations for shipped work, which are correct history and were left
alone; the outstanding-work references that remain (`docs/tools-reference.md` on P49.3/P49.4) point at
genuinely open items. Doc-only, no behavior change, no test; `go build ./...` and `go vet ./...` clean.

---

### P52.15 — Wall-clock run budget (the dimension that actually hurts on local hardware) — SHIPPED 2026-08-01

Three budgets existed and none of them bound *time*: `BudgetUSD` is an explicit no-op for unpriced
local usage, `MaxTokensPerRun` defaults to 0, and `MaxIterations` defaults to 40. On a model measured
at ~7 tok/s (the P38.1 note), 40 iterations is potentially hours before any safety valve trips — and
the user's actual constraint is almost always "don't spend more than N minutes on this", which
nothing expressed.

**Shipped.** `engine.Options.MaxWallClockPerRun` is checked at both existing budget gates (before
each model turn — the P9 dead-zone placement, since a guard corrective retry or a max-token
continuation is just as much elapsed time as a tool round — and again before each tool round, so a
run stops before side effects rather than one iteration late). Aborts wrap an exported
`engine.ErrWallClockLimit` so a caller can classify "ran out of time" apart from "ran out of
iterations", the same idiom as `ErrToolFailureLimit`; the message names
`cost.max_wall_clock_per_run`. Configured as `cost.max_wall_clock_per_run` (seconds, read via
`CostConfig.MaxWallClockPerRun()`), wired into the daemon engine build, the CLI chat engine, and both
swarm backends.

**Four decisions worth keeping.** (1) **Off by default**, and this is the load-bearing one: a
wall-clock cap cannot distinguish a stalled run from a slow one making real progress, so any non-zero
default would eventually guillotine legitimate long work — the same regression shape the P52.3
reconcile caught. Opt-in only. (2) **Per-`Run`, not global**, which in the phased drive makes it
per phase turn — the roadmap item worried a global cap would kill a long build mid-phase, and
per-`Run` scoping resolves that for free. (3) **Fatal to the drive**, unlike a context overflow
(P47.2/P47.7) or a tool-failure stall (P52.3), which reset and resume. Those are conditions a fresh
context genuinely clears; a wall-clock limit is an operator saying "stop after N minutes", and
resetting past it would defeat setting it. Pinned by a test asserting both classifiers decline
`ErrWallClockLimit` and consume no reset budget. (4) **Sub-agents inherit the bound whole** rather
than getting a divided share the way the cost/token floors do — spend is additive across siblings,
elapsed time is not; teammates run concurrently, so "N minutes" means the same N minutes for each.

**One incidental fix.** `patchCost` splices in a freshly built `cost:` block, so any key
`buildCostBlock` doesn't write is erased from the user's file. Adding a new cost key without
threading it through `CostPatch` would have made `aegis harden` silently delete a wall-clock bound
the user had set. Carried through with a regression test; `harden` itself still sets no wall-clock
value (it's an operator preference, not a security control).

**Its stated gate is now resolved.** The item said "build P52.3 first and see whether this is still
wanted" — P52.3 shipped, and it does not cover this: the breaker fires on *failing* tool calls, not
on a run that is progressing slowly or wandering productively. The surface that motivated shipping is
spawned swarm teammates (`inprocess.go` / `worker.go`), which had budget floors for USD and tokens
but nothing bounding duration, and no human present to interrupt.


### P52.17 — Run the tool-calling probe automatically on first use of a newly selected model — CLOSED 2026-08-01 (already implemented)

**The item's premise was a review error; it describes work that shipped with P34.2 itself.** Filed on
the observation that the engine's P34.2 notice detects a tool-incapable model only *after* a turn is
spent — true of that notice (lever 2), but it is not the only mechanism. **Lever (1) already does
exactly what this item proposes:** `Server.toolCallingWarning` (`internal/server/toolcalling.go`) runs
the smoke probe at **run start** against the *resolved* model and emits an up-front warning
(`internal/server/messages.go`), backed by `toolcallprobe.Gate` — a singleflight per-model verdict
cache that probes once per model per daemon and deliberately declines to cache an inconclusive
verdict. Warning is bounded to once per session per model, re-firing on a model switch. Shipped in
commit `e1b55f1`, months before the review that filed this.

The review found lever (2) and stopped. The lesson worth carrying: a roadmap item asserting "X is not
done" needs the *absence* verified, not just the presence of a worse mechanism nearby.

**Placement is already better than what the item asked for.** It names three model-selection sites
(daemon start, `PATCH /sessions/{id}`, the TUI picker); run start is the one choke point downstream
of all three, and the only place the model is known after the persona pin, the per-session `/model`
override, and P30 routing have resolved. It is also the moment the model gets loaded anyway, so the
probe shares that cold load instead of adding one. That rationale is recorded in
`internal/server/toolcalling.go`'s doc comment.

**Two residuals, both already decided against in the P34.2 notes** (see ../releases.md) and neither
reopened: the verdict cache is in-memory so it doesn't survive a daemon restart (the item's own
"design question"), and the CLI `chat` path is excluded on purpose — probing in a one-shot process
doubles the model calls of every scripted `aegis chat` and never repays the cache, and lever (2)
covers that surface at zero cost.


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

### P38.8 — External per-phase threat-model wrapper as interim autonomous-build workaround — SUPERSEDED, closed 2026-08-01

Until the built-in drive reliably converged, a completed, verify-clean suite was reachable by
driving Aegis outside the `--skill` loop, one phase at a time with bounded context. A reference
implementation was recorded at `tools/aegis-threatmodel.sh` (+ `tools/THREAT-MODEL-AUTOMATION.md`)
in the FirewallRiskRater repo: it ran `scaffold.py`, then a small **skill-free** `aegis chat` per
phase (architecture → DFD → STRIDE → findings → assessment), re-invoking while a phase's file
still had `PENDING` markers with an "act now" preamble, then ran the P37 checks and looped their
failures back to the model until clean. Because each turn's context was just the prompt + that
phase's files, the compaction wedge and preload bloat that hit the built-in path never triggered.
Validated per-phase on `qwen3.6-fast-32k`, 2026-07-21 — all five content phases completed and the
suite verified clean after the fix loop.

**Superseded 2026-07-24**, the day after filing: the built-in `chat --skill threat-modeling` drive
gained the same per-phase context reset natively (`internal/cli/chat_phased.go`, later lifted into
`internal/drive` by P52.12), so the external script duplicated harness behavior rather than filling
a gap. Kept parked rather than deleted so the working recipe wasn't lost while the in-harness path
was still being hardened (P47.x/P50.x). **Closed 2026-08-01** during a roadmap cleanup: the
in-harness path has had a full stability batch (P47.x) and a daemon-wide lift (P52.12) since, so the
wrapper needs no further investment and the historical reference no longer earns a roadmap slot.

### P52.x batch build order and reconciliation findings

**Build order.** Tier order was the priority; within a tier, built in the sequence below — it
front-loaded the two correctness/security items, then the cheap self-contained wins, and deferred
the two larger structural items until their dependencies existed.

| Order | Item | Tier | Outcome |
|---|---|---|---|
| 1 | **P52.1** per-model context window | 1 | SHIPPED 2026-07-31 |
| 2 | **P52.2** `latex_build` confinement | 1 | SHIPPED 2026-07-30 |
| 3 | **P52.3** tool-failure circuit breaker | 2 | SHIPPED 2026-07-31 |
| 4 | **P52.4** per-request `num_ctx` | 2 | SHIPPED 2026-07-31 — with P52.1, as required |
| 5 | **P52.5** `think`-rejection latch | 2 | SHIPPED 2026-07-30 |
| 6 | **P52.6** `RaiseContextWindow` mutex | 2 | SHIPPED 2026-07-30 |
| 7 | **P52.7** suite-wide hollowness check | 2 | SHIPPED 2026-07-30 |
| 8 | **P52.8** threat-model substance floor | 2 | SHIPPED 2026-07-31 |
| 9 | **P52.9** `yaml_validate` tool | 2 | SHIPPED 2026-07-30 |
| 10 | **P52.10** `latex_build` bib pass | 2 | SHIPPED 2026-07-31 |
| 11 | **P52.11** documentation-as-code skill | 2 | SHIPPED 2026-07-30 |
| 12 | **P52.13** `workspace.additional_roots` | 3 | SHIPPED 2026-08-01 |
| 13 | **P52.12** lift phased drive into daemon | 3 | SHIPPED 2026-08-01 |
| 14 | **P52.14** session-scoped loop detector | 4 | open — see [roadmap.md](roadmap.md) |
| 15 | **P52.15** wall-clock run budget | 4 | SHIPPED 2026-08-01 |
| 16 | **P52.16** native tool-result disambiguation | 4 | open — see [roadmap.md](roadmap.md) |
| 17 | **P52.17** auto tool-calling probe on model switch | 4 | CLOSED 2026-08-01 (already implemented) |

**Batch 1 shipped 2026-07-30 (parallel).** P52.2, P52.5+P52.6, P52.7 and P52.9 were built
concurrently as four file-disjoint lanes and reconciled in one pass. Two findings from that batch
changed later work: **(a)** P52.2's prescribed `openin_any=p` fix is a **no-op on TeX Live 2026**
(upstream made the setting inert), so the confinement is carried by a static source scan instead —
which matters for **P52.10**, whose `biber`/`bibtex` subprocesses the scan does not cover; **(b)**
P52.7 added check 15 rather than renaming check 12, because `chat_phased.go` routes on the literal
check name.

**Batch 2 shipped 2026-07-31 (parallel) — the batch's Tier-1/Tier-2 work closed.** P52.1+P52.4 (one
lane, two halves of one correctness story), P52.3, P52.8 and P52.10 were built concurrently as four
file-disjoint lanes and reconciled in one pass. Four findings from that batch mattered to later work:

- **The reconcile pass earned its keep.** P52.3's new abort would have been a *regression* for the
  phased drive: `runPhasedSkillDrive` treats any engine error that is not backend-down or a context
  overflow as fatal, so a stall that used to burn to `maxIterations` and limp onward would have killed
  an unattended run — the exact manual-re-invocation failure P47.x/P50.x exist to remove. Neither lane
  could see it (the engine lane was confined to `internal/engine`; the drive lived in `internal/cli`).
  The abort wraps an exported `engine.ErrToolFailureLimit` and the drive treats it as a resumable
  phase reset at all three `eng.Run` sites. This generalized cleanly into **P52.12**: every new
  terminal engine error got a deliberate answer to "what does the phased drive do with it?" as the
  drive moved into the daemon.
- **P52.4 did not touch `internal/engine`.** The per-run window reaches the request through a
  `provider.WithNumCtx` decorator the server wraps its shared adapter with, following the existing
  `Unwrap() Adapter` convention — the seam **P52.12** reused rather than adding a `num_ctx` field to
  `engine.Options`.
- **P52.10 rejected `latexmk`**, the roadmap's stated first preference. Not for the rc-file reason
  (`-norc` answers that) but because the `.bcf` confinement check must sit *between* latexmk's own
  generate and invoke-biber steps, and latexmk exposes no seam there. Option 2 shipped instead.
- **The threat-model Python scripts had no automated coverage at all** before P52.8 added
  `_verify_substance_test.py`. Anything touching `verify.py`/`scaffold.py`/`normalize_ids.py` should
  extend it rather than assume the Go side covers them — it only stubs `verify.py` or checks it
  materializes byte-identically.

**Both leads filed by batch 2 were investigated and closed the same day (2026-07-31).** (a) The
compaction summarizer was tuned to the *global* model's window while running on
`provider.small_model`: confirmed real, fixed by keying the summarizer to a new `s.compModel` and
resolving its window through `effectiveContextWindowFor`, with a post-run refresh so the entry
doesn't stay stuck on a startup guess. (b) Sub-agent `ContextWindowTokens: 0`: confirmed, fixed,
**and the lead's framing was wrong in a way worth recording** — spawns were *not* left with no
compaction. `engine.Run` calls the compactor **unconditionally at entry** (`engine.go:345`),
independent of `ContextWindowTokens`; what a spawn lacked was the *per-turn* 85%-fill check. The
first attempt at a regression test passed against unfixed code for exactly that reason — any future
test of compaction behaviour must count calls, not merely assert one happened.

**Follow-up filed by batch 2: content-substance check routing (extends the P52.7 follow-up) —
shipped as part of P52.12, 2026-08-01.** P52.7 noted that a `section-bodies-nonempty` failure fell
through to the generic verify-fix turn instead of routing to the phase owning the named file; P52.8
made this bigger — checks 16-19 are also per-file, so five checks wanted file-aware routing, and
`contentSubstanceChecks` mapped check-name → phase and couldn't express it. The fix parses the
failing file out of the `file:line` failure line and maps file → owning phase (the phase globs
already encode that mapping — `skillPhase.globs` is a file→phase table read the other way). Landed
alongside P52.12's move of the phased drive into `internal/drive`: see `contentSubstanceChecks`'s
`perFile` field and `fileOwnerPhase` in `internal/drive/drive.go`. (This follow-up never got its own
`P<n>.<m>` number and roadmap.md kept describing it as outstanding after it shipped; corrected
2026-08-01.)

**Remaining P38.1 debt at the time:** the in-harness phased-drive convergence tracking (see
[roadmap.md](roadmap.md)'s P38.1 entry). The 2026-07-23 gpt-oss:20b housekeeping closed the same
day — **P39.10**/**P39.11** were already coded, shipped, and verified live; as of 2026-07-27 they
also have their own regression tests (`TestDriveOraclesSkipBuiltinSkillsSubtree` +
`TestDriveOraclesSkipRealMaterializedBuiltins` cover the oracle skip of a materialized-skill PENDING
marker; `chat --skill` workspace materialization is covered by `internal/skills/embedded_test.go`).

---

### P49.1 — Import/dependency edges in the repository map (repo-map batch head)

**Shipped 2026-07-29.** `internal/repomap` produced a flat file→symbol list with **no edges** — an
agent asking "who uses this?" still had to grep. The map now carries per-file import edges, extracted
from the same file bytes `Build` already reads (regex-cheap, inside the no-CGo single-binary
constraint):

- `FileEntry` gained `Imports []string`. New per-language extractors sit beside `langPatterns`: Go
  (single `import "x"` **and** block `import ( … )`), Python (`import a.b`, `from a.b import`,
  dotted-relative `from .pkg` / `from ..pkg`), JS/TS (`import … from "x"`, bare `import "x"`,
  `export … from "x"`, `require("x")` / dynamic `import("x")`), Rust (`use`), Ruby
  (`require`/`require_relative`). Module-local Go imports resolve to the repo-relative **package
  directory** (module prefix stripped from `go.mod`); `./`/`../` and `require_relative` specifiers
  resolve against the importing file's directory; anything escaping the repo root or naming a
  third-party/stdlib package stays a bare token (still useful signal, cheap to filter). Edges are
  deduped and capped at 40 per file so a generated file can't dominate the budget.
- `Render` emits a compact `→ a, b, c` line **after** each file's symbols, and a tight byte budget
  drops the edge line first — symbols are never dropped while a file's entry is kept.
- A new `schemaVersion` constant (v2) is mixed into the fingerprint in both `Build` and the
  stat-only `fingerprint` freshness check, so an edge-less v1 `.aegis/repomap.json` is reported
  stale and rebuilds rather than loading without edges. No new command surface — `aegis index`,
  `POST /repomap/index`, and the `/index` TUI refresh flow through `Build`/`Render` unchanged.
- Added `LoadOrBuild(root, cachePath, opts) (*Map, error)` — the parsed-`Map` analog of `Load`
  (which returns only rendered text), reusing the fresh on-disk cache or rebuilding+refreshing it.
  It backs the P49.2 tool so map/skeleton/importers read the same cache the injector writes.

*Tested:* `internal/repomap/repomap_test.go` adds Go-module-local + blank-underscore edge resolution,
relative JS/Python resolution (including an escaping `..` import kept raw), schema-version cache
invalidation, the drop-edges-before-symbols budget rule, and `LoadOrBuild` cache-reuse/rebuild.
Verified live: `aegis index` on this repo resolves `internal/cli`/`internal/api` to repo-relative
dirs while keeping `fmt`/`net/http` bare.

---

### P49.2 — On-demand `repomap` query tool (skeleton / importers / map)

**Shipped 2026-07-29.** The repo map was a single always-injected `<repo_map>` block, hard-capped at
4000 bytes under a local-prompt profile — so on a large repo the model got a *truncated* map with no
way to ask for more. Following Aegis's existing progressive-disclosure pattern (skills, `tool_search`),
a new read-capability builtin tool `repomap` is registered **deferred** (name+description only,
alongside `diagram`/`latex`) so it costs ~nothing until the model pulls it via `tool_search`:

- `action:"map"` — the whole cached map at a large budget (200KB, vs the 4000-byte injected slice),
  optionally filtered by a `path.Match` glob.
- `action:"skeleton", path:<file>` — one file's symbols and P49.1 import edges without a `read` on the
  file body; a clean non-error message when the path isn't an indexed source file.
- `action:"importers", path:<file>` — the reverse "who uses this / blast radius" query over P49.1's
  edges. Because P49.1 resolves Go imports to the package *directory* and JS/TS/Python relative
  imports to extension-less paths, `edgeRefersTo` matches an edge against the target's exact path,
  its extension-stripped path, **or** its containing directory — so querying importers of any file in
  a Go package returns everything importing that package.

`Capability() → CapRead` (runs concurrently in `engine.runTools`), backed by `repomap.LoadOrBuild`
against the same `.aegis/repomap.json` the injector uses — no new daemon state, HTTP surface, or
config. Path normalization deliberately avoids `ValidatePath`'s `EvalSymlinks` (which would diverge
from the un-resolved keys `Build` produces, e.g. macOS `/var`→`/private/var`) while still rejecting
`../` escapes.

*Tested:* `internal/tool/builtin/repomap_test.go` covers all three actions across a Go+TS fixture —
map, glob-filter (+empty), skeleton (+non-source +missing-path), importers (Go package-dir case, JS
extension-less case, +none), and unknown/empty action. Verified live against this repo: `importers` of
`internal/repomap/repomap.go` correctly returns every file importing the `internal/repomap` package.

**Not built — P49.3 / P49.4 (Tier 4, measure-first).** P49.3 (LSP-backed symbol precision via
`internal/lsp` `documentSymbol`/`references`) and P49.4 (an opt-in `aegis index --semantic` LLM
concept-node pass) remain open and were deliberately left unbuilt: both are Tier-4 measure-first with
no live-run trigger, and the roadmap gates them on the structural tier (P49.1/P49.2) demonstrably
failing to close the discovery gap first — P49.4 additionally being unsettled on whether concept nodes
belong in a new store or extend `knowledge`/`memory`.

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

