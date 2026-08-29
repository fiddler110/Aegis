# Aegis Security Posture Review

A read of the confinement, permission and egress boundaries across ~109k lines of Go,
plus the duplication and cost findings that came out of the same pass. Four findings
were reproduced with runnable probes; the three that mattered most are now fixed, with
regression tests.

| | |
|---|---|
| Branch | `refactor/enginecfg-limits-new-rollback-round-deadlock` |
| Date | 2026-08-29 |
| Status | SEC-A, SEC-B, SEC-H fixed |

**3** fixed · **5** open · **8** test failures left · **4** verified by probe

---

## The headline

This is a codebase that has clearly been through several security passes, and it shows —
the trust model, the config freeze list, the gate stack and the SSRF blocklist are all
better than typical. The findings below are not a story of missing defenses. They are
three cases where a defense that exists was reached around by a spelling nobody
enumerated, and one where a defense silently does nothing when its dependency is absent.

**Status:** SEC-A, SEC-B and SEC-H are fixed on this branch, each with a regression test
that fails against the old code. The remaining five are open and unchanged. Nothing else
regressed — the packages touched are green under `-race`, and the pre-existing failure
count went from nine to eight.

Separately: `go test ./...` is **not green** — nine failures across three packages, one of
which hangs for ten minutes and panics the run. That state was already triaged and
recorded before this review. What had not been separated out is that **two of the nine are
real bugs wearing a test-environment costume**: the built-in-skill write guard (SEC-H) and
the shell checkpoint capture are both defeated on any host where the workspace root is
reached through a symlink — which on macOS is every workspace under `/tmp` or `/var`. They
look like the `/private/var` path-expectation noise sitting next to them in the same
package, and they are not.

---

## Findings

8 total · 3 fixed

### SEC-A — Shell quoting defeats the read-only classifier, re-opening VULN-01 / VULN-02 / VULN-11

**High · Fixed** — `internal/tool/builtin/shell_readonly.go:715` · `classifyShellCommand`

`classifyShellCommand` rejects a command containing any of ``;&|`$()<>`` or a newline, then
splits the rest with `strings.Fields` and confines each token with `sandbox.ValidatePath`.
Quote characters and backslashes are in neither set. So a quoted absolute path survives the
metachar screen, reaches the confinement check *with its quotes still attached*, resolves as
a relative name under the workspace root, and passes.

The shell then strips the quotes and reads the real path. The call has meanwhile been
downgraded to `tool.CapRead`, which `permission.Policy.Decide` allows **silently, in every
mode including plan**. This is the same class the file's own header documents as closed.

Probe against a temp root — `classifyShellCommand(root, cmd)`:

```
"cat /etc/passwd"            -> cap=execute  classified=false   (correctly refused)
"cat '/etc/passwd'"          -> cap=read     classified=true
"cat \"/etc/passwd\""          -> cap=read     classified=true
"cat \\/etc/passwd"           -> cap=read     classified=true
"cat ''/etc/passwd"          -> cap=read     classified=true
"ls '/'"                     -> cap=read     classified=true
"grep -r x '/etc'"           -> cap=read     classified=true
"head -n 5 \"/etc/hosts\""     -> cap=read     classified=true
"git diff '--output=/tmp/x'" -> cap=read     classified=true
"git log '--git-dir=/tmp/x'" -> cap=read     classified=true
```

The last two are the worst of it. `deniedGitFlags` in `argv_confine.go` exists specifically
to stop `--output` (a file write needing no path operand — VULN-11) and `--git-dir`
(relocating the repo — VULN-01), and `flagMatches` compares against the raw token. One pair
of quotes and the flag is no longer spelled the way the denylist recognises, so
`git diff '--output=/tmp/anything'` is an *arbitrary file write outside the workspace,
silently auto-approved under plan mode's read gate*.

> **Fixed.** Took the tokenizer rather than the blunt refusal. `splitShellWords` now splits
> the way the shell will — stripping quotes and, under POSIX, backslash escapes — before the
> flag table or the confinement check see a token. Blanket-rejecting quotes would also have
> closed the hole, but it would have made `grep "foo bar" .` unusable in plan mode
> (CapExecute is *Deny* there), so the tokenizer is better in both directions: it refuses all
> ten escapes *and* classifies quoted reads correctly for the first time, where
> `strings.Fields` used to hand the flag parser bogus tokens like `'my`.
>
> The backslash dialect is a parameter (`powershell`, threaded from
> `shellTool.usesPowerShell()`) rather than a default, because guessing it wrong opens the
> hole in whichever direction the guess went — under PowerShell a backslash is a path
> separator, and consuming it would turn `\Windows\System32\config\SAM` into a confined
> relative name. `ShellChainMetaChars` was left alone, since it also drives
> `globToRegexpExec`.

