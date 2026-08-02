# Aegis Capability Roadmap

**Last updated:** 2026-08-02

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items (15).** The **P55.x container-only-scanning batch** was filed 2026-08-02 off a full
functional test of the multiscanner container and a review of method resolution across all 17
registered scanners — **9 items**, and the first Tier-1 work this file has carried since the P52.x
batch: **P55.1** (image/source drift), **P55.2** (all-or-nothing `update-db`), **P55.3**
(`verify-image` smoke test) in Tier 1; **P55.4** (container-first resolution), **P55.5** (global
pin by default), **P55.6** (DB-age surfacing) in Tier 2; **P55.7** (`aegis-netscanner`, split by
mount posture) and **P55.8** (gosec two-phase) in Tier 3; **P55.9** (relevance gating for the
always-on scanners) parked in Tier 4. The batch's strategic goal is that a user installs **one
image instead of 17 tools**; its Tier-1 half is the integrity work that has to land first, because
container-only makes a broken container the whole product rather than a degraded path. Full origin
note and test evidence in the Tier 1 header.

**The 6 pre-existing items**, unchanged by the P55.x filing and described below in their own
tiers. Before P55.x landed, Tier 2 held **P38.1** only and Tier 3 was empty — **P53.6** (non-native tool-calling
shim) shipped 2026-08-02, closing the **P53.x local-LLM comparative-review batch** at **0 open of
6**. The batch's confirmed capability gap and highest-value item is now built: Aegis had been
detecting a model that writes tool calls into its prose and discarding the signal, and
`provider.tool_call_shim: on` now serves the tool schemas in the system prompt and parses tagged
JSON back into real tool calls — opt-in only, through the same permission gate as a native call,
with a parser that declines a malformed attempt rather than repairing it. The rest of the batch —
**P53.1** (stale `WithKeepAlive` comment), **P53.2** (loop detector: polling exemption +
differentiated outcomes), **P53.3** (compaction summarization-call headroom), **P53.4** (probe
conformance rate) and **P53.5** (persisted per-model capability records) — shipped 2026-08-01/02.
All six write-ups are in [releases.md](releases.md).

**One follow-up P53.6 deliberately left unfiled-as-built:** auto-engaging the shim off a low P53.4
conformance rate. The plumbing exists (the rate is persisted per model via
`modelcaps.Store.ToolCalling`), and the shim's config key rejects `"auto"` rather than accepting it
as a no-op precisely so the word stays available. It is worth taking only once the rate is
trustworthy — i.e. once live runs show it predicting drive outcomes — and not before.

Tier 2 also: **P38.1** — the threat-model conformance umbrella. Mechanism
(recon → scaffold → incremental fill, no orchestration mis-route) is live-confirmed repeatedly;
conformance (a live unattended run reaching a verify-clean suite) is still unmet. Stays open as
live-run verification tracking, not independent build work — see its body below for the full
re-test history. Tier 4, all measure-first or parked with no build trigger yet: **P52.14**
(session-scoped loop detector — reviewed 2026-08-01, see below), **P52.16** (native Ollama
tool-result disambiguation for parallel same-tool calls), **P49.3** (LSP-backed repo-map symbol
precision), **P49.4** (LLM-summarized repo-map concept nodes), **P25.9** (per-session scoping of
the last daemon-singleton service, `lsp.Manager` — parked pending a concrete multi-tenant need).

**Everything else has shipped or closed.** The **P52.x full-stack review batch** (filed
2026-07-30) closed 15 of its 17 items between 2026-07-30 and 2026-08-01 — **P52.15** (wall-clock
run budget) shipped and **P52.17** (auto tool-calling probe on model switch) closed as
already-implemented were the last two, leaving **P52.14** and **P52.16** as the batch's only open
items. Also shipped/closed since the last cleanup: **P51.1** (macOS seatbelt profile), the
**P50.x** phased-drive determinism batch, the **P49.x** repo-map batch head (**P49.1**/**P49.2** —
**P49.3**/**P49.4** remain open above), the entire **P47.x** phased-drive stability batch,
**P48.1** (config-test hermeticity), and **P38.8** (external per-phase threat-model wrapper —
superseded once its mechanism shipped in-harness for P38.1, see releases.md). Full write-ups for
all of it are in [releases.md](releases.md).

