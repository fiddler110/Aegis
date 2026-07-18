# Aegis Capability Roadmap

**Last updated:** 2026-07-18 (P35.1-P35.7 shipped)

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 1 — the parked item **P25.9**. **P35.7** shipped 2026-07-18 (see
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
P35 fixes applied, to verify closure. Two things happened. First, a *fifth* stacked blocker
surfaced and was fixed on the spot (`aegis chat` registered the built-in `skill` tool but never
injected the `<skills_available>` index into its system prompt, so the model couldn't discover the
skill to load it — the daemon path does this via `skills.BuildIndex`, the CLI path didn't; fix in
`internal/cli/chat.go`, shipped separately). Second, with discovery fixed the model *did* load the
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
whether reuse is *actually* happening in practice remains unconfirmed, and P35.5's "raise the
ceiling" vs. "make prefill cheap" question stays open pending a live run with the new
instrumentation.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.  
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.  
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).  
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** 0 open. (P35.5 shipped 2026-07-18 — see [releases.md](releases.md#latest-changes);
P35.1, P35.2 shipped 2026-07-18; P33.1 and P33.2 shipped 2026-07-15;
P31.1, P31.2, P30.1-P30.3 shipped 2026-07-14; P32.1-P32.4 shipped 2026-07-15.)

---

## Open Work — Tier 2

**Status:** 0 open. (P35.6 shipped 2026-07-18 — see [releases.md](releases.md#latest-changes);
P35.3 shipped 2026-07-18; P34.12 shipped 2026-07-17; P34.9 and P34.10 shipped 2026-07-17;
P34.5-P34.8 shipped 2026-07-17; P34.3 shipped 2026-07-16; P34.2 shipped 2026-07-16, both levers;
P34.1 shipped 2026-07-16; P34.4 shipped 2026-07-16; P33.13, P33.14, P33.15, P33.17, P33.18 shipped
2026-07-16; P33.3-P33.8 shipped 2026-07-15; P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14;
P32.5-P32.7 shipped 2026-07-15.)

Worth a look for a future item: the same "accurate refusal, error-shaped" question for the other
scanners' documented exit codes, noted while shipping P35.6. P34.6 checked the *language*-targeted
tools; nothing has swept the SCA/secrets tools for non-zero exits that mean "nothing to do" rather
than "I broke".

---

## Open Work — Tier 3

**Status:** 0 open. (P35.7 shipped 2026-07-18 — see [releases.md](releases.md#latest-changes);
P35.4 shipped 2026-07-18; P33.10, P33.11, P33.16, P33.19 shipped 2026-07-17;
P32.8 shipped 2026-07-15; P33.9, the keystone that unblocked P33.10 and P33.19, shipped
2026-07-16.)

---

## Open Work — Tier 4

One item parked — P25.9. Low urgency, no trigger, or explicitly parked pending demand. Do not
build speculatively — revisit only if a concrete trigger appears, and check with the user before
starting.
(P33.20 shipped 2026-07-17 alongside P33.11 — its message-allowlist fix was implemented as part of
that work; P32.9-P32.11 shipped 2026-07-15; P33.12, P33.21, and P33.22 shipped 2026-07-17, see
releases.md.)

### P25.9 — Per-session scoping of `lsp.Manager`

Effort: L — parked, no concrete trigger

The P25.1 deliberately-deferred gap list originally named six daemon-wide singletons; five
shipped (see [releases.md](releases.md#latest-changes)): `knowledge.Store`, `longmem.Store`, the
cached repo-map, persona/agent-def directory discovery, and the `os` sandbox backend's
write-confinement profile are all now session-Workdir-aware. `lsp.Manager` alone remains parked —
re-scoping it means starting a second set of real language-server subprocesses per distinct
session root, with no natural bound (P25.8 already threads Workdir through cron/swarm/debate, so
a long-lived daemon could accumulate many distinct roots). That's either an unbounded resource
leak (no cap) or a new eviction/restart failure surface (capped LRU) for a narrower benefit than
the other five. Trigger: a concrete pain point in a future live-eval pass, or a deliberate design
for capped/LRU per-root manager pooling.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
