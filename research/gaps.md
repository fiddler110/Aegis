# Aegis Security & Quality Review — Gap Log

Running log of confirmed gaps and follow-ups from the phased review (see conversation
for full context). Each entry: phase, file:line, severity, description, status.

## Phase 1 — Tool sandbox confinement audit (internal/tool/builtin)

**Result: no confirmed gaps.** All 46 built-in tools that touch the filesystem or
spawn subprocesses route through `sandbox.ValidatePath`/`ValidatePathIn` (via
`resolveRead`/`resolveWrite`/`resolvePath` wrappers) or build argv slices rather
than shell strings. `argv_confine.go` documents three previously-closed
vulnerabilities (VULN-01/02/11) from a prior audit round.

### GAP-1.1 — `internal/repomap.Build` reads symlinked source files with no confinement check (Low)
- **File:** `internal/repomap/repomap.go:394-430` (`Build`'s `filepath.WalkDir`
  callback, `os.ReadFile(path)` at line 420); `fingerprint` at lines 775-797
  has the same pattern but is stat-only, so no read there.
- **Description:** Follow-up to the Phase 1 note that `internal/repomap` (the
  actual filesystem-access layer behind `internal/tool/builtin/repomap.go`,
  which does not call `os.*` directly) had not been independently verified.
  Every one of the 46 built-in tools resolves paths through
  `sandbox.ValidatePath`/`ValidatePathIn`, which calls `filepath.EvalSymlinks`
  and confines the *resolved* target to root
  (`internal/sandbox/pathvalidator.go:94,157,231,252`). `repomap.Build` does
  not: `filepath.WalkDir` doesn't follow symlinked directories (so
  directory-escape isn't possible), but a symlinked *file* with a recognized
  source extension (`.go`, `.py`, `.js`, etc.) is opened directly via
  `os.ReadFile(path)` with no symlink-target check. Its declaration lines
  (`func`/`type`/`class`/`def`) and import edges are extracted and end up in
  the rendered `<repo_map>` block injected into the model's system prompt
  (`internal/repomap/repomap.go:707-713`), and reachable on-demand via the
  `repomap` tool.
- **Not a gap:** `repoRelative` in `internal/tool/builtin/repomap.go:166-181`
  (the `skeleton`/`importers`/`map` glob path arguments) only rejects
  `../`-escaping strings for an in-memory lookup against already-built
  `m.Files` — it never opens a file, so it carries no traversal risk itself.
  `root` itself is not attacker-controlled per-call (comes from
  `effectiveRoot`, the already-validated session workdir), so this is
  specifically about symlinks planted *inside* an already-trusted root, not
  about `root` selection.
- **Failure scenario:** A workspace (or a file a prior turn wrote, or synced
  from an untrusted source) contains a symlink such as
  `internal/leak.go -> /etc/some-other-project/secrets.go`. The daemon
  process's own read permissions govern access — anything readable by that OS
  user, wherever it lives, gets its declaration-line signatures echoed into
  the model-visible repo map, bypassing the sandbox confinement boundary
  every other file-touching path enforces.
- **Suggested fix (not applied):** in `Build`'s walk callback, resolve `path`
  via `filepath.EvalSymlinks` and verify the result still falls under the
  resolved root before calling `os.ReadFile`, mirroring
  `sandbox.ValidatePath`'s check — or simply `Lstat` and skip symlinks
  entirely, since indexing a repomap has no real need to traverse them.
- **Status:** FIXED. `Build`'s walk callback now skips any entry whose
  `d.Type()&fs.ModeSymlink != 0` before calling `os.ReadFile` — `fs.WalkDir`
  reports a symlinked file's direct (unfollowed) mode, so this is a cheap
  check with no extra stat. Pinned by `TestBuildSkipsSymlinkedFiles` in
  `internal/repomap/repomap_test.go` (skips itself in an environment without
  symlink-creation privilege, e.g. non-admin/non-Developer-Mode Windows).

## Phase 2 — Concurrency & goroutine-lifecycle audit

**Result: one confirmed gap.** Audited goroutine lifecycles and lock discipline
across internal/heartbeat, internal/engine (toolround.go), internal/provider
(admission.go), internal/swarm (adaptive.go, subprocess.go), and internal/server
(auto-pruner, SSE heartbeat, checkpoint git-SHA capture).

