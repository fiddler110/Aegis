// Package eval is a scenario-based regression harness for agent behavior
// (P9.1). Unit tests elsewhere in internal/engine cover one mechanism at a
// time (budget, loop detection, guard, ...); a Scenario instead drives a
// fully-wired engine.Engine through one or more user turns the way a real
// session would, so a change to persona wiring, tool registration, or the
// engine's turn loop that breaks the interaction between those mechanisms
// shows up here even if every individual unit test still passes.
//
// Scenarios run against a scripted/deterministic provider.Adapter (see
// internal/engine's test adapters for the pattern), not a live model, so
// this suite is part of `go test ./...` and requires no API key or reachable
// model server — it verifies mechanism (which tool ran, whether a gate
// blocked a call, whether an error occurred), not response quality.
//
// A separate live-model tier (live_test.go, gated behind the live_eval build
// tag) covers what a scripted adapter can't: whether the actual text a real
// model produces satisfies a quality rubric, via ExpectRubric + a live
// provider.Adapter. It's excluded from `go test ./...` since it needs a
// reachable model server; see .github/workflows/nightly-eval.yml, which runs
// it nightly against a freshly pulled local model.
package eval

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/engine"
	"github.com/fiddler110/aegis/internal/guard"
	"github.com/fiddler110/aegis/internal/provider"
)

// Scenario describes a scripted multi-turn conversation to run against a
// fully-configured engine. Options must set at minimum Adapter and Tools;
// Gate, Cost, BudgetUSD, LoopThreshold, OutputGuard etc. are all supported
// since Options is passed straight through to engine.New.
type Scenario struct {
	Name    string
	System  string
	Options engine.Options
	Turns   []string // user messages, sent one after another on the same conversation
}

// GuardResult captures one output-guard verdict emitted during a turn (a
// corrective retry's failure, or the final surfaced-anyway warning after
// retries are exhausted).
type GuardResult struct {
	Passed bool
	Reason string
}

// TurnResult captures one user turn's outcome.
type TurnResult struct {
	FinalText   string
	ToolCalls   []string // tool names invoked this turn, in call order
	Steers      []string // steer texts the engine injected this turn, in order
	GuardEvents []GuardResult
	Err         error
}

// Result is the full outcome of running a Scenario.
type Result struct {
	Scenario string
	Turns    []TurnResult
}

// AllToolCalls flattens tool calls across every turn, in order.
func (r *Result) AllToolCalls() []string {
	var out []string
	for _, tr := range r.Turns {
		out = append(out, tr.ToolCalls...)
	}
	return out
}

// AllSteers flattens injected steer texts across every turn, in order.
func (r *Result) AllSteers() []string {
	var out []string
	for _, tr := range r.Turns {
		out = append(out, tr.Steers...)
	}
	return out
}

// AllGuardEvents flattens guard verdicts across every turn, in order.
func (r *Result) AllGuardEvents() []GuardResult {
	var out []GuardResult
	for _, tr := range r.Turns {
		out = append(out, tr.GuardEvents...)
	}
	return out
}

// FinalText returns the last turn's accumulated text ("" if there were no
// turns), the common case for asserting on the scenario's end state.
func (r *Result) FinalText() string {
	if len(r.Turns) == 0 {
		return ""
	}
	return r.Turns[len(r.Turns)-1].FinalText
}

// Run builds a fresh engine from s.Options and drives it through each of
// s.Turns in sequence on a single conversation, collecting a Result. It does
// not fail the test itself — pair it with Check/RunAndCheck or AssertGolden.
func Run(ctx context.Context, s Scenario) (*Result, error) {
	eng, err := engine.New(s.Options)
	if err != nil {
		return nil, fmt.Errorf("eval: build engine for scenario %q: %w", s.Name, err)
	}

	conv := &engine.Conversation{System: s.System}
	result := &Result{Scenario: s.Name}
	for _, userText := range s.Turns {
		conv.Append(provider.Message{Role: provider.RoleUser, Content: []provider.Block{provider.TextBlock{Text: userText}}})

		var tr TurnResult
		tr.Err = eng.Run(ctx, conv, func(ev engine.Event) {
			switch ev.Kind {
			case engine.KindText:
				tr.FinalText += ev.Text
			case engine.KindToolCall:
				tr.ToolCalls = append(tr.ToolCalls, ev.ToolName)
			case engine.KindSteer:
				tr.Steers = append(tr.Steers, ev.Text)
			case engine.KindGuard:
				tr.GuardEvents = append(tr.GuardEvents, GuardResult{Passed: ev.GuardPassed, Reason: ev.GuardReason})
			}
		})
		result.Turns = append(result.Turns, tr)
	}
	return result, nil
}

