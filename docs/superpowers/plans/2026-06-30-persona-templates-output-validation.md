# Persona Templates (P3.3) + Output Validation (P2.4) Implementation Plan

> **For agentic workers:** Implement this plan task-by-task. Each task ends with an independently testable deliverable. Steps use checkbox (`- [ ]`) syntax for tracking. Use TDD: write the failing test, watch it fail, implement minimally, watch it pass, commit.

**Goal:** Add shareable file-based persona templates (system, model, mode, tools, rules, output_guard) and a dual-layer output-validation guard that runs at the end of a session turn, on by default, with a per-session `/guard` toggle.

**Architecture:** A dependency-free `internal/guard` package provides validator funcs (schema + llm). The engine gains an optional `OutputGuard` callback invoked at the final-answer point, with retry. `internal/persona` gains override fields and a markdown-frontmatter file loader. Config gains an `output_guard` default and a `personas` model-override map. The server resolves a session's persona into model/rules/guard at run time and forwards a new `KindGuard` event to the TUI.

**Tech Stack:** Go 1.x, SQLite (`modernc.org/sqlite`), koanf config, `go.yaml.in/yaml/v3` for frontmatter, lipgloss/bubbletea TUI. No new third-party runtime deps beyond promoting the already-vendored yaml module to direct.

## Global Constraints

- Module path: `github.com/scottymacleod/aegis`. All imports use this prefix.
- TDD throughout: `go build ./...` and `go test ./...` must be green at every commit.
- Guards must **fail open**: any validator error, missing model/adapter, or unparseable verdict returns `pass` — never trap the user's answer.
- Output guard is **on by default** (`output_guard.enabled: true`), applies a generic rubric to every persona unless overridden/disabled, and is toggleable per session at runtime.
- Model override is **same-provider only** (model id string); built-in personas leave `Model` empty so behaviour is unchanged by default.
- Effective model precedence (first non-empty): `cfg.Personas[name].Model` → `persona.Model` → `cfg.Provider.Model`.
- Do not apply output guards to sub-agents (`agentdef`); top-level sessions only.

---

### Task 1: `internal/guard` package — validators + resolver

**Files:**
- Create: `internal/guard/guard.go`
- Test: `internal/guard/guard_test.go`

**Interfaces:**
- Produces:
  - `type Func func(ctx context.Context, text string) (ok bool, reason string)`
  - `type Config struct { Disabled bool; Mode string; Schema []string; Rubric string; MaxRetries int }`
  - `func SchemaGuard(required []string) Func`
  - `func LLMGuard(adapter provider.Adapter, model, rubric string) Func`
  - `func Resolve(c Config, adapter provider.Adapter, model string) (Func, int)`

- [ ] **Step 1: Write the failing test**

```go
package guard

import (
	"context"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
)

// fakeAdapter returns a fixed text response, ignoring the request.
type fakeAdapter struct{ reply string }

func (f fakeAdapter) Name() string { return "fake" }
func (f fakeAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	ch := make(chan provider.Event, 2)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: f.reply}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn}
	close(ch)
	return ch, nil
}

func TestSchemaGuard(t *testing.T) {
	g := SchemaGuard([]string{"findings", "summary"})
	if ok, _ := g(context.Background(), `{"findings":[],"summary":"x"}`); !ok {
		t.Error("valid object with required keys should pass")
	}
	if ok, reason := g(context.Background(), `{"summary":"x"}`); ok || reason == "" {
		t.Errorf("missing key should fail with reason, got ok=%v reason=%q", ok, reason)
	}
	if ok, _ := g(context.Background(), `not json`); ok {
		t.Error("non-JSON should fail")
	}
	// Fenced JSON is tolerated.
	if ok, _ := g(context.Background(), "```json\n{\"findings\":1,\"summary\":2}\n```"); !ok {
		t.Error("fenced JSON should pass")
	}
}

func TestLLMGuardPassFail(t *testing.T) {
	pass := LLMGuard(fakeAdapter{reply: "PASS"}, "m", "rubric")
	if ok, _ := pass(context.Background(), "answer"); !ok {
		t.Error("PASS verdict should pass")
	}
	fail := LLMGuard(fakeAdapter{reply: "FAIL: missing citations"}, "m", "rubric")
	if ok, reason := fail(context.Background(), "answer"); ok || reason != "missing citations" {
		t.Errorf("FAIL verdict should fail with reason, got ok=%v reason=%q", ok, reason)
	}
	// Unparseable verdict fails open.
	weird := LLMGuard(fakeAdapter{reply: "I think maybe"}, "m", "rubric")
	if ok, _ := weird(context.Background(), "answer"); !ok {
		t.Error("unparseable verdict should fail open (pass)")
	}
}

func TestResolve(t *testing.T) {
	if g, _ := Resolve(Config{Disabled: true}, nil, ""); g != nil {
		t.Error("disabled config returns nil guard")
	}
	if g, n := Resolve(Config{Mode: "schema", Schema: []string{"a"}}, nil, ""); g == nil || n != 1 {
		t.Errorf("schema mode returns guard + default retries 1, got g=%v n=%d", g != nil, n)
	}
	if g, _ := Resolve(Config{Mode: "llm", Rubric: "r"}, nil, ""); g != nil {
		t.Error("llm mode with no adapter/model returns nil (skipped)")
	}
	if g, n := Resolve(Config{Mode: "llm", Rubric: "r", MaxRetries: 3}, fakeAdapter{reply: "PASS"}, "m"); g == nil || n != 3 {
		t.Errorf("llm mode with adapter returns guard + retries 3, got g=%v n=%d", g != nil, n)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/guard/`
Expected: FAIL — build error, `undefined: SchemaGuard`, etc.

- [ ] **Step 3: Write minimal implementation**

```go
// Package guard validates a persona's final answer before it is returned to the
// user. Two modes: a deterministic JSON schema check and an LLM rubric check.
// Guards always fail open — any internal error yields a pass so a flaky
// validator never blocks the user's answer.
package guard

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/scottymacleod/aegis/internal/provider"
)

// Func validates a final answer. ok=false means it failed; reason is a short
// explanation appended to the corrective retry prompt.
type Func func(ctx context.Context, text string) (ok bool, reason string)

// Config is the resolved guard configuration for a single persona/session.
type Config struct {
	Disabled   bool
	Mode       string   // "schema" | "llm"
	Schema     []string // schema mode: required top-level JSON keys
	Rubric     string   // llm mode: pass/fail rubric
	MaxRetries int
}

// SchemaGuard requires text to parse as a JSON object containing every required
// key. A leading ```json fence is tolerated.
func SchemaGuard(required []string) Func {
	return func(_ context.Context, text string) (bool, string) {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stripFence(text)), &obj); err != nil {
			return false, "output is not a valid JSON object"
		}
		var missing []string
		for _, k := range required {
			if _, ok := obj[k]; !ok {
				missing = append(missing, k)
			}
		}
		if len(missing) > 0 {
			return false, "missing required keys: " + strings.Join(missing, ", ")
		}
		return true, ""
	}
}