**2026-08-01 cleanup.** This file had drifted from its own stated contract — full SHIPPED/CLOSED
write-ups for the P52.x/P51.1/P50.x/P47.6/P47.10 items had accumulated here instead of in
releases.md. Moved them out; no content was changed, only relocated. While auditing, one stale note
was caught and corrected: a "content-substance check routing" follow-up mentioned alongside the
P52.7/P52.8 write-ups (never given its own `P<n>.<m>` number) was still being described as
outstanding. It isn't — file-aware routing (`perFile`, `fileOwnerPhase`) shipped as part of
P52.12's move into `internal/drive` (`internal/drive/drive.go:837-878`); there was no separate gap
to track.

---

## Tiering Criteria

**Tier 1** = real, currently-exploitable security/robustness gaps, small effort, no dependency.
**Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained hardening.
**Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other work).
**Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build speculatively.

---

## Open Work — Tier 1

**Status:** 3 open — **P55.1**, **P55.2**, **P55.3**, the Tier-1 half of the P55.x
container-only-scanning batch (see the batch origin note below). See [releases.md](releases.md)
for the full Tier-1 history (P52.1, P52.2, P51.1, P50.1, and the P47.x batch head).

**P55.x batch origin.** A full functional test of the multiscanner container (2026-08-02) against
a purpose-built multi-language vulnerable fixture, plus a review of `internal/security`'s method
resolution across all 17 registered scanners. The headline is that the container's *scanning* is
sound — 14/14 bundled tools execute offline, and detection is genuinely good (trivy 59 vulns / 26
misconfigs / 5 secrets, osv-scanner 59 offline, gitleaks 5/5 planted secrets, bandit 6, hadolint 6,
opengrep 7, end-to-end `aegis scan` 173 findings with cross-tool dedup and ASVS tagging intact).
What the test found instead is a cluster of **provisioning** failures, three of which share one
shape: *the scanner silently or loudly stopped working and no layer of the system noticed*. That
shape is Tier 1 because `internal/security/multiscanner.go` already names it as the thing this
design most fears — "a silent all-clear from a scanner that never looked at a database."

**Four defects found by the test are already fixed and are not filed here**, but they are the
evidence base for P55.3 and worth keeping together, because every one of them was invisible to the
checks that existed:

- **kubescape was fatally broken in container mode.** No rego policy library was baked, so
  `--network none` gave `open $HOME/.kubescape/allcontrols.json: no such file or directory` —
  fatal, exit 1, empty stdout. Fixed by baking `kubescape download artifacts` into the Containerfile
  *and* naming an explicit framework in the container invocation (0 → 24 findings on the fixture).
  The second half is load-bearing and non-obvious: bare `kubescape scan <dir>` defaults to the
  `workloadscan` framework, which fails from a baked cache with `framework from file not matching`
  **even when workloadscan.json is present**. Hand-fetching the release assets does not work either
  — their names and contents differ from what the offline loader reads back.
- **kubescape's SARIF was unparseable even once it ran.** The invocation used
  `--output /dev/stdout`, but kubescape writes its human summary table to stdout as well, so the
  report interleaved with box-drawing characters and the parse died on the first non-JSON byte
  (`invalid character 'â'`). Fixed by writing to a file in the container and `cat`-ing it.
- **njsscan was broken by the semgrep removal (30d4671).** njsscan doesn't just import its engine,
  it shells out to a `semgrep` binary by name; the `/usr/local/bin/njsscan` symlink put njsscan on
  PATH but not its venv's siblings, so the lookup escaped to the system PATH and only ever resolved
  because a standalone semgrep happened to be there. Removing that scanner left njsscan dying with
  `FileNotFoundError: 'semgrep'` on every run, with its own correctly pinned `semgrep==1.86.0`
  sitting unreachable in `/opt/venv/njsscan/bin`. Fixed with a wrapper that prepends the tool's own
  venv bin.
- **grype was absent from the pinned image entirely** — see P55.1, which is filed because the
  *detection* gap remains even though this instance is fixed.

The njsscan case is the sharpest argument for P55.3's canary requirement: the Containerfile's
build-time `njsscan --version` check **passed for the entire duration of the breakage**, because
`--version` never reaches the semgrep subprocess. Two of these four defects survived a green
`go test ./...`, a successful image build, and a scan that reported findings. Write-ups in
[releases.md](releases.md).

The batch's strategic driver is a decision to make the container the **only** way Aegis scans, so
a user installs one image instead of 17 tools. P55.4-P55.8 sequence that; P55.1-P55.3 are the
integrity work that has to land first, because container-only means a broken container is no
longer a degraded path — it is the whole product.

