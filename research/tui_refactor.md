# `internal/tui/tui.go` cleanup — 2026-08-24

`tui.go` had grown to 2,612 lines as a catch-all for code that no longer needed to live there — most
of it self-contained subsystems with their own home files (or an obvious one) already sitting next to
it in the package. This pass did the mechanical part: moving existing code, unchanged, to files
matching its concern. No behavior changed; `go build ./...`, `go vet ./internal/tui/...`, and
`go test ./internal/tui/...` all pass before and after.

## Done — pure code motion

| Moved | From | To |
|---|---|---|
| `toolEntry`, `toolCard`, `toolGroup`, `toolBlock` | `tui.go` | new `toolcard.go` |
| `handleTerminalKey` | `tui.go` | `terminal.go` (existing) |
| `applySwitchedSession`, `loadHistory` | `tui.go` | `update_session.go` (existing) |
| `@image:`/`@shell` token parsing (`extractImageRefs`, `imageExts`, `looksLikeImagePath`, `attachTokenFor`, `resolveAttachPath`, `extractShellRefs`, `shellRefText`, `lastNLines`, and their regexes/consts) | `tui.go` | new `attach.go` |
| Welcome-screen rendering (`buildWelcomeContent`, `getUsername`, `shortenPath`) | `tui.go` | new `welcome.go` |
| Generic helpers (`wrap`, `truncate`, `oneLine`, `short`, `contextWindowFor`, `renderContextBar`) | `tui.go` | new `helpers.go` |

`tui.go` dropped from 2,612 to ~2,030 lines after this pass. Every moved doc comment (including the
P-numbered rationale comments) was preserved verbatim — they carry load-bearing context per this
repo's convention, not paraphraseable summary.

`newGlamourRenderer` and `contextWindowSize` (a `model` method) stayed in `tui.go` — they weren't part
of the original scope and don't share a natural home with the moved helpers.

## Also done — P77.2, P77.3, P77.4, P77.5 (2026-08-24, same day, follow-on sessions)

The four deferred items below were picked up the same day at the user's request to work through the
whole batch. Full narrative: [releases.md](research/releases.md#p774-shipped-2026-08-24) (P77.4) and
[releases.md](research/releases.md#p772-and-p773-shipped-2026-08-24) (P77.2/P77.3).

- **P77.5 shipped** — every `*Msg` type declaration (`statusInfoMsg`, `streamStartedMsg`, `eventMsg`,
  `bangMsg`, `teammatesMsg`, and a dozen more) moved into a new `messages.go`. Pure code motion.
  `tui.go`: ~2,030 → 1,862 lines.
- **P77.2 shipped** — `model`'s tool-tracking fields (`pendingTools`, `pendingToolOrder`,
  `pendingToolSeq`, `toolBlocks`, `activeReadGroup`, `soloReadCard`, `soloReadEntry`,
  `pendingReadPaths`) now live on a `toolState toolState` field; the streaming-phase fields
  (`streamStart`, `firstTokenAt`, `outBytes`, `modelWaitAt`) now live on a `phase streamPhase` field.
  Done as two separate incremental steps (streamPhase first, toolState second), each verified with a
  full build/vet/test pass before the next started, given the field reads spread across a dozen+
  files. The top-level Elm `model` type itself is unchanged. `tui.go`: 1,862 → 1,874 lines (the two new
  struct definitions cost slightly more than the field-list compaction saved).
- **P77.3 shipped** — `sandbox.shellCommand` exported as `sandbox.ShellCommand`; `internal/tui`'s
  `bangShellCommand` and `internal/security`'s `shellInvocation` deleted in favor of calling it
  directly. Also found and consolidated a **fourth** copy the original filing didn't name —
  `internal/hooks`' own `shellCommand`. All four were confirmed byte-identical in behavior before
  unifying.
- **P77.4 initially left parked**, then shipped after all — reviewed against its own promotion
  criteria first (an eighth near-identical constructor appearing, or a bug needing the same fix
  across all seven); neither had happened, so it was left as-is at first. The user then asked for it
  directly, and a `fetchCmd[T any](timeout, fn, wrap) tea.Cmd` generic now backs the three constructors
  that were a genuine single-call round trip — `fetchTeammates`, `fetchSessions`, `switchSessionCmd`
  — plus `fetchTeammatesQuiet`. `fetchBacktrackTargets`/`forkAndSwitchCmd` (a second dependent call
  plus branching) and `startStream`/`startDrive` (`context.WithCancel`, not a timeout) stayed literal,
  matching the original filing's own caution about forcing every one of the seven through a shared
  shape.

Final verification after all of the above: `go build ./...`, `go vet ./...`, and `go test ./...`
(whole repo, all non-live tiers) all clean; `gofmt -l` clean on every touched file.
