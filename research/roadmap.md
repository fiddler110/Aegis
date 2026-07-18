# Aegis Capability Roadmap

**Last updated:** 2026-07-18 (P35.1-P35.4 shipped; P35.5-P35.7 filed from the verification pass)

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 4 — the one parked item **P25.9**, plus **P35.5-P35.7**, a new cluster filed
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
root-cause diagnosis. The `keep_alive` residency from P35.4 was confirmed working live
(`ollama ps` showed the model resident with `CONTEXT 131072`), so P35.7 exists to establish whether
that residency is actually sparing per-turn prefill or whether prefill is being reprocessed in full
every turn — which decides whether P35.5's real fix is a longer timeout or genuine cache reuse.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.  
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.  
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).  
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** 1 open — **P35.5**. (P35.1, P35.2 shipped 2026-07-18 — see
[releases.md](releases.md#latest-changes); P33.1 and P33.2 shipped 2026-07-15;
P31.1, P31.2, P30.1-P30.3 shipped 2026-07-14; P32.1-P32.4 shipped 2026-07-15.)

### P35.5 — Native-Ollama agentic runs die on the shared 5-minute response-header timeout

Priority: Tier 1 — real robustness gap; blocks the doctor-recommended native-Ollama
threat-model/skill flow from ever completing on a slower local box.

Effort: M

A live `/threat-model stride` run (P35 verification pass, 2026-07-18) on the doctor-recommended
native-Ollama setup (`provider.default: ollama`, qwen3.6:35b-a3b-fast, `context_window: 131072`,
`keep_alive` resident per P35.4) reproducibly dies mid-exploration with
`ollama: request failed: … net/http: timeout awaiting response headers`, before writing any report
file. It got 5 turns / 27 tool calls / ~62k input tokens deep — further than the pre-P35.3 run, but
still a hard failure. Cause: `internal/provider/sse/sse.go:30` hardcodes
`responseHeaderTimeout = 5 * time.Minute`, shared by *every* adapter via `NewStreamingClient` and
configurable nowhere. Ollama withholds the HTTP response header until prompt-eval (prefill)
completes, so on a large local context a legitimately-slow prefill trips the cap and the whole turn
is aborted as a transport error. Fix options, cheapest first: (a) make it configurable
(`provider.response_header_timeout`, default unchanged) so a slow box can raise it; (b) scale the
default with `context_window` / `LocalPromptProfile`; (c) reduce per-turn context growth so the
prefill stays cheap (stronger bounded-read discipline; overlaps P35.4's skill half — the model
still read 23 files here). Whether raising the ceiling is the real fix or a band-aid depends on
**P35.7**: if prefill is being fully reprocessed every turn, the win is cache reuse, not a longer
timeout. Pairs with **P35.6** (make the timeout error actionable when it does fire).

---

## Open Work — Tier 2

**Status:** 1 open — **P35.6**. (P35.3 shipped 2026-07-18; P34.12 shipped 2026-07-17 — see
[releases.md](releases.md#latest-changes); P34.9 and P34.10 shipped 2026-07-17; P34.5-P34.8
shipped 2026-07-17; P34.3 shipped 2026-07-16; P34.2 shipped 2026-07-16, both levers; P34.1
shipped 2026-07-16; P34.4 shipped 2026-07-16; P33.13, P33.14, P33.15, P33.17, P33.18 shipped
2026-07-16; P33.3-P33.8 shipped 2026-07-15; P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14;
P32.5-P32.7 shipped 2026-07-15.)

### P35.6 — Rewrap the response-header-timeout error to be actionable

Priority: Tier 2 — cheap, self-contained; mirrors P35.2's actionable-error precedent.

Effort: S

When P35.5's timeout fires the surfaced error is the bare Go transport string
(`net/http: timeout awaiting response headers`) — indistinguishable from a dead server and naming
no remedy. P35.2 set the precedent for the other local-model failure mode (context truncation):
detect the signal, raise an actionable, correctly-(non-)retryable error naming the lever. Do the
same here — detect the response-header timeout on the native-Ollama / OpenAI-compat paths and
rewrap it to explain the likely cause (context too large / prefill slower than the header-timeout
budget on a local backend) and the levers (raise `provider.response_header_timeout` per P35.5,
lower `context_window`, reduce per-turn context growth). Non-retryable — a blind retry just
re-processes the same oversized prefill and times out again. Small enough to land alongside P35.5.

Worth a look for a future item: the same "accurate refusal, error-shaped" question for the other
scanners' documented exit codes. P34.6 checked the *language*-targeted tools; nothing has swept
the SCA/secrets tools for non-zero exits that mean "nothing to do" rather than "I broke".

---

## Open Work — Tier 3

**Status:** 1 open — **P35.7**. (P35.4 shipped 2026-07-18 — see
[releases.md](releases.md#latest-changes); P33.10, P33.11, P33.16, P33.19 shipped 2026-07-17;
P32.8 shipped 2026-07-15; P33.9, the keystone that unblocked P33.10 and P33.19, shipped
2026-07-16.)

### P35.7 — Confirm/instrument inter-turn KV-cache reuse on the native Ollama path

Priority: Tier 3 — root-cause diagnostic that decides the right fix for P35.5; sequence-first.

Effort: M

P35.4 kept the model resident across turns (`keep_alive` 30m default; verified live via
`ollama ps`), on the premise that Ollama's native `/api/chat` reuses its KV-cache prefix across
requests while the model stays resident. But the P35.5 timeout — hit only after the context grew to
~62k tokens over 5 turns — is equally consistent with prefill *not* being spared: each turn
reprocessing the whole growing conversation from scratch, prefill time climbing with context until
it crosses the 5-minute ceiling. The adapter already reads `prompt_eval_count` and `load_duration`
(`internal/provider/ollama/ollama.go:322,411`); add `prompt_eval_duration` and log per-turn prefill
token-count and duration. The tell: on turn N+1, does `prompt_eval_count` drop to roughly the
newly-appended delta (cache hit) or stay at the full running total (full reprocess)? If reuse is
*not* happening, find why the sent prefix stops matching Ollama's cached one — candidates: thinking
blocks round-tripped into history changing the byte-exact prefix, tool-result formatting, or the
system prompt being regenerated non-deterministically per turn — and stabilize it. That, not a
longer timeout, is the real fix and the intended payoff of P35.4; it feeds P35.5's choice between
"raise the ceiling" and "make prefill cheap."

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
