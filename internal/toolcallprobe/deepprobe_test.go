package toolcallprobe

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/provider"
)

// deepScriptedAdapter replays one Stream response per call, in order — the
// same pattern internal/engine's tests use for a deterministic multi-turn
// adapter double.
type deepScriptedAdapter struct {
	turns [][]provider.Event
	calls int
}

func (a *deepScriptedAdapter) Name() string { return "deep-scripted" }

func (a *deepScriptedAdapter) Stream(_ context.Context, _ provider.Request) (<-chan provider.Event, error) {
	events := a.turns[a.calls]
	a.calls++
	ch := make(chan provider.Event, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch, nil
}

func fillInput(t *testing.T, oldStr, newStr string, replaceAll bool) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(struct {
		OldString  string `json:"old_string"`
		NewString  string `json:"new_string"`
		ReplaceAll bool   `json:"replace_all"`
	}{oldStr, newStr, replaceAll})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestRunDeepFill_CleanPass is the baseline: a model that fills each of the
// three sections one at a time, targeting a unique string per call, should
// report no failure shape.
func TestRunDeepFill_CleanPass(t *testing.T) {
	adapter := &deepScriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "1", Name: "edit_fill", Input: fillInput(t, "## Section Alpha\n<!-- PENDING -->", "## Section Alpha\nAlpha content.", false)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "2", Name: "edit_fill", Input: fillInput(t, "## Section Beta\n<!-- PENDING -->", "## Section Beta\nBeta content.", false)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "3", Name: "edit_fill", Input: fillInput(t, "## Section Gamma\n<!-- PENDING -->", "## Section Gamma\nGamma content.", false)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "All sections are now filled."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	res, err := RunDeepFill(context.Background(), adapter, "m")
	if err != nil {
		t.Fatalf("RunDeepFill: %v", err)
	}
	if !res.Clean() {
		t.Errorf("expected a clean result, got %+v", res)
	}
}

// TestRunDeepFill_FabricatedCompletion is P38.6's shape: the model claims the
// document is finished on its very first turn, having made zero tool calls.
func TestRunDeepFill_FabricatedCompletion(t *testing.T) {
	adapter := &deepScriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventTextDelta, Text: "The document is now complete — all sections filled."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	res, err := RunDeepFill(context.Background(), adapter, "m")
	if err != nil {
		t.Fatalf("RunDeepFill: %v", err)
	}
	if !res.FabricatedCompletion {
		t.Errorf("expected FabricatedCompletion, got %+v", res)
	}
	if res.ClobberedMarkers || res.TimedOut {
		t.Errorf("expected only FabricatedCompletion set, got %+v", res)
	}
}

// TestRunDeepFill_ClobberedMarkers is P38.7's shape: the model targets the
// bare, identical PENDING marker with replace_all instead of a
// section-specific string, blanket-overwriting every remaining section.
func TestRunDeepFill_ClobberedMarkers(t *testing.T) {
	adapter := &deepScriptedAdapter{turns: [][]provider.Event{
		{
			{Type: provider.EventToolUse, ToolUse: &provider.ToolUseBlock{ID: "1", Name: "edit_fill", Input: fillInput(t, deepFillMarker, "internet-facing", true)}},
			{Type: provider.EventDone, Stop: provider.StopToolUse},
		},
		{
			{Type: provider.EventTextDelta, Text: "Filled the document."},
			{Type: provider.EventDone, Stop: provider.StopEndTurn},
		},
	}}

	res, err := RunDeepFill(context.Background(), adapter, "m")
	if err != nil {
		t.Fatalf("RunDeepFill: %v", err)
	}
	if !res.ClobberedMarkers {
		t.Errorf("expected ClobberedMarkers, got %+v", res)
	}
	if res.FabricatedCompletion || res.TimedOut {
		t.Errorf("expected only ClobberedMarkers set, got %+v", res)
	}
}

// TestRunDeepFill_TimedOut is the non-convergence shape: the model never
// makes progress (no tool calls, no completion claim) and the probe gives up
// after its bounded turn budget.
func TestRunDeepFill_TimedOut(t *testing.T) {
	stallTurn := []provider.Event{
		{Type: provider.EventTextDelta, Text: "Still working on it."},
		{Type: provider.EventDone, Stop: provider.StopEndTurn},
	}
	adapter := &deepScriptedAdapter{turns: [][]provider.Event{
		stallTurn, stallTurn, stallTurn, stallTurn, stallTurn, stallTurn,
	}}

	res, err := RunDeepFill(context.Background(), adapter, "m")
	if err != nil {
		t.Fatalf("RunDeepFill: %v", err)
	}
	if !res.TimedOut {
		t.Errorf("expected TimedOut, got %+v", res)
	}
	if res.FabricatedCompletion || res.ClobberedMarkers {
		t.Errorf("expected only TimedOut set, got %+v", res)
	}
}

// The probe must offer the mechanism the phased drive actually uses. Offering
// only the exact-match edit_fill under-reported fitness: qwen3:14b aborts that
// probe on "old_string occurs 3 times" and then fills a complete threat-model
// suite through fill_marker without one failure.
func TestMarkerFillToolTargetsOneMarker(t *testing.T) {
	fx := newFillFixture()
	mf := &markerFillTool{fx: fx}
	ctx := context.Background()

	res, err := mf.Execute(ctx, json.RawMessage(`{}`))
	if err != nil || res.IsError {
		t.Fatalf("listing failed: %v %q", err, res.Content)
	}
	if !strings.Contains(res.Content, "3 marker(s) remain") {
		t.Errorf("listing = %q, want 3 remaining", res.Content)
	}

	// Filling the middle marker leaves the others intact.
	res, err = mf.Execute(ctx, json.RawMessage(`{"index":2,"content":"Beta body."}`))
	if err != nil || res.IsError {
		t.Fatalf("fill failed: %v %q", err, res.Content)
	}
	fx.mu.Lock()
	got := fx.content
	fx.mu.Unlock()
	if !strings.Contains(got, "Beta body.") {
		t.Errorf("content not filled:\n%s", got)
	}
	if n := strings.Count(got, deepFillMarker); n != 2 {
		t.Errorf("expected 2 markers left, got %d:\n%s", n, got)
	}
	if !strings.Contains(got, "## Section Alpha\n"+deepFillMarker) {
		t.Errorf("Alpha's marker was disturbed:\n%s", got)
	}

	// An out-of-range index reports the real count instead of failing blankly.
	res, _ = mf.Execute(ctx, json.RawMessage(`{"index":9,"content":"x"}`))
	if !res.IsError || !strings.Contains(res.Content, "2 marker(s) remain") {
		t.Errorf("out-of-range error should name the real count, got %q", res.Content)
	}

	// Filling every marker reports completion, which is the probe's done signal.
	_, _ = mf.Execute(ctx, json.RawMessage(`{"index":1,"content":"Alpha body."}`))
	res, _ = mf.Execute(ctx, json.RawMessage(`{"index":1,"content":"Gamma body."}`))
	if !strings.Contains(res.Content, "no markers remain") {
		t.Errorf("final fill = %q, want a no-markers-remain signal", res.Content)
	}
}

// The probe's prompts must steer to fill_marker — the mechanism the drive
// uses — while edit_fill stays registered so the P38.7 clobber shape remains
// reachable and still reported.
func TestDeepProbePromptsSteerToFillMarker(t *testing.T) {
	all := deepFillSystem + deepFillPrompt + deepFillContinuePrompt
	if !strings.Contains(all, "fill_marker") {
		t.Error("probe prompts must name fill_marker")
	}
	if strings.Contains(all, "edit_fill") {
		t.Error("probe prompts should not steer to edit_fill")
	}
}