### P55.1 — Detect image/source drift (the pinned image can silently fall behind the Containerfile)

The image-ID pin (`verifyMultiscannerImage`) verifies the image **hasn't changed since it was
pinned**. It cannot see that the image no longer matches the *source it was built from*, and that
gap produced a real, undetected failure: the pinned image was built 2026-07-16T21:18, and all
three multiscanner commits landed after it. The image therefore never contained `grype`, which
`fetch.sh` had since added.

The consequence chained, and every link failed quietly. `update-db.sh` runs `grype db update`
under `set -eu`, so on an image without grype the **entire database refresh aborted** — which is
why `/cache/grype` has never existed on this machine. `multiscannerDBTools` correctly lists
`grype` as needing `/cache/grype/db/6/vulnerability.db`, so container-method grype would have been
refused with a clear reason; it was masked only because grype happened to be on the host PATH.
Nothing surfaced that the pinned image was two commits stale.

Fix: record a **source fingerprint** at build time — a hash over the embedded Containerfile,
`fetch.sh`, and `update-db.sh` (the `go:embed` set is already the authoritative copy) — into config
alongside `image_id`, and compare it at scan time the way the image ID is compared. A mismatch
should report "the multiscanner image was built from an older Containerfile — re-run `aegis
security build-image`", not fail closed silently. This is the same reasoning that made the image-ID
pin replace the digest rule (README: "that verifies content, which is what the digest rule was
reaching for"); it just extends the check one link further back, to the source.

**Priority:** Tier 1 — a currently-live provisioning gap with a demonstrated silent failure, and
small: one hash written at build, one comparison at resolve. **Effort:** S.

### P55.2 — `update-db` is all-or-nothing and leaves a partially-populated cache

`update-db.sh` runs under `set -eu` with the three database fetches in sequence (trivy → grype →
osv-scanner). One tool's failure aborts the rest, and the script has no notion of partial success —
so the observed state on this machine is trivy and osv-scanner fully populated (1.2GB and 271MB,
both verified working offline) while grype has no database at all, from a single early `grype db
update` failure. The operator saw one error and a non-zero exit; nothing said "2 of 3 databases are
fine, grype's is missing."

This matters more under container-only, where a missing DB is the difference between a scanner
running and a scanner being refused. It also interacts badly with P55.1: the failure that aborted
the run was itself caused by image drift, so an operator re-running `update-db` against the same
stale image gets the same abort with no hint at the real cause.

Fix: run each tool's refresh independently, collect per-tool outcomes, and report a summary
(`trivy ok / osv-scanner ok / grype FAILED: <reason>`), exiting non-zero if any failed. Keep every
existing plausibility assertion — the "implausibly small" checks on the npm archive and grype's
`vulnerability.db` are exactly the right instinct and should stay — they just shouldn't take the
other tools down with them.

Fold in the **trivy misconfiguration policy cache**, which no step populates today. Every scan logs
`ERROR [misconfig] failed to check cache: cache does not exist at "/cache/trivy/policy/content"`
and falls back to embedded checks. Findings still appear (26 on the fixture), so this is degraded
rather than broken — but it's an ERROR-level log on every single scan, which trains operators to
ignore scanner errors, and the embedded checks are a frozen subset rather than the current bundle.

**Priority:** Tier 1 — small, self-contained shell work on a script whose failure mode is a
half-provisioned scanner set. **Effort:** S.

### P55.3 — `aegis security verify-image`: prove the built image's tools actually run

`MultiscannerTools(profile)` is a **static list**. Aegis routes a scan to the container on the
strength of that list plus an image-ID match — it never checks that the named tool is present in
the image or that it can produce a result. Both failure modes were live simultaneously: `grype` was
in the list and absent from the image, and `kubescape` was in the list, present, and fatal on every
invocation. A single smoke run would have caught both instantly; instead grype was masked by a host
binary and kubescape surfaced as an availability message that read like a configuration problem.

Fix: a `verify-image` subcommand (and a step at the end of `build-image`) that, for each tool the
profile claims, runs a version probe and a **canary scan against a tiny embedded fixture with known
findings**, then reports a per-tool table. The canary is the part that matters — a `--version` probe
would have caught grype's absence but *not* kubescape's fatal, and would not catch the more
dangerous class this codebase already documents for gosec and osv-scanner: a tool that exits clean
and reports zero because it never loaded its data. Assert a **non-zero finding count**, not exit 0.

This also gives P55.1 and P55.2 an end-to-end check to hang off, and gives container-only scanning
the provisioning gate it needs: one command that answers "is my scanner image actually good?"

**Priority:** Tier 1 — directly closes the detection gap behind two shipped-broken scanners, and
is the natural gate for the whole container-only sequence. **Effort:** M.

## Open Work — Tier 2

**Status:** 4 open — **P55.4**, **P55.5**, **P55.6** (the Tier-2 half of the P55.x
container-only-scanning batch; see the Tier 1 header for the batch's origin and test evidence),
plus **P38.1** (threat-model conformance umbrella), which is live-run verification tracking rather
than independent build work. P38.1 is no longer what `roadmap-status.sh` suggests — Tier 1 now
leads with buildable P55.x items — but it stays at the head of this tier in document order until a
live re-confirmation run happens. The **P53.x local-LLM
comparative-review batch**'s Tier-2 half (**P53.1**-**P53.4**, filed and shipped 2026-08-01) and the
earlier batch — P52.3, P52.4, P52.5, P52.6, P52.7, P52.8, P52.9, P52.10, P52.11, the full P47.x
self-contained batch, P48.1, P49.1, and P50.2/P50.3/P50.4 — have shipped; see
[releases.md](releases.md). Tier 3 is now empty (**P53.6** shipped 2026-08-02), so there is no
buildable item queued behind P38.1 — the next work is either a live P38.1 re-test or a newly filed
item.

**P53.x batch origin.** A comparative review (2026-08-01) of how six frontier harnesses — opencode,
crush, pi, aider, OpenHands, goose — drive local models, cross-checked line-by-line against Aegis.
The review's headline is that Aegis is **ahead** on the four things that dominate local-model bug
trackers elsewhere: native `/api/chat` transport (opencode/crush/pi are all on OpenAI-compat `/v1`,
which structurally cannot carry `num_ctx` — the root cause of crush's flagship "local models won't
call tools" discussion #1828), `/api/show`-based proactive context detection (`internal/ollamainfo`,
vs goose's blind 128k default for unknown models), a bounded resident `keep_alive` default
(`providerfactory/factory.go:31`), and P25.6's `LocalProfile` tool-schema deferral (goose #6883
reports Qwen3-coder silently degrading to XML-in-content past ~5 tools). Five candidate gaps from a
first-pass review were **refuted by direct code reading** and are recorded here so they are not
re-filed: proactive `num_ctx` (`ollamainfo.go:135-162`), config-driven `keep_alive`
(`factory.go:44,215`), ID-based tool-result correlation (`ollama.go:605` — the wire-level name-only
limitation is separately tracked as **P52.16**), `<think>`-before-tool-parse ordering (structurally
impossible to break on the native path — reasoning and tool calls arrive in separate NDJSON fields,
`ollama.go:590-600`), and tool-set shrinking for weak models (P25.6, `builtin.go:170-180`). What
follows is what survived verification.

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
  regression tests; this housekeeping is closed. With the scripts reachable,
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
  P39.6 verify loop, the P38.1 quality pass) — only the context lifetime changed. Originally in
  `internal/cli/chat_phased.go`; lifted into `internal/drive` by P52.12 so every client (daemon,
  TUI, web UI), not just the CLI, gets it. Unit-tested for phase sequencing/completion/prompt
  wiring.

- **2026-07-24, qwen3.6:35b-a3b-fast vs FirewallRuleAnalyzer (phased drive, stability):** the
  phased drive **reached a verify-clean suite** — 23 threats / 22 findings across 9 components, all
  `verify.py`/`lint_dfd.py`/`inventory.py --check` passing, content grounded in real file:line
  evidence and its own quality pass catching genuine inaccuracies — i.e. the mechanism/conformance
  closure condition below was **met**. But it took **three manual re-invocations**: the CLI
  `chat --skill` drive engine wired no proactive compaction, so each phase's context grew — the
  model re-reading files and recomputing STRIDE counts by hand — until Ollama hard-rejected the
  request and the drive aborted on a terminal `NewContextTruncationError` rather than a resumable
  stop. Root-caused into the **P47.x phased-drive stability batch** (P47.1-P47.6, all shipped):
  single-invocation stability was the bar, distinct from the mechanism closure already demonstrated
  here.

- **2026-07-27, qwen3.6:35b-a3b-fast vs FirewallRiskRater (hollow-report checks + self-heal,
  validated; phase-6 gap found):** first live run of the ec0127c hollow-report checks + afd6764
  self-heal, against a resumed suite whose `<!-- PENDING -->` markers were already deleted but whose
  finding bodies were empty. **Confirmed working:** self-heal auto-refreshed the stale project
  `verify.py` on launch, the three new checks turned the previously false-passing hollow suite
  (`11 passed, 0 failed` on the old verifier) into `12 passed, 2 failed` with exact file:line, and
  the drive fixed the `no-duplicate-header-rows` failure live. **New gap found and fixed:** the
  phase-6 verify/quality remediation loop lacked the overflow-reset and anti-monolithic-write
  guardrails the content phases carry — fixed by **P47.7**/**P47.8** (shipped), with **P47.9**
  (route hollow-body failures to the owning content phase) as the Tier-3 follow-up, **shipped
  2026-07-30**.

Reproduce: `cd <fresh target copy>` (must be inside the target — the sandbox rejects reads
outside the workspace root); run `aegis chat "threat model this repo" --skill threat-modeling --mode build --yes`
(the prompt is required — `aegis chat` errors with "no prompt provided" without one) — it now
prints a `phased mode` notice and resets context each phase. Closure condition: the real suite's
PENDING markers reach zero and `verify.py`/`lint_dfd.py`/`inventory.py --check` all pass (met
2026-07-24 on FirewallRuleAnalyzer; **unattended single-invocation** stability shipped via P47.x but
has not had its own dedicated live re-confirmation run since — the umbrella stays open until one
happens).

Priority: Tier 2 — every load-bearing harness fix the re-tests have root-caused (P39.5-P39.15) has
shipped, and P47.x/P52.12 address the two structural gaps (single-invocation stability,
CLI-only reach) found since. This item stays open only as the conformance **umbrella**, closeable
once a live built-in drive — now reachable from any client via P52.12 — is confirmed to reach a
verify-clean suite unattended, in one invocation, on a local model. Not Tier 1 because it is
live-run verification tracking, not independent build work.

**Lead closed 2026-08-02 as P54.2 — swept, no gap found.** The "accurate refusal, error-shaped"
exit-code question for the SCA/secrets scanners (P34.6 checked only the *language*-targeted tools)
was answered by running all six at their pinned versions against an empty tree and a docs/C/shell
tree with no dependency manifests. trivy, grype and syft exit 0 with valid output; gitleaks forces
`--exit-code 0` and its report file is read independently of the run's error; trufflehog exits 0
with empty stdout, which is the success branch. **osv-scanner's exit 128 is the only refusal of this
shape, and it was already interpreted by P34.12** — so osv-scanner is to the SCA/secrets half what
brakeman was to the language-targeted half: the only one. No gate added; the measurements are now
recorded in the `runJSON` doc comment (`internal/security/scanners.go`) so the sweep isn't re-run
from scratch. Write-up in [releases.md](releases.md).

### P55.4 — Container-first method resolution (`auto` currently defeats the container investment)

`Resolve`'s `auto` branch (`internal/security/method.go`) tries **host → container → WSL**, and
returns the host binary the moment `lookPath` succeeds. The practical result on a machine that has
both: of the scanners that ran in the end-to-end fixture test, seven resolved to `host` and exactly
one to a container path — and that one went to **WSL**, not the container. The multiscanner image
was built, pinned, cache-populated, and almost entirely unused.

Host-first was the right default when the container was a *fallback* for tools the operator hadn't
installed. It is the wrong default once the container is the supported path, and it actively
undermines the guarantees the container exists to provide: the host binaries are unpinned, whatever
version happened to be on PATH, with no reproducibility and no `--network none` confinement. Two
scans on two machines can silently use different scanner versions with different rule sets.

Fix: when the multiscanner covers a tool and its image verifies, prefer `MethodContainer` under
`auto`; fall back to host only when the container is unavailable, and say so. Keep an explicit
`method: host` escape hatch — it stays necessary for the tools that genuinely can't containerize
(P55.7/P55.8), and operators on machines without a container runtime need a working path.

Sequence this **after P55.1-P55.3**: inverting the precedence makes the container load-bearing, and
routing every scan through an image whose integrity isn't verified is how a silent all-clear
becomes the default rather than the exception.

**Priority:** Tier 2 — small and self-contained (one branch in `Resolve`, plus status/message
updates), but deliberately gated behind the Tier-1 integrity work. **Effort:** S.

### P55.5 — Pin the multiscanner globally by default

`aegis security build-image` writes `security.multiscanner` to the **project's**
`.aegis/config.yaml` unless `--global` is passed. Since the image itself is machine-wide podman
storage and the database volume (`aegis-scanner-cache`) is explicitly "a single named volume shared
by every scan in every project — one cache, machine-wide," project-scoped configuration is the odd
one out: the only machine-wide asset that is remembered per-repo.

The effect is invisible and confusing. Verified: inside the Aegis repo, `security status` reports
kubescape and opengrep as `container`; from any other directory the same binary and the same built
image report *"kubescape not installed ... run `aegis security build-image`"* — advice to rebuild an
image that already exists. An operator provisions once and concludes it didn't work.

Fix: default `build-image` to the user config and offer `--project` for the narrow case of pinning
a different image per repo. Under container-only this stops being a papercut and becomes the
difference between scanning working everywhere and working in exactly one directory.

**Priority:** Tier 2 — a default flip plus a flag rename, but it gates whether P55.4 is felt
outside this repo at all. **Effort:** S.

### P55.6 — Surface vulnerability-database age

Nothing reports how old the scanners' data is. Measured on this machine: trivy's DB carries
`NextUpdate 2026-07-17` and was read on 2026-08-02 — 16 days past its own refresh horizon — and
scans reported no concern. That silence is partly by construction: the image sets
`GRYPE_DB_VALIDATE_AGE=false` and `TRIVY_SKIP_*_UPDATE=true`, deliberately disabling the tools' own
staleness guards, because `--network none` scans cannot refresh and a cached DB is *expected* to be
old (the Containerfile says exactly this, and it is correct).

But suppressing the tools' warnings shifted the responsibility to Aegis, and Aegis never picked it
up. A stale SCA database doesn't fail — it under-reports, which is the same silent-all-clear shape
as P55.1-P55.3. A scan against a three-month-old DB looks identical to a clean repo.

Fix: read the cache markers already known to `multiscannerDBTools` (plus trivy's `metadata.json`,
which carries `UpdatedAt` directly), report age in `aegis security status`, and warn past a
threshold with the `aegis security update-db` remedy. Do **not** auto-refresh or fail the scan:
air-gapped operation is a supported posture, and the existing design decision to keep network out
of scan runs should not be reopened for this.

**Priority:** Tier 2 — small, read-only, and closes the last of the four silent-degradation paths
the container test surfaced. **Effort:** S.

## Open Work — Tier 3

**Status:** 2 open — **P55.7** and **P55.8**, the two structural items in the P55.x
container-only-scanning batch (see the Tier 1 header for the batch's origin). Both are sequenced
behind the Tier-1/Tier-2 P55.x work and are what actually close the "zero required host tools"
goal; everything before them makes the *existing* container trustworthy and preferred, while these
two extend containerization to the tools that currently cannot use it at all.

**P53.6** (non-native tool-calling shim — `internal/toolshim`,
`provider.tool_call_shim`) shipped 2026-08-02, the last of the P53.x local-LLM comparative-review
batch (see the Tier 2 header for the batch's origin and for the five candidate gaps that were
refuted by code reading). The two former Tier-3 items **P52.12** (lift the phased drive into the
daemon) and **P52.13** (`workspace.additional_roots`) shipped 2026-08-01. All three write-ups are in
[releases.md](releases.md).

**Two leads left by P53.6, neither filed:**

- **Auto-engage the shim off a low conformance rate.** Explicitly sequenced as a follow-up rather
  than dropped: the persisted P53.4 rate is already readable per model (`modelcaps.Store.ToolCalling`)
  and the config key rejects `"auto"` rather than silently accepting it, so the word stays available
  for exactly this. Promote when live runs show the rate predicting drive outcomes — engaging a
  prose-parsing fallback off a signal that isn't trustworthy is worse than requiring the operator to
  ask for it.
- **Grammar-constrained decoding** (Ollama structured outputs, llama.cpp GBNF). Deliberately not
  folded into P53.6 and still unfiled. It attacks the opposite end of the problem — making malformed
  tool-call JSON mechanically impossible rather than parsed-and-declined — and targets models that
  *do* speak the protocol but truncate or malform arguments (the P35.2 failure class), whereas the
  shim targets models that cannot speak it at all. None of the six reviewed harnesses does it. The
  two share no implementation, so this needs its own `### P<n>.<m>` heading if pursued.

