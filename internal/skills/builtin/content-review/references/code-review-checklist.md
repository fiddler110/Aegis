# Code Review Checklist

Categories to check, not a script to narrate. Skip any category with
nothing worth reporting — don't write "no issues" entries.

## Correctness
- Does it do what the surrounding code/caller/spec expects on the
  happy path?
- Edge cases: empty input, nil/zero values, boundary indices, very
  large/small values, duplicate entries.
- Error handling: are errors checked, or silently swallowed/ignored? Does
  a partial failure leave state (files, DB rows, in-memory structures)
  inconsistent?
- Concurrency: shared state without a lock, a lock taken in inconsistent
  order (deadlock risk), a goroutine/thread whose panic or error is never
  observed, a race between two operations that individually look correct.

## Security
- Untrusted input (user input, network responses, file contents, tool/LLM
  output) flowing into something trust-bearing: a shell command, a SQL
  query, a file path, a permission decision, a prompt fed back to a model.
- Secrets: logged, persisted in plaintext, or included in output that
  ends up somewhere less trusted than its source.
- A privilege/trust-boundary check that exists in one place but not an
  equivalent sibling code path that carries the same risk (this is the
  single most common miss in a codebase's *second* security review).

## Reuse & Simplification
- Logic duplicated elsewhere in the codebase that should call the existing
  implementation instead.
- Abstraction introduced for a single call site with no second use in
  sight.
- Code that's more defensive/general than the actual call sites require
  (validating inputs that can't occur, feature flags for a single fixed
  behavior).

## Efficiency
- Obviously superlinear work on a hot path where linear (or better) is
  available (e.g. repeated linear scans instead of a map/index).
- Unbounded growth: a cache, buffer, or retained list with no eviction.
- Redundant I/O or network calls that could be batched, cached, or hoisted
  out of a loop.

## Test coverage
- Does a new or changed code path have a test that would fail if the
  logic were wrong?
- Do existing tests still test the real behavior, or did the change make
  an assertion vacuous (e.g. now always true)?
- Are the edge cases identified under Correctness actually covered, not
  just the happy path?

## Consistency with the codebase
- Does this follow the same pattern the rest of the project uses for the
  same kind of problem (error handling style, naming, layering)? A novel
  pattern next to nine instances of an existing one is itself worth
  flagging even if the novel version isn't wrong.
