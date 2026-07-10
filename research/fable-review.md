# Aegis Codebase Review — Fable 5

_Objective review pass, 2026-07-10. Scope: full application code under `cmd/` and
`internal/`. Deliberately ignored everything in `research/` (roadmap, releases,
prior reviews) so the findings below are derived only from the code as it stands._

The codebase is in good shape: the daemon/client split is clean, the tool
capability model is coherent, and the security-sensitive seams (path validation,
SSRF dialer, env stripping, permission gate stacking, sub-agent mode clamping)
show real defensive thought and are backed by tests. The findings below are the
gaps that remain — ordered by severity within each section.

---

## 1. Security

### S1 — MCP server sampling bypasses every cost and rate control (High)

**Where:** `internal/server/helpers.go:165` (`buildSamplingHandler`), wired in
`internal/server/server.go:513-518`; request entry at `internal/mcp/mcp.go:279`
(`handleSampling`).

Every configured MCP server is granted the `sampling` capability at
initialization (`mcp.go:352-359`), and `buildSamplingHandler` is attached to all
clients unconditionally when an adapter exists. When a server issues
`sampling/createMessage`, the handler calls `adapter.Stream` directly with:

- no `cost.Tracker`,
- no check against `BudgetUSD` / `MaxTokensPerRun` / session caps / daily caps,
- `context.Background()` as the root (`mcp.go:289`), so it isn't even bound to a
  request lifetime.

The result: a compromised, malicious, or simply buggy MCP server can drive
unbounded, unbilled model spend that is invisible to `/status`, the daily-cap
gate (`beginDailySpend`), and every other accounting path. This is the same
"spent real tokens without being gated or recorded" class of bug the `spendGuard`
refactor (`messages.go:404`) was built to prevent — but sampling never goes
through it.

**Recommendation:** Route sampling through the same daily-spend guard and a cost
tracker; enforce a per-request `MaxTokens` ceiling (it already clamps *down* to
`req.MaxTokens` but never *up* to a server-side cap) and a call-rate/quota per
server. Consider making sampling opt-in per MCP server (a `sampling: true` flag on
`ServerConfig`) rather than granted to all servers by default.

### S2 — `/ui` hands the real auth token to any client that can reach the port (Medium)

**Where:** `internal/server/webui.go:51` (`handleWebUI`),
`internal/server/auth.go:143` (`handleAuthExchange`), auth bypass list at
`auth.go:51`.

`generateAndWriteToken` deliberately writes the auth token `0600` and applies a
non-inherited owner-only ACL on Windows (`restrictToOwner`) so that on a shared
host another local account cannot read it. But `GET /ui` is exempt from
`authMiddleware`, mints a page token, and embeds it in the returned HTML; `POST
/auth/exchange` (also exempt) trades any valid page token for the **real** daemon
token. So any local user or process that can open a connection to the loopback
port can do: `GET /ui` → scrape the page token from the HTML → `POST
/auth/exchange` → receive the real token → call every authenticated endpoint.

Loopback is not user-partitioned, so this effectively downgrades the security
boundary from "can read the owner's `0600` token file" to "can connect to the
port" — undermining the file-permission hardening in `auth.go`. The
`originMiddleware` doesn't mitigate it: a non-browser client simply sends no
`Origin` header and is allowed through (`auth.go:80-88`).

**Recommendation:** Gate `/ui` and `/auth/exchange` behind something a peer local
user can't trivially obtain — e.g. require the page load itself to carry a
one-time secret handed to the browser by the CLI that launched it, or bind
exchange to a value only the token-file owner has. At minimum, document that the
HTTP port must be treated as owner-only trust and consider a per-user socket
(unix socket with `0600`, or a loopback + token-file-gated `/ui`).

### S3 — Network egress policy silently does not cover the shell tool (Medium)

**Where:** startup warning at `internal/server/server.go:402-406`; policy applied
in `internal/server/engine_build.go:113` (`ContextualGate`).

`security.egress_then_write` and `network_allowlist` are enforced by the
`ContextualGate` against tools with the `network` capability (`web_fetch`,
`web_search`, MCP network tools). The `shell` tool has capability `execute`, so
`curl`/`wget`/`nc`/`python -c 'urllib...'` run from a shell command are never
evaluated against the allowlist. There *is* a startup log warning, but it is easy
to miss and the policy looks active from the operator's point of view (it shows in
config, gates the web tools, and writes audit decisions). An operator who set an
allowlist to contain exfiltration would reasonably believe shell egress is
constrained when it is not.

**Recommendation:** This is a real limitation of host-level execution, correctly
called out in the log. Strengthen it by surfacing the warning in `/status` and
`/healthz` (as the sandbox-fallback state already is), and/or refusing to start
with a network policy set unless a container/OS sandbox that can actually enforce
egress is selected.

### S4 — `read_file` and `ls` can flood context with unbounded output (Medium — also a token issue, see T1)

**Where:** `internal/tool/builtin/file.go:40` (`readTool`), `ls.go:29`.