### P55.7 — `aegis-netscanner`: a second image split by mount posture, not by tool category

Six scanners have no container path today (`gosec`, `dockle`, `trivy image`, `grype <ref>`, `nmap`,
`nuclei`), and `reconContainerFallbackUnsupported` states the reason plainly: they need network
egress, "and punching a network hole through that hardening isn't done for v1." That leaves nmap
and nuclei **baked into the multiscanner image but routed through WSL** — on Windows, an operator
must install and provision a Kali distro to run two tools that are already sitting in the image
they built.

Reviewing what each tool actually needs shows the split is not offline-vs-network. It is **what the
container is allowed to see**:

| Tool | Needs | Needs the workspace? |
|---|---|---|
| `trivy image`, `grype <ref>` | registry egress | No — takes an image reference |
| `nmap`, `nuclei` | egress to the target host | No — takes a target list |
| `zap` | egress to the target app | No — takes a URL |
| `dockle` | container **engine socket** | No — inspects a built image |
| `gosec` | Go toolchain + module-proxy egress | **Yes** |
| `trufflehog --verify` | egress to provider APIs | **Yes** |

Only the last two need the workspace *and* the network at once — which is precisely the
combination the current hardening forbids, and rightly: workspace + egress is an exfiltration path
out of a hostile repo. Everything above them needs egress but has nothing to steal, because it
scans a remote target rather than local source.