// LLMGuard asks model whether text satisfies rubric, expecting "PASS" or
// "FAIL: <reason>". Any error or unparseable reply fails open.
func LLMGuard(adapter provider.Adapter, model, rubric string) Func {
	return func(ctx context.Context, text string) (bool, string) {
		if adapter == nil || model == "" {
			return true, ""
		}
		prompt := "You are an output validator. Given the RUBRIC and the OUTPUT, reply with exactly " +
			"\"PASS\" if the output satisfies the rubric, or \"FAIL: <one-line reason>\" if it does not. " +
			"Reply with nothing else.\n\nRUBRIC:\n" + rubric + "\n\nOUTPUT:\n" + text
		cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		defer cancel()
		ch, err := adapter.Stream(cctx, provider.Request{
			Model:     model,
			MaxTokens: 256,
			Messages: []provider.Message{
				{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: prompt}}},
			},
		})
		if err != nil {
			return true, ""
		}
		var sb strings.Builder
		for ev := range ch {
			if ev.Type == provider.EventTextDelta {
				sb.WriteString(ev.Text)
			}
		}
		return parseVerdict(sb.String())
	}
}

// Resolve builds a concrete guard and its retry count from a Config. Returns
// (nil, 0) when guards are disabled or an llm guard lacks the model/adapter it
// needs (skipped, fail open).
func Resolve(c Config, adapter provider.Adapter, model string) (Func, int) {
	if c.Disabled {
		return nil, 0
	}
	retries := c.MaxRetries
	if retries <= 0 {
		retries = 1
	}
	switch c.Mode {
	case "schema":
		return SchemaGuard(c.Schema), retries
	case "llm":
		if adapter == nil || model == "" || strings.TrimSpace(c.Rubric) == "" {
			return nil, 0
		}
		return LLMGuard(adapter, model, c.Rubric), retries
	default:
		return nil, 0
	}
}

func parseVerdict(s string) (bool, string) {
	s = strings.TrimSpace(stripThink(s))
	upper := strings.ToUpper(s)
	if strings.HasPrefix(upper, "PASS") {
		return true, ""
	}
	if i := strings.Index(upper, "FAIL"); i >= 0 {
		reason := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s[i+4:]), ":"))
		if reason == "" {
			reason = "output did not satisfy the rubric"
		}
		return false, reason
	}
	return true, "" // unparseable → fail open
}

// stripFence removes a single ```lang … ``` code fence if present.
func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	if nl := strings.IndexByte(s, '\n'); nl >= 0 {
		s = s[nl+1:]
	}
	return strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(s), "```"))
}

// stripThink removes <think>…</think> reasoning blocks from a validator reply.
func stripThink(s string) string {
	for {
		start := strings.Index(s, "<think>")
		if start < 0 {
			return s
		}
		end := strings.Index(s[start:], "</think>")
		if end < 0 {
			return strings.TrimSpace(s[:start])
		}
		s = s[:start] + s[start+end+len("</think>"):]
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/guard/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/guard/
git commit -m "feat(guard): add output validation package (schema + llm modes)"
```

---

### Task 2: Engine guard seam

**Files:**
- Modify: `internal/engine/engine.go` (Event struct ~46-57; EventKind consts ~33-43; Options ~90-106; Engine struct ~109-125; New ~154-170; Run final-answer block ~238-251)
- Test: `internal/engine/guard_test.go` (create)

**Interfaces:**
- Consumes: `engine.Options`, `engine.Event`, `engine.Run`.
- Produces: `Options.OutputGuard func(ctx context.Context, text string) (bool, string)`, `Options.OutputGuardMaxRetries int`, `EventKind` value `KindGuard`, `Event.GuardReason string`, `Event.GuardPassed bool`.

- [ ] **Step 1: Write the failing test**

```go
package engine

import (
	"context"
	"testing"

	"github.com/scottymacleod/aegis/internal/provider"
)

// scriptAdapter returns one text response per Stream call, in order.
type scriptAdapter struct {
	replies []string
	i       int
}

func (a *scriptAdapter) Name() string { return "script" }
func (a *scriptAdapter) Stream(ctx context.Context, req provider.Request) (<-chan provider.Event, error) {
	text := "done"
	if a.i < len(a.replies) {
		text = a.replies[a.i]
	}
	a.i++
	ch := make(chan provider.Event, 3)
	ch <- provider.Event{Type: provider.EventTextDelta, Text: text}
	ch <- provider.Event{Type: provider.EventDone, Stop: provider.StopEndTurn, Usage: &provider.Usage{IsEstimated: true}}
	close(ch)
	return ch, nil
}

func runWith(t *testing.T, opts Options) []Event {
	t.Helper()
	eng, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	conv := &Conversation{Messages: []provider.Message{
		{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: "hi"}}},
	}}
	var got []Event
	if err := eng.Run(context.Background(), conv, func(ev Event) { got = append(got, ev) }); err != nil {
		t.Fatalf("run: %v", err)
	}
	return got
}

func kinds(evs []Event) []EventKind {
	var k []EventKind
	for _, e := range evs {
		k = append(k, e.Kind)
	}
	return k
}

func TestGuardPassEmitsDoneOnly(t *testing.T) {
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"final"}}, Model: "m",
		OutputGuard:           func(context.Context, string) (bool, string) { return true, "" },
		OutputGuardMaxRetries: 2,
	})
	for _, k := range kinds(evs) {
		if k == KindGuard {
			t.Error("passing guard should emit no KindGuard event")
		}
	}
}

