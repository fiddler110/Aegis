---
name: structured-build
description: Use when implementing a multi-step feature, plan, or task list where each task should land as its own reviewable commit with a bounded file footprint and passing tests. Triggers on "build this feature", "implement this plan", "work through these tasks", "one commit per task", "structured build". Prefer this over ad-hoc editing whenever the work spans several files or several distinct tasks.
---

# Structured Build Skill

The failure mode this skill prevents is the mega-diff: a dozen loosely
related changes across the tree, landed as one opaque commit, some of which
were never actually tested. Reviewable history and bounded blast radius come
from *discipline enforced mechanically*, not from good intentions — so this
skill leans on two real gates Aegis provides rather than asking you to
remember to be careful:

- the **`scope` tool** (P46.1) — a per-task file-write allowlist the
  permission gate enforces: once set, any write outside it is *refused*, not
  merely discouraged;
- the **pre-commit test gate** (P46.2) — when the operator has configured
  `git.pre_commit_test_command`, `git_commit` runs it first and a red suite
  blocks the commit outright.

Work one task at a time, in this loop. Do not batch tasks together.

## 1. Turn the request into an explicit task list

Before touching any file, write the plan down as an ordered list of tasks
(use `todo_add` / the todo tools if available, otherwise state the list in
chat). Each task must be:

- **single-purpose** — one behavior change, small enough to land as one
  commit a reviewer can understand in isolation;
- **scoped** — you can name, up front, the handful of files it should touch.

If a "task" needs to touch files all over the tree, it's really several
tasks — split it. If you genuinely can't predict the file footprint yet
(exploratory work), do the exploration first as read-only investigation,
*then* write the task list.

## 2. For each task: declare its scope

Call the `scope` tool with `action: "set"` and the workspace-relative path
globs this task is allowed to write, e.g.:

```
scope(action="set", paths=["internal/auth/**", "internal/auth/token_test.go"])
```

From here until you clear it, any `write_file` / `edit_file` / `multi_edit`
outside those globs is denied by the gate. That's the point: if the task
turns out to need a file you didn't declare, you find out immediately and
consciously — either it belongs in this task (widen the scope deliberately)
or it's a different task (finish this one first). Reads are never restricted,
so you can still look anywhere.

## 3. Make the edits, then verify

Implement the task within scope. Then **run the tests yourself** — the
relevant test/build command for the code you changed — and read the output.
Don't assume; confirm. If the operator configured a pre-commit test command,
the commit in step 4 will re-run it as a hard gate, but you should not be
finding out a suite is red *at commit time* — verify as you go.

If tests fail, fix them within the same scope before committing. A task is
not done until its own tests pass.

## 4. Commit exactly one task

Commit the task's changes as a single commit with a message describing that
one change:

```
git_commit(message="auth: validate token expiry before use")
```

If the pre-commit test gate is configured and the suite is red, this commit
is refused and you'll get the failure output — go back to step 3, fix it,
and retry. One task produces exactly one commit: don't fold the next task's
changes into this one, and don't commit a half-finished task "to save
progress."

## 5. Clear the scope and move to the next task

Call `scope(action="clear")` (or immediately `scope(action="set", ...)` with
the next task's paths). Then repeat from step 2 for the next task.

## 6. When the plan is done

Summarize what landed — one line per commit — so the sequence of commits
reads as the plan you started from. If the work is destined for a pull
request, one plan maps to one PR: the commit sequence *is* the reviewable
narrative, so keep it that way rather than squashing distinct tasks together.

## Stopping when a task won't converge

If the same task fails its tests repeatedly and you're rewriting the same
code without progress, stop rather than thrashing: leave the working tree as
it is, show the current diff and the failing output, and hand it back for a
human decision. A stuck task surfaced early with its diff intact is far more
useful than a pile of speculative rewrites.