So: a second locally-built image run with **network on and no workspace mount, ever** — carrying
nmap, nuclei, `trivy image` and `grype <ref>`. The invariant is enforceable rather than
conventional: its runner takes a target argument and has no directory parameter to pass. The
existing `--network none` + workspace-mounted runner stays exactly as it is, and the two runners
never converge.

Two carve-outs. **ZAP is already solved** and should stay as-is: `dast.go` runs it from the
official zaproxy image with its own `/zap/wrk` mount contract, so it already requires no host
install; folding a large Java app into a locally-built image buys nothing. **dockle needs the
engine socket**, which is a *third* privilege axis — socket access is effectively host root, not
merely egress. It can live in this image, but it must run socket-mounted and workspace-free, and
whether Aegis should mount a container socket at all deserves an explicit decision rather than
arriving as a side effect of this item.

**Priority:** Tier 3 — real value (it is most of the remaining install burden, and removes the WSL
dependency on Windows outright) but larger, and sequence-dependent: it wants P55.3's verification
harness to cover a second image, and it reopens a hardening posture that was closed deliberately,
so the mount-posture invariant needs to be built in from the start rather than retrofitted.
**Effort:** L.

### P55.8 — gosec without a host Go toolchain (two-phase, network and analysis never overlap)

`gosec` is the one tool container-only cannot simply absorb, and `multiscannerExcludedTools`
already states why with unusual precision: it is compile-assisted, resolves packages via `go list`
(needing a Go toolchain and the module cache, i.e. the network), and **reports zero findings rather
than failing when it can't**. Measured on this repo: host 244 findings, container 0. Dropping the
host path without solving this would silently delete all Go SAST coverage from a Go-first codebase
— the single worst outcome available, and exactly the failure class P55.1-P55.3 exist to prevent.

