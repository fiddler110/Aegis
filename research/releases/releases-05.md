# Aegis Release History — Part 5

Start at [releases.md](../releases.md) for the index.

---

### P29 batch — docs-vs-implementation drift (all 6 items, Tier 2/3, Effort S/M)

Filed 2026-07-14 from a full parallel audit comparing every `docs/*.md` file against the actual
implementation (`internal/tool/builtin`, `internal/permission`, `internal/config`,
`internal/provider`, plus persona/skills/swarm/debate/MCP/session/memory, which matched the code
exactly — no items filed there). All were pure documentation drift except P29.4, which the user
chose to resolve by changing behavior instead of docs.

- **P29.1** — `docs/tools-reference.md` and `docs/multi-agent.md` named the team-task-creation tool
  `team_task_create`; the real registered name is `team_task_add`
  (`internal/tool/builtin/team.go:40`). Corrected both docs; the tool itself was already correctly
  named everywhere in code and tests, so no code change.
- **P29.2** — `docs/permissions.md` (and, found during the same pass, `docs/security_scan.md`)
  described a fabricated per-session audit mechanism (`~/.local/share/aegis/audit/<session-id>.jsonl`,
  fields including `session_id`/`capability`, decision values `ask_approved`/`ask_denied`) that
  doesn't exist. Rewrote both to describe the real single global file, `<data_dir>/audit.jsonl`
  (`internal/server/server.go:628`), with its real phase-keyed schema
  (`internal/hooks/hooks.go:67-82`: `pre`/`post`/`policy_decision`/`subagent_stop` phases, real
  decision values `allow`/`deny`/`ask`).
- **P29.3** — `docs/sessions.md` and `docs/configuration.md` claimed the default data directory is
  `~/.local/share/aegis` (macOS/Linux) / `%LocalAppData%\aegis` (Windows); the real
  `defaultDataDir()` (`internal/config/config.go:874-890`) uses `~/.config/aegis` /
  `%AppData%\aegis` — a different XDG category on both platforms. Corrected both named files, plus
  the same stale path found in `docs/extensibility.md`, `docs/memory-and-knowledge.md`,
  `docs/overview.md`, `docs/personas.md`, and `docs/tools-reference.md` during the sweep.
- **P29.4** — `docs/configuration.md` listed `GROQ_API_KEY`/`OPENROUTER_API_KEY` as native provider
  env vars, but `config.ProviderAPIKey` only ever read `ANTHROPIC_API_KEY`/`OPENAI_API_KEY`/the
  hardcoded `"ollama"` fallback. Asked the user which fix path they wanted (doc-only correction vs.
  actually wiring the vars); they chose to wire them. `ProviderAPIKey`'s `"openai"` case
  (`internal/config/config.go`) now falls back to `GROQ_API_KEY` then `OPENROUTER_API_KEY` when
  `OPENAI_API_KEY` is unset — Groq/OpenRouter are reached via `provider.default: openai` plus a
  custom `base_url` (see `docs/providers.md`), not distinct provider names, so the fallback lives in
  the `"openai"` branch rather than as new provider cases. `docs/configuration.md` gained a short
  note clarifying the mechanism. Tested: `TestProviderAPIKeyGroqOpenRouterFallback`
  (`internal/config/config_test.go`) exercises the priority order
  (`OPENAI_API_KEY` > `GROQ_API_KEY` > `OPENROUTER_API_KEY` > empty) purely via env vars — no live
  API keys needed, matching how the rest of the package's env-driven config tests work.
- **P29.5** — `provider.prompt_profile`, `security.wsl_distro`, `security.dast.allowed_targets`,
  `security.dast.allow_active`, and `security.redact_secrets` were implemented and functional
  (some documented only in `docs/providers.md`) but missing from `docs/configuration.md`'s main
  reference. Added all five with their real defaults and behavior.
- **P29.6** — `docs/configuration.md`'s sample config showed `tui.humor_mode: false` (built-in
  default is `true`, `internal/config/config.go:857`) and `sandbox.backend: os` unqualified (built-in
  default is `local`, `internal/config/config.go:844` — `os` is only what `aegis --first-init`'s
  generated template writes). Corrected the humor_mode sample value and added an inline note next to
  `backend: os` explaining it reflects the first-init template, not the built-in default.

Tested: `go build ./...`, `go test ./internal/config/... ./internal/providerfactory/...` pass clean;
the rest of the batch is documentation-only with no runtime surface to exercise.

**P28.5** (Tier 3, Effort M/L) shipped: a resumable-run design so a web UI SSE stream that drops
mid-turn (network blip, backgrounded-tab throttling, daemon restart) reattaches and catches up
instead of surfacing a dead-end "Error: ..." — the gap flagged by the same 2026-07-14 live
evaluation, where local-model turns routinely ran 30s-150s+, making a mid-turn drop meaningfully
more likely than with fast cloud round-trips.