Verified as OK (no gap):
- `internal/heartbeat`'s chaining claim holds — `With` conses a linked list
  rather than overwriting the ctx value, so a sub-agent's watch never shadows
  its parent's.
- `internal/engine/toolround.go` matches every documented invariant precisely:
  execLock scoping, round-vs-turn context separation, started-call tracking,
  per-call panic recovery.
- `internal/swarm/subprocess.go` reaps child processes correctly on both POSIX
  (`Pdeathsig`/`Setpgid`) and Windows (Job Object `KILL_ON_JOB_CLOSE`) — no
  zombie/orphan risk even on abnormal daemon death.
- `internal/provider/admission.go`'s abandon-drain goroutine and
  `internal/swarm/adaptive.go`'s ctx-watcher both join/cleanup correctly.
- `internal/server`'s session auto-pruner and SSE heartbeat both bind to
  `ctx.Done()` and are explicitly joined before their handlers return.
- No deadlock-prone lock ordering found; no mutex that should be an RWMutex
  under the observed (short, low-contention) hold-time patterns.

### GAP-2.1 — Unbounded goroutine/subprocess leak on a hung `git rev-parse`
- **File:** `internal/server/messages.go:283-289` (spawn site), calling into
  `captureGitSHA` → `execGitCmd` in `internal/server/sessions.go:886-901`.
- **Severity:** Low (rare trigger, small per-occurrence footprint, but the one
  goroutine in this codebase with *no* cancellation path at all).
- **Description:** The checkpoint git-SHA capture is spawned as
  `go func(cpID string) { captureGitSHA(context.Background(), workdir) ... }`,
  and `execGitCmd` calls `exec.CommandContext(ctx, "git", ...)` with that same
  `context.Background()`. Since the context carries no deadline,
  `exec.CommandContext` enforces none — if `git rev-parse HEAD` blocks (corrupted
  repo, network-mounted workdir, a hanging git hook, `.git/index` lock
  contention), both the goroutine and the child `git` process leak for the life
  of the daemon process. Not bounded by session teardown, run cancellation, or
  even daemon shutdown.
- **Failure scenario:** A user works from a workdir whose git repo has a stuck
  hook or a network-filesystem stall. Every checkpoint-creating turn thereafter
  spawns another leaked goroutine+process — a slow, message-count-scaled leak.
- **Suggested fix (for a later phase, not applied now):** give the spawn site
  its own `context.WithTimeout` (a few seconds is ample for `rev-parse HEAD`)
  instead of `context.Background()`.
- **Status:** FIXED. `captureGitSHA` (`internal/server/sessions.go`) now
  bounds itself with `captureGitSHACmdTimeout` (5s) regardless of the
  caller's context, and the spawn site (`internal/server/messages.go`) passes
  `s.daemonCtx` — a new `Server` field set from `ListenAndServe`'s ctx — so a
  graceful shutdown also cancels an in-flight capture immediately rather than
  waiting out the timeout. Pinned by `TestCaptureGitSHA_RespectsTimeout` and
  `TestCaptureGitSHA_RespectsShutdownCancellation` in
  `internal/server/gitsha_test.go`.

## Phase 5 — Regression tests for confirmed gaps

Originally logged as proposals for review; all have since been written to
the tree and are green. Each mirrors an existing convention in this codebase
(see `internal/session/session_test.go`'s `TestOpenAppliesPermissionHardening`,
the `FIND-29/P24.16` pattern) so they read as a natural addition, not a
foreign style, and run alongside existing pinned tests rather than replacing
them.

- **GAP-2.1** — `TestCaptureGitSHA_RespectsTimeout` and
  `TestCaptureGitSHA_RespectsShutdownCancellation` in
  `internal/server/gitsha_test.go`. Uses a compiled (not shelled-out) fake
  `git` binary on `PATH` that sleeps well past the bound, since a `.bat`/`.sh`
  wrapper on Windows spawns a grandchild process that survives killing the
  wrapper — defeating the very timeout under test.
- **GAP-3.1** — `TestLogFileAppliesPermissionHardening` in
  `internal/logging/logging_windows_test.go` (asserts the DACL directly, one
  subtest per open path) and `TestNewAppliesPermissionHardening` in
  `internal/logging/logging_test.go` (cross-platform smoke).
