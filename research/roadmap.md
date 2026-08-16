# Aegis Capability Roadmap

**Last updated:** 2026-08-15. This document tracks only **open** work and what's next. For
shipped-feature history, batch origins, pass-by-pass narrative, refutation records and full design
rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading with a
`Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so keep it when
adding items.

---

## Status

**31 open items: 25 build (Tier 1-4) + 6 verification-only.**

**2026-08-15: the P66 batch is a full-stack code review**, not a feature line. Six specialist
reviewers, an adversarial debate (advocate / refuter / arbitrator) and a static-analysis pass
produced 70 findings against HEAD `3c2b57b`, recorded in [CodeReview.md](../CodeReview.md) with
per-finding evidence. The 23 items filed below (**P66.1**-**P66.23**) carry every finding worth
acting on; each names the finding IDs it closes, so the review document is the rationale and this
document is the work. Two are Critical and four more are exploitable today — the first time this
roadmap has had a non-empty Tier 1 since the P55 line closed.

Tiers 1-4 are **build work** — every item there requires writing code that doesn't exist yet.
**Verification work** is its own track, not a tier: every item in it is code that is *already
written*, sitting behind one gate — a live-model run producing evidence the item's closure
condition names. Mixing the two under one tiering scheme was misleading a reader into treating
"go run a test" and "go design and build a feature" as the same kind of next action. See
[Verification Work](#verification-work) below.

- **Tier 1:** 1 — **P66.5**. (**P66.2** shipped 2026-08-15; **P66.1** and **P66.4**, the
  two Criticals, **P66.3**, the read-only tier, and **P66.6**, the approval dialog, shipped
  2026-08-16.)
- **Tier 2:** 7 — **P66.7** (LLM-01 remainder only), **P66.9**, **P66.10**, **P66.11**, **P66.12**,
  **P66.16**, **P66.21**. (**P66.8** shipped and **P66.24** was filed and fixed, both 2026-08-16.)
- **Tier 3:** 3 — **P66.13**, **P66.14**, **P66.15**.
- **Tier 4:** 14 — **P66.17**, **P66.18**, **P66.19**, **P66.20**, **P66.23**, plus the nine pre-existing:
  **P65.4**, **P65.5**, **P64.4**, **P64.5**, **P61.7** (remainder), **P60.3**, **P52.14**,
  **P25.9**, **P63.10**.
- **Verification:** 6 — **P66.22**, **P38.1**, **P62.9**, **P65.2** (prompt half), **P65.3**
  (local half), **P62.8**.

**What to do next.** **The day plan is finished** — all four blocks are done (P66.2; the two
Criticals P66.1 and P66.4; P66.3; then P66.6, P66.7's LLM-16 half and P66.8). What the plan never
covered is now the whole of Tier 1: **P66.5**, inverting the config freeze list, which the plan
deliberately deferred (see [Explicitly not tomorrow](#explicitly-not-tomorrow)) and which is
unblocked — P66.1 has had a day to settle and P66.5 touches the same file.

After P66.5, Tier 1 is empty and the batch has no forced order left. Two things are worth naming
anyway. **P66.7's LLM-01 remainder** is the natural follow-on to what just shipped: the startup
notice now makes the uncapped context-file injection *visible* on any run that hits it, so the cap
can be designed against an observed number rather than a measured-once one. It also still gates
**P66.22**, the live-tier run: the warning half changed nothing about prompt content, so the cap and
P66.14 are both still ahead of the three numbers P66.22 measures.

The P38.1 guidance below is unchanged and still correct, but it is not the highest-value next
action: it was written when every tier was empty, and P66.5 is the last of the six findings that
were exploitable the day the review landed.

**What to do next (pre-P66, retained).** P38.1's live conformance re-run is the single
highest-value next action *among verification items*: it is
the only open item anywhere whose outcome produces new information rather than new code, and three of
the other verification items (P38.1 itself, P62.9's remaining live-tier evidence, P65.2's prompt
half) all close on the *same* harness — a local model driving `aegis chat --skill
threat-modeling`. One live-tier setup answers all three:

- **P38.1** — the conformance verdict itself (see its entry for the closure condition and reproduce
  steps).
- **P62.9** — needs n≥10 per arm on the deferred-`edit_file` surface, plus a default-prose control
  that has not yet been run (first n=3 evidence is in its entry).
- **P65.2**'s prompt half — does a local model fill the compaction-summary skeleton without losing
  what the old terse-bullet prompt kept? The summarizer fires mid-drive, so P38.1's run is this
  question's fixture too.

No Tier 4 build item currently has a fired trigger (re-verified 2026-08-15: `sandbox.backend` still
defaults to `"local"`, `lsp.Manager` is still one shared daemon singleton, both TUI asymmetries in
P63.10 are still present as described) — see each entry's **Promote when** for what would change that.

**Method notes worth re-reading before filing or building anything new** (full detail in
releases.md's pass history): before measuring an optimization, check the instrument the rest of the
system is running on — this document has twice recorded a fixed instrument *inverting* an
already-acted-on verdict. Every documented live-tier command needs `-count=1`, or a re-run silently
replays Go's cached verdict instead of reproducing. Mutation-test any new numeric threshold — a short
fixture cannot tell adjacent thresholds apart, and a count assertion cannot tell *when* something
fired. And **read the refutation records in releases.md before filing anything** against
`internal/provider`, `internal/ollamainfo`, `internal/repomap`, or scanner method resolution — several
obvious-looking gaps there have already been checked and answered.

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

## Execution plan for the P66 batch

**Written 2026-08-15 to be executed 2026-08-16.** This is the day plan for Tier 1 plus the two
cheapest Tier 2 items. It is ordered by dependency and by blast radius, not by severity — P66.2 is
not the most serious item but it must be the first commit, because it changes the toolchain every
subsequent test run uses.

**Working rules for the day.** One item per commit, each with its test. Run `go test ./...`
(36s, all 68 packages green today) before every commit, so any breakage is attributable to the item
in hand. The suite passing is *not* sufficient evidence for this batch — four of these items fix
defects that live in a fully green tree, and each names the test that must newly exist.

#### Block 1 — Toolchain, ~30 min — **DONE 2026-08-15**

**P66.2.** Shipped. `go.mod` pins `toolchain go1.26.6`; `govulncheck ./...` prints
`No vulnerabilities found` (was seven stdlib CVEs, six reachable) and `go test ./...` is green.
`.github/workflows/ci.yml` runs both tools on the ubuntu leg, installing each with
`GOTOOLCHAIN="$(go env GOVERSION)"` — the trap comment beside the install step is the P66.12 note,
taking the pin from the resolved toolchain rather than hardcoding a version that would drift from
`go.mod`. Verified against go1.26.6, not just the go1.26.5 in the review: the pinned install yields a
staticcheck that analyzes the tree (28 findings) instead of 21 compile errors.

`staticcheck` is `continue-on-error: true` for now, because those 28 findings are **P66.12's** work
and this item is not licensed to fix them. Deleting that line is part of P66.12.

#### Block 2 — The two Criticals, ~3 hours — **DONE 2026-08-16**

**P66.1.** Shipped as `92f72be`. All four parts landed: trust is resolved before any
project-controlled file is read (so `.aegis/.env` is skipped entirely for an untrusted directory),
`AEGIS_*` keys are dropped and logged from `.env` even when trusted, the baseline layer is built over
an `environSnapshot()` taken before `loadDotEnv`, and `applyWorkspaceTrust` no longer forges
`Trusted = true` from a missing `config.yaml`. SEC-09 folded in: `unsandboxedAutoExecError` now covers
`ModeAuto` as well as `AutoApproveExec` under the same `allow_unsandboxed_auto_exec` opt-out.

Both named tests exist in `internal/config/dotenv_trust_test.go`, plus the non-`AEGIS_` loader-variable
half and a blast-radius guard that a genuine operator-set `AEGIS_*` still applies and does not read as
a project change. Every one was confirmed failing against the unfixed tree before the fix landed.
`TestWorkspaceTrustNoProjectConfigIsTrusted` asserted the behaviour this item reverses and was
rewritten as `TestWorkspaceTrustNoProjectConfigFreezesNothing`. No loader-variable denylist, per the
arbitration.

**P66.4.** Shipped as `46dde08`. `tools` moved into a `toolTable` carrying its own mutex (lock order:
`Registry.mu` before `toolTable.mu`); a clone's own `Register`/`Upsert` now writes a clone-local
overlay shadowing the shared table, which is the fix for the deterministic cross-session leak.
`subAgentToolRegistry` hands each spawn a clone of its parent session's registry — `SpawnConfig`
already carried `ParentSessionID`, so no new plumbing — and `debate.go:102` had the identical
one-line defect and was fixed with it. Lazy clone at `sessionToolRegistry` (ARCH-11). ARCH-08's
residual closed as a side effect exactly as predicted.

`TestConcurrentSkillActivationAcrossSessions` reproduced the reported race verbatim under `-race`
before the fix (two clones' `Upsert` on one map, from `activateSessionSkill`), and also fails on the
deterministic leak without `-race`. `TestCloneUpsertStaysLocal` pins the overlay contract in both
directions including clone-of-clone; `TestSubAgentToolSearchDoesNotWidenTheDaemon` guards ARCH-02 on
identity as well as effect.

*Found in passing, and fixed (**P66.24**, same day).* `internal/mcp`'s `TestSamplingHandler` and
`TestToolsChangedNotification` each started `go io.Copy(io.Discard, serverReader)` **and** a
`json.Decoder` on that same pipe. The two readers competed, and when the drain goroutine won the
initialize request the fake server never replied — `c.initialize(context.Background())` has no
deadline, so the package hung until the 10-minute test timeout killed the whole suite. Hit once
during this block and not reproducible in six isolated re-runs, which is the profile of a flake that
fires on a loaded CI box and gets dismissed as infrastructure.

The drain now starts *after* the initialize read (one reader on the pipe at a time), and an `initCtx`
helper bounds every handshake at 10s so a future regression is a named failure in seconds rather than
a suite-wide timeout. Verified by stress rather than by re-running: `-race -count=120` over the two
tests hangs to the 241s timeout on the old code, with `io.Copy` at `mcp_test.go:238` in the panic
stack, and finishes in 2.6s on the fixed code.

#### Block 3 — The read-only tier, ~3 hours — **DONE 2026-08-16**

**P66.3.** Shipped. Everything the two read-only argv paths must agree on now lives in
`internal/tool/builtin/argv_confine.go`: one union git-flag denylist (`deniedGitFlags`, including
`--no-index`), one attached-value-aware flag matcher, one path-candidate extractor, and
`validateReadOnlyGitArgv`, which both `gitTool.Execute` and `readOnlyGitCommand` now call on the
same argv. `git.go`'s `deniedGitArgPrefixes`/`validateGitArgs` and `shell_readonly.go`'s
`gitConfigOverrideFlags`/`shellArgsStayInRoot` are gone. The budget note's three spellings all
landed; the item did not overrun.

*Three deviations from the plan worth knowing.*

**`-p` came off the denylist rather than onto it.** The union of the two lists would have denied it,
but `-p` is the pager alias — an external program — only in the *pre-subcommand* position, and
neither call path can reach that position: the git tool takes the subcommand as its own field and
prepends it, and the shell classifier requires the first token after `git` to be an allowlisted
subcommand. Post-subcommand `-p` is `--patch` and is read-only, so denying it (as the shell path did)
cost `git log -p` for nothing. `--paginate` stays denied — it has no post-subcommand meaning to lose.

**Three more argv0 drops than the plan named, and they close VULN-02 at its root.** Beyond `ps`,
`less` and `more` (SEC-04), `sort`, `tree` and `uniq` came off `readOnlyShellArgv0` as well: each has
a documented file-*writing* form (`sort -o FILE`, `tree -o FILE`, `uniq INPUT OUTPUT`), so no
argument parsing makes them read-only. Confinement stops those forms escaping the workspace, but a
write *inside* the workspace is still a write and plan mode allows `CapRead` silently. The review's
own VULN-02 fix section reached the same conclusion for `sort`; `tree` and `uniq` are the same
criterion applied consistently, which is the whole argument of this item. A regression case pins that
this did not cost `grep -o` (`--only-matching`), the one allowlisted `-o` that is a read.

**The separated `-o <path>` spelling needed no case of its own** — its value is a bare operand, and
operand confinement was already being added. The helper handles `--flag=value` and `-ovalue`; the
third spelling falls out.

*Verified against the unfixed tree, not just green afterwards.* A worktree at `184497d` accepted all
eight escapes: the six shell classifications (`git diff --output=`, `sort --output=`, `sort -o`
attached, `ps auxwwe`, `less`, `more`) all returned `CapRead`, and the git tool ran both
`--no-index` and the escaping pathspec without a refusal. VULN-01 reproduced verbatim on Windows —
`git diff --no-index -- NUL <abs path>` through the `CapRead` git tool returned the full contents of
a file outside the workspace.

The deliverable is `TestReadOnlyGitArgvAgreesAcrossBothPaths` (`argv_confine_test.go`), a table of 19
argvs asserting the two paths reach the same verdict, with the shell string *derived* from the argv
so equivalence is guaranteed by construction rather than by proofreading.
`TestReadOnlyTierRefusesEscapesInPlanMode` states the property in plan-mode terms and records the one
real asymmetry between the paths: the shell tool refuses by declining the `CapRead` downgrade, while
the git tool is statically `CapRead` and is always reached, so it must refuse inside `Execute`.

Closed VULN-01 (+SEC-05), VULN-02, VULN-11, SEC-04, SEC-10.

#### Block 4 — Cheap, high-value, low-risk, ~2 hours — **DONE 2026-08-16**

**P66.6.** Shipped as `f72e116`. Sanitized at ingestion (`stream.go`'s `KindApprovalRequest`), with
`StripControlSeqs` rather than `StripDangerousSeqs` — the dialog applies its own lipgloss styling
*after* ingestion, verified by reading the render path, so model-supplied SGR can only fight the
TUI's own colours.

*Two things the item's own description would have missed.* The suggested **"allow always" rule
pattern** carried the escape too, so even the one covered path (`shell`, patched under P28.1) leaked —
via `suggestRulePattern`, which `renderShellCall`'s stripping never saw. And a single strip over
`string(ev.ToolInput)` is **not sufficient**: a real provider delivers the payload as the
six-character JSON escape for ESC, which is plain ASCII on the wire and only becomes a control byte
when `renderWriteDiff` unmarshals `content`. Raw ESC bytes are the *other* shape, and they make the
JSON unparseable — which drops the preview into `renderApprovalPreview`'s generic excerpt branch that
prints the bytes verbatim. `sanitizeToolInputJSON` does both passes.

Checked before shipping: `approvalState.input` is render-only (the approval response carries just the
id), so sanitizing cannot alter the call that actually runs.

The closure condition needed one honest amendment. A literal `ContainsRune(out, 0x1b) == false` can
never pass, because the dialog's own chrome *is* ESC bytes — lipgloss emits truecolor SGR for the
frame and option list even under `NO_COLOR`. `TestApprovalDialogStripsControlSequencesFromToolInput`
removes SGR only (`\x1b\[[0-9;]*m`, the sole form the TUI emits) and asserts no ESC survives that, so
anything left is by construction an escape the event smuggled in. Eight carriers across both render
paths, all eight confirmed failing against the unfixed tree.

**P66.7, LLM-16 half.** Shipped as `5ed832d`. One `KindNotice` at run construction when
`tokenest(system) + requestOverhead` crosses `oversizedSystemPercent` of the served window, naming
both numbers and taking its remedy clause verbatim from `ollamainfo.Result.Describe()` so `/status`
and this read as one voice. Silent when the window is unknown — that is "not known yet", not "tiny".

**The threshold is 50%, not the review's ~60%,** and the disagreement was resolved rather than split:
`compactionTrigger` is floored at `window/2`, so `window/2` is the lowest estimate at which proactive
compaction can fire at all. A fixed prompt at that point puts every turn over the trigger from its
first message — the state actually worth naming — and 60% leaves a band of runs sitting in it
unwarned. `TestOversizedSystemPromptThresholdMutation` hardcodes 49%/51% so it discriminates: the
constant at 60 fails the "just above" case and at 40 fails the "just below" case (both run).

Nothing about prompt *content* changed. The `localContextFilesMaxBytes` cap and the realistic-`CLAUDE.md`
budget fixture (LLM-01) stay open under P66.7.

**P66.8.** Shipped as `35e8f95`, and it was two defects, not one. The timeouts were the reported half;
the beat could not have arrived anyway, because `withStallBeat` was a bare `context.WithValue` and a
sub-agent's engine installed its watch over the same key.

`internal/heartbeat` (new) carries the beat chain. It is a **leaf package** because the three parties
sit on opposite sides of the import graph — `internal/tool` already imports `internal/provider`, so no
home inside any of the three is reachable from the other two. `agent.go` now bounds each *individual*
wait at `maxAgentDuration` and beats on every completion (per teammate, per debate role); the
aggregate batch/debate contexts stay as the outer cap and are admissible **precisely because** they
decompose into sub-900s waits with observable activity between them. The per-wait bound is what fixes
sequential and loop mode, where one teammate could previously spend the whole batch budget on a single
silent wait. `admissionAdapter` beats every 30s while queued — the one wait in the codebase *known* to
be alive while producing nothing, which is what licenses a blind ticker there and nowhere else.

The docs were corrected rather than deleted: the true relation is "above every **per-call** bound",
and an aggregate above 900s is admissible only if it decomposes. That sentence now appears in
`config.go`, `docs/configuration.md` and CLAUDE.md, which also closes P66.21's first bullet.

`TestToolTimeoutsStayUnderTheStallBound` mirrors `TestResultCapsCanBindBeforeTheContextWindow`, and
its **grep-the-source half** counts the `context.WithTimeout` sites in the package and requires the
tables to name all 13 — so a new timeout cannot be added without a decision, which is exactly how the
two agent bounds drifted 40 and 80 minutes above a limit the docs claimed they were under. Mutation
checks run, not asserted: the pre-P66.8 per-teammate wait fails at 40m0s; `stallBound` at 5 minutes
fails all six 10-minute entries and neither latex entry.
`TestChildStallWatchDoesNotHideItsParent` pins the chain in both directions and reproduces the
shadowing verbatim against a reverted `withStallBeat`.

#### If the day is short

**P66.2, P66.1, P66.4** — in that order. Those are the one-line CVE fix and the two Criticals, and
they are mutually independent once P66.2 lands. Everything else can wait a day without the risk
profile changing.

#### Explicitly not tomorrow

**P66.5** (invert the freeze list) is Tier 1 and M-sized, and it touches the same file as P66.1. Land
P66.1 first and let it settle; inverting the freeze list is a design change that wants a clear head
and its own test pass, not the tail end of a long day.

**P66.13** and **P66.14** are Tier 3 for a reason — P66.13 needs `newChatCmd` split before either bug
is fixable, and that refactor should not be started in the same session as five security fixes.

**P66.22** (the live-tier run) must wait for P66.7 and P66.14, which change three of the five numbers
it measures. *Still true after Block 4:* P66.7's LLM-16 half added a notice and changed no prompt
content, so the cap half is still ahead of it.

**All four blocks are now done.** This plan is retained as the record of what was built and what was
found while building it — several of the notes above correct the item they were written from — not as
outstanding work.

---

## Open Work — Tier 1

**Status: 1 open**, from the P66 review batch (P66.2 shipped 2026-08-15; **P66.1 and P66.4, the two
Criticals, P66.3, the read-only tier, and P66.6, the approval dialog, shipped 2026-08-16** — see the
Block 2, 3 and 4 notes in the execution plan above, and [releases.md](releases.md) for the
rationale). It is exploitable today with no dependency on anything else in this document. Evidence is
in [CodeReview.md](../CodeReview.md) at the finding IDs named in its heading.

### P66.5 — Invert the config freeze list

`securityRelevantDiff` (`internal/config/config.go:1842-1927`) is an **enumerated denylist** of
security-relevant keys frozen from untrusted project config, and the enumeration is incomplete in
every direction the review looked:

- **SEC-02** — `commands:` is unfrozen, and `internal/toolpath` execs a relative override. `grep` is
  `CapRead` and therefore silently allowed in *plan* mode, so an untrusted repo gets arbitrary binary
  execution through a read-capability tool.
- **SEC-03** — `security.*` is unfrozen, so an untrusted repo turns off `egress_then_write` and the
  network allowlist.
- **SEC-06** — `server.addr`, `server.allow_remote` and `data_dir` are unfrozen.

This is the same defect three times, and the project has already found it incomplete three times on
its own (P42.1, P46.2, P52.13). That is six independent discoveries of one structural problem, which
is the argument for inverting rather than extending: enumerate the **project-settable** keys, freeze
everything else, and add the grep-the-source invariant test this repo already owns the pattern for
(`TestEveryRegisterCallSiteDecidesTheLocalProfile`) so the build fails when a new `Config` field
appears in neither list. Follow the `Security.DAST.AllowedTargets` precedent (`:1809`) for `data_dir`
and `security.*` — baseline-only, never project-settable even after `aegis trust`. Reject
relative-path `commands:` overrides from any project-sourced layer.

Fold in SEC-07 if cheap: trust grants are permanent and content-blind, so a `git pull` that adds a
`hooks:` block re-prompts nothing. Re-prompting on a change to the security-relevant subset falls out
of having a well-defined subset, which is what this item builds.

Closes SEC-02, SEC-03, SEC-06, SEC-07. Priority: Tier 1 — M. Sequence after P66.1 (same file).

---

## Open Work — Tier 2

**Status: 7 open**, all from the P66 review batch (**P66.8** shipped 2026-08-16; **P66.7** is reduced
to its LLM-01 half). Each is self-contained and independently shippable; none blocks or is blocked by
another.

### P66.7 — Context files are injected uncapped, and the budget test cannot see them (LLM-01 remainder)

The local prompt budget is one of the most carefully disciplined things in the codebase — four
shrinking mechanisms, a 4,550-token ceiling enforced by `TestEffectiveSystem_localProfileBudget`, a
measured 37% cut in P62.6, and a repo map capped at 4,000 bytes. `memory.readIfExists`
(`internal/memory/memory.go:232`) injects `CLAUDE.md`/`AGENTS.md` with **no cap at all**: measured
**11,611 tokens on this repository**, 2.6x the entire enforced ceiling and 2.8x Ollama's default
4,096-token window. The most carefully budgeted prompt in the project is blown by the file that
documents the budget.

The ceiling test is structurally blind to it because it runs over a bare fixture where every
project-varying component is empty.

**LLM-16 shipped 2026-08-16** as `5ed832d` (see the Block 4 note above) — a run-start notice now
fires when the uncompactable part of the request crosses 50% of the served window, so this item's
condition is *visible* on any run that hits it rather than inferred from a one-off measurement. That
was deliberately the cheap half: it needed no policy decision about what to truncate.

**What remains is the cap and the test.** Apply a `localContextFilesMaxBytes` symmetric with
`localRepoMapMaxBytes` (`internal/server/helpers.go:37`), which sits three lines away and caps a
*smaller* block. Extend the budget test to run over a fixture carrying a realistic `CLAUDE.md`, or it
keeps measuring only the components that never grow. Design the cap against a number the new notice
actually reports, not against the 11,611-token figure alone — that one is scoped to *this*
repository.

Closes LLM-01 (LLM-16 closed). Priority: Tier 2 — S. Highest-value item in this tier for the local path.

### P66.9 — Detached-run event writes are per-event, and `bg_events` is never bounded

`internal/server/messages.go:251-260` wraps `send` for detached runs so every stream event —
including every text delta — is JSON-marshalled and INSERTed as its own fsync-bound SQLite
transaction, inline on the engine's stream-consumption goroutine, over a `SetMaxOpenConns(1)`
connection. `tui.go:938` sets `Resumable = true` unconditionally, so this is the default interactive
path.

**The debate correctly cut the latency half of this finding.** `origSend` is a non-blocking channel
enqueue that runs *before* the DB write, so no display latency is added, and 0.5 ms/event against
local generation at ~33 ms/token is ~1.5% overhead — not the dominant per-token cost originally
claimed. What survives, and is the real item, is **unbounded growth**: `bg_events` is pruned only by
whole-session delete, and the auto-pruner is gated on `Cleanup.SessionTTLDays`, which **has no
default**. On a default install the table grows without limit for the life of the install, storing
rows that duplicate text `session_messages` already holds whole.

Fix: a per-session `bg_events` retention bound in `internal/session/session.go` that does **not**
depend on `Cleanup.SessionTTLDays` being configured. Then coalesce text deltas (one row per ~200ms
or on a change of event kind, non-delta events written through immediately). Only afterwards consider
`synchronous=NORMAL` — unconditionally for `knowledge.db` and `longmem.db`, and for `sessions.db`
only with the durability trade written down, since it holds checkpoints, the cost ledger and traces
(PERF-02, which the debate downgraded to Low for exactly that reason).

Closes PERF-01, PERF-02. Priority: Tier 2 — S.

### P66.10 — The bounded security remainder

Three independent small fixes, grouped only because each is under an hour and none deserves its own
heading:

- **ARCH-03** — the output guard reads files back via `reader.Execute(ctx, …)`
  (`internal/engine/engine.go:2354`) with a context lacking `WithWorkdir`/`WithExtraRoots`, which are
  attached only in `executeTool`. On any session with a custom workdir the guard silently validates
  nothing. Fix with an `e.toolCtx(ctx)` helper used by both `executeTool` and `collectWrittenFiles`,
  shipped with a custom-workdir guard test.
- **VULN-03** — the SSRF blocklist misses `0.0.0.0/8`, IPv6 `::` and `100.64.0.0/10`, verified against
  the real resolver, and is duplicated in `internal/tool/builtin/web.go` and `internal/mcp/http.go`.
  Add `IsUnspecified()` plus the missing ranges, and keep **one** copy.
- **VULN-05** — `LocalBackend.Exec` buffers subprocess output unbounded, so the 24 KiB shell cap
  applies only *after* a 10-minute `cat /dev/urandom` is already in the daemon heap, killing every
  concurrent session. Use a capped writer for `CombinedOutput`.

Closes ARCH-03, VULN-03, VULN-05. Priority: Tier 2 — S-M total.

### P66.11 — Nothing redacts, and the turn trace is too thin to debug a bad run

Two halves of one gap: what leaves the process, and what is kept about a run.

`internal/share` performs **no redaction at all** (SEC-08) — a shared session carries whatever the
transcript holds, including anything a `.env` or `ps` call put there. Add a redaction pass reusing
`internal/mcp/outbound.go`'s existing credential patterns, emit a redaction count so a silent miss is
visible, and apply it to the audit trail too (SEC-11's redact-don't-truncate half).

`TurnTrace` carries no stop reason, no compaction event, no guard verdict, no retry record and no run
id (GAP-01) — **all of which are already computed and discarded one line after they are produced**.
For a project whose entire method is measurement-driven, that is the gap most at odds with how the
project works: every live-tier item in this document would be easier to close with it. Widen the
struct; **skip the OTel/Prometheus half**, which is a dependency decision this item does not need to
make.

Closes SEC-08, SEC-11, GAP-01. Priority: Tier 2 — M.

### P66.16 — OpenAI adapter drops tool calls and never synthesizes an ID

Two correctness defects on the adapter that the documented local-Ollama configuration actually uses
(`docs/providers.md` recommends `provider.default: openai` against `:11434/v1`):

- **LLM-04** — `openai.Finish` iterates `for i := 0; i < len(tools); i++` over a map keyed by **wire
  index**. A 1-based or gapped index silently drops tool calls *after* `EventToolUseStart` has already
  fired, so the engine sees a started call that never completes.
- **LLM-05** — the adapter never synthesizes a tool-call ID, so a backend that omits one breaks
  `tool_result` correlation.

Both are small, both are on the path most local users are on, and both are the kind of defect that
presents as "the model is behaving strangely."

Closes LLM-04, LLM-05. Priority: Tier 2 — S.

### P66.12 — staticcheck cleanup

`staticcheck` (2026.1) now runs clean across the tree and reports **28 issues in 173k lines** — no new
correctness or security defect, which is a good result worth recording. What is left is housekeeping:
17 U1000 unused symbols (including `doctorToolCallSmokePrompt` at `internal/cli/doctor.go:616`, a
leftover from before the probe moved to `internal/toolcallprobe` and an invitation to edit the wrong
copy); three vestigial `SA4005` fields on `fakeImageScanner` (`internal/security/security_test.go:315`)
that are positioned and commented exactly like a working P55.7 assertion but are dead — the real
assertion runs through `recordingImageScanner`'s pointer; one genuine `SA4006` test gap at
`internal/compaction/compaction_test.go:95`, where the under-budget path never inspects `out`; and
style hits.

Two are **false positives** worth rewording anyway: the deliberate side-effecting
`d.record("a") || d.record("a")` at `internal/engine/loopdetect_test.go:286`, and prose beginning
`go:embed` in a doc comment at `internal/security/multiscanner_test.go:781` — in the one file whose
subject is embed patterns silently omitting files.

`staticcheck` is **already in CI** as of P66.2, with the toolchain pin and its rationale documented
beside the install step (`honnef.co/go/tools` carries `toolchain go1.25.13` in its own `go.mod`, so
`GOTOOLCHAIN=auto go install` produces a binary that cannot analyze this go1.26 module and reports 21
compile errors instead of analysis). It runs `continue-on-error: true` precisely because these 28
findings are still open. **Deleting that line is this item's closure condition** — clearing the
backlog without making the step gate leaves the next 28 to accumulate the same way.

Closes QUAL-15. Priority: Tier 2 — S.

### P66.21 — Documentation corrections the review proved wrong

Four documented claims that the code contradicts. Grouped because doc work should not be counted as
remediation effort, and because a wrong doc in this repo is load-bearing — CLAUDE.md is the primary
knowledge store (QUAL-14) and these sentences are why a maintainer would *not* look.

- ~~CLAUDE.md: the 900s stall bound "sits deliberately above every narrower timeout it backstops" —
  false, see P66.8.~~ **Done 2026-08-16** with P66.8. The claim lived in `internal/config/config.go`'s
  `DefaultMaxTurnStallSec` comment and `docs/configuration.md` as well as CLAUDE.md; all three now
  state the true relation ("above every *per-call* bound") and the condition under which a larger
  aggregate is admissible.
- CLAUDE.md: write/execute tools serialize via `sync.RWMutex` — it is a plain `sync.Mutex`, and the
  guarantee is narrower than the doc implies (ARCH-13).
- `buildChatSystem`'s doc comment claims equivalence with the daemon's `effectiveSystem` — false, see
  P66.13.
- `internal/tui/view.go:305-312` still asserts the pre-P35.13 claim that `prompt_eval_count` is a
  cache-hit delta. P35.13 corrected the code; this comment survived and is now wrong in the *opposite*
  direction, telling a maintainer the context meter "understates how full the context window is" when
  on native Ollama it is accurate — and proposing remediation that should not be done (LLM-09).

Closes ARCH-13, LLM-09, and the doc half of P66.13 (P66.8's doc half is closed). Priority: Tier 2 — S.

---

## Open Work — Tier 3

**Status: 3 filed items**, all from the P66 review batch — each larger or sequence-dependent rather
than urgent. P62.9, P65.2's prompt half and P65.3's local half all moved to
[Verification Work](#verification-work) — in each case the code is already shipped and what remains
is a live-run result, not a design or implementation task.

### P66.13 — `aegis chat` re-derives what the daemon centralizes, and every copy has drifted

The dominant defect shape in this codebase — *a mechanism built for one path that a second path
silently bypasses* — with the CLI as the second path. Four instances of one root cause:

- **QUAL-01** — `internal/cli/chat.go:274` builds a **bare** `permission.New(...)`. The daemon's
  `buildGate` (`internal/server/engine_build.go:162-224`) stacks five layers. `permission.rules` deny
  rules and `security.egress_then_write` are therefore silently inert under `aegis chat`, and
  `internal/cli/dryrun.go` has no gate at all. `cli/worker.go:174`'s own comment names this exact
  bypass as the one P10.1 closed — `worker.go` was fixed and `chat.go` was not.
- **QUAL-02** — `buildChatSystem` (`chat.go:871`) claims in its doc comment to be "equivalent to the
  daemon's `effectiveSystem`". It omits `<deferred_tools>` **entirely**, so the 26 deferred tools the
  whole P62.6 line is about are undiscoverable via `tool_search` on the CLI path — a pure capability
  loss with the token saving already banked. It also skips the P25.6 local repo-map cap, on the path
  that *is* the local-model path.
- **ARCH-06** — `aegis chat` ignores `max_iterations`, `loop_threshold`, `redact_secrets`, the output
  guard and hooks.
- **QUAL-06** — `builtin.Options` is a 27-field struct filled differently at all five call sites; the
  CLI omits `Commands`, so the entire `toolpath`/ripgrep contract is inert there.

Fix by extraction, not by patching each: pull `buildGate` into a constructor both the daemon and
`chat.go` call, and do the same for the cost limits and `builtin.Options`. Emit `deferredToolsBlock`
from `buildChatSystem` or stop deferring on that path. `newChatCmd` is a 683-line function wrapping a
615-line `RunE` closure holding both bugs (QUAL-03) — splitting it far enough to make both testable is
the **enabling refactor**, not a finding in its own right, which is why this is Tier 3 and M-L rather
than a quick fix.

**Ship the invariant test, not just the fix:** every production site that builds an engine either
stacks the full gate or states in a comment why it does not — the same grep-the-source shape as
`TestEveryRegisterCallSiteDecidesTheLocalProfile`, which is the instrument this class of defect
actually responds to.

Closes QUAL-01, QUAL-02, QUAL-03, QUAL-06, ARCH-05, ARCH-06. Priority: Tier 3 — M-L, sequence-dependent.

### P66.14 — Two compaction thresholds that disagree, and a calibrator that never fires

Four findings in the token-accounting path, grouped because they share one seam and fixing them
separately would mean touching it three times:

- **LLM-02** — P59.1's completion-sized compaction trigger is discarded one layer down.
  `engine.compactionTrigger` (`engine.go:495`) reserves room for the completion;
  `compaction.Summarizer.shouldCompact` (`compaction.go:243`) uses a flat 20%-free rule and never sees
  `maxTokens`. At a 4,096 window the engine triggers at 2,048 and the summarizer refuses until 3,277 —
  so summarization finally happens with 819 tokens left for a completion the request asked 32,768 for.
  `SetEstimateCorrection` exists precisely because these two gates must not disagree; the argument was
  never applied to the trigger itself. Fix with one shared trigger function taking `(window,
  maxTokens)` used by both.
- **LLM-03** — the P62.4 calibration is inert on the OpenAI-compat path. `afterTurn`
  (`engine/compact.go:446`) gates on `PromptEvalDurationMS > 0`, which only the native Ollama adapter
  sets — while `docs/providers.md` recommends `provider.default: openai` + `:11434/v1` for Ollama.
  **Every user following the documented configuration runs the whole session on the uncorrected
  20-33% undercount.** Gate on a positive backend identification instead; `sharedContextWindow` is
  already one.
- **ARCH-07** — `SetEstimateCorrection` pushes a per-run overhead into a Summarizer built once per
  *server* and shared by every session, which that type's own doc comment argues per-session data
  cannot live on.
- **PERF-03** — `compactionGuard.requestOverhead` is snapshotted once in the constructor
  (`compact.go:260`), but `tool_search`'s `reg.Load` mutates the exposed set mid-run, silently
  undercounting the compaction trigger by up to 593 tokens for a single tool against a 4,550 budget.

Closes LLM-02, LLM-03, ARCH-07, PERF-03. Priority: Tier 3 — M.

### P66.15 — Sweep the two packages this review did not read

`internal/tui` (16,163 non-test lines) and `internal/security` (8,435) are **26% of production Go**
and produced three findings between them, two of which were a struct-field count and a stale comment.
That is not evidence they are clean; it is evidence nobody read them. The one hour eventually spent
in `internal/tui/approval.go` during arbitration produced P66.6 — a Medium security finding at the
last line of the threat model.

Two specific sweeps, each with a stated reason to expect findings:

- **`internal/security`'s scanner-output parsers.** 8.4k lines that shell out to fifteen external
  tools and parse their SARIF/JSON/XML output back into model context. The review noticed the
  prompt-injection shape *for DAST* (SEC-06's 0777 work directory) and did not generalize it; nothing
  swept the parsers. `security.parseNmapXML` is already implicated in P66.2's CVE list.
- **`internal/tui`'s rendering and approval paths.** P66.6 fixes the ingestion point; this is the
  audit that establishes whether it is the only one.

Also unexamined as categories, and worth folding in: DoS against the daemon (session/checkpoint
growth, the 60s auth-lockout cap, unbounded `sessionSems`/`sessionPermCache` growth reachable by a
loopback caller creating sessions in a loop); the `internal/checkpoint` **restore** path, where
`/rewind` writes files back into the workspace from a BLOB and nobody asked whether it path-validates;
and the `internal/swarm` mailbox as a cross-agent injection channel, since `trust.Wrap` covers MCP and
web but nobody checked the mailbox.

Priority: Tier 3 — M. Not a defect; a known gap in coverage with a demonstrated hit rate.

**Four leads sit here unfiled, each with a stated promotion trigger.** None is a `### P<n>.<m>` item
yet, deliberately — filing one before its trigger fires would commit to a design question that has no
answer.

- **Whether Aegis should ever mount a container engine socket.** `dockle` is the only tool that wants
  one — it inspects an image through the local engine rather than pulling it — and socket access is
  effectively host root, a third privilege axis beyond the network/workspace split P55.7 is built on.
  dockle stays host-only and says so in code. **Promote when** someone actually needs container-only
  dockle.
- **Auto-engage the tool-calling shim off a low conformance rate.** The persisted P53.4 rate is already
  readable per model (`modelcaps.Store.ToolCalling`) and `provider.tool_call_shim` rejects `"auto"`
  rather than silently accepting it as a no-op, precisely so the word stays available for this.
  **Promote when** live runs show the rate predicting drive outcomes.
- **Grammar-constrained decoding for *tool calls*** (Ollama structured outputs, llama.cpp GBNF). P59.8
  took this lead at the one caller with no open design question (the schema guard's corrective retry)
  and deliberately did not widen it. Targets models that speak the tool-call protocol but truncate or
  malform arguments (the P35.2 failure class). Needs its own heading if pursued.
- **Code Mode — the model writes a program against a generated SDK instead of emitting tool calls.**
  The token argument is real and attacks the same 84.3%-of-base-prompt cost the P62.x line has been
  chipping at, but our own evidence points the other way for the primary target: P39.16 shipped
  handle-based editors because a 14B model failed `edit_file`'s byte-exact match 12 times running, and
  writing a correct *program* with control flow over those same tools is a harder generation task, not
  an easier one. **Promote when** a measured local run shows a model composing multi-step tool work
  reliably enough that round-trips are the binding cost, or a cloud/mechanical-fan-out target emerges.

**Four more leads, same reasoning — a real mechanism with no observed problem behind it here.**

- **Per-file mutation serialization keyed on `realpath`.** `engine.runTools` serializes *all*
  write/execute tools behind one `execLock`. A per-path queue would let unrelated-file edits run
  concurrently. **Promote when** a measured drive shows serialized writes as a real fraction of wall
  clock — the current coarse lock is correct, and trading it for a fine one on no measurement is how
  concurrency bugs are bought.
- **A wider hook surface.** `internal/hooks` exposes only `PreToolUse`/`PostToolUse`. Four gaps worth
  naming for vocabulary alone: `context` (mutate the message list before every provider call),
  `before_provider_request`/`after_provider_response`, and an `agent_end` vs `agent_settled` split that
  names a distinction the drive's reset ladder already implements without a word for it. Do **not**
  adopt an in-process JS extension runtime for this — it would hand arbitrary code the capabilities
  `internal/permission`/`internal/sandbox` exist to gate. **Promote when** something concrete needs one
  of these four points.
- **Full-text search over session history.** Aegis is already SQLite-backed, so FTS5 over
  `session_messages` is close to free and would back a `/search` the TUI does not have. Co-located FTS
  triggers are the trap — an index failure can roll back canonical writes. **Promote when** someone
  asks for cross-session search.
- **Pinned distribution for skills and personas** (`aegis skills install git:host/user/repo@ref`),
  gated on the existing `internal/workspacetrust` grant. **Promote when** there is a second party
  publishing Aegis skills — this is distribution, not capability, and worth nothing until someone is on
  the other end of it.

---

## Open Work — Tier 4

**Status: 13 open** — 9 pre-existing (all blocked or explicitly parked, none with a fired trigger)
plus 4 from the P66 review batch. The P66 entries here are **deliberately grouped grab-bags**: each
collects the Low-severity residue of one review domain. They are filed so no finding is lost, not
because any of them should be scheduled. Take one only when already working in that file.

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
record parses `<read-files>` tags out of *assistant* text (LLM-15). The SSE idle watchdog counts
consumer backpressure as a stalled runner (LLM-17). `reapSpills` scans the whole spill directory on
every spill (LLM-18).

**Promote when:** one of them is implicated in a live-run failure, or you are already in the file.
LLM-06 and LLM-10 are the two most likely to matter on a 16GB-VRAM machine, since both cause an
avoidable model reload.

Priority: Tier 4 — real, individually cheap, no trigger. Do not schedule.

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

**Promote when:** VULN-04's *general* form — schema validation for tool input — is worth its own item
if a read- or network-capability tool ever grows an enum that gates a path or a binary. The rest are
opportunistic.

Priority: Tier 4 — all Low, all confirmed, none with a fired trigger.

### P66.18 — Architecture, quality and maintainability residue

A mid-stream `EventError` discards the whole assistant turn **including text already streamed to the
user**, so the transcript loses content the user watched arrive (ARCH-09) — the most user-visible item
in this grab-bag and the one most likely to be reported as a bug. Session-scoped in-memory state leaks
on prune, and two maps leak on delete (ARCH-10).

`hardenDBPermissions` is triplicated verbatim across `internal/knowledge`, `internal/longmem` and
`internal/session` — a **file-permission boundary** copied three times, which is the one kind of
duplication worth de-duplicating on principle rather than on measurement (QUAL-04). `internal/tui` is
a god package with a 97-field god struct (QUAL-05). Ten ad-hoc `truncate` helpers sit alongside the one
canonical truncation policy in `truncate.go` (QUAL-07). `context.Background()` appears inside
request-scoped handlers (QUAL-08). `internal/drive` has no package doc and ~10.5% of exported symbols
are undocumented (QUAL-09).

**Promote when:** QUAL-04 should go with any change to DB file permissions; QUAL-05 with any
substantial TUI work (it would also make P66.15's sweep cheaper). The rest are opportunistic.

Priority: Tier 4 — no trigger. QUAL-04 is the only one with a security-adjacent argument.

### P66.19 — Capability gaps with no fired trigger

Assessed against what a mature coding agent needs, and honestly reported as absent rather than
planned: no log rotation and no size cap, with a *text* handler despite the "structured logging"
claim (GAP-02); LSP is seven read-only tools with no rename and no code action, and diagnostics have
exactly one caller so nothing feeds back after an edit (GAP-03); git support stops short of branching
and `internal/worktree` exposes no tool at all (GAP-04); no OS-level sandbox on Windows, conspicuous
because the rest of the Windows story is handled well (GAP-05); the MCP server side lags the mature
client (GAP-07); no test-runner feedback loop as a first-class concept (GAP-08); structured outputs
are wired but used at exactly one call site (GAP-09).

**GAP-03 and GAP-08 are the same missing idea** — nothing closes the loop after an edit — and are the
pair most worth taking together if this tier is ever opened. GAP-02 is the cheapest and the only one
with an operational failure mode (an unbounded log file).

**Promote when:** a user hits one. GAP-06 (resume across daemon death) is **not** here — it is the
pre-existing P65.4 and stays as filed.

Priority: Tier 4 — no triggers. Do not build speculatively.

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

**How to use this tier.** Every Tier-4 item that has actually been measured so far turned out to be
wrong in some way — an unmeasured dependency that was actually our own code, a gate unmeetable by the
work it proposed, a cap that wasn't the largest one in the tree. Take the measurement first, then
re-read the item; do not treat a Tier-4 write-up as a build plan. Details of past measurements are in
[releases.md](releases.md).

### P64.4 — Edit results carry no diff, and a tool cannot attach anything a replay can render

`edit_file`, `edit_section`, `multi_edit` and `fill_marker` return prose ("updated successfully", a
replacement count); the TUI and web transcript have nothing else to render for an edit. The presenter
runs on both live streaming and transcript replay, so it must be deterministic and cannot do I/O at
replay time. A **tool-private, persisted presentation channel** — `execute` attaches an opaque JSON
payload the core stores with the result and hands back to the presenter, computed once at result time
and read back on every replay — is the reusable mechanism; a diff (applied hunk ± context lines) would
be the first consumer. Cost: an overwrite would need to hold both prior and new text in memory to
compute a UI-only hunk.

**Promote when:** the TUI or web transcript is being worked on for another reason, or a user reports
not being able to tell what an edit actually changed. This is presentation with no correctness or
security edge.

Priority: Tier 4 — real, cheap-ish, no trigger. Do not build speculatively.

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

### P61.7 — Retry/terminal classification over *backend-echoed* text (remainder)

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

### P60.3 — Checkpoints capture files only, so `/rewind` is silent about everything else

`internal/checkpoint` snapshots each file a write tool touched (capped at 16MiB) and rewind writes
those contents back — correct within its documented scope. Rewinding a turn that ran `pip install`,
applied a DB migration, started a background process, or wrote a >16MiB artifact restores the source to
pre-turn state while leaving the environment in post-turn state, and the user is told the turn was
undone. If a session owns a persistent container (P60.2, shipped 2026-08-05), a checkpoint could be a
container snapshot/commit instead, making rewind honest about installed packages and process state.

**Re-verified 2026-08-06:** `sandbox.backend` still defaults to `"local"`, so this only helps sessions
using the container backend, which is not the default.

**Promote when:** the container backend is a realistic default for real sessions, or a user reports a
rewind that restored files into an environment that no longer matched them.

Priority: Tier 4 — no longer blocked, but speculative until someone is actually rewinding inside a
container.

### P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)

`newLoopDetector` is constructed inside `Run`, so its window resets every call. In the TUI and web UI
each user turn is a separate `Run`, so a model that loops *across* user turns (re-reading the same file
every time the user nudges it, re-running the same failing command after each correction) is never
detected. Fix: hoist the detector to session scope via an optional caller-owned detector in
`engine.Options`, so the daemon can hold one per session while the CLI keeps today's per-`Run`
behavior. Open design question: a user legitimately asking for the same call twice across two turns
isn't a loop, so a session-scoped detector likely needs a higher threshold or a reset rule keyed on
whether a user message is a bare retry — fuzzier than the current mechanism.

**Re-verified 2026-08-06:** still constructed inside `Run`; design question still the blocker, not the
port.

**Promote when:** a live run shows a cross-turn loop that per-`Run` detection missed.

Priority: Tier 4 — real but unproven, and the false-positive risk is higher than the current detector's.

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of six daemon-singleton services are per-session-scoped; `lsp.Manager` was deliberately left
shared — its per-session resource-growth tradeoff was judged worse than the isolation gap. Parked
pending a concrete multi-tenant need.

**Re-verified 2026-08-06:** still one shared `lsp.NewManager` at daemon construction. No trigger fired.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

### P63.10 — Two small TUI message-handling asymmetries, seen while splitting `Update`

Both pre-existing, found while reading every `Update` case during an unrelated refactor and
deliberately left in place (fixing a bug inside a no-behavior-change refactor destroys the property
that made the refactor safe).

1. **The spinner tick chain dies while idle.** `updateSpinnerTick` drops the `tea.Cmd` returned by
   `m.sp.Update(msg)` when `!m.streaming`; only the streaming branch re-queues. Looks intentional, but
   the chain is *terminated* rather than paused, so it depends on something else re-starting it at the
   next stream — worth confirming that always happens.
2. **A stale toast expiry can retire a newer toast.** `updateToastExpired` clears `m.activeToast`
   unconditionally, without checking the expiry identifies the toast currently shown. Two toasts in
   quick succession cut the second one short by the first one's timer. Fix needs a toast identity to
   compare before clearing.

Priority: Tier 4 — both cosmetic, neither reachable as a correctness/security problem. No trigger; fix
opportunistically if either file is open for another reason.

### P65.4 — Resume is phase-granular, artifact-inferred, and only the drive has it

**What Aegis has, stated carefully — the gap is narrower than it first looks.** The phased drive
already resumes well: a phase whose files carry no `PENDING` marker costs zero model turns on re-entry,
and the whole reset ladder is built on re-entering from disk. Two limits:

- **It is phase-granular and the granularity is the artifact** — the oracle is the `PENDING` marker in
  the skill's own scaffolded files, so a crash 40 turns into phase 6 re-runs phase 6 from its start.
  Probably the right trade at the drive's scale.
- **It exists only because the *skill* supplies the oracle.** A plain TUI/web-UI session, a cron job, a
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

**Promote when:** P65.2 has shipped its summary format, **and** a real session loses reasoning worth
keeping to a `/rewind` or a fork. Both conditions, not either.

Priority: Tier 4 — no trigger, sequenced behind P65.2, changes what a well-understood command means.

---

## Verification Work

**Status: 6 open.** Every item here has its code already written and merged — nothing below is a
design or implementation task. Each is closed by running a live-model harness and recording the
result the item's closure condition names, not by writing more code. They are **not tiered**:
tiering answers "how urgent is this build," and there is no build left to prioritize. **Five of the
six share one harness** (a local model driving `aegis chat --skill threat-modeling` /
`TestLiveWorkflow`) and are listed first so one live-tier setup can answer all five in a sitting; the
sixth (P62.8) is blocked on hardware, not on scheduling. P66.22 is the newest and is the only one
with a *sequencing* constraint — it must run after the P66 fixes it measures, not before.

### P66.22 — The LLM-tier findings are all estimates; one live run converts them to measurements

The P66 review never ran a live model. **LLM-01, LLM-02, LLM-03, LLM-10 and ARCH-04 are all claims
about runtime behaviour against a local model, argued entirely from source.** The arbitration upheld
all five and they are well-argued — but CLAUDE.md is emphatic that this class of claim is settled by
measurement, and this document has twice recorded a fixed instrument *inverting* an already-acted-on
verdict.

One `TestLiveWorkflow` run against `qwen3:14b-32k` answers all of them, and it is the same harness
P38.1, P62.9 and P65.2's prompt half already need — so this costs no additional setup if scheduled
with them.

**Closure conditions**, each a number this review could only estimate:

- **LLM-01** — the measured base-prompt token count with a realistic `CLAUDE.md` present, against the
  4,550 ceiling and against the served window. The estimate is 11,611 tokens for the context files
  alone.
- **LLM-02** — the turn at which compaction actually fires against the turn the engine's trigger
  wanted, at a pinned 4,096 window. The claim is that the summarizer refuses until 3,277 when the
  engine asked at 2,048.
- **LLM-03** — whether the P62.4 correction ever fires on the `openai` + `:11434/v1` path. Expected:
  it does not, and the session runs on the uncorrected 20-33% undercount.
- **LLM-10** — whether a model reload occurs between the tool-call probe and the first real turn.
- **ARCH-04** — whether a fan-out or debate call trips `MaxTurnStall` before its own timeout.

Run it **after** P66.7 and P66.14 ship, not before: those change three of the five numbers, and
measuring the pre-fix state answers a question nobody will have afterwards. Use `-count=1` — a
re-run without it replays Go's cached verdict, which this document has been caught by before.

Priority: Verification — one run, five answers. Sequence after the Tier 2/3 items it measures.

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
dated log (2026-07-21 through 2026-08-09) is in [releases.md](releases.md) (*P38.1 re-test log*).
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
`edit_file`, because an anchored edit asks the model to *reproduce* text rather than only produce it.
Handle-based tools (`fill_marker`, `edit_section`) remove the reproduction step and are what finally
made the fill loop reliable on a 14B model.

**Reproduce:** `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads outside
the workspace root); run
`aegis chat "threat model this repo" --skill threat-modeling --mode build --yes` (the prompt is
required). It prints a `phased mode` notice and resets context each phase.

**Closure condition:** the real suite's PENDING markers reach zero and `verify.py` / `lint_dfd.py` /
`inventory.py --check` all pass, **unattended, in one invocation**. Met once, 2026-07-24 on
FirewallRuleAnalyzer; not repeated since.

Priority: Verification — every load-bearing harness fix the re-tests have root-caused has shipped
(P39.5-P39.18, P47.1-P47.9, P52.12, P57.1). This item stays open only as the conformance **umbrella**,
closeable once a live built-in drive is confirmed to reach a verify-clean suite unattended, in one
invocation, on a local model. No code work remains; it is live-run tracking.

### P62.9 — The exposed-schema half of the base prompt: five editing tools and three prose blocks

**Built 2026-08-14** (local-profile base prompt 4,907 → 4,317 estimated tokens): `edit_file` deferred
under the local profile with the four P39.16 handle-based tools left exposed, and local variants of the
three shared prose blocks that compress rather than drop rules. `ScopeExposed` was also fixed to load a
named deferred tool for a drive phase's declared surface instead of leaving it silently hidden — two
phases (`dfdPhaseTools`, `assessmentPhaseTools`) had been running prompts naming tools not in their
arrays since the day they were written. Full write-up in [releases.md](releases.md).

**Closure condition (not met):** a live-tier measurement (`TestLiveWorkflow`) showing the agent's
behaviour is not worse, watching two things: whether a small model with `edit_file` deferred actually
reaches the handle-based tools instead of burning a turn on `tool_search`, and whether the compressed
`completing-tasks` block still holds the write-the-file rules a small model was measured dropping
first.

**First live evidence, 2026-08-14 (qwen3:14b, seeded-bug task via `aegis chat`, three runs per arm).**
The `tool_search` detour did not happen — across three runs the model went straight to `edit_section`
or `multi_edit`. A pointer defect was found and fixed instead: `edit_section`'s description and
no-headings error both pointed at the deferred `edit_file`, costing one run three failed calls and a
tool-failure-breaker trip before it reached `multi_edit`; both now name `multi_edit`, exposed under
both profiles. What's unanswered is turn cost, not correctness: deferred-surface runs solved the task
in 4-6 tool calls against a steady 3 with `edit_file` exposed, but a control arm with `edit_file`
exposed also failed the task outright twice (by explaining the fix in prose instead of applying it),
so single-run differences on this task are inside the noise, and **no default-prose control has been
run** for the second watch item.

**What would close it:** the same task at n≥10 per arm, or a task whose edit is unambiguous enough
that a single run means something, plus a default-prose control for the prose-attributable failures
above. Both are runs, not code.

Priority: Verification — the code is in; what remains is verification competing with P38.1 for the
same scarce live tier, and they can be run in one sitting.

### P65.2 — Compaction summaries are free prose, and nothing carries the file set forward (prompt half)

**Deterministic half shipped 2026-08-14**: `<read-files>`/`<modified-files>` tags now accumulate
across compactions and survive the fallback path, carried via a context decorator
(`engine.FileContextCompactor`) since `Summarizer` is built once per server and shared across sessions.
Cost measured at delta 66 tokens for the skeleton, 33 tokens for a 10-path file list (17 at the 40-path
cap) — comfortably inside budget. Full write-up in [releases.md](releases.md).

**What remains, and it is a run rather than code:** the prompt half — a fixed summary skeleton (`##
Goal` / `## Constraints` / `## Progress` / `## Key Decisions` / `## Next Steps`) instead of free-form
"use terse bullet points" — is built but held open on its own stated gate: a live run showing a local
model fills the skeleton without losing content the terse-bullet prompt kept. Free-form compression is
*generation* and structured fill is *completion*, and every measurement in the P38.x line says local
models degrade on the first and hold up on the second — this is the last unstructured-prose ask left in
the engine, at the moment the model's context is fullest.

**Promote when:** P38.1's re-run is done and the live tier is free — the prompt change wants the same
harness, so running them together costs one setup instead of two.

Priority: Verification — real value, unblocked, code already built, gated on live evidence rather
than on design.

### P65.3 — The summarizer and the guard ride the conversation's cache posture, and neither is reused

**Mechanism shipped 2026-08-15.** Question 1 (cloud) is answered by code-reading, not inference: the
compaction summarizer (`compaction.go:684`) and the output guard's validation pass (`guard.go:194`)
both call `Stream` on the *same shared* Anthropic adapter instance the conversation uses
(`s.modelAdapter`), which emitted `cache_control` breakpoints unconditionally whenever prompt caching
was on — so both were billed a cache **write** for a one-off prompt with no possible matching read.
Worse than the roadmap's framing: neither call site ever read `ev.Usage` off the stream, so that cost
wasn't just unattributed, it was invisible to Aegis entirely. Confirmed, not refuted — promoted and
built same day.

**Fix:** `provider.Request.SuppressCache` (`provider.go`, alongside `NumCtx`/`Format`) — set `true` by
the summarizer and the guard, honored by the Anthropic adapter (`cache := a.cache && !req.SuppressCache`,
`anthropic.go:313`) to skip breakpoints on `System`, `Tools`, and the last message block; every other
adapter ignores the field, same pattern as `NumCtx`. `TestPromptCachingSuppressedPerRequest` pins it.
Debate roles were *not* touched — the roadmap named them as another rider on the shared adapter, but a
debate role's prompt is not necessarily one-off the way a summarizer/guard call is, and no measurement
established it needs the same suppression; leaving it is the narrower change.

**What remains — Question 2 (local), still gated on live evidence:** does a summarization or guard call
between turns measurably raise the *next* turn's prefill on a local backend? The P62.2 re-measurement
apparatus answers this, `-count=1` discipline included, but needs a reachable local model server this
pass did not have. The dropped-`Usage` gap found while answering Question 1 (compaction/guard discard
`EventDone.Usage` entirely rather than attributing it) is a separate, adjacent item — not required to
close this one, worth its own line if someone wants session cost totals to include it.

**Promote when:** a live local-tier session is available — same harness as P38.1/P62.9/P65.2.

Priority: Verification → mechanism closed; the local half stays open behind the same live-tier gate
as the other three items above.

### P62.8 — The prefix-cache gate's large-window regime has never been measured

`compaction.shouldPrune` has two regimes: below `largeContextWindowThreshold` (200,000) it fires at a
25%-free ratio; above it, a fixed 40k buffer, which on a large window places the prune much earlier in
relative terms than anything measured so far. Everything known about this gate comes from a
24,576-token window (the ratio branch only), and P62.2's history is a specific warning against
generalising from it — the same fixture gave opposite verdicts before and after an instrument fix,
because what mattered was *where in the window* the prune landed relative to the backend's
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
