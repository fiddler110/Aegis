# Aegis Capability Roadmap

**Last updated:** 2026-07-20 (**threat-modeling skill pivoted to a non-orchestrated, single-context
linear build** as its primary path — P38.1, Tier 1 — after live runs showed no local model can drive the
phased sub-agent orchestration; orchestration parked. P38.2/P38.3 refocused as its dependencies. Earlier
today: P36 live-verification attempted on qwen3:14b — P36.1 & recon confirmed, P36.3 refuted; P38.1-P38.3
filed; P37.6 + two P37-script fixes shipped. Earlier: P37.1-P37.5 shipped — threat-model suite scripting
complete; P36.1-P36.3 shipped; P35.1-P35.13 shipped)

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 4 — **P38.1** (Tier 1), **P38.2** (Tier 2), **P38.3** (Tier 3), plus the parked
**P25.9** (Tier 4).

The active focus is **P38.1: make the threat-modeling skill actually work on the local models in use, via
a non-orchestrated single-context linear build** — the model works the phases itself in one context and
writes all seven files, with no sub-agents and no `agent`-tool orchestration. This is a deliberate pivot
(2026-07-20): three live runs proved that no tested local model (qwen3:14b, mythos-sec:24b) can drive the
phased sub-agent orchestration P36.3 shipped — qwen3:14b sprays the `{mode,agents}` payload onto whatever
tool it emits (`skill`, then `ls`) and hand-writes an incomplete suite with a false completion claim, and
a first fix (SKILL.md callout + `skill`-tool guard) didn't help. So **orchestration is parked** and the
linear build is the primary path. The context bounding that phasing was meant to provide comes instead
from levers that already exist: `recon.py`'s digest, P36.2 pruning, incremental section-by-section writes,
and the deterministic P37 scripts. **P38.2** (one-shot `aegis chat` must not yield mid-build) and
**P38.3** (per-turn context telemetry) are now P38.1's direct dependencies — a linear build is many turns
in one session, so it has to be driven to completion and its context growth has to be measurable.
Confirmed-and-done from the live run: **P36.1** (deterministic skill load) and **P37.1** (`recon.py`).

