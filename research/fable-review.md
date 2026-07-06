# Aegis — Objective Evaluation (Fable Review)

**Date:** 2026-07-06
**Method:** Independent ground-up review of the codebase, build, test suite, CLI surface, and documentation. Nothing under `research/` was used as input — all claims below were verified directly against source, command output, or docs. Citations are `file:line` where useful.

---

## Executive summary

Aegis is a **daemon + client AI agent harness** (~67k lines of Go, 130 commits, 50 internal packages) with an unusually strong engineering core for a project of its age. The build is clean, the full test suite passes across all 52 test packages, `go vet` is silent, and the codebase carries essentially **zero TODO/FIXME debt**. Its differentiators are real: a security-scanning subsystem integrating 19 scanners with graceful host/WSL/container fallback, a defense-in-depth permission model with honest documentation of its own limits, a multi-agent debate primitive, and both directions of MCP integration.

The gaps are mostly **project-maturity gaps rather than design flaws**: no CI pipeline, no release versioning, a handful of monolithic files, a minimal web UI that lags far behind the TUI, permissive out-of-the-box security defaults, and a few Windows-specific soft spots (token-file permissions, POSIX-mode assumptions).

**Overall: a technically mature, security-literate harness that now needs delivery-engineering scaffolding (CI, releases, hardened default profile) more than it needs new features.**

---

## Verification results

| Check | Result |
|---|---|
| `go build ./cmd/aegis` | ✅ clean |
| `go test ./...` | ✅ all 52 packages pass |
| `go vet ./...` | ✅ no findings |
| TODO/FIXME markers in non-test source | ✅ zero (all grep hits are LaTeX-template/prompt content) |
| Test volume | 142 test files, ~22,000 test lines vs ~44,900 source lines (**≈49% test-to-source ratio**) |
| Fuzz tests | ❌ none (`func Fuzz` count: 0) |
| CI configuration | ❌ none (no `.github/`, no pipeline config of any kind) |
| Release versioning | ❌ zero git tags; binary reports `0.0.1-dev` |

---

## Architecture & daemon

### Strengths

- **Clean layering with one seam per concern.** The engine talks to any LLM through a single `provider.Adapter.Stream` interface; permission gating, compaction, output guard, cost tracking, and hooks are all injected via `engine.Options`. Swarm and debate are decoupled from the engine via caller-supplied `RunFunc`s. This is textbook dependency inversion and it shows in testability (the whole eval harness runs against a scripted adapter with no API key).
- **The agent loop is defensively engineered** (`internal/engine/engine.go:256-485`) to a degree rare even in mature harnesses:
  - Repairs orphaned `tool_use` blocks from interrupted runs that would otherwise permanently brick a session (line 261).
  - Proactive compaction at 85% of the context window *before* each turn, not just reactively (line 329).
  - Budget gates before **both** the model call and the tool round, with a comment documenting the "dead zone" bug this closes (line 299).
  - Loop detection on repeated identical tool-call signatures.
  - Graceful step-limit exhaustion: injects a summarize-instruction and suppresses tool schemas rather than erroring (line 349), and discards hallucinated tool calls in that mode.
  - Token-limit continuation prompting instead of silently returning truncated output (line 394).
  - Output-guard corrective retries with an unusually well-crafted corrective prompt that tells the model to fix the artifact, not just apologize (line 410).
  - Mid-run steering drained between tool rounds.
- **Provider resilience is layered correctly**: per-adapter retry decorator (exponential backoff from 500ms, Retry-After-aware, 4 retries default — `internal/provider/retry.go`) underneath a failover chain across providers (`internal/provider/failover.go`), with typed `APIError.Retryable()` classification.
- **Concurrency-aware tool dispatch**: read/network tools run concurrently, write/execute serialized via RWMutex.
- **Lean, well-chosen dependencies**: Charm stack for TUI, cobra, koanf, CGo-free `modernc.org/sqlite`. Provider clients are hand-rolled HTTP — no vendor SDK lock-in.
- **HTTP server hygiene**: `ReadHeaderTimeout`/`ReadTimeout`/`IdleTimeout` set, `WriteTimeout` deliberately omitted for SSE with a comment explaining why (`internal/server/server.go:620-627`); graceful shutdown cascades through cron, swarm, tasks, sandbox, LSP, MCP clients.