Investigated first (per the roadmap item's own note): the existing detached-run infrastructure —
`runRegistry` (`internal/server/runs.go`), the `bg_events` SQLite buffer
(`Store.AppendBGEvent`/`ListBGEvents`), and the web UI's `watchLive` reattach poller
(`app.tsx`) — already solves this completely for sessions explicitly marked `background` (P3.2): a
background session's run runs on a `context.Background()`-rooted context so a client disconnect
can't cancel it, and every event is buffered to SQLite so `watchLive` can catch up via
`GET /sessions/{id}/events?since=N`. The gap was that a normal (non-background) session's run uses
`r.Context()` as its base context, so a dropped connection cancels it via the engine's existing
`ctx.Done()` check (`engine.ErrInterrupted`) — there was nothing left running to reconnect *to*.
Generalizing background's survive-disconnect + event-buffering behavior to every run would have
also broken Stop: today, aborting the fetch is the *only* way either the TUI or the web UI stops a
run, by tearing down the same request context the engine runs on — both clients share this pattern
(`internal/tui/tui.go`'s `m.cancel`, the web UI's `controllerRef`). So resumability had to be opt-in
per request, not a blanket change to every run's lifetime.

Fix: `api.PostMessageRequest` gained `Resumable bool` (`internal/api/api.go`) — off by default, so
the TUI/CLI/mcp-serve keep today's exact disconnect-cancels-the-run behavior; only the web UI's
`send()` (`app.tsx`) sets it. In `handlePostMessage` (`internal/server/messages.go`), a `detached :=
sess.Background || resumable` local now gates both the context root (`context.Background()` instead
of `r.Context()`) and the event-buffering `send` wrapper that used to be `sess.Background`-only —
the two mechanisms were already identical in shape, this just extends who gets them. Because a
detached run's context is no longer tied to its request, disconnecting can no longer stop it either
— so `runRegistry` (`internal/server/runs.go`) gained a `cancel context.CancelFunc` per run
(`setCancel`) and a `stopSession` lookup, and a new endpoint, `POST /sessions/{id}/stop`
(`handleStopRun`), cancels the active resumable run for a session. The web UI's Stop button now
calls both `controller.abort()` (stop listening) and this endpoint (stop the run) — a plain
TUI/CLI run is unaffected, since it has no registered cancel and keeps stopping via disconnect. As a
side effect, an explicitly-`background` session — previously unstoppable once its owning client had
disconnected — can now also be stopped this way.

Client side (`internal/server/webui/frontend/src`): `api.ts`'s `consumeSSE` is unchanged; `app.tsx`'s
`send()` now sends `resumable: true` and distinguishes three stream-exit cases — a clean resolve
(the daemon closed the response itself, meaning the run is genuinely over, success or not: no
action), an `AbortError` (the user's own Stop click: show "Stopped." as before), and any other
exception (the connection was actually severed while the run may still be executing server-side: show
"Connection lost — reconnecting…" and hand off to the existing `watchLive(sessionId)` reattach
poller instead of building a second implementation of the same catch-up logic).

Tested: `internal/server/runs_test.go` — `TestRunRegistryStopSession` (cancel registration/lookup in
isolation), `TestResumableRunSurvivesClientDisconnect` (end-to-end over the real HTTP+SSE seam via
`httptest.NewServer` + `internal/client`: a `blockingAdapter` mid-stream run keeps executing and
buffering events after the client request is cancelled, confirmed via `GET /runs` staying non-empty
and `GetBGEvents` containing the terminal `done` event once released), and
`TestStopRunCancelsResumableRun` (`POST /sessions/{id}/stop` actually interrupts the run, and 404s
on a session with nothing resumable to stop). `internal/client/client.go` gained `StopRun` to drive
the new endpoint from tests (and any future non-web-UI caller). Frontend: `tsc --noEmit` and
`npm run build` both pass clean; `dist/` regenerated and committed. `go build ./...`, `go vet ./...`,
and the full `go test ./...` (plus `-race` on the touched packages) pass clean.

**P28.3** (Tier 3, Effort M) shipped: the engine now detects a suspicious zero-tool-call completion
on a plainly actionable task and nudges the model to reconsider and act, instead of silently
accepting a text-only turn as done — the `deepseek-r1:8b` failure mode from the same 2026-07-14
live evaluation that filed the whole P28 batch, where the model's reasoning got dumped as the final
answer instead of being followed by a structured tool call.

Investigated first (per the roadmap item's own note): Ollama's OpenAI-compatible endpoint
(`docs.ollama.com/api/openai-compatibility`) explicitly does not support `tool_choice`, ruling out
sending `tool_choice: "required"` from the OpenAI adapter for this repo's primary local-model
target. That left the corrective-nudge/retry path — the same shape as the existing output-guard
retry (P25.3) — as the one to build.

Fix, `internal/engine/engine.go`: in the `len(toolUses) == 0` branch of `Engine.Run`, after the
existing max-tokens-continuation check and before the output-guard check, a new condition fires the
nudge: no tool round has completed yet this run (`toolRoundsCompleted == 0` — a text-only wrap-up
*after* real tool use is a legitimate final answer, not a suspicious non-action), tools are actually
registered (`len(e.tools.Schemas()) > 0`), a retry budget remains (new `Options.ZeroToolNudgeMaxRetries`,
0 → default 1, negative disables), and the triggering request `looksActionable` — a new purely local
heuristic (same "regex/word-count, never an extra model call" philosophy as `routing.go`'s
`classifyTurn`) that strips a leading politeness wrapper ("could you please...") and checks for a
leading imperative verb from a fixed vocabulary (fix, implement, add, write, run, refactor, ...)
against the most recent user message (`lastUserText`). Deliberately biased toward missing a real task
(the safe, today's-behavior default) over firing on a genuine question — a wrong nudge wastes one
turn but corrupts nothing. On a match, the engine appends a corrective prompt (`zeroToolNudgeText`)
telling the model to call the appropriate tool now rather than just describing the action, and loops.
Once the run settles (whether the nudge succeeded or the single retry was also text-only and gets
surfaced anyway), `retractZeroToolNudges` strips the nudge prompt and the text-only answer it was
reacting to from the durable transcript — mirroring `retractGuardCorrectives` exactly, including the
same marker-prefix-based matching so a mid-run compaction or prepare-step rewrite can't desync
index-based bookkeeping.

Wired end to end: `ProviderConfig` gained `zero_tool_nudge` (`internal/config/config.go`, 0 = default
1 retry, negative disables, mirroring `loop_threshold`'s convention), and `s.newEngine`
(`internal/server/engine_build.go`) passes it through as `ZeroToolNudgeMaxRetries`.

Tested: new `internal/engine/nudge_test.go` — `TestLooksActionable` (table-driven heuristic cases,
including polite phrasing and plain questions that must *not* match), `TestZeroToolNudgeRetriesOnActionableTextOnlyTurn`
(full nudge-then-tool-call-then-final-answer round trip via a `scriptedAdapter`, asserting the nudge
text reached the retry request and was retracted from the final transcript),
`TestZeroToolNudgeSkippedOnNonActionablePrompt`, `TestZeroToolNudgeSkippedWithoutTools`,
`TestZeroToolNudgeSkippedAfterToolRound` (three no-nudge-fires regressions),
`TestZeroToolNudgeExhaustedSurfacesTextAnswer` (retry budget exhausted still surfaces an answer
rather than looping), and `TestZeroToolNudgeDisabledByNegativeOption`. `go build ./...`, `go vet
./...`, and the full `go test ./...` pass clean. `docs/providers.md`'s tool-calling-reliability
section, which previously noted this as "not yet built," now points at the shipped behavior instead.

**P28.7** (Tier 2, Effort S) shipped: a persistent connection/model-health indicator in the TUI
status area and the web UI header.

Real usage evidence, not a hypothetical: this daemon's own `GET /sessions` history contained at
least 6 near-duplicate sessions from 2026-06-26/27 titled things like "test that the model is
connected," "validate model is connected," "confirm that the model is connected," and "Check that
the model is connected" — a recorded pattern of users spending a full conversational turn just to
sanity-check daemon-to-model connectivity. `aegis doctor` and `GET /status`
(`internal/server/server.go`'s `handleStatusInfo`) already answered this server-side, but neither
client surfaced it passively — a user had to know to run one of them.

Fix, server side: `GET /status`'s response (`api.StatusInfo`, `internal/api/api.go`) gained two
fields, `provider_reachable` and `provider_latency_ms`, populated by a new
`Server.probeProviderReachability` (`internal/server/provider_health.go`). This mirrors `aegis
doctor`'s existing provider check (`doctorProviderCheck`/`ollamaNativeBase` in
`internal/cli/doctor.go`) rather than inventing new semantics: for an Ollama-style provider
(`provider.default: ollama`, or a `base_url` containing the default Ollama port — the same
detection doctor.go uses) it's a live `GET /api/version` with a 2-second timeout, timed for
latency (reusing `internal/ollamainfo.IsOllama`, already used by the context-window
auto-detection path); for a cloud provider, a live call on every `/status` poll would be wasteful
or, for a paid API, costly, so reachability there is just "an API key is present in the resolved
config" — the same signal doctor uses — with latency left unmeasured (0). `handleStatusInfo` calls
this and adds the two fields to its response; no new endpoint was added.

Fix, TUI side (`internal/tui/tui.go`): the daemon `/status` payload was already fetched at startup
and after each run (for the effective-context-window fallback, P23.1) but never polled
continuously. Added a new `statusTickMsg`/`statusTickCmd` pair that reschedules a `/status`
re-fetch every 20 seconds (`statusRefreshInterval`), independent of run activity, so the indicator
stays current without user action. New model fields `connKnown`/`connReachable`/`connLatencyMS`
are set from each `statusInfoMsg` (a request error — the daemon itself unreachable — is
distinguished from the daemon reporting its configured provider unreachable, both rendering as
"down"). Rendered in two places: a compact colored-dot glyph (`renderConnBadge`, green/red/muted
for reachable/unreachable/unknown, plus a `NNms` suffix once latency is measured) in the
always-visible title bar next to the model name, and a fuller `reachable · NNms` /
`unreachable` / `checking…` line (`renderConnDetail`) under the sidebar's existing MODEL section.

Fix, web UI side (`internal/server/webui/frontend/src`): `types.ts`'s `StatusInfo` interface
gained the two new fields. `app.tsx`'s existing `loadStatus()` poll (previously only called at
mount and after specific actions) now also runs on a 20-second `setInterval`, mirroring the TUI's
cadence. A new chip in the topbar — not gated on `currentId`, since `/status` is daemon-wide, not
per-session — shows a colored dot, the configured model name, and the latency when measured, with
a tooltip carrying the full detail (provider/model, reachable/unreachable, latency). New
`.chip.conn-ok`/`.chip.conn-down` CSS rules in `style.css` reuse the green/red palette already
established for scanner availability (`.avail.ok`/`.avail.bad`) and other status chips elsewhere
in the same file, rather than inventing new colors.

Tested: `go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean, including new
`TestProbeProviderReachability_Ollama` (fake Ollama server via `httptest`, live `/api/version`
round trip), `TestProbeProviderReachability_OllamaUnreachable`, `TestProbeProviderReachability_Cloud`
(API-key-present/absent), and `TestProbeProviderReachability_BaseURLPortDetection`
(`internal/server/provider_health_test.go`), plus an extended `TestServerStatusEndpoint`
(`internal/server/server_test.go`) asserting `ProviderReachable`/`ProviderLatencyMS` for a
no-API-key cloud provider; and new `TestStatusInfoMsgUpdatesConnectionState`,
`TestStatusTickMsgReschedules`, `TestRenderConnBadgeAndDetail`
(`internal/tui/status_health_test.go`) covering the TUI's state transitions and rendering.
Frontend: `npm --prefix internal/server/webui/frontend run build` (`tsc -b && vite build`) passed
clean — TypeScript type-checked the new `StatusInfo` fields and topbar chip — and the regenerated
`internal/server/webui/dist/` output is committed alongside the source change per this repo's
embedded-webui convention. This was the last of Tier 2's four items — see [roadmap.md](roadmap.md).

Same day (2026-07-14): **P28.4** (Tier 2, compaction robustness) shipped: proactive
per-turn context compaction now falls back to a deterministic, non-LLM shortening pass after the
LLM summarizer fails twice in a row for the same run, instead of skipping compaction indefinitely.

Live evaluation (`TestLiveWorkflow` against `qwythos:latest`, `deepseek-r1:8b`, `gpt-oss:20b`,
2026-07-14, the same pass that filed the whole P28 batch) observed `proactive compaction failed:
summarizer returned empty output` (`internal/compaction/compaction.go:212`) against both
`qwythos:latest` and `gpt-oss:20b`. Before this fix, `internal/engine/engine.go`'s proactive
per-turn compaction check (P2.7, ~85% context fill) just logged a `Warn` and skipped compaction for
that turn entirely on any `Compactor.Compact` error — no retry, no fallback. Long local-model
sessions run far more turns/tokens per task than cloud sessions (observed: 87k input / 2.4k output
tokens over 13 tool calls for one bug fix with `gpt-oss:20b`), so a summarizer that unreliably
returns empty output could repeatedly fail to shrink context every single turn, drifting toward the
hard context-window ceiling with no safety valve — the model server would then start silently
dropping the oldest tokens (including the system prompt) rather than Aegis ever compacting on its
own terms.

Investigated the existing compaction data model first (`internal/compaction/compaction.go`): a
"turn" boundary for compaction is chosen by `Summarizer.boundary`, which finds the first assistant
message at or after the `keepRecent` cutoff so the summarized prefix never splits a
`tool_use`/`tool_result` pair; the LLM summary then replaces that whole prefix with a single
synthetic `user` message ("Summary of earlier conversation...") spliced in ahead of the preserved
suffix. There was no existing per-session or per-run failure-count tracking to piggyback on — the
`compaction.Summarizer` is a single daemon-wide singleton (`s.compactor`, built once in
`internal/server/server.go`) shared across every session, and `engine.Engine` itself is
reconstructed fresh per HTTP request/turn (`s.newEngine` in `internal/server/messages.go`) — so
neither was a natural home for cross-request state. Since `engine.Run`'s own tool-round loop
(`for iter := 0; iter < e.maxIterations; iter++`, default cap 40) already spans every tool round of
a single long local-model task — the exact shape of the observed failure (13 tool calls in one bug
fix, all inside one `Run` call) — a run-scoped counter was sufficient to catch "twice in a row"
without needing to thread new state through the session store: a new `compactionFailures` local
counter in `Engine.Run`, reset to 0 on any successful compaction (LLM-summarized or
deterministic-fallback) and incremented on each `Compact` error, mirroring the existing
`guardRetries`/`ctxFullWarned` per-run locals already in that function.

Fix, in two parts. (1) `internal/compaction/compaction.go` gets a new
`(*Summarizer).FallbackCompact(msgs []provider.Message) (out []provider.Message, changed bool)` —
deterministic, makes no adapter call, and so cannot itself return empty output. It reuses the exact
same `boundary` selection as `Compact`/`ForceCompact` (protecting the `keepRecent` tail and tool-use
pairing) but replaces the summarized prefix with a terse, programmatically generated note (message
counts, tool-call count, and the distinct tool names used) instead of an AI-generated summary — a
structurally valid replacement for the LLM summary, not just an arbitrary non-empty string. (2)
`internal/engine/engine.go` gets a new optional `FallbackCompactor` interface
(`FallbackCompact(msgs) (out, changed)`) that the proactive-compaction block in `Engine.Run`
type-asserts for on `e.compactor` — so a `Compactor` that only implements `Compact` (e.g. a test
double or a future non-LLM implementation) keeps today's warn-and-skip behavior unchanged. On the
2nd consecutive `Compact` failure within a run, the engine calls `FallbackCompact` if the configured
compactor supports it; on success it splices the deterministic result in exactly like a normal
compaction, emits the existing `KindNotice` (now naming the fallback explicitly, e.g. "context ~87%
full — summarizer unavailable, applied deterministic fallback compaction (42→6 messages)"), and
resets the failure counter. `compaction.New`'s production `*Summarizer` now satisfies
`FallbackCompactor` automatically, so the daemon's real compactor gets the fallback with no wiring
changes in `internal/server`.

New tests: `TestFallbackCompactShrinksWithoutLLM`, `TestFallbackCompactPreservesToolPair`,
`TestFallbackCompactTooShortIsNoop` (`internal/compaction/compaction_test.go`, mirroring the
existing `Compact`/`ForceCompact` coverage for the new deterministic path) and
`TestProactiveCompactionFallsBackAfterTwoFailures` (`internal/engine/contextnotice_test.go`, a new
`failingFallbackCompactor` test double that always fails `Compact` but implements
`FallbackCompact` — asserts the fallback fires on exactly the 2nd consecutive failure, not the 1st,
and that the resulting notice mentions the fallback). `go build ./...`, `go vet ./...`, and the full
`go test ./...` pass clean. This was one of Tier 2's four items (P28.2, P28.4, P28.6, P28.7); two
remain — see [roadmap.md](roadmap.md).

Same day (2026-07-14): **P28.2** (Tier 2, cheap no-dependency win) shipped: guidance on which
locally-runnable model families reliably drive Aegis's tool-calling loop, plus a new `aegis doctor`
check that catches the failure mode live. Live evaluation (`TestLiveWorkflow` against
`qwythos:latest`, `deepseek-r1:8b`, `gpt-oss:20b`, 2026-07-14) found wide variance in local-model
tool-calling reliability: `qwythos:latest` (this repo's own configured `provider.model` default)
correctly diagnosed a seeded bug in its response text but never called `edit_file`/`write_file` to
actually fix it; `deepseek-r1:8b` made **zero tool calls** on an explicit run/fix/verify task,
answering entirely in prose instead (a known R1-distill failure mode — reasoning dumped as the final
answer instead of a structured `tool_call`); only `gpt-oss:20b` completed the task end-to-end (13
tool calls, 2m28s). `aegis doctor`'s existing provider check (`doctorProviderCheck`) only verifies
reachability and model availability, never tool-calling competence, so this class of failure was
invisible until a real task hit it.

Fix, part (a): a new "Tool-calling reliability for local models" section in `docs/providers.md`
(right after the Ollama setup section, alongside the existing "better tool use" model-pull hints)
documents the three live-eval outcomes above and recommends instruction-tuned/tool-calling-marketed
models (`gpt-oss:20b`-class, `qwen2.5:32b`+) over small reasoning-distilled models for agentic tasks,
cross-references the doctor check below, and notes the `qwythos:latest` diagnose-but-don't-act pattern
responds well to a more directive follow-up prompt while the underlying engine has no automatic
nudge/retry yet (that's **P28.3**, deliberately out of scope here — investigation-gated, not built
speculatively).

Fix, part (b): a new `doctorToolCallCheck` (`internal/cli/doctor.go`), wired into `runDoctorChecks`
right after `doctorProviderCheck`, sends a single cheap live request — one trivial `list_files` tool
schema plus an unambiguous "call the tool now, don't describe it" prompt (`MaxTokens: 256`, 20s
timeout) — through the same `providerfactory.Build` adapter construction the daemon uses, and counts
`provider.EventToolUse` events in the response stream. Scoped to local (Ollama-style) providers only,
via the same `ollamaNativeBase` gate `doctorProviderCheck` already uses: this is where the observed
variance lives, cloud providers have well-established tool-calling support, and skipping them keeps
the check free of live network cost for the common cloud-provider case — it silently returns PASS
("skipped") for a cloud provider, an unresolved (`auto`/empty) model, or an adapter-construction
failure `doctorProviderCheck` already reports. Any failure past that point — transport error, stream
error, or a genuine zero-tool-call response — degrades to WARN, **never FAIL**: this check must not be
able to make `aegis doctor` exit non-zero on its own, matching how `doctorDaemonChecks` already
degrades to WARN (not FAIL) when no daemon is reachable, and keeping it safe for offline/CI use. A
zero-tool-call WARN's `Fix` field points at the new `docs/providers.md` section by name.

Tested: new `TestDoctorToolCallCheckSkipsCloudProvider`, `TestDoctorToolCallCheckSkipsUnresolvedModel`,
`TestDoctorToolCallCheckDetectsZeroToolCalls`, `TestDoctorToolCallCheckPassesOnToolCall`,
`TestDoctorToolCallCheckWarnsOnTransportFailure` (`internal/cli/doctor_test.go`) — the latter three
drive a real `httptest.Server` emitting hand-written OpenAI-compatible SSE chunks (no live model
needed) to exercise the zero-tool-call, one-tool-call, and unreachable-server paths deterministically,
reproducing the `deepseek-r1:8b`/`gpt-oss:20b` outcomes observed live without a network dependency in
CI. Existing `TestDoctorCleanSetupExitsZero` and `TestDoctorNamesPodmanMisconfig` (which configure a
cloud provider) continue to pass unchanged, confirming the cloud-skip path adds no new network
dependency to the existing suite. `go build ./...`, `go vet ./...`, and `go test ./...` pass clean.
This was the second of Tier 2's four remaining items; **P28.6**, **P28.7** remain open.

Same day (2026-07-14): **P28.6** (Tier 2, harness-quality fix, not a product bug) shipped:
`TestLiveWorkflow/LocalPromptProfileReducesFirstTurnTokens` (`internal/eval/live_workflow_test.go`)
compared first-turn input token counts between the daemon's `local` and `default` prompt profiles,
expecting `local` to come out lower. Investigation traced the "local" prompt profile's only actual
effect anywhere in the code: `effectiveSystem` (`internal/server/helpers.go:62`) omits the injected
repo map entirely when `LocalPromptProfile()` is true *and* the rendered map exceeds
`localRepoMapMaxBytes` (4000 bytes, `helpers.go:35`) — nothing else differs between the two
profiles. The subtest's shared fixture (`writeSeededBugFixture`, a 2-file temp directory with no
`.aegis/repomap.json` cache) never gets a repo map injected at all regardless of profile — the
daemon's own `loadRepoMap` (`internal/server/server.go:583`) returns `""` when no cache file exists
— so `local` and `default` produced byte-identical system prompts for this fixture. The observed
pass/fail was therefore just noise in the live model's own reported token usage, not a signal about
the feature: passed for `gpt-oss:20b` (5638<5942 tokens), failed for `deepseek-r1:8b`
(3183>2567) on the same code, with nothing else changed between runs.

Fix, per the roadmap item's option (a) — a fixture large enough to actually trigger the cap, kept
inside the live daemon+HTTP+SSE integration path rather than moved to a plain unit test (a
non-live-tagged unit test doing exactly that already exists,
`TestEffectiveSystem_localProfileTrimsPrompt` in `internal/server/server_test.go`, so duplicating it
inside the live-tagged file would add nothing; the point of this specific subtest is verifying the
profile's effect survives the real daemon-to-live-model round trip, which only this tier can check).
New `writeBigRepoMapFixture` (`internal/eval/live_workflow_test.go`) writes 15 filler `.py` files
(10 functions each) into a dedicated workspace, then pre-builds and saves a `repomap.json` cache
directly via `repomap.Build`/`Map.Save` — what `aegis index` or the daemon's own startup
`loadRepoMap` would produce — so the daemon picks up a real, cached repo map on process start. The
fixture self-checks its own rendered-block size against a local `bigRepoMapCapBytes` constant
(4000, mirroring the unexported `localRepoMapMaxBytes`) and fails loudly if a future repo-map
format change ever shrinks it back under the cap, rather than silently reintroducing the original
bug. Verified standalone (outside the gated test, since it needs no live model): the generated
fixture renders a 5934-byte `<repo_map>` block — comfortably above the 4000-byte cap and below
repomap's own 8000-byte internal truncation budget, so the two profiles end up "full map" vs. "no
map" rather than "full map" vs. "truncated map" — a large, deterministic difference that should
dominate any live-model token-accounting noise. The `LocalPromptProfileReducesFirstTurnTokens`
subtest now chdir's into this dedicated workspace for its own duration only (restored after,
matching the file's existing single-process-chdir convention) rather than reusing the shared
`FixSeededBug`/`GuardNoMetaLeak` fixture, so this change doesn't affect those other subtests.
`internal/server/helpers.go`'s actual repo-map-cap behavior was deliberately left untouched — this
is a harness fix only.

Tested: `go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean, plus
`go build -tags live_workflow ./...` and `go vet -tags live_workflow ./...` to confirm the
tagged file still compiles. The fixture's byte-size math was independently verified by running its
exact generation logic in a throwaway, non-tagged test inside `internal/repomap` (deleted after
confirming the 5934-byte result above) — not by executing `TestLiveWorkflow` itself, since that
needs a reachable Ollama server this environment doesn't have. The live-tagged
`LocalPromptProfileReducesFirstTurnTokens` subtest was therefore reasoned about and compiled, not
run end-to-end; the reasoning rests on: (1) `effectiveSystem`'s cap-check logic
(`internal/server/helpers.go:62`) is unchanged and already covered by
`TestEffectiveSystem_localProfileTrimsPrompt`, which passes non-live and confirms the omit/include
split at this exact threshold; (2) the new fixture's cache is built with the same `repomap.Build`/
`Map.Save`/`Load` path production code uses, not a hand-rolled stand-in; (3) the chdir scoping was
checked by hand against the subtest ordering (this is the last of the three subtests in
`TestLiveWorkflow`, so scoping the extra chdir to it doesn't disturb `FixSeededBug`/
`GuardNoMetaLeak`, which run first and already completed against the original fixture). This was
the third of Tier 2's four remaining items; **P28.7** remains open.

Before that, same day (2026-07-14): **P28.1** (Tier 1, real exploitable robustness gap) shipped: the TUI
now strips dangerous terminal escape sequences from untrusted tool output before it reaches the
real terminal. This closed the P27 threat model's last open needs-verification question — whether
the TUI fully neutralizes terminal escape sequences in untrusted tool output — which this same pass
confirmed it did not. `stripControlSeqs` (P24.20/FIND-17) only ever ran on the model's own generated
prose inside `mdRender`; raw tool output (`shell` stdout/stderr, `read_file` contents,
`grep`/`web_fetch`/`web_search` results) rendered via `renderBlock`/`renderLinesBlock`
(`internal/tui/toolview.go`) only ever passed through `remapANSI16` (`internal/tui/ansi16.go`), which
rewrites SGR colour codes and leaves every other escape sequence untouched — OSC 8 hyperlink-text
spoofing, OSC 52 clipboard hijack, cursor-hide, alternate-screen-buffer switches, OSC 0/2 title-bar
spoofing all reached the terminal unfiltered from a malicious/compromised file read or
shell/web_fetch/web_search result.

Fix: a new `stripDangerousSeqs` (`internal/tui/sanitize.go`) — a sibling to `stripControlSeqs` that
keeps CSI SGR sequences (`ESC [ ... m`, needed for `remapANSI16` to have something to rewrite) but
strips everything else recognized: OSC/DCS/APC/PM/SOS strings, other 7-bit C1 forms, and any non-SGR
CSI (cursor movement/hiding, alternate-screen-buffer switches). Wired in at three points so no
raw-tool-output path is missed: `renderToolResult` (`internal/tui/toolview.go`) — the single funnel
every rendered tool result (single-line, multi-line generic block, and `read_file`) passes through —
sanitizes `result` once up front; `renderBlock` sanitizes independently too, since it's also called
from `renderShellCall`'s fallback path outside `renderToolResult`; and `renderShellCall` itself
sanitizes the model-supplied command before handing it to chroma for syntax highlighting, since a
successful highlight match bypasses `renderBlock` entirely via `renderLinesBlock`. New tests:
`TestStripDangerousSeqs`, `TestStripDangerousSeqsIdempotent` (`internal/tui/sanitize_test.go`),
`TestRenderToolResult_SanitizesDangerousSeqs`, `TestRenderShellCall_SanitizesDangerousSeqs`
(`internal/tui/toolview_test.go`) — covering OSC 52 clipboard hijack, OSC 8 hyperlink-target
spoofing, cursor manipulation, and alternate-screen switches across the single-line, multi-line, and
`read_file` render branches, plus confirming SGR colour still survives sanitization plus
`remapANSI16`'s truecolor rewrite. `go build ./...`, `go vet ./...`, and the full `go test ./...`
pass clean. This was the roadmap's sole Tier 1 item.

Before that, same day (2026-07-13): **P27.19** (FIND-17, Tier 4, CVSS 5.9) shipped: documentation-only
close-out of the P27 threat model's container-socket-trust finding. FIND-17 flagged that
Docker/Podman socket access is root-equivalent on the host and asked for docs recommending
"rootless Podman or a socket-proxy." The rootless-Podman half, along with the
`--cap-drop=ALL`/`--security-opt=no-new-privileges` hardening and the `--network none` default
FIND-17 also cites, was already shipped and already documented under **P24.10 (FIND-06)** — an
earlier threat-model pass that found and fixed the same underlying issue — in the "Docker/Podman
socket privilege equivalence" section of `docs/security_scan.md`. The one genuine gap between
FIND-17's remediation text and the pre-existing docs was the socket-proxy option, which wasn't
mentioned anywhere. Added a bullet to that section recommending a socket-proxy (e.g.
`docker-socket-proxy`) restricted to the container-create/start/stop endpoints Aegis needs, as an
alternative to rootless Podman for deployments stuck on a rootful Docker daemon. No code changes —
Aegis doesn't ship or manage a socket-proxy itself, this is operator guidance only. Confirms the
verification the finding itself asked for ("confirm the documented guidance recommends
rootless/socket-proxy configurations and that default container flags include `--cap-drop=ALL` and
`no-new-privileges`") is now fully true. Closes Tier 4's P27.19; **P27.20** (optional at-rest
SQLite encryption) remains parked with no concrete trigger.

Before that, same day (2026-07-13): **P27.16** (FIND-15, Tier 3, CVSS 3.6) shipped: quarantine-on-FAIL
for the output guard, closing the gap where a guard verdict of FAIL that exhausted the corrective
retry budget only ever led to the failing response being surfaced anyway — any file a `write_file`/
`edit_file` call made that turn already landed on disk and stayed there exactly as the failing model
left it. Aegis already had a full checkpoint/rewind mechanism (`internal/checkpoint`) built for the
user-facing `/rewind` feature: `write_file`/`edit_file` call `checkpoint.SnapshotterFrom(ctx).
Capture(absPath)` before every write, lazily recording each touched path's pre-turn content into the
turn's checkpoint the first time it's touched, and `Store.RestoreFiles(ctx, checkpointID)` restores
every captured path back to that pre-turn state (deleting files that did not exist before the turn).
Rather than build a second, parallel mechanism, `internal/engine/engine.go`'s exhausted-retries FAIL
branch (previously just `emit(Event{Kind: KindGuard, GuardPassed: false, ...})` and nothing else)
now also calls the checkpoint machinery: a new `(*checkpoint.Snapshotter).RestoreFiles(ctx)` method
(`internal/checkpoint/checkpoint.go`) delegates to the existing `Store.RestoreFiles` for that
snapshotter's own checkpoint ID, so the engine doesn't need a new field or a direct `*Store`
reference — it already receives the run's `Snapshotter` via `checkpoint.SnapshotterFrom(ctx)`, the
same context value `internal/server/messages.go` already wires in via `checkpoint.WithSnapshotter`
before every turn. `RestoreFiles` is nil-safe (mirrors `Capture`'s existing nil-safety), so a caller
with no checkpoint store configured (`s.checkpoints == nil`, or an embedded engine used outside the
daemon) is a no-op — rollback is skipped and today's retry-then-surface behavior is unchanged rather
than erroring. The rollback is surfaced to callers two ways: a new `Engine.Event.GuardFilesRestored`
int on the terminal `KindGuard` failure event, and the restored-file count appended in prose to that
same event's `GuardReason` (e.g. "... — rolled back 2 file(s) written this turn"), which the TUI's
existing `⚠ output guard: ...` warning line already renders verbatim with no new UI wiring. A plain
`e.logger.Warn` line also records the rollback (or a restore failure) for daemon-log visibility. Of
the finding's two suggested remediations — (a) quarantine/roll back on FAIL, or (b) a lighter
pre-write guard pass before irreversible writes — (a) was chosen as the more surgical fit that reuses
existing machinery end-to-end rather than adding a second validation pass; scope stayed tight,
matching the finding's Tier 3 "Effort: M" sizing. New tests: `TestGuardExhaustedRollsBackWrittenFile`
and `TestGuardExhaustedNoCheckpointStoreSkipsRollback` (`internal/engine/guard_test.go`, driving the
engine against the real `write_file` tool and a real temp-file-backed checkpoint store) plus
`TestSnapshotterRestoreFiles` and `TestNilSnapshotterRestoreFiles` (`internal/checkpoint/
checkpoint_test.go`). `go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean.

---

Before that, same day (2026-07-13): **P27.17** (FIND-16, Tier 3, CVSS 3.4) shipped: propagate a
shared/proportional budget ceiling into detached swarm sub-agent spawns, so they can't escape the
fan-out tree's cost cap.

The finding's own evidence pointed at `internal/swarm/agent.go`, but investigation before writing
any code found the actual production spawn path was `internal/tool/builtin/agent.go`'s
`spawnBackground` — the only place a detached/background sub-agent spawn (`agent` tool with
`background: true`) is created — and that it *already* carried the shared cost tracker forward
correctly: `task.Manager.Start` runs its job under a context derived from `context.Background()`
(severed from the request that created it), so `spawnBackground` reads `swarm.CostTrackerFromContext`
off the caller's ctx *before* calling `Start`, then explicitly re-attaches it (`swarm.WithCostTracker`)
onto the job's own context before handing off to the swarm backend — a fix that traces back to commit
`c368b4b`, well predating this threat-model pass, with an explanatory comment already in place at
`agent.go:476-484`. `internal/swarm/subprocess.go`'s `SubprocessBackend.Spawn` — the backend a
detached spawn actually reaches when running in subprocess mode — already used that carried-forward
tracker to compute a fair-share-reduced `WorkerSpec.RemainingBudgetUSD`/`RemainingTokens` (P10.3's
`remainingBudget`/`remainingTokens`, with P24.15's fair-share floor), and `internal/cli/worker.go`'s
`runWorker` already uses those spec fields as the spawned engine's actual budget/token caps in place
of the daemon's full configured ones. Each half already had its own unit coverage
(`TestAgentToolBackgroundSpawnCarriesCostTracker` against a stub backend; several
`TestSubprocessSpawn*RemainingBudget*`/`*FairShareFloor*` tests calling `Spawn` directly with a
context that already carried a tracker) — but nothing had ever exercised both halves together,
through the real production entry point, with a real (non-stub) subprocess backend actually
receiving the reduced ceiling. New `TestAgentToolBackgroundSpawnRespectsSharedBudgetCeiling`
(`internal/tool/builtin/agent_subprocess_test.go`) closes exactly that gap: it drives the real
`agentTool.Execute` with `background: true` through a real `task.Manager` and a real
`*swarm.SubprocessBackend` (backed by a small fake-worker `TestMain`, mirroring the pattern
`internal/swarm/subprocess_test.go` already uses to let the test binary double as the headless
worker process SubprocessBackend re-execs), with a shared `*cost.Tracker` that already has
significant prior spend attached to the caller's ctx before the detach point, and asserts the
detached child's `WorkerSpec` actually carries the fair-share-reduced remaining ceiling rather than
the daemon's full configured cap. To confirm the new test isn't vacuous, the carry-forward in
`spawnBackground` was temporarily disabled locally and the test observed to fail with
`RemainingBudgetUSD`/`RemainingTokens` both at zero (the daemon's full cap, unreduced) before the
carry-forward was restored and the test re-verified passing — i.e., this is a real, confirmed
regression test, not one that happens to pass regardless. **No production code changes were
needed**; this shipped as a verification/hardening item, closing a real "never verified end-to-end"
test-coverage gap rather than a live bug (mirroring how P27.15's writeup below found an existing
mechanism — the per-job `auto_approve` field — already satisfied that finding's core ask). Also
corrected a stale comment at `internal/swarm/subprocess.go:155-157`, which claimed the ctx-carried
tracker is nil for "some background paths" — no longer accurate given the above, and now says so
with a pointer to the new test. `go build ./...`, `go vet ./...`, the full `go test ./...`, and
`go test -race ./internal/swarm/... ./internal/tool/builtin/...` all pass clean.

Investigation also confirmed `internal/tool/builtin/agent.go`'s `spawnBackground` is the sole
production entry point for a detached/background sub-agent spawn — the only other `task.Manager.Start`
callers outside this package are `internal/server/helpers.go`'s cron job runner (runs a shell
command, not an agent spawn — no cost tracker involved) and `internal/tool/builtin/task.go`/`shell.go`
(backgrounded shell commands, same). `internal/debate` never detaches: its role runs happen
synchronously inline on the caller's own ctx, so they inherit the tracker through normal ctx
propagation without needing this carry-forward pattern at all. The in-process backend path
(`internal/server/server.go`'s `subAgentRunner`) needed no change either — every sub-agent's engine,
foreground or (once `spawnBackground` reattaches it) detached, shares the literal same `*cost.Tracker`
pointer, so `engine`'s budget gate checking cumulative `TotalUSD()` against one `BudgetUSD` already
bounds total spend across the whole in-process fan-out tree.

Before that, same day (2026-07-13): **P27.18** (FIND-19, Tier 3, CVSS 5.5) shipped: confine the `os`
sandbox backend's file reads to the workspace plus a toolchain allowlist, instead of the entire host
filesystem. Shipped ahead of the then-still-open P27.16/P27.17 (both Tier 3, but this one was
self-contained and didn't depend on either) — both have since shipped too, closing out Tier 3
entirely; see their entries above.

Seatbelt's profile was `(allow default)` with only `file-write*` denied outside the workspace, and
bwrap's was `--ro-bind / /` — read-only-mounting the whole host root — so a compromised shell command
running under `sandbox.backend: os` could still read (and, unless `network: false` was also set,
exfiltrate) `~/.ssh`, cloud credential files, or any other host file, even though writes were already
confined. `internal/sandbox/os_sandbox.go`: `seatbeltProfile` now adds a `(deny file-read*)` +
`(allow file-read* ...)` pair mirroring the existing write-confinement rules — narrower than the
`(allow default)` a from-scratch lockdown would need, since it leaves every other default-allowed
operation (process exec, mach lookups, sysctl reads, signals) untouched and only tightens
`file-read*`/`file-write*`; `bwrapArgs` drops `--ro-bind / /` entirely and instead `--ro-bind`s only
the allowlisted paths, so an unlisted read gets `ENOENT` rather than the real host file. The allowlist
(`defaultOSReadPaths`) is OS-specific system dirs (`/usr`, `/bin`, `/lib`, `/etc`, `/opt`, etc.) plus
common per-language toolchain caches under `$HOME` (`go`, `.cargo`, `.rustup`, `.npm`, `.nvm`,
`.pyenv`, `.gem`, `.bundle`, `.local`, `.cache`) — chosen so ordinary builds keep working — and
deliberately omits credential directories (`~/.ssh`, `~/.aws`, `~/.config`, `~/.kube`, `~/.docker`,
`~/.gnupg`): those are simply never bound/allowed, not detected-and-blocked, so they're unreadable
from inside the sandbox regardless of what's in them. `mergeReadPaths` dedupes the built-in list
against the new `sandbox.os_extra_read_paths` config field (`config.SandboxConfig.OSExtraReadPaths`,
threaded through `NewOSBackend`'s new `extraReadPaths` param) and drops any entry that doesn't exist
on the host, since bwrap fails to bind a missing source and a nonexistent seatbelt subpath is a
silent no-op either way. The network-egress-deny-by-default half of this finding needed no change:
`sandbox.network` already defaults to `false` and `NewOSBackend`'s `denyNet` is already `!allowNetwork`.

This remains an allowlist, not a hard boundary the way write-confinement is — a toolchain cache
directory that happens to also hold a stray credential file would still be readable, and the
allowlist has to stay broad enough to cover real toolchains. Docs updated to stop describing the `os`
backend's reads as fully unconfined (`docs/configuration.md`, `docs/security_scan.md`'s security
properties table and "when to use" guidance). New/updated tests: `TestSeatbeltProfile` and
`TestBwrapArgs` (`internal/sandbox/os_sandbox_test.go`) extended to assert the read-path allowlist
entries and the absence of `--ro-bind / /`; new `TestMergeReadPaths`. `go build ./...`, `go vet
./...`, and the full `go test ./...` pass clean.

Before that, same day (2026-07-13): **P27.15** (FIND-08, Tier 3, CVSS 5.6) shipped: apply the full
permission stack, not just the coarse mode check, at cron fire time.

Cron fire-time gating (FIND-03/P24.3) previously re-checked only the coarse permission mode via
`permission.Policy.Decide`, so an operator's text-based deny rule or the contextual egress/network
policy — both fully enforced for interactive tool calls — had no effect on an unattended cron fire.
`internal/server/helpers.go`'s `newCronRunFunc` now takes a `permCheck func(ctx, cron.Job) (bool,
string)` thunk instead of a bare `mode func() permission.Mode`; the new `Server.cronPermCheck`
builds the identical gate stack `buildGate` assembles for every interactive engine run — mode →
contextual egress/network policy → text allow/deny rules, with an empty `persona.Persona{}` since a
cron job has no persona of its own (matching how sub-agent runs skip the persona layers) — and
checks it against the real `"shell"` tool with `{"command": job.Command}` as input. A job's
`auto_approve` opt-in resolves any Ask-tier decision anywhere in that stack (previously it only
covered the single mode-level Ask); an explicit `deny` rule or a Deny-mode decision still blocks
regardless of `auto_approve`, and an explicit `allow` rule now lets a job fire unattended without
needing `auto_approve` set at all — matching how rules already override the mode gate for
interactive calls.

Construction-order wrinkle: `newCronRunFunc`/`cron.NewScheduler` are built early in `Server.New()`,
before the `*Server` exists, because the scheduler has to already exist when the tool registry
registers `cron_create`/etc. with `Cron: cronSched`. Rather than add a setter to `cron.Scheduler` or
restructure construction order, `New()` now predeclares `var s *Server` and the `permCheck` thunk
passed to `newCronRunFunc` closes over that variable, calling `s.cronPermCheck` — since the thunk is
only ever invoked at actual fire time (long after `New()` finishes assigning `s`), capturing the
not-yet-initialized pointer is safe standard Go closure semantics, not a race.

Also new: a human-facing review view for persisted cron jobs (the finding's "surface persisted
auto-approve jobs in a review view") — previously a job's `auto_approve` status was visible only to
the model itself via the `cron_list` tool, with no operator-facing surface at all. Added `GET
/cron/jobs` (`api.CronJobInfo`, `internal/server/sessions.go`), `Client.ListCronJobs`
(`internal/client/client.go`), and a new `aegis cron list` CLI command
(`internal/cli/cron.go`, wired into `root.go`'s session group) that flags each auto_approve job
inline (`--auto-approve-only` to filter to just those). The finding's other suggestion — "require a
separately-confirmed flag for `auto_approve` jobs" — was satisfied by the existing per-job
`auto_approve` field itself (already explicit, boolean, and distinct from the daemon's ambient
permission mode) rather than adding a second flag; its scope was extended in place instead, per the
scope note in [roadmap.md](roadmap.md#priority-order).

Docs updated (`docs/tools-reference.md`, cron_create section). New tests:
`TestNewCronRunFuncBlockedByDenyRuleEvenInAutoMode`, `TestNewCronRunFuncAllowedByRuleEvenInPlanMode`,
`TestServerCronPermCheck`, `TestHandleListCronJobs` (`internal/server/cron_test.go`); the 6
pre-existing `newCronRunFunc` tests were updated to the new signature via a `cronPermCheckFor` test
helper (builds a gate from `permission` package primitives directly, no full daemon needed) rather
than dropped. `go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean.

Before that, same day (2026-07-13): **P27.14** (FIND-04, Tier 3, CVSS 6.8) shipped: warn/recommend
against the unconfined `local` sandbox backend.

The default `local` sandbox backend runs shell commands on the host with only env-var stripping —
no fs/net/process isolation; the build-mode approval prompt and `ValidatePath` are the only
compensating controls. `internal/server/server.go`'s `New()` now logs a persistent startup `WARN`
("sandbox backend is 'local' (unconfined): ... consider sandbox.backend: os ... or container")
any time the effective backend is local and `permission.mode` isn't `plan` (i.e. execute-capable
tools are reachable at all) — this covers the default `build`-mode case, which previously got no
startup signal whatsoever; only the sharper `auto`-mode-with-no-approval and `auto_approve_exec`
cases already warned. `internal/cli/doctor.go`'s `doctorSandboxCheck` now reports the same
local-backend case as a `WARN` (with a `Fix` naming `sandbox.backend: os`/`container`) instead of a
silent `PASS` it previously buried in the detail text.

`aegis --first-init`'s generated global config template (`internal/cli/init.go`) now defaults new
installs to `sandbox.backend: os` — OS-level isolation (macOS seatbelt / Linux bubblewrap) with no
container runtime required — instead of `local`. This is a zero-risk-of-breakage change:
`SelectSandbox` already gracefully falls back to the unsandboxed `local` backend (logging the new
warning above) rather than hard-failing when no OS sandbox mechanism is available on the host
(bubblewrap not installed on Linux, or Windows, which has neither mechanism) unless
`sandbox.strict` is set. macOS installs — where `sandbox-exec` ships by default — get real write/
network confinement for free; Linux installs without bubblewrap and all Windows installs fall back
to exactly today's behavior plus the new warning. Existing on-disk configs are untouched (the
template only affects a fresh `--first-init`); the base `config.Load()` default used when no config
file exists at all (tests, embedders) deliberately stays `local` as the conservative absolute
fallback. "Defaulting new installs to the OS sandbox where available" was one of two options the
finding suggested (the other being a persistent warning banner) — both were implemented, since the
OS-sandbox default carries no downside given the graceful fallback.

Docs updated: `docs/configuration.md` (sandbox section default + rationale), `docs/security_scan.md`
(new "Local sandbox, execute-capable tools" note under Startup warning). Tests:
`TestNewWarnsLocalSandboxBuildMode` and `TestNewSkipsLocalSandboxWarningInPlanMode`
(`internal/server/sandbox_startup_test.go`, the latter confirming `plan` mode — which denies
execute entirely — is correctly exempted from the new warning); `TestDoctorCleanSetupExitsZero`
updated to assert the sandbox row is now a `WARN` naming "no isolation" rather than a silent `PASS`.
`go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean.

**Last updated:** 2026-07-13 — **P27.1** and **P27.2**, the P27 threat model's Tier 1, shipped.

*P27.1 — workspace-trust gate (FIND-01 + FIND-02, CVSS 8.5/8.2).* A cloned repository's
`.aegis/config.yaml` was merged with no confirmation and its `session_start`/`pre_tool_use` `hooks`
ran automatically — silent code execution (CWE-94) and silent widening of
`permission.mode`/`sandbox.*`/`mcp.servers`/`notify.webhook` (CWE-829) via config alone. New
`internal/workspacetrust` package: a small JSON store (`<data_dir>/workspace_trust.json`,
ACL-hardened via `fsguard.RestrictToOwner` like the session DB and `.env`) mapping normalized
absolute directory paths to a trust decision, deliberately anchored to the fixed user-level data
dir rather than `cfg.DataDir` — a hostile project config overriding `DataDir` must not be able to
point the trust store somewhere it controls. `config.Load()` now loads two koanf layers — the
normal one (defaults → global → project → env) and a "baseline" one with the project file excluded
— unmarshals both, and for an untrusted directory (no `workspacetrust` entry) with any diff between
them in `permission.*`/`sandbox.*`/`mcp.servers`/`notify.webhook`/`hooks`, overwrites the merged
config's fields with the baseline's, exposing what happened via a new `cfg.WorkspaceTrust` field
(`Dir`, `Trusted`, `Frozen`, `Changes []string`). Frozen state surfaces three ways: a daemon-log
WARN (`internal/server/server.go`, alongside the existing local-sandbox/auto-exec posture
warnings), a stderr banner printed before the TUI takes over the terminal
(`cli.warnWorkspaceTrust`, mirroring the existing `warnSandboxFallback`), and a new `aegis doctor`
check. New `aegis trust` command (`internal/cli/trust.go`) shows the diff and prompts before
recording a trust decision for the current directory (`--yes` to skip the prompt, `--status` to
inspect without prompting, `--revoke` to undo). The two pre-existing first-party writers of a gated
key — `config.PatchProjectSandbox` (`aegis sandbox use --project`) and
`config.AppendProjectPermissionRule` (the TUI's "allow always for this pattern" approval option,
TQ6) — now auto-trust the directory they write to as a side effect of a successful write, since
that write is an explicit local operator action in that exact directory, not a setting silently
inherited from a cloned repo's pre-existing config; this is also what keeps their existing tests
(which write-then-immediately-reload) passing unchanged. Tests: `internal/workspacetrust`
(persistence, revoke, normalization), `internal/config` (freeze-on-untrusted, apply-after-trust,
non-gated keys unaffected, no-project-config trivially trusted, both auto-trust call sites),
`internal/cli` (`aegis trust` status/yes/revoke/declined-confirmation, the new doctor check).

*P27.2 — `provider.base_url` allowlist/warn (FIND-03, CVSS 7.1).* `provider.base_url` had no
destination validation, so a project-config-sourced value could redirect API-key-bearing requests
to an attacker host (CWE-522) with no warning. New `providerfactory.validateBaseURL`, called from
`buildOne` for both the primary adapter and every fallback target: a non-loopback plaintext-HTTP
`base_url` is refused outright when a real API key would be attached (Ollama's non-secret
`"ollama"` placeholder is exempted, so the common local/LAN Ollama-over-HTTP setup keeps working
unchanged); a non-default host for a cloud provider (compared against `api.anthropic.com`/
`api.openai.com`) isn't blocked — legitimate corporate-gateway/self-hosted-proxy setups are common —
but logs a prominent WARN naming the override. `config.IsLoopbackBaseURL` exported (was already
`isLoopbackBaseURL`, used internally by `LocalPromptProfile`) so `providerfactory` reuses the exact
same loopback test rather than a second implementation. Tests: refuse-on-plaintext-non-loopback,
allow-on-loopback, warn-on-non-default-host, no-warn-on-default-host, plus the existing
fallback/cloud-gating tests unaffected (none of them set `BaseURL`).

`go build ./...`, `go vet ./...`, and the full `go test ./...` pass clean.

**Last updated:** 2026-07-13 — the P27 threat model's entire Tier 2 shipped: **P27.3, P27.4, P27.5,
P27.6, P27.7, P27.8, P27.9, P27.10, P27.11, P27.12, P27.13** (all 11 Tier 2 items; P27.1/P27.2 above
already closed Tier 1). Implemented in parallel by
7 isolated sub-agents in separate git worktrees, grouped by file-overlap risk rather than 1:1 with
finding IDs — 6 agents each owned a fully disjoint package (no two agents ever touched the same
file), and one agent bundled the 5 items that all needed to edit `internal/config/config.go`'s
shared `defaults()` map/`Load()` path (P27.3, P27.5, P27.9, P27.12, P27.13) into a single branch to
avoid the map-literal collisions that splitting them further would have caused. All 7 branches
merged into `main` with **zero manual conflict resolution** (git auto-merged every overlapping file,
including the two three-way merges touching `config.go` and `server.go`); full `go build ./...`,
`go vet ./...`, and `go test -count=1 ./...` pass clean on the fully integrated tree.

*P27.3 (FIND-05) — `security.redact_secrets` default on.* One-line default flip
(`defaults()["security.redact_secrets"] = true`); gitleaks-backed masking now runs by default on
read-tool/conversation content before it reaches a cloud provider. Fails open if gitleaks isn't on
PATH, so this is low-risk to default on.

*P27.4 (FIND-06) — default auth token for `mcp-serve`/ACP stdio.* Both interfaces previously ran
fully unauthenticated when `AEGIS_MCP_TOKEN`/`AEGIS_ACP_TOKEN` was unset. New
`config.GenerateAndWriteToken` (mirrors the daemon's own `generateAndWriteToken` for `daemon.token`)
auto-generates a random token per process start and writes it to an owner-only
`<data_dir>/mcp.token`/`acp.token` (`fsguard.RestrictToOwner`-hardened) when the env var isn't set;
an explicit env var still always wins. `--help` text and the `.aegis/config.yaml` init template now
document the token file path so a calling harness can discover it. `mcpserver`/`acp` library APIs
are unchanged (empty token still means "open") — only the CLI wiring now guarantees a non-empty
token by default.

*P27.5 (FIND-13) — pinned-cert loopback TLS on by default.* The riskiest of the five bundled items:
`defaults()["server.tls.enabled"] = true` turns on the pinned self-signed-cert TLS
(`internal/server/tls.go`, P24.18) for client↔daemon loopback traffic that was previously plaintext.
Verified end to end, not just via unit tests — built the binary, ran `aegis serve`, confirmed
`daemon.crt`/`daemon.key` auto-generate and `aegis sessions list`/`aegis doctor` succeed over the
pinned HTTPS connection with zero manual setup (`client.NewFromConfig` and the TUI's daemon
auto-start path already had full TLS support wired in from P24.18). Along the way, found and fixed a
real latent bug (noted again below): `envKeyCallback`'s single-split heuristic never reached the
nested `server.tls.enabled` key, so the documented `AEGIS_SERVER_TLS_ENABLED` env-var escape hatch
silently did nothing — now fixed with a regression test, which matters more once TLS is the default.

*P27.6 (FIND-07) — trust-wrap project context/memory files.* `AGENTS.md`/`CLAUDE.md`/
`.aegis/context.md`/`.aegis/memory.md` content is now wrapped with the same `internal/trust.Wrap`
untrusted-provenance marker P24.4 already applied to persona/skill bodies, at the two live read
sites (`internal/memory/context.go`'s `loadContextDirect`, `internal/memory/memory.go`'s
`loadDirect`) — both project- and user/global-sourced files get the identical wrap, matching the
P24.4 precedent of not distinguishing provenance for any disk-loaded file.

*P27.7 (FIND-09) — gate project-persona control fields on workspace trust.* A project persona's
`mode`/`tools`/`rules`/`output_guard` frontmatter is now dropped at parse time
(`internal/persona/load.go`) unless the persona's project directory is trusted per the P27.1
`workspacetrust` store — queried directly rather than via `cfg.WorkspaceTrust.Trusted`, since that
config-level flag is forced true whenever no project `config.yaml` exists to freeze, which would
have missed a hostile repo shipping only a persona file. `Model`/`Description`/`System` (already
wrapped by P24.4) are untouched; user/global personas keep full control unconditionally.

*P27.8 (FIND-10) — SSRF-safe dialer for the HTTP/SSE MCP client.* `internal/mcp/http.go` gained its
own `mcpSSRFSafeDialer`/`mcpValidateNotPrivate`/private-CIDR table, deliberately a small duplicate
of `internal/tool/builtin/web.go`'s `ssrfSafeDialer` rather than a cross-package import — matching
existing precedent in `internal/security/target.go`, which already duplicates the same table rather
than coupling `internal/sandbox` to `internal/config`. Both the MCP client's POST `/message` and GET
`/sse` requests are now protected, with redirect targets re-validated the same way `web_fetch` does.

*P27.9 (FIND-11) — DAST `allowed_targets` sourced from user/global config only.* `config.Load()` now
unconditionally overwrites `cfg.Security.DAST.AllowedTargets` from the same project-excluded
baseline layer the P27.1 trust gate already computes — reusing that machinery rather than a third
koanf load. This is intentionally stronger than trust-gating: a hostile repo's `allowed_targets`
never applies, even after the directory is `aegis trust`-ed, since project-controlled network-scan
targets is a different risk shape than project-controlled permission mode.

*P27.10 (FIND-18, ACL half) — fsguard-harden `longmem.db`/`knowledge.db`.* Both now call
`fsguard.RestrictToOwner` on the main db file (fatal on error, since Aegis creates the file itself)
and best-effort on `-wal`/`-shm` sidecars (logged, not fatal), exactly mirroring
`internal/session/session.go`'s existing `hardenDBPermissions`.

*P27.11 (FIND-20) — harden swarm mailbox file permissions.* Processed mailbox files now write
`0o600` instead of `0o644`, and the `teams/` root directory is `fsguard`-hardened on every
`OpenMailbox`. This surfaced a real pre-existing Windows ACL bug: `fsguard_windows.go`'s ACE had no
inheritance flags, so hardening a populated directory left descendant files with an effectively
empty inherited DACL — denying even the owner. Fixed by adding `OICI` (object-inherit/container-
inherit) flags, a no-op for the pattern's other pre-existing file-only call sites (daemon token,
session DB, `.env`, TLS key). The per-run shared-secret/HMAC message-authentication stretch goal was
explicitly scoped out for a future item — the file-permission fix was the priority and is complete.

*P27.12 (FIND-14) — default concurrency/rate caps + invalid-auth throttling.*
`server.max_concurrent_runs` now defaults to 10 and `server.max_run_duration_sec` to 1800s (bounding
only top-level HTTP-driven runs, not in-process swarm sub-agents — a normal single-user session
never approaches either ceiling). `recordInvalidAuthAttempt` (`internal/server/auth.go`) gained a
consecutive-failure-streak lockout (separate from the existing cumulative FIND-11 counter) with
exponential backoff (1s→60s cap) past a threshold of 10 attempts, set above the pre-existing
`TestServerInvalidAuthAttemptsLoggedAndCounted` test's 6 attempts.

*P27.13 (FIND-12, default-on half) — injection scan on by default.* `search.scan_output` now
defaults true via the `defaults()` map; the per-MCP-server `scan_output` (a list element with no
koanf-defaults mechanism) was converted to `*bool` with a `ScanOutputEnabled()` resolver, mirroring
the existing `SecurityToolConfig.Enabled` pattern, and now also defaults true. Confirmed via
`internal/trust.Wrap` that a scan hit only adds a visible warning — it never blocks or mutates
content — making this genuinely low-risk to default on.

**Last updated:** 2026-07-12 — **P22.5, P22.6, P20.2, P20.3** shipped as a second user-selected
batch of four Tier 4 parked items, same day as the first batch below. P25.9 and P6.1 were
deliberately excluded from this round (both Effort L, both large/high-blast-radius — daemon
singleton rescoping and the core engine streaming loop, respectively — better suited to focused
solo work than parallel automation) and P13.3.3 stays excluded as its ACP-host-usage precondition
still hasn't materialized. All four were implemented in parallel by isolated sub-agents in separate
git worktrees, then merged into `main` sequentially; one doc-only conflict (`docs/tui-guide.md` —
both P22.5 and P22.6 appended to the same table) was resolved by combining both additions, no code
conflicts. `go build ./...` and the full `go test ./...` both pass clean on the merged tree.

*P20.3 — hardware-aware local model recommendation.* New `internal/hwinfo` package detects CPU
core count (`runtime.NumCPU()`, always reliable) and total system RAM via platform-specific,
`//go:build`-tagged best-effort probes (`/proc/meminfo` on Linux, `sysctl -n hw.memsize` on macOS,
Win32 `GlobalMemoryStatusEx` via `golang.org/x/sys/windows` on Windows — matching the existing
syscall idiom in `internal/fsguard/fsguard_windows.go`), failing soft to an "unknown" source on any
other platform or probe failure — never erroring. Deliberately excludes GPU/VRAM detection,
following the precedent P17.5 already set for the exact same reason: "no VRAM/GPU/host
introspection — Aegis would be reimplementing that heuristic blind from a fragile,
platform-specific proxy signal." `internal/modelcatalog`'s `TierLocal` entries now carry a
`MinRAMGB` floor (qwen3/qwen2.5-coder: 4, llama3.1: 8, deepseek-r1: 16 — qualitative rules of
thumb, not measured benchmarks, matching `Curated()`'s existing framing) and a new
`RecommendLocal(hw)` filters to what fits detected RAM, falling back to the full unnarrowed list
when RAM is undetected. Surfaced via `aegis models --recommend` (detected hardware + narrowed
table + `ollama pull <model>` suggestions for anything not already pulled, cross-referenced against
`internal/discover`'s Ollama probe — printed only, never auto-executed, matching P13.4's
`security_advise` guarded-suggestion precedent) and a per-entry hardware-fit badge in the TUI's
`/models` picker (`internal/tui/modelpicker.go`). Tests: portable tests for the fail-soft/unknown
path plus platform-guarded tests per OS (skip gracefully when the real facility isn't reachable);
table-driven `RecommendLocal` coverage; build-tagged files verified to compile cleanly under
cross-compiled `GOOS=linux`/`GOOS=darwin` in addition to the native Windows build. Docs:
`docs/providers.md`, `docs/cli-reference.md`.

*P20.2 — blind model compare (`aegis compare`).* New `aegis compare <model-A> <model-B> [prompt]`
command (`internal/cli/compare.go`), a separate command rather than a `parallel` flag since its
output contract — withhold identity, vote, reveal, optional synthesis — is different enough from
`parallel`'s plain interleaved-progress contract to muddy both if merged. Mirrors
`runOneParallel`'s create-session/PATCH-model/post-message/drain-events shape (`runOneCompare`),
setting each session's model via the existing P14.7 `PATCH /sessions/{id}` mechanism. Identities
are hidden during the run — progress is logged only by generic label ("Response 1"/"Response
2"), with slot assignment randomized via `crypto/rand` so position isn't a tell — then revealed
after the user votes (`1`/`2`/`tie`/`skip` read from stdin). `--synthesize` (default off, plus
`--synth-model`) makes one further call combining both revealed answers, clearly labeled as a
synthesis rather than a third blind response. Both underlying sessions persist and remain
resumable via `aegis --resume <id>`, matching the existing convention `parallel.go` already
established (it never deletes its sessions either). Tests: vote parsing, a regression test proving
mid-run logs never leak model identity, randomization producing both slot orders, and command/flag
construction. Docs: new `## aegis compare` section in `docs/cli-reference.md`.

*P22.6 — raw scrollback mode.* `/scrollback [on|off]` releases the TUI's dashboard rendering for
native terminal scrollback/selection/search. The investigation corrected the roadmap item's own
framing: bubbletea v2 moved alt-screen/mouse-capture control from `tea.NewProgram` options to
per-frame `tea.View()` fields, and this app's `View()` does set `AltScreen=true` /
`MouseMode=CellMotion` on every frame — so alt-screen genuinely was on, contrary to what a grep for
the v1-era `WithAltScreen`/`EnterAltScreen` APIs suggested. But alt-screen turned out to be only
half the blocker: `transcriptPane.View()` (`internal/tui/transcript.go`) independently clips to a
bounded, fixed-height, in-place-redrawn viewport regardless of alt-screen state — the same screen
rows get reused every frame instead of old content ever scrolling into the terminal's real
history. Raw scrollback mode flips both: `View()` sets `AltScreen=false`/`MouseMode=None`, and the
transcript's rendered height tracks its own unbounded content height instead of the terminal
window, so appended lines genuinely scroll off into terminal history as the conversation grows.
The sidebar, scrollbar column, and terminal pane (`Ctrl+X`) are hidden while it's on (they assume a
fixed-height dashboard) and restored — including prior sidebar open/closed state — when toggled
back off. Off by default, resets on restart, same convention as `/tools` and `/humor`. Known
cosmetic limitation (not pursued, S/M effort tier): dialog/picker overlays composite against a
canvas sized to the terminal window, not the grown transcript frame, so one opened after the
transcript has scrolled past a screenful renders near the top rather than the current bottom.
Tests: `internal/tui/scrollback_test.go` (dispatcher sentinels, on/off/toggle transitions,
`View()` field assertions, the unclipping rendering branch including content appended after the
mode is already on, sidebar-hidden branch). Docs: `docs/tui-guide.md`.

*P22.5 — `/side` ephemeral side conversation.* `/side <question>` answers a quick, unrelated
question without touching the main conversation's history, cost counters, or active session id.
`cmdSide` (`internal/tui/slash.go`) creates a genuinely separate session (`Mode: "plan"` —
read-only, since the handler has no way to surface an interactive approval mid-flight — default
persona/system prompt, not the current session's), posts the question, drains its SSE stream into
an answer, and appends the Q&A to the main transcript as plain output clearly marked `[side <id8>]
<question>`; `SwitchToSession`/`ReloadSession` are never set, so the main session is provably
untouched (covered by a dedicated isolation test). The side session is kept rather than deleted —
abrupt deletion would lose the answer if the user wants to revisit it, and it stays fully usable
via `/session list`, `/fork`, `/rewind` like any other session — but its title is prefixed `"[side]
"` so it's visually distinct in the session list rather than adding a new `Ephemeral` field that
would need threading through the store and every session-management surface for what a title
prefix already accomplishes. Tests: `internal/tui/side_test.go` (usage-error fast path,
`commandDefs` registration guard, the isolation-invariant assertion). Docs: `docs/tui-guide.md`.

*P13.4 — `security_advise` engagement tooling (notebook + CVE lookup + guarded suggestions +
status digest).* New builtin tool `security_advise` (`internal/tool/builtin/advise.go`, capability
`network`) with an action-style interface: `note`/`list`/`log` against a file-backed, append-only
JSONL **engagement notebook** (`internal/security/notebook.go`) keyed by a sanitized engagement
name and rooted under the daemon's per-user data directory — deliberately a dedicated store rather
than extending `internal/memory`'s single project/user file, which doesn't fit a
named-multi-notebook, multi-day-persistent shape (the same conclusion the original 2026-07-06
deferral reached: "a real idea, separate scoped item"). `cve_lookup` queries the NVD CVE 2.0 REST
API by ID or keyword (`internal/security/cve.go`), with injectable base URL/HTTP client for
tests and explicit 403/429 handling that surfaces a clear rate-limit error (naming the `NVD_API_KEY`
env var for a higher limit) instead of hanging. `suggest` (`internal/security/suggest.go`) returns
**guarded** next-step suggestions as plain text only, from simple explainable keyword rules over
notebook content (no recon logged, findings undocumented, a CVE mentioned but never looked up) —
it never auto-executes a tool and isn't a second LLM call, preserving human/model-in-the-loop
judgment per the original "guarded" framing. `status` returns a digest of the current engagement
rather than extending `api.StatusInfo`/`/status` as P13.4.4 originally sketched — that endpoint is
daemon-global with no existing per-entity-key precedent, so folding a per-engagement digest into it
would have been a bigger, differently-shaped change than the digest itself is worth; documented as
a deliberate scope call, not an oversight. Wired into the `red-team`, `security`, and
`security-critic` personas' advisory `Tools:` lists (matching how `dast_scan`/`recon_scan` were
added for P13.5/P13.8); left off `security-arbiter` since that persona introduces no new claims and
does no independent investigation, so a research/notebook tool doesn't fit its role. Tests:
`internal/security/{notebook,cve,suggest}_test.go` (notebook persistence-across-restart and
engagement-isolation, CVE lookup against a mocked HTTP transport — no live network calls — covering
ID/keyword/403/429/500/malformed-args, and table-driven suggestion-rule coverage) plus
`internal/tool/builtin/advise_test.go` for tool-level action dispatch. Docs:
`docs/tools-reference.md` and a new section in `docs/security_scan.md`. This closes out P13 except
P13.3 (terminal enhancements, still Tier 4/parked).

*P9.4 — opt-in per-task model routing.* `ProviderConfig.TaskRouting` (`koanf:"task_routing"`,
default `false`) lets a session route each user-facing turn between `Model` and the existing
`SmallModel` (previously used only for title generation, compaction, and P25.3's output-guard
verdicts — never for an actual answering turn). Routing only engages when both `TaskRouting` is
enabled and `SmallModel` is configured, mirroring the existing "no SmallModel = no behavior
change" precedent those three call sites already established; an explicit per-session `/model`
override (P14.7) always short-circuits routing entirely; a turn continuing a session with prior
tool calls stays on the big model rather than bouncing down, since a task the model already judged
worth using tools for isn't a "simple turn" candidate. The classifier (`internal/server/routing.go`
`classifyTurn`) is a purely local heuristic — no extra model call, which would defeat the point —
biased toward the expensive model whenever uncertain: a false negative (big model on an easy turn)
just costs a bit more, a false positive (small model on a hard turn) produces a wrong answer.
Signals, in priority order: prior tool calls in the session (checked first), a code fence, ≥2
multi-step list markers, message length (words or chars, to also catch dense single-token content
like stack traces), and ≥3 sentence boundaries. Logs a `Debug` line with the routing outcome so
this is observable rather than a silent behavior change. Tests: `internal/server/routing_test.go`
(table-driven classifier cases plus a routing-resolution test proving the session override still
wins, mirroring `TestGuardModelPrefersSmallModel`'s shape). Docs: `docs/configuration.md`.

*P13.3.2 — `@shell` context token.* Extends the TUI's `@`-mention system (`internal/tui/completion.go`'s
`refTypes`, previously `image:`/`diagnostics`/`url:`/`symbol:`, only `image:` locally resolved) with
`@shell` (default last 50 lines) / `@shell:N` (explicit line count), resolved on submit by pulling
the embedded terminal pane's most recent run (`termPane.lastCmd`/`lastOutput`/`lastExitCode`/
`lastFailed`, tracked since P13.3.1's shell-aware error assist) and splicing formatted text in place
of the token — the same clean-and-inject shape `extractImageRefs` already uses for `@image:`, just
text instead of an image attachment. A word-boundary-anchored regex (`@shell(?::(\d+))?\b`) avoids
false-matching `@shellac`; no terminal run yet substitutes a short placeholder rather than failing
submission. Tests: `internal/tui/shellref_test.go` (placeholder case, default/explicit line counts,
failed-command framing, multiple occurrences in one message, the token-boundary negative case).
Docs: `docs/tui-guide.md`'s `@` references table and Terminal Pane section. `@diagnostics`/`@url:`/
`@symbol:` are untouched — they stay textual, resolved by the agent's own tools, not locally.

*P24.21 — bearer-token scrubbing in `Client` process memory (FIND-33).* The only one of 35 findings
from the 2026-07-10 STRIDE-A threat model still open (the other 34 shipped as P24.1-P24.20/P24.22
or were verified existing controls) — Low severity, CVSS 2.8, explicitly low priority per the
finding itself ("host/OS access is already a significant compromise"). Best-effort defense-in-depth,
not a hard guarantee, in a garbage-collected language with immutable strings — documented as such
in code. `Client.authToken` changed from `string` to `[]byte`; `WithTokenFile` reads the token file
straight into the byte slice, never round-tripping through a string; the public `WithToken(string)`
API still takes one unavoidable copy at the boundary. New `Client.Zero()` overwrites the backing
bytes in place and nils the field. `setAuth`'s own `"Bearer "+string(...)` concatenation and
`http.Request.Header.Set`'s internal copy remain outside `Zero`'s reach — documented explicitly
rather than oversold. Wired at real lifecycle points, each a judgment call commented in code:
one-shot CLI commands `defer cl.Zero()` right after construction (`internal/cli/{sessions,bg,doctor,
parallel,ui}.go` and others); the long-lived `acp`/`mcp-serve` stdio bridges defer `Zero` after the
daemon-reachability reassignment so it captures the client actually used; the interactive TUI's
client is scrubbed by `tui.Run` right after `p.Run()` returns. Daemon-side token generation/storage
was left untouched — out of scope for this client-side finding. Tests:
`TestZeroOverwritesBackingBytes` (aliases the backing array before calling `Zero`, asserts every
byte was actually overwritten, not just the field nilled) and `TestZeroSafeOnEmptyClient`, plus a
`-race` run on `internal/client`.

*P26.2 — fixed a `sessionWorkdirs`/`sessionSkills` map leak on session delete.* A fresh regression
in the very P25.1/P25.8 batch that just shipped: `handleCreateSession` (internal/server/sessions.go)
populates `Server.sessionWorkdirs` (P25.1) and `activateSessionSkill` (internal/server/server.go)
populates `Server.sessionSkills` per session, but `handleDeleteSession` only ever called
`s.sessionTools.Delete(id)` — never `sessionWorkdirs.Delete(id)` or `sessionSkills.Delete(id)` — so
both `sync.Map`s grew one entry per deleted session forever on a long-lived daemon. The same
never-evicted-entry shape as the swarm-mailbox leak P8.3 already fixed, just in two more maps.
Fix: `handleDeleteSession` now also calls `s.sessionWorkdirs.Delete(id)` and
`s.sessionSkills.Delete(id)`. Test: new `TestServerDeleteSessionClearsWorkdirAndSkillMaps`
(internal/server/server_test.go) creates a session with an explicit `Workdir`, activates a
built-in skill on it, deletes it, and asserts both maps no longer hold an entry for that session ID
— failed against the pre-fix code, passes now.

*P15.13 — web UI session workdir picker + display.* P25.1 gave sessions a `Workdir` field over the
API, but the web UI never sent one — a browser has no filesystem cwd of its own, so every web
session silently fell back to the daemon's root, P25.1's exact failure mode surviving for the one
client that most needed the fix. Backend: `api.StatusInfo` gained `Workspace` (already added for
P26.1, unused by the frontend until now) and a new `WorkdirAllowlist` field
(internal/api/api.go, internal/server/server.go's `handleStatusInfo`) mirroring
`server.session_workdir_allowlist`, so the picker can suggest directories known to be accepted
instead of guessing blind. Frontend (internal/server/webui/frontend/src): the sidebar's "+ New"
button now expands an inline directory picker (`SessionList.tsx`) — a free-text input backed by a
`<datalist>` of suggestions (the allowlist plus recently-used workdirs, deduped and sorted by
recency, derived client-side from the already-loaded session list — no new endpoint needed for
that half) — sent as `workdir` on `POST /sessions` when non-empty; leaving it blank keeps today's
behavior (the daemon's default workspace, named in the hint text). The chat header
(`app.tsx`'s topbar) now shows a `📁 <workdir>` chip next to the persona/model chip, falling back
to the daemon's workspace label when the session has none. Error handling: `api()`'s shared fetch
wrapper (`api.ts`) now unwraps the daemon's `{"error": "..."}` JSON body into the thrown `Error`'s
message instead of surfacing the raw JSON blob — a small, backward-compatible improvement that
benefits every existing toast, not just this one — so a rejected workdir (nonexistent path, or
outside the allowlist once `server.allow_remote` is set) shows the daemon's actual 400/403 message
in a toast; the picker's "Start chat" button keeps the dialog open (rather than silently falling
back to the default workspace) until the user fixes the path or cancels. Tests: extended
`TestServerStatusEndpoint` (internal/server/server_test.go) to assert `WorkdirAllowlist` round-
trips through `GET /status`. Manually verified end-to-end against a real running daemon over the
raw HTTP API (no browser available in this environment): `POST /sessions` with a valid absolute
workdir creates the session with that `workdir` echoed back and persisted; the same request with a
nonexistent path returns `400 {"error":"workdir does not exist or is not a directory"}` — the exact
message the picker now surfaces instead of a silent fallback.

*P26.1 — `aegis doctor` preflight self-diagnostic.* Each P25 fix addressed one silent-
misconfiguration class the live eval hit (sandbox, workdir, guard, tokens) in its own corner of the
codebase; `doctor` (internal/cli/doctor.go) generalizes the pattern into a single command an
operator runs first. Every check but the last works standalone with no daemon required — a true
preflight, safe to run before `aegis serve` — and prints a PASS/WARN/FAIL row plus a corrective
config key or command for anything short of PASS: **provider** (Ollama `/api/tags` reachability
and configured-model-is-pulled check via the existing `ollamaNativeBase` helper, or a cloud
provider's API key actually present in the environment); **sandbox** (re-runs the exact
`server.SelectSandbox` the daemon calls at startup — the same function the subprocess swarm worker
already reconstructs — so a backend that silently falls back to unsandboxed local, P25.2's bug
class, is caught before the daemon ever starts, not just after); **scanners** (`security.Resolve`
across every *enabled* built-in scanner descriptor — opt-in tools left off are silently skipped,
so an unconfigured DAST/zap scanner isn't a false alarm); **output guard** (warns when
`output_guard.mode: llm` targets a model that looks like a thinking model — an explicit
`provider.think`/`reasoning_effort`, or a name carrying a marker like "-deep"/"deepseek"/"-r1"/
"qwq" — with no `provider.small_model` set, P25.3's failure mode); **workdir allowlist**
(`server.session_workdir_allowlist` posture — a no-op on the default loopback bind, worth flagging
once `server.allow_remote` is set and the allowlist is still empty); and, only if a daemon is
reachable, **daemon** (`/healthz` reachability, degrading to a WARN rather than a FAIL when none is
running), **daemon workspace** (new `Workspace` field on `GET /status`'s `api.StatusInfo`, set from
`Server.workspace` — compared against the CLI's own cwd to catch P25.1's exact failure mode: a
session created with no explicit `Workdir` silently getting the daemon's workspace instead of the
caller's), and **daemon sandbox** (cross-checks the *running* daemon's live
`SandboxFallback`/`SandboxFallbackReason` against what the standalone sandbox check just computed
from the config on disk — a mismatch means the daemon is stale relative to a config edit and needs
a restart). Nonzero exit on any FAIL row so it can gate scripts. Tests
(internal/cli/doctor_test.go): `TestDoctorNamesPodmanMisconfig` reproduces P25.2's exact live-eval
misconfig (`sandbox.backend: podman`, no podman runtime) and asserts both the WARN row and the
named `sandbox.backend` config key; `TestDoctorCleanSetupExitsZero` asserts a nil error (no FAIL
rows) on an unmodified config; `TestLooksLikeThinkingModel`/`TestDoctorGuardCheck`/
`TestDoctorWorkdirCheck`/`TestSamePath`/`TestDoctorProviderCheckMissingAPIKey` cover the pure
per-check logic directly. Manually verified end-to-end against a real running daemon: starting
`aegis serve` from one directory and running `aegis doctor` from another reproduces P25.1's
mismatch and names it correctly, alongside the daemon's own live sandbox-fallback state.

*P25.8 — thread session workdir through the spawn/cron/debate seams.* P25.1 gave top-level
sessions their own working directory, but three seams never received it and kept silently
operating in the daemon's root regardless of which session drove them. (a) **Swarm sub-agents:**
`swarm.SpawnConfig` gained a `Workdir` field; the `agent` tool (internal/tool/builtin/agent.go)
captures the spawning turn's workdir via `tool.WorkdirFromContext` once per `Execute` call and
sets it on every `SpawnConfig` it builds (single-agent, workflow, and debate-mode spawns alike);
`subAgentRunner` (internal/server/server.go) now sets `engine.Options.Workdir` from `cfg.Workdir`
explicitly instead of relying on the parent session's ctx value leaking through — the fix that
actually matters for a detached/background spawn, whose job runs under a context derived from
`context.Background()` (`task.Manager.Start`) and would otherwise silently lose it; the subprocess
backend threads `Workdir` through `WorkerSpec` JSON (already automatic, being a `SpawnConfig`
field) and `internal/cli/worker.go`'s new `resolveWorkerCwd` prefers it over the worker process's
own cwd. (b) **Cron:** `cron.Job` gained an optional `Workdir` field (SQLite migration,
`Scheduler.Create` parameter); `cron_create` (internal/tool/builtin/cron.go) captures the calling
turn's workdir the same way the agent tool does; `cronShellRunner`/`newCronRunFunc`
(internal/server/helpers.go) now take a per-fire `dir` argument that falls back to the daemon's
default cwd when a job carries none. (c) **Debate:** `api.DebateRequest` gained a `Workdir` field
(session-less, so it needs its own — there's no session to inherit from); `handleDebate`
(internal/server/debate.go) validates it through the same `resolveSessionWorkdir` P25.1 uses, and
`debateRoleRunner` sets `engine.Options.Workdir` from it so every role's tool calls — and
`debate.WithFiles`-named fixture paths — resolve against the request's directory instead of always
falling back to the daemon's default workspace. Tests: workdir-propagation coverage across all
three swarm spawn shapes (foreground in-process, background/detached, subprocess) —
`TestAgentToolCapturesSpawningWorkdir`/`TestAgentToolWorkflowCapturesSpawningWorkdir`/
`TestAgentToolDebateCapturesSpawningWorkdir`/`TestAgentToolBackgroundSpawnCarriesWorkdir`
(internal/tool/builtin), `TestSubAgentRunnerUsesSpawnConfigWorkdir` (internal/server),
`TestSubprocessSpawnPropagatesWorkdir` (internal/swarm), `TestResolveWorkerCwdPrefersSpecWorkdir`
(internal/cli); cron round-trip and fire-time propagation —
`TestCronCreateCapturesCallingWorkdir` (internal/tool/builtin),
`TestNewCronRunFuncPassesJobWorkdir` (internal/server), `TestStoreRoundTrip` workdir assertion
(internal/cron); debate — `TestDebateRoleRunnerUsesRequestWorkdir` and
`TestHandleDebateRejectsBadWorkdir` (internal/server).

*P25.7 — promoted the live-eval harness into `internal/eval`.* Every P25 finding above was found
by driving the running daemon over its real HTTP/SSE API against a live local model — the
existing `internal/eval` scenario tier runs a scripted adapter (good for engine-loop regressions,
blind to daemon/sandbox/guard integration) and the `live_eval` tier judges prompt/persona quality
against a bare engine, neither of which touches the seam P25.1–P25.6 actually lived in. Ported
`research/eval-harness-drive.py` to a `live_workflow`-tagged Go test
(`internal/eval/live_workflow_test.go`, `TestLiveWorkflow`): it writes the seeded-bug
`temps.py`/`temps.csv` fixture, `chdir`s into it (mirroring the harness recipe's `cd
<target-project> && aegis serve` — the exact "daemon cwd wrong" failure mode P25.1 fixed), builds
a real daemon via `server.New` (full production wiring, not the synthetic `newWithDeps` other
`internal/server` tests use) served over an in-process `httptest.Server`, and drives it with
`internal/client.Client` — the same HTTP/SSE seam the TUI and web UI use. Three subtests assert
workflow-shape invariants rather than golden text: `FixSeededBug` (guard off) checks the task
actually completed (re-running the fixture script itself rather than trusting the model's claim),
≥2 shell calls (initial run + verification re-run), no `web_search`/`web_fetch`/`find /`-style
detours, a tool-call ceiling, no unrequested files or `remember` calls (P25.6), non-zero token
usage on `done` (P25.5), and ≤2 approval requests under auto-approve (P25.4); `GuardNoMetaLeak`
(guard on) checks the final answer never leaks PASS/FAIL/VERDICT meta-text (P25.3);
`LocalPromptProfileReducesFirstTurnTokens` runs an identical trivial prompt against a `local`- and
a `default`-profile daemon and asserts the local profile's first-turn input tokens are strictly
lower (P25.6). On-demand only, gated behind the `live_workflow` build tag, same
no-scheduled-CI-job policy as `live_eval` — documented next to it in CLAUDE.md. Skips (not fails)
when no `python3`/`python` is on PATH, since a missing interpreter is an environment gap, not a
regression.

*P25.4 — approval ergonomics: dead hotkeys, bad generated rules, read-only shell gating.* Three
independent frictions from the live TUI run, all approval-related. (a) **Dead `y` hotkey:** the
approval dialog already short-circuited key handling, but the "Steer the model" composer stayed
visually focused (blinking cursor) and could still intercept input on some message types. Fixed
by blurring the composer the instant a dialog opens and refocusing it on every resolution path
(answer or run-abort); the shared textarea-update path is skipped entirely while a dialog is up,
and the status bar shows "⏸ respond to the approval dialog above" so focus state is visible.
(b) **Generated Allow-always rules:** `suggestRulePattern`/new `suggestShellPattern`
(internal/tui/approval.go) now strip a leading `cd <dir> &&` and env-var prefixes before keying
the suggestion on binary + subcommand (`git status*`, not the old useless
`cd ... && python3 temps.py*`), and refuse to emit any pattern containing a
redirection/pipe/substitution/chaining metacharacter — those fall back to "once only — no safe
rule; write one by hand" rather than ever baking in something like `shell(cat >*)`.
(c) **Read-only shell gating:** a new classifier (`internal/tool/builtin/shell_readonly.go`)
allowlists read-only argv[0]+flag shapes (`ls`, `cat`, `head`, `tail`, `wc`, `pwd`, `stat`,
`file`, PowerShell read cmdlets, `git status`/`log`/`diff` without config-override flags) and
rejects outright on any shell metacharacter; wired through a new optional
`tool.CapabilityOverrider` interface and `tool.EffectiveCapability` helper consumed by
`permission.Gate.Check`, `engine.serializeTool`, and secret redaction — the shell tool's static
`Capability()` (used for rule subject-matching) is untouched, so deny rules against `shell` still
block a read-classified call. Tests: `TestApprovalDialogTakesKeyPriorityOverComposer`, table-driven
rule-generation tests (cd/env stripping, metacharacter refusal), and classifier bypass-attempt
tests (`cat f > /etc/x`, `git -c core.pager=sh log`, `ls; rm -rf /` all correctly rejected).

*P25.5 — token-usage observability for local providers.* Every API-driven run reported
`done in=0 out=0` on the SSE `done` event while the TUI status bar showed live counts for the
same engine, because `internal/engine/engine.go`'s terminal `KindDone` emission
(`emit(Event{Kind: KindDone})`) carried no usage at all — per-turn estimated usage
(`provider.Usage`, `IsEstimated`) was already computed and emitted on each `KindTurnDone` event,
but only the TUI's live status bar read it. `Run()` now accumulates each turn's usage (real or
character-estimated) as turns complete, tracking whether any contributing turn lacked real
provider-reported usage, and attaches the accumulated `*provider.Usage` to the final `KindDone`
event — `IsEstimated` set accordingly, passed through the existing `toAPIEvent`/`TokensEstimated`
wiring unchanged. `internal/server/messages.go` was already folding every `KindTurnDone`'s usage
into session totals (a pre-existing P10.5 path), so `SessionMeta` needed no change — just test
coverage confirming it. Tests: `TestDoneEventCarriesEstimatedUsage` (engine),
`TestDoneEventAndSessionMetaCarryEstimatedTokens` (server, full HTTP/SSE round-trip with a
zero-usage adapter). Live-harness verification against a real Ollama daemon (confirming the
eval-harness summary now shows non-zero in/out) is deferred to the next live-eval session.

*P25.6 — local-model profile: prompt weight + scope-creep guardrails.* The first model call
carried ~10k input tokens (system prompt + always-exposed tool schemas + repo map + skills
preamble) before the user said a word, and `qwen3coder:30b` over-delivered on a simple bug-fix
task (unrequested try/except robustness, an unrequested summary file, an unprompted `remember`
call) because nothing in the prompt said not to. Shipped: (a) `config.ProviderConfig` gained
`PromptProfile` (`prompt_profile: local|default|auto`, default `auto`) and
`LocalPromptProfile() bool`, which auto-detects from `base_url` (new `isLoopbackBaseURL` helper:
`localhost`/`127.0.0.1`/`::1`, with or without port, http/https) unless explicitly overridden.
`internal/tool/builtin/builtin.go` gained `Options.LocalProfile`: under the local profile,
`git_pr`/`web_fetch`/`web_search`/`security_scan` move from always-exposed (`reg.Register`) to
deferred (`reg.RegisterDeferred`, loaded on demand via `tool_search`) — the default profile is
unaffected. `effectiveSystem` (internal/server/helpers.go) now skips injecting the repo map when
it exceeds `localRepoMapMaxBytes` (4000) under the local profile only. (b) Two new rules were
added to the shared `toolUseBlock`/`completingTasksBlock` (internal/persona/persona.go, injected
into every session regardless of persona or profile): prefer local file tools over network tools
for file-scoped tasks, and don't write files/call `remember`/add unrequested robustness beyond
what was explicitly asked. Both new rules apply to every profile, not just local. Tests:
`TestProviderConfig_LocalPromptProfile` (14-case detection table),
`TestRegisterLocalProfileDefersNetworkAndScanTools`,
`TestToolUseBlock_preferLocalOverNetwork`/`TestCompletingTasksBlock_noScopeCreep`, and
`TestEffectiveSystem_localProfileTrimsPrompt` (oversized repo map dropped under a loopback
`base_url` but kept under a remote one; local prompt strictly shorter; both profiles still carry
the two new shared rules). Actual latency/instruction-following measurement needs the P25.7
harness — deferred, per that item's acceptance criteria.

*P25.3 — output guard vs local/thinking models.* In the live eval, a correct answer from
`qwen3.6:35b-a3b-deep` with the default `output_guard.enabled: true` + `mode: llm` tripled turn
time (26 s → 78 s): the verdict failed to parse, fail-closed forced a corrective retry that
re-ran tools, the retry's verdict failed to parse again, and the surfaced answer opened with
leaked meta-text ("**PASS.** The fix is confirmed working…") because the retry answered the
guard instead of the user. Shipped, four parts. (a) **Verdict parsing symmetry**
(internal/guard/guard.go): the old parser matched PASS only at position 0 but FAIL anywhere, so
a thinking model's reasoning preamble fail-closed nearly every *passing* verdict. `parseVerdict`
now recognizes a verdict at the reply's start OR on its last non-empty line (tolerating
markdown emphasis and a "VERDICT:" label, via `verdictAt`), after stripping `<think>` **and**
`<thinking>` blocks; FAIL-anywhere still counts as a failure, PASS mid-sentence is still never
trusted ("does not PASS the rubric"), and a genuinely ambiguous reply still fails closed — the
asymmetry was the bug, not the strictness. (b) **SmallModel routing** (new
`Server.guardModel`, internal/server/engine_build.go): guard verdict calls now run on
`provider.small_model` when set — the same preference session titles and compaction already
had — so a fast non-thinking judge makes the strict "reply exactly PASS" contract satisfiable;
falls back to the session model otherwise. (c) **Retry replaces, not appends, the visible
answer**: the engine's failed-guard-with-retry event is flagged `GuardRetrying`
(engine.Event/api.Event `guard_retrying`, threaded through `toAPIEvent`); the TUI now flushes
assistant answers via `AppendBlock` and, on a retrying guard event, rewrites the failed
answer's transcript item in place to a dim "answer withdrawn — retrying" note
(`SetItemRaw`) so the retry renders as *the* answer; and after the run settles, the engine's
new `retractGuardCorrectives` strips each failed answer + corrective prompt from the
conversation (content-keyed on `guardCorrectivePrefix`, immune to mid-run
compaction/prepare-step index drift; tool rounds a retry ran are kept), setting
`Persisted = -1` so the server's existing flush path re-saves the cleaned transcript — durable
history and later model context hold only the answer the user actually saw. The corrective
prompt itself now ends by forbidding any mention of the validation step or PASS/FAIL verdict
words, closing the leak. TUI pass events (empty reason) no longer print a stray "⚠ output
guard:" line. (d) **`--first-init` template** (internal/cli/init.go): the Ollama-flavored
global config now ships `output_guard.enabled: false` with a comment explaining the latency
economics, plus a `small_model` hint in the provider block; built-in defaults are unchanged
(`enabled: true` for configured/cloud setups), and `/guard on` still enables per session. Web
UI ignores guard events (parity no-op), so no frontend change. Tests:
`TestParseVerdictShapes` (16-case table incl. reasoning preambles, verdict-on-last-line,
negated PASS, unclosed think block), engine `TestGuardRetryReplacesVisibleAnswer` /
`TestGuardExhaustedRetractsIntermediateAttempts` (retraction + `GuardRetrying` flag +
`Persisted=-1`), the two corrective-prompt tests reworked to observe the model-visible request
via a capturing adapter (the corrective no longer survives in `conv.Messages` — that's the
feature), `TestGuardModelPrefersSmallModel`, `TestToAPIEventGuard` retry-flag round-trip, and
the template test now asserting disabled-but-configured. Acceptance beyond unit tests (guard-on
latency ≤ ~15 % vs guard-off on the seeded-bug task) needs the live harness — re-run
`research/eval-harness-drive.py` with guard on next live-eval session; the P25.7 suite locks
the "no PASS/FAIL meta-text in the final answer" invariant.

*P25.1 — per-session working directory.* A TUI session started in directory X against a daemon
started in directory Y displayed `Dir X` in the welcome screen but executed every tool in Y — in
the live eval the agent answered `git status` from the wrong repo, concluded the target file
didn't exist, web-searched, then ran `find /`; `read_file` with the session dir's absolute path
was refused (outside workspace root), pushing the model to shell `cat`/`ls` and an approval prompt
each time. Root cause: `internal/server/server.go` captured `os.Getwd()` once at daemon startup
and that single value became the tool workspace root, `s.workspace`, memory/repo-map/LSP/knowledge
roots, persona/command discovery dirs, and sandbox `ExecOpts.Dir`; sessions had no workdir of
their own, and `aegis chat` (in-process engine rooted at the caller's cwd) masked the bug by
behaving differently from the TUI against the same daemon.
Shipped: rather than a per-root `tool.Registry` cache — which would mean reconnecting MCP servers,
re-registering plugins, and rebuilding the swarm/agent tool once per distinct session directory —
the daemon keeps one shared, MCP/plugin/swarm-wired registry and threads the session's workdir
through `context.Context` (`tool.WithWorkdir`/`tool.WorkdirFromContext`, mirroring the existing
`tool.WithRegistry` pattern `tool_search` already relied on). `engine.Options.Workdir` sets it
once per turn (`executeTool`, right next to `tool.WithRegistry`); every workspace-confined tool
(file ops, `ls`/`glob`/`grep`, git, `shell`, security/diagram/latex/dast/recon tools,
`remember`/`save_skill`, background shell jobs) resolves its effective root from that context
value, falling back to its own construction-time root when unset. `sandbox.ExecOpts.Dir` was
already per-call for the local and container backends, so this reaches the shell tool with no
sandbox-package changes. `CreateSessionRequest`/`SessionMeta`/`session.Session`/`session.Meta`
gained `Workdir`; the session store persists it via the same idempotent
`ALTER TABLE ... ADD COLUMN` pattern as the P14.7 `Model` field. `handleCreateSession` resolves
and validates it (must exist, be a directory) and enforces the trust boundary: a new
`server.session_workdir_allowlist` config key (alongside `server.allow_remote`) restricts a
remote-accessible daemon to the daemon's own workspace or an explicitly allowlisted root;
loopback-only daemons (the default) accept any existing directory, matching today's trust model.
TUI (`internal/cli/root.go`) sends its cwd on create and prefers a resumed session's own
persisted `Workdir` over the local cwd; ACP (`internal/acp/agent.go`) now forwards the
`session/new` `cwd` param it was previously parsing and discarding. `aegis chat`, the web UI,
`mcp-serve`, and `parallel.go` are unchanged.
Deliberately deferred (documented gap, not a silent one): `lsp.Manager`, `knowledge.Store`,
`longmem.Store`, the cached repo-map (`s.repoMap`), and persona/command/agent-def directory
discovery all remain scoped to the daemon's own default workspace regardless of a session's
Workdir — each is a daemon-wide singleton today (one set of language servers, one knowledge DB,
etc.) and re-scoping them per session is a materially larger change nothing yet requires.
`sandbox.OSBackend` (seatbelt/bwrap) also bakes its write-confinement profile to the daemon's
workspace at construction — a session on a different Workdir under the `os` sandbox backend won't
get write access extended to its own directory; `resolveSessionWorkdir` logs a one-time warning
when this combination is detected. Revisit if a concrete pain point shows up in a future
live-eval pass. Tests: `internal/engine/workdir_test.go`, session-store persistence and
workdir-validation coverage in `internal/server`.

*P25.2 — sandbox backend name trap + untruthful `/config/sandbox`.* `sandbox.backend: podman` (or
`docker`) was accepted everywhere — config file, `AEGIS_SANDBOX_BACKEND`, `PATCH /config/sandbox`
— and `GET /config/sandbox` echoed it back, but `SelectSandbox` switched only on
`"container" | "auto" | "os"`, so anything else silently hit `default:` → local backend: execution
ran on the **host, unsandboxed**, and with `auto_approve_exec: true` (the exact combo the docs
suggest for containerized auto-runs) every shell command ran on the host unprompted. Verified
live: host-path tracebacks until the backend was respelled, `/workspace` tracebacks after.
Shipped: (a) `config.SandboxConfig.Normalize()` (internal/config/config.go), called from
`config.Load()` and reused by the `PATCH /config/sandbox` handler, aliases
`docker`/`podman`/`wsl`/`wslc`/`apple` → `backend: container` + the matching `runtime` (an
explicit `runtime` already set is preserved) and hard-errors on any other unrecognized `backend`
value naming the offending value and the correct keys; `SelectSandbox`
(internal/server/server.go) also hardened its own `default:` case as defense-in-depth for any
`SandboxConfig` built outside `config.Load()`. (b) `api.ConfigSandboxResponse` gained
`active_backend`/`fallback`/`fallback_reason`; both `/config/sandbox` handlers now report the
daemon's actual `s.sandbox.Name()` and `s.sandboxFallback(Reason)` alongside the configured
values, verified live (`AEGIS_SANDBOX_BACKEND=podman` with no podman installed correctly reports
`active_backend: "local", fallback: true` with the underlying error as the reason). (c) new
`permission.allow_unsandboxed_auto_exec` config key (default false); daemon startup now refuses
to start (`unsandboxedAutoExecError` in server.go) when `auto_approve_exec: true` and the
effective backend is local, unless the opt-out is set — verified live, including the opt-out
downgrading back to a WARN. (d) web UI: `StatusInfo`/new `ConfigSandboxResponse` TS types gained
the fallback fields; the sidebar's "Security check" button now shows a warning badge when
`/status` reports `sandbox_fallback`, and the Security panel gained a read-only "Sandbox" tab
(`SandboxSection` in SecurityPanel.tsx) showing configured vs. active backend and the fallback
reason via `GET /config/sandbox`; frontend rebuilt and `dist/` committed. Tests:
`internal/config/sandbox_normalize_test.go` (alias/validation table),
`internal/server/sandbox_test.go` (`/config/sandbox` reflects the active backend + fallback
reason), `internal/server/sandbox_startup_test.go` (auto-approve + local-sandbox startup refusal
and the opt-out).

Earlier — **P15.3–P15.10 — web UI parity with the TUI (batches A, B,
C) and P24.14 (FIND-12) — MCP outbound tool-call argument flow.** The Tier 3 pass, four ships in one
day; P15.2's config-mutation endpoints and P15.12's token hardening had already landed, so all three
web-UI batches were frontend work against existing daemon APIs (plus two small wire-shape additions
in batch C, below). P15.11's plain-language framing was the design lens throughout — every panel
speaks user language ("Stress-test a claim", "What the assistant remembers", "Accepted risks"), not
subsystem language.

*Batch A (`d8fc58e`, P15.3–P15.5, P15.10):* "Assistant" topbar chip opens a persona picker with
plain-language descriptions (GET /personas), switching via PATCH on the session, with a per-chat
model override behind an "Advanced" disclosure; persistent cost/token readout in the topbar (this
chat + today's totals, caps in the tooltip) refreshed after every run, and `cost_alert` SSE events —
previously dropped — surfaced as warning toasts; "Restore" chip lists per-turn restore points with
an inline destructive-action confirmation before POST /rewind; approval prompts gained a "Don't ask
again for requests like this" checkbox with an editable pattern (allow_always/pattern on approve),
pre-filled by a TS port of the TUI's `suggestRulePattern` (command prefix / file directory / URL
host).

*Batch B (`eb5a14c`, P15.8–P15.9):* debate ("stress-test a claim") and project-knowledge panels as
sidebar tools; archived-chats tab with archive/restore, prune-old-chats with confirmation,
background-session toggle plus reattach-to-a-running-response via the buffered-events endpoint, and
a daemon-wide activity view (runs + teammates).

*Batch C (`05ca71f` merge of `8686c42`/`bc38dd3`, P15.6–P15.7):* the last two panels, built **in
parallel by two sub-agents in isolated git worktrees** (disjoint backend files, overlapping only on
the frontend seams — `app.tsx`/`types.ts`/`SessionList.tsx`/`style.css` — resolved additively at
merge). P15.6 ("Security check"): scanner-status list from GET /security/status with the two-phase
guided-install flow preserved (POST /security/install first shows the exact host command, only an
explicit second confirm click runs it), run-a-scan with a severity-sorted findings table
(expandable description/remediation/CWE/ASVS rows, skipped-scanner reasons, suppressed count, raw
report in a collapsible), and the accepted-risk baseline as a read-only table with
active/expired/invalid badges. Its backend half: `api.ScanResponse` — previously just the formatted
text `report` — gained structured fields mirroring `security.Report` (`api.ScanFinding` mirrors
`security.Finding`, same mirror-not-import convention as `SecurityBaselineEntry`), populated for
workspace/path, image, and recon scans in `handleScan`. P15.7 ("Skills & memory"): project/user
memory as read-only views with per-scope "Add a note" composers (POST /memory), the
currently-usable playbook list, and a built-in-skills toggle list with a project/global scope
selector and an explicit dirty-tracking Save that always sends the complete set (PATCH
/config/skills is deliberately full-replace). Its backend half: `ConfigSkillsResponse` gained an
`available` catalog (name + description per embedded built-in, from `skills.Builtins()`) so the
toggle UI doesn't hardcode the skill list.
Tests: new `TestHandleScanReturnsStructuredFields` (pins trufflehog so the ran/skipped/findings
shape is deterministic regardless of installed binaries), extended
`TestConfigSkillsGetAndPatchRoundTrip` (catalog present, PATCH echoes it); `go build ./...` clean
and `go test ./...` shows only the pre-existing machine-specific failures, verified identical on
the pre-merge base commit in a throwaway worktree; frontend `tsc` + vite build clean, `dist/`
rebuilt once after the merge and committed.

*P24.14 (`73880ae`, FIND-12):* tool-call arguments are model-constructed and forwarded verbatim to
whichever MCP server the call targets, making an untrusted server an exfiltration channel for
anything the model has read into context. docs/mcp-trust-boundary.md gained an outbound section
(§3) covering the data flow, the injection→exfiltration composition, and how to evaluate configured
servers; new per-server opt-in `scan_arguments` (default false, the outbound mirror of
`scan_output`) checks tools/call, resources/read, and prompts/get arguments against a small
conservative credential-shaped pattern set (PEM keys, AWS key IDs, sk-/GitHub/Slack tokens, JWTs,
bearer tokens, api_key/password assignments) in `internal/mcp/outbound.go`. A hit logs a Warn
naming the server, tool, and pattern class — never the matched text — and is flag-only, never
blocking or mutating the call, matching the inbound scan philosophy. Table-driven tests cover
pattern coverage, off-by-default no-op, warn-and-proceed, and the resource/prompt adapters.

Earlier — **P24.20 (FIND-17) — strip/escape ANSI/OSC control sequences in
streamed model output before TUI render.** Flagged by the STRIDE-A threat model as defense-in-depth
for an already-mitigated prompt-injection vector: if adversarial content ever reached the model's
output verbatim, the TUI's markdown render path had no sanitization step, so embedded raw ANSI/OSC
escape sequences could reach the terminal — cursor repositioning, hidden/overwritten text, or
OSC-based clipboard/title-bar tricks on terminals that support them. `internal/tui/tui.go`'s
`mdRender` (the single choke point both the mid-stream `liveBlock.render` and end-of-turn
`flushLiveText` route through) renders raw model text via glamour or, on renderer failure, a plain
`wrap` fallback — neither strips unrelated escape sequences embedded in the source text. Added
`stripControlSeqs` in a new `internal/tui/sanitize.go`, a byte-scanner that strips CSI sequences
(`ESC [ … <final byte>`), OSC/DCS/APC/PM sequences (`ESC ] … (BEL | ESC \)` and similar,
BEL/ST-terminated), bare/other 7-bit C1 two-byte escape forms, and raw C0 control bytes (except
tab/LF/CR) and DEL, while leaving printable text — including multi-byte UTF-8 — untouched; called at
the top of `mdRender` before both the glamour and fallback paths, so the sanitization happens on the
untrusted input rather than only on glamour's own (trusted) styled output. Deliberately separate from
`internal/tui/ansi16.go`'s `remapANSI16`, which remaps SGR colour codes in already-trusted shell-tool
output and preserves them by design (`internal/tui/toolview.go`'s tool-result rendering path) — that
path was left untouched since it isn't the vector the finding describes.
Tests: new `internal/tui/sanitize_test.go` (`TestStripControlSeqs`, table-driven — cursor-
repositioning CSI, SGR color CSI, OSC terminal-title and OSC-8 hyperlink/OSC-52 clipboard sequences,
bare ESC, C0 controls, DEL, an unterminated trailing OSC, and markdown/unicode passthrough — plus
`TestStripControlSeqsIdempotent`). `go build ./...`, `go test ./internal/tui/...`, and
`go test -race ./internal/tui/...` all clean; `go test ./...` shows only pre-existing, unrelated
failures (Windows-path-format assertions in `internal/sandbox`/`internal/security`/`internal/lsp` and
network-timeout flakes in `internal/server`) that reproduce identically on `main` before this change.

Earlier — **P24.22 — quote/escape the `distro` argument in
`sandbox.WSLInstallCommand`.** Flagged by the STRIDE-A threat model as a latent injection vector:
`WSLInstallCommand` (`internal/sandbox/wsl.go`) already quoted its `linuxCmd` argument with
`bashQuote` (correct, since that portion is embedded inside the inner `bash -lc '...'`), but
concatenated `distro` directly into the command line unquoted — `"wsl -d " + distro + " -- ..."`.
Currently dead code from a security-impact standpoint (the only call site,
`internal/security/install.go:42`, hardcodes `distro` to `""`), but worth closing before a second,
config-driven caller turns it into a real injection vector. The whole returned string is parsed by
PowerShell first (`shellInvocation` runs it via `<pwsh-or-powershell> -NoProfile -NonInteractive
-Command <command>`, which then invokes `wsl.exe -d <distro> -- bash -lc '...'`), so `distro` needed
PowerShell single-quote escaping, not bash quoting — doubling any embedded single quote, no
backslash escape. Added `powershellQuote` next to the existing `bashQuote` in
`internal/sandbox/wsl.go` and applied it to the `distro` argument only; `linuxCmd`/`bashQuote` is
unchanged.
Tests: `internal/sandbox/wsl_test.go`'s `TestWSLInstallCommandWithDistro` updated to expect the
now-quoted `'kali-linux'`, plus a new `TestWSLInstallCommandQuotesDangerousDistro` asserting a
distro name containing a single quote and a semicolon (`kali'; rm -rf C:\ ; '`) is safely escaped
rather than breaking out of the PowerShell argument. `go build ./...` and
`go test ./internal/sandbox/...` clean (pre-existing, unrelated `-race` failures on this machine —
macOS `/private/var` symlink resolution in `TestValidatePath*` and a flaky
`TestWSLPathConvertsBackslashesViaForwardSlash` — reproduce identically on `main` before this
change).

Earlier — **P24.15 (FIND-14) — give each swarm sub-agent a guaranteed
minimum budget floor.** `internal/swarm/subprocess.go`'s `SubprocessBackend.Spawn` computed each
worker's remaining cost/token allowance as the shared fan-out tracker's cap minus whatever siblings
had already spent, floored only at a near-zero epsilon (`minRemainingBudgetUSD`/
`minRemainingTokens`) once exhausted — so one expensive or runaway early sub-agent could reduce a
later sibling's allowance to essentially nothing, even though the swarm run wasn't done spawning
its intended workers (STRIDE-A: Denial of Service, CVSS 3.6). Added a `fairShareFraction` (0.2)
constant: once the shared cap is exhausted, `remainingBudget`/`remainingTokens` now floor a
worker's allowance at 20% of the *original* cap rather than the epsilon, so a handful of siblings
can each still get a meaningful floor; the epsilon floor is kept as a fallback for the degenerate
case where 20% of the cap itself rounds to (near) zero. `SpawnConfig` carries no team-size/worker-
count hint, so this is a fixed conservative fraction rather than an exact 1/N split — worst case
total spend across floors alone is bounded at 5x the original cap, an accepted trade against the
fairness gap it closes.
Tests: `internal/swarm/subprocess_test.go` gained `TestRemainingBudgetFairShareFloor`,
`TestRemainingBudgetFairShareFloorFallsBackToEpsilon`, `TestRemainingTokensFairShareFloor`, and an
end-to-end `TestSubprocessSpawnLaterSiblingGetsFairShareFloor` (spawns a worker after a shared
tracker shows the cap already blown past, asserts the reported remaining budget/tokens land at the
fair-share floor instead of the old near-zero epsilon). `go build ./...`, `go test ./internal/swarm/...`,
and `go test -race ./internal/swarm/...` all clean.

Earlier — **P24.19 (FIND-15) — document that local-Ollama traffic is
typically unencrypted.** `internal/provider/openai/openai.go` applies no TLS enforcement specific
to a local base URL, and Ollama's own default configuration binds and serves over plain HTTP on
`127.0.0.1` — on a single-user machine this is no different from any other loopback traffic, but on
a **shared multi-user host** another local account could observe or tamper with daemon↔Ollama
traffic. Not independently actionable in Aegis's own code, since the plaintext behavior originates
from Ollama's own default configuration — remediation is documentation only. `docs/providers.md`'s
"Ollama (recommended for local use)" section gained a "Shared-host note" covering the exposure and
recommending TLS (where Ollama supports it) or a single-user host for sensitive work.

Earlier — **P24.16, P24.17, and P24.18 — the STRIDE-A threat model's Tier 3
third batch, closing out Tier 3 entirely — shipped in parallel via isolated git-worktree
sub-agents** (see [roadmap.md](roadmap.md#priority-order)):

**P24.16 (FIND-29) — extend Windows DACL hardening beyond `daemon.token`.** `daemon.token` got an
explicit, non-inherited, owner-only Windows DACL via a `restrictToOwner` helper
(`internal/server/token_windows.go`/`token_other.go`), but the SQLite session database and
`.aegis/.env` inherited whatever ACL the data/project directory already had — on a shared Windows
host, another local account with read access to that directory could read conversation history or
`.env` secrets, neither of which are encrypted at rest. Extracted the SDDL-based logic
(`"D:PAI(A;;FA;;;OW)"`, same idiom WireGuard for Windows uses) into a new leaf package,
`internal/fsguard` (`RestrictToOwner`, same windows/other build-tag split as before), so
`internal/session` and `internal/config` can call it without creating an import cycle through
`internal/server`; the old server-local `token_windows.go`/`token_other.go` were deleted and
`auth.go`'s `generateAndWriteToken` now calls the shared function. `session.Open`
(`internal/session/session.go`) hardens `sessions.db` and its WAL-mode `-wal`/`-shm` sidecar files
right after `migrate()` succeeds — checkpoint snapshots needed no separate treatment since
`internal/checkpoint` shares the same SQLite connection via `NewStore(db *sql.DB)`. A hardening
failure on the main database file propagates as an `Open` error, matching how `daemon.token` has
always treated a genuine ACL-set failure; a sidecar failure (including "doesn't exist yet", which
`fsguard.RestrictToOwner` treats as a no-op on any file) is only logged, since the sidecars may not
have been created yet at open time and the primary db file being locked down already covers the
bulk of the exposure. `config.loadDotEnv` (`internal/config/config.go`) applies the same hardening
to `.aegis/.env` right after a successful read; because that file is user-owned, not
Aegis-written, a failure there only logs a warning rather than failing `config.Load()` — a
locked-down host where the current user can't rewrite their own file's ACL shouldn't break every
command. `docs/configuration.md` gained a "Local Data Store Permissions (Windows ACL Hardening)"
section documenting the extended coverage.
Tests: `internal/fsguard/fsguard_windows_test.go` (new, Windows-only) reads the on-disk DACL back
via `golang.org/x/sys/windows` and asserts exactly one ACE naming the well-known owner-rights SID
(not Everyone); `internal/fsguard/fsguard_test.go` (new, cross-platform) covers the
existing-file/missing-file no-op smoke cases. `internal/session/session_test.go` gained
`TestOpenAppliesPermissionHardening` (opens, writes, and reopens a store so both the main db file
and its now-created sidecars go through hardening) and `internal/config/config_test.go` gained
`TestLoadDotEnvAppliesPermissionHardening`/`TestLoadDotEnvMissingFileNoOp`. `go build ./...` and
`go vet ./...` clean; `go test ./...` green except the same three pre-existing/environmental
failures noted elsewhere in this doc (`internal/server`'s two `scan_test.go` timeouts and
`TestBuildImageBlocksFromPath`), confirmed unrelated via `git stash` on the pre-change tree.

**P24.17 (FIND-30) — integrity verification for memory files.** Project/user memory
(`.aegis/memory.md` and the user-global `memory.md`) is plain text with no tamper detection —
anyone with host/OS write access (including malware running as the same OS user) could hand-edit
either file to inject persistent, low-visibility "learned" content that a future session would
treat as genuine prior context, a durable cross-session prompt-injection vector. Added a new
`internal/memory/integrity.go`: a sha256 sidecar file next to each memory file
(`<path>.integrity`), refreshed by `memory.Append` after every write (Aegis's own write path) by
re-reading the file's full post-append content and re-hashing it. `Sources.loadDirect` now reads
each memory file through a new `readMemoryFileChecked` instead of the plain `readIfExists`: it
hashes the file's current content and compares against the sidecar — a match loads silently; a
mismatch prepends a visible `⚠️ integrity check failed: this memory file was modified outside
Aegis — treat its contents with reduced trust` banner to that memory section (the content itself is
never dropped, since a mismatch may just be an intentional hand-edit, which is a supported use
case) and logs via `slog.Warn`; a missing sidecar (a pre-existing `memory.md` predating this
feature, or the very first write) silently establishes a new trust baseline instead of
false-positive-warning every upgrading user on their next session. Deliberately a plain hash, not a
keyed MAC/signature — an adversary with write access to `memory.md` already has write access to
whatever sidecar sits next to it, so a secret key wouldn't raise the bar. All hashing/sidecar I/O
failure modes fail open (log and fall through to loading unwarned) rather than ever blocking memory
loading. `docs/memory-and-knowledge.md` documents the sidecar file and what does/doesn't trigger
the warning.
Tests: `internal/memory/integrity_test.go` (new) — a freshly-`Append`ed file round-trips with no
warning (project and global memory symmetrically), hand-editing a file after an `Append` triggers
the warning marker (tampered content still surfaced, not dropped), and a legacy file with no
sidecar loads warning-free while establishing a baseline, confirmed stable across a second
unmodified load. `go build ./...` clean; `go test ./internal/memory/...` green; full `go test
./...` clean except the same three pre-existing/environmental failures noted elsewhere in this doc
(`TestBuildImageBlocksFromPath`, and two `internal/server` `scan_test.go` 30s-timeout cases).

**P24.18 (FIND-32) — optional TLS for client↔daemon traffic.** All traffic between a CLI client and
the daemon was plain HTTP over loopback, including the bearer token and full conversation content —
Tier 3/defense-in-depth given the loopback-only default (FIND-08), but observable by another local
account on a shared host with packet-capture privilege. Chose optional TLS over a Unix-domain-
socket/named-pipe transport, since TLS is one code path across the Windows/macOS/Linux targets this
project supports where a UDS/named-pipe split would need two — and this box (Windows) made the
cross-platform cost of the split concrete rather than theoretical. New opt-in
`server.tls.enabled` config (`internal/config/config.go`'s new `ServerTLSConfig`, default false —
byte-for-byte unchanged behavior when unset) plus optional `cert_file`/`key_file` for an operator-
supplied certificate. When enabled with no cert/key configured, `internal/server/tls.go`'s new
`ensureTLSCert` generates a self-signed ECDSA P-256 certificate on first start and persists it as
`<data_dir>/daemon.crt`/`daemon.key` — the same generate-once-reuse-unless-missing convention
`generateAndWriteToken` uses for `daemon.token` — and the private key gets the same Windows DACL
hardening as the auth token via `fsguard.RestrictToOwner`, the shared leaf package P24.16 (FIND-29,
above) extracted for exactly this kind of cross-package reuse; `tls.go` originally called a
server-package-local `restrictToOwner` (the only copy that existed in this item's own worktree,
built independently and in parallel), and was updated to the shared package while reconciling the
three P24.16/P24.17/P24.18 worktrees onto `main`. `internal/server/server.go`'s `ListenAndServe` calls
`ListenAndServeTLS("", "")` with the loaded certificate already set on `http.Server.TLSConfig` when
TLS is enabled, `ListenAndServe()` unchanged otherwise. Client side: new `client.WithTLS(certPath)`
(`internal/client/client.go`) pins the daemon's certificate into a dedicated `*x509.CertPool` —
`InsecureSkipVerify` is never used, so an unrecognized certificate fails the TLS handshake closed
rather than silently connecting — and a new `client.NewFromConfig(cfg)` convenience constructor
centralizes the base-URL/bearer-token/TLS wiring in one place (confirmed no import cycle: `internal/
config` imports nothing under `internal/`). All ~9 `client.New(cfg.Server.Addr).WithTokenFile(...)`
call sites across `internal/cli/{root,acp,mcpserve,sessions,ui}.go` now go through it instead of
repeating the wiring. `aegis ui`'s printed URL (`webUIURL`) switches to `https://` when TLS is
enabled and the command prints a one-line "browser will warn about the self-signed certificate —
this is expected" notice, since a browser (unlike the pinned CLI clients) has no way to trust the
self-signed cert automatically.
Tests: `internal/server/tls_test.go` (new) — `TestListenAndServeTLSRoundTrip` starts a real
`ListenAndServe` with TLS enabled on an ephemeral loopback port, confirms the cert/key files are
written, a client pinned via `WithTLS` reaches `/healthz` successfully, and an unpinned
`https://` client fails closed against the self-signed cert; `TestListenAndServeTLSDisabledUnchanged`
confirms no cert/key files are written and plain HTTP still works when TLS is left off (the
default). `go build ./...` clean; `go test ./...` green except the same three pre-existing/
environmental failures noted elsewhere in this doc (`TestBuildImageBlocksFromPath`,
`TestHandleScanDefaultsToWholeWorkspace`, `TestHandleScanImageRoutesToImageScan`), confirmed
unrelated via `git stash` on the pre-change tree. Docs: `docs/configuration.md` (`server.tls.*` full
reference plus an `AEGIS_SERVER_TLS_ENABLED` env-var row) and `docs/security_scan.md` (new
"Client<->Daemon Transport" section covering the threat model, what TLS does and does not protect
against, and its off-by-default posture).

Earlier — **P24.11, P24.12, and P24.13 — the STRIDE-A threat model's Tier 3
second batch — shipped in parallel via isolated git-worktree sub-agents** (see
[roadmap.md](roadmap.md#priority-order)):
**P24.11 (FIND-07) — allowlist/trust-gate LSP server commands.** `internal/lsp/client.go`'s
`NewClient` passed a project/user-config-supplied `command`/`args` straight to
`exec.CommandContext` with no allowlist or verification — a malicious project `.aegis/config.yaml`
could point the LSP client at an arbitrary binary for code execution the first time LSP integration
activated. All configured LSP servers start eagerly and synchronously at daemon construction time
(`internal/server/server.go`, inside `server.New`), before any TUI/session/interactive approver
exists, which ruled out a live TOFU-confirmation prompt — there's no human present at the point
that matters. Added a built-in allowlist of common LSP server binary basenames
(`internal/lsp/trust.go`, matched case-insensitively against just the basename, not the full path)
plus an explicit per-server `lsp[].trust: true` config opt-in for anything else; `Manager.Start`
now calls a new pure `checkTrusted` before spawning and refuses (with an actionable error naming
the config knob) instead of exec'ing an unrecognized, non-trusted command.
Tests: `internal/lsp/trust_test.go` (new) — allowlisted basename, allowlisted-via-full-path
(including Windows-style), non-allowlisted refused, non-allowlisted allowed with `trust: true`,
case-insensitivity. `go build ./...` clean; `go test ./internal/lsp/... ./internal/config/...
./internal/server/...` green except the same three pre-existing/environmental `internal/server`
failures noted elsewhere in this doc.
**P24.12 (FIND-09) — opt-in secret redaction pass for tool-read content.** Full conversation and
tool-read file content streams to whichever provider is configured with no content-filtering step
— for a cloud provider (Anthropic, OpenAI, any OpenAI-compatible cloud endpoint), a secret embedded
in a file a tool reads goes to that third party unmasked. Added a new `security.RedactText`
(`internal/security/redact.go`), extending the FIND-13 gitleaks-backed detection machinery
(`ScanText`) to also capture the literal matched secret string from gitleaks' JSON report and mask
each occurrence to `[REDACTED:<RuleID>]` — same fail-open posture as `ScanText` (no gitleaks on
PATH, or any scan error, leaves the text unchanged rather than blocking). New opt-in
`security.redact_secrets` config flag (off by default); when set, `engine.executeTool`
(`internal/engine/engine.go`) runs every successful read-capability tool result through it before
the result re-enters the model's context, logging a count at Info rather than ever blocking the
call. `docs/providers.md` gained a "Data Exposure & Redaction" section documenting local-Ollama as
the no-exposure alternative for sensitive codebases.
Tests: `internal/security/redact_test.go` (new, no-gitleaks-on-PATH + live AWS-key-pattern cases,
both exercised for real on this box) and `internal/engine/redact_test.go` (new, stubs the
`redactSecretsFn` seam so it runs unconditionally in CI without a gitleaks dependency). `go build
./...` clean; `go test ./internal/security/... ./internal/engine/... ./internal/server/...` green
except the same three pre-existing failures.
**P24.13 (FIND-10) — detect zero-width/base64-obfuscated injection attempts.** The shared
opt-in MCP/web-fetch prompt-injection heuristic (`trust.ScanForInjection`, ~14 plain regexes) was
trivially bypassed by encoding a payload or inserting zero-width/invisible Unicode characters
inside a trigger word. `ScanForInjection` now additionally matches the same regex set against (a) a
copy of the content with Unicode `Cf` (zero-width/invisible format) characters stripped, and (b)
the decoded text of any base64-looking substring (20+ contiguous base64-alphabet characters) that
decodes to valid UTF-8 — hits inside decoded content are labeled distinctly so the surfaced warning
makes clear the match was inside an encoded payload. The original content handed to `trust.Wrap` is
never altered, only throwaway matching copies are. `docs/mcp-trust-boundary.md`'s "What this does
not do" bypass list was updated to reflect the new boundary (homoglyphs, translation, other
encodings, and multi-call-split payloads still aren't caught), and a new "Evaluating a model-based
classifier" section documents why a model-based classifier is deferred rather than built now — it
would add a real network/latency/cost dependency and a new attackable trust surface, with no
evidence yet that the heuristic is inadequate for its defense-in-depth role — with a concrete
revisit trigger (an opt-in `scan_output: model` mode if false-negative reports accumulate).
Tests: `internal/trust/trust_test.go` gained zero-width-obfuscation, base64-encoded-payload, and
benign-base64ish-no-false-positive cases alongside the existing pattern tests. `go build ./...`,
`gofmt`, `go vet` clean; `go test ./internal/trust/... ./internal/mcp/... ./internal/tool/...`
green.
All three merged independently (`go build ./...` and the full `go test ./...` re-verified clean
after each merge and after all three landed together — same three pre-existing/environmental
`internal/server` failures, confirmed unrelated via `git stash` on the pre-change tree by two of
the three sub-agents independently).
Earlier — **P15.2, the daemon config-mutation endpoints, shipped via an
isolated git-worktree sub-agent — closing out Tier 3's first batch (alongside P21.2 and P24.10,
below), 2026-07-10** (see [roadmap.md](roadmap.md#priority-order)):
**P15.2 — new daemon config-mutation endpoints.** The web UI's planned sandbox/security/skills
config panels and security-tooling admin panel (P15.6/P15.7) had no HTTP surface to talk to —
every config mutation (sandbox backend, security scanner policy, `skills.builtin_enabled`, the
hardened profile, guided scanner installs) was CLI/TUI-only. Added seven endpoints:
`GET/PATCH /config/sandbox`, `/config/security`, `/config/skills`, `POST /config/harden`,
`GET /security/status`, `GET /security/baseline`, `POST /security/install`
(`internal/server/config.go`, `internal/server/security_admin.go`). All PATCH handlers write
through the existing `config.Patch{Global,Project}*` functions rather than hand-rolling YAML
mutation; the sandbox/security PATCH bodies use pointer fields for genuine partial-update semantics
since the underlying patches otherwise replace their whole config block. `aegis harden`'s
cap-computation was extracted from `internal/cli/harden.go` into `config.ComputeHardenPlan`
(`internal/config/harden.go`) so the CLI and the new `POST /config/harden` share one source of
truth instead of duplicating the cap thresholds and "leave an already-hardened knob alone"
exceptions; harden and install both require an explicit `{"confirm": true}` before writing/running
anything, since there's no terminal to show the CLI's `[y/N]` prompt. `GET /security/status` mirrors
`internal/tui/securityconfig.go`'s tool-probe/status wording exactly so a future web panel matches
the TUI. New wire types in `internal/api/api.go` plus typed `internal/client/client.go` methods
follow the existing `Scan`/`Knowledge`/`Debate` client idiom. Scope selection (project vs. global
config) defaults to project when the daemon's workspace has a `.aegis/` directory, else global,
overridable per-request — flagged as a judgment call worth a second look if project/global scoping
ever needs to be more explicit.
Tests: `internal/server/config_test.go` (new) — GET defaults, PATCH apply+persist+partial-update,
project/global scope resolution including auto-detection, unknown-scope rejection, harden
preview/apply/idempotency; environment-tolerant smoke tests for install/status/baseline (no scanner
binary required, matching `scan_test.go`'s existing convention on this box). `go build ./...`,
`go vet ./...` clean; full `internal/server`/`config`/`security`/`cli`/`api`/`client` suites green
except the same three pre-existing/environmental failures noted elsewhere in this doc (confirmed
via `git stash` to fail identically without this change).
Earlier the same day — **P21.2, tool-call cards, shipped via an isolated git-worktree
sub-agent** (see [roadmap.md](roadmap.md#priority-order)):
**P21.2 — tool-call cards (in-place updating block).** A tool call used to render as two
independent, static transcript items — `renderToolCall` appended at `KindToolCall`,
`renderToolResult` appended separately at `KindToolResult` with no link back to the call — so every
call looked "finished" the instant it started, and concurrent tool calls (`engine.runTools` runs
read/network tools concurrently, and results don't necessarily land in call order) relied on a
same-name FIFO queue that could silently cross-attribute a result, or a `read_file`'s highlighted
path, to the wrong call. The real fix needed a stable per-call identity that didn't exist on the
wire: added `ToolID` (the provider's `tool_use` ID) to `engine.Event`/`api.Event`, populated at
every emission site including the panic-recovery path and threaded through
`messages.go`'s `toAPIEvent`. The TUI (`internal/tui/tui.go`) now `AppendBlock`s a pending card at
`KindToolCall`, keyed by `ToolID` in a `pendingTools` map, and `SetItemRaw`s it to ok/err in place
at `KindToolResult` instead of appending a second item; `pendingReadPaths` moved off the same
same-name-FIFO pattern onto the same keyed map, fixing the same latent cross-talk bug for
concurrent reads. `resolveStuckToolCards` finalizes any still-pending cards to an "interrupted"
state from `KindError` and from `streamClosedMsg` (the only signal guaranteed to fire on every run
end, since a client-initiated cancel hits neither `KindError` nor `KindDone`). New
`renderToolCardPending`/`-Done`/`-Stuck` in `internal/tui/toolview.go` wrap the existing
call/diff-preview renderers unchanged, reusing the existing `shimmerText` animation primitive for
the pending state rather than inventing a new one. Session replay (`loadHistory`) was deliberately
left rendering call+result as two static items — both halves are already known at replay time, so
combining them would be cosmetic, not a fix.
Tests: `internal/tui/toolcard_test.go` (new) — pending→ok, pending→err, two concurrent calls
resolving independently out of order with no cross-talk, a turn-error resolving a stuck card,
`streamClosedMsg` resolving a stuck card, and the ID-less FIFO fallback. `go build ./...`,
`go vet ./...` clean; `go test ./internal/tui/... ./internal/engine/... ./internal/api/...` green.
No interactive PTY was available to visually verify the pending→ok/err transition in a real
terminal — noted explicitly rather than claimed.
Earlier the same day — **P24.10, the first of the STRIDE-A threat model's Tier 3 findings,
shipped via an isolated git-worktree sub-agent** (see [roadmap.md](roadmap.md#priority-order)):
**P24.10 (FIND-06) — document Docker/Podman-socket privilege equivalence, recommend rootless
backends.** FIND-06 flagged that Docker/Podman socket access is privilege-equivalent to local root
(Docker) or the invoking user (rootful Podman), and that `internal/sandbox/docker.go` showed no
capability-dropping. Re-verified against current code first: `ociRunArgs` already applies
`--cap-drop=ALL` and `--security-opt=no-new-privileges` unconditionally to every docker/podman run,
so that half of the finding was already shipped — this change didn't touch it. What remained open
was the doc gap (the inherent socket-level privilege equivalence, which no container-run flag can
close) and rootless-backend guidance. Added a "Docker/Podman socket privilege equivalence"
subsection to `docs/security_scan.md` and new `sandbox.SocketRuntime`/`SocketPrivilegeNotice`
helpers, logged once via `Server.SelectSandbox` when a docker/podman backend is selected — no
automatic rootless-vs-rootful detection, since no reliable cross-platform client-side signal exists
without a fragile `docker/podman info` parse (documented as a deliberate scope decision, not an
oversight).
Tests: `internal/sandbox/docker_test.go` (new `TestSocketRuntime`/`TestSocketPrivilegeNotice`);
`go build ./...`, `go vet ./...` clean, `go test ./internal/sandbox/... ./internal/cli/...` and the
`internal/server` `TestSelectSandbox*` suite green.
Earlier the same day — **P24.5–P24.9, the STRIDE-A threat model's Tier 2 quick wins, all
shipped in parallel via isolated git-worktree sub-agents** (see
[roadmap.md](roadmap.md#priority-order)):
**P24.5 (FIND-11) — count and log repeated invalid-bearer-token attempts.** `authMiddleware`
(`internal/server/auth.go`) previously rejected a request with a missing/wrong `Authorization:
Bearer` header with a 401 and nothing else — no signal that the daemon was being probed. Added a
process-wide `atomic.Uint64` counter on `Server` (deliberately not a per-IP map, so the audit fix
itself can't become a memory-growth DoS vector) and a `slog.Warn` on the first failure and every
5th thereafter, logging remote address, path, and cumulative count — never the attempted token.
**P24.6 (FIND-13) — scan PR titles/bodies for secrets before `gh pr create`.** `git_pr`
(`internal/tool/builtin/gitpr.go`) previously sent the model-composed title/body straight to GitHub
with no inspection. `internal/security` gained an exported `ScanText` (factored out of the existing
`gitleaksScanner`'s host-scan path) that writes text to a temp file and runs gitleaks against it —
silently a no-op if the binary isn't on PATH, never a hard dependency. `git_pr` now calls it before
pushing or creating the PR and refuses (naming the rule/location) if it finds anything; a scan
error itself fails open, matching how the rest of the security tooling treats gitleaks as
best-effort. **P24.7 (FIND-16) — distinguish `OutputGuard` fail-open from a genuine pass.**
`guard.Func` previously returned a bare `(ok bool, reason string)` — a genuine PASS and a swallowed
transport error were byte-for-byte identical, and the engine emitted nothing at all on success
either way. Added `guard.Status` (`passed`/`failed`/`skipped_transport_error`) as a third return
value; the engine (`internal/engine/engine.go`) now emits a `KindGuard` event with that status on
every path, including the previously-silent success path. **P24.8 (FIND-31) — audit
`internal/security/install.go`'s installer-script argument construction.** Verification-only:
traced every `Install` map entry (`internal/security/method.go`, all compile-time literals),
`shellInvocation`, and `exec.CommandContext(shell, args...)` — confirmed the install command always
reaches the shell as a single, unmodified argv element, never re-split or built from
runtime/config-controlled data. Locked in with three regression tests; found one latent,
currently-unreachable issue (unquoted `distro` arg in `sandbox.WSLInstallCommand`, dead code today
since `install.go`'s only call site hardcodes `""`) tracked as new roadmap item P24.22. **P24.9
(FIND-34) — dedicated cron-execution audit log.** Cron firings were only visible via transient
`slog` lines and the generic task-manager view. Added a `cron_runs` table (job ID, fired-at,
status — `ok`/`error`/`blocked`, truncated combined output) to `cron.Store`, wired through a new
`newCronRunFunc` (extracted from the inline closure in `Server.New`) that records every fire
attempt including ones the P24.3 permission gate blocks, and a new read-only `cron_history` tool
mirroring `cron_list`'s shape.
Tests: `internal/server/server_test.go` (new `TestServerInvalidAuthAttemptsLoggedAndCounted`),
`internal/security/scantext_test.go` (new), `internal/tool/builtin/gitpr_test.go`,
`internal/guard/guard_test.go`, `internal/engine/guard_test.go`, `internal/security/install_test.go`
(new regression tests), `internal/cron/cron_test.go`, `internal/tool/builtin/cron_test.go`,
`internal/server/cron_test.go` (new). `go build ./...`, `go vet ./...` clean; `go test ./...` green
except the same three pre-existing/environmental failures noted below (confirmed present before any
of this work).
Earlier the same day — **P24.1–P24.4, the STRIDE-A threat model's Critical/Important
findings (Tier 1), all shipped same day as the pass that produced them**:
**P24.1 (FIND-01) — bind the `/ui` page-token exchange to the browser that loaded the page.**
Previously `GET /ui`'s minted page token and `POST /auth/exchange` had no check on *who* was
asking — any local process reaching the loopback port, not just the operator's own browser, could
mint and redeem a page token for the real daemon bearer token, collapsing the whole auth model to
"can this process reach 127.0.0.1." Added a double-submit CSRF nonce (`internal/server/auth.go`):
`mintPageToken` now also generates a nonce, set both as an `HttpOnly`/`SameSite=Strict` cookie
(`aegis_ui_csrf`) and baked into the served HTML (`data-csrf-token`); `handleAuthExchange` requires
the cookie and an explicit `X-Aegis-CSRF` header (which only same-origin JS reading the page's own
DOM could construct) to match the nonce bound to the presented page token. This closes the
realistic instance of the gap — a hostile cross-origin webpage/tab driving the flow blind, which
can't read an `HttpOnly` cookie or this page's response body (no CORS grant, `X-Frame-Options:
DENY` blocks framing) — while a raw local process with direct HTTP access remains an accepted
residual risk, the same class as reading `daemon.token` off disk for a same-OS-user adversary.
Frontend (`internal/server/webui/frontend/src/api.ts`) sends the header; `dist/` rebuilt via
`npm run build` and committed. **P24.2 (FIND-02) — authenticate `aegis mcp-serve` and the ACP
server.** Both accepted commands from any local process able to write to the subprocess's stdin
with no credential check. `aegis acp` now implements ACP's real `authenticate` method for real: set
`AEGIS_ACP_TOKEN` in the editor's launch environment and `initialize` advertises a `shared_secret`
auth method; `session/new`/`session/prompt` are denied until the client authenticates with a
matching token (`internal/acp/agent.go`, constant-time compare). `aegis mcp-serve` gets an
equivalent, MCP-spec-external `aegis/authenticate` request gating `tools/call` the same way, opt-in
via `AEGIS_MCP_TOKEN` (`internal/mcpserver/server.go`). Both default to today's no-auth behavior
when the env var is unset — zero breaking change for every existing integration. **P24.3 (FIND-03)
— gate cron firings through the daemon's permission mode.** Scheduled jobs previously ran
unattended shell commands via `cronShellRunner` with no gate of any kind, regardless of permission
mode. `internal/cron.Job` gained an `AutoApprove` field (persisted, migrated via `ALTER TABLE ...
ADD COLUMN`); the fire-time closure in `internal/server/server.go` now evaluates
`permission.Policy{Mode: currentMode}.Decide(tool.CapExecute)` fresh on every tick — plan mode
blocks the job outright, build mode requires the job's explicit `auto_approve` opt-in (mirroring
`mcp_server.auto_approve`) since no one is present to answer an approval prompt, auto mode is
unchanged. `cron_create`'s new `auto_approve` argument and `cron_list`'s `[auto_approve]` marker
make the opt-in visible to the model. **P24.4 (FIND-05) — wrap persona/skill file bodies as
untrusted content.** Project/user `.aegis/personas/*.md` and `.aegis/skills/*.md` files are
arbitrary content from disk — a compromised dependency or cloned project could plant one to inject
instructions into every session that loads it — and were spliced into the system prompt verbatim.
`parsePersonaFile` (`internal/persona/load.go`) and `appendFromDir` (`internal/skills/skills.go`)
now wrap a file-loaded persona's `System` prompt / a project-or-user skill's body in the same
`internal/trust.Wrap` provenance marker used for MCP/web output (FIND-04/P21.6) before it can reach
the model — built-in personas/skills (compiled into the binary) are left unwrapped since they
aren't attacker-reachable. Unlike MCP/web wrapping, the heuristic injection scan is left off here
(`scan=false`): this content re-injects every session, and persona/skill prose routinely discusses
its own instructions/role, which the scan's patterns (e.g. `\bsystem prompt\b`) flag as false
positives on entirely benign text — caught by `TestPersonaNewThenShowRoundTrip` tripping on the
persona scaffold's own boilerplate. `docs/mcp-trust-boundary.md` extended to cover this.
Tests: `internal/server/webui_test.go` (new `TestAuthExchangeRejectsMismatchedCSRF`),
`internal/mcpserver/server_test.go`, `internal/acp/agent_test.go`, `internal/cron/cron_test.go`,
`internal/persona/load_test.go`, `internal/skills/skills_test.go`. `go build ./...`, `go vet ./...`
clean; `go test ./...` green except the same three pre-existing/environmental failures noted below
(confirmed present before any of this work).
Earlier the same day — **Tier 2 high-visibility wins shipped**, both in parallel via
isolated git-worktree sub-agents:
**P21.3 — streaming caret.** A blinking write-head caret (`█`) at the end of live-streaming
assistant text, so a long reply reads as "alive" rather than "redrawing." Rendered in
`refresh()` (`internal/tui/tui.go`): the caret is appended directly after the last rendered
character of the live tail — trimming and restoring glamour's trailing newline so it lands at
the true write-head rather than on its own blank line — and blinks on the pre-existing
`animStep` tick that already drives the "thinking" shimmer, so no new ticker was introduced.
Only shown while streaming with non-empty live text; never baked into the persisted transcript.
**P22.3 — Esc-Esc backtrack + `/fork`.** A new `POST /sessions/{id}/fork` endpoint
(`internal/server/sessions.go`) creates a new session copying the source session's
system/mode/persona and messages — optionally truncated to a checkpoint's cut point, the same
`Seq` boundary `/rewind`'s conversation scope uses — without mutating the source. `/fork [n]`
mirrors `/rewind`'s checkpoint numbering (no arg forks the current end of the conversation as a
sandbox branch point); pressing Esc twice while idle with an empty input box (mirroring the
existing streaming double-tap-to-cancel detection) opens a picker
(`internal/tui/backtrackpicker.go`) of prior user turns, forks at the selected turn, switches
into the new session (reusing the Ctrl+Y session-switch path), and pre-fills the input box with
the original message text for editing before resending.
**P21.5 — daemon resource ceilings.** `sessionSems` capped runs to one-per-session but had no
global cap on total concurrent runs and no bound on SSE buffer growth — a live gap now that
`aegis mcp-serve` exposes sessions to external MCP clients, not just a theoretical one. Added a
non-blocking global run semaphore (`Server.runSem`, `internal/server/messages.go`) that rejects a
request beyond `server.max_concurrent_runs` with an immediate 429 instead of queuing it; an
optional per-run wall-clock ceiling (`server.max_run_duration_sec`) via `context.WithTimeout`
around the run context, reusing the engine's existing clean-cancellation path; and a new
`sseWriter` (`internal/server/sse.go`) that decouples the engine's event-producing goroutine from
how fast the HTTP client actually reads, dropping the oldest queued event on overflow
(`server.sse_buffer_size`, default 256) rather than growing memory or blocking the producer. All
three configurable via matching `AEGIS_SERVER_*` env vars, default to unlimited/256 so existing
deployments are unaffected. **P15.12 — harden the `/ui` token-injection mechanism.** `GET /ui`
previously injected the daemon's real, long-lived auth token straight into HTML shell source — any
local process reaching the loopback port with no `Origin` header (so the origin guard didn't apply)
got that standing secret in cleartext, replayable for the daemon's whole lifetime. `handleWebUI`
now mints a random single-use "page token" (32 bytes, 60s TTL — `mintPageToken`,
`internal/server/auth.go`) and injects that instead; a new `POST /auth/exchange` endpoint (exempt
from the auth check for the obvious reason, still origin-guarded) redeems it exactly once — deleted
from the server-side map on first read regardless of outcome — and returns the real token, which
the frontend now fetches on load (`internal/server/webui/frontend/src/api.ts`) before making any
other API call, using it exactly as before thereafter. **P21.6 — MCP tool output trust boundary.**
MCP tool output flowed back into model context completely unfiltered despite MCP tools already
being capability-gated — a compromised MCP server was an unguarded prompt-injection vector. Added
an always-on provenance marker (`internal/mcp/trust.go`, `wrapUntrusted`) wrapping every
`tools/call`/`resources/read`/`prompts/get` result in an `<mcp_untrusted_output>` frame naming the
source server and instructing the model to treat the content as untrusted data, not instructions —
no configuration needed. Layered on top: an opt-in per-server heuristic scan
(`scan_output` on `MCPServerConfig`, mirroring the existing per-server `capability` field) that
flags prompt-injection-shaped output (ignore-prior-instructions phrasing, role-override attempts,
fake system-prompt tags, secret-exfiltration patterns) with a `[SECURITY WARNING]` line inside the
same frame — flagged, never silently dropped, matching the engine's existing non-fatal
`notice`-event convention. New `docs/mcp-trust-boundary.md` documents the boundary end to end.
Tests: `internal/server/limits_test.go`, `internal/config/config_test.go`,
`internal/server/webui_test.go`, `internal/mcp/trust_test.go`. `go build ./...`, `go vet ./...`
clean; `go test ./...` green except three pre-existing/environmental failures on this box
(`TestBuildImageBlocksFromPath`, and two `scan_test.go` 30s-timeout tests) confirmed unrelated —
each agent verified via `git stash` that they failed identically before its change. P21.5/P21.6's
track is fully shipped (no open items remain); P15.12's track is covered in
[roadmap.md](roadmap.md#open-work--p15-web-ui-parity-with-the-tui).
Earlier, 2026-07-08 — **`/threat-model` framework picker** (follow-up polish to P13.6:
a recognized leading framework name skips the clarifying question, otherwise a picker dialog opens
listing all six with descriptions; see the P13.6 section below) shipped after **P23**
(local-model context-window truth: Ollama detection, proactive-compaction notices, incremental
threat-model writing); see its section below.
Earlier the same day — **P22.1** (`/diff` command), **P22.4** (Ctrl+R input-history
search), and **P22.2** (`/review` read-only review mode) shipped from the same-day Codex CLI
evaluation.
**P22.1** adds a no-model-turn `/diff [--staged] [path]` — same pattern as `/scan` — showing the
working-tree git diff (tracked changes vs `HEAD`, or `--staged` for just the index) plus a
synthetic "new file" diff for every untracked file via `git diff --no-index -- /dev/null <file>`
(plain `git diff` omits untracked files entirely; this needed no index mutation like `git add -N`
would have). Rendered through chroma's built-in `diff` lexer (`highlightUnifiedDiff`, a sibling to
the existing `highlightSource`) for `+`/`-`/`@@` coloring, threaded through a `\x00diff` transcript
marker so the rendering happens in `tui.go` where the active theme lives — the same reason
`/theme`/`/clear` use marker passthrough instead of pre-rendering in the dispatcher.
**P22.4** adds Ctrl+R as a filterable, newest-first picker over sent-message history (the existing
`listDialog`/`list.Item` machinery, same as the timeline/model pickers), reusing the list's
built-in fuzzy filter as the actual "search." Ctrl+R was already bound to the session switcher, so
per an explicit user decision that switcher moved to **Ctrl+Y** — `docs/tui-guide.md`,
`docs/sessions.md`, and `/help`'s keybind-only-features line were updated to match. Selecting a
history entry recalls it onto the input line for further editing (does not auto-send), mirroring a
shell reverse-search accept.
**P22.2** adds `/review [--staged | <branch|commit>]`: resolves the target diff (uncommitted,
staged, a branch/tag's merge-base, or a single commit), inlines it into a prompt that loads the
already-shipped `content-review` builtin skill for structured severity-rubric findings, and
switches the session to `plan` (read-only) mode for the duration if it isn't already — real
permission-gate enforcement, reported back with how to switch back afterward. P22.3/P22.5/P22.6
remain open — see
[roadmap.md](roadmap.md#open-work--p22-openai-codex-cli-evaluation--2026-07-08).
**Previously, 2026-07-07:** **P20.1** (deep-research workflow, first of the three adopted
Odysseus-review items) shipped skill-first as scoped: new `deep-research` embedded builtin skill
(`internal/skills/builtin/deep-research/SKILL.md`) encoding a structured research playbook —
scope-the-question first (primary question + 2–5 sub-questions, budgets up front), iterative
plan → search → select → read → record rounds capped at 8 with explicit stop conditions
(saturation, all sub-questions corroborated, cap, or diminishing returns), a source-quality bar
(primary/authoritative preferred, corroborate-only tiers, reject SEO/AI-aggregator pages,
two-independent-sources rule for load-bearing claims), a structured findings log
(`url/title/type+date/summary/evidence/bearing` per source) plus an analyzed-URLs audit trail
including rejected URLs with reasons (kept in a `.aegis/research/<slug>.md` working file on longer
runs so compaction can't destroy it), numbered inline-citation discipline
(single-source claims flagged, contradictions surfaced with both sides cited), and a six-part
report format that can hand off to `html-report`/`latex-report` (`/report`) for a shareable
artifact. New `/research [topic]` TUI command (`commandDefs` entry + `cmdResearch` in
`internal/tui`) — the P13/P20 cross-cutting TUI-surface requirement, automatically covered by the
P14.1/P14.10 command-surface sync tests. Concept-level reimplementation only per the P20 AGPL
constraint — no Odysseus code, prompts, or assets were reused. `TestBuiltinsListsEmbeddedSkills`
want-list extended with `deep-research` (and `latex-report`, which had been missed);
built-in-skills lists in `CLAUDE.md`, `docs/skills.md`, `docs/configuration.md`,
`docs/memory-and-knowledge.md`, and `docs/tui-guide.md`'s command table updated. No persona
changes needed: `skill` is already in every non-debate persona's advisory Tools list (P13.7) and
`web_search`/`web_fetch` already in 19 of 22.
**Previously, 2026-07-07:** **P19** (docs/command misc bucket, both items) shipped: **P19.1**
(skill authoring guide) added `docs/skills.md`, a sibling to `docs/personas.md`/`docs/debate.md`
covering minimal single-file skills, bundled directory skills with a companion script and how the
generated `<skill_assets>` manifest exposes it to the model, frontmatter fields, project/user/
builtin precedence and name collisions, and a worked example — the mechanism was previously fully
built but documented only in code comments. Cross-linked from `docs/README.md`'s table and folded
into `docs/memory-and-knowledge.md`'s now-slimmer Skills section, which also picked up two accuracy
fixes found while writing the guide: the documented user-skills path (`~/.local/share/aegis/
skills/*.md`) didn't match the actual loader (`~/.aegis/skills/*.md`), and the documented memory-load
order had project/user skills reversed relative to the real project-shadows-user precedence.
**P19.2** (manual `/compact`) added a `Summarizer.ForceCompact` (`internal/compaction/
compaction.go`) that runs the same summarization pass as the automatic budget-driven `Compact` but
skips both `shouldCompact` budget checks, a `POST /sessions/{id}/compact` daemon endpoint
(`internal/server/sessions.go`, serialized against an in-flight run via the same per-session
semaphore `/rewind` uses) and TUI `/compact` command, for forcing compaction ahead of a known
tool-heavy stretch rather than waiting for the 85%-fill auto-trigger. Reports "nothing to compact"
(`Compacted: false`) rather than fabricating a summary when the conversation is shorter than
`KeepRecent` messages. Verified no collision with the pre-existing `/tools compact` (an unrelated
`/tools` subcommand toggling tool-output display width, not conversation compaction) — separate
top-level command-table entries, distinct dispatch paths. Tested with new unit tests for
`ForceCompact`'s ignore-budget and too-short-is-noop behavior (`internal/compaction/
compaction_test.go`) and server-level tests exercising the full endpoint round-trip including the
no-compactor-configured error path (`internal/server/server_compact_test.go`).
**Previously, 2026-07-07:** **P13.3.1** (shell-aware error assist) and **P13.3.5** (configurable
keybinding remap) shipped, picked as the two genuinely-valuable P13.3 items (over P13.3.2/P13.3.3,
judged lower-leverage — see [roadmap.md](roadmap.md#p133--terminal-enhancements-microsoft
-intelligent-terminal-review)). P13.3.1 deliberately excludes the `shell` tool itself: a tool call
the model makes already sees its own result on the next turn, so only the two surfaces where the
model has *no* automatic visibility needed a bridge — the embedded terminal pane and `!` bang
commands. New `termPane.beginRun`/`runOutput`/`lastCmd`/`lastOutput`/`lastExitCode`/`lastFailed`
(`internal/tui/terminal.go`) track a run's own output separately from the pane's full scrollback
buffer; `model.lastFailure` (`internal/tui/tui.go`) holds whichever of the two surfaces failed most
recently. `ctrl+g` (the new `Diagnose` binding) sends the failed command + its output as a new user
turn asking the model to diagnose and fix it, via the existing `sendUserMessage` path — same as
typing it by hand, just pre-filled. The terminal pane's status line and the bang-command transcript
entry both show a `<key> diagnose` hint when a command fails, reading the actual bound key rather
than a hardcoded one. P13.3.5 added a `tui.keybindings` config map (action name -> one or more
`bubbles/key` sequences, e.g. `terminal: ["alt+t"]`), applied via a new
`keyMap.applyKeybindings`/`bindingsByName` (`internal/tui/keymap.go`) that regenerates each
overridden binding's help label from its new primary key — so the F1 overlay and `/help` (which
previously always rendered `defaultKeyMap()`, ignoring any override; `SlashDispatcher` now carries
its own `keys` field, set from the model's actual keymap) both stay accurate after a remap. An
unknown action name in `tui.keybindings` is validated at TUI startup (`tui.Run`) and fails with a
named error rather than silently doing nothing.
**Previously, 2026-07-07:** **P17** (adaptive sub-agent concurrency, all 5 items) shipped: new
`internal/swarm/adaptive.go` (`AdaptiveLimiter`) throttles how many agents in a `parallel` workflow
batch run *simultaneously*, separate from the existing `MaxParallelAgents` (8) hard ceiling on how
many an `agents` array may *request*. Starts conservative at the floor (2) and adjusts with an AIMD
scheme driven by measured wall-clock speedup within each batch (`sum(individual durations) /
batch elapsed`) rather than static config or host/GPU introspection — evaluated and rejected
introspecting Ollama's own `OLLAMA_NUM_PARALLEL` heuristic since it isn't exposed via the API and
would mean reimplementing it blind from a fragile `nvidia-smi`/`rocm-smi` proxy signal.
`executeWorkflow`'s `"parallel"` case in `internal/tool/builtin/agent.go` now acquires a limiter slot
per spawn instead of firing every goroutine at once; a spawn error consistent with resource
exhaustion (timeout, connection refused, connection reset, 429) also triggers the same
multiplicative-decrease path as a low-speedup batch. One `AdaptiveLimiter` instance per daemon
process (`Server.agentLimiter`, threaded through a new `WithConcurrencyLimiter` `AgentToolOption`,
constructed alongside `NewAgentTool` in `server.go`) — in-memory only, does not persist across
restarts, since re-converging from the floor costs only a couple of batches. Current cap surfaced on
the existing `GET /status` / `/status` TUI surface (`api.StatusInfo.AgentConcurrency`) rather than a
new endpoint. Tested with deterministic unit tests against injected/synthetic durations (no real
sleeps) for the AIMD transitions, channel-synchronized (not sleep-based) concurrency-gating tests for
`Acquire`/`Release`, and an integration test confirming the `parallel` dispatch itself respects the
cap.
**Previously, 2026-07-07:** **P16.9** (in-terminal image rendering) shipped, closing out the
entire P16 track: new `internal/tui/imagerender.go` renders a half-block ANSI truecolor thumbnail
(upper-half-block trick — each cell's foreground is its top source pixel, background the pixel
below) in the transcript whenever an image attachment is sent, live or replayed from session
history. Gated on terminal capability via `charmbracelet/colorprofile`'s env/`NO_COLOR`/`CLICOLOR`
detection (256-color-or-better only), configurable with new `tui.image_rendering: auto|off`.
Decoding is best-effort — an unreadable path or unsupported format (notably WebP) silently falls
back to the pre-existing text notice. True kitty-graphics/iTerm2-inline-image protocol support was
deliberately descoped: bubbletea/ultraviolet's cell-diffed redraw model has no primitive for opaque
out-of-band terminal state (unlike its OSC-8-hyperlink `Cell.Link` support), and there was no real
kitty/iTerm2 terminal available to verify escape-sequence behavior against — the half-block
fallback needed none of that risk. See
[P16 shipped](releases-06.md#shipped--p16-items-tui-polish--interaction-parity) below.
**Previously, 2026-07-07:** **P16.8** (clipboard image paste) shipped: new
`internal/tui/clipboard_image.go` reads an image directly off the OS clipboard (not a pasted file
path) into a temp PNG, per-OS the same way `copyToClipboard` already is — `System.Windows.Forms.
Clipboard` + `Bitmap.Save` via an `-Sta` PowerShell call on Windows (verified end-to-end against a
real clipboard image and against clipboard text with no image), `pngpaste` on macOS, `wl-paste`/
`xclip -t image/png` on Linux. New `ctrl+v` keybinding plus a `/paste-image` slash-command fallback
for terminals that intercept ctrl+v themselves; both feed the existing `@image:` attachment-token
path, so no daemon-side changes were needed. See
[P16 shipped](releases-06.md#shipped--p16-items-tui-polish--interaction-parity) below.
**Previously, 2026-07-07:** **P16.7** (runtime-loadable themes) shipped: new
`internal/tui/theme_loader.go` derives a full `colorScheme` from a `themeFile` JSON schema
(background/foreground + the standard 16-color ANSI palette — the shape most published terminal
color schemes already ship in) by blending, reusing P16.3's `blend()` helper. Four embedded
built-ins (catppuccin, dracula, gruvbox, tokyonight) ship the same way builtin skills do, plus a
loader for project `.aegis/themes/<name>.json` and user `~/.aegis/themes/<name>.json` (project
wins). `/theme` and `tui.theme` now accept any of dark/light/builtin/custom name; an unknown name
lists everything currently resolvable instead of a fixed "want dark or light". See
[P16 shipped](releases-06.md#shipped--p16-items-tui-polish--interaction-parity) below.
**Previously, 2026-07-07:** **P16.2** (chroma syntax highlighting) and **P16.3** (diff
presentation upgrade) shipped together, as the roadmap's suggested sequencing called for ("one
visual unit"). New `internal/tui/highlight.go`: a `chroma.Style` built from the existing
colorscheme palette (P16.2), applied to diff added/removed/context lines, `read_file` result
blocks (stripping and re-deriving the gutter from the tool's own "N\t" line-number prefix), and
shell-command previews. `diffLines` (`toolview.go`) was rewritten for P16.3: a real line-number
gutter, hunk headers with actual `@@ -a,b +c,d @@` ranges (previously a bare placeholder), tinted
add/removed row backgrounds (`colDiffAddBg`/`colDiffDelBg`, derived by blending the theme's
success/destructive roles into the background so the tint stays on-theme), and word-level
intraline emphasis for single-line replacements (reusing the existing generic LCS `buildEdits` at
word granularity rather than a new diff algorithm). P16.2/P16.3 also fixed a same-session bug
caught before commit: the first hunk-header implementation computed the header only once its hunk's
full extent was known, i.e. *after* emitting that hunk's lines — headers must precede their
content, so hunk boundaries are now precomputed before the render pass. See
[P16 shipped](releases-06.md#shipped--p16-items-tui-polish--interaction-parity) below.
**Previously, 2026-07-07:** **P16.1** (TUI notifications & attention system) shipped: terminal
bell + OSC 9/777 desktop notification on stream-end/approval-pending/error (suppressed while the
terminal is focused, via bubbletea v2's `tea.FocusMsg`/`BlurMsg`), OSC 0/2 window-title updates
reflecting streaming/ready/approval state, new `tui.notifications` config + `/notify` command.
**Previously, 2026-07-06:** **P15.1** (web UI frontend architecture) shipped: moved `aegis ui`
off the old dependency-free single-file page to a bundled Vite + Preact + TypeScript frontend
(`internal/server/webui/frontend/`), built output committed at `internal/server/webui/dist/` and
embedded via `go:embed` (`internal/server/webui.go`) so `go build`/`go run` still need no Node.js.
Same session ported the prior page's exact feature set 1:1 onto the new stack — no new panels.
P15.2–P15.11 (the rest of the web-UI-parity track) are now unblocked but not started.
**Previously, 2026-07-06:** **P13.2** (trufflehog secret scanner, opt-in alongside gitleaks,
with a host-only-gated live-verification opt-in) shipped. Only P13.3/P13.4/P13.7 remain open in P13.
**Also 2026-07-06:** **P13.6** (`threat-modeling` builtin skill covering STRIDE/LINDDUN/
PASTA/Trike/VAST/NIST 800-154, `/threat-model` TUI command, `security-architect` persona updated to
name the skill) shipped.
**Also 2026-07-06 (user-requested, not a roadmap item):** per-scanner selection + language
auto-detection + persisted reports for `/scan`/`aegis scan`/`security_scan`. `--scanner
<name-or-category>` (CLI, repeatable) / `scanners` (tool JSON) / a `/scan <selector>[,<...>]
[path]` TUI arg now restrict a scan to specific scanners (exact name, e.g. "trufflehog") or a
category alias ("secrets" → gitleaks+trufflehog, "sast", "sca"/"deps", "iac", "misconfig"),
force-enabling the selection for that run regardless of config — same posture `/scan image`
already had for its own distinct scanner set. A plain scan with no selector now also
auto-detects the project's language (go.mod/*.go, requirements.txt/*.py, Gemfile/*.rb,
package.json/*.js) and auto-enables the matching opt-in SAST engine (gosec/bandit/brakeman/
njsscan) for that run — `AutoEnableLanguageScanners` never overrides an explicit
`security.tools.<name>.enabled` either direction, tracked via a new `ToolPolicy.EnabledExplicit`
bit set in `OptionsFromConfig`. Every findings scan (path/image/network/dast, across CLI/TUI/
tool surfaces) is now also persisted as JSON under `.aegis/security/` (`scan.json`/`image.json`/
`network.json`/`dast.json`, overwritten each run — same posture as `.aegis/sbom.cdx.json`),
per an explicit ask that scan results survive past terminal scrollback/a model turn. New
`internal/security/select.go` (`ResolveSelector`/`SelectScanners`/`DetectLanguages`/
`AutoEnableLanguageScanners`) and `report_artifact.go` (`WriteReportArtifact`).
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