func TestGuardFailThenPass(t *testing.T) {
	calls := 0
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"bad", "good"}}, Model: "m",
		OutputGuardMaxRetries: 2,
		OutputGuard: func(_ context.Context, text string) (bool, string) {
			calls++
			if text == "good" {
				return true, ""
			}
			return false, "needs work"
		},
	})
	var guardEvents int
	for _, e := range evs {
		if e.Kind == KindGuard {
			guardEvents++
			if e.GuardReason != "needs work" {
				t.Errorf("guard reason = %q", e.GuardReason)
			}
		}
	}
	if guardEvents != 1 {
		t.Errorf("expected 1 KindGuard event, got %d", guardEvents)
	}
	if calls != 2 {
		t.Errorf("expected guard called twice, got %d", calls)
	}
}

func TestGuardExhaustedSurfaces(t *testing.T) {
	evs := runWith(t, Options{
		Adapter: &scriptAdapter{replies: []string{"a", "b", "c", "d"}}, Model: "m",
		OutputGuardMaxRetries: 2,
		OutputGuard:           func(context.Context, string) (bool, string) { return false, "always bad" },
	})
	var guardEvents, doneEvents int
	for _, e := range evs {
		switch e.Kind {
		case KindGuard:
			guardEvents++
		case KindDone:
			doneEvents++
		}
	}
	// 2 retries => 2 failure events on retries + 1 final exhausted event = 3.
	if guardEvents != 3 {
		t.Errorf("expected 3 KindGuard events, got %d", guardEvents)
	}
	if doneEvents != 1 {
		t.Errorf("expected exactly 1 KindDone, got %d", doneEvents)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/engine/ -run TestGuard`
Expected: FAIL — `unknown field OutputGuard`, `undefined: KindGuard`.

- [ ] **Step 3: Add the event kind, event fields, and options**

In `internal/engine/engine.go`, add to the EventKind const block (after `KindSteer`):

```go
	KindGuard      EventKind = "guard"       // output validation result (warning)
```

Add to the `Event` struct (after `Err error`):

```go
	GuardReason string // KindGuard: why validation failed
	GuardPassed bool   // KindGuard: whether the guard ultimately passed
```

Add to `Options` (after `PrepareStep`):

```go
	OutputGuard           func(ctx context.Context, text string) (ok bool, reason string) // optional; validates the final answer
	OutputGuardMaxRetries int                                                              // corrective retries on guard failure; 0 -> 1 when a guard is set
```

Add to `Engine` struct (after `prepareStep`):

```go
	outputGuard      func(ctx context.Context, text string) (bool, string)
	outputGuardMax   int
```

In `New`, after `prepareStep: opts.PrepareStep,` add:

```go
		outputGuard:    opts.OutputGuard,
		outputGuardMax: opts.OutputGuardMaxRetries,
```

- [ ] **Step 4: Integrate the guard into the run loop**

In `internal/engine/engine.go`, declare a retry counter before the loop (next to `var loop *loopDetector`):

```go
	guardRetries := 0
```

Replace the final-answer block (currently emits `KindDone` when `len(toolUses) == 0` and `stopReason != StopMaxTokens`). The existing code is:

```go
			emit(Event{Kind: KindDone})
			return nil
```

Replace those two lines with:

```go
			if e.outputGuard != nil {
				maxRetries := e.outputGuardMax
				if maxRetries <= 0 {
					maxRetries = 1
				}
				if final := assistantText(assistant); final != "" {
					ok, reason := e.outputGuard(ctx, final)
					if !ok && guardRetries < maxRetries {
						guardRetries++
						emit(Event{Kind: KindGuard, GuardPassed: false, GuardReason: reason})
						conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{
							provider.TextBlock{Text: "[Your previous response did not pass output validation: " + reason +
								". Revise and produce a corrected final answer.]"},
						}})
						continue
					}
					if !ok {
						emit(Event{Kind: KindGuard, GuardPassed: false,
							GuardReason: "surfacing the response after " + itoa(maxRetries) + " failed validation attempt(s): " + reason})
					}
				}
			}
			emit(Event{Kind: KindDone})
			return nil
```

Add two helpers at the end of `internal/engine/engine.go`:

```go
// assistantText concatenates the text blocks of an assistant message.
func assistantText(m provider.Message) string {
	var sb strings.Builder
	for _, b := range m.Content {
		if t, ok := b.(provider.TextBlock); ok {
			sb.WriteString(t.Text)
		}
	}
	return strings.TrimSpace(sb.String())
}

func itoa(n int) string { return strconv.Itoa(n) }
```

Ensure `strings` and `strconv` are imported in `engine.go` (add `strconv` if absent).

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/engine/`
Expected: PASS (new guard tests and all existing engine tests).

- [ ] **Step 6: Commit**

```bash
git add internal/engine/
git commit -m "feat(engine): output guard seam with corrective retry + KindGuard event"
```

---

### Task 3: Persona override fields + file loader

**Files:**
- Modify: `internal/persona/persona.go` (Persona struct ~12-16; add `GuardConfig` type)
- Create: `internal/persona/load.go`
- Test: `internal/persona/load_test.go`

**Interfaces:**
- Produces:
  - `Persona` gains `Model string`, `Mode string`, `Tools []string`, `Rules []string`, `Guard *GuardConfig`.
  - `type GuardConfig struct { Disabled bool; Mode string; Schema []string; Rubric string; MaxRetries int }`
  - `func DiscoverDirs(dataDir, projectRoot string) []string`
  - `func LoadFromDirs(dirs ...string) int`
  - `func register(p Persona)` (internal helper used by the loader; file personas override built-ins)

- [ ] **Step 1: Write the failing test**

```go
package persona

import (
	"os"
	"path/filepath"
	"testing"
)

func writePersona(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadFromDirsRichFrontmatter(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "secure-reviewer.md", `---
description: Strict secure code reviewer
model: claude-opus-4-8
mode: build
tools: [read_file, grep, shell]
rules:
  - "deny write(*)"
  - "allow shell(git diff*)"
output_guard:
  mode: llm
  rubric: "Every finding cites file:line."
  max_retries: 2
