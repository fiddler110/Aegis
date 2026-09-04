# P67.13 — Copy-on-write plan overlay: design note

**Status: design only. No code exists. Written 2026-09-03 at the user's direction, as the
"design doc only" option against roadmap item
[P67.13](roadmap.md#p6713--there-is-no-way-to-execute-a-plan-without-committing-to-it).**

This document exists so that the build is cheap the day a consumer appears. It is not a
commitment to build, and it does not change P67.13's tier: the item's own promote-when
condition — *the overlay has a named first consumer* — has **not** fired. The roadmap's warning
still governs everything below: building the overlay with no consumer produces an untested second
write path, which is strictly worse than not having it.

Nothing here is transcribed from the leaked comparison source that motivated the P67 batch. The
mechanism is described from Aegis's own seams, which are named at file and line throughout.

---

## 1. The problem, stated precisely

Plan mode describes intent. It cannot show the diff that intent would produce, because producing
the diff means performing the writes.

That is not an incidental limitation. Plan mode's read-only guarantee runs through
`classifyShellCommand` and `permission.Gate.Check`, which consults `tool.EffectiveCapability`
*before* `Policy.Decide` (CLAUDE.md, "Plan mode's guarantee runs through a parser"). A plan-mode
run is read-only by construction: `CapWrite` and `CapExecute` are denied, so there is no execution
of the plan to observe. The user's only options today are to read the prose plan and guess, or to
leave plan mode and let the writes land in the real tree with `internal/checkpoint` as the undo.

Checkpoint-as-undo is a genuinely different thing from an overlay, and the difference is the whole
point of this item:

| | checkpoint + rewind | overlay |
|---|---|---|
| when the effect lands | in the real tree, immediately | never, unless accepted |
| what the user reviews | the tree after the fact | a diff before anything lands |
| cost of rejecting | restore N file blobs, and see P60.3 — non-file effects are **not** undone | delete a directory |
| honest about `pip install` | no (P60.3) | yes — it *refuses to run it*, and says so |

The last row is the one that matters most. P60.3 is open precisely because rewind is silent about
everything that is not a file. An overlay does not solve that by undoing non-file effects; it
solves it by **refusing to perform them and recording where it stopped**. That is a weaker promise
that can actually be kept, which is why it is the right one.

---

## 2. Mechanism

Four parts. Only the first is novel; the other three are existing seams doing what they already do.

### 2.1 The overlay itself

A directory outside the workspace (under the session's data dir, not under the workspace root —
otherwise `glob`, `grep` and `repomap` walk into it and the model reads its own speculative writes
as if they were the repo).

- **Write to path P**: if P is not yet in the overlay, copy the real file in first (copy-on-write,
  preserving mode), then apply the write to the overlay copy. Record P in a claimed set.
- **Read of path P**: if P is in the claimed set, serve from the overlay. Otherwise read through to
  the real tree.
- **Delete of path P**: record a tombstone in the claimed set; reads of P then report absent, and
  read-through is suppressed.
- **Accept**: rename/copy each claimed path from the overlay into the workspace, tombstones become
  deletes. This is the only moment a write lands.
- **Discard**: `os.RemoveAll` the overlay directory.

The claimed-set-plus-read-through shape is not new to this tree.
`checkpoint.Snapshotter.claim` (`internal/checkpoint/checkpoint.go:673`) already maintains exactly
such a set — first-touch-wins, so a file captured once in a turn is not re-captured — and
`Snapshotter` is already carried on the context and fetched by write tools via
`checkpoint.WithSnapshotter` / `SnapshotterFrom` (`checkpoint.go:687`, `:693`). **The overlay
belongs at that same seam.**

**Measured 2026-09-03, and it changes the shape of the work.** Every write site does reach the
seam — nine call sites, and they are all of them: `file.go:370` and `:535`, `editsection.go:283`
and `:392`, `fillmarker.go:140`, `multiedit.go:137`, plus `shell.go:193` and `tests.go:76` through
`captureShellWrites`, and `agent.go:46` passing it down to a sub-agent. So the seam is universal,
which is the good half. But the call is `checkpoint.SnapshotterFrom(ctx).Capture(abs)` — the
snapshotter is **notified** of a path about to be written and copies the *old* contents away; the
tool then performs its own `os.WriteFile` against the real path. It observes; it does not
intercept.

That means the overlay cannot simply be installed underneath it. The work is to **upgrade the seam
from notifying to path-resolving**: a single context-carried interface whose method takes the
validated absolute path and returns the path the tool should actually write to, with the checkpoint
capture happening inside it. In the no-overlay case it returns the path unchanged after capturing,
which is exactly today's behavior. Each of the nine sites changes from

```go
checkpoint.SnapshotterFrom(ctx).Capture(abs)   // then write to abs
```

to

```go
abs = checkpoint.SnapshotterFrom(ctx).CaptureFor(abs)   // then write to abs
```

This is still the single most important implementation decision in this note, and it is a
mechanical nine-site change rather than a new write path — but it is *not* zero, and §5 is costed
accordingly. `captureShellWrites` (two of the nine) is the one that needs real thought: it
discovers written paths by observing the filesystem around a shell command rather than being told
one in advance, so it has no path to redirect. In the first version a shell command that writes is
simply a **boundary** (§2.3) — which the conservative `CapExecute` rule already makes it.

### 2.2 Path resolution stays where it is

Overlay path mapping happens **after** `sandbox.ValidatePath` (`internal/sandbox/pathvalidator.go`),
never instead of it. The confinement question ("is this path inside the root?") is answered against
the *real* root, symlink-aware, exactly as today; only then is the validated path translated to its
overlay location. Any design that validates against the overlay root instead re-opens every
containment defect `internal/sandbox` has closed, including the P32.1 Windows-rooted-without-volume
trap and the workspace-symlink escape closed under P66.23/VULN-07.

### 2.3 The boundary — where execution stops

The overlay can contain filesystem writes inside the workspace. It cannot contain anything else.
Execution stops at the first effect it cannot contain, and records a **typed boundary** describing
where and why.

The classification is already available and must not be re-derived. `tool.EffectiveCapability`
(`internal/tool/tool.go:101`) resolves a per-call capability, memoized for the call via
`tool.WithCapabilityMemo` (`:153`) so the overlay's question costs no extra filesystem I/O. Against
the five capabilities (`tool.go:44-48`):

| capability | overlay verdict |
|---|---|
| `CapRead` | contained — read through the overlay |
| `CapWrite` | contained — copy-on-write into the overlay |
| `CapExecute` | **boundary**, unless the call downgrades to `CapRead` via the shell classifier |
| `CapNetwork` | **boundary** — no way to un-send a request |
| `CapSpawn` | **boundary** — a sub-agent has its own engine and its own write path |

The `CapExecute` row is where **P67.8**'s flag-level classifier would earn its keep. Today
`classifyShellCommand` already downgrades a read-only shell command to `CapRead`, and that verdict
is exactly the one the overlay needs — a `shell git status` is containable, a `shell npm install`
is not. Without a finer classifier the overlay is *conservative*, not *wrong*: it stops at commands
it cannot prove are reads. That is an acceptable first version, and it should ship that way rather
than waiting on P67.8.

A write to a path outside the workspace root is also a boundary rather than an overlay write, since
the overlay's accept step has no confined destination for it.

**Boundary record shape** (typed, not prose — the whole point is that the consumer can render it):

```go
type Boundary struct {
    Tool       string          // tool name as the model called it
    Capability tool.Capability // the resolved effective capability
    Reason     string          // "network call", "unclassifiable shell command", ...
    Detail     string          // the argv, the URL host, the path outside root
    Input      json.RawMessage // the call as made, for the user to judge
}
```

One boundary ends the speculative run. Multiple boundaries are not collected by continuing past the
first — continuing would mean the state the second call observed is a state that never existed.

### 2.4 Isolation for the speculative run

`internal/swarm` already forks a run with its own engine and its own registry clone (CLAUDE.md:
"Never give a sub-agent, debate role or session `s.tools` itself; hand it a clone"). A speculative
run is that, plus the overlay on its context, plus a gate that turns the three boundary
capabilities into a stop rather than an Ask. The permission layer needs no new concept: this is a
policy that returns a distinct terminal verdict, sitting in the stack `enginecfg.BuildGate` already
assembles.

---

## 3. What this must not become

**Not speculation.** The comparison source uses this machinery to predict the user's next prompt
during idle time and pre-execute it. That half is explicitly not recommended and should not be
built. The prediction is the expensive, risky, low-confidence part; the overlay-and-boundary
machinery is the durable part, and its value must not depend on guessing right.

**Not a second write path.** If write tools grow an `if overlay != nil` branch, the design has
failed. The overlay goes in below the existing context-carried snapshotter seam, or it does not go
in. A useful test of any implementation: `git diff` on the write tools should be empty.

**Not a replacement for checkpoints.** They answer different questions (§1). Both stay.

**Not a way to weaken plan mode.** A speculative run's gate is *stricter* than build mode's, never
looser. Nothing about this item permits a `CapWrite` call in plan mode; it permits a `CapWrite`
call against an overlay in a **separate, explicitly-entered speculative run**, whose writes land in
the workspace only on an explicit accept.

---

## 4. Consumer

The promote-when condition is a named first consumer. The obvious one — a `--dry-run` that shows a
real diff — has a naming collision worth knowing before the build: **`aegis dry-run` already
exists** (`internal/cli/dryrun.go`, 148 lines) and is a *configuration* preview — resolved config,
tools, memory, and context, without calling the model. It does not execute anything and has nothing
to do with this. A consumer for the overlay needs a different name (`aegis preview`, or a
plan-mode-scoped `/preview`), or `dry-run` needs a subcommand split. Do not assume the name is
available.

---

## 5. Effort and sequencing, honestly

- The overlay filesystem itself: **M**. It is a claimed-set + read-through, and the claimed-set half
  can be read off `checkpoint.Snapshotter`.
- Upgrading the snapshotter seam from notifying to path-resolving, across the nine call sites §2.1
  enumerates: **S-M**. Mechanical, but it touches every write tool and every one of their tests, and
  `captureShellWrites` does not fit the shape (§2.1) — that site stays a boundary in v1.
- The boundary gate: **S**. `EffectiveCapability` does the work.
- Accept/discard, including the P81.31 external-change question (a file changed by something other
  than the speculative run must not be silently overwritten on accept — the same HTTP 428
  confirmation posture rewind already takes): **S-M**.
- A consumer that renders the diff and the boundary: **M**, and it is the part with the actual
  user-facing value.

**The seam measurement was taken while writing this note, and it came back half-wrong** — which is
the roadmap's standing advice for this tier vindicated once more: every Tier-4 item that has
actually been measured so far turned out to be wrong in some way. The original assumption was that
every write already funnels through one context-carried *interceptor*; what exists is a universal
context-carried *notifier* (§2.1). The overlay is still buildable at that seam, and no other part
of this note changes, but the wiring is a nine-site mechanical change rather than free.

**What is still unmeasured, and should be measured before any build:** whether `Snapshotter`'s
first-touch-wins `claim` semantics are correct for an overlay. Checkpointing wants the *oldest*
content per turn and so ignores a second touch; an overlay wants the *newest* content per path and
must not. They are the same set with opposite update rules, and sharing the type without noticing
that would be a quiet correctness bug of exactly the kind this tier keeps producing.
