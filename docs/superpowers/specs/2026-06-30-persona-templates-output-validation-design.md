# Persona Templates (P3.3) + Output Validation (P2.4) — Design

**Date:** 2026-06-30
**Roadmap items:** `research/roadmap.md` §P3.3 (component serialization / persona
templates) and §P2.4 (dual-layer output validation), implemented together because
P3.3 provides the configuration home that P2.4's `output_guard` needs.

---

## Goals

1. **Shareable persona templates (P3.3):** define a persona as a single
   markdown-plus-frontmatter file — system prompt, description, model, permission
   mode, tools, permission rules, and output guard — that can be dropped into a
   project or user directory and selected like any built-in persona, with no code
   changes.
2. **Output validation (P2.4):** validate a persona's final answer before
   returning it, retrying on failure, and surface the result. Two modes: a
   lightweight deterministic `schema` check and an `llm` rubric check.
3. **On by default, toggleable:** a generic quality guard applies to every
   persona unless overridden or disabled, and can be flipped per session at
   runtime via `/guard`.

## Non-goals (this pass)

- Per-session **tool filtering** from a persona (the `tools` field is parsed and
  stored but not yet enforced at the session level; deferred).
- **Cross-provider** model override (model id only, within the configured
  provider).
- Applying output guards to **sub-agents** (`agentdef`); guards are top-level
  session concerns here.
- Persisting the per-session `/guard` toggle across daemon restarts (in-memory;
  resets to the config default).

---

## Architecture

### 1. `internal/persona` — file-loaded personas (P3.3)

Extend the `Persona` struct with optional override fields; built-ins leave them
zero-valued so their behaviour is unchanged:

```go
type Persona struct {
    Name        string
    Description string
    System      string
    Model       string        // model id override (same provider); "" = global
    Mode        string         // permission mode; "" = session/config default
    Tools       []string       // parsed, not yet enforced (deferred)
    Rules       []string       // permission rules merged into the session gate
    Guard       *GuardConfig   // nil = use global default; Disabled = no guard
}

type GuardConfig struct {
    Disabled   bool     // "output_guard: none" in frontmatter
    Mode       string   // "schema" | "llm"
    Schema     []string // schema mode: required top-level JSON keys
    Rubric     string   // llm mode: pass/fail rubric
    MaxRetries int      // 0 = use default
}
```

New file `internal/persona/load.go`:

- `DiscoverDirs(dataDir, projectRoot) []string` → `<dataDir>/personas`,
  `<projectRoot>/.aegis/personas` (project overrides user).
- `LoadFromDirs(dirs ...string) int` scans `*.md` files, parses markdown with
  YAML frontmatter, and registers each into the package registry (file personas
  add to or override built-ins). Returns the count loaded.
- Frontmatter is parsed with koanf's YAML parser (already a direct dependency).
  The body (after the closing `---`) becomes `System`.
- `output_guard` frontmatter accepts either the scalar `none`/`false` (→
  `Guard{Disabled:true}`) or a map (`mode`, `schema`, `rubric`, `max_retries`).

Frontmatter fields: `description`, `model`, `mode`, `tools`, `rules`,
`output_guard`. Unknown keys are ignored. A file with no `name` uses the
filename stem.

Example `.aegis/personas/secure-reviewer.md`:

```markdown
---
description: Strict secure code reviewer
model: claude-opus-4-8
mode: build
tools: [read_file, grep, shell, web_search]
rules:
  - "deny write(*)"
  - "allow shell(git diff*)"
output_guard:
  mode: llm
  rubric: "Every finding must cite a file:line and a concrete remediation."
  max_retries: 2
---
You are a strict secure code reviewer. ...
```

The server already calls `agentdef.LoadFromDirs` at startup; it will likewise
call `persona.LoadFromDirs(persona.DiscoverDirs(cfg.DataDir, cwd)...)`.

**Model overrides via config (preferred for built-ins).** Built-in personas have
no file, so their model is overridden from the config file rather than by writing
a persona file. The config gains a `personas:` map keyed by persona name; the
`aegis init` template lists every built-in with a **blank** `model` and a
commented recommendation, so the global `provider.model` is used by default and a
user changes a persona's model by editing one line:

```yaml
personas:
  # Leave model blank to use the global provider.model. Recommendations are
  # guidance only — set what your provider serves.
  security-architect: { model: "" }   # rec: claude-opus-4-8 — deep threat-model reasoning
  appsec-engineer:    { model: "" }   # rec: claude-opus-4-8 — careful code review
  report-writer:      { model: "" }   # rec: claude-sonnet-4-6 — strong long-form writing
  data-analyst:       { model: "" }   # rec: claude-opus-4-8
  developer:          { model: "" }   # rec: claude-sonnet-4-6 — fast iteration
  general:            { model: "" }
  # … all built-ins listed, all blank by default …
```

**Effective model precedence** (first non-empty wins):
`config personas[name].model` → persona file/struct `Model` → global
`provider.model`.

### 2. `internal/guard` — output validators (P2.4)

A small, dependency-free package decoupled from the engine:

```go
// Func validates a final answer. ok=false means the output failed; reason is a
// short human/LLM-readable explanation appended to the retry prompt.
type Func func(ctx context.Context, text string) (ok bool, reason string)

// SchemaGuard requires text to parse as a JSON object containing every key in
// required. No external schema library — valid JSON + required-key presence.
func SchemaGuard(required []string) Func

// LLMGuard asks model (via adapter) whether text satisfies rubric, expecting a
// reply of "PASS" or "FAIL: <reason>". A malformed/empty reply is treated as a
// pass (fail-open) so a flaky validator never blocks the user's answer.
func LLMGuard(adapter provider.Adapter, model, rubric string) Func
```

`internal/persona` (or the server) resolves a `GuardConfig` + global default into
a concrete `Func` and retry count. The resolver:

- returns `(nil, 0)` when guards are disabled (persona `Disabled`, or the
  per-session/global toggle is off);
- builds `SchemaGuard` or `LLMGuard` per `Mode`;
- for `llm`, uses `provider.SmallModel` when set (cheap), else `provider.Model`;
  if no model/adapter is available, returns `(nil, 0)` (guard skipped, logged).

### 3. `internal/engine` — guard seam (P2.4)

Additions to `Options` and `Engine`:

```go
OutputGuard           func(ctx, text string) (ok bool, reason string)
OutputGuardMaxRetries int // default 1 when a guard is set
```

New event:

```go
KindGuard EventKind = "guard"
// Event gains: GuardReason string, GuardPassed bool
```

In `Run`, at the existing final-answer point (`len(toolUses) == 0` and
`stopReason != StopMaxTokens`):

1. If no guard, or retries exhausted → emit `KindDone`, return (current
   behaviour).
2. Otherwise extract the final text (concatenated `TextBlock`s of the assistant
   message). If empty → treat as pass.
3. Call the guard. On **pass** → `KindDone`, return.
4. On **fail** with retries remaining → emit `KindGuard{GuardPassed:false,
   GuardReason:reason}`, append a corrective user message
   (`[Your previous response did not pass output validation: <reason>. Revise and
   produce a corrected final answer.]`), increment the retry counter, `continue`
   the loop.
5. On **fail** with retries exhausted → emit `KindGuard{GuardPassed:false,
   GuardReason: "...surfacing raw output after N retries"}`, then `KindDone`,
   return (never trap the user — always surface something).

The guard never runs on intermediate (tool-using) turns or on the
max-tokens-continuation path. The retry budget is independent of the
loop/iteration cap.

### 4. Session plumbing (P3.3 overrides + toggle)

- **Schema:** add a `persona` column to the `sessions` table (idempotent
  `ALTER TABLE`, matching the existing token/cost migration pattern). Add
  `Persona string` to `session.Session`/`Meta`; `Store.Create` takes a persona
  name and persists it.
- **Creation:** `handleCreateSession` stores `req.Persona`. If `req.Mode` is
  empty and the persona declares a `Mode`, the persona's mode seeds the session.
- **Message time:** `handlePostMessage` resolves `persona.Get(sess.Persona)` and
  passes it to `newEngine`, which applies:
  - **model:** effective model = first non-empty of
    `cfg.Personas[name].Model`, `persona.Model`, `cfg.Provider.Model`; set
    `engine.Options.Model` to it (same adapter, different model id);
  - **rules:** `append(globalRules, parsedPersonaRules...)` → the existing
    `permission.RuleGate` (outermost gate from the P2.2 work);
  - **output_guard:** resolve `GuardConfig` + global default + the session toggle
    into `OutputGuard`/`OutputGuardMaxRetries`.