`read_file` reads up to `maxReadBytes = 50 MiB` and, when no `limit` is given,
returns *every* line of the file into the conversation. `ls` walks the tree with
no cap on the number of entries returned. Compare with `grep`/`glob`, which cap at
500–1000 matches (`search.go:74,191`), and `shell`/`git`, which truncate at
200/100 KiB. A model (or a prompt-injected instruction) that reads a large
generated file or lists a huge directory can single-handedly blow the context
window — on a local Ollama backend this then triggers silent front-truncation of
the system prompt. It's both a stability/security concern (untrusted content can
force-evict instructions) and a cost concern.

**Recommendation:** Give `read_file` a default line/byte cap (e.g. 2000 lines like
the harness's own Read tool) with an explicit "truncated — use offset/limit"
footer, and cap `ls` output the way `grep` already does.

### S5 — Minor / defensive

- **SSRF dialer TOCTOU (Low):** `ssrfSafeDialer` (`web.go:102`) resolves the host,
  checks the IPs are public, then dials `ips[0]`. Because it dials the
  already-resolved IP rather than re-resolving, it avoids the classic
  rebind-between-check-and-connect window — good. Worth a comment noting that
  invariant so a future refactor to `DialContext(ctx, network, addr)` doesn't
  reintroduce the gap. No change needed today.
- **`expandFileMentions` reads outside workspace intent (Low):**
  `messages.go:690` joins `@path#Lx-y` mentions against `workspace` with
  `filepath.Join` but does not run them through `sandbox.ValidatePath`, so
  `@../../etc/passwd#L1-5` in a user message would be inlined. This is
  user-supplied (not model-supplied) text, so the trust level is the operator's
  own, but it is inconsistent with every file tool going through `resolvePath`.
  Consider validating for consistency.

---

## 2. Service interaction gaps

### G1 — Two different token estimators can disagree about when to compact (Medium)

**Where:** `internal/engine/engine.go:103` (`estimateTokens`, script-aware) vs.
`internal/compaction/compaction.go:101` (`EstimateTokens`, flat chars/4).

The engine's proactive-compaction trigger (`engine.go:390`) uses the script-aware
estimator, which prices CJK at ~1 token/char. The `Summarizer.shouldCompact`
check (`compaction.go:86`) uses a separate flat `chars/4` estimator that
undercounts the same CJK text by up to 4×. So on a CJK-heavy conversation the
engine can decide "85% full, time to compact," call `Compact`, and the summarizer
can independently conclude "not over budget, nothing to do" and return
`changed=false` — leaving the engine's `!compacted` branch to emit the "nothing
left to compact" notice even though a compaction was warranted. The two components
should share one estimate.

**Recommendation:** Have compaction consume the engine's estimator (or export one
shared function). At minimum, make `shouldCompact` use the script-aware count so
the trigger and the actor agree.

### G2 — `writtenPathsFromInput` / guard file collection misses several write tools (Low)

**Where:** `engine.go:908` (`writtenPathsFromInput`), used by the output guard.

The guard reads back written files to validate the real deliverable, but the path
extractor only recognizes `path` / `file_path` / `edits[].path`. It does not
recognize the `diagram` tool's output path, `latex` build outputs, or any MCP/
custom write tool with different field names, so the guard silently validates only
the chat summary for those. This is documented as a known limitation in the code
comment, and it fails safe (guard still sees the text), but it means "output
guard on" is quietly weaker for report/diagram-generating personas — exactly the
personas most likely to enable it.

**Recommendation:** Have write-capable tools report the paths they wrote via the
`tool.Result` (a structured field) rather than the engine re-parsing input JSON by
convention; then the guard covers every writer uniformly.

### G3 — `serializeTool` treats `spawn` as concurrent-safe (Low, by design — worth confirming)

**Where:** `engine.go:760` (`serializeTool`).

Only `write`/`execute` tools take the exclusive lock; `spawn` (the `agent` tool)
runs concurrently with other tools in the same round. That's intentional for
parallel fan-out, but a spawned sub-agent can itself issue `write`/`execute` calls
that race the parent round's writes, since the child engine has its own
`execLock`. In practice the parent blocks on `agent`'s result before its own next
round, and parallel workflows are the explicit use case, so this is likely fine —
but it's the one place the "writes never race" invariant depends on runtime
behavior rather than the lock. Worth an explicit note or test.

### G4 — MCP `tools/list_changed` refresh mutates the daemon-wide registry, not session clones (Low)

**Where:** `internal/mcp/tool.go:284-291` (`onToolsChanged`), session clones at
`server.go:199` (`sessionToolRegistry`).