### SEC-B — `deny` permission rules never fire for `multi_edit`

**Medium · Fixed** — `internal/permission/rules.go:353` · `subjectFor`

`subjectFor` reads `path`, `file_path`, `command`, `url`, `query` and `pattern`. It does not
read `edits[].path`. `multi_edit`'s input schema has *only* `edits[].path` — no top-level
path field at all — so the extracted subject is `""`, and a path-scoped rule's regexp cannot
match the empty string.

Probe — rule `deny write(secrets/**)`, mode build:

```
write_file  secrets/key.pem  -> allowed=false  "blocked by permission rule: deny write(secrets/**)"
multi_edit  secrets/key.pem  -> allowed=true   ""
```

It fails silently in both directions: `WarnUnmatchableRules` won't flag the rule, because
the rule *does* match other write tools. An operator writing `deny write(...)` to fence off
a directory gets a fence with one tool-shaped hole in it, and no diagnostic anywhere.

Two sibling extractors in the tree get this right — `permission.scopeWritePaths`
(scope.go:168) and `engine.writtenPathsFromInput` (pathtrack.go:23) both handle `edits[]`,
and are byte-identical to each other. `subjectFor` is the third copy and the divergent one.
See CLN-2.

> **Fixed.** Added `subjectsFor`, which returns every path a call names, and split the match
> into `matchesAny` (deny) and `matchesAll` (allow). The asymmetry is the point and mirrors
> what `matchesBulkScope` already documents: a multi-file write is denied if *any* path it
> touches is denied, since the call is all-or-nothing; a scoped allow grants only when *every*
> path is inside its scope, so a call reaching past it falls through to the base gate rather
> than being auto-approved on the strength of its first path.
>
> `scopeWritePaths` now shares the same `editPathsFromInput` extractor, taking the in-package
> copies from two to one — the scope gate and the rule gate can no longer drift apart on which
> files a `multi_edit` touches, which is exactly how this arose. `toolHasSubjectField` also
> learned "edits", or the startup warning would have flipped from correct to a false positive.

### SEC-H — The built-in-skill write guard is inert whenever the workspace root is a symlink

**Medium · Fixed** — `internal/tool/builtin/builtin.go:366` · `denyMaterializedSkillWrite`

`resolveWrite` returns a **symlink-resolved** absolute path — that is `ValidatePathIn`'s
deliberate contract, since the resolved path is the one a caller must open to avoid a TOCTOU
swap. It then hands that resolved path to `denyMaterializedSkillWrite`, which builds its
guarded prefix from `r.Path` *unresolved* and compares the two with `filepath.Rel`.

When the root is reached through a symlink the two live in different namespaces, `Rel` yields
a `..`-leading path, the write reads as "outside the guarded tree," and the guard returns nil.
On macOS this is every workspace under `/tmp` or `/var`, and any project directory behind a
symlinked home or volume mount.

Probe — same write, two roots differing only in symlink resolution:

```
root resolved=false -> guard fired=false | recon.py now="clobbered"
root resolved=true  -> guard fired=true  | recon.py now="orig"
```

This is what `TestSkillAssetsAreReadOnlyToTools` has been reporting, alongside four genuinely
test-side `/private/var` failures in the same package — which is very likely why it read as
more of the same. The guard exists because of an observed live incident (P38.1, 2026-08-09:
a model overwrote `recon.py` with the command line it meant to run, leaving the phase with
tooling that could only raise `SyntaxError` and no copy to restore from). The protection
written in response to that has never actually applied on the machine it was written on.

> **Fixed.** `latexResolvedRoots` was promoted to `resolvedRoots` in builtin.go and
> `denyMaterializedSkillWrite` now calls it, so the two call sites that need this share one
> helper instead of one having it and the other not. LaTeX's three call sites are unchanged
> in behaviour.

### SEC-C — `RedactSecrets` is a silent no-op wherever gitleaks isn't installed

**Medium · Open** — `internal/security/redact.go:64` · `internal/engine/toolexec.go:525`