### Issues

- **`internal/server/server.go` is a 2,830-line monolith** carrying routing, auth middleware, session handlers, engine construction, sub-agent wiring, file-mention expansion, and more. `internal/tui/tui.go` (2,791 lines) and `internal/tui/slash.go` (1,703) have the same problem. Tests pass, but change-risk concentrates in these files.
- **Cross-cutting invariants are convention-enforced.** The `routes()` doc comment (`server.go:735-744`) candidly admits that any handler spending model tokens must remember to call `checkDailyCaps`/`recordDailySpend`, that nothing enforces this, and that `/debate` already shipped once without it. This is an accident waiting to recur; a wrapper type for "model-spending handler" would make the compiler enforce it.
- **Context estimation is chars/4** (`engine.go:332`). Fine for ASCII-dominant conversations; materially wrong for CJK or emoji-heavy content, which could delay compaction past the window on some providers.

---

## Capabilities

### Breadth (all verified against the binary or source)

- **54 built-in tool implementations** spanning file ops, git/PR, shell, web fetch/search, LSP (definition/references/hover/symbols/call-hierarchy/diagnostics), security scanning, DAST, recon, diagrams, LaTeX document builds, cron, todo/task tracking, team mailbox, knowledge base, memory, skills, and tool search. Default exposure is deliberately smaller (~19 in `dry-run`); the rest register with feature config — a sensible progressive-disclosure choice.
- **22 built-in personas** covering a coherent security-practice org chart (security architect, appsec, red-team, risk assessor, cloud security, SRE…) plus generic/security debate-role trios (critic/arbiter). Custom personas are hot-reloaded `.md` files with model/mode/tools/rules/guard overrides.
- **7 embedded skills** (security-audit, threat-modeling, redteam-engagement, content-review, debug-investigation, html-report, architecture-diagram), dormant by default at zero system-prompt cost until enabled — a genuinely good design for prompt budget.
- **Security scanning: 19 scanners** (SAST ×7, secrets ×3, SCA/SBOM ×3, container ×4, IaC, DAST, network ×2) with per-tool resolution to host binary → WSL → digest-pinned container image, and `aegis security status` explaining exactly why any tool won't run and how to fix it. Cross-tool dedup, CWE→ASVS mapping, and an accepted-risk baseline **with mandatory expiry** round this out. This subsystem alone distinguishes Aegis from general-purpose harnesses.
- **Multi-surface access**: interactive TUI, one-shot `chat`, `parallel` fan-out, background/detached sessions, browser web UI, ACP (Zed/Neovim), MCP client *and* MCP server (`mcp-serve` exposes sessions as tools to other harnesses), cron scheduling, git worktree isolation.
- **Debate primitive**: propose/critique/rebut/arbitrate over any claim, domain-selectable persona trios, evidence-citation checking, shared budget tracking, file grounding.
- **Ops affordances**: per-turn traces, checkpoints/rewind, session TTL auto-pruning, cost ledger shared across a whole sub-agent tree, `dry-run` for context inspection.

### Gaps / opportunities

1. **Web UI is a 324-line single HTML file** — functional but an order of magnitude behind the TUI. Either invest (session list, approval UI, streaming polish) or explicitly document it as a debug viewer so expectations are set.
2. **Provider matrix is Anthropic + OpenAI-compatible only.** OpenAI-compat covers Ollama/vLLM/LM Studio, so local coverage is good, but there is no native Gemini, Bedrock, or Vertex adapter. The `Adapter` seam makes each a bounded, mechanical addition.
3. **Eval harness is deterministic-only.** The package doc (`internal/eval/eval.go`) openly states live-model quality evaluation was deferred for lack of a trigger. Reasonable — but as personas/skills/prompts grow, prompt-regression risk grows with no detection mechanism. A small nightly rubric-judged suite against a local model would close this cheaply.
4. **No fuzz tests** despite several parser-shaped attack surfaces that consume untrusted input: SARIF ingestion, scanner-output normalizers, MCP messages, `@file#L10-40` mention expansion, ACP JSON-RPC.

---

## TUI & visuals

Assessed from source (`internal/tui/`, 10.6k lines) and the 355-line TUI guide.

### Strengths