- **GAP-3.2** — `TestSpillWriteAppliesPermissionHardening` in
  `internal/tool/builtin/spill_windows_test.go` and
  `TestSpillWriteSucceedsWithHardening` in
  `internal/tool/builtin/spill_test.go`, same split.
- **GAP-4.2** — `TestMultiEditLoadFile_ReadError_Unwraps` in
  `internal/tool/builtin/multiedit_test.go`, using `errors.As` against
  `*fs.PathError` (a directory-as-file read) rather than `fs.ErrNotExist`,
  since that's the branch the `%v`→`%w` fix actually touched.
- **GAP-1.1** — `TestBuildSkipsSymlinkedFiles` in
  `internal/repomap/repomap_test.go` (skips itself when the environment
  lacks symlink-creation privilege, e.g. non-admin/non-Developer-Mode
  Windows).

GAP-4.1 (config.go file split) and GAP-4.3 (latex.go split) were structural,
not behavior changes — no new test was meaningful for either; existing tests
already pinned each package's external behavior and were the regression
check for the split itself (both green afterward).

## Phase 4 — Idiomatic Go / structural review

Scoped to the packages already touched in Phases 1-3 (internal/tool/builtin,
internal/sandbox, internal/engine, internal/heartbeat, internal/provider,
internal/swarm, internal/server, internal/session, internal/trace,
internal/logging, internal/redact, internal/share, internal/config,
internal/fsguard) rather than the whole tree. **Result: no design defects,
three minor/moderate polish items.** This scope was already validated twice
for security in Phases 1-3, and that discipline carries over — no god-package,
circular dependency, interface bloat, receiver-mixing, or naked-return
overuse found.

### GAP-4.1 — `internal/config/config.go` is a 2370-line, ~20-domain file (Moderate, structure)
- **File:** `internal/config/config.go`.
- **Description:** One file holds every config sub-schema (Sandbox, Cost,
  Provider, Compaction, Server, TLS, Permission, Git, Security, Multiscanner,
  Netscanner, DAST, OutputGuard, Skills, Diagram, Log, …) plus their methods.
  Still one cohesive package — this is a file-organization issue, not an
  architectural one — but every unrelated config change touches the same file
  in diffs/blame, and locating one domain's struct is harder than it needs to
  be.
- **Suggested fix:** split into `config_<domain>.go` files within the same
  package; pure reorganization, no import-graph change.
- **Status:** FIXED. Split into 9 files, code moved verbatim (no behavior,
  identifier, or import-graph change): `config.go` (801 lines — core `Config`
  struct, `WorkspaceTrustStatus`, `Load()`, layer merging, defaults, `.env`
  loading, workspace-trust freeze, path helpers), `config_provider.go` (430),
  `config_security.go` (256 — Security/Multiscanner/Netscanner/DAST/Debate),
  `config_runtime.go` (185 — TUI/Cleanup/Swarm/Tools/Workspace/OutputGuard/
  PersonaOverride/Skills/Diagram/Log), `config_integrations.go` (164 — MCP
  server mode/Embeddings/Hooks/Search/RepoMap/Notify/LSP/Plugins),
  `config_server.go` (173 — Server/TLS/Permission/Git), `config_sandbox.go`
  (139), `config_cost.go` (139), `config_compaction.go` (114).
  `go build ./...`, `go vet`, `gofmt -l`, and
  `go test ./internal/config/... ./internal/cli/... ./internal/server/...`
  all clean.

### GAP-4.2 — `internal/tool/builtin/multiedit.go:194` drops the error chain (Minor, error-handling)
- **File:** `internal/tool/builtin/multiedit.go:194`.
- **Description:** `fmt.Errorf("cannot read %s: %v", first.Path, err)` uses
  `%v` instead of `%w` — the sole non-wrapping error format found across the
  reviewed scope; every sibling error path already uses `%w`. A caller doing
  `errors.Is`/`errors.As` up this path silently fails here while succeeding
  everywhere else.
- **Suggested fix:** change `%v` to `%w`.
- **Status:** FIXED. Changed to `%w`. Pinned by
  `TestMultiEditLoadFile_ReadError_Unwraps` in
  `internal/tool/builtin/multiedit_test.go`.