// Check inspects a Result and returns a descriptive error if it fails,
// nil if it passes.
type Check func(*Result) error

// ExpectToolCalled requires name to appear at least once across all turns.
func ExpectToolCalled(name string) Check {
	return func(r *Result) error {
		for _, tc := range r.AllToolCalls() {
			if tc == name {
				return nil
			}
		}
		return fmt.Errorf("expected tool %q to be called; calls were %v", name, r.AllToolCalls())
	}
}

// ExpectToolNotCalled requires name to never appear across all turns — used
// to assert a permission gate or persona restriction actually blocked
// execution rather than merely being requested by the model.
func ExpectToolNotCalled(name string) Check {
	return func(r *Result) error {
		for _, tc := range r.AllToolCalls() {
			if tc == name {
				return fmt.Errorf("expected tool %q not to be called, but it was", name)
			}
		}
		return nil
	}
}

// ExpectSteerInjected requires text to have been pulled off the steer channel
// and injected into the conversation at least once across all turns.
func ExpectSteerInjected(text string) Check {
	return func(r *Result) error {
		for _, st := range r.AllSteers() {
			if st == text {
				return nil
			}
		}
		return fmt.Errorf("expected steer %q to be injected; injected steers were %v", text, r.AllSteers())
	}
}

// ExpectNoSteerInjected requires the engine to never have injected a steer —
// the P33.2 case, where a run that reaches no further tool round leaves
// whatever was posted sitting on the channel for the daemon to hand back
// rather than injecting (or dropping) it.
func ExpectNoSteerInjected() Check {
	return func(r *Result) error {
		if steers := r.AllSteers(); len(steers) > 0 {
			return fmt.Errorf("expected no steer to be injected, got %v", steers)
		}
		return nil
	}
}

// ExpectNoError requires every turn to have completed without an engine
// error (no budget/loop/max-iteration abort).
func ExpectNoError() Check {
	return func(r *Result) error {
		for i, tr := range r.Turns {
			if tr.Err != nil {
				return fmt.Errorf("turn %d: unexpected error: %w", i, tr.Err)
			}
		}
		return nil
	}
}

// ExpectErrorContains requires at least one turn to have failed with an error
// whose message contains substr (e.g. "cost budget reached", "loop").
func ExpectErrorContains(substr string) Check {
	return func(r *Result) error {
		for _, tr := range r.Turns {
			if tr.Err != nil && strings.Contains(tr.Err.Error(), substr) {
				return nil
			}
		}
		return fmt.Errorf("expected some turn's error to contain %q, got errors: %v", substr, turnErrors(r))
	}
}

// ExpectFinalTextContains requires the last turn's accumulated text to
// contain substr.
func ExpectFinalTextContains(substr string) Check {
	return func(r *Result) error {
		if ft := r.FinalText(); !strings.Contains(ft, substr) {
			return fmt.Errorf("final text %q does not contain %q", ft, substr)
		}
		return nil
	}
}

// ExpectGuardFailureContains requires at least one guard verdict across the
// run to have failed (Passed == false) with a reason containing substr — used
// to assert an adversarial injection attempt did not fool the output guard
// into passing content it shouldn't have.
func ExpectGuardFailureContains(substr string) Check {
	return func(r *Result) error {
		for _, g := range r.AllGuardEvents() {
			if !g.Passed && strings.Contains(g.Reason, substr) {
				return nil
			}
		}
		return fmt.Errorf("expected a failed guard verdict containing %q, got: %v", substr, r.AllGuardEvents())
	}
}

