# Aegis Capability Roadmap

**Last updated:** 2026-07-18 (P35.1-P35.7 shipped; P35.9-P35.12 filed)

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 6 — **P35.9** (Tier 1), **P35.10** and **P35.11** (Tier 2), and the parked items
**P25.9**, **P35.8**, and **P35.12** (Tier 4). **P35.7** shipped 2026-07-18 (see
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

**P35.9-P35.12** were filed 2026-07-18 from a code-review pass over the whole P33.9-P35.7
native-Ollama body of work (adapter, factory wiring, timeout/error handling, health probing,
telemetry). The headline finding is P35.9: the native adapter mints tool-call IDs from a counter
that resets every request, so IDs collide across turns, historical tool results get re-labeled
with the wrong `tool_name` on replay, and the serialized prompt prefix mutates between requests —
a missed fourth cache-invalidation candidate that P35.7's code-reading pass didn't catch, and one
that intermittently defeats the P35.4 KV-cache reuse whenever consecutive turns lead with
different tools. P35.10-P35.12 are the smaller observations from the same pass.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.  
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.  
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).  
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** 1 open — P35.9. (P35.5 shipped 2026-07-18 — see [releases.md](releases.md#latest-changes);
P35.1, P35.2 shipped 2026-07-18; P33.1 and P33.2 shipped 2026-07-15;
P31.1, P31.2, P30.1-P30.3 shipped 2026-07-14; P32.1-P32.4 shipped 2026-07-15.)

### P35.9 — Native-Ollama tool-call IDs collide across turns: wrong `tool_name` on replayed results + KV-cache churn

Priority: Tier 1 · Effort: S

`consume` (`internal/provider/ollama/ollama.go`) mints tool-use IDs from a counter that resets on
every request (`toolIndex := 0`, producing `tu_0`, `tu_1`, …). The engine persists those IDs into
session history (`ToolResultBlock{ToolUseID: tu.ID}`, `internal/engine/engine.go`), so every
assistant turn's first tool call in a session carries the ID `tu_0`, the second `tu_1`, and so on.
That collides in `translate`: `toolNames` prebuilds a single ID→name map over the *entire* history,
last occurrence wins. When a later turn's `tu_0` is a different tool than an earlier turn's `tu_0`
(read-file in turn 1, run-shell in turn 3 — the normal shape of an agentic run), every earlier
`tu_0` tool-result message is sent to Ollama with the *wrong* `tool_name`. The native API
correlates results by name, so the model sees results attributed to tools that didn't produce them.

Two consequences, one correctness and one performance:

1. **Mislabelled history** — the model is shown tool results under the wrong tool's name, a silent
   quality degradation on exactly the multi-tool agentic runs the local-model work targets.
2. **KV-cache invalidation** — because the name assigned to an *early* tool result changes whenever
   a *later* turn's same-index call uses a different tool, the serialized prompt prefix mutates
   between requests, and Ollama's prefix cache dies at the first changed byte: the whole
   conversation re-prefills. This is the missed fourth candidate in P35.7's non-determinism sweep
   (thinking blocks / tool-result formatting / system-prompt regeneration were checked; ID reuse
   wasn't). P35.7's clean live confirmation (37-token deltas across 8 turns) holds only when each
   turn's leading tool call happens to repeat the same tool name — plausible for that read-heavy
   STRIDE run, not for mixed-tool runs, so the live confirmation is weaker than recorded.

Fix (either side suffices; both is best):

- **Positional name resolution in `translate`** — walk the messages once, updating the ID→name map
  as tool-use blocks appear, so each tool result resolves against the nearest *preceding* use.
  This alone repairs both the mislabelling and the cache churn, including for existing sessions
  with colliding IDs already stored.
- **Per-stream-unique minted IDs** — e.g. a per-request nonce prefix on `tu_%d`, matching how the
  anthropic/openai adapters rely on server-issued unique IDs.

Regression test: a two-turn translate fixture where turn 1 calls tool A and turn 2 calls tool B
(both minted as `tu_0`), asserting turn 1's result keeps `tool_name: A`; plus a byte-stability
assertion that serializing the turn-1 prefix is unchanged by appending turn 2.

---

## Open Work — Tier 2

**Status:** 2 open — P35.10 and P35.11. (P35.6 shipped 2026-07-18 — see [releases.md](releases.md#latest-changes);
P35.3 shipped 2026-07-18; P34.12 shipped 2026-07-17; P34.9 and P34.10 shipped 2026-07-17;
P34.5-P34.8 shipped 2026-07-17; P34.3 shipped 2026-07-16; P34.2 shipped 2026-07-16, both levers;
P34.1 shipped 2026-07-16; P34.4 shipped 2026-07-16; P33.13, P33.14, P33.15, P33.17, P33.18 shipped
2026-07-16; P33.3-P33.8 shipped 2026-07-15; P30.4-P30.8 and P31.3-P31.5 shipped 2026-07-14;
P32.5-P32.7 shipped 2026-07-15.)

Worth a look for a future item: the same "accurate refusal, error-shaped" question for the other
scanners' documented exit codes, noted while shipping P35.6. P34.6 checked the *language*-targeted
tools; nothing has swept the SCA/secrets tools for non-zero exits that mean "nothing to do" rather
than "I broke".

### P35.10 — `InputTokens` on the native-Ollama path means "uncached prefill tokens", not "prompt size"

Priority: Tier 2 · Effort: S

With P35.4's keep_alive residency working, Ollama's `prompt_eval_count` on a KV-cache hit reports
only *newly evaluated* tokens (the P35.7 live run: 37 after turn 1's 3944), and the native adapter
maps it straight into `usage.InputTokens` (`internal/provider/ollama/ollama.go`). Compaction is
safe — the proactive check uses `conv.estimatedTokens()`, not usage — but the engine's estimate
fallback (`internal/engine/engine.go`, the `InputTokens == 0 && OutputTokens == 0` guard) never
fires on a cached turn, so per-turn traces, session token totals, and the TUI's `in=` display now
understate prompt size on every cache-hit turn. Arguably the truthful "work done" number, but the
shift in meaning is undocumented and anything that later reads `InputTokens` as "context size"
will be wrong. Work: decide and document the semantics (rename in docs, or record both the raw
`prompt_eval_count` and an estimated prompt size), and audit the consumers
(`internal/cost`, turn traces, session totals, TUI/`aegis chat` displays) against the chosen
meaning.

### P35.11 — `/status` reachability probe live-hits Ollama on every poll

Priority: Tier 2 · Effort: S

`probeProviderReachability` (`internal/server/provider_health.go`) fires a live
`GET /api/version` against the Ollama server on every `/status` request. Locally cheap, but a UI
polling at 1-2s means a steady request stream to Ollama for a value that changes rarely. Cache the
probe result (and its latency) for a few seconds — the same freshness the UI can actually render —
so a fast poll loop coalesces to one upstream request per window.

---

## Open Work — Tier 3

**Status:** 0 open. (P35.7 shipped 2026-07-18 — see [releases.md](releases.md#latest-changes);
P35.4 shipped 2026-07-18; P33.10, P33.11, P33.16, P33.19 shipped 2026-07-17;
P32.8 shipped 2026-07-15; P33.9, the keystone that unblocked P33.10 and P33.19, shipped
2026-07-16.)

---

## Open Work — Tier 4

Three items parked — P25.9, P35.8, and P35.12. Low urgency, no trigger, or explicitly parked
pending demand. Do not build speculatively — revisit only if a concrete trigger appears, and check
with the user before starting.
(P33.20 shipped 2026-07-17 alongside P33.11 — its message-allowlist fix was implemented as part of
that work; P32.9-P32.11 shipped 2026-07-15; P33.12, P33.21, and P33.22 shipped 2026-07-17, see
releases.md.)

### P35.8 — Unexplained `aegis.exe` process disappearance during a background-launched `chat` run

Effort: M — parked, unconfirmed, no reliable repro

During the P35.7 live-validation run (`aegis chat` against local Ollama, STRIDE threat-modeling an
external repo), the `aegis.exe` process vanished mid-run with no exit trace anywhere: no crash log,
no panic, no final answer text, no explicit stop-reason in either the stdout transcript or the
debug log (`C:\Users\<user>\AppData\Roaming\aegis\aegis.log`). It had completed 8 turns cleanly (no
timeout, no error — see P35.7's live-confirmed entry in
[releases.md](releases.md#latest-changes)) and written 5 of the threat-modeling skill's 7 expected
output files before it stopped short. By the time the harness went to kill the tracked PID, it was
already gone.

Prime suspect is the test harness, not Aegis itself: the process was launched via a background
shell (`nohup … &`) from inside a sandboxed bash-tool subshell, and that kind of child can get
reaped when the spawning subshell's process group is torn down between tool calls on this
Windows/Git-Bash setup — a harness artifact, not necessarily a daemon/engine bug. Nothing in
Aegis's own logs points to a self-inflicted abort. Trigger to revisit: the same silent
disappearance reproducing under a normal (non-harness-launched, e.g. plain terminal or `aegis
serve`-backed) run, which would rule out the process-group theory and point at something in
`aegis chat`/`engine.Run` itself. Until then this is parked rather than actively chased — the
signal-to-noise on debugging an unreproduced, harness-adjacent process death is too low to justify
speculative work.

### P35.12 — Native-Ollama stream cosmetics: raw-JSON error fallback and the 4MiB line cap

Priority: Tier 4 · Effort: S — parked, cosmetic/edge-case, no concrete trigger

Two minor observations from the P35.9-filing review pass, batched here so they aren't lost:

1. `errorMessage`'s object fallback (`internal/provider/ollama/ollama.go`) returns the raw trimmed
   JSON when an error envelope is an object without a `message` field — the user would see
   `{"error":{...}}` innards verbatim instead of a cleaned message. Cosmetic; only reachable via a
   proxy or future Ollama version that changes the error shape.
2. The shared 4MiB scanner cap (`internal/provider/sse/sse.go`) was sized for SSE deltas, but the
   native path delivers each tool call *whole* on one NDJSON line — a tool-call argument payload
   over 4MiB fails as an opaque `ollama: read stream: bufio.Scanner: token too long` rather than
   anything actionable. No live occurrence; revisit only if a real run hits it (the fix is either a
   larger cap for the native path or rewrapping `bufio.ErrTooLong` into an actionable error naming
   the oversized tool call).

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