### GAP-4.3 — `internal/tool/builtin/latex.go` (1347 lines) — confirmed split-candidate (Minor, structure)
- **File:** `internal/tool/builtin/latex.go`.
- **Description:** Follow-up read confirms this is not one cohesive unit —
  five low-coupling responsibilities share only the package, not the
  call-graph:
  1. `latex_build` tool registration/schema/`Execute` (lines 1-234).
  2. Bibliography pass — `latexBibControlFile`, `checkLatexBibConfinement`,
     `runLatexBibTool`, `latexBibEnv`, `firstLatexBibLine`,
     `latexSourceDeclaresBibliography`, and the `latexBCFDatasourceRE`-family
     regexes (lines 236-546, ~310 lines).
  3. Workspace confinement / source walk —
     `latexHardenedFlags`/`latexHardenedEnv`, `checkLatexConfinement`,
     `latexWalkSources`, `latexFileRefs`, `latexResolveRef`,
     `latexRefIsRooted`, `latexSourceCandidates`, `stripTeXComments`, plus
     `latexResolvedRoots`/`latexPrimaryRoot` (lines 425-451, called from the
     bibliography pass too — the only real cross-section coupling, and it's
     trivial) (lines 548-897, ~350 lines).
  4. Log parsing/formatting — `latexLogSummary`, `parseLatexLog`,
     `formatBuildResult`; pure string processing, no filesystem/subprocess
     (lines 899-1007, ~110 lines).
  5. `latex_new_document` tool + `buildLatexDocument` template generator —
     self-contained, zero coupling to sections 1-4 beyond the package name
     (lines 1009-1347, ~340 lines).
- **Suggested fix:** mechanical file split, same package, no import-graph
  change: `latex_build.go` (~234), `latex_bibliography.go` (~300),
  `latex_confinement.go` (~350, keeps `latexResolvedRoots`/`latexPrimaryRoot`),
  `latex_log.go` (~110), `latex_template.go` (~340). Existing tests keep
  pinning behavior through the move.
- **Status:** FIXED. Split into 5 files exactly per the plan, code moved
  verbatim: `latex_build.go` (232), `latex_bibliography.go` (299),
  `latex_confinement.go` (390, keeps `latexResolvedRoots`/`latexPrimaryRoot`,
  called from the bibliography file), `latex_log.go` (116),
  `latex_template.go` (352). Original `latex.go` deleted — nothing left over.
  `go build ./...`, `go vet`, and `go test ./internal/tool/builtin/...` all
  clean.

## Phase 3 — Secrets-and-logging trace