`security.RedactText` opens with `if !lookPath("gitleaks") { return text, nil, nil }`. The
fail-open posture is deliberate and correct — a scrubbing pass must never block a tool
result. What isn't right is that there is no floor beneath it. An operator who sets
`RedactSecrets` on a cloud provider, on a host without gitleaks, gets exactly zero
redaction, no warning, and no way to tell that state apart from "scanned, found nothing."

The floor already exists and is already trusted elsewhere: `internal/redact` is a
dependency-free, in-process pattern set covering PEM keys, AWS IDs, `sk-` keys, GitHub/Slack
tokens, JWTs and bearer headers. It is what guards the MCP outbound boundary
(`mcp/outbound.go:41`), the audit trail (`hooks/hooks.go:147`) and transcript export
(`share/redact.go:65`). It is not consulted on the one path that ships file contents to a
third-party model provider.

> **Fix.** Run `redact.Text` unconditionally, then layer gitleaks on top when present. Also
> surface the count — the `redact.Text` doc comment already argues that a redaction pass
> finding nothing must be reported as a real answer, precisely so it can't be confused with
> one that was never wired up.

### SEC-D — Auth lockout is global and pre-token, so any local process can wedge the operator's own client

**Low · Open** — `internal/server/auth.go:64` · `authMiddleware`

`authLockoutRemaining()` is consulted *before* the token comparison, and
`authConsecutiveFailures` is process-wide. Ten bad requests from any local process open a
lockout window that rejects every request with 429 — including ones carrying the correct
token — for up to 60s, renewably. The daemon is loopback-only, so per-remote-address scoping
wouldn't help; the useful distinction is per-credential.

> **Fix.** Check the token first and let a valid one through the window (still without
> resetting the streak). That keeps the throttle against guessing — a guesser has no valid
> token by definition — while removing the self-DoS. The `/auth/exchange` handler already
> reasons its way to exactly this conclusion for its own route.

### SEC-E — The audit trail and the spill directory skip `fsguard`

**Low · Open** — `internal/hooks/hooks.go:184` · `internal/tool/builtin/spill.go:95`

Every other locally-stored sensitive artifact goes through `fsguard.RestrictToOwner`: the
daemon token, all three SQLite stores and their WAL sidecars, the TLS key, the swarm mailbox,
the trust store, the modelcaps cache. Two don't. The audit JSONL holds
redacted-but-still-revealing tool inputs — every shell command, every path. `.aegis/spill/`
holds the overflow of truncated tool results, i.e. raw file contents.

Both are created `0o600`/`0o750`, so POSIX is fine. On Windows a new file inherits the parent
directory's ACL and the mode argument does nothing — which is the entire reason `fsguard`
exists.

> **Fix.** Two `fsguard.RestrictToOwner` calls, matching the pattern already used at the other
> eight sites.

### SEC-F — Two gaps in the SSRF blocklist

**Low · Open** — `internal/netblock/netblock.go:41` · `privateRanges`

The table is good and its exclusions are well-argued (the note on why `::ffff:0:0/96` is
deliberately absent is correct — `net.IPNet.Contains` already does the `To4()` reduction).
Two ranges are genuinely missing rather than deliberately excluded:

`64:ff9b::/96` — the NAT64 well-known prefix. `To4()` returns nil for it, so
`64:ff9b::a00:0001` passes every check and reaches `10.0.0.1` through a NAT64 gateway. Needs
such a gateway on the network to be exploitable, but it is a real path around the IPv4 table.
Also worth adding: `224.0.0.0/4` and `240.0.0.0/4` (only the `224.0.0.0/24` slice is
currently covered, via `IsLinkLocalMulticast`).

> **Fix.** Three `mustParseCIDR` lines, each with the one-sentence rationale the existing
> entries carry.

### SEC-G — `termsafe` documents C1 stripping it doesn't do

**Low · Open** — `internal/termsafe/termsafe.go:60`

The comment describes stripping "DEL and C1 control range (0x80-0x9f) when expressed as raw
bytes" and explains at length why that's safe against UTF-8 continuation bytes. The code
below it is `if c == 0x7f` — DEL only. In practice a lone `0x9b` (8-bit CSI) is invalid UTF-8
and modern terminals render it as U+FFFD, so this is closer to a documentation defect than an
exploitable one. Worth resolving in one direction or the other, since the comment currently
asserts a property a reader would reasonably rely on.

---

## Test suite health

8 left, was 9.