- **Toggle:** the server holds `guardOverrides sync.Map` (session ID → bool).
  `newEngine` consults it (falling back to `cfg.OutputGuard.Enabled`) to decide
  whether to wire the guard. A new `/guard [on|off|status]` command updates the
  map for the current session and reports state. It mirrors how `/mode` is
  handled. The override is in-memory and resets to the config default on restart.

### 5. Configuration

New global section, with built-in defaults so the feature is on out of the box:

```yaml
output_guard:
  enabled: true        # per-session /guard toggles from this default
  mode: llm            # default guard mode for personas without their own
  max_retries: 1
  rubric: |            # generic default; personas may override
    The response must directly and completely address the request, contain no
    placeholders or TODOs, and ground factual claims in tool output where
    applicable.
```

`config.OutputGuardConfig{Enabled bool; Mode string; Rubric string; MaxRetries int}`.

Per-persona overrides (model now; struct kept extensible):

```go
type PersonaOverride struct {
    Model string `koanf:"model"` // "" = use global provider.model
}
// Config.Personas map[string]PersonaOverride `koanf:"personas"`
```

The `aegis init` template documents both `output_guard` and a `personas:` map —
every built-in persona listed with a blank `model` and a commented recommended
model — so the global model is used by default and editing one line changes a
persona's model. The README gains a section covering persona templates, the
config map, output guards, and `/guard`.

### 6. TUI surfacing (P2.4)

- The engine `KindGuard` event is forwarded by the server as an SSE event
  (`api.KindGuard`), carrying the reason and pass/fail.
- The TUI renders a dim warning line in the transcript:
  `⚠ output guard: <reason> — retrying` on a retry, and
  `⚠ output guard failed after N retries; showing the response anyway` on
  exhaustion.
- `/guard` prints its status line inline like other meta commands.

---

## Data flow (one user turn, guard on)

```
user message
   └─ handlePostMessage
        ├─ resolve persona (model, mode, rules, guard)
        ├─ newEngine(mode, persona, toggle)
        │     ├─ Model    = persona.Model or global
        │     ├─ RuleGate = global + persona rules
        │     └─ OutputGuard = resolve(persona.Guard, global default, toggle)
        └─ engine.Run
              ├─ … tool rounds …
              └─ final answer
                    ├─ guard.Check(text)
                    │     ├─ pass → KindDone
                    │     └─ fail → KindGuard + corrective msg + retry (≤N)
                    │                  └─ exhausted → KindGuard(warn) → KindDone
                    └─ SSE → TUI (warning line on KindGuard)
```

---

## Testing

- **persona/load:** frontmatter parsing (all fields; `output_guard: none`; nested
  map; missing name → filename; project overrides user); registry integration.
- **guard:** `SchemaGuard` (valid JSON + required keys present/absent; non-JSON
  fails); `LLMGuard` against a fake adapter returning `PASS` / `FAIL: x` /
  malformed (fail-open).
- **engine:** guard pass → single `KindDone`; guard fail then pass → one
  `KindGuard` + corrective message + final `KindDone`; persistent fail →
  `MaxRetries` `KindGuard` events then `KindDone` with raw output; nil guard →
  unchanged behaviour. Use a scripted fake adapter.
- **config:** `output_guard` defaults present; YAML override respected.
- **server:** session persists/returns `persona`; `/guard off` disables the guard
  for the next run.

All via TDD (red → green), `go build ./...` and `go test ./...` green.

---

## Risks / considerations

- **Cost/latency:** a default-on `llm` guard adds a validator call (and up to
  `max_retries` corrective turns) per user turn. Mitigated by `SmallModel`,
  `max_retries: 1`, fail-open behaviour, and the per-session `/guard off`.
- **Core-loop change:** the engine seam is additive and gated on a non-nil guard;
  existing behaviour is untouched when no guard is configured.
- **Provider readiness:** if the model/adapter isn't available, the guard is
  skipped rather than erroring.