The approach that preserves the hardening invariant is the split this codebase already uses for
`update-db`: **two phases, where the phase with network does no analysis and the phase that
analyzes has no network.** Mount the workspace read-only into a networked container and run
`go mod download` into a persistent module-cache volume; then run gosec `--network none` against
that warmed cache. `go mod download` fetches modules but does not execute them, so the exposure is
materially smaller than a general network-plus-workspace grant, and the analysis step keeps the
same confinement every other scanner has.

The weaker fallback, if the two-phase split proves impractical: one networked container with the
workspace mounted **read-only** and egress restricted to the module proxy. Worse, and it should be
named as a deliberate concession rather than slipped in — but still strictly better for a user than
"install Go and gosec yourself."

`trufflehog --verify` is the same shape (workspace + egress, currently host-only per its `Resolve`
override) and can ride whichever mechanism lands, though it is off by default and far less costly
to leave host-only.

Whatever is built, the acceptance test is not "it runs." It is **finding parity against the host
run on this repo** — a container gosec reporting near-zero while exiting clean is the precise
failure this item exists to avoid, and P55.3's canary assertion should cover it.

**Priority:** Tier 3 — the last blocker to "zero required host tools" for a Go project, but the
most delicate item in the batch: it is the one place where container-only trades against the
network/workspace separation, so it should land last, after the verification work can prove the
result. **Effort:** L.

