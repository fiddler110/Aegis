package builtin

import (
	"context"
	"strings"
	"testing"

	"github.com/fiddler110/aegis/internal/tool"
)

func TestAdviseToolIdentity(t *testing.T) {
	at := &adviseTool{root: t.TempDir()}
	if at.Name() != "security_advise" {
		t.Errorf("Name() = %q", at.Name())
	}
	if at.Capability() != tool.CapNetwork {
		t.Errorf("Capability() = %q, want network", at.Capability())
	}
}

func TestAdviseToolNoteAndList(t *testing.T) {
	at := &adviseTool{root: t.TempDir()}
	ctx := context.Background()

	res, err := at.Execute(ctx, mustJSON(t, map[string]any{
		"action":     "note",
		"engagement": "acme-2026q3",
		"text":       "kicked off recon against 10.0.0.0/24",
		"tags":       []string{"recon"},
	}))
	if err != nil || res.IsError {
		t.Fatalf("note: %v %+v", err, res)
	}

	res, err = at.Execute(ctx, mustJSON(t, map[string]any{
		"action":     "list",
		"engagement": "acme-2026q3",
	}))
	if err != nil || res.IsError {
		t.Fatalf("list: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "kicked off recon") {
		t.Errorf("list output missing note text: %q", res.Content)
	}

	// "log" is documented as an alias for "list".
	res2, err := at.Execute(ctx, mustJSON(t, map[string]any{
		"action":     "log",
		"engagement": "acme-2026q3",
	}))
	if err != nil || res2.IsError {
		t.Fatalf("log: %v %+v", err, res2)
	}
	if res2.Content != res.Content {
		t.Errorf("log and list produced different output:\nlog:  %q\nlist: %q", res2.Content, res.Content)
	}
}

func TestAdviseToolListUnknownEngagement(t *testing.T) {
	at := &adviseTool{root: t.TempDir()}
	res, err := at.Execute(context.Background(), mustJSON(t, map[string]any{
		"action":     "list",
		"engagement": "never-started",
	}))
	if err != nil || res.IsError {
		t.Fatalf("list: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "no notes recorded") {
		t.Errorf("expected a no-notes message, got %q", res.Content)
	}
}

func TestAdviseToolNoteRequiresEngagement(t *testing.T) {
	at := &adviseTool{root: t.TempDir()}
	res, err := at.Execute(context.Background(), mustJSON(t, map[string]any{
		"action": "note",
		"text":   "no engagement given",
	}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.IsError {
		t.Error("expected an error result when engagement is missing")
	}
}

func TestAdviseToolSuggest(t *testing.T) {
	at := &adviseTool{root: t.TempDir()}
	ctx := context.Background()

	res, err := at.Execute(ctx, mustJSON(t, map[string]any{
		"action":     "suggest",
		"engagement": "fresh-engagement",
	}))
	if err != nil || res.IsError {
		t.Fatalf("suggest on empty notebook: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "recon_scan") {
		t.Errorf("expected a recon_scan suggestion for a fresh engagement, got %q", res.Content)
	}

	if _, err := at.Execute(ctx, mustJSON(t, map[string]any{
		"action":     "note",
		"engagement": "fresh-engagement",
		"text":       "ran recon_scan, no findings",
	})); err != nil {
		t.Fatal(err)
	}
	res, err = at.Execute(ctx, mustJSON(t, map[string]any{
		"action":     "suggest",
		"engagement": "fresh-engagement",
	}))
	if err != nil || res.IsError {
		t.Fatalf("suggest after recon: %v %+v", err, res)
	}
	if strings.Contains(res.Content, "No notes yet") {
		t.Errorf("stale suggestion after a note was recorded: %q", res.Content)
	}
}

func TestAdviseToolStatus(t *testing.T) {
	at := &adviseTool{root: t.TempDir()}
	ctx := context.Background()
	if _, err := at.Execute(ctx, mustJSON(t, map[string]any{
		"action":     "note",
		"engagement": "digest-eng",
		"text":       "ran recon_scan and dast_scan, no findings",
	})); err != nil {
		t.Fatal(err)
	}
	res, err := at.Execute(ctx, mustJSON(t, map[string]any{
		"action":     "status",
		"engagement": "digest-eng",
	}))
	if err != nil || res.IsError {
		t.Fatalf("status: %v %+v", err, res)
	}
	if !strings.Contains(res.Content, "1 note") {
		t.Errorf("expected the digest to report 1 note, got %q", res.Content)
	}
}

func TestAdviseToolUnknownAction(t *testing.T) {
	at := &adviseTool{root: t.TempDir()}
	res, err := at.Execute(context.Background(), mustJSON(t, map[string]any{"action": "delete-everything"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.IsError {
		t.Error("expected an error result for an unknown action")
	}
}

// TestAdviseToolCVELookupWiring exercises the cve_lookup action's argument
// validation without touching the network: LookupCVE (internal/security,
// tested separately against a mocked HTTP transport in cve_test.go) rejects
// a call with neither cve_id nor keyword before ever making a request, so
// this confirms the tool wires the action through correctly.
func TestAdviseToolCVELookupWiring(t *testing.T) {
	at := &adviseTool{root: t.TempDir()}
	res, err := at.Execute(context.Background(), mustJSON(t, map[string]any{"action": "cve_lookup"}))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.IsError {
		t.Error("expected an error result when neither cve_id nor keyword is given")
	}
	if !strings.Contains(res.Content, "cve_lookup failed") {
		t.Errorf("expected a cve_lookup-flavored error, got %q", res.Content)
	}
}

func TestAdviseToolEngagementIsolation(t *testing.T) {
	at := &adviseTool{root: t.TempDir()}
	ctx := context.Background()
	if _, err := at.Execute(ctx, mustJSON(t, map[string]any{"action": "note", "engagement": "eng-a", "text": "note a"})); err != nil {
		t.Fatal(err)
	}
	if _, err := at.Execute(ctx, mustJSON(t, map[string]any{"action": "note", "engagement": "eng-b", "text": "note b"})); err != nil {
		t.Fatal(err)
	}
	res, err := at.Execute(ctx, mustJSON(t, map[string]any{"action": "list", "engagement": "eng-a"}))
	if err != nil || res.IsError {
		t.Fatalf("list eng-a: %v %+v", err, res)
	}
	if strings.Contains(res.Content, "note b") || !strings.Contains(res.Content, "note a") {
		t.Errorf("engagement isolation broken: %q", res.Content)
	}
}
