# Aegis Capability Roadmap

**Last updated:** 2026-07-21 (**P38.3 and P38.5 shipped** — both Tier 3. P38.3: per-turn usage
(`InputTokens`/`OutputTokens`/cache counts/`PromptEvalDurationMS`) is now on the wire for both the daemon
SSE `turn_done` event and `aegis chat --output-format stream-json`'s `turn_done` line, closing the gap
where the latter carried no usage at all. P38.5: the native Ollama adapter now retries once with `think`
omitted (and warns) when a model 400s on the parameter entirely, instead of aborting the run. See
[releases.md](releases.md#latest-changes). Earlier 2026-07-20: **P38.4 shipped** — `scaffold.py`
pre-writes all seven threat-model files from the skeletons, real structure + `<!-- PENDING -->` per
fillable section, so a weak model fills sections instead of authoring structure it gets wrong; a
freshly-scaffolded suite lints 6/6 and a filled one verifies 9/9. This closes the conformance gap the
qwen3:14b live test exposed and unblocks P38.1's re-test. Earlier: **P38.2 shipped** — `aegis chat
--skill <name>` preloads a skill body and drives it to completion while `<!-- PENDING -->` markers
remain — and the **P38.1 linear build was live-tested** on qwen3:14b vs AiGateway: mechanism
**confirmed** but output did not conform, which P38.4 fixed. P38.1 is Tier 2 (mechanism done;
conformance re-test now unblocked by P38.4). Earlier: the threat-modeling skill was reworked to the
non-orchestrated linear build (SKILL.md + verification-and-updates.md); orchestration parked. Earlier:
P37.6 + two P37-script fixes shipped; P37.1-P37.5 shipped — threat-model suite scripting complete;
P36.1-P36.3 shipped; P35.1-P35.13 shipped)

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 1 — **P38.1** (Tier 2, now unblocked), plus the parked **P25.9** (Tier 4). **P38.3** and
**P38.5** (Tier 3) shipped 2026-07-21 — see [releases.md](releases.md#latest-changes).

The active focus is now **P38.1's conformance re-test**, which **P38.4 just unblocked**. P38.4
(deterministic skeleton scaffolding, `scaffold.py`) shipped 2026-07-20 — see
[releases.md](releases.md#latest-changes): a stdlib script pre-writes all seven threat-model files *from
the skeletons* with real structure (headings, table headers + separators, fixed-value lists, the DFD's
`flowchart LR` + three `classDef`s) and a `<!-- PENDING -->` marker per fillable section, so the model
**fills sections** (`edit_file`) instead of authoring the structure the 14B model got wrong. It closed the
exact gap the 2026-07-20 live test exposed (qwen3:14b, via the newly-shipped **P38.2** `aegis chat --skill`
drive-to-completion): that test **confirmed the P38.1 linear build's mechanism** — one context, no
sub-agents, **no `{mode,agents}` mis-route** (the P36.3 failure), `recon.py` → all seven files → the P37
scripts, inside the window (~44K input tokens / 33 tool calls) — **but the output did not conform** because
the 14B model never loaded the skeleton/`output-formats` templates and re-authored freeform structure each
pass. With scaffolding, a freshly-scaffolded suite lints 6/6 and a minimally-filled one verifies 9/9, so
self-correction now converges against a real structure. **P38.1**'s remaining work is the re-test itself:
confirm a verify-clean seven-file build on a capable-enough local model.
**P38.5** (models that reject `think` should degrade, not 400) came out of the same test: mythos-sec:24b
400'd on `think` and, with it disabled, proved too weak a tool-caller to matter (it invents tool names and
doesn't substitute command placeholders — a model-quality dead end, not a skill problem). It shipped
2026-07-21 (adapter-level retry-without-think + warning) — see [releases.md](releases.md#latest-changes);
the underlying model-quality dead end is unaffected. Already confirmed from the run: **P36.1**
(deterministic skill load), **P37.1** (`recon.py`), and now the P38.1 linear-build mechanism and P38.2.

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

**Status:** 0 open. (**P38.4** shipped 2026-07-20 — deterministic skeleton scaffolding, `scaffold.py`; see
[releases.md](releases.md#latest-changes). P38.2 shipped 2026-07-20; P36.1 shipped 2026-07-19;
P35.9 shipped 2026-07-18;
P35.5 shipped 2026-07-18; P35.1, P35.2 shipped 2026-07-18; P33.1 and P33.2 shipped 2026-07-15;
P31.1, P31.2, P30.1-P30.3 shipped 2026-07-14; P32.1-P32.4 shipped 2026-07-15.)

The remaining threat-modeling work — the P38.1 conformance re-test that P38.4 unblocked — is tracked in
Tier 2 below (it's a live-run verification, not independent build work).

---

## Open Work — Tier 2

**Status:** 1 open — **P38.1** (non-orchestrated linear threat-model build; rework shipped 2026-07-20,
mechanism live-confirmed, conformance re-test now unblocked by P38.4). (P38.4, P38.2 shipped 2026-07-20 —
see [releases.md](releases.md#latest-changes); P37.2, P37.3 shipped 2026-07-19; P35.13 shipped 2026-07-19; P35.10 and P35.11 shipped 2026-07-18;
P35.6 shipped 2026-07-18;
P35.3 shipped 2026-07-18; P34.12 shipped 2026-07-17; P34.9 and P34.10 shipped 2026-07-17;
P34.5-P34.8 shipped 2026-07-17; P34.3 shipped 2026-07-16; P34.2 shipped 2026-07-16, both levers;
P34.1 shipped 2026-07-16; P34.4 shipped 2026-07-16; P33.13, P33.14, P33.15, P33.17, P33.18 shipped
2026-07-16; P33.3-P33.8 shipped 2026-07-15; P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14;
P32.5-P32.7 shipped 2026-07-15.)

### P38.1 — Non-orchestrated, single-context threat-model build (primary path for local models)

**The rework shipped 2026-07-20** (see [releases.md](releases.md#latest-changes)): the threat-modeling
skill's primary build is now a single-context linear build the driving model runs itself — no sub-agents,
no `agent`-tool orchestration. SKILL.md and `references/verification-and-updates.md` were de-orchestrated
(the §4.2 `agent`/`mode:"sequential"` call block, the terse-final-answer contract, and the shared-pool
time budget all removed; phase ordering and per-file structure kept; the debate step reframed as a
standalone `agent` `mode:"debate"` call at depth 1). Context stays bounded, without phasing, by levers
that already exist (SKILL.md §4): `recon.py`'s ~11KB digest, P36.2 pruning of spent write/read payloads,
incremental section-at-a-time writes, and the deterministic P37 scripts. This closes out the abandoned
phased-orchestration approach (P36.3), which no tested local model could drive — qwen3:14b sprayed the
`{mode,agents}` payload onto whatever tool it emitted and a first fix (SKILL.md callout + `skill`-tool
guard) didn't help.

**Live-test result (2026-07-20, qwen3:14b vs AiGateway, via P38.2 drive-to-completion):**
- ✅ **Mechanism confirmed.** One context, no orchestration, **no `{mode,agents}` mis-route** — the model
  loaded the (preloaded) skill, ran `recon.py`, wrote all seven files itself, and ran `verify.py`,
  `lint_dfd.py`, `inventory.py`. The P36.3-era failure that killed every prior run is gone.
- ✅ **Stayed inside context.** ~44K input tokens over 33 tool calls with no overflow — a partial live
  confirmation of the P36.2-pruning + recon bound (the run was too short to stress compaction hard, so
  this is "held for a 33-call run", not "proven for the worst case").
- ❌ **Output did not conform** — the 14B model skipped the skeleton templates, so `verify.py` failed 6/10
  and it couldn't self-converge. **That gap was P38.4, now shipped** (deterministic skeleton scaffolding,
  `scaffold.py`): the seven files are pre-written from the skeletons with real structure + `<!-- PENDING -->`
  per fillable section, so the model fills sections instead of authoring structure. A freshly-scaffolded
  suite lints 6/6 and a filled one verifies 9/9 — the remaining work here is the live re-test that this
  actually carries the 14B (or a stronger local) model to a verify-clean suite end-to-end.
- mythos-sec:24b is **not viable** independent of the skill: it 400s on `think` (→ P38.5) and, with that
  disabled, invents tool names and doesn't substitute command placeholders — a model-quality dead end.

Priority: Tier 2 — the mechanism is done and live-confirmed and the conformance blocker (P38.4) has
shipped; what remains is the re-test to a **verify-clean** suite on a capable-enough local model. Not Tier 1
because this is a live-run verification, not independent build work — the whole build path now exists.

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

**Status:** 0 open. **P38.3** (peak-context telemetry not externally observable) and **P38.5** (models
that reject `think` fail with a raw 400) both shipped 2026-07-21 — see
[releases.md](releases.md#latest-changes). (P37.4, P37.5 shipped 2026-07-19; P36.3 shipped 2026-07-19;
P36.2 shipped 2026-07-19;
P35.7 shipped 2026-07-18;
P35.4 shipped 2026-07-18; P33.10, P33.11, P33.16, P33.19 shipped 2026-07-17;
P32.8 shipped 2026-07-15; P33.9, the keystone that unblocked P33.10 and P33.19, shipped
2026-07-16.)

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
>
> **Update 2026-07-20 (linear-build live test):** a completed P38.1 linear run finally happened
> (qwen3:14b vs AiGateway, driven by P38.2). **P36.2 is now partially confirmed live** — the run wrote all
> seven files and ran the check scripts inside the window (~44K input tokens / 33 tool calls, no
> overflow). It's "held for a 33-call run", not "proven for the worst case": qwen3:14b couldn't produce a
> *conforming* suite (→ P38.4), so the run was shorter than a clean build would be. The definitive P36.2
> confirmation now rides on the P38.4-scaffolded re-test, measured through P38.3.

**Note (lead, not yet filed):** mythos-sec:24b's failure mode is pure shell-invocation incompetence —
**reconfirmed in the 2026-07-20 P38.1 linear-build test** (with `think` disabled per P38.5): it invented
tool names (`recon.py` and `execute` as if they were tools), ran the skill's literal placeholder command
`` <path>/recon.py <workspace-root>`` without substituting, used wrong paths (`D:\…\recon.py` — the script
lives under the materialized `builtin-skills/` path, not the target), and doubled a directory name. The
earlier run showed the same class (no `python` prefix, bad relative paths, invented `execute_command`/
`run_tool`, looping until the loop detector fired). Mostly model quality, but it hints the shell tool's
error messages could coach better (when a bare `*.py` command fails on Windows, suggest `python <path>`;
when an unknown tool name is called, name the real one). Worth a small usability item if other local
models show the same flailing — but note mythos-sec:24b itself is a dead end regardless.

---

## Open Work — Tier 4

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
