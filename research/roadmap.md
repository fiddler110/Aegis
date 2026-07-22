# Aegis Capability Roadmap

**Last updated:** 2026-07-21

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 1 actionable (P38.1 conformance umbrella) + P39.9 (native-adapter half investigated 2026-07-21,
adapter exonerated — residual is repro-gated) + 2 parked (Tier 4).

**Codebase review 2026-07-21** added P40.1–P40.6, **all shipped 2026-07-21**: **P40.1** (Tier 1) a plan-mode
secret-leak via `env`/`printenv` in the read-only shell allowlist; **P40.2–P40.4** (Tier 2) file-mode-clobber on write,
`read_file` bounded-read allocation, and repo-root `*.err` cleanup; **P40.5–P40.6** (Tier 3) decomposing
the 4.7K-line `tui.go` (now 2.3K + `view.go`/`stream.go`/`update.go`) and the nudge/guard bookkeeping in
`engine.Run`. See [releases.md](releases.md#latest-changes).

**P39.5, P39.6, P39.7, P39.8 shipped 2026-07-21** (code + unit tests — see
[releases.md](releases.md#latest-changes)); **P39.9 partially shipped** (the `/v1`-can't-send-`num_ctx`
warning half). What remains open:

- **P38.1** (Tier 2) — the conformance **umbrella**. The four load-bearing harness fixes (P39.5–P39.8) are now
  implemented, but P38.1 closes only once a **live re-test confirms** the built-in `--skill` drive reaches a
  verify-clean suite on a local model — the fixes are unit-tested, not yet live-verified end-to-end. This is
  the next concrete step: re-run the FirewallRiskRater / AiGateway drive on `qwen3.6:35b-a3b` (or `gpt-oss:20b`)
  and confirm (a) the P39.5 preamble compaction keeps the per-turn window bounded, (b) the P39.7 nudge unsticks
  stalls, (c) the P39.6 verify loop catches the consistency faults, (d) the P39.8 latch stops the summarizer
  waste. An interim external wrapper is parked as **P38.8**.
- **P39.9** (Tier 3) — **investigated 2026-07-21.** The `/v1`+`num_ctx` warning shipped; the native-adapter
  "no tool call after 8+ min" half was **reproduced against and disproven for** the available models
  (gpt-oss:20b, qwen3:14b — tool calls emit promptly, `think` on/off, even under prompt overflow). The
  residual is not an adapter tool-call defect but (i) a prefill-latency observability gap and (ii) a
  `server/contextwindow.go` `/api/ps`-verification lead; both want a constrained-VRAM (or `qwen3.6-fast-32k`)
  repro before coding. See the P39.9 body.
- **P38.8** (Tier 4) — external per-phase threat-model wrapper, parked as a recorded interim workaround.
- **P25.9** (Tier 4) — per-session scoping of the remaining daemon-singleton services (`lsp.Manager`).
  Parked pending demand; do not build speculatively.

A handful of unfiled **leads** (condensed under Tier 2/Tier 3 below) capture mechanical follow-ups worth
their own item when a concrete need appears.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.  
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.  
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).  
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** 0 open. **P40.1 shipped 2026-07-21** — see [releases.md](releases.md#latest-changes).

*(P40.1 — `env`/`printenv` dropped from the read-only shell allowlist, closing a plan-mode
provider-API-key leak. The **related design caution** — `readOnlyShellCommand` is a lexical scan that
does not parse quotes, so resist growing `readOnlyShellArgv0`/`readOnlyGitSubcommands` with any
binary/subcommand that takes an `--exec`/`-e`/format-string escape hatch; the `git -c` exclusion is the
pattern to follow — carries forward as standing guidance, not an open item.)*

---

## Open Work — Tier 2

**Status:** 1 open — **P38.1** (conformance umbrella; the four load-bearing fixes are shipped, closes once a
live re-test confirms a verify-clean drive). **P39.7 and P39.6 shipped 2026-07-21** — see
[releases.md](releases.md#latest-changes).

*(P39.7 no-progress guard and P39.6 phase-6-verify-in-drive shipped 2026-07-21 — full write-ups in
[releases.md](releases.md#latest-changes).)*

### P38.1 — Non-orchestrated, single-context threat-model build (primary path for local models)

The threat-modeling skill's primary build is a single-context linear build the driving model runs itself —
no sub-agents, no `agent`-tool orchestration. Context stays bounded by levers that already exist (SKILL.md
§4): `recon.py`'s ~11KB digest, P36.2 pruning of spent write/read payloads, incremental section-at-a-time
writes, and the deterministic P37 scripts. `scaffold.py` (P38.4) pre-writes all seven files from the
skeletons with real structure + a unique `<!-- PENDING: <section> -->` marker per fillable section, so the
model fills sections instead of authoring structure.

**Mechanism: live-confirmed.** In the 2026-07-21 re-test (qwen3:14b vs AiGateway, `aegis chat --skill
threat-modeling` drive-to-completion) the model ran `recon.py` → `scaffold.py` and wrote all seven files in
one context with no orchestration mis-route — the P36.3-era failure that killed every prior run is gone.

**Conformance: still unmet on qwen3:14b.** The 14B model's weakness has moved from "authoring structure"
(P38.4 fixed that) to "incrementally filling it via `edit_file`": it scaffolds but can't drive the fill to a
verify-clean suite. The two *actionable* findings from that re-test — think-mode fabrication and the
identical-marker `replace_all` footgun — were split out and **shipped** as P38.6 and P38.7 (see
[releases.md](releases.md#latest-changes)).

**2026-07-21 re-test on `qwen3.6:35b-a3b` (environment gate lifted).** The MoE is now installed; run against
FirewallRiskRater (a Rust/Axum API + Flask frontend), both the `-deep` and a 32k-`num_ctx` `-fast` derivative.
Findings: (1) the **mechanism re-confirms** — `recon.py` → `scaffold.py` → seven files in one context, no
mis-route, and the architecture doc it produced was genuinely high quality (correct anchors, deployment
class, security-infra inventory); (2) but the autonomous `--skill` drive still does **not** converge to a
verify-clean suite — one scaffolded resume made **86 tool calls across 3 drive iterations and cleared 0 of 23
`PENDING` markers**, because the ~9K-token SKILL.md preload rides *every* turn (`prompt_bytes≈31534` at turn 0)
and fills the 32K local window before the model can `edit_file` (→ **P39.5**); (3) the native Ollama adapter
emitted no tool call at all, forcing the legacy `/v1` adapter + a `num_ctx 32768` modelfile (→ **P39.9**);
(4) compaction/`output_guard` return empty on this model (42× `summarizer returned empty output` in the daemon
log, → **P39.8**); (5) the finished suite carried duplicate threat IDs, tier↔prerequisite mismatches and stale
counts that no autonomous verify pass caught (→ **P39.6**). Driving the *same* model one phase at a time
**without** the preload completed all seven files and verified clean after a fix loop — proof the blocker is
harness-side context bounding, **not** model capability. Full evidence + reference wrapper: FirewallRiskRater
`tools/THREAT-MODEL-AUTOMATION.md` (recorded as **P38.8**).

**2026-07-21 corroboration (`gpt-oss:20b` MoE vs AiGateway).** A second model/target pair reproduces the same
harness-side wall. `gpt-oss:20b` is the **first local model to pass `aegis doctor --deep`** (the synthetic
structured multi-turn fill probe) where qwen3:14b, gemma4:12b and mythos-sec:24b all fail it — yet passing
`--deep` did **not** predict a verify-clean build, because the probe never exercises the P39.5 SKILL.md-preload
pressure (it is a necessary-not-sufficient gate, not a conformance signal). Three `--skill` runs against
AiGateway: (1) pointed at a *pre-existing* complete suite, it ran `scaffold.py` as a no-op and then
**confabulated** a full build+verify report (0 `edit_file`, verify scripts never run) — an output-text analogue
of the P38.6 think fabrication; (2) on a clean fresh scaffold it filled **0 of 35 `PENDING` markers** and
yielded 3× with markers still present — a second instance of the **P39.7** "announce-then-yield" stall; (3)
adding an explicit "one section per turn, act now via `edit_file`" preamble to the prompt **unstuck the fill**
(first real `edit_file` landed) before it snagged on two lower-level faults — `scaffold.py .` dumping skeletons
into the repo root (bad run-dir + malformed `--framework stride-a` args) and an Ollama tool-call JSON parse
error on a rich markdown-table `new_string` (invalid `\'` escape + a non-ASCII hyphen). Takeaways: the preamble
result is direct corroboration that **P39.7's "act now" nudge is the right lever**, on a second model; and
`gpt-oss:20b`'s tool-call serialization is fragile on large `edit_file` payloads (relates to **P39.9**).

P36.2 (pruning that keeps a single-context linear build inside the window) is the load-bearing mechanism
here and is **partially confirmed live** (a 33-call run held inside ~44K input tokens with no overflow); its
definitive confirmation rides on a scaffolded, verify-clean re-test measured through P38.3's per-turn usage
telemetry.

Priority: Tier 2 — the environment gate is **lifted**, the re-test is done, and the four root-caused
harness fixes (**P39.5–P39.8**) are now **shipped** (code + unit tests, 2026-07-21). This item stays open as
the conformance **umbrella** — closeable once a **live re-test confirms** the built-in `--skill` drive reaches
a verify-clean suite on a local model with those fixes in place (they are unit-tested, not yet live-verified
end-to-end). Not Tier 1 because it is live-run verification tracking, not independent build work.

**Lead (not yet filed):** the "accurate refusal, error-shaped" exit-code question for the SCA/secrets
scanners. P34.6 checked the *language*-targeted tools; nothing has swept the SCA/secrets tools for non-zero
exits that mean "nothing to do" rather than "I broke". No `### P<n>.<m>` heading yet.

---

**P40.2, P40.3, P40.4 shipped 2026-07-21** — see [releases.md](releases.md#latest-changes).

*(P40.2 — `write_file`/`edit_file` now preserve an existing file's permission bits on overwrite via a
`writePreservingMode` helper (`os.Stat` the target, reuse its mode; `newFileMode` 0o644 only for
create-new), so overwriting a `0700` script or a key/token file no longer drops the exec bit or widens to
world-readable. P40.3 — `read_file` switched from whole-file `strings.Split` to a `bufio.Scanner` with a
`splitLinesKeepFinal` split func that stops at `offset+limit`, so a bounded read allocates only what it
returns; a 10-case oracle test asserts byte-identical output to the old renderer. P40.4 — the stray
repo-root `*.err` files and the `*.gitignore` entry were already handled in the prior review commit.)*

---

## Open Work — Tier 3

**Status:** **P39.9** investigated 2026-07-21 — the native-adapter no-tool-call "hang" was reproduced against
and disproven for the available models; residual is a repro-gated observability + `/api/ps`-verification lead
(the `/v1`+`num_ctx` warning half already shipped). **P39.5, P39.8, P40.5, P40.6 shipped 2026-07-21** — see
[releases.md](releases.md#latest-changes). Plus open leads below.

*(P39.5 drive-loop context bounding shipped 2026-07-21 — the drive rewrites the first user message once after
the opening turn, swapping the ~9K-token SKILL.md body for a compact re-read pointer. The fuller "per-phase
reference loading" form is subsumed by this compact-pointer approach; revisit only if a live re-test shows the
model needs a specific phase's reference re-injected. Full write-up in
[releases.md](releases.md#latest-changes).)*

### P39.9 — Native-Ollama adapter emits no tool call on large skill-preload turns (`/v1` `num_ctx` half SHIPPED; (a) investigated 2026-07-21)

**Half shipped 2026-07-21.** (b, shipped) The `/v1` compat path can't send `num_ctx`, so a skill drive on it
overflows the modelfile default (`request (34774 tokens) exceeds the available context size (16384)`); `aegis
chat --skill` now probes the served window and warns up front with a runnable modelfile-derivative recipe
(`LegacyOllamaModelfileRecipe`) — see [releases.md](releases.md#latest-changes). (a, **investigated
2026-07-21 — adapter exonerated; residual reclassified below**) With `provider.default: ollama` (native
adapter) the original report was **no tool call and no run directory after 8+ min on the skill-preload turn**.

**Repro run** (this machine: RX 7900 GRE 16GB VRAM / 16GB RAM; native `/api/chat` with a big system prompt +
tool schemas + a "call the tool now" user message, faithfully mirroring the adapter's wire shape):

| model | think | num_ctx | prompt | result |
|---|---|---|---|---|
| gpt-oss:20b | false | 32768 | ~10.6K tok | header 47s (prefill 10668 tok), **tool call at 48.5s** ✓ |
| qwen3:14b | false | 32768 | ~10.6K tok | header 67s, **instant tool call** ✓ |
| qwen3:14b | true | 32768 | ~10.6K tok | header 2.6s (warm), **tool call 9.2s** ✓ |
| qwen3:14b | false | 32768 | ~34K tok (**overflow**) | silently truncated to prompt_eval=**28577**, header 153s, **tool call still correct** ✓ |

`/api/ps` after the 32768 request reported `context_length=32768`, fully in VRAM (`size_vram==size`).

**Findings:**
1. **The native adapter is not structurally broken.** It emits tool calls promptly (once prefill completes)
   on two tool-capable models, with `think` on *and* off (the default is `think=false` per
   `providerfactory.buildOne`), and even under prompt overflow. The "think-mode? / oversized-prompt?"
   hypotheses in the prior note are **ruled out** for the models tested.
2. **Prefill withholds the response header for its entire duration** (153s for ~28K tokens here; it scales
   with prompt size and model). The default `provider.response_header_timeout` is **5 min**
   (`sse.DefaultResponseHeaderTimeout`), and the adapter already rewraps a prefill that exceeds it into an
   actionable header-timeout error (P35.6). So an *8-min* wait implies either a raised timeout **or** the
   specific failing model (`qwen3.6-fast-32k`, not installed here) generating non-tool content — **not** an
   adapter tool-call defect. The user-facing gap is *observability*: a multi-minute prefill is
   indistinguishable from a hang because nothing is emitted until the header arrives.
3. **Silent front-truncation is real and reproduced** (34774 → 28577), but on the tested models the tool
   schemas survived and tool-calling still worked; the hazard is the *system-prompt / skill* front being
   dropped, which degrades instruction-following — exactly what **P39.5** (preamble compaction to bound the
   per-turn window) already targets.

**Ready-fix lead surfaced by the investigation** — `server/contextwindow.go:32-38` trusts native
`num_ctx = cfgWin` as authoritative and **skips the `/api/ps` probe** ("the served window is exactly what's
configured"). That assumption held on this machine (allocation matched, fully in VRAM), but on
VRAM-constrained hardware Ollama can allocate *less* than the requested `num_ctx` (or offload KV/layers to
CPU, turning prefill into the multi-minute wait of finding 2) — either reintroduces the silent truncation the
daemon then can't see. A ready fix: still call `ollamainfo.Detect` (reads `/api/ps`) for the native+`cfgWin>0`
case and, when the loaded allocation is below `cfgWin`, treat the loaded value as effective and emit the same
warning `applyDetectedWindow` already logs for the compat path. **Not shipped** — needs a constrained-VRAM
repro to validate, since on this machine the allocation matched exactly. Relates to P35.9/P39.3.

Priority: Tier 3 — investigation done; the adapter tool-call defect is disproven for available models. What
remains is (i) the prefill-latency observability gap and (ii) the `/api/ps`-verification lead above, both of
which want a constrained-VRAM or `qwen3.6-fast-32k` repro before coding.

*(P39.8 weak-local-model compaction robustness shipped 2026-07-21 — the engine latches the LLM summarizer off
for the rest of a run after `summarizerGiveUpThreshold` cumulative failures, compacting deterministically
thereafter. Full write-up in [releases.md](releases.md#latest-changes).)*

**Lead — doc-inconsistency (surfaced building the P37 scripts):**
(a) **threat-ID form** — `references/skeletons/skeleton-stride.md` writes threat IDs as bare sequential
`T1`/`T2`, but `output-formats.md`'s coverage / Related-Threats examples use composite `T04.S` form; the
P37 scripts match both, but the docs should settle on one canonical form.
(b) **inventory YAML style** — `skeleton-inventory.md`'s example is block-style while directive #13 says
list entries are one-line, and `inventory.py` emits one-line flow mappings; the skeleton example should
match what the generator produces. Both cosmetic doc drift, not code bugs.

**Lead — `recon.py` (P37.1) depth follow-ups**, left out of v1 deliberately:
(a) **data-flow edge inference** — seed the DFD's `DF##` flows from import graphs / client instantiations
so phase 2 starts from real edges;
(b) **config-default resolution** — parse the actual bind-address default from the config struct /
`config.yaml` to settle the deployment class deterministically (and downgrade `EXPOSE`/`0.0.0.0` to
`internal-network` when the k8s `Service` is `NodePort`/`ClusterIP` with no TLS terminator, rather than
over-flagging `internet-facing`);
(c) **richer symbol extraction** — functions/methods and route→handler maps, optionally via
`ctags`/tree-sitter when on PATH;
(d) **target-commit in the sidecar** — let `inventory.py` take an optional `--target-dir`/`--repo` (or read
the commit from `0-assessment.md`) so a run directory kept outside the target repo still records the
analyzed code's commit.

**P40.5 and P40.6 shipped 2026-07-21** — behavior-preserving refactors, see
[releases.md](releases.md#latest-changes).

*(P40.5 — `internal/tui/tui.go` dropped from 4,731 to 2,285 lines via pure code motion into three new
same-package files: `view.go` (the `View`/`render*` layer, 733 lines), `stream.go` (`applyStreamBatch`/
`applyEvent` + tool-card management, 509 lines), and `update.go` (the `Update` message-routing switch,
1,249 lines). No logic changed; `go test ./internal/tui/...` stays green. The finer per-domain split of the
`Update` switch itself is left as opportunistic follow-up when TUI work is next in flight. P40.6 — the three
parallel nudge/guard counters in `engine.Run` (`guardRetries`, `zeroToolNudges`, `emptyAnswerNudges`) and
their trio of terminal retraction if-blocks collapsed into a `nudgeState` struct with a single `retractAll`
method; the eval golden transcripts show **no** diff, confirming behavior preservation.)*

---

## Open Work — Tier 4

### P38.8 — External per-phase threat-model wrapper as interim autonomous-build workaround (parked)

Until P39.5–P39.7 land, a completed, verify-clean suite is reachable **today** by driving Aegis outside the
`--skill` loop, one phase at a time with bounded context. A reference implementation is recorded at
`tools/aegis-threatmodel.sh` (+ `tools/THREAT-MODEL-AUTOMATION.md`) in the FirewallRiskRater repo: it runs
`scaffold.py`, then a small **skill-free** `aegis chat` per phase (architecture → DFD → STRIDE → findings →
assessment), re-invoking while a phase's file still has `PENDING` markers with an "act now" preamble, then
runs the P37 checks and loops their failures back to the model until clean. Because each turn's context is
just the prompt + that phase's files, the compaction wedge (P39.8) and preload bloat (P39.5) never trigger.
Validated per-phase on `qwen3.6-fast-32k`, 2026-07-21 — all five content phases completed and the suite
verified clean after the fix loop.

Priority: Tier 4 — a workaround that lives *outside* the harness and duplicates what the drive loop should do
natively. Recorded so the working recipe isn't lost; **superseded by P39.5 + P39.6 + P39.7** once the built-in
path converges. Do not invest in it beyond the reference.

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of the six daemon-singleton services were per-session-scoped when P25.9 first shipped; `lsp.Manager`
was deliberately left as a shared singleton — its per-session resource-growth tradeoff was judged worse
than the isolation gap. Parked pending a concrete multi-tenant need.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
