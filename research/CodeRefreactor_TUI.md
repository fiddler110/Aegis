The file itself is fine — no dead code, well-commented — it's just become a catch-all for things that already have dedicated homes (or should). Prioritized list:

1. toolCard/toolGroup/toolBlock (117–212) — self-contained subsystem with its own test files but no home; move to toolcard.go.
2. model struct (214–538) — 300+ lines, 80+ fields mixing streaming state, tool tracking, sidebar, history, etc. Group into sub-structs (toolState, streamPhase). Highest value, but riskiest — fields are read throughout the package.
3. handleTerminalKey (2107–2192) — duplicates the shape of logic already in terminal.go; move there.
4. applySwitchedSession/loadHistory (2196–2307) — belongs in update_session.go (currently only 77 lines).
5. Attachment parsing (extractImageRefs etc., 1097–1230) — has its own attach_test.go but no attach.go; extract.
6. Welcome-screen rendering (buildWelcomeContent etc., 2449–2510) — unrelated to surrounding code; pull into welcome.go.
7. Generic helpers at the bottom (wrap, truncate, contextWindowFor, etc., 2524–2612) — move to helpers.go or an existing small file.
8. bangShellCommand (904–909) — its own comment admits it duplicates shell-selection logic in internal/sandbox and internal/security; worth consolidating into one exported implementation.
9. Command constructors (874–1096) — ~7 near-identical context.WithTimeout + call + wrap-in-msg functions; could collapse via a generic helper, though that's a readability/indirection tradeoff worth deciding deliberately.
10. Message type declarations scattered across two comment banners (608–745) — grouping all \*Msg types together would make the file easier to scan.

Net effect of items 1, 3–7 (pure code moves, low risk): tui.go would shrink from ~2,600 to roughly 1,400–1,600 lines, with pieces landing in files that already exist for those concerns.