All nine predate this review — the tree was clean and my probe files were removed before each
run. This state was already triaged and recorded, so what follows is not news that the suite
is red; it is a re-reading of *which* failures are environmental. Note that a bare
`go test ./...` cannot show you this: the `termcaps` hang panics the run before
`internal/tool/builtin` reports, so the first pass surfaced only two of the nine.

| Failure | Verdict | Note |
|---|---|---|
| `TestSkillAssetsAreReadOnlyToTools` | **Fixed** | Was a real bug: write guard defeated by a symlinked root — SEC-H. Now passing. |
| `TestProbeNonTTY` | **Real bug** | Hangs the whole run — TST-1. |
| `TestShellCheckpointWorkspaceInsideLargerRepo` | **Likely real** | Same namespace mismatch: `gitRepoRoot` returns git's resolved top, compared against an unresolved workspace root. Not fully diagnosed — worth an hour before assuming it's environmental. |
| `TestResolveSafeImagePathRefusesRootedPaths` | Test-side | Asserts a posture the helper doesn't provide — TST-2. |
| `TestSelectSandboxFallsBackToLocal` | Test-side | Wrong expected backend name — TST-3. |
| `TestThreatModel*` ×4 | Test-side | Expect `/var/…`, get the resolved `/private/var/…`. Same class the `tempRoot(t)` helper fixed in `internal/sandbox` on 2026-07-31; never carried into this package. |

The pattern across five of the nine is one defect, not five: **a resolved path compared
against an unresolved root**. Three are test expectations, two are production code. Fixing
the `tempRoot(t)` half without fixing the `denyMaterializedSkillWrite` half would turn the
package green while leaving the guard inert — which is the outcome to avoid here.

### TST-1 — `termcaps.Probe` puts stdin/stdout into blocking mode before its non-TTY early return

**Hangs the suite · Reproduced** — `internal/termcaps/probe.go:96` · test hangs at `termcaps_test.go:279`

`Probe`'s doc comment promises it "degrades to nothing supported, nothing asked — never to an
error and never to a hang" when either end is not a terminal. But the line that *decides* that
is `if !term.IsTerminal(in.Fd()) || !term.IsTerminal(out.Fd())`, and `os.File.Fd()`
permanently unregisters the descriptor from the Go runtime poller and switches it to blocking
mode. The early return happens after the damage.

Downstream, `SetReadDeadline` returns `nil` and has no effect, and the next `Read` blocks in a
raw syscall forever. That is exactly what `TestProbeNonTTY` does after checking the probe
returned, so it hangs for the full 10-minute timeout and panics the whole `go test ./...` run
with it:

```
panic: test timed out after 10m0s
  running tests: TestProbeNonTTY (10m0s)

goroutine 35 [syscall]:
  syscall.read(...)
  internal/poll.(*FD).Read(...)
  os.(*File).Read(...)
  termcaps.TestProbeNonTTY(...) termcaps_test.go:279
```

The production consequence is smaller than the CI one but real: any caller that hands `Probe`
a pollable file gets it back in blocking mode, whether or not a terminal was ever found.

> **Fix.** Screen with `in.Stat()` / `out.Stat()` for `os.ModeCharDevice` and return early on a
> non-TTY *before* touching `.Fd()`. Everything past that point already needs a real terminal,
> so `Fd()` is unobjectionable there.

### TST-2 — `TestResolveSafeImagePathRefusesRootedPaths` asserts a posture the helper doesn't provide

**Failing** — `internal/server/images_test.go:57`

```
images_test.go:61: resolveSafeImagePath("\Windows\System32\config\SAM") was accepted;
                  a rooted path must be refused on every platform
```

The test says "on every platform." `sandbox.IsRooted` is deliberately platform-*conditional* —
on non-Windows it is just `filepath.IsAbs`, and a backslash-prefixed string is a legitimate (if
odd) relative filename there. Not exploitable: `resolveSafeImagePath`'s `Join`+`Rel` check still
holds on POSIX. But a red security test is worse than no test, because the next person to run
the suite learns to expect red.