// ExpectNoGuardFailure requires every guard verdict across the run to have
// passed — used to assert legitimate content isn't wrongly rejected.
func ExpectNoGuardFailure() Check {
	return func(r *Result) error {
		for _, g := range r.AllGuardEvents() {
			if !g.Passed {
				return fmt.Errorf("expected no guard failures, got: %v", r.AllGuardEvents())
			}
		}
		return nil
	}
}

// ExpectRubric judges the final turn's text against rubric via a live model
// call (guard.LLMGuard — the same mechanism the engine's output guard uses),
// closing the gap the package doc calls out: the Check helpers above assert
// on mechanism (which tool ran, whether an error occurred), which a scripted
// adapter can verify deterministically, but nothing here could previously
// catch a persona or prompt-engineering change that makes response quality
// worse while every mechanism still behaves identically. adapter/model must
// be a real, reachable provider — pair this with a Scenario whose
// Options.Adapter is a live adapter, not the scripted test adapters used
// elsewhere in this package's own tests.
func ExpectRubric(ctx context.Context, adapter provider.Adapter, model, rubric string) Check {
	return func(r *Result) error {
		judge := guard.LLMGuard(adapter, model, rubric)
		ok, reason, _ := judge(ctx, guard.Input{Text: r.FinalText()})
		if !ok {
			return fmt.Errorf("rubric %q failed: %s", rubric, reason)
		}
		return nil
	}
}

func turnErrors(r *Result) []error {
	var errs []error
	for _, tr := range r.Turns {
		errs = append(errs, tr.Err)
	}
	return errs
}

// RunAndCheck runs s and reports every failing check via t.Errorf, with the
// scenario name prefixed for easy identification in test output.
func RunAndCheck(t *testing.T, ctx context.Context, s Scenario, checks ...Check) *Result {
	t.Helper()
	r, err := Run(ctx, s)
	if err != nil {
		t.Fatalf("scenario %q: %v", s.Name, err)
	}
	for _, check := range checks {
		if err := check(r); err != nil {
			t.Errorf("scenario %q: %v", s.Name, err)
		}
	}
	return r
}

// goldenTranscript is the deterministic, serializable subset of a Result used
// for golden-file comparison — it deliberately omits nothing timing-related
// (Result has no such fields) but exists as its own type so a future
// non-deterministic field added to Result doesn't silently break every golden
// file.
type goldenTranscript struct {
	Scenario string       `json:"scenario"`
	Turns    []goldenTurn `json:"turns"`
}

type goldenTurn struct {
	FinalText string   `json:"final_text"`
	ToolCalls []string `json:"tool_calls,omitempty"`
	Err       string   `json:"err,omitempty"`
}

func toGolden(r *Result) goldenTranscript {
	g := goldenTranscript{Scenario: r.Scenario}
	for _, tr := range r.Turns {
		gt := goldenTurn{FinalText: tr.FinalText, ToolCalls: tr.ToolCalls}
		if tr.Err != nil {
			gt.Err = tr.Err.Error()
		}
		g.Turns = append(g.Turns, gt)
	}
	return g
}

// AssertGolden compares r's deterministic transcript against the JSON file at
// path, failing the test on any mismatch. Set the environment variable
// AEGIS_EVAL_UPDATE=1 to (re)write the golden file instead of comparing —
// review the diff before committing, the same as any other golden-file
// workflow.
func AssertGolden(t *testing.T, path string, r *Result) {
	t.Helper()
	got, err := json.MarshalIndent(toGolden(r), "", "  ")
	if err != nil {
		t.Fatalf("marshal golden transcript: %v", err)
	}
	got = append(got, '\n')

	if os.Getenv("AEGIS_EVAL_UPDATE") != "" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("create golden dir: %v", err)
		}
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden file: %v", err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v (rerun with AEGIS_EVAL_UPDATE=1 to create it)", path, err)
	}
	if string(want) != string(got) {
		t.Errorf("golden transcript mismatch for %s:\n--- want ---\n%s\n--- got ---\n%s", path, want, got)
	}
}