- **Real visual design effort**: a gradient-rendered Greek-shield logo with Gorgon-eye/Omega motif (`logo.go`), a hand-rolled shimmer "working" pulse built on `lipgloss.Blend1D` with no extra dependencies (`shimmer.go`), semantic color roles with dark + light schemes bound at startup (`colorscheme.go`), and an ANSI16 fallback path for low-color terminals — accessibility thinking most TUIs skip.
- **Comprehensive style system**: distinct styles for diffs (add/del/meta), tool output with gutter rules, extended-thinking display, turn separators, elapsed-time counters (`theme.go`) — the terminal timeline is treated as a designed surface, not a log dump.
- **Deep interaction feature set**: ~39 slash commands with argument hints and per-command detailed help, interactive persona/session/timeline pickers, approval dialogs, a config wizard, a security-config wizard, `@path#L10-40` file references, multi-line input, input history, message queueing, draft stash, live task-progress strip, toasts, `Ctrl+X` terminal pane, `/copy`, `/share html|md|json`, theme switching at runtime.
- Modern stack: bubbletea/lipgloss/bubbles/glamour/huh **v2** with charmtone — current, not legacy, Charm APIs.

### Issues

- `tui.go` at 2,791 lines concentrates the whole model-update loop; component extraction (as already done for pickers/wizards/toasts) hasn't reached the core.
- No automated visual regression (golden-render tests exist in Charm's ecosystem via `teatest`); TUI correctness relies on unit tests of logic plus manual inspection.

---

## Security posture

This is the strongest dimension of the project, and notably the codebase is honest about its own limitations in comments and docs rather than overclaiming.

### Strengths

- **Daemon API auth done right** (`server.go:2709-2775`): 32-byte `crypto/rand` token per start, written `0o600`, required on every route except `/healthz` and the UI page bootstrap, compared with `subtle.ConstantTimeCompare`, plus an Origin middleware that blocks non-loopback origins (DNS-rebinding mitigation). The server **refuses to start** if token generation fails, and the middleware defends in depth against an empty token. Binds to `127.0.0.1:4127` only.
- **Coherent capability/permission model**: every tool declares read/write/execute/network/spawn; three modes (plan/build/auto) map capabilities to allow/ask/deny (`permission/permission.go:54-76`); text allow/deny rules layer on top; contextual gates add stateful rules (egress-then-write requires approval; network domain allowlist).
- **Honest threat-model documentation**: the `ContextualGate` doc block (`permission/contextual.go:27-35`) explicitly states these are fetch-layer controls that a shell `curl` bypasses entirely, and points to the container sandbox as the real egress boundary. This kind of candor prevents false confidence.
- **Sub-agent parity**: spawned teammates get the *full* gate stack (contextual + rules), clamped modes that cascade to grandchildren, and draw against the parent's shared cost ledger (`server.go:556-616`) — the classic "sub-agent bypasses parent policy" hole is closed, and subprocess workers inherit *remaining* budget rather than a fresh allowance (`swarm/subprocess.go`).
- **Safe defaults where it counts**: MCP server defaults to `plan` mode with auto-approve off; non-interactive approver defaults to deny (`AutoDeny`); scanner container fallbacks require digest-pinned images; active DAST and network scanners (nmap/nuclei/zap/trufflehog verification) are opt-in with explicit target allowlists; security baseline entries require expiry dates.
- **Output guard on by default** with a thoughtful rubric, plus audit logging of contextual policy decisions.

### Gaps and issues

1. **Windows token-file permissions are cosmetic.** `os.WriteFile(path, token, 0o600)` does not produce a user-only ACL on Windows — the file typically inherits the parent directory's ACL. On a shared Windows machine another local user may be able to read the daemon token. Fix: explicit ACL (e.g. `golang.org/x/sys/windows` security descriptor) or DPAPI encryption on Windows.
2. **"Plan" mode is read-only for the disk, not the network.** `CapNetwork` is *Allow* in plan mode (`permission/permission.go:66-70`). Combined with read access, a prompt-injected plan-mode session can read sensitive files and exfiltrate via `web_fetch` with no prompt. The network allowlist can mitigate, but it's empty by default. Consider Ask-gating network in plan mode, or an explicit "research" vs "offline-plan" distinction.
3. **Permissive defaults overall** (verified via `dry-run` resolved config): sandbox `local` (host execution), all cost caps 0/unlimited, `EgressThenWrite` off, no network allowlist. Each is individually defensible for a local-Ollama default, but there is no one-command hardened profile. Opportunity: `aegis harden` or a `--profile strict` that flips sandbox=auto, egress-then-write=on, caps on, plan-mode network=ask.
4. **Daily-cap enforcement is a per-handler convention** (self-flagged at `server.go:735`), already missed once for `/debate`. Structural fix available and cheap relative to the risk.
5. **Web UI serves the bearer token to any local process** that can GET `/ui` (the page injects the token; the endpoint is auth-exempt by necessity). On single-user machines this is equivalent to reading the token file, so it's mostly acceptable — but combined with (1) on Windows it broadens the local-attacker story. A per-page short-lived session token would be tighter.
6. **No CI means no continuous security regression.** The golden-file scan regression tests and the strong unit suite only run when someone runs them.

---

## Code quality & engineering practice

- **Comment discipline is exceptional.** Non-obvious decisions carry *why* comments, frequently with the historical bug that motivated them (budget dead-zone, P10.1 sub-agent bypass, orphaned-tool repair, SSE WriteTimeout omission). The code reads like it expects an auditor.
- **Test suite is broad and behavioral**: 49% test-to-source line ratio; scenario-based eval harness driving a fully-wired engine through multi-turn conversations with golden transcripts; adversarial tests; regression goldens for scanner output.
- **Docs are substantial**: ~6,900 lines across 16 files (913-line security doc, 1,017-line tools reference, per-subsystem guides). Risk: they're hand-maintained against a fast-moving surface — 4 docs files sit modified in the working tree right now — and there is no doc-vs-code drift check.
- **Naming/versioning maturity lags the code**: no git tags, no changelog automation, `0.0.1-dev` version string, no CI, no lint config beyond vet, single-platform build scripts (`build-macos.sh`, `build-linux.sh`, `build-windows.ps1`) with no cross-compile matrix.

---

## Prioritized recommendations

| # | Recommendation | Effort | Impact |
|---|---|---|---|
| 1 | **Add CI** (build + test + vet + race on push, all three OSes) | Low | High — the excellent test suite currently protects nothing automatically |
| 2 | **Windows token-file ACL/DPAPI fix** (`generateAndWriteToken`) | Low | High on shared Windows hosts — auth secret readable cross-user |
| 3 | **Compiler-enforce the daily-cap handler convention** (typed spending-handler wrapper) | Low | Medium — closes a self-documented recurring-bug class |
| 4 | **Hardened profile** (`--profile strict` / `aegis harden`): sandbox auto, egress-then-write on, network allowlist prompt, caps | Medium | High — converts existing controls from opt-in to one decision |
| 5 | **Gate or split network access in plan mode** | Low | Medium — closes the read-then-exfiltrate path in "read-only" sessions |
| 6 | **Release engineering**: tags, versioned builds (`-ldflags` version stamping), changelog from `releases.md` | Low | Medium |
| 7 | **Split the three monoliths** (`server.go`, `tui.go`, `slash.go`) along their existing comment-section seams | Medium | Medium — pure maintainability |
| 8 | **Fuzz the untrusted-input parsers** (SARIF ingest, scanner normalizers, file-mention expansion, ACP/MCP framing) | Medium | Medium |
| 9 | **Live-model eval tier** (nightly, local model, rubric judge) for prompt/persona regression | Medium | Medium — grows in value with every persona/skill added |
| 10 | **Decide the web UI's ambition** (invest or document as debug surface) | Low–High | Low–Medium |
| 11 | Replace chars/4 context estimation with a cheap tokenizer approximation for non-ASCII content | Low | Low |

---

## Bottom line

Aegis's core — engine, permission model, provider layer, security scanning — is engineered to a standard that many funded agent products don't reach: defensively coded, honestly documented, and thoroughly tested. Its distinctive value is the **security-practitioner harness** angle (scanners, personas, debate, threat-modeling, baseline governance), which no general-purpose competitor bundles coherently. The weaknesses cluster in *delivery* maturity (CI, releases, hardened defaults, web UI) and a small number of platform-specific security nuances. All of the top-five recommendations are low-to-medium effort; none require architectural change.
