# Aegis Capability Roadmap

**Last updated:** 2026-07-14

This document tracks only **open** work and what's next. For shipped-feature history and full
design rationale, see [releases.md](releases.md). Every open item is a `### P<n>.<m>` heading
with a `Priority:` line in its body — `scripts/roadmap-status.sh` parses exactly that shape, so
keep it when adding items.

---

## Status

**Open items:** 8, filed 2026-07-14 from two audits: the P30 batch (a code-gap scan for
TODO/stub/skip/robustness markers, and a docs-vs-implementation drift scan of every docs/*.md file
against current source) run after the P29 batch closed out all prior open work, plus the P31 batch
(GitHub CodeQL code-scanning alerts pulled from the `fiddler110/Aegis` repo — 24 open alerts across
`go/path-injection`, `go/command-injection`, and `go/cookie-secure-not-set`, individually read
against source and triaged into 2 genuine fixes, 1 hardening item, and 2 batched
false-positive-triage/dismissal items covering the rest) — see [releases.md](releases.md#latest-changes).
Tier 4 also still has 1 parked item with no active trigger (see [Parked](#open-work--parked-tier-4)).
The P27 threat model's needs-verification list remains fully closed — see
[releases.md](releases.md#latest-changes) for the 2026-07-14 verification pass that confirmed the
last two items (hook execution timing, cron fire-time rule application) were already resolved by
shipped mechanisms, no code change needed. All four Tier 1 items shipped 2026-07-14: **P31.1**
(nuclei `templates_version` path traversal / git-arg injection), **P31.2** (session-workdir
existence-oracle gate ordering), **P30.1** (LSP client hang on transport death), and **P30.2** and
**P30.3** (hooks and TUI bang command both hardcoded `sh -c` on Windows) — see
[releases.md](releases.md#latest-changes).

**Next session:** no Tier 1 work remains — pick up Tier 2 starting with P31.3 (Web UI CSRF cookie
`Secure` flag), first in priority order below. Re-run `TestLiveWorkflow` (recipe in CLAUDE.md)
after any change touching the engine/server/sandbox/guard/swarm/cron/debate seams; `aegis doctor`
is the standalone preflight companion for the same misconfiguration classes.

---

## Priority Order

Tiering criteria: **Tier 1** = real, currently-exploitable security/robustness gaps, small effort,
no dependency. **Tier 2** = cheap, no-dependency wins — user-facing polish or small self-contained
hardening. **Tier 3** = real value but larger or sequence-dependent (blocks or is blocked by other
work). **Tier 4** = low urgency, no trigger, or explicitly parked pending demand — do not build
speculatively.

**Tier 1:** none open. (P31.1, P31.2, P30.1, P30.2, and P30.3 shipped 2026-07-14.)

**Tier 2:** P31.3, P31.4, P31.5, P30.4, P30.5, P30.6, P30.7, P30.8 — in priority order: real
security hardening (P31.3) and alert-noise reduction (P31.4, P31.5) ahead of pure docs-drift
cleanup (P30.4-P30.8).

**Tier 3:** none open.

**Tier 4:** parked — P25.9. See [Parked](#open-work--parked-tier-4).

---

## Open Work

### P31.3 — Web UI CSRF cookie never sets `Secure`, even when TLS is enabled

Priority: Tier 2 · Effort: S · [CodeQL alert #3](https://github.com/fiddler110/Aegis/security/code-scanning/3), `go/cookie-secure-not-set`, medium

`internal/server/webui.go:79-86` (`handleWebUI`) sets the `HttpOnly`/`SameSite=Strict` double-submit
CSRF cookie (FIND-01/P24.1) without `Secure`, unconditionally. The default loopback-only deployment
this file's own doc comment describes doesn't need it, but `server.tls.enabled` (P24.18,
`internal/server/tls_test.go`) is a supported config for remote-accessible daemons, and on that path
the cookie should be marked `Secure` so it's never sent back over a downgraded plaintext connection.
Fix: change the handler's unused `_ *http.Request` parameter to `r`, and set
`Secure: r.TLS != nil` on the cookie.

### P31.4 — Two `go/command-injection` alerts are argv-exec/by-design, not exploitable — dismiss with justification

Priority: Tier 2 · Effort: S · [alert #7](https://github.com/fiddler110/Aegis/security/code-scanning/7) (`internal/tool/builtin/git.go:68`) and [alert #5](https://github.com/fiddler110/Aegis/security/code-scanning/5) (`internal/hooks/exec.go:95`), both `go/command-injection`, critical

Both read against source: `git.go:68`'s `runGit` already runs `exec.CommandContext(ctx, "git",
args...)` as an argument vector (never a shell string — the function's own comment says so), gated
by `validateGitArgs`' `deniedGitArgPrefixes` blocklist (git.go:44-61) against option tokens that
could write files or invoke external programs; CodeQL's `go/command-injection` query flags it purely
because `args` is caller-influenced, without crediting the blocklist. `hooks/exec.go:95`'s `sh -c
s.Command` is intentional — a hook's whole purpose is to run an operator-configured shell command
(already tracked for its separate Windows-portability gap as P30.2); `s.Command` comes from trusted
local config, not request/remote input. Action: dismiss both GitHub alerts with a written
justification (CodeQL supports per-alert dismissal reasons: "used in tests" doesn't fit, but a
custom note explaining the argv-vector/by-design-config reasoning does) rather than changing code.
If `validateGitArgs`' blocklist approach is a lingering worry, consider hardening git.go toward an
allowlist of safe read-only subcommands instead — but that's a separate, optional robustness
improvement, not a fix for this alert.

### P31.5 — Nineteen `go/path-injection` alerts trace to already-validated session Workdir or directory enumeration — triage and suppress

Priority: Tier 2 · Effort: M · [alerts #8-27 minus #4](https://github.com/fiddler110/Aegis/security/code-scanning) (19 of the 20 open `go/path-injection` alerts; #4 is P31.2), all high severity

Read each flagged line against source (`internal/persona/load.go:152,216`;
`internal/agentdef/agentdef.go:126,148`; `internal/skills/skills.go:108,131,149,182`;
`internal/memory/memory.go:134,137,165,169`; `internal/memory/integrity.go:44,54`;
`internal/security/sbom.go:45,48`; `internal/security/report_artifact.go:34,37`;
`internal/security/baseline.go:41`). All follow one of two safe shapes CodeQL's taint tracking
doesn't credit: (1) `os.ReadDir(dir)` enumerates a directory and re-joins `e.Name()` — a filename
CodeQL taints back to `dir`, but the actual traversal-relevant string is filesystem-enumerated, not
attacker-supplied; or (2) the path is `filepath.Join(root, fixedSuffix)` where `root` is a session
Workdir/project root that already passed `resolveSessionWorkdir`'s existence-and-allowlist gate
(P25.1, see P31.2) several calls upstream, and `fixedSuffix` is a hardcoded string
(`.aegis/memory.md`, `.aegis/sbom.cdx.json`, `.aegis/security-baseline.yaml`, `.aegis/skills`) —
`memory.go`'s `SaveSkill` additionally sanitizes its one real user-supplied segment (`name`) through
`sanitize()` (memory.go:193-205, alphanumeric + `-`/`_` only) before the join CodeQL flags at
memory.go:169. None of the 19 have an unvalidated attacker-controlled path segment reaching disk.
Action: don't code-change these — add each to `.aegis/security-baseline.yaml` (the suppression
format `internal/security/baseline.go` already reads, `rule_id` + `reason` + `expires`) or dismiss
the corresponding GitHub alert with the specific safe-shape justification above, so future CodeQL
runs stop resurfacing confirmed-safe findings as noise that could mask a real future alert. If a
sanitizer-recognition false-positive persists after dismissal, a CodeQL query customization
(recognizing `sanitize()` as a barrier) is the next step, not a code change to already-safe call
sites.

### P30.4 — Six docs/*.md files link to a `security.md` that no longer exists

Priority: Tier 2 · Effort: S

`docs/README.md`, `cli-reference.md`, `configuration.md`, `permissions.md`, `tools-reference.md`,
and `installation.md` all link to `security.md` / `docs/security.md`, but that file was renamed to
`docs/security_scan.md` (commit `4f336bc`, "Rename security.md to security_scan.md") and every one
of these six cross-references is now a broken link. Mechanical fix: update the six links.

### P30.5 — Four shipped CLI commands undocumented in cli-reference.md

Priority: Tier 2 · Effort: S

`aegis trust` (`internal/cli/trust.go`, the P27.1 workspace-trust gate — arguably the most
security-relevant CLI surface in the repo), `aegis doctor` (`internal/cli/doctor.go`, the standalone
preflight command this very roadmap tells contributors to run), `aegis cron list`
(`internal/cli/cron.go`), and `aegis config update` (`internal/cli/configupdate.go`) are all fully
implemented but absent from cli-reference.md's command tree. Two other docs already assume readers
can find them there: docs/providers.md:72 says "see `aegis doctor` (see [CLI
Reference](cli-reference.md))" and docs/tools-reference.md:372 tells readers to run `aegis cron
list` "from the CLI" — both point at a section that doesn't exist yet.

### P30.6 — Two shipped TUI slash commands undocumented in tui-guide.md

Priority: Tier 2 · Effort: S

`/fork [n]` (`internal/tui/commands.go` ~line 215 — branch a session, optionally from a checkpoint,
backed by `POST /sessions/{id}/fork`, P22.3) and `/notify <off|bell|desktop|both>`
(`internal/tui/commands.go` ~line 293) are both fully implemented but missing from tui-guide.md's
Slash Commands tables. `/notify` is already referenced from docs/configuration.md:349 ("Also
settable live with `/notify <mode>`") — another doc assuming a home for it that doesn't exist yet.

### P30.7 — Remaining small doc-drift: `cron_history` tool, LSP deferred-tag, `zero_tool_nudge` config key

Priority: Tier 2 · Effort: S

Three smaller omissions from the same audit pass, grouped since each is a one-paragraph fix in a
different doc:
- `docs/tools-reference.md`'s Scheduling section lists `cron_create`/`cron_list`/`cron_delete`/
  `cron_toggle` but omits `cron_history` (`internal/tool/builtin/cron.go`, read-capability audit-
  history tool shipped in P24.9/FIND-34).
- The same doc's LSP tools list tags `definition`/`hover`/`document_symbols`/`workspace_symbols`/
  `call_hierarchy` as `*(deferred)*` but shows `diagnostics` and `references` without that tag,
  implying they load eagerly. `internal/tool/builtin/builtin.go:181` and `lsp.go:15-25` show
  `LSPTools(...)` returns all seven together and all seven go into the deferred slice —
  `diagnostics`/`references` are deferred too; fix the tag.
- `docs/configuration.md`'s exhaustive `provider:` YAML reference block omits
  `provider.zero_tool_nudge` (`internal/config/config.go:396`, P28.3's zero-tool-call retry bound).
  docs/providers.md:79 already names it by key; configuration.md's own reference block should too.

### P30.8 — Stale comment in `webui.go` still points at a since-closed P15 gap

Priority: Tier 2 · Effort: S

`internal/server/webui.go:41-46`'s doc comment on `handleWebUI` says the web UI's "current scope
covers the core chat loop only... not persona/mode switching, cost/token display,
checkpoints/rewind, security scanning, skills, or memory management," attributing the gap to "the
subject of research/roadmap.md's P15 track." `releases.md` shows the entire P15 web-UI-parity track
(persona picker, debate/knowledge panels, security-check panel, skills & memory panel, `/rewind`
with confirmation dialog) shipped and closed, and this roadmap currently has no open P15 item —
the comment actively misdirects a future contributor who reads it before "extending this file ad
hoc" (its own stated purpose). Update the comment to reflect current web UI scope, or remove the
stale gap description if the UI now has full parity.

---

## Open Work — Parked (Tier 4)

Low urgency, no trigger, or explicitly parked pending demand. Do not build speculatively —
revisit only if a concrete trigger (user demand, reported pain, incident) appears, and check
with the user before starting any of these.

### P25.9 — Per-session scoping of `lsp.Manager`

Priority: Tier 4 · Effort: L — parked, no concrete trigger

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
