# Documentation Review Checklist

Every claim in a doc is a testable assertion about the code, config, or
behavior it describes. Verify against the actual source before accepting
it — don't grade a doc purely on internal consistency.

## Accuracy
- Does each command, flag, config key, function/type name, and default
  value actually exist and behave as described, in the current code?
- Is a described behavior still true, or did the implementation change
  since the doc was written (check git history/blame on the relevant code
  if unsure)?
- Do code samples actually run/compile as shown, or are they missing an
  import, a step, or a required setup the doc doesn't mention?

## Completeness
- Is there a documented feature, flag, or command with no corresponding
  code (aspirational/stale), or working code with no mention in the docs
  at all?
- Are prerequisites, side effects, and failure modes stated, or only the
  happy path?
- For a reference doc: does it cover every public surface it claims to
  cover (every flag, every endpoint, every config key)?

## Clarity
- Could a reader who doesn't already know the answer follow this without
  guessing? Watch for unexplained jargon, acronyms, or internal codenames
  used before they're defined.
- Are steps ordered the way they must actually be performed?
- Is the scope of an instruction clear (does "restart the service" mean
  the whole system or one component)?

## Consistency
- Same term used for the same concept throughout (not "session" in one
  section and "conversation" for the same thing in another).
- Cross-references and links point to sections/files that still exist.
- Formatting/structure consistent with sibling docs in the same project
  (heading levels, code block language tags, table conventions).

## Currency
- Version numbers, dates, and "as of" statements still accurate.
- Screenshots, sample output, or example paths that reflect an old
  UI/CLI/file layout.
- TODO/FIXME/"coming soon" markers that the code shows are actually
  already done (or abandoned).