**Result: two confirmed gaps**, both the same class (missing Windows ACL
hardening), one low-severity and one materially exposed. Traced request flow
end-to-end across internal/logging, internal/session, internal/trace,
internal/tool/builtin/spill.go, internal/provider/* adapters,
internal/redact/internal/share (export path), and internal/config's
.aegis/.env handling.

Verified as OK (no gap):
- `internal/redact` is a real, deliberately-scoped credential-pattern filter
  (PEM keys, AWS/OpenAI/Anthropic/GitHub/Slack tokens, JWTs, bearer tokens,
  key/secret assignments).
- `internal/share/redact.go` (the SEC-08 export path) genuinely applies
  `redact.Text` across the full transcript — message text, thinking, tool
  inputs/outputs, title, system prompt — before any of the 3 export renderers.
  Not a dead/narrow library call; this is the one artifact designed to leave
  the machine, and it's covered.
- `internal/session`'s SQLite store (and WAL/SHM sidecars) genuinely calls
  `fsguard.RestrictToOwner` via `hardenDBPermissions` — the CLAUDE.md claim is
  implemented, not just a doc-comment aspiration.
- `internal/trace` stores full turn content unredacted, but this is
  intentional and documented ("the export path is where redaction belongs") —
  it's the same DB file already covered by session hardening, not a new
  disclosure surface.
- `internal/config`'s `.aegis/.env` handling: no evidence secrets get merged
  into a loggable "effective config" dump; the file itself gets
  `fsguard.RestrictToOwner`.
- `internal/provider/*` adapters (anthropic, openai, ollama) and
  `internal/engine`: logging is metadata-only (counts, durations, tokens,
  clamp decisions) on all paths checked, including error paths — no
  header/body/API-key logging found anywhere.

### GAP-3.1 — Log files skip Windows ACL hardening (Low)
- **File:** `internal/logging/logging.go:43,91,141`.
- **Description:** Every other security-sensitive file in this codebase
  (session DB, longmem DB, knowledge DB, daemon auth token, TLS key, config
  token/env, swarm mailbox) calls `fsguard.RestrictToOwner` after creation, per
  the documented rationale that `0o600` alone is cosmetic on Windows (new files
  inherit the parent directory's ACL). The log file is opened with `0o600`
  only, in all three code paths (initial open, rotating-writer open,
  post-rotation reopen) — no `fsguard.RestrictToOwner` call.
- **Failure scenario:** Content is currently metadata-only, so exposure today
  is low — but on a shared Windows host another local account can read the log
  file, and a future `logger.Debug` call anywhere in the tree that logs
  anything sensitive would silently inherit this weaker permission model.
- **Status:** FIXED. `fsguard.RestrictToOwner` is now called at all three
  open paths (`New`'s non-rotating open, `newRotatingWriter`'s initial open,
  `rotateLocked`'s post-rotation reopen). Pinned by
  `TestLogFileAppliesPermissionHardening` (`internal/logging/logging_windows_test.go`,
  asserts the DACL directly, one subtest per open path) and
  `TestNewAppliesPermissionHardening` (`internal/logging/logging_test.go`,
  cross-platform smoke).

### GAP-3.2 — Spilled tool output skips Windows ACL hardening, and is realistically sensitive (Medium)
- **File:** `internal/tool/builtin/spill.go:103`.
- **Description:** Spill files are raw, unredacted dumps of tool output (by
  explicit design, for full recoverability) written at `0o600` only — no
  `fsguard.RestrictToOwner` call, unlike every DB/token file in the codebase.
  This is more material than GAP-3.1 because the content is realistically
  sensitive: a model running `cat .env`, `env`, or a shell command whose output
  embeds an API response with a bearer token, once large enough to exceed the
  inline result cap, gets spilled verbatim into
  `<workspace>/.aegis/spill/*.txt`.
- **Failure scenario:** On a shared Windows host, another local account with
  read access to the workspace directory reads a spilled file and recovers a
  credential that was never meant to leave the current tool call's inline
  result.
- **Status:** FIXED. `spillText` now calls `fsguard.RestrictToOwner` on the
  file right after `os.WriteFile` succeeds, best-effort like every other
  failure path in this file. Pinned by
  `TestSpillWriteAppliesPermissionHardening`
  (`internal/tool/builtin/spill_windows_test.go`, asserts the DACL directly)
  and `TestSpillWriteSucceedsWithHardening` (`internal/tool/builtin/spill_test.go`,
  cross-platform smoke).

## Phase 6 — Permission/trust/lifecycle audit (previously-unaudited packages)

Covers `internal/permission`, `internal/mcp`, `internal/acp`, `internal/sandbox`
(the package's own internals, not just its call sites — those were already
covered in Phase 1), `internal/checkpoint`, `internal/cron`,
`internal/workspacetrust`, `internal/guard` — named in CLAUDE.md's package
table but not independently audited by Phases 1-5. **Result: no confirmed
gaps.**

Verified as OK (no gap):
- `internal/permission/permission.go`'s mode/capability decision table matches
  its doc comment exactly (plan: read+spawn allow, network ask, write/execute
  deny; build: execute ask, else allow; auto: all allow); `Gate.Check` uses
  `tool.EffectiveCapability`, not a tool's static worst-case capability,
  matching the documented P25.4c rationale.
- `internal/permission/contextual.go`'s `EgressThenWrite`/`NetworkAllowList`
  rules are correctly scoped and honestly self-documented as fetch-layer-only,
  explicitly not a shell/exec (`CapExecute`) firewall — a stated, deliberate
  limitation, not an undocumented gap.
- `TestEveryEngineCallSiteDecidesItsGate`
  (`internal/enginecfg/callsite_test.go`) statically scans every `engine.New(`
  call site project-wide (confirmed coverage of `internal/server`,
  `internal/cli`, `internal/drive`, `internal/toolcallprobe`, `internal/eval`)
  for a `enginecfg.BuildGate`-sourced Gate or a documented exemption — the
  P66.13 bypass class is structurally enforced, not just convention.
  `internal/acp/agent.go` and `internal/cron` never construct an engine
  directly; both route through the daemon's existing HTTP session/message path
  (cron's shell-command jobs go straight to `sandbox.Backend.ExecStreaming`,
  bypassing an engine/tool-registry entirely), so neither introduces a second,
  ungated engine construction site.
- `internal/server/helpers.go`'s `cronPermCheck`/`newCronRunFunc`: a cron
  job's `auto_approve` only resolves *Ask*-tier decisions (via
  `permission.AutoApprove{}` as the approver) inside the same full gate stack
  (`s.buildGate`) an interactive run gets — mode, text rules, and contextual
  egress/network policy all still apply, and every fire (blocked, ok, error)
  is durably audited via `cron_runs`. This is the documented P27.15/FIND-08
  fix, implemented as described.
- `internal/workspacetrust/workspacetrust.go`: `Store.Trust`/`Check` correctly
  implement the fingerprint-pinned model (P66.25/SEC-07) — a pre-fingerprint
  or moved-fingerprint grant reads as `Stale` (treated as `Untrusted` for
  gating), never silently `Trusted`. The store file is written `0o600` +
  `fsguard.RestrictToOwner` (doesn't repeat GAP-3.1/3.2's omission).
- `internal/checkpoint/checkpoint.go`'s `RestoreFiles` (P70.1): validates
  every captured path against the checkpoint's recorded `workspace_root` via a
  real symlink-resolving containment check (`withinRoot`/`resolveForCompare`,
  same discipline as `sandbox.ValidatePathIn`) *before* writing anything,
  all-or-nothing — a checkpoint with no recorded root (pre-P70.1, or empty) is
  refused outright rather than trusted.
- `internal/guard/guard.go`: transport failure fails open (documented,
  correct — a flaky validator must not block the user), but an ambiguous or
  malformed verdict fails *closed*, the correct posture since that shape is
  indistinguishable from a successful prompt injection in judged content.
  Untrusted content is angle-bracket-escaped before being wrapped in
  `<output>`/`<file>` tags, preventing a fake closing-tag injection.
- `internal/sandbox/pathvalidator.go`: `ValidatePath`/`ValidatePathIn`
  genuinely resolve symlinks on the real filesystem (not just lexical
  `..`-stripping) and check containment per-root against each root's own
  `EvalSymlinks` identity — matches Phase 1's characterization exactly; this
  is the primitive Phase 1 already validated all 46 builtin tools route
  through, and GAP-1.1 (above) found *not* consistently applied outside
  `internal/tool/builtin`.
- `internal/mcp/http.go`: outbound HTTP (streamable-HTTP MCP servers) reuses
  the same private/loopback/link-local dialer guard as `web_fetch`, not a
  separate, possibly-weaker SSRF check.
- `internal/acp/agent.go`: shared-secret auth
  (`crypto/subtle.ConstantTimeCompare`) gates `session/new` and
  `session/prompt` when a token is configured; the CLI's `aegis acp` entry
  point always resolves a non-empty token, so the package-level "empty token =
  unauthenticated" default is never what actually ships.

No GAP-6.N entries — this phase found nothing to add to the log.

**Scope note (resolved):** `internal/permission/rules.go` and
`internal/permission/scope.go` were not read in depth in the initial Phase 6
pass. A follow-up deep read confirmed both hold up: `RuleGate.Check`'s
deny-then-allow-then-defer precedence has no ordering bug; deny matching
deliberately over-matches via a broad regex while allow matching against
execute-capability tools excludes shell metacharacters from wildcard
expansion (`allow bash(npm test*)` cannot be widened into a chained
command); `normalizePathLike` case-folds and lexically cleans traversal
symmetrically before comparison; `matchesBulkScope` denies on any possible
overlap and only clears an allow for an unconstrained `*` pattern; `ScopeGate`
denies by default and gates on `tool.EffectiveCapability`, not a tool's
static capability; `s.permRules` is copied into a fresh slice per engine
build before constructing a `RuleGate`, so no runtime-mutation race. No
`%v`-vs-`%w` issues either. Status: CLOSED — no gap found.
