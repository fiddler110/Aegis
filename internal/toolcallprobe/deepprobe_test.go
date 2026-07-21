package toolcallprobe

import (
	"context"
	"encoding/json"
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
