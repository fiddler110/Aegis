# Aegis Capability Roadmap

- [Aegis Capability Roadmap](#aegis-capability-roadmap)
  - [Status](#status)
    - [Standing constraints on the open batches](#standing-constraints-on-the-open-batches)
    - [Decisions that outlive the items that made them](#decisions-that-outlive-the-items-that-made-them)
  - [Tiering Criteria](#tiering-criteria)
  - [Up next](#up-next)
  - [Threat model 2026-08-31 — the P81 batch](#threat-model-2026-08-31--the-p81-batch)
  - [Open Work — Tier 1](#open-work--tier-1)
    - [P81.6 — `provider.base_url` is already security-relevant — REFUTED (FIND-06)](#p816--providerbase_url-is-already-security-relevant--refuted-find-06)
  - [Open Work — Tier 2](#open-work--tier-2)
    - [P76.2 — Quit doesn't cancel a running interactive-terminal command](#p762--quit-doesnt-cancel-a-running-interactive-terminal-command)
    - [P79.1 — Windows read-only-shell classifier no longer detects absolute-path escapes (regression, PR #51)](#p791--windows-read-only-shell-classifier-no-longer-detects-absolute-path-escapes-regression-pr-51)
    - [P81.9 — The session workdir allowlist is skipped on exactly the bind it ships with (FIND-09)](#p819--the-session-workdir-allowlist-is-skipped-on-exactly-the-bind-it-ships-with-find-09)
    - [P81.13 — Sandbox and scanner images are mutable tags, and `verify-image` is a separate command (FIND-13)](#p8113--sandbox-and-scanner-images-are-mutable-tags-and-verify-image-is-a-separate-command-find-13)
    - [P81.15 — Every cost and token budget defaults to unlimited (FIND-15)](#p8115--every-cost-and-token-budget-defaults-to-unlimited-find-15)
    - [P81.16 — The unauthenticated `/ui` mint can be flooded to deny the operator's own UI (FIND-16)](#p8116--the-unauthenticated-ui-mint-can-be-flooded-to-deny-the-operators-own-ui-find-16)
    - [P81.17 — The committed `dist/` has no drift check that runs (FIND-17)](#p8117--the-committed-dist-has-no-drift-check-that-runs-find-17)
    - [P81.19 — `/healthz` is fine today and has no test keeping it that way (FIND-19)](#p8119--healthz-is-fine-today-and-has-no-test-keeping-it-that-way-find-19)
    - [P81.24 — Conversation history, checkpoints and spill are unprotected on Windows (FIND-24, ACL half)](#p8124--conversation-history-checkpoints-and-spill-are-unprotected-on-windows-find-24-acl-half)
    - [P81.25 — The daemon token never rotates and the pinned certificate regenerates silently (FIND-25)](#p8125--the-daemon-token-never-rotates-and-the-pinned-certificate-regenerates-silently-find-25)
    - [P81.29 — `recon_scan` auto-authorizes the operator's entire LAN (FIND-29)](#p8129--recon_scan-auto-authorizes-the-operators-entire-lan-find-29)
    - [P81.32 — Scan reports land inside the repository they describe (FIND-32)](#p8132--scan-reports-land-inside-the-repository-they-describe-find-32)
    - [P81.33 — The TUI render path is unbounded, and a parallel round trains click-through (FIND-33)](#p8133--the-tui-render-path-is-unbounded-and-a-parallel-round-trains-click-through-find-33)
  - [Open Work — Tier 3](#open-work--tier-3)
    - [P76.1 — Audit the codebase's unread 26%: `internal/tui` and `internal/security`](#p761--audit-the-codebases-unread-26-internaltui-and-internalsecurity)
    - [P76.3 — A hostile repo can plant its own security-scan baseline to hide its own findings](#p763--a-hostile-repo-can-plant-its-own-security-scan-baseline-to-hide-its-own-findings)
    - [P80.1 — An MCP client can list, and post turns into, sessions it did not create](#p801--an-mcp-client-can-list-and-post-turns-into-sessions-it-did-not-create)
    - [P81.1 — Untrusted content is marked, not contained (FIND-01)](#p811--untrusted-content-is-marked-not-contained-find-01)
    - [P81.8 — `web_fetch` is an unreviewed egress channel (FIND-08)](#p818--web_fetch-is-an-unreviewed-egress-channel-find-08)
    - [P81.5 — Outbound provider payloads and tool arguments are never redacted (FIND-05)](#p815--outbound-provider-payloads-and-tool-arguments-are-never-redacted-find-05)
    - [P81.2 — External MCP servers are trusted on configuration alone (FIND-02)](#p812--external-mcp-servers-are-trusted-on-configuration-alone-find-02)
    - [P81.3 — Config PATCH endpoints can disable command isolation with only the bearer token (FIND-03)](#p813--config-patch-endpoints-can-disable-command-isolation-with-only-the-bearer-token-find-03)
    - [P81.4 — The web UI hands the real daemon token to browser JavaScript (FIND-04)](#p814--the-web-ui-hands-the-real-daemon-token-to-browser-javascript-find-04)
    - [P81.10 — The container workspace mount is broader than any single command needs (FIND-10)](#p8110--the-container-workspace-mount-is-broader-than-any-single-command-needs-find-10)
    - [P81.12 — Release artifacts ship without checksums, signatures or provenance (FIND-12)](#p8112--release-artifacts-ship-without-checksums-signatures-or-provenance-find-12)
    - [P81.14 — There is no default audit trail for anything privileged (FIND-14)](#p8114--there-is-no-default-audit-trail-for-anything-privileged-find-14)
    - [P81.20 — Plan mode's guarantee rests on a 1,129-line classifier, structurally (FIND-20)](#p8120--plan-modes-guarantee-rests-on-a-1129-line-classifier-structurally-find-20)
    - [P81.22 — Command isolation degrades to unsandboxed host execution on a warning line (FIND-22)](#p8122--command-isolation-degrades-to-unsandboxed-host-execution-on-a-warning-line-find-22)
    - [P81.23 — Scheduled jobs run unattended in whatever mode they were created with (FIND-23)](#p8123--scheduled-jobs-run-unattended-in-whatever-mode-they-were-created-with-find-23)
    - [P81.26 — Sandboxed commands inherit the daemon environment minus a denylist (FIND-26)](#p8126--sandboxed-commands-inherit-the-daemon-environment-minus-a-denylist-find-26)
    - [P81.27 — Trust grants are unauthenticated local state, and `.aegis/.env` is outside the fingerprint (FIND-27)](#p8127--trust-grants-are-unauthenticated-local-state-and-aegisenv-is-outside-the-fingerprint-find-27)
    - [P81.30 — Parallel rounds do not order shell commands against concurrent writes (FIND-30)](#p8130--parallel-rounds-do-not-order-shell-commands-against-concurrent-writes-find-30)
  - [Open Work — Tier 4](#open-work--tier-4)
    - [P80.2 — Three packages the security audit never read](#p802--three-packages-the-security-audit-never-read)
    - [P80.3 — `Server`'s 60-field struct, after the file split](#p803--servers-60-field-struct-after-the-file-split)
    - [P74.21 — The local-model harness still can't touch a prompt or a tool description](#p7421--the-local-model-harness-still-cant-touch-a-prompt-or-a-tool-description)
    - [P71.6 — Nothing memoizes a fetch or a search within a session](#p716--nothing-memoizes-a-fetch-or-a-search-within-a-session)
    - [P71.7 — `web_search` results carry no publication date, so the source-quality bar cannot be applied](#p717--web_search-results-carry-no-publication-date-so-the-source-quality-bar-cannot-be-applied)
    - [P71.11 — The deep-research budgets are cloud-window constants handed to a local model](#p7111--the-deep-research-budgets-are-cloud-window-constants-handed-to-a-local-model)
    - [P71.12 — Main-content extraction for `web_fetch` — measured, and smaller than it looks](#p7112--main-content-extraction-for-web_fetch--measured-and-smaller-than-it-looks)
    - [P71.13 — Aegis could manage its own SearXNG container instead of only pointing at one](#p7113--aegis-could-manage-its-own-searxng-container-instead-of-only-pointing-at-one)
    - [P66.26 — `synchronous=NORMAL` on the three SQLite databases (PERF-02, refiled from P66.9)](#p6626--synchronousnormal-on-the-three-sqlite-databases-perf-02-refiled-from-p669)
    - [P66.17 — Local-model path: the Low-severity residue](#p6617--local-model-path-the-low-severity-residue)
    - [P66.23 — Go-code security residue](#p6623--go-code-security-residue)
    - [P66.18 — Architecture, quality and maintainability residue](#p6618--architecture-quality-and-maintainability-residue)
    - [P66.19 — Capability gaps with no fired trigger](#p6619--capability-gaps-with-no-fired-trigger)
    - [P77.6 — No OS-level process sandbox on Windows (GAP-05, spun out of P66.19)](#p776--no-os-level-process-sandbox-on-windows-gap-05-spun-out-of-p6619)
    - [P66.20 — Efficiency residue](#p6620--efficiency-residue)
    - [P64.4 — Edit results carry no diff, and a tool cannot attach anything a replay can render](#p644--edit-results-carry-no-diff-and-a-tool-cannot-attach-anything-a-replay-can-render)
    - [P64.5 — `ask_user` is one free-form question; unattended answers cannot be routed](#p645--ask_user-is-one-free-form-question-unattended-answers-cannot-be-routed)
    - [P61.7 — Retry/terminal classification over _backend-echoed_ text (remainder)](#p617--retryterminal-classification-over-backend-echoed-text-remainder)
    - [P60.3 — Checkpoints capture files only, so `/rewind` is silent about everything else](#p603--checkpoints-capture-files-only-so-rewind-is-silent-about-everything-else)
    - [P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)](#p5214--session-scoped-loop-detector-cross-run-loops-are-invisible)
    - [P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)](#p259--per-session-scoping-of-lspmanager-remaining-daemon-singleton)
    - [P65.4 — Resume is phase-granular, artifact-inferred, and only the drive has it](#p654--resume-is-phase-granular-artifact-inferred-and-only-the-drive-has-it)
    - [P65.5 — Rewinding away from a branch discards its work instead of summarizing it forward](#p655--rewinding-away-from-a-branch-discards-its-work-instead-of-summarizing-it-forward)
    - [P67.10 — Four seams the tool interface does not have](#p6710--four-seams-the-tool-interface-does-not-have)
    - [P67.11 — Every budget is a ceiling; none expresses how much effort is wanted](#p6711--every-budget-is-a-ceiling-none-expresses-how-much-effort-is-wanted)
    - [P67.12 — Personas cannot accumulate anything across runs](#p6712--personas-cannot-accumulate-anything-across-runs)
    - [P67.13 — There is no way to execute a plan without committing to it](#p6713--there-is-no-way-to-execute-a-plan-without-committing-to-it)
    - [P67.14 — Hand-composed ANSI has no rule about transitions versus state](#p6714--hand-composed-ansi-has-no-rule-about-transitions-versus-state)
    - [P81.7 — The local model endpoint is unauthenticated plaintext HTTP on loopback (FIND-07)](#p817--the-local-model-endpoint-is-unauthenticated-plaintext-http-on-loopback-find-07)
    - [P81.18 — The self-signed certificate warning conditions operators to click through (FIND-18)](#p8118--the-self-signed-certificate-warning-conditions-operators-to-click-through-find-18)
    - [P81.28 — Prose tool-call parsing can promote quoted untrusted text into real calls (FIND-28)](#p8128--prose-tool-call-parsing-can-promote-quoted-untrusted-text-into-real-calls-find-28)
    - [P81.31 — Checkpoint growth is unbounded and `/rewind` can silently discard outside edits (FIND-31)](#p8131--checkpoint-growth-is-unbounded-and-rewind-can-silently-discard-outside-edits-find-31)
    - [P81.11 — The merge gate exists, passes, and does not run — accepted risk (FIND-11)](#p8111--the-merge-gate-exists-passes-and-does-not-run--accepted-risk-find-11)
  - [Verification Work](#verification-work)
    - [P80.4 — `live_workflow`'s two standalone tests both need a stronger model than this machine has run](#p804--live_workflows-two-standalone-tests-both-need-a-stronger-model-than-this-machine-has-run)
    - [P66.22 — The LLM-tier findings are all estimates; one live run converts them to measurements](#p6622--the-llm-tier-findings-are-all-estimates-one-live-run-converts-them-to-measurements)
    - [P38.1 — Non-orchestrated, single-context threat-model build (primary path for local models)](#p381--non-orchestrated-single-context-threat-model-build-primary-path-for-local-models)
    - [P68.4 — The triage rubric's measuring band sits below the strongest local model](#p684--the-triage-rubrics-measuring-band-sits-below-the-strongest-local-model)
    - [P68.5 — P52.16's `toolResultEcho` measurement was taken through a defective template](#p685--p5216s-toolresultecho-measurement-was-taken-through-a-defective-template)
    - [P68.6 — The 14b family never produces the report, and nothing in the run says why](#p686--the-14b-family-never-produces-the-report-and-nothing-in-the-run-says-why)
    - [P62.9 — The exposed-schema half of the base prompt: five editing tools and three prose blocks](#p629--the-exposed-schema-half-of-the-base-prompt-five-editing-tools-and-three-prose-blocks)
    - [P65.2 — Compaction summaries are free prose, and nothing carries the file set forward (prompt half)](#p652--compaction-summaries-are-free-prose-and-nothing-carries-the-file-set-forward-prompt-half)
    - [P62.8 — The prefix-cache gate's large-window regime has never been measured](#p628--the-prefix-cache-gates-large-window-regime-has-never-been-measured)

**Last updated:** 2026-08-31 (third entry the same day) — **the three items that had led [Up
next](#up-next) since 23/30 August are worked**: **P76.2** (closed — the fix was already in the tree;
what shipped is `TestQuitPathsCancelTheTerminalRun`, which is what keeps it there), **P76.3** (closed —
a security-scan baseline read out of an untrusted scan target now suppresses nothing and is printed
entry by entry, so a repo shipping one to hide its own planted finding is louder than a repo shipping
none) and **P80.1**'s interim on both surfaces (`mcp_server.default_mode`, and `aegis acp --mode`, are
now ceilings on a session the caller _borrowed_, so the C1/F1 clamp is no longer bypassable by posting
into someone else's session; the session-origin schema decision stays open and stays Tier 3). Two of
the three were part-built already — the same thing the first wave found hours earlier, and now the
standing caution at the top of [Up next](#up-next). **P81.14** leads the table.

**Last updated (previous):** 2026-08-31 (second entry the same day) — **the P81 batch's first wave is worked**:
**P81.15**, **P81.16**, **P81.19**, **P81.29** and **P81.32** shipped, **P81.24**, **P81.25** and
**P81.27** shipped in part, and **P81.6** and **P81.11** closed without production code — refuted and
accepted respectively. Four disjoint-package sub-agents ran the wave; every finding was checked against
the tree before it was built, and **that is what most of the value turned out to be**. Three of the
eight items were substantially wrong as filed: `provider.base_url` is already frozen (the policy table
is in `freeze.go`, not the `fingerprint.go` the report cited), `sqlitestore.HardenPermissions` already
covers the `-wal`/`-shm` sidecars, and `workspacetrust.save()` already calls `fsguard`. What the passes
found _instead_ is the better haul — a spill directory inheriting the workspace ACL, `session.Prune`
skipping archived sessions so archiving made a conversation immortal, `daemon.crt` at `0o644` while
`daemon.key` was hardened, and `/healthz` publishing `sandbox_fallback_reason` to any process holding
no credential. Two roadmap-visible decisions came out of it: **P81.11** is an accepted risk, and
`freeze.go` gained a third trust policy, **`projectMayTighten`**, so a project may narrow a spend bound
and never loosen one. Records below and in [releases.md](releases.md).

**Last updated (previous):** 2026-08-31 — **P81.1**–**P81.33** filed (32 items; **P81.21** is deliberately absent)
from a full STRIDE-A threat model of the working tree at commit `88cea69`, output in
[threat-model-20260831-002123/](../threat-model-20260831-002123/0-assessment.md). Nothing shipped this
sitting: the batch is intake, not work. The report analysed 22 elements across 4 trust boundaries,
enumerated 138 threats and distilled 33 findings — **0 Tier 1 (nothing directly exploitable), 19
Tier 2, 14 Tier 3**, overall rating **Elevated** on a `LOCALHOST_SERVICE` classification. Its own
summary of the shape is the sentence worth keeping: _"the controls that constrain the machine are
strong; the controls that constrain the model are advisory."_ Loopback binding, TLS-by-default with a
pinned certificate, constant-time token compare behind an exponential lockout, the shared SSRF
blocklist, container capability-dropping and fingerprint-pinned trust grants all held up under review.
What did not: untrusted content is marked but not contained (**P81.1**), persona tool lists prompt
rather than enforce, `warnOutboundSecrets` warns rather than blocks, sandboxing degrades to unconfined
host execution on a warning line (**P81.22**), and the repo's own blocking merge gate runs only on
manual dispatch (**P81.11** — since **accepted as deliberate**; the triggers stay off and the residual
coverage question is recorded in its entry). Two findings landed on items already open — FIND-21 **is** **P80.1** and
got no new number, and FIND-20 is the structural half of **P79.1**, filed as **P81.20**. Index and
full mapping: [Threat model 2026-08-31 — the P81 batch](#threat-model-2026-08-31--the-p81-batch).

**Last updated (previous):** 2026-08-30 (second entry the same day) — **P79.2**, **P79.3** and **P79.4** filed
_and shipped_, and **P80.1**–**P80.4** filed, all out of finishing the comprehensive architecture and
security audit that had been sitting in `Review.md`. That document's own worklist was 24 of 26 findings
closed with two coverage-debt entries open; both were worked this sitting, and `Review.md` is gone —
its full record, register and phase evidence now live in
[releases.md](releases.md#comprehensive-architecture-and-security-audit-remediated-in-full-2026-08-30),
where completed work belongs. The three P79 items came out of running the `live_workflow` tier for the
first time (**C2**): the daemon released nothing unless it exited through `ListenAndServe`; compaction's
summarizer spent its entire completion budget on a thinking preamble and returned empty on every cycle;
and the empty-answer nudge re-asked over the channel that had just swallowed the answer — the last of
which was what made a 9B model look like it was failing a security-triage task at 3/12 when it was
scoring 12/12 and losing the reply. The **C1** unreviewed-surface pass closed `internal/mcpserver` (two
findings fixed, one filed as **P80.1**) and the `internal/swarm` subprocess backend (one Windows-ACL
fix). What did _not_ get finished is filed here as P80.1–P80.4 rather than left in a deleted document.

**Last updated (previous):** 2026-08-30 — **P79.1** filed. Surfaced while committing an unrelated gap-fix batch
(`research/gaps.md`): `TestReadOnlyGitArgvAgreesAcrossBothPaths`, `TestReadOnlyShellAttachedValueConfinement`,
`TestReadOnlyShellPowerShellPathConfinement` and `TestReadOnlyShellCommandWindowsPaths` in
`internal/tool/builtin` were already failing on a pristine pull of `main` (commit `519efbd`, PR #51's
security-hardening sweep), before any of that session's own changes — isolated by running the same
tests against that commit in isolation with everything else stashed. Not filed as shipped or even
triaged past "reproduces and isn't mine" — see the entry for what's known.

**Last updated (previous):** 2026-08-26 — **P78.1**–**P78.9** filed and shipped the same day. Filed from a
five-track code-quality audit (sprawl/duplication/gaps, not security — that axis is `CodeReview.md`'s
and **P76.1**'s) that read the whole tree in parallel by package group. All nine were opportunistic
Tier 4 items with no fired trigger, picked up together rather than left parked, run as seven parallel
subagents by disjoint package: the four god-file splits (**P78.1** `chat.go`, **P78.2** `engine.go`'s
`Run()`, **P78.3** `slash.go`, **P78.4** `drive.go`), the provider-layer cleanup (**P78.5** dedup'd
adapter helpers, **P78.6** `buildOne`'s struct bundle, **P78.7** Anthropic `Healthy()`), the
`config.go` PATCH-endpoint generic plus the `/config/cost` gap it surfaced (**P78.8**), and six small
residue findings (**P78.9**). Full record: [releases.md](releases.md#p781-p789-shipped-2026-08-26).

**Last updated (previous):** 2026-08-25 — **P77.1** shipped. It was parked Tier 4 pending "a user reports
specifically wanting the reasoning content itself" — the user did, directly. Investigating found the
roadmap entry's own premise stale: `provider.ThinkingBlock`/`EventThinkingDelta` and the TUI's live
dim-text-then-collapsible-block rendering (`ctrl+o` to expand) already existed end to end for both
Anthropic and Ollama/OpenAI-compat adapters — the entry's "nothing shows reasoning" was true only in
the sense that every path was opt-in and undiscoverable. The user chose the narrowest fix: native
Ollama's `provider.think` now defaults to `true` instead of `false` (`internal/providerfactory/factory.go`),
since local reasoning is unbilled (unlike Anthropic's thinking budget, left opt-in) and a model that
rejects the parameter already has a graceful one-shot-400-then-latch fallback (P38.5). Live-verified
against this machine's own Ollama server with the config default unset: `aegis-qwen35-9b:16k` streamed
real `EventThinkingDelta` content, while `aegis-phi4-reasoning:16k`/`phi4-mini-reasoning:3.8b` 400'd
("does not support thinking") and were absorbed by the existing retry/latch path. Full record:
[releases.md](releases.md#p771-shipped-2026-08-25).

**Last updated (previous):** 2026-08-24 — **P77.4** shipped (a `fetchCmd[T]` generic now backs the four
`tui.go` command constructors that were a genuine single-call round trip — `fetchTeammates`,
`fetchTeammatesQuiet`, `fetchSessions`, `switchSessionCmd` — closing out the last open item from the
`internal/tui/tui.go` cleanup pass; **P77.2**, **P77.3**, and **P77.5** shipped earlier the same day).
`fetchBacktrackTargets`/`forkAndSwitchCmd` (multi-step, branching) and `startStream`/`startDrive`
(`context.WithCancel`, not a timeout) stayed literal — forcing those through the generic would have
cost more in accommodating parameters than it saved. Full record:
[releases.md](releases.md#p774-shipped-2026-08-24).

**Last updated (previous):** 2026-08-23 — **P76.1**'s two sessions (the scoped, read-only audit of `internal/tui`
and `internal/security` filed 2026-08-21) both ran this sitting and closed, each with one survivor:
**P76.2** (Tier 2 — a TUI quit path that doesn't cancel a running interactive-terminal command) and
**P76.3** (Tier 3 — a hostile repo can plant its own security-scan baseline to hide its own findings,
needs a design decision). Earlier the same day: the un-numbered PXX.1 request closed out (its concrete
asks were already shipped; its one unaddressed thread — visibility into the model's reasoning —
refiled as **P77.1**, Tier 4), and **P68.3** compressed to a pointer now that its full record lives in
[releases.md](releases.md), matching how **P68.2** was already handled. Before it: 2026-08-22, after
**P68.1** shipped (the live tier can now run a measurement it can read back — closing Tier 2 out).
This document tracks only **open** work and what's next. For
shipped-feature history, batch origins, pass-by-pass narrative, refutation records and full design
rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading with a
`Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so keep it when
adding items.

---

## Status

**62 open items: 53 build + 9 verification-only.** Eight P81 items closed on 2026-08-31 (five
shipped, two closed without code, one — **P81.19** — shipped and superseded by the larger gap it
uncovered); three more shipped in part and stay open for their remainder. Later the same day
**P76.2** and **P76.3** closed and **P80.1** shipped its interim (open for the schema decision only),
clearing the three items that had led [Up next](#up-next). Tier 1: **0**. Both of the P81 batch's Tier 1
items closed on 2026-08-31 without production code: **P81.6** was **refuted** (the trust freeze
already covers `provider.base_url` — the report looked in `fingerprint.go`, the policy table is in
`freeze.go`), and **P81.11** is an **accepted risk** re-tiered to 4 after the operator confirmed the
CI disablement is deliberate. Two of the report's own Quick Wins, neither of which was a defect. Tier 2: 12 (**P79.1**, filed
2026-08-30, a Windows read-only-shell confinement regression; and 11 from the P81 threat-model batch —
**P76.2** closed 2026-08-31). Tier 3: 17 (**P76.1**, filed
2026-08-21, both sessions now done — see its entry; **P80.1**, filed 2026-08-30, whose interim shipped
2026-08-31 and which is now open only for the session-origin schema decision, not for the escalation
it described; and 15 from the P81 batch, including **P81.1**, the threat model's one `Critical`.
**P76.3** closed 2026-08-31). Tier 4: 31 (**P80.2** and
**P80.3** added 2026-08-30; **P81.7**, **P81.18**, **P81.28** and **P81.31** added 2026-08-31, plus
**P81.11** re-tiered there the same day).
Verification: 9 (**P80.4** added 2026-08-30).

**The 2026-08-31 threat-model batch was 32 build items when filed; 24 remain.** That is intake from
an external analysis rather than a backlog that accumulated. The first wave's result is the number
worth carrying forward: of eight items worked, **three were refuted against the tree and two of the
five real ones were smaller than filed** — while the same passes turned up four defects the report
never saw. Treat a finding's premise as a claim to check, not a work order. See
[Threat model 2026-08-31 — the P81 batch](#threat-model-2026-08-31--the-p81-batch) for the
finding-to-item index and the suggested order.

**Shipped history lives in [releases.md](releases.md), not here.** This document tracks only open
work and current counts; a completed item's full record — what it was, what building it found, and
what was measured to close it — moves there the day it ships. Most recent sittings, newest first,
each a pointer only: **2026-08-21** — **P63.10**, **P75.1**, and the last three of the P74 batch
(**P74.15**–**P74.17**), closing it out entirely (twenty items, P74.1–P74.20). **2026-08-20** —
seventeen more of the P74 batch, filed and shipped the same day. **2026-08-19** — fourteen items
across three sittings (**P71.1–P71.5/P71.9/P71.10**, **P72.1–P72.3**, **P73.1–P73.2**), several
filed by the live verification of the item before them. **2026-08-18 and earlier** — **P70.1–P70.4**,
**P66.13–P66.15**, **P67.6–P67.9**, **P69.1/P69.5/P69.6**, and the rest of the P66 review batch. Full
records, mechanisms and measurements for every one of these are in
[releases.md](releases.md#latest-changes).

**Every shipped item was closed against a live-verified test or a live probe run on this machine,
recorded in its release entry — never asserted from reading a diff.** That standard constrains future
work here.

Tiers 1-4 are **build work** — every item there requires writing code that doesn't exist yet.
**Verification work** is its own track, not a tier: every item in it is code that is _already
written_, sitting behind one gate — a live-model run producing evidence the item's closure
condition names. Mixing the two under one tiering scheme was misleading a reader into treating
"go run a test" and "go design and build a feature" as the same kind of next action. See
[Verification Work](#verification-work) for that track's own status.

**Everything left in the verification track is blocked on something other than a model server**,
which is why parking it costs little. P38.1 needs permission to launch an unattended auto-approving
agent; P62.9 needs a _better task_ rather than more runs of the current one; LLM-03, LLM-10, ARCH-04
and P65.2 all need a session trace from a run whose data dir survives — **P68.1** shipped that
2026-08-22 (live-verified against `TestLiveWorkflow`/`aegis-qwen35-9b:16k`: the kept data dir's
`sessions.db` outlived the test process and `aegis sessions trace <id>` printed the compaction
summary text, the calibration sample count and each turn's stop reason), so those four now have
readable evidence to judge whenever the parked live-tier row is next picked up — nobody has yet.
Only P62.8 is still purely waiting on hardware.

**No Tier 4 build item currently has a fired trigger** (re-verified 2026-08-15: `sandbox.backend`
still defaults to `"local"`, `lsp.Manager` is still one shared daemon singleton, both TUI
asymmetries in P63.10 are still present as described — that one item has since shipped) — see each
entry's **Promote when** for what would change that. Two of them, **P71.6** (response caching) and
**P71.11** (window-derived budgets), were held pending phasing — "setting them first fits a constant
to a regime about to change" — and that regime changed when P71.8 landed; the reason they were
parked no longer applies, so re-check them rather than assuming Tier 4 still fits. **P71.12** is the
opposite case: a filed _negative_ measurement (main-content extraction is worth 3–12% per page,
because the existing converter already takes 66 KB of HTML down to 11 KB of text), recorded so
nobody re-derives it. Explicitly do not schedule.

**P66.26** (PERF-02) is the one refiled sub-item still open, and it stays Tier 4: a Low-severity
durability trade on the one database holding checkpoints, the cost ledger and traces, with P66.9
having already removed most of the pressure behind it.

### Standing constraints on the open batches

**The three P67 constraints, which apply to every P67.10–P67.14 entry and are not repeated in them.**
That batch is a comparative reading of an external agent implementation, not a review of this
codebase: on 2026-08-16 the leaked Claude Code CLI source was read against Aegis for mechanisms worth
having here.

- **That source is leaked proprietary code. Nothing may be transcribed from it.** Each item is a
  design reading — a mechanism and the reasoning behind it — and needs an independent Go
  implementation written from this document, not from that repository.
- **The leak is partial.** `src/utils/**` is absent, so permission internals, `forkedAgent` and
  `toolResultStorage` were legible only through call sites. Where an entry's claim about _their_
  implementation rests on a call site rather than the code, it says so.
- **Every claim about Aegis in these entries was checked against this tree**, at the file and line
  cited, not against the docs. The claims about their side were not, and cannot be — treat them as
  motivation, never as a specification.

**The two P74 constraints, which apply to every P74.\* entry and are not repeated in them.** That
batch is a comparative reading of two external agent implementations, filed 2026-08-20.

- **`tanbiralam/claude-code` contains two rendering modes and the batch was first read against the
  wrong one.** `src/utils/fullscreen.ts:112` gates them on `process.env.USER_TYPE === 'ant'`:
  alt-screen fullscreen internally, inline document flow for external users. The fullscreen path has
  its own virtual scroll, transcript search and mouse selection. **Every TUI entry here has been
  re-read against the alt-screen mode**; P74.2 was rewritten and P74.18 was filed as a result. Anyone
  adding to this lane should check which mode a mechanism belongs to before filing it.
- **`langchain-ai/deepagents` is Apache-2.0 and Python; `tanbiralam/claude-code` is neither.** The
  second repository is a reconstruction of a shipped Claude Code bundle — its own README states the
  source is Anthropic's property, several modules carry a literal `not included in leaked source`
  stub, and the TypeScript has been through the React compiler. It is the same class of source as the
  P67 batch and carries the same rule: **nothing may be transcribed from it.** Each TUI entry is a
  reading of _observed interface behaviour_ — glyphs, layout decisions, gating thresholds — and needs
  an independent Bubbletea implementation written from this document. The practical point reinforces
  the legal one: it is React and Ink, and none of it is portable anyway.
- **Every claim about Aegis in these entries was checked against this tree**, at the file and line
  cited. **P74.1 goes further and was proved by running the real gate** — a throwaway test against
  `NewRuleGate` with the actual `grep` schema, not a reading of `subjectFor`. Claims about either
  external side were not checked and cannot be: treat them as motivation, never as a specification.

The batch was filed from a review artifact whose finding ids differ from the roadmap's, because the
roadmap renumbers into implementation order. The mapping, for anyone reading the two side by side:

| Roadmap | Artifact | Roadmap | Artifact | Roadmap | Artifact            |
| ------- | -------- | ------- | -------- | ------- | ------------------- |
| P74.1   | SEC-1    | P74.7   | TUI-5    | P74.13  | TUI-9               |
| P74.2   | TUI-1    | P74.8   | DA-2     | P74.14  | DA-5                |
| P74.3   | TUI-2    | P74.9   | DA-3     | P74.15  | DA-6                |
| P74.4   | TUI-3    | P74.10  | TUI-8    | P74.16  | DA-4                |
| P74.5   | TUI-4    | P74.11  | TUI-7    | P74.17  | DA-1                |
| P74.6   | TUI-6    | P74.12  | TUI-10   | P74.18  | _(new, 2026-08-20)_ |
|         |          |         |          | P74.19  | _(new, 2026-08-20)_ |
|         |          |         |          | P74.20  | _(new, 2026-08-20)_ |

**The P66 entries here are deliberately grouped grab-bags**, each collecting the Low-severity residue
of one review domain, filed so no finding is lost rather than because any of them should be
scheduled. Take one only when already working in that file. The review itself — six specialist
reviewers, an adversarial debate and a static-analysis pass, 70 findings against HEAD `3c2b57b` — is
in [CodeReview.md](CodeReview.md) with per-finding evidence. **Read the corrections in releases.md
before acting on that document directly:** several shipped items contradict the finding they were
built from (VULN-03's suggested `::ffff:0:0/96` addition would have blocked the entire public
internet; LLM-04 drops _every_ tool call on a 1-based backend, not only trailing ones).

### Decisions that outlive the items that made them

**Three trust-posture questions were answered on 2026-08-18 and they do not all point the same way,
which is the point.** The swarm mailbox **is** wrapped as untrusted (P70.2) and so is a sub-agent's
result (P70.4), because in both cases content crossed a boundary before being relayed onward;
`security_scan`'s workspace-derived output is **deliberately not** wrapped (P70.3) because a file the
model can already read directly is not a boundary crossing. Zero trust is the stated posture for
_ingestion_ and for _relayed_ content, not a rule that every byte gets a marker. Settle the next such
question against those three, not afresh.

**The TUI keeps alt-screen and the app-owned frame. Decided 2026-08-20, after two wrong answers.** The
question was how to get native-feeling scroll and selection. The first answer was "move to document
flow and delegate scroll, selection and search to the terminal" — a 4–6 day commit/live rewrite that
would have retired `/search`, deleted `selection.go`, and **silently given up re-wrap on resize**, since
content hard-wrapped and printed into scrollback can never reflow. The user caught it by asking whether
resize would still re-wrap.

**What the check found is the reusable part.** The comparison client ships _two_ rendering modes, and
`src/utils/fullscreen.ts:112` decides between them with `return process.env.USER_TYPE === 'ant'` —
**alt-screen fullscreen is the internal default; inline document flow is what external users get.** The
fullscreen path carries its own virtual scroll, its own transcript search and its own mouse selection
(the theme's `selectionBg` token is commented "alt-screen mouse selection"). The mode that re-wraps on
resize is the alt-screen one, which is the architecture Aegis already has.

So the settled position: **the gap was never the rendering model, it was the chrome and the quality of
the in-app implementations.** P74.2 is a one-sitting chrome removal, P74.18 fixes the selection
highlight, and `rawScrollback` stays as an opt-in for anyone who wants true terminal scrollback and
will trade re-wrap for it. Anyone reopening this should start from the two-mode fact, not from the
public build's behaviour.

**The follow-on question was settled the same day: selection stays app-owned, and the clipboard gets
fixed instead.** Releasing mouse capture (**P74.19**) would hand selection to the terminal, which is the
only thing that works over SSH today — but in alt-screen a released wheel event goes to the emulator,
so it buys terminal-side copy at the cost of wheel scroll, and both halves were named as important. The
actual defect is narrower: `copyToClipboard` shells out to `pbcopy`/`xclip`/`wl-copy` with **no OSC 52
path**, so a remote session copies to the wrong machine and says it succeeded. **P74.20** fixes that
directly and keeps wheel scroll, click-to-focus and the P74.18 highlight. P74.19 survives as an
off-by-default escape hatch for `tmux`/`kitty` copy-mode workflows. **Generalize: when a preference and
a defect point at the same symptom, fix the defect before trading away a capability for the
preference.**

**Two method notes, both earned the hard way here.**

- **A mode whose tests only assert on the frame the model produces has not been shown to work.**
  `rawScrollback`'s P22.6 tests check `plainView(m)` — the string Aegis emits — and never what a
  terminal does with a 3,000-line frame in a 40-row window. That is what let the mode read as finished
  and drove the first wrong sizing. Applies to any future rendering-mode work in `internal/tui`.
- **When reading an external implementation for mechanisms, establish which build you are reading
  first.** The whole P74 TUI lane was filed against the public Claude Code behaviour while the
  interesting mode was behind an env check in a file nobody had opened. Two of the batch's items had
  their direction inverted by that one fact.

**Read the P67.7 record before touching `internal/engine`.** That item asked for tool calls to be
dispatched as their blocks complete in the stream, and named four constraints. Building it found two
more: the P53.2 loop guard can _abort_ a run on the complete round's signature, and the pre-tool-round
budget gate exists specifically so a turn whose own usage crosses the cap stops before its tool calls
run — and neither can rule on a prefix of a round. The resolution is a restriction on _when_ early
dispatch is active, not a weakening of either gate. Anyone widening it is reopening that decision.

**Read P66.13's record before adding a permission layer or a run bound anywhere**: both now live in
`internal/enginecfg` and are built once rather than per entry point. Its own correction outlives it —
the item named four instances of one root cause and there were six, so **counting the instances of a
bypass by reading the file where it was found undercounts it.** `TestEveryEngineCallSiteDecidesItsGate`
enumerates them instead. P73.2, three days later, was the same failure mode in the same package:
`BuiltinOptions` never wired `cfg.Search`, so every non-daemon entry point ignored a configured search
provider.

**Two unwired-seam corrections that are still true and still unfiled as work:**

- **P67.5's recall path has no production callers at all.** `LoadRelevant`/`FormatEntries` are
  unwired — memory reaches the prompt through `Sources.Load()`, which injects both files whole and
  unfiltered. The dedupe, freshness and gotcha bias are built and tested; **wiring a caller is
  separate work nobody has filed**, and should be, before the next item that assumes scored recall is
  live.
- **P67.2's memoization is safe on only four of ten prompt sections.** Five read state Aegis mutates
  mid-conversation (skills, memory, context files, repo map, deferred tools). The volatile set is now
  the exhaustive, justified list of what breaks prefill reuse each turn.

**Method notes worth re-reading before filing or building anything new** (full detail in releases.md's
pass history): before measuring an optimization, check the instrument the rest of the system is
running on — this document has three times recorded a fixed instrument _inverting_ an already-acted-on
verdict. When a harness "just doesn't work", run it once with the tool calls printed before forming a
theory: the P71 sitting cost eleven minutes and invalidated half its own theory, and the two
hypotheses that survived were both arithmetic facts visible in the source that nobody had checked
because the interesting-looking ones were elsewhere. Every documented live-tier command needs
`-count=1`, or a re-run silently replays Go's cached verdict instead of reproducing. Mutation-test any
new numeric threshold. And **read the refutation records in releases.md before filing anything**
against `internal/provider`, `internal/ollamainfo`, `internal/repomap`, or scanner method resolution —
several obvious-looking gaps there have already been checked and answered.

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

**Updated 2026-08-31 (third entry the same day)**: **P76.2, P76.3 and P80.1 — the three items that
had sat at the top of this table since 23 and 30 August — are worked and off it.** Two of the three
turned out to be part-built already, which is the same pattern the P81 first wave found hours earlier
and is now worth stating as a rule: *read the tree before estimating the work.* **P76.2**'s three quit
paths already cancelled the terminal run; what was missing was the test that keeps them doing it.
**P76.3**'s disclosure half was already shipped inside `Report.Format`; the trust gate on top of it is
this sitting's code, and the two halves the entry called "not necessarily exclusive" are both in.
**P80.1** shipped its _interim_ on both the MCP and ACP surfaces — `default_mode` is now a ceiling on a
borrowed session, so F1's clamp is no longer bypassable by reuse — while the session-origin schema
decision it was really filed for stays open and stays Tier 3. That moves **P81.14** to the top.

**Updated 2026-08-31 (second entry the same day)**: the P81 batch is filed, the first wave is being
worked, and one item is already closed by decision rather than by code — **P81.11**, where the
operator confirmed the `ci.yml`/`release.yml` trigger disablement is **deliberate and permanent**. It
is re-tiered to 4 as an accepted risk. That answer propagates: **P81.17**'s drift check is no longer
"subsumed by P81.11" and is now a standalone item that matters _more_, **P81.12**'s release-signing
half is moot until releases resume while its action-pinning half still applies to the one workflow
that does run, and **P81.20**'s fuzz-in-CI step has no home to be restored to. Each entry is updated.

The ranking below now puts **P81.6** at the top as the only Tier 1 item — real, currently reachable,
small, no dependency. **P81.14** (the default audit sink and the message-origin
stamp) is ranked above its own severity: six other items in the batch, plus **P80.1**, want its origin
stamp or its sink, so building it early is what stops six half-solutions. **P81.1** is the batch's one
`Critical` and is _not_ at the top, deliberately — it is a `High`-effort redesign that wants
**P81.8**'s ledger underneath it, and taking it first would mean designing containment with no
instrument to measure it. The remaining Tier 2 items are individually small and mostly independent;
take them opportunistically when their file is already open, which is noted in each entry.

**Updated 2026-08-30**: the comprehensive audit in `Review.md` is finished and the document is gone —
its record is in [releases.md](releases.md#comprehensive-architecture-and-security-audit-remediated-in-full-2026-08-30).
Four items came out of it and are ranked below or filed in their tiers: **P80.1** (Tier 3, the one
`internal/mcpserver` finding needing a product decision), **P80.2** and **P80.3** (Tier 4, the packages
the audit never read and the `Server` struct half of its split), and **P80.4** (Verification, the two
`live_workflow` tests that need a model this machine does not have). **P79.1** remains the highest-tier
open item and is _not_ in this table for the same reason it was not before — it was filed as
"reproduces and isn't mine", and confirming real-path exploitability is its own first step.

**Updated 2026-08-23**: **P76.1**'s both sessions have now run and closed, read-only, no code changes.
Session B (`internal/tui`) found one survivor, **P76.2** (Tier 2 — S). Session A (`internal/security`)
found one survivor, **P76.3** (Tier 3 — needs a trust-gate-or-disclosure design decision before code).
**P76.1** itself is done and demoted off this table — its remaining value is as a pointer to the
closure record, not open work. Document order in the Tier sections below is the same order as this
table, deliberately, so `scripts/roadmap-status.sh` and this ranking agree.

**The whole P74 batch — twenty items, P74.1 through P74.20 — has shipped**, P74.17 last, deliberately.
See [releases.md](releases.md) for every record.

| #   | Item                                                                                              | Tier / size          | Why now                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                      |
| --- | ------------------------------------------------------------------------------------------------- | -------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1   | **P81.14** — no default audit trail for anything privileged                                       | Tier 3 — M           | Filed 2026-08-31, threat model FIND-14. Ranked above its own severity: **P81.3**, **P81.5**, **P81.8**, **P81.23**, **P81.29** and **P80.1** all want its message-origin stamp or its sink. Build it before the six things that depend on it.                                                                                                                                                                                                                                                                                                                |
| 2   | **P81.8** then **P81.1** — the egress ledger, then containment for untrusted content              | Tier 3 — M then High | Filed 2026-08-31, threat model FIND-08 and FIND-01 (the report's one `Critical`). In this order deliberately: the ledger is useful standalone and is the instrument the taint work is measured with. Together they close the exfiltration path the report describes end to end.                                                                                                                                                                                                                                                                              |
| 3   | **The live-tier remainder** (P66.22, P38.1, P62.9, P65.2, P80.4) — _parked by choice, 2026-08-16_ | Verification         | Unchanged and still last for the same reason: **the user parked it**, not a dependency. **P38.1** needs permission to launch an unattended auto-approving agent, **P62.9** needs a _better task_ rather than more runs of the current one, and **P65.2**, **LLM-03**, **LLM-10** and **ARCH-04** now have what they needed — a surviving data dir and `aegis sessions trace <id>`, shipped as **P68.1** (2026-08-22) — so whenever this row is next picked up, the next sitting can actually judge them instead of reproducing the same unreadable evidence. |

**One item is deliberately off this list, Tier 4 with no fired trigger.** **P74.21** (filed
2026-08-21) is the half of P74.17's own roadmap entry that did not ship with it — see
[P74.17's Tier 3 record](#p7417--the-entire-local-model-story-is-one-boolean) for what shipped and what
didn't. It sits in Tier 4 rather than here because, exactly like P74.17 before it shipped, it has no
concrete cargo yet: nothing in the tree today needs a per-model prompt suffix or tool-description
override, only the flag-shaped repair behaviors P74.17 already covers. Promote once something concrete
asks for it. **P77.1** was this item's neighbor here until 2026-08-25, when the user gave it exactly the
concrete cargo it was waiting on — see [its shipped record](releases.md#p771-shipped-2026-08-25).

**Sizes are estimates from reading, not from building, and the batch had a known bias.** The P71
record is the caution: several of its rows were smaller than filed and one was larger. P74.17 itself
confirmed it again: the reading estimated it at Tier 3/L, and what shipped was a leaner, correctly-scoped
provider-decorator mechanism rather than the full tool-registration generalization the reading sketched
— the build found the real shape, same as the note here warned it would.

**This table outranks `scripts/roadmap-status.sh`.** That script reports open items in _document_
order. It still cannot see the cross-tier ranking. Use it for repo state and for the parse; use this
table for what to take.

---

## Threat model 2026-08-31 — the P81 batch

**Source:** a full STRIDE-A threat model of the working tree at commit `88cea69`, run 2026-08-31,
output in [threat-model-20260831-002123/](../threat-model-20260831-002123/0-assessment.md) —
[assessment](../threat-model-20260831-002123/0-assessment.md),
[architecture](../threat-model-20260831-002123/0.1-architecture.md),
[DFD](../threat-model-20260831-002123/1-threatmodel.md),
[STRIDE analysis](../threat-model-20260831-002123/2-stride-analysis.md),
[findings](../threat-model-20260831-002123/3-findings.md). 22 elements, 4 trust boundaries, 138
threats, 33 findings, rating **Elevated**, classification `LOCALHOST_SERVICE` — which is why **there
are no Tier 1 findings in the report**: the daemon binds `127.0.0.1:4127` and refuses anything else
without an explicit flag, so nothing here is reachable by an unauthenticated remote attacker. Report
tiers are _exploitability_ tiers and do not map to this document's _work_ tiers; the mapping below is
this repo's own judgement, not the report's.

**What the report found is a shape, not a scatter.** Its sentence, kept verbatim because it is the
most useful thing in the document: _"The controls that constrain the machine are strong; the controls
that constrain the model are advisory."_ Loopback enforcement, TLS-by-default with a pinned
certificate, the constant-time compare behind an exponential lockout that deliberately checks the
token _before_ the lockout window, the single-use `/ui` page token with its CSRF nonce, one shared
SSRF blocklist re-validated on every redirect, `--cap-drop=ALL` plus `no-new-privileges` plus
resource limits, and fingerprint-pinned trust grants all held up. The advisory half is what this batch
is mostly about.

**Two findings got no number of their own.** FIND-21 (an MCP client enumerating and posting into
sessions it did not create) **is** [**P80.1**](#p801--an-mcp-client-can-list-and-post-turns-into-sessions-it-did-not-create),
which the report cites back and adds an interim mitigation to — read it there. FIND-20 is the
structural half of [**P79.1**](#p791--windows-read-only-shell-classifier-no-longer-detects-absolute-path-escapes-regression-pr-51)
and is filed separately as **P81.20**, because P79.1 is "did this specific Windows regression
reproduce" and P81.20 is "the arrangement that has now produced three of these."

**Numbering is deliberate: P81._N_ is FIND-_N_.** `P81.21` does not exist, and that gap is the record
of FIND-21 belonging to P80.1.

| Finding | CVSS / SDL      | Item          | Work tier | One line                                                                                                         |
| ------- | --------------- | ------------- | --------- | ---------------------------------------------------------------------------------------------------------------- |
| FIND-01 | 8.7 `Critical`  | **P81.1**     | Tier 3    | Untrusted content is marked, not contained                                                                       |
| FIND-02 | 8.2 `Important` | **P81.2**     | Tier 3    | External MCP servers are trusted on configuration alone                                                          |
| FIND-03 | 8.1 `Important` | **P81.3**     | Tier 3    | Config PATCH endpoints can disable isolation with only the bearer token                                          |
| FIND-04 | 7.6 `Important` | **P81.4**     | Tier 3    | The web UI hands the real daemon token to browser JavaScript                                                     |
| FIND-05 | 7.5 `Important` | **P81.5**     | Tier 3    | Outbound provider payloads and tool arguments are never redacted                                                 |
| FIND-06 | 7.3 `Important` | **P81.6**     | —         | `provider.base_url` redirects credentials on a warning only — **REFUTED 2026-08-31**, the control already exists |
| FIND-07 | 7.1 `Important` | **P81.7**     | Tier 4    | The local model endpoint is unauthenticated plaintext HTTP                                                       |
| FIND-08 | 6.9 `Important` | **P81.8**     | Tier 3    | `web_fetch` is an unreviewed egress channel                                                                      |
| FIND-09 | 6.8 `Important` | **P81.9**     | Tier 2    | The session workdir allowlist is skipped on the default bind                                                     |
| FIND-10 | 6.5 `Important` | **P81.10**    | Tier 3    | The container workspace mount is broader than any command needs                                                  |
| FIND-11 | 6.3 `Important` | **P81.11**    | Tier 4    | The merge gate exists, passes, and does not run — **accepted risk 2026-08-31**                                   |
| FIND-12 | 6.1 `Moderate`  | **P81.12**    | Tier 3    | Release artifacts ship without checksums, signatures or provenance                                               |
| FIND-13 | 5.9 `Moderate`  | **P81.13**    | Tier 2    | Sandbox and scanner images are mutable tags                                                                      |
| FIND-14 | 5.7 `Moderate`  | **P81.14**    | Tier 3    | No default audit trail for anything privileged                                                                   |
| FIND-15 | 5.3 `Moderate`  | **P81.15**    | Tier 2    | Every cost and token budget defaults to unlimited                                                                |
| FIND-16 | 5.3 `Moderate`  | **P81.16**    | Tier 2    | The unauthenticated `/ui` mint can be flooded                                                                    |
| FIND-17 | 5.1 `Moderate`  | **P81.17**    | Tier 2    | The committed `dist/` has no drift check that runs                                                               |
| FIND-18 | 4.3 `Low`       | **P81.18**    | Tier 4    | The self-signed certificate warning conditions click-through                                                     |
| FIND-19 | 2.3 `Low`       | **P81.19**    | Tier 2    | `/healthz` is fine today and has no test keeping it that way                                                     |
| FIND-20 | 7.3 `Important` | **P81.20**    | Tier 3    | Plan mode's guarantee rests on a 1,129-line classifier                                                           |
| FIND-21 | 7.1 `Important` | _(**P80.1**)_ | Tier 3    | An MCP client can post turns into sessions it did not create                                                     |
| FIND-22 | 6.9 `Important` | **P81.22**    | Tier 3    | Command isolation degrades to unsandboxed execution on a warning                                                 |
| FIND-23 | 6.5 `Important` | **P81.23**    | Tier 3    | Scheduled jobs run unattended in the mode they were created with                                                 |
| FIND-24 | 6.3 `Important` | **P81.24**    | Tier 2    | History, checkpoints and spill are unprotected on Windows                                                        |
| FIND-25 | 5.9 `Moderate`  | **P81.25**    | Tier 2    | The token never rotates; the pinned certificate regenerates silently                                             |
| FIND-26 | 5.7 `Moderate`  | **P81.26**    | Tier 3    | Sandboxed commands inherit the daemon environment minus a denylist                                               |
| FIND-27 | 5.5 `Moderate`  | **P81.27**    | Tier 3    | Trust grants are unauthenticated local state                                                                     |
| FIND-28 | 5.4 `Moderate`  | **P81.28**    | Tier 4    | Prose tool-call parsing can promote quoted untrusted text                                                        |
| FIND-29 | 5.1 `Moderate`  | **P81.29**    | Tier 2    | `recon_scan` auto-authorizes the operator's entire LAN                                                           |
| FIND-30 | 4.8 `Moderate`  | **P81.30**    | Tier 3    | Parallel rounds do not order shell against concurrent writes                                                     |
| FIND-31 | 4.4 `Low`       | **P81.31**    | Tier 4    | Checkpoint growth is unbounded; `/rewind` discards outside edits                                                 |
| FIND-32 | 3.9 `Low`       | **P81.32**    | Tier 2    | Scan reports land inside the repository they describe                                                            |
| FIND-33 | 3.6 `Low`       | **P81.33**    | Tier 2    | The TUI render path is unbounded; rounds train click-through                                                     |

**Suggested order, which is not severity order and says why.** **Both Tier 1 items are already
closed** and neither took production code: **P81.11** is an accepted risk, **P81.6** was refuted (the
freeze already covers `provider.base_url`). Start at **P81.14**, ranked above its own `Moderate` severity because P81.3, P81.5, P81.8, P81.23, P81.29 and
P80.1 all want its message-origin stamp or its sink, and building it late means six half-solutions.
Then **P81.8** _before_ **P81.1**, because the egress ledger is useful standalone and is the instrument
the containment work is judged with — the reverse order designs containment with nothing to measure it.
Everything else in Tier 2 is opportunistic: take it when its file is already open, which each entry
names.

**Three clusters worth taking as single sittings rather than as separate items.** The
`fsguard.RestrictToOwner` asymmetry appears three times — the session DB and its `-wal`/`-shm`
companions (**P81.24**), `daemon.crt`/`daemon.key` (**P81.25**), and the trust store (**P81.27**) —
each one line, all the same reasoning `daemon.token` already documents, and all of them bite on
Windows specifically, which is this machine. The supply-chain items (**P81.12**, **P81.17**) no
longer cluster with **P81.11** now that it is accepted — and what is left of them is deliberately _not_
workflow edits, which is the point of reading their updated entries rather than the original findings.
And the `internal/security` target-policy work
(**P81.13**, **P81.29**, **P81.32**) sits in the same files as **P76.3**.

**Six items the report flagged as unverified, and they change conclusions if wrong.** The report's own
"Needs Verification" table is reproduced in
[0-assessment.md](../threat-model-20260831-002123/0-assessment.md#needs-verification) and the ones with
teeth are: whether any audit sink is wired by default (**P81.14**'s whole premise), whether
`redact.Text` is applied anywhere on the outbound provider path (**P81.5**'s, and explicitly "absence
of evidence, not proof of absence"), what the SQLite `-wal`/`-shm` ACLs actually are on Windows
(**P81.24**), and whether disabling `ci.yml` was deliberate and permanent (**P81.11** — **answered
2026-08-31: it was**, so that item closed as an accepted risk rather than a fix, exactly as this row
anticipated). Check the premise before building the fix in each of the remaining cases.

**Two threats are `Platform`, not `Open`, and neither is Aegis's to fix**: Docker's
group-membership-equals-host-root model (T21.E1) and the absence of signatures on Ollama model files
(T18.E). Both are worth an operator-facing note in `docs/installation.md` and neither gets a roadmap
item.

**The report is also a reader of this document.** It treats `research/roadmap.md` and
[releases.md](releases.md) as primary sources, cites specific entries back by line number, and confirms
two open items independently (P80.1, and P79.1's structure as P81.20). That is a useful property to
preserve: the entries above are written to be citable the same way.

---

## Open Work — Tier 1

**Status: 0 open.** The tier was briefly occupied on 2026-08-31 by the two Tier 1 items of the P81
threat-model batch, and **both closed the same day without a line of production code** — which is a
result worth stating rather than burying, because it is the second time this document has recorded an
audit finding that did not survive contact with the tree.

- **P81.11** (the merge gate runs only on manual dispatch) — **accepted risk**. The disablement is
  deliberate; re-tiered to 4, where its one residual scheduling question lives.
- **P81.6** (`provider.base_url` frozen only by a warning) — **refuted**. The control already exists
  and already works; see the record below.

An item enters this tier when it is a real, currently-exploitable security or robustness gap that is
small and has no dependency. Nothing is currently open here.

### P81.6 — `provider.base_url` is already security-relevant — REFUTED (FIND-06)

**Filed 2026-08-31 from the threat model; refuted the same day, no code change.** The finding
([**FIND-06**](../threat-model-20260831-002123/3-findings.md#find-06-providerbase_url-override-redirects-credentials-and-model-control-on-a-warning-only),
CVSS 7.3, `Important`) said an HTTPS `provider.base_url` pointing at a non-default host proceeds on a
startup `WARN`, and prescribed moving the key into the security-relevant set so a project config
changing it re-triggers the workspace-trust prompt. **That is already the behaviour.**

`internal/config/freeze.go:120` classifies the whole `provider` block as `frozenUntilTrusted`, and its
doc comment states this item's own threat model in advance: _"provider carries base_url, headers and
the fallback chain. Every prompt, file read and tool result travels to that endpoint, so a project
pointing it at a host it controls is notify.webhook's exfiltration channel at conversation volume."_
The `projectSettable` entries beneath it are a **narrowing allowlist** of 18 named sub-keys
(`provider.model`, `max_tokens`, `temperature`, …) that exists so the ordinary "this repo wants
qwen3:14b" case needs no trust prompt. `base_url` and `headers` are not on that list, `policyFor`
defaults an unlisted sub-key to frozen, and `securityRelevantConfigLines` therefore already hashes
them. A file below even names three keys _deliberately_ left off the settable list for the same
reason.

**Why both the report and this entry got it wrong, which is the part worth keeping.** FIND-06's
evidence cites `internal/config/fingerprint.go:99-117` and hedges correctly — _"whether
`provider.base_url` is frozen depends on that policy table."_ It never resolved the hedge. The policy
table is not in `fingerprint.go`; it is in `freeze.go`. This roadmap entry then dropped the hedge and
restated the unverified half as fact, complete with a prescription ("move it out of `projectSettable`")
for a move with nothing to move. **The report flagged its own uncertainty and the intake lost it** —
that is the transcription failure to watch for the next time a batch is filed from an external
analysis, and it is why the other five Needs-Verification rows are called out in the batch index.

**What was built instead, and it is worth having.** The property was _emergent_: `base_url` is frozen
because nobody added it to the settable list, not because anything asserted it must stay off.
`TestFingerprintCoversProviderBaseURL` (`internal/config/fingerprint_test.go`) now pins it — a project
config introducing `base_url`, changing it, or introducing `headers` each moves the fingerprint to
`Stale`, while a `provider.model`-only edit still does not. It passed against the **unmodified** policy
table on first run, which is the empirical proof the finding was already closed.

**Residue, unaddressed and smaller than the original finding.** `validateBaseURL`'s startup `WARN` for
a non-default HTTPS host is still only a warning _within a trusted workspace_ — trust is what the
freeze gates, so an operator who has trusted the workspace gets no second acknowledgement for a
specific host. FIND-06's remediation step 2 (a one-time interactive acknowledgement per non-default
host, recorded alongside the trust grant) is the only part of this finding that survives, and it is
defence-in-depth on an already-trusted directory rather than a gap. Not filed as its own item; noted
here in case the question returns. **P74.1** (a path-scoped deny rule can never match `grep`) shipped 2026-08-20, the
same day it was filed — record in [releases.md](releases.md). Before it, the tier was empty for one
day; before that it was last occupied by **P71.1** and **P71.10**, both shipped 2026-08-19 the day they
were filed, and before them **P69.6** (2026-08-17) and **P66.5** (2026-08-16), which closed the last of
the P66 review's exploitable-on-the-day findings. Records for all of them are in
[releases.md](releases.md), and several correct the item they were built from — which is the part worth
reading before trusting [CodeReview.md](CodeReview.md) directly.

An item enters this tier when it is a real, currently-exploitable security or robustness gap that is
small and has no dependency.

---

## Open Work — Tier 2

**Status: 0 open.** **P68.1** (the live tier can now run a measurement it can read back) shipped
2026-08-22 — record in [releases.md](releases.md). Before it, this tier's most recent shipment was
**P74.15** (HTML
comments stripped from injected memory files) on 2026-08-21, right after **P74.14** (a malformed
dangling call gets its own message instead of the interrupted wording), right after **P74.13** (the
stable per-agent colour), right after **P74.12** (the eased token counter),
right after **P74.11** (the stall shimmer ramp), which shipped right after **P74.10** (the reduced-motion
setting), which shipped right after **P74.9**'s first
half (empty-result normalization, `builtin.NormalizeEmptyResult`), which shipped right after **P74.8**
(prose-tool-call salvage, head of the harness lane) on 2026-08-20, all right after **P74.7** (the real
terminal cursor, closing out the menu lane), which shipped right after **P74.5** (the pickers' selection
chrome, down to one cue) and **P74.6** (the filter affordance and match count), all in the same sitting
as directed, **P74.19** (the mouse-capture escape hatch), **P74.20** (the OSC 52 clipboard fix) and
**P74.18** (the selection-highlight bug) also 2026-08-20, **P71.2**, **P71.3**, **P71.4**, **P71.5**,
**P71.9**, **P72.2**, **P72.3** and **P73.2** on 2026-08-19, and **P66.25**/**P67.2**–**P67.5** before
them. Records in [releases.md](releases.md).

**Two sub-lanes from the 2026-08-19/20 batch are the rest of this tier's recent history, and they did
not block each other.** The selection/clipboard group and the
whole menu lane are both fully shipped (**P74.19**, **P74.20**, **P74.18** — see
[releases.md](releases.md#mouse-capture-becomes-a-config-choice-not-a-package-deal-2026-08-20-p7419);
**P74.5**, **P74.6**, **P74.7** — see
[releases.md](releases.md#pickers-drop-to-one-selection-cue-and-a-filter-hint-2026-08-20-p745-p746)
and [releases.md](releases.md#the-real-terminal-cursor-lands-on-the-focused-row-2026-08-20-p747)).
Local-model tool-call repair is now most of the way shipped: **P74.8** (see
[releases.md](releases.md#a-tool-call-written-as-text-becomes-a-call-2026-08-20-p748)), **P74.9**'s
first half (see
[releases.md](releases.md#an-empty-tool-result-becomes-a-named-placeholder-2026-08-20-p749)), and
**P74.14** (see
[releases.md](releases.md#a-dangling-call-whose-arguments-never-parsed-gets-its-own-message-2026-08-20-p7414))
have all shipped; the argument-shape repair P74.9 deferred is now P74.17's to carry, not a row of its
own. Motion and status is now fully shipped in `internal/tui`, now that **P74.10** (the reduced-motion
flag), **P74.11** (the stall shimmer ramp), **P74.12** (the eased token counter) and **P74.13** (the
stable per-agent colour) have all shipped (see
[releases.md](releases.md#there-is-no-reduced-motion-setting-fixed-2026-08-20-p7410),
[releases.md](releases.md#stall-becomes-a-visible-ramp-not-just-an-abort-2026-08-20-p7411),
[releases.md](releases.md#the-token-counter-jumps-instead-of-climbing-2026-08-20-p7412) and
[releases.md](releases.md#a-running-swarm-gets-a-stable-colour-not-three-grey-lines-2026-08-20-p7413)).
**P74.15** (stripping HTML comments from injected memory files) shipped 2026-08-21 — see
[releases.md](releases.md#injected-memory-files-stop-paying-for-their-own-authoring-notes-2026-08-21-p7415).
**P68.1** (the live tier can now run a measurement it can read back) shipped 2026-08-22, closing this
tier out — record in [releases.md](releases.md).

**P76.2** (filed 2026-08-23, first survivor of **P76.1** Session B) is now open in this tier — see
below.

**Eleven arrived 2026-08-31 with the P81 threat-model batch, and six closed the same day.** Shipped:
**P81.15** (cloud spend ceilings, plus the `projectMayTighten` policy that makes them a bound rather
than a suggestion), **P81.16** (per-address `/ui` mint limit), **P81.19** (`/healthz`, which turned out
to be disclosing more than the report thought — see its entry), **P81.29** (private-range recon targets
need an allowlist entry) and **P81.32** (scan reports default out of the workspace). **P81.24**,
**P81.25** and **P81.27** shipped in part and remain open for their remainders. Still untouched:
**P81.9**, **P81.13**, **P81.17** and **P81.33**.
They are individually small and mostly independent, and several are explicitly worth taking while
their file is already open for something else — **P81.33** with **P76.2**/**P80.2** in `internal/tui`,
**P81.32** with **P76.3** and **P81.13** in `internal/security`, and the three
`fsguard.RestrictToOwner` fixes (**P81.24**, **P81.25**, **P81.27**) as one sitting. **Status is
therefore 13 open**, not 2.

### P76.2 — Quit doesn't cancel a running interactive-terminal command

**Filed 2026-08-23, out of P76.1 Session B** (the read-only `internal/tui` audit). `Run()`'s doc
comment at `internal/tui/tui.go:90-98` claims every quit path cancels the in-flight request's context.
That's true for `m.cancel` (the model-turn context) but not for `m.termRun.cancel` — the context behind
a command running in the interactive terminal pane (`ctrl+t`, `internal/tui/terminal.go:38-82`). None
of the three quit paths (`update_key.go:158-170`, `update_overlay.go:152-157`,
`update_slash.go:25-28`) touch it.

**Effect:** if a shell command is running in the terminal pane when the user quits — ctrl+c-to-quit,
confirming quit with "y", or `/quit`/`/exit` — the `execTermCmd` goroutine and the child process behind
it (via `sandbox.NewLocalBackend().ExecStreaming`) are never cancelled. They're orphaned past `p.Run()`
returning: a resource leak on exit, not a data-loss or security issue, and only reachable while
`termOpen` with a command actually running.

**Fix is mechanical, not a design question** — add `if m.termRun != nil { m.termRun.cancel() }` (or
equivalent) to each of the three quit paths, matching what `m.cancel` already does there, and update
the `Run()` doc comment's claim to match once it's actually true again.

**SHIPPED 2026-08-31 — and the code half was already in the tree.** All three quit paths
(`update_key.go`'s `ctrl+c`, `update_overlay.go`'s quit confirmation, `update_slash.go`'s
`/quit`/`/exit`) already cancelled `m.termRun`, and `Run()`'s doc comment already claimed it; the fix
went in incidentally during a later `internal/tui` sitting and this entry was never closed. What was
missing is the part that keeps it true: **nothing pinned it**, and a fourth quit path added later
would have re-opened the leak silently against a doc comment still promising otherwise.
`TestQuitPathsCancelTheTerminalRun` (`internal/tui/quit_termrun_test.go`) now drives each of the three
paths with a live `termRun` and asserts its context is cancelled. Mutation-checked: deleting the
cancel from `update_slash.go` fails the `/quit` case. No production code changed.

Priority: Tier 2 — S, no dependency, self-contained. **Closed.**

### P79.1 — Windows read-only-shell classifier no longer detects absolute-path escapes (regression, PR #51)

**Filed 2026-08-30, surfaced incidentally** while committing an unrelated gap-fix batch
(`research/gaps.md`'s security/quality review) — `go test ./...` on a freshly-pulled `main` showed
four failing tests in `internal/tool/builtin`:
`TestReadOnlyGitArgvAgreesAcrossBothPaths/pathspec_escape`,
`TestReadOnlyShellAttachedValueConfinement` (ten subtests — `grep --file=`, `rg --ignore-file=`,
`fd --base-directory=`, `file --magic-file=`, attached-short forms, `tree`/`uniq` operands, …),
`TestReadOnlyShellPowerShellPathConfinement` (`Get-Content -Path`/`-Path:`), and
`TestReadOnlyShellCommandWindowsPaths` (`Get-Content`, `Get-ChildItem` with a bare absolute operand).
All assert `readOnlyShellCommand`/`classifyShellCommand` (`internal/tool/builtin/shell_readonly.go`)
reject a command whose operand or attached-flag value is an absolute Windows path outside the
confinement root; all instead accept it.

**Isolated to before this session's own changes**: stashed everything (tracked and untracked),
fast-forward-pulled to `main`'s tip at the time (`519efbd`, "enginecfg bypass fix, round-deadlock fix,
and security hardening sweep (#51)" — the PR that added 85 lines to
`internal/sandbox/pathvalidator.go` and touched `shell_readonly.go` itself), and ran the same four
tests against that commit alone with nothing else applied. They failed identically, confirming the
regression predates and is unrelated to the gap-fix commit — not root-caused further; the likely seam
is `internal/tool/builtin/argv_confine.go`'s `firstArgvEscape` → `sandbox.ValidatePath`, since that's
the one confinement primitive both the git-argv and shell-argv paths share and PR #51 touched
`pathvalidator.go` directly, but that's a hypothesis to verify, not a finding.

**Why this matters more than a typical failing test**: `shellTool.CapabilityFor`
(`internal/tool/builtin/shell.go:54-68`) uses this same classifier to downgrade a shell call from
`tool.CapExecute` to `tool.CapRead` — a `CapRead` classification is _silently allowed_ under the
plan-mode read gate instead of requiring execute approval (`internal/permission`). If the classifier
now accepts a command whose real target resolves outside the workspace root as if it were a safe
read-only-within-root command, a plan-mode session on Windows could have a shell read arbitrary
host files (an SSH key, `/etc/hosts`-equivalent) without an approval prompt. Whether that path is
_actually_ reachable in production (vs. only in the two test files calling the classifier directly)
has not been checked — the four failing tests exercise `readOnlyShellCommand`/`classifyShellCommand`
directly, not `shellTool.CapabilityFor` end-to-end, so confirming exploitability through the real
tool call is the first thing a build session on this item should do.

Priority: Tier 2 — S, no dependency, self-contained, but confirm real-path exploitability (not just
the unit-level classifier) before treating severity as settled — it may belong in Tier 1 once that's
known. The threat model's **FIND-20** (filed here as **P81.20**) is the structural half of this item
and verified on 2026-08-31 that the four named tests now pass in the working tree — which addresses
the specific regression and leaves this item's real-path reachability question exactly where it was.

### P81.9 — The session workdir allowlist is skipped on exactly the bind it ships with (FIND-09)

**Filed 2026-08-31**, from the threat model
([**FIND-09**](../threat-model-20260831-002123/3-findings.md#find-09-session-working-directory-allowlist-is-not-enforced-on-the-default-bind),
CVSS 6.8, `Important`, CWE-22). `server.session_workdir_allowlist` bounds which directories a client
may root a session at, and `internal/config/config_server.go`'s doc comment states plainly that it is
"Ignored — every existing-directory request is accepted — on the default loopback-only bind, where a
client is already as trusted as a local shell user."

That reasoning holds for the operator's own shell and for nothing else on this daemon. An MCP client
(**P80.1**), an editor plugin over ACP, and a scheduled job are all "local" in the sense the exemption
uses, and none of them is the operator choosing a directory. Any token holder can root a session
anywhere the daemon process can read and turn the file tools into a filesystem oracle over the whole
home directory — the exact outcome the doc comment says the key exists to prevent. The adjacent
mechanism, `workspace.additional_roots`, does require a trust grant per root; this path does not.

**What to do.** Apply the allowlist unconditionally, defaulting it to the daemon's own workspace plus
anything nested under it, and route a legitimate request for another directory through the same
`config.WorkspaceTrusted`/`TrustWorkspace` prompt `additional_roots` already uses. If the interactive
TUI path is meant to keep the exemption, key it on request origin rather than on bind address — which
is the same origin-stamping **P81.14** needs anyway.

Priority: Tier 2 — S. The allowlist logic is already written; this is removing an exemption and
picking what the default allowlist contains. Sequenced loosely behind **P81.14**'s origin stamp if the
TUI carve-out is wanted.

### P81.13 — Sandbox and scanner images are mutable tags, and `verify-image` is a separate command (FIND-13)

**Filed 2026-08-31**, from the threat model
([**FIND-13**](../threat-model-20260831-002123/3-findings.md#find-13-container-and-scanner-images-are-referenced-by-mutable-tag),
CVSS 5.9, `Moderate`, CWE-494). `sandbox.image` defaults to `ubuntu:22.04` — a tag whose backing image
changes over time and can be replaced locally with no signal — and the scanner images `MultiScanner`
runs are the same shape. `aegis security verify-image` exists and works, but it is an operator command
rather than a precondition of a scan, so a scan normally runs against an unverified image.

**What to do.** Resolve `sandbox.image` and every scanner image to a digest at first use, pin it in
the data directory, and refuse a changed digest without re-confirmation. Make `verify-image` a
precondition of a scan run with an explicit override flag for local development. Record the resolved
digest in the scan report and the session record, so a finding traces to the exact scanner build that
produced it — which also gives **P76.3**'s baseline-tampering question a second anchor.

Priority: Tier 2 — S. Self-contained, no design decision, and the verification command it needs
already exists.

### P81.15 — Every cost and token budget defaults to unlimited (FIND-15)

**Filed 2026-08-31**, from the threat model
([**FIND-15**](../threat-model-20260831-002123/3-findings.md#find-15-all-cost-and-token-budgets-default-to-unlimited),
CVSS 5.3, `Moderate`, CWE-770). `cost.budget_usd`, `cost.max_tokens_per_run`, `cost.session_cap_usd`,
`cost.daily_cap_usd`, `cost.session_token_cap` and `cost.daily_token_cap` all default to `0`, which
means unlimited (`internal/config/config.go:175-186`). The single bound that _is_ on by default,
`cost.max_turn_stall`, catches silence — a model that keeps producing tokens never trips it.
`internal/cost` implements the accounting correctly. Nothing is switched on.

On a loopback provider this is free and correct. Against a metered cloud endpoint it means a looping
model, or one steered by an injected instruction (**P81.1**), runs with no ceiling until the operator
notices the bill.

**What to do.** Ship a non-zero `cost.daily_cap_usd`/`cost.session_cap_usd` default that applies only
when the resolved provider is a metered cloud endpoint — `config.IsLoopbackBaseURL` already draws that
line for `validateBaseURL`, so reuse it rather than inventing a second test. Refuse to start a
cloud-provider session with every spend bound at zero unless an explicit acknowledgement key is set,
and surface accumulated session/daily spend in the TUI status line so `cost.alert_threshold: 0.8` has
something to be a threshold of. A crossed cap should take the documented resettable-abort path, not
the fatal one.

Priority: Tier 2 — S. Default values plus one provider-class test; the enforcement is built.

### P81.16 — The unauthenticated `/ui` mint can be flooded to deny the operator's own UI (FIND-16)

**Filed 2026-08-31**, from the threat model
([**FIND-16**](../threat-model-20260831-002123/3-findings.md#find-16-the-unauthenticated-ui-mint-can-be-flooded-to-deny-the-operators-own-ui),
CVSS 5.3, `Moderate`, CWE-770). `GET /ui` is exempt from `authMiddleware` because a browser navigation
cannot carry a bearer token, and it mints a page-token entry per load. `mintPageToken` sweeps expired
entries then refuses once `maxPageTokens` (1024) unexpired entries exist. **Refusing rather than
evicting is the right call** — eviction would let a flood invalidate legitimate page tokens — but the
consequence is that a local process issuing more than 1024 `/ui` requests inside the 60-second TTL
locks the operator out of their own UI, presenting as "the UI won't load" with nothing logged.

**What to do.** Add a per-remote-address minting rate limit ahead of the cap, sized well above a
browser's reload rate. Log at `Warn` when the cap or the limit engages, naming the address, so the
condition is diagnosable. Consider skipping the mint entirely when the request already carries a valid
bearer token, which removes the unauthenticated path for API-driven loads.

Priority: Tier 2 — S. One rate limiter in front of an existing cap, in one file.

### P81.17 — The committed `dist/` has no drift check that runs (FIND-17)

**Filed 2026-08-31**, from the threat model
([**FIND-17**](../threat-model-20260831-002123/3-findings.md#find-17-web-ui-assets-are-served-from-a-committed-dist-whose-drift-check-no-longer-runs),
CVSS 5.1, `Moderate`, CWE-353). The web UI is served from `internal/server/webui/dist/`, committed and
`go:embed`-ed — a deliberate choice that keeps `go build` free of Node.js, and one this repo should
keep. The cost is that the committed bundle is a build artifact nothing verifies against its source,
and the one check that did (`ci.yml`'s `npm ci && npm run build && git diff --exit-code -- ../dist`)
runs only on manual dispatch. A modified `dist/` is indistinguishable from a rebuilt one at review
time, and the bundle it produces runs in the operator's browser holding the daemon token (**P81.4**).

**Filed as "largely subsumed by P81.11" and no longer is — updated 2026-08-31.** The original read was
that the drift job lives in the same workflow, so re-enabling the triggers restores it for free. The
operator has since confirmed the disablement is deliberate and permanent (**P81.11**, now an accepted
risk), so **nothing will restore this check**. That inverts the item: the runtime half is no longer
leftovers after a cheaper fix, it is the whole fix, and it is the only thing that will ever verify the
committed bundle against its source.

**What to do.** Record a hash of `dist/` in a committed manifest and log it at daemon start, so a
mismatch between the shipped bundle and the reviewed one is observable at runtime. Consider a
`go generate`-style local check, or a pre-commit hook, since there is no PR gate to hang it on. If the
frontend is rebuilt rarely enough, a documented manual `npm run build && git diff --exit-code`
step in the release checklist may be the honest answer — say so in the entry rather than building
machinery nobody runs.

Priority: Tier 2 — S, **no longer sequenced behind P81.11** and slightly more valuable than filed,
because the cheap alternative that would have covered it is gone by choice.

### P81.19 — `/healthz` is fine today and has no test keeping it that way (FIND-19)

**Filed 2026-08-31**, from the threat model
([**FIND-19**](../threat-model-20260831-002123/3-findings.md#find-19-healthz-discloses-daemon-presence-without-authentication),
CVSS 2.3, `Low`, CWE-200). `/healthz` is exempt from `authMiddleware`, so any process reaching the
loopback port can confirm the daemon is present without a credential. That is a normal health-endpoint
design and the disclosure is negligible — the finding exists for what it would become, not what it is.
Adding a version string, workspace path or session count to that payload turns it into a
reconnaissance primitive, and there is nothing in the tree that would fail if someone did.

**What to do.** Add a test asserting the `/healthz` body carries a bare readiness indicator and no
version, path or count field. Keep richer diagnostics on the authenticated `GET /status`, where they
already live. No behaviour change today.

Priority: Tier 2 — XS. A regression test that pins a property the code already has, which is the whole
reason to write it now rather than after someone widens the payload.

### P81.24 — Conversation history, checkpoints and spill are unprotected on Windows (FIND-24, ACL half)

**Filed 2026-08-31**, from the threat model
([**FIND-24**](../threat-model-20260831-002123/3-findings.md#find-24-conversation-history-and-checkpoints-persist-secrets-unencrypted),
CVSS 6.3, `Important`, CWE-311). Everything the agent reads lands on disk in plaintext and stays
there: the conversation in the session SQLite database, whole file copies in checkpoint snapshots, and
truncated tool-result remainders in `<workspace>/.aegis/spill/`. None of it is encrypted, none has a
retention policy, and the snapshots and spill files outlive the session that made them.

The part of this that is a defect rather than a design posture is the permissions asymmetry.
`daemon.token` gets `0o600` **and** `fsguard.RestrictToOwner`, precisely because the mode bit is
cosmetic on Windows where a new file inherits its parent's ACL. `sqlitestore.Open`
(`internal/sqlitestore/sqlitestore.go:52-61`) creates the parent directory `0o700` and applies no
`fsguard` to the database file, and the `-wal`/`-shm` companions are created by the driver. On a shared
Windows host **the conversation database is less protected than the credential guarding access to
it** — and this machine is a Windows box, so it is the live case, not the hypothetical one.

**Split deliberately.** The ACL and reaping half is this Tier 2 item: apply `fsguard.RestrictToOwner`
to the session DB and its companions, the checkpoint directory and the spill directory; reap spill
files and checkpoints at session end; add a retention policy that prunes archived sessions (shared
with **P81.31**). At-rest encryption keyed to the OS credential store, and redaction on the
persistence path as well as the sharing path, are the `High`-effort half — file them off this item
only if a trigger appears; do not start there.

**How to verify:** on Windows, create a session as one user and confirm a second local account cannot
read the database, `-wal`, checkpoint or spill files. The threat model's own "Needs Verification" table
flags that the driver's file-creation mode was not traced, so measure the current ACL with `icacls`
before writing the fix — the gap may be narrower or wider than read.

Priority: Tier 2 — S for the ACL half, and the platform this is developed on is the platform it bites.

### P81.25 — The daemon token never rotates and the pinned certificate regenerates silently (FIND-25)

**Filed 2026-08-31**, from the threat model
([**FIND-25**](../threat-model-20260831-002123/3-findings.md#find-25-the-daemon-token-and-pinned-certificate-never-rotate-and-are-not-integrity-checked),
CVSS 5.9, `Moderate`, CWE-522). The token itself is well built — 32 random bytes, `0o600` plus a real
owner-only ACL via `fsguard.RestrictToOwner`, constant-time compare, exponential lockout that
deliberately checks the token _before_ the lockout window. What is missing is lifecycle. It never
rotates, so a credential captured once through a backup, a disk snapshot or a screen share stays valid
for as long as the file exists.

The certificate has the mirrored gap. `daemon.crt`/`daemon.key` are generated on first start and
reused, and `client.NewFromConfig` pins whatever is at that path. Deleting them regenerates silently
on the next start, changing the certificate every pinned client trusts with no operator-visible
signal; a same-user process that writes `daemon.crt` before the client first reads it makes the client
trust an impersonating listener. And `daemon.crt` does not get the `fsguard` treatment `daemon.token`
does — the same asymmetry as **P81.24**, in a different file.

**What to do**, cheapest first: apply `fsguard.RestrictToOwner` to `daemon.crt` and `daemon.key`; log
at `Warn` when certificate material is regenerated over a previously-existing pin; record the pinned
fingerprint client-side and require acknowledgement when it changes. Token rotation on each daemon
start (clients re-read the file) is the larger piece and needs a decision about long-lived
integrations that cache it — MCP, ACP and cron all hold one.

Priority: Tier 2 — S for the ACL, warning and fingerprint-acknowledgement pieces. The rotation
decision can be deferred without blocking any of them.

### P81.29 — `recon_scan` auto-authorizes the operator's entire LAN (FIND-29)

**Filed 2026-08-31**, from the threat model
([**FIND-29**](../threat-model-20260831-002123/3-findings.md#find-29-recon_scan-auto-authorizes-every-loopback-and-rfc1918-target),
CVSS 5.1, `Moderate`, CWE-284). `isHostAllowed` (`internal/security/target.go:9-33`) is the shared
network-target policy for the DAST and recon scanners, and its core design is careful: hostnames match
literally and are never DNS-resolved, so a declared target's identity cannot change under the check.
But `if isLoopbackOrPrivateHost(host) { return true, "" }` runs _before_ the allowlist is consulted at
all, so loopback **and every RFC-1918 address** are allowed unconditionally, on the reasoning that
"scan my locally running app/home lab needs no config."

On a developer laptop attached to a corporate network, `10.0.0.0/8` is not the operator's home lab. A
model-issued `recon_scan` can port-scan and template-scan the whole LAN with no configuration and no
per-target consent, and `security.dast.allow_active: false` gates the aggressive checks but not the
passive nmap/nuclei sweep. There is a plausible route to that call through **P81.1**.

**What to do.** Require an explicit allowlist entry for private-range targets, keeping the zero-config
path for `127.0.0.1`/`localhost` only. Gate any recon call whose target set includes an address the
daemon did not originate from behind an operator approval. Record every recon target and verdict in
the audit sink (**P81.14**).

Priority: Tier 2 — S. Moving one early-return below the allowlist check, plus the default that keeps
loopback zero-config.

### P81.32 — Scan reports land inside the repository they describe (FIND-32)

**Filed 2026-08-31**, from the threat model
([**FIND-32**](../threat-model-20260831-002123/3-findings.md#find-32-scan-reports-embed-source-excerpts-into-the-workspace),
CVSS 3.9, `Low`, CWE-532). Scan reports embed source excerpts, dependency inventories and finding
context, and are written into the workspace. `internal/security/redact.go` scrubs findings, which
handles the highest-risk content. What remains is a workflow exposure rather than a code defect: a
report written inside the repository is a report that can be committed and pushed, taking a curated
map of the project's weaknesses with it.

**What to do.** Default the report output path to the data directory, with the workspace as an
explicit opt-in. When the workspace path is chosen, add or update a `.gitignore` entry for the report
directory and say so in the tool result. Keep the redaction pass and add a test asserting a synthetic
credential in scanned source does not reach the emitted report.

Priority: Tier 2 — S. A default path change plus a `.gitignore` write. Worth taking alongside
**P81.13** and **P76.3**, which are in the same files.

### P81.33 — The TUI render path is unbounded, and a parallel round trains click-through (FIND-33)

**Filed 2026-08-31**, from the threat model
([**FIND-33**](../threat-model-20260831-002123/3-findings.md#find-33-the-tui-render-path-is-unbounded-and-repeated-approvals-invite-blanket-approval),
CVSS 3.6, `Low`, CWE-400). Two issues in the operator's primary surface. A pathological model response
— one multi-megabyte line, or dense wide-glyph content — can stall the render loop; the caps in
`internal/tool/builtin/truncate.go` bound what a _tool_ returns, not what the model emits.

The second is the one that matters for posture. A long parallel tool round produces a run of approval
prompts in quick succession, which trains the operator to approve without reading. **Every gate in
this system that asks rather than denies depends on the operator actually reading the prompt** — the
persona tool list, `warnOutboundSecrets`, the trust prompt, the sandbox warning. A UI that emits many
similar prompts in a row is working against all of them at once, which is why a `Low`-CVSS TUI item is
in this tier rather than parked.

**What to do.** Bound per-render line length and total buffered output in the view layer with an
explicit truncation marker. Batch one parallel round's approvals into a single reviewable summary
listing every call rather than prompting serially. Show the resolved argv and the effective sandbox
backend in the prompt (shared with **P81.22**), so what is being approved is legible at a glance.

Priority: Tier 2 — M. The render bound is small; the batched-approval surface is a real UI change and
the reason this is not XS. Take it while `internal/tui` is open for **P76.2** or **P80.2**.

---

## Open Work — Tier 3

**Status: 18 open.** Three pre-existing — **P76.1** (both sessions done, entry kept as a pointer),
**P76.3** (its Session A survivor), and **P80.1** (filed 2026-08-30, the one `internal/mcpserver`
audit finding whose fix is a product decision rather than a defect repair). Fifteen arrived 2026-08-31
with the P81 threat-model batch: **P81.1**, **P81.8**, **P81.5**, **P81.2**, **P81.3**, **P81.4**,
**P81.10**, **P81.12**, **P81.14**, **P81.20**, **P81.22**, **P81.23**, **P81.26**, **P81.27** and
**P81.30**. They are listed in that order below, which is the order to take them in: **P81.14** first
because six of the others want its origin stamp, then **P81.8** before **P81.1** because the ledger is
the instrument the containment work is judged with. **P75.1** (per-block tool-result expand/collapse, filed 2026-08-21 the same day
the styling follow-up below shipped) shipped in full the same day, both slices — record in
[releases.md](releases.md#p751-shipped-in-full-2026-08-21). **P74.17** (per-model harness profiles)
shipped 2026-08-21 too, closing the tier out before this —
record in [releases.md](releases.md#local-model-repair-behaviors-resolve-per-model-instead-of-per-boolean-2026-08-21-p7417).
**P74.2** (the chrome removal — sidebar to an overlay,
auto-hidden scrollbar, title bar folded into the status line) shipped the same day, unblocking P74.3,
which itself shipped the same day and unblocked P74.4, which shipped the same day in turn.
The tier was emptied on 2026-08-18 (**P66.15**, **P67.6**, **P67.7**, **P67.8**, **P67.9**, then
**P70.4**) and the 2026-08-19 sitting kept it that way: **P71.8**, **P73.1** and **P72.1** all shipped
the day each was filed. Records in [releases.md](releases.md).

Two of those records constrain future work here and are summarized under [Decisions that outlive the
items that made them](#decisions-that-outlive-the-items-that-made-them): read **P67.7**'s before
touching `internal/engine`, and **P66.13**'s before adding a permission layer or a run bound anywhere.
Read **P66.14**'s (2026-08-16) before touching the compaction path, because the shared trigger it
introduced changed which numbers two already-shipped heuristics see.

An item enters this tier when it has real value but is larger or sequence-dependent — it blocks, or
is blocked by, other work. **P72.1 is the worked example of the "sequence-dependent" half**: it sat
here rather than being built the day it was filed because it needed a cold-start policy decided, not
a wire, and the resolution was to put four design questions to the user before writing anything.

### P76.1 — Audit the codebase's unread 26%: `internal/tui` and `internal/security`

**Filed 2026-08-21**, from reconciling a generic sprawl/hot-path/security refactor-audit prompt
(`research/CodeRefreactorPrompt.md`) against what already exists. Most of what that prompt asks for
is not open work — it's already-answered work. `CodeReview.md` (2026-08-15) ran a six-specialist
audit against exactly its three axes — sprawl (QUAL-01…15), hot paths (PERF-01…09), security
(SEC-01…14, VULN-01…12, ARCH-01…13) — through adversarial debate and arbitration, a rigor level a
fresh single-pass audit won't match. Most of the high-value findings are already shipped (QUAL-01/02/
03/06, ARCH-05/06, the P66 Tier-1 exploitable set, SEC-01's `.env` gate, VULN-11's flag-parity class),
and the rest is already triaged into Tier 4 with a stated reason (**P66.17**, **P66.23**, **P66.26**,
the QUAL-04/05/07/08/09 grab-bag) or explicitly WONTFIX'd (QUAL-05 — the TUI "god struct" is just the
Bubbletea Elm-architecture model, downgraded to Info absent a concrete bug). Re-running that prompt's
Phase 1 across the whole tree would mostly reproduce `CodeReview.md`.

**What it doesn't reproduce: `CodeReview.md`'s own Section 10.4 names the one gap it left standing.**
`internal/tui` and `internal/security` together are **26% of production Go** and were "still
substantially unread" by the six-specialist pass — the review's stated remaining exposure, not this
item's own guess. `internal/tui` is also the single largest package (16,080 LOC / 56 files) and has
had 20+ items shipped into it in the past week (the whole P74 batch plus P75.1) with no structural
pass behind any of them.

**Plan — deliberately multi-session, one phase per sitting, mirroring why the original review used
six separate specialist tracks instead of one:**

- **Session A — `internal/security` only.** Read-only, no code changes (the original prompt's own
  "AUDIT and STRUCTURAL BLUEPRINT, do not change files" constraint carries over — it's the right
  discipline regardless of which prompt asked for it). Before writing up any candidate finding, check
  it against `CodeReview.md`'s SEC-_/VULN-_/ARCH-\* sections and against `releases.md` — if it's already
  there and shipped, or already parked in Tier 4 with a reason, it is not new work. Output: a short
  findings addendum, each item cited `file:line`, each marked CONFIRMED (traced and, where security-
  relevant, executed — see how VULN-01/02/11 were verified) or SUSPECTED, matching the existing
  review's own discipline rather than inventing a new one.
- **Session B — `internal/tui` only.** Same read-only rule, same cross-check requirement — in
  particular against QUAL-05 (already WONTFIX'd; don't re-litigate it) and against [Decisions that
  outlive the items that made them](#decisions-that-outlive-the-items-that-made-them), which records
  two rendering-mode decisions this package's owner already made deliberately.
- **Session C+ — file survivors as real roadmap entries.** Whatever passes the Session A/B cross-check
  becomes its own `P76.2`, `P76.3`, ... with its own Priority line and tier, sized by the existing
  [Tiering Criteria](#tiering-criteria) — not a blanket "Phase A wave" imposed ahead of knowing what
  the findings are.
- **Validation, every session:** `go build ./...`, `go vet ./...`, `go test -race ./...`, plus
  `staticcheck ./...` and `govulncheck ./...` — both already ran clean as of `CodeReview.md` Section
  10; re-run to catch drift since 2026-08-15, not to re-derive that baseline from scratch.

**Do not** re-audit packages `CodeReview.md` already covered in depth (`internal/engine`,
`internal/provider`, `internal/permission`, `internal/session`, `internal/tool/builtin`) without a
reason narrower than "general audit" — those already carry a debated, arbitrated finding set and a
live shipped-fix record; re-reading them from a generic prompt is exactly the duplicate work this
item exists to avoid.

**Both sessions ran 2026-08-23; the audit is closed.** Session B (`internal/tui`) found one survivor:
a resource leak where none of the three quit paths cancel `m.termRun`, so a command running in the
interactive terminal pane (`ctrl+t`) outlives `p.Run()` returning. Filed as **P76.2**, Tier 2.
Everything else in `internal/tui` checked clean — no other goroutine/channel issues, no contradiction
of the alt-screen/app-owned-frame or app-owned-selection decisions, QUAL-05 correctly left alone.

Session A (`internal/security`) found one survivor: `applyBaseline` (`security.go:397-424`,
`baseline.go`) reads `.aegis/security-baseline.yaml` straight from the _scan target_ directory with no
workspace-trust gate, and the report surfaces suppressions only as a bare count — never which rule/CVE/
location was hidden. A hostile repository — exactly the threat model the `--network none` scanner
design defends against — can ship a baseline that pre-suppresses the finding for its own planted
vulnerability, and nothing distinguishes an operator-authored baseline from a repo-planted one. Needs a
trust-gate-or-disclosure design decision, not a one-line fix. Filed as **P76.3**, Tier 3. Everything
else in `internal/security` checked clean — network isolation is consistently correct and test-pinned
(including the gosec warm/analyze split's fatal-on-warm-failure invariant, actually enforced in code,
not just documented), no injection surface in `install.go`/`recon.go`'s argv construction, `RedactText`'s
fail-open posture confirmed as the deliberate P24.12/FIND-09 design. VULN-06 (already Tier 4/P66.23),
the XML recursion sweep, and nmap/nuclei flag-injection defenses were all correctly excluded as
already-covered.

**P76.1 itself is done** — both sessions complete, survivors filed as their own entries above/below.
This item's remaining value is as a pointer for anyone asking "has `internal/tui`/`internal/security`
had a structural pass" — yes, 2026-08-23, see `releases.md` for the closure record and **P76.2**/
**P76.3** for what it found.

Priority: Tier 3 — L, multi-session by design. No dependency, but Session A and B should not be
collapsed into one sitting — the prompt budget discipline `CLAUDE.md` documents elsewhere is the same
reason the original six-specialist review didn't run as one pass either.

### P76.3 — A hostile repo can plant its own security-scan baseline to hide its own findings

**Filed 2026-08-23, out of P76.1 Session A** (the read-only `internal/security` audit). `applyBaseline`
(`internal/security/security.go:397-424`, `internal/security/baseline.go`) reads
`.aegis/security-baseline.yaml` straight from the scan **target** directory, with no workspace-trust
gate. `Report.Format()` (`security.go:541-542`) then surfaces every suppression as a bare count —
`"Suppressed by baseline: N"` — never the rule ID, CVE, severity, or location of what was hidden.

**Why this matters here specifically:** the whole scanner subsystem is built around one threat model —
`gosec.go`'s own comments call out "an exfiltration path out of a hostile repo," which is why
multiscanner runs `--network none` with the workspace mounted. A baseline file is a gap in that same
model: a hostile or untrusted repository can ship a `.aegis/security-baseline.yaml` that pre-suppresses
the CVE/SAST finding for a vulnerability it planted itself. The operator, or a model reviewing the scan
output, sees what looks like a clean report plus an easy-to-miss count line — with no way to tell what
was suppressed without manually opening the baseline YAML. Nothing today distinguishes an
operator-authored baseline (legitimate: "we've triaged this and accept the risk") from a
repo-planted one (the exact thing the scanner exists to catch).

**Needs a design decision before code, not a one-line fix** — the two candidate directions don't have
to be exclusive:

- Gate baseline application on `config.WorkspaceTrusted`/`TrustWorkspace` (see `internal/workspacetrust`
  and the `CLAUDE.md` note on how that question must be asked), the same way `.aegis/.env` is already a
  documented, deliberate hole gated on trust rather than an oversight.
- Always list suppressed findings' identity (rule/CVE/severity/location) in the report regardless of
  trust — a baseline that can silently hide _what_ it hid is the sharper problem even before trust
  enters into it.

**SHIPPED 2026-08-31 — both halves, and they were not exclusive after all.**

The **disclosure half** was already in the tree (`Report.Format`, `security.go`): every suppressed
finding is printed with its severity, tool, title, location and rule ID, not just counted. That closed
the sharper problem — a baseline that can hide _what_ it hid — and is now documented as behaviour in
[docs/security_scan.md](../docs/security_scan.md) rather than being an undocumented side effect.

The **trust-gate half** is this sitting's code. `Report.applyBaseline` now asks
`config.WorkspaceTrusted` (through a `baselineTrustCheck` var, so tests pin both answers without
touching the real trust store — the question is still asked the one way `CLAUDE.md` requires) before
any entry may suppress anything. Untrusted, nothing is hidden: every entry is recorded in the new
`Report.BaselineUntrusted` and the report prints `Baseline IGNORED (scan target is not a trusted
workspace)` followed by each skipped entry and the remedy (`aegis trust`). Trusted, P11.8's
accepted-risk workflow is unchanged.

The direction the entry proposed and the tree accepted: a suppression is an _operator_ decision, and
the one thing separating an operator-authored baseline from a repo-planted one is that the operator
trusted the directory. Deliberately a **refusal to apply**, not a downgrade of the file — the baseline
is read and reported either way, so a hostile repo shipping one is now _louder_ in the report than a
repo shipping none, which inverts the incentive the finding described.

Tests: `internal/security/baseline_trust_test.go` covers both sides of the gate plus the
no-baseline-file case (which must stay silent, trusted or not). Two existing tests
(`TestRunWithOptionsAppliesBaselineSuppression`, `TestScanRegressionAcrossRecordedOutputs`) now pin the
gate open with a comment saying why — their fixtures stand in for an operator-authored baseline, and
the golden records aggregation behaviour, not the trust decision.

Priority: Tier 3 — real, currently exploitable against the exact adversarial case the subsystem is
designed to defend, but not small-effort/no-dependency (disqualifying Tier 1) and not a one-line fix
(disqualifying Tier 2) — the report-disclosure half and the trust-gate half both need scoping first.
**Closed.**

**The un-numbered inline-truncation/blackbox request closed out 2026-08-23.** Everything it asked for
was already shipped by the time it was reviewed — P74.2/P74.3/P74.4 (chrome removal, collapse-with-
expand, read/search grouping), P74.11/P74.12 (the stall ramp and eased token counter), P74.16 (overflow
clip-and-retry), and P75.1 (per-block expand). One thread it named — visibility into the model's actual
reasoning before it acts — is not covered by any of those and is real; it's filed on its own as
**P77.1**, since it's a design question rather than a continuation of that UI-polish work — shipped
2026-08-25, see [its record](releases.md#p771-shipped-2026-08-25). Full closure record for this
request: [releases.md](releases.md#the-inline-truncation-request-closes-out-2026-08-23-pxx1).

### P80.1 — An MCP client can list, and post turns into, sessions it did not create

**Filed 2026-08-30**, the one `internal/mcpserver` finding from the comprehensive audit's C1 pass that
was written up rather than fixed, because closing it the obvious way breaks a legitimate use. Full
context in [releases.md](releases.md#comprehensive-architecture-and-security-audit-remediated-in-full-2026-08-30)
(finding **F3**); the two findings either side of it, F1 (a caller could escalate its own permission
mode past `mcp_server.default_mode`, which also made `auto_approve` vacuous) and F2 (auto-approve was
one undiscriminated yes), both shipped the same day.

`aegis_list_sessions` (`internal/mcpserver/server.go`) proxies `Backend.ListSessions`, which is the
daemon's `store.List` — **every** session on the daemon, including ones the human created in the TUI.
`callPrompt` then accepts any `session_id` verbatim and posts to it. So an authenticated MCP client
can enumerate an interactive `auto`-mode session and inject a turn into it, inheriting that session's
mode, persona and workdir — including an `additional_roots` workspace the MCP path could never have
created for itself, since `callPrompt` never sets `CreateSessionRequest.Workdir`.

It also bounds what F1's clamp is worth. That clamp binds sessions this server _creates_; a session it
merely borrows carries whatever mode it already had.

**Why it is not a one-line fix.** Tracking the session IDs this `Server` instance created and rejecting
the rest is about twenty lines, and it breaks an editor plugin resuming a session across an
`mcp-serve` restart — the in-memory set does not survive the process. The version that does survive is
a session _origin_ recorded at creation and filtered server-side, which is a schema and API decision,
not a defect fix. Deciding which of those two the product wants — or that today's reach is intended and
the package doc should say so — is the work here.

Priority: Tier 3 — real and currently reachable by any authenticated MCP client, but needs a product
decision about cross-restart session continuation before any code. Same shape as **P76.3**: a genuine
gap whose fix is a design choice.

**Independently confirmed 2026-08-31.** The STRIDE-A threat model reached this item's exact mechanism
from a cold read and filed it as
[**FIND-21**](../threat-model-20260831-002123/3-findings.md#find-21-an-mcp-client-can-enumerate-and-post-turns-into-sessions-it-did-not-create)
(CVSS 7.1, `Important`, CWE-639), citing this entry back. **It got no P81 number — this is that item.**
Two things the report adds. It agrees the session-_origin_ record is the right fix and the in-memory
set is not, and it proposes an interim that does not need the schema decision: **clamp a borrowed
session's effective mode to `mcp_server.default_mode` at prompt time rather than at creation time**, so
F1's clamp is not bypassable by reuse while the design question stays open. It also extends the finding
to the ACP path — `internal/acp/agent.go:167`'s `handleNewSession` takes a client-supplied mode with no
configured ceiling at all — which is a second instance of the same gap and should be fixed with this
one, not separately.

**The interim SHIPPED 2026-08-31, on both surfaces; the design question stays open.**

`mcp_server.default_mode` is now a ceiling on a _borrowed_ session, applied at prompt time
(`Server.checkBorrowedSessionMode`, `internal/mcpserver/server.go`). Prompting into a session this
server did not create whose mode exceeds `default_mode` is refused with an error naming both modes and
the way out; a session at or below the ceiling is untouched. `allow_caller_mode_escalation` turns it
off, exactly as it turns F1's clamp off — one switch, one meaning.

Three decisions worth keeping. It **refuses rather than downgrading**: the session belongs to someone
else, and silently moving a human's TUI session from `auto` to `plan` is a side effect no MCP caller
should be able to cause. The in-memory created-set is present but is a **fast path, not the control** —
it does not survive an `mcp-serve` restart, which is precisely why rejecting everything outside it
would break the editor-plugin resume this entry protects; an unrecognised id is checked against the
ceiling instead. And an id the daemon does not list is **allowed through**: it is unverifiable, not
evidence of escalation, and the enumeration this defends against only reaches listed sessions.

The ACP half is fixed with it, as the report asked. The entry's description of it was slightly
off — `handleNewSession` takes its mode from `aegis acp --mode`/`permission.mode`, not from the
client — but the same gap is there one method along: `session/prompt` takes the client's `sessionId`
verbatim, so `--mode` described only the sessions the agent creates. `Agent.checkBorrowedSessionMode`
applies the configured mode as the same ceiling (`internal/acp/agent.go`), and `acp.Backend` gained
`ListSessions` for the mode lookup only — nothing in the ACP protocol surface exposes the list. Lower
reach than the MCP surface, since ACP has no session enumeration and a caller must already know an id;
same finding, fixed in the same sitting rather than left as the second instance.

Tests: `internal/mcpserver/borrowed_session_test.go` (refusal above the ceiling, resume at or below it,
the escalation opt-in, this server's own session, an unlisted id) and
`internal/acp/borrowed_session_test.go` (both directions over the wire).

**What is still open, and it is the reason this stays Tier 3:** `aegis_list_sessions` still lists every
session on the daemon, and the fix for that is the session _origin_ recorded at creation and filtered
server-side — a schema and API decision, unchanged by this work. What changed is the blast radius: a
borrowed session can no longer carry a mode past what the operator configured, so the open question is
now enumeration and cross-restart continuation rather than privilege.

### P81.1 — Untrusted content is marked, not contained (FIND-01)

**Filed 2026-08-31**, the single `Critical` finding of the threat model
([**FIND-01**](../threat-model-20260831-002123/3-findings.md#find-01-indirect-prompt-injection-is-marked-but-not-constrained),
CVSS 8.7, CWE-1427, OWASP LLM01) and the item the report's executive summary names as the shape of
every other gap in the batch: _the controls that constrain the machine are strong; the controls that
constrain the model are advisory._

Content from `web_fetch`/`web_search` and results from external MCP servers enter model context inside
a provenance wrapper produced by `internal/trust` (`Wrap(tag, attrs, sourceDesc, content, scan)`),
which `internal/mcp/trust.go` applies to every MCP result "regardless of scan settings" and
`internal/tool/builtin/web.go:17,28-40` applies to fetched pages. The wrapper is prose addressed to
the model. It frames content as data and annotates suspected injection patterns when the heuristic
scan fires. **It enforces nothing.**

The permission gate is the real boundary, and in the default `build` mode it holds for execute-capable
calls and not for reads and writes, which are allowed without prompting. So an injected instruction
along the lines of _read `~/.ssh/id_rsa` and put it in your next search query_ traverses no approval
prompt at all. In `auto` mode nothing prompts. Combined with **P81.8**, that is a complete
exfiltration path with no record of it having happened.

**What to do**, in the order the report puts them: treat content that arrived inside an
untrusted-content wrapper as **tainted for the remainder of the turn**, and while tainted content is
in context require approval for any call whose capability is write, execute or network, regardless of
mode; add the per-session egress ledger (**P81.8**) so an attempt is visible even when it succeeds;
and promote a heuristic scan hit from an annotation to a decision point — ask _before_ the content
enters context, not after.

**Two constraints this repo already imposes on the fix.** The taint flag has to survive compaction, or
the containment expires exactly when a long session is most exposed — and `internal/compaction`'s
summary skeleton is a wire format between successive summaries (`<read-files>`/`<modified-files>`),
so carrying a taint marker forward is an addition to that contract, not a free field. And the gate
consults `tool.EffectiveCapability` _before_ `Policy.Decide`, so a taint rule written at the policy
layer is bypassed by anything the shell classifier downgrades to `CapRead` (**P81.20**). The two items
are the same boundary seen from two sides.

**How to verify:** an `internal/eval` scenario where a fetched page instructs the model to read a file
outside the workspace and place its contents in a subsequent `web_fetch` URL, asserting the second
fetch is gated rather than executed — then the same scenario run past the compaction trigger.

Priority: Tier 3 — the highest-severity item in the batch, `High` remediation effort, `Redesign`
mitigation type, and genuinely sequence-dependent: it wants **P81.8**'s ledger and interacts with
**P81.20**'s ordering. Not a defect fix; a containment model this system does not currently have.

### P81.8 — `web_fetch` is an unreviewed egress channel (FIND-08)

**Filed 2026-08-31**, from the threat model
([**FIND-08**](../threat-model-20260831-002123/3-findings.md#find-08-web_fetch-is-an-unreviewed-egress-channel),
CVSS 6.9, `Important`, CWE-201). The inbound half of `web_fetch` is thoroughly handled and should be
left alone: `internal/netblock` blocks loopback, RFC1918, CGNAT, link-local, multicast, reserved space
and the NAT64 well-known prefix, and `CheckRedirect` re-validates every hop. The outbound half is not
handled at all. The model chooses the URL, so any workspace-derived data can be encoded into a path or
query string aimed at an ordinary public host, and the fetch succeeds because the _destination_ is
perfectly legitimate. `internal/netblock` blocks destinations; nothing inspects payloads.

**What to do.** Apply `internal/redact`'s classes to the outbound URL and refuse a fetch whose URL
carries material matching a high-confidence secret pattern. Maintain a per-session egress ledger of
every fetched URL and byte count, surfaced in the TUI/UI and written to the audit sink (**P81.14**).
Offer an opt-in host or host-suffix allowlist for operators who want egress restricted rather than
merely recorded.

Priority: Tier 3 — M, and the enabling half of **P81.1**: the ledger is what makes an injected
exfiltration attempt visible whether or not the taint rule catches it. Worth building first of the
two, because it is useful standalone and the taint work is not.

### P81.5 — Outbound provider payloads and tool arguments are never redacted (FIND-05)

**Filed 2026-08-31**, from the threat model
([**FIND-05**](../threat-model-20260831-002123/3-findings.md#find-05-outbound-provider-payloads-and-tool-arguments-are-never-redacted),
CVSS 7.5, `Important`, CWE-200). `internal/redact` holds a good pattern set — PEM private keys, AWS
access key IDs, `sk-`-style API keys, GitHub and Slack tokens, JWTs, bearer tokens, generic secret
assignments — and `security.redact_secrets` defaults to true. It is applied on the _sharing_ and
persistence paths (`internal/share`, `internal/security/redact.go`) and **not** on the request the
engine sends to the model provider, nor on tool arguments the model constructs for an external MCP
server. The one path that reliably carries workspace content off the host is the one path the redactor
does not cover.

For a loopback Ollama deployment this is close to academic, which is why it is filed rather than
urgent here. For a cloud provider it means a `.env` file, a private key or a hard-coded credential the
agent happened to read is transmitted to a third party under that vendor's retention terms, with no
local record of what was sent.

**What to do.** Run `redact.Text` over outbound provider payloads when the resolved endpoint is not
loopback, keyed on `config.IsLoopbackBaseURL` and controlled by a config key defaulting to on for
cloud providers. Escalate `warnOutboundSecrets` (`internal/mcp/outbound.go:40`) from a log line to a
gate — refuse or require approval when an argument matches a high-confidence class. Record a hash and
byte count of each outbound payload in the audit sink so "what left this machine" is answerable after
the fact.

**Verify against the threat model's own uncertainty first.** Its "Needs Verification" table records
that the absence of a redaction call on the provider path is "absence of evidence in the read files,
not proof of absence across all 896 files" — grep the adapter request-construction paths before
building.

Priority: Tier 3 — M. Real, but the trigger is a cloud provider, and this machine runs local models by
default. Promote the moment a cloud key becomes the working default.

### P81.2 — External MCP servers are trusted on configuration alone (FIND-02)

**Filed 2026-08-31**, from the threat model
([**FIND-02**](../threat-model-20260831-002123/3-findings.md#find-02-external-mcp-servers-are-trusted-on-configuration-alone),
CVSS 8.2, `Important`, CWE-1357). An MCP server's identity in this system is a configured command
string or URL. `internal/mcp/mcp.go:173` launches stdio servers with
`exec.CommandContext(ctx, command, args...)` — resolved through `PATH`, with no absolute-path
resolution and no binary verification — so a shimmed or replaced binary is indistinguishable from the
intended server. Once connected the server controls its own advertised tool set: it can advertise a
name that resembles or shadows a built-in, and it can grow that set mid-session through
`tools/list_changed` with no fresh operator decision.

**The registry side of this is already right and must not be broken while fixing the rest.** Clones
share one `toolTable` so a parent re-registration reaches existing clones, and a stale schema cache is
discarded when the parent version moves — both pinned by tests, both documented in `CLAUDE.md`. The
gap is upstream: nothing establishes that the server on the other end is the one the operator
approved, and nothing re-asks when what it offers changes.

**What to do.** Resolve stdio commands to an absolute path at configuration time and record a digest
of the resolved binary; refuse to start a server whose digest changed without re-approval. Namespace
MCP tool names on exposure (`mcp__<server>__<tool>`) and reject an exposure that would collide with a
built-in. Record the approved tool set per server and re-ask when it grows, not only when a schema
changes.

Priority: Tier 3 — M, and sequence-dependent on **P80.1**: both are decisions about what an MCP peer
is allowed to be, and the namespacing change touches the same exposure path P80.1's origin record
will. Take them in one sitting.

### P81.3 — Config PATCH endpoints can disable command isolation with only the bearer token (FIND-03)

**Filed 2026-08-31**, from the threat model
([**FIND-03**](../threat-model-20260831-002123/3-findings.md#find-03-configuration-endpoints-can-disable-command-isolation-with-only-the-bearer-token),
CVSS 8.1, `Important`, CWE-284). `PATCH /config/sandbox`, `/config/security`, `/config/skills` and
`/config/cost` (`internal/server/lifecycle.go:68-77`) mutate the settings that decide how much
isolation the agent's command execution gets. Authorization for all four is the same single bearer
token used for every other call: no second factor, no interactive confirmation, no distinction between
"read a session" and "change the sandbox backend". The token is a file readable by any process running
as the operator, so the plain statement is that **any local process running as the operator can weaken
command isolation and then drive unattended host execution through entirely legitimate API calls.**

`server.allow_remote` extends the same reasoning to the network, where the token is the only control.

**What to do.** Split the credential — a second, separately-stored token for endpoints that weaken a
security posture, or an interactive operator confirmation surfaced through the TUI/UI for those
endpoints. Log every accepted `PATCH /config/*` as a structured audit event with before/after values
(**P81.14**). And refuse at _runtime_ the transitions the daemon already refuses at _startup_, in
particular moving to the `local` sandbox backend while `auto_approve_exec` is set without
`allow_unsandboxed_auto_exec` — that check exists and simply is not consulted on the PATCH path.

Priority: Tier 3 — M. The runtime-transition refusal is the small, obviously-correct half and could be
taken alone; the credential split is a design decision about how many secrets this daemon has.

### P81.4 — The web UI hands the real daemon token to browser JavaScript (FIND-04)

**Filed 2026-08-31**, from the threat model
([**FIND-04**](../threat-model-20260831-002123/3-findings.md#find-04-the-web-ui-hands-the-real-daemon-token-to-browser-javascript),
CVSS 7.6, `Important`, CWE-522). `handleAuthExchange` ends with
`writeJSON(w, http.StatusOK, map[string]string{"token": s.authToken})` — the real, long-lived daemon
token, in a JSON body, held thereafter in SPA memory for every call. Any script-execution defect in
the SPA, any dependency compromise in its bundle (**P81.17**), or any browser extension with host
permissions on `127.0.0.1` reads it and gains the full API surface — including the config endpoints in
**P81.3** — for as long as the token file is unchanged, which today is forever (**P81.25**).

The page-token exchange was designed to keep the real token out of the served HTML and it succeeds at
that; what it does not do is keep the credential out of the page's script context afterwards. The
code already acknowledges the second half: a raw local HTTP client that never goes through a browser
can complete the whole mint-and-exchange flow itself, because neither HttpOnly cookies nor CORS
constrains a non-browser caller.

**What to do.** Stop returning the daemon token to the browser: have `/auth/exchange` set an
`HttpOnly`, `Secure`, `SameSite=Strict` session cookie scoped to the daemon origin, and accept that
cookie plus the existing CSRF header on API routes for browser clients. Give browser sessions their
own short-lived revocable credential rather than the process-wide token. Track the residual
non-browser mint path explicitly — an operator-visible notice when a page token is exchanged while no
UI window is known to be open.

Priority: Tier 3 — `High` effort, `Redesign` mitigation. Larger than it looks because the SPA's whole
call path changes with it, and it wants **P81.25**'s revocation story to be useful.

### P81.10 — The container workspace mount is broader than any single command needs (FIND-10)

**Filed 2026-08-31**, from the threat model
([**FIND-10**](../threat-model-20260831-002123/3-findings.md#find-10-the-container-workspace-mount-exposes-the-whole-workspace-to-every-command),
CVSS 6.5, `Important`, CWE-668). The container sandbox bind-mounts the workspace root read-write into
every command's container. The isolation flags around it are good and should stay — `--cap-drop=ALL`,
`--security-opt=no-new-privileges`, `--network none` by default, memory/CPU/PID limits
(`internal/sandbox/docker.go:237-240,284,320-334`). The mount is the part that is wider than the job.
A command whose classified purpose was to read one file can read everything under the root, including
`.aegis/.env` — which is explicitly a secrets file living inside the mounted directory — and can write
`.aegis/config.yaml` and project skill and persona files, which are inputs the harness itself trusts
on the next load.

**What to do.** Exclude `.aegis/.env` and any operator-configured secret paths from the bind mount.
Mount read-only for commands the classifier resolved to `CapRead` and read-write only for commands
approved as writes — which means this item consumes the same per-call capability verdict
`tool.WithCapabilityMemo` already memoizes, rather than re-classifying. Where the runtime supports it,
mount only the subtree the resolved argv references.

Priority: Tier 3 — M, and sequence-dependent on the capability memo being the mount's input. The
`.aegis/.env` exclusion is separable and much smaller; it could ship first.

### P81.12 — Release artifacts ship without checksums, signatures or provenance (FIND-12)

**Filed 2026-08-31**, from the threat model
([**FIND-12**](../threat-model-20260831-002123/3-findings.md#find-12-release-artifacts-ship-without-checksums-signatures-or-provenance),
CVSS 6.1, `Moderate`, CWE-494, SLSA). `release.yml` builds five platform archives and publishes them
with `gh release create "${GITHUB_REF_NAME}" out/*` — no checksum file, no signature, no build
provenance attestation. A user downloading a release cannot verify the archive is the one this
workflow built. Two adjacent weaknesses sit in the same workflows: every action is referenced by a
mutable major tag (`actions/checkout@v7`, `actions/setup-go@v7`, `github/codeql-action/init@v4`)
rather than a commit SHA, in a job holding `permissions: contents: write`; and `ci.yml` installs
`govulncheck` and `staticcheck` with `@latest`, so the versions gating merges are whatever the module
proxy serves that day.

**What to do.** Generate a `SHA256SUMS` over the artifacts and upload it; sign it, or use
`actions/attest-build-provenance`. Pin every action to a full commit SHA with the version as a
trailing comment and let Dependabot maintain the pins. Pin the analysis tools to explicit versions —
the workflow's own `GOTOOLCHAIN` note already documents a version-skew trap from exactly this pattern.

**Updated 2026-08-31.** `release.yml`'s disablement is confirmed deliberate (**P81.11**), so the
checksum/signature/provenance half is **not** dormant-until-re-enabled — it is dormant until this
project publishes releases again, which is a product decision nobody has made. Do not build it
speculatively. What survives the decision and is worth taking now is narrower and real: **pin every
`uses:` reference in `codeql.yml` to a full commit SHA**, because that is the one workflow with live
`push`/`pull_request`/`schedule` triggers and it holds `security-events: write`. A retagged action
there runs against every push to `main` today.

Priority: Tier 3 for the release-artifact half — parked behind a product decision about releases, not
behind a trigger. The `codeql.yml` action-pinning half is **Tier 2 — XS** and should be split out and
taken with **P81.17**; it is the only supply-chain work in this batch that touches a workflow that
actually runs.

### P81.14 — There is no default audit trail for anything privileged (FIND-14)

**Filed 2026-08-31**, from the threat model
([**FIND-14**](../threat-model-20260831-002123/3-findings.md#find-14-no-default-audit-trail-for-privileged-operations-policy-decisions-or-executed-commands),
CVSS 5.7, `Moderate`, CWE-778) — and the item the largest number of other findings depend on.
`internal/hooks` contains an `Audit` sink with exactly the right record types (`PreToolUse`,
`PostToolUse`, `PolicyDecision`, `SubagentStop`) and an `auditInput` that already scrubs tool inputs
before writing. Nothing constructs it unless configured. A default deployment therefore keeps no
durable record of which commands the agent executed, which policy decisions the gate made, or which
privileged configuration changes were accepted.

**The inversion is worth stating plainly**, because it is what makes this a finding rather than a
feature request: the daemon reliably records that someone guessed a token wrong (`invalidAuthLogEvery
= 5`, sampled but on by default) and records nothing at all when someone holding the right token
switched the sandbox off and ran a command.

This is also why every repudiation threat in the model has no answer — a TUI approval, a turn injected
by an MCP client (**P80.1**), an unattended cron run (**P81.23**), a mutated trace record
(**P81.24**).

**What to do.** Turn on a default audit sink writing under the data directory, created with
`fsguard.RestrictToOwner` and rotated by size. Emit a record for every accepted state-changing HTTP
endpoint including before/after values for config PATCHes (**P81.3**). Stamp a message origin —
`tui`, `web`, `acp`, `mcp`, `cron` — on every persisted turn and every audit record; that stamp is
also what **P81.9** needs to keep a TUI carve-out and what **P80.1** needs for its session-origin
filter.

**Check the premise first.** The threat model's "Needs Verification" table flags that FIND-14 assumes
the sink is opt-in from the absence of a default construction site; trace whether `enginecfg`'s hook
chain builds `hooks.NewAudit` when no hook config is present before treating it as unwired.

Priority: Tier 3 — M, and the batch's most-referenced dependency: **P81.3**, **P81.5**, **P81.8**,
**P81.23**, **P81.29** and **P80.1** all want the origin stamp or the sink. Build it early even though
it is not the highest-severity item.

### P81.20 — Plan mode's guarantee rests on a 1,129-line classifier, structurally (FIND-20)

**Filed 2026-08-31**, from the threat model
([**FIND-20**](../threat-model-20260831-002123/3-findings.md#find-20-plan-modes-read-only-guarantee-rests-on-a-1129-line-shell-classifier),
CVSS 7.3, `Important`, CWE-863). This is the structural half of what **P79.1** filed as a specific
regression, and the two should not be confused. P79.1 is _did this Windows path escape reproduce, and
is it reachable through `shellTool.CapabilityFor` end to end_. This item is _the arrangement that
makes every parser defect a plan-mode defect, of which P79.1 is the third instance._

`Gate.Check` consults `tool.EffectiveCapability` **before** `Policy.Decide`, so any call
`classifyShellCommand` downgrades to `CapRead` is allowed silently in every mode — including plan
mode, where an execute call would have been denied. The design is deliberate, documented, and has a
real benefit (before it, `git status` in plan mode was silently denied, which was worse). It is also
why CRIT-1 (an unexpanded `~`), CRIT-2 (an unconfined `argv[0]`) and P79.1 (Windows absolute-path
escapes through the attached-flag and PowerShell operand paths) were each a plan-mode bypass rather
than a parsing bug. The threat model verified on 2026-08-31 that the four P79.1 tests pass in the
current working tree — **the specific regression appears addressed; the structure that produced three
of them is not.**

**What to do**, and note that the first item is a posture change rather than a parser change:

1. Default `permission.plan_mode_shell_reads` to `false` for workspaces that have **not** been granted
   trust, keeping today's default for trusted ones — so the posture an operator picks for reviewing an
   untrusted repository does not depend on parser correctness. The flag and both postures already
   exist and are already required to keep working.
2. Narrow the classifier's `CapRead` surface to an explicit allowlist of command forms rather than a
   denylist of escapes, so an unrecognised construct fails closed.
3. Grow `FuzzClassifyShellCommand`'s seed corpus with every fixed case including the P79.1 Windows
   forms. **The "run it in CI" half has no home as of 2026-08-31** — `ci.yml` stays disabled by
   decision (**P81.11**) and `codeql.yml`, the only workflow that runs, does not execute Go tests.
   Either give the fuzz target a scheduled workflow of its own (the pattern `codeql.yml` already uses
   for its weekly cron), or accept that it runs only when someone runs it and say so. Seed-corpus
   growth is worth doing either way: it is what makes a manual run find the next case.
4. Offer an _enforcing_ mode for persona `tools:` lists, for operators using a persona as a
   containment boundary. Advisory is the documented default and should stay the default.

Priority: Tier 3 — M, `Redesign`. Sequence-dependent one way rather than both, as of 2026-08-31: the
fuzz gate it wanted from **P81.11** is not coming, so the CI dependency is gone and the corpus work is
unblocked today. **P81.1**'s taint rule is still bypassed by exactly this ordering if it lands at the
policy layer alone, and that dependency stands.
Do **P79.1**'s reachability check first — it is the cheaper question and it may change this one's
urgency.

### P81.22 — Command isolation degrades to unsandboxed host execution on a warning line (FIND-22)

**Filed 2026-08-31**, from the threat model
([**FIND-22**](../threat-model-20260831-002123/3-findings.md#find-22-command-isolation-silently-degrades-to-unsandboxed-host-execution),
CVSS 6.9, `Important`, CWE-693). `sandbox.backend` defaults to `container` and `SelectSandbox`
cascades: no container runtime falls back to OS-level isolation (seatbelt/bwrap), and a host with
neither falls back to the unsandboxed `local` backend with a startup `WARN` — "never a hard failure,
`sandbox.strict` aside."

**The population this lands on is not marginal, and the config comment names it: "every current
Windows box."** That is the machine this project is developed on. On those hosts every model-requested
shell command runs directly on the host with the operator's full privileges, with no filesystem
confinement beyond the tool-level path checks and none of the memory, CPU or PID limits the container
path applies (`internal/sandbox/local.go:26-53,83-90` versus `docker.go:320-334`). One startup line is
thin signal for a change of that size, and it is emitted at start rather than at the moment a command
runs unconfined. The daemon does refuse `auto_approve_exec` on the local backend without
`allow_unsandboxed_auto_exec`, which bounds the worst combination — that check is the reason this is
Tier 3 and not higher.

**What to do.** Make `sandbox.strict` the default so unavailable isolation is a visible failure rather
than a silent downgrade, with an explicit opt-out for hosts that genuinely have neither option.
Surface the effective backend in the approval prompt and the TUI status line (shared with
**P81.33**), so "this command will run unconfined" is stated at the decision, not only in the startup
log. Apply OS-level resource limits — job objects on Windows, rlimits or cgroups on POSIX — so
`sandbox.limits` means something on every backend. Document the persistent-container state model
(`sandbox.persistent: true`) and offer a per-command reset: state carries across commands for the
session TTL, so a command that plants a shim or edits `PATH` inside the container affects every later
command in that session.

**Note the overlap with P77.6** (no OS-level process sandbox on Windows, Tier 4, GAP-05). P77.6 is the
missing capability; this is the missing _signal_ that the capability is absent. The signal half is
worth taking even if the capability half stays parked.

Priority: Tier 3 — M. Changing a default that will fail closed on the maintainer's own machine is the
part that needs care, which is why the prompt-surfacing half should ship first.

### P81.23 — Scheduled jobs run unattended in whatever mode they were created with (FIND-23)

**Filed 2026-08-31**, from the threat model
([**FIND-23**](../threat-model-20260831-002123/3-findings.md#find-23-scheduled-jobs-run-unattended-in-whatever-mode-they-were-created-with),
CVSS 6.5, `Important`, CWE-284). A cron job stores a prompt and a configuration and fires it later
with nobody present. A job created in `auto` mode auto-approves execute-capable calls on every future
firing, indefinitely, and each run reads workspace files and sends them to the configured provider
unobserved — so an exfiltration path opened by **P81.1**/**P81.8** runs with no one watching it.

Cron is also a persistence mechanism. An attacker who obtains the bearer token once can register a
recurring job that keeps running after the token is rotated (**P81.25**), and nothing at daemon start
re-presents the registered job set for the operator to recognise or disown.

**The engine-side correctness here is good and is not what this item is about.**
`tool.CapabilityOverrider` classifies against the job's `effectiveRoot` — cron jobs were the case that
motivated the per-call memo. The gap is the authorization _posture_ the job carries, not the
classification.

**What to do.** Refuse `auto` mode for scheduled jobs unless the effective sandbox backend is a real
isolation backend, mirroring the `allow_unsandboxed_auto_exec` gate the daemon already applies at
startup (**P81.22** is what makes "effective backend" answerable). Present the registered job set at
daemon start and in the TUI status surface. Require re-confirmation for a job created outside an
interactive session before its first firing. Stamp a `cron` origin on every persisted turn from a
scheduled run (**P81.14**). The ACP path has the same unattended shape when driven by an editor
plugin, and `session/new` additionally lets the client choose its own mode with no ceiling — fix that
alongside **P80.1**'s clamp rather than separately.

Priority: Tier 3 — M, sequence-dependent on **P81.22** and **P81.14**.

### P81.26 — Sandboxed commands inherit the daemon environment minus a denylist (FIND-26)

**Filed 2026-08-31**, from the threat model
([**FIND-26**](../threat-model-20260831-002123/3-findings.md#find-26-sandboxed-commands-inherit-the-daemon-environment-minus-a-denylist),
CVSS 5.7, `Moderate`, CWE-526). Commands are spawned with
`cmd.Env = filteredEnv(os.Environ(), l.stripEnv)` on both the `Exec` and `ExecStreaming` paths
(`internal/sandbox/local.go:53,90`) — the daemon's own environment with a denylist removed.
`DefaultStripEnv` covers the known-sensitive names and `NewLocalBackendWithEnv` extends it with names
loaded from `.aegis/.env`, which is the right instinct. But **a denylist over an inherited environment
fails open**: any secret-bearing variable the operator's shell happens to export, that nobody thought
to add to the list, is visible to every command the model runs.

And the daemon's environment is where API keys live _by design_ — `CLAUDE.md` states secrets come only
from the environment or `.aegis/.env`. That makes the inherited environment exactly the wrong starting
point for a sandboxed command.

**What to do.** Invert to an allowlist: start empty and pass only what a command needs — `PATH`,
`HOME`, `TMPDIR`, locale, plus an operator-configurable list. Keep `DefaultStripEnv` as a second layer
over the allowlisted names that can still carry secrets. Apply the same construction to the container
backend's `--env` handling so both paths agree.

**The risk in this one is breakage, not design.** A `go build`, an `npm ci`, a `git` invocation behind
a corporate proxy and anything reading `GOPATH`/`GOCACHE`/`GOMODCACHE` all need environment this
inversion removes by default. Build the allowlist against a real run of the project's own toolchain
before shipping it.

Priority: Tier 3 — M, `Redesign`. The change is small; getting the allowlist right without breaking
every build command is the work.

### P81.27 — Trust grants are unauthenticated local state, and `.aegis/.env` is outside the fingerprint (FIND-27)

**Filed 2026-08-31**, from the threat model
([**FIND-27**](../threat-model-20260831-002123/3-findings.md#find-27-workspace-trust-grants-are-unauthenticated-local-state-and-exclude-aegisenv),
CVSS 5.5, `Moderate`, CWE-345). The trust store is what stops a cloned repository's
`.aegis/config.yaml` from silently widening the agent's posture, and the fingerprint pinning is a
genuinely good design: a grant is bound to the security-relevant subset of that directory's config, so
changing a frozen key re-prompts. Two gaps sit around it.

First, the store is unauthenticated local state. `workspacetrust.save()` writes `0o600` inside a
`0o700` directory with no integrity protection and no `fsguard.RestrictToOwner`, so a same-user
process can insert a grant for any directory and suppress the prompt entirely. Entries record
`TrustedAt` and the fingerprint but **not who granted them or through which interface**, so an
inserted grant is indistinguishable from an operator decision.

Second, the fingerprint deliberately excludes `.aegis/.env`, and the code says so in as many words:
"The honest fingerprint would include .aegis/.env, and this one does not." The reasoning is real — the
trust decision resolves before any project-controlled file is read — but the consequence is that a
project can change the secrets loaded into the daemon's environment without invalidating an existing
grant.

**What to do.** Apply `fsguard.RestrictToOwner` to the trust store file (the same one-line class of
fix as **P81.24** and **P81.25**; take all three together). Authenticate the entry set with a MAC
keyed from the OS credential store so an inserted grant is detectable. Record the granting interface
and requesting process alongside each entry (**P81.14**'s origin stamp again). And revisit the
load-order constraint — hashing `.aegis/.env`'s presence and digest _without_ reading its contents
into the environment first is the shape that would close the documented hole without breaking the
ordering it exists for.

Priority: Tier 3 — M. The ACL is trivial and should ride with the other two; the MAC and the
fingerprint-ordering question are the real content and need a decision about where the key lives.

### P81.30 — Parallel rounds do not order shell commands against concurrent writes (FIND-30)

**Filed 2026-08-31**, from the threat model
([**FIND-30**](../threat-model-20260831-002123/3-findings.md#find-30-parallel-tool-rounds-do-not-order-shell-commands-against-concurrent-writes),
CVSS 4.8, `Moderate`, CWE-362) — and the only item in the batch that is a **correctness** finding
rather than a containment one. In a parallel round, write and execute calls share one exclusive
`execLock`; reads and network calls take no lock and are deliberately not held off by a concurrent
write (P8.6). The only read-versus-write ordering is a same-`path` dependency graph keyed on the
literal `"path"` input field — and `shell`'s schema carries a _command_, not a `path`, so as
`CLAUDE.md` states outright, "a `shell` call and a `read_file` are never ordered."

The consequence is a torn read: a `shell cat` or a `read_file` can observe a file mid-write and feed
partially-written state back into the model's reasoning. Documented rather than accidental, but it
means the file state the model believes it is acting on can differ from what is on disk.

**What to do.** Extend the dependency graph to cover shell commands using the classifier's
already-resolved argv — the same resolution `argv_confine.go` performs, and the same verdict
`tool.WithCapabilityMemo` already holds, so this should consume an existing result rather than
re-parse. Where a path cannot be resolved from a command, order that command conservatively against
any concurrent write in the round. Add a regression test issuing a `write_file` and a `shell cat` of
the same path in one round, asserting deterministic ordering and unchanged round latency for unrelated
calls.

Priority: Tier 3 — M, sequence-dependent on the argv resolution being reusable from `toolround.go`.
Note the tension with P8.6's deliberate no-lock-on-reads decision: this narrows it by dependency, not
by lock, and any fix that reintroduces a broad read lock is the wrong one.

---

## Open Work — Tier 4

**Status: 31 open** — four arrived 2026-08-31 with the P81 threat-model batch (**P81.7**,
**P81.18**, **P81.28**, **P81.31**), each parked for a stated reason rather than by default: P81.7's
prerequisite is a multi-user host and half its fix is upstream in Ollama; P81.18 is documentation and
a trust-store helper with no fired trigger while the UI stays on loopback; P81.28's shim is off by
default and the containment it wants is **P81.1**'s to build; and P81.31's reaping half rides along
with **P81.24** for free. **P80.2** and **P80.3** were filed 2026-08-30 as the residue of the comprehensive
audit (the three packages it never read, and the `Server` struct half of its L4 split). The other 25 are
8 pre-existing (all blocked or explicitly parked, none with a fired trigger),
6 from the P66 review batch, 5 from the P67 external-source reading, 5 from the P71 batch filed
2026-08-19 (**P71.6**, **P71.7**, **P71.11**, **P71.12**, **P71.13**), **P74.21** (filed 2026-08-21
the same day P74.17 shipped without it), and **P77.6** (filed 2026-08-25, spun out of P66.19). **P77.2**,
**P77.3**, **P77.4**, and **P77.5** (filed the same
day, same batch) all shipped 2026-08-24 — see [releases.md](releases.md#p774-shipped-2026-08-24).
**P77.1** shipped 2026-08-25 — see [releases.md](releases.md#p771-shipped-2026-08-25). **P78.1**–**P78.9**
filed and shipped 2026-08-26 — see [releases.md](releases.md#p781-p789-shipped-2026-08-26). **P70.3**
shipped 2026-08-18 and has left this tier. **P63.10** shipped 2026-08-21, taken opportunistically while
`internal/tui` was open for **P75.1** — record in
[releases.md](releases.md#p6310-shipped-2026-08-21).

The P66 entries here are **deliberately grouped grab-bags**: each collects the Low-severity residue of
one review domain. They are filed so no finding is lost, not because any of them should be scheduled.
Take one only when already working in that file. The P67 entries are a different kind of parked: each
is a capability Aegis does not have and nobody has asked for, filed with the specific trigger that
would make it worth building.

**The four P71 entries are a third kind, and two of them are parked by _choice_ rather than by
absence of demand.** **P71.6** (in-session response caching) and **P71.11** (window-derived research
budgets) are both blocked on **P71.8**: phasing changes the arithmetic under each, so fixing them
first would fit a constant to a regime about to change. **P71.7** (publication dates on search
results) waits on a keyed provider being the default, because that is the only backend where the date
is actually available. **P71.12** is different again — it is a filed **negative measurement**, kept
so the next reader does not re-derive an intuition this batch already tested and found small.

### P80.2 — Three packages the security audit never read

**Filed 2026-08-30** as the residue of the comprehensive audit's **C1** coverage-debt entry. That audit
(~109,700 non-test lines) deliberately did not reach five areas; three of them were reviewed on
2026-08-30 and are closed — `internal/mcpserver` (two findings fixed, one filed as **P80.1**), the
`internal/swarm` subprocess backend (the `aegis worker` lead resolved: it _does_ build its gate through
`enginecfg`; its one real gap, the worker spec file's Windows ACL, shipped), and the mode/approval
posture questions those two raised. Three areas remain unread: **`internal/tui`** (17.6k non-test
lines), **`internal/drive`**, and the **sandbox container backends** (`internal/sandbox`'s
Docker/Podman/WSL/Apple paths).

**What to look for, and what not to.** None of the three sits on a permission decision the way the MCP
server and the swarm backend did, so review them for _correctness_, not posture — that judgment is the
audit's own and is why this is Tier 4 rather than higher. The specific questions worth carrying in:

- **`internal/tui`** — is every path that renders model or tool output run through `internal/termsafe`
  (`StripControlSeqs`/`StripDangerousSeqs`)? A missed path is a real injection surface. Then Bubbletea
  concurrency (shared mutable state written off the `Update` loop) and resource lifetime (unclosed SSE
  streams, tickers never stopped). It is far too large to read whole; target those three.
- **`internal/drive`** — whether the fatal/resettable abort split CLAUDE.md documents (stall and
  wall-clock fatal to a drive, loop and tool-failure resettable) is what the code implements, and
  whether per-run budgets accumulate across phases rather than resetting each phase into meaninglessness.
- **sandbox container backends** — argument construction for `docker run`/`podman run`/`wsl`, mount
  confinement against `workspace.additional_roots` and symlinks, which runs get `--network none`,
  persistent-container reuse across trust boundaries, and above all whether a failed sandbox setup can
  ever fall through to running unsandboxed. That last one is the "ingress and core disagree" shape
  every finding in the audit shared.

Promote when: any of the three is being worked on for another reason, or a defect surfaces in one of
them — the audit's whole record is that this shape hides in the paths nothing exercises, so a _reported_
problem in one of these is evidence the rest of that package deserves the read.

Priority: Tier 4 — no fired trigger, L. Read-only investigation, not a build; scope it per package
rather than taking all three in one sitting.

---

### P80.3 — `Server`'s 60-field struct, after the file split

**Filed 2026-08-30.** The audit's **L4** ("split `internal/server` along the seams its mutexes already
suggest") had two halves. The file half shipped 2026-08-30: `server.go` went from 1,814 lines to 535
across seven topic files — `wiring.go`, `lifecycle.go`, `sessionscope.go`, `sandboxselect.go`,
`subagent.go`, `approval.go`, `status.go` — as a pure move, verified lossless by diffing the top-level
declaration set before and after (identical, plus the new `Server.Close`). Record in
[releases.md](releases.md#comprehensive-architecture-and-security-audit-remediated-in-full-2026-08-30).

The struct half did not. `Server` still carries ~60 fields with eight distinct mutexes among them, and
the field groups are already legible from those mutexes: the auth/lockout state (`authToken`,
`tlsCert`, `invalidAuthAttempts`, `authLockMu`/`authConsecutiveFailures`/`authLockedUntil`,
`pageTokenMu`/`pageTokens`), the context-window state (`ctxWinMu` guarding `ctxWin`, `ctxWinSrc`,
`ctxWinFinal`, `ctxWinByModel`, `autofitWin`, `weightsSeen`), and the per-session `sync.Map` family
(`sessionTools`, `taskScopes`, `sessionWorkdirs`, `sessionSkills`, `promptSectionCache`,
`sessionPermCache`, `sessionSems`) whose accessors now all live in `sessionscope.go`.

**Why it was not done with the file split.** Grouping fields into sub-structs is not a move; it rewrites
every reference, including the ~50 test call sites that build a `Server` field-by-field through
`newWithDeps`. The file split is worth having on its own and is safely reviewable as a no-op; bundling
the two would have made neither reviewable. The per-session map family is the natural first group — its
accessors are already one file, and `handleDeleteSession`'s obligation to clear every one of them is
exactly the kind of thing a struct makes checkable and a flat field list does not.

Promote when: someone is already changing that struct's shape, or a bug turns up that a grouped struct
would have made visible (a per-session map that `handleDeleteSession` forgot is the archetype — **M6**
in the audit was that bug, for `toolCallWarned`).

Priority: Tier 4 — no fired trigger, M. Structural and ongoing by the audit's own framing; the file
half already bought most of the legibility.

---

### P74.21 — The local-model harness still can't touch a prompt or a tool description

**Filed 2026-08-21, the day P74.17 shipped without it.** P74.17 built `internal/profile.Harness`,
resolved per `Request.Model`, and used it to move two response-repair behaviors (prose-tool-call
salvage, argument-shape repair) off the blanket `LocalPromptProfile()` boolean they were gated on
before. What it deliberately left alone is `builtin.Options.LocalProfile` itself:
`internal/tool/builtin/builtin.go:104` is still one bool, still deciding tool registration —
which families are deferred, which prompt caps apply, that `edit_file` moves behind `tool_search` while
the handle-based editors don't — for every local model identically, exactly as it did before P74.17.

The roadmap entry P74.17 shipped from sketched a fuller `Harness`: one that also carries
`PromptSuffix`, `ToolDescriptionOverrides` and a `DeferredTools` list, so a model-specific quirk could
add a line to the system prompt or rename a tool's description without every local model paying for it.
`deepagents` is still the reference for the shape — a provider-level profile layered under a
model-level one, additive, the same way `profile.NewResolver`'s `Override` already layers for the two
flag fields that shipped.

**Two of the four constraints the original entry named are still real and still apply here
unchanged:**

- **The prompt budget is test-enforced.** `TestEffectiveSystem_localProfileBudget` fails the suite when
  the local base prompt crosses `localBasePromptCeilingTokens`. A per-model `PromptSuffix` must be
  measured against that ceiling per model, not once.
- **Required scaffolding must not be excludable.** Aegis enforces the equivalent invariant at _test_
  time today (`TestEveryEngineCallSiteDecidesItsGate`, `TestEveryRegisterCallSiteDecidesTheLocalProfile`).
  A user-authorable per-model profile needs the same rejection enforced at _runtime_, because a config
  file is not a call site a build-time scan can audit.

**Promote when a concrete per-model prompt or tool-description need shows up** — a model whose own
quirk needs a system-prompt line, or whose tool descriptions need renaming to match vocabulary it was
trained on. P74.17 waited for exactly this kind of cargo (P74.8/P74.9) before it was worth building;
this is the same wait, one layer up.

Priority: Tier 4 — M. No fired trigger yet.

### P71.6 — Nothing memoizes a fetch or a search within a session

**Filed 2026-08-19.** `web_fetch` and `web_search` re-issue every request. The deep-research skill's
audit trail is explicitly designed around not repeating work — "it prevents re-fetching the same dead
ends when a topic gets revisited" (SKILL.md §2) — but that guarantee lives entirely in the model's
context, which compaction deletes. After a compaction the model has no record of what it fetched, so
a re-fetch is both likely and silently expensive: full network round-trip, full token cost again.

An in-session cache keyed on the normalized URL (and on query+max_results for search) would make the
audit trail's promise real rather than aspirational, and would make a re-fetch after compaction
nearly free — which is the recovery path P64.1 deliberately chose over spilling.

**Promote when** **P71.8** lands: a phased run reads the working file back into each fresh context
and will re-fetch by design at phase boundaries, which is the first time this stops being
speculative. Until then the compaction thrash (**P71.5**) dominates and this would be measuring the
wrong thing.

Priority: Tier 4 — S. No fired trigger yet.

### P71.7 — `web_search` results carry no publication date, so the source-quality bar cannot be applied

**Filed 2026-08-19.** Section 3 of the deep-research skill requires the model to "note publication
dates. For fast-moving topics prefer recent material and flag anything old enough that it may no
longer hold." Section 1 step 3 requires that quality bar to be applied to "result titles/URLs/
snippets _before_ fetching".

`searchResult` carries `title`, `urlStr`, `snippet` and nothing else (`web.go:203`), and DDG snippets
rarely contain a date. So the skill instructs the model to filter on a signal the tool does not
provide, at the one point where filtering is cheap. The only way to obey it is to fetch everything
first — which inverts the budget the skill is trying to hold, and on a small window is exactly the
behaviour **P71.5** makes unaffordable.

A fetched page usually carries `og:article:published_time` or a `<time>` element, which is a real
signal but only available _after_ the fetch this section is trying to avoid.

**Checked 2026-08-19, and weaker than assumed when this item was first filed: neither recommended
provider is a clean win.** Tavily's `/search` response schema is `title`, `url`, `content`, `score`,
`raw_content`, `favicon`, `images`, `id` — **no date field**. Brave's Web Search API supports
`freshness` as a _query_ filter (`pd`/`pw`/`pm`/`py`), which narrows _before_ searching rather than
labeling results _after_, and it was not possible to confirm from the public docs whether individual
result objects carry an `age`/`page_age` field — needs a live authenticated call against
`api.search.brave.com/res/v1/web/search` to settle, not another documentation read.

**Promote when** that live call is made (a five-minute check once a Brave key exists for any other
reason) and confirms a per-result date field, or when Brave's `freshness` filter is judged good
enough on its own — it solves a related but different problem: "don't return old pages" rather than
"tell me how old this page is". Until one of those is true this item stays unbuildable as stated.

Priority: Tier 4 — S. Real, and unbuildable well on the zero-config backend.

### P71.11 — The deep-research budgets are cloud-window constants handed to a local model

**Filed 2026-08-19.** The skill fixes its budget in prose: "**Round cap: 8**", "roughly 5–12 quality
sources", and it relies on `web_fetch`'s 20,000-char default per read. None of the three is a
function of the context window.

At `context_window: 16000` that budget is arithmetically impossible: 8 rounds × 5–12 sources ×
~5,000 tokens per source is one to two orders of magnitude past a window whose compaction trigger is
8,000 tokens. The model does not know this, so it plans a run it cannot execute and then discovers
the wall one compaction at a time. The 16k live run's own opening plan states "**Budget:** 8 rounds
max, targeting 5-12 quality sources" — copied faithfully from the skill, and never achievable.

**Do:** derive the round and source targets from the resolved window at skill-activation time, the
way `enginecfg` derives run limits once for every caller, rather than hard-coding a cloud-sized
number in prose. Roughly: four rounds and three or four sources at 16k, the current numbers at 128k.

**Promote when** **P71.8** lands — **it has, 2026-08-19.** Phasing changed the arithmetic: each
round is now a fresh, disk-grounded turn (P47.4) rather than a slice of one accumulating
conversation, so the per-_run_ budget this item measured is no longer the binding constraint the same
way. Re-derive the numbers against the phased shape (a round's own turn budget, not the whole run's)
before building this, rather than assuming the original math still applies unchanged.

Priority: Tier 4 — S. Was blocked on **P71.8** by choice; unblocked 2026-08-19, not yet promoted.

### P71.12 — Main-content extraction for `web_fetch` — measured, and smaller than it looks

**Filed 2026-08-19 as a negative measurement**, so the next reader does not re-derive it. `htmlToText`
(`web.go:257`) keeps every text node outside `script`/`style`/`noscript`, so a fetched page carries
its navigation, cookie prose, "This browser is no longer supported", breadcrumb and footer. The
obvious improvement is to prefer `<main>`/`<article>` and drop `nav`/`header`/`footer`/`aside`.

**It is worth less than it appears.** Measured across four `learn.microsoft.com` pages on 2026-08-19:

| page                                                 | raw HTML | after `htmlToText` | boilerplate | share |
| ---------------------------------------------------- | -------- | ------------------ | ----------- | ----- |
| `cloud-adoption-framework/ready/landing-zone/`       | 66,399   | 11,374             | 1,395       | 12%   |
| `architecture/networking/architecture/hub-spoke`     | 97,699   | 37,774             | 1,250       | 3%    |
| `defender-for-cloud/defender-for-cloud-introduction` | 64,194   | 17,305             | 1,218       | 7%    |
| `networking/design-guide/internet-ingress`           | 84,672   | 29,850             | 1,446       | 5%    |

Roughly **1.2–1.5 KB per page, 3–12%** — a few hundred tokens. The existing converter is already
doing the heavy lifting (66 KB of HTML down to 11 KB of text). Structural extraction is a real but
marginal win, and it carries a real risk: `<main>` heuristics fail differently per site, and a
mis-detected container silently returns _less_ than the naive walk.

**Promote when** already editing `htmlToText` for another reason, or if a site is found where the
boilerplate share is large enough to change a fetch's usable content. Do **not** schedule it as a
context-budget measure — **P71.5** is that measure, and it is worth roughly twenty times as much.

Priority: Tier 4 — S. Confirmed small. Do not schedule.

### P71.13 — Aegis could manage its own SearXNG container instead of only pointing at one

**Filed 2026-08-19**, out of the P71.2 discussion. `provider: searxng` (`websearch_providers.go:111`)
already exists and works — verified live against a user-hosted instance at `10.0.0.2:8787`, which is
now this repo's own `search.base_url` in `.aegis/config.yaml`. What doesn't exist is Aegis standing
one up itself, the way `internal/sandbox`/`aegis security build-image` already manage the scanner
containers' lifecycle (pull, run, health-check, teardown).

**Weighed against just recommending Tavily, and it is not a clean win — record why before building
it.** A self-hosted SearXNG proxies out to the same upstream engines (Google, Bing, Brave, DDG) a
zero-config scrape already hits, so it does not remove the challenge-page failure mode **P71.1**
detects — it moves that risk one layer down, into a container Aegis would now be responsible for,
and a datacenter/CI host's IP is _more_ likely to get blocked by those upstreams than a residential
one. It also introduces a hard container-runtime dependency for a feature that is currently
zero-infra (`go build` needs none — see CLAUDE.md), which is a bigger ask than the scanner containers
make, since those are opt-in security tooling rather than a chat-loop dependency.

**Do, if picked up:** scope it as strictly opt-in (`search.provider: searxng` with no `base_url` set
could trigger a "manage one for me" prompt, never a silent default), reuse the sandbox package's
container lifecycle rather than inventing a second one, and pin an engine list known not to earn an
instant block (Mojeek/Startpage/Brave over Google/Bing) — cross-check against **P71.2**'s untested
candidates, since a self-managed SearXNG and a direct scrape of the same engines are solving the same
problem two ways. Document the honest tradeoff (still dependent on the same upstreams, now with
container-ops cost) rather than pitching it as escaping "external parties" — it doesn't.

Priority: Tier 4 — M. No fired trigger; a real second search backend (**P71.2**) and confirming this
repo's own bring-your-own-instance config work were both dependencies-in-spirit, not blockers, and
both are now satisfied.

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
planned. The user chose to act on a prioritized subset (2026-08-25) rather than wait for a trigger;
**GAP-02, GAP-03, GAP-08, and GAP-09 have shipped** (see below). GAP-05 was spun out to its own
future item, **P77.6**, rather than attempted in this pass. **GAP-04** (git support stops short of
branching, `internal/worktree` exposes no tool at all) and **GAP-07** (the MCP server side lags the
mature client — no `resources/*`, `prompts/*`, `sampling/*`, or `notifications/*`) remain open,
unpromoted.

- ~~GAP-02: no log rotation and no size cap~~ — `internal/logging` now rotates `aegis.log` at a
  configurable size (`log.max_size_mb`/`log.max_backups`, default 20MB/5 backups).
- ~~GAP-03: diagnostics have exactly one caller, nothing feeds back after an edit~~ — `write_file`,
  `edit_file`, `multi_edit`, `edit_section`, and `fill_marker` now fold LSP diagnostics for the
  changed file into their own result when a server is configured for it (`appendLSPFeedback`,
  `internal/tool/builtin/lsp.go`).
- ~~GAP-08: no test-runner feedback loop as a first-class concept~~ — a new deferred `run_tests`
  tool (`internal/tool/builtin/tests.go`) auto-detects the project's test command and parses
  go/pytest/jest/cargo summary output into structured pass/fail counts and failing test names.
- ~~GAP-09: structured outputs wired but used at exactly one call site~~ — `guard.LLMGuard`
  (`internal/guard/guard.go`) is now a second use of `provider.Request.Format`: it asks for (and,
  on a backend that honors Format, is constrained to) a `{"verdict":...}` JSON reply, tried first
  and falling through unchanged to the pre-existing text-heuristic `parseVerdict` on anything else —
  additive, not a rewrite of the tuned local-model parsing.

Priority: Tier 4 for the two remaining items (GAP-04, GAP-07) — no triggers. Do not build
speculatively.

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

### P60.3 — Checkpoints capture files only, so `/rewind` is silent about everything else

`internal/checkpoint` snapshots each file a write tool touched (capped at 16MiB) and rewind writes
those contents back — correct within its documented scope. Rewinding a turn that ran `pip install`,
applied a DB migration, started a background process, or wrote a >16MiB artifact restores the source to
pre-turn state while leaving the environment in post-turn state, and the user is told the turn was
undone. If a session owns a persistent container (P60.2, shipped 2026-08-05), a checkpoint could be a
container snapshot/commit instead, making rewind honest about installed packages and process state.

**Re-verified 2026-08-06:** the note here previously said `sandbox.backend` "still defaults to
`local`" — that was stale even then; the actual default at the time was `"os"` (P4.7), not `local`.
Either way, `"os"` isn't `"container"`, so the promote condition was unmet.

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

### P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)

`newLoopDetector` is constructed inside `Run`, so its window resets every call. In the TUI and web UI
each user turn is a separate `Run`, so a model that loops _across_ user turns (re-reading the same file
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

### P67.10 — Four seams the tool interface does not have

`tool.Tool` is deliberately rendering-agnostic, and that is what lets one registry serve the TUI, the
web UI, ACP and MCP. Nothing here changes that. Four _optional_ seams are missing, each small on its
own and none currently blocking anything:

- **A per-tool equivalence predicate.** Loop detection currently normalizes call signatures centrally,
  with two per-tool opt-outs beside it — `PollExempter` (hide the call entirely) and
  `SignatureTransparent` (hide only its arguments). A tool-supplied "are these two inputs the same
  call" predicate is the third member of that family and is a better factoring for tools whose inputs
  are equivalent in ways a generic normalizer cannot see. If built, it goes in the same tests that
  keep the existing two sets narrow and disjoint.
- **Destructive as an axis distinct from write.** `tool.Capability` distinguishes read from write from
  execute, but not reversible writes from irreversible ones (delete, overwrite, send). The permission
  layer currently has no way to prompt harder for the second kind.
- **Interrupt behavior.** When the user submits a new message while a tool is running, the choice is
  cancel-and-discard or keep-running-and-queue. Aegis applies one answer to every tool; a long
  `shell` build and a two-second `read_file` do not want the same one.
- **A search hint for deferred tools.** `<deferred_tools>` prints `tool.Summarize`, which serves both
  the prompt budget and discoverability with one string and is therefore optimized for neither. A
  short keyword line, separate from the summary, would let a model find a deferred tool by capability
  rather than by name.

**Promote when** one of them is actually needed: a loop the current detector misses (predicate), a
destructive tool auto-approved where it should not be (destructive axis), a user complaint about
interruption (interrupt behavior), or a measured failure to find a deferred tool (search hint). Do not
build all four together; they are related only by living on the same interface.

Priority: Tier 4 — no fired trigger. Do not build speculatively.

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

### P67.14 — Hand-composed ANSI has no rule about transitions versus state

Small, and filed as a discipline note rather than a feature. Where Aegis emits escape sequences
directly — kitty graphics chunking in `internal/tui/imagerender.go`, the stripping and rewriting in
`internal/termsafe` — there is no stated rule distinguishing sequences that express **state** from
sequences that express a **transition**.

The distinction has teeth. Style sequences computed as a diff from the previous style are transitions:
two adjacent ones may be concatenated but the earlier one may never be dropped as redundant, because
its reset codes are not guaranteed to be a subset of the later one's — and a dropped background reset
leaks into the next erase via background-colour erase. State-setting sequences (an absolute cursor
position, an explicit colour) can be collapsed to the last one freely. Optimizations that are correct
for one class silently corrupt the other, on one terminal, months later.

Write the rule down where those sequences are composed. **Promote to real work when** Aegis gains a
frame-diff or output-batching layer that would be tempted to dedupe them — until then the comment is
the whole deliverable.

Priority: Tier 4 — no concrete trigger, XS. A comment, not a feature.

**P78.1**–**P78.9** (filed and shipped 2026-08-26, from a five-track code-quality/sprawl/duplication
audit of the whole tree) — full record: [releases.md](releases.md#p781-p789-shipped-2026-08-26).

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

**What would close it.** Support a shared secret or bearer header for the local provider endpoint and
document configuring Ollama to require it. Support a Unix domain socket (or Windows named pipe)
endpoint, which removes the port-binding race and the capture exposure together. Allow TLS with a
pinned certificate to a local provider, reusing the pinning machinery `client.NewFromConfig` already
implements.

**Why Tier 4 rather than higher.** The prerequisite is another local account, or a hostile process
already running as someone on this host, on a single-user development machine — and Ollama itself has
no authentication to configure against, so half the remediation is upstream. Promote if Aegis is ever
run on a shared or multi-user host, which is also the condition that promotes **P81.24**'s encryption
half.

Priority: Tier 4 — real mechanism, no fired trigger on a single-user box, and partly gated on what the
local model server supports.

### P81.18 — The self-signed certificate warning conditions operators to click through (FIND-18)

**Filed 2026-08-31**, from the threat model
([**FIND-18**](../threat-model-20260831-002123/3-findings.md#find-18-the-self-signed-certificate-warning-conditions-operators-to-click-through),
CVSS 4.3, `Low`, CWE-295, `Transfer Risk`). Every in-repo client pins the daemon's self-signed
certificate through `client.NewFromConfig`, so the pin is transparent for the TUI, ACP and MCP paths.
The browser is the one consumer that is not pinned: opening `aegis ui` with TLS on produces a
certificate warning the operator must dismiss. The CLI calls this out explicitly, which is the right
handling. The residual effect is that the operator learns to click through certificate warnings on
this origin — including one presented by something that is not the daemon.

Narrow on loopback. It widens the moment the UI is tunnelled or proxied, which is exactly the
configuration where the warning would have meant something.

**What would close it.** Document in `docs/installation.md` a supported path for tunnelled or proxied
UI access using `cert_file`/`key_file` with a certificate the operator's browser already trusts. Offer
a helper that adds the generated certificate to the OS trust store on request, so the default local
experience has no warning to normalise. Print the certificate fingerprint in the CLI output so an
operator who does click through has something to compare against — which is also what **P81.25** needs
for its pin-change acknowledgement.

Priority: Tier 4 — mostly documentation and a convenience helper, no fired trigger while the UI stays
on loopback. Promote when someone actually tunnels it. The fingerprint line is small enough to take
with **P81.25**.

### P81.28 — Prose tool-call parsing can promote quoted untrusted text into real calls (FIND-28)

**Filed 2026-08-31**, from the threat model
([**FIND-28**](../threat-model-20260831-002123/3-findings.md#find-28-prose-tool-call-parsing-can-promote-quoted-untrusted-text-into-real-tool-calls),
CVSS 5.4, `Moderate`, CWE-1427). `internal/provider/prosetoolcall.go` and `internal/toolshim` exist
because some local models emit tool calls as free-form text rather than structured calls, and they
recover those — P74.8's whole point. What the parser cannot do is distinguish a call the model
_intended_ from a call the model merely _quoted_, and untrusted content that reaches model context is
frequently quoted back verbatim in a summary or an explanation.

**The shim is off by default** (`provider.tool_call_shim: off`), which is what keeps this narrow and
what makes it Tier 4. It becomes reachable the moment an operator enables it for a local model — which
is exactly the population the local profile targets, so the trigger is plausible rather than exotic.

**What would close it.** Never run the prose parser over a span of model output that reproduces
content which arrived inside an untrusted-content wrapper, tracked by content hash for the turn — the
same taint bookkeeping **P81.1** needs, which is the real reason to sequence this after it rather than
build a second mechanism. When the shim recovers a call, surface it in the approval prompt as
"recovered from prose" so the operator sees the provenance of what they are approving. Keep the shim
off by default and document the injection interaction in `docs/local-model-tuning.md`.

Priority: Tier 4 — off by default, and the containment it wants is **P81.1**'s to build. The
documentation half and the "recovered from prose" prompt label are separable and cheap.

### P81.31 — Checkpoint growth is unbounded and `/rewind` can silently discard outside edits (FIND-31)

**Filed 2026-08-31**, from the threat model
([**FIND-31**](../threat-model-20260831-002123/3-findings.md#find-31-checkpoint-growth-is-unbounded-and-rewind-can-silently-discard-non-agent-changes),
CVSS 4.4, `Low`, CWE-770). Checkpoints are per-turn file snapshots taken before mutating calls. On a
large workspace across a long session they grow with no documented bound and can fill the disk.
Separately, `/rewind` restores those snapshots over the live working tree — a legitimate and useful
feature that can also silently revert changes the agent did not make, including a reviewer's edits
made in another editor while the session was open.

Neither is severe alone. Together they mean the restore path is both unbounded in cost and unconfirmed
in effect.

**What would close it.** Cap total snapshot bytes per session, evict oldest-first, and surface the cap
as a config key. Before a rewind, compare current file state against the snapshot's recorded post-turn
digest and require confirmation for any file changed outside the agent's own tool calls. Reap
checkpoints when a session is archived or pruned — shared with **P81.24**'s retention work, and the
cheapest way to get this one moving.

**Related and not the same.** **P60.3** (Tier 4) is that checkpoints capture files only, so `/rewind`
is silent about everything else. That is a coverage gap; this is a bound and a confirmation. Take them
together if `internal/checkpoint` is open.

Priority: Tier 4 — no fired trigger (no report of a filled disk or a lost external edit), and the
reaping half rides along with **P81.24** for free.

### P81.11 — The merge gate exists, passes, and does not run — accepted risk (FIND-11)

**Filed 2026-08-31 as Tier 1; re-tiered to 4 the same day, on an operator decision.** The threat model
([**FIND-11**](../threat-model-20260831-002123/3-findings.md#find-11-build-test-vulnerability-and-lint-gates-no-longer-run-on-push-or-pull-request),
CVSS 6.3, `Important`, CWE-693) read `ci.yml`'s `# Temporarily disabled (non-security pipeline)`
comment at face value and filed the fix as "uncomment two trigger lines." Its own "Needs Verification"
table hedged the same entry: _was the disablement deliberate and permanent?_ **It was.** The operator
confirmed it on 2026-08-31. The triggers stay off. This is an accepted risk, not a pending fix, and it
is kept here rather than moved to [releases.md](releases.md) because a residual question is genuinely
open — see below.

**What is actually switched off**, stated plainly so the acceptance is informed rather than implicit.
`ci.yml` builds on three operating systems and runs `go test -race ./...`, gofmt, `go vet`,
`govulncheck ./...` (blocking), `staticcheck ./...` (blocking), and the web UI `dist/` drift check.
`release.yml` is disabled the same way. Verified 2026-08-31: **`codeql.yml` is the only workflow in
this repo with live triggers** (`push`, `pull_request`, and a weekly `17 3 * * 1` cron), and
`nightly-eval.yml` is `workflow_dispatch`-only for its own separate and well-documented reason. CodeQL
covers part of the static-analysis surface and **none** of the test, vet, vulnerability, format or
drift checks.

**Why the acceptance is defensible.** This is a single-maintainer project with a strong local
verification culture — every shipped item in [releases.md](releases.md) was closed against a
live-verified test run on this machine, never against a green CI badge, and that standard is stated
twice in this document. A merge gate on a repository whose only committer already runs `go test ./...`
before shipping buys less than it does on a team. The Actions cost of a three-OS race matrix per push
is real and the benefit is partly duplicated.

**Why it is not free, which is the part worth re-reading in six months.** The `govulncheck` step was
added because "a project that ships a vulnerability scanner had never scanned its own toolchain", and
it found seven stdlib CVEs on the pinned toolchain the day it landed. That class of finding is exactly
the kind a local pre-ship habit misses, because it depends on the _world_ changing rather than on the
code changing — a new CVE lands against an unchanged toolchain and nothing locally prompts a re-check.
The weekly CodeQL cron is the shape of the answer; `govulncheck` has no equivalent today.

**The residual question, and the only open work here.** Three checks lost their home and are worth
different answers:

- **`govulncheck`** — time-dependent, not change-dependent. Strongest candidate for a scheduled
  workflow of its own, mirroring `codeql.yml`'s weekly cron. Cheap (one OS, no matrix, seconds) and it
  catches the thing local discipline structurally cannot.
- **`staticcheck`, `gofmt`, `go vet`, `go test -race`** — change-dependent, and genuinely covered by
  the local pre-ship habit. Reasonable to accept outright.
- **The `dist/` drift check** — now **P81.17**'s entire content rather than a freebie; see that entry,
  which was rewritten when this decision landed.

**Also affected by this decision, all updated 2026-08-31:** **P81.17** (drift check, no longer
subsumed by this item), **P81.12** (its release-signing half is parked behind a product decision about
releases; its `codeql.yml` action-SHA pinning half is split out as Tier 2 — XS, and is the one
supply-chain fix in the batch touching a workflow that actually runs), and **P81.20** (its
"run `FuzzClassifyShellCommand` in CI" step has no pipeline to be restored to).

**What would close this entry:** a decision on the `govulncheck` schedule — build it, or accept its
absence in writing. Either closes the item; leaving it undecided is the only outcome that does not.

Priority: Tier 4 — the risk is accepted by explicit operator decision and needs no code. What remains
is one scheduling question, with no fired trigger behind it. Do not re-open this as a fix without
re-asking the operator, whose answer is recorded above.

---

## Verification Work

**Status: 9 open** — **P80.4** was filed 2026-08-30 when the `live_workflow` tier was run for the first
time: the run itself closed the audit's C2 entry and produced three shipped fixes, leaving two
standalone tests that decline to report because the model available here will not follow the fixture's
14-file chain. The other 8: (**P68.4**, **P68.5** and **P68.6** filed 2026-08-17; **P68.2** and **P68.3** both
filed _and closed_ 2026-08-17, their records moved to
[releases.md](releases.md#the-template-that-ate-the-tool-calls-2026-08-17) and
[releases.md](releases.md#the-tiers-task-was-a-boolean-2026-08-17-p683) respectively;
**P65.3** closed 2026-08-16, its record is likewise in [releases.md](releases.md)). Every
item here has its code already written and merged — nothing below is a design or implementation task.
Each is closed by running a live-model harness and recording the result the item's closure condition
names, not by writing more code. They are **not tiered**: tiering answers "how urgent is this build,"
and there is no build left to prioritize.

**The 2026-08-16 sitting changed how these should be scheduled.** They were listed as four items
sharing one harness plus P62.8 waiting on hardware. After running it: the shared-harness premise
holds only for what the tier can _observe_, and four closure conditions (LLM-03, LLM-10, ARCH-04 and
P65.2) turned out not to be observable there at all — they needed **P68.1** first, which shipped
2026-08-22 (a session id that survives the test, plus `aegis sessions trace <id>` now printing the
compaction summary text, the calibration sample count and each turn's stop reason). Those four are
now observable whenever the next live sitting runs; nobody has judged them against real evidence yet.
P38.1 needs a permission rather than a schedule slot, and P62.9 needs a better task rather than more
runs. **This whole track is parked at the one remaining row of [Up next](#up-next) by choice**;
what is written below each item is what the run established, so a future sitting starts from evidence
rather than from the pre-run plan.

**P68.2 — The stock Qwen3 chat template deletes tool calls from history — filed and closed
2026-08-17.** Full record, measurements and the shipped detector (`ollamainfo.TemplateDropsToolCalls`)
are in [releases.md](releases.md#the-template-that-ate-the-tool-calls-2026-08-17). Kept here only
because it still hands something to open work: it's why **P62.9** needs a task replacement rather than
another run of the same one (a 6-run control arm and a concrete reason the 2026-08-16 failures weren't
purely competence), and why **P52.16**'s `toolResultEcho` measurement — taken on the affected
`qwen2.5-coder:1.5b` — is worth a cheap re-run nobody has done yet.

### P80.4 — `live_workflow`'s two standalone tests both need a stronger model than this machine has run

**Filed 2026-08-30**, from the audit's **C2** entry finally being executed. The tier had never been run;
running it three times found three product defects (**P79.2** the daemon released nothing unless it
exited through `ListenAndServe`; **P79.3** compaction's summarizer spent its whole budget on a thinking
preamble and returned empty on every cycle; **P79.4** the empty-answer nudge re-asked over the channel
that had just swallowed the answer), all fixed and re-verified live the same day — record in
[releases.md](releases.md#comprehensive-architecture-and-security-audit-remediated-in-full-2026-08-30).
`TestLiveWorkflow`'s four subtests now pass against `aegis-qwen35-9b:32k`, `SecurityTriage` at 12/12
where it scored 3/12 before P79.4.

**What is left is not code.** The two standalone tests in that file decline to report rather than
passing vacuously, and both declines are about the model:

- **`TestLiveWorkflowCompactionPrefixCacheGate`** had two complaints and now has one. "No compaction
  actually ran, so this run measures nothing about the gate" is gone — P79.3 means compaction runs and
  succeeds, three cycles per arm (62%, 78%, 79% fill). What remains is that this model abandons the
  fixture's 14-file read chain after five files, so the conversation never grows as designed and the
  test refuses to report a gate comparison from it. It needs a model that will follow a long mechanical
  chain. Note this is the _small_-window arm; **P62.8**'s large-window regime is a separate, still
  hardware-blocked question against the same test.
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

---

### P66.22 — The LLM-tier findings are all estimates; one live run converts them to measurements

The P66 review never ran a live model. **LLM-01, LLM-02, LLM-03, LLM-10 and ARCH-04 are all claims
about runtime behaviour against a local model, argued entirely from source.** The arbitration upheld
all five and they are well-argued — but CLAUDE.md is emphatic that this class of claim is settled by
measurement, and this document has twice recorded a fixed instrument _inverting_ an already-acted-on
verdict.

One `TestLiveWorkflow` run against `qwen3:14b-32k` answers all of them, and it is the same harness
P38.1, P62.9, P65.2's prompt half and P65.3's local half already need — so this costs no additional
setup if scheduled with them. That bundle is the one remaining row of the [Up next](#up-next)
ten, and it was one row precisely because running the harness without recording all five wastes the
setup. _(It ran on 2026-08-16 — see below for what that premise turned out to be worth. The remainder
is now row #6, parked.)_

**It was scheduled on 2026-08-16 and did not run: no model server was reachable** (nothing listening
on `:11434`). Nothing about the item changed — it is a measurement, so there is no partial credit and
nothing to substitute for it. Both of its gates shipped that day instead.

**It ran later the same day against `qwen3:14b-32k`. Three of the five closure conditions are met;
two are not observable from this tier.** Full record in [releases.md](releases.md) (_The live-tier
sitting, 2026-08-16_):

- **LLM-01 — met.** Local profile 4,871 provider-reported first-turn tokens against 8,393 default,
  neither clamped at the 16,384 window. With a realistic over-cap `CLAUDE.md`, the deterministic
  budget measures 6,383 estimated tokens against a 6,650 ceiling — the 11,611-token figure this item
  was filed on is three fixes stale. The same prompt costs **5,775 / 9,591** on
  `aegis-qwen35-9b:32k`: the ceiling is in `tokenest` units, not in any tokenizer's, and ~19% spread
  between two local models is normal rather than a regression.
- **LLM-02 — met, and it found the _next_ question.** Compaction fires exactly where the shared
  trigger says (85% of 24,576 ≈ 20,889). What it does after that is the finding: **eleven
  compactions in fifteen turns, each summarizing two messages and leaving the context at ~90% full**,
  so every subsequent turn re-crosses the trigger. Prefill quadruples at the first compaction and
  stays there. P62.7's minimum-yield rule suppressed none of the eleven — read that before **P67.6**.
  **Reproduced identically on a second model** (`aegis-qwen35-9b:32k`: same 11 compactions, same
  11→9, prefill 2.3s → 9.2s), where it settles at ~96% full rather than ~90%. It is a property of
  compaction's yield, not of one model.
- **LLM-03 — not read directly.** The fix is in and the path is right; `estimated=false` on every
  `done` event and estimates tracking served counts to ~11% are consistent with a calibrating
  session, but the sample count itself lives in a session trace, and the live-tier daemons delete
  their data dirs on cleanup.
- **LLM-10 and ARCH-04 — not observable from this tier at all.** Both want `aegis sessions trace
<id>` against a surviving data dir. Closing them needs a harness change (keep the data dir, read
  the trace) or a hand-run session, not another workflow run.

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

**Both gates are now closed.** P66.7 and P66.14 both shipped 2026-08-16 — the reason to sequence
behind them was that they change three of the five numbers, and measuring the pre-fix state answers a
question nobody will have afterwards. Use `-count=1` — a re-run without it replays Go's cached verdict,
which this document has been caught by before.

**P66.11 shipped too, so the instrument is in place.** `TurnTrace` now carries the stop reason, the
compaction event (applied / summarized / suppressed, tokens freed, and the estimate and trigger the
decision was made on), the guard verdict, the correctives the engine injected, and a run id — LLM-02's
and ARCH-04's closure conditions restated as a struct. Read it with `aegis sessions trace <id>`, whose
`WHY` column renders exactly these, or from the JSON export for the full record.

**Two of the five expectations above have already moved, and the item's own text is now the pre-fix
statement rather than the prediction:**

- **LLM-02** is fixed rather than merely measurable: one shared trigger means the two gates cannot
  differ, so what a live run now measures is _whether the shared number is the right one_, not whether
  the two agree. The 2,048-vs-3,277 disagreement it describes no longer exists.
- **LLM-03** is fixed: the calibration gate is now a positive backend identification, so it fires on
  the `openai` + `:11434/v1` path. The run should confirm a non-zero sample count rather than
  discovering there is none.
- A **third expectation is retired by a side effect**: the prune-thrash the P62.7 minimum-yield rule
  rate-limits was a _consequence_ of the LLM-02 disagreement, and on the P62.7 fixture it disappears
  entirely once the trigger is shared. A run that was expected to observe it should not.

Priority: Verification — one run, five answers. Both gates shipped 2026-08-16; needs only a reachable
model server.

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

**P68.3 — The tier's task was a boolean, so it could not rank anything — filed and shipped
2026-08-17.** Full record, the grading rubric and the measured separation
(`aegis-qwen35-9b:32k` 10.7/12 vs `qwen3:14b-32k` 2.7/12, complete at n=3) are in
[releases.md](releases.md#the-tiers-task-was-a-boolean-2026-08-17-p683). Kept here only because later
items build on it: it shipped `TriageTask` (`internal/eval/triagetask.go`) as the tier's ranking
instrument, kept `SeededBugTask` as the control, and is the closure condition **P62.9** and **P68.4**
both cite.

### P68.4 — The triage rubric's measuring band sits below the strongest local model

**Filed 2026-08-17, from a temperature A/B that measured nothing — twice.** P68.3 shipped a task that
ranks _models_ well (9b 10.7 vs 14b 2.7, complete separation at n=3). The attempt to use it for the
next question — do the sampling parameters `docs/local-model-tuning.md` recommends actually help? —
found it cannot rank _configurations_, because both available substrates sit against a rail:

| substrate             | temp 0.2      | temp 0.6   | reading                                        |
| --------------------- | ------------- | ---------- | ---------------------------------------------- |
| `aegis-qwen35-9b:32k` | 12, 12, 12    | 12, 12, 12 | **ceiling** — rubric exhausted                 |
| `qwen3:14b-32k-fix`   | 3, 3, 3, 3, 3 | 3, 3, 3    | **pinned low** — one repeated minimal strategy |

Both arms of both A/Bs are flat, and **neither is evidence that temperature does not matter** — a
saturated instrument returns exactly this pattern whether the variable matters or not. Reading these
as a null would be the same error as reading P68.2's 0/6-against-2/6 as a win, in the other direction.

Two instrument checks were run before concluding, and both came back clean, which is what makes the
"no headroom" reading the surviving one rather than a guess: the derived Modelfiles differ **only** in
`temperature` (`ollama show` confirms `num_ctx` and everything else carried), and all four derived
models still carry the **corrected** chat template (the `FROM <derived model>` inheritance was the
obvious way for this to be a silent P68.2 regression rather than a real result).

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
- P62.9 has an unresolved watch item about exactly this class of detour, and its `tool_search` signal
  has now been unobserved at n=5 across two sittings.

**Both models were removed from the machine on 2026-08-17**, so this is not reproducible locally
without re-pulling `qwen3:14b` — worth knowing before someone plans a sitting around it. The
behaviour is recorded in enough detail above to be recognised if it recurs on another model, and
whether it is Qwen3-specific or general is itself now an open question.

**What would close it:** read one such run's trace — **P68.1** (shipped 2026-08-22) means a future run's
data dir can now survive and be read with `aegis sessions trace <id>` — and establish whether the
model ever attempted a write tool and failed, or never selected one. Those are an Aegis problem and a
model problem respectively, and the run as recorded cannot tell them apart.

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

**Superseded in part by P68.3, 2026-08-17.** The second half of the sentence below is the half that
was right, and it has now been built: `TriageTask` is graded out of 12 and separated two models
completely at **n=3** (10.7 vs 2.7), where this task returned p ≈ 0.45 at n=6. Re-running _this_
task at n≥10 would buy a tighter estimate of the wrong quantity, exactly as recorded below.

**What would close it:** the same task at n≥10 per arm, or a task whose edit is unambiguous enough
that a single run means something, plus a default-prose control for the prose-attributable failures
above. Both are runs, not code.

**A second model closes the first watch item, 2026-08-16.** On `aegis-qwen35-9b:32k` the whole
`TestLiveWorkflow` tier passes, including the seeded-bug task — the first time it has been solved on
this tier. With `edit_file` deferred, the model went straight to `multi_edit` (5 tool calls, 13.8s,
no `tool_search`, no detour). The guard arm solved the same task the long way and is the better
record: `edit_section` errored, `multi_edit` errored, and the model re-read the file and got the next
`multi_edit` right — recovery in two calls, no breaker trip. **The deferred surface is reachable and
the compressed prose holds; what is unmeasured is now only turn _cost_ against an exposed-`edit_file`
control.**

**Two runs on `qwen3:14b-32k` the same day argue for replacing the task rather than repeating it.** Neither touched `tool_search` — the detour this item watches for is now unobserved at n=5
across two sittings. But both failed the task: one rewrote `temps.py`, re-ran it, and reported a
confidently wrong average; the other ran the script once, read the `TypeError`, and stopped without
editing anything. With the 2026-08-14 control arm failing outright twice as well, **the seeded-bug
task is measuring model competence, not tool reachability** — n≥10 on it would buy a tighter estimate
of the wrong quantity. Replacing the task is the cheaper close. Record in
[releases.md](releases.md).

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
_generation_ and structured fill is _completion_, and every measurement in the P38.x line says local
models degrade on the first and hold up on the second — this is the last unstructured-prose ask left in
the engine, at the moment the model's context is fullest.

**Promote when:** P38.1's re-run is done and the live tier is free — the prompt change wants the same
harness, so running them together costs one setup instead of two.

**2026-08-16: the harness cannot see what this item needs to judge.** The live tier ran twenty-two
compactions across the two P62.2 arms — the skeleton prompt was exercised repeatedly — but a
compaction's _summary text_ never reaches the SSE stream, so the run reports that compaction happened
and nothing about what it kept. Judging skeleton-fill against terse-bullet output needs the summary
itself: either a session trace from a run whose data dir survives, or a notice/event carrying the
summary. That is a small harness change, and it is now this item's real blocker rather than tier
availability.

Priority: Verification — real value, unblocked, code already built, gated on live evidence rather
than on design.

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