---
You are a strict secure code reviewer.`)

	n := LoadFromDirs(dir)
	if n != 1 {
		t.Fatalf("expected 1 persona loaded, got %d", n)
	}
	p, ok := Get("secure-reviewer")
	if !ok {
		t.Fatal("persona not registered")
	}
	if p.Model != "claude-opus-4-8" || p.Mode != "build" {
		t.Errorf("model/mode = %q/%q", p.Model, p.Mode)
	}
	if len(p.Tools) != 3 || len(p.Rules) != 2 {
		t.Errorf("tools=%v rules=%v", p.Tools, p.Rules)
	}
	if p.Guard == nil || p.Guard.Mode != "llm" || p.Guard.Rubric == "" || p.Guard.MaxRetries != 2 {
		t.Errorf("guard = %+v", p.Guard)
	}
	if p.System != "You are a strict secure code reviewer." {
		t.Errorf("system = %q", p.System)
	}
}

func TestLoadGuardDisabledScalar(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "fast.md", "---\noutput_guard: none\n---\nBody.")
	LoadFromDirs(dir)
	p, _ := Get("fast")
	if p.Guard == nil || !p.Guard.Disabled {
		t.Errorf("expected disabled guard, got %+v", p.Guard)
	}
}

func TestLoadNameFromFilename(t *testing.T) {
	dir := t.TempDir()
	writePersona(t, dir, "my-helper.md", "---\ndescription: x\n---\nBody.")
	LoadFromDirs(dir)
	if _, ok := Get("my-helper"); !ok {
		t.Error("persona should be registered under its filename stem")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/persona/ -run TestLoad`
Expected: FAIL — `undefined: LoadFromDirs`, `p.Model undefined`.

- [ ] **Step 3: Extend the Persona struct and add GuardConfig**

In `internal/persona/persona.go`, replace the `Persona` struct with:

```go
// Persona is a named behavioral profile. The override fields are populated by
// file-loaded personas; built-in personas leave them zero-valued.
type Persona struct {
	Name        string
	Description string
	System      string
	Model       string       // model id override (same provider); "" = global
	Mode        string       // permission mode; "" = session/config default
	Tools       []string     // allowed tools (parsed; enforcement deferred)
	Rules       []string     // permission rules merged into the session gate
	Guard       *GuardConfig // nil = global default; Disabled = no guard
}

// GuardConfig is a persona's output-validation override parsed from frontmatter.
type GuardConfig struct {
	Disabled   bool
	Mode       string   // "schema" | "llm"
	Schema     []string // schema mode: required top-level JSON keys
	Rubric     string   // llm mode rubric
	MaxRetries int
}
```

- [ ] **Step 4: Write the loader**

Create `internal/persona/load.go`:

```go
package persona

import (
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

// DiscoverDirs returns the standard directories searched for persona files:
// user-global first, project-local second (project overrides global).
func DiscoverDirs(dataDir, projectRoot string) []string {
	return []string{
		filepath.Join(dataDir, "personas"),
		filepath.Join(projectRoot, ".aegis", "personas"),
	}
}

// LoadFromDirs scans directories for *.md persona files and registers each.
// Later directories override earlier ones, and file personas override built-ins
// of the same name. Returns the number of personas loaded.
func LoadFromDirs(dirs ...string) int {
	count := 0
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
				continue
			}
			p, err := parsePersonaFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			register(p)
			count++
		}
	}
	return count
}

// register adds or overrides a persona in the registry and Names list.
func register(p Persona) {
	if _, exists := registry[p.Name]; !exists {
		nameOrder = append(nameOrder, p.Name)
	}
	registry[p.Name] = p
}

type frontmatter struct {
	Description string    `yaml:"description"`
	Model       string    `yaml:"model"`
	Mode        string    `yaml:"mode"`
	Tools       []string  `yaml:"tools"`
	Rules       []string  `yaml:"rules"`
	OutputGuard yaml.Node `yaml:"output_guard"`
}

func parsePersonaFile(path string) (Persona, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Persona{}, err
	}
	fmText, body := splitFrontmatter(string(data))
	var fm frontmatter
	if fmText != "" {
		if err := yaml.Unmarshal([]byte(fmText), &fm); err != nil {
			return Persona{}, err
		}
	}
	p := Persona{
		Name:        strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)),
		Description: fm.Description,
		System:      strings.TrimSpace(body),
		Model:       fm.Model,
		Mode:        fm.Mode,
		Tools:       fm.Tools,
		Rules:       fm.Rules,
		Guard:       parseGuard(fm.OutputGuard),
	}
	return p, nil
}

// parseGuard interprets the output_guard node: a scalar "none"/"false" disables
// the guard; a mapping is decoded into a GuardConfig; absence returns nil.
func parseGuard(n yaml.Node) *GuardConfig {
	switch n.Kind {
	case 0: // absent
		return nil
	case yaml.ScalarNode:
		switch strings.ToLower(strings.TrimSpace(n.Value)) {
		case "none", "false", "off", "disabled":
			return &GuardConfig{Disabled: true}
		}
		return nil
	case yaml.MappingNode:
		var g struct {
			Mode       string   `yaml:"mode"`
			Schema     []string `yaml:"schema"`
			Rubric     string   `yaml:"rubric"`
			MaxRetries int      `yaml:"max_retries"`
		}
		if err := n.Decode(&g); err != nil {
			return nil
		}
		return &GuardConfig{Mode: g.Mode, Schema: g.Schema, Rubric: g.Rubric, MaxRetries: g.MaxRetries}
	}
	return nil
}

// splitFrontmatter splits a markdown file into its YAML frontmatter and body.
// Returns ("", whole) when there is no leading --- block.
func splitFrontmatter(content string) (fm, body string) {
	if !strings.HasPrefix(content, "---") {
		return "", content
	}
	rest := content[3:]
	idx := strings.Index(rest, "\n---")
	if idx < 0 {
		return "", content
	}
	fm = strings.TrimSpace(rest[:idx])
	body = rest[idx+len("\n---"):]
	if nl := strings.IndexByte(body, '\n'); nl >= 0 {
		body = body[nl+1:]
	}
	return fm, body
}
```

- [ ] **Step 5: Convert the Names list to a mutable order slice**

The loader appends to `nameOrder`. In `internal/persona/persona.go`, replace the hard-coded `Names()` (the `return []string{...}` block ~708-727) with a package-level slice and accessor:

```go
// nameOrder preserves registration order: built-ins first, then file personas.
var nameOrder = []string{
	"general", "security", "platform-architect", "security-architect",
	"security-engineer", "appsec-engineer", "developer", "security-researcher",
	"risk-assessor", "business-analyst", "data-analyst",
	"network-security-architect", "report-writer", "sre",
	"infrastructure-architect", "cloud-architect", "cloud-security-engineer",
}

// Names returns the available persona names in registration order.
func Names() []string {
	out := make([]string, len(nameOrder))
	copy(out, nameOrder)
	return out
}
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./internal/persona/`
Expected: PASS. Then `go mod tidy` to promote the yaml module to a direct dependency.

Run: `go build ./...`
Expected: builds clean.

