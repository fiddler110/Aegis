# Aegis Capability Roadmap

**Last updated:** 2026-07-30

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** the **P52.x full-stack review batch** (filed 2026-07-30, **11 open** of 17 —
**P52.1** Tier 1, **P52.3**/**P52.4**/**P52.8**/**P52.10** Tier 2, **P52.12**/**P52.13** Tier 3,
**P52.14**-**P52.17** Tier 4 measure-first; **P52.2**, **P52.5**, **P52.6**, **P52.7**, **P52.9** and
**P52.11** shipped 2026-07-30) — see the batch summary and its
build-order table below, which is the authoritative sequence for future sessions. Plus **P38.1**
(Tier 2 umbrella) + **P51.1** (Tier 1, macOS seatbelt profile — **shipped**
2026-07-30) + the
remaining **P49.x repo-map / index enrichment batch** (**P49.3**/**P49.4** Tier 4, both
measure-first — the structural head **P49.1** and the on-demand query tool **P49.2** have **shipped**
2026-07-29; see [releases.md](releases.md)) +
2 parked (Tier 4: **P38.8**, **P25.9**). **P50.5** is **superseded by P52.12**. Everything else filed since the last cleanup — the P39.10-P39.15
threat-model harness fixes, the P40.x TUI/UX batch, P41.1, P44.1, P45.1, P45.2, the P46.x
codex-build track, **P48.1** (config-test hermeticity), **P47.6** (drive model-selection guidance,
doc-only), **P47.10** (CLI/TUI parity, resolved as documentation), **P49.1**/**P49.2** (repo-map
import edges + on-demand `repomap` query tool), and the entire **P47.x phased-drive stability batch**
(**P47.1**, **P47.2**, **P47.3**, **P47.4**, **P47.5**, **P47.7**, **P47.8**, **P47.9**) — has
**shipped**; see [releases.md](releases.md) for what each one did.

**Next batch — P47.x phased-drive stability (filed 2026-07-24):** the 2026-07-24
FirewallRuleAnalyzer run reached a **verify-clean suite** on `qwen3.6:35b-a3b-fast` (all
`verify.py`/`lint_dfd.py`/`inventory.py --check` passing) — P38.1's mechanism/conformance closure
condition — but only after **three manual re-invocations**: the CLI `chat --skill` drive engine has
no proactive compaction wired in, so each phase's context grew until Ollama hard-rejected the
request (observed 173,816 vs a 131,072 window) and the drive aborted. P47.1-P47.6 make that same
run succeed in **one unattended invocation**; tackle in number order. The whole batch has now
**shipped**: **P47.1** (wire proactive compaction into the CLI drive engine), **P47.2** (treat a
mid-phase context overflow as a resumable phase reset, not a fatal abort), **P47.3** (stop content
phases burning context on manual self-verification), **P47.4** (make in-phase continuations
near-stateless to cap peak context), and **P47.5** (auto-size + auto-escalate the per-phase context
window) — see [releases.md](releases.md).

**Batch extension — phase-6 remediation resilience (filed 2026-07-27):** the first live run of the
ec0127c hollow-report checks + afd6764 self-heal (FirewallRiskRater, `qwen3.6:35b-a3b-fast`)
confirmed both shipped fixes work — self-heal auto-deployed the new `verify.py` and the checks
turned a false-passing hollow suite into `12 passed, 2 failed` with file:line. But with the checks
now correctly failing, the phase-6 verify/quality remediation loop exposed the same class of gaps
P47.1-P47.3 fixed for content phases, one tier down: it had none of them. **P47.7** (extend the
P47.2 overflow-reset to the phase-6 loop), **P47.8** (carry the P39.14 anti-monolithic-write
guardrail into the phase-6 prompts), and **P47.9** (route hollow-body failures back through the
owning content phase) have all **shipped** (see [releases.md](releases.md)); **P47.10** (the
CLI-only drive-to-completion / TUI `/threat-model` parity question) was **resolved 2026-07-27 as
documentation** — see [releases.md](releases.md).

**New batch — P49.x repo-map / index enrichment (filed 2026-07-29):** the `index` functionality
(`internal/repomap`) today gives the model a flat, regex-extracted file→symbol list injected as
`<repo_map>`. Prompted by a review of nanonets/graft (tree-sitter structural pass + LLM concept
nodes + on-demand query tools), this batch evolves the same capability *inside the single-Go-binary,
no-external-runtime constraint* rather than vendoring graft's Node package. Sequence head **P49.1**
(import/dependency edges in the map, Tier 2, cheap + self-contained) and **P49.2** (an on-demand
`repomap` query tool — skeleton/importers/map — Tier 3) **shipped 2026-07-29** (see
[releases.md](releases.md)); the structural tier now exists. The remaining two follow *only if it
doesn't close the discovery gap*: **P49.3** (LSP-backed symbol precision, Tier 4, measure-first) and
**P49.4** (LLM-summarized concept nodes, Tier 4, speculative). Both are measure-first — neither has a
live-run trigger yet, so do not build them speculatively; build only once P49.1/P49.2 have
demonstrably fallen short on a live run.

**New batch — P50.x phased-drive determinism & resilience (filed 2026-07-30):** the 2026-07-30
FirewallRiskRater run *did* reach a verify-clean, quality-stamped suite in one unattended
invocation (P38.1's closure condition), but the drive to get there surfaced three concrete
weaknesses worth hardening before the mechanism is called done. (1) **No backend liveness** — the
run's real stall was Ollama dying silently mid-phase with the `aegis` process left with nothing to
retry against (the retry layer only covers *synchronous* Stream errors and gives up after ~4
backoffs; a mid-stream `model runner has unexpectedly stopped` or a longer outage kills the drive
with no resume). (2) **The model invents non-canonical IDs** (`T1.S`, `T2.T` …) that don't match the
plain `T1..Tn` in the analysis file, so `coverage-matches-related-threats` bounces for an extra
verify round before it self-heals — the exact doc-drift the Tier-3 "threat-ID form" lead already
names. (3) **The P38.1 quality pass can regress a clean suite** — asked to re-tier a finding it
hand-renumbered `FIND-##`, duplicated `FIND-07`, and hit the inner 40-round step limit mid-edit;
only the round-2 mechanical recheck caught it. Sequence: **P50.1** (backend liveness + resumable
reset + heartbeat, Tier 1), **P50.2** (deterministic `normalize_ids.py` canonicalizer so ID
renumbering is scripted, not LLM-authored — closes weakness 2 *and* the root cause of 3, Tier 2),
**P50.3** (snapshot-and-rollback guard so the quality pass can never ship a suite that verifies worse
than the mechanically-clean state it started from, Tier 2), **P50.4** (a live per-turn progress
heartbeat so a hung/dead phase is observable — the precondition for any external supervision, Tier
2). **P50.5** (wire the phased drive into the TUI `/threat-model`) revisits the P47.10 decision and
stays a Tier-3 lead, not this batch's work. **P50.1-P50.4 all shipped 2026-07-30** (code + tests
green, P50.2 validated end-to-end on the real FirewallRiskRater suite) — see [releases.md](releases.md);
their live-run confirmation folds into the P38.1 umbrella.

**New batch — P52.x full-stack review (filed 2026-07-30):** a comprehensive review of the whole
application — TUI, web UI, daemon/engine, the Ollama seam, the threat-model skill, and the
document-authoring surface — filed 17 items. Three themes came out of it. (1) **The local-model
reliability work is CLI-only**: everything P47.x/P50.x built (fresh context per phase, hollow
re-entry, backend liveness, context escalation, no-progress guard) lives in
`internal/cli/chat_phased.go` and is unreachable from the TUI and web UI, which still run the
single-context drive the phased drive exists *because* it fails — **P52.12** generalizes the earlier
**P50.5** lead from "wire it into the TUI" to "lift it into the daemon so every client gets it".
(2) **Context-window detection is keyed to the wrong model**: `contextwindow.go` resolves one
server-wide window for `cfg.Provider.Model`, but `resolveModel`/`turnModel` route individual turns to
a persona-pinned or `small_model` target, so the engine can be handed the wrong window and let Ollama
silently truncate the system prompt — the exact failure `ollamainfo` exists to prevent, reintroduced
through the per-session model path (**P52.1**/**P52.4**). (3) **`latex_build` is the one tool that
escapes workspace confinement** — it shells out to a TeX compiler with no `-no-shell-escape` and
inherits `openin_any=a`, so a model-authored `.tex` can read any file on the host (**P52.2**). The
review also confirmed the parts that are *clean* and need no work: daemon auth (constant-time compare,
lockout backoff, page-token exchange), the web UI (strict CSP, zero `innerHTML`, one runtime dep), and
`sandbox.ValidatePath`'s symlink-aware confinement.

**P52.x build order.** Tier order is the priority; within a tier, build in the sequence below —
it front-loads the two correctness/security items, then the cheap self-contained wins, and defers
the two larger structural items until their dependencies exist. Items marked *measure-first* must
not be built speculatively.

| Order | Item | Tier | Why here |
|---|---|---|---|
| 1 | **P52.1** per-model context window | 1 | Silent prompt truncation; correctness, contained — **next up** |
| 2 | **P52.2** `latex_build` confinement | 1 | **SHIPPED 2026-07-30** |
| 3 | **P52.3** tool-failure circuit breaker | 2 | Biggest remaining local-model stall; no dependency |
| 4 | **P52.4** per-request `num_ctx` | 2 | Adapter half of P52.1 — build immediately after it |
| 5 | **P52.5** `think`-rejection latch | 2 | **SHIPPED 2026-07-30** |
| 6 | **P52.6** `RaiseContextWindow` mutex | 2 | **SHIPPED 2026-07-30** — P52.12 is now unblocked on this axis |
| 7 | **P52.7** suite-wide hollowness check | 2 | **SHIPPED 2026-07-30** |
| 8 | **P52.8** threat-model substance floor | 2 | Builds on P52.7's marker manifest — **manifest now exists** |
| 9 | **P52.9** `yaml_validate` tool | 2 | **SHIPPED 2026-07-30** |
| 10 | **P52.10** `latex_build` bib pass | 2 | Makes the shipped biblatex preamble actually work |
| 11 | **P52.11** documentation-as-code skill | 2 | **SHIPPED 2026-07-30** |
| 12 | **P52.13** `workspace.additional_roots` | 3 | Unblocks cross-repo research→document |
| 13 | **P52.12** lift phased drive into daemon | 3 | Largest surface; P52.6 has now landed |
| 14 | **P52.14** session-scoped loop detector | 4 | *measure-first* |
| 15 | **P52.15** wall-clock run budget | 4 | *measure-first* |
| 16 | **P52.16** native tool-result disambiguation | 4 | *measure-first* — needs a live A/B |
| 17 | **P52.17** auto tool-calling probe on model switch | 4 | Polish; no trigger yet |

**Batch 1 shipped 2026-07-30 (parallel).** P52.2, P52.5+P52.6, P52.7 and P52.9 were built
concurrently as four file-disjoint lanes and reconciled in one pass — see [releases.md](releases.md).
Two findings from that batch change later work: **(a)** P52.2's prescribed `openin_any=p` fix is a
**no-op on TeX Live 2026** (upstream made the setting inert), so the confinement is carried by a
static source scan instead — which matters for **P52.10**, whose `biber`/`bibtex` subprocesses the
scan does not cover; **(b)** P52.7 added check 15 rather than renaming check 12, because
`chat_phased.go` routes on the literal check name.

**Remaining P38.1 debt:** the in-harness phased-drive convergence tracking (see the P38.1 body). The
2026-07-23 gpt-oss:20b housekeeping is now **closed** — **P39.10**/**P39.11** were already coded,
shipped, and verified live; as of 2026-07-27 they also have their releases.md entry and regression
tests (`TestDriveOraclesSkipBuiltinSkillsSubtree` + `TestDriveOraclesSkipRealMaterializedBuiltins`
cover the oracle skip of a materialized-skill PENDING marker; `chat --skill` workspace materialization
is covered by `internal/skills/embedded_test.go`).

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** 1 open — **P52.1** (per-model context window), filed 2026-07-30 by the P52.x full-stack
review and now the batch's next item. **P52.2** (`latex_build` workspace confinement) **shipped
2026-07-30**. **P51.1** (macOS seatbelt profile runs no commands) **shipped 2026-07-30**;
**P50.1** (backend liveness + resumable reset), the P50.x batch head, **shipped 2026-07-30** (see
[releases.md](releases.md)); batch head **P47.1** (wire proactive compaction into the CLI
`chat --skill` drive engine) **shipped** 2026-07-24.

### P52.1 — Context window is detected for the global model, not the model the turn actually runs on

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
root, validated through `sandbox.ValidatePath`. See [releases.md](releases.md) for the covered
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

## Open Work — Tier 2

**Status:** 5 open — **P38.1** (threat-model conformance umbrella, live-run verification tracking
rather than independent build work) plus the remainder of the P52.x full-stack review's Tier-2 batch:
**P52.3** (tool-failure circuit breaker), **P52.4** (per-request `num_ctx`), **P52.8** (threat-model
substance floor — **P52.7**'s manifest now exists, so it is unblocked), **P52.10** (`latex_build`
bibliography pass). **P52.5** (`think`-rejection latch), **P52.6** (`RaiseContextWindow`
synchronization), **P52.7** (suite-wide hollowness check), **P52.9** (`yaml_validate` tool) and
**P52.11** (documentation-as-code skill) all **shipped 2026-07-30** — see
[releases.md](releases.md). Build in the order given in the Status section's P52.x table. The self-contained batch items **P47.1**, **P47.2**,
**P47.3**, **P47.4**, **P47.5**, **P47.7**, **P47.8**, **P47.9** (the full P47.x phased-drive
stability batch), **P48.1** (config-test hermeticity), **P49.1** (repo-map import edges, the
P49.x batch head), and **P50.2**/**P50.3**/**P50.4** (the P50.x phased-drive determinism batch —
deterministic ID canonicalizer, quality-pass rollback guard, live heartbeat) have all shipped — see
[releases.md](releases.md).

### P52.3 — Consecutive-tool-failure circuit breaker (the loop the loop detector cannot see)

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

### P52.4 — Per-request `num_ctx` (stop a small-model turn allocating the primary model's KV cache)

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
it". See [releases.md](releases.md).

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
[releases.md](releases.md).

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
file, rather than falling through to the generic verify-fix turn. See [releases.md](releases.md).

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

### P52.8 — Mechanical substance floor for threat-model content (anti-`TBD`)

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
[releases.md](releases.md).

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

### P52.10 — `latex_build` can never resolve citations (the biblatex preamble is decorative)

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
  regression tests (see Status above); this housekeeping is closed. With the scripts reachable,
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
  P39.6 verify loop, the P38.1 quality pass) — only the context lifetime changed. Lives in
  `internal/cli/chat_phased.go`; `phasePlanFor` gates it to threat-modeling (every other
  PENDING-driven skill keeps the generic single-context drive), and `AEGIS_SKILL_DRIVE=linear`
  forces the old path for comparison. Unit-tested for phase sequencing/completion/prompt wiring;
  **live convergence against a local model is the remaining validation** (see next steps).

- **2026-07-24, qwen3.6:35b-a3b-fast vs FirewallRuleAnalyzer (phased drive, stability):** the
  phased drive **reached a verify-clean suite** — 23 threats / 22 findings across 9 components, all
  `verify.py`/`lint_dfd.py`/`inventory.py --check` passing, content grounded in real file:line
  evidence and its own quality pass catching genuine inaccuracies — i.e. the mechanism/conformance
  closure condition below was **met**. But it took **three manual re-invocations**: the CLI
  `chat --skill` drive engine wires no proactive compaction (`internal/cli/chat.go:199` sets neither
  `ContextWindowTokens` nor `Compactor`, unlike the daemon at `internal/server/engine_build.go:279,288`),
  so each phase's context grew — the model re-reading files and recomputing STRIDE counts by hand —
  until Ollama hard-rejected the request and the drive aborted on a terminal
  `NewContextTruncationError` rather than a resumable stop. Root-caused into the **P47.x phased-drive
  stability batch** (P47.1-P47.6): single-invocation stability is now the bar, distinct from the
  mechanism closure already demonstrated here.

- **2026-07-27, qwen3.6:35b-a3b-fast vs FirewallRiskRater (hollow-report checks + self-heal,
  validated; phase-6 gap found):** first live run of the ec0127c hollow-report checks + afd6764
  self-heal, against a resumed suite whose `<!-- PENDING -->` markers were already deleted but whose
  finding bodies were empty. **Confirmed working:** self-heal auto-refreshed the stale project
  `verify.py` on launch (two `refreshed 1 stale built-in skill file(s)` notices — data dir +
  project), the three new checks turned the previously false-passing hollow suite (`11 passed, 0
  failed` on the old verifier) into `12 passed, 2 failed` with exact file:line, and the drive fixed
  the `no-duplicate-header-rows` failure live. **New gap:** with the checks now correctly failing,
  the phase-6 verify/quality remediation loop had to fix them — and it lacks the P47.2 overflow-reset
  and the P39.14 anti-monolithic-write guardrail the content phases carry, so the first big fill
  attempt (a whole-file `write_file` of the ~400-line `3-findings.md` to fill 15 empty bodies)
  truncated into a malformed tool call → context overflow → drive aborted **uncaught** (raw `ollama:
  response truncated at the context limit` with no `[notice: … resetting]`, no `.quality-stamp.json`,
  verify rounds 2/3 + the quality pass never ran). Fixed by **P47.7** (overflow-reset in phase 6,
  **shipped**) and **P47.8** (guardrail in the phase-6 prompts, **shipped**); **P47.9** (route
  hollow-body failures to the owning content phase) is the Tier-3 follow-up, **shipped 2026-07-30**,
  and the CLI-only drive vs TUI `/threat-model` parity is noted as **P47.10**.

Reproduce: `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads
outside the workspace root); run `aegis chat "threat model this repo" --skill threat-modeling --mode build --yes`
(the prompt is required — `aegis chat` errors with "no prompt provided" without one) — it now
prints a `phased mode` notice and resets context each phase. Closure condition: the real suite's
PENDING markers reach zero and `verify.py`/`lint_dfd.py`/`inventory.py --check` all pass (met
2026-07-24 on FirewallRuleAnalyzer; **unattended single-invocation** stability tracked by P47.x).

Priority: Tier 2 — every load-bearing harness fix the re-tests have root-caused (P39.5-P39.15) has
shipped. This item stays open only as the conformance **umbrella**, closeable once a live
built-in `--skill` drive is confirmed to reach a verify-clean suite on a local model. Not Tier 1
because it is live-run verification tracking, not independent build work.

**Lead (not yet filed):** the "accurate refusal, error-shaped" exit-code question for the
SCA/secrets scanners. P34.6 checked the *language*-targeted tools; nothing has swept the
SCA/secrets tools for non-zero exits that mean "nothing to do" rather than "I broke". No
`### P<n>.<m>` heading yet.

---

## Open Work — Tier 3

**Status:** 2 open — **P52.12** (lift the phased drive into the daemon, which **supersedes P50.5**)
and **P52.13** (`workspace.additional_roots`), both filed 2026-07-30 by the P52.x full-stack review.
Build **P52.13** first: it is smaller, independent, and unblocks the cross-repo document workflow.
P52.12's prerequisite **P52.6** **shipped 2026-07-30**, so that blocker is cleared. Of the earlier items — **P47.4** (phased-drive near-stateless continuations) and
**P47.9** (route hollow-body failures back through the owning content phase), the last two P47.x
batch items, **shipped 2026-07-30**; **P49.2** (on-demand repo-map query tool) **shipped 2026-07-29**
— see [releases.md](releases.md). Both P47.4 and P47.9 were built ahead of their measure-first
triggers (the roadmap called them build-only-if-the-cheaper-items-don't-hold) and each ships with an
escape hatch — `AEGIS_PHASE_CONV=growing` restores the pre-P47.4 growing conversation, and P47.9's
re-entry falls back to the generic verify-fix loop — so a live run can still measure whether they
earn their keep. The leads below are mechanical follow-ups worth their own item once a concrete need
appears.

### P52.13 — `workspace.additional_roots` (unblock the cross-repo research→document workflow)

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

### P52.12 — Lift the phased drive into the daemon (every client gets the local-model machinery) — supersedes P50.5

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

## Open Work — Tier 4

**Status:** the four P52.x items below join the existing P49.3/P49.4/P38.8/P25.9 set. All four are
**measure-first or trigger-less** — they came out of the 2026-07-30 full-stack review as real
observations, but none has a live-run failure attached yet. Do not build them speculatively; each
names the signal that should promote it.

### P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)

`newLoopDetector` is constructed **inside** `Run` (`engine.go:355-358`), so its window resets on every
call. In the TUI and web UI, each user turn is a separate `Run` — so a model that loops *across* user
turns (re-reading the same file every time the user nudges it, re-running the same failing command
after each correction) is never detected, no matter how many turns it repeats.

Fix would be to hoist the detector to session scope, plumbed through `engine.Options` as an optional
caller-owned detector so the daemon can hold one per session while the CLI keeps today's per-`Run`
behavior. The complication worth thinking through before building: a user *legitimately* asking for
the same tool call twice in two turns is not a loop, so a session-scoped detector likely needs a
higher threshold than the per-`Run` one, or needs to reset on any user message that isn't a bare
retry — which is a fuzzier judgment than the current mechanism makes.

**Promote when:** a live run shows a cross-turn loop that per-`Run` detection missed.

**Priority:** Tier 4 — real but unproven, and the false-positive risk is higher than the current
detector's.

### P52.15 — Wall-clock run budget (the dimension that actually hurts on local hardware)

Three budgets exist and none of them bound *time*: `BudgetUSD` is an explicit no-op for unpriced local
usage (`engine.go:541-550`), `MaxTokensPerRun` defaults to 0, and `MaxIterations` defaults to 40. On a
model measured at ~7 tok/s (the P38.1 note), 40 iterations is potentially hours before any safety
valve trips — and the user's actual constraint is almost always "don't spend more than N minutes on
this", which nothing expresses.

Fix would be a `MaxWallClockPerRun` option checked at the same points as the existing budget gates
(before each turn, and again before a tool round), aborting with a message that distinguishes "ran out
of time" from "ran out of iterations". It should interact sensibly with the phased drive, where the
meaningful unit is the *phase*, not the run — likely a per-phase budget rather than a global one, or
it will guillotine a long build mid-phase, which is worse than letting it finish.

**Promote when:** someone actually wants to bound a local run by time, or a phased build needs a
per-phase timeout. **Priority:** Tier 4 — cheap to build, but **P52.3** addresses the concrete stall
that motivates it, so build P52.3 first and see whether this is still wanted.

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

### P52.17 — Run the tool-calling probe automatically on first use of a newly selected model

`internal/toolcallprobe` and `aegis doctor` exist precisely because a model's Ollama manifest can
claim tool support while the model cannot actually speak the protocol (the qwen2.5-coder:1.5b
signature). The engine's P34.2 notice (`engine.go:627-632`) correctly detects the symptom after the
fact and points at `aegis doctor` — but only *after* a user has already spent a turn on a model that
was never going to work, and only when no tool call has succeeded all run.

Fix would be to run the smoke probe automatically the first time a session uses a model not seen
before (on `/model`, on a persona switch that pins a different model, and at first-run for the
configured default), caching the verdict per model. A failing probe would surface a clear up-front
warning rather than a mid-run diagnosis. The cost is one small model call per new model, and the
design question is where to cache the verdict so it survives a daemon restart without going stale
when a model is re-pulled.

**Promote when:** a new user hits the manifest-lies failure, or model switching becomes common enough
that the up-front cost is clearly worth it. **Priority:** Tier 4 — polish on a path that already
degrades gracefully and already names the fix.

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

### P49.4 — LLM-summarized concept nodes (graft pass-2 analog)

graft's second pass has an LLM summarize files into ~20–50 plain-English "concept nodes" with typed
links — the part that gives an agent *what a subsystem does*, not just its symbols. The analog here
would be an opt-in `aegis index --semantic` pass that groups files into concept summaries cached by
content hash. Two reasons this is last and speculative: (1) it costs an LLM pass per file (real
latency/token cost, cache-invalidation surface), unlike every other P49 item which is deterministic
and free; (2) it overlaps `internal/knowledge` and `internal/memory`, which already carry
project-level prose context — a semantic index might belong *there* rather than as a third store.

Priority: Tier 4 — speculative, **do not build until measured**: only if the deterministic
structural tiers (P49.1–P49.3) demonstrably fail to close the re-discovery gap, and only after
deciding whether the summaries live in a new store or extend `knowledge`/`memory`. No trigger yet.

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

### P38.8 — External per-phase threat-model wrapper as interim autonomous-build workaround (parked)

Until the built-in drive reliably converges, a completed, verify-clean suite is reachable **today**
by driving Aegis outside the `--skill` loop, one phase at a time with bounded context. A reference
implementation is recorded at `tools/aegis-threatmodel.sh` (+ `tools/THREAT-MODEL-AUTOMATION.md`)
in the FirewallRiskRater repo: it runs `scaffold.py`, then a small **skill-free** `aegis chat` per
phase (architecture → DFD → STRIDE → findings → assessment), re-invoking while a phase's file
still has `PENDING` markers with an "act now" preamble, then runs the P37 checks and loops their
failures back to the model until clean. Because each turn's context is just the prompt + that
phase's files, the compaction wedge and preload bloat that hit the built-in path never trigger.
Validated per-phase on `qwen3.6-fast-32k`, 2026-07-21 — all five content phases completed and the
suite verified clean after the fix loop.

Priority: Tier 4 — a workaround that lives *outside* the harness and duplicates what the drive
loop should do natively. Recorded so the working recipe isn't lost. **Its mechanism is now
implemented in-harness** (2026-07-24, `internal/cli/chat_phased.go`): the built-in
`chat --skill threat-modeling` drive resets context per phase exactly as this wrapper does, so the
external script is fully superseded for threat-modeling and needs no further investment. See the
P38.1 "in-harness phased drive" bullet above; this section stays only as the historical reference.

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of the six daemon-singleton services were per-session-scoped when P25.9 first shipped;
`lsp.Manager` was deliberately left as a shared singleton — its per-session resource-growth
tradeoff was judged worse than the isolation gap. Parked pending a concrete multi-tenant need.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
