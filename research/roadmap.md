# Aegis Capability Roadmap

- [Aegis Capability Roadmap](#aegis-capability-roadmap)
  - [Status](#status)
  - [Tiering Criteria](#tiering-criteria)
  - [Up next](#up-next)
  - [Open Work — Tier 4](#open-work--tier-4)
    - [P81.7 — The local model endpoint is unauthenticated plaintext HTTP on loopback (FIND-07)](#p817--the-local-model-endpoint-is-unauthenticated-plaintext-http-on-loopback-find-07)
    - [P81.28 — Prose tool-call parsing can promote quoted untrusted text into real calls (FIND-28)](#p8128--prose-tool-call-parsing-can-promote-quoted-untrusted-text-into-real-calls-find-28)
    - [P66.18 — Architecture, quality and maintainability residue](#p6618--architecture-quality-and-maintainability-residue)
    - [P66.20 — Efficiency residue](#p6620--efficiency-residue)
    - [P66.23 — Go-code security residue](#p6623--go-code-security-residue)
    - [P66.17 — Local-model path: the Low-severity residue](#p6617--local-model-path-the-low-severity-residue)
    - [P77.6 — No OS-level process sandbox on Windows (GAP-05, spun out of P66.19)](#p776--no-os-level-process-sandbox-on-windows-gap-05-spun-out-of-p6619)
    - [P60.3 — Checkpoints capture files only, so `/rewind` is silent about everything else](#p603--checkpoints-capture-files-only-so-rewind-is-silent-about-everything-else)
    - [P65.4 — Resume is phase-granular, artifact-inferred, and only the drive has it](#p654--resume-is-phase-granular-artifact-inferred-and-only-the-drive-has-it)
    - [P67.13 — There is no way to execute a plan without committing to it](#p6713--there-is-no-way-to-execute-a-plan-without-committing-to-it)
    - [P66.26 — `synchronous=NORMAL` on the three SQLite databases (PERF-02, refiled from P66.9)](#p6626--synchronousnormal-on-the-three-sqlite-databases-perf-02-refiled-from-p669)
    - [P80.3 remainder — `Server`'s auth/lockout and context-window field groups](#p803-remainder--servers-authlockout-and-context-window-field-groups)
    - [P66.19 — Capability gaps with no fired trigger](#p6619--capability-gaps-with-no-fired-trigger)
    - [P64.5 — `ask_user` is one free-form question; unattended answers cannot be routed](#p645--ask_user-is-one-free-form-question-unattended-answers-cannot-be-routed)
    - [P61.7 — Retry/terminal classification over _backend-echoed_ text (remainder)](#p617--retryterminal-classification-over-backend-echoed-text-remainder)
    - [P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)](#p5214--session-scoped-loop-detector-cross-run-loops-are-invisible)
    - [P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)](#p259--per-session-scoping-of-lspmanager-remaining-daemon-singleton)
    - [P65.5 — Rewinding away from a branch discards its work instead of summarizing it forward](#p655--rewinding-away-from-a-branch-discards-its-work-instead-of-summarizing-it-forward)
    - [P67.11 — Every budget is a ceiling; none expresses how much effort is wanted](#p6711--every-budget-is-a-ceiling-none-expresses-how-much-effort-is-wanted)
    - [P67.12 — Personas cannot accumulate anything across runs](#p6712--personas-cannot-accumulate-anything-across-runs)
  - [Verification Work](#verification-work)
    - [P80.4 — `live_workflow`'s two standalone tests both need a stronger model than this machine has run](#p804--live_workflows-two-standalone-tests-both-need-a-stronger-model-than-this-machine-has-run)
    - [P38.1 — Non-orchestrated, single-context threat-model build (primary path for local models)](#p381--non-orchestrated-single-context-threat-model-build-primary-path-for-local-models)
    - [P68.4 — The triage rubric's measuring band sits below the strongest local model](#p684--the-triage-rubrics-measuring-band-sits-below-the-strongest-local-model)
    - [P68.5 — P52.16's `toolResultEcho` measurement was taken through a defective template](#p685--p5216s-toolresultecho-measurement-was-taken-through-a-defective-template)
    - [P68.6 — The 14b family never produces the report, and nothing in the run says why](#p686--the-14b-family-never-produces-the-report-and-nothing-in-the-run-says-why)
    - [P62.8 — The prefix-cache gate's large-window regime has never been measured](#p628--the-prefix-cache-gates-large-window-regime-has-never-been-measured)

---

## Status

**Updated 2026-09-03.** Tier 1: **0 open**. Tier 2: **0 open**. Tier 3: **0 open** — the P81
threat-model batch (filed 2026-08-31) is fully resolved except two Tier 4 items (P81.11 closed the
same day it was re-tiered, P81.31 shipped in full, P81.18 shipped except one explicitly-deferred
helper — see below); full closing
record in
[releases.md](releases/releases-01.md#threat-model-2026-08-31--the-p81-batch-closing-record).
Tier 4: **20 open** — parked build items, none with a fired trigger; see
[Open Work — Tier 4](#open-work--tier-4). Verification: **6 open** — code already written,
waiting on a live-model run; see [Verification Work](#verification-work).

**P81.11 closed 2026-09-03.** Its residual scheduling question — whether `govulncheck` gets a
schedule of its own independent of the disabled `ci.yml` — turned out to already be answered: a
weekly-cron `govulncheck` job has run in `codeql.yml` since 2026-08-07 (P63.2, commit `899912b`),
**before** the threat model that filed this item even ran. The entry's own text describing the
question as open was stale from the moment it was written. No code changed to close this; full
record in [releases.md](releases/releases-01.md#p8111-closed-p817-and-p8128-partially-shipped-2026-09-03).

**P81.31 shipped in full, P81.18 shipped except its trust-store helper, 2026-09-03.** P81.31/FIND-31:
a per-session checkpoint byte cap with oldest-first eviction
(`cleanup.checkpoint_max_session_mb`), and a rewind-time confirmation gate that refuses (HTTP 428) to
overwrite a file changed by something other than the agent's own turn until the caller confirms.
P81.18/FIND-18: `aegis ui` now prints the daemon's certificate fingerprint alongside its existing
self-signed-certificate notice, and `docs/configuration.md` documents the supported tunnelled/proxied
path; an automated OS-trust-store-import command was deliberately not built (real, platform-specific,
no fired trigger — documented as a manual step instead). Full record in
[releases.md](releases/releases-01.md#p8131-shipped-p8118-shipped-except-its-trust-store-helper-2026-09-03).

**This document tracks only open work and current counts.** A completed item's full record — what
it was, what building it found, and what was measured to close it — belongs in
[releases.md](releases.md), not here; it moves there the day it ships. (2026-09-03 housekeeping:
several items whose write-ups had drifted back into this file — the drift
[roadmap-status.md](../.claude/skills/roadmap-status.md) warns against — were moved out; see
[the migration record](releases/releases-01.md#roadmap-housekeeping-closed-items-migrated-from-roadmapmd-2026-09-03).)

**Every shipped item is closed against a live-verified test or a live probe run on this machine,
recorded in its release entry — never asserted from reading a diff.** That standard constrains
future work here.

Tiers 1-4 are **build work** — every item there requires writing code that doesn't exist yet.
**Verification work** is its own track, not a tier: every item in it is code that is _already
written_, sitting behind one gate — a live-model run producing evidence the item's closure
condition names.

**No Tier 4 item currently has a fired trigger** — see each entry's own **Promote when** for what
would change that.

**Standing constraints on the open batches.** **The three P67 constraints, which apply to every P67.11–P67.13 entry below and are not repeated
in them.** Those three are what remains open from a comparative reading of the leaked Claude Code
CLI source against Aegis, done 2026-08-16 — P67.10 and P67.14 have since shipped; full batch
record in
[releases.md](releases/releases-01.md#standing-constraints--the-p67-and-p74-external-source-reading-batches-p74-half-closed).

- **That source is leaked proprietary code. Nothing may be transcribed from it.** Each item is a
  design reading — a mechanism and the reasoning behind it — and needs an independent Go
  implementation written from this document, not from that repository.
- **The leak is partial.** `src/utils/**` is absent, so permission internals, `forkedAgent` and
  `toolResultStorage` were legible only through call sites. Where an entry's claim about _their_
  implementation rests on a call site rather than the code, it says so.
- **Every claim about Aegis in these entries was checked against this tree**, at the file and line
  cited, not against the docs. The claims about their side were not, and cannot be — treat them as
  motivation, never as a specification.

**The P66 entries here are deliberately grouped grab-bags** (**P66.17**, **P66.18**, **P66.20**,
**P66.23**), each collecting the Low-severity residue of one review domain, filed so no finding is
lost rather than because any of them should be scheduled. Take one only when already working in
that file. The review itself — six specialist reviewers, an adversarial debate and a
static-analysis pass, 70 findings against HEAD `3c2b57b` — is in [CodeReview.md](CodeReview.md)
with per-finding evidence. **Read the corrections in releases.md before acting on that document
directly:** several shipped items contradict the finding they were built from (VULN-03's
suggested `::ffff:0:0/96` addition would have blocked the entire public internet; LLM-04 drops
_every_ tool call on a 1-based backend, not only trailing ones).

---

## Tiering Criteria

Applies to **build work** (Tier 1-4) only — items requiring new code. Items whose code is already
written and are only waiting on a live-run result belong in [Verification Work](#verification-work)
instead, regardless of how large or urgent the underlying question is.

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Up next

**Last updated: 2026-09-03.** The only genuinely scheduled row is the live-tier remainder — the
rest of the Verification track (**P66.22**, **P62.9**, **P65.2**) closed 2026-09-01/09-02 by live
evidence and moved to [releases.md](releases/releases-01.md#roadmap-housekeeping-closed-items-migrated-from-roadmapmd-2026-09-03).

| #   | Item                                                                                             | Tier / size  | Why now                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| --- | ------------------------------------------------------------------------------------------------ | ------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | **The live-tier remainder** (P38.1, P80.4) — _parked by choice, 2026-08-16_                      | Verification | **P38.1** still needs permission to launch an unattended auto-approving agent — the recipe is `--yes` plus `auto_approve_exec`, and a session hasn't been permitted to launch that unattended. **P80.4** is unchanged: its two standalone tests need a model that will complete the fixture's 14-file read chain, and nothing available here has done that yet. Both remain parked by choice rather than blocked on code — everything load-bearing for either has already shipped (see each entry below).                                                                                                                                                                                                                                                                                                                                                                                                       |

**Sizes are estimates from reading, not from building, and the batch had a known bias** — several
past estimates were smaller than filed and one was larger. Treat any size estimate below as a
rough signal, not a commitment.

**This table outranks `scripts/roadmap-status.sh`.** That script reports open items in _document_
order and cannot see the cross-tier ranking. Use it for repo state and for the parse; use this
table for what to take.

**Everything in Tier 4 below has no fired trigger** — see each entry's own **Promote when**
condition for what would change that.

---

## Open Work — Tier 4

**Status: 20 open.** Eight pre-existing (all blocked or explicitly parked, none with a fired
trigger), two from the P81 threat-model batch (**P81.7**, **P81.28**, each parked for a stated
reason — see each entry; both partially shipped 2026-09-03, remainder described in each entry),
six from the P66 review batch, three from the P67 external-source reading (**P67.11**, **P67.12**,
**P67.13**), and **P77.6** (spun out of P66.19). **P81.11** closed 2026-09-03 (see
[Status](#status)) rather than staying in this count; **P81.31** shipped in full and **P81.18**
shipped except its explicitly-deferred trust-store helper, both 2026-09-03 (see
[releases.md](releases/releases-01.md#p8131-shipped-p8118-shipped-except-its-trust-store-helper-2026-09-03)).
Everything else that was ever in this tier has shipped — see
[releases.md](releases/releases-01.md#roadmap-housekeeping-closed-items-migrated-from-roadmapmd-2026-09-03)
for the closed-item record.

**How to use this tier.** Every Tier-4 item that has actually been measured so far turned out to
be wrong in some way — an unmeasured dependency that was actually our own code, a gate unmeetable
by the work it proposed, a cap that wasn't the largest one in the tree. Take the measurement
first, then re-read the item; do not treat a Tier-4 write-up as a build plan. Details of past
measurements are in [releases.md](releases.md).

**Below, the still-open items are ordered by importance/impact/benefit — highest first — rather
than by filing order or ID.** A scored, threat-model-sourced finding with real CVSS weight
outranks a residue grab-bag; a correctness bug users would actually notice outranks a speculative
feature idea; an item a prior sitting explicitly judged not worth its cost sits last. This is a
reading order, not a build queue — nothing below has a fired trigger, and each item's own
"Priority: Tier 4" line and "do not build speculatively" caveat still governs whether to act on
it.

### P81.7 — The local model endpoint is unauthenticated plaintext HTTP on loopback (FIND-07)

**Filed 2026-08-31**, from the threat model
([**FIND-07**](../threat-model-20260831-002123/3-findings.md#find-07-the-local-model-endpoint-is-unauthenticated-plaintext-http-on-loopback),
CVSS 7.1, `Important`, CWE-319). The default local deployment sends every prompt — workspace file
contents included — to `http://localhost:11434` over plaintext HTTP with no authentication in either
direction. A local process with packet-capture privilege reads the whole conversation; a local process
that binds the port first, or wins a restart race, answers **as the model** and dictates what tool
calls the agent attempts next.

**The argument for taking it seriously is the project's own.** `server.tls.enabled` defaults to true
specifically because "plaintext HTTP still leaves the bearer token and full conversation content
readable to another local account on a shared host with packet-capture privilege." The content on the
provider hop is identical and there is no authentication at all. `validateBaseURL` exempts loopback
endpoints from the plaintext refusal deliberately, "matching how such setups already work today" — a
compatibility decision, not a security one.

**Shipped 2026-09-03: the bearer-token half, plus hardening documentation.**
`provider.local_auth_token` (`internal/config/config_provider.go`, `ProviderConfig.HeadersFor`) sends
`Authorization: Bearer <token>` to any target `config.LocalBackend` resolves as local, wired into both
call sites in `providerfactory.Build` (primary and fallback), and an explicit `headers.Authorization`
still wins over it. `docs/installation.md` now documents the honest limit: **Ollama itself does not
check the header** — there is no upstream config to require it — so the token alone does not close
either threat against a bare `ollama serve`. What it does is give an authenticating reverse proxy
placed in front of Ollama something to enforce (the documented pattern: Aegis → HTTPS+bearer → proxy
→ plaintext loopback → Ollama, with the proxy also terminating TLS), and it closes the gap directly
against any other local OpenAI-compatible server that does validate bearer tokens (llama.cpp's server,
vLLM, LiteLLM). `TestProviderConfig_HeadersFor` pins the local-only scoping and the explicit-header
precedence.

**Checked 2026-09-03: the Unix-socket/named-pipe path is not viable today.** `OLLAMA_HOST=unix://...`
support is [PR #8072](https://github.com/ollama/ollama/pull/8072) against upstream Ollama, still open
and unmerged as of the most recent check; the original request
([issue #739](https://github.com/ollama/ollama/issues/739)) has been open since 2023 with nothing
shipped. No Windows named-pipe support has ever been proposed. This confirms the original entry's own
caveat — "half the remediation is upstream" — was optimistic rather than merely cautious; there is
currently no upstream socket/pipe surface to build against at all.

**What remains.** TLS-with-pinned-cert direct to a local provider is subsumed by the documented proxy
pattern (the proxy terminates TLS) rather than needing `client.NewFromConfig`'s pinning machinery
extended to a second endpoint type — not filed separately. The socket/pipe path stays parked on
upstream.

**Why Tier 4 rather than higher.** The prerequisite is another local account, or a hostile process
already running as someone on this host, on a single-user development machine. Promote the
remaining socket/pipe question if Ollama ever merges PR #8072 (or ships Windows named-pipe support) —
that's also the condition that promotes **P81.24**'s encryption half.

Priority: Tier 4 — mechanism half shipped; the remainder is upstream-blocked, not a local decision.

### P81.28 — Prose tool-call parsing can promote quoted untrusted text into real calls (FIND-28)

**Filed 2026-08-31**, from the threat model
([**FIND-28**](../threat-model-20260831-002123/3-findings.md#find-28-prose-tool-call-parsing-can-promote-quoted-untrusted-text-into-real-tool-calls),
CVSS 5.4, `Moderate`, CWE-1427). `internal/provider/prosetoolcall.go` and `internal/toolshim` exist
because some local models emit tool calls as free-form text rather than structured calls, and they
recover those — P74.8's whole point. What the parser cannot do is distinguish a call the model
_intended_ from a call the model merely _quoted_, and untrusted content that reaches model context is
frequently quoted back verbatim in a summary or an explanation.

**The `internal/toolshim` half is off by default** (`provider.tool_call_shim: off`). **The
`internal/provider/prosetoolcall.go` half is not** — `WithProseToolCallSalvage` is on by default for
every model served by a local provider (`profile.NewResolver`, `ProseToolCallSalvage: true`), so this
finding's exposure is broader than "off by default" suggested: it is live on the local profile today,
not gated behind an opt-in.

**Shipped 2026-09-03: the two separable, cheap halves.** A tool call recovered by either mechanism —
`provider.IsProseSalvagedCallID` for the always-on salvage path, an id prefix check for the shim's
per-call ids — now carries a **"recovered from prose"** label appended to the approval-prompt reason
(`tool.WithCallProvenance`/`tool.CallProvenance`, read by `permission.Gate.Check` and by the
contextual gate's egress-then-write and taint-after-untrusted-content Ask branches). This is
provenance, not containment: it gives a human approver the signal that a pending write/execute/network
call did not arrive as a native structured call, so they can judge whether the model actually meant to
make it. `docs/local-model-tuning.md` §5 now documents the injection interaction and the label's
meaning. Regression tests: `TestGateCheckAnnotatesReasonWithCallProvenance`,
`TestRecoveredCallProvenance`.

**What remains.** The actual containment — never promoting a call parsed out of a span of model output
that reproduces content which arrived inside an untrusted-content wrapper, tracked by content hash for
the turn — still needs **P81.1**'s taint bookkeeping, which does not exist yet. Building a second,
weaker taint mechanism just for this would be wasted work; this stays sequenced behind P81.1.

Priority: Tier 4 — the cheap, separable half has shipped. The remaining containment is **P81.1**'s to
build, with no fired trigger of its own.

### P66.18 — Architecture, quality and maintainability residue

A mid-stream `EventError` discards the whole assistant turn **including text already streamed to the
user**, so the transcript loses content the user watched arrive (ARCH-09) — the most user-visible item
in this grab-bag and the one most likely to be reported as a bug. Session-scoped in-memory state leaks
on prune, and two maps leak on delete (ARCH-10). Ten ad-hoc `truncate` helpers sit alongside the one
canonical truncation policy in `truncate.go` (QUAL-07). `context.Background()` appears inside
request-scoped handlers (QUAL-08). `internal/drive` has no package doc and ~10.5% of exported symbols
are undocumented (QUAL-09).

Priority: Tier 4 — no trigger.

### P66.20 — Efficiency residue

The obvious performance work is genuinely done — the review verified per-turn session writes, WAL with
`busy_timeout` on all four stores, incremental token estimates, package-level regexes at all 68 call
sites, real read-tool concurrency, persistent sandbox containers. This is the residue after P66.9
takes the one item that mattered.

The `<repo_map>` is built once at daemon startup and never invalidated; the staleness check was
benchmarked at **11.5 ms** against a 185 ms full rebuild, so the cautious fix is affordable — but note
the finding's title was wrong and `POST /repomap/index` already exists (`server.go:115`), so this is
narrower than reported (PERF-04). `toolshim.Prompt` rebuilds a multi-KB prompt string per turn
(PERF-06). Checkpoint file snapshots are uncompressed, undeduplicated and uncapped (PERF-07) — related
to the pre-existing P60.3. Two `flushMessages` calls per turn where one would do (PERF-09).
`MaterializeBuiltins` re-reads 800 KB of embedded skills on every daemon start at a measured 46.7 ms
(PERF-05, **withdrawn** by arbitration as real-but-nil-impact; recorded here so it is not re-filed).

`sseWriter.send` drops the **oldest** queued event under backpressure, which is right for tool calls
and would silently corrupt text (PERF-08) — marked SUSPECTED and never confirmed. If any item here is
promoted, promote that one, and confirm it first.

Priority: Tier 4 — no triggers. PERF-08 is the only one with a correctness edge.

### P66.23 — Go-code security residue

Six line-level findings the debate left standing but small. Grouped so none is lost; none is
scheduled.

`latex_build` runs an arbitrary binary because its `compiler` enum is never enforced — and the
general fact behind it is worth more than the instance: **nothing in this module validates tool input
against `InputSchema()`, so every enum in every builtin is advisory** (VULN-04, downgraded to Low by
arbitration because `latexBuildTool.Capability()` is already `CapExecute`, so no boundary is crossed —
but a future `CapRead` tool with an enum would be a different story). The DAST work directory is
chmod'ed 0777 in a shared temp dir, letting a local user plant the SARIF that becomes both the
operator's report and model context (VULN-06, POSIX-only, needs a hostile local user racing a scan
window). `expandFileMentions` confines lexically only, so a workspace symlink reads outside the root —
bypassing the symlink check every other read path gets (VULN-07, reachability caveat: only the
planted-symlink variant is confirmed). Windows reserved device names and ADS (`file.txt:stream`) are
not rejected by path validation (VULN-08, read but never executed). Five walk callbacks read whole
files unbounded (VULN-09). Hook stderr is captured unbounded and returned to the model (VULN-10).

**Promote when:** VULN-04's _general_ form — schema validation for tool input — is worth its own item
if a read- or network-capability tool ever grows an enum that gates a path or a binary. The rest are
opportunistic.

Priority: Tier 4 — all Low, all confirmed, none with a fired trigger.

### P66.17 — Local-model path: the Low-severity residue

Eleven findings from the local-runner review, none individually worth a trip. `tokenest.Message`
ignores `ImageBlock` and `ThinkingBlock`, so images and thinking history are free in every estimate
(LLM-07). The Anthropic adapter's mid-stream errors are unclassifiable and therefore never retryable,
and its tool-call JSON is emitted unvalidated (LLM-08). The P59.5 local-backend carve-out reached the
output guard but not compaction or titles, though `routing.go:13` names all three sites (LLM-06). The
tool-call probe loads the model at the wrong `num_ctx`, forcing a reload on the first real turn
(LLM-10). Failover switches models without re-resolving the context window (LLM-11).
`ollamainfo.Detect` makes an unconditional, always-wasted `/api/show` round-trip (LLM-12).
`fitTranscript` re-renders and re-tokenizes the whole prefix up to O(n) times (LLM-13). A
misconfigured `summary_tokens` silently disables the summarizer's fit check (LLM-14). The carried file
record parses `<read-files>` tags out of _assistant_ text (LLM-15). The SSE idle watchdog counts
consumer backpressure as a stalled runner (LLM-17). `reapSpills` scans the whole spill directory on
every spill (LLM-18).

**Promote when:** one of them is implicated in a live-run failure, or you are already in the file.
LLM-06 and LLM-10 are the two most likely to matter on a 16GB-VRAM machine, since both cause an
avoidable model reload.

Priority: Tier 4 — real, individually cheap, no trigger. Do not schedule.

### P77.6 — No OS-level process sandbox on Windows (GAP-05, spun out of P66.19)

`internal/sandbox`'s OS-level (no container runtime) backend covers darwin (`sandbox-exec`/seatbelt)
and linux (`bwrap`) in `detectOSSandbox()` (`internal/sandbox/os_sandbox.go`) — there is no
`case "windows"`. A Windows host without podman/docker/WSL installed falls all the way through to
`LocalBackend`: commands run directly on the host with no filesystem or network confinement at all,
the one platform where `sandbox.go`'s backend-selection order has nothing OS-level to fall back to.
Conspicuous because the rest of the Windows story (PowerShell shell tool, path handling, CI) is
otherwise handled well.

**Recommended direction, from the user (2026-08-25):** a Job Object (resource/kill-on-close
containment) plus a restricted access token (drop admin/write privileges outside the workspace) —
native Windows primitives, no new external dependency. AppContainer (the same primitive UWP apps
use) is stronger but was explicitly set aside as higher-complexity for a general-purpose CLI
subprocess model; revisit only if the Job Object + restricted token approach proves insufficient in
practice.

Priority: Tier 3/4 — down the road, not speculative-build-now. No code changes yet.

### P60.3 — Checkpoints capture files only, so `/rewind` is silent about everything else

`internal/checkpoint` snapshots each file a write tool touched (capped at 16MiB) and rewind writes
those contents back — correct within its documented scope. Rewinding a turn that ran `pip install`,
applied a DB migration, started a background process, or wrote a >16MiB artifact restores the source to
pre-turn state while leaving the environment in post-turn state, and the user is told the turn was
undone. If a session owns a persistent container (P60.2, shipped 2026-08-05), a checkpoint could be a
container snapshot/commit instead, making rewind honest about installed packages and process state.

**Update 2026-08-25:** `sandbox.backend`'s default is now `"container"`, cascading to `"os"` and then
`"local"` when no container runtime is available (`SelectSandbox`, internal/server/server.go). A host
with Docker/Podman running now gets the container backend by default. This satisfies the first half of
the promote condition below — the container backend is now the default a session lands on wherever
Docker/Podman is actually present — but the container-commit checkpoint mechanism itself (the actual
fix this item describes) has not been built yet.

**Promote when:** someone builds the container-commit checkpoint mechanism now that the default flip
has landed, or a user reports a rewind that restored files into an environment that no longer matched
them.

Priority: Tier 4 — no longer blocked, but speculative until someone is actually rewinding inside a
container.

### P65.4 — Resume is phase-granular, artifact-inferred, and only the drive has it

**What Aegis has, stated carefully — the gap is narrower than it first looks.** The phased drive
already resumes well: a phase whose files carry no `PENDING` marker costs zero model turns on re-entry,
and the whole reset ladder is built on re-entering from disk. Two limits:

- **It is phase-granular and the granularity is the artifact** — the oracle is the `PENDING` marker in
  the skill's own scaffolded files, so a crash 40 turns into phase 6 re-runs phase 6 from its start.
  Probably the right trade at the drive's scale.
- **It exists only because the _skill_ supplies the oracle.** A plain TUI/web-UI session, a cron job, a
  swarm sub-agent, an `aegis chat` run with no skill — none of these has artifacts with markers, so none
  resumes at all. Kill the daemon mid-turn and the turn is gone; the in-memory `repairOrphanedToolUses`
  patch (P65.1, shipped) is the entire recovery story outside the drive.

A durable version would need: write-once entries / mutable namespaced registers / an append-only usage
ledger, one register overwritten with the operation's complete current state after every step (recovery
reads that one row and switches on it rather than replaying a journal), and each tool declaring
`replay: "safe" | "never"` so a mid-effect interruption writes a synthetic result under an id reserved
before the effect started rather than re-running it.

**Why Tier 4 and must not be built speculatively.** The session store is SQLite and already has the
storage substrate; `Capability` already partitions the tool set close to what `replay` would need. But
**nobody has reported losing work this way** — the drive, the only workload long enough for a crash to
be expensive, is also the only one that already resumes.

**If it is ever built:** don't design the durable version first. P65.1's in-process `startedTools`
record is the part with a user-visible payoff already shipped, and the durable version is that same
record written through the session store before the effect and cleared after.

**Promote when:** a real run loses work to a daemon restart or crash outside the drive, or a non-drive
workload grows long enough that it would (unattended cron chains, long-lived swarm sub-agents are the
two candidates).

Priority: Tier 4 — no trigger, large, and its cheapest and most valuable slice already shipped as
P65.1.

### P67.13 — There is no way to execute a plan without committing to it

Plan mode describes intent; it cannot show the diff that intent would produce, because producing the
diff means performing the writes. The mechanism that resolves this is a **copy-on-write overlay**:
writes are redirected into an overlay directory after copying the original in, reads of
already-written paths are served from the overlay, everything else reads through to the real tree, and
execution stops at the first effect the overlay cannot contain — a non-read-only shell command, a
network call, anything outside the workspace — recording a typed boundary describing where and why it
stopped. Accepting promotes the overlay into the workspace; discarding costs a directory delete.

Aegis has most of the substrate: `internal/sandbox` for isolation, `internal/swarm` for forked runs,
`internal/checkpoint` for per-turn restore, and a permission layer that already classifies calls by
capability. **P67.8**'s flag-level classifier is what would decide the shell boundary precisely rather
than conservatively.

The comparison source uses this for _speculation_ — predicting the user's next prompt during idle time
and pre-executing it. **That half is not recommended.** The prediction is the expensive, risky,
low-confidence part; the overlay-and-boundary machinery is the durable part, and its first consumer
should be an honest dry-run mode, where the value does not depend on guessing right.

**Promote when** the overlay has a named first consumer — a `--dry-run` that shows a real diff is the
plausible one. Building the overlay with no consumer produces an untested second write path, which is
strictly worse than not having it.

Priority: Tier 4 — no fired trigger, L. Do not build speculatively.

### P66.26 — `synchronous=NORMAL` on the three SQLite databases (PERF-02, refiled from P66.9)

**Filed 2026-08-16**, carved out of P66.9 so a deliberately-skipped sub-item is visible rather than
buried in a closed entry. Every SQLite database runs at the default `synchronous=FULL`, paying an
fsync per transaction.

The item splits cleanly, and only one half is contentious. **`knowledge.db` and `longmem.db` are
unconditionally safe**: both are derived stores, rebuildable from their sources, and losing the tail
of a write on power loss costs a re-index. Neither was in P66.9's reach — they live in
`internal/knowledge` and `internal/memory`, not `internal/session` — which is the only reason they
did not ship with it. **`sessions.db` is the contentious half**, and is why the debate downgraded
PERF-02 to Low: it holds checkpoints (`/rewind`), the cost ledger and traces, so `NORMAL` trades a
durability guarantee on the one database whose loss is not recoverable from elsewhere. Do that half
only with the trade written down at the DSN, or not at all.

Note that P66.9 already removed most of the pressure that motivated this: delta coalescing cut the
`bg_events` insert rate by roughly the coalescing factor, and that table was the source of the
fsync-per-token pattern the finding was reacting to. Re-measure before building — this document has
twice recorded a fixed instrument inverting an already-acted-on verdict.

**Promote when** a measurement on the _current_ tree (post-P66.9) shows fsync cost still material on
the local path, or when `knowledge.db` re-indexing becomes a noticed cost on its own.

Closes PERF-02. Priority: Tier 4 — S. No dependency.

### P80.3 remainder — `Server`'s auth/lockout and context-window field groups

**P80.2 (the three unread packages) and the first slice of P80.3 (the `Server` struct's per-session
`sync.Map` family) both shipped 2026-09-03 on direct request.** Full record:
[releases.md](releases/releases-01.md#p802-and-p803-2026-09-03). P80.2's investigation closed
`internal/tui` and the sandbox container backends clean (correctness only, no permission-posture
findings — the questions the original entry named all check out) and found one real defect in
`internal/drive`: budgets were accumulating across a phased drive's shared `cost.Tracker` instead of
resetting per phase-turn as `internal/engine/budget.go`'s own doc comment says they should. That was
fixed the same sitting (`runBudget` now baselines the tracker's totals per `Run`), closing P80.2 in
full — nothing left to promote.

**P80.3 is only partly closed.** The struct half of the audit's L4 named three field groups legible
from `Server`'s eight mutexes: the per-session `sync.Map` family (now grouped into `sessionMaps`,
reachable as `Server.sess` — the slice that shipped), the auth/lockout state (`authToken`, `tlsCert`,
`invalidAuthAttempts`, `authLockMu`/`authConsecutiveFailures`/`authLockedUntil`,
`pageTokenMu`/`pageTokens`), and the context-window state (`ctxWinMu` guarding `ctxWin`, `ctxWinSrc`,
`ctxWinFinal`, `ctxWinByModel`, `autofitWin`, `weightsSeen`). The other two groups are still flat fields
on `Server`. Unlike the per-session maps, both remaining groups had non-`sync.Map` state guarded by a
plain `sync.Mutex`, so grouping them is a larger rewrite (real fields with real zero-value defaults some
tests do set directly, not a `sync.Map` every test already skips) — worth its own sitting rather than
folding into this one speculatively.

Promote when: someone is already changing the auth/lockout or context-window state's shape, or a bug
turns up that a grouped struct would have made visible (the M6 archetype — a per-session map
`handleDeleteSession` forgot — doesn't apply to either remaining group, since neither is per-session
state needing session-scoped cleanup; the risk here is closer to "a lock protects fields that turn out
not to all be adjacent in the source").

Priority: Tier 4 — no fired trigger, M. Structural and ongoing by the audit's own framing; the file
half and now the per-session-map slice already bought most of the legibility.

### P66.19 — Capability gaps with no fired trigger

Assessed against what a mature coding agent needs, and honestly reported as absent rather than
planned. The user chose to act on a prioritized subset (2026-08-25), and four of the six findings
here have since shipped — see
[releases.md](releases/releases-01.md#qual-04-and-gap-02030809--resolved-sub-findings-with-no-other-record)
for that record. GAP-05 was spun out to its own item, **P77.6** (still open). What remains here is
**GAP-04** (git support stops short of branching, `internal/worktree` exposes no tool at all) and
**GAP-07** (the MCP server side lags the mature client — no `resources/*`, `prompts/*`,
`sampling/*`, or `notifications/*`), both unpromoted.

Priority: Tier 4 for the two remaining items (GAP-04, GAP-07) — no triggers. Do not build
speculatively.

### P64.5 — `ask_user` is one free-form question; unattended answers cannot be routed

`internal/tool/builtin/ask.go` is one question string, an optional `[]string` of choices, a string
back. A batch answered by anything other than a human at a terminal — the non-interactive
`Questioner`, a future policy answerer, a parent agent answering for a sub-agent — has to match answers
to questions by question text today, since there's no caller-supplied `id` echoed in the answer; the
text is model-authored and may repeat. A structured error taxonomy (user-cancelled vs
no-provider-registered) would also let an unattended drive respond differently to those two outcomes,
which deserve opposite responses.

Against building it: `ask_user` is close to unusable in the unattended drive that is Aegis's main
proving ground today (`AutoAnswer` returns a fixed string), so the routing problem is real but has no
current caller.

**Promote when:** something other than the TUI is answering questions — a policy answerer, a parent
agent answering for a spawned one, or a drive phase that legitimately needs to stop and ask.

Priority: Tier 4 — no trigger, no current caller. Do not build speculatively.

### P61.7 — Retry/terminal classification over _backend-echoed_ text (remainder)

`classifyStreamError` decides whether a mid-stream failure is retried or fatal by substring match
against a free-form server error string. The in-repo half shipped 2026-08-06 (Aegis's own OpenAI
adapter was splicing model-authored tool names into a message the classifier then matched — fixed via
`APIError.Detail`, rendered but never classified). **What's left is the case originally described:** a
server or proxy echoing generation fragments into its own `{"error":…}` envelope, where the text is
genuinely external. Still unmeasured, and a fix means guessing at a structural signal (status code, an
error `type` field) most local backends don't supply.

**Promote when:** a misclassification is actually observed, or a backend is found that demonstrably
echoes generation content into its error envelope.
`TestModelAuthoredTextDoesNotSteerClassification` exists as a regression test for the shipped half;
extending it to envelope text is the natural next probe.

Priority: Tier 4 — narrowed to the external case; real surface, unquantified likelihood, no incident.

### P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)

`newLoopDetector` is constructed inside `Run`, so its window resets every call. In the TUI and web UI
each user turn is a separate `Run`, so a model that loops _across_ user turns (re-reading the same file
every time the user nudges it, re-running the same failing command after each correction) is never
detected. Fix: hoist the detector to session scope via an optional caller-owned detector in
`engine.Options`, so the daemon can hold one per session while the CLI keeps today's per-`Run`
behavior. Open design question: a user legitimately asking for the same call twice across two turns
isn't a loop, so a session-scoped detector likely needs a higher threshold or a reset rule keyed on
whether a user message is a bare retry — fuzzier than the current mechanism.

**Promote when:** a live run shows a cross-turn loop that per-`Run` detection missed.

Priority: Tier 4 — real but unproven, and the false-positive risk is higher than the current detector's.

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of six daemon-singleton services are per-session-scoped; `lsp.Manager` was deliberately left
shared — its per-session resource-growth tradeoff was judged worse than the isolation gap. Parked
pending a concrete multi-tenant need.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

### P65.5 — Rewinding away from a branch discards its work instead of summarizing it forward

`internal/checkpoint` gives per-turn restore points and the TUI has `/rewind` and fork; rewinding
restores, it does not carry anything forward. Correct default for the common case (the user wants the
last turns gone) but wrong for the case of exploring an approach, learning something real, abandoning
it, and then watching the model rediscover the same dead end because the transcript no longer contains
it. A branch-navigation summary — find the common ancestor, summarize the abandoned span, append it as
an entry on the target branch, same structured format as P65.2's compaction skeleton and file
tracking — would fix it, offered rather than automatic (or `/rewind` stops meaning "undo").

**Why Tier 4 and not higher.** Downstream of P65.2 (shares the summary format and file tracking —
designing it twice would be wasted work), and there's no complaint behind it: `/rewind` has not been
reported as losing anything a user wanted.

A wider version — **lanes**, named cursors into one shared session tree, each owning its own leaf,
model config, queue, and at most one in-flight operation — would be a cleaner model for
`internal/swarm` sub-agents than spawning goroutines with separate histories, but is a session-storage
rewrite with no complaint behind it either; not filed.

**Promote when:** a real session loses reasoning worth keeping to a `/rewind` or a fork.

Priority: Tier 4 — no trigger, sequenced behind P65.2 (shipped), changes what a well-understood command
means.

### P67.11 — Every budget is a ceiling; none expresses how much effort is wanted

`internal/engine/budget.go` is entirely ceilings — `BudgetUSD`, `MaxTokensPerRun`, `MaxIterations`,
`MaxWallClockPerRun`, `MaxTurnStall` — and all of them abort. There is no knob meaning "this task is
worth 200K tokens of thoroughness, keep going until you have spent it or stopped making progress."

The inverted form is coherent: nudge the run to continue while spend is below a target, and stop early
when returns diminish — the workable test being several consecutive continuations whose token deltas
are each below a small threshold, so a run that is still finding things keeps going and one that is
circling stops.

If ever built, it must be a **separate opt-in target with its own resettable stop**, not a second
meaning layered onto an existing knob. A value that is simultaneously a floor and a ceiling is a
footgun, and the existing abort semantics are load-bearing and asymmetric — stall and wall-clock
aborts are fatal to a drive, loop and tool-failure aborts are resettable. The natural home is
`internal/drive`, where "keep working until this phase is genuinely done" is already the model.

**Promote when** a real drive run ends early with budget unspent and work unfinished. The
diminishing-returns stop test is worth remembering separately from the rest of the idea; it is the
part most likely to be useful in another context.

Priority: Tier 4 — no fired trigger, speculative. Do not build speculatively.

### P67.12 — Personas cannot accumulate anything across runs

Personas are stateless prompts, and memory is session- and project-scoped. A persona that learns
something durable about this codebase — a build quirk, a convention, a place where the obvious
approach fails — has nowhere to put it that survives the run.

The shape worth copying is a per-persona memory directory in one of three scopes: **user** (shared
across projects), **project** (committed, shared with the team), and **local** (project-specific,
never committed). The third is the one that carries most of the practical value, and it maps onto the
`~/.aegis/personas/` vs `.aegis/personas/` split Aegis already has.

Two implementation constraints, both cheap and both easy to miss: normalize paths before the
containment check so `..` cannot escape the memory root, and sanitize the persona name for the
filesystem — namespaced names carry characters Windows rejects outright.

**Promote when** a persona is in repeated use on one project and its operator is re-explaining the
same context each run. Until then this is storage without a demonstrated reader.

Priority: Tier 4 — no fired trigger. Do not build speculatively.

---

## Verification Work

**Status: 6 open.** Every item here has its code already written and merged — nothing below is a
design or implementation task. Each is closed by running a live-model harness and recording the
result the item's closure condition names, not by writing more code. They are **not tiered**:
tiering answers "how urgent is this build," and there is no build left to prioritize.

**Two of the six are blocked on something other than a model server:** **P38.1** needs permission
to launch an unattended auto-approving agent, and **P62.8** is waiting on hardware (a backend
serving a >200,000-token window). The rest — **P80.4**, **P68.4**, **P68.5**, **P68.6** — need a
live-tier sitting with a stronger local model than what's historically been available here; see
each entry for specifics. **P66.22**, **P62.9** and **P65.2** closed by live evidence
2026-09-01/09-02 — full record in
[releases.md](releases/releases-01.md#roadmap-housekeeping-closed-items-migrated-from-roadmapmd-2026-09-03).

### P80.4 — `live_workflow`'s two standalone tests both need a stronger model than this machine has run

**Filed 2026-08-30**, from the audit's **C2** entry finally being executed. The tier had never been run;
running it three times found three product defects (**P79.2** the daemon released nothing unless it
exited through `ListenAndServe`; **P79.3** compaction's summarizer spent its whole budget on a thinking
preamble and returned empty on every cycle; **P79.4** the empty-answer nudge re-asked over the channel
that had just swallowed the answer), all fixed and re-verified live the same day — record in
[releases.md](releases/releases-01.md#comprehensive-architecture-and-security-audit-remediated-in-full-2026-08-30).
`TestLiveWorkflow`'s four subtests now pass against `aegis-qwen35-9b:32k`, `SecurityTriage` at 12/12
where it scored 3/12 before P79.4.

**What is left is not code.** The two standalone tests in that file decline to report rather than
passing vacuously, and both declines are about the model:

- **`TestLiveWorkflowCompactionPrefixCacheGate`** had two complaints and now has one. "No compaction
  actually ran, so this run measures nothing about the gate" is gone — P79.3 means compaction runs and
  succeeds. What remains is that this model abandons the fixture's 14-file read chain after five files,
  so the conversation never grows as designed and the test refuses to report a gate comparison from it.
  It needs a model that will follow a long mechanical chain. Note this is the _small_-window arm;
  **P62.8**'s large-window regime is a separate, still hardware-blocked question against the same test.
- **`TestLiveWorkflowForcedContextOverflow`** passed on one run and skipped on the next — the model's
  `write_file` call was not long enough to hit the 8,192-token ceiling the test needs, so it declined to
  measure. That is run-to-run variance in how much a live model chooses to emit, detected correctly.
  Making it deterministic means raising the fixture's requested line count or lowering the window until
  overflow is forced rather than hoped for, which _is_ a small code change — but one worth making with
  a run in front of you rather than blind.

Closure condition: one `live_workflow` run on a model that completes the 14-file chain, producing
either a prefix-cache gate comparison or a recorded reason the comparison is still not meaningful; plus
a forced-overflow fixture that overflows on every run rather than on some.

Priority: Verification — the code is in and green; what is missing is a model. Shares its blocker with
**P68.4**/**P68.6** (both "the local models available here sit below the band the measurement needs")
rather than with the hardware-blocked **P62.8**.

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
one context with no orchestration mis-route.

**Conformance: still unmet.** Every re-test has stalled short of an unattended verify-clean suite, but
each stall has moved the blocker further from the harness and closer to raw model throughput. Full
dated log (2026-07-21 through 2026-08-09) is in [releases.md](releases.md) (_P38.1 re-test log_).
Most recent result:

- **2026-08-09, LFM2.5-2.6B then qwen3:14b vs AiGateway — conformance still unmet, ten harness
  defects root-caused and shipped as P39.16.** The 2.6B produced zero files in two runs and is now
  refused by a pre-flight gate. The qwen3:14b arm built the **complete suite** — six files, ~35KB, all
  five content phases, every marker cleared — further than any prior run on this target, but
  verification did not pass unattended: `component-name-consistency`, `count-consistency` and
  `coverage-ledger-complete` remained after the bounded fix loop. Two of those three were then fixed
  structurally (P39.16); the third re-run hung before reaching a verdict. All ten defects were the same
  shape — a tool that held the information the model needed and returned an error without it.

**Direction (user, 2026-07-24):** the strongest lever is making local models **piecemeal both their
reads and their writes**, then finishing with a **quality-validation pass** — P39.12-P39.15 implement
this, and P39.16 (2026-08-09) extended it: piecemeal writing still failed while it went through
`edit_file`, because an anchored edit asks the model to _reproduce_ text rather than only produce it.
Handle-based tools (`fill_marker`, `edit_section`) remove the reproduction step and are what finally
made the fill loop reliable on a 14B model.

**Reproduce:** `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads outside
the workspace root); run
`aegis chat "threat model this repo" --skill threat-modeling --mode build --yes` (the prompt is
required). It prints a `phased mode` notice and resets context each phase.

**Closure condition:** the real suite's PENDING markers reach zero and `verify.py` / `lint_dfd.py` /
`inventory.py --check` all pass, **unattended, in one invocation**. Met once, 2026-07-24 on
FirewallRuleAnalyzer; not repeated since.

**2026-08-16: scheduled with the live-tier sitting and did not run.** The model server was reachable
and the target copy staged; the run itself was refused, because the recipe is an unattended agent
with auto-approved host shell (`--yes` plus `auto_approve_exec`) and the session driving it was not
permitted to launch that. Nothing about the item changed. This is a standing property of the recipe,
not a one-off: whoever runs it next either runs it by hand or grants the permission deliberately.

Priority: Verification — every load-bearing harness fix the re-tests have root-caused has shipped
(P39.5-P39.18, P47.1-P47.9, P52.12, P57.1). This item stays open only as the conformance **umbrella**,
closeable once a live built-in drive is confirmed to reach a verify-clean suite unattended, in one
invocation, on a local model. No code work remains; it is live-run tracking.

### P68.4 — The triage rubric's measuring band sits below the strongest local model

**Filed 2026-08-17, from a temperature A/B that measured nothing — twice.** P68.3 shipped a task that
ranks _models_ well (9b 10.7 vs 14b 2.7, complete separation at n=3). The attempt to use it for the
next question — do the sampling parameters `docs/local-model-tuning.md` recommends actually help? —
found it cannot rank _configurations_, because both available substrates sit against a rail:

| substrate             | temp 0.2      | temp 0.6   | reading                                        |
| --------------------- | ------------- | ---------- | ----------------------------------------------- |
| `aegis-qwen35-9b:32k` | 12, 12, 12    | 12, 12, 12 | **ceiling** — rubric exhausted                 |
| `qwen3:14b-32k-fix`   | 3, 3, 3, 3, 3 | 3, 3, 3    | **pinned low** — one repeated minimal strategy |

Both arms of both A/Bs are flat, and **neither is evidence that temperature does not matter** — a
saturated instrument returns exactly this pattern whether the variable matters or not.

Two instrument checks were run before concluding, and both came back clean, which is what makes the
"no headroom" reading the surviving one rather than a guess: the derived Modelfiles differ **only** in
`temperature` (`ollama show` confirms `num_ctx` and everything else carried), and all four derived
models still carry the **corrected** chat template.

**The substrate was removed from the machine on 2026-08-17, after this was filed**, which makes the
item harder rather than staler: `qwen3:14b-32k` and its corrected build are both gone, so the only
mid-range scorer either A/B had is no longer available. What remains locally is
`aegis-qwen35-9b:32k` (saturates at 12/12), `qwen2.5-coder:1.5b` (historically zero tool calls on the
older tier — see [providers.md](../docs/providers.md)) and **`gemma4:12b`, which is untested here and
is the obvious first thing to score**: its manifest advertises tools and its template is clean by the
P68.2 detector, so it is a candidate mid-range substrate rather than a known one. Re-pulling
`qwen3:14b` and rebuilding the corrected variant per
[docs/local-model-tuning.md](../docs/local-model-tuning.md) is the fallback, and is cheap.

**What it needs:** a harder tier of criteria so a strong model has somewhere left to go, and a floor
that a weak model clears by more than one repeated strategy. Candidates, none costed:

- a sixth planted issue that only a cross-module data-flow trace finds (the current hardest, the
  `wire.py` → `jobs.py` pickle, is the one criterion the 9b sometimes misses — so the difficulty
  gradient is right, there is just not enough of it above);
- severity grading, currently parsed and discarded — a finding reported at the wrong severity is
  presently worth the same as one reported correctly;
- points for _not_ touching the three files the task never mentions, which the 14b family edits.

**Until this lands, `docs/local-model-tuning.md`'s sampling section stays labelled reasoned-not-
measured**, and it says so in the document. That is the honest state: two experiments were run and
both were void, which is different from "tested and found not to matter", and the page must not drift
into implying the latter.

### P68.5 — P52.16's `toolResultEcho` measurement was taken through a defective template

**Filed 2026-08-17.** P52.16's echo experiment — 32/40 bare → 38/40 echoed, the measurement the whole
`toolResultEcho` mechanism rests on — was run on **`qwen2.5-coder:1.5b`**, which P68.2's detector
flags as shipping the `else if … .ToolCalls` template. That experiment measured tool-result
_correlation_ through a renderer that was deleting the calls being correlated, which is close to the
worst possible confound for it: the echo's stated purpose is carrying an association "in content
where the protocol cannot carry it in metadata", and the protocol was losing even more than assumed.

Nothing is retracted here. The +15pp may well survive — the echo could be _more_ valuable when the
call is missing entirely, not less — but the number as recorded describes a setup nobody would choose
today.

**What would close it:** re-run the 3-parallel-`read_file` attribution task, 40 trials per arm, on
`qwen2.5-coder:1.5b` with the P68.2 mitigation active, and again on a template-corrected build. This
is a probe rather than a workflow tier — cheap, and it does not need the live-tier sitting's setup.

Priority: Verification. It is the one re-run the 2026-08-17 sitting identified and did not do.

### P68.6 — The 14b family never produces the report, and nothing in the run says why

**Filed 2026-08-17, from P68.3's first live sittings.** Across six graded runs on `qwen3:14b-32k` and
its template-corrected build, the dominant failure is not finding and not fixing — it is that
`findings.json` **is never written, or is written naming 2 of 5 issues**, after the model has greped
the codebase extensively. One run made sixteen tool calls of which ten were consecutive `grep`s and
produced no artifact at all.

This is a model-behaviour observation, but it is not obviously _only_ that, which is why it is filed
rather than noted in a doc:

- the task names the output file explicitly and gives its schema in the prompt, so this is not an
  ambiguous instruction;
- the local prompt profile defers `edit_file` and exposes the handle-based editors, and the runs that
  do write use `write_file`/`multi_edit` — so it is worth checking whether a model that has decided
  to "write a JSON report" finds a tool that obviously does that, or bounces off the deferred surface
  and falls back to searching;
- P62.9 (closed) had an unresolved watch item about exactly this class of detour, and its `tool_search`
  signal was unobserved at n=5 across two sittings before it closed.

**Both models were removed from the machine on 2026-08-17**, so this is not reproducible locally
without re-pulling `qwen3:14b` — worth knowing before someone plans a sitting around it. The
behaviour is recorded in enough detail above to be recognised if it recurs on another model, and
whether it is Qwen3-specific or general is itself now an open question.

**What would close it:** read one such run's trace — **P68.1** (shipped 2026-08-22) means a future run's
data dir can now survive and be read with `aegis sessions trace <id>` — and establish whether the
model ever attempted a write tool and failed, or never selected one. Those are an Aegis problem and a
model problem respectively, and the run as recorded cannot tell them apart.

### P62.8 — The prefix-cache gate's large-window regime has never been measured

`compaction.shouldPrune` has two regimes: below `largeContextWindowThreshold` (200,000) it fires at a
25%-free ratio; above it, a fixed 40k buffer, which on a large window places the prune much earlier in
relative terms than anything measured so far. Everything known about this gate comes from a
24,576-token window (the ratio branch only), and P62.2's history is a specific warning against
generalising from it — the same fixture gave opposite verdicts before and after an instrument fix,
because what mattered was _where in the window_ the prune landed relative to the backend's
context-shifting point. The buffer branch changes exactly that relationship and is unmeasured. The
gate itself needs no new code — this is purely a measurement gap.

**Why parked rather than queued.** Needs a backend serving a >200,000-token window. Models on hand top
out at 40,960 (qwen3:14b) and 262,144 (gemma4:12b, but a 200k+ KV cache on 16GB VRAM / 16GB system RAM
is swap-bound, so it would measure paging rather than the gate). Hardware block, not a design question.

**How to run it when hardware allows:**
`AEGIS_EVAL_MODEL=<model> go test -tags live_workflow -count=1 ./internal/eval/ -run
TestLiveWorkflowCompactionPrefixCacheGate -v`, with `compactionNumCtx` raised past 200,000 and
`writeCompactionFixture`'s per-file payload scaled up so the chain still crosses the trigger.

Priority: Verification — no trigger, no user impact, blocked on hardware rather than on any decision
or any remaining code.

---

For shipped feature history, batch origins, refutation records, competitive-landscape review and the
full gap analysis, see [releases.md](releases.md).