The **P37.1-P37.5** threat-model
suite-scripting batch shipped 2026-07-19 (see [releases.md](releases.md#latest-changes)): five bundled
stdlib scripts that codify the mechanical parts of the threat-modeling skill and leave judgment to the
model. **P37.1** (`recon.py`) replaces the exhaustive workspace read with one deterministic digest pass
(git/langs/deps/bind-sites/env-keys/security-infra signals/component candidates), cutting a ~500-file
repo's phase-1 gathering from megabytes of raw source to a ~11KB digest and making component ids /
deployment class / `inventory.yaml` ids stable across runs. **P37.2** (`inventory.py`) generates and
`--check`-validates the `inventory.yaml` sidecar deterministically (deriving each tier from its
prerequisite), killing the sibling skill's two most-cited failure modes (truncated arrays, field-name
drift). **P37.3** (`verify.py`) mechanizes the phase-6 cross-file self-check (name consistency,
threat↔coverage bijection, tier/prerequisite, counts). **P37.4** (`lint_dfd.py`) validates the DFD's
Mermaid conventions and `.mmd`↔`.md` equality. **P37.5** (`diff_inventory.py`) diffs two sidecars for
the update workflow's Changes Since Baseline section. Together they lift the Aegis builtin past the
`.claude/skills/threat-model-analyst` sibling it was benchmarked against. The whole **P36.1-P36.3**
batch shipped
2026-07-19, all filed the same day from a `/threat-model stride` dogfooding session against this repo
on a local-model (Ollama) setup, with one shared **live-verification debt** carried forward (see the
note under Tier 3 — the batch landed without an Ollama server available to confirm the token-growth
and peak-context wins live).

**P36.1-P36.3 shipped 2026-07-19** (see [releases.md](releases.md#latest-changes)). **P36.1** (Tier 1)
— the model skipped the `skill` tool call entirely, wandered into a plain directory listing of the
just-materialized `.aegis/skills/threat-modeling/` folder, and lost the original instruction; fixed by
making the initial skill-body load deterministic (slash commands now inject the body server-side
instead of relying on a tool round-trip). **P36.2** (Tier 3) — the per-turn context-growth question
P35.5 explicitly scoped out is now addressed: `write_file`/`edit_file` payloads and one-time
skill-reference reads are pruned by `compaction.pruneStaleToolResults` in the pre-`keepRecent` prefix.
**P36.3** (Tier 3) — the threat-modeling skill's build stages are now phased through the `agent` tool's
`mode: "sequential"` workflow (each phase in a fresh, isolated sub-agent context, only terse stable
identifiers threaded forward) instead of one long-lived, ever-growing run, bounding peak context per
request on local models. **P35.13**
shipped 2026-07-19: its
doc/comment corrections and the `--first-init` native-adapter default landed 2026-07-18, and its
final open piece — the summed-token-surface decision — was resolved 2026-07-19 as "tokens
processed" (the correct cloud-cost basis; see Tier 2 below). **P35.12** shipped 2026-07-18 (native-Ollama
stream cosmetics) and **P35.8** shipped 2026-07-18 as exit-trace instrumentation for `aegis chat`
(see [releases.md](releases.md#latest-changes)). **P35.10** and
**P35.11** shipped 2026-07-18 (see [releases.md](releases.md#latest-changes)), closing out Tier 2.
**P35.9** shipped 2026-07-18 (see
[releases.md](releases.md#latest-changes)), the last Tier 1 item. **P35.7** shipped 2026-07-18 (see
[releases.md](releases.md#latest-changes)), closing out the P35.5-P35.7 cluster. P35.5-P35.7 were a cluster filed
2026-07-18 from the verification pass that followed the P35.1-P35.4 batch. P35.1-P35.4 were all
filed the same day from one live dogfooding pass: running the threat-modeling skill's
`/threat-model stride` flow against an external repo (a small ~15-file Python project, not this
one) on the local-model setup `aegis doctor` itself recommends (Ollama, qwen3.6:35b-a3b-fast). The
run never produced a completed threat-model suite — it died partway through the mandatory
workspace-exploration step every time, for four distinct, stacked reasons. **All four shipped
2026-07-18**: P35.1-P35.3 (the three surface-cleanly-then-fix items) plus both halves of P35.4 —
the skill-level bounded-read guidance and the provider-side keep-alive residency that lets a
native-Ollama run reuse its KV cache across turns. For the shipped-batch history and the lessons
drawn from each (P33/P34 diagnosis accuracy, threat-model closure surfaces, live-verification
findings), see [releases.md](releases.md#latest-changes) — that history has been consolidated
there so this document stays limited to what's actually open.

**The P35.5-P35.7 cluster** came out of re-running that same `/threat-model stride` flow with the
P35 fixes applied, to verify closure. Two things happened. First, a _fifth_ stacked blocker
surfaced and was fixed on the spot (`aegis chat` registered the built-in `skill` tool but never
injected the `<skills_available>` index into its system prompt, so the model couldn't discover the
skill to load it — the daemon path does this via `skills.BuildIndex`, the CLI path didn't; fix in
`internal/cli/chat.go`, shipped separately). Second, with discovery fixed the model _did_ load the
skill and explore properly — 5 turns, 27 tool calls, ~62k input tokens deep — and then still died,
this time on the native adapter's hardcoded 5-minute HTTP response-header timeout during a
large-context prefill, before writing any report file. P35.5-P35.7 are that timeout and its
root-cause diagnosis. **P35.5 shipped 2026-07-18**: `provider.response_header_timeout` (seconds)
now lets a slow-prefill local box raise the ceiling, defaulting unchanged at 5 minutes — fix option
(a) from the filing; scaling the default with context (b) and shrinking per-turn context growth (c)
remain out of scope for that item. **P35.6 shipped 2026-07-18**: the bare Go transport string a
response-header timeout used to surface as (`net/http: timeout awaiting response headers`) is now
rewrapped, on the native-Ollama and OpenAI-compat paths, into an actionable, non-retryable error
naming the cause and the levers — mirroring P35.2's context-truncation precedent. The `keep_alive`
residency from P35.4 was confirmed working live (`ollama ps` showed the model resident with
`CONTEXT 131072`), so P35.7 existed to establish whether that residency is actually sparing per-turn
prefill or whether prefill is being reprocessed in full every turn — which decides whether a longer
timeout or genuine cache reuse is the durable fix. **P35.7 shipped 2026-07-18** as instrumentation
plus a code-reading pass over the three named non-determinism candidates: `prompt_eval_duration`
is now read off the wire and logged every turn (`prompt_eval_count`/`prompt_eval_duration_ms`)
alongside the existing `prompt_eval_count`/`load_duration` telemetry, so a live run can compare
turn N vs. N+1 and read off cache-hit-vs-full-reprocess directly. The code-reading pass found no
confirmed bug in any of the three candidates — thinking blocks are round-tripped into
`Conversation.Messages` but the native-Ollama `translate()` (`internal/provider/ollama/ollama.go`)
has no case for `ThinkingBlock` in its assistant-message switch, so they're silently and
consistently dropped on every re-serialization, not a source of drift; tool-result content is
re-emitted byte-for-byte from stored `ToolResultBlock.Content`, no reformatting on replay; and the
system prompt (`Server.effectiveSystem`, `internal/server/helpers.go`) is rebuilt fresh every turn
but every constituent (persona blocks, memory/context files, the skills index, the sorted deferred-
tools list, the sorted tool schema list) is either static or deterministically sorted, with no
timestamp or nonce found anywhere in the chain — given unchanged underlying files/config it should
render byte-identical turn over turn. This is a code-reading conclusion, not a live one: no Ollama
server was available this session to actually observe `prompt_eval_count` behavior across turns, so
whether reuse is _actually_ happening in practice remains unconfirmed, and P35.5's "raise the
ceiling" vs. "make prefill cheap" question stays open pending a live run with the new
instrumentation.

**P35.9-P35.12** were filed 2026-07-18 from a code-review pass over the whole P33.9-P35.7
native-Ollama body of work (adapter, factory wiring, timeout/error handling, health probing,
telemetry). The headline finding was P35.9: the native adapter mints tool-call IDs from a counter
that resets every request, so IDs collide across turns, historical tool results got re-labeled
with the wrong `tool_name` on replay, and the serialized prompt prefix mutated between requests —
a missed fourth cache-invalidation candidate that P35.7's code-reading pass didn't catch, and one
that intermittently defeated the P35.4 KV-cache reuse whenever consecutive turns led with
different tools. **P35.9 shipped 2026-07-18** (see [releases.md](releases.md#latest-changes)):
`translate` now resolves each tool result against the nearest preceding tool-use by walking
messages in order, instead of a whole-history ID→name map — fixes both the mislabelling and the
cache churn without touching how IDs are minted. P35.10-P35.12 were the smaller observations from
the same pass; **P35.10** (InputTokens semantics), **P35.11** (status-probe caching), and
**P35.12** (error-fallback cleanup + an actionable over-4MiB-line error on the native path) all
shipped 2026-07-18.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.  
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.  
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).  
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** 1 open — **P38.1** (non-orchestrated, single-context threat-model build — the primary path
for local models; filed 2026-07-20, reframed from the abandoned phased-orchestration approach after the
P36 live-verification run). (P36.1 shipped 2026-07-19 — see [releases.md](releases.md#latest-changes);
P35.9 shipped 2026-07-18;
P35.5 shipped 2026-07-18; P35.1, P35.2 shipped 2026-07-18; P33.1 and P33.2 shipped 2026-07-15;
P31.1, P31.2, P30.1-P30.3 shipped 2026-07-14; P32.1-P32.4 shipped 2026-07-15.)

### P38.1 — Non-orchestrated, single-context threat-model build (primary path for local models)

**Direction (decided 2026-07-20): the threat-modeling skill's primary build is a single-context linear
build the driving model runs itself — no sub-agents, no `agent`-tool orchestration.** The goal is a
skill that actually completes on the local models in use (qwen3:14b and similar), not one that depends on
multi-agent capability those models don't have. The phased sub-agent orchestration (P36.3) is taken off
the critical path (see "Orchestration, parked" below); it is not the local path and is no longer the
skill's default.

*Why the pivot (background):* the P36.3 design had the model drive a six-phase build through the `agent`
tool with `mode:"sequential"`. Three 2026-07-20 live runs against AiGateway proved local models can't do
this: qwen3:14b sprayed the `{mode,agents}` payload onto whatever tool it happened to emit (`skill`
pre-fix, `ls` post-fix) and then hand-wrote a thin suite with 2 `<!-- PENDING -->` stubs and a **false
completion claim**; mythos-sec:24b couldn't even invoke `recon.py` and loop-aborted before orchestration.
A first fix (SKILL.md `agent`-call callout + a `skill`-tool guard, shipped 2026-07-20 — see
[releases.md](releases.md#latest-changes)) did not help, because the misroute target varies and the model
isn't confusing two named tools — it just can't be relied on to spawn. So orchestration is abandoned for
local models rather than patched.

**The deliverable — rework the skill to a linear build:**
- Rewrite SKILL.md so the build is one context working the phases in dependency order (architecture →
  DFD → framework analysis → findings → assessment → self-check), writing all seven files itself. Remove
  the §4.2 `agent`/`mode:"sequential"` dependency and the terse-final-answer contract (both exist only to
  serve orchestration); keep the phase *ordering* and per-file structure.
- **Context stays bounded without phasing** by the levers that were already doing the real work:
  (a) `recon.py` replaces the megabyte architecture-phase reads with an ~11KB digest; (b) P36.2 pruning
  drops stale `write_file`/`edit_file` payloads and one-time skill/reference reads from the running
  context; (c) incremental writes — stub each file, then `edit_file` one section at a time — keep the
  working set small; (d) the deterministic scripts (`inventory.py`, `verify.py`, `lint_dfd.py`) mean the
  model never has to hold the whole analysis in context to produce the sidecar or run the checks.
- Keep incremental, resumable output (the `<!-- PENDING -->` stub-first pattern already in §4.1) so a run
  that stops mid-way resumes cleanly — this matters more now that everything is one long linear run.

**What must be verified (the real open question):** that a full seven-file linear build actually stays
inside the context window on the target local models. That is exactly what the pruning (P36.2) + recon +
incremental-write levers are for, and it needs a live run to confirm — which in turn needs **P38.2**
(one-shot `aegis chat` must not yield mid-build, since a linear build is many turns in one context) and
**P38.3** (per-turn usage telemetry, to see the context curve). P38.2/P38.3 are now direct dependencies
of *this* item, not the orchestration one.

**Orchestration, parked.** The `agent`-tool phased path can stay available for capable (cloud/large)
models, but it is optional and no longer the default; nobody needs to make it work on local models. The
would-be follow-ups from the orchestration approach are demoted to leads, not active work: an
engine-level interceptor for misrouted `mode`/`agents` payloads (minor robustness only, since the linear
build never emits them), and validating the phased path's capability floor on a cloud/>24B model (not a
goal). The mythos-sec shell-invocation-competence observation (below) stands as its own lead.

Priority: Tier 1 — this is the path to a threat-modeling skill that actually works on the local models in
use; everything else about the skill (the P37 scripts, P36.2 pruning) already exists to support exactly
this build.

---

## Open Work — Tier 2

**Status:** 1 open — **P38.2** (one-shot `aegis chat` yields mid-workflow, filed 2026-07-20). (P37.2, P37.3 shipped 2026-07-19; P35.13 shipped 2026-07-19; P35.10 and P35.11 shipped 2026-07-18 — see [releases.md](releases.md#latest-changes);
P35.6 shipped 2026-07-18;
P35.3 shipped 2026-07-18; P34.12 shipped 2026-07-17; P34.9 and P34.10 shipped 2026-07-17;
P34.5-P34.8 shipped 2026-07-17; P34.3 shipped 2026-07-16; P34.2 shipped 2026-07-16, both levers;
P34.1 shipped 2026-07-16; P34.4 shipped 2026-07-16; P33.13, P33.14, P33.15, P33.17, P33.18 shipped
2026-07-16; P33.3-P33.8 shipped 2026-07-15; P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14;
P32.5-P32.7 shipped 2026-07-15.)

### P38.2 — One-shot `aegis chat` yields mid-workflow instead of completing long skill runs

Both 2026-07-20 live threat-model runs ended with the model asking *"Would you like me to proceed? The
process will take approximately 70 minutes"* and returning — the second one *after* an explicit
"this is a NON-INTERACTIVE run: do not stop to ask for confirmation and do not return a partial result".
A one-shot `aegis chat` turn ends when the model yields, and nothing drives a long, multi-phase skill
workflow to completion; the model treats a scripted run as interactive and hands back a stub. This makes
`aegis chat` unsuitable for dogfooding or automating any skill whose work spans many turns (threat
model, deep research, security audit). Options: a `--continue`/`--max-turns` drive-to-completion mode for
chat, an auto-continue when a skill workflow is mid-run with `<!-- PENDING -->` markers still on disk,
or — at minimum — document that multi-phase skills need the interactive/daemon path and have `chat`
say so instead of silently yielding.

This is now a **direct dependency of the P38.1 linear build** and more important than before: a
single-context linear build is *many* turns in one session, so if `chat` yields mid-build the run just
stops with a partial suite (exactly the qwen3:14b failure — it wrote a few files and declared victory).
A working local threat model needs the build to be driven to completion (all `<!-- PENDING -->` markers
gone), whether via a drive-to-completion `chat` mode or the interactive/daemon path. The
auto-continue-while-PENDING-markers-exist option fits the linear build especially well.

Priority: Tier 2 — gates whether the P38.1 linear build can actually finish a run non-interactively; a
bounded drive-to-completion mode is a contained change.

**Note (future item, not yet filed):** the same "accurate refusal, error-shaped" question for the
other scanners' documented exit codes, noted while shipping P35.6 and again while closing P35.13.
P34.6 checked the _language_-targeted tools; nothing has swept the SCA/secrets tools for non-zero
exits that mean "nothing to do" rather than "I broke". No `### P<n>.<m>` heading yet — filed here
as a lead so the status script doesn't treat it as active work.

**Note (future item, not yet filed):** P36.2's write/edit Input-pruning rule covers `write_file` and
`edit_file` but not `multi_edit`, whose nested `edits[]` array (each with `old_string`/`new_string`)
also embeds verbatim file content that survives unpruned. Extending `pruneWriteEditInput` to the
array shape is a mechanical follow-up. No `### P<n>.<m>` heading yet — lead only.

---

## Open Work — Tier 3

**Status:** 1 open — **P38.3** (peak-context telemetry not externally observable, filed 2026-07-20). (P37.4, P37.5 shipped 2026-07-19; P36.3 shipped 2026-07-19 — see [releases.md](releases.md#latest-changes);
P36.2 shipped 2026-07-19;
P35.7 shipped 2026-07-18;
P35.4 shipped 2026-07-18; P33.10, P33.11, P33.16, P33.19 shipped 2026-07-17;
P32.8 shipped 2026-07-15; P33.9, the keystone that unblocked P33.10 and P33.19, shipped
2026-07-16.)

### P38.3 — Per-turn context usage is not externally observable

Confirming that the P38.1 linear build stays inside the context window needs per-turn token numbers, and
the 2026-07-20 runs found none are exposed: `--output-format stream-json`'s `turn_done` events carry
**no usage**, the engine emits **no per-request token log line** at info/debug (grep of
`internal/engine`/`internal/provider` finds none), and one-shot `aegis chat` uses an ephemeral session
store so nothing is queryable afterward. The only figure available was the final aggregate
(`input_tokens` in the `result` event). Add per-turn usage to `turn_done` (and/or a `slog.Debug` line per
provider request with prompt-token count), so the **linear build's turn-over-turn context growth** —
i.e. whether recon + P36.2 pruning + incremental writes actually keep it bounded — is measurable from
the outside without SQLite spelunking.

Priority: Tier 3 — instrumentation, not a user-facing defect; but the P38.1 linear build cannot be
*measured* as staying within context without it (it's how you tell a working build from one that's about
to overflow).

**Note (two doc-inconsistency leads, not yet filed — surfaced while building the P37 scripts):**
(a) **threat-ID form** — `references/skeletons/skeleton-stride.md` writes threat IDs as bare sequential
`T1`/`T2`, but `output-formats.md`'s coverage / Related-Threats examples use composite `T04.S` form;
the P37 scripts match literally and handle both, but the docs should settle on one canonical form.
(b) **inventory YAML style** — `skeleton-inventory.md`'s example is block-style (`- id:` on its own
line) while directive #13 says list entries are one-line, and `inventory.py` emits one-line flow
mappings (`- {id: "T1", ...}`); `diff_inventory.py` parses both, but the skeleton example should match
what the generator produces. Both are cosmetic doc drift, not code bugs. No `### P<n>.<m>` heading yet.

**Note (future item, not yet filed):** `recon.py` (P37.1) depth follow-ups, left out of v1
deliberately and each worth its own item when a concrete need appears: (a) **data-flow edge inference** —
seed the DFD's `DF##` flows from import graphs / client instantiations (which component calls which) so
phase 2 starts from real edges instead of re-deriving them; (b) **config-default resolution** — recon
currently punts a config/flag-driven bind address to the model ("confirm the default"); parsing the
actual default from the config struct / `config.yaml` would settle the deployment class deterministically
in the common case — and (b′) a specialization surfaced by the 2026-07-20 AiGateway eval: when the only
internet-facing evidence is `EXPOSE`/`0.0.0.0` but the k8s `Service` is `NodePort`/`ClusterIP` (not
`LoadBalancer`/`Ingress`) and no TLS terminator is present, recon should downgrade the suggestion to
`internal-network` rather than `internet-facing`; it already parses the compose/k8s files, so it has the
signal to be less alarmist (recon suggested `internet-facing` for AiGateway, whose real class is
`internal-network`); (c) **richer symbol extraction** — functions/methods and route→handler maps,
optionally via `ctags`/tree-sitter when on PATH, falling back to today's regex; (d) **target-commit in
the sidecar** — `inventory.py`'s `git_commit()` runs `git -C <run-dir>`, so when the run directory is
kept outside the target repo (the recommended clean-target setup) the sidecar records *no* commit for
the analyzed code, losing the one field a future diff most wants; let it take an optional
`--target-dir`/`--repo` or read the commit from `0-assessment.md`'s Git Commit row. No `### P<n>.<m>`
heading yet — leads only, so the status script doesn't treat them as active work. (The `inventory.py`
deployment-classification mis-parse and the missing architecture↔analysis classification check that the
same eval surfaced were **fixed** 2026-07-20 — see [releases.md](releases.md#latest-changes).)

> **Live-verification debt (P36.1-P36.3) — attempted 2026-07-20, NOT retired.** A real live run
> finally happened: the rebuilt binary drove `aegis chat` STRIDE threat models of an external target
> (`D:\Development\AiGateway`, a FastAPI AI gateway) against the config-default **qwen3:14b** on the
> native Ollama adapter (32k window). Two runs (a plain prompt, then a forceful "run recon first, use
> the §4.2 sequential agent workflow, produce all 7 files, do not stop to ask"). Results:
> - **P36.1 (deterministic skill load): CONFIRMED.** Both runs' first tool call was
>   `skill{threat-modeling}`; the model never wandered off the instruction.
> - **P37.1 (`recon.py`) live: CONFIRMED.** Run 2 executed it from the materialized
>   `builtin-skills/threat-modeling/recon.py` path — the `go:embed`→materialize→run pipeline works
>   end-to-end on a real model.
> - **P36.3 (phased sequential orchestration): REFUTED on qwen3:14b.** The model **mis-invokes** it:
>   it knows the `{mode:"sequential",agents:[…]}` payload but attaches it to the **`skill`** tool
>   instead of the **`agent`** tool (both are registered/available), which errors — then it bails.
>   The phased path never executes, so its peak-context bound can't even be measured. Both runs
>   produced no usable suite (run 1: 2 shallow files; run 2: 7 `<!-- PENDING -->` stubs).
> - **P36.2 (pruning): still UNTESTED** — both runs ended at ~8 turns, far too short to stress
>   compaction/pruning.
>
> Two reproducible blockers (now filed): **P38.1** (the `skill`/`agent` mis-routing that kills the
> phased workflow on small local models) and **P38.2** (one-shot `aegis chat` yields mid-workflow —
> both runs ended with "Would you like me to proceed? (~70 min)" despite an explicit "do not stop").
> Also **P38.3**: peak-context-per-phase is not externally observable (`turn_done` stream events carry
> no usage, the engine logs no per-request token count, and one-shot chat doesn't persist to the
> session store) — so the P36.3 claim is unmeasurable until that's fixed, independent of whether the
> path runs. Caveat: only qwen3:14b was tested; the doctor-recommended larger MoE
> (qwen3.6:35b-a3b) wasn't on-disk to try and may drive the orchestration correctly — re-testing on it
> is part of closing this. The debt stays open behind P38.1-P38.3.
>
> **Update 2026-07-20 (post-fix):** the P38.1 first fix (SKILL.md `agent`-call callout + `skill`-tool
> guard) was shipped and re-verified with three more live runs — **it did not work.** qwen3:14b
> mis-routed the workflow payload to `ls` (guard never fired) and hand-wrote an incomplete suite with a
> false completion claim; mythos-sec:24b couldn't even invoke `recon.py` and loop-aborted. Neither
> tested local model drives the phased workflow.
>
> **Decision 2026-07-20 — the phased-orchestration approach is abandoned for local models.** This part
> of the debt (P36.3) is not being retired; it is being *superseded* by the **P38.1 non-orchestrated
> linear build**, now the skill's primary path. What still carries forward from this debt is **P36.2**:
> its pruning is now the load-bearing mechanism that must keep a single-context linear build inside the
> window, so verifying P36.2 live (via a completed P38.1 linear run, measured through P38.3) is the
> remaining verification task. P36.1 (deterministic skill load) and P37.1 (`recon.py`) were confirmed
> live and need nothing further. In short: the debt's P36.3 half is closed-by-abandonment; its P36.2
> half folds into P38.1's "what must be verified".

**Note (lead, not yet filed):** mythos-sec:24b's failure mode in the 2026-07-20 run was pure
shell-invocation incompetence in the PowerShell sandbox — it never prefixed `python`, used bad relative
paths (`../recon.py`, `cd /d /c …`), and invented tool names (`execute_command`, `run_tool`), looping
until the engine's loop detector killed it. Some of this is model quality, but it hints the shell tool's
error messages could coach better (e.g. when a bare `*.py` command fails on Windows, suggest the
`python <path>` form; when an unknown tool name is called, name the real one). Worth a small usability
item if other local models show the same flailing.

---

## Open Work — Tier 4

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