- [ ] **Step 7: Commit**

```bash
git add internal/persona/ go.mod go.sum
git commit -m "feat(persona): file-loaded persona templates with model/mode/tools/rules/guard"
```

---

### Task 4: Config — output_guard defaults + personas model map

**Files:**
- Modify: `internal/config/config.go` (Config struct ~23-37; add types; `defaults()` ~133-156; env `sections` map ~254-258)
- Test: `internal/config/config_test.go` (add cases)

**Interfaces:**
- Produces:
  - `Config.OutputGuard OutputGuardConfig` (`koanf:"output_guard"`)
  - `Config.Personas map[string]PersonaOverride` (`koanf:"personas"`)
  - `type OutputGuardConfig struct { Enabled bool; Mode string; Rubric string; MaxRetries int }`
  - `type PersonaOverride struct { Model string }`
  - `const DefaultGuardRubric string`

- [ ] **Step 1: Write the failing test**

Add to `internal/config/config_test.go`:

```go
func TestOutputGuardDefaults(t *testing.T) {
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.OutputGuard.Enabled {
		t.Error("output_guard.enabled should default to true")
	}
	if cfg.OutputGuard.Mode != "llm" {
		t.Errorf("default mode = %q, want llm", cfg.OutputGuard.Mode)
	}
	if cfg.OutputGuard.MaxRetries != 1 {
		t.Errorf("default max_retries = %d, want 1", cfg.OutputGuard.MaxRetries)
	}
	if cfg.OutputGuard.Rubric == "" {
		t.Error("default rubric should be set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/config/ -run TestOutputGuard`
Expected: FAIL — `cfg.OutputGuard undefined`.

- [ ] **Step 3: Add the config types and defaults**

In `internal/config/config.go`, add to the `Config` struct (after `Security`):

```go
	OutputGuard OutputGuardConfig          `koanf:"output_guard"`
	Personas    map[string]PersonaOverride `koanf:"personas"`
```

Add the types (near `SecurityConfig`):

```go
// OutputGuardConfig sets the default output-validation behaviour applied to
// every persona unless the persona overrides or disables it.
type OutputGuardConfig struct {
	Enabled    bool   `koanf:"enabled"`     // global default; per-session /guard toggles from this
	Mode       string `koanf:"mode"`        // "llm" (default) or "schema"
	Rubric     string `koanf:"rubric"`      // default llm rubric
	MaxRetries int    `koanf:"max_retries"` // corrective retries on failure
}

// PersonaOverride holds per-persona config overrides keyed by persona name.
type PersonaOverride struct {
	Model string `koanf:"model"` // "" = use global provider.model
}

// DefaultGuardRubric is the generic quality rubric applied when output guarding
// is on and a persona declares no rubric of its own.
const DefaultGuardRubric = "The response must directly and completely address the request, " +
	"contain no placeholders or TODOs, and ground factual claims in tool output where applicable."
```

In `defaults()`, add to the returned map (before the closing `}`):

```go
		"output_guard.enabled":     true,
		"output_guard.mode":        "llm",
		"output_guard.max_retries": 1,
		"output_guard.rubric":      DefaultGuardRubric,
```

In the env `sections` map, add `"output_guard": true,` so `AEGIS_OUTPUT_GUARD_ENABLED` maps correctly. (The `personas` map is config-file only; no env section needed.)

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./internal/config/`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/
git commit -m "feat(config): output_guard defaults + per-persona model override map"
```

---

### Task 5: Session — persona column

**Files:**
- Modify: `internal/session/session.go` (Session ~21-33; Meta ~36-45; migrate ~82-109; Create ~111-129; Get SELECT/Scan ~132-160; List SELECT/Scan ~228-260)
- Test: `internal/session/session_test.go` (add case)

**Interfaces:**
- Produces: `Session.Persona string`, `Meta.Persona string`, `Store.Create(ctx, title, system, mode, persona string)`.
- Consumes: existing `Store` API.

- [ ] **Step 1: Write the failing test**

Add to `internal/session/session_test.go`:

```go
func TestCreatePersistsPersona(t *testing.T) {
	store := newTestStore(t) // existing helper in this test file
	s, err := store.Create(context.Background(), "t", "sys", "build", "security-architect")
	if err != nil {
		t.Fatal(err)
	}
	got, err := store.Get(context.Background(), s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Persona != "security-architect" {
		t.Errorf("persona = %q, want security-architect", got.Persona)
	}
}
```

> If the test file uses a different store-construction helper, match it; check the top of `session_test.go` for the existing pattern (e.g. `Open(filepath.Join(t.TempDir(), "s.db"))`).

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/session/ -run TestCreatePersistsPersona`
Expected: FAIL — `too many arguments in call to store.Create` / `got.Persona undefined`.

- [ ] **Step 3: Add the column and field**

In `migrate()`, add `persona TEXT NOT NULL DEFAULT ''` to the `CREATE TABLE` body (after `mode`), and append to the idempotent `ALTER TABLE` slice:

```go
		`ALTER TABLE sessions ADD COLUMN persona TEXT NOT NULL DEFAULT ''`,
```

Add `Persona string \`json:"persona"\`` to both `Session` (after `Mode`) and `Meta` (after `Mode`).

Update `Create`:

```go
func (s *Store) Create(ctx context.Context, title, system, mode, persona string) (*Session, error) {
	now := time.Now()
	sess := &Session{
		ID:        uuid.NewString(),
		Title:     title,
		System:    system,
		Mode:      mode,
		Persona:   persona,
		CreatedAt: now,
		UpdatedAt: now,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sessions (id, title, system, mode, persona, messages, created_at, updated_at) VALUES (?, ?, ?, ?, ?, '[]', ?, ?)`,
		sess.ID, sess.Title, sess.System, sess.Mode, sess.Persona, now.UnixMilli(), now.UnixMilli())
	if err != nil {
		return nil, fmt.Errorf("insert session: %w", err)
	}
	return sess, nil
}
```

Update `Get`'s SELECT to include `persona` and its `Scan` to read it:

```go
		`SELECT id, title, system, mode, persona, messages, traces, input_tokens, output_tokens, cost_usd, created_at, updated_at FROM sessions WHERE id = ?`, id)