## Open Work — Tier 4

**Status:** 6 open, all measure-first or explicitly parked; none has a build trigger yet.

### P55.9 — Relevance gating for the always-on scanners (measure-first)

Scanner selection is already good and this item should not be read as a gap in it: `DetectLanguages`
auto-enables the matching language engine, and `RelevanceChecker` skips tools whose input isn't
present. Verified on a Go-only fixture — gosec auto-enabled from `go.mod`, while hadolint
("no Dockerfile found"), kubescape ("no Kubernetes manifests found") and brakeman ("no Rails
application found") each skipped with an accurate reason.

The residue is that `RelevanceChecker` is implemented by only three scanners (kubescape, hadolint,
brakeman). The dependency-oriented tools — `osv-scanner`, `grype`, `syft` — run unconditionally,
including on trees with no dependency manifest at all, where they walk the workspace to conclude
nothing. On the fixture that cost ~12s of a ~15s scan for osv-scanner alone.

Two reasons this is Tier 4 rather than an obvious win. First, the cost is wall-clock only — these
tools already exit 0 with valid empty output on a manifest-free tree (measured and recorded by
P54.2), so there is no correctness gap to close. Second, a manifest check is a genuinely worse
oracle than it looks: vendored dependencies, lockfiles in unexpected locations, and syft's
binary-artifact detection all find packages with no manifest present, so a naive `Relevant` would
convert a slow-but-correct scan into a fast wrong one — the trade this batch is otherwise trying to
eliminate.

**Promote when:** scan latency is an actual complaint, and only with a relevance check validated
against a corpus that includes vendored and lockfile-only projects.

**Priority:** Tier 4 — measure-first, no correctness trigger; do not build speculatively. **Effort:** S.

### P52.14 — Session-scoped loop detector (cross-`Run` loops are invisible)

`newLoopDetector` is constructed **inside** `Run` (`engine.go:422-424`), so its window resets on
every call. In the TUI and web UI, each user turn is a separate `Run` — so a model that loops
*across* user turns (re-reading the same file every time the user nudges it, re-running the same
failing command after each correction) is never detected, no matter how many turns it repeats.

Fix would be to hoist the detector to session scope, plumbed through `engine.Options` as an optional
caller-owned detector so the daemon can hold one per session while the CLI keeps today's per-`Run`
behavior. The complication worth thinking through before building: a user *legitimately* asking for
the same tool call twice in two turns is not a loop, so a session-scoped detector likely needs a
higher threshold than the per-`Run` one, or needs to reset on any user message that isn't a bare
retry — which is a fuzzier judgment than the current mechanism makes.

**Promote when:** a live run shows a cross-turn loop that per-`Run` detection missed.

**Precondition now met (2026-08-01).** P53.2 deliberately landed first: widening the scope of a
detector that mis-fired on polling and always aborted fatally would have multiplied both defects.
With the poll exemption and the nudge-once-then-abort outcome split shipped, a session-scoped
detector would now inherit a sounder mechanism — but the promotion trigger above is still the
observed cross-turn false negative, which has not happened.

**Priority:** Tier 4 — real but unproven, and the false-positive risk is higher than the current
detector's.

**Reviewed 2026-08-01: still correctly parked, no code change.** Confirmed against current code —
`newLoopDetector` is still constructed inside `Run` at every call site, unchanged since this item
was filed. No live run has reported the cross-turn loop this would catch, so the promotion trigger
has not fired. Not worth building speculatively: the design question it flags (what counts as a
legitimate repeat vs. a loop across turns) is real work, not a mechanical port of the per-`Run`
detector to a wider scope, and building it without a concrete false-negative in hand risks shipping
a detector tuned against a guess rather than an observed failure mode.

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

### P25.9 — per-session scoping of `lsp.Manager` (remaining daemon singleton)

Five of the six daemon-singleton services were per-session-scoped when P25.9 first shipped;
`lsp.Manager` was deliberately left as a shared singleton — its per-session resource-growth
tradeoff was judged worse than the isolation gap. Parked pending a concrete multi-tenant need.

Priority: Tier 4 — no trigger, explicitly parked. Do not build speculatively.

---

For shipped feature history, design rationale, competitive-landscape review, and the full gap
analysis, see [releases.md](releases.md).