Session-scoped tool registries are clones of `s.tools` created lazily per session
(the P9 fix so `tool_search` exposure doesn't leak across sessions). A dynamic MCP
`tools/list_changed` re-lists and upserts into the base registry, but existing
session clones already handed out won't see the change until they're recreated.
The reverse (a removed tool still callable via a stale clone) is the more relevant
case. Low impact, but the clone/refresh interaction is worth a test.

---

## 3. Performance & optimization

### P1 — `effectiveSystem` is fully rebuilt on every message, defeating prompt caching (Medium)

**Where:** `internal/server/messages.go:214` calls
`s.effectiveSystem(sess.System, id)` per POST; body at `helpers.go:32`.

The Anthropic adapter places a cache breakpoint on the system block
(`anthropic.go:196-205`) so a stable system prefix is cached across turns. But
`effectiveSystem` recomputes the whole prompt each message, folding in
`memory.LoadContext()`, `memory.Load()`, `skills.BuildIndex(...)`, and the repo
map. If any of those change between turns (memory is the likely one — it's
relevance-scored and can reorder), the system string changes and the cache prefix
is invalidated, forcing a full re-encode of a large system prompt every turn. On
cloud providers that's real money and latency; the cost is proportional to system
prompt size, which here includes tool-use guidance, platform block, memory,
skills index, repo map, and deferred-tools block.

**Recommendation:** Split the system prompt into a stable prefix (persona +
platform + tool-use guidance + deferred-tools + repo map) and a volatile suffix
(memory, session skills), and only cache-breakpoint the stable prefix. Or cache
the assembled `effectiveSystem` per session and rebuild only when an input
signature changes (the persona-refresh pattern already does exactly this for
persona files).

### P2 — Whole-file reads back through the guard multiply token cost (Low)

**Where:** `engine.go:958` (`collectWrittenFiles`) reads up to 5 written files in
full each time the guard runs. Combined with T1 (no read cap), a guarded turn that
wrote a large report re-reads the entire file into the validator prompt on every
corrective retry. Capping read size (S4/T1) fixes this too.

### P3 — `estimatedTokens` cache is invalidated on every in-place rewrite (Low, acceptable)

**Where:** `engine.go:53` (`invalidate`). Compaction, repair, and `prepareStep`
returning a modified slice all call `invalidate()`, forcing a full re-scan on the
next `estimatedTokens()`. This is correct and the re-scan is O(conversation),
which is unavoidable after a rewrite — noting it only to confirm it's not a
regression. The incremental `Append` path (`engine.go:44`) correctly keeps the
cache warm in the common case.

---

## 4. Token / context usage optimization

### T1 — Add default output caps to `read_file` and `ls` (High-value, easy)

See **S4**. This is the single highest-leverage token change: an uncapped
`read_file` is the most likely way for a single tool call to consume the entire
context window, and it happens on completely normal tasks (reading a big lockfile,
a generated bundle, a data file). A 2000-line default cap with an explicit
truncation footer aligns it with `grep`/`glob`/`shell`, which are already capped.

### T2 — Deferred-tool and skills progressive disclosure is well done (Positive)

The `deferred` tool tier (`builtin.go:134`) + `tool_search`, and the skills
`name — description` index that loads full bodies on demand (`helpers.go`), are
exactly the right pattern for keeping per-turn schema/prompt tokens low. Worth
preserving as new tools are added: default niche tools to `RegisterDeferred`
rather than the always-exposed `tools` slice. No change needed — flagging as the
model to follow.

### T3 — Tool-result pruning before summarization is good; consider extending it (Low)

`pruneStaleToolResults` (`compaction.go:143`) drops superseded file reads / old
search dumps deterministically before paying for an LLM summarization call — a
cheap, high-value token win. The one gap: it runs only inside the compaction path
(when over budget). A large `read_file` result that is later re-read (stale) still
sits in context for many turns before compaction fires. If T1 caps reads this
matters less, but pruning obviously-superseded reads eagerly (on append of a newer
read of the same path) would keep context leaner between compactions.

### T4 — `renderTranscript` truncates tool results to 800 runes for summarization (Positive)

`compaction.go:228` already bounds how much of each tool result feeds the
summarizer, so a giant tool dump doesn't balloon the summarization call itself.
Good; no change.

---

## Summary of recommended actions, by priority

| # | Severity | Action |
|---|----------|--------|
| S1 | High | Route MCP sampling through the daily-spend guard + cost tracker; cap/quota it; make it opt-in per server |
| T1/S4 | High | Add a default line/byte cap to `read_file`; cap `ls` output |
| S2 | Medium | Close the `/ui` → `/auth/exchange` real-token-handout path on shared hosts |
| P1 | Medium | Cache/split `effectiveSystem` so the prompt-cache prefix stays stable across turns |
| G1 | Medium | Unify the two token estimators so the compaction trigger and actor agree |
| S3 | Medium | Surface the "shell bypasses network policy" state in `/status`; consider refusing to start without an enforcing sandbox |
| G2 | Low | Have write tools report written paths structurally so the output guard covers all writers |
| S5, G3, G4, P2–P3, T3 | Low | Defensive hardening and small token wins as noted above |

**Overall:** no critical remote-exploitable defects were found in the reviewed
code. The two findings that matter most are both about *unbounded consumption* —
S1 (unmetered model spend via MCP sampling) and T1 (unbounded context via
`read_file`) — because each lets a single actor (a malicious MCP server, or a
large file) escape a limit the rest of the system carefully enforces everywhere
else.