```
```go
	if err := row.Scan(&sess.ID, &sess.Title, &sess.System, &sess.Mode, &sess.Persona, &msgBlob, &traceBlob,
		&sess.InputTokens, &sess.OutputTokens, &sess.CostUSD, &created, &upd); err != nil {
```

Update the List query (~line 231) SELECT to include `persona` and its row scan to read into `m.Persona`. Locate the `SELECT id, title, mode, input_tokens, ...` and the corresponding `rows.Scan(&m.ID, &m.Title, &m.Mode, ...)` and add `persona` / `&m.Persona` right after the `mode` / `&m.Mode` positions.

- [ ] **Step 4: Fix all Create callers**

Run `grep -rn "store.Create(\|\.Create(ctx" internal/ --include="*.go"` (and check `cmd/`). Update each non-test session `Create` call to pass a persona argument. Known call sites:
- `internal/server/server.go` `handleCreateSession` → pass `req.Persona` (handled fully in Task 6; for now pass `""` to keep the build green, Task 6 replaces it).
- Any test helpers calling `Create(...)` with 4 args → add a 5th `""` argument.

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./internal/session/ && go build ./...`
Expected: PASS and a clean build.

- [ ] **Step 6: Commit**

```bash
git add internal/session/ internal/server/server.go
git commit -m "feat(session): persist persona name on sessions"
```

---

### Task 6: Server wiring — persona overrides + guard resolution + toggle

**Files:**
- Modify: `internal/server/server.go` (imports; `New` startup loaders ~284-287; `newEngine` ~552-580; `handleCreateSession` ~620-642; `handlePostMessage` ~952; add helpers)
- Modify: `internal/api/api.go` (CreateSessionRequest already has Persona; PostMessageRequest add `GuardEnabled *bool`)
- Test: `internal/server/server_guard_test.go` (create)

**Interfaces:**
- Consumes: `persona.Get`, `persona.LoadFromDirs`, `persona.DiscoverDirs`, `guard.Resolve`, `guard.Config`, `permission.ParseRules`, `config.OutputGuardConfig`, `config.PersonaOverride`.
- Produces: `newEngine(mode string, approver permission.Approver, steerCh <-chan string, p persona.Persona, guardEnabled bool)`, helpers `personaModel`, `outputGuardConfig`.

- [ ] **Step 1: Write the failing test (guard resolution helper)**

Create `internal/server/server_guard_test.go`:

```go
package server

import (
	"testing"

	"github.com/scottymacleod/aegis/internal/config"
	"github.com/scottymacleod/aegis/internal/persona"
)

func TestOutputGuardConfigMerge(t *testing.T) {
	s := &Server{cfg: &config.Config{OutputGuard: config.OutputGuardConfig{
		Enabled: true, Mode: "llm", Rubric: "global", MaxRetries: 1,
	}}}

	// No persona override → global default.
	g := s.outputGuardConfig(persona.Persona{Name: "general"})
	if g.Mode != "llm" || g.Rubric != "global" {
		t.Errorf("default merge = %+v", g)
	}

	// Persona disables → Disabled.
	g = s.outputGuardConfig(persona.Persona{Name: "x", Guard: &persona.GuardConfig{Disabled: true}})
	if !g.Disabled {
		t.Error("persona disable should win")
	}

	// Persona overrides rubric + retries.
	g = s.outputGuardConfig(persona.Persona{Name: "x", Guard: &persona.GuardConfig{Rubric: "local", MaxRetries: 3}})
	if g.Rubric != "local" || g.MaxRetries != 3 || g.Mode != "llm" {
		t.Errorf("override merge = %+v", g)
	}
}

func TestPersonaModelPrecedence(t *testing.T) {
	s := &Server{cfg: &config.Config{
		Provider: config.ProviderConfig{Model: "global"},
		Personas: map[string]config.PersonaOverride{"pinned": {Model: "from-config"}},
	}}
	if m := s.personaModel(persona.Persona{Name: "pinned", Model: "from-file"}); m != "from-config" {
		t.Errorf("config override should win, got %q", m)
	}
	if m := s.personaModel(persona.Persona{Name: "other", Model: "from-file"}); m != "from-file" {
		t.Errorf("file model should win when no config override, got %q", m)
	}
	if m := s.personaModel(persona.Persona{Name: "plain"}); m != "global" {
		t.Errorf("global model fallback, got %q", m)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run 'TestOutputGuardConfigMerge|TestPersonaModelPrecedence'`
Expected: FAIL — `s.outputGuardConfig undefined`, `s.personaModel undefined`.

- [ ] **Step 3: Add the helpers**

Add to `internal/server/server.go` (near `newEngine`):

```go
// personaModel resolves the effective model for a persona: a config override
// wins, then the persona's own Model, then the global provider model.
func (s *Server) personaModel(p persona.Persona) string {
	if ov, ok := s.cfg.Personas[p.Name]; ok && ov.Model != "" {
		return ov.Model
	}
	if p.Model != "" {
		return p.Model
	}
	return s.cfg.Provider.Model
}

// outputGuardConfig merges the global output-guard default with a persona's
// override into a guard.Config.
func (s *Server) outputGuardConfig(p persona.Persona) guard.Config {
	c := guard.Config{
		Mode:       s.cfg.OutputGuard.Mode,
		Rubric:     s.cfg.OutputGuard.Rubric,
		MaxRetries: s.cfg.OutputGuard.MaxRetries,
	}
	if p.Guard != nil {
		if p.Guard.Disabled {
			return guard.Config{Disabled: true}
		}
		if p.Guard.Mode != "" {
			c.Mode = p.Guard.Mode
		}
		if len(p.Guard.Schema) > 0 {
			c.Schema = p.Guard.Schema
		}
		if p.Guard.Rubric != "" {
			c.Rubric = p.Guard.Rubric
		}
		if p.Guard.MaxRetries > 0 {
			c.MaxRetries = p.Guard.MaxRetries
		}
	}
	return c
}
```

Add the imports `"github.com/scottymacleod/aegis/internal/guard"` and confirm `"github.com/scottymacleod/aegis/internal/persona"` is present.

- [ ] **Step 4: Run helper tests to verify they pass**

Run: `go test ./internal/server/ -run 'TestOutputGuardConfigMerge|TestPersonaModelPrecedence'`
Expected: PASS

- [ ] **Step 5: Extend `newEngine` to apply persona overrides + guard**

Change the signature and body. Replace:

```go
func (s *Server) newEngine(mode string, approver permission.Approver, steerCh <-chan string) (*engine.Engine, error) {
```
with:
```go
func (s *Server) newEngine(mode string, approver permission.Approver, steerCh <-chan string, p persona.Persona, guardEnabled bool) (*engine.Engine, error) {
```

In the rules block, merge persona rules with the global ones:

```go
	rules := s.permRules
	if len(p.Rules) > 0 {
		if pr, err := permission.ParseRules(p.Rules); err == nil {
			rules = append(append([]permission.Rule{}, s.permRules...), pr...)
		} else {
			s.logger.Warn("ignoring invalid persona rules", "persona", p.Name, "err", err)
		}
	}
	if len(rules) > 0 {
		gate = permission.NewRuleGate(gate, rules,
			permission.WithRuleObserver(func(d permission.ContextualDecision) {
				if s.audit != nil {
					s.audit.PolicyDecision(d.Tool, d.Cap, d.Rule, string(d.Decision), d.Reason)
				}
			}))
	}
```

> This replaces the existing `if len(s.permRules) > 0 { gate = permission.NewRuleGate(gate, s.permRules, …) }` block.

In the `engine.New(engine.Options{...})` call, set the model and guard. Change `Model: s.cfg.Provider.Model,` to:

```go
		Model:         s.personaModel(p),
```

And add, before `Logger:`:

```go
		OutputGuard:           guardFn,
		OutputGuardMaxRetries: guardRetries,
```

Immediately before the `return engine.New(...)`, resolve the guard:

```go
	var guardFn func(ctx context.Context, text string) (bool, string)
	var guardRetries int
	if guardEnabled {
		guardFn, guardRetries = guard.Resolve(s.outputGuardConfig(p), s.adapter, s.personaModel(p))
	}
```

- [ ] **Step 6: Update `handleCreateSession` to store persona + seed mode**

In `handleCreateSession`, after resolving `system`, seed the mode from the persona when unset, and pass the persona to `Create`:

```go
	p, _ := persona.Get(req.Persona)
	if req.System == "" {
		system = p.System
	}
	if req.Mode == "" && p.Mode != "" {
		mode = p.Mode
	}
	sess, err := s.store.Create(r.Context(), req.Title, system, mode, req.Persona)
```

> Adjust the surrounding existing lines so `system`/`mode` resolution still works (the existing code resolves `system` from the persona only when `req.System == ""`). Keep that behaviour; just add the mode seed and the `Create` persona argument. Note the `mode != "plan"/"build"/"auto"` validation runs *before* this seed, so validate again or seed before validation — seed `mode` before the validation block.

- [ ] **Step 7: Resolve persona + toggle in `handlePostMessage`**

Add `GuardEnabled *bool \`json:"guard_enabled,omitempty"\`` to `api.PostMessageRequest` in `internal/api/api.go`.

In `handlePostMessage`, before the `s.newEngine(...)` call, resolve the persona and the toggle:

```go
	p, _ := persona.Get(sess.Persona)
	guardEnabled := s.cfg.OutputGuard.Enabled
	if req.GuardEnabled != nil {
		guardEnabled = *req.GuardEnabled
	}
```

Change:
```go
	eng, err := s.newEngine(sess.Mode, runApprover, steerCh)
```
to:
```go
	eng, err := s.newEngine(sess.Mode, runApprover, steerCh, p, guardEnabled)
```

- [ ] **Step 8: Load persona files at startup**

In `New`, alongside the existing `agentdef.LoadFromDirs(...)` call, add:

```go
	if n := persona.LoadFromDirs(persona.DiscoverDirs(cfg.DataDir, cwd)...); n > 0 {
		logger.Info("loaded custom personas", "count", n)
	}
```

- [ ] **Step 9: Fix any other `newEngine` callers**

Run `grep -rn "s.newEngine(\|\.newEngine(" internal/`. For any caller other than `handlePostMessage` (e.g. a dry-run or parallel path), pass `persona.Get("")` (the general persona) and `s.cfg.OutputGuard.Enabled`. If `handlePostMessage` is the only caller, nothing else to change.

- [ ] **Step 10: Run the full server suite**

Run: `go test ./internal/server/ && go build ./...`
Expected: PASS and clean build.

- [ ] **Step 11: Commit**

```bash
git add internal/server/ internal/api/
git commit -m "feat(server): apply persona model/rules/guard per session + guard toggle"
```

---

### Task 7: `/guard` toggle command + KindGuard surfacing in the TUI

**Files:**
- Modify: `internal/server/server.go` `toAPIEvent` (~1272-1293)
- Modify: `internal/api/api.go` (EventKind consts ~50-60)
- Modify: `internal/tui/tui.go` (event switch ~1416-1504; add `guardEnabled` state + send it in message POST)
- Modify: `internal/tui/slash.go` (help ~190; dispatch)
- Test: `internal/api/api_test.go` or `internal/server/server_test.go` (assert mapping); a TUI render assertion if the file has existing render tests

**Interfaces:**
- Consumes: `engine.KindGuard`, `engine.Event.GuardReason`, `api.PostMessageRequest.GuardEnabled`.
- Produces: `api.KindGuard EventKind = "guard"`; TUI `/guard [on|off|status]`.

- [ ] **Step 1: Write the failing test (event mapping)**

Add to `internal/server/server_test.go`:

```go
func TestToAPIEventGuard(t *testing.T) {
	ev := toAPIEvent(engine.Event{Kind: engine.KindGuard, GuardReason: "missing citations"})
	if ev.Kind != api.KindGuard {
		t.Errorf("kind = %q, want guard", ev.Kind)
	}
	if ev.Text != "missing citations" {
		t.Errorf("text = %q, want the guard reason", ev.Text)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestToAPIEventGuard`
Expected: FAIL — `undefined: api.KindGuard`.

- [ ] **Step 3: Add the api kind and map it**

In `internal/api/api.go`, add to the EventKind const block:

```go
	KindGuard           EventKind = "guard"            // output validation warning
```

In `toAPIEvent` (`internal/server/server.go`), after the `if ev.Err != nil` block, add:

```go
	if ev.Kind == engine.KindGuard {
		out.Text = ev.GuardReason
	}
```

- [ ] **Step 4: Run mapping test**

Run: `go test ./internal/server/ -run TestToAPIEventGuard`
Expected: PASS

- [ ] **Step 5: Render KindGuard in the TUI**

In `internal/tui/tui.go`, add a case to the event switch (before `case api.KindDone`):

```go
	case api.KindGuard:
		m.flushThinking()
		m.flushLiveText()
		m.transcript.WriteString("\n" + m.th.elapsedDim.Render("⚠ output guard: "+ev.Text) + "\n")
```

- [ ] **Step 6: Add `/guard` state + command**

In `internal/tui/tui.go`, add a field to the `model` struct: `guardEnabled *bool // nil = server default`. Where the message POST request is built, include it: set `GuardEnabled: m.guardEnabled` on the `api.PostMessageRequest`.

In `internal/tui/slash.go`, add help text near the `/mode` help:

```go
	case "guard":
		return "/guard [on|off|status]\n  Toggle output validation for the current session.\n  Defaults to the configured output_guard.enabled; resets on restart."
```

And handle the command where slash commands are dispatched (mirror the `/mode` handler that returns a `SlashResult{Output: ...}`):

```go
	case "guard":
		switch arg := strings.ToLower(strings.TrimSpace(firstArg(args))); arg {
		case "on", "true":
			v := true
			d.setGuard(&v)
			return SlashResult{Output: "Output guard: on (this session)"}
		case "off", "false":
			v := false
			d.setGuard(&v)
			return SlashResult{Output: "Output guard: off (this session)"}
		default:
			return SlashResult{Output: "Output guard: " + d.guardStatus() + "\nUsage: /guard [on|off|status]"}
		}
```

> Implement `setGuard(*bool)` and `guardStatus() string` on the slash-dispatch struct (`d`), backed by `model.guardEnabled`. `guardStatus` returns "on"/"off"/"default (on)" based on `guardEnabled` and `output_guard.enabled`. Match the exact dispatch shape used by `/mode` in this file — inspect how `d.mode` is read/written and follow the same pattern. Register `guard` in the slash-command completion list next to `mode`.

- [ ] **Step 7: Run tests + build**

Run: `go test ./... && go build ./...`
Expected: PASS and clean build.

- [ ] **Step 8: Commit**

```bash
git add internal/api/ internal/server/ internal/tui/
git commit -m "feat(tui): /guard toggle + output-guard warning line"
```

---

### Task 8: Documentation — init template + README

**Files:**
- Modify: `internal/cli/init.go` (config template, near the `permission:` block ~198-209)
- Modify: `README.md` (new sections after Permission Modes / near Personas)
- No tests (documentation); verify `aegis init` output by inspection.

- [ ] **Step 1: Add config template entries**

In `internal/cli/init.go`, after the `permission:` block, add commented `output_guard` and `personas` sections:

```yaml
# ─────────────────────────────────────────────────────────────────────────────
#  Output validation  (on by default; toggle per session with /guard)
# ─────────────────────────────────────────────────────────────────────────────

output_guard:
  enabled: true            # validate each final answer; /guard off disables per session
  mode: llm                # "llm" (rubric check) or "schema" (required JSON keys)
  max_retries: 1           # corrective retries before surfacing the raw answer
  # rubric: |              # uncomment to override the built-in generic rubric
  #   The response must directly and completely address the request, contain no
  #   placeholders or TODOs, and ground factual claims in tool output.

# ─────────────────────────────────────────────────────────────────────────────
#  Per-persona model overrides  (blank = use the global provider.model above)
# ─────────────────────────────────────────────────────────────────────────────

personas:
  general:                     { model: "" }
  security:                    { model: "" }   # rec: claude-opus-4-8 — deep reasoning
  platform-architect:          { model: "" }   # rec: claude-opus-4-8
  security-architect:          { model: "" }   # rec: claude-opus-4-8 — threat modeling
  security-engineer:           { model: "" }
  appsec-engineer:             { model: "" }   # rec: claude-opus-4-8 — code review
  developer:                   { model: "" }   # rec: claude-sonnet-4-6 — fast iteration
  security-researcher:         { model: "" }
  risk-assessor:               { model: "" }
  business-analyst:            { model: "" }
  data-analyst:                { model: "" }   # rec: claude-opus-4-8
  network-security-architect:  { model: "" }
  report-writer:               { model: "" }   # rec: claude-sonnet-4-6 — long-form writing
  sre:                         { model: "" }
  infrastructure-architect:    { model: "" }
  cloud-architect:             { model: "" }
  cloud-security-engineer:     { model: "" }
```

- [ ] **Step 2: Add README sections**

In `README.md`, add a "Persona Templates" subsection (near the persona/Names discussion) documenting `.aegis/personas/*.md`, the frontmatter fields (description, model, mode, tools, rules, output_guard), the `personas:` config model map and precedence, and an example file. Add an "Output Validation" subsection documenting on-by-default behaviour, `schema` vs `llm` modes, retry/fail-open semantics, and `/guard [on|off|status]`.

- [ ] **Step 3: Verify and commit**

Run: `go build ./... && go test ./...`
Expected: PASS

```bash
git add internal/cli/init.go README.md
git commit -m "docs: persona templates, per-persona model map, and output validation"
```

---

## Self-Review

**Spec coverage:**
- P3.3 file personas → Task 3 (loader) + Task 6 (startup load, selection). ✓
- P3.3 model/mode/tools/rules/guard fields → Task 3 (parse) + Task 6 (apply model/rules/guard; mode seeded at create). Tools parsed, not enforced (non-goal). ✓
- P3.3 config model map → Task 4 + Task 6 precedence + Task 8 template. ✓
- P2.4 schema + llm guards → Task 1. ✓
- P2.4 engine retry + KindGuard → Task 2. ✓
- P2.4 on-by-default generic rubric → Task 4 (defaults) + Task 6 (merge). ✓
- P2.4 per-session /guard toggle → Task 6 (request field) + Task 7 (command). ✓
- P2.4 TUI surfacing → Task 7. ✓
- Session persona persistence → Task 5. ✓

**Placeholder scan:** Task 6 Step 6 and Task 7 Step 6 reference matching the existing `/mode` dispatch shape rather than inlining it — this is because the slash-dispatch struct's exact field/method names must be read from `slash.go` at implementation time; the required behaviour and method signatures (`setGuard(*bool)`, `guardStatus() string`) are specified. All code-bearing steps include complete code.

**Type consistency:** `guard.Config`/`guard.Func`/`guard.Resolve` signatures match between Task 1 and Task 6. `persona.GuardConfig` (frontmatter) is distinct from `guard.Config` (resolver input); Task 6's `outputGuardConfig` converts between them. `Store.Create` 5-arg signature is consistent across Tasks 5 and 6. `newEngine` 5-arg signature consistent across Tasks 6 and 7. `KindGuard`/`GuardReason`/`GuardPassed` consistent across Tasks 2, 6, 7.