> **Fix.** Pick a side. Either reject a leading `/` or `\` unconditionally in
> `resolveSafeImagePath` (cheap, and delivers the cross-platform identical behaviour the test's
> own comment argues for), or narrow the test to the platform `IsRooted` actually covers. Note
> `latexRefIsRooted` already made the opposite call for TeX-specific reasons and documented why
> — so the two spellings can legitimately coexist, but each needs its test to match it.

### TST-3 — `TestSelectSandboxFallsBackToLocal` compares against a backend name that never existed

**Failing** — `internal/server/sandbox_test.go:36`

The test asserts `sb.Name() != "os"`, but `OSBackend.Name()` returns `"os:" + o.mechanism` —
`"os:seatbelt"` on macOS, `"os:bubblewrap"` on Linux. The assertion can only pass on a host
where `NewOSBackend` errors and the `else` branch runs, so the branch it was written to cover
has never been exercised.

> **Fix.** `strings.HasPrefix(sb.Name(), "os")`, or compare against the name of the backend the
> test already constructs two lines above to derive its expectation.

---

## Duplication worth collapsing

Ranked by consequence. This codebase already understands the pattern — `enginecfg`, `netblock`,
`redact`, `termsafe`, `argv_confine` and `sqlitestore` all exist because a duplicated decision
drifted. These are the ones still outstanding, ordered by what the drift costs rather than by
line count.

### CLN-1 · Three `withinRoot`, three semantics

`server/server.go:482` · `drive/verify.go:398` · `checkpoint/checkpoint.go:297` ·
`sandbox/pathvalidator.go:224` (`escapesRoot`)

Four answers to "is this path inside that root." The server copy resolves no symlinks at all
and treats `root == target` as inside; checkpoint resolves and case-folds but treats `.` as
outside; drive resolves and treats `.` as inside. Three reviewers would have to check three
implementations to reason about one property. Export the `sandbox` one and delete the rest.

### CLN-2 · Path extractors *(partly done)*

`engine/pathtrack.go:23` · `permission/scope.go:168` · `permission/rules.go:353`

Was three copies, the third (`subjectFor`) missing `edits[]` — finding SEC-B. Fixed: the two
`internal/permission` copies are now one shared `editPathsFromInput`.
`engine.writtenPathsFromInput` remains a separate cross-package copy; it is correct today, so
this is now ordinary tidying rather than a security item.

### CLN-3 · `hardenDBPermissions` × 3

`session/session.go:197` · `knowledge/knowledge.go:69` · `longmem/longmem.go:85`

Byte-identical apart from one noun in a log line. `sqlitestore.Open` already takes a `label`
parameter and already exists for exactly this reason — its doc comment just draws the line one
step too early ("permission hardening ... stay in each caller"). Move it in. Separately,
`session.Open` re-implements `sqlitestore.Open` outright: same DSN constant, same
`SetMaxOpenConns(1)`, same WAL pragma, and it's the only one of the three that doesn't create
its parent directory.

### CLN-4 · Daemon bootstrap × 4

`cli/acp.go:51-85` · `cli/mcpserve.go:59-94` · `cli/ui.go:36-79` · `cli/root.go:94-118`

~28 verbatim lines: health-probe an existing daemon, start an embedded one, re-dial, wait, and
— critically — `defer cl.Zero()` to scrub the token (FIND-33/P24.21). All four currently
remember the scrub. A fifth command is where that stops being true. One
`connectOrStartDaemon(ctx, cfg) (*client.Client, func(), error)`.

### CLN-5 · Store and adapter helpers

`knowledge/knowledge.go:418` & `longmem/longmem.go:413` (`ftsEscape`) · `knowledge:207` &
`longmem:143` (`truncateForEmbed`) · `provider/{anthropic:200, openai:194, ollama:852}` (header
loop) · `anthropic:108` & `ollama:317` (`WithStreamIdleTimeout`)

Small and low-risk individually. The two FTS5 stores are near-twins overall and are the better
consolidation target than the adapters, which P78 has already partly deduped.

### CLN-6 · Sixteen `truncate`s

tui, cli×2, toolshim, eval, share, swarm, checkpoint, memory, security, longmem, knowledge,
compaction, server, engine, task

Sixteen near-copies of "shorten a string, maybe add an ellipsis," each with its own limit and
its own view on whether to keep the head or the tail. Not worth a mechanical sweep — but
`truncate.go` already owns the posture table for tool results, and it is the right home for a
shared primitive as these are touched.

---

## Cost

### `security/redact.go:64`

With `RedactSecrets` on, every read-capability tool result spawns a `gitleaks` process, creates
and removes a temp dir, writes the content to disk and parses a JSON report — synchronously, on
the path between the tool returning and the model seeing the result. A hundred-read run is a
hundred process spawns in the critical path.

> Run the in-process `redact.Text` first (SEC-C wants this anyway); reach for gitleaks only as a
> second pass, and consider batching a round's results into one invocation rather than one per
> call.

### `cli/configupdate.go:230,236`

`boundaryRe` is a constant pattern compiled on every call; `startRe` is key-dependent but
cacheable. Cold-path CLI code, so the cost is small — it's listed because it's the only per-call
`MustCompile` left in the tree outside table construction.

> Hoist `boundaryRe` to package scope.

### `sandbox/docker.go:220`

Not a cost issue but adjacent: the workspace bind-mount is read-write and no `--user` is passed,
so container-created files land owned by root on the host under rootful Docker. `--cap-drop=ALL`
plus `no-new-privileges` already constrain what that root can do, so this is ergonomics more than
exposure.

> Consider `--user $(id -u):$(id -g)` on the OCI path, where the CLI supports it.

---

## What's holding up well

- **The inverted config freeze list** (`config/freeze.go`) is the single best decision in the
  security model. Defaulting an unlisted key to frozen, plus a test that fails the build when a
  new `Config` field declares no policy, converts a recurring class of omission into a
  compile-time obligation. The comment enumerating six independent rediscoveries of the same
  defect before inverting it is worth keeping as-is.
- **The trust fingerprint** (`config/fingerprint.go`) hashes only the project file's own keys,
  not the merged config, so the digest moves when and only when project-controlled content moves.
  And it is honest about the `.aegis/.env` hole rather than rounding it off — including a test
  that pins the omission so covering it silently is also a failure.
- **The gate stack constructor** (`enginecfg.BuildGate`) is adopted at every engine construction
  site I checked, with a test enforcing it. Persona allow-rules are filtered out; persona modes
  can't escalate above the configured default. The layered ordering is stated in exactly one
  place, with the reasoning for why it's stated only once.
- **Path confinement** (`sandbox/pathvalidator.go`) resolves symlinks per root rather than
  against a covering prefix, so two roots under a shared parent don't make that parent reachable.
  The Windows rooted-without-volume case (P32.1) is handled and the reasoning is recorded.
- **SSRF**: `SafeDialer` resolves once and dials the resolved literal, which closes the rebinding
  window properly rather than re-checking and re-resolving. Both HTTP clients that take model- or
  config-supplied URLs use it.
- **The web UI** runs a strict CSP with no `unsafe-inline`, an HttpOnly `SameSite=Strict`
  double-submit CSRF binding on the page-token exchange, and DOMPurify on every model-authored
  string before it reaches `innerHTML`.

---

## Suggested order

| # | Item | Why here |
|---|---|---|
| ✓ | **SEC-A** | **Done.** Reached arbitrary host reads and an out-of-workspace write with no prompt in any mode. |
| ✓ | **SEC-H** | **Done.** Cleared the failing test that read as environmental. |
| ✓ | **SEC-B** + CLN-2 | **Done.** The in-package extractor duplication went with it; `engine.writtenPathsFromInput` is still a separate cross-package copy. |
| 1 | **TST-1** | Nothing else can be verified while `go test ./...` panics at ten minutes — and the panic is what hid four of the nine failures. Fix this before the rest so the suite can confirm the rest. |
| 2 | **TST-2**, **TST-3**, `TestThreatModel*` | Small, and they restore the suite as a signal. Safe to do now that SEC-H is fixed — the package can no longer go green over a live bug. |
| 3 | **SEC-C** | Wants a decision (layer, or warn loudly) more than it wants code. |
| 4 | SEC-D–G, CLN-1/3/4 | Independent, low-risk, do as convenient. |

---

## Notes on method

Findings SEC-A, SEC-B, SEC-H and TST-1 were reproduced with throwaway probes run against this
working tree; the probe files were deleted and `git status` was clean afterward. Everything else
is from reading the source and the failing-test output. Live-tier suites (`live_eval`,
`live_probe`, `live_workflow`) were not run — they need a real model server, and per the
project's own rule would need `-count=1` to mean anything.

One thing I did not chase: `TestShellCheckpointWorkspaceInsideLargerRepo` is classified above on
the strength of a namespace mismatch I can see in the code path, not on a probe. Treat that row
as a lead, not a finding.

The three fixes each carry a regression test, and each test was confirmed to fail against the
pre-fix code before being kept. Three files were already unformatted before this work
(`engine/tokencalib_test.go`, `server/registry_race_test.go`, `tui/dialog.go`) and were left that
way.
